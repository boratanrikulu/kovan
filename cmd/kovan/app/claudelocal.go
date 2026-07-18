package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/mode"
)

// CLAUDE.local.md loads every session, survives compaction, is per-worktree and
// gitignored — the right home for a pointer to this agent's task docs.
const claudeLocalFile = "CLAUDE.local.md"

// The kovan-managed region is sentinel-delimited so re-runs and the user's own
// edits to the rest of the file are safe.
const (
	managedStart = "<!-- >>> kovan managed >>> -->"
	managedEnd   = "<!-- <<< kovan <<< -->"
)

// writeClaudeLocal upserts the kovan-managed block into the worktree's
// CLAUDE.local.md: the brief pointer plus @import lines for this agent's method
// layers (account, domain, project-private). taskAbs is the agent's durable
// task-doc dir (outside the worktree). The user's own content is intact.
func writeClaudeLocal(worktree, id, title, taskAbs, account, domain, repo string, m *mode.Mode) error {
	path := filepath.Join(worktree, claudeLocalFile)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", claudeLocalFile, err)
	}
	block := claudeLocalBlock(id, title, taskAbs, m)
	// The mode's working method (its method.md) is @imported by its live path, so
	// the agent carries it across sessions and edits to it reach existing agents —
	// like every other method layer.
	modeMethod := ""
	if m != nil {
		if home, err := config.Dir(); err == nil {
			if p, e := mode.MethodFile(home, m.Name); e == nil {
				modeMethod = p
			}
		}
	}
	if section := methodSection(account, domain, repo, modeMethod); section != "" {
		block += "\n\n" + section
	}
	out := upsertManagedBlock(string(existing), block)
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", claudeLocalFile, err)
	}
	return nil
}

// methodSection renders @import lines for the agent's per-worktree method layers
// plus the task mode's own method file (modeMethod, "" when the mode ships none),
// or "" when nothing contributes a file.
func methodSection(account, domain, repo, modeMethod string) string {
	home, err := config.Dir()
	if err != nil {
		return ""
	}
	// Only the directly-listed files get an @import line; Claude Code expands
	// their transitive imports itself, so we skip Depth > 0.
	var files []string
	for _, l := range method.Worktree(home, account, domain, repo) {
		for _, f := range l.Files {
			if f.Depth == 0 {
				files = append(files, f.Path)
			}
		}
	}
	if modeMethod != "" {
		files = append(files, modeMethod)
	}
	if len(files) == 0 {
		return ""
	}
	return "## Method\n\n" + importLines(files)
}

// claudeLocalBlock is the maintenance protocol the agent reads each session: the
// brief to execute and where to keep status, learnings, and the test plan. The
// docs live in the durable kovan store, so the paths are absolute.
func claudeLocalBlock(id, title, taskAbs string, m *mode.Mode) string {
	ctx := filepath.Join(taskAbs, "context.md")
	learnings := filepath.Join(taskAbs, "learnings.md")
	protocol := modeProtocol(taskAbs, m)
	return fmt.Sprintf(`## Task — %s: %s  (mode: %s)

Brief: %s — read it first. %s Keep the brief's Status current; record gotchas in %s.`,
		id, title, modeName(m), ctx, protocol, learnings)
}

// modeName is the mode's name, or "code" when unset (older manifests / quick).
func modeName(m *mode.Mode) string {
	if m == nil {
		return mode.Default
	}
	return m.Name
}

// modeProtocol is the durable role statement the agent re-reads each session: who
// it is (the mode), the artifact it owns, and its posture. For the code mode it's
// spec-then-approval; for read-only modes it's produce-the-artifact, don't touch
// code (and the read-only gate backs that up).
func modeProtocol(taskAbs string, m *mode.Mode) string {
	if m == nil || m.Artifact() == "" {
		return "Follow your task mode."
	}
	artifact := filepath.Join(taskAbs, m.Artifact())
	if m.ReadOnly() {
		return fmt.Sprintf("You are the %s. You maintain %s. Read-only: do not modify the repo's code; write only to your task docs.", m.Name, artifact)
	}
	return fmt.Sprintf("You are the %s. Before writing code, write your spec to %s and wait for my approval; implement only after I say go.", m.Name, artifact)
}

// importLines renders one Claude Code @import line per file (absolute paths).
func importLines(files []string) string {
	lines := make([]string, len(files))
	for i, f := range files {
		lines[i] = "@" + f
	}
	return strings.Join(lines, "\n")
}

// removeManagedBlock strips the kovan-managed block (and the blank lines that
// joined it) from existing, leaving the user's own content. It is the inverse of
// upsertManagedBlock, used to clean an in-place checkout's CLAUDE.local.md on
// teardown. A file with no managed block is returned unchanged.
func removeManagedBlock(existing string) string {
	start := strings.Index(existing, managedStart)
	end := strings.Index(existing, managedEnd)
	if start < 0 || end <= start {
		return existing
	}
	before := strings.TrimRight(existing[:start], "\n")
	after := strings.TrimLeft(existing[end+len(managedEnd):], "\n")
	switch {
	case before == "":
		return after
	case after == "":
		return before + "\n"
	default:
		return before + "\n\n" + after
	}
}

// clearClaudeLocal removes kovan's managed block from a worktree's
// CLAUDE.local.md, deleting the file entirely when nothing of the user's
// remains. A missing file is a no-op.
func clearClaudeLocal(worktree string) error {
	path := filepath.Join(worktree, claudeLocalFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", claudeLocalFile, err)
	}
	out := removeManagedBlock(string(data))
	if strings.TrimSpace(out) == "" {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", claudeLocalFile, err)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", claudeLocalFile, err)
	}
	return nil
}

// upsertManagedBlock replaces the kovan-managed block in existing, or appends it
// when absent, returning the new file content. The inner block is wrapped in the
// sentinels here.
func upsertManagedBlock(existing, block string) string {
	managed := managedStart + "\n" + block + "\n" + managedEnd
	start := strings.Index(existing, managedStart)
	end := strings.Index(existing, managedEnd)
	if start >= 0 && end > start {
		return existing[:start] + managed + existing[end+len(managedEnd):]
	}
	if strings.TrimSpace(existing) == "" {
		return managed + "\n"
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + managed + "\n"
}
