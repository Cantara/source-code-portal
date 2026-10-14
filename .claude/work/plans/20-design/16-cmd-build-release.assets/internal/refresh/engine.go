package refresh

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const numPrio = 4 // job.PrioInteractive .. job.PrioBackfill

// classOf maps a priority to its ready-heap index. Priorities are 1-based in the kernel (unit 01), so the
// numeric value must never be used as an index.
func classOf(p job.Priority) int { return int(p) - int(job.PrioInteractive) }

// prioOf is the inverse of classOf.
func prioOf(c int) job.Priority { return job.PrioInteractive + job.Priority(c) }

func prioName(p job.Priority) string {
	switch p {
	case job.PrioInteractive:
		return "interactive"
	case job.PrioWebhook:
		return "webhook"
	case job.PrioScheduled:
		return "scheduled"
	case job.PrioBackfill:
		return "backfill"
	}
	return "invalid"
}

var (
	// ErrUnknownKind: Enqueue for a key whose kind has no registered job.Spec (a wiring bug; not a job sentinel).
	ErrUnknownKind = errors.New("refresh: no job registered for kind")
	// ErrUnknownUpstream: a registered Spec names an upstream that was not added.
	ErrUnknownUpstream = errors.New("refresh: unknown upstream")
	// ErrInvalidPriority: Enqueue with a priority below job.PrioInteractive (the unset zero value; a wiring bug).
	ErrInvalidPriority = errors.New("refresh: invalid priority")
	// ErrAbandoned is returned by Run when jobs did not return after Grace + cancel + AbandonAfter.
	ErrAbandoned = errors.New("refresh: jobs abandoned at shutdown")
)

// ParkedKey is a key whose job failed permanently (or panicked).
type ParkedKey struct {
	Key     job.Key
	Since   time.Time
	RetryAt time.Time // one probe attempt at this time
	Reason  string
}

// ParkedError is passed to the observer when an Enqueue is suppressed because the key is parked, so
// the planner learns the key is parked even after a warm start. It satisfies job.Permanent.
type ParkedError struct{ ParkedKey }

func (e *ParkedError) Error() string   { return "parked: " + e.Reason }
func (e *ParkedError) Permanent() bool { return true }

// panicError wraps a recovered panic; it is permanent.
type panicError struct{ v any }

func (e *panicError) Error() string   { return fmt.Sprintf("panic: %v", e.v) }
func (e *panicError) Permanent() bool { return true }

type UpstreamConfig struct {
	MaxConcurrent int           // github default 4
	Pace          time.Duration // minimum spacing between job starts; github default 100ms
	JobTimeout    time.Duration // default per-run deadline when Spec.Timeout is 0; default 2m
}

type state uint8

const (
	stDelayed state = iota
	stReady
	stRunning
)

type entry struct {
	key       job.Key
	spec      *job.Spec
	up        *upstream
	prio      job.Priority
	st        state
	runAt     time.Time
	attempts  int
	inBackoff bool // runAt came from a retry backoff: an Enqueue does not promote it unless forced
	dirty     bool
	probe     bool // single retry_at probe of a parked key
	seq       uint64
	idx       int
	startedAt time.Time
	deadline  time.Time
	stuck     bool
	eventAt   time.Time // first event-priority enqueue since the last success (webhook-to-publish)
	dirtyAt   time.Time
}

type readyHeap []*entry

func (h readyHeap) Len() int           { return len(h) }
func (h readyHeap) Less(i, j int) bool { return h[i].seq < h[j].seq }
func (h readyHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].idx, h[j].idx = i, j }
func (h *readyHeap) Push(x any)        { e := x.(*entry); e.idx = len(*h); *h = append(*h, e) }
func (h *readyHeap) Pop() any          { o := *h; n := len(o); e := o[n-1]; *h = o[:n-1]; return e }

type delayedHeap []*entry

func (h delayedHeap) Len() int           { return len(h) }
func (h delayedHeap) Less(i, j int) bool { return h[i].runAt.Before(h[j].runAt) }
func (h delayedHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].idx, h[j].idx = i, j }
func (h *delayedHeap) Push(x any)        { e := x.(*entry); e.idx = len(*h); *h = append(*h, e) }
func (h *delayedHeap) Pop() any          { o := *h; n := len(o); e := o[n-1]; *h = o[:n-1]; return e }

type upstream struct {
	name       string
	cfg        UpstreamConfig
	ready      [numPrio]readyHeap
	delayed    delayedHeap
	running    map[*entry]struct{}
	stuck      int
	allStuckAt time.Time
	pausedTill time.Time
	nextStart  time.Time
	rlStreak   int       // consecutive rate-limit EVENTS without a success (see pauseSetAt)
	pauseSetAt time.Time // when pausedTill was last raised: results of jobs started before it belong to the same event
}

func (u *upstream) pending() (ready, delayed int) {
	for i := range u.ready {
		ready += len(u.ready[i])
	}
	return ready, len(u.delayed)
}

type UpstreamStats struct {
	Ready, Delayed, Running, Stuck, MaxConcurrent int
	PausedUntil                                   time.Time // zero when not paused
	AllStuckSince                                 time.Time // zero unless every slot holds a stuck job
	RateLimitStreak                               int
}

type Stats struct {
	At        time.Time
	Heartbeat time.Time // last dispatcher pass; /livez requires it fresh
	Keys      int       // keyed table size (queued + running)
	Parked    int
	Upstreams map[string]UpstreamStats
	Jobs      map[string]uint64 // cumulative by outcome (same labels as scp_sync_jobs_total)
}

// Engine is the keyed coalescing queue with per-upstream dispatch. See the design document, section 4.
type Engine struct {
	o       options
	backoff Backoff
	rnd     *rand.Rand
	m       *metrics
	log     *slog.Logger

	mu        sync.Mutex
	specs     map[string]*job.Spec
	ups       map[string]*upstream
	upOrder   []*upstream
	byKey     map[job.Key]*entry
	parked    map[job.Key]ParkedKey
	nextProbe time.Time
	seq       uint64
	heartbeat time.Time
	counts    map[string]uint64
	observe   func(k job.Key, o job.Outcome, err error)
	started   bool
	closed    bool
	wake      chan struct{}
}

func New(b Backoff, seed uint64, opts ...Option) *Engine {
	if b.Base <= 0 {
		b.Base = 2 * time.Second
	}
	if b.Cap <= 0 {
		b.Cap = 15 * time.Minute
	}
	if b.MaxAttempts <= 0 {
		b.MaxAttempts = 8
	}
	o := buildOptions(opts)
	e := &Engine{
		o: o, backoff: b, rnd: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		log: o.deps.Log, specs: map[string]*job.Spec{}, ups: map[string]*upstream{},
		byKey: map[job.Key]*entry{}, parked: map[job.Key]ParkedKey{}, counts: map[string]uint64{},
		wake: make(chan struct{}, 1),
	}
	for _, p := range o.restoreParked {
		e.parked[p.Key] = p
	}
	e.recomputeProbe()
	e.m = newMetrics(o.deps.Meter, e)
	return e
}

func (e *Engine) AddUpstream(name string, cfg UpstreamConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		panic("refresh: AddUpstream after Run")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 2 * time.Minute
	}
	u := &upstream{name: name, cfg: cfg, running: map[*entry]struct{}{}}
	e.ups[name] = u
	e.upOrder = append(e.upOrder, u)
}

func (e *Engine) Register(s job.Spec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		panic("refresh: Register after Run")
	}
	if s.Run == nil || s.Kind == "" {
		panic("refresh: Register: Kind and Run are required")
	}
	if e.ups[s.Upstream] == nil {
		panic("refresh: Register: upstream " + s.Upstream + " was not added (AddUpstream first)")
	}
	e.specs[s.Kind] = &s
}

// Observe installs the single result observer (the planner's OnResult). Once, before Run.
func (e *Engine) Observe(f func(k job.Key, o job.Outcome, err error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.observe != nil {
		panic("refresh: Observe called twice or after Run")
	}
	e.observe = f
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// after collects hooks inside a state transition; they run after the mutex is released.
type after []func()

// run executes the collected hooks. A hook that panics (a store write in a bridge, the planner) must not take
// the process down: the panic is logged at ERROR and the remaining hooks still run.
func (a after) run(log *slog.Logger) {
	for _, f := range a {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.LogAttrs(context.Background(), slog.LevelError, "refresh hook panicked",
						slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
				}
			}()
			f()
		}()
	}
}

func (e *Engine) recomputeProbe() {
	e.nextProbe = time.Time{}
	for _, p := range e.parked {
		if e.nextProbe.IsZero() || p.RetryAt.Before(e.nextProbe) {
			e.nextProbe = p.RetryAt
		}
	}
}

func (e *Engine) count(outcome, kind string) {
	e.counts[outcome]++
	e.m.jobs.Add(context.Background(), 1, kindOutcome(kind, outcome))
}

// Enqueue never blocks. See design section 4.2 for the state-by-event table.
func (e *Engine) Enqueue(k job.Key, p job.Priority, notBefore time.Time, force bool) error {
	var post after
	e.mu.Lock()
	err := e.enqueueLocked(k, p, notBefore, force, &post)
	e.mu.Unlock()
	post.run(e.log)
	return err
}

func (e *Engine) enqueueLocked(k job.Key, p job.Priority, notBefore time.Time, force bool, post *after) error {
	if e.closed {
		return job.ErrClosed
	}
	spec := e.specs[k.Kind()]
	if spec == nil {
		return fmt.Errorf("%w: %q", ErrUnknownKind, k.Kind())
	}
	up := e.ups[spec.Upstream]
	if up == nil {
		return fmt.Errorf("%w: %q", ErrUnknownUpstream, spec.Upstream)
	}
	if p < job.PrioInteractive {
		return fmt.Errorf("%w: %d", ErrInvalidPriority, p)
	}
	if p > job.PrioBackfill {
		p = job.PrioBackfill
	}
	if pk, isParked := e.parked[k]; isParked {
		if !force {
			e.count("parked_suppressed", spec.Kind)
			if obs := e.observe; obs != nil {
				perr := &ParkedError{pk}
				*post = append(*post, func() { obs(k, job.Outcome{}, perr) })
			}
			return nil
		}
		delete(e.parked, k)
		e.recomputeProbe()
		if f := e.o.onUnparked; f != nil {
			*post = append(*post, func() { f(k, "forced") })
		}
	}
	now := time.Now()
	if notBefore.Before(now) {
		notBefore = now
	}
	if cur := e.byKey[k]; cur != nil {
		if p <= job.PrioWebhook && cur.eventAt.IsZero() && cur.st != stRunning {
			cur.eventAt = now
		}
		switch cur.st {
		case stRunning:
			cur.dirty = true
			if p <= job.PrioWebhook && cur.dirtyAt.IsZero() {
				cur.dirtyAt = now
			}
			if p < cur.prio {
				cur.prio = p
			}
			e.count("marked_dirty", spec.Kind)
		default:
			e.count("coalesced", spec.Kind)
			if p < cur.prio {
				cur.prio = p
			}
			if notBefore.Before(cur.runAt) && (!cur.inBackoff || force) {
				cur.runAt = notBefore
				cur.inBackoff = false
			}
			e.place(cur, now)
		}
		e.poke()
		return nil
	}
	if len(e.byKey) >= e.o.t.MaxKeys {
		e.count("queue_full", spec.Kind)
		return job.ErrQueueFull
	}
	e.seq++
	ent := &entry{key: k, spec: spec, up: up, prio: p, runAt: notBefore, seq: e.seq, idx: -1}
	if p <= job.PrioWebhook {
		ent.eventAt = now
	}
	e.byKey[k] = ent
	e.place(ent, now)
	e.poke()
	return nil
}

// remove takes ent out of whichever heap holds it. mu held.
func (e *Engine) remove(ent *entry) {
	if ent.idx < 0 {
		return
	}
	switch ent.st {
	case stDelayed:
		heap.Remove(&ent.up.delayed, ent.idx)
	case stReady:
		heap.Remove(&ent.up.ready[classOf(ent.prio)], ent.idx)
	}
	ent.idx = -1
}

// place puts ent into the delayed heap or its priority class according to runAt. mu held.
// The class an entry sits in is derived from ent.prio, so callers that change prio must remove first.
func (e *Engine) place(ent *entry, now time.Time) {
	if ent.st != stRunning {
		// find and remove from the old location: the class may differ if prio was raised
		e.removeAny(ent)
	}
	if ent.runAt.After(now) {
		ent.st = stDelayed
		heap.Push(&ent.up.delayed, ent)
	} else {
		ent.st = stReady
		heap.Push(&ent.up.ready[classOf(ent.prio)], ent)
	}
}

// removeAny removes ent from every possible location (cheap: it is in at most one).
func (e *Engine) removeAny(ent *entry) {
	if ent.idx < 0 {
		return
	}
	u := ent.up
	if ent.st == stDelayed && ent.idx < len(u.delayed) && u.delayed[ent.idx] == ent {
		heap.Remove(&u.delayed, ent.idx)
		ent.idx = -1
		return
	}
	for c := range u.ready {
		if ent.idx < len(u.ready[c]) && u.ready[c][ent.idx] == ent {
			heap.Remove(&u.ready[c], ent.idx)
			ent.idx = -1
			return
		}
	}
	ent.idx = -1
}

// Run is the dispatcher. It returns after ctx is cancelled and every in-flight job returned (or was
// abandoned after Grace + cancel + AbandonAfter, in which case it returns ErrAbandoned).
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		panic("refresh: Run called twice")
	}
	e.started = true
	e.mu.Unlock()

	var wg sync.WaitGroup
	hctx, hcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer hcancel()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		next := e.dispatch(ctx, hctx, &wg)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(max(0, time.Until(next)))
		select {
		case <-ctx.Done():
			e.mu.Lock()
			e.closed = true
			e.mu.Unlock()
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			// Wait for Grace, cancel the jobs, then give them AbandonAfter more.
			select {
			case <-done:
				return nil
			case <-time.After(e.o.t.Grace):
			}
			hcancel()
			select {
			case <-done:
				return nil
			case <-time.After(e.o.t.AbandonAfter):
				return ErrAbandoned
			}
		case <-e.wake:
		case <-timer.C:
		}
	}
}

// gateWaits asks the Gate (outside any lock) about every priority class of every unpaused upstream that has
// ready OR delayed work (a delayed entry may be promoted during the same pass and must not bypass the gate).
// The result records every answer, 0 included: a (upstream, class) pair that is absent was not asked, and the
// dispatcher defers it to the next pass instead of admitting it unasked.
func (e *Engine) gateWaits(now time.Time) map[gateKey]time.Duration {
	g := e.o.gate
	if g == nil {
		return nil
	}
	var keys []gateKey
	e.mu.Lock()
	for _, u := range e.upOrder {
		ready, delayed := u.pending()
		if (ready == 0 && delayed == 0) || !now.After(u.pausedTill) {
			continue
		}
		for c := range u.ready {
			keys = append(keys, gateKey{u.name, prioOf(c)})
		}
	}
	e.mu.Unlock()
	out := make(map[gateKey]time.Duration, len(keys))
	for _, k := range keys {
		w := g.Admit(k.up, k.p)
		out[k] = max(w, 0)
		if w > 0 {
			e.m.gateDenied.Add(context.Background(), 1, upPrio(k.up, k.p))
		}
	}
	return out
}

type gateKey struct {
	up string
	p  job.Priority
}

// dispatch starts every job that may start now and returns the next instant it must run again.
func (e *Engine) dispatch(ctx, hctx context.Context, wg *sync.WaitGroup) (next time.Time) {
	now := time.Now()
	waits := e.gateWaits(now)

	var post after
	e.mu.Lock()
	now = time.Now()
	e.heartbeat = now
	setNext := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	setNext(now.Add(e.o.t.Heartbeat))
	if ctx.Err() != nil {
		e.mu.Unlock()
		return time.Time{}
	}
	// parked probes
	if !e.nextProbe.IsZero() {
		if !e.nextProbe.After(now) {
			for k, p := range e.parked {
				if p.RetryAt.After(now) {
					continue
				}
				spec := e.specs[k.Kind()]
				if spec == nil || e.ups[spec.Upstream] == nil {
					continue
				}
				// A probe is a single attempt: keep the key parked until the attempt decides.
				if _, busy := e.byKey[k]; busy {
					continue
				}
				e.seq++
				ent := &entry{key: k, spec: spec, up: e.ups[spec.Upstream], prio: spec.Prio, runAt: now, seq: e.seq, idx: -1, probe: true}
				e.byKey[k] = ent
				e.place(ent, now)
			}
			// push the retry_at of probing keys forward so they are not re-probed on every pass
			for k, p := range e.parked {
				if !p.RetryAt.After(now) {
					p.RetryAt = now.Add(e.o.t.ParkRetry)
					e.parked[k] = p
				}
			}
			e.recomputeProbe()
		}
		if !e.nextProbe.IsZero() {
			setNext(e.nextProbe)
		}
	}
	for _, up := range e.upOrder {
		for up.delayed.Len() > 0 && !up.delayed[0].runAt.After(now) {
			ent := heap.Pop(&up.delayed).(*entry)
			ent.idx = -1
			ent.st = stReady
			heap.Push(&up.ready[classOf(ent.prio)], ent)
		}
		if up.delayed.Len() > 0 {
			setNext(up.delayed[0].runAt)
		}
		// stuck watchdog
		for ent := range up.running {
			if ent.stuck {
				continue
			}
			limit := ent.deadline.Add(e.o.t.StuckAfter)
			if !limit.After(now) {
				ent.stuck = true
				up.stuck++
				e.m.ignoredCancel.Add(context.Background(), 1, kindAttr(ent.spec.Kind))
				post = append(post, func() {
					e.log.LogAttrs(context.Background(), slog.LevelError, "job ignores cancellation",
						slog.String("job_key", string(ent.key)), slog.String("job_kind", ent.spec.Kind), slog.String("upstream", up.name))
				})
			} else {
				setNext(limit)
			}
		}
		if up.stuck > 0 && up.stuck == len(up.running) && up.stuck >= up.cfg.MaxConcurrent {
			if up.allStuckAt.IsZero() {
				up.allStuckAt = now
			}
		} else {
			up.allStuckAt = time.Time{}
		}
		if now.Before(up.pausedTill) {
			setNext(up.pausedTill)
			continue
		}
		for {
			if len(up.running) >= up.cfg.MaxConcurrent {
				break // a completion pokes us
			}
			if now.Before(up.nextStart) {
				setNext(up.nextStart)
				break
			}
			var ent *entry
			for c := range up.ready {
				if len(up.ready[c]) == 0 {
					continue
				}
				if e.o.gate != nil {
					w, asked := waits[gateKey{up.name, prioOf(c)}]
					if !asked { // enqueued between gateWaits and the lock: ask on the next pass
						setNext(now)
						continue
					}
					if w > 0 {
						setNext(now.Add(min(w, e.o.t.GateRecheck)))
						continue
					}
				}
				ent = heap.Pop(&up.ready[c]).(*entry)
				break
			}
			if ent == nil {
				break
			}
			ent.idx = -1
			ent.st = stRunning
			ent.startedAt = now
			timeout := ent.spec.Timeout
			if timeout <= 0 {
				timeout = up.cfg.JobTimeout
			}
			ent.deadline = now.Add(timeout)
			ent.stuck = false
			up.running[ent] = struct{}{}
			up.nextStart = now.Add(up.cfg.Pace)
			wait := now.Sub(ent.runAt)
			e.m.wait.Record(context.Background(), wait.Seconds(), kindAttr(ent.spec.Kind))
			e.count("started", ent.spec.Kind)
			k, prio := ent.key, ent.prio
			wg.Go(func() { e.runOne(hctx, ent, k, prio, timeout) })
		}
	}
	e.mu.Unlock()
	post.run(e.log)
	return next
}

func (e *Engine) runOne(hctx context.Context, ent *entry, k job.Key, prio job.Priority, timeout time.Duration) {
	jctx, cancel := context.WithTimeout(hctx, timeout)
	defer cancel()
	jctx, span := e.o.deps.Tracer.Start(jctx, "job.run", trace.WithAttributes(
		attribute.String("kind", ent.spec.Kind), attribute.String("key", string(k)),
		attribute.String("upstream", ent.up.name), attribute.Int("attempt", ent.attempts+1)))
	defer span.End()
	started := time.Now()
	var (
		out job.Outcome
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = &panicError{r}
				e.log.LogAttrs(context.Background(), slog.LevelError, "job panicked",
					slog.String("job_key", string(k)), slog.String("job_kind", ent.spec.Kind),
					slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		out, err = ent.spec.Run(jctx, k)
	}()
	took := time.Since(started)
	switch {
	case err == nil:
		span.SetAttributes(attribute.String("outcome", "success"))
	default:
		span.RecordError(err)
		span.SetStatus(codes.Error, "job failed")
	}
	e.m.duration.Record(context.Background(), took.Seconds(), kindAttr(ent.spec.Kind))
	if took > timeout {
		e.m.overrun.Add(context.Background(), 1, kindAttr(ent.spec.Kind))
	}
	e.finish(hctx, ent, out, err)
}

func (e *Engine) finish(hctx context.Context, ent *entry, out job.Outcome, err error) {
	var post after
	e.mu.Lock()
	up := ent.up
	delete(up.running, ent)
	if ent.stuck {
		up.stuck--
		ent.stuck = false
	}
	now := time.Now()
	kind := ent.spec.Kind
	k := ent.key
	obs := e.observe
	if obs != nil {
		o2, err2 := out, err
		post = append(post, func() { obs(k, o2, err2) })
	}
	// Precedence (unit 01 JB-05): Gone > Permanent > RateLimited > Canceled > Transient, but a rate-limit signal
	// is never lost: whatever else the error is, the upstream pause is imposed (G8, GR-23).
	if retryAt, rl := job.AsRateLimited(err); rl && !(hctx.Err() != nil && errors.Is(err, context.Canceled)) {
		// One limit event hits every job that was in flight when it began: only a job that started after
		// the pause was imposed counts as a new event and escalates the floor.
		if !ent.startedAt.Before(up.pauseSetAt) {
			up.rlStreak++
		}
		if pause := e.pauseUntil(up, retryAt, now); pause.After(up.pausedTill) {
			up.pausedTill = pause
			up.pauseSetAt = now
		}
		until, streak := up.pausedTill, up.rlStreak
		post = append(post, func() {
			e.log.LogAttrs(context.Background(), slog.LevelWarn, "upstream paused by rate limit",
				slog.String("upstream", up.name), slog.Time("until", until), slog.Int("streak", streak))
		})
	}
	switch {
	case err == nil:
		if !ent.startedAt.Before(up.pausedTill) { // only a job that started after the pause ended proves the limit lifted
			up.rlStreak = 0
		}
		e.count("success", kind)
		ent.attempts = 0
		ent.inBackoff = false
		if !ent.eventAt.IsZero() {
			e.m.toPublish.Record(context.Background(), now.Sub(ent.eventAt).Seconds(), kindAttr(kind))
			ent.eventAt = time.Time{}
		}
		if ent.probe {
			ent.probe = false
			if _, was := e.parked[k]; was {
				delete(e.parked, k)
				e.recomputeProbe()
				if f := e.o.onUnparked; f != nil {
					post = append(post, func() { f(k, "probe succeeded") })
				}
			}
		}
		if ent.dirty {
			ent.dirty = false
			ent.eventAt, ent.dirtyAt = ent.dirtyAt, time.Time{}
			ent.runAt = now
			ent.st = stDelayed // place() recomputes
			e.place(ent, now)
		} else {
			delete(e.byKey, k)
		}
	case hctx.Err() != nil && errors.Is(err, context.Canceled):
		e.count("canceled", kind)
		delete(e.byKey, k)
	case job.IsGone(err):
		e.count("gone", kind)
		delete(e.byKey, k)
		if _, was := e.parked[k]; was {
			delete(e.parked, k)
			e.recomputeProbe()
			if f := e.o.onUnparked; f != nil {
				post = append(post, func() { f(k, "gone") }) // the bridge drops the store row
			}
		}
	case job.IsPermanent(err):
		pk := ParkedKey{Key: k, Since: now, RetryAt: now.Add(e.o.t.ParkRetry), Reason: err.Error()}
		if old, was := e.parked[k]; was {
			pk.Since = old.Since
		}
		e.parked[k] = pk
		e.recomputeProbe()
		delete(e.byKey, k)
		var pe *panicError
		if errors.As(err, &pe) {
			e.count("panic", kind)
		}
		e.count("parked", kind)
		post = append(post, func() {
			e.log.LogAttrs(context.Background(), slog.LevelError, "job parked",
				slog.String("job_key", string(k)), slog.String("job_kind", kind), slog.String("reason", pk.Reason), slog.Time("retry_at", pk.RetryAt))
		})
		if f := e.o.onParked; f != nil {
			post = append(post, func() { f(pk) })
		}
	case isRateLimited(err): // no attempt consumed; the pause was imposed above
		e.count("ratelimited", kind)
		ent.dirty = false
		ent.inBackoff = false
		ent.runAt = up.pausedTill
		e.place(ent, now)
	default:
		ent.attempts++
		ent.dirty = false
		e.count("retried", kind)
		if ent.attempts == e.backoff.MaxAttempts {
			e.count("exhausted", kind)
			att := ent.attempts
			post = append(post, func() {
				e.log.LogAttrs(context.Background(), slog.LevelWarn, "job retries exhausted, still retrying at the cap",
					slog.String("job_key", string(k)), slog.String("job_kind", kind), slog.Int("attempt", att), slog.String("err", err.Error()))
			})
			if f := e.o.onExhausted; f != nil {
				a, er := ent.attempts, err
				post = append(post, func() { f(k, a, er) })
			}
		}
		ent.inBackoff = true
		ent.runAt = now.Add(e.backoff.Delay(ent.attempts, e.rnd))
		e.place(ent, now)
	}
	e.mu.Unlock()
	e.poke()
	post.run(e.log) // outside the mutex, after the slot was released
}

func isRateLimited(err error) bool { _, ok := job.AsRateLimited(err); return ok }

// pauseUntil: honour RetryAt (clamped); a RetryAt that is not meaningfully in the future (secondary limit
// without headers) uses an exponential floor 60s, 120s, ... capped at RateLimitMax, plus a 0-1s stagger.
func (e *Engine) pauseUntil(up *upstream, retryAt, now time.Time) time.Time {
	t := retryAt
	if t.Before(now.Add(time.Second)) {
		floor := e.o.t.RateLimitMin << min(up.rlStreak-1, 20)
		if floor <= 0 || floor > e.o.t.RateLimitMax {
			floor = e.o.t.RateLimitMax
		}
		t = now.Add(floor)
	}
	if limit := now.Add(e.o.t.MaxPause); t.After(limit) {
		t = limit
	}
	return t.Add(time.Duration(e.rnd.Int64N(int64(time.Second))))
}

// Inspect returns a consistent copy of the engine state for /status, /livez and metrics.
func (e *Engine) Inspect() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	s := Stats{At: now, Heartbeat: e.heartbeat, Keys: len(e.byKey), Parked: len(e.parked),
		Upstreams: make(map[string]UpstreamStats, len(e.upOrder)), Jobs: make(map[string]uint64, len(e.counts))}
	for k, v := range e.counts {
		s.Jobs[k] = v
	}
	for _, u := range e.upOrder {
		r, d := u.pending()
		us := UpstreamStats{Ready: r, Delayed: d, Running: len(u.running), Stuck: u.stuck, MaxConcurrent: u.cfg.MaxConcurrent,
			AllStuckSince: u.allStuckAt, RateLimitStreak: u.rlStreak}
		if u.pausedTill.After(now) {
			us.PausedUntil = u.pausedTill
		}
		s.Upstreams[u.name] = us
	}
	return s
}

// Parked lists parked keys (oldest first) for /status.
func (e *Engine) Parked() []ParkedKey {
	e.mu.Lock()
	out := make([]ParkedKey, 0, len(e.parked))
	for _, p := range e.parked {
		out = append(out, p)
	}
	e.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	return out
}
