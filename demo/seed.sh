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
mkdir -p "$HOME_DIR/method/global/skills/commit-style" \
	"$HOME_DIR/method/accounts/personal" \
	"$HOME_DIR/method/domains/code" \
	"$HOME_DIR/method/domains/notes" \
	"$HOME_DIR/modes/mentor" "$HOME_DIR/modes/finance" "$HOME_DIR/modes/publish" \
	"$HOME_DIR/projects/vault"

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

cat >"$HOME_DIR/method/global/skills/commit-style/SKILL.md" <<'EOF'
---
name: commit-style
description: Conventional commits, single line, imperative mood
---

# Commit style

`feat(scope): description` / `fix(scope): description`, lowercase subject,
no trailing period, one line for routine commits.
EOF

cat >"$HOME_DIR/method/accounts/personal/voice.md" <<'EOF'
# Personal account

Open-source voice: direct, technical, no marketing words. English for
anything public.
EOF

cat >"$HOME_DIR/method/domains/code/code.md" <<'EOF'
# Code domain

The suite is green before a task is called done. Format with the language's
standard tooling. Branches follow the repo's template; commits are
conventional.
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

# --- repos and worktrees -----------------------------------------------------
for repo in gecit gobee bpfvet durdur quik vault website; do
	make_repo "$repo"
done

# Public method + domain wiring for the repos the method view showcases:
# AGENTS.md is the project (public) layer, .kovan.yaml selects the domain.
cat >"$REPOS/gecit/AGENTS.md" <<'EOF'
# gecit

A network tool in Go. eBPF programs live under bpf/, the loader under
internal/. The verifier is the reviewer that matters: keep programs simple
enough to pass it.
EOF
printf 'domain: code\n' >"$REPOS/gecit/.kovan.yaml"
demo_git -C "$REPOS/gecit" add AGENTS.md .kovan.yaml
demo_git -C "$REPOS/gecit" commit -qm "docs: agents and kovan config"

cat >"$REPOS/vault/AGENTS.md" <<'EOF'
# vault

A notes vault, not a code repo. Markdown only; the folder layout is the
API. Agents work in-place on main and stay inside their mode's folders.
EOF
printf 'domain: notes\n' >"$REPOS/vault/.kovan.yaml"
demo_git -C "$REPOS/vault" add AGENTS.md .kovan.yaml
demo_git -C "$REPOS/vault" commit -qm "docs: agents and kovan config"

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
	"$HOME_DIR/projects/bpfvet/works/featreq" \
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
