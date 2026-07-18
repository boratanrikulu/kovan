package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

// switchCmd prints an agent's worktree path. It is meant to be wrapped by the
// shell function from `kovan shell-init` so `kovan switch <id>` changes dir.
var switchCmd = &cobra.Command{
	Use:   "switch <id>",
	Short: "Print an agent's worktree path (used by the shell-init wrapper to cd)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := findSession(args[0])
		if err != nil {
			return err
		}
		fmt.Println(m.Worktree)
		return nil
	},
}
