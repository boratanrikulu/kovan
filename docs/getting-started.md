# Getting started

From zero to a board full of working agents. Five minutes, three commands of
setup, then it's daily-driver territory.

## Install

```sh
go install github.com/boratanrikulu/kovan/cmd/kovan@latest
```

Prerequisites on PATH: `git`, `tmux`, and the `claude` CLI (Claude Code).
macOS additionally gets desktop notifications and clipboard-image paste in
the brief editor; Linux runs fine without both.

## Wire the hooks (once per machine)

```sh
kovan setup
```

This merges a single kovan hook into `~/.claude/settings.json` (backed up
first, idempotent, your existing hooks and settings untouched). The hook is
how the board knows what every agent is doing: it stamps live state
(`working` / `needs-you` / `idle`) into each agent's manifest and enforces
the gates. It resolves whether a session belongs to kovan and leaves every
other Claude session alone.

## Onboard a repo

```sh
cd your-repo
kovan init
```

kovan does the boring half itself: scaffolds `~/.kovan` (a fully commented
`config.yaml`), writes a commented `.kovan.yaml` starter into the repo, and
installs the hooks if you skipped `setup`. Then it launches Claude in the
repo with one job: read your existing AI files (`CLAUDE.md`, `AGENTS.md`,
scattered rules) and propose how they split into layers, universal rules
into the global method, private per-repo notes into the kovan store, public
facts staying in the repo. It waits for your go before writing anything.

Have a repo whose rules you like? `kovan init --reference ../that-repo`
points the onboarding at it.

## Start your first agent

### From the cockpit (the main way)

Run bare `kovan`, press `n`.

<!-- TODO(bora): screenshot of the new-agent form, ideally with a pasted
[[image #1]] token visible in the brief. -->

The form: an `id` (leave blank, one is generated), a `title`, a project
picker, where it runs (a new workspace, this checkout, or a tab in an
existing workspace), the base branch, the task mode, the account, a row
color. Then the brief: what you actually want done, written inline.
`ctrl+v` pastes a clipboard image into it (macOS), `ctrl+f` expands it
full-screen, `ctrl+d` submits. The agent spawns in its own worktree and tmux
session and starts working immediately.

### From the CLI

```sh
kovan start ABC-1 "fix the flaky vfs test"
```

Same thing, except the brief opens in `$EDITOR`. Useful flags:

| flag | what it does |
|---|---|
| `--mode review` | task mode: `code` (default), `review`, `analyze`, `write` |
| `--account personal` | which Claude account the agent runs under |
| `--from origin/release` | base branch for the new worktree |
| `--quick` | skip the brief, run the title as the goal |
| `--in-place` | run in this checkout, no separate worktree |
| `--in ABC-1` | join ABC-1's workspace as a tab, sharing its branch |

The brief becomes `context.md` in a durable store at
`~/.kovan/projects/<repo>/works/<id>/`, next to the mode's docs (`spec.md`
and `test-plan.md` for code, `review.md` for review, and so on) and
`learnings.md`. That store outlives the worktree: remove the agent, the
notes stay.

The default `code` mode is spec-first: the agent reads the brief, writes its
plan to `spec.md`, then stops and asks you to review before touching code. A
wrong assumption dies in the spec, not in a diff.

## The board

<!-- TODO(bora): GIF of the board — a few agents, one flipping to needs-you,
entering it, detaching back. This is the money shot. -->

Bare `kovan` is the cockpit: every agent across every project, refreshed
every 1.5s. Columns: state, permission mode, task mode, account, id, repo,
age, workspace (branch), title. States: `working` (busy), `idle` (turn done,
waiting for input), `needs-you` (blocked on a decision, colored to pop),
`stopped` (session gone). Under the board sits the selected agent's
AI-written summary and a live peek of its terminal.

The keys that matter:

| key | action |
|---|---|
| `j`/`k`, hover | move (the peek and summary follow) |
| `enter`, double-click | open the agent's own Claude TUI; detach returns here |
| `n` | new agent |
| `S` | the monitor page: every running agent with its full summary |
| `m` | the method inspector: the exact files an agent runs under |
| `c` | edit an agent (title, mode, account, color) |
| `/` | filter, `tab` active/archived, `p` pin, `a` archive |
| `v` | choose and reorder the board's columns (kept across restarts) |
| `e` `s` `t` `w` | open editor / git GUI / terminal / task notes on the agent |
| `?` | the rest |

The mouse works everywhere it should: hover selects, the wheel scrolls the
list or the peek, clicking the header switches active/archived, clicks focus
form fields and pick options. Inside an agent's tmux session, `prefix k`
opens the same editor/merge/terminal/notes menu.

## The daily loop

Hand over the goal, walk away. When an agent hits a gate (a push, a commit
on main) or finishes its turn, the board flips to `needs-you` or `idle` and
macOS notifies you. Glance at the summary to see what it wants, `enter` to
drop in, say "go" or steer, `ctrl+b d` to detach. The board is where you
live; the agents are where the work happens.

Done with an agent?

```sh
kovan remove ABC-1     # kill the session, drop the worktree, keep the branch
```

Or archive it from the board (`a`, once it's stopped): worktree and docs
kept, row moved out of sight, restorable any time by opening it. `kovan
status` prints the same board as plain text for scripts and CI
(`--filter`, `--archived`).

## Next

- [configuration](configuration.md) — gates, accounts, modes, tmux options,
  every knob with its default
- [design](design.md) — how it all works underneath
