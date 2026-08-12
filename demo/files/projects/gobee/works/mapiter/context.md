# mapiter — fix verifier rejection on map iteration

## Summary

The verifier rejects the generated map-iteration loop: it loses the proven
bound on the iterator, so the array access reads as unbounded. Reshape the
codegen so the bound survives.

## Decisions

- Two candidates on the table: a bounded-loop rewrite and a callback form.
  Waiting on a pick before implementing.

## Status

Rejection reproduced with a minimal program. Both codegen shapes sketched;
needs a decision.
