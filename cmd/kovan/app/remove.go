package app

import (
	"fmt"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/spf13/cobra"
)

var removeForce bool

var removeCmd = &cobra.Command{
	Use:     "remove <id>",
	Aliases: []string{"rm"},
	Short:   "Remove an agent: kill session, remove worktree, keep the branch",
	Long: `Run the per-repo teardown hook, kill the agent's tmux session, and remove
its worktree. The branch is kept, so committed work is preserved.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, err := removeAgent(args[0], removeForce)
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	},
}

func init() {
	removeCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "remove the worktree even with uncommitted changes")
}

// removeAgent is the shared core behind both `kovan remove` and the TUI: it
// runs the teardown hook, kills the session, and removes the worktree, keeping
// the branch. The agent's extra worktrees go the same way, except adopted ones
// and ones another agent works in, which stay on disk. It returns a one-line
// result for the caller to surface.
func removeAgent(id string, force bool) (string, error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return "", err
	}
	run, err := newRunner(global.Runner)
	if err != nil {
		return "", err
	}

	name, _, err := resolveSession(id)
	if err != nil {
		return "", err
	}

	m, err := session.ReadByTmux(name)
	if err != nil {
		// The worktree is already gone; drop the stale index entry.
		if alive, _ := run.Exists(name); alive {
			if err := run.Kill(name); err != nil {
				return "", err
			}
		}
		if err := session.RemovePointer(name); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed stale record for %s (worktree already gone).", id), nil
	}

	repo, err := git.Open(m.RepoRoot)
	if err != nil {
		return "", err
	}

	// Every extra is checked for uncommitted changes before anything is torn
	// down, so a dirty one never leaves the agent half-removed. The extras are
	// this tab's own, so they go even when sibling tabs keep the primary.
	if !force {
		for _, w := range m.Extra {
			if err := guardClean(m.Tmux, w); err != nil {
				return "", err
			}
		}
	}

	// First-in set the worktree up; last-out tears it down. While sibling tabs
	// still occupy this worktree, removing one only stops that tab and drops its
	// manifest — the worktree, branch, skills, hooks, and CLAUDE.local stay for the
	// others. The dirty guard is skipped here because nothing is being removed.
	if siblings := tabsSharingWorktree(m.Worktree, m.Tmux); siblings > 0 {
		if err := teardownExtras(m); err != nil {
			return "", err
		}
		if alive, _ := run.Exists(m.Tmux); alive {
			if err := run.Kill(m.Tmux); err != nil {
				return "", err
			}
		}
		if err := session.RemovePointer(m.Tmux); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed tab %s. Worktree kept — %d other tab(s) still use it; task docs kept.", id, siblings), nil
	}

	if !force {
		if err := guardClean(m.Tmux, m.Workspaces()[0]); err != nil {
			return "", err
		}
	}
	if err := teardownExtras(m); err != nil {
		return "", err
	}

	// This checkout (in-place, or the last guest tab that shared it): there is no
	// separate worktree to drop, so teardown kills the session and reverses the
	// checkout mutations, keeping the checkout on its branch.
	if m.Worktree == m.RepoRoot {
		if alive, _ := run.Exists(m.Tmux); alive {
			if err := run.Kill(m.Tmux); err != nil {
				return "", err
			}
		}
		home, err := config.Dir()
		if err != nil {
			return "", err
		}
		repoCfg, err := config.LoadRepo(home, m.Repo)
		if err != nil {
			return "", err
		}
		if err := cleanupInPlace(m.Worktree, m.Tmux, m.Account, repoCfg.Domain, m.Repo); err != nil {
			return "", err
		}
		if err := session.RemovePointer(m.Tmux); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed the agent in this checkout (%s). Checkout kept on branch %s; task docs kept in the kovan store.", id, m.Branch), nil
	}

	main, err := repo.MainWorktree()
	if err != nil {
		return "", err
	}
	env := hookEnv{Path: m.Worktree, ID: m.ID, Branch: m.Branch, Base: m.Base, Main: main, Repo: m.Repo}
	if err := runRepoHook(repo.Root, "teardown", env); err != nil {
		return "", err
	}

	if alive, _ := run.Exists(m.Tmux); alive {
		if err := run.Kill(m.Tmux); err != nil {
			return "", err
		}
	}

	// The task docs live in the durable kovan store, not the worktree, so
	// removing the worktree never touches them — no preserve-back step.
	// Force-remove: untracked scaffolding (the manifest, setup-hook artifacts)
	// is expected and safe to drop; tracked work was guarded above.
	if err := repo.WorktreeRemove(m.Worktree, true); err != nil {
		return "", err
	}
	if err := session.RemovePointer(m.Tmux); err != nil {
		return "", err
	}

	return fmt.Sprintf("Removed %s. Branch %s kept; task docs kept in the kovan store.", id, m.Branch), nil
}

// tabsSharingWorktree counts agents other than exceptTmux whose worktree is the
// given path — the sibling tabs that keep a worktree alive after this one leaves.
func tabsSharingWorktree(worktree, exceptTmux string) int {
	all, err := session.List()
	if err != nil {
		return 0
	}
	n := 0
	for _, m := range all {
		if m.Tmux != exceptTmux && m.Worktree == worktree {
			n++
		}
	}
	return n
}

// teardownExtras tears down every extra workspace of m (see teardownWorkspace).
// It records progress in the manifest, so a failure part-way is retried from
// where it stopped.
func teardownExtras(m *session.Manifest) error {
	for len(m.Extra) > 0 {
		if err := teardownWorkspace(m, m.Extra[0]); err != nil {
			return err
		}
		m.Extra = m.Extra[1:]
		if err := m.Write(); err != nil {
			return err
		}
	}
	return nil
}
