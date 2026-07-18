package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadGlobalDefaults(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.Runner != "tmux" || g.Agent != "claude" || g.Notify != "macos" {
		t.Errorf("defaults wrong: %+v", g)
	}
	if g.Author != "" {
		t.Errorf("author should default empty, got %q", g.Author)
	}
	if g.Apps.Editor != "code" || g.Apps.Merge != "smerge" {
		t.Errorf("apps defaults wrong: %+v", g.Apps)
	}
}

func TestLoadGlobalOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write(t, filepath.Join(home, "config.yaml"), "runner: tmux\nagent: codex\nauthor: bora\n")

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.Agent != "codex" {
		t.Errorf("agent = %q, want codex", g.Agent)
	}
	if g.Author != "bora" {
		t.Errorf("author = %q, want bora", g.Author)
	}
	if g.Notify != "macos" {
		t.Errorf("unset notify should still default, got %q", g.Notify)
	}
}

func TestLoadGlobalTmuxOptions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write(t, filepath.Join(home, "config.yaml"),
		"tmux:\n  options:\n    - mouse on\n    - history-limit 50000\n")

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mouse on", "history-limit 50000"}
	if len(g.Tmux.Options) != len(want) {
		t.Fatalf("options = %v, want %v", g.Tmux.Options, want)
	}
	for i, w := range want {
		if g.Tmux.Options[i] != w {
			t.Errorf("option %d = %q, want %q", i, g.Tmux.Options[i], w)
		}
	}
}

func TestLoadGlobalTmuxDefaults(t *testing.T) {
	// No config (absent key) → kovan's defaults.
	t.Setenv("KOVAN_HOME", t.TempDir())
	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mouse on", "history-limit 50000"}
	if len(g.Tmux.Options) != len(want) || g.Tmux.Options[0] != want[0] || g.Tmux.Options[1] != want[1] {
		t.Errorf("default options = %v, want %v", g.Tmux.Options, want)
	}

	// `options: []` is an explicit opt-out (non-nil empty) → stays empty.
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write(t, filepath.Join(home, "config.yaml"), "tmux:\n  options: []\n")
	if g, err = LoadGlobal(); err != nil {
		t.Fatal(err)
	}
	if len(g.Tmux.Options) != 0 {
		t.Errorf("explicit `options: []` should opt out, got %v", g.Tmux.Options)
	}

	// A user list is taken verbatim, not merged with defaults.
	home = t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write(t, filepath.Join(home, "config.yaml"), "tmux:\n  options:\n    - set -g foo bar\n")
	if g, err = LoadGlobal(); err != nil {
		t.Fatal(err)
	}
	if len(g.Tmux.Options) != 1 || g.Tmux.Options[0] != "set -g foo bar" {
		t.Errorf("user options should be verbatim, got %v", g.Tmux.Options)
	}
}

func TestLoadGlobalGatesDefaults(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.Gates.Push != "ask" {
		t.Errorf("push = %q, want ask", g.Gates.Push)
	}
	if g.Gates.DefaultBranch.Action != "ask" {
		t.Errorf("default_branch.action = %q, want ask", g.Gates.DefaultBranch.Action)
	}
}

func TestLoadGlobalGatesOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	write(t, filepath.Join(home, "config.yaml"),
		"gates:\n  push: off\n  default_branch:\n    action: off\n")

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.Gates.Push != "off" {
		t.Errorf("push = %q, want off (not overridden by default)", g.Gates.Push)
	}
	if g.Gates.DefaultBranch.Action != "off" {
		t.Errorf("default_branch.action = %q, want off", g.Gates.DefaultBranch.Action)
	}
	if len(g.Gates.DefaultBranch.Branches) == 0 {
		t.Error("unset branches should still default")
	}
}

func TestLoadGlobalAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, "config.yaml"),
		"accounts:\n  personal: {token_file: ~/.kovan/tokens/personal}\n  company: {token_file: /etc/company}\ndefault_account: personal\n")

	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.DefaultAccount != "personal" {
		t.Errorf("default_account = %q, want personal", g.DefaultAccount)
	}
	wantPersonal := filepath.Join(home, ".kovan/tokens/personal")
	if g.Accounts["personal"].TokenFile != wantPersonal {
		t.Errorf("personal token_file = %q, want %q (~ expanded)", g.Accounts["personal"].TokenFile, wantPersonal)
	}
	if g.Accounts["company"].TokenFile != "/etc/company" {
		t.Errorf("company token_file = %q, want /etc/company (absolute unchanged)", g.Accounts["company"].TokenFile)
	}
}

func TestLoadGlobalNoAccounts(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	g, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if g.Accounts != nil || g.DefaultAccount != "" {
		t.Errorf("no accounts configured should stay empty, got %v / %q", g.Accounts, g.DefaultAccount)
	}
}

func TestLoadRepoDefaults(t *testing.T) {
	root := t.TempDir()

	r, err := LoadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Base(root); r.Worktree.Prefix != want {
		t.Errorf("prefix = %q, want repo basename %q", r.Worktree.Prefix, want)
	}
	if r.Worktree.BranchTemplate != "feat/{author}_{id}_{slug}" {
		t.Errorf("branch_template = %q", r.Worktree.BranchTemplate)
	}
	if r.Worktree.Base != "" {
		t.Errorf("base should default empty for autodetect, got %q", r.Worktree.Base)
	}
	if r.Task.Dir != "works" {
		t.Errorf("task.dir = %q, want works", r.Task.Dir)
	}
}

func TestLoadRepoOverrides(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".kovan.yaml"),
		"worktree:\n  prefix: agent\n  base: develop\ntask:\n  dir: tickets\naccount: company\ndomain: code\n")

	r, err := LoadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Worktree.Prefix != "agent" {
		t.Errorf("prefix = %q, want agent", r.Worktree.Prefix)
	}
	if r.Worktree.Base != "develop" {
		t.Errorf("base = %q, want develop", r.Worktree.Base)
	}
	if r.Task.Dir != "tickets" {
		t.Errorf("task.dir = %q, want tickets", r.Task.Dir)
	}
	if r.Account != "company" || r.Domain != "code" {
		t.Errorf("account/domain = %q/%q, want company/code", r.Account, r.Domain)
	}
}

func TestScaffoldGlobalDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := ScaffoldGlobal(home); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scaffold did not write config.yaml: %v", err)
	}

	// The template is documentation only: a freshly-scaffolded file must load to
	// exactly the code defaults, so live values can never drift from them.
	got, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOVAN_HOME", t.TempDir()) // a home with no file at all
	want, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scaffolded config did not load to the defaults:\n got %+v\nwant %+v", got, want)
	}
}

func TestScaffoldGlobalNeverClobbers(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	write(t, path, "agent: codex\n")
	if err := ScaffoldGlobal(home); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "agent: codex\n" {
		t.Errorf("scaffold overwrote an existing file: %q", got)
	}
}

func TestScaffoldRepo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".kovan.yaml")

	if err := ScaffoldRepo(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scaffold did not write .kovan.yaml: %v", err)
	}
	// An empty starter must still load to the repo defaults (all values commented).
	r, err := LoadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Task.Dir != "works" || r.Worktree.Prefix != filepath.Base(root) {
		t.Errorf("scaffolded .kovan.yaml did not load to defaults: %+v", r)
	}

	write(t, path, "domain: writing\n")
	if err := ScaffoldRepo(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "domain: writing\n" {
		t.Errorf("scaffold overwrote an existing file: %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
