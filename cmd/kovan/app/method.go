package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/spf13/cobra"
)

var methodCmd = &cobra.Command{
	Use:   "method",
	Short: "Open the context manager: inspect and edit your layered method",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMethod()
	},
}

var methodLinkClaudeMd string

var methodLinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Wire the global method into ~/.claude/CLAUDE.md",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := config.Dir()
		if err != nil {
			return err
		}
		if err := method.Scaffold(home); err != nil {
			return err
		}
		path := methodLinkClaudeMd
		if path == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			path = filepath.Join(h, ".claude", "CLAUDE.md")
		}
		files, err := linkGlobalMethod(path, home)
		if err != nil {
			return err
		}
		// Wiring the global layer also wires its global skills — symlinked into
		// the sibling skills dir of the same ~/.claude.
		skills, err := linkGlobalSkills(home, filepath.Join(filepath.Dir(path), "skills"))
		if err != nil {
			return err
		}
		if len(files) == 0 {
			fmt.Printf("Linked global method into %s — add *.md to %s/global to fill it.\n", path, method.Dir(home))
		} else {
			fmt.Printf("Linked %d global method file(s) into %s.\n", len(files), path)
		}
		if len(skills) > 0 {
			fmt.Printf("Linked %d global skill(s) into %s.\n", len(skills), filepath.Join(filepath.Dir(path), "skills"))
		}
		return nil
	},
}

// linkGlobalMethod upserts a kovan-managed block of @import lines for the global
// method into claudeMd, preserving the rest of the user's file. It backs the
// file up first and is idempotent.
func linkGlobalMethod(claudeMd, home string) ([]string, error) {
	files := method.Global(home)
	existing, err := os.ReadFile(claudeMd)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", claudeMd, err)
	}

	out := upsertManagedBlock(string(existing), globalMethodBlock(home, files))
	if existed {
		if err := os.WriteFile(claudeMd+".bak", existing, 0o644); err != nil {
			return nil, fmt.Errorf("back up %s: %w", claudeMd, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(claudeMd), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(claudeMd, []byte(out), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", claudeMd, err)
	}
	return files, nil
}

func globalMethodBlock(home string, files []string) string {
	if len(files) == 0 {
		return fmt.Sprintf("## kovan method\n\n_No global method yet — add *.md to %s/global._", method.Dir(home))
	}
	return "## kovan method\n\n" + importLines(files)
}

var methodShowCmd = &cobra.Command{
	Use:   "show [<id>]",
	Short: "List the effective method files for an agent, by layer",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		return runMethodShow(id)
	},
}

func runMethodShow(id string) error {
	home, err := config.Dir()
	if err != nil {
		return err
	}
	ctx, err := methodContext(id)
	if err != nil {
		return err
	}

	fmt.Printf("Method — account: %s · domain: %s · repo: %s\n", orNone(ctx.account), orNone(ctx.domain), ctx.repo)
	for _, layer := range effectiveMethod(home, ctx) {
		fmt.Printf("\n  %s:\n", layer.Name)
		if len(layer.Files) == 0 && len(layer.Skills) == 0 {
			fmt.Println("    (none)")
			continue
		}
		for _, f := range layer.Files {
			fmt.Println(methodFileLabel(f))
		}
		for _, s := range layer.Skills {
			fmt.Println(skillLabel(s))
		}
	}
	return nil
}

// methodCtx is the resolved context an agent's method composes against.
type methodCtx struct {
	account, domain, repo, repoRoot, taskDir, id, mode string
}

// methodContext resolves the layer-selecting context for a given agent id, or
// the current repo (with the default account) when id is empty.
func methodContext(id string) (methodCtx, error) {
	if id != "" {
		m, err := findSession(id)
		if err != nil {
			return methodCtx{}, err
		}
		rc, err := config.LoadRepo(m.RepoRoot)
		if err != nil {
			return methodCtx{}, err
		}
		return methodCtx{m.Account, rc.Domain, m.Repo, m.RepoRoot, rc.Task.Dir, m.ID, m.TaskMode}, nil
	}
	repo, err := openRepo()
	if err != nil {
		return methodCtx{}, err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return methodCtx{}, err
	}
	rc, err := config.LoadRepo(repo.Root)
	if err != nil {
		return methodCtx{}, err
	}
	account := resolveAccount("", rc.Account, global.DefaultAccount)
	return methodCtx{account, rc.Domain, filepath.Base(repo.Root), repo.Root, rc.Task.Dir, "", resolveMode("", rc.Mode, global.DefaultMode)}, nil
}

// effectiveMethod is the full ordered layer set kovan composes for an agent:
// global, the per-worktree layers, the repo's public method, and the task docs.
func effectiveMethod(home string, c methodCtx) []method.Layer {
	// Worktree layers come pre-expanded; the others are expanded here so every
	// layer shows the files its @imports transitively pull in.
	layers := []method.Layer{{
		Name:   "global",
		Files:  method.ResolveImports(method.Global(home)),
		Skills: method.SkillFiles(method.GlobalSkills(home)),
	}}
	layers = append(layers, method.Worktree(home, c.account, c.domain, c.repo)...)
	// The task mode contributes its working method (its method.md) as a layer, so
	// the inspector shows what a review/analyze/… agent is actually governed by.
	if path, err := mode.MethodFile(home, c.mode); err == nil && path != "" {
		layers = append(layers, method.Layer{Name: "mode (" + c.mode + ")", Files: method.ResolveImports([]string{path})})
	}
	layers = append(layers, method.Layer{Name: "project (public)", Files: method.ResolveImports(publicMethodFiles(c.repoRoot))})
	if c.id != "" {
		layers = append(layers, method.Layer{Name: "task", Files: method.ResolveImports(method.Files(taskDocsDir(home, c.repo, c.taskDir, c.id)))})
	}
	return layers
}

// publicMethodFiles are the repo's committed method files, loaded from cwd.
func publicMethodFiles(root string) []string {
	var out []string
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

var methodEditCmd = &cobra.Command{
	Use:   "edit [layer]",
	Short: "Open the method directory (or a layer) in $EDITOR",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := config.Dir()
		if err != nil {
			return err
		}
		if err := method.Scaffold(home); err != nil {
			return err
		}
		target := method.Dir(home)
		if len(args) > 0 {
			target = filepath.Join(target, args[0])
		}
		return runEditor(target)
	},
}

func init() {
	methodLinkCmd.Flags().StringVar(&methodLinkClaudeMd, "claude-md", "", "path to CLAUDE.md (default ~/.claude/CLAUDE.md)")
	methodCmd.AddCommand(methodLinkCmd, methodShowCmd, methodEditCmd)
}
