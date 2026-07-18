package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	Long: `Scaffold ~/.kovan if needed, then launch Claude in this repo to read its
existing AI files and sort them into kovan's layers — global method, per-repo
private context, and what stays in the repo — writing .kovan.yaml with you.
kovan does the deterministic scaffolding; Claude does the judgment. Re-runnable;
the launched session never clobbers your files without asking.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit(initReference, initAccount)
	},
}

func init() {
	initCmd.Flags().StringVar(&initReference, "reference", "", "repo whose AGENTS.md rules to lift into the global method")
	initCmd.Flags().StringVar(&initAccount, "account", "", "Claude account to run the onboarding session under (see accounts in config)")
}

func runInit(reference, account string) error {
	repo, data, err := prepareInit(reference, account)
	if err != nil {
		return err
	}
	// Wire the Claude Code hooks here too, so `init` alone is enough — no
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
	claude, err := claudeDir()
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
	// access to ~/.kovan and ~/.claude (and the reference repo) via --add-dir.
	args := []string{"--add-dir", home, "--add-dir", claude}
	if reference != "" {
		args = append(args, "--add-dir", reference)
	}
	// "--" before the prompt: --add-dir is variadic and would otherwise swallow
	// the positional prompt, leaving claude with "no prompt".
	args = append(args, "--", "Read "+promptPath+" and follow it to onboard this repository into kovan.")

	cmd := initLaunch(global.Agent, args, tokenFile)
	cmd.Dir = repo.Root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Env is left unset so the child inherits ours — kovan stays on PATH for the
	// `kovan method link` step the onboarding session runs.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch %s: %w", global.Agent, err)
	}
	return nil
}

// initLaunch builds the onboarding launch. With an account in play the agent
// runs through a shell that reads the token file at exec time — like every
// other launch, only the path appears in argv, never the token value.
func initLaunch(agent string, args []string, tokenFile string) *exec.Cmd {
	if tokenFile == "" {
		return exec.Command(agent, args...)
	}
	script := oauthEnvKey + "=" + tokenReadExpr(tokenFile) + " exec " + shellQuote(agent)
	for _, a := range args {
		script += " " + shellQuote(a)
	}
	return exec.Command("sh", "-c", script)
}

// ensureHooks installs kovan's Claude Code hooks when missing, so `kovan init`
// alone yields a live board — no separate `kovan setup` step first. Idempotent:
// a no-op when they are already wired.
func ensureHooks() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	added, err := installHooks(path)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		fmt.Printf("Installed kovan's Claude Code hooks into %s.\n", path)
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
	if err := config.ScaffoldRepo(repo.Root); err != nil {
		return nil, onboard.Data{}, err
	}
	if err := method.Scaffold(home); err != nil {
		return nil, onboard.Data{}, err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, onboard.Data{}, err
	}
	repoCfg, err := config.LoadRepo(repo.Root)
	if err != nil {
		return nil, onboard.Data{}, err
	}
	claude, err := claudeDir()
	if err != nil {
		return nil, onboard.Data{}, err
	}

	return repo, onboard.Data{
		Repo:        repo.Root,
		ClaudeMD:    filepath.Join(claude, "CLAUDE.md"),
		Account:     resolveAccount(account, repoCfg.Account, global.DefaultAccount),
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
