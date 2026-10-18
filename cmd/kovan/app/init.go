package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/onboard"
	"github.com/spf13/cobra"
)

var (
	initReference string
	initAccount   string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Onboard this repo into kovan (scaffold method + config, sort its AI files)",
	Long: `Scaffold ~/.kovan if needed, then launch your default agent CLI in this repo
to read its existing AI files and sort them into kovan's layers (global method,
per-repo private context, and what stays in the repo), writing the repo's
settings to ~/.kovan/projects/<repo>/config.yaml with you. kovan does the
deterministic scaffolding; the agent does the judgment. Re-runnable; the
launched session never clobbers your files without asking.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit(initReference, initAccount)
	},
}

func init() {
	initCmd.Flags().StringVar(&initReference, "reference", "", "repo whose AGENTS.md rules to lift into the global method")
	initCmd.Flags().StringVar(&initAccount, "account", "", "account to run the onboarding session under (see accounts in config)")
}

func runInit(reference, account string) error {
	repo, data, err := prepareInit(reference, account)
	if err != nil {
		return err
	}
	// Wire the agent CLIs' hooks here too, so `init` alone is enough: no
	// "init or setup first?" sequencing for the user to get wrong.
	if err := ensureHooks(); err != nil {
		return err
	}
	prompt, err := onboard.Prompt(data)
	if err != nil {
		return err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	// Validate the account before the launch, so a missing token file is a
	// clear error here, not a session dying at "Not logged in".
	tokenFile, err := accountTokenFile(global, data.Account)
	if err != nil {
		return err
	}
	home, err := config.Dir()
	if err != nil {
		return err
	}
	repoCfg, err := config.LoadRepo(home, filepath.Base(repo.Root))
	if err != nil {
		return err
	}
	agentName := onboardAgent(global, repoCfg)
	d, err := driverOf(agentName)
	if err != nil {
		return err
	}

	// Hand the prompt over as a file, not a CLI arg: Claude Code stats a
	// positional arg as a possible path, and a multi-KB prompt blows the
	// filename limit (ENAMETOOLONG). The file lives under ~/.kovan (already
	// granted) for the session and is removed when it ends.
	promptPath, err := writeOnboardPrompt(home, prompt)
	if err != nil {
		return err
	}
	defer os.Remove(promptPath)

	// Run in the repo (natural for reading/writing repo files); grant write
	// access to ~/.kovan and the CLI's home (and the reference repo).
	dirs := []string{home, d.DefaultHome()}
	if reference != "" {
		dirs = append(dirs, reference)
	}
	cmd := initLaunch(authPrefix(global, d, data.Account, tokenFile), d.Command(agent.Launch{
		Bin:    agentBin(global, agentName),
		Prompt: "Read " + promptPath + " and follow it to onboard this repository into kovan.",
		Dirs:   dirs,
	}))
	cmd.Dir = repo.Root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The child inherits our env, so kovan stays on PATH for the `kovan method
	// link` step the onboarding session runs; only the tab name goes, since this
	// session is no agent's.
	cmd.Env = withoutEnv(os.Environ(), sessionEnv)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch %s: %w", agentName, err)
	}
	return nil
}

// onboardAgent is the agent CLI that onboards a repo: the repo's default, else
// the global one, as for an agent started there.
func onboardAgent(global *config.Global, repoCfg *config.Repo) string {
	return resolveAgentName("", repoCfg.Agent, global.Agent)
}

// initLaunch runs the onboarding launch through a shell, which reads an
// account's token file at exec time: like every other launch, only the path
// appears in argv, never the token value.
func initLaunch(prefix, command string) *exec.Cmd {
	return exec.Command("sh", "-c", prefix+"exec "+command)
}

// ensureHooks installs kovan's hooks when missing, so `kovan init` alone yields
// a live board: no separate `kovan setup` step first. Idempotent: a no-op when
// they are already wired.
func ensureHooks() error {
	global, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	for _, t := range hookTargets(global) {
		added, err := installHooks(t.path, t.d)
		if err != nil {
			return err
		}
		if len(added) > 0 {
			fmt.Printf("Installed kovan's %s hooks into %s.\n", t.d.Name(), t.path)
		}
	}
	return nil
}

// prepareInit does the deterministic half of init: ensure ~/.kovan exists and
// resolve the onboarding context for the current repo. It scaffolds but writes
// no config — the launched Claude does that, with the user.
func prepareInit(reference, account string) (*git.Repo, onboard.Data, error) {
	repo, err := openRepo()
	if err != nil {
		return nil, onboard.Data{}, err
	}
	home, err := config.Dir()
	if err != nil {
		return nil, onboard.Data{}, err
	}
	// Scaffold config first so the user lands in a ~/.kovan with a yaml to
	// discover, then the empty method layer dirs to author into. Never clobbers.
	if err := config.ScaffoldGlobal(home); err != nil {
		return nil, onboard.Data{}, err
	}
	if err := config.ScaffoldRepo(home, filepath.Base(repo.Root)); err != nil {
		return nil, onboard.Data{}, err
	}
	if err := method.Scaffold(home); err != nil {
		return nil, onboard.Data{}, err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, onboard.Data{}, err
	}
	repoCfg, err := config.LoadRepo(home, filepath.Base(repo.Root))
	if err != nil {
		return nil, onboard.Data{}, err
	}
	claude, err := claudeDir()
	if err != nil {
		return nil, onboard.Data{}, err
	}
	d, err := driverOf(onboardAgent(global, repoCfg))
	if err != nil {
		return nil, onboard.Data{}, err
	}
	if account, err = accountFor(global, d, account, repoCfg.Account); err != nil {
		return nil, onboard.Data{}, err
	}

	return repo, onboard.Data{
		Repo:        repo.Root,
		ClaudeMD:    filepath.Join(claude, "CLAUDE.md"),
		Account:     account,
		Reference:   reference,
		GlobalEmpty: len(method.Global(home)) == 0,
	}, nil
}

// writeOnboardPrompt drops the rendered prompt into a temp file under ~/.kovan
// (which the launched Claude can read) and returns its path.
func writeOnboardPrompt(home, prompt string) (string, error) {
	f, err := os.CreateTemp(home, ".onboard-*.md")
	if err != nil {
		return "", fmt.Errorf("write onboarding prompt: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(prompt); err != nil {
		return "", fmt.Errorf("write onboarding prompt: %w", err)
	}
	return f.Name(), nil
}

func claudeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude"), nil
}
