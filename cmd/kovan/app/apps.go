package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/spf13/cobra"
)

// appCmd opens an app (editor / merge / terminal on the worktree, or the editor
// on the agent's task-doc dir via "notes") for the agent at a path, the same
// actions as the board's e/s/t/w keys, but runnable from inside an agent's own
// tmux session (where the board's keymap isn't active). The tmux "prefix k"
// binding calls "app menu" with the session, client and pane, which shows a
// menu of these actions over every workspace the agent has. Menu items name
// their target by session + workspace index or by pane id, never by path:
// tmux expands a menu command as a format twice, so a path in it is not data.
var appCmd = &cobra.Command{
	Use:    "app <editor|merge|terminal|notes|menu> [path]",
	Short:  "Open an agent's app at a path (used by the in-session tmux menu)",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] == "menu" {
			return showAppMenu(appSession, appClient, appPane)
		}
		dir, err := appTarget(args)
		if err != nil {
			return err
		}
		return runApp(args[0], dir, appSession)
	},
}

// appSession names the exact tab (by tmux session) for notes, the menu, and
// --workspace, so sibling tabs sharing a worktree still resolve to their own
// task-doc dir. appClient is the tmux client the menu opens on; appPane a
// tmux pane whose path stands in for a non-agent session; appWorkspace an
// index into the tab's workspaces, the primary first.
var (
	appSession   string
	appClient    string
	appPane      string
	appWorkspace int
)

func init() {
	appCmd.Flags().StringVar(&appSession, "session", "", "resolve the agent by tmux session name")
	appCmd.Flags().StringVar(&appClient, "client", "", "tmux client to show the menu on")
	appCmd.Flags().StringVar(&appPane, "pane", "", "tmux pane whose current path is the target")
	appCmd.Flags().IntVar(&appWorkspace, "workspace", -1, "index of the session's workspace to open, the primary first")
}

// appTarget resolves the dir an app opens on: the --session tab's workspace by
// --workspace index, else the --pane's current path, else the path argument
// (runApp falls back to the cwd).
func appTarget(args []string) (string, error) {
	switch {
	case appSession != "" && appWorkspace >= 0:
		m, err := session.ReadByTmux(appSession)
		if err != nil {
			return "", err
		}
		ws := m.Workspaces()
		if appWorkspace >= len(ws) {
			return "", fmt.Errorf("agent %s has no workspace %d", m.ID, appWorkspace)
		}
		return ws[appWorkspace].Worktree, nil
	case appPane != "":
		return panePath(appPane)
	case len(args) == 2:
		return args[1], nil
	}
	return "", nil
}

// panePath is a tmux pane's current path.
func panePath(pane string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux pane %s: %w", pane, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runApp opens the configured app for the agent at dir. Worktree apps act on the
// agent's worktree (dir's worktree, or dir itself when it isn't a kovan
// worktree); the notes app opens the editor on the agent's durable task-doc dir.
func runApp(kind, dir, sessionName string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	switch kind {
	case "editor", "e":
		return launchApp(global.Apps.Editor, worktreeFrom(dir))
	case "merge", "s":
		return launchApp(global.Apps.Merge, worktreeFrom(dir))
	case "terminal", "term", "t":
		return openTerminal(global.Apps, worktreeFrom(dir))
	case "notes", "w":
		notes, err := notesDirFor(sessionName, dir)
		if err != nil {
			return err
		}
		return launchApp(global.Apps.Editor, notes)
	default:
		return fmt.Errorf("unknown app %q (editor|merge|terminal|notes)", kind)
	}
}

// worktreeFrom resolves the worktree root for dir: the agent workspace (primary
// or extra) containing it, or dir itself when dir isn't inside one.
func worktreeFrom(dir string) string {
	if _, w, found, _ := session.FindWorkspace(dir); found {
		return w.Worktree
	}
	return dir
}

// notesDirFor resolves an agent's durable task-doc dir. With sessionName set the
// tab is loaded by its tmux name, so sibling tabs sharing a worktree resolve to
// their own per-id dir; otherwise the agent is found from dir's worktree.
func notesDirFor(sessionName, dir string) (string, error) {
	var m *session.Manifest
	if sessionName != "" {
		got, err := session.ReadByTmux(sessionName)
		if err != nil {
			return "", fmt.Errorf("no agent for session %q: %w", sessionName, err)
		}
		m = got
	} else {
		got, _, found, err := session.FindWorkspace(dir)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("no kovan agent at %q", dir)
		}
		m = got
	}
	notes, err := taskDocsFor(m)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(notes); err != nil {
		return "", fmt.Errorf("no task docs for %s: %w", m.ID, err)
	}
	return notes, nil
}

// launchApp runs an external GUI program against an agent's worktree, detached,
// and returns immediately — the board keeps running while the editor or git GUI
// opens. cmdStr is split on spaces; a "{path}" token is replaced with the
// worktree, otherwise the worktree is appended as the final argument. Quoted or
// multi-word arguments are out of scope, matching tmux.options.
func launchApp(cmdStr, worktree string) error {
	fields := strings.Fields(cmdStr)
	if len(fields) == 0 {
		return fmt.Errorf("no command configured")
	}
	substituted := false
	for i, f := range fields {
		if strings.Contains(f, "{path}") {
			fields[i] = strings.ReplaceAll(f, "{path}", worktree)
			substituted = true
		}
	}
	if !substituted {
		fields = append(fields, worktree)
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Dir = worktree
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %q: %w", fields[0], err)
	}
	// Release the child so it keeps running after the board process; we never
	// wait on it.
	return cmd.Process.Release()
}

// openTerminal opens a terminal in the worktree. When apps.terminal is set it
// runs like any other opener; otherwise on macOS it opens a new iTerm2 tab cd'd
// to the worktree (the common "drop me a shell here" want).
func openTerminal(apps config.Apps, worktree string) error {
	if apps.Terminal != "" {
		return launchApp(apps.Terminal, worktree)
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("set apps.terminal in ~/.kovan/config.yaml to open a terminal here")
	}
	return openITermTab(worktree)
}

// openITermTab opens a new tab in the current iTerm2 window and cd's it to the
// worktree, detached. kovan's own TUI runs in a terminal, so a current window
// exists.
func openITermTab(worktree string) error {
	script := fmt.Sprintf(`tell application "iTerm2"
	activate
	tell current window to create tab with default profile
	tell current session of current window to write text "cd %s"
end tell`, shellQuote(worktree))
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open iTerm2 tab: %w", err)
	}
	return cmd.Process.Release()
}

// appMenuArgs builds the `tmux display-menu` arguments for agent m's app menu on
// client. With one workspace the items are editor / merge / terminal on its
// worktree; with several, each app is listed per repo, keyed e/s/t for the
// primary and E/S/T for the first extra. Notes opens the agent's task docs,
// which are per agent, so it appears once. pane stands in for a session that
// is no kovan agent (m.Tmux empty): its apps act on the pane's path, no notes.
func appMenuArgs(m *session.Manifest, client, pane string) []string {
	args := []string{"display-menu", "-c", client, "-x", "C", "-y", "C", "-T", "kovan apps"}
	apps := []struct{ kind, key string }{{"editor", "e"}, {"merge", "s"}, {"terminal", "t"}}
	if m.Tmux == "" {
		for _, a := range apps {
			args = append(args, a.kind, a.key, tmuxShellCommand("kovan app "+a.kind+" --pane "+shellQuote(pane)))
		}
		return args
	}
	sess := " --session " + shellQuote(m.Tmux)
	workspaces := m.Workspaces()
	for i, w := range workspaces {
		if i > 0 {
			args = append(args, "")
		}
		for _, a := range apps {
			name, key := a.kind, a.key
			if len(workspaces) > 1 {
				name = a.kind + " · " + tmuxEscape(w.Repo)
				key = menuKey(a.key, i)
			}
			args = append(args, name, key, tmuxShellCommand(fmt.Sprintf("kovan app %s%s --workspace %d", a.kind, sess, i)))
		}
	}
	return append(args, "", "notes", "w", tmuxShellCommand("kovan app notes"+sess))
}

// menuKey is the shortcut for an app on the i-th workspace: lowercase for the
// primary, uppercase for the first extra, none past that.
func menuKey(key string, i int) string {
	switch i {
	case 0:
		return key
	case 1:
		return strings.ToUpper(key)
	default:
		return ""
	}
}

// tmuxShellCommand wraps a shell command line as a tmux run-shell command. Inside
// tmux double quotes a backslash, quote or $ is special, and a menu command is
// expanded as a format twice (when chosen, then by run-shell), so '#' is
// doubled twice; each reaches the shell literally.
func tmuxShellCommand(line string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "#", "####")
	return `run-shell "` + r.Replace(line) + `"`
}

// showAppMenu opens the app menu for the agent in tmux session name on client.
// A tmux session that is no kovan agent gets the apps on pane's path.
func showAppMenu(name, client, pane string) error {
	m, err := session.ReadByTmux(name)
	if err != nil {
		m = &session.Manifest{}
	}
	if out, err := exec.Command("tmux", appMenuArgs(m, client, pane)...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux display-menu: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
