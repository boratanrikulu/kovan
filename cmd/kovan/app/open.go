package app

import (
	"fmt"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:     "open <id>",
	Aliases: []string{"attach"},
	Short:   "Open an agent's session, waking it if stopped",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOpen(args[0])
	},
}

func runOpen(id string) error {
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	run, err := newRunner(global.Runner)
	if err != nil {
		return err
	}
	name, woke, err := openTarget(id, run)
	if err != nil {
		return err
	}
	if woke {
		fmt.Printf("waking agent %s…\n", id)
	}
	return run.Attach(name)
}

// openTarget is the shared core behind both `kovan open` and the TUI: it
// resolves the agent, wakes it if its session has stopped, and returns the
// tmux name to hand the terminal to. woke reports whether a stopped agent was
// relaunched.
func openTarget(id string, run runner.Runner) (name string, woke bool, err error) {
	name, _, err = resolveSession(id)
	if err != nil {
		return "", false, err
	}
	alive, err := run.Exists(name)
	if err != nil {
		return "", false, err
	}
	m, readErr := session.ReadByTmux(name)

	// Opening an archived agent restores it: clear the flag so it returns to the
	// board, then take the normal wake path (its worktree was kept).
	if readErr == nil && m.Archived {
		m.Archived = false
		_ = m.Write()
	}

	switch decideOpen(alive, readErr == nil) {
	case actionMissing:
		return "", false, fmt.Errorf("worktree missing; run `kovan remove %s`", id)
	case actionWake:
		sess, err := runnerSession(m, launchResume, "")
		if err != nil {
			return "", false, err
		}
		if err := run.Start(sess); err != nil {
			return "", false, err
		}
		woke = true
	}
	return name, woke, nil
}
