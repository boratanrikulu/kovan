package app

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEditFormPrefill(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir()) // built-in modes, no accounts configured
	row := boardRow{ID: "TASK-1", Repo: "agent", Worktree: "/wt", Title: "fix vfs", Mode: "review", Account: "company", Color: "blue"}
	e := newEditForm(row)
	if e.title.Value() != "fix vfs" {
		t.Errorf("title prefill = %q, want fix vfs", e.title.Value())
	}
	if e.selectedMode() != "review" {
		t.Errorf("mode prefill = %q, want review", e.selectedMode())
	}
	if e.selectedColor() != "blue" {
		t.Errorf("color prefill = %q, want blue", e.selectedColor())
	}
	if e.total() != 3 { // title + mode + color; no accounts → no account field
		t.Errorf("total = %d, want 3", e.total())
	}
	if !e.onTitle() {
		t.Error("focus should start on the title")
	}
}

func TestEditFormColorCycler(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	e := newEditForm(boardRow{ID: "TASK-1", Title: "x"})
	if e.selectedColor() != "" {
		t.Errorf("no color should select \"\", got %q", e.selectedColor())
	}
	e.setFocus(e.total() - 1)
	if !e.onColor() {
		t.Fatal("expected focus on the color field")
	}
	e, _ = e.update(tea.KeyMsg{Type: tea.KeyRight})
	if e.selectedColor() != rowTintNames[0] {
		t.Errorf("after right = %q, want %q", e.selectedColor(), rowTintNames[0])
	}
	e, _ = e.update(tea.KeyMsg{Type: tea.KeyLeft})
	if e.selectedColor() != "" {
		t.Errorf("cycling back to none should select \"\", got %q", e.selectedColor())
	}
}

func TestEditSubmitRequiresTitle(t *testing.T) {
	m := model{mode: modeEdit, edit: editModel{title: textinput.New()}}
	after, _ := m.submitEdit()
	mm := after.(model)
	if mm.mode != modeEdit || !mm.statusErr {
		t.Errorf("empty title should stay on the edit modal with an error; mode=%d err=%v", mm.mode, mm.statusErr)
	}
}

func TestEditedErrorStaysOnModal(t *testing.T) {
	m := model{mode: modeEdit}
	after, _ := m.Update(editedMsg{id: "x", err: errTest})
	if after.(model).mode != modeEdit {
		t.Error("an edit error should keep the modal open")
	}
	after, _ = m.Update(editedMsg{id: "x"})
	if after.(model).mode != modeBoard {
		t.Error("a successful edit should switch to the board")
	}
}
