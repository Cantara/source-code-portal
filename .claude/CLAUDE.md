# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## State of this repository

This repository (`git@github.com:Cantara/source-code-portal.git`, default branch `main`, Go module `github.com/cantara/source-code-portal`) is the home of the Go rewrite of Source Code Portal (SCP). It contains **no product code yet**: `.claude/work/plans/` (about 2,000 files, 21 MB) is the complete greenfield design package, including the full transformation from the old Java SCP, written 2026-10-13/14. The Java product (`Cantara/SourceCodePortal`, checked out at `/Users/overanheim/Code/descoped/SourceCodePortal`) is read-only reference; never modify it, push to it or open issues or pull requests on it.

Current phase: planning. Do not write product code yet. Work on the plans means editing documents under `.claude/work/plans/`.

Layout rule (owner, 2026-10-14): all project work material (plans, design, notes, roadmap, ...) lives under `.claude/work/`. `docs/` is reserved for user documentation and must never hold planning or design documents. The seed follows it: `bootstrap.sh` lays `50-repo-seed/repo/` into this repository in place (no design mirror, nothing overwritten), the design map and seed notes live in `50-repo-seed/`, new ADRs go to `11-adr/` from ADR-026 (template `ADR-000-template.md`), and every tool skips `.claude/` (seed deviation S-12, superseded).

## Where to start

- `.claude/work/plans/llms.txt`: one-line purpose of every key document. Read first.
- `.claude/work/plans/README.md`: index, reading order, glossary, the 23 visible behaviour changes versus Java (section 8), and the owner confirmations OC-01..OC-24 with the default that ships (section 9). Section 5.6 lists the plans and seed.
- `.claude/work/plans/40-implementation-plan.md`: milestones M0..M11, spikes SP-1..SP-10, owner decisions O-1..O-14, Definition of Done D1..D25, plan deviations DP-1..7 and planning findings PF-1..12.
- `.claude/work/plans/41-test-plan.md`: test strategy, deviations D-T1..D-T8, open spikes S-T1..S-T6.

## Precedence (binding)

`04-decisions.md` (including section 7 amendments A-01..A-33) > `10-architecture.md` > `11-adr/ADR-001..025` > unit designs `20-design/NN-*.md` > research `05-research/` > Java review (`00`..`03`, `review/`). When two documents disagree the higher one wins and the disagreement is a finding, not a licence. A change to a decision is proposed only in a "Deviations" section with evidence; never diverge silently. `02-do-not-inherit.md` (105 DNI items) is binding; `10-architecture.md` section 20 maps each to its preventing mechanism.

## Layout of `.claude/work/plans/`

- `20-design/`: 18 unit designs (`01-kernel` .. `18-web-pages`), each with `<unit>.assets/` reference code (own `go.mod`, compiled and tested at design time per the docs; units 05 and 13 have no code). Catalogues that are the single source for a key space: `90-config-reference.md` (370 config keys), `91-metrics-catalogue.md`, `92-error-catalogue.md`, `93-deviations-and-open-questions.md` (authoritative for open items), `01-kernel.routes.tsv`, and `api/` (OpenAPI roots). **Copy OpenAPI from `20-design/api/`, not from the older `common.yaml` copies under some `.assets/`.**
- `30-verification/`: one report per unit plus the architecture reviews and `cross-design-consistency.md`. Newer than the row texts in 93 where they conflict.
- `50-repo-seed/`: `bootstrap.sh` plus `repo/` (the starter files of this repository, laid in at M0: Makefile, archtest, CI, Dockerfiles, web scaffold, 76 `doc.go`). `repo/CLAUDE.md` and `repo/AGENTS.md` become the root `CLAUDE.md` and `AGENTS.md` of this repository at M0 (product-build guidance); this file stays the planning guidance. `repo/` is derived from `20-design/16-seed-files/` (the unit 16 design-time seed) plus the unit 17 prototype, with proposed deviations S-1..S-13 (`50-repo-seed/README.md`); where the two disagree, `50-repo-seed/repo/` is the newer copy, pending owner sign-off of S-1..S-13.
- `05-research/`, `review/`, `00`..`03`: evidence and Java-side background. Prototypes are throwaway.

## Architecture in brief (full text: `10-architecture.md`, ADR-001/018/024)

One static Go binary (Go 1.27.2, `CGO_ENABLED=0`) with an embedded React 19 + shadcn/ui (Base UI) + TypeScript SPA from `web/` (Node is build-time only). API-first: the OpenAPI 3.1 contract under `/api/v1` is the source of truth; `llms.txt`, raw Markdown and JSON are server-generated and independent of the SPA. Two listeners: public `:9090`, admin `:9091`.

Data direction: GitHub feeds **jobs**; jobs write SQLite (rebuildable last-good cache) and publish **immutable snapshots** behind an atomic pointer; request handlers read only the current snapshot. Nothing on a request path talks to GitHub or the database.

Rules that hold the design together:
1. Package by feature under `internal/`. Dependencies point down a **rank table**; equal rank never imports equal rank. The exact allowed-import matrix is `10-architecture.md` section 5.2, implemented as a closed-world `internal/archtest` whose table also generates the depguard rules.
2. **Features never import features.** They communicate through the snapshot (kernel value types in `scm`), consumer-defined interfaces (direct, or bridged in `cmd/scp/internal/bridge`), job specs registered with `refresh`, and `scm.Event`s fanned out by `reactor.Fan`.
3. Composition only in `cmd/scp`; no DI framework, no globals, no `init()` in own code.
4. Failure is data: every item carries freshness; serve the last good snapshot; readiness never regresses.
5. Untrusted bytes (README, descriptor, manifests, webhook bodies) pass only through pure, bounded, fuzzed parsers and the single sanitiser that produces `SafeHTML`.

## Rules for editing the plans

- Never invent a package, config key, metric, route or error code. Add it to the catalogue that owns it (90, 91, 92, `routes.Table`, the OpenAPI fragment) in the same change as the design text that needs it.
- Library and version facts move fast. Every pin was read on 2026-10-13/14; re-verify against the primary source and date it before relying on one (task `PL-9`).
- Open owner decisions ship their stated default; keep them open in `README.md` section 9 / `40` section 2 rather than silently deciding them.
- Known unresolved inconsistencies (decide before first push, do not paper over): `schema.d.ts` path `web/src/api/` (design 16 4.6.3) vs `web/src/lib/api/` (architecture, seed); open items CD-CF-10, CD-MT-06, CD-DM-03 and CD-FX-01..05 in 93 section 3; seed deviations S-1..S-13 in `50-repo-seed/README.md`.
- Documents say `~/.scp` in places (the package's earlier home). `bootstrap.sh` resolves the design package and the repository root from its own location (`.claude/work/plans/50-repo-seed/`).

## Commands (once the seed is laid in, at M0)

From the seed's `AGENTS.md` and `Makefile`; they apply after `bootstrap.sh` has laid the seed into this repository:

```
.claude/work/plans/50-repo-seed/bootstrap.sh --commit --install   # from the repository root; lists conflicts and copies nothing if a file differs
make check      # PR gate at bootstrap step 1: gen-check modverify lint arch test lint-web vuln licenses
make arch       # archtest: closed-world matrix, rank proof, depguard drift, source rules
make arch-fix   # regenerate the depguard block of .golangci.yml after editing internal/archtest/rules.go
make test-go    # go test -race ./...   (single test: go test -race -run TestName ./internal/<pkg>/)
make test-web   # vitest
make gen / make gen-check   # generated source is committed and drift-checked
make build      # pnpm build, then static binary with SPA embedded -> bin/scp
```

Toolchain pins: Go 1.27.2 (`GOTOOLCHAIN=local` in CI), Node 24.21.0, pnpm 12.10.1, golangci-lint v2.14.0 (binary). `-race` needs cgo, so CI tests use it while releases are `CGO_ENABLED=0`. In this repository, commits are Conventional Commits and generated files are never edited by hand.

## Build order (when implementation starts)

Run the M0 spikes first (W1 oapi-codegen 3.1 subset, W3 `ProblemMux`, W21 modernc read-only/`temp_store`, W22 prototype of `Planner`/`Gate`/`Timeout`/`reactor`/`lifecycle.Plan` in synctest bubbles; plus the live-GitHub session SP-10 needing a test organisation with a GitHub App). Then land units in rank order: kernel (01), platform (02), read model and store (03), engine/webhook/GitHub adapter (04, 05; unit 05 has no reference code and is the highest risk), HTTP/contract/catalogue (06, 07, 08), features (09..15), SPA (17, 18), image and release (16). One unit at a time: read its design, its verification report and its rows in 93; each landing flips its archtest row from `Planned`. No test touches the network; use the fake GitHub and recorded fixtures.
