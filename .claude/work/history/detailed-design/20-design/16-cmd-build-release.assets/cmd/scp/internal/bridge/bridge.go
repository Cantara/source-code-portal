// Package bridge holds the wiring types that implement one package's consumer interface over another package's data
// (architecture 6.2, 4.5). It is wiring, not a feature: it may import feature roots, refresh and store, and only
// cmd/scp imports it. Every type here takes plain functions or tiny local interfaces for the packages the matrix does
// not let it import (github, platform/httpx), so the closed-world table needs no extra edge.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/ops"
	"github.com/cantara/source-code-portal/internal/refresh"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// The slices of store.Store the bridges write through, declared here by their consumer (architecture 5.5). *sqlite.Store and the
// in-memory store satisfy them structurally; cmd/scp/assert.go proves it.
type (
	PlanStore interface {
		SavePlans(ctx context.Context, rows []store.PlanRow) error
	}
	TaskStore interface {
		SaveTask(ctx context.Context, name string, s store.TaskState) error
		LoadTasks(ctx context.Context) (map[string]store.TaskState, error)
	}
	ParkStore interface {
		Park(ctx context.Context, key, reason string, retryAt time.Time) error
		Unpark(ctx context.Context, key string) error
	}
)

// DefaultPlanFlushEvery is the batch interval of PlanSink (04-refresh-webhook 2.5: losing the last 5 s only makes an item due early).
const DefaultPlanFlushEvery = 5 * time.Second

// PlanSink implements refresh.PlanSink. SaveState never blocks on I/O: it records the latest PlanState per key in a map;
// Run flushes the map through store.SavePlans on a timer, one transaction per flush, and Close flushes once more after the
// refresh stage has stopped, so the planner's last states reach the database.
type PlanSink struct {
	st    PlanStore
	every time.Duration
	onErr func(op string, err error)
	kinds map[string]string // job kind -> item kind (cross-design review CD-JB-01); nil or a missing entry means "same name"

	flushMu sync.Mutex // serializes Flush: a slow failing flush A must not put back an older state after a newer flush B succeeded
	mu      sync.Mutex
	pending map[job.Key]refresh.PlanState
	dropped uint64 // global keys and malformed keys: counted, never persisted (store.SavePlans skips them anyway)
}

// NewPlanSink panics on a nil store; every <= 0 selects DefaultPlanFlushEvery; onErr may be nil.
func NewPlanSink(st PlanStore, every time.Duration, onErr func(op string, err error)) *PlanSink {
	if st == nil {
		panic("bridge: NewPlanSink: nil store")
	}
	if every <= 0 {
		every = DefaultPlanFlushEvery
	}
	if onErr == nil {
		onErr = func(string, error) {}
	}
	return &PlanSink{st: st, every: every, onErr: onErr, pending: map[job.Key]refresh.PlanState{}}
}

var _ refresh.PlanSink = (*PlanSink)(nil)

// WithItemKinds installs the job-kind to item-kind table (CD-JB-01, unit 03 plan-kind rule 2.5): the store's plan columns live on the
// `item` row, whose kind is the owning feature's ItemKind (`commits.ring`, `docs.repo`, `manifests.facts`, `catalog.stats`), while the
// engine and planner speak job kinds (`commits.refresh`, `docs.refresh`, `manifests.refresh`, `catalog.stats`). The first draft used the
// job kind as the item kind, so SavePlans matched no item row for commits, docs and manifests and no plan was ever persisted
// (every restart would have re-fetched everything). cmd/scp builds the table from the features' exported constants.
func (p *PlanSink) WithItemKinds(m map[string]string) *PlanSink {
	p.kinds = make(map[string]string, len(m))
	for k, v := range m {
		p.kinds[k] = v
	}
	return p
}

// ItemKindOf is the item kind that carries the plan of a job kind.
func (p *PlanSink) ItemKindOf(jobKind string) string {
	if v, ok := p.kinds[jobKind]; ok {
		return v
	}
	return jobKind
}

// JobKindOf is the inverse, used when LoadPlans rows are turned back into job keys for Planner.Restore (an item kind that no job
// maps to is returned unchanged).
func (p *PlanSink) JobKindOf(itemKind string) string {
	for j, i := range p.kinds {
		if i == itemKind {
			return j
		}
	}
	return itemKind
}

// SaveState implements refresh.PlanSink.
func (p *PlanSink) SaveState(k job.Key, st refresh.PlanState) {
	if _, ok := p.itemKey(k); !ok {
		p.mu.Lock()
		p.dropped++
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	p.pending[k] = st
	p.mu.Unlock()
}

// itemKey maps the engine key "kind/source/owner/name" to the store key; global keys ("kind/_") have no item row.
func (p *PlanSink) itemKey(k job.Key) (store.ItemKey, bool) {
	parts := k.Parts()
	if len(parts) != 3 || parts[0] == "_" || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return store.ItemKey{}, false
	}
	return store.ItemKey{Repo: store.RepoKey{Source: parts[0], Owner: parts[1], Name: parts[2]}, Kind: p.ItemKindOf(k.Kind())}, true
}

// Pending reports how many keys await the next flush (tests, /status).
func (p *PlanSink) Pending() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.pending) }

// Dropped reports the number of SaveState calls ignored because the key has no item row.
func (p *PlanSink) Dropped() uint64 { p.mu.Lock(); defer p.mu.Unlock(); return p.dropped }

// Flush writes every pending state in one SavePlans call. On error the rows are put back unless SaveState wrote a newer
// state for the same key meanwhile, so a failing database never loses the latest state and never regresses it.
func (p *PlanSink) Flush(ctx context.Context) error {
	p.flushMu.Lock()
	defer p.flushMu.Unlock()
	p.mu.Lock()
	batch := p.pending
	p.pending = map[job.Key]refresh.PlanState{}
	p.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	rows := make([]store.PlanRow, 0, len(batch))
	for k, st := range batch {
		ik, _ := p.itemKey(k)
		rows = append(rows, store.PlanRow{Key: ik, NextDue: st.NextDue, UnchangedStreak: st.UnchangedStreak})
	}
	if err := p.st.SavePlans(ctx, rows); err != nil {
		p.mu.Lock()
		for k, st := range batch {
			if _, newer := p.pending[k]; !newer {
				p.pending[k] = st
			}
		}
		p.mu.Unlock()
		p.onErr("SavePlans", err)
		return fmt.Errorf("save plans: %w", err)
	}
	return nil
}

// Run flushes every p.every until ctx ends. It does not flush on exit: Close does, after the stage worker was joined.
func (p *PlanSink) Run(ctx context.Context) error {
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = p.Flush(ctx) // already counted by onErr; the next tick retries
		}
	}
}

// Close is the final flush (lifecycle stage Close, after Run returned and the engine stopped).
func (p *PlanSink) Close(ctx context.Context) error { return p.Flush(ctx) }

// RateAdmitter is what the gate bridge needs from the GitHub adapter: *github.Client satisfies it (05 section 2.3). Declared
// here by its consumer.
type RateAdmitter interface {
	Admit(class github.ReserveClass) time.Duration
}

// RateGate implements refresh.Gate over the adapter's budget gate (05 section 2.3, architecture 6.2 and 8.6, ADR-015). It
// resolves open item X6 of unit 04 in favor of unit 05: the reserve arithmetic (40/15/2 percent) lives in the adapter next to the
// rate book it reads, and refresh keeps only the Gate interface. Only the upstream "github" is gated; every other upstream
// (jenkins, snyk, maven-central, deps-dev, local) admits at once.
type RateGate struct{ C RateAdmitter }

var _ refresh.Gate = RateGate{}

// Admit implements refresh.Gate. The default arm maps any future priority to the interactive class, which admits whenever the
// bucket is not empty (05 section 2.3).
func (g RateGate) Admit(upstream string, p job.Priority) time.Duration {
	if upstream != UpstreamGitHub {
		return 0
	}
	switch p {
	case job.PrioBackfill:
		return g.C.Admit(github.ClassBackfill)
	case job.PrioScheduled:
		return g.C.Admit(github.ClassScheduled)
	case job.PrioWebhook:
		return g.C.Admit(github.ClassWebhook)
	default:
		return g.C.Admit(github.ClassInteractive)
	}
}

// UpstreamGitHub is the engine upstream name of every job that calls the GitHub adapter (04 section 2.5).
const UpstreamGitHub = "github"

// SubjectSetter is *refresh.Planner's method; Trigger is *refresh.Scheduler's.
type SubjectSetter interface {
	SetSubjects(kind string, keys []job.Key)
}
type Trigger interface{ Trigger(name string) bool }

// Tracker implements catalog.Tracker: refresh knows only string keys (G8), so the bridge turns repositories into keys.
type Tracker struct {
	kinds []string // per-repository job kinds (ScopeRepo specs), fixed at wiring time
	set   SubjectSetter
	tick  Trigger
}

// PlannerTickTask is the scheduler task that Tracker nudges after the subject set changed (04 2.5).
const PlannerTickTask = "planner.tick"

func NewTracker(kinds []string, set SubjectSetter, tick Trigger) *Tracker {
	return &Tracker{kinds: append([]string(nil), kinds...), set: set, tick: tick}
}

// SetRepos implements catalog.Tracker. The order of repos is the tie-break order of the planner's tick (catalog passes the
// most recently pushed first), so it is preserved.
func (t *Tracker) SetRepos(repos []scm.RepoRef) {
	for _, kind := range t.kinds {
		keys := make([]job.Key, len(repos))
		for i, r := range repos {
			keys[i] = job.NewKey(kind, string(r.Source), r.Owner, r.Name)
		}
		t.set.SetSubjects(kind, keys)
	}
	if t.tick != nil {
		t.tick.Trigger(PlannerTickTask)
	}
}

// Retrier implements ops.Retrier: the admin "retry" action force-enqueues a key at interactive priority through the PLANNER
// (04 section 2.5: the planner, not the engine, is the Enqueuer for every caller that is not a Spec's own Run). It translates the
// engine's sentinels into the three ops.Err* values the admin handler maps to 404 and 503 (ops cannot import job or refresh).
type Retrier struct{ enq job.Enqueuer }

func NewRetrier(enq job.Enqueuer) *Retrier { return &Retrier{enq: enq} }

// Retry implements ops.Retrier.
func (r *Retrier) Retry(key string) error {
	k, err := job.ParseKey(key)
	if err != nil {
		return ops.ErrUnknownKey
	}
	switch err := r.enq.Enqueue(k, job.PrioInteractive, time.Time{}, true); {
	case err == nil:
		return nil
	case errors.Is(err, refresh.ErrUnknownKind):
		return ops.ErrUnknownKey
	case errors.Is(err, job.ErrQueueFull):
		return ops.ErrQueueFull
	case errors.Is(err, job.ErrClosed):
		return ops.ErrClosed
	default:
		return fmt.Errorf("retry %s: %w", k.Kind(), err) // the kind only: a key can carry a repository name the log line must not repeat
	}
}

// Watermarks implements webhook.Watermarks over task_state: last_success carries the watermark.
type Watermarks struct{ st TaskStore }

func NewWatermarks(st TaskStore) *Watermarks { return &Watermarks{st: st} }

// Load returns the zero time when no watermark was saved.
func (w *Watermarks) Load(ctx context.Context, name string) (time.Time, error) {
	m, err := w.st.LoadTasks(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("load watermark %s: %w", name, err)
	}
	return m[name].LastSuccess, nil
}

// Save stores t as both last attempt and last success.
func (w *Watermarks) Save(ctx context.Context, name string, t time.Time) error {
	if err := w.st.SaveTask(ctx, name, store.TaskState{LastAttempt: t, LastSuccess: t}); err != nil {
		return fmt.Errorf("save watermark %s: %w", name, err)
	}
	return nil
}

// TaskSink is refresh.Task.OnDone persisted into task_state (scheduler cadence resumes after a restart).
type TaskSink struct {
	st    TaskStore
	onErr func(op string, err error)
}

func NewTaskSink(st TaskStore, onErr func(op string, err error)) *TaskSink {
	if onErr == nil {
		onErr = func(string, error) {}
	}
	return &TaskSink{st: st, onErr: onErr}
}

// OnDone has the signature of refresh.Task.OnDone and runs outside the scheduler mutex; the write is bounded.
func (t *TaskSink) OnDone(name string, s refresh.TaskStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.st.SaveTask(ctx, name, store.TaskState{LastAttempt: s.LastAttempt, LastSuccess: s.LastSuccess, LastError: s.LastErr}); err != nil {
		t.onErr("SaveTask", err)
	}
}

// ParkedSink persists the engine's park and unpark hooks (WithOnParked, WithOnUnparked). Hooks run on a worker goroutine
// after the engine mutex was released and may take a millisecond, so each write is bounded at 5 s and failures are only
// counted: a lost row means a poison key is retried after a restart and parked again, which is harmless (04 5).
type ParkedSink struct {
	st    ParkStore
	onErr func(op string, err error)
}

func NewParkedSink(st ParkStore, onErr func(op string, err error)) *ParkedSink {
	if onErr == nil {
		onErr = func(string, error) {}
	}
	return &ParkedSink{st: st, onErr: onErr}
}

func (p *ParkedSink) Park(pk refresh.ParkedKey) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.st.Park(ctx, string(pk.Key), pk.Reason, pk.RetryAt); err != nil {
		p.onErr("Park", err)
	}
}

func (p *ParkedSink) Unpark(k job.Key, _ string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.st.Unpark(ctx, string(k)); err != nil {
		p.onErr("Unpark", err)
	}
}
