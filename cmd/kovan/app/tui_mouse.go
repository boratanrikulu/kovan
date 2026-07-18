package app

import (
	"fmt"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// boardBodyTop is the first board body line: the header line and the board's
// column header sit above it.
const boardBodyTop = 2

// doubleClickWindow is how close two clicks on the same row must be to open it.
const doubleClickWindow = 400 * time.Millisecond

// boardRowAt maps a terminal line to a visible-row index, given the cursor
// (which anchors the scroll window), the visible count, and the body height.
// -1 outside the body or past the last row.
func boardRowAt(y, cursor, n, bodyRows int) int {
	if y < boardBodyTop || y >= boardBodyTop+bodyRows {
		return -1
	}
	idx := windowStart(cursor, n, bodyRows) + (y - boardBodyTop)
	if idx < 0 || idx >= n {
		return -1
	}
	return idx
}

// headerTabAt maps an x position on the header line to the tab under it:
// 0 active, 1 archived, -1 neither. The offsets mirror header()/tabs() plain
// text: the brand chip, "  · ", then the two labels separated by two spaces.
func headerTabAt(x, active, archived int) int {
	start := utf8.RuneCountInString(" kovan ") + 4
	aw := len(fmt.Sprintf("active %d", active))
	bw := len(fmt.Sprintf("archived %d", archived))
	switch {
	case x >= start && x < start+aw:
		return 0
	case x >= start+aw+2 && x < start+aw+2+bw:
		return 1
	}
	return -1
}

// isWheel reports whether the event is a wheel tick (wheel arrives as a press).
func isWheel(msg tea.MouseMsg) bool {
	return msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
}

// handleMouse routes a mouse event by page: hover and click work the board,
// the form takes clicks only (hover must not steal typing focus), and the
// method/monitor pages take the wheel for their viewports.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeBoard, modeFilter:
		return m.handleBoardMouse(msg)
	case modeForm:
		return m.handleFormMouse(msg)
	case modeMethod:
		var cmd tea.Cmd
		m.methodVP, cmd = m.methodVP.Update(msg)
		return m, cmd
	case modeMonitor:
		var cmd tea.Cmd
		m.monitorVP, cmd = m.monitorVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) handleBoardMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if isWheel(msg) {
		return m.handleBoardWheel(msg)
	}
	switch msg.Action {
	case tea.MouseActionMotion:
		// Hover selects: the cursor follows the pointer over the rows. Selection
		// has no side effect beyond which agent the peek shows.
		if row := boardRowAt(msg.Y, m.cursor, len(m.visible()), m.boardRows); row >= 0 && row != m.cursor {
			m.cursor = row
			return m, m.refresh()
		}
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			return m.handleBoardClick(msg)
		}
	}
	return m, nil
}

// handleBoardWheel scrolls the peek viewport when the pointer is over it (or
// below it), and moves the cursor like j/k anywhere above. The strip's height
// is live (it grows with the summary), so the boundary is computed per event.
func (m model) handleBoardWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	_, stripLines := m.summaryStrip()
	if peekTop := boardBodyTop + m.boardRows + stripLines + 1; msg.Y >= peekTop {
		var cmd tea.Cmd
		m.peek, cmd = m.peek.Update(msg)
		return m, cmd
	}
	if msg.Button == tea.MouseButtonWheelUp && m.cursor > 0 {
		m.cursor--
		return m, m.refresh()
	}
	if msg.Button == tea.MouseButtonWheelDown && m.cursor < len(m.visible())-1 {
		m.cursor++
		return m, m.refresh()
	}
	return m, nil
}

// handleBoardClick selects the clicked row, switches tabs on the header's tab
// bar, and opens the row on a double-click. With hover-select active a single
// click is usually a no-op, so open stays a deliberate double-click.
func (m model) handleBoardClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Y == 0 {
		archived := archivedCount(m.rows)
		if tab := headerTabAt(msg.X, len(m.rows)-archived, archived); tab >= 0 {
			if view := tab == 1; view != m.archivedView {
				m.archivedView = view
				m.cursor = 0
				m.dismissOnSwitch()
				return m, m.refresh()
			}
		}
		return m, nil
	}
	row := boardRowAt(msg.Y, m.cursor, len(m.visible()), m.boardRows)
	if row < 0 {
		return m, nil
	}
	double := row == m.lastClickRow && time.Since(m.lastClickAt) < doubleClickWindow
	m.lastClickRow, m.lastClickAt = row, time.Now()
	if row != m.cursor {
		m.cursor = row
		return m, m.refresh()
	}
	if double {
		if r := m.current(); r != nil {
			return m, m.openCmd(r.ID)
		}
	}
	return m, nil
}

// What a line of the new-agent form is under the mouse: a focusable field row,
// an option row in the options panel, the brief editor, or nothing.
type formHitKind int

const (
	hitNone formHitKind = iota
	hitField
	hitOption
	hitBrief
)

// formHitAt maps a y position in the form view to what's under it, mirroring
// view()'s line order: header, fields, spacer, options panel, spacer + CONTEXT
// header, then the brief. The field and option targets come from the same
// builders the view renders with, so the hit map can't drift from the layout.
func (f formModel) formHitAt(y int) (formHitKind, int) {
	y -= len(f.formHeader())
	fields, ftargets := f.fieldColumn()
	if y >= 0 && y < len(fields) {
		if t := ftargets[y]; t >= 0 {
			return hitField, t
		}
		return hitNone, 0
	}
	y -= len(fields) + 1 // the spacer above the options panel
	opts, otargets := f.optionsPanel()
	if y >= 0 && y < len(opts) {
		if t := otargets[y]; t >= 0 {
			return hitOption, t
		}
		return hitNone, 0
	}
	y -= len(opts) + 2 // the spacer above the CONTEXT header, and the header
	if y >= 0 && y < f.brief.Height() {
		return hitBrief, 0
	}
	return hitNone, 0
}

// handleFormMouse: a click focuses the field (or brief) under it or picks the
// clicked option; the wheel moves the focused picker's selection when over the
// options panel and scrolls the brief when over it. Hover never moves focus —
// focus decides where typing goes. The full-screen brief takes only the wheel.
func (m model) handleFormMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.form.briefFull {
		if isWheel(msg) {
			m.form.scrollBrief(msg.Button == tea.MouseButtonWheelDown)
		}
		return m, nil
	}
	if isWheel(msg) {
		switch kind, _ := m.form.formHitAt(msg.Y); kind {
		case hitOption:
			key := tea.KeyMsg{Type: tea.KeyUp}
			if msg.Button == tea.MouseButtonWheelDown {
				key = tea.KeyMsg{Type: tea.KeyDown}
			}
			var cmd tea.Cmd
			m.form, cmd = m.form.update(key)
			return m, cmd
		case hitBrief:
			// Wheeling the brief engages it, like a click — it must be focused
			// anyway for the textarea to reposition its view.
			m.form.setFocus(m.form.total() - 1)
			m.form.scrollBrief(msg.Button == tea.MouseButtonWheelDown)
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	switch kind, idx := m.form.formHitAt(msg.Y); kind {
	case hitField:
		m.form.setFocus(idx)
	case hitOption:
		m.form = m.form.setOption(idx)
	case hitBrief:
		m.form.setFocus(m.form.total() - 1)
	}
	return m, nil
}

// scrollBrief scrolls the brief textarea one wheel tick by moving its cursor —
// the textarea has no free scroll, its view follows the cursor. Three lines
// per tick, the same unit bubbles viewports scroll by. The empty Update is
// what repositions the view: the cursor methods only move the cursor, and
// the textarea realigns its viewport at the end of a (focused) Update.
func (f *formModel) scrollBrief(down bool) {
	for i := 0; i < 3; i++ {
		if down {
			f.brief.CursorDown()
		} else {
			f.brief.CursorUp()
		}
	}
	f.brief, _ = f.brief.Update(nil)
}
