# 20-design / 04 - Sync engine and webhook receiver (`internal/refresh`, `internal/webhook`)

Status: design for implementation. Date: 2026-10-14. Unit key `04-refresh-webhook`.
Conforms to `04-decisions.md` (D-03, D-09, Q-09, Q-15, Q-24, amendments A-03, A-04, A-11, A-20, A-21), `10-architecture.md` (sections 4, 5, 6, 8.3, 8.6, 8.7, 10, 11, 15-18, 20-23) and ADR-003, ADR-015, ADR-024, ADR-025. Cross-unit contracts it depends on are those of `01-kernel.md` (1-based `job.Priority`/`job.Scope`, `job.GlobalKey`, JB-05 classification precedence), `06-httpapi.md` (`Problem.Header`), `02-platform` (`httpx.Limiter`, `ClientIPResolver`), `05-github-adapter.md` and `08-catalog.md`; every place where they disagree with this unit is listed in section 14 (X1..X6).

This document is self-sufficient: an engineer who has never seen this conversation can implement both packages from it. A throw-away reference prototype that compile-checks and tests every contract below lives next to it in `20-design/04-refresh-webhook.assets/` (README there). **What was compile-checked and executed** is listed in section 12.1; the one-line version: all exported signatures in this document are the output of `go doc` on that module (Go 1.27.1, `go vet` clean), and 62 tests (31 refresh, 21 webhook, 7 reference verifier, 3 e2e) plus 2 fuzz targets pass under `-race -count=10`. The document and the prototype were independently verified on 2026-10-14; 17 defects found in the first draft are fixed in both (the Verification log at the end; report in `30-verification/design-04-refresh-webhook.md`).

Conventions: "MUST/MUST NOT" are binding for the implementation; "design choice" marks a decision taken here where the architecture was silent; "Deviation Dn" points at section 14; "Question Qn" at section 15. Digits like `scp_sync_jobs_total` are metric names (section 9).

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

`internal/refresh` keeps every fetched item fresh without anything else knowing how: it is the process-wide **work engine**. Features register `job.Spec`s ("bring this item up to date"); the engine runs them with keyed coalescing, per-upstream concurrency and pacing, a budget gate, typed failure handling (retry with jittered backoff, rate-limit pause, parking, drop), a per-job deadline, and an orderly shutdown. A level-triggered **planner** decides *when* an item is due; a small **scheduler** runs local housekeeping.

`internal/webhook` is the **only inbound accelerator**: it accepts GitHub deliveries on the public listener, proves they are authentic, drops duplicates, hands the normalised `scm.Event` to the wired fan-out (`reactor.Fan`) and answers with a status code that tells GitHub the truth. An optional job (`webhook.redelivery`) recovers deliveries that failed while SCP was down or saturated.

Product intent: "GitHub pushes make the portal fresh within seconds", "data refreshes on its own" (every kind, GitHub data as well as badge statuses), "background work with retry" (GR-21, GR-24, GR-26).

### 1.2 What is in scope

| Area | In this unit |
|---|---|
| Queue | keyed table, coalescing, dirty-while-running, priorities, per-upstream ready/delayed heaps, `MaxKeys`, `ErrQueueFull`, `ErrClosed` |
| Dispatch | concurrency, pace, budget `Gate` (the reserve arithmetic is the adapter's, CD-CF-02), rate-limit pause, per-job `Timeout`, stuck detection, heartbeat |
| Failure handling | classification by behaviour interface, full-jitter backoff with floor, exhaustion, parking with 24 h probe, `Gone`, panic containment |
| Planner | level-triggered due-scan, TTL + jitter + idle back-off, `inflight` marking, cold-start priority, restore/persist through `PlanSink`, progress counters |
| Scheduler | fixed-delay jittered local tasks, skip-if-running, completion-time bookkeeping |
| Readiness contribution | what the engine and planner expose so `ops` can compute `/readyz` and `/livez` (it does **not** own the gates, architecture 10.2) |
| Shutdown | `Run` contract: stop dispatching, grace drain, cancel, abandon |
| Webhook receiver | routes, source resolution, body limits, signature **contract** (reference implementation in the assets), de-duplication, status mapping, event routing contract, metrics |
| Redelivery | `webhook.redelivery` global job, watermark, resync event on retention gap |

### 1.3 Non-goals

- No persisted job log, no persisted webhook intents (ADR-003, risk W12): a crash between 202 and processing is repaired by the planner within the item's TTL.
- No multi-replica semantics, no leader election, no distributed locks (Q-10).
- No GitHub knowledge inside `refresh` (G8): keys are strings, errors are classified by behaviour interfaces, events arrive only as `Enqueue` calls.
- No feature logic in `webhook`: it does not look at the catalogue, paths, branches or repositories. Resolving "is this repo ours, is this the default branch, did a doc path change" is the reactors' job (architecture 6.4, GR-21).
- No second webhook parser: payload decoding and the HMAC check live in `github.Verifier` (ADR-004); this unit owns the **contract** and a reference implementation used by its tests.
- No AIMD concurrency control for secondary limits in v1 (ADR-015: phase 2); the pause and the gate are the v1 protection.
- No per-installation pause (open question Q1): the engine pauses a whole upstream.

### 1.4 Directories and files (exact)

```
internal/refresh/
  engine.go        Engine, entry states, Enqueue, dispatch, finish, Run, Inspect, Parked
  backoff.go       Backoff.Delay (saturating full jitter with floor)
  gate.go          Gate (interface only; reserves live in github.Client.Admit, CD-CF-02)
  planner.go       Planner, PlanSink, PlanState, Progress
  scheduler.go     Scheduler, Task, TaskStatus
  deps.go          Deps, Tunables, Option and the With* options
  metrics.go       instrument names (constants) and creation from the injected Meter
  *_test.go        section 12
internal/webhook/
  receiver.go      Receiver, Config, Sources, Verifier/Sink/Limiter interfaces, status mapping, Stats
  dedupe.go        bounded delivery-id set
  redelivery.go    Redeliverer, Watermarks, Redelivery (the job), RedeliveryConfig
  *_test.go        section 12
```

(The prototype names its receiver file `webhook.go`; use `receiver.go` as in architecture section 7.)

Nothing else is owned: no SQL tables (section 5), no OpenAPI fragment (the webhook has no JSON API surface: route class *server-only*, architecture 11.2), no SPA code. Bridges that these packages need live in `cmd/scp/internal/bridge` and are specified here as contracts (section 2.4) but implemented by that unit's owner.

---

## 2. Position in the architecture

### 2.1 Allowed imports (architecture 5.2); no deviation needed

| Package | Rank | May import (internal) | Third party | What this design actually imports |
|---|---|---|---|---|
| `refresh` | 2 | `job`, `freshness`, `platform/logx` | `x/sync`, `x/time/rate`, otel API | `job`, `freshness` (only through `job.Spec.Policy`); stdlib; `go.opentelemetry.io/otel/{attribute,codes,metric,trace}` API modules (+ `metric/noop`, `trace/noop` for the zero `Deps`). It does **not** import `logx`: it logs through the injected `*slog.Logger` with `LogAttrs(ctx, ...)` so the `logx` handler adds `trace_id`; `x/sync` and `x/time/rate` are not needed. |
| `webhook` | 4 | `scm`, `job`, `httpapi`, `platform/logx` | otel API | `scm`, `job`, `httpapi`; stdlib; otel API. Same logging rule as `refresh`. It does **not** import `freshness`: the redelivery `job.Spec` is built by assigning `sp.Policy.TTL`/`Jitter` through the selector (a field of a struct value can be set without naming its type), so the first draft's Deviation D1 is withdrawn (section 14). |

`go list` of the prototype's own imports (re-run by the verifier and kept as the test `e2e.TestImportsStayInsideTheMatrix`): `refresh` -> {`job`}; `webhook` -> {`httpapi`, `job`, `scm`}; both are subsets of their rows of 5.2 (`freshness` arrives only transitively through `job`, which is allowed). Test files may additionally import `freshness`, `ghverify` and test helpers (5.2, last paragraph).

Nobody else may import `refresh` except `cmd/scp` and `cmd/scp/internal/bridge` (feature rows exclude it). Nobody else may import `webhook` except `cmd/scp` (and `ops` through a bridge for `Stats`).

### 2.2 Who imports whom at run time (wiring)

```
cmd/scp ──► refresh.Engine, refresh.Planner, refresh.Scheduler
        ──► webhook.Receiver, webhook.Redelivery
bridge  ──► refresh.PlanSink impl, refresh.Budget impl, WithOnParked/Unparked persistence,
            catalog.Tracker (planner.SetSubjects), ops.Retrier (planner.Enqueue force=true: the PLANNER, not the engine),
            webhook.Watermarks, ops view of webhook.Stats and engine.Inspect
httpx   ──► *httpx.Limiter satisfies webhook.Limiter DIRECTLY and (*httpx.ClientIPResolver).ClientIP is the clientIP func
            (both exist in the 02-platform prototype; no bridge)
features ──► job.Enqueuer (they receive *refresh.Planner, section 2.5), job.Spec via Jobs()
github  ──► (nothing from here) satisfies webhook.Verifier, webhook.Redeliverer structurally
```

### 2.3 Consumer-defined interfaces this unit **requires** (exact signatures)

`refresh` consumes (declared in `refresh`):

```go
// refresh/gate.go
type Gate interface {
	Admit(upstream string, p job.Priority) (wait time.Duration)
}
type Budget interface {                                  // D3: satisfied by a bridge over github rate state
	Remaining(upstream string) (s BudgetState, ok bool)
}
// refresh/planner.go
type PlanSink interface{ SaveState(k job.Key, st PlanState) } // satisfied by a bridge that batches store.SavePlans
```

`webhook` consumes (declared in `webhook`):

```go
type Verifier interface { // satisfied DIRECTLY by *github.Verifier (kernel types only)
	Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error)
}
type Sink interface { // satisfied by *reactor.Fan
	Handle(ctx context.Context, ev scm.Event) error
}
type Redeliverer interface { // satisfied DIRECTLY by *github.Client
	FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error
}
type Limiter interface{ Allow(key string) bool }          // satisfied DIRECTLY by *httpx.Limiter built for class ratelimit.webhook_invalid
type Watermarks interface {                               // bridged over store.LoadTasks / store.SaveTask
	Load(ctx context.Context, name string) (time.Time, error)
	Save(ctx context.Context, name string, t time.Time) error
}
```

`Verifier`, `Sink`, `Redeliverer` are exactly the signatures of architecture 8.7. `Limiter` and `Watermarks` are additions (D4) because `webhook` may not import `httpx` or `store`; `Limiter` needs no bridge (`httpx.Limiter.Allow(key string) bool` is the same signature, kernel types only), `Watermarks` does (it speaks `store.LoadTasks/SaveTask`).

### 2.4 Interfaces this unit **provides** (what others call)

- `*refresh.Engine` and `*refresh.Planner` satisfy `job.Enqueuer` (architecture 8.3): `Enqueue(k job.Key, p job.Priority, notBefore time.Time, force bool) error`.
- `*refresh.Engine.Observe(planner.OnResult)`, `Inspect() Stats`, `Parked() []ParkedKey` for `ops` (through the `ops.Retrier`/status bridges).
- `*webhook.Receiver` is an `http.Handler`; `webhook.Receiver.Stats()` for `/status`.
- `webhook.Redelivery.Jobs() []job.Spec` (the `Jobs()` convention of architecture 6.3).

`cmd/scp/assert.go` (compile-time proofs added by this unit, verified in the prototype `internal/e2e/assert_test.go`):

```go
var (
	_ job.Enqueuer     = (*refresh.Engine)(nil)
	_ job.Enqueuer     = (*refresh.Planner)(nil)
	_ refresh.Gate     = bridge.RateGate{}   // CD-CF-02: the bridge maps job.Priority onto github.Client.Admit(ReserveClass)
	_ webhook.Verifier = (*github.Verifier)(nil)
	_ webhook.Redeliverer = (*github.Client)(nil)
	_ webhook.Sink     = (*reactor.Fan)(nil)
	_ refresh.PlanSink = (*bridge.PlanSink)(nil)
	_ refresh.Budget   = (*bridge.RateBudget)(nil)
	_ webhook.Limiter  = (*httpx.Limiter)(nil)          // direct, no bridge
	_ webhook.Watermarks = (*bridge.Watermarks)(nil)
)
```

### 2.5 Wiring contract (what `cmd/scp` must do, in this order)

```go
// wire_jobs.go (steps 6 and 8 of architecture 10.1). ORDER MATTERS: the specs are registered LAST, because the
// webhook redelivery spec comes from an object that needs the reactor fan, which needs the planner (a first draft
// registered the specs before the redelivery object existed, so its own job was never registered).
eng := refresh.New(refresh.Backoff{Base: c.Backoff.Base, Cap: c.Backoff.Cap, MaxAttempts: c.Backoff.MaxAttempts}, seed,
	refresh.WithDeps(deps), refresh.WithGrace(c.Grace), refresh.WithMaxKeys(c.MaxKeys),
	refresh.WithGate(bridge.NewRateGate(ghClient)),   // CD-CF-02 (X6): reserves are github.defaults.budget.reserve.*, not refresh.gate.*
	refresh.WithOnParked(parkedSink.Park), refresh.WithOnUnparked(parkedSink.Unpark),   // store.Park / store.Unpark
	refresh.WithRestoreParked(parkedFromStore),                                        // store.LoadParked
	refresh.WithOnExhausted(func(k job.Key, n int, err error) { /* log only: the engine already counted */ }))
for name, u := range c.Upstreams { // github, jenkins, snyk, maven-central, local: every upstream a Spec names, BEFORE Register
	eng.AddUpstream(name, refresh.UpstreamConfig{MaxConcurrent: u.Concurrency, Pace: u.Pace, JobTimeout: u.JobTimeout})
}

planner := refresh.NewPlanner(eng, planSink, seed, refresh.WithDeps(deps))
eng.Observe(planner.OnResult)                           // once, before Run
enq := planner                                          // THE job.Enqueuer handed to reactor.New, every feature, ops.Retrier and the first jobs below
fan := reactor.New(enq, reactorDeps, reactors...)       // features are constructed with enq before this line; their Jobs() are collected below

recv, err := webhook.NewReceiver(c.Webhook, webhook.Sources{IDs: enabledSourceIDs, Default: defaultSource}, ghVerifier, fan,
	invalidSigLimiter /* *httpx.Limiter, class webhook_invalid */, clientIPResolver.ClientIP, deps)
redelivery := webhook.NewRedelivery(redeliverySources, ghClient, fan, watermarks, deps)

allSpecs := append(featureSpecs, redelivery.Jobs()...)  // redelivery.Jobs() is nil when no source enabled it
for _, s := range allSpecs {
	if err := s.Validate(); err != nil { return err }    // unit 01 JB-03: every violation listed, startup aborts
	eng.Register(s)                                     // panics if s.Upstream was not added
	planner.Track(s)
}
planner.Restore(statesFromStoreLoadPlans)               // warm start; works before or after SetSubjects, before the first Tick

sched := refresh.NewScheduler(seed, refresh.WithDeps(deps))
sched.Add(refresh.Task{Name: "planner.tick", Every: c.PlannerTick, Jitter: 0.1, Run: func(context.Context) error { planner.Tick(time.Now()); return nil }})
// store.optimize, pages.reload: other units add their own local Tasks; Task.OnDone persists store.SaveTask

// catalog's Tracker bridge: for each kind: planner.SetSubjects(kind, keysOf(kind, repos)); then sched.Trigger("planner.tick")

// first job of the process: through the PLANNER (it marks the idle item in flight; test TestFirstGlobalJobThroughPlannerIsNotRunTwice)
_ = planner.Enqueue(job.GlobalKey("catalog.reconcile"), job.PrioWebhook, time.Time{}, false)
// webhook.redelivery needs no manual first enqueue: Track makes every global kind due at once and the first planner tick
// starts it; to delay it, planner.Restore({job.GlobalKey("webhook.redelivery"): {NextDue: now.Add(30 * time.Second)}}).
```

**Why the planner, not the engine, is the `Enqueuer` of reactors (D2).** `Planner.Enqueue` marks a tracked idle item `inflight` *before* forwarding to the engine (marking afterwards races with a fast completion, which was a bug found while prototyping). Without it, a webhook-triggered run that is still in flight when the next 15 s tick arrives is seen as "due" and the tick enqueues it again, which the engine turns into one `dirty` follow-up run (test `TestPlannerWebhookRunInFlightIsNotTickedAgain`). `*refresh.Engine` still satisfies `job.Enqueuer` exactly as architecture 6.3 says; this is a wiring choice, not a signature change. The same rule applies to every caller that is not a Spec's own `Run`: the first `catalog.reconcile`, the `ops.Retrier` bridge (`force=true`) and any feature that enqueues a sibling job. Handing a global kind to the engine directly lets the first 15 s tick enqueue the still-running item again, which costs a full second reconcile (test `TestFirstGlobalJobThroughPlannerIsNotRunTwice` shows both outcomes: 1 run through the planner, 2 through the engine).

**Stage and timeouts.** The `refresh` lifecycle stage runs `eng.Run` and `sched.Run` concurrently (joined error). Its `StopTimeout` MUST be `Grace + AbandonAfter + 1 s` = 19 s (D9: architecture 8.16 point 4 says "Grace (15 s) is the StopTimeout", which would cut off the abandon window). Worst-case shutdown becomes 5 + 15 + 19 + 1 + 2 = 42 s, inside `terminationGracePeriodSeconds: 45`. `Engine.Run` returns `ErrAbandoned` when a job ignored cancellation for `AbandonAfter`; `lifecycle.Run` joins it into its result, so the process exits non-zero (deliberate: a leaked job is visible to the supervisor) after the remaining stages stopped normally. The scheduler stops immediately at the stage context, i.e. **the planner stops ticking during the 15 s grace**: in-flight jobs finish, nothing new is planned.

### 2.6 What this unit requires from other units

| Unit | Requirement |
|---|---|
| `github` | `Verifier.Verify` follows the contract of section 4.12: error behaviour interfaces `Unauthenticated() bool` (the receiver also honours unit 05's spelling `InvalidSignature() bool`, X1), `Malformed() bool`, `Ignored() bool`; signature before decode; `X-GitHub-Delivery` required and matching `[0-9A-Za-z-]{1,64}` (the receiver refuses anything else with 400); `PathsComplete` rules including forced and commit-less pushes; decode of every event of the 4.12 table. `Client.FailedDeliveries` returns **one id per GUID, OLDEST first** (X3), only GUIDs without a successful attempt since `since`, at most 3 redelivery attempts per GUID, and returns its partial result together with an error satisfying `Truncated() bool` when it stopped at its page cap (X4). `Client.Redeliver` maps "no such delivery" to a `job.Gone` error, rate limits/5xx to `job.RateLimited`/transient errors as in architecture 15, and **a 422 is not success** (X5). |
| `reactor` | `Fan.Handle` returns `errors.Join` of reactor errors; `job.ErrQueueFull`/`job.ErrClosed` stay visible to `errors.Is` through the join (verified in `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted`); a malformed `Repo` is dropped whole with an `scm.ErrInvalid` error (unit 01 RA-01a), which is **not** a refusal and therefore answers 202. Every reactor MUST ignore an event with a zero `Repo` unless its kind is installation-level (`installation`, `installation_repositories`, including the synthetic `resync`): a zero `RepoRef` would otherwise become the job key `commits.refresh/...` of nothing. |
| `catalog` | `React` treats `scm.EventInstallationRepos` with `Action == "resync"` and a zero `Repo` as "full reconcile of `ev.Source`" (used by the redelivery gap path); `Tracker.SetRepos` calls `Planner.SetSubjects` per kind with the most recently pushed repositories first, then `Scheduler.Trigger("planner.tick")`. |
| every feature with a `Jobs()` | specs name an upstream known to the engine; `Run` maps `scm.ErrNotFound`-on-the-repo, `readmodel.ErrUnknownRepo`, `store.ErrNoRepo` to a `job.Gone` error and `readmodel.ErrInvalidSnapshot` to `job.Permanent` (architecture 8.3, 15). Reactors enqueue with `PrioWebhook` and pass `force=true` only when the event proves content changed (push, head_changed), `false` for everything else. |
| `ops` | `/livez` rule of section 4.11; `/status` renders `Engine.Inspect`, `Engine.Parked`, `Planner.Progress`, `Receiver.Stats`. |
| `platform/config` | decodes `time.Duration` strings and IEC sizes (`25MiB`) into `int64`; secrets as `secret.Secret` from `*_FILE`. |
| `httpapi` | `WriteProblem` and `Problem{Status, Code, Title, RetryAfterSeconds, Header}` of unit 06: **`Retry-After` is written only from `Problem.Header`** (RetryAfterSeconds is a body member, 06 section 4.1), so the receiver sets both together; codes `unsupported-media-type` (already in 06) and `overloaded`, `request-timeout` (open enum in 06, added by this unit, D5). |
| `platform/httpx` | `*httpx.Limiter` (`Allow(key string) bool`, class `ratelimit.webhook_invalid`) and `(*httpx.ClientIPResolver).ClientIP` as the receiver's `lim` and `clientIP`; `ClientIP` returns `""` for an unparsable peer, which shares one bucket (acceptable: it is the failure path). |

---

## 3. Domain model and Go types

All types below are the compile-checked prototype (Appendix A prints the full `go doc` output).

### 3.1 `refresh`: engine

```go
type UpstreamConfig struct {
	MaxConcurrent int           // jobs running at once for this upstream; >= 1 (github default 4)
	Pace          time.Duration // minimum spacing between job STARTS; >= 0 (github default 100ms)
	JobTimeout    time.Duration // per-run deadline when Spec.Timeout == 0; > 0 (default 2m)
}
type Backoff struct{ Base, Cap time.Duration; MaxAttempts int } // defaults 2s, 15m, 8 (zero values are replaced by these)
func (b Backoff) Delay(attempt int, r *rand.Rand) time.Duration // attempt 1 = first retry
func New(b Backoff, seed uint64, opts ...Option) *Engine
func (e *Engine) AddUpstream(name string, cfg UpstreamConfig)  // before Run; panics after
func (e *Engine) Register(s job.Spec)                          // before Run; panics if Kind/Run empty or Upstream unknown
func (e *Engine) Observe(f func(k job.Key, o job.Outcome, err error)) // exactly once, before Run; panics otherwise
func (e *Engine) Enqueue(k job.Key, p job.Priority, notBefore time.Time, force bool) error
func (e *Engine) Run(ctx context.Context) error                // blocks; returns nil or ErrAbandoned
func (e *Engine) Inspect() Stats
func (e *Engine) Parked() []ParkedKey                          // oldest first
```

Field semantics:

- `Spec` (kernel, architecture 8.3): `Kind` is the part of the key before the first `/`; `Upstream` selects the heap set, pace and concurrency; `Prio` is the priority of planner-driven runs; `Policy` is read by the **planner**, never by the engine; `Timeout` 0 means the upstream's `JobTimeout`; `Scope` is read by the planner only.
- Keys: `job.NewKey(kind, parts...)`; per-repo keys are `kind/<source>/<owner>/<name>` (`scm.RepoRef.Key()` tail, lower-cased by unit 01 SC-04), global keys are `job.GlobalKey(kind)` = `kind/_`.
- **Priorities and scopes are 1-based in the kernel** (unit 01: `PrioInteractive = iota + 1`, `ScopeRepo = iota + 1`; the zero value is invalid on purpose). The engine never uses `uint8(p)` as an index: `classOf(p) = int(p) - int(job.PrioInteractive)` and `prioOf(c)` are the only conversions (the first draft indexed with the value and would panic with index out of range on `PrioBackfill`; test `TestPrioritiesAreOneBasedAndZeroIsInvalid`). `Planner.Track` panics on an invalid `Scope`; `cmd/scp` calls `Spec.Validate` (JB-03) first, so this is a second line of defence.
- `Outcome.Unchanged` drives idle back-off in the planner; `Outcome.NextDue` overrides the planner's computed next run (used by jobs that know better, e.g. a status provider with `Retry-After`).
- Zero-value behaviour: `Backoff{}` is valid (defaults). `New` with no options is valid for tests (no gate, discard logger, no-op instruments). `Enqueue` with `notBefore` zero or in the past means "now". `Enqueue` before `Run` is allowed (entries wait for the dispatcher). A `Priority` above `PrioBackfill` is clamped to `PrioBackfill`; a `Priority` below `PrioInteractive` (the unset zero value) returns `ErrInvalidPriority` (a wiring bug, like `ErrUnknownKind`: not a refusal, so a webhook still answers 202).

Errors (all sentinel values, `errors.Is`):

```go
var (
	ErrUnknownKind     error // Enqueue: no Spec for the key's kind (a wiring bug, returned not panicked: a webhook must not crash the process)
	ErrUnknownUpstream error // Enqueue: unreachable after Register's check, kept as a defensive guard
	ErrInvalidPriority error // Enqueue: priority below job.PrioInteractive (unset)
	ErrAbandoned       error // Run: jobs still running AbandonAfter after the grace cancel
)
// from job: job.ErrQueueFull (len(keys) >= MaxKeys and the key is new), job.ErrClosed (after shutdown began)
type ParkedKey struct{ Key job.Key; Since, RetryAt time.Time; Reason string }
type ParkedError struct{ ParkedKey }   // satisfies job.Permanent; only ever passed to the observer
```

`Stats` (for `/status`, `/livez`, tests):

```go
type Stats struct {
	At        time.Time         // snapshot time
	Heartbeat time.Time         // last dispatcher pass; /livez requires it fresher than 30 s (3 x Heartbeat)
	Keys      int               // keyed table size (queued + running)
	Parked    int
	Upstreams map[string]UpstreamStats
	Jobs      map[string]uint64 // cumulative by outcome: started success coalesced marked_dirty retried ratelimited parked parked_suppressed panic exhausted gone canceled queue_full
}
type UpstreamStats struct {
	Ready, Delayed, Running, Stuck, MaxConcurrent int
	PausedUntil     time.Time // zero when not paused
	AllStuckSince   time.Time // zero unless every slot holds a stuck job (the /livez signal)
	RateLimitStreak int       // consecutive rate-limit results without a success
}
```

Options and tunables:

```go
type Option func(*options)   // shared by New, NewPlanner, NewScheduler; options that do not apply are ignored
func WithDeps(d Deps) Option; func WithGate(g Gate) Option; func WithGrace(d time.Duration) Option
func WithMaxKeys(n int) Option; func WithTunables(t Tunables) Option // WithTunables overrides only the NON-ZERO fields (a zero Heartbeat would spin the dispatcher)
func WithOnParked(f func(ParkedKey)) Option; func WithOnUnparked(f func(k job.Key, reason string)) Option
func WithOnExhausted(f func(k job.Key, attempts int, err error)) Option; func WithRestoreParked(p []ParkedKey) Option

type Deps struct{ Log *slog.Logger; Meter metric.Meter; Tracer trace.Tracer } // otel API types only (G11)
type Tunables struct {   // defaults from DefaultTunables(); config keys only where section 6 says so
	Grace time.Duration        // 15s    in-flight drain after shutdown starts
	AbandonAfter time.Duration // 3s     extra wait after Grace + cancel
	MaxKeys int                // 50000
	ParkRetry time.Duration    // 24h    retry_at probe interval of a parked key
	StuckAfter time.Duration   // 5s     a job still running this long past its deadline is Stuck
	GateRecheck time.Duration  // 5s     upper bound of one gate denial before asking again
	Heartbeat time.Duration    // 10s    the dispatcher wakes at least this often
	MaxPause time.Duration     // 65m    clamp for a far-future RetryAt
	RateLimitMin time.Duration // 60s    floor of a repeated rate-limit pause when RetryAt is not in the future
	RateLimitMax time.Duration // 15m    cap of that floor
}
```

Hooks are the **only** way the engine talks to the outside during a state transition: `Observe`, `WithOnParked`, `WithOnUnparked`, `WithOnExhausted`. Invariant (architecture 8.6 point 1, test `TestHooksRunOutsideTheMutex`): every hook runs on the worker goroutine **after** the engine mutex is released and **after** the job's concurrency slot is released; a hook may call `Enqueue`/`Inspect`, may take a millisecond (the SQLite `Park` write), and must not block indefinitely. **A panicking hook is recovered** per hook (logged at ERROR `refresh hook panicked` with the stack; the remaining hooks and the engine continue): hooks run outside the job's own `recover`, so without this a bug in a bridge or in `Planner.OnResult` would kill the whole process (test `TestHookPanicIsContained`). The same holds for `Task.OnDone` in the scheduler. `Gate.Admit` is also never called under the mutex (section 4.4).

### 3.2 `refresh`: planner and scheduler

```go
type PlanState struct{ NextDue time.Time; UnchangedStreak int }   // what survives a restart
type PlanSink interface{ SaveState(k job.Key, st PlanState) }       // non-blocking, no error
type Progress struct{ Subjects, Completed, Inflight, Parked, Due int }
func NewPlanner(enq job.Enqueuer, sink PlanSink, seed uint64, opts ...Option) *Planner
func (p *Planner) Track(s job.Spec)                          // panics on a second Track of a kind; global kinds get subject "kind/_", due now
func (p *Planner) SetSubjects(kind string, keys []job.Key)   // per-repo kinds; order = tie-break of Tick; unknown kind ignored
func (p *Planner) Restore(states map[job.Key]PlanState)      // warm start; works before or after SetSubjects
func (p *Planner) Tick(now time.Time)
func (p *Planner) OnResult(k job.Key, o job.Outcome, err error)  // pass to Engine.Observe
func (p *Planner) Enqueue(k job.Key, pr job.Priority, notBefore time.Time, force bool) error // job.Enqueuer
func (p *Planner) Progress(kind string, now time.Time) Progress
```

Item state machine (per subject key): `idle(NextDue)` -> `inflight` (enqueued by `Tick` or by `Planner.Enqueue`) -> `idle(NextDue')` on success; stays `inflight` across transient failures and rate limits (the engine owns retries); -> `parked` on a permanent result (including `ParkedError`); removed on `Gone`; `idle(now)` on shutdown cancellation. `everDone` records "has completed at least once" (set by a success or by `Restore`).

```go
type Task struct {
	Name string; Every time.Duration; Jitter float64
	InitialDelay func(rnd *rand.Rand) time.Duration   // from persisted last_success so a restart resumes cadence
	Run func(ctx context.Context) error; Timeout time.Duration
	OnDone func(name string, st TaskStatus)           // after every run, outside the scheduler mutex (bridge: store.SaveTask) - addition D2
}
type TaskStatus struct{ LastAttempt, LastSuccess time.Time; LastErr string; Runs, Failures, Skipped, Overruns uint64 }
func NewScheduler(seed uint64, opts ...Option) *Scheduler
func (s *Scheduler) Add(t Task)               // before Run; panics after, on a duplicate name, Every <= 0 or nil Run
func (s *Scheduler) Trigger(name string) bool // false (counted Skipped) if running or unknown
func (s *Scheduler) Status(name string) (TaskStatus, bool)
func (s *Scheduler) Run(ctx context.Context) error // returns after ctx done and every task goroutine returned
```

### 3.3 `refresh`: gate

```go
type Gate interface{ Admit(upstream string, p job.Priority) (wait time.Duration) }
// CD-CF-02: Budget, BudgetState, Reserves, DefaultReserves and NewReserveGate were removed (X6 settled by unit 16 D-7): the 0.40/0.15/0.02
// arithmetic is github.Client.Admit(ReserveClass) of unit 05, keyed github.defaults.budget.reserve.{backfill,scheduled,webhook}.
```

`Admit` contract: called only by the dispatcher goroutine, never under a lock, at most once per `(upstream, priority class)` per dispatch pass, **for every class of every unpaused upstream that has ready or delayed work** (a delayed entry promoted in the same pass must not bypass the gate; the first draft asked only about classes whose ready heap was non-empty); MUST be cheap, non-blocking, MUST NOT call the engine. An entry that becomes ready between the `Admit` calls and the dispatch lock is deferred to the next pass (`next = now`), never admitted unasked. A nil gate admits everything. `bridge.RateGate` MUST answer 0 for an upstream without a budget (`jenkins`, `snyk`, `deps-dev`, `local`).

### 3.4 `webhook`

```go
type Config struct {                      // mounted at github.defaults.webhook (section 6)
	MaxBody       int64         `yaml:"max_body"`       // 25 MiB
	DedupeTTL     time.Duration `yaml:"dedupe_ttl"`     // 15m
	DedupeMax     int           `yaml:"dedupe_max"`     // 10000
	MaxConcurrent int           `yaml:"max_concurrent"` // 4 bodies read and verified at once
	AcquireWait   time.Duration `yaml:"acquire_wait"`   // 2s
	RetryAfter    time.Duration `yaml:"retry_after"`    // 5s
	BodyTimeout   time.Duration `yaml:"body_timeout"`   // 8s: the whole body must arrive within this (< GitHub's 10 s)
}
type Sources struct{ IDs []scm.SourceID; Default scm.SourceID } // Default "" => the source-less route answers 404
type Deps struct{ Log *slog.Logger; Meter metric.Meter; Tracer trace.Tracer }
func DefaultConfig() Config
func NewReceiver(cfg Config, srcs Sources, v Verifier, sink Sink, lim Limiter, clientIP func(*http.Request) string, d Deps) (*Receiver, error)
func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (rc *Receiver) Stats() Stats  // Stats{LastAccepted time.Time; Results map[string]uint64; DedupeSize int}

type RedeliveryConfig struct {            // mounted at github.sources[].webhook.redelivery
	Enabled bool `yaml:"enabled"`; Interval time.Duration `yaml:"interval"` /*6h*/; MaxPerRun int `yaml:"max_per_run"` /*50 (CD-CF-03)*/
	Rate time.Duration `yaml:"rate"` /*1s*/; MaxAge time.Duration `yaml:"max_age"` /*72h*/
}
type RedeliverySource struct{ ID scm.SourceID; Cfg RedeliveryConfig }
func DefaultRedelivery() RedeliveryConfig
func NewRedelivery(srcs []RedeliverySource, rd Redeliverer, sink Sink, wm Watermarks, d Deps) *Redelivery
func (r *Redelivery) Jobs() []job.Spec                     // nil when no source enabled redelivery
func (r *Redelivery) Run(ctx context.Context, _ job.Key) (job.Outcome, error)
const RedeliveryKind = "webhook.redelivery"
```

`NewReceiver` returns an error for a nil `Verifier`/`Sink`, an empty source set, a `Default` that is not in `IDs`, or any non-positive config value (test `TestNoDefaultSourceMeansSourcelessRouteIs404`). `lim` and `clientIP` may be nil (no invalid-signature throttling; tests). When **no** source has webhooks enabled `NewReceiver` is not called; `cmd/scp` then registers the same two patterns with a handler that answers the 404 `not-found` problem (section 7), so `routes.Table.Verify()` still finds every row.

### 3.5 Invariants (each has a test, section 12)

1. At most one keyed entry per `job.Key`; at most one run of a key at any time; every `Enqueue` that returns nil is followed by a run that **starts after the call** (a running job never "absorbs" a later enqueue: it is marked dirty).
2. `len(keys) <= MaxKeys`; the table cannot grow beyond the catalogue in normal operation, so `ErrQueueFull` is a bug detector, not a regular path.
3. A rate-limit result never consumes a retry attempt and never starts a job on a paused upstream before the pause ends.
4. A parked key never runs unless forced, or probed at `RetryAt`.
5. No hook and no `Gate.Admit` runs under a lock; the planner never holds its mutex across a call into the engine.
6. After `Run` has begun shutdown, `Enqueue` returns `job.ErrClosed`.
7. The webhook never parses, decodes or logs a body before `Verify` succeeded, never records a delivery id for a delivery that was answered 5xx, and never stores or echoes a delivery id that is not `[0-9A-Za-z-]{1,64}`.
8. A body slot is never held longer than `BodyTimeout` + the verification time (test `TestSlowBodyReleasesItsSlotAtBodyTimeout`).
9. One rate-limit event paused the upstream once: results of jobs that were already in flight when the pause began do not escalate the floor (test `TestRateLimitOneEventManyInFlightJobsDoesNotEscalate`).

---

## 4. Behavioural specification

### 4.1 Entry life cycle (engine)

An entry is `delayed` (in the upstream's delayed heap ordered by `runAt`), `ready` (in the upstream's ready heap of its priority class, FIFO by sequence number) or `running`. There are `numPrio = 4` ready heaps per upstream (one per `job.Priority`, indexed by `classOf(p) = int(p) - int(job.PrioInteractive)`, section 3.1), not one heap ordered by (priority, seq) as in the research prototype: a gate denial applies to a *class*, and skipping a class must be O(1) (design choice, `TestGateReservesPerPriority`).

### 4.2 `Enqueue(k, p, notBefore, force)`: state-by-event table

```
Enqueue(k, p, notBefore, force):                      -- never blocks; holds only the engine mutex
  if closed                    -> return job.ErrClosed
  spec := specs[k.Kind()];  nil -> return ErrUnknownKind
  if k is parked:
        if !force: count parked_suppressed; schedule observe(k, {}, &ParkedError{...}) after unlock; return nil
        else:      delete parked entry; schedule onUnparked(k, "forced") after unlock
  if p < PrioInteractive -> return ErrInvalidPriority     (unset zero value)
  now := time.Now(); notBefore = max(notBefore, now); p = min(p, PrioBackfill)
  cur := byKey[k]
  if cur == nil:
        if len(byKey) >= MaxKeys -> count queue_full; return job.ErrQueueFull
        create entry{prio: p, runAt: notBefore, seq: ++seq}; place(); poke dispatcher; return nil
  switch cur.state:
     running:            cur.dirty = true; cur.prio = min(cur.prio, p); count marked_dirty
     ready | delayed:    cur.prio = min(cur.prio, p)                     -- raise, never lower
                         if notBefore < cur.runAt && (!cur.inBackoff || force) { cur.runAt = notBefore; cur.inBackoff = false }
                         place(cur); count coalesced
  poke; return nil
```

Table (what each combination does; every cell has a test):

| Key state | `Enqueue` arrives | Job result |
|---|---|---|
| absent | create `ready` (notBefore <= now) or `delayed` | success: delete key |
| ready / delayed, plain | **coalesce**: raise priority, promote `runAt` to the earlier time | transient failure: `attempts++`, `runAt = now + backoff`, `inBackoff = true`, back to delayed |
| delayed **in backoff** | raise priority only; `runAt` promoted **only if `force`** (design choice: a webhook must not turn a 2 s..15 min backoff into a hammering loop; the retry is level-triggered and observes the newer state anyway; `TestBackoffEntryNotPromotedByEnqueueUnlessForced`) | |
| running | set `dirty` (counted `marked_dirty`), raise priority | success + dirty: **exactly one** follow-up run at `now`; any failure: `dirty` cleared (the retry itself picks up newer state) |
| parked | ignored unless `force`; the observer is told (`ParkedError`) so the planner learns the key is parked even after a warm start | |
| any | | rate limited: pause the whole upstream, no attempt consumed; permanent or panic: park, drop `dirty`; `Gone`: delete key, drop parked entry |

Why `dirty` exists: a run that began before the event may have read the upstream before the event happened; skipping the event would lose it, queuing N follow-ups would waste budget. Burst of 10 enqueues while queued = 1 run; 10 while running = 1 run + exactly 1 follow-up (`TestCoalesceQueuedAndDirtyWhileRunning`).

### 4.3 Dispatch (single dispatcher goroutine)

```
Run(ctx):
  started = true (second Run panics)
  hctx := WithCancel(WithoutCancel(ctx))          -- job context: outlives ctx by Grace
  loop:
    next := dispatch(); reset timer to next; wait on { ctx.Done, wake, timer }
  on ctx.Done: closed = true; wait for in-flight with Grace; hcancel(); wait AbandonAfter; return nil | ErrAbandoned

dispatch():
  now; waits := gateWaits(now)                    -- (1) OUTSIDE the lock
  lock
  heartbeat = now; next = now + Heartbeat
  if ctx.Err() != nil: unlock; return              -- draining: start nothing new
  (2) parked probes: for each parked key with RetryAt <= now and no live entry:
        create entry{probe:true, prio: spec.Prio, runAt: now}; then push every due parked RetryAt to now + ParkRetry
  for each upstream u (registration order):
    (3) promote delayed entries with runAt <= now into ready[prio]
    (4) stuck watchdog: for each running entry past deadline + StuckAfter and not yet marked: mark stuck, u.stuck++, count job_ignored_cancel, log ERROR;
        track u.allStuckAt (set when u.stuck == len(running) >= MaxConcurrent, cleared otherwise)
    (5) if now < u.pausedTill: next = min(next, u.pausedTill); continue
    (6) while len(u.running) < MaxConcurrent and now >= u.nextStart:
            pick the lowest-numbered priority class c with ready entries and NOT denied by waits[(u,c)]
               (a denied non-empty class sets next = min(next, now + min(wait, GateRecheck)))
            none -> break
            pop; state = running; deadline = now + (Spec.Timeout or u.JobTimeout); u.nextStart = now + Pace
            start worker goroutine; count started; record queue wait (now - runAt)
  unlock; run collected post-actions; return next

gateWaits(now):   -- needs the lock briefly to learn which upstreams are unpaused and have ready OR delayed work, releases it,
                  -- calls Gate.Admit once per (such upstream, EVERY class) and returns ALL answers, 0 included
pick (in dispatch): a non-empty class whose (upstream, class) answer is ABSENT was enqueued after gateWaits: skip it and set
                  next = now (asked on the next pass); a class with a wait > 0 is skipped with next = now + min(wait, GateRecheck)
```

Design choice (D-none, refinement): the gate answer is consulted *once per pass per class* (and, since the verification, for every class of an upstream with delayed work too: promotion from the delayed heap happens inside the same pass, test `TestGateAppliesToPromotedDelayedEntries`), so a pass may start up to `MaxConcurrent` jobs on one answer. Budget changes between starts are visible on the next pass (which every completion triggers). The reserves are arithmetic on a number that is itself one response stale; per-start precision would be false precision.

`Pace` is the minimum spacing between starts for the whole upstream (github default 100 ms = at most 10 starts/s, matching GitHub's "make requests serially" advice and the 900 points/min secondary limit with margin). Observed in the prototype: 3,000 first-run jobs complete their first start in 299.9 s of virtual time (`TestColdStartThreeThousandItems`).

### 4.4 Worker and result handling (`runOne`, `finish`)

```
runOne(ent):
  jctx := WithTimeout(hctx, timeout);  span "job.run"{kind,key,upstream,attempt}
  out, err := spec.Run(jctx, key)               -- recover(): err = panicError (Permanent); log ERROR with stack
  record duration; if elapsed > timeout: count job_overrun
  finish(...)

finish(ent, out, err):     -- under the mutex; hooks collected and run AFTER unlock and AFTER the slot is released
  release slot (delete from running; undo stuck mark); schedule observe(key, out, err)
  -- the rate-limit SIGNAL is never lost (unit 01 JB-05): if job.AsRateLimited(err) and not a shutdown cancellation, impose the
  -- upstream pause FIRST (4.5), whatever else the error is; then act by the precedence Gone > Permanent > RateLimited > Canceled > Transient:
  switch:
   err == nil:
        if the job STARTED after pausedTill: up.rlStreak = 0      -- an in-flight success of a pre-pause job proves nothing
        count success; attempts = 0; inBackoff = false
        record scp_webhook_to_publish_seconds{kind} if the key had an event-priority enqueue (PrioInteractive/PrioWebhook) since its last success
        if probe: unpark (onUnparked("probe succeeded"))
        if dirty: dirty = false; runAt = now; re-place        -- the single follow-up
        else delete key
   hctx cancelled and errors.Is(err, context.Canceled):        -- shutdown cut it short
        count canceled; delete key      (row untouched: overdue after restart, the planner repairs it)
   job.IsGone(err):       count gone; delete key; drop any parked entry (onUnparked(k, "gone") so the bridge deletes the row); no retry, no parking
   job.IsPermanent(err) (includes panics): park (4.6); delete key; drop dirty      -- even when the same error is also RateLimited: the pause above still applies
   job.AsRateLimited(err): count ratelimited; no attempt consumed; dirty cleared; runAt = pausedTill
   default (transient, includes context.DeadlineExceeded of the per-job timeout):
        attempts++; count retried; if attempts == MaxAttempts: count exhausted, onExhausted once
        inBackoff = true; runAt = now + Backoff.Delay(attempts); re-place (keeps retrying at the cap forever)
```

Classification is by behaviour interface only (`job.RateLimited`, `job.Permanent`, `job.Gone` via `errors.As`; the same precedence as unit 01's `job.Classify`, which the implementation may call instead of its own chain); **no error text is ever inspected** (architecture 15.6). The first draft ranked RateLimited above Permanent; an error that is both (a 403 secondary limit on a blocked repository) then retried forever instead of parking. The adapter decides, the engine acts (architecture 15 table):

| Upstream signal (adapter) | Engine action |
|---|---|
| 200/304 -> nil | success; `Outcome.Unchanged` feeds idle back-off |
| 404 on a content item stored as a typed state -> nil | success (a state, not an error) |
| `RateLimited{RetryAt}` (primary, secondary, 401-pause, open breaker) | pause the upstream (4.5) |
| `Permanent` (403 blocked/SSO, 410, 451, 422, parse failure bound to a content SHA), panic | park |
| `Gone` (the repository itself vanished) | drop |
| anything else (5xx, timeout, DNS, reset) | backoff retry |

### 4.5 Backoff and rate-limit pause

**Backoff** (`Backoff.Delay`): `delay = Base + U[0, hi-Base]`, `hi = min(Cap, Base << attempt)` computed with a saturating shift (a large `Base` or attempt cannot overflow, `TestBackoffBounds`), `attempt` = consecutive transient failures including this one (1-based). With the defaults (2 s, 15 min, 8): attempt 1 in [2 s, 4 s], 2 in [2 s, 8 s], 3 in [2 s, 16 s], ..., 8 in [2 s, 512 s], attempt 9 and later in [2 s, 900 s]. After the 8th consecutive failure `OnExhausted` fires **once** (counter `exhausted`, `/status`) and retries continue at the cap; only permanent errors park (an upstream outage ends). Randomness comes from the seeded `*rand.Rand` held under the engine mutex (not goroutine-safe).

**Rate-limit pause** (`pauseUntil`): `t = RetryAt`; if `t < now + 1 s` (a secondary limit without headers: the adapter could say nothing better) use an exponential floor `RateLimitMin << (streak-1)` capped at `RateLimitMax` (60 s, 120 s, 240 s, ... 15 min); clamp to `now + MaxPause` (65 min: a primary reset is at most an hour away); add a uniform 0..1 s stagger so keys do not resume in lock step. `up.pausedTill = max(pausedTill, t)`. **The streak counts limit *events*, not results**: one event hits every job that was in flight when it began (up to `MaxConcurrent`), and each of them returns `RateLimited`; only a result of a job that *started after the current pause was imposed* (`startedAt >= pauseSetAt`) increments `rlStreak`, and a success resets it only when the job started after `pausedTill`. The first draft incremented per result: four concurrent jobs gave 60+120+240+480 s, an 8 minute pause for one 1 minute event (test `TestRateLimitOneEventManyInFlightJobsDoesNotEscalate`). Nothing starts on that upstream before `pausedTill` (`TestRateLimitPausesUpstreamWithoutAttemptAndFloorGrows`); other upstreams are unaffected (a paused GitHub never starves Jenkins: GR-22). The pause is per **upstream**, not per installation (Q1).

### 4.6 Parking, probing, unparking

- **Park**: `parked[key] = ParkedKey{Since, RetryAt: now + ParkRetry (24 h), Reason: err.Error()}`; the key's entry is deleted; the engine logs ERROR once (`job parked`, with `job_key`, `job_kind`, `reason`, `retry_at`), counts `parked` (and `panic` for panics), and calls `onParked` after unlock (the bridge persists `store.Park(key, reason, retryAt)`). Reason strings are error texts from our own adapters and MUST NOT contain secrets (adapters use typed errors, `secret.Secret` cannot be formatted).
- **Probe**: when `RetryAt` passes, the dispatcher creates one entry flagged `probe`. Success unparks (`onUnparked("probe succeeded")`, bridge calls `store.Unpark`); a permanent failure re-parks with `Since` preserved and a new `RetryAt`; a transient failure keeps the key parked while the probe retries with backoff (the flag survives, so the eventual success still unparks; found and fixed while prototyping).
- **Force** (`Enqueue(..., force=true)`): the webhook-reactor convention (push, head_changed), the admin "retry" action and the `Retrier` bridge. `onUnparked("forced")`.
- **Warm start**: `WithRestoreParked(store.LoadParked())`. The planner is told lazily: its first `Tick` enqueue of a parked key is suppressed and the engine reports a `ParkedError` to the observer, which sets the item `parked`; the planner stops ticking it (`TestPlannerLearnsParkedAfterWarmStart`).
- A repository that disappears is **not** a dead letter: the feature job returns `Gone` (key dropped); reconcile removal cascades the rows; a `Gone` of a parked key calls `onUnparked(k, "gone")` so the bridge deletes the `store.parked` row (the first draft dropped it from memory only).
- **Herd**: parked keys restored with an overdue `RetryAt` are all probed in the first dispatch pass, and a systematic failure (500 repositories blocked by SSO) parks them with the same `RetryAt`, so they probe together every 24 h. Probes run at `Spec.Prio`, paced by `Pace` and the gate like any job; acceptable, no jitter added (Q6 measures the observer cost).
- **Force on a full table**: `Enqueue(force=true)` on a parked key removes the parked entry *before* the `MaxKeys` check; if the table is full the call returns `ErrQueueFull` and the key is neither parked nor queued. The caller (webhook: 503, redelivery) retries and the planner re-derives it, so nothing is lost; recorded because the planner item stays `parked` until the next result (`Planner.Enqueue` reverts only an `idle` item).

### 4.7 Per-job deadline, overrun, stuck

`Spec.Timeout` (default `UpstreamConfig.JobTimeout`, 2 m) is applied with `context.WithTimeout` on the job context. A job that returns after its deadline is counted (`scp_job_overrun_total{kind}`); a deadline error is a transient failure. A job that has **ignored** cancellation for more than `StuckAfter` (5 s) past its deadline is marked `Stuck`: counted (`scp_job_ignored_cancel_total{kind}`), logged at ERROR, visible in `Inspect`. A stuck job keeps its slot (Go cannot kill a goroutine). `UpstreamStats.AllStuckSince` is set when every slot of an upstream is stuck; **`/livez` MUST fail when `AllStuckSince` is older than 2 minutes** (architecture 8.6 point 2, 10.2), or when `now - Stats.Heartbeat > 30 s`. The only remedy is a restart by the supervisor (`TestJobTimeoutOverrunAndStuckAllSlots`).

### 4.8 Shutdown

`Run(ctx)` (ctx = the refresh stage's context):

1. On `ctx.Done`: `closed = true` (every later `Enqueue` returns `job.ErrClosed`), the dispatcher starts nothing new, queued and delayed entries are left (their rows are untouched; the planner re-derives them after restart).
2. In-flight jobs keep running under `hctx`, which is **not** cancelled by `ctx`, for `Grace` (15 s). A job that finishes inside the grace never sees a cancellation (`TestShutdownGraceDrainsThenCancels`: a 5 s job completes clean, a blocked one is cancelled exactly at 15 s).
3. At `Grace`: `hctx` is cancelled. Jobs return `context.Canceled`; `finish` classifies it as `canceled` (key dropped, observer told with `context.Canceled`, the planner returns the item to `idle(now)`).
4. `AbandonAfter` (3 s) later `Run` returns `ErrAbandoned` if a job still has not returned (`TestShutdownAbandonsJobsThatIgnoreCancel`); otherwise `nil` as soon as the last job returned.

The scheduler stops by `ctx` immediately (its tasks are local and microsecond-scale). The plan flusher (bridge) flushes in the stage `Close`, after `Run` returned, so the final `SaveState` calls are included (`PlanSink` flush every 5 s and at shutdown; losing the last 5 s only makes an item due early).

### 4.9 Planner

State of record: per item `NextDue`, `UnchangedStreak`, `everDone`, state. Per kind: the `job.Spec` (Policy, Prio, Scope).

```
Tick(now):                                    -- called by the planner.tick scheduler task every refresh.planner_tick (15 s +-10%)
  lock
  for kind in registration order:
     for item in subject order:
        if item.state != idle or item.NextDue > now: continue
        prio := spec.Prio;  if spec.Scope == ScopeRepo && !item.everDone && prio < PrioBackfill: prio = PrioBackfill   -- cold start
        item.state = inflight
        due += {key, prio}
     stable-sort this kind's due items by NextDue (ties keep subject order)
  unlock                                       -- NO planner lock across a call into the engine
  for d in due: err := enq.Enqueue(d.key, d.prio, zero, false)
        err != nil -> item back to idle with NextDue = now + 30 s; count planner_enqueue{result}; on ErrClosed stop and release the rest
```

`OnResult(k, o, err)` (the engine observer; it **owns** `next_due_at` and `unchanged_streak`):

| Result | Planner effect |
|---|---|
| `nil` | `streak = o.Unchanged ? streak+1 : 0`; `everDone = true`; `idle`; `NextDue = o.NextDue` if non-zero else `now + interval(TTL, Jitter, IdleBackoffMax, streak)`; `sink.SaveState(k, {NextDue, streak})` |
| `job.IsGone` | forget the item |
| `job.IsPermanent` (incl. `ParkedError`) | `parked` (not ticked) |
| `errors.Is(err, context.Canceled)` | `idle` with `NextDue = now` (shutdown) |
| anything else (transient, rate limit) | unchanged: the engine retries; the item stays `inflight` |

`interval`: `d = TTL`; if `IdleBackoffMax > TTL`: double once per unchanged streak step until `IdleBackoffMax` (TTL 2 h, cap 12 h: 2 h, 4 h, 8 h, 12 h, 12 h ...); then `d *= 1 + Jitter*(2r-1)` with the planner's own seeded `rand`; never less than 1 s. A webhook-driven run that succeeds pushes the next scheduled run out (the planner is level-triggered on `NextDue` only).

**Cold start.** Items with no state (never completed) are due immediately and are enqueued at `PrioBackfill` in subject order, kinds in registration order: all `commits.refresh` before all `docs.refresh` before all `manifests.refresh` (FIFO within a class). At the github defaults (10 starts/s) 1,000 repositories x 3 kinds drain in 300 s; the 1,000 commit jobs, which gate `/readyz`, start within the first 100 s. The 300 s figure is `jobs / min(1/Pace, MaxConcurrent / job duration)` for jobs that take one call; `docs.refresh` and `manifests.refresh` make several calls per run, so with a job time of 1 s and 4 slots the ceiling is 4 starts/s and 3,000 jobs take 750 s: the startup probe budget of 10-15 minutes (architecture 10.2) is sized for this, and the unit 05 adapter has its own limiter underneath (`github.defaults.budget.rps` 8, burst 16, semaphore `budget.concurrency` 4): the real ceiling is the **minimum** of `1/Pace` (10/s), the adapter's 8 requests/s divided by the calls per job, and `MaxConcurrent / job duration`; cold start therefore takes 5 to 15 minutes, not 300 s, which is why `/readyz` has the 10-minute deadline escape and the startup probe budget is 10-15 minutes. With a PAT (5,000/h) the backfill reserve (40%) stops cold start after 3,000 calls and the rest waits for the reset; that is by design (webhooks and scheduled work keep their reserve) and `/readyz` has the deadline-based escape (architecture 10.2). Restored items (persisted `NextDue`) are not cold: a restart inside the TTL makes about zero calls, a restart after a long downtime enqueues the overdue ones at their `Spec.Prio`.

`Progress(kind, now)` gives `Subjects`, `Completed` (ever done), `Inflight`, `Parked`, `Due`: the fallback for the readiness coverage numerator and the `/status` per-kind view. The authoritative coverage remains the per-feature snapshot probes of architecture 10.2.

### 4.10 Scheduler

Local housekeeping only; it never calls an upstream (those are global `job.Spec`s run in the engine). One goroutine per task, **fixed-delay with jitter**: the next interval starts after completion, so a task never overlaps itself and no backlog forms (GR-24); `Trigger` skips if running (counted `Skipped`); panics are recovered into `LastErr`; `LastAttempt/LastSuccess/LastErr` are written **on completion**, never at queue time, so the status reports runs that happened (GR-41); `Overruns` counts `duration > Every`; every task owns a `*rand.Rand` seeded from `(seed, fnv64a(name))`, so jitter is reproducible and independent of registration order; `Add` is frozen at `Run`; `OnDone` runs outside the mutex. `InitialDelay` is derived by `cmd/scp` from `store.LoadTasks()[name].LastSuccess` so a restart resumes the cadence instead of re-running everything at t=0.

### 4.11 Readiness and liveness contribution (what this unit provides, not what it decides)

`/readyz` (architecture 10.2) is owned by `ops`. This unit contributes **inputs** and **start-up ordering**:

1. *Ordering*: warm load -> `planner.Restore(LoadPlans)`, `WithRestoreParked(LoadParked)` -> warm `SetSubjects` from the warm catalogue -> `engine.Run` + first `catalog.reconcile` at `PrioWebhook` (paced, gated, parked like any call) -> a `Scheduler.Trigger("planner.tick")` after every `SetSubjects`. Because `Restore` accepts states before or after the subjects exist, the order of the first two calls does not matter.
2. *Inputs for `ops`*: `Planner.Progress(kind, now)` (coverage fallback and `/status`), `Engine.Inspect()` (queue depth, pause, stuck, heartbeat, parked count, outcome counters), `Engine.Parked()`.
3. *`/livez`* (process-local, outcome-based, no outbound call; GR-41): fails when the dispatcher heartbeat is older than 30 s, or `AllStuckSince` of any upstream is older than 2 min. A zero `Heartbeat` (the engine has not started its first pass) counts as alive for the first 30 s of the process, then as failed. A paused or saturated upstream is **not** a liveness failure.
4. Readiness **never regresses** (architecture 10.2): nothing in this unit turns an upstream outage into `/readyz` 503; the engine reports it as pause/backoff/stale items, which the SPA shows through `freshness`.

### 4.12 Webhook: verification contract (normative for `github.Verifier`)

The receiver calls `Verify` and trusts nothing else. The implementation in `internal/github/webhook.go` MUST behave as the reference in `04-refresh-webhook.assets/internal/ghverify` (verified with the official test vector, 12 rejection cases, a fuzz target and the recorded push shape):

1. **Header and algorithm.** Only `X-Hub-Signature-256` (verified 2026-10-14: GitHub "recommends" it; the value is `sha256=` + lowercase/uppercase hex of HMAC-SHA256 over the **raw request body** keyed by the webhook secret; the header is absent when no secret is configured). `X-Hub-Signature` (SHA-1) is never read: no fallback. The HMAC is computed over the exact bytes received, not over a decoded string (GR-20).
2. **Format first.** `len == 7 + 64`, prefix exactly `sha256=` (case-sensitive), 64 hex digits. Anything else is `unauthenticated` without HMAC work. (The receiver performs the same cheap check **before reading the body**, section 4.13, so garbage costs no body read.)
3. **Constant-time comparison** with `hmac.Equal` over decoded bytes (never `==`, never `strings.EqualFold`). The loop over configured secrets does the HMAC for every non-empty secret and does not `break` on the first match, so the number of secrets does not leak through timing.
4. **Secrets.** The source's *current* secret and optional *previous* secret (rotation overlap: a webhook has exactly one secret at a time, so for the minutes between changing it on GitHub and deploying the new one both must verify). An empty secret never verifies (`Secret.IsZero()` is "unset", GR-13); an empty list never verifies. The secret of source A never verifies a delivery addressed to source B: verification uses only `src`'s secrets, there is no "try every secret" (research 09 section 3.4).
5. **App sources**: additionally enforce `X-GitHub-Hook-Installation-Target-ID == app_id` and that `payload.installation.id` belongs to the registry; an unknown installation or a target mismatch is not an authentication failure: the verifier returns an `Ignored()` error (unit 05's choice, adopted here: the event never reaches a reactor and `scp_webhook_total{result="ignored"}` counts it; research 09 section 3.4). The headers `X-GitHub-Hook-Installation-Target-Type` and `-ID` exist (re-fetched 2026-10-14); the exact Target-Type value for an App is unverified: check against a recorded fixture in M0.
6. **Decode after verify**, into minimal typed structs tolerant of unknown fields (`encoding/json`), into `scm.Event`:

| Event header | `scm.Event` |
|---|---|
| `ping` | `{Kind: EventPing, Delivery, Source}` |
| `push` | `Kind, Delivery, Source, Installation (payload.installation.id or 0), Ref (full ref, `refs/heads/...` or `refs/tags/...`), Before, After, Created, Deleted, Forced, CommitCount = len(commits), Repo{Source, ID = repository.id, Owner = owner.login else owner.name (push payloads carry `owner.name`), Name = repository.name}, HeadTreeOID = head_commit.tree_id (empty for a deleted ref), ChangedPaths = de-duplicated union of added+removed+modified across **all** commits (order unspecified: the reference uses first-seen, unit 05 sorts; reactors MUST NOT rely on it), PathsComplete` |
| `release` | `Kind=EventRelease, Action = payload.action` (`published`, `created`, ...), `Ref = "refs/tags/" + release.tag_name`, `Repo` from `repository.{id, name, owner.login}`; no repository -> `Malformed()` |
| `create` | `Kind=EventCreate, Created=true`, `Ref = "refs/heads/" + ref` or `"refs/tags/" + ref` from `ref_type` (the payload carries the plain name: recorded `ref: "v0.0.1", ref_type: "tag"`), `Repo` as above |
| `repository` | `Kind=EventRepository, Action` (`created`, `deleted`, `renamed`, `privatized`, ...), `Repo` as above (identity is the id, a rename resolves through the reconcile) |
| `installation`, `installation_repositories` | `Kind=EventInstallation` / `EventInstallationRepos`, `Action`, `Installation = installation.id`, zero `Repo` (reactors re-list; the payload's repository lists are not carried) |
| anything else (`star`, `fork`, `delete`, ...) | error `Ignored()` (after the signature check, with `ev.Delivery` set) |
| missing `X-GitHub-Event` | error `Malformed()` (after the signature check) |
| JSON that does not decode | error `Malformed()` |

7. **`PathsComplete`** is `false` when the push is **forced** (history was rewritten: the commit list names only what is new, removed or reverted files appear in no list) or when the head moved without any listed commit (`len(commits) == 0` on a non-deleted ref: GitHub omits the list above its caps; the first draft returned `true` with an empty path list, so a force-push to the default branch would never have refreshed the docs; `TestForcedOrCommitlessPushHasIncompletePaths`); a **deleted** ref is complete (nothing to refresh). It is also `false` when `len(commits) >= 2048` (GitHub caps the array at 2,048, verified 2026-10-14; exactly 2,048 cannot be told from truncation), when **any** commit lacks one of the three arrays (a missing array means "unknown", never "empty": `TestMissingArraysMeanIncompleteNotEmpty`), or when the union exceeds the 20,000-path cap. This resolves risk V13/W20 as far as it can be resolved without a live delivery: GitHub's own events page still does not document the per-commit arrays or `tree_id` (re-fetched 2026-10-14), but recorded real deliveries (organisation webhook, 2018) carry `commits[].{id, tree_id, distinct, added, removed, modified}` and `head_commit.{tree_id, added, removed, modified}`; the trimmed shape is the prototype fixture `testdata/push_two_commits.json`. M0 still records a **current** delivery (Q-21).
8. **Error vocabulary.** Errors satisfy structural behaviour interfaces the receiver tests with `errors.As` (the `github` package never imports `webhook`): `Unauthenticated() bool` (unit 05's `WebhookError` spells it `InvalidSignature() bool`; the receiver honours both until X1 is settled), `Malformed() bool`, `Ignored() bool`. A joined or wrapped error is fine. Any other error is an internal bug and becomes a 500, never a 400 (GR-16; unit 05 says 400, conflict X2).
9. **Delivery id.** `X-GitHub-Delivery` is required (`Malformed()` when absent: unit 05 already does this) and must match `[0-9A-Za-z-]{1,64}` (a UUID in practice). The receiver re-checks it and answers 400 for anything else from a verified sender: the id is the key of the de-dup set (10,000 entries; an unbounded header would break the ~1 MB memory bound) and is echoed in the 202 body.

### 4.13 Webhook: receive pipeline

Order is the security argument (GR-20): size -> signature -> parse -> act.

```
ServeHTTP(w, r):                                   -- route: POST only (the ServeMux answers 405 otherwise)
  span "webhook.handle"
  1. src := r.PathValue("source"), or Sources.Default if empty;  unknown or "" -> 404 not-found                    [unknown_source]
  2. Content-Type media type must be application/json (charset ignored) else 415 unsupported-media-type              [unsupported_media]
  3. header X-Hub-Signature-256 must be plausible ("sha256=" + 64 hex) else invalid(): 401 (or 429)                  [invalid_signature | throttled]
  4. Content-Length > MaxBody -> 413 payload-too-large                                                               [too_large]
  5. acquire one of MaxConcurrent body slots; wait up to AcquireWait (or r.Context()) else 503 overloaded+Retry-After [busy]
  6. ResponseController.SetReadDeadline(now + BodyTimeout)                       -- a plausible header is free to forge: a slot may not be held longer
     body := read(MaxBytesReader(r.Body, MaxBody)) into a buffer preallocated to Content-Length when known
       *http.MaxBytesError -> 413 [too_large];  os.ErrDeadlineExceeded -> 408 request-timeout [body_timeout];  other read error -> 400 [malformed]
  7. ev, err := Verifier.Verify(src, r.Header, body); body = nil; release the slot  (memory bound: MaxConcurrent x (MaxBody + 512 B) with a Content-Length, about 2.5x for chunked bodies)
       err Unauthenticated (or InvalidSignature) -> invalid() ; Ignored -> 202 "ignored" ; Malformed -> 400 validation-failed ; other -> 500 internal
     ev.Delivery not [0-9A-Za-z-]{1,64} -> 400 validation-failed [malformed]   (verified sender, hostile or broken id)
  8. ev.Kind == ping -> 202 "pong" (not forwarded, no side effect)                                                  [ping]
     ev.Kind == head_changed or "" from a verifier -> 400 (a bug in the verifier; the synthetic kind never arrives over HTTP)
  9. ev.Source = src; key = src + "/" + ev.Delivery
     if dedupe.Seen(key, now) -> 202 "duplicate"                                                                    [duplicate]
 10. err := Sink.Handle(ctx, ev)   -- synchronous on this goroutine (GitHub waits 10 s; reactors are cheap pure decisions + non-blocking Enqueue)
       errors.Is(err, job.ErrQueueFull) || errors.Is(err, job.ErrClosed) -> 503 queue-full + Retry-After; NOT recorded    [queue_full]  (code `queue-full` of the shared catalogue, CD-ER-03)
       any other error -> logged with delivery_id; the delivery is still accepted (events are hints)
 11. dedupe.Record(key, now) (only now: after a successful enqueue); lastAccepted = now
 12. 202 {"status":"accepted","delivery":"<id>"}                                                                        [accepted]

invalid(): if limiter != nil && clientIP != nil && !limiter.Allow(clientIP(r)) -> 429 rate-limited + Retry-After          [throttled]
           else log WARN {reason, delivery_id = sanitised header (<= 64 chars of [0-9A-Za-z-])}, 401 unauthorized     [invalid_signature]
```

Properties and design choices:

- **Nothing is blocked before verification.** The invalid-signature limiter (architecture 17.4: 10 invalid/min/IP) only upgrades a 401 to a 429 *after* verification failed; a throttled address sending a valid signature is accepted (`TestInvalidSignatureBudgetGives429ButValidIsNeverLimited`). A "block IP early" design would let anyone who shares an address with GitHub's delivery IPs behind a misconfigured proxy deny real deliveries. HMAC cost is bounded by the body slots (4 x 25 MiB) and the plausibility check.
- **Slot starvation is bounded in time, not prevented.** The slots exist before verification (the body must be read to verify it), so a sender with a plausible header can occupy them; `body_timeout` (default 8 s, below GitHub's own 10 s) caps how long, the answer to a delivery that finds no slot is 503 + `Retry-After` after `acquire_wait`, and a lost delivery is repaired by the planner (and by `webhook.redelivery` for Apps). A distributed slow-body attack can still degrade the accelerator, never the portal; the operator's proxy (read timeouts, connection limits) is the second layer and the runbook says so. A per-IP concurrency cap was rejected: GitHub delivers from few addresses and ADR-025 rejects per-IP limits on valid webhooks.
- **No body, no unverified header content in logs** (GR-20): the 401 path logs the reason and a sanitised delivery header only (`TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised`, which also covers log injection through a newline in the header). Log arguments are lazy (`LogAttrs`), never `fmt`-formatted payloads.
- **De-duplication is recorded after a successful enqueue** (A-11): GitHub reuses `X-GitHub-Delivery` for a redelivery, so recording before success would swallow exactly the redeliveries that recover failures (`TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted` for `ErrQueueFull`, `ErrClosed`, wrapped and joined forms). Exactness is not required: two concurrent identical deliveries may both pass `Seen`; both are idempotent (race test `TestConcurrentDeliveriesRace`).
- **The set** (`dedupe.go`): entries `(key, recordedAt)` in a `container/list` plus a map; every call first evicts expired entries from the front (all entries share one TTL, so insertion order **is** expiry order; no goroutine, no sweeper); `Record` evicts the oldest while `len > DedupeMax`; a `Seen` hit does **not** refresh the entry (a replayed id must not extend its own life). It is "LRU" in the architecture text; the insertion-ordered form is the correct one for a fixed TTL. Memory bound: 10,000 x (about 100 bytes) ~ 1 MB. A redelivery after the TTL (15 min) is processed again: one idempotent refresh (risk W12).
- **Events are accelerators** (A-11, architecture 6.4): the receiver does not look at paths, branches or repositories; it forwards. An unknown repository, a non-default branch, a tag push, an irrelevant path: the reactors decide and the answer is still 202 so GitHub shows a green delivery.
- **Ping** is verified like any delivery (it is signed) and answered 202 `pong`; it is counted `ping` and never reaches the sink (no side effects).
- **Every `scm.EventKind` is accounted for** (GR-21; unit test `TestEveryEventKindIsHandledOrCountedIgnored`, section 12): `ping` handled here; `push`, `release`, `create`, `repository`, `installation`, `installation_repositories` forwarded; `head_changed` is synthetic (emitted by the catalog head sweep straight into the sink, never over HTTP); every other GitHub event is counted `ignored` by design. Subscribed events (Q-24 plus App defaults): `push`, `release`, `create`, `repository`; App webhooks additionally get `installation` and `installation_repositories` automatically (no subscription possible). `create` is redundant with `push` for tags but kept for branch-creation latency; a push to `refs/tags/...` is a `push` event whose `Ref` reactors can ignore.

### 4.14 Status-code mapping (complete)

| Condition | Status | Body | `code` | Result label | Header |
|---|---|---|---|---|---|
| verified push/release/create/repository/installation* delivered to the sink | **202** | `{"status":"accepted","delivery":"<guid>"}` | | `accepted` | |
| same delivery id seen within `dedupe_ttl` | 202 | `{"status":"duplicate",...}` | | `duplicate` | |
| `ping` | 202 | `{"status":"pong",...}` | | `ping` | |
| verified but event not handled by design | 202 | `{"status":"ignored",...}` | | `ignored` | |
| other reactor error (logged) | 202 | accepted | | `accepted` | |
| unknown `{source}` or source-less route without a default source | 404 | problem | `not-found` | `unknown_source` | |
| body not `application/json` (form-encoded hook) | 415 | problem | `unsupported-media-type` | `unsupported_media` | |
| signature missing, implausible, or wrong | **401** | problem | `unauthorized` | `invalid_signature` | |
| as above but the client IP exhausted its invalid-signature budget | 429 | problem | `rate-limited` | `throttled` | `Retry-After: 5` |
| `Content-Length` or streamed body above `max_body` | 413 | problem | `payload-too-large` | `too_large` | |
| body unreadable (client abort mid-read) | 400 | problem | `validation-failed` | `malformed` | |
| whole body not received within `body_timeout` (slow sender) | 408 | problem | `request-timeout` | `body_timeout` | |
| verified, `X-GitHub-Delivery` not `[0-9A-Za-z-]{1,64}` | 400 | problem | `validation-failed` | `malformed` | |
| verified, payload undecodable or `X-GitHub-Event` missing | **400** | problem | `validation-failed` | `malformed` | |
| no body slot within `acquire_wait` | 503 | problem | `overloaded` | `busy` | `Retry-After: 5` |
| sink returned `job.ErrQueueFull` / `job.ErrClosed` | **503** | problem | `queue-full` (was `overloaded`; CD-ER-03: one code for a full job queue everywhere) | `queue_full` | `Retry-After: 5` |
| verifier returned an unclassified error | 500 | problem | `internal` | `error` | |

400 is for a caller error only after authentication; 401 for authentication; 5xx for ours; 503 + `Retry-After` for saturation (GR-16, GR-21). GitHub does not retry on its own ("GitHub does not automatically redeliver failed deliveries", verified 2026-10-14), which is why a 503 is only half the story and the redelivery job exists.

Problem bodies are RFC 9457 through `httpapi.WriteProblem` (`Cache-Control: no-store`). **`Retry-After` reaches the wire only through `Problem.Header`** (unit 06 section 4.1: `WriteProblem` copies `Allow`, `Retry-After`, `WWW-Authenticate` from it and nothing else), so the receiver sets `Header["Retry-After"]` and `RetryAfterSeconds` to the same value in one helper; the first draft set only the body member against a stub that wrote the header itself, so the real `httpapi` would have sent 503 and 429 without it (`TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted` now runs against a stub with the real semantics). They contain **fixed text only**: no verifier message, no header value, no payload (GR-17: nothing the sender wrote is reflected).

### 4.15 Redelivery sweep (`webhook.redelivery`, optional)

Purpose: GitHub's delivery log keeps about three days (verified 2026-10-14: "You can redeliver webhook deliveries that occurred in the past 3 days"), does not redeliver automatically, and `GET /app/hook/deliveries` / `POST /app/hook/deliveries/{id}/attempts` (App JWT; verified 2026-10-14: list with `per_page` <= 100, `cursor`, `status=success|failure`; delivery objects carry `id`, `guid`, `delivered_at`, `redelivery`, `status`, `status_code`, `event`, `installation_id`, `repository_id`; redeliver answers 202, and 422 "Validation failed, or the endpoint has been spammed") let an App ask again. This job re-sends deliveries that failed while SCP was down, saturated, or misconfigured. It is App-source only; for PAT/organisation webhooks the recovery after downtime is the planner (research 09 section 3.3).

It is one global job `webhook.redelivery/_` (`Upstream "github"`, `Scope Global`, `Prio Scheduled`, `Policy.TTL = min(enabled intervals)`, jitter 10%, `Timeout = sum(max_per_run) x rate + 2 min`), so pacing, gate, pause and parking apply. It needs no manual first enqueue: `Planner.Track` makes every global kind due at once and the first tick starts it (section 2.5 shows how to delay it).

```
Run(): for each enabled source in order:
  name := "webhook.redelivery/" + source
  w := Watermarks.Load(name)                      -- zero when never run
  since := w
  if w.IsZero():                since = now - 24h
  elif now - w > MaxAge (72h):  since = now - MaxAge              -- retention gap: older failures are unrecoverable
        count gap; Sink.Handle({Kind: EventInstallationRepos, Action: "resync", Source: s, Delivery: "redelivery-gap/<s>/<rfc3339>"})
        (ErrQueueFull/ErrClosed from the sink fail the run: the engine retries it)
  ids, err := Redeliverer.FailedDeliveries(ctx, s, since)   -- one id per GUID, OLDEST first, failed GUIDs only
  err with Truncated() == true: keep ids, remember "adapter stopped at its page cap"; any other err: return it (engine classifies)
  truncated := adapterTruncated || len(ids) > MaxPerRun;  ids = ids[:MaxPerRun]   -- the oldest, which are the next to age out
  for i, id in ids:  (wait Rate between redeliveries, ctx-aware)
        Redeliver -> nil: count redelivered ; job.IsGone: count gone, continue ; other error: count failed, RETURN it (engine classifies)
  if !truncated: Watermarks.Save(name, scanStart - 1 min)  -- only after the whole window was handled
  else: leave the watermark: redelivered GUIDs now have a successful attempt and drop out of the next list, so the next run continues
return Outcome{Unchanged: no redelivery happened}
```

**Adapter contract** (unit 05 owns the code; the verifier added what the first draft left implicit, cross-unit items X3 to X5): (1) list `GET /app/hook/deliveries?per_page=100` pages newest first until `delivered_at < since` or `limits.max_pages`; the list must cover **both** outcomes of the window, because a delivery that failed once and succeeded on redelivery appears in the failure list as its original attempt: group by `guid` and keep a GUID only when its **newest** attempt is a failure; (2) `status=failure` documents `status_code` 400-599 only (verified 2026-10-14, `success` is 200-399), so a **timed-out delivery (no status code) may be missed by a status filter**: the adapter must not rely on the filter alone (M0 task: record what a timeout looks like and pick `status != "OK"` client-side if needed); (3) return the **oldest first**, at most `max_per_run` ids; (4) skip a GUID that already has 3 redelivery attempts in the window (a delivery that fails for ever, e.g. a 401 from a wrong secret, is otherwise redelivered every `interval` for three days); (5) on hitting the page cap return the ids found plus an error with `Truncated() bool`; (6) `Redeliver`: 202 is success; 404 / "no such delivery" is `Gone`; 429 and secondary limits are `RateLimited`; **422 is documented as "Validation failed, or the endpoint has been spammed" and must not be mapped to success** (unit 05 maps it to nil as "already redelivered"; the verifier found no source for that reading): treat it as `RateLimited{RetryAt: now + 10 min}` unless the response body proves "already redelivered". Both calls use the App JWT bucket and are not charged to installation budgets (unit 05).

Properties: redelivered deliveries come back through the normal receiver (same GUID): accepted because the failed original was never recorded in the de-dup set (a delivery answered 2xx is never listed as failed); a delivery that failed again is listed again at the next run until it has 3 attempts or ages out of the 3-day window (bounded by `max_per_run`, `rate` and the attempt cap); the watermark only moves after a complete pass; a save failure is logged and harmless (the next run re-lists); a rate-limited or failing source stops the run and keeps its watermark (`TestRedeliveryTruncatedRunDoesNotAdvanceAndErrorsPropagate`); an adapter that stopped at its page cap keeps the watermark too (`TestRedeliveryAdapterTruncationKeepsWatermark`; without the signal the oldest failures would have been skipped silently). If truncation persists for longer than `max_age` the gap path emits one `resync` per run: bounded by the interval, and it says exactly what is true (more failures than the job can recover). Lost records cannot be redelivered (GitHub incident 2026-07-14 per research 09), so this job **narrows** the gap; it does not close it: the planner's TTLs do (a 2-hour worst case for commits).

The "resync" event is the existing normalised vocabulary (mechanism D) rather than a job-key string shared with `catalog` (a magic string across features) or a new `scm.EventKind` (D10).

### 4.16 Behavioural decisions and why

Each row is a decision this unit makes for webhook handling and background work, and the failure it rules out.

| Decision | Why |
|---|---|
| routes `POST /webhooks/github/{source}` and `POST /webhooks/github` (the default source); one handler; anything else under `/webhooks/` is a 404 problem | one route table and one handler keep the surface small and testable (GR-15) |
| `X-Hub-Signature-256`, `X-GitHub-Event` and `X-GitHub-Delivery` are required; SHA-256 over the **raw bytes**; JSON content type required (415 otherwise) | a signature checked over a decoded string depends on the charset and can accept bytes that were never signed (GR-20) |
| size -> verify -> parse; nothing is logged, pretty-printed or parsed before `Verify`; `MaxBody` and body slots | parsing or formatting a body for a log line before verification exposes the parser to unauthenticated input and makes memory unbounded (GR-20) |
| `hmac.Equal`, prefix and length checked, an empty secret never verifies and is a **startup error**, one secret key per source (plus `previous_secret`) | an unchecked prefix, a non-constant-time comparison, an empty default secret or two competing secret keys each let a forged or misconfigured delivery through (GR-20, GR-12) |
| table 4.14: 202/400/401/408/413/415/429/500/503, each counted; `ErrQueueFull` -> 503 + `Retry-After` | one status for every failure (for example 400 for a full queue) tells GitHub the sender is at fault and the delivery is lost silently (GR-16) |
| `Receiver.Stats().LastAccepted` + `scp_webhook_last_accepted_timestamp_seconds`, set only after a verified, accepted delivery | "last seen" must mean a delivery that authenticated, so silence can be alerted on (GR-41) |
| `ping` 202 `pong`; unhandled events 202 `ignored`, counted | every kind is accounted for, none is advertised and silently dropped (GR-21) |
| `push`: reactors compare the full `Ref` with `refs/heads/<default branch>`, resolve `RepoID`/full name against the catalogue, and enqueue one coalesced `commits.refresh` per repository | keying on the last `/` segment of a ref conflates `refs/heads/feature/x` with `x` and tags with branches; one job per commit multiplies work (GR-10, GR-21) |
| `Event.ChangedPaths` is the union over all commits of added+removed+modified; `docs.React` enqueues one `docs.refresh` of that repository when README/docs/descriptor paths are touched or `PathsComplete` is false; the job re-fetches by path and SHA, never "the modified file" | the head commit's `modified` list alone misses earlier commits, added and removed files; fetching files named in an event from another repository stores content in the wrong place (GR-21) |
| decode tolerant of absent arrays (`PathsComplete=false`), `full_name` and `id` read; deleted refs (`deleted: true`, `head_commit` null) are events reactors drop or treat as removal | a missing array means "unknown", never "empty"; a decoder that assumes presence crashes on legal payloads (GR-29) |
| `create`, `release`: forwarded to reactors: `catalog.React` refreshes `RepoStats`/release, `docs`/`manifests` react to `create` of the default branch | no stub handler is advertised as supported (GR-04) |
| fixed-delay scheduler with recover and skip-if-running; the planner refreshes **every** kind on its TTL | a fixed-rate scheduler with an unguarded task builds backlogs and dies on the first exception; data without a TTL never expires (GR-24, GR-26) |
| delayed full-jitter retries, exhaustion counted and logged, parking, panic containment, coalescing | a retry cap that drops work without a log line, immediate requeue on any error, or a worker killed by a panic loses work silently or hot-loops (GR-24) |
| section 12: signed deliveries through the real `ServeMux` patterns, status and metric assertions, test vectors | a test that posts an unsigned payload and asserts nothing proves nothing (GR-43) |

---

## 5. Data

This unit owns **no table and no migration**. It reads and writes existing `store` structures only through bridges:

| Store structure (architecture 8.5, 9.2) | Who writes | Written when | Retention |
|---|---|---|---|
| `item.next_due_at`, `item.unchanged_streak` | `PlanSink` bridge via `store.SavePlans` | after every successful planner `OnResult`, batched: flush every 5 s and in the stage `Close` | with the item row (cascade on repository delete) |
| `parked(key, since, reason, retry_at)` | engine hooks `WithOnParked` / `WithOnUnparked` through the bridge (`store.Park`, `store.Unpark`) | at park, at unpark (force, probe success) | until unparked; rows of removed repositories are deleted by the bridge on reconcile removal |
| `task_state(name, last_attempt, last_success, last_error)` | `Task.OnDone` bridge (`store.SaveTask`); `webhook.redelivery/<source>` watermark through `Watermarks.Save` (`last_success` carries the watermark) | after each task run / each complete redelivery scan | forever (tiny) |
| webhook delivery ids | **nothing**: in memory only (`dedupe.go`), 15 min, 10,000 entries | | process lifetime; a restart loses it by design (risk W12) |

Warm start (architecture 9.3): `LoadPlans` -> `Planner.Restore`; `LoadParked` -> `WithRestoreParked`; `LoadTasks` -> `Task.InitialDelay`, redelivery `Watermarks.Load`. Failure semantics: if the store is degraded, `SaveState`/`Park` calls fail inside the bridge, are counted (`scp_store_errors_total{op}`) and logged once; the engine and planner continue from memory (serving matters more than durability). A lost `parked` row means a poison key is retried after restart and parked again: harmless.

---

## 6. Configuration

Keys are owned by the types named; `cmd/scp/config.go` composes them into the root struct (architecture 16). Env names follow `SCP_<PATH_IN_UPPER_SNAKE>` with list indices; secrets also accept `*_FILE`. Every key is read by the consumer named in the last column (the documented-keys == consumed-keys test, GR-12).

### 6.1 `refresh:` (consumer: `cmd/scp` building `refresh.New`/`NewPlanner`/`Task`)

| Key | Type | Default | Validation | Env | Consumed by |
|---|---|---|---|---|---|
| `refresh.upstreams.<name>.concurrency` | int | github 4; others 1 | 1..64 | `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY` | `UpstreamConfig.MaxConcurrent` |
| `refresh.upstreams.<name>.pace` | duration | github 100ms; others 0 | 0..10s | `SCP_REFRESH_UPSTREAMS_GITHUB_PACE` | `UpstreamConfig.Pace` |
| `refresh.upstreams.<name>.job_timeout` | duration | 2m | 1s..30m | `SCP_REFRESH_UPSTREAMS_GITHUB_JOB_TIMEOUT` | `UpstreamConfig.JobTimeout` (addition to the outline; the field already exists in architecture 8.6) |
| `refresh.backoff.base` | duration | 2s | > 0 | `SCP_REFRESH_BACKOFF_BASE` | `Backoff.Base` |
| `refresh.backoff.cap` | duration | 15m | >= base | `SCP_REFRESH_BACKOFF_CAP` | `Backoff.Cap` |
| `refresh.backoff.max_attempts` | int | 8 | >= 1 | `SCP_REFRESH_BACKOFF_MAX_ATTEMPTS` | `Backoff.MaxAttempts` |
| `refresh.grace` | duration | 15s | 1s..60s | `SCP_REFRESH_GRACE` | `WithGrace` (also sizes the stage `StopTimeout`, section 2.5) |
| `refresh.planner_tick` | duration | 15s | 1s..5m | `SCP_REFRESH_PLANNER_TICK` | `planner.tick` `Task.Every` |
| `refresh.max_keys` | int | 50000 | >= 1000 | `SCP_REFRESH_MAX_KEYS` | `WithMaxKeys` |
| `refresh.parking.retry` | duration | 24h | 1m..30d | `SCP_REFRESH_PARKING_RETRY` | `Tunables.ParkRetry` (new) |

Fixed (not configuration, defined in `Tunables` so tests can override): `AbandonAfter` 3 s, `StuckAfter` 5 s, `GateRecheck` 5 s, `Heartbeat` 10 s, `MaxPause` 65 min, `RateLimitMin` 60 s, `RateLimitMax` 15 min, planner re-try after an enqueue refusal 30 s, plan flush 5 s. The jitter seed is not configuration: `cmd/scp` draws it from `crypto/rand` at start (tests pass fixed seeds). Per-kind TTL, jitter and idle cap are `job.Spec.Policy` values supplied by the owning features (architecture 9.6), each with its own config key in that feature.

### 6.2 `github.defaults.webhook` and `github.sources[].webhook` (consumer: `webhook.NewReceiver` / `NewRedelivery`)

**Ownership (CD-CF-04, cross-design review):** the keys below are declared once, in unit 05's `github.WebhookDefaults` and `SourceConfig.Webhook` structs (05 section 6, which now carries all seven `defaults.webhook` keys and the `redelivery` `rate` and `max_age`); `webhook.Config` is the consumer-side copy that `cmd/scp` fills from them. Ranges and defaults are the ones in this table.

| Key | Type | Default | Validation | Env example | Consumed by |
|---|---|---|---|---|---|
| `github.defaults.webhook.max_body` | size | 25MiB | 1KiB..64MiB | `SCP_GITHUB_DEFAULTS_WEBHOOK_MAX_BODY` | `Config.MaxBody` (GitHub caps payloads at 25 MB, verified 2026-10-14; the `httpx` body-limit override for this route is 26 MiB, architecture 11.1, so this limit is the effective one) |
| `github.defaults.webhook.dedupe_ttl` | duration | 15m | 1m..24h | `..._DEDUPE_TTL` | `Config.DedupeTTL` |
| `github.defaults.webhook.dedupe_max` | int | 10000 | 100..1,000,000 | `..._DEDUPE_MAX` | `Config.DedupeMax` |
| `github.defaults.webhook.max_concurrent` | int | 4 | 1..32 | `..._MAX_CONCURRENT` | `Config.MaxConcurrent` (new) |
| `github.defaults.webhook.acquire_wait` | duration | 2s | 0..9s (must stay below GitHub's 10 s) | `..._ACQUIRE_WAIT` | `Config.AcquireWait` (new) |
| `github.defaults.webhook.retry_after` | duration | 5s | 1s..5m | `..._RETRY_AFTER` | `Config.RetryAfter` (new) |
| `github.defaults.webhook.body_timeout` | duration | 8s | 1s..9s (below GitHub's 10 s) | `..._BODY_TIMEOUT` | `Config.BodyTimeout` (new; slow-body defence, 4.13) |
| `github.sources[].webhook.enabled` | bool | true | | `SCP_GITHUB_SOURCES_0_WEBHOOK_ENABLED` | `cmd/scp`: whether the source is in `Sources.IDs` (new; false is the explicit "no webhooks for this source" answer to the startup refusal below) |
| `github.sources[].webhook.default` | bool | implied for a single source | at most one `true` | `..._WEBHOOK_DEFAULT` | `Sources.Default` (the source-less route; new) |
| `github.sources[].webhook.secret` | secret | none | non-empty when `enabled`; **startup error** otherwise (GR-20) | `SCP_GITHUB_SOURCES_0_WEBHOOK_SECRET_FILE` | `github.Verifier` (current secret) |
| `github.sources[].webhook.previous_secret` | secret | none | optional; must differ from `secret` | `..._WEBHOOK_PREVIOUS_SECRET_FILE` | `github.Verifier` (rotation overlap; new) |
| `github.sources[].webhook.public_url` | URL | | https; SSRF policy | | `scp doctor` (not read here) |
| `github.sources[].webhook.redelivery.enabled` | bool | false | only for `github_app` sources | `..._REDELIVERY_ENABLED` | `RedeliveryConfig.Enabled` |
| `...redelivery.interval` | duration | 6h | 10m..48h | | `Spec.Policy.TTL` |
| `...redelivery.max_per_run` | int | 50 (was 200; CD-CF-03: unit 05's value, the sweep is rate-paced at 1 s per call) | 1..500 | | `RedeliveryConfig.MaxPerRun` (new) |
| `...redelivery.rate` | duration | 1s | 100ms..1m | | `RedeliveryConfig.Rate` (new) |
| `...redelivery.max_age` | duration | 72h | 1h..168h (github.com 3 days; GHES documents 7, research 09) | | `RedeliveryConfig.MaxAge` (new) |
| `ratelimit.webhook_invalid` | rate | 10/min burst 10 | | `SCP_RATELIMIT_WEBHOOK_INVALID` | `httpx` keyed limiter class behind `webhook.Limiter` (new class; architecture 17.4 lists the policy, 16 omits the key) |

Startup validation owned here (architecture 16 list): a source with `webhook.enabled` and an empty secret is refused (a startup error, GR-20; the receiver would never verify an empty secret anyway); `default` on more than one source; `redelivery.enabled` on a non-App source; `previous_secret == secret`; `acquire_wait + body_timeout >= 10s`.

**Keys that two units define (X6, RESOLVED by the cross-design review CD-CF-02 in favour of unit 05's path, as unit 16 D-7 decided; this paragraph is kept as the record).** Architecture 16 and unit 05 also define `github.defaults.budget.{reserve, concurrency, rps, burst}` for the **adapter** (a semaphore of 4 and a limiter of 8 requests/s under the engine) and unit 05 implements the reserve arithmetic itself (`Client.Admit(ReserveClass)`, reserves 0.40/0.15/0.02, mapped by `bridge.RateGate`, which is what architecture 6.2/8.6 and ADR-015 literally describe), whereas this document (D3) puts the same arithmetic into `refresh.NewReserveGate` with keys `refresh.gate.reserves.*`. **Default taken here:** this document as written (`refresh.NewReserveGate` over a `Budget` bridge, 6.1 keys). **If the owner prefers 05's path** (it is the one that matches the architecture text): delete `NewReserveGate`, `Budget`, `BudgetState`, `Reserves` and the three `refresh.gate.reserves.*` keys from this unit, wire `refresh.WithGate(bridge.RateGate)`, and nothing else changes (the `Gate` interface is identical). **In no case may both exist**: two sets of reserve keys, one of them unconsumed, fail the documented-keys == consumed-keys test (GR-12) and two gates would double-count. `refresh.upstreams.github.concurrency` (engine slots) and `github.defaults.budget.concurrency` (adapter semaphore) are independent layers; the smaller one is the effective limit, and the runbook must say so.

---

## 7. HTTP surface owned

Route class **server-only** (never falls back to the SPA, architecture 11.2; reserved prefix `/webhooks/`), implemented by `webhook.Receiver.ServeHTTP`. No OpenAPI fragment (it is not part of `/api/v1`; GitHub is the only client). `CrossOriginProtection` is bypassed for exactly these routes (A-11; architecture 11.1 `POST /webhooks/github*`; GitHub sends neither `Origin` nor `Sec-Fetch-Site`, and requests without them are not cross-origin browser requests), the body-limit override is 26 MiB behind the 4-slot semaphore, rate limiting is none for valid signatures and the `webhook_invalid` class otherwise.

| Route name (unit 01 `routes.Table`, architecture 11.2 "table owner may split names") | Pattern | Notes |
|---|---|---|
| `webhook.source` | `POST /webhooks/github/{source}` | `{source}` = `github.sources[].id`; unknown -> 404 |
| `webhook` | `POST /webhooks/github` | the default source; no default -> the receiver answers 404 |

**Both patterns are always registered** to the same handler (a first draft left the source-less pattern out when no source is the default, which `Table.Verify()` reports as a missing handler: architecture 10.1 step 7 makes that a startup error). The receiver's 404 for "no default" is therefore the tested path (`TestNoDefaultSourceMeansSourcelessRouteIs404`); when no source enables webhooks at all, `cmd/scp` registers a 404 problem handler on the two patterns instead of a `Receiver`.

A method-less pattern would conflict with `GET /{path...}` (A-12), hence `POST ` in every pattern. Request: `Content-Type: application/json`, headers `X-GitHub-Event`, `X-GitHub-Delivery`, `X-Hub-Signature-256` (plus informational `X-GitHub-Hook-ID`, `User-Agent: GitHub-Hookshot/...`). Responses: section 4.14. ETag/caching: not applicable (`Cache-Control: no-store`). Authz class: unauthenticated-by-session; authenticated by HMAC only; the admin listener never serves it (A-12 reserved prefixes, GR-17).

---

## 8. Concurrency and lifecycle

Goroutines owned (extends architecture 10.3; every one has an owner that waits for it, G10):

| Goroutine(s) | Owner | Started by | Stopped by | Notes |
|---|---|---|---|---|
| engine dispatcher (1) | `refresh.Engine` | `Engine.Run` | `ctx` cancel | one timer; wakes on `wake` channel (cap 1), the next due time, or `Heartbeat` |
| job workers (<= sum of `MaxConcurrent`) | `refresh.Engine` | dispatcher (`WaitGroup.Go`) | job return; cancelled at `Grace` | recover panics; hooks run on this goroutine after unlock |
| scheduler task goroutines (1 per task) | `refresh.Scheduler` | `Scheduler.Run` | `ctx` cancel; joined | |
| webhook request goroutines | `net/http` server (owned by `lifecycle` servers stage) | per request | request end; `Server.Shutdown` | reactors run synchronously here; bounded by the 4 body slots only while reading/verifying |
| plan flusher | bridge (refresh stage) | stage `Run` | stage cancel; final flush in `Close` | |
| (none for dedupe, redelivery) | | | | dedupe evicts lazily on call; redelivery runs as an engine job |

Locks: `Engine.mu` (one; never held across a call out: hooks, `Gate.Admit`, `Spec.Run`, `Enqueue` from a hook all run unlocked); `Planner.mu` (one; copy-then-call-out); `Scheduler.mu` (status only); `dedupe.mu`; `Receiver.mu` (result counters). Lock order: none nested (`Planner.Enqueue` takes `Planner.mu`, releases it, calls the engine, re-takes it only to revert on error). `Planner.OnResult` is called by the engine after its mutex is released, so engine -> planner never nests either.

Start order (architecture 8.16, 10.1): telemetry, store, **refresh stage** (engine, scheduler, flusher), then servers. Stop order is the reverse: servers stop first (no new webhooks are accepted; webhooks accepted before were enqueued and, if the engine was already closed, answered 503), then the refresh stage drains (section 4.8), then store, telemetry. Cancellation: the engine never sees the signal context directly; it gets the stage context (one context per stage, `lifecycle`), and jobs get `hctx` (cancelled only at `Grace`).

Goroutine hygiene: `goleak.VerifyTestMain` in both packages (outside bubbles); inside a `synctest` bubble the bubble itself fails on leaked blocked goroutines. Package-level `sync.WaitGroup` variables are forbidden (they cannot be bubble-associated); all waitgroups are struct fields.

---

## 9. Observability

Instruments are created from the injected `metric.Meter` (G11); names are exported constants next to the instruments and the generated `docs/reference/metrics.md` test compares them (verified in the prototype: an SDK `ManualReader` collected exactly the instrument names below for the exercised paths, `internal/e2e`). Label sets are bounded: `kind` comes from registered specs only, `upstream` from config, `result`/`outcome` from fixed vocabularies; **delivery ids, repository slugs, event names from headers and error texts are never labels**.

### 9.1 `refresh`

| Name | Type, unit | Labels | Meaning |
|---|---|---|---|
| `scp_sync_pending` | gauge | `upstream` | ready + delayed entries |
| `scp_sync_running` | gauge | `upstream` | running jobs |
| `scp_sync_parked` | gauge | | parked keys |
| `scp_upstream_paused_seconds` | gauge, s | `upstream` | seconds until the pause ends, 0 when not paused |
| `scp_sync_jobs_total` | counter | `kind`, `outcome` in `started success coalesced marked_dirty retried ratelimited parked parked_suppressed panic exhausted gone canceled queue_full` | architecture 18.2 set plus `started`, `parked_suppressed`, `gone`, `canceled`, `queue_full` |
| `scp_job_duration_seconds` | histogram, s | `kind` | run time |
| `scp_job_wait_seconds` | histogram, s | `kind` | `now - runAt` at start (queue latency) |
| `scp_job_overrun_total` | counter | `kind` | returned after its deadline |
| `scp_job_ignored_cancel_total` | counter | `kind` | stuck: ignored cancellation for 5 s |
| `scp_gate_denied_total` | counter | `upstream`, `priority` in `interactive webhook scheduled backfill` | gate denials per dispatch pass (names, not numbers: kernel priorities are 1-based) |
| `scp_webhook_to_publish_seconds` | histogram, s | `kind` | **end-to-end freshness**: time from the first event-priority (`PrioInteractive`/`PrioWebhook`) enqueue of a key to the success of the run that served it. Measured in the engine because only it sees both ends (D14); includes `head_changed`-driven enqueues (same priority), which no label can tell apart in v1 (the `job.Enqueuer` carries no origin). Prototype: 0.30 s for a 0.3 s job |
| `scp_planner_tick_seconds` | histogram, s | | duration of a `Tick` |
| `scp_planner_enqueue_total` | counter | `kind`, `result` in `ok closed queue_full error` | planner enqueue outcomes |
| `scp_planner_subjects` | gauge | `kind` | subjects per kind |
| `scp_item_age_seconds` | **not emitted here; emitted by `ops` (15 D15-6), settled CD-MT-02** | | architecture 18.2 lists it under the engine, but the engine and planner know no `fetched_at` (only `NextDue`), and unit 09 assumes the engine contributes it: open question Q12 (default: `ops` derives it from the snapshot's `freshness.Info.FetchedAt`, which is exact and includes 304 revalidations) |
| `scp_task_runs_total` | counter | `task`, `outcome` in `ok error` | scheduler runs |
| `scp_task_overruns_total` | counter | `task` | `duration > Every` |

### 9.2 `webhook`

| Name | Type, unit | Labels | Meaning |
|---|---|---|---|
| `scp_webhook_total` | counter | `result` in `accepted duplicate invalid_signature malformed ignored ping queue_full unknown_source too_large unsupported_media throttled busy body_timeout error` | the architecture's six values plus eight additive ones (D5) |
| `scp_webhook_body_bytes` | histogram, By | | accepted body sizes (sizing `max_body`) |
| `scp_webhook_dedupe_entries` | gauge | | size of the de-dup set |
| `scp_webhook_last_accepted_timestamp_seconds` | gauge, s | | unix time of the last accepted delivery (alert on silence) |
| `scp_webhook_redelivery_total` | counter | `source`, `result` in `redelivered gone failed gap` | sweep outcomes |

### 9.3 Logs (levels and fields; `slog` JSON; fields from architecture 18.1)

| Event | Level | Fields |
|---|---|---|
| job parked | ERROR (once) | `job_key`, `job_kind`, `reason`, `retry_at` |
| job panicked | ERROR | `job_key`, `job_kind`, `panic`, `stack` |
| job ignores cancellation (stuck) | ERROR | `job_key`, `job_kind`, `upstream` |
| retries exhausted | WARN (once) | `job_key`, `job_kind`, `attempt`, `err` |
| upstream paused by rate limit | WARN | `upstream`, `until`, `streak` |
| webhook rejected (401) | WARN | `reason`, `delivery_id` (sanitised), never the body or headers |
| refresh hook or task `OnDone` panicked | ERROR | `panic`, `stack` |
| webhook not accepted: engine refused | WARN | `delivery_id`, `source`, `event`, `err` |
| webhook reactor error | ERROR | `delivery_id`, `source`, `event`, `err` |
| webhook verifier failure | ERROR | `source`, `err` |
| redelivery watermark not saved | WARN | `source`, `err` |

Job and webhook context attributes (`job_key`, `job_kind`, `attempt`, `delivery_id`, `trace_id`) flow through the context to the `logx` handler; `LogAttrs(ctx, ...)` always passes the context. Payloads and secrets are never logged (`Secret` cannot format).

### 9.4 Traces

`job.run` (kind, key, upstream, attempt, outcome; error recorded, status error on failure) wraps every execution; `webhook.handle` (event, delivery) wraps every delivery. The architecture's span link `webhook.handle -> job.run` (18.3) **cannot be built**: `job.Enqueuer.Enqueue` carries no context, so there is nothing to link through (D12). Correlation is by `delivery_id` in the receiver's logs and the reactors' debug log of the enqueued `job_key`.

---

## 10. Security considerations

Threat -> control, specific to this unit (extends architecture 17.2 "Webhook" and guardrails GR-20, GR-21):

| Threat | Control | Test |
|---|---|---|
| Forged delivery | HMAC-SHA256 over raw bytes, `hmac.Equal`, SHA-1 header ignored, empty secret never verifies, per-source secret, route absent without a secret | official vector + 12 rejection cases (`ghverify`), `TestInvalidSignatureMatrix` |
| Parsing/logging unauthenticated input | order size -> verify -> decode; body never logged; unauthenticated header content sanitised before logging | `TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised`, fuzz |
| Giant bodies, memory | `Content-Length` pre-check, `MaxBytesReader`, 4 body slots (memory <= 4 x (25 MiB + 512 B) with a Content-Length; about 2.5x for chunked), buffer preallocated to the Content-Length | `TestRequestShapeRejections`, `TestBodySlotsBoundConcurrency` |
| Slow-body sender holding the 4 slots (the signature header is free to forge, so the slots are taken before verification) | per-request read deadline `body_timeout` 8 s through `ResponseController.SetReadDeadline` (below GitHub's 10 s; the server `ReadTimeout` of 30 s alone would let four connections block every delivery for 30 s each), 408 and the slot is released; residual distributed risk documented in 4.13 | `TestSlowBodyReleasesItsSlotAtBodyTimeout` |
| Hostile `X-GitHub-Delivery` from a verified sender (memory of the de-dup set, response body injection) | `[0-9A-Za-z-]{1,64}` or 400; the 202 body is built with `encoding/json` | `TestHostileDeliveryIdFromAVerifiedSenderIsRefusedAndNeverStored` |
| A bug in a hook or bridge takes the process down | hooks and `Task.OnDone` are recovered and logged | `TestHookPanicIsContained` |
| HMAC CPU as a DoS | plausibility check before reading the body; body slots; per-IP invalid budget upgrades to 429 | `TestInvalidSignatureMatrix` (the verifier is not called for implausible headers) |
| Source confusion | secret chosen by `{source}` only; verifier never tries other sources' secrets; unknown source 404 | `TestAcceptedPushReachesSink`, 404 cases |
| Replay of a captured signed body | de-dup for 15 min; **beyond the window a replay is processed**, which costs one idempotent, coalesced refresh (the signature covers no timestamp; GitHub documents none): accepted risk W12 | `TestDuplicateWithinTTLAndAfter` |
| Redelivery swallowed by de-dup | record only after a successful enqueue | `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted` |
| Event storms | keyed coalescing (10,000 pushes to one repo = 1 run + 1 follow-up), `MaxKeys`, 503 beyond | `TestCoalesceQueuedAndDirtyWhileRunning`, `TestQueueFullAndUnknownKind` |
| Poison job loops | permanent -> park with 24 h probe; panic -> park; transient backoff capped at 15 min; rate limit -> upstream pause (GitHub warns that continuing while limited can get an integration banned) | engine tests |
| Information leaks | problem bodies are fixed text; no echo of headers/body (GR-17); `Retry-After` only on 429/503 | `TestRequestShapeRejections` |
| Wrong repository/branch trusted from a signed payload (GR-21) | the receiver trusts nothing in the payload; reactors resolve `RepoID`/full name against the catalogue and `refs/heads/<default>` only | reactor tests (other units) + `TestEveryEventKindIsHandledOrCountedIgnored` |
| Secret exposure | `secret.Secret` everywhere; `*_FILE`; verifier keeps secrets as `[]byte` only inside the HMAC call; config logging iterates typed fields | config tests |
| IP allow-list (GitHub publishes webhook IPs in `GET /meta` `hooks`) | **not implemented in v1**; documented as defence in depth for operators at the proxy (GitHub: "This can block spoofed requests"); HMAC is the authentication | Q9 |
| `X-Forwarded-*` spoofing for the invalid-signature key | `clientIP` is the trusted-proxy resolver of `httpx` (architecture 17.4) | httpx tests |

Never-do list for implementers: no `==` on signatures; no string conversion of the body before verification; no `log` of `r.Header` wholesale; no default secret; no `http.Error` with `err.Error()`; no retry loop inside the handler; no goroutine spawned from the handler.

---

## 11. Feature and guardrail trace

### 11.1 Features and gaps

| Feature | Implemented in section | Notes |
|---|---|---|
| Webhook receiver and signature check | 4.12, 4.13, 4.14, 7 | SHA-256, raw bytes, size limit, 202, 503 + `Retry-After`, refuse to start without a secret (6.2) |
| Push: commit refresh | 4.13 (routing), 4.12 (`Event` fields), 2.6 (reactor contract) | one coalesced job per repository, `refs/heads/` only; reactors live in `commits` |
| Push: documentation refresh | 4.12 (`ChangedPaths` union, `PathsComplete`), 4.16 | touched-path logic is `docs.React`; added/removed/renamed count |
| `create`/`release` events | 4.13 (forwarded, no stubs), 4.16 | reactions in `catalog`, `docs`, `manifests` |
| `ping` | 4.13 | verified, 202 `pong`, counted, no side effect |
| Startup prefetch | 4.9 (cold start), 4.11 | first run of the engine; readiness gated by `ops` |
| Periodic refresh | 4.9, 4.10 | every kind on its TTL, GitHub data included |
| Queue with retry | 4.1-4.8 | keyed, coalescing, per-upstream, jittered, counted |
| Resilience and rate limits | 4.4, 4.5 (consumption side), 3.3 | classification is the adapter's; pause and gate are here |
| G-02 freshness model | 4.9 (planner feeds `next_due_at`), 5 | `fetched_at` etc. belong to the feature jobs |
| G-03 conditional requests, rate-aware scheduling | 4.3, 4.9 idle back-off, 3.3 gate | |
| G-04 readiness gating, explicit startup | 4.11, 2.5 | |
| G-05 stale-while-error | 4.4 | the engine never touches payloads; failures only schedule retries |
| G-06 delivery de-dup and replay protection | 4.13 | bounded in-memory set, after enqueue |
| G-11 graceful shutdown, resumable sync | 4.8, 5 | grace drain, restore of plans and parked keys |
| Live refresh of the commit panel | 4.9, 4.13 | freshness is produced here (webhook accelerator within seconds, planner TTL otherwise); the version signal and the panel belong to `live` and `commits` |
| Health endpoints | 4.11, 4.7 | this unit supplies the outcome-based inputs (`Heartbeat`, `AllStuckSince`, `Progress`, `Receiver.Stats`); the endpoints are `ops` |
| Work-engine status | 4.11, 9.1 | `Inspect` and the `scp_sync_*` gauges; nothing reports a pool as saturated merely because it is busy at start-up (GR-41) |
| Scheduled task status | 4.10 | `Scheduler.Status(name)` -> `/status` |
| Secrets handling (webhook secret) | 6.2, 10 | `secret.Secret` from `*_FILE`, current and previous secret, never logged or formatted |

### 11.2 Guardrails -> mechanism

| Guardrail | Mechanism in this unit |
|---|---|
| GR-20 webhook ingress: limit, verify, then parse | SHA-256 over raw bytes, `hmac.Equal`, format check (4.12); size -> verify -> parse order and `MaxBody` (4.13); empty secret is a startup refusal and never verifies (6.2); de-dup of delivery ids (4.13); no body or unverified header content in logs, sanitised delivery header (9.3, 10); vector, rejection and fuzz tests |
| GR-21 webhook events are hints that become coalesced refresh jobs | receiver is repository-agnostic; reactors resolve against the catalogue (contract 2.6); `ChangedPaths` union over all commits and `PathsComplete`; reactors enqueue "refresh repository X", never fetch (4.12, 4.16); every kind handled or counted `ignored` (`TestEveryEventKindIsHandledOrCountedIgnored`); 503 + `Retry-After` on a full queue and the redelivery job (4.14, 4.15) |
| GR-10 Git refs are resolved from the repository | receiver forwards the full `Ref`; reactors compare with `refs/heads/<default branch>` |
| GR-16 truthful HTTP responses | status table 4.14; an unclassified verifier error is a 500, never a 400 |
| GR-17 nothing reflects requests | fixed-text problems, no echo of headers or body (10); the admin listener never serves the webhook (7) |
| GR-18 strict browser policy | no CORS code anywhere; the webhook routes bypass `CrossOriginProtection` explicitly and are authenticated by HMAC only (7) |
| GR-24 the job engine never loses work silently | 4.2-4.6: delayed full-jitter retries, exhaustion counter and log, parking, `ErrQueueFull` that never throws, coalescing; 4.10: fixed-delay scheduler, recover, skip-if-running, completion-time status |
| GR-22 one client, breaker and limiter per upstream | per-upstream heaps, concurrency, pace, pause; the breaker lives in the adapter (one per upstream) |
| GR-23 typed upstream failures, rate limits honoured | 4.4 classification by behaviour interfaces; a rate limit pauses the upstream without consuming an attempt |
| GR-25 upstream requests are budgeted | gate reserves, pace, idle back-off, coalescing; cold start 300 s for 3,000 items, no double runs (`TestColdStartThreeThousandItems`) |
| GR-26 freshness is data | planner TTL for every kind (4.9) |
| GR-41 health derives from outcomes | outcome-based `/livez` inputs: dispatcher heartbeat and the stuck-slot rule (4.7, 4.11); no probe makes a call; every `Task` has a consumer and records completion; goroutine/queue/in-flight metrics (9.1) |
| GR-01 / GR-02 explicit startup, readiness gated on data | one explicit startup order (2.5), first `catalog.reconcile` enqueued through the planner, cold start and readiness inputs (4.9, 4.11) |
| GR-03 explicit dependencies, owned lifecycles | no package-level state; `Deps`, options and constructors only (3.1, 8); `Run`-based lifecycles; `-race` and goleak tests |
| GR-13 secrets are a type | `secret.Secret` everywhere, `LogAttrs` with fixed fields, `Secret` cannot format (9.3, 10) |
| GR-12 one strict configuration | one `github.sources[].webhook.secret` (+ `previous_secret`) per source, strict decode, documented-keys == consumed-keys test (6, 12.3 item 9) |
| GR-43 tests assert and exercise the shipping code | the receiver is tested through the real `ServeMux` patterns with signed deliveries, status and metric assertions (12.2) |
| GR-04 nothing without a consumer | every option, hook and interface has a consumer in the wiring (2.5); X6 records the one place where two units define the same thing and requires one to go |

### 11.3 Fixture provenance

`push_two_commits.json`, `create_tag.json` (`ref: "v0.0.1"`, `ref_type: "tag"`, no `action`), `release_published.json` (`action: "published"`, `release.tag_name`) and `ping.json` (`zen`, `hook_id`) are trimmed from recorded organisation-webhook deliveries (2018). `repository_renamed.json`, `installation_created.json` and `installation_repositories_added.json` are hand-written from the documented shapes and must be replaced by real recordings in M0 (T0.2).

---

## 12. Test plan

### 12.1 What already exists and passes (reference prototype, `04-refresh-webhook.assets/`)

Environment: Go 1.27.1 (installed; architecture pins 1.27.2), `GOFLAGS=-mod=mod`, otel `metric`/`trace`/`sdk/metric` v1.47.0 (module versions fetched 2026-10-14). `go vet ./...` clean, `gofmt -l` empty, `go test -race -count=10 ./...` green (re-run by the independent verifier on 2026-10-14 after the fixes: 62 tests, 2 fuzz targets), fuzz `FuzzReceiverNeverPanics` 20 s (4.5 M execs) and `FuzzVerifyNoPanic` 10 s (3.7 M execs) found nothing. The kernel stubs in the prototype now follow unit 01 (1-based `Priority`/`Scope`) and unit 06 (`Problem.Header`), which is what exposed the index-out-of-range and the missing `Retry-After` of the first draft. Not compile-checked: `bridge` types, `reactor.Fan`, `github.Verifier` (other units; stubs and the reference verifier stand in).

Time is virtual everywhere: engine, planner, scheduler and receiver tests run inside `testing/synctest` bubbles (no `Clock` interface, ADR-003); randomness takes a seed; the receiver is driven with `httptest.NewRecorder` and `http.ServeMux` (no sockets).

### 12.2 Named cases (implement all; the prototype has the bodies)

Engine (`refresh/refresh_test.go`; all in a bubble):

| Test | Asserts |
|---|---|
| `TestCoalesceQueuedAndDirtyWhileRunning` | 10 enqueues while queued = 1 run; 10 while running = exactly 1 follow-up; stats `marked_dirty=10`, `coalesced=9` |
| `TestPaceAndConcurrency` | peak concurrency = `MaxConcurrent`; start offsets 0, 100 ms, 200 ms, 1 s, 1.1 s |
| `TestBackoffBounds` | every delay in `[Base, min(Cap, Base<<attempt)]` for attempts 1..199; saturating shift (Base 1 h, attempt 100) |
| `TestTransientRetryExhaustedKeepsRetrying` | `OnExhausted` once at attempt 8; run 9 succeeds; `retried=8` |
| `TestRateLimitPausesUpstreamWithoutAttemptAndFloorGrows` | nothing starts before `RetryAt`; 4 keys run after; `retried=0`; past `RetryAt` gives 60 s then 120 s floors |
| `TestPermanentParksForceUnparksProbeAfter24h` | parked after one run; suppressed enqueue observed as `ParkedError`; probe at 24 h fails and re-parks; force runs and unparks |
| `TestPanicIsContainedAndParked` | `panic=1`, `parked=1`, process alive |
| `TestGoneDropsKeyWithoutParking` | key gone, nothing parked |
| `TestHooksRunOutsideTheMutex` | `OnParked` and `Observe` call `Inspect`/`Enqueue` without deadlock (the reproduced prototype defect) |
| `TestEnqueueAfterRunReturnedIsErrClosed` | `errors.Is(err, job.ErrClosed)` |
| `TestQueueFullAndUnknownKind` | `ErrQueueFull` at `MaxKeys`, coalescing does not need a slot, `ErrUnknownKind` |
| `TestGateReservesPerPriority` | at 30% remaining backfill waits, scheduled and webhook run; recovery to 90% releases backfill within `GateRecheck` |
| `TestBackoffEntryNotPromotedByEnqueueUnlessForced` | webhook enqueue does not skip a backoff; force does |
| `TestRateLimitOneEventManyInFlightJobsDoesNotEscalate` | four concurrent jobs, one limit event: pause <= 62 s, streak 1 |
| `TestGateAppliesToPromotedDelayedEntries` | a delayed backfill entry promoted while the gate denies backfill does not start; starts within `GateRecheck` after the gate opens |
| `TestHookPanicIsContained` | a panicking `OnParked` and observer: process alive, other jobs run, hooks counted |
| `TestPermanentAndRateLimitedParksAndStillPauses` | unit 01 JB-05 precedence: parked and the upstream paused |
| `TestPrioritiesAreOneBasedAndZeroIsInvalid` | 1-based kernel priorities order correctly; 0 is `ErrInvalidPriority`; 200 clamps to backfill |
| `TestWithTunablesKeepsDefaultsForZeroFields` | merge semantics |
| `TestJobTimeoutOverrunAndStuckAllSlots` | not stuck at deadline + 2 s, stuck 5 s past it, `AllStuckSince` set, cleared when the job returns |
| `TestShutdownGraceDrainsThenCancels` | a 5 s job completes uncancelled, a blocked one is cancelled at exactly 15 s, `Run` returns `nil` at 15 s |
| `TestShutdownAbandonsJobsThatIgnoreCancel` | `ErrAbandoned` |

Planner and scheduler (same file):

| Test | Asserts |
|---|---|
| `TestPlannerTickDuringRunningJobDoesNotRunTwice` | a 40 s job across five ticks runs once; `marked_dirty=0` |
| `TestPlannerWebhookRunInFlightIsNotTickedAgain` | a `Planner.Enqueue` run in flight is not ticked again (the ordering bug fixed during prototyping) |
| `TestPlannerIdleBackoffRestoreAndColdPriority` | restored state not due, unchanged streak doubles up to the cap, `Progress` counts |
| `TestPlannerColdItemsUseBackfillPriority` | repo kinds start at `PrioBackfill`, global kinds keep their priority |
| `TestPlannerLearnsParkedAfterWarmStart` | parked item is not re-ticked |
| `TestPlannerGoneForgetsItem`, `TestPlannerQueueFullReturnsItemToIdle` | forgotten / retried after 30 s |
| `TestFirstGlobalJobThroughPlannerIsNotRunTwice` | the first `catalog.reconcile` through `Planner.Enqueue` runs once; through the engine directly it runs twice (documents the wiring rule) |
| `TestSchedulerJitterPanicTriggerAndFrozen` | 54..66 s intervals, panic survived and counted, `Trigger` on a running task skipped, `Add` after `Run` panics |

Receiver, de-dup, redelivery (`webhook/webhook_test.go`):

| Test | Asserts |
|---|---|
| `TestAcceptedPushReachesSink` | 202 on both routes; the event carries `Source`, `ChangedPaths` union; stats |
| `TestDuplicateWithinTTLAndAfter` | duplicate 202 not forwarded; after 15 min 1 s processed again |
| `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted` | 503 + `Retry-After: 5` for `ErrQueueFull`, `ErrClosed`, wrapped, joined; the retry is accepted |
| `TestOtherReactorErrorsAreAcceptedAndRecorded` | 202 and recorded |
| `TestInvalidSignatureMatrix` | missing, SHA-1 only, short, non-hex, wrong value: 401; sink never called; verifier called only for the plausible one |
| `TestInvalidSignatureBudgetGives429ButValidIsNeverLimited` | `[401 401 429 429]`, then a valid delivery 202 |
| `TestRequestShapeRejections` | 413 (Content-Length and chunked), 415 (form-encoded), 404 unknown source, second source 202, ping `pong` not forwarded, unsupported event `ignored`, undecodable 400, unclassified verifier error 500, GET not 2xx |
| `TestNoDefaultSourceMeansSourcelessRouteIs404` | 404 and constructor errors |
| `TestBodySlotsBoundConcurrency` | 2 slots, 3 requests: third 503 after `acquire_wait`; others 202 |
| `TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised` | canary absent from logs; one log line; newline injection neutralised |
| `TestConcurrentDeliveriesRace` | 200 parallel deliveries, 10 ids, `-race` clean |
| `TestDedupeEvictionAndNoRefreshOnHit` | FIFO eviction at cap, TTL expiry, hits do not extend life |
| `TestSlowBodyReleasesItsSlotAtBodyTimeout` | one slot, a never-finishing body: real delivery gets 503; after `body_timeout` the slow one gets 408 and the slot is free |
| `TestHostileDeliveryIdFromAVerifiedSenderIsRefusedAndNeverStored` | empty, 65 chars, quote, space, slash, non-ASCII: 400, nothing stored or forwarded; an ignored event echoes a sanitised id |
| `TestInvalidSignatureSpellingOfUnit05Gives401` | `InvalidSignature()` alias |
| `TestRedeliveryAdapterTruncationKeepsWatermark` | adapter `Truncated()` keeps the watermark; other list errors fail the run |
| `TestRedeliverySpecIsValidAgainstTheKernelRules` | unit 01 JB-03 fields of the `webhook.redelivery` spec |
| `TestRedeliveryPacesAdvancesWatermarkAndSkipsGone` | 1 s spacing, 24 h first lookback, watermark `scanStart - 1 min`, `Unchanged` when idle |
| `TestRedeliveryTruncatedRunDoesNotAdvanceAndErrorsPropagate` | cap 3 of 5 keeps the watermark; a rate limit propagates as `job.RateLimited` |
| `TestRedeliveryGapEmitsResyncAndLimitsWindow` | `installation_repositories`/`resync` event once, window 72 h, `ErrQueueFull` fails the run |
| `TestRedeliveryDisabledHasNoJob` | `Jobs()` is nil |
| `FuzzReceiverNeverPanics` | any body/event/signature/content type: status in {202,400,401,413,415,429,500,503} |

Reference verifier (`ghverify`): `TestOfficialVector`, `TestSignatureRejections` (10 cases + rotation + upper-case hex), `TestDecodeRecordedPush`, `TestMissingArraysMeanIncompleteNotEmpty`, `TestMissingEventHeaderIsMalformedAfterAuth`, `TestDecodeOtherEvents` (create tag, release, repository, installation, installation_repositories, ping; star ignored; create without repository malformed), `TestForcedOrCommitlessPushHasIncompletePaths`, `FuzzVerifyNoPanic`. Integration (`e2e`): `TestImportsStayInsideTheMatrix` (`go list` of both packages against their rows of 5.2), `TestColdStartThreeThousandItems` (3,000 runs, none more than twice, peak <= 4, `marked_dirty=0`, last first-pass start at 4 m 59.9 s), `TestWebhookToJobAndEndToEndMetric` (signed push -> receiver -> fan-out -> planner -> engine -> job; `scp_webhook_to_publish_seconds` about the job time).

### 12.3 Still to write (not in the prototype)

0. **Mutation check (done by the verifier, 2026-10-14; repeat in CI as a nightly job if cheap)**: each regression test fails when its fix is reverted. Seven one-line mutations of the prototype and the test that caught each: no hook `recover` -> `TestHookPanicIsContained` (process panic); no read deadline -> `TestSlowBodyReleasesItsSlotAtBodyTimeout` (deadlock detected by the bubble); `Retry-After` only in the body -> `TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted`; forced/commit-less rule removed -> `TestForcedOrCommitlessPushHasIncompletePaths`; gate asked only for non-empty ready classes **and** the unasked deferral removed -> `TestGateAppliesToPromotedDelayedEntries` (either half alone is not caught: they are redundant, the first saves one dispatch pass); priority used as an array index -> every engine test (the run hangs until the test timeout: a panic on the dispatcher goroutine, so a `-timeout` is mandatory in CI); per-result rate-limit streak -> `TestRateLimitOneEventManyInFlightJobsDoesNotEscalate`.
1. `TestEveryEventKindIsHandledOrCountedIgnored`: iterate every `scm.EventKind`; a table says forwarded / handled / synthetic; adding a kind without a row fails.
2. Model-based fuzz of the engine queue: random `Enqueue`/complete/advance sequences against a 30-line reference model; invariants of 3.5 (one running per key; every enqueue followed by a run that starts after it; no key lost unless parked; `len(keys) <= MaxKeys`).
3. `-race` stress: 64 goroutines enqueueing, planner ticking, hooks re-entering, 10 s of virtual time, `-count=30`.
4. `goleak.VerifyTestMain` in both packages for the non-bubble tests.
5. Budget arithmetic test (the architecture's W13): fake GitHub counting calls for 1,000 repos x 3 kinds: cold start call count, steady state per hour with and without webhooks, restart inside the TTL = about 0 calls; acceptance: within the numbers of architecture 13.4/9.6.
6. Bridge tests (other unit): `PlanSink` flush batching and final flush; `Park`/`Unpark` persistence round trip through `store/memory`; `Watermarks` over `LoadTasks/SaveTask`.
7. `cmd/scp` `run()` e2e: boot against `githubtest` with a configured secret; POST a signed push; assert 202, the job ran, snapshot version advanced, `/readyz` flips; POST with a wrong signature 401; send the same delivery twice; kill the engine stage and see 503.
8. Contract/golden: the problem bodies of 4.14 (fixed text), the exact response JSON of 202 variants.
9. Config tests: documented keys == consumed keys for sections 6.1/6.2; startup refusals (empty secret, two defaults, redelivery on a PAT source, `acquire_wait >= 10s`).
10. archtest: `refresh` imports only `job`, `freshness`; `webhook` imports only `scm`, `job`, `httpapi`, `freshness` (after D1); `go list -deps` assertion.
11. Lifecycle integration (`lifecycle.Plan` with the real refresh stage): a failing stage, a job ignoring cancellation, stage `StopTimeout = Grace + AbandonAfter + 1 s`, total within 42 s.
12. M0 fixture task: record a **current** push, release, create, repository, installation and ping delivery from a real GitHub App/organisation webhook (headers included) into `testdata/fixtures/webhook/`; re-run `TestDecodeRecordedPush`-style tests against them; confirm `X-GitHub-Hook-Installation-Target-Type` for an App.

### 12.4 Determinism rules

Engine/planner/scheduler/receiver/redelivery tests run only inside `synctest.Test`; no `time.Sleep` outside a bubble; no `t.Parallel` inside; goroutines started by the test are joined before the bubble ends (a `close(block)` for deliberately stuck jobs); SQLite is never used in a bubble (bridges are tested outside); fixed seeds; assertions on virtual durations are exact where pace/timers decide them and ranges where seeded jitter does.

---

## 13. Implementation tasks

Ordered; each task lists files and a definition of done (DoD). Dependencies on other units are marked `[dep: unit]`. Milestones follow architecture 23 (M0 = spikes) and 22.

### M0. Spike and kernel contracts (the W22 prototype, already done here)

| # | Task | Files | DoD |
|---|---|---|---|
| T0.1 | Copy the reference prototype as the starting skeleton and re-verify it in the new repository | `internal/refresh/*`, `internal/webhook/*` (from `04-refresh-webhook.assets`; rename `webhook.go` to `receiver.go`; replace the stubs `internal/{job,scm,freshness,httpapi}` by the real packages of units 01 and 06, keeping the 1-based priorities and `Problem.Header` semantics) | `go vet`, `go test -race -timeout 5m -count=20` green in the real module with the real `job`, `freshness`, `scm`, `httpapi` packages [dep: kernel units]; `-timeout` is mandatory (a dispatcher panic hangs a bubble) |
| T0.2 | Record current webhook fixtures (section 12.3 item 12) | `testdata/fixtures/webhook/*.json` + `README` with the regeneration script | push (default branch, tag, deleted, forced, 2 commits), release, create, repository, installation, ping with headers; reference verifier decodes each |

### M1. Engine core `[dep: job, freshness]`

| # | Task | Files | DoD |
|---|---|---|---|
| T1.1 | `Backoff.Delay`, `Deps`, `Tunables`, options | `backoff.go`, `deps.go` | `TestBackoffBounds`; no overflow fuzz |
| T1.2 | Entry model, heaps, `Enqueue` (4.2) | `engine.go` | coalesce/dirty/backoff-promotion/parked/ErrQueueFull/ErrClosed tests |
| T1.3 | Dispatch with per-priority classes, pace, concurrency, delayed promotion (4.3) | `engine.go` | pace/concurrency and priority-order tests |
| T1.4 | `finish` classification, retry, exhaustion, `Gone`, cancel (4.4) | `engine.go` | retry/exhausted/gone tests |
| T1.5 | Rate-limit pause with floor growth (4.5) | `engine.go` | pause test incl. no attempt consumed, other upstream unaffected |
| T1.6 | Parking, probe, force, warm restore, `ParkedError` (4.6) | `engine.go` | park/probe/force/warm tests; hooks run outside the mutex |
| T1.7 | Per-job timeout, overrun, stuck, heartbeat, `Inspect` (4.7) | `engine.go` | stuck test; `AllStuckSince` |
| T1.8 | `Run` shutdown (4.8) | `engine.go` | grace and abandon tests |
| T1.9 | Metrics and logs of 9.1/9.3 | `metrics.go`, `engine.go` | SDK `ManualReader` test lists exactly the constants; log fields asserted |

### M2. Planner, gate, scheduler `[dep: M1]`

| # | Task | Files | DoD |
|---|---|---|---|
| T2.1 | `Gate` interface | `gate.go` | `TestGateReservesPerPriority` (with a test gate; the arithmetic table test moves to unit 05) |
| T2.2 | Planner state machine, `Tick`, `OnResult`, idle back-off, cold priority, `Restore`, `Progress` | `planner.go` | planner tests; property test: for any sequence of `Tick`/`OnResult` no item is enqueued twice while inflight |
| T2.3 | `Planner.Enqueue` (mark-before-forward) | `planner.go` | in-flight webhook test; race test |
| T2.4 | Scheduler | `scheduler.go` | scheduler test; `OnDone` outside the mutex |
| T2.5 | Budget arithmetic test with the fake GitHub (12.3 item 5) | `refresh/budget_test.go` [dep: githubtest] | numbers within architecture 13.4 |

### M3. Wiring for refresh `[dep: store, github, catalog, ops, lifecycle, bridge]`

| # | Task | Files | DoD |
|---|---|---|---|
| T3.1 | Bridges `PlanSink`, `RateBudget` (or `RateGate` if X6 goes unit 05's way), parked persistence (incl. `onUnparked(k, "gone")` deleting the row), `Tracker` (SetSubjects + Trigger), `Retrier` (enqueues through the planner) | `cmd/scp/internal/bridge/*` | bridge tests (12.3 item 6); `assert.go` compiles |
| T3.2 | `wire_jobs.go` per section 2.5; refresh lifecycle stage with `StopTimeout = Grace + AbandonAfter + 1 s` | `cmd/scp/wire_jobs.go`, `run.go` | `run()` e2e boots, first reconcile runs, planner ticks, SIGTERM shutdown within 42 s |
| T3.3 | Config types and validation of 6.1; generated reference entries | `cmd/scp/config.go` | config tests; consumption test |
| T3.4 | `/livez` and `/status` inputs from `Inspect`, `Parked`, `Progress` | `ops` bridge | `/livez` fails on a stuck upstream and a stale heartbeat |

### M4. Webhook receiver `[dep: github.Verifier, reactor, httpapi, httpx]`

| # | Task | Files | DoD |
|---|---|---|---|
| T4.1 | `github.Verifier` per 4.12 (reference code to port) | `internal/github/webhook.go` | vector, rejection matrix, decode goldens from T0.2 fixtures, fuzz |
| T4.2 | `dedupe.go` | `internal/webhook/dedupe.go` | eviction/TTL/no-refresh tests |
| T4.3 | `Receiver` pipeline and status mapping (4.13, 4.14) | `receiver.go` | all receiver tests incl. race and fuzz |
| T4.4 | `ratelimit.webhook_invalid` class: build one `*httpx.Limiter` for it and pass `limiter` and `resolver.ClientIP` to `NewReceiver` (no bridge: `_ webhook.Limiter = (*httpx.Limiter)(nil)` in `assert.go`) | `cmd/scp/wire_http.go` | 401 -> 429 test through the real `httpx.Limiter`; valid signature still 202 from the throttled address |
| T4.5 | Routes: register both patterns (`webhook`, `webhook.source`) to the one handler (a 404 problem handler when no source enables webhooks), `CrossOriginProtection` bypass for both, 26 MiB override | `routes`, `cmd/scp/wire_http.go` | `Table.Verify()` passes with and without a default source; startup refusal tests (empty secret); the enumeration test finds no webhook route on the admin mux |
| T4.6 | Config types and validation of 6.2 | `cmd/scp/config.go` | refusal tests |
| T4.7 | Metrics of 9.2 and `Stats` bridge to `/status` | `receiver.go`, `ops` bridge | registry test; `/status` shows last delivery and result counts |
| T4.8 | `TestEveryEventKindIsHandledOrCountedIgnored` | `receiver_test.go` | fails when `scm.EventKind` gains a value without a row |

### M5. Redelivery and hardening `[dep: github (JWT endpoints), catalog (resync), store]`

| # | Task | Files | DoD |
|---|---|---|---|
| T5.1 | `github.Client.FailedDeliveries/Redeliver` (App JWT, cursor pagination, one id per GUID, `Gone`/rate-limit mapping) | `internal/github/redelivery.go` | adapter tests against `githubtest` (fault injection) |
| T5.2 | `webhook.Redelivery` and `Watermarks` bridge | `redelivery.go`, `bridge` | redelivery tests; watermark round trip |
| T5.3 | `catalog.React` handles `installation_repositories`/`resync` | `catalog/react.go` | reactor test |
| T5.4 | Model-based fuzz and stress (12.3 items 2-4); archtest rows; lifecycle integration | `*_test.go`, `internal/archtest` | green in CI; `-race -count=30` nightly |
| T5.5 | Docs: operator runbook page for webhook setup (content type JSON, events, secret rotation with `previous_secret`, redelivery, what 401/503 mean) and the metric/alert suggestions of 9 | `docs/operations/webhooks.md` | reviewed; linked from `scp doctor` output |

---

## 14. Deviations proposed

Every item keeps the architecture as written in the design above; these are the places where the unit needs a change or an addition, with evidence. None blocks implementation.

| ID | Where | Proposal | Evidence |
|---|---|---|---|
| D1 | architecture 5.2, `webhook` row | **WITHDRAWN by the verification (2026-10-14).** The first draft added `freshness` to the row because a `job.Spec{Policy: freshness.Policy{...}}` literal names the type. A struct field can be assigned through its selector (`sp.Policy.TTL = ttl`) without naming `freshness.Policy`, so `webhook` stays inside its row; `e2e.TestImportsStayInsideTheMatrix` proves the import set | no change to architecture 5.2 is needed |
| D2 | architecture 8.6 | **Additive** API: `Deps`, `Tunables`, variadic `Option` on `NewPlanner` and `NewScheduler`, `WithOnUnparked`, `WithRestoreParked`, `WithTunables`, `Engine.Parked()`, `ParkedKey`, `ParkedError`, `Stats`/`UpstreamStats` shapes, `Planner.Progress`, `Planner.Enqueue`, `Task.OnDone`, `ErrUnknownKind`, `ErrAbandoned`, `ErrInvalidPriority`. Wiring: hand `*Planner` (not `*Engine`) to reactors as their `job.Enqueuer` | store has `Park/Unpark/LoadParked/SaveTask/LoadTasks`, which nothing could call without these hooks; mark-before-forward prevents the double run (test `TestPlannerWebhookRunInFlightIsNotTickedAgain`); every call in 8.6 still compiles unchanged (prototype) |
| D3 | architecture 6.2 / 8.6 | **(WITHDRAWN by CD-CF-02: the arithmetic stays in unit 05, the text below is the superseded proposal)** the **reserve arithmetic** (40/15/2%) lives in `refresh.NewReserveGate(Budget, Reserves)`; `bridge.RateGate` becomes `bridge.RateBudget` implementing `refresh.Budget` (a 10-line adapter over `github.RateState`); `refresh.Gate` keeps its exact signature | puts testable, config-backed arithmetic in one unit (`refresh.gate.reserves.*` keys need a consumer); `TestGateReservesPerPriority` |
| D4 | architecture 8.7 | `webhook` gains two consumer interfaces, `Limiter` and `Watermarks`. `Limiter` is satisfied **directly** by `*httpx.Limiter` (same signature, no bridge; the first draft planned `bridge.InvalidSigLimiter`); `Watermarks` by `bridge.Watermarks` | `webhook` may not import `httpx` or `store` (5.2); ADR-025 requires per-IP invalid-signature limiting; research 09 requires a persisted redelivery watermark; `02-platform` `httpx.Limiter.Allow(key string) bool` and `ClientIPResolver.ClientIP` exist |
| D5 | architecture 15, 18.2 | statuses 404, 408, 413, 415, 429, 500 besides 202/400/401/503; problem codes `overloaded` and `request-timeout` added (`unsupported-media-type` already exists in unit 06; 06 declares `code` an open enum); `scp_webhook_total` gains result values `ping unknown_source too_large unsupported_media throttled busy body_timeout error` | HTTP semantics (GR-16); additive (codes are open strings, label values additive) |
| D9 | architecture 8.16 point 4 | refresh stage `StopTimeout = Grace + AbandonAfter + 1 s` (19 s), not `Grace` | `Run` must be able to cancel at `Grace` and report `ErrAbandoned`; 5+15+19+1+2 = 42 s still fits 45 s |
| D10 | architecture 6.4 | the redelivery retention-gap path emits a synthetic `scm.Event{Kind: EventInstallationRepos, Action: "resync"}` through the existing sink instead of enqueuing another feature's job key | avoids a cross-feature magic string and a new kernel `EventKind`; needs `catalog.React` to treat it as a full reconcile |
| D11 | architecture 8.7 `webhook.Verifier` | the error vocabulary of the verifier is fixed as three structural behaviour interfaces (`Unauthenticated`, `Malformed`, `Ignored`) | the receiver cannot import `github`; without a vocabulary every verifier failure would be a 400 or 500 (GR-16) |
| D12 | architecture 18.3 | drop the span link `webhook.handle -> job.run` | `job.Enqueuer.Enqueue` carries no context; correlate by `delivery_id` logs |
| D13 | research 05 section 5.3 table | an `Enqueue` does not promote the `runAt` of an entry that is **in backoff** unless `force` | prevents webhook bursts from turning a failing job into a hot loop; the level-triggered retry observes the newer state anyway |
| D14 | architecture 18.2 | `scp_webhook_to_publish_seconds` is emitted by the engine (label `kind`), not by `webhook` | only the engine sees both the first event-priority enqueue and the success |

(D6 to D8 were folded into D2 and D13 while drafting; the numbering is kept so references in comments stay stable.)

### Cross-unit conflicts found by the verification (X1 to X6)

These are disagreements between this unit and a sibling unit's document that cannot be settled inside this document. Each has the default this document implements and the exact edit for the other side. None is an architecture change.

| ID | Conflict | Default here | Needs |
|---|---|---|---|
| X1 | Unit 05's `WebhookError` implements `InvalidSignature() bool`; this unit's contract (D11) is `Unauthenticated() bool`. Taken literally, a bad signature would reach the receiver's default branch and answer 500 | the receiver honours **both** spellings (`TestInvalidSignatureSpellingOfUnit05Gives401`) | **SETTLED (CD-IF-08)**: canonical `InvalidSignature() bool` (unit 05's verifier, its code and 12 tests); `Unauthenticated() bool` stays a tolerated alias until 05's implementation lands, then the alias branch and its test are removed |
| X2 | An unclassified error from `Verify`: unit 05 says 400 ("GitHub would redeliver"); this unit says 500 (GR-16: 5xx is ours). GitHub does **not** redeliver automatically (verified 2026-10-14), so 05's reason does not hold; the redelivery job redelivers at most 3 times per GUID | 500 | **SETTLED (CD-IF-08)**: 500; unit 05 4.11 text changed |
| X3 | Order of `FailedDeliveries`: this unit's first draft said newest first (truncation then kept the newest and let the oldest age out of the 3-day window); unit 05 says oldest first | oldest first (this document now says so) | none |
| X4 | `FailedDeliveries` has no way to say "I stopped at `limits.max_pages`" (05): the watermark would advance over failures that were never listed | adapter returns its ids **and** an error with `Truncated() bool`; `Redelivery` honours it | **SETTLED (CD-IF-08)**: unit 05 text changed; implementation in `redeliver.go` |
| X5 | `Redeliver` maps 422 to nil ("already redelivered", 05); GitHub documents 422 as "Validation failed, or the endpoint has been spammed" | 422 is `RateLimited` (10 min) unless the body proves otherwise | **SETTLED (CD-IF-08), the other way**: unit 05's evidence-based `Permanent/status_422` wins; the job counts that id `failed`, continues, and stops after three consecutive failures (no rate-limit pause) |
| X6 | Reserve arithmetic and its config keys exist twice: `refresh.NewReserveGate` + `refresh.gate.reserves.*` (this unit, D3) and `Client.Admit(ReserveClass)` + `github.defaults.budget.reserve` (05, matching architecture 6.2/8.6 and ADR-015). Two gates would double-count and one key set would be unconsumed (GR-12, GR-04) | this document as written | **owner decision before implementation**; the edit for 05's path is in section 6.2 |

---

## 15. Open questions

| # | Question | Default taken in this design | Who decides |
|---|---|---|---|
| Q1 | One engine upstream `github` for all sources means a rate limit on one installation pauses every source. Per-installation isolation needs the upstream (or a pause key) to be derivable from the job key, e.g. `Spec.UpstreamFor func(job.Key) string` | single upstream, global pause (conservative; correct for the common one-App case) | owner, before multi-source deployments are supported |
| Q2 | Should reactors pass `force=true` on `push`/`head_changed` so a new commit un-parks a content-bound failure (a parse error tied to a blob SHA)? | yes for those two events, `false` otherwise; the 24 h probe is the backstop | feature owners (`docs`, `manifests`) |
| Q3 | Does `catalog.React` accept the `resync` event, and does it also use `installation_repositories.removed` to drop the planner subjects early? | yes, full reconcile of `ev.Source` | `catalog` design |
| Q4 | `X-GitHub-Hook-Installation-Target-Type` value for an App webhook is `[K]` (unverified); the App target id check needs it | enforce only `Target-ID == app_id` until a recorded fixture confirms the type | `github` design, M0 fixture task T0.2 |
| Q5 | The per-commit `added/removed/modified` and `tree_id` fields are still undocumented on GitHub's events page (re-fetched 2026-10-14); only 2018 recordings show them | treat absent arrays as `PathsComplete=false`; the head sweep is the backstop | settled by T0.2 |
| Q6 | A key's `Observe` is called for every attempt including suppressed enqueues of parked keys: at 3,000 parked keys and a 15 s tick that is 200 calls/s of cheap work. Acceptable? | yes (planner stops ticking parked items after the first suppression) | measure in T5.4 |
| Q7 | `scp_webhook_to_publish_seconds` includes `head_changed` enqueues. Is a label for the origin worth widening `job.Enqueuer`? | no, keep the kernel interface minimal | owner |
| Q8 | GHES retains deliveries 7 days (documented in research 09, GHES page not re-fetched here): should `redelivery.max_age` default per host kind? | 72 h everywhere, configurable | `github` design |
| Q9 | Should v1 offer an optional allow-list of GitHub's webhook source IPs (from `GET /meta` `hooks`, refreshed periodically) as defence in depth? | no; documented as a proxy-level control | owner |
| Q10 | `github.defaults.budget.reserve.backfill = 0.40` (05; was `refresh.gate.reserves.backfill`) means a PAT (5,000/h) stops cold start at 60% consumption (3,000 calls) and finishes after the hourly reset. Acceptable, or should `readiness.core_kinds` jobs run at `PrioScheduled` on cold start? | acceptable; `/readyz` has the 10-minute deadline escape; PAT is the documented fallback credential | owner (affects the PAT experience only) |
| Q11 | Event ordering: deliveries can arrive out of order or very late (research 09: p99 delays of an hour reported). The design is order-agnostic because every job is level-triggered and idempotent. Confirm no reactor stores event fields (e.g. `After` SHA) as state | none stored | reviewers of reactors |
| Q12 | `scp_item_age_seconds{kind}` (architecture 18.2, listed under the engine; unit 09 assumes the engine contributes it) needs `fetched_at`, which the engine and planner do not hold | `ops` derives it from `freshness.Info.FetchedAt` in the snapshot (exact, includes 304s); this unit emits nothing and section 9.1 says so | `ops` design, unit 09 to stop assuming the engine |
| Q13 | Does a timed-out webhook delivery (no HTTP status) appear in `GET /app/hook/deliveries?status=failure`? The documented ranges are 200-399 for `success` and 400-599 for `failure` | the adapter does not rely on the filter alone (4.15 contract item 2) | M0 task T0.2: provoke a timeout against a test App and record the list entry |
| Q14 | Should the first `webhook.redelivery` run be delayed after start (thundering with the first reconcile)? | no: it is a JWT call outside the installation budget; delay with `planner.Restore` if wanted (2.5) | owner |

---

## Appendix A. Compile-checked public API (output of `go doc`, comments trimmed)

Generated from the prototype module on 2026-10-14 with Go 1.27.1 and re-checked after the verification fixes (`go doc` of both packages: the only API deltas against the first draft are `ErrInvalidPriority`, `Config.BodyTimeout` and `ResBodyTimeout`; 1-based kernel priorities are in the stub `job` package).

```go
package refresh // import "github.com/cantara/source-code-portal/internal/refresh"

const ( // names asserted by the metrics registry test
	MetricSyncPending = "scp_sync_pending"; MetricSyncRunning = "scp_sync_running"; MetricSyncParked = "scp_sync_parked"
	MetricUpstreamPaused = "scp_upstream_paused_seconds"; MetricSyncJobs = "scp_sync_jobs_total"
	MetricJobDuration = "scp_job_duration_seconds"; MetricJobWait = "scp_job_wait_seconds"
	MetricJobOverrun = "scp_job_overrun_total"; MetricJobIgnoredCancel = "scp_job_ignored_cancel_total"
	MetricGateDenied = "scp_gate_denied_total"; MetricToPublish = "scp_webhook_to_publish_seconds"
	MetricPlannerTick = "scp_planner_tick_seconds"; MetricPlannerEnqueue = "scp_planner_enqueue_total"
	MetricPlannerSubjects = "scp_planner_subjects"; MetricTaskRuns = "scp_task_runs_total"; MetricTaskOverruns = "scp_task_overruns_total"
)
var ErrUnknownKind, ErrUnknownUpstream, ErrInvalidPriority, ErrAbandoned error

type Backoff struct{ Base, Cap time.Duration; MaxAttempts int }
func (b Backoff) Delay(attempt int, r *rand.Rand) time.Duration
type Budget interface{ Remaining(upstream string) (s BudgetState, ok bool) }
type BudgetState struct{ Fraction float64; Reset time.Time }
type Deps struct{ Log *slog.Logger; Meter metric.Meter; Tracer trace.Tracer }
type Engine struct{ /* unexported */ }
func New(b Backoff, seed uint64, opts ...Option) *Engine
func (e *Engine) AddUpstream(name string, cfg UpstreamConfig)
func (e *Engine) Enqueue(k job.Key, p job.Priority, notBefore time.Time, force bool) error
func (e *Engine) Inspect() Stats
func (e *Engine) Observe(f func(k job.Key, o job.Outcome, err error))
func (e *Engine) Parked() []ParkedKey
func (e *Engine) Register(s job.Spec)
func (e *Engine) Run(ctx context.Context) error
type Gate interface{ Admit(upstream string, p job.Priority) (wait time.Duration) }
type Option func(*options)
func WithDeps(d Deps) Option; func WithGate(g Gate) Option; func WithGrace(d time.Duration) Option; func WithMaxKeys(n int) Option
func WithOnExhausted(f func(k job.Key, attempts int, err error)) Option; func WithOnParked(f func(ParkedKey)) Option
func WithOnUnparked(f func(k job.Key, reason string)) Option; func WithRestoreParked(p []ParkedKey) Option; func WithTunables(t Tunables) Option
type ParkedError struct{ ParkedKey }               // Error() string; Permanent() bool
type ParkedKey struct{ Key job.Key; Since, RetryAt time.Time; Reason string }
type PlanSink interface{ SaveState(k job.Key, st PlanState) }
type PlanState struct{ NextDue time.Time; UnchangedStreak int }
type Planner struct{ /* unexported */ }
func NewPlanner(enq job.Enqueuer, sink PlanSink, seed uint64, opts ...Option) *Planner
func (p *Planner) Enqueue(k job.Key, pr job.Priority, notBefore time.Time, force bool) error
func (p *Planner) OnResult(k job.Key, o job.Outcome, err error)
func (p *Planner) Progress(kind string, now time.Time) Progress
func (p *Planner) Restore(states map[job.Key]PlanState)
func (p *Planner) SetSubjects(kind string, keys []job.Key)
func (p *Planner) Tick(now time.Time)
func (p *Planner) Track(s job.Spec)
type Progress struct{ Subjects, Completed, Inflight, Parked, Due int }
type Scheduler struct{ /* unexported */ }
func NewScheduler(seed uint64, opts ...Option) *Scheduler
func (s *Scheduler) Add(t Task); func (s *Scheduler) Run(ctx context.Context) error
func (s *Scheduler) Status(name string) (TaskStatus, bool); func (s *Scheduler) Trigger(name string) bool
type Stats struct{ At, Heartbeat time.Time; Keys, Parked int; Upstreams map[string]UpstreamStats; Jobs map[string]uint64 }
type Task struct{ Name string; Every time.Duration; Jitter float64; InitialDelay func(rnd *rand.Rand) time.Duration
	Run func(ctx context.Context) error; Timeout time.Duration; OnDone func(name string, st TaskStatus) }
type TaskStatus struct{ LastAttempt, LastSuccess time.Time; LastErr string; Runs, Failures, Skipped, Overruns uint64 }
type Tunables struct{ Grace, AbandonAfter time.Duration; MaxKeys int; ParkRetry, StuckAfter, GateRecheck, Heartbeat, MaxPause, RateLimitMin, RateLimitMax time.Duration }
func DefaultTunables() Tunables
type UpstreamConfig struct{ MaxConcurrent int; Pace, JobTimeout time.Duration }
type UpstreamStats struct{ Ready, Delayed, Running, Stuck, MaxConcurrent int; PausedUntil, AllStuckSince time.Time; RateLimitStreak int }
```

```go
package webhook // import "github.com/cantara/source-code-portal/internal/webhook"

const RedeliveryKind = "webhook.redelivery"
const ( // result labels of scp_webhook_total
	ResAccepted = "accepted"; ResDuplicate = "duplicate"; ResInvalidSig = "invalid_signature"; ResMalformed = "malformed"
	ResIgnored = "ignored"; ResPing = "ping"; ResQueueFull = "queue_full"; ResUnknownSource = "unknown_source"; ResTooLarge = "too_large"
	ResUnsupportedType = "unsupported_media"; ResThrottled = "throttled"; ResBusy = "busy"; ResBodyTimeout = "body_timeout"; ResError = "error"
)
const ( MetricWebhookTotal = "scp_webhook_total"; MetricWebhookBodyBytes = "scp_webhook_body_bytes"; MetricWebhookDedupeSize = "scp_webhook_dedupe_entries"
	MetricWebhookLastAccept = "scp_webhook_last_accepted_timestamp_seconds"; MetricRedelivery = "scp_webhook_redelivery_total" )
func NewReceiver(cfg Config, srcs Sources, v Verifier, sink Sink, lim Limiter, clientIP func(*http.Request) string, d Deps) (*Receiver, error)
type Config struct{ MaxBody int64; DedupeTTL time.Duration; DedupeMax, MaxConcurrent int; AcquireWait, RetryAfter, BodyTimeout time.Duration } // yaml tags in 3.4
func DefaultConfig() Config
type Deps struct{ Log *slog.Logger; Meter metric.Meter; Tracer trace.Tracer }
type Limiter interface{ Allow(key string) bool }
type Receiver struct{ /* unexported */ }
func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request); func (rc *Receiver) Stats() Stats
type Redeliverer interface{ FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error)
	Redeliver(ctx context.Context, src scm.SourceID, deliveryID int64) error }
type Redelivery struct{ /* unexported */ }
func NewRedelivery(srcs []RedeliverySource, rd Redeliverer, sink Sink, wm Watermarks, d Deps) *Redelivery
func (r *Redelivery) Jobs() []job.Spec; func (r *Redelivery) Run(ctx context.Context, _ job.Key) (job.Outcome, error)
type RedeliveryConfig struct{ Enabled bool; Interval time.Duration; MaxPerRun int; Rate, MaxAge time.Duration } // yaml tags in 3.4
func DefaultRedelivery() RedeliveryConfig
type RedeliverySource struct{ ID scm.SourceID; Cfg RedeliveryConfig }
type Sink interface{ Handle(ctx context.Context, ev scm.Event) error }
type Sources struct{ IDs []scm.SourceID; Default scm.SourceID }
type Stats struct{ LastAccepted time.Time; Results map[string]uint64; DedupeSize int }
type Verifier interface{ Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error) }
type Watermarks interface{ Load(ctx context.Context, name string) (time.Time, error); Save(ctx context.Context, name string, t time.Time) error }
```

## Appendix B. External facts re-verified on 2026-10-14 (and what stays unverified)

| Fact | Source | Result |
|---|---|---|
| `X-Hub-Signature-256` is `sha256=` + HMAC hex over the payload; never compare with `==`; handle payload as UTF-8; header not sent without a secret; test vector (`It's a Secret to Everybody` / `Hello, World!` -> `757107ea...3e17`) | https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries | confirmed; the vector is a test |
| respond 2xx within 10 s; process asynchronously; a redelivery reuses `X-GitHub-Delivery` | https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks | confirmed |
| GitHub does not automatically redeliver failed deliveries; a failed delivery has `status` not `OK` | https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries | confirmed; the "same GUID on redelivery" and retention are not stated on that page (confirmed on the best-practices page and the next row) |
| deliveries from the past **3 days** can be redelivered | https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks | confirmed |
| App endpoints (JWT): `GET /app/hook/deliveries` (`per_page` <= 100, `cursor`, `status=success|failure`), `GET /app/hook/deliveries/{id}`, `POST /app/hook/deliveries/{id}/attempts` -> 202; fields `id, guid, delivered_at, redelivery, status, status_code, event, action, installation_id, repository_id, duration, throttled_at` | https://docs.github.com/en/rest/apps/webhooks | confirmed |
| push `commits` holds at most 2,048 entries; payload cap 25 MB; no event for > 5,000 branches or > 3 tags pushed at once; headers `X-GitHub-Event`, `X-GitHub-Delivery`, `X-GitHub-Hook-ID`, `X-Hub-Signature-256`, `User-Agent: GitHub-Hookshot/...` | https://docs.github.com/en/webhooks/webhook-events-and-payloads | confirmed; per-commit `added/removed/modified` and `tree_id` **not documented** there (Q5) |
| OpenTelemetry API modules `otel/metric`, `otel/trace`, `otel/sdk/metric` v1.47.0 resolve and build with Go 1.27.1 | `go get` in the scratch module | confirmed |
| `testing/synctest.Test` bubbles with a fake clock, `httptest.NewTestServer` exists, `http.MaxBytesError` | `go doc` with Go 1.27.1 | confirmed |

Re-verified by the independent verifier on 2026-10-14 (WebFetch of docs.github.com): `GET /app/hook/deliveries` parameters `per_page` (max 100, default 30), `cursor`, `status` (`success` = 200-399, `failure` = 400-599) and the field list including `throttled_at`; `POST /app/hook/deliveries/{id}/attempts` needs a JWT and answers 202, 400 or 422 ("Validation failed, or the endpoint has been spammed"); the delivery headers list includes `X-Hub-Signature` (SHA-1, never read) and `X-GitHub-Hook-Installation-Target-Type` and `-ID`; push `commits` "includes a maximum of 2048 commits", payloads "capped at 25 MB", no event above 5,000 branches or more than three tags; the push parameter table lists no `added`/`removed`/`modified`/`tree_id` (Q5 stands); "deliveries that occurred in the past 3 days" can be redelivered; the signature test vector and the `X-Hub-Signature-256` header; otel `metric`/`trace`/`sdk/metric` v1.47.0 is the latest on the proxy; `go doc` confirms `http.ResponseController.SetReadDeadline`, `httptest.NewTestServer` and `testing/synctest`. Not found on the redelivery page: whether a redelivery keeps the GUID (the best-practices page says so, as cited above).

Unverified and carried: whether a timed-out delivery appears under `status=failure` (Q13); the `Target-Type` header value of an App webhook (Q4); GHES webhook delivery retention (Q8); the real-world rate of out-of-order or delayed deliveries (research 09 reports p99 delays and a lost-records incident on 2026-06/07, not re-verified); GraphQL-related budget numbers belong to other units.

## Verification log

Independent verification on 2026-10-14 (verifier is not the author). Scratch module compile-checked with Go 1.27.1; final run `go vet ./...` clean and `go test -race -timeout 5m -count=3 ./...` green (62 tests, 2 fuzz targets). Full findings: `30-verification/design-04-refresh-webhook.md`. Original preserved by the verifier outside the repository.

### Fixes applied to this document and the assets

| # | Severity | Fix |
|---|----------|-----|
| V1 | high | Priorities and scopes aligned to the unit 01 kernel (1-based, zero is invalid): `ErrInvalidPriority`, `classOf`/`prioOf`, planner `Track` panics on invalid scope; stub `job` follows the kernel. |
| V2 | high | One rate-limit event with many in-flight jobs escalated the exponential floor once per job. Streak now increments once per event (`startedAt` vs `pauseSetAt`); reset only for work started after the pause ended. |
| V3 | high | Gate was bypassed for promoted delayed entries and for classes nobody asked about. `gateWaits` asks every class with ready or delayed work; pick loop defers unasked classes. |
| V4 | high | A panicking `After` hook or scheduler `OnDone` could kill the engine goroutine; both recovered and logged. |
| V5 | high | Permanent-plus-rate-limited error parked the job but skipped the pause. The pause is imposed first for any `AsRateLimited` error; classification precedence follows `job.Classify`. |
| V6 | high | Slow-body (slowloris) deliveries held a body slot indefinitely. `BodyTimeout` (default 8 s) with `http.ResponseController.SetReadDeadline`, 408 `request-timeout`. |
| V7 | high | Delivery id from a verified sender was stored and echoed unvalidated (log and response injection, unbounded memory). `validDelivery` check (400), `safeID` for echo, `json.Marshal` for the response. |
| V8 | medium | `Retry-After` was emitted through a field that does not exist in unit 06's `Problem`; now via `Problem.Header` allow-list. |
| V9 | medium | Webhook imported `freshness` (violates matrix 5.2); Spec built via `sp.Policy.TTL`; import test `TestImportsStayInsideTheMatrix` added. |
| V10 | medium | De-dup was skipped for an empty delivery id; now refused as invalid. |
| V11 | medium | Redelivery adapter truncation lost the watermark; `Truncated()` handling and test added. Spec timeout corrected to `per*rate + 2 min`. |
| V12 | medium | First global job via the planner could be run twice; mark-in-flight before forwarding. |
| V13 | medium | `WithTunables` zeroed unset fields; now merges over defaults. |
| V14 | medium | Metric priority label uses names, not numbers; `scp_item_age_seconds` documented as not emitted here. |
| V15 | medium | Reference decoder: other events (release, create, repository, installation) decoded; forced or commit-less pushes mark `PathsComplete=false`. |
| V16 | low | Wiring section 2.5 ordering (planner, fan, receiver, then `Validate`/`Register`/`Track`), `ErrAbandoned` exit, no planner tick during grace. |
| V17 | low | Section 14 deviations D1/D4/D5 revised, X1-X6 cross-unit table added, Q12-Q14 open questions added, Appendix A and B refreshed. |

Mutation check: seven regression mutations (priority by value, gate bypass, streak per job, hook recover removed, pause-after-switch, body timeout removed, delivery-id validation removed) each fail at least one test.

### Unresolved (see report)

X2 (400 vs 500 for unclassified `Verify` error), X4/X5 (unit 05 must implement `Truncated()` and the 422 mapping), X6 (duplicate reserve gate and config keys between units 04 and 05), Q12 (item-age metric owner), Q13 (timed-out deliveries vs status filter, M0 spike), unverified Target-Type header value, GHES retention, hand-written installation/repository fixtures awaiting real recordings (T0.2).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: reserve gate (X6) is owned by the adapter, so `refresh.Gate` has no `Budget`, `Reserves` or `NewReserveGate` (CD-CF-02; code and tests).
- Changed: redelivery keys `max_per_run` 50, `rate` 1 s, `max_age` 72 h (1 h to 168 h) as unit 05 declares (CD-CF-03); webhook admission keys and `webhook.enabled/default` live in unit 05 structs (CD-CF-04).
- Changed: webhook verifier vocabulary `InvalidSignature` (alias `Unauthenticated` tolerated), `Truncated` for FailedDeliveries; 422 and 400 redeliveries are Permanent `status_<n>`; queue-full problem code `queue-full` (CD-IF-08, CD-ER-03).
- Changed: `scp_item_age_seconds` is emitted by `ops` (CD-MT-02).
- Owner: Q1 global pause per upstream in v1 (A-30 note; 05 Q-10).
