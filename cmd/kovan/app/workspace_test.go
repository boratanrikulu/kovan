package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
)

// siblingRepos builds two throwaway repos, agent/ and cloud/, side by side the
// way kovan lays worktrees out, and an agent whose primary is agent/.
func siblingRepos(t *testing.T) (parent string, m *session.Manifest) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("KOVAN_HOME", t.TempDir())
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent", "cloud"} {
		root := filepath.Join(parent, name)
		mustGit(t, parent, "init", "-q", "-b", "main", root)
		mustGit(t, root, "config", "user.name", "t")
		mustGit(t, root, "config", "user.email", "t@t")
		mustGit(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	}
	agent := filepath.Join(parent, "agent")
	m = &session.Manifest{ID: "ab12", Title: "fix x", Repo: "agent", RepoRoot: agent, Worktree: agent, Tmux: "kovan-agent-ab12", Branch: "main"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	return parent, m
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestAddWorkspaceCreatesWorktree(t *testing.T) {
	parent, m := siblingRepos(t)

	ws, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatalf("addWorkspace: %v", err)
	}
	want := filepath.Join(parent, "cloud-ab12")
	if ws.Worktree != want || ws.Repo != "cloud" || ws.Adopted {
		t.Errorf("workspace = %+v, want a kovan-made cloud worktree at %s", ws, want)
	}
	if ws.Branch != "feat/t_ab12_fix-x" || currentBranch(want) != ws.Branch {
		t.Errorf("branch = %q (checked out %q), want the repo's template with the agent's id", ws.Branch, currentBranch(want))
	}
	saved, err := session.ReadByTmux(m.Tmux)
	if err != nil || len(saved.Extra) != 1 || saved.Extra[0].Worktree != want {
		t.Fatalf("manifest extra = %+v (err %v), want the cloud worktree recorded", saved, err)
	}

	// Adding the same repo again returns the recorded workspace, no second worktree.
	again, tracked, err := addWorkspace(saved, "cloud", "", "")
	if err != nil || !tracked || again.Worktree != want || len(saved.Extra) != 1 {
		t.Errorf("re-add = %+v, %v (extras %d); want the same workspace once", again, err, len(saved.Extra))
	}
}

func TestAddWorkspaceBranchOverride(t *testing.T) {
	_, m := siblingRepos(t)
	ws, _, err := addWorkspace(m, "cloud", "", "bor_SC-1_fix")
	if err != nil || ws.Branch != "bor_SC-1_fix" || currentBranch(ws.Worktree) != "bor_SC-1_fix" {
		t.Errorf("workspace = %+v, %v; want branch bor_SC-1_fix", ws, err)
	}
}

func TestAddWorkspaceAdoptsExistingWorktree(t *testing.T) {
	parent, m := siblingRepos(t)
	manual := filepath.Join(parent, "cloud-manual")
	mustGit(t, filepath.Join(parent, "cloud"), "worktree", "add", "-q", "-b", "mine", manual)

	ws, _, err := addWorkspace(m, manual, "", "")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !ws.Adopted || ws.Repo != "cloud" || ws.Worktree != manual || ws.Branch != "mine" {
		t.Errorf("workspace = %+v, want cloud-manual adopted on branch mine", ws)
	}
}

func TestAddWorkspaceRejectsOwnRepo(t *testing.T) {
	_, m := siblingRepos(t)
	if _, _, err := addWorkspace(m, "agent", "", ""); err == nil {
		t.Error("adding the agent's own repo should error")
	}
	if _, _, err := addWorkspace(m, "nope", "", ""); err == nil {
		t.Error("an unknown repo should error")
	}
}

func TestRemoveWorkspace(t *testing.T) {
	parent, m := siblingRepos(t)
	made, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(made.Worktree, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, made.Worktree, "add", "f")

	if _, err := removeWorkspace(m, "cloud", false); err == nil {
		t.Fatal("a dirty workspace must not be removed without force")
	}
	if _, err := removeWorkspace(m, "cloud", true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if _, err := os.Stat(made.Worktree); !os.IsNotExist(err) {
		t.Errorf("worktree %s still on disk", made.Worktree)
	}
	if !(&git.Repo{Root: filepath.Join(parent, "cloud")}).BranchExists(made.Branch) {
		t.Errorf("branch %s must be kept", made.Branch)
	}
	if saved, _ := session.ReadByTmux(m.Tmux); len(saved.Extra) != 0 {
		t.Errorf("extras = %+v, want none", saved.Extra)
	}

	manual := filepath.Join(parent, "cloud-manual")
	mustGit(t, filepath.Join(parent, "cloud"), "worktree", "add", "-q", "-b", "mine", manual)
	if _, _, err := addWorkspace(m, manual, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := removeWorkspace(m, manual, false); err != nil {
		t.Fatalf("remove adopted: %v", err)
	}
	if _, err := os.Stat(manual); err != nil {
		t.Errorf("an adopted worktree must stay on disk: %v", err)
	}
}

func TestAddWorkspaceRefreshesClaudeLocal(t *testing.T) {
	_, m := siblingRepos(t)
	code := &mode.Mode{Name: "code", Posture: "edit", Docs: []string{"spec.md"}}
	if err := writeClaudeLocal(m.Worktree, m.ID, m.Title, "/notes", "", "", m.Repo, code, nil); err != nil {
		t.Fatal(err)
	}
	ws, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.Worktree, claudeLocalFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## Workspaces") || !strings.Contains(string(data), ws.Worktree) {
		t.Errorf("CLAUDE.local.md does not list the new workspace:\n%s", data)
	}
}

func TestRemoveAgentTearsDownExtras(t *testing.T) {
	parent, m := siblingRepos(t)
	t.Chdir(m.RepoRoot)
	made, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(made.Worktree, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, made.Worktree, "add", "f")

	// A dirty extra stops the whole remove before anything is torn down.
	if _, err := removeAgent(m.ID, false); err == nil {
		t.Fatal("remove with a dirty extra workspace should error")
	}
	if _, err := os.Stat(made.Worktree); err != nil {
		t.Fatalf("the refused remove must leave the extra in place: %v", err)
	}
	if _, err := session.ReadByTmux(m.Tmux); err != nil {
		t.Fatalf("the refused remove must keep the manifest: %v", err)
	}

	if _, err := removeAgent(m.ID, true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if _, err := os.Stat(made.Worktree); !os.IsNotExist(err) {
		t.Errorf("extra worktree %s survived the agent's removal", made.Worktree)
	}
	if !(&git.Repo{Root: filepath.Join(parent, "cloud")}).BranchExists(made.Branch) {
		t.Errorf("branch %s must be kept", made.Branch)
	}
}

func TestAddWorkspaceNameBeatsSubdir(t *testing.T) {
	parent, m := siblingRepos(t)
	// The agent's own checkout has a cloud/ subdir; a bare name still means the
	// sibling repo, not a path relative to wherever the agent's shell is.
	if err := os.MkdirAll(filepath.Join(m.Worktree, "cloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(m.Worktree)
	ws, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil || ws.Worktree != filepath.Join(parent, "cloud-ab12") {
		t.Errorf("add cloud = %+v, %v; want the sibling repo's worktree", ws, err)
	}
}

func TestAddWorkspaceConflictingReAdd(t *testing.T) {
	parent, m := siblingRepos(t)
	if _, _, err := addWorkspace(m, "cloud", "", ""); err != nil {
		t.Fatal(err)
	}
	manual := filepath.Join(parent, "cloud-manual")
	mustGit(t, filepath.Join(parent, "cloud"), "worktree", "add", "-q", "-b", "mine", manual)
	if _, _, err := addWorkspace(m, manual, "", ""); err == nil || !strings.Contains(err.Error(), "already has") {
		t.Errorf("adding a second cloud worktree = %v, want an 'already has' error", err)
	}
	if _, _, err := addWorkspace(m, "cloud", "", "other"); err == nil {
		t.Error("re-adding with a different --branch should error, not return the old branch")
	}
}

func TestAddWorkspaceOwnRepoByIdentity(t *testing.T) {
	parent, m := siblingRepos(t)
	// The agent was started from inside a linked worktree of agent/, so its repo
	// name is that worktree's basename. agent/ itself is still its own repo.
	linked := filepath.Join(parent, "agent-xyz")
	mustGit(t, m.RepoRoot, "worktree", "add", "-q", "-b", "xyz", linked)
	m.Repo, m.RepoRoot, m.Worktree = "agent-xyz", linked, linked
	if _, _, err := addWorkspace(m, filepath.Join(parent, "agent"), "", ""); err == nil {
		t.Error("the agent's own repo under another name should be refused")
	}
}

func TestCreateWorkspaceNamesOwnerOnCollision(t *testing.T) {
	parent, m := siblingRepos(t)
	if _, _, err := addWorkspace(m, "cloud", "", ""); err != nil {
		t.Fatal(err)
	}
	other := &session.Manifest{ID: "ab12", Title: "y", Repo: "docs", RepoRoot: filepath.Join(parent, "docs"), Worktree: filepath.Join(parent, "docs"), Tmux: "kovan-docs-ab12"}
	if err := other.Write(); err != nil {
		t.Fatal(err)
	}
	_, _, err := addWorkspace(other, "cloud", "", "")
	if err == nil || !strings.Contains(err.Error(), "agent ab12 (agent)") || !strings.Contains(err.Error(), "kovan worktree add <path>") {
		t.Errorf("collision error = %v, want it to name the owning agent and the adopt way out", err)
	}
}

func TestRemoveAgentWithMissingExtra(t *testing.T) {
	parent, m := siblingRepos(t)
	t.Chdir(m.RepoRoot)
	made, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Removed by hand in the other repo: the dir and git's entry are both gone.
	mustGit(t, filepath.Join(parent, "cloud"), "worktree", "remove", "--force", made.Worktree)
	if _, err := removeAgent(m.ID, false); err != nil {
		t.Fatalf("remove with a vanished extra = %v, want it to just drop the record", err)
	}
}

func TestRemoveAgentKeepsExtraAnotherAgentUses(t *testing.T) {
	_, m := siblingRepos(t)
	t.Chdir(m.RepoRoot)
	made, _, err := addWorkspace(m, "cloud", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A second agent started in-place inside that worktree.
	guest := &session.Manifest{ID: "rv", Repo: "cloud-ab12", RepoRoot: made.Worktree, Worktree: made.Worktree, Tmux: "kovan-cloud-ab12-rv"}
	if err := guest.Write(); err != nil {
		t.Fatal(err)
	}
	// The guest has uncommitted work there; it is not this agent's to guard.
	if err := os.WriteFile(filepath.Join(made.Worktree, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, made.Worktree, "add", "f")
	if _, err := removeAgent(m.ID, false); err != nil {
		t.Fatalf("remove = %v; a shared extra is kept, so its changes must not block", err)
	}
	if _, err := os.Stat(made.Worktree); err != nil {
		t.Errorf("a worktree another agent works in must survive: %v", err)
	}
}

func TestAddWorkspaceRefusesUnlistableBranch(t *testing.T) {
	parent, m := siblingRepos(t)
	// A backtick would close the code span CLAUDE.local.md lists the branch in,
	// and an @path after it would read as an import.
	if _, _, err := addWorkspace(m, "cloud", "", "x`@/etc/hosts"); err == nil {
		t.Fatal("a branch with a backtick should be refused")
	}
	if _, err := os.Stat(filepath.Join(parent, "cloud-ab12")); !os.IsNotExist(err) {
		t.Errorf("the refused add left a worktree behind: %v", err)
	}
}
