// Package sync runs the jobs that read GitHub. Every job key has one entry with a due time; the dispatcher starts the most urgent
// due entry while concurrency, pace, rate budget and source pauses allow. A result is written to the store in one transaction and
// handed to the job kind's Publish, which is also how the warm start rebuilds the snapshot from the store.
package sync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

const (
	pace           = 100 * time.Millisecond // between job starts
	jitter         = 0.1                    // of the TTL, both ways
	retryBase      = 2 * time.Second
	retryMax       = 15 * time.Minute
	permanentRetry = 24 * time.Hour
	waitRetry      = 5 * time.Minute // a job waiting for a credential (Waiting): at the latest then, at once on an event
	rateWaitBase   = time.Minute     // secondary limit without Retry-After; doubles to rateWaitMax
	rateWaitMax    = 15 * time.Minute
	authWaitBase   = 5 * time.Minute
	authWaitMax    = time.Hour
	idleWake       = time.Minute
	defaultTimeout = 2 * time.Minute
	metaRelease    = "release" // the store's meta key of the release that last ran on it
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
	EventOpen          Event = "open"   // the count of open issues and pull requests in a listing changed (the safety net when no webhook came)
	EventTag           Event = "tag"    // a tag was pushed or deleted, or a release published
	EventBranch        Event = "branch" // a branch other than the default one was pushed or deleted: only the history listens
)

// Job is what a Spec's Run receives.
type Job struct {
	Key    string
	Source string           // configured source id
	Repo   snapshot.RepoKey // empty for per-source jobs
	Prev   store.Item       // the last stored outcome; State never when there is none
	// Event: the run was asked for by an event (a webhook), not by its time. What GitHub lists a few seconds after an event may not
	// show it yet: such a job asks to run again soon (Result.Again) to confirm.
	Event bool
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
	// Again, when shorter than the TTL, runs the job again after it: a result that wants confirming soon.
	Again time.Duration
}

// Spec is one kind of job.
type Spec struct {
	Kind      string
	PerSource bool // keyed "<kind>/<source>"; otherwise "<kind>/<owner>/<name>" for every catalogued repository
	Lists     bool // per-source: results carry the source's catalogue
	Prio      Priority
	TTL       time.Duration
	// PollTTL, when set, replaces TTL for the repositories of a source nothing tells about changes (Engine.SetPolling): TTL is then
	// only a net under events, and a source without events needs the shorter poll. Zero: the same TTL everywhere.
	PollTTL time.Duration
	Timeout time.Duration // default 2m
	On      []Event
	// Settle is the least time from one run's start to the next that an event may cause; 0: none. A job whose events anyone can
	// send (a comment on a public repository's issue) settles, so a stream of them costs one run a Settle, not one each.
	Settle time.Duration
	Run    func(ctx context.Context, j Job) (Result, error)
	// Publish puts a stored item into the snapshot. It runs for live results and at the warm start, in registration order.
	Publish func(b *snapshot.Builder, it store.Item, fresh snapshot.Freshness)
}

// Shared is one recorded outcome as instances share it (internal/platform/cluster): the item with its schedule, for listing jobs the source's
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
	waiting     interface{ Waiting() bool } // nothing to read it with yet (scp enterprise: no person's token): not an attempt
	coded       interface{ ErrorCode() string }
)

// Engine runs the registered specs.
type Engine struct {
	store   *store.DB
	pub     *snapshot.Publisher
	budget  Budget
	sources []string
	n       int    // concurrency
	release string // the running release, for the warm start
	log     *slog.Logger

	specs   []*Spec
	polling map[string]bool // sources that get no events (SetPolling); read-only once running
	wake    chan struct{}
	done    chan outcome

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
	started time.Time // when the last run started
	dirty   bool      // an event arrived while running: run once more
	rev     int64     // revision of item when it was shared or received
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

// SetPolling names the sources that get no events (no receiver, no delivery log): their jobs run at their spec's PollTTL where it has
// one. Call it before Run; the engine only reads it.
func (e *Engine) SetPolling(sources ...string) {
	e.polling = make(map[string]bool, len(sources))
	for _, s := range sources {
		e.polling[s] = true
	}
}

// TTLOf is how long a result of spec's kind for source is fresh: the spec's TTL, or its PollTTL for a source that is polled.
func (e *Engine) TTLOf(s *Spec, source string) time.Duration {
	if s.PollTTL > 0 && e.polling[source] {
		return s.PollTTL
	}
	return s.TTL
}

// Cadence is each registered kind's TTL for a source, by kind name.
func (e *Engine) Cadence(source string) map[string]time.Duration {
	out := make(map[string]time.Duration, len(e.specs))
	for _, s := range e.specs {
		out[s.Kind] = e.TTLOf(s, source)
	}
	return out
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

// Release names the release the engine runs, before Run. When it differs from the release that last ran on the store, the warm start
// runs every failed job again at once: a failure the new release may have fixed is not left to its backoff (up to a day).
func (e *Engine) Release(v string) { e.release = v }

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

func (e *Engine) spec(kind string) *Spec {
	for _, s := range e.specs {
		if s.Kind == kind {
			return s
		}
	}
	return nil
}
