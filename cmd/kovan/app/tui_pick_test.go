package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBoardAppPicksAmongWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("apps:\n  editor: true\n  merge: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	multi := boardRow{ID: "A", Repo: "agent", Worktree: "/w/agent-a",
		Extra: []session.Workspace{{Repo: "cloud", Worktree: "/w/cloud-a"}}}

	m := press(testModel(modeBoard, []boardRow{multi}), runeKey('e'))
	if m.mode != modePickWorkspace || m.pickKind != "editor" {
		t.Fatalf("e on a multi-repo agent: mode=%d kind=%q, want the workspace picker for editor", m.mode, m.pickKind)
	}
	view := m.View()
	for _, want := range []string{"agent", "/w/agent-a", "cloud", "/w/cloud-a"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view missing %q:\n%s", want, view)
		}
	}
	if m = press(m, runeKey('j')); m.pickCursor != 1 {
		t.Errorf("j: cursor = %d, want 1", m.pickCursor)
	}
	if got := m.pickTarget(); got != "/w/cloud-a" {
		t.Errorf("target = %q, want the cloud worktree", got)
	}
	if m = press(m, tea.KeyMsg{Type: tea.KeyEnter}); m.mode != modeBoard {
		t.Errorf("enter: mode = %d, want back on the board", m.mode)
	}
	if m = press(press(testModel(modeBoard, []boardRow{multi}), runeKey('s')), tea.KeyMsg{Type: tea.KeyEsc}); m.mode != modeBoard {
		t.Errorf("esc: mode = %d, want the board", m.mode)
	}

	single := boardRow{ID: "B", Repo: "agent", Worktree: "/w/agent-b"}
	if m := press(testModel(modeBoard, []boardRow{single}), runeKey('e')); m.mode != modeBoard {
		t.Errorf("e on a single-repo agent opens directly; mode = %d", m.mode)
	}
}

func TestRepoCellCountsExtras(t *testing.T) {
	r := boardRow{Repo: "agent", Extra: []session.Workspace{{Repo: "cloud"}}}
	if got := repoCell(r); got != "agent+1" {
		t.Errorf("repoCell = %q, want agent+1", got)
	}
	if !rowMatches(r, "cloud") {
		t.Error("the filter should match an extra workspace's repo")
	}
}

func TestPickerKeepsItsAgentWhenRowsReorder(t *testing.T) {
	multi := boardRow{ID: "A", Repo: "agent", Tmux: "kovan-agent-A", Worktree: "/w/agent-a",
		Extra: []session.Workspace{{Repo: "cloud", Worktree: "/w/cloud-a"}}}
	m := press(testModel(modeBoard, []boardRow{multi}), runeKey('e'))
	m = press(m, runeKey('j'))
	// A board refresh puts a new agent first while the picker is open.
	m.rows = []boardRow{{ID: "B", Repo: "x", Tmux: "kovan-x-B", Worktree: "/w/x-b"}, multi}
	if got := m.pickTarget(); got != "/w/cloud-a" {
		t.Errorf("target after reorder = %q, want A's cloud worktree", got)
	}
}
