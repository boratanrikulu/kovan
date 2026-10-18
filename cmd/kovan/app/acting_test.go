package app

import (
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

func TestActingAgent(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	t.Setenv("TMUX_PANE", "")
	t.Chdir(t.TempDir())
	for _, id := range []string{"1", "2"} {
		if err := (&session.Manifest{ID: id, Repo: "a", Tmux: "kovan-a-" + id}).Write(); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv(sessionEnv, "kovan-a-1")
	if m, err := actingAgent(""); err != nil || m.Tmux != "kovan-a-1" {
		t.Errorf("from env = %v, %v; want kovan-a-1", m, err)
	}
	if m, err := actingAgent("1"); err != nil || m.Tmux != "kovan-a-1" {
		t.Errorf("--agent naming the caller = %v, %v; want kovan-a-1", m, err)
	}
	if _, err := actingAgent("2"); err == nil {
		t.Error("inside agent 1, --agent 2 must be refused")
	}

	// The same id in another repo: inside agent 1, --agent 1 still means itself.
	if err := (&session.Manifest{ID: "1", Repo: "b", Tmux: "kovan-b-1"}).Write(); err != nil {
		t.Fatal(err)
	}
	if m, err := actingAgent("1"); err != nil || m.Tmux != "kovan-a-1" {
		t.Errorf("--agent 1 with a twin in repo b = %v, %v; want the caller", m, err)
	}

	t.Setenv(sessionEnv, "")
	if m, err := actingAgent("2"); err != nil || m.Tmux != "kovan-a-2" {
		t.Errorf("--agent from a plain shell = %v, %v; want kovan-a-2", m, err)
	}
	if _, err := actingAgent(""); err == nil {
		t.Error("outside an agent with no --agent should error")
	}
	if _, err := actingAgent("missing"); err == nil {
		t.Error("an unknown id should error")
	}
}
