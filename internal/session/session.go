// Package session is the source of truth for a running agent (a "tab"): a
// manifest file under ~/.kovan/sessions, one per tab, keyed by the tab's unique
// tmux session name. Many tabs can share one worktree (a "workspace"), so the
// manifest is keyed by the tab, not the worktree. The board reads every manifest
// in the index; gates resolve the acting tab by its Claude session id.
package session

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boratanrikulu/kovan/internal/config"
	"gopkg.in/yaml.v3"
)

// NewID returns a random UUIDv4, used as the agent's Claude session id so its
// transcript lands at a known ~/.claude/projects/*/<id>.jsonl path.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Manifest records everything the board and the lifecycle commands need to
// know about one agent.
type Manifest struct {
	ID           string    `yaml:"id"`
	Title        string    `yaml:"title"`
	Repo         string    `yaml:"repo"`
	RepoRoot     string    `yaml:"repo_root"`
	Worktree     string    `yaml:"worktree"`
	Branch       string    `yaml:"branch"`
	Base         string    `yaml:"base"`
	InPlace      bool      `yaml:"in_place,omitempty"` // the worktree IS the repo checkout: no separate dir, no new branch
	Tmux         string    `yaml:"tmux"`
	Agent        string    `yaml:"agent"`
	Account      string    `yaml:"account,omitempty"`    // kovan's account label, never the token
	SessionID    string    `yaml:"session_id,omitempty"` // Claude session id; locates the transcript
	State        string    `yaml:"state"`                // working | needs-you | idle, kept live by the gate hook
	Archived     bool      `yaml:"archived,omitempty"`   // set aside: tmux killed, worktree kept, hidden from the default board
	Pinned       bool      `yaml:"pinned,omitempty"`     // kept at the top of the board; pure metadata, no lifecycle effect
	Color        string    `yaml:"color,omitempty"`      // board row tint, a palette name (red, blue, …); pure tagging
	Mode         string    `yaml:"mode,omitempty"`       // hook-seen permission mode; the board prefers the transcript's
	TaskMode     string    `yaml:"task_mode,omitempty"`  // the task's working style: code | review | analyze | write | …; the gates resolve posture and write paths live from it
	Effort       string    `yaml:"effort,omitempty"`
	Summary      string    `yaml:"summary,omitempty"`    // last monitor summary; anything scanning the index reads it without a tmux capture
	SummaryAt    time.Time `yaml:"summary_at,omitempty"` // when that summary was generated
	LastActivity time.Time `yaml:"last_activity,omitempty"`
	CreatedAt    time.Time `yaml:"created_at"`
}

// manifestRel is the legacy pre-tabs manifest path, one per worktree. loadEntry
// reads it once to migrate an old session into the index-resident layout.
const manifestRel = ".kovan/session.yaml"

// manifestFile is where a tab's manifest lives: index-resident, keyed by its
// unique tmux session name, so one worktree can host many tabs.
func manifestFile(dir, tmux string) string {
	return filepath.Join(dir, tmux+".yaml")
}

// Write persists the manifest into the index, keyed by the tab's tmux name. It
// is written atomically (temp + rename) because the gate hook updates it
// concurrently with the board reading it.
func (m *Manifest) Write() error {
	dir, err := indexDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session index: %w", err)
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return writeAtomic(manifestFile(dir, m.Tmux), data)
}

// ReadByTmux loads a tab's manifest from the index by its tmux session name.
func ReadByTmux(tmux string) (*Manifest, error) {
	dir, err := indexDir()
	if err != nil {
		return nil, err
	}
	return loadEntry(dir, tmux)
}

// loadEntry reads a tab's manifest by tmux name, transparently migrating a
// pre-tabs entry (an index pointer file holding a worktree path, with the
// manifest inside the worktree) to the index-resident layout on first read.
//
// Migration only ever *writes* the index-resident copy; it never removes the
// legacy pointer here. List() runs concurrently from every agent's gate hook, so
// a delete on this path would race a sibling that has the file mid-flight. The
// inert legacy pointer is retired safely by List once the .yaml is in place.
func loadEntry(dir, tmux string) (*Manifest, error) {
	if data, err := os.ReadFile(manifestFile(dir, tmux)); err == nil {
		// The .yaml is canonical and confirmed present, so retiring any leftover
		// legacy pointer of the same name can't lose data, even under concurrency.
		_ = os.Remove(filepath.Join(dir, tmux))
		return unmarshalManifest(data)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	legacy := filepath.Join(dir, tmux)
	wt, err := os.ReadFile(legacy)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(string(trimNewline(wt)), manifestRel))
	if err != nil {
		return nil, err
	}
	m, err := unmarshalManifest(data)
	if err != nil {
		return nil, err
	}
	_ = m.Write() // adopt the index-resident layout
	return m, nil
}

func unmarshalManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// FindFrom returns the tab whose worktree contains dir, choosing the deepest
// match. found is false when no kovan worktree contains dir, which the git-hook
// gate treats as a non-kovan checkout to leave alone. Tabs sharing a worktree
// share its branch and approval markers, so any one of them is the right answer
// for the git hook; the PreToolUse gate resolves the acting tab by session id.
func FindFrom(dir string) (m *Manifest, found bool, err error) {
	all, err := List()
	if err != nil {
		return nil, false, err
	}
	for _, c := range all {
		if c.Worktree == "" {
			continue
		}
		if dir == c.Worktree || strings.HasPrefix(dir, c.Worktree+string(filepath.Separator)) {
			if m == nil || len(c.Worktree) > len(m.Worktree) {
				m = c
			}
		}
	}
	return m, m != nil, nil
}

// FindBySessionID returns the tab with the given Claude session id, used by the
// gate hook to resolve the acting tab even when several share a worktree.
func FindBySessionID(id string) (m *Manifest, found bool, err error) {
	if id == "" {
		return nil, false, nil
	}
	all, err := List()
	if err != nil {
		return nil, false, err
	}
	for _, c := range all {
		if c.SessionID == id {
			return c, true, nil
		}
	}
	return nil, false, nil
}

// List returns every tab's manifest from the index. It is a read path: it never
// deletes an index-resident manifest, because every agent's gate hook calls List
// concurrently and a delete here would race a sibling mid-write. The only file it
// retires is a dead *legacy* pointer — one with no .yaml and an unreadable
// worktree manifest — which no concurrent migration can be resurrecting.
func List() ([]*Manifest, error) {
	dir, err := indexDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session index: %w", err)
	}
	seen := map[string]bool{}
	var out []*Manifest
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue // skip in-flight atomic writes
		}
		tmux := strings.TrimSuffix(e.Name(), ".yaml")
		if seen[tmux] {
			continue // a migrating entry may briefly have both <tmux> and <tmux>.yaml
		}
		seen[tmux] = true
		m, err := loadEntry(dir, tmux)
		if err != nil {
			// Unreadable: an index-resident .yaml is self-contained, so only a
			// legacy pointer fails here. Retire it iff no .yaml exists for it, so a
			// concurrently-migrated entry is never deleted.
			if _, statErr := os.Stat(manifestFile(dir, tmux)); os.IsNotExist(statErr) {
				os.Remove(filepath.Join(dir, tmux))
			}
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Pointer returns the worktree path of the tab registered under a tmux name. It
// resolves a tab even if its worktree was deleted, so the lifecycle commands can
// still clean it up. ok is false when no such tab exists.
func Pointer(name string) (worktree string, ok bool, err error) {
	m, err := ReadByTmux(name)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return m.Worktree, true, nil
}

// RemovePointer deletes a tab's index entry (and any not-yet-migrated legacy
// pointer of the same name).
func RemovePointer(name string) error {
	dir, err := indexDir()
	if err != nil {
		return err
	}
	for _, p := range []string{manifestFile(dir, name), filepath.Join(dir, name)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// writeAtomic writes data to path via a temp file and rename, so a concurrent
// reader never sees a half-written manifest.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "session-*.tmp")
	if err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func indexDir() (string, error) {
	home, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sessions"), nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
