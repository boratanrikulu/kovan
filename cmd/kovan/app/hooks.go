package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// hookEnv is the context passed to per-repo setup/teardown hooks.
type hookEnv struct {
	Path   string // worktree path
	ID     string
	Branch string
	Base   string
	Main   string // main worktree path
	Repo   string // repo name
}

func (h hookEnv) vars() []string {
	return append(os.Environ(),
		"KOVAN_PATH="+h.Path,
		"KOVAN_ID="+h.ID,
		"KOVAN_BRANCH="+h.Branch,
		"KOVAN_BASE="+h.Base,
		"KOVAN_MAIN="+h.Main,
		"KOVAN_REPO="+h.Repo,
	)
}

// runRepoHook runs <repoRoot>/.kovan/<name>.sh if it exists, streaming its
// output. A missing hook is not an error.
func runRepoHook(repoRoot, name string, env hookEnv) error {
	script := filepath.Join(repoRoot, ".kovan", name+".sh")
	if _, err := os.Stat(script); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = env.Path
	cmd.Env = env.vars()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook: %w", name, err)
	}
	return nil
}
