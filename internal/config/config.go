// Package config loads kovan's two configuration layers, both under ~/.kovan:
// the global config.yaml and the per-repository projects/<repo>/config.yaml.
// Both are optional; every field falls back to a sane default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Global is kovan's own settings, read from ~/.kovan/config.yaml.
type Global struct {
	Runner         string             `yaml:"runner"`
	Agent          string             `yaml:"agent"` // the default agent CLI: claude | codex
	Agents         map[string]Agent   `yaml:"agents"`
	Notify         string             `yaml:"notify"`
	Author         string             `yaml:"author"`
	Tmux           Tmux               `yaml:"tmux"`
	Gates          Gates              `yaml:"gates"`
	Apps           Apps               `yaml:"apps"`
	Monitor        Monitor            `yaml:"monitor"`
	Accounts       map[string]Account `yaml:"accounts"`
	DefaultAccount string             `yaml:"default_account"`
	DefaultMode    string             `yaml:"default_mode"` // task mode when neither repo nor command sets one
}

// Agent overrides how one agent CLI is launched.
type Agent struct {
	Command string `yaml:"command"` // the binary; default: the agent's name on PATH
}

// Monitor configures the cockpit's agent summaries: the model each agent's
// one-shot summarizer uses, under the agent's own account.
type Monitor struct {
	Model  string            `yaml:"model"`  // claude's summarizer model: opus | sonnet | haiku
	Models map[string]string `yaml:"models"` // per agent CLI, e.g. codex: gpt-5.5; wins over model
}

// Apps are the external programs the board opens against a selected agent's
// worktree root. Each value is a command; if it contains "{path}" the worktree
// path is substituted there, otherwise it is appended as the last argument.
type Apps struct {
	Editor   string `yaml:"editor"`   // board key e
	Merge    string `yaml:"merge"`    // board key s
	Terminal string `yaml:"terminal"` // board key t; empty → a new iTerm2 tab on macOS
}

// Account is an account an agent can run under. A Claude account's OAuth token
// lives in a file (referenced, never inlined), read at launch; a Codex account
// is its own CODEX_HOME, logged in once.
type Account struct {
	Agent     string            `yaml:"agent"` // the agent CLI it belongs to; default claude
	TokenFile string            `yaml:"token_file"`
	Home      string            `yaml:"home"` // the CLI's home dir for this account (codex: CODEX_HOME)
	Env       map[string]string `yaml:"env"`
}

// Tmux holds tmux options kovan applies to every agent session it spawns, so
// the stage is set consistently without touching the user's ~/.tmux.conf.
type Tmux struct {
	Options []string `yaml:"options"`
}

// Gates configures the built-in enforcement gates evaluated on PreToolUse. A
// gate is active when its action is "ask"; "off" disables it.
type Gates struct {
	Push          string        `yaml:"push"`        // ask | off
	ReadOnly      string        `yaml:"read_only"`   // ask | off — for read-only modes, confirm edits to the repo
	WritePaths    string        `yaml:"write_paths"` // ask | off — for path-scoped modes, confirm edits outside their write paths
	DefaultBranch DefaultBranch `yaml:"default_branch"`
	Patterns      []Pattern     `yaml:"patterns"` // user-defined command gates, same engine as the built-ins
}

// DefaultBranch gates git commit on a protected branch (main/master), where a
// commit usually belongs on a feature branch instead.
type DefaultBranch struct {
	Action   string   `yaml:"action"`   // ask | off
	Branches []string `yaml:"branches"` // protected branch names; default main, master
}

// Pattern is a user-defined command gate: a regexp matched against each command
// segment. It runs through the same engine as the built-in git/gh gates.
type Pattern struct {
	Match  string `yaml:"match"`  // regexp, matched against a command segment
	Action string `yaml:"action"` // ask | deny
	Reason string `yaml:"reason"` // shown to the user on a match
}

// Repo is a repository's kovan settings, read from
// ~/.kovan/projects/<repo>/config.yaml — the repo's private home. Nothing
// lives in the repository itself, so every worktree and every checkout sees
// the same settings.
type Repo struct {
	Worktree   Worktree `yaml:"worktree"`
	Task       Task     `yaml:"task"`
	Account    string   `yaml:"account"`     // default account for agents in this repo
	Agent      string   `yaml:"agent"`       // default agent CLI for this repo
	Domain     string   `yaml:"domain"`      // method domain (code/writing/…) for this repo
	Mode       string   `yaml:"mode"`        // default task mode (code/review/…) for this repo
	Color      string   `yaml:"color"`       // default board stripe color, a palette name
	WritePaths []string `yaml:"write_paths"` // extra allowed write prefixes for scoped modes in this repo
}

// Worktree controls how worktrees and branches are named.
type Worktree struct {
	Prefix         string `yaml:"prefix"`
	Base           string `yaml:"base"`
	BranchTemplate string `yaml:"branch_template"`
	IDPattern      string `yaml:"id_pattern"`
}

// Task controls where per-agent task docs live.
type Task struct {
	Dir   string `yaml:"dir"`
	Token string `yaml:"token"`
}

// Dir is kovan's home directory, ~/.kovan, overridable via KOVAN_HOME.
func Dir() (string, error) {
	if d := os.Getenv("KOVAN_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".kovan"), nil
}

// LoadGlobal reads ~/.kovan/config.yaml, applying defaults for anything unset.
// A missing file yields the full set of defaults.
func LoadGlobal() (*Global, error) {
	g := &Global{}
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := readYAML(filepath.Join(dir, "config.yaml"), g); err != nil {
		return nil, err
	}
	if g.Runner == "" {
		g.Runner = "tmux"
	}
	if g.Agent == "" {
		g.Agent = "claude"
	}
	if g.Notify == "" {
		g.Notify = "macos"
	}
	if g.Gates.Push == "" {
		g.Gates.Push = "ask"
	}
	if g.Gates.ReadOnly == "" {
		g.Gates.ReadOnly = "ask"
	}
	if g.Gates.WritePaths == "" {
		g.Gates.WritePaths = "ask"
	}
	if g.Gates.DefaultBranch.Action == "" {
		g.Gates.DefaultBranch.Action = "ask"
	}
	if g.Gates.DefaultBranch.Branches == nil {
		g.Gates.DefaultBranch.Branches = []string{"main", "master"}
	}
	// nil means the key was absent → apply kovan's defaults; an empty non-nil
	// slice (`options: []`) is the user explicitly opting out, so it is honored.
	if g.Tmux.Options == nil {
		g.Tmux.Options = []string{"mouse on", "history-limit 50000"}
	}
	if g.Apps.Editor == "" {
		g.Apps.Editor = "code"
	}
	if g.Apps.Merge == "" {
		g.Apps.Merge = "smerge"
	}
	if g.Monitor.Model == "" {
		g.Monitor.Model = "opus"
	}
	for name, acct := range g.Accounts {
		acct.TokenFile = expandTilde(acct.TokenFile)
		acct.Home = expandTilde(acct.Home)
		if acct.Agent == "" {
			acct.Agent = "claude"
		}
		g.Accounts[name] = acct
	}
	for name, a := range g.Agents {
		a.Command = expandTilde(a.Command)
		g.Agents[name] = a
	}
	return g, nil
}

// expandTilde resolves a leading ~ to the user's home directory.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// RepoConfigPath is where a repository's kovan settings live, keyed by the
// repo name (the checkout's basename).
func RepoConfigPath(home, repo string) string {
	return filepath.Join(home, "projects", repo, "config.yaml")
}

// LoadRepo reads ~/.kovan/projects/<repo>/config.yaml, applying defaults for
// anything unset. Worktree.Base is left empty when unset so the caller can
// autodetect it.
func LoadRepo(home, repo string) (*Repo, error) {
	r := &Repo{}
	if err := readYAML(RepoConfigPath(home, repo), r); err != nil {
		return nil, err
	}
	if r.Worktree.Prefix == "" {
		r.Worktree.Prefix = repo
	}
	if r.Worktree.BranchTemplate == "" {
		r.Worktree.BranchTemplate = "feat/{author}_{id}_{slug}"
	}
	if r.Task.Dir == "" {
		r.Task.Dir = "works"
	}
	return r, nil
}

// globalTemplate is a fully-commented ~/.kovan/config.yaml: documentation only,
// no active values. LoadGlobal owns the defaults, so a live value here would
// silently drift when a default changes — every line stays commented, and a
// freshly-scaffolded file must LoadGlobal to exactly the code defaults.
const globalTemplate = `# kovan's own settings. Every line below is commented: the values shown are the
# built-in defaults, so uncomment only what you want to override.

# runner: tmux            # how agents are run (tmux)
# agent: claude           # the default agent CLI: claude | codex
# agents:                 # per-CLI launch overrides
#   codex:
#     command: codex      # the binary; default: the name on PATH
# notify: macos           # desktop notifications (macos)
# author: ""              # branch author; falls back to git config user

# tmux:                   # kovan applies "mouse on" + "history-limit 50000" by default;
#   options:              # setting tmux.options REPLACES those (your ~/.tmux.conf is untouched).
#     - mouse on
#     - history-limit 50000

# gates:                  # built-in enforcement gates (each agent CLI's pre-tool hook)
#   push: ask             # ask | off — confirm before git push / gh pr create
#   read_only: ask        # ask | off — in read-only modes, confirm edits to the repo
#   write_paths: ask      # ask | off — in path-scoped modes, confirm edits outside their write paths
#   default_branch:
#     action: ask         # ask | off — confirm git commit on a protected branch
#     branches: [main, master]
#   patterns:             # extra command gates (regexp), same engine as the built-ins
#     - match: 'terraform\s+apply'
#       action: ask       # ask | deny
#       reason: "kovan: confirm terraform apply"

# apps:                   # programs the board opens on a selected agent's worktree
#   editor: code          # board key e — "{path}" is substituted, else appended
#   merge: smerge         # board key s
#   terminal: ""          # board key t — empty opens a new iTerm2 tab on macOS

# monitor:                # the cockpit's per-agent summaries (board strip + S page)
#   model: opus           # model the claude summarizer uses (opus/sonnet/haiku)
#   models:               # per agent CLI; wins over model
#     codex: gpt-5.5

# accounts:               # accounts an agent can run under
#   personal:
#     token_file: ~/.kovan/tokens/personal   # claude setup-token, then chmod 600
#     env:                                   # extra env for this account's agents
#       ANTHROPIC_DEFAULT_OPUS_MODEL: claude-opus-5[1m]
#   openai:
#     agent: codex                           # a Codex account: its own CODEX_HOME,
#     home: ~/.kovan/codex/openai            # logged in once with CODEX_HOME=... codex login
# default_account: personal

# default_mode: code      # task mode when neither the repo nor the command sets one
`

// repoTemplate is a fully-commented ~/.kovan/projects/<repo>/config.yaml
// starter: documentation only. Same rule as globalTemplate — LoadRepo owns the
// defaults.
const repoTemplate = `# This repository's kovan settings. Every line is commented; omit the file
# entirely and these defaults apply. The repository itself stays clean: this
# file lives in kovan's home, so every checkout and worktree sees it.

# worktree:
#   prefix: agent                       # worktree dir = <prefix>-<id>; default: repo basename
#   base: master                        # branch base; default: auto-detected origin/HEAD
#   branch_template: "feat/{author}_{id}_{slug}"
#   id_pattern: "^TASK-[0-9]+$"           # optional: validate the id

# task:
#   dir: works                          # task-doc folder under ~/.kovan/projects/<repo>/; default: works
#   token: TASK-XXXXX                     # optional: template token substituted with the id

# account: company                      # default account for agents in this repo
# agent: claude                         # default agent CLI (claude/codex) for this repo
# domain: code                          # method domain layer (code/writing/…) to compose
# mode: code                            # default task mode (code/review/analyze/write) for this repo
# color: cyan                           # default board stripe color for this repo's agents
#                                       # (red/orange/yellow/green/cyan/blue/magenta/grey)

# write_paths:                          # extra allowed write prefixes (worktree-relative) for this
#   - Daily/                            # repo's scoped modes; adds to the mode's own write_paths,
#                                       # never restricts an unscoped edit mode
`

// ScaffoldGlobal writes a commented ~/.kovan/config.yaml template if absent. It
// never clobbers an existing file.
func ScaffoldGlobal(home string) error {
	return scaffoldFile(filepath.Join(home, "config.yaml"), globalTemplate)
}

// ScaffoldRepo writes a commented ~/.kovan/projects/<repo>/config.yaml starter
// if absent. It never clobbers an existing file.
func ScaffoldRepo(home, repo string) error {
	return scaffoldFile(RepoConfigPath(home, repo), repoTemplate)
}

// scaffoldFile writes content to path only when path does not already exist.
func scaffoldFile(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readYAML decodes path into v. A missing file is not an error.
func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
