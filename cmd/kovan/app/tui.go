package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// windowTitle is the terminal tab/window title the board claims, so returning
// from an agent (whose tmux session set the title to its window name) restores
// the kovan title instead of leaving the agent's.
const windowTitle = "kovan"

const (
	refreshInterval = 1500 * time.Millisecond
	peekLines       = 14
	// Status toasts: info fades on its own; an error persists and is dismissed by
	// a page switch, but never before errorFloor — so a failure isn't missed.
	infoTTL    = 5 * time.Second
	errorFloor = 8 * time.Second
)

// infoExpired reports whether an info message of the given age should auto-clear
// on a tick. Errors never expire on the tick.
func infoExpired(isErr bool, age time.Duration) bool {
	return !isErr && age >= infoTTL
}

// keepErrorOnSwitch reports whether a message must survive a page switch: an
// error still within its floor. Info (and old errors) clear on a switch.
func keepErrorOnSwitch(isErr bool, age time.Duration) bool {
	return isErr && age < errorFloor
}

// action is the intent behind a keystroke on the board, decided separately from
// rendering so the keymap is unit-testable and future per-agent commands
// (review, fork) bind as one more case.
type action int

const (
	actNone action = iota
	actUp
	actDown
	actNew
	actOpen
	actRemove
	actHelp
	actQuit
	actMethod
	actEdit
	actMerge
	actTerminal
	actNotes
	actConfigure
	actFilter
	actArchive
	actPin
	actToggleView
	actMonitor
	actColumns
)

// keyMap is the whole keymap in one place; it drives both key matching and the
// bubbles/help bar.
type keyMap struct {
	Up, Down              key.Binding
	New, Open, Remove     key.Binding
	Method                key.Binding
	Edit, Merge, Terminal key.Binding
	Notes                 key.Binding
	Configure             key.Binding
	Filter, Archive       key.Binding
	Pin                   key.Binding
	ToggleView, Monitor   key.Binding
	Columns               key.Binding
	Help, Quit            key.Binding
	PageUp, PageDown      key.Binding
}

var keys = keyMap{
	Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "move")),
	Down:       key.NewBinding(key.WithKeys("j", "down")),
	New:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Open:       key.NewBinding(key.WithKeys("enter", "o"), key.WithHelp("enter", "open")),
	Remove:     key.NewBinding(key.WithKeys("d", "x"), key.WithHelp("d", "remove")),
	Method:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "method")),
	Edit:       key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "editor")),
	Merge:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "merge")),
	Terminal:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "terminal")),
	Notes:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "notes")),
	Configure:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "edit")),
	Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Archive:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Pin:        key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pin")),
	ToggleView: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "active/archived")),
	Monitor:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "monitor")),
	Columns:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "columns")),
	Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	PageUp:     key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("PgUp/PgDn", "scroll peek")),
	PageDown:   key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
}

// ShortHelp and FullHelp satisfy help.KeyMap for the status bar and help screen.
func (k keyMap) ShortHelp() []key.Binding {
	// tab (active/archived) is omitted here — the header tab bar already advertises
	// it; it stays in the full help (?). Keeps the always-on bar from overflowing.
	return []key.Binding{k.Up, k.New, k.Open, k.Filter, k.Configure, k.Pin, k.Archive, k.Remove, k.Method, k.Monitor, k.Columns, k.Edit, k.Merge, k.Terminal, k.Notes, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp},
		{k.New, k.Open, k.Configure, k.Pin, k.Archive, k.Remove, k.Method, k.Monitor},
		{k.Filter, k.ToggleView, k.Columns, k.Edit, k.Merge, k.Terminal, k.Notes},
		{k.Help, k.Quit},
	}
}

// action maps a keystroke to a board action, decided separately from rendering
// so the keymap stays unit-testable.
func (k keyMap) action(msg tea.KeyMsg) action {
	switch {
	case key.Matches(msg, k.Up):
		return actUp
	case key.Matches(msg, k.Down):
		return actDown
	case key.Matches(msg, k.New):
		return actNew
	case key.Matches(msg, k.Open):
		return actOpen
	case key.Matches(msg, k.Remove):
		return actRemove
	case key.Matches(msg, k.Method):
		return actMethod
	case key.Matches(msg, k.Edit):
		return actEdit
	case key.Matches(msg, k.Merge):
		return actMerge
	case key.Matches(msg, k.Terminal):
		return actTerminal
	case key.Matches(msg, k.Notes):
		return actNotes
	case key.Matches(msg, k.Configure):
		return actConfigure
	case key.Matches(msg, k.Filter):
		return actFilter
	case key.Matches(msg, k.Archive):
		return actArchive
	case key.Matches(msg, k.Pin):
		return actPin
	case key.Matches(msg, k.ToggleView):
		return actToggleView
	case key.Matches(msg, k.Monitor):
		return actMonitor
	case key.Matches(msg, k.Columns):
		return actColumns
	case key.Matches(msg, k.Help):
		return actHelp
	case key.Matches(msg, k.Quit):
		return actQuit
	}
	return actNone
}

// panelHeights splits the terminal height between the board table and the peek
// viewport, after the fixed rows: header, board header, peek title, status (4),
// plus the multi-line summary strip and the wrapped help bar.
func panelHeights(total int) (board, peek int) {
	avail := total - 4 - summaryStripLines - helpLineRows
	if avail < 2 {
		return 1, 1
	}
	peek = avail / 3
	if peek < 3 {
		peek = 3
	}
	if peek > avail-3 {
		peek = avail - 3
	}
	if peek < 1 {
		peek = 1
	}
	board = avail - peek
	if board < 1 {
		board = 1
	}
	return board, peek
}

type uiMode int

const (
	modeBoard uiMode = iota
	modeHelp
	modeForm
	modeConfirm
	modeMethod
	modeEdit
	modeFilter
	modeMonitor
	modeColumns
)

type model struct {
	run       runner.Runner
	rows      []boardRow
	cursor    int
	peek      viewport.Model
	help      help.Model
	mode      uiMode
	form      formModel
	edit      editModel // the edit modal (modeEdit)
	confirmID string    // agent pending removal in modeConfirm

	filter       string // live board filter (modeFilter); "" shows everything
	archivedView bool   // the board shows the archived tab instead of the active one

	// modeColumns: the board layout (column order + visibility, persisted to
	// ~/.kovan/board.yaml) and the overlay's cursor over the ordered columns.
	layout    boardLayout
	colCursor int

	// modeMethod: the inspected agent's effective layers, a flat cursor over the
	// real files (empty layers are skipped), and an isolated viewport for the
	// focused file — separate from peek so board ticks don't clobber it.
	mctx         methodCtx
	methodLayers []method.Layer
	methodGates  []string // gate summary lines shown in the method inspector
	methodFile   int
	methodVP     viewport.Model

	// modeMonitor: per-agent summaries (board strip + the S table), cached
	// in-memory and refreshed while the page is open.
	monitor   map[string]agentSummary // keyed by tmux name
	monitorAt time.Time               // last full monitor-page refresh
	monitorVP viewport.Model          // scrolls the agent blocks when they overflow

	// Double-click detection on the board: the last clicked visible row and when.
	lastClickRow int
	lastClickAt  time.Time

	account   string    // default account label for the header, resolved once
	status    string    // transient message shown in the status bar
	statusErr bool      // status is an error (red) vs informational (green)
	statusAt  time.Time // when status was set, for toast auto-dismissal
	boardRows int       // body rows the board may show, from the last resize
	width     int
	height    int
	ready     bool // a WindowSizeMsg has arrived; panels are sized
}

type tickMsg time.Time

type boardMsg struct {
	rows []boardRow
	peek string
	err  error
}

// handoffReadyMsg carries the prepared attach command from openTarget so Update
// can either ExecProcess it (blocking) or run it inline (switch-client).
type handoffReadyMsg struct {
	id       string
	woke     bool
	cmd      *exec.Cmd
	blocking bool
	err      error
}

type handoffDoneMsg struct{ err error }

type startedMsg struct {
	id  string
	err error
}

type editedMsg struct {
	id  string
	err error
}

type removedMsg struct {
	msg string
	err error
}

type archivedMsg struct {
	msg string
	err error
}

type pinnedMsg struct {
	msg string
	err error
}

func newModel() (model, error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return model{}, err
	}
	run, err := newRunner(global.Runner)
	if err != nil {
		return model{}, err
	}
	return model{run: run, mode: modeBoard, account: global.DefaultAccount, layout: loadLayout(), help: help.New(), peek: viewport.New(0, 0), methodVP: viewport.New(0, 0), monitorVP: viewport.New(0, 0), monitor: map[string]agentSummary{}}, nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick(), tea.SetWindowTitle(windowTitle))
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// refresh reloads the board and captures only the selected agent's output, so a
// crowded board costs one capture per tick.
func (m model) refresh() tea.Cmd {
	run := m.run
	var selected string
	if row := m.current(); row != nil {
		selected = row.Tmux
	}
	return func() tea.Msg {
		rows, err := loadBoard()
		if err != nil {
			return boardMsg{err: err}
		}
		peek := ""
		if selected != "" {
			if out, e := run.Capture(selected, peekLines); e == nil {
				peek = out
			}
		}
		return boardMsg{rows: rows, peek: peek}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		board, peek := panelHeights(msg.Height)
		m.boardRows = board
		m.peek.Width, m.peek.Height = msg.Width, peek
		m.help.Width = msg.Width
		m.ready = true
		// The method contents pane fills below its (variable) layer list, so it
		// is sized from the list height, not the fixed peek split.
		m.sizeMethodViewport()
		m.sizeMonitorViewport()
		m.sizeForm()
		return m, nil
	case tickMsg:
		if m.status != "" && infoExpired(m.statusErr, time.Since(m.statusAt)) {
			m.status = ""
		}
		cmds := append([]tea.Cmd{m.refresh(), tick()}, m.summariesForTick()...)
		return m, tea.Batch(cmds...)
	case summaryMsg:
		if m.monitor == nil {
			m.monitor = map[string]agentSummary{}
		}
		m.monitor[msg.tmux] = agentSummary{text: msg.text, at: time.Now(), err: msg.err, state: msg.state}
		if m.mode == modeMonitor {
			m.setMonitorContent()
		}
		return m, nil
	case boardMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.rows = msg.rows
		m.seedSummaries()
		m.clampCursor()
		m.peek.SetContent(msg.peek)
		m.peek.GotoBottom()
		if m.mode == modeMonitor {
			m.setMonitorContent()
		}
		return m, nil
	case handoffReadyMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		if msg.woke {
			m.setInfo("waking agent " + msg.id + "…")
		}
		if msg.blocking {
			return m, tea.ExecProcess(withTitleReset(msg.cmd, windowTitle), func(err error) tea.Msg { return handoffDoneMsg{err} })
		}
		cmd := msg.cmd
		return m, func() tea.Msg { return handoffDoneMsg{cmd.Run()} }
	case handoffDoneMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		}
		// Returning from an agent: its tmux session left the terminal title set to
		// its window name, so reclaim the board's title. Mouse reporting must be
		// re-enabled too — bubbletea's RestoreTerminal brings back the alt screen
		// but not the mouse mode, so it dies with every ExecProcess round-trip.
		return m, tea.Batch(m.refresh(), tea.SetWindowTitle(windowTitle), tea.EnableMouseAllMotion)
	case startedMsg:
		if msg.err != nil {
			// Stay on the form (mode is unchanged) so the brief and fields survive;
			// the error shows in the form's status line and the user can retry.
			m.setErr(msg.err)
			return m, nil
		}
		m.mode = modeBoard
		m.setInfo("started " + msg.id)
		return m, m.refresh()
	case editedMsg:
		if msg.err != nil {
			// Stay on the edit modal so nothing is lost; the error shows there.
			m.setErr(msg.err)
			return m, nil
		}
		m.mode = modeBoard
		m.setInfo("updated " + msg.id)
		return m, m.refresh()
	case removedMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.setInfo(msg.msg)
		return m, m.refresh()
	case archivedMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.setInfo(msg.msg)
		return m, m.refresh()
	case pinnedMsg:
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.setInfo(msg.msg)
		return m, m.refresh()
	case methodEditedMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		}
		m.loadMethodFile() // reload the focused file's contents on editor return
		// The editor ran via ExecProcess, which loses the mouse mode on resume.
		return m, tea.EnableMouseAllMotion
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) setErr(err error)     { m.setStatus(err.Error(), true) }
func (m *model) setErrMsg(msg string) { m.setStatus(msg, true) }
func (m *model) setInfo(msg string)   { m.setStatus(msg, false) }

// setStatus sets the transient message and stamps when, so it can auto-dismiss.
func (m *model) setStatus(msg string, isErr bool) {
	m.status, m.statusErr, m.statusAt = msg, isErr, time.Now()
}

// dismissOnSwitch clears the status on a page switch, unless it's an error still
// within its floor (which must stay at least errorFloor even across a switch).
func (m *model) dismissOnSwitch() {
	if keepErrorOnSwitch(m.statusErr, time.Since(m.statusAt)) {
		return
	}
	m.status, m.statusErr = "", false
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeHelp:
		m.mode = modeBoard
		return m, nil
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirm:
		return m.handleConfirmKey(msg)
	case modeMethod:
		return m.handleMethodKey(msg)
	case modeEdit:
		return m.handleEditKey(msg)
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeMonitor:
		return m.handleMonitorKey(msg)
	case modeColumns:
		return m.handleColumnsKey(msg)
	}

	// Peek scrolling is separate from board navigation so j/k stay on the board.
	switch {
	case key.Matches(msg, keys.PageUp):
		m.peek.HalfViewUp()
		return m, nil
	case key.Matches(msg, keys.PageDown):
		m.peek.HalfViewDown()
		return m, nil
	}

	switch keys.action(msg) {
	case actUp:
		if m.cursor > 0 {
			m.cursor--
			return m, m.refresh() // refresh the peek for the new selection
		}
	case actDown:
		if m.cursor < len(m.visible())-1 {
			m.cursor++
			return m, m.refresh()
		}
	case actNew:
		m.mode = modeForm
		m.form = newForm()
		m.sizeForm()
		m.dismissOnSwitch()
	case actOpen:
		if row := m.current(); row != nil {
			return m, m.openCmd(row.ID)
		}
	case actRemove:
		if row := m.current(); row != nil {
			m.mode = modeConfirm
			m.confirmID = row.ID
			m.dismissOnSwitch()
		}
	case actMethod:
		return m.enterMethod()
	case actEdit:
		m.openApp(func(a config.Apps) string { return a.Editor }, "editor")
	case actMerge:
		m.openApp(func(a config.Apps) string { return a.Merge }, "merge")
	case actTerminal:
		m.openTerminalApp()
	case actNotes:
		m.openNotesApp()
	case actConfigure:
		if row := m.current(); row != nil {
			m.edit = newEditForm(*row)
			m.mode = modeEdit
			m.dismissOnSwitch()
		}
	case actFilter:
		m.mode = modeFilter
		m.dismissOnSwitch()
	case actArchive:
		if row := m.current(); row != nil {
			switch row.State {
			case "archived":
				return m, archiveCmd(row.ID, false) // restore
			case "stopped":
				return m, archiveCmd(row.ID, true) // safe: no live session to tear down
			default:
				m.setErrMsg("only a stopped agent can be archived — exit its session first")
			}
		}
	case actPin:
		if row := m.current(); row != nil {
			return m, pinCmd(row.ID, !row.Pinned)
		}
	case actToggleView:
		m.archivedView = !m.archivedView
		m.cursor = 0
		m.dismissOnSwitch()
	case actColumns:
		m.mode = modeColumns
		m.dismissOnSwitch()
	case actMonitor:
		m.mode = modeMonitor
		m.monitorAt = time.Now()
		m.dismissOnSwitch()
		cmds := m.fireSummaries(m.liveRows(), false)
		m.sizeMonitorViewport()
		m.monitorVP.GotoTop()
		return m, tea.Batch(cmds...)
	case actHelp:
		m.mode = modeHelp
		m.dismissOnSwitch()
	case actQuit:
		return m, tea.Quit
	}
	return m, nil
}

// handleFilterKey edits the live board filter: runes append, backspace deletes,
// enter applies and returns to the board (the filter stays active), esc clears
// and exits. The filtered list updates live as you type.
func (m model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter = ""
		m.mode = modeBoard
		m.clampCursor()
		return m, nil
	case "enter":
		m.mode = modeBoard
		return m, nil
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
		m.cursor = 0
		return m, m.refresh()
	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.cursor = 0
			return m, m.refresh()
		}
	}
	return m, nil
}

func (m model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// In full-screen brief, esc backs out to the fields rather than canceling
		// the whole form, so a long brief isn't lost to a stray keystroke.
		if m.form.briefFull {
			m.setBriefFull(false)
			return m, nil
		}
		m.mode = modeBoard
		return m, nil
	case "ctrl+f":
		m.setBriefFull(!m.form.briefFull)
		return m, nil
	case "ctrl+d":
		return m.submitForm()
	case "enter":
		// Enter confirms the focused field and advances, like tab; submit is ctrl+d.
		// In the brief textarea Enter is a newline, so let it fall through there.
		if !m.form.onBrief() {
			m.form.setFocus(m.form.focus + 1)
			return m, nil
		}
	case "ctrl+v":
		// macOS-only: capture a clipboard image and drop a [[image #N]] token at
		// the cursor. Off darwin, fall through so the input handles ctrl+v.
		if runtime.GOOS == "darwin" {
			m.attachClipboardImage()
			return m, nil
		}
	case "tab", "shift+tab":
		// Full-screen brief locks focus to the brief; the fields are hidden.
		if m.form.briefFull {
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.update(msg)
	return m, cmd
}

// setBriefFull toggles the full-screen brief: it focuses the brief (so keys go
// to the textarea) and resizes it to fill the terminal.
func (m *model) setBriefFull(full bool) {
	m.form.briefFull = full
	if full {
		m.form.setFocus(m.form.total() - 1) // the brief is always last
	}
	m.sizeForm()
}

// submitForm validates the form and launches the agent with its brief.
func (m model) submitForm() (tea.Model, tea.Cmd) {
	id, title := m.form.value(0), m.form.value(1)
	if title == "" {
		m.setErrMsg("title is required (id is optional — left blank, one is generated)")
		return m, nil
	}
	proj := m.form.project.value()
	if proj.root == "" {
		m.setErrMsg("choose a project (type a path to a repo)")
		return m, nil
	}
	// The target shapes how the agent lands: a new worktree off a base, the
	// checkout in-place, or a tab in an existing workspace (which inherits its
	// branch, so neither base nor in-place applies).
	var from, in string
	inPlace := false
	switch m.form.target {
	case targetCheckout:
		inPlace = true
		from = m.form.from.value()
	case targetTab:
		if in = m.form.workspaces.value(); in == "" {
			m.setErrMsg("no existing workspace to join — pick another target")
			return m, nil
		}
	default:
		from = m.form.from.value()
	}
	brief := briefInput{text: strings.TrimRight(m.form.brief.Value(), "\n"), images: m.form.images}
	// Stay on the form while the scaffold runs; startedMsg switches to the board
	// only on success, so a create error keeps the form and its brief intact.
	m.setInfo("creating agent…")
	return m, startBriefedCmd(proj.root, id, title, from, m.form.selectedAccount(), m.form.selectedMode(), m.form.selectedColor(), inPlace, in, brief)
}

func (m model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBoard
		return m, nil
	case "ctrl+d":
		return m.submitEdit()
	case "enter":
		if !m.edit.onTitle() {
			return m.submitEdit()
		}
	}
	var cmd tea.Cmd
	m.edit, cmd = m.edit.update(msg)
	return m, cmd
}

// submitEdit applies the edit modal's changes. It stays on the modal until the
// update succeeds, so an error keeps the fields intact.
func (m model) submitEdit() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.edit.title.Value())
	if title == "" {
		m.setErrMsg("title is required")
		return m, nil
	}
	m.setInfo("saving…")
	return m, editAgentCmd(m.edit.tmux, title, m.edit.selectedMode(), m.edit.selectedAccount(), m.edit.selectedColor())
}

func editAgentCmd(tmux, title, modeFlag, account, color string) tea.Cmd {
	return func() tea.Msg {
		id, err := editAgent(tmux, title, modeFlag, account, color)
		return editedMsg{id: id, err: err}
	}
}

// attachClipboardImage captures the clipboard image to a temp file, stashes it
// on the form, and inserts a [[image #N]] token at the brief cursor. It never
// fails the form — a missing image just sets a status.
func (m *model) attachClipboardImage() {
	f, err := os.CreateTemp("", "kovan-paste-*.png")
	if err != nil {
		m.setErrMsg("clipboard: " + err.Error())
		return
	}
	tmp := f.Name()
	f.Close()
	switch ok, err := captureClipboardImage(tmp); {
	case err != nil:
		os.Remove(tmp)
		m.setErrMsg("clipboard: " + err.Error())
	case !ok:
		os.Remove(tmp)
		m.setInfo("no image in clipboard")
	default:
		m.form.images = append(m.form.images, tmp)
		m.form.brief.InsertString(fmt.Sprintf("[[image #%d]]", len(m.form.images)))
		m.setInfo(fmt.Sprintf("image attached (%d)", len(m.form.images)))
	}
}

// openApp launches a configured external program (editor or git GUI) on the
// selected agent's worktree, detached. pick selects which command from the
// config; label names it for the status line. It never blocks the board.
func (m *model) openApp(pick func(config.Apps) string, label string) {
	row := m.current()
	if row == nil {
		return
	}
	if row.Worktree == "" {
		m.setErrMsg("no worktree for " + row.ID)
		return
	}
	global, err := config.LoadGlobal()
	if err != nil {
		m.setErr(err)
		return
	}
	if err := launchApp(pick(global.Apps), row.Worktree); err != nil {
		m.setErr(err)
		return
	}
	m.setInfo("opened " + label + " · " + row.ID)
}

// openTerminalApp opens a terminal in the selected agent's worktree (a new iTerm2
// tab by default; apps.terminal overrides), detached.
func (m *model) openTerminalApp() {
	row := m.current()
	if row == nil {
		return
	}
	if row.Worktree == "" {
		m.setErrMsg("no worktree for " + row.ID)
		return
	}
	global, err := config.LoadGlobal()
	if err != nil {
		m.setErr(err)
		return
	}
	if err := openTerminal(global.Apps, row.Worktree); err != nil {
		m.setErr(err)
		return
	}
	m.setInfo("opened terminal · " + row.ID)
}

// openNotesApp opens the selected agent's durable task-doc dir in the editor,
// detached. The dir is keyed per tab (by tmux name), so it's the selected tab's
// own notes even when siblings share a worktree.
func (m *model) openNotesApp() {
	row := m.current()
	if row == nil {
		return
	}
	if row.Tmux == "" {
		m.setErrMsg("no session for " + row.ID)
		return
	}
	global, err := config.LoadGlobal()
	if err != nil {
		m.setErr(err)
		return
	}
	notes, err := notesDirFor(row.Tmux, "")
	if err != nil {
		m.setErr(err)
		return
	}
	if err := launchApp(global.Apps.Editor, notes); err != nil {
		m.setErr(err)
		return
	}
	m.setInfo("opened notes · " + row.ID)
}

func (m model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		id := m.confirmID
		m.mode = modeBoard
		return m, removeAgentCmd(id)
	case "n", "esc":
		m.mode = modeBoard
	}
	return m, nil
}

// visible is the board as displayed: m.rows screened by the live filter and the
// show-archived toggle. The cursor indexes into this, so all selection and
// rendering go through it.
func (m model) visible() []boardRow {
	return filterRows(m.rows, m.filter, m.archivedView)
}

// clampCursor keeps the cursor within the visible list after a refresh or a
// filter change.
func (m *model) clampCursor() {
	if n := len(m.visible()); m.cursor >= n {
		m.cursor = max(0, n-1)
	}
}

func (m model) current() *boardRow {
	v := m.visible()
	if m.cursor < len(v) {
		return &v[m.cursor]
	}
	return nil
}

// openCmd prepares the handoff off the UI thread: it wakes the agent if needed
// and returns the attach command for Update to run.
func (m model) openCmd(id string) tea.Cmd {
	run := m.run
	return func() tea.Msg {
		name, woke, err := openTarget(id, run)
		if err != nil {
			return handoffReadyMsg{err: err}
		}
		cmd, blocking := run.AttachCmd(name)
		return handoffReadyMsg{id: id, woke: woke, cmd: cmd, blocking: blocking}
	}
}

// startBriefedCmd scaffolds the agent with the cockpit-composed brief and
// launches it in one step off the UI thread — no $EDITOR round-trip, since the
// brief was written in the form.
func startBriefedCmd(repoRoot, id, title, from, account, modeFlag, color string, inPlace bool, in string, brief briefInput) tea.Cmd {
	return func() tea.Msg {
		spec, err := scaffoldAgent(repoRoot, id, title, from, account, modeFlag, color, inPlace, in, brief)
		if err != nil {
			return startedMsg{id: id, err: err}
		}
		_, err = launchScaffolded(spec, false)
		return startedMsg{id: spec.manifest.ID, err: err}
	}
}

func removeAgentCmd(id string) tea.Cmd {
	return func() tea.Msg {
		msg, err := removeAgent(id, false)
		return removedMsg{msg: msg, err: err}
	}
}

// withTitleReset wraps an attach command so the terminal's icon+window title is
// reset to the board's after it exits. tmux's set-titles sets the title via OSC 0
// (which also names the iTerm tab) while attached; bubbletea only resets OSC 2 on
// resume, so the tab would keep the agent's name. We reclaim it with an OSC 0
// printf in the same shell — after tmux releases the terminal, before the board
// redraws (no concurrent write with the renderer).
func withTitleReset(cmd *exec.Cmd, title string) *exec.Cmd {
	quoted := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		quoted[i] = shellQuote(a)
	}
	script := fmt.Sprintf(`%s; printf '\033]0;%s\007'`, strings.Join(quoted, " "), title)
	wrapped := exec.Command("sh", "-c", script)
	wrapped.Env = cmd.Env
	wrapped.Dir = cmd.Dir
	return wrapped
}

func archiveCmd(id string, archived bool) tea.Cmd {
	return func() tea.Msg {
		msg, err := setArchived(id, archived)
		return archivedMsg{msg: msg, err: err}
	}
}

func pinCmd(id string, pinned bool) tea.Cmd {
	return func() tea.Msg {
		msg, err := setPinned(id, pinned)
		return pinnedMsg{msg: msg, err: err}
	}
}

func (m model) View() string {
	switch m.mode {
	case modeHelp:
		return m.centered(m.fullHelpView())
	case modeConfirm:
		body := fmt.Sprintf("Remove agent %s?  Its branch is kept.\n\n%s",
			m.confirmID, dimStyle.Render("y confirm · n cancel"))
		return m.centered(boxStyle.Render(body))
	}
	if !m.ready {
		return "loading…"
	}
	if m.mode == modeMethod {
		return m.methodView()
	}
	if m.mode == modeMonitor {
		return m.monitorView()
	}
	if m.mode == modeEdit {
		return m.centered(boxStyle.Render(m.edit.view(m.formStatusLine())))
	}
	if m.mode == modeColumns {
		return m.centered(boxStyle.Render(m.columnsView()))
	}
	if m.mode == modeForm {
		// Full-screen so a long brief has room; status + hints pinned at the bottom.
		if m.form.briefFull {
			return m.form.viewFull(m.formStatusLine())
		}
		return m.form.view(m.formStatusLine())
	}
	// header / board / summary strip / peek-title / peek / status / help — sized
	// to fill exactly. The strip grows to show its summary in full and the peek
	// gives up exactly those lines, so the total height never drifts. Status and
	// help are separate lines so the shortcuts stay visible while a transient
	// message is shown; the status line is blank when there's none.
	strip, stripLines := m.summaryStrip()
	peek := m.peek
	if extra := stripLines - summaryStripLines; extra > 0 {
		peek.Height -= extra
	}
	return strings.Join([]string{
		m.header(),
		boardView(m.visible(), m.cursor, m.width, m.boardRows, m.layout),
		strip,
		m.peekTitle(),
		peek.View(),
		m.statusLine(),
		m.helpLine(),
	}, "\n")
}

func (m model) centered(s string) string {
	if !m.ready {
		return s
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s)
}

func (m model) header() string {
	// The tabs carry their own styling and must not be re-dimmed, so they are
	// rendered outside the dim header context (the rest stays dim).
	line := brandMark() + "  " + dimStyle.Render("·") + " " + m.tabs()
	var extra []string
	if m.account != "" {
		extra = append(extra, m.account)
	}
	if m.filter != "" {
		extra = append(extra, "filter: "+m.filter)
	}
	if len(extra) > 0 {
		line += dimStyle.Render(" · " + strings.Join(extra, " · "))
	}
	return line
}

// tabs renders the active/archived tab bar with counts, the current one bright
// and the other dim — so the archived view (and the `tab` key to reach it) is
// visible and the active tab is unmistakable.
func (m model) tabs() string {
	archived := archivedCount(m.rows)
	active := len(m.rows) - archived
	activeLabel := fmt.Sprintf("active %d", active)
	archivedLabel := fmt.Sprintf("archived %d", archived)
	if m.archivedView {
		return dimStyle.Render(activeLabel) + "  " + tabOnStyle.Render(archivedLabel)
	}
	return tabOnStyle.Render(activeLabel) + "  " + dimStyle.Render(archivedLabel)
}

func (m model) peekTitle() string {
	label := "peek"
	if row := m.current(); row != nil {
		label = "peek · " + row.ID
	}
	return rule(label, m.width)
}

// statusLine is the transient message, styled by kind, or blank when there is
// none — so it never displaces the help line below it. While filtering, it shows
// the live filter prompt instead.
func (m model) statusLine() string {
	if m.mode == modeFilter {
		return cursorStyle.Render("/"+m.filter+"▌") + dimStyle.Render("   enter apply · esc clear")
	}
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return errStyle.Render(m.status)
	}
	return infoStyle.Render(m.status)
}

// helpLineRows is the fixed height of the shortcut bar: it wraps across this many
// lines so the full keymap shows instead of truncating with "…".
const helpLineRows = 1

// helpLine is the always-visible shortcut bar: a single line that always ends
// with "? help · q quit" and fills the space before it with as many of the
// other shortcuts (in priority order) as the width allows. The full keymap is on
// "?". This keeps the bar to one line at any width instead of wrapping.
// Key names sit a rung over their dim descriptions; width is counted on the
// plain text, the styling is render-only.
func (m model) helpLine() string {
	keyDesc := func(b key.Binding) string { h := b.Help(); return h.Key + " " + h.Desc }
	styled := func(b key.Binding) string {
		h := b.Help()
		return helpKeyStyle.Render(h.Key) + dimStyle.Render(" "+h.Desc)
	}
	sep := dimStyle.Render(" · ")
	tail := styled(keys.Help) + sep + styled(keys.Quit)
	used := utf8.RuneCountInString(keyDesc(keys.Help) + " · " + keyDesc(keys.Quit))
	var prefix []string
	for _, b := range keys.ShortHelp() {
		if b.Help().Key == keys.Help.Help().Key || b.Help().Key == keys.Quit.Help().Key {
			continue // help + quit are pinned in the tail
		}
		if used+utf8.RuneCountInString(keyDesc(b))+3 > m.width { // +3 for the " · " separator
			break
		}
		prefix = append(prefix, styled(b))
		used += utf8.RuneCountInString(keyDesc(b)) + 3
	}
	line := tail
	if len(prefix) > 0 {
		line = strings.Join(prefix, sep) + sep + tail
	}
	return line
}

// wrapJoin packs items into at most max lines, each joined by sep within width.
func wrapJoin(items []string, sep string, width, max int) []string {
	var lines []string
	cur := ""
	for _, it := range items {
		if cur == "" {
			cur = it
			continue
		}
		if utf8.RuneCountInString(cur)+utf8.RuneCountInString(sep)+utf8.RuneCountInString(it) <= width {
			cur += sep + it
			continue
		}
		lines = append(lines, cur)
		if len(lines) == max {
			return lines
		}
		cur = it
	}
	if cur != "" && len(lines) < max {
		lines = append(lines, cur)
	}
	return lines
}

// formStatusLine is the new-agent form's transient message (image attached,
// errors), or blank — the form reserves a line for it so the layout is stable.
func (m model) formStatusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return errStyle.Render(m.status)
	}
	return infoStyle.Render(m.status)
}

// sizeForm grows the form's brief textarea to fill the terminal below the
// fixed fields, so a long brief has room and the hints stay pinned at the bottom.
func (m *model) sizeForm() {
	// The form (and its textarea) only exists in modeForm; on the board it is the
	// zero value, whose textarea would nil-deref on SetWidth.
	if !m.ready || m.mode != modeForm {
		return
	}
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	m.form.brief.SetWidth(w)

	if m.form.briefFull {
		h := m.height - fullBriefChrome
		if h < 3 {
			h = 3
		}
		m.form.briefPad = 0
		m.form.brief.SetHeight(h)
		return
	}
	// Inline, the brief is a compact preview (it's written full-screen), capped
	// and padded so it has breathing room above and below rather than filling the
	// whole terminal. The leftover space becomes blank lines below it.
	avail := m.height - m.form.chromeLines()
	if avail < 4 {
		avail = 4
	}
	h := avail - 1 // at least one blank line of breathing room below
	if h > maxBriefRows {
		h = maxBriefRows
	}
	if h < 3 {
		h = 3
	}
	m.form.briefPad = avail - h
	m.form.brief.SetHeight(h)
}

func (m model) fullHelpView() string {
	h := m.help
	h.ShowAll = true
	return brandMark() + "\n\n" + h.View(keys)
}

// brandMark is the gold "kovan" chip — the same mark as the tmux status bar, so
// the board, the help screen, and the in-session bar all read as one product.
func brandMark() string { return brandStyle.Render(" kovan ") }

// brandHeader leads a view with the brand chip, then the dim context that says
// where you are (· N agents, · method · repo: …).
func brandHeader(context string) string {
	return brandMark() + "  " + dimStyle.Render("· "+context)
}

// boardColumn is one board column: its header and how a row renders in it.
// The order here is the board's fixed column order; which columns show is the
// user's choice, held in boardLayout and edited in the columns overlay (v).
type boardColumn struct {
	name string
	cell func(boardRow) string
}

var boardColumns = []boardColumn{
	{"STATE", func(r boardRow) string { return r.State }},
	{"ID", idCell},
	{"REPO", func(r boardRow) string { return r.Repo }},
	{"MODE", func(r boardRow) string { return orDash(r.Mode) }},
	{"AGE", func(r boardRow) string { return r.Age }},
	{"ACCOUNT", func(r boardRow) string { return orDash(r.Account) }},
	{"PERM", func(r boardRow) string { return orDash(r.Perm) }},
	{"WORKSPACE", workspaceCell},
	{"TITLE", func(r boardRow) string { return r.Title }},
}

// isBoardColumn reports whether name is a known column, for screening a loaded
// layout.
func isBoardColumn(name string) bool {
	for _, c := range boardColumns {
		if c.name == name {
			return true
		}
	}
	return false
}

// boardLayout is the user's board preferences: the column order and which
// columns are hidden. The zero value is the default board — every column on,
// in the built-in order.
type boardLayout struct {
	Order  []string // full column order by name; empty means the built-in order
	Hidden map[string]bool
}

// ordered is every column in the user's order, hidden ones included — the
// columns overlay lists them all. Columns the saved order doesn't know
// (added since it was written) keep their built-in position at the end.
func (l boardLayout) ordered() []boardColumn {
	if len(l.Order) == 0 {
		return boardColumns
	}
	out := make([]boardColumn, 0, len(boardColumns))
	seen := map[string]bool{}
	for _, n := range l.Order {
		for _, c := range boardColumns {
			if c.name == n && !seen[n] {
				out = append(out, c)
				seen[n] = true
			}
		}
	}
	for _, c := range boardColumns {
		if !seen[c.name] {
			out = append(out, c)
		}
	}
	return out
}

// columns is the visible column set in the user's order. Defensive: if
// everything is hidden (a hand-edited board.yaml), the full set renders.
func (l boardLayout) columns() []boardColumn {
	out := make([]boardColumn, 0, len(boardColumns))
	for _, c := range l.ordered() {
		if !l.Hidden[c.name] {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return boardColumns
	}
	return out
}

// headers is the visible column names, for the board's header row.
func (l boardLayout) headers() []string {
	cols := l.columns()
	hs := make([]string, len(cols))
	for i, c := range cols {
		hs[i] = c.name
	}
	return hs
}

func rowCells(cols []boardColumn, r boardRow) []string {
	cells := make([]string, len(cols))
	for i, c := range cols {
		cells[i] = c.cell(r)
	}
	return cells
}

// idCell renders the ID column: the id, prefixed with a marker when pinned.
// The marker is what says "pinned" everywhere — plain `kovan status` has no
// color at all.
func idCell(r boardRow) string {
	if r.Pinned {
		return "* " + r.ID
	}
	return r.ID
}

// workspaceCell renders the WORKSPACE column: the branch, prefixed with a tree
// marker on a sibling tab so a shared workspace reads as one cluster.
func workspaceCell(r boardRow) string {
	if r.Cont {
		return "└ " + r.Branch
	}
	return r.Branch
}

// boardView renders the board to exactly bodyRows+1 lines (header + body),
// coloring each row by state, highlighting the cursor, and windowing the rows
// so the cursor stays visible. It is ANSI-safe: each line is two self-
// contained styled segments (the stripe cell, then the rest).
func boardView(rows []boardRow, cursor, width, bodyRows int, layout boardLayout) string {
	if width <= 0 {
		width = 80
	}
	if bodyRows < 1 {
		bodyRows = 1
	}
	cols := layout.columns()
	widths := boardColumnWidths(cols, rows, width)
	header := headerStyle.Render("   " + formatRow(layout.headers(), widths))
	if len(rows) == 0 {
		empty := lipgloss.Place(width, bodyRows, lipgloss.Center, lipgloss.Center,
			dimStyle.Render("No agents yet — press n to start one."))
		return header + "\n" + empty
	}
	start := windowStart(cursor, len(rows), bodyRows)
	lines := make([]string, bodyRows)
	for i := range lines {
		if idx := start + i; idx < len(rows) {
			lines[i] = boardRowLine(rows[idx], cols, widths, idx == cursor)
		}
	}
	return header + "\n" + strings.Join(lines, "\n")
}

// boardRowLine is a stripe cell plus the state-styled row: [stripe][›][ ]
// then the cells. Both segments carry the selection background on the cursor
// row, so the band runs edge to edge with the stripe sitting on it.
func boardRowLine(r boardRow, cols []boardColumn, widths []int, cursor bool) string {
	fg, bg := rowColors(r, cursor)
	style := lipgloss.NewStyle().Foreground(fg)
	if bg != "" {
		style = style.Background(bg)
	}
	if cursor || r.State == "needs-you" {
		style = style.Bold(true)
	}
	gutter := "  "
	if cursor {
		gutter = "› "
	}
	return stripeCell(r, cursor) + style.Render(gutter+formatRow(rowCells(cols, r), widths))
}

// stripeCell is the 1-char lead of every row: the user color's stripe glyph,
// or a blank on an untagged row — so tagged and plain rows always align. The
// user hue lives only here; the row's text follows the state rules like any
// other row.
func stripeCell(r boardRow, cursor bool) string {
	glyph := " "
	style := lipgloss.NewStyle()
	if c, ok := rowTints[r.Color]; ok {
		glyph = stripeGlyph
		style = style.Foreground(c)
	}
	if cursor {
		style = style.Background(colorCursorBg)
	}
	return style.Render(glyph)
}

// rowColors picks a row's foreground and background. The fg is the state's
// ramp rung, lifted one rung on a pinned row and again under the cursor (so
// dim rows stay readable on the selection band); needs-you keeps the accent
// either way. The cursor row is the only row with a background.
func rowColors(r boardRow, cursor bool) (fg, bg lipgloss.Color) {
	fg = stateColor(r.State)
	if r.Pinned {
		fg = brighten(fg)
	}
	if cursor {
		fg = brighten(fg)
		bg = colorCursorBg
	}
	return fg, bg
}

// boardColumnWidths sizes the visible columns to the data, then lets the last
// one absorb the remaining width so the row fills the terminal.
func boardColumnWidths(cols []boardColumn, rows []boardRow, width int) []int {
	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = utf8.RuneCountInString(c.name)
	}
	for _, r := range rows {
		for i, v := range rowCells(cols, r) {
			if n := utf8.RuneCountInString(v); n > w[i] {
				w[i] = n
			}
		}
	}
	for i, c := range cols {
		// Cap WORKSPACE so the last column has room — unless it is the last
		// column itself, where it absorbs the slack instead.
		if c.name == "WORKSPACE" && i != len(cols)-1 && w[i] > 28 {
			w[i] = 28
		}
	}
	fixed := 3 + 2*(len(cols)-1) // stripe + gutter + separators
	for i := 0; i < len(w)-1; i++ {
		fixed += w[i]
	}
	if w[len(w)-1] = width - fixed; w[len(w)-1] < 8 {
		w[len(w)-1] = 8
	}
	return w
}

func formatRow(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = pad(truncate(c, widths[i]), widths[i])
	}
	return strings.Join(parts, "  ")
}

func pad(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// windowStart keeps the cursor visible when there are more rows than fit.
func windowStart(cursor, n, rows int) int {
	if n <= rows {
		return 0
	}
	start := cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start > n-rows {
		start = n - rows
	}
	return start
}

// rule draws a labeled horizontal divider across the width.
func rule(label string, width int) string {
	prefix := "─ " + label + " "
	fill := width - lipgloss.Width(prefix)
	if fill < 0 {
		fill = 0
	}
	return dimStyle.Render(prefix + strings.Repeat("─", fill))
}
