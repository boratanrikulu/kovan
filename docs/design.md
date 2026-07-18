# Design

How kovan works underneath, and why it's shaped this way. The short version:
two pillars. Your method lives in one place and reaches every agent; agents
run in parallel and a board tells you the moment one is yours to unblock.

## The two pillars

**Pillar 1, the method.** Everything about how you work (your rules, your
voice, per-repo context, task workflows) lives once, under `~/.kovan/`.
Project repositories hold only their own facts. Change a rule once and it
applies to every agent, running ones included.

**Pillar 2, the agents.** Each agent gets an isolated git worktree and a
detached tmux session. One board watches all of them; you engage only when
one needs you.

```
┌─ PILLAR 1: METHOD — edit HERE, once ─────────────────────────┐
│  ~/.kovan/                                                   │
│    method/global/       who you are, universal rules         │
│    method/accounts/<a>/ per-account conventions and voice    │
│    method/domains/<d>/  per-kind-of-work knowledge           │
│    projects/<repo>/     private per-repo notes, never public │
│    modes/<name>/        task workflows (prompt + posture)    │
└───────────────────────────┬──────────────────────────────────┘
                            │ composed via @import, live
        ┌───────────────────┼────────────────────┐
     your-app/           your-lib/            your-notes/
     CLAUDE.md           CLAUDE.md            (non-coding
     repo facts only     repo facts only       agents too)
        └───────────────────┼────────────────────┘
┌─ PILLAR 2: AGENTS — spawn + monitor ─────────────────────────┐
│  worktree + tmux per agent, each governed by the method      │
│  board reads manifests →  ● needs-you   ◐ working   ✓ idle   │
└───────────────────────────────────────────────────────────────┘
```

## The delegation loop

The loop kovan is built around: hand over the end goal, walk away, come back
only when a decision is genuinely yours.

```
  you: END GOAL ──▶ kovan start ──▶ [worktree + tmux: agent runs free]
                                       │
                hits a gate (push? main?) ──▶ ● needs-you ──▶ desktop ping
                                       │                         │
                                       ◀──── you: "go" / steer ◀─┘
                                       │
                                     done ──▶ ✓ ping ──▶ you review ──▶ ok / feedback
```

The pause points are your own rules. A detached auto-mode agent that tries to
`git push` is held at an `ask`; that pause *is* the `needs-you` state on the
board, the summary tells you what it wants, and the notification brings you
back. Nothing narrates every step; the board lights up only when a decision
is yours.

## Method delivery

kovan never copies or renders your method; it links it, so an edit
propagates live:

- The global layer is `@import`ed into `~/.claude/CLAUDE.md` (a
  sentinel-marked block that `kovan method link` maintains, never clobbering
  your own content).
- When an agent is scaffolded, its worktree's `CLAUDE.local.md` gets
  `@import` lines for the resolved account, domain, and per-repo layers, plus
  its mode's method file, and a pointer to its task brief.
- Skills are symlinked, never copied: global ones into `~/.claude/skills/`,
  scoped ones into the worktree, no-clobber in both directions.

Effective context = global + account + domain + project-private +
project-public (the repo's own committed files) + the task brief. Most
general first, most specific last. For one concrete agent — say a `code`-mode
agent on ticket `PAY-42` in a `payments-api` repo, under your work account —
the stack reads like this, and `kovan method` shows exactly this view live:

```
global:             ~/.kovan/method/global/methodology.md   how you work, everywhere
                    ~/.kovan/method/global/delegation.md
account (work):     ~/.kovan/method/accounts/work/voice.md  this employer's conventions
                      skill: ticket-flow                    + skills, symlinked
domain (code):      ~/.kovan/method/domains/code/style.md   knowledge for this kind of repo
project (private):  ~/.kovan/projects/payments-api/context.md   never committed
mode (code):        ~/.kovan/modes/code/method.md           the workflow: spec → go → build
project (public):   payments-api/CLAUDE.md                  committed, kovan leaves it alone
task:               ~/.kovan/projects/payments-api/works/PAY-42/
                      context.md · spec.md · learnings.md   this task's brief and memory
gates:              push: ask · read-only: per mode         what holds it all
```

Each layer answers a different question: who you are, whose work this is,
what kind of work it is, what this repo needs, how this task type runs, and
what today's job actually is. The agent reads it as one context; you edit
each piece exactly once, in one place.

## The gate engine

Claude Code's hooks call one command, `kovan gate run`, on five events
(PreToolUse, PostToolUse, UserPromptSubmit, Notification, Stop). Every event
observes: it keeps the agent's manifest state live, which is what the board
renders. PreToolUse additionally enforces.

```
  Claude Code ──hook JSON──▶ kovan gate run
                                │
                    resolve agent (session id, cwd)
                    not a kovan agent? ──▶ exit 0, untouched
                                │
                    update manifest state ──▶ board, notifications
                                │
                    PreToolUse? evaluate gates
                                │
              match ──▶ {"permissionDecision": "ask", reason}
              no match ──▶ exit 0 (normal permission flow)
```

Why hooks: a rule written in prose is advisory, and a permission rule is
skipped in full auto-mode. A PreToolUse hook runs in every permission mode,
so it's the one mechanism that still holds after you hand an agent autonomy.
That's the whole thesis: prose rules slip; hooks don't.

Properties worth knowing:

- kovan never emits `allow`, so it can only add friction, never remove your
  own prompts. Every built-in defaults to `ask`; any error inside the hook is
  recovered to exit 0, because a kovan bug must never block a tool call.
- The Bash matcher splits commands on chains, subshells, and grouping, and
  peels quotes, env prefixes, binary paths, `bash -c`, and wrappers like
  `sudo`/`env`/`eval`/`xargs`. A shell alias or a `$VAR` binary carries no
  command to recognize; that gap is documented, and the agent is assumed
  cooperative (see [SECURITY.md](../SECURITY.md)).
- A mode's posture and write paths are resolved from its files at every
  check, not frozen at spawn. Edit a mode's `mode.yaml` and every running
  session follows at its next tool call.
- Your own gates are regex patterns in config; a bad pattern is skipped,
  never fatal.

## Task memory

Every task gets a durable workspace in the kovan store, outside the worktree:

```
~/.kovan/projects/<repo>/works/<id>/
  context.md     the brief: what you actually want done
  spec.md        the agent's plan, written before code (code mode)
  learnings.md   gotchas that accumulate across sessions
```

The agent's cwd is the worktree; its notes live in the store via `--add-dir`.
Because they're outside the worktree, removing the agent removes nothing you
care about: the branch stays, the notes stay, and the next agent on that
task starts where the last one stopped. The default `code` mode is
spec-first: the agent turns the brief into `spec.md` and stops for your
approval, so a wrong assumption dies before any code exists.

## The monitor

The board's summaries answer "what is every agent doing, and does anyone
need me" without opening a single session:

```
  agent's session transcript ──▶ digest (real turns + tool calls only)
                                    │
              one-shot claude -p (under the agent's own account)
                                    │
              board strip + S page + the agent's manifest
```

Summaries are grounded in the transcript, not a screen scrape, so an unsent
draft sitting in an agent's input box can never read as a sent prompt. The
live board state (from the hooks) rides along as ground truth, and a summary
regenerates the moment its agent flips to needs-you. Each summary is also
stamped into the agent's manifest, so your other agents can read the hive's
status from the session index.

## Design principles

- **A launcher, not a wrapper.** Opening an agent hands you its real TUI;
  kovan never re-implements or proxies the agent. Same with `kovan init`:
  kovan does the deterministic scaffolding, then launches the agent to do
  the judgment work.
- **No embedded LLM.** The binary never calls a model API. The one exception
  in spirit, the monitor, launches the agent CLI you already have.
- **No sync engine.** The method propagates by `@import` and symlinks. There
  is nothing to rebuild, regenerate, or drift.
- **tmux is the runner.** A pty multiplexer can detach and reattach an
  interactive TUI; OS job control can't. The runner is behind an interface,
  and the agent CLI is a config value: Claude Code today, Codex next.
- **Enforcement is hooks, not prose.** Anything that must hold in auto-mode
  lives as a hook; CLAUDE.md shapes behavior but blocks nothing.
- **Personal-first.** kovan changes as fast as the agent tooling world does.
  It stays honest by being one person's daily driver first and a product
  never.
