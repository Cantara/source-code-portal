# Independent Go review of `10-architecture.md` and `11-adr/`

Reviewer role: independent Go expert (idiom and design). Date of review: 2026-10-14. Scope: `10-architecture.md` (all 23 sections), ADR-001..025, `04-decisions.md` (read first, binding), and the research artefacts the architecture leans on (`05-research/05-store-sync-engine.md` and its compile-checked prototypes, `09-github-app-integration.md`). I did not edit the architecture.

Verdict: **approve with changes**. The shape is sound and unusually well argued: the rank matrix is genuinely acyclic, the read model and the engine are the right size, and failure is modelled as data. But the document contains **compile-level contradictions in its own signatures** (section 8.7 cannot be satisfied by one `*github.Client`; `job.FanOut` cannot call its reactors; `httpapi.Mux` does not fit the generated code), a closed-world archtest whose table is **incomplete on day one**, two **reproducible concurrency defects** inherited from the research prototype, and a handful of un-owned seams (metrics, lifecycle ordering, per-job timeouts, planner locking). None needs a redesign; all are fixable in the document before `20-design/`.

Severity counts: 1 blocker (F-01), 14 major (F-02..F-15), 11 minor (F-16..F-26); 26 numbered findings in total.

---

## 1. Method and what I actually ran

1. Read 04 first, then the full architecture and ADRs.
2. **Built the import graph from the stated matrix** (section 5.2) in a script (`/private/tmp/.../scratchpad/g/graph2.py`, about 55 package rows after expanding `{}` and `*`), checked every edge for strictly decreasing rank, ran a DFS cycle check, computed transitive closures, and diffed the package tree of section 7 against the matrix rows.
3. Compiled the architecture's own kernel signatures (`job`, consumer interfaces of 8.7) in a scratch module with Go 1.27.1.
4. Re-ran the research prototypes (`engine`, `readmodel`) with `go test -race -count=3` (pass), then wrote two extra tests against them to probe concerns F-05 and F-08 (both reproduce).
5. Checked external facts against primary sources (list in section 6).

### 1.1 Result of the acyclicity check

- **The matrix as written is acyclic and rank-monotone.** 0 cycles, 0 rank violations, 0 unknown package names in any row. The proof in 5.4 holds for the table. Same-rank edges: none.
- But the matrix is **not closed over the document's own package tree**. Several packages named in section 4 or 7 have no row, so `internal/archtest` (which, per ADR-018, fails on any package without a rule) would fail on the first run: `httpapi/apitest`, `web` (the Go embed package), `docs/schema`, `manifests/parsertest`, plus `tools` (if it is a package of the main module), and `gitsource` is listed in the matrix but "created only post-v1" (a row for a package that does not exist is harmless; a missing row is not). See F-04.
- The matrix **also omits edges the text needs**: OTel metric/trace API for features, `refresh`, `github`, `logx` (F-03); route name for the access log in `httpx` (F-23); `job` or `scm` for `ops` admin retry (F-24). Adding these keeps the graph acyclic but they must be decided, not discovered by archtest failures.
- Transitive reach is wider than the prose suggests: every feature (even `badges`, `webhook`, `live`) transitively reaches `platform/httpx` and kin-openapi through `httpapi`. "Features never import `httpx`" is true only for direct imports (F-13).

---

## 2. Findings

Format: **ID, severity, where, problem, evidence, fix.**

### F-01 (BLOCKER) One `*github.Client` cannot satisfy the consumer interfaces of section 8.7 / ADR-004

- **Where:** 8.7; ADR-004 "Consumer-side interfaces ... `github.Client` satisfies them structurally"; rule 5.1(7) "consumer-defined interfaces use only stdlib and kernel types".
- **Problem (three independent compile failures):**
  1. `docs.FileReader.ReadFile(ctx, r, ref, path, v scm.Validators) (scm.Result[scm.File], error)` and `media.RepoImageReader.ReadFile(ctx, r, ref, path, maxBytes int64) (scm.File, error)` share a method name with different signatures. Go has no overloading, so no type can implement both.
  2. `docs.TreeLister.ListTree(..., opt TreeOptions)` and `manifests.TreeReader.ListTree(..., opt TreeOptions)`: each package declares its **own** `TreeOptions`. Method signatures must be identical, so `*github.Client` can implement at most one. (8.7 says `TreeOptions` is "declared by the consumer that needs them".)
  3. `catalog.RepositoryLister.ListRepositories(...) (scm.Result[RepoList], error)` and `ops.InstallationStatus` (`InstallationView`, `RateView`) use consumer-owned types in the signature. `github` is rank 2 and may not import `catalog`/`ops` (rank 4), so it cannot return those types. The rule in 5.1(7) is violated by the document's own examples. (`badges.Facts` has the same shape but is correctly satisfied by a bridge that imports `badges`; that is the only working pattern, and it is not what ADR-004 says for `github`.)
- **Evidence:** reproduced in a scratch module (`go vet`): `*GithubClient does not implement ManifestsTreeReader (wrong type for method ListTree) have ... docsTreeOptions want ... manifestsTreeOptions`.
- **Fix (pick one, state it in 8.7/ADR-004):** (a) move `TreeOptions`, `ListState`, `RepoList` and the installation/rate view types into `scm` (kernel vocabulary, which is what they are), and rename the media method (`ReadImage`) or unify the two read methods; or (b) keep consumer-local types and write the adapters in `cmd/scp` (or `overview/bridges`), and delete the claim that `*github.Client` satisfies the interfaces directly. Option (a) keeps "no adapter glue" and is what I recommend; the cost is ~4 small types in `scm`.

### F-02 (MAJOR) `job.FanOut` / `Reactor` / `EventSink` do not fit together, and the generic is unnecessary

- **Where:** 8.3, 6.4, 8.7 (`catalog.EventSink`), ADR-024.
- **Problem:** `Reactor[E]` is `func(ctx, ev E, enq Enqueuer)`, but `FanOut[E](rs ...Reactor[E]) func(ctx, ev E) error` has **no `Enqueuer` parameter**, so it has nothing to pass to the reactors (compiled: the only legal argument is `nil`). `catalog.EventSink` is an interface with a `Handle` method but `FanOut` returns a bare func (needs an adapter type or a method). Separately, the stated reason for the generic ("so `job` need not import `scm`") is wrong under the document's own rule: `job` is rank 1 and `scm` is rank 0, so `job -> scm` is an allowed edge. The generic type parameter exists to avoid an import the rank rule already permits, and has exactly one instantiation (`scm.Event`). G8 ("`refresh` and `scm` never learn about each other") is the only argument, and it concerns `refresh`, not `job`.
- **Fix:** `func FanOut(enq Enqueuer, rs ...Reactor) EventHandler` with `type Reactor func(context.Context, scm.Event, Enqueuer)` and `type EventHandler func(context.Context, scm.Event) error` having a `Handle` method; let `job` import `scm` (add it to the matrix). Delete `EventView`. If G8 is to hold transitively, put `Reactor`/`FanOut` in a tiny package that imports both `job` and `scm`. Also state that reactors run **synchronously on the webhook request goroutine** (GitHub's 10 s deadline) and must not do I/O.

### F-03 (MAJOR) Metrics and trace instrumentation has no legal path under the matrix; ADR-013 contradicts section 5

- **Where:** ADR-013 ("instruments are created through the OTel API in features"); 5.2 (third-party for features is "none"; `refresh` lists only `x/sync, x/time/rate`; `github` lists only otelhttp); 18.2-18.3 (dozens of `scp_*` instruments and `job.run`, `snapshot.apply`, `render.markdown` spans emitted from features, `refresh`, `readmodel`); 5.3 G6 (only the SDK is confined; the API is not).
- **Problem:** `scp_webhook_total`, `scp_badges_total`, `scp_docs_render_seconds`, `scp_manifests_*`, `scp_sync_*`, `scp_snapshot_publish_total` must be recorded inside packages whose allowed imports exclude `go.opentelemetry.io/otel/metric` and `/trace`. `logx` must read the span context for `trace_id` (also not allowed). Using the global `otel.Meter(...)` would reintroduce a global (G5, GR-03); injecting a `metric.MeterProvider` is not in any constructor signature in section 8. archtest will reject whichever the implementer picks.
- **Fix:** decide one: (a) add `go.opentelemetry.io/otel/{metric,trace}` (API modules only, no SDK) to the allowed third-party list of every package that emits telemetry, and make every constructor take a `metric.Meter`/`trace.Tracer` (or a `telemetry.Provider`-free struct of them) from `cmd/scp`; or (b) define one narrow consumer-side `Observer` interface per package (`refresh.Observer`, `docs.Metrics`) satisfied by `platform/telemetry` adapters. (a) is idiomatic and cheaper. Add the metric-name registry as a generated/tested list so 18.2 and code cannot drift.

### F-04 (MAJOR) The closed-world archtest table is incomplete (it would fail on day one)

- **Where:** 5.2 vs sections 4 and 7; ADR-018.
- **Problem:** see 1.1. Packages without rows: `httpapi/apitest` (mentioned only in a test-files paragraph), `web`, `docs/schema` (has a Go embed file, served at `/schemas/scp/*`), `manifests/parsertest`, `tools`.
- **Fix:** add rows (ranks 1-2 are sufficient: `web` rank 0 `embed`-only, `docs/schema` rank 0, `manifests/parsertest` and `httpapi/apitest` test-only), and make the generator that emits depguard rules also emit a "tree vs table" assertion in CI.

### F-05 (MAJOR, reproduced) Engine callbacks run under the engine mutex; any hook that re-enters the engine deadlocks

- **Where:** 8.6 `WithOnParked`, `WithOnExhausted`; research prototype `engine.go` `finish()` (arch is derived from it); 5.11 (OnParked persists to `parked` table and logs).
- **Problem:** in the prototype `OnParked`/`OnExhausted` are invoked with `e.mu` held. The architecture assigns them real work: a SQLite write (`Store.Park`, ~1 ms) and logging. Consequences: (1) **every `Enqueue` (webhook handlers, "never blocks") and every dispatch stalls for the duration of a database write**; (2) a hook that calls `Enqueue`/`Inspect` deadlocks. I reproduced (2): a hook calling `e.Enqueue(...)` makes `Run` never return (`TestOnParkedReentrancy`, scratch copy of the prototype).
- **Fix:** collect callbacks in `finish`, release the lock, then invoke them (or hand them to a bounded channel owned by a goroutine the engine joins). Add the rule "no callback is ever invoked with a lock held" to 10.3 and a test (the one above) to the engine suite.

### F-06 (MAJOR) Planner semantics are unspecified where it matters: locking, outcome wiring, and the re-enqueue-while-running trap

- **Where:** 8.6 `Planner`; ADR-003; research states the Planner was "not prototyped".
- **Problems:**
  1. **No locking is specified.** `Tick` (scheduler goroutine), `OnResult` (worker goroutines), `SetSubjects` (catalog job), `Restore` (startup) touch the same state. If `OnResult` is called from `Engine.finish` (it must be; see 3) and `Tick` calls `Engine.Enqueue` while holding the planner lock, you get a lock-order inversion (engine.mu -> planner.mu vs planner.mu -> engine.mu). Rule: `Tick` snapshots due keys under the planner lock, releases, then enqueues.
  2. **`Tick` re-enqueues keys that are already running.** `NextDue` is only updated in `OnResult`, i.e. after completion. A job running across a 15 s tick boundary is seen as "due" and `Enqueue` marks it `dirty`, which by design causes **exactly one follow-up run**. At cold start (3,000 jobs at 10/s) a large fraction of jobs run more than once, contradicting the budget arithmetic in 13.4 and ADR-022. Planner must set `NextDue = +inf` (or an `inflight` flag) when it enqueues, and only webhook/reactor enqueues should use dirty-while-running.
  3. **How does `OnResult` get called?** `Engine.Register(spec)` and `Option` (`WithGate, WithGrace, WithOnParked, WithOnExhausted, WithMaxKeys`) contain no result hook, `NewPlanner(e *Engine, ...)` needs the engine, and the engine needs the planner: a construction cycle with no stated resolution (`WithOnResult` set after construction, or planner wraps `Spec.Run`).
  4. **Double ownership of `next_due_at`.** `store.Item.NextDue`, `Revalidated(..., nextDue)` and `Failure.NextDue` are written by the *job* (which does not know the planner's jitter/idle back-off), while `PlanSink.SaveState` writes the same columns from the *planner* afterwards: two writers, two transactions per job. Pick one owner (planner writes `next_due_at/attempts/unchanged_streak` only; job writes payload/validators).
- **Fix:** specify the above in 8.6; add synctest cases: "tick during a running job causes no follow-up", "no lock held across Enqueue".

### F-07 (MAJOR) No per-job deadline; one hung job permanently consumes a concurrency slot

- **Where:** 8.3 `job.Spec` (no `Timeout`), 8.6 (`Task.Timeout` exists for scheduler tasks only), 10.3.
- **Problem:** the engine passes a context to `Spec.Run` but nothing bounds it except the HTTP client's total timeout. A CPU-bound render/parse, a stuck SQLite call, or a handler that ignores `ctx` holds one of `MaxConcurrent` (default 4) github slots forever; with 4 such jobs the upstream is dead while `/livez` stays green (heartbeat goroutine alive). On shutdown the only cap is `Grace`.
- **Fix:** `Spec.Timeout` (default per upstream, enforced by `context.WithTimeout` in the engine), an overrun counter, and a "job exceeded timeout and ignored cancellation" counter so leaks are visible. Make `livez` depend on dispatcher progress, not on a goroutine existing (GR-41 is invoked elsewhere for exactly this).

### F-08 (MAJOR, reproduced) `readmodel.Put` can resurrect a removed repository ("zombie" entries)

- **Where:** 8.4, 9.1 validators, 10.3 (jobs run concurrently with `catalog.reconcile`).
- **Problem:** `Put` creates the entry if the repository is absent (prototype: `old == nil -> b.members = true`). Sequence: `catalog.reconcile` publishes `Remove(x)`; an in-flight `commits.refresh` for `x` (already past its fetch) then calls `Put(x, commitsSlot, v)`. Result: `Repos()` contains `x` with a commits slot but **no `catalog.repo` slot**. None of the listed validators catches it (they check group members exist in `catalog.repo`, not the converse). I reproduced it on the prototype (`TestZombie`). Handlers that iterate `Repos()` then see a repository with no `scm.Repository`. The store side has the matching symptom: `PutItem` for a cascade-deleted repo hits the FK, and the error is "transient" so the key is retried with backoff until the planner drops it.
- **Fix:** `Put` on an unknown id is an error (`ErrUnknownRepo`) unless the slot is the one designated as the existence slot; add the converse validator ("every repo has `catalog.repo`"); map `scm.ErrNotFound`/FK failures to `job.Permanent`-with-drop (not park) in the job wrapper. Same race for `manifests.graph` (built from snapshot S, validated against S' where a unit's repo is gone): build inside `Apply` (9 ms under the publish lock is acceptable) or rebase on `ErrInvalidSnapshot`.

### F-09 (MAJOR) `singleflight` with the first caller's context, and shared slices from memoised results

- **Where:** 8.7 ("adapter memoises `ListTree` for 60 s ... with `singleflight` so `docs` and `manifests` share one call"); 8.8 (`TokenSource.Token ... coalesced per scope (singleflight)`).
- **Problem:** `golang.org/x/sync/singleflight` `Do`/`DoChan` take no context (verified 2026-10-14, pkg.go.dev) and hand **the same result to every duplicate caller**. If the call runs under the first caller's `ctx`, that caller's cancellation (its job timeout, shutdown) fails every coalesced waiter: a docs job timing out cancels the manifests job's tree call and the installation-token mint for everyone. And the memoised `scm.Tree{Entries []TreeEntry}` slice is shared mutable state between two features (neither is allowed to mutate it; nothing enforces that).
- **Fix:** run the shared call with `context.WithoutCancel(ctx)` plus its own bounded timeout (or a refcounted cancel), return per-caller `slices.Clone` or an explicitly read-only view type for `Tree`, and add a test that cancels the first caller. The research already said "singleflight only inside adapters" (05 section 5.9) but did not call out this hazard.

### F-10 (MAJOR) Docs eviction contradicts the immutable snapshot model

- **Where:** 9.4 (`docs.resident_budget`, eviction, "API answers 202 `loading` and enqueues an interactive job"), 9.1 (`docs.repo` slot with `SafeHTML` bodies), 8.4 ("slot value is immutable once Put").
- **Problem:** a budgeted, evicting body cache needs per-document last-access state updated **on the read path**, but readers only hold an immutable `*Snapshot`. Either (a) a mutable LRU lives beside the snapshot (shared mutable state with its own locking, undocumented, and the snapshot no longer says what is resident), or (b) every eviction is a published snapshot (an `Apply` per eviction decision, plus a publish storm when the budget is exceeded, plus the "202 loading" state on a read-mostly documentation view). It also makes a GET depend on a background job, contrary to GR-02 and GR-31 in spirit. 192 MiB resident against up to 50-200 docs x 1,000 repos makes the eviction path the common case, not the corner.
- **Fix:** keep only README (and descriptor) resident; render other docs on demand from the store payload through a bounded, separately owned `docs.Cache` (explicit type, explicit lock, `singleflight` with detached context, not part of the snapshot), or lift the cap and size memory honestly. Document it as a deliberate carve-out from "nothing on the request path touches the store".

### F-11 (MAJOR) `httpapi.Mux` does not fit oapi-codegen's `ServeMux`, and generated registration bypasses `routes.Table`

- **Where:** 8.14 (`Mux interface{ HandleFunc(...) }`, `Mounter`), 11.4, ADR-020, 8.13 `Table.Handle/Verify`.
- **Problem:** (1) oapi-codegen's std-http output declares `type ServeMux interface { HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)); http.Handler }` and `HandlerFromMux(si, m ServeMux)` (observed in the generated smoke output on the repository's `main` branch, 2026-10-14; confirm for v2.8.0 in the M0 spike). `httpapi.Mux` lacks `http.Handler`, so a feature's `Mount(m httpapi.Mux)` cannot pass `m` to `HandlerFromMux`: compile error. (2) Generated code registers its **own** patterns (`"GET /api/v1/..."` from the spec) with `m.HandleFunc`; the named `routes.Table.Handle` path is bypassed, so `Table.Verify()` ("every named route has a handler, none for an unknown name") **cannot see API routes**, and the 8.13 claim that the table is "the only source of URLs" is false for the whole `/api/` class. Three registries (OpenAPI paths, `routes.Table`, `web/routes.ts`) are kept equal only by tests.
- **Fix:** make `httpapi.Mux` embed `http.Handler` (or just pass `*http.ServeMux`); make `routes.Table` implement `Mux` as a recording wrapper that maps registered patterns to route names and fails `Verify()` on unknown or missing patterns; or generate the API half of the table from the bundled OpenAPI so there is one registry for `/api/`.

### F-12 (MAJOR) Lifecycle ordering is asserted but `lifecycle.Plan` is not defined; several ordering bugs follow if it is naive

- **Where:** 4.2 (`Run(ctx, Plan)`), 10.1 step 8, 10.3, 10.4.
- **Problem:** the diagram needs **at least three contexts**: the signal context, the engine/scheduler run context (cancelled only after servers finish), and the store worker context (cancelled only after the engine returned). `lifecycle` is specified as `x/sync/errgroup`; a plain `errgroup.WithContext` cancels all members at once, which contradicts 10.4 (servers must drain *before* the engine stops; the research 5.12 describes the same order). Further: (a) `Store.Run` (hourly backup / degraded probe) and `Store.Close` are both described; `Close` must not run before `Run` has been joined, or `VACUUM INTO` can race the handle close; (b) telemetry is listed as a worker started in step 8 but flushed last: it must not share the worker context that is cancelled with the others; (c) "failure of any worker takes the process down in this same order" needs the failing worker excluded from the join; (d) scheduler tasks receive `ctx` with **no grace** while the engine gets 15 s, so a `catalog.reconcile` running in the scheduler is cancelled instantly (fine for a rolled-back tx, but the asymmetry should be explicit); (e) SSE "told to close" at drain start, with `retry: 3000`, makes clients reconnect during the 5 s drain.
- **Fix:** specify `Plan` as an ordered list of stages `{Name, Start(ctx) error, Stop(ctx) error}` with a documented stop order, each stage with its own context and shutdown deadline; test it in a synctest bubble with an injected failing stage.

### F-13 (MAJOR) `overview` is the service layer in disguise; `httpapi` is a hub; `scm` is the models bucket it was meant to avoid

- **Where:** 4.4 `overview`, 8.14 `httpapi`, 8.1 `scm`; ADR-001 "price of no feature-to-feature imports".
- **Problem:** `overview` imports seven feature roots and holds: site info, the composite repo resource, the whole `llms.txt` family, sitemap/robots, content negotiation, **and** the bridges that implement other features' consumer interfaces. That is at least four reasons to change (SRP, principle 3). Everything cross-cutting will accrete there; it becomes the layer the owner rejected, one package deep. Bridges are *wiring*, not a feature.
  `httpapi` is imported by every feature and bundles problem details, cursors, ETag, kin-openapi validation, `ProblemMux`, `Negotiate`, `Static`, *and* authorisation (`Principal`, `Authorizer`, `LookupRepo` hard-wired to the `catalog.repo` slot type). A feature that only wants `WriteProblem` transitively links kin-openapi (verified in the closure: every feature reaches `httpx` and openapi through it).
  `scm` mixes provider vocabulary (`Commit`, `Tree`, `Event`) with portal domain (`Slug`, `GroupID`, `Membership`, `GroupMeta`); the package name promises source control only. `Membership` has behaviour and an owner (`catalog`) yet lives in the kernel.
- **Fix:** split `overview` into `overview` (composite API + llms/sitemap, one purpose: read composition) and move bridges to `cmd/scp/bridges.go` or `internal/wire`; split `httpapi` into `httpapi` (problem, page, etag: no kin-openapi) and `httpapi/validate` + `httpapi/authz`; rename `scm` to a name that fits (`core` is banned by the spirit of G1, so `repo`/`catalogue`-neutral names, or accept `scm` and move `Slug/GroupID/Membership` to `readmodel`-adjacent `portal` kernel package). At minimum, write down the exit condition for `overview` (ADR-001's exit condition is about `scm` only).

### F-14 (MAJOR) The manifests watchdog "abandons" goroutines and the memory bound is not honest if it does

- **Where:** 10.3 ("per-file watchdog goroutine abandoned and counted on timeout"), 13.2 (weighted semaphore of 256 MiB over `size x amplification`).
- **Problem:** Go cannot cancel a goroutine. An abandoned parser keeps burning CPU and holding its allocation. If its semaphore weight is released at abandonment, the 256 MiB bound is false; if it is held, a few poison files can deadlock the pool. `goleak` (per G10) would flag the leak in tests. Repeated hostile input becomes a slow DoS of the single process.
- **Fix:** parsers must poll `ctx.Err()` at token boundaries (the Limits "2,000,000 tokens" counter is the natural place); the watchdog is only a last resort that (a) holds the weight until the goroutine really exits, (b) trips a per-parser circuit that disables that parser after N abandonments until restart, (c) is surfaced in `/status`. Fuzz invariant 2 ("bounded time") should be asserted by cancellation, not by abandonment.

### F-15 (MAJOR) Webhook de-duplication is specified three different ways

- **Where:** 17.2 and 6.4 (15-minute in-memory set), config sample in section 16 (`dedupe_ttl: 168h`), `05-research/09` (store table with TTL >= 7 days, after *durable* enqueue), W12 ("in-memory dedupe set"), GR-21.
- **Problem:** the 168 h default in an in-memory structure is unbounded (no cap, no sweeper in the goroutine table 10.3; `dedupe.go` has no owner) and lost on restart; 15 minutes does not cover GitHub's 3-day redelivery window (harmless only because jobs are idempotent). The "record after enqueue" rule is also racy for two concurrent identical deliveries (both pass the check): harmless by coalescing, but then the "duplicate" metric is not exact.
- **Fix:** since webhooks are hints (the doc's own principle), de-duplication is an optimisation. Pick: bounded in-memory LRU with TTL (15 min), drop the `dedupe_ttl` key (or document that it is the TTL of that LRU), lazy eviction on insert (no goroutine), and say plainly that exactness is not required. Add the missing scheduler task for `redelivery.enabled` (config) to the task set in 8.6.

### F-16 (MINOR) Global-scope jobs: two execution paths with different resilience semantics

- **Where:** 8.3 `Scope` ("ScopeGlobal (scheduler task)"), 8.6 task list (`catalog.reconcile`, `catalog.heads`, `integrations.*` call GitHub/Jenkins), 10.1 step 8 ("first `catalog.reconcile` runs at `PrioWebhook`").
- **Problem:** `Scheduler.Task.Run func(ctx) error` runs outside the engine, so a scheduler task that calls GitHub is not subject to per-upstream pacing, the budget `Gate`, the upstream **rate-limit pause**, parking, or panic accounting by outcome class (the scheduler only records `LastErr`). The text also says the first reconcile has a `PrioWebhook` priority, which only makes sense through the engine.
- **Fix:** state that `Scheduler` tasks only `Enqueue` keyed jobs (Task.Run = a one-line enqueue + trigger), so every upstream call goes through the engine.

### F-17 (MINOR) Name collisions and identity sprawl

- `readmodel.RepoID` is a `string` slug; `scm.RepoID` is `int64`; both appear in files that import both. Rename the readmodel type (`readmodel.Key`) or make the snapshot generic over its key.
- Four encodings of one repository: `scm.RepoRef{Source,ID,Owner,Name}`, `store.RepoKey{Source,Org,Name}` (`Org` vs `Owner`), `job.Key` string (`kind/source/owner/name`), `readmodel.RepoID` (slug). Every boundary converts; a rename changes three of them differently (slug alias vs GitHub id vs `(source, org, name)` unique key). Specify the conversion in one place (`scm`) and test round-trips and rename.
- `store.Item.State string` while the store imports `freshness`: use `freshness.State` (or drop the import from the matrix).
- Generated contract packages: directory `contract/` but package `catalogapi`/`commitsapi`: package name != last path element forces import aliases everywhere. Name the directory after the package (`catalogapi/`) or the package `contract`.
- `internal/spec` is a vague name for "embedded bundled OpenAPI documents" (and sits next to `httpapi/openapi`); `openapispec` or fold into `httpapi/openapi`.

### F-18 (MINOR) `SafeHTML` cannot cross two boundaries it must cross

- **Where:** 8.10, ADR-005, `pages` ("Markdown through an injected `Renderer` interface"), 9.3 (payload persisted in SQLite), 9.4 ("re-rendered from `item.payload`").
- **Problem:** `SafeHTML` lives in `docs/internal/render`; Go's `internal` rule means `pages` and `overview` cannot name the type, so `pages.Renderer` can only return `string`, defeating principle 6 for the announcement and static pages. And the warm start must rebuild `SafeHTML` values from stored bytes: either `UnmarshalJSON/Text` mints one from arbitrary bytes (the "only minter is the sanitiser" claim becomes false, and the store becomes a trusted source) or the source Markdown is re-rendered at boot (CPU, but honest).
- **Fix:** put `SafeHTML` in a tiny public kernel package (`internal/safehtml`, constructor reachable only by `render` via an internal handle or a `Mint(token)` pattern), and persist rendered output with its pipeline version and re-sanitise-on-load or re-render-on-load; state which.

### F-19 (MINOR) SVG via `text/template` + a hand-written escaper

- **Where:** ADR-008, ADR-011, GR-33 row in section 20.
- **Problem:** GR-33 forbids building XML/SVG by string replacement. `text/template` with an escaper is the same mechanism with a function the author must remember to call in every attribute and text position; message text (release tag, licence string) is attacker-influenced (anyone who can push a tag). The mitigation is golden tests, not structure.
- **Fix:** build SVG with `encoding/xml` (`xml.Encoder` / `EscapeText`) or `html/template`'s contextual escaping, or a typed `escaped` string whose only constructor escapes; keep the ~30-string golden test and add a fuzz test asserting well-formed XML for arbitrary input.

### F-20 (MINOR) SQLite handling: items the architecture leaves open

All checked against the architecture and `05-research/05`; none contradicts the single-writer design, but each is a trap:
- **Which handle runs `Backup`/`Check`?** Not stated. `VACUUM INTO` is not a write on the source (sqlite.org/lang_vacuum.html, 2026-10-14) and measured 144 ms at 66 MB, but with the 192 MiB blob cache cap it will be ~0.5-1 s. If it runs on the single `rw` connection it **stalls every job's write** for that time hourly. Run it on an `ro` connection (whether `query_only=1` permits it is **not stated in the SQLite pragma or vacuum docs**, so it needs a 5-line test in the M0 spike).
- **`GetCache` updates `last_used`:** a read that writes. Four parse goroutines then serialise on the one writer connection per blob. Batch the touch (in memory, flush with the next write) or accept approximate LRU.
- **Read-only root filesystem and temp files:** SQLite may need a temp directory for large sorts/indices and `VACUUM` (SQLite docs: temp indices/tables are stored per `temp_store`; default is compile-time). With `readOnlyRootFilesystem` and distroless there is no writable `/tmp`. Set `PRAGMA temp_store=MEMORY` or mount an `emptyDir` at `/tmp` and add a smoke test in the image job.
- **`rw` is one connection:** a transaction held while calling another `Store` method deadlocks (research 05 notes this from River's guide). Add a lint/test rule: no `Store` method calls another `Store` method on the same handle, and `LoadItems(ctx, fn)` must never be given a callback that writes.
- **Downgrade handling:** "newer schema -> quarantine and rebuild" turns an application rollback into a full cold start (readiness 10-15 min, ~3,000 API calls). Acceptable for a cache, but call it out in the runbook and consider read-compatible additive migrations.
- **`store.Store` is 20 methods and carries commit-specific verbs** (`AppendCommits`, `LoadCommits`, `Park`, `SaveTask`) while claiming to be feature-agnostic ("`Kind` is owned by features"). Fine for v1; the carve-out interfaces keep features honest.

### F-21 (MINOR) Context and goroutine hygiene details

- `Task.InitialDelay func(rnd *rand.Rand)` and `Scheduler` seeds: `*rand.Rand` is not goroutine-safe; the prototype gives each task its own `Rand` (good), the architecture does not say so. State it.
- `Scheduler.Add` after `Run` starts races on `s.tasks` (prototype reads it unlocked in `Trigger`). Freeze like `readmodel.Registry`.
- `Engine.Enqueue` after `Run` has returned silently accepts work that never starts; during shutdown the webhook handler would return `202` for an event that is dropped. Return `ErrClosed` (the webhook maps it to 503, which is what GitHub retries).
- `readmodel.Publisher.Apply(fn)` runs `fn` under the publish mutex: document "fn must not block or do I/O" and enforce by review/lint, since a slow `fn` stalls every other job's publish.
- `Apply` returning `ErrInvalidSnapshot` after `store.PutItem` succeeded leaves store and snapshot diverged; classify `ErrInvalidSnapshot` as a permanent (parked, loud) outcome rather than a transient retry loop that rewrites the same row.
- FanOut reactors and `catalog`'s `EventSink` are called concurrently (webhook goroutines, scheduler); reactors must be safe for concurrent use: say so.

### F-22 (MINOR) Immutability is a convention, not a guarantee

- `Slot[T]` stores `T` by value, but `scm.Repository.Topics []string`, `scm.RepoStats.Release *Release`, `scm.File.Data []byte`, `Snapshot.Repos() []RepoID` and every `[]`/`map` in `RepoFacts` are shared with all readers. A job that reuses a buffer after `Put`, or a handler that sorts a returned slice, is a data race the type system cannot catch. The architecture leans on `-race` and review.
- **Fix:** name the rule in 8.4 ("`Put` takes ownership; callers must not touch the value afterwards; readers must not mutate"), add a `slices.Clone` at the producer boundary for kernel types, and a `go vet`-style custom test that walks `Value[T]` with reflection in unit tests asserting no later mutation (cheap property test with `-race` and a mutating reader).

### F-23 (MINOR) Route name for the access log has no legal source

- 11.1 logs "route name"; the name lives in `routes` (rank 0) and `Table.Handle` registers it; `platform/httpx` (rank 2, matrix: `logx` only) has no way to read it. Log `r.Pattern` (available since Go 1.23) and keep names for metrics only via a context value defined in `routes` and read by `httpx` (add `routes` to httpx's row). Same shape problem for `webapp` needing reserved prefixes (rank 0 imports "none"): pass them in `Options`.

### F-24 (MINOR) Rule text contradicts the rule's own table

- 5.1(1) "equal rank never imports equal rank, except a feature root importing its own subpackages": the subpackages are rank 1 and 5, not 4, so the exception is never exercised; delete it.
- 5.1(2) says features import "only ... `httpapi`"; `ops` needs to retry jobs (`POST /admin/jobs/retry`) but has no `job`/`scm` import: define `ops.Retrier{Retry(key string) error}` or add `job`.
- 5.3 G5 / A-18: the "init() exception for SQLite driver registration" is vacuous. The modernc driver's own `init` registers it; `gochecknoinits` checks only this module. State "blank import of the driver in `store/sqlite`" instead. Applies to every third-party `init` (OTel, chroma lexers): the principle "no `init()` side effects" is about own code.
- ADR-014: "`//` redirects with 307 (Go 1.26+)". The Go 1.26 release notes (go.dev/doc/go1.26, 2026-10-14) say **trailing-slash** redirects now use 307, not path-cleaning redirects. Re-verify the cleaning case or drop the claim.
- 8.1 `Membership.Groups() []GroupMeta`: `GroupMeta` is never defined.

### F-25 (MINOR) Over-engineering candidates (abstractions with one implementation that add little)

- `job.Reactor[E EventView]` generic (F-02).
- `gitsource` in the package map, matrix and ADR-018 although "created only at the post-v1 milestone": remove from v1 documents; add the row when the package exists.
- SSE machinery in the v1 kernel: `Publisher.Subscribe`, the 256-entry change ring, `ChangesSince`, and per-repo `Rev` ship in v1 while SSE is v1.1 behind a flag. `Rev` pays for ETags now (keep); the ring and `ChangesSince` could wait.
- Three route registries (F-11) and `routes.Table.Class/Owner` metadata that only tests read.
- `manifests/providers/{githubgraph,sbom,depsdev}`, `DependencySource`, `PomSource`, `StatusProvider` for a provider that is off by default: acceptable where the owner mandated flexibility (D-17), but each needs its "first consumer" or it violates principle GR-04 ("no interface without a second reader or a test seam"). The architecture itself cites that rule in section 20.
- Counter-balance: I would **not** remove `Gate`, `PlanSink`, `Tracker` (each exists to keep `refresh` free of `store`/`github`), nor `store/memory` (needed for synctest bubbles).

### F-26 (MINOR) Under-engineering candidates

- No `Spec.Timeout` (F-07), no metrics injection (F-03), no `Plan` type (F-12), no stated planner locking (F-06).
- No back-pressure in `Publisher.Apply` for a publish storm at cold start: 3,000 completions = 3,000 `maps.Clone` of the 1,000-entry map plus projector/validator passes serialised on one mutex. Measured costs are small (0.31 ms index), so this is fine at target scale; add a coalescing option ("publish at most every 100 ms, batching builders") before the 10,000-repo exit condition, not after.
- `FanOut` panic recovery logs per reactor but there is no timeout; a reactor that blocks stalls the webhook request until GitHub's 10 s deadline.
- `ErrQueueFull` (research) is missing from the engine API in 8.6 although 15/17 map it to 503.

---

## 3. Layering, interfaces, goroutines, shutdown: summary answers to the brief

| Question | Answer |
|---|---|
| Is the import matrix really acyclic? | Yes as written (script: 0 cycles, 0 rank violations). It is **incomplete** relative to the package tree (F-04) and relative to needs (F-03, F-23, F-24). |
| Interfaces defined by consumers, and small? | Mostly small and consumer-side (`CommitLister`, `Gate`, `PlanSink`, `Verifier`, `Tracker`). The GitHub-facing set is **not implementable** by one type (F-01). `store.Store` is large by design with carve-outs (F-20). `job.Enqueuer` is kernel-defined (acceptable, one method). |
| Layering disguised as features? | `overview` and `httpapi` (F-13). `store` partially (commit-specific verbs). |
| God packages? | `overview`, `httpapi`, `scm` (F-13); `cmd/scp` wiring is a legitimate composition root. |
| Shared mutable state? | Snapshot values and memoised trees are shared by convention (F-09, F-22); docs eviction (F-10); the planner's state (F-06). The snapshot pointer swap itself is correct (prototype passes `-race`; `Apply` serialises on a mutex; `notify` lock order `p.mu -> subMu` is consistent). |
| Goroutine ownership / leaks | The table in 10.3 is good. Gaps: abandoned manifests goroutines (F-14), no per-job timeout (F-07), dedupe/`httpx` sweeper ownership (F-15), singleflight (F-09). |
| Shutdown ordering | The intended order is right (readiness off, drain, servers, engine, store, telemetry); the mechanism that guarantees it is unspecified (F-12). |
| Races in snapshot swap / store writes | Swap: sound. Zombie Put (F-08), graph validate race (F-08), store/snapshot divergence on `ErrInvalidSnapshot` (F-21), planner locking (F-06), engine callbacks (F-05). |
| SQLite single-writer pitfalls | Design is the recommended one (WAL, one `rw` with `BEGIN IMMEDIATE`, small `ro` pool). Open: which handle runs backup, `last_used` writes, temp dir on read-only rootfs (F-20). |
| Context misuse | singleflight (F-09), scheduler without grace (F-12), `Engine.Enqueue` after stop (F-21). No ctx-in-struct or ctx-as-optional-argument found. |
| Over/under-engineering | F-25 / F-26. |
| Un-idiomatic naming/APIs | F-17, F-24. |
| Would not compile | F-01 (hard), F-02, F-11; `EventSink` vs `FanOut` shape (F-02). The kernel types `scm`, `freshness`, `readmodel`, `store`, `job` (other than FanOut) compile; the prototypes compile and pass `-race`. |

---

## 4. Claims checked against the research reports

| Claim in architecture | Source | Result |
|---|---|---|
| 52 MB / +0.12 MB per extra snapshot / 0.31 ms index rebuild at 1,000 repos | `05` M1 | Matches (52.1 MB, +0.12 MB, 308 us). |
| Engine "about 600 lines, 10 synctest tests, `-race -count=30`" | `05` 5.14, `assets/engine` | I re-ran `-race -count=3`: pass. `Planner`, `Gate`, AIMD and `ErrQueueFull` are **not** in the prototype (research says so); architecture's `Planner`/`Gate` signatures are new and untested (F-06). |
| `Gate.Admit(upstream, prio)` | `05` 5.6 has `Admit(p Priority)` | Architecture changed the signature; fine, but ADR-015 reserves (40/15/2%) are per-priority only. Not a defect. |
| Webhook dedupe | `05` 5.10 (15 min, memory), `09` section 190 (store table, >= 7 days, after durable enqueue), config 168h | **Three-way conflict** (F-15). |
| Shutdown order | `05` 5.12 | Architecture adds drain + telemetry + budget 38 s; consistent. Mechanism missing (F-12). |
| `synctest` GA in 1.25, `Sleep` and `httptest.NewTestServer` in 1.27 | go.dev/doc/go1.27 (2026-10-14) | **Verified** for `Sleep`, `NewTestServer`, `Server.MaxHeaderValueCount` ("If unset, `DefaultMaxHeaderValueCount` is used"). |
| `signal.NotifyContext` cause names the signal | go.dev/doc/go1.26 (2026-10-14) | **Verified**. |
| Green Tea GC default since 1.26 | go.dev/doc/go1.26 | **Verified**. |
| `encoding/json` "v2 engine" | go.dev/doc/go1.27 | **Verified** (`encoding/json` is backed by v2; opt-out `GOEXPERIMENT=nojsonv2`). |
| pprof `goroutineleak` in 1.27 | go.dev/doc/go1.27 | **Verified**, with the caveat in the notes that leaks via globals or runnable goroutines can be missed. |
| ServeMux "`//` redirects with 307 (Go 1.26+)" | go.dev/doc/go1.26 | **Not supported** by the cited note: it says trailing-slash redirects (F-24). |
| `HandlerFromMux(si, ServeMux)` usable from `httpapi.Mux` | oapi-codegen generated output (main branch, 2026-10-14) | **Contradicted**: generated `ServeMux` also requires `http.Handler` (F-11). Re-check on v2.8.0. |

---

## 5. What is good (keep)

- The rank table plus an *executable* proof (archtest checks the table itself) is the right enforcement style; the matrix is internally consistent.
- Consumer-side interfaces for `CommitLister`, `Gate`, `PlanSink`, `Tracker`, `Verifier`, plus bridges in a single aggregator, are idiomatic Go ("accept interfaces, return structs") once F-01 is resolved.
- Immutable snapshot behind `atomic.Pointer`, one publish point, per-request single load, versions not pointers in long-lived goroutines: correct, measured, and tested under `-race`.
- No `Clock` interface in favour of `testing/synctest` is a sound modern call (verified against the 1.27 notes).
- Level-triggered planning with webhooks as accelerators removes the need for a durable job log; this is the main reason the single-writer SQLite store is enough.
- The goroutine ownership table (10.3), `run()` returning an error, `signal.NotifyContext` plus `stop()` for a second signal, and `Run(ctx) error` per component follow the Go conventions.
- `Secret`, `SafeHTML`, `freshness.Value[T]`, typed IDs: correct by construction where the type system can enforce it (see F-18, F-22 for where it cannot).

---

## 6. Verification log (facts I re-checked; date 2026-10-14)

| Fact | Result | Source |
|---|---|---|
| Go 1.27: `testing/synctest.Sleep`, `httptest.NewTestServer`, `Server.MaxHeaderValueCount`, `goroutineleak` profile GA, `encoding/json` backed by v2 | Verified | https://go.dev/doc/go1.27 |
| Go 1.26: `NotifyContext` cancels with a cause naming the signal; Green Tea GC on by default; ServeMux trailing-slash redirects use 307; `errors.AsType`; `new(expr)` | Verified | https://go.dev/doc/go1.26 |
| `singleflight.Do/DoChan/Forget` take no context; duplicates receive the same results | Verified (doc says nothing about slice copying) | https://pkg.go.dev/golang.org/x/sync/singleflight |
| oapi-codegen std-http generated `ServeMux` interface = `HandleFunc` + `http.Handler`; routes registered with `m.HandleFunc(method+" "+BaseURL+path, ...)` | Verified on generated smoke output at `main`; **not** verified for the pinned v2.8.0 tag | https://github.com/oapi-codegen/oapi-codegen (`internal/test/servers/routers/smoke/std-http/out.gen.go`) |
| `PRAGMA query_only` blocks CREATE/DELETE/DROP/INSERT/UPDATE; docs do not mention `VACUUM INTO` | Verified; **`VACUUM INTO` under `query_only` unverified** | https://www.sqlite.org/pragma.html |
| `VACUUM INTO` is "transactional" (consistent snapshot) and, unlike `VACUUM`, is not described as a write operation on the source; locking behaviour in WAL not stated | Verified / **unverified for blocking** | https://www.sqlite.org/lang_vacuum.html |
| Prototypes pass `go test -race -count=3` (Go 1.27.1, darwin/arm64) | Run locally | `05-research/05-store-sync-engine.assets/{engine,readmodel}` (copied to scratch, unmodified) |
| Zombie `Put` after `Remove` on the prototype `readmodel`; `OnParked` calling `Enqueue` deadlocks the prototype `engine` | Reproduced locally with two extra tests in the scratch copies | scratch only (nothing written to the design package except this report) |
| Library versions in Appendix A (modernc 1.60.1, goldmark 1.8.6, oapi-codegen 2.8.0, TanStack, React 19.3, distroless digest, etc.) | **Not re-verified by me**: outside the Go-idiom brief; carried as "per architecture 0 verification log and research" | n/a |

Things I could not verify and did not guess: oapi-codegen v2.8.0's exact generated `ServeMux` shape; whether SQLite allows `VACUUM INTO` on a `query_only` connection; WAL blocking behaviour of `VACUUM INTO` on the modernc build; whether ServeMux path-cleaning redirects changed status in 1.26; how GitHub App installation tokens behave against the async SBOM endpoints (already open item S-A).

---

## 7. Recommended order of fixes

1. F-01 (blocker) and F-02, F-11: rewrite 8.3/8.7/8.14 so every signature compiles; add a `go vet`-checked `doc_signatures_test` in the M0 spike that compiles the section 8 interfaces against stub types.
2. F-04, F-03, F-24: complete the matrix; decide the telemetry import policy; re-run the acyclicity script (I can supply it) as part of the table review.
3. F-05, F-06, F-07, F-08, F-12: fix the engine/planner/publisher/lifecycle contracts before `20-design/04-refresh-webhook.md` and `20-design/03-readmodel-store.md`.
4. F-09, F-10, F-14, F-15: the concurrency hazards in adapter and docs/manifests design.
5. Everything MINOR in one editing pass.

---

## Reviser response (2026-10-14)

Verdict on the review: sound and unusually well evidenced. Every finding was checked against the architecture, the ADRs and (for F-05, F-08) the prototype source; F-11 and F-24 were re-verified on primary sources (oapi-codegen **v2.8.0 tag**, go.dev/doc/go1.26); F-20 was reproduced locally (SQLite 3.54.0: `VACUUM INTO` fails under `PRAGMA query_only=1` and works on a read-only connection). All blockers and majors, and every minor except those below, are fixed in `10-architecture.md` (see its Revision log, table R.1, for the section of each fix) and in the ADR amendments. Only the items below are rejected or changed.

**F-10 (accepted problem, different fix).** The review suggested rendering non-README documents on demand from the store payload through a bounded `docs.Cache`. That puts a SQLite read on the request path, which D-02 forbids ("never on the request path"), and 04 allows changes to a decision only with evidence in a deviation. The defect (read-path eviction state cannot live in an immutable snapshot, a GET must not depend on a background job) is real and is removed differently: the docs job decides residency **at publish time** by deterministic admission (nav order, per-repository and global byte budgets), and a document that is not admitted is a visible `not_resident` state with its GitHub URL, never a `202 loading`. Nothing on the request path reads the store.

**F-13 (partly rejected).** Accepted: bridges leave `overview` for `cmd/scp/internal/bridge`; `httpapi` is split into `httpapi`, `httpapi/authz`, `httpapi/validate` (a feature no longer links kin-openapi or `httpx`); `GroupMeta` is defined; `overview` gets an explicit exit condition. Rejected for now: (a) renaming `scm`. 04 D-17 names the provider-neutral model `scm`, and `Slug`, `GroupID`, `Membership` are the identity of a repository read by at least three features, so they belong in a shared kernel; a separate `portal` package would be one more rank-0/1 package with the same readers. A concrete split point is written into ADR-001 (about 30 exported types or a second provider). (b) Splitting `overview` now. After the bridges leave, what remains is one purpose, "read composition for humans and agents", and splitting it before it grows adds a package and a rank row with no second reason to change; ADR-001 states the split condition (state, jobs, store or upstream access, about 2,500 lines, or a second reason to change).

**F-25 (partly rejected).** Accepted: the change ring, `Changed`, `Subscribe` and `ChangesSince` move to v1.1 with SSE; `DependencySource` is created with the first provider. Rejected: (a) removing `gitsource` from the v1 documents: 04 D-17 (binding) says git mode is "specified in the design", and a separate review found that a package name and a row did not meet that, so section 8.15 now specifies it and the row is flagged `planned`; (b) removing `StatusProvider`: it has two mandated implementations (Jenkins, Snyk), so GR-04's "second reader" test is met; (c) dropping `routes.Table.Class/Owner`: they are read by `Verify()`, by the reserved-prefix computation and by the visibility enumeration test, not only by tests of the table.

**F-26 (partly rejected).** The `Fan` has no per-reactor timeout: Go cannot preempt a blocked function, so a timeout would only abandon the goroutine; the control is the contract (no I/O, non-blocking `Enqueue`) plus a test with a deadline assertion. Publish coalescing is documented with its trigger (profile evidence or the 10,000-repository exit condition) rather than built, because the measured cost at target scale is small (0.31 ms index rebuild).

**F-22 (accepted as rule, not type).** Immutability stays a convention backed by `Put` taking ownership, producer-side clones and a `-race` property test; deep immutability in Go's type system would need accessor-only types everywhere, which the review itself judged disproportionate. The one place where sharing is real, the memoised `scm.Tree`, is made structurally immutable.

**Unverifiable items noted by the review stay unverified and are tracked:** modernc `mode=ro` URIs (W21), planner/lifecycle prototypes (W22).
