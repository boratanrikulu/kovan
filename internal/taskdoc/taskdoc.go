// Package taskdoc scaffolds and preserves the per-agent task documents
// (context / test-plan / learnings) that carry a task's brief and learnings.
package taskdoc

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*.md
var defaultTemplates embed.FS

// Brief is the document holding the agent's brief — the one start opens in the
// editor and points the agent at. Learnings accumulates gotchas across worktrees.
// Both are scaffolded for every task; the rest of the doc set is mode-specific.
const (
	Brief     = "context.md"
	Learnings = "learnings.md"
)

// Spec is the document the code mode's agent writes before coding: its plan,
// tasks, and assumptions, drawn from the brief. The human reviews it first.
const Spec = "spec.md"

// Scaffold writes the task documents into destDir, substituting the task id and
// title. The doc set is the brief and learnings (always) plus the mode's docs
// (e.g. spec.md, review.md). Templates come from templatesDir when it holds a
// matching file, else the built-in defaults. token, when set, is also replaced
// with id (e.g. a TASK-XXXXX placeholder) alongside the {{id}}/{{title}}
// placeholders. Existing docs are left untouched.
func Scaffold(destDir, id, title, token, templatesDir string, modeDocs []string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create task dir: %w", err)
	}
	names := append([]string{Brief}, modeDocs...)
	names = append(names, Learnings)
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		dest := filepath.Join(destDir, name)
		if _, err := os.Stat(dest); err == nil {
			continue // never clobber an existing doc
		}
		tmpl, err := templateFor(name, templatesDir)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(substitute(tmpl, id, title, token)), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// BriefTemplate returns the raw context.md template (user override or built-in),
// so the cockpit can prefill its brief editor with the same skeleton $EDITOR
// would show. Placeholders are left unsubstituted; Substitute fills them on save.
func BriefTemplate(templatesDir string) (string, error) {
	return templateFor(Brief, templatesDir)
}

// Substitute fills a template's {{id}}/{{title}} placeholders (and the optional
// token) — exported so the in-form brief is written the same way Scaffold writes.
func Substitute(tmpl, id, title, token string) string {
	return substitute(tmpl, id, title, token)
}

func templateFor(name, templatesDir string) (string, error) {
	if templatesDir != "" {
		b, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err == nil {
			return string(b), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read template %s: %w", name, err)
		}
	}
	b, err := defaultTemplates.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("builtin template %s: %w", name, err)
	}
	return string(b), nil
}

func substitute(tmpl, id, title, token string) string {
	out := strings.NewReplacer("{{id}}", id, "{{title}}", title).Replace(tmpl)
	if token != "" {
		out = strings.ReplaceAll(out, token, id)
	}
	return out
}
