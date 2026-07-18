package app

import (
	"bytes"

	"github.com/boratanrikulu/kovan/internal/session"
)

// transcriptTailBytes is how much of the end of a transcript we scan for the
// latest permissionMode — enough to span recent turns without reading a large
// file every board tick.
const transcriptTailBytes = 64 << 10

// permMode returns the board's PERM column for an agent: the live permission
// mode from its Claude transcript (which records it every turn, so it tracks
// shift+tab toggles the hook never sees), falling back to the hook-seen Mode in
// the manifest when the transcript or field isn't found.
func permMode(m *session.Manifest) string {
	if mode, ok := transcriptMode(m.SessionID); ok {
		return mapMode(mode)
	}
	return m.Mode
}

// transcriptMode returns the last permissionMode recorded in the tail of the
// agent's transcript (found by session id).
func transcriptMode(sessionID string) (string, bool) {
	tail, ok := transcriptTail(sessionID, transcriptTailBytes)
	if !ok {
		return "", false
	}
	return lastPermissionMode(tail)
}

// lastPermissionMode extracts the value of the last "permissionMode":"..." in
// data. It only accepts a quoted string value (so a null or absent field is no
// match), and scans from the end so the most recent turn wins.
func lastPermissionMode(data []byte) (string, bool) {
	key := []byte(`"permissionMode"`)
	for i := bytes.LastIndex(data, key); i >= 0; i = bytes.LastIndex(data[:i], key) {
		rest := data[i+len(key):]
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != ':' {
			continue
		}
		j++
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != '"' {
			continue // null or non-string value
		}
		j++
		if end := bytes.IndexByte(rest[j:], '"'); end >= 0 {
			return string(rest[j : j+end]), true
		}
	}
	return "", false
}

// mapMode translates Claude's permissionMode values to kovan's board labels;
// an unknown value passes through unchanged (still better than a stale label).
func mapMode(claude string) string {
	switch claude {
	case "default":
		return "default"
	case "acceptEdits":
		return "auto"
	case "plan":
		return "plan"
	case "bypassPermissions":
		return "bypass"
	default:
		return claude
	}
}
