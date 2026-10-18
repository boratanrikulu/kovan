package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// codex is OpenAI's Codex CLI.
type codex struct{}

func (codex) Name() string { return Codex }

// Command builds the codex launch. Codex cannot take a session id up front, so
// a fresh start lets it pick one and kovan learns it from the first hook. A
// resume needs that id: "resume the latest" would take whatever conversation
// ran last in the cwd, a sibling tab's or the user's own.
//
// The sandbox is workspace-write: the agent edits its worktree and the dirs
// kovan grants (its task docs, its other worktrees). Codex refuses to start
// with --add-dir under the read-only sandbox it gives a folder it has not
// been told to trust.
func (codex) Command(l Launch) string {
	cmd := l.Bin
	if l.Resume {
		cmd += " resume"
	}
	cmd += " --sandbox workspace-write" + addDirs("--add-dir", l.Dirs) + codexMethod(l.Method)
	if l.Resume {
		return cmd + " " + Quote(l.SessionID)
	}
	return cmd + " -- " + Quote(l.Prompt)
}

// codexMethod passes the method as developer instructions. -c parses its value
// as TOML, so the file holds the text as a TOML string (see EncodeMethod).
func codexMethod(file string) string {
	if file == "" {
		return ""
	}
	return " -c developer_instructions=" + catExpr(file)
}

// OneShot runs codex exec, which appends piped stdin to the prompt. It is
// ephemeral so a summary leaves no conversation of its own behind, and
// read-only: it runs with no kovan gate, and its job is only to write text.
func (codex) OneShot(bin, model, instruction, method string) string {
	cmd := bin + " exec --ephemeral --sandbox read-only"
	if model != "" {
		cmd += " -m " + Quote(model)
	}
	return cmd + codexMethod(method) + " " + Quote(instruction)
}

func (codex) PresetID() bool   { return false }
func (codex) TokenEnv() string { return "" }
func (codex) HomeEnv() string  { return "CODEX_HOME" }

func (codex) DefaultHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	return filepath.Join(homeDir(), ".codex")
}

// HooksFile is hooks.json, which takes the same shape as Claude Code's
// settings.json hooks block.
func (codex) HooksFile(home string) string { return filepath.Join(home, "hooks.json") }

// HookEvents has no Notification: codex signals a pending approval with
// PermissionRequest instead. SessionStart carries the session id before the
// first tool call.
func (codex) HookEvents() []string {
	return []string{"SessionStart", "PreToolUse", "PostToolUse", "UserPromptSubmit", "PermissionRequest", "Stop"}
}

func (codex) Hook(raw []byte) (Event, error) {
	p, ev, err := decodeHook(raw)
	if err != nil {
		return ev, err
	}
	if p.HookEventName == "PermissionRequest" {
		ev.Name, ev.NotificationType = "Notification", "permission_prompt"
		return ev, nil
	}
	switch p.ToolName {
	case "Bash":
		ev.Tool.Kind, ev.Tool.Command = ToolShell, p.ToolInput.Command
	case "apply_patch":
		ev.Tool.Kind, ev.Tool.Paths = ToolEdit, patchPaths(p.ToolInput.Command, p.Cwd)
	}
	return ev, nil
}

// patchPaths lists the files an apply_patch body writes, resolved against the
// cwd the patch runs in.
func patchPaths(patch, cwd string) []string {
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File:", "*** Update File:", "*** Delete File:", "*** Move to:"} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				if p = strings.TrimSpace(p); p != "" {
					if !filepath.IsAbs(p) && cwd != "" {
						p = filepath.Join(cwd, p)
					}
					out = append(out, filepath.Clean(p))
				}
			}
		}
	}
	return out
}

// CanAsk is false: codex treats a hook's "ask" as a failed hook and runs the
// tool anyway.
func (codex) CanAsk() bool { return false }

// Decision always denies, for the reason CanAsk gives.
func (codex) Decision(_, reason string) ([]byte, error) {
	return preToolUseDecision("deny", reason)
}

func (codex) TranscriptGlob(home, id string) string {
	return filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl")
}

// codexLine is one rollout line: {"timestamp", "type", "payload"}.
type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type      string          `json:"type"`
		Message   string          `json:"message"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		Input     string          `json:"input"`
		Action    json.RawMessage `json:"action"`
	} `json:"payload"`
}

// Digest reads the human and agent turns from event_msg lines, not from the
// response_item messages: those also carry the instructions codex injects
// (AGENTS.md, environment context), which are no turn of anyone's.
func (codex) Digest(tail []byte) string {
	var d digest
	for _, line := range bytes.Split(tail, []byte("\n")) {
		var rec codexLine
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		p := rec.Payload
		switch {
		case rec.Type == "event_msg" && p.Type == "user_message":
			d.you(p.Message)
		case rec.Type == "event_msg" && p.Type == "agent_message":
			d.agent(p.Message)
		case rec.Type == "response_item" && p.Type == "function_call":
			d.tool(p.Name, codexArgs(p.Arguments))
		case rec.Type == "response_item" && p.Type == "custom_tool_call":
			d.tool(p.Name, map[string]any{"command": p.Input})
		}
	}
	return d.String()
}

// codexArgs decodes a function call's JSON arguments, joining an argv-style
// command into one string.
func codexArgs(raw string) map[string]any {
	args := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil
	}
	if argv, ok := args["command"].([]any); ok {
		parts := make([]string, 0, len(argv))
		for _, a := range argv {
			if s, ok := a.(string); ok {
				parts = append(parts, s)
			}
		}
		args["command"] = strings.Join(parts, " ")
	}
	return args
}

// Perm is left to the hook payload's permission_mode: the rollout records an
// approval policy and a sandbox, not a mode.
func (codex) Perm([]byte) (string, bool) { return "", false }

// EncodeMethod quotes the text as a JSON string, which is also a valid TOML
// basic string once DEL is escaped: Go escapes ", \, the other control
// characters and <>& (as \uXXXX), all of which TOML reads the same way.
func (codex) EncodeMethod(text string) []byte {
	out, _ := json.Marshal(text)
	// Go leaves DEL raw, and TOML forbids it raw in a basic string.
	return bytes.ReplaceAll(out, []byte{0x7f}, []byte(`\u007f`))
}

func (codex) GlobalMemory(string) string { return "" }
func (codex) SkillsDir() string          { return filepath.Join(".agents", "skills") }
func (codex) GlobalSkillsDir() string    { return filepath.Join(homeDir(), ".agents", "skills") }
