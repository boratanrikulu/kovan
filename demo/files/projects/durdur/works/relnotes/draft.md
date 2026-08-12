# durdur v0.2

The big one is live policy reloads: durdur now swaps its rule map without
dropping the datapath, so a ruleset change no longer blips the firewall.

- feat: live policy-map reload with a versioned layout
- feat: per-rule counters exposed on the status socket
- fix: teardown left a dangling qdisc on an unclean exit
