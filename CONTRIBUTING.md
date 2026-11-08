# Contributing

The same way of working applies to everyone on this project, people and coding agents alike (agents read it in `AGENTS.md` and
`.claude/CLAUDE.md`).

## Goal first

Every piece of work starts with three things, written down before the code, in the pull request and in its design note or its row of
`.claude/work/plan.md`:

1. **Goal**: what is better for a user or an operator when this is done, in a sentence.
2. **Acceptance criteria**: observable and checkable statements, each with the test, command or page that shows it.
3. **Definition of done**: the code and its tests; `make check` green locally (`make rehearse`, the upgrade and rollback rehearsal of the
   store, is part of it); documents (`docs/`), the changelog, the code map in `.claude/CLAUDE.md` and the design note current; the pull
   request open and CI green.

## Then carry it through

- Decide the details yourself. Pick the most conventional, safest option, record it in `.claude/work/decisions.md` and go on. Do not
  wait for an answer to a question you can settle by reading the code, the documents or the tests.
- Stop only for what you cannot do yourself (a release or tag, a secret, a real-world account, something the rules below forbid) and
  name that once, with the exact step left.
- Report what is done, how it was verified and what is not verified. Do not end with an offer or a question.

## The rules that stay

- One branch, one pull request per feature; commits are Conventional Commits; nothing is pushed to `main`; the owner's team tags
  releases.
- Read-only towards GitHub; the security model is never weakened for convenience (`docs/security-model.md`).
- Data lives in SQLite and memory only loads it; an instance that dies must come back with everything it had acknowledged (`make rehearse` kills a real one to prove it).
- Deployed instances are not broken: configuration and the store change only in additive ways, and `make rehearse` proves it.
- Tests never touch the network. Generated files come from `make gen`.
- Keep it small: no package, key or dependency before the code that needs it.

Details for the repository's layout and commands: `AGENTS.md`.
