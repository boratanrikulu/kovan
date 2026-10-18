package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/session"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// methodHome is a kovan home with a file in every layer, and a HOME with no
// ~/.claude/CLAUDE.md, so no layer is loaded natively.
func methodHome(t *testing.T) (home string, files map[string]string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", t.TempDir())
	files = map[string]string{
		"global":   filepath.Join(home, "method", "global", "rules.md"),
		"imported": filepath.Join(home, "method", "global", "extra", "deep.md"),
		"account":  filepath.Join(home, "method", "accounts", "personal", "voice.md"),
		"project":  filepath.Join(home, "projects", "kovan", "notes.md"),
		"other":    filepath.Join(home, "projects", "cloud", "cloud.md"),
	}
	writeFile(t, files["global"], "GLOBAL RULE\n@extra/deep.md\n")
	writeFile(t, files["imported"], "DEEP RULE\n")
	writeFile(t, files["account"], "ACCOUNT VOICE\n")
	writeFile(t, files["project"], "PROJECT NOTE\n")
	writeFile(t, files["other"], "CLOUD NOTE\n")
	return home, files
}

func TestComposeMethod(t *testing.T) {
	home, _ := methodHome(t)
	m := &session.Manifest{ID: "TASK-1", Title: "fix vfs", Repo: "kovan", Worktree: "/w/kovan-1",
		Account: "personal", TaskMode: "review", Extra: []session.Workspace{{Repo: "cloud", Worktree: "/w/cloud-1", Branch: "b"}}}
	text, err := composeMethod(home, m, mustDriver(agent.Codex))
	if err != nil {
		t.Fatal(err)
	}
	// The task block first, then every layer in order, the mode's method last.
	order := []string{
		"## Task — TASK-1: fix vfs  (mode: review)",
		filepath.Join(home, "projects", "kovan", "works", "TASK-1", "context.md"),
		"## Workspaces",
		"GLOBAL RULE", "DEEP RULE", "ACCOUNT VOICE", "PROJECT NOTE", "CLOUD NOTE",
		filepath.Join(home, "modes", "review", "method.md"),
	}
	at := 0
	for _, want := range order {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("method missing %q after offset %d:\n%s", want, at, text)
		}
		at += i
	}
}

// TestComposeMethodQuick: a throwaway task carries the layers, not a protocol.
func TestComposeMethodQuick(t *testing.T) {
	home, _ := methodHome(t)
	m := &session.Manifest{ID: "q", Title: "quick one", Repo: "kovan", Quick: true}
	text, err := composeMethod(home, m, mustDriver(agent.Claude))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "## Task") || !strings.Contains(text, "GLOBAL RULE") {
		t.Errorf("quick method = \n%s\nwant layers without the task block", text)
	}
}

// TestComposeMethodSkipsLinkedGlobal: claude already loads what
// ~/.claude/CLAUDE.md imports, so composing it again would load it twice.
// Codex has no such file and gets it composed.
func TestComposeMethodSkipsLinkedGlobal(t *testing.T) {
	home, files := methodHome(t)
	writeFile(t, filepath.Join(os.Getenv("HOME"), ".claude", "CLAUDE.md"), "@"+files["global"]+"\n")
	m := &session.Manifest{ID: "t", Title: "x", Repo: "kovan"}
	claude, _ := composeMethod(home, m, mustDriver(agent.Claude))
	if strings.Contains(claude, "GLOBAL RULE") || strings.Contains(claude, "DEEP RULE") {
		t.Errorf("claude method repeats the linked global layer:\n%s", claude)
	}
	if !strings.Contains(claude, "PROJECT NOTE") {
		t.Errorf("claude method lost the unlinked layers:\n%s", claude)
	}
	codex, _ := composeMethod(home, m, mustDriver(agent.Codex))
	if !strings.Contains(codex, "GLOBAL RULE") || !strings.Contains(codex, "DEEP RULE") {
		t.Errorf("codex method is missing the global layer:\n%s", codex)
	}
}

// TestWriteMethodPicksUpEdits: the file is recomposed on every launch, so a
// layer edit reaches the agent on its next wake.
func TestWriteMethodPicksUpEdits(t *testing.T) {
	_, files := methodHome(t)
	m := &session.Manifest{ID: "t", Title: "x", Repo: "kovan", Tmux: "kovan-kovan-t"}
	path, err := writeMethod(m, mustDriver(agent.Claude))
	if err != nil || path == "" {
		t.Fatalf("writeMethod = %q, %v", path, err)
	}
	writeFile(t, files["project"], "EDITED NOTE\n")
	if _, err := writeMethod(m, mustDriver(agent.Claude)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "EDITED NOTE") || strings.Contains(string(data), "PROJECT NOTE") {
		t.Errorf("relaunch did not pick up the edit:\n%s", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("method file mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestRunnerSessionLoadsMethod: the launch hands the composed file to the CLI,
// and a kovan-managed CLAUDE.local.md block for this agent goes, so the
// method is not loaded twice. Another tab's block is not ours to strip.
func TestRunnerSessionLoadsMethod(t *testing.T) {
	home, _ := methodHome(t)
	wt := t.TempDir()
	legacy := upsertManagedBlock("my own notes\n", taskHeading("t")+" old\n@/old/import.md")
	writeFile(t, filepath.Join(wt, claudeLocalFile), legacy)

	m := &session.Manifest{ID: "t", Title: "x", Repo: "kovan", Tmux: "kovan-kovan-t", Worktree: wt, Agent: agent.Claude}
	sess, err := runnerSession(m, launchFresh, "go")
	if err != nil {
		t.Fatal(err)
	}
	file := methodRunFile(home, m.Tmux)
	if !strings.Contains(sess.Cmd, `--append-system-prompt "$(cat '`+file+`')" --system-prompt-snapshot off`) {
		t.Errorf("launch does not load the method:\n%s", sess.Cmd)
	}
	data, _ := os.ReadFile(filepath.Join(wt, claudeLocalFile))
	if strings.Contains(string(data), managedStart) || !strings.Contains(string(data), "my own notes") {
		t.Errorf("CLAUDE.local.md after launch = %q, want only the user's notes", data)
	}

	other := upsertManagedBlock("", taskHeading("host")+" host's\n")
	writeFile(t, filepath.Join(wt, claudeLocalFile), other)
	if _, err := runnerSession(m, launchResume, ""); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(wt, claudeLocalFile)); string(data) != other {
		t.Errorf("another tab's block was touched: %q", data)
	}
}
