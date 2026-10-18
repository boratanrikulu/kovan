package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
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
		got, ok := eventState(agent.Event{Name: c.event, NotificationType: c.ntype})
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
	if err := runGate(agent.Claude, strings.NewReader(in), io.Discard); err != nil {
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
		if err := runGate(agent.Claude, strings.NewReader(in), io.Discard); err != nil {
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
	m := &session.Manifest{ID: "M-1", Title: "g", Repo: "grepo", Worktree: wt, RepoRoot: wt, Tmux: "tm", State: "working", TaskMode: "scoped"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	edit := func(path string) string {
		t.Helper()
		in := `{"hook_event_name":"PreToolUse","cwd":"` + wt + `","tool_name":"Edit","tool_input":{"file_path":"` + path + `"}}`
		var buf bytes.Buffer
		if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
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

	// Repo-level extras extend a scoped mode; the repo config lives in the
	// kovan home, so it reaches the session from any worktree.
	if err := os.MkdirAll(filepath.Join(home, "projects", "grepo"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(config.RepoConfigPath(home, "grepo"), "write_paths:\n  - Notes/\n")
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

func TestRunGateScopesExtraWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, "config.yaml"), "notify: none\n")
	write(filepath.Join(home, "modes", "scoped", "prompt.md"), "scoped: {{brief}}")
	write(filepath.Join(home, "modes", "scoped", "mode.yaml"), "posture: read-only\n")
	// Only the cloud repo carves out Notes/, so the carve-out resolves against
	// the workspace the file is in, with that repo's config.
	write(config.RepoConfigPath(home, "cloud"), "write_paths:\n  - Notes/\n")

	primary, extra := t.TempDir(), t.TempDir()
	m := &session.Manifest{ID: "M-1", Title: "g", Repo: "agent", Worktree: primary, RepoRoot: primary, Tmux: "tm", State: "working", TaskMode: "scoped",
		SessionID: "sid-1", Extra: []session.Workspace{{Repo: "cloud", RepoRoot: extra, Worktree: extra}}}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	edit := func(path string) string {
		t.Helper()
		in := `{"hook_event_name":"PreToolUse","session_id":"sid-1","cwd":"` + primary + `","tool_name":"Edit","tool_input":{"file_path":"` + path + `"}}`
		var buf bytes.Buffer
		if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	if out := edit(extra + "/web/app.ts"); !strings.Contains(out, "read-only") {
		t.Errorf("a read-only agent editing its cloud worktree should ask, got %q", out)
	}
	if out := edit(extra + "/Notes/idea.md"); out != "" {
		t.Errorf("cloud's write_paths should allow its Notes/, got %q", out)
	}
	if out := edit(primary + "/Notes/idea.md"); !strings.Contains(out, "read-only") {
		t.Errorf("cloud's write_paths must not open the agent repo's Notes/, got %q", out)
	}
}

func TestRunGateCwdFallbackIgnoresExtras(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	primary, extra := t.TempDir(), t.TempDir()
	m := &session.Manifest{ID: "M-1", Repo: "agent", Worktree: primary, Tmux: "tm", State: "idle", SessionID: "sid-agent",
		Extra: []session.Workspace{{Repo: "cloud", Worktree: extra, Adopted: true}}}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	// A plain claude session Bora runs by hand in that worktree is not the agent.
	in := `{"hook_event_name":"PreToolUse","session_id":"someone-else","cwd":"` + extra + `","tool_name":"Bash","tool_input":{"command":"git push"}}`
	var buf bytes.Buffer
	if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Errorf("an unrelated session in an extra worktree got a gate decision: %q", buf.String())
	}
	if got, _ := session.ReadByTmux("tm"); got.State != "idle" {
		t.Errorf("agent state = %q, want untouched idle", got.State)
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
	if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
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
	if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
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
	if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
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

	want, err := gateRunCommand(mustDriver(agent.Claude))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(exeOf(want)) || want == "kovan gate run" {
		t.Fatalf("hook command should be an absolute path, got %q", want)
	}

	added, err := installHooks(path, mustDriver(agent.Claude))
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != len(mustDriver(agent.Claude).HookEvents()) {
		t.Fatalf("added %v, want all four events", added)
	}

	again, err := installHooks(path, mustDriver(agent.Claude))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("re-run changed %v, want nothing (idempotent)", again)
	}

	hooks := readHooks(t, path)
	for _, ev := range mustDriver(agent.Claude).HookEvents() {
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

	want, _ := gateRunCommand(mustDriver(agent.Claude))
	if _, err := installHooks(path, mustDriver(agent.Claude)); err != nil {
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

	want, _ := gateRunCommand(mustDriver(agent.Claude))
	if _, err := installHooks(path, mustDriver(agent.Claude)); err != nil {
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
	if _, err := installHooks(path, mustDriver(agent.Claude)); err == nil {
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

// hookDecision is the PreToolUse decision a claude hook reads back.
type hookDecision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// gateHome is a throwaway kovan home with notifications off.
func gateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("notify: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunGateResolvesByKovanSession: two tabs share a worktree and the event
// carries an id no manifest knows yet, so only KOVAN_SESSION can tell which
// tab it is. The cwd alone would pick either.
func TestRunGateResolvesByKovanSession(t *testing.T) {
	gateHome(t)
	wt := t.TempDir()
	for _, tab := range []string{"t1", "t2"} {
		if err := (&session.Manifest{ID: tab, Worktree: wt, Tmux: tab, State: "idle"}).Write(); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(sessionEnv, "t2")
	in := `{"hook_event_name":"UserPromptSubmit","session_id":"unknown","cwd":"` + wt + `"}`
	if err := runGate(agent.Codex, strings.NewReader(in), io.Discard); err != nil {
		t.Fatal(err)
	}
	t1, _ := session.ReadByTmux("t1")
	t2, _ := session.ReadByTmux("t2")
	if t2.State != "working" || t1.State != "idle" {
		t.Errorf("states = t1 %q, t2 %q; want only t2 working", t1.State, t2.State)
	}
}

// TestRunGateLearnsSessionID: codex picks its own session id, so kovan records
// the one its first hook reports, and wake can resume that conversation.
func TestRunGateLearnsSessionID(t *testing.T) {
	gateHome(t)
	wt := t.TempDir()
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: wt, Tmux: "cx", State: "working"}).Write(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionEnv, "cx")
	send := func(d, in string) {
		t.Helper()
		if err := runGate(d, strings.NewReader(in), io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	send(agent.Codex, `{"hook_event_name":"SessionStart","session_id":"cx-1","cwd":"`+wt+`"}`)
	got, _ := session.ReadByTmux("cx")
	if got.SessionID != "cx-1" {
		t.Fatalf("session id = %q, want the one SessionStart reported", got.SessionID)
	}
	if got.State != "working" {
		t.Errorf("SessionStart carries no state, but state became %q", got.State)
	}
	// A later event never moves a known id.
	t.Setenv(sessionEnv, "")
	send(agent.Codex, `{"hook_event_name":"Stop","session_id":"cx-1","cwd":"`+wt+`"}`)
	if got, _ := session.ReadByTmux("cx"); got.SessionID != "cx-1" || got.State != "idle" {
		t.Errorf("after Stop = id %q state %q, want cx-1 idle", got.SessionID, got.State)
	}

	// Claude's id is chosen by kovan; a claude manifest without one (an old
	// agent) learns nothing from a hook.
	if err := (&session.Manifest{ID: "o", Worktree: t.TempDir(), Tmux: "old"}).Write(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionEnv, "old")
	send(agent.Claude, `{"hook_event_name":"Stop","session_id":"stray"}`)
	if got, _ := session.ReadByTmux("old"); got.SessionID != "" {
		t.Errorf("claude manifest learned %q, want no id", got.SessionID)
	}
}

// TestRunGateCodexDeniesPush: codex treats "ask" as a failed hook and runs the
// tool, so the push gate reaches it as a deny.
func TestRunGateCodexDeniesPush(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	wt := t.TempDir()
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: wt, Tmux: "cx", SessionID: "cx-1", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	in := `{"hook_event_name":"PreToolUse","session_id":"cx-1","cwd":"` + wt + `","tool_name":"Bash","tool_input":{"command":"git push origin feat/x"}}`
	var buf bytes.Buffer
	if err := runGate(agent.Codex, strings.NewReader(in), &buf); err != nil {
		t.Fatal(err)
	}
	var d hookDecision
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("expected a decision, got %q: %v", buf.String(), err)
	}
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("decision = %+v, want deny", d.HookSpecificOutput)
	}
}

// TestRunGateCodexPatchReadOnly: a read-only agent's apply_patch into its
// worktree is gated, like Claude's Edit.
func TestRunGateCodexPatchReadOnly(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	wt := t.TempDir()
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: wt, Tmux: "cx", SessionID: "cx-2", TaskMode: "review"}).Write(); err != nil {
		t.Fatal(err)
	}
	ev, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "cx-2", "cwd": wt, "tool_name": "apply_patch",
		"tool_input": map[string]string{"command": "*** Begin Patch\n*** Update File: main.go\n*** End Patch"},
	})
	var buf bytes.Buffer
	if err := runGate(agent.Codex, bytes.NewReader(ev), &buf); err != nil {
		t.Fatal(err)
	}
	var d hookDecision
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("expected a decision, got %q: %v", buf.String(), err)
	}
	if d.HookSpecificOutput.PermissionDecision != "deny" || !strings.HasPrefix(d.HookSpecificOutput.PermissionDecisionReason, readOnlyReason) {
		t.Errorf("decision = %+v, want the read-only gate", d.HookSpecificOutput)
	}
}

// TestRunGateLearnsOnlyItsOwn: an id is learned only from the tab's own CLI,
// found by KOVAN_SESSION. A codex run inside a claude tab, or a codex the user
// runs by hand in the worktree (found by cwd), must not hand it an id.
func TestRunGateLearnsOnlyItsOwn(t *testing.T) {
	gateHome(t)
	wt := t.TempDir()
	if err := (&session.Manifest{ID: "k", Agent: agent.Claude, Worktree: t.TempDir(), Tmux: "kt"}).Write(); err != nil {
		t.Fatal(err)
	}
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: wt, Tmux: "ct"}).Write(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionEnv, "kt")
	if err := runGate(agent.Codex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"nested"}`), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, _ := session.ReadByTmux("kt"); got.SessionID != "" {
		t.Errorf("claude tab learned %q from a nested codex", got.SessionID)
	}
	t.Setenv(sessionEnv, "")
	in := `{"hook_event_name":"SessionStart","session_id":"by-hand","cwd":"` + wt + `"}`
	if err := runGate(agent.Codex, strings.NewReader(in), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, _ := session.ReadByTmux("ct"); got.SessionID != "" {
		t.Errorf("codex tab learned %q from a session found by cwd", got.SessionID)
	}
}

// TestRunGateCodexApprove: codex can't ask, so a gate denies. A message from
// the user (not a sub-agent) with kovan-allow lets the pending command run
// once, in that turn; anything else stays denied.
func TestRunGateCodexApprove(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: t.TempDir(), Tmux: "cx", SessionID: "cx-9", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	tool := func(cmd string) string {
		return `{"hook_event_name":"PreToolUse","session_id":"cx-9","tool_name":"Bash","tool_input":{"command":"` + cmd + `"}}`
	}
	prompt := func(text, agentID string) string {
		return `{"hook_event_name":"UserPromptSubmit","session_id":"cx-9","agent_id":"` + agentID + `","prompt":"` + text + `"}`
	}
	stop := `{"hook_event_name":"Stop","session_id":"cx-9"}`
	push, force := tool("git push origin feat/x"), tool("git push --force")
	decide := func(in string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := runGate(agent.Codex, strings.NewReader(in), &buf); err != nil {
			t.Fatal(err)
		}
		if buf.Len() == 0 {
			return "pass"
		}
		var d hookDecision
		if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d.HookSpecificOutput.PermissionDecision
	}
	steps := []struct {
		name, in, want string
	}{
		{"first push", push, "deny"},
		{"plain go", prompt("yes go, push it", ""), "pass"},
		{"push after a plain go", push, "deny"},
		{"approve from a sub-agent", prompt("kovan-allow", "a1"), "pass"},
		{"push after the sub-agent", push, "deny"},
		{"the user approves", prompt("Kovan-Allow, re-run it", ""), "pass"},
		{"the approved push", push, "pass"},
		{"the same push again", push, "deny"},
		{"approve", prompt("kovan-allow", ""), "pass"},
		{"another command", force, "deny"},
		{"push: the pending command is now --force", push, "deny"},
		{"approve", prompt("kovan-allow", ""), "pass"},
		{"turn ends unused", stop, "pass"},
		{"push in a later turn", push, "deny"},
		{"approve", prompt("kovan-allow", ""), "pass"},
		{"interrupted, the next message has no word", prompt("actually wait", ""), "pass"},
		{"push after the interrupt", push, "deny"},
	}
	for _, st := range steps {
		if got := decide(st.in); got != st.want {
			t.Errorf("%s = %s, want %s", st.name, got, st.want)
		}
	}
	// The deny names the command and the word.
	var buf bytes.Buffer
	_ = runGate(agent.Codex, strings.NewReader(push), &buf)
	if !strings.Contains(buf.String(), "git push origin feat/x") || !strings.Contains(buf.String(), "kovan-allow") {
		t.Errorf("deny reason = %s, want the command and kovan-allow", buf.String())
	}
}

// TestRunGateClaudeAsksEveryTime: claude asks the user itself, so it never
// gets a pass to spend.
func TestRunGateClaudeAsksEveryTime(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	if err := (&session.Manifest{ID: "k", Worktree: t.TempDir(), Tmux: "kt", SessionID: "k-1", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{
		`{"hook_event_name":"PreToolUse","session_id":"k-1","tool_name":"Bash","tool_input":{"command":"git push"}}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"k-1","prompt":"go"}`,
		`{"hook_event_name":"PreToolUse","session_id":"k-1","tool_name":"Bash","tool_input":{"command":"git push"}}`,
	} {
		var buf bytes.Buffer
		if err := runGate(agent.Claude, strings.NewReader(in), &buf); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(in, "PreToolUse") && !strings.Contains(buf.String(), `"ask"`) {
			t.Errorf("claude push = %q, want ask", buf.String())
		}
	}
}

// TestRunGateCodexPatchRelative: a patch path codex reports relative to an
// empty cwd belongs to the agent's worktree, and is gated there.
func TestRunGateCodexPatchRelative(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	wt := t.TempDir()
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: wt, Tmux: "cx", SessionID: "cx-r", TaskMode: "review"}).Write(); err != nil {
		t.Fatal(err)
	}
	ev, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "cx-r", "tool_name": "apply_patch",
		"tool_input": map[string]string{"command": "*** Begin Patch\n*** Add File: notes.md\n*** End Patch"},
	})
	var buf bytes.Buffer
	if err := runGate(agent.Codex, bytes.NewReader(ev), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), readOnlyReason) {
		t.Errorf("decision = %q, want the read-only gate", buf.String())
	}
}

// codexDecide runs one codex hook event through the gate and returns the
// decision, "" when none.
func codexDecide(t *testing.T, ev map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(ev)
	var buf bytes.Buffer
	if err := runGate(agent.Codex, bytes.NewReader(raw), &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		return ""
	}
	var d hookDecision
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d.HookSpecificOutput.PermissionDecision + ": " + d.HookSpecificOutput.PermissionDecisionReason
}

// TestRunGateShellPatch: a shell apply_patch is an edit and a shell command
// at once. The push chained after it is still gated; a patch in a read-only
// mode is gated wherever it reads its body from, or whatever dir it cd's to.
func TestRunGateShellPatch(t *testing.T) {
	gateHome(t)
	t.Setenv(sessionEnv, "")
	code, ro := t.TempDir(), t.TempDir()
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: code, Tmux: "cc", SessionID: "s-code", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	if err := (&session.Manifest{ID: "r", Agent: agent.Codex, Worktree: ro, Tmux: "cr", SessionID: "s-ro", TaskMode: "review"}).Write(); err != nil {
		t.Fatal(err)
	}
	bash := func(sid, cwd, cmd string) map[string]any {
		return map[string]any{"hook_event_name": "PreToolUse", "session_id": sid, "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]string{"command": cmd}}
	}
	patch := "apply_patch <<'EOF'\n*** Begin Patch\n*** Update File: main.go\n*** End Patch\nEOF"
	if got := codexDecide(t, bash("s-code", code, patch+"\ngit push origin feat/x")); !strings.Contains(got, pushReason) {
		t.Errorf("patch then push = %q, want the push gate", got)
	}
	if got := codexDecide(t, bash("s-code", code, patch)); got != "" {
		t.Errorf("a patch in a code-mode tab = %q, want no gate", got)
	}
	for _, cmd := range []string{patch, "apply_patch < /tmp/p.diff", "cd sub && " + patch} {
		if got := codexDecide(t, bash("s-ro", ro, cmd)); !strings.Contains(got, readOnlyReason) {
			t.Errorf("read-only %q = %q, want the read-only gate", cmd, got)
		}
	}
}

// TestRunGateDenyOutranksAsk: a deny pattern anywhere in the command wins over
// a built-in ask, so an approval for the push cannot carry the denied part.
func TestRunGateDenyOutranksAsk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv(sessionEnv, "")
	cfg := "notify: none\ngates:\n  patterns:\n    - {match: 'terraform', action: ask, reason: 'kovan: terraform'}\n    - {match: 'terraform +destroy', action: deny, reason: 'kovan: no destroy'}\n    - {match: 'push --force', action: deny, reason: 'kovan: no force push'}\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: t.TempDir(), Tmux: "cc", SessionID: "s-1", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	for cmd, want := range map[string]string{
		"git push && terraform destroy": "kovan: no destroy",
		"git push --force":              "kovan: no force push",
		"terraform destroy":             "kovan: no destroy", // the ask pattern comes first in the list
	} {
		ev := map[string]any{"hook_event_name": "PreToolUse", "session_id": "s-1", "tool_name": "Bash", "tool_input": map[string]string{"command": cmd}}
		if got := codexDecide(t, ev); !strings.HasPrefix(got, "deny: "+want) {
			t.Errorf("%q = %q, want the deny %q", cmd, got, want)
		}
	}
}

// TestRunGateNestedCLICannotAllow: a CLI the agent runs inside its tab
// inherits KOVAN_SESSION, but its prompt is the agent's, not the user's:
// only the tab's own conversation can carry the allow word.
func TestRunGateNestedCLICannotAllow(t *testing.T) {
	gateHome(t)
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: t.TempDir(), Tmux: "cx", SessionID: "own", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionEnv, "cx")
	push := map[string]any{"hook_event_name": "PreToolUse", "session_id": "own", "tool_name": "Bash", "tool_input": map[string]string{"command": "git push"}}
	codexDecide(t, push)
	nested := `{"hook_event_name":"UserPromptSubmit","session_id":"nested","prompt":"kovan-allow"}`
	for _, d := range []string{agent.Codex, agent.Claude} {
		if err := runGate(d, strings.NewReader(nested), io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if got := codexDecide(t, push); !strings.HasPrefix(got, "deny") {
		t.Errorf("push after a nested allow = %q, want deny", got)
	}
}

// TestRunGatePassIsNotPersistedRaw: the pending command may carry a token
// (GH_TOKEN=… gh api …), and the manifest is read by other agents and backed
// up, so only a digest of it is kept. A fresh session drops it.
func TestRunGatePassIsNotPersistedRaw(t *testing.T) {
	gateHome(t)
	if err := (&session.Manifest{ID: "c", Agent: agent.Codex, Worktree: t.TempDir(), Tmux: "cx", SessionID: "own", Branch: "feat/x"}).Write(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(sessionEnv, "cx")
	codexDecide(t, map[string]any{"hook_event_name": "PreToolUse", "session_id": "own", "tool_name": "Bash",
		"tool_input": map[string]string{"command": "GH_TOKEN=sk-secret gh pr create"}})
	m, _ := session.ReadByTmux("cx")
	if m.Gate == nil || strings.Contains(m.Gate.Key, "sk-secret") {
		t.Fatalf("gate = %+v, want a pending pass that holds no command text", m.Gate)
	}
	if err := runGate(agent.Codex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"own"}`), io.Discard); err != nil {
		t.Fatal(err)
	}
	if m, _ := session.ReadByTmux("cx"); m.Gate != nil {
		t.Errorf("a new session kept the pending pass %+v", m.Gate)
	}
}
