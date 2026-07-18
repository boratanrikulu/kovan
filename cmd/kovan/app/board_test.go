package app

import (
	"strings"
	"testing"
	"time"

	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

func TestFilterRows(t *testing.T) {
	rows := []boardRow{
		{ID: "TASK-1", Repo: "agent", Mode: "code", Title: "fix vfs", State: "working"},
		{ID: "f0a8", Repo: "webapp", Mode: "review", Title: "review pr", State: "idle"},
		{ID: "old", Repo: "kovan", Mode: "code", Title: "done thing", State: "archived"},
	}

	if got := filterRows(rows, "", false); len(got) != 2 {
		t.Errorf("active view should show non-archived: got %d, want 2", len(got))
	}
	if got := filterRows(rows, "", true); len(got) != 1 || got[0].ID != "old" {
		t.Errorf("archived view should show only archived: got %v, want [old]", got)
	}
	// Filter by mode (free, since mode is a matched field).
	if got := filterRows(rows, "review", false); len(got) != 1 || got[0].ID != "f0a8" {
		t.Errorf("mode filter = %v, want [f0a8]", got)
	}
	// Filter by repo substring.
	if got := filterRows(rows, "webapp", false); len(got) != 1 || got[0].ID != "f0a8" {
		t.Errorf("repo filter = %v, want [f0a8]", got)
	}
	// A filter matching an archived row still respects showArchived.
	if got := filterRows(rows, "done", false); len(got) != 0 {
		t.Errorf("archived row should stay hidden under a filter: got %d", len(got))
	}
	if got := filterRows(rows, "done", true); len(got) != 1 {
		t.Errorf("archived row should match when shown: got %d", len(got))
	}
}

func TestAssembleBoardArchived(t *testing.T) {
	// Archived wins over liveness: even a live tmux renders as archived.
	rows := assembleBoard([]*session.Manifest{{Tmux: "t", Archived: true, State: "working"}}, func(string) bool { return true })
	if rows[0].State != "archived" {
		t.Errorf("archived manifest state = %q, want archived", rows[0].State)
	}
}

func TestAssembleBoardArchivedPerm(t *testing.T) {
	// An archived agent's PERM is the frozen hook-seen mode, mapped — no
	// transcript scan, so its (possibly huge) session id is never globbed.
	m := &session.Manifest{Tmux: "t", Archived: true, Mode: "bypassPermissions", SessionID: "ffffffff-0000-4000-8000-000000000000"}
	rows := assembleBoard([]*session.Manifest{m}, func(string) bool { return false })
	if rows[0].Perm != "bypass" {
		t.Errorf("archived perm = %q, want bypass from the manifest's mode", rows[0].Perm)
	}
}

func TestBoardVisible(t *testing.T) {
	m := model{rows: []boardRow{
		{ID: "TASK-1", Repo: "agent", Mode: "code", State: "working"},
		{ID: "f0a8", Repo: "sc", Mode: "review", State: "idle"},
		{ID: "old", Repo: "kovan", State: "archived"},
	}}
	if len(m.visible()) != 2 {
		t.Errorf("default visible should hide archived: %d", len(m.visible()))
	}
	m.filter = "review"
	if v := m.visible(); len(v) != 1 || v[0].ID != "f0a8" {
		t.Errorf("filtered visible = %v, want [f0a8]", v)
	}
	m.filter = ""
	m.archivedView = true
	if v := m.visible(); len(v) != 1 || v[0].ID != "old" {
		t.Errorf("archived view = %v, want [old]", v)
	}
}

func TestPinnedSortsFirst(t *testing.T) {
	now := time.Now()
	// An old pinned workspace (owner + tab, pin on the tab) and a newer
	// unpinned lone agent: the pin lifts the whole cluster above the newer one.
	manifests := []*session.Manifest{
		{ID: "owner", Repo: "r", Worktree: "/wt/a", Branch: "feat/a", Tmux: "t-owner", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "tab", Repo: "r", Worktree: "/wt/a", Branch: "feat/a", Tmux: "t-tab", Pinned: true, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "lone", Repo: "r", Worktree: "/wt/b", Branch: "feat/b", Tmux: "t-lone", CreatedAt: now},
	}
	rows := filterRows(assembleBoard(manifests, func(string) bool { return false }), "", false)
	if rows[0].ID != "owner" || rows[1].ID != "tab" || rows[2].ID != "lone" {
		t.Fatalf("order = %s,%s,%s; want owner,tab,lone (pin lifts the cluster)", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	if !rows[1].Cont {
		t.Error("the pinned tab should still be a continuation of its owner")
	}
	if rows[0].Pinned || !rows[1].Pinned {
		t.Error("Pinned should be carried onto the actually-pinned row only")
	}

	// Two pinned groups keep the normal newest-first order between them.
	both := []*session.Manifest{
		{ID: "p-old", Worktree: "/wt/c", Tmux: "t-c", Pinned: true, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "p-new", Worktree: "/wt/d", Tmux: "t-d", Pinned: true, CreatedAt: now.Add(-time.Hour)},
	}
	rows = assembleBoard(both, func(string) bool { return false })
	if rows[0].ID != "p-new" || rows[1].ID != "p-old" {
		t.Errorf("pinned groups = %s,%s; want newest-first p-new,p-old", rows[0].ID, rows[1].ID)
	}
}

func TestPinnedSortsFirstPerView(t *testing.T) {
	now := time.Now()
	manifests := []*session.Manifest{
		// A fresh unpinned archived tab whose worktree-mate is a pinned ACTIVE
		// agent: its group's pin must not lift it over pinned archived agents.
		{ID: "act", Worktree: "/wt/x", Tmux: "t-act", Pinned: true, CreatedAt: now},
		{ID: "arc-tab", Worktree: "/wt/x", Tmux: "t-arc", Archived: true, CreatedAt: now.Add(-time.Hour)},
		{ID: "arc-pin", Worktree: "/wt/y", Tmux: "t-pin", Archived: true, Pinned: true, CreatedAt: now.Add(-72 * time.Hour)},
	}
	rows := filterRows(assembleBoard(manifests, func(string) bool { return false }), "", true)
	if len(rows) != 2 || rows[0].ID != "arc-pin" || rows[1].ID != "arc-tab" {
		t.Fatalf("archived order = %s,%s; want arc-pin,arc-tab (pin ranks within its own view)",
			rows[0].ID, rows[1].ID)
	}
	active := filterRows(assembleBoard(manifests, func(string) bool { return false }), "", false)
	if len(active) != 1 || active[0].ID != "act" {
		t.Fatalf("active view = %v, want just act", active)
	}
}

func TestIDCellMarksPin(t *testing.T) {
	if got := idCell(boardRow{ID: "6cf0", Pinned: true}); got != "* 6cf0" {
		t.Errorf("pinned id cell = %q, want \"* 6cf0\"", got)
	}
	if got := idCell(boardRow{ID: "6cf0"}); got != "6cf0" {
		t.Errorf("unpinned id cell = %q, want the bare id", got)
	}
	// The TUI shows the marker too: a colored row's tint hides the pinned tint,
	// so the marker is the one signal that always reads.
	if got := rowCells(boardColumns, boardRow{ID: "6cf0", Pinned: true})[1]; got != "* 6cf0" {
		t.Errorf("TUI id cell = %q, want the pin marker", got)
	}
}

func TestRowStripe(t *testing.T) {
	rows := []boardRow{{State: "idle", Color: "magenta"}, {State: "idle"}}
	widths := boardColumnWidths(boardColumns, rows, 100)
	tagged := boardRowLine(rows[0], boardColumns, widths, false)
	plain := boardRowLine(rows[1], boardColumns, widths, false)
	if !strings.Contains(tagged, stripeGlyph) {
		t.Error("a tagged row should carry the stripe glyph")
	}
	if strings.Contains(plain, stripeGlyph) {
		t.Error("an untagged row should have no stripe")
	}
	// The stripe survives selection, and an unknown color name draws nothing.
	if s := boardRowLine(rows[0], boardColumns, widths, true); !strings.Contains(s, stripeGlyph) {
		t.Error("the stripe should stay visible on the selected row")
	}
	if s := boardRowLine(boardRow{State: "idle", Color: "no-such"}, boardColumns, widths, false); strings.Contains(s, stripeGlyph) {
		t.Error("an unknown color name should draw no stripe")
	}
	// Column alignment is identical with and without a stripe.
	if lipgloss.Width(tagged) != lipgloss.Width(plain) {
		t.Errorf("tagged row width %d != plain row width %d", lipgloss.Width(tagged), lipgloss.Width(plain))
	}
	// Every palette name resolves to a stripe hue.
	for _, name := range rowTintNames {
		if c, ok := rowTints[name]; !ok || c == "" {
			t.Errorf("palette name %q has no stripe hue", name)
		}
	}
}

func TestRowColors(t *testing.T) {
	// Text keeps its state rung everywhere, tagged or not; the user color
	// lives in the stripe, never in the text or a row background.
	if fg, _ := rowColors(boardRow{State: "stopped", Color: "green"}, false); fg != stateColor("stopped") {
		t.Errorf("stopped tagged fg = %v, want the state grey", fg)
	}
	if fg, _ := rowColors(boardRow{State: "needs-you", Color: "green"}, true); fg != colorAccent {
		t.Errorf("needs-you under the cursor fg = %v, want the accent", fg)
	}
	// The cursor band is the only row background, tagged rows included.
	for _, r := range []boardRow{{State: "stopped"}, {State: "stopped", Color: "green"}, {State: "stopped", Pinned: true}} {
		if _, bg := rowColors(r, true); bg != colorCursorBg {
			t.Errorf("cursor bg on %+v = %v, want the cursor band", r, bg)
		}
		if _, bg := rowColors(r, false); bg != "" {
			t.Errorf("bg on %+v = %v, want none off-cursor", r, bg)
		}
	}
}

func TestSummaryStripBanner(t *testing.T) {
	m := model{width: 100, rows: []boardRow{{ID: "A", State: "needs-you", Tmux: "t"}},
		monitor: map[string]agentSummary{"t": {text: "Approval pending: push the branch?"}}}
	strip, n := m.summaryStrip()
	lines := strings.Split(strip, "\n")
	if len(lines) != summaryStripLines || n != summaryStripLines {
		t.Fatalf("strip is %d lines (reported %d), want %d", len(lines), n, summaryStripLines)
	}
	// The banner is a short constant marker; the detail stays in the dim
	// summary below it, never duplicated into the banner.
	if !strings.Contains(lines[0], "waiting on you") || strings.Contains(lines[0], "Approval pending") {
		t.Errorf("banner line = %q, want the bare marker without the summary text", lines[0])
	}
	if !strings.Contains(strip, "summary · Approval pending: push the branch?") {
		t.Error("the summary text should render below the banner")
	}
	// Nothing pending → no banner, same height.
	m.rows[0].State = "working"
	strip, _ = m.summaryStrip()
	if strings.Contains(strip, "waiting on you") {
		t.Error("a working agent should have no banner")
	}
	if got := len(strings.Split(strip, "\n")); got != summaryStripLines {
		t.Errorf("strip without a banner is %d lines, want %d", got, summaryStripLines)
	}
}

// TestSummaryStripGrows pins the no-trim rule: a long summary takes lines from
// the peek (up to maxStripLines) instead of being cut mid-thought, and the
// reported count matches so the view keeps the total height exact.
func TestSummaryStripGrows(t *testing.T) {
	long := strings.Repeat("all work and no play makes the summary long. ", 12)
	m := model{width: 60, rows: []boardRow{{ID: "A", State: "needs-you", Tmux: "t"}},
		monitor: map[string]agentSummary{"t": {text: long}}}
	m.peek = viewport.New(60, 10) // plenty to spare

	strip, n := m.summaryStrip()
	lines := strings.Split(strip, "\n")
	if len(lines) != n {
		t.Fatalf("reported %d lines, rendered %d", n, len(lines))
	}
	if n != maxStripLines {
		t.Errorf("long summary should fill the cap: %d lines, want %d", n, maxStripLines)
	}

	// A cramped peek limits how far the strip may grow.
	m.peek = viewport.New(60, 4)
	if _, n := m.summaryStrip(); n != summaryStripLines+1 {
		t.Errorf("cramped peek: strip = %d lines, want %d (only one spare line)", n, summaryStripLines+1)
	}
}

func TestStateRamp(t *testing.T) {
	// needs-you is the only colored state; the rest sit on the grayscale ramp
	// in brightness order: archived < stopped < idle < working.
	if stateColor("needs-you") != colorAccent {
		t.Errorf("needs-you = %v, want the accent", stateColor("needs-you"))
	}
	order := []string{"archived", "stopped", "idle", "working"}
	rung := func(c lipgloss.Color) int {
		for i, r := range grayRamp {
			if r == c {
				return i
			}
		}
		t.Fatalf("color %v is not on the gray ramp", c)
		return -1
	}
	for i := 1; i < len(order); i++ {
		lo, hi := stateColor(order[i-1]), stateColor(order[i])
		if rung(lo) >= rung(hi) {
			t.Errorf("%s (%v) should be dimmer than %s (%v)", order[i-1], lo, order[i], hi)
		}
	}
}

func TestPinnedBrightens(t *testing.T) {
	// A pin lifts the row one rung and never adds a background — pinned must
	// not look selected.
	for _, state := range []string{"working", "idle", "stopped", "archived"} {
		plain, _ := rowColors(boardRow{State: state}, false)
		pinned, bg := rowColors(boardRow{State: state, Pinned: true}, false)
		if pinned != brighten(plain) {
			t.Errorf("pinned %s fg = %v, want one rung over %v", state, pinned, plain)
		}
		if bg != "" {
			t.Errorf("pinned %s bg = %v, want none", state, bg)
		}
	}
	// The accent is not on the ramp: a pinned needs-you keeps it.
	if fg, _ := rowColors(boardRow{State: "needs-you", Pinned: true}, false); fg != colorAccent {
		t.Errorf("pinned needs-you fg = %v, want the accent", fg)
	}
	// The top rung has nowhere to go: pinned working under the cursor stays bright.
	if fg, _ := rowColors(boardRow{State: "working", Pinned: true}, true); fg != colorBright {
		t.Errorf("pinned working under cursor fg = %v, want the top rung", fg)
	}
}

func TestCursorBrightens(t *testing.T) {
	// The cursor lifts the fg one rung so dim rows stay readable on the band.
	for _, state := range []string{"working", "idle", "stopped", "archived"} {
		plain, _ := rowColors(boardRow{State: state}, false)
		fg, bg := rowColors(boardRow{State: state}, true)
		if fg != brighten(plain) {
			t.Errorf("cursor %s fg = %v, want one rung over %v", state, fg, plain)
		}
		if bg != colorCursorBg {
			t.Errorf("cursor %s bg = %v, want the cursor band", state, bg)
		}
	}
}

func TestPinAnyState(t *testing.T) {
	// Pinning is pure metadata, so any state returns a command — including a
	// working agent (unlike archive) and an archived one.
	for _, state := range []string{"working", "stopped", "archived"} {
		m := model{mode: modeBoard, archivedView: state == "archived",
			rows: []boardRow{{ID: "A", State: state, Tmux: "t"}}}
		if _, cmd := m.handleKey(runeKey('p')); cmd == nil {
			t.Errorf("pinning a %s agent should return a command", state)
		}
	}
}

func TestArchiveOnlyStopped(t *testing.T) {
	// A live agent (working) can't be archived: refused with an error, no command.
	working := model{mode: modeBoard, rows: []boardRow{{ID: "A", State: "working", Tmux: "t"}}}
	after, cmd := working.handleKey(runeKey('a'))
	if m := after.(model); !m.statusErr || cmd != nil {
		t.Errorf("archiving a working agent should be refused (statusErr=%v, cmd=%v)", m.statusErr, cmd != nil)
	}
	// A stopped agent archives (a command is returned).
	stopped := model{mode: modeBoard, rows: []boardRow{{ID: "A", State: "stopped", Tmux: "t"}}}
	if _, cmd := stopped.handleKey(runeKey('a')); cmd == nil {
		t.Error("archiving a stopped agent should return a command")
	}
	// An archived agent restores (a command is returned).
	archived := model{mode: modeBoard, archivedView: true, rows: []boardRow{{ID: "A", State: "archived", Tmux: "t"}}}
	if _, cmd := archived.handleKey(runeKey('a')); cmd == nil {
		t.Error("restoring an archived agent should return a command")
	}
}
