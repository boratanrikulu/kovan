# Configuration

Two files, both optional. kovan runs entirely on defaults; config exists to
override them.

- `~/.kovan/config.yaml` — global: gates, accounts, apps, monitor, tmux.
- `~/.kovan/projects/<repo>/config.yaml` — per repo: worktree naming, task
  dir, account, domain, default mode, board color.

Jump to: [global config](#kovanconfigyaml) ·
[per-repo config](#kovanprojectsrepoconfigyaml) ·
[checking your config](#checking-your-config) ·
[agent CLIs](#agent-clis) ·
[accounts](#accounts) ·
[gates](#gates) ·
[method layers](#method-layers) ·
[modes](#modes) ·
[tmux](#tmux)

Both live in kovan's home; the repository itself carries no kovan file, so
every checkout and worktree sees the same settings. Both are scaffolded as
fully commented templates (`kovan init` writes them; every line shows its
default, uncomment to override). `KOVAN_HOME` moves the `~/.kovan` home,
mostly useful for tests and experiments.

## ~/.kovan/config.yaml

| key | default | what it does |
|---|---|---|
| `runner` | `tmux` | how agents run (tmux is the only runner today) |
| `agent` | `claude` | the default agent CLI: `claude` or `codex`, see [agent CLIs](#agent-clis) |
| `agents.<cli>.command` | the CLI's name | the binary to launch for that CLI, a name on PATH or a path |
| `notify` | `macos` | desktop notifications; any other value disables them |
| `author` | git `user.name` | the `{author}` in branch names |
| `tmux.options` | `mouse on`, `history-limit 50000` | applied to each agent session, session-scoped; setting the list replaces the defaults |
| `gates.*` | see [gates](#gates) | |
| `apps.editor` | `code` | board `e` opens this on the worktree |
| `apps.merge` | `smerge` | board `s` |
| `apps.terminal` | (empty) | board `t`; empty opens a new iTerm2 tab on macOS |
| `monitor.model` | `opus` | model for Claude's one-shot summaries |
| `monitor.models.<cli>` | (none) | model for that CLI's summaries; wins over `monitor.model` (e.g. `codex: gpt-5.5`) |
| `accounts` | (none) | named accounts, see [accounts](#accounts) |
| `default_account` | (none) | account used when nothing else picks one |
| `default_mode` | `code` | task mode when neither repo nor flag sets one |

## ~/.kovan/projects/<repo>/config.yaml

Keyed by the repo name (the checkout's basename).

| key | default | what it does |
|---|---|---|
| `worktree.prefix` | repo name | worktree dir is `<prefix>-<id>`, a sibling of the repo |
| `worktree.base` | auto (`origin/HEAD`) | base branch for new worktrees |
| `worktree.branch_template` | `feat/{author}_{id}_{slug}` | branch naming |
| `worktree.id_pattern` | (none) | regexp validating a typed id (a blank id still auto-generates) |
| `task.dir` | `works` | task-doc folder under `~/.kovan/projects/<repo>/` |
| `task.token` | (none) | placeholder in templates substituted with the id (e.g. `TASK-XXXXX`) |
| `account` | (none) | default account for this repo's agents |
| `agent` | (none) | default agent CLI for this repo's agents |
| `domain` | (none) | method domain layer to compose (e.g. `code`, `writing`) |
| `mode` | (none) | default task mode for this repo |
| `color` | (none) | default board stripe color for this repo's agents |
| `write_paths` | (none) | extra allowed write prefixes for this repo's scoped modes (adds to the mode's own list) |

The worktree keys also apply when an agent from another repo adds a worktree
here with `kovan worktree add <repo>`: it gets `<prefix>-<agent id>` on this
repo's branch template and base, and this repo's `.kovan/setup.sh` runs on it.

## Checking your config

Templates are scaffolded once and never touched again, so a config written
months ago drifts: keys get removed, new gates and options land. `kovan
doctor` compares your files with the binary you are running and reports,
grouped per file:

- **no longer read** — keys you set that the current binary does not know
  (removed keys, typos).
- **stale comments** — keys your file still documents in comments that no
  longer exist.
- **new since your config was written** — options the current template has
  that your file never mentions, one line each with what it does.
- **check values** — values the loader rejects or the code silently ignores
  (a gate action that is not `ask`/`off`, an invalid pattern regexp, an
  unknown palette color, an account without a valid token file).

Report only; your files are never modified. Inside a repo it covers that
repo's `~/.kovan/projects/<repo>/config.yaml` too, and flags a legacy
`.kovan.yaml` still sitting in the repo. Exits `1` when something needs
attention (unparseable file, dead keys, bad values); staleness alone is
informational and exits `0`.

`kovan doctor --sync` brings the files up to date in place. Template
documentation refreshes silently; every line you set is kept exactly as
written, and notes you wrote next to your settings travel with them; removing
a dead key asks first, one question per key, defaulting to keep. A file with nothing uncommented is documentation only and is replaced
by the fresh template without questions. The original is saved next to the
file as `.bak` before anything is written. Values you set are never changed,
so anything under "check values" stays yours to fix.

## Agent CLIs

kovan runs Claude Code (`claude`) and Codex (`codex`). Each agent picks one:
`kovan start --agent codex` (or the new-agent form's agent field) > the repo's
`agent:` > the global `agent:` > `claude`. The board's AGENT column shows which
runs where, and the edit modal (`c`) moves a running agent to the other CLI.

Both CLIs get the same method, gates and board. Where they differ:

| | Claude Code | Codex |
|---|---|---|
| method | `--append-system-prompt`, composed at each launch | `-c developer_instructions`, composed at each launch |
| gates | `ask`: Claude Code prompts you | `deny`: Codex can't ask from a hook; reply `kovan-allow` to let the command through once |
| needs-you | the permission prompt notification | a pending approval (`PermissionRequest`) |
| session id | kovan picks it at start | Codex picks it; kovan records it from the first hook |
| accounts | a token file per account | a `CODEX_HOME` per account |
| hooks | `~/.claude/settings.json` | `hooks.json` in each `CODEX_HOME`, trusted once in `/hooks` |
| skills | `.claude/skills`, `~/.claude/skills` | `.agents/skills`, `~/.agents/skills` |

**Switching an agent's CLI.** Pick another agent in the edit modal and save.
The conversation can't move between CLIs, so kovan first has the old CLI write
`handoff.md` into the task docs (done, decided, open, next; the raw last turns
if that summary fails). Then it stops the session and starts the new CLI in
the same worktree on a takeover prompt: read the brief and the handoff, check
the git state, report where things stand and wait for your go. The worktree,
the branch, uncommitted changes, task docs and method all stay. Each CLI's
conversation is kept, so switching back resumes the old one.

## Accounts

Run agents under different accounts on one machine, regardless of which one
is logged in. An account belongs to one agent CLI (`agent:`, default
`claude`); the pickers only offer the accounts of the agent's CLI.

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

A Codex account is a home of its own instead of a token: `home` becomes the
agent's `CODEX_HOME`, which holds its login, its sessions and its hooks. Log in
once per home, then run `kovan setup` to install the hooks there:

```yaml
accounts:
  openai:
    agent: codex
    home: ~/.kovan/codex/openai
```

```sh
CODEX_HOME=~/.kovan/codex/openai codex login
kovan setup
```

Resolution order: `kovan start --account X` > the repo's `account:` >
`default_account` > whatever account is logged in. A default that belongs to
another CLI doesn't apply; the agent runs under its own CLI's login. The monitor's summarizers
run under each agent's own account too, so summary cost lands on the right
plan.

An injected token authenticates against its plan but arrives without the
account's entitlements, so a feature your plan includes — the 1M context
window on Max and Team, for one — can be missing inside an agent even though
it works in a normal `claude` session. Two ways around it. Pick **default
(system)** in the new-agent form or the board's edit modal (`c`) to inject no
token at all: the agent inherits the machine's login and everything that comes
with it, at the cost of billing to whichever account is logged in. Or give the
account an `env` map, applied to that account's agents at launch:

```yaml
accounts:
  company:
    token_file: ~/.kovan/tokens/company
    env:
      ANTHROPIC_DEFAULT_OPUS_MODEL: claude-opus-5[1m]
```

Pinning the model this way asks for the window directly instead of going
through the picker that refuses it, and keeps the per-account billing. Note a
pin holds until you change it: the agent stays on that exact model when a newer
one ships.

## Gates

A gate is the point where an agent has to stop and hand a decision back to
you. Prose in a method file asks an agent to behave; a gate makes the
behaviour structural.

Gates run as the agent CLI's pre-tool hook (`PreToolUse`), wired once by
`kovan setup`. That matters: hooks fire in **every permission mode**,
including full auto-mode, so a detached agent hits the same gate as a
supervised one. The hook only ever emits `ask` or `deny`. It never emits
`allow`, so it can tighten your setup and never loosen it. For any session
that is not a kovan agent, the hook exits silently.

`ask` is the default everywhere. Under Claude Code the agent pauses, Claude
Code shows your reason, and the board flips that row to `needs-you` with the
summary telling you what it wants before you open the session. Codex has no
`ask` for hooks (it treats one as a failed hook and runs the command), so under
Codex an `ask` gate denies instead, and the reason quotes the command:
"`git push origin main` needs your approval. Reply with kovan-allow to allow
it once." kovan records that command as pending. Your next message in the chat
that contains `kovan-allow` (any case, anywhere in the message) lets that one
command through once, in the turn your message starts:

```
› push it
• git push origin main      ✗ `git push origin main` needs your approval…
› kovan-allow
• git push origin main      ✓ pushed
› push again
• git push origin main      ✗ needs your approval… (each run needs its own)
```

A plain "go" approves nothing, and only your own messages in that
conversation count: a prompt from a Codex sub-agent, or from a CLI the agent
runs inside its tab, approves nothing. Another gated command replaces the
pending one (a `deny` pattern touches no pass). An approval the turn does not
use ends with the turn, and your next message without the word takes it back
(say you pressed Esc first). A
fresh session starts with nothing pending. A `deny` pattern stays a deny,
approval or not, and wins over the ask for any other part of the same command:
`git push && terraform destroy` is denied outright when `terraform destroy` is.
The manifest keeps only a digest of the pending command, never its text.

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

Every built-in takes `ask` or `off`, nothing else. `off` disables that gate
outright. Under Claude Code only your own `patterns` can `deny`; kovan itself
never refuses a command on your behalf. Under Codex an `ask` arrives as a deny,
for the reason above.

### The built-in gates

| gate | fires on | what it catches |
|---|---|---|
| `push` | Bash | `git push`; `gh pr create`; a `gh api` call that looks like a write (`-X`, `--method`, `POST`, `PATCH`, `PUT`, `DELETE`, or a `refs` path); any `curl` touching `api.github.com` |
| `default_branch` | Bash | `git commit` while the agent's branch is one of `branches` |
| `read_only` | Edit, Write, MultiEdit, NotebookEdit | any edit inside the worktree, for a mode whose posture is read-only |
| `write_paths` | Edit, Write, MultiEdit, NotebookEdit | an edit inside the worktree but outside the prefixes the mode is allowed to write |

The `gh api` and `curl` rules are deliberately biased toward catching too
much rather than too little: a read-only `gh api` call passes, anything that
smells like a mutation asks.

`read_only` and `write_paths` enforce a mode's posture, declared in its
`mode.yaml`, see [Modes](#modes). Two things about them are worth knowing.
They only apply **inside the agent's worktrees** (its primary one and any it
added with `kovan worktree add`, each scoped by its own repo's `write_paths`),
so writes to the task-doc store (brief, spec, learnings) always pass and a
read-only agent can still produce its artifact. And they fail open: a relative
path, an empty path, or anything outside the agent's worktrees is not gated,
because a gate that guesses is worse than one that stays quiet.

### Your own gates

`patterns` is a list of regexes matched against each command segment:

```yaml
gates:
  patterns:
    - match: "docker push"
      action: ask                       # escalate to you
      reason: "kovan: confirm before pushing an image"
    - match: "npm publish"
      action: deny                      # refuse outright, no prompt
      reason: "kovan: releases are not an agent's job"
    - match: 'psql .*(-h|--host) +prod'
      action: deny
      reason: "kovan: no agent talks to prod"
```

- `match` is a Go regexp, matched against the segment as text (not the
  tokens), so flags and arguments are visible to it.
- `action` is `ask`, `deny`, or `off`. Omitted means `ask`. `off` keeps the
  entry in the file without it firing, which is easier to reason about than
  commenting out YAML.
- `reason` is the line both you and the agent see at the prompt. Omitted,
  you get `kovan: gated command`.
- A regexp that does not compile is skipped, never fatal. A typo in config
  must not wedge every tool call the agent makes.

### Order: a deny always wins

A `deny` pattern wins over every `ask`, wherever it sits: over a built-in
(`match: "push --force"` with `action: deny` denies `git push --force` even
with `push: ask` on), over an `ask` pattern listed before it, and over an
`ask` for another part of the same command (`git push && terraform destroy`
is denied when `terraform destroy` is). Among asks, the built-ins come first,
then your `ask` patterns in their listed order; the first one names the
reason.

```yaml
gates:
  patterns:
    - match: 'git push .*--force'
      action: deny
      reason: "kovan: force push is mine to run"
```

This matters most under Codex, where an `ask` is something `kovan-allow` lets
through once: the deny never is.

### How a command is read

A single Bash call can carry several commands, and the interesting one is
rarely first. kovan splits the command on `&&`, `||`, `;`, `|`, `&`,
newlines, and subshell or brace grouping, then checks each piece on its own.

Each piece is then unwrapped until the real command shows: quotes are
stripped, leading `VAR=value` assignments dropped, a path-prefixed binary
reduced to its name, `bash -c '…'` (also `sh`, `zsh`, `dash`) opened up, and
wrappers peeled: `sudo`, `env`, `eval`, `xargs`, `command`, `time`, `nice`,
`setsid`, `stdbuf`. For `git`, global options are skipped to find the real
subcommand, so `git -C /repo push` is a push.

Worked example. This whole line is one Bash call:

```sh
cd repo && git commit -m "fix the push path" && sudo /usr/bin/git push
```

Three segments. The first is ignored. The second is a `git commit`, gated
only if the branch is protected; the quoted word "push" in the message is
just an argument and does not trigger the push gate. The third unwraps
`sudo`, then `/usr/bin/git` to `git`, and asks.

### What it cannot see

The matcher needs a command to recognize. Some shapes carry none:

- a shell alias or git alias, where the real command lives in a config file
- a `$VAR` or `$(…)` binary resolved at runtime
- a script whose name says nothing about what it does inside

These are gaps by construction, not bugs waiting on a fix. Gates are a
supervision layer for a cooperative agent, not a sandbox: they assume the
agent is not trying to deceive you. Defaulting to `ask` on everything the
matcher does see is what keeps the gap small. If you need a hard boundary
against an adversarial process, use OS-level sandboxing.

## Method layers

Your method lives under `~/.kovan` and kovan composes it into every agent at
each launch, most general first, the same way for every agent CLI:

| layer | path | reaches |
|---|---|---|
| global | `~/.kovan/method/global/*.md` | every agent (and, after `kovan method link`, every Claude Code session via `~/.claude/CLAUDE.md`) |
| account | `~/.kovan/method/accounts/<acct>/*.md` | agents on that account |
| domain | `~/.kovan/method/domains/<domain>/*.md` | repos that set `domain:` |
| project (private) | `~/.kovan/projects/<repo>/*.md` | that repo's agents, never committed |
| mode | `~/.kovan/modes/<mode>/method.md` | agents running that task mode |
| project (public) | the repo's own `CLAUDE.md`/`AGENTS.md` | committed, kovan leaves it alone |

The composed text goes to the CLI as part of its system prompt (each file
under a heading with its path, `@import`s expanded), so nothing lands in the
worktree. A layer edit reaches an agent on its next wake; there is no sync or
copy step. A file the CLI already loads itself is left out, so a Claude agent
doesn't get the global layer twice once `kovan method link` has wired it into
`~/.claude/CLAUDE.md`. Skills work the same way: drop one under any layer's
`skills/<name>/` and kovan symlinks it where each CLI looks (`.claude/skills`
and `.agents/skills`), without clobbering anything already there.
`kovan method` opens the inspector showing exactly which files govern a
selected agent, with `e` to edit and `E` to hand the file to your default agent CLI.

<!-- TODO(bora): screenshot of the method inspector with a few layers open. -->

## Modes

A mode is a working style: an opening prompt, a posture, and the docs it
scaffolds. Four ship with kovan:

| mode | posture | scaffolds | style |
|---|---|---|---|
| `code` | edit | `spec.md`, `test-plan.md` | spec first, implement after your go |
| `review` | read-only | `review.md` | findings table, posts to GitHub only on your go |
| `analyze` | read-only | `analysis.md` | evidence-backed report, file:line pointers |
| `write` | read-only | `draft.md` | prose in your voice, no code |

### Which mode a task gets

Most specific wins:

1. the mode picked in the `n` form, or `kovan start --mode <name>`
2. the repo's `mode:` in `~/.kovan/projects/<repo>/config.yaml`
3. `default_mode:` in `~/.kovan/config.yaml`
4. `code`

So `default_mode: review` makes review the mode you land on everywhere, and a
repo that is mostly prose can set `mode: write` for itself without touching
the global default.

Read-only is enforced, not advised: the read-only gate confirms any edit
inside the worktree while the task docs stay writable.

### Adding and retuning modes

A mode lives in `~/.kovan/modes/<name>/` and holds up to three files, all
optional:

| file | what it sets |
|---|---|
| `prompt.md` | the opening prompt; placeholders `{{brief}}` and `{{artifact}}` |
| `mode.yaml` | `posture: edit\|read-only`, `docs: [...]`, `write_paths: [...]` |
| `method.md` | the working method the agent carries across sessions |

For a name that matches a built-in, the directory layers on top rather than
replacing it, file by file and inside `mode.yaml` field by field. Drop a
`prompt.md` into `~/.kovan/modes/review/` to reword it and review stays
read-only, still scaffolding `review.md`. Set only `write_paths:` and the
posture, docs and prompt stay the built-in's. A key you leave out keeps the
built-in's value; an explicit empty list (`docs: []`) means none.

A name that matches no built-in is a mode of your own, and then `prompt.md` is
required: a directory without one is not a mode, and kovan reports the name as
unknown.

Running `kovan` materializes a built-in's `method.md` to
`~/.kovan/modes/<name>/method.md` the first time it is used, without
clobbering an existing file, so a shipped method becomes a live file you can
edit like any other method layer.

`write_paths` scopes an editing mode to a corner of the repo, or acts as a
carve-out from read-only:

```yaml
# ~/.kovan/modes/docs-only/mode.yaml
posture: read-only
write_paths: [docs/]     # carve-out: read-only everywhere, editable under docs/
```

Posture and write paths are resolved from the files at every gate check, so
editing them reaches running sessions immediately; a repo can add its own
carve-outs for scoped modes with `write_paths:` in its
`~/.kovan/projects/<repo>/config.yaml`.

## tmux

Options under `tmux.options` are applied per agent session, so your
`~/.tmux.conf` stays untouched. kovan also gives each session a status bar
(the kovan chip, the agent id, repo, branch, title) and binds `prefix k` to
the editor/merge/terminal/notes menu inside any agent session. An agent with
worktrees in several repos gets each app per repo in that menu.
