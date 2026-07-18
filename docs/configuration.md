# Configuration

Two files, both optional. kovan runs entirely on defaults; config exists to
override them.

- `~/.kovan/config.yaml` — global: gates, accounts, apps, monitor, tmux.
- `<repo>/.kovan.yaml` — per repo: worktree naming, task dir, account, domain,
  default mode.

Both are scaffolded as fully commented templates (`kovan init` writes them;
every line shows its default, uncomment to override). `KOVAN_HOME` moves the
`~/.kovan` home, mostly useful for tests and experiments.

## ~/.kovan/config.yaml

| key | default | what it does |
|---|---|---|
| `runner` | `tmux` | how agents run (tmux is the only runner today) |
| `agent` | `claude` | the agent CLI kovan launches |
| `notify` | `macos` | desktop notifications; any other value disables them |
| `author` | git `user.name` | the `{author}` in branch names |
| `tmux.options` | `mouse on`, `history-limit 50000` | applied to each agent session, session-scoped; setting the list replaces the defaults |
| `gates.*` | see [gates](#gates) | |
| `apps.editor` | `code` | board `e` opens this on the worktree |
| `apps.merge` | `smerge` | board `s` |
| `apps.terminal` | (empty) | board `t`; empty opens a new iTerm2 tab on macOS |
| `monitor.model` | `opus` | model for the one-shot summaries |
| `accounts` | (none) | named Claude accounts, see [accounts](#accounts) |
| `default_account` | (none) | account used when nothing else picks one |
| `default_mode` | `code` | task mode when neither repo nor flag sets one |
| `projects.<repo>.color` | (none) | default board stripe color for a repo's agents |

## <repo>/.kovan.yaml

| key | default | what it does |
|---|---|---|
| `worktree.prefix` | repo basename | worktree dir is `<prefix>-<id>`, a sibling of the repo |
| `worktree.base` | auto (`origin/HEAD`) | base branch for new worktrees |
| `worktree.branch_template` | `feat/{author}_{id}_{slug}` | branch naming |
| `worktree.id_pattern` | (none) | regexp validating a typed id (a blank id still auto-generates) |
| `task.dir` | `works` | task-doc folder under `~/.kovan/projects/<repo>/` |
| `task.token` | (none) | placeholder in templates substituted with the id (e.g. `TASK-XXXXX`) |
| `account` | (none) | default Claude account for this repo's agents |
| `domain` | (none) | method domain layer to compose (e.g. `code`, `writing`) |
| `mode` | (none) | default task mode for this repo |
| `write_paths` | (none) | extra allowed write prefixes for this repo's scoped modes (adds to the mode's own list) |

## Accounts

Run agents under different Claude accounts on one machine, regardless of
which one is logged in.

```yaml
accounts:
  personal: { token_file: ~/.kovan/tokens/personal }
  company:  { token_file: ~/.kovan/tokens/company }
default_account: personal
```

Create each token with `claude setup-token`, save it to the file, `chmod
600`. kovan never reads or stores the token itself: the launch command reads
the file at exec time, so the value never appears in argv, logs, or the
manifest. A missing or group/world-readable token file is a hard error that
refuses to spawn.

Resolution order: `kovan start --account X` > the repo's `account:` >
`default_account` > whatever account is logged in. The monitor's summarizers
run under each agent's own account too, so summary cost lands on the right
plan.

## Gates

Everything defaults to `ask`: the agent pauses and the decision escalates to
you, which is exactly the `needs-you` ping on the board. kovan never
auto-allows and never blocks silently by default.

```yaml
gates:
  push: ask              # git push, gh pr create, writing gh api calls, curl to api.github.com
  read_only: ask         # read-only modes: confirm any edit to the repo (task docs always pass)
  write_paths: ask       # path-scoped modes: confirm edits outside their write paths
  default_branch:
    action: ask          # confirm git commit on a protected branch
    branches: [main, master]
  patterns: []           # your own gates, see below
```

The `read_only` and `write_paths` gates enforce a mode's posture. A mode's
write paths are defined in its `mode.yaml`, see [Modes](#modes).

Custom gates are regex patterns run against every command segment:

```yaml
gates:
  patterns:
    - match: "terraform +(apply|destroy)"
      action: ask        # escalate to you
      reason: "kovan: confirm terraform changes"
    - match: 'rm\s+-rf\s+/'
      action: deny       # block outright, no prompt
      reason: "kovan: refusing rm -rf /"
```

A bad regexp is skipped, never fatal. `reason` is the line the agent (and
you) see at the prompt.

How matching works, short version: commands are split on `&&`, `;`, `|`,
newlines, and subshell grouping, then each segment is unwrapped past quotes,
`VAR=value` prefixes, binary paths, `bash -c`, and wrappers like `sudo`,
`env`, `eval`, `xargs`. So `sudo /usr/bin/git push` is gated and a quoted
"push" in a commit message is not. A git alias or a `$VAR` binary carries no
command to recognize; that gap is documented, and `ask` on everything the
matcher does see keeps it small.

## Method layers

Your method lives under `~/.kovan` and composes into every agent via
`@import`, most general first:

| layer | path | reaches |
|---|---|---|
| global | `~/.kovan/method/global/*.md` | every agent (via `~/.claude/CLAUDE.md`) |
| account | `~/.kovan/method/accounts/<acct>/*.md` | agents on that account |
| domain | `~/.kovan/method/domains/<domain>/*.md` | repos that set `domain:` |
| project (private) | `~/.kovan/projects/<repo>/*.md` | that repo's agents, never committed |
| mode | `~/.kovan/modes/<mode>/method.md` | agents running that task mode |
| project (public) | the repo's own `CLAUDE.md`/`AGENTS.md` | committed, kovan leaves it alone |

Editing a layer file propagates live; there is no sync or copy step. Skills
work the same way: drop one under any layer's `skills/<name>/` and kovan
symlinks it where Claude looks, without clobbering anything already there.
`kovan method` opens the inspector showing exactly which files govern a
selected agent, with `e` to edit and `E` to hand the file to Claude.

<!-- TODO(bora): screenshot of the method inspector with a few layers open. -->

## Modes

A mode is a working style: an opening prompt, a posture, and the docs it
scaffolds.

| mode | posture | scaffolds | style |
|---|---|---|---|
| `code` | edit | `spec.md`, `test-plan.md` | spec first, implement after your go |
| `review` | read-only | `review.md` | findings table, posts to GitHub only on your go |
| `analyze` | read-only | `analysis.md` | evidence-backed report, file:line pointers |
| `write` | read-only | `draft.md` | prose in your voice, no code |

Read-only is enforced, not advised: the read-only gate confirms any edit
inside the worktree while the task docs stay writable. Override a built-in or
add your own by creating `~/.kovan/modes/<name>/` with a `prompt.md`
(placeholders `{{brief}}`, `{{artifact}}`), an optional `mode.yaml`
(`posture: edit|read-only`, `docs: [...]`, `write_paths: [...]`), and an
optional `method.md` the agent carries across sessions. `write_paths` scopes
an editing mode to a corner of the repo, or acts as a carve-out from
read-only:

```yaml
# ~/.kovan/modes/docs-only/mode.yaml
posture: read-only
write_paths: [docs/]     # carve-out: read-only everywhere, editable under docs/
```

Posture and write paths are resolved from the files at every gate check, so
editing them reaches running sessions immediately; a repo can add its own
carve-outs for scoped modes with `write_paths:` in `.kovan.yaml`.

## tmux

Options under `tmux.options` are applied per agent session, so your
`~/.tmux.conf` stays untouched. kovan also gives each session a status bar
(the kovan chip, the agent id, repo, branch, title) and binds `prefix k` to
the editor/merge/terminal/notes menu inside any agent session.
