package git

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
		{"branch", "feat/x"},
		{"branch", "release"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	got := (&Repo{Root: root}).Branches()
	want := map[string]bool{"feat/x": true, "main": true, "release": true}
	if len(got) != 3 {
		t.Fatalf("Branches() = %v, want the 3 local branches", got)
	}
	for _, b := range got {
		if !want[b] {
			t.Errorf("unexpected branch %q in %v", b, got)
		}
	}
	// Sorted.
	if got[0] != "feat/x" || got[1] != "main" || got[2] != "release" {
		t.Errorf("Branches() not sorted: %v", got)
	}
}

func TestCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
		{"branch", "feat/x"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	repo := &Repo{Root: root}
	if err := repo.Checkout("feat/x"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if b, err := repo.CurrentBranch(root); err != nil || b != "feat/x" {
		t.Fatalf("CurrentBranch = %q, %v; want feat/x", b, err)
	}
	if err := repo.Checkout("does-not-exist"); err == nil {
		t.Error("Checkout of a missing branch should error")
	}
}

// gitT runs a git command in dir, failing the test on error.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitOut runs a git command in dir and returns trimmed stdout, failing on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// upstreamClone builds an upstream repo with one commit on master and a clone of
// it, returning the upstream and clone paths.
func upstreamClone(t *testing.T) (up, local string) {
	t.Helper()
	root := t.TempDir()
	up = filepath.Join(root, "up")
	gitT(t, root, "init", "-q", "-b", "master", "up")
	gitT(t, up, "config", "user.name", "t")
	gitT(t, up, "config", "user.email", "t@t")
	gitT(t, up, "commit", "-q", "--allow-empty", "-m", "c1")
	gitT(t, root, "clone", "-q", "up", "local")
	local = filepath.Join(root, "local")
	gitT(t, local, "config", "user.name", "t")
	gitT(t, local, "config", "user.email", "t@t")
	return up, local
}

func TestFetchBaseForksOffLatest(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	up, local := upstreamClone(t)
	gitT(t, up, "commit", "-q", "--allow-empty", "-m", "c2") // advance upstream past the clone
	want := gitOut(t, up, "rev-parse", "master")

	repo := &Repo{Root: local}
	ref, err := repo.FetchBase("master")
	if err != nil {
		t.Fatalf("FetchBase: %v", err)
	}
	if ref != "origin/master" {
		t.Fatalf("forkRef = %q, want origin/master", ref)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	if err := repo.WorktreeAdd(wt, "feat/x", ref, true); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	if got := gitOut(t, wt, "rev-parse", "HEAD"); got != want {
		t.Errorf("worktree HEAD = %q, want upstream tip %q", got, want)
	}
	// The new branch must not track the base, or a bare push would target it.
	if out, err := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "@{upstream}").CombinedOutput(); err == nil {
		t.Errorf("feat/x should have no upstream, got %q", strings.TrimSpace(string(out)))
	}
}

func TestFetchBaseNoOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	gitT(t, root, "init", "-q", "-b", "master")
	gitT(t, root, "config", "user.name", "t")
	gitT(t, root, "config", "user.email", "t@t")
	gitT(t, root, "commit", "-q", "--allow-empty", "-m", "c1")

	ref, err := (&Repo{Root: root}).FetchBase("master")
	if err != nil {
		t.Fatalf("FetchBase with no origin should not error: %v", err)
	}
	if ref != "master" {
		t.Errorf("forkRef = %q, want local master", ref)
	}
}

func TestFetchBaseLocalAhead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	up, local := upstreamClone(t)
	gitT(t, up, "commit", "-q", "--allow-empty", "-m", "c2")       // upstream advances
	gitT(t, local, "commit", "-q", "--allow-empty", "-m", "local") // local master diverges

	ref, err := (&Repo{Root: local}).FetchBase("master")
	if err != nil {
		t.Fatalf("FetchBase: %v", err)
	}
	if ref != "master" {
		t.Errorf("forkRef = %q, want local master (diverged: keep local commits)", ref)
	}
}

func TestParseWorktrees(t *testing.T) {
	out := "worktree /home/bora/repo\n" +
		"HEAD abc123\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /home/bora/repo-TASK-1\n" +
		"HEAD def456\n" +
		"branch refs/heads/feat/bora_TASK-1_fix\n" +
		"\n"

	got := parseWorktrees(out)
	if len(got) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(got))
	}
	if got[0].Path != "/home/bora/repo" || got[0].Branch != "main" || got[0].Head != "abc123" {
		t.Errorf("entry 0 wrong: %+v", got[0])
	}
	if got[1].Branch != "feat/bora_TASK-1_fix" {
		t.Errorf("entry 1 branch = %q", got[1].Branch)
	}
}

func TestParseWorktreesDetached(t *testing.T) {
	// A detached worktree has no branch line and ends the stream without a
	// trailing blank line.
	out := "worktree /home/bora/repo\nHEAD abc123\ndetached"
	got := parseWorktrees(out)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Branch != "" {
		t.Errorf("detached worktree should have empty branch, got %q", got[0].Branch)
	}
}

func TestDirtyFromStatus(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"clean", "", false},
		{"untracked only", "?? new.txt\n?? .kovan/session.yaml\n", false},
		{"ignored only", "!! build/\n", false},
		{"modified tracked", " M main.go\n", true},
		{"staged add", "A  added.go\n", true},
		{"mixed", "?? scratch\n M main.go\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dirtyFromStatus(c.out); got != c.want {
				t.Errorf("dirtyFromStatus(%q) = %v, want %v", c.out, got, c.want)
			}
		})
	}
}
