package onboard

import (
	"strings"
	"testing"
)

func TestPromptFirstTime(t *testing.T) {
	got, err := Prompt(Data{
		Repo:        "/work/gobee",
		ClaudeMD:    "/home/bora/.claude/CLAUDE.md",
		Account:     "personal",
		Reference:   "/work/agent",
		GlobalEmpty: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"/work/gobee",                     // repo path
		"/home/bora/.claude/CLAUDE.md",    // claude md
		"this repo's account is personal", // account filled
		"/work/agent",                     // reference lifted
		"Global method",                   // global section shown
		"kovan method link",               // wiring step
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestPromptGlobalAlreadySet(t *testing.T) {
	got, err := Prompt(Data{Repo: "/work/gobee", GlobalEmpty: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Global method") || strings.Contains(got, "kovan method link") {
		t.Errorf("global section should be omitted when global is non-empty:\n%s", got)
	}
	if !strings.Contains(got, "This repo") || !strings.Contains(got, "/work/gobee") {
		t.Error("repo section should always be present")
	}
}

func TestPromptNoReferenceNoAccount(t *testing.T) {
	got, err := Prompt(Data{Repo: "/r", GlobalEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Lift the universal rules") {
		t.Error("no reference → no lift line")
	}
	if strings.Contains(got, "account is") {
		t.Error("no account → no account parenthetical")
	}
}
