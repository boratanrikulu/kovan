package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:   "shell-init <shell>",
	Short: "Print the shell wrapper that lets attach/switch change directory",
	Long: `Print a shell function that wraps kovan so that 'kovan switch <id>' cds into
the agent's worktree. Add it to your shell rc:

  eval "$(kovan shell-init zsh)"`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash", "zsh":
			fmt.Print(posixWrapper)
			return nil
		default:
			return fmt.Errorf("unsupported shell %q (supported: bash, zsh)", args[0])
		}
	},
}

// posixWrapper works for both bash and zsh: `switch` captures the printed path
// and cds; every other subcommand passes straight through.
const posixWrapper = `kovan() {
    if [ "$1" = "switch" ]; then
        local _kovan_dir
        _kovan_dir="$(command kovan switch "${@:2}")" || return $?
        cd "$_kovan_dir"
    else
        command kovan "$@"
    fi
}
`
