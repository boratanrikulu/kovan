package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/boratanrikulu/kovan/internal/taskdoc"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The new-agent target: a fresh worktree (default), the repo checkout itself
// (in-place), or a tab joining an existing agent's worktree.
const (
	targetWorktree = iota
	targetCheckout
	targetTab
)

// formModel is the new-agent modal: free-text id and title, a searchable project
// picker, a searchable from-branch picker, an optional account selector, and the
// brief textarea. Existing refs (project, branch, account) are picked, not typed
// — only id/title/brief are free text (the project picker also accepts a path).
type formModel struct {
	inputs          []textinput.Model // id, title
	project         projectPicker
	from            branchPicker
	workspaces      workspacePicker // join target when target == targetTab
	brief           textarea.Model
	focus           int
	repo            string   // repo the agent will be created in (header context)
	modes           []string // available task modes (always non-empty: the built-ins)
	mode            int      // selected index into modes
	accounts        []string // configured account names; empty hides the field
	account         int      // selected index into accounts
	colors          []string // stripe color choices: "none" + the palette names
	color           int      // selected index into colors
	target          int      // targetWorktree | targetCheckout | targetTab
	checkoutBranch  string   // branch a live agent holds in this checkout, "" if free; a tab here inherits it
	checkoutCurrent string   // the checkout's currently checked-out branch (shown in the "stay on" option)
	images          []string // temp paths of clipboard images, in [[image #N]] order
	briefFull       bool     // the brief textarea is expanded to fill the screen
	briefPad        int      // blank lines below the inline brief (breathing room), set by sizeForm
}

// maxBriefRows caps the inline CONTEXT editor's height; the brief is written
// full-screen (ctrl+f), so the inline view is a compact preview with whitespace
// around it rather than a wall of textarea.
const maxBriefRows = 12

// fullBriefChrome is the non-textarea line count of the full-screen brief view
// (header is 2 lines, then the brief section header, status, and footer), so the
// textarea can be sized to fill the rest.
const fullBriefChrome = 5

func newForm() formModel {
	id := textinput.New()
	id.Placeholder = "TASK-101 (blank → auto)"
	id.Prompt = "" // the field label carries the name; no inline "> "
	id.Focus()

	title := textinput.New()
	title.Placeholder = "fix the vfs handler"
	title.Prompt = ""

	project := projectPicker{projects: knownProjects()}
	from := branchPicker{}
	workspaces := workspacePicker{}
	repoName := ""
	checkoutBranch := ""
	checkoutCurrent := ""
	if sel := project.value(); sel.root != "" {
		from.branches = branchesFor(sel.root)
		workspaces.opts = workspacesFor(sel.root)
		checkoutBranch = checkoutHeldBranch(sel.root)
		checkoutCurrent = currentBranch(sel.root)
		repoName = filepath.Base(sel.root)
	}

	brief := textarea.New()
	brief.Placeholder = "the brief — what to do, why, any context (ctrl+v pastes an image)"
	brief.ShowLineNumbers = false
	// Keep the line-wrap cache at full size (it holds up to the line cap) so a
	// long brief stays memoized — the default MaxHeight (99) shrinks the cache on
	// the first keystroke and thrashes, making long text slow to type and scroll.
	brief.MaxHeight = 10000
	// A static cursor avoids the blink timer re-rendering the whole view.
	brief.Cursor.SetMode(cursor.CursorStatic)
	// Prefill the task-doc skeleton (minus its heading; the id/title fields cover
	// it and writeBrief re-adds it), so the structure is preserved and editable.
	if skel := briefSkeleton(); skel != "" {
		brief.SetValue(skel)
	}
	brief.SetWidth(60)
	brief.SetHeight(8)

	f := formModel{inputs: []textinput.Model{id, title}, project: project, from: from, workspaces: workspaces, brief: brief, repo: repoName, checkoutBranch: checkoutBranch, checkoutCurrent: checkoutCurrent}
	f.colors = append([]string{"none"}, rowTintNames...)
	defaultMode := mode.Default
	if home, err := config.Dir(); err == nil {
		f.modes = mode.List(home)
		if g, err := config.LoadGlobal(); err == nil && g.DefaultMode != "" {
			defaultMode = g.DefaultMode
		}
		f.mode = indexOf(f.modes, defaultMode)
	}
	if g, err := config.LoadGlobal(); err == nil && len(g.Accounts) > 0 {
		for name := range g.Accounts {
			f.accounts = append(f.accounts, name)
		}
		sort.Strings(f.accounts)
		f.account = indexOf(f.accounts, g.DefaultAccount)
	}
	return f
}

// projectPicker is a searchable list of repos to spawn the agent in: the known
// projects (current repo + repos with existing agents) filtered by a typed
// query, plus a "path: <query>" entry so a repo not yet known is reachable by
// typing its path.
type projectPicker struct {
	query    string
	projects []project
	sel      int
}

// items are the selectable projects: those whose name or root matches the query,
// plus a typed-path entry whenever the query is non-empty.
func (p projectPicker) items() []project {
	q := strings.ToLower(p.query)
	var items []project
	for _, pr := range p.projects {
		if q == "" || strings.Contains(strings.ToLower(pr.name), q) || strings.Contains(strings.ToLower(pr.root), q) {
			items = append(items, pr)
		}
	}
	if p.query != "" {
		items = append(items, project{name: "path: " + p.query, root: p.query})
	}
	return items
}

// value is the selected project, or the zero project when nothing matches and
// nothing was typed (the form then asks for a project).
func (p projectPicker) value() project {
	items := p.items()
	if len(items) == 0 {
		return project{}
	}
	sel := p.sel
	if sel >= len(items) {
		sel = len(items) - 1
	}
	return items[sel]
}

func (p projectPicker) update(key tea.KeyMsg) projectPicker {
	switch key.String() {
	case "up":
		if p.sel > 0 {
			p.sel--
		}
	case "down":
		if p.sel < len(p.items())-1 {
			p.sel++
		}
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.sel = 0
		}
	default:
		if key.Type == tea.KeyRunes {
			p.query += string(key.Runes)
			p.sel = 0
		}
	}
	if max := len(p.items()) - 1; p.sel > max && max >= 0 {
		p.sel = max
	}
	return p
}

// pickerRow renders one option in a project/from picker. The current choice is
// always marked with a "›" so it's visible even when the field isn't focused;
// focus only makes it bold. Other rows stay dim.
func pickerRow(label string, selected, focused bool) string {
	switch {
	case selected && focused:
		return cursorStyle.Render("› " + label)
	case selected:
		return selStyle.Render("› " + label)
	default:
		return dimStyle.Render("  " + label)
	}
}

// branchPicker is a searchable list of base-branch options for a new worktree:
// a "(default)" entry first (empty value → auto-detected base), then the repo's
// branches filtered by a typed substring query.
type branchPicker struct {
	query    string
	branches []string
	sel      int
}

// items are the selectable options: "" (default) first, then branches matching
// the query (case-insensitive substring).
func (p branchPicker) items() []string {
	items := []string{""}
	q := strings.ToLower(p.query)
	for _, b := range p.branches {
		if q == "" || strings.Contains(strings.ToLower(b), q) {
			items = append(items, b)
		}
	}
	return items
}

// value is the selected branch, or "" for the default base.
func (p branchPicker) value() string {
	items := p.items()
	if p.sel >= len(items) {
		return items[len(items)-1]
	}
	return items[p.sel]
}

// update handles a key while the from-picker is focused: typing filters, ↑/↓
// move the selection.
func (p branchPicker) update(key tea.KeyMsg) branchPicker {
	switch key.String() {
	case "up":
		if p.sel > 0 {
			p.sel--
		}
	case "down":
		if p.sel < len(p.items())-1 {
			p.sel++
		}
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.sel = 0
		}
	default:
		if key.Type == tea.KeyRunes {
			p.query += string(key.Runes)
			p.sel = 0
		}
	}
	if max := len(p.items()) - 1; p.sel > max {
		p.sel = max
	}
	return p
}

// workspaceOpt is one joinable workspace in the project: an existing agent's
// worktree, identified by an agent id in it and labeled by its branch.
type workspaceOpt struct {
	id    string // an agent id whose worktree to join (--in target)
	label string
}

// workspacePicker is a searchable list of the project's existing workspaces to
// join as a tab. Unlike the branch picker it has no blank default — a tab must
// name a workspace.
type workspacePicker struct {
	query string
	opts  []workspaceOpt
	sel   int
}

func (p workspacePicker) items() []workspaceOpt {
	q := strings.ToLower(p.query)
	var items []workspaceOpt
	for _, o := range p.opts {
		if q == "" || strings.Contains(strings.ToLower(o.label), q) {
			items = append(items, o)
		}
	}
	return items
}

// value is the selected workspace's join id, or "" when the project has none.
func (p workspacePicker) value() string {
	items := p.items()
	if len(items) == 0 {
		return ""
	}
	sel := p.sel
	if sel >= len(items) {
		sel = len(items) - 1
	}
	return items[sel].id
}

func (p workspacePicker) update(key tea.KeyMsg) workspacePicker {
	switch key.String() {
	case "up":
		if p.sel > 0 {
			p.sel--
		}
	case "down":
		if p.sel < len(p.items())-1 {
			p.sel++
		}
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.sel = 0
		}
	default:
		if key.Type == tea.KeyRunes {
			p.query += string(key.Runes)
			p.sel = 0
		}
	}
	if max := len(p.items()) - 1; p.sel > max {
		p.sel = max
	}
	return p
}

// workspacesFor lists the joinable workspaces in the repo at root: one entry per
// distinct worktree of an agent already in that repo, so a new tab can pick which
// to share.
func workspacesFor(root string) []workspaceOpt {
	repoName := filepath.Base(root)
	all, err := session.List()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var opts []workspaceOpt
	for _, m := range all {
		if m.Repo != repoName || m.Worktree == "" || seen[m.Worktree] {
			continue
		}
		seen[m.Worktree] = true
		opts = append(opts, workspaceOpt{id: m.ID, label: m.Branch + "  (" + m.ID + ")"})
	}
	return opts
}

// checkoutHeldBranch returns the branch a live agent occupies the repo checkout
// at root with, or "" if the checkout is free. A second agent on this checkout
// joins as a tab and shares that branch, so the form shows it as inherited
// instead of offering a branch picker that could not switch it anyway.
func checkoutHeldBranch(root string) string {
	if id := inPlaceAgentFor(root); id != "" {
		all, err := session.List()
		if err != nil {
			return ""
		}
		for _, m := range all {
			if m.ID == id {
				return m.Branch
			}
		}
	}
	return ""
}

// stayLabel is the "no switch" option for the This-checkout branch field. It
// names the currently checked-out branch when known, so the user can see what
// staying means (and that picking another entry switches the checkout to it).
func (f formModel) stayLabel() string {
	if f.checkoutCurrent != "" {
		return "stay on " + f.checkoutCurrent + " (current)"
	}
	return "stay on the current branch"
}

// briefSkeleton is the context.md template minus its heading line, so the brief
// editor opens on the same structure $EDITOR would, without the redundant
// "# id — title" (the form has those fields; writeBrief re-adds the heading).
func briefSkeleton() string {
	tmpl, err := taskdoc.BriefTemplate(templatesDir())
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(tmpl, '\n'); i >= 0 {
		return strings.TrimLeft(tmpl[i+1:], "\n")
	}
	return tmpl
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}

func (f formModel) value(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }

// selectedAccount is the chosen account name, or "" when none are configured.
func (f formModel) selectedAccount() string {
	if len(f.accounts) == 0 {
		return ""
	}
	return f.accounts[f.account]
}

// selectedMode is the chosen task mode, or "" when none are listed (then start
// resolves the default).
func (f formModel) selectedMode() string {
	if len(f.modes) == 0 {
		return ""
	}
	return f.modes[f.mode]
}

// selectedColor is the chosen stripe color, or "" for none (then start falls
// back to the repo's default from ~/.kovan/config.yaml).
func (f formModel) selectedColor() string {
	if len(f.colors) == 0 || f.colors[f.color] == "none" {
		return ""
	}
	return f.colors[f.color]
}

// total counts the focusable elements: id/title, the project picker, the target
// toggle, the from picker, the mode picker, the optional account picker, the
// color picker, and the brief textarea (always last).
func (f formModel) total() int {
	n := len(f.inputs) + 6 // + project + target + from + mode + color + brief
	if len(f.accounts) > 0 {
		n++
	}
	return n
}

func (f formModel) onProject() bool { return f.focus == len(f.inputs) }

func (f formModel) onTarget() bool { return f.focus == len(f.inputs)+1 }

func (f formModel) onFrom() bool { return f.focus == len(f.inputs)+2 }

func (f formModel) onMode() bool { return f.focus == len(f.inputs)+3 }

func (f formModel) onAccount() bool {
	return len(f.accounts) > 0 && f.focus == len(f.inputs)+4
}

func (f formModel) onColor() bool {
	n := len(f.inputs) + 4
	if len(f.accounts) > 0 {
		n++
	}
	return f.focus == n
}

func (f formModel) onBrief() bool {
	return f.focus == f.total()-1
}

// update moves focus on tab/shift+tab; on the from field typing filters and ↑/↓
// pick a branch; on the account field left/right cycle; on the brief field keys
// route to the textarea; otherwise to the focused text input.
func (f formModel) update(msg tea.Msg) (formModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "tab":
			f.setFocus(f.focus + 1)
			return f, nil
		case "shift+tab":
			f.setFocus(f.focus - 1)
			return f, nil
		}
		if f.onProject() {
			before := f.project.value().root
			f.project = f.project.update(key)
			return f.reloadProjectRefs(before), nil
		}
		if f.onTarget() {
			switch key.String() {
			case "right", "l", " ", "down":
				f.target = (f.target + 1) % 3
			case "left", "h", "up":
				f.target = (f.target + 2) % 3
			}
			return f, nil
		}
		if f.onFrom() {
			switch {
			case f.target == targetTab:
				f.workspaces = f.workspaces.update(key)
			case f.target == targetCheckout && f.checkoutBranch != "":
				// Branch is inherited from the occupying agent — nothing to pick.
			default:
				f.from = f.from.update(key)
			}
			return f, nil
		}
		if f.onMode() && len(f.modes) > 0 {
			switch key.String() {
			case "left", "h", "up":
				f.mode = (f.mode - 1 + len(f.modes)) % len(f.modes)
			case "right", "l", "down":
				f.mode = (f.mode + 1) % len(f.modes)
			}
			return f, nil
		}
		if f.onAccount() {
			switch key.String() {
			case "left", "h", "up":
				f.account = (f.account - 1 + len(f.accounts)) % len(f.accounts)
			case "right", "l", "down":
				f.account = (f.account + 1) % len(f.accounts)
			}
			return f, nil
		}
		if f.onColor() {
			switch key.String() {
			case "left", "h", "up":
				f.color = (f.color - 1 + len(f.colors)) % len(f.colors)
			case "right", "l", "down":
				f.color = (f.color + 1) % len(f.colors)
			}
			return f, nil
		}
	}
	if f.onProject() || f.onTarget() || f.onFrom() || f.onMode() || f.onAccount() || f.onColor() {
		return f, nil
	}
	if f.onBrief() {
		var cmd tea.Cmd
		f.brief, cmd = f.brief.Update(msg)
		return f, cmd
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return f, cmd
}

// reloadProjectRefs reloads the branch and workspace pickers and the checkout
// state when the selected project changed from before, so every WHERE field
// belongs to the project being spawned into.
func (f formModel) reloadProjectRefs(before string) formModel {
	after := f.project.value().root
	if after == before {
		return f
	}
	f.from = branchPicker{branches: branchesFor(after)}
	f.workspaces = workspacePicker{opts: workspacesFor(after)}
	f.checkoutBranch = checkoutHeldBranch(after)
	f.checkoutCurrent = currentBranch(after)
	f.repo = filepath.Base(after)
	return f
}

// setOption applies a click on the options panel: the focused field's
// selection becomes the clicked item. Indexes come from optionsPanel's target
// map, so they are valid for the panel as rendered; the guards only cover the
// placeholder rows a picker renders when it has no real items.
func (f formModel) setOption(idx int) formModel {
	switch {
	case f.onProject():
		before := f.project.value().root
		if idx < len(f.project.items()) {
			f.project.sel = idx
		}
		return f.reloadProjectRefs(before)
	case f.onTarget():
		if idx < 3 {
			f.target = idx
		}
	case f.onFrom():
		switch {
		case f.target == targetTab:
			if idx < len(f.workspaces.items()) {
				f.workspaces.sel = idx
			}
		case f.target == targetCheckout && f.checkoutBranch != "":
			// The branch is inherited — the panel is informational.
		default:
			if idx < len(f.from.items()) {
				f.from.sel = idx
			}
		}
	case f.onMode():
		if idx < len(f.modes) {
			f.mode = idx
		}
	case f.onAccount():
		if idx < len(f.accounts) {
			f.account = idx
		}
	case f.onColor():
		if idx < len(f.colors) {
			f.color = idx
		}
	}
	return f
}

func (f *formModel) setFocus(i int) {
	n := f.total()
	f.focus = (i%n + n) % n
	for j := range f.inputs {
		if j == f.focus {
			f.inputs[j].Focus()
		} else {
			f.inputs[j].Blur()
		}
	}
	if f.onBrief() {
		f.brief.Focus()
	} else {
		f.brief.Blur()
	}
}

// fieldLabelW is the width of the form's label column, so values align.
const fieldLabelW = 10

// fieldRow renders one collapsed field: a focus gutter, the padded label, and
// the value on a single line.
func fieldRow(label, value string, focused bool) string {
	gutter := "  "
	if focused {
		gutter = cursorStyle.Render("▸ ")
	}
	return gutter + dimStyle.Render(pad(label, fieldLabelW)) + value
}

// panelRows is the fixed number of option rows in the form's options panel. The
// panel is always present, so focusing a field never moves the layout.
const panelRows = 5

// targetName is the label for a target index (worktree, checkout, tab).
func targetName(t int) string {
	return []string{"New workspace", "This checkout", "Tab in a workspace"}[t]
}

// fieldColumn builds the WHAT / WHERE / HOW section. Every field is exactly one
// line, always — the focused field's options live in the separate, fixed-height
// options panel, so nothing in the form ever shifts as focus moves. The second
// return maps each line to the focus index a click on it should give (-1 for
// headers and spacers), built alongside the lines so the two can never drift.
func (f formModel) fieldColumn() ([]string, []int) {
	var l []string
	var targets []int
	add := func(s string, t int) {
		l = append(l, s)
		targets = append(targets, t)
	}
	add(headerStyle.Render("WHAT"), -1)
	add(fieldRow("id", f.inputs[0].View(), f.focus == 0), 0)
	add(fieldRow("title", f.inputs[1].View(), f.focus == 1), 1)

	add("", -1)
	add(headerStyle.Render("WHERE"), -1)
	proj := "—"
	if p := f.project.value(); p.name != "" {
		proj = p.name
	}
	n := len(f.inputs)
	add(fieldRow("project", proj, f.onProject()), n)
	add(fieldRow("where", targetName(f.target), f.onTarget()), n+1)
	cl, cv := f.ctxCollapsed()
	add(fieldRow(cl, cv, f.onFrom()), n+2)

	add("", -1)
	add(headerStyle.Render("HOW"), -1)
	if len(f.modes) > 0 {
		add(fieldRow("mode", f.modes[f.mode], f.onMode()), n+3)
	}
	color := n + 4
	if len(f.accounts) > 0 {
		add(fieldRow("account", f.accounts[f.account], f.onAccount()), n+4)
		color++
	}
	add(fieldRow("color", colorPreview(f.colors[f.color]), f.onColor()), color)
	return l, targets
}

// optionsPanel renders the focused field's choices in a fixed-height block, so
// selection always happens in the same place and the layout never moves. List
// fields (project / base / workspace) filter live as you type; the rest list
// their choices. The second return maps each line to the option index a click
// on it should pick (-1 for the title, the search line, blanks, and every line
// of an informational panel).
func (f formModel) optionsPanel() ([]string, []int) {
	title, items, sel, search, filter := f.focusedOptions()
	lines := []string{dimStyle.Render("─ " + title)}
	targets := []int{-1}
	rows := panelRows
	if filter {
		// The live search input, on its own first line — your typing goes here.
		lines = append(lines, cursorStyle.Render("  > "+search))
		targets = append(targets, -1)
		rows--
	}
	start := windowStart(sel, len(items), rows)
	if sel < 0 { // an informational panel (recap) has no cursor
		start = 0
	}
	for i := 0; i < rows; i++ {
		idx := start + i
		if idx < 0 || idx >= len(items) {
			lines = append(lines, "")
			targets = append(targets, -1)
			continue
		}
		lines = append(lines, "  "+pickerRow(truncate(items[idx], 76), idx == sel, true))
		if sel >= 0 {
			targets = append(targets, idx)
		} else {
			targets = append(targets, -1)
		}
	}
	return lines, targets
}

// focusedOptions returns the options panel's header, its option labels, the
// selected index, and — for a searchable field — the live filter text and a flag
// to render the search input line. A negative sel means an informational panel
// (the recap) with no cursor.
func (f formModel) focusedOptions() (title string, items []string, sel int, search string, filter bool) {
	switch {
	case f.focus == 0:
		return "id · leave blank to auto-generate, or type a ticket id", f.recap(), -1, "", false
	case f.focus == 1:
		return "title · a short label (becomes the branch slug)", f.recap(), -1, "", false
	case f.onProject():
		var it []string
		for _, p := range f.project.items() {
			it = append(it, p.name)
		}
		return "project · type to filter · ↑/↓ select", it, f.project.sel, f.project.query, true
	case f.onTarget():
		return "where · ↑/↓ choose", []string{
			"New workspace · a fresh worktree on a new branch",
			"This checkout · run here on the branch you pick, no separate worktree",
			"Tab in a workspace · share an existing workspace's branch",
		}, f.target, "", false
	case f.onFrom():
		if f.target == targetTab {
			var it []string
			for _, o := range f.workspaces.items() {
				it = append(it, o.label)
			}
			if len(it) == 0 {
				it = []string{"(no workspaces in this project to join)"}
			}
			return "workspace · type to filter · ↑/↓ select", it, f.workspaces.sel, f.workspaces.query, true
		}
		if f.target == targetCheckout && f.checkoutBranch != "" {
			// A live agent already holds the checkout; a second tab here shares its
			// branch and cannot switch it, so there is nothing to pick.
			return "branch · this checkout is in use — inherited, not switchable",
				[]string{"inherits " + f.checkoutBranch}, -1, "", false
		}
		var it []string
		for _, b := range f.from.items() {
			switch {
			case b != "":
				it = append(it, b)
			case f.target == targetCheckout:
				it = append(it, "("+f.stayLabel()+")")
			default:
				it = append(it, "(latest default branch)")
			}
		}
		label := "base branch"
		if f.target == targetCheckout {
			label = "branch"
		}
		return label + " · type to filter · ↑/↓ select", it, f.from.sel, f.from.query, true
	case f.onMode():
		return "mode · ↑/↓ choose", f.modes, f.mode, "", false
	case f.onAccount():
		return "account · ↑/↓ choose", f.accounts, f.account, "", false
	case f.onColor():
		return "color · ↑/↓ choose · the row's board stripe", f.colors, f.color, "", false
	default: // the context editor
		return "brief · what you're about to create", f.recap(), -1, "", false
	}
}

// recap is a live summary of what submitting will create, shown in the options
// panel while a text field (id / title / brief) is focused — so that panel is
// always full and doubles as a final review before ctrl+d.
func (f formModel) recap() []string {
	id := f.value(0)
	if id == "" {
		id = "(auto-generated)"
	}
	mode := "—"
	if len(f.modes) > 0 {
		mode = f.modes[f.mode]
	}
	account := f.selectedAccount()
	if account == "" {
		account = "—"
	}
	cl, cv := f.ctxCollapsed()
	how := "mode · " + mode + "   account · " + account
	if c := f.selectedColor(); c != "" {
		how += "   color · " + c
	}
	return []string{
		"id · " + id,
		"project · " + f.repo,
		"where · " + targetName(f.target),
		cl + " · " + cv,
		how,
	}
}

// ctxCollapsed is the where-context field's one-line label and value.
func (f formModel) ctxCollapsed() (label, value string) {
	switch f.target {
	case targetTab:
		if id := f.workspaces.value(); id != "" {
			for _, o := range f.workspaces.items() {
				if o.id == id {
					return "workspace", o.label
				}
			}
		}
		return "workspace", "(no workspaces — pick another target)"
	case targetCheckout:
		if f.checkoutBranch != "" {
			return "branch", "inherits " + f.checkoutBranch + " (this checkout is in use)"
		}
		if v := f.from.value(); v != "" {
			return "branch", "switch to " + v
		}
		return "branch", f.stayLabel()
	default:
		if v := f.from.value(); v != "" {
			return "base", v
		}
		return "base", "latest default branch (auto)"
	}
}

// chromeLines is the form's non-textarea line count, derived from the same
// builders view uses so the two can never drift (the brief textarea fills the
// rest of the terminal exactly).
func (f formModel) chromeLines() int {
	// fieldColumn and optionsPanel are both fixed height, so the brief never
	// resizes as focus moves. The +5: a spacer above the options panel, a spacer
	// above the CONTEXT header, the CONTEXT header, status, and footer.
	fields, _ := f.fieldColumn()
	opts, _ := f.optionsPanel()
	return len(f.formHeader()) + len(fields) + len(opts) + 5
}

// formHeader names the target repo and echoes the chosen mode/account, then a
// blank spacer line.
func (f formModel) formHeader() []string {
	head := "new agent"
	if f.repo != "" {
		head += " · " + f.repo
	}
	right := ""
	if len(f.modes) > 0 {
		right = f.modes[f.mode]
	}
	if a := f.selectedAccount(); a != "" {
		if right != "" {
			right += " · "
		}
		right += a
	}
	line := brandHeader(head)
	if right != "" {
		line += "  " + dimStyle.Render(right)
	}
	return []string{line, ""}
}

// briefHeader is the CONTEXT section header (the brief becomes the task's
// context.md), noting any pasted images.
func (f formModel) briefHeader() string {
	h := "CONTEXT"
	if len(f.images) > 0 {
		h += fmt.Sprintf("  (%d image%s)", len(f.images), plural(len(f.images)))
	}
	return headerStyle.Render(h)
}

// view renders the full-screen new-agent form: a static column of one-line
// fields, a fixed options panel for the focused field, then the brief textarea
// filling the rest, with status + key hints pinned at the bottom. Nothing moves
// as focus changes.
func (f formModel) view(status string) string {
	var lines []string
	lines = append(lines, f.formHeader()...)
	fields, _ := f.fieldColumn()
	lines = append(lines, fields...)
	lines = append(lines, "") // blank spacer above the options panel
	opts, _ := f.optionsPanel()
	lines = append(lines, opts...)
	lines = append(lines, "", f.briefHeader(), f.brief.View()) // blank spacer above the editor
	for i := 0; i < f.briefPad; i++ {
		lines = append(lines, "") // breathing room below the editor
	}
	lines = append(lines, status)
	footer := "ctrl+d submit · tab next · ctrl+f fullscreen context · esc cancel"
	if runtime.GOOS == "darwin" {
		footer += " · ctrl+v image"
	}
	lines = append(lines, dimStyle.Render(footer))
	return strings.Join(lines, "\n")
}

// viewFull renders the brief expanded to the whole terminal: the header, the
// brief section header, the textarea filling the middle, and status + hints.
func (f formModel) viewFull(status string) string {
	lines := f.formHeader()
	lines = append(lines, f.briefHeader(), f.brief.View(), status)
	footer := "ctrl+d submit · ctrl+f / esc back to fields"
	if runtime.GOOS == "darwin" {
		footer += " · ctrl+v image"
	}
	lines = append(lines, dimStyle.Render(footer))
	return strings.Join(lines, "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
