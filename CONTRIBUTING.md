# Contributing

kovan is personal infrastructure that happens to be public. I use it daily
and evolve it with my own working method, so the bar for features is "does
this fit the philosophy", not "is this useful to someone". Issues and PRs are
welcome and I read everything; features that don't fit will be kindly
declined. For anything bigger than a bugfix, open an issue first so we agree
on the shape before you write code.

## Development

```sh
make test    # build + vet + gofmt + tests; safe, tests never touch your ~/.kovan
```

Tests isolate themselves: every package that could reach the kovan home sets
a throwaway `KOVAN_HOME` in its `TestMain`.

## Conventions

- Go, formatted with `gofmt` + `goimports`. Short functions, clear over
  clever, no unnecessary abstractions, no "what" comments.
- Shell out to `git` and `tmux`; don't add heavyweight dependencies for them.
- Errors wrapped with `fmt.Errorf("...: %w", err)`.
- Commits: conventional, imperative mood, lowercase subject, single line
  (`fix(gates): resolve posture live from the mode`).
- Bug fixes come with a test that reproduces the bug first.
