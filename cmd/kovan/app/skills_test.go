package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/git"
)

func mkKovanSkill(t *testing.T, home, layerRel, name string) string {
	t.Helper()
	dir := filepath.Join(home, layerRel, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLinkGlobalSkills(t *testing.T) {
	home := t.TempDir()
	src := mkKovanSkill(t, home, filepath.Join("method", "global"), "foo")
	claudeSkills := filepath.Join(t.TempDir(), "skills")

	linked, err := linkGlobalSkills(home, claudeSkills)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0] != "foo" {
		t.Fatalf("linked = %v, want [foo]", linked)
	}
	if target, _ := os.Readlink(filepath.Join(claudeSkills, "foo")); target != src {
		t.Errorf("foo links to %q, want %q", target, src)
	}

	// A pre-existing non-kovan skill of the same name is left untouched.
	mkKovanSkill(t, home, filepath.Join("method", "global"), "bar")
	theirs := filepath.Join(claudeSkills, "bar")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}

	linked, err = linkGlobalSkills(home, claudeSkills)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 0 {
		t.Errorf("re-run linked %v, want nothing (foo ours, bar theirs)", linked)
	}
	if info, _ := os.Lstat(theirs); info.Mode()&os.ModeSymlink != 0 {
		t.Error("kovan clobbered a pre-existing non-kovan skill with a symlink")
	}
}

func TestLinkWorktreeSkills(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	acc := mkKovanSkill(t, home, filepath.Join("method", "accounts", "personal"), "voice")
	proj := mkKovanSkill(t, home, filepath.Join("projects", "myrepo"), "repo-skill")

	repoRoot := t.TempDir()
	if out, err := exec.Command("git", "-C", repoRoot, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	repo, err := git.Open(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	// A committed repo skill of the same shape must be left alone.
	committed := filepath.Join(repoRoot, ".claude", "skills", "sta-ticket")
	if err := os.MkdirAll(committed, 0o755); err != nil {
		t.Fatal(err)
	}

	linked, err := linkWorktreeSkills(repo, repoRoot, "personal", "code", "myrepo")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range linked {
		got[n] = true
	}
	if len(linked) != 2 || !got["voice"] || !got["repo-skill"] {
		t.Errorf("linked = %v, want voice and repo-skill", linked)
	}

	for name, want := range map[string]string{"voice": acc, "repo-skill": proj} {
		if target, _ := os.Readlink(filepath.Join(repoRoot, ".claude", "skills", name)); target != want {
			t.Errorf("%s links to %q, want %q", name, target, want)
		}
	}
	if info, _ := os.Lstat(committed); info.Mode()&os.ModeSymlink != 0 {
		t.Error("kovan clobbered the committed sta-ticket skill")
	}

	exclude, err := os.ReadFile(filepath.Join(repoRoot, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".claude/skills/voice", ".claude/skills/repo-skill"} {
		if !strings.Contains(string(exclude), want) {
			t.Errorf("exclude missing %q:\n%s", want, exclude)
		}
	}
	if strings.Contains(string(exclude), "sta-ticket") {
		t.Errorf("a skipped (committed) skill should not be excluded:\n%s", exclude)
	}
}
