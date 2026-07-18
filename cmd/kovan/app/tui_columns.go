package app

import (
	"fmt"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// loadLayout reads the persisted board layout, screening out column names the
// board no longer has. Any read problem falls back to the default layout: the
// file is machine-written, and the next change overwrites it.
func loadLayout() boardLayout {
	saved, err := config.LoadBoardLayout()
	if err != nil {
		return boardLayout{}
	}
	l := boardLayout{}
	for _, n := range saved.Columns {
		if isBoardColumn(n) {
			l.Order = append(l.Order, n)
		}
	}
	for _, n := range saved.HiddenColumns {
		if !isBoardColumn(n) {
			continue
		}
		if l.Hidden == nil {
			l.Hidden = map[string]bool{}
		}
		l.Hidden[n] = true
	}
	if len(l.Hidden) >= len(boardColumns) {
		l.Hidden = nil // never start with an all-hidden board
	}
	return l
}

// saveLayout persists the board layout; a write error shows in the status bar
// but the in-memory layout still applies.
func (m *model) saveLayout() {
	saved := &config.BoardLayout{Columns: m.layout.Order}
	for _, c := range m.layout.ordered() {
		if m.layout.Hidden[c.name] {
			saved.HiddenColumns = append(saved.HiddenColumns, c.name)
		}
	}
	if err := config.SaveBoardLayout(saved); err != nil {
		m.setErr(err)
	}
}

// handleColumnsKey drives the columns overlay: j/k moves the cursor, space
// toggles the focused column's visibility, J/K moves the column itself in the
// order, r resets to the default layout, esc closes.
func (m model) handleColumnsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "v", "q":
		m.mode = modeBoard
		return m, nil
	case " ":
		m.toggleColumn(m.layout.ordered()[m.colCursor].name)
		return m, nil
	case "J":
		m.moveColumn(m.colCursor, m.colCursor+1)
		return m, nil
	case "K":
		m.moveColumn(m.colCursor, m.colCursor-1)
		return m, nil
	case "r":
		m.layout = boardLayout{}
		m.saveLayout()
		m.setInfo("board layout reset to default")
		return m, nil
	}
	switch keys.action(msg) {
	case actUp:
		if m.colCursor > 0 {
			m.colCursor--
		}
	case actDown:
		if m.colCursor < len(boardColumns)-1 {
			m.colCursor++
		}
	}
	return m, nil
}

// toggleColumn flips a column's visibility, refusing to hide the last one.
func (m *model) toggleColumn(name string) {
	if m.layout.Hidden[name] {
		delete(m.layout.Hidden, name)
	} else {
		if len(m.layout.Hidden) >= len(boardColumns)-1 {
			m.setErrMsg("at least one column must stay visible")
			return
		}
		if m.layout.Hidden == nil {
			m.layout.Hidden = map[string]bool{}
		}
		m.layout.Hidden[name] = true
	}
	m.saveLayout()
}

// moveColumn swaps the column at from with its neighbor at to; the overlay
// cursor follows the moved column.
func (m *model) moveColumn(from, to int) {
	if to < 0 || to >= len(boardColumns) {
		return
	}
	cols := m.layout.ordered()
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.name
	}
	names[from], names[to] = names[to], names[from]
	m.layout.Order = names
	m.colCursor = to
	m.saveLayout()
}

// columnsView is the columns overlay: every board column in its current order
// with its visibility mark, the focused one highlighted.
func (m model) columnsView() string {
	lines := []string{
		brandHeader("columns"),
		dimStyle.Render("choose the board's columns and their order"),
		"",
	}
	for i, c := range m.layout.ordered() {
		mark := "✓"
		if m.layout.Hidden[c.name] {
			mark = "·"
		}
		label := fmt.Sprintf("%s %s", mark, c.name)
		if i == m.colCursor {
			lines = append(lines, cursorStyle.Render("› "+label))
		} else if m.layout.Hidden[c.name] {
			lines = append(lines, dimStyle.Render("  "+label))
		} else {
			lines = append(lines, "  "+label)
		}
	}
	lines = append(lines, "", m.formStatusLine(),
		dimStyle.Render("space show/hide · J/K move · r reset · esc close"))
	return strings.Join(lines, "\n")
}
