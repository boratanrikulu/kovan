package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkGlobalMethod(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, "method", "global")
	if err := os.MkdirAll(global, 0o755); err != nil {
		t.Fatal(err)
	}
	soul := filepath.Join(global, "soul.md")
	if err := os.WriteFile(soul, []byte("be kind"), 0o644); err != nil {
		t.Fatal(err)
	}

	claudeMd := filepath.Join(t.TempDir(), "CLAUDE.md")
	files, err := linkGlobalMethod(claudeMd, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("linked %v, want 1 file", files)
	}
	got, _ := os.ReadFile(claudeMd)
	if !strings.Contains(string(got), "@"+soul) {
		t.Errorf("CLAUDE.md missing the import line:\n%s", got)
	}
	if !strings.Contains(string(got), managedStart) {
		t.Error("missing the managed sentinel")
	}

	// Idempotent: a second link keeps a single managed block.
	if _, err := linkGlobalMethod(claudeMd, home); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(claudeMd)
	if strings.Count(string(got), managedStart) != 1 {
		t.Errorf("re-link duplicated the block:\n%s", got)
	}
}

func TestEffectiveMethod(t *testing.T) {
	home := t.TempDir()
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{home}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	soul := mk("method", "global", "soul.md")
	voice := mk("method", "accounts", "personal", "voice.md")
	priv := mk("projects", "kovan", "notes.md")

	repoRoot := t.TempDir()
	agentsMd := filepath.Join(repoRoot, "AGENTS.md")
	if err := os.WriteFile(agentsMd, []byte("facts"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Task docs live in the durable kovan store, keyed by repo + task.dir + id.
	ctxMd := filepath.Join(home, "projects", "kovan", "works", "TASK-1", "context.md")
	if err := os.MkdirAll(filepath.Dir(ctxMd), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(ctxMd, []byte("brief"), 0o644)

	c := methodCtx{account: "personal", domain: "", repo: "kovan", repoRoot: repoRoot, taskDir: "works", id: "TASK-1"}
	layers := effectiveMethod(home, c)

	want := map[string]string{
		"global":           soul,
		"account:personal": voice,
		"project:kovan":    priv,
		"project (public)": agentsMd,
		"task":             ctxMd,
	}
	for _, l := range layers {
		if exp, ok := want[l.Name]; ok {
			if len(l.Files) != 1 || l.Files[0].Path != exp || l.Files[0].Depth != 0 {
				t.Errorf("layer %s = %v, want [%s] at depth 0", l.Name, l.Files, exp)
			}
			delete(want, l.Name)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing layers: %v", want)
	}
}

func TestLinkGlobalMethodNoClobber(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "method", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMd := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(claudeMd, []byte("# my own memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := linkGlobalMethod(claudeMd, home); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(claudeMd)
	if !strings.Contains(string(got), "# my own memory") {
		t.Error("clobbered the user's CLAUDE.md")
	}
	if _, err := os.Stat(claudeMd + ".bak"); err != nil {
		t.Error("backup not written")
	}
	// Empty global layer still leaves a managed block (a placeholder note).
	if !strings.Contains(string(got), managedStart) {
		t.Error("no managed block written for an empty global layer")
	}
}
