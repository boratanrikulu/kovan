package taskdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldDefaults(t *testing.T) {
	dir := t.TempDir()
	// The code mode's docs stand in for an arbitrary mode's doc set.
	if err := Scaffold(dir, "TASK-1", "fix vfs", "", "", []string{Spec, "test-plan.md"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{Brief, Spec, "test-plan.md", Learnings} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, Brief))
	if !strings.Contains(string(got), "# TASK-1 — fix vfs") {
		t.Errorf("context.md missing filled title heading:\n%s", got)
	}
	if strings.Contains(string(got), "{{") {
		t.Errorf("unsubstituted placeholder remains:\n%s", got)
	}

	spec, _ := os.ReadFile(filepath.Join(dir, Spec))
	for _, want := range []string{"# TASK-1 — Spec", "## Plan", "## Tasks", "## Assumptions & Open Questions"} {
		if !strings.Contains(string(spec), want) {
			t.Errorf("spec.md missing %q:\n%s", want, spec)
		}
	}
}

func TestScaffoldToken(t *testing.T) {
	tmplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmplDir, "context.md"), []byte("ticket TASK-XXXXX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Scaffold(dir, "TASK-42", "t", "TASK-XXXXX", tmplDir, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	if string(got) != "ticket TASK-42\n" {
		t.Errorf("token not substituted: %q", got)
	}
}

func TestScaffoldNoClobber(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, Brief)
	if err := os.WriteFile(brief, []byte("my brief"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Scaffold(dir, "TASK-1", "t", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(brief); string(got) != "my brief" {
		t.Errorf("clobbered existing brief: %q", got)
	}
}
