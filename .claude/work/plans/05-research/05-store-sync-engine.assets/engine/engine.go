// Package engine is a compile-checked prototype of the SCP sync engine core
// (keyed coalescing queue, per-upstream limits, delayed jittered retries,
// rate-limit pause, parking of permanent failures). Prototype for research, not production.
package engine

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

type (
	Key      string
	Upstream string
	Priority uint8
)

const (
	PrioInteractive Priority = iota // a user is waiting on this key
	PrioWebhook
	PrioScheduled
	PrioBackfill
)

type Job struct {
	Key      Key
	Kind     string
	Upstream Upstream
	Prio     Priority
}

// Handler performs one idempotent, level-triggered refresh: "bring item Key up to date".
type Handler func(ctx context.Context, j Job) error

// Typed outcomes (classification by errors.As / errors.AsType).
type RateLimitedError struct{ ResetAt time.Time }

func (e *RateLimitedError) Error() string {
	return "rate limited until " + e.ResetAt.Format(time.RFC3339)
}

type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return "permanent: " + e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

type UpstreamConfig struct {
	MaxConcurrent int
	Pace          time.Duration // minimum spacing between job starts
}

type Backoff struct {
	Base, Cap   time.Duration
	MaxAttempts int // after this many consecutive transient failures: keep retrying at Cap, emit OnExhausted
}

func (b Backoff) delay(attempt int, r *rand.Rand) time.Duration {
	d := b.Base << min(attempt, 30)
	if d <= 0 || d > b.Cap {
		d = b.Cap
	}
	// Full jitter (AWS Architecture Blog, 2015) with a floor of Base so a retry is never ~0s:
	// delay is uniform in [Base, d].
	if d <= b.Base {
		return b.Base
	}
	return b.Base + time.Duration(r.Int64N(int64(d-b.Base)+1))
}

type state uint8

const (
	stDelayed state = iota
	stReady
	stRunning
)

type entry struct {
	job      Job
	st       state
	runAt    time.Time
	attempts int
	dirty    bool // enqueue arrived while running: run once more after completion
	seq      uint64
	ri, di   int // heap indexes
}

type readyHeap []*entry

func (h readyHeap) Len() int { return len(h) }
func (h readyHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.job.Prio != b.job.Prio {
		return a.job.Prio < b.job.Prio
	}
	return a.seq < b.seq
}
func (h readyHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].ri, h[j].ri = i, j }
func (h *readyHeap) Push(x any)   { e := x.(*entry); e.ri = len(*h); *h = append(*h, e) }
func (h *readyHeap) Pop() any     { o := *h; n := len(o); e := o[n-1]; *h = o[:n-1]; return e }

type delayedHeap []*entry

func (h delayedHeap) Len() int           { return len(h) }
func (h delayedHeap) Less(i, j int) bool { return h[i].runAt.Before(h[j].runAt) }
func (h delayedHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].di, h[j].di = i, j }
func (h *delayedHeap) Push(x any)        { e := x.(*entry); e.di = len(*h); *h = append(*h, e) }
func (h *delayedHeap) Pop() any          { o := *h; n := len(o); e := o[n-1]; *h = o[:n-1]; return e }

type upstream struct {
	cfg        UpstreamConfig
	ready      readyHeap
	delayed    delayedHeap
	running    int
	pausedTill time.Time
	nextStart  time.Time
}

type Stats struct {
	Enqueued, Coalesced, MarkedDirty, Started, Succeeded, Retried, RateLimited, Parked, Exhausted, Panics uint64
}

type Engine struct {
	handlers map[string]Handler
	backoff  Backoff
	rnd      *rand.Rand

	mu     sync.Mutex
	ups    map[Upstream]*upstream
	byKey  map[Key]*entry
	parked map[Key]error
	seq    uint64
	stats  Stats
	wake   chan struct{}

	// Grace is how long in-flight handlers may keep running after Run's ctx is cancelled (graceful drain);
	// after Grace their context is cancelled too.
	Grace time.Duration

	OnParked    func(Job, error) // dead-letter hook (log, metric, persist status)
	OnExhausted func(Job, int, error)
}

func New(b Backoff, seed uint64) *Engine {
	return &Engine{
		handlers: map[string]Handler{}, backoff: b,
		rnd: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		ups: map[Upstream]*upstream{}, byKey: map[Key]*entry{}, parked: map[Key]error{},
		wake: make(chan struct{}, 1),
	}
}

func (e *Engine) AddUpstream(name Upstream, cfg UpstreamConfig) { e.ups[name] = &upstream{cfg: cfg} }
func (e *Engine) Register(kind string, h Handler)               { e.handlers[kind] = h }

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

var ErrUnknownUpstream = errors.New("unknown upstream")

// Enqueue never blocks and never fails for capacity: the key space is bounded by the catalogue.
// force=true clears a parked (permanent-failure) key.
func (e *Engine) Enqueue(j Job, notBefore time.Time, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	up, ok := e.ups[j.Upstream]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownUpstream, j.Upstream)
	}
	if _, isParked := e.parked[j.Key]; isParked {
		if !force {
			return nil // suppressed; scheduler must not resurrect poison keys
		}
		delete(e.parked, j.Key)
	}
	now := time.Now()
	if notBefore.Before(now) {
		notBefore = now
	}
	e.stats.Enqueued++
	if cur, ok := e.byKey[j.Key]; ok {
		switch cur.st {
		case stRunning:
			cur.dirty = true // coalesce into exactly one follow-up run
			if j.Prio < cur.job.Prio {
				cur.job.Prio = j.Prio
			}
			e.stats.MarkedDirty++
		default:
			e.stats.Coalesced++
			if j.Prio < cur.job.Prio {
				cur.job.Prio = j.Prio
			}
			if notBefore.Before(cur.runAt) { // promote, never demote
				cur.runAt = notBefore
			}
			e.reschedule(up, cur)
		}
		e.poke()
		return nil
	}
	e.seq++
	ent := &entry{job: j, runAt: notBefore, seq: e.seq, ri: -1, di: -1}
	e.byKey[j.Key] = ent
	e.reschedule(up, ent)
	e.poke()
	return nil
}

// reschedule places ent in the ready or delayed heap according to runAt. mu held.
func (e *Engine) reschedule(up *upstream, ent *entry) {
	if ent.ri >= 0 {
		heap.Remove(&up.ready, ent.ri)
		ent.ri = -1
	}
	if ent.di >= 0 {
		heap.Remove(&up.delayed, ent.di)
		ent.di = -1
	}
	if ent.runAt.After(time.Now()) {
		ent.st = stDelayed
		heap.Push(&up.delayed, ent)
	} else {
		ent.st = stReady
		heap.Push(&up.ready, ent)
	}
}

// Run is the dispatcher. It returns after ctx is cancelled and all in-flight jobs have returned.
func (e *Engine) Run(ctx context.Context) {
	var wg sync.WaitGroup
	// Handlers run under hctx: it outlives ctx by Grace so a shutdown drains instead of aborting.
	hctx, hcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer hcancel()
	stopGrace := context.AfterFunc(ctx, func() { time.AfterFunc(e.Grace, hcancel) })
	defer stopGrace()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		next := e.dispatch(hctx, ctx, &wg)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if next.IsZero() {
			next = time.Now().Add(time.Hour)
		}
		timer.Reset(max(0, time.Until(next)))
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-e.wake:
		case <-timer.C:
		}
	}
}

// dispatch starts every job that may start now and returns the next instant at which it should run again.
func (e *Engine) dispatch(hctx, ctx context.Context, wg *sync.WaitGroup) (next time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	setNext := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	if ctx.Err() != nil {
		return time.Time{} // draining: start nothing new
	}
	for _, up := range e.ups {
		for up.delayed.Len() > 0 && !up.delayed[0].runAt.After(now) {
			ent := heap.Pop(&up.delayed).(*entry)
			ent.di = -1
			ent.st = stReady
			heap.Push(&up.ready, ent)
		}
		if up.delayed.Len() > 0 {
			setNext(up.delayed[0].runAt)
		}
		for up.ready.Len() > 0 {
			if now.Before(up.pausedTill) {
				setNext(up.pausedTill)
				break
			}
			if up.running >= up.cfg.MaxConcurrent {
				break // a completion pokes us
			}
			if now.Before(up.nextStart) {
				setNext(up.nextStart)
				break
			}
			ent := heap.Pop(&up.ready).(*entry)
			ent.ri = -1
			ent.st = stRunning
			up.running++
			up.nextStart = now.Add(up.cfg.Pace)
			e.stats.Started++
			job := ent.job
			wg.Go(func() { e.runOne(hctx, up, ent, job) })
		}
	}
	return next
}

func (e *Engine) runOne(ctx context.Context, up *upstream, ent *entry, job Job) {
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				e.mu.Lock()
				e.stats.Panics++
				e.mu.Unlock()
				err = &PermanentError{Err: fmt.Errorf("panic: %v", r)}
			}
		}()
		h := e.handlers[job.Kind]
		if h == nil {
			err = &PermanentError{Err: fmt.Errorf("no handler for kind %q", job.Kind)}
			return
		}
		err = h(ctx, job)
	}()
	e.finish(ctx, up, ent, err)
}

func (e *Engine) finish(ctx context.Context, up *upstream, ent *entry, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	defer e.poke()
	up.running--
	now := time.Now()
	var rl *RateLimitedError
	var perm *PermanentError
	switch {
	case err == nil:
		e.stats.Succeeded++
		ent.attempts = 0
		if ent.dirty {
			ent.dirty = false
			ent.runAt = now
			e.reschedule(up, ent)
			return
		}
		delete(e.byKey, ent.job.Key)
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		// shutting down: leave state untouched; level-triggered planner re-derives the work after restart.
		delete(e.byKey, ent.job.Key)
	case errors.As(err, &rl):
		e.stats.RateLimited++
		// pause the whole upstream; do not count an attempt; small deterministic-ish stagger after reset.
		resume := rl.ResetAt.Add(time.Duration(e.rnd.Int64N(int64(time.Second))))
		if resume.After(up.pausedTill) {
			up.pausedTill = resume
		}
		ent.dirty = false
		ent.runAt = up.pausedTill
		e.reschedule(up, ent)
	case errors.As(err, &perm):
		e.stats.Parked++
		delete(e.byKey, ent.job.Key)
		e.parked[ent.job.Key] = err
		if e.OnParked != nil {
			e.OnParked(ent.job, err)
		}
	default: // transient
		ent.attempts++
		e.stats.Retried++
		if e.backoff.MaxAttempts > 0 && ent.attempts == e.backoff.MaxAttempts {
			e.stats.Exhausted++
			if e.OnExhausted != nil {
				e.OnExhausted(ent.job, ent.attempts, err)
			}
		}
		ent.dirty = false // the retry itself picks up any newer state
		ent.runAt = now.Add(e.backoff.delay(ent.attempts, e.rnd))
		e.reschedule(up, ent)
	}
}

func (e *Engine) Inspect() (s Stats, pending int, parked int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats, len(e.byKey), len(e.parked)
}
