package app

import (
	"fmt"

	"github.com/boratanrikulu/kovan/internal/session"
)

// setPinned pins or unpins an agent on the board. Pure metadata: the manifest
// flag keeps the agent's workspace at the top of the list, nothing about the
// session or worktree changes.
func setPinned(id string, pinned bool) (string, error) {
	name, _, err := resolveSession(id)
	if err != nil {
		return "", err
	}
	if err := session.Update(name, func(m *session.Manifest) error {
		m.Pinned = pinned
		return nil
	}); err != nil {
		return "", fmt.Errorf("pin agent: %w", err)
	}
	if pinned {
		return fmt.Sprintf("Pinned %s.", id), nil
	}
	return fmt.Sprintf("Unpinned %s.", id), nil
}
