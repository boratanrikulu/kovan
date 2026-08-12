package mode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuiltins(t *testing.T) {
	home := t.TempDir() // no user modes → built-ins
	cases := []struct {
		name     string
		posture  string
		artifact string
	}{
		{"code", "edit", "spec.md"},
		{"review", "read-only", "review.md"},
		{"analyze", "read-only", "analysis.md"},
		{"write", "read-only", "draft.md"},
	}
	for _, c := range cases {
		m, err := Load(home, c.name)
		if err != nil {
			t.Fatalf("Load(%q): %v", c.name, err)
		}
		if m.Posture != c.posture {
			t.Errorf("%s posture = %q, want %q", c.name, m.Posture, c.posture)
		}
		if m.Artifact() != c.artifact {
			t.Errorf("%s artifact = %q, want %q", c.name, m.Artifact(), c.artifact)
		}
		if !strings.Contains(m.Prompt, "{{brief}}") {
			t.Errorf("%s prompt should reference {{brief}}:\n%s", c.name, m.Prompt)
		}
	}
}

func TestLoadDefaultAndUnknown(t *testing.T) {
	home := t.TempDir()
	if m, err := Load(home, ""); err != nil || m.Name != Default {
		t.Errorf("empty name should resolve to %q: got %+v, err %v", Default, m, err)
	}
	if _, err := Load(home, "nope"); err == nil {
		t.Error("unknown mode should error")
	}
}

func TestUserOverride(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "modes", "code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("MY OWN PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode.yaml"), []byte("posture: read-only\ndocs: [notes.md]\nwrite_paths: [kovan/mentor/]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(home, "code")
	if err != nil {
		t.Fatal(err)
	}
	if m.Prompt != "MY OWN PROMPT" || m.Posture != "read-only" || m.Artifact() != "notes.md" {
		t.Errorf("user override not applied: %+v", m)
	}
	if len(m.WritePaths) != 1 || m.WritePaths[0] != "kovan/mentor/" {
		t.Errorf("write_paths not parsed: %v", m.WritePaths)
	}
}

// writeMode drops the given files into ~/.kovan/modes/<name>/.
func writeMode(t *testing.T, home, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(home, "modes", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A prompt-only override must not reset the rest of the built-in: retuning
// review's wording once dropped it from read-only to edit, silently.
func TestPromptOnlyOverrideKeepsPosture(t *testing.T) {
	home := t.TempDir()
	writeMode(t, home, "review", map[string]string{"prompt.md": "MY REVIEW PROMPT {{brief}}"})

	m, err := Load(home, "review")
	if err != nil {
		t.Fatal(err)
	}
	if m.Prompt != "MY REVIEW PROMPT {{brief}}" {
		t.Errorf("prompt = %q, want the user's", m.Prompt)
	}
	if !m.ReadOnly() {
		t.Errorf("posture = %q, want read-only from the built-in", m.Posture)
	}
	if m.Artifact() != "review.md" {
		t.Errorf("artifact = %q, want review.md from the built-in", m.Artifact())
	}
}

// A mode.yaml with no prompt.md is a valid retune of a built-in, and only the
// fields it sets move.
func TestConfigOnlyOverrideIsPerField(t *testing.T) {
	home := t.TempDir()
	writeMode(t, home, "code", map[string]string{"mode.yaml": "write_paths: [docs/]\n"})

	m, err := Load(home, "code")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.WritePaths) != 1 || m.WritePaths[0] != "docs/" {
		t.Errorf("write_paths = %v, want the user's", m.WritePaths)
	}
	if m.Posture != "edit" || m.Artifact() != "spec.md" {
		t.Errorf("built-in posture/docs lost: %+v", m)
	}
	if !strings.Contains(m.Prompt, "{{brief}}") {
		t.Errorf("built-in prompt lost:\n%s", m.Prompt)
	}
}

// An explicit empty list says "none"; leaving the key out keeps the built-in's.
func TestEmptyDocsListMeansNone(t *testing.T) {
	home := t.TempDir()
	writeMode(t, home, "code", map[string]string{"mode.yaml": "docs: []\n"})

	m, err := Load(home, "code")
	if err != nil {
		t.Fatal(err)
	}
	if m.Artifact() != "" {
		t.Errorf("artifact = %q, want none", m.Artifact())
	}
}

// A directory that is not a built-in still needs a prompt.md to be a mode.
func TestUserModeWithoutPromptIsUnknown(t *testing.T) {
	home := t.TempDir()
	writeMode(t, home, "finance", map[string]string{"mode.yaml": "posture: read-only\n"})

	if _, err := Load(home, "finance"); err == nil {
		t.Error("Load should fail for a mode dir with no prompt.md and no built-in")
	}
}

func TestList(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "modes", "finance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "modes", "finance", "prompt.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	names := List(home)
	if len(names) == 0 || names[0] != Default {
		t.Errorf("List should put %q first: %v", Default, names)
	}
	has := func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"code", "review", "analyze", "write", "finance"} {
		if !has(want) {
			t.Errorf("List missing %q: %v", want, names)
		}
	}
}

func TestMethodFile(t *testing.T) {
	home := t.TempDir()
	// A built-in's embedded method.md is materialized to disk on first use, so the
	// path is live and editable.
	path, err := MethodFile(home, "review")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "modes", "review", "method.md")
	if path != want {
		t.Errorf("MethodFile path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("materialized method should exist: %v", err)
	}
	if !strings.Contains(string(body), "Review method") {
		t.Errorf("materialized review method missing its content:\n%s", body)
	}

	// An existing on-disk method.md is left untouched (no-clobber) and reused.
	if err := os.WriteFile(path, []byte("MY REVIEW METHOD"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err = MethodFile(home, "review")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "MY REVIEW METHOD" {
		t.Errorf("MethodFile clobbered the user's method: %q", got)
	}

	// A mode with no method anywhere yields "".
	if p, err := MethodFile(home, "no-such-mode"); err != nil || p != "" {
		t.Errorf("unknown mode method = %q, err %v; want empty", p, err)
	}
}
