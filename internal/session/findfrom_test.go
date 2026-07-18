package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindFrom(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	wt := t.TempDir()
	sub := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{ID: "X", Worktree: wt, Tmux: "t"}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}

	got, found, err := FindFrom(sub)
	if err != nil || !found {
		t.Fatalf("walk up: found=%v err=%v", found, err)
	}
	if got.ID != "X" {
		t.Errorf("id = %q, want X", got.ID)
	}

	if _, found, err := FindFrom(t.TempDir()); err != nil || found {
		t.Errorf("unrelated dir: found=%v err=%v, want not found", found, err)
	}
}
