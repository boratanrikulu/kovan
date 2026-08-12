# ipv6 — spec

## Plan

Add IPv6 tuple extraction to the sock_ops path and wire it into the redirect
map, leaving the IPv4 fast path untouched.

## Tasks

- [x] Extend the tuple struct with the v6 addresses.
- [x] Extract the family at runtime, no v4-mapped special case.
- [ ] Wire the redirect map lookup.
- [ ] Integration matrix across v4, v6, dual-stack.

## Open questions

- None outstanding; the map-key growth was approved.
