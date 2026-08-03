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
	if got := launchCommand("claude", "fix vfs", launchFresh, "", nil, "/notes", "sid"); got != "claude --add-dir '/notes' --session-id 'sid' -- 'fix vfs'" {
		t.Errorf("no account = %q", got)
	}
	got := launchCommand("claude", "fix vfs", launchFresh, "/x/tok", nil, "/notes", "sid")
	want := `CLAUDE_CODE_OAUTH_TOKEN="$(cat '/x/tok')" claude --add-dir '/notes' --session-id 'sid' -- 'fix vfs'`
	if got != want {
		t.Errorf("with account = %q, want %q", got, want)
	}
	if got := launchCommand("claude", "ignored", launchResume, "/x/tok", nil, "/notes", "sid"); !strings.HasSuffix(got, "claude --add-dir '/notes' --resume 'sid'") {
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

// TestAccountPicks pins the system entry: it heads the picker and maps to the
// empty account, which is what skips token injection so the agent inherits the
// machine's login (and with it the account's entitlements).
func TestAccountPicks(t *testing.T) {
	if got := accountPicks(nil); got != nil {
		t.Errorf("no configured accounts should hide the field, got %v", got)
	}
	picks := accountPicks([]string{"company", "personal"})
	want := []string{systemAccount, "company", "personal"}
	if len(picks) != len(want) {
		t.Fatalf("picks = %v, want %v", picks, want)
	}
	for i := range want {
		if picks[i] != want[i] {
			t.Fatalf("picks = %v, want %v", picks, want)
		}
	}
	if got := accountValue(systemAccount); got != "" {
		t.Errorf("system entry = %q, want empty (no injection)", got)
	}
	if got := accountValue("company"); got != "company" {
		t.Errorf("named account = %q, want company", got)
	}
	// An unset account lands on the system entry rather than a named one.
	if idx := indexOf(picks, ""); idx != 0 {
		t.Errorf("unset account index = %d, want 0 (system)", idx)
	}
}

// TestLaunchCommandAccountEnv pins the per-account environment: entries are
// sorted for a stable command and land ahead of the token, so an account whose
// token lacks its plan's entitlements can pin the model that restores them.
func TestLaunchCommandAccountEnv(t *testing.T) {
	env := map[string]string{
		"ZZ_LAST":                      "1",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-5[1m]",
	}
	got := launchCommand("claude", "fix vfs", launchFresh, "/x/tok", env, "/notes", "sid")
	want := `ANTHROPIC_DEFAULT_OPUS_MODEL='claude-opus-5[1m]' ZZ_LAST='1' ` +
		`CLAUDE_CODE_OAUTH_TOKEN="$(cat '/x/tok')" claude --add-dir '/notes' --session-id 'sid' -- 'fix vfs'`
	if got != want {
		t.Errorf("account env =\n%q\nwant\n%q", got, want)
	}
	// Env without an account still applies; no token is injected.
	solo := launchCommand("claude", "g", launchFresh, "", env, "/notes", "sid")
	if strings.Contains(solo, oauthEnvKey) {
		t.Errorf("no token file should inject no token env, got %q", solo)
	}
	if !strings.HasPrefix(solo, "ANTHROPIC_DEFAULT_OPUS_MODEL='claude-opus-5[1m]' ") {
		t.Errorf("env prefix missing, got %q", solo)
	}
}

// TestAccountEnvLookup: only a named account carries env; the system entry ("")
// launches clean so it inherits the machine's login untouched.
func TestAccountEnvLookup(t *testing.T) {
	g := &config.Global{Accounts: map[string]config.Account{
		"company": {TokenFile: "/x/tok", Env: map[string]string{"K": "v"}},
	}}
	if got := accountEnv(g, "company"); got["K"] != "v" {
		t.Errorf("company env = %v", got)
	}
	if got := accountEnv(g, ""); got != nil {
		t.Errorf("system account env = %v, want nil", got)
	}
	if got := accountEnv(g, "missing"); got != nil {
		t.Errorf("unknown account env = %v, want nil", got)
	}
}
