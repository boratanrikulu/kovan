# ipv6 — handle IPv6 flows on the sock_ops path

## Summary

The sock_ops path only extracts IPv4 tuples today, so IPv6 flows fall through
to the slow path. Add v6 tuple extraction and wire it into the redirect map,
keeping the v4 fast path untouched.

## Decisions

- Extend the existing tuple struct instead of adding a parallel v6 one; the
  map key grows but stays a single shape.
- Dual-stack sockets resolve to their actual address family at extraction
  time, no v4-mapped special case in the map.

## Key Files

- internal/sockops/tuple.go — extraction
- internal/sockops/redirect.go — map wiring

## Status

Extraction and map plumbing done, tests green. Wiring the redirect path,
then the integration matrix.
