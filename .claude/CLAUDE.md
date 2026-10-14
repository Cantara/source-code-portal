# CLAUDE.md

Guidance for Claude Code in this repository.

## State

This repository (`git@github.com:Cantara/source-code-portal.git`, branch `main`, Go module `github.com/cantara/source-code-portal`) is the
home of the Go rewrite of Source Code Portal (SCP). No product code yet; the next step is milestone M0.

- **`.claude/work/plan.md` is the binding plan**: scope (v1, v1.1, later, out), architecture, stack, milestones M0..M5. Read it first.
- `.claude/work/decisions.md`: every decision taken, with where it is recorded.
- `.claude/work/plans/`: the earlier detailed design package (about 1,900 files). **Reference only**: consult it when building a part,
  copy what helps, ignore what `plan.md` dropped. It is not kept in sync; when it disagrees with `plan.md`, `plan.md` wins.

The Java product (`Cantara/SourceCodePortal`, checked out at `/Users/overanheim/Code/descoped/SourceCodePortal`) is read-only reference;
never modify it, push to it or open issues or pull requests on it.

## Working rules

- Decide and record (`decisions.md`); do not leave open questions in documents and do not ask the owner.
- Keep it small: no package, config key, metric or dependency before the code that needs it. Avoid over-engineering.
- Dependencies must be current (release within 18 months), widely used and pure Go; `bluemonday` is the one exception (`plan.md` section 4).
- Planning and design live under `.claude/work/`; `docs/` is user documentation only.
- Commits: Conventional Commits, no AI attribution lines. Generated files are never edited by hand.
- No test touches the network: fake GitHub server and recorded fixtures.

## Architecture in brief

One static Go binary (`CGO_ENABLED=0`) with the React SPA embedded. GitHub feeds sync jobs; jobs write SQLite (a rebuildable cache) and
publish an immutable snapshot behind an atomic pointer; request handlers read only the snapshot. Features never import features; wiring
only in `cmd/scp`. Untrusted bytes pass through bounded parsers and one sanitiser producing `SafeHTML`. Public port `:9090`, admin `:9091`.

## M0 starting point

`.claude/work/plans/50-repo-seed/` holds a validated repository seed (Makefile, CI, Dockerfiles, archtest, web scaffold). It was built for
the detailed design; at M0, take what fits `plan.md` and trim the rest (for example its 76 pre-created packages and generated import tables).
