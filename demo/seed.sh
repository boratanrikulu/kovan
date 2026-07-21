#!/bin/sh
# Seed a self-contained kovan demo world for README screenshots: a throwaway
# KOVAN_HOME full of fake agents, tiny git repos, and dummy tmux sessions.
# Everything lives under /tmp/kovan-demo (KOVAN_DEMO_ROOT overrides); the real
# ~/.kovan is never touched. Idempotent: each run starts from a clean root.
set -eu

ROOT="${KOVAN_DEMO_ROOT:-/tmp/kovan-demo}"
HOME_DIR="$ROOT/home"
REPOS="$ROOT/repos"
TREES="$ROOT/worktrees"
PANES="$ROOT/panes"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- start clean -------------------------------------------------------------
tmux ls -F '#S' 2>/dev/null | grep '^kovan-demo-' | while read -r s; do
	tmux kill-session -t "$s"
done
rm -rf "$ROOT"
mkdir -p "$HOME_DIR/sessions" "$HOME_DIR/tokens" "$REPOS" "$TREES" "$PANES"

# --- helpers -----------------------------------------------------------------
# RFC3339 UTC timestamp $1 minutes ago (BSD date first, GNU date fallback).
ago() {
	if date -u -v-1M +%s >/dev/null 2>&1; then
		date -u -v-"$1"M +%Y-%m-%dT%H:%M:%SZ
	else
		date -u -d "$1 minutes ago" +%Y-%m-%dT%H:%M:%SZ
	fi
}
NOW="$(ago 0)"

demo_git() {
	git -c user.name=Demo -c user.email=demo@example.com -c commit.gpgsign=false "$@"
}

# make_repo <name>: a tiny git repo with a couple of generic commits.
make_repo() {
	r="$REPOS/$1"
	git init -q -b main "$r"
	printf '# %s\n\ndemo checkout for the kovan board.\n' "$1" >"$r/README.md"
	demo_git -C "$r" add README.md
	demo_git -C "$r" commit -qm "docs: readme"
	mkdir -p "$r/internal"
	printf 'package internal\n' >"$r/internal/doc.go"
	demo_git -C "$r" add internal
	demo_git -C "$r" commit -qm "chore: scaffold"
}

# make_tree <repo> <id> <branch>: the agent's worktree on its own branch.
make_tree() {
	git -C "$REPOS/$1" worktree add -q "$TREES/$2" -b "$3"
}

# skill <layer-dir> <name> <description>: a SKILL.md under <layer-dir>/skills/.
# What the method view lists as "skill: <name>" for that layer.
skill() {
	d="$1/skills/$2"
	mkdir -p "$d"
	printf -- '---\nname: %s\ndescription: %s\n---\n\n# %s\n\n%s\n' \
		"$2" "$3" "$2" "$3" >"$d/SKILL.md"
}

# --- demo home ---------------------------------------------------------------
printf 'demo-token-not-real\n' >"$HOME_DIR/tokens/personal"
chmod 600 "$HOME_DIR/tokens/personal"

cat >"$HOME_DIR/config.yaml" <<EOF
runner: tmux
agent: claude
notify: macos
accounts:
  personal: { token_file: $HOME_DIR/tokens/personal }
default_account: personal
EOF

# --- method layers (what the method view composes) ----------------------------
# The layers stack general → specific: global (every agent), account, domain,
# per-repo private, mode, the repo's own public files, then the task docs.
# Each layer can carry method files and skills; the demo wires the code path
# (gecit) richly so the method view shows the full composition.
mkdir -p "$HOME_DIR/method/global" \
	"$HOME_DIR/method/accounts/personal" \
	"$HOME_DIR/method/domains/notes" \
	"$HOME_DIR/modes/mentor" "$HOME_DIR/modes/finance" "$HOME_DIR/modes/publish" "$HOME_DIR/modes/qa" \
	"$HOME_DIR/projects/gecit" "$HOME_DIR/projects/gobee" "$HOME_DIR/projects/bpfvet" "$HOME_DIR/projects/durdur" "$HOME_DIR/projects/quik" "$HOME_DIR/projects/website" "$HOME_DIR/projects/vault"

cat >"$HOME_DIR/method/global/methodology.md" <<'EOF'
# Methodology

Universal working rules. Every repo, every agent.

- Plan before non-trivial work: write the spec, get a go, then build.
- Bug fixing is test-first: reproduce with a failing test, fix it green.
- Short functions, clear over clever, no unnecessary abstractions.
- Comments describe the current state, never the journey.
EOF

cat >"$HOME_DIR/method/global/delegation.md" <<'EOF'
# Delegation

Hand over the end goal, not the steps. Review what comes back before it
lands anywhere; a sub-agent's result is a lead, not a fact.
EOF

cat >"$HOME_DIR/method/global/soul.md" <<'EOF'
# Soul

Who you are across everything: a systems engineer who likes small, sharp
tools. Practical over trendy. Honest about trade-offs. One binary, one
command, no config files where you can help it.
EOF

mkdir -p "$HOME_DIR/method/global/skills/commit-style"
cat >"$HOME_DIR/method/global/skills/commit-style/SKILL.md" <<'EOF'
---
name: commit-style
description: Conventional commits, single line, imperative mood
---

# Commit style

`feat(scope): description` / `fix(scope): description`, lowercase subject,
no trailing period, one line for routine commits.
EOF

skill "$HOME_DIR/method/global" spec-writing \
	"Write the spec before code: plan, tasks, open questions; get a go first"

cat >"$HOME_DIR/method/accounts/personal/voice.md" <<'EOF'
# Personal account

Open-source voice: direct, technical, no marketing words. English for
anything public.
EOF

skill "$HOME_DIR/method/accounts/personal" oss-triage \
	"Triage an incoming issue: reproduce, label, ask for the missing detail"
skill "$HOME_DIR/method/accounts/personal" release-notes \
	"Cut release notes from the merged PRs since the last tag"

# project (private) layer for gecit — the code path the method view showcases:
# the repo's private notes plus repo-specific skills, from a different source
# than the global and account layers above.
cat >"$HOME_DIR/projects/gecit/context.md" <<'EOF'
# gecit — private notes

Not committed to the repo. The BPF objects are prebuilt into internal/bpf/;
regenerate them with the build skill, never by hand. The verifier is the
real reviewer — if a program grows past what it proves, split the function,
don't fight the checker.
EOF

skill "$HOME_DIR/projects/gecit" bpf-build \
	"Rebuild the BPF objects with clang and vendor them into internal/bpf/"
skill "$HOME_DIR/projects/gecit" verifier-debug \
	"Read a verifier rejection: find the lost range, trace it to the branch"
skill "$HOME_DIR/projects/gecit" flow-trace \
	"Trace a packet through the sock_ops path with bpftrace"
skill "$HOME_DIR/projects/gecit" xdp-bench \
	"Benchmark the datapath: pps and p99 latency against the baseline"

cat >"$HOME_DIR/projects/gobee/context.md" <<'EOF'
# gobee — private notes

The verifier is the oracle. When generated code is rejected, reduce to the
smallest failing program and read the verifier log top-down; the first lost
range is the bug, not the last.
EOF

skill "$HOME_DIR/projects/gobee" codegen-reduce \
	"Reduce a rejected program to the smallest shape that still fails"
skill "$HOME_DIR/projects/gobee" verifier-log \
	"Read a kernel verifier log: map the rejection back to the emitted insn"

cat >"$HOME_DIR/projects/bpfvet/context.md" <<'EOF'
# bpfvet — private notes

Every check ships with a passing and a failing fixture under testdata/.
Never loosen a check to make a real program pass; fix the program or the
check's model of the rule.
EOF

skill "$HOME_DIR/projects/bpfvet" check-fixture \
	"Add a check with its passing and failing fixtures"

cat >"$HOME_DIR/projects/durdur/context.md" <<'EOF'
# durdur — private notes

Policy-map versioning: bump the version byte and add a loader path, never
edit the existing map shape. A running host reloads the policy live, so a
bad layout takes the firewall down.
EOF

cat >"$HOME_DIR/projects/quik/context.md" <<'EOF'
# quik — private notes

pion is pinned in go.mod; a bump touches the SDP munging in signal.go. Revive
the demo room after any bump and watch for a renegotiation loop.
EOF

cat >"$HOME_DIR/projects/website/context.md" <<'EOF'
# website — private notes

Posts live under content/posts/, assets under static/img/ (resized to
1600px on the way in). The publish step is manual on purpose: staging is
safe, live is not.
EOF

cat >"$HOME_DIR/method/domains/notes/notes.md" <<'EOF'
# Notes domain

Plain markdown, dated entries, wiki-links between notes. Never restructure
folders without asking; append, don't rewrite.
EOF

cat >"$HOME_DIR/projects/vault/context.md" <<'EOF'
# vault — private notes

The vault's layout: journal/ for dailies, finance/ for money notes,
mentor/ for check-ins. Reports go under the topic's reports/ folder.
EOF

cat >"$HOME_DIR/modes/mentor/method.md" <<'EOF'
# Mentor method

Read the period's notes, reflect, leave a dated check-in. Observations over
advice; flag patterns, don't prescribe. Write only under mentor/.
EOF

cat >"$HOME_DIR/modes/finance/method.md" <<'EOF'
# Finance method

Numbers come from the notes, never from memory. Draft reports with totals
and deltas; flag anomalies for a human call. Never act on money decisions.
EOF

cat >"$HOME_DIR/modes/publish/method.md" <<'EOF'
# Publish method

Prepare everything, publish nothing. Stage the post, resize the assets,
write the captions, then stop for a go before anything is live.
EOF

cat >"$HOME_DIR/modes/qa/method.md" <<'EOF'
# QA method

Run the real thing, don't reason about it. Every check is a command and its
observed result: pass, fail, or blocked. Report the matrix; never fix from
inside the QA run.
EOF

# --- repos and worktrees -----------------------------------------------------
for repo in gecit gobee bpfvet durdur quik vault website; do
	make_repo "$repo"
done

# Public method + domain wiring for the repos the method view showcases: the
# repo's own committed method files are the project (public) layer, .kovan.yaml
# selects the domain. gecit's CLAUDE.md @imports docs/AGENTS.md, so the method
# view shows the import nested under it (the ↳ in the public layer).
mkdir -p "$REPOS/gecit/docs"
cat >"$REPOS/gecit/CLAUDE.md" <<'EOF'
# gecit

A network tool in Go. This file is the entry point the agent always loads;
the architecture detail lives in the imported doc.

@docs/AGENTS.md
EOF
cat >"$REPOS/gecit/docs/AGENTS.md" <<'EOF'
# gecit — architecture

eBPF programs live under bpf/, the loader under internal/. The verifier is
the reviewer that matters: keep programs simple enough to pass it.
EOF
printf 'domain: code\n' >"$REPOS/gecit/.kovan.yaml"
demo_git -C "$REPOS/gecit" add CLAUDE.md docs/AGENTS.md .kovan.yaml
demo_git -C "$REPOS/gecit" commit -qm "docs: claude entry, agents, kovan config"

cat >"$REPOS/vault/AGENTS.md" <<'EOF'
# vault

A notes vault, not a code repo. Markdown only; the folder layout is the
API. Agents work in-place on main and stay inside their mode's folders.
EOF
printf 'domain: notes\n' >"$REPOS/vault/.kovan.yaml"
demo_git -C "$REPOS/vault" add AGENTS.md .kovan.yaml
demo_git -C "$REPOS/vault" commit -qm "docs: agents and kovan config"

# The remaining repos each get a public AGENTS.md and a domain, so every
# agent's method view has a project (public) layer, not just gecit and vault.
pub() { # pub <repo> <domain>: commit the AGENTS.md already written + a domain.
	printf 'domain: %s\n' "$2" >"$REPOS/$1/.kovan.yaml"
	demo_git -C "$REPOS/$1" add AGENTS.md .kovan.yaml
	demo_git -C "$REPOS/$1" commit -qm "docs: agents and kovan config"
}

cat >"$REPOS/gobee/AGENTS.md" <<'EOF'
# gobee

A BPF codegen library in Go: it emits eBPF bytecode that must pass the kernel
verifier. Correctness is the verifier's verdict — if generated code is
rejected, the bug is in codegen, not the program.
EOF
pub gobee code

cat >"$REPOS/bpfvet/AGENTS.md" <<'EOF'
# bpfvet

A static analyzer for BPF objects: it reports what a program needs and where
it breaks the rules. Every finding carries a file:line pointer; a finding you
can't locate is not a finding.
EOF
pub bpfvet code

cat >"$REPOS/durdur/AGENTS.md" <<'EOF'
# durdur

An eBPF firewall. Rules compile to a policy map the datapath enforces. Never
change the map layout without a migration path; a running host reloads it
live.
EOF
pub durdur code

cat >"$REPOS/quik/AGENTS.md" <<'EOF'
# quik

A WebRTC library over pion. The demo room is the integration test: if it
connects and streams, the stack is healthy. pion is pinned, bumps are
deliberate.
EOF
pub quik code

cat >"$REPOS/website/AGENTS.md" <<'EOF'
# website

A static site. Posts are markdown, assets are resized on the way in. Nothing
goes live without a human go: publishing is the last, explicit step.
EOF
pub website notes

make_tree gecit ipv6 feat/ipv6-sock-ops
make_tree gobee mapiter fix/map-iteration-verifier
make_tree bpfvet featreq review/feature-requirements
make_tree gecit tun-qa qa/tun-macos
make_tree gobee o2 analyze/o2-verifier
make_tree durdur relnotes write/v0.2-notes
make_tree quik pion spike/pion-bump
make_tree website photos publish/photo-post
make_tree bpfvet core-checks feat/core-reloc-checks
# weekly and budget run in-place in the vault checkout (a shared workspace).

# --- session manifests ---------------------------------------------------------
cat >"$HOME_DIR/sessions/kovan-demo-ipv6.yaml" <<EOF
id: ipv6
title: handle IPv6 flows on the sock_ops path
repo: gecit
repo_root: $REPOS/gecit
worktree: $TREES/ipv6
branch: feat/ipv6-sock-ops
base: main
tmux: kovan-demo-ipv6
agent: claude
account: personal
state: working
pinned: true
color: cyan
mode: auto
task_mode: code
summary: >-
  The agent is wiring IPv6 tuple extraction into the sock_ops redirect path;
  parsing and map plumbing are done and it is heading into the integration
  tests. Nothing needs you.
summary_at: $NOW
last_activity: $(ago 2)
created_at: $(ago 130)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-mapiter.yaml" <<EOF
id: mapiter
title: fix verifier rejection on map iteration
repo: gobee
repo_root: $REPOS/gobee
worktree: $TREES/mapiter
branch: fix/map-iteration-verifier
base: main
tmux: kovan-demo-mapiter
agent: claude
account: personal
state: needs-you
pinned: true
color: orange
mode: auto
task_mode: code
summary: >-
  It reproduced the verifier rejection and has two candidate codegen shapes
  for the map iteration; it needs you to pick between the bounded-loop
  rewrite and the callback form before it goes on.
summary_at: $NOW
last_activity: $(ago 18)
created_at: $(ago 300)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-featreq.yaml" <<EOF
id: featreq
title: "review: feature-requirement extraction"
repo: bpfvet
repo_root: $REPOS/bpfvet
worktree: $TREES/featreq
branch: review/feature-requirements
base: main
tmux: kovan-demo-featreq
agent: claude
account: personal
state: idle
mode: default
task_mode: review
summary: >-
  It finished the review and wrote the findings table to review.md, two
  medium and one low. Idle until you want the findings discussed or posted.
summary_at: $NOW
last_activity: $(ago 25)
created_at: $(ago 65)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-tun-qa.yaml" <<EOF
id: tun-qa
title: verify TUN device setup on macOS
repo: gecit
repo_root: $REPOS/gecit
worktree: $TREES/tun-qa
branch: qa/tun-macos
base: main
tmux: kovan-demo-tun-qa
agent: claude
account: personal
state: working
color: green
mode: auto
task_mode: qa
summary: >-
  It is working through the TUN setup matrix on macOS, three of five checks
  green so far; route install and teardown checks still to run. Nothing
  needs you yet.
summary_at: $NOW
last_activity: $(ago 1)
created_at: $(ago 45)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-o2.yaml" <<EOF
id: o2
title: why -O2 output fails the verifier
repo: gobee
repo_root: $REPOS/gobee
worktree: $TREES/o2
branch: analyze/o2-verifier
base: main
tmux: kovan-demo-o2
agent: claude
state: idle
mode: auto
task_mode: analyze
summary: >-
  It answered the question in analysis.md, the -O2 output spills the
  bounds-checked register so the verifier loses the proven range, with
  instruction-level evidence. Idle, waiting for you to read it.
summary_at: $NOW
last_activity: $(ago 55)
created_at: $(ago 1560)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-relnotes.yaml" <<EOF
id: relnotes
title: draft release notes for v0.2
repo: durdur
repo_root: $REPOS/durdur
worktree: $TREES/relnotes
branch: write/v0.2-notes
base: main
tmux: kovan-demo-relnotes
agent: claude
account: personal
state: needs-you
color: yellow
mode: default
task_mode: write
summary: >-
  The draft of the v0.2 release notes is ready in draft.md; it needs a voice
  check from you on the opening paragraph before it calls the text final.
summary_at: $NOW
last_activity: $(ago 35)
created_at: $(ago 185)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-pion.yaml" <<EOF
id: pion
title: "spike: bump pion, revive the demo room"
repo: quik
repo_root: $REPOS/quik
worktree: $TREES/pion
branch: spike/pion-bump
base: main
tmux: kovan-demo-pion
agent: claude
state: idle
mode: auto
task_mode: code
summary: >-
  It mapped the pion API changes and sketched the upgrade path, then
  stopped before reviving the demo room.
summary_at: $NOW
last_activity: $(ago 2600)
created_at: $(ago 2900)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-weekly.yaml" <<EOF
id: weekly
title: weekly review
repo: vault
repo_root: $REPOS/vault
worktree: $REPOS/vault
branch: main
base: main
in_place: true
tmux: kovan-demo-weekly
agent: claude
account: personal
state: idle
mode: auto
task_mode: mentor
summary: >-
  It read the week's notes and left a dated check-in in the vault. The
  agent is idle; nothing needs you.
summary_at: $NOW
last_activity: $(ago 90)
created_at: $(ago 480)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-budget.yaml" <<EOF
id: budget
title: monthly budget report
repo: vault
repo_root: $REPOS/vault
worktree: $REPOS/vault
branch: main
base: main
in_place: true
tmux: kovan-demo-budget
agent: claude
account: personal
state: needs-you
pinned: true
color: blue
mode: auto
task_mode: finance
summary: >-
  The monthly budget report is drafted; one recurring subscription looks
  unused and it needs a keep-or-cancel call from you to finish.
summary_at: $NOW
last_activity: $(ago 40)
created_at: $(ago 1470)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-photos.yaml" <<EOF
id: photos
title: "photo post: pick, caption, publish"
repo: website
repo_root: $REPOS/website
worktree: $TREES/photos
branch: publish/photo-post
base: main
tmux: kovan-demo-photos
agent: claude
account: personal
state: working
mode: auto
task_mode: publish
summary: >-
  It is resizing the shortlisted photos for the post; captions and the
  publish step come next. Nothing needs you.
summary_at: $NOW
last_activity: $(ago 3)
created_at: $(ago 30)
EOF

cat >"$HOME_DIR/sessions/kovan-demo-core-checks.yaml" <<EOF
id: core-checks
title: add CO-RE relocation checks
repo: bpfvet
repo_root: $REPOS/bpfvet
worktree: $TREES/core-checks
branch: feat/core-reloc-checks
base: main
tmux: kovan-demo-core-checks
agent: claude
account: personal
state: idle
archived: true
mode: auto
task_mode: code
summary: >-
  It shipped the CO-RE relocation checks and the suite is green; the task
  is done and archived.
summary_at: $NOW
last_activity: $(ago 4200)
created_at: $(ago 4300)
EOF

# --- task docs (the method view's task layer) --------------------------------
mkdir -p "$HOME_DIR/projects/gecit/works/ipv6" \
	"$HOME_DIR/projects/gecit/works/tun-qa" \
	"$HOME_DIR/projects/gobee/works/mapiter" \
	"$HOME_DIR/projects/gobee/works/o2" \
	"$HOME_DIR/projects/bpfvet/works/featreq" \
	"$HOME_DIR/projects/durdur/works/relnotes" \
	"$HOME_DIR/projects/quik/works/pion" \
	"$HOME_DIR/projects/website/works/photos" \
	"$HOME_DIR/projects/vault/works/weekly" \
	"$HOME_DIR/projects/vault/works/budget"

cat >"$HOME_DIR/projects/gecit/works/ipv6/context.md" <<'EOF'
# ipv6 — handle IPv6 flows on the sock_ops path

## Summary

The sock_ops path only extracts IPv4 tuples today, so IPv6 flows fall through
to the slow path. Add v6 tuple extraction and wire it into the redirect map,
keeping the v4 fast path untouched.

## Decisions

- Extend the existing tuple struct instead of adding a parallel v6 one; the
  map key grows but stays a single shape.
- Dual-stack sockets resolve to their actual address family at extraction
  time, no v4-mapped special case in the map.

## Key Files

- internal/sockops/tuple.go — extraction
- internal/sockops/redirect.go — map wiring

## Status

Extraction and map plumbing done, tests green. Wiring the redirect path,
then the integration matrix.
EOF

cat >"$HOME_DIR/projects/gecit/works/ipv6/learnings.md" <<'EOF'
# Learnings

- The verifier needs the address-family branch to stay in one function;
  splitting it into a helper loses the range it proved on the packet
  pointer.
EOF

cat >"$HOME_DIR/projects/gecit/works/ipv6/spec.md" <<'EOF'
# ipv6 — spec

## Plan

Add IPv6 tuple extraction to the sock_ops path and wire it into the redirect
map, leaving the IPv4 fast path untouched.

## Tasks

- [x] Extend the tuple struct with the v6 addresses.
- [x] Extract the family at runtime, no v4-mapped special case.
- [ ] Wire the redirect map lookup.
- [ ] Integration matrix across v4, v6, dual-stack.

## Open questions

- None outstanding; the map-key growth was approved.
EOF

cat >"$HOME_DIR/projects/gecit/works/ipv6/test-plan.md" <<'EOF'
# ipv6 — test plan

- Unit: tuple extraction for v4, v6, and dual-stack sockets.
- Verifier: the extraction function passes with the range preserved.
- Integration: a v6 flow takes the redirect fast path end to end.
EOF

cat >"$HOME_DIR/projects/bpfvet/works/featreq/context.md" <<'EOF'
# featreq — review: feature-requirement extraction

## Summary

Review the feature-requirement extraction pass: does it find every helper
and map feature a program depends on, and does it report them at the right
source locations?

## Decisions

- Findings only, no patches; severity-ranked table in review.md.

## Key Files

- internal/extract/requirements.go — the pass under review

## Status

Review finished: 2 medium, 1 low, table in review.md with file:line
pointers. Waiting to discuss.
EOF

cat >"$HOME_DIR/projects/vault/works/budget/context.md" <<'EOF'
# budget — monthly budget report

## Summary

Draft the monthly budget report from the vault's finance notes: totals,
category deltas against last month, and the recurring-subscription list
with anything that looks unused flagged.

## Decisions

- Flag, don't cancel: subscription calls stay with the human.

## Key Files

- finance/subscriptions.md
- finance/reports/monthly.md — the report

## Status

Report drafted. One flagged subscription needs a keep-or-cancel call, then
the report is final.
EOF

# Task docs for the rest of the active agents, so every agent's task layer
# has content (the mode's own docs: spec/test-plan for code, review.md for a
# review, analysis.md for analyze, draft.md for write).
cat >"$HOME_DIR/projects/gobee/works/mapiter/context.md" <<'EOF'
# mapiter — fix verifier rejection on map iteration

## Summary

The verifier rejects the generated map-iteration loop: it loses the proven
bound on the iterator, so the array access reads as unbounded. Reshape the
codegen so the bound survives.

## Decisions

- Two candidates on the table: a bounded-loop rewrite and a callback form.
  Waiting on a pick before implementing.

## Status

Rejection reproduced with a minimal program. Both codegen shapes sketched;
needs a decision.
EOF

cat >"$HOME_DIR/projects/gobee/works/mapiter/spec.md" <<'EOF'
# mapiter — spec

## Plan

Emit the map iteration so the verifier keeps the iterator's range across the
loop body, without changing the public codegen API.

## Tasks

- [x] Reproduce the rejection with a minimal program.
- [ ] Pick the loop shape (bounded rewrite vs callback).
- [ ] Regenerate the corpus and re-run the verifier suite.

## Open questions

- Bounded rewrite keeps one function; the callback form is smaller but adds
  an indirect call. Which do you want?
EOF

cat >"$HOME_DIR/projects/gobee/works/o2/context.md" <<'EOF'
# o2 — why -O2 output fails the verifier

## Summary

At -O2 the compiler spills a bounds-checked register, so the verifier loses
the proven range and rejects the later access. Explain exactly where, with
instruction-level evidence. Analysis only, no code changes.

## Status

Answered in analysis.md with the spill site and the lost-range chain. Idle.
EOF

cat >"$HOME_DIR/projects/gobee/works/o2/analysis.md" <<'EOF'
# o2 — analysis

The bound is proven at the compare, then the register is spilled to the
stack across a call and reloaded without the range. The verifier treats the
reload as unbounded, so the map access fails.

- codegen/emit.go:212 — the compare that proves the range
- codegen/emit.go:240 — the spill that drops it

Fix direction (out of scope here): pin the checked value in a callee-saved
register, or re-prove the bound after the reload.
EOF

cat >"$HOME_DIR/projects/gecit/works/tun-qa/context.md" <<'EOF'
# tun-qa — verify TUN device setup on macOS

## Summary

Run the TUN setup matrix on macOS: device create, address assign, route
install, teardown, and a re-create after an unclean exit. Report pass/fail
per step with the commands used.

## Status

Three of five green. Route install and teardown still to run.
EOF

cat >"$HOME_DIR/projects/durdur/works/relnotes/context.md" <<'EOF'
# relnotes — draft release notes for v0.2

## Summary

Draft the v0.2 release notes from the merged PRs since v0.1: group by
feature/fix, lead with the policy-map migration, keep the voice plain.

## Status

Draft ready in draft.md. Needs a voice check on the opening before it's
final.
EOF

cat >"$HOME_DIR/projects/durdur/works/relnotes/draft.md" <<'EOF'
# durdur v0.2

The big one is live policy reloads: durdur now swaps its rule map without
dropping the datapath, so a ruleset change no longer blips the firewall.

- feat: live policy-map reload with a versioned layout
- feat: per-rule counters exposed on the status socket
- fix: teardown left a dangling qdisc on an unclean exit
EOF

cat >"$HOME_DIR/projects/quik/works/pion/context.md" <<'EOF'
# pion — spike: bump pion, revive the demo room

## Summary

Scope a pion bump: what breaks in the SDP munging, and whether the demo room
still connects after. A spike, not a merge — map the work, don't land it.

## Status

API changes mapped, upgrade path sketched. Stopped before touching signal.go.
EOF

cat >"$HOME_DIR/projects/website/works/photos/context.md" <<'EOF'
# photos — photo post: pick, caption, publish

## Summary

Build a photo post: shortlist the shots, resize to the site's max width,
write captions, stage the post. Stop before publishing.

## Status

Resizing the shortlist. Captions and staging next; nothing goes live without
a go.
EOF

cat >"$HOME_DIR/projects/vault/works/weekly/context.md" <<'EOF'
# weekly — weekly review

## Summary

Read the week's notes and leave a dated check-in: what moved, what stalled,
a pattern or two. Observations, not advice. Write only under mentor/.

## Status

Check-in written for the week. Idle.
EOF

cat >"$HOME_DIR/projects/bpfvet/works/featreq/review.md" <<'EOF'
# featreq — review findings

| # | severity | location | finding |
|---|---|---|---|
| 1 | medium | internal/extract/requirements.go:88 | a tail-call target's features are not folded into the caller's set |
| 2 | medium | internal/extract/requirements.go:140 | map-in-map inner features are missed |
| 3 | low | internal/extract/requirements.go:31 | helper id reported without the program section |

Findings only, no patches. Waiting to discuss before anything is posted.
EOF

# --- pane tails + dummy tmux sessions -----------------------------------------
cp "$SCRIPT_DIR/panes/"*.txt "$PANES/"

for id in ipv6 mapiter featreq tun-qa o2 relnotes weekly budget photos; do
	tmux new-session -d -s "kovan-demo-$id" "cat '$PANES/$id.txt'; sleep 3600"
done
# pion has no session on purpose: the board shows it as stopped.

# --- done ----------------------------------------------------------------------
# Prefer the repo's freshly built binary: an older installed kovan may
# predate behavior the demo depends on.
KOVAN_BIN=kovan
if [ -x "$SCRIPT_DIR/../bin/kovan" ]; then
	KOVAN_BIN="$SCRIPT_DIR/../bin/kovan"
fi

echo "demo seeded under $ROOT"
echo
echo "run the cockpit:"
echo "  cd $REPOS/gecit && KOVAN_HOME=$HOME_DIR $KOVAN_BIN"
echo
echo "plain board:"
echo "  KOVAN_HOME=$HOME_DIR $KOVAN_BIN status"
echo
echo "tear down:"
echo "  $SCRIPT_DIR/teardown.sh"
