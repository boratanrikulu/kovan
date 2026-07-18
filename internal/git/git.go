// Package git is a thin wrapper that shells out to the git binary. kovan does
// not pull in a git library; everything goes through `git -C <dir>`.
package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Repo is a handle on a git repository, identified by its top-level directory.
type Repo struct {
	Root string
}

// Open resolves the repository that contains dir and returns a handle on its
// top level.
func Open(dir string) (*Repo, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository (%s): %w", dir, err)
	}
	return &Repo{Root: strings.TrimSpace(out)}, nil
}

// DefaultBase reports the repository's default branch, preferring the symbolic
// ref origin/HEAD and falling back to a local main or master.
func (r *Repo) DefaultBase() (string, error) {
	if out, err := r.git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(out)
		return strings.TrimPrefix(ref, "origin/"), nil
	}
	for _, b := range []string{"main", "master"} {
		if r.BranchExists(b) {
			return b, nil
		}
	}
	return "", fmt.Errorf("cannot determine default base branch; set worktree.base in .kovan.yaml")
}

// BranchExists reports whether branch exists locally.
func (r *Repo) BranchExists(branch string) bool {
	_, err := r.git("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// FetchBase brings the base branch up to date from origin and returns the ref a
// new worktree should fork from, so a workspace starts on the latest pushed tip
// rather than a stale local ref. With an origin remote it fetches origin/<base>
// and returns "origin/<base>"; it falls back to the bare base (the local ref)
// when there is no origin, when origin lacks the branch, or when the local branch
// has commits origin does not, so the fork never silently drops unpushed work. A
// fetch failure is returned so the caller can warn and fall back to the local base.
func (r *Repo) FetchBase(base string) (string, error) {
	if _, err := r.git("remote", "get-url", "origin"); err != nil {
		return base, nil // no origin remote: nothing to update
	}
	bare := strings.TrimPrefix(base, "origin/")
	if _, err := r.git("fetch", "origin", bare); err != nil {
		return base, fmt.Errorf("fetch origin %s: %w", bare, err)
	}
	if !r.RemoteBranchExists(bare) {
		return base, nil
	}
	remoteRef := "origin/" + bare
	// Only fork off the remote tip when the local branch (if any) is an ancestor
	// of it; a local branch that is ahead or has diverged keeps its commits.
	if r.BranchExists(bare) {
		if _, err := r.git("merge-base", "--is-ancestor", bare, remoteRef); err != nil {
			return base, nil
		}
	}
	return remoteRef, nil
}

// Branches lists candidate base branches for a new worktree: local branches
// plus origin-tracking branches with no local counterpart (as origin/<name>),
// sorted. Errors yield an empty list so the picker simply offers no branches.
func (r *Repo) Branches() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if local, err := r.git("for-each-ref", "--format=%(refname:short)", "refs/heads"); err == nil {
		for _, b := range strings.Fields(local) {
			add(b)
		}
	}
	if remote, err := r.git("for-each-ref", "--format=%(refname:short)", "refs/remotes/origin"); err == nil {
		for _, b := range strings.Fields(remote) {
			if strings.HasSuffix(b, "/HEAD") {
				continue
			}
			if short := strings.TrimPrefix(b, "origin/"); !seen[short] {
				add(b) // origin/<name> with no local namesake
			}
		}
	}
	sort.Strings(out)
	return out
}

// RemoteBranchExists reports whether branch exists on origin.
func (r *Repo) RemoteBranchExists(branch string) bool {
	_, err := r.git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// WorktreeAdd creates a worktree at path. When create is true it makes a new
// branch off base; otherwise it checks out the existing branch.
func (r *Repo) WorktreeAdd(path, branch, base string, create bool) error {
	var err error
	if create {
		// branch.autoSetupMerge=false: forking off origin/<base> must not make the
		// new branch track the base, or a bare `git push` would target the base.
		_, err = r.git("-c", "branch.autoSetupMerge=false", "worktree", "add", "-b", branch, path, base)
	} else {
		_, err = r.git("worktree", "add", path, branch)
	}
	if err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// Checkout switches the repository's main checkout to branch. Used by in-place
// agents, which work directly in the repo on an existing branch instead of in a
// separate worktree.
func (r *Repo) Checkout(branch string) error {
	if _, err := r.git("checkout", branch); err != nil {
		return fmt.Errorf("git checkout %s: %w", branch, err)
	}
	return nil
}

// WorktreeRemove removes the worktree at path.
func (r *Repo) WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if _, err := r.git(args...); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// Worktree is one entry from `git worktree list`.
type Worktree struct {
	Path   string
	Branch string
	Head   string
}

// WorktreeList parses `git worktree list --porcelain`.
func (r *Repo) WorktreeList() ([]Worktree, error) {
	out, err := r.git("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktrees(out), nil
}

// parseWorktrees turns the output of `git worktree list --porcelain` into
// structs. Entries are separated by a blank line.
func parseWorktrees(out string) []Worktree {
	var (
		list []Worktree
		cur  Worktree
	)
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "":
			if cur.Path != "" {
				list = append(list, cur)
			}
			cur = Worktree{}
		}
	}
	if cur.Path != "" {
		list = append(list, cur)
	}
	return list
}

// MainWorktree returns the path of the repository's main worktree (the first
// entry in `git worktree list`), used to share build caches across worktrees.
func (r *Repo) MainWorktree() (string, error) {
	list, err := r.WorktreeList()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return r.Root, nil
	}
	return list[0].Path, nil
}

// Dirty reports whether the worktree at dir has uncommitted changes to tracked
// files (staged or unstaged). Untracked and ignored files do not count: in an
// agent worktree those are scaffolding, not work to protect.
func (r *Repo) Dirty(dir string) (bool, error) {
	out, err := run(dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return dirtyFromStatus(out), nil
}

// dirtyFromStatus reports whether `git status --porcelain` output shows any
// tracked changes. Untracked ("??") and ignored ("!!") entries are excluded.
func dirtyFromStatus(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		if code := line[:2]; code != "??" && code != "!!" {
			return true
		}
	}
	return false
}

// CurrentBranch returns the checked-out branch of the worktree at dir.
func (r *Repo) CurrentBranch(dir string) (string, error) {
	out, err := run(dir, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("git branch --show-current: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// UserName returns the configured git user.name, or "" if unset.
func (r *Repo) UserName() string {
	out, err := r.git("config", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// AddExclude appends pattern to the repository's local exclude file
// ($GIT_COMMON_DIR/info/exclude), idempotently. This ignores generated files
// (the session manifest) across all worktrees without touching tracked
// .gitignore.
func (r *Repo) AddExclude(pattern string) error {
	out, err := r.git("rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("locate git dir: %w", err)
	}
	common := strings.TrimSpace(out)
	if !filepath.IsAbs(common) {
		common = filepath.Join(r.Root, common)
	}
	info := filepath.Join(common, "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", info, err)
	}
	path := filepath.Join(info, "exclude")

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == pattern {
				return nil
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, pattern); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// git runs a git subcommand against this repository's root.
func (r *Repo) git(args ...string) (string, error) {
	return run(r.Root, args...)
}

// run executes git in dir and returns combined stdout, with stderr folded into
// the returned error on failure.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}
	return stdout.String(), nil
}
