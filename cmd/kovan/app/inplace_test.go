package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/session"
)

// gitRepo builds a throwaway repo on `main` with one commit and the given extra
// branches, and returns its root.
func gitRepo(t *testing.T, branches ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	cmds := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	}
	for _, b := range branches {
		cmds = append(cmds, []string{"branch", b})
	}
	for _, args := range cmds {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func TestResolveInPlaceBranch(t *testing.T) {
	root := gitRepo(t, "feat/x")
	repo := &git.Repo{Root: root}

	// Blank target → the current branch, no switch.
	if got, err := resolveInPlaceBranch(repo, ""); err != nil || got != "main" {
		t.Fatalf("blank target = %q, %v; want main", got, err)
	}
	if b, _ := repo.CurrentBranch(root); b != "main" {
		t.Fatalf("blank target must not switch; on %q", b)
	}

	// Same-as-current target → no switch.
	if got, err := resolveInPlaceBranch(repo, "main"); err != nil || got != "main" {
		t.Fatalf("same target = %q, %v; want main", got, err)
	}

	// Explicit other branch on a clean tree → switch.
	if got, err := resolveInPlaceBranch(repo, "feat/x"); err != nil || got != "feat/x" {
		t.Fatalf("explicit target = %q, %v; want feat/x", got, err)
	}
	if b, _ := repo.CurrentBranch(root); b != "feat/x" {
		t.Fatalf("explicit target must switch; on %q", b)
	}

	// origin/<name> is reduced to <name> (DWIM tracking branch). Back on main first.
	if err := repo.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveInPlaceBranch(repo, "origin/feat/x"); err != nil || got != "feat/x" {
		t.Fatalf("origin-stripped target = %q, %v; want feat/x", got, err)
	}
}

func TestResolveInPlaceBranchDirtyRefuses(t *testing.T) {
	root := gitRepo(t, "feat/x")
	repo := &git.Repo{Root: root}
	// Dirty the tree on a tracked file.
	if out, err := exec.Command("git", "-C", root, "commit", "-q", "--allow-empty", "-m", "x").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "f.txt").CombinedOutput(); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// Switching to another branch with a dirty tree is refused, and no switch happens.
	if _, err := resolveInPlaceBranch(repo, "feat/x"); err == nil {
		t.Fatal("dirty switch should be refused")
	}
	if b, _ := repo.CurrentBranch(root); b != "main" {
		t.Fatalf("refused switch must leave the branch alone; on %q", b)
	}
	// Staying on the current branch is fine even when dirty.
	if got, err := resolveInPlaceBranch(repo, "main"); err != nil || got != "main" {
		t.Fatalf("dirty same-branch = %q, %v; want main (no switch needed)", got, err)
	}
}

func TestInPlaceAgentFor(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	root := t.TempDir()

	if id := inPlaceAgentFor(root); id != "" {
		t.Fatalf("no agents yet, got %q", id)
	}

	m := &session.Manifest{
		ID: "a1", Repo: "r", RepoRoot: root, Worktree: root,
		Branch: "main", Tmux: "kovan-r-a1", InPlace: true, State: "working",
		CreatedAt: time.Now(),
	}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	if id := inPlaceAgentFor(root); id != "a1" {
		t.Fatalf("should find the in-place agent, got %q", id)
	}

	// A different repo is not a collision.
	if id := inPlaceAgentFor(t.TempDir()); id != "" {
		t.Fatalf("other repo should not collide, got %q", id)
	}

	// An archived in-place agent no longer holds the checkout: its tmux is dead,
	// so there is no live tab to join — a newcomer takes the checkout fresh.
	m.Archived = true
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	if id := inPlaceAgentFor(root); id != "" {
		t.Fatalf("archived in-place agent should not hold the checkout, got %q", id)
	}
}

// TestStartAgentInPlace exercises the in-place start core end to end: no worktree
// dir is created, the manifest sits at the repo root marked in-place, the chosen
// branch is checked out, a second agent on the checkout joins as a tab sharing
// the branch, and teardown leaves the checkout on its branch with no kovan
// residue. Skipped without git/tmux.
func TestStartAgentInPlace(t *testing.T) {
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

	repo := gitRepo(t, "feat/x")
	t.Chdir(repo)
	// git.Open canonicalizes the root (resolving /var → /private/var on macOS),
	// so compare against the resolved path the manifest will carry.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	parent := filepath.Dir(repo)
	before, _ := os.ReadDir(parent)

	res, err := startAgent("ip1", "in-place goal", "feat/x", "", "", "", true, "")
	if err != nil {
		t.Fatalf("startAgent in-place: %v", err)
	}
	t.Cleanup(func() { _, _ = removeAgent("ip1", true) })

	m := res.Manifest
	if !m.InPlace {
		t.Error("manifest should be marked in-place")
	}
	if m.Worktree != repo {
		t.Errorf("in-place worktree = %q, want repo root %q", m.Worktree, repo)
	}
	if m.Branch != "feat/x" {
		t.Errorf("branch = %q, want feat/x (the checked-out target)", m.Branch)
	}
	if b := currentBranch(repo); b != "feat/x" {
		t.Errorf("checkout should be switched to feat/x, on %q", b)
	}
	// No sibling worktree dir was created.
	after, _ := os.ReadDir(parent)
	if len(after) != len(before) {
		t.Errorf("in-place must not create a worktree dir: %d → %d entries", len(before), len(after))
	}

	// A second agent on the same checkout joins as a tab: it shares the worktree
	// and branch, is not in-place, and creates no new worktree dir.
	t.Cleanup(func() { _, _ = removeAgent("ip2", true) })
	res2, err := startAgent("ip2", "second", "", "", "", "", true, "")
	if err != nil {
		t.Fatalf("a second agent on the checkout should join as a tab, not be refused: %v", err)
	}
	if !res2.Tab {
		t.Error("the second agent on the checkout should be flagged as a tab")
	}
	if res2.Manifest.InPlace {
		t.Error("a tab joining the checkout is not in-place")
	}
	if res2.Manifest.Worktree != repo {
		t.Errorf("tab worktree = %q, want the checkout %q", res2.Manifest.Worktree, repo)
	}
	if res2.Manifest.Branch != "feat/x" {
		t.Errorf("tab branch = %q, want inherited feat/x", res2.Manifest.Branch)
	}
	if after, _ := os.ReadDir(parent); len(after) != len(before) {
		t.Errorf("a tab must not create a worktree dir: %d → %d entries", len(before), len(after))
	}

	// Removing the first agent while the tab lives keeps the checkout (last-out
	// tears down); the branch stays.
	if _, err := removeAgent("ip1", true); err != nil {
		t.Fatalf("removeAgent ip1: %v", err)
	}
	if b := currentBranch(repo); b != "feat/x" {
		t.Errorf("removing one tab should keep the branch, on %q", b)
	}
	if list, _ := session.List(); len(list) != 1 || list[0].ID != "ip2" {
		t.Errorf("only the tab should remain, got %+v", list)
	}

	// Removing the last tab does the in-place cleanup, keeping the branch and
	// clearing the index.
	if _, err := removeAgent("ip2", true); err != nil {
		t.Fatalf("removeAgent ip2: %v", err)
	}
	if b := currentBranch(repo); b != "feat/x" {
		t.Errorf("teardown should keep the branch, on %q", b)
	}
	if list, _ := session.List(); len(list) != 0 {
		t.Errorf("teardown should clear the index; still has %d", len(list))
	}
	if _, err := os.Stat(filepath.Join(home, "projects", filepath.Base(repo), "works", "ip1", "context.md")); err != nil {
		t.Errorf("task docs should survive teardown: %v", err)
	}
}

func TestCleanupInPlace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	repoName := "myrepo"

	// A project-layer skill to be linked into the checkout, then reversed.
	skillSrc := filepath.Join(home, "projects", repoName, "skills", "demo")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The "checkout": a manifest, a CLAUDE.local.md with the managed block, and
	// the linked skill — plus a foreign skill kovan must not touch.
	wt := t.TempDir()
	skillsDir := filepath.Join(wt, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skillSrc, filepath.Join(skillsDir, "demo")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(skillsDir, "mine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(wt, claudeLocalFile)
	if err := os.WriteFile(local, []byte(upsertManagedBlock("# mine\n", "BODY")), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &session.Manifest{ID: "a1", Repo: repoName, RepoRoot: wt, Worktree: wt, Branch: "main", Tmux: "kovan-myrepo-a1", InPlace: true, CreatedAt: time.Now()}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	if err := cleanupInPlace(wt, "kovan-myrepo-a1", "", "", repoName); err != nil {
		t.Fatalf("cleanupInPlace: %v", err)
	}

	// Skill symlink gone; the foreign real dir kept.
	if _, err := os.Lstat(filepath.Join(skillsDir, "demo")); !os.IsNotExist(err) {
		t.Error("scoped skill symlink should be removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("foreign skill dir must be left intact")
	}
	// Manifest gone from the index.
	if _, err := session.ReadByTmux("kovan-myrepo-a1"); !os.IsNotExist(err) {
		t.Errorf("manifest should be removed, got err = %v", err)
	}
	// CLAUDE.local.md: managed block gone, user content kept.
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("CLAUDE.local.md should remain (user content): %v", err)
	}
	if strings.Contains(string(data), managedStart) || !strings.Contains(string(data), "# mine") {
		t.Errorf("clear left wrong content:\n%s", data)
	}
}
