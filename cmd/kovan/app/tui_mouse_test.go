package app

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBoardRowAt(t *testing.T) {
	cases := []struct {
		name                   string
		y, cursor, n, bodyRows int
		want                   int
	}{
		{"above the body", 1, 0, 5, 10, -1},
		{"first body line", boardBodyTop, 0, 5, 10, 0},
		{"third body line", boardBodyTop + 2, 0, 5, 10, 2},
		{"past the last row", boardBodyTop + 5, 0, 5, 10, -1},
		{"below the body", boardBodyTop + 10, 0, 5, 10, -1},
		{"empty board", boardBodyTop, 0, 0, 10, -1},
		// A long list windowed around the cursor: line 0 of the body is the
		// window start, not row 0.
		{"windowed list", boardBodyTop, 50, 100, 10, windowStart(50, 100, 10)},
		{"windowed list offset", boardBodyTop + 3, 50, 100, 10, windowStart(50, 100, 10) + 3},
	}
	for _, c := range cases {
		if got := boardRowAt(c.y, c.cursor, c.n, c.bodyRows); got != c.want {
			t.Errorf("%s: boardRowAt(%d,%d,%d,%d) = %d, want %d", c.name, c.y, c.cursor, c.n, c.bodyRows, got, c.want)
		}
	}
}

func TestHeaderTabAt(t *testing.T) {
	// " kovan " (7) + "  · " (4) = 11; "active 3" is 8 wide, then 2 spaces,
	// then "archived 12" (11 wide).
	cases := []struct {
		x    int
		want int
	}{
		{10, -1},
		{11, 0},
		{18, 0},
		{19, -1}, // the gap between the labels
		{20, -1},
		{21, 1},
		{31, 1},
		{32, -1},
	}
	for _, c := range cases {
		if got := headerTabAt(c.x, 3, 12); got != c.want {
			t.Errorf("headerTabAt(%d) = %d, want %d", c.x, got, c.want)
		}
	}
}

// mouseTestForm is a minimal form (two inputs, modes, colors, no accounts) so
// hit-testing is deterministic: total focusables = 8 (id, title, project,
// target, from, mode, color, brief).
func mouseTestForm() formModel {
	f := formModel{
		inputs: []textinput.Model{textinput.New(), textinput.New()},
		brief:  textarea.New(),
		modes:  []string{"code", "review"},
		colors: []string{"none", "red"},
	}
	f.brief.SetHeight(4)
	return f
}

func TestFormHitAt(t *testing.T) {
	f := mouseTestForm()
	// view() order: header (2) + fields (12: WHAT,id,title,blank,WHERE,project,
	// where,ctx,blank,HOW,mode,color) + spacer + options (6) + spacer + CONTEXT
	// header + brief.
	fields, targets := f.fieldColumn()
	if len(fields) != 12 {
		t.Fatalf("fieldColumn lines = %d, want 12", len(fields))
	}
	wantTargets := []int{-1, 0, 1, -1, -1, 2, 3, 4, -1, -1, 5, 6}
	for i, w := range wantTargets {
		if targets[i] != w {
			t.Errorf("fieldColumn target[%d] = %d, want %d", i, targets[i], w)
		}
	}

	cases := []struct {
		name     string
		y        int
		wantKind formHitKind
		wantIdx  int
	}{
		{"form header", 0, hitNone, 0},
		{"WHAT header", 2, hitNone, 0},
		{"id row", 3, hitField, 0},
		{"title row", 4, hitField, 1},
		{"project row", 7, hitField, 2},
		{"where row", 8, hitField, 3},
		{"ctx row", 9, hitField, 4},
		{"mode row", 12, hitField, 5},
		{"color row", 13, hitField, 6},
		{"spacer above options", 14, hitNone, 0},
		{"options title", 15, hitNone, 0}, // focus 0 → informational recap
		{"recap line", 16, hitNone, 0},
		{"CONTEXT header", 22, hitNone, 0},
		{"brief first line", 23, hitBrief, 0},
		{"brief last line", 26, hitBrief, 0},
		{"below the brief", 27, hitNone, 0},
	}
	for _, c := range cases {
		kind, idx := f.formHitAt(c.y)
		if kind != c.wantKind || idx != c.wantIdx {
			t.Errorf("%s: formHitAt(%d) = (%d,%d), want (%d,%d)", c.name, c.y, kind, idx, c.wantKind, c.wantIdx)
		}
	}
}

func TestFormHitAtOptions(t *testing.T) {
	f := mouseTestForm()
	f.setFocus(5) // mode: a non-searchable picker, options are clickable
	// Options panel starts after header(2) + fields(12) + spacer(1) = 15:
	// title at 15, then the option rows.
	if kind, _ := f.formHitAt(15); kind != hitNone {
		t.Errorf("options title: kind = %d, want hitNone", kind)
	}
	for i, mode := range f.modes {
		kind, idx := f.formHitAt(16 + i)
		if kind != hitOption || idx != i {
			t.Errorf("option %q: formHitAt(%d) = (%d,%d), want (hitOption,%d)", mode, 16+i, kind, idx, i)
		}
	}
	if kind, _ := f.formHitAt(16 + len(f.modes)); kind != hitNone {
		t.Errorf("blank option row: want hitNone")
	}
}

func TestFormSetOption(t *testing.T) {
	f := mouseTestForm()

	f.setFocus(3) // target
	f = f.setOption(targetTab)
	if f.target != targetTab {
		t.Errorf("target = %d, want %d", f.target, targetTab)
	}

	f.setFocus(5) // mode
	f = f.setOption(1)
	if f.selectedMode() != "review" {
		t.Errorf("mode = %q, want review", f.selectedMode())
	}

	f.setFocus(6) // color
	f = f.setOption(1)
	if f.selectedColor() != "red" {
		t.Errorf("color = %q, want red", f.selectedColor())
	}

	// A from-picker click lands on the picker items, not the fake placeholder
	// rendered when a project has no workspaces.
	f.setFocus(4)
	f.target = targetTab
	f = f.setOption(0) // no workspaces: must not panic, selection unchanged
	if got := f.workspaces.value(); got != "" {
		t.Errorf("workspace value = %q, want empty", got)
	}
}

func TestHandleFormMouseClick(t *testing.T) {
	m := model{mode: modeForm, form: mouseTestForm(), ready: true}
	m.form.setFocus(0)

	// Click the title row → focus moves to it.
	next, _ := m.handleFormMouse(tea.MouseMsg{Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if got := next.(model).form.focus; got != 1 {
		t.Errorf("click title: focus = %d, want 1", got)
	}

	// Click the brief → focus moves to the last field.
	next, _ = m.handleFormMouse(tea.MouseMsg{Y: 23, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	f := next.(model).form
	if !f.onBrief() {
		t.Errorf("click brief: focus = %d, want the brief (%d)", f.focus, f.total()-1)
	}

	// Hover must not move focus.
	next, _ = m.handleFormMouse(tea.MouseMsg{Y: 4, Action: tea.MouseActionMotion})
	if got := next.(model).form.focus; got != 0 {
		t.Errorf("hover: focus = %d, want 0 (unchanged)", got)
	}

	// The full-screen brief takes no clicks.
	m.form.briefFull = true
	next, _ = m.handleFormMouse(tea.MouseMsg{Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if got := next.(model).form.focus; got != 0 {
		t.Errorf("briefFull click: focus = %d, want 0 (unchanged)", got)
	}
}

func TestHandleFormMouseWheelBrief(t *testing.T) {
	m := model{mode: modeForm, form: mouseTestForm(), ready: true}
	m.form.brief.SetValue("l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8")
	m.form.brief.CursorStart()
	for i := 0; i < 7; i++ { // cursor to the last line
		m.form.brief.CursorDown()
	}
	if m.form.brief.Line() != 7 {
		t.Fatalf("setup: line = %d, want 7", m.form.brief.Line())
	}

	// Inline: wheel up over the brief region engages it and scrolls (the cursor
	// moves 3 lines; focus is needed for the textarea to reposition its view).
	next, _ := m.handleFormMouse(tea.MouseMsg{Y: 23, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	m = next.(model)
	if got := m.form.brief.Line(); got != 4 {
		t.Errorf("inline wheel up: line = %d, want 4", got)
	}
	if !m.form.onBrief() {
		t.Errorf("inline wheel up: focus = %d, want the brief", m.form.focus)
	}
	// Wheel away from the brief leaves it alone.
	next, _ = m.handleFormMouse(tea.MouseMsg{Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	m = next.(model)
	if got := m.form.brief.Line(); got != 4 {
		t.Errorf("wheel off-brief: line = %d, want 4 (unchanged)", got)
	}

	// Full-screen: the wheel scrolls regardless of y.
	m.form.briefFull = true
	next, _ = m.handleFormMouse(tea.MouseMsg{Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	m = next.(model)
	if got := m.form.brief.Line(); got != 7 {
		t.Errorf("briefFull wheel down: line = %d, want 7", got)
	}
}

func mouseBoardModel(n int) model {
	rows := make([]boardRow, n)
	for i := range rows {
		rows[i] = boardRow{ID: string(rune('a' + i)), State: "idle", Tmux: "kovan-x"}
	}
	return model{mode: modeBoard, rows: rows, boardRows: 10, width: 80, ready: true, peek: viewport.New(80, 5)}
}

func TestHandleBoardMouseHoverAndClick(t *testing.T) {
	m := mouseBoardModel(5)

	// Hover over the third row moves the cursor.
	next, _ := m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop + 2, Action: tea.MouseActionMotion})
	if got := next.(model).cursor; got != 2 {
		t.Errorf("hover: cursor = %d, want 2", got)
	}

	// A single click selects; a quick second click on the same row opens.
	m = next.(model)
	next, cmd := m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = next.(model)
	if cmd != nil {
		t.Fatalf("first click: unexpected command (open?)")
	}
	if m.lastClickRow != 2 {
		t.Fatalf("first click: lastClickRow = %d, want 2", m.lastClickRow)
	}
	_, cmd = m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Errorf("double click: want an open command")
	}

	// A stale second click (outside the window) does not open.
	m.lastClickAt = time.Now().Add(-time.Second)
	_, cmd = m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil {
		t.Errorf("stale click: got an open command, want selection only")
	}
}

func TestHandleBoardMouseWheel(t *testing.T) {
	m := mouseBoardModel(5)
	m.cursor = 2

	next, _ := m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if got := next.(model).cursor; got != 1 {
		t.Errorf("wheel up: cursor = %d, want 1", got)
	}
	next, _ = m.handleBoardMouse(tea.MouseMsg{Y: boardBodyTop, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if got := next.(model).cursor; got != 3 {
		t.Errorf("wheel down: cursor = %d, want 3", got)
	}

	// Over the peek region the wheel scrolls the viewport, not the cursor.
	peekTop := boardBodyTop + m.boardRows + summaryStripLines + 1
	next, _ = m.handleBoardMouse(tea.MouseMsg{Y: peekTop, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if got := next.(model).cursor; got != 2 {
		t.Errorf("wheel over peek: cursor = %d, want 2 (unchanged)", got)
	}
}

func TestHandleBoardMouseTabBar(t *testing.T) {
	m := mouseBoardModel(3)
	m.rows[2].State = "archived"
	// active 2 → "active 2" spans x 11..18; "archived 1" starts at 21.
	next, _ := m.handleBoardMouse(tea.MouseMsg{Y: 0, X: 22, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !next.(model).archivedView {
		t.Errorf("click archived tab: archivedView = false, want true")
	}
	next, _ = next.(model).handleBoardMouse(tea.MouseMsg{Y: 0, X: 12, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if next.(model).archivedView {
		t.Errorf("click active tab: archivedView = true, want false")
	}
}
