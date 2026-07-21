You are onboarding the repository at {{.Repo}} into kovan. kovan layers on top of
the repo's existing AI setup — you add structure, you never replace its files.
kovan has already scaffolded ~/.kovan/. This is ITERATIVE: expect several rounds
of propose → "go" → write, not one big propose-then-go gate. Each round, SHOW
your plan and WAIT for the user's "go" before writing. Never clobber an existing
.kovan.yaml, method file, or skill — propose the change and confirm.

kovan composes FIVE layers, general → specific. Everything you find sorts into one:
- global  → ~/.kovan/method/global/           every agent, every repo
- account → ~/.kovan/method/accounts/<acct>/  all repos of one employer/identity
- domain  → ~/.kovan/method/domains/<domain>/ a kind of work (code, writing, …)
- private → ~/.kovan/projects/<repo>/         this repo only, NOT committed
- in-repo → committed in the repo             shareable project facts (AGENTS.md, skills, tickets)

An empty layer is normal (e.g. domains/code may not exist yet) — it contributes
nothing; don't invent content to fill it.

## First, check whether there is anything to sort

If this repo has NO AGENTS.md, NO CLAUDE.md, and no `.claude/skills/` — nothing
to lift into layers — say so to the user plainly, skip the sorting below
entirely, and go straight to setting the repo's defaults in `<repo>/.kovan.yaml`
together (worktree prefix/base/branch_template, task.dir, account, domain, mode).
{{- if .GlobalEmpty}} The global bootstrap section still applies first, since the
global method is empty.{{end}} Then you are done — don't invent files for empty
layers.
{{if .GlobalEmpty}}
## Global method (the global layer ~/.kovan/method/global/ is empty)

Bootstrap the user's universal method first:
- Read the user's existing rules in {{.ClaudeMD}}.
{{- if .Reference}}
- Lift the universal rules from {{.Reference}} (its AGENTS.md): plan-then-implement,
  ask-before-push, the comment rule, and commit style.
{{- end}}
- Ask the user for their soul/voice — who they are, how they write.
- Write focused files into ~/.kovan/method/global/ (e.g. soul.md, methodology.md).
- Then run: kovan method link
{{end}}
## This repo ({{.Repo}})

Read its AGENTS.md / CLAUDE.md / task dir / skills / any worktree setup script,
then sort what you find across the five layers — this is the judgment work:

- Before assigning anything to project-private or in-repo, ASK where it really
  belongs: is it shared across several of this employer's repos? → account layer.
  Is it about a kind of work rather than this repo? → domain layer. Don't
  duplicate the same rule per project; lift it once into the broadest layer that
  fits.
- Sibling repos: look for other repos under the same account/employer (often
  sibling directories). Shared knowledge between them belongs in the account
  layer — and watch for one repo's content that has leaked into another's docs,
  and lift it out.
- AGENTS.md is the shareable, committed layer, but it is often NOT clean. Keep
  objective project facts there. If it carries personal or machine-local content
  — VM/build setup, the user's own workflow, how they drive AI, another repo's
  internals, cross-repo ticket knowledge — PROPOSE lifting that into the right
  kovan layer (global / account / domain / project-private).
- Put genuinely repo-specific, uncommitted context in ~/.kovan/projects/<repo>/.
- Set the repo's defaults in <repo>/.kovan.yaml — worktree (prefix, base,
  branch_template), task.dir, account, domain, mode (the default task mode;
  e.g. analyze for a notes vault, write for a blog){{if .Account}} (this repo's account is {{.Account}}){{end}}.
  Tripwire: setting account: requires the token file ~/.kovan/tokens/<acct> to
  exist, or `kovan start` refuses to spawn — have the user run `claude setup-token`
  and save it there (chmod 600).
- task.dir is just the leaf folder name: kovan scaffolds task docs durably under
  ~/.kovan/projects/<repo>/<task.dir>/<id>/, never committed to the repo.
- Skills: sort each .claude/skills/<name>/ by scope — universal → global/skills/,
  repo-private → projects/<repo>/skills/ (kovan symlinks them in); genuinely
  shareable ones stay committed. Relocating a committed skill means `git rm`-ing
  it from the repo: kovan is no-clobber, so a skill left committed SHADOWS the
  layered copy and the move silently does nothing.
- Port repo-specific machine setup (build caches, prebuilt objects, include pins)
  into <repo>/.kovan/setup.sh.

## The layer split

Show the user how each piece is sorted across the five layers, then wait for
their go — and remember it is iterative, refine over rounds:
- global  → ~/.kovan/method/global/           universal method + skills, every agent
- account → ~/.kovan/method/accounts/<acct>/  shared across one employer's repos
- domain  → ~/.kovan/method/domains/<domain>/ a kind of work (code, writing, …)
- private → ~/.kovan/projects/<repo>/         repo-specific, NOT committed
- in-repo → committed in the repo             shareable: clean AGENTS.md, skills, tickets
