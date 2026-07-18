package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Tmux runs agent sessions in tmux.
type Tmux struct{}

// SessionName sanitizes raw into a name tmux accepts: tmux treats '.' and ':'
// specially in target names, so they are replaced with '-'.
func SessionName(raw string) string {
	return strings.NewReplacer(".", "-", ":", "-", " ", "-").Replace(raw)
}

// Start launches the session detached via `tmux new-session -d`.
func (Tmux) Start(s Session) error {
	if err := ensureTmux(); err != nil {
		return err
	}
	args := []string{"new-session", "-d", "-s", s.Name}
	if s.Title != "" {
		args = append(args, "-n", s.Title)
	}
	args = append(args, "-c", s.Dir)
	for _, e := range s.Env {
		args = append(args, "-e", e)
	}
	args = append(args, s.Cmd)
	cmd := exec.Command("tmux", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux new-session %q: %w", s.Name, err)
	}
	applyOptions(s.Name, s.Options)
	applyTitleOptions(s.Name)
	applyScrollBindings()
	applyAppMenuBinding()
	return nil
}

// applyAppMenuBinding binds "prefix k" to a tmux menu that opens the configured
// apps (editor / merge / terminal on the worktree, or "notes" — the editor on
// the agent's task-doc dir) for the agent in the current pane, so they're
// reachable from inside an agent's session — where the board's e/s/t/w keys
// aren't. Notes passes the session name (not just the pane path), since the
// task-doc dir is keyed per tab. Like the scroll bindings it lives in the
// server-global key table (no per-session binding exists); it uses the unbound
// "prefix k" so it doesn't clobber tmux's default prefix keys. Best-effort.
func applyAppMenuBinding() {
	item := func(kind string) string {
		return fmt.Sprintf("run-shell \"kovan app %s '#{pane_current_path}'\"", kind)
	}
	notesItem := "run-shell \"kovan app notes --session '#{session_name}' '#{pane_current_path}'\""
	args := []string{
		"bind-key", "k", "display-menu", "-T", "kovan apps",
		"editor", "e", item("editor"),
		"merge", "s", item("merge"),
		"terminal", "t", item("terminal"),
		"notes", "w", notesItem,
	}
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "kovan: tmux bind app menu: %s\n", strings.TrimSpace(string(out)))
	}
}

// applyTitleOptions keeps the window name kovan set (id + title) from being
// overwritten — tmux auto-renames windows to the running command by default —
// and mirrors it to the terminal's tab title via set-titles. Session-scoped, so
// the user's other tmux windows are untouched.
func applyTitleOptions(session string) {
	applyOptions(session, []string{
		"automatic-rename off",
		"allow-rename off",
		"set-titles on",
		"set-titles-string #W",
	})
}

// scrollBindings make the mouse wheel scroll smoothly under `mouse on`: wheel-up
// over an app that does not grab the mouse enters copy-mode (instead of tmux's
// default arrow-key burst), and each notch then scrolls a couple of lines rather
// than a chunk. Apps that do grab the mouse still receive the wheel directly.
//
// tmux key bindings live in a server-global key table — there is no per-session
// binding — so these affect every tmux session, not just kovan's. That is a
// deliberate, benign departure from kovan's otherwise session-scoped options,
// accepted in exchange for scrolling that feels native.
var scrollBindings = [][]string{
	{"bind-key", "-n", "WheelUpPane", "if-shell", "-F", "-t", "=", "#{mouse_any_flag}", "send-keys -M", "copy-mode -e"},
	{"bind-key", "-T", "copy-mode", "WheelUpPane", "send-keys", "-X", "-N", "2", "scroll-up"},
	{"bind-key", "-T", "copy-mode", "WheelDownPane", "send-keys", "-X", "-N", "2", "scroll-down"},
	{"bind-key", "-T", "copy-mode-vi", "WheelUpPane", "send-keys", "-X", "-N", "2", "scroll-up"},
	{"bind-key", "-T", "copy-mode-vi", "WheelDownPane", "send-keys", "-X", "-N", "2", "scroll-down"},
}

// applyScrollBindings installs the smooth-wheel key bindings, best-effort and
// idempotent (re-binding the same key is harmless), so a tmux quirk never fails
// the spawn.
func applyScrollBindings() {
	for _, b := range scrollBindings {
		if out, err := exec.Command("tmux", b...).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "kovan: tmux %s: %s\n", strings.Join(b, " "), strings.TrimSpace(string(out)))
		}
	}
}

// applyOptions sets session-scoped tmux options. It is best-effort: the session
// and the agent are already up, so a bad option only warns — failing here would
// roll the spawn back and delete the agent over a tmux typo.
func applyOptions(session string, options []string) {
	for _, opt := range options {
		key, value := splitOption(opt)
		if key == "" {
			continue
		}
		args := []string{"set-option", "-t", session, key}
		if value != "" {
			args = append(args, value)
		}
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "kovan: tmux set-option %q: %s\n", opt, strings.TrimSpace(string(out)))
		}
	}
}

// splitOption parses "key value..." into the option key (first token) and value
// (the remainder). Quoted or multi-word values are out of scope for v1.
func splitOption(opt string) (key, value string) {
	fields := strings.SplitN(strings.TrimSpace(opt), " ", 2)
	key = fields[0]
	if len(fields) == 2 {
		value = strings.TrimSpace(fields[1])
	}
	return key, value
}

// Attach replaces the current process with the handover command, handing over
// the terminal. It returns only if exec fails.
func (Tmux) Attach(name string) error {
	if err := ensureTmux(); err != nil {
		return err
	}
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	args, _ := attachPlan(name, inTmux())
	return syscall.Exec(path, append([]string{"tmux"}, args...), os.Environ())
}

// AttachCmd returns the handover command and whether it blocks the caller.
func (Tmux) AttachCmd(name string) (*exec.Cmd, bool) {
	args, blocking := attachPlan(name, inTmux())
	return exec.Command("tmux", args...), blocking
}

// attachPlan picks how to hand over to a session. Inside tmux we switch the
// client (non-blocking, so a running TUI survives and can be switched back to);
// otherwise we attach, which takes over the terminal until detach.
func attachPlan(name string, inTmux bool) (args []string, blocking bool) {
	if inTmux {
		return []string{"switch-client", "-t", name}, false
	}
	return []string{"attach", "-t", name}, true
}

// Capture returns the last lines of the session's visible output.
func (Tmux) Capture(name string, lines int) (string, error) {
	if err := ensureTmux(); err != nil {
		return "", err
	}
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", name).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane %q: %w", name, err)
	}
	return tailLines(string(out), lines), nil
}

// tailLines drops trailing blank lines (capture-pane pads to the pane height)
// and returns at most the last n lines.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Kill terminates the session.
func (Tmux) Kill(name string) error {
	if err := ensureTmux(); err != nil {
		return err
	}
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux kill-session %q: %s", name, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Exists reports whether the session is alive.
func (Tmux) Exists(name string) (bool, error) {
	if err := ensureTmux(); err != nil {
		return false, err
	}
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	return false, err
}

// Sessions returns every live session's name. A non-zero exit means the tmux
// server isn't running, i.e. no sessions.
func (Tmux) Sessions() ([]string, error) {
	if err := ensureTmux(); err != nil {
		return nil, err
	}
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

func ensureTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found on PATH; install tmux or set runner to a supported alternative")
	}
	return nil
}

func inTmux() bool {
	return os.Getenv("TMUX") != ""
}
