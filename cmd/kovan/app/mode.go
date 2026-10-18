package app

import (
	"github.com/boratanrikulu/kovan/internal/session"
)

// transcriptTailBytes is how much of the end of a transcript we scan for the
// latest permission mode: enough to span recent turns without reading a large
// file every board tick.
const transcriptTailBytes = 64 << 10

// permMode returns the board's PERM column for an agent: the live permission
// mode from its transcript when the CLI records one there (Claude does, every
// turn, so it tracks shift+tab toggles the hook never sees), falling back to
// the hook-seen Mode in the manifest.
func permMode(m *session.Manifest) string {
	if mode, ok := manifestConvo(m).perm(); ok {
		return mapMode(mode)
	}
	return m.Mode
}

// perm returns the last permission mode recorded in the transcript's tail.
func (c convo) perm() (string, bool) {
	tail, ok := c.tail(transcriptTailBytes)
	if !ok {
		return "", false
	}
	return c.d.Perm(tail)
}

// mapMode translates the permission mode values Claude Code and Codex report
// to kovan's board labels; an unknown value passes through unchanged (still
// better than a stale label).
func mapMode(mode string) string {
	switch mode {
	case "default":
		return "default"
	case "acceptEdits":
		return "auto"
	case "plan":
		return "plan"
	case "bypassPermissions":
		return "bypass"
	default:
		return mode
	}
}
