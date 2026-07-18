package app

import (
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func colNames(cols []boardColumn) string {
	var out []string
	for _, c := range cols {
		out = append(out, c.name)
	}
	return strings.Join(out, " ")
}

func TestLayoutOrdered(t *testing.T) {
	// Empty order is the built-in order.
	if got, want := colNames(boardLayout{}.ordered()), colNames(boardColumns); got != want {
		t.Errorf("default order = %q, want %q", got, want)
	}

	// A custom order is honored; names it doesn't know keep their built-in
	// position at the end; unknown names contribute nothing.
	l := boardLayout{Order: []string{"ID", "TITLE", "NO-SUCH", "STATE"}}
	got := colNames(l.ordered())
	if !strings.HasPrefix(got, "ID TITLE STATE") {
		t.Errorf("ordered = %q, want it to start with the custom order", got)
	}
	if !strings.Contains(got, "PERM") || strings.Contains(got, "NO-SUCH") {
		t.Errorf("ordered = %q, want the missing built-ins appended and unknowns dropped", got)
	}
	if n := len(l.ordered()); n != len(boardColumns) {
		t.Errorf("ordered has %d columns, want %d", n, len(boardColumns))
	}
}

func TestLayoutColumns(t *testing.T) {
	all := boardLayout{}
	if got := len(all.columns()); got != len(boardColumns) {
		t.Fatalf("default layout has %d columns, want %d", got, len(boardColumns))
	}

	l := boardLayout{Hidden: map[string]bool{"PERM": true, "ACCOUNT": true}}
	for _, c := range l.columns() {
		if c.name == "PERM" || c.name == "ACCOUNT" {
			t.Errorf("hidden column %s still visible", c.name)
		}
	}

	// An all-hidden layout renders the full set instead of nothing.
	everything := map[string]bool{}
	for _, c := range boardColumns {
		everything[c.name] = true
	}
	if got := len(boardLayout{Hidden: everything}.columns()); got != len(boardColumns) {
		t.Errorf("all-hidden layout has %d columns, want the full set", got)
	}
}

func TestBoardViewHidesColumns(t *testing.T) {
	rows := []boardRow{{State: "working", ID: "TASK-1", Repo: "kovan", Title: "fix"}}
	l := boardLayout{Hidden: map[string]bool{"PERM": true, "WORKSPACE": true}}
	v := boardView(rows, 0, 100, 3, l)
	for _, gone := range []string{"PERM", "WORKSPACE"} {
		if strings.Contains(v, gone) {
			t.Errorf("board view still shows hidden column %s", gone)
		}
	}
	for _, want := range []string{"STATE", "TASK-1", "TITLE"} {
		if !strings.Contains(v, want) {
			t.Errorf("board view missing %q", want)
		}
	}
}

func TestBoardViewColumnOrder(t *testing.T) {
	rows := []boardRow{{State: "working", ID: "TASK-1", Title: "fix"}}
	l := boardLayout{Order: []string{"ID", "STATE"}}
	header := strings.SplitN(boardView(rows, 0, 100, 3, l), "\n", 2)[0]
	id, state := strings.Index(header, "ID"), strings.Index(header, "STATE")
	if id < 0 || state < 0 || id > state {
		t.Errorf("header = %q, want ID before STATE", header)
	}
}

func TestColumnsOverlay(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	base := model{mode: modeBoard, rows: []boardRow{{ID: "A"}, {ID: "B"}}}

	m := press(base, runeKey('v'))
	if m.mode != modeColumns {
		t.Fatalf("v: mode = %d, want modeColumns", m.mode)
	}
	if m = press(m, tea.KeyMsg{Type: tea.KeyEsc}); m.mode != modeBoard {
		t.Fatalf("esc: mode = %d, want modeBoard", m.mode)
	}

	// Space toggles the focused column; a second press restores it.
	m = press(press(base, runeKey('v')), runeKey(' '))
	if !m.layout.Hidden["STATE"] {
		t.Fatal("space on STATE did not hide it")
	}
	if m = press(m, runeKey(' ')); m.layout.Hidden["STATE"] {
		t.Fatal("second space did not restore STATE")
	}

	// J moves the focused column down the order and the cursor follows.
	m = press(m, runeKey('J'))
	if got := colNames(m.layout.ordered()); !strings.HasPrefix(got, "ID STATE") {
		t.Fatalf("J: order = %q, want ID STATE …", got)
	}
	if m.colCursor != 1 {
		t.Fatalf("J: cursor = %d, want 1 (following the column)", m.colCursor)
	}
	// K moves it back up; K at the top is a no-op.
	m = press(m, runeKey('K'))
	if got := colNames(m.layout.ordered()); !strings.HasPrefix(got, "STATE ID") {
		t.Fatalf("K: order = %q, want STATE ID …", got)
	}
	if m = press(m, runeKey('K')); m.colCursor != 0 {
		t.Fatalf("K at top: cursor = %d, want 0", m.colCursor)
	}

	// j moves the cursor; the last column cannot be hidden.
	m = press(base, runeKey('v'))
	for range boardColumns {
		m = press(m, runeKey(' ')) // hide the focused column
		m = press(m, runeKey('j'))
	}
	if hidden := len(m.layout.Hidden); hidden != len(boardColumns)-1 {
		t.Errorf("hidden %d columns, want %d (one must survive)", hidden, len(boardColumns)-1)
	}
}

func TestColumnsReset(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	base := model{mode: modeBoard, rows: []boardRow{{ID: "A"}}}

	// Hide a column, move one, then r restores the default — on disk too.
	m := press(press(base, runeKey('v')), runeKey(' '))
	m = press(m, runeKey('J'))
	m = press(m, runeKey('r'))
	if len(m.layout.Order) != 0 || len(m.layout.Hidden) != 0 {
		t.Fatalf("r: layout = %+v, want the default", m.layout)
	}
	if l := loadLayout(); len(l.Order) != 0 || len(l.Hidden) != 0 {
		t.Errorf("reloaded layout after reset = %+v, want the default", l)
	}
}

func TestColumnsOverlayView(t *testing.T) {
	m := testModel(modeColumns, nil)
	m.layout = boardLayout{Hidden: map[string]bool{"PERM": true}, Order: []string{"AGE"}}
	v := m.View()
	for _, want := range []string{"columns", "STATE", "AGE", "space show/hide"} {
		if !strings.Contains(v, want) {
			t.Errorf("columns view missing %q", want)
		}
	}
	// The custom order leads the list.
	if age, state := strings.Index(v, "AGE"), strings.Index(v, "STATE"); age > state {
		t.Errorf("overlay should list AGE before STATE with the custom order")
	}
}

func TestLayoutPersistence(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())

	// A toggle and a move persist across a reload.
	base := model{mode: modeBoard, rows: []boardRow{{ID: "A"}}}
	m := press(press(base, runeKey('v')), runeKey(' ')) // hide STATE
	m = press(m, runeKey('J'))                          // move STATE below ID
	_ = m

	l := loadLayout()
	if !l.Hidden["STATE"] {
		t.Error("reloaded layout lost the hidden column")
	}
	if got := colNames(l.ordered()); !strings.HasPrefix(got, "ID STATE") {
		t.Errorf("reloaded order = %q, want ID STATE …", got)
	}

	// Unknown names from a stale file are screened out.
	if err := config.SaveBoardLayout(&config.BoardLayout{
		Columns: []string{"GONE", "REPO"}, HiddenColumns: []string{"NO-SUCH", "REPO"},
	}); err != nil {
		t.Fatal(err)
	}
	l = loadLayout()
	if l.Hidden["NO-SUCH"] || !l.Hidden["REPO"] {
		t.Errorf("screened layout = %v, want just REPO hidden", l.Hidden)
	}
	if got := colNames(l.ordered()); !strings.HasPrefix(got, "REPO ") || strings.Contains(got, "GONE") {
		t.Errorf("screened order = %q, want REPO first and no unknowns", got)
	}
}
