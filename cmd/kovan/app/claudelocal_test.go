package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/mode"
)

func TestClaudeLocalBlockPaths(t *testing.T) {
	taskAbs := filepath.Join("/home/.kovan/projects/kovan/works", "TASK-1")
	codeMode := &mode.Mode{Name: "code", Posture: "edit", Docs: []string{"spec.md", "test-plan.md"}}
	got := claudeLocalBlock("TASK-1", "fix vfs", taskAbs, codeMode)
	for _, want := range []string{
		"## Task — TASK-1: fix vfs  (mode: code)",
		filepath.Join(taskAbs, "context.md"),
		filepath.Join(taskAbs, "spec.md"),
		filepath.Join(taskAbs, "learnings.md"),
		"approval",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}

	// A read-only mode tells the agent not to touch code and names its artifact.
	ro := &mode.Mode{Name: "review", Posture: "read-only", Docs: []string{"review.md"}}
	got = claudeLocalBlock("TASK-2", "review pr", taskAbs, ro)
	for _, want := range []string{"(mode: review)", filepath.Join(taskAbs, "review.md"), "do not modify"} {
		if !strings.Contains(got, want) {
			t.Errorf("read-only block missing %q:\n%s", want, got)
		}
	}
}

func TestUpsertManagedBlock(t *testing.T) {
	// Empty file → just the block.
	out := upsertManagedBlock("", "BODY")
	if !strings.Contains(out, managedStart) || !strings.Contains(out, "BODY") || !strings.Contains(out, managedEnd) {
		t.Fatalf("empty upsert wrong:\n%s", out)
	}

	// Existing user content is preserved and the block appended once.
	withUser := upsertManagedBlock("# my notes\n", "FIRST")
	if !strings.Contains(withUser, "# my notes") {
		t.Error("user content lost on append")
	}

	// Re-running replaces the block in place, not duplicating it.
	again := upsertManagedBlock(withUser, "SECOND")
	if strings.Count(again, managedStart) != 1 {
		t.Errorf("managed block duplicated:\n%s", again)
	}
	if !strings.Contains(again, "SECOND") || strings.Contains(again, "FIRST") {
		t.Errorf("block not replaced:\n%s", again)
	}
	if !strings.Contains(again, "# my notes") {
		t.Error("user content lost on replace")
	}
}

func TestRemoveManagedBlock(t *testing.T) {
	// User content on both sides is kept; the block (and its joining blanks) goes.
	withUser := upsertManagedBlock("# my notes\n", "BODY")
	got := removeManagedBlock(withUser)
	if strings.Contains(got, managedStart) || strings.Contains(got, "BODY") {
		t.Errorf("managed block not stripped:\n%s", got)
	}
	if !strings.Contains(got, "# my notes") {
		t.Errorf("user content lost:\n%s", got)
	}

	// Block-only file reduces to empty.
	if got := removeManagedBlock(upsertManagedBlock("", "BODY")); strings.TrimSpace(got) != "" {
		t.Errorf("block-only file should reduce to empty, got %q", got)
	}

	// A file with no managed block is returned unchanged.
	if got := removeManagedBlock("# plain\n"); got != "# plain\n" {
		t.Errorf("unmanaged file changed: %q", got)
	}
}

func TestClearClaudeLocal(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	wt := t.TempDir()
	path := filepath.Join(wt, claudeLocalFile)

	// A file with user content keeps the content, drops the block.
	if err := os.WriteFile(path, []byte(upsertManagedBlock("# mine\n", "BODY")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clearClaudeLocal(wt); err != nil {
		t.Fatalf("clearClaudeLocal: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file should remain (had user content): %v", err)
	}
	if strings.Contains(string(data), managedStart) || !strings.Contains(string(data), "# mine") {
		t.Errorf("clear left wrong content:\n%s", data)
	}

	// A block-only file is removed entirely.
	if err := os.WriteFile(path, []byte(upsertManagedBlock("", "BODY")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clearClaudeLocal(wt); err != nil {
		t.Fatalf("clearClaudeLocal: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("block-only CLAUDE.local.md should be removed")
	}

	// A missing file is a no-op.
	if err := clearClaudeLocal(wt); err != nil {
		t.Errorf("missing file should be a no-op, got %v", err)
	}
}

func TestWriteClaudeLocal(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir()) // no method layers → just the brief pointer
	wt := t.TempDir()
	taskAbs := filepath.Join("/notes", "TASK-1")
	if err := writeClaudeLocal(wt, "TASK-1", "fix vfs", taskAbs, "", "", "kovan", &mode.Mode{Name: "code", Posture: "edit", Docs: []string{"spec.md"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wt, "CLAUDE.local.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), filepath.Join(taskAbs, "context.md")) {
		t.Errorf("CLAUDE.local.md missing brief path:\n%s", got)
	}

	// A second write (e.g. re-run) keeps a single managed block.
	if err := writeClaudeLocal(wt, "TASK-1", "fix vfs v2", taskAbs, "", "", "kovan", &mode.Mode{Name: "code", Posture: "edit", Docs: []string{"spec.md"}}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Count(string(got), managedStart) != 1 {
		t.Errorf("re-run duplicated the block:\n%s", got)
	}
	if !strings.Contains(string(got), "fix vfs v2") {
		t.Error("re-run did not update the title")
	}
}

func TestWriteClaudeLocalMethod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	voice := filepath.Join(home, "method", "accounts", "personal", "voice.md")
	priv := filepath.Join(home, "projects", "kovan", "notes.md")
	for _, f := range []string{voice, priv} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	wt := t.TempDir()
	taskAbs := filepath.Join(home, "projects", "kovan", "works", "TASK-1")
	if err := writeClaudeLocal(wt, "TASK-1", "fix vfs", taskAbs, "personal", "", "kovan", &mode.Mode{Name: "code", Posture: "edit", Docs: []string{"spec.md"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(wt, "CLAUDE.local.md"))
	for _, want := range []string{"## Method", "@" + voice, "@" + priv, filepath.Join(taskAbs, "context.md")} {
		if !strings.Contains(string(got), want) {
			t.Errorf("CLAUDE.local.md missing %q:\n%s", want, got)
		}
	}
	if strings.Count(string(got), managedStart) != 1 {
		t.Error("method composition should stay in the single managed block")
	}
}

func TestWriteClaudeLocalModeMethod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	rev, err := mode.Load(home, "review") // built-in: read-only, ships a method.md
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	taskAbs := filepath.Join("/notes", "TASK-1")
	if err := writeClaudeLocal(wt, "TASK-1", "review pr", taskAbs, "", "", "kovan", rev); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(wt, "CLAUDE.local.md"))
	for _, want := range []string{
		"@" + filepath.Join(home, "modes", "review", "method.md"), // the mode's method @imported by its live path
		"Read-only",                         // the role block states the posture
		filepath.Join(taskAbs, "review.md"), // the artifact it owns
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("CLAUDE.local.md missing %q:\n%s", want, got)
		}
	}
}
