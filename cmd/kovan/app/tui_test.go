package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/method"
	"github.com/boratanrikulu/kovan/internal/session"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func TestKeymapAction(t *testing.T) {
	cases := []struct {
		msg  tea.KeyMsg
		want action
	}{
		{runeKey('k'), actUp},
		{tea.KeyMsg{Type: tea.KeyUp}, actUp},
		{runeKey('j'), actDown},
		{runeKey('n'), actNew},
		{tea.KeyMsg{Type: tea.KeyEnter}, actOpen},
		{runeKey('o'), actOpen},
		{runeKey('d'), actRemove},
		{runeKey('x'), actRemove},
		{runeKey('m'), actMethod},
		{runeKey('w'), actNotes},
		{runeKey('a'), actArchive},
		{runeKey('p'), actPin},
		{runeKey('/'), actFilter},
		{runeKey('v'), actColumns},
		{runeKey('r'), actNone}, // refresh key removed; the board auto-refreshes
		{runeKey('?'), actHelp},
		{runeKey('q'), actQuit},
		{tea.KeyMsg{Type: tea.KeyCtrlC}, actQuit},
		{runeKey('z'), actNone},
	}
	for _, c := range cases {
		if got := keys.action(c.msg); got != c.want {
			t.Errorf("action(%q) = %d, want %d", c.msg.String(), got, c.want)
		}
	}
}

func TestPanelHeights(t *testing.T) {
	for _, total := range []int{40, 24, 12} {
		b, p := panelHeights(total)
		if b < 1 || p < 1 {
			t.Errorf("total %d: non-positive board=%d peek=%d", total, b, p)
		}
		if reserved := 4 + summaryStripLines + helpLineRows; b+p+reserved != total {
			t.Errorf("total %d: board+peek+%d = %d, want exact fill", total, reserved, b+p+reserved)
		}
	}
	if b, p := panelHeights(3); b < 1 || p < 1 {
		t.Errorf("tiny terminal degraded to board=%d peek=%d", b, p)
	}
}

func TestIsCharDevice(t *testing.T) {
	if isCharDevice(0) {
		t.Error("regular file mode is not a char device")
	}
	if !isCharDevice(os.ModeCharDevice) {
		t.Error("char device mode should be detected")
	}
}

func TestAssembleBoard(t *testing.T) {
	now := time.Now()
	manifests := []*session.Manifest{
		{ID: "OLD", Repo: "r", Title: "g1", Tmux: "t-old", CreatedAt: now.Add(-time.Hour)},
		{ID: "NEW", Repo: "r", Title: "g2", Tmux: "t-new", CreatedAt: now},
	}
	alive := func(name string) bool { return name == "t-new" }

	rows := assembleBoard(manifests, alive)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ID != "NEW" || rows[1].ID != "OLD" {
		t.Errorf("not sorted newest-first: %s, %s", rows[0].ID, rows[1].ID)
	}
	if rows[0].State != "working" {
		t.Errorf("NEW state = %q, want working", rows[0].State)
	}
	if rows[1].State != "stopped" {
		t.Errorf("OLD state = %q, want stopped", rows[1].State)
	}
}

func TestBoardClustersTabs(t *testing.T) {
	now := time.Now()
	// Two workspaces: "wsA" hosts an owner + a tab; "wsB" is a lone newer agent.
	manifests := []*session.Manifest{
		{ID: "owner", Repo: "r", Worktree: "/wt/a", Branch: "feat/a", Tmux: "t-owner", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "lone", Repo: "r", Worktree: "/wt/b", Branch: "feat/b", Tmux: "t-lone", CreatedAt: now.Add(-time.Minute)},
		{ID: "tab", Repo: "r", Worktree: "/wt/a", Branch: "feat/a", Tmux: "t-tab", CreatedAt: now.Add(-time.Hour)},
	}
	rows := filterRows(assembleBoard(manifests, func(string) bool { return false }), "", false)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// wsB is the newest group → first. wsA's owner (older) leads its tab.
	if rows[0].ID != "lone" || rows[1].ID != "owner" || rows[2].ID != "tab" {
		t.Fatalf("clustering order = %s,%s,%s; want lone,owner,tab", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	if rows[0].Cont || rows[1].Cont {
		t.Error("lone and owner are not continuations")
	}
	if !rows[2].Cont {
		t.Error("tab sharing the owner's worktree should be a continuation")
	}
	if got := workspaceCell(rows[2]); got != "└ feat/a" {
		t.Errorf("continuation cell = %q, want the tree-marked branch", got)
	}
}

func TestBoardStatePrecedence(t *testing.T) {
	mk := func(state string) []*session.Manifest {
		return []*session.Manifest{{Tmux: "t", State: state}}
	}
	alive := func(string) bool { return true }
	dead := func(string) bool { return false }

	cases := []struct {
		name string
		rows []boardRow
		want string
	}{
		{"alive needs-you", assembleBoard(mk("needs-you"), alive), "needs-you"},
		{"alive idle", assembleBoard(mk("idle"), alive), "idle"},
		{"alive empty is working", assembleBoard(mk(""), alive), "working"},
		{"dead is stopped", assembleBoard(mk("needs-you"), dead), "stopped"},
	}
	for _, c := range cases {
		if c.rows[0].State != c.want {
			t.Errorf("%s: state = %q, want %q", c.name, c.rows[0].State, c.want)
		}
	}
}

func TestFormAccountPicker(t *testing.T) {
	f := formModel{
		inputs:   make([]textinput.Model, 2),
		accounts: []string{"company", "personal"},
		account:  1, // personal
	}
	if f.selectedAccount() != "personal" {
		t.Fatalf("selectedAccount = %q, want personal", f.selectedAccount())
	}
	f.setFocus(len(f.inputs) + 4) // id/title, project, target, from, mode, then account
	if !f.onAccount() {
		t.Fatal("expected focus on the account field")
	}
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyRight})
	if f.selectedAccount() != "company" {
		t.Errorf("after right (wrap) = %q, want company", f.selectedAccount())
	}

	none := formModel{inputs: make([]textinput.Model, 3)}
	if none.selectedAccount() != "" {
		t.Errorf("no accounts should select \"\", got %q", none.selectedAccount())
	}
}

func TestFormTargetCycle(t *testing.T) {
	f := formModel{inputs: make([]textinput.Model, 2)}
	f.setFocus(len(f.inputs) + 1) // the target toggle
	if !f.onTarget() {
		t.Fatal("expected focus on the target field")
	}
	// Right cycles worktree → checkout → tab → worktree.
	for _, want := range []int{targetCheckout, targetTab, targetWorktree} {
		f, _ = f.update(tea.KeyMsg{Type: tea.KeyRight})
		if f.target != want {
			t.Fatalf("after right, target = %d, want %d", f.target, want)
		}
	}
	// Left goes back.
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyLeft})
	if f.target != targetTab {
		t.Errorf("after left, target = %d, want targetTab", f.target)
	}
}

// TestFormCheckoutInherits checks that when a live agent holds the checkout, the
// "This checkout" target's branch field shows the inherited branch instead of a
// picker — a second agent there joins as a tab and cannot switch the branch.
func TestFormCheckoutInherits(t *testing.T) {
	f := formModel{inputs: make([]textinput.Model, 2), target: targetCheckout, checkoutBranch: "feat/x"}

	label, value := f.ctxCollapsed()
	if label != "branch" || !strings.Contains(value, "inherits feat/x") {
		t.Errorf("collapsed field = %q/%q, want branch / inherits feat/x", label, value)
	}

	f.setFocus(len(f.inputs) + 2) // the from/branch field
	if !f.onFrom() {
		t.Fatal("expected focus on the from/branch field")
	}
	_, items, sel, _, filter := f.focusedOptions()
	if filter {
		t.Error("an occupied checkout should not offer a live branch filter")
	}
	if sel != -1 || len(items) != 1 || !strings.Contains(items[0], "inherits feat/x") {
		t.Errorf("options = %v (sel %d), want a single informational 'inherits feat/x'", items, sel)
	}

	// A free checkout names the currently checked-out branch in its "stay" option.
	free := formModel{inputs: make([]textinput.Model, 2), target: targetCheckout, checkoutCurrent: "main"}
	if _, v := free.ctxCollapsed(); v != "stay on main (current)" {
		t.Errorf("free checkout collapsed value = %q, want it to name the current branch", v)
	}
	if l := free.stayLabel(); l != "stay on main (current)" {
		t.Errorf("stayLabel = %q, want it to name the current branch", l)
	}
	// With the current branch unknown, it falls back to the generic phrase.
	if l := (formModel{}).stayLabel(); l != "stay on the current branch" {
		t.Errorf("stayLabel fallback = %q", l)
	}
}

func TestWorkspacePicker(t *testing.T) {
	p := workspacePicker{opts: []workspaceOpt{
		{id: "a", label: "feat/a  (a)"},
		{id: "b", label: "feat/b  (b)"},
	}}
	if p.value() != "a" {
		t.Errorf("default selection = %q, want a", p.value())
	}
	if p = p.update(tea.KeyMsg{Type: tea.KeyDown}); p.value() != "b" {
		t.Errorf("after down = %q, want b", p.value())
	}
	// Empty picker yields no join id (submit then blocks the tab).
	if (workspacePicker{}).value() != "" {
		t.Error("empty workspace picker should select nothing")
	}
}

func TestFormBriefField(t *testing.T) {
	// No accounts → focusables are id, title, project, target, from, mode,
	// color, brief; brief is last.
	f := formModel{inputs: make([]textinput.Model, 2), brief: textarea.New()}
	if f.total() != 8 {
		t.Fatalf("total = %d, want 8 (id, title, project, target, from, mode, color, brief)", f.total())
	}
	f.setFocus(f.total() - 1)
	if !f.onBrief() {
		t.Fatal("expected focus on the brief field")
	}
	// A captured image drops a token at the cursor.
	f.brief.InsertString("[[image #1]]")
	if !strings.Contains(f.brief.Value(), "[[image #1]]") {
		t.Errorf("brief should hold the image token, got %q", f.brief.Value())
	}
}

func TestFormHeader(t *testing.T) {
	h := strings.Join(formModel{repo: "myrepo"}.formHeader(), "\n")
	if !strings.Contains(h, "new agent") || !strings.Contains(h, "myrepo") {
		t.Errorf("header should name the target repo:\n%s", h)
	}
	if h := strings.Join(formModel{}.formHeader(), "\n"); strings.Contains(h, "new agent ·") {
		t.Errorf("empty repo should not add a separator:\n%s", h)
	}
	// The options panel explains each target choice when where is focused.
	w := formModel{inputs: make([]textinput.Model, 2)}
	w.setFocus(len(w.inputs) + 1) // focus the where field
	_, items, _, _, _ := w.focusedOptions()
	if !strings.Contains(strings.Join(items, "\n"), "fresh worktree") {
		t.Errorf("focused where options should explain the choices: %v", items)
	}
}

func TestBranchPicker(t *testing.T) {
	branches := []string{"main", "feat/x", "feat/y", "release"}

	p := branchPicker{branches: branches}
	if p.value() != "" {
		t.Errorf("default selection = %q, want empty (default base)", p.value())
	}
	if p = p.update(tea.KeyMsg{Type: tea.KeyDown}); p.value() != "main" {
		t.Errorf("after down = %q, want main", p.value())
	}

	// Typing filters by substring; default stays first.
	f := branchPicker{branches: branches}
	for _, r := range "feat" {
		f = f.update(runeKey(r))
	}
	if items := f.items(); len(items) != 3 || items[1] != "feat/x" || items[2] != "feat/y" {
		t.Errorf("filtered items = %v, want [\"\" feat/x feat/y]", items)
	}
	if f = f.update(tea.KeyMsg{Type: tea.KeyDown}); f.value() != "feat/x" {
		t.Errorf("filtered selection = %q, want feat/x", f.value())
	}
}

func TestBriefSkeleton(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir()) // no user template → built-in
	s := briefSkeleton()
	if strings.Contains(s, "{{id}}") || strings.Contains(s, "{{title}}") {
		t.Errorf("skeleton should drop the heading line, got:\n%s", s)
	}
	if !strings.Contains(s, "## Summary") {
		t.Errorf("skeleton should keep the template sections, got:\n%s", s)
	}
}

func TestSizeFormSafeOnBoard(t *testing.T) {
	// On the board the form is the zero value; sizing it must not panic
	// (regression: textarea.SetWidth nil-derefs on an uninitialized model).
	m := model{mode: modeBoard, ready: true, width: 100, height: 24}
	m.sizeForm()
}

func TestFormViewFills(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir()) // no accounts, built-in template
	m := testModel(modeForm, nil)
	// Two list-pickers (project + from) make the form taller than the 24-row
	// default; size it to a realistic terminal so the brief fills the rest.
	m.height = 40
	m.form = newForm()
	m.sizeForm()
	if got := strings.Count(m.View(), "\n") + 1; got != m.height {
		t.Errorf("form view = %d lines, want %d (fills the terminal)", got, m.height)
	}

	// The full-screen brief also fills the terminal exactly.
	m.setBriefFull(true)
	if !m.form.briefFull || !m.form.onBrief() {
		t.Fatal("full-screen brief should be on and focus the brief")
	}
	if got := strings.Count(m.View(), "\n") + 1; got != m.height {
		t.Errorf("full-screen brief view = %d lines, want %d", got, m.height)
	}
}

func TestAssembleBoardAccount(t *testing.T) {
	manifests := []*session.Manifest{{Tmux: "t", Account: "personal"}}
	rows := assembleBoard(manifests, func(string) bool { return true })
	if rows[0].Account != "personal" {
		t.Errorf("account = %q, want personal", rows[0].Account)
	}
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func press(m model, k tea.KeyMsg) model {
	nm, _ := m.handleKey(k)
	return nm.(model)
}

func TestHandleKeyTransitions(t *testing.T) {
	base := model{mode: modeBoard, rows: []boardRow{{ID: "A"}, {ID: "B"}}}

	if m := press(base, runeKey('j')); m.cursor != 1 {
		t.Errorf("j: cursor = %d, want 1", m.cursor)
	}
	if m := press(base, runeKey('k')); m.cursor != 0 {
		t.Errorf("k at top: cursor = %d, want 0 (clamped)", m.cursor)
	}
	if m := press(base, runeKey('n')); m.mode != modeForm {
		t.Errorf("n: mode = %d, want modeForm", m.mode)
	}

	confirm := press(base, runeKey('d'))
	if confirm.mode != modeConfirm || confirm.confirmID != "A" {
		t.Errorf("d: mode=%d id=%q, want modeConfirm A", confirm.mode, confirm.confirmID)
	}
	if m := press(confirm, runeKey('n')); m.mode != modeBoard {
		t.Errorf("confirm+n: mode = %d, want modeBoard", m.mode)
	}

	form := press(base, runeKey('n'))
	if m := press(form, tea.KeyMsg{Type: tea.KeyEsc}); m.mode != modeBoard {
		t.Errorf("form+esc: mode = %d, want modeBoard", m.mode)
	}

	help := model{mode: modeHelp}
	if m := press(help, runeKey('z')); m.mode != modeBoard {
		t.Errorf("help+any: mode = %d, want modeBoard", m.mode)
	}
}

func testModel(mode uiMode, rows []boardRow) model {
	m := model{mode: mode, rows: rows, help: help.New(), peek: viewport.New(80, 6), ready: true, width: 100, height: 24, boardRows: 10}
	return m
}

func TestViewRenders(t *testing.T) {
	rows := []boardRow{{State: "working", ID: "TASK-1", Repo: "kovan", Age: "2m", Branch: "feat/x", Title: "fix vfs"}}

	board := testModel(modeBoard, rows)
	for _, want := range []string{"kovan", "STATE", "TASK-1", "peek · TASK-1", "quit"} {
		if !strings.Contains(board.View(), want) {
			t.Errorf("board view missing %q", want)
		}
	}

	if !strings.Contains(newForm().view(""), "new agent") {
		t.Error("form view missing 'new agent'")
	}
	confirm := testModel(modeConfirm, nil)
	confirm.confirmID = "TASK-1"
	if !strings.Contains(confirm.View(), "Remove agent TASK-1") {
		t.Error("confirm view missing prompt")
	}
}

// TestBrandConsistency: the gold chip is the shared mark, and the full-screen
// views lead with it — the method view especially must lead with the brand, not
// with "method".
func TestBrandConsistency(t *testing.T) {
	if !strings.Contains(brandMark(), "kovan") {
		t.Fatalf("brandMark = %q, want the kovan chip", brandMark())
	}

	board := testModel(modeBoard, []boardRow{{State: "idle", ID: "A"}})
	if bh := board.header(); !strings.Contains(bh, "kovan") || !strings.Contains(bh, "active 1") {
		t.Errorf("board header = %q, want the brand + the active tab", bh)
	}

	m := testModel(modeMethod, nil)
	m.mctx = methodCtx{account: "company", repo: "kovan"}
	m.methodVP = viewport.New(80, 6)
	v := m.methodView()
	brand, method := strings.Index(v, "kovan"), strings.Index(v, "method")
	if brand < 0 || method < 0 || brand > method {
		t.Errorf("method view must lead with the brand before %q:\n%s", "method", v)
	}
}

// TestStatusAndHelpBothVisible: a transient status must not hide the shortcut
// bar — both appear as separate lines.
func TestStatusAndHelpBothVisible(t *testing.T) {
	m := testModel(modeBoard, []boardRow{{State: "idle", ID: "A"}})
	m.setInfo("started A")
	v := m.View()
	if !strings.Contains(v, "started A") {
		t.Error("status text missing from board view")
	}
	if !strings.Contains(v, "quit") {
		t.Error("help hints missing while a status is shown")
	}
}

func TestBoardViewFills(t *testing.T) {
	rows := []boardRow{{State: "working", ID: "A"}, {State: "idle", ID: "B"}}
	for _, bodyRows := range []int{1, 5, 10} {
		got := strings.Count(boardView(rows, 0, 100, bodyRows, boardLayout{}), "\n") + 1
		if got != bodyRows+1 {
			t.Errorf("bodyRows=%d: rendered %d lines, want %d (header + body)", bodyRows, got, bodyRows+1)
		}
	}
	// Empty board still fills exactly.
	if got := strings.Count(boardView(nil, 0, 100, 6, boardLayout{}), "\n") + 1; got != 7 {
		t.Errorf("empty board rendered %d lines, want 7", got)
	}
}

// TestStartAgentCore exercises the shared start core end to end: a real temp
// repo, a fake agent, and the from-branch override. Skipped without git/tmux.
func TestStartAgentCore(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	fake := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("agent: "+fake+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
		{"branch", "feature-base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(repo)

	res, err := startAgent("TASK-1", "demo goal", "feature-base", "", "", "", false, "")
	if err != nil {
		t.Fatalf("startAgent: %v", err)
	}
	t.Cleanup(func() { _, _ = removeAgent("TASK-1", true) })

	if res.Manifest.Base != "feature-base" {
		t.Errorf("from not honored: base = %q, want feature-base", res.Manifest.Base)
	}
	if res.Manifest.ID != "TASK-1" || res.Manifest.Title != "demo goal" {
		t.Errorf("manifest fields wrong: %+v", res.Manifest)
	}
	if _, err := session.ReadByTmux(res.Manifest.Tmux); err != nil {
		t.Errorf("manifest not written to the index: %v", err)
	}
	// A session id is minted at scaffold so the board can find the transcript.
	if res.Manifest.SessionID == "" {
		t.Error("scaffold did not assign a session id")
	}

	// Task docs live in the durable kovan store, not the ephemeral worktree.
	notes := filepath.Join(home, "projects", filepath.Base(repo), "works", "TASK-1")
	if _, err := os.Stat(filepath.Join(notes, "context.md")); err != nil {
		t.Errorf("task docs not scaffolded under ~/.kovan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Manifest.Worktree, "works")); !os.IsNotExist(err) {
		t.Errorf("worktree should hold no task docs, stat works/ = %v", err)
	}

	// Removing the agent must leave the durable docs untouched.
	if _, err := removeAgent("TASK-1", true); err != nil {
		t.Fatalf("removeAgent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(notes, "context.md")); err != nil {
		t.Errorf("task docs should survive remove, got %v", err)
	}
}

func TestFlattenMethodFiles(t *testing.T) {
	layers := []method.Layer{
		{Name: "global", Files: []method.File{{Path: "g1"}, {Path: "g2", Depth: 1}}},
		{Name: "account", Files: nil}, // empty layer contributes nothing
		{Name: "project", Files: []method.File{{Path: "p1"}}},
	}
	got := flattenMethodFiles(layers)
	want := []method.File{{Path: "g1"}, {Path: "g2", Depth: 1}, {Path: "p1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flatten = %v, want %v", got, want)
	}
	if flattenMethodFiles(nil) != nil {
		t.Error("flatten(nil) should be empty")
	}
}

func TestHandleMethodKey(t *testing.T) {
	dir := t.TempDir()
	f1, f2 := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	for _, f := range []string{f1, f2} {
		if err := os.WriteFile(f, []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	layers := []method.Layer{
		{Name: "global", Files: []method.File{{Path: f1}}},
		{Name: "account:x", Files: nil}, // skipped — the cursor visits only real files
		{Name: "project (public)", Files: []method.File{{Path: f2}}},
	}
	base := model{mode: modeMethod, methodLayers: layers, methodVP: viewport.New(80, 6)}

	if down := press(base, runeKey('j')); down.methodFile != 1 {
		t.Errorf("j: methodFile = %d, want 1 (skips the empty layer)", down.methodFile)
	}
	end := press(press(base, runeKey('j')), runeKey('j'))
	if end.methodFile != 1 {
		t.Errorf("j at end: methodFile = %d, want 1 (clamped)", end.methodFile)
	}
	if top := press(base, runeKey('k')); top.methodFile != 0 {
		t.Errorf("k at top: methodFile = %d, want 0 (clamped)", top.methodFile)
	}
	if b := press(base, tea.KeyMsg{Type: tea.KeyEsc}); b.mode != modeBoard {
		t.Errorf("esc: mode = %d, want modeBoard", b.mode)
	}
	if b := press(base, runeKey('m')); b.mode != modeBoard {
		t.Errorf("m: mode = %d, want modeBoard", b.mode)
	}
	// q backs out to the board (never quits) — consistent for both `m` and `kovan method`.
	if b := press(base, runeKey('q')); b.mode != modeBoard {
		t.Errorf("q: mode = %d, want modeBoard (back to board, not quit)", b.mode)
	}
}

func TestMethodViewRenders(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "soul.md")
	if err := os.WriteFile(f1, []byte("be good"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, "commit-helper", "SKILL.md")
	m := testModel(modeMethod, nil)
	m.mctx = methodCtx{account: "personal", domain: "code", repo: "kovan"}
	m.methodLayers = []method.Layer{
		{Name: "global", Files: []method.File{{Path: f1}}, Skills: []method.File{{Path: skill}}},
		{Name: "account:personal", Files: nil},
	}
	m.methodVP = viewport.New(80, 6)
	m.loadMethodFile()

	v := m.View()
	for _, want := range []string{"method", "personal", "global", "soul.md", "skill: commit-helper", "(none)", "ai-edit"} {
		if !strings.Contains(v, want) {
			t.Errorf("method view missing %q", want)
		}
	}
}

// TestMethodViewFills: the method view fills the terminal exactly (no dead space)
// with the status/help pinned at the bottom, like the board.
func TestMethodViewFills(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := testModel(modeMethod, nil)
	m.mctx = methodCtx{repo: "kovan"}
	m.methodLayers = []method.Layer{{Name: "global", Files: []method.File{{Path: f}}}}
	m.methodVP = viewport.New(80, 6)
	m.loadMethodFile()
	m.sizeMethodViewport()

	v := m.methodView()
	if got := strings.Count(v, "\n") + 1; got != m.height {
		t.Errorf("method view = %d lines, want %d (exact fill of the terminal)", got, m.height)
	}
}

// TestMethodCursorReachesSkills: the cursor walks past method files onto a
// layer's skills, focusing the skill's SKILL.md so e/E edits it.
func TestMethodCursorReachesSkills(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, "my-skill", "SKILL.md")
	layers := []method.Layer{
		{Name: "global", Files: []method.File{{Path: f}}, Skills: []method.File{{Path: skill}}},
	}
	base := model{mode: modeMethod, methodLayers: layers, methodVP: viewport.New(80, 6)}

	m := press(base, runeKey('j')) // file → skill
	if m.methodFile != 1 {
		t.Fatalf("methodFile = %d, want 1 (the skill)", m.methodFile)
	}
	if got := focused(flattenMethodFiles(m.methodLayers), m.methodFile); got != skill {
		t.Errorf("focused = %q, want the skill SKILL.md %q", got, skill)
	}
}

// TestAIEditArgs guards against the variadic-flag regression: "--" must sit
// immediately before the prompt (see init.go).
func TestAIEditArgs(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir()) // no config → agent defaults to claude
	cmd := aiEditCommand("/some/dir/soul.md")

	sep := -1
	for i, a := range cmd.Args {
		if a == "--" {
			sep = i
		}
	}
	if sep == -1 || sep+1 >= len(cmd.Args) {
		t.Fatalf(`no "--" guard before the prompt: %v`, cmd.Args)
	}
	if !strings.HasPrefix(cmd.Args[sep+1], "Open soul.md") {
		t.Errorf(`arg after "--" = %q, want the prompt`, cmd.Args[sep+1])
	}
	if cmd.Dir != "/some/dir" {
		t.Errorf("cmd.Dir = %q, want /some/dir", cmd.Dir)
	}
}

func TestProjectPicker(t *testing.T) {
	projects := []project{
		{name: "agent", root: "/repos/agent"},
		{name: "webapp", root: "/repos/webapp"},
		{name: "kovan", root: "/repos/kovan"},
	}

	// Empty query selects the first project (the current repo).
	p := projectPicker{projects: projects}
	if p.value().root != "/repos/agent" {
		t.Errorf("default selection = %q, want /repos/agent", p.value().root)
	}

	// Typing filters by name substring.
	f := projectPicker{projects: projects}
	for _, r := range "web" {
		f = f.update(runeKey(r))
	}
	items := f.items()
	// matches webapp, plus the always-present "path: web" entry.
	if len(items) != 2 || items[0].root != "/repos/webapp" {
		t.Fatalf("filtered items = %v, want [webapp, path:web]", items)
	}
	if f.value().root != "/repos/webapp" {
		t.Errorf("filtered selection = %q, want /repos/webapp", f.value().root)
	}

	// A query that matches nothing known is usable as a direct path.
	g := projectPicker{projects: projects}
	for _, r := range "/tmp/new-repo" {
		g = g.update(runeKey(r))
	}
	if g.value().root != "/tmp/new-repo" {
		t.Errorf("typed path = %q, want /tmp/new-repo", g.value().root)
	}

	// No projects and no query → nothing selectable.
	if (projectPicker{}).value().root != "" {
		t.Error("empty picker should select nothing")
	}
}

func TestPickerRowMarksSelection(t *testing.T) {
	// The selected row is marked even when the field is not focused, so the
	// current choice is visible while the cursor is elsewhere.
	if got := pickerRow("kovan", true, false); !strings.Contains(got, "› kovan") {
		t.Errorf("unfocused selected row should be marked with ›, got %q", got)
	}
	if got := pickerRow("kovan", true, true); !strings.Contains(got, "› kovan") {
		t.Errorf("focused selected row should be marked with ›, got %q", got)
	}
	if got := pickerRow("agent", false, false); strings.Contains(got, "›") {
		t.Errorf("unselected row should not be marked, got %q", got)
	}
}

func TestSubmitErrorStaysOnForm(t *testing.T) {
	id := textinput.New()
	title := textinput.New()
	title.SetValue("demo")
	f := formModel{
		inputs:  []textinput.Model{id, title},
		project: projectPicker{projects: []project{{name: "x", root: "/x"}}},
		brief:   textarea.New(),
	}
	f.brief.SetValue("a brief worth keeping")
	m := model{mode: modeForm, form: f}

	// Submitting keeps us on the form while the async scaffold runs.
	after, _ := m.submitForm()
	m = after.(model)
	if m.mode != modeForm {
		t.Fatalf("submit should stay on the form, got mode %d", m.mode)
	}

	// A create error keeps the form (brief intact) and shows the error.
	after, _ = m.Update(startedMsg{id: "x", err: errTest})
	m = after.(model)
	if m.mode != modeForm {
		t.Error("a create error should keep the form open")
	}
	if !m.statusErr || m.form.brief.Value() != "a brief worth keeping" {
		t.Errorf("error should show and the brief survive; status=%q brief=%q", m.status, m.form.brief.Value())
	}

	// Success switches to the board.
	after, _ = m.Update(startedMsg{id: "x"})
	if after.(model).mode != modeBoard {
		t.Error("a successful start should switch to the board")
	}
}

var errTest = errInline("boom")

type errInline string

func (e errInline) Error() string { return string(e) }

func TestHandleFilterKey(t *testing.T) {
	m := model{mode: modeFilter}
	for _, r := range "rev" {
		after, _ := m.handleFilterKey(runeKey(r))
		m = after.(model)
	}
	if m.filter != "rev" {
		t.Errorf("filter = %q, want rev", m.filter)
	}
	// backspace deletes one rune.
	after, _ := m.handleFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m = after.(model)
	if m.filter != "re" {
		t.Errorf("after backspace = %q, want re", m.filter)
	}
	// esc clears and exits to the board.
	after, _ = m.handleFilterKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = after.(model)
	if m.filter != "" || m.mode != modeBoard {
		t.Errorf("esc should clear+exit: filter=%q mode=%d", m.filter, m.mode)
	}
}

func TestStatusToast(t *testing.T) {
	// Info expires on the tick once it's older than infoTTL; an error never does.
	if !infoExpired(false, infoTTL+time.Second) {
		t.Error("info older than infoTTL should expire")
	}
	if infoExpired(false, time.Second) {
		t.Error("fresh info should not expire")
	}
	if infoExpired(true, time.Hour) {
		t.Error("errors must not info-expire on the tick")
	}

	// An error is kept across a switch until errorFloor; info is dropped on a switch.
	if !keepErrorOnSwitch(true, time.Second) {
		t.Error("a fresh error should survive a page switch")
	}
	if keepErrorOnSwitch(true, errorFloor+time.Second) {
		t.Error("an error past the floor should drop on a switch")
	}
	if keepErrorOnSwitch(false, time.Second) {
		t.Error("info should never be kept on a switch")
	}

	// dismissOnSwitch honors the floor: a fresh error stays, info clears.
	m := model{status: "boom", statusErr: true, statusAt: time.Now()}
	m.dismissOnSwitch()
	if m.status == "" {
		t.Error("fresh error should survive dismissOnSwitch")
	}
	m.statusAt = time.Now().Add(-errorFloor - time.Second)
	m.dismissOnSwitch()
	if m.status != "" {
		t.Error("error past the floor should clear on dismissOnSwitch")
	}
	info := model{status: "done", statusErr: false, statusAt: time.Now()}
	info.dismissOnSwitch()
	if info.status != "" {
		t.Error("info should clear on dismissOnSwitch")
	}
}

func TestGateSummary(t *testing.T) {
	home := t.TempDir() // built-in modes are embedded
	g := config.Gates{Push: "ask", ReadOnly: "ask"}

	ro := strings.Join(gateSummary(home, methodCtx{mode: "review"}, g), "\n")
	for _, want := range []string{"push", "read-only", "active: this mode can't edit the repo"} {
		if !strings.Contains(ro, want) {
			t.Errorf("read-only summary missing %q:\n%s", want, ro)
		}
	}
	edit := strings.Join(gateSummary(home, methodCtx{mode: "code"}, g), "\n")
	if !strings.Contains(edit, "applies only to read-only modes") {
		t.Errorf("edit-mode summary should note read-only is n/a:\n%s", edit)
	}
	if !strings.Contains(gateLine("push", "", "x"), "off") {
		t.Error("an empty gate action should render as off")
	}
}

func TestWithTitleReset(t *testing.T) {
	orig := exec.Command("tmux", "attach", "-t", "kovan-agent-x")
	w := withTitleReset(orig, "kovan")
	if len(w.Args) != 3 || w.Args[0] != "sh" || w.Args[1] != "-c" {
		t.Fatalf("want sh -c wrapper, got %v", w.Args)
	}
	script := w.Args[2]
	if !strings.Contains(script, "tmux") || !strings.Contains(script, "attach") {
		t.Errorf("script should run the original attach: %q", script)
	}
	// OSC 0 (icon + window title) resets the iTerm tab, not just OSC 2.
	if !strings.Contains(script, `printf '\033]0;kovan\007'`) {
		t.Errorf("script should reset the title via OSC 0: %q", script)
	}
}

func TestFormColorPicker(t *testing.T) {
	colors := append([]string{"none"}, rowTintNames...)
	f := formModel{inputs: make([]textinput.Model, 2), colors: colors}
	if f.selectedColor() != "" {
		t.Fatalf("default color = %q, want \"\" (none)", f.selectedColor())
	}
	// Without accounts, color sits right after mode.
	f.setFocus(len(f.inputs) + 4)
	if !f.onColor() {
		t.Fatal("expected focus on the color field")
	}
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyRight})
	if f.selectedColor() != rowTintNames[0] {
		t.Errorf("after right = %q, want %q", f.selectedColor(), rowTintNames[0])
	}
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyLeft})
	if f.selectedColor() != "" {
		t.Errorf("after left back to none = %q, want \"\"", f.selectedColor())
	}

	// With accounts configured, the color field shifts one slot down and the
	// brief stays last. (focus is set directly: setFocus would Focus a zero-
	// value textarea on the brief slot.)
	fa := formModel{inputs: make([]textinput.Model, 2), accounts: []string{"a"}, colors: colors}
	fa.focus = len(fa.inputs) + 5
	if !fa.onColor() {
		t.Fatal("expected focus on the color field after account")
	}
	fa.focus = fa.total() - 1
	if !fa.onBrief() || fa.onColor() {
		t.Fatal("the last slot should be the brief, not color")
	}
}
