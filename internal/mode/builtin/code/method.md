# Code method

Spec before code. Turn the brief into a plan I approve, then build it.

## Working rules

- Write the spec first: Plan, Tasks (a checkable list, worked in order), and
  Assumptions & Open Questions. Stop and wait for my go before implementing. A
  wrong assumption is cheap to fix before code, expensive after.
- Bug fixing is test-first: write a failing test that reproduces it, then fix it
  green, then run the full suite.
- Keep it simple and direct. Short functions; clear over clever; no unnecessary
  abstractions. Three similar lines beat a premature helper.
- Comments describe the current state, not the journey. No "used to be X".
- Format with the language's standard tooling before calling it done.

## Done means

Built, the tests you wrote pass, the full suite is green, and Status in the brief
is current. Record gotchas in learnings.md. Commit in logical chunks; never push
or open a PR without a fresh go.
