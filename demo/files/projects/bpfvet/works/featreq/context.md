# featreq — review: feature-requirement extraction

## Summary

Review the feature-requirement extraction pass: does it find every helper
and map feature a program depends on, and does it report them at the right
source locations?

## Decisions

- Findings only, no patches; severity-ranked table in review.md.

## Key Files

- internal/extract/requirements.go — the pass under review

## Status

Review finished: 2 medium, 1 low, table in review.md with file:line
pointers. Waiting to discuss.
