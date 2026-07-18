package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

func TestEventState(t *testing.T) {
	cases := []struct {
		name  string
		event string
		ntype string
		want  string
		ok    bool
	}{
		{"pre-tool", "PreToolUse", "", "working", true},
		{"post-tool", "PostToolUse", "", "working", true},
		{"prompt submitted", "UserPromptSubmit", "", "working", true},
		{"stop", "Stop", "", "idle", true},
		{"unknown event", "SessionStart", "", "", false},

		// Notifications carry a type: only the blocking kinds mean needs-you.
		{"permission prompt", "Notification", "permission_prompt", "needs-you", true},
		{"mcp elicitation", "Notification", "elicitation_dialog", "needs-you", true},
		{"idle nudge", "Notification", "idle_prompt", "idle", true},
		{"untyped (older CLI)", "Notification", "", "needs-you", true},
		{"auth success", "Notification", "auth_success", "", false},
		{"unknown type", "Notification", "something_new", "", false},
	}
	for _, c := range cases {
		got, ok := eventState(hookEvent{HookEventName: c.event, NotificationType: c.ntype})
		if got != c.want || ok != c.ok {
			t.Errorf("%s: eventState = %q,%v want %q,%v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestShouldNotify(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"working", "needs-you", true},
		{"working", "idle", true},
		{"needs-you", "idle", true},
		{"needs-you", "needs-you", false},
		{"idle", "working", false},
		{"working", "working", false},
	}
	for _, c := range cases {
		if got := shouldNotify(c.prev, c.next); got != c.want {
			t.Errorf("shouldNotify(%q,%q) = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}

func TestRunGateUpdatesManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	m := &session.Manifest{ID: "TASK-1", Title: "g", Worktree: wt, Tmux: "t", State: "idle"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	in := `{"hook_event_name":"PreToolUse","cwd":"` + wt + `","permission_mode":"plan","effort":{"level":"high"}}`
	if err := runGate(strings.NewReader(in), io.Discard); err != nil {
		t.Fatal(err)
	}

	got, err := session.ReadByTmux("t")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "working" || got.Mode != "plan" || got.Effort != "high" {
		t.Errorf("manifest = state %q mode %q effort %q", got.State, got.Mode, got.Effort)
	}
	if got.LastActivity.IsZero() {
		t.Error("last_activity not set")
	}
}

// TestRunGateNotificationTypes pins the screenshot bug: the idle nudge must
// correct a stale needs-you instead of creating one, and an informational
// notification must not touch the state at all.
func TestRunGateNotificationTypes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	m := &session.Manifest{ID: "TASK-1", Title: "g", Worktree: wt, Tmux: "t", State: "needs-you"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	send := func(ntype string) *session.Manifest {
		t.Helper()
		in := `{"hook_event_name":"Notification","notification_type":"` + ntype + `","cwd":"` + wt + `"}`
		if err := runGate(strings.NewReader(in), io.Discard); err != nil {
			t.Fatal(err)
		}
		got, err := session.ReadByTmux("t")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := send("idle_prompt"); got.State != "idle" {
		t.Errorf("idle nudge: state = %q, want idle (stale needs-you corrected)", got.State)
	}
	if got := send("auth_success"); got.State != "idle" {
		t.Errorf("informational: state = %q, want idle (untouched)", got.State)
	}
	if got := send("permission_prompt"); got.State != "needs-you" {
		t.Errorf("permission prompt: state = %q, want needs-you", got.State)
	}
}

// TestRunGateLiveModeScope pins the fix for the frozen write_paths bug: the
// gate resolves posture and write paths from the mode's files at every check,
// so editing mode.yaml reaches a running session with no manifest change.
func TestRunGateLiveModeScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modeDir := filepath.Join(home, "modes", "scoped")
	if err := os.MkdirAll(modeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(modeDir, "prompt.md"), "scoped: {{brief}}")
	write(filepath.Join(modeDir, "mode.yaml"), "posture: edit\nwrite_paths:\n  - allowed/\n")

	wt := t.TempDir()
	m := &session.Manifest{ID: "M-1", Title: "g", Worktree: wt, RepoRoot: wt, Tmux: "tm", State: "working", TaskMode: "scoped"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	edit := func(path string) string {
		t.Helper()
		in := `{"hook_event_name":"PreToolUse","cwd":"` + wt + `","tool_name":"Edit","tool_input":{"file_path":"` + path + `"}}`
		var buf bytes.Buffer
		if err := runGate(strings.NewReader(in), &buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	if out := edit(wt + "/private/journal.md"); !strings.Contains(out, "write paths") {
		t.Fatalf("outside the mode's paths should ask, got %q", out)
	}
	if out := edit(wt + "/allowed/log.md"); out != "" {
		t.Fatalf("inside the mode's paths should pass, got %q", out)
	}

	// The repro: extend the mode's write_paths on disk — the identical event
	// now passes, with no manifest change and no session restart.
	write(filepath.Join(modeDir, "mode.yaml"), "posture: edit\nwrite_paths:\n  - allowed/\n  - private/\n")
	if out := edit(wt + "/private/journal.md"); out != "" {
		t.Fatalf("a mode.yaml edit should reach the running session, got %q", out)
	}

	// Repo-level extras extend a scoped mode.
	write(filepath.Join(wt, ".kovan.yaml"), "write_paths:\n  - Notes/\n")
	if out := edit(wt + "/Notes/idea.md"); out != "" {
		t.Fatalf("repo write_paths should extend a scoped mode, got %q", out)
	}

	// A posture flip on disk reaches the session the same way.
	write(filepath.Join(modeDir, "mode.yaml"), "posture: read-only\n")
	if out := edit(wt + "/main.go"); !strings.Contains(out, "read-only") {
		t.Fatalf("a posture flip should reach the running session, got %q", out)
	}

	// Repo write_paths never restrict an unscoped edit mode.
	write(filepath.Join(modeDir, "mode.yaml"), "posture: edit\n")
	if out := edit(wt + "/main.go"); out != "" {
		t.Fatalf("an unscoped edit mode must stay ungated despite repo write_paths, got %q", out)
	}
}

// TestRunGateUnknownModeFailsOpen: a deleted or unknown mode imposes no
// posture restrictions — a config error must not lock an agent out.
func TestRunGateUnknownModeFailsOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	m := &session.Manifest{ID: "G-1", Title: "g", Worktree: wt, Tmux: "tg", State: "working", TaskMode: "ghost"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	in := `{"hook_event_name":"PreToolUse","cwd":"` + wt + `","tool_name":"Edit","tool_input":{"file_path":"` + wt + `/main.go"}}`
	var buf bytes.Buffer
	if err := runGate(strings.NewReader(in), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("unknown mode should fail open, got %q", buf.String())
	}
}

func TestRunGateNoManifestNoop(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	in := `{"hook_event_name":"PreToolUse","cwd":"` + t.TempDir() + `","tool_name":"Bash","tool_input":{"command":"git push"}}`
	var buf bytes.Buffer
	if err := runGate(strings.NewReader(in), &buf); err != nil {
		t.Errorf("no manifest should be a no-op, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("non-kovan session should get no decision, got %q", buf.String())
	}
}

func TestRunGateEmitsPushDecision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	m := &session.Manifest{ID: "TASK-1", Title: "g", Worktree: wt, Tmux: "t", State: "working"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	in := `{"hook_event_name":"PreToolUse","cwd":"` + wt + `","tool_name":"Bash","tool_input":{"command":"git push origin main"}}`
	var buf bytes.Buffer
	if err := runGate(strings.NewReader(in), &buf); err != nil {
		t.Fatal(err)
	}

	var d hookDecision
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("expected a decision, got %q: %v", buf.String(), err)
	}
	if d.HookSpecificOutput.PermissionDecision != "ask" {
		t.Errorf("decision = %+v", d.HookSpecificOutput)
	}
	if got, _ := session.ReadByTmux("t"); got.State != "working" {
		t.Errorf("enrichment regressed: state = %q", got.State)
	}
}

func TestInstallHooksFromScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	want, err := gateRunCommand()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(exeOf(want)) || want == "kovan gate run" {
		t.Fatalf("hook command should be an absolute path, got %q", want)
	}

	added, err := installHooks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != len(gateHookEvents) {
		t.Fatalf("added %v, want all four events", added)
	}

	again, err := installHooks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("re-run changed %v, want nothing (idempotent)", again)
	}

	hooks := readHooks(t, path)
	for _, ev := range gateHookEvents {
		arr, _ := hooks[ev].([]any)
		if got := gateCommandIn(arr); got != want {
			t.Errorf("%s command = %q, want %q", ev, got, want)
		}
	}
}

func TestInstallHooksPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"model":"opus","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"other-tool"}]}]}}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	want, _ := gateRunCommand()
	if _, err := installHooks(path); err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root["model"] != "opus" {
		t.Error("clobbered the model key")
	}
	pre, _ := root["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("PreToolUse groups = %d, want 2 (existing + kovan)", len(pre))
	}
	if got := gateCommandIn(pre); got != want {
		t.Errorf("kovan hook not appended to PreToolUse: got %q", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("backup not written")
	}
}

func TestInstallHooksRepointsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"/old/path/kovan gate run"}]}]}}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	want, _ := gateRunCommand()
	if _, err := installHooks(path); err != nil {
		t.Fatal(err)
	}

	hooks := readHooks(t, path)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Errorf("re-point should not append: PreToolUse groups = %d, want 1", len(pre))
	}
	if got := gateCommandIn(pre); got != want {
		t.Errorf("stale hook not re-pointed: got %q, want %q", got, want)
	}
}

func TestInstallHooksRejectsBadShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"hooks":{"PreToolUse":"oops"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installHooks(path); err == nil {
		t.Error("expected an error rather than clobbering a non-array hooks event")
	}
}

// gateCommandIn returns the kovan gate-run command found in a list of hook
// groups, or "" if none.
func gateCommandIn(groups []any) string {
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); isGateRun(cmd) {
				return cmd
			}
		}
	}
	return ""
}

// exeOf extracts the binary path from a "<path> gate run" hook command.
func exeOf(command string) string {
	return strings.Trim(strings.TrimSuffix(command, " gate run"), `"`)
}

func readHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	hooks, _ := root["hooks"].(map[string]any)
	return hooks
}
