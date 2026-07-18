// Package method resolves Bora's layered method — the per-agent context kovan
// composes from ~/.kovan: the global layer, the account and domain layers, and
// each repo's private layer. It only locates files; composition (the @import
// wiring) lives in the app.
package method

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dir is the method root, ~/.kovan/method.
func Dir(home string) string { return filepath.Join(home, "method") }

func globalDir(home string) string        { return filepath.Join(Dir(home), "global") }
func accountDir(home, name string) string { return filepath.Join(Dir(home), "accounts", name) }
func domainDir(home, name string) string  { return filepath.Join(Dir(home), "domains", name) }
func projectDir(home, repo string) string { return filepath.Join(home, "projects", repo) }

// mdFiles returns the sorted *.md files in dir, or nil when the dir is absent —
// an empty layer simply contributes nothing.
func mdFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// File is one file in a method layer. Depth 0 is a file listed directly in the
// layer; Depth > 0 is a file pulled in transitively via @import, nested under
// the file that imports it.
type File struct {
	Path  string
	Depth int
}

// Layer is one method layer's resolved contents: method files (with @imports
// expanded, delivered via @import) and skills (each a SKILL.md, delivered by
// symlink). They are separate because they reach Claude by different mechanisms.
type Layer struct {
	Name   string
	Files  []File
	Skills []File
}

// SkillFiles turns skill directories into File entries pointing at each skill's
// SKILL.md — for listing skills in the method inspector / `method show`.
func SkillFiles(dirs []string) []File {
	out := make([]File, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, File{Path: filepath.Join(d, "SKILL.md")})
	}
	return out
}

// Global returns the global-layer files listed directly, without @import
// expansion: `kovan method link` writes one @import line per file and Claude
// Code expands them itself, so linking must see only the directly-listed files.
func Global(home string) []string { return mdFiles(globalDir(home)) }

// Files lists the sorted *.md files in dir directly (no @import expansion),
// empty when absent — for callers that resolve an arbitrary layer dir.
func Files(dir string) []string { return mdFiles(dir) }

// Worktree returns the per-worktree layers an agent composes — account, domain,
// project-private — in order, each with its @imports expanded, skipping any
// layer that contributes no files. The CLAUDE.local.md writer takes only the
// Depth-0 files (Claude Code expands the imports itself).
func Worktree(home, account, domain, repo string) []Layer {
	var layers []Layer
	add := func(name, dir string) {
		files := ResolveImports(mdFiles(dir))
		skills := SkillFiles(skillDirs(dir))
		if len(files) > 0 || len(skills) > 0 {
			layers = append(layers, Layer{Name: name, Files: files, Skills: skills})
		}
	}
	if account != "" {
		add("account:"+account, accountDir(home, account))
	}
	if domain != "" {
		add("domain:"+domain, domainDir(home, domain))
	}
	add("project:"+repo, projectDir(home, repo))
	return layers
}

const maxImportDepth = 5

// importRe matches a Claude Code @import: an @ at start-of-line or after
// whitespace, then the path as the following non-space run. The leading
// (^|\s) is what excludes emails like me@bora.sh, where @ follows a letter.
var importRe = regexp.MustCompile(`(^|\s)@(\S+)`)

// ResolveImports expands the @import references in direct transitively. The
// result lists each direct file at Depth 0 immediately followed by the files it
// imports (Depth > 0), depth-first. Recursion is capped at maxImportDepth, and a
// visited set seeded with the direct files both stops cycles and de-duplicates,
// so a file (including a direct file imported elsewhere) is listed at most once.
// A path that does not exist on disk is still listed; it simply imports nothing.
func ResolveImports(direct []string) []File {
	visited := make(map[string]bool, len(direct))
	for _, f := range direct {
		visited[filepath.Clean(f)] = true
	}
	var out []File
	for _, f := range direct {
		out = append(out, File{Path: f, Depth: 0})
		out = append(out, expandImports(f, 1, visited)...)
	}
	return out
}

func expandImports(file string, depth int, visited map[string]bool) []File {
	if depth > maxImportDepth {
		return nil
	}
	var out []File
	for _, imp := range fileImports(file) {
		key := filepath.Clean(imp)
		if visited[key] {
			continue
		}
		visited[key] = true
		out = append(out, File{Path: imp, Depth: depth})
		out = append(out, expandImports(imp, depth+1, visited)...)
	}
	return out
}

// fileImports returns the @import paths in file, each resolved against the
// file's own directory (absolute and leading-~ paths are honored). @ lines
// inside fenced code blocks are not imports and are skipped.
func fileImports(file string) []string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	base := filepath.Dir(file)
	var out []string
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range importRe.FindAllStringSubmatch(line, -1) {
			ref := m[2]
			resolved := resolveImportPath(base, ref)
			// A bare @mention (no "/" or extension) that doesn't exist on disk is a
			// handle, not a file — Claude Code doesn't import it either. A path-like
			// ref (or one that resolves to a real file) is a genuine import, even if
			// path-like-but-missing (so a not-yet-created @real/path.md stays listed).
			if pathLike(ref) || fileExists(resolved) {
				out = append(out, resolved)
			}
		}
	}
	return out
}

// pathLike reports whether ref looks like a file path — it has a directory
// separator or a file extension — as opposed to a bare @mention handle.
func pathLike(ref string) bool {
	return strings.Contains(ref, "/") || filepath.Ext(ref) != ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func resolveImportPath(base, p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// skillDirs lists the skill directories under layerDir/skills — each a <name>/
// holding a SKILL.md — sorted, or nil when the dir is absent. A subdirectory
// without SKILL.md is not a skill and is skipped.
func skillDirs(layerDir string) []string {
	root := filepath.Join(layerDir, "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "SKILL.md")); err != nil {
			continue
		}
		out = append(out, filepath.Join(root, e.Name()))
	}
	sort.Strings(out)
	return out
}

// GlobalSkills returns the global-layer skill dirs, surfaced to every Claude
// session (kovan symlinks them into ~/.claude/skills).
func GlobalSkills(home string) []string { return skillDirs(globalDir(home)) }

// WorktreeSkills returns the scoped skill dirs for an agent — account, then
// domain, then project — one per name, the narrowest scope winning a name
// collision (project over domain over account). kovan symlinks these into the
// worktree's .claude/skills.
func WorktreeSkills(home, account, domain, repo string) []string {
	byName := map[string]string{}
	add := func(layerDir string) {
		for _, d := range skillDirs(layerDir) {
			byName[filepath.Base(d)] = d
		}
	}
	if account != "" {
		add(accountDir(home, account))
	}
	if domain != "" {
		add(domainDir(home, domain))
	}
	add(projectDir(home, repo))

	out := make([]string, 0, len(byName))
	for _, d := range byName {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Scaffold creates the method directory skeleton so each layer has a home to
// author into. Existing directories are left untouched.
func Scaffold(home string) error {
	for _, d := range []string{
		globalDir(home),
		filepath.Join(Dir(home), "accounts"),
		filepath.Join(Dir(home), "domains"),
		filepath.Join(home, "projects"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
