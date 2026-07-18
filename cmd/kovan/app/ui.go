package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func runUI() error {
	m, err := newModel()
	if err != nil {
		return err
	}
	// AllMotion (not CellMotion) so hover reports without a button held —
	// hover-select on the board needs it.
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

// isCharDevice reports whether a file mode is a character device — a terminal,
// as opposed to a pipe or regular file.
func isCharDevice(mode os.FileMode) bool {
	return mode&os.ModeCharDevice != 0
}

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return isCharDevice(info.Mode())
}
