package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// editorCommand builds the command that opens path in the user's editor,
// falling back to vi when $EDITOR is unset. $EDITOR may carry flags (e.g.
// "code -w"), so it is split into words.
func editorCommand(path string) *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	fields := strings.Fields(editor)
	return exec.Command(fields[0], append(fields[1:], path)...)
}

// runEditor opens path in the editor, wired to the current terminal. It is the
// CLI handoff; the TUI uses editorCommand via tea.ExecProcess instead.
func runEditor(path string) error {
	c := editorCommand(path)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	return nil
}
