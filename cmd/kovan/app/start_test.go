package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
)

func TestOpeningPrompt(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "context.md")
	if err := os.WriteFile(brief, []byte("scaffold"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := &agentSpec{
		manifest:  &session.Manifest{Title: "fix vfs"},
		briefPath: brief,
		taskAbs:   dir,
		mode: &mode.Mode{
			Name: "code", Posture: "edit", Docs: []string{"spec.md", "test-plan.md"},
			Prompt: "Read your brief at {{brief}}. Write your spec to {{artifact}}; do not implement until I approve.",
		},
		scaffolded: []byte("scaffold"),
	}

	if got := openingPrompt(spec, true); got != "fix vfs" {
		t.Errorf("quick should use the title, got %q", got)
	}
	if got := openingPrompt(spec, false); got != "fix vfs" {
		t.Errorf("unedited scaffold should fall back to the title, got %q", got)
	}

	if err := os.WriteFile(brief, []byte("# a real brief\n\ndo the thing"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A written brief is spec-first: point at the brief and the spec, and tell the
	// agent to plan and wait for approval before coding.
	got := openingPrompt(spec, false)
	for _, want := range []string{brief, filepath.Join(dir, "spec.md"), "do not implement until I approve"} {
		if !strings.Contains(got, want) {
			t.Errorf("spec-first prompt missing %q:\n%s", want, got)
		}
	}

	if err := os.WriteFile(brief, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := openingPrompt(spec, false); got != "fix vfs" {
		t.Errorf("empty brief should fall back to the title, got %q", got)
	}
}

// TestScaffoldColorDefaults exercises the stripe-color resolution at scaffold
// time: an explicit choice wins, the repo's default from ~/.kovan/config.yaml
// fills an empty one, and no config means no color. Skipped without git.
func TestScaffoldColorDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)

	repo := filepath.Join(t.TempDir(), "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	scaffold := func(id, color string) string {
		t.Helper()
		spec, err := scaffoldAgent(repo, id, "goal", "", "", "", color, false, "", briefInput{})
		if err != nil {
			t.Fatalf("scaffold %s: %v", id, err)
		}
		defer spec.rollback()
		return spec.manifest.Color
	}

	// No config: explicit sticks, empty stays empty.
	if got := scaffold("c1", "red"); got != "red" {
		t.Errorf("explicit color = %q, want red", got)
	}
	if got := scaffold("c2", ""); got != "" {
		t.Errorf("no config, no choice = %q, want empty", got)
	}

	cfg := "projects:\n  myrepo:\n    color: yellow\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := scaffold("c3", ""); got != "yellow" {
		t.Errorf("config default = %q, want yellow", got)
	}
	if got := scaffold("c4", "blue"); got != "blue" {
		t.Errorf("explicit over config = %q, want blue", got)
	}
}
