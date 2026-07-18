package app

import (
	"fmt"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/taskdoc"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Per-agent work context (context / test-plan / learnings)",
}

var taskNewCmd = &cobra.Command{
	Use:   "new <id> [title]",
	Short: "Scaffold a task-doc dir from templates",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := ""
		if len(args) > 1 {
			title = args[1]
		}
		return runTaskNew(args[0], title)
	},
}

func runTaskNew(id, title string) error {
	repo, err := openRepo()
	if err != nil {
		return err
	}
	repoCfg, err := config.LoadRepo(repo.Root)
	if err != nil {
		return err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	home, err := config.Dir()
	if err != nil {
		return err
	}
	// Scaffold the repo's default mode's docs (the same set `kovan start` would).
	taskMode, err := mode.Load(home, resolveMode("", repoCfg.Mode, global.DefaultMode))
	if err != nil {
		return err
	}
	// Scaffold into the durable kovan store, the same place `kovan start` uses,
	// so the docs persist independent of any worktree.
	dest := taskDocsDir(home, filepath.Base(repo.Root), repoCfg.Task.Dir, id)
	if err := taskdoc.Scaffold(dest, id, title, repoCfg.Task.Token, templatesDir(), taskMode.Docs); err != nil {
		return err
	}
	fmt.Printf("Scaffolded task docs at %s\n", dest)
	return nil
}

func init() {
	taskCmd.AddCommand(taskNewCmd)
}
