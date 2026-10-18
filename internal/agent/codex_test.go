package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCodexCommand(t *testing.T) {
	c := codex{}
	cases := []struct {
		name string
		l    Launch
		want string
	}{
		{"fresh", Launch{Bin: "codex", Prompt: "fix it", Dirs: []string{"/notes"}},
			"codex --sandbox workspace-write --add-dir '/notes' -- 'fix it'"},
		// A preset id is ignored on a fresh start: codex picks its own.
		{"fresh ignores id", Launch{Bin: "codex", Prompt: "g", SessionID: "sid"},
			"codex --sandbox workspace-write -- 'g'"},
		{"resume by id", Launch{Bin: "codex", Dirs: []string{"/notes"}, SessionID: "sid", Resume: true},
			"codex resume --sandbox workspace-write --add-dir '/notes' 'sid'"},
		{"method", Launch{Bin: "codex", Prompt: "g", Method: "/s/t.method.md"},
			`codex --sandbox workspace-write -c developer_instructions="$(cat '/s/t.method.md')" -- 'g'`},
	}
	for _, tc := range cases {
		if got := c.Command(tc.l); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCodexOneShot(t *testing.T) {
	got := codex{}.OneShot("codex", "gpt-5.5", "INSTRUCT", "")
	if got != "codex exec --ephemeral --sandbox read-only -m 'gpt-5.5' 'INSTRUCT'" {
		t.Errorf("one-shot = %q", got)
	}
}

func TestCodexHook(t *testing.T) {
	c := codex{}
	shell, err := c.Hook([]byte(`{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"/w/a","permission_mode":"default",
		"tool_name":"Bash","tool_input":{"command":"git push"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if shell.Name != "PreToolUse" || shell.SessionID != "s1" || shell.Tool.Kind != ToolShell || shell.Tool.Command != "git push" {
		t.Errorf("shell event = %+v", shell)
	}

	patch := "*** Begin Patch\n*** Update File: main.go\n@@\n-a\n+b\n*** Add File: /abs/new.go\n+x\n*** Update File: old.go\n*** Move to: moved/old.go\n*** End Patch"
	raw, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "cwd": "/w/a", "tool_name": "apply_patch",
		"tool_input": map[string]string{"command": patch},
	})
	edit, err := c.Hook(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/w/a/main.go", "/abs/new.go", "/w/a/old.go", "/w/a/moved/old.go"}
	if edit.Tool.Kind != ToolEdit || !reflect.DeepEqual(edit.Tool.Paths, want) {
		t.Errorf("apply_patch = %+v, want paths %v", edit.Tool, want)
	}

	perm, err := c.Hook([]byte(`{"hook_event_name":"PermissionRequest","session_id":"s1","tool_name":"Bash"}`))
	if err != nil {
		t.Fatal(err)
	}
	if perm.Name != "Notification" || perm.NotificationType != "permission_prompt" {
		t.Errorf("a pending approval should read as needs-you, got %+v", perm)
	}
}

// TestCodexDecision pins the deny: codex runs the tool anyway on "ask".
func TestCodexDecision(t *testing.T) {
	if (codex{}).CanAsk() {
		t.Error("codex cannot ask from a hook")
	}
	for _, action := range []string{"ask", "deny"} {
		raw, err := codex{}.Decision(action, "kovan: confirm before push/PR")
		if err != nil {
			t.Fatal(err)
		}
		var d hookDecision
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		if out := d.HookSpecificOutput; out.PermissionDecision != "deny" || out.PermissionDecisionReason != "kovan: confirm before push/PR" {
			t.Errorf("%s = %+v, want a deny with the reason as given", action, out)
		}
	}
}

func TestCodexDigest(t *testing.T) {
	lines := []string{
		`{"timestamp":"t","type":"session_meta","payload":{"id":"s1"}}`,
		`{"timestamp":"t","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>injected</environment_context>"}]}}`,
		`{"timestamp":"t","type":"event_msg","payload":{"type":"user_message","message":"Read your brief"}}`,
		`{"timestamp":"t","type":"event_msg","payload":{"type":"agent_message","message":"Reading it now."}}`,
		`{"timestamp":"t","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"go test ./...\"]}"}}`,
		`{"timestamp":"t","type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch"}}`,
		`{"timestamp":"t","type":"turn_context","payload":{"approval_policy":"on-request"}}`,
	}
	got := codex{}.Digest([]byte(strings.Join(lines, "\n")))
	want := strings.Join([]string{
		"you: Read your brief",
		"agent: Reading it now.",
		"agent ran shell: bash -lc go test ./...",
		"agent ran apply_patch: *** Begin Patch",
	}, "\n")
	if got != want {
		t.Errorf("digest =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "injected") {
		t.Error("the digest leaked codex's injected context")
	}
}

// TestCodexEncodeMethod pins that the method survives -c's TOML parse: the
// file holds a quoted string, so quotes, newlines and backslashes round-trip.
func TestCodexEncodeMethod(t *testing.T) {
	text := "line \"one\"\n\tC:\\path <b>"
	var back string
	if err := json.Unmarshal(codex{}.EncodeMethod(text), &back); err != nil || back != text {
		t.Errorf("round trip = %q, %v", back, err)
	}
}

func TestFor(t *testing.T) {
	for name, want := range map[string]string{"": Claude, "claude": Claude, "/opt/bin/codex": Codex} {
		d, err := For(name)
		if err != nil || d.Name() != want {
			t.Errorf("For(%q) = %v, %v; want %s", name, d, err, want)
		}
	}
	if _, err := For("cdex"); err == nil {
		t.Error("an unknown agent should error")
	}
}

// TestCodexShellIsShell: a Bash call stays a shell command whatever it runs,
// so every shell gate sees all of it; the app finds an apply_patch inside.
func TestCodexShellIsShell(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "cwd": "/w/a", "tool_name": "Bash",
		"tool_input": map[string]string{"command": "apply_patch <<'EOF'\n*** Begin Patch\n*** End Patch\nEOF\ngit push"},
	})
	ev, err := codex{}.Hook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Tool.Kind != ToolShell || !strings.Contains(ev.Tool.Command, "git push") {
		t.Errorf("shell call = %+v, want the whole command as a shell call", ev.Tool)
	}
}

// TestCodexEncodeMethodTOML: TOML forbids a raw DEL in a basic string, which
// Go's JSON encoder leaves raw; codex would then read the text as garbage.
func TestCodexEncodeMethodTOML(t *testing.T) {
	if out := string(codex{}.EncodeMethod("a\x7fb")); strings.ContainsRune(out, 0x7f) || !strings.Contains(out, `\u007f`) {
		t.Errorf("encoded = %q, want DEL escaped", out)
	}
}
