package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
)

func TestResolveAccount(t *testing.T) {
	cases := []struct {
		flag, repo, global, want string
	}{
		{"flagacct", "repoacct", "globalacct", "flagacct"},
		{"", "repoacct", "globalacct", "repoacct"},
		{"", "", "globalacct", "globalacct"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := resolveAccount(c.flag, c.repo, c.global); got != c.want {
			t.Errorf("resolveAccount(%q,%q,%q) = %q, want %q", c.flag, c.repo, c.global, got, c.want)
		}
	}
}

func TestAccountTokenFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "personal")
	if err := os.WriteFile(good, []byte("sk-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("sk-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	global := &config.Global{Accounts: map[string]config.Account{
		"personal": {TokenFile: good},
		"loose":    {TokenFile: loose},
		"gone":     {TokenFile: filepath.Join(dir, "absent")},
	}}

	if path, err := accountTokenFile(global, ""); err != nil || path != "" {
		t.Errorf("empty account = (%q, %v), want (\"\", nil)", path, err)
	}
	if path, err := accountTokenFile(global, "personal"); err != nil || path != good {
		t.Errorf("personal = (%q, %v), want (%q, nil)", path, err, good)
	}
	if _, err := accountTokenFile(global, "unknown"); err == nil {
		t.Error("unknown account should error")
	}
	if _, err := accountTokenFile(global, "gone"); err == nil || !strings.Contains(err.Error(), "setup-token") {
		t.Errorf("missing file error = %v, want one mentioning setup-token", err)
	}
	if _, err := accountTokenFile(global, "loose"); err == nil || !strings.Contains(err.Error(), "readable") {
		t.Errorf("loose perms error = %v, want one about readability", err)
	}
}

func TestLaunchCommand(t *testing.T) {
	if got := launchCommand("claude", "fix vfs", launchFresh, "", "/notes", "sid"); got != "claude --add-dir '/notes' --session-id 'sid' -- 'fix vfs'" {
		t.Errorf("no account = %q", got)
	}
	got := launchCommand("claude", "fix vfs", launchFresh, "/x/tok", "/notes", "sid")
	want := `CLAUDE_CODE_OAUTH_TOKEN="$(cat '/x/tok')" claude --add-dir '/notes' --session-id 'sid' -- 'fix vfs'`
	if got != want {
		t.Errorf("with account = %q, want %q", got, want)
	}
	if got := launchCommand("claude", "ignored", launchResume, "/x/tok", "/notes", "sid"); !strings.HasSuffix(got, "claude --add-dir '/notes' --resume 'sid'") {
		t.Errorf("resume = %q", got)
	}
	// The token value itself must never appear — only the path.
	if strings.Contains(got, "sk-") {
		t.Error("launch command leaked a token")
	}
}

func TestRunnerSessionInjectsAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	tokenDir := filepath.Join(home, "tokens")
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(tokenDir, "personal")
	if err := os.WriteFile(tokenFile, []byte("sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := "accounts:\n  personal: {token_file: " + tokenFile + "}\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(write), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &session.Manifest{Tmux: "t", Worktree: "/wt", Agent: "claude", Title: "g", Account: "personal"}
	sess, err := runnerSession(m, launchFresh, "fix vfs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.Cmd, oauthEnvKey+`="$(cat `) || !strings.Contains(sess.Cmd, tokenFile) {
		t.Errorf("cmd should read the token file at launch, got %q", sess.Cmd)
	}
	if strings.Contains(sess.Cmd, "sk-secret") {
		t.Error("the token value leaked into the launch command")
	}

	// No account → no token injection.
	plain, err := runnerSession(&session.Manifest{Tmux: "t", Worktree: "/wt", Agent: "claude", Title: "g"}, launchFresh, "g")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.Cmd, oauthEnvKey) {
		t.Errorf("no account should inject no token env, got %q", plain.Cmd)
	}
}
