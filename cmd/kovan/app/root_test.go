package app

import (
	"strings"
	"testing"
)

// listedCommands returns the command names rendered in a usage string (the first
// token of each indented line), so tests check the real help output.
func listedCommands(usage string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(line, "  ") {
			if f := strings.Fields(line); len(f) > 0 {
				out[f[0]] = true
			}
		}
	}
	return out
}

func TestUsageGroups(t *testing.T) {
	usage := rootCmd.UsageString()

	for _, title := range []string{"Setup:", "Agents:", "Method:"} {
		if !strings.Contains(usage, title) {
			t.Errorf("usage missing group title %q:\n%s", title, usage)
		}
	}

	listed := listedCommands(usage)
	for _, want := range []string{"setup", "init", "start", "open", "remove", "status", "method"} {
		if !listed[want] {
			t.Errorf("command %q should be listed:\n%s", want, usage)
		}
	}
	// ui is gone; plumbing and help stay hidden.
	for _, hidden := range []string{"ui", "gate", "task", "switch", "shell-init", "help"} {
		if listed[hidden] {
			t.Errorf("command %q should NOT be listed:\n%s", hidden, usage)
		}
	}
}

func TestMethodCommandIsOpener(t *testing.T) {
	if methodCmd.RunE == nil {
		t.Error("methodCmd should have a RunE so `kovan method` opens the inspector")
	}
	if methodCmd.GroupID != "method" {
		t.Errorf("methodCmd.GroupID = %q, want %q", methodCmd.GroupID, "method")
	}
}
