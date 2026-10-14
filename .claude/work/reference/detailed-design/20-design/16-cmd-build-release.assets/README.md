# 16-cmd-build-release.assets

Companion of `../16-cmd-build-release.md` (design 16). Everything here is Go source or a script that the document refers to by path. The files that go to the repository root verbatim (Makefile, workflows, GoReleaser, Renovate, Dockerfiles, `.golangci.yml`, `tools/`) are in `../16-seed-files/`, not here.

**Standalone build.** This directory alone is not compilable: `cmd/scp` imports `internal/platform/*` from unit 02. To build and test the skeleton copy `../02-platform.assets/internal/platform` into `internal/platform`, copy `../16-seed-files/.golangci.yml` to the module root (the depguard test and the linter read it) and run the commands below. `goreleaser check` additionally needs a git remote named `origin`.

The module is a scratch module named `github.com/cantara/source-code-portal` (`go.mod`, Go 1.27.1 was the toolchain on the machine; the product pins 1.27.2). It is **partial**: only the packages this unit imports exist, and some of those are stand-ins (below).

## Directories

| Path | What it is | Status |
|---|---|---|
| `cmd/scp/` | the composition root and CLI: `main`, `Env`, `run`, every command, configuration loading, `plan.go` (stage plan), `refresh.go`, `storestage.go`, `assert.go`, OTLP and network factories, `default.yaml`, tests | compiled, vetted, linted (golangci-lint v2.14.0 with the product `.golangci.yml`, `./cmd/... ./internal/archtest/...`: 0 issues), tested with `-race` |
| `cmd/scp/internal/bridge/` | the bridge types of design 16 section 5 (`PlanSink`, `RateGate`, `Tracker`, `Retrier`, `Watermarks`, `TaskSink`, `ParkedSink`, `LateSink`, `ReactorMetrics`) and their tests | compiled and tested (11 tests) |
| `internal/archtest/` | the architecture test: the rule table (`rules.go`), checks, depguard generator, source rules, loader | compiled; 9 tests; the real-tree tests fail in a partial tree only with `tree.nopackage` |
| `compilecheck/` | the consumer interfaces of architecture 8.7 re-declared, and one adapter type that must satisfy all of them | compiled; a separate tiny module that requires the scratch module |
| `docs/reference/` | the generated configuration reference (`config.md`, `config.schema.json`, `env.txt`), produced by `go generate ./cmd/scp` | generated from the skeleton's types; the product regenerates it |
| `web/` | the embed package: `embed_spa.go` (tag `spa`, `//go:embed all:dist`), `embed_placeholder.go`, the committed placeholder page | compiled both ways |
| `tools/go.mod` | the nested tools module's `go.mod` (the complete module, with `go.sum` and `third_party.tmpl`, is `../16-seed-files/tools/`) | |
| `union/` | scripts that assemble the **union module**: the real assets of the sibling units overlaid on this skeleton | see below |
| `internal/{github,docs}` | **stand-ins** written from the sibling documents | see below; `assemble.sh` copies only these two |
| `internal/{catalog,freshness,job,ops,reactor,readmodel,refresh,routes,scm,store,webhook}` | trimmed copies of the sibling units' assets that let the skeleton compile on its own | the union module replaces them by the real ones |

## What was compile-checked, and what was not

Compiled and run on 2026-10-14: everything in `cmd/`, `internal/archtest/`, `compilecheck/`, `web/`.
`go vet ./cmd/... ./internal/archtest/...`, `CGO_ENABLED=1 go test -race -count=1 ./cmd/...` and `go test -tags devtools -run TestDev ./cmd/scp` pass.

**Not** compile-checked: the product `wire.go` (design 16 section 4.2.2 is a specification; it needs every feature unit's real `New`).

## Stand-ins

Unit 05 ships **no code assets** (`05-github-adapter.assets/` holds fake-GitHub worlds only), so `internal/github` here is a stand-in written from 05's document: only the identifiers `cmd/scp` uses (`Client.Admit`, `ReserveClass` and the four classes, `DoctorOptions`, `Finding`, `ManifestOptions`, `ManifestResult`, `Network`, `HTTPConfig`, `RunAppManifest`, `Severity`, `Feature`). `internal/docs/schema` is likewise a minimal stand-in for 10's descriptor schema (used by `descriptor lint` and `config schema --descriptor`). The github stand-in proves the **shape** the document asks of 05 and nothing about 05's implementation. When 05 ships its package, delete the stand-in and re-run the union (below).

The other packages under `internal/` are trimmed copies of the sibling units' assets (kernel, store, refresh, ops, catalog, ...), enough to compile the skeleton standalone; the union module replaces them by the real ones.

## The union module

```
union/assemble.sh <empty-dir>            # overlays the real 01, 02, 03, 04, 08, 15 assets on cmd/scp; runs go get / go mod tidy
cd <dir> && go build ./... && go vet ./cmd/...
go test ./cmd/scp/internal/bridge        # passes against the real refresh, job, store, ops, reactor
union/swap-catalog.py <dir> [--propose-fixes]
```

`swap-catalog.py` replaces the skeleton's `catalogStub` by the real `catalog.Config` to reproduce the cross-unit findings of design 16 section 14.1: without `--propose-fixes` the build fails at `config.Describe` (U-1); with it, three mechanical fixes are applied **to the copy** and `TestUnionCatalogMatchersLoad` then fails because a one-key matcher mapping cannot be assigned by `config.Load` (U-2).

Expected results in the union tree, all of them explained in design 16 section 12.5:

| Check | Expected |
|---|---|
| `go build ./...`, `go vet ./cmd/...` | pass |
| `go test ./cmd/scp/internal/bridge` | pass |
| `go test ./cmd/scp` before the swap | `TestStoreCommands` fails (it writes text files as fake databases; the real `sqlite.CheckFile` refuses them) and `TestUnionCatalogMatchersLoad` fails (the stand-in catalog section accepts no matcher keys) |
| `go test ./internal/archtest` | the only expected failures in a partial tree are `tree.nopackage` (rows of packages that are not overlaid) and, from the overlay, `tree.norule` for the local `ghverify` and `logx` copies, one `import` finding for the local `httpapi` copy (U-4) and five `route-literal` findings in `internal/catalog` (U-3). In the scratch module without the overlay only `tree.nopackage` appears. `TestDepguardInSync` needs `.golangci.yml` from `../16-seed-files/` at the module root |

`union/union_test.go.txt` is copied to `cmd/scp/union_test.go` by `assemble.sh` (it needs the real `catalog.Config`).

## Using these files in the repository

Design 16 section 13, tasks B-1 and B-2: copy `cmd/`, `internal/archtest/`, `web/` and `docs/reference/` into the repository tree; do **not** copy the stand-ins under `internal/` (the real packages come from the units that own them) and not `compilecheck/` or `union/`. Remove `catalogStub` and `storeStub` from `cmd/scp/config.go` when the real `catalog.Config` and `sqlite.Config` exist, and mark each row of `internal/archtest/rules.go` as no longer `Planned` as its package lands.
