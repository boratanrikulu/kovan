// Package onboard renders the prompt that drives kovan init: kovan scaffolds and
// launches Claude, and this prompt is the judgment work Claude does. The prompt
// is a single embedded template so it stays easy to edit.
package onboard

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed prompt.md
var promptTemplate string

// Data fills the onboarding prompt for one repo.
type Data struct {
	Repo        string // repo root being onboarded
	ClaudeMD    string // path to the user's ~/.claude/CLAUDE.md
	Account     string // resolved account for this repo ("" if none)
	Reference   string // optional repo whose rules to lift ("" if none)
	GlobalEmpty bool   // ~/.kovan/method/global/ has no files yet
}

// Prompt renders the onboarding prompt for the launched Claude.
func Prompt(d Data) (string, error) {
	t, err := template.New("onboard").Parse(promptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse onboarding prompt: %w", err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", fmt.Errorf("render onboarding prompt: %w", err)
	}
	return b.String(), nil
}
