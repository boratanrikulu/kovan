package method

import (
	"os"
	"path/filepath"
	"testing"
)

func mkSkill(t *testing.T, layerDir, name string) string {
	t.Helper()
	dir := filepath.Join(layerDir, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func bases(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func TestGlobalSkills(t *testing.T) {
	home := t.TempDir()
	if got := GlobalSkills(home); got != nil {
		t.Errorf("absent skills = %v, want nil", got)
	}

	g := filepath.Join(home, "method", "global")
	mkSkill(t, g, "foo")
	mkSkill(t, g, "bar")
	// A subdir without SKILL.md is not a skill.
	if err := os.MkdirAll(filepath.Join(g, "skills", "nope"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := bases(GlobalSkills(home))
	if len(got) != 2 || got[0] != "bar" || got[1] != "foo" {
		t.Errorf("GlobalSkills = %v, want sorted [bar foo]", got)
	}
}

func TestWorktreeLayerSkills(t *testing.T) {
	home := t.TempDir()
	// account: a method file plus a skill; project: a skill only (no *.md).
	acc := filepath.Join(home, "method", "accounts", "personal")
	if err := os.MkdirAll(acc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acc, "voice.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkSkill(t, acc, "voice-skill")
	mkSkill(t, filepath.Join(home, "projects", "kovan"), "repo-skill")

	byName := map[string]Layer{}
	for _, l := range Worktree(home, "personal", "", "kovan") {
		byName[l.Name] = l
	}
	a, ok := byName["account:personal"]
	if !ok || len(a.Files) != 1 || len(a.Skills) != 1 {
		t.Fatalf("account layer = %+v, want 1 file + 1 skill", a)
	}
	if filepath.Base(filepath.Dir(a.Skills[0].Path)) != "voice-skill" || filepath.Base(a.Skills[0].Path) != "SKILL.md" {
		t.Errorf("account skill = %q, want .../voice-skill/SKILL.md", a.Skills[0].Path)
	}
	// A layer with skills but no method files is still surfaced.
	p, ok := byName["project:kovan"]
	if !ok || len(p.Files) != 0 || len(p.Skills) != 1 {
		t.Errorf("skills-only project layer = %+v, want it present with 1 skill", p)
	}
}

func TestWorktreeSkills(t *testing.T) {
	home := t.TempDir()
	mkSkill(t, filepath.Join(home, "method", "accounts", "personal"), "voice")
	mkSkill(t, filepath.Join(home, "method", "domains", "code"), "lint")
	mkSkill(t, filepath.Join(home, "projects", "kovan"), "repo")

	got := bases(WorktreeSkills(home, "personal", "code", "kovan"))
	want := map[string]bool{"voice": true, "lint": true, "repo": true}
	if len(got) != 3 {
		t.Fatalf("WorktreeSkills = %v, want voice/lint/repo", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected skill %q in %v", n, got)
		}
	}

	// Empty account/domain layers contribute nothing.
	if got := bases(WorktreeSkills(home, "", "", "kovan")); len(got) != 1 || got[0] != "repo" {
		t.Errorf("scoped to repo only = %v, want [repo]", got)
	}

	// On a name collision the narrowest scope (project) wins.
	mkSkill(t, filepath.Join(home, "method", "accounts", "personal"), "dup")
	proj := mkSkill(t, filepath.Join(home, "projects", "kovan"), "dup")
	var dupPath string
	for _, p := range WorktreeSkills(home, "personal", "", "kovan") {
		if filepath.Base(p) == "dup" {
			dupPath = p
		}
	}
	if dupPath != proj {
		t.Errorf("dup resolved to %q, want the project skill %q", dupPath, proj)
	}
}
