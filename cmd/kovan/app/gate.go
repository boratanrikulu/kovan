package app

import "github.com/spf13/cobra"

var gateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Hook dispatcher (called by Claude Code, not by hand)",
	Long: `kovan is the single hook handler. Claude Code's hooks call "kovan gate run",
which keeps each agent's manifest live. Run "kovan setup" to wire those hooks
into ~/.claude/settings.json.`,
}

var gateBuildCmd = &cobra.Command{
	Use:    "build",
	Short:  "Compile custom gates in ~/.kovan/gates into the cache",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return errNotImplemented("gate build")
	},
}

func init() {
	gateCmd.AddCommand(gateBuildCmd, gateRunCmd)
}
