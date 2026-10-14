package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// SourceDeps describes one configured source as catalog needs it, in configuration order.
type SourceDeps struct {
	ID     scm.SourceID
	Policy DiscoveryPolicy
	Host   HostClass
}

// Deps are the constructor arguments of New; every field is required except where noted.
type Deps struct {
	Config    Config
	Sources   []SourceDeps
	Lister    RepositoryLister
	Prober    HeadProber
	Store     CatalogueStore
	Publisher *readmodel.Publisher
	Slots     Slots
	Tracker   Tracker
	Events    EventSink
	Log       *slog.Logger
	Meter     metric.Meter
	Tracer    trace.Tracer
	Seed      uint64 // jitter of the per-source due check; tests pass a constant
}

// Service owns the catalogue. It starts no goroutine: every operation runs on an engine worker (jobs), a webhook goroutine
// (React) or a request goroutine (handlers).
type Service struct {
	d        Deps
	compiled *Compiled
	m        metrics

	commitMu sync.Mutex // serialises reconcile commits (held across the store write and the publish)
	pubMu    sync.Mutex // serialises every change of tracked + snapshot (commit's in-memory phase, hide, status publishes); never held across I/O
	mu       sync.Mutex // guards the fields below; held only for in-memory work, never across I/O
	tracked  []Tracked
	hiddenAt map[RepoKey]hideHint // repositories hidden or demoted by an event; see commit
	stats    map[RepoKey]scm.RepoStats
	sources  map[scm.SourceID]*sourceState
	headPos  int           // rotating start of the head sweep
	hideGen  uint64        // incremented by every hide: lets commit notice that an event arrived while it was writing the store
	groups   []GroupHealth // health of the groups as of the last published membership (input of status-only publishes)

	lastUnknownReconcile atomic.Int64 // unix nanoseconds of the last reconcile triggered by an event for an unknown repository
}

// hideHint remembers an event that changed what the portal may show before a listing confirmed it.
type hideHint struct {
	at     time.Time
	demote bool // privatized under installation_scope: the repository stays, as private; otherwise it left the portal
}

// admitsPrivate reports whether the source's policy keeps private repositories (installation_scope).
func (s *Service) admitsPrivate(id scm.SourceID) bool {
	src, ok := s.source(id)
	return ok && src.Policy.Visibility == VisibilityInstallationScope
}

type pageEntry struct {
	etag string
	page scm.Page[scm.Repository]
}

type sourceState struct {
	warmAt      time.Time            // newest SeenAt restored by Warm (display only; never makes the source not-due)
	wanted      bool                 // an event asked for this source to be listed now (cleared when a listing starts)
	pages       map[string]pageEntry // cursor -> last page and its validators (memory only: validators are not persisted)
	lastAttempt time.Time
	lastSuccess time.Time
	state       string
	errCode     string
	listed      int
	excluded    map[Exclusion]int
	held        *Held
	heldCount   int
	quarantined []Quarantine
	findings    []Finding // R301/R302 of this source's last complete plan
}

// New validates the configuration (a defect here is a programming error: cmd/scp already refused startup on findings) and builds the service.
func New(d Deps) (*Service, error) {
	if HasErrors(d.Config.Validate(ValidateContext{Sources: sourceIDs(d.Sources)})) {
		return nil, errors.New("catalog: invalid configuration (run Config.Validate first)")
	}
	c, err := Compile(d.Config)
	if err != nil {
		return nil, err
	}
	s := &Service{d: d, compiled: c, stats: map[RepoKey]scm.RepoStats{}, hiddenAt: map[RepoKey]hideHint{}, sources: map[scm.SourceID]*sourceState{}}
	for _, src := range d.Sources {
		s.sources[src.ID] = &sourceState{pages: map[string]pageEntry{}, state: "never"}
	}
	if s.m, err = newMetrics(d.Meter); err != nil {
		return nil, err
	}
	return s, nil
}

func sourceIDs(ss []SourceDeps) []scm.SourceID {
	out := make([]scm.SourceID, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}

func (s *Service) source(id scm.SourceID) (SourceDeps, bool) {
	for _, x := range s.d.Sources {
		if x.ID == id {
			return x, true
		}
	}
	return SourceDeps{}, false
}

// prefix implements the slug prefix lookup: "source/owner" first, then "source".
func (s *Service) prefix(src scm.SourceID, owner string) string {
	if p, ok := s.d.Config.SlugPrefixBySource[string(src)+"/"+owner]; ok {
		return p
	}
	return s.d.Config.SlugPrefixBySource[string(src)]
}

// reconcileTTL is the planner TTL of catalog.reconcile: the smallest reconcile_every; per-source due checks skip sources that are not yet due.
func (s *Service) reconcileTTL() time.Duration {
	ttl := 30 * time.Minute
	for i, src := range s.d.Sources {
		if i == 0 || src.Policy.ReconcileEvery < ttl {
			ttl = src.Policy.ReconcileEvery
		}
	}
	return ttl
}

// Jobs returns the three job specs of the feature (section 8). The job timeouts are deliberately above the upstream default.
func (s *Service) Jobs() []job.Spec {
	h := s.d.Config.Heads
	return []job.Spec{
		{Kind: JobReconcile, Upstream: "github", Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
			Policy: freshness.Policy{TTL: s.reconcileTTL(), Jitter: 0.1}, Timeout: 5 * time.Minute, Run: s.runReconcile},
		{Kind: JobHeads, Upstream: "github", Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
			Policy: freshness.Policy{TTL: h.Interval, Jitter: 0.1}, Timeout: 10 * time.Minute, Run: s.runHeads},
		{Kind: JobStats, Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
			Policy: freshness.Policy{TTL: h.ArchivedInterval, Jitter: 0.1, IdleBackoffMax: h.ArchivedInterval}, Timeout: 2 * time.Minute, Run: s.runStats},
	}
}

// Mount registers the four read-only API routes (section 7). Handlers are specified there; the scratch module does not implement them.
func (s *Service) Mount(m httpapi.Mux) {
	// m.HandleFunc("GET /api/v1/groups", ...); "GET /api/v1/groups/{groupId}"; "GET /api/v1/groups/{groupId}/repos"; "GET /api/v1/repos"
}

var _ httpapi.Mounter = (*Service)(nil)

// want marks a source (every source when src is empty) as due regardless of reconcile_every: an authenticated event said the
// repository set changed. It is cleared when the next listing of that source starts, so an event that arrives during a listing is not lost.
func (s *Service) want(src scm.SourceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.sources {
		if src == "" || id == src {
			st.wanted = true
		}
	}
}

// goneError tells the engine that the subject of a job no longer exists: drop the key, no retry, no parking.
type goneError struct{ what string }

func (e goneError) Error() string { return "gone: " + e.what }
func (e goneError) Gone() bool    { return true }

// permanentError is returned for ErrInvalidSnapshot: retrying would rewrite the same row.
type permanentError struct{ err error }

func (e permanentError) Error() string   { return e.err.Error() }
func (e permanentError) Unwrap() error   { return e.err }
func (e permanentError) Permanent() bool { return true }

// errCode maps an error to the stable code stored in SourceStatus.ErrCode and metrics.
func errCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case job.IsGone(err):
		return "gone"
	case job.IsPermanent(err):
		return "permanent"
	}
	if _, rl := job.AsRateLimited(err); rl {
		return "rate_limited"
	}
	return "transient"
}

func lowerEq(a, b string) bool { return strings.EqualFold(a, b) }

// lookupRef finds a tracked repository the way scm.Membership.SlugFor does: by (source, id) when the event carries a non-zero id (so an
// event for a repository renamed or transferred since the last listing still resolves), otherwise by the folded owner/name.
func (s *Service) lookupRef(ref scm.RepoRef) (Tracked, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tracked {
		r := t.Repo.Ref
		if r.Source != ref.Source {
			continue
		}
		if ref.ID != 0 && r.ID == ref.ID {
			return t, true
		}
	}
	for _, t := range s.tracked {
		r := t.Repo.Ref
		if r.Source == ref.Source && lowerEq(r.Owner, ref.Owner) && lowerEq(r.Name, ref.Name) {
			return t, true
		}
	}
	return Tracked{}, false
}

// Repos returns the references of every tracked repository (sorted by folded slug). cmd/scp passes it to the planner bridge after Warm: at warm
// start (startup step 5) the planner does not exist yet, so Warm cannot call Tracker itself.
func (s *Service) Repos() []scm.RepoRef { return s.refs() }

func (s *Service) refs() []scm.RepoRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]scm.RepoRef, len(s.tracked))
	for i, t := range s.tracked {
		out[i] = t.Repo.Ref
	}
	return out
}

var _ = slices.Clone[[]int]
var _ = fmt.Sprintf
