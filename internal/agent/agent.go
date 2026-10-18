// Package agent holds what kovan knows about each agent CLI it can launch: the
// flags, the hook payload and decision format, where transcripts live and how
// to read them. Everything else in kovan works on the CLI-neutral shapes here,
// so a new CLI is one more driver, not a change across the app.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Names of the built-in drivers.
const (
	Claude = "claude"
	Codex  = "codex"
)

// Launch is one interactive start of an agent.
type Launch struct {
	Bin       string   // the binary, a name on PATH or a path
	Prompt    string   // the opening user message; unused on resume
	Dirs      []string // extra dirs the agent may read and write
	SessionID string   // the conversation to create (fresh) or reopen (resume)
	Resume    bool
	Method    string // the composed method file, loaded as part of the system prompt; "" for none
}

// ToolKind sorts tool calls into what the gates care about.
type ToolKind int

const (
	ToolOther ToolKind = iota
	ToolShell          // runs a shell command
	ToolEdit           // writes files directly
)

// Tool is a pending or finished tool call.
type Tool struct {
	Name    string
	Kind    ToolKind
	Command string   // the shell command, for ToolShell
	Paths   []string // the files written, for ToolEdit
}

// Event is a hook event in kovan's terms. Names follow Claude Code's events
// (PreToolUse, PostToolUse, UserPromptSubmit, Notification, Stop,
// SessionStart); a driver maps its CLI's events onto them.
type Event struct {
	Name             string
	SessionID        string
	Cwd              string
	PermissionMode   string
	NotificationType string
	Effort           string
	Prompt           string // the submitted prompt, on UserPromptSubmit
	SubAgent         bool   // sent by a sub-agent the agent spawned, not by the user
	Tool             Tool
}

// Driver is one agent CLI.
type Driver interface {
	Name() string
	// Command is the shell command that starts the agent.
	Command(l Launch) string
	// OneShot is the shell command that answers instruction once and exits,
	// with more input on stdin.
	OneShot(bin, model, instruction, method string) string
	// PresetID reports whether kovan can choose the session id up front. When
	// it cannot, the id is learned from the first hook event.
	PresetID() bool
	// TokenEnv is the env var that carries an account's token, or "".
	TokenEnv() string
	// HomeEnv is the env var that moves the CLI's home dir, or "".
	HomeEnv() string
	// DefaultHome is the CLI's home dir when no account moves it.
	DefaultHome() string
	// HooksFile is the file under home that holds the CLI's hooks.
	HooksFile(home string) string
	// HookEvents are the events kovan installs its hook for.
	HookEvents() []string
	// Hook decodes a hook payload.
	Hook(raw []byte) (Event, error)
	// CanAsk reports whether a hook can hand a decision to the user. A CLI
	// that cannot only allows or denies.
	CanAsk() bool
	// Decision encodes a gate decision ("ask" or "deny") for a PreToolUse hook.
	Decision(action, reason string) ([]byte, error)
	// TranscriptGlob matches the transcript of session id under home.
	TranscriptGlob(home, id string) string
	// Digest renders a transcript tail as short labeled lines.
	Digest(tail []byte) string
	// Perm is the latest permission mode recorded in a transcript tail.
	Perm(tail []byte) (string, bool)
	// EncodeMethod is the method file's content for the composed method text.
	EncodeMethod(text string) []byte
	// GlobalMemory is the file the CLI loads for every session, or "".
	GlobalMemory(home string) string
	// SkillsDir is where the CLI looks for skills, relative to a worktree.
	SkillsDir() string
	// GlobalSkillsDir is where the CLI looks for skills in every session.
	GlobalSkillsDir() string
}

var drivers = map[string]Driver{
	Claude: claude{},
	Codex:  codex{},
}

// For returns the driver named name. A path or a binary name works too: the
// driver is picked by its base name, so "/opt/bin/claude" is the claude driver.
func For(name string) (Driver, error) {
	if name == "" {
		name = Claude
	}
	if d, ok := drivers[filepath.Base(name)]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(Names(), ", "))
}

// Names lists the known drivers, sorted.
func Names() []string {
	out := make([]string, 0, len(drivers))
	for n := range drivers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Quote single-quotes s for a shell command line.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// catExpr is the shell expression that reads file when the command runs, so
// only the path sits in the command line.
func catExpr(file string) string {
	return `"$(cat ` + Quote(file) + `)"`
}

func addDirs(flag string, dirs []string) string {
	s := ""
	for _, d := range dirs {
		s += " " + flag + " " + Quote(d)
	}
	return s
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
