package method

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGlobal(t *testing.T) {
	home := t.TempDir()
	if got := Global(home); got != nil {
		t.Errorf("absent global layer = %v, want nil", got)
	}
	touch(t, filepath.Join(home, "method", "global", "soul.md"))
	touch(t, filepath.Join(home, "method", "global", "rules.md"))
	touch(t, filepath.Join(home, "method", "global", "notes.txt")) // ignored: not .md
	got := Global(home)
	if len(got) != 2 || filepath.Base(got[0]) != "rules.md" || filepath.Base(got[1]) != "soul.md" {
		t.Errorf("Global = %v, want sorted [rules.md soul.md]", got)
	}
}

func TestWorktree(t *testing.T) {
	home := t.TempDir()
	touch(t, filepath.Join(home, "method", "accounts", "personal", "voice.md"))
	touch(t, filepath.Join(home, "method", "domains", "code", "style.md"))
	touch(t, filepath.Join(home, "projects", "kovan", "notes.md"))

	layers := Worktree(home, "personal", "code", "kovan")
	if len(layers) != 3 {
		t.Fatalf("got %d layers, want 3: %+v", len(layers), layers)
	}
	if layers[0].Name != "account:personal" || layers[1].Name != "domain:code" || layers[2].Name != "project:kovan" {
		t.Errorf("layer order wrong: %s, %s, %s", layers[0].Name, layers[1].Name, layers[2].Name)
	}

	// Empty domain and a repo with no private dir are skipped.
	layers = Worktree(home, "personal", "", "other")
	if len(layers) != 1 || layers[0].Name != "account:personal" {
		t.Errorf("empty layers should be skipped, got %+v", layers)
	}

	// No account configured → no account layer.
	if layers := Worktree(home, "", "", "other"); len(layers) != 0 {
		t.Errorf("no layers expected, got %+v", layers)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveImportsRelativeAndTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()

	write(t, filepath.Join(dir, "relative.md"), "rel")
	write(t, filepath.Join(home, "abs.md"), "abs")
	parent := filepath.Join(dir, "CLAUDE.md")
	write(t, parent, "intro\n@relative.md\nmore @~/abs.md here\n")

	got := ResolveImports([]string{parent})
	want := []File{
		{Path: parent, Depth: 0},
		{Path: filepath.Join(dir, "relative.md"), Depth: 1},
		{Path: filepath.Join(home, "abs.md"), Depth: 1},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ResolveImports =\n %v\nwant\n %v", got, want)
	}
}

func TestResolveImportsIgnoresEmailAndFence(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "CLAUDE.md")
	write(t, parent, "Contact me@example.com for help\n```\n@infence.md\n```\n@real.md\n")

	got := ResolveImports([]string{parent})
	if len(got) != 2 {
		t.Fatalf("got %d files, want 2 (parent + real.md): %v", len(got), got)
	}
	if got[1].Path != filepath.Join(dir, "real.md") || got[1].Depth != 1 {
		t.Errorf("second file = %v, want real.md at depth 1", got[1])
	}
	for _, f := range got {
		if filepath.Base(f.Path) == "example.com" || filepath.Base(f.Path) == "infence.md" {
			t.Errorf("email/fenced @ wrongly imported: %s", f.Path)
		}
	}
}

func TestResolveImportsCycleAndDepthCap(t *testing.T) {
	dir := t.TempDir()
	// A cycle a -> b -> a must terminate, listing each once.
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	write(t, a, "@b.md\n")
	write(t, b, "@a.md\n")
	if got := ResolveImports([]string{a}); len(got) != 2 {
		t.Errorf("cycle: got %d files, want 2 (a, b): %v", len(got), got)
	}

	// A long chain c0 -> c1 -> … is capped at depth 5.
	for i := 0; i < 8; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("c%d.md", i)), fmt.Sprintf("@c%d.md\n", i+1))
	}
	got := ResolveImports([]string{filepath.Join(dir, "c0.md")})
	if len(got) != 6 { // c0 (depth 0) … c5 (depth 5)
		t.Fatalf("depth cap: got %d files, want 6: %v", len(got), got)
	}
	if got[len(got)-1].Depth != 5 {
		t.Errorf("deepest file depth = %d, want 5", got[len(got)-1].Depth)
	}
}

func TestResolveImportsDeduplicatesDirect(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	write(t, a, "hi\n")
	write(t, b, "@a.md\n") // b imports a, which is also listed directly

	got := ResolveImports([]string{a, b})
	if len(got) != 2 {
		t.Fatalf("got %d files, want 2 (a once, b once): %v", len(got), got)
	}
	count := 0
	for _, f := range got {
		if filepath.Clean(f.Path) == filepath.Clean(a) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("a listed %d times, want 1", count)
	}
}

func TestResolveImportsSkipsMentions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "notes.md"), "n")
	write(t, filepath.Join(dir, "docs", "AGENTS.md"), "a")
	parent := filepath.Join(dir, "CLAUDE.md")
	write(t, parent, strings.Join([]string{
		"intro",
		"cc @fulyauluturk on DB files", // mention → skip
		"@docs/AGENTS.md",              // path-like (has /) → import
		"@notes.md",                    // file extension → import
		"@real/missing.md",             // path-like but missing → still listed
		"contact me@x.com please",      // email → ignored (@ follows a letter)
	}, "\n"))

	var imports []string
	for _, f := range ResolveImports([]string{parent}) {
		if f.Depth > 0 {
			imports = append(imports, f.Path)
		}
		if strings.Contains(f.Path, "fulyauluturk") || strings.Contains(f.Path, "x.com") {
			t.Errorf("phantom import from a mention/email: %q", f.Path)
		}
	}

	want := []string{
		filepath.Join(dir, "docs", "AGENTS.md"),
		filepath.Join(dir, "notes.md"),
		filepath.Join(dir, "real", "missing.md"),
	}
	if fmt.Sprint(imports) != fmt.Sprint(want) {
		t.Errorf("imports =\n %v\nwant\n %v", imports, want)
	}
}

func TestScaffold(t *testing.T) {
	home := t.TempDir()
	if err := Scaffold(home); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"method/global", "method/accounts", "method/domains", "projects"} {
		if info, err := os.Stat(filepath.Join(home, d)); err != nil || !info.IsDir() {
			t.Errorf("Scaffold did not create %s: %v", d, err)
		}
	}
}
