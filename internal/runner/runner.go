// Package runner abstracts the process multiplexer that hosts each agent.
// tmux is the only implementation today; the interface keeps zellij/abduco
// swappable without touching the rest of kovan.
package runner

import "os/exec"

// Session describes a detached agent session.
type Session struct {
	Name    string   // multiplexer session name, unique across all repos
	Title   string   // human window/tab title (the agent's id + title)
	Dir     string   // working directory the agent starts in
	Cmd     string   // command line to run (e.g. claude "fix the bug")
	Env     []string // extra environment, as KEY=VALUE
	Options []string // "key value" options applied to the session, best-effort
}

// Runner hosts long-lived agent sessions in a pty multiplexer.
type Runner interface {
	// Start launches the session detached.
	Start(s Session) error
	// Attach hands the current terminal to the session, replacing this
	// process. It does not return on success.
	Attach(name string) error
	// AttachCmd returns the command that hands over to the session and whether
	// it blocks the caller. A front-end that owns the terminal (the TUI) runs
	// the blocking form via ExecProcess; the non-blocking form (switching an
	// existing multiplexer client) lets the caller keep running.
	AttachCmd(name string) (cmd *exec.Cmd, blocking bool)
	// Capture returns the last lines of the session's visible output.
	Capture(name string, lines int) (string, error)
	// Kill terminates the session.
	Kill(name string) error
	// Exists reports whether the session is currently alive.
	Exists(name string) (bool, error)
	// Sessions returns the names of every live session in one call, for
	// callers that check many agents at once (the board).
	Sessions() ([]string, error)
}
