package app

import (
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"fix the vfs handler", "fix-the-vfs-handler"},
		{"Add OAuth2 support!", "Add-OAuth2-support"},
		{"  leading/trailing  ", "leadingtrailing"},
		{"already-clean_slug", "already-clean_slug"},
		{"emoji 🐝 dropped", "emoji--dropped"},
		{"--edges--", "edges"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderBranch(t *testing.T) {
	got := renderBranch("feat/{author}_{id}_{slug}", "bora", "TASK-1", "fix-vfs")
	want := "feat/bora_TASK-1_fix-vfs"
	if got != want {
		t.Errorf("renderBranch = %q, want %q", got, want)
	}
}

func TestRenderBranchCustomTemplate(t *testing.T) {
	got := renderBranch("{id}/{slug}", "ignored", "T-9", "demo")
	if got != "T-9/demo" {
		t.Errorf("renderBranch = %q, want T-9/demo", got)
	}
}

func TestSessionNameStable(t *testing.T) {
	if got := sessionName("myrepo", "TASK-1"); got != "kovan-myrepo-TASK-1" {
		t.Errorf("sessionName = %q, want kovan-myrepo-TASK-1", got)
	}
}

func TestRunnerSessionPassesSessionID(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	m := &session.Manifest{Tmux: "t", Worktree: "/wt", Agent: "claude", Title: "g", SessionID: "sid-xyz"}
	sess, err := runnerSession(m, launchFresh, "go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sess.Cmd, "--session-id 'sid-xyz'") {
		t.Errorf("launch cmd missing the session id: %q", sess.Cmd)
	}
}

func TestRunnerSessionWindowTitle(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	m := &session.Manifest{Tmux: "t", Worktree: "/wt", Agent: "claude", ID: "TASK-1", Title: "fix vfs"}
	sess, err := runnerSession(m, launchFresh, "go")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Title != "TASK-1: fix vfs" {
		t.Errorf("window title = %q, want %q (id: title)", sess.Title, "TASK-1: fix vfs")
	}
}

func TestStatusBarOptions(t *testing.T) {
	m := &session.Manifest{ID: "TASK-1", Repo: "kovan", Branch: "feat/x", Title: "fix the vfs handler"}
	opts := strings.Join(statusBarOptions(m), "\n")
	for _, want := range []string{"TASK-1", " kovan ", "feat/x", "fix the vfs handler", "colour" + string(colorBrand), "^b d → kovan", "^b k apps"} {
		if !strings.Contains(opts, want) {
			t.Errorf("status options missing %q:\n%s", want, opts)
		}
	}

	// An empty title leaves no stray trailing separator on the right side.
	right := joinNonEmpty(" · ", "kovan", "feat/y", "")
	if right != "kovan · feat/y" {
		t.Errorf("empty title produced %q, want %q (no trailing separator)", right, "kovan · feat/y")
	}
}

func TestTmuxEscape(t *testing.T) {
	if got := tmuxEscape("feat/#42"); got != "feat/##42" {
		t.Errorf("tmuxEscape = %q, want feat/##42", got)
	}
}

func TestAgentCommand(t *testing.T) {
	// Fresh: --add-dir grants the task-doc dir, --session-id pins the transcript,
	// "--" guards the positional prompt.
	if got := agentCommand("claude", "fix the vfs handler", launchFresh, "/notes", "sid-1"); got != `claude --add-dir '/notes' --session-id 'sid-1' -- 'fix the vfs handler'` {
		t.Errorf("fresh = %q", got)
	}
	// Resume reattaches the tab's own conversation by id — tabs share a cwd, so
	// --continue would grab a sibling's conversation (and no prompt).
	if got := agentCommand("claude", "ignored on resume", launchResume, "/notes", "sid-1"); got != "claude --add-dir '/notes' --resume 'sid-1'" {
		t.Errorf("resume = %q", got)
	}
	// Resume without a recorded session id falls back to --continue.
	if got := agentCommand("claude", "ignored on resume", launchResume, "/notes", ""); got != "claude --add-dir '/notes' --continue" {
		t.Errorf("resume without id = %q", got)
	}
	// No add-dir / no session id → just the guarded prompt.
	if got := agentCommand("claude", "g", launchFresh, "", ""); got != "claude -- 'g'" {
		t.Errorf("minimal = %q", got)
	}
}

func TestDecideOpen(t *testing.T) {
	cases := []struct {
		alive, worktreePresent bool
		want                   openAction
	}{
		{true, true, actionOpen},
		{true, false, actionOpen},
		{false, true, actionWake},
		{false, false, actionMissing},
	}
	for _, c := range cases {
		if got := decideOpen(c.alive, c.worktreePresent); got != c.want {
			t.Errorf("decideOpen(%v, %v) = %v, want %v", c.alive, c.worktreePresent, got, c.want)
		}
	}
}

func TestAliasResolution(t *testing.T) {
	cases := map[string]string{
		"go":     "start",
		"attach": "open",
		"rm":     "remove",
	}
	for alias, want := range cases {
		cmd, _, err := rootCmd.Find([]string{alias})
		if err != nil {
			t.Errorf("Find(%q): %v", alias, err)
			continue
		}
		if cmd.Name() != want {
			t.Errorf("alias %q resolved to %q, want %q", alias, cmd.Name(), want)
		}
	}
}

func TestResolveID(t *testing.T) {
	if got, err := resolveID("TASK-101", ""); err != nil || got != "TASK-101" {
		t.Errorf("given id should pass through: got %q, err %v", got, err)
	}
	if got, err := resolveID("TASK-101", "^TASK-[0-9]+$"); err != nil || got != "TASK-101" {
		t.Errorf("matching id should pass through: got %q, err %v", got, err)
	}
	if _, err := resolveID("nope", "^TASK-[0-9]+$"); err == nil {
		t.Error("a typed id that breaks the pattern should error")
	}
	// A blank id always generates, even when the repo sets an id_pattern — leaving
	// it blank opts into an auto id.
	for _, pattern := range []string{"", "^TASK-[0-9]+$"} {
		got, err := resolveID("", pattern)
		if err != nil {
			t.Fatalf("blank id (pattern %q) should generate one: %v", pattern, err)
		}
		if len(got) != 4 {
			t.Errorf("generated id = %q, want 4 chars", got)
		}
	}
}

func TestGenerateIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, err := generateID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 4 {
			t.Fatalf("id %q is not 4 chars", id)
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("id %q has a non-hex char %q", id, c)
			}
		}
		seen[id] = true
	}
	if len(seen) < 40 {
		t.Errorf("only %d distinct ids in 50 draws — randomness looks weak", len(seen))
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		flag, repo, global, want string
	}{
		{"review", "analyze", "write", "review"}, // flag wins
		{"", "analyze", "write", "analyze"},      // repo default next
		{"", "", "write", "write"},               // global default next
		{"", "", "", "code"},                     // built-in default
	}
	for _, c := range cases {
		if got := resolveMode(c.flag, c.repo, c.global); got != c.want {
			t.Errorf("resolveMode(%q,%q,%q) = %q, want %q", c.flag, c.repo, c.global, got, c.want)
		}
	}
}
