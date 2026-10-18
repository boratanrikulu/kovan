package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/session"
)

// The task block below is composed into every agent's method at launch
// (methodcompose.go). A worktree's CLAUDE.local.md can still hold a
// kovan-managed block of its own; these helpers find and remove it.
const claudeLocalFile = "CLAUDE.local.md"

// The kovan-managed region is sentinel-delimited so re-runs and the user's own
// edits to the rest of the file are safe.
const (
	managedStart = "<!-- >>> kovan managed >>> -->"
	managedEnd   = "<!-- <<< kovan <<< -->"
)

// workspacesSection lists the agent's worktrees when it works in more than one
// repo, or "" for a single-repo agent.
func workspacesSection(worktree, repo string, extra []session.Workspace) string {
	if len(extra) == 0 {
		return ""
	}
	lines := []string{
		"## Workspaces",
		"",
		"You work in more than one repo. Each has its own worktree and branch; edit a repo only inside its worktree.",
		"",
		fmt.Sprintf("- `%s`: `%s` (this one)", repo, worktree),
	}
	for _, w := range extra {
		lines = append(lines, fmt.Sprintf("- `%s`: `%s`, branch `%s`", w.Repo, w.Worktree, w.Branch))
	}
	return strings.Join(lines, "\n")
}

// claudeLocalBlock is the maintenance protocol the agent carries: the
// brief to execute and where to keep status, learnings, and the test plan. The
// docs live in the durable kovan store, so the paths are absolute.
func claudeLocalBlock(id, title, taskAbs string, m *mode.Mode) string {
	ctx := filepath.Join(taskAbs, "context.md")
	learnings := filepath.Join(taskAbs, "learnings.md")
	protocol := modeProtocol(taskAbs, m)
	return fmt.Sprintf(`%s %s  (mode: %s)

Brief: %s — read it first. %s Keep the brief's Status current; record gotchas in %s.`,
		taskHeading(id), title, modeName(m), ctx, protocol, learnings)
}

// taskHeading opens the managed block; it names the agent that owns the block.
func taskHeading(id string) string {
	return fmt.Sprintf("## Task — %s:", id)
}

// ownsClaudeLocal reports whether content's managed block belongs to agent id.
func ownsClaudeLocal(content, id string) bool {
	return strings.Contains(content, managedStart+"\n"+taskHeading(id))
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
