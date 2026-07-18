package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/session"
)

// boardRow is one agent's row on the board, shared by `status` and the TUI.
type boardRow struct {
	State     string
	Perm      string // permission mode (auto/default/plan/bypass), from the transcript
	Mode      string // task mode (code/review/analyze/write)
	ID        string
	Repo      string
	Account   string
	Age       string
	Branch    string
	Title     string
	Tmux      string
	Worktree  string    // worktree root, for board apps (editor/git GUI); not rendered
	Cont      bool      // a sibling tab sharing the row above's worktree (a workspace continuation)
	SessionID string    // Claude session id; locates the transcript the summarizer digests
	Summary   string    // last persisted monitor summary, seeds the cockpit's cache
	SummaryAt time.Time // when that summary was generated
	Pinned    bool      // kept at the top of the board
	Color     string    // the row's stripe color, a palette name
}

// loadBoard reads every agent's manifest and resolves its live state.
func loadBoard() ([]boardRow, error) {
	manifests, err := session.List()
	if err != nil {
		return nil, err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}
	run, err := newRunner(global.Runner)
	if err != nil {
		return nil, err
	}
	// One Sessions call answers liveness for every agent; per-agent Exists
	// checks would spawn a subprocess each on every board refresh.
	alive := map[string]bool{}
	if names, err := run.Sessions(); err == nil {
		for _, n := range names {
			alive[n] = true
		}
	}
	return assembleBoard(manifests, func(name string) bool {
		return alive[name]
	}), nil
}

// assembleBoard turns manifests into board rows. Tabs sharing a worktree are
// clustered together (the workspace), groups ordered newest-first, and within a
// group the first-in (owner) leads its sibling tabs. State comes from the
// injected liveness check so the assembly is testable without a runner.
func assembleBoard(manifests []*session.Manifest, alive func(name string) bool) []boardRow {
	sortClustered(manifests)
	rows := make([]boardRow, 0, len(manifests))
	for _, m := range manifests {
		// Archived agents are set aside (tmux killed, worktree kept); otherwise a
		// dead tmux session is stopped and a live one trusts the manifest's live
		// state (working before the first hook fires).
		state := "stopped"
		switch {
		case m.Archived:
			state = "archived"
		case alive(m.Tmux):
			if state = m.State; state == "" {
				state = "working"
			}
		}
		// An archived agent's permission mode is frozen, so it skips the
		// transcript scan and shows the last hook-seen mode.
		perm := mapMode(m.Mode)
		if !m.Archived {
			perm = permMode(m)
		}
		rows = append(rows, boardRow{
			State:     state,
			Perm:      perm,
			Mode:      m.TaskMode,
			ID:        m.ID,
			Repo:      m.Repo,
			Account:   m.Account,
			Age:       age(m.CreatedAt),
			Branch:    m.Branch,
			Title:     m.Title,
			Tmux:      m.Tmux,
			Worktree:  m.Worktree,
			SessionID: m.SessionID,
			Pinned:    m.Pinned,
			Color:     m.Color,
			Summary:   m.Summary,
			SummaryAt: m.SummaryAt,
		})
	}
	return rows
}

// sortClustered orders manifests so a workspace's tabs are adjacent: pinned
// groups lead (a group is pinned when any of its tabs is, so a pin lifts the
// whole cluster and the └ marker's adjacency holds), groups (by worktree) are
// ranked by their newest member so they stay roughly newest-first, and within
// a group the oldest (the first-in/owner) leads.
func sortClustered(manifests []*session.Manifest) {
	// Group by worktree; an agent with no worktree is its own group so unrelated
	// agents still sort newest-first. A workspace's archived tabs form their own
	// group: active and archived are separate board views, so an agent must rank
	// by its own view's pins and recency, not a worktree-mate's on the other tab.
	key := func(m *session.Manifest) string {
		k := m.Worktree
		if k == "" {
			k = "\x00" + m.Tmux
		}
		if m.Archived {
			return "archived\x00" + k
		}
		return k
	}
	newest := map[string]time.Time{}
	pinned := map[string]bool{}
	for _, m := range manifests {
		if t, ok := newest[key(m)]; !ok || m.CreatedAt.After(t) {
			newest[key(m)] = m.CreatedAt
		}
		if m.Pinned {
			pinned[key(m)] = true
		}
	}
	sort.SliceStable(manifests, func(i, j int) bool {
		a, b := manifests[i], manifests[j]
		if key(a) != key(b) {
			if pinned[key(a)] != pinned[key(b)] {
				return pinned[key(a)]
			}
			return newest[key(a)].After(newest[key(b)])
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}

// filterRows screens the board for one view: the active view keeps non-archived
// agents, the archived view keeps only archived ones (they are separate tabs).
// When query is non-empty, only rows matching it (a case-insensitive substring
// across the visible fields) are kept. It marks workspace continuations on the
// final list, so a tab is flagged only when its worktree-mate is the row above
// it in the same view. Shared by the cockpit and `kovan status`.
func filterRows(rows []boardRow, query string, archivedOnly bool) []boardRow {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]boardRow, 0, len(rows))
	for _, r := range rows {
		if (r.State == "archived") != archivedOnly {
			continue // wrong tab for this row
		}
		if q != "" && !rowMatches(r, q) {
			continue
		}
		r.Cont = r.Worktree != "" && len(out) > 0 && out[len(out)-1].Worktree == r.Worktree
		out = append(out, r)
	}
	return out
}

// rowMatches reports whether any of a row's visible fields contains q (lowercased).
func rowMatches(r boardRow, q string) bool {
	for _, f := range []string{r.State, r.Mode, r.Account, r.ID, r.Repo, r.Branch, r.Title} {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// archivedCount is how many rows are archived, for the board header.
func archivedCount(rows []boardRow) int {
	n := 0
	for _, r := range rows {
		if r.State == "archived" {
			n++
		}
	}
	return n
}

// orDash renders an empty field as a dash for table legibility.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func age(t time.Time) string {
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// truncate shortens s to n display columns (runes), ending with an ellipsis.
// It counts runes, not bytes, so multi-byte glyphs like the workspace tree
// marker "└" don't throw column alignment off.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n < 1 {
		return "…"
	}
	return string([]rune(s)[:n-1]) + "…"
}
