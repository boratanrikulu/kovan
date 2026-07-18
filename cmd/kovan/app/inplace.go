package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/session"
)

// cleanupInPlace reverses the mutations an in-place agent made to the repo
// checkout, leaving it as the human had it (on the branch the agent used; the
// switch is deliberately not reverted). It unlinks the scoped-skill symlinks,
// strips kovan's CLAUDE.local.md block, and removes the tab's index manifest.
// Each step is attempted even if an earlier one fails; the first error is returned.
//
// The .git/info/exclude patterns kovan added are intentionally left: they live
// in the shared common git dir, a sibling worktree agent may rely on them, and
// they are harmless (they only keep kovan/Claude-local paths out of git status).
func cleanupInPlace(worktree, tmux, account, domain, repoName string) error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if _, err := unlinkWorktreeSkills(worktree, account, domain, repoName); err != nil {
		note(err)
	}
	note(clearClaudeLocal(worktree))

	if err := session.RemovePointer(tmux); err != nil {
		note(fmt.Errorf("remove manifest: %w", err))
	}
	// Drop the worktree's .kovan dir if it is now empty (in-place never installed
	// git hooks, and the manifest lives in the index, not here).
	kovanDir := filepath.Join(worktree, ".kovan")
	if entries, err := os.ReadDir(kovanDir); err == nil && len(entries) == 0 {
		_ = os.Remove(kovanDir)
	}
	return firstErr
}
