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
	m, err := session.ReadByTmux(name)
	if err != nil {
		return "", fmt.Errorf("read agent: %w", err)
	}
	m.Pinned = pinned
	if err := m.Write(); err != nil {
		return "", err
	}
	if pinned {
		return fmt.Sprintf("Pinned %s.", id), nil
	}
	return fmt.Sprintf("Unpinned %s.", id), nil
}
