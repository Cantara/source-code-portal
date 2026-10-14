# 05-research/04 - Go architecture: package-by-feature, boundaries, lifecycle, hardening, testing

Date of research: 2026-10-13. Author: research subagent. Binding input: `04-decisions.md` (read first, re-read after it changed at 22:59; D-16 and D-17 are reflected). Read for context: `02-design-guardrails.md`.

How facts are marked. Every fact that depends on the live web carries `(source, 2026-10-13)`; all web verification happened on that date unless another date is shown. Other evidence tags:

- `[LOCAL]` = I ran it on `go1.27.1 darwin/arm64` in a scratch module (outside this design package). The code that matters is reproduced in this report.
- `[GH-API]` = read through the GitHub REST API (`gh api`) on 2026-10-13 (repo metadata, directory listings, file contents, release lists).
- `[DESIGN]` = my reasoning or recommendation, not a fact from the web.
- `[UNVERIFIED]` = could not be confirmed; listed again at the end.

Source URLs are written without the scheme to keep lines short; all are `https://`.

---

## 1. Executive summary (10 lines)

1. Recommended layout A: vertical feature packages under `internal/` (catalog, docs, commits, badges, manifests, pages, integrations), two tiny dependency-free kernel packages (`scm`, `freshness`), adapters that import only the kernel (`github`, `store/sqlite`), one cross-feature read aggregator (`overview`), and the only composition root in `cmd/scp`.
2. Features never import each other. Each feature declares its own 1-3 method consumer interfaces whose signatures use only stdlib and kernel types, so any adapter satisfies them structurally; the wiring call in `cmd/scp` is the compile-time conformance check. Fat producer interfaces (Grafana `DashboardService` 21 methods, Woodpecker `Store` 112 methods) are the anti-pattern.
3. Enforce boundaries in three layers: the compiler (`internal/`, including nested `feature/internal/...`), a closed-world `go list` test (every package must have a rule; prototype verified, code in section 8), and depguard allow-lists in golangci-lint v2.14.0 for editor-time feedback; go-arch-lint v1.19.0 and arch-go v2.1.2 were run and work, but are optional.
4. Proposed amendments to 04 section 5 (all in section 13, with evidence): add `scm` and `freshness`, add `overview`, rename `internal/sync` to `internal/refresh` (it shadows stdlib `sync`), replace the single global `Snapshot` by per-feature immutable state plus a generic record `Store`, drop `platform/clock` in favour of `testing/synctest`.
5. Target Go 1.27 (current: go1.27.2 released 2026-10-08; Go 1.25 is already unsupported). Use `testing/synctest` plus `httptest.NewTestServer` (in-memory network, new in 1.27) for time and upstream scenarios: a 10 s client timeout against a 30 s slow upstream ran in about 0 ms of wall time with exact fake elapsed time [LOCAL].
6. Lifecycle: every long-lived component exposes a blocking `Run(ctx) error`; constructors start no goroutines; shutdown order is readiness off, drain delay, HTTP servers, workers, store, telemetry. A 60-line reference runner and a synctest ordering test are included and pass under `-race` [LOCAL].
7. HTTP hardening is a constructor, not a checklist: `httpx.NewServer` cannot be built without `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`/`MaxHeaderBytes`; middleware order request-id, recover (re-panic `http.ErrAbortHandler`), body limit; SSE needs per-handler write deadlines and a shutdown hook or `Shutdown` hangs.
8. Testing architecture: fake GitHub as an `http.Handler` with fault injection, `Store` contract suite (Dex pattern), golden files via a 20-line `-update` helper on `go-cmp`, native fuzz seeds always run in `go test`, `goleak` in `TestMain` for non-bubble packages (v1.3.0 verified on 1.27.1), `run()`-based end-to-end tests.
9. Conflict inside 04: D-11 says `CGO_ENABLED=0` and D-10 says `-race`; the race detector requires cgo, so the CI test job must run with cgo enabled and only the release build with `CGO_ENABLED=0`.
10. Biggest risks: the kernel growing into a models bucket, the aggregator becoming a god package, and rules rotting if the closed-world test is not mandatory in CI; mitigations (admission criteria, size budgets, "unlisted package fails") are in sections 7 and 8.

---

## 2. Toolchain and ecosystem facts that shape the architecture

| Fact | Source | Verified |
|---|---|---|
| Go 1.27 released 2026-08-19, latest patch go1.27.2 (2026-10-08). Go 1.26 released 2026-02-10, latest go1.26.9 (2026-10-08). Supported: 1.27 and 1.26. Go 1.25 (latest go1.25.14, 2026-08-19) is no longer supported. | go.dev/doc/devel/release | 2026-10-13 |
| Go 1.27: `encoding/json` is now backed by the v2 implementation (behaviour preserved, error text may differ; opt out with `GOEXPERIMENT=nojsonv2`, opt-out expected to be removed); `encoding/json/v2` and `jsontext` are new. | go.dev/doc/go1.27 | 2026-10-13 |
| Go 1.27: `httptest.NewTestServer(t, handler)` uses an in-memory fake network suitable for `testing/synctest`; `synctest.Sleep` added; `goroutineleak` profile is GA (also served at `/debug/pprof/goroutineleak`); `Server.MaxHeaderValueCount`; HTTP/1 `Response.Body` drains unread content on close up to a limit; `go test` runs the `stdversion` vet check; the `printf` vet check now flags `%w` with a pointer-to-error-type argument. | go.dev/doc/go1.27, pkg.go.dev/net/http/httptest#NewTestServer, pkg.go.dev/testing/synctest | 2026-10-13 |
| Go 1.26: `errors.AsType[E error](err) (E, bool)` (docs: "For most uses, prefer AsType"); `new(expr)`; `slog.NewMultiHandler`; `T.ArtifactDir`; `go fix` rebuilt on the vet framework with modernizers. | go.dev/doc/go1.26, pkg.go.dev/errors | 2026-10-13 |
| Go 1.25: `testing/synctest` graduated (`synctest.Test`, `Wait`); `sync.WaitGroup.Go`; `net/http.CrossOriginProtection`; container-aware `GOMAXPROCS` (cgroup CPU limit, updated at runtime); `go.mod` `ignore` directive; vet `waitgroup` and `hostport` analyzers. | go.dev/doc/go1.25 | 2026-10-13 |
| Go 1.24: `testing.T.Context`, `B.Loop`, `os.Root`/`os.OpenRoot` (traversal-safe directory access), `tool` directive in `go.mod`, `omitzero`, `runtime.AddCleanup`, `crypto/rand.Text` (>= 128 bits, base32). | go.dev/doc/go1.24, pkg.go.dev/crypto/rand#Text | 2026-10-13 |
| `ServeMux` patterns with methods and wildcards, `{$}`, most-specific-wins, conflicting registrations panic at registration time, `Request.PathValue`. | go.dev/blog/routing-enhancements (Go team, 13 Feb 2024) | 2026-10-13 |
| The official layout guidance says server projects keep their packages in `internal/` and commands in `cmd/`; it does not discuss package-by-feature versus layers. | go.dev/doc/modules/layout | 2026-10-13 |
| golang-standards/project-layout states in its own README that it is "NOT an official standard defined by the core Go dev team" and points to go.dev/doc/modules/layout. | github.com/golang-standards/project-layout README via gh api | 2026-10-13 |
| Effective Go carries the note "written for Go's release in 2009 and is not actively updated ... does not cover generics, modules, or libraries added since." Use it for naming and idioms only. | go.dev/doc/effective_go | 2026-10-13 |
| golangci-lint v2.14.0 (2026-09-24, GPL-3.0; used as an external tool, not linked). Its docs "recommend binary installation" and "don't recommend using `go tool`". | github.com/golangci/golangci-lint releases [GH-API]; golangci-lint.run/docs/welcome/install/local/ | 2026-10-13 |
| depguard: latest module tag v2.2.1, repo active (push 2026-09-22), GPL-3.0; the GitHub "latest release" object is still v2.0.0 (2023), so pin by golangci-lint version, not depguard. | github.com/OpenPeeDeeP/depguard [GH-API] | 2026-10-13 |
| go-arch-lint v1.19.0 (2026-09-07), MIT, `.go-arch-lint.yml` `version: 3`, building from source requires Go 1.25+. arch-go v2.1.2 (2026-02-03), MIT, repo active (push 2026-10-13), `arch-go.yml`, can run from `go test` via its `api` package. | github.com/fe3dback/go-arch-lint, github.com/arch-go/arch-go [GH-API + README] | 2026-10-13 |
| goleak v1.3.0 (tagged 2023-10-24; last commit 2026-09-15), MIT, "supports the two most recent minor versions of Go". Works on go1.27.1 [LOCAL]. | github.com/uber-go/goleak [GH-API + README] | 2026-10-13 |
| `golang.org/x/sync` latest v0.24.0 (2026-09-23) (pkg.go.dev's "not the latest" banner is wrong; module proxy says v0.24.0 is latest [LOCAL `go list -m -versions`]). | pkg.go.dev/golang.org/x/sync/errgroup | 2026-10-13 |
| oapi-codegen v2.8.0 (2026-07-17), Apache-2.0, module `github.com/oapi-codegen/oapi-codegen/v2`, builds with Go 1.25+, supports OpenAPI 3.0 and 3.1, `std-http-server` target (needs Go 1.24+), strict server, and one Go package per OpenAPI file ("import mapping"). | github.com/oapi-codegen/oapi-codegen README via gh api | 2026-10-13 |
| `oklog/run` v1.2.0 (2025-06-27), `thejerf/suture` v4.0.6, `golang/go` active. | gh api repos/... | 2026-10-13 |
| go-cmp v0.7.0 (2025-01-14, repo push 2026-06-18); goldie v2.8.0 (repo push 2025-11-22); gotest.tools v3.5.2 (repo push 2026-09-14); rogpeppe/go-internal (`testscript`) v1.16.0 (push 2026-10-05). | gh api repos/... | 2026-10-13 |
| otelhttp v0.72.0 (2026-10-02): `NewHandler`/`NewTransport` take `WithTracerProvider`, `WithMeterProvider`, `WithPropagators`; "uses the global provider if omitted". | pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp | 2026-10-13 |

Consequences: set `go 1.27.0` in `go.mod` (the `stdversion` vet check then protects against accidentally using newer symbols, and I observed it reporting `httptest.NewTestServer requires go1.27 or later (file is go1.26)` [LOCAL]); keep CI on the two supported majors only if the library surface used allows it (1.26 lacks `NewTestServer`, so either gate those tests behind a build tag or test on 1.27 only: recommend 1.27 only, single-binary product).

---

## 3. What the Go team and the established guides say (and where they are silent)

### 3.1 Layout and naming

- Server projects: "keep the Go packages implementing the server's logic in the `internal` directory"; split off to separate modules only when something becomes shareable (go.dev/doc/modules/layout, 2026-10-13).
- Package names: lower case, short; "Avoid meaningless package names like util, common, misc, api, types, and interfaces" (go.dev/wiki/CodeReviewComments, 2026-10-13); Google style: avoid `util`, `utility`, `common`, `helper`, `model`, `testhelper` (google.github.io/styleguide/go/decisions#package-names, 2026-10-13). The Go blog "Package names" (Sameer Ajmani, 4 Feb 2015) adds: package name and contents form a pair (`http.Server`, not `HTTPServer`), and "if you can't find a meaningful package name, the package abstraction boundary may be wrong" (go.dev/blog/package-names, 2026-10-13).
- Shadowing stdlib names: "It is not a good idea to use variables with the same name as standard packages other than very small scopes" (google.github.io/styleguide/go/best-practices, 2026-10-13). This applies to `internal/sync` (see section 13).
- Size: Google best practices say not to put a whole project in one package, but tightly coupled types may share a package to keep implementation detail unexported (best-practices, "package size", 2026-10-13). Warning sign found in the wild: `coder/coder/coderd` holds 181 `.go` files directly in one package, tests included [GH-API].
- Package-by-feature versus layers: the Go team does not rule on it. Ben Johnson's "Standard Package Layout" (gobeyond.dev/standard-package-layout, 10 Aug 2016, read 2026-10-13) argues against Rails-style grouping by type because of repetitive names (`controller.UserController`) and circular dependencies when types reference each other. Go forbids import cycles, which is why by-layer layouts (controllers -> services -> models, with models needing to refer back) tend to hurt in Go; this is the reasoning behind principle 1 in 04. [DESIGN]

### 3.2 Interfaces

- "Go interfaces generally belong in the package that uses values of the interface type, not the package that implements those values" and "Do not define interfaces before they are used" (go.dev/wiki/CodeReviewComments, 2026-10-13). Google decisions: "The consumer of the interface should define it (not the package implementing the interface)", "Avoid creating interfaces until a real need exists", and the adage "Functions should take interfaces as arguments but return concrete types" (decisions#interfaces, 2026-10-13).
- Exception documented in Effective Go: when a type exists only to implement an interface and has no other exported methods, "the constructor should return an interface value rather than the implementing type" (`crc32.NewIEEE` returns `hash.Hash32`) (go.dev/doc/effective_go, 2026-10-13). Use this only for sealed implementations; it is not our default.
- Attribution of "accept interfaces, return structs" to Jack Lindamood could not be tied to a primary source (WebSearch only returned secondary posts); I do not cite it as a Go-team rule. The Google style text above is the citable form.
- Ports and adapters: Cockburn's intent is "to equally be driven by users, programs, automated test or batch scripts, and to be developed and tested in isolation from its eventual run-time devices and databases"; primary (driving) and secondary (driven) ports; adapters convert between the port API and a technology (alistair.cockburn.us/hexagonal-architecture, dated 2005-09-04, read 2026-10-13). The idiomatic Go rendering: a port is a consumer-side interface (no `ports/` directory), an adapter is a package grouped by the external dependency it wraps (Ben Johnson principle 2), the primary adapter is the HTTP handler inside the feature, and the composition root chooses implementations (principle 4). [DESIGN]

### 3.3 Options, config, constructors

- Google best practices: use an option struct when a function needs many inputs; "Contexts are never included in option structs"; prefer variadic (functional) options "when most callers need no options, there are many options, or options can fail", at the cost of "a substantial amount of additional code" (best-practices, 2026-10-13).
- Dave Cheney (17 Oct 2014): functional options give defaults, growth room and self-documentation, and avoid "nil or an empty value to keep the compiler happy"; config structs make zero values ambiguous (dave.cheney.net/2014/10/17/functional-options-for-friendly-apis, 2026-10-13). The article lists no drawbacks, which is why Google's counterweight above matters.
- Mat Ryer (Grafana blog, 10 Feb 2024): `NewServer` takes all dependencies as arguments ("If a handler function wants a dependency, it can bloody well ask for it as an argument"), one `routes.go`, `main` calls `run(ctx, args, getenv, stdin, stdout, stderr) error`, tests start the whole program through `run()` and wait for `/readyz` (grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/, 2026-10-13).

### 3.4 Context

- "Contexts should not be stored inside a struct type, but instead passed to each function that needs it" (go.dev/blog/context-and-structs, Barkhuysen and Proud, 24 Feb 2021; pkg.go.dev/context; Google decisions#contexts: "Do not add a context member to a struct type"), 2026-10-13. Context keys must be unexported types with typed accessors; values are for request-scoped data only (pkg.go.dev/context, 2026-10-13).
- Available helpers: `WithoutCancel` (1.21), `AfterFunc` (1.21), `WithCancelCause`/`Cause` (1.20), `WithTimeoutCause` (1.21) (pkg.go.dev/context, 2026-10-13).

### 3.5 Errors, concurrency, globals

- Errors: give errors structure when callers must interrogate them; never distinguish by string; do not annotate when it adds no information (Google best-practices, 2026-10-13). `errors.AsType` is preferred over `errors.As` from Go 1.26 (pkg.go.dev/errors, 2026-10-13). RFC 9457 (July 2023, obsoletes RFC 7807) defines `application/problem+json` with `type`, `status`, `title`, `detail`, `instance`; clients must ignore unknown extension members; details must not expose implementation internals such as stack dumps (rfc-editor.org/rfc/rfc9457.html, 2026-10-13).
- Goroutines: "When you spawn goroutines, make it clear when or whether they exit"; "Prefer synchronous functions" (CodeReviewComments, Google decisions#goroutine-lifetimes, 2026-10-13).
- Global state: "Libraries should not force their clients to use APIs that rely on global state"; registries, singletons for backends/storage and package-level state are flagged; a default instance is acceptable only if it is a thin proxy, usable only by binaries, with a reset API; registration via `init` is fragile (best-practices "Global state", 2026-10-13). Litmus test: independent tests must not interact through the state, and no ordering requirement on `init`/flags.
- `init()`: no dedicated Google section; the above litmus tests plus `gochecknoinits` are how this is enforced in practice. [DESIGN]

---

## 4. Real open-source examples: what to copy, what to avoid

All entries read through `gh api` on 2026-10-13 (directory listings, file contents, counts computed by me).

| Repo | Observed structure | Copy | Avoid |
|---|---|---|---|
| grafana/grafana | `pkg/services/<feature>` has 77 feature directories (dashboards, folder, datasources, ngalert, ...); `services/dashboards` contains `dashboard.go`, `models.go`, `errors.go`, `accesscontrol.go`, plus `service/` and `database/` subpackages | Feature directories with implementation subpackages; error types next to the feature | `DashboardService` is defined in the producer package with 21 methods, and that package imports `accesscontrol`, `folder`, `search/model` and unified-storage protobufs: a fat producer interface drags a graph of siblings into every consumer; generated in-package mocks (`dashboard_service_mock.go`) hide the coupling |
| coder/coder | top-level `coderd`, `codersdk`, `httpmw`, `provisioner*`, `testutil`, `site` | Separate `httpmw` (middleware) and `testutil` packages | `coderd` has 181 `.go` files in one package (tests included): one package became the HTTP layer for every feature |
| authelia/authelia | `internal/{authentication,authorization,handlers,middlewares,session,storage,oidc,totp,webauthn,mocks,...}` | Everything under `internal/`; concern-named packages; separate `clock` and `random` packages (test seams) | `handlers` has 111 `.go` files (a layer bucket); a central `mocks` package; mixed concern/layer naming |
| miniflux/v2 | `internal/{api,ui,storage,model,reader,worker,template,http,config,...}` | Flat `internal`, small packages, `worker` separate from handlers | One `storage` package (18 files) serving every feature, and a shared `model` bucket: the layered shape 04 principle 1 rejects |
| woodpecker-ci/woodpecker | `server/forge/forge.go` defines `Forge` (about 18 methods) with one subpackage per implementation (`github`, `gitea`, `gitlab`, `bitbucket`, `forgejo`, ...) plus `mocks`; `server/store` | Parent package with the contract, one subpackage per adapter, a `types` package for request-level helpers | `store.Store` has 112 methods; `Forge` signatures use the shared `model` package so each adapter imports the product model. For SCP: define per-consumer slices, keep the adapter returning kernel types |
| dexidp/dex | `storage/` with `storage.go` (contract), `memory`, `sql`, `etcd`, `kubernetes`, `ent`, and `storage/conformance` | **A shared conformance suite**: `conformance.RunTests(t, newStorage func(t) storage.Storage)` run against every implementation | The contract interface itself is large; acceptable only because it is a multi-implementation plugin contract |
| usememos/memos | `store/` (with a `store/db` directory), `server/`, `provider/`; `store.Driver` interface lists attachment, memo, space... methods in one interface | Driver implementations separated from the domain-facing store | A single `Driver` interface spanning every model |
| ThreeDotsLabs/wild-workouts-go-ddd-example | `internal/{common,trainer,trainings,users}`; each service has `adapters/ app/ domain/ ports/ service/` | `service/` as the per-service composition; explicit ports/adapters naming for teaching | Five subdirectories per context is ceremony for one-implementation ports; a `common` package is a Google-style naming smell |
| benbjohnson/wtf | Root domain package, `postgres`, `http`, `mock` subpackages; `cmd/` mains | The four principles (section 3.1); hand-written mock with function fields | Last push 2022-10-31; root package holds every domain type and every producer-defined service interface, so it scales to "one models bucket" |
| go-gitea/gitea | top-level `models`, `modules`, `routers`, `services`, `templates` | Embedded assets, `cmd` | Pure layer buckets |
| ardanlabs/service | `app/{domain,sdk}`, `business/{domain,sdk,types}` | Layer-first with feature folders inside, if a team insists on layering | Not evaluated for import rules; I only read the directory listing |

---

## 5. Design topics, with recommendations

### 5.1 What a feature package is (anatomy)

Default: one Go package per feature, flat, 5 to 15 files. Typical file set for `commits`:

```
internal/commits/
  commits.go        package doc, Config, Deps, Service, New(...)
  deps.go           consumer-side interfaces (Source, Persister, Enqueuer), 1-3 methods each
  model.go          feature-private types and views
  refresh.go        Refresh(ctx, scm.RepoRef) error  (the job body; no scheduling knowledge)
  query.go          read side over the immutable state (atomic.Pointer[state])
  state.go          immutable state type, build and swap
  api.go            HTTP handlers + wire DTOs + mapping (no domain type is serialised directly)
  api_test.go       black-box handler tests (package commits_test)
  *_test.go, testdata/
```

Split into a subpackage only for one of three reasons: (1) isolate a heavy third-party dependency so its types cannot leak (`docs/internal/render` for goldmark/bluemonday/chroma), (2) there are several implementations of one port (`integrations/jenkins`, `integrations/snyk`), (3) a piece must be unreachable from the rest of the module (nested `internal/`). Do not pre-split into `domain/ app/ ports/ adapters/` (layout B below).

Compiler-enforced privacy inside a feature: a package under `internal/docs/internal/render` can be imported only by code rooted at `internal/docs`. Verified [LOCAL]: `docs` and `docs/sub` compile, while `internal/commits` importing it fails with `use of internal package example.com/scp/internal/docs/internal/render not allowed`. This is the cheapest boundary there is: it needs no tool.

### 5.2 Shared domain types without a models bucket (the kernel)

Problem: features need a common vocabulary (repository identity, ref, commit, release, tree entry, webhook event, freshness) and the adapter must return it. If the vocabulary lives in a feature, every other feature imports that feature; if it lives in a `models` package, it breaks principle 1.

Recommendation [DESIGN, aligned with D-17, which already asks for "a `scm` model: Repository, Ref, Commit, Tree, Blob, Release, WebhookEvent" so that no GitHub-specific type leaks into features]:

- `internal/scm`: value types of the source-control domain only (`RepoRef{Source, Org, Name}` with `String()`/`ParseRepoRef` as the stable key codec, `Ref`, `SHA`, `Commit`, `Release`, `TreeEntry`, `Visibility`, normalised `Event`, conditional-request validators). No I/O, no third-party imports, no service interfaces, no behaviour beyond validation and parsing.
- `internal/freshness`: `Status`, `Meta{FetchedAt, ...}`, staleness policy; the vocabulary behind principle 7 ("every fetched item has freshness and status") and the API's freshness fields (D-16).
- Admission rule for the kernel: a type enters only if it is (a) produced by an adapter and (b) consumed by at least two packages, or it crosses the HTTP boundary in the same shape for every feature. Otherwise it is feature-private. Size budget: review at 1,200 lines or 40 exported types; the import rule "imports nothing from this module and no third-party package" is mechanically checkable and keeps it from turning into a bucket.
- Everything else (views, DTOs, state) stays inside its feature. DTOs and generated wire types never cross package boundaries; `overview` consumes feature domain views, not feature DTOs.

### 5.3 Consumer-defined interfaces, done so that adapters can satisfy them

Pitfall to design out: a consumer-defined interface whose method signatures mention consumer-defined types cannot be implemented by an adapter without the adapter importing the consumer (inverting the arrow). Rule: interface signatures use only stdlib and kernel types. Then:

```go
// internal/commits/deps.go
type Source interface {
    RecentCommits(ctx context.Context, repo scm.RepoRef, ref scm.Ref, limit int, cond scm.Validator) (scm.Result[[]scm.Commit], error)
}
type Persister interface {
    Put(ctx context.Context, rec store.Record) error
    Get(ctx context.Context, kind, key string) (store.Record, bool, error)
}
type Enqueuer interface { Enqueue(ctx context.Context, job, key string) }
```

`*github.Client` (and a future `gitsource.Reader`, D-17) satisfies `commits.Source` without importing `commits`; `*refresh.Engine` satisfies `Enqueuer` without anyone importing `refresh`. The compile-time conformance check is the wiring call in `cmd/scp` (passing `gh` where a `commits.Source` is required fails to compile on any mismatch); do not add `var _ commits.Source = (*github.Client)(nil)` in the adapter, that is the forbidden import.

This resolves D-17's `SourceReader`: it stays a *capability name* (what an acquisition mode provides), not one Go interface. Each consumer slices it: `catalog.Source` (list repos), `commits.Source`, `docs.Source` (file at ref), `manifests.Source` (tree plus blobs). The Grafana and Woodpecker examples show the cost of the single big interface.

When a producer-side interface is fine: a multi-implementation plugin contract with a conformance suite, such as `store.Store` (memory, sqlite) tested by `store/storetest` (Dex pattern). Features still take their own slice (`Persister` above), they do not take `store.Store`.

### 5.4 Constructors, options and configuration

| Situation | Use | Why |
|---|---|---|
| Feature or engine constructed once in `cmd/scp` | `New(deps Deps, cfg Config) (*T, error)`; `Deps` is a struct of interfaces and `*slog.Logger`, `Config` is plain validated data with its own `Validate()` | Call site reads as key: value; no ambiguity from zero values because `Validate()` and defaults are explicit; Google: option struct for many inputs |
| Adapter with many optional knobs (`github.NewClient`: base URLs for GHES/GHEC, HTTP client, breaker, user agent) | `NewClient(cfg Config, opts ...Option)` | Optional, rarely changed, may fail; Google's criteria for variadic options |
| Test-only seams (jitter source, hooks) | Unexported fields set by `export_test.go` or an option documented as test-only | Keeps production API small |
| Anything with `context.Context` | Parameter, never a field or option | Google, Go blog |

Per-feature `Config` structs live in their feature (so the feature owns its keys); the root config struct that embeds them, the YAML tags, `Validate()` fan-out and the JSON Schema generation live in `cmd/scp/config.go` using machinery from `platform/config` (strict decode, `*_FILE`, env overrides). Features never import `platform/config`. This keeps D-07 ("documented keys == consumed keys") testable in one place. [DESIGN]

### 5.5 Composition root and `run()`

- `cmd/scp/main.go` is about 20 lines: build a signal context, call `run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)`, print the error, `os.Exit` once. All wiring is in `run.go` and `wire_<feature>.go` files in `package main`; tests in `package main` call `run()` with a fake `getenv`, a `:0` listener and the fake GitHub (Mat Ryer pattern, section 3.3). This stays inside D-01 (composition root in `cmd/scp`); promote the wiring to `internal/app` only if a second binary appears or `wire*.go` grows beyond roughly 600 lines. [DESIGN]
- Wiring is explicit constructor calls; no DI framework (GR-01, GR-03). Order: platform (logger, telemetry providers) -> store -> adapters -> features -> engine registration -> aggregator -> HTTP mux -> `lifecycle.Run`.
- No globals: the logger is created in `cmd/scp` and passed down. `slog.SetDefault` is allowed only in `cmd/scp` (Google's "default instance" criteria: only binaries). Pass OpenTelemetry providers explicitly to `otelhttp.NewHandler/NewTransport` (`WithTracerProvider`, `WithMeterProvider`, `WithPropagators`) because the default is the global provider (pkg.go.dev otelhttp, 2026-10-13).
- `init()`: none in our code. Accepted exceptions are third-party self-registration (`modernc.org/sqlite` registers its `database/sql` driver in `init`) confined to `store/sqlite`, and `//go:embed` (not an `init`).

### 5.6 Context propagation rules

1. First parameter, named `ctx`; never stored; never inside option structs; key types unexported.
2. Request context goes handler -> use case -> adapter. Adapters add per-upstream timeouts with `context.WithTimeout`; use the `...Cause` variants so logs say why (`WithTimeoutCause(ctx, d, errUpstreamTimeout)`).
3. A webhook handler never hands `r.Context()` to asynchronous work (it is cancelled when the response is written). It verifies, dedupes, `Enqueue`s by key and returns 202 (GitHub requires a 2XX within 10 s and recommends acknowledging then processing in the background: docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks, 2026-10-13). If trace values must survive, use `context.WithoutCancel` plus a fresh timeout.
4. Workers get the engine's context, which is cancelled only after HTTP has drained (section 9).
5. Shutdown work uses `context.WithTimeout(context.WithoutCancel(rootCtx), budget)` so it is not already cancelled by the signal that triggered it.
6. Tests: `t.Context()` (Go 1.24) instead of `context.Background()` (lint: `usetesting`).

### 5.7 Errors

- Define sentinels and typed errors in the package that detects the condition (`github.ErrNotFound`, `*github.RateLimitError`); consumers test with `errors.Is` and `errors.AsType[T]` (Go 1.26+; fall back to `errors.As` only below 1.26).
- Prefer behaviour interfaces so consumers need no import: the engine defines `interface{ RetryAfter() (time.Duration, bool) }`, `*github.RateLimitError` implements it; `httpapi` defines `interface{ HTTPStatus() int }` and feature errors implement it. This is the same structural-typing trick as consumer interfaces and is what lets `refresh` implement "rate-limit pause" (D-03) without knowing GitHub.
- Wrap with `%w` once per boundary and only when adding information (Google). Never compare `err.Error()`. Go 1.27's `printf` vet check flags `%w` with a pointer-to-error-type argument.
- HTTP edge: one function `httpapi.Write(w, r, err)` maps errors to RFC 9457 problems; `detail` helps the client fix the request and never carries stack or internal text (RFC 9457 section 5, 2026-10-13); 5xx bodies are generic and the request id is the correlation key.
- Aggregate shutdown errors with `errors.Join` (as in `lifecycle.Run`).

### 5.8 Concurrency, workers and structured lifetimes

- Contract: a long-lived component exposes `Run(ctx context.Context) error` that blocks until `ctx` is done and returns nil on a clean stop. Constructors (`New`) never start goroutines. This makes lifetimes obvious (Google), makes tests deterministic, and removes `Start`/`Stop` ordering bugs.
- `errgroup` (x/sync v0.24.0): `WithContext` cancels the derived context on the first non-nil error or when `Wait` returns; `SetLimit` and `TryGo` bound concurrency; do not reuse a `Group` across tasks; only the first error is returned (pkg.go.dev/golang.org/x/sync/errgroup, 2026-10-13). `sync.WaitGroup.Go` (Go 1.25) is enough for error-free fan-out. Loop-variable capture is per-iteration since Go 1.22, so `for _, w := range ws { g.Go(func() error { return w(ctx) }) }` is safe with `go 1.27`.
- Supervisors: `oklog/run` (v1.2.0, tiny actor group) and `thejerf/suture` (v4.0.6, supervisor trees with restarts) exist. Recommendation: neither. The 60-line runner in section 9 covers our case; process-level restart belongs to the orchestrator (crash, restart), while per-job fault isolation belongs inside `refresh` (recover at the job boundary, count it, mark the item failed, continue). A panicking job must not take the portal down, but must never be silent. [DESIGN]
- Tick loops: `time.NewTicker` with jitter from `math/rand/v2`, skip-if-running per key (the engine's keyed coalescing queue, D-03). Go 1.23+ timer semantics apply (and `asynctimerchan` is removed in 1.27: go.dev/doc/go1.27).
- Per-upstream limits: one `errgroup` limit or `x/sync/semaphore` per upstream, owned by `refresh` configuration, not by features.

### 5.9 DTOs, templates and generated code

- DTOs live next to the handler that uses them, in the feature (`api.go` or generated `api.gen.go`). Domain types are never serialised directly; the handler maps. This also gives the OpenAPI contract (D-16) one place per feature to be verified.
- Generated code: oapi-codegen v2.8.0 supports OpenAPI 3.1 and 3.0, has a `std-http-server` target and strict server, and supports "a package per OpenAPI file" with import mapping (README via gh api, 2026-10-13). This fits package-by-feature: split `api/openapi.yaml` into per-feature files that `$ref` a shared components file, and generate one package per file. Known constraint: when a strict-server spec references responses defined in another spec through import mapping, the destination spec must also be generated with `strict-server: true` (README, issue #2010). Whether to split or keep one spec is for the API-contract research task; the architectural requirement is only "wire types stay inside their feature (or its generated subpackage)". [UNVERIFIED for ogen: its README says "OpenAPI v3" and mentions 3.0.3; 3.1 support not confirmed]
- Templates: there are no HTML templates (D-16). Text templates for `llms.txt`, `robots.txt`, sitemap and badge SVGs are `//go:embed`-ed by the package that owns the output (`badges`, `overview`); there is no global `templates/` directory. The SPA host `webapp` owns `index.html` (CSP nonce or hash injection) and nothing else.
- Route registration: each feature has `Mount(mux *http.ServeMux)`. Conflicting patterns panic at registration (go.dev/blog/routing-enhancements, 2026-10-13), so a test that builds the full mux (via `run()`) catches them. `ServeMux.Handler(req)` returns the matched pattern string, verified [LOCAL] (`"GET /api/v1/repos/{repo}/commits"`, and `""` for a method that is not registered); a contract test can therefore assert that the set of registered patterns equals the set of `(method, path)` pairs in the OpenAPI document.

### 5.10 Cycles between features: ordered playbook

Greenfield advantage: cycles are cheap to prevent now and very expensive later (DoltHub's attempt to remove one cycle by moving code took 64 commits and 467 files; dolthub.com/blog/2025-03-14-go-import-cycle-strategies, 14 Mar 2025, read 2026-10-13). Order of preference:

1. Re-cut the nouns: if A and B need each other, there is usually a third concept; move the noun to the kernel (if it meets the admission rule) or to a new small package.
2. Narrow consumer interface in the package that needs the capability (DoltHub's "good" option: introduce a small interface and push it down). Example: `manifests.RepoLister` (satisfied by `*catalog.Catalog` at wiring), `badges.Facts` (satisfied by `commits`, `integrations`, `manifests`), `ops.Check`.
3. Events for "something happened": `webhook` produces `scm.Event`; the composition root builds a synchronous fan-out `Sink` from each feature's `HandleEvent(ctx, scm.Event) error` (which, for instance, decides whether a push touches manifest paths and calls `Enqueue`). No global bus, no `any` payloads, bounded and synchronous so errors and backpressure stay visible. Event types live in the kernel.
4. Read aggregation: `overview` is the only package allowed to know several features, and it is a leaf above them (nothing imports it except `cmd/scp`).
5. Function-typed dependencies for single-method needs (`type RepoLister func(ctx) []scm.RepoRef`) with an adapter method, as `http.HandlerFunc` does.
6. Never: a package-level function variable assigned from `main`/`init` (DoltHub's "Ugly" option; Google's registration critique), an `internal/common`, or merging features into one package.

### 5.11 Read model, state and the store (amendment to D-02)

D-02 says serving state is one immutable `Snapshot` built by the sync engine and owned by `store`. In a package-by-feature layout that forces `store` (or `refresh`) to import every feature's types, which inverts the dependency arrow and creates a merge hotspot. Proposal [DESIGN, see Deviations]:

- Each feature owns an immutable `state` value behind its own `atomic.Pointer[state]`, rebuilt by the feature's `Refresh` and swapped atomically. Reads are lock-free and consistent within the feature.
- `store` is feature-agnostic: `store.Record{Kind, Key, ETag, Meta, Payload []byte}` (the row shape the store design uses, see `05-store-sync-engine.md`), `store.Store` as the implementers' contract, `store/sqlite` and `store/memory` implementations, `store/storetest` conformance suite. Each feature serialises its own payloads and versions them.
- Cross-feature consistency is not guaranteed and is not needed: every item carries its own freshness (principle 7). If a consistent composed view is ever required, `overview` can build and cache one from feature states, because it already may import them; no other package changes.
- Warm start: each feature loads its records at construction time through its `Persister`; `refresh` readiness gates on "required jobs completed at least once or warm-started" (D-03).

---

## 6. Layout proposals for this product

Common to all three: single module, `cmd/scp`, everything else under `internal/`, `web/` outside Go, `api/openapi.yaml`, D-17 sources. They differ in how domain, use cases and adapters are cut.

### Layout A: vertical features, tiny kernel, ports at the consumer (recommended)

```
cmd/scp/                       main.go run.go wire_*.go config.go  (package main, composition root, e2e tests)
internal/
  scm/                         KERNEL value types (no deps)
  freshness/                   KERNEL status/age vocabulary (no deps)
  platform/{secret,logx,config,telemetry,httpx}
  httpapi/                     problem details, pagination, ETag, principal/authorizer, contract-test helpers
  store/                       Record + Store contract
    sqlite/ memory/ storetest/
  github/                      adapter (auth, client, ETag, rate limit, breaker, webhook decode/verify)
    githubtest/                fake GitHub server (test-only package)
  gitsource/                   optional git acquisition mode (D-17), same rules as github
  catalog/ docs/ commits/ badges/ manifests/ pages/ integrations/    FEATURES
    docs/internal/render/      private pipeline (goldmark+bluemonday+chroma -> SafeHTML)
    manifests/{maven,npm,...}  ecosystem parsers behind a registry (or inside, decided in 07-manifest-parsing)
    integrations/{jenkins,snyk}/
  refresh/                     generic job engine (was `sync`)
  webhook/                     receiver: size limit, dedupe, event fan-out
  access/                      optional trusted-proxy identity
  ops/                         livez/readyz/status/metrics/pprof
  webapp/                      embedded SPA host
  overview/                    cross-feature read compositions: dashboard, group, repo overview, llms.txt, sitemap
web/   api/
```

### Layout B: hexagonal inside every feature

```
internal/commits/{domain,app,ports,adapters/{http,memory}}/   x 7 features
internal/shared/{...}                                         shared kernel
internal/platform/...  internal/github/... (adapter implements ports of each feature)
```

Pros: textbook separation; each feature's domain package is trivially pure. Cons: for one-implementation ports (Q-12) it adds 3 to 4 directories per feature (about 25 extra packages), cross-feature rules must be written per layer as well as per feature, adapters that serve several features import several `ports` packages (the arrow from `github` to every feature), the GitHub adapter becomes aware of every feature, and navigation cost is high for humans and agents. Wild-workouts shows the shape and its ceremony (section 4). Verdict: reserve for a feature that grows several adapters and a rich domain (manifests may qualify later); do not start there.

### Layout C: Ben Johnson root domain package

```
internal/scp/            ROOT: all domain types and all service interfaces (Catalog, Commits, Docs, ...)
internal/github/ sqlite/ http/ mock/     adapters implementing root interfaces
internal/service/{commits,docs,...}      implementations of root interfaces (or inside adapters)
cmd/scp/
```

Pros: cycles impossible by construction (everything imports the root), the simplest rule set, proven in small apps ("works up to roughly 10K SLOC" as a monolith; the article stops there). Cons: the root package becomes the models bucket the owner rejected (principle 1), interfaces are producer-defined (contradicts principle 2), every feature change edits the root, and ownership of a type is invisible. Verdict: its good ideas are already in A (domain vocabulary at the bottom, adapters grouped by dependency, `main` wires), without the root bucket.

### Comparison

| Criterion | A | B | C |
|---|---|---|---|
| Conforms to 04 principles 1-4 | Yes | Yes (heavier) | Violates 1 and 2 |
| Go packages (approx.) | 28 | 50+ | 20 |
| Cycle risk | Low (kernel + consumer interfaces + aggregator) | Low inside, medium across features | None, by centralisation |
| Boundary rules to maintain | One table (section 7), about 20 rows | Table plus intra-feature layer rules | Very few |
| Adapter coupling | Adapter knows only the kernel | Adapter knows every feature's ports | Adapter knows the root |
| Test seams | Consumer fakes in `_test.go`, `githubtest`, `storetest` | Same plus per-layer fakes | Shared `mock` package |
| Navigability for humans and agents | Feature = one folder; kernel small | Deep trees | Root grows large |
| Extract a feature to its own service later | Feasible (feature + its consumer interfaces) | Feasible | Hard (root coupling) |
| Fit for 1 implementation per port (Q-12) | Good | Poor (ceremony) | Good |

Recommendation: layout A.

---

## 7. Layout A: dependency rules (who may import whom)

"Internal" below means packages of this module. Standard library imports are unrestricted except as listed in the global rules. "Feature" means one of `catalog docs commits badges manifests pages integrations`.

| Package | May import (internal) | Notable third-party | Must not import | Notes |
|---|---|---|---|---|
| `scm` | none | none | everything | Value types and key codec only |
| `freshness` | none | none | everything | |
| `platform/secret` | none | none | everything | `Secret` type, redaction |
| `platform/logx` | `platform/secret` | none | features, adapters | slog handlers, context attributes |
| `platform/config` | `platform/secret` | YAML, JSON Schema | features, adapters | Machinery only; root struct is in `cmd/scp` |
| `platform/telemetry` | none | OpenTelemetry SDK, Prometheus exporter | features, adapters | The only package allowed to import `go.opentelemetry.io/otel/sdk/...` |
| `platform/httpx` | `platform/logx` | none | features, adapters | `NewServer`, request id, recover, body limit, access log |
| `httpapi` | `scm`, `freshness`, `platform/httpx` | OpenAPI runtime (tests: validator) | features, adapters, `store` | Problem details, pagination, ETag, `Principal`/`Authorizer` |
| `store` | `freshness` | none | features | `Record`, `Store` contract |
| `store/sqlite` | `store`, `freshness` | modernc sqlite (only here) | features | Only `cmd/scp` may import it |
| `store/memory` | `store`, `freshness` | none | features | Used by unit tests and bubble tests |
| `store/storetest` | `store`, `freshness` | none | features | Imported from `_test.go` only |
| `github` | `scm`, `freshness`, `platform/secret` | breaker, JWT/App auth, otelhttp | all features, `store`, `refresh`, `webhook`, `httpapi` | Returns kernel types; owns typed errors and webhook verify/decode |
| `github/githubtest` | none (or `scm`) | none | everything else | Fake server; imported from tests and e2e |
| `gitsource` | same as `github` | go-git or git binary wrapper | same as `github` | Optional mode |
| Feature (each) | `scm`, `freshness`, `store`, `httpapi`, `platform/secret`, `platform/logx` | otel API only (`otel/metric`, `otel/trace`), domain libraries owned by that feature | any other feature, `github`, `gitsource`, `refresh`, `webhook`, `overview`, `ops`, `access`, `webapp`, `platform/{config,telemetry,httpx}`, `store/{sqlite,memory}` | Own `Config`, `Deps`, state, handlers |
| Feature subpackage (`integrations/jenkins`, `integrations/snyk`, `docs/internal/render`) | its own feature root (adapters only), `scm`, `freshness`, `platform/secret` | provider SDK or parser libs | other features | Feature root must not import its adapter subpackages (wiring does) |
| `refresh` | `freshness`, `platform/logx` | `x/sync`, `x/time/rate` | `scm`, all features, all adapters, `webhook` | Keys are strings; errors classified by behaviour interfaces |
| `webhook` | `scm`, `httpapi`, `platform/logx` | none | `github`, `refresh`, all features | Consumes `Verifier` and `Sink` interfaces; does size limit, dedupe, 202 |
| `access` | `scm`, `httpapi`, `platform/logx` | none | features | Implements `httpapi.Authorizer` |
| `ops` | `freshness`, `httpapi`, `platform/logx` | Prometheus handler | features, adapters | Consumes a `Check` interface; mounted on the admin listener |
| `webapp` | none | none | everything | `embed`, SPA fallback, CSP injection |
| `overview` | `scm`, `freshness`, `httpapi`, `platform/logx`, and the roots of `catalog docs commits badges manifests pages integrations` | none | adapters, `store`, `refresh`, `webhook`, `ops`, `access`, `webapp`, `platform/{config,telemetry,httpx}` | Calls features through its own narrow interfaces; imports their packages for type names only |
| `cmd/scp` | everything | everything | n/a | Only place that may import adapters together with features, `store/sqlite`, `os.Getenv`, `os.Exit`, `signal.NotifyContext`, `slog.SetDefault`, and create `http.Server`s |

Test files: a `_test.go` may import what its package may import, plus its own package under test (black-box `x_test`), plus test-only helper packages (`githubtest`, `storetest`) and test libraries. Feature tests must not import adapters or other features (use hand-written fakes named after behaviour, per Google best-practices on test doubles); only `cmd/scp` and the adapters' own tests may use `githubtest` against the real client. Nobody imports another package's `_test` helpers except through a `*test` package.

Global rules (apply to all packages, enforced by depguard `deny`, `forbidigo`, `sloglint`, `gochecknoinits`):

- G1 No import cycles (compiler) and no package named `util`, `common`, `helper`, `model(s)`, `types`, `api`, `interfaces`, `base`.
- G2 No `github.com/pkg/errors`; `math/rand` only as `math/rand/v2`; no `log` and no `fmt.Print*` outside `cmd/scp`.
- G3 No `slog.Info`/`slog.Default`/`slog.SetDefault` outside `cmd/scp` (sloglint `no-global: all`, `context: all`).
- G4 No `http.DefaultClient`, `http.DefaultServeMux`, `http.Get/Post/Head/PostForm`, `http.ListenAndServe`, `http.Server{}` literals outside `platform/httpx`/`cmd/scp`.
- G5 No `init()` outside `store/sqlite` (driver) and generated code; no mutable package-level variables (sentinel errors and compiled regexps are logically constant).
- G6 Heavy libraries are owned: goldmark, bluemonday, chroma only in `docs/internal/render`; modernc sqlite only in `store/sqlite`; OpenTelemetry SDK only in `platform/telemetry`; provider SDKs only in their adapter packages.
- G7 Wire/DTO types are not imported across packages.
- G8 `refresh` and `scm` never learn about each other (the engine's keys are strings).

The dependency graph, top to bottom (an arrow means "may import"):

```
cmd/scp
  -> overview -> features -> {store, httpapi, platform/{secret,logx}} -> {scm, freshness}
  -> webhook, access, ops, webapp, refresh      (each -> httpapi/platform/kernel as in the table)
  -> adapters: github, gitsource, integrations/*, store/sqlite  (-> kernel, platform/secret only)
```

---

## 8. Enforcing the boundaries (verified tooling)

Defence in depth, cheapest first.

1. The compiler: `internal/` at the module root, and nested `internal/` for feature-private packages (verified [LOCAL], section 5.1). Go's own layout guidance recommends keeping server packages in `internal` "as much as possible" (go.dev/doc/modules/layout, 2026-10-13).
2. Closed-world import test (recommended primary gate). It lists every package with the internal packages it may import and fails if a package has no rule, which tools configured per rule cannot do. Prototype [LOCAL, go1.27.1], about 80 lines using `go list -json ./...`; essential parts:

```go
// internal/archtest/arch_test.go  (test-only package)
const mod = "github.com/cantara/source-code-portal/"

var rules = map[string][]string{          // package -> allowed internal import patterns (path.Match)
    "internal/scm":      {},
    "internal/commits":  {"internal/scm", "internal/freshness", "internal/store", "internal/httpapi",
                          "internal/platform/secret", "internal/platform/logx"},
    "internal/overview": {"internal/scm", "internal/freshness", "internal/httpapi", "internal/catalog", "internal/commits" /* ... */},
    "cmd/scp":           {"internal/*", "internal/*/*"},
    // every package must appear; an unlisted package fails the test
}

func TestImportBoundaries(t *testing.T) {
    registerInputs(t, "../..")                       // see pitfall below
    cmd := exec.Command("go", "list", "-json", "./...")
    cmd.Dir = "../.."
    // decode ImportPath, Imports, TestImports, XTestImports for each package;
    // skip stdlib/third-party; permit self and own subpackages; report every violation
}
```

   Verified behaviour [LOCAL]: with `internal/commits` importing `internal/github` the test fails with `internal/commits must not import internal/github (allowed: [...])`, and a package without a rule fails with `no boundary rule declared`.

   Pitfall found [LOCAL]: `go test` caches a passing result because the `go list` child process is invisible to the test cache, so a violation introduced later did not fail the cached run. Mitigation: read every `.go` file and `go.mod` inside the test (`registerInputs` walks the module and `os.ReadFile`s them). After adding it, my runs were no longer reported as `(cached)` and the same edit failed immediately; I did not determine the exact cache rule that changed, so treat this as an observed mitigation. The robust alternative is to run this one test with `-count=1` in CI. (Observed on go1.27.1 only.)

3. depguard inside golangci-lint v2.14.0, for editor and pre-commit feedback. Verified [LOCAL] with the real v2.14.0 binary downloaded from the release: `list-mode: lax` with a broad `deny` prefix and more specific `allow` entries gives an allow-list per package.

```yaml
version: "2"
linters:
  default: none
  enable: [depguard]            # plus the others in the list below
  settings:
    depguard:
      rules:
        commits:
          list-mode: lax        # allowed if not denied, or if the allow entry is more specific (longer) than the deny entry
          files: ["**/internal/commits/**"]
          deny:
            - pkg: github.com/cantara/source-code-portal/internal
              desc: "commits may import only scm, freshness, store, httpapi, platform/secret, platform/logx; define a consumer-side interface instead"
          allow:
            - $gostd
            - github.com/cantara/source-code-portal/internal/commits       # itself
            - github.com/cantara/source-code-portal/internal/scm$          # '$' = exact match
            - github.com/cantara/source-code-portal/internal/freshness$
            - github.com/cantara/source-code-portal/internal/store$
            - github.com/cantara/source-code-portal/internal/httpapi$
```

   Output with a violation: `import 'example.com/scp/internal/github' is not allowed from list 'commits': commits may import only ... (depguard)`; after the fix: `0 issues.` Limits: a package with no rule is unrestricted (hence the closed-world test), and rules are written per package, so generate them from the same table as the Go test (a small `go generate` step) to avoid drift. [DESIGN]

   Other linters worth enabling (all present in the v2.14.0 reference configuration read from the golangci-lint repo on 2026-10-13): `gochecknoglobals`, `gochecknoinits`, `forbidigo` (with `analyze-types: true` for `^http\.(Get|DefaultClient|DefaultServeMux|ListenAndServe)$`), `sloglint` (`no-global: all`, `context: all`), `noctx`, `contextcheck`, `containedctx`, `bodyclose`, `errorlint`, `errname`, `exhaustive`, `usetesting`, `thelper`, `modernize`, `revive` (package comments, import shadowing), `staticcheck`, `gosec`, `spancheck`. Configure `gochecknoglobals` for sentinel errors before turning it on [UNVERIFIED how it treats `regexp.MustCompile` and `errors.New` variables]. golangci-lint is GPL-3.0 and depguard GPL-3.0: acceptable as external build tools; they are not linked into the product. Install by binary (documented recommendation), pin the version in CI.

4. go-arch-lint v1.19.0 (optional). Verified [LOCAL]: `version: 3`, `workdir: internal`, components, `commonComponents: [scm, freshness]`, `deps: { commits: { mayDependOn: [httpapi] } }`. Gotcha: a `deps` entry with `{}` is rejected ("should have ref in mayDependOn/canUse or at least one flag"); leaf components are simply omitted. It reports `Component commits shouldn't depend on example.com/scp/internal/github in .../commits.go:5` and also flags files not attached to any component (a closed-world check of its own). It adds a dependency-graph command and vendor rules (`depOnAnyVendor`).

5. arch-go v2.1.2 (optional). Verified [LOCAL]: `arch-go.yml` rule `package: "**.internal.commits"`, `shouldOnlyDependsOn: { internal: [...] }` fails with `ShouldOnlyDependsOn.Internal rule doesn't contains imported package '.../internal/github'` and a compliance table; it additionally supports contents, function-size and naming rules, and can run from `go test` through its `api` package. Use it only if the owner wants rules beyond imports (for example "kernel exports no interfaces").

Recommendation: item 2 is mandatory CI gate; item 3 is for fast feedback and for non-import rules (G2-G6); items 4 and 5 are optional and I would not run more than one config-driven checker in addition to the Go test. I found no authoritative head-to-head comparison of these tools (WebSearch only returned directory pages and blog posts); the comparison above rests on the local runs.

Rejected: multi-module (`go.work`) per feature as a hard boundary. It gives compiler enforcement but forces versioning and replace directives for a single-binary product, contradicts the layout guidance ("split off to separate modules" only when sharing), and `internal/` already provides most of the benefit.

---

## 9. Runtime architecture: lifecycle, shutdown and workers

### 9.1 Facts from Kubernetes and net/http

- On pod deletion the kubelet runs the `preStop` hook, then sends SIGTERM; endpoint removal happens in parallel and is not a prerequisite; terminating endpoints always have `ready=false`, so load balancers stop using them for regular traffic, but propagation is not instantaneous; the default `terminationGracePeriodSeconds` is 30 s and the countdown starts before `preStop`; after it expires SIGKILL is sent (kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/, 2026-10-13).
- A `sleep` handler exists for lifecycle hooks (kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/, 2026-10-13); its feature-state line was not visible in the fetched text, so "stable since version N" is [UNVERIFIED]. In-process drain delay (below) works on every cluster version and does not depend on it.
- `Server.Shutdown` closes listeners, closes idle connections, then waits indefinitely for active connections to become idle until the context expires; it "does not attempt to close nor wait for hijacked connections such as WebSockets"; `ListenAndServe` returns `ErrServerClosed` immediately and the program must wait for `Shutdown` to return; `RegisterOnShutdown` functions "should start protocol-specific graceful shutdown, but should not wait" (pkg.go.dev/net/http, 2026-10-13). A long-lived non-hijacked response such as Server-Sent Events is an active connection forever, so `Shutdown` would run to its deadline unless the handler observes a shutdown signal (section 9.4).
- Practitioner guidance matches: fail readiness first, wait for propagation, budget about 80 % of the grace period, check the error from `Shutdown`, avoid ignoring context in handlers, call `stop()` after the first signal so a second Ctrl+C kills (victoriametrics.com/blog/go-graceful-shutdown/, Phuong Le, 2 May 2025, read 2026-10-13).

### 9.2 Shutdown order (binding for the architecture)

1. SIGTERM/SIGINT cancels the root context (`signal.NotifyContext`); call `stop()` immediately so a second signal kills.
2. Mark not ready: `/readyz` returns 503 (the liveness probe `/livez` stays 200, it must not look at dependencies). Keep serving normally.
3. Drain delay (default 5 s, configurable) so endpoint removal propagates.
4. `Shutdown` the public server, then the admin server (admin last so `/metrics` and `/status` stay available during the drain), with one shared deadline. On deadline expiry call `Close()` as a last resort and log it.
5. Cancel workers (sync engine and anything else with `Run`), wait for them with the remaining budget; in-flight jobs may be interrupted because state is a rebuildable cache.
6. Close the store (flush, WAL checkpoint), then telemetry providers (flush last so shutdown itself is observable).
7. Return the joined error from `run()`; `main` exits non-zero if any.

Budget rule: `drain + shutdown timeout + worker stop + store/telemetry flush < terminationGracePeriodSeconds` (default 30 s; SIGKILL cannot be caught). Defaults for the product: drain 5 s, server shutdown 15 s, worker stop 5 s, flush 2 s, document `terminationGracePeriodSeconds: 45` in the manifests. [DESIGN]

### 9.3 Reference implementation (verified)

Prototype `internal/lifecycle` [LOCAL, passes `go test -race -count=1` on go1.27.1]:

```go
type Server interface { ListenAndServe() error; Shutdown(ctx context.Context) error } // *http.Server satisfies it

type Plan struct {
    Workers         []func(ctx context.Context) error // blocking Run funcs
    Servers         []Server                          // public first, admin last
    MarkNotReady    func()
    Drain           time.Duration
    ShutdownTimeout time.Duration
}

func Run(ctx context.Context, p Plan) error {
    serving, sctx := errgroup.WithContext(ctx)
    wctx, stopWorkers := context.WithCancelCause(context.WithoutCancel(ctx)) // workers outlive the signal
    defer stopWorkers(nil)
    workers, _ := errgroup.WithContext(wctx)
    failed := make(chan error, len(p.Workers))
    for _, w := range p.Workers {
        workers.Go(func() error {
            err := w(wctx)
            if err != nil { failed <- err } // a dying worker brings the process down in order
            return err
        })
    }
    serving.Go(func() error {
        select { case err := <-failed: return err; case <-sctx.Done(): return nil }
    })
    for _, s := range p.Servers {
        serving.Go(func() error {
            if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { return err }
            return nil
        })
    }
    <-sctx.Done()                 // signal, a server failure, or a worker failure
    p.MarkNotReady()
    time.Sleep(p.Drain)           // still serving
    shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.ShutdownTimeout)
    defer cancel()
    var errs []error
    for _, s := range p.Servers { errs = append(errs, s.Shutdown(shutCtx)) }
    errs = append(errs, serving.Wait())
    stopWorkers(errors.New("shutting down"))
    errs = append(errs, workers.Wait())
    return errors.Join(errs...)
}
```

Tests (both pass [LOCAL]): under `synctest.Test`, after a fake minute of running, cancelling the context yields exactly `ready=false @60s`, `public.shutdown @65s`, `admin.shutdown @65s`, `worker.stopped @67s`; and a worker that fails after 10 fake seconds produces `ready=false @10s` and returns the worker's error through `errors.Is`. Gaps to close in production code (not prototyped): fall back to `Server.Close()` when `Shutdown` returns the deadline error, make the drain sleep context-aware for a second signal, and store/telemetry closers in reverse construction order via `defer` in `run()`.

### 9.4 SSE and long-lived handlers

If D-16's live updates use SSE: raise or clear the write deadline per handler with `http.NewResponseController(w).SetWriteDeadline(...)` (the controller exposes `Flush`, deadline setters and returns an `ErrNotSupported`-matching error when unsupported; pkg.go.dev/net/http, 2026-10-13), do not wrap those handlers in `http.TimeoutHandler` (it supports neither `Flusher` nor `Hijacker`; same source), and have the handler select on `r.Context().Done()` and a `shuttingDown` channel closed from `srv.RegisterOnShutdown`, otherwise step 4 above runs to its deadline. Polling plus ETag (the other D-16 candidate) has none of these issues; that is an argument for it that the frontend research may weigh.

---

## 10. HTTP service hardening

Defaults are [DESIGN] values chosen to be safe for a portal behind a proxy; the semantics come from the cited documentation.

| Control | Setting | Evidence and notes |
|---|---|---|
| Construction | `httpx.NewServer(cfg)` is the only way to create a `*http.Server` (G4); it sets every field below | Verified shape [LOCAL] |
| `ReadHeaderTimeout` | 5 s | Docs: after headers are read the read deadline is reset, handlers decide body deadlines; "most users should prefer ReadHeaderTimeout" over `ReadTimeout` (pkg.go.dev/net/http, 2026-10-13) |
| `ReadTimeout` | 30 s | Bounds the whole request including body; webhooks are small and fast. Raise per handler with `ResponseController.SetReadDeadline` if needed |
| `WriteTimeout` | 60 s (SSE handlers override) | Resets per request header read (same docs) |
| `IdleTimeout` | 120 s | Zero falls back to `ReadTimeout` (same docs), so set it explicitly |
| `MaxHeaderBytes` | 16 KiB | Limits request line and headers only, not the body (same docs). Go 1.27 adds `Server.MaxHeaderValueCount` (go.dev/doc/go1.27); set a modest value |
| Body size | `http.MaxBytesHandler(next, n)` globally (1 MiB for `/api`), `http.MaxBytesReader` inside the webhook handler | Reads past the limit return `*http.MaxBytesError`; verified [LOCAL]: 10-byte body against an 8-byte limit yields 413. GitHub caps webhook payloads at 25 MB (docs.github.com/en/webhooks/webhook-events-and-payloads, 2026-10-13); set the webhook limit to 26 MiB, cap concurrent webhook bodies (semaphore of 4) because HMAC needs the whole raw body in memory |
| Webhook response time | verify, dedupe, enqueue, 202 | GitHub requires 2XX within 10 s; recommends async processing (best-practices page, 2026-10-13) |
| Webhook dedupe | `X-GitHub-Delivery` | It is a GUID per delivery (webhook-events page, 2026-10-13), but a manual redelivery reuses the original value (best-practices page), so dedupe must not drop redeliveries that are the recovery path; refresh jobs are idempotent keyed jobs, so duplicates are harmless. The signature header is `X-Hub-Signature-256` (HMAC hex of the raw body with SHA-256) |
| Panic recovery | middleware recovers, logs stack with request id, returns 500 `application/problem+json`; re-panics `http.ErrAbortHandler` | `ErrAbortHandler` panics suppress the server's stack log by contract (pkg.go.dev/net/http). Verified [LOCAL]: panic yields 500 with `application/problem+json` and `X-Request-Id`. Goroutines started by handlers are not covered; they must not exist (use `refresh`) |
| Request id | accept inbound `X-Request-Id` only if it matches `^[A-Za-z0-9._-]{8,64}$`, else mint with `crypto/rand.Text()` (>= 128 bits); echo in the response; put in `slog` via a context handler; use the trace id when tracing is on | `rand.Text` added in Go 1.24 (pkg.go.dev/crypto/rand#Text, 2026-10-13); sanitising prevents log injection. [DESIGN] |
| Per-handler deadline | `context.WithTimeout` inside handlers; avoid blanket `http.TimeoutHandler` | `TimeoutHandler` returns 503 and lacks `Flusher`/`Hijacker` (pkg.go.dev/net/http) |
| CSRF | `http.CrossOriginProtection` only if cookie-authenticated unsafe methods exist (admin UI); the SPA uses same-origin `fetch` with no cookies for public data | It rejects non-safe cross-origin browser requests using `Sec-Fetch-Site`/`Origin`; requests without those headers are assumed same-origin or non-browser, so webhooks need no bypass (pkg.go.dev/net/http#CrossOriginProtection, go.dev/doc/go1.25, 2026-10-13) |
| Errors | RFC 9457 problems; 5xx generic; request id as `instance` or extension | rfc-editor.org/rfc/rfc9457.html, 2026-10-13 |
| Listeners | public and admin on separate `http.Server`s; `/debug/pprof/` and `/metrics` only on admin; `/debug/pprof/goroutineleak` is available in Go 1.27 | go.dev/doc/go1.27, 2026-10-13 |
| Outbound clients | One `http.Client` per upstream with transport timeouts, never `http.DefaultClient`; always close bodies (`bodyclose`); Go 1.27 drains unread bodies on close up to a limit | go.dev/doc/go1.27, 2026-10-13 |
| Server error log | `ErrorLog: slog.NewLogLogger(handler, slog.LevelError)` | Verified compiles [LOCAL] |
| `BaseContext` | the process root with `WithoutCancel` so SIGTERM does not abort in-flight requests; cancel it only on forced close | victoriametrics.com article warns about cancelling the base context too early (2026-10-13) |
| Static serving | `webapp` serves `embed.FS` with `Cache-Control` per class; `os.Root` for any runtime directory of pages (traversal-safe) | `os.Root` Go 1.24 (go.dev/doc/go1.24, 2026-10-13); `pages` loading Markdown from a directory should use it |

Middleware order, outermost first: request id -> recover -> access log (with status, bytes, duration, request id) -> body limit -> security headers -> mux. [DESIGN]

---

## 11. Testing architecture

| Layer | What | Tooling | Where |
|---|---|---|---|
| Unit | Pure logic: matchers, front matter, parsers, badge rendering, state building | table tests, `go-cmp` | each package, `_test.go` |
| Feature | A feature with hand-written fakes of its consumer interfaces | fakes named by behaviour (Google best-practices), `memory` store | each feature |
| Handler | `api.go` against `httptest.NewRecorder` or in-memory server; contract against OpenAPI (pattern set equality, schema validation of responses) | `ServeMux.Handler` pattern check [LOCAL]; response validation lib decided by the API-contract task | each feature + `httpapi` helpers |
| Adapter | `github` against `githubtest` fake upstream; `store/sqlite` and `store/memory` against `storetest` | fake server with fault injection; conformance suite | `github`, `store/*` |
| Time-driven | scheduler jitter, backoff, breaker windows, retention, drain, token refresh | `testing/synctest` | `refresh`, `github`, `lifecycle`, `httpx` |
| Property and robustness | matcher, config, renderer/sanitiser, manifest parsers, webhook signature, front matter | native fuzzing | the owning package |
| Architecture | import rules, route set vs OpenAPI | `archtest` (section 8) | `internal/archtest`, `cmd/scp` |
| End to end | boot `run()` against `githubtest`, wait for `/readyz`, exercise `/api`, send SIGTERM-equivalent cancel, assert clean exit | `run()` pattern, `:0` listener | `cmd/scp` |
| Smoke | the built binary in the container image (D-10) | shell + `curl`, optional coverage via `go build -cover` | CI |

Details and verified facts:

- Fake upstream. `githubtest.Server` is an `http.Handler` over a small in-memory GitHub model (orgs, repos, commits, READMEs, trees, releases) that supports ETag/`If-None-Match`, `Link` pagination, rate-limit headers, 403/429 secondary limits, 5xx and slow responses; recorded fixtures (re-recorded per Q-21) feed the model from `testdata/`. Use it two ways: with `httptest.NewServer` for adapter and e2e tests; with `httptest.NewTestServer` (Go 1.27) inside a synctest bubble for time scenarios. Verified [LOCAL]: inside `synctest.Test`, a handler that waits 30 fake seconds and a client context of 10 fake seconds returns `context.DeadlineExceeded` with `time.Since(start) == 10s` and about 0 ms of wall time. Gotchas: `Server.URL` is empty on the in-memory network (documented: `Server.Listener` is not set), so use any `https://upstream.test/` URL; the client sends every request to the test server regardless of host. Network I/O over real sockets is not durably blocking and prevents a bubble from becoming idle (pkg.go.dev/testing/synctest, 2026-10-13), so do not mix real listeners into bubbles.
- `synctest` rules from the docs (2026-10-13): fake clock starts at 2000-01-01 UTC and advances only when every goroutine in the bubble is durably blocked; channels, timers and tickers must be created inside the bubble; mutexes, I/O and syscalls are not durably blocking; `Test` waits for all bubble goroutines to exit, so leaked goroutines fail the test (a built-in leak check); `T.Run`, `T.Parallel`, `T.Deadline` must not be called inside a bubble; `T.Cleanup` runs inside the bubble; a package-level `sync.WaitGroup` variable cannot join a bubble. Consequence: bubble tests use `store/memory`, never SQLite files. Because the bubble also virtualises `time.Now`, an injected clock abstraction is unnecessary for bubble tests (hence dropping `platform/clock`; keep a `func() time.Time` field only where a test outside a bubble truly needs it).
- Golden files. A 20-line helper in a `*test` package: `golden.Check(t, name, got)` compares with `testdata/<name>.golden` using `cmp.Diff`, rewrites when `-update` is set (flag defined in the test package). Rationale: Google best-practices call `cmp` "designed to compare (and to diff) values" and say assertion helpers are not idiomatic; third-party golden libraries (goldie v2.8.0, repo push 2025-11-22; gotest.tools v3.5.2) are optional. Use goldens for: sanitised HTML from `docs`, badge SVGs, `config lint` reports, `llms.txt`, problem+json bodies, OpenAPI examples. Normalise volatile fields; review golden diffs in PRs.
- Fuzzing (go.dev/doc/security/fuzz, 2026-10-13): `FuzzXxx(f *testing.F)`, seeds from `f.Add` and `testdata/fuzz/<Name>/` run on every plain `go test`; `-fuzz` matches exactly one fuzz test per invocation; a failing input is minimised and written to `testdata/fuzz/<Name>/<hash>` and becomes a regression test; targets must be fast (a 1 s execution timeout), deterministic and stateless; supported argument types are primitives, `string`, `[]byte`; instrumentation is available on amd64 and arm64 only. Policy: seeds always run in CI; a scheduled job loops over `go test -list '^Fuzz'` and runs each for 30-60 s with `-fuzztime`. Targets: catalog matchers (glob, anchored RE2), config decoder and migrator, markdown render+sanitise (property: output has no `<script`, no `javascript:` URL, no event-handler attributes), front-matter parser, each manifest parser (never panics, bounded work), webhook signature verification and body limit, `scm.ParseRepoRef`.
- goleak. `goleak.VerifyTestMain(m)` in packages that start goroutines outside bubbles (`github`, `httpx`, `webhook`, `cmd/scp`); README warns it cannot tell leaks from unfinished parallel tests when `t.Parallel` is used, so use `VerifyTestMain` there (github.com/uber-go/goleak README, 2026-10-13). Verified [LOCAL]: v1.3.0 passes a clean package and reports an injected `select {}` goroutine on go1.27.1. Maintenance note: no tagged release since 2023-10-24, though commits continue (2026-09-15) and it states support for the two most recent Go minors; low risk (tiny, test-only) but track it. In production, the Go 1.27 `goroutineleak` profile at `/debug/pprof/goroutineleak` complements it (go.dev/doc/go1.27, 2026-10-13).
- Race detector. Requires cgo, and on non-Darwin a C compiler; typical cost 2-20x CPU and 5-10x memory (go.dev/doc/articles/race_detector, 2026-10-13). Therefore the CI test job runs `CGO_ENABLED=1 go test -race -shuffle=on ./...`, and only the release build uses `CGO_ENABLED=0 -trimpath` (resolves the D-10/D-11 tension). Verified [LOCAL]: the lifecycle and httpx tests pass under `-race` with a cgo-enabled toolchain (SQLite with a pure-Go driver under `-race` was not tested).
- End to end via `run()` (Mat Ryer). Pass `getenv` as a function, listeners on `:0`, and the fake GitHub URL through config; cancel the context to drive shutdown and assert `run` returns within the budget. For binary smoke tests with coverage: `go build -cover`, `GOCOVERDIR`, `go tool covdata`; coverage data is written only on normal exit from `main`/`os.Exit`, not on unrecovered panic or fatal signal (go.dev/doc/build-cover, 2026-10-13), so the SIGTERM path must return normally.
- Store contract suite. `storetest.Run(t, newStore func(t *testing.T) store.Store)` run against `memory` and `sqlite`, copying Dex's `conformance` package (`runTests(t, newStorage, tests)` per sub-test; github.com/dexidp/dex/storage/conformance, 2026-10-13).
- Other: `testing.T.Context`, `B.Loop`, `T.ArtifactDir` (Go 1.26) for test outputs; `go vet` includes `stdversion` in `go test`.

---

## 12. Pitfall checklist (for design reviews)

1. Package names: avoid `util/common/helper/model/types/api/interfaces`; avoid shadowing stdlib (`sync`, `context`, `errors`, `log`, `http`); no stutter (`commits.Commit` is fine, `commits.CommitsService` is not).
2. Cycles: if you reach for a shared package to break one, first ask whether the noun belongs in the kernel; then try a consumer interface; events last.
3. Consumer interfaces mentioning consumer types cannot be implemented by adapters; use kernel types.
4. A producer-side interface over 5 methods is a smell unless it is a plugin contract with a conformance suite.
5. Never store `ctx` in structs; never pass `r.Context()` to async work.
6. Constructors start goroutines: forbidden; `Run(ctx)` only.
7. Global default clients, muxes, loggers, tracer providers, registries: forbidden outside `cmd/scp`.
8. `init()` and package-level mutable state: forbidden; third-party driver registration is isolated.
9. Test cache hides architecture violations unless the test reads its inputs.
10. A fake that is not exercised against the real adapter's contract drifts; run the same fixtures through `githubtest` with the real client.
11. `httptest.NewTestServer` URL is empty; synctest bubbles cannot contain real sockets or SQLite files.
12. `-race` needs cgo; do not set `CGO_ENABLED=0` globally in CI.
13. SSE and `Shutdown`: long-lived handlers must observe shutdown.
14. Webhook handler doing real work inline will miss GitHub's 10 s window.
15. Generated and DTO types leaking into other packages recreate the shared models bucket.
16. Kernel growth: enforce "imports nothing" and the admission rule.
17. Aggregator growth: `overview` may import features but must not hold business logic; if a rule is needed by two views, it belongs in the feature.

---

## 13. Deviations proposed against 04 (for the architecture document to adopt or reject)

Each is a proposal with evidence; 04 remains binding until accepted.

| # | Proposal | Evidence / reason |
|---|---|---|
| Dv-1 | Add `internal/scm` and `internal/freshness` as dependency-free kernel packages | D-17 already requires an `scm` model so that no GitHub-specific type leaks; principle 7 requires freshness on every item; Ben Johnson's root-domain principle (gobeyond.dev, 2026-10-13) without his producer-side interfaces; needed so adapters can satisfy consumer interfaces (section 5.3) |
| Dv-2 | Rename `internal/sync` to `internal/refresh` (package name `refresh`) | `sync` shadows the standard library package; Google best-practices on shadowing (2026-10-13); every file that needs both would need an import alias |
| Dv-3 | Add `internal/overview` for cross-feature read compositions (dashboard, group page, repo overview, `llms.txt`, `llms-full.txt`, sitemap) | 04 section 5 has no owner for the dashboard, group-page and app-shell endpoints that combine catalog groups, commits, badges and status; `docs` is listed as owning "LLM outputs" but `llms.txt` indexes groups and repos too; without an aggregator, features would import each other |
| Dv-4 | D-02: per-feature immutable state behind its own `atomic.Pointer`, plus a feature-agnostic `store.Record`/`store.Store` (sqlite, memory, storetest); drop the single global `Snapshot` type | A global `Snapshot` owned by `store` or `refresh` must import all feature types (inverted arrow, merge hotspot); cross-feature atomicity is not needed because each item has its own freshness; `overview` can compose consistent views if ever required |
| Dv-5 | Drop `platform/clock` | `testing/synctest` virtualises `time.Now`, timers and sleeps (go.dev/doc/go1.25, pkg.go.dev/testing/synctest), and `httptest.NewTestServer` (Go 1.27) makes HTTP usable inside bubbles |
| Dv-6 | `webhook` holds receiving, size limit, dedupe and fan-out; signature verification and payload decoding live in `github` behind a `webhook.Verifier` interface | `X-Hub-Signature-256` and payload shapes are GitHub-specific (D-04 already places payload types in `github`); keeps `webhook` provider-neutral per D-17 |
| Dv-7 | `integrations` root is a feature that owns the `Provider` interface (consumer side); `jenkins`/`snyk` are subpackages | Matches "StatusProvider" with Q-05 and keeps consumers (`badges`, `overview`) on slices of `integrations`, not on provider SDKs |
| Dv-8 | Config: per-feature `Config` structs in the features; root struct, tags, Validate fan-out and schema generation in `cmd/scp/config.go`; `platform/config` is machinery only | Keeps features free of `platform/config`; makes "documented keys == consumed keys" testable in one place |
| Dv-9 | CI: test job with `CGO_ENABLED=1` and `-race`; release build `CGO_ENABLED=0` | go.dev/doc/articles/race_detector (2026-10-13) |
| Dv-10 | `go 1.27.0` in `go.mod`; CI on 1.27 only | Section 2; `NewTestServer`, `synctest.Sleep` are 1.27 APIs |
| Dv-11 | Composition root stays in `cmd/scp` (package main) with `run()`; promote to `internal/app` only on a second binary or when wiring exceeds about 600 lines | Mat Ryer pattern; tests in `package main` can call `run()` |

---

## 14. Evidence log (what was run)

Local prototype module (scratch, outside this design package), `go1.27.1 darwin/arm64`, `go 1.27.0` directive, cgo enabled:

- `archtest`: closed-world import rules; violation detected; test-cache blind spot reproduced and fixed.
- `golangci-lint v2.14.0` with a depguard `lax` rule: violation reported, then `0 issues`.
- `go-arch-lint v1.19.0` (`version: 3` config): violation reported; empty `deps` entries rejected.
- `arch-go v2.1.2`: `shouldOnlyDependsOn` violation reported with compliance table.
- Nested `internal/` visibility: allowed inside the tree, rejected outside.
- `lifecycle.Run` plus two synctest tests (shutdown order at exact fake times; worker failure triggers ordered shutdown), `-race` clean.
- `httpx` (request id, recover, body limit, `NewServer`) tested with `httptest.NewTestServer`: 500 problem+json with `X-Request-Id` on panic; 413 on oversize body.
- Fake upstream timeout inside a synctest bubble: exact 10 s fake elapsed.
- `httptest.NewTestServer` `URL` is empty; relative URLs fail with `unsupported protocol scheme ""`.
- `ServeMux.Handler` returns the matched pattern.
- `goleak v1.3.0`: clean pass; injected leak reported.
- `go list -m -versions golang.org/x/sync`: v0.24.0 is the latest.

One unexplained observation: an early run of the `httpx` test with `srv.URL + "/boom"` reported `ok` although the same URL form fails in isolation; I corrected the test to absolute URLs and re-ran it green, but did not determine why the first run passed. It does not affect any recommendation.

---

## 15. Sources (all read on 2026-10-13)

Go team and standards: go.dev/doc/modules/layout; go.dev/doc/devel/release; go.dev/doc/go1.24, go1.25, go1.26, go1.27; go.dev/wiki/CodeReviewComments; google.github.io/styleguide/go/decisions and /best-practices; go.dev/doc/effective_go; go.dev/blog/package-names; go.dev/blog/context-and-structs; go.dev/blog/synctest (Damien Neil, 19 Feb 2025, experimental-era post; API since changed to `synctest.Test`); go.dev/blog/routing-enhancements; go.dev/doc/security/fuzz; go.dev/doc/articles/race_detector; go.dev/doc/build-cover; go.dev/doc/modules/managing-dependencies; pkg.go.dev/net/http, /net/http/httptest#NewTestServer, /testing/synctest, /context, /errors, /crypto/rand#Text, /golang.org/x/sync/errgroup, /go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp.

Patterns: gobeyond.dev/standard-package-layout (Ben Johnson, 10 Aug 2016); alistair.cockburn.us/hexagonal-architecture (2005-09-04); grafana.com/blog/2024/02/09/how-i-write-http-services-in-go-after-13-years/ (Mat Ryer); dave.cheney.net/2014/10/17/functional-options-for-friendly-apis; dolthub.com/blog/2025-03-14-go-import-cycle-strategies; victoriametrics.com/blog/go-graceful-shutdown/ (2 May 2025); rfc-editor.org/rfc/rfc9457.html; kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/; kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/; docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks; docs.github.com/en/webhooks/webhook-events-and-payloads; golangci-lint.run/docs/welcome/install/local/.

Repositories via GitHub API: grafana/grafana, coder/coder, authelia/authelia, miniflux/v2, woodpecker-ci/woodpecker, dexidp/dex, usememos/memos, ThreeDotsLabs/wild-workouts-go-ddd-example, benbjohnson/wtf, go-gitea/gitea, ardanlabs/service, golang-standards/project-layout, golangci/golangci-lint (including `.golangci.reference.yml`), OpenPeeDeeP/depguard, fe3dback/go-arch-lint, arch-go/arch-go, uber-go/goleak, oapi-codegen/oapi-codegen, ogen-go/ogen, oklog/run, thejerf/suture, google/go-cmp, sebdah/goldie, gotestyourself/gotest.tools, rogpeppe/go-internal, open-telemetry/opentelemetry-go-contrib.

---

## 16. Corrections to the decision record and earlier assumptions

Items in `04-decisions.md` and in earlier toolchain assumptions that the web (or a local run) shows to be outdated, wrong or inconsistent.

1. Toolchain assumption "Current stable Go (1.25 or newer at the time of writing; verify)". Verified: current is Go 1.27 (go1.27.2, 2026-10-08); Go 1.25 is no longer supported since 1.27's release on 2026-08-19; supported are 1.27 and 1.26 (go.dev/doc/devel/release, 2026-10-13).
2. Container-aware `GOMAXPROCS` from Go 1.25 "(verify)". Verified in the Go 1.25 notes (cgroup CPU limit, runtime updates, `GODEBUG=containermaxprocs=0` to disable); `automaxprocs` is not needed (go.dev/doc/go1.25, 2026-10-13). `GOMEMLIMIT` still needs to be set by the deployer.
3. The `04` D-10 testing list does not mention `testing/synctest`, `httptest.NewTestServer` or `T.Context`; all three are now stable and change how time-driven tests (breakers, backoff, drain) should be written (sections 2 and 11).
4. `04` D-10 lists a "route-crawl test" while D-16 states it is replaced by a typed-route plus link e2e test; D-10 was not updated (internal inconsistency).
5. `04` D-10 (`-race`) versus D-11 (`CGO_ENABLED=0`): the race detector requires cgo (go.dev/doc/articles/race_detector, 2026-10-13); CI must run tests with cgo enabled (Dv-9).
6. `04` D-16 "OpenAPI 3.1 ... generated strict-server stubs; research decides": the common assumption that Go generators lag at 3.0 is outdated for oapi-codegen, which now supports 3.0 and 3.1, std `net/http` servers and strict servers (v2.8.0, 2026-07-17; README read 2026-10-13). ogen: unverified.
7. `04` section 5 package name `internal/sync` shadows stdlib `sync` (Dv-2).
8. `04` section 5 lists `internal/store` as owning "`Snapshot` read model"; with package-by-feature this creates an inverted dependency (Dv-4).
9. `04` section 5 has no owner for cross-feature views and `llms.txt` (Dv-3).
10. `04` D-13 and D-17 changed identity to `source/org/repo`; examples in this report use `scm.RepoRef{Source, Org, Name}`. D-12's route text still says "repo name within the configured organisation"; consistent only for the single-source case (not an error, but the architecture should state the routing rule once).
11. `04` principle 2 names "`go list`-based or `depguard`/`go-arch-lint`" as alternatives; they are not equivalent: only a closed-world test catches unlisted packages (section 8).
12. `errgroup` for lifecycle is correct, but add that `errgroup.WithContext` cancels on first error and when `Wait` returns, so staged shutdown (HTTP first, workers after) needs separate groups, as in section 9.3.
13. `go-vcr`, `sony/gobreaker` etc. are not re-verified here (other research tasks own them).

---

## 17. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Kernel (`scm`) becomes a models bucket | Recreates the layering 04 rejects; every change touches everything | Admission rule, "imports nothing" check, size review at 1,200 lines or 40 exported types |
| `overview` becomes a god package | Business logic leaks out of features; aggregator imports everything | Rule: no business rules in `overview`; no one imports it; review any logic that two views share and move it into a feature |
| Per-feature state loses cross-feature atomicity | A page could briefly show new commits with an old repo list | Acceptable by principle 7; compose in `overview` later if needed; document in the API (freshness per item) |
| Rule table drifts from code | Silent boundary erosion | Closed-world test mandatory in CI; generate depguard rules from the same table; failing on unlisted packages |
| Consumer interfaces proliferate | Many tiny interfaces, duplicated fakes | Keep them in `deps.go`, 1-3 methods; share fakes only inside a feature; accept some duplication (Go proverb) |
| `go test` caching hides architecture failures | False green | `registerInputs` or `-count=1` for the arch test (section 8) |
| Go 1.27-only APIs (`NewTestServer`, `synctest.Sleep`) | Cannot build on 1.26 | `go 1.27.0`, CI on 1.27; the vet `stdversion` check catches slips |
| `encoding/json` backed by v2 in Go 1.27 | Error text differs; edge-case behaviour options | Tests assert on decoded values, not error strings; Go says marshal/unmarshal behaviour is preserved; opt-out `GOEXPERIMENT=nojsonv2` is temporary |
| goleak has no release since 2023-10-24 | Test-only dependency may stall on a future Go | Tiny; `synctest` leak check and Go's `goroutineleak` profile are alternatives |
| go-arch-lint / arch-go single-maintainer tools | Config drift or abandonment | Keep them optional; the Go test has no tool dependency |
| depguard and golangci-lint are GPL-3.0 | Licence concern if redistributed | Used only as external CI tools; not linked into the product |
| SSE plus `Shutdown` | Shutdown stalls to deadline | Section 9.4 or choose polling plus ETag |
| Webhook memory pressure from 25 MB bodies under HMAC | Memory spikes | Semaphore on concurrent webhook bodies, early `Content-Length` rejection, reject unsigned requests before reading |
| `oapi-codegen` multi-spec split has known constraints | Slower contract-first adoption | Decide in the API-contract task; architecture only requires wire types to stay local |
| Over-splitting features into subpackages | Navigation cost, unintended imports | Section 5.1 criteria; add subpackages only for isolation, multiple implementations or privacy |
| Prototype results were produced on darwin/arm64 with go1.27.1 | Linux CI may differ in small ways (test cache, race) | Re-run the prototype tests in CI once the repo exists |

---

## 18. Unverified

- Jack Lindamood as the origin of "accept interfaces, return structs": only secondary sources found.
- The Kubernetes `sleep` preStop handler's stable-since version: feature-state line not present in the fetched text. Everything else in section 9.1 comes from the pod-lifecycle page.
- `ogen` OpenAPI 3.1 support: README says "OpenAPI v3" and shows 3.0.3 only.
- How `gochecknoglobals` treats sentinel errors and `regexp.MustCompile` globals; verify when configuring.
- Whether older Go toolchains (before 1.27) show the same test-cache behaviour for the `go list` architecture test.
- `http.MaxBytesHandler` and `http.Server.MaxHeaderBytes` default value: confirmed to compile and behave on go1.27.1 [LOCAL], but the "added in" version was not read from documentation.
- `DefaultMaxHeaderBytes` numeric default (docs say "zero means DefaultMaxHeaderBytes"; value not read).
- FOSDEM 2026 modular-monolith talk (search result) and the DZone article on package flow: not fetched successfully (404 and 403), not used as evidence.
- Whether GitHub's 25 MB payload cap and 10 s response window apply identically to GitHub Enterprise Server: only github.com docs were read.
- The proposed default timeouts, drain and budgets in sections 9 and 10 are design values, not measured; they should be validated against the real probe periods of the deployment.
- Whether GraphQL/REST adapters need a different `scm.Result` shape for conditional requests; the kernel type is sketched, not designed.
