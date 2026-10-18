package session

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkspacesPrimaryFirst(t *testing.T) {
	m := &Manifest{Repo: "agent", RepoRoot: "/w/agent", Worktree: "/w/agent-1", Branch: "feat/a", Base: "master",
		Extra: []Workspace{{Repo: "cloud", RepoRoot: "/w/cloud", Worktree: "/w/cloud-1", Branch: "feat/c"}}}
	got := m.Workspaces()
	if len(got) != 2 {
		t.Fatalf("workspaces = %v, want primary + 1 extra", got)
	}
	want := Workspace{Repo: "agent", RepoRoot: "/w/agent", Worktree: "/w/agent-1", Branch: "feat/a", Base: "master"}
	if got[0] != want {
		t.Errorf("primary = %+v, want %+v", got[0], want)
	}
	if got[1].Repo != "cloud" {
		t.Errorf("extra = %+v, want cloud", got[1])
	}
}

func TestWorkspaceFor(t *testing.T) {
	m := &Manifest{Repo: "agent", Worktree: "/w/agent-1",
		Extra: []Workspace{{Repo: "cloud", Worktree: "/w/cloud-1"}}}
	cases := map[string]string{
		"/w/agent-1":            "agent",
		"/w/cloud-1/web/src":    "cloud",
		"/w/cloud-10":           "",
		"/somewhere/else/again": "",
	}
	for dir, want := range cases {
		ws, ok := m.WorkspaceFor(dir)
		if got := ws.Repo; ok != (want != "") || got != want {
			t.Errorf("WorkspaceFor(%q) = %q, %v; want %q", dir, got, ok, want)
		}
	}
}

func TestManifestWithoutExtraOmitsField(t *testing.T) {
	data, err := yaml.Marshal(&Manifest{ID: "X"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["extra"]; ok {
		t.Errorf("a single-repo manifest should not carry an extra key:\n%s", data)
	}
}

func TestFindFromPrimaryOnlyFindWorkspaceAll(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	primary, extra := t.TempDir(), t.TempDir()
	m := &Manifest{ID: "X", Tmux: "t", Repo: "agent", Worktree: primary,
		Extra: []Workspace{{Repo: "cloud", Worktree: extra}}}
	if err := m.Write(); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := FindFrom(filepath.Join(extra, "sub")); found {
		t.Error("FindFrom must not claim a dir inside an extra worktree")
	}
	got, w, found, err := FindWorkspace(filepath.Join(extra, "sub"))
	if err != nil || !found || got.ID != "X" || w.Worktree != extra {
		t.Errorf("FindWorkspace = %v, %+v, %v, %v; want X's cloud workspace", got, w, found, err)
	}
}

func TestReadByTmuxRefusesPaths(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())
	for _, name := range []string{"", "..", "../x", "a/b"} {
		if _, err := ReadByTmux(name); err == nil || !strings.Contains(err.Error(), "invalid session name") {
			t.Errorf("ReadByTmux(%q) = %v, want an invalid-name error", name, err)
		}
	}
}
