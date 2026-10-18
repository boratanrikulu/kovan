package app

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/boratanrikulu/kovan/internal/agent"
	"github.com/boratanrikulu/kovan/internal/config"
)

// gateInput is everything the built-in gates need about a pending tool call.
// Passing scalars (not the manifest) keeps evalGates a pure, testable decision.
type gateInput struct {
	kind       agent.ToolKind
	command    string   // the shell command
	filePath   string   // the file an edit writes
	readOnly   bool     // the agent's mode is read-only
	worktree   string   // the agent workspace holding the edited file, to scope the posture gates
	writePaths []string // worktree-relative prefixes the mode may write to
	branch     string   // the agent's branch, for the default-branch commit gate (best-effort)
}

// gateResult is a gate's decision: the reason to show and the permission
// action. A zero reason means no gate matched.
type gateResult struct {
	reason string
	action string // "ask" | "deny"
}

// Reasons shown to the user when a gate matches.
const (
	pushReason          = "kovan: confirm before push/PR"
	defaultBranchReason = "kovan: commit on a protected branch — confirm or switch branches"
	readOnlyReason      = "kovan: read-only mode — write to your task docs, not the repo"
	writePathsReason    = "kovan: write outside the mode's write paths — confirm or write within them"
)

// ask is a built-in gate's decision: always the "ask" action.
func ask(reason string) gateResult {
	return gateResult{reason: reason, action: "ask"}
}

// evalGates returns the gate raised by a built-in or user gate, or a zero result
// when nothing matches.
func evalGates(in gateInput, gates config.Gates) gateResult {
	// Posture gates: a read-only mode editing any file inside its own worktree
	// (the repo) is gated; a path-scoped mode (write_paths set) is gated only
	// outside its allowed prefixes. Writes to the task-doc store (outside the
	// worktree, reached via --add-dir) pass, so the agent still writes its
	// artifact.
	if in.kind == agent.ToolEdit && withinWorktree(in.filePath, in.worktree) &&
		!withinWritePaths(in.filePath, in.worktree, in.writePaths) {
		if in.readOnly && gates.ReadOnly == "ask" {
			return ask(readOnlyReason)
		}
		if len(in.writePaths) > 0 && gates.WritePaths == "ask" {
			return ask(writePathsReason)
		}
	}

	if in.kind != agent.ToolShell {
		return gateResult{}
	}
	var results []gateResult
	for _, seg := range splitSegments(in.command) {
		results = append(results, matchSegment(seg, gates, in.branch))
	}
	return strongest(results)
}

// strongest is the decision a set of gate results adds up to: any deny wins,
// else the first ask, else none. Letting the first match win would let an ask
// for one part of a command cover a denied other part.
func strongest(results []gateResult) gateResult {
	var first gateResult
	for _, r := range results {
		if r.action == "deny" {
			return r
		}
		if first.reason == "" {
			first = r
		}
	}
	return first
}

// matchSegment recognizes a gated command in a single segment: a deny pattern
// first, since it outranks every ask; then the built-in git and gh/curl gates,
// then the remaining user patterns.
func matchSegment(seg string, gates config.Gates, branch string) gateResult {
	pattern := patternGate(seg, gates)
	if pattern.action == "deny" {
		return pattern
	}
	t := effectiveTokens(seg)
	if r := gitGate(t, gates, branch); r.reason != "" {
		return r
	}
	if r := ghCurlGate(seg, t, gates); r.reason != "" {
		return r
	}
	return pattern
}

// gitGate gates git push (the push gate) and git commit (the default-branch
// gate).
func gitGate(t []string, gates config.Gates, branch string) gateResult {
	if len(t) == 0 || t[0] != "git" {
		return gateResult{}
	}
	switch gitSubcommand(t) {
	case "push":
		if reason := pushGated(gates); reason != "" {
			return ask(reason)
		}
	case "commit":
		if reason := commitGated(gates, branch); reason != "" {
			return ask(reason)
		}
	}
	return gateResult{}
}

// ghCurlGate gates the non-git push paths: gh pr create, a writing gh api ref
// call, and a curl to the GitHub API. Best-effort, biased to recall.
func ghCurlGate(seg string, t []string, gates config.Gates) gateResult {
	if gates.Push != "ask" || len(t) == 0 {
		return gateResult{}
	}
	switch t[0] {
	case "gh":
		if len(t) >= 3 && t[1] == "pr" && t[2] == "create" {
			return ask(pushReason)
		}
		if len(t) >= 2 && t[1] == "api" && looksLikeWrite(seg) {
			return ask(pushReason)
		}
	case "curl":
		if strings.Contains(seg, "api.github.com") {
			return ask(pushReason)
		}
	}
	return gateResult{}
}

// looksLikeWrite reports whether a gh api / curl segment looks like a mutation
// rather than a read, so a plain read is not gated.
func looksLikeWrite(seg string) bool {
	up := strings.ToUpper(seg)
	for _, w := range []string{"-X", "--METHOD", "REFS", "POST", "PATCH", "PUT", "DELETE"} {
		if strings.Contains(up, w) {
			return true
		}
	}
	return false
}

// patternGate applies the user-defined gates: a deny among the patterns that
// match wins, wherever it sits in the list, else the first ask. A bad regexp
// is skipped, never fatal: a typo in config must not wedge every tool call.
func patternGate(seg string, gates config.Gates) gateResult {
	var results []gateResult
	for _, p := range gates.Patterns {
		action := p.Action
		if action == "" {
			action = "ask"
		}
		if action == "off" {
			continue
		}
		re, err := regexp.Compile(p.Match)
		if err != nil {
			continue
		}
		if re.MatchString(seg) {
			reason := p.Reason
			if reason == "" {
				reason = "kovan: gated command"
			}
			results = append(results, gateResult{reason: reason, action: action})
		}
	}
	return strongest(results)
}

// pushGated returns the push gate's reason when it is active, else "".
func pushGated(gates config.Gates) string {
	if gates.Push == "ask" {
		return pushReason
	}
	return ""
}

// commitGated returns the reason a commit is gated (a protected branch), or ""
// when it isn't.
func commitGated(gates config.Gates, branch string) string {
	if gates.DefaultBranch.Action == "ask" && isProtectedBranch(branch, gates.DefaultBranch.Branches) {
		return defaultBranchReason
	}
	return ""
}

// isProtectedBranch reports whether branch is one of the protected names.
func isProtectedBranch(branch string, protected []string) bool {
	for _, p := range protected {
		if branch == p {
			return true
		}
	}
	return false
}

// withinWorktree reports whether target is a file inside worktree. An empty or
// non-absolute target, or one outside the worktree, returns false — the gate
// fails open and never blocks on uncertainty.
func withinWorktree(target, worktree string) bool {
	if target == "" || worktree == "" || !filepath.IsAbs(target) {
		return false
	}
	rel, err := filepath.Rel(worktree, filepath.Clean(target))
	if err != nil {
		return false
	}
	// "." is the worktree root itself (not a file in it); ".."-prefixed escapes it.
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// withinWritePaths reports whether target falls under one of the mode's
// worktree-relative write paths (or is exactly one of them, so a single
// allowed file works too). An empty list allows nothing.
func withinWritePaths(target, worktree string, paths []string) bool {
	for _, p := range paths {
		allowed := filepath.Join(worktree, p)
		if filepath.Clean(target) == allowed || withinWorktree(target, allowed) {
			return true
		}
	}
	return false
}

// segmentSplit breaks a command on the chaining operators, newlines, and shell
// grouping, so a command after a newline, inside a subshell/brace group, or in a
// command substitution is seen, not just the first one. Deliberate obfuscation
// is out of scope (a non-adversarial agent), and ask is the safe outcome.
var segmentSplit = regexp.MustCompile(`&&|\|\||;|\||&|\n|\r|\(|\)|\{|\}`)

// splitSegments breaks a shell command into individually-checkable segments.
func splitSegments(command string) []string {
	var out []string
	for _, p := range segmentSplit.Split(command, -1) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// commandWrappers run another command given as their argument; the gated command
// hides behind them. They are peeled so the real command is classified.
var commandWrappers = map[string]bool{
	"eval": true, "xargs": true, "command": true, "env": true,
	"time": true, "nice": true, "sudo": true, "setsid": true, "stdbuf": true,
}

var shellRunners = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true}

// effectiveTokens returns the segment's real command as tokens, seeing past the
// shapes that hide it: quotes, leading env assignments, a path-prefixed binary,
// shell runners (bash -c '…'), and command wrappers (eval/xargs/env/…). The
// genuinely dynamic shapes (a shell or git alias, a $VAR binary) have no command
// here to recognize — a documented gap.
func effectiveTokens(seg string) []string {
	f := strings.Fields(stripQuotes(seg))
	for len(f) > 0 && isEnvAssign(f[0]) {
		f = f[1:]
	}
	if len(f) == 0 {
		return nil
	}
	cmd := unpath(f[0])
	if shellRunners[cmd] {
		if rest, ok := afterDashC(f[1:]); ok {
			return effectiveTokens(rest)
		}
	}
	if commandWrappers[cmd] {
		return effectiveTokens(strings.Join(f[1:], " "))
	}
	out := append([]string{cmd}, f[1:]...)
	return out
}

// afterDashC returns the command following a shell runner's -c flag, joined.
func afterDashC(args []string) (string, bool) {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			return strings.Join(args[i+1:], " "), true
		}
	}
	return "", false
}

// stripQuotes drops shell quote characters so a quoted command (bash -c 'git
// push') and a quoted argument (commit -m "fix push") tokenize as plain words.
func stripQuotes(s string) string {
	return strings.NewReplacer("'", "", `"`, "", "`", "").Replace(s)
}

// unpath strips a leading path from a binary, so /usr/bin/git and ./git read as
// git.
func unpath(tok string) string {
	if i := strings.LastIndexByte(tok, '/'); i >= 0 {
		return tok[i+1:]
	}
	return tok
}

// gitSubcommand returns git's real subcommand, skipping the global options that
// can precede it. -C and -c take a separate value token; --opt=val is
// self-contained; other flags stand alone.
func gitSubcommand(t []string) string {
	for i := 1; i < len(t); i++ {
		tok := t[i]
		if !strings.HasPrefix(tok, "-") {
			return tok
		}
		if tok == "-C" || tok == "-c" {
			i++ // option consumes the following value token
		}
	}
	return ""
}

func isEnvAssign(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	return eq > 0 && !strings.ContainsAny(tok[:eq], "/ ")
}
