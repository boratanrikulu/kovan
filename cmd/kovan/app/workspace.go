package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/session"
)

// addWorkspace gives agent m a worktree in another repo, named by target: a
// repo name or a path. A path to an existing linked worktree is adopted as-is;
// anything else gets a fresh worktree made with that repo's own config, the
// same way `kovan start` makes the primary. branch overrides the repo's branch
// template and from its base. tracked is true when m already had exactly this
// workspace, so nothing was made.
func addWorkspace(m *session.Manifest, target, from, branch string) (ws session.Workspace, tracked bool, err error) {
	repo, top, err := openWorkspaceRepo(m, target)
	if err != nil {
		return ws, false, err
	}
	main, err := repo.MainWorktree()
	if err != nil {
		return ws, false, err
	}
	if own, err := mainOf(m.RepoRoot); err == nil && own == main {
		return ws, false, fmt.Errorf("%s is this agent's own repo; its worktree is %s", main, m.Worktree)
	}
	if w, ok := extraIn(m, main); ok {
		if (top != main && top != w.Worktree) || (branch != "" && branch != w.Branch) {
			return ws, false, fmt.Errorf("agent %s already has a %s worktree at %s on %s; `kovan worktree rm %s` first", m.ID, w.Repo, w.Worktree, w.Branch, w.Repo)
		}
		return w, true, nil
	}

	name := filepath.Base(main)
	for _, field := range []string{name, top, branch} {
		if err := checkListable(field); err != nil {
			return ws, false, err
		}
	}
	if top != main {
		ws, err = adoptWorkspace(m, name, main, top)
	} else {
		ws, err = createWorkspace(m, &git.Repo{Root: main}, name, from, branch)
	}
	if err != nil {
		return ws, false, err
	}
	if err := recordWorkspace(m, ws); err != nil {
		_ = teardownWorkspace(m, ws)
		return ws, false, err
	}
	return ws, false, nil
}

// recordWorkspace appends ws to the agent's manifest. The manifest is re-read
// first: a setup hook can run for
// minutes, and the board, the monitor and the gate hook write the same file
// meanwhile.
func recordWorkspace(m *session.Manifest, ws session.Workspace) error {
	if err := session.Update(m.Tmux, func(fresh *session.Manifest) error {
		fresh.Extra = append(fresh.Extra, ws)
		*m = *fresh
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// openWorkspaceRepo opens the repo target names and returns it with the top
// level of target's checkout. A target with a path separator, or "." / "..",
// is a path. A bare name is a repo another agent works in by that name, else a
// sibling of the agent's repo; it is never read relative to the cwd, where a
// subdir of the same name would shadow the repo.
func openWorkspaceRepo(m *session.Manifest, target string) (*git.Repo, string, error) {
	if target == "." || target == ".." || strings.ContainsRune(target, filepath.Separator) {
		repo, err := git.Open(target)
		if err != nil {
			return nil, "", err
		}
		return repo, repo.Root, nil
	}
	for _, c := range repoCandidates(m, target) {
		if repo, err := git.Open(c); err == nil {
			return repo, repo.Root, nil
		}
	}
	return nil, "", fmt.Errorf("no repo %q: give a path, or the name of a repo an agent works in or one next to %s", target, m.RepoRoot)
}

// repoCandidates lists where a repo called name may live: the main checkout of
// any agent workspace in a repo by that name, then the sibling dir.
func repoCandidates(m *session.Manifest, name string) []string {
	var out []string
	if all, err := session.List(); err == nil {
		for _, o := range all {
			for _, w := range o.Workspaces() {
				if w.Repo == name && w.RepoRoot != "" {
					out = append(out, w.RepoRoot)
				}
			}
		}
	}
	if m.RepoRoot != "" {
		out = append(out, filepath.Join(filepath.Dir(m.RepoRoot), name))
	}
	return out
}

// mainOf returns the main worktree of the repo checked out at dir, so two
// checkouts of one repo compare equal whatever their dir names.
func mainOf(dir string) (string, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return "", err
	}
	return repo.MainWorktree()
}

// extraIn returns m's extra workspace in the repo whose main worktree is main.
func extraIn(m *session.Manifest, main string) (session.Workspace, bool) {
	for _, w := range m.Extra {
		if w.RepoRoot == main {
			return w, true
		}
	}
	return session.Workspace{}, false
}

// adoptWorkspace records a worktree that already exists, refusing one another
// agent works in. Adopted worktrees stay on disk when the agent is removed.
func adoptWorkspace(m *session.Manifest, name, main, worktree string) (session.Workspace, error) {
	branch := currentBranch(worktree)
	if err := checkListable(branch); err != nil {
		return session.Workspace{}, err
	}
	if owner, _, found, _ := session.FindWorkspace(worktree); found && owner.Tmux != m.Tmux {
		return session.Workspace{}, fmt.Errorf("%s belongs to agent %s (%s)", worktree, owner.ID, owner.Repo)
	}
	return session.Workspace{Repo: name, RepoRoot: main, Worktree: worktree, Branch: branch, Adopted: true}, nil
}

// checkListable refuses a repo name, path or branch that cannot be listed as
// one line of the agent's method: a newline would start a line of its own, a
// backtick would end the code span it sits in.
func checkListable(field string) error {
	if strings.ContainsAny(field, "\n\r`") {
		return fmt.Errorf("%q has a newline or backtick; kovan cannot list it in the agent's method safely", field)
	}
	return nil
}

// createWorkspace makes the agent's worktree in repo, then runs the repo's
// setup hook and links its scoped skills.
func createWorkspace(m *session.Manifest, repo *git.Repo, name, from, branch string) (ws session.Workspace, err error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return ws, err
	}
	home, err := config.Dir()
	if err != nil {
		return ws, err
	}
	cfg, err := config.LoadRepo(home, name)
	if err != nil {
		return ws, err
	}
	if err := checkListable(worktreePath(repo.Root, cfg.Worktree.Prefix, m.ID)); err != nil {
		return ws, err
	}
	wt, err := makeWorktree(repo, cfg, global, m.ID, m.Title, from, branch, "pass that path to adopt it",
		"make one by hand and adopt it with `kovan worktree add <path>`")
	if err != nil {
		return ws, err
	}
	defer func() {
		if err != nil {
			_ = repo.WorktreeRemove(wt.path, true)
		}
	}()
	env := hookEnv{Path: wt.path, ID: m.ID, Branch: wt.branch, Base: wt.base, Main: repo.Root, Repo: name}
	if err := runRepoHook(repo.Root, "setup", env); err != nil {
		return ws, err
	}
	if _, e := linkWorktreeSkills(repo, wt.path, m.Account, cfg.Domain, name); e != nil {
		fmt.Fprintf(os.Stderr, "kovan: link skills: %v\n", e)
	}
	return session.Workspace{Repo: name, RepoRoot: repo.Root, Worktree: wt.path, Branch: wt.branch, Base: wt.base}, nil
}

// newWorktree is what makeWorktree made.
type newWorktree struct {
	path, branch, base string
	reused             bool // the branch existed, so it was checked out rather than forked
}

// makeWorktree creates the worktree every kovan agent gets in a repo:
// <prefix>-<id> next to it, on branch or else the repo's branch template filled
// with the id and title, forked off from, else the repo's base, else its
// default branch. A fresh branch forks off the latest fetched base, falling
// back to the local one when offline. When the path is taken, existsHint ends
// the error if no agent owns it and ownedHint if one does.
func makeWorktree(repo *git.Repo, cfg *config.Repo, global *config.Global, id, title, from, branch, existsHint, ownedHint string) (newWorktree, error) {
	wt := newWorktree{base: from, branch: branch}
	if wt.base == "" {
		wt.base = cfg.Worktree.Base
	}
	if wt.base == "" {
		base, err := repo.DefaultBase()
		if err != nil {
			return wt, err
		}
		wt.base = base
	}
	wt.path = worktreePath(repo.Root, cfg.Worktree.Prefix, id)
	if _, err := os.Stat(wt.path); err == nil {
		if owner, _, found, _ := session.FindWorkspace(wt.path); found {
			return wt, fmt.Errorf("%s belongs to agent %s (%s); %s", wt.path, owner.ID, owner.Repo, ownedHint)
		}
		return wt, fmt.Errorf("worktree already exists at %s (%s)", wt.path, existsHint)
	}
	if wt.branch == "" {
		wt.branch = renderBranch(cfg.Worktree.BranchTemplate, resolveAuthor(global, repo), id, slugify(title))
	}
	wt.reused = repo.BranchExists(wt.branch) || repo.RemoteBranchExists(wt.branch)
	forkRef := wt.base
	if !wt.reused {
		if ref, err := repo.FetchBase(wt.base); err != nil {
			fmt.Fprintf(os.Stderr, "kovan: fetch %s: %v (using local %s)\n", wt.base, err, wt.base)
		} else {
			forkRef = ref
		}
	}
	return wt, repo.WorktreeAdd(wt.path, wt.branch, forkRef, !wt.reused)
}

// removeWorkspace drops one of m's extra workspaces, named by repo or by a path
// inside it: torn down like a primary when kovan made it, only untracked when
// adopted. force skips the uncommitted-changes guard.
func removeWorkspace(m *session.Manifest, target string, force bool) (session.Workspace, error) {
	i := extraIndex(m, target)
	if i < 0 {
		return session.Workspace{}, fmt.Errorf("agent %s has no worktree in %q", m.ID, target)
	}
	ws := m.Extra[i]
	if !force {
		if err := guardClean(m.Tmux, ws); err != nil {
			return ws, err
		}
	}
	if err := teardownWorkspace(m, ws); err != nil {
		return ws, err
	}
	err := session.Update(m.Tmux, func(fresh *session.Manifest) error {
		var kept []session.Workspace
		for _, w := range fresh.Extra {
			if w.Worktree != ws.Worktree {
				kept = append(kept, w)
			}
		}
		fresh.Extra = kept
		*m = *fresh
		return nil
	})
	return ws, err
}

// extraIndex finds the extra workspace target names, by repo or by a path
// inside it, or -1.
func extraIndex(m *session.Manifest, target string) int {
	abs, _ := filepath.Abs(target)
	in, inside := m.WorkspaceFor(abs)
	for i, w := range m.Extra {
		if w.Repo == target || (inside && w.Worktree == in.Worktree) {
			return i
		}
	}
	return -1
}

// guardClean errors when a workspace tab owns and will remove has uncommitted
// tracked changes. Adopted ones and ones another agent works in are never
// removed, and a vanished one has nothing left to lose, so none needs the
// guard.
func guardClean(tab string, ws session.Workspace) error {
	if ws.Adopted || !exists(ws.Worktree) || sharedWorkspace(tab, ws.Worktree) {
		return nil
	}
	dirty, err := (&git.Repo{Root: ws.RepoRoot}).Dirty(ws.Worktree)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("worktree %s has uncommitted changes; commit them or rerun with --force", ws.Worktree)
	}
	return nil
}

// teardownWorkspace runs the repo's teardown hook and removes one of m's
// kovan-made extra worktrees, keeping its branch. It leaves alone a worktree
// that was adopted or that another agent also works in, and one already gone
// from disk only has git's record of it pruned.
func teardownWorkspace(m *session.Manifest, ws session.Workspace) error {
	if ws.Adopted || sharedWorkspace(m.Tmux, ws.Worktree) {
		return nil
	}
	repo := &git.Repo{Root: ws.RepoRoot}
	if !exists(ws.Worktree) {
		return repo.WorktreePrune()
	}
	env := hookEnv{Path: ws.Worktree, ID: m.ID, Branch: ws.Branch, Base: ws.Base, Main: ws.RepoRoot, Repo: ws.Repo}
	if err := runRepoHook(ws.RepoRoot, "teardown", env); err != nil {
		return err
	}
	return repo.WorktreeRemove(ws.Worktree, true)
}

// sharedWorkspace reports whether a tab other than exceptTmux has worktree as
// one of its workspaces.
func sharedWorkspace(exceptTmux, worktree string) bool {
	all, err := session.List()
	if err != nil {
		return true // unsure: keeping a worktree is the recoverable mistake
	}
	for _, o := range all {
		if o.Tmux == exceptTmux {
			continue
		}
		for _, w := range o.Workspaces() {
			if w.Worktree == worktree {
				return true
			}
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
