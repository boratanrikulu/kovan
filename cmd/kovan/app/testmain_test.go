package app

import (
	"os"
	"os/exec"
	"testing"
)

// TestMain forces KOVAN_HOME to a throwaway dir when the caller didn't set
// one, so no test can ever read or prune the real ~/.kovan session index.
// CODEX_HOME always gets one: a test that installs hooks would otherwise write
// them into the real ~/.codex once codex is on PATH. tmux gets a server of its
// own (TMUX_TMPDIR, with TMUX unset so no command follows it to the outer
// one), so tests that start agents never touch the user's sessions or key
// bindings.
func TestMain(m *testing.M) {
	var temps []string
	tempEnv := func(key, pattern string) string {
		dir, err := os.MkdirTemp("", pattern)
		if err != nil {
			panic(err)
		}
		os.Setenv(key, dir)
		temps = append(temps, dir)
		return dir
	}
	tempEnv("CODEX_HOME", "kovan-test-codex-")
	if os.Getenv("KOVAN_HOME") == "" {
		tempEnv("KOVAN_HOME", "kovan-test-home-")
	}
	for _, k := range []string{"TMUX", "TMUX_PANE", sessionEnv} {
		os.Unsetenv(k)
	}
	tmuxDir := tempEnv("TMUX_TMPDIR", "kovan-test-tmux-")
	code := m.Run()
	if os.Getenv("TMUX_TMPDIR") == tmuxDir { // never the user's server
		_ = exec.Command("tmux", "kill-server").Run()
	}
	for _, d := range temps {
		os.RemoveAll(d)
	}
	os.Exit(code)
}
