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

## The README screenshots, automated

```sh
./demo/shots.sh
```

Regenerates `docs/img/{board,method,needs-you,monitor}.png` with no clicking:
seeds the demo, opens a fixed-size iTerm2 window running the cockpit, drives
the TUI to each state, and window-captures it (`screencapture -l`) — rounded
transparent corners and drop shadow, no mouse cursor. **macOS + iTerm2 only**,
and the terminal running it needs Screen Recording permission (System
Settings › Privacy & Security › Screen Recording) or the grabs come out
empty.

Leave the mouse off the window while it runs: the board has hover-select, so
a stray pointer over a row moves the selection and the method shot lands on
the wrong agent.

Tunables (env): `KOVAN_SHOT_COLS`/`KOVAN_SHOT_ROWS` (window size; the
`127`×`36` default matches the committed images — set once for your font),
`KOVAN_SHOT_PROFILE` (iTerm2 profile, default `Default`),
`KOVAN_SHOT_TEARDOWN=1` (remove the demo after).

## The shots, by hand

All from that one cockpit (what `shots.sh` automates):

- **Board / needs-you**: it opens on the board; `j` selects `mapiter`, whose
  row is `needs-you`. `j/k` picks which agent's summary and peek show below.
- **Summaries**: `S`. Every running agent with its full summary.
- **Method manager**: cursor on `ipv6`, then `m`. That agent composes every
  layer: global (files plus skills), account, domain, project-private, the
  `code` mode, the repo's public `CLAUDE.md` (which imports `docs/AGENTS.md`),
  and the task docs. `budget` is the non-code (`finance`) variant.
- **Create form**: `n`. Pre-filled for the current repo; type a title to
  make it look mid-use. Do not submit (`ctrl+d`), that would spawn a real
  agent in the demo repo.

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
