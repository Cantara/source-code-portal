# 20-design / 14 - Integrations and access control

Unit: `14-integrations-access`. Packages: `internal/integrations`, `internal/integrations/jenkins`, `internal/integrations/snyk`, `internal/access`.
Date: 2026-10-14. Status: design for implementation. Conforms to `04-decisions.md` (incl. section 7), `10-architecture.md` (revision 2) and ADR-023, ADR-024, ADR-025, ADR-011, ADR-015.

Companion code: `14-integrations-access.assets/` holds a **compile-checked and tested reference implementation** of everything in this document that is code (Go 1.27.1, `go vet` clean, `go test -race` clean, `FuzzIdentify` and `FuzzParseJobPath` run 10 s each on 2026-10-14; **re-verified and corrected on 2026-10-14 by an independent verifier: 22 findings fixed, see the Verification log at the end and `30-verification/design-14-integrations-access.md`**). The architecture pins Go 1.27.2; the scratch module used 1.27.1 (the installed toolchain); nothing here depends on the patch level. The document is self-sufficient (every rule, signature and algorithm is stated here); the assets are the executable form of the same text. Where this document says "reference implementation", it means those files. Section 12 lists which tests exist there and which must still be written.

What was compile-checked: the complete Go surface of this unit (types, interfaces, constructors, sweep, providers, access middleware) in a scratch module that stubs the other units' packages with the signatures of `10-architecture.md` section 8 (`scm`, `freshness`, `job`, `readmodel` minimal semantics, `store`, `platform/secret`, `httpapi/authz`). Two stub details are **not** architecture signatures and are marked as such in the assets README: `authz.WithPrincipal` (deviation D-2) and the scratch `readmodel`/`scm` constructors.

---

## 0. Verification log (facts checked at design time)

All fetched on 2026-10-14 unless stated. "Unverified" items are carried into section 15 and have a fallback.

| # | Fact the design depends on | Result | Source |
|---|---|---|---|
| V1 | Snyk regional REST hosts | US-01 `https://api.snyk.io/rest`, US-02 `https://api.us.snyk.io/rest`, EU-01 `https://api.eu.snyk.io/rest`, AU-01 `https://api.au.snyk.io/rest`; (re-fetched 2026-10-14 by the verifier: the four hosts are confirmed). "Snyk tokens are region-specific" is **not stated on the regional-hosting page** (it says only that the region must be configured before authenticating); treat it as a diagnostic hint in the 401 message, not as a documented fact. SNYK-GOV-01 has no listed API URL | docs.snyk.io regional-hosting-and-data-residency; docs.snyk.io about-the-rest-api |
| V2 | Snyk REST `version` parameter | required query parameter `version=YYYY-MM-DD` (optionally `~beta`/`~experimental`); docs recommend `2024-10-15` or the current date; `GET https://api.snyk.io/rest/openapi` lists 324 versions, newest GA `2026-03-25` | docs.snyk.io about-the-rest-api; api.snyk.io/rest/openapi |
| V3 | `GET /orgs/{org_id}/projects` in versions 2024-10-15 and 2026-03-25 (downloaded OpenAPI documents) | same parameters in both: `limit` (default 10, max 100, multipleOf 10), `starting_after`, `expand=target`, `meta.latest_issue_counts` (boolean), `names`, `origins`, `types`, `target_id`; response `data[]{id, attributes{name, origin, status(active|inactive), type}, meta.latest_issue_counts{critical,high,medium,low,updated_at}, relationships.target.data.attributes.display_name}`; `links.next` | `https://api.snyk.io/rest/openapi/2024-10-15`, `.../2026-03-25` (downloaded, parsed) |
| V4 | Snyk auth header | OpenAPI security schemes: `APIToken` (header `Authorization`, "API key value must be prefixed with `Token `") and `BearerAuth`; the design uses `Authorization: Token <token>` | same OpenAPI documents |
| V5 | Snyk target display name format | spec example `snyk-fixtures/goof` (owner/name); the design matches `owner/repo` case-insensitively | OpenAPI `ProjectRelationshipsTarget.display_name` |
| V6 | Snyk REST rate limit | **the REST limit is not published**: the figure "1,620 requests/minute" of the first draft could not be confirmed on 2026-10-14 (Snyk's V1 API page states 2,000/min); only an aggregator mentions `Retry-After`: unverified, the provider defaults to 60 s. The design does not depend on the number: a sweep costs about 30 requests per 15 minutes | web search 2026-10-14 (verifier); docs.snyk.io/snyk-api/make-calls-to-the-snyk-api/snyk-api-rate-limits |
| V7 | Jenkins JSON API `tree` | "tree query parameter ... XPath-ish path expression ... list of property names to include, with sub-properties inside square braces"; arrays use the plural name | jenkinsci/jenkins `core/src/main/resources/hudson/model/Api/index.jelly` |
| V8 | Jenkins auth for scripts | "HTTP BASIC authentication" with a user and API token; "Requests authenticating with an API token are exempt from CSRF protection"; CSRF protection concerns `POST` (a GET needs no crumb) | jenkins.io remote-access-api; jenkins.io security/csrf-protection |
| V9 | Jenkins `Result` values | `SUCCESS`, `UNSTABLE`, `FAILURE`, `NOT_BUILT`, `ABORTED` (`result` is `null` while a build runs) | javadoc.jenkins.io `hudson.model.Result` |
| V10 | Multibranch branch names containing `/` are double-encoded in URLs (`%252F`) | **Unverified in Jenkins documentation**; corroborated by Jenkins issue tracker reports (JENKINS-36154, JENKINS-42390, JENKINS-33660). The provider implements it and M0 verifies it on a real instance. `_class` values containing `MultiBranch` / `Folder` are likewise heuristic until M0 records real fixtures | web search |
| V11 | oauth2-proxy identity headers | `--pass-user-headers`: `X-Forwarded-User, X-Forwarded-Groups, X-Forwarded-Email, X-Forwarded-Preferred-Username`; `--set-xauthrequest`: `X-Auth-Request-User/-Groups/-Email/-Preferred-Username` response headers; `--skip-auth-strip-headers` "strips X-Forwarded-* style authentication headers" that oauth2-proxy would set; `--trusted-ip` bypasses authentication | oauth2-proxy.github.io configuration/overview |
| V12 | OpenTelemetry API modules | `go.opentelemetry.io/otel/{metric,trace}` v1.47.0 resolve from the module proxy (and `sdk/metric` for tests); used by the reference code | `go get` 2026-10-14 |
| V13 | Go toolchain features used by tests | `testing/synctest.Test` and `httptest.NewTestServer` exist in the installed Go 1.27.1 | `go doc` 2026-10-14 |
| V15 | Snyk OpenAPI 2024-10-15, re-downloaded and parsed by the verifier | `GET /orgs/{org_id}/projects` parameters `expand` (array, enum `[target]`, form, explode false), `origins` (array, form, explode false), `meta.latest_issue_counts` (boolean), `limit` (default 10, min 10, max 100, multipleOf 10) confirmed; `LatestIssueCounts` = `{critical, high, low, medium: number, updated_at}`, no description of ignored issues (open item Q-3 stands) | `https://api.snyk.io/rest/openapi/2024-10-15` |
| V16 | oauth2-proxy header names, re-fetched by the verifier | `--pass-user-headers` (default true): `X-Forwarded-User/-Groups/-Email/-Preferred-Username`; `--set-xauthrequest` adds `X-Auth-Request-*` response headers (+ `X-Auth-Request-Access-Token` with `--pass-access-token`); **`--pass-access-token` sends `X-Forwarded-Access-Token`, which the first draft's strip list lacked (fixed)**; `--skip-auth-strip-headers` (default true) strips "X-Forwarded-* style authentication headers & Authorization" | oauth2-proxy.github.io/oauth2-proxy/configuration/overview |

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

Two small, security-relevant concerns that the rest of the product leans on:

1. **Integrations** (Q-05, Q-25): optional, off-by-default status providers (Jenkins build status, Snyk vulnerability counts) behind one `StatusProvider` interface. They turn *structured JSON* from the provider's API into a small, honest, freshness-stamped `Status` per repository, kept in a snapshot slot, refreshed by two global sweep jobs, and consumed by the badges (kinds `build`, `security`), the repository composite API and the llms outputs. No SVG is ever fetched, stored, parsed or proxied (GR-33). Nothing is hard-coded to an organisation (GR-14).
2. **Access** (G-08; Q-03; ADR-023): the optional trusted-proxy identity mode that makes non-public repositories possible at all, implemented as (a) an identity middleware that establishes an `authz.Principal` from headers a trusted SSO proxy sets and defeats spoofing, (b) the `authz.Authorizer` that maps a principal and a repository visibility to a yes/no (and therefore to a `scm.ViewClass`), and (c) the specification of how visibility is enforced across every output surface (API, badges, media, llms, sitemap, composites, caches).

### 1.2 In scope

- `integrations`: config, target resolution (descriptor > group default > instance default), `StatusProvider`/`BulkProvider`/`Checker` contracts, typed provider errors, the status slot value `Statuses`, the two sweep jobs (`integrations.jenkins`, `integrations.snyk`), stale-while-error reduction, persistence of status items, warm-start decoding, `Fact` (what badges need), `Health` (what `/status` needs).
- `integrations/jenkins`: Jenkins JSON API provider: multiple instances, classic and multibranch jobs, Basic auth with an API token, redirect pinning (SSRF/credential defence), response caps, state mapping.
- `integrations/snyk`: Snyk REST provider: one paged project listing per sweep, regional hosts, token, matching by target display name, severity thresholds.
- `access`: config + validation, identity extraction, spoofing defences, `authz.Authorizer` (public-only and trusted-proxy), the operator authorizer for the admin listener, the `Cache-Control: private` response wrapper, metrics and `/status` info.
- The visibility-enforcement contract for all surfaces (section 4.8) and the tests that prove it (section 12). The enforcement *code* of each surface lives in the owning unit; this document fixes the rules and the cross-unit requirements.

### 1.3 Non-goals

- OIDC or any in-process login (Q-03: a documented later extension; the `Principal` abstraction does not change).
- Fine-grained per-repository ACLs. The model is two view classes (public, all); see 4.7.4 for why a finer split is rejected.
- Providers other than Jenkins and Snyk; GitHub Actions/Checks status (would be a third provider behind the same interface; not in 04).
- Shields.io, SVG scraping, third-party badge proxying (Q-05). The Shields kinds are rendered locally by `badges` from GitHub data.
- Webhooks from Jenkins/Snyk (the research mentions none are needed; sweeps poll).
- Rendering badges (owned by `badges`; this unit supplies `Fact`), the repository composite DTO (owned by `overview`), the client-IP resolution and rate limiter (owned by `platform/httpx`), the SSRF dialer (owned by `platform/netguard`).

### 1.4 Directories covered

```
internal/integrations/            config.go provider.go status.go target.go service.go      (+ testdata/jenkins, testdata/snyk, integrations_test.go)
internal/integrations/jenkins/    jenkins.go                                                  (+ jenkins_test.go)
internal/integrations/snyk/       snyk.go                                                     (+ snyk_test.go)
internal/access/                  config.go identity.go authorizer.go access.go               (+ access_test.go)
```

The architecture's tree lists `provider.go service.go jobs.go` for `integrations` and `identity.go authorizer.go` for `access`; this design uses a few more files for readability (`config.go`, `status.go`, `target.go`; `jobs.go` is folded into `service.go`). File names are not binding.

---

## 2. Position in the architecture

### 2.1 Ranks and allowed imports (architecture 5.2, checked with `go list` on the reference code)

| Package | Rank | Internal imports actually used | Third-party actually used | Matrix row says |
|---|---|---|---|---|
| `integrations` | 4 | `freshness`, `job`, `platform/secret`, `readmodel`, `scm`, `store` | otel API: `attribute`, `metric`, `trace`, `trace/noop` | K4 + otel API: **conforms** |
| `integrations/jenkins` | 5 | `integrations`, `scm` | none | `integrations`, `scm`, `freshness`, `platform/secret`: **conforms** |
| `integrations/snyk` | 5 | `integrations` | none | same: **conforms** |
| `access` | 4 | `httpapi/authz`, `platform/secret`, `scm` | otel API: `attribute`, `metric` | row: `scm`, `httpapi`, `httpapi/authz`, `platform/logx`, third-party "none": **needs `platform/secret` and the otel API**, deviation D-1. **Tests** additionally import `otel/sdk/metric` and `metricdata` (an in-memory reader): deviation D-9 |

Rules that follow:

- `integrations` imports no other feature (not `docs` for the descriptor, not `catalog` for groups, not `badges`); it never imports `refresh`, `reactor`, `github`, `httpapi/*` (it serves no route), `platform/{config,httpx,netguard,telemetry}`.
- `jenkins` and `snyk` never import each other or a feature other than their root; they build no transport. They receive an `*http.Client` that `cmd/scp` constructed with `platform/netguard` (connect-time IP policy, `Proxy: nil`, per-instance CIDR/port allow-list) and make a shallow copy to install their **own** redirect policy (host pinning), so the credential-safety rule does not depend on how `cmd/scp` configured the client.
- `access` imports no other feature. It does not import `platform/httpx`: it never uses the client IP resolved by httpx; it reads `r.RemoteAddr` itself (4.7.2). **Cross-unit requirement (found by the verifier):** nothing in the public chain may rewrite `r.RemoteAddr` before `access.Middleware` (chi-style "real IP" middleware does exactly that, and would make every internet client look like the proxy). The reference `httpx.ClientIPResolver` only reads it (checked in `02-platform.assets/.../clientip.go`); `cmd/scp` carries `TestRemoteAddrUntouchedByChain`.
- Nothing imports `access` except `cmd/scp`. Features use `httpapi/authz` (`Principal`, `Authorizer`, `ClassOf`, `LookupRepo`) and never know which authorizer is wired.
- Concurrency rule G10: `integrations` starts goroutines inside a sweep (bounded, joined before the job returns) and `access` starts none. The reference tests contain **no** `goleak` `TestMain`; the sweep tests run inside `testing/synctest` bubbles, which fail on goroutines still blocked at the end of the bubble. The real repository adds `goleak` in `TestMain` of `integrations` and `jenkins` (task I-2.5, section 12.2).

### 2.2 Who imports what

```
cmd/scp               -> access, integrations, integrations/jenkins, integrations/snyk
cmd/scp/internal/bridge -> integrations (roots only): BadgeFacts reads integrations.Service.FactFor; IntegrationOverrides reads docs; StatusHealth reads integrations.Service.Health
overview (rank 5)     -> integrations root (type names Status/Statuses/Fact; method StatusOf, Providers)
```

`badges`, `ops`, `docs`, `catalog` do **not** import `integrations`; they see it through bridged consumer interfaces (2.4).

### 2.3 Interfaces this unit defines (consumer side) and what satisfies them

```go
package integrations

// Contract of an optional status source (architecture 8.9, verbatim shape).
type StatusProvider interface {
    Name() string
    Fetch(ctx context.Context, t Target) (Status, error)
}

// Optional: a provider whose upstream answers many targets in one pass (Snyk). len(results) == len(targets), same order.
type BulkProvider interface {
    StatusProvider
    FetchBulk(ctx context.Context, targets []Target) ([]Result, error)
}

// Optional: `scp doctor`. One cheap authenticated read per instance.
type Checker interface{ Check(ctx context.Context) []CheckResult }

// Bridged (docs descriptor lives in a feature that integrations cannot import).
type Overrides interface {
    IntegrationOverrides(s *readmodel.Snapshot, slug scm.Slug) map[string]map[string]string
}

// Carve-out of store.Store (satisfied directly by store/sqlite and store/memory).
type ItemStore interface {
    PutItem(ctx context.Context, it store.Item) error
    Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
    RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
```

```go
package access   // implements, never defines, the shared contract

// Satisfies authz.Authorizer (architecture 8.14): func (a *authorizer) CanView(ctx, p authz.Principal, v scm.Visibility) bool
// Satisfies the httpx middleware shape: func (a *Access) Middleware(next http.Handler) http.Handler
```

`jenkins.Provider` and `snyk.Provider` satisfy `integrations.StatusProvider` (+ `Checker`; Snyk also `BulkProvider`). `cmd/scp/assert.go` gains:

```go
var (
    _ integrations.StatusProvider = (*jenkins.Provider)(nil)
    _ integrations.Checker        = (*jenkins.Provider)(nil)
    _ integrations.BulkProvider   = (*snyk.Provider)(nil)
    _ integrations.Checker        = (*snyk.Provider)(nil)
    _ integrations.ItemStore      = (store.Store)(nil)          // the carve-out is a subset of store.Store
    _ authz.Authorizer            = (*access.Access)(nil).Authorizer()   // compile-time shape; value assertion lives in access_test
)
```

### 2.4 Interfaces other units must declare to consume this unit (cross-unit contract)

| Consumer | Interface it declares (kernel/stdlib types only unless marked bridged) | Satisfied by |
|---|---|---|
| `badges` | `Facts.Facts(s *readmodel.Snapshot, slug scm.Slug) (badges.RepoFacts, bool)` (already in 8.7). `RepoFacts` must carry two optional facts `Build, Security *struct{Message string; Tone Tone; Stale bool}` with `Tone` a badges-owned enum | `bridge.BadgeFacts` calls `integrations.Service.FactFor(snap, slug, provider, now)` and maps `integrations.Fact` onto badges' own type. `ok == false` from `FactFor` means "provider disabled": the `build`/`security` kinds answer 404 like any unknown kind |
| `overview` (imports the root, rank 5) | none; calls `Service.StatusOf(snap, slug, provider)` and `Service.Providers()` directly | `*integrations.Service` |
| `ops` | `ops.IntegrationHealth interface{ IntegrationHealth() []ops.IntegrationView }` (bridged: ops-owned view type) | `bridge.StatusHealth` over `Service.Health()` and `access.Access.Info()` |
| `docs` | `docs.Config.IntegrationInstances []string` (plain data, filled by `cmd/scp` from `integrations.Config.InstanceNames()`), so the descriptor loader accepts only configured instance names | wiring |
| `catalog` (config validation) | none: `cmd/scp`'s root `Validate()` calls `integrations.Config.ValidateGroupDefaults(catalogGroups)` and `access.Config.AllowsNonPublic()` | wiring |
| `httpapi/authz` | must export `WithPrincipal(ctx, Principal) context.Context` (deviation D-2) | `authz` |

### 2.5 Wiring in `cmd/scp` (illustrative; the lines that matter, not compiled because they use other units' real constructors)

```go
// wire_features.go. The netguard calls follow the signatures of unit 02 (`NewPolicy(allowCIDRs, ports)`, `NewClient(ClientOptions{Policy, Hosts, AllowHTTP, MaxRedirects, Timeout, Wrap})`;
// the first draft of this document used a `netguard.Client(Policy{Name...})` that no unit defines). Unit 02's client never follows redirects
// by default (MaxRedirects 0); the providers copy the client and install their own host-pinned redirect policy, so a redirect to the trailing-slash form works.
// Hosts pins request host (and redirect hops) in netguard as well: the same rule is enforced twice (netguard Hosts, provider CheckRedirect).
acc, err := access.New(cfg.Access, access.Deps{Logger: log, Meter: meter})          // validates; returns every violation
...
var providers []integrations.StatusProvider
if cfg.Integrations.JenkinsEnabled() {
    clients := map[string]*http.Client{}
    for _, in := range cfg.Integrations.Jenkins.Instances {
        pol, err := netguard.NewPolicy(in.AllowPrivateCIDRs, in.Ports)              // unit 02 API (02-platform.assets netguard.go)
        c, err := netguard.NewClient(netguard.ClientOptions{Policy: pol, Hosts: []string{hostOf(in.BaseURL)}, AllowHTTP: in.AllowHTTP,
            Timeout: -1 /* the provider sets its own per-instance timeout on a copy */, Wrap: otelhttpTransport})
        clients[in.Name] = c
    }
    // in.Token is resolved (secret.Secret.Resolve) by the config loader before this point: a pending {file:} secret reveals "".
    p, err := jenkins.New(cfg.Integrations.Jenkins.Instances, clients)
    providers = append(providers, p)
}
if cfg.Integrations.SnykEnabled() {
    pol, err := netguard.NewPolicy(cfg.Integrations.Snyk.AllowPrivateCIDRs, cfg.Integrations.Snyk.Ports)
    c, err := netguard.NewClient(netguard.ClientOptions{Policy: pol, Hosts: []string{hostOf(cfg.Integrations.Snyk.BaseURL)}, Timeout: -1, Wrap: otelhttpTransport})
    p, err := snyk.New(cfg.Integrations.Snyk, c)
    providers = append(providers, p)
}
integ, err := integrations.New(cfg.Integrations, integrations.Deps{ Logger, Meter, Tracer, Publisher: pub, Repos: catalogRepoSlot,
    Membership: catalogMembershipGlobal, Status: statusSlot, Overrides: bridge.IntegrationOverrides(docsSvc), Store: st, Providers: providers })
specs = append(specs, integ.Jobs()...)                       // engine.Register + planner.Track (global scope)
engine.AddUpstream("jenkins", refresh.UpstreamConfig{MaxConcurrent: 1, JobTimeout: 5 * time.Minute})   // sweeps are one global job each
engine.AddUpstream("snyk",    refresh.UpstreamConfig{MaxConcurrent: 1, JobTimeout: 3 * time.Minute})
// wire_http.go: public chain = ... clientIP -> acc.Middleware -> securityHeaders -> rateLimit -> crossOriginProtection -> mux
//               admin chain  = (no access middleware); admin handlers get access.Operator() as their authz.Authorizer
```

The `statusSlot` is registered once: `readmodel.Register[integrations.Statuses](reg, "integrations.status")`.

---

## 3. Domain model and Go types

All types below exist in the reference implementation exactly as shown (comments shortened).

### 3.1 `integrations`: status model

```go
// State is the provider-neutral outcome. The ZERO VALUE IS StateUnknown on purpose: an unset Status never reads as healthy.
// (The architecture lists StateOK first; numeric values are never serialised, String() is, so the order is free.)
type State uint8
const ( StateUnknown State = iota; StateOK; StateFailing; StateUnstable; StateDisabled )
func (s State) String() string // "unknown" "ok" "failing" "unstable" "disabled"

type Target struct {            // what to ask a provider about one repository; never a host name
    Repo     scm.RepoRef
    Instance string             // Jenkins instance name ("" for Snyk)               [added vs 8.9, D-3]
    Job      string             // Jenkins job path "a/b" validated by ParseJobPath
    Project  string             // Snyk target display name "owner/repo" (matched case-insensitively)
    Branch   string             // Jenkins multibranch ref = the repository's resolved default branch   [added, D-3]
}

type Status struct {
    Provider   string           // "jenkins" | "snyk"
    State      State
    Summary    string           // <= 120 runes, generated from a closed vocabulary and numbers; never provider free text
    DetailURL  string           // empty unless the operator exposed it (Jenkins expose_detail_url, Snyk detail_url_template)
    ObservedAt time.Time        // UTC; when SCP observed it (the sweep's completion time)
    Counts     map[string]int   // snyk: critical/high/medium/low (all four keys present); nil for jenkins
}

// Statuses is the slot value (architecture 9.1 "integrations.status"): provider -> freshness-stamped status.
// Disabled providers and repositories without a target are ABSENT. Immutable once Put; writers clone (maps.Clone) first.
type Statuses map[string]freshness.Value[Status]

func Disabled(provider string) Status   // Status{Provider, State: StateDisabled}: what consumers show for an unconfigured provider, never "ok" (GR-41)

type Tone uint8   // ToneNeutral (zero), ToneGood, ToneWarn, ToneBad
func (t Tone) String() string   // "neutral" | "success" | "warning" | "danger": the names unit 09's bridge maps by (an unknown name reads as neutral there: cross-unit contract, TestToneNamesAreTheCrossUnitContract)
type Fact struct{ Provider, Message string; Tone Tone; Stale bool }
func FactOf(v freshness.Value[Status], now time.Time) Fact
```

Invariants: `Summary` never contains text copied from an upstream response except decimal numbers (build number, issue counts) and enum words chosen by this code. `Counts` of a Snyk status always has exactly the four severity keys. `DetailURL`, when non-empty, is built by this code from operator configuration plus validated values, never taken from a response. A `Statuses` value is never mutated after `readmodel.Put`. **Stored bytes are untrusted too:** `decodeStatus` (warm start) drops a `DetailURL` that is not an absolute http(s) URL with a host and no userinfo (the SPA renders it as an anchor; a tampered row must not carry `javascript:`), keeps only the four severity keys of `Counts` clamped to `0..MaxCount` (1,000,000), and never yields `StateDisabled` (a read-time value only). `MaxCount` is also the saturation value for upstream counts: a count above it **saturates, it never wraps to 0** (a count that reads as "no issues" fails open; the first draft's `clamp` returned 0 above 1e6).

### 3.2 `integrations`: provider contract types and errors

```go
type Result struct{ Status Status; Err error }
type CheckResult struct{ Instance string; Err error }

var (
    ErrNoData   // "upstream answered, knows nothing about the target" (Snyk: not monitored). Wraps scm.ErrNotFound.
    ErrProtocol // the answer is not what the provider API promises (content type, JSON, HTTP 4xx other than the mapped ones)
    ErrTooLarge // response exceeded max_response_bytes
    ErrAborted  // target not attempted: its instance's sweep was aborted
)

// Satisfies job.RateLimited structurally (RetryAt). The engine pauses the upstream ("jenkins"/"snyk") until At.
type RateLimitError struct{ Provider string; At time.Time; Err error }
// Satisfies job.Permanent structurally. Credentials rejected, organisation unknown, configuration wrong.
type PermanentError struct{ Provider, Code string /* "auth" | "config" | "invalid_snapshot" */; Err error }

func ErrCode(err error) string   // stable machine code: rate_limited, auth, config, no_data, forbidden, not_found, protocol, too_large, aborted, timeout, transient
```

`scm.ErrForbidden` is the per-target "HTTP 403 for this job" (Jenkins folder or matrix permissions); code `forbidden`. Only a run of `abort_after` of them in a row becomes a `PermanentError{auth}` for the instance (4.2.1).

Why separate error types: `job.AsRateLimited` and `job.IsPermanent` use `errors.As` against behaviour interfaces; one type with both methods would make **every** provider error rate-limited or permanent. A plain wrapped error is "transient" by the engine's definition (architecture 8.3). A test (`TestTransientErrorHasNoBehaviour`) pins this.

### 3.3 `integrations`: service

```go
type Deps struct {
    Logger     *slog.Logger
    Meter      metric.Meter
    Tracer     trace.Tracer                                      // nil => no-op
    Publisher  *readmodel.Publisher
    Repos      readmodel.Slot[freshness.Value[scm.Repository]]   // catalog.repo (existence slot)
    Membership readmodel.Global[scm.Membership]                  // catalog.membership
    Status     readmodel.Slot[Statuses]                          // integrations.status
    Overrides  Overrides                                         // bridged over the docs descriptor; may be nil
    Store      ItemStore
    Providers  []StatusProvider                                  // exactly the enabled ones
}
type Service struct{ /* unexported */ }
func New(cfg Config, d Deps) (*Service, error)            // applies cfg.WithDefaults(), Validate()s it; fails if Deps.Publisher is nil or the provider set != the enabled set
func (s *Service) Jobs() []job.Spec                       // one global spec per enabled provider
func (s *Service) Sweep(ctx context.Context, provider string) (job.Outcome, error)   // the body of the job
func (s *Service) StatusOf(snap *readmodel.Snapshot, slug scm.Slug, provider string) freshness.Value[Status]  // always returns a value
func (s *Service) FactFor(snap *readmodel.Snapshot, slug scm.Slug, provider string, now time.Time) (Fact, bool)
func (s *Service) Providers() []string                    // enabled names, stable order (jenkins, snyk)
func (s *Service) Health() []Health                       // for /status: {Provider, Enabled, Targets, Invalid, Probed, Failed, FailedCode, LastSweep, LastSuccess, LastError, ConsecutiveFailures}
func (s *Service) WarmItem(b *readmodel.Builder, id readmodel.EntryID, it store.Item) error
func ResolveJenkins(cfg JenkinsConfig, snap *readmodel.Snapshot, repos readmodel.Slot[...], mem scm.Membership, ov Overrides) ([]Target, []InvalidTarget)
func ResolveSnyk(snap *readmodel.Snapshot, repos readmodel.Slot[...], mem scm.Membership, ov Overrides) ([]Target, []InvalidTarget)
func ParseJobPath(s string) (JobPath, error); func (p JobPath) URLPath() string
```

**Per-instance defaults.** A YAML list element cannot carry an embedded default, so `Config.WithDefaults()` (copy; the caller's slice is not mutated) fills `discover: declared`, `multibranch: auto`, `timeout: 10s`, `max_response_bytes: 1 MiB` per Jenkins instance. `Validate()` and `New()` call it themselves; the loader need not (the first draft left the call point unspecified and `New` failed with "discover must be declared or all" for an omitted key).

`StatusOf` semantics (read path, no I/O): provider not enabled -> `{V: Disabled(p), Info: {State: never}}`; enabled but no entry for the repo -> `{V: Status{Provider, StateUnknown}, Info: {State: never}}`; otherwise the stored value. `FactFor` returns `ok == false` only when the provider is disabled.

### 3.4 `access`

```go
type Mode string; const ( ModePublic Mode = "public"; ModeTrustedProxy Mode = "trusted-proxy" )
type Config struct { Mode Mode; TrustedProxies []string; AllowPublicProxies bool; Identity Identity; ViewAll ViewAll }
type Identity struct { UserHeader, EmailHeader, GroupsHeader, GroupsSeparator string; MaxGroups int; ProxySecretHeader string; ProxySecret secret.Secret }
type ViewAll struct{ Groups, Subjects []string }
func DefaultConfig() Config
func (c Config) Validate() error; func (c Config) AllowsNonPublic() bool

type Deps struct{ Logger *slog.Logger; Meter metric.Meter }
type Access struct{ /* unexported */ }
func New(cfg Config, d Deps) (*Access, error)
func (a *Access) Middleware(next http.Handler) http.Handler       // PUBLIC chain only
func (a *Access) Authorizer() authz.Authorizer                    // public-only in mode public; view-class authorizer in trusted-proxy
func Operator() authz.Authorizer                                  // admin listener: allows everything
func (a *Access) Info() Info                                      // /status: mode, ranges, drops, warnings
func SubjectHash(subject string) string                           // 8 hex chars of SHA-256: the only form of a subject allowed in logs
type Reason string  // ReasonUntrustedPeer "untrusted_peer", ReasonBadSecret "bad_secret", ReasonDuplicate "duplicate", ReasonMalformed "malformed", ReasonModePublic "mode_public"
```

`authz.Principal{Subject string; Groups []string; Anonymous bool}` is the architecture's type. **Invariant owned by this unit**: a principal is authenticated iff `Subject != "" && !Anonymous`. The zero `Principal{}` is **not** authenticated (the Go zero value has `Anonymous == false`; every check here also requires a non-empty subject), and `authz.PrincipalFrom` must return `Principal{Anonymous: true}` when the context carries none.

---
## 4. Behavioural specification

### 4.1 Target resolution

Input: the current snapshot (`catalog.repo` slot for every repository, `catalog.membership` global), the config, the descriptor overrides (bridged). Output: `[]Target` sorted by slug and `[]InvalidTarget{Slug, Reason}`. Pure and deterministic: no map iteration order, no I/O, no clock (property test `TestResolveIndependentOfMembershipOrder`; GR-08: which group supplies a default never depends on iteration order).

**Repository filter.** Archived repositories get no target (no builds will happen; saves calls). Forks are not filtered (a fork may have its own pipeline). A repository without a `catalog.repo` value is skipped.

**Merge order per key** (architecture 8.1 `GroupMeta.Integrations`): (1) the repository descriptor block (`scp.yaml` `integrations`, via `Overrides`), (2) the **first group in configuration order** (`Membership.Groups()` order, intersected with `Membership.GroupsOf(slug)`) that defines the key, (3) the instance default. Never membership or map order.

**Jenkins** (keys: `instance`, `job` from the descriptor; `instance`, `job_prefix` from groups):

```
instance := first non-empty of descriptor.instance, group.instance, the default instance (single instance, or default: true)
if instance is not configured                       -> InvalidTarget("unknown jenkins instance")          [only when the repo asked for an instance]
if descriptor.job != ""                             -> path := descriptor.job
else if some group defines job_prefix (even "")     -> path := prefix + repo.Name
else if instance.discover == "all"                  -> path := instance.job_prefix + repo.Name             // blind probing, opt-in only
else                                                -> no target ("declared" mode, the default: no blind probing)
ParseJobPath(path) fails                            -> InvalidTarget("invalid jenkins job path")           // repo-authored value: re-validated here
Target{Repo, Instance, Job: path, Branch: repo.DefaultBranch}
```

`ParseJobPath` (`FuzzParseJobPath`): non-empty, at most 300 bytes; `/`-separated segments; every segment 1..100 bytes, not `.` or `..`, no leading/trailing space, characters only `[A-Za-z0-9._ -]`. No `%`, no `?`, no `#`, no `\`, no control or non-ASCII characters, no empty segment, no leading/trailing `/`. `URLPath()` renders `"/job/<escaped seg>/job/<escaped seg>"` with `url.PathEscape` per segment. Invariant (fuzzed): the rendered path contains exactly two `/` per segment, no `/./`, `/../`, `//`, `%2F`. Because every segment is validated and escaped, no repo-controlled byte can change the URL structure (GR-15).

**Snyk** (keys: `project` from the descriptor, pattern `^[A-Za-z0-9._-]+$` without `/`; `project_prefix` from groups): every non-archived repository is a candidate (the sweep is one bulk call, blind matching is free). `Project = repo.Owner + "/" + name` where `name` is the descriptor `project`, else `prefix + repo.Name` when a group defines `project_prefix`, else `repo.Name`. Invalid values -> `InvalidTarget`. The match against Snyk is by the target's `display_name` (V5), case-insensitive.

**Invalid targets** are logged once per sweep (`warn`, repo slug and a fixed reason; never the offending value, which is repository-authored text), counted in `scp_integrations_targets{result="invalid"}` and shown in `/status`. They never make a sweep fail.

**Group-level validation at startup** (`Config.ValidateGroupDefaults(groups)`, called from the root `Validate()` in `cmd/scp`): provider key must be `jenkins` or `snyk` and that provider must be configured (a setting that would be ignored is an error, GR-12); jenkins keys `job_prefix` (valid path prefix, may be empty, may end with `/` for a folder) and `instance` (must exist); snyk key `project_prefix`; any other key is an error. `project_prefix` has exactly one effect: it selects the Snyk target name (4.1 Snyk); every group key has a consumer (GR-04).

### 4.2 The sweep job (one per enabled provider)

`Jobs()` returns for each enabled provider:

```go
job.Spec{
    Kind: "integrations.jenkins" | "integrations.snyk",        // key: "integrations.jenkins/_"
    Upstream: "jenkins" | "snyk", Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
    Policy: freshness.Policy{TTL: cfg.TTL /*5m | 15m*/, Jitter: 0.1},   // IdleBackoffMax 0: statuses are polled, never backed off
    Timeout: cfg.SweepTimeout /*4m | 2m, always < TTL*/,
    Run: func(ctx, _ job.Key) (job.Outcome, error) { return svc.Sweep(ctx, name) },
}
```

Global scope because one sweep serves the whole catalogue (and the engine gives it pacing, the rate-limit pause, parking and panic accounting like any call, architecture 6.3). Per-repository scope would create 1,000 planner items and 1,000 publishes.

`Sweep(ctx, provider)`:

```
span "integrations.sweep" {provider}
snap := publisher.Current()                              // the ONLY read of the snapshot for resolution
targets, invalid := Resolve*(cfg, snap, ...)
slugOf := map[RepoRef]Slug from snap

// 1. what to probe now
probe := targets minus (jenkins only) those whose previous value is Info.State==not_found and now-FetchedAt < not_found_ttl   // no 404 storm
        // ... unless this process saw the 404 for a DIFFERENT instance|job than the one resolved now: a corrected descriptor is probed at once (in-memory map, guarded by Service.mu; after a restart the stored 404 is trusted for up to not_found_ttl)
// 2. fetch
results, sweepErr := fetchAll(ctx, provider, probe)      // len(results)==len(probe), always. See 4.2.1
// 3. reduce each result against the previous value                (4.3)
// 4. repositories that lost their target -> update{remove}
// 5. persist (outside the publish lock, detached 10 s context so a deadline-cut sweep still stores what it learned)
// 6. publish once: publisher.Apply(merge into the FRESHEST slot value)    (4.3)
// 7. metrics, health, span attributes
return job.Outcome{Unchanged: !changed}, sweepErr
```

#### 4.2.1 `fetchAll`

- **Bulk provider** (`snyk`): one `FetchBulk(ctx, probe)`. A returned error fails every target with that error and is the sweep error (`RateLimitError` / `PermanentError` pass through). A result slice of the wrong length is `ErrProtocol` for the whole sweep.
- **Per-target provider** (`jenkins`): a pool of `jenkins.concurrency` goroutines (default 4), fed through an unbuffered channel, **owned and joined by `fetchPool` before it returns** (no goroutine outlives the job). Request starts are paced by a shared "next slot" timestamp (`jenkins.pace`, default 100 ms: at most 10 request starts per second, so 1,000 targets take about 100 s, inside the 4 min sweep timeout; the formula `targets x max(pace, latency / concurrency) < sweep_timeout` is logged as a warning at sweep start when exceeded by 80 %).
- **Per-instance abort rule** (this is the circuit breaker of these two batch upstreams; `gobreaker` is not in the adapters' import rows and a per-call breaker is meaningless in a batch). Outcome classes per target: *neutral* (`nil`, `scm.ErrNotFound`, `ErrProtocol`, `ErrTooLarge`: the instance answered) reset both counters; **`scm.ErrForbidden` (HTTP 403)** resets the failure counter and increments a separate *denied* counter, and at `jenkins.abort_after` (default 5) **in a row** aborts the instance as `PermanentError{auth}` (Jenkins answers 403 per job or folder, so isolated 403s are per-target results; a run of them means anonymous read is disabled or the token lacks Overall/Read; the first draft treated any 403 as instance-wide and parked the whole sweep key for 24 h); `RateLimitError` cancels the whole sweep; a `PermanentError` (HTTP 401, redirect to `/login`, unknown instance) aborts that instance; any other error increments the consecutive counter and at `abort_after` aborts the instance with `"jenkins instance \"x\" unavailable after N consecutive failures: ..."`. Targets of an aborted instance that were not attempted get `ErrAborted` (code `aborted`): they are recorded, not silently dropped.
- **Folding instance circuits into the sweep error** (`fetchOutcome{results, sweepErr, partial}`): a rate limit anywhere is the sweep error (priority 3; the engine pauses the upstream until `RetryAt`). Otherwise, **only when every probed instance is aborted** is the sweep error the worst cause (a permanent error beats a transient one: the engine parks `integrations.<p>/_` with a 24 h probe for a rejected token, or backs off 2 s to 15 min for an unreachable host). **When at least one instance finished unaborted the sweep returns no error** and the aborted instance is reported through `partial` into `Health.LastError` and `/status` (the first draft returned the abort as the sweep error, so one dead instance of two parked or repeatedly re-ran the healthy instance's sweep: with a transient abort the engine's retry would re-fetch the healthy instance's 1,000 targets after 2 s, 4 s, ...). Tests `TestOneAbortedInstanceDoesNotFailTheSweep`, `TestAllInstancesAbortedPrefersPermanent`, `TestForbiddenIsPerTargetUntilARun`.
- A sweep is **healthy** (`Health.LastSuccess`, `scp_integrations_last_success_timestamp_seconds`, `ConsecutiveFailures` reset) only when it has no sweep error, no aborted instance, **and not every probed target failed**. A wrong `base_url`, an HTML login page behind a 200, or a proxy error page makes every target a per-target `protocol` error; without this rule that reads as a successful sweep, and a component that reports itself healthy without a good outcome hides the outage (GR-41). `Health.Failed`, `FailedCode` (most frequent code) and `ConsecutiveFailures` are shown in `/status` (`TestAllTargetsFailedIsNotASuccess`).
- The sweep error handed to the engine is, in priority order: rate limit, permanent, transient. A context cancellation or deadline during the sweep yields `ctx.Err()` as a transient error; results obtained before it are still published (test `TestDeadlineStillPublishes`).
- Per-target failures are **results**, never sweep errors: one broken Jenkins job cannot starve the others (GR-22, GR-23).

### 4.3 Reduction, persistence and publication

Per probed target, `reduce(provider, prev, result)`:

| Result | New slot value | Store action |
|---|---|---|
| `Err == nil` | `V = status` (`Provider` forced, `ObservedAt = now`, `Summary` capped); `Info{State: ok, FetchedAt: now, CheckedAt: now, ContentSHA: sha(canonical(status) without ObservedAt), TTL}` | `PutItem(kind "integrations.<p>", payload JSON, state ok)` when the SHA differs from the previous value's, else `Revalidated(now)` (no payload rewrite; the 304 analogue, so an unchanged sweep is 1,000 tiny writes, not 1,000 payload rewrites) |
| `errors.Is(err, scm.ErrNotFound)` | `V = Status{StateUnknown, "no data"}`; `Info{State: not_found}` or, for `ErrNoData` (Snyk: not monitored), `Info{State: no_content}`; `FetchedAt = now` (the determination succeeded) | `PutItem` / `Revalidated` by SHA, state stored as `not_found` / `no_content` |
| any other error (including `aborted` and `rate_limited`) | **stale-while-error**: `V` stays the previous good value (or `Status{Provider, StateUnknown}` if none); `Info.State = error`, `CheckedAt = now`, `Attempts = prev+1`, `ErrCode = ErrCode(err)`, **`FetchedAt` unchanged** | `RecordFailure(code)` |

`freshness.Info.Derive(now)` then yields Stale (a good value exists, age beyond 1.5 TTL or last attempt failed) or Error (never a good value). Absence is a typed freshness state, never a cached `null` (GR-29).

Repositories whose target disappeared (descriptor edited, group default removed, repository archived) and still hold an entry for this provider get `update{remove: true}`: the provider's key is deleted from their `Statuses` in the same publish. Store rows of removed targets are left (no `DeleteItem` verb exists; they are cascade-deleted with the repository and ignored on warm start when no target exists... they are re-read into the slot at warm start and removed by the next sweep: documented, harmless).

**Publication** (`publish`): one `Publisher.Apply` per sweep. `fn` does no I/O and never blocks (architecture 8.4 rule): inside `fn`, for each update it reads the **freshest** `Statuses` of that repo from `b.View()` (not from the sweep-start snapshot), clones it with `maps.Clone`, sets or deletes only this provider's key, and `readmodel.Put`s it. This is what keeps the two sweeps (Jenkins at 5 min, Snyk at 15 min) from overwriting each other: reading from the sweep-start snapshot would lose the other provider's concurrent update (test `TestConcurrentProvidersMerge`, `-race`, 20 interleaved sweep pairs). `readmodel.ErrUnknownRepo` for a repository removed by a concurrent reconcile is **skipped**, not propagated (one removal must never abort the publish, and a late write must never resurrect a repository; architecture finding F-08). Any other error (`ErrInvalidSnapshot`) becomes `PermanentError{Code: "invalid_snapshot"}`: the job is parked loudly instead of retried.

`Outcome.Unchanged` is true when no repository's observable answer (state, summary, detail URL, counts, info state) changed. A snapshot is still published every sweep because `Info.FetchedAt` must advance (staleness is derived from it): one publish per 5 min (Jenkins) and per 15 min (Snyk), well inside the budget of architecture 8.4. **Known side effect (accepted, low):** every such publish changes `Snapshot.Rev(id)` of every repository that has a status, so the per-repository ETags (`Rev`) and, in v1.1, SSE change sets cover about 1,000 repositories every 5 minutes even when nothing visible changed; clients revalidate once per sweep. The mitigation, if profiling asks for it, is to publish a repository's entry only when its answer changed or its `FetchedAt` is older than `TTL/2 + sweep duration` (staleness tolerates it: `Derive` allows 1.5 x TTL); not built in v1. Failures of kind `aborted` and `rate_limited` also increment `Attempts` and write `RecordFailure` rows although the engine does not count a rate limit as an attempt: the per-item `Attempts` is "consecutive sweeps without a good answer", which is the honest reading for the badge (it goes grey).

### 4.4 Jenkins provider

**What it asks.** For a target `{Instance, Job, Branch}`:

```
GET {base_url}{JobPath.URLPath()}/api/json?tree=_class,color,lastCompletedBuild[number,result,timestamp,duration],lastBuild[number,building]
Accept: application/json        User-Agent: scp-integrations/1
Authorization: Basic base64(user:token)          // only when user+token are configured; attached to this instance's host only
```

`base_url` may carry a context path (`https://ci.example.org/jenkins`). The URL is `base.String()` + the escaped job path + a constant query: **no byte of a response and no unvalidated byte of a descriptor reaches the URL** (GR-15).

**Multibranch.** If `multibranch` is `always`, or `auto` and the first answer's `_class` contains `MultiBranch` (heuristic, V10), a second request asks `{jobpath}/job/<branchSegment(Branch)>/api/json?tree=...` where `branchSegment(b) = PathEscape(PathEscape(b))` (a `/` in the branch becomes `%252F`, V10). A 404 on the branch job yields `Status{Unknown, "branch not built"}` (not an error: the parent exists). An empty `Branch`, `.` or `..` (git ref names cannot be those, and `url.PathEscape` leaves dot segments untouched) yields `Status{Unknown, "no branch to look up"}` and no request. The branch is the repository's resolved default branch (Q-08); there is no per-repo branch override in v1.

**Mapping** (`lastCompletedBuild` is used, not `lastBuild`: a running build has `result: null`; the "running" fact is appended):

| Jenkins | State | Summary |
|---|---|---|
| `color == "disabled"` | Unknown | `job disabled` |
| no `lastCompletedBuild`, `_class` contains `Folder` | Unknown | `job is a folder` |
| no `lastCompletedBuild` | Unknown | `no completed builds` |
| `SUCCESS` | OK | `build #N passed` |
| `UNSTABLE` | Unstable | `build #N unstable` |
| `FAILURE` | Failing | `build #N failed` |
| `ABORTED` / `NOT_BUILT` / anything else | Unknown | `build #N aborted` / `not built` / `result unknown` |

If `lastBuild.building` and `lastBuild.number != N`, append ` (build #M running)`. `DetailURL = base + jobpath + "/" + N + "/"` **only if** `expose_detail_url` (default false: Jenkins URLs usually name an internal host and a public portal must not publish them).

**HTTP status mapping** (typed, no body text is ever copied into an error): 200 proceed; **401 -> `PermanentError{auth}`** (credentials rejected); **403 -> per-target `scm.ErrForbidden`** (code `forbidden`: Jenkins answers 403 for a job or folder the user may not read; `abort_after` in a row abort the instance as `auth`, 4.2.1; `Check` maps a 403 at the instance root to `auth`); **a response whose final URL path ends in `/login`** (redirect to the login page: anonymous read disabled and no credentials) -> `PermanentError{auth}` (heuristic, verified in M0 with the recorded `anonymous-denied` fixture, Q-2); any other HTML answer is `ErrProtocol` by the content-type rule; 404 -> `scm.ErrNotFound` wrap (job missing); 429, or 503 **with** `Retry-After` -> `RateLimitError` (`Retry-After` seconds or HTTP-date, clamped to 1 h, default 60 s); other 5xx -> transient; other statuses -> `ErrProtocol`. Content type must parse to `application/json` else `ErrProtocol`. A small error body is drained (4 KiB) so the connection is reused. Body read through `io.LimitReader(max_response_bytes+1)`; over the cap -> `ErrTooLarge`. `encoding/json` decode into the typed struct (unknown fields ignored: the upstream evolves); failure -> `ErrProtocol`. **No XML** is parsed anywhere (GR-34).

**Redirect policy (credential and SSRF safety).** The provider copies the injected `*http.Client` and installs `CheckRedirect`: at most 3 redirects, each to exactly the same scheme and `host:port` as `base_url`, otherwise `http.ErrUseLastResponse` (the 3xx response is returned and mapped to `ErrProtocol`). Credentials therefore never reach another host even if the injected client had a permissive policy (`TestRedirectToOtherHostIsPinned`). The client's `Timeout` is set from `instance.timeout`.

**`Check(ctx)`** (for `scp doctor`): per instance, `GET {base}/api/json?tree=_class`; reports auth/reachability errors with the same typed codes.

### 4.5 Snyk provider (Q-25)

**One listing per sweep.** `FetchBulk(ctx, targets)` pages the organisation's projects:

```
GET {base_url}/rest/orgs/{org_id}/projects?version={api_version}&limit=100&expand=target&meta.latest_issue_counts=true[&origins=a,b][&starting_after=CUR]
Accept: application/vnd.api+json     Authorization: Token {token}       User-Agent: scp-integrations/1
```

Cost: `ceil(projects / 100)` requests per 15 min (about 30 for 1,000 repositories with 3 projects each); Snyk does not publish the REST limit (V6), and 2 requests per minute are far below any plausible limit.

**Paging.** `links.next` may be a string or `{"href": ...}`. **The URL is never followed**: only its `starting_after` query value is extracted (<= 512 bytes) and put into a request the provider builds itself, against the configured host and the fixed path. A cursor equal to the previous one ends the loop; more than 200 pages is `ErrProtocol`. This blocks a hostile or buggy `links.next` from steering the token to another host (`TestNextLinkNeverFollowed`).

**Aggregation.** Only `attributes.status == "active"` projects with `meta.latest_issue_counts` count. Key = lower-cased `relationships.target.data.attributes.display_name`; counts of all projects of a target are **summed** per severity; each project's count is clamped to `0..integrations.MaxCount` (1,000,000), **saturating** at the cap (a negative or NaN value is 0; the first draft returned 0 for any value above 1e6, which turns a huge count into "no known issues": `TestHugeCountSaturates`). The sum of up to 20,000 projects cannot overflow an `int`. The first valid UUID project id is kept for the optional detail link.

**Mapping per target.** No aggregate for `lower(Target.Project)` -> `ErrNoData` (stored as `no_content`: "not monitored"). Otherwise with `fail_on` (default `high`): any severity at or above `fail_on` with count > 0 -> Failing; any lower severity > 0 -> Unstable; all zero -> OK. Summary = `"N critical, N high, ..."` listing non-zero severities in severity order, or `no known issues`. `Counts` has all four keys. `DetailURL` = expansion of `detail_url_template` (placeholders `{project_id}` `{owner}` `{repo}`; owner/repo `PathEscape`d; project id must be a UUID) only if the template is configured and the result parses as an https URL with a host and no userinfo; empty template = no link (the organisation slug needed for Snyk's app URLs is not in the projects response, so SCP does not guess the URL format).

**HTTP mapping.** 200 proceed; 401/403 -> `PermanentError{auth}` with the hint "check base_url: Snyk is regional, and a token for one region is commonly rejected by another" (V1: a diagnostic hint, not a documented guarantee); 404 (organisation) and 400 (`version`/`origins` rejected) -> `PermanentError{config}`; 429 -> `RateLimitError` (`Retry-After` if present and numeric, else 60 s, V6); 5xx -> transient; other -> `ErrProtocol`. Content type must be `application/vnd.api+json` or `application/json`. Caps: `max_response_bytes` per page (4 MiB). The token appears only in the `Authorization` header (GR-13), never in errors, logs or URLs (`TestErrors` asserts the token is absent from error text).

**`Fetch(ctx, t)`** = `FetchBulk` of one target (used by `doctor` and tests; sweeps use the bulk path). **`Check(ctx)`** = one page request.

**Ignored issues.** Whether `meta.latest_issue_counts` excludes ignored issues is not stated in the OpenAPI text (unverified; section 15). The provider reports what Snyk reports.

### 4.6 Badge and API facts

`FactOf(value, now)` (`TestFactOf`): derive freshness; **Unknown/Error -> message `unknown`, neutral**; **Stale -> keeps the last known message, `Stale: true`, tone forced neutral** (a green "passing" that is two hours old is a lie; grey signals staleness without a new colour); fresh -> tone from state. Messages: Jenkins `passing` / `failing` / `unstable` / `unknown`; Snyk `no issues` or `<n> <highest severity>` (for example `3 high`), `unknown`. The badge label comes from the badge-kind enum (`build`, `security`), the message from these closed strings: nothing from upstream text (`badges` builds the SVG locally with structural XML escaping, GR-33).

**Tone contract with `badges`.** `Fact.Tone` reaches unit 09's `bridge.BadgeFacts`, which maps **by name**: `Tone.String()` returns exactly `success` (good), `warning`, `danger`, `neutral`; an unknown name reads as neutral there, so the first draft's enum without names would have rendered every build badge grey (found by the verifier reading 09 section 2.4; `TestToneNamesAreTheCrossUnitContract`).

Badge kinds: `GET /badges/{repo}/build.svg` (Jenkins) and `/security.svg` (Snyk), per ADR-011. A kind of a **disabled** provider is an unknown kind (404). A known repository whose provider is enabled but has no data yet is 200 grey `unknown` with `max-age=60`. `build` and `security` are the only badge kinds this unit feeds.

### 4.7 Access (G-08, Q-03)

#### 4.7.1 Modes

- `public` (default). No identity, `Authorizer` is public-only (`CanView` true only for `VisPublic`). The middleware still **strips** identity headers and counts any that were present (`mode_public`), so a request that carries `X-Forwarded-User` can never influence a handler in any mode.
- `trusted-proxy`. An SSO proxy (oauth2-proxy, Authelia, an ingress with `auth_request`) authenticates users and sets identity headers; SCP believes them only under the rules below. This is the only way non-public repositories are served (ADR-023). Startup refuses non-public visibility with `mode: public` (`Config.AllowsNonPublic()` is consulted by the root `Validate()`).

#### 4.7.2 Identification algorithm (`identifier.identify`, reference code `identity.go`)

Input: `r.RemoteAddr` (**the TCP peer, never `X-Forwarded-For`**) and the request header map. Steps, in this order:

1. `present` := any header of the **strip set** exists. The strip set = the configured user/email/groups/secret header names **plus** a built-in deny list (`X-Forwarded-User/-Email/-Groups/-Preferred-Username`, `X-Auth-Request-User/-Email/-Groups/-Preferred-Username/-Access-Token`, **`X-Forwarded-Access-Token`** (oauth2-proxy `--pass-access-token`, V16: an access token must not reach a handler or a log either), `X-Remote-User/-Groups/-Email`, `Remote-User/-Groups/-Email/-Name`, `X-Authenticated-User`, `X-User`) plus the underscore spelling of each (`X_forwarded_user`, for proxies that map `_` to `-`). The built-in list means reconfiguring header names can never leave a stale spoofable name behind.
2. Copy the values of the four configured headers out of the map.
3. **Delete every strip-set header from the request, unconditionally.** After this line no handler, middleware or logger downstream can read an identity header. (Handlers must use only `authz.PrincipalFrom(ctx)`; a `forbidigo` rule bans `Header.Get("X-Forwarded-User")`-style reads outside `access`.)
4. If none were present: anonymous, no reason (the normal public request).
5. If `mode != trusted-proxy`: anonymous, reason `mode_public`.
6. If the peer is not inside `trusted_proxies` (`netip.ParseAddrPort(RemoteAddr)`, IPv4-mapped IPv6 unmapped, zone dropped; unix sockets, empty or garbage addresses are untrusted): anonymous, reason `untrusted_peer`.
7. If a proxy secret is configured: the secret header must have **exactly one** value whose SHA-256 equals the configured secret's SHA-256, compared with `subtle.ConstantTimeCompare` (hashing first hides the length); otherwise anonymous, reason `bad_secret`. (The secret is defence in depth against a compromised neighbour inside the trusted range; it is *not* the primary control.)
8. Subject = the user header; if absent, the email header (this is what the `email_header` key is for). More than one value line for the user or email header is ambiguous (header smuggling, `X-Forwarded-User: alice` appended after `admin`): anonymous, reason `duplicate`. No subject at all (trusted proxy, no user: an SSO-bypass path such as badges): anonymous, no reason.
9. Validate the subject: valid UTF-8, 1..256 bytes, no control characters (Cc), no Unicode format characters (Cf: bidi overrides, zero-width), **no line/paragraph separators (Zl, Zp: U+2028/2029) and no private-use characters (Co)**, no U+FFFD. Failure: anonymous, reason `malformed`.
10. Groups header (all lines merged): split on `groups_separator` (default `,`), trim, drop empty, de-duplicate; each group 1..128 bytes, same character rules; more than `max_groups` (default 64) distinct groups is `malformed` (truncating could drop the group that grants access, so over-cap fails closed); total line length <= 4096 bytes.
11. Return `Principal{Subject, Groups}` (not anonymous). The email address is **not** stored (GR-35); logs carry `SubjectHash(subject)` only.

Middleware (`Access.Middleware`): runs `identify`; on a reason: counter `scp_identity_header_spoof_total{reason}` (every request) and a `warn` log with the reason only (never a header value), **sampled to one line per reason per minute** (the first draft logged every request and claimed the rate limiter bounded it, but the limiter sits after this middleware in the chain; the counter carries the volume); computes the view class (`authenticated && Authorizer.CanView(VisPrivate)`); counter `scp_access_requests_total{principal, class}`; stores the principal with `authz.WithPrincipal`; for `ViewAll` principals wraps the `ResponseWriter` so `Cache-Control` becomes private (4.7.5). Placement (cmd/scp): public chain only, **directly after client-IP resolution**: `... clientIP -> access.Middleware -> securityHeaders -> rateLimit -> CrossOriginProtection -> mux`. It is **never installed on the admin listener** (ADR-025: identity-trust logic never runs there); admin handlers receive `access.Operator()`.

#### 4.7.3 Why `access.trusted_proxies` is separate from `server.public.trusted_proxies`

`httpx` trusts proxies to walk `X-Forwarded-For` for the client IP and rate-limit keys; `access` trusts proxies to *authenticate users*. The second is a strictly stronger statement, and the ranges may legitimately differ (an L4 load balancer may be XFF-trusted yet must not be believed about identities). Each is configured explicitly; neither defaults to the other. A prefix written in IPv4-mapped IPv6 form (`::ffff:10.0.0.0/104`) is normalised to its IPv4 form (peers are unmapped before matching; unnormalised it would never match). `Info().Warnings` adds `broad_trusted_range` when a trusted range is wider than IPv4 /16 or IPv6 /48 and `no_proxy_secret` when no secret is configured; both show in `/status`.

#### 4.7.4 Authorizer and view classes

```
CanView(ctx, p, v):
    v == VisPublic            -> true
    (any other value, including future ones: fail closed)
    mode != trusted-proxy     -> false
    p.Anonymous || p.Subject == "" -> false
    no view_all lists         -> true                          // every authenticated principal
    subject in view_all.subjects (case-insensitive)  -> true
    any p.Groups[i] in view_all.groups (exact, case-sensitive) -> true
    else false
```

**Monotonicity (property-tested, `TestAuthorizerMonotone`)**: `CanView(.., VisInternal) == CanView(.., VisPrivate)` for every principal, and `authz.ClassOf(az, p) == ViewAll` iff `CanView(.., VisPrivate)`. `scm.ViewClass` has two values and every global projection (`commits.latest`, `manifests.graph`, `site.meta` counts, group badge counts, `llms-full.txt`, `dependencies.md`) exists in exactly two variants (architecture 17.3). Allowing "internal for staff, private for admins" would need a third variant per projection and would reopen the count leak of M2; it is rejected. GitHub `internal` repositories (GHEC/GHES) therefore have the same audience as private ones.

Groups are matched exactly (IdP group names are case-sensitive and a lenient match risks an accidental grant); subjects **ASCII-case-insensitively** (e-mail addresses): `asciiLower` folds `A-Z` only. `strings.ToLower`/`EqualFold` apply Unicode simple folding, which maps U+212A KELVIN SIGN to `k` (executed by the verifier), so a subject registered at the IdP as `\u212Aevin@example.org` would match the allow-list entry `kevin@example.org` of another person (`TestSubjectCaseFoldIsASCIIOnly`).

`Operator()` allows every visibility for every principal: it is the authorizer of handlers mounted on the admin listener (`/api/v1/status`, retry), which is reachable only from a restricted network and shows repository names of parked keys.

#### 4.7.5 Cache safety

Responses that a `ViewAll` principal receives may contain non-public data, so the wrapper rewrites the response's `Cache-Control` at `WriteHeader` time: `public` -> `private`, none -> `private`, otherwise `private, <existing>`; unchanged when it already contains `private` or `no-store`. Informational statuses (1xx, e.g. 103 Early Hints) pass through without consuming the one-shot rewrite. The wrapper implements `Flush` and `Unwrap` (so `http.ResponseController` and SSE keep working). Anonymous principals keep the handler's headers (their variant is the public one). This protects a shared cache (CDN, ingress cache) that sits between SCP and an authenticated user, in addition to the weak ETag variant (`W/"g<version>-<variant>"`) the handlers already compute per class.

### 4.8 Visibility enforcement across every output surface

The rule is one sentence: **an invisible repository must be indistinguishable from a nonexistent one, in names, counts, content, status codes, bodies, headers and ETags, on every surface; a global summary must be computed from the visible set only.** Mechanisms are `authz.LookupRepo` (single resolver), list filtering through `Authorizer.CanView`, and `scm.Classed[T]` variants chosen by `authz.ClassOf`. This unit owns the matrix and the tests; each owner implements its row.

| Surface (route name) | Rule for a principal that cannot see repo R | Mechanism / owner |
|---|---|---|
| `api.repo`, `api.docs.*`, `api.manifests.*`, `api.pages`? (no repo), per-repo `commits` | 404 `not-found`, same body and headers as a slug that never existed (including `detail` text) | `LookupRepo` first statement of every handler (each owner) |
| `api.repos`, `api.groups`, `api.group.repos`, `api.commits` (`repo=`, `group=` filters) | R absent; `group=` of a group with no visible member is 404; page totals and cursors computed from the visible set | filter by `CanView` before pagination; `commits` k-way merge over visible repos only |
| `api.groups` | a configured group with **no visible member** is omitted from the public variant (matches a deployment that only has the public repositories, where such a group has no repositories) | catalog / overview |
| `api.site` (`repoCount`), group repository-count badge, `commits.latest`, `manifests.graph` (producers, reverse index, histograms, `/components?purl=`), `llms-full.txt`, `dependencies.md`, sitemap | computed per `ViewClass`; the public variant is built from `VisPublic` repositories only; in a deployment without non-public repositories both variants are the same value | `scm.Classed[T]`, `authz.ClassOf` (each projector's owner) |
| `badge` (`/badges/{repo}/{file}`) | identical to an unknown repo: 404 with the small grey "unknown" body, `Cache-Control: public, max-age=60`, no ETag; the visibility decision happens **before** any repo-specific work so the response cannot differ in timing class or headers | badges; decision uses `LookupRepo`-equivalent on the snapshot |
| `badge.group` | counts from the class variant | badges |
| `media.repo` (`/media/{repo}/{ref}/{path...}`) | 404 as for an unknown repo | media (`LookupRepo`) |
| `media.ext` (`/media/ext/{sig}/{id}`) | not repository-bound: the signed URL only exists inside responses the principal was already authorised to read; no visibility check, no repository data in the response (external image bytes only) | media |
| `readme.md`, `doc.md`, `agents.md`, `changelog.md`, `/repos/{repo}/llms.txt`, `/repos/{repo}/dependencies.md` | 404 as unknown | docs / manifests / overview (`LookupRepo`) |
| `negotiated.repo*`, SPA shell for `/repos/{repo}...` | status **404** (shell body), also for the alias path | overview |
| **slug alias redirects** (`catalog.aliases`, 308) | redirect only if the **target** repository is visible to the principal; otherwise 404 (a redirect for a renamed private repository would confirm that it exists) | overview |
| `llms.txt`, `llms-full.txt`, group-scoped files, `sitemap.xml` | list only visible repositories; class variant | overview |
| `integrations` statuses inside `api.repo` | part of the repo composite: gated by the repo's visibility; **and** `DetailURL` is empty unless the operator opted in | integrations + overview |
| `api.snapshot`, `api.events` (SSE, v1.1) | payload carries no repository names for the public class; **version/ETag side channel**: see requirement R-1 | live, httpapi |
| `api.status`, `api.admin.retry`, `/metrics`, `/debug/pprof` | admin listener only, `Operator()` authorizer, never routed publicly | ops |
| webhooks, probes, `csp.report`, assets | no repository data in any response | n/a |

**R-1 (cross-unit requirement, snapshot activity side channel).** `api.snapshot`'s `version` and the `W/"g<version>-<variant>"` ETag use the global snapshot version, which also advances when only a **private** repository changes. An anonymous observer can therefore infer activity in private repositories (low severity, but real and cheap to close). Requirement on `readmodel`/`live`/`httpapi`: expose a per-class change counter (`Snapshot.ClassRev(ViewClass)`: increments only when something visible to that class changed) and use it for the ETag and for `api.snapshot` of the public class. Until then the anonymous enumeration test pins the current behaviour as a documented, accepted leak (section 12, `E2E-enum-etag`). Raised as open question Q-1.

### 4.9 Failure modes and typed errors

| Situation | Where it surfaces | Typed result | Effect |
|---|---|---|---|
| Config invalid (`integrations`, `access`) | startup | `Validate()` joined error with key paths | process refuses to start |
| Provider set != enabled set (wiring bug) | `integrations.New` | plain error naming the provider | startup abort |
| Jenkins 401 or `/login` redirect, Snyk 401/403/404/400 | provider | `*PermanentError{auth|config}` | aborts the instance; **sweep error only when every probed instance is aborted**, then the engine parks `integrations.<p>/_` (24 h probe); otherwise reported through `Health.LastError`; statuses go stale-with-error |
| Jenkins 403 | provider | wraps `scm.ErrForbidden` (`forbidden`) | per-target result; `abort_after` in a row = instance `auth` as above |
| Every probed target failed (HTML login page, wrong URL) | sweep | per-target `protocol` results | not a healthy sweep: `LastError` = most frequent code, `ConsecutiveFailures++`, no `LastSuccess` |
| 429 (and Jenkins 503 + `Retry-After`) | provider | `*RateLimitError` | engine pauses the upstream until `RetryAt`; no attempt consumed |
| Network error, timeout, 5xx | provider | wrapped plain error (`transient`) | per target: stale-while-error; after `abort_after` consecutive failures the instance is aborted; the sweep returns a transient error (engine backoff) only if no other instance remains healthy |
| Job/target missing | provider | wraps `scm.ErrNotFound` / `ErrNoData` | stored `not_found` / `no_content`, not re-probed for `not_found_ttl` (Jenkins) |
| HTML or wrong content type, bad JSON | provider | `ErrProtocol` | per-target error |
| Response over cap | provider | `ErrTooLarge` | per-target error |
| Repository removed during a sweep | `publish` | `readmodel.ErrUnknownRepo` skipped | no failure |
| `ErrInvalidSnapshot` | `publish` | `PermanentError{invalid_snapshot}` | parked, loud |
| Store write fails | `persist` | logged (`warn`), counted by the store | snapshot still published (architecture 9.3) |
| Identity header from untrusted peer / bad secret / duplicates / malformed | `access` | principal anonymous, reason counted | request proceeds as anonymous |
| Non-public repo with `access.mode: public` | startup | validation error | refuse to start |

---
## 5. Data

### 5.1 Snapshot slots

| Slot | Name | Value | Writer | Readers |
|---|---|---|---|---|
| per repository | `integrations.status` | `integrations.Statuses` (`map[provider]freshness.Value[Status]`) | the two sweeps (`integrations.Service`), merged per provider inside `Apply` | `overview` (repo composite, llms), `badges` (via `bridge.BadgeFacts`), `ops` (via health bridge) |

Registered by `cmd/scp` in the frozen `readmodel.Registry`: `readmodel.Register[integrations.Statuses](reg, "integrations.status")`. It is **not** the existence slot. No new global and no projector: statuses are per repository and need no cross-repository index. No validator (a status never references another entity).

Architecture 9.1 names the slot value `integrations.Status (per provider map)`; the concrete Go type is the named map `Statuses` (deviation D-4, cosmetic).

Memory: a `Statuses` map with two entries is about 400 bytes; 1,000 repositories about 0.4 MB, negligible against the 52 MB baseline.

### 5.2 Durable rows (no schema change)

Rows in the existing `item` table (architecture 9.2), one per (repository, provider), `kind = "integrations.jenkins" | "integrations.snyk"`; no DDL.

| Column | Content |
|---|---|
| `status` | `ok`, `not_found`, `no_content`, `error` (freshness states) |
| `fetched_at` | last successful determination (UTC seconds); unchanged by a failure |
| `checked_at` | last attempt |
| `attempts` | store-owned consecutive failures |
| `content_sha` | first 8 bytes (16 hex) of SHA-256 over the canonical status (provider, state, summary, detail URL, sorted counts; **not** `ObservedAt`) |
| `error` | the `ErrCode` of the last failure |
| `payload` | JSON, schema 1: `{"schema":1,"state":"ok|failing|unstable|unknown|disabled","summary":"...","detailUrl":"...","observedAt":"RFC3339Z","counts":{"critical":0,...}}`; not rewritten by `Revalidated` or `RecordFailure` (the stored payload stays and is what warm start loads; the `Item.Payload == nil` of `store.Item` describes the verb argument, not the row) |
| `next_due_at`, `unchanged_streak` | not used for these kinds: the sweeps are **global** keys tracked by the planner under `integrations.<p>/_`, not per-repository items |

Write verbs used: `PutItem` (200-equivalent), `Revalidated` (same content SHA), `RecordFailure` (keeps payload and `fetched_at`). One transaction each; a Jenkins sweep of 1,000 targets is at most 1,000 one-millisecond transactions per 5 minutes and, when nothing changed, 1,000 `Revalidated` writes (no payload bytes).

**Warm start.** `cmd/scp`'s warm load, after `catalog.repo` is applied, calls `Service.WarmItem(b, id, item)` for every item whose kind starts with `integrations.`; the caller resolves the slug from the `store.RepoKey` (it holds the catalogue). `WarmItem` ignores other kinds, ignores a provider that is no longer enabled, drops (with a `warn`) rows whose payload fails to decode or has another `schema` (the next sweep rebuilds them), and never fails the start for a bad row. Payload text is **untrusted bytes**: `Summary` is re-capped at 120 runes, `State` goes through a closed `parseState`, counts are plain integers; no HTML is involved (these fields are plain text rendered as text by the SPA).

**Retention.** Rows live and die with their repository (`ON DELETE CASCADE`). Rows of a repository that lost its target stay until the repository is removed or the provider's rows are rewritten; they are loaded at warm start and removed from the slot by the first sweep. There is no history table: only the latest status is kept.

**Stored rows after config change.** Disabling a provider: its rows are ignored at warm start and remain inert in SQLite (the DB is a rebuildable cache). Changing an instance's `base_url`: the next sweep overwrites.

---

## 6. Configuration

Feature-owned types `integrations.Config` and `access.Config` compose into the root `Config` as `integrations:` and `access:` (architecture 16). Strict decode; `Validate()` returns **all** violations with key paths. Durations are `time.Duration` strings; sizes are plain integer bytes (`max_response_bytes`) because feature packages cannot import the byte-size machinery of `platform/config` (rank). Environment name = `SCP_` + upper-snake path with list indices (`SCP_INTEGRATIONS_JENKINS_INSTANCES_0_BASE_URL`); secrets use a `{file: ...}` block or the `*_FILE` variable. Defaults are the embedded default; "consumer" is the code the documented-keys == consumed-keys test must find.

### 6.1 `integrations`

Per-instance defaults are applied by `Config.WithDefaults()` inside `Validate()` and `New()` (3.3). Everything is **off unless configured**: Jenkins is enabled iff `instances` is non-empty; Snyk iff `snyk.enabled`. The embedded default contains no URL, no host, no organisation (GR-14: a default that names a real host would send probes and credentials to it).

| Key | Type | Default | Validation | Consumer |
|---|---|---|---|---|
| `integrations.jenkins.instances[]` | list | `[]` (disabled) | names unique; if more than one, exactly one `default: true` | `Config.JenkinsEnabled`, `ResolveJenkins`, `jenkins.New` |
| `...instances[].name` | string | required | `^[a-z0-9][a-z0-9-]{0,31}$` | `ResolveJenkins` (descriptor/group `instance` lookup), metrics label |
| `...instances[].base_url` | string | required | absolute `https` (`http` only with `allow_http`); host present; **no userinfo, query, fragment, empty or percent-escaped path segments** (`ValidateBaseURL`); path prefix allowed (`/jenkins`) | `jenkins.New`, `cmd/scp` (netguard host allow-list) |
| `...instances[].default` | bool | `false` | see above; a single instance is implicitly the default | `ResolveJenkins` |
| `...instances[].allow_http` | bool | `false` | explicit opt-in for plain http on a private instance | `ValidateBaseURL`, `cmd/scp` (netguard `AllowHTTP`) |
| `...instances[].user` / `.token` | string / Secret | unset | **both or neither** (empty secret = unset, GR-13) | `jenkins` (basic auth) |
| `...instances[].allow_private_cidrs` | `[]CIDR` | `[]` | valid CIDRs; `/0` refused | `cmd/scp` -> `netguard` policy (a Jenkins on a private address is blocked unless listed, architecture 17.2) |
| `...instances[].ports` | `[]int` | `[]` (= 80/443) | 1..65535 | `cmd/scp` -> `netguard` |
| `...instances[].discover` | `declared`\|`all` | `declared` | enum | `ResolveJenkins` |
| `...instances[].job_prefix` | string | `""` | valid path prefix | `ResolveJenkins` (only with `discover: all`) |
| `...instances[].multibranch` | `auto`\|`always`\|`never` | `auto` | enum | `jenkins.Fetch` |
| `...instances[].timeout` | duration | `10s` | 1s..1m | `jenkins.New` (client timeout) |
| `...instances[].max_response_bytes` | int | `1048576` | 1 KiB..16 MiB | `jenkins` (`io.LimitReader`) |
| `...instances[].expose_detail_url` | bool | `false` | | `jenkins` (detail URL) |
| `integrations.jenkins.ttl` | duration | `5m` | >= 1m | `Jobs()` (`Policy.TTL`) |
| `...sweep_timeout` | duration | `4m` | > 0 and < `ttl` | `Jobs()` (`Spec.Timeout`) |
| `...concurrency` | int | `4` | 1..16 | `fetchPool` |
| `...pace` | duration | `100ms` | >= 0 | `fetchPool` |
| `...abort_after` | int | `5` | >= 1 | `fetchPool` |
| `...not_found_ttl` | duration | `1h` | >= 1m | `Sweep` (carry) |
| `integrations.snyk.enabled` | bool | `false` | | `Config.SnykEnabled` |
| `...snyk.token` | Secret | unset | required when enabled; set while disabled = error (decorative key) | `snyk.New` |
| `...snyk.org_id` | string | `""` | UUID when enabled | `snyk` request path |
| `...snyk.base_url` | string | `https://api.snyk.io` | bare https origin (no path/query/userinfo); host in `{api.snyk.io, api.us.snyk.io, api.eu.snyk.io, api.au.snyk.io}` (V1) unless `allow_custom_host` | `snyk.New`, `cmd/scp` (netguard host) |
| `...snyk.allow_custom_host` | bool | `false` | explicit acknowledgement for hosts outside the documented list (for example a government region) | `Validate` |
| `...snyk.api_version` | string | `2024-10-15` | `^\d{4}-\d{2}-\d{2}(~(beta\|experimental))?$` | `snyk` (`version=`) |
| `...snyk.origins` | `[]string` | `[]` (no filter) | | `snyk` (`origins=`); the legal values are not enumerated in the OpenAPI text and are unverified (section 15) |
| `...snyk.fail_on` | enum | `high` | critical\|high\|medium\|low | `snyk` state mapping |
| `...snyk.detail_url_template` | string | `""` (no link) | https, placeholders only `{project_id}` `{owner}` `{repo}` | `snyk` |
| `...snyk.ttl` | duration | `15m` | >= 1m | `Jobs()` |
| `...snyk.sweep_timeout` | duration | `2m` | > 0, < `ttl` | `Jobs()` |
| `...snyk.timeout` | duration | `20s` | >= 1s | `snyk.New` |
| `...snyk.max_response_bytes` | int | `4194304` | 1 KiB..64 MiB | `snyk` |
| `...snyk.allow_private_cidrs`, `.ports` | as Jenkins | `[]` | valid CIDRs (`/0` refused); ports 1..65535 (the first draft did not validate Snyk ports) | `cmd/scp` -> netguard (GHES-style private Snyk proxies) |

Cross-config rules in the root `Validate()` (`cmd/scp`): `integrations.ValidateGroupDefaults(catalog groups)`; every descriptor-selectable instance name list handed to `docs`; refresh upstream entries `jenkins` and `snyk` are created exactly when the provider is enabled. The refresh config keys `refresh.upstreams.jenkins.*` / `.snyk.*` need no new documentation: they are the existing `refresh.upstreams.<name>` map, whose defaults for these names are `MaxConcurrent 1`, `Pace 0`, `JobTimeout` >= the provider's `sweep_timeout` (the sweep paces itself).

Group-level keys (owned by `catalog.Config`, validated here): `catalog.groups[].integrations.jenkins.{job_prefix, instance}`, `.snyk.project_prefix`. Descriptor keys (owned by the `docs` schema): `integrations.jenkins.{instance, job}`, `integrations.snyk.project`.

### 6.2 `access`

| Key | Type | Default | Validation | Consumer |
|---|---|---|---|---|
| `access.mode` | `public`\|`trusted-proxy` | `public` | enum | `New`, `AllowsNonPublic` (root `Validate`) |
| `access.trusted_proxies` | `[]CIDR` | `[]` | required non-empty in `trusted-proxy`; error in `public`; **`/0` of either family refused**; each range must be private, loopback or link-local unless `allow_public_proxies`; bare addresses allowed | `identifier.peerTrusted` |
| `access.allow_public_proxies` | bool | `false` | acknowledgement | `ParsePrefixes` |
| `access.identity.user_header` | string | `X-Forwarded-User` | token chars `^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`; required in `trusted-proxy`; not reserved (`Authorization`, `Cookie`, `Host`, `Forwarded`, `X-Forwarded-For/-Proto/-Host`, `X-Real-Ip`, hop-by-hop names, `X-SCP-Request`); all four names distinct | `identifier` |
| `access.identity.email_header` | string | `X-Forwarded-Email` | as above; optional (`""` disables); fallback subject only | `identifier` |
| `access.identity.groups_header` | string | `X-Forwarded-Groups` | as above; optional | `identifier` |
| `access.identity.groups_separator` | string | `,` | 1..4 bytes when a groups header is set | `parseGroups` |
| `access.identity.max_groups` | int | `64` | 1..256 | `parseGroups` |
| `access.identity.proxy_secret_header` | string | `""` | **set together with `proxy_secret`** | `identifier` |
| `access.identity.proxy_secret` | Secret | unset | together with the header; empty = unset | `identifier` (hashed once at construction) |
| `access.view_all.groups` | `[]string` | `[]` | non-empty entries <= 128 bytes; requires `groups_header`; error in `public` | `authorizer` |
| `access.view_all.subjects` | `[]string` | `[]` | non-empty entries <= 256 bytes; error in `public` | `authorizer` |

Both `view_all` lists empty in `trusted-proxy` means **every authenticated principal sees non-public repositories**: that is correct when the SSO proxy admits only the organisation's members, and the operator must say so consciously by choosing the mode.

`config lint` also checks: `trusted-proxy` without `proxy_secret_header` -> warning code `no_proxy_secret`; ranges wider than IPv4 /16 or IPv6 /48 -> `broad_trusted_range` (both are surfaced by `Access.Info()` in `/status` at runtime).

---

## 7. HTTP/API surface

**This unit owns no route.** Its outputs travel inside other units' responses; this section fixes the contract for those contributions.

| Contribution | Where it appears | Shape | AuthZ class |
|---|---|---|---|
| Repository statuses | `api.repo` composite (owner `overview`), field `statuses[]`, schema `ProviderStatus` of `20-design/api/openapi-public.yaml` (unit 07, **authoritative for the wire shape**) | `[{provider, state, summary, detailUrl, observedAt, counts, freshness}]` with **all seven keys required**: `state` in `ProviderState` (`ok\|failing\|unstable\|unknown\|disabled`, open set); `summary` <= 300 chars (ours <= 120 runes); `detailUrl` string or `null` (never omitted); `observedAt` `NullableTimestamp`; `counts` an object, **`{}` for Jenkins** (never omitted); `freshness` is the shared `Freshness` object with **all seven keys**: `status`, `kind` = `"integrations.<provider>"`, `asOf` (= `Status.ObservedAt`), `fetchedAt`, `checkedAt`, `staleSince`, `lastErrorCode` (string or null). `lastErrorCode` is namespaced like the contract's example `github.rate_limited`: **`<provider>.<Info.ErrCode>`** (for example `jenkins.forbidden`, `snyk.rate_limited`). The first draft listed `detailUrl?`/`counts?` as optional and `freshness` without `kind`/`checkedAt`, which the validated contract would reject. Only enabled providers with an entry appear; a disabled provider is **absent**, never `ok` (clients treat enum values as open; `disabled` exists only inside the Go type and in the contract enum). `freshness` is derived from `freshness.Info` at read time (`Derive(now)`) | public (the repo's visibility decides, 4.8) |
| Badges | `GET /badges/{repo}/build.svg`, `/security.svg` (owner `badges`) | from `Fact{Message, Tone, Stale}` | public, repo-visibility-gated |
| llms | `/repos/{repo}/llms-full.txt`, group files (owner `overview`) | one line per provider: `Build (jenkins): passing (observed 2026-10-14T10:00:00Z)`; the same text rules as `Summary`; **`DetailURL` is never printed into llms output** (LLM surfaces never carry operator-internal URLs) | repo-visibility-gated |
| `/status` | `ops` (admin) | `integrations: [{provider, enabled, targets, invalid, probed, failed, failedCode, lastSweep, lastSuccess, lastError, consecutiveFailures}]` from `Health()`; `access: {mode, trustedRanges, spoofDropped, proxySecret, warnings[]}` from `Info()`; parked sweep keys appear through the engine's parked list | admin listener only |

Caching: the contributions inherit the host response's weak ETag (`W/"g<version>-<variant>"`) and `Cache-Control: no-cache`; for `ViewAll` principals the wrapper of 4.7.5 makes it `private`. Status codes: a repository that is invisible or unknown is 404 everywhere (4.8); no new problem codes are introduced.

**Cross-unit asks (additions to other units' contracts, none of them a new route):** `overview` composes `statuses[]` through `Service.StatusOf` for each of `Service.Providers()`; the group API and the dashboard card show the `build`/`security` badge URLs only for `Providers()`; the SPA hides the columns when the provider list is empty.

---

## 8. Concurrency and lifecycle

**Goroutines owned.**

| Goroutine(s) | Owner | Started | Stopped |
|---|---|---|---|
| `fetchPool` workers (<= `jenkins.concurrency`) | the running `Sweep` call | inside `Sweep`, per sweep | joined (`WaitGroup`) before `Sweep` returns; they stop on the job context (engine `Timeout` or shutdown) |
| none else | | | |

`access` starts no goroutine; `snyk` makes sequential requests; `Service` has no `Run`, no ticker (the planner and engine own cadence; `Jobs()` hands them specs). `goleak` runs in `TestMain` of `integrations` and `jenkins`.

**Start order** (architecture 10.1/8.16): store -> readmodel registry (slot registered) -> catalogue warm load (`catalog.repo` exists) -> `Service.WarmItem` for stored rows -> engine + planner (`Jobs()` registered with `Upstream: "jenkins"|"snyk"`, `AddUpstream` called) -> servers (the `access` middleware is part of the public handler chain). The first sweep is due immediately for a key without a stored `fetched_at`, at the engine's pace; statuses are a **late-arriving fact family and never gate readiness** (10.2).

**Stop order**: servers stop first (no new requests), then the refresh stage: the engine cancels in-flight sweeps after `Grace` (15 s). A cancelled sweep finalises with a detached 10 s context: results already obtained are persisted and published, and the sweep returns the context error (transient). Because `persist` and `publish` run on that detached context, shutdown during a sweep never leaves the store writing on a closed handle: the store stage stops after the refresh stage has joined.

**Cancellation inside a sweep**: every provider call takes the sweep context; `jenkins`' client timeout and `snyk`'s per-request timeout bound each call; `fetchPool` stops feeding on `ctx.Done()` and marks the rest `ErrAborted`; the rate limit case cancels the pool via an internal cancel.

**Locks**: `Service.mu` guards only the `Health` map; no lock is held across I/O, `Apply`, or a call into the engine. `Apply` is the only serialisation point and its function does map copies only.

---
## 9. Observability

Instruments are created from the injected `metric.Meter` (G11); names are constants next to their instruments (`MetricStatus`, ... in `service.go`; `MetricSpoof`, `MetricRequests` in `access.go`) and are part of the generated `docs/reference/metrics.md` test. OpenTelemetry instruments exported to Prometheus (dots/underscores as the exporter renders them; units as UCUM strings on the instrument).

| Metric (type, unit) | Labels | Meaning | Owner |
|---|---|---|---|
| `scp_integrations_enabled` (gauge, `1`) | `provider` | 1 when configured, 0 otherwise (recorded once at construction): "disabled" is visible, never faked as ok (GR-41) | integrations |
| `scp_integrations_status` (gauge, `{repository}`) | `provider`, `state` (`unknown`,`ok`,`failing`,`unstable`) | repositories per state after the last sweep | integrations |
| `scp_integrations_targets` (gauge, `{repository}`) | `provider`, `result` (`resolved`,`invalid`,`no_data`) | resolved targets, invalid descriptors/groups, targets Snyk does not monitor | integrations |
| `scp_integrations_fetch_total` (counter, `{request}`) | `provider`, `outcome` (`ok`, or an `ErrCode`: `not_found`,`no_data`,`auth`,`config`,`rate_limited`,`protocol`,`too_large`,`aborted`,`timeout`,`transient`) | per-target (Jenkins) or per-listing result (Snyk bulk counts every target) | integrations |
| `scp_integrations_fetch_seconds` (histogram, `s`) | `provider` | latency of one provider call (one Jenkins request or one Snyk listing) | integrations |
| `scp_integrations_sweep_seconds` (histogram, `s`) | `provider` | duration of one sweep job body | integrations |
| `scp_integrations_last_success_timestamp_seconds` (gauge, `s`) | `provider` | Unix time of the last sweep without a sweep-level error; alert on `time() - x > 3 * ttl` | integrations |
| `scp_job_*` (engine-owned) | `kind=integrations.jenkins\|integrations.snyk` | duration, outcome (`ratelimited`, `parked`, `retried`) of the sweep keys | refresh |
| `scp_identity_header_spoof_total` (counter, `{request}`) | `reason` (`untrusted_peer`,`bad_secret`,`duplicate`,`malformed`,`mode_public`) | requests whose identity headers were dropped (the architecture names the counter; the `reason` label is added, D-1) | access |
| `scp_access_requests_total` (counter, `{request}`) | `principal` (`anonymous`,`authenticated`), `class` (`public`,`all`) | public-listener requests by principal kind and view class | access |

Logs (`slog`, fields from the architecture's list plus): `provider`, `instance`, `repo` (slug), `outcome`/`err_code`, `reason` (access). Rules: log the **route pattern and job path class, never a URL with query, never a token or header value**; a repository-authored value (descriptor job, project) is never logged (a fixed `reason` string is); a subject is logged only as `SubjectHash`. Levels: sweep start/end `info` with counts, invalid target `warn`, dropped identity headers `warn` (**sampled in `access`: one line per reason per minute**; the counter carries the volume; the rate limiter cannot bound it because it sits after `access.Middleware`), store write failure `warn`.

Traces: span `integrations.sweep` {`provider`, `targets`, `changed`}, child of the engine's `job.run`; per-request client spans come from the `otelhttp` transport that `cmd/scp` installs on the netguard clients (there is no per-target span in the reference code, to keep cardinality low).

`/status`: `Service.Health()` per provider (`Enabled`, `Targets`, `Invalid`, `LastSweep`, `LastSuccess`, `LastError` code) and `Access.Info()` (`Mode`, `TrustedRanges`, `SpoofDropped`, `ProxySecret`, `Warnings`). A provider that is enabled and whose `LastSuccess` is older than `3 x ttl`, or whose `ConsecutiveFailures > 0`, or whose last sweep had `Failed > 0` and `LastError != ""`, is shown `degraded`; **disabled providers are shown as `disabled`, not ok**.

---

## 10. Security considerations specific to this unit

| Threat | Control |
|---|---|
| **SSRF through operator URLs** (`base_url` mistyped or config compromised) | Provider clients come from `platform/netguard` (connect-time IP policy, metadata and private ranges blocked unless the instance lists `allow_private_cidrs` and `ports`, `Proxy: nil`); `base_url` validated (https, no userinfo/query/fragment, plain path); Snyk host restricted to the four documented regions unless acknowledged; startup logs each instance's host and allow-list. Descriptors can only name an instance, never a host (D-06 rule enforced twice: `docs` schema and `ResolveJenkins`) |
| **Credential forwarding** | Basic auth and the Snyk token are attached only to requests built from `base_url`; `CheckRedirect` allows same scheme+host:port only, max 3 (Jenkins) / 2 (Snyk); `links.next` is never followed (cursor only); `DetailURL` and job URLs are built, never copied from a response; tests assert the foreign host receives zero requests and no `Authorization` |
| **Repo-authored descriptor injection** (`job: "../../../x"`, `a%2F..`, newline, very long) | `ParseJobPath` whitelist with fuzzed invariant; per-segment `PathEscape`; length caps; invalid values dropped with a fixed log reason; a repository cannot change which host, path root, scheme or query is requested |
| **Untrusted provider bytes** | Typed JSON decode with `encoding/json` only (**no XML parser exists in the unit**, GR-34); content-type check; `io.LimitReader` caps (1 MiB Jenkins, 4 MiB Snyk page); page cap 200; summaries generated from closed vocabularies and integers; counts clamped; stored payload re-validated at warm start |
| **Log/response injection via provider text** | No provider free text reaches `Summary`, logs, metrics labels, llms or badges; error text never embeds response bodies (`TestErrors` greps the token) |
| **Header spoofing** (`X-Forwarded-User: admin` from the internet) | 4.7.2: peer-based trust on `RemoteAddr`, unconditional stripping, constant-time proxy secret, duplicate-line rejection, character validation, startup refusal of `/0` and (by default) public ranges, `broad_trusted_range`/`no_proxy_secret` warnings, counter per drop plus a sampled warn log, mode `public` strips too, `X-Forwarded-Access-Token` stripped, subject comparison ASCII-only |
| **Compromised neighbour inside the trusted range** | Proxy secret header (defence in depth); documented NetworkPolicy so only the ingress can reach the public port; `broad_trusted_range` warning; admin listener has no identity logic at all |
| **Subject-based abuse** (`scp:operator`-style names, unicode tricks) | no reserved subjects exist (the operator authorizer is a different object wired only on the admin listener, not a magic subject); bidi/format/control characters rejected; subjects only compared (case-insensitively), never interpolated into HTML or paths |
| **Enumeration of private repositories** (names, counts, activity, timing) | 4.8 matrix; 404 not 403; byte-identical responses to the N-public deployment (test E2E-enum); class variants of all global projections; alias redirects gated by target visibility; requirement R-1 for the version/ETag side channel |
| **Shared-cache leak** | `Cache-Control: private` forced for `ViewAll` principals; per-class weak ETag; admin never behind a cache |
| **Configuration foot-guns** | `trusted-proxy` without proxies is an error; ignored keys are errors; non-public visibility requires `trusted-proxy`; the default config is `public` and trusts nobody |
| **DoS of the portal by a slow Jenkins** | per-request client timeout, per-sweep timeout < TTL, `abort_after` per instance, pool bounded at 16, `pace`; engine isolation: the `jenkins` upstream has its own slot and cannot starve GitHub jobs (GR-22) |
| **Secrets** | `secret.Secret` fields; `IsZero` = unset; token never logged (`LogValue` redacts), not in errors, not in URLs; `scp config` dumps iterate typed fields |
| **Information in `/status`** | admin only; shows codes and counts, not tokens, not header values |

**Residual risk found by the verifier: source-NAT in front of the public port.** `access` trusts the TCP peer. With Kubernetes `externalTrafficPolicy: Cluster` (kube-proxy SNAT) or a NodePort/LoadBalancer that reaches the pod directly, an internet client arrives with a node or pod address; if `trusted_proxies` covers that range (it usually does, "the cluster CIDR"), a forged `X-Forwarded-User` is **believed**. The proxy secret is the control that survives this, so for a deployment that cannot guarantee that only the ingress can reach the public port the secret is not "defence in depth" but the primary control: the runbook says so, `no_proxy_secret` is a warning in `config lint` for private-only setups and **an error recommendation (documented, not enforced) whenever `trusted_proxies` is wider than a /24**. A NetworkPolicy admitting only the ingress, `externalTrafficPolicy: Local` or PROXY protocol with a trusted LB address are the structural fixes.

Residual risks (accepted, recorded): SSO-bypass paths (badges for public repos) rely on the proxy to omit identity headers there; if the proxy forwards a client-supplied header on a bypass path, the peer is trusted and the value is believed. The documented proxy snippets **overwrite** (never append) identity headers on every location, and the proxy secret narrows this to proxies that hold the secret. An operator who exposes Jenkins build URLs (`expose_detail_url`) publishes that host name.

Reference proxy configuration for the ops documentation (to be verified against current oauth2-proxy/nginx docs when written; names verified in V11): oauth2-proxy with `--pass-user-headers` (and its default `--skip-auth-strip-headers`), nginx `auth_request` with `auth_request_set $u $upstream_http_x_auth_request_user;` and `proxy_set_header X-Forwarded-User $u;` plus `proxy_set_header X-SCP-Proxy-Secret "<secret>";` on **every** location that reaches SCP, and `proxy_set_header X-Forwarded-User "";` on SSO-bypass locations.

---

## 11. Requirements and guardrail trace

### 11.1 Requirements, gaps and decisions

| Requirement | What is required | Implemented in | How |
|---|---|---|---|
| Group-level integration defaults | Per-group Jenkins job prefix and Snyk project prefix, applied deterministically | 4.1, 6.1, `ResolveJenkins/ResolveSnyk`, `ValidateGroupDefaults` | Deterministic merge descriptor > first group in config order > instance default; typed, validated group keys; each key has exactly one documented effect |
| Jenkins build status | Build state per repository from Jenkins, refreshed every 5 min | 4.4, 4.2, 4.6 | Structured `api/json`, declared targets (opt-in blind discovery), multibranch, off by default, locally rendered badge kind `build` |
| Snyk vulnerability status | Severity counts per repository from Snyk, refreshed every 15 min | 4.5 | REST API with token, one listing per sweep, severity counts, region host; kind `security`; off by default |
| G-08 | Optional authentication/authorisation for non-public content | 4.7, `access` | Trusted-proxy identity, authorizer, operator authorizer, cache safety |
| Visibility policy | Repository visibility (public/private/all) is always backed by access control | 4.7.1, 4.8, 6.2 | Non-public visibility only with `trusted-proxy`; enforced on every surface; startup refusal |
| Visibility filter at discovery (part) | Which repositories enter the catalogue | catalog (`visibility`), `Config.AllowsNonPublic` | this unit supplies the mode check |
| Unknown-state badges (part) | A badge never pretends to know | 4.6 | disabled kind = 404; enabled-no-data = grey 200 |
| Q-03 | v1 public only; trusted-proxy extension | 4.7 | as decided; OIDC later |
| Q-05 | Integrations optional, one `StatusProvider`, structured data, Shields not called | 2.3, 4.4, 4.5 | as decided |
| Q-25 | Snyk REST API with token, optional | 4.5, 6.1 | as decided; org id and regional host keys added (traceability m2) |
| ADR-023 | Visibility classes, 404 not 403, enumeration test | 4.7.4, 4.8, 12 | as decided |
| ADR-025 | Identity spoofing rules, `scp_identity_header_spoof_total`, admin has no identity logic | 4.7.2, 4.7.3, 9 | as decided |
| Badge kinds `build` and `security` (part) | `/badges/{repo}/build.svg` and `/security.svg` embeddable in READMEs | 4.6, `Fact`/`Tone.String()` contract with unit 09 | this unit supplies the facts; the endpoint is `badges`' |
| Integration status in `/status` | Operators see whether each provider is enabled and healthy | 9 `/status`, `Health()`, `Access.Info()` | provider enabled, targets, last success, last error; **the Jenkins URL is not shown** (operator-internal) |
| Dependency health tracking | `lastAttempt`, `lastSuccess`, `lastError`, `consecutiveFailures` per upstream | `Health{LastSweep, LastSuccess, LastError, ConsecutiveFailures, Probed, Failed, FailedCode}`, metrics of section 9, 4.2.1 healthy-sweep rule | real outcomes only; a sweep where every target failed is not a success |
| Configuration keys (part) | Every `integrations.*` key is typed and consumed | 6.1 (`snyk.token` without `enabled` is an error; every key has a consumer) | GR-12: typed, strict, consumed == documented |
| Secrets handling (part) | Snyk token, Jenkins token, proxy secret | `secret.Secret` fields, 10 "Secrets" | redacting type, empty = unset (GR-13) |
| Data-age signal | Every status carries when it was observed and when it was last confirmed | `freshness.Info` per status, `Derive` at read time, `ObservedAt`, `FetchedAt`; `/status` | data age is first-class (GR-26) |

### 11.2 Guardrails (`02-design-guardrails.md`) and the mechanism that enforces each here

| GR | Guardrail as it applies to this unit | Mechanism here |
|---|---|---|
| GR-04 | No decorative keys and no stub services | a key set while its provider is disabled is an error; a group key has one documented effect; the only jobs are real sweeps |
| GR-08 | Target resolution is independent of iteration order | `groupDefault` iterates `Membership.Groups()` (config order); `TestResolveIndependentOfMembershipOrder` |
| GR-12 | One typed, strict, validated configuration | strict decode (platform/config), `Validate()` returns every violation with key paths, documented == consumed test |
| GR-13 | Credentials are a type and travel only in headers | token only in `Authorization`; never in URL/log/error; `Secret` type; `user`+`token` and `proxy_secret_header`+`proxy_secret` set together; `Secret.IsZero` = unset; `snyk.token` required when enabled |
| GR-14 | Neutral defaults | embedded default has no host/URL and enables nothing; `TestDefaultConfigHasNoForeignHosts` |
| GR-15 | URLs are generated from validated, escaped parts | `ParseJobPath` + per-segment `PathEscape` (fuzzed); Snyk paths built from the configured host and fixed path; `links.next` reduced to a cursor |
| GR-17 | The management plane is separate | `access.Middleware` never on the admin listener; `Operator()` only there; `/status` shows no operator-internal URL |
| GR-22 | One client per upstream, bounded by time, no head-of-line blocking | one provider object per upstream, client timeout, sweep `Timeout` < TTL, `abort_after` per instance (4.2.1), own upstream slot in the engine |
| GR-23 | Upstream failures are typed; rate limits are honoured | typed `RateLimitError`/`PermanentError`/`ErrNotFound`/`ErrNoData` + classification table 4.9 |
| GR-24 | The job engine never loses work silently | one global job per provider through the engine (pacing, backoff, parking, panic accounting); unattempted targets recorded as `aborted` |
| GR-25 | Upstream requests are budgeted | declared targets by default, one Snyk listing per sweep, `not_found_ttl` carry, `pace`; `TestBudgetJenkinsSweep` |
| GR-26 | Freshness is data | `freshness.Value[Status]`, TTL sweeps, stale-while-error, `Derive` at read |
| GR-29 | Absence is a typed state | typed `not_found` / `no_content` freshness states; disabled is `StateDisabled`, never `ok` |
| GR-32 | Only sanitised content is served as trusted | no third-party SVG or HTML is ever fetched, stored or served by this unit |
| GR-33 | Badges are rendered locally | badges render from `Fact` with closed vocabularies; an unknown, disabled or invisible kind/repository is 404 (4.6, 4.8) |
| GR-34 | Untrusted documents are parsed safely and within limits | no XML/SVG parsing exists; JSON via `encoding/json` with caps; stored payloads re-validated at warm start |
| GR-35 | Access is an explicit policy; personal data is minimised | 4.7 + 4.8; startup refusal of non-public visibility without `trusted-proxy`; anonymous enumeration test; email never stored; subject only hashed in logs |
| GR-41 | Health derives from outcomes | real sweeps; `scp_integrations_last_success_timestamp_seconds`; healthy-sweep rule of 4.2.1; disabled reported as disabled |

---
## 12. Test plan

Principles (architecture 5.3, 22): no network outside loopback/in-memory, `-race`, no sleeps (virtual time through `testing/synctest`), hand-written fakes named after behaviour (feature tests never import adapters or other features), golden/fixture files small and documented, fuzz seeds run in `go test`, longer fuzz nightly, `goleak` in `TestMain`.

**Deterministic time.** Sweeps are tested inside `synctest.Test` bubbles with a hand-written `fakeProvider` (it satisfies `integrations.StatusProvider`) and an in-memory `ItemStore`; `time.Sleep(5 * time.Minute)` advances virtual time between sweeps so freshness derivation, `not_found_ttl` and pacing are exact and instant. Provider tests (`jenkins`, `snyk`) use `httptest.NewTLSServer` **outside** a bubble (real loopback sockets); once Go 1.27's `httptest.NewTestServer` is adopted they can also run inside a bubble with an in-memory network. The fake GitHub is not needed here: the catalogue enters the tests as a hand-built `readmodel` snapshot (`newEnv`).

### 12.1 Tests that exist in the reference implementation (all pass under `-race`)

| Test (package) | Kind | Asserts |
|---|---|---|
| `TestConfigValidate` (`access`) | table, 18 rows | every validation rule of 6.2 incl. `0.0.0.0/0`, `::/0`, public ranges, straddling ranges (`10.0.0.0/7`), secret/header pairing, reserved and duplicate header names, groups rule without header; **all** violations reported |
| `TestIdentify` (`access`) | table, 18 rows | trusted peer success (v4, v6, IPv4-mapped), untrusted peer, unix socket, wrong/missing/duplicated secret, duplicated user line (smuggling), email fallback, no identity, bidi/control/over-long subject, too many groups, multi-line groups, `X-Auth-Request-*` and underscore spellings stripped; **no identity header survives** in every row |
| `TestPublicModeStripsAndNeverTrusts` | unit | mode `public`: headers stripped, anonymous, reason `mode_public` |
| `TestMiddlewareContextAndMetrics` | unit + in-memory metric reader | principal in context, raw header absent, `Cache-Control: public, max-age=300` -> `private, max-age=300` for `ViewAll`, unchanged for a spoofer; both instruments exist; drop counted |
| `TestAuthorizerTable`, `TestAuthorizerMonotone` | table + property | fail-closed unknown visibility, zero principal not authenticated, group exact/subject case-insensitive, internal == private for every principal and mode, `ClassOf` consistent |
| `FuzzIdentify` | fuzz (10 s run) | an authenticated result has a valid subject/groups and required the correct secret; **no header survives** |
| `TestInfoWarnings` | unit | `no_proxy_secret`, `broad_trusted_range` |
| `TestConfigValidate`, `TestValidateGroupDefaults`, `TestDefaultConfigHasNoForeignHosts` (`integrations`) | table | 6.1 rules; group keys; default has no foreign host and enables nothing |
| `TestParseJobPath`, `FuzzParseJobPath` | table + fuzz (10 s run) | whitelist, traversal, encodings; rendered path has exactly two slashes per segment and no `.`/`..`/`%2F` |
| `TestResolveJenkinsTable`, `TestResolveIndependentOfMembershipOrder`, `TestResolveSnyk` | table | descriptor > first group in config order > instance default; declared vs `discover: all`; archived excluded; invalid descriptor values -> `InvalidTarget`; membership order irrelevant |
| `TestFactOf` | table, 10 rows | tone/message/stale rules of 4.6 |
| `TestNewChecksWiring`, `TestJobsSpecs` | unit | provider set == enabled set; disabled reads `StateDisabled`, no fact; spec kind/upstream/scope/TTL/timeout, key `integrations.jenkins/_` |
| `TestSweepStaleWhileErrorAndRevalidate` | bubble | first sweep changes; second identical sweep is `Unchanged` and uses `Revalidated` only; failure keeps the good value, `Info.State=error`, `Attempts=1`, `ErrCode=transient`, `RecordFailure`; `Derive` = Stale; fact grey with the last message |
| `TestSweepNotFoundCarry` | bubble | a 404 job is not probed again before `not_found_ttl`, probed after it |
| `TestSweepAbortsInstanceAfterConsecutiveFailures` | bubble | provider called exactly `abort_after` times, remaining targets recorded `aborted`, sweep error text |
| `TestSweepErrorClassification`, `TestTransientErrorHasNoBehaviour` | bubble | auth -> `job.Permanent`, 429 -> `job.RateLimited`, both stop after the first call; a plain error matches neither behaviour interface |
| `TestTargetLostIsRemoved`, `TestRemovedRepoDuringSweep` | bubble | descriptor edit removes the entry; a repository removed mid-sweep neither fails the publish nor reappears |
| `TestConcurrentProvidersMerge` | race | 20 interleaved Jenkins/Snyk sweep pairs on 4 repositories lose no entry |
| `TestBulkWrongLengthIsProtocolError`, `TestBulkNoDataIsNoContent` | bubble | bulk contract; `ErrNoData` stored as `no_content` |
| `TestWarmItemRoundTrip` | bubble | store row -> new process slot; corrupt schema and foreign kinds skipped |
| `TestDeadlineStillPublishes` | bubble | a sweep cut by its deadline still publishes finished targets; the rest `error`; sweep returns an error |
| `TestSummaryCap` | unit | 120-rune cap on a rune boundary |
| `TestFetchMapping` (7 fixtures), `TestDetailURLHiddenByDefault`, `TestRequestShapeAndAuth`, `TestMultibranch`, `TestMultibranchMissingBranch` (`jenkins`) | httptest TLS | state/summary/detail URL per fixture; default hides URLs; escaped path (`my%20team`), tree parameter, Basic auth; multibranch second request and `%252F`/`%2520` encoding; missing branch is not an error |
| `TestErrors` (`jenkins`) | table, 9 rows | 404 -> `ErrNotFound`, 401 -> `auth`, **403 -> per-target `forbidden` (not permanent)**, **redirect to `/login` -> `auth`**, 429 + `Retry-After` -> rate limited with the right time, 500 transient, HTML with 200 -> `ErrProtocol`, oversize -> `ErrTooLarge`, bad JSON -> `ErrProtocol` |
| `TestRedirectToOtherHostIsPinned`, `TestRedirectSameHostFollowed`, `TestBadTargetsRejected`, `TestCheck` | httptest TLS | foreign host gets **zero** requests and no `Authorization`; same-host trailing-slash redirect works; traversal jobs and unknown instances rejected; `Check` shape |
| *Added by the verification (15 tests plus two new `TestErrors` rows, all green under `-race`, 40 repetitions of the concurrency ones):* `TestOneAbortedInstanceDoesNotFailTheSweep`, `TestAllInstancesAbortedPrefersPermanent`, `TestForbiddenIsPerTargetUntilARun`, `TestAllTargetsFailedIsNotASuccess`, `TestCorrectedJobIsProbedDespiteNotFoundCarry`, `TestWarmItemRevalidatesPayload`, `TestConfigDefaultsAreAppliedByValidateAndNew`, `TestToneNamesAreTheCrossUnitContract` (`integrations`, `verify_test.go`); `TestDotBranchIsNotRequested` (`jenkins`); `TestHugeCountSaturates` (`snyk`); `TestSubjectCaseFoldIsASCIIOnly`, `TestIdentifyRejectsLineSeparatorsAndStripsAccessToken`, `TestMappedIPv6TrustedRangeIsNormalised`, `TestPrivateWriterIgnoresInformationalStatus`, `TestSpoofLogIsSampledCounterIsNot` (`access`) | httptest / bubble / unit | each pins one verifier finding (V-01..V-17 in the verification report) |
| `TestBulkPagingAndMapping`, `TestFailOnThreshold`, `TestNextLinkNeverFollowed`, `TestPagingLoopAndCap`, `TestErrors`, `TestDetailURLTemplate`, `TestOversizedResponse`, `TestCheck` (`snyk`) | httptest TLS | two-page listing, query parameters, `Token` auth, summed counts and case-insensitive match, inactive projects ignored, thresholds, evil `links.next` never followed (cursor only), same-cursor termination and 200-page cap, status mapping and token absence from errors, detail template, size cap |

### 12.2 Tests to write in the real repository (named, with acceptance)

| Test | Where | Fixture | Acceptance |
|---|---|---|---|
| `E2E-enum-anon` (anonymous visibility enumeration, count level) | `cmd/scp` e2e (`run()` against fake GitHub) | catalogue with 5 public + 3 private repos (one private repo in a group of public ones, one private-only group, one renamed private repo with an alias, a private repo with Jenkins/Snyk statuses) | for **every** route in `routes.Public()` expanded over slugs/groups/files (including badges of every kind, `media`, `llms*`, `sitemap`, `commits?repo=`, `components?purl=` for a package only a private repo declares) with an anonymous principal: no private name, slug, group-only name, count, content or status appears; every response is **byte-identical** to the same request against a deployment holding only the 5 public repositories (status, body, headers except `Date`/request id); invisible `/repos/{private}` and an unknown slug differ in nothing |
| `E2E-enum-etag` | same | same | pins current behaviour of the version/ETag side channel (R-1): documented expected failure until `ClassRev` exists; flips to a hard assertion when it does |
| `E2E-authed-all` | same | same, principal via trusted-proxy headers from a trusted peer | the principal sees all 8 repos, responses are `Cache-Control: private`, class-`all` counts |
| `E2E-spoof` | same | | the same headers from an untrusted peer and from a trusted peer with a wrong secret yield the anonymous responses of `E2E-enum-anon`; `scp_identity_header_spoof_total{reason}` increments by the right reason |
| `TestAccessNotOnAdmin` | `cmd/scp` | | the admin handler chain contains no `access.Middleware` (asserted by wiring test: a request with valid identity headers on the admin listener sees `PrincipalFrom == anonymous`), and admin handlers use `Operator()` |
| `TestConfigDocumentedEqualsConsumed` rows | `cmd/scp` | | every key of 6.1/6.2 is consumed (section 6 "consumer" column) |
| `TestMetricNamesRegistry` rows | `cmd/scp` | in-memory reader | the instruments of section 9 appear with these names, units, label sets |
| `TestSweepAgainstRealEngine` | `cmd/scp` or `refresh` integration | `refresh.Engine` + `Planner` in a bubble, fake providers | `Jobs()` specs register; a `PermanentError` parks the key (24 h), a `RateLimitError` pauses the `jenkins` upstream until `RetryAt` without consuming an attempt and without affecting the `github` upstream |
| `TestBudgetJenkinsSweep` | `integrations` bubble | 1,000 declared targets, provider with 100 ms latency | with defaults (concurrency 4, pace 100 ms) the sweep completes within `sweep_timeout`; call count == 1,000; a second sweep within `not_found_ttl` makes 0 calls for 404 jobs |
| `FuzzNextCursor` | `snyk` | seeds: string, `{"href":..}`, null, hostile URL | never panics; result <= 512 bytes; never a URL |
| `TestJenkinsRecordedFixtures` | `jenkins` (M0) | **recorded** from a real Jenkins (classic freestyle, pipeline, multibranch parent and branch, branch with `/`, folder, disabled, never built, anonymous-denied, login redirect) | the provider maps each as in 4.4; V10 verified; regeneration script documented |
| `TestSnykRecordedFixtures` | `snyk` (M0) | recorded listing pages from a real org in each available region | mapping as in 4.5; resolves the `Retry-After`, ignored-issue and `origins` open items |
| `TestLogsNeverContainSecrets` | `integrations`, `access` | token/secret sentinels | log output, error strings, metric labels contain none of the sentinels |
| `ArchTest rows` | `internal/archtest` | | rows for `integrations`, `integrations/jenkins`, `integrations/snyk`, `access` as in 2.1 (with D-1 applied) |
| `TestMain goleak` | `integrations`, `jenkins` | | no goroutine outlives a sweep |
| `TestRemoteAddrUntouchedByChain` | `cmd/scp` | the real public chain with a recording handler | `r.RemoteAddr` seen by `access.Middleware` equals the TCP peer for a request carrying `X-Forwarded-For`/`Forwarded` from a trusted and from an untrusted peer (no middleware before `access` rewrites it) |
| `TestStatusContractShape` | `overview` + `cmd/scp` | a repository with a Jenkins and a Snyk status | the `statuses[]` JSON validates against `ProviderStatus` of `api/openapi-public.yaml`: seven required keys, `counts: {}` for Jenkins, `detailUrl: null` by default, `freshness.kind == "integrations.<p>"`, `lastErrorCode` namespaced |
| `TestBadgeToneBridge` | `cmd/scp/internal/bridge` | one Fact per `Tone` | `integrations.Tone.String()` maps to the `badges.Tone` of the same name; none maps to neutral by accident |
| `TestSpoofViaSNATSimulated` | `cmd/scp` e2e | peer inside `trusted_proxies`, wrong or missing secret | anonymous responses, reason `bad_secret`; documents that the secret is the control when the peer address is shared |

### 12.3 Fixture policy

`internal/integrations/testdata/jenkins/*.json` and `testdata/snyk/projects-page{1,2}.json` in the reference implementation are **hand-written from the API schemas** (Jenkins `tree=` output shape; the Snyk OpenAPI 2024-10-15 `ProjectAttributes`/`LatestIssueCounts`). They prove the mapping code, not the real upstream; milestone M0 replaces or complements them with recorded responses and a `testdata/README` explaining the regeneration command. Each fixture is small (under 1 KiB) and each test names the one behaviour it pins.

---

## 13. Implementation tasks

Ordered; each task lists files and a definition of done (DoD). Dependencies on other units are in **bold**. Milestones follow the architecture's M0 (spikes) then feature milestones.

### I-0: Spikes (no production code; blocks nothing else except the fixtures)

| # | Task | DoD |
|---|---|---|
| I-0.1 | Record Jenkins fixtures from a real instance (or a Jenkins container with Docker): classic, pipeline, multibranch with slashed branch, folder, disabled, never built, anonymous-denied | files in `testdata/jenkins/recorded/`, V10 answered in `docs`/this file, `TestJenkinsRecordedFixtures` green |
| I-0.2 | Record Snyk project listings (one org, any region): confirm `display_name` = `owner/repo` for GitHub-integrated projects, `latest_issue_counts` vs ignored issues, `origins` values, whether 429 carries `Retry-After` | answers written into section 15, defaults of `origins`/`Retry-After` handling confirmed or changed |
| I-0.3 | Confirm oauth2-proxy/nginx snippet (section 10) against current docs | snippet in `docs/operations/` |

### I-1: `access` (needs **`httpapi/authz`** with `WithPrincipal`, **`platform/secret`**)

| # | Task | Files | DoD |
|---|---|---|---|
| I-1.1 | Config, defaults, validation, prefix parsing | `access/config.go` | `TestConfigValidate` green; `ParsePrefixes` rejects `/0`, public ranges |
| I-1.2 | Identity extraction and stripping | `access/identity.go` | `TestIdentify`, `FuzzIdentify` green (30 s fuzz in CI touched-parser job) |
| I-1.3 | Authorizer, operator authorizer | `access/authorizer.go` | `TestAuthorizerTable`, `TestAuthorizerMonotone` green |
| I-1.4 | Middleware, metrics, cache wrapper, `Info` | `access/access.go` | `TestMiddlewareContextAndMetrics`, `TestInfoWarnings` green; wrapper preserves `Flush`/`Unwrap` (SSE test in `live` unit) |
| I-1.5 | `archtest` row, depguard rule, `forbidigo` ban on raw identity header reads outside `access` | `internal/archtest`, `.golangci.yml` | archtest green with D-1 and D-9 applied |
| I-1.6 | Chain test: nothing before `access.Middleware` rewrites `r.RemoteAddr` | `cmd/scp` | `TestRemoteAddrUntouchedByChain` green |

### I-2: `integrations` core (needs **`readmodel`** registry, **`store`**, **`job`**, **`catalog`** repo slot and membership global)

| # | Task | Files | DoD |
|---|---|---|---|
| I-2.1 | Config + validation + `ValidateGroupDefaults` + `InstanceNames` | `integrations/config.go` | config tests green |
| I-2.2 | Status model, payload codec, `FactOf` | `status.go` | `TestFactOf`, `TestWarmItemRoundTrip` green |
| I-2.3 | `ParseJobPath`, `ResolveJenkins`, `ResolveSnyk` | `target.go` | resolution tests, `FuzzParseJobPath` green |
| I-2.4 | Provider contract and typed errors | `provider.go` | `TestSweepErrorClassification`, `TestTransientErrorHasNoBehaviour` green |
| I-2.5 | `Service`: `New`, `Jobs`, `Sweep`, `fetchAll`/`fetchPool`, reduce, persist, publish, health, instruments | `service.go` | all `integrations` tests green under `-race`; `TestConcurrentProvidersMerge` x 50 iterations clean |
| I-2.6 | Metric-name constants and registry test row | `service.go`, `cmd/scp` test | `TestMetricNamesRegistry` includes section 9 |
| I-2.7 | `goleak` `TestMain` in `integrations` and `jenkins`; `Config.WithDefaults` used by the root loader's lint path | `integrations_test.go`, `jenkins_test.go` | no leaked goroutine; `scp config lint` reports a missing Jenkins key without a spurious `discover` error |

### I-3: Providers (needs I-2.4)

| # | Task | Files | DoD |
|---|---|---|---|
| I-3.1 | Jenkins provider: URL building, multibranch, mapping, errors, redirect pinning, `Check` | `jenkins/jenkins.go` | jenkins tests green; recorded-fixture test after I-0.1 |
| I-3.2 | Snyk provider: listing, paging (cursor only), aggregation, mapping, errors, detail template, `Check` | `snyk/snyk.go` | snyk tests green; `FuzzNextCursor` written; recorded-fixture test after I-0.2 |

### I-4: Wiring and cross-unit contracts (needs **`cmd/scp`**, **`platform/netguard`**, **`refresh`**, **`badges`**, **`overview`**, **`ops`**, **`docs`**, **`catalog`**)

| # | Task | Files | DoD |
|---|---|---|---|
| I-4.1 | Construct `access` and the providers; netguard clients per instance; `Jobs()` registered; `AddUpstream` for `jenkins`/`snyk`; slot registered | `cmd/scp/wire_features.go`, `wire_jobs.go` | e2e boots with both providers against fake Jenkins/Snyk servers |
| I-4.2 | Public chain order; admin chain without access; `Operator()` | `cmd/scp/wire_http.go` | `TestAccessNotOnAdmin` green |
| I-4.3 | Bridges: `IntegrationOverrides` (docs descriptor -> `map[provider]map[key]value`), `BadgeFacts` additions (`build`, `security`), health bridge for `ops` | `cmd/scp/internal/bridge` | bridge unit tests; `assert.go` lines of 2.3 compile |
| I-4.4 | Root `Validate()` calls: `Access.AllowsNonPublic` vs catalog visibility, `ValidateGroupDefaults`, instance names to `docs` | `cmd/scp/config.go` | config lint tests |
| I-4.5 | Warm load calls `Service.WarmItem` | `cmd/scp/wire_readmodel.go` | restart e2e: statuses present before the first sweep |
| I-4.6 | Consumers: `overview` composes `statuses[]` and llms lines (no `DetailURL` in llms); `badges` kinds `build`/`security`; `ops` `/status` sections; SPA columns hidden when `Providers()` is empty | respective units | contract tests in each unit; link crawl covers the badge URLs |
| I-4.7 | `scp doctor` calls `Checker.Check` per provider | `cmd/scp/cmd_doctor.go` | doctor prints instance, host, result code |
| I-4.8 | Reference docs: generated config/metrics reference rows; ops runbook (proxy snippets, NetworkPolicy, secret rotation, "Jenkins shows unknown: what to check") | `docs/` | docs lint green |

### I-5: Verification (needs the e2e harness)

| # | Task | DoD |
|---|---|---|
| I-5.1 | `E2E-enum-anon`, `E2E-authed-all`, `E2E-spoof`, `E2E-enum-etag` | green in CI (`E2E-enum-etag` pinned as expected-failure until R-1 lands) |
| I-5.2 | `TestSweepAgainstRealEngine`, `TestBudgetJenkinsSweep` | green; budget numbers recorded in the ops runbook |
| I-5.3 | Nightly: `FuzzIdentify`, `FuzzParseJobPath`, `FuzzNextCursor` 5 min each | green |

---

## 14. Deviations proposed

Evidence-based; designing continues against the architecture as written. None blocks the unit.

| ID | Proposed change | Evidence | Fallback if rejected |
|---|---|---|---|
| **D-1** | Extend the `access` row of the architecture matrix (5.2) with `platform/secret` and the OpenTelemetry **API** modules (`otel/metric`, `otel/attribute`); the row currently lists only `scm`, `httpapi`, `httpapi/authz`, `platform/logx` and "none" for third-party. Also add the label `reason` to `scp_identity_header_spoof_total` | 5.1(2) and ADR-001 say features **and services** (`webhook access ops live`) import `platform/{secret,logx}` and the otel API; G11 requires instruments from an injected `Meter`; the proxy secret is a `secret.Secret` loaded by the config machinery. The row is stricter than the rule text it is supposed to instantiate. The reference code imports `httpapi/authz`, `platform/secret`, `scm`, `otel/attribute`, `otel/metric` only (it does **not** use `httpapi`, `logx`) | `access.Config.Identity.ProxySecret` becomes `string` read by `cmd/scp` from the file (loses redaction in the struct), counters through an injected `func(reason string)` (loses G11 uniformity) |
| **D-2** | `httpapi/authz` exports `func WithPrincipal(ctx context.Context, p Principal) context.Context` next to `PrincipalFrom`, and `PrincipalFrom` returns `Principal{Anonymous: true}` for a context without one | 8.14 lists `PrincipalFrom` and `ClassOf` but no setter; a middleware cannot establish a principal without one; the `Anonymous` zero value is `false`, which is unsafe | `access` keeps its own unexported context key and `authz.PrincipalFrom` is implemented by an interface hook: worse (two keys) |
| **D-3** | `integrations.Target` gains `Instance string` and `Branch string` (additive to 8.9's `Repo, Job, Project`) | the descriptor schema (ADR-006) selects an `instance`; multibranch needs the ref; "never hostnames" still holds | encode `instance` into `Job` as `"instance:path"`: stringly typed |
| **D-4** | Slot value type is the named map `integrations.Statuses` (`map[string]freshness.Value[Status]`) rather than `integrations.Status` | 9.1 says `integrations.Status (per provider map)`; every fetched item carries freshness (principle 7), so the map's values are `freshness.Value[Status]` | none needed (cosmetic) |
| **D-5** | `integrations` adds two optional interfaces next to `StatusProvider`: `BulkProvider` (Snyk lists once for the organisation) and `Checker` (`scp doctor`); and `State` zero value is `StateUnknown` | Snyk's REST model is org-wide; per-target `Fetch` would cost one listing per repository (about 1,000 calls per sweep instead of about 30). Doctor is D-17. The zero value must never read as healthy | per-target Snyk calls with `names=`/`target_id=` filters: ~30x the requests |
| **D-7** | Three extra bridges in `cmd/scp/internal/bridge`: `IntegrationOverrides`, a `BadgeFacts` extension for `build`/`security`, and an integrations health bridge for `ops` (the architecture's list names `BadgeFacts, PlanSink, Tracker, RateGate, Retrier, InstallationStatus`) | mechanism B is unchanged (consumer-defined interface, bridge in `cmd/scp`); `integrations` cannot import `docs`, `ops` cannot import `integrations` | none needed |
| **D-8** | Cross-unit requirement R-1: `readmodel`/`live`/`httpapi` provide a per-`ViewClass` change counter used for the public ETag and `api.snapshot` | 4.8: the global version leaks private activity to anonymous observers | accept as documented low-severity leak |
| **D-9** | `_test.go` files of `access` and `integrations` may import `go.opentelemetry.io/otel/sdk/metric` (+ `metricdata`) for an in-memory reader (`archtest` allows the SDK in `_test.go` of features; G6 stays for non-test code) | 5.3 G6 says "OTel SDK only in `platform/telemetry`" and G11 asks for a `cmd/scp` test with an in-memory reader; the reference tests of `access` assert instrument names and label sets with `metric.NewManualReader`, as `04-refresh-webhook` does. The matrix text "test libraries" does not say whether the SDK counts | the same assertions move to the `cmd/scp` registry test (`TestMetricNamesRegistry`), losing the per-package check of the drop counter's `reason` label |
| **D-10** | `Status.DetailURL` may be `http://` when the instance has `allow_http: true` | 8.9 says "https only, host must match the configured instance"; an operator who explicitly allowed plain http for a private Jenkins would otherwise get no link even with `expose_detail_url` | drop the link for `allow_http` instances (one `if`) |

---

## 15. Open questions

| # | Question | Owner / how to close | Interim decision |
|---|---|---|---|
| Q-1 | R-1: who implements the per-class change counter, and is a public-class ETag allowed to stay stable while private repositories change? | architecture owner / `readmodel`, `live` designers | accept the leak, `E2E-enum-etag` pinned |
| Q-2 | Jenkins: exact `_class` strings for multibranch/folder jobs, `%252F` encoding of slashed branches, `lastCompletedBuild` availability on pipeline jobs, behaviour of anonymous-denied instances (302 to login vs 403) | spike I-0.1 | heuristics in 4.4; fixtures record the truth |
| Q-3 | Snyk: do `meta.latest_issue_counts` exclude ignored issues; legal `origins` values; is `Retry-After` sent with 429; sunset policy of `api_version` `2024-10-15` (what happens when Snyk retires a GA version) | spike I-0.2; `scp doctor` should warn on 400/410 for the version | defaults: no `origins` filter, 60 s on 429, `api_version` configurable |
| Q-4 | Snyk detail link: the app URL format needs the organisation slug, which the projects response does not carry | operator supplies `detail_url_template`; or a later `GET /orgs/{id}` lookup | no link by default |
| Q-5 | Warm-start convention: the architecture does not say how `cmd/scp` dispatches stored items to features. This design offers `WarmItem(b, id, item)` with the caller resolving the slug | `cmd/scp` / `store` designers | adopt, adjust signature if the convention differs |
| Q-6 | Persisted plan rows for global keys (`integrations.jenkins/_`): `PlanRow` is keyed by `ItemKey{Repo, Kind}` and a global key has no repository | `refresh`/`store` designers | if unsupported, global keys restart "due immediately" after a restart, which is harmless for sweeps |
| Q-7 | Should `access.trusted_proxies` default to `server.public.trusted_proxies` when unset? | owner | no: explicit, different trust statements (4.7.3) |
| Q-8 | Should the SPA show "signed in as" or an "all repositories" indicator in trusted-proxy mode (it needs a `viewer` object on `api.site`)? | `overview`/SPA designers; costs a privacy review (subject display) | not in v1 |
| Q-9 | Kubernetes: the proxy-secret rotation procedure (two secrets accepted during rotation) | ops runbook | v1 supports exactly one secret; rotate by restarting both sides |
| Q-10 | Group-level or descriptor-level `ref` for Jenkins multibranch (branch other than the default branch) | owner | default branch only (Q-08) |
| Q-11 | A second Snyk organisation | owner | one org in v1; a list is additive |
| Q-12 | Owner confirmation of the defaults: Jenkins/Snyk are off until configured, Jenkins is "declared only" by default (no blind probe of every repository), the group key `project_prefix` selects the Snyk target name, `DetailURL`s are hidden by default | owner | recorded in 6.1 and 11.1 |
| Q-13 | Jenkins 403 semantics: confirm on the recorded fixtures (I-0.1) that an unauthorised job answers 403 (not 404 "Job/Discover") and that anonymous-denied instances answer 403 or 302 to `/login` | spike I-0.1 | 403 per target, `abort_after` in a row = instance `auth`; `/login` redirect = `auth` |
| Q-14 | Should the per-repository entry be published only when its answer changed or its freshness needs it (ETag/SSE churn of 4.3)? | `readmodel`/`live` designers after profiling | publish every sweep |
| Q-15 | `authz.PublicOnly{}` exists in unit 06's authz; `access` has its own public-only authorizer. Use `PublicOnly` for mode `public` and delete the duplicate? | unit 06 / `access` owner | keep the duplicate (identical behaviour, tested), switch when 06 is frozen |

---

## 16. Verification log (independent verifier, 2026-10-14)

Scope: conformance with `10-architecture.md` (matrix 5.2, signatures 8.x, wire contract of unit 07, siblings 02/06/09), traceability against the requirements and guardrails, technical correctness (compiled and run in `/private/tmp/.../scratchpad/verify-14-integrations-access/`, Go 1.27.1, `go vet`, `go test -race -count=3`, 40 repetitions of the concurrency tests, `FuzzIdentify` 8 s, `FuzzParseJobPath` 5 s), live facts, security, testability. Full report with evidence: `30-verification/design-14-integrations-access.md`. The companion code was corrected and extended; all tests pass.

**Re-verified unchanged.** Import directions of the four packages by `go list` against matrix 5.2 (only D-1 and the test-only D-9 differ from the matrix); 8.3 `job` behaviour interfaces (`RetryAt`, `Permanent() bool`) satisfied structurally; 8.4 `Apply`/`Put` semantics and `ErrUnknownRepo` skip; 8.5 `ItemStore` is a subset of `store.Store`; the 15-section structure; Snyk regional hosts, `expand`, `origins`, `meta.latest_issue_counts`, `limit` (V1, V15); oauth2-proxy header names (V16).

**Findings fixed (22).** V-01 per-job HTTP 403 no longer aborts and parks the whole Jenkins sweep; V-02 one dead instance of several no longer fails the healthy instance's sweep; V-03 a sweep in which every target failed is no longer "healthy", `ConsecutiveFailures`/`Failed` added; V-04 Snyk counts above 1e6 wrapped to 0 (fail open), now saturate; V-05 Unicode case folding of subjects (Kelvin sign) now ASCII-only; V-06 warm-start payload (`detailUrl`, counts, state) is validated as untrusted; V-07 per-request warn log under spoofing is sampled (the rate limiter is after `access`); V-08 `X-Forwarded-Access-Token` stripped, Zl/Zp/Co rejected in subjects; V-09 section 7 aligned with unit 07's `ProviderStatus` (required keys, `freshness.kind`, namespaced `lastErrorCode`); V-10 wiring snippet used a `netguard` API that does not exist, replaced by unit 02's; V-11 per-instance defaults had no application point (`WithDefaults`); V-12 corrected descriptor was hidden by the 404 carry for up to 1 h; V-13 `Cache-Control` rewrite consumed by a 1xx status; V-14 login redirect now `auth`; V-15 canonical SHA used the numeric `State`, `New` accepted a nil publisher, Snyk ports and size unchecked, `..` branch, unreused connections, IPv4-mapped trusted prefixes; V-16 trace table lacked the badge-kind, admin-status, dependency-health, configuration-key and secrets rows; V-17 Snyk "1,620/min" and "tokens are region-specific" were unverified claims presented as facts; V-18 deviations D-9 (SDK in tests) and D-10 (`http` detail URL) added; V-19 requirement that nothing rewrites `RemoteAddr` and the SNAT residual risk; V-20 ETag/SSE churn of per-sweep publishing recorded (Q-14); V-21 payload "absent on Revalidated" wording; V-22 tone names are a cross-unit contract with unit 09 (a mismatch would grey every badge); `goleak` claim corrected.

**Left open (owner or spike).** R-1 per-class change counter (Q-1); recorded fixtures and Jenkins `403` versus `404` semantics (Q-2, Q-13, I-0.1); Snyk `Retry-After`, ignored issues, `origins` values (Q-3, I-0.2); publish-only-on-change (Q-14); `authz.PublicOnly` reuse (Q-15).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: the stand-in `readmodel` registers with the real options; tests updated (CD-IF-11); `integrations.status` is a `Slot[Statuses]` map (CD-SL-01); metric unit fixed (CD-MT-03).
- Requirement R-09-1/R-OVR-1: `integrations.StatusOf` returns `Value[Status]`, bridged for `overview` and `badges`.
- Open: Jenkins and Snyk fixtures are hand-written httptest; recorded fixtures remain pending (fixtures matrix). `access` imports `secret` and the otel API (A-30).
