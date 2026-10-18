package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/notify"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/spf13/cobra"
)

var gateRunAgent string

var gateRunCmd = &cobra.Command{
	Use:    "run",
	Short:  "Dispatch an agent CLI's hook event onto the agent's manifest",
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
		if err := runGate(gateRunAgent, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "kovan gate run:", err)
		}
		return nil
	},
}

func init() {
	gateRunCmd.Flags().StringVar(&gateRunAgent, "agent", agent.Claude, "the agent CLI whose hook payload arrives on stdin")
}

// eventState maps a hook event to the agent state it implies. Notifications
// carry a type: only the kinds that block on the human mean needs-you, the
// idle nudge means idle, and informational kinds carry no state at all — a
// false orange erodes trust in the board more than a missed one.
func eventState(ev agent.Event) (string, bool) {
	switch ev.Name {
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

func runGate(agentName string, r io.Reader, w io.Writer) error {
	// The monitor summarizer runs a helper agent inside an agent's worktree for
	// context; it sets KOVAN_MONITOR so its hook events (which would otherwise
	// resolve to that agent's manifest via cwd, e.g. Stop → idle) are no-ops.
	if os.Getenv("KOVAN_MONITOR") != "" {
		return nil
	}
	d, err := driverOf(agentName)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read hook event: %w", err)
	}
	ev, err := d.Hook(raw)
	if err != nil {
		return err
	}

	found, byTab, err := resolveAgent(ev)
	if err != nil {
		return err
	}
	if found == nil {
		return nil // non-kovan session: leave it untouched, gates included
	}
	// The read below and every write after it happen under the index lock:
	// the CLI fires hooks for parallel tool calls in parallel processes, and
	// each would otherwise write over another's change.
	unlock, err := session.Lock()
	if err != nil {
		return err
	}
	notify, err := gateLocked(d, ev, found.Tmux, byTab, w)
	unlock()
	if notify != "" {
		fireNotify(found, notify) // after the unlock: osascript can take a while
	}
	return err
}

// gateLocked is the part of a hook that reads and writes the manifest. It
// returns the state to notify about, "" for none.
func gateLocked(d agent.Driver, ev agent.Event, tmux string, byTab bool, w io.Writer) (notify string, err error) {
	m, err := session.ReadByTmux(tmux)
	if err != nil {
		return "", err
	}
	// Only the tab's own CLI, in the tab's own session, reports its session id:
	// a nested CLI, or one run by hand in the worktree, is someone else's.
	learn := byTab && !d.PresetID() && mustDriver(m.Agent).Name() == d.Name()
	// Enrichment is best-effort; its failure must not suppress gate evaluation.
	notify, err = enrich(m, ev, learn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kovan gate run: enrich:", err)
	}
	if ev.Name == "PreToolUse" {
		return notify, emitGateDecision(d, ev, m, w)
	}
	return notify, nil
}

// resolveAgent finds the tab this hook event belongs to. The session id comes
// first, so the right tab is found even when several share a worktree. Then
// KOVAN_SESSION, which kovan sets in every session it launches and the hook
// inherits: it names the tab before a CLI that picks its own ids has reported
// one. The event's cwd is the fallback when neither names a tab. byTab
// reports that KOVAN_SESSION found it; a nil manifest means no kovan agent.
func resolveAgent(ev agent.Event) (m *session.Manifest, byTab bool, err error) {
	if m, found, err := session.FindBySessionID(ev.SessionID); err != nil || found {
		return m, false, err
	}
	if name := os.Getenv(sessionEnv); name != "" {
		if m, err := session.ReadByTmux(name); err == nil {
			return m, true, nil
		}
	}
	m, found, err := session.FindFrom(eventDir(ev))
	if !found {
		m = nil
	}
	return m, false, err
}

func eventDir(ev agent.Event) string {
	if ev.Cwd != "" {
		return ev.Cwd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// enrich updates the agent's manifest from an event and returns the state to
// notify about on a transition, "" for none. With learn, the event's session
// id is recorded for an agent that has none yet, so wake resumes that
// conversation.
func enrich(m *session.Manifest, ev agent.Event, learn bool) (notify string, err error) {
	changed := false
	if learn && m.SessionID == "" && ev.SessionID != "" {
		m.SessionID, changed = ev.SessionID, true
	}
	if trackApproval(m, ev) {
		changed = true
	}
	state, ok := eventState(ev)
	if !ok {
		if changed {
			return "", m.Write()
		}
		return "", nil
	}
	prev := m.State
	m.State = state
	if ev.PermissionMode != "" {
		m.Mode = ev.PermissionMode
	}
	if ev.Effort != "" {
		m.Effort = ev.Effort
	}
	m.LastActivity = time.Now()
	if err := m.Write(); err != nil {
		return "", err
	}
	if shouldNotify(prev, state) {
		return state, nil
	}
	return "", nil
}

// emitGateDecision evaluates the built-in gates and writes a decision when one
// matches; no match emits nothing, deferring to normal permission flow. The
// agent's posture and write paths are resolved live from its mode at every
// check (edit tools only; the shell gates don't use them), so editing a
// mode's mode.yaml reaches every running session at its next tool call.
func emitGateDecision(d agent.Driver, ev agent.Event, m *session.Manifest, w io.Writer) error {
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	var results []gateResult
	paths := ev.Tool.Paths
	if ev.Tool.Kind == agent.ToolShell {
		results = append(results, evalGates(gateInput{kind: agent.ToolShell, command: ev.Tool.Command, branch: m.Branch}, global.Gates))
		paths = shellPatchPaths(ev.Tool.Command, m.Worktree)
	}
	for _, p := range paths {
		results = append(results, editGate(p, m, global.Gates))
	}
	res := strongest(results)
	if res.reason == "" {
		return nil
	}
	if res.action == "ask" && !d.CanAsk() {
		key := passKey(ev.Tool)
		if m.Gate != nil && m.Gate.Approved && m.Gate.Key == key {
			m.Gate = nil // the user's approval, spent
			return m.Write()
		}
		m.Gate = &session.GatePass{Key: key}
		if err := m.Write(); err != nil {
			return err
		}
		res.reason += fmt.Sprintf(": `%s` needs your approval. Reply with %s to allow it once.", gateKey(ev.Tool), allowWord)
	}
	out, err := d.Decision(res.action, res.reason)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// shellPatchPaths is what a shell command writes through apply_patch, which
// codex also runs as a shell command and applies itself: any file in the
// worktree, written as "<worktree>/*" (the root itself is no file). Codex can
// apply it in a workdir the hook does not report, so its paths can't be
// judged: in a scoped mode a shell patch is asked.
func shellPatchPaths(command, worktree string) []string {
	for _, seg := range splitSegments(command) {
		if t := effectiveTokens(seg); len(t) > 0 && (t[0] == "apply_patch" || t[0] == "applypatch") {
			return []string{filepath.Join(worktree, "*")}
		}
	}
	return nil
}

// allowWord is what the user writes to let a gated command through once,
// for a CLI whose hooks cannot ask. A word nobody types by accident, unlike
// "go".
const allowWord = "kovan-allow"

// trackApproval keeps the pass for a CLI that cannot ask, reporting whether
// it changed. Each message from the user sets whether the pending command is
// approved: one with the allow word approves it, any other takes an unused
// approval back (the user moved on, or interrupted the turn it was for). Only
// the tab's own conversation counts; a sub-agent's prompt, or one from a CLI
// the agent runs inside its tab, is the agent's. The end of a turn ends an
// unused approval, and a new session starts with nothing pending.
func trackApproval(m *session.Manifest, ev agent.Event) bool {
	if m.Gate == nil {
		return false
	}
	switch ev.Name {
	case "UserPromptSubmit":
		if ev.SubAgent || ev.SessionID == "" || ev.SessionID != m.SessionID {
			return false
		}
		approved := strings.Contains(strings.ToLower(ev.Prompt), allowWord)
		changed := approved != m.Gate.Approved
		m.Gate.Approved = approved
		return changed
	case "Stop":
		if m.Gate.Approved {
			m.Gate = nil
			return true
		}
	case "SessionStart":
		m.Gate = nil
		return true
	}
	return false
}

// passKey identifies a gated tool call in the manifest by a digest of
// gateKey: the command can carry a token, and the manifest is read by other
// agents and backed up.
func passKey(t agent.Tool) string {
	sum := sha256.Sum256([]byte(gateKey(t)))
	return hex.EncodeToString(sum[:])
}

// gateKey names a gated tool call: its command, or the files it writes.
func gateKey(t agent.Tool) string {
	if t.Kind == agent.ToolEdit {
		return strings.Join(t.Paths, ", ")
	}
	return strings.TrimSpace(t.Command)
}

// editGate checks one written file. The posture gates scope to the workspace
// holding it, so an agent with worktrees in several repos is held to its mode
// in each, with each repo's own write_paths.
func editGate(path string, m *session.Manifest, gates config.Gates) gateResult {
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.Worktree, path) // the agent's launch dir
	}
	worktree, repo := m.Worktree, m.Repo
	if w, ok := m.WorkspaceFor(filepath.Clean(path)); ok {
		worktree, repo = w.Worktree, w.Repo
	}
	readOnly, writePaths := modeScope(m.TaskMode, repo)
	return evalGates(gateInput{
		kind:       agent.ToolEdit,
		filePath:   path,
		readOnly:   readOnly,
		worktree:   worktree,
		writePaths: writePaths,
	}, gates)
}

// modeScope resolves the agent's posture and write paths from the live
// sources: the mode's mode.yaml, plus repoName's config write_paths as
// extra allowances. The repo paths only extend a mode that is already scoped
// (read-only or path-scoped) — they never turn an unscoped edit mode into a
// gated one. Fail-open: an unresolvable mode imposes no restrictions.
func modeScope(taskMode, repoName string) (readOnly bool, writePaths []string) {
	home, err := config.Dir()
	if err != nil {
		return false, nil
	}
	md, err := mode.Load(home, taskMode)
	if err != nil {
		return false, nil
	}
	readOnly = md.ReadOnly()
	writePaths = md.WritePaths
	if (readOnly || len(writePaths) > 0) && repoName != "" {
		if repo, err := config.LoadRepo(home, repoName); err == nil {
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
