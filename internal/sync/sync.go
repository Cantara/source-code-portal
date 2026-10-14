// Package sync runs the jobs that read GitHub. Every job key has one entry with a due time; the dispatcher starts the most urgent
// due entry while concurrency, pace, rate budget and source pauses allow. A result is written to the store in one transaction and
// handed to the job kind's Publish, which is also how the warm start rebuilds the snapshot from the store.
package sync

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

const (
	pace           = 100 * time.Millisecond // between job starts
	jitter         = 0.1                    // of the TTL, both ways
	retryBase      = 2 * time.Second
	retryMax       = 15 * time.Minute
	permanentRetry = 24 * time.Hour
	rateWaitBase   = time.Minute // secondary limit without Retry-After; doubles to rateWaitMax
	rateWaitMax    = 15 * time.Minute
	authWaitBase   = 5 * time.Minute
	authWaitMax    = time.Hour
	idleWake       = time.Minute
	defaultTimeout = 2 * time.Minute
)

// Priority orders due jobs; lower runs first.
type Priority uint8

const (
	PrioEvent     Priority = iota + 1 // a webhook or a change seen in a listing
	PrioScheduled                     // the TTL ran out
	PrioBackfill                      // never fetched
)

// reserve is the share of a credential's rate limit a priority leaves untouched.
func (p Priority) reserve() float64 {
	switch p {
	case PrioEvent:
		return 0.02
	case PrioScheduled:
		return 0.15
	default:
		return 0.40
	}
}

// Event is a reason to run a job before its TTL. It is a name, stable on the wire between instances: the engine's own below, and
// for any other GitHub delivery about a repository its GitHub event name (webhook.Normalise), so a spec subscribes to it by name.
type Event string

const (
	EventPush          Event = "push"   // new commits on a repository's default branch, or a changed pushed_at
	EventSourceChanged Event = "source" // repositories were added, removed or renamed in a source
	EventTag           Event = "tag"    // a tag was pushed or a release published
)

// Job is what a Spec's Run receives.
type Job struct {
	Key    string
	Source string           // configured source id
	Repo   snapshot.RepoKey // empty for per-source jobs
	Prev   store.Item       // the last stored outcome; State never when there is none
}

// Result is a successful run.
type Result struct {
	State      snapshot.State // default ok
	Unchanged  bool           // the content was confirmed (304 or equal SHA): only the times are updated
	ContentSHA string
	Payload    []byte
	// Catalogue, when not nil, is the complete list of the source's repositories (per-source jobs only). Repositories that left are
	// deleted with their items; new ones get entries for every per-repository kind.
	Catalogue []store.Repo
}

// Spec is one kind of job.
type Spec struct {
	Kind      string
	PerSource bool // keyed "<kind>/<source>"; otherwise "<kind>/<owner>/<name>" for every catalogued repository
	Lists     bool // per-source: results carry the source's catalogue
	Prio      Priority
	TTL       time.Duration
	Timeout   time.Duration // default 2m
	On        []Event
	Run       func(ctx context.Context, j Job) (Result, error)
	// Publish puts a stored item into the snapshot. It runs for live results and at the warm start, in registration order.
	Publish func(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness)
}

// Shared is one recorded outcome as instances share it (internal/cluster): the item with its schedule, for listing jobs the source's
// catalogue, and the keys of the items that left with their repositories. Rev orders the outcomes of one key.
type Shared struct {
	Rev       int64
	Source    string
	Item      store.Item
	Catalogue []store.Repo
	Removed   []string
}

// Budget reports the share of the rate limit left for owner's credential in source, and when it resets.
type Budget interface {
	Remaining(source, owner string) (frac float64, reset time.Time)
}

// Errors from Run are classified by these behaviours; anything else is transient.
type (
	rateLimited interface{ RateLimitedUntil() (time.Time, bool) }
	authFailed  interface{ AuthFailed() bool }
	permanent   interface{ Permanent() bool }
	coded       interface{ ErrorCode() string }
)

// Engine runs the registered specs.
type Engine struct {
	store   *store.DB
	pub     *snapshot.Publisher
	budget  Budget
	sources []string
	n       int // concurrency
	log     *slog.Logger

	specs []*Spec
	wake  chan struct{}
	done  chan outcome

	observe func(kind, result string, d time.Duration)
	beat    atomic.Int64 // unix nanoseconds of the dispatcher's last loop
	share   func(Shared) // nil when the instance is alone
	leader  atomic.Bool  // runs jobs
	warm    chan struct{}

	mu      gosync.Mutex
	entries map[string]*entry
	repos   map[snapshot.RepoKey]store.Repo
	pauses  map[string]*pause // by source
	rev     int64             // the newest revision made or seen
	orphans map[string]Shared // received items of repositories not catalogued yet, by key
}

type entry struct {
	spec    *Spec
	key     string
	source  string
	repo    snapshot.RepoKey
	item    store.Item
	due     time.Time
	prio    Priority
	backoff bool // due is a retry time that events do not pull earlier
	running bool
	dirty   bool  // an event arrived while running: run once more
	rev     int64 // revision of item when it was shared or received
}

type pause struct {
	until      time.Time
	rateStreak int // pauses without Retry-After since the last success
	authStreak int
}

type outcome struct {
	e       *entry
	res     Result
	err     error
	started time.Time
}

// New returns an engine for the configured source ids. budget may be nil.
func New(st *store.DB, pub *snapshot.Publisher, budget Budget, sources []string, concurrency int, log *slog.Logger) *Engine {
	e := &Engine{store: st, pub: pub, budget: budget, sources: slices.Clone(sources), n: max(concurrency, 1), log: log,
		wake: make(chan struct{}, 1), done: make(chan outcome), warm: make(chan struct{}), entries: map[string]*entry{},
		repos: map[snapshot.RepoKey]store.Repo{}, pauses: map[string]*pause{}, orphans: map[string]Shared{},
		observe: func(string, string, time.Duration) {}}
	e.leader.Store(true)
	return e
}

// Share makes the engine one of several instances: fn receives every recorded outcome, and the engine runs no jobs until Lead. Call
// it before Run.
func (e *Engine) Share(fn func(Shared)) {
	e.share = fn
	e.leader.Store(false)
}

// Lead starts or stops running jobs. A job that finishes after the lead was lost is dropped: the new leader runs it again.
func (e *Engine) Lead(on bool) {
	e.leader.Store(on)
	e.poke()
}

// Warm is closed when the warm start has published the store's content.
func (e *Engine) Warm() <-chan struct{} { return e.warm }

// Observe sets fn to receive every finished job: its kind, result (ok, not_modified, error, rate_limited) and duration. Call it
// before Run.
func (e *Engine) Observe(fn func(kind, result string, d time.Duration)) { e.observe = fn }

// Alive reports whether the dispatcher loop ran recently; it is true until Run starts.
func (e *Engine) Alive() bool {
	b := e.beat.Load()
	return b == 0 || time.Since(time.Unix(0, b)) < 3*idleWake
}

// Depth returns how many jobs are due and waiting, and how many run; both are zero on an instance that does not lead.
func (e *Engine) Depth() (waiting, running int) {
	if !e.leader.Load() {
		return 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, en := range e.entries {
		switch {
		case en.running:
			running++
		case !en.due.After(now):
			waiting++
		}
	}
	return waiting, running
}

// Register adds a job kind. It must be called before Run.
func (e *Engine) Register(s Spec) error {
	if s.Kind == "" || strings.Contains(s.Kind, "/") || s.Run == nil || s.Publish == nil {
		return fmt.Errorf("sync: spec %q: kind without /, Run and Publish are required", s.Kind)
	}
	if slices.ContainsFunc(e.specs, func(o *Spec) bool { return o.Kind == s.Kind }) {
		return fmt.Errorf("sync: duplicate kind %q", s.Kind)
	}
	if s.Timeout <= 0 {
		s.Timeout = defaultTimeout
	}
	e.specs = append(e.specs, &s)
	return nil
}

// Notify runs every spec listening for ev soon: per-repository specs for repo, per-source specs for repo's source. It never blocks.
// A job that is running runs once more after it finishes; a job in retry backoff keeps its retry time.
func (e *Engine) Notify(ev Event, repo snapshot.RepoKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, known := e.repos[repo]
	if !known {
		return
	}
	now := time.Now()
	for _, s := range e.specs {
		if !slices.Contains(s.On, ev) {
			continue
		}
		key := s.Kind + "/" + string(repo)
		if s.PerSource {
			key = s.Kind + "/" + r.ID.Source
		}
		if en := e.entries[key]; en != nil {
			en.event(now)
		}
	}
	e.poke()
}

// event makes the entry due now at event priority; a running job runs once more, a retry keeps its time. Callers hold e.mu.
func (en *entry) event(now time.Time) {
	en.prio = PrioEvent
	switch {
	case en.running:
		en.dirty = true
	case !en.backoff && en.due.After(now):
		en.due = now
	}
}

// NotifySource runs every per-source spec listening for ev for source soon, for events about repositories the engine may not know yet
// (created, transferred in, installation changes). It never blocks.
func (e *Engine) NotifySource(ev Event, source string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, s := range e.specs {
		if !s.PerSource || !slices.Contains(s.On, ev) {
			continue
		}
		if en := e.entries[s.Kind+"/"+source]; en != nil {
			en.event(now)
		}
	}
	e.poke()
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Run loads the store into the snapshot, then runs jobs until ctx ends. Jobs in flight at the end are cancelled; their rows are
// untouched, so they are due again after a restart.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.warmStart(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	running := 0
	var lastStart time.Time
	for {
		e.beat.Store(time.Now().UnixNano())
		wait := idleWake
		if running < e.n && e.leader.Load() {
			if next := lastStart.Add(pace); time.Now().Before(next) {
				wait = time.Until(next)
			} else if en, at := e.next(time.Now()); en != nil {
				e.start(ctx, en)
				running++
				lastStart = time.Now()
				wait = pace
			} else if !at.IsZero() {
				wait = min(wait, max(time.Until(at), 0))
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			for ; running > 0; running-- {
				<-e.done
			}
			return nil
		case o := <-e.done:
			running--
			e.finish(ctx, o)
		case <-e.wake:
		case <-timer.C:
		}
	}
}

// next returns the most urgent startable entry, or when the earliest one becomes startable.
func (e *Engine) next(now time.Time) (*entry, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var best *entry
	var at time.Time
	later := func(t time.Time) {
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	for _, en := range e.entries {
		if en.running {
			continue
		}
		if en.due.After(now) {
			later(en.due)
			continue
		}
		if p := e.pauses[en.source]; p != nil && p.until.After(now) {
			later(p.until)
			continue
		}
		if e.budget != nil {
			if frac, reset := e.budget.Remaining(en.source, en.repo.Owner()); frac < en.prio.reserve() {
				later(reset)
				continue
			}
		}
		if best == nil || en.prio < best.prio || en.prio == best.prio && (en.due.Before(best.due) || en.due.Equal(best.due) && en.key < best.key) {
			best = en
		}
	}
	if best != nil {
		best.running, best.dirty = true, false
	}
	return best, at
}

func (e *Engine) start(ctx context.Context, en *entry) {
	j := Job{Key: en.key, Source: en.source, Repo: en.repo, Prev: en.item}
	go func() {
		started := time.Now()
		jctx, cancel := context.WithTimeout(ctx, en.spec.Timeout)
		defer cancel()
		res, err := safeRun(jctx, en.spec, j)
		e.done <- outcome{e: en, res: res, err: err, started: started}
	}()
}

func safeRun(ctx context.Context, s *Spec, j Job) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = permanentError{fmt.Errorf("panic in %s: %v", j.Key, r)}
		}
	}()
	return s.Run(ctx, j)
}

type permanentError struct{ error }

func (permanentError) Permanent() bool   { return true }
func (permanentError) ErrorCode() string { return "panic" }
func (p permanentError) Unwrap() error   { return p.error }

// finish records an outcome: the store row and schedule, the in-memory entry, pauses and the snapshot.
func (e *Engine) finish(ctx context.Context, o outcome) {
	en, now := o.e, time.Now()
	if ctx.Err() != nil {
		return // shutting down: the row stays as it was
	}
	e.mu.Lock()
	en.running = false
	if e.entries[en.key] != en || !e.leader.Load() {
		e.mu.Unlock()
		return // the repository left the catalogue while the job ran, or another instance leads now
	}
	class, retryAt := classify(o.err)
	result := "error"
	switch {
	case class == classOK && o.res.Unchanged:
		result = "not_modified"
	case class == classOK:
		result = "ok"
	case class == classRate:
		result = "rate_limited"
	}
	e.observe(en.spec.Kind, result, now.Sub(o.started))
	switch class {
	case classOK:
		e.succeeded(en, o.res, now)
	case classRate, classAuth:
		if until := e.pauseSource(en.source, retryAt, class == classAuth, now); !until.IsZero() {
			e.log.Warn("source paused", "source", en.source, "until", until, "err", o.err)
		}
	case classPermanent:
		en.item = failed(en.item, o.err)
		en.due, en.backoff = now.Add(permanentRetry), true
	case classTransient:
		en.item = failed(en.item, o.err)
		en.due, en.backoff = now.Add(backoff(en.item.Attempts)), true
	}
	if en.dirty && !en.backoff {
		en.due, en.prio = now, PrioEvent
	}
	en.dirty = false
	if class == classRate || class == classAuth {
		e.mu.Unlock()
		return // not an attempt: the row is untouched and the job runs after the pause
	}
	en.item.NextDue = en.due
	it, spec := en.item, en.spec
	ch := store.Change{Item: it}
	var removed []string
	if class == classOK && o.res.Catalogue != nil {
		ch.Source, ch.Catalogue = en.source, o.res.Catalogue
		removed = e.setCatalogue(en.source, o.res.Catalogue, now)
	}
	var shared Shared
	if e.share != nil {
		en.rev = e.nextRev()
		shared = Shared{Rev: en.rev, Source: en.source, Item: it, Catalogue: ch.Catalogue, Removed: removed}
	}
	e.mu.Unlock()

	if o.err != nil {
		e.log.Warn("job failed", "key", en.key, "attempts", it.Attempts, "err", o.err)
	}
	if err := e.store.Apply(ctx, ch); err != nil {
		e.log.Error("store write failed; serving from memory", "key", en.key, "err", err)
	}
	fresh := freshness(it, spec.TTL)
	e.pub.Update(func(b *snapshot.Builder) { publish(b, spec, it, fresh, removed) })
	if e.share != nil {
		e.share(shared)
	}
}

// publish sets an item in the snapshot with its digest and drops the digests of the items that left.
func publish(b *snapshot.Builder, spec *Spec, it store.Item, fresh snapshot.Freshness, removed []string) {
	spec.Publish(b, it, fresh)
	b.Mark(it.Key, digest(it))
	for _, k := range removed {
		b.Mark(k, 0)
	}
}

// digest is the item's contribution to the snapshot version: what its publish shows, in the precision the store keeps.
func digest(it store.Item) uint64 {
	h := fnv.New64a()
	for _, s := range []string{it.Key, string(it.State), it.ErrCode, it.ContentSHA} {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(it.CheckedAt.Unix())) //nolint:gosec // G115: bits of a digest
	binary.BigEndian.PutUint64(b[8:], uint64(it.FetchedAt.Unix())) //nolint:gosec // G115: bits of a digest
	_, _ = h.Write(b[:])
	_, _ = h.Write(it.Payload)
	return max(h.Sum64(), 1)
}

// nextRev returns a revision newer than every one made or seen. Callers hold e.mu.
func (e *Engine) nextRev() int64 {
	e.rev = max(time.Now().UnixNano(), e.rev+1)
	return e.rev
}

// Receive applies an outcome the leader recorded, as if the job had run here: the store row, the schedule and the snapshot. An
// outcome older than what the engine has for its key is ignored; one for a repository not catalogued yet waits for the listing that
// admits it. It waits for the warm start.
func (e *Engine) Receive(ctx context.Context, r Shared) {
	select {
	case <-e.warm:
	case <-ctx.Done():
		return
	}
	now := time.Now()
	e.mu.Lock()
	e.rev = max(e.rev, r.Rev)
	en := e.entries[r.Item.Key]
	if en == nil {
		if _, known := e.repos[r.Item.Repo]; r.Item.Repo != "" && !known && e.spec(r.Item.Kind) != nil {
			if o, ok := e.orphans[r.Item.Key]; !ok || o.Rev < r.Rev {
				e.orphans[r.Item.Key] = r
			}
		}
		e.mu.Unlock()
		return
	}
	if r.Rev <= en.rev {
		e.mu.Unlock()
		return
	}
	en.rev, en.item = r.Rev, r.Item
	en.due, en.backoff, en.prio = r.Item.NextDue, r.Item.Attempts > 0, en.spec.Prio
	if en.due.IsZero() {
		en.due = now
	}
	ch := store.Change{Item: r.Item}
	var adopted []Shared
	var removed []string
	if r.Catalogue != nil {
		ch.Source, ch.Catalogue = r.Source, r.Catalogue
		removed = e.setCatalogue(r.Source, r.Catalogue, now)
		for k, o := range e.orphans {
			switch {
			case e.entries[k] != nil:
				adopted = append(adopted, o)
				delete(e.orphans, k)
			case o.Source == r.Source:
				delete(e.orphans, k) // the listing does not admit its repository
			}
		}
	}
	it, spec := en.item, en.spec
	e.mu.Unlock()

	if err := e.store.Apply(ctx, ch); err != nil {
		e.log.Error("store write failed; serving from memory", "key", it.Key, "err", err)
	}
	fresh := freshness(it, spec.TTL)
	e.pub.Update(func(b *snapshot.Builder) { publish(b, spec, it, fresh, removed) })
	slices.SortFunc(adopted, func(a, b Shared) int { return strings.Compare(a.Item.Key, b.Item.Key) })
	for _, o := range adopted {
		e.Receive(ctx, o)
	}
	e.poke()
}

// Records returns every recorded outcome, the listings with their catalogues first, for an instance that takes the lead and fills
// the shared state. Outcomes never shared get a new revision.
func (e *Engine) Records() []Shared {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Shared
	for _, en := range e.entries {
		if en.item.State == snapshot.StateNever {
			continue
		}
		if en.rev == 0 {
			en.rev = e.nextRev()
		}
		r := Shared{Rev: en.rev, Source: en.source, Item: en.item}
		if en.spec.Lists {
			r.Catalogue = []store.Repo{}
			for _, repo := range e.repos {
				if repo.ID.Source == en.source {
					r.Catalogue = append(r.Catalogue, repo)
				}
			}
			slices.SortFunc(r.Catalogue, func(a, b store.Repo) int { return strings.Compare(string(a.Key), string(b.Key)) })
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Shared) int {
		if (a.Catalogue != nil) != (b.Catalogue != nil) {
			if a.Catalogue != nil {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Item.Key, b.Item.Key)
	})
	return out
}

func (e *Engine) spec(kind string) *Spec {
	for _, s := range e.specs {
		if s.Kind == kind {
			return s
		}
	}
	return nil
}

type class uint8

const (
	classOK class = iota
	classTransient
	classRate
	classAuth
	classPermanent
)

func classify(err error) (class, time.Time) {
	if err == nil {
		return classOK, time.Time{}
	}
	var rl rateLimited
	if errors.As(err, &rl) {
		if t, ok := rl.RateLimitedUntil(); ok {
			return classRate, t
		}
	}
	var af authFailed
	if errors.As(err, &af) && af.AuthFailed() {
		return classAuth, time.Time{}
	}
	var pm permanent
	if errors.As(err, &pm) && pm.Permanent() {
		return classPermanent, time.Time{}
	}
	return classTransient, time.Time{}
}

// succeeded updates en after a successful run. Callers hold e.mu.
func (e *Engine) succeeded(en *entry, r Result, now time.Time) {
	it := en.item
	if !r.Unchanged {
		it.State, it.FetchedAt, it.ContentSHA, it.Payload = r.State, now, r.ContentSHA, r.Payload
		if it.State == "" {
			it.State = snapshot.StateOK
		}
	}
	it.CheckedAt, it.Attempts, it.ErrCode = now, 0, ""
	en.item = it
	en.due, en.prio, en.backoff = now.Add(withJitter(en.spec.TTL)), en.spec.Prio, false
	if p := e.pauses[en.source]; p != nil {
		p.rateStreak, p.authStreak = 0, 0
	}
}

func failed(it store.Item, err error) store.Item {
	it.Attempts++
	it.ErrCode = "error"
	var c coded
	if errors.As(err, &c) {
		it.ErrCode = c.ErrorCode()
	}
	if it.CheckedAt.IsZero() {
		it.State = snapshot.StateError
	}
	return it
}

// pauseSource stops starts for source. A pause already in force is not extended or escalated, so one rate limit seen by several
// jobs in flight pauses once. Callers hold e.mu.
func (e *Engine) pauseSource(source string, retryAt time.Time, auth bool, now time.Time) time.Time {
	p := e.pauses[source]
	if p == nil {
		p = &pause{}
		e.pauses[source] = p
	}
	if p.until.After(now) {
		return time.Time{}
	}
	switch {
	case auth:
		p.until = now.Add(min(authWaitBase<<p.authStreak, authWaitMax))
		p.authStreak++
	case !retryAt.IsZero():
		p.until = retryAt.Add(rand.N(time.Second)) //nolint:gosec // G404: stagger, not security
	default:
		p.until = now.Add(min(rateWaitBase<<p.rateStreak, rateWaitMax))
		p.rateStreak++
	}
	return p.until
}

// backoff is the wait before retry number attempts: full jitter between retryBase and the capped exponential.
func backoff(attempts int) time.Duration {
	ceil := retryMax
	if attempts < 20 {
		ceil = min(retryBase<<(attempts-1), retryMax)
	}
	if ceil <= retryBase {
		return retryBase
	}
	return retryBase + rand.N(ceil-retryBase) //nolint:gosec // G404: jitter, not security
}

func withJitter(ttl time.Duration) time.Duration {
	d := time.Duration(float64(ttl) * jitter)
	if d <= 0 {
		return ttl
	}
	return ttl - d + rand.N(2*d) //nolint:gosec // G404: jitter, not security
}

func freshness(it store.Item, ttl time.Duration) snapshot.Freshness {
	return snapshot.Freshness{State: it.State, FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, Attempts: it.Attempts,
		ErrCode: it.ErrCode, TTL: ttl}
}

// setCatalogue replaces the repositories of source, dropping the entries of those that left or changed identity and adding entries
// for new ones. It returns the keys of the dropped entries. Callers hold e.mu.
func (e *Engine) setCatalogue(source string, repos []store.Repo, now time.Time) (removed []string) {
	next := make(map[snapshot.RepoKey]store.Repo, len(repos))
	for _, r := range repos {
		next[r.Key] = r
	}
	for k, r := range e.repos {
		if n, listed := next[k]; (r.ID.Source == source || listed) && n != r {
			delete(e.repos, k)
			for key, en := range e.entries {
				if en.repo == k {
					delete(e.entries, key) // the store deletes its items with the repository
					removed = append(removed, key)
				}
			}
		}
	}
	for _, r := range repos {
		e.repos[r.Key] = r
		e.addRepoEntries(r, nil, now)
	}
	slices.Sort(removed)
	return removed
}

// addRepoEntries makes sure every per-repository spec has an entry for r. Callers hold e.mu.
func (e *Engine) addRepoEntries(r store.Repo, stored map[string]store.Item, now time.Time) {
	for _, s := range e.specs {
		if s.PerSource {
			continue
		}
		key := s.Kind + "/" + string(r.Key)
		if e.entries[key] != nil {
			continue
		}
		e.entries[key] = e.newEntry(s, key, r.ID.Source, r.Key, stored[key], now)
	}
}

func (e *Engine) newEntry(s *Spec, key, source string, repo snapshot.RepoKey, it store.Item, now time.Time) *entry {
	en := &entry{spec: s, key: key, source: source, repo: repo, item: it, due: now, prio: s.Prio}
	if it.Key == "" {
		en.item = store.Item{Key: key, Kind: s.Kind, Repo: repo, State: snapshot.StateNever}
		en.prio = PrioBackfill
		return en
	}
	if !it.NextDue.IsZero() && it.NextDue.After(now) {
		en.due = it.NextDue
		en.backoff = it.Attempts > 0
		if limit := now.Add(s.TTL); !en.backoff && en.due.After(limit) {
			en.due = limit // the TTL was shortened since the schedule was stored
		}
	}
	return en
}

// warmStart drops rows of removed sources and kinds, loads the rest, publishes it as the first snapshot and restores the schedule.
func (e *Engine) warmStart(ctx context.Context) error {
	kinds := make([]string, 0, len(e.specs))
	for _, s := range e.specs {
		kinds = append(kinds, s.Kind)
	}
	if err := e.store.RetainSources(ctx, e.sources, kinds); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	repos, items, err := e.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	stored := map[string]store.Item{}
	for _, it := range items {
		stored[it.Key] = it
	}
	now := time.Now()
	e.mu.Lock()
	for _, s := range e.specs {
		if !s.PerSource {
			continue
		}
		for _, src := range e.sources {
			key := s.Kind + "/" + src
			e.entries[key] = e.newEntry(s, key, src, "", stored[key], now)
		}
	}
	for _, r := range repos {
		e.repos[r.Key] = r
		e.addRepoEntries(r, stored, now)
	}
	e.mu.Unlock()

	e.pub.Update(func(b *snapshot.Builder) {
		for _, s := range e.specs { // registration order: the catalogue before what hangs off it
			for _, it := range items {
				if it.Kind == s.Kind {
					publish(b, s, it, freshness(it, s.TTL), nil)
				}
			}
		}
	})
	e.pub.Flush()
	close(e.warm)
	e.log.Info("warm start", "repos", len(repos), "items", len(items))
	return nil
}
