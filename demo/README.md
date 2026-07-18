# demo

A fake board for screenshots: seeded manifests, tiny throwaway git repos,
and dummy tmux sessions standing in for agents. Everything lives under
`/tmp/kovan-demo` (`KOVAN_DEMO_ROOT` overrides). The real `~/.kovan` is
never read or written.

## Seed and run

```sh
./demo/seed.sh
cd /tmp/kovan-demo/repos/gecit && KOVAN_HOME=/tmp/kovan-demo/home kovan
```

Use a freshly built binary (`make build`, then `./bin/kovan`); the board
depends on current behavior. Size the terminal to how you want the shots to
look before opening the cockpit.

Re-running `seed.sh` is always safe: it tears the previous demo down first,
so it doubles as a reset (fresh ages, fresh summaries).

## The shots

All from that one cockpit:

- **Board**: it opens on it. `j/k` picks which agent's summary and peek
  show below the list.
- **Summaries**: `S`. Every running agent with its full summary.
- **Create form**: `n`. Pre-filled for the current repo; type a title to
  make it look mid-use. Do not submit (`ctrl+d`), that would spawn a real
  agent in the demo repo.
- **Method manager**: cursor on `budget`, then `m`. That agent composes
  every layer: global (files plus a skill), account, domain, project-private,
  a custom mode (`finance`), the repo's public `AGENTS.md`, and the task
  docs. `ipv6` is the code-domain variant.

Plain-text check without the TUI:

```sh
KOVAN_HOME=/tmp/kovan-demo/home kovan status
```

## Notes

- The dummy panes run `sleep 3600`; after an hour those sessions exit and
  their rows flip to `stopped`. Re-seed.
- The AGE column is live. Seeded ages hold for a while, then grow; re-seed
  for fresh ones.
- `pion` has no tmux session on purpose (renders `stopped`), and one
  archived agent fills the `archived` tab.

## Tear down

```sh
./demo/teardown.sh
```

Kills the `kovan-demo-*` tmux sessions and removes `/tmp/kovan-demo`.
Nothing else is touched.
