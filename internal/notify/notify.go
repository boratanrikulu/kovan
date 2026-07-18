// Package notify sends short desktop notifications when an agent changes state.
package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

// Notifier delivers a notification to the user.
type Notifier interface {
	Notify(title, body string) error
}

// For returns the notifier for the configured backend. Unknown or disabled
// backends get a no-op, so callers never special-case "notifications off".
func For(kind string) Notifier {
	if kind == "macos" {
		return MacOS{}
	}
	return Noop{}
}

// Noop drops notifications.
type Noop struct{}

func (Noop) Notify(string, string) error { return nil }

// MacOS posts through osascript, which keeps kovan dependency-free.
type MacOS struct{}

func (MacOS) Notify(title, body string) error {
	script := fmt.Sprintf("display notification %s with title %s", quote(body), quote(title))
	return exec.Command("osascript", "-e", script).Run()
}

// quote renders s as an AppleScript string literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
