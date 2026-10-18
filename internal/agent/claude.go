package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// claude is Claude Code.
type claude struct{}

func (claude) Name() string { return Claude }

// Command builds the claude launch. Resume targets the tab's own conversation
// by id: --continue picks the most recent conversation in the cwd, which is a
// sibling's when tabs share a worktree, so it is only the fallback for a
// manifest with no id. A "--" guards the positional prompt from the variadic
// --add-dir flag.
func (claude) Command(l Launch) string {
	cmd := l.Bin + addDirs("--add-dir", l.Dirs)
	if l.Resume {
		if l.SessionID != "" {
			cmd += " --resume " + Quote(l.SessionID)
		} else {
			cmd += " --continue"
		}
		return cmd + claudeMethod(l.Method)
	}
	if l.SessionID != "" {
		cmd += " --session-id " + Quote(l.SessionID)
	}
	return cmd + claudeMethod(l.Method) + " -- " + Quote(l.Prompt)
}

// claudeMethod appends the method to the system prompt. With the snapshot on
// (claude's default) a resumed conversation replays the prompt recorded on its
// first request and ignores the text a later launch passes, so a method edit
// would never reach it; off renders the passed text on every request.
func claudeMethod(file string) string {
	if file == "" {
		return ""
	}
	return " --append-system-prompt " + catExpr(file) + " --system-prompt-snapshot off"
}

func (claude) OneShot(bin, model, instruction, method string) string {
	cmd := bin
	if model != "" {
		cmd += " --model " + Quote(model)
	}
	if method != "" {
		cmd += " --append-system-prompt " + catExpr(method)
	}
	return cmd + " -p " + Quote(instruction)
}

func (claude) PresetID() bool   { return true }
func (claude) TokenEnv() string { return "CLAUDE_CODE_OAUTH_TOKEN" }
func (claude) HomeEnv() string  { return "" }

func (claude) DefaultHome() string { return filepath.Join(homeDir(), ".claude") }

func (claude) HooksFile(home string) string { return filepath.Join(home, "settings.json") }

func (claude) HookEvents() []string {
	return []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Notification", "Stop"}
}

// hookPayload is the subset of the Claude Code hook payload kovan reads. Codex
// sends the same shape.
type hookPayload struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	Cwd              string `json:"cwd"`
	PermissionMode   string `json:"permission_mode"`
	NotificationType string `json:"notification_type"`
	Prompt           string `json:"prompt"`
	AgentID          string `json:"agent_id"`
	ToolName         string `json:"tool_name"`
	ToolInput        struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	Effort struct {
		Level string `json:"level"`
	} `json:"effort"`
}

func decodeHook(raw []byte) (hookPayload, Event, error) {
	var p hookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, Event{}, fmt.Errorf("decode hook event: %w", err)
	}
	return p, Event{
		Name:             p.HookEventName,
		SessionID:        p.SessionID,
		Cwd:              p.Cwd,
		PermissionMode:   p.PermissionMode,
		NotificationType: p.NotificationType,
		Effort:           p.Effort.Level,
		Prompt:           p.Prompt,
		SubAgent:         p.AgentID != "",
		Tool:             Tool{Name: p.ToolName},
	}, nil
}

var claudeEditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

func (claude) Hook(raw []byte) (Event, error) {
	p, ev, err := decodeHook(raw)
	if err != nil {
		return ev, err
	}
	switch {
	case p.ToolName == "Bash":
		ev.Tool.Kind, ev.Tool.Command = ToolShell, p.ToolInput.Command
	case claudeEditTools[p.ToolName]:
		ev.Tool.Kind = ToolEdit
		if p.ToolInput.FilePath != "" {
			ev.Tool.Paths = []string{p.ToolInput.FilePath}
		}
	}
	return ev, nil
}

type hookDecision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func preToolUseDecision(action, reason string) ([]byte, error) {
	if action == "" {
		action = "ask"
	}
	var d hookDecision
	d.HookSpecificOutput.HookEventName = "PreToolUse"
	d.HookSpecificOutput.PermissionDecision = action
	d.HookSpecificOutput.PermissionDecisionReason = reason
	out, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("write decision: %w", err)
	}
	return append(out, '\n'), nil
}

func (claude) CanAsk() bool { return true }

func (claude) Decision(action, reason string) ([]byte, error) {
	return preToolUseDecision(action, reason)
}

// TranscriptGlob needs no path encoding: the id is unique, so any project dir
// holding it is the one.
func (claude) TranscriptGlob(home, id string) string {
	return filepath.Join(home, "projects", "*", id+".jsonl")
}

// The slices of a transcript line the digest reads. Message content is either
// a plain string (a typed human turn) or an array of blocks (assistant text /
// tool_use, or a user-side tool_result).
type claudeRecord struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// Digest parses transcript JSONL into labeled entries: "you:" for what the
// human actually sent, "agent:" for the agent's replies, "agent ran" for its
// tool calls. Everything else (tool results, thinking, system records) is
// skipped, as is any unparseable line: the tail may open mid-record.
func (claude) Digest(tail []byte) string {
	var d digest
	for _, line := range bytes.Split(tail, []byte("\n")) {
		var rec claudeRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		switch rec.Type {
		case "user":
			var text string
			if err := json.Unmarshal(rec.Message.Content, &text); err != nil {
				continue // array content is a tool_result, not a human turn
			}
			d.you(text)
		case "assistant":
			var blocks []claudeBlock
			if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					d.agent(b.Text)
				case "tool_use":
					d.tool(b.Name, b.Input)
				}
			}
		}
	}
	return d.String()
}

// Perm extracts the value of the last "permissionMode":"..." in tail. Claude
// records it every turn, so it tracks shift+tab toggles no hook sees. Only a
// quoted string value counts (a null or absent field is no match), and the
// scan runs from the end so the most recent turn wins.
func (claude) Perm(data []byte) (string, bool) {
	key := []byte(`"permissionMode"`)
	for i := bytes.LastIndex(data, key); i >= 0; i = bytes.LastIndex(data[:i], key) {
		rest := data[i+len(key):]
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != ':' {
			continue
		}
		j++
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != '"' {
			continue // null or non-string value
		}
		j++
		if end := bytes.IndexByte(rest[j:], '"'); end >= 0 {
			return string(rest[j : j+end]), true
		}
	}
	return "", false
}

func (claude) EncodeMethod(text string) []byte { return []byte(text) }

func (claude) GlobalMemory(home string) string { return filepath.Join(home, "CLAUDE.md") }
func (claude) SkillsDir() string               { return filepath.Join(".claude", "skills") }
func (claude) GlobalSkillsDir() string         { return filepath.Join(homeDir(), ".claude", "skills") }
