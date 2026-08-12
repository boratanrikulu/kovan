// Package mode loads task modes: the per-task working style selected at start —
// the opening prompt, the posture (whether it edits the repo), and the output
// docs it scaffolds. Built-in modes are embedded; a directory of the same name
// under ~/.kovan/modes/ layers over one file by file, so a mode is tuned or
// added with no recompile and retuning one part leaves the others alone.
package mode

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin
var builtin embed.FS

// Default is the mode used when neither the repo nor the command selects one.
const Default = "code"

// Mode is a task's working style.
type Mode struct {
	Name       string
	Prompt     string   // opening-prompt template; {{brief}} and {{artifact}} are filled at launch
	Posture    string   // edit | read-only
	Docs       []string // task-docs scaffolded beyond context.md + learnings.md
	WritePaths []string // worktree-relative prefixes the mode may write to; empty means posture alone governs
}

// MethodFile returns the on-disk path of the mode's working-method file
// (~/.kovan/modes/<name>/method.md), or "" when the mode ships no method. A
// built-in's embedded method.md is materialized to that path on first use
// (no-clobber), so it becomes a live, editable file that every agent @imports —
// edits to it propagate to existing agents on their next session, like every
// other method layer. An empty home, or a mode with no method anywhere, yields "".
func MethodFile(home, name string) (string, error) {
	if home == "" || name == "" {
		return "", nil
	}
	path := filepath.Join(home, "modes", name, "method.md")
	if _, err := os.Stat(path); err == nil {
		return path, nil // user/scaffolded file already on disk
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat mode method %s: %w", path, err)
	}
	embedded, err := builtin.ReadFile("builtin/" + name + "/method.md")
	if err != nil {
		return "", nil // no on-disk and no embedded method: the mode ships none
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create mode dir: %w", err)
	}
	if err := os.WriteFile(path, embedded, 0o644); err != nil {
		return "", fmt.Errorf("materialize mode method %s: %w", path, err)
	}
	return path, nil
}

// Artifact is the mode's primary output doc (the first it scaffolds), or "" when
// the mode scaffolds none.
func (m *Mode) Artifact() string {
	if len(m.Docs) == 0 {
		return ""
	}
	return m.Docs[0]
}

// ReadOnly reports whether the mode must not modify the repo's code.
func (m *Mode) ReadOnly() bool { return m.Posture == "read-only" }

type config struct {
	Posture    string   `yaml:"posture"`
	Docs       []string `yaml:"docs"`
	WritePaths []string `yaml:"write_paths"`
}

// Load returns the named mode: the built-in of that name with whatever the user
// dropped in ~/.kovan/modes/<name>/ layered on top. The overlay is per file, and
// inside mode.yaml per field, so retuning one part of a shipped mode never
// silently resets the others: a new prompt.md on review keeps it read-only. An
// empty name resolves to Default.
func Load(home, name string) (*Mode, error) {
	if name == "" {
		name = Default
	}
	prompt, cfg, isBuiltin, err := readEmbed(name)
	if err != nil {
		return nil, err
	}
	userPrompt, userCfg, has, err := readDir(filepath.Join(home, "modes", name))
	if err != nil {
		return nil, err
	}
	if !isBuiltin && !has.prompt {
		return nil, fmt.Errorf("unknown mode %q (see ~/.kovan/modes or the built-ins)", name)
	}
	if has.prompt {
		prompt = userPrompt
	}
	if has.config {
		cfg = overlay(cfg, userCfg)
	}
	return assemble(name, prompt, cfg), nil
}

// overlay applies the fields a user's mode.yaml actually sets on top of the
// built-in's. A missing list keeps the built-in's; an explicit empty list
// (`docs: []`) means none.
func overlay(base, over config) config {
	if over.Posture != "" {
		base.Posture = over.Posture
	}
	if over.Docs != nil {
		base.Docs = over.Docs
	}
	if over.WritePaths != nil {
		base.WritePaths = over.WritePaths
	}
	return base
}

// List returns the available mode names: the built-ins plus any user modes under
// ~/.kovan/modes/, deduped and sorted, Default first.
func List(home string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	if entries, err := builtin.ReadDir("builtin"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				add(e.Name())
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(home, "modes")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				add(e.Name())
			}
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == Default {
			return true
		}
		if names[j] == Default {
			return false
		}
		return names[i] < names[j]
	})
	return names
}

func assemble(name, prompt string, cfg config) *Mode {
	posture := cfg.Posture
	if posture == "" {
		posture = "edit"
	}
	return &Mode{Name: name, Prompt: strings.TrimSpace(prompt), Posture: posture, Docs: cfg.Docs, WritePaths: cfg.WritePaths}
}

// present records which files a user's mode dir actually carries, so Load
// overlays those and leaves the rest to the built-in.
type present struct{ prompt, config bool }

// readDir reads what a user mode directory holds. Both files are optional and a
// missing directory is not an error: the caller decides whether the built-in
// alone is enough.
func readDir(dir string) (prompt string, cfg config, has present, err error) {
	p, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
	switch {
	case err == nil:
		prompt, has.prompt = string(p), true
	case !os.IsNotExist(err):
		return "", config{}, present{}, fmt.Errorf("read mode prompt %s: %w", dir, err)
	}
	cfg, found, err := readConfig(os.ReadFile, filepath.Join(dir, "mode.yaml"))
	if err != nil {
		return "", config{}, present{}, err
	}
	has.config = found
	return prompt, cfg, has, nil
}

func readEmbed(name string) (prompt string, cfg config, ok bool, err error) {
	p, err := builtin.ReadFile("builtin/" + name + "/prompt.md")
	if err != nil {
		return "", config{}, false, nil
	}
	cfg, _, err = readConfig(builtin.ReadFile, "builtin/"+name+"/mode.yaml")
	if err != nil {
		return "", config{}, false, err
	}
	return string(p), cfg, true, nil
}

// readConfig decodes a mode.yaml via the given reader; found is false when the
// file is absent, which is not an error (mode.yaml is optional).
// errors.Is(fs.ErrNotExist) covers both the os and embed.FS "not found" cases.
func readConfig(read func(string) ([]byte, error), path string) (cfg config, found bool, err error) {
	data, err := read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return config{}, false, nil
		}
		return config{}, false, fmt.Errorf("read mode config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, false, fmt.Errorf("parse mode config %s: %w", path, err)
	}
	return cfg, true, nil
}
