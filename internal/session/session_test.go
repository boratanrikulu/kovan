package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestManifestRoundTrip(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	worktree := t.TempDir()

	want := &Manifest{
		ID:        "TASK-1",
		Title:     "fix the vfs handler",
		Repo:      "kovan",
		RepoRoot:  "/home/bora/kovan",
		Worktree:  worktree,
		Branch:    "feat/bora_TASK-1_fix",
		Base:      "main",
		Tmux:      "kovan-kovan-TASK-1",
		Agent:     "claude",
		Account:   "personal",
		State:     "working",
		Pinned:    true,
		Color:     "red",
		CreatedAt: time.Now(),
	}
	if err := want.Write(); err != nil {
		t.Fatal(err)
	}

	got, err := ReadByTmux(want.Tmux)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Branch != want.Branch ||
		got.Tmux != want.Tmux || got.Agent != want.Agent || got.State != want.State ||
		got.Account != want.Account || got.Pinned != want.Pinned || got.Color != want.Color {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, want.CreatedAt)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "TASK-1" {
		t.Errorf("List() = %+v, want one TASK-1 manifest", list)
	}
}

func TestListMigratesLegacyLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	wt := t.TempDir()

	// Pre-tabs layout: the manifest lives in the worktree, and the index holds a
	// bare pointer (the worktree path) named by the tmux session.
	m := &Manifest{ID: "old", Repo: "r", Worktree: wt, Tmux: "kovan-r-old", CreatedAt: time.Now()}
	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".kovan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".kovan", "session.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(home, "sessions")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(index, "kovan-r-old"), []byte(wt+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "old" {
		t.Fatalf("List() = %+v, want one migrated 'old'", list)
	}
	// The first pass writes the index-resident manifest (migration is write-only,
	// so a concurrent reader can never lose it).
	if _, err := os.Stat(filepath.Join(index, "kovan-r-old.yaml")); err != nil {
		t.Errorf("migrated manifest missing: %v", err)
	}
	// A later pass, with the .yaml confirmed present, safely retires the legacy
	// pointer — and still lists exactly one agent.
	list, err = List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("second pass: List() = %d, want 1", len(list))
	}
	if _, err := os.Stat(filepath.Join(index, "kovan-r-old")); !os.IsNotExist(err) {
		t.Errorf("legacy pointer should be retired on the second pass, err = %v", err)
	}
}

func TestTabsShareWorktree(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	wt := t.TempDir() // one workspace, two tabs

	a := &Manifest{ID: "a", Worktree: wt, Tmux: "kovan-r-a", SessionID: "sid-a", CreatedAt: time.Now()}
	b := &Manifest{ID: "b", Worktree: wt, Tmux: "kovan-r-b", SessionID: "sid-b", CreatedAt: time.Now()}
	if err := a.Write(); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(); err != nil {
		t.Fatal(err)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("two tabs in one worktree should both list, got %d", len(list))
	}

	m, found, err := FindBySessionID("sid-b")
	if err != nil || !found {
		t.Fatalf("FindBySessionID: found=%v err=%v", found, err)
	}
	if m.ID != "b" {
		t.Errorf("session id resolved to tab %q, want b", m.ID)
	}
}

// TestListConcurrentMigrationSafe reproduces the incident where many gate hooks
// called List() at once while legacy pointers migrated, and the racing deletes
// wiped freshly-migrated manifests. List must never drop a valid entry.
func TestListConcurrentMigrationSafe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	index := filepath.Join(home, "sessions")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}

	// 20 legacy-layout agents: a worktree manifest plus a bare index pointer.
	const n = 20
	for i := 0; i < n; i++ {
		wt := t.TempDir()
		tmux := "kovan-r-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		m := &Manifest{ID: tmux, Worktree: wt, Tmux: tmux, CreatedAt: time.Now()}
		data, err := yaml.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(wt, ".kovan"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".kovan", "session.yaml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(index, tmux), []byte(wt+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Hammer List from many goroutines, as concurrent gate hooks would.
	done := make(chan int, 16)
	for g := 0; g < 16; g++ {
		go func() {
			got := 0
			for r := 0; r < 10; r++ {
				if l, err := List(); err == nil {
					got = len(l)
				}
			}
			done <- got
		}()
	}
	for g := 0; g < 16; g++ {
		<-done
	}

	if l, err := List(); err != nil || len(l) != n {
		t.Fatalf("after concurrent migration: List() = %d entries, want %d (err %v)", len(l), n, err)
	}
}

func TestListPrunesDeadPointers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)

	index := filepath.Join(home, "sessions")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	dead := filepath.Join(index, "kovan-ghost")
	if err := os.WriteFile(dead, []byte("/nonexistent/worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("List() = %+v, want empty", list)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("dead pointer should have been pruned, stat err = %v", err)
	}
}
