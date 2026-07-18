package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/notify"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/spf13/cobra"
)

var gateRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Dispatch a Claude Code hook event onto the agent's manifest",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// A hook must never block the agent: recover from any panic and log
		// internal errors instead of propagating them, so the process exits 0
		// and normal permission flow and the turn stay untouched.
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintln(os.Stderr, "kovan gate run: recovered:", r)
			}
		}()
		if err := runGate(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "kovan gate run:", err)
		}
		return nil
	},
}

// hookEvent is the subset of Claude Code's hook stdin JSON that kovan reads.
type hookEvent struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	Cwd              string `json:"cwd"`
	PermissionMode   string `json:"permission_mode"`
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
	ToolInput        struct {
		Command  string `json:"command"`   // Bash
		FilePath string `json:"file_path"` // Edit/Write
	} `json:"tool_input"`
	Effort struct {
		Level string `json:"level"`
	} `json:"effort"`
}

// eventState maps a hook event to the agent state it implies. Notifications
// carry a type: only the kinds that block on the human mean needs-you, the
// idle nudge means idle, and informational kinds carry no state at all — a
// false orange erodes trust in the board more than a missed one.
func eventState(ev hookEvent) (string, bool) {
	switch ev.HookEventName {
	case "PreToolUse", "PostToolUse", "UserPromptSubmit":
		return "working", true
	case "Notification":
		switch ev.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			return "needs-you", true
		case "idle_prompt":
			return "idle", true
		case "":
			// An older CLI without the field: every notification reads as the
			// blocking kind, the pre-classification behavior.
			return "needs-you", true
		}
		return "", false
	case "Stop":
		return "idle", true
	}
	return "", false
}

// shouldNotify fires only when the state changes into a "come back to me"
// state, so the user is not pinged on every tool call.
func shouldNotify(prev, next string) bool {
	if prev == next {
		return false
	}
	return next == "needs-you" || next == "idle"
}

func runGate(r io.Reader, w io.Writer) error {
	// The monitor summarizer launches a helper `claude` inside an agent's worktree
	// for context; it sets KOVAN_MONITOR so its hook events (which would otherwise
	// resolve to that agent's manifest via cwd, e.g. Stop → idle) are no-ops.
	if os.Getenv("KOVAN_MONITOR") != "" {
		return nil
	}
	var ev hookEvent
	if err := json.NewDecoder(r).Decode(&ev); err != nil {
		return fmt.Errorf("decode hook event: %w", err)
	}

	m, found, err := resolveAgent(ev)
	if err != nil {
		return err
	}
	if !found {
		return nil // non-kovan session: leave it untouched, gates included
	}

	// Enrichment is best-effort; its failure must not suppress gate evaluation.
	if err := enrich(m, ev); err != nil {
		fmt.Fprintln(os.Stderr, "kovan gate run: enrich:", err)
	}

	if ev.HookEventName == "PreToolUse" {
		return emitGateDecision(ev, m, w)
	}
	return nil
}

// resolveAgent finds the tab this hook event belongs to. It prefers the Claude
// session id (so the right tab is found even when several share a worktree) and
// falls back to the event's cwd for an agent whose session id is unknown.
func resolveAgent(ev hookEvent) (*session.Manifest, bool, error) {
	if m, found, err := session.FindBySessionID(ev.SessionID); err != nil || found {
		return m, found, err
	}
	return session.FindFrom(eventDir(ev))
}

func eventDir(ev hookEvent) string {
	if ev.Cwd != "" {
		return ev.Cwd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// enrich updates the agent's manifest from a state-bearing event and notifies
// on a state transition.
func enrich(m *session.Manifest, ev hookEvent) error {
	state, ok := eventState(ev)
	if !ok {
		return nil
	}
	prev := m.State
	m.State = state
	if ev.PermissionMode != "" {
		m.Mode = ev.PermissionMode
	}
	if ev.Effort.Level != "" {
		m.Effort = ev.Effort.Level
	}
	m.LastActivity = time.Now()
	if err := m.Write(); err != nil {
		return err
	}
	if shouldNotify(prev, state) {
		fireNotify(m, state)
	}
	return nil
}

// emitGateDecision evaluates the built-in gates and writes a decision when one
// matches; no match emits nothing, deferring to normal permission flow. The
// agent's posture and write paths are resolved live from its mode at every
// check (edit tools only — the Bash gates don't use them), so editing a
// mode's mode.yaml reaches every running session at its next tool call.
func emitGateDecision(ev hookEvent, m *session.Manifest, w io.Writer) error {
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	var readOnly bool
	var writePaths []string
	if editTools[ev.ToolName] {
		readOnly, writePaths = modeScope(m)
	}
	res := evalGates(gateInput{
		toolName:   ev.ToolName,
		command:    ev.ToolInput.Command,
		filePath:   ev.ToolInput.FilePath,
		readOnly:   readOnly,
		worktree:   m.Worktree,
		writePaths: writePaths,
		branch:     m.Branch,
	}, global.Gates)
	if res.reason == "" {
		return nil
	}
	return writeDecision(w, res.action, res.reason)
}

// modeScope resolves the agent's posture and write paths from the live
// sources: its mode's mode.yaml, plus the repo's .kovan.yaml write_paths as
// extra allowances. The repo paths only extend a mode that is already scoped
// (read-only or path-scoped) — they never turn an unscoped edit mode into a
// gated one. Fail-open: an unresolvable mode imposes no restrictions.
func modeScope(m *session.Manifest) (readOnly bool, writePaths []string) {
	home, err := config.Dir()
	if err != nil {
		return false, nil
	}
	md, err := mode.Load(home, m.TaskMode)
	if err != nil {
		return false, nil
	}
	readOnly = md.ReadOnly()
	writePaths = md.WritePaths
	if (readOnly || len(writePaths) > 0) && m.RepoRoot != "" {
		if repo, err := config.LoadRepo(m.RepoRoot); err == nil {
			writePaths = append(writePaths, repo.WritePaths...)
		}
	}
	return readOnly, writePaths
}

// fireNotify is best-effort: a failed notification never fails the hook.
func fireNotify(m *session.Manifest, state string) {
	global, err := config.LoadGlobal()
	if err != nil {
		return
	}
	_ = notify.For(global.Notify).Notify("kovan: "+m.ID, state+": "+m.Title)
}
