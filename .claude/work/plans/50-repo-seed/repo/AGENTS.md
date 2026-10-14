# AGENTS.md

Guidance for people and coding agents working in this repository. Keep it short: the reasons live in `.claude/work/plans/`.

## What this is

Source Code Portal (SCP): one static Go binary that aggregates a GitHub organisation into a dashboard, per-repository
documentation, commit logs, local badges, package-manifest intelligence and an LLM-readable surface. React + shadcn/ui + TypeScript SPA
embedded in the binary. Module `github.com/cantara/source-code-portal`, Go 1.27.2 (`go.mod`), Node 24.21 and pnpm 12.10.1 for `web/`.
Status: seed (bootstrap step 1 of design 16 section 4.12): every package exists as a `doc.go` and the architecture test is green;
features land one pull request at a time.

## The design is binding

Read in this order when a rule is unclear (the design package is `.claude/work/plans/`: index `README.md`, map `50-repo-seed/design-map.md`):
`04-decisions.md` (incl. section 7 amendments) > `10-architecture.md` > `11-adr/` > `20-design/NN-*.md` (one document per unit, each
with its tasks) > background (`00`-`03`, `05-research`, `30-verification`).
Planning: `40-implementation-plan.md` (milestones M0-M11 with task ids) and `41-test-plan.md` say what to build next and how it is proven.
If you believe the architecture or an ADR must change, do not diverge silently: write the proposal with evidence in a new ADR
(`.claude/work/plans/11-adr/`, template `ADR-000-template.md`) and a "Deviations proposed" note in the pull request, then follow the owner's decision.

## Architecture in one screen

- Package by feature under `internal/`; one composition root, `cmd/scp`, which alone wires everything (`cmd/scp/internal/bridge` holds
  the small adapters between one feature's data and another's consumer interface).
- **Dependencies point down by rank, equal rank never imports equal rank.** Ranks: 0 kernel and leaf infrastructure (`scm freshness
  routes safehtml readmodel webapp apispec platform/{secret,netguard,telemetry,lifecycle}` ...), 1 (`job`, `store`, `platform/{logx,config}`,
  `github/auth`, generated `*api`, `docs/internal/render`), 2 (`refresh`, `reactor`, `github`, `httpapi`, `store/{sqlite,memory}`,
  `platform/httpx`), 3 (`httpapi/{authz,validate}`), 4 features and services (`catalog commits docs manifests badges pages
  integrations media webhook access live ops`), 5 (`overview`, `bridge`, `integrations/{jenkins,snyk}`, `manifests/ecosystems/*`), 6 `cmd/*`.
- **No feature imports another feature.** Features talk through the snapshot (kernel value types in `scm`), consumer-defined interfaces
  satisfied by adapters or bridges, job registration with `refresh`, and normalised events through `reactor`.
- **The exact allowed-import matrix is `internal/archtest/rules.go`**, a closed world: every package must have a row and every row a
  package. `go test ./internal/archtest/...` (`make arch`) checks the tree, the rank proof, the owned heavy libraries (goldmark/chroma only in
  `docs/internal/render`, bluemonday only in `safehtml`, SQLite only in `store/sqlite`, OTel SDK only in `platform/telemetry`, kin-openapi only
  in `httpapi/validate`, ...) and the drift of the depguard block in `.golangci.yml`, which is generated from the same table.
- Every package has a `doc.go` that states its responsibility and its allowed imports; `TestDocGoStatesTheRow` fails when the list drifts from the row.

## Rules the tooling enforces (do not fight them)

G1 no packages named util/common/helper/model/types/api/base. G2 no `log`, no `fmt.Print*` outside `cmd/scp`, `math/rand/v2` only.
G3 no `slog` globals. G4 no `http.DefaultClient`, `http.ListenAndServe`, `http.Server{}` outside `platform/httpx` and `cmd/scp`.
G5 no `init()` and no mutable package variables in our code. G6 heavy libraries are owned (above). G7 only the owner imports its `<feature>api`.
G8 `refresh` and `scm` never learn about each other. G9 `os.Getenv/Args/Exit`, signals and process globals only in `cmd/scp`; time arrives as
a value. G10 a package that starts a goroutine has a blocking `Run(ctx)` and `goleak` in `TestMain`. G11 telemetry through injected
`Meter`/`Tracer` (OTel API modules), never the global accessors. Source rules: no server URL literal outside `internal/routes` (use the
builders), no `t.Skip` without an issue number, no `TODO` without `(#123)`.

## Commands

```
make help          list targets
make check         the pull-request gate at the current bootstrap step (gen-check modverify lint arch test lint-web vuln licenses)
make arch          closed-world matrix, rank proof, depguard drift, source rules
make arch-fix      regenerate the depguard block of .golangci.yml after editing internal/archtest/rules.go
make test-go       go test -race ./...            make test-web   vitest        make lint   golangci-lint v2.14.0 (binary) + vet
make gen           go generate + pnpm gen (OpenAPI bundles, TS types, route tree) + THIRD_PARTY.md; generated files are committed
make gen-check     fails when generation changes anything (CI runs it)
make build         pnpm build, then the static binary with the SPA embedded -> bin/scp        make build-go   Go only, placeholder SPA
make tools         verify the nested tools module (oapi-codegen, govulncheck, go-licenses, oasdiff)
make dev           fake GitHub + portal + Vite (needs the dev tooling of design 16 section 4.11 and 05)
```

`GOTOOLCHAIN=local` in CI: install the Go version in `go.mod` yourself. golangci-lint is a pinned binary, not `go install`.

## Adding things

- **A package**: create it, add its row to `internal/archtest/rules.go` (drop `Planned` if it was one), run `make arch-fix`, write `doc.go`.
  The row is the review of the dependency decision; a new edge needs a reason in the pull request.
- **A dependency**: it must be in the owner column of the matrix; add its owner rule if heavy; Renovate keeps versions, exact pins only in `web/`.
- **An interface between features**: the consumer defines it with stdlib and kernel types; add its compile-time assertion to `cmd/scp/assert.go`.
- **A config key**: owner unit, default, range, env name in the config table; the documented-keys equals consumed-keys test must stay green.
- **An endpoint**: edit the OpenAPI fragment first (spec-first, ADR-020), regenerate, then implement the strict handler.
- **A route**: `internal/routes` table first (one name, one pattern); the SPA shape in `web/routes.ts` and the golden tests change together.
- **An ADR**: copy `.claude/work/plans/11-adr/ADR-000-template.md` to the next free number (025 is the last design-phase ADR); Status starts as Proposed.

## Conventions

- Go: `gofmt` + `goimports` (local prefix), errors wrapped with `%w` and classified by behaviour interfaces (`job.RateLimited`, `Permanent`,
  `Gone`), sentinels allowed, `context.Context` first, `log/slog` with static messages and snake_case keys, no panics for control flow.
  Table-driven tests with hand-written fakes named after behaviour; `testing/synctest` for time; `-race` always; fuzz targets for parsers.
- Web: feature-sliced (`web/src/features/<x>` imports other features only through their `index.ts`, one direction, `eslint-plugin-boundaries`);
  no hard-coded portal URLs (typed routes and `lib/routes.ts`); HTML only through `shared/safe-html`; no `fetch`, no `localStorage` outside `lib/`;
  every user-visible string is a Lingui message; exact dependency pins; `pnpm` policy in `web/pnpm-workspace.yaml`.
- Generated files (`*.gen.go`, `internal/apispec/*.bundled.yaml`, `web/src/lib/api/schema.d.ts`, `web/src/app/routeTree.gen.ts`,
  `THIRD_PARTY.md`) are committed, never edited by hand, and drift-checked.
- Commits: Conventional Commits (`feat(commits): ...`, `fix(webhook): ...`, `chore(deps): ...`), one logical change, tests with the change.
  A change that touches the matrix, a contract or a decision says so in the pull request.
- Comments say why, not history. Design notes belong under `.claude/work/` (`docs/` is user documentation only), not in code. Nothing is added without a consumer.

## Security posture (short)

Secrets only through `platform/secret` (redacting type, `*_FILE`). Outbound HTTP only through `netguard` clients (SSRF-guarded). Documentation HTML
is a `safehtml.SafeHTML` produced only by `safehtml.Sanitize`. Visibility is a security control (`httpapi/authz`): anonymous users never see
private repositories. Static CSP, no inline script or style in the SPA. See architecture section 17 and ADR-025.

## Design guardrails

`.claude/work/plans/02-design-guardrails.md` lists the guardrails (GR-01..GR-45): each has a principle, a rationale and the test that enforces it,
and architecture section 20 maps each one to its enforcement mechanism. A change that relaxes a guardrail needs an ADR.
