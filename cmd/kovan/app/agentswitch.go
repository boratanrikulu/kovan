package app

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
)

// A tab can move to another agent CLI mid-task. The conversation cannot come
// along (no CLI reads another's transcript), but everything else does: the
// worktree and its uncommitted changes, the branch, the task docs and the
// method, which every CLI gets the same way. What the conversation held is
// bridged by handoff.md, written from the old conversation before the switch.

const handoffFile = "handoff.md"

// handoffInstruction asks the old agent's CLI to summarize its conversation for
// whoever picks the task up next.
const handoffInstruction = "You will receive a digest of a coding agent's conversation: 'you:' lines " +
	"are what the human sent, 'agent:' lines the agent's replies, 'agent ran' lines its tool calls. " +
	"Another agent takes over this task next and cannot see that conversation. Write the handoff note " +
	"it needs, in Markdown, under four headings: Done, Decided, Open, Next. Be concrete: name files, " +
	"commands and decisions; keep only what the next agent cannot rebuild from the code and the task " +
	"docs. Start at the first heading, with no title of your own. Output only the note."

// switchAgent moves m to agent CLI to under account (the caller's explicit
// pick, "" being the CLI's own login) and starts it there. A running session
// is stopped first. The new CLI resumes its own conversation when this tab ran
// under it before, else starts fresh on the takeover prompt. A failed launch
// leaves the tab on its old CLI. The warning is set when the new CLI's hooks
// are missing.
func switchAgent(m *session.Manifest, global *config.Global, run runner.Runner, to, account string) (warning string, err error) {
	d, err := checkSwitch(global, to, account)
	if err != nil {
		return "", err
	}
	next := *m
	next.Sessions = maps.Clone(m.Sessions)
	swapSession(&next, d.Name())
	next.Agent, next.Account, next.State = to, account, "working"
	next.Gate = nil
	launch, prompt := launchResume, ""
	if next.SessionID == "" || !manifestConvo(&next).exists() {
		freshSession(&next, d)
		notes, _ := taskDocsFor(&next)
		launch, prompt = launchFresh, takeoverPrompt(&next, notes)
	}
	if err := writeHandoff(m, global, to); err != nil {
		fmt.Fprintf(os.Stderr, "kovan: handoff: %v\n", err)
	}
	sess, err := runnerSession(&next, launch, prompt)
	if err != nil {
		return "", err
	}
	if alive, err := run.Exists(m.Tmux); err != nil {
		return "", err
	} else if alive {
		if err := run.Kill(m.Tmux); err != nil {
			return "", fmt.Errorf("stop %s: %w", m.ID, err)
		}
	}
	// The new CLI's first hook reads the manifest, so it must already name that
	// CLI; a failed start puts the old one back. Both writes take the lock, so
	// a hook of the dying old session cannot land after them.
	if err := writeLocked(&next); err != nil {
		return "", err
	}
	if err := run.Start(sess); err != nil {
		if werr := writeLocked(m); werr != nil {
			return "", fmt.Errorf("%w (and restoring the manifest: %v)", err, werr)
		}
		return "", err
	}
	*m = next
	return hooksWarning(global, d, account), nil
}

// writeLocked writes m whole under the index lock.
func writeLocked(m *session.Manifest) error {
	unlock, err := session.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	return m.Write()
}

// checkSwitch refuses a switch that cannot work: an unknown or uninstalled
// CLI, or an account that is not that CLI's or has a bad token file.
func checkSwitch(global *config.Global, to, account string) (agent.Driver, error) {
	d, err := agent.For(to)
	if err != nil {
		return nil, err
	}
	if !installed(global, to) {
		return nil, fmt.Errorf("%s is not installed (not on PATH)", agentBin(global, to))
	}
	if acct, ok := global.Accounts[account]; ok && acctAgent(acct) != d.Name() {
		return nil, fmt.Errorf("account %q is a %s account; this agent runs %s", account, acctAgent(acct), d.Name())
	}
	if _, err := accountTokenFile(global, account); err != nil {
		return nil, err
	}
	return d, nil
}

// freshSession sets the id a fresh launch runs under: a new one when the CLI
// takes it up front, none when the CLI picks its own (the first hook reports
// it, and a stale id left here would never be replaced).
func freshSession(m *session.Manifest, d agent.Driver) {
	m.Gate = nil // a pass belongs to the conversation that was asked
	if !d.PresetID() {
		m.SessionID = ""
		return
	}
	if id, err := session.NewID(); err == nil {
		m.SessionID = id
	}
}

// swapSession parks the current CLI's session id and picks up the one this tab
// had under to, if any.
func swapSession(m *session.Manifest, to string) {
	from := mustDriver(m.Agent).Name()
	if m.Sessions == nil {
		m.Sessions = map[string]string{}
	}
	if m.SessionID != "" {
		m.Sessions[from] = m.SessionID
	}
	m.SessionID = m.Sessions[to]
	delete(m.Sessions, to)
	if len(m.Sessions) == 0 {
		m.Sessions = nil
	}
}

// writeHandoff writes handoff.md into m's task docs from its conversation so
// far. A failed summary leaves the raw digest instead, so a switch never waits
// on it.
func writeHandoff(m *session.Manifest, global *config.Global, to string) error {
	notes, err := taskDocsFor(m)
	if err != nil {
		return err
	}
	from := mustDriver(m.Agent).Name()
	body := "No conversation was recorded under " + from + "."
	if digest := manifestConvo(m).digest(); digest != "" {
		input := fmt.Sprintf("Agent task: %s (mode: %s)\n\nConversation:\n%s", m.Title, orDash(m.TaskMode), digest)
		if text, err := runOneShot(global, m.Agent, m.Account, m.Tmux, m.Worktree, handoffInstruction, input); err == nil && text != "" {
			body = text
		} else {
			body = "The summary failed; the last turns of the conversation, as recorded:\n\n```\n" + digest + "\n```"
		}
	}
	content := fmt.Sprintf("# Handoff: %s\n\nWritten %s, when this task moved from %s to %s.\n\n%s\n",
		m.ID, time.Now().Format("2006-01-02 15:04"), from, mustDriver(to).Name(), body)
	if err := os.MkdirAll(notes, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(notes, handoffFile), []byte(content), 0o644)
}

// takeoverPrompt opens a fresh session for an agent picking up a task another
// agent started: the code and the notes carry the work, the handoff carries
// what the old conversation knew.
func takeoverPrompt(m *session.Manifest, notes string) string {
	p := "You are taking over task " + m.ID + " (" + m.Title + ") from another agent; its conversation does not carry over, but this worktree holds the code and its branch the commits."
	if notes != "" {
		p += " Read " + filepath.Join(notes, "context.md") + " first, then " + handoffFile + " and the rest of the docs in " + notes + "."
	}
	if m.Base != "" {
		p += " Check git log and git diff against " + m.Base + "."
	}
	return p + " Then tell me where the task stands and wait for my go before changing anything."
}
