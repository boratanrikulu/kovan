package app

import (
	"testing"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
)

func allGates() config.Gates {
	return config.Gates{
		Push:          "ask",
		ReadOnly:      "ask",
		WritePaths:    "ask",
		DefaultBranch: config.DefaultBranch{Action: "ask", Branches: []string{"main", "master"}},
	}
}

// TestPushMatrix pins the 21-shape bypass matrix from the analysis. The matcher
// must ask on the shapes it can see; a git/shell alias and a $VAR binary carry
// no command to recognize and are matcher-blind by design — a documented gap.
// Evaluated on a feature branch so only the push gate is in play.
func TestPushMatrix(t *testing.T) {
	matcherAsks := []string{
		"git push",
		"git add -A && git commit -m x && git push",
		"git -C /repo push",
		"git -c core.hooksPath=/dev/null push",
		"git add -A\ngit commit -m x\ngit push origin main", // newline chain
		"(git push)",                   // subshell
		"{ git push; }",                // brace group
		"bash -c 'git push'",           // shell runner
		"eval 'git push'",              // eval
		"$(git push)",                  // command substitution
		"/usr/bin/git push",            // absolute path
		"./git push",                   // ./ path
		"command git push",             // command builtin
		"env git push",                 // env prefix
		"echo origin | xargs git push", // xargs
		"gh pr create --fill",
		"gh api -X PATCH repos/o/r/git/refs/heads/main",
		"curl -X POST https://api.github.com/repos/o/r/merges",
	}
	for _, cmd := range matcherAsks {
		got := evalGates(gateInput{kind: agent.ToolShell, command: cmd, branch: "feat/x"}, allGates())
		if got.reason != pushReason {
			t.Errorf("matcher should ask on %q: reason = %q, want %q", cmd, got.reason, pushReason)
		}
	}

	// Matcher-blind by design: no command to recognize (alias / $VAR).
	blind := []string{"git pp", "g push", "$GIT push"}
	for _, cmd := range blind {
		got := evalGates(gateInput{kind: agent.ToolShell, command: cmd, branch: "feat/x"}, allGates())
		if got.reason != "" {
			t.Errorf("matcher should be blind to %q (documented gap): reason = %q", cmd, got.reason)
		}
	}

	// Innocent commands that must not trip the push gate.
	clean := []string{"git pull", "echo git push", "gh pr view", `git commit -m "fix push"`}
	for _, cmd := range clean {
		got := evalGates(gateInput{kind: agent.ToolShell, command: cmd, branch: "feat/x"}, allGates())
		if got.reason != "" {
			t.Errorf("matcher should pass %q: reason = %q", cmd, got.reason)
		}
	}
}

func TestSplitSegments(t *testing.T) {
	got := splitSegments("git pull && git push ; echo done | cat\n(git status)")
	want := []string{"git pull", "git push", "echo done", "cat", "git status"}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommitGated(t *testing.T) {
	g := allGates()
	cases := []struct {
		name   string
		branch string
		want   string
	}{
		{"protected branch main", "main", defaultBranchReason},
		{"protected branch master", "master", defaultBranchReason},
		{"feature branch", "feat/x", ""},
	}
	for _, c := range cases {
		if got := commitGated(g, c.branch); got != c.want {
			t.Errorf("%s: commitGated = %q, want %q", c.name, got, c.want)
		}
	}
	if got := commitGated(config.Gates{}, "main"); got != "" {
		t.Errorf("all-off commitGated = %q, want empty", got)
	}
}

func TestEvalGates(t *testing.T) {
	on := allGates()
	const ro = readOnlyReason
	const wt = "/repos/agent"

	cases := []struct {
		name  string
		in    gateInput
		gates config.Gates
		want  string
	}{
		{"push", gateInput{kind: agent.ToolShell, command: "git push origin main", branch: "feat/x"}, on, pushReason},
		{"pr create", gateInput{kind: agent.ToolShell, command: "gh pr create", branch: "feat/x"}, on, pushReason},
		{"commit on feature branch", gateInput{kind: agent.ToolShell, command: "git commit -m x", branch: "feat/x"}, on, ""},
		{"commit on protected branch", gateInput{kind: agent.ToolShell, command: "git commit -m x", branch: "main"}, on, defaultBranchReason},
		{"plain bash", gateInput{kind: agent.ToolShell, command: "echo hi"}, on, ""},
		{"edit-mode edit untouched", gateInput{kind: agent.ToolEdit, filePath: wt + "/main.go", worktree: wt}, on, ""},

		// Read-only posture.
		{"read-only edit in repo", gateInput{kind: agent.ToolEdit, filePath: wt + "/main.go", readOnly: true, worktree: wt}, on, ro},
		{"read-only write to task store", gateInput{kind: agent.ToolEdit, filePath: "/home/.kovan/projects/agent/works/TASK-1/review.md", readOnly: true, worktree: wt}, on, ""},
		{"read-only read is fine", gateInput{kind: agent.ToolOther, filePath: wt + "/main.go", readOnly: true, worktree: wt}, on, ""},
		{"read-only bash untouched", gateInput{kind: agent.ToolShell, command: "git diff", readOnly: true, worktree: wt}, on, ""},

		// Path-scoped posture (write_paths).
		{"scoped write inside allowed path", gateInput{kind: agent.ToolEdit, filePath: wt + "/allowed/log.md", writePaths: []string{"allowed/"}, worktree: wt}, on, ""},
		{"scoped write to allowed file itself", gateInput{kind: agent.ToolEdit, filePath: wt + "/allowed/log.md", writePaths: []string{"allowed/log.md"}, worktree: wt}, on, ""},
		{"scoped write outside allowed path", gateInput{kind: agent.ToolEdit, filePath: wt + "/private/notes.md", writePaths: []string{"allowed/"}, worktree: wt}, on, writePathsReason},
		{"scoped write escaping via ..", gateInput{kind: agent.ToolEdit, filePath: wt + "/allowed/../secret.md", writePaths: []string{"allowed/"}, worktree: wt}, on, writePathsReason},
		{"scoped write to task store", gateInput{kind: agent.ToolEdit, filePath: "/home/.kovan/projects/agent/works/TASK-1/report.md", writePaths: []string{"allowed/"}, worktree: wt}, on, ""},
		{"scoped read is fine", gateInput{kind: agent.ToolOther, filePath: wt + "/main.go", writePaths: []string{"allowed/"}, worktree: wt}, on, ""},
		{"read-only with carve-out, inside", gateInput{kind: agent.ToolEdit, filePath: wt + "/allowed/log.md", readOnly: true, writePaths: []string{"allowed/"}, worktree: wt}, on, ""},
		{"read-only with carve-out, outside", gateInput{kind: agent.ToolEdit, filePath: wt + "/main.go", readOnly: true, writePaths: []string{"allowed/"}, worktree: wt}, on, ro},
		{"write-paths gate disabled", gateInput{kind: agent.ToolEdit, filePath: wt + "/main.go", writePaths: []string{"kovan/"}, worktree: wt}, config.Gates{WritePaths: "off"}, ""},

		{"push disabled", gateInput{kind: agent.ToolShell, command: "git push", branch: "feat/x"}, config.Gates{Push: "off"}, ""},
	}
	for _, c := range cases {
		got := evalGates(c.in, c.gates)
		if got.reason != c.want {
			t.Errorf("%s: reason = %q, want %q", c.name, got.reason, c.want)
		}
	}
}

func TestUserPatternGate(t *testing.T) {
	g := config.Gates{Patterns: []config.Pattern{
		{Match: `terraform\s+apply`, Action: "ask", Reason: "kovan: confirm terraform apply"},
		{Match: `rm\s+-rf\s+/`, Action: "deny", Reason: "kovan: refusing rm -rf /"},
	}}
	ask := evalGates(gateInput{kind: agent.ToolShell, command: "terraform apply -auto-approve"}, g)
	if ask.reason == "" || ask.action != "ask" {
		t.Errorf("terraform pattern: %+v", ask)
	}
	deny := evalGates(gateInput{kind: agent.ToolShell, command: "rm -rf /"}, g)
	if deny.action != "deny" {
		t.Errorf("rm pattern: action = %q, want deny", deny.action)
	}
	// A bad regexp must be skipped, not fatal.
	bad := config.Gates{Patterns: []config.Pattern{{Match: `([`, Action: "ask", Reason: "x"}}}
	if got := evalGates(gateInput{kind: agent.ToolShell, command: "echo hi"}, bad); got.reason != "" {
		t.Errorf("bad regexp should be skipped: %+v", got)
	}
}

func TestWithinWorktree(t *testing.T) {
	const wt = "/repos/agent"
	cases := []struct {
		target string
		want   bool
	}{
		{wt + "/main.go", true},
		{wt + "/pkg/x/y.go", true},
		{wt, false},                     // the root itself is not "inside"
		{"/repos/other/main.go", false}, // sibling repo
		{"/home/.kovan/projects/agent/works/TASK-1/review.md", false}, // the task store
		{wt + "/../escape.go", false},                                 // escapes the worktree
		{"main.go", false},                                            // relative → fail open
		{"", false},
	}
	for _, c := range cases {
		if got := withinWorktree(c.target, wt); got != c.want {
			t.Errorf("withinWorktree(%q) = %v, want %v", c.target, got, c.want)
		}
	}
}
