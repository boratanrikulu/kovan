# o2 — analysis

The bound is proven at the compare, then the register is spilled to the
stack across a call and reloaded without the range. The verifier treats the
reload as unbounded, so the map access fails.

- codegen/emit.go:212 — the compare that proves the range
- codegen/emit.go:240 — the spill that drops it

Fix direction (out of scope here): pin the checked value in a callee-saved
register, or re-prove the bound after the reload.
