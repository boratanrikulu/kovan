package app

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	statusFilter   string
	statusArchived bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the board as plain text (for scripts/CI)",
	Long: `Read the per-worktree session manifests and print one board: which agent
is working, which needs you, which is done, and how long each has been idle.
By default only active agents are shown; --archived lists the archived ones
instead; --filter narrows by a substring match across
id/title/repo/branch/mode/account/state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusFilter, "filter", "", "show only agents matching this substring")
	statusCmd.Flags().BoolVar(&statusArchived, "archived", false, "list archived agents instead of active ones")
}

func runStatus() error {
	rows, err := loadBoard()
	if err != nil {
		return err
	}
	rows = filterRows(rows, statusFilter, statusArchived)
	if len(rows) == 0 {
		fmt.Println("No agents. Start one with `kovan start <id> <title>`.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE\tMODE\tID\tREPO\tACCOUNT\tAGE\tWORKSPACE\tTITLE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.State, orDash(r.Mode), idCell(r), r.Repo, orDash(r.Account), r.Age, workspaceCell(r), truncate(r.Title, 50))
	}
	return w.Flush()
}
