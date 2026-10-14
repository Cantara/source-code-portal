# CLAUDE.md

Guidance for Claude Code in this repository.

## State

This repository (`git@github.com:Cantara/source-code-portal.git`, branch `main`, Go module `github.com/cantara/source-code-portal`) is the
home of the Go rewrite of Source Code Portal (SCP). M0 (skeleton) and M1 (catalogue and sync) are done; the next milestone is M2. Coding instructions for the
repository itself are in the root `AGENTS.md`.

- **`.claude/work/plan.md` is the binding plan**: scope (v1, v1.1, later), architecture, stack, milestones M0..M6. Read it first.
- `.claude/work/README.md`: index of the development package.
- `.claude/work/design/`: short v1 design notes per area (GitHub, sync/snapshot/store, catalog, content, HTTP/API, web, build/ops,
  principles). Binding below `plan.md`.
- `.claude/work/decisions.md`: every decision taken, with where it is recorded.
- `.claude/work/reference/detailed-design/`: the earlier detailed design package (about 1,900 files). **Reference only**: consult it
  when building a part, copy what helps. It is not kept in sync; `plan.md` and `design/` win.

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

## Next: M2

The read API for the pages (`plan.md` section 5): commits and repository detail. Read `design/content.md`, `design/http-api.md` and the
M1 entry in `decisions.md` (what M1 deferred to M2 and M3).
