package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// transcriptPaths caches session id → transcript path. The glob that resolves
// a transcript walks every dir under ~/.claude/projects and dominates the
// board's refresh cost, while the answer never changes for a live session —
// so it is resolved once and re-globbed only if the cached file stops being
// readable.
var transcriptPaths sync.Map

// transcriptTail returns up to the last max bytes of the session's transcript
// — ~/.claude/projects/*/<id>.jsonl, no path-encoding logic needed — resolving
// the path once and re-globbing only when the cached file goes away.
func transcriptTail(sessionID string, max int64) ([]byte, bool) {
	if sessionID == "" {
		return nil, false
	}
	if p, ok := transcriptPaths.Load(sessionID); ok {
		if tail, err := readTail(p.(string), max); err == nil {
			return tail, true
		}
		transcriptPaths.Delete(sessionID)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return nil, false
	}
	transcriptPaths.Store(sessionID, matches[0])
	tail, err := readTail(matches[0], max)
	if err != nil {
		return nil, false
	}
	return tail, true
}

// readTail returns up to the last max bytes of path.
func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > max {
		if _, err := f.Seek(info.Size()-max, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}

// digestTailBytes is how much transcript tail feeds a summary. Bigger than the
// permission-mode scan window: a single tool_result line can run to hundreds
// of kilobytes, and the window must still reach past it to real turns.
const digestTailBytes = 256 << 10

// digestMaxEntries caps how many conversation entries a digest carries.
const digestMaxEntries = 40

// Clip widths for digest entries, in runes.
const (
	digestTextClip = 300
	digestToolClip = 120
)

// transcriptDigest renders the tail of an agent's conversation as short
// labeled lines — real turns only. Text sitting unsent in the agent's input
// box never reaches the transcript, so it cannot leak into a summary. Empty
// when the session has no transcript (or nothing summarizable yet).
func transcriptDigest(sessionID string) string {
	tail, ok := transcriptTail(sessionID, digestTailBytes)
	if !ok {
		return ""
	}
	return digest(tail)
}

// The slices of a transcript line the digest reads. Message content is either
// a plain string (a typed human turn) or an array of blocks (assistant text /
// tool_use, or a user-side tool_result).
type transcriptRecord struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// digest parses transcript JSONL into labeled entries: "you:" for what the
// human actually sent, "agent:" for the agent's replies, "agent ran" for its
// tool calls. Everything else (tool results, thinking, system records) is
// skipped, as is any unparseable line — the tail may open mid-record.
func digest(tail []byte) string {
	var entries []string
	for _, line := range bytes.Split(tail, []byte("\n")) {
		var rec transcriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		switch rec.Type {
		case "user":
			var text string
			if err := json.Unmarshal(rec.Message.Content, &text); err != nil {
				continue // array content is a tool_result, not a human turn
			}
			if s := oneLine(text); s != "" {
				entries = append(entries, "you: "+clip(s, digestTextClip))
			}
		case "assistant":
			var blocks []contentBlock
			if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if s := oneLine(b.Text); s != "" {
						entries = append(entries, "agent: "+clip(s, digestTextClip))
					}
				case "tool_use":
					entries = append(entries, toolEntry(b))
				}
			}
		}
	}
	if len(entries) > digestMaxEntries {
		entries = entries[len(entries)-digestMaxEntries:]
	}
	return strings.Join(entries, "\n")
}

// toolEntry renders a tool_use block as one line: the tool name plus the most
// telling scrap of its input.
func toolEntry(b contentBlock) string {
	e := "agent ran " + b.Name
	for _, key := range []string{"description", "command", "file_path", "prompt"} {
		if v, ok := b.Input[key].(string); ok && v != "" {
			return e + ": " + clip(oneLine(v), digestToolClip)
		}
	}
	return e
}
