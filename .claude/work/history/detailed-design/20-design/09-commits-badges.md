# 09 - Commits and badges (`internal/commits`, `internal/badges`)

Design unit 09. Date of writing: 2026-10-14. Status: ready to implement.

Precedence used throughout: `04-decisions.md` (including the section 7 amendments A-01..A-26) > `10-architecture.md` > `11-adr/`. Where this document differs from the architecture it says so in section 14 with evidence. Neighbouring unit designs this document is aligned with (read, not restated): `03-readmodel-store.md`, `04-refresh-webhook.md`, `14-integrations-access.md`, `api/openapi-public.yaml`, `api/common.yaml`.

**What was compile-checked.** Every Go type, signature and function body quoted in this document comes from the reference prototype in `20-design/09-commits-badges.assets/` (module `github.com/cantara/source-code-portal`, `go 1.27.1`, darwin/arm64; `README.txt` there lists the files and the commands). It contains the two packages in full and, **copied unchanged from the sibling units' design assets as they stood on 2026-10-14 after those units' own verification** (the author's first copy predated that verification and had drifted: `store.KeyOf` folding, the ETag digest convention, `httpapi.RequestFrom`, `scm.CleanLine`/`CompareCommits`/`RepoRef.KeyParts` did not exist in it; see V-04 in the verification log), the real kernel and infrastructure code they depend on: `scm`, `freshness`, `job`, `routes`, `reactor` (unit 01), `readmodel` and the `store` contract files (unit 03), `httpapi`, `httpapi/authz`, `httpapi/openapi` (generated `common.gen.go`) and `logx` (unit 06), and the **generated** `commits/commitsapi` strict server of the final contract (a byte copy of unit 07's `split` output with the import path rewritten; oapi-codegen v2.8.0). So the consumer interfaces, `job.Spec`, `readmodel.Register/Put/NewPublisher/Apply`, `freshness.Info.AfterSuccess/AfterRevalidated/AfterFailure/Derive`, `scm.NewMembership/CleanLine/CompareCommits`, `store.KeyOf` and the item/commit validators, `authz.LookupRepo/ClassOf`, `httpapi.ParsePage/EncodeCursor/DecodeCursor/QueryFingerprint/WeakETag/WithDigest/FreshnessDigest/NotModified/NewFreshness/NewPageInfo/NextLink/Err/NotReady/RequestFrom/WithRequest/StrictErrors/BindingError` and the generated `commitsapi.StrictServerInterface` used below are the real ones, not stubs. Result (verification run, 2026-10-14): `go vet ./...` clean; `go test -race -count=1 ./...` passes (51 top-level tests, 60 with subtests; coverage 85.4 % commits, 81.7 % badges); both fuzz targets ran 15 s each on the final code without a finding (`FuzzMerge` 1,285,943 executions, `FuzzRenderWellFormed` 1,970,353); the width generator `tools/badgewidths` (in the single nested module `tools/go.mod`, `golang.org/x/image` v0.47.0) reproduces `widths_gen.go` byte for byte.

Not compiled against real code (the sibling artefacts do not exist yet or are not compilable standalone): the engine (`refresh`, unit 04), the GitHub adapter (`github.Client`, unit 05), `sqlite.Store` (unit 03 has it; the fake `CommitStore` in the tests stands in for it and, since the verification, enforces the store contract: folded `RepoKey`, `ValidateCommit`, `ValidateItem`, `ErrNoItem` on `Revalidated`), the catalogue (unit 08; its slot value types are kernel types and compile), the integrations bridge (unit 14), `bridge.BadgeFacts`, and `cmd/scp` wiring. The HTTP layer is exercised **through the generated strict handler** (`commitsapi.NewStrictHandlerWithOptions` behind `httpapi.WithRequest`); `Service.ListCommits` is the compile-checked adapter that section 7.1 shows. The kernel copies are *not* owned by this unit: when units 01/03/06/07 change, task C0/B0 below re-runs the build. The prototype is a reference implementation, not a deliverable; the implementation tasks of section 13 re-create it in the real tree (its file names differ from section 1.4 in one respect: `service.go` holds `run`, `React`, `Restore`, `Coverage`, `Recent`; `normalise.go`, `record.go` hold what section 1.4 calls `ingest.go` and `retention.go`; section 1.4 is the target layout).
---

## 1. Purpose, scope, non-goals, exact packages

### 1.1 Purpose

`commits` keeps, per repository, a bounded, validated, always-sorted ring of the newest commits of the default branch, serves the portal-wide / per-group / per-repository commit log through one cursor-paged, ETag-validated endpoint, and precomputes the dashboard's latest-commits feed inside every snapshot publish (GR-28, GR-30).

`badges` renders the portal's own flat-square SVG badges locally from structured facts (ADR-011): it never proxies, parses, or interpolates third-party SVG (GR-33), is accessible (WCAG 2.2 contrast, `role="img"`, accessible name), and is cheap enough to sit behind GitHub's image proxy (Camo) and a README on thousands of pages.

### 1.2 In scope

| Area | Commits | Badges |
|---|---|---|
| Ingestion | job `commits.refresh` (per repository), head-sweep skip, conditional fetch, normalisation, ring persistence, warm-start restore, webhook / head-changed reaction | none (no job, no store, no state of its own) |
| Retention | ring of `commits.keep` (default 30) per repository, membership record, eviction accounting | none |
| Read model | slot `commits.view`, global `commits.latest` (per view class), projector, validator | reads other features' slots only through `badges.Facts` |
| API | `GET /api/v1/commits` (route `api.commits`): k-way merge, keyset cursor, `repo`/`group` filters, freshness block | `GET /badges/{repo}/{file}`, `GET /badges/groups/{groupId}/{file}` |
| Rendering | none (the SPA groups by day in the viewer's zone, section 4.9) | SVG generator, width table, palette, kinds and states, text sanitation, cache headers |
| Observability | 9 metrics, 1 span, structured logs | 2 metrics |

### 1.3 Non-goals

* No `author` filter, no search, no per-branch log (Q-18: default branch only).
* No history beyond the ring (30 per repository, GR-30); no pagination into GitHub's history. "Load more" past the ring is a link to GitHub's commit page (`https://github.com/{owner}/{repo}/commits/{branch}`, built by the SPA from the repository's `htmlUrl`).
* No author e-mail addresses are ever fetched into a stored field (GR-35): the adapter's `scm.Commit` has no e-mail, and `commits` stores only SHA, subject, login, display name, URL, date.
* No server-side day grouping or time-zone handling (A-25 / API unit: the SPA groups by calendar day in the viewer's zone, timestamps are UTC; a server-side grouping would have to pick one zone for every viewer, GR-38).
* No dynamic badge text from a request. Label, colours and structure come from closed sets; the only free text on a badge is data (a count, a tag, a licence id, a language) that has been sanitised and is XML-escaped by construction.
* No third-party SVG (Shields.io, Jenkins, Snyk or any other) is fetched, parsed or served (GR-33, GR-25). Integrations contribute a *fact* (message + tone), not an image.
* No custom badge styles (`flat`, `plastic`, `for-the-badge`), no logos, no `?label=` / `?color=` parameters, no PNG. (Rationale: each parameter is an injection and cache-key surface; Camo ignores nothing it can cache by URL.)
* No SSE publishing from these packages (v1.1 `live` consumes snapshot generations; commits needs nothing from it).
* The oapi-generated types (`commits/commitsapi`) and the OpenAPI fragment are owned by the API unit; this unit supplies the handler behind the generated route.

### 1.4 Exact packages and files

```
internal/commits/                      rank 4 feature
    config.go       Config, DefaultConfig, Validate
    model.go        View, CommitRef, FeedInfo, Latest, slot/global/item/job names
    ports.go        CommitLister, CommitStore (consumer-defined)
    ingest.go       normalise (+ dropStats, cleanSHA/cleanLine/cleanLogin/cleanURL), the job Run
    retention.go    ringRecord / rowRecord codec, Restore, evicted accounting
    merge.go        Compare, Position, Source, Entry, Merge, PositionOf
    project.go      Slots, RegisterSlots, Projection (Projector, Validator), feed summaries
    jobs.go         Service, Deps, New (rejects nil dependencies), Jobs(), Coverage, Recent (read side of overview, R-09-1), goneError/permanentError, errCode
    react.go        React (also records the push generation that bypasses the head-sweep skip, 4.5)
    handler.go      ListCommits (the generated StrictServerInterface method), List(ctx, r, ListQuery) (the operation), cursor codec, Mount (generated registration)
    metrics.go      instruments
    commitsapi/     generated (owned by the API unit; spec in 20-design/api/openapi-public.yaml)

internal/badges/                       rank 4 feature (no api package: server-only SVG routes)
    config.go       Config, DefaultConfig, Validate
    kinds.go        Kind, State, Spec, Evaluate, EvaluateRepos, ParseFile, Alt
    facts.go        Facts, RepoFacts, Fact (consumer-defined)
    palette.go      Tone, Color, Palette (closed colour set)
    render.go       Badge, Render (encoding/xml)
    text.go         cleanText
    format.go       formatCount, relativeDay
    widths.go       advanceUnits, textWidth
    widths_gen.go   generated, committed (CI drift check)
    handler.go      Deps, Service, New, Repo(), Group(), headers
tools/                                 the single nested module of the architecture (tools/go.mod; x/image never enters the product module graph)
    badgewidths/    main.go  font/DejaVuSans.ttf  font/LICENSE-DejaVu.txt
```

File names follow the architecture tree (section 7 of `10-architecture.md`) except the additions listed as D-8 in section 14. The generator is a package of the architecture's single nested module `tools/go.mod` (architecture 4.5 and 5.2: "`tools/` is a separate nested Go module (`tools/go.mod`)"); the first draft gave it a second `go.mod` of its own (V-14).

---

## 2. Architecture position

### 2.1 Rank and allowed imports

Both packages are rank 4 (architecture 5.2).

| Package | May import | Third-party | Imported by |
|---|---|---|---|
| `commits` | K4 = `scm`, `freshness`, `job`, `routes`, `readmodel`, `store`, `httpapi`, `httpapi/authz`, `platform/secret`, `platform/logx`; own `commits/commitsapi`; **plus `httpapi/openapi` (rank 0, generated shared schema types, D-12)** | otel API modules only (`go.opentelemetry.io/otel/{attribute,metric,trace}`) | `overview` (rank 5, root import for `commits.View`/`Latest`), `cmd/scp` (composition) |
| `badges` | K4 (uses `scm`, `freshness`, `readmodel`, `httpapi`, `httpapi/authz`) | otel API (metric, attribute); `encoding/xml`, `crypto/sha256` from the standard library | `cmd/scp` only |

`go list -f '{{.Imports}}'` on the prototype packages (2026-10-14, after the verification) shows exactly: `commits` -> `commits/commitsapi`, `freshness`, `httpapi`, `httpapi/authz`, `httpapi/openapi` (D-12), `job`, `readmodel`, `scm`, `store` and the three otel API packages; `badges` -> `freshness`, `httpapi`, `httpapi/authz`, `readmodel`, `scm`, otel `attribute` and `metric`, plus `crypto/sha256` and `encoding/xml` from the standard library. **Test files obey the same row** (architecture 5.2, last paragraph): `go list -f '{{.TestImports}}'` shows only packages of the row plus `otel/{metric,trace}/noop` and stdlib. The first draft's `assert_test.go` imported `reactor`, which is not in the `commits` row (rule 2 of 5.1: a feature never imports `reactor`); the assertion that `(*Service).React` is a `reactor.Func` belongs to `cmd/scp/assert.go` (V-03). Neither package imports another feature. In particular `badges` does **not** import `commits`, `catalog`, `manifests` or `integrations`: everything it displays arrives through the consumer-defined `badges.Facts` (mechanism B), satisfied by `bridge.BadgeFacts` in `cmd/scp/internal/bridge`.

Not imported on purpose: `net/http` clients, `store/sqlite`, any `github` package (the lister is an interface), `platform/telemetry` (instruments come through injected `metric.Meter` / `trace.Tracer`, A-24), `time/tzdata` (no zone logic server-side).

### 2.2 Communication mechanisms used

| Mechanism | Commits | Badges |
|---|---|---|
| A. Snapshot slots | owns slot `commits.view` and global `commits.latest`; reads `catalog.repo`, `catalog.stats`, `catalog.membership`, `catalog.aliases` | reads `catalog.repo`, `catalog.membership`, `catalog.aliases`; the facts of other slots arrive through the bridge |
| B. Consumer interfaces | `CommitLister` (direct: `*github.Client`), `CommitStore` (direct: `*sqlite.Store`) | `Facts` (bridged: `bridge.BadgeFacts`) |
| C. Job registration | `Jobs() []job.Spec` returns `commits.refresh` | none |
| D. Events | `React(ctx, ev, enq)` registered on `reactor.Fan` for `EventPush` and `EventHeadChanged` | none |

### 2.3 Consumer-defined interfaces (exact signatures)

Commits (`ports.go`; compile-checked):

```go
// CommitLister is satisfied directly by *github.Client (architecture section 8.7). Contract the adapter must keep:
//   - the result is the newest n commits of ref in branch order (tip first), not re-sorted by date;
//   - an empty repository (GitHub answers 409) is an empty slice and a nil error;
//   - v.SHA is the head SHA of the previous result; when the adapter learns the head is unchanged it may return
//     NotModified (no value). Validators.SHA of a fresh value is the head SHA it observed;
//   - an error that wraps scm.ErrNotFound for the repository itself also satisfies job.Gone.
type CommitLister interface {
	LatestCommits(ctx context.Context, r scm.RepoRef, ref string, n int, v scm.Validators) (scm.Result[[]scm.Commit], error)
}

// CommitStore is the slice of store.Store that commits uses (carved out by this consumer; *sqlite.Store and the
// test fake satisfy it).
type CommitStore interface {
	AppendCommits(ctx context.Context, repo store.RepoKey, cs []store.CommitRow, keep int) error
	LoadCommits(ctx context.Context, repo store.RepoKey, limit int) ([]store.CommitRow, error)
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}
```

`CommitLister` is identical to architecture 8.7. The only additions are the contract sentences in its doc comment, which unit 05's design (`05-github-adapter.md` 4.9) already satisfies (D-2, Q-1).

Badges (`facts.go`; compile-checked):

```go
// Facts is the consumer-defined view of everything a badge can show; the satisfier is bridge.BadgeFacts in
// cmd/scp/internal/bridge, which reads the catalog, commits, manifests and integrations slots (architecture 6.2).
// The bool is false when the repository has no entry in s.
type Facts interface {
	Facts(s *readmodel.Snapshot, slug scm.Slug) (RepoFacts, bool)
}

type RepoFacts struct {
	Repo          scm.Repository
	Stats         freshness.Value[scm.RepoStats]
	LastCommit    freshness.Value[time.Time] // newest commit time; State no_content for an empty repository
	Language      freshness.Value[string]    // primary language of the manifests; State unsupported when none was recognised
	BuildTool     freshness.Value[string]
	LatestVersion freshness.Value[string] // declared version of the repository's primary artifact
	InternalDeps  freshness.Value[int]    // number of other catalogue repositories this one depends on
	Build         *Fact                   // nil: no Jenkins provider is configured, the "build" kind does not exist (404)
	Security      *Fact                   // nil: no Snyk provider is configured, the "security" kind does not exist (404)
}

// Fact is a ready-made message from an integration: the closed message strings and the tone are decided by the
// provider package, so no upstream text reaches a badge. Stale means the message is the last known one and the
// data is older than the provider's TTL allows.
type Fact struct {
	Message string
	Tone    Tone
	Stale   bool
}
```

`Fact` and `Tone` are the shapes the integrations unit asked for in `14-integrations-access.md` (section 4.6 table row `badges`: "`Build, Security *struct{Message string; Tone Tone; Stale bool}` with `Tone` a badges-owned enum"). The architecture's 8.7 `RepoFacts` is extended by exactly these two fields (deviation D-6).

### 2.4 What `bridge.BadgeFacts` must do (for unit 12 / `cmd/scp`)

The bridge is not part of this unit's packages, but the behaviour badges rely on is specified here so the bridge task can be written from it:

| `RepoFacts` field | Source | Mapping rule |
|---|---|---|
| `Repo` | `catalog.repo` | the entry's `scm.Repository` |
| `Stats` | `catalog.stats` | verbatim `freshness.Value` (open issues, release, licence come from it) |
| `LastCommit` | `commits.view` | `Latest[0].CommittedAt` with the view's `Info`; empty ring: zero time with the `Info` (state `no_content`); no view: zero `freshness.Value` (never fetched) |
| `Language`, `BuildTool`, `LatestVersion`, `InternalDeps` | `manifests` slot / graph (via `manifests` root import in the bridge) | verbatim values with their `Info` |
| `Build`, `Security` | `integrations.Service.FactFor(snap, slug, provider, now)` | `ok == false` (provider disabled) gives `nil`; otherwise `&badges.Fact{Message, Tone, Stale}` with `Tone` mapped by name (`success`, `info`, `warning`, `danger`, `neutral`); an unknown tone maps to `ToneNeutral` |

The bool result is `false` only when the slug has no entry in the snapshot; badges has already resolved the slug through `catalog.repo` before it calls `Facts`, so `false` is a race (repository removed between two reads) and yields the grey "unknown" badge.

### 2.5 What this unit exposes

* `commits.Service` with: `Jobs() []job.Spec`, `React(ctx, ev, enq) error`, `Restore(ctx, b, id, repo, item) error`, `Coverage(snap) (have, total int)`, `EnqueueUnrestored(snap, enq) (int, error)` (the post-warm-start pass of 4.7), `Recent(snap, slug, n) ([]scm.Commit, freshness.Info, bool)` (the read side `overview` asks for in `15-live-ops-overview.md` R-09-1; its consumer interface `CommitsSource` is satisfied **directly**), `ListCommits(ctx, commitsapi.ListCommitsRequestObject)` (the generated `StrictServerInterface`, section 7.1), `List(ctx, r, ListQuery) (ListResult, error)` (the operation behind it) and `Mount(httpapi.Mux)` (registers the generated strict handler like every other feature, 06 Appendix B); plus the publisher-side pair `commits.RegisterSlots(reg)` and `commits.NewProjection(cfg, slots)` with `Projector()` and `Validator()`, which exist **before** the `Service` because `readmodel.NewPublisher` takes projectors and validators at construction and `Service` needs the publisher.
* Owner types read by rank-5 `overview` and the bridges: `commits.View`, `commits.Latest`, `commits.CommitRef`, `commits.FeedInfo`, `commits.Compare`, the name constants `SlotName`, `GlobalName`, `ItemKind`, `JobKind`, and `commits.APICommit` (D-12; `overview` maps `scm.Commit` itself in its composite, so the wire form of a commit has two mappers that the contract conformance tests of both units keep identical).
* `badges.Service` with `Repo()` and `Group()` returning `http.Handler`, the route-name constants `RouteBadge` and `RouteBadgeGroup`, the kind vocabulary (`Kind`, `Kind.Alt()`, `Kind.String` via the string type) for the SPA contract tests, and the pure functions `Evaluate`, `EvaluateRepos`, `Render`.

### 2.6 Composition (done only in `cmd/scp`)

```go
// 1. registry: catalogue handles from the catalogue's registration, ours from commits.RegisterSlots
view, latest := commits.RegisterSlots(reg)                       // slot "commits.view", global "commits.latest"
slots := commits.Slots{Repos: cat.Repo, Stats: cat.Stats, Members: cat.Membership, Aliases: cat.Aliases, View: view, Latest: latest}

// 2. the publisher takes projectors and validators at construction, so the pure half of commits comes first
proj := commits.NewProjection(cfg.Commits, slots)
pub  := readmodel.NewPublisher(reg, []readmodel.Projector{proj.Projector(), ...}, []readmodel.Validator{proj.Validator(), ...}, rmDeps)

// 3. the service (needs the publisher)
commitsSvc, err := commits.New(commits.Deps{Cfg: cfg.Commits, Lister: ghClient, Store: sqliteStore, Pub: pub, Slots: slots,
                                            Authz: authzr, Meter: meter, Tracer: tracer, Log: log})
for _, sp := range commitsSvc.Jobs() { engine.Register(sp) }
fan := reactor.NewNamed(engine, reactor.Deps{...}, reactor.Reactor{Name: "commits", React: commitsSvc.React}, ...)
warm.Restore(commits.ItemKind, commitsSvc.Restore)               // warm start, after LoadItems returned
// ... planner.Restore, engine.Run ...
commitsSvc.EnqueueUnrestored(pub.Current(), engine)              // once, after the engine runs: rings that Restore skipped (4.7)
ready.Register("commits", commitsSvc.Coverage)                   // readiness probe, core kind
commitsSvc.Mount(apiMux)                                         // generated strict handler; httpapi.WithRequest is mounted innermost around apiMux (06 D29)
planKinds[commits.JobKind] = commits.ItemKind                    // the PlanSink bridge translates job kind to item kind (03 section 2.5, T-IN-8)
knownKinds = append(knownKinds, commits.ItemKind)                // store Deps.KnownKinds are ITEM kinds: "commits.ring", never "commits.refresh"
ov := overview.New(overview.Deps{..., Commits: commitsSvc})      // CommitsSource.Recent, satisfied directly

badgesSvc, err := badges.New(badges.Deps{Cfg: cfg.Badges, Facts: bridge.BadgeFacts{...}, Pub: pub, Repos: cat.Repo, Members: cat.Membership,
                                         Aliases: cat.Aliases, Authz: authzr, Enabled: manifestKinds(cfg), Meter: meter})
routes: RouteBadge -> badgesSvc.Repo(); RouteBadgeGroup -> badgesSvc.Group()
```

Names of the calls on `warm` and `ready` are placeholders for units 03 and 12 (`lifecycle`); everything else is the real signature (`engine.Register(job.Spec)` from `refresh`, `reactor.NewNamed` and `reactor.Func` from unit 01). `cmd/scp/assert.go` carries the compile-time assertions of this unit: `var _ reactor.Func = (*commits.Service)(nil).React`, `var _ commitsapi.StrictServerInterface = (*commits.Service)(nil)`, `var _ httpapi.Mounter = (*commits.Service)(nil)` (all three verified to compile against the prototype), and the structural ones for `commits.CommitLister` (`*github.Client`) and `commits.CommitStore` (`*sqlite.Store`); they cannot live in the feature's own tests because `reactor` is not in its import row (V-03). Only the order and the ownership are binding: slot and global names are registered with exactly `commits.SlotName` / `commits.GlobalName` through `RegisterSlots`.

---

## 3. Domain model and Go types

All types below are compile-checked. Zero values are meaningful unless stated.

### 3.1 Commits: names and constants

```go
const (
	SlotName   = "commits.view"
	GlobalName = "commits.latest"
	ItemKind   = "commits.ring"     // store item kind of the ring record (membership list)
	JobKind    = "commits.refresh"  // engine job kind
)
```

### 3.2 `View` (slot value, one per repository)

```go
type View struct {
	Latest []scm.Commit   // newest first by compareWithin, at most Config.Keep entries; immutable once Put
	Info   freshness.Info // Info.ContentSHA is the branch head SHA the ring was built from
}
func (v View) Newest() (scm.Commit, bool)
```

Invariants (enforced by `normalise`, checked by `TestNormalise*` and the validator):

1. `Latest` is sorted by `scm.CompareCommits` (newest first, equal timestamps by SHA **descending**: the one commit order of the kernel, 01 SC-08; no commit in a ring lacks a date); no duplicate SHA; `len <= Keep`.
2. Every `scm.Commit` has: `SHA` lower-case hex of length 40 or 64; `CommittedAt` UTC, whole seconds, in `[1970-01-02, now+24h]` (later values are clamped to ingestion time); `URL` an absolute `https` URL without credentials, at most 2048 bytes; `Subject` first line only, cleaned by the kernel's `scm.CleanLine` (invisible, bidirectional and tag-block characters dropped, Unicode spaces to one space) and capped at 200 runes with a trailing `…` when cut; `AuthorName` likewise at most 100 runes; `AuthorLogin` matches GitHub login syntax or is empty.
3. `Info` follows the kernel's state machine (`freshness.Info.AfterSuccess`, `AfterRevalidated`, `AfterFailure`): `State` is `ok` when the ring has entries, `no_content` for a repository without commits (empty repository or no default branch), `error` when the **last attempt failed** (the previous ring and `FetchedAt` are still held and served: stale-while-error, `HasValue()` stays true, `Derive` answers `stale`; `ErrCode` and `Attempts` say why and how often), `never` or no view before the first success. After a restart the store row of a failing repository still says `ok` (the store keeps the status of a row that has a value), so `Restore` maps `Attempts > 0` with a stored error code back to `State = error`.
4. `Info.ContentSHA` is "" only for `no_content`.
5. The slice is shared between successive `View` values when the ring did not change (revalidation): both are immutable, nothing may append to it.

Zero value `View{}` means "no view": `readmodel.Get` returns `ok=false` before the first publish for a repository; the handler and the projector treat it as `Never` (`!Info.HasValue()`).

### 3.3 `CommitRef`, `FeedInfo`, `Latest` (projection)

```go
type CommitRef struct {
	Slug scm.Slug
	SHA  string
	At   time.Time
}

// FeedInfo summarises the freshness of the repositories behind one feed; Derive turns it into a Status at read time.
type FeedInfo struct {
	Repos           int           // repositories in scope
	Never           int           // without any good value yet
	Failing         int           // last attempt failed (value may still be served)
	OldestFetchedAt time.Time     // oldest FetchedAt among repositories with a good value; zero when none
	ErrCode         string        // ErrCode of the failing repository with the smallest slug; "" when none fails
	TTL             time.Duration // policy TTL (identical for all repositories)
}
func (f FeedInfo) Derive(now time.Time) freshness.Status

// Latest is the commits.latest global (one per scm.ViewClass). All and ByGroup are the newest LatestN+1 refs: the extra
// one is a sentinel that lets a first page of exactly LatestN entries know that more exist.
type Latest struct {
	All       []CommitRef
	ByGroup   map[scm.GroupID][]CommitRef
	AllInfo   FeedInfo
	GroupInfo map[scm.GroupID]FeedInfo
}
```

`Derive` (a pure function of `FeedInfo` and `now`, so the status ages with the clock without a republish, and the ETag includes it, section 7.4):

| Condition (first match) | Status |
|---|---|
| `Repos == 0` | `fresh` (an empty scope has nothing to be stale about) |
| `Never == Repos` and `Failing == 0` | `unknown` |
| `Never == Repos` | `error` |
| `Never > 0` or `Failing > 0` or `now - OldestFetchedAt > 1.5 * TTL` | `stale` |
| otherwise | `fresh` |

`Latest` extends the architecture's `Latest{ByGroup, All}` additively with the two `*Info` fields (D-5). `commits.latest` is a `scm.Classed[Latest]`: `Public` computed from `VisPublic` repositories only, `All` from every repository; when no repository is non-public the two variants are the same value (one computation) - this is A-23 and the anonymous-enumeration test compares them.

### 3.4 Merge types (pure, exported for `overview` tests and fuzzing)

```go
func Compare(aSlug scm.Slug, a scm.Commit, bSlug scm.Slug, b scm.Commit) int // negative: a is listed before b

type Position struct {
	At   time.Time
	Slug scm.Slug
	SHA  string
}
type Source struct {
	Slug    scm.Slug
	Commits []scm.Commit // ordered by compareWithin, read-only
}
type Entry struct {
	Slug scm.Slug
	C    *scm.Commit // points into the immutable ring of its source
}
func Merge(srcs []Source, pos *Position, limit int, dst []Entry) (out []Entry, more bool)
func PositionOf(e Entry) Position
```

**Total order.** `(CommittedAt desc, Slug asc, SHA desc)`. The first draft and architecture 9.5 key the cursor on `(committedAt, sha)`. That order is not total across repositories because the same SHA may legitimately exist in two repositories (forks, mirrors; GR-07: every entity has its own identity, and a SHA alone is not one), so a cursor sitting on such a SHA would skip or repeat the twin. Adding the slug makes the order total and the cursor unambiguous (D-4). Within one repository `Slug` is constant so the per-ring order is exactly `scm.CompareCommits` (`CommittedAt` desc, `SHA` **desc**): the kernel states that "every list, projector and cursor in the portal uses this one order" (01 SC-08), and the first draft broke the tie the other way round (`SHA` asc), which would have made `commits` and every other consumer of `CompareCommits` disagree on commits of the same second (V-08, `TestRingOrderIsTheKernelOrder`).

### 3.5 Persistence records (JSON payloads; `record.go` -> `retention.go`)

```go
const ringSchema = 1

type ringRecord struct {            // payload of the store item (kind commits.ring)
	Schema int      `json:"schema"`
	Branch string   `json:"branch"`
	Head   string   `json:"head"`
	SHAs   []string `json:"shas"`    // ring order: newest first
}
type rowRecord struct {             // payload of one commit_log row; SHA and CommittedAt live in the row columns
	V       int    `json:"v"`
	Subject string `json:"s,omitempty"`
	Login   string `json:"l,omitempty"`
	Name    string `json:"n,omitempty"`
	URL     string `json:"u,omitempty"`
}
```

Why two records: `store.AppendCommits(repo, rows, keep)` is *insert-or-ignore plus trim in one transaction* (architecture 9.2, `03-readmodel-store.md`), i.e. `commit_log` holds the union of everything ever appended, trimmed to the newest `keep` by `committed_at`. After a force push or rebase the table may still contain commits that are no longer on the branch, and a future-dated commit that was later clamped has a different date in the new ring than in an old row. The *membership list* in the item payload is therefore the authority for what the ring is; `commit_log` is the content-addressed body store. This also keeps `Restore` simple: load `keep` rows, select exactly the SHAs named in the ring record (section 4.7).

### 3.6 Config types

```go
type Config struct {
	Keep           int           `yaml:"keep"`             // commits retained per repository (ring size), 1..100
	LatestN        int           `yaml:"latest_n"`         // length of the precomputed dashboard feeds, 1..200
	RefreshTTL     time.Duration `yaml:"refresh_ttl"`      // drift bound of commits.refresh, 1m..24h
	IdleBackoffMax time.Duration `yaml:"idle_backoff_max"` // cap of the idle back-off, RefreshTTL..168h
}
func DefaultConfig() Config // Keep 30, LatestN 50, RefreshTTL 2h, IdleBackoffMax 12h
func (c Config) Validate(path string) []error
```

Badges:

```go
type Config struct {
	Palette     string        `yaml:"palette"`       // "aa" (white text >= 4.5:1) or "aaa" (>= 7:1)
	CacheMaxAge time.Duration `yaml:"cache_max_age"` // max-age of badges that carry data; 0 means no-cache; at most 1h
}
func DefaultConfig() Config // palette aa, cache_max_age 5m
```

### 3.7 Badge types

```go
type Kind string
const (
	KindIssues        Kind = "issues"
	KindLastCommit    Kind = "last-commit"
	KindRelease       Kind = "release"
	KindLicence       Kind = "licence"
	KindRepos         Kind = "repos"          // group form only
	KindLanguage      Kind = "language"
	KindBuildTool     Kind = "build-tool"
	KindLatestVersion Kind = "latest-version"
	KindInternalDeps  Kind = "internal-deps"
	KindBuild         Kind = "build"          // optional: integrations (Jenkins)
	KindSecurity      Kind = "security"       // optional: integrations (Snyk)
)

type State uint8 // StateOK, StateNone, StateUnknown; String() "ok" / "none" / "unknown" (the metric label)

type Spec struct {
	Label, Message string
	Tone           Tone
	State          State
}

type Tone uint8 // ToneLabel, ToneSuccess, ToneInfo, ToneWarning, ToneDanger, ToneNeutral
type Palette uint8 // PaletteAA, PaletteAAA
type Color struct{ r, g, b uint8 } // opaque; no constructor from a string

type Badge struct {
	Label, Message string
	LabelColor     Color
	MessageColor   Color
}
func Render(b Badge) []byte
func Evaluate(k Kind, f RepoFacts, now time.Time) Spec     // pure
func EvaluateRepos(n int) Spec                              // pure; the group form
func ParseFile(file string) (Kind, bool)                    // "issues.svg" -> KindIssues; "license.svg" accepted
func (k Kind) Alt() string                                  // accessible text without a value, for the SPA <img alt>
```

`Color` has no constructor from a string and the palette is a closed `switch`: a colour value that reaches an SVG attribute can only be one of 12 constants. That is the type-level prevention of attribute injection (GR-33) for colours; text is handled by `encoding/xml` (section 4.12).

Kind availability: `issues`, `last-commit`, `release`, `licence` always exist; `language`, `build-tool`, `latest-version`, `internal-deps` exist when `manifests.enabled` (`Deps.Enabled`); `build` and `security` exist per repository when the bridge supplies a non-nil `Fact` (i.e. the provider is configured); `repos` exists only in the group route.

### 3.8 Deps and Service

```go
// commits
type Deps struct {
	Cfg    Config
	Lister CommitLister
	Store  CommitStore
	Pub    *readmodel.Publisher
	Slots                              // the same value that built the Projection
	Authz  authz.Authorizer
	Meter  metric.Meter
	Tracer trace.Tracer
	Log    *slog.Logger
}
type Slots struct {
	Repos   readmodel.Slot[freshness.Value[scm.Repository]] // catalog.repo (existence slot)
	Stats   readmodel.Slot[freshness.Value[scm.RepoStats]]  // catalog.stats
	Members readmodel.Global[scm.Membership]                // catalog.membership
	Aliases readmodel.Global[map[scm.Slug]scm.Slug]         // catalog.aliases
	View    readmodel.Slot[View]                            // SlotName
	Latest  readmodel.Global[scm.Classed[Latest]]           // GlobalName
}
func RegisterSlots(reg *readmodel.Registry) (readmodel.Slot[View], readmodel.Global[scm.Classed[Latest]])
type Projection struct{ Slots; /* cfg */ }                    // the pure, publisher-side half
func NewProjection(cfg Config, s Slots) *Projection
func (p *Projection) Projector() readmodel.Projector
func (p *Projection) Validator() readmodel.Validator
func New(d Deps) (*Service, error)                             // Service embeds *Projection

// badges
type Deps struct {
	Cfg     Config
	Facts   Facts
	Pub     *readmodel.Publisher
	Repos   readmodel.Slot[freshness.Value[scm.Repository]]
	Members readmodel.Global[scm.Membership]
	Aliases readmodel.Global[map[scm.Slug]scm.Slug] // catalog.aliases: renamed slug -> current slug
	Authz   authz.Authorizer
	Enabled []Kind // manifest-based kinds served when manifests.enabled; build and security exist when RepoFacts carries a Fact
	Meter   metric.Meter
}
func New(d Deps) (*Service, error)
```

`New` validates the config **and every dependency**: it returns `errors.Join` of every violation, a nil `Lister`, `Store`, `Pub`, `Authz`, `Meter`, `Tracer` or `Log` (commits) and a nil `Facts`, `Pub`, `Authz` or `Meter` (badges) being violations (the first draft let a nil logger or meter through, and the first failed store write then panicked, V-12; `TestNewRejectsMissingDependencies` in both packages). Neither service starts a goroutine. `badges.Service` holds no mutable state; `commits.Service` holds exactly one piece, the push marker of 4.1 (a `sync.Map` and an `atomic.Uint64`); everything else lives in the snapshot and the store, so both are safe for concurrent use by engine workers, webhook goroutines and handlers (section 8).

---

## 4. Behavioural specification

### 4.1 The `commits.refresh` job

Registration (`Jobs()`):

```go
job.Spec{
	Kind: "commits.refresh", Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
	Policy: freshness.Policy{TTL: Cfg.RefreshTTL, Jitter: 0.1, IdleBackoffMax: Cfg.IdleBackoffMax},
	Run:    s.run,
}
```

Job key: `commits.refresh/{source}/{owner}/{name}` with owner and name **folded to ASCII lower case**: built with `job.NewKey(JobKind, ref.KeyParts()...)` (01 SC-04), never from the display case, because the planner builds the same key from the catalogue's ref and a key in another case would not coalesce with it (and `store.KeyOf` folds, so the rows of one repository would be addressed by two spellings). The first draft built the key from `r.Owner, r.Name` (V-01; `TestReactKeyIsFoldedAndRunWritesTheFoldedStoreKey`). TTL 2 h and idle back-off cap 12 h are the architecture 9.6 values; `RefreshTTL` and `IdleBackoffMax` are config keys because 9.6 says every policy value is a config key with a consumer (D-3).

Pseudocode of `run(ctx, key) (Outcome, error)`; the code is `run` in the scratch module:

```
ref  <- scm.ParseRepoKey(parts(key))           ; malformed => permanentError
snap <- Pub.Current()
id, repo <- resolve(snap, ref)                  ; not found => goneError (metric result=gone)  # the engine drops the key
prior <- Get(snap, id, View)                    # may be absent
branch <- repo.DefaultBranch
pushed, gen <- push marker of ref.Key()         # recorded by React for a push to the default branch (4.5); false when none is pending

# 1. head-sweep skip: no GitHub request at all
if stats.HeadSHA == prior.ContentSHA and prior.State == ok and prior.ContentSHA != ""   # state error (last attempt failed) never skips
   and stats.Info.Derive(now) == fresh and not pushed:      # a pending push means the sweep value predates the change: fetch
       return revalidate(prior, result="skipped_head")        # Revalidated(item) + republish prior.Info.AfterRevalidated(now), Unchanged

if branch == "": return publishEmpty(...)                     # state no_content, Outcome.Unchanged

# 2. fetch
res <- Lister.LatestCommits(ctx, repo.Ref, branch, Keep, Validators{SHA: prior.ContentSHA})
err => fail(...)                                              # section 4.4 (the push marker stays: the retry fetches too)
fetched <- true                                               # on return without error: CompareAndDelete(marker of ref.Key(), gen)
res.NotModified => if !prior.HasValue(): fail(...)          # 304 without a ring: refetch without validators next time
                   else return revalidate(prior, "not_modified")

# 3. normalise
kept, head, stats <- normalise(res.Value, now, Keep, repo.HTMLURL, prior.Latest)
count stats (dropped by reason, clamped)
len(kept) == 0 => publishEmpty(...)
head <- cleanSHA(res.Validators.SHA) if valid                 # GitHub does not document list order (verified 2026-10-14),
        else head from first element, else kept[0].SHA        # so the adapter's observed tip is preferred

# 4. unchanged?
if head == prior.ContentSHA and sameSHAs(prior.Latest, kept) and prior.HasValue() and prior.State != no_content:
   return revalidate(prior, "unchanged")                       # also clears a previous failure (AfterRevalidated: state ok, attempts 0)
# revalidate: Store.Revalidated; on store.ErrNoItem (the store holds no good item: an earlier write failed, or the row was pruned)
#   it writes the ring being served instead (AppendCommits + PutItem), so a missed write heals at the next confirmation
account evictions(prior.Latest, kept)                         # aged_out vs rewritten

# 5. persist then publish
rows <- encodeRows(kept); ring <- encodeRing(branch, head, kept)
AppendCommits(repo, rows, Keep) ; PutItem(ring item {State ok, FetchedAt now, ContentSHA head})
  # rows first, membership second: a crash in between leaves a ring record naming SHAs the rows may lack;
  # Restore detects that ("incomplete") and the job is due at once.
  ErrNoRepo => goneError ; other store error => count persist_errors, log WARN, publish anyway (the snapshot is the serving state)
publish View{Latest: kept, Info: Info{}.AfterSuccess(ok, now, head) with TTL}   # Pub.Apply(Put)
return Outcome{} (changed)
```

Notes:

* **The push marker.** `React` stores, per repository key, an increasing generation number when a push to the default branch arrives, **before** it enqueues. A run that finds a marker does not use the head-sweep skip (the sweep value was taken before the push and still names the old head; without this rule the job swallowed every push webhook for up to the sweep interval, against architecture 9.6 "`push` (seconds)" and GR-21). The run deletes the marker with `CompareAndDelete(key, gen)` only after a successful fetch, so a push that lands while the fetch is in flight leaves a newer generation behind and the follow-up run (`marked_dirty`) fetches again. A failed run keeps the marker; a `goneError` deletes it. This is the one piece of mutable state of the `Service` (a `sync.Map` and an `atomic.Uint64`, bounded by the number of repositories). `TestPushBypassesTheHeadSweepSkip` pins all four cases and was mutation-checked (removing the bypass, or deleting unconditionally, fails it).
* **Why the head sweep skip is safe.** The catalogue's head sweep (`catalog.heads`, every 5 min, one batched GraphQL probe) records the head SHA of every default branch. If it equals the head our ring was built from, and the sweep value itself is fresh (`Derive(now) == fresh`), the ring cannot be stale; the job confirms by `Revalidated` and republishes the `View` with a new `FetchedAt` and **no GitHub request**. This is what makes a 2 h drift-bound TTL cheap at 1,000 repositories (budget arithmetic of architecture 9.6; counted by the fake-GitHub budget test, section 12).
* **Idle back-off.** `Outcome.Unchanged = true` is returned for `revalidate`, `publishEmpty`; the engine then lengthens the next interval up to `IdleBackoffMax`. A real change returns the zero `Outcome`, resetting the back-off.
* **A push webhook shortens the wait, never replaces the fetch** (webhooks are hints, section 17.2 of the architecture): `React` enqueues; the fetch decides.
* **Publishing is per repository** (`Apply` with one `Put`), so at cold start a 1,000-repository portal runs 1,000 `commits.refresh` publishes (other kinds add their own: the architecture counts about 3,000 per cold start in total). Measured cost of one real `Apply` including both projector variants, the existence check and the commits validator at 1,000 repositories is 0.81 ms and 1.24 MB (BenchmarkPublish, section 7.2), i.e. about 0.8 s of publish CPU for the commits share, spread over minutes by the engine's pacing; the cost is linear in the repository count (about 2.5 ms at 3,000).
* **`Info.TTL`** is stored in the view so the handler can compute `Derive` without reading config.

### 4.2 Normalisation (`normalise`)

Pure function: `normalise(in []scm.Commit, now time.Time, keep int, repoURL string, prior []scm.Commit) (out []scm.Commit, head string, st dropStats)` (`prior` is the previous ring or nil); `dropStats{NoDate, InvalidSHA, NoURL, Duplicate, Clamped int}`.

Per input element, in input order:

| Step | Rule | Counted as |
|---|---|---|
| SHA | trim, lower-case; length 40 or 64; hex only; else drop | `invalid_sha` |
| Duplicate | second occurrence of the same cleaned SHA is dropped (first wins) | `duplicate` |
| Date | `CommittedAt` converted to UTC; before 1970-01-02 (zero time, for example a missing `commit.author` in the upstream payload) => drop | `no_date` |
| Future | later than `now + 24 h` => clamp to `now`, keep; when the same SHA was in the previous ring with a date not after `now` (it was clamped at an earlier ingestion) that earlier date is kept, so the commit ages instead of tracking `now` at every refresh | `clamped` (and `scp_commits_clamped_total`) |
| URL | `cleanURL(c.URL)`: absolute, scheme `https`, host present, no userinfo, at most 2048 bytes; if empty derive `repoURL/commit/sha` from the repository's own `HTMLURL`; if still empty => drop | `no_url` |
| Text | `Subject` = `cleanLine(.., 200)`, `AuthorName` = `cleanLine(.., 100)`, `AuthorLogin` = `cleanLogin` | not counted |
| Truncation | `CommittedAt` truncated to whole seconds | |

After the loop: stable sort by `compareWithin` (= `scm.CompareCommits`; the branch order from GitHub is *not* trusted to be date order: rebases and merges produce non-monotonic dates), cut to `keep`. `head` is the cleaned SHA of the **first input element** (the branch tip as the adapter reports it) or "" when that element is invalid.

`cleanLine(s, limit)` = the kernel's cleaner, not a local one: keep the text before the first `\n`, pass it through `scm.CleanLine(line, limit+1)` (invalid UTF-8 to U+FFFD; every Unicode space to one space, runs collapsed, ends trimmed; other C0 and C1 controls, soft hyphen, U+061C, U+200B, U+200E/F, U+202A..202E, U+2060..2064, U+2066..206F, U+FEFF, the tag block U+E0000..E007F ("ASCII smuggling" into an LLM context, which matters because commit subjects reach the `llms.txt` surface) and the variation-selector supplement dropped; U+200C, U+200D and U+FE00..FE0F kept for scripts and emoji), and, when the result has more than `limit` runes, cut it to `limit-1` runes plus `…`. The first draft carried its own cleaner (SC-09 says the kernel's is "the one implementation shared by `commits` and `github`"): it missed the tag block, the soft hyphen and the invisible operators U+2061..2064 (V-07; `TestCleanLineUsesKernelCleaner`).

`cleanLogin` accepts a safe subset of GitHub login syntax: ASCII alphanumerics and hyphens (not leading), at most 39 characters before an optional `[bot]` suffix (it is a character filter, not a validity check: `a--b` passes); anything else becomes "" (the SPA then shows the display name or nothing, and never builds an avatar or profile URL from untrusted text).

Why "first line only, 200 runes": the API returns up to 200 and the SPA truncates visually with CSS (the dashboard panel shows about 60 characters); storing more is a bounded-size and spoofing risk, not a value.

### 4.3 Retention and eviction

* The ring holds at most `Keep` commits per repository (config 1..100, default 30; 100 is GitHub's `per_page` maximum, so one request always suffices; verified 2026-10-14 in the REST docs "List commits": `per_page` max 100, default 30).
* In memory the ring is the `View.Latest` slice, replaced wholesale at each change (never mutated).
* In the store, `AppendCommits(repo, rows, keep)` inserts the new rows (ignoring existing SHAs) and trims `commit_log` of that repository to the newest `keep` rows by `committed_at` in the same transaction. The item payload (`ringRecord`) is the authoritative membership.
* **Eviction accounting** (`evicted`): commits of the previous ring that are not in the new ring are classified `aged_out` (older than the oldest new commit: the normal case) or `rewritten` (inside the new time range: force push, rebase, changed default branch). Metric `scp_commits_evicted_total{reason}`. A sustained `rewritten` rate is the operator's early signal of a repository with a rewriting workflow (not an error).
* Repository removed from the catalogue: the repository's rows and items are deleted by the store's cascade (`store.ErrNoRepo` on a later write), the snapshot entry disappears with the catalogue's `Apply`, and the next job run returns `goneError`. Nothing in `commits` deletes data itself.
* Clamped commits: a commit dated in the future is stored with the clamped date (ingestion time). It then ages like any other commit (the clamped date is carried over from the previous ring, `TestNormaliseClampIsStableAcrossRefreshes`); without the clamp a commit dated 2099 would sit at the top of every feed forever, and without the carry-over it would track `now` and stay above every real commit while it remained in the fetch window.

### 4.4 Failure handling (typed errors)

`fail(ctx, id, itemKey, prior, now, err)` classifies and records:

| Error | Classification | Effect |
|---|---|---|
| `job.AsRateLimited(err)` ok, or `ctx.Err() != nil`, or `job.IsGone(err)` | not a failure of the data | returned untouched; metric result `deferred`; no store write |
| `errors.Is(err, scm.ErrNotFound)` for the repository | `Gone` (adapter wraps) | engine drops the key; the catalogue's next reconcile removes the repository |
| other | data failure | `Store.RecordFailure(item, Failure{Code, At})`; republish the **previous ring unchanged** with `Info = prior.Info.AfterFailure(now, code)` (State `error`, `FetchedAt` and `ContentSHA` kept, `Attempts + 1`, `ErrCode = code`; `Derive` answers `stale`, or `error` when no ring was ever held) and the policy `TTL`; metric result `error`; return err to the engine (back-off with jitter) |

`errCode` is a closed set (at most 64 bytes, matches the store's check): `permanent`, `not_found`, `forbidden`, `unavailable`, `error`. It is what API and logs show (`freshness.lastErrorCode`); never an error string (no internal text on the wire).

Typed errors the package defines, both implementing the behaviour interfaces of `job`:

```go
type goneError struct{ err error }       // Gone() bool => true     (unknown repository, store.ErrNoRepo, readmodel.ErrUnknownRepo)
type permanentError struct{ err error }  // Permanent() bool => true (malformed job key, un-encodable record, readmodel.ErrInvalidSnapshot)
```

Stale-while-error: the ring and `FetchedAt` stay as they were, so `freshness.status` becomes `stale` once `now - FetchedAt > 1.5 TTL` and the SPA shows the age; the data remains served (G-05).

### 4.5 `React` (webhook and head-sweep accelerator)

```go
func (s *Service) React(_ context.Context, ev scm.Event, enq job.Enqueuer) error
```

| Event | Condition | Action |
|---|---|---|
| `EventPush` | repo known; `ev.Ref == "refs/heads/" + DefaultBranch`; not `Deleted` | record the push generation (4.1, "The push marker"), then `Enqueue(job.NewKey(JobKind, ref.KeyParts()...), PrioWebhook, time.Time{}, false)`; result `enqueued` |
| `EventPush` | other ref (feature branch, tag) | none; result `not_default_branch` (Q-18; the full ref is compared, never its last path segment, GR-10) |
| `EventHeadChanged` | repo known | `Enqueue(.., PrioScheduled, ..)` (no marker: the sweep value is by definition newer than the event) |
| any | unknown repository | none; result `unknown_repo` |
| other kinds | | none |

It is a cheap decision (one scan of the snapshot, one `sync.Map` store) plus a non-blocking `Enqueue`, retains nothing of `ev`, runs on the webhook goroutine, and never returns an error for "nothing to do". The repository is resolved by a scan of `catalog.repo` (the numeric ID first when both refs carry one, else the folded key), so a webhook for a repository renamed on GitHub still finds it before the reconcile has caught up; the job key and the marker are built from the **catalogue's** ref (`KeyParts`, `Key()`), never from the event's names. The default branch is read from the snapshot (`catalog.repo`), never from the event, so a webhook cannot make us fetch a branch of its choosing (GR-10).

### 4.6 Projector and validator

`Projector()` runs inside every `Apply`, pure and deterministic (a function of the candidate snapshot only):

```
for each repo in snap: entry{slug, visibility, ring source, info}
mem   <- catalog.membership
pub   <- buildLatest(entries, mem, ViewPublic)
all   <- pub if no non-public repository exists else buildLatest(entries, mem, ViewAll)
PutGlobal(commits.latest, Classed{Public: pub, All: all})

buildLatest(class):
  srcs <- rings of repositories visible in class
  All      = refs(Merge(srcs, nil, LatestN+1))    ; AllInfo = feedInfo(those repos)       # +1: the sentinel of the fast path
  for g in groups: ByGroup[g] = refs(Merge(rings of Membership.VisibleReposOf(g, class), nil, LatestN+1)); GroupInfo[g] = feedInfo(..)
```

Cost of one whole `Apply` (one `Put` of one repository, then this projector, the existence validator and `Validator()`) at 1,000 repositories x 30 commits and 51 groups of 40 members (BenchmarkPublish, Apple M4 Max, 2026-10-14): 0.81 ms (median of 5 runs; 0.80..0.85), 1.24 MB, 2,245 allocs.

`Validator()` rejects a candidate snapshot in which any ref of any feed names a repository with no view or a SHA that is not in that repository's ring. (Together with the catalogue validators "every group member exists" it guarantees the handler's fast path never dereferences a missing commit.)

### 4.7 Warm start (`Restore`)

`cmd/scp` calls `Restore(ctx, b, id, repo, item)` once per stored item of kind `commits.ring`, **after** `LoadItems` returned (a `LoadItems` callback must not call the store), with the builder of the version-1 `Apply` in which the catalogue entry already exists.

```
if item.State has no result or item.FetchedAt is zero => return, not counted   # the error row RecordFailure creates for a repository that never had a ring (V-05)
ring <- decodeRing(item.Payload)               ; bad schema/JSON => skip, result invalid
len(ring.SHAs) == 0 => Put(View{Info: restoredInfo})         # no_content
rows <- LoadCommits(repo, Keep)                ; store read error => return error (abort warm start step)
for sha in ring.SHAs: row <- rows[sha]         ; missing => skip, result incomplete  (job is due at once)
                      decodeRow                ; bad => skip, result invalid
kept <- normalise(commits, now, Keep, "", nil) ; any drop or count change => skip, result invalid
Put(View{Latest: kept, Info: restoredInfo(item, ring)})       # FetchedAt/CheckedAt/ContentSHA/ErrCode from the item
```

A repository that is skipped has no view: it appears as `never` in `FeedInfo` and `Coverage` does not count it. **Nothing makes its job due by itself**: the planner restores the persisted `NextDue` of the item (04: it knows `NextDue` and the streak, never `fetched_at`; 03 assumes the opposite, "the planner sees no `fetched_at`"), which can be up to the idle back-off cap (12 h) away, and the first draft's claim that the job "is due immediately at `PrioBackfill`" held only for a repository without any item row. `Service.EnqueueUnrestored(snap, enq) (int, error)` closes the gap: `cmd/scp` calls it once after the engine runs and the warm start is done; it enqueues `commits.refresh` at `PrioBackfill` for every repository of the snapshot that has no `commits.view` (keys the planner already enqueues coalesce, so a cold start is unaffected; a refusal stops the walk and is returned with the count; `TestEnqueueUnrestoredCoversSkippedRings`, V-13). `Restore` itself cannot enqueue: it runs inside the version-1 `Apply` and has no `Enqueuer`. The orphan rows left by a force push are ignored because only the SHAs of the membership list are loaded (test `TestRestoreIgnoresOrphansAfterForcePush`).

`restoredInfo.ErrCode` is the item's stored `Error` code (closed set from `errCode`); a row with a value, `Attempts > 0` and an error code is restored in `State = error` (the store keeps `ok` for such a row, stale-while-error), so a repository that was failing when the process stopped is `stale`, not `fresh`, after the restart (`TestRestoreOfAFailingRepositoryIsStale`).

### 4.8 `Coverage` (readiness)

`Coverage(snap) (have, total int)`: repositories that hold a value (`Info.HasValue()`: states `ok`, `no_content`, and `error` with a held ring) over all repositories. It feeds the `commits` core kind of the readiness definition (`readiness.core_kinds: [commits]`, `min_coverage: 0.9`, architecture 10.2).

### 4.9 Client-side day grouping (specification for the SPA unit)

The server returns UTC instants. Grouping rows by calendar day is a presentation decision made in the browser, in the viewer's time zone (or `site.display_time_zone` from `api.site` when the viewer has no preference). Server-side day grouping has three classic pitfalls; each one is a rule here and a test (GR-38):

| Pitfall | Rule here |
|---|---|
| grouping in the server's own zone, unlabelled | zone is an explicit IANA name; the day heading shows the zone (`Europe/Oslo`) once in the page header, `<time datetime="...Z">` on each row |
| a crash on a commit without a date | the server never emits a commit without a date (dropped, `no_date`) |
| stateful single-use iterator | grouping is a pure fold `groupByDay(items, zone)`; no state across calls |

Algorithm (pure function, in `web/src/features/commits/groupByDay.ts`):

```ts
const fmts = new Map<string, Intl.DateTimeFormat>();   // one formatter per zone: constructing one costs about 33 us, formatting about 1 us (Node 26: 66 ms vs 1.6 ms for 2,000 rows)
const fmt = (zone: string) => {
  let f = fmts.get(zone);
  if (!f) { f = new Intl.DateTimeFormat("en-CA", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit" }); fmts.set(zone, f); }
  return f;
};
export function dayKey(iso: string, zone: string): string { return fmt(zone).format(new Date(iso)); }  // "2026-06-16"
export function groupByDay<T extends { committedAt: string }>(items: T[], zone: string): { day: string; items: T[] }[] {
  const out: { day: string; items: T[] }[] = [];
  for (const it of items) {
    const day = dayKey(it.committedAt, zone);
    const last = out[out.length - 1];
    if (last && last.day === day) last.items.push(it); else out.push({ day, items: [it] });
  }
  return out;
}
```

Because the API order is strictly time-descending, equal days are always adjacent, so a single pass suffices; when a further page is appended (infinite scroll), the first group of the new page is merged into the last group of the accumulated list when the keys are equal (same function applied to the concatenation). The labels "Today" and "Yesterday" are computed by comparing the day keys with `dayKey(new Date().toISOString(), zone)` and the key of the previous calendar day computed by `Temporal`-free arithmetic on the three numbers (not by subtracting 24 h; DST days are 23 or 25 hours long).

Golden vectors (verified 2026-10-14 with Python `zoneinfo` and Node `Intl.DateTimeFormat`; re-verified by the independent verifier with Node v26.10.0 and Python 3 `zoneinfo`, all nine rows agree):

| Instant (UTC) | Zone | Day key | Why it is in the table |
|---|---|---|---|
| 2026-03-29T23:30:00Z | Europe/Oslo | 2026-03-30 | after the spring DST change (01:00 UTC, +02:00) |
| 2026-10-25T00:30:00Z | Europe/Oslo | 2026-10-25 | 02:30 +02:00, before the autumn change |
| 2026-10-25T01:30:00Z | Europe/Oslo | 2026-10-25 | 02:30 +01:00: the repeated hour has the same day |
| 2026-10-25T23:30:00Z | Europe/Oslo | 2026-10-26 | 00:30 +01:00 |
| 2026-01-01T00:00:00Z | America/Los_Angeles | 2025-12-31 | previous year |
| 2026-06-15T10:00:00Z | Pacific/Kiritimati | 2026-06-16 | UTC+14 |
| 2026-06-15T18:30:00Z | Asia/Kolkata | 2026-06-16 | half-hour offset |
| 2026-03-08T09:59:59Z / 10:00:00Z | America/Los_Angeles | 2026-03-08 / 2026-03-08 | the 01:59:59 to 03:00:00 jump stays one day |

These vectors are a contract test of the SPA (`groupByDay.test.ts`); they are listed here because the server contract (UTC, whole seconds, monotone order, stable tie-break) is what makes the single pass correct.

### 4.10 The commit log endpoint

Authoritative contract: `20-design/api/openapi-public.yaml`, operation `listCommits` (`GET /api/v1/commits`, route `api.commits`, `x-scp-etag: snapshot-variant`). Behaviour implemented by this unit:

| Aspect | Rule |
|---|---|
| Parameters | `repo` (slug), `group` (group id), `limit` (1..200, default 50), `cursor` (opaque), `If-None-Match` |
| `limit` | present and not in 1..200 => `400 validation-failed` with pointer `#/query/limit` (`httpapi.ParsePage` treats `0` as absent, so an explicit `limit=0` is rejected by the handler before it); absent => 50 |
| Cursor | opaque `httpapi.EncodeCursor` token (`base64url(0x01 || JSON)`, at most 512 bytes) of `{"t": <unix seconds>, "r": <slug>, "s": <sha>, "f": <filter fingerprint>}`; `f` is `httpapi.QueryFingerprint(repo, group)` so a cursor is **bound to the filter it was issued for** (06-httpapi T12; a position cursor would remain meaningful under another filter, but a client that mixes them has a bug worth a 400, and forged cursors gain nothing). Decode failure (strict: unknown field, trailing data), slug empty or longer than 100, `t` negative or > 2^40, SHA not 40/64 hex, or a fingerprint mismatch => `400 invalid-cursor`. Visibility is re-applied on every page, the cursor carries no authority |
| Not ready | snapshot version 0 => `503 not-ready` with `Retry-After: 5` (`httpapi.NotReady`) |
| Authorisation class | `authz.ClassOf(Authz, principal)`: `ViewPublic` or `ViewAll` |
| `repo` | the value is ASCII-lower-cased first (GitHub names are case-insensitive and links often carry the display case, V-10), then an alias of a renamed repository is resolved **silently** to the current slug; then `authz.LookupRepo` (invisible and non-existent are the same `404 not-found`) |
| `group` | unknown group => `404 not-found`; `Membership.Groups()` lists every group to every class (group existence is configuration, the dashboard lists them), so an existing group whose members are all private is an empty page for an anonymous caller, never a 404; members are taken with `Membership.VisibleReposOf(group, class)` |
| `repo` and `group` | intersection: the repository must be a member of the group, otherwise the page is empty (200, `items: []`) |
| Order | `(committedAt desc, repo asc, sha desc)` |
| Page | `Merge(srcs, pos, limit)`: `more` true iff entries remain after the last returned one => `page.nextCursor` set to the cursor of the last returned entry, and `Link: <...>; rel="next"` |
| Freshness block | `status` from `FeedInfo.Derive(now)`; `kind: "commits.view"`; `asOf` = snapshot build time; `fetchedAt` = oldest `FetchedAt` among repositories in scope with a good value; `staleSince` = `fetchedAt + 1.5 TTL` when not fresh; `lastErrorCode` from the failing repository with the smallest slug |
| Items | `{sha, repo (slug), subject, authorName|null, authorLogin|null, committedAt (UTC), url (https)}`; no e-mail; no timezone-derived field |

**Two paths, one result.**

* *Fast path* - first page (no cursor) of the unfiltered log or of a group (no `repo` filter) with `limit <= latest_n`: served from the precomputed feed (`commits.latest`). The feeds hold `LatestN + 1` refs; the extra one is a **sentinel** so that a page of exactly `latest_n` entries still knows whether more exist (`more` is true iff the feed is longer than the page; with fewer than `LatestN + 1` refs the feed is complete). It resolves the refs to commits through the slots of the same snapshot by SHA (O(limit x ring)). This is the dashboard's "latest commits" panel and the SPA's default first page (default `limit` 50 equals default `latest_n` 50, which is why the sentinel exists: without it the default page would always take the merge path). `TestFastPathBoundaryAtLatestN` pins fast == merge for totals just below, at and above `LatestN`.
* *Merge path* - everything else: collect the rings of the repositories in scope from the snapshot (O(k)), position each by binary search past the cursor, heap-merge `limit` entries. Cost O(k log n + limit log k); nothing proportional to the 30 x k retained commits (GR-28). `TestHandlerFastPathEqualsMergePathAndPaging` pins that both paths yield byte-identical bodies.

A **cursor stays valid across publishes**: it is a key position, not an offset, so a commit arriving at the top between two page requests neither shifts nor repeats entries (`TestMergeCursorSurvivesInsertionAtTop`); a commit that was removed leaves a position that still totally orders against the remaining elements.

### 4.11 Badge kinds, states and texts

Every badge is `Label` (closed set per kind) + `Message` + tone colour. States: `ok` (a value is shown), `none` (the fact is known to be absent: grey "none"), `unknown` (no usable data: grey "unknown"). The `fact` helper classifies a `freshness.Value[T]`: a value exists exactly when `FetchedAt` is set (stale-while-error keeps the last good value and `FetchedAt`); its `State` tells value (`ok`, `error`) from recorded absence (`no_content`, `not_found`, `unsupported`); anything else (never fetched, `too_large`, `never`) is `noData`.

| Kind | Label | Message | Tone / rule | `none` when | `unknown` when |
|---|---|---|---|---|---|
| `issues` | `issues` | `{n} open` (`formatCount`) | `n == 0` success, else info | | no stats |
| `last-commit` | `last commit` | `today`, `yesterday`, `N days ago` (2..13), `N weeks ago` (2..8 weeks), `N months ago` (2..23), `N years ago` in UTC calendar days | success up to 90 days, info 91..365, warning beyond 365 | repository with no commits (state `no_content`) | no view |
| `release` | `release` | tag name | info; pre-release warning | no release | no stats |
| `licence` | `licence` | SPDX id | info; `NOASSERTION`/`OTHER` => message `other`, neutral | no licence | no stats |
| `repos` (group only) | `repos` | count of repositories visible to the caller (`formatCount`) | info; `0` neutral | | |
| `language` | `language` | primary language | info | nothing recognised | never fetched |
| `build-tool` | `build tool` | tool | info | none recognised | never fetched |
| `latest-version` | `version` | declared version of the primary artifact | info | none declared | never fetched |
| `internal-deps` | `internal deps` | `1 repo`, `N repos` | info | 0 | never fetched |
| `build` | `build` | `Fact.Message` (+ ` (stale)` when `Stale`) | `Fact.Tone` | | message is empty or `unknown` |
| `security` | `security` | `Fact.Message` (+ ` (stale)`) | `Fact.Tone` | | message is empty or `unknown` |

`formatCount`: 0..999 plain; 1,000..999,999 as `1k`, `1.2k`, `12k`, `999k` (truncate to a tenth, never round across a unit boundary; `999.9k` stays `999k`); at least 10^6 as `1M`, `1.2M`; negative => `0`. `relativeDay` uses UTC calendar-day difference (not 24 h multiples) so that the text changes exactly at 00:00 UTC (hence the content-hash ETag, section 7.6); a future instant (clock skew) is `today`.

A stale integration fact says so in words: colour is never the only carrier of a state (WCAG 1.4.1).

Absent and unknown data are rendered states, never placeholder image files (GR-29): data that does not exist yet is the grey `issues: unknown`, never a misleading `0 open`, while a real zero is `issues: 0 open` (green); a licence is rendered as `licence: {id}` from the SPDX id (never spliced into a template, GR-33), no licence is `licence: none`, and `NOASSERTION`/`OTHER` become `licence: other`; `build`, `security`, `last-commit` and `release` without data are the grey `unknown` (or `none` when the absence is known) of the respective kind. No image file is read at request time.

### 4.12 SVG generation specification

**Layout** (the Shields "flat-square" geometry without gradient, shadow, blur, ids or `<defs>`; height 20; text 11 px; text y 140 in a `scale(.1)` group at font-size 110):

```
labelText = cleanText(label, 40) ; msgText = cleanText(message, 40)       # runes, after sanitation
lt = textWidth(labelText), mt = textWidth(msgText)                         # integer pixels, odd (below)
lw = lt + 2*5, mw = mt + 2*5                                               # 5 px padding each side
width = lw + mw
rect1: x=0     width=lw  fill=label colour      rect2: x=lw  width=mw  fill=tone colour
text1: x = lw*5 + 10        y=140 textLength = lt*10 transform="scale(.1)"   # centre of the label half, in tenths of a pixel
text2: x = lw*10 + mw*5 - 10 y=140 textLength = mt*10 transform="scale(.1)"
```

**Text widths.** Browsers render the SVG with their own font, so the server cannot know the exact width; the strategy (as in Shields) is a measured width table plus `textLength`, which forces the drawn length: a measurement error shows as slightly tighter or looser letter spacing, never as overflow or a clipped half.

* Font: DejaVu Sans **2.37** (name table `Version 2.37`; the first draft called it "2.37.3", a version that does not exist). Source: `ttf/DejaVuSans.ttf` inside `dejavu-fonts-ttf-2.37.tar.bz2` of the release `version_2_37` at `https://github.com/dejavu-fonts/dejavu-fonts/releases/download/version_2_37/dejavu-fonts-ttf-2.37.tar.bz2`; archive sha256 `fa9ca4d13871dd122f61258a80d01751d603b4d3ee14095d65453b4e846e17d7`, file sha256 `7da195a74c55bef988d0d48f9508bd5d849425c1770dba5d7bfc6ce9ed848954` (both computed by the verifier from a fresh download on 2026-10-14; the generator checks the file hash before it writes anything). The CSS font stack of the SVG is `DejaVu Sans,Verdana,Geneva,sans-serif`. It is **not** Shields' stack (Shields serves `Verdana,Geneva,DejaVu Sans,sans-serif`, live response of 2026-10-14, and measures with Verdana): DejaVu is first here because the table is DejaVu's, so a viewer that has DejaVu Sans renders exactly the measured width and any other font is forced to it by `textLength`. Against Shields' own flat-square output (live, 24 half widths of 12 label/message pairs, 2026-10-14) 17 are identical and the rest differ by 2 to 4 px (`licence` 49 vs 47, `Java` 33 vs 35, `1.0.0-SNAPSHOT` 101 vs 105), which is the Verdana-versus-DejaVu difference and harmless because the drawn length is forced.
* Table: advance widths in **font units** (2048 per em), big-endian `uint16`, for U+0020..U+007E and U+00A0..U+017F (printable ASCII, Latin-1 supplement, Latin Extended-A: Western and Central European text including the Norwegian letters). Code points outside the table: combining marks and format characters (categories Mn, Me, Cf) 0; East Asian wide and fullwidth ranges 2048 (one em); everything else 1271 (mean advance of `[A-Za-z0-9]`).
* Width: `sum(advances) * 11 / 2048` with integer truncation, then rounded up to the next odd integer. Using font units and truncating once at the end matters: an earlier version rounded each glyph to 1/64 px and pushed 33.99 px to 34, giving 35 instead of 33 for `issues`; the fixed version agrees with an **independent** Python TTF parser (`ttfwidth.py`: `fontTools`-free, reads `hmtx`/`cmap` directly) for every golden string, and with the Shields sample (`issues: 12 open`: left 43, right 55, total 98; text x 225 / 695, textLength 330 / 450; the verifier fetched `https://img.shields.io/badge/issues-12%20open-blue?style=flat-square` and `last%20commit-3%20days%20ago` live on 2026-10-14: the first is identical in every number, the second has the same 146 x 20 as `aa-last-commit.svg`).
* The generator is the package `tools/badgewidths` of the single nested module `tools/go.mod` (it needs `golang.org/x/image` v0.47.0, which must never enter the product module graph; architecture 4.5 and 5.2 allow exactly one nested `tools` module, so the first draft's second `go.mod` under `tools/badgewidths` is removed, V-14); `cd tools && go run ./badgewidths -font badgewidths/font/DejaVuSans.ttf -out ../internal/badges/widths_gen.go` reproduces the committed file byte for byte (re-run by the verifier); CI regenerates it and fails on a diff. The font file is committed with its licence (Bitstream Vera licence with public-domain changes; redistribution permitted with the licence text; the table itself is metric data, not the font, and the font is not embedded in the binary).

**Escaping and well-formedness.** The document is produced by `xml.Marshal` of typed structs (`svg`, `shapes`, `labels`, `rect`, `text`); no string is ever concatenated into markup. Consequences, all tested: `<`, `>`, `&`, `"`, `'` in text and attribute values are escaped structurally; characters that XML 1.0 cannot carry (C0 controls except tab/CR/LF, U+FFFE/U+FFFF, lone surrogates from invalid UTF-8) become U+FFFD; colours can only be palette constants; numbers are integers. `FuzzRenderWellFormed` (and every golden) asserts that for arbitrary label and message the output is a **closed document**: strict XML; exactly one root `svg`; only the elements `svg`, `title`, `g`, `rect`, `text`; only the attributes `xmlns`, `width`, `height`, `role`, `aria-label`, `shape-rendering`, `x`, `y`, `fill`, `text-anchor`, `font-family`, `text-rendering`, `font-size`, `textLength`, `transform` (so no `script`, `foreignObject`, `style`, `href`, `on*` handler can occur); no processing instruction or directive (so no entity declaration); `width` of the root equals the sum of the two rectangle widths; and the text contains no `<script` markup. 2,527,978 executions in 20 s on the final code found nothing. Hostile golden (`aa-hostile.svg`): the message `<script>alert(1)</script>&"' end` renders as `&lt;script&gt;...` in both `aria-label`, `<title>` and the text node.

**Accessibility.**

* The root has `role="img"` and `aria-label="{label}: {message}"` and a `<title>` with the same text (screen readers announce one name; the visible glyphs are `aria-hidden` by being inside the labelled image).
* Contrast: white text (`#fff`) on every half colour. The closed palette meets WCAG 2.2 SC 1.4.3 (AA, normal text 4.5:1) with the default `aa` palette and SC 1.4.6 (AAA, 7:1) with `aaa` (thresholds re-verified in the WCAG 2.2 recommendation on 2026-10-14). Ratios computed with the standard relative-luminance formula (`contrast.py`, and `TestPaletteContrast` using the sRGB threshold 0.04045):

| Tone | `aa` colour | ratio | `aaa` colour | ratio |
|---|---|---|---|---|
| label (left half) | `#555555` | 7.46 | `#444444` | 9.74 |
| success | `#1a7f37` | 5.08 | `#116329` | 7.39 |
| info | `#0b61a4` | 6.44 | `#0a4f86` | 8.48 |
| warning | `#b45309` | 5.02 | `#8a4600` | 7.10 |
| danger | `#c0392b` | 5.44 | `#a40e26` | 7.87 |
| neutral (unknown, none) | `#6e6e6e` | 5.10 | `#595959` | 7.00 |

* State is never conveyed by colour alone: `none`, `unknown` and `(stale)` are words in the message.
* The SPA supplies `alt` for each `<img>` from `Kind.Alt()` / the API's `Badge.label` ("Open issues", "Last commit", "Latest release", "Licence", "Number of repositories", "Primary language", "Build tool", "Latest version", "Internal dependencies", "Build status", "Security scan"), so every image has a text alternative (GR-39). The SVG's own `role`/`aria-label` serve the case where the SVG is opened as a document or embedded with `<object>`.

**Examples** (goldens, `aa` palette; byte-exact):

`aa-issues.svg` (`issues: 12 open`):

```xml
<svg xmlns="http://www.w3.org/2000/svg" width="98" height="20" role="img" aria-label="issues: 12 open"><title>issues: 12 open</title><g shape-rendering="crispEdges"><rect width="43" height="20" fill="#555555"></rect><rect x="43" width="55" height="20" fill="#0b61a4"></rect></g><g fill="#fff" text-anchor="middle" font-family="DejaVu Sans,Verdana,Geneva,sans-serif" text-rendering="geometricPrecision" font-size="110"><text x="225" y="140" textLength="330" transform="scale(.1)">issues</text><text x="695" y="140" textLength="450" transform="scale(.1)">12 open</text></g></svg>
```

`aa-unknown.svg` (`issues: unknown`, 102 x 20, message colour `#6e6e6e`); `aa-last-commit.svg` (`last commit: 3 days ago`, 146 x 20, `#1a7f37`); `aa-long.svg` (message cut at 40 runes with an ellipsis: `release: v1.2.3-rc.v1.2.3-rc.v1.2.3-rc.v1.2.3-rc…`, 274 x 20); `aa-hostile.svg` (above, 236 x 20). The complete set (`aa-*`: issues, last-commit, release, licence, repos, language, build, security, unknown, long, hostile, nonascii; `aaa-*`: issues, last-commit, release, licence, language, build, security) is in `testdata/golden/badges/`.

**Rendering cost**: `BenchmarkRender` 4.25 us/op (median of 5), 9.1 KB, 54 allocs (Apple M4 Max, 2026-10-14) - a badge response is about 100 us of server time including the lookup; no render cache is needed (the HTTP cache and the content-hash ETag do the work).

### 4.13 Badge request handling

```
GET /badges/{repo}/{file}           route "badge"          kind = ParseFile(file)     ("issues.svg", "license.svg" accepted)
GET /badges/groups/{groupId}/{file} route "badge.group"     only "repos.svg"
```

```
serveRepo(kind, slug):
  1. kind unknown, or manifest kind not enabled => 404 grey "invalid / unknown kind"           (no ETag; max-age 60)
  2. slug empty or > 200 bytes => 404
  3. snapshot not ready (version 0) => 503 grey "starting", Retry-After 15, Cache-Control no-store
  4. slug <- asciiLower(slug); slug <- aliases[slug] when present   # README embeds often carry the display case (Billing-Service); a renamed repository keeps serving its embedded badges (no redirect)
  5. repo <- authz.LookupRepo(ctx, snap, Repos, Authz, slug)    # the ONLY existence check
     problem => 404 with the SAME body, headers and Cache-Control as case 1/2 (hidden == missing)
  6. spec <- unknown(kind)
     if Facts.Facts(snap, slug) ok:
         (kind == build and f.Build == nil) or (kind == security and f.Security == nil) => 404 (provider not configured)
         spec <- Evaluate(kind, f, now)
  7. respond(200, spec, private = repo.Visibility != VisPublic)
```

* **404 vs 200-grey.** A 200 placeholder for everything would hide broken embeds and blur existence with absence of data (GR-16). Here: a repository or kind that does not exist (or is not visible) is `404` with a small grey SVG body so an `<img>` still shows something and broken embeds are visible in monitoring; a repository that exists but has no data yet is `200` grey `unknown`/`none` with a short max-age (<= 60 s) so the real badge appears quickly once data arrives.
* Existence is decided only by `LookupRepo` against the caller's class, so an anonymous caller gets byte-identical responses for `/badges/secret/issues.svg` and `/badges/does-not-exist/issues.svg` (`TestHandlerUnknownAndHidden` compares body and `Cache-Control`).
* Group badge: `repos` for a group: `len(Membership.VisibleReposOf(group, class))` (a class-dependent count, A-23, no per-repository lookups); `private` when the class is `ViewAll`. Unknown group (`Membership.Group` false) => 404.

### 4.14 Time, clocks and determinism

* `time.Now()` is read at the edges only (job `run`, handler); `normalise`, `Evaluate`, `FeedInfo.Derive`, `relativeDay` take `now` as an argument. Tests use `testing/synctest` where wall-clock passage matters (no `Clock` interface: architecture 8.12).
* Every stored and emitted instant is UTC, whole seconds.

### 4.15 Failure-mode table

| Failure | Detected by | Behaviour | Visible as |
|---|---|---|---|
| GitHub 5xx / network | `LatestCommits` error | ring kept, `ErrCode` set, back-off | `freshness.status` stale after 1.5 TTL; `scp_commits_refresh_total{result="error"}` |
| Rate limited | `job.AsRateLimited` | deferred, no state change | `result="deferred"` |
| Repository deleted upstream | `ErrNotFound` => `Gone` | key dropped, catalogue reconcile removes it | `result="gone"` |
| Malformed commit data | `normalise` | commit dropped | `scp_commits_dropped_total{reason}` |
| Future-dated commit | `normalise` | clamped | `scp_commits_clamped_total` |
| Store write fails | `AppendCommits`/`PutItem` error | warn, publish anyway (snapshot serves) | `scp_commits_persist_errors_total`; next restart has an older ring |
| Crash between rows and item | `Restore` | `incomplete` => no view, job due at once | `scp_commits_restore_total{result="incomplete"}` |
| Snapshot validator rejects | `Validator` | `Apply` returns `ErrInvalidSnapshot` => `permanentError` for that attempt; previous snapshot stays | `scp_refresh_*` of the engine, ERROR log |
| 304 without a stored ring | job | treated as a failure, next attempt sends no validator | `result="error"` |
| Badge: facts missing | `Facts` false | grey `unknown` 200 | `scp_badges_total{state="unknown"}` |
| Badge: not ready | version 0 | 503 grey `starting`, `no-store` | |

---

## 5. Data

### 5.1 Snapshot slots and globals

| Name | Kind | Type | Producer | Consumers | Size |
|---|---|---|---|---|---|
| `commits.view` | per-repository slot | `commits.View` | `commits.refresh` (via `Pub.Apply` with one `Put`) and `Restore` | `commits` handler and projector; `overview` (root import, rank 5); `badges` via `bridge.BadgeFacts` (only the newest date and `Info`) | up to `keep` commits x about 450 bytes (SHA 40, subject up to 200 runes, URL about 80, names) = about 13.5 KB per repository, 13.5 MB at 1,000 repositories (the architecture's memory budget 9.4 counts 30 commits per repository) |
| `commits.latest` | global | `scm.Classed[commits.Latest]` | the `commits` projector inside every `Apply` | `commits` handler (fast path), `overview` | `LatestN + 1` refs (about 100 bytes) x (1 + groups) x 2 classes: at 51 groups, 51 refs: about 0.5 MB |

Ownership rules (architecture 6.1): a value is immutable after `Put` and **`Put` takes ownership**. `commits` builds the `[]scm.Commit` once in `normalise` and never touches it again; revalidation republishes the same slice under a new `Info`. Nothing in `commits.latest` aliases a mutable structure: it holds `CommitRef` values (slug, SHA, time), which the handler resolves through the slot of the *same* snapshot, so a feed entry and its commit can never come from different generations (the validator proves the entry resolves, section 4.6).

`badges` owns no slot and no store item.

### 5.2 Store usage

Uses only the generic tables of `03-readmodel-store.md` (no commits-specific DDL, no migration owned by this unit):

```sql
-- item: one row per (repo, kind); kind 'commits.ring'
--   status        'ok' | 'no_content' (| 'error' only through RecordFailure on a row that never had a good value)
--   fetched_at    last successful fetch or successful validation (Revalidated)
--   content_sha   branch head SHA the ring was built from
--   payload       ringRecord JSON {"schema":1,"branch":"main","head":"<sha>","shas":["<sha>", ...]}
-- commit_log: body store, union semantics, trimmed to the newest `keep` by (committed_at, id)
CREATE TABLE commit_log (id INTEGER PRIMARY KEY, repo_id INTEGER NOT NULL REFERENCES repo(id) ON DELETE CASCADE,
  sha TEXT NOT NULL, committed_at INTEGER NOT NULL, payload BLOB NOT NULL, UNIQUE (repo_id, sha)) STRICT;
CREATE INDEX commit_log_recent ON commit_log (repo_id, committed_at DESC, id DESC);
```

Row payload (`rowRecord`, schema version `v: 1`): `{"v":1,"s":"<subject>","l":"<login>","n":"<name>","u":"<https url>"}`; keys with empty values are omitted. SHA and `committed_at` live in the columns. No e-mail address, no committer/author id, no message body beyond the subject (GR-35).

**Plan-kind rule (03 section 2.5, verification V-02, open until T-IN-8 lands).** The planner persists its `NextDue` and streak on the **item** row, addressed by item kind, while the engine speaks job kinds. This unit stores its item under kind `commits.ring` for job kind `commits.refresh`, so it exports `commits.ItemKind` next to `commits.JobKind` and `cmd/scp` must (1) give the `PlanSink` bridge the pair `{commits.JobKind: commits.ItemKind}`, (2) list `commits.ItemKind` (never `JobKind`) in the store's `Deps.KnownKinds`, otherwise `Open` prunes every ring at every start, and (3) pass the pair to the startup assertion `TestEveryRepoJobKindHasAPlanItemKind`. The first draft did not mention any of this although unit 03 names this unit as the reason for the rule.

Write sequence per change: `AppendCommits(repo, rows, keep)` (one transaction: insert-or-ignore, trim) then `PutItem(ring item)`. Per unchanged confirmation: `Revalidated(itemKey, now)` only (no rows). Per failure: `RecordFailure`. Estimated writes at 1,000 repositories: a change costs one transaction of at most 30 inserts that mostly ignore (existing SHAs) plus one item upsert; with the head-sweep skip an unchanged repository costs one tiny `UPDATE` every 2 h to 12 h.

### 5.3 Retention and consistency

| Data | Retention | Mechanism |
|---|---|---|
| ring in memory | newest `keep` | replaced wholesale at every change |
| `commit_log` rows | newest `keep` per repository | trim in the same transaction as the insert (`03` section on retention; `scp_store_commits_trimmed_total`) |
| `commits.ring` item | one per repository | upsert; deleted with the repository by `ON DELETE CASCADE` |

Two consistency edge cases are accepted and self-healing, and both have tests:

1. **Trim boundary tie.** The store trims by `(committed_at, id)` while the ring orders by `(committed_at, sha)`; with several commits in the same second at the cut, the store may drop a row the ring lists. `Restore` then finds a listed SHA missing, skips the repository (`incomplete`), and the job re-fetches at once.
2. **Crash between rows and item.** Rows are committed first, the membership item second. A crash in between leaves the previous membership item and extra rows (union semantics): the old ring restores correctly from its own SHAs, and the next job run replaces it.

### 5.4 Migrations

None owned. `ringRecord.schema` and `rowRecord.v` are versioned in the payload; an unknown version is treated as `invalid` at restore (the repository refetches), so a downgrade after an upgrade that bumped them is safe. A future change of the item layout bumps `ringSchema` and keeps a decoder for the previous one for one release.

### 5.5 Capacity (1,000 repositories, 30 commits)

Snapshot: 13.5 MB of ring data. Store: 30,000 rows x about 400 bytes = about 12 MB plus indexes. API page: at `limit=200`, at most 200 x about 450 bytes = about 90 KB JSON.

---

## 6. Configuration

Owned keys (root YAML, feature-owned `Config` types composed in `cmd/scp/config.go`; strict decode; env override `SCP_` + path with `_` for nesting and dots, e.g. `SCP_COMMITS_KEEP=50`; durations are Go duration strings):

| Key | Env | Type | Default | Validation (path-accurate errors, all collected) | Consumer |
|---|---|---|---|---|---|
| `commits.keep` | `SCP_COMMITS_KEEP` | int | 30 | 1..100 (one GitHub page) | `run` (fetch size, ring cap), `Restore`, `AppendCommits` trim, `badges`-independent |
| `commits.latest_n` | `SCP_COMMITS_LATEST_N` | int | 50 | 1..200 (`limit` maximum is 200: a feed longer than any page is pointless) | `Projector`, handler fast-path predicate |
| `commits.refresh_ttl` | `SCP_COMMITS_REFRESH_TTL` | duration | 2h | 1m..24h | `Jobs()` policy TTL; `FeedInfo.TTL`; freshness 1.5 x rule |
| `commits.idle_backoff_max` | `SCP_COMMITS_IDLE_BACKOFF_MAX` | duration | 12h | `refresh_ttl`..168h | `Jobs()` policy |
| `badges.palette` | `SCP_BADGES_PALETTE` | enum | `aa` | `aa` or `aaa` | `New` (palette), `render` |
| `badges.cache_max_age` | `SCP_BADGES_CACHE_MAX_AGE` | duration | 5m | 0..1h; `0` means `no-cache` | `respond` (Cache-Control of data-carrying badges) |

Architecture section 16 lists `commits: {keep: 30, latest_n: 50}` and `badges: {palette: aa, cache_max_age: 5m}`. `refresh_ttl` and `idle_backoff_max` are additions because 9.6 says each policy value is a config key with a consumer (D-3). Cross-section dependencies owned by other units that this unit reads (not owned here): `site.display_time_zone` reaches the SPA, not the server code of this unit; `ratelimit.badges` (120/min per client, class `badges`) is enforced by middleware in `httpapi`; `manifests.enabled` becomes `badges.Deps.Enabled` in `cmd/scp`; integration provider presence becomes the nil-ness of `RepoFacts.Build`/`Security` in the bridge.

Startup validation refuses an out-of-range value with the full path (`commits.keep: must be between 1 and 100 (GitHub page size), got 150`).

---

## 7. HTTP / API surface

All routes are public-listener routes owned by these packages; the route names and patterns are in the `routes` table (architecture 11.2) and the API contract for the JSON route is in `20-design/api/openapi-public.yaml`.

| Route name | Pattern | Owner | Content type | Authz |
|---|---|---|---|---|
| `api.commits` | `GET /api/v1/commits` | commits | `application/json` | public listener; class from `authz.ClassOf`; repository visibility enforced as in section 4.10 |
| `badge` | `GET /badges/{repo}/{file}` | badges | `image/svg+xml` | repository visibility via `LookupRepo`; public repositories for everyone |
| `badge.group` | `GET /badges/groups/{groupId}/{file}` | badges | `image/svg+xml` | count follows the caller's class |

`/badges/groups/{groupId}/{file}` is a three-segment path that cannot collide with the two-segment repo route; the slug `groups` is reserved by the catalogue (architecture 11.3). `{file}` is a kind plus `.svg` because `{kind}.svg` is not a legal ServeMux wildcard (A-12).

### 7.1 `GET /api/v1/commits`

Request: section 4.10. Response `200`, `application/json` (shape = generated `CommitPage`: `items` are `openapi.Commit`, `page` is `openapi.PageInfo`, `freshness` is `openapi.Freshness`):

```json
{
  "items": [
    {"sha": "3f2a9c1e...", "repo": "billing-service", "subject": "Fix rounding in invoice totals",
     "authorName": "Ada Lovelace", "authorLogin": "ada", "committedAt": "2026-10-14T08:12:44Z",
     "url": "https://github.com/acme/billing-service/commit/3f2a9c1e..."}
  ],
  "page": {"nextCursor": "AXsidCI6MTc2MDQzMTIwMCwiciI6ImJpbGxpbmctc2VydmljZSIsInMiOiIzZjJhOWMxZDdiNWU0YTBmOGM2ZDJiMWE5ZTdmNWMzZDFiMGE4ZTZmIiwiZiI6IjRiZmQ4Nzk2MmQ0MCJ9", "limit": 50},
  "freshness": {"status": "fresh", "kind": "commits.view", "asOf": "2026-10-14T08:13:02Z",
                "fetchedAt": "2026-10-14T07:58:11Z", "staleSince": null, "lastErrorCode": null}
}
```

(the cursor above decodes to version byte `0x01` and `{"t":1760431200,"r":"billing-service","s":"3f2a9c1d7b5e4a0f8c6d2b1a9e7f5c3d1b0a8e6f","f":"4bfd87962d40"}`.) `authorName` and `authorLogin` are `null` when unknown. `committedAt` is an RFC 3339 UTC instant. `url` is an absolute `https` URL (schema `HttpsUrl`).

| Status | When | Body (RFC 9457 problem, `code`) |
|---|---|---|
| 200 | page (possibly empty) | `CommitPage` |
| 304 | `If-None-Match` matches the current ETag | none; ETag and `Cache-Control` repeated |
| 400 | `limit` not in 1..200; malformed cursor | `validation-failed`, `invalid-cursor` |
| 404 | unknown or invisible `repo`; unknown `group` | `not-found` (invisible equals unknown) |
| 429 | rate limit (middleware) | `rate-limited` |
| 503 | no snapshot yet | `not-ready`, `Retry-After: 5` |

The request validator (unit 06 chain `ProblemMux -> RequestValidator -> APIDefaults -> Gzip -> mux -> strict handler`) enforces parameter types and ranges first; `Service.List` repeats what the schema cannot say (cursor content and filter binding) and `limit >= 1` defensively, because the validator may be disabled in tests. Problems are returned as errors (`httpapi.Err(problem)`) and rendered by the strict server's `ResponseErrorHandlerFunc` (`httpapi.StrictErrors`, 06 D16).

**The strict-server method (compile-checked against the generated `commitsapi` of the final contract).** `Service.List(ctx, r, ListQuery) (ListResult, error)` takes the `*http.Request` because `httpapi.NotModified(r, etag)` and `httpapi.NextLink(r, cursor)` read it; oapi-codegen's strict handlers pass only `ctx`. Unit 06 settled this (06 D29): `cmd/scp` mounts `httpapi.WithRequest` innermost around the API mux and a strict handler reads the request with `httpapi.RequestFrom(ctx)`; the first draft's request for such a function (D-9, Q-2) is therefore closed. The feature implements the generated `StrictServerInterface` itself and registers it like every other feature (06 Appendix B, `08-catalog.md` 7.1); there is no adapter type in `cmd/scp`:

```go
func (s *Service) ListCommits(ctx context.Context, req commitsapi.ListCommitsRequestObject) (commitsapi.ListCommitsResponseObject, error) {
	r := httpapi.RequestFrom(ctx)
	if r == nil { // fails closed: without the request there is no conditional handling and no Link header
		return nil, errors.New("commits: httpapi.WithRequest is not mounted")
	}
	p := req.Params
	q := ListQuery{Repo: httpapi.Deref(p.Repo), Group: httpapi.Deref(p.Group), Limit: p.Limit, Cursor: httpapi.Deref(p.Cursor)}
	res, err := s.List(ctx, r, q)
	if err != nil {
		return nil, err // a *ProblemError: rendered by the ResponseErrorHandlerFunc of httpapi.StrictErrors (06 D16)
	}
	if res.NotModified {
		return commitsapi.ListCommits304Response{Headers: openapi.NotModifiedResponseHeaders{ETag: &res.ETag}}, nil
	}
	h := commitsapi.ListCommits200ResponseHeaders{ETag: &res.ETag}
	if res.Link != "" {
		h.Link = &res.Link
	}
	return commitsapi.ListCommits200JSONResponse{Body: commitsapi.CommitPage{Items: res.Items, Page: res.Page, Freshness: res.Freshness}, Headers: h}, nil
}

func (s *Service) Mount(m httpapi.Mux) {
	reqErr, respErr := httpapi.StrictErrors(s.d.Log)
	h := commitsapi.NewStrictHandlerWithOptions(s, nil, commitsapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: reqErr, ResponseErrorHandlerFunc: respErr})
	commitsapi.HandlerWithOptions(h, commitsapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}
```

The generated parameter types are aliases (`Limit = int`, `Cursor = string`, `Slug = string`, `GroupId = string`), so `p.Limit` is a `*int` and needs no conversion. The tests drive the route **through this generated handler** (`mounted(e)` = `httpapi.WithRequest(mux)` with `Mount` on it): fast and merge paths, paging, 304, 400 (`limit=abc` is a binding error answered by `BindingError`; `limit=0` and a foreign cursor by `List`), 404, and the fail-closed 500 without `WithRequest`. The `apitest` conformance suite of unit 06 (`TestListCommitsConformance`, planned) covers the schema of every response in the real tree.

### 7.2 Caching and ETag (`x-scp-etag: snapshot-variant`)

* ETag: `httpapi.WithDigest(httpapi.WeakETag(snapshotVersion, classTag), httpapi.FreshnessDigest(status.String()))` (the convention of 06 D15 and Appendix B; the first draft put the status in as a third part and rendered `W/"g1842-p-fresh"`, a shape no other unit and not `apitest.ETagCoversBody` expects). While the derived status is `fresh` the tag is the short `W/"g1842-p"`; otherwise a digest of the status is appended inside the quotes (`W/"g1842-p.<16 hex>"`). The tag carries the snapshot version, the view class (`p`ublic or `a`ll; the body differs per class) and the **derived freshness status** (`fresh`, `stale`, `error`, `unknown`). The status is in the validator because it ages with the clock without a new snapshot and the body contains it: an ETag of the version alone would answer 304 for a body that now says `stale` (06 D15). `TestETagFollowsDerivedStatus` (`testing/synctest`): ETag unchanged and 304 at +2 h (TTL 2 h, threshold 1.5 TTL = 3 h), 200 with `status: stale`, a `staleSince` and a different ETag at +4 h on the same snapshot. `httpapi.NotModified` compares weakly, so any `W/"..."` or plain form matches.
* `Cache-Control` is **not set by the handler**: `httpapi.APIDefaults` adds `no-cache` (or `private, no-cache` when `access.mode != public`) to every 2xx/304 `GET` under `/api/` that carries none (06 section 4.4). A response that depends on the principal's class is therefore always `private` exactly when identity can matter (`access.mode != public`); in `public` mode there is only one class and the response is shareable with revalidation.
* `Vary`: none from this handler (`Accept-Encoding` is added by the gzip middleware).
* `Link: <path?query&cursor=...>; rel="next"` via `httpapi.NextLink` (a relative reference: no `Host` header is trusted) when there is a next page.
* Never any `Access-Control-*` header (06: no CORS on `/api/`).

Cost (1,000 repositories x 30 commits, 51 groups of 40, Apple M4 Max, Go 1.27.1, 2026-10-14, medians of 3 runs of `go test -bench`, re-measured by the verifier on the final code): `Service.List` without JSON encoding: precomputed first page 3.8 us (10 KB, 133 allocs), precomputed group page 4.3 us, merge path first page of 200: 143 us (300 KB, 445 allocs), merge path with a cursor, 50 entries: 146 us (268 KB); `Merge` alone over 1,000 rings, 50 entries: first page 42 us, cursor into the middle of the 30,000 commits 89 us (1 alloc). One whole `Apply` (publish): 0.81 ms. `resolve` 34.2 us (engine path only, not a request path). (The author's figures of 33.6 and 73.6 us for `Merge` were 15 to 20 % lower; the ordering change of V-08 swaps one `strings.Compare` argument order and no code path of `Merge`, so the difference is not attributed to it; both are far inside the 5x gates.) **CI gates** (5x headroom, run by `go test -bench` in the nightly job and compared with the stored baseline, GR-28): precomputed page <= 25 us, merge page <= 1 ms and <= 1 MB, publish <= 5 ms, badge render <= 25 us.

### 7.3 Authorisation summary (commits)

| Caller | `/api/v1/commits` |
|---|---|
| anonymous, `access.mode: public` | public repositories only; `class = ViewPublic` |
| authenticated principal with `CanView` on non-public repositories | `class = ViewAll`; private response caching |
| any caller, repository not visible | `404` identical to non-existent; absent from every feed and from `FeedInfo` counts of its class (the anonymous enumeration test compares counts of both classes, A-23) |

### 7.4 Badge responses

Common response headers of every badge (`200`, `404`, `503`):

```
Content-Type: image/svg+xml
X-Content-Type-Options: nosniff
Content-Security-Policy: default-src 'none'; sandbox
Cross-Origin-Resource-Policy: cross-origin
Content-Length: <n>
```

No `Access-Control-Allow-Origin` (an `<img>` does not need CORS; leaving it off prevents `fetch()` from other origins reading badge text), no `Set-Cookie`, no `Vary: Cookie`. The `sandbox` CSP matters when someone opens the SVG URL directly: scripts in it (there are none by construction) could not run, and the response is a document with no capability (GR-33, defence in depth).

| Status | Condition | `Cache-Control` | ETag |
|---|---|---|---|
| 200 | data badge | `public, max-age=<cache_max_age>` (default 300) or `private, max-age=300` when the repository is not public | strong, `"<16 hex of sha256(body)>"` |
| 200 | `unknown` state | `public, max-age=min(cache_max_age, 60)` (private variant likewise) | strong |
| 200 | `cache_max_age = 0` | `public, no-cache` | strong |
| 304 | `If-None-Match` equals the body hash | same as the 200 | repeated |
| 404 | unknown repo, hidden repo, bad kind, disabled kind, bad group | `public, max-age=60` | none |
| 503 | not ready | `no-store`, `Retry-After: 15` | none |

**Why a content-hash ETag, not the snapshot generation.** `last commit: today` becomes `yesterday` at 00:00 UTC without any data change. A generation-based ETag would let caches confirm the stale text with 304 indefinitely. Hashing the body costs about 1 us for 500 bytes and makes the ETag change exactly when pixels change; `TestETagFollowsBodyAcrossMidnight` (synctest) proves it. A non-200 never carries an ETag (a cache validating a 404 with 304 would be wrong, and 404s expire in 60 s anyway).

**Camo caveats (recorded, not engineered around).** GitHub's image proxy caches by URL, ignores most conditional semantics, and honours `max-age` loosely (a typical README badge refreshes in minutes to hours); the portal therefore sets `max-age` 5 min rather than relying on revalidation, serves a renamed repository's badge directly under its old slug instead of redirecting, and returns small bodies (about 500 bytes, well under Camo's limits). A badge that must be fresher than Camo's cache is not achievable from the origin; research 06 records this and the SPA, which fetches directly, is not affected. Camo's treatment of redirects and of `Cache-Control: private` remains unverified (Q-3).

`private` for non-public repositories: an authenticated caller's badge must not be stored in a shared cache; such badges cannot be embedded in public READMEs by design (a private repository's README is only visible to its readers, whose browsers send the session).

---

## 8. Concurrency and lifecycle

| Component | Goroutines | Shared state | Notes |
|---|---|---|---|
| `commits.Service` | none of its own | the push marker (`sync.Map` of repository key to generation, plus an atomic counter; bounded by the repository count, 4.1); everything else is in the snapshot and the store | `run` is executed by an engine worker (one running instance per key; the engine coalesces); `React` on the webhook goroutine; handlers on `net/http` goroutines |
| `Projector` | runs inside `Apply` on the publisher's goroutine | reads the candidate snapshot, writes the builder | pure; must not block (the whole `Apply` is 0.81 ms at 1,000 repositories) |
| `badges.Service` | none | none (palette and enabled-kind set are immutable after `New`) | every request reads `Pub.Current()` once and works on that snapshot |

Rules:

* **One snapshot per request.** Handlers call `Pub.Current()` exactly once and use the value for the whole request, so the lookup, the facts and the render are consistent even while publishes continue (the `*Snapshot` is immutable).
* **Locks.** None in these packages. `readmodel.Publisher.Apply` serialises writers; `commits.run` calls it once per outcome and holds no lock across the GitHub call.
* **Context.** `run` passes the job context to `LatestCommits` and to every store call; cancellation during shutdown returns the context error, classified as `deferred` (not a data failure, not persisted).
* **Start.** `New` has no side effects. `cmd/scp` registers jobs, reactor, projector, validator, restore, coverage and routes in the order of the startup sequence (architecture 10.1: store open, warm start with `Restore`, engine start, listeners).
* **Stop.** Nothing to stop: no goroutines, no buffers to flush. In-flight engine jobs finish or are cancelled by the engine's grace period (`refresh.grace`); handlers by `http.Server.Shutdown`.
* **Webhook flood.** `React` is O(repository scan) (about 20 us per 1,000 repositories) plus a non-blocking `Enqueue`; the engine coalesces keys, so a burst of pushes to one repository yields at most one queued and one running job (`marked_dirty`).
* **Race tests.** `go test -race` covers: concurrent `run` for different repositories against concurrent handler calls (`TestRunChangedUnchangedAndSkip`, `TestHandlerFastPathEqualsMergePathAndPaging`), and parallel badge requests during publishes.

---

## 9. Observability

Names are constants next to the instruments; the `cmd/scp` registry test compares them with the metrics reference (architecture 14 observability list). Labels are closed sets (no slug, SHA or user input as a label value: unbounded cardinality is prevented by construction).

### 9.1 Metrics

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `scp_commits_refresh_total` | counter | `result`: `changed`, `unchanged`, `skipped_head`, `not_modified`, `empty`, `error`, `deferred`, `gone` | outcome of `commits.refresh` |
| `scp_commits_dropped_total` | counter | `reason`: `no_date`, `invalid_sha`, `no_url`, `duplicate` | commits rejected by validation |
| `scp_commits_clamped_total` | counter | | future-dated commits clamped |
| `scp_commits_evicted_total` | counter | `reason`: `aged_out`, `rewritten` | commits that left a ring |
| `scp_commits_persist_errors_total` | counter | | store writes of the job that failed (served from memory anyway) |
| `scp_commits_restore_total` | counter | `result`: `ok`, `invalid`, `incomplete` | warm-start outcome per repository |
| `scp_commits_reactions_total` | counter | `result`: `enqueued`, `not_default_branch`, `unknown_repo` | webhook / head-sweep reactions |
| `scp_commits_merge_seconds` | histogram | `path`: `fast`, `merge` | duration of one feed page |
| `scp_commits_retained` | observable gauge | | commits held in the current snapshot |
| `scp_badges_total` | counter | `kind` (closed set plus `invalid`), `state`: `ok`, `none`, `unknown`, `not_found`, `unavailable` | badge responses |
| `scp_badges_conditional_total` | counter | `result`: `200`, `304` | conditional-request effectiveness |

The engine contributes the per-kind job metrics (`scp_sync_jobs_total{kind="commits.refresh"}`, `scp_job_duration_seconds{kind}`) and `ops` (unit 15) contributes `scp_item_age_seconds{kind,stat}` from `freshness.Info.FetchedAt` (D15-6, MT-02: the engine knows no fetch time); this unit duplicates neither. Suggested alerts (documented in the operations runbook, not code): `rate(scp_commits_refresh_total{result="error"}) / rate(...) > 0.2` for 15 min; `scp_commits_restore_total{result="incomplete"} > 5 %` of repositories after a restart; `rate(scp_commits_evicted_total{reason="rewritten"})` sustained; `scp_badges_total{state="not_found"}` spike (broken embeds).

### 9.2 Logs

Structured `slog` with the injected logger (`sloglint` enforces key style); no log line contains a commit subject, author name or URL. Fields: `repo` (slug), `fetched`, `kept`, `head` (SHA, public), `result`.

| Level | Event |
|---|---|
| DEBUG | `commits refreshed` (per change) |
| WARN | `commits: store write failed; publishing anyway` |
| ERROR | the engine's `permanentError` for an invalid snapshot candidate |
| INFO | none per request; badge handlers do not log (the access log middleware carries `r.Pattern` and status) |

### 9.3 Spans

`commits.ingest` (one per `run`, attributes `repo`); child spans come from the GitHub adapter and the store. Handlers get the `otelhttp` span of the router (route pattern as name, A-24); no per-badge span. Tracer and meter are injected, never global.

### 9.4 `/status` contribution

`Coverage` gives `commits: have/total`; per-repository job state is shown by the engine's admin view. The freshness block of `api.commits` is the user-visible health of the feed.

---

## 10. Security

Trust boundaries (architecture 17): commit data and every string in it come from repository authors (untrusted, may be hostile or merely odd); badge requests come from anyone who can reach the public listener (including GitHub's image proxy); the snapshot and the store are trusted but rebuilt from untrusted input on every fetch.

| Threat | Where | Control (and the test that proves it) |
|---|---|---|
| Markup / script injection through badge text (an SPDX id, tag or count spliced into markup; GR-33) | `badges.Render` | `encoding/xml` marshals typed structs; no string concatenation; closed element and attribute sets; colours are palette constants (`Color` has no constructor from a string). `FuzzRenderWellFormed`, `aa-hostile.svg` golden |
| Third-party SVG served from the portal origin (GR-33) | badges | no upstream SVG is fetched, parsed or served; integrations deliver `Fact{Message, Tone, Stale}` with closed provider-owned message strings. `TestEvaluateStates` (`build`, `security` rows) |
| SVG opened as a document executes script | `/badges/*` | headers `Content-Security-Policy: default-src 'none'; sandbox`, `X-Content-Type-Options: nosniff`, `Cross-Origin-Resource-Policy: cross-origin` (embeddable in READMEs, not readable by other origins' scripts); no CORS. `TestHandlerHeadersAndConditional` |
| Existence oracle for private repositories through badges or the commit API | `LookupRepo`, `authz.ClassOf` | one existence check (`LookupRepo` against the caller's class); identical status, body bytes and `Cache-Control` for hidden and missing; feeds and counts computed per view class; anonymous enumeration test. `TestHandlerUnknownAndHidden`, `TestHandlerHidesPrivateRepoFromAnonymous`, `TestProjectorViewClassesAndValidator` |
| Shared-cache leak of a private repository's badge or commit page | badges, `api.commits` | `private` for non-public repositories (badges), `APIDefaults` `private, no-cache` whenever `access.mode != public` (API); the class is in the ETag; no `Vary: Cookie` games |
| Display spoofing and prompt smuggling in commit subjects and author names (bidi overrides, zero-width characters, control characters, U+202E, the Unicode tag block U+E0000..E007F that hides ASCII text from a reader but not from an LLM) | `normalise` | `cleanLine` = the kernel's `scm.CleanLine` (SC-09), first line only, collapsed spaces, rune caps. `TestCleanLine`, `TestCleanLineUsesKernelCleaner` (tag characters, soft hyphen, U+2062, U+2066) |
| Hidden repository reachable through another spelling of its name | `badges`, `api.commits` | the path value / `repo` parameter is ASCII-lower-cased, then aliased, then `LookupRepo` (which rejects everything but canonical slugs); every spelling of a hidden repository gives the same 404 as a missing one. `TestHandlerFoldsTheRepositoryCase` |
| Shared-cache shadowing of a 404 (accepted, low) | badges | the 404 of a repository the caller may not see is `public, max-age=60` (it must equal the 404 of a missing one, byte for byte); a shared cache in front of an `access.mode != public` deployment may therefore show an authorised user `unknown` for a private repository for up to 60 s. Private-repository badges cannot be embedded in public READMEs anyway (7.4); deployments with a shared cache should bypass it for `/badges/` |
| Dangerous links (`javascript:`, `data:`, credentials in URLs) | `normalise`, API schema | only absolute `https` URLs without userinfo, at most 2,048 bytes (`cleanURL`); the URL is derived from the repository's own `HTMLURL` when the adapter's value is unusable. `TestCleanURLAndLogin`. The schema type is `HttpsUrl` |
| Login-based injection or avatar URL forgery | `cleanLogin` | GitHub login syntax only; the SPA builds nothing from it except text |
| Unbounded growth | commits | ring at most `keep` (1..100); rows trimmed in the same transaction; subjects 200 runes, names 100, URLs 2 KB. Worst case per repository about 100 x 2.5 KB. GR-30 |
| Cursor tampering or forgery | `api.commits` | strict versioned decode (unknown field, trailing data rejected), `<= 512` bytes, shape validated, **bound to the filter** by `QueryFingerprint`, carries no authority (visibility is re-applied on every page). `TestHandlerRejectsBadLimitAndForeignCursor`, `FuzzMerge` (positions) |
| CPU / memory exhaustion by crafted requests | handlers | `limit <= 200`; merge cost `O(k log n + limit log k)`; badge render 4.25 us, 9 KB; badge text capped at 40 runes; path values bounded (slug <= 100 by `LookupRepo`'s syntax check, other path values <= 200 bytes); the `badges` rate class (120/min per client) and `api` class apply in middleware |
| A webhook choosing which branch is fetched | `React` | the default branch comes from the snapshot, never from the event; a push to another ref is ignored |
| Error text or internal state on the wire | job errors, API | `ErrCode` is a closed set; problems are generated by `httpapi` constructors without internal text; logs never contain commit subjects, names or URLs |
| Personal data | store | no e-mail addresses are fetched into a field or stored (GR-35); login and display name are public GitHub profile data already shown by GitHub, kept because the UI shows them |
| Clock abuse by a future-dated commit | `normalise` | clamp (> now + 24 h) and carry-over of the clamped date. `TestNormaliseClampIsStableAcrossRefreshes` |
| Width-table or font supply chain | `tools/badgewidths` | the generator verifies the font's SHA-256 before writing; the output is committed and CI-diffed; `x/image` stays in the nested tools module; the font licence file is committed next to the font |
| Server-side request forgery | none | neither package makes an outbound request; `CommitLister` is the adapter's responsibility (unit 05) |

Not in this unit: HTTPS and HSTS, the general security-header middleware (GR-18; the badge headers above are set explicitly because the SVG route is not an HTML document), authentication and the `Authorizer` (unit 14), webhook signature verification (unit 04).

---

## 11. Traceability

### 11.1 Capabilities and goals

| Capability or goal | Handled in |
|---|---|
| Commit log, all repositories | 4.10 (unfiltered), 7.1; `Merge`, `commits.latest` fast path; client grouping 4.9 |
| Commit log, per group | 4.10 `group` filter, `ByGroup` feeds (4.6); intersection with `repo` |
| Commit log, per repository | 4.10 `repo` filter, default branch only (Q-18) |
| Commit ingestion, newest 30 on the default branch | 4.1 to 4.4, 5; job `commits.refresh`, `store.AppendCommits`, ring membership item |
| Licence display | detection is `catalog.stats` (GitHub's SPDX id); display is the `licence` badge, 4.11 (`NOASSERTION`/`OTHER` => `other`, none => `none`) |
| Badge endpoints | 4.13: `/badges/{repo}/{file}` and `/badges/groups/{groupId}/{file}`; 404/503/200-grey semantics |
| Build-status badge (optional integration) | kind `build`, `Fact` from integrations (2.3, 2.4, 4.11) |
| Security-scan badge (optional integration) | kind `security`, `Fact` |
| Issues, last-commit and release badges | kinds `issues`, `last-commit`, `release`; rendered locally (4.12) from `catalog.stats` and `commits.view` |
| Licence badge | kind `licence` (the American spelling `license.svg` is accepted) |
| Repository-count badge | `GET /badges/groups/{groupId}/repos.svg`, 4.13; count per view class |
| "Unknown" and "none" badge states | state `unknown` / `none`, 404 grey, 503 grey `starting` (4.11, 4.13, 7.4); no image files read per request |
| G-02 Freshness model on every fetched item | `View.Info` through the kernel state machine; `FeedInfo.Derive`; `freshness` block of the API; ETag covers the derived status (7.2) |
| G-03 Conditional requests and rate-aware scheduling | `Validators{SHA}` and `NotModified` (4.1), head-sweep skip (no request), idle back-off, `RateLimited` deferral (4.4), webhook accelerator (4.5) |
| G-05 Stale-while-error with a visible staleness indicator | `AfterFailure` keeps the ring; `status: stale` and `staleSince`; badge `(stale)` text for integration facts (4.4, 4.11) |
| G-10 Route table with named routes and crawl test | `RouteBadge`, `RouteBadgeGroup`, `api.commits` are names in `routes`; badge URLs in API payloads are built by `routes.Badge(slug, kind)` (not here); crawl test in unit 01/06 follows `badges[].url` |
| Dashboard latest-commits panel and live refresh | `commits.latest` is precomputed inside every `Apply` per view class and serves the first page of `GET /api/v1/commits` (fast path, 4.6, 4.10); a dashboard refresh is one cheap ETag-validated request, never a sort of the whole data set (7.2, GR-28); the v1.1 `live` unit invalidates topic `commits` (15: `commits.view`, `commits.latest`) |
| Group page (repository table, latest commits) | `Latest.ByGroup[g]` behind `group=` (4.6, 4.10); the group's `repos` count badge (4.13) |
| Branch configuration | the ring is fetched from `catalog.repo`'s `DefaultBranch` (the catalogue's effective tracked ref, 08 D-1), never a literal branch name; the branch name is kept in the ring record (3.5) |
| Release and tag data | the `release` badge reads `catalog.stats.Release` (4.11); this unit fetches no release data |
| Webhook `push`: commit refresh | `React` (4.5): one coalesced `commits.refresh` per repository, matched on `refs/heads/<default branch>` against the catalogue, never per commit and never from the event's names; the push marker makes the run fetch although the head sweep is stale (4.1) |
| Visibility policy (public / private / all) | `scm.ViewClass` end to end: `Classed[Latest]`, `Membership.VisibleReposOf(g, class)`, `authz.LookupRepo`, `private` badge caching, anonymous enumeration test (4.10, 4.13, 7.3, 7.4) |
| Latest-commit count limits | `commits.latest_n` (default 50) and the API `limit` (1..200), both real keys with a consumer (6, GR-12) |
| G-04 Readiness gating | `Coverage` and `readiness.core_kinds: [commits]` (4.8); `EnqueueUnrestored` after the warm start (4.7) |
| G-08 Authentication/authorisation option | class from `authz.ClassOf`; one existence check through `LookupRepo` in both packages (4.10, 4.13) |
| G-12 Structured logs, request ids, trace context | `slog` with the injected logger, no commit text in logs, span `commits.ingest` (9.2, 9.3) |
| G-14 Content security policy and security headers | the explicit badge header set (7.4: `nosniff`, `default-src 'none'; sandbox`, CORP) |

### 11.2 Guardrails (`02-design-guardrails.md`)

| Guardrail | What it rules out here | Mechanism in this unit |
|---|---|---|
| GR-07 Every entity has its own identity | keys whose equality is wrong; a SHA used as the identity of a commit across repositories; group feeds stored as copies | commits are keyed by `(repo slug, sha)`; group feeds are queries over membership; `Position` and `CommitRef` are comparable value types; the order key includes slug and SHA (D-4) |
| GR-08 Behaviour is deterministic and independent of order | comparators that collapse entries; sort order used as identity; "first group wins" | `Compare` is a total order (`TestMergeEqualsNaiveSortPaged`, `FuzzMerge`, `TestRingOrderIsTheKernelOrder`); sort order is never identity |
| GR-10 Git refs are resolved from the repository | a configured branch string used as the ref while the repository's `default_branch` is ignored; a branch taken from the last path segment of a ref | the ring is built from the snapshot's `DefaultBranch` (4.1); pushes are matched against the full ref `refs/heads/<default branch>` (4.5); the branch is stored in the ring record; a webhook cannot choose the branch |
| GR-12 One typed, strict, validated configuration | a configuration key that configures nothing next to a hard-coded limit | `commits.keep`, `commits.latest_n`, `refresh_ttl`, `idle_backoff_max`, `badges.*` are real keys with a consumer and path-accurate validation (6) |
| GR-15 One route table; links are generated, escaped and crawled | hand-written path routers; user-facing URLs that drift from the handlers; branch and repository names as raw path segments | `net/http` patterns `/badges/{repo}/{file}` and `/badges/groups/{groupId}/{file}` from the route table; the router owns 404/405; slugs are one segment validated by `LookupRepo`; the commit `url` is an absolute GitHub URL built or validated server-side; badge URLs are crawl-tested |
| GR-16 HTTP responses are truthful | soft 404s; a 200 placeholder for everything | badges answer real `404`/`503` (grey bodies) and `200` only for an existing, visible repository (4.13, D-7); the API answers RFC 9457 problems (`not-found`, `invalid-cursor`, `not-ready`) |
| GR-18 Browser security policy is strict by default | missing security headers; wildcard CORS | explicit headers on the badge routes (7.4); no `Access-Control-*` on any route of this unit (7.2, 7.4); API headers by middleware |
| GR-21 Webhook events are hints that become coalesced refresh jobs | a webhook refresh that fetches from the repository named by the event; a push swallowed by a stale sweep | `React` enqueues "bring this repository up to date" for the **catalogue's** ref (`KeyParts`), never from the event's names, never fetches or writes; the push marker (4.1, 4.5) |
| GR-23 Upstream failures are typed and rate limits are honoured | unclassified failures; a non-200 that silently keeps the old value | typed failure classes (`deferred`, `gone`, data failure; 4.4); closed `errCode` set |
| GR-25 Upstream requests are budgeted | fetch storms; scraping third-party badge services per repository | `Validators{SHA}` and `NotModified`, head-sweep skip, idle back-off (4.1); no badge-service calls at all: issue count, release and licence come from `catalog.stats` (one batched probe per 5 minutes) |
| GR-26 Freshness is data; reconciliation is authoritative | data that never expires or carries no `fetchedAt` | `commits.refresh` TTL 2 h with idle back-off, `freshness.Info` on every `View`, stale-while-error with `ErrCode` and `Attempts`, `freshness` block per page, removal through the catalogue (4.1, 4.4) |
| GR-28 The read model is an immutable snapshot with write-time indexes | whole-cache scans and sort-all per request | per-repository sorted rings, precomputed `commits.latest`, k-way merge with binary-search positioning; CI benchmark gates (7.2) |
| GR-29 Absence and unsupported input are typed states | caching `null`; placeholder images for unknown data | `no_content` ring (empty repository), `none`/`unknown` badges, `unknown` freshness status (4.11) |
| GR-30 Retention is bounded and measured | rings that grow without bound | bounded ring of `keep` + rows trimmed in the same transaction + `scp_commits_evicted_total` (4.3) |
| GR-33 Badges are rendered locally with structural XML escaping | third-party SVG proxied verbatim; upstream strings interpolated into SVG; unescaped attribute values | no upstream SVG is ever fetched; `encoding/xml` marshals typed structs; closed element and attribute sets; colours are palette constants; CSP `sandbox` + `nosniff` on every badge (4.12, 10) |
| GR-34 Untrusted documents are parsed safely and within limits | XML parsed with permissive defaults | badges are never parsed (no upstream SVG exists here); the only XML code is `encoding/xml` **marshalling** typed structs |
| GR-35 Access is an explicit policy; personal data is minimised | author e-mails persisted; a 403 that reveals existence | `scm.Commit` has no e-mail; `rowRecord` stores subject, login, name, URL only (5.2); invisible equals missing (404) for badges and the API |
| GR-38 Times are stored in UTC and shown with an explicit zone | undated commits crashing a view; grouping in the server's zone; stateful iterators | `normalise` drops undated commits (`no_date`) and clamps future dates; UTC whole seconds on the wire; the SPA groups with an explicit zone in a pure function (4.9, golden vectors) |
| GR-39 The frontend is accessible and purpose-written | images without text alternatives; colour as the only carrier of state | `Kind.Alt()` and the API `Badge.label` supply `alt`; the SVG has `role="img"`, `aria-label`, `<title>`; contrast tested; states are words (4.12) |
| GR-40 Live updates respect visibility and show staleness | a poll that re-sorts everything and shows no stale indicator | precomputed feed per view class, ETag that follows the derived freshness status, `freshness` block (7.2) |

---

## 12. Test plan

All tests below exist in the scratch module (names are final) unless marked **planned**. The tests added by the verification were **mutation-checked**: the fix they cover was reverted by a script and the named test failed (V-01, V-02, V-07, V-08, V-09, V-13). File names follow the target tree: `*_test.go` next to the code; goldens under `internal/badges/testdata/golden/`. (The scratch uses `testdata/golden/badges/` at the module root; the move is part of task B1.)

### 12.1 Commits: unit, table, property

| Test | Proves |
|---|---|
| `TestNormalise` | table: invalid SHA, duplicate, no date, future date clamp, uppercase SHA, SHA-256 length, URL fallback, counts in `dropStats` |
| `TestNormaliseDerivesURLFromRepository`, `TestNormaliseCapsAndKeepsNewest` | https-only URL and fallback from `HTMLURL`; the cut to `keep` keeps the newest by date, not by branch order |
| `TestNormaliseClampIsStableAcrossRefreshes` | a clamped date is carried over, a clamped commit ages |
| `TestCleanLine`, `TestCleanURLAndLogin` | bidi and zero-width removal, first line, rune cap with ellipsis, `javascript:`/`data:`/userinfo/oversize URL rejected, login syntax |
| `TestMergeEqualsNaiveSortPaged` | **property**: `Merge` over random rings (including identical SHAs in two repositories and equal timestamps) paged with every limit equals sorting the union and slicing |
| `TestMergeCursorSurvivesInsertionAtTop`, `TestMergeLimitEdges` | cursor stability under change; `limit` 0, 1, more than available; empty and single sources |
| `FuzzMerge` | arbitrary rings and positions: never panics, strictly increasing order, no duplicates, `more` exact (1,782,128 executions in 20 s) |
| `TestProjectorViewClassesAndValidator` | public and all variants differ exactly by the non-public repository; `FeedInfo.Repos` follows the class |
| `TestValidatorRejectsDanglingRef` | a feed naming a commit the ring does not hold is rejected (`ErrInvalidSnapshot`, permanent) and the previous snapshot stays current |
| `TestFastPathBoundaryAtLatestN` | precomputed first page == merge path for totals just below, at and above `latest_n`; `nextCursor` presence is exact (sentinel) |
| `TestRingOrderIsTheKernelOrder` | **property** (200 random rings with equal timestamps and repeated SHAs): `compareWithin` sorts exactly like `scm.CompareCommits` and `Compare` within one slug agrees (V-08) |
| `TestCleanLineUsesKernelCleaner` | tag-block characters, soft hyphen, U+2062 and U+2066 removed; cap of exactly `limit` runes is not cut, longer is cut with `…`; first line only (V-07) |
| `TestRowRecordHasNoEmailAndOnlyDocumentedKeys` | the persisted row has only `v s l n u` and no `@` (GR-35) |

### 12.2 Commits: job, store, restore (fake lister; fake store with the real `AppendCommits` semantics **and the store contract's validation**: folded `RepoKey.Validate`, `ValidateCommit` (non-empty payload), `ValidateItem` (result states only), `ValidateFailure`, `ErrNoItem` from `Revalidated` on a missing or `error` row, an `error` row created by `RecordFailure`; the first draft's fake accepted everything, so a mixed-case key or an empty payload could not fail a test, V-20)

| Test | Proves |
|---|---|
| `TestRunChangedUnchangedAndSkip` | change publishes and persists; same head and SHAs revalidates without rows; the head-sweep skip makes **no** lister call; idle back-off outcome |
| `TestRunFailureIsStaleWhileError` | ring and `FetchedAt` survive a failure; `State error`, `ErrCode`, `Attempts`; `Derive` = `stale`; error returned to the engine |
| `TestRunGoneAndRateLimited` | unknown repository is `job.IsGone`; a rate-limit error is returned untouched and writes nothing |
| `TestRestoreIgnoresOrphansAfterForcePush` | union-semantics store holds orphans; restore selects only the membership list |
| `TestRestoreIncompleteRingYieldsNoView` | a lost row yields no view, the repository is due at once |
| `TestRestoreOfAFailingRepositoryIsStale` | a failing repository restarts as `stale`, `Coverage` counts it |
| `TestReact` | default-branch push enqueues at `PrioWebhook`; other refs, deleted, unknown repository, other event kinds do not; case-insensitive owner/name |
| `TestJobSpecIsValidForTheEngine` | `job.Spec.Validate` passes; kind, scope, upstream |
| `TestConcurrentJobsAndReaders` (`-race`) | eight jobs publishing in parallel while four readers page: every page sorted and consistent; coverage 8/8 |
| `TestReactKeyIsFoldedAndRunWritesTheFoldedStoreKey` | a catalogue entry `Acme/Billing-Service` and an event spelled `ACME/BILLING-SERVICE`: the enqueued key is `commits.refresh/s/acme/billing-service` (equals `job.NewKey(JobKind, ref.KeyParts()...)`), the run under it writes the rows under the folded `RepoKey` (V-01; mutation-checked) |
| `TestPushBypassesTheHeadSweepSkip` | without a push a fresh sweep value that equals the ring's head is trusted (no lister call); after a push the run fetches although the sweep is stale; the marker is consumed by a successful fetch; a push that lands **during** the fetch survives it and causes exactly one more fetch (V-02; mutation-checked: no bypass, unconditional delete) |
| `TestRevalidationHealsAMissingStoreItem` | the store lost the item and rows; the next confirmation (`unchanged`) writes the served ring again (V-09; mutation-checked) |
| `TestRestoreSkipsAnErrorRowWithoutValueQuietly` | the error row of a repository that never had a ring creates no view and no `scp_commits_restore_total` increment (V-13; asserts the counter through a recording meter) |
| `TestEnqueueUnrestoredCoversSkippedRings` | only repositories without a view are enqueued, at `PrioBackfill`, with the folded key; a refusal stops the walk and is returned (V-13) |
| `TestNewRejectsMissingDependencies` | a nil `Lister`, `Meter` and `Log` are all named in one error |
| `TestRecentForOverview` | `Recent(snap, slug, n)`: n newest, a copy (mutating the result does not change the ring), the item's `Info`, `n` beyond the ring and negative `n`, `false` before the first view and for an unknown slug (R-09-1) |

### 12.3 Commits: HTTP

| Test | Proves |
|---|---|
| `TestHandlerFastPathEqualsMergePathAndPaging` | both paths yield the same items; walking every page with the cursor yields the union once, in order; `Link` header and `ETag` shape |
| `TestHandlerRejectsBadLimitAndForeignCursor` | `limit` 0/-3/201 and a malformed cursor are 400; a cursor issued for one filter is `invalid-cursor` under another |
| `TestHandlerFiltersCombineAndAliases` | `repo` + `group` intersection (non-member = empty page), alias resolved silently |
| `TestHandlerHidesPrivateRepoFromAnonymous` | the private repository is absent from every feed and 404 as a filter |
| `TestETagFollowsDerivedStatus` (`testing/synctest`) | the ETag changes when the derived status changes without a new snapshot; 304 before, 200 after |
| `TestRepoFilterIsCaseInsensitive` | `?repo=A` finds repository `a` (V-10) |
| `TestGeneratedHandlerProblems` | through the **generated** strict handler: unknown `repo` and `group` 404; `limit=abc` and a malformed cursor 400; a valid filter 200; without `httpapi.WithRequest` the operation fails closed with 500 |
| **planned** `TestListCommitsConformance` | generated adapter against `openapi-public.yaml` through unit 06's `apitest` (schema validation of every response, the `ETagCoversBody` property, HEAD, 406, no CORS) |

### 12.4 Badges

| Test | Proves |
|---|---|
| `TestGoldenSVG` + 19 golden files (`aa-*`, `aaa-*`) | byte-exact SVG per kind and palette, plus `unknown`, `repos`, `hostile`, `nonascii`, `long`; every golden also passes the closed-document check |
| `FuzzRenderWellFormed` | any label and message yields a closed, well-formed document whose width equals the sum of the rectangles (2,527,978 executions in 20 s) |
| `TestPaletteContrast` | white text on every tone: >= 4.5 (aa) and >= 7.0 (aaa) with the exact WCAG formula |
| `TestWidthTableGolden` | 30 strings (ASCII, Norwegian letters, CJK, empty, space, `W`, `i`) pinned; an independent stdlib-only Python TTF parser (`ttfwidth.py`) computed the same numbers: `issues` 33, `12 open` 45, `last commit` 65, `Apache-2.0` 63 |
| `TestFormatCountAndRelativeDay` | `999`, `1k`, `1.2k`, `999k`, `1M`; today, yesterday, 2 days .. 13 days, weeks, months, years; future instant |
| `TestEvaluateStates` | table over every kind and state (`ok`, `none`, `unknown`), including stale-while-error values, absent facts, integration `Fact` with `Stale` |
| `TestHandlerHeadersAndConditional` | fixed header set, strong ETag, 304 with the same `Cache-Control`, no CORS |
| `TestETagFollowsBodyAcrossMidnight` (`synctest`) | `today` -> `yesterday` at 00:00 UTC changes the ETag without any data change |
| `TestHandlerUnknownAndHidden` | hidden == missing (body, status, cache header); kind and repository matrix; known repository without data is 200 grey with `max-age=60`; provider not configured is 404 |
| `TestHandlerGroup` | group badge counts per class (the private repository is not counted for an anonymous caller; `private` caching for an authorised one); unknown group 404; the group form serves `repos.svg` only |
| `TestHandlerFoldsTheRepositoryCase` | `/badges/API/issues.svg` and `/badges/Old-API/issues.svg` (a mixed-case alias of a renamed repository) equal the canonical badge; `/badges/SECRET/...` stays 404 (V-10) |
| `TestNewRejectsMissingDependencies` | nil `Facts`, `Pub`, `Meter` named in one error |
| **planned** `TestBadgeWidthsMatchBrowser` (Playwright, nightly) | renders each golden in Chromium and Firefox and asserts that the text box lies within the rectangles (`getComputedTextLength` within 1 px of `textLength`) |
| **planned** `TestGeneratedWidthsAreCurrent` | CI: `cd tools && go run ./badgewidths -font badgewidths/font/DejaVuSans.ttf -out ../internal/badges/widths_gen.go` and `git diff --exit-code internal/badges/widths_gen.go` |

### 12.5 Integration (planned; fake GitHub `cmd/fakegithub` and the engine of unit 04)

| Test | Proves |
|---|---|
| `TestCommitsBudgetColdStart` | 1,000 repositories: exactly one list-commits request per repository at cold start (W13 call counting); zero after a second pass when the head sweep reports no change; one within seconds of a push webhook |
| `TestCommitsWarmRestart` | process restart with a populated store: all rings restored (`restore_total{ok}`), no list-commits request before the TTL, `commits` readiness satisfied |
| `TestAnonymousEnumeration` (e2e) | for every route in `routes.Public()`, an anonymous client cannot learn the existence, name or count of a private repository through commits or badges (compares `FeedInfo` counts of both classes) |
| `TestCrawlFollowsBadgeURLs` | the crawl test of unit 06 requests every `badges[].url` emitted by the API and gets 200 `image/svg+xml` |
| `TestForcePushRewritesRing` | fake GitHub rewrites history: new ring, `evicted_total{reason="rewritten"}`, old rows remain in `commit_log` but never served |
| `TestPushWebhookRefreshesWithinSeconds` | with the head sweep configured to 5 min and its last value fresh, a push delivery makes the new commit appear in `/api/v1/commits` within seconds (architecture 9.6 "`push` (seconds)"; the end-to-end version of V-02) |
| `TestRestartWithACorruptRingRefetchesAtOnce` | one `commits.ring` payload corrupted in the store: after the restart that repository has no view until `EnqueueUnrestored` runs, then it is fetched without waiting for its persisted `NextDue` (V-13) |

### 12.6 Fixtures

* Decode fixtures of GitHub's list-commits response (null top-level `author`, missing `commit.author`, `committer` only, 409 empty repository, a 100-item page) belong to unit 05; `commits` consumes the normalised `scm.Commit`, and `TestNormalise` carries the equivalent rows (no date, no URL).
* `testdata/golden/badges/*.svg` (19 files), `tools/badgewidths/font/DejaVuSans.ttf` (SHA-256 `7da195a7...8954`).
* Day-grouping vectors (4.9) in `web/src/features/commits/groupByDay.test.ts` (**planned**, unit 12).

### 12.7 Acceptance criteria (unit is done when)

1. `go vet ./...`, `golangci-lint` (depguard import matrix, sloglint, forbidigo for `time.Now` outside edges) and `go test -race ./internal/commits/... ./internal/badges/...` pass.
2. `archtest`: `commits` and `badges` import only the packages of section 2.1; neither imports a feature.
3. Fuzz targets run 60 s each in CI without a finding; the seed corpus is committed.
4. Benchmarks stay within the gates of 7.2.
5. The e2e anonymous-enumeration and crawl tests pass; the API conformance test of unit 06 passes for `listCommits`.
6. Coverage: at least 80 % of statements in both packages (85.7 % and 81.8 % now).
7. The metrics registry test finds every instrument of 9.1 in the metrics reference.

### 12.8 CI benchmark gates

`BenchmarkList/*`, `BenchmarkCommitsPage/*`, `BenchmarkPublish`, `BenchmarkRender` run in the nightly job with `-count=5`; the gate is the median against the thresholds of 7.2 (about 5x the measured values on the reference machine) so that a regression in complexity (a scan on the hot path) fails the build while machine noise does not.

---

## 13. Implementation tasks (ordered, with milestones)

Cross-unit dependencies are named in the last column; a task may start when its dependencies' interfaces exist (they need not be complete).

### Milestone C-A: commits core (ingest, ring, store)

| # | Task | Files | Definition of done | Depends on |
|---|---|---|---|---|
| C0 | Re-run the scratch build against the current kernel copies (units 01/03/06/07) and fix drift (V-04 shows the drift is real: the first copies had no `KeyParts`, `CleanLine`, `CompareCommits`, `RequestFrom`, `WithDigest`) | `scm`, `freshness`, `job`, `routes`, `readmodel`, `store`, `httpapi`, `commitsapi` imports | `go vet` and the scratch tests green against the real tree; differences recorded in the PR | units 01, 03, 06 merged |
| C1 | `config.go`, `model.go`, `ports.go` | `internal/commits/{config,model,ports}.go` | types of section 3; `Config.Validate` table test; `commits.CommitLister` and `CommitStore` satisfied by the adapters in `cmd/scp/assert.go` (the structural assertions only; the three that name `reactor` and `commitsapi` live there too, V-03) | 01 |
| C2 | `normalise` and sanitisers (a thin wrapper over `scm.CleanLine`; no private copy of the cleaner, V-07) | `ingest.go` | `TestNormalise*`, `TestCleanLine`, `TestCleanLineUsesKernelCleaner`, `TestCleanURLAndLogin`, clamp carry-over | C1 |
| C3 | Ring codec, `Restore` (skips a repository without a held result quietly) and `EnqueueUnrestored` | `retention.go` | restore tests incl. orphans, incomplete, failing, error row without value; `TestEnqueueUnrestoredCoversSkippedRings` | C1, 03 (`store.Item`, `LoadCommits`) |
| C4 | Job `commits.refresh` (`run` with the push-marker bypass, `revalidate` with the `ErrNoItem` re-persist, `fail`, `publishEmpty`), `Jobs()`; the plan-kind registration in `cmd/scp` (`planKinds`/`knownKinds`, 5.2) | `jobs.go`, `ingest.go` | `TestRun*`, `TestPushBypassesTheHeadSweepSkip`, `TestRevalidationHealsAMissingStoreItem`, `TestJobSpecIsValidForTheEngine`, `-race` | 03 (`Publisher.Apply`, `ErrNoItem`), 04 (`job.Spec` registration, plan kinds), 05 (`LatestCommits` contract, Q-1) |
| C5 | `React` (folded job key through `RepoRef.KeyParts()`, push marker) | `react.go` | `TestReact`, `TestReactKeyIsFoldedAndRunWritesTheFoldedStoreKey`; registered by name `commits` in `reactor.NewNamed` | 04 (`reactor`), 05/08 (`EventPush`, `EventHeadChanged`, `Deleted`) |
| C6 | Metrics, spans, logs | `metrics.go` | the 9 instruments exist; registry test | 12 (`telemetry`) |

### Milestone C-B: commits read side

| # | Task | Files | DoD | Depends on |
|---|---|---|---|---|
| C7 | `Compare`, `Position`, `Merge` | `merge.go` | property test, fuzz, `BenchmarkCommitsPage` gate | 01 |
| C8 | `Slots`, `RegisterSlots`, `Projection` (projector + validator) | `project.go` | `TestProjectorViewClassesAndValidator`, `BenchmarkPublish` gate | 03 (`NewPublisher`), 08 (slot handles: `catalog.repo`, `stats`, `membership`, `aliases`) |
| C9 | `List`, `ListCommits` (the generated `StrictServerInterface`), `Mount`, `Recent` | `handler.go`, `service.go` | handler tests through the generated strict handler (`TestGeneratedHandlerProblems`); `TestRecentForOverview`; conformance test (`TestListCommitsConformance`, planned) | 06 (`httpapi`, `RequestFrom`/`WithRequest`, D-9 closed), 07 (generated `commitsapi`), 15 (`CommitsSource`, R-09-1) |
| C10 | `Coverage`, readiness wiring; call `EnqueueUnrestored` once after the warm start and engine start; `cmd/scp/assert.go` assertions | `jobs.go`, `cmd/scp` | `core_kinds: [commits]` gates readiness in the integration test; `TestRestartWithACorruptRingRefetchesAtOnce` (planned) | 12 (`ops`) |

### Milestone B-A: badges rendering

| # | Task | Files | DoD | Depends on |
|---|---|---|---|---|
| B0 | Re-run the scratch build against the real kernel copies | as C0 | green | 01, 03, 06 |
| B1 | `tools/badgewidths` + `widths.go`, `widths_gen.go`; move goldens. The generator belongs to the **single** nested module `tools/go.mod` of architecture 4.5 (package `tools/badgewidths`, no module of its own; it needs `golang.org/x/image` v0.47.0) | `tools/badgewidths`, `internal/badges/{widths,widths_gen}.go` | `TestWidthTableGolden`; CI drift check (`cd tools && go run ./badgewidths ...` then `git diff --exit-code`); font licence committed | 02 (nested `tools` module layout) |
| B2 | Palette, text sanitation, renderer | `palette.go`, `text.go`, `render.go` | `TestGoldenSVG`, `FuzzRenderWellFormed`, `TestPaletteContrast`, `BenchmarkRender` | none |
| B3 | Kinds, formats, `Evaluate` | `kinds.go`, `format.go`, `facts.go` | `TestEvaluateStates`, `TestFormatCountAndRelativeDay` | 14 (`Fact` shape), 13 (manifests facts) |

### Milestone B-B: badges serving

| # | Task | Files | DoD | Depends on |
|---|---|---|---|---|
| B4 | Handlers `Repo()`, `Group()` (slug folded, alias resolved), headers, ETag, metrics | `handler.go`, `config.go` | all `TestHandler*`, `TestETag*` | 01 (route names), 06 (`authz.LookupRepo`, `ClassOf`), 08 (slots) |
| B5 | `bridge.BadgeFacts` (not in this package) | `cmd/scp/internal/bridge` | table of 2.4 as a unit test; `RepoFacts.Build/Security` nil when provider disabled | 08, 13, 14, this unit's types |
| B6 | SPA: `<img alt>` from `Kind.Alt()`/`Badge.label`, `groupByDay`, load-more link to GitHub | `web/src/features/commits`, `web/src/features/badges` | day-grouping vectors; axe test on the commits page | 12 (frontend) |

### Milestone H: hardening

| # | Task | DoD |
|---|---|---|
| H1 | Integration tests of 12.5 | green in CI with `cmd/fakegithub` |
| H2 | Browser width check (Playwright nightly) | text box within the badge at 100 % and 200 % zoom in Chromium and Firefox |
| H3 | Runbook entries (alerts of 9.1) and the metrics reference rows | doc review |
| H4 | Camo check against a real README (manual, recorded in `docs/operations`) resolving Q-3 | note with date and result |

Critical path: C0 -> C1 -> C2/C3 -> C4 -> C8 -> C9 (needs 07) and B2 -> B4 (needs 01's route names and 08's slots). The two packages are independent until `bridge.BadgeFacts`.

---

## 14. Deviations proposed

Each row names what it changes, the reference it deviates from, and the evidence.

| ID | Deviation | From | Evidence and consequence |
|---|---|---|---|
| D-1 | A second store item per repository, kind `commits.ring`, holds the membership list of the ring (branch, head SHA, SHAs in order) | architecture 9.2/9.3 describe `commits.view` restored from `commit_log` alone | `store.AppendCommits` is insert-or-ignore plus trim (union semantics, `03-readmodel-store.md`, `AppendCommits`): after a force push the table holds commits that are not on the branch, so restoring "the newest 30 rows" would serve orphans. `TestRestoreIgnoresOrphansAfterForcePush`. Uses only the generic `item` table; item kinds are feature-owned (03 `ValidKind`), no store change |
| D-2 | `CommitLister` contract sentences: result is in branch order; empty repository (409) is an empty slice with a nil error; `Validators.SHA` of a fresh value is the observed tip; `ErrNotFound` for the repository implies `job.Gone` | signature identical to architecture 8.7; semantics added | GitHub does not document list order or when 409 occurs (REST docs "List commits", checked 2026-10-14: only that `per_page` max is 100, default 30, `sha` selects the start, statuses 200/400/404/409/422/429/500). **Unit 05's design (`05-github-adapter.md` 4.9) already provides all four**: 409 is an empty list and success, `Validators{SHA: commits[0].SHA}`, a conditional `NotModified` by SHA through the `vnd.github.sha` media type, `NotFound(Subject=Repository)` for a missing repository, `n` capped at 100, no e-mail. The sentences are therefore a recorded dependency, not a request |
| D-3 | Config keys `commits.refresh_ttl` (default 2h) and `commits.idle_backoff_max` (default 12h) | architecture 16 shows only `commits: {keep, latest_n}` | architecture 9.6 says every policy value is a config key with a consumer; values equal the 9.6 table |
| D-4 | Cursor and merge order is `(committedAt desc, slug asc, sha desc)`; the cursor holds `{t, r, s, f}` | architecture 9.5: heap keyed by `(committedAt, sha)`, cursor holding `(committedAt, sha)`; 01 SC-08 (`CompareCommits`, ties by SHA descending, within one repository) | the same SHA may exist in two repositories (forks, mirrors; GR-07), so `(committedAt, sha)` is not a total order and a cursor on such a SHA skips or repeats its twin. `TestMergeEqualsNaiveSortPaged` includes identical SHAs in two repositories. The filter fingerprint `f` follows 06-httpapi T12 |
| D-5 | `commits.Latest` has `AllInfo` and `GroupInfo` in addition to `All` and `ByGroup`; feeds hold `LatestN + 1` refs; `Slots`, `RegisterSlots`, `Projection` exist so the projector can be built before the service | architecture 9.1 `Latest{ByGroup, All}`; "top N" | the freshness block needs the status of the repositories behind each feed without scanning (`FeedInfo`); the sentinel makes the default page (`limit` 50 = `latest_n` 50) a fast-path page; `readmodel.NewPublisher(reg, projectors, validators, deps)` takes projectors at construction while the service needs the publisher (compile-checked) |
| D-6 | `badges.RepoFacts` gains `Build, Security *Fact` with `Fact{Message string; Tone Tone; Stale bool}`; `Tone` is a badges-owned enum | architecture 8.7 `RepoFacts` | requested by `14-integrations-access.md` section 4.6 (rows `badges` and D-6 there); nil means the provider is disabled and the kind is a 404 |
| D-7 | Badge status semantics: unknown repository, hidden repository, unknown or disabled kind: `404` with a grey body; known repository without data: `200` grey `unknown`/`none` with `max-age=60` | architecture 11.2 does not specify | a `200` placeholder for everything would hide broken embeds and blur "does not exist" with "no data yet" (GR-16); the body is still an image, so embeds degrade gracefully; monitoring can count `state="not_found"`. ADR-011 requires an "unknown" state, not the status code |
| D-8 | File names beyond the architecture tree: `commits/{model,ports,metrics}.go`, `badges/{config,palette,text,format,facts}.go`, `tools/badgewidths/` (a package of the single nested `tools` module, not a module of its own) with the committed DejaVu Sans 2.37 font (757 KB, Bitstream Vera licence; archive SHA-256 verified against the release by the verifier) and licence text | architecture section 7 tree | package boundaries unchanged; the font is committed so the generator is reproducible offline; its SHA-256 is verified by the generator. Alternative (download at generation time) is Q-5 |
| D-9 (closed) | Request that `httpapi` offer `RequestFrom(ctx) *http.Request` (set by a strict middleware) | `06-httpapi.md` section 4.4 recipe writes `NotModified(r, etag)` in a strict handler | **Answered by `06-httpapi.md` D29**: `httpapi.WithRequest` and `httpapi.RequestFrom` exist and the prototype uses them (`ListCommits` fails closed with 500 without `WithRequest`, `TestGeneratedHandlerProblems`). Kept for the record only |
| D-11 | Strong, content-hash ETag for badges (not the snapshot generation) | architecture 11.2: "ETag" convention is the weak snapshot-variant form for `/api/` | badge text depends on the clock (relative dates), so a generation ETag could confirm a stale text; badges are not API routes. `TestETagFollowsBodyAcrossMidnight` |
| D-12 | `commits` imports `httpapi/openapi` (rank 0, generated types only, no dependencies) and `httpapi/authz` (already in K4) | architecture 5.2 row `commits`: K4 and `commits/commitsapi` (the matrix of 10-architecture 5.2 does not list `httpapi/openapi` for rank-4 features; the prototype's `go list` shows the import and the test files obey the same row) | the shared schema `Commit` (also used by the repository composite's `latestCommits`) lives in `common.yaml`; building an item needs the type name `openapi.Commit`, which `commitsapi` does not re-export (oapi-codegen references it through `import-mapping`). The scratch build imports it; `commits.APICommit(slug, commit) openapi.Commit` is the one mapping, exported so `overview` (rank 5) reuses it. Alternative: aliases in `httpapi` (`type Commit = openapi.Commit`) |

---

## 15. Open questions

| # | Question | Needed from | Default if unanswered |
|---|---|---|---|
| Q-1 | Is `NotFound(Subject=Repository)` from `LatestCommits` classified `job.Gone` by the engine (04) so that the key is dropped (the rest of D-2 is confirmed by `05-github-adapter.md` 4.9)? Unit 05 also says the planner calls the commits job only when the head changed (`ProbeHeads`); this design makes the same decision inside the job (head-sweep skip), which is idempotent with it | units 04, 05 | the job treats any error wrapping `scm.ErrNotFound` as gone; both mechanisms may run, the second costs nothing |
| Q-2 (closed) | How does a strict handler obtain the `*http.Request` (D-9)? | unit 06 | **Closed**: `httpapi.WithRequest` and `RequestFrom` (06 D29); the prototype compiles and tests against them |
| Q-3 | Does GitHub's Camo proxy follow redirects, honour `max-age` below its own floor, and cache `404` bodies? (unverified; research 06 item 7) | task H4, manual check with a real README | direct serving (already designed), `max-age=300`, `404` `max-age=60` |
| Q-4 | Are `site.display_time_zone` and the viewer's preference both inputs of `groupByDay`? Which wins? | unit 12 (frontend) | viewer's browser zone, falling back to `display_time_zone`, label shown once in the page header |
| Q-5 | Commit the 757 KB DejaVu font, or download it at generation time from a pinned URL with the checksum? (The archive hash of `dejavu-fonts-ttf-2.37.tar.bz2` and the SHA-256 of `DejaVuSans.ttf` are now both known and verified, V-16, so the pinned download is possible too) | maintainers | commit (offline, reproducible); revisit if repository size matters |
| Q-6 | Should `commits.keep` above 30 raise memory by a visible warning at startup (100 x 1,000 repositories is about 45 MB of ring data)? | maintainers | no warning; the memory budget table in the operations doc lists it |
| Q-7 | Do we want a `last-commit` colour policy configurable by the operator (90/365 day thresholds)? | product | fixed thresholds (success up to 90 days, info up to 365, warning beyond) |
| Q-9 | Is a per-author or per-date-range filter wanted later (architecture 9.5 removed `author`)? | product | not in v1; additive later with an index projector |
| Q-10 | `freshness.lastErrorCode` examples in `common.yaml` are dotted (`github.rate_limited`); this unit emits the closed set `permanent`, `not_found`, `forbidden`, `unavailable`, `error`. Should the adapter-level codes be mapped to the dotted vocabulary by the engine or by each feature? | unit 07 / 04 | closed set above (documented in the API description) |
| Q-11 | Architecture 17.4 gives `/badges` its own class (120/min, burst 60, per client) and this unit relies on it (section 6, 10). Residual point for unit 06 / `httpx`: Camo and corporate gateways fetch all badges of a README from one address, so 60 burst is about three READMEs of 20 badges; is per-client bucketing by peer address enough, or do these proxies need a larger class? (A first draft of this verification proposed a new class `badges`; the architecture already defines it, so no deviation is recorded.) | unit 06 / 12 | 120/min burst 60 unchanged; revisit with the `scp_ratelimit_rejected_total{class="badges"}` metric after launch |
| Q-12 | Slug case: the kernel's `scm.ParseSlug` is canonical lower case while the catalogue may hold an owner or repository name with upper case (06 Q12). This unit folds every input and every job key itself (V-01, V-10), so it is correct either way | units 01 / 08 | fold in this unit (done); the catalogue unit decides whether `catalog.repo` keys are folded |
| Q-13 | Does the planner of 04 see `fetched_at` (04: "knows `NextDue` and the streak, never `fetched_at`") or not (03: "the planner sees no `fetched_at`")? The two documents disagree about who can make a restored repository due, which is why `EnqueueUnrestored` exists | units 03 / 04 | `EnqueueUnrestored` after the warm start (4.7); harmless if the planner also does it, keys coalesce |

---

## Verification log

Verifier: independent (not the author). Date: 2026-10-14. Document and prototype were changed in place; fixes carry V-nn markers in the text. The original is kept outside the repository for diffing. Report: `30-verification/design-09-commits-badges.md`.

### Method

* The prototype under `09-commits-badges.assets/` was rebuilt against the **current** sibling assets (01, 03, 06, 07), `go vet ./...` clean, `go test -race -count=1 ./...` green (51 top-level tests, 60 with subtests; coverage 85.4 % `commits`, 81.7 % `badges`).
* `go list` for both packages (imports and test imports) was compared with the matrix row: `commits` imports `commitsapi`, `freshness`, `httpapi`, `httpapi/authz`, `httpapi/openapi`, `job`, `readmodel`, `scm`, `store`; `badges` imports `freshness`, `httpapi`, `httpapi/authz`, `readmodel`, `scm`. No `reactor`, no other feature, in production or test files.
* Both fuzz targets re-run on the final code (10 s each, 905,956 and 1,153,824 executions, no finding); the width generator reproduces `widths_gen.go` byte for byte.
* Each fix marked "mutation-checked" was verified by reverting it with a script and observing the named test fail (`TestReactKeyIsFoldedAndRunWritesTheFoldedStoreKey`, `TestPushBypassesTheHeadSweepSkip`, `TestRevalidationHealsAMissingStoreItem`, `TestRestoreSkipsAnErrorRowWithoutValueQuietly`, `TestRingOrderIsTheKernelOrder`, `TestCleanLineUsesKernelCleaner`).

### Findings

| ID | Sev | Issue | Fixed |
|---|---|---|---|
| V-01 | high | Job key built from `r.Owner, r.Name` instead of `RepoRef.KeyParts()`: a mixed-case catalogue entry and a lower-case event enqueue two keys for one repository, and `store.KeyOf` folds so both address the same rows | yes (4.1, `React`, `run`) |
| V-02 | high | The head-sweep skip (a fresh sweep value equal to the ring's head means "nothing to do") also swallowed webhook pushes, so a push inside the sweep window was not refetched, against architecture 9.6 ("`push` (seconds)") | yes: push marker (`sync.Map` plus counter), consumed by a successful fetch with compare-and-delete; 4.1, 8 |
| V-03 | medium | `assert_test.go` imported `reactor`, outside the `commits` row (rule 2 of 5.1); test files obey the row | yes: file removed; assertions moved to `cmd/scp/assert.go` (2.1, 2.6) |
| V-04 | medium | The author's scratch copies of kernel code predated the siblings' own verification (no `KeyParts`, `CleanLine`, `CompareCommits`, `WithDigest`, `RequestFrom`; `store.KeyOf` not folding) | yes: rebuilt on the current assets; header, C0/B0 |
| V-05 | medium | ETag put the freshness status in as a third part (`W/"g1842-p-fresh"`), not the convention of 06 D15 (`WithDigest(WeakETag, FreshnessDigest)`) | yes (7.2, tests) |
| V-06 | medium | The handler was an adapter around `List(r, ...)`; unit 06 D29 provides `WithRequest`/`RequestFrom`, so D-9/Q-2 are closed and the feature implements the generated `StrictServerInterface` itself, compile-checked, tested through the generated handler (404, 400, 304, fail-closed 500) | yes (7.1, D-9, Q-2) |
| V-07 | medium | Private `cleanLine` instead of the kernel's `scm.CleanLine` (SC-09: one implementation): the private one missed the tag block, soft hyphen and U+2061..2064 | yes (4.2) |
| V-08 | medium | Ring order tie-break differed from `scm.CompareCommits` (SHA descending) and the merge order was not a total order across repositories with an equal SHA | yes (3.4, D-4) |
| V-09 | medium | A lost store write was never healed: `Revalidated` returning `ErrNoItem` made every later "unchanged" confirmation fail forever | yes: `repersist` (4.1) |
| V-10 | medium | `?repo=` and badge path slugs compared case-sensitively against folded slugs; `/badges/API/issues.svg` was a 404 | yes (4.10, 4.13) |
| V-12 | medium | `New` let a nil logger, meter, store or authoriser through; the first failed store write or request then panicked | yes: validation of every dependency, both packages (3.8) |
| V-13 | medium | A restored repository without a ring (skipped, corrupt, or an `error` row that never held a value) had no view and nothing made its job due; the claim that it "is due immediately" held only without an item row; the error row also incremented `restore_total` | yes: `Restore` skips quietly; `EnqueueUnrestored` (4.7) |
| V-14 | low | A second `go.mod` under `tools/badgewidths`; architecture 4.5 allows exactly one nested `tools` module | yes (1.4, B1) |
| V-15 | low | Font called "2.37.3" (a version that does not exist); the claim that the badge widths match Shields' was not backed by a comparison | yes: 2.37; live comparison with Shields' output recorded (4.12) |
| V-16 | low | Provenance of the font incomplete: archive hash unknown | yes: archive and file SHA-256 recorded and verified (4.12, Q-5) |
| V-17 | medium | Overview needs the read side `Recent` (unit 15 R-09-1) and the post-restore `EnqueueUnrestored`; neither existed; 2.5 listed neither | yes (2.5, tests) |
| V-18 | medium | Plan-kind rule (03 2.5): the planner persists `NextDue` on the item row by item kind while the engine speaks job kinds; the unit stored `commits.ring` for job `commits.refresh` and did not state the wiring | yes: `planKinds`/`knownKinds` in 2.6, 5.2 |
| V-19 | medium | Trace table lacked capabilities and guardrails that this unit implements or consumes (dashboard panel, group page, branch configuration, release data, push webhook, visibility policy, count limits; G-04, G-08, G-12, G-14; GR-12, GR-15, GR-18, GR-21, GR-23, GR-25, GR-34, GR-40) | yes (11.1, 11.2) |
| V-20 | medium | The test fake store accepted anything (mixed-case keys, empty payloads, any item state); the tests could not fail on a store-contract violation; one restore test passed with its fix removed | yes: fake enforces the contract; counting meter; 12.2 |
| V-21 | low | Section 6 said the 30 was "hard-coded in the fetch task"; the task passes no `per_page`, so 30 is GitHub's default page size | yes (6) |
| V-22 | low | Badge 404 bodies are shareable by caches that serve them to a later authorised requester for 60 s (hidden == missing is kept, but a 404 cached by a shared proxy shadows a repository that becomes visible) | accepted: `max-age=60`, section 10 row |
| V-23 | medium | 03 says "the planner sees no `fetched_at`", 04 says it "knows `NextDue` and the streak, never `fetched_at`"; the two disagree about who makes a restored repository due | no: cross-unit; Q-13; mitigated by `EnqueueUnrestored` |
| V-24 | low | Slug case: the kernel's `ParseSlug` is lower case, the catalogue may keep case (06 Q12) | worked around in this unit (fold everywhere); Q-12 |

### External facts checked

* GitHub REST "List commits": `per_page` default 30, maximum 100; `sha` selects the start; statuses 200/400/404/409/422/429/500 (docs.github.com, 2026-10-14).
* DejaVu fonts release `version_2_37`: archive SHA-256 `fa9ca4d1...e17d7`, `ttf/DejaVuSans.ttf` SHA-256 `7da195a7...48954`, `name` table `Version 2.37`; licence file committed.
* Shields.io badge geometry (height 20, 11 px text scaled by 10, padding) compared with the Go renderer's widths; Verdana versus DejaVu width difference measured and recorded in 4.12 (DejaVu is the substitute; its widths are measured by an independent stdlib TTF reader `ttfwidth.py` and equal the generator's table).
* `oapi-codegen` v2.8.0 strict server output (the `commitsapi` copy compiles and is exercised); `golang.org/x/image` v0.47.0 resolves on the module proxy and builds the generator.
* WCAG 2.x contrast of white text on the closed palette recomputed independently by `contrast.py` (Python, threshold 0.03928) and by `TestPaletteContrast` (Go): the six AAA colours score 7.0 to 9.74, the six AA colours 5.02 to 6.44 (minimum 4.5 required).

### Unresolved

1. V-23 / Q-13: units 03 and 04 disagree on what the planner knows; this unit's `EnqueueUnrestored` makes the outcome independent of the answer.
2. Q-11: rate-limit class for shared proxies (unit 06 / `httpx`).
3. V-24 / Q-12: catalogue slug case (06 Q12).
4. Q-3: Camo behaviour (redirects, `max-age` floor, 404 caching) is unverified; direct serving is the design default.
5. Planned tests (`TestListCommitsConformance`, the Playwright width check, the integration tests of 12.5) need the engine, the real adapter, the SPA and `cmd/fakegithub`; they cannot run until those units exist.

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `errCode` follows the closed vocabulary (Gone, Permanent, RateLimited precedence; default `transient`) (CD-ER-01); logx path (CD-IF-06); `scp_item_age_seconds` belongs to `ops` (CD-MT-02).
- Requirement R-09-1: commits and badges must expose the read accessors that `overview` consumes (badge kind list, latest commit per repo); `overview` currently names them as bridged (15 R-09-1).
- Q-13 is closed: the planner sees no `fetched_at`; `EnqueueUnrestored` is correct (CD-JB).
- The unit's `common.yaml` copy is a stale snapshot; use `api/common.yaml`.
