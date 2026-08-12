# o2 — why -O2 output fails the verifier

## Summary

At -O2 the compiler spills a bounds-checked register, so the verifier loses
the proven range and rejects the later access. Explain exactly where, with
instruction-level evidence. Analysis only, no code changes.

## Status

Answered in analysis.md with the spill site and the lost-range chain. Idle.
