package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// gateHookEvents are the Claude Code events kovan observes to keep the board live.
var gateHookEvents = []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Notification", "Stop"}

// gateRunCommand is the hook command: the absolute path of the running kovan
// binary plus "gate run". The absolute path is required because hooks run under
// /bin/sh, where kovan is usually not on PATH.
func gateRunCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return quoteArg(exe) + " gate run", nil
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
	Short: "Install kovan's Claude Code hooks (run once per machine)",
	Long: `Install the global hooks that keep the board live: as Claude Code runs, each
agent's state and mode are written to its manifest. Idempotent and safe — it
backs up settings.json and preserves everything else.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := setupSettings
		if path == "" {
			p, err := settingsPath()
			if err != nil {
				return err
			}
			path = p
		}
		added, err := installHooks(path)
		if err != nil {
			return err
		}
		if len(added) == 0 {
			fmt.Println("kovan hooks already installed; nothing to add.")
			return nil
		}
		fmt.Printf("Wired kovan hooks into %s (%d events).\n", path, len(added))
		fmt.Println("hooks installed — spawn a fresh agent (kovan start …) to see live mode/state on the board.")
		return nil
	},
}

func init() {
	setupCmd.Flags().StringVar(&setupSettings, "settings", "", "settings.json path (default ~/.claude/settings.json)")
}

// settingsPath is the default Claude Code settings file, ~/.claude/settings.json.
func settingsPath() (string, error) {
	claude, err := claudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(claude, "settings.json"), nil
}

// installHooks merges the kovan hook entries into the settings file, preserving
// every existing key and hook. It is idempotent, re-points an existing kovan
// hook whose binary path changed, and backs the file up before writing. It
// returns the events it added or re-pointed (empty when already correct).
func installHooks(path string) ([]string, error) {
	command, err := gateRunCommand()
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
	for _, ev := range gateHookEvents {
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

// isGateRun reports whether cmd is a kovan gate-run hook, matched by suffix so a
// re-pointed binary path is recognized as the same hook rather than a new one.
func isGateRun(cmd string) bool {
	return strings.HasSuffix(cmd, "gate run")
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
