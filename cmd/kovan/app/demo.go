package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/boratanrikulu/kovan/demo"
	"github.com/spf13/cobra"
)

var (
	demoRoot     string
	demoRemove   bool
	demoSeedOnly bool
)

// defaultDemoRoot keeps the demo world out of the user's home, somewhere a
// reboot clears on its own.
func defaultDemoRoot() string {
	return filepath.Join(os.TempDir(), "kovan-demo")
}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Seed a throwaway board and open it (no account, no tokens)",
	Long: `Build a self-contained demo world and open the cockpit on it: fake agents
across seven tiny repos, each with a worktree, a manifest and a tmux session.

Nothing here touches your real ~/.kovan, your Claude account, or your repos.
It needs git and tmux, and nothing else. Remove it with --remove.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := demoRoot
		if root == "" {
			root = defaultDemoRoot()
		}
		if demoRemove {
			if err := demo.Teardown(root); err != nil {
				return err
			}
			fmt.Printf("demo removed (%s, kovan-demo-* tmux sessions)\n", root)
			return nil
		}
		home, err := demo.Seed(root)
		if err != nil {
			return err
		}
		if demoSeedOnly {
			fmt.Printf("demo seeded under %s\n\nopen the board:\n  cd %s && KOVAN_HOME=%s kovan\n", root, demo.Cockpit(root), home)
			return nil
		}
		fmt.Printf("demo seeded under %s\nremove it with: kovan demo --remove\n\n", root)
		return openDemoBoard(home, demo.Cockpit(root))
	},
}

// openDemoBoard runs the board against the demo home in a child process. A
// fresh process is what keeps the demo hermetic: KOVAN_HOME is read once at
// startup, so re-execing is how the board sees the demo world and nothing of
// the real one.
func openDemoBoard(home, dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	board := exec.Command(exe)
	board.Dir = dir
	board.Env = append(os.Environ(), "KOVAN_HOME="+home)
	board.Stdin, board.Stdout, board.Stderr = os.Stdin, os.Stdout, os.Stderr
	return board.Run()
}

func init() {
	demoCmd.Flags().StringVar(&demoRoot, "root", "", "where the demo world lives (default $TMPDIR/kovan-demo)")
	demoCmd.Flags().BoolVar(&demoRemove, "remove", false, "tear the demo world down and exit")
	demoCmd.Flags().BoolVar(&demoSeedOnly, "seed-only", false, "build the world but do not open the board")
}
