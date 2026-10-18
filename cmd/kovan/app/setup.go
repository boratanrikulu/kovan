package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/spf13/cobra"
)

// gateRunCommand is the hook command for driver d: the absolute path of the
// running kovan binary plus "gate run", naming the CLI whose payload the hook
// decodes (claude, the default, stays unnamed). The absolute path is required
// because hooks run under /bin/sh, where kovan is usually not on PATH.
func gateRunCommand(d agent.Driver) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cmd := quoteArg(exe) + " gate run"
	if d.Name() != agent.Claude {
		cmd += " --agent " + d.Name()
	}
	return cmd, nil
}

// quoteArg double-quotes a path only when it contains spaces, leaving the common
// space-free case readable in settings.json.
func quoteArg(s string) string {
	if strings.Contains(s, " ") {
		return `"` + s + `"`
	}
	return s
}

var setupSettings string

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install kovan's hooks into each agent CLI (run once per machine)",
	Long: `Install the global hooks that keep the board live: as an agent runs, its
state and mode are written to its manifest, and the gates check its tool calls.
Claude Code's hooks go into ~/.claude/settings.json; Codex's into hooks.json in
~/.codex and in every Codex account's home. Idempotent and safe: it backs up
each file and preserves everything else.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		global, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		targets := hookTargets(global)
		if setupSettings != "" {
			targets[0].path = setupSettings
		}
		for _, t := range targets {
			added, err := installHooks(t.path, t.d)
			if err != nil {
				return err
			}
			if len(added) == 0 {
				fmt.Printf("%s: kovan hooks already installed in %s.\n", t.d.Name(), t.path)
				continue
			}
			fmt.Printf("%s: wired kovan hooks into %s (%d events).\n", t.d.Name(), t.path, len(added))
			if t.d.Name() == agent.Codex {
				fmt.Println("  Codex runs a new hook only once you trust it: open codex with this home and approve kovan's hook in /hooks.")
			}
		}
		return nil
	},
}

func init() {
	setupCmd.Flags().StringVar(&setupSettings, "settings", "", "Claude Code settings.json path (default ~/.claude/settings.json)")
}

// hookTarget is one hooks file kovan installs into.
type hookTarget struct {
	d    agent.Driver
	path string
}

// hookTargets lists every hooks file kovan installs into: Claude Code's
// settings first, always; then each other CLI that is installed or has an
// account, in its default home and in every home an account gives it.
func hookTargets(global *config.Global) []hookTarget {
	claude := mustDriver(agent.Claude)
	targets := []hookTarget{{claude, claude.HooksFile(claude.DefaultHome())}}
	for _, name := range agent.Names() {
		d := mustDriver(name)
		if name == agent.Claude || d.HomeEnv() == "" {
			continue
		}
		homes := map[string]bool{}
		for _, acct := range global.Accounts {
			if acct.Agent == name && acct.Home != "" {
				homes[acct.Home] = true
			}
		}
		if len(homes) == 0 && !installed(global, name) {
			continue
		}
		homes[d.DefaultHome()] = true
		for _, h := range sortedKeys(homes) {
			targets = append(targets, hookTarget{d, d.HooksFile(h)})
		}
	}
	return targets
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// installHooks merges the kovan hook entries into the settings file, preserving
// every existing key and hook. It is idempotent, re-points an existing kovan
// hook whose binary path changed, and backs the file up before writing. It
// returns the events it added or re-pointed (empty when already correct).
func installHooks(path string, d agent.Driver) ([]string, error) {
	command, err := gateRunCommand(d)
	if err != nil {
		return nil, err
	}

	root := map[string]any{}
	existed := false
	if data, err := os.ReadFile(path); err == nil {
		existed = true
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	hooks, err := asObject(root["hooks"])
	if err != nil {
		return nil, fmt.Errorf("hooks: %w", err)
	}

	var changed []string
	for _, ev := range d.HookEvents() {
		groups, err := asArray(hooks[ev])
		if err != nil {
			return nil, fmt.Errorf("hooks.%s: %w", ev, err)
		}
		newGroups, didChange := ensureGateRun(groups, command)
		if didChange {
			hooks[ev] = newGroups
			changed = append(changed, ev)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	root["hooks"] = hooks

	if existed {
		if err := backup(path); err != nil {
			return nil, err
		}
	}
	if err := writeSettings(path, root); err != nil {
		return nil, err
	}
	return changed, nil
}

// asObject and asArray refuse to clobber an existing value of the wrong shape.
func asObject(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected an object, found %T", v)
	}
	return m, nil
}

func asArray(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}
	s, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected an array, found %T", v)
	}
	return s, nil
}

// isGateRun reports whether cmd is a kovan gate-run hook, matched on the
// subcommand rather than the whole line so a re-pointed binary path is
// recognized as the same hook rather than a new one.
func isGateRun(cmd string) bool {
	return strings.HasSuffix(cmd, " gate run") || strings.Contains(cmd, " gate run --agent ")
}

// ensureGateRun guarantees groups holds the desired kovan hook: it re-points an
// existing one whose command differs (the binary moved or was reinstalled) and
// appends a new group only when none is present. changed is false when the hook
// was already correct.
func ensureGateRun(groups []any, command string) (out []any, changed bool) {
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		hs, ok := gm["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range hs {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); isGateRun(cmd) {
				if cmd == command {
					return groups, false
				}
				hm["command"] = command
				return groups, true
			}
		}
	}
	return append(groups, gateGroup(command)), true
}

func gateGroup(command string) map[string]any {
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	}
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", data, 0o644)
}

func writeSettings(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
