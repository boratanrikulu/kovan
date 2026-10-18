package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
)

// TestHookTargets: claude's settings always; codex's hooks.json in its
// default home and every account home, once each, and only when codex is
// installed or has an account.
func TestHookTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PATH", "/nonexistent")
	paths := func(g *config.Global) []string {
		var out []string
		for _, h := range hookTargets(g) {
			out = append(out, h.path)
		}
		return out
	}
	home := os.Getenv("HOME")
	claude := filepath.Join(home, ".claude", "settings.json")
	if got := paths(&config.Global{}); !slices.Equal(got, []string{claude}) {
		t.Errorf("no codex = %v, want claude only", got)
	}
	acct := t.TempDir()
	g := &config.Global{Accounts: map[string]config.Account{
		"a": {Agent: agent.Codex, Home: acct},
		"b": {Agent: agent.Codex, Home: acct},
		"k": {Agent: agent.Claude, TokenFile: "/x"},
	}}
	want := []string{claude, filepath.Join(home, ".codex", "hooks.json"), filepath.Join(acct, "hooks.json")}
	slices.Sort(want[1:])
	if got := paths(g); !slices.Equal(got, want) {
		t.Errorf("codex accounts = %v, want %v", got, want)
	}
}

// TestHooksInstalledStale: hooks pointing at another kovan binary are present
// but stale, which doctor reports and the start warning does not.
func TestHooksInstalledStale(t *testing.T) {
	d := mustDriver(agent.Claude)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := installHooks(path, d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	cmd, _ := gateRunCommand(d)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), cmd, "/old/kovan gate run")), 0o644); err != nil {
		t.Fatal(err)
	}
	if hooksInstalled(path, d) || !hooksPresent(path, d) {
		t.Errorf("installed = %v, present = %v; want stale but present", hooksInstalled(path, d), hooksPresent(path, d))
	}
}

// TestCodexConvo: a codex conversation is found under the CODEX_HOME of the
// agent's account, by the id in the rollout's name.
func TestCodexConvo(t *testing.T) {
	kh := t.TempDir()
	t.Setenv("KOVAN_HOME", kh)
	acct := t.TempDir()
	writeFile(t, filepath.Join(kh, "config.yaml"), "accounts:\n  oa: {agent: codex, home: "+acct+"}\n")
	rollout := filepath.Join(acct, "sessions", "2026", "10", "18", "rollout-2026-10-18T20-00-00-x-7.jsonl")
	writeFile(t, rollout, `{"type":"event_msg","payload":{"type":"user_message","message":"hello codex"}}`+"\n")
	transcriptPaths.Delete("x-7")
	m := &session.Manifest{Agent: agent.Codex, Account: "oa", SessionID: "x-7"}
	if c := manifestConvo(m); !c.exists() || c.digest() != "you: hello codex" {
		t.Errorf("exists = %v, digest = %q", c.exists(), c.digest())
	}
	transcriptPaths.Delete("x-7")
	if (manifestConvo(&session.Manifest{Agent: agent.Codex, SessionID: "x-7"})).exists() {
		t.Error("without the account, the default CODEX_HOME holds no such rollout")
	}
}

// TestHelperSessionsAreNoAgents: the onboarding and method-editor sessions run
// from inside the board, which can sit in an agent's tmux session; they must
// not carry its KOVAN_SESSION, or their hooks count as that agent's.
func TestHelperSessionsAreNoAgents(t *testing.T) {
	t.Setenv(sessionEnv, "kovan-x-1")
	cmd := aiEditCommand("/some/dir/soul.md")
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, sessionEnv+"=") {
			t.Errorf("method editor carries %s", kv)
		}
	}
	if len(cmd.Env) == 0 {
		t.Error("method editor env is unset, so it inherits KOVAN_SESSION")
	}
}

// TestComposeMethodUnloadableMode: a mode that no longer loads still leaves
// the agent its layers and task block.
func TestComposeMethodUnloadableMode(t *testing.T) {
	home, _ := methodHome(t)
	text, err := composeMethod(home, &session.Manifest{ID: "t", Title: "x", Repo: "kovan", TaskMode: "gone"}, mustDriver(agent.Codex))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Task") || !strings.Contains(text, "PROJECT NOTE") {
		t.Errorf("method with an unloadable mode =\n%s", text)
	}
}

// TestHooksWarning: an agent whose CLI has no kovan hooks runs with no gates,
// which must never pass in silence.
func TestHooksWarning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	g := &config.Global{}
	d := mustDriver(agent.Claude)
	if w := hooksWarning(g, d, ""); !strings.Contains(w, "kovan setup") {
		t.Errorf("no hooks = %q, want a warning", w)
	}
	if _, err := installHooks(d.HooksFile(d.DefaultHome()), d); err != nil {
		t.Fatal(err)
	}
	if w := hooksWarning(g, d, ""); w != "" {
		t.Errorf("hooks installed = %q, want none", w)
	}
}
