package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/boratanrikulu/kovan/internal/taskdoc"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// editAgent updates an existing agent's editable fields — title, task mode,
// account, and row color — on its manifest, then re-applies their side effects:
// it re-renders the worktree's CLAUDE.local.md (so the mode's protocol pointer
// is current) and scaffolds any docs the new mode adds. The opening prompt already fired at
// launch, so a mode change shapes the standing instructions, not that first
// turn; the account takes effect on the next wake (the token is injected at
// launch). Structural fields (id, branch, base, repo) are not editable.
func editAgent(tmux, title, modeFlag, account, color string) (id string, err error) {
	man, err := session.ReadByTmux(tmux)
	if err != nil {
		return "", fmt.Errorf("read agent: %w", err)
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return "", err
	}
	repoCfg, err := config.LoadRepo(man.RepoRoot)
	if err != nil {
		return "", err
	}
	home, err := config.Dir()
	if err != nil {
		return "", err
	}
	// Validate the account token before writing anything, so a bad file fails
	// without half-applying the edit.
	if _, err := accountTokenFile(global, account); err != nil {
		return "", err
	}
	taskMode, err := mode.Load(home, modeFlag)
	if err != nil {
		return "", err
	}

	man.Title = title
	man.TaskMode = taskMode.Name
	man.Account = account
	man.Color = color
	if err := man.Write(); err != nil {
		return "", err
	}

	taskAbs := taskDocsDir(home, man.Repo, repoCfg.Task.Dir, man.ID)
	if err := taskdoc.Scaffold(taskAbs, man.ID, title, repoCfg.Task.Token, templatesDir(), taskMode.Docs); err != nil {
		return "", err
	}
	if err := writeClaudeLocal(man.Worktree, man.ID, title, taskAbs, account, repoCfg.Domain, man.Repo, taskMode); err != nil {
		return "", err
	}
	return man.ID, nil
}

// editModel is the board's edit modal: the agent's title (free text), task mode,
// account, and row color, pre-filled from the selected row. id/branch/base are fixed.
type editModel struct {
	id       string
	repo     string
	tmux     string
	title    textinput.Model
	modes    []string
	mode     int
	accounts []string
	account  int
	colors   []string
	color    int
	focus    int // 0 title, 1 mode, then account (when configured), color last
}

// newEditForm builds the edit modal pre-filled from the selected board row.
func newEditForm(row boardRow) editModel {
	title := textinput.New()
	title.SetValue(row.Title)
	title.Focus()
	e := editModel{id: row.ID, repo: row.Repo, tmux: row.Tmux, title: title}
	if home, err := config.Dir(); err == nil {
		e.modes = mode.List(home)
		e.mode = indexOf(e.modes, row.Mode)
	}
	if g, err := config.LoadGlobal(); err == nil && len(g.Accounts) > 0 {
		for name := range g.Accounts {
			e.accounts = append(e.accounts, name)
		}
		sort.Strings(e.accounts)
		e.account = indexOf(e.accounts, row.Account)
	}
	e.colors = append([]string{"none"}, rowTintNames...)
	if row.Color != "" {
		e.color = indexOf(e.colors, row.Color)
	}
	return e
}

func (e editModel) total() int {
	n := 3 // title + mode + color
	if len(e.accounts) > 0 {
		n++
	}
	return n
}

func (e editModel) onTitle() bool { return e.focus == 0 }
func (e editModel) onMode() bool  { return e.focus == 1 }
func (e editModel) onAccount() bool {
	return len(e.accounts) > 0 && e.focus == 2
}
func (e editModel) onColor() bool { return e.focus == e.total()-1 }

func (e editModel) selectedMode() string {
	if len(e.modes) == 0 {
		return ""
	}
	return e.modes[e.mode]
}

func (e editModel) selectedAccount() string {
	if len(e.accounts) == 0 {
		return ""
	}
	return e.accounts[e.account]
}

// selectedColor is the manifest value: the palette name, or "" for none.
func (e editModel) selectedColor() string {
	if len(e.colors) == 0 || e.colors[e.color] == "none" {
		return ""
	}
	return e.colors[e.color]
}

func (e *editModel) setFocus(i int) {
	n := e.total()
	e.focus = (i%n + n) % n
	if e.onTitle() {
		e.title.Focus()
	} else {
		e.title.Blur()
	}
}

func (e editModel) update(msg tea.Msg) (editModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "tab":
			e.setFocus(e.focus + 1)
			return e, nil
		case "shift+tab":
			e.setFocus(e.focus - 1)
			return e, nil
		}
		if e.onMode() && len(e.modes) > 0 {
			switch key.String() {
			case "left", "h":
				e.mode = (e.mode - 1 + len(e.modes)) % len(e.modes)
			case "right", "l":
				e.mode = (e.mode + 1) % len(e.modes)
			}
			return e, nil
		}
		if e.onAccount() {
			switch key.String() {
			case "left", "h":
				e.account = (e.account - 1 + len(e.accounts)) % len(e.accounts)
			case "right", "l":
				e.account = (e.account + 1) % len(e.accounts)
			}
			return e, nil
		}
		if e.onColor() && len(e.colors) > 0 {
			switch key.String() {
			case "left", "h":
				e.color = (e.color - 1 + len(e.colors)) % len(e.colors)
			case "right", "l":
				e.color = (e.color + 1) % len(e.colors)
			}
			return e, nil
		}
	}
	if e.onTitle() {
		var cmd tea.Cmd
		e.title, cmd = e.title.Update(msg)
		return e, cmd
	}
	return e, nil
}

func (e editModel) view(status string) string {
	head := "edit " + e.id
	if e.repo != "" {
		head += " · " + e.repo
	}
	lines := []string{
		brandHeader(head),
		dimStyle.Render("change the title, mode, account, or color; account applies on the next wake"),
		"",
		dimStyle.Render("title"), e.title.View(), "",
	}
	if len(e.modes) > 0 {
		lines = append(lines, dimStyle.Render("mode"), cycler(e.modes[e.mode], e.onMode()), "")
	}
	if len(e.accounts) > 0 {
		lines = append(lines, dimStyle.Render("account"), cycler(e.accounts[e.account], e.onAccount()), "")
	}
	lines = append(lines, dimStyle.Render("color"), colorCycler(e.colors[e.color], e.onColor()), "")
	lines = append(lines, status, dimStyle.Render("ctrl+d save · tab next · esc cancel"))
	return strings.Join(lines, "\n")
}

// cycler renders a left/right-selectable value, emphasized when focused.
func cycler(value string, focused bool) string {
	if focused {
		return cursorStyle.Render("‹ "+value+" ›") + dimStyle.Render("   ←/→")
	}
	return "  " + value
}

// colorCycler is the cycler for the color field: the name is previewed with
// the stripe the row will carry.
func colorCycler(name string, focused bool) string {
	chip := colorPreview(name)
	if focused {
		return cursorStyle.Render("‹ ") + chip + cursorStyle.Render(" ›") + dimStyle.Render("   ←/→")
	}
	return "  " + chip
}
