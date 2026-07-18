# Security

## Reporting

Email `me@bora.sh`, or use GitHub's private vulnerability reporting on this
repository. Best-effort response; this is a personal project. Only the latest
`main` is supported (0.x, no backports).

## How kovan handles OAuth tokens

kovan can run each agent under a chosen Claude account. The design keeps the
token value out of kovan entirely:

- Tokens live in files you create yourself (`claude setup-token`, saved to
  e.g. `~/.kovan/tokens/personal`). kovan refuses to use a token file that is
  group- or world-readable; `chmod 600` is required.
- kovan never reads, stores, or transmits the token. The launch command reads
  the file at exec time (`CLAUDE_CODE_OAUTH_TOKEN="$(cat <file>)" claude …`),
  so the value never appears in argv, process listings kovan controls, logs,
  config, or session manifests. Only the account *name* is recorded.
- The monitor's summarizers use the same mechanism, per agent.

## What kovan touches on your machine

- `kovan setup` merges hook entries into `~/.claude/settings.json` (the file
  is backed up first; existing keys and hooks are preserved). The hooks call
  `kovan gate run` on Claude Code tool events.
- The hook resolves whether a session belongs to kovan and exits silently for
  every other Claude session. It never emits an `allow` decision, so it can
  loosen nothing; gates only escalate to `ask` (or `deny` for your own regex
  patterns).
- Everything else lives under `~/.kovan/` and the git worktrees kovan creates.

## What the gates are, and are not

The gates are a supervision layer for cooperative agents, not a sandbox. The
command matcher sees through common shapes (chains, subshells, quoting,
`sudo`/`env`/`bash -c` wrappers), but a shell alias or a `$VAR` binary
carries no command to recognize; that gap is documented. If you need a hard
security boundary against an adversarial process, use OS-level sandboxing.
kovan's gates assume the agent is not trying to deceive you.
