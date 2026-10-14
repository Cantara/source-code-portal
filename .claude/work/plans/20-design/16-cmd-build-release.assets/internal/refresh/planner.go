package refresh

import (
	"context"
	"errors"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PlanSink persists the planner-owned columns. SaveState is non-blocking and has no error: the bridge
// buffers and flushes batches through store.SavePlans (every 5 s and at shutdown).
type PlanSink interface{ SaveState(k job.Key, st PlanState) }

// PlanState is what survives a restart. Attempts live in the store, backoff attempts in the engine.
type PlanState struct {
	NextDue         time.Time
	UnchangedStreak int
}

type itemState uint8

const (
	itIdle     itemState = iota // waiting for NextDue
	itInflight                  // enqueued by Tick or running; waiting for the engine's result
	itParked                    // the engine parked it: not ticked until a result says otherwise
)

type item struct {
	key      job.Key
	ord      int
	state    itemState
	nextDue  time.Time
	streak   int
	everDone bool
}

type kindState struct {
	spec  job.Spec
	items map[job.Key]*item
	order []*item // subject order given by the caller: tie-break of Tick
}

// Progress is the per-kind view for /status and the readiness coverage fallback.
type Progress struct{ Subjects, Completed, Inflight, Parked, Due int }

// Planner is level-triggered: state of record is NextDue per item; webhooks only accelerate.
type Planner struct {
	enq   job.Enqueuer
	sink  PlanSink
	rnd   *rand.Rand
	o     options
	mTick metric.Float64Histogram
	mEnq  metric.Int64Counter
	mSubj metric.Int64Gauge

	mu       sync.Mutex
	kinds    map[string]*kindState
	kindList []string // registration order
	restored map[job.Key]PlanState
	closed   bool
}

// RetryAfterEnqueueFail is how long an item waits after Enqueue refused it (queue full).
const retryAfterEnqueueFail = 30 * time.Second

func NewPlanner(enq job.Enqueuer, sink PlanSink, seed uint64, opts ...Option) *Planner {
	o := buildOptions(opts)
	p := &Planner{enq: enq, sink: sink, rnd: rand.New(rand.NewPCG(seed, seed^0xa5a5a5a5)), o: o,
		kinds: map[string]*kindState{}, restored: map[job.Key]PlanState{}}
	p.mTick, _ = o.deps.Meter.Float64Histogram(MetricPlannerTick, metric.WithUnit("s"))
	p.mEnq, _ = o.deps.Meter.Int64Counter(MetricPlannerEnqueue)
	p.mSubj, _ = o.deps.Meter.Int64Gauge(MetricPlannerSubjects)
	return p
}

// Track registers a kind. Global kinds get their single subject "kind/_" immediately, due now.
func (p *Planner) Track(s job.Spec) {
	if s.Scope != job.ScopeRepo && s.Scope != job.ScopeGlobal {
		panic("refresh: Planner.Track: invalid Scope for kind " + s.Kind) // unit 01: the zero value is invalid on purpose
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.kinds[s.Kind]; dup {
		panic("refresh: Planner.Track twice for kind " + s.Kind)
	}
	ks := &kindState{spec: s, items: map[job.Key]*item{}}
	p.kinds[s.Kind] = ks
	p.kindList = append(p.kindList, s.Kind)
	if s.Scope == job.ScopeGlobal {
		p.setSubjectsLocked(ks, []job.Key{job.NewKey(s.Kind, "_")})
	}
}

// SetSubjects replaces the subject set of a per-repository kind (catalog reconcile through its Tracker).
// New subjects are due immediately unless Restore supplied their state; removed subjects are forgotten.
// The given order is the tie-break of Tick, so the caller can put the most recently pushed repositories first.
func (p *Planner) SetSubjects(kind string, keys []job.Key) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ks := p.kinds[kind]
	if ks == nil {
		return
	}
	p.setSubjectsLocked(ks, keys)
}

func (p *Planner) setSubjectsLocked(ks *kindState, keys []job.Key) {
	next := make(map[job.Key]*item, len(keys))
	order := make([]*item, 0, len(keys))
	for i, k := range keys {
		it := ks.items[k]
		if it == nil {
			it = &item{key: k}
			if st, ok := p.restored[k]; ok {
				it.nextDue, it.streak, it.everDone = st.NextDue, st.UnchangedStreak, true
				delete(p.restored, k)
			}
		}
		it.ord = i
		next[k] = it
		order = append(order, it)
	}
	ks.items, ks.order = next, order
	p.mSubj.Record(context.Background(), int64(len(keys)), metric.WithAttributes(attribute.String("kind", ks.spec.Kind)))
}

// Restore is the warm start: persisted NextDue/streak per key, applied now to known subjects and later to
// subjects that appear. Call before the first Tick.
func (p *Planner) Restore(states map[job.Key]PlanState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, st := range states {
		if ks := p.kinds[k.Kind()]; ks != nil {
			if it := ks.items[k]; it != nil {
				it.nextDue, it.streak, it.everDone = st.NextDue, st.UnchangedStreak, true
				continue
			}
		}
		p.restored[k] = st
	}
}

type due struct {
	key job.Key
	pr  job.Priority
	it  *item
}

// Tick enqueues every idle item with NextDue <= now. It copies the due keys under the planner mutex,
// releases it, then calls Enqueue (no planner lock is held across a call into the engine).
func (p *Planner) Tick(now time.Time) {
	t0 := time.Now()
	var batch []due
	p.mu.Lock()
	for _, kind := range p.kindList {
		ks := p.kinds[kind]
		var local []due
		for _, it := range ks.order {
			if it.state != itIdle || it.nextDue.After(now) {
				continue
			}
			pr := ks.spec.Prio
			if ks.spec.Scope == job.ScopeRepo && !it.everDone && pr < job.PrioBackfill {
				pr = job.PrioBackfill // cold start: every first fetch is backfill
			}
			it.state = itInflight
			local = append(local, due{it.key, pr, it})
		}
		sort.SliceStable(local, func(i, j int) bool { return local[i].it.nextDue.Before(local[j].it.nextDue) })
		batch = append(batch, local...)
	}
	p.mu.Unlock()

	for i, d := range batch {
		err := p.enq.Enqueue(d.key, d.pr, time.Time{}, false)
		res := "ok"
		switch {
		case err == nil:
		case errors.Is(err, job.ErrClosed):
			res = "closed"
		case errors.Is(err, job.ErrQueueFull):
			res = "queue_full"
		default:
			res = "error"
		}
		if err != nil {
			p.mu.Lock()
			d.it.state, d.it.nextDue = itIdle, now.Add(retryAfterEnqueueFail)
			p.mu.Unlock()
		}
		p.mEnq.Add(context.Background(), 1, metric.WithAttributes(attribute.String("kind", d.key.Kind()), attribute.String("result", res)))
		if errors.Is(err, job.ErrClosed) { // the rest cannot be accepted either: leave them idle and due
			p.mu.Lock()
			for _, r := range batch[i+1:] {
				r.it.state = itIdle
			}
			p.mu.Unlock()
			break
		}
	}
	p.mTick.Record(context.Background(), time.Since(t0).Seconds())
}

// OnResult is the engine's observer. It owns next_due_at and unchanged_streak.
func (p *Planner) OnResult(k job.Key, o job.Outcome, err error) {
	now := time.Now()
	p.mu.Lock()
	ks := p.kinds[k.Kind()]
	if ks == nil {
		p.mu.Unlock()
		return
	}
	it := ks.items[k]
	if it == nil {
		p.mu.Unlock()
		return
	}
	var save *PlanState
	switch {
	case err == nil:
		pol := ks.spec.Policy
		if o.Unchanged {
			it.streak++
		} else {
			it.streak = 0
		}
		it.everDone = true
		it.state = itIdle
		if !o.NextDue.IsZero() {
			it.nextDue = o.NextDue
		} else {
			it.nextDue = now.Add(p.interval(pol.TTL, pol.Jitter, pol.IdleBackoffMax, it.streak))
		}
		save = &PlanState{NextDue: it.nextDue, UnchangedStreak: it.streak}
	case job.IsGone(err):
		delete(ks.items, k)
		for i, x := range ks.order {
			if x == it {
				ks.order = append(ks.order[:i], ks.order[i+1:]...)
				break
			}
		}
	case job.IsPermanent(err):
		it.state = itParked
	case errors.Is(err, context.Canceled):
		it.state, it.nextDue = itIdle, now // shutdown: due again at the next start
	default:
		// transient failure or rate limit: the engine retries; the item stays inflight.
		if it.state == itParked {
			it.state = itInflight
		}
	}
	p.mu.Unlock()
	if save != nil && p.sink != nil {
		p.sink.SaveState(k, *save)
	}
}

// interval = TTL doubled once per consecutive unchanged result up to idleMax, then +-jitter.
func (p *Planner) interval(ttl time.Duration, jitter float64, idleMax time.Duration, streak int) time.Duration {
	d := ttl
	if idleMax > ttl {
		for i := 0; i < streak && d < idleMax; i++ {
			d *= 2
		}
		d = min(d, idleMax)
	}
	if jitter > 0 {
		d = time.Duration(float64(d) * (1 + jitter*(2*p.rnd.Float64()-1)))
	}
	return max(d, time.Second)
}

// Progress summarises one kind. due counts idle items whose NextDue has passed at now.
func (p *Planner) Progress(kind string, now time.Time) Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pr Progress
	if ks := p.kinds[kind]; ks != nil {
		pr.Subjects = len(ks.items)
		for _, it := range ks.items {
			if it.everDone {
				pr.Completed++
			}
			switch it.state {
			case itInflight:
				pr.Inflight++
			case itParked:
				pr.Parked++
			case itIdle:
				if !it.nextDue.After(now) {
					pr.Due++
				}
			}
		}
	}
	return pr
}

// Enqueue makes *Planner a job.Enqueuer for features and reactors: it marks a tracked idle item inflight
// BEFORE forwarding to the engine (marking afterwards races with a fast completion that already ran
// OnResult), and reverts the mark if the engine refused the key. A Tick that fires while a webhook-triggered
// run is in flight then does not enqueue the item a second time (which would cause one dirty follow-up run).
func (p *Planner) Enqueue(k job.Key, pr job.Priority, notBefore time.Time, force bool) error {
	var it *item
	p.mu.Lock()
	if ks := p.kinds[k.Kind()]; ks != nil {
		if x := ks.items[k]; x != nil && x.state == itIdle {
			x.state, it = itInflight, x
		}
	}
	p.mu.Unlock()
	err := p.enq.Enqueue(k, pr, notBefore, force)
	if err != nil && it != nil {
		p.mu.Lock()
		if it.state == itInflight {
			it.state = itIdle
		}
		p.mu.Unlock()
	}
	return err
}

var _ job.Enqueuer = (*Planner)(nil)
