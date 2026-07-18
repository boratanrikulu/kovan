package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

// TestTabInExistingWorktree exercises a guest tab end to end: spawning with
// --in joins another agent's worktree (sharing its branch, creating no new dir),
// removing the host while the tab lives keeps the worktree, and removing the last
// tab tears it down. Skipped without git/tmux.
func TestTabInExistingWorktree(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	fake := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agent: "+fake+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := gitRepo(t)
	t.Chdir(repo)

	// The host: a normal worktree agent.
	host, err := startAgent("host", "host goal", "", "", "", "", false, "")
	if err != nil {
		t.Fatalf("startAgent host: %v", err)
	}
	t.Cleanup(func() { _, _ = removeAgent("host", true); _, _ = removeAgent("tab1", true) })

	parent := filepath.Dir(host.Manifest.Worktree)
	before, _ := os.ReadDir(parent)

	// The guest tab joins the host's worktree.
	tab, err := startAgent("tab1", "companion", "", "", "", "", false, "host")
	if err != nil {
		t.Fatalf("startAgent tab: %v", err)
	}
	if !tab.Tab {
		t.Error("result should be flagged as a tab")
	}
	if tab.Manifest.Worktree != host.Manifest.Worktree {
		t.Errorf("tab worktree = %q, want host's %q", tab.Manifest.Worktree, host.Manifest.Worktree)
	}
	if tab.Manifest.Branch != host.Manifest.Branch {
		t.Errorf("tab branch = %q, want inherited %q", tab.Manifest.Branch, host.Manifest.Branch)
	}
	if tab.Manifest.InPlace {
		t.Error("a tab in a worktree is not in-place")
	}
	// No new worktree directory was created for the tab.
	if after, _ := os.ReadDir(parent); len(after) != len(before) {
		t.Errorf("a tab must not create a worktree dir: %d → %d", len(before), len(after))
	}

	// Removing the host while the tab lives keeps the worktree (last-out tears down).
	if _, err := removeAgent("host", true); err != nil {
		t.Fatalf("removeAgent host: %v", err)
	}
	if _, err := os.Stat(host.Manifest.Worktree); err != nil {
		t.Errorf("worktree should survive while the tab remains: %v", err)
	}
	if list, _ := session.List(); len(list) != 1 || list[0].ID != "tab1" {
		t.Errorf("only the tab should remain, got %+v", list)
	}

	// Removing the last tab tears the worktree down.
	if _, err := removeAgent("tab1", true); err != nil {
		t.Fatalf("removeAgent tab: %v", err)
	}
	if _, err := os.Stat(host.Manifest.Worktree); !os.IsNotExist(err) {
		t.Errorf("last tab removed: worktree should be gone, stat err = %v", err)
	}
}

// TestTabFlagConflicts pins the rejected flag combinations for --in.
func TestTabFlagConflicts(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	repo := gitRepo(t)

	if _, err := scaffoldAgent(repo, "x", "t", "", "", "", "", true, "host", briefInput{}); err == nil {
		t.Error("--in with --in-place should be refused")
	}
	if _, err := scaffoldAgent(repo, "x", "t", "somebranch", "", "", "", false, "host", briefInput{}); err == nil {
		t.Error("--in with --from should be refused")
	}
}

// TestEditTabInLivePosture: the shared-worktree clobber warning resolves each
// tab's posture from its mode live, so a mode.yaml edit changes the verdict
// with no manifest change.
func TestEditTabInLivePosture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	wt := t.TempDir()
	m := &session.Manifest{ID: "T-1", Title: "g", Worktree: wt, Tmux: "tt", State: "working", TaskMode: "review"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	// review is read-only: no clobber risk.
	if got := editTabIn(wt); got != "" {
		t.Errorf("read-only companion should not warn, got %q", got)
	}

	// Override review's posture on disk to edit — same manifest now warns.
	dir := filepath.Join(home, "modes", "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("r: {{brief}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode.yaml"), []byte("posture: edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := editTabIn(wt); got != "T-1" {
		t.Errorf("posture override to edit should warn, got %q", got)
	}
}
