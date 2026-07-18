package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check your config files against what this binary understands",
	Long: `Compares ~/.kovan/config.yaml (and, inside a repo, .kovan.yaml) with the
current binary: keys it no longer reads, keys added since the file was
written, and values it would reject or silently ignore. Report only; the
files are never modified. Exits 1 when something needs attention.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		clean, err := runDoctor(cmd.OutOrStdout())
		if err != nil {
			return err
		}
		if !clean {
			os.Exit(1)
		}
		return nil
	},
}

func runDoctor(w io.Writer) (clean bool, err error) {
	home, err := config.Dir()
	if err != nil {
		return false, err
	}
	global, globalClean := doctorGlobal(w, home)
	repoClean := doctorRepo(w, home, global)
	return globalClean && repoClean, nil
}

// doctorGlobal reports on ~/.kovan/config.yaml and returns the loaded config
// for the repo checks that reference it (nil when it cannot be loaded).
func doctorGlobal(w io.Writer, home string) (*config.Global, bool) {
	path := filepath.Join(home, "config.yaml")
	rep, data := checkFile(path, config.CheckGlobal)
	var global *config.Global
	if data != nil && rep.ParseErr == "" {
		if g, err := config.LoadGlobal(); err == nil {
			global = g
			rep.Values = append(rep.Values, globalValueFindings(g, home)...)
		}
	}
	printReport(w, path, rep)
	return global, reportClean(rep)
}

func doctorRepo(w io.Writer, home string, global *config.Global) bool {
	repo, err := openRepo()
	if err != nil {
		return true // not inside a repo: nothing to check
	}
	path := filepath.Join(repo.Root, ".kovan.yaml")
	rep, data := checkFile(path, config.CheckRepo)
	if data != nil && rep.ParseErr == "" {
		if r, err := config.LoadRepo(repo.Root); err == nil {
			rep.Values = append(rep.Values, repoValueFindings(r, global, home)...)
		}
	}
	fmt.Fprintln(w)
	printReport(w, path, rep)
	return reportClean(rep)
}

func checkFile(path string, check func([]byte) *config.Report) (*config.Report, []byte) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &config.Report{Missing: true}, nil
	}
	if err != nil {
		return &config.Report{ParseErr: err.Error()}, nil
	}
	return check(data), data
}

// reportClean is the exit-code rule: problems (unparseable file, dead active
// keys, bad values) fail; staleness (stale comments, new keys) only informs.
func reportClean(rep *config.Report) bool {
	return rep.ParseErr == "" && len(rep.Dead) == 0 && len(rep.Values) == 0
}

// globalValueFindings flags values the code would reject or silently ignore,
// mirroring the exact comparisons the consuming code makes.
func globalValueFindings(g *config.Global, home string) []config.Finding {
	var out []config.Finding
	add := func(path, note string) { out = append(out, config.Finding{Path: path, Note: note}) }
	if g.Runner != "tmux" {
		add(fmt.Sprintf("runner: %q", g.Runner), "unknown runner; only tmux is supported, agents will not start")
	}
	for _, gate := range []struct{ path, action string }{
		{"gates.push", g.Gates.Push},
		{"gates.read_only", g.Gates.ReadOnly},
		{"gates.write_paths", g.Gates.WritePaths},
		{"gates.default_branch.action", g.Gates.DefaultBranch.Action},
	} {
		if gate.action != "ask" && gate.action != "off" {
			add(fmt.Sprintf("%s: %q", gate.path, gate.action), `not "ask" or "off"; this silently disables the gate`)
		}
	}
	for i, p := range g.Gates.Patterns {
		if _, err := regexp.Compile(p.Match); err != nil {
			add(fmt.Sprintf("gates.patterns[%d].match: %q", i, p.Match), "invalid regexp; this pattern is silently skipped")
		}
		switch p.Action {
		case "", "ask", "deny", "off":
		default:
			add(fmt.Sprintf("gates.patterns[%d].action: %q", i, p.Action), `not "ask", "deny" or "off"; sent verbatim to Claude, which rejects it`)
		}
	}
	for name, p := range g.Projects {
		if p.Color == "" {
			continue
		}
		if _, ok := rowTints[p.Color]; !ok {
			add(fmt.Sprintf("projects.%s.color: %q", name, p.Color), "not a palette color (red/orange/yellow/green/cyan/blue/magenta/grey); silently ignored")
		}
	}
	if g.DefaultMode != "" {
		if _, err := mode.Load(home, g.DefaultMode); err != nil {
			add(fmt.Sprintf("default_mode: %q", g.DefaultMode), err.Error())
		}
	}
	if g.DefaultAccount != "" {
		if _, ok := g.Accounts[g.DefaultAccount]; !ok {
			add(fmt.Sprintf("default_account: %q", g.DefaultAccount), "not configured under accounts")
		}
	}
	for name := range g.Accounts {
		if _, err := accountTokenFile(g, name); err != nil {
			add("accounts."+name+".token_file", err.Error())
		}
	}
	return out
}

func repoValueFindings(r *config.Repo, global *config.Global, home string) []config.Finding {
	var out []config.Finding
	add := func(path, note string) { out = append(out, config.Finding{Path: path, Note: note}) }
	if r.Worktree.IDPattern != "" {
		if _, err := regexp.Compile(r.Worktree.IDPattern); err != nil {
			add(fmt.Sprintf("worktree.id_pattern: %q", r.Worktree.IDPattern), "invalid regexp; kovan start will refuse every typed id")
		}
	}
	if r.Mode != "" {
		if _, err := mode.Load(home, r.Mode); err != nil {
			add(fmt.Sprintf("mode: %q", r.Mode), err.Error())
		}
	}
	if r.Domain != "" {
		if _, err := os.Stat(filepath.Join(home, "method", "domains", r.Domain)); err != nil {
			add(fmt.Sprintf("domain: %q", r.Domain), "no ~/.kovan/method/domains/"+r.Domain+"; the domain layer silently contributes nothing")
		}
	}
	if r.Account != "" && global != nil {
		if _, ok := global.Accounts[r.Account]; !ok {
			add(fmt.Sprintf("account: %q", r.Account), "not configured in ~/.kovan/config.yaml accounts")
		}
	}
	return out
}

func printReport(w io.Writer, header string, rep *config.Report) {
	fmt.Fprintln(w, header)
	if rep.Missing {
		fmt.Fprintln(w, "  not present, defaults apply")
		return
	}
	if rep.ParseErr != "" {
		fmt.Fprintln(w, "  cannot parse:", rep.ParseErr)
		return
	}
	empty := true
	for _, sec := range []struct {
		title    string
		findings []config.Finding
	}{
		{"no longer read", rep.Dead},
		{"stale comments", rep.Stale},
		{"new since your config was written", rep.New},
		{"check values", rep.Values},
	} {
		if len(sec.findings) == 0 {
			continue
		}
		empty = false
		fmt.Fprintln(w, "  "+sec.title)
		for _, f := range sec.findings {
			if f.Note == "" {
				fmt.Fprintln(w, "    "+f.Path)
				continue
			}
			fmt.Fprintf(w, "    %-36s%s\n", f.Path, f.Note)
		}
	}
	if empty {
		fmt.Fprintln(w, "  ok — matches the current schema")
	}
}
