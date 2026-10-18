package app

import (
	"fmt"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/spf13/cobra"
)

var (
	worktreeAgent  string
	worktreeBase   string
	worktreeBranch string
	worktreeForce  bool
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Give an agent worktrees in other repos",
	Long: `An agent starts in one repo. When its task spans more, "kovan worktree add"
gives it a worktree in another repo, made with that repo's kovan config. kovan
then tracks it as part of the agent: the apps menu offers it, wake grants it,
and remove tears it down.

Run it from inside the agent's session, or name the agent with --agent <id>.`,
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <repo|path>",
	Short: "Create (or adopt) a worktree in another repo for this agent",
	Long: `Create a worktree for this agent in another repo, named by its repo name or
path: <prefix>-<id> next to that repo, on its branch template filled with the
agent's id and title, off its base. A path to an existing linked worktree is
adopted as-is, and removing the agent leaves an adopted worktree on disk.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := actingAgent(worktreeAgent)
		if err != nil {
			return err
		}
		ws, tracked, err := addWorkspace(m, args[0], worktreeBase, worktreeBranch)
		if err != nil {
			return err
		}
		verb := "Created"
		switch {
		case tracked:
			verb = "Already tracking"
		case ws.Adopted:
			verb = "Adopted"
		}
		fmt.Printf("%s %s worktree %s on branch %s.\n", verb, ws.Repo, ws.Worktree, ws.Branch)
		if home, err := config.Dir(); err == nil {
			fmt.Printf("Its private context: %s\n", filepath.Join(home, "projects", ws.Repo))
		}
		fmt.Printf("A running agent gains access with: /add-dir %s\n", ws.Worktree)
		return nil
	},
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List this agent's worktrees, the primary first",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := actingAgent(worktreeAgent)
		if err != nil {
			return err
		}
		for _, w := range m.Workspaces() {
			fmt.Printf("%s\t%s\t%s\n", w.Repo, w.Worktree, w.Branch)
		}
		return nil
	},
}

var worktreeRmCmd = &cobra.Command{
	Use:   "rm <repo|path>",
	Short: "Drop one of this agent's extra worktrees, keeping its branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := actingAgent(worktreeAgent)
		if err != nil {
			return err
		}
		ws, err := removeWorkspace(m, args[0], worktreeForce)
		if err != nil {
			return err
		}
		if ws.Adopted {
			fmt.Printf("Stopped tracking %s; it was adopted, so it stays on disk.\n", ws.Worktree)
			return nil
		}
		if sharedWorkspace(m.Tmux, ws.Worktree) {
			fmt.Printf("Stopped tracking %s; another agent works in it, so it stays on disk.\n", ws.Worktree)
			return nil
		}
		fmt.Printf("Removed %s. Branch %s kept.\n", ws.Worktree, ws.Branch)
		return nil
	},
}

func init() {
	worktreeCmd.PersistentFlags().StringVar(&worktreeAgent, "agent", "", "the agent's id (default: the calling agent)")
	worktreeAddCmd.Flags().StringVar(&worktreeBase, "from", "", "base to branch from (default: the repo's base)")
	worktreeAddCmd.Flags().StringVar(&worktreeBranch, "branch", "", "branch name (default: the repo's branch template)")
	worktreeRmCmd.Flags().BoolVarP(&worktreeForce, "force", "f", false, "remove the worktree even with uncommitted changes")
	worktreeCmd.AddCommand(worktreeAddCmd, worktreeListCmd, worktreeRmCmd)
	// The root's template lists grouped commands only; these subcommands are
	// ungrouped, so the worktree help uses cobra's stock template.
	worktreeCmd.SetUsageTemplate((&cobra.Command{}).UsageTemplate())
}
