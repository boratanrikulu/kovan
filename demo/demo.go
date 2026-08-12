// Package demo seeds a self-contained kovan world: a throwaway KOVAN_HOME
// full of fake agents, tiny git repos, and dummy tmux sessions. It exists so
// the cockpit can be walked without a Claude account, tokens, or a checkout.
package demo

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/boratanrikulu/kovan/internal/session"
)

// assets are the demo world's static parts: the KOVAN_HOME skeleton (method
// layers, modes, project contexts, task docs) and the pane tails the fake
// tmux sessions print.
//
//go:embed all:files panes
var assets embed.FS

// tmuxPrefix names every session the demo creates, so teardown can find them
// without touching a real agent's session.
const tmuxPrefix = "kovan-demo-"

// repos are the throwaway checkouts the fake agents are attached to.
var repos = []string{"bpfvet", "durdur", "gecit", "gobee", "quik", "vault", "website"}

// agent is one row on the demo board.
type agent struct {
	id, title, repo, branch string
	inPlace                 bool
	account                 string
	state, color            string
	mode, taskMode          string
	pinned, archived        bool
	createdMin, activityMin int
	summary                 string
}

var agents = []agent{
	{
		id:          "ipv6",
		title:       "handle IPv6 flows on the sock_ops path",
		repo:        "gecit",
		branch:      "feat/ipv6-sock-ops",
		account:     "personal",
		state:       "working",
		color:       "cyan",
		mode:        "auto",
		taskMode:    "code",
		pinned:      true,
		createdMin:  130,
		activityMin: 2,
		summary:     "The agent is wiring IPv6 tuple extraction into the sock_ops redirect path; parsing and map plumbing are done and it is heading into the integration tests. Nothing needs you.",
	},
	{
		id:          "mapiter",
		title:       "fix verifier rejection on map iteration",
		repo:        "gobee",
		branch:      "fix/map-iteration-verifier",
		account:     "personal",
		state:       "needs-you",
		color:       "orange",
		mode:        "auto",
		taskMode:    "code",
		pinned:      true,
		createdMin:  300,
		activityMin: 18,
		summary:     "It reproduced the verifier rejection and has two candidate codegen shapes for the map iteration; it needs you to pick between the bounded-loop rewrite and the callback form before it goes on.",
	},
	{
		id:          "featreq",
		title:       "review: feature-requirement extraction",
		repo:        "bpfvet",
		branch:      "review/feature-requirements",
		account:     "personal",
		state:       "idle",
		mode:        "default",
		taskMode:    "review",
		createdMin:  65,
		activityMin: 25,
		summary:     "It finished the review and wrote the findings table to review.md, two medium and one low. Idle until you want the findings discussed or posted.",
	},
	{
		id:          "tun-qa",
		title:       "verify TUN device setup on macOS",
		repo:        "gecit",
		branch:      "qa/tun-macos",
		account:     "personal",
		state:       "working",
		color:       "green",
		mode:        "auto",
		taskMode:    "qa",
		createdMin:  45,
		activityMin: 1,
		summary:     "It is working through the TUN setup matrix on macOS, three of five checks green so far; route install and teardown checks still to run. Nothing needs you yet.",
	},
	{
		id:          "o2",
		title:       "why -O2 output fails the verifier",
		repo:        "gobee",
		branch:      "analyze/o2-verifier",
		state:       "idle",
		mode:        "auto",
		taskMode:    "analyze",
		createdMin:  1560,
		activityMin: 55,
		summary:     "It answered the question in analysis.md, the -O2 output spills the bounds-checked register so the verifier loses the proven range, with instruction-level evidence. Idle, waiting for you to read it.",
	},
	{
		id:          "relnotes",
		title:       "draft release notes for v0.2",
		repo:        "durdur",
		branch:      "write/v0.2-notes",
		account:     "personal",
		state:       "needs-you",
		color:       "yellow",
		mode:        "default",
		taskMode:    "write",
		createdMin:  185,
		activityMin: 35,
		summary:     "The draft of the v0.2 release notes is ready in draft.md; it needs a voice check from you on the opening paragraph before it calls the text final.",
	},
	{
		id:          "pion",
		title:       "spike: bump pion, revive the demo room",
		repo:        "quik",
		branch:      "spike/pion-bump",
		state:       "idle",
		mode:        "auto",
		taskMode:    "code",
		createdMin:  2900,
		activityMin: 2600,
		summary:     "It mapped the pion API changes and sketched the upgrade path, then stopped before reviving the demo room.",
	},
	{
		id:          "weekly",
		title:       "weekly review",
		repo:        "vault",
		branch:      "main",
		inPlace:     true,
		account:     "personal",
		state:       "idle",
		mode:        "auto",
		taskMode:    "mentor",
		createdMin:  480,
		activityMin: 90,
		summary:     "It read the week's notes and left a dated check-in in the vault. The agent is idle; nothing needs you.",
	},
	{
		id:          "budget",
		title:       "monthly budget report",
		repo:        "vault",
		branch:      "main",
		inPlace:     true,
		account:     "personal",
		state:       "needs-you",
		color:       "blue",
		mode:        "auto",
		taskMode:    "finance",
		pinned:      true,
		createdMin:  1470,
		activityMin: 40,
		summary:     "The monthly budget report is drafted; one recurring subscription looks unused and it needs a keep-or-cancel call from you to finish.",
	},
	{
		id:          "photos",
		title:       "photo post: pick, caption, publish",
		repo:        "website",
		branch:      "publish/photo-post",
		account:     "personal",
		state:       "working",
		mode:        "auto",
		taskMode:    "publish",
		createdMin:  30,
		activityMin: 3,
		summary:     "It is resizing the shortlisted photos for the post; captions and the publish step come next. Nothing needs you.",
	},
	{
		id:          "core-checks",
		title:       "add CO-RE relocation checks",
		repo:        "bpfvet",
		branch:      "feat/core-reloc-checks",
		account:     "personal",
		state:       "idle",
		mode:        "auto",
		taskMode:    "code",
		archived:    true,
		createdMin:  4300,
		activityMin: 4200,
		summary:     "It shipped the CO-RE relocation checks and the suite is green; the task is done and archived.",
	},
}

// Seed builds the demo world under root and returns its KOVAN_HOME. It reads
// KOVAN_HOME from the environment through the session package, so the caller
// must point that at the returned home before writing anything else.
func Seed(root string) (string, error) {
	if err := Teardown(root); err != nil {
		return "", err
	}
	home := filepath.Join(root, "home")
	if err := copyTree("files", home); err != nil {
		return "", err
	}
	if err := writeToken(home); err != nil {
		return "", err
	}
	if err := writeConfig(home); err != nil {
		return "", err
	}
	for _, r := range repos {
		if err := makeRepo(filepath.Join(root, "repos", r), r); err != nil {
			return "", err
		}
	}
	os.Setenv("KOVAN_HOME", home)
	for _, a := range agents {
		if err := seedAgent(root, a); err != nil {
			return "", err
		}
	}
	return home, nil
}

// Teardown removes everything Seed created and nothing else.
func Teardown(root string) error {
	for _, s := range tmuxSessions() {
		exec.Command("tmux", "kill-session", "-t", s).Run()
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove demo root: %w", err)
	}
	return nil
}

// Home is the KOVAN_HOME a seeded root exposes.
func Home(root string) string { return filepath.Join(root, "home") }

// Cockpit is the directory to launch the board from, a repo the demo owns so
// the board opens on a real checkout.
func Cockpit(root string) string { return filepath.Join(root, "repos", "gecit") }

func seedAgent(root string, a agent) error {
	repoRoot := filepath.Join(root, "repos", a.repo)
	worktree := repoRoot
	if !a.inPlace {
		worktree = filepath.Join(root, "worktrees", a.id)
		if err := run(repoRoot, "git", "worktree", "add", "-q", worktree, "-b", a.branch); err != nil {
			return err
		}
	}
	if err := a.manifest(repoRoot, worktree).Write(); err != nil {
		return err
	}
	return a.startPane(root)
}

func (a agent) manifest(repoRoot, worktree string) *session.Manifest {
	now := time.Now().UTC()
	return &session.Manifest{
		ID:           a.id,
		Title:        a.title,
		Repo:         a.repo,
		RepoRoot:     repoRoot,
		Worktree:     worktree,
		Branch:       a.branch,
		Base:         "main",
		InPlace:      a.inPlace,
		Tmux:         tmuxPrefix + a.id,
		Agent:        "claude",
		Account:      a.account,
		State:        a.state,
		Archived:     a.archived,
		Pinned:       a.pinned,
		Color:        a.color,
		Mode:         a.mode,
		TaskMode:     a.taskMode,
		Summary:      a.summary,
		SummaryAt:    now,
		LastActivity: now.Add(-time.Duration(a.activityMin) * time.Minute),
		CreatedAt:    now.Add(-time.Duration(a.createdMin) * time.Minute),
	}
}

// startPane runs a dummy tmux session that prints the agent's pane tail, so
// the board's peek shows something real. An agent with no pane file is
// deliberately session-less: the board renders it as stopped or archived.
func (a agent) startPane(root string) error {
	tail, err := assets.ReadFile("panes/" + a.id + ".txt")
	if err != nil {
		return nil
	}
	path := filepath.Join(root, "panes", a.id+".txt")
	if err := writeFile(path, tail, 0o644); err != nil {
		return err
	}
	cmd := fmt.Sprintf("cat %s; sleep 86400", shellQuote(path))
	return run("", "tmux", "new-session", "-d", "-s", tmuxPrefix+a.id, cmd)
}

func makeRepo(dir, name string) error {
	if err := run("", "git", "init", "-q", "-b", "main", dir); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "README.md"),
		[]byte(fmt.Sprintf("# %s\n\ndemo checkout for the kovan board.\n", name)), 0o644); err != nil {
		return err
	}
	if err := commit(dir, "docs: readme"); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "internal", "doc.go"), []byte("package internal\n"), 0o644); err != nil {
		return err
	}
	return commit(dir, "chore: scaffold")
}

// commit stages everything and commits with a fixed identity, so the demo does
// not depend on (or disturb) the user's git config.
func commit(dir, message string) error {
	if err := run(dir, "git", "add", "-A"); err != nil {
		return err
	}
	return run(dir, "git",
		"-c", "user.name=Demo",
		"-c", "user.email=demo@example.com",
		"-c", "commit.gpgsign=false",
		"commit", "-qm", message)
}

func writeToken(home string) error {
	return writeFile(filepath.Join(home, "tokens", "personal"), []byte("demo-token-not-real\n"), 0o600)
}

func writeConfig(home string) error {
	body := fmt.Sprintf(`runner: tmux
agent: claude
notify: macos
accounts:
  personal: { token_file: %s }
default_account: personal
`, filepath.Join(home, "tokens", "personal"))
	return writeFile(filepath.Join(home, "config.yaml"), []byte(body), 0o644)
}

// copyTree unpacks an embedded directory to dst, preserving structure.
func copyTree(src, dst string) error {
	return fs.WalkDir(assets, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dst, rel), body, 0o644)
	})
}

func writeFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxSessions lists the demo's own tmux sessions, ignoring a missing server.
func tmuxSessions() []string {
	out, err := exec.Command("tmux", "ls", "-F", "#S").Output()
	if err != nil {
		return nil
	}
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, tmuxPrefix) {
			found = append(found, line)
		}
	}
	return found
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
