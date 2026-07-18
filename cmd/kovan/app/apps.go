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
// on the agent's task-doc dir via "notes") for the agent at a path — the same
// actions as the board's e/s/t/w keys, but runnable from inside an agent's own
// tmux session (where the board's keymap isn't active). The tmux app-menu
// binding calls this with the pane's path; for "notes" it also passes
// --session, since the task-doc dir is keyed per tab.
var appCmd = &cobra.Command{
	Use:    "app <editor|merge|terminal|notes> [path]",
	Short:  "Open an agent's app at a path (used by the in-session tmux menu)",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := ""
		if len(args) == 2 {
			dir = args[1]
		}
		return runApp(args[0], dir, appSession)
	},
}

// appSession names the exact tab (by tmux session) for the notes app, so sibling
// tabs sharing a worktree still resolve to their own task-doc dir.
var appSession string

func init() {
	appCmd.Flags().StringVar(&appSession, "session", "", "resolve the agent by tmux session name (for the notes app, keyed per tab)")
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

// worktreeFrom resolves the worktree root for dir: the containing agent's
// worktree, or dir itself when dir isn't inside a kovan worktree.
func worktreeFrom(dir string) string {
	if m, found, _ := session.FindFrom(dir); found {
		return m.Worktree
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
		got, found, err := session.FindFrom(dir)
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
