package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// methodEditedMsg fires when the method-file editor (or AI-edit) exits, so the
// focused file's contents can be reloaded.
type methodEditedMsg struct{ err error }

// runMethod is the `kovan method` entry: it boots the cockpit straight into the
// method inspector for the current repo. The board still refreshes underneath
// (Init runs the refresh), so q/esc/m back out to it just like pressing `m`.
func runMethod() error {
	m, err := newModel()
	if err != nil {
		return err
	}
	started, _ := m.enterMethod()
	_, err = tea.NewProgram(started, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

// enterMethod opens the method inspector for the selected agent, or the current
// repo when the board is empty. Errors keep us on the board.
func (m model) enterMethod() (tea.Model, tea.Cmd) {
	home, err := config.Dir()
	if err != nil {
		m.setErr(err)
		return m, nil
	}
	id := ""
	if row := m.current(); row != nil {
		id = row.ID
	}
	ctx, err := methodContext(id)
	if err != nil {
		m.setErr(err)
		return m, nil
	}
	m.mctx = ctx
	m.methodLayers = effectiveMethod(home, ctx)
	if g, err := config.LoadGlobal(); err == nil {
		m.methodGates = gateSummary(home, ctx, g.Gates)
	}
	m.methodFile = 0
	m.mode = modeMethod
	m.dismissOnSwitch()
	m.loadMethodFile()
	m.sizeMethodViewport()
	return m, nil
}

// flattenMethodFiles is the flat list of files the method cursor walks, in
// render order (direct files followed by their imports); empty layers
// contribute nothing and so are skipped.
func flattenMethodFiles(layers []method.Layer) []method.File {
	var files []method.File
	for _, l := range layers {
		files = append(files, l.Files...)
		files = append(files, l.Skills...) // same order methodList renders
	}
	return files
}

// loadMethodFile reads the focused file into the contents viewport. A file that
// does not exist on disk (a layer pointing at a not-yet-created path) shows a
// (missing) body rather than erroring out of the view.
func (m *model) loadMethodFile() {
	files := flattenMethodFiles(m.methodLayers)
	if len(files) == 0 {
		m.methodVP.SetContent(dimStyle.Render("(no method files)"))
		m.methodVP.GotoTop()
		return
	}
	m.methodFile = clamp(m.methodFile, 0, len(files)-1)
	path := files[m.methodFile].Path
	if data, err := os.ReadFile(path); err == nil {
		m.methodVP.SetContent(string(data))
	} else {
		m.methodVP.SetContent(dimStyle.Render("(missing) " + path))
	}
	m.methodVP.GotoTop()
}

// gateSummary describes the built-in gates that govern this agent, for the method
// inspector: the push gate applies to every agent, and the read-only gate
// applies only when the agent's mode is read-only.
func gateSummary(home string, mctx methodCtx, g config.Gates) []string {
	lines := []string{
		gateLine("push", g.Push, "confirm before git push / gh pr create"),
	}
	note := "applies only to read-only modes"
	if md, err := mode.Load(home, mctx.mode); err == nil && md.ReadOnly() {
		note = "active: this mode can't edit the repo"
	}
	lines = append(lines, gateLine("read-only", g.ReadOnly, note))
	return lines
}

// gateLine formats one gate row: name, its action (ask/off), and a short note.
func gateLine(name, action, note string) string {
	if action == "" {
		action = "off"
	}
	return fmt.Sprintf("%-11s %-3s — %s", name+":", action, note)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m model) handleMethodKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Contents scrolling mirrors the board peek.
	switch {
	case key.Matches(msg, keys.PageUp):
		m.methodVP.HalfViewUp()
		return m, nil
	case key.Matches(msg, keys.PageDown):
		m.methodVP.HalfViewDown()
		return m, nil
	}

	files := flattenMethodFiles(m.methodLayers)
	switch msg.String() {
	case "esc", "m", "q":
		// Back out to the board (all agents) — never quit from the method view,
		// whether we got here via `m` or `kovan method`. q quits from the board.
		m.mode = modeBoard
		return m, nil
	case "e", "enter":
		if path := focused(files, m.methodFile); path != "" {
			return m, tea.ExecProcess(editorCommand(path), func(err error) tea.Msg {
				return methodEditedMsg{err: err}
			})
		}
		return m, nil
	case "E":
		if path := focused(files, m.methodFile); path != "" {
			return m, tea.ExecProcess(aiEditCommand(path), func(err error) tea.Msg {
				return methodEditedMsg{err: err}
			})
		}
		return m, nil
	}

	switch keys.action(msg) {
	case actUp:
		if m.methodFile > 0 {
			m.methodFile--
			m.loadMethodFile()
		}
	case actDown:
		if m.methodFile < len(files)-1 {
			m.methodFile++
			m.loadMethodFile()
		}
	}
	return m, nil
}

func focused(files []method.File, i int) string {
	if i >= 0 && i < len(files) {
		return files[i].Path
	}
	return ""
}

// aiEditCommand launches the agent on a method file to revise it interactively.
// "--" before the prompt is mandatory: the agent's --add-dir-style flags are
// variadic and would otherwise swallow the positional prompt (the init bug). We
// pass no --add-dir here — the file's own directory is the cwd.
func aiEditCommand(path string) *exec.Cmd {
	agent := "claude"
	if g, err := config.LoadGlobal(); err == nil && g.Agent != "" {
		agent = g.Agent
	}
	cmd := exec.Command(agent, "--",
		"Open "+filepath.Base(path)+" and help me revise it. "+
			"Ask what I want to change before editing.")
	cmd.Dir = filepath.Dir(path)
	return cmd
}

func (m model) methodView() string {
	context := strings.Join([]string{
		"method",
		"account: " + orNone(m.mctx.account),
		"domain: " + orNone(m.mctx.domain),
		"repo: " + m.mctx.repo,
	}, " · ")
	// header / layer list / contents rule / contents (fills) / status / help —
	// the same top-fills, bottom-pinned shape as the board's peek panel.
	return strings.Join([]string{
		brandHeader(context),
		m.methodList(),
		rule("contents", m.width),
		m.methodVP.View(),
		m.methodStatusLine(),
		m.methodHelpLine(),
	}, "\n")
}

// sizeMethodViewport grows the contents pane to fill everything below the layer
// list, down to the pinned status + help lines — so the contents read like the
// board's peek (no dead space, status at the very bottom) but larger, since
// reading the focused file is the point.
func (m *model) sizeMethodViewport() {
	if !m.ready {
		return
	}
	listLines := strings.Count(m.methodList(), "\n") + 1
	// header(1) + list + contents-rule(1) + status(1) + help(1).
	h := m.height - listLines - 4
	if h < 1 {
		h = 1
	}
	m.methodVP.Width, m.methodVP.Height = m.width, h
}

// methodList renders the layers as blue headers (the board's column-header role)
// with their files indented, the focused entry highlighted and empty layers (none).
func (m model) methodList() string {
	var b strings.Builder
	idx := 0
	emit := func(label string) {
		if idx == m.methodFile {
			label = cursorStyle.Render(label)
		}
		b.WriteString(label + "\n")
		idx++
	}
	for _, l := range m.methodLayers {
		// Layer headers share the board's column-header role.
		b.WriteString(headerStyle.Render(l.Name+":") + "\n")
		if len(l.Files) == 0 && len(l.Skills) == 0 {
			b.WriteString("    " + dimStyle.Render("(none)") + "\n")
			continue
		}
		for _, f := range l.Files {
			emit(methodFileLabel(f))
		}
		for _, s := range l.Skills {
			emit(skillLabel(s))
		}
	}
	// Gates govern the agent too, so the inspector shows them — informational, not
	// part of the file cursor.
	if len(m.methodGates) > 0 {
		b.WriteString(headerStyle.Render("gates:") + "\n")
		for _, g := range m.methodGates {
			b.WriteString("    " + dimStyle.Render(g) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// methodFileLabel indents a method file by its import depth: directly-listed
// files at one level, imported files one level deeper with a ↳ marker.
func methodFileLabel(f method.File) string {
	indent := strings.Repeat("  ", f.Depth+2)
	if f.Depth == 0 {
		return indent + f.Path
	}
	return indent + "↳ " + f.Path
}

// skillLabel renders a skill by its directory name (the parent of its SKILL.md),
// marked so it reads distinctly from the method files.
func skillLabel(f method.File) string {
	return "    skill: " + filepath.Base(filepath.Dir(f.Path))
}

// methodStatusLine and methodHelpLine mirror the board's statusLine/helpLine: a
// transient message above an always-visible keys bar, so both stay readable.
func (m model) methodStatusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return errStyle.Render(m.status)
	}
	return infoStyle.Render(m.status)
}

func (m model) methodHelpLine() string {
	return dimStyle.Render("j/k move · e/enter edit · E ai-edit · PgUp/PgDn scroll · esc/m back")
}
