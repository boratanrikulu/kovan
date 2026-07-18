package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/mode"
	"github.com/boratanrikulu/kovan/internal/runner"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/boratanrikulu/kovan/internal/taskdoc"
	"github.com/spf13/cobra"
)

var (
	startFrom    string
	startAccount string
	startMode    string
	startQuick   bool
	startInPlace bool
	startIn      string
)

var startCmd = &cobra.Command{
	Use:     "start <id> <title>",
	Aliases: []string{"go"},
	Short:   "Start an agent: scaffold task docs, brief it, launch it",
	Long: `Create a git worktree for <id>, scaffold its task docs, open context.md so you
can write the brief, then launch the agent pointed at it. With --quick, skip the
editor and launch with the title as the goal. Then walk away; the board tells you
when it needs you or is done.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart(args[0], args[1])
	},
}

func init() {
	startCmd.Flags().StringVar(&startFrom, "from", "", "base branch for the new worktree (with --in-place, the branch to work on); overrides config and autodetect")
	startCmd.Flags().StringVar(&startAccount, "account", "", "Claude account to run the agent under (see accounts in config)")
	startCmd.Flags().StringVar(&startMode, "mode", "", "task mode: code | review | analyze | write | … (overrides the repo default)")
	startCmd.Flags().BoolVar(&startQuick, "quick", false, "skip the brief editor; launch with the title as the goal")
	startCmd.Flags().BoolVar(&startInPlace, "in-place", false, "run in this checkout on the --from branch (a second agent here joins as a tab), no separate worktree")
	startCmd.Flags().StringVar(&startIn, "in", "", "run as a tab in an existing agent's worktree (its branch is inherited); pass that agent's id")
}

func runStart(id, title string) error {
	repo, err := openRepo()
	if err != nil {
		return err
	}
	spec, err := scaffoldAgent(repo.Root, id, title, startFrom, startAccount, startMode, "", startInPlace, startIn, briefInput{})
	if err != nil {
		return err
	}
	if !startQuick {
		if err := runEditor(spec.briefPath); err != nil {
			spec.rollback()
			return err
		}
	}
	res, err := launchScaffolded(spec, startQuick)
	if err != nil {
		return err
	}
	m := res.Manifest
	switch {
	case res.Tab:
		fmt.Printf("Running as a tab in %s on branch %s\n", m.Worktree, m.Branch)
	case m.InPlace:
		fmt.Printf("Working in this checkout on branch %s (no separate worktree)\n", m.Branch)
	case res.Reused:
		fmt.Printf("Reusing existing branch %s\n", m.Branch)
	default:
		fmt.Printf("Created branch %s from %s\n", m.Branch, m.Base)
	}
	fmt.Printf("\n  Agent spawned for %s\n\n", m.ID)
	fmt.Printf("  Path:   %s\n", m.Worktree)
	fmt.Printf("  Branch: %s\n", m.Branch)
	fmt.Printf("  Brief:  %s\n", spec.briefPath)
	fmt.Printf("  Tmux:   %s\n\n", m.Tmux)
	fmt.Printf("  kovan open %s   # drop into the agent\n", m.ID)
	fmt.Printf("  kovan status     # the board\n\n")
	return nil
}

// startResult reports what a launch created.
type startResult struct {
	Manifest *session.Manifest
	Reused   bool // the branch already existed and was checked out, not created
	Tab      bool // spawned as a guest tab in an existing worktree (--in)
}

// startAgent scaffolds and launches in one step, without opening the editor —
// the convenience entry for callers that don't run the brief flow. It targets
// the repo containing the current directory.
func startAgent(id, title, from, account, modeFlag, color string, inPlace bool, in string) (startResult, error) {
	repo, err := openRepo()
	if err != nil {
		return startResult{}, err
	}
	spec, err := scaffoldAgent(repo.Root, id, title, from, account, modeFlag, color, inPlace, in, briefInput{})
	if err != nil {
		return startResult{}, err
	}
	return launchScaffolded(spec, true)
}

// agentSpec is a scaffolded-but-not-launched agent: the worktree, docs, and
// setup hook are done, but the agent has not started and the manifest is not
// recorded. The editor step runs against it before launchScaffolded.
type agentSpec struct {
	manifest      *session.Manifest
	repo          *git.Repo
	run           runner.Runner
	briefPath     string     // absolute path to the brief (context.md)
	taskAbs       string     // absolute task-doc dir in the durable kovan store
	domain        string     // method domain for this repo, for layer composition
	mode          *mode.Mode // the task mode: its prompt, posture, and docs
	scaffolded    []byte     // the scaffolded brief, to detect whether one was written
	briefProvided bool       // a brief was composed in the cockpit form
	reused        bool
	inPlace       bool // the agent runs in the repo checkout, not a worktree
	tab           bool // a guest tab sharing an existing worktree (--in); owns nothing but its manifest
}

// artifactPath is the absolute path of the mode's primary output doc, or "" when
// the mode scaffolds none.
func (s *agentSpec) artifactPath() string {
	if s.mode == nil || s.mode.Artifact() == "" {
		return ""
	}
	return filepath.Join(s.taskAbs, s.mode.Artifact())
}

func (s *agentSpec) rollback() {
	// A guest tab owns only its manifest; the worktree, skills, and CLAUDE.local
	// belong to the worktree's first tab and must survive.
	if s.tab {
		_ = session.RemovePointer(s.manifest.Tmux)
		return
	}
	if s.inPlace {
		_ = cleanupInPlace(s.manifest.Worktree, s.manifest.Tmux, s.manifest.Account, s.domain, s.manifest.Repo)
		return
	}
	_ = s.repo.WorktreeRemove(s.manifest.Worktree, true)
}

// briefInput is the brief composed in the cockpit's new-agent form: the body
// text (with [[image #N]] tokens) and the captured clipboard images. The zero
// value means "no in-form brief" — the CLI path scaffolds the template and opens
// $EDITOR instead.
type briefInput struct {
	text   string
	images []string // temp paths, in [[image #N]] order
}

// scaffoldAgent creates the worktree + branch, runs the setup hook, scaffolds
// the task docs, and builds (but does not write) the manifest. On any error it
// rolls the worktree back so a failed scaffold leaves nothing behind.
func scaffoldAgent(repoRoot, id, title, from, account, modeFlag, color string, inPlace bool, in string, brief briefInput) (spec *agentSpec, err error) {
	guest := in != ""
	if guest && inPlace {
		return nil, fmt.Errorf("--in and --in-place are mutually exclusive")
	}
	if guest && from != "" {
		return nil, fmt.Errorf("--in joins an existing worktree; its branch is inherited, so --from does not apply")
	}
	repo, err := git.Open(repoRoot)
	if err != nil {
		return nil, err
	}
	global, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}
	repoCfg, err := config.LoadRepo(repo.Root)
	if err != nil {
		return nil, err
	}
	home, err := config.Dir()
	if err != nil {
		return nil, err
	}

	// Resolve and validate the account before creating anything, so a missing
	// token file fails fast without spawning a worktree.
	account = resolveAccount(account, repoCfg.Account, global.DefaultAccount)
	if _, err := accountTokenFile(global, account); err != nil {
		return nil, err
	}

	// Resolve and load the task mode up front: an unknown mode fails before
	// anything is created.
	modeName := resolveMode(modeFlag, repoCfg.Mode, global.DefaultMode)
	taskMode, err := mode.Load(home, modeName)
	if err != nil {
		return nil, err
	}

	if id, err = resolveID(id, repoCfg.Worktree.IDPattern); err != nil {
		return nil, err
	}

	run, err := newRunner(global.Runner)
	if err != nil {
		return nil, err
	}

	repoName := filepath.Base(repo.Root)

	// The stripe color: an explicit choice wins, else the repo's default from
	// ~/.kovan/config.yaml. An unknown name is kept as-is and simply draws no
	// stripe, so a config typo never blocks a spawn.
	if color == "" {
		color = global.Projects[repoName].Color
	}

	// The primary checkout is a workspace like any other: if a live agent already
	// holds it, a newcomer joins as a tab (inheriting the branch) instead of taking
	// the checkout over. A checkout is one working tree on one branch, so the tab
	// must share that branch — a requested --from cannot apply, so it is dropped
	// with a note. Only the first occupant runs truly in-place (and may switch the
	// branch).
	if inPlace {
		if host := inPlaceAgentFor(repo.Root); host != "" {
			if from != "" {
				fmt.Fprintf(os.Stderr, "kovan: checkout is in use; --from %s ignored (a tab shares the branch — use a new workspace for %s)\n", from, from)
			}
			guest, in, inPlace, from = true, host, false, ""
		}
	}

	// Three spawn targets diverge here. A guest tab joins an existing agent's
	// worktree (no dir, no branch, no setup — it owns nothing but its manifest);
	// in-place works directly in the repo checkout on an existing branch; the
	// default creates an isolated worktree off a base.
	var (
		wtPath string
		branch string
		base   string
		reuse  bool
	)
	switch {
	case guest:
		target, terr := findSession(in)
		if terr != nil {
			return nil, fmt.Errorf("--in %s: %w", in, terr)
		}
		if target.Repo != repoName {
			return nil, fmt.Errorf("--in %s is in repo %s, not %s; run from that repo", in, target.Repo, repoName)
		}
		wtPath = target.Worktree
		branch = target.Branch
		// Two editing tabs in one worktree can clobber each other's uncommitted
		// files (the reason worktrees exist). Warn, but leave the call to the human
		// — a read-only companion (review/analyze) is the safe pattern.
		if taskMode.Posture != "read-only" {
			if other := editTabIn(wtPath); other != "" {
				fmt.Fprintf(os.Stderr, "kovan: warning — agent %s already edits %s; two editing tabs share one working tree and can clobber each other. Consider a read-only mode (--mode review/analyze) for this tab.\n", other, wtPath)
			}
		}
		// A guest's only artifact is its manifest; drop it if a later step fails.
		defer func() {
			if err != nil {
				_ = session.RemovePointer(sessionName(repoName, id))
			}
		}()
	case inPlace:
		wtPath = repo.Root
		if branch, err = resolveInPlaceBranch(repo, from); err != nil {
			return nil, err
		}
		// Reverse the in-place mutations if a later step fails. (The branch switch,
		// guarded clean above, is left as-is.)
		defer func() {
			if err != nil {
				_ = cleanupInPlace(wtPath, sessionName(repoName, id), account, repoCfg.Domain, repoName)
			}
		}()
	default:
		base = from
		if base == "" {
			base = repoCfg.Worktree.Base
		}
		if base == "" {
			if base, err = repo.DefaultBase(); err != nil {
				return nil, err
			}
		}
		wtPath = worktreePath(repo.Root, repoCfg.Worktree.Prefix, id)
		if _, err := os.Stat(wtPath); err == nil {
			return nil, fmt.Errorf("worktree already exists at %s (use `kovan remove %s` first)", wtPath, id)
		}
		branch = renderBranch(repoCfg.Worktree.BranchTemplate, resolveAuthor(global, repo), id, slugify(title))
		reuse = repo.BranchExists(branch) || repo.RemoteBranchExists(branch)
		// Fork a fresh branch off the latest pushed base, not a stale local ref.
		// Best-effort: offline or a flaky remote falls back to the local base.
		forkRef := base
		if !reuse {
			if ref, ferr := repo.FetchBase(base); ferr != nil {
				fmt.Fprintf(os.Stderr, "kovan: fetch %s: %v (using local %s)\n", base, ferr, base)
			} else {
				forkRef = ref
			}
		}
		if err := repo.WorktreeAdd(wtPath, branch, forkRef, !reuse); err != nil {
			return nil, err
		}
		// The worktree now exists; undo it if any later step fails.
		defer func() {
			if err != nil {
				_ = repo.WorktreeRemove(wtPath, true)
			}
		}()
	}

	// Keep kovan's generated files out of the agent's `git status`. (In-place,
	// these patterns are accepted residue on teardown: they live in the shared
	// $GIT_COMMON_DIR/info/exclude and a sibling worktree agent may rely on them.)
	if err := repo.AddExclude(".kovan/session.yaml"); err != nil {
		return nil, err
	}
	if err := repo.AddExclude(claudeLocalFile); err != nil {
		return nil, err
	}

	// The setup hook warms a fresh worktree's build caches; in-place reuses the
	// checkout's and a guest tab reuses its host worktree's, so both skip it.
	if !inPlace && !guest {
		main, err := repo.MainWorktree()
		if err != nil {
			return nil, err
		}
		env := hookEnv{Path: wtPath, ID: id, Branch: branch, Base: base, Main: main, Repo: repoName}
		if err := runRepoHook(repo.Root, "setup", env); err != nil {
			return nil, err
		}
	}

	// Task docs live in the durable kovan store, not the ephemeral worktree, so
	// they survive worktree removal without any preserve-back step. The mode
	// decides which docs beyond the brief and learnings get scaffolded.
	taskAbs := taskDocsDir(home, repoName, repoCfg.Task.Dir, id)
	if err := taskdoc.Scaffold(taskAbs, id, title, repoCfg.Task.Token, templatesDir(), taskMode.Docs); err != nil {
		return nil, err
	}
	briefPath := filepath.Join(taskAbs, taskdoc.Brief)
	// Baseline captured before the in-form brief is written, so writing it counts
	// as "brief written" and the agent is told to read it.
	scaffolded, _ := os.ReadFile(briefPath)

	// A brief composed in the cockpit replaces the template: move its images into
	// the task-doc dir and resolve the [[image #N]] tokens to real references.
	if err := writeBrief(taskAbs, briefPath, id, title, repoCfg.Task.Token, brief); err != nil {
		return nil, err
	}

	// Surface the agent's scoped skills (account + domain + project) in the
	// worktree. Best-effort — a skills hiccup must never roll back the spawn. A
	// guest tab inherits the host worktree's already-linked skills, so it skips this.
	if !guest {
		if _, e := linkWorktreeSkills(repo, wtPath, account, repoCfg.Domain, repoName); e != nil {
			fmt.Fprintf(os.Stderr, "kovan: link skills: %v\n", e)
		}
	}

	// A known session id makes the agent's transcript findable, so the board can
	// read the live permission mode from it (the hook can't see mode toggles).
	sessionID, err := session.NewID()
	if err != nil {
		return nil, err
	}
	m := &session.Manifest{
		ID:        id,
		Title:     title,
		Repo:      repoName,
		RepoRoot:  repo.Root,
		Worktree:  wtPath,
		Branch:    branch,
		Base:      base,
		InPlace:   inPlace,
		Tmux:      sessionName(repoName, id),
		Agent:     global.Agent,
		Account:   account,
		SessionID: sessionID,
		State:     "working",
		TaskMode:  taskMode.Name,
		Color:     color,
		CreatedAt: time.Now(),
	}
	return &agentSpec{
		manifest:      m,
		repo:          repo,
		run:           run,
		briefPath:     briefPath,
		taskAbs:       taskAbs,
		domain:        repoCfg.Domain,
		mode:          taskMode,
		scaffolded:    scaffolded,
		briefProvided: brief.text != "",
		reused:        reuse,
		inPlace:       inPlace,
		tab:           guest,
	}, nil
}

// editTabIn returns the id of an agent already editing (non-read-only posture)
// the given worktree, or "" if none — used to warn before adding a second
// editing tab that would share the working tree.
func editTabIn(worktree string) string {
	all, err := session.List()
	if err != nil {
		return ""
	}
	home, err := config.Dir()
	if err != nil {
		return ""
	}
	for _, m := range all {
		if m.Worktree != worktree {
			continue
		}
		// Posture resolves live from the mode; an unresolvable mode counts as
		// editing, so the clobber warning errs toward firing.
		if md, err := mode.Load(home, m.TaskMode); err == nil && md.ReadOnly() {
			continue
		}
		return m.ID
	}
	return ""
}

// inPlaceAgentFor returns the id of the live agent currently holding the repo
// checkout at root, or "" if none. It is used to route a newcomer: when an agent
// already occupies the checkout, the new one joins it as a tab rather than taking
// it over. Archived agents are skipped — their tmux is dead, so there is no live
// tab to join and the newcomer becomes a fresh first occupant.
func inPlaceAgentFor(root string) string {
	manifests, err := session.List()
	if err != nil {
		return ""
	}
	for _, m := range manifests {
		if m.InPlace && m.RepoRoot == root && !m.Archived {
			return m.ID
		}
	}
	return ""
}

// resolveInPlaceBranch determines and checks out the branch an in-place agent
// works on directly. A blank target means the checkout's current branch (no
// switch). An explicit target is checked out; switching from a dirty tree is
// refused so the human's open work is never clobbered. An origin/<name> ref is
// reduced to <name> so git creates a local tracking branch.
func resolveInPlaceBranch(repo *git.Repo, target string) (string, error) {
	current, err := repo.CurrentBranch(repo.Root)
	if err != nil {
		return "", err
	}
	target = strings.TrimPrefix(target, "origin/")
	if target == "" {
		if current == "" {
			return "", fmt.Errorf("checkout is on a detached HEAD; pass a branch to work on (--from <branch>)")
		}
		return current, nil
	}
	if target == current {
		return current, nil
	}
	dirty, err := repo.Dirty(repo.Root)
	if err != nil {
		return "", err
	}
	if dirty {
		return "", fmt.Errorf("checkout has uncommitted changes; commit or stash them, or pick the current branch (%s), before switching to %s", current, target)
	}
	if err := repo.Checkout(target); err != nil {
		return "", err
	}
	return target, nil
}

// launchScaffolded starts the agent with the brief (or, when quick or the brief
// was left unwritten, the title) and records the manifest. It rolls everything
// back on failure.
func launchScaffolded(spec *agentSpec, quick bool) (res startResult, err error) {
	m := spec.manifest
	started := false
	defer func() {
		if err == nil {
			return
		}
		if started {
			_ = spec.run.Kill(m.Tmux)
			_ = session.RemovePointer(m.Tmux)
		}
		spec.rollback()
	}()

	// A throwaway (--quick) task gets no maintenance protocol; a briefed one gets
	// a CLAUDE.local.md pointer the agent reads every session. A guest tab leaves
	// the shared file alone (it belongs to the worktree's first tab) and is briefed
	// through its opening prompt and task-doc dir instead.
	if !quick && !spec.tab {
		if err := writeClaudeLocal(m.Worktree, m.ID, m.Title, spec.taskAbs, m.Account, spec.domain, m.Repo, spec.mode); err != nil {
			return res, err
		}
	}

	sess, err := runnerSession(m, launchFresh, openingPrompt(spec, quick))
	if err != nil {
		return res, err
	}
	if err := spec.run.Start(sess); err != nil {
		return res, err
	}
	started = true
	if err := m.Write(); err != nil {
		return res, err
	}
	return startResult{Manifest: m, Reused: spec.reused, Tab: spec.tab}, nil
}

// openingPrompt renders the task mode's prompt, pointing the agent at its brief
// and the mode's output artifact. A quick task (or one launched with no brief)
// just gets the title as a throwaway goal.
func openingPrompt(spec *agentSpec, quick bool) string {
	if quick || (!spec.briefProvided && !briefWritten(spec)) {
		return spec.manifest.Title
	}
	return strings.NewReplacer(
		"{{brief}}", spec.briefPath,
		"{{artifact}}", spec.artifactPath(),
	).Replace(spec.mode.Prompt)
}

func briefWritten(spec *agentSpec) bool {
	cur, err := os.ReadFile(spec.briefPath)
	if err != nil || len(bytes.TrimSpace(cur)) == 0 {
		return false
	}
	return !bytes.Equal(cur, spec.scaffolded) // unchanged scaffold means no brief
}

// writeBrief renders a cockpit-composed brief into context.md: it moves each
// captured image into <taskAbs>/images/paste-N.png, resolves every [[image #N]]
// token in the body to a markdown reference, and writes the brief under the task
// heading. An empty brief is a no-op (the scaffolded template stands, for the
// CLI's $EDITOR path).
func writeBrief(taskAbs, briefPath, id, title, token string, brief briefInput) error {
	if brief.text == "" {
		return nil
	}
	body := taskdoc.Substitute(brief.text, id, title, token)
	if len(brief.images) > 0 {
		dir := filepath.Join(taskAbs, "images")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create images dir: %w", err)
		}
		for i, src := range brief.images {
			name := fmt.Sprintf("paste-%d.png", i+1)
			if err := moveFile(src, filepath.Join(dir, name)); err != nil {
				return fmt.Errorf("attach image: %w", err)
			}
			token := fmt.Sprintf("[[image #%d]]", i+1)
			ref := fmt.Sprintf("![image #%d](./images/%s)", i+1, name)
			body = strings.ReplaceAll(body, token, ref)
		}
	}
	out := fmt.Sprintf("# %s — %s\n\n%s\n", id, title, body)
	if err := os.WriteFile(briefPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write brief: %w", err)
	}
	return nil
}

// moveFile renames src to dst, falling back to copy+remove across devices (temp
// files and the kovan store can sit on different filesystems).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// templatesDir is ~/.kovan/templates; taskdoc falls back to built-ins per file
// when it is absent.
func templatesDir() string {
	if dir, err := config.Dir(); err == nil {
		return filepath.Join(dir, "templates")
	}
	return ""
}
