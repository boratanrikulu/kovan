package app

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kovan",
	Short: "Run many coding agents in parallel; keep your method in one place",
	Long: `kovan runs many coding agents in parallel, each in its own git worktree,
and keeps your AI working method in a single place that governs every project.

It does not replace Claude Code. It sets the stage and the agent does the work.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// On a terminal the board is the home screen; piped or in CI, fall back
		// to the plain status print so kovan stays scriptable.
		if isTerminal(os.Stdout) {
			return runUI()
		}
		return runStatus()
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kovan:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	// List commands in registration order, not alphabetically, so each group
	// reads in its intended order (setup before init, start before the rest).
	cobra.EnableCommandSorting = false

	// Groups make help read task-first. They must be registered before any
	// AddCommand carrying a matching GroupID, or cobra panics.
	rootCmd.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup:"},
		&cobra.Group{ID: "agents", Title: "Agents:"},
		&cobra.Group{ID: "method", Title: "Method:"},
	)
	setupCmd.GroupID = "setup"
	initCmd.GroupID = "setup"
	startCmd.GroupID = "agents"
	openCmd.GroupID = "agents"
	removeCmd.GroupID = "agents"
	statusCmd.GroupID = "agents"
	methodCmd.GroupID = "method"

	// Machine-called plumbing, kept off the help listing: hooks call `gate run`,
	// the shell-init wrapper calls `switch`, and task is the off-menu CLI twin of
	// the method/TUI flow.
	for _, c := range []*cobra.Command{
		switchCmd, shellInitCmd, gateCmd, taskCmd,
	} {
		c.Hidden = true
	}

	rootCmd.AddCommand(
		setupCmd,
		initCmd,
		startCmd,
		openCmd,
		removeCmd,
		statusCmd,
		methodCmd,
		gateCmd,
		taskCmd,
		shellInitCmd,
		switchCmd,
		appCmd,
	)

	// cobra's default usage template force-lists the help command; this one
	// renders the groups and lists only available (non-hidden) commands, so help
	// and the plumbing stay functional but unlisted.
	rootCmd.SetUsageTemplate(usageTemplate)
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "help [command]",
		Short:  "Help about any command",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			target, _, err := rootCmd.Find(args)
			if err != nil || target == nil {
				target = rootCmd
			}
			_ = target.Help()
		},
	})
}

const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{if not .HasParent}}                 open the board (the home screen){{end}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if .HasAvailableSubCommands}}
{{range $group := .Groups}}
{{$group.Title}}{{range $.Commands}}{{if (and (eq .GroupID $group.ID) .IsAvailableCommand)}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}
{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// errNotImplemented marks a command stubbed until its roadmap milestone lands.
func errNotImplemented(name string) error {
	return fmt.Errorf("%q is not implemented yet (see docs/AGENTS.md roadmap)", name)
}
