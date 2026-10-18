package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
)

// recordRunner is a runner that records what it was asked to do.
type recordRunner struct {
	alive   bool
	killed  []string
	started []runner.Session
}

func (r *recordRunner) Start(s runner.Session) error {
	r.started = append(r.started, s)
	return nil
}
func (r *recordRunner) Attach(string) error                 { return nil }
func (r *recordRunner) AttachCmd(string) (*exec.Cmd, bool)  { return nil, false }
func (r *recordRunner) Capture(string, int) (string, error) { return "", nil }
func (r *recordRunner) Exists(string) (bool, error)         { return r.alive, nil }
func (r *recordRunner) Sessions() ([]string, error)         { return nil, nil }
func (r *recordRunner) Kill(name string) error              { r.killed = append(r.killed, name); return nil }
func (r *recordRunner) last() string                        { return r.started[len(r.started)-1].Cmd }

func TestSwapSession(t *testing.T) {
	m := &session.Manifest{Agent: agent.Claude, SessionID: "c-1"}
	swapSession(m, agent.Codex)
	if m.SessionID != "" || !reflect.DeepEqual(m.Sessions, map[string]string{"claude": "c-1"}) {
		t.Fatalf("to codex = id %q, sessions %v", m.SessionID, m.Sessions)
	}
	m.Agent, m.SessionID = agent.Codex, "x-1"
	swapSession(m, agent.Claude)
	if m.SessionID != "c-1" || !reflect.DeepEqual(m.Sessions, map[string]string{"codex": "x-1"}) {
		t.Errorf("back to claude = id %q, sessions %v; want c-1 and codex parked", m.SessionID, m.Sessions)
	}
}

// switchSetup is a machine with both CLIs installed as fakes: claude answers a
// one-shot (-p) with note, or fails when note is "".
func switchSetup(t *testing.T, note string) (*config.Global, *session.Manifest) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(sessionEnv, "")
	bin := t.TempDir()
	answer := "exit 1"
	if note != "" {
		answer = "cat >/dev/null; echo '" + note + "'; exit 0"
	}
	writeFile(t, filepath.Join(bin, "claude"), "#!/bin/sh\nfor a; do [ \"$a\" = -p ] && { "+answer+"; }; done\nexit 0\n")
	writeFile(t, filepath.Join(bin, "codex"), "#!/bin/sh\nexit 0\n")
	for _, f := range []string{"claude", "codex"} {
		if err := os.Chmod(filepath.Join(bin, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeTranscript(t, "c-1", `{"type":"user","message":{"content":"make the gate deny"}}`+"\n")
	transcriptPaths.Delete("c-1")

	m := &session.Manifest{ID: "T-1", Title: "gate work", Repo: "kovan", Tmux: "kovan-kovan-T-1",
		Worktree: t.TempDir(), Base: "main", Agent: agent.Claude, SessionID: "c-1", TaskMode: "code"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	global, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	return global, m
}

func handoffOf(t *testing.T, m *session.Manifest) string {
	t.Helper()
	notes, _ := taskDocsFor(m)
	data, err := os.ReadFile(filepath.Join(notes, handoffFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSwitchAgent: a running claude tab moves to codex. The old session stops,
// a handoff note is written from claude's conversation, and codex starts fresh
// on the takeover prompt. Switching back resumes claude's own conversation.
func TestSwitchAgent(t *testing.T) {
	global, m := switchSetup(t, "HANDOFF NOTE BODY")
	run := &recordRunner{alive: true}
	if _, err := switchAgent(m, global, run, agent.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(run.killed, []string{m.Tmux}) {
		t.Errorf("killed = %v, want the running tab stopped", run.killed)
	}
	if cmd := run.last(); !strings.Contains(cmd, "codex ") || !strings.Contains(cmd, "taking over task T-1") || strings.Contains(cmd, " resume") {
		t.Errorf("codex launch = %q, want a fresh start on the takeover prompt", cmd)
	}
	got, _ := session.ReadByTmux(m.Tmux)
	if got.Agent != agent.Codex || got.SessionID != "" || got.Sessions["claude"] != "c-1" {
		t.Errorf("manifest = agent %q id %q sessions %v", got.Agent, got.SessionID, got.Sessions)
	}
	note := handoffOf(t, m)
	if !strings.Contains(note, "HANDOFF NOTE BODY") || !strings.Contains(note, "from claude to codex") {
		t.Errorf("handoff.md =\n%s", note)
	}
	home, _ := config.Dir()
	if method, _ := os.ReadFile(methodRunFile(home, m.Tmux)); !strings.Contains(string(method), handoffFile) {
		t.Errorf("the new agent's method does not point at the handoff note:\n%s", method)
	}

	// Codex reported its id; going back resumes claude's conversation and parks
	// codex's.
	got.SessionID = "x-1"
	if err := got.Write(); err != nil {
		t.Fatal(err)
	}
	run.alive = false
	if _, err := switchAgent(got, global, run, agent.Claude, ""); err != nil {
		t.Fatal(err)
	}
	if len(run.killed) != 1 {
		t.Errorf("a stopped tab was killed again: %v", run.killed)
	}
	if cmd := run.last(); !strings.Contains(cmd, "--resume 'c-1'") {
		t.Errorf("claude launch = %q, want a resume of c-1", cmd)
	}
	back, _ := session.ReadByTmux(m.Tmux)
	if back.SessionID != "c-1" || back.Sessions["codex"] != "x-1" {
		t.Errorf("manifest = id %q sessions %v", back.SessionID, back.Sessions)
	}
}

// TestSwitchAgentHandoffFallback: a failed summary still leaves the last turns
// in the note, and the switch goes on.
func TestSwitchAgentHandoffFallback(t *testing.T) {
	global, m := switchSetup(t, "")
	if _, err := switchAgent(m, global, &recordRunner{}, agent.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if note := handoffOf(t, m); !strings.Contains(note, "summary failed") || !strings.Contains(note, "make the gate deny") {
		t.Errorf("handoff.md =\n%s\nwant the raw digest", note)
	}
}

// TestSwitchAgentRefusals: a CLI that is not installed, or an account of
// another CLI, stops the switch before anything changes.
func TestSwitchAgentRefusals(t *testing.T) {
	global, m := switchSetup(t, "x")
	t.Setenv("PATH", "/nonexistent")
	run := &recordRunner{alive: true}
	if _, err := switchAgent(m, global, run, agent.Codex, ""); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v, want not installed", err)
	}
	if len(run.killed)+len(run.started) != 0 {
		t.Errorf("a refused switch touched the session: %+v", run)
	}
	if got, _ := session.ReadByTmux(m.Tmux); got.Agent != agent.Claude {
		t.Errorf("a refused switch changed the agent to %q", got.Agent)
	}

	global, m = switchSetup(t, "x")
	global.Accounts = map[string]config.Account{"personal": {Agent: agent.Claude, TokenFile: "/x"}}
	if _, err := switchAgent(m, global, run, agent.Codex, "personal"); err == nil || !strings.Contains(err.Error(), "claude account") {
		t.Errorf("err = %v, want a claude account refused for codex", err)
	}
}

// TestSwitchToClaudeGetsSessionID: claude's id is kovan's to pick, so a tab
// that never ran under claude needs one minted, or every later wake falls back
// to --continue and the transcript is never found.
func TestSwitchToClaudeGetsSessionID(t *testing.T) {
	global, m := switchSetup(t, "x")
	m.Agent, m.SessionID = agent.Codex, "x-1"
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	run := &recordRunner{}
	if _, err := switchAgent(m, global, run, agent.Claude, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := session.ReadByTmux(m.Tmux)
	if got.SessionID == "" || !strings.Contains(run.last(), "--session-id '"+got.SessionID+"'") {
		t.Errorf("claude launch = %q with id %q, want a minted --session-id", run.last(), got.SessionID)
	}
}

// TestSwitchAgentStartFails: a launch that fails leaves the manifest as it was,
// so the next wake brings back the old CLI rather than a half-switched tab.
func TestSwitchAgentStartFails(t *testing.T) {
	global, m := switchSetup(t, "x")
	run := &failRunner{recordRunner{alive: true}}
	if _, err := switchAgent(m, global, run, agent.Codex, ""); err == nil {
		t.Fatal("a failed start should be an error")
	}
	got, _ := session.ReadByTmux(m.Tmux)
	if got.Agent != agent.Claude || got.SessionID != "c-1" || got.Sessions != nil {
		t.Errorf("manifest after a failed switch = agent %q id %q sessions %v, want it unchanged", got.Agent, got.SessionID, got.Sessions)
	}
}

type failRunner struct{ recordRunner }

func (f *failRunner) Start(runner.Session) error { return os.ErrPermission }

// TestSwitchAgentAccountIsExplicit: the modal's pick is the choice, "" being
// the CLI's own login; a default account is never filled in behind it.
func TestSwitchAgentAccountIsExplicit(t *testing.T) {
	global, m := switchSetup(t, "x")
	global.Accounts = map[string]config.Account{"oa": {Agent: agent.Codex, Home: t.TempDir()}}
	global.DefaultAccount = "oa"
	if _, err := switchAgent(m, global, &recordRunner{}, agent.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := session.ReadByTmux(m.Tmux); got.Account != "" {
		t.Errorf("account = %q, want the explicit system pick kept", got.Account)
	}
}

// TestEditAgentRefusedSwitchChangesNothing: a switch that cannot happen is
// refused before the other fields are written.
func TestEditAgentRefusedSwitchChangesNothing(t *testing.T) {
	_, m := switchSetup(t, "x")
	t.Setenv("PATH", "/nonexistent")
	if _, _, err := editAgent(m.Tmux, "new title", "code", agent.Codex, "", "red"); err == nil {
		t.Fatal("switching to a CLI that is not installed should fail")
	}
	if got, _ := session.ReadByTmux(m.Tmux); got.Title != "gate work" || got.Color != "" {
		t.Errorf("a refused switch wrote title %q color %q", got.Title, got.Color)
	}
}

// TestStartAgentFlagIsADriverName: --agent names a CLI. A path or anything
// else lands in the launch command, so only a config file may carry one.
func TestStartAgentFlagIsADriverName(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	for _, bad := range []string{"x;rm -rf ~;/claude", "/opt/bin/codex", "cdex"} {
		if _, err := scaffoldAgent(t.TempDir(), "x", "t", startOpts{Agent: bad}); err == nil || !strings.Contains(err.Error(), "agent") {
			t.Errorf("--agent %q: err = %v, want refused", bad, err)
		}
	}
}

// TestSwitchAgentWritesBeforeStart: the new CLI's first hook reads the
// manifest, so it must already name that CLI when the session starts.
func TestSwitchAgentWritesBeforeStart(t *testing.T) {
	global, m := switchSetup(t, "x")
	run := &readingRunner{tmux: m.Tmux}
	if _, err := switchAgent(m, global, run, agent.Codex, ""); err != nil {
		t.Fatal(err)
	}
	if run.agentAtStart != agent.Codex {
		t.Errorf("manifest at Start names %q, want codex", run.agentAtStart)
	}
}

type readingRunner struct {
	recordRunner
	tmux         string
	agentAtStart string
}

func (r *readingRunner) Start(runner.Session) error {
	if m, err := session.ReadByTmux(r.tmux); err == nil {
		r.agentAtStart = m.Agent
	}
	return nil
}
