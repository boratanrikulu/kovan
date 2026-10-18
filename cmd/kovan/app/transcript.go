package app

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
)

// transcriptPaths caches session id → transcript path. The glob that resolves
// a transcript walks every project dir under the CLI's home and dominates the
// board's refresh cost, while the answer never changes for a live session, so
// it is resolved once and re-globbed only if the cached file stops being
// readable. Session ids are UUIDs, unique across CLIs.
var transcriptPaths sync.Map

// convo is an agent's conversation on disk: which CLI wrote it, that CLI's
// home, and the session id.
type convo struct {
	d    agent.Driver
	home string
	id   string
}

// convoOf locates the conversation of an agent running under account. Only a
// CLI whose home an account can move (codex) needs the config to find it.
func convoOf(agentName, account, id string) convo {
	d := mustDriver(agentName)
	home := d.DefaultHome()
	if account != "" && d.HomeEnv() != "" {
		if global, err := config.LoadGlobal(); err == nil {
			home = agentHome(global, d, account)
		}
	}
	return convo{d: d, home: home, id: id}
}

// manifestConvo is the conversation of the agent a manifest describes.
func manifestConvo(m *session.Manifest) convo {
	return convoOf(m.Agent, m.Account, m.SessionID)
}

// tail returns up to the last max bytes of the transcript, resolving the path
// once and re-globbing only when the cached file goes away.
func (c convo) tail(max int64) ([]byte, bool) {
	path, ok := c.path()
	if !ok {
		return nil, false
	}
	tail, err := readTail(path, max)
	if err != nil {
		return nil, false
	}
	return tail, true
}

// path locates the transcript, caching the hit. Not found means the CLI has
// expired it: Claude Code prunes transcripts on a retention window (30 days by
// default), long before a kovan agent's worktree and notes go anywhere.
func (c convo) path() (string, bool) {
	if c.id == "" {
		return "", false
	}
	if p, ok := transcriptPaths.Load(c.id); ok {
		if _, err := os.Stat(p.(string)); err == nil {
			return p.(string), true
		}
		transcriptPaths.Delete(c.id)
	}
	matches, err := filepath.Glob(c.d.TranscriptGlob(c.home, c.id))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	transcriptPaths.Store(c.id, matches[0])
	return matches[0], true
}

// exists reports whether an agent still has a conversation to resume.
func (c convo) exists() bool {
	_, ok := c.path()
	return ok
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

// digest renders the tail of the conversation as short labeled lines, real
// turns only. Text sitting unsent in the agent's input box never reaches the
// transcript, so it cannot leak into a summary. Empty when there is no
// transcript (or nothing summarizable yet).
func (c convo) digest() string {
	tail, ok := c.tail(digestTailBytes)
	if !ok {
		return ""
	}
	return c.d.Digest(tail)
}
