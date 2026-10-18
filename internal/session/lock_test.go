package session

import (
	"testing"
	"time"
)

// TestLockSurvivesList: List prunes what it can't read as a manifest; the lock
// must not be among it, or the next locker gets a fresh file and both run.
func TestLockSurvivesList(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	unlock, err := Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := List(); err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		u, err := Lock()
		if err == nil {
			u()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("a second Lock got in while the first was held")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the second Lock never got in after the release")
	}
}

// TestUpdate re-reads under the lock, so a write made in between survives.
func TestUpdate(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	if err := (&Manifest{ID: "a", Tmux: "t", Title: "old"}).Write(); err != nil {
		t.Fatal(err)
	}
	stale, _ := ReadByTmux("t")
	fresh, _ := ReadByTmux("t")
	fresh.Summary = "kept"
	if err := fresh.Write(); err != nil {
		t.Fatal(err)
	}
	if err := Update(stale.Tmux, func(m *Manifest) error { m.Title = "new"; return nil }); err != nil {
		t.Fatal(err)
	}
	if m, _ := ReadByTmux("t"); m.Title != "new" || m.Summary != "kept" {
		t.Errorf("after Update = title %q summary %q, want new and kept", m.Title, m.Summary)
	}
}
