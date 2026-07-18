package app

import (
	"fmt"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
)

// setArchived archives or restores an agent. Archiving sets the manifest flag
// and kills the tmux session (the agent stops) but keeps the worktree, branch,
// pointer, and task docs — so it stays listable and resumes with its
// conversation intact. Restoring just clears the flag; `open` then wakes it.
func setArchived(id string, archived bool) (string, error) {
	name, _, err := resolveSession(id)
	if err != nil {
		return "", err
	}
	m, err := session.ReadByTmux(name)
	if err != nil {
		return "", fmt.Errorf("read agent: %w", err)
	}
	m.Archived = archived
	if err := m.Write(); err != nil {
		return "", err
	}
	if archived {
		global, err := config.LoadGlobal()
		if err != nil {
			return "", err
		}
		if run, err := newRunner(global.Runner); err == nil {
			_ = run.Kill(name) // best-effort; the worktree and manifest are what matter
		}
		return fmt.Sprintf("Archived %s — worktree kept; open it to resume.", id), nil
	}
	return fmt.Sprintf("Restored %s.", id), nil
}
