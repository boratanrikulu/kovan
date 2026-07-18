package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSummarizeScript(t *testing.T) {
	withTok := summarizeScript("claude", "/tok/company", "opus", "INSTRUCT")
	for _, want := range []string{`CLAUDE_CODE_OAUTH_TOKEN="$(cat '/tok/company')"`, "claude", "--model 'opus'", "-p 'INSTRUCT'"} {
		if !strings.Contains(withTok, want) {
			t.Errorf("script missing %q:\n%s", want, withTok)
		}
	}
	noTok := summarizeScript("claude", "", "opus", "INSTRUCT")
	if strings.Contains(noTok, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Errorf("no-token script should not inject a token: %s", noTok)
	}
	if noTok != "claude --model 'opus' -p 'INSTRUCT'" {
		t.Errorf("no-token script = %q", noTok)
	}
}

func TestStale(t *testing.T) {
	now := time.Now()
	if !stale(agentSummary{}, false, false) {
		t.Error("a missing summary is stale")
	}
	if stale(agentSummary{pending: true, at: now}, true, false) || stale(agentSummary{pending: true, at: now}, true, true) {
		t.Error("a pending summary is never re-fired")
	}
	if stale(agentSummary{at: now}, true, false) {
		t.Error("a fresh summary is not stale")
	}
	if !stale(agentSummary{at: now.Add(-staleWindow - time.Second)}, true, false) {
		t.Error("an old summary is stale")
	}
	if !stale(agentSummary{at: now}, true, true) {
		t.Error("force re-summarizes a fresh one")
	}
}

func TestFireSummaries(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	m := &model{run: runner.Tmux{}, monitor: map[string]agentSummary{}}
	rows := []boardRow{
		{ID: "A", Tmux: "tA", State: "working", SessionID: "sA"},
		{ID: "old", Tmux: "tOld", State: "archived", SessionID: "sOld"},
		{ID: "gone", Tmux: "tGone", State: "stopped", SessionID: "sGone"},
		{ID: "seeded", Tmux: "tSeed", State: "needs-you"},
	}
	cmds := m.fireSummaries(rows, false)
	if len(cmds) != 1 {
		t.Fatalf("want 1 cmd (the live, stale agent), got %d", len(cmds))
	}
	if !m.monitor["tA"].pending {
		t.Error("the fired agent should be marked pending")
	}
	if _, ok := m.monitor["tOld"]; ok {
		t.Error("archived agents must be skipped")
	}
	if _, ok := m.monitor["tGone"]; ok {
		t.Error("stopped agents must be skipped — their pane is gone")
	}
	if _, ok := m.monitor["tSeed"]; ok {
		t.Error("an agent with no session id has no transcript to digest — never fired")
	}
	if c := m.fireSummaries(rows, false); len(c) != 0 {
		t.Errorf("a pending agent should not re-fire: %d", len(c))
	}
}

func TestSummariesForTickBoardWide(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	m := &model{run: runner.Tmux{}, mode: modeBoard, monitor: map[string]agentSummary{}}
	m.rows = []boardRow{
		{ID: "A", Tmux: "tA", State: "working", SessionID: "sA"},
		{ID: "B", Tmux: "tB", State: "idle", SessionID: "sB"},
		{ID: "gone", Tmux: "tGone", State: "stopped", SessionID: "sGone"},
	}
	// The board keeps every live agent's summary warm, not just the selected
	// row's — the cursor sits on A, but B fires too.
	if cmds := m.summariesForTick(); len(cmds) != 2 {
		t.Fatalf("want 2 cmds (both live agents), got %d", len(cmds))
	}
	if _, ok := m.monitor["tGone"]; ok {
		t.Error("a stopped agent must not be summarized from the board tick")
	}
	if cmds := m.summariesForTick(); len(cmds) != 0 {
		t.Errorf("pending summaries must not re-fire on the next tick: %d", len(cmds))
	}
}

// TestNeedsSummaryOnNeedsYouFlip pins the freshness rule: a summary written
// before the agent blocked must be regenerated the moment it flips to
// needs-you, even inside the staleness window.
func TestNeedsSummaryOnNeedsYouFlip(t *testing.T) {
	m := &model{monitor: map[string]agentSummary{
		"t": {text: "was working on X", at: time.Now(), state: "working"},
	}}
	if m.needsSummary(boardRow{Tmux: "t", State: "working"}, false) {
		t.Error("fresh summary under the same state must not refire")
	}
	if !m.needsSummary(boardRow{Tmux: "t", State: "needs-you"}, false) {
		t.Error("a flip into needs-you must refire even a fresh summary")
	}
	m.monitor["t"] = agentSummary{text: "waiting for approval of git push", at: time.Now(), state: "needs-you"}
	if m.needsSummary(boardRow{Tmux: "t", State: "needs-you"}, false) {
		t.Error("already summarized under needs-you: no refire")
	}
}

func TestSummaryText(t *testing.T) {
	m := model{monitor: map[string]agentSummary{
		"p":   {pending: true},
		"e":   {err: true, text: "boom"},
		"ok":  {text: "doing the thing\nline two"},
		"re":  {pending: true, text: "was doing X"},
		"ree": {pending: true, err: true, text: "boom"},
	}}
	cases := map[string]string{
		"p":   "summarizing…",
		"e":   "(boom)",
		"x":   "— (S to monitor)",
		"ok":  "doing the thing line two", // oneLine collapses newlines
		"re":  "⟳ was doing X",            // refresh in flight keeps the old text
		"ree": "⟳ (boom)",
	}
	for tmux, want := range cases {
		if got := m.summaryText(tmux); got != want {
			t.Errorf("summaryText(%q) = %q, want %q", tmux, got, want)
		}
	}
}

func TestSummaryInput(t *testing.T) {
	writeTranscript(t, "sess-input-1",
		`{"type":"user","message":{"content":"fix the parser"}}`+"\n")

	// The digest is the only source; the pane is never consulted.
	row := boardRow{Title: "t", Mode: "code", State: "needs-you", SessionID: "sess-input-1", Tmux: "x"}
	in, ok := summaryInput(row)
	if !ok || !strings.Contains(in, "Recent conversation:\nyou: fix the parser") {
		t.Errorf("transcript input = %q, %v", in, ok)
	}
	if !strings.Contains(in, "state: needs-you") {
		t.Errorf("header must carry the board state: %q", in)
	}

	// No transcript yet: not summarizable.
	row.SessionID = "sess-none"
	if _, ok := summaryInput(row); ok {
		t.Error("no transcript should not summarize")
	}
}

func TestPersistSummary(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	man := &session.Manifest{ID: "x1", Tmux: "kovan-x1", Repo: "demo", State: "idle", CreatedAt: time.Now()}
	if err := man.Write(); err != nil {
		t.Fatal(err)
	}
	persistSummary("kovan-x1", "reviewing the diff")
	got, err := session.ReadByTmux("kovan-x1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "reviewing the diff" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.SummaryAt.IsZero() {
		t.Error("SummaryAt should be stamped")
	}
	persistSummary("kovan-ghost", "no manifest") // must not panic or create anything
}

func TestSeedSummaries(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	m := &model{
		rows: []boardRow{
			{Tmux: "t1", Summary: "persisted one", SummaryAt: at},
			{Tmux: "t2"}, // nothing persisted
			{Tmux: "t3", Summary: "old on disk", SummaryAt: at},
		},
		monitor: map[string]agentSummary{"t3": {text: "fresh in memory", at: time.Now()}},
	}
	m.seedSummaries()
	if s := m.monitor["t1"]; s.text != "persisted one" || !s.at.Equal(at) {
		t.Errorf("t1 not seeded with its real age: %+v", s)
	}
	if _, ok := m.monitor["t2"]; ok {
		t.Error("t2 has nothing to seed")
	}
	if s := m.monitor["t3"]; s.text != "fresh in memory" {
		t.Errorf("a live in-memory summary must win over the disk one: %+v", s)
	}
	// A seeded old summary is still stale, so the tick re-summarizes it.
	if !m.needsSummary(boardRow{Tmux: "t1"}, false) {
		t.Error("an hour-old seeded summary should count as stale")
	}
}

func TestMonitorListsOnlyRunning(t *testing.T) {
	m := model{run: runner.Tmux{}, monitor: map[string]agentSummary{}, width: 80}
	m.rows = []boardRow{
		{ID: "live1", Tmux: "t1", State: "working", Repo: "kovan", Title: "x"},
		{ID: "gone1", Tmux: "t2", State: "stopped", Repo: "kovan", Title: "y"},
		{ID: "live2", Tmux: "t3", State: "needs-you", Repo: "kovan", Title: "z"},
		{ID: "old1", Tmux: "t4", State: "archived", Repo: "kovan", Title: "w"},
	}
	content := m.monitorContent()
	for _, want := range []string{"live1", "live2"} {
		if !strings.Contains(content, want) {
			t.Errorf("monitor should list running agent %q:\n%s", want, content)
		}
	}
	for _, skip := range []string{"gone1", "old1"} {
		if strings.Contains(content, skip) {
			t.Errorf("monitor must not list %q:\n%s", skip, content)
		}
	}
	if view := m.monitorView(); !strings.Contains(view, "2 running") {
		t.Errorf("header should count the running agents: %q", view)
	}
}

func TestWrapLines(t *testing.T) {
	lines := wrapLines("alpha beta gamma delta", 11, 2)
	if len(lines) != 2 || lines[0] != "alpha beta" || lines[1] != "gamma delta" {
		t.Fatalf("wrapLines = %v", lines)
	}
	for _, l := range lines {
		if len(l) > 11 {
			t.Errorf("line over width: %q", l)
		}
	}
	if l := wrapLines("a b c d e f", 1, 2); len(l) != 2 {
		t.Errorf("should cap at max lines: %v", l)
	}
}

func TestMonitorScrolls(t *testing.T) {
	m := model{run: runner.Tmux{}, mode: modeBoard, monitor: map[string]agentSummary{}}
	for i := 0; i < 20; i++ {
		m.rows = append(m.rows, boardRow{
			ID:    fmt.Sprintf("a%02d", i),
			Tmux:  fmt.Sprintf("t%02d", i),
			State: "working", Repo: "kovan", Title: "task",
		})
	}
	mdl, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m = mdl.(model)
	mdl, _ = m.Update(runeKey('S'))
	m = mdl.(model)
	if m.mode != modeMonitor {
		t.Fatal("S should open the monitor page")
	}

	view := m.View()
	if got := strings.Count(view, "\n") + 1; got > m.height {
		t.Fatalf("monitor view is %d lines on a %d-line terminal — must scroll, not overflow", got, m.height)
	}
	if !strings.Contains(view, "a00") {
		t.Fatalf("first agent should be visible on open:\n%s", view)
	}
	if strings.Contains(view, "a19") {
		t.Fatal("last agent can't fit a 12-line terminal — it should start off-screen")
	}

	for i := 0; i < 30 && !strings.Contains(m.View(), "a19"); i++ {
		mdl, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = mdl.(model)
	}
	view = m.View()
	if !strings.Contains(view, "a19") {
		t.Fatalf("PgDn never reached the last agent:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "esc/S back") {
		t.Errorf("hint line must stay pinned at the bottom, got %q", last)
	}
}

func TestWrapJoin(t *testing.T) {
	lines := wrapJoin([]string{"aa", "bb", "cc", "dd"}, " · ", 7, 2)
	if len(lines) != 2 || lines[0] != "aa · bb" || lines[1] != "cc · dd" {
		t.Errorf("wrapJoin = %v", lines)
	}
}
