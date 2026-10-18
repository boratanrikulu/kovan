package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boratanrikulu/kovan/internal/session"
)

// fakeApp writes a script that records its arguments (one per line) to out, so a
// test can assert how launchApp built the argv.
func fakeApp(t *testing.T, dir, out string) string {
	t.Helper()
	script := filepath.Join(dir, "fake")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + out + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// argsAfter waits for the detached app to write out, then returns its recorded
// arguments.
func argsAfter(t *testing.T, out string) []string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if data, err := os.ReadFile(out); err == nil && len(data) > 0 {
			return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("app never recorded its arguments to %s", out)
	return nil
}

func TestLaunchAppAppendsPath(t *testing.T) {
	wt := t.TempDir()
	out := filepath.Join(wt, "args.txt")
	script := fakeApp(t, wt, out)

	if err := launchApp(script, wt); err != nil {
		t.Fatalf("launchApp: %v", err)
	}
	if got := argsAfter(t, out); len(got) != 1 || got[0] != wt {
		t.Errorf("args = %v, want [%q]", got, wt)
	}
}

func TestLaunchAppSubstitutesToken(t *testing.T) {
	wt := t.TempDir()
	out := filepath.Join(wt, "args.txt")
	script := fakeApp(t, wt, out)

	// "{path}" is replaced in place; the path is not also appended.
	if err := launchApp(script+" --flag {path}", wt); err != nil {
		t.Fatalf("launchApp: %v", err)
	}
	got := argsAfter(t, out)
	want := []string{"--flag", wt}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestLaunchAppEmptyCommand(t *testing.T) {
	if err := launchApp("   ", t.TempDir()); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}

// writeTab writes a manifest for a tab and creates its durable task-doc dir, so
// notesDirFor's existence guard passes. It returns the task-doc dir.
func writeTab(t *testing.T, home, repo, id, tmux, worktree string) string {
	t.Helper()
	m := &session.Manifest{ID: id, Repo: repo, Tmux: tmux, Worktree: worktree}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(home, "projects", repo, "works", id)
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	return notes
}

func TestNotesDirForResolvesPerTab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	wt := t.TempDir()

	// Two tabs share one worktree but have their own id, so their task-doc dirs
	// differ — the property that forces resolution by tmux name, not worktree.
	aDir := writeTab(t, home, "repoA", "aa", "kovan-repoA-aa", wt)
	bDir := writeTab(t, home, "repoA", "bb", "kovan-repoA-bb", wt)

	if got, err := notesDirFor("kovan-repoA-aa", ""); err != nil || got != aDir {
		t.Errorf("by session aa = %q (err %v), want %q", got, err, aDir)
	}
	if got, err := notesDirFor("kovan-repoA-bb", ""); err != nil || got != bDir {
		t.Errorf("by session bb = %q (err %v), want %q", got, err, bDir)
	}
	if aDir == bDir {
		t.Fatal("sibling tabs must not share a task-doc dir")
	}
}

func TestNotesDirForFromWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	wt := t.TempDir()
	dir := writeTab(t, home, "repoA", "aa", "kovan-repoA-aa", wt)

	if got, err := notesDirFor("", filepath.Join(wt, "sub")); err != nil || got != dir {
		t.Errorf("from worktree = %q (err %v), want %q", got, err, dir)
	}
}

func TestNotesDirForErrors(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())

	if _, err := notesDirFor("missing-session", ""); err == nil {
		t.Error("unknown session should error")
	}
	if _, err := notesDirFor("", t.TempDir()); err == nil {
		t.Error("a non-kovan dir should error")
	}

	// A known tab whose task-doc dir was never created (e.g. cleaned up) errors
	// clearly instead of launching the editor on a missing path.
	m := &session.Manifest{ID: "cc", Repo: "repoA", Tmux: "kovan-repoA-cc", Worktree: t.TempDir()}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	_, err := notesDirFor("kovan-repoA-cc", "")
	if err == nil || !strings.Contains(err.Error(), "no task docs") {
		t.Errorf("missing task docs error = %v, want a 'no task docs' message", err)
	}
}

func TestWorktreeFromExtraWorkspace(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	primary, extra := t.TempDir(), t.TempDir()
	m := &session.Manifest{ID: "aa", Repo: "agent", Tmux: "kovan-agent-aa", Worktree: primary,
		Extra: []session.Workspace{{Repo: "cloud", Worktree: extra}}}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	// An app opened on a path in the agent's cloud worktree opens that worktree,
	// not the agent's primary one.
	if got := worktreeFrom(filepath.Join(extra, "web")); got != extra {
		t.Errorf("worktreeFrom(extra/web) = %q, want %q", got, extra)
	}
	if got := worktreeFrom(filepath.Join(primary, "pkg")); got != primary {
		t.Errorf("worktreeFrom(primary/pkg) = %q, want %q", got, primary)
	}
}

func TestAppMenuArgsSingleWorkspace(t *testing.T) {
	m := &session.Manifest{ID: "aa", Repo: "agent", Tmux: "kovan-agent-aa", Worktree: "/w/agent-aa"}
	got := strings.Join(appMenuArgs(m, "/dev/ttys001", "%3"), "\n")
	for _, want := range []string{
		"-c\n/dev/ttys001",
		"editor\ne\nrun-shell \"kovan app editor --session 'kovan-agent-aa' --workspace 0\"",
		"merge\ns\n",
		"terminal\nt\n",
		"notes\nw\nrun-shell \"kovan app notes --session 'kovan-agent-aa'\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("menu args missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/w/agent-aa") {
		t.Errorf("a path must never reach a menu command (tmux expands it as a format):\n%s", got)
	}
}

func TestAppMenuArgsExtraWorkspaces(t *testing.T) {
	m := &session.Manifest{ID: "aa", Repo: "agent", Tmux: "kovan-agent-aa", Worktree: "/w/agent-aa",
		Extra: []session.Workspace{{Repo: "cloud", Worktree: "/w/cloud-aa"}, {Repo: "docs#1", Worktree: "/w/docs-aa"}}}
	got := strings.Join(appMenuArgs(m, "c", "%3"), "\n")
	for _, want := range []string{
		"editor · agent\ne\nrun-shell \"kovan app editor --session 'kovan-agent-aa' --workspace 0\"",
		"editor · cloud\nE\nrun-shell \"kovan app editor --session 'kovan-agent-aa' --workspace 1\"",
		"terminal · cloud\nT\n",
		"merge · docs##1\n\nrun-shell \"kovan app merge --session 'kovan-agent-aa' --workspace 2\"",
		"notes\nw\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("menu args missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "notes\n") != 1 {
		t.Errorf("notes is per agent, not per repo; want one item:\n%s", got)
	}
}

func TestAppTargetByWorkspaceIndex(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	m := &session.Manifest{ID: "aa", Repo: "agent", Tmux: "kovan-agent-aa", Worktree: "/w/agent-aa",
		Extra: []session.Workspace{{Repo: "cloud", Worktree: "/w/cloud-aa"}}}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	appSession, appWorkspace = "kovan-agent-aa", 1
	t.Cleanup(func() { appSession, appWorkspace = "", -1 })
	if got, err := appTarget([]string{"editor"}); err != nil || got != "/w/cloud-aa" {
		t.Errorf("appTarget = %q, %v; want the cloud worktree", got, err)
	}
	appWorkspace = 2
	if _, err := appTarget([]string{"editor"}); err == nil {
		t.Error("an index past the workspaces should error")
	}
}

func TestTmuxCommandArgEscapes(t *testing.T) {
	// tmux expands $VAR and \ inside double quotes, and a menu command is
	// expanded as a format twice (when the item is chosen, then by run-shell),
	// so a path carrying them must reach the shell literally.
	got := tmuxShellCommand(`kovan app editor '/w/a"$HOME\#1'`)
	want := `run-shell "kovan app editor '/w/a\"\$HOME\\####1'"`
	if got != want {
		t.Errorf("tmuxShellCommand = %s, want %s", got, want)
	}
}

func TestAppMenuArgsOutsideAgent(t *testing.T) {
	got := strings.Join(appMenuArgs(&session.Manifest{}, "c", "%3"), "\n")
	if !strings.Contains(got, "kovan app editor --pane '%3'") || strings.Contains(got, "notes") {
		t.Errorf("a non-agent pane gets the worktree apps and no notes:\n%s", got)
	}
}
