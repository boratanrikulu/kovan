package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
)

// The method reaches every agent CLI the same way: kovan composes it at each
// launch from the live layer files and hands it over as part of the system
// prompt. Nothing about it lives in the worktree, so an edit to a layer lands
// on the agent's next wake whatever CLI it runs.

// methodRunFile is where a tab's composed method lives between launches. It is
// rewritten on every launch and never edited by hand.
func methodRunFile(home, tmux string) string {
	return filepath.Join(home, "run", tmux+".method.md")
}

// writeMethod composes m's method and writes it for driver d, returning the
// file to load ("" when there is nothing to compose).
func writeMethod(m *session.Manifest, d agent.Driver) (string, error) {
	home, err := config.Dir()
	if err != nil {
		return "", err
	}
	text, err := composeMethod(home, m, d)
	if err != nil || text == "" {
		return "", err
	}
	path := methodRunFile(home, m.Tmux)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("write method: %w", err)
	}
	if err := os.WriteFile(path, d.EncodeMethod(text), 0o600); err != nil {
		return "", fmt.Errorf("write method: %w", err)
	}
	return path, nil
}

// composeMethod is the text an agent carries: its task block (unless it is a
// --quick task), then every method file that applies to it, in layer order,
// each under a heading naming its path.
func composeMethod(home string, m *session.Manifest, d agent.Driver) (string, error) {
	cfg, err := config.LoadRepo(home, m.Repo)
	if err != nil {
		return "", err
	}
	taskMode, err := mode.Load(home, resolveMode(m.TaskMode, cfg.Mode, ""))
	if err != nil {
		taskMode = nil // an unloadable mode still leaves the other layers
	}
	var parts []string
	if !m.Quick {
		notes := taskDocsDir(home, m.Repo, cfg.Task.Dir, m.ID)
		block := claudeLocalBlock(m.ID, m.Title, notes, taskMode)
		if h := filepath.Join(notes, handoffFile); fileExists(h) {
			block += "\n\nAnother agent worked on this task before you; its handoff note is " + h + "."
		}
		if section := workspacesSection(m.Worktree, m.Repo, m.Extra); section != "" {
			block += "\n\n" + section
		}
		parts = append(parts, block)
	}
	for _, f := range methodFiles(home, m, cfg.Domain, taskMode, d) {
		data, err := os.ReadFile(f)
		if err != nil {
			continue // a dangling @import contributes nothing
		}
		parts = append(parts, "## "+f+"\n\n"+strings.TrimSpace(string(data)))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "# kovan method\n\n" + strings.Join(parts, "\n\n") + "\n", nil
}

// methodFiles lists the method files for m, @imports expanded, each once:
// global, account, domain, the project layer of every repo it works in, then
// its mode's method. A file the CLI already loads on its own (Claude Code's
// ~/.claude/CLAUDE.md after `kovan method link`) is left out.
func methodFiles(home string, m *session.Manifest, domain string, taskMode *mode.Mode, d agent.Driver) []string {
	skip := map[string]bool{}
	if mem := d.GlobalMemory(d.DefaultHome()); mem != "" {
		for _, f := range method.ResolveImports([]string{mem}) {
			skip[filepath.Clean(f.Path)] = true
		}
	}
	var files []string
	add := func(fs []method.File) {
		for _, f := range fs {
			p := filepath.Clean(f.Path)
			if !skip[p] {
				skip[p] = true
				files = append(files, p)
			}
		}
	}
	add(method.ResolveImports(method.Global(home)))
	layers := method.Worktree(home, m.Account, domain, m.Repo)
	for _, w := range m.Extra {
		layers = append(layers, method.Worktree(home, "", "", w.Repo)...)
	}
	for _, l := range layers {
		add(l.Files)
	}
	if taskMode != nil {
		if p, err := mode.MethodFile(home, taskMode.Name); err == nil && p != "" {
			add(method.ResolveImports([]string{p}))
		}
	}
	return files
}

// stripClaudeLocalBlock removes m's kovan-managed block from the worktree's
// CLAUDE.local.md. Claude Code loads that file on its own, so a block there
// would hand the agent a second, stale copy of its method. A block another tab
// owns is left alone.
func stripClaudeLocalBlock(m *session.Manifest) {
	data, err := os.ReadFile(filepath.Join(m.Worktree, claudeLocalFile))
	if err != nil || !ownsClaudeLocal(string(data), m.ID) {
		return
	}
	if err := clearClaudeLocal(m.Worktree); err != nil {
		fmt.Fprintf(os.Stderr, "kovan: clean %s: %v\n", claudeLocalFile, err)
	}
}
