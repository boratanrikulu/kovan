package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/boratanrikulu/kovan/internal/session"
)

// sessionEnv carries a tab's tmux session name into the agent's environment, so
// a kovan command the agent runs knows which tab it came from.
const sessionEnv = "KOVAN_SESSION"

// actingAgent resolves the tab a command acts for: KOVAN_SESSION, else the tmux
// session of the calling pane. id names an agent explicitly, for a command run
// from a shell outside any agent; inside one, it must name the calling agent,
// so a mistyped id never acts on another agent's worktrees.
func actingAgent(id string) (*session.Manifest, error) {
	caller := os.Getenv(sessionEnv)
	if caller == "" {
		caller = paneSession()
	}
	if id != "" {
		if m, err := session.ReadByTmux(caller); caller != "" && err == nil && m.ID == id {
			return m, nil
		}
		m, err := findSession(id)
		if err != nil {
			return nil, err
		}
		if caller != "" && caller != m.Tmux {
			if _, err := session.ReadByTmux(caller); err == nil {
				return nil, fmt.Errorf("this is agent session %s; --agent %s names another agent", caller, id)
			}
		}
		return m, nil
	}
	if caller == "" {
		return nil, fmt.Errorf("not inside a kovan agent; pass --agent <id>")
	}
	m, err := session.ReadByTmux(caller)
	if err != nil {
		return nil, fmt.Errorf("no kovan agent for session %q: %w", caller, err)
	}
	return m, nil
}

// paneSession names the tmux session holding the calling pane, or "" outside
// tmux. TMUX_PANE pins the pane; without -t, tmux would answer for whichever
// client it picks.
func paneSession() string {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
