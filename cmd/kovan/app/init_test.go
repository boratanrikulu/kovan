package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo makes a throwaway git repo and cd's into it; skips if git is absent.
func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	return repo
}

func TestPrepareInit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	initRepo(t)

	repo, data, err := prepareInit("/work/agent", "flagacct")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(data.Repo) != filepath.Base(repo.Root) {
		t.Errorf("data.Repo = %q, want the repo root", data.Repo)
	}
	if !data.GlobalEmpty {
		t.Error("fresh KOVAN_HOME should report an empty global layer")
	}
	if data.Reference != "/work/agent" {
		t.Errorf("reference = %q", data.Reference)
	}
	if data.Account != "flagacct" {
		t.Errorf("account = %q, want the flag account", data.Account)
	}
	if !strings.HasSuffix(data.ClaudeMD, filepath.Join(".claude", "CLAUDE.md")) {
		t.Errorf("claudeMD = %q", data.ClaudeMD)
	}
	if _, err := os.Stat(filepath.Join(home, "method", "global")); err != nil {
		t.Errorf("prepareInit did not scaffold ~/.kovan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
		t.Errorf("prepareInit did not scaffold config.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, ".kovan.yaml")); err != nil {
		t.Errorf("prepareInit did not scaffold .kovan.yaml: %v", err)
	}
}

func TestRunInitLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", t.TempDir()) // isolate ~/.claude — runInit now installs hooks
	repo := initRepo(t)

	// A fake agent that records its cwd and args instead of launching Claude.
	fake := filepath.Join(t.TempDir(), "fakeclaude")
	record := filepath.Join(t.TempDir(), "record")
	script := "#!/usr/bin/env bash\npwd > " + record + "\nfor a in \"$@\"; do echo \"$a\" >> " + record + "; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agent: "+fake+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit("/work/agent", ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	// The prompt is handed over as a file, not a giant positional arg.
	for _, want := range []string{"--add-dir", home, "/work/agent", "Read " + home, "follow it"} {
		if !strings.Contains(out, want) {
			t.Errorf("launch missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "## This repo") {
		t.Error("the full prompt should be in a file, not passed as an argument")
	}
	// "--" must precede the prompt, or variadic --add-dir swallows it.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	sep, prompt := -1, -1
	for i, l := range lines {
		if l == "--" {
			sep = i
		}
		if strings.HasPrefix(l, "Read ") {
			prompt = i
		}
	}
	if sep == -1 || prompt == -1 || sep >= prompt {
		t.Errorf("expected \"--\" immediately before the prompt arg:\n%s", out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; filepath.Base(first) != filepath.Base(repo) {
		t.Errorf("launched cwd = %q, want the repo root", first)
	}
}

func TestRunInitInstallsHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	claudeHome := t.TempDir()
	t.Setenv("HOME", claudeHome) // claudeDir → claudeHome/.claude
	initRepo(t)

	fake := filepath.Join(t.TempDir(), "fakeclaude")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agent: "+fake+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := filepath.Join(claudeHome, ".claude", "settings.json")
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("settings.json should be absent before init: %v", err)
	}
	if err := runInit("", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(settings); err != nil {
		t.Errorf("init did not install the hooks: %v", err)
	}

	// Re-running is a no-op: still exactly one gate-run hook per event.
	if err := runInit("", ""); err != nil {
		t.Fatal(err)
	}
	hooks := readHooks(t, settings)
	for _, ev := range gateHookEvents {
		arr, _ := hooks[ev].([]any)
		n := 0
		for _, g := range arr {
			gm, _ := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				if cmd, _ := hm["command"].(string); isGateRun(cmd) {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("%s has %d gate-run hooks after re-init, want 1 (idempotent)", ev, n)
		}
	}
}

func TestRunInitInjectsAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", t.TempDir())
	initRepo(t)

	// A fake agent that records its args and the token env it received.
	fake := filepath.Join(t.TempDir(), "fakeclaude")
	record := filepath.Join(t.TempDir(), "record")
	script := "#!/usr/bin/env bash\necho \"token=$CLAUDE_CODE_OAUTH_TOKEN\" > " + record + "\nfor a in \"$@\"; do echo \"$a\" >> " + record + "; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(home, "tokens-personal")
	if err := os.WriteFile(tokenFile, []byte("sk-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "agent: " + fake + "\naccounts:\n  personal: {token_file: " + tokenFile + "}\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit("", "personal"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, "token=sk-secret") {
		t.Errorf("agent did not receive the account token:\n%s", out)
	}
	// The token reaches the agent through the env only, never as an argument.
	if strings.Contains(strings.SplitN(out, "\n", 2)[1], "sk-secret") {
		t.Errorf("token value leaked into argv:\n%s", out)
	}
	for _, want := range []string{"--add-dir", "follow it"} {
		if !strings.Contains(out, want) {
			t.Errorf("launch missing %q:\n%s", want, out)
		}
	}
}

func TestRunInitBadTokenFileDoesNotLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", t.TempDir())
	initRepo(t)

	fake := filepath.Join(t.TempDir(), "fakeclaude")
	record := filepath.Join(t.TempDir(), "record")
	script := "#!/usr/bin/env bash\ntouch " + record + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "agent: " + fake + "\naccounts:\n  personal: {token_file: " + filepath.Join(home, "absent") + "}\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runInit("", "personal")
	if err == nil || !strings.Contains(err.Error(), "setup-token") {
		t.Fatalf("err = %v, want a missing-token-file error", err)
	}
	if _, serr := os.Stat(record); !os.IsNotExist(serr) {
		t.Error("agent was launched despite the bad token file")
	}
}

func TestInitLaunch(t *testing.T) {
	plain := initLaunch("claude", []string{"--add-dir", "/notes"}, "")
	if filepath.Base(plain.Path) != "claude" || len(plain.Args) != 3 {
		t.Errorf("no token file should exec the agent directly, got %v", plain.Args)
	}
	cmd := initLaunch("claude", []string{"--add-dir", "/notes"}, "/x/tok")
	script := cmd.Args[len(cmd.Args)-1]
	want := `CLAUDE_CODE_OAUTH_TOKEN="$(cat '/x/tok')" exec 'claude' '--add-dir' '/notes'`
	if filepath.Base(cmd.Path) != "sh" || script != want {
		t.Errorf("with token file = %q, want %q via sh", script, want)
	}
}

func TestWriteOnboardPrompt(t *testing.T) {
	home := t.TempDir()
	path, err := writeOnboardPrompt(home, "the brief\nfollow it")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != home || !strings.HasSuffix(path, ".md") {
		t.Errorf("prompt file = %q, want a .md under %q", path, home)
	}
	if got, _ := os.ReadFile(path); string(got) != "the brief\nfollow it" {
		t.Errorf("prompt file content = %q", got)
	}
}
