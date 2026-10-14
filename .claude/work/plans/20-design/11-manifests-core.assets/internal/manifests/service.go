package manifests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// Job kinds, store item kind and cache namespaces owned by this package.
const (
	KindRefresh = "manifests.refresh"
	KindGraph   = "manifests.graph"
	ItemKind    = "manifests.facts"
	NSBlob      = "manifests.blob"
	NSParse     = "manifests.parse"
	NSLocked    = "manifests.locked"
)

// Store is the carve-out of store.Store this feature uses (satisfied by store/sqlite directly, by a hand-written fake in tests).
type Store interface {
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
	GetCache(ctx context.Context, ns, key string) ([]byte, bool, error)
	PutCache(ctx context.Context, ns, key string, data []byte) error
}

// Slots are the read-model handles this feature uses. Facts and Graph are registered by RegisterSlots; the others by their owners.
type Slots struct {
	Repos      readmodel.Slot[freshness.Value[scm.Repository]]
	Stats      readmodel.Slot[freshness.Value[scm.RepoStats]]
	Facts      readmodel.Slot[freshness.Value[RepoFacts]]
	Graph      readmodel.Global[scm.Classed[*Graph]]
	Membership readmodel.Global[scm.Membership]
}

// RegisterSlots registers the slot and global this feature writes ("manifests.repo", "manifests.graph"); called by cmd/scp before Freeze.
func RegisterSlots(reg *readmodel.Registry) (readmodel.Slot[freshness.Value[RepoFacts]], readmodel.Global[scm.Classed[*Graph]]) {
	return readmodel.Register[freshness.Value[RepoFacts]](reg, "manifests.repo"), readmodel.RegisterGlobal[scm.Classed[*Graph]](reg, "manifests.graph")
}

// Deps are the collaborators of Service; everything is injected (no globals, G3/G11).
type Deps struct {
	Registry  *Registry // frozen
	Trees     TreeReader
	Blobs     BlobReader
	BlobBatch BlobBatchReader // optional
	Store     Store
	Publisher *readmodel.Publisher
	Slots     Slots
	Log       *slog.Logger
	Meter     metric.Meter
	Tracer    trace.Tracer
	Authz     authz.Authorizer // nil means authz.PublicOnly{}
	Version   string           // application version, recorded in the SBOM tool component
	Private   bool             // access mode on: text outputs use Cache-Control private
	PublicURL string           // absolute base URL for links in text outputs
}

// Service owns the manifests feature at runtime. It starts no goroutine of its own; every goroutine it creates lives inside one job run.
type Service struct {
	cfg       Config
	d         Deps
	m         *metrics
	idx       refIndex
	wd        *limits.Watchdog
	parseConc int
	enq       atomic.Pointer[job.Enqueuer]
	graphN    atomic.Uint64
	lastFP    atomic.Pointer[string]

	mu         sync.Mutex
	cleanOID   map[store.RepoKey]string // push events that provably touched no manifest: repo -> tree oid known to be manifest-equivalent
	lockMiss   map[store.RepoKey]struct{}
	recheck    map[store.RepoKey]struct{}  // a push or create touched this repository: the catalogue's HeadTreeOID may be stale, so the next run must read the tree
	lockMissAt map[store.RepoKey]time.Time // last forced re-parse request per repository (cooldown)
}

// New validates cfg and dependencies and builds the Service. It does not touch the network, the store or the snapshot.
func New(cfg Config, d Deps) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch {
	case d.Registry == nil, d.Trees == nil, d.Blobs == nil, d.Store == nil, d.Publisher == nil, d.Log == nil, d.Meter == nil, d.Tracer == nil:
		return nil, errors.New("manifests: incomplete Deps")
	}
	if d.Authz == nil { // fail closed: the public class (authz.PublicOnly), never "serve everything"
		d.Authz = authz.PublicOnly{}
	}
	m, err := newMetrics(d.Meter)
	if err != nil {
		return nil, fmt.Errorf("manifests: metrics: %w", err)
	}
	conc := cfg.Limits.ParseConcurrency
	if conc <= 0 {
		conc = min(runtime.NumCPU(), 4)
	}
	wd := limits.NewWatchdog(limits.NewBudget(int64(cfg.Limits.ParseMemory)))
	wd.Grace, wd.Trip = cfg.Limits.WatchdogGrace, cfg.Limits.AbandonTrip
	wd.OnAbandon = func(id string) { m.inc(m.abandoned, "parser", id) }
	s := &Service{cfg: cfg, d: d, m: m, wd: wd, parseConc: conc, cleanOID: map[store.RepoKey]string{}, lockMiss: map[store.RepoKey]struct{}{}, recheck: map[store.RepoKey]struct{}{}, lockMissAt: map[store.RepoKey]time.Time{}}
	if err := s.registerGauges(); err != nil {
		return nil, fmt.Errorf("manifests: gauges: %w", err)
	}
	return s, nil
}

// Bind hands the service the engine's Enqueuer (built after the specs it registers). Exactly once, before the engine runs.
func (s *Service) Bind(enq job.Enqueuer) {
	if !s.enq.CompareAndSwap(nil, &enq) {
		panic("manifests: Bind called twice")
	}
}

// Drain waits for abandoned parser goroutines (shutdown, goleak in tests).
func (s *Service) Drain(ctx context.Context) error { return s.wd.Wait(ctx) }

// Jobs returns the two job specs registered with refresh.Engine and tracked by the Planner.
func (s *Service) Jobs() []job.Spec {
	r := s.cfg.Refresh
	return []job.Spec{
		// Prio is the steady-state priority; the planner raises a key that never completed to PrioBackfill itself (unit 04, cold start).
		{Kind: KindRefresh, Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
			Policy:  freshness.Policy{TTL: r.TTL, Jitter: r.Jitter, IdleBackoffMax: r.IdleBackoffMax},
			Timeout: r.JobTimeout, Run: s.runRefresh},
		{Kind: KindGraph, Upstream: "local", Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
			Policy:  freshness.Policy{TTL: s.cfg.Graph.TTL, Jitter: 0.1},
			Timeout: time.Minute, Run: s.runGraph},
	}
}

func graphKey() job.Key { return job.GlobalKey(KindGraph) }

func refreshKey(r scm.RepoRef) job.Key { return job.NewKey(KindRefresh, r.KeyParts()...) }

// Guard returns the privacy guard built on the strictest graph variant (all repositories, public and private), so a coordinate produced by a
// private repository is never sent to an external provider (RR-14). IsPrivate = !Guard().Allow(id). Safe for concurrent use.
func (s *Service) Guard() PrivacyGuard {
	var internal func(ComponentID) bool
	if g, ok := readmodel.GetGlobal(s.d.Publisher.Current(), s.d.Slots.Graph); ok {
		internal = InternalFunc(g.All)
	}
	return NewPrivacyGuard(s.cfg.PomSource, internal)
}
