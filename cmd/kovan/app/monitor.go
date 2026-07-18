package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// monitorRefresh is how often the open monitor page re-summarizes the active set.
const monitorRefresh = 60 * time.Second

// staleWindow is how long a cached summary is trusted before it's re-generated.
const staleWindow = 2 * time.Minute

// summarySem caps how many `claude -p` summarizers run at once, so opening the
// monitor over many agents doesn't spawn a process per agent simultaneously.
var summarySem = make(chan struct{}, 4)

// monitorInstruction is the one-shot prompt; the agent's conversation digest
// is piped in.
const monitorInstruction = "You are monitoring another coding agent. Piped in below is " +
	"a digest of its recent conversation: 'you:' lines are what the human actually " +
	"sent, 'agent:' lines are the agent's replies, 'agent ran' lines are its tool " +
	"calls. The 'state' value in the header is authoritative for whether it waits on " +
	"the human: needs-you means blocked on a decision or approval, idle means its turn " +
	"is finished, working means mid-task. In one or two sentences, say what the agent " +
	"is doing right now and whether it needs anything from me (a decision, an " +
	"approval, or an answer). Be concrete and terse. Output only the summary, no " +
	"preamble."

// agentSummary is a cached per-agent summary. pending marks one in flight;
// state is the board state the summary was generated under, so a transition
// into needs-you can invalidate text written before the agent blocked.
type agentSummary struct {
	text    string
	at      time.Time
	err     bool
	pending bool
	state   string
}

// summaryMsg carries a finished summary back to the model.
type summaryMsg struct {
	tmux  string
	text  string
	err   bool
	state string // board state the summary was generated under
}

// stale reports whether an agent needs a fresh summary: none cached, or the
// cached one is neither pending nor recent. force ignores recency (but still
// skips one already in flight).
func stale(s agentSummary, ok bool, force bool) bool {
	if !ok {
		return true
	}
	if s.pending {
		return false
	}
	return force || time.Since(s.at) >= staleWindow
}

// summarizeScript builds the shell command that runs the summarizer under the
// agent's account: the token is read at exec time (never in argv/logs), the
// instruction is the prompt, and the conversation digest is piped on stdin by
// the caller.
func summarizeScript(agent, tokenFile, model, instruction string) string {
	cmd := agent
	if model != "" {
		cmd += " --model " + shellQuote(model)
	}
	cmd += " -p " + shellQuote(instruction)
	if tokenFile != "" {
		return "CLAUDE_CODE_OAUTH_TOKEN=\"$(cat " + shellQuote(tokenFile) + ")\" " + cmd
	}
	return cmd
}

// summarizeCmd summarizes an agent's recent conversation with a one-shot
// `claude -p` under the agent's own account. Concurrency is capped.
func summarizeCmd(global *config.Global, row boardRow) tea.Cmd {
	return func() tea.Msg {
		input, ok := summaryInput(row)
		if !ok {
			return summaryMsg{tmux: row.Tmux, err: true, text: "no conversation yet", state: row.State}
		}
		tokenFile, ferr := accountTokenFile(global, row.Account)
		if ferr != nil {
			tokenFile = "" // fall back to the logged-in account rather than failing
		}
		summarySem <- struct{}{}
		defer func() { <-summarySem }()

		// Run in the agent's worktree so the summarizer loads the same context the
		// agent has (its CLAUDE.md / CLAUDE.local.md → the method), for a better-
		// grounded read. KOVAN_MONITOR makes the gate hook a no-op for this helper
		// (the hook resolves agents by cwd; without the guard its Stop → idle would
		// corrupt the agent's manifest).
		script := summarizeScript(global.Agent, tokenFile, global.Monitor.Model, monitorInstruction)
		cmd := exec.Command("sh", "-c", script)
		cmd.Stdin = strings.NewReader(input)
		cmd.Dir = row.Worktree
		cmd.Env = append(os.Environ(), "KOVAN_MONITOR=1")
		out, err := cmd.Output()
		if err != nil {
			return summaryMsg{tmux: row.Tmux, err: true, text: "summary failed", state: row.State}
		}
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = "(no summary)"
		}
		persistSummary(row.Tmux, text)
		return summaryMsg{tmux: row.Tmux, text: text, state: row.State}
	}
}

// summaryInput builds the summarizer's stdin: the agent's task header, then
// its conversation digest. The digest comes from the transcript — real turns
// only, so the unsent draft the pane's input box may show can never read as a
// sent prompt. False when the session has no transcript to digest yet.
func summaryInput(row boardRow) (string, bool) {
	d := transcriptDigest(row.SessionID)
	if d == "" {
		return "", false
	}
	head := fmt.Sprintf("Agent task: %s (mode: %s, state: %s)", row.Title, orDash(row.Mode), orDash(row.State))
	return head + "\n\nRecent conversation:\n" + d, true
}

// persistSummary stamps the summary and its time into the agent's manifest, so
// anything scanning the session index (other agents, scripts) can read what
// each agent is doing without a tmux capture. Only real summaries land here —
// capture/summarize errors never overwrite the last good one — and a write
// failure is ignored: the summary still shows from memory.
func persistSummary(tmux, text string) {
	m, err := session.ReadByTmux(tmux)
	if err != nil {
		return
	}
	m.Summary, m.SummaryAt = text, time.Now()
	_ = m.Write()
}

// seedSummaries adopts the manifests' persisted summaries for agents the
// in-memory cache hasn't seen (a fresh cockpit), stamped with their real age
// so staleness stays honest. A live in-memory entry always wins.
func (m *model) seedSummaries() {
	if m.monitor == nil {
		m.monitor = map[string]agentSummary{}
	}
	for _, r := range m.rows {
		if r.Summary == "" {
			continue
		}
		if _, ok := m.monitor[r.Tmux]; !ok {
			m.monitor[r.Tmux] = agentSummary{text: r.Summary, at: r.SummaryAt}
		}
	}
}

// liveRows are the rows with a running session — the monitor's scope, and the
// set the board keeps summaries warm for.
func (m model) liveRows() []boardRow {
	var out []boardRow
	for _, r := range m.rows {
		if r.State != "archived" && r.State != "stopped" {
			out = append(out, r)
		}
	}
	return out
}

// needsSummary: the cached summary is stale, or the agent has flipped into
// needs-you since it was written — the moment an agent blocks is exactly when
// the text must say what it's waiting for, not what it was doing before.
func (m *model) needsSummary(row boardRow, force bool) bool {
	s, ok := m.monitor[row.Tmux]
	if stale(s, ok, force) {
		return true
	}
	return row.State == "needs-you" && s.state != "needs-you" && !s.pending
}

// fireSummaries marks the given agents pending and returns their summarize
// commands, skipping any with a fresh or in-flight summary (unless force).
// Archived and stopped agents are always skipped: nothing new is happening in
// their conversations, and their last summary is worth keeping. So is an agent
// with no session id — no transcript can ever exist for it, so a run could only
// yield "no conversation yet" and clobber a persisted summary.
func (m *model) fireSummaries(rows []boardRow, force bool) []tea.Cmd {
	global, err := config.LoadGlobal()
	if err != nil {
		return nil
	}
	if m.monitor == nil {
		m.monitor = map[string]agentSummary{}
	}
	var cmds []tea.Cmd
	for _, r := range rows {
		if r.SessionID == "" || r.State == "archived" || r.State == "stopped" || !m.needsSummary(r, force) {
			continue
		}
		s := m.monitor[r.Tmux]
		s.pending, s.at, s.state = true, time.Now(), r.State
		m.monitor[r.Tmux] = s
		cmds = append(cmds, summarizeCmd(global, r))
	}
	return cmds
}

// summariesForTick fires on the refresh tick: the live agents on the board
// when their summaries go stale, or the active set on the monitor page every
// monitorRefresh. Bounded — staleWindow gates each agent to one summary per
// window and summarySem caps the concurrent runs, so this is never a fan-out
// of every agent on every tick.
func (m *model) summariesForTick() []tea.Cmd {
	switch m.mode {
	case modeBoard:
		return m.fireSummaries(m.liveRows(), false)
	case modeMonitor:
		if time.Since(m.monitorAt) >= monitorRefresh {
			m.monitorAt = time.Now()
			return m.fireSummaries(m.liveRows(), false)
		}
	}
	return nil
}

func (m model) handleMonitorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The page has no cursor, so j/k scroll lines; PgUp/PgDn mirror the method view.
	switch {
	case key.Matches(msg, keys.PageUp):
		m.monitorVP.HalfViewUp()
		return m, nil
	case key.Matches(msg, keys.PageDown):
		m.monitorVP.HalfViewDown()
		return m, nil
	case key.Matches(msg, keys.Up):
		m.monitorVP.ScrollUp(1)
		return m, nil
	case key.Matches(msg, keys.Down):
		m.monitorVP.ScrollDown(1)
		return m, nil
	}

	switch msg.String() {
	case "esc", "S", "q", "m":
		m.mode = modeBoard
		return m, nil
	case "r":
		m.monitorAt = time.Now()
		return m, tea.Batch(m.fireSummaries(m.liveRows(), true)...)
	}
	return m, nil
}

// oneLine collapses whitespace so a multi-sentence summary fits one row.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// summaryText is the display text for an agent's cached summary. A refresh in
// flight keeps the last summary readable behind a ⟳ marker; only a first-ever
// summary shows the placeholder.
func (m model) summaryText(tmux string) string {
	s, ok := m.monitor[tmux]
	switch {
	case !ok:
		return "— (S to monitor)"
	case s.pending && s.text == "":
		return "summarizing…"
	case s.pending:
		return "⟳ " + cachedSummary(s)
	default:
		return cachedSummary(s)
	}
}

// cachedSummary renders a completed summary: errors parenthesized, prose on
// one line.
func cachedSummary(s agentSummary) string {
	if s.err {
		return "(" + s.text + ")"
	}
	return oneLine(s.text)
}

// summaryStripLines is the strip's reserved height, the minimum it renders at;
// panelHeights budgets for exactly this many lines. A longer summary grows the
// strip up to maxStripLines and the peek gives up the difference — a summary
// is for reading, never cut mid-thought.
const summaryStripLines = 3

// maxStripLines caps how far the strip can grow into the peek's space.
const maxStripLines = 7

// stripBudget is how many lines the strip may use right now: its reserve plus
// whatever the peek can spare while keeping a readable minimum.
func (m model) stripBudget() int {
	spare := m.peek.Height - 3
	if spare < 0 {
		spare = 0
	}
	budget := summaryStripLines + spare
	if budget > maxStripLines {
		budget = maxStripLines
	}
	return budget
}

// summaryStrip is the selected agent's summary, wrapped in full above the
// board's peek panel, always muted. When the selected agent waits on you, the
// strip's first line becomes a one-line accent banner — the only accent
// element in the summary/peek area. Returns the strip and its line count, so
// the view can shrink the peek by the lines the strip grew.
func (m model) summaryStrip() (string, int) {
	row := m.current()
	text := "summary · —"
	if row != nil {
		text = "summary · " + m.summaryText(row.Tmux)
	}
	var out []string
	budget := m.stripBudget()
	if row != nil && row.State == "needs-you" {
		out = append(out, accentStyle.Render(clip("▌ waiting on you", m.width)))
		budget--
	}
	lines := wrapLines(text, m.width, budget)
	for len(out)+len(lines) < summaryStripLines {
		lines = append(lines, "")
	}
	for _, l := range lines {
		out = append(out, dimStyle.Render(l))
	}
	return strings.Join(out, "\n"), len(out)
}

// wrapLines word-wraps s to width across at most max lines (a long word is hard-
// clipped). Widths are counted in runes, since summaries carry multibyte glyphs
// (— … ·) that byte length would miscount.
func wrapLines(s string, width, max int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			if len(lines) == max {
				return lines
			}
			cur = w
		}
	}
	if cur != "" && len(lines) < max {
		lines = append(lines, cur)
	}
	for i := range lines {
		lines[i] = clip(lines[i], width)
	}
	return lines
}

// clip shortens s to at most width runes (with an ellipsis), without splitting a
// multibyte rune the way the byte-based truncate can.
func clip(s string, width int) string {
	if width < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}

// monitorContent is the scrollable body of the S page: a block per running
// agent — a colored header line (state · id · repo) and its summary wrapped
// below — so the full summary is readable. Stopped agents have nothing to
// monitor and are left to the board.
func (m model) monitorContent() string {
	rows := m.liveRows()
	if len(rows) == 0 {
		return "\n" + dimStyle.Render("  no running agents")
	}
	const indent = "    "
	var lines []string
	for _, r := range rows {
		left := fmt.Sprintf("%-9s %-12s %-16s ", r.State, truncate(r.ID, 12), truncate(r.Repo, 16))
		titleW := m.width - utf8.RuneCountInString(left)
		head := lipgloss.NewStyle().Foreground(stateColor(r.State)).Render(left)
		if titleW > 0 {
			head += dimStyle.Render(clip(r.Title, titleW))
		}
		lines = append(lines, head)
		for _, l := range wrapLines(m.summaryText(r.Tmux), m.width-len(indent), 4) {
			lines = append(lines, indent+dimStyle.Render(l))
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// setMonitorContent re-renders the blocks into the viewport. SetContent keeps
// the scroll offset, so a refresh doesn't move the reader.
func (m *model) setMonitorContent() {
	m.monitorVP.SetContent(m.monitorContent())
}

// sizeMonitorViewport fills the monitor page between the pinned header and the
// bottom status + hint lines, and re-wraps the content to the new width.
func (m *model) sizeMonitorViewport() {
	if !m.ready {
		return
	}
	h := m.height - 3 // header(1) + status(1) + hint(1)
	if h < 1 {
		h = 1
	}
	m.monitorVP.Width, m.monitorVP.Height = m.width, h
	m.setMonitorContent()
}

// monitorView is the S page: the header pinned on top, the agent blocks in a
// scrolling viewport, status + hint pinned to the bottom.
func (m model) monitorView() string {
	return strings.Join([]string{
		brandHeader(fmt.Sprintf("monitor · %d running", len(m.liveRows()))),
		m.monitorVP.View(),
		m.statusLine(),
		dimStyle.Render("r refresh · j/k · PgUp/PgDn scroll · esc/S back · summaries: claude -p, per account"),
	}, "\n")
}
