# 08 - Design: `catalog`

Status: design for implementation. Date: 2026-10-14. Unit: 08-catalog. Package: `internal/catalog` (rank 4).
Binding inputs, in precedence order: `04-decisions.md`, `10-architecture.md` (revision 2), `11-adr/` (ADR-023 above all). Where this document needs
something the architecture does not say it is listed in section 14 (Deviations) and never silently assumed.

Reference prototype: `08-catalog.assets/` next to this file (`mod/` is a compile-checked, race-tested Go module with the algorithms, the types, the
codec, 50 test functions, three fuzz targets and two benchmarks; `fixtures/` and `out/` are the example inputs and the lint output quoted in 4.10). The prototype is throw-away: the
implementation copies `mod/internal/catalog/**` into the real module. Since the independent verification of 2026-10-14 (section 16) the module carries the **real**
kernel, readmodel and store packages of units 01 and 03 (no stubs), so everything quoted as Go in this document compiled against the real kernel with go1.27.1 (section 12.9).
Where section 16 corrected this document, the corrected text is in place and the finding id (V-nn) is named next to it.

## 0. How to read this document

| If you are | Read |
|---|---|
| implementing the package | 2 (boundaries), 3 (types), 4 (rules and pseudocode), 5 (data), 13 (task order) |
| writing `cmd/scp` wiring | 2.4 (what `cmd/scp` must do), 6 (config), 8.1 (startup order) |
| writing `overview`, `commits`, `docs`, `manifests`, `badges`, `webhook`, `ops` | 2.3 (what you may rely on), 3.4 (published values), 7 (API) |
| reviewing security | 4.3, 4.8, 10 |
| the owner confirming decisions | 14 (deviations), 15 (open questions) |
| checking what the independent verification changed | 16 (verification log, V-01 to V-21) |

Notation. "R-step" is a step of the reconcile algorithm (4.4). Finding codes: `Cnnn` config error, `Wnnn` config warning, `Rnnn` lint or runtime
finding. All of them are stable identifiers: tests, docs and `/status` compare codes, never message text.

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

The catalogue answers three questions for the rest of the portal and for nothing else:

1. **Which repositories exist and are allowed to be shown?** (discovery, visibility policy, exclusion rules, rename and removal handling)
2. **Which groups does each repository belong to, and which repository represents a group?** (typed matchers, many-to-many membership, default repo)
3. **Did a repository's HEAD or headline statistics change?** (the head/stats sweep that accelerates every other feature)

It owns the product's identity layer: `scm.Repository` values (the existence slot), `scm.RepoStats` values, `scm.Membership`, the slug and its aliases,
and the configuration schema of groups. It is the only package that decides that a repository leaves the portal.

### 1.2 Scope (in)

| Area | Content | Decisions / guardrails |
|---|---|---|
| Group configuration | `catalog:` YAML block, typed matchers (exact, prefix, glob, regexp, topic, catch_all), strict decoding contract, JSON Schema, validation rules `C*`/`W*` | Q-07, D-07, GR-09, GR-12 |
| Repository discovery | paging through `RepositoryLister`, normalisation, policy admission, diff, rename/transfer detection, quarantine | GR-26 |
| Reconcile safety | complete-listing rule, `removal_confirmations`, mass-removal guard with hold, immediate visibility removal, 304 page reuse | GR-25, GR-26 |
| Visibility policy | `public_only` / `installation_scope`, GHES/GHEC-DR acknowledgement, fail-closed admission | Q-03, ADR-023, GR-35 |
| Slugs | derivation, prefixes, reserved words, collisions, aliases, group aliases | ADR-023, W17, GR-07 |
| Membership | immutable `scm.Membership` index, default repository resolution, tracked-ref overrides | GR-07, GR-08, GR-10 |
| Head/stats sweep | `ProbeHeads` batches, `scm.RepoStats` (licence, latest release, HEAD), synthetic `EventHeadChanged`, per-repository `catalog.stats` job | GR-21, GR-25 |
| Events | `React` for `repository`, `installation`, `installation_repositories`, `release`, `create`, unknown-repo `push` | GR-21 |
| Persistence | catalogue rows and `catalog.stats` items through `CatalogueStore`; warm start | GR-31, 03 section 2 |
| Lint | offline `Config.Validate`, online `Lint` against a repository list | GR-12 |
| API | `GET /api/v1/groups`, `GET /api/v1/groups/{groupId}` and the `catalogapi` contract | GR-15, GR-16 |

### 1.3 Non-goals

- Fetching anything from GitHub itself: the HTTP client, auth, ETags on the wire, rate-limit state, GraphQL queries belong to `github` (ADR-004). `catalog` sees two consumer interfaces.
- READMEs, docs, commits, manifests, badges, integrations: they read the catalogue's published values and react to its events; none of them is imported here.
- Per-repository descriptor metadata (`scp.yaml`: title, lifecycle, owners, tags, hidden): `docs` owns it; `catalog` has no opinion about descriptor fields. A repository hidden by its descriptor is still in the catalogue.
- The repository list endpoint `GET /api/v1/repos`, the group repository table `GET /api/v1/groups/{groupId}/repos` and the repository composite: `overview` owns them in the public OpenAPI (section 7.1 records this against the architecture table).
- Authentication and the principal: `access` and `httpapi/authz` own them; `catalog` only applies `Authorizer.CanView` through `ClassOf`.
- A second source provider. `Source` is a GitHub account, an App installation set or a GHES host (D-17); nothing in this package generalises further.
- Scheduling and retry mechanics: the engine and planner do that (ADR-003). `catalog` supplies three `job.Spec` values and returns typed errors.
- Persisting the mass-removal hold or the confirmation counters beyond what a catalogue row carries (4.4 explains why a restart restarting the hold is the safe direction).
- Config file loading, layering, env mapping, schema generation machinery: `platform/config` and `cmd/scp` (ADR-012). `catalog` owns key definitions, defaults, validation and a JSON Schema document.

### 1.4 Packages and files

```
internal/catalog/                 package catalog, rank 4
  config.go        Config, HeadsConfig, GroupConfig, GroupIntegrations, DefaultConfig, ValidateContext, Config.Validate (C*, W*)
  matcher.go       Matcher, MatcherKind, CompiledMatcher, compileGlob, compileAnchored, MatcherError
  slug.go          MakeSlug (canonical lower-case, kernel scm.ParseSlug is the final check), FoldSlug, ValidGroupID, SlugError, reserved words and suffixes
  finding.go       Finding, Severity, SortFindings (numeric-aware paths), HasErrors
  normalise.go     normaliseListed, cleanText, cleanURL: validation and cleaning of everything the listing returns
  visibility.go    VisibilityPolicy, HostClass, DiscoveryPolicy, PolicyContext, Exclusion, Admit, DiscoveryPolicy.Validate (C080-C086)
  plan.go          PlanInput, Plan, Tracked, Held, Quarantine, Removal, Rename, PlanReconcile (pure; R-steps 1-7)
  membership.go    Compiled, Compile, BuildMembership, GroupHealth, MembershipResult
  lint.go          Lint (R201-R209)
  codec.go         EncodeRow/DecodeRow (store.Repo.Meta schema 1), encodeStats/decodeStats (catalog.stats payload schema 1)
  interfaces.go    consumer interfaces, slot and job names, Slots, RegisterSlots, Status, SourceStatus
  service.go       Deps, SourceDeps, Service, New, Jobs, Mount (the file the architecture calls jobs.go + service.go + handler.go in part)
  reconcile.go     runReconcile, listSource, commit (R-step 8), publish, buildAliases, statusFor, recordGauges
  heads.go         HeadChange, DiffHeads, HeadEvent, runHeads, runStats, applyStats
  react.go         React, unknownRepo, hide
  warm.go          Warm, WarmInfo, Slots.Validator, Slots.Coverage
  groups.go        Slots.Groups, Slots.Group, GroupCard: the class-aware read model of the two routes (V-12); the handlers only map it to catalogapi types
  metrics.go       instrument names and newMetrics
  schema.go        //go:embed catalog.schema.json -> Schema
  catalog.schema.json
  catalogapi/      catalog.yaml (fragment of the public OpenAPI), generated stubs (apispec unit)
```

The architecture's file list (`config.go matcher.go reconcile.go heads.go membership.go aliases.go visibility.go jobs.go react.go service.go handler.go`)
is kept in spirit; the prototype splits the pure planning code (`plan.go`, `slug.go`, `lint.go`, `finding.go`) from the effectful code and folds `aliases.go` into
`reconcile.go`. This is a file-level choice only, recorded as deviation D-12 because the architecture tree is binding at package level.

## 2. Position in the architecture

### 2.1 Rank and allowed imports

`catalog` is a feature at rank 4 (10-architecture 5.1/5.2). It imports only:

| Allowed | Used for |
|---|---|
| `scm`, `freshness`, `job`, `routes` | kernel types; job specs and typed errors; `routes` only by the handler (named route URLs for `uiUrl`) |
| `readmodel` | slots, globals, `Publisher.Apply`, validators |
| `store` | the `Repo`, `Item`, `RepoKey`, `ItemKey` value types and `ErrNoRepo` (the store interface itself is the consumer-defined `CatalogueStore`) |
| `httpapi`, `httpapi/authz` | `Mounter`, `Mux`, `WriteProblem`, `Principal`, `ClassOf`, `LookupRepo` |
| `platform/secret`, `platform/logx` | not used by the prototype (no secrets in this package); `logx` for the request id in handlers |
| `catalog/catalogapi` | generated request/response types of its own routes |
| OTel API (`otel/metric`, `otel/trace`) | injected `Meter` and `Tracer` (ADR-013 amendment) |
| third party | **none**: no YAML library, no JSON Schema library, no `regexp` replacement. `regexp`, `regexp/syntax`, `encoding/json`, `crypto/sha256` are stdlib |

It never imports `github`, `refresh`, `reactor`, `store/sqlite`, `overview`, another feature or `platform/{config,telemetry,httpx,netguard,lifecycle}`.
The Matcher's polymorphic YAML decoding is therefore written with the yaml.v3 *obsolete* unmarshaler signature (`UnmarshalYAML(unmarshal func(any) error) error`), which
needs no import (verified in 12.8 and in the Go module source: `obsoleteUnmarshaler` in `go.yaml.in/yaml/v3@v3.0.5/yaml.go:39`).

### 2.2 Consumer-defined interfaces (exact signatures)

Declared in `internal/catalog/interfaces.go`. "Direct" rows use stdlib and kernel types only and are asserted in `cmd/scp/assert.go`.

```go
// Direct: satisfied by *github.Client. One call returns one page; Next == "" ends the listing; a NotModified result carries no Value.
type RepositoryLister interface {
    ListRepositories(ctx context.Context, src scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error)
}

// Direct: satisfied by *github.Client (GraphQL bulk or REST fallback; the caller cannot tell which).
// A repository missing from the returned map was not found or not visible to the credential; the caller never treats that as removal.
type HeadProber interface {
    ProbeHeads(ctx context.Context, repos []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error)
}

// Bridged (cmd/scp/internal/bridge): adapts refresh.Planner.SetSubjects for every per-repository job kind, including catalog.stats.
// Receives the COMPLETE tracked set after every committed change. After Warm, cmd/scp passes Service.Repos() itself: the planner does not exist yet at startup step 5.
type Tracker interface{ SetRepos(repos []scm.RepoRef) } // must return quickly and must NOT call back into catalog: it runs under pubMu (8.2)

// Direct: the wired *reactor.Fan. catalog emits scm.EventHeadChanged into it.
type EventSink interface{ Handle(ctx context.Context, ev scm.Event) error }

// Direct: carved out of store.Store; *sqlite.Store and store/memory satisfy it.
type CatalogueStore interface {
    LoadCatalogue(ctx context.Context) ([]store.Repo, error)
    LoadItems(ctx context.Context, fn func(store.Item) error) error
    ReplaceCatalogue(ctx context.Context, repos []store.Repo) (removed []store.RepoKey, err error)
    PutItem(ctx context.Context, it store.Item) error
    Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
}

// Direct: *readmodel.Publisher. A handler calls Current exactly once per request (6.1 rule 2).
type Reader interface{ Current() *readmodel.Snapshot }
```

Notes that fix questions the architecture left open:

- `RepositoryLister.ListRepositories` takes an **opaque cursor**: the adapter encodes GitHub's `Link: rel="next"` URL (GitHub: clients "should not try to manually construct pagination queries", verified 2026-10-14). `catalog` never builds a page number and never interprets a cursor; the first call passes `""`.
- A `NotModified` result for a cursor `catalog` holds a page for means "reuse that page". A `NotModified` for a cursor it holds nothing for is answered by exactly one unconditional retry (R-step 0, 4.4), because an adapter that answers 304 to a validator `catalog` never sent is a bug that must not empty the portal.
- `ProbeHeads` is called with at most `catalog.heads.batch_size` (1..100, default 50) repositories. `HeadProber` must return `RepoStats` with `HeadSHA`, `HeadTreeOID`, `OpenIssues`, `Stars`, `License`, `Release`, `PushedAt`; `catalog` stores them verbatim.
- `Tracker` is called **outside every `catalog` lock**, after the snapshot publish, so a bridge that calls into the planner cannot deadlock against `catalog`. `Warm` does not call it (the planner is built after the warm load); `cmd/scp` calls `Service.Repos() []scm.RepoRef` once the planner exists.

### 2.3 What other units may rely on (the published surface)

Everything other features read arrives through the snapshot (mechanism A). They hold `readmodel` handles of kernel types and never import `catalog`; `cmd/scp` hands them the handles from `catalog.RegisterSlots`.

| Name | Kind | Value type | Reader |
|---|---|---|---|
| `catalog.repo` | per-repository slot, **the existence slot** | `freshness.Value[scm.Repository]` | everyone (`LookupRepo`, projectors, validators) |
| `catalog.stats` | per-repository slot | `freshness.Value[scm.RepoStats]` | `badges`, `overview`, `commits` (HEAD sha), `manifests` (tree oid) |
| `catalog.membership` | global | `scm.Membership` (immutable) | `commits` projector, `overview`, `manifests` rollups, `badges` group counts |
| `catalog.aliases` | global | `map[scm.Slug]scm.Slug` old slug to current slug (all slugs are canonical lower case; V-01) | `overview` (308 redirect) |
| `catalog.group_aliases` | global | `map[scm.GroupID]scm.GroupID` lower-case earlier id to current id | `overview` (308 redirect of `/groups/{groupId}`) |
| `catalog.sync` | global | `catalog.Status` (**admin class**) | `ops` (`/status`, readiness gates), `overview` for `LastReconcile` only |

Guarantees a reader may rely on, each pinned by a test (12):

1. Every slug in any `ReposOf(g)` has a `catalog.repo` value in the **same** snapshot (validator `Slots.Validator`, 4.6).
2. `DefaultRepo(g)` returns `ok == true` only for a member of `g`; a group whose configured default is missing, excluded or not selected has `ok == false` and the reason is in `catalog.sync`; the package never substitutes another repository.
3. `catalog.repo`'s `scm.Repository.Slug` is the canonical lower-case slug of the kernel (`scm.ParseSlug`, at most 100 bytes) and `Ref.Owner`/`Ref.Name` keep GitHub's display case (V-01). `catalog.repo`'s `scm.Repository.DefaultBranch` is the **effective tracked ref**: the group `ref` override when one applies (first group in config order wins), else the repository's GitHub default branch. This is deviation D-1 (3.4): features need exactly one answer to "which branch do I read", and the architecture's Q-08 rule makes that a catalogue decision.
4. `Membership.Groups()` and each `ReposOf` list are in a deterministic order: groups in configuration order, members by slug. `GroupsOf(slug)` is in configuration order.
5. A repository that became private, internal or was deleted/transferred/renamed away is **absent from the snapshot within one `Apply`** of the event that announced it (4.8), without waiting for a listing.
6. A value in `catalog.repo` is never mutated after `Put` (ownership transferred; slices cloned at the boundary).
7. After `Warm`, `catalog.sync.LastReconcile` is the zero time: a warm snapshot never claims a live listing.
8. `catalog.repo`'s `Info.FetchedAt` is the time of the last complete listing that contained the repository (refreshed by every listing, V-05), so `Derive` says `fresh` for an unchanged repository for as long as listings succeed, and `stale` once they stop for more than 1.5 x the reconcile TTL.
9. A restart never serves a repository that the **current** visibility policy excludes: `Warm` re-applies `Admit` and skips a row excluded for visibility (V-20).

`catalog.Status` is admin-class data: it counts repositories the policy excluded. It is never serialised into a public response (10.3).

Exported entry points other units call (the full signatures are in 3 and 4):

| Caller | Calls |
|---|---|
| `cmd/scp` | `catalog.New`, `(*Service).Jobs`, `Warm`, `React`, `Mount`, `RegisterSlots`, `Slots.Validator`, `Slots.Coverage`, `Config.Validate`, `DiscoveryPolicy.Validate`, `Lint`, `catalog.Schema`, `DefaultConfig`, `DefaultDiscoveryPolicy` |
| `cmd/scp config lint` | `Config.Validate`, `DiscoveryPolicy.Validate`, `Lint` |
| `cmd/scp config schema` | `catalog.Schema` |
| `ops` through a bridge | `readmodel.GetGlobal(s, slots.Sync)` |

### 2.4 What `cmd/scp` must do (wiring contract)

```go
// cmd/scp/wire_catalog.go (sketch; compile-checked shape in the prototype tests)
slots := catalog.RegisterSlots(reg)                                  // before reg.Freeze()
validators = append(validators, slots.Validator())
for i, s := range cfg.GitHub.Sources {                               // discovery keys are catalog's, inlined into github's source config
    fs = append(fs, s.Discovery.Validate(fmt.Sprintf("/github/sources/%d/discovery", i),
        catalog.PolicyContext{Host: hostClass(s.Host), AccessEnabled: cfg.Access.Mode != "public"})...)
}
fs = append(fs, cfg.Catalog.Validate(catalog.ValidateContext{Sources: sourceIDs})...)   // refuse to start on catalog.HasErrors(fs)
svc, err := catalog.New(catalog.Deps{Config: cfg.Catalog, Sources: sourceDeps, Lister: ghClient, Prober: ghClient,
    Store: st, Publisher: pub, Slots: slots, Tracker: bridge.NewTracker(planner, kinds), Events: fan,
    Log: log, Meter: meter, Tracer: tracer, Seed: seed})
for _, sp := range svc.Jobs()   { eng.Register(sp); planner.Track(sp) }
reactors = append(reactors, svc.React)                              // reactor.Func by method value
fn, info, err := svc.Warm(ctx); _, err = pub.Apply(fn)              // startup step 5, builds snapshot version 1
trackerBridge.SetRepos(svc.Repos())                                  // step 6, once the planner exists: the warm subjects for planner.Restore
svc.Mount(mux)                                                       // httpapi.Mounter
eng.Enqueue(job.NewKey(catalog.JobReconcile, "_"), job.PrioWebhook, time.Time{}, false)   // the first live listing
```

Compile-time assertions added to `cmd/scp/assert.go`:

```go
var (
    _ catalog.RepositoryLister = (*github.Client)(nil)
    _ catalog.HeadProber       = (*github.Client)(nil)
    _ catalog.EventSink        = (*reactor.Fan)(nil)
    _ catalog.CatalogueStore   = (*sqlite.Store)(nil)
    _ catalog.Reader           = (*readmodel.Publisher)(nil)
    _ httpapi.Mounter          = (*catalog.Service)(nil)
    _ reactor.Func             = (*catalog.Service)(nil).React   // method value assignability
)
```

`catalog.Tracker` is bridged (`bridge.Tracker`), asserted in `cmd/scp/internal/bridge`.

## 3. Domain model and Go types

All types below are in the prototype and compile. Names and shapes are binding for the implementation; bodies are in `08-catalog.assets/mod/internal/catalog/`.

### 3.1 Identity

| Concept | Type | Rules |
|---|---|---|
| Source | `scm.SourceID` | a configured GitHub account/installation set/host (`github.sources[].id`) |
| Repository identity | `scm.RepoRef{Source, ID, Owner, Name}` | `ID` is GitHub's numeric repository id: it survives rename and transfer and is the only join key between two listings. `RepoKey{Source, ID}` is the comparable map key (GitHub ids are only unique per host) |
| Slug | `scm.Slug` | the portal-visible id: one URL segment, canonical **lower case** (`^[a-z0-9._-]{1,100}$`, kernel `scm.ParseSlug`; the public API's `Slug` type says the same), derived by `MakeSlug(prefix, name)` (4.2), unique across sources by construction |
| Group | `scm.GroupID` | `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 63 bytes; unique together with every group alias, case-insensitively |
| Entry id | `readmodel.EntryID(slug)` | the snapshot key of a repository |

A repository **rename** keeps `RepoRef.ID`, changes `Owner`/`Name`, and (without a prefix change) changes the slug; the old slug becomes an alias (4.2). A repository **transfer** to another owner behaves the same way. Neither is a removal.

### 3.2 Configuration types

```go
type Ungrouped string // "track" (default) | "ignore"

type Config struct {                       // the `catalog:` block
    Ungrouped          Ungrouped          `yaml:"ungrouped"`
    SlugPrefixBySource map[string]string  `yaml:"slug_prefix_by_source"` // key "source" or "source/owner"
    Heads              HeadsConfig        `yaml:"heads"`
    Groups             []GroupConfig      `yaml:"groups"`
}
type HeadsConfig struct {
    BatchSize        int           `yaml:"batch_size"`        // 1..100, default 50
    Interval         time.Duration `yaml:"interval"`          // 1m..1h, default 5m: TTL of catalog.heads
    ArchivedInterval time.Duration `yaml:"archived_interval"` // 1h..168h, default 24h: TTL of the per-repository catalog.stats job
}
type GroupConfig struct {
    ID           scm.GroupID       `yaml:"id"`
    DisplayName  string            `yaml:"display_name"`
    Description  string            `yaml:"description"`
    DefaultRepo  string            `yaml:"default_repo"` // a slug, matched case-insensitively
    Sources      []scm.SourceID    `yaml:"sources"`      // empty = every source
    Match        []Matcher         `yaml:"match"`
    Ref          string            `yaml:"ref"`          // tracked ref override; "" = each repository's default branch
    Aliases      []string          `yaml:"aliases"`      // earlier ids of this group
    Integrations GroupIntegrations `yaml:"integrations"`
}
type GroupIntegrations struct { Jenkins *JenkinsDefaults `yaml:"jenkins"`; Snyk *SnykDefaults `yaml:"snyk"` }
type JenkinsDefaults struct{ JobPrefix string `yaml:"job_prefix"`; Instance string `yaml:"instance"` }   // instance: a configured Jenkins instance name (14 section 6.1)
type SnykDefaults struct{ ProjectPrefix string `yaml:"project_prefix"` }
func (g GroupConfig) Meta() scm.GroupMeta                 // Integrations: {"jenkins":{"job_prefix":...,"instance":...},"snyk":{"project_prefix":...}}
func DefaultConfig() Config                               // Ungrouped track, Heads{50, 5m, 24h}; cmd/scp decodes the user's file on top of it
```

```go
type Matcher struct{ Kind MatcherKind; Value string }     // Value "true" for catch_all
type MatcherKind string                                   // KindExact "exact", KindPrefix "prefix", KindGlob "glob", KindRegexp "regexp", KindTopic "topic", KindCatchAll "catch_all"
func (m *Matcher) UnmarshalYAML(unmarshal func(any) error) error   // exactly one key; catch_all must be true
func (m Matcher) Compile() (CompiledMatcher, error)                // error is *MatcherError{Kind, Code, Msg}
type CompiledMatcher struct{ /* immutable, safe for concurrent use */ }
func (c CompiledMatcher) Match(name string, topics []string) bool
func (c CompiledMatcher) Kind() MatcherKind; func (c CompiledMatcher) Source() string; func (c CompiledMatcher) MatchesEmpty() bool
const (MaxMatchersPerGroup = 64; MaxPatternBytes = 256; MaxRegexpProgram = 1000; MaxGlobWildcards = 6)
```

```go
type VisibilityPolicy string  // VisibilityPublicOnly "public_only" (default) | VisibilityInstallationScope "installation_scope"
type HostClass uint8          // HostGitHubCom | HostGHES | HostGHECDataResidency
type DiscoveryPolicy struct { // the catalog-owned keys of github.sources[].discovery
    Visibility            VisibilityPolicy `yaml:"visibility"`
    IncludeArchived       bool             `yaml:"include_archived"`        // default true
    IncludeForks          bool             `yaml:"include_forks"`           // default false
    ReconcileEvery        time.Duration    `yaml:"reconcile_every"`         // 1m..24h, default 30m
    MaxRemovalRatio       float64          `yaml:"max_removal_ratio"`       // (0,1], default 0.3
    RemovalConfirmations  int              `yaml:"removal_confirmations"`   // 1..10, default 2
    MassRemovalHold       time.Duration    `yaml:"mass_removal_hold"`       // 0..168h, default 6h
    AccessAcknowledgement string           `yaml:"access_acknowledgement"`  // "" | network_restricted | oidc_proxy | trusted_proxy_headers
}
func DefaultDiscoveryPolicy() DiscoveryPolicy
type PolicyContext struct{ Host HostClass; AccessEnabled bool }
func (p DiscoveryPolicy) Validate(path string, pc PolicyContext) []Finding   // C080-C086
func (p DiscoveryPolicy) Admit(r scm.Repository) Exclusion                   // fail closed
type Exclusion uint8 // ExcludedNone, ExcludedInvalid, ExcludedVisibility, ExcludedArchived, ExcludedFork, ExcludedUngrouped
```

```go
type Severity uint8 // SevInfo, SevWarning, SevError
type Finding struct{ Code string; Severity Severity; Path, Message, Hint string }   // Path is JSON-pointer-like: "/catalog/groups/3/match/1"
func SortFindings(fs []Finding)   // errors first, then path (numeric segments compared numerically), then code, then message
func HasErrors(fs []Finding) bool
type ValidateContext struct{ Sources []scm.SourceID }       // empty disables source-reference checks (offline lint of a fragment)
func (c Config) Validate(vc ValidateContext) []Finding       // offline, never touches the network, never panics on hostile input
func Lint(cfg Config, vc ValidateContext, prefix func(src scm.SourceID, owner string) string, repos []scm.Repository) []Finding
```

### 3.3 Catalogue state types

```go
type RepoKey struct{ Source scm.SourceID; ID scm.RepoID }
func KeyOf(r scm.RepoRef) RepoKey

type Tracked struct {          // one catalogue entry
    Repo      scm.Repository   // Slug set; Repo.DefaultBranch is GitHub's default branch as listed
    Aliases   []scm.Slug       // earlier slugs, newest first, at most MaxSlugAliasLen (8)
    FirstSeen time.Time        // UTC; the tie-break that makes slug ownership stable
    SeenAt    time.Time        // UTC; last complete listing that contained it
    Misses    int              // consecutive complete listings without it
}
type Held struct{ Fingerprint string; Since time.Time; Candidates int }      // a mass removal being held back (memory only)
type Quarantine struct{ Ref scm.RepoRef; Slug scm.Slug; Reason, Detail string } // slug_collision | slug_reserved | slug_invalid | slug_too_long | slug_dot_segment
type Removal struct{ Ref scm.RepoRef; Reason string }                          // absent | visibility | archived | fork | ungrouped | quarantined
type Rename struct{ Ref scm.RepoRef; OldSlug, NewSlug scm.Slug }

type PlanInput struct {
    Source  scm.SourceID
    Policy  DiscoveryPolicy
    Listed  []scm.Repository                              // the COMPLETE listing of Source; an incomplete listing is never planned
    Current []Tracked                                     // the whole catalogue, all sources
    Held    *Held
    Now     time.Time
    Prefix  func(src scm.SourceID, owner string) string   // "" when none
    Grouped func(scm.Repository) bool                     // nil means ungrouped == track
}
type Plan struct {
    Next        []Tracked                 // whole catalogue after the plan, sorted by folded slug
    Added       []scm.RepoRef; Removed []Removal; Renamed []Rename; Updated []scm.RepoRef
    Quarantined []Quarantine
    Excluded    map[Exclusion]int
    Held        *Held; HeldCount int
    Findings    []Finding                 // R301 (held), R302 (quarantined)
}
func PlanReconcile(in PlanInput) Plan     // PURE: no I/O, no clock, no goroutine; deterministic for any listing order
```

```go
type Compiled struct{ /* validated Config with every matcher compiled; immutable */ }
func Compile(cfg Config) (*Compiled, error)
func (c *Compiled) Matches(i int, r scm.Repository) bool    // does group i select r
func (c *Compiled) Grouped(r scm.Repository) bool
func (c *Compiled) BuildMembership(repos []scm.Repository) (MembershipResult, error) // error: the kernel scm.NewMembership rejected the input (cannot happen for a plan result)
type MembershipResult struct {
    Membership   scm.Membership
    Groups       []GroupHealth                // config order
    Multi        map[scm.Slug][]scm.GroupID   // repositories in more than one group
    Ungrouped    []scm.Slug
    TrackedRef   map[scm.Slug]string          // effective ref for repositories under a group `ref` override
    RefConflicts map[scm.Slug][]string        // groups that disagree; the first in config order wins
}
type GroupHealth struct{ ID scm.GroupID; Members int; DefaultRepo scm.Slug; DefaultReason string } // "" | "missing" | "not_selected"; default_repo is a repository NAME (V-11)
```

```go
type Status struct {                  // published as the global catalog.sync (ADMIN class)
    Sources       []SourceStatus
    Groups        []GroupHealth
    Findings      []Finding           // runtime R301/R302/R205 findings; R301/R302 are kept per source, so a held source keeps its R301 while another source commits (V-07)
    LastReconcile time.Time           // oldest LastSuccess over all sources; zero until every source has been listed live once
}
type SourceStatus struct {
    ID scm.SourceID
    State string                      // never | warm | ok | empty | failed | held
    LastAttempt, LastSuccess time.Time
    ErrCode string                    // rate_limited | permanent | transient | canceled | timeout | gone
    Listed, Tracked int
    Excluded map[string]int           // by Exclusion.String()
    HeldCount, Quarantined int
}
```

```go
type Deps struct {                    // every field required except where noted
    Config    Config
    Sources   []SourceDeps            // configuration order; SourceDeps{ID scm.SourceID; Policy DiscoveryPolicy; Host HostClass}
    Lister    RepositoryLister
    Prober    HeadProber
    Store     CatalogueStore
    Publisher *readmodel.Publisher
    Slots     Slots
    Tracker   Tracker                 // may be nil (tests)
    Events    EventSink               // may be nil (tests)
    Log       *slog.Logger
    Meter     metric.Meter
    Tracer    trace.Tracer
    Seed      uint64                  // jitter of due checks; tests pass a constant
}
func New(d Deps) (*Service, error)    // refuses a Config with findings of severity error
func (s *Service) Jobs() []job.Spec
func (s *Service) React(ctx context.Context, ev scm.Event, enq job.Enqueuer) error
func (s *Service) Warm(ctx context.Context) (func(*readmodel.Builder) error, WarmInfo, error)
func (s *Service) Mount(m httpapi.Mux)
type WarmInfo struct{ Repos, Skipped, StatsItems int; Newest time.Time }
```

### 3.4 Published values and the one value-semantics deviation

`catalog.repo` carries `scm.Repository` exactly as GitHub listed it **except** two fields: `Slug` is the portal slug, and `DefaultBranch` is the *effective tracked ref* (D-1). Rationale: Q-08 says "branch = repository `default_branch` unless a group sets an explicit `ref`", and `commits`, `docs` and `manifests` all need that one answer; computing it per consumer would let each feature pick its own branch (GR-10: a ref is resolved from the repository, once). When a group `ref` applies, `catalog` writes the override into `DefaultBranch`, and the head sweep still probes GitHub's real default branch (warning W103 tells the operator that a `ref` override is not covered by the sweep). `Tracked.Repo.DefaultBranch` keeps GitHub's value so that a config change from `ref: release` to none restores it without a listing.

### 3.5 Value semantics rules

- Everything handed to `readmodel.Put` is built fresh and owned by the snapshot afterwards: `Topics` slices are cloned at the listing boundary (`slices.Clone` of each page, `listSource`), `Membership` is built by `scm.NewMembership` which takes ownership of freshly built maps and slices.
- `Tracked` values are plain data: no pointers into the adapter's pages, so a later listing cannot mutate a published value.
- Time is UTC everywhere (`now := time.Now().UTC()`); `Tracked.FirstSeen` is set once and never rewritten.

---

## 4. Behavioural specification

### 4.1 Matchers

A group's `match` is a list of one-key mappings. **A repository is a member when any matcher of the group selects it**; a repository may be a member of any number of groups (Q-07). Matching is on the repository **name** (the GitHub name, not the slug), except `topic`.

| Kind | YAML | Semantics | Operand rules |
|---|---|---|---|
| `exact` | `- exact: Widget` | whole name equals, case-insensitive (GitHub names are case-insensitive) | 1..256 bytes of `[A-Za-z0-9._-]` |
| `prefix` | `- prefix: Widget-` | name starts with, case-insensitive | same |
| `glob` | `- glob: "*-SDK"` | whole name, case-insensitive; only `*` (any run, also empty) and `?` (one char) are special; every other character must be a name character, so `.` is literal | at most 6 `*`; consecutive `*` collapse |
| `regexp` | `- regexp: '(Widget\|Config)-.*'` | **RE2, anchored to the whole name** (`^(?:p)$`), case-sensitive unless the pattern says `(?i)` | at most 256 bytes; the compiled program at most 1,000 instructions |
| `topic` | `- topic: iam` | the repository has this GitHub topic, case-insensitive | GitHub topic grammar: 1..50 of `[a-z0-9-]`, starts with a letter or digit (verified 2026-10-14) |
| `catch_all` | `- catch_all: true` | every repository | the value must be `true`; `false` is an error |

Rules and the reasoning behind them:

1. **Typed and closed.** An entry with zero or two kinds, an unknown kind, a bare scalar entry with no kind (for example `- Widget*`) or `catch_all: false` is a decode error (strict decoding, D-07). This is GR-09: a glob-looking pattern evaluated as an unanchored regular expression is the classic trap (`IoT*` read as a regex is `Io` followed by zero or more `T`, and an unanchored search then selects every name containing "Io"), so a pattern's kind is always explicit and every kind matches the whole name.
2. **RE2 only, and linear.** `regexp` uses Go's RE2 engine (guaranteed linear-time matching; the Go documentation of `regexp` states the RE2 syntax and the linear guarantee, `regexp/syntax` documents the 1,000 repetition cap, verified 2026-10-14). `glob` is translated to an RE2 program `(?is)^…$` with `.*` for runs, so it is linear too and never backtracks. The 256-byte operand cap, the 1,000-instruction program cap and the 6-star cap bound the work a hostile or careless config can cause; the per-group cap is 64 matchers and 500 groups. A repository name is at most 100 bytes, so the worst case is 64 x 500 x 100 steps per repository, still tiny (benchmark in 12.7).
3. **The anchor wrapper cannot escape.** `compileAnchored` first parses the user's pattern **on its own** with `syntax.Parse(p, syntax.Perl)`, so an unbalanced `)` or a top-level alternation cannot leak out of `^(?:` … `)$`; only then is `^(?:p)$` compiled. A test asserts the wrapped program is a single concatenation for every pattern that parses (`TestAnchorWrapperNeverEscapes`) and `FuzzMatcherCompile` fuzzes arbitrary strings (1.29 million executions, no failure, 2026-10-14).
4. **Case.** `exact`/`prefix`/`glob`/`topic` fold case; `regexp` does not unless it opts in with `(?i)`. This is the documented trade: names on GitHub are case-insensitive but a regexp author who writes `[A-Z]` means it.
5. **Empty-matching regexps** (`.*`, `a*`) select every repository including the empty name; `Validate` warns (`W108`) and recommends `catch_all: true`.
6. **Ungrouped repositories** (selected by no group) are tracked and listed without a group by default (`ungrouped: track`); `ignore` does not track them at all and costs no upstream budget (4.4, R-step 2). With `ignore`, a repository that later matches a group is picked up at the next reconcile.

Pseudocode (`Compiled.Matches`, `CompiledMatcher.Match`):

```
Matches(group g, repo r):
    if g.sources != nil and r.source not in g.sources: return false
    for m in g.matchers: if m.Match(r.name, r.topics): return true
    return false

Match(m, name, topics):
    catch_all -> true
    exact     -> lower(name) == m.fold
    prefix    -> hasPrefix(lower(name), m.fold)
    glob, regexp -> m.re.MatchString(name)          // RE2, anchored at compile time
    topic     -> any(lower(t) == m.fold for t in topics)
```

### 4.2 Slugs, prefixes and aliases

`MakeSlug(prefix, name) (scm.Slug, error)` is pure and deterministic. With `s := prefix + name`:

| Condition | Result |
|---|---|
| `name == ""` or `s` has a character outside `[A-Za-z0-9._-]` | `*SlugError{Code: "invalid"}` |
| `s` is `.` or `..` | `dot_segment` |
| `len(s) > 100` (`scm.MaxSlugBytes`; V-01: the kernel's `scm.ParseSlug` caps a slug at 100 bytes, so a long prefix shortens the longest legal name) | `too_long` |
| `lower(s)` in `{ext, groups, avatars}` | `reserved`, hint: set `slug_prefix_by_source` |
| last dot-suffix (case-insensitive) in `{md, adoc, txt, xml, svg, json, yaml}` | that dot becomes `-`: `notes.md` -> `notes-md` |
| otherwise | the result is **lower-cased** and passed through `scm.ParseSlug` (the kernel is the final authority; a result it rejects is `invalid`) |

**The slug is canonical lower case (V-01).** The kernel's `scm.Slug` grammar is `^[a-z0-9._-]{1,100}$`, the public API's `Slug` type says the same, and `store.KeyOf` folds. `Widget-Core` therefore has the slug `widget-core`; the display case lives in `Ref.Owner`/`Ref.Name` (and in `Repository.Name`). A slug is a pure function of `prefix + name`, two repositories whose names differ only in case collide (below), and nothing in the package compares slugs case-insensitively any more because there is no second spelling to compare.

Why the suffix rule: `/repos/{slug}.md`-style routes (`readme.md`, `llms.txt`, `dependencies.md`) and the SPA fallback (which never serves `index.html` for a path whose last segment looks like a document, 10-architecture 11.2) would make `/repos/notes.md` ambiguous. Organisations commonly have dotted repositories such as `.github` and `acme.github.io` (both in the example fixture), whose last suffix (`github`, `io`) is not reserved, so both keep their names; the rule exists for the day someone creates `docs.md` or `api.json` (the fixture's `notes.md` becomes `notes-md`). Names with a leading dot or underscore (`.github`, `_parentpom`, `_deprecated_ledger-v1`) are legal slugs and are asserted by `TestMakeSlug`.

Prefix lookup (`Service.prefix`): `slug_prefix_by_source["source/owner"]` first, then `slug_prefix_by_source["source"]`, else `""`.

**Uniqueness and collisions (R-step 6).** Slugs are unique across the whole catalogue (canonical lower case, so the comparison is exact). When two repositories derive the same slug (two owners in one source, two sources, case-only difference), exactly one keeps it; the stable winner is chosen by this order: (1) the repository that **already owned** the slug before this plan, (2) the earlier `FirstSeen`, (3) the smaller `Source`, (4) the smaller `ID`. Every loser is **quarantined**: not tracked, listed in `Plan.Quarantined`, reported as finding `R302` (error) with the hint to set a prefix, counted in `scp_catalog_repositories{state="quarantined"}`. The deviation from ADR-023 ("duplicates are a startup error") is D-3: a collision created by a *new repository on GitHub* must not take the whole portal down at 03:00, and a startup error is still raised by `scp config lint` (`R206`) for the repository list known at lint time.

**Aliases.** When a tracked repository's slug changes (rename, transfer, prefix change), `pushAlias` puts the old slug first in `Tracked.Aliases`, drops duplicates and the current slug, and caps the list at 8. `buildAliases` flattens all aliases into `catalog.aliases` (old slug to current slug), skipping an alias that is a real slug. Because every slug is lower case there is no "lower-case form" to add: an old upper-case URL (`/repos/Widget-Core`) is canonicalised by `overview` (it lower-cases the segment before the lookup, 14 section 4.8) and an old slug of a renamed repository is found through this map. `overview` answers 308 from this map (A-13/ADR-023 amendment). Slugs of removed repositories are not kept (a deleted repository's URL is a real 404).

**Group aliases.** `GroupConfig.Aliases` lists earlier group ids, so renaming a group does not break its links. `catalog.group_aliases` maps the lower-cased alias to the group id; `overview` consults it for `/groups/{groupId}` and answers 308 to the current id (and checks that the target group is visible to the caller). A pure case difference needs no alias: `overview` lower-cases the requested id before the lookup, so `/groups/Atlas` reaches `atlas` (V-18).

### 4.3 Visibility policy and admission

`public_only` (default) admits a repository only when `Visibility == scm.VisPublic` (the kernel's zero value is now `VisUnknown`, which no policy admits: V-14, D-8 resolved). `installation_scope` admits everything the credential can see (`VisPublic`, `VisInternal`, `VisPrivate`).

`Admit` is **fail closed**: a visibility value this code does not know, a zero `RepoRef.ID`, an empty name or owner, or an unknown policy string is `ExcludedVisibility`/`ExcludedInvalid`. `TestAdmitFailsClosed` feeds `VisInternal`, `VisPrivate` and an out-of-range value to `public_only`.

Admission order (first match wins; counted per reason in `scp_catalog_excluded`):

1. invalid identity -> `invalid`
2. visibility -> `visibility`
3. `archived && !include_archived` -> `archived`
4. `fork && !include_forks` -> `fork`
5. `ungrouped == ignore` and no group selects it -> `ungrouped`

Startup refusals (`DiscoveryPolicy.Validate`, 4.9), all errors:

| Code | Refusal |
|---|---|
| C080 | `visibility` is not `public_only`/`installation_scope`; or `installation_scope` without `access.mode` other than `public` (private data without access control, GR-35) |
| C081 | `public_only` on GHEC with data residency (no public repositories exist there; the catalogue would be silently empty) |
| C082 | `installation_scope` on GHES or GHEC-DR without `access_acknowledgement` in {`network_restricted`, `oidc_proxy`, `trusted_proxy_headers`} |
| C083 | `reconcile_every` outside 1m..24h |
| C084 | `max_removal_ratio` outside (0, 1] |
| C085 | `removal_confirmations` outside 1..10 |
| C086 | `mass_removal_hold` outside 0..168h |

**Security-relevant asymmetry.** An exclusion for visibility is applied **at once, in the plan, with no confirmation counter and no mass-removal hold**, because a repository that became private must disappear immediately. Every other removal (archived, fork, ungrouped, absent, and an entry that became invalid, which counts as absent) is a *candidate* subject to confirmation and the hold. A 10-repository organisation that is made private in one action therefore drops all ten at the next listing; the guard never delays a confidentiality removal (`TestPlanVisibilityIsImmediateAndUnguarded`).

**Runtime fork default.** The architecture's `include_forks: false` is kept (D-3 does not touch it). Its cost is visible in forks-heavy organisations: a fork that is a group's `default_repo` is excluded, the group degrades (`defaultRepoState: missing`, status finding `R205`), and the fix is one key (`include_forks: true`). 15 (Q-2) asks the owner whether the default should flip.

### 4.4 Reconcile (job `catalog.reconcile`, global scope)

`runReconcile` iterates the configured sources in order and, for each **due** source, performs R-step 0 (list), then `commit` (R-steps 1-8). A source is due when an event asked for it (`wanted`), or it has never been listed live in this process, or `reconcile_every - 10%` has elapsed since its last success (the 10% slack keeps the planner's jitter from skipping a source). A rate-limit error, a cancelled context or a closed engine ends the whole run at once so the engine can pause the upstream; any other failure of one source is joined into the returned error and the next source still runs.

```
runReconcile(ctx):
    next := zero ; errs := []
    for src in sources:
        if not due(src): count(result=skipped); continue
        changed, err := reconcileSource(ctx, src)            // R0 + commit
        if err:
            if rateLimited(err) or ctx.Err() or engineClosed(err): return Outcome{}, err
            errs += err; continue
        if hasUnconfirmed(src):                              // a tracked repository was missing from this complete listing
            want(src); next = min(next, now + confirmGap)    // confirmGap = 2 minutes
    return Outcome{Unchanged: nothing changed, NextDue: next}, join(errs)
```

**R0 list.** Pages are requested one after another with the validators of the previous pass (`ETag` per cursor, in memory only: ETags are valid only inside one token lifetime, ADR-004). A page answered `NotModified` is reused from memory. Any page failure abandons the whole listing: **a partial list is never diffed** (GR-26). A listing that has not ended after 1,000 pages is an error (`ListError`, a runaway cursor must not run forever). After a complete listing, cached pages whose cursor was not used are dropped (GitHub's cursor space changed). `ListError{Source, Page, Err}` unwraps to the adapter's error so `job.AsRateLimited` still sees a rate limit.

**R1 normalise.** Set `Ref.Source`; `normaliseListed` rejects entries with a zero id, an owner or name that `scm.RepoRef.Validate` refuses (GitHub's grammar, at most 100 bytes of `[A-Za-z0-9._-]`, and not `.` or `..`: V-17, found by fuzzing the package against the kernel) or a default branch that is not a legal ref (counted `invalid`), and **cleans** the free text of the rest (control and bidirectional characters removed from the description, 1,000 bytes at a rune boundary, topics outside GitHub's grammar dropped, at most 20, a URL that is not **https** (the API's `HttpsUrl` type) or has credentials or is longer than 2,000 bytes dropped: V-16); the last duplicate of one `RepoKey` wins (a pagination shift can list a repository twice).

**R2 classify.** For each listed repository, `Policy.Admit`; then `Grouped` (when `ungrouped: ignore`). Result: `eligible` map and `excl` map by exclusion.

**R3 classify what the source already tracks.**

```
for each tracked t of this source, in ID order:
    if t in eligible:  Misses = 0; SeenAt = now; Updated if fields differ; keep (slug recomputed in R6)
    elif t in listed but excluded:
         visibility -> Removed{visibility} NOW (security; never held)
         else (archived, fork, ungrouped) -> keep, candidate Removal{reason}
    else (absent from a COMPLETE listing):
         Misses++ ; keep ; if Misses >= removal_confirmations: candidate Removal{absent}
```

An absence therefore needs `removal_confirmations` (default 2) **consecutive complete listings**. The follow-up listing is scheduled `confirmGap` (2 minutes) after the first, not a whole `reconcile_every` later, so a deleted repository leaves within minutes. A follow-up is scheduled only while some repository has `0 < Misses < removal_confirmations` (`hasUnconfirmed`); once the count is reached the removal is applied or held by R4, and a **held** plan is re-evaluated at the normal cadence (V-08: the first prototype re-listed every two minutes for the whole 6 h hold, 75 listings). Why not remove on the first miss: GitHub's pagination is offset-based, and a repository created or deleted *during* a listing shifts later pages, so an unrelated repository can be missing from one otherwise complete listing (`TestPlanRemovalNeedsTwoPasses`).

**R4 mass-removal guard.** Let `prev` be the number of repositories this source tracked before the plan, `allowed = max(int(max_removal_ratio * prev), 2)` (the floor of 2 lets a three-repository portal lose one), and `added` the repositories that are new in this listing.

```
if candidates > 0 and (len(candidates) > allowed  or  prev - len(candidates) + len(added) == 0):
    fp := sha256(sorted "source/id/reason" of candidates)[:8 bytes]
    if held != nil and held.fp == fp and now - held.since >= mass_removal_hold:
         apply the removals            // the same removal has been observed for the whole hold period
    else:
         Plan.Held = {fp, since = (held.since if same fp else now), n}; candidates = none
         finding R301 (warning): "holding back the removal of N of M repositories (limit L per pass); applies after <hold> if the listing stays the same"
```

The hold is **per source**, deterministic (no wall-clock read inside the plan: `Now` is an input) and **fingerprinted**: if the set of candidates changes, the clock restarts. It protects against a listing that is wrong in a way that still parses (an empty page after a token problem, a mis-scoped installation, a partial outage), which would otherwise empty the portal in one pass (GR-26). Visibility removals are never held (4.3). A held plan is *not* an error: the catalogue stays as it was, the source state is `held`, `scp_catalog_reconcile_total{result="held"}` counts it and `/status` shows R301. The owner can release it by raising `max_removal_ratio` or by waiting out the hold.

The hold lives in memory. After a restart `Held` is nil, the first listing recomputes the same candidates, and the clock restarts: strictly more cautious than before (a restart can only delay a mass removal, never speed it up).

**R5 additions.** Every eligible key not already tracked is added with `FirstSeen = SeenAt = now`.

**R6 slugs.** For every kept or added repository derive `MakeSlug(prefix(source, owner), name)`; an error quarantines it (`slug_invalid`, `slug_reserved`, `slug_too_long`, `slug_dot_segment`; if it was tracked it is also `Removed{quarantined}`). Then resolve collisions with the stable ordering of 4.2; the losers are quarantined. For a repository whose slug changed, `pushAlias` and `Rename` are recorded.

**R7 assemble.** `Next` = the repositories of other sources unchanged, plus this source's, sorted by folded slug. `Added`, `Removed`, `Renamed`, `Updated`, `Quarantined` are sorted by ID. `R302` findings are generated for every quarantine. The whole function is a pure function of its input: `TestPlanIsOrderIndependent` and `TestPlanInvariants` (200 passes x 20 seeds of random worlds: renames, transfers, deletions, visibility flips, duplicate listing entries) assert that no slug is duplicated, no non-public repository is present in a `public_only` catalogue, no repository is both removed and kept, aliases are at most 8 and never equal the current slug, and shuffling the listing does not change the plan.

**R8 commit (effectful).** Serialised by `commitMu` for the whole of persist + publish; the in-memory phase additionally holds `pubMu` (8.2), which `hide` also takes (V-03):

```
commit(src, listed, listStart):
    lock commitMu
    in   := readInput(...)              // under mu: clone of s.tracked, held of this source, hideGen; drops from the listing what an event hid
                                        // after listStart (hiddenAt hints; a demote hint forces Visibility private)
    plan := PlanReconcile({...in...})
    rows := EncodeRow(t) for t in plan.Next
    store.ReplaceCatalogue(rows)        // FIRST: restart state. A store error is counted (scp_catalog_store_errors_total{op=replace_catalogue}),
                                        //        logged, and NEVER blocks serving; ctx cancellation aborts without publishing
    lock pubMu                          // from here to the end nothing else may change tracked or the snapshot
    if hideGen != in.hideGen:           // an event hid a repository while the rows were being written
         in = readInput(...); plan = PlanReconcile(...)      // pure, no I/O: the plan is recomputed, never the stale one published
    mr := BuildMembership(plan.Next)    // error: kernel NewMembership refused -> Permanent
    publish(plan, mr)                   // ONE readmodel.Apply: remove entries whose slug changed or that left; put new/changed catalog.repo values
                                        // (carrying remembered stats under the new entry id; EVERY listed repository gets a fresh Info.FetchedAt,
                                        // V-05); put membership, aliases, group_aliases, sync
    lock mu: s.tracked = plan.Next; s.groups; source state (lastSuccess, listed, excluded, held, quarantined, findings, state)
    metrics, one log line per removal and rename
    recordGauges
    Tracker.SetRepos(refs)              // still under pubMu: the bridge only forwards to the planner and must not call back into catalog
```

Without the `pubMu` phase an event that arrives while the store write is in progress is lost twice over: `hide` publishes the removal, then the commit publishes a plan computed before it and the repository reappears for a whole `reconcile_every` (V-03, reproduced by `TestVerifyHideDuringCommitIsNotUndone`; with the lock removed 38 candidate snapshots were rejected or overwritten in the stress test).

**A failed listing is published too (V-06).** `markFailed` records the attempt and republishes **only** `catalog.sync` (under `pubMu`), so `/status` and the readiness gate see `failed` and `errCode` instead of a state frozen at the last success. `catalog.repo` and the membership are untouched (`TestServiceListFailureChangesNothing` asserts the catalogue part is unchanged and the status says `failed`/`transient`).

**Findings are kept per source (V-07).** `R301` and `R302` belong to the source that produced them; a commit for source B no longer erases the `R301` of a held source A. `Status.Findings` is the concatenation over sources, then the runtime `R205` per degraded group.

Why persist first and publish second: the store is the restart source and the snapshot is the serving source (5.3 and 10-architecture 9.3); a crash between the two leaves the store ahead of the snapshot, which `Warm` repairs by publishing the store's state, never the reverse. If `Apply` fails validation (`readmodel.ErrInvalidSnapshot`), the error is returned as `Permanent` (the engine parks the key and shows a critical status) because retrying would rewrite the same snapshot; the in-memory `tracked` is *not* updated, so memory and snapshot never disagree.

**Event-driven listing.** `React` marks a source `wanted` (4.8) so a webhook-announced change is listed even if `reconcile_every` has not elapsed; `wanted` is cleared when a listing of that source *starts*, so an event arriving during a listing triggers one more.

**Budget.** One reconcile costs `ceil(repos/100)` REST calls per source (the listing page size is 100, the documented maximum for `/installation/repositories` and `/orgs/{org}/repos`, verified 2026-10-14): 2 for a 171-repository organisation, 10 for 1,000 repositories. At the default 30-minute cadence that is 20 calls/hour for 1,000 repositories, and unchanged pages are conditional requests: a `304` with a correct `Authorization` header does not count against the primary rate limit (verified 2026-10-14). The follow-up confirmation listing adds one pass per detected absence, not a steady cost.

### 4.5 The head/stats sweep (job `catalog.heads`, global; job `catalog.stats`, per repository)

Purpose: detect, for the whole catalogue, a moved HEAD, a new tree and changed headline statistics with one batched probe per `heads.interval` (default 5 minutes), and turn a moved HEAD into a normalised event so that `commits`, `docs` and `manifests` refresh within minutes without a webhook (the design relies on this, 10-architecture 9.6).

```
runHeads(ctx):
    refs := tracked repositories that are NOT archived, sorted by ID, rotated left by headPos    // rotating start
    for batch in chunks(refs, heads.batch_size):
        res := Prober.ProbeHeads(ctx, batch)
        if err: count(heads_probe_total{result=error}, len(batch)); return err        // headPos kept: the next sweep resumes here
        applyStats(batch, res)
        headPos += len(batch)
    return Outcome{Unchanged: no stats changed}
```

The rotation guarantees that a sweep that keeps failing midway (rate limit, timeout) cannot starve the tail of the catalogue: the next sweep starts where the last one stopped.

`applyStats(batch, res)`, one call per batch:

1. Under `mu`, for each repository of the batch still tracked: take `res[repo.ID]`; **a repository missing from the map is counted (`missing`) and otherwise ignored: absence from a probe never removes anything**, only the listing does. Compare with the remembered stats: `DiffHeads(prev *RepoStats, next) -> HeadChange{First, HeadMoved, StatsMoved}`. `HeadMoved` requires both SHAs non-empty and different; a first observation has `First`, not `HeadMoved` (no event storm at start).
2. Persist (outside `mu`): `StatsMoved` -> `PutItem(Key{repo, "catalog.stats"}, State ok, ContentSHA = HeadSHA, Payload = statsPayload schema 1)`; otherwise `Revalidated` (no payload write). `store.ErrNoRepo` (the repository was removed meanwhile) is ignored; other errors are counted and logged and never block the publish.
3. Publish **one `Apply` per batch**: `Put(catalog.stats)` for every probed repository; a `readmodel.ErrUnknownRepo` for a repository removed in the meantime is skipped (`Put` can never resurrect a removed repository, A-22). `ErrInvalidSnapshot` is `Permanent`.
4. Only after a successful publish are the new stats remembered (`s.stats`), so a failed publish is retried by the next sweep instead of being forgotten.
5. After the publish and outside every lock, for each `HeadMoved` repository emit `HeadEvent` into `EventSink`: `scm.Event{Kind: EventHeadChanged, Delivery: "head-<repoID>-<sha12>", Source, Repo, Ref: "refs/heads/<branch>", Before, After, HeadTreeOID, PathsComplete: false}`. The delivery id is deterministic, so a retried sweep produces the same id. `PathsComplete: false` is deliberate: the sweep knows the tree oid, not the changed paths, so `manifests.React` compares `HeadTreeOID` with its stored tree (10-architecture 13.5). A reactor error other than `job.ErrClosed` is logged and counted (`head_events_total{result=error}`) and the loop continues; `ErrClosed` aborts.

`catalog.stats` (per-repository job) serves two purposes: it is the per-repository **TTL** of the stats value (default `archived_interval` 24h; for archived repositories, which the sweep skips, it is the only refresh, for the others it is a drift bound behind the 5-minute sweep, and `Outcome.Unchanged` lets the planner back off to that cap) and the **accelerator** for `release` and `create` events (`React` enqueues `catalog.stats/<source>/<owner>/<name>`, the **folded** key of `RepoRef.KeyParts`, V-09). It probes one repository with the same `applyStats`. An unknown key (the repository was removed) returns a `Gone` error; a malformed key a `Permanent` error.

Event ordering fact worth keeping: the stats publish happens **before** the `head_changed` event, so a reactor that reads the snapshot sees the new HEAD.

### 4.6 The membership index and snapshot validators

`BuildMembership(repos) (MembershipResult, error)` is deterministic: it sorts by slug, iterates groups in configuration order, and hands the kernel one `scm.MembershipSpec{Repos []MemberRepo, Groups []GroupSpec}` (V-13: the kernel constructor, not the prototype stub, is the only way to build an `scm.Membership`; its error return is propagated and cannot occur for a plan result, so the commit treats it as `Permanent`). From the spec the kernel derives

- `members[g]`: slugs selected by `g`, in slug order (empty slice, never nil, for an empty group so `ReposOf` is stable);
- `groupsOf(slug)`: in configuration order;
- the **class-aware** accessors `VisibleReposOf(g, class)`, `DefaultRepoFor(g, class)`, `Len(class)`, `Slugs(class)` and `SlugFor(ref)`: the kernel builds the per-class views from each `MemberRepo.Visibility` once, which is what makes the enumeration rule of 7.2 a property of the value rather than of every handler (V-12);
- `defaults[g]`: the member whose GitHub **repository name** (`Ref.Name`, case-insensitive) equals `default_repo` (V-11: `default_repo` is a repository name, not a slug, because the slug depends on the prefix and the case rule);
- `Multi`, `Ungrouped`, `TrackedRef`/`RefConflicts` for lint and status (catalogue-side, not in the kernel value).

`GroupHealth.DefaultReason`: `missing` (no tracked repository has that name: renamed, deleted, forked-out by policy) or `not_selected` (tracked, but the group's own matchers do not select it). The group still exists and still lists its members; the API reports `defaultRepoState: missing` and the UI degrades the card instead of dropping the group. A group is never hidden because of a configuration mistake: silently skipping it would leave the operator with a dashboard that looks complete and is not (GR-29); the degraded card and the `/status` finding say what to fix.

Validators (`Slots.Validator`, registered with the publisher; run on every candidate snapshot, including the ones built by `hide`): every slug listed by any `ReposOf(g)` (that is the `ViewAll` list) has a `catalog.repo` value; the default repository of a group is a member; every alias target has a `catalog.repo` value. The registry's built-in converse (every entry has a value in the existence slot) is not duplicated. Because every `Apply` of the catalogue recomputes membership and aliases *together with* the repository change, a removal can never be rejected by a stale membership (the failure the architecture's M1 note describes for the manifests graph does not apply here).

### 4.7 Default-repository validation (three layers)

| Layer | When | Rule | Outcome |
|---|---|---|---|
| Offline | `Config.Validate`, `scp config lint`, startup | `default_repo` is a legal GitHub repository name (`C032`: `[A-Za-z0-9._-]{1,100}`, not `.`/`..`), present (`C030`), and **selected by at least one of the group's own matchers** (`C031`, error). With a `topic` matcher present it cannot be decided offline: `C031` is then `info` | refuse start on error |
| Online lint | `Lint(..., repos)`, `scp doctor` | a repository of that name exists (`R205` error: "no repository named X exists") or exists but the group does not select it (`R205`; suppressed when `C031` already reported the same path); the default is archived (`R209` warning) | report |
| Runtime | every commit | resolved by `BuildMembership`; unresolved -> `GroupHealth.DefaultReason`, status finding `R205` (warning), `defaultRepoState: missing` in the API | the group degrades; nothing is guessed |

The offline rule is what `C031` buys: in the worked example (4.10) the `search` group fails `C031` because its default repository `BillingSearch` is not selected by `exact: Search` - a mistake a substring matcher would have hidden by selecting it by accident.

### 4.8 Events: `React`

`React(ctx, ev, enq) error` is a `reactor.Func`. It runs **synchronously on the webhook goroutine**: a cheap decision, non-blocking `Enqueue`, no I/O (10-architecture 6.4). It never retains `ev`.

| Event | Action | Reaction | Priority |
|---|---|---|---|
| `installation`, `installation_repositories` | any (incl. the synthetic `resync` with a zero `Repo` from the redelivery gap path, refresh design D10) | `want(ev.Source)` (all sources when `Source == ""`); enqueue `catalog.reconcile/_` | `PrioWebhook` |
| `repository` | `privatized`, `deleted`, `transferred` | `hide(ev.Repo, demote = action is privatized and the source admits private repositories)` **now** (below); `want`; enqueue reconcile with `force=true` | `PrioInteractive` |
| `repository` | `created`, `renamed`, `publicized`, `archived`, `unarchived`, `edited` | `want`; enqueue reconcile delayed 2 s (coalesces bursts) | `PrioWebhook` |
| `repository` | any other | counted `ignored` | - |
| `release`, `create` | - | known repository: enqueue `catalog.stats/<source>/<owner>/<name>` with the **folded** key parts of `RepoRef.KeyParts` (the same key the planner derives from `Repos()`; an unfolded key never coalesced with the planned run, V-09); unknown: `unknownRepo` | `PrioWebhook` |
| `push` | - | known: counted `ignored` (commits, docs, manifests react to pushes; the head sweep covers the catalogue's own data); unknown: `unknownRepo` | - |
| everything else | - | counted `ignored` | - |

`unknownRepo`: an authenticated event for a repository the catalogue does not hold (a new repository whose `created` event was lost, or a repository the policy excludes and that pushes all day). At most **one** reconcile per 5 minutes is triggered (lock-free CAS on an atomic timestamp), delayed by 10 s, so excluded repositories cannot cause a listing per push (`TestServiceReactHidesPrivatizedAtOnce` drives two events and expects one enqueue).

**`hide(ref, source)`** is the confidentiality fast path. A `privatized`, `deleted` or `transferred` hint removes the repository from the in-memory tracked set and from the snapshot in one `Apply` (`Builder.Remove(entry)`, membership, aliases and `catalog.sync` recomputed in the **same** Apply so the validators hold), without touching the store and without any I/O. It runs under `pubMu` (8.2), so it can neither be undone by a commit that planned before the event (V-03) nor publish a membership computed from an older tracked set; it looks the repository up by `RepoRef` through `lookupRef` (source and id first, then the folded `KeyOf`), because a `privatized` delivery can carry the new name of a repository whose rename the catalogue has not reconciled yet (V-04, `TestVerifyHideFindsRenamedRepositoryByID`).

**Demotion under `installation_scope` (V-10).** `privatized` means "no longer public", not "no longer tracked", for a source whose policy admits private repositories (`admitsPrivate(source)`): `hide(ref, source, demote=true)` keeps the entry, sets `Visibility = VisPrivate` in `catalog.repo` and in memory, and recomputes the membership, so an anonymous principal loses it at once and `ViewAll` principals keep it (`TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved`). `hiddenAt` remembers the hint with its kind; an older in-flight listing that still says `public` is overridden to `private` for that repository. `deleted` and `transferred` always remove. Under `public_only` every `privatized` event removes. The enqueued reconcile (forced, interactive priority) then either confirms and persists the removal (the listing marks it private or absent) or, if the hint was false, re-adds the repository. A forged hint can therefore at worst hide a repository for the few seconds a listing takes; the webhook is authenticated (HMAC) before it reaches `React`, and a repository can only be re-added by the same listing that normally adds it. `installation_repositories.removed` is *not* a fast path: `scm.Event` carries one `Repo`, not the removed list, and the same listing handles it at `PrioWebhook` (answer to refresh design question Q3: reconcile, not an early drop).

### 4.9 Configuration validation and lint rules

`Config.Validate(vc)` is offline and total: it returns findings sorted by severity, path, code; the process refuses to start when `HasErrors`. `Lint(cfg, vc, prefix, repos)` runs `Validate` and then the rules that need a repository list. `scp config lint` prints both; CI lints every shipped example (D-07).

| Code | Sev | Rule |
|---|---|---|
| C002 | error | `ungrouped` is `track` or `ignore` |
| C003 | error | `heads.batch_size` 1..100; `heads.interval` 1m..1h; `heads.archived_interval` 1h..168h |
| C010 | error | group id matches `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 63 bytes |
| C011 | error | group ids and aliases are unique together, case-insensitively |
| C012 | error | `display_name` required, at most 120 characters, no control characters |
| C013 | error | `description` at most 500 characters, plain text (no control characters other than newline) |
| C014 | error | an alias is 1..80 characters without control characters; at most 16 per group |
| C020 | error | a group has 1..64 matchers |
| C022 | error | a matcher does not compile (charset, glob stars, regexp syntax, program size, topic grammar, unknown kind) |
| C030 | error | `default_repo` required |
| C031 | error / info | `default_repo` is not selected by any matcher of its own group (info when a topic matcher makes it undecidable offline) |
| C032 | error | `default_repo` is not a legal GitHub repository name (V-11) |
| C040 | error | `ref` is not a valid branch name (practical subset of `git check-ref-format`) |
| C050 | error | an integration prefix is longer than 100 bytes or has control characters |
| C051 | error | `integrations.jenkins.instance` is not 1..63 characters of `[A-Za-z0-9._-]` (that the instance exists is `integrations.ValidateGroupDefaults`) |
| C060 | error | `slug_prefix_by_source` names an unknown source |
| C061 | error | a slug prefix is at most 32 characters of `[A-Za-z0-9._-]` (it is lower-cased inside the slug) |
| C070 | error | a group's `sources` entry is not a configured source |
| C080-C086 | error | discovery policy refusals (4.3) |
| C090 | error | more than 500 groups |
| W101 | warning | two groups have identical matcher sets and sources |
| W102 | warning | more than one group uses `catch_all` |
| W103 | warning | a `ref` override is not covered by the head sweep |
| W107 | warning | two groups share a display name (case-insensitive) |
| W108 | warning | a regexp or glob matches the empty name (probably a catch-all in disguise) |
| R201 | warning | a matcher matches no repository of the list |
| R202 | warning | a group has no members |
| R203 | info | repositories selected by more than one **non-catch-all** group (listed) |
| R204 | info / warning | repositories selected by no group (warning when `ungrouped: ignore`, because they are then not tracked) |
| R205 | error | no repository has the name `default_repo`, or one exists but the group does not select it |
| R206 | error | a repository's slug is invalid, reserved or collides; names the prefix key to set |
| R207 | warning | groups give one repository different `ref` overrides; the first in config order wins |
| R209 | warning | the default repository is archived |
| R301 | warning | (runtime, `catalog.sync`) a mass removal is held back |
| R302 | error | (runtime) a repository is quarantined (slug) |
| R208, R210 | - | reserved, unused |

W104 to W106 do not exist: the prototype's first draft had "a `*` in an `exact`/`prefix` operand" (W104), but that input is already a `C022` charset error, so the warning was dead code and was removed (test `TestValidateRules`, case "star in prefix is a charset error").

### 4.10 Lint against a repository list (worked example)

`Lint` is the operator's preview of what a configuration does to a real organisation before it is deployed. `scp config lint --repos repos.json`
runs `Validate` and then the `R2xx` rules against a repository list, with **no network access**. `--repos` is a flat JSON array of GitHub
repository objects (`gh api --paginate /orgs/ORG/repos | jq -s 'add'` produces it; only `name`, `default_branch`, `archived`, `fork` and `topics`
are read). `scp doctor` (7.5) does the same against a live listing.

The prototype carries a worked example: `fixtures/example-repos.json` (16 public repositories of an organisation `acme`: a dotted name, a
leading-underscore name, a fork, two archived repositories, a `notes.md`, a `feature/foundation` default branch) and `fixtures/example.catalog.yaml`
(five groups with four deliberate mistakes). Head of the catalogue (the first three groups; the rest in the assets):

```yaml
catalog:
  ungrouped: track
  groups:
    - id: atlas
      display_name: Atlas identity platform
      description: Gateway, administration and client libraries
      default_repo: Atlas-Gateway
      match:
        - prefix: Atlas
      integrations:
        jenkins:
          job_prefix: atlas-
    - id: billing
      display_name: Billing
      description: Invoicing and payment processing
      default_repo: Billing
      match:
        - exact: Billing
        - exact: Billing-Worker
        - exact: Invoicing
    - id: ledger
      display_name: Ledger
      description: Double-entry ledger service and its UI
      default_repo: Ledger-Service
      match:
        - prefix: Ledger
```

`Lint` of that file against the example list (`go run ./cmd/lintdemo`), exactly as produced and committed as `out/example.lint.txt`
(compared byte for byte by `TestExampleCatalogLintGolden`):

```text
error R205 /catalog/groups/2/default_repo: no repository named "Ledger-Service" exists
error C031 /catalog/groups/3/default_repo: "BillingSearch" is not selected by any matcher of group "search", so it can never be a member
warning R201 /catalog/groups/1/match/2/exact: matches no repository (exact: "Invoicing")
warning R202 /catalog/groups/3/match: group "search" has no members
warning R201 /catalog/groups/3/match/0/exact: matches no repository (exact: "Search")
5 findings, errors=true
```

What each finding demonstrates (each is a design driver):

1. **`R205` (online).** `ledger` names a default repository that does not exist. Offline validation cannot know that; the online lint can. At runtime the same condition is a degraded card with status finding `R205` (4.6), never a guessed substitute.
2. **`C031` (offline).** `search` names `BillingSearch`, which `exact: Search` does not select. The typed, whole-name matcher makes the mistake visible; a substring matcher would have selected `BillingSearch` by accident and hidden it (GR-09). `C031` is an error, so the process refuses to start.
3. **`R201`.** `exact: Invoicing` selects nothing: harmless, but usually a typo or a repository that was renamed. A warning, not an error.
4. **`R202` + `R201`.** `search` has no members; under Q-1 an empty group is omitted from `api.groups` for every principal, so lint is where the operator learns about it.

The member counts the real kernel computes for this file (`atlas` 4, including the fork and the archived repository because `Lint` evaluates matchers without the discovery policy; `billing` 2; `ledger` 2; `search` 0; `everything` 16) are pinned by `TestExampleCatalogBuildsMembership`. Because a file with an error finding refuses to start, a mistake like `search` is caught in CI rather than shipped as an empty card.

### 4.11 Warm start (`Warm`)

`Warm(ctx)` is startup step 5 (10-architecture 10.1). It reads the persisted catalogue and `catalog.stats` items and returns the function `cmd/scp` runs inside the **single** `Apply` that builds snapshot version 1:

1. `LoadCatalogue` (one read transaction, streaming). Each row is decoded with `DecodeRow`; an undecodable row (unknown schema, unknown visibility, bad slug or id, a store key that does not equal `store.KeyOf` of its own metadata) is skipped and counted (`WarmInfo.Skipped`), never fatal: the next listing re-adds it. **Each decoded row is then passed through `Admit` with the current policy of its source and skipped (counted) when it is excluded for visibility** (V-20): a catalogue persisted under `installation_scope` must not publish its private repositories after the operator tightened the source to `public_only` and restarted (`TestVerifyWarmAppliesTheCurrentVisibilityPolicy`). Archived and fork exclusions are not re-applied (the first live listing settles them). A row whose source is no longer configured is skipped too (its repositories leave with the first live reconcile).
2. `LoadItems` with a callback that keeps only `catalog.stats` items in state `ok` for tracked repositories; a bad payload is a missing item.
3. Membership, aliases and group aliases are recomputed from the **current configuration** (a config change takes effect on restart without a listing).
4. In-memory state is installed (`Service.Repos()` returns the warm set, which `cmd/scp` hands to the planner bridge at step 6 so `planner.Restore` finds real subjects; `Warm` itself cannot call `Tracker` because the planner is built after the warm load); the status says `State: "warm"`, `LastSuccess` shows the newest `SeenAt`, `LastReconcile` stays zero, and **a warm start never postpones the first live listing** (`warmAt` is display-only; the first `catalog.reconcile`, enqueued by `cmd/scp` at `PrioWebhook`, always lists).

`WarmInfo.Newest` feeds the `/readyz` `catalogue` gate (`readiness.warm_max_age`, default 72 h): the pod is ready on a warm catalogue younger than that, and `/status` reports `degraded` until a live listing succeeds. A cold start (empty store) is never ready until the first live listing completes (10-architecture 10.2).

---

## 5. Data

### 5.1 Snapshot slots

| Name | Kind | Value | Written by | Notes |
|---|---|---|---|---|
| `catalog.repo` | slot, `readmodel.Existence()` | `freshness.Value[scm.Repository]` | reconcile `Apply`, `Warm`, `hide` removes | `Info{State ok, FetchedAt = CheckedAt = listing time, TTL = reconcile TTL}`; the **value** is written only when it changed (`sameRepo`), so unchanged listings do not bump entry revisions, but the **`Info` is refreshed by every complete listing** as a re-`Put` of the same value with the new `Info`, but only when the previous `Info.FetchedAt` is older than half the TTL (V-05: without it every unchanged repository turned `stale` 1.5 x TTL after the first listing and `Derive` said so for the rest of the process life; the half-TTL rule bounds the revision churn to two bumps per TTL) |
| `catalog.stats` | slot | `freshness.Value[scm.RepoStats]` | `applyStats`, `publish` (carried over a rename), `Warm` | `Info{State ok, FetchedAt, CheckedAt, ContentSHA = HeadSHA, TTL = heads.interval}` |
| `catalog.membership` | global | `scm.Membership` | every reconcile and `hide`, `Warm` | immutable; built by the kernel's `scm.NewMembership(MembershipSpec)` (V-13); class-aware (`VisibleReposOf`, `DefaultRepoFor`) |
| `catalog.aliases` | global | `map[scm.Slug]scm.Slug` | same | |
| `catalog.group_aliases` | global | `map[scm.GroupID]scm.GroupID` | reconcile and `Warm` | config-derived; deviation D-4 (not in the architecture's global list) |
| `catalog.sync` | global | `catalog.Status` | reconcile, **failed listings and `hide`** (V-06), `Warm` | admin class; deviation D-4 |

`Slots` carries typed handles; `RegisterSlots(reg)` registers them in that order **before** `reg.Freeze()`.

A rename is, for the snapshot, `Remove(old entry)` + `Put(new entry)` within one `Apply`. The remembered `catalog.stats` value is carried to the new entry; other features' slot values for the old entry go with the old entry and are re-fetched under the new planner key (a rename changes `Owner/Name`, hence the job key, in every case except a pure prefix change). Open question 15 (Q-5) asks whether `readmodel.Builder` should grow a `Rename(old, new)` that carries slots; the prototype does not need it.

### 5.2 Store rows (SQLite, owned by the `store` unit)

`catalog` writes through `CatalogueStore` and defines two JSON payloads, both with an inner `schema` field (architecture 8.5: "feature-owned codec with an inner schema field").

**`store.Repo` row** (table `repo`, written only by `ReplaceCatalogue`, which also cascades the removal of items and commits of repositories that left):

| Column | Content |
|---|---|
| `Key` | `store.KeyOf(ref)`: `RepoKey{Source, Owner, Name}` with owner and name **folded to lower case** (the real store validates and rejects any other spelling, V-02); `source` is the `repo.source` column |
| `GitHubID` | `scm.RepoRef.ID` |
| `Slug` | the portal slug (canonical lower case) |
| `SeenAt` | last complete listing that contained it (UTC) |
| `Meta` | `repoMeta` JSON, schema 1 |

```go
type repoMeta struct {
    Schema        int       `json:"schema"`                    // 1; unknown schema = row skipped on Warm
    Owner         string    `json:"owner"`                     // display case (the key is folded): V-02
    Name          string    `json:"name"`                      // display case: regexp matchers are case-sensitive and the UI shows it
    Description   string    `json:"description,omitempty"`
    DefaultBranch string    `json:"default_branch,omitempty"`  // GitHub's, as listed
    Topics        []string  `json:"topics,omitempty"`
    Visibility    string    `json:"visibility"`                // public | internal | private (anything else = row skipped)
    Archived      bool      `json:"archived,omitempty"`
    Fork          bool      `json:"fork,omitempty"`
    HTMLURL       string    `json:"html_url,omitempty"`
    CreatedAt     time.Time `json:"created_at,omitzero"`
    PushedAt      time.Time `json:"pushed_at,omitzero"`
    Aliases       []string  `json:"aliases,omitempty"`         // newest first, at most 8
    FirstSeen     time.Time `json:"first_seen"`
    Misses        int       `json:"misses,omitempty"`          // survives a restart: a confirmation in progress is not forgotten
}
```

**`store.Item`, kind `catalog.stats`** (one per repository; state `ok`, `FetchedAt`, `CheckedAt`, `ContentSHA` = HEAD sha, `Payload` = `statsPayload` JSON schema 1: `open_issues, stars, license, release{tag,name,url,prerelease,published_at}, head_sha, head_tree_oid, pushed_at`). An unchanged probe calls `Revalidated` (no payload write: the common case costs one small update per 5 minutes per repository, or none when batched by the store).

JSON field names are explicit tags, not `scm` struct fields: the kernel type is not a wire format.

### 5.3 Retention and failure semantics

- **Catalogue rows**: exactly the current tracked set; a removal deletes the row and (by the store's cascade) its items and commits. No tombstones (a deleted repository's URL is a real 404).
- **Stats items**: one row per repository, overwritten; no history.
- **Aliases**: at most 8 old slugs per repository, kept in `Meta` for as long as the repository is tracked; they die with the repository.
- **Store errors never block serving**: a failed `ReplaceCatalogue` is counted and logged and the snapshot is still published (the snapshot is authoritative for serving); the planner's next listing rewrites the rows. `ErrNoRepo` on an item write means "removed meanwhile" and is ignored.
- **Divergence rule**: store ahead of snapshot (crash between persist and publish) is repaired by `Warm`; snapshot ahead of store (failed write) is repaired by the next successful `ReplaceCatalogue`, which always writes the whole set.

### 5.4 Schema evolution

There is no `catalog`-owned SQL. Schema evolution is the inner `schema` field: schema 2 of `repoMeta` or `statsPayload` ships a decoder that still reads schema 1 and an encoder that writes 2; a row with a schema the binary does not know is skipped on `Warm` (and rewritten at the first listing), never a startup error. Adding a field is not a schema bump (unknown fields are ignored on read, `encoding/json` default). Removing or re-typing a field is. The store's own migration runner (ADR-002) handles table changes.

Configuration evolution: the `catalog:` block is part of the root config `version: 1` (D-07). Renaming a key is a root-version bump with `scp config lint` printing the rename; adding an optional key with a default is not. `catalog.schema.json` is versioned by the root schema generation, not by its own `$id` suffix.

---

## 6. Configuration

All keys live under the root config of `cmd/scp` (`catalog:` and `github.sources[].discovery:`). Duration values are Go duration strings; the strict decoder rejects unknown keys (`KnownFields(true)`); `Validate`/`DiscoveryPolicy.Validate` collect path-accurate findings. Environment names follow `SCP_<PATH_IN_UPPER_SNAKE>` with list indices (ADR-012). Keys whose names contain characters that cannot appear in an environment variable are file-only (marked).

### 6.1 `catalog:`

| Key | Type | Default | Validation (code) | Env name | Consumer |
|---|---|---|---|---|---|
| `catalog.ungrouped` | enum `track`\|`ignore` | `track` | C002 | `SCP_CATALOG_UNGROUPED` | `Service.commit` (R-step 2 `Grouped`) |
| `catalog.slug_prefix_by_source` | map string to string | `{}` | key is `source` or `source/owner` with a known source (C060); prefix `[A-Za-z0-9._-]{0,32}` (C061) | file-only (the key contains `/`) | `Service.prefix` -> `PlanInput.Prefix` |
| `catalog.heads.batch_size` | int | `50` | 1..100 (C003) | `SCP_CATALOG_HEADS_BATCH_SIZE` | `runHeads` |
| `catalog.heads.interval` | duration | `5m` | 1m..1h (C003) | `SCP_CATALOG_HEADS_INTERVAL` | `Jobs()` TTL of `catalog.heads`; `freshness.Info.TTL` of `catalog.stats` slots |
| `catalog.heads.archived_interval` | duration | `24h` | 1h..168h (C003) | `SCP_CATALOG_HEADS_ARCHIVED_INTERVAL` | `Jobs()` TTL and `IdleBackoffMax` of `catalog.stats` |
| `catalog.groups` | list | `[]` | at most 500 (C090) | `SCP_CATALOG_GROUPS_<i>_...` | `Compile`, `BuildMembership`, `buildGroupAliases` |
| `catalog.groups[i].id` | string | required | C010, C011 | `SCP_CATALOG_GROUPS_<i>_ID` | membership, routes, aliases |
| `catalog.groups[i].display_name` | string | required | C012 | `SCP_CATALOG_GROUPS_<i>_DISPLAY_NAME` | `GroupMeta.DisplayName` -> API |
| `catalog.groups[i].description` | string | `""` | C013 | `SCP_CATALOG_GROUPS_<i>_DESCRIPTION` | `GroupMeta.Description` -> API |
| `catalog.groups[i].default_repo` | GitHub repository **name** | required | C030, C031, C032; online R205, R209 | `SCP_CATALOG_GROUPS_<i>_DEFAULT_REPO` | `BuildMembership` defaults |
| `catalog.groups[i].sources` | list of source ids | `[]` (all) | C070 | `SCP_CATALOG_GROUPS_<i>_SOURCES_<j>` | `compiledGroup.sources` |
| `catalog.groups[i].match` | list of one-key maps | required, 1..64 | C020, C022, W108, R201 | `SCP_CATALOG_GROUPS_<i>_MATCH_<j>_<KIND>` (e.g. `..._MATCH_0_PREFIX`) | `Compile`, `Matches` |
| `catalog.groups[i].ref` | branch name | `""` | C040, W103, R207 | `SCP_CATALOG_GROUPS_<i>_REF` | `BuildMembership` `TrackedRef` -> `catalog.repo.DefaultBranch` |
| `catalog.groups[i].aliases` | list of strings | `[]` | C014, C011 | `SCP_CATALOG_GROUPS_<i>_ALIASES_<j>` | `buildGroupAliases` |
| `catalog.groups[i].integrations.jenkins.job_prefix` | string | `""` | C050 | `SCP_CATALOG_GROUPS_<i>_INTEGRATIONS_JENKINS_JOB_PREFIX` | `GroupConfig.Meta` -> `scm.GroupMeta.Integrations["jenkins"]["job_prefix"]` -> `integrations` |
| `catalog.groups[i].integrations.jenkins.instance` | string | `""` | C051 (existence: `integrations`) | `SCP_CATALOG_GROUPS_<i>_INTEGRATIONS_JENKINS_INSTANCE` | same, `["jenkins"]["instance"]` |
| `catalog.groups[i].integrations.snyk.project_prefix` | string | `""` | C050 | `SCP_CATALOG_GROUPS_<i>_INTEGRATIONS_SNYK_PROJECT_PREFIX` | same, `["snyk"]["project_prefix"]` |

The group list is expected to live in the config **file**; the per-index env names exist because ADR-012's mapping is generic, and `scp config lint` prints the effective merged list.

### 6.2 `github.sources[i].discovery:` (catalog-owned keys)

`github` owns `mode` and `org`; `catalog` owns the keys below. `cmd/scp`'s source struct embeds `catalog.DiscoveryPolicy` with `yaml:",inline"` and the generated schema grafts `catalog.schema.json#/$defs/discovery` next to github's own with `allOf` + `unevaluatedProperties: false` (so a typo is still an error). The Go struct tags are the single source of the key names; a test asserts the schema's `discovery` property set equals the struct's yaml tags.

| Key | Type | Default | Validation | Env name | Consumer |
|---|---|---|---|---|---|
| `...discovery.visibility` | enum `public_only`\|`installation_scope` | `public_only` | C080, C081, C082 | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_VISIBILITY` | `Admit` |
| `...discovery.include_archived` | bool | `true` | - | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_INCLUDE_ARCHIVED` | `Admit` |
| `...discovery.include_forks` | bool | `false` | - | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_INCLUDE_FORKS` | `Admit` |
| `...discovery.reconcile_every` | duration | `30m` | 1m..24h (C083) | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_RECONCILE_EVERY` | `due`, `reconcileTTL` |
| `...discovery.max_removal_ratio` | float | `0.3` | (0,1] (C084) | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_MAX_REMOVAL_RATIO` | R4 guard |
| `...discovery.removal_confirmations` | int | `2` | 1..10 (C085) | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_REMOVAL_CONFIRMATIONS` | R3 absence rule |
| `...discovery.mass_removal_hold` | duration | `6h` | 0..168h (C086) | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_MASS_REMOVAL_HOLD` | R4 hold |
| `...discovery.access_acknowledgement` | enum `""`\|`network_restricted`\|`oidc_proxy`\|`trusted_proxy_headers` | `""` | C082 | `SCP_GITHUB_SOURCES_<i>_DISCOVERY_ACCESS_ACKNOWLEDGEMENT` | startup validation only |

`removal_confirmations` and `mass_removal_hold` are new (deviation D-5); `visibility`, `include_archived`, `include_forks`, `reconcile_every` and `max_removal_ratio` are in the architecture's example block and `access_acknowledgement` is ADR-023's.

### 6.3 Constants that are deliberately not configuration

| Constant | Value | Why fixed |
|---|---|---|
| `confirmGap` | 2 min | the delay of the follow-up listing that confirms an absence; shorter than any useful setting, longer than a pagination race |
| `guardFloor` | 2 | removals per pass that never trigger the mass guard |
| `unknownRepoReconcileEvery` | 5 min | rate limit of event-triggered listings for repositories the catalogue does not hold |
| `maxListPages` | 1000 | runaway-cursor bound (100,000 repositories) |
| `MaxSlugAliasLen` / `MaxAliases` | 8 / 16 | bound the alias map and the config |
| `MaxGroups`, `MaxMatchersPerGroup`, `MaxPatternBytes`, `MaxRegexpProgram`, `MaxGlobWildcards` | 500, 64, 256, 1000, 6 | work bounds against hostile config (4.1) |
| job timeouts | reconcile 5 min, heads 10 min, stats 2 min | the upstream default (2 min) is too short for a 10-page listing at pacing; set in `Jobs()` |

### 6.4 JSON Schema

`internal/catalog/catalog.schema.json` (`$schema` 2020-12, 82 lines, embedded as `catalog.Schema`, full text in appendix A) is hand-written because the matcher is a one-key mapping and `invopop/jsonschema`-style generation cannot express it. `cmd/scp config schema` grafts its `properties` under `/properties/catalog` and `$defs/discovery` into the generated root schema. Semantic rules (uniqueness, default membership, reserved words) are not expressible there; they are the `C`/`W`/`R` rules. A test in `cmd/scp` asserts documented keys == consumed keys: every yaml tag of `Config`, `GroupConfig`, `HeadsConfig`, `GroupIntegrations`, `DiscoveryPolicy` appears in the schema and vice versa (D-07, GR-12).

---

## 7. HTTP and API surface

### 7.1 Routes owned by `catalog`

`catalog` implements `httpapi.Mounter` and owns exactly two public API operations of the OpenAPI document (`api/openapi-public.yaml`, endpoint catalogue `07-openapi-contract-catalogue.md`):

| Operation | Route name | Method and path | ETag | Statuses |
|---|---|---|---|---|
| `listGroups` | `api.groups` | `GET /api/v1/groups` (`sort=config\|name`, `q`, `limit` default and max 1000, `cursor`) | `snapshot-variant` | 200 304 400 429 503 |
| `getGroup` | `api.group` | `GET /api/v1/groups/{groupId}` | `snapshot-variant` | 200 304 404 429 503 |

The architecture's route table (10-architecture 8.13) lists `api.group.repos` and `api.repos` as `catalog` too; the public OpenAPI (unit 07) assigns them to `overview` because each item is a composite of docs, commits, manifests and integration facts that `catalog` may not import. This design follows the OpenAPI (deviation D-6). `catalog` therefore has no repository list endpoint: `overview` builds it from `catalog.repo`, `catalog.stats` and `catalog.membership`.

Other URLs that mention catalogue data but are **not** `catalog`'s: `/groups/{groupId}`, `/repos/{repo}` (SPA shells and negotiated Markdown, `overview`), the 308 redirect of an earlier group id (`overview`, reads `catalog.group_aliases`), the 308 repository redirects (`overview`, reads `catalog.aliases` and checks the target's visibility), `GET /badges/groups/{groupId}/{file}` (`badges`, reads the membership).

`catalogapi` (generated, `internal/catalog/catalogapi`) provides the `StrictServerInterface` with `ListGroups` and `GetGroup`; `Service` implements it and `Mount` is

```go
func (s *Service) Mount(m httpapi.Mux) {
    h := catalogapi.NewStrictHandlerWithOptions(s, nil, catalogapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: reqErr, ResponseErrorHandlerFunc: respErr})
    catalogapi.HandlerFromMux(h, m)       // registers "GET /api/v1/groups" and "GET /api/v1/groups/{groupId}" on the routes.Recorder
}
```

(`reqErr`/`respErr` come from `httpapi.StrictErrors(log)`; handlers return problems as errors, `httpapi.Err(httpapi.NotFound(...))`.) The prototype leaves `Mount` empty; the shapes above are those of the generated code verified by the `httpapi` unit (06 section 4.4).

### 7.2 `listGroups` algorithm

```
ListGroups(ctx, req):
    snap  := rd.Current()                                           // exactly once
    p     := authz.PrincipalFrom(ctx); class := authz.ClassOf(az, p)
    page  := httpapi.ParsePage(limit, cursor, 1000, 1000)          // validation-failed / invalid-cursor problems (BEFORE the ETag: a bad request is never a 304)
    cards, ok := slots.Groups(snap, class)                          // groups.go: class-aware cards, empty-for-this-class groups already omitted
    if !ok: return NotReady(15s)                                    // before the first publish with a catalogue (503 not-ready)
    cards = filter(cards, q, GroupCard.MatchesQuery); order(cards, sort)   // `sort=name`: lower-cased display name, then id; default configuration order
    etag  := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), class, httpapi.Digest(sort, q, limit, cursor)),
                                httpapi.FreshnessDigest(statuses(cards, now)...))   // V-15: the body contains freshness derived from the CLOCK
    if httpapi.NotModified(r, etag): return 304
    items := []
    for c in page(cards):
        items += GroupSummary{id, displayName, description (null when empty), repoCount: c.RepoCount, defaultRepoState: c.DefaultState,
                              defaultRepo: RepoLink{slug, name, description, htmlUrl, uiUrl: routes.Repo(slug)} when ok,
                              integrations: c.Integrations, badges, uiUrl: routes.Group(id), commitsUiUrl, apiUrl, llmsUrl,
                              freshness: httpapi.NewFreshness(c.Freshness.Kind, c.Freshness.Info.Derive(now))}
    return page of items with nextCursor
```

Rules fixed here:

- **Badges** are URL builders only (`routes.BadgeGroup(id, "repos")`, `routes.Badge(slug, kind)`): the group repository-count badge always; `issues`, `last-commit`, `release` when the default repository resolves; `build` when the group has a `jenkins` integration entry. `catalog` does not render or fetch anything.
- **`defaultRepoState`**: `ok`; `missing` (configured but not tracked, not selected, or invisible to the caller); `unset` is never produced in v1 because `default_repo` is required.
- **`freshness`**: the freshness of what the card shows: when the default repository resolves, `httpapi.NewFreshness` of its `catalog.stats` value (`Kind: "catalog.stats"`, status from `Info.Derive(time.Now())` evaluated in the handler, never stored); otherwise of the first visible member's `catalog.repo` value (`Kind: "catalog.repo"`). **The ETag must change when a derived status changes** (V-15): `Derive` depends on the clock, so a response whose snapshot version has not moved can still flip a card from `fresh` to `stale`; the ETag therefore carries `httpapi.FreshnessDigest` of the statuses in the response (empty when every card is `fresh`, so the common case keeps the plain `W/"g<version>-<class>"` form of 06-httpapi), and the response says `Cache-Control: no-cache`. `getGroup` does the same over its one card. Question 6 (section 15) records that the example in the OpenAPI always says `catalog.stats`.
- **Visibility (V-12; deviation D-7 withdrawn).** The kernel's `scm.Membership` is class-aware: `NewMembership` receives each member's visibility and precomputes the per-class member lists, default repositories and counts, so `catalog.membership` stays **one** global value and no handler can forget the filter. `Slots.Groups(snap, class)` and `Slots.Group(snap, class, id)` (`groups.go`, compiled and tested) are the only readers the handlers use; they derive `RepoCount`, members, `DefaultState` and the omission of empty groups from `VisibleReposOf`/`DefaultRepoFor`. Because `repoCount`, `repos`, the omission of empty groups and `defaultRepoState` are all derived from the visible set, an anonymous response is byte-identical to the response of a deployment that tracks only the public repositories (the count-level enumeration test of 10-architecture 17.3, extended in 12.4).
- **Order**: configuration order is the default (the operator orders the file deliberately); `sort=name` gives a stable display-name order (A-25).

### 7.3 `getGroup`

`GroupDetail` = `GroupSummary` + `repos` (`GroupCard.Members`): the visible member slugs, **default repository first (when it resolves and is visible), then the others by case-folded slug**. The OpenAPI says "in configuration order"; no per-repository configuration order exists (matchers select repositories, they do not order them), and its example lists the default first, so this is the deterministic reading (clarification C-1 in section 14). An unknown id, a group with no visible member, and an earlier id (alias) all answer `404 not-found` with the **same body** (`httpapi.NotFound("group not found")`); `overview` resolves an alias with a 308 on `/groups/{groupId}` before the SPA ever calls the API.

### 7.4 What `/status` (admin, `ops`) reads

`ops` reads the `catalog.sync` global through a bridge. Field contract (admin class; never public): per source `id, state (never|warm|ok|empty|failed|held), lastAttempt, lastSuccess, errCode, listed, tracked, excluded{reason: n}, heldCount, quarantined`; per group `id, members, defaultRepo, defaultReason`; `findings[]` (`R205` warning per degraded group, `R301`, `R302`); `lastReconcile`. Readiness gate `catalogue` is `state == ok|empty` for a live listing or a warm catalogue younger than `readiness.warm_max_age`; `coverage` uses `Slots.Coverage(snapshot) (have, total int)` for the kind `catalog` (repositories with a `catalog.stats` value in state `ok`).

### 7.5 CLI surface

Not HTTP, but part of the unit's public surface (`cmd/scp` implements the commands, this package supplies the logic):

| Command | Calls | Output / exit |
|---|---|---|
| `scp config lint [--config f] [--repos repos.json]` | `Config.Validate`, `DiscoveryPolicy.Validate`, `Lint` | findings text or `--format json` (4.10); exit 1 on any error |
| `scp config schema` | `catalog.Schema` grafted into the root schema | JSON Schema on stdout |
| `scp doctor` (catalog part) | one live `ListRepositories` pass through `PlanReconcile` with an empty `Current` | per source: listed, admitted, excluded by reason, quarantined, groups with member counts, `Lint` findings; no state is written |

---

## 8. Concurrency and lifecycle

### 8.1 Jobs

`(*Service).Jobs()` returns three `job.Spec` values. All run inside the engine, so pacing, the budget gate, the rate-limit pause, parking and panic accounting apply to every upstream call (10-architecture 6.3).

| Kind | Scope and key | Upstream | TTL (+-10% jitter) | Priority | Timeout | `Outcome` |
|---|---|---|---|---|---|---|
| `catalog.reconcile` | global, `catalog.reconcile/_` | `github` | `min(discovery.reconcile_every)` (default 30m); per-source `due` check inside | `PrioScheduled`; events `PrioWebhook` / `PrioInteractive` | 5 min | `Unchanged` when nothing was added, removed, renamed or updated; `NextDue = now + 2m` while an absence is unconfirmed |
| `catalog.heads` | global, `catalog.heads/_` | `github` | `heads.interval` (5m) | `PrioScheduled` | 10 min | `Unchanged` when no stats value changed |
| `catalog.stats` | per repository, `catalog.stats/<source>/<owner>/<name>` | `github` | `heads.archived_interval` (24h), `IdleBackoffMax` the same | `PrioScheduled`; `release`/`create` events `PrioWebhook` | 2 min | `Unchanged` when stats are equal |

Error mapping (the engine classifies structurally, 10-architecture 8.3):

| Situation | Returned | Engine reaction |
|---|---|---|
| adapter rate limit (any page, any probe) | the adapter's error (wrapped by `ListError`; `job.AsRateLimited` sees it) | pause the upstream until the reset; no attempt consumed; the run ends at once (no other source is listed) |
| other adapter or network error | `ListError` / the error | transient: jittered backoff |
| `readmodel.ErrInvalidSnapshot` | `permanentError{err}` (`Permanent() == true`) | park the key, critical status (retrying would rewrite the same snapshot) |
| a `catalog.stats` key for a repository no longer tracked | `goneError` (`Gone() == true`) | drop the key |
| malformed `catalog.stats` key | `permanentError` | park |
| context cancelled / deadline | the context error | retried or dropped by shutdown |
| `job.ErrClosed` from an `EventSink` | returned | the run ends (shutting down) |

### 8.2 Goroutines and locks

`Service` starts **no goroutine**. Code runs on: engine workers (the three jobs; the engine guarantees one run per key, so `catalog.reconcile` never overlaps itself, but `reconcile`, `heads` and several `stats` runs overlap each other); webhook request goroutines (`React`, many at once); HTTP request goroutines (handlers, read-only on a snapshot); the startup goroutine (`New`, `Warm`).

| Lock | Protects | Held across | Order |
|---|---|---|---|
| `commitMu` | serialises reconcile commits | the whole persist + publish of one commit, including the store write | outermost |
| `pubMu` | every change of `tracked` together with the snapshot: the in-memory phase of `commit`, `hide`, `publishStatus` (V-03) | build and `Apply` of one candidate snapshot, **never** a store write or a network call; held for microseconds to about a millisecond | after `commitMu`, before `mu` |
| `mu` | `tracked`, `groups`, `stats`, `sources`, `headPos`, `hiddenAt`, `hideGen` | in-memory work only: never I/O, never a callback, never `Apply` | innermost |
| (publisher's mutex) | the snapshot build | inside `Apply` only | `Apply` callbacks take **no** catalog lock and do no I/O |

Rules enforced by review and by `-race` tests: (1) `mu` is never held across a callback: `EventSink.Handle`, `Publisher.Apply` and the store are called with `mu` released; `Publisher.Apply` and `Tracker.SetRepos` run under `pubMu` but their callbacks take no catalog lock, and the bridge behind `SetRepos` must not call back into `catalog` (contract in 2.2); (2) `React` never takes `commitMu`, so a webhook is never delayed by a store write; it takes `pubMu` only inside `hide`, whose wait is bounded by one `Apply`; (3) a reconcile that started before an event is not allowed to undo it, in two ways: `hide` records `hiddenAt[repo]` and `commit` drops from its listing every repository hidden **after** the listing began (`TestServiceHiddenRepositoryIsNotResurrectedByAnOlderListing`), and a `hideGen` counter lets the commit notice an event that arrived **while the store was being written** and plan again under `pubMu` (`TestVerifyHideDuringCommitIsNotUndone`); (4) in-memory `tracked` and the snapshot are updated in the order store -> snapshot -> memory, and memory only after a successful publish, all of it inside one `pubMu` section, so a reader of either never sees the other ahead by more than one `Apply`; (5) `hiddenAt` hints older than the listing that satisfied them are forgotten in `readInput`, so the map is bounded by the events of one listing interval.

### 8.3 Startup and shutdown

Startup (10-architecture 10.1, catalog's part; `New` does no I/O and needs only the store, the publisher and the bridge object, so `cmd/scp` constructs the `Service` just before step 5 although the architecture lists feature construction as step 6): validate `Config` and every `DiscoveryPolicy` (refuse on error) -> `RegisterSlots` before `Freeze`, `Slots.Validator` into the publisher -> `New` -> `Jobs()` registered with engine and planner -> `Warm` and one `Apply` (snapshot version 1) -> after the planner exists, `Tracker.SetRepos(svc.Repos())` -> mount routes -> engine runs -> `cmd/scp` enqueues `catalog.reconcile/_` at `PrioWebhook`; the first live listing always runs (4.11). Both listeners are open early, so `/livez` answers and pages show "warming up" until the `catalogue` gate passes.

Shutdown: nothing to stop. An in-flight job sees its context cancelled and returns `ctx.Err()` without publishing (`commit` checks the context after a failed `ReplaceCatalogue`, and `listSource` stops between pages). A cancelled listing changes nothing (`TestServiceListFailureChangesNothing`). `Warm` must not be called after the engine started; `React` and handlers are safe at any time after `New`.

### 8.4 Failure behaviour (what an operator sees)

| Failure | Catalogue | Serving | Status |
|---|---|---|---|
| a list page fails (any status) | unchanged | last good | source `failed`, `errCode` **published at once** (V-06); `scp_catalog_reconcile_total{result="failed"}` |
| listing complete but wrong (empty, mis-scoped) | removals held by R4 | last good | source `held`, finding `R301` |
| `ReplaceCatalogue` fails | memory and snapshot updated | serving continues | `scp_catalog_store_errors_total{op="replace_catalogue"}`; rewritten at the next listing |
| validator rejects the snapshot | unchanged | last good | job parked (`permanent`), critical `/status` |
| probe fails | unchanged | last good stats, `Derive` turns them stale after 1.5 x TTL | `heads_probe_total{result="error"}` |
| a repository missing from a probe | unchanged | unchanged | `heads_probe_total{result="missing"}`; never a removal |
| rate limit | unchanged | last good | upstream paused by the engine; `/status` shows `Paused until` |

---

## 9. Observability

Instruments are created from the injected `Meter` (OTel API only); names are constants next to the instruments (`metrics.go`) and a `cmd/scp` test compares them with the generated metric reference (ADR-013). Cardinality is bounded by configuration (sources and groups, at most 500), never by repository or slug.

### 9.1 Metrics (all prefixed `scp_catalog_`)

| Name | Type | Labels | Meaning |
|---|---|---|---|
| `scp_catalog_repositories` | gauge | `source`, `state` = tracked, quarantined, held | repositories by state, refreshed after every publish and at warm start |
| `scp_catalog_excluded` | gauge | `source`, `reason` = invalid, visibility, archived, fork, ungrouped | listed repositories the policy kept out (admin information; the same numbers as `catalog.sync`) |
| `scp_catalog_groups` | gauge | `state` = ok, degraded | groups with a resolved default and at least one member / the rest |
| `scp_catalog_group_members` | gauge | `group` | members per group (bounded by `MaxGroups`) |
| `scp_catalog_reconcile_total` | counter | `source`, `result` = ok, failed, held, skipped | reconcile outcomes per source |
| `scp_catalog_reconcile_seconds` | histogram | `source` | listing plus commit |
| `scp_catalog_list_pages_total` | counter | `source`, `result` = 200, 304 | pages fetched; the 304 share shows the ETag saving |
| `scp_catalog_changes_total` | counter | `source`, `change` = added, removed, renamed, updated | repositories changed by reconciles |
| `scp_catalog_heads_probe_total` | counter | `result` = ok, error, missing | repositories probed |
| `scp_catalog_heads_seconds` | histogram | - | duration of one sweep |
| `scp_catalog_head_changes_total` | counter | - | repositories whose HEAD moved |
| `scp_catalog_head_events_total` | counter | `result` = emitted, error | synthetic `head_changed` events handed to the fan |
| `scp_catalog_react_total` | counter | `kind`, `result` = enqueued, hidden, ignored, unknown_repo, error | webhook events handled by `React` |
| `scp_catalog_membership_build_seconds` | histogram | - | duration of `BuildMembership` |
| `scp_catalog_store_errors_total` | counter | `op` = replace_catalogue, stats | store writes that failed |

Suggested alerts (documented for operators, not shipped): `scp_catalog_groups{state="degraded"} > 0` for 1h; `scp_catalog_reconcile_total{result="held"}` increasing; `time() - last success > 3 x reconcile_every` (from `/status`); `scp_catalog_repositories{state="quarantined"} > 0`.

### 9.2 Logs

`slog` JSON through the injected `*slog.Logger` (no global); every record from a job carries the engine's `job_key`, `job_kind`, and `trace_id` via `logx`. Catalog adds `source`, `repo_id`, `repo` (slug where known), `reason`, `old_slug`, `group`, `err_code`.

| Level | Message | Fields |
|---|---|---|
| INFO | `repository left the catalogue` | source, repo_id, repo (GitHub name), reason |
| INFO | `repository renamed` | source, repo_id, old_slug, slug |
| WARN | `catalogue row skipped` | err (Warm) |
| WARN | `stats not persisted` | repo, err |
| WARN | `head_changed not fully handled` | repo, err (a reactor failed) |
| ERROR | `catalogue not persisted` | source, err |
| ERROR | `hide failed` | repo, err |

R301/R302 are findings in `catalog.sync`, surfaced by `/status`, and logged once per transition by `ops`, not by this package on every pass. No log line contains a description, a topic or a URL from GitHub (untrusted text), nor any secret (none passes through this package).

### 9.3 Spans

`catalog.reconcile` (job), child `catalog.reconcile.source` (attribute `source`), `catalog.heads`. Listing pages and probes are spans of the adapter (`github.client` with `otelhttp`), so the trace of a reconcile shows the page calls underneath. Handlers are covered by `otelhttp` and `r.Pattern`.

---

## 10. Security

### 10.1 Trust boundaries

Trusted: the configuration file (written by the operator, but still validated and bounded), the snapshot, the store file (but its bytes are re-validated on load). **Untrusted**: everything the listing returns (names, descriptions, topics, URLs, branch names, visibility) because anyone with admin on one repository of the organisation controls those fields; the webhook payload (authenticated by HMAC but a hint only, 4.8); a repository list passed to `scp config lint --repos`.

### 10.2 Threats and controls

| Threat | Control | Test |
|---|---|---|
| A private repository becomes visible (a policy bug, an adapter that forgets visibility) | `Admit` fails closed on unknown visibility; `public_only` admits only an explicit `VisPublic`; visibility removal bypasses confirmations and the hold; handlers filter by `Authorizer.CanView` through `ClassOf`; the kernel's zero `scm.Visibility` is `VisUnknown`, which no policy admits, so an adapter that leaves the field unset is excluded, not published (V-14; D-8 is resolved by the kernel, `github` keeps a contract test that every listed repository sets it) | `TestAdmitFailsClosed` (includes the zero value), `TestPlanVisibilityIsImmediateAndUnguarded`, `TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment`, property test (no non-public repository in a `public_only` catalogue) |
| A repository that just went private stays visible until the next listing | `React` hides (or, under `installation_scope`, demotes) it in the same `Apply` as the event (4.8); neither an older in-flight listing nor a commit that planned before the event can resurrect it (`pubMu`, `hideGen`, `hiddenAt`) | `TestServiceReactHidesPrivatizedAtOnce`, `TestServiceHiddenRepositoryIsNotResurrectedByAnOlderListing`, `TestVerifyHideDuringCommitIsNotUndone`, `TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved` |
| A private repository persisted under a looser policy is served after the policy was tightened and the pod restarted | `Warm` re-applies `Admit` to every row (V-20) | `TestVerifyWarmAppliesTheCurrentVisibilityPolicy` |
| Forged or replayed event removes repositories | HMAC verified in `webhook` before `React`; `hide` is reversible by the next listing; `unknownRepo` is rate-limited to one reconcile per 5 minutes so event floods cannot force listings | react tests |
| A wrong listing empties the portal (GR-26) | complete-listing rule, confirmations, mass-removal guard with hold | `TestPlanMassRemovalHeldThenReleased`, `TestPlanEverythingVanishingIsHeld` |
| Regular-expression denial of service by the configuration | RE2 only (linear); operand 256 bytes, program 1,000 instructions, 64 matchers per group, 500 groups; glob translated to RE2 with at most 6 stars; anchor wrapper cannot escape | `TestMatcherRejects`, `FuzzMatcherCompile` |
| Hostile repository name or owner (path traversal, header or markup injection, reserved routes) | GitHub grammar enforced at ingestion by the kernel's `RepoRef.Validate` (V-17), canonical lower-case slug of the kernel (`scm.ParseSlug`, `[a-z0-9._-]`, 100 bytes), `.`/`..` refused, reserved words and document suffixes handled, the same checks re-run on every stored row at warm start | `TestMakeSlug`, `FuzzMakeSlug`, `FuzzNormaliseListedAgainstKernel`, `TestNormaliseListed` |
| Hostile free text (description, topics, URL, branch) | stored and served as plain text only: control and bidirectional characters removed, 1,000 bytes, at most 20 topics matching GitHub's grammar, URL must be https without credentials and at most 2,000 bytes, branch must be a legal ref; no HTML anywhere in this package | `TestNormaliseListed` |
| Configuration text rendered as HTML | `display_name` and `description` are plain text with control characters refused (C012, C013); the SPA renders text nodes; `integrations` prefixes are bounded and control-free | `TestValidateRules` |
| Slug collision lets one repository shadow another | stable owner order; losers quarantined, never silently dropped or renamed | `TestPlanSlugCollisionAndReserved`, `TestPlanSlugWinnerIsStable` |
| Group id confusion (case, alias reuse) | ids lower-case only (and slugs lower-case, V-01); ids and aliases unique together case-insensitively; aliases never shadow a real id | `TestValidateRules` |
| Private data in `catalog.sync` or in `catalog.aliases` reaching the public API | `Status` is admin class (documented, bridged only to `ops`); `catalog.aliases` also lists aliases of non-public repositories under `installation_scope`: **the consumer must check the target's visibility before redirecting** (`overview`, 14 section 4.8) and `catalog` never serves it itself | enumeration test (14) |
| Side channel through the global snapshot version / ETag (14 requirement R-1) | not solvable inside `catalog`; the dependency is `Snapshot.ClassRev` in `readmodel` | pinned in the enumeration test |
| Secrets | none handled: the package takes no token, key or URL with credentials (`cleanURL` rejects `user:pass@` URLs and every non-https URL from GitHub) | - |

### 10.3 Visibility rules the package guarantees to its consumers

1. `catalog.repo` contains only repositories the source's `DiscoveryPolicy` admits. Under `public_only` every entry is `VisPublic`; under `installation_scope` entries may be `VisInternal` or `VisPrivate` and **every reader must apply `authz.LookupRepo` or `Authorizer.CanView`**.
2. A group, a count or a default repository is never derived from a repository the viewer cannot see (7.2): `Slots.Groups`/`Slots.Group` read only through the class-aware `scm.Membership`.
3. Startup refuses `installation_scope` without access control (C080), `public_only` on a host without public repositories (C081) and non-GitHub.com `installation_scope` without an acknowledgement (C082).

---

## 11. Guardrail and decision trace

`GR-` design guardrail (`02-design-guardrails.md`), `Q-`/`D-`/`A-` decision records, `ADR-` architecture decisions, `W` architecture-review items. "Where" points into this document.

| Item | Decision here | Where | Test |
|---|---|---|---|
| GR-01 one composition root, explicit startup | `Warm` and the first reconcile are explicit startup steps of `cmd/scp`; `New` does no I/O | 2.4, 8.3 | lifecycle test |
| GR-02 readiness gated on data | readiness is false until the first live listing or a warm catalogue younger than `warm_max_age`; `catalogue` gate from `catalog.sync` and `WarmInfo.Newest` | 4.11, 7.4 | `TestServiceWarmRoundTrip` |
| GR-05 code tells the truth | the readiness gate and `/status` read `catalog.sync` (`listed`, `tracked`, `LastSuccess`) and `Slots.Coverage`; nothing renders a time that is not a data time | 7.4 | `TestServiceReconcileLifecycle`, `TestVerifyStatusShowsFailureAndListedCount` |
| GR-07 every entity has its own identity | `RepoKey`, `Tracked`, slugs are comparable values; entry ids and job keys use the **folded** `KeyParts`; sorted views are not identity | 3.1, 4.5 | `TestPlanInvariants`, `TestVerifyStatsKeyIsFoldedLikeThePlannersKey`, `TestVerifyRawCaseKeyIsRejectedByTheRealStore` |
| GR-08 deterministic, order-independent | explicit immutable `Membership` in configuration order; `ref` first in config order, integrations merged by `integrations`; the plan is independent of listing order | 4.1, 4.4, 4.6 | `TestMembershipIsOrderIndependent`, `TestPlanIsOrderIndependent` |
| GR-09 typed, anchored matchers | six typed matchers, whole-name, RE2, bounded, fuzzed | 4.1 | `TestMatcherTable`, `TestMatcherRejects`, `FuzzMatcherCompile` |
| GR-10 refs resolved from the repository | the effective ref is in `catalog.repo.DefaultBranch`; `ref` override per group, `W103`, `R207` (D-1) | 3.4, 4.6 | `TestMembershipManyToManyDefaultsAndRefs` |
| GR-11 domain, wire and view types are distinct | plain `Tracked`/`Config` types, API DTOs in `catalogapi`, explicit JSON tags in the store codec | 3, 5.2 | `TestCodecRoundTrip` |
| GR-12 one typed, strict, validated configuration | typed YAML block, strict decode, obsolete-form unmarshaler for matchers, JSON Schema, documented keys == consumed keys, every shipped example linted in CI | 3.2, 4.9, 6.4 | `TestStrictDecodeRejects`, `TestSchemaAgreesWithDecoder`, T-2, T-3 |
| GR-14 neutral defaults | no organisation, host or prefix in the package; group ids, titles, descriptions and integration prefixes are configuration; discovery is per `github.sources[]` entry | 6 | review |
| GR-16 truthful HTTP responses | an unknown group, a group with no visible member and an alias answer the same real `404` | 7.3 | T-11 |
| GR-21 webhook events are hints | `React` turns events into coalesced reconcile and `catalog.stats` jobs; `hide` is reversible by the next listing; `create`/`release` enqueue `catalog.stats` | 4.8 | `TestServiceReactHidesPrivatizedAtOnce`, T-10 |
| GR-25 upstream requests are budgeted | opaque cursor with per-cursor `ETag`, `304` page reuse, `ceil(n/100)` calls per reconcile, 20 probes of 50 per 5 minutes | 4.4, 4.5 | `TestServiceReconcileLifecycle` (304 pass), T-15 |
| GR-26 freshness is data; reconciliation is authoritative | paginated listing, complete-listing rule, confirmations, mass-removal guard with hold; `Info.TTL` on every value, refreshed by every complete listing (half-TTL rule); a failed listing keeps the last good data and publishes the failure in `catalog.sync` | 4.4, 5.1, 8.4 | `TestServiceListFailureChangesNothing`, `TestPlanMassRemovalHeldThenReleased`, `TestPlanEverythingVanishingIsHeld`, `TestVerifyRepoFreshnessIsRefreshedByEveryCompleteListing` |
| GR-28 immutable snapshot with write-time indexes | `groupsOf` and `members` are built once per commit inside `scm.Membership`; handlers do map lookups only | 4.6, 7.2 | `BenchmarkBuildMembership` |
| GR-29 absence is a typed state | an unresolved default is `GroupHealth.DefaultReason`, `defaultRepoState: missing` and a degraded card; nothing is guessed or silently skipped | 4.6, 4.7, 7.2 | `TestValidateRules`, lint golden |
| GR-31 restart is warm | `Warm` publishes the persisted catalogue in snapshot version 1 and re-applies the current visibility policy | 4.11 | `TestServiceWarmRoundTrip`, `TestVerifyWarmAppliesTheCurrentVisibilityPolicy` |
| GR-35 explicit access policy, minimal personal data | `public_only` default, `installation_scope` needs access control (C080), fail-closed admission, immediate removal and demotion on events, class-aware membership; no e-mails or authors stored | 4.3, 4.8, 7.2, 10 | `TestDiscoveryPolicyValidate`, `TestAdmitFailsClosed`, `TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment` |
| GR-43 tests assert and run offline | example fixtures, `fakeLister`/`fakeProber`, T-9 against `cmd/fakegithub` | 12 | `TestExampleCatalogBuildsMembership` |
| Q-07 many-to-many, typed matchers | the whole unit | 4.1, 4.9 | |
| Q-08 default branch unless `ref` | D-1 | 3.4 | |
| Q-09 1,000 repositories | budget 4.4 (listing) and 4.5 (sweep: 20 probes of 50 per 5 minutes) | 4.4, 12.7 | benchmarks |
| ADR-023 identity, slug, aliases, visibility | 4.2, 4.3 (D-3 refines "duplicates are a startup error") | 4.2, 4.3 | |
| W17 slug uniqueness and renames at outline level only | detailed here | 4.2 | `TestPlanAddRenameAlias`, `TestPlanSlugCollisionAndReserved` |
| A-22 `Put` cannot resurrect | stats `Put` tolerates `ErrUnknownRepo`; removals precede additions in one `Apply` | 4.5 | `TestServiceHeadsSweep` |

---

## 12. Test plan

Rules (D-10, ADR-016): no network, no wall clock, no sleeping for real, `-race` everywhere, golden files for generated text, fuzzing for every parser of untrusted text, hand-written fakes named for behaviour (`fakeLister`, `fakeProber`, `memStore`, `recTracker`, `recEvents`, `recEnq`), no mocking framework. Time-dependent tests run inside `testing/synctest` bubbles (GA since Go 1.25; the prototype uses `synctest.Test` with `time.Sleep` advancing virtual time between listings, so 31 minutes of reconcile cadence cost microseconds).

### 12.1 What exists in the prototype (all passing, `-race`, 80.3% statement coverage of `internal/catalog`)

50 test functions (46 in `internal/catalog`, 4 in `internal/crosscheck`), 3 fuzz targets, 2 benchmarks, run on 2026-10-14 with go1.27.1 **against the real kernel, readmodel and store packages** (section 16).

| Area | Tests |
|---|---|
| Matchers | `TestMatcherTable` (21 cases incl. the substring traps `ACS`/`ACSSearch`, `IoT`/`_non_maintained_IoT-Service`), `TestMatcherRejects` (12 invalid operands incl. `(a{1000}){1000}`, `a)\|(b`, `a**b**c**d**e**f**g**h`), `TestAnchorWrapperNeverEscapes`, `FuzzMatcherCompile` |
| Slugs | `TestMakeSlug` (accepted incl. `.github`, `acme.github.io`, `notes.md` -> `notes-md`, `Acme-API` -> `acme-api`; rejected incl. `too_long` at 101 bytes), `FuzzMakeSlug` (never a slug the kernel's `scm.ParseSlug` refuses; deterministic) |
| Plan (pure) | `TestPlanAddRenameAlias`, `TestPlanRemovalNeedsTwoPasses`, `TestPlanGuardFloorLetsASmallPortalLoseOne`, `TestPlanMassRemovalHeldThenReleased`, `TestPlanEverythingVanishingIsHeld`, `TestPlanVisibilityIsImmediateAndUnguarded`, `TestPlanForksAndArchived`, `TestPlanSlugCollisionAndReserved`, `TestPlanSlugWinnerIsStable`, `TestPlanIsOrderIndependent`, `TestPlanUngroupedIgnore`, `TestPlanDuplicateAndInvalidListEntries`, `TestNormaliseListed`, `FuzzNormaliseListedAgainstKernel` (everything accepted passes `RepoRef.Validate` and `job.NewKey`) |
| Plan (property) | `TestPlanInvariants`: 20 seeds x 200 passes of random worlds (renames, transfers, deletions, visibility flips, duplicate listing entries): no duplicate slug, no non-public repository in a `public_only` catalogue, no repository both removed and kept, aliases bounded and never the current slug, plan independent of listing order |
| Membership and config | `TestMembershipManyToManyDefaultsAndRefs`, `TestMembershipIsOrderIndependent`, `TestValidateRules` (15 cases + duplicate ids and aliases), `TestDiscoveryPolicyValidate` (C080-C082), `TestAdmitFailsClosed`, `TestCodecRoundTrip`, `TestFindingsSortNumerically` |
| Service (synctest, fake GitHub) | `TestServiceReconcileLifecycle` (cold start over two pages, not-due no-op, 304 pass, rename with alias and default loss), `TestServiceListFailureChangesNothing`, `TestServiceHeadsSweep`, `TestServiceReactHidesPrivatizedAtOnce`, `TestServiceEventForcesListingBeforeReconcileEvery`, `TestServiceAbsenceIsConfirmedByAFollowUp`, `TestServiceRenameCarriesStatsWithTheirOwnFreshness`, `TestServiceHiddenRepositoryIsNotResurrectedByAnOlderListing`, `TestServiceWarmRoundTrip`, `TestServiceConcurrentUse` (reconcile, sweeps, `React` and readers concurrently under `-race`) |
| Verification regressions (section 16; each failed on the first port and is a permanent test) | `TestVerifyRawCaseKeyIsRejectedByTheRealStore`, `TestVerifyMixedCaseOrganisationPersistsAndWarms`, `TestVerifyHideDuringCommitIsNotUndone`, `TestVerifyRepoFreshnessIsRefreshedByEveryCompleteListing`, `TestVerifyHideFindsRenamedRepositoryByID`, `TestVerifyStatusShowsFailureAndListedCount`, `TestVerifyStatsKeyIsFoldedLikeThePlannersKey`, `TestVerifyHeldRemovalDoesNotPollEveryTwoMinutes`, `TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved`, `TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment` (T-11 at card level), `TestVerifyWarmAppliesTheCurrentVisibilityPolicy`; the service tests run a `memStore` that applies the real `store.ValidateRepo`/`ValidateItem` rules |
| Cross-package (`cmd/scp` in the real repo, `internal/crosscheck` here) | `TestStrictDecodeRejects` (8 malformed documents), `TestExampleCatalogBuildsMembership` (strict decode, compile and the real kernel membership of the example catalogue), `TestExampleCatalogLintGolden` (4.10), `TestSchemaAgreesWithDecoder` |
| Benchmarks | `BenchmarkBuildMembership` (1,000 repositories x 50 groups x 4 matchers, now through the real `scm.NewMembership`): 23 ms, 12.1 MB, 17.5k allocs per build; `BenchmarkPlanReconcile1000`: 1.7-3.2 ms, 4.0 MB (Apple M4 Max, 2026-10-14; V-21: the benchmark's generator produced duplicate slugs that the stub accepted and the kernel rejects) |

### 12.2 What the implementation adds (required before the unit is done)

| ID | Test | Layer |
|---|---|---|
| T-1 | **Golden files** for `Lint`: the example catalogue against the example repository list (the output printed in 4.10, compared byte for byte) and every shipped example configuration against the same list; `go test -update` regenerates; a CI step fails when the committed `testdata/golden/*` differ from a fresh run | golden |
| T-2 | Every example configuration shipped in the repository decodes strictly, has no `Validate` error (except the deliberate ones of the lint example) and validates against `catalog.schema.json` (CI) | unit |
| T-3 | `TestSchemaKeysEqualStructTags`: every yaml tag of `Config`, `GroupConfig`, `HeadsConfig`, `GroupIntegrations`, `DiscoveryPolicy` is a property in `catalog.schema.json` and vice versa; the `discovery` definition equals the struct | unit |
| T-4 | `TestEveryReferencedCodeIsDocumented`: every `Cnnn/Wnnn/Rnnn` code that `errf`/`warnf` can emit appears in the generated reference and in this document's tables | unit |
| T-5 | `FuzzConfigDecode`: arbitrary bytes into the strict decoder, then `Validate` and `Compile`: never panics, never loops, bounded time | fuzz |
| T-6 | `FuzzNormaliseListed` and `FuzzDecodeRow`: no output violates the GitHub grammar; decode of arbitrary `Meta` never panics | fuzz |
| T-7 | `FuzzLint`: a decoded arbitrary configuration and a random repository list into `Lint`: never panics, findings are sorted and identical for a shuffled repository list | fuzz |
| T-8 | Real `sqlite` and `memory` store through `storetest`: `ReplaceCatalogue` rename-by-id keeps `catalog.stats` items (03 section 4.5); a removed repository cascades; `LoadItems` callback never calls the store | integration |
| T-9 | Integration against `cmd/fakegithub` (httptest, fault injection): paginated `/installation/repositories` with a repository created mid-listing (pagination shift), a 304 on every page, a 403 rate limit on page 3, a 5xx on page 2, a token rotation (ETag mismatch), an empty first page; assert the catalogue is never wrongly emptied and that the call count matches the budget in 4.4 | integration |
| T-10 | `TestEveryEventKindIsHandledOrCountedIgnored` for `catalog.React` (all `scm.EventKind` values and all repository actions in the verified list: archived, created, deleted, edited, privatized, publicized, renamed, transferred, unarchived) | unit |
| T-11 | Count-level **anonymous enumeration** (owned by 14, rows implemented here): catalogue of 5 public + 3 private repositories, one private repository inside a group of public ones, one group whose members are all private, one renamed private repository whose old slug is an alias; the responses of `api.groups` and `api.group` for an anonymous principal are byte-identical to those of a deployment that tracks only the 5 public repositories (same counts, same omitted group, same 404 body for the private-only group) | e2e |
| T-12 | Import-boundary test (`archtest`) rows: `catalog` imports no YAML or JSON-Schema library | arch |
| T-13 | `goleak` in `TestMain` (the package starts no goroutine; the assertion keeps it that way) | unit |
| T-14 | `-race` stress of the full handler path: 8 reader goroutines on `ListGroups` while reconcile, sweeps and events run (extends `TestServiceConcurrentUse` once handlers exist) | race |
| T-15 | Budget test with the fake GitHub: 1,000 repositories, one hour of virtual time (`synctest`): listing calls <= 2 x 10 pages x 2 passes plus confirmations, probe calls = 20 per 5 minutes, no more | integration |
| T-17 | **ETag and freshness** for `api.groups`/`api.group` (V-15): with the snapshot version fixed and the clock advanced across a card's `fresh` -> `stale` boundary the ETag changes and a conditional request does not get a `304`; with every card fresh the ETag equals the plain form; a malformed `limit`/`cursor` is a `400` before any `304` | unit (handler) |
| T-18 | Anonymous enumeration over the **real** `Slots.Groups`/`Slots.Group` for every `ViewClass` pair, property form: for random catalogues the public-class cards equal the cards of the catalogue restricted to public repositories (`TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment` is the fixed instance) | property |
| T-16 | A hand-recorded real `GET /installation/repositories` page and `repository` webhook deliveries (`privatized`, `renamed`, `deleted`, `transferred`) as `testdata/fixtures/*` for the adapter-facing assumptions: field names of the listing used by the decoder in `github` (owner of that test: 05) and the action list used here | fixture |

### 12.3 Determinism

Pure functions (`PlanReconcile`, `BuildMembership`, `Lint`, `Validate`) take no clock and no randomness; tests pass `Now`. Effectful code reads `time.Now()` only inside `synctest` bubbles in tests. `Deps.Seed` only feeds jitter helpers (none in the prototype's reconcile: the engine applies the job jitter). Map iteration order never reaches output: every map is converted through a sorted key list (`sortedKeys`), asserted by order-independence tests with shuffled input.

### 12.4 Golden-file procedure

`testdata/golden/<example>.lint.txt` files are produced by `go test ./internal/catalog -update` (a flag handled in the test), reviewed in the pull request and never edited by hand. Inputs are the example configurations and `testdata/fixtures/example-repos.json` (the prototype's `fixtures/` and `out/`). Changing a rule changes the goldens, which is exactly the review signal wanted.

### 12.5 Cross-package agreement tests

`TestSchemaAgreesWithDecoder` and `TestExampleCatalogBuildsMembership` are the contract between the hand-written schema, the strict decoder of `cmd/scp` and the real kernel membership (6.4): a schema that drifts from the Go struct tags, or a document the decoder accepts and the schema rejects, fails CI.

### 12.6 Mutation checks run on the prototype

To show that the tests detect what they claim to, six one-line mutations were applied to a copy of the prototype on 2026-10-14 and each made the named test fail: removing the confirmation condition (`Misses >= RemovalConfirmations`) fails `TestPlanRemovalNeedsTwoPasses`; moving the visibility removal behind the guard fails `TestPlanVisibilityIsImmediateAndUnguarded`; deleting the pre-parse in `compileAnchored` fails `TestMatcherRejects` (`a)|(b` would compile); making `commit` ignore `hiddenAt` fails `TestServiceHiddenRepositoryIsNotResurrectedByAnOlderListing`; dropping the `wanted` check in `due` fails `TestServiceEventForcesListingBeforeReconcileEvery`; and setting the guard floor to 0 originally **survived** and led to `TestPlanGuardFloorLetsASmallPortalLoseOne`, which now kills it. Two further mutations were applied during verification (section 16): removing the `hideGen` replan in `commit`, and removing `pubMu` from `hide`; each made `TestVerifyHideDuringCommitIsNotUndone` and `TestServiceConcurrentUse` fail (38 candidate snapshots rejected or overwritten in the stress run). The implementation repeats the exercise in CI as an optional nightly mutation job (`go-mutesting` or `gremlins`, not a merge gate).

### 12.7 Performance targets and measured values

| Operation | Target | Measured (prototype, M4 Max) |
|---|---|---|
| `BuildMembership`, 1,000 repositories x 50 groups x 4 matchers | < 50 ms | 23 ms |
| `PlanReconcile`, 1,000 repositories, nothing changed | < 10 ms | 1.7-3.2 ms |
| `ListGroups` handler, 50 groups x 1,000 repositories, filtering by class | < 1 ms | not built; `Slots.Groups` (the reader the handler calls) is built and tested but not yet benchmarked (benchmark T-14) |
| memory of the catalogue's own state, 1,000 repositories | < 5 MB beyond the snapshot | not measured; `Tracked` is about 600 bytes plus strings |
| per-sweep upstream cost, 1,000 repositories | 20 probes per 5 minutes | by construction (`batch_size` 50) |

### 12.8 Verification log

Facts checked against the live web or the module source on **2026-10-14**:

| Fact | Source | Result |
|---|---|---|
| `GET /installation/repositories`: `per_page` default 30, max 100; response `total_count`, `repositories`, `repository_selection`; paged with `per_page`/`page` | docs.github.com REST apps/installations | confirmed |
| `repository` webhook actions: archived, created, deleted, edited, privatized, publicized, renamed, transferred, unarchived; `installation_repositories`: added, removed; `installation`: created, deleted, new_permissions_accepted, suspend, unsuspend | docs.github.com webhook events and payloads | confirmed; the reaction table of 4.8 covers every one |
| a conditional request answered `304` with a correct `Authorization` header does not count against the primary rate limit; "should not try to manually construct pagination queries"; use the `Link` headers | docs.github.com best practices for the REST API | confirmed (drives the opaque cursor and the 304 reuse) |
| GraphQL: at most 500,000 nodes per call, `first`/`last` 1-100 per connection, cost = requests/100 rounded, minimum 1 point; 5,000 points/hour for users and PATs, App installations 5,000 rising to 12,500; **GHEC installations 10,000** | docs.github.com GraphQL rate and node limits | confirmed; note: the architecture (A-04) says "GHEC 15,000/h" for REST installations, the GraphQL page says 10,000 points: a probe batch of 50 repositories costs 1 point either way, so nothing here depends on it; flagged for unit 05 |
| repository names: ASCII letters, digits, `.`, `-`, `_`, at most 100 characters | docs.github.com creating a new repository | confirmed (drives `nameRe`, `slugRe`, `MaxSlugBytes` 100 + 32) |
| topics: lowercase letters, digits and hyphens, at most 50 characters, at most 20 per repository | docs.github.com classifying your repository with topics | confirmed (drives `topicRe`, `maxTopics`) |
| Go `regexp`: RE2 syntax; `regexp/syntax` `Perl` flags = `ClassNL \| OneLine \| PerlX \| UnicodeGroups`; counted repetition above 1,000 is rejected; `ErrNestingDepth` exists | pkg.go.dev/regexp/syntax | confirmed. The linear-time guarantee is stated on pkg.go.dev/regexp, not on the `regexp/syntax` page the fetch returned; it is also what the 1.29 M-execution fuzz run and `TestMatcherRejects` rely on |
| `go.yaml.in/yaml/v3` v3.0.5 (2026-07-26) is the latest v3; v4 exists only as `v4.0.0-rc.1..rc.5`; the repository is maintained by the YAML organisation; `Decoder.KnownFields` exists; the **obsolete** `UnmarshalYAML(unmarshal func(interface{}) error)` form is still supported (`obsoleteUnmarshaler` in `yaml.go:39`, `callObsoleteUnmarshaler` in `decode.go:377`, test types in `decode_test.go`; pkg.go.dev documents only the new signature) | proxy.golang.org, pkg.go.dev, module source | confirmed in source; pinned `v3.0.5` |
| `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 (2026-06-28); `go.opentelemetry.io/otel/{metric,trace}` v1.47.0 (2026-10-02) | proxy.golang.org | confirmed; the first is a test-only dependency in the prototype (schema cross-check) and a `cmd/scp` dependency in the real repository |
| `testing/synctest.Test` available with go1.27.1 | local toolchain | used by 12 tests |
| facts of the real kernel (slug grammar, `NewMembership`, `VisUnknown`, `store.KeyOf`, `RepoRef.Validate`) | module source of units 01 and 03, compiled into the module | see section 16 |

### 12.9 What was compile-checked

A scratch module `github.com/cantara/source-code-portal` (go 1.27, toolchain go1.27.1 darwin/arm64) in `08-catalog.assets/mod/` contains: `internal/catalog` (the whole package except `Mount`, whose handlers are specified in 7), the **real** kernel and storage packages of units 01 and 03 (`scm`, `freshness`, `job`, `readmodel`, `store` with `memory`, `sqlite` and `storetest`, `reactor`, `routes`) plus `httpapi/mux.go` only, and `internal/crosscheck`. Compile-checked and vetted: every Go signature quoted in sections 2 and 3, the consumer interfaces, `Slots`/`RegisterSlots`, the three `job.Spec` values, the codecs, the metrics. `gofmt -l` clean, `go vet ./...` clean, `go test -race -count=1 ./...` green, 8 s `FuzzNormaliseListedAgainstKernel` (1.1 M executions), 5 s `FuzzMakeSlug` and 5 s `FuzzMatcherCompile` clean on the final code. **Not compile-checked**: the `catalogapi` strict-server glue and `Mount` (they need oapi-codegen output from the `httpapi`/`apispec` units), `cmd/scp` wiring (sketch in 2.4), and the real SQLite store under the catalogue service tests (the service tests use `memStore`, which applies the real validation rules; the real `sqlite` and `memory` stores are in the module and their own suites pass, T-8 runs the catalogue against them).

---

## 13. Implementation tasks (ordered, with dependencies)

Sizes: S under half a day, M about a day, L two to three days. Dependencies name other design units by their short key; "kernel" = `scm`, `freshness`, `job`, `routes`.

### M1. Pure core `[dep: kernel]`

| Task | Files | Size | Depends |
|---|---|---|---|
| C-1.1 `Finding`, `Severity`, `SortFindings` (numeric-aware paths), `HasErrors` | `finding.go` | S | - |
| C-1.2 `Matcher`, `MatcherKind`, `Compile`, `compileGlob`, `compileAnchored`, `MatcherError`, `UnmarshalYAML` (obsolete form); `matcher_test.go`, `FuzzMatcherCompile` | `matcher.go` | M | C-1.1 |
| C-1.3 `MakeSlug` (lower-case, `scm.ParseSlug`), `FoldSlug`, `ValidGroupID`; `slug_test.go`, `FuzzMakeSlug` | `slug.go` | S | - |
| C-1.4 `normaliseListed`, `cleanText`, `cleanURL`; `TestNormaliseListed`, fuzz T-6 | `normalise.go` | S | C-1.3 |
| C-1.5 `Config`, `GroupConfig`, `DefaultConfig`, `Validate` (C002-C090, W101-W108), `GroupConfig.Meta` | `config.go` | M | C-1.2, C-1.3 |
| C-1.6 `DiscoveryPolicy`, `Admit`, `Validate` (C080-C086); tests | `visibility.go` | S | C-1.4 |
| C-1.7 `PlanReconcile` (R1-R7), `pushAlias`, `fingerprint`, tests, property test | `plan.go` | L | C-1.3, C-1.4, C-1.6 |
| C-1.8 `Compile`, `BuildMembership`, `GroupHealth`, `MembershipResult`; tests, benchmark. **Uses the kernel's `scm.NewMembership(MembershipSpec)`** (V-13) | `membership.go` | M | C-1.5, kernel |
| C-1.9 `Lint` (R201-R209) | `lint.go` | M | C-1.8 |
| C-1.10 JSON Schema, `//go:embed`, T-3 | `catalog.schema.json`, `schema.go` | S | C-1.5, C-1.6 |

### M2. Lint tooling and CLI `[dep: M1, platform/config]`

| Task | Files | Size | Depends |
|---|---|---|---|
| C-2.1 Golden files T-1, T-2 and the cross-checks of 12.5 in `cmd/scp` | `testdata/**`, `cmd/scp/config_catalog_test.go` | M | C-1.9, C-1.10, `platform/config` |
| C-2.2 CLI `scp config lint` (with `--repos`) and `scp config schema` | `cmd/scp` | M | C-1.9, C-1.10 |

### M3. Effectful service `[dep: readmodel, store, job, github (listing/probe), refresh (Tracker bridge)]`

| Task | Files | Size | Depends |
|---|---|---|---|
| C-3.1 `interfaces.go`: consumer interfaces, slot names, `Slots`, `RegisterSlots`, `Status`; compile assertions in `cmd/scp/assert.go` | `interfaces.go` | S | readmodel |
| C-3.2 `codec.go` (`repoMeta`, `statsPayload`), round-trip and hostile-row tests | `codec.go` | S | store |
| C-3.3 `Service`, `New`, `Jobs`, `metrics.go` | `service.go`, `metrics.go` | M | C-3.1 |
| C-3.4 `listSource`, `runReconcile`, `commit` (with the `pubMu`/`hideGen` rules of 8.2), `publish`, `publishStatus`, aliases, status, gauges | `reconcile.go` | L | C-1.7, C-1.8, C-3.2, C-3.3 |
| C-3.5 `Warm` (re-applies `Admit`), `Validator`, `Coverage`; `groups.go` (`Slots.Groups`, `Slots.Group`) | `warm.go`, `groups.go` | M | C-3.4 |
| C-3.6 `runHeads`, `runStats`, `applyStats`, `DiffHeads`, `HeadEvent` | `heads.go` | M | C-3.4, `reactor` |
| C-3.7 `React`, `hide`, `unknownRepo`, `want`; T-10 | `react.go` | M | C-3.4 |
| C-3.8 Service tests with synctest (12.1), T-8 (real stores), T-9 (fake GitHub), T-13, T-15 | `*_test.go` | L | C-3.4..C-3.7, `cmd/fakegithub`, `storetest` |

### M4. API and wiring `[dep: httpapi, httpapi/authz, apispec/catalogapi, routes, overview (contract), cmd/scp]`

| Task | Files | Size | Depends |
|---|---|---|---|
| C-4.1 `catalogapi` fragment generated and committed; `Mount` | `catalogapi/`, `handler.go` | M | `apispec`, `httpapi` |
| C-4.2 `ListGroups`, `GetGroup` per 7.2, 7.3; T-11, T-14 | `handler.go` | M | C-4.1, C-3.5 |
| C-4.3 `cmd/scp` wiring (2.4), `bridge.Tracker`, root `Validate()` calling `Config.Validate` and `DiscoveryPolicy.Validate`, `integrations.ValidateGroupDefaults` | `cmd/scp` | M | C-3.x, `integrations`, `refresh` |
| C-4.4 `scp doctor` catalog part (7.5) | `cmd/scp` | S | C-3.4 |
| C-4.5 `/status` bridge for `catalog.sync` (7.4) | `cmd/scp/internal/bridge` | S | `ops` |

### M5. Hardening

| Task | Size | Depends |
|---|---|---|
| C-5.1 hand-recorded real listing and webhook deliveries as fixtures (T-16) | S | `github` |
| C-5.2 chaos run with the fake GitHub's fault injector: random 5xx, 304, rate limits, mid-listing creates and deletes over 24 virtual hours; invariants of `TestPlanInvariants` asserted on the published snapshot | M | C-3.8 |
| C-5.3 readiness gate wiring and `/status` rendering reviewed with `ops` | S | `ops` |

Cross-unit dependencies (what other units must deliver or know):

| Unit | Needs from `catalog` | `catalog` needs from it |
|---|---|---|
| `scm` kernel | - | `NewMembership(MembershipSpec)` (error return), class-aware `Membership`, `VisUnknown` zero value, `ParseSlug`, `RepoRef.Validate`/`KeyParts` (all present, V-13, V-14) |
| `readmodel` | slots, validators | `Builder.View()` (already in 8.4), `Existence()`, `ErrUnknownRepo`; optionally `ClassRev` (14 R-1) |
| `store` | `CatalogueStore` carve-out | `ReplaceCatalogue` rename-by-id (03 4.5) |
| `github` | `RepositoryLister`, `HeadProber` | opaque cursor, 100-per-page listing with per-cursor ETag, `Visibility` always set (an unset value is excluded, V-14), `ProbeHeads` contract in 2.2 |
| `refresh` | `job.Spec` x3, `Tracker` | `Planner.SetSubjects` per kind through `bridge.Tracker` |
| `reactor` | `React` | `Fan` implements `EventSink` |
| `webhook` | - | delivers `scm.Event` with `Source`, `Repo`, `Action`; the `resync` event (04 D10) |
| `integrations` | `GroupMeta.Integrations` keys `jenkins.{job_prefix,instance}`, `snyk.project_prefix` | `ValidateGroupDefaults(groups)` takes `[]scm.GroupMeta` built by `GroupConfig.Meta` |
| `overview` | `catalog.aliases` (308, visibility check), membership, `repos` and `group repos` endpoints | - |
| `badges` | membership counts, `catalog.stats` | group badge URLs from `routes.BadgeGroup` |
| `access` / `authz` | - | `ClassOf`, `Authorizer` |
| `ops` | `catalog.sync`, `Slots.Coverage` | readiness gate registration |
| `apispec` | `catalogapi` fragment | bundled spec including `listGroups`, `getGroup` |

---

## 14. Deviations proposed

Each item names the binding text it touches. None is applied silently; the prototype implements them so they can be judged.

| ID | Touches | Proposal | Evidence |
|---|---|---|---|
| D-1 | 10-architecture 8.1 (`scm.Repository.DefaultBranch` is GitHub's default branch), Q-08 | `catalog.repo`'s `DefaultBranch` carries the **effective tracked ref** (group `ref` override, else GitHub's default); `Tracked` keeps GitHub's. One value answers "which branch" for `commits`, `docs`, `manifests`; no feature recomputes it | GR-10; 3.4; `TestMembershipManyToManyDefaultsAndRefs` |
| D-2 | 10-architecture 8.1: `scm.Membership` lists methods only | **resolved by the kernel unit (V-13)**: `scm.NewMembership(MembershipSpec) (Membership, error)` exists, derives `groupsOf` and the per-class views, and is what `BuildMembership` calls; the proposed signature of the first draft is withdrawn | module source `scm/membership.go` |
| D-3 | ADR-023 "duplicates are a startup error" | a slug collision caused by a repository created on GitHub **after** startup quarantines the loser (not tracked, finding `R302`, gauge) instead of failing the process; `scp config lint` and `scp doctor` still report it as an error (`R206`) for the repositories known at that time, and startup validation of the configured prefixes stays an error | a new GitHub repository must not take the portal down; 4.2, `TestPlanSlugCollisionAndReserved` |
| D-4 | 10-architecture 9.1 slot and global list | two globals added: `catalog.group_aliases` (earlier group ids) and `catalog.sync` (`Status`, admin class); (the earlier clause "`catalog.aliases` also carries the lower-case form of mixed-case slugs" is withdrawn: slugs are lower case, V-01) | needed by `overview`'s group-id redirect and by `/status`; 4.2, 5.1 |
| D-5 | 10-architecture 16 config example (`discovery`) | two keys added to `github.sources[].discovery`: `removal_confirmations` (default 2) and `mass_removal_hold` (default 6h); `access_acknowledgement` is ADR-023's; `visibility`, `include_*`, `reconcile_every`, `max_removal_ratio` unchanged | the removal guard needs a confirmation count and a hold; 4.4 |
| D-6 | 10-architecture 8.13 route table owner of `api.group.repos` and `api.repos` | owned by `overview` (as the public OpenAPI, unit 07, already has them); `catalog` owns `api.groups` and `api.group` only | 7.1 |
| D-7 | 10-architecture 6.1 rule 5, 17.3 | **withdrawn (V-12)**: the kernel's `scm.Membership` is itself class-aware, so no deviation is needed; `catalog.membership` is one global value whose per-class accessors are the kernel's; the handlers read only `Slots.Groups`/`Slots.Group` | 7.2 |
| D-8 | 10-architecture 8.1 `scm.Visibility` | **resolved (V-14)**: the kernel now has `VisUnknown = 0`; `Admit` excludes it (`TestAdmitFailsClosed`); `github` keeps its contract test | 10.2 |
| D-9 | 10-architecture 8.1/8.3 | **obsolete**: the stubs were replaced by the real kernel packages (section 16). The first draft listed, as its assumptions, that: `job.Key.Parts` returns the parts after the kind; `readmodel.Builder.View()` is used to read the candidate inside `Apply` (it is in 8.4); `freshness.Info` has no `Kind`; none changes the architecture, listed so the implementer knows what was assumed | 12.9 |
| D-10 | 10-architecture 10.1 (`first catalog.reconcile` enqueued by `cmd/scp`; warm load at step 5, planner at step 6) | a warm start never postpones that first listing (`warmAt` is display only); a webhook-announced change lists the source even when `reconcile_every` has not elapsed (`wanted`); `Warm` does not call `Tracker`, `cmd/scp` passes `Service.Repos()` after the planner exists | 4.4, 4.11, 8.3 |
| D-11 | 10-architecture 8.6 job list and 9.6 TTL table | a third job kind `catalog.stats` (per repository, TTL 24h, accelerator for `release`/`create`) is added to the table; the `catalog.reconcile` TTL is `min(reconcile_every)` over sources with a per-source due check; after an unconfirmed absence the job returns `NextDue = now + 2 min` | 4.4, 4.5, 8.1 |
| D-12 | 10-architecture 4 file tree of `internal/catalog` | the prototype's files differ (1.4); package-level contract unchanged | 1.4 |
| D-15 | 10-architecture 16 config example (`default_repo`, type not stated) | `default_repo` is a GitHub repository **name** (case-insensitive), not a slug: the slug depends on `slug_prefix_by_source` and the case rule, so the name is the only reference an operator can write that survives a prefix change | V-11; 4.7, 6.1 |
| D-16 | 10-architecture 9.1 (who writes which global) | `catalog.sync` is also published by a **failed** listing and by `hide`, as a one-global `Apply` under `pubMu`, so a failure reaches `/status` and the readiness gate at once | V-06; 5.1, 8.4 |
| D-18 | 10-architecture 8.1 (the `Tracker` bridge) | `Tracker.SetRepos` is called while the catalogue's publish lock is held and must therefore not call back into `catalog` (the `cmd/scp` bridge only forwards to `planner.SetSubjects`); `hide` does not call it (the forced reconcile that follows does) | V-03; 2.2, 8.2 |

Clarifications (no change to binding text):

- **C-1** `getGroup.repos` is "in configuration order" in the OpenAPI; no per-repository configuration order exists; the deterministic reading is default repository first, then case-folded slug (7.3).
- **C-2** `catalog` never reads a repository descriptor; `RepoLink.title` is therefore omitted by `catalog` (optional in the schema) and `overview` may enrich the group card.
- **C-3** (corrected, V-18) The group id grammar is lower case only; `overview` lower-cases a requested id before the lookup, so only non-case changes of an id need an alias.

---

## 15. Open questions

Each has a proposed answer implemented in the prototype; none blocks M1-M3.

| ID | Question | Owner | Proposed |
|---|---|---|---|
| Q-1 | Is a group with no visible member omitted from `api.groups` for **every** principal, including in a public-only deployment where a configured group is simply empty (the `search` card of 4.10 would disappear)? 14 section 4.8 requires it for the public class; the alternative (show empty groups when no private repository exists) makes the response depend on the deployment's private repositories, which breaks the byte-identical rule | owner, units 07 and 14 | omit for everyone; surface in `/status` and lint |
| Q-2 | Should `discovery.include_forks` default to `true`? In forks-heavy organisations a fork is often a group's default repository, and `false` then degrades that group; the architecture default is `false` | owner | keep `false` (forks are noise in most organisations); the degraded group shows `R205` in `/status` and the fix is `include_forks: true` |
| Q-3 | Should `removal_confirmations` be 2 by default? Under 1 a pagination shift can drop a repository for 30 minutes; the follow-up listing is only 2 minutes later | owner | 2 |
| Q-4 | Should the mass-removal hold also apply to visibility removals under `installation_scope` when the credential's permission set shrinks (every private repository disappears from the listing but is not listed as private)? Today an absent repository is an absence, subject to the guard; a listed-but-private one is immediate | owner, `github` | keep; the credential change surfaces as `R301`, which is the correct signal |
| Q-5 | Should `readmodel.Builder` gain `Rename(old, new)` that carries the slots of other features across a slug change? Without it a rename refetches docs and commits under the new planner key | `readmodel` owner | not needed for a rename (the key changes anyway); worth it for a pure `slug_prefix` change only; defer |
| Q-6 | The OpenAPI example for `freshness` of a group is always `kind: catalog.stats`; this design uses `catalog.stats` of the default repository and falls back to `catalog.repo` | unit 07 | accept the fallback |
| Q-7 | Does `integrations.ValidateGroupDefaults` take `[]scm.GroupMeta`? (this design assumes so; `GroupConfig.Meta()` produces them) | unit 14 | yes |
| Q-8 | Should `catalog.aliases` be published as `scm.Classed` so that no consumer can forget the target-visibility check before a 308? | `overview`, `access` | no; the enumeration test pins it (T-11) |
| Q-9 | The mass-removal hold is in memory; should it persist so a crash-looping process cannot be tricked into applying a held removal sooner? A restart restarts the clock, so it can only be later | owner | no persistence |
| Q-10 | The per-repository `catalog.stats` job for non-archived repositories is a 24-hour drift bound behind a 5-minute sweep; is the extra call volume (1,000 per day) worth it? | owner | keep: it is the only recovery path if the sweep's GraphQL query silently omits a field |
| Q-11 | `installation_repositories.removed` could hide the removed repositories at once if `scm.Event` carried their ids; today only the next listing handles it (answers refresh question Q3) | `webhook` / kernel | extend `Event` with a bounded `Repos []RepoRef` in v1.1 if private repositories are common |

---

## Appendix A. `catalog.schema.json` (embedded, 82 lines)

The complete file is `08-catalog.assets/catalog.schema.json`; the structure is reproduced here for orientation (the file is authoritative).

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://scp.invalid/schemas/catalog-v1.json",
  "type": "object", "additionalProperties": false,
  "properties": {
    "ungrouped": { "enum": ["track", "ignore"], "default": "track" },
    "slug_prefix_by_source": { "type": "object", "propertyNames": { "pattern": "^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?$" },
                               "additionalProperties": { "type": "string", "pattern": "^[A-Za-z0-9._-]{0,32}$" } },
    "heads": { "type": "object", "additionalProperties": false, "properties": {
        "batch_size": { "type": "integer", "minimum": 1, "maximum": 100, "default": 50 },
        "interval": { "$ref": "#/$defs/duration", "default": "5m" },
        "archived_interval": { "$ref": "#/$defs/duration", "default": "24h" } } },
    "groups": { "type": "array", "maxItems": 500, "items": { "$ref": "#/$defs/group" } }
  },
  "$defs": {
    "duration": { "type": "string", "pattern": "^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$" },
    "group": { "type": "object", "additionalProperties": false, "required": ["id", "display_name", "default_repo", "match"],
      "properties": { "id": { "pattern": "^[a-z0-9]+(-[a-z0-9]+)*$", "maxLength": 63 }, "display_name": { "minLength": 1, "maxLength": 120 },
        "description": { "maxLength": 500 }, "default_repo": { "pattern": "^[A-Za-z0-9._-]{1,100}$" },
        "sources": { "type": "array", "uniqueItems": true }, "match": { "type": "array", "minItems": 1, "maxItems": 64, "items": { "$ref": "#/$defs/matcher" } },
        "ref": { "maxLength": 200, "pattern": "^[^\\s~^:?*\\[\\\\]+$" }, "aliases": { "type": "array", "maxItems": 16 },
        "integrations": { "properties": { "jenkins": { "properties": { "job_prefix": { "maxLength": 100 }, "instance": { "pattern": "^[A-Za-z0-9._-]{1,63}$" } } },
                                          "snyk": { "properties": { "project_prefix": { "maxLength": 100 } } } } } } },
    "matcher": { "type": "object", "minProperties": 1, "maxProperties": 1, "additionalProperties": false,
      "properties": { "exact": { "pattern": "^[A-Za-z0-9._-]{1,256}$" }, "prefix": { "pattern": "^[A-Za-z0-9._-]{1,256}$" },
        "glob": { "pattern": "^[A-Za-z0-9._*?-]{1,256}$" }, "regexp": { "minLength": 1, "maxLength": 256 },
        "topic": { "pattern": "^[A-Za-z0-9][A-Za-z0-9-]{0,49}$" }, "catch_all": { "const": true } } },
    "discovery": { "properties": { "visibility": { "enum": ["public_only", "installation_scope"] }, "include_archived": { "type": "boolean" },
        "include_forks": { "type": "boolean" }, "reconcile_every": { "$ref": "#/$defs/duration" }, "max_removal_ratio": { "exclusiveMinimum": 0, "maximum": 1 },
        "removal_confirmations": { "minimum": 1, "maximum": 10 }, "mass_removal_hold": { "$ref": "#/$defs/duration" },
        "access_acknowledgement": { "enum": ["", "network_restricted", "oidc_proxy", "trusted_proxy_headers"] } } }
  }
}
```

## Appendix B. Example `catalog:` block with every key

```yaml
catalog:
  ungrouped: track                       # track | ignore
  slug_prefix_by_source:
    acme: ""                             # per source
    "acme/archive-org": "archive-"       # per source and owner (file only)
  heads: { batch_size: 50, interval: 5m, archived_interval: 24h }
  groups:
    - id: widget
      display_name: Widget Platform
      description: Widget services, libraries and tooling
      default_repo: Widget-Core
      sources: [acme]                    # optional
      match:
        - prefix: Widget
        - glob: "*-widget-*"
        - regexp: '(?i)sso-.*'
        - topic: widget
        - exact: JAU
      ref: release                       # optional: tracked branch (W103: not covered by the head sweep)
      aliases: ["Widget Suite"]
      integrations:
        jenkins: { job_prefix: "widget-", instance: main }
        snyk: { project_prefix: "widget-" }
    - id: everything
      display_name: All repositories
      default_repo: PathWatcher
      match: [{ catch_all: true }]

github:
  sources:
    - id: acme
      discovery:                         # github owns mode/org; catalog owns the rest
        visibility: public_only
        include_archived: true
        include_forks: false
        reconcile_every: 30m
        max_removal_ratio: 0.3
        removal_confirmations: 2
        mass_removal_hold: 6h
        access_acknowledgement: ""
```

## Appendix C. Index of the companion assets

| Path under `08-catalog.assets/` | Content |
|---|---|
| `README.md` | how to run the prototype |
| `catalog.schema.json` | the embedded JSON Schema |
| `mod/internal/catalog/*.go` | the package (20 source files, including `groups.go`, and 7 test files, including `regress_test.go`) |
| `mod/internal/{scm,freshness,job,readmodel,reactor,routes,store,httpapi}` | the **real** kernel and storage packages of units 01 and 03 (copied from the verified build; `httpapi` is `mux.go` only; `store` includes `memory`, `sqlite` and `storetest`), not stubs |
| `mod/internal/crosscheck` | the cross-package tests |
| `mod/cmd/lintdemo` | lint runner (4.10) |
| `fixtures/example-repos.json` | the 16-repository example list |
| `fixtures/example.catalog.yaml` | the example catalogue of 4.10 |
| `out/example.lint.txt` | the golden lint output of 4.10 |

---

## 16. Verification log

Independent verification of 2026-10-14 (a second reader who did not write the unit; findings report: `30-verification/design-08-catalog.md`). Method: the prototype module was re-based from the unit's own kernel stubs onto the **real** kernel, readmodel and store packages of units 01 and 03 (copied from their verified builds), compiled and tested with go1.27.1 darwin/arm64 under `-race`, then attacked with regression tests written first (each failed on the first port, then was fixed), a new fuzz target against the kernel's own identity validation, and two mutation checks. Every item below is in this document already; the id is what the report and the tests quote.

### 16.1 Defects found and fixed (document and prototype)

| Id | Severity | Defect | Fixed in | Regression |
|---|---|---|---|---|
| V-01 | high | Slugs kept GitHub's case and allowed 132 bytes; the kernel's `scm.Slug` is `^[a-z0-9._-]{1,100}$` and `store.KeyOf` folds, so `Widget-Core` could not be a slug (design contradicted the kernel and the public API's `Slug` type) | 2.3, 3.1, 4.2, 11, 14 (D-4), App. A | `TestMakeSlug`, `FuzzMakeSlug` |
| V-02 | high | Store rows were keyed by the raw-case `RepoKey`; the real store rejects it, and `repoMeta` lacked owner and name so a restart could not rebuild the display case | 5.2 (codec), 4.11 | `TestVerifyRawCaseKeyIsRejectedByTheRealStore`, `TestVerifyMixedCaseOrganisationPersistsAndWarms` |
| V-03 | high | An event hiding a repository while a commit was writing the store was undone by the commit's older plan (and two `Apply`s could interleave): a private repository reappeared for a whole `reconcile_every` | 4.4 R8, 4.8, 8.2 (`pubMu`, `hideGen`), 10.2 | `TestVerifyHideDuringCommitIsNotUndone`; mutation: no `pubMu` or no replan fails it |
| V-04 | medium | `hide` looked the repository up by the event's name; a `privatized` delivery for a renamed, not yet reconciled repository was not found | 4.8 | `TestVerifyHideFindsRenamedRepositoryByID` |
| V-05 | high | `catalog.repo` entries were written once; their `Info.FetchedAt` never moved, so every unchanged repository was reported `stale` after 1.5 x TTL for the rest of the process | 5.1, guarantee 8 | `TestVerifyRepoFreshnessIsRefreshedByEveryCompleteListing` |
| V-06 | medium | A failed listing changed only memory; `/status` and the readiness gate kept showing the last success; the published `listed` count was 0 | 4.4, 5.1, 8.4, D-16 | `TestVerifyStatusShowsFailureAndListedCount` |
| V-07 | medium | `R301`/`R302` findings were rebuilt per commit, so a commit for source B erased the `R301` of held source A | 3.3, 4.4 | `TestServiceListFailureChangesNothing`, plan tests |
| V-08 | medium | A held mass removal re-listed every `confirmGap` (2 min) for the whole 6 h hold (75 listings, ~750 calls for 1,000 repositories) | 4.4 | `TestVerifyHeldRemovalDoesNotPollEveryTwoMinutes` |
| V-09 | medium | The `catalog.stats` job key built by `React` was not the folded key the planner derives, so an event-driven run never coalesced with the planned one | 4.5, 4.8 | `TestVerifyStatsKeyIsFoldedLikeThePlannersKey` |
| V-10 | high | Under `installation_scope`, a `privatized` event removed the repository for everyone; the intended behaviour (keep it, visible to `ViewAll` only) was documented nowhere | 4.8 | `TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved` |
| V-11 | medium | `default_repo` was specified as a slug; the slug depends on prefix and case rule, so a configured default broke whenever a prefix changed; it is now the repository **name** (case-insensitive) | 3.2, 4.6, 4.7, 4.9 (C032, R205), 6.1, App. A, D-15 | lint golden, `TestValidateRules` |
| V-12 | high | The design kept one class-blind membership and filtered in each handler (deviation D-7), although the real kernel membership is class-aware: the enumeration rule depended on every handler remembering the filter | 4.6, 7.2, 10.3, D-7 withdrawn; code `groups.go` | `TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment` |
| V-13 | low | The prototype's `scm.NewMembership(groups, members, defaults)` (D-2) did not exist; the kernel's is `NewMembership(MembershipSpec) (Membership, error)` | 3.3, 4.6, 13, D-2 | all membership tests |
| V-14 | medium | D-8 asked the kernel for a non-public zero visibility; the kernel has `VisUnknown`; the design still described the fail-open limitation | 4.3, 10.2, D-8 | `TestAdmitFailsClosed` |
| V-15 | medium | `listGroups`/`getGroup` froze the ETag on `(snapshot, class)` while the body contains freshness derived from the clock, so a client could be told `304` for a card that had turned stale; the 400 for a bad `limit` could be preceded by a 304 | 7.2, 7.3, T-17 | specified; handler not built (open) |
| V-16 | low | `cleanURL` accepted `http` URLs and any length; the API's `HttpsUrl` type is https and 2,000 bytes | 4.4 R1, 10.2 | `TestNormaliseListed` |
| V-17 | medium | `normaliseListed` accepted the name `..` (found by the new fuzz target against `RepoRef.Validate`/`job.NewKey`) | 4.4 R1, 10.2 | `FuzzNormaliseListedAgainstKernel` |
| V-18 | low | The document described the case handling of group ids inconsistently (C-3, 4.2); ids are lower case only and `overview` folds a requested id before the lookup | 4.2, C-3 | - |
| V-19 | low | Counts and statements out of date or incomplete: 39 tests, 78.7% coverage, the trace table lacked several relevant guardrails (readiness, warm restart, freshness, budget, typed absence) | 11, 12.1, 12.7, 12.9, 1 (header), App. C | - |
| V-20 | high | `Warm` published every persisted row regardless of the current visibility policy: tightening a source from `installation_scope` to `public_only` and restarting served the private repositories until the first live listing | 4.11, guarantee 9, 10.2 | `TestVerifyWarmAppliesTheCurrentVisibilityPolicy` |
| V-21 | low | `BenchmarkBuildMembership` generated duplicate slugs that the stub accepted and the real kernel rejects (the benchmark failed on the real kernel) | 12.1, 12.7 | benchmark |

### 16.2 External facts re-checked

Library and platform versions of 12.8 were re-checked against the module proxy and pkg.go.dev; the kernel facts (slug grammar, `NewMembership`, `VisUnknown`, `store.KeyOf`, `RepoRef.Validate`/`KeyParts`, `job.NewKey`, `store.ValidateRepo`/`ValidateItem`, `httpapi` helpers) against the kernel source and 06-httpapi. Not verifiable offline: the exact field list of the `repository` webhook payload for `transferred` (the design needs only `Repo` and `Action`), recorded as T-16.

### 16.3 Tests run (final state)

`go vet ./...` and `gofmt -l .` clean; `go test -race -count=1 ./...` green (every package of the module, including the real `sqlite` store, 34 s); `internal/catalog` 80.3% statement coverage; 46 + 4 test functions; `FuzzNormaliseListedAgainstKernel` 8 s (1.1 M executions), `FuzzMakeSlug` 5 s, `FuzzMatcherCompile` 5 s, no failure; benchmarks in 12.1; mutation checks in 12.6.

### 16.4 Left open (owners named)

1. **Handlers and `Mount`** are specified (7) but not built (they need `catalogapi` from the `apispec` unit); the ETag rule of V-15 and test T-17/T-18 are therefore specification, not code. Owner: implementer of M4.
2. **T-8, T-9, T-15** (real stores through `storetest`, fake GitHub fault injection, the budget test) are not implemented. Owner: M3 implementer.
3. **Zero-visibility trade-off.** Visibility removal is immediate and unguarded by design (4.3). A credential whose permission set shrinks so that every private repository disappears from the listing is an *absence*, not a visibility change (Q-4); a listing that returns every repository as `VisUnknown` excludes them all at once, which the mass-removal guard does not see. Mitigation: the `github` contract test (D-8) and `R301` for absences; decision: owner (Q-4).
4. **Q-1** (omit empty groups for every principal) still needs the owners of units 07 and 14.
5. GitHub payload field documentation for `repository.transferred` could not be confirmed offline (16.2).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Settled elsewhere: discovery keys (`removal_confirmations`, `mass_removal_hold`) are declared in unit 05 (CD-CF-07).
- Still to do: `catalog.Config` `Describe` tags and one-key matcher `UnmarshalYAML` (CD-CF-10).
- Slots `catalog.repo` (existence), `catalog.stats`, `catalog.membership`, `catalog.aliases`, `catalog.group_aliases`, `catalog.sync` and the jobs `catalog.reconcile`, `catalog.heads`, `catalog.stats` are the registry entries (CD-SL-01, CD-JB-03).
- Note: `api.repos` and `api.group.repos` handlers are in `overview`, which reads the catalog slots (CD-RT-01).
- Owner: Q-1 and Q-2.
