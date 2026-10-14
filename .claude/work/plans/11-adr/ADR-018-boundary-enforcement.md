# ADR-018: Boundary enforcement: closed-world archtest plus generated depguard rules

Status: Accepted. Date: 2026-10-14. Refines 04 principle 2 and D-10 (A-09).

## Context
The dependency rule (ADR-001) is only as good as its enforcement. `depguard` rules are per package, so an unlisted package is unrestricted; `go test` caches results because the `go list` child process is invisible to the test cache.

## Decision
1. `internal/archtest`: a test that runs `go list -json ./...`, requires **every** package to have a rule (closed world) and every non-`planned` rule to name an existing package (tree versus table, both directions), checks `Imports`, `TestImports` and `XTestImports` against the allowed list, reports all violations, and verifies the rule table itself (every allowed edge strictly decreases rank, the executable acyclicity proof). It reads every `.go` file and `go.mod` in the module so the test cache invalidates, and CI additionally runs it with `-count=1`.
2. `depguard` rules in `.golangci.yml` are **generated from the same table** (`go generate`, drift = CI failure) for editor and pre-commit feedback; `list-mode: lax` with broad deny and specific allow entries.
3. Global rules G1-G10 via `forbidigo` (with `analyze-types`), `sloglint` (`no-global: all`, `context: all`), `gochecknoinits`, `gochecknoglobals`, `noctx`, `contextcheck`, `bodyclose`, `errorlint`, `godox`, `usetesting`, `thelper`, `revive`, `gosec`, `modernize`.
4. Compiler-level: `internal/` at the module root and nested `internal/` for feature-private packages.
5. `go-arch-lint` v1.19.0 and `arch-go` v2.1.2 are optional; at most one extra config-driven checker is run.

## Alternatives considered
Multi-module `go.work` per feature (versioning and replace overhead), depguard alone (no closed world), arch-go as primary (extra config, no executable proof).

## Consequences
Adding a package requires adding its rule row (a deliberate friction); the same table documents the architecture. depguard and golangci-lint are GPL tools used as external binaries and are not linked into the product.

## Evidence
`05-research/04-go-architecture-patterns.md` sections 7-8 (verified locally, including the cache pitfall and the depguard lax-mode behaviour).

## Amendment 2026-10-14 (review revision)

The first table lacked rows for `web`, `docs/schema`, `httpapi/apitest`, `manifests/parsertest` and `tools`, so the test would have failed on its first run (F-04, M6). Decisions: `web` stays at the repository root as a rank-0 `embed`-only package imported by `cmd/scp` (`go:embed` cannot reach `web/dist` from `internal/`); `tools/` is a nested module outside `go list ./...`; test-helper packages are imported from `_test.go` only and are exempt from the rank comparison in that direction; `gitsource` has a `planned` row. The depguard generator emits the same table. The proof was re-run by script on the revised table (66 packages, 0 cycles, 0 violations).
