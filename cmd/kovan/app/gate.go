package app

import "github.com/spf13/cobra"

var gateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Hook dispatcher (called by the agent CLIs, not by hand)",
	Long: `kovan is the single hook handler. Each agent CLI's hooks call "kovan gate run",
which keeps each agent's manifest live and checks its tool calls against the
gates. Run "kovan setup" to wire those hooks into every agent CLI.`,
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
