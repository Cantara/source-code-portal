# 05-research / 02 - Go stack currency and library selection

Verification date for every live-web fact in this document: **2026-10-13** (the harness date; the shell clock read `Tue 13 Oct 2026 20:55 UTC`). Scope: Go toolchain currency and library selection for SCP. Binding input: `04-decisions.md` (D-01..D-16). Only this file was written.

## 0. Executive summary

1. **Toolchain: Go 1.27.2** (released 2026-10-08; 1.27.0 on 2026-08-19). Go 1.26.9 is the only other supported line; Go 1.25 is out of support (policy: a release is supported until two newer majors exist). Every library chosen below requires `go >= 1.26.0`, so `go 1.27.2` in `go.mod` is coherent. 1.26.9 and 1.27.2 both carry security fixes to `net/http` and `html/template`, so do not pin anything older.
2. **Go 1.27 changes the toolchain baseline**: `encoding/json/v2` and `jsontext` are GA (no GOEXPERIMENT) and `encoding/json` is now backed by v2; stdlib has a new `uuid` package (v4/v7); `testing/synctest` is GA since 1.25 and 1.27 adds `httptest.NewTestServer` for it; GOMAXPROCS is cgroup-aware since 1.25 (drop `automaxprocs`); `go.mod` `tool` directive since 1.24. `GOMEMLIMIT` is still not automatic.
3. **GitHub boundary**: `google/go-github/v92` (v92.0.0, 2026-09-14) behind an adapter, because upstream ships a breaking major roughly monthly (v87 2026-05-18 to v92 2026-09-14). Authenticate with `jferrl/go-githubauth` v1.9.1 (oauth2.TokenSource, jwt/v5, KMS-capable) behind our own `TokenSource` interface; `bradleyfalzon/ghinstallation` v2.19.0 is the mature alternative but links go-github v88 into the binary. **AVOID** `gofri/go-github-ratelimit` (dormant since 2025-03). Own conditional-request layer (go-github does not handle 304) and own SHA-256-only webhook HMAC check (go-github's `ValidatePayload` falls back to SHA-1 and skips validation when the secret is empty).
4. **Resilience**: `sony/gobreaker/v2` v2.4.0 + `x/sync` v0.24.0 (errgroup, semaphore, singleflight) + `x/time/rate` v0.16.0 + `cenkalti/backoff/v7` v7.0.1 (correction: v5 is not current; v7 is). `failsafe-go` v0.9.8 is a credible but pre-1.0 alternative.
5. **Content pipeline**: `goldmark` **v1.8.6** (not v2.1.6 yet: the v2 extension ecosystem requires `chroma/v3` alpha and Go 1.27) + `chroma/v2` v2.27.0 + `bluemonday` v1.0.27 (works, but upstream is slow: no release since 2024-07 and open CSS-handler DoS reports; never allow `style`). Parse front matter ourselves with strict YAML.
6. **Config and YAML**: `go.yaml.in/yaml/v3` v3.0.5 (YAML-org maintained successor; `gopkg.in/yaml.v3` / `go-yaml/yaml` is archived) with `KnownFields(true)`, `caarlos0/env/v11` v11.4.1 for `SCP_*` and `,file` secrets, hand-written layering; `invopop/jsonschema` v0.14.0 (with `FieldNameTag: "yaml"`, tested) for schema generation, `santhosh-tekuri/jsonschema/v6` v6.0.3 for validation in tests. `koanf` v2.3.8 is the alternative.
7. **Observability**: OpenTelemetry Go **traces, metrics and logs are all Stable** (logs became stable 2026-10-02, v1.47.0). Pin `otel` v1.47.0; the Prometheus exporter (v0.69.0), `otelhttp` (v0.72.0), `otelslog` (v0.21.0) and runtime instrumentation are still 0.x modules.
8. **Store**: `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4, CGO-free, WAL fine) is the default; `ncruces/go-sqlite3` v0.35.6 (now wasm2go, not wazero) is a strong alternative; `bbolt` v1.5.0 stays the KV fallback. `otter/v2` v2.3.0 only if profiling asks for it.
9. **Quality gates**: `golangci-lint` v2.14.0 installed as a pinned binary (its docs say not to use `go tool`), `govulncheck` v1.9.0, stdlib `testing` + `go-cmp` v0.7.0 + `goleak` v1.3.0 (verified working on Go 1.27.1) + `testscript`; own import-boundary test plus `depguard`; GoReleaser v2.18.3 for release assets, multi-stage Dockerfile onto `gcr.io/distroless/static-debian13:nonroot` by digest; **Renovate**, not Dependabot (Dependabot lacks `tool` directive support and Go major-version handling).
10. **Empirically verified**: a scratch module with the ADOPT set resolved with `go mod tidy`, built with `CGO_ENABLED=0` on Go 1.27.1 (18.1 MiB stripped), and `govulncheck -mode=binary` reported no vulnerabilities (section 9).

## 1. Method and conventions

- **Version/date source**: Go module proxy, `https://proxy.golang.org/<module>/@latest` and `/@v/<version>.mod` (gives the exact latest tag, its publish time and the `go` directive it requires). Cited as `[proxy]`.
- **Maintenance/licence/stars/archived/last push**: GitHub REST API `https://api.github.com/repos/<owner>/<repo>`, plus `/releases`, `/commits`, `/issues` (read through an authenticated `gh` CLI, read-only). Cited as `[gh <owner>/<repo>]`, which means `https://github.com/<owner>/<repo>`.
- **Primary docs** are cited by URL where used. All were fetched on 2026-10-13.
- Pins are **verified existing tags**, not guesses. "Last push" is the repo default-branch push date and is not a release date.
- Where a fact could not be confirmed it is in section 12 (Unverified), not in the tables.

## 2. Go toolchain: what is current and what matters for SCP

| Topic | Verified fact | Source (verified 2026-10-13) | Consequence for SCP |
|---|---|---|---|
| Current release | Go 1.27.0 released 2026-08-19; 1.27.1 2026-09-01; **1.27.2 2026-10-08** (security fixes to go command, crypto/tls, html/template, net/http, net/textproto, os). Go 1.26.9 released the same day with the same security fixes. | https://go.dev/doc/devel/release | Build with 1.27.2. Local machine has 1.27.1: `govulncheck` against it reports stdlib issues fixed in 1.27.2 (13 "in modules you require, not called"), a reason to pin the patch level. |
| Support policy | "Each major Go release is supported until there are two newer major releases." So 1.27 and 1.26 are supported; **1.25 is not** (derived from this rule and 1.27 being out). | https://go.dev/doc/devel/release | CI uses 1.27.2; optionally also run 1.26.9 as a canary. |
| Language | 1.26: `new(expr)`, self-referential generic constraints. 1.27: generic methods (methods may declare their own type parameters), generalised type inference. 1.24: generic type aliases. 1.23: range-over-func (`iter`). 1.22: per-iteration loop vars, range over int. | https://go.dev/doc/go1.27 , https://go.dev/doc/go1.26 , https://go.dev/doc/go1.24 , https://go.dev/doc/go1.23 , https://go.dev/doc/go1.22 | Use `new(expr)` for optional pointer DTO fields (OpenAPI nullables); no generics gymnastics needed. |
| `net/http.ServeMux` patterns | Method + wildcard patterns (`"GET /groups/{groupId}"`, `{path...}`, `{$}`, `Request.PathValue`) since 1.22; **1.26: ServeMux trailing-slash redirects now use 307 instead of 301**. 1.27: HTTP/2 server honours RFC 9218 priorities, `Server.MaxHeaderValueCount`, HTTP/1 `Response.Body` drains on close up to a limit. | https://go.dev/doc/go1.22 , https://go.dev/doc/go1.26 , https://go.dev/doc/go1.27 | Plain `net/http` mux is enough for the route table in D-12 and the SPA fallback. A 307 for trailing slash is not a permanent redirect; any permanent redirect the route table defines must be an explicit `301/308` handler, not left to ServeMux. |
| `log/slog` | 1.22 vet checks for bad key/value pairs; 1.25 `GroupAttrs`, `Record.Source`; **1.26 `slog.NewMultiHandler`**. Nothing new in 1.27. | https://go.dev/doc/go1.25 , https://go.dev/doc/go1.26 | No third-party multi-handler (e.g. `samber/slog-multi` v1.8.0) is needed. |
| `iter` | Range-over-func since 1.23; go-github v92 generates `*Iter` pagination iterators. | https://go.dev/doc/go1.23 ; go-github README (https://github.com/google/go-github) | Use iterator pagination for repo/commit lists. |
| **GOMAXPROCS** | **Go 1.25: on Linux GOMAXPROCS defaults to the cgroup CPU bandwidth limit when lower than the logical CPU count, and is updated periodically at runtime**; disabled if `GOMAXPROCS` is set; `GODEBUG=containermaxprocs=0,updatemaxprocs=0` opt out. The runtime ignores Kubernetes "CPU requests". | https://go.dev/doc/go1.25 | **Drop `uber-go/automaxprocs`.** Set a CPU *limit* in the Deployment or set `GOMAXPROCS` explicitly. No automatic `GOMEMLIMIT` appears in the 1.25/1.26/1.27 notes (grep returned 0), so set `GOMEMLIMIT` (about 80-90% of the container limit) in the image/manifest. |
| `testing/synctest` | Experimental in 1.24, **GA in 1.25** (`synctest.Test` bubble with a fake clock, `synctest.Wait`). **1.27 adds `synctest.Sleep` and `httptest.NewTestServer` (in-memory fake network for synctest).** | https://go.dev/doc/go1.25 , https://go.dev/doc/go1.27 | The sync engine (jitter, backoff, rate-limit pause, staleness) can be tested with zero real sleeps and no hand-rolled clock fakes; `internal/platform/clock` can stay a thin interface for production wiring only. |
| `testing` extras | `testing.B.Loop` (1.24), `T.Attr` (1.25), `sync.WaitGroup.Go` (1.25). 1.27: `go test` runs the `stdversion` vet check by default. | https://go.dev/doc/go1.24 , 1.25 , 1.27 notes | Use `b.Loop()` and `wg.Go`. |
| `go.mod` `tool` directive | **Since Go 1.24**: `go get -tool <pkg>`, `go tool <name>`, `go install tool`. Tool requirements take part in MVS like any other requirement. | https://go.dev/doc/go1.24 , https://go.dev/doc/modules/managing-dependencies#tools | Use for light tools only (see section 8). `go.mod` also has `ignore` directive (1.25). `go mod init` now writes `go 1.(N-1).0` (1.26); `go mod tidy` for `go 1.27` modules merges to at most two `require` blocks. |
| **`encoding/json/v2`** | **New in 1.27 as ordinary packages `encoding/json/v2` and `encoding/json/jsontext`** (was `GOEXPERIMENT=jsonv2` in 1.25/1.26). `encoding/json` is now backed by the v2 engine, behaviour preserved, "unmarshal performance significantly faster", marshal performance unchanged; opt-out `GOEXPERIMENT=nojsonv2` "expected to be removed in a future release". v2 defaults: rejects invalid UTF-8 and **duplicate object names**. v1 API continues to be supported. Removed during the experiment: `format` and `unknown` tag options, `DiscardUnknownMembers`, `SkipFunc`; `inline` tag renamed `embed`. | https://go.dev/doc/go1.27 | Keep `encoding/json` (v1 API) for the API and generated code: faster for free, no migration. Consider `json/v2` only at trust boundaries where duplicate-key ambiguity matters (webhook bodies, manifests). Do not depend on v2 tag spellings from older blog posts (`inline`, `format`). |
| New stdlib packages | 1.27: `uuid` (`New`, `NewV4`, `NewV7`, `Parse`, `MustParse`; v7 sorts by time), `crypto/mldsa`, experimental `simd`, `simd/archsimd`. 1.24: `os.Root`, `weak`, `crypto/hkdf,pbkdf2,sha3`. 1.26: `crypto/fips140` module v1.26.0 selectable; `goroutineleak` profile experiment, **GA in 1.27** (`/debug/pprof/goroutineleak`). | https://go.dev/doc/go1.27 , https://pkg.go.dev/uuid@go1.27.2 | Use stdlib `uuid.NewV7` for request/delivery IDs (drops `google/uuid`). `os.Root` is a good fit for serving the `pages` directory safely. Expose the goroutine-leak profile on the admin listener (it comes with `net/http/pprof`). |
| GC / runtime | 1.26: Green Tea GC default (10-40% lower GC overhead claimed), faster cgo calls; opt-out `GOEXPERIMENT=nogreenteagc` removed in 1.27. 1.27: size-specialised small allocations (~1% in alloc-heavy code), goroutine labels in tracebacks. | https://go.dev/doc/go1.26 , https://go.dev/doc/go1.27 | Nothing to configure. |
| PGO | `default.pgo` in the main package directory is applied automatically (`-pgo=auto`, default since 1.21); stated gain "around 2-14%" (as of 1.22), build time increases. Recommended when you can collect **production** profiles. | https://go.dev/doc/pgo | **Not for v1.** A single-replica dashboard with a cached read model is not CPU-bound; revisit only if production profiles show hot paths. |
| FIPS 140-3 | `GOFIPS140=latest|v1.0.0|inprocess|certified|off` at build time, `GODEBUG=fips140=on|only`. Module v1.0.0 has CMVP cert #5247; v1.26.0 was "Pending Review" in the CMVP in-process list as of 2026-04-28. Go+BoringCrypto is incompatible and slated for removal. | https://go.dev/doc/security/fips140 | **Not for v1** (no stated FIPS requirement). The build is a single env var if a customer ever demands it; do not enable unnecessarily. |
| GODEBUG hygiene | 1.27 removed several long-deprecated GODEBUG settings (e.g. `asynctimerchan`, `tls3des`); new `htmlmetacontenturlescape` (html/template, backported to 1.25.8/1.26.1). | https://go.dev/doc/godebug | Using `go 1.27.2` in `go.mod` selects current defaults; do not add `godebug` overrides. |
| gofmt/vet/fix | 1.26 rewrote `go fix` as the home of modernizers (analysis framework shared with vet); 1.27 adds modernizers and changes `gofmt` column alignment (re-run gofmt once at repo start). | https://go.dev/doc/go1.26 , https://go.dev/doc/go1.27 | Run `go fix ./...` in CI as a "no diff" check from day one. |

## 3. Decision table (ADOPT / ALTERNATIVE / AVOID)

Legend: **ADOPT** = start with this; **ALT** = acceptable, documented fallback; **AVOID** = do not use for the stated reason. Dates are the publish time of the pinned tag from the Go proxy. "Min Go" is the `go` directive in that tag's `go.mod`. Licence from GitHub's licence detector except where noted.

### 3.1 GitHub boundary and auth

| Candidate | Verdict | Pin | Released | Min Go | Licence | Evidence and justification |
|---|---|---|---|---|---|---|
| `github.com/google/go-github/v92` | **ADOPT** (isolated in `internal/github`) | **v92.0.0** | 2026-09-14 | 1.26.0 | BSD-3-Clause | Active (push 2026-10-13, 11.3k stars). Majors: v87 2026-05-18, v88 05-21, v89 07-06, v90 08-04, v91 09-03, v92 09-14: **six breaking majors in four months**; v92 itself renames Codespaces, dependency-graph and comment request types. Dependencies are only `go-cmp` and `go-querystring`. API: `github.NewClient(opts ...ClientOptionsFunc) (*Client, error)` with `WithHTTPClient`, `WithTransport`, `WithAuthToken`, `WithEnterpriseURLs`, `WithDisableRateLimitCheck`; iterator methods (`ListIter`); `X-GitHub-Api-Version` default 2022-11-28, and `2026-03-10` is supported. **No conditional-request support**: `CheckResponse` turns every non-2xx, including 304, into `*ErrorResponse`; README recommends a caching `http.Transport`. Policy: use only the services we need (Repositories, Git trees/blobs, Commits, Releases, Apps), keep types out of other packages (D-04), pin with Renovate `gomodUpdateImportPaths` and bump on a schedule, not on every release. [gh google/go-github], [proxy], https://github.com/google/go-github/releases/tag/v92.0.0, source read at tag v92.0.0 (`github/github.go`, `github/messages.go`) |
| `github.com/jferrl/go-githubauth` | **ADOPT (provisional), behind our own `TokenSource`** | **v1.9.1** | 2026-09-19 | 1.26.0 | MIT | `oauth2.TokenSource` implementations for App JWT, installation token and PAT; `crypto.Signer` so the App key can live in KMS/Vault; GHES (`WithEnterpriseURL`) and GHEC data residency; proactive refresh (`WithExpirySkew`); only deps `golang-jwt/jwt/v5` (v5.3.1) and `x/oauth2`. Recommended in the current go-github README. Weakness: young and small (created 2024-06, 34 stars, 7 contributors, 18 releases, very rapid recent releases). Mitigation: it is a tiny, stable protocol (JWT RS256 then `POST /app/installations/{id}/access_tokens`), isolated behind the Q-13 `TokenSource`; fallback is ~100 lines on `golang-jwt/jwt/v5`. [gh jferrl/go-githubauth], [proxy] |
| `github.com/bradleyfalzon/ghinstallation/v2` | **ALT** | v2.19.0 | 2026-06-11 | 1.25.0 | Apache-2.0 | Mature (375 stars) and moderately active (releases 2026-03, 2026-06). But `transport.go` imports go-github (`go.mod` requires **go-github/v88**), so it compiles a second go-github major into our binary next to v92, and it uses `golang-jwt/jwt/v4` (v4.5.2, last v4 release 2025-03-21; v5 is current). [gh bradleyfalzon/ghinstallation], [proxy] |
| `github.com/gofri/go-github-ratelimit` (v1 / v2) | **AVOID** | v1.1.1 / v2.0.2 | 2025-03 | 1.19 / 1.23 | MIT | Last commit 2025-03-14 (19 months dormant), pinned to old go-github majors. go-github has built-in primary/secondary limit errors (`RateLimitError`, `AbuseRateLimitError`, `Retry-After`) and `WithMaxSecondaryRateLimitRetryAfterDuration`; we write the pause logic once in the `Upstream` type (also required by D-03 for readiness). [gh gofri/go-github-ratelimit] |
| `bartventer/httpcache` v0.14.0 and `bored-engineer/github-conditional-http-transport` v0.0.8 | **ALT** (not adopted) | - | 2026-08-11 / 2026-10-03 | 1.25 / 1.26 | - | Both are named in go-github's README for conditional requests, both pre-1.0. We need validators and bodies **persisted in the Store** for warm start (D-02) and keyed by repo+kind, so a thin own transport/adapter (If-None-Match, treat 304 as "unchanged") is simpler than adapting a generic RFC 9111 cache. [proxy] |
| `github.com/google/go-github/otel/v92` | **AVOID** | pseudo-version only | 2026-10-13 | 1.26.0 | BSD-3 | Only pseudo-versions on the proxy; uses deprecated semantic-convention attribute names (`http.method`, `http.url`, `http.status_code`). Use `otelhttp.NewTransport` and add GitHub rate-limit attributes ourselves. Source read at tag v92.0.0 (`otel/transport.go`). |
| `shurcooL/githubv4` (GraphQL) | **ALT, defer** | pseudo-version `v0.0.0-20260209031235-2402fdf4a9ed` | 2026-02-09 | 1.19 | MIT | No tagged releases; commits are schema regenerations (2024-11, 2025-01, 2026-02). Recommended by go-github's README. Alternatives: `hasura/go-graphql-client` v0.16.0 (2026-04-11), `Khan/genqlient` v0.8.1 (2025-05-18, stale). Adopt only if REST request counts at 1,000 repos prove too high; REST + conditional requests (304 does not count against the primary limit, see 3.9) is the v1 path. [gh shurcooL/githubv4], [proxy] |
| `golang.org/x/oauth2` | **ADOPT** (needed by go-githubauth; also the stdlib-adjacent OIDC extension later) | **v0.37.0** | 2026-08-25 | 1.26.0 | BSD-3-Clause | Active (push 2026-09-08). [gh golang/oauth2], [proxy] |

**Webhook handling (D-09/D-24 conflict with go-github helper).** `github.ValidatePayload(r, secret)` reads `X-Hub-Signature-256` **but falls back to the older SHA-1 `X-Hub-Signature`**, and `ValidatePayloadFromBody` only validates "if present or if one is expected (secretToken is non-empty)", so an empty secret skips verification. It also accepts `application/x-www-form-urlencoded`. The decisions require SHA-256 only, constant time, size limit. **Recommendation**: implement the check in `internal/webhook` (`io.LimitReader` -> `hmac.New(sha256.New, secret)` -> `hmac.Equal` on the `sha256=` header), then use go-github only for typed payloads via `github.ParseWebHook`. go-github's own cap is 25 MiB (`maxPayloadSize`), far above what push events need; set ours explicitly. (Read from `github/messages.go` at v92.0.0, 2026-10-13.)

**GitHub REST versioning.** Supported API versions: `2026-03-10` (no end date) and `2022-11-28` (default, end of support 2028-03-10); header `X-GitHub-Api-Version`; each version supported at least 24 months after a successor. Pin the header explicitly in the adapter to `2022-11-28` and schedule a move to `2026-03-10` after reading its breaking-changes list (not read here). Source: https://docs.github.com/en/rest/about-the-rest-api/api-versions (2026-10-13).

### 3.2 Resilience and concurrency

| Candidate | Verdict | Pin | Released | Min Go | Licence | Evidence and justification |
|---|---|---|---|---|---|---|
| `github.com/sony/gobreaker/v2` | **ADOPT** | **v2.4.0** | 2026-01-01 | 1.22.0 | MIT | Generic `NewCircuitBreaker[T](Settings)`; `Settings` has `ReadyToTrip`, `OnStateChange`, `IsSuccessful`, and (2026-01-01) tri-state outcome evaluation with `IsExcluded`; rolling-window buckets (`BucketPeriod`); 3.7k stars; last commit 2026-02-07 (low churn is expected for a finished library). Maps cleanly onto the "one breaker per upstream" design and `OnStateChange` -> metric. Note: the old `github.com/sony/gobreaker` v1.0.0 (2024-04) is non-generic; use `/v2`. [gh sony/gobreaker], [proxy] |
| `github.com/failsafe-go/failsafe-go` | **ALT** | v0.9.8 | 2026-09-30 | 1.22 | MIT | Actively released (v0.9.3 2025-12 to v0.9.8 2026-09) with composable Retry, Fallback, Circuit Breaker, Adaptive Limiter/Throttler, Bulkhead, Rate Limiter, Timeout, Hedge, Cache. Still **0.x** (API may move), and one coherent policy chain is more than SCP needs for one dominant upstream. Revisit if optional Jenkins/Snyk providers multiply. [gh failsafe-go/failsafe-go], [proxy] |
| `golang.org/x/sync` | **ADOPT** | **v0.24.0** | 2026-09-23 | 1.26.0 | BSD-3 | `errgroup` (with `SetLimit`) for lifecycle, `semaphore` for bulkheads, `singleflight` for in-flight dedupe. `sync.WaitGroup.Go` (Go 1.25) covers the no-error case. [proxy] |
| `golang.org/x/time/rate` | **ADOPT** | **v0.16.0** | 2026-08-19 | 1.26.0 | BSD-3 | Token bucket for self-imposed request pacing under the 5,000/h budget. [proxy] |
| `github.com/cenkalti/backoff/v7` | **ADOPT narrowly** (wrap in `internal/platform/retry`) | **v7.0.1** | 2026-09-30 | 1.23 | MIT | **Correction: v5 is not current.** v5.0.3 (2025-07-23) -> v6.0.0 (2026-06-16: `Retry` returns `*RetryError`, `ErrPermanent/ErrExhausted/ErrMaxElapsedTime`) -> v7.0.0 (2026-06-30: `RetryAfter(d, cause)` requires a cause) -> v7.0.1 (2026-09-30: overflow and nil fixes). `RetryAfterError` fits GitHub's `Retry-After`. Churn is a risk: the README itself says "copy it from retry.go and adapt it". The sync engine schedules retries through the job queue (D-03), so this library is only for short in-call retries; keep it behind one package so a 20-line own implementation can replace it. Zero module dependencies beyond stdlib per `go.mod`. [gh cenkalti/backoff], [proxy], https://github.com/cenkalti/backoff/blob/master/CHANGELOG.md |
| `github.com/robfig/cron/v3` | **AVOID** | v3.0.1 | 2020-01-04 | 1.12 | MIT | Last release 2020, last push 2024-07. The schedule is intervals with jitter (D-03), not calendar cron; `time.Ticker` + jitter covers it. If calendar schedules appear, `go-co-op/gocron/v2` v2.22.0 (2026-07-09) is the maintained option. |

### 3.3 Rendering, templating, front end delivery

D-16 supersedes server-rendered templates and HTMX, so these rows are recorded for completeness and for the small server-side templates that remain (index shell with CSP nonce/hash, SVG badges, `llms.txt`).

| Candidate | Verdict | Pin | Released | Licence | Evidence and justification |
|---|---|---|---|---|---|
| stdlib `html/template` / `text/template` | **ADOPT** | Go 1.27.2 | 2026-10-08 | BSD-3 | Enough for the index shell, SVG badges (XML-escape via template, GR-33) and `llms.txt`. 1.26.9/1.27.2 carry html/template security fixes; 1.27 adds a meta-content URL escape default. |
| `github.com/a-h/templ` | **AVOID for v1** (no server-rendered pages after D-16) | v0.3.1070 | 2026-10-04 | MIT | Very active (push 2026-10-09, 10.6k stars) but still 0.x. Would only matter if D-16 were reversed. [gh a-h/templ], [proxy] |
| htmx | **N/A (superseded by D-16)** | 4.0.0 / 2.0.10 | 2026-08-28 / 2026-09-06 | 0BSD (file header "Zero-Clause BSD"; GitHub reports NOASSERTION) | 4.0.0 is a new major (betas 2026-05 to 07); 2.0.x continues to receive releases. Recorded only. [gh bigskysoftware/htmx] |
| `github.com/yuin/goldmark` v1 | **ADOPT** | **v1.8.6** | 2026-09-03 | MIT | CommonMark 0.31.2, GFM extensions (`extension.GFM`: tables, strikethrough, linkify, task lists), footnotes, definition lists, typographer. v1.8.0 (2026-03-24) added position info to all nodes. Upstream policy: "maintain bug fixes, including security fixes, up to one major version prior to the latest major", so v1 is maintained while v2 is latest. [gh yuin/goldmark], [proxy] |
| `github.com/yuin/goldmark/v2` | **ALT / planned migration spike** | v2.1.6 | 2026-09-27 | MIT | v2.0.0 on 2026-08-27 after 15+ betas/RCs; semantic AST, generics, parser/renderer split, builds nodes programmatically, ships an LLM-assisted migration plugin. Six releases in the first month, including exported-field refactors in a patch (v2.0.2), and the README says "v2 is still in the early stages of release. If you are using an extension that does not support v2, please use v1." Ecosystem split: `goldmark-highlighting/v3` v3.1.0 **requires goldmark v2, chroma/v3 (alpha.5 only), Go 1.27 and json/v2**; `goldmark-alert` v1.0.1 (GitHub alerts) exists **only for goldmark v2**. Decision: start on v1 behind `internal/docs/render`, re-evaluate in 2027-Q1. [gh yuin/goldmark] README on branch v2, [gh yuin/goldmark-highlighting] README, `go.mod` of goldmark-alert@v1.0.1 |
| `github.com/alecthomas/chroma/v2` | **ADOPT** | **v2.27.0** | 2026-06-17 | MIT (LICENSE text; GitHub says NOASSERTION) | Active (push 2026-10-12). `chroma/v3` exists only as alphas (v3.0.0-alpha.5, 2026-07-08): **AVOID v3**. Important pin trap: `goldmark-highlighting/v2` pins chroma v2.2.0 from 2023, so `go mod tidy` selects chroma **v2.2.0** unless you `go get github.com/alecthomas/chroma/v2@v2.27.0` explicitly (observed in section 9). [gh alecthomas/chroma], [proxy] |
| `github.com/yuin/goldmark-highlighting/v2` | **ADOPT (frozen)** | v2.0.0-20230729083705-37449abec8cc | 2023-07-29 | MIT | Stable, unreleased-as-semver but widely used; alternative is ~50 lines of own fenced-code renderer calling chroma directly with class-based output (preferred if we want CSP-friendly CSS classes and no inline styles). [proxy] |
| GitHub alerts (`> [!NOTE]`) on goldmark v1 | **ADOPT own transformer** | - | - | - | The alert extension is v2-only; `ZMT-Creative/gm-alert-callouts` is a 2025-09 pseudo-version. Alerts are a blockquote whose first line is `[!KIND]`; a ~60-line AST transformer is cheaper than a dependency and is portable to v2 later. |
| `github.com/microcosm-cc/bluemonday` | **ADOPT with constraints** | **v1.0.27** | 2024-07-04 | BSD-3 | No release since 2024-07-04; last commit 2025-04-04; 10 open PRs; 2026 issues include **#225 (2026-06-26) "Unreasonably long sanitization" (2-3 s CPU on a `style` attribute with the `font` handler)** and PR **#227 (2026-09-05) "remove exponential-time CSS DoS"** in the shorthand style handlers; #224 notes govulncheck flags old `x/net`. Mitigations: never call `AllowStyles`/`AllowAttrs("style")`, use a strict allow-list (no `UGCPolicy` blindly), cap input size before sanitising, force `golang.org/x/net >= v0.61.0` (done in section 9), run sanitisation in the sync worker (never on a request path) with a context deadline, fuzz it (D-10). There is no maintained drop-in competitor; a custom `x/net/html` allow-list sanitiser is the fallback. [gh microcosm-cc/bluemonday] issues and commits (2026-10-13) |
| Front matter | **ADOPT own splitter + strict YAML** | - | - | - | Split `---` delimiters yourself, decode with `go.yaml.in/yaml/v3` `KnownFields(true)` into a typed struct (same code path as the `scp.yaml` descriptor). `go.abhg.dev/goldmark/frontmatter` v0.3.0 (2025-11-17, BSD-3) is v1-only and pulls toml + `gopkg.in/yaml.v3`. [proxy] |

### 3.4 Configuration and YAML

| Candidate | Verdict | Pin | Released | Min Go | Licence | Evidence and justification |
|---|---|---|---|---|---|---|
| `go.yaml.in/yaml/v3` | **ADOPT** | **v3.0.5** | 2026-07-26 | 1.16 | Apache-2.0 | Maintained by the YAML organisation after Gustavo Niemeyer marked `go-yaml/yaml` unmaintained (April 2025; repo archived, last push 2025-04-01). README: v1-v3 are frozen ("security-fixes only"); all new work in **v4**. testify v1.12.1 already depends on `go.yaml.in/yaml/v3 v3.0.5`. Verified: `Decoder.KnownFields(true)` gives `line 3: field bogus not found in type main.Cfg` (section 9). [gh yaml/go-yaml], [proxy] |
| `go.yaml.in/yaml/v4` | **ALT, wait** | v4.0.0-rc.6 | 2026-06-17 | 1.18 | Apache-2.0 | Release candidates only (rc.1..rc.6), active (commits 2026-10-13), open design work on plugin APIs. Migrate when v4.0.0 is tagged; the import path swap is mechanical. [gh yaml/go-yaml] |
| `github.com/goccy/go-yaml` | **ALT** | v1.19.2 | 2026-01-08 | 1.21.0 | MIT | Good error annotation with source excerpts and path-based API, 2.2k stars, but 246 open issues, a single dominant maintainer, last commit 2026-04-07, and parser behaviour differs from libyaml-derived parsers (fixes in 2026 such as "sibling anchor aliases silently resolving to nil"). Pick only if source-annotated errors become a hard UX requirement for `scp config lint`. [gh goccy/go-yaml] |
| `gopkg.in/yaml.v3` | **AVOID** (direct use) | v3.0.1 | 2022-05-27 | - | MIT/Apache | Frozen since 2022, upstream archived. It will still appear indirectly via older libs (it did in section 9: via goldmark-highlighting/other deps). [gh go-yaml/yaml] archived=true |
| `github.com/caarlos0/env/v11` | **ADOPT** | **v11.4.1** | 2026-05-01 | 1.18 (module 1.26 on master) | MIT | Env -> struct with `env:"NAME"`, `envDefault`, `,required`, `,notEmpty`, `,unset`, **`,file` (value is a path to read)**, `envPrefix`, `,init`. 6.3k stars, active. Several v11 tags are **retracted** (v11.0.1, v11.2.0, v11.3.0): never float the version. Gives `SCP_*` and `*_FILE` semantics without reflection of our own. [gh caarlos0/env], [proxy] |
| `github.com/knadh/koanf/v2` | **ALT** | v2.3.8 | 2026-10-07 | 1.23.0 | MIT | Very active, layered providers (file, env, flags) but map-based: strictness comes from mapstructure `ErrorUnused`, error messages lose YAML line numbers, and providers/parsers are separate modules. Reasonable if layering rules become complex; our layering is fixed (defaults, file, env, flags). [gh knadh/koanf] |
| `github.com/spf13/viper` | **AVOID** | v1.21.0 | 2025-09-08 | 1.23.0 | MIT | Global state, case-insensitive key folding, weak unknown-key handling; conflicts with D-07 (strict, validated, documented keys == consumed keys). |
| `github.com/ardanlabs/conf/v3` | **AVOID** | v3.13.0 | 2026-07-26 | 1.23.0 | Apache-2.0 | Env and flags only (no YAML file layer), tag grammar is idiosyncratic. Maintained, but covers less than caarlos0/env + our code. |
| `github.com/kelseyhightower/envconfig` | **AVOID** | v1.4.0 | 2019-05-24 | - | MIT | Last release 2019; no `_FILE` support. |
| stdlib `flag` | **ADOPT** | - | - | - | The CLI has three subcommands (`serve`, `config lint`, `version`); stdlib `flag.FlagSet` per subcommand is enough. `spf13/cobra` v1.10.2 (2025-12-03) is the maintained heavier alternative if shell completion is wanted. |

**Recommended layering (D-07):** embedded default YAML -> file (strict decode) -> `env.ParseWithOptions` (`SCP_` prefix, `,file`) -> flags -> `Validate()`. Documented-keys-equal-consumed-keys test: reflect over the struct tags that generate the schema and the reference doc, and assert every `env`/`yaml` tag appears in the generated doc.

### 3.5 JSON Schema

| Candidate | Verdict | Pin | Released | Min Go | Licence | Evidence and justification |
|---|---|---|---|---|---|---|
| `github.com/invopop/jsonschema` | **ADOPT** (generation) | **v0.14.0** | 2026-04-23 | 1.24 | MIT | Draft 2020-12 output; `Reflector.FieldNameTag` selects the struct tag used for names. **README says "yaml tags dropped", but `reflect.go` still exposes `FieldNameTag`, and a scratch test with `FieldNameTag: "yaml"`, `RequiredFromJSONSchemaTags`, `DoNotReference` produced the expected schema (required list, enum, descriptions, defaults, `additionalProperties:false`)** (section 9). Pre-1.0, 960 stars, last push 2026-04-23, 62 open issues. Use `jsonschema:"required,enum=1,description=..."` tags; keep `AddGoComments` out of the build (needs source at runtime). [gh invopop/jsonschema], [proxy] |
| `github.com/santhosh-tekuri/jsonschema/v6` | **ADOPT** (validation in tests and `scp config lint --schema` only) | **v6.0.3** | 2026-06-28 (GitHub release 2026-08-06) | 1.21 | Apache-2.0 | Passes the official JSON-Schema-Test-Suite for drafts 4 to 2020-12 (bowtie), format assertions, custom regex engine, cycle detection. Verified: it rejected a document with an unknown key. Runtime config validation stays strict struct decoding plus `Validate()`; the schema is an IDE/CI artefact. [gh santhosh-tekuri/jsonschema] |
| `github.com/google/jsonschema-go` | **ALT** | v0.4.3 | 2026-04-17 | 1.23.0 | MIT | Zero-dependency inference (`For[T]`) plus validation in one module, backed by Google (MCP SDK). Pre-1.0. A fine swap if one dependency should replace two. [gh google/jsonschema-go] |
| `github.com/xeipuuv/gojsonschema` | **AVOID** | v1.2.0 | 2019-10-15 | - | Apache-2.0 | Unmaintained. |
| `github.com/kaptinlin/jsonschema` | **AVOID** | v0.9.10 | 2026-09-13 | **1.27.0** | - | Requires Go 1.27.0 in `go.mod`, pre-1.0, little track record. |

### 3.6 Observability

Status per the project README and CHANGELOG (https://github.com/open-telemetry/opentelemetry-go, 2026-10-13): **Traces Stable, Metrics Stable, Logs Stable.** CHANGELOG entry `[1.47.0/0.69.0/0.23.0/0.1.0] - 2026-10-02`: "first stable release of the OpenTelemetry Go Logs API and SDK" (`otel/log`, `otel/sdk/log`). All modules below need Go 1.26.0.

| Module | Verdict | Pin | Released | Stability | Notes |
|---|---|---|---|---|---|
| `go.opentelemetry.io/otel`, `/sdk`, `/sdk/metric`, `/trace`, `/metric` | **ADOPT** | **v1.47.0** | 2026-10-02 | Stable (`stable-v1`) | Instrument via the OTel API; SDK wired only in `cmd/scp`. |
| `go.opentelemetry.io/otel/exporters/prometheus` | **ADOPT** | **v0.69.0** | 2026-10-02 | **0.x (unstable versioning)** | Serves `/metrics` on the admin listener (D-08). Pulls `prometheus/client_golang` (resolved v1.24.1; pin **v1.25.0**, 2026-10-08). 0.x means minor-version breaking changes are allowed; isolate in `internal/platform/telemetry`. Fallback: `prometheus/client_golang` directly (stable 1.x) for metrics only. |
| `otlptracehttp`, `otlpmetrichttp` | **ADOPT (optional, off by default)** | v1.47.0 | 2026-10-02 | Stable | `WithMaxResponseSize` added in 1.47.0. |
| `go.opentelemetry.io/otel/log`, `/sdk/log` | **ADOPT only if OTLP logs are wanted** | v1.47.0 | 2026-10-02 | **Stable as of 2026-10-02 (11 days old)** | slog JSON to stdout stays the default; add the bridge only when an OTLP log pipeline exists. |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | **ADOPT** | **v0.72.0** | 2026-10-02 | `experimental-instrumentation` set (0.x) | Server middleware and `otelhttp.NewTransport` for the GitHub client; tracks semconv v1.43.0. Widely used despite 0.x. |
| `go.opentelemetry.io/contrib/bridges/otelslog` | **ADOPT if OTLP logs** | v0.21.0 | 2026-10-02 | `experimental-bridge` (0.x) | |
| `go.opentelemetry.io/contrib/instrumentation/runtime` | **ADOPT** | v0.72.0 | 2026-10-02 | 0.x | Go runtime metrics. |

Source for stability sets: `versions.yaml` in https://github.com/open-telemetry/opentelemetry-go-contrib (verified 2026-10-13: `stable-v1` v1.47.0, `stable-v2` v2.6.0, `experimental-instrumentation` v0.72.0, `experimental-bridge` v0.21.0).

### 3.7 Persistence and cache

| Candidate | Verdict | Pin | Released | Min Go | Licence | Evidence and justification |
|---|---|---|---|---|---|---|
| `modernc.org/sqlite` | **ADOPT** | **v1.60.1** (SQLite **3.53.4**; also pin `modernc.org/libc v1.77.1`, which `go mod tidy` selected) | 2026-09-29 | 1.26.0 | BSD-3-Clause (plus 17 third-party licences listed in `LICENSE-3RD-PARTY.md`) | Transpiled C, CGO-free, `database/sql` driver. v1.60.0 (2026-09-28): a fault reading the mmap'd `-shm` of a WAL database no longer crashes the process (fixes a WAL-specific crash class); adds `StrictPragmas` (opt-in; **enable it**: `_pragma` values run as SQL text), SECURITY.md, SBOM (CycloneDX + SPDX) and signed provenance per release (v1.61.0 notes). Maintainer states only the latest release is supported. Sponsors include Tailscale. **Known coupling**: downstream `go.mod` must pin the same `modernc.org/libc` as the sqlite repo (GitLab issue #177). The 2026-09-30 CHANGELOG lists v1.61.0, but the Go proxy had no such version on 2026-10-13: use v1.60.1. [gh modernc-org/sqlite] (mirror), https://gitlab.com/cznic/sqlite/-/raw/master/CHANGELOG.md , [proxy] |
| `github.com/ncruces/go-sqlite3` | **ALT (strong)** | v0.35.6 | 2026-09-23 | 1.26.0 | MIT | **Since 2026-03-12 it uses `wasm2go` (Wasm translated to Go) instead of wazero**; only required deps are Go and `x/sys` plus its own `go-sqlite3-wasm` module. Pure-Go VFS; WAL uses `mmap` shared memory on Unix and `MapViewOfFile` on Windows; without shared memory WAL needs EXCLUSIVE locking and `SetMaxOpenConns(1)`. Higher memory per connection ("each database connection executes within a Wasm sandboxed environment"). In the author-maintained `modernc.org/sqlite-bench` scorecard (darwin, Apple M1, run by the modernc author, "scores fluctuate about +/-10 points") ranking is mattn 103, **ncruces 66, modernc 39**; ncruces wins concurrent queries, modernc wins several large-query cases. Performance is irrelevant for SCP (never on the request path), so choose on operability: modernc has more history and explicit security/SBOM process; ncruces has a sandboxed engine and a smaller dependency story. [gh ncruces/go-sqlite3], https://pkg.go.dev/modernc.org/sqlite-bench |
| `zombiezen.com/go/sqlite` | **ALT (API style)** | v1.4.2 | 2025-05-23 | 1.23.0 | ISC | Non-`database/sql` low-level API on top of modernc (crawshaw lineage), has a migration package; releases 17 months old. Only if we want per-connection control instead of `database/sql`. [gh zombiezen/go-sqlite] |
| `github.com/mattn/go-sqlite3` | **AVOID** (conflicts with D-11 `CGO_ENABLED=0`) | v1.14.52 | 2026-09-05 | 1.21 | MIT | Fastest and most mature, but needs CGO, which breaks the static distroless build and cross-compile. |
| `crawshaw/sqlite`, `glebarez/go-sqlite`, `tursodatabase/go-libsql` | **AVOID** | v0.3.2 (2020) / v1.23.0 (2026-08) / pseudo 2026-04 | - | - | - | crawshaw abandoned; glebarez is a thin modernc wrapper that lags; libsql is CGO/remote oriented. |
| `go.etcd.io/bbolt` | **ALT (documented fallback in D-02)** | v1.5.0 | tag 2026-06-03 (GitHub release 2026-06-21) | 1.25.0 | MIT | Active (push 2026-10-13), single-file KV, no CGO. Choose only if relational inspection is judged unnecessary. [gh etcd-io/bbolt] |
| `github.com/maypok86/otter/v2` | **ALT (optional only if profiling asks)** | v2.3.0 | 2025-12-22 | 1.24.0 | Apache-2.0 | Stable v2 line (semver, loader, refresh-ahead, expiry, stats, persistence), but **no release in 10 months** while `main` is busy (commits through 2026-10-12). The read model is an `atomic.Pointer[Snapshot]`; a rendered-fragment cache keyed by blob SHA can start as a bounded map or `sync.Map` plus LRU. Otter v1 (v1.2.4, 2024-11) is superseded. `dgraph-io/ristretto/v2` v2.4.2 (2026-07-07) is the other mainstream option. [gh maypok86/otter], [proxy] |

### 3.8 Testing

| Candidate | Verdict | Pin | Released | Licence | Evidence and justification |
|---|---|---|---|---|---|
| stdlib `testing` (+ `synctest`, `t.Context`, `b.Loop`, fuzz) | **ADOPT** | Go 1.27.2 | - | BSD-3 | Primary. |
| `github.com/google/go-cmp` | **ADOPT** | **v0.7.0** | 2025-01-14 | BSD-3 | Stable, small; `cmp.Diff` for golden structs. Last push 2026-06-18. |
| `github.com/stretchr/testify` | **ALT** | v1.12.1 | 2026-08-17 | MIT | Maintained "at v1, no breaking changes will be accepted"; now depends on `go.yaml.in/yaml/v3 v3.0.5`; `suite` does not support parallel tests. Acceptable if the team prefers `require`/`assert`; not needed with go-cmp and `t.Fatalf`. |
| `go.uber.org/goleak` | **ADOPT** | **v1.3.0** | 2023-10-24 | MIT | Last tag is three years old but the repo is active (commits to 2026-09-15: Go 1.27 CI, Go 1.26 flake fixes). **Verified empirically on Go 1.27.1 (go 1.27 module, goroutine with pprof labels): `VerifyNone` passes when clean and `Find` reports a leaked labelled goroutine**; the label-bearing 1.27 goroutine headers parse fine. Open PRs: `VerifyCleanup` (t.Cleanup support, #159), `tracebackancestors` parsing (#149). Complement with the GA `goroutineleak` runtime profile in production. [gh uber-go/goleak] |
| `github.com/rogpeppe/go-internal/testscript` | **ADOPT** (CLI golden tests: `config lint`, `version`) | **v1.16.0** | 2026-07-01 | BSD-3 | Maintained (push 2026-10-05); txtar-based scripts match D-10 golden approach. [gh rogpeppe/go-internal] |
| `gopkg.in/dnaeon/go-vcr.v4` | **ALT** | v4.0.7 | 2026-06-25 | BSD-2 | Record/replay is unnecessary where an `httptest` fake GitHub serves checked-in fixtures (D-10); use only to bootstrap fixtures. Import path is `gopkg.in/dnaeon/go-vcr.v4` (also resolvable as `github.com/dnaeon/go-vcr/v4`). [gh dnaeon/go-vcr] |
| `gotest.tools/v3` | **AVOID** | v3.5.2 | 2024-09-05 | Apache-2.0 | Redundant with stdlib + go-cmp. |

### 3.9 Lint, architecture, supply chain

| Candidate | Verdict | Pin | Released | Licence | Evidence and justification |
|---|---|---|---|---|---|
| `golangci-lint` v2 | **ADOPT** (pinned binary, not `go tool`) | **v2.14.0** | 2026-09-24 | GPL-3.0 (tool run only, not linked into SCP) | **Go 1.27 support landed in v2.13.0** (2026-08-19, "go1.27 support"). v2 merges `staticcheck`+`gosimple`+`stylecheck` into `staticcheck`, moves formatters (`gofmt`, `gofumpt`, `goimports`, `gci`) to a `formatters` section, requires `version: "2"`. Docs: "We don't recommend using `go tool`"; `go install` "aren't guaranteed to work"; binary install recommended; use a pinned version in the action. `gomodguard` is deprecated since v2.12.0 in favour of `gomodguard_v2`. https://golangci-lint.run/docs/welcome/install/local/ , https://golangci-lint.run/docs/product/migration-guide/ , [gh golangci/golangci-lint] |
| `honnef.co/go/tools` (staticcheck standalone) | **ALT / not needed** (bundled) | 2026.2.2 (proxy v0.8.2) | 2026-10-12 | MIT | Release notes: "Staticcheck now supports the updated export data format used by Go 1.27.2": **a standalone staticcheck older than 2026.2.2 can fail on Go 1.27.2**. The bundled one inside golangci-lint is the single source. [gh dominikh/go-tools] |
| `depguard` (via golangci-lint) | **ADOPT** for third-party and `internal` import bans | bundled in golangci-lint v2.14.0 (module v2.2.1, 2025-03-06) | - | GPL-3.0 | Rules: `files`, `allow`, `deny` (with `desc`), `list-mode` (`original|strict|lax`); default allows only `$gostd`. Good for "only `internal/github` may import go-github" style rules. https://golangci-lint.run/docs/linters/configuration/ |
| **Own import-boundary test** (`go list -json ./...` or `golang.org/x/tools/go/packages`) | **ADOPT** | - | - | - | D-01/D-10 need "no feature imports another feature's internals". A 60-line test that loads all packages and asserts a feature -> feature import matrix is the most expressive and needs no tool. |
| `github.com/fe3dback/go-arch-lint` | **ALT** | v1.19.0 | 2026-09-07 | MIT | Active (releases 2026-08-05, 08-21, 09-07), YAML-declared components with `mayDependOn`, `commonComponents`, wildcards. Expressiveness for "each of N features is its own component and none may import another" was **not tested** (probably needs one component per feature). [gh fe3dback/go-arch-lint] |
| `github.com/arch-go/arch-go/v2` | **ALT** | v2.1.2 | 2026-02-03 | MIT | Dependency, content-of-package and function-shape rules; slower release cadence (v2.1.0..2.1.2 between 2025-11 and 2026-02). [gh arch-go/arch-go] |
| `golang.org/x/vuln` (govulncheck) | **ADOPT** | **v1.9.0** | 2026-10-11 | BSD-3 | Source mode (call-graph reachability) in CI and `-mode=binary` on the built artefact; formats `text|json|sarif|openvex`. **Verified on Go 1.27.1 against the section 9 binary: "No vulnerabilities found."** `golang/govulncheck-action` v1.1.0 (2026-07-10). Data: https://vuln.go.dev (DB updated 2026-10-08 22:31 UTC at test time). |
| `github.com/goreleaser/goreleaser/v2` | **ADOPT for release assets** | **v2.18.3** | 2026-10-08 | MIT | Archives, checksums, SBOM, cosign signing, changelog; `before.hooks` can run `pnpm install && pnpm build` before `go build` (hook doc covers `dir`, `env`, fail-on-error; pnpm itself is not documented there, verify in the pipeline). `dockers_v2` (since v2.12; buildx multi-arch; provisional name, becomes `dockers` in GoReleaser v3) is optional. https://goreleaser.com/customization/dockers_v2/ |
| `github.com/google/ko` | **ALT** | v0.19.1 | 2026-06-29 | Apache-2.0 | Daemonless, SBOM by default, fast multi-arch. Cannot run the pnpm build, so `web/dist` must exist first. **Default base image is `cgr.dev/chainguard/static`** (override with `defaultBaseImage`/`KO_DEFAULTBASEIMAGE`); Chainguard's free tier is `:latest`-only per its 2025 announcement and digest pulls get no updates (that source is undated; verify), so override to distroless. https://ko.build/configuration/ |
| Image base | **ADOPT** | `gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3` (index digest observed 2026-10-13) | - | Apache-2.0 | The distroless README lists only **Debian 13 (trixie)** images, tags `latest|nonroot|debug|debug-nonroot`, archs amd64, arm64, arm, s390x, ppc64le, riscv64, "All distroless images are signed by cosign with ephemeral keys (keyless)", built with Bazel, and warns that unqualified names will change, so always use the `-debian13` suffix. Verify with `cosign verify ... --certificate-identity keyless@distroless.iam.gserviceaccount.com`. https://github.com/GoogleContainerTools/distroless , digest via `gcr.io/v2/distroless/static-debian13/manifests/nonroot` |
| Dependency updates | **ADOPT Renovate** (hosted app or self-hosted) | `renovatebot/renovate` active (push 2026-10-13) | - | AGPL-3.0 (service; hosted Mend app avoids running it) | gomod manager supports the **`tool` directive** (depType `tool`), `gomodUpdateImportPaths` (migrates import paths on major bumps: essential for go-github monthly majors, "most major Go upgrades won't be immediately mergeable"), `gomodTidy`, `constraintsFiltering: "strict"`, does not bump the `go` directive unless configured. **Dependabot gomod**: version and security updates supported, private registries unsupported; **tool-directive support is an open issue (dependabot-core #13655, opened 2025-11-27)** and "Major version updates for Go modules" is open since 2018 (#2213). https://docs.renovatebot.com/modules/manager/gomod/ , https://docs.github.com/en/code-security/dependabot/ecosystems-supported-by-dependabot/supported-ecosystems-and-repositories |

**GitHub Actions to pin (by SHA at implementation; latest tags seen 2026-10-13):** `actions/checkout` v7.0.1, `actions/setup-go` v7.0.0 (use `go-version-file: go.mod`), `golangci/golangci-lint-action` v9.3.0, `goreleaser/goreleaser-action` v7.3.0, `docker/build-push-action` v7.4.0, `sigstore/cosign-installer` v4.1.2, `actions/setup-node` v7.1.0, `pnpm/action-setup` v6.1.0, `golang/govulncheck-action` v1.1.0, `github/codeql-action` v4.38.3, `anchore/sbom-action` v0.24.3 (all `[gh <owner>/<repo>] /releases`). SHAs were not captured here.

**GitHub rate-limit facts relevant to the sync engine** (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api and https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api, 2026-10-13): App installation baseline 5,000 req/h, +50/h per repo beyond 20 repos and +50/h per user beyond 20 users, capped at 12,500 (15,000 on Enterprise Cloud); secondary limits: 100 concurrent requests, 900 points/min per REST endpoint (GET 1 point, writes 5), 90 s CPU per 60 s; **"a 304 does not count against your primary rate limit" when the request carried an `Authorization` header**; GitHub says to prefer webhooks, make requests serially, honour `Retry-After`, else `x-ratelimit-reset`, else wait at least a minute. This supports Q-09 (5,000/h target, webhook-first, conditional refresh) and the per-upstream concurrency of 1-4 in D-03.

### 3.10 Adjacent pointer (belongs to the API-contract research, not decided here)

D-16 mandates OpenAPI 3.1 contract-first. Verified so the later research starts from facts: `oapi-codegen/v2` **v2.8.0** (2026-07-17, Apache-2.0) states support for OpenAPI 3.0 **and 3.1**, strict-server generation, and a `std-http-server` target (Go 1.24+); `kin-openapi` v0.150.0 (2026-10-13) and `pb33f/libopenapi` v0.41.6 (2026-10-10, 3.0/3.1/3.2) parse and validate; `danielgtaylor/huma/v2` v2.39.1 (2026-07-29) is code-first (contradicts D-16's contract-first). [proxy] plus READMEs on GitHub.

## 4. Library-by-library notes on items the brief named

- **go-github current major**: v92 (2026-09-14). Reading the v92 release notes shows the typical breakage is request-body types being renamed and passed by value; compile errors, not silent behaviour changes.
- **ghinstallation**: see 3.1. If chosen, accept the extra go-github/v88 in the binary, or avoid by using `NewAppsTransport` only.
- **x/oauth2**: v0.37.0; needed because go-githubauth returns `oauth2.TokenSource`; `oauth2.NewClient` handles reuse and refresh.
- **x/sync**: only `errgroup` (lifecycle), `semaphore` (bulkhead), `singleflight` (dedupe of identical in-flight fetches by `org/repo/kind/sha`).
- **x/time/rate**: one limiter per upstream sized from the observed `x-ratelimit-*` headers.
- **templ vs html/template**: moot after D-16; use `html/template` for the three small server-side templates.
- **htmx**: moot after D-16 (4.0.0 and 2.0.10 recorded above).
- **goldmark/chroma/bluemonday**: section 3.3.
- **Config/YAML/JSON Schema**: sections 3.4 and 3.5.
- **OpenTelemetry**: section 3.6; Logs stable since 2026-10-02.
- **SQLite drivers**: section 3.7.
- **otter v2**: section 3.7.
- **Testing helpers**: section 3.8.
- **Linters, govulncheck, GoReleaser/ko, distroless, Renovate**: section 3.9.

## 5. Recommended first `go.mod` (all verified to resolve)

```go
module github.com/cantara/source-code-portal

go 1.27.2

require (
	github.com/alecthomas/chroma/v2 v2.27.0                 // explicit: goldmark-highlighting/v2 would otherwise select v2.2.0
	github.com/caarlos0/env/v11 v11.4.1
	github.com/cenkalti/backoff/v7 v7.0.1
	github.com/google/go-cmp v0.7.0
	github.com/google/go-github/v92 v92.0.0
	github.com/invopop/jsonschema v0.14.0
	github.com/jferrl/go-githubauth v1.9.1
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/prometheus/client_golang v1.25.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3         // tests and lint only
	github.com/sony/gobreaker/v2 v2.4.0
	github.com/yuin/goldmark v1.8.6
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc // optional; own renderer is an alternative
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.72.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/exporters/prometheus v0.69.0
	go.opentelemetry.io/otel/sdk v1.47.0
	go.opentelemetry.io/otel/sdk/metric v1.47.0
	go.uber.org/goleak v1.3.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/net v0.61.0                                  // >= v0.61.0 keeps govulncheck clean for bluemonday
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
	golang.org/x/time v0.16.0
	modernc.org/libc v1.77.1                                  // must match modernc.org/sqlite's own pin
	modernc.org/sqlite v1.60.1
)

// optional / later
//   github.com/maypok86/otter/v2 v2.3.0
//   github.com/rogpeppe/go-internal v1.16.0   (testscript)
//   go.etcd.io/bbolt v1.5.0                   (only if SQLite is rejected)
//   golang.org/x/mod v0.42.0                  (go.mod parsing for manifests, D-15)

// tools (light ones only; golangci-lint and goreleaser are installed as pinned binaries)
tool golang.org/x/vuln/cmd/govulncheck
```

Tool binaries pinned outside `go.mod`: `golangci-lint v2.14.0`, `goreleaser v2.18.3`; `oapi-codegen v2.8.0` pending the API research (consider a `tools/go.mod` used via `go tool -modfile=tools/go.mod` so tool dependencies cannot influence the product's MVS; the Go docs state tool requirements "participate in minimal version selection", https://go.dev/doc/modules/managing-dependencies#tools).

Observed resolution details worth keeping: `go mod tidy` initially picked `prometheus/client_golang v1.24.1`, `x/net v0.59.0`, `x/text v0.42.0` and `chroma v2.2.0`; the pins above are explicit upgrades. `gopkg.in/yaml.v3 v3.0.1`, `go.yaml.in/yaml/v2 v2.4.4` and `go.yaml.in/yaml/v4 v4.0.0-rc.2` still appeared indirectly in `go list -m all` (104 modules total in the scratch build); the lone direct YAML import is `go.yaml.in/yaml/v3`.

## 6. Architecture-relevant recommendations that fall out of the research

1. **`internal/github` owns every go-github type** (D-04) and exposes consumer-side interfaces; this is the blast-radius control for monthly go-github majors.
2. **Conditional requests are an adapter concern**: send `If-None-Match`/`If-Modified-Since` from the persisted `etag`, treat a 304 `*ErrorResponse` as "unchanged"; 304s are free against the primary limit when authenticated.
3. **Webhook verification is ours** (SHA-256 only, constant time, bounded body, delivery-id dedupe); go-github only parses typed events.
4. **Test time with `synctest`** (GA, plus `httptest.NewTestServer`); add `goleak` in `TestMain` of every package that starts goroutines.
5. **Markdown pipeline is a replaceable package** (`internal/docs/render`): goldmark v1 now, v2 spike later; sanitiser and chroma are separate steps; sanitisation runs in the sync worker with a deadline and is fuzzed.
6. **JSON strictness**: decode config with strict YAML; consider `encoding/json/v2` for webhook and manifest inputs where duplicate keys must be rejected.
7. **Container**: set `GOMEMLIMIT` and a CPU limit; GOMAXPROCS follows the cgroup automatically on Go 1.25+.
8. **SQLite DSN**: enable `StrictPragmas` and put WAL/`busy_timeout` in code, not in an externally supplied DSN.

## 7. Corrections to earlier assumptions

| Topic | Earlier assumption | What the web shows (2026-10-13) |
|---|---|---|
| `encoding/json` choice | "`encoding/json/v2` is still experimental in the Go versions I know; do not depend on it." | **GA in Go 1.27 (2026-08-19)** as ordinary packages; `encoding/json` itself is now backed by the v2 engine (faster unmarshal). Still: keep the v1 API as the default; v2 is optional. https://go.dev/doc/go1.27 |
| Container-aware `GOMAXPROCS` | "container-aware `GOMAXPROCS` from Go 1.25 (verify; earlier versions need `automaxprocs`)" | Verified: Go 1.25, cgroup CPU *limit* aware, dynamic updates; ignores CPU *requests*; `GOMEMLIMIT` is **not** automatic. https://go.dev/doc/go1.25 |
| Retry/backoff library; task brief | "`cenkalti/backoff` v5" | Current is **v7.0.1** (v6.0.0 2026-06-16, v7.0.0 2026-06-30, v7.0.1 2026-09-30); both v6 and v7 changed the error and `RetryAfter` APIs. https://github.com/cenkalti/backoff/blob/master/CHANGELOG.md |
| Alternative SQLite driver | "`ncruces/go-sqlite3` is a wazero-based alternative" | Switched to **wasm2go on 2026-03-12** (commit "Switch from wazero to wasm2go (#362)"); `go.mod` has no wazero. |
| GitHub client stack | go-github "(current major)" with `ghinstallation` and `gofri/go-github-ratelimit` | Current major **v92** (monthly majors); `ghinstallation` v2.19.0 requires **go-github/v88** and jwt v4 (second go-github copy in the binary); `go-github-ratelimit` is **dormant since 2025-03**; go-github's README now also documents `jferrl/go-githubauth`. |
| Conditional requests, D-04 | go-github ETag handled by "ETag/conditional request transport" | go-github has **no** 304 handling (non-2xx including 304 returns `*ErrorResponse`); `ValidatePayload` **falls back to SHA-1** and skips validation without a secret, contrary to Q-24/D-09. |
| Linting, D-10, D-11 | "`staticcheck`/`golangci-lint`" as separate tools | golangci-lint v2 bundles staticcheck (merged `gosimple`+`stylecheck`); standalone staticcheck needs 2026.2.2 for Go 1.27.2; golangci-lint docs advise **against** `go tool` installation. |
| Dependency updates | "Renovate or Dependabot for `gomod`" | Prefer **Renovate**: Dependabot gomod lacks `tool` directive support (open issue since 2025-11-27) and long-standing Go major-version gaps; Renovate has `gomodUpdateImportPaths`. |
| Markdown renderer, D-06 | "`yuin/goldmark` with GFM extension... goldmark-highlighting" | goldmark **v2** exists (v2.1.6, 2026-09-27) but its ecosystem is split (highlighting/v3 needs chroma/v3 alpha and Go 1.27; GitHub-alerts extension is v2-only). Stay on v1.8.6 for now. |
| HTML sanitiser, D-06 | bluemonday presented as the sanitiser | Works but is slow-moving: no release since 2024-07-04, open CSS-handler DoS reports (#225, #227). Constrain use (no `style`). |
| Container base image, D-11 | "`gcr.io/distroless/static:nonroot`" | The distroless README now lists **only `-debian13` images**; use `gcr.io/distroless/static-debian13:nonroot` by digest (unqualified names will change). |
| 04 D-02 "Confirm driver/version in research" | open | Confirmed: `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4); note the `modernc.org/libc` pin coupling and that CHANGELOG lists v1.61.0 not yet on the proxy. |
| 04 D-05 "(or templ if research favours it)" | open | templ is v0.3.1070 (pre-1.0) and irrelevant after D-16; stdlib `html/template` for the few server templates. |
| 04 D-07 "YAML" | library unspecified | `gopkg.in/yaml.v3`/`go-yaml/yaml` is **archived**; use `go.yaml.in/yaml/v3`. |
| 04 D-08 | "OpenTelemetry metrics+traces" | OTel **Logs** are now Stable as well (2026-10-02); the Prometheus exporter, `otelhttp`, `otelslog` remain 0.x. |
| 04 D-10 `goleak` | no version | v1.3.0 (last tag 2023-10) is still the pin and works on Go 1.27.1 (tested); expect to bump to the next tag when it ships. |
| In-process cache library | `maypok86/otter` v2 | v2.3.0 is the latest tag (2025-12-22); no release in 10 months while `main` is active. Still optional. |

## 8. Risks

1. **go-github major churn** (six majors in four months): version bumps are routine work; budget a quarterly bump task, keep the adapter thin, use Renovate import-path migration.
2. **go-githubauth maturity** (34 stars, one primary author, rapid releases): isolated behind `TokenSource`; fallback to ghinstallation or a 100-line own implementation on jwt/v5.
3. **bluemonday stagnation and DoS reports**: restrict policy, bound input and time, fuzz, and keep a fallback sanitiser plan; watch PR #227 and issue #225.
4. **goldmark v2 transition**: v1 maintained "up to one major prior"; a future v3 would end v1 support. Isolated renderer package and an upstream LLM migration plugin lower the cost.
5. **OTel 0.x modules** (Prometheus exporter, otelhttp, otelslog, runtime): breaking changes can land in minor versions; confine to `internal/platform/telemetry`. OTel Logs stable status is 11 days old.
6. **modernc SQLite**: transpiled code is a distinct bug class (they say so in SECURITY.md); `modernc.org/libc` must match; single-person project with sponsors. Mitigation: one narrow `Store` interface, SQLite is rebuildable cache of GitHub (Q-10), ncruces as a drop-in at the `database/sql` level.
7. **Go 1.27 `json` engine switch**: error text changes; `GOEXPERIMENT=nojsonv2` opt-out will be removed. Do not assert on `encoding/json` error strings in tests.
8. **Tool licences**: `golangci-lint` and `depguard` are GPL-3.0 (tools only, not linked); Renovate is AGPL-3.0 (use hosted or self-host unmodified). Confirm with the owner's policy; nothing GPL/AGPL is in the shipped binary.
9. **Version skew traps**: `goldmark-highlighting/v2` drags chroma v2.2.0; `ghinstallation` drags go-github v88; `go mod tidy` picks old `x/net`; always add explicit `go get` pins and check with `go list -m all` and `govulncheck`.
10. **Distroless/ko base-image policy** (Chainguard free-tier tag policy) can change: pin by digest and keep Renovate's docker manager enabled.
11. **GitHub API version header** moves from 2022-11-28 (end of support 2028-03-10) to 2026-03-10 eventually; breaking-change list not reviewed here.
12. **Binary size**: the scratch build was 18.1 MiB stripped with go-github, modernc SQLite, OTel, goldmark/chroma; acceptable, but go-github alone is large; only the SBOM/size budget in the architecture doc should decide if it matters.

## 9. Empirical checks run on 2026-10-13 (Go 1.27.1 on darwin/arm64, scratch directories only)

| Check | Result |
|---|---|
| Scratch module (`go 1.27.2`) importing go-github v92, go-githubauth, x/oauth2, gobreaker/v2, errgroup/semaphore/singleflight, x/time/rate, backoff/v7, goldmark v1, goldmark-highlighting/v2, bluemonday, yaml/v3, caarlos0/env/v11, invopop/jsonschema, santhosh jsonschema/v6, otter/v2, otelhttp, otel prometheus exporter, otel sdk/metric, modernc sqlite | `go mod tidy` succeeded; after explicit `go get` of chroma v2.27.0, client_golang v1.25.0, x/net v0.61.0, x/text v0.43.0 the build with `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` succeeded: **18.1 MiB**; 104 modules in `go list -m all`. (`go vet` flagged lock copies in my throwaway `main.go`, not in libraries.) |
| `govulncheck v1.9.0 -mode=binary` on that binary | "No vulnerabilities found." (DB updated 2026-10-08) |
| `govulncheck v1.9.0 ./...` on an empty `go 1.27` module under Go 1.27.1 | "13 vulnerabilities in modules you require, but your code doesn't appear to call these" (stdlib, fixed by 1.27.2) |
| goleak v1.3.0 under Go 1.27.1 with `pprof.Do` labelled goroutines | Clean case passes; leaked labelled goroutine detected and reported with `{feature: sync}` label. |
| invopop/jsonschema v0.14.0 with `FieldNameTag:"yaml"` on a config struct | Produced a 2020-12 schema with `required`, `enum`, `description`, `default`, nested object, `additionalProperties:false`; santhosh v6.0.3 accepted a valid YAML->JSON document and rejected one with an unknown key; `yaml.v3 KnownFields(true)` rejected unknown key with line number. |
| Container digest | `gcr.io/distroless/static-debian13:nonroot` index digest `sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3` returned by the registry on 2026-10-13. |

## 10. Version table (quick reference, pins as of 2026-10-13)

| Item | Pin | Date |
|---|---|---|
| Go | 1.27.2 (1.26.9 fallback) | 2026-10-08 |
| go-github | v92.0.0 | 2026-09-14 |
| go-githubauth | v1.9.1 | 2026-09-19 |
| ghinstallation (alt) | v2.19.0 | 2026-06-11 |
| x/oauth2, x/sync, x/time, x/net, x/text, x/mod | v0.37.0, v0.24.0, v0.16.0, v0.61.0, v0.43.0, v0.42.0 | 2026-08-25, 09-23, 08-19, 10-09, 10-09, 10-09 |
| gobreaker/v2 / failsafe-go (alt) | v2.4.0 / v0.9.8 | 2026-01-01 / 2026-09-30 |
| backoff | v7.0.1 | 2026-09-30 |
| goldmark / chroma / highlighting / bluemonday | v1.8.6 / v2.27.0 / v2.0.0-20230729... / v1.0.27 | 2026-09-03 / 06-17 / 2023-07-29 / 2024-07-04 |
| yaml | go.yaml.in/yaml/v3 v3.0.5 (v4 rc.6) | 2026-07-26 (2026-06-17) |
| env / koanf (alt) | v11.4.1 / v2.3.8 | 2026-05-01 / 2026-10-07 |
| invopop/jsonschema / tekuri v6 / google jsonschema-go (alt) | v0.14.0 / v6.0.3 / v0.4.3 | 2026-04-23 / 06-28 / 04-17 |
| otel / sdk / metric / log | v1.47.0 | 2026-10-02 |
| otel prometheus exporter / otelhttp / otelslog / runtime | v0.69.0 / v0.72.0 / v0.21.0 / v0.72.0 | 2026-10-02 |
| prometheus/client_golang | v1.25.0 | 2026-10-08 |
| modernc sqlite (libc) / ncruces (alt) / bbolt (alt) / otter (opt) | v1.60.1 (v1.77.1) / v0.35.6 / v1.5.0 / v2.3.0 | 2026-09-29 / 09-23 / 2026-06-03 / 2025-12-22 |
| go-cmp / testify (alt) / goleak / testscript / go-vcr (alt) | v0.7.0 / v1.12.1 / v1.3.0 / go-internal v1.16.0 / v4.0.7 | 2025-01-14 / 2026-08-17 / 2023-10-24 / 2026-07-01 / 2026-06-25 |
| golangci-lint / staticcheck (bundled) / govulncheck | v2.14.0 / 2026.2.2 / v1.9.0 | 2026-09-24 / 10-12 / 10-11 |
| GoReleaser / ko (alt) | v2.18.3 / v0.19.1 | 2026-10-08 / 2026-06-29 |
| distroless | `static-debian13:nonroot@sha256:e2e927ec...` | 2026-10-13 |

## 11. What to do next in the design phase

1. Put the pins in section 5 into the scaffold and commit `go.sum`; add the Renovate config with `gomodUpdateImportPaths`, `constraintsFiltering: "strict"`, grouped OTel and x/* updates, and a manual-approval rule for go-github majors.
2. Add the import-boundary test and `depguard` rules in milestone 0, before features exist.
3. Prototype the `internal/github` adapter against a fake server: App auth via `TokenSource`, ETag store, 304 handling, rate-limit pause, SHA-256 webhook verification.
4. Decide SQLite driver by a 1-day spike (modernc vs ncruces) on the real schema with WAL and a crash/restart test; default stays modernc.
5. Schedule a goldmark v2 spike for 2027-Q1 and a Go 1.28 review (Feb 2027).

## 12. Unverified (could not be confirmed, or not tested)

- **modernc.org/sqlite v1.61.0**: appears in the upstream CHANGELOG (dated 2026-09-30) but `proxy.golang.org` returned "unknown revision v1.61.0" on 2026-10-13; pinned v1.60.1 instead.
- **ncruces vs modernc performance**: only the modernc author's own benchmark page (macOS M1, noisy) was read; no independent benchmark of SCP's workload was run. WAL behaviour of ncruces inside Linux containers is documented (mmap shared memory) but untested here.
- **go-arch-lint and arch-go** expressiveness for "N features, no cross-imports" was not tested; recommendation relies on an own import test plus `depguard`.
- **Chainguard free-tier tag policy**: the only source found was an undated vendor blog post surfaced by search; treat as unconfirmed.
- **`go-githubauth` token-refresh edge cases and GHES specifics** were read from its README, not exercised against a GitHub App.
- **caarlos0/env `,file`** is documented in its README (lines for `,file`), not run-tested here.
- **failsafe-go** was assessed from its README, release cadence and version number only; API ergonomics not tried.
- **Renovate hosting model/limits** (hosted Mend app vs self-hosted) and **Dependabot's actual behaviour on Go major bumps** were inferred from docs and open issues (#13655, #2213), not from running them.
- **GitHub REST `2026-03-10` breaking changes** were not read.
- **HTMX 4 licence text** (GitHub reports NOASSERTION; the LICENSE header read "Zero-Clause BSD").
- **Go 1.25 end of support** is derived from the release-policy rule and the 1.27 release, not from an explicit statement on the release page.
- **GitHub Actions SHAs** for pinning were not captured, only tag names.
- **Whether `encoding/json/v2` API is covered by an explicit stability statement beyond the 1.27 release notes** (the notes say the `encoding/json` v1 API "will continue to be supported" and list removals made during the experiment); I treated it as GA but did not find a separate compatibility guarantee text for `json/v2`.
- **OpenTelemetry Prometheus exporter and contrib instrumentation stability**: read from `versions.yaml` module sets (0.x "experimental-*"), not from a prose stability statement.
