# mapiter — spec

## Plan

Emit the map iteration so the verifier keeps the iterator's range across the
loop body, without changing the public codegen API.

## Tasks

- [x] Reproduce the rejection with a minimal program.
- [ ] Pick the loop shape (bounded rewrite vs callback).
- [ ] Regenerate the corpus and re-run the verifier suite.

## Open questions

- Bounded rewrite keeps one function; the callback form is smaller but adds
  an indirect call. Which do you want?
