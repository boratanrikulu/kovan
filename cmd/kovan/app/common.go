package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
)

// launchMode selects how the agent process is started in its worktree.
type launchMode int

const (
	launchFresh  launchMode = iota // hand the agent its goal as the opening prompt
	launchResume                   // reconnect the agent to its existing conversation
)

// agentCommand builds the shell command that launches the agent. Both forms
// live here so that swapping the agent tool stays a single-site change. addDir
// grants the agent access to its task docs (which live outside the worktree); a
// "--" guards the positional prompt from the variadic --add-dir flag.
func agentCommand(agent, prompt string, mode launchMode, addDir, sessionID string) string {
	add := ""
	if addDir != "" {
		add = " --add-dir " + shellQuote(addDir)
	}
	// Resume targets the tab's own conversation by id: --continue picks the most
	// recent conversation in the cwd, which is a sibling's when tabs share a
	// worktree. --resume reuses the id, so it stays valid across wakes.
	if mode == launchResume {
		if sessionID != "" {
			return agent + add + " --resume " + shellQuote(sessionID)
		}
		return agent + add + " --continue"
	}
	sid := ""
	if sessionID != "" {
		sid = " --session-id " + shellQuote(sessionID)
	}
	return agent + add + sid + " -- " + shellQuote(prompt)
}

// runnerSession builds the detached session that launches the agent, applying
// the user's tmux options and the manifest's account token, and granting access
// to the agent's durable task-doc dir. prompt is the fresh-start opening prompt;
// it is ignored on the wake/resume path.
func runnerSession(m *session.Manifest, mode launchMode, prompt string) (runner.Session, error) {
	global, err := config.LoadGlobal()
	if err != nil {
		return runner.Session{}, err
	}
	tokenFile, err := accountTokenFile(global, m.Account)
	if err != nil {
		return runner.Session{}, err
	}
	notes, err := taskDocsFor(m)
	if err != nil {
		return runner.Session{}, err
	}
	// kovan's branding goes last so it wins over any user status options.
	opts := append(append([]string{}, global.Tmux.Options...), statusBarOptions(m)...)
	return runner.Session{
		Name:    m.Tmux,
		Title:   fmt.Sprintf("%s: %s", m.ID, m.Title), // tmux window + terminal tab
		Dir:     m.Worktree,
		Cmd:     launchCommand(m.Agent, prompt, mode, tokenFile, notes, m.SessionID),
		Options: opts,
	}, nil
}

// statusBarOptions brands the tmux status bar with the agent's static identity:
// a "kovan" badge plus the id on the left, and repo · branch · title on the
// right. It is static — the board owns the live state/age columns — and
// session-scoped, so the user's own tmux config is untouched.
func statusBarOptions(m *session.Manifest) []string {
	identity := joinNonEmpty(" · ", tmuxEscape(m.Repo), tmuxEscape(m.Branch), tmuxEscape(truncate(m.Title, 40)))
	// A dim, low-emphasis reminder of the app menu and how to detach back to the
	// board. Assumes tmux's default prefix (C-b); a user who remapped it knows
	// their binding.
	hint := fmt.Sprintf("#[fg=colour%s]^b k apps · ^b d → kovan#[default]", string(colorDim))
	right := hint
	if identity != "" {
		right = identity + "  " + hint
	}
	ink := string(colorInk)
	return []string{
		fmt.Sprintf("status-style bg=colour%s,fg=colour250", ink),
		"status-left-length 64",
		"status-right-length 160",
		// The gold "kovan" chip — same mark as the board's brandMark().
		fmt.Sprintf("status-left #[bg=colour%s,fg=colour%s,bold] kovan #[default] %s ", string(colorBrand), ink, tmuxEscape(m.ID)),
		fmt.Sprintf("status-right %s ", right),
	}
}

// tmuxEscape doubles '#' so a literal '#' in a dynamic value is not read as the
// start of a tmux format directive (#[...], #H, …).
func tmuxEscape(s string) string {
	return strings.ReplaceAll(s, "#", "##")
}

// joinNonEmpty joins the non-empty parts with sep, so an absent field leaves no
// stray separator.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// taskDocsDir is an agent's durable task-doc directory, under the repo's kovan
// projects dir and keyed by task.dir + id. It lives outside the worktree, so
// the notes survive worktree removal by design.
func taskDocsDir(home, repo, taskDir, id string) string {
	return filepath.Join(home, "projects", repo, taskDir, id)
}

// taskDocsFor resolves taskDocsDir from a manifest, loading the repo's task.dir.
func taskDocsFor(m *session.Manifest) (string, error) {
	home, err := config.Dir()
	if err != nil {
		return "", err
	}
	repoCfg, err := config.LoadRepo(m.RepoRoot)
	if err != nil {
		return "", err
	}
	return taskDocsDir(home, m.Repo, repoCfg.Task.Dir, m.ID), nil
}

// openAction is how `open` should recover a session, given whether its tmux
// session is alive and whether its worktree manifest is still present.
type openAction int

const (
	actionOpen    openAction = iota // alive: attach to it
	actionWake                      // dead but worktree present: relaunch, then attach
	actionMissing                   // worktree gone: nothing to attach to
)

func decideOpen(alive, worktreePresent bool) openAction {
	switch {
	case alive:
		return actionOpen
	case worktreePresent:
		return actionWake
	default:
		return actionMissing
	}
}

// resolveSession finds an agent's tmux name and recorded worktree path by id.
// Inside a repo it resolves deterministically from the naming scheme, which
// still locates a session whose worktree has been deleted; otherwise it falls
// back to scanning the index.
func resolveSession(id string) (name, worktree string, err error) {
	if repo, e := openRepo(); e == nil {
		name = sessionName(filepath.Base(repo.Root), id)
		wt, ok, e := session.Pointer(name)
		if e != nil {
			return "", "", e
		}
		if ok {
			return name, wt, nil
		}
	}
	m, e := findSession(id)
	if e != nil {
		return "", "", e
	}
	return m.Tmux, m.Worktree, nil
}

// shellQuote single-quotes s for safe interpolation into a shell command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// project is a repo the new-agent form can target: a display name and its root.
type project struct {
	name string
	root string
}

// knownProjects lists the repos the new-agent form can spawn into: the repo
// containing the current directory first (the common "spawn here" case, so it
// needs no picking), then the distinct repos of existing agents. A brand-new
// repo not in this list is reachable by typing its path in the picker.
func knownProjects() []project {
	seen := map[string]bool{}
	var projects []project
	add := func(name, root string) {
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		projects = append(projects, project{name: name, root: root})
	}
	if repo, err := openRepo(); err == nil {
		add(filepath.Base(repo.Root), repo.Root)
	}
	if manifests, err := session.List(); err == nil {
		for _, m := range manifests {
			add(m.Repo, m.RepoRoot)
		}
	}
	return projects
}

// branchesFor lists the branches of the repo at root, or nil if it isn't a
// (readable) git repo — the from-picker just shows "(default)" then.
func branchesFor(root string) []string {
	if repo, err := git.Open(root); err == nil {
		return repo.Branches()
	}
	return nil
}

// openRepo opens the git repository containing the current directory.
func openRepo() (*git.Repo, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return git.Open(wd)
}

// newRunner returns the configured runner. tmux is the only one today.
func newRunner(name string) (runner.Runner, error) {
	switch name {
	case "", "tmux":
		return runner.Tmux{}, nil
	default:
		return nil, fmt.Errorf("unknown runner %q", name)
	}
}

// resolveAuthor picks the branch-template author: the global config's author,
// else the repo's git user.name, else the OS username, slugified so it is safe
// in a branch name.
func resolveAuthor(global *config.Global, repo *git.Repo) string {
	candidate := global.Author
	if candidate == "" {
		candidate = repo.UserName()
	}
	if candidate == "" {
		if u, err := user.Current(); err == nil {
			candidate = u.Username
		}
	}
	if a := slugify(candidate); a != "" {
		return a
	}
	return "agent"
}

// sessionName builds the multiplexer session name for an agent: a repo-scoped,
// collision-free identifier across all projects.
func sessionName(repo, id string) string {
	return runner.SessionName(fmt.Sprintf("kovan-%s-%s", repo, id))
}

// worktreePath is the worktree directory: a sibling of the repo named
// <prefix>-<id>, matching the layout of the reference scripts.
func worktreePath(repoRoot, prefix, id string) string {
	return filepath.Join(filepath.Dir(repoRoot), prefix+"-"+id)
}

// renderBranch fills a branch_template with the author, id, and slug.
func renderBranch(tmpl, author, id, slug string) string {
	r := strings.NewReplacer(
		"{author}", author,
		"{id}", id,
		"{slug}", slug,
	)
	return r.Replace(tmpl)
}

// findSession resolves an agent by id. When run inside a repository, a match
// in that repo wins; otherwise the id must be unique across all projects.
func findSession(id string) (*session.Manifest, error) {
	all, err := session.List()
	if err != nil {
		return nil, err
	}
	if repo, err := openRepo(); err == nil {
		for _, m := range all {
			if m.ID == id && m.RepoRoot == repo.Root {
				return m, nil
			}
		}
	}
	var matches []*session.Manifest
	for _, m := range all {
		if m.ID == id {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no agent with id %q (see `kovan status`)", id)
	case 1:
		return matches[0], nil
	default:
		var repos []string
		for _, m := range matches {
			repos = append(repos, m.Repo)
		}
		return nil, fmt.Errorf("id %q exists in multiple repos (%s); run from inside the one you mean", id, strings.Join(repos, ", "))
	}
}

// resolveMode picks the task mode by precedence: an explicit flag, then the
// repo's default, then the global default, then the built-in default.
func resolveMode(flag, repoDefault, globalDefault string) string {
	switch {
	case flag != "":
		return flag
	case repoDefault != "":
		return repoDefault
	case globalDefault != "":
		return globalDefault
	default:
		return mode.Default
	}
}

// resolveID returns the id to use. A blank id always gets a short generated one,
// even when the repo sets an id_pattern: leaving it blank is an explicit opt-in
// to an auto id, and the pattern only constrains ids you actually type. A typed
// id is validated against the pattern when one is set.
func resolveID(id, idPattern string) (string, error) {
	if id == "" {
		return generateID()
	}
	if idPattern != "" {
		ok, err := regexp.MatchString(idPattern, id)
		if err != nil {
			return "", fmt.Errorf("invalid id_pattern %q: %w", idPattern, err)
		}
		if !ok {
			return "", fmt.Errorf("id %q does not match id_pattern %q", id, idPattern)
		}
	}
	return id, nil
}

// generateID returns a short, branch- and path-safe id for an agent started
// without one: four hex characters from crypto/rand (e.g. "a3f9"). Enough to
// disambiguate the handful of agents on the board without being a mouthful.
func generateID() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

var nonSlug = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// slugify turns a free-form title into a branch-safe slug: spaces to hyphens,
// everything else outside [a-zA-Z0-9_-] dropped.
func slugify(s string) string {
	s = strings.ReplaceAll(s, " ", "-")
	s = nonSlug.ReplaceAllString(s, "")
	return strings.Trim(s, "-")
}

// currentBranch returns the worktree's checked-out branch, or "" on a detached
// HEAD or error.
func currentBranch(worktree string) string {
	out, err := exec.Command("git", "-C", worktree, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
