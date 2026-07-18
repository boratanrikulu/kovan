package config

import (
	"strings"
	"testing"
)

func TestSyncPristineFileGetsFreshTemplate(t *testing.T) {
	stale := "# kovan's own settings, old header.\n\n# gates:\n#   work_hours: \"09:00-18:00\"\n"
	got := string(SyncGlobal([]byte(stale), nil))
	if got != globalTemplate {
		t.Errorf("pristine file not replaced by the template:\n%s", got)
	}
}

func TestSyncFreshScaffoldUnchanged(t *testing.T) {
	got := string(SyncGlobal([]byte(globalTemplate), nil))
	if got != globalTemplate {
		t.Errorf("fresh scaffold changed by sync:\n%s", got)
	}
}

func TestSyncPreservesUserValueAndRefreshesDocs(t *testing.T) {
	in := "runner: podman\n"
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "runner: podman\n") {
		t.Errorf("user value lost:\n%s", got)
	}
	if strings.Contains(got, "# runner: tmux") {
		t.Errorf("template doc for a user-set key still present:\n%s", got)
	}
	// untouched keys keep their fresh documentation
	for _, want := range []string{"# agent: claude", "# gates:", "# default_mode: code"} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses template line %q:\n%s", want, got)
		}
	}
}

func TestSyncActiveSectionGainsNewDocs(t *testing.T) {
	in := "gates:\n  push: ask\n"
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "gates:\n  push: ask\n") {
		t.Errorf("active gates block lost:\n%s", got)
	}
	// the section header stays active, the unset keys arrive as fresh docs
	for _, want := range []string{"#   read_only: ask", "#   default_branch:", "#   patterns:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "# gates:") {
		t.Errorf("gates emitted twice (commented header kept):\n%s", got)
	}
}

func TestSyncDeadKeyRemovedOnDecision(t *testing.T) {
	in := "gates:\n  work_hours: \"09:00-18:00\"\n  push: ask\n"
	got := string(SyncGlobal([]byte(in), map[string]bool{"gates.work_hours": true}))
	if strings.Contains(got, "work_hours") {
		t.Errorf("removed dead key still present:\n%s", got)
	}
	if !strings.Contains(got, "  push: ask\n") {
		t.Errorf("sibling user key lost:\n%s", got)
	}
}

func TestSyncDeadKeyKeptInSectionWhenDeclined(t *testing.T) {
	in := "gates:\n  work_hours: \"09:00-18:00\"\n  push: ask\n"
	got := string(SyncGlobal([]byte(in), map[string]bool{}))
	if !strings.Contains(got, "  work_hours: \"09:00-18:00\"\n") {
		t.Errorf("kept dead key lost:\n%s", got)
	}
	// it stays inside the gates section: before the apps section starts
	if strings.Index(got, "work_hours") > strings.Index(got, "# apps:") {
		t.Errorf("kept dead key escaped its section:\n%s", got)
	}
}

func TestSyncDeadTopLevelKeptAtEOF(t *testing.T) {
	in := "montior:\n  model: opus\nrunner: tmux\n"
	got := string(SyncGlobal([]byte(in), map[string]bool{}))
	idx := strings.Index(got, "montior:")
	if idx < 0 || idx < strings.Index(got, "# default_mode") {
		t.Errorf("unknown top-level key not kept at end of file:\n%s", got)
	}
	if !strings.Contains(got, "montior:\n  model: opus\n") {
		t.Errorf("kept block lost its children:\n%s", got)
	}
}

func TestSyncStaleCommentAskedAndRemoved(t *testing.T) {
	in := "# gates:\n#   work_hours: \"09:00-18:00\"\ngates:\n  push: ask\n"
	got := string(SyncGlobal([]byte(in), map[string]bool{"gates.work_hours": true}))
	if strings.Contains(got, "work_hours") {
		t.Errorf("removed stale comment still present:\n%s", got)
	}
}

func TestSyncStaleCommentKeptWhenDeclined(t *testing.T) {
	in := "# gates:\n#   work_hours: \"09:00-18:00\"\ngates:\n  push: ask\n"
	got := string(SyncGlobal([]byte(in), map[string]bool{}))
	if !strings.Contains(got, "#   work_hours: \"09:00-18:00\"") {
		t.Errorf("kept stale comment lost:\n%s", got)
	}
}

func TestSyncAccountsBlockSplicedVerbatim(t *testing.T) {
	in := `accounts:
  personal:
    token_file: ~/.kovan/tokens/personal
  company:
    token_file: ~/.kovan/tokens/company
default_account: personal
`
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "  company:\n    token_file: ~/.kovan/tokens/company\n") {
		t.Errorf("accounts entries lost:\n%s", got)
	}
	if !strings.Contains(got, "default_account: personal\n") {
		t.Errorf("default_account lost:\n%s", got)
	}
	if strings.Contains(got, "# accounts:") {
		t.Errorf("accounts placeholder docs still present:\n%s", got)
	}
}

func TestSyncListValuesPreserved(t *testing.T) {
	in := "tmux:\n  options:\n    - mouse off\n    - history-limit 9000\n"
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "    - mouse off\n    - history-limit 9000\n") {
		t.Errorf("list scalars lost:\n%s", got)
	}
	if strings.Contains(got, "#     - mouse on") {
		t.Errorf("template list placeholders still present:\n%s", got)
	}
}

func TestSyncPatternsSpliced(t *testing.T) {
	in := "gates:\n  patterns:\n    - match: 'rm -rf'\n      action: deny\n      reason: \"no\"\n"
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "    - match: 'rm -rf'\n      action: deny\n") {
		t.Errorf("pattern entries lost:\n%s", got)
	}
	if strings.Contains(got, "terraform") {
		t.Errorf("pattern placeholder docs still present:\n%s", got)
	}
}

func TestSyncUserProseCommentTravels(t *testing.T) {
	in := "# rotate this token every quarter\nrunner: tmux\n"
	got := string(SyncGlobal([]byte(in), nil))
	if !strings.Contains(got, "# rotate this token every quarter\nrunner: tmux\n") {
		t.Errorf("user prose comment lost or detached:\n%s", got)
	}
}

func TestSyncIdempotent(t *testing.T) {
	in := `# note: mine
runner: podman
gates:
  work_hours: "x"
  push: ask
accounts:
  personal:
    token_file: ~/.kovan/tokens/personal
`
	once := SyncGlobal([]byte(in), map[string]bool{})
	twice := SyncGlobal(once, map[string]bool{})
	if string(once) != string(twice) {
		t.Errorf("sync not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestSyncRepoPreservesAndRefreshes(t *testing.T) {
	in := "worktree:\n  base: master\nmode: review\n"
	got := string(SyncRepo([]byte(in), nil))
	for _, want := range []string{"  base: master\n", "mode: review\n", "#   prefix: agent", "# task:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
}

func TestSyncResultIsCleanExceptValues(t *testing.T) {
	in := "gates:\n  work_hours: \"x\"\n  push: aks\n"
	out := SyncGlobal([]byte(in), map[string]bool{"gates.work_hours": true})
	rep := CheckGlobal(out)
	if len(rep.Dead) != 0 || len(rep.Stale) != 0 || len(rep.New) != 0 || rep.ParseErr != "" {
		t.Errorf("synced file still drifts: %+v\n%s", rep, out)
	}
}

// The real-world work_hours shape: a dead key that is a container. Removing it
// must take its children; keeping it must emit the block exactly once.
const deadContainer = `gates:
  push: ask
  work_hours:
    action: off           # ask | off
    start: "10:00"
    end: "18:00"
`

func TestSyncDeadContainerRemovedWithChildren(t *testing.T) {
	got := string(SyncGlobal([]byte(deadContainer), map[string]bool{"gates.work_hours": true}))
	for _, gone := range []string{"work_hours", "action: off", "start:", "end:"} {
		if strings.Contains(got, gone) {
			t.Errorf("removed container leaked %q:\n%s", gone, got)
		}
	}
	if !strings.Contains(got, "  push: ask\n") {
		t.Errorf("sibling lost:\n%s", got)
	}
}

func TestSyncDeadContainerKeptOnce(t *testing.T) {
	got := string(SyncGlobal([]byte(deadContainer), map[string]bool{}))
	want := "  work_hours:\n    action: off           # ask | off\n    start: \"10:00\"\n    end: \"18:00\"\n"
	if strings.Count(got, "work_hours") != 1 || !strings.Contains(got, want) {
		t.Errorf("kept container mangled:\n%s", got)
	}
}

func TestSyncTrailingUserNoteTravels(t *testing.T) {
	in := `accounts:
  personal:
    token_file: ~/.kovan/tokens/personal
# default_account left unset: the logged-in account is used.
# only company repos need a token file.
`
	got := string(SyncGlobal([]byte(in), nil))
	want := "    token_file: ~/.kovan/tokens/personal\n# default_account left unset: the logged-in account is used.\n# only company repos need a token file.\n"
	if !strings.Contains(got, want) {
		t.Errorf("trailing note lost or detached:\n%s", got)
	}
}

func TestSyncProseBetweenActiveLinesAttachesBelow(t *testing.T) {
	in := "runner: tmux\n# my agent override, do not touch\nagent: claude-next\n"
	got := string(SyncGlobal([]byte(in), nil))
	want := "runner: tmux\n# my agent override, do not touch\nagent: claude-next\n"
	if !strings.Contains(got, want) {
		t.Errorf("sandwiched note lost, duplicated, or detached:\n%s", got)
	}
	if strings.Count(got, "my agent override") != 1 {
		t.Errorf("sandwiched note duplicated:\n%s", got)
	}
}
