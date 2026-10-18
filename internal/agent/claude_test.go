package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestClaudeCommand(t *testing.T) {
	// Fresh: --add-dir grants the task-doc dir, --session-id pins the transcript,
	// "--" guards the positional prompt.
	if got := (claude{}).Command(Launch{Bin: "claude", Prompt: "fix the vfs handler", Dirs: []string{"/notes"}, SessionID: "sid-1"}); got != `claude --add-dir '/notes' --session-id 'sid-1' -- 'fix the vfs handler'` {
		t.Errorf("fresh = %q", got)
	}
	// Resume reattaches the tab's own conversation by id: tabs share a cwd, so
	// --continue would grab a sibling's conversation (and no prompt).
	if got := (claude{}).Command(Launch{Bin: "claude", Prompt: "ignored on resume", Dirs: []string{"/notes"}, SessionID: "sid-1", Resume: true}); got != "claude --add-dir '/notes' --resume 'sid-1'" {
		t.Errorf("resume = %q", got)
	}
	// Resume without a recorded session id falls back to --continue.
	if got := (claude{}).Command(Launch{Bin: "claude", Prompt: "ignored on resume", Dirs: []string{"/notes"}, Resume: true}); got != "claude --add-dir '/notes' --continue" {
		t.Errorf("resume without id = %q", got)
	}
	// Every dir gets its own flag: the task docs plus the agent's other workspaces.
	if got := (claude{}).Command(Launch{Bin: "claude", Dirs: []string{"/notes", "/w/cloud-1"}, SessionID: "sid-1", Resume: true}); got != "claude --add-dir '/notes' --add-dir '/w/cloud-1' --resume 'sid-1'" {
		t.Errorf("two dirs = %q", got)
	}
	// No add-dir / no session id → just the guarded prompt.
	if got := (claude{}).Command(Launch{Bin: "claude", Prompt: "g"}); got != "claude -- 'g'" {
		t.Errorf("minimal = %q", got)
	}
}

func TestClaudeDecision(t *testing.T) {
	const reason = "kovan: confirm before push/PR"
	raw, err := (claude{}).Decision("ask", reason)
	if err != nil {
		t.Fatal(err)
	}
	var d hookDecision
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	out := d.HookSpecificOutput
	if out.HookEventName != "PreToolUse" || out.PermissionDecision != "ask" {
		t.Errorf("decision = %+v", out)
	}
	if out.PermissionDecisionReason != reason {
		t.Errorf("reason = %q", out.PermissionDecisionReason)
	}
}

func TestClaudeDigest(t *testing.T) {
	lines := []string{
		`ge","content":[{"type":"text","text":"partial first line from a mid-record tail"}]}}`,
		`{"type":"user","message":{"role":"user","content":"Read your brief and execute it"}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"I'll start by reading the brief."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...","description":"Run the suite"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok  \tpkg 0.2s"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/repo/main.go"}}]}}`,
		`{"type":"system","subtype":"turn_duration","level":null}`,
		`{"type":"last-prompt","lastPrompt":"an unsent suggestion would not even be here","sessionId":"x"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Done. Tests pass."}]}}`,
	}
	got := (claude{}).Digest([]byte(strings.Join(lines, "\n")))
	want := []string{
		"you: Read your brief and execute it",
		"agent: I'll start by reading the brief.",
		"agent ran Bash: Run the suite",
		"agent ran Read: /repo/main.go",
		"agent: Done. Tests pass.",
	}
	if got != strings.Join(want, "\n") {
		t.Errorf("digest =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if strings.Contains(got, "private") || strings.Contains(got, "tool_result") || strings.Contains(got, "unsent") {
		t.Errorf("digest leaked a skipped record:\n%s", got)
	}
}

func TestDigestCapsEntries(t *testing.T) {
	var lines []string
	for i := 0; i < DigestMaxEntries+10; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"content":"turn %d"}}`, i))
	}
	got := strings.Split((claude{}).Digest([]byte(strings.Join(lines, "\n"))), "\n")
	if len(got) != DigestMaxEntries {
		t.Fatalf("entries = %d, want %d", len(got), DigestMaxEntries)
	}
	if got[len(got)-1] != fmt.Sprintf("you: turn %d", DigestMaxEntries+9) {
		t.Errorf("cap must keep the newest entries, last = %q", got[len(got)-1])
	}
}

func TestClaudePerm(t *testing.T) {
	data := []byte(`{"type":"user","permissionMode":"default"}
{"type":"assistant","permissionMode":"acceptEdits"}
{"type":"user","msg":"no mode here"}
`)
	if got, ok := (claude{}).Perm(data); !ok || got != "acceptEdits" {
		t.Errorf("lastPermissionMode = (%q,%v), want acceptEdits", got, ok)
	}
	// A null value is not a match; fall back to the prior string value.
	withNull := append(data, []byte(`{"permissionMode":null}`+"\n")...)
	if got, ok := (claude{}).Perm(withNull); !ok || got != "acceptEdits" {
		t.Errorf("with trailing null = (%q,%v), want acceptEdits", got, ok)
	}
	if _, ok := (claude{}).Perm([]byte(`{"type":"user"}`)); ok {
		t.Error("no permissionMode should not match")
	}
}
