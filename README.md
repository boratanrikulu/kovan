# kovan

> **Teach your engineering methodology once. Every future agent inherits it.**

![test](https://github.com/boratanrikulu/kovan/actions/workflows/test.yml/badge.svg)
[![License](https://img.shields.io/github/license/boratanrikulu/kovan)](LICENSE)

AI agents are temporary. Your way of working isn't.

Kovan turns your engineering methodology into infrastructure. Every new agent
inherits it, gates enforce it, and task knowledge survives long after the
agent is gone. Around that core, agents run in isolated worktrees, stay
detached in tmux, and are watched from a single terminal cockpit.

One binary. No daemon. No custom runtime.
Kovan doesn't replace Claude Code. It gives it a durable operating environment.

![the board: every agent, every project, one screen](docs/img/needs-you.png)
*Ten agents across seven projects, each in its own worktree and its own tmux
session. Two are paused: full auto-mode, and they still stopped at a decision
that is yours. The mode column is the other half of the story. `code`,
`review`, `write` and `analyze` ship with kovan; `qa`, `publish`, `finance`
and `mentor` are ones this user added, a directory each. Same machinery,
different method.*

## what you get

- **your method, everywhere.** rules live once under `~/.kovan` in layers
  (global, per-account, per-domain, per-repo) and compose into every agent via
  `@import`. edit a layer file, every agent picks it up live. modes aren't
  code-only: `code`, `review`, `write` and `analyze` ship, and a new mode is a
  directory with a method file and a posture, so the same machinery runs a QA
  or a finance agent.
- **gates that hold in auto-mode.** escalated to you as Claude Code hooks,
  which fire in every permission mode. prose rules slip; hooks don't. the
  built-ins and your own rules live in one place, `~/.kovan/config.yaml`:

  ```yaml
  gates:
    push: ask                    # git push, gh pr create, writes to the GitHub API
    default_branch:
      action: ask                # git commit on main or master
    read_only: ask               # a read-only mode editing the repo
    patterns:
      - match: "docker push"     # and your own, as regex
        action: ask              # stop, hand the decision to you
      - match: "npm publish"
        action: deny             # refuse outright, no prompt
  ```

  patterns run per command segment, after quoting, `sudo`, `env` and
  `bash -c` are peeled off. [configuration](docs/configuration.md#gates) has
  the rest, including what the matcher cannot see.
- **tasks that remember.** every task gets a brief, a spec, and a learnings
  file in a durable store outside the worktree. agents come and go, the notes
  accumulate.

![the method stack one agent runs under](docs/img/method.png)
*Every agent is born into this: your global rules, account voice, domain
knowledge, mode workflow, project context, task brief, composed live, plus
the gates that hold it all.*

## why not just a multiplexer

Multiplexers keep your agents alive. kovan keeps their work disciplined:
written methods, gates the agent can't talk its way past, and task memory
that outlives any session. Good multiplexers exist (claude-squad, herdr) and
kovan overlaps on the worktrees and the board, but it's built for the day
after the demo: you never re-teach an agent your rules, auto-mode can't push
without you, and what an agent learned outlives its worktree. If you want
many providers in one view, use a multiplexer. If you want your agents to
work your way, this is that.

The [design doc](docs/design.md) walks the whole machine: how one method
reaches every agent, and why enforcement is hooks, not prose.

## install

macOS, via Homebrew:

```sh
brew install boratanrikulu/tap/kovan
```

Linux, from the [releases](https://github.com/boratanrikulu/kovan/releases)
page — `.deb`, `.rpm` and `.apk`, amd64 and arm64:

```sh
sudo dpkg -i kovan_0.1.0_linux_amd64.deb
```

Or from source, anywhere:

```sh
go install github.com/boratanrikulu/kovan/cmd/kovan@latest
```

Prebuilt tarballs (darwin/linux, amd64/arm64) are on the same releases page.

You need `git`, `tmux`, and the `claude` CLI on PATH. Claude Code is the
supported agent today; Codex support is on the way. Desktop notifications and
clipboard-image paste are macOS; everything else works anywhere Go and tmux
do.

## demo in 60 seconds

No tokens, no agents, no Claude account. One command seeds a fake fleet and
opens the board on it:

```sh
kovan demo
```

Ten agents across seven throwaway repos, each with its own worktree and tmux
session, so every key on the board does what it does for real. Your `~/.kovan`
and your own repos are never touched. `kovan demo --remove` deletes every
trace.

## quickstart

```sh
kovan setup                # once per machine: wire the Claude Code hooks
cd your-repo
kovan init                 # onboard the repo: sort its AI files into layers (or just set defaults if it has none)
kovan                      # the board
```

Press `n`. One form: id, title, project, where it runs, mode, account, and
the brief written inline (`ctrl+v` pastes a screenshot straight into it).
`ctrl+d` submits; the agent spawns in its own worktree and starts working,
detached.

The agent pauses where your rules say it must. A detached auto-mode agent
that tries to push gets held at an `ask`; the board flips to needs-you, the
summary tells you what it wants, and macOS pings you. You drop in, say go or
steer, detach again.

## the cockpit around it

- **one board, every project.** color-coded states (working, idle, needs-you,
  stopped), a live peek of the selected agent, keyboard and mouse both.
- **summaries instead of tab-cycling.** every running agent gets a one-line
  summary: what it's doing, whether it's blocked on you. on the board, on one
  page (`S`), and in each manifest so your other agents can read the hive too.
- **isolated by construction.** each agent in its own worktree and its own
  plain tmux session you can always attach to yourself. remove the agent,
  keep the branch and the notes.
- **accounts side by side.** personal-plan agents next to company ones; the
  right OAuth token injected per session, never through argv, logs, or the
  manifest.

![every agent, one page](docs/img/monitor.png)

## docs

- [getting started](docs/getting-started.md) — install to daily loop
- [configuration](docs/configuration.md) — every knob: gates, accounts, modes, apps
- [design](docs/design.md) — the two pillars, the delegation loop, why hooks
- [security](SECURITY.md) — token handling, what the hooks touch
- [contributing](CONTRIBUTING.md)

## status

Personal infrastructure that happens to be public. I use it daily and evolve
it with my own working method; breaking changes land whenever they make the
tool better for me (0.x, no stability promises). Claude Code is the agent it
drives today, with Codex support planned. Issues and PRs are welcome and I
read everything, but support is best-effort and features that don't fit the
philosophy will be kindly declined. Need stability? Pin a version or fork.
