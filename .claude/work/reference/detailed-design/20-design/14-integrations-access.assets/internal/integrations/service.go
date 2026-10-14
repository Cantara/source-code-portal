package integrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// Provider names and item/job kinds.
const (
	ProviderJenkins = "jenkins"
	ProviderSnyk    = "snyk"
	KindPrefix      = "integrations." // job kind and store item kind: integrations.jenkins, integrations.snyk
)

// Metric names (G11: constants next to their instruments).
const (
	MetricStatus      = "scp_integrations_status"
	MetricFetch       = "scp_integrations_fetch_total"
	MetricFetchSecs   = "scp_integrations_fetch_seconds"
	MetricSweepSecs   = "scp_integrations_sweep_seconds"
	MetricTargets     = "scp_integrations_targets"
	MetricEnabled     = "scp_integrations_enabled"
	MetricLastSuccess = "scp_integrations_last_success_timestamp_seconds"
)

// ItemStore is the carve-out of store.Store this feature writes through.
type ItemStore interface {
	PutItem(ctx context.Context, it store.Item) error
	Revalidated(ctx context.Context, k store.ItemKey, now time.Time) error
	RecordFailure(ctx context.Context, k store.ItemKey, f store.Failure) error
}

// Deps are injected by cmd/scp.
type Deps struct {
	Logger     *slog.Logger
	Meter      metric.Meter
	Tracer     trace.Tracer
	Publisher  *readmodel.Publisher
	Repos      readmodel.Slot[freshness.Value[scm.Repository]] // catalog.repo
	Membership readmodel.Global[scm.Membership]                // catalog.membership
	Status     readmodel.Slot[Statuses]                        // integrations.status, registered by cmd/scp
	Overrides  Overrides                                       // bridged over the docs descriptor; may be nil
	Store      ItemStore
	Providers  []StatusProvider // exactly the enabled providers
}

// Service owns target resolution, the two sweep jobs and the status slot.
type Service struct {
	cfg       Config
	d         Deps
	log       *slog.Logger
	providers map[string]StatusProvider

	fetchTotal  metric.Int64Counter
	fetchSecs   metric.Float64Histogram
	sweepSecs   metric.Float64Histogram
	stateGauge  metric.Int64Gauge
	targetGauge metric.Int64Gauge
	enabled     metric.Int64Gauge
	lastOK      metric.Int64Gauge

	mu     sync.Mutex
	health map[string]*Health
	nf     map[scm.RepoRef]string // instance|job of the last 404, so a corrected descriptor is probed at once (guarded by mu)
	now    func() time.Time       // time.Now in production; synctest virtualises it, so no Clock type
}

// Health is the /status and metrics view of one provider.
type Health struct {
	Provider    string
	Enabled     bool
	Targets     int
	Invalid     int
	LastSuccess time.Time
	LastError   string // stable code of the sweep-level error, else of an aborted instance, else of a sweep in which every probed target failed; "" when healthy
	LastSweep   time.Time

	Probed              int    // targets asked in the last sweep (carried 404s are not probed)
	Failed              int    // of those, targets whose result was a failure (stale-while-error)
	FailedCode          string // most frequent failure code among Failed
	ConsecutiveFailures int    // sweeps in a row with LastError != ""; a successful sweep resets it
}

// New validates the wiring: the set of providers must equal the set the configuration enables.
func New(cfg Config, d Deps) (*Service, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if d.Publisher == nil {
		return nil, errors.New("integrations: Deps.Publisher is required")
	}
	s := &Service{cfg: cfg, d: d, log: d.Logger, providers: map[string]StatusProvider{}, health: map[string]*Health{}, nf: map[scm.RepoRef]string{}, now: time.Now}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	for _, p := range d.Providers {
		if _, dup := s.providers[p.Name()]; dup {
			return nil, fmt.Errorf("integrations: provider %q registered twice", p.Name())
		}
		s.providers[p.Name()] = p
	}
	want := map[string]bool{ProviderJenkins: cfg.JenkinsEnabled(), ProviderSnyk: cfg.SnykEnabled()}
	for name, on := range want {
		if _, have := s.providers[name]; have != on {
			return nil, fmt.Errorf("integrations: provider %q enabled=%v in configuration but registered=%v", name, on, have)
		}
		s.health[name] = &Health{Provider: name, Enabled: on}
	}
	for name := range s.providers {
		if _, known := want[name]; !known {
			return nil, fmt.Errorf("integrations: unknown provider %q", name)
		}
	}
	if d.Tracer == nil {
		s.d.Tracer = noop.NewTracerProvider().Tracer("integrations")
	}
	if d.Meter != nil {
		if err := s.instruments(d.Meter); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) instruments(m metric.Meter) (err error) {
	if s.fetchTotal, err = m.Int64Counter(MetricFetch, metric.WithUnit("{request}"), metric.WithDescription("Per-target provider fetches by outcome.")); err != nil {
		return
	}
	if s.fetchSecs, err = m.Float64Histogram(MetricFetchSecs, metric.WithUnit("s"), metric.WithDescription("Latency of one provider fetch.")); err != nil {
		return
	}
	if s.sweepSecs, err = m.Float64Histogram(MetricSweepSecs, metric.WithUnit("s"), metric.WithDescription("Duration of one sweep job.")); err != nil {
		return
	}
	if s.stateGauge, err = m.Int64Gauge(MetricStatus, metric.WithUnit("{repository}"), metric.WithDescription("Repositories by provider and status state after the last sweep.")); err != nil {
		return
	}
	if s.targetGauge, err = m.Int64Gauge(MetricTargets, metric.WithUnit("{repository}"), metric.WithDescription("Targets by provider and result (resolved, invalid, no_data).")); err != nil {
		return
	}
	if s.enabled, err = m.Int64Gauge(MetricEnabled, metric.WithDescription("1 when the provider is configured.")); err != nil {
		return
	}
	s.lastOK, err = m.Int64Gauge(MetricLastSuccess, metric.WithUnit("s"), metric.WithDescription("Unix time of the last fully successful sweep."))
	for name, h := range s.health {
		v := int64(0)
		if h.Enabled {
			v = 1
		}
		s.enabled.Record(context.Background(), v, metric.WithAttributes(attribute.String("provider", name)))
	}
	return
}

// Jobs returns one global sweep spec per enabled provider (mechanism C). Keys are "integrations.<provider>/_".
func (s *Service) Jobs() []job.Spec {
	var out []job.Spec
	add := func(name string, ttl, timeout time.Duration) {
		out = append(out, job.Spec{
			Kind: KindPrefix + name, Upstream: name, Scope: job.ScopeGlobal, Prio: job.PrioScheduled,
			Policy:  freshness.Policy{TTL: ttl, Jitter: 0.1},
			Timeout: timeout,
			Run:     func(ctx context.Context, _ job.Key) (job.Outcome, error) { return s.Sweep(ctx, name) },
		})
	}
	if s.cfg.JenkinsEnabled() {
		add(ProviderJenkins, s.cfg.Jenkins.TTL, s.cfg.Jenkins.SweepTimeout)
	}
	if s.cfg.SnykEnabled() {
		add(ProviderSnyk, s.cfg.Snyk.TTL, s.cfg.Snyk.SweepTimeout)
	}
	return out
}

// Providers lists the enabled provider names in a stable order. overview and the group API use it to decide
// which badge kinds (build, security) and status columns exist at all.
func (s *Service) Providers() []string {
	var out []string
	for _, n := range []string{ProviderJenkins, ProviderSnyk} {
		if _, ok := s.providers[n]; ok {
			out = append(out, n)
		}
	}
	return out
}

// Health returns a copy of the per-provider health for /status (bridged to ops).
func (s *Service) Health() []Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Health
	for _, n := range []string{ProviderJenkins, ProviderSnyk} {
		out = append(out, *s.health[n])
	}
	return out
}

// StatusOf is the read API for overview and the badges bridge. It always returns a value: Disabled for a provider that
// is not configured, StateUnknown with freshness "never" for a repository that has no entry yet.
func (s *Service) StatusOf(snap *readmodel.Snapshot, slug scm.Slug, provider string) freshness.Value[Status] {
	if _, on := s.providers[provider]; !on {
		return freshness.Value[Status]{V: Disabled(provider), Info: freshness.Info{State: freshness.StateNever}}
	}
	if m, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Status); ok {
		if v, ok := m[provider]; ok {
			return v
		}
	}
	return freshness.Value[Status]{V: Status{Provider: provider, State: StateUnknown}, Info: freshness.Info{State: freshness.StateNever}}
}

// FactFor is StatusOf reduced to what a badge needs; ok is false when the provider is disabled (the badge kind is not offered).
func (s *Service) FactFor(snap *readmodel.Snapshot, slug scm.Slug, provider string, now time.Time) (Fact, bool) {
	if _, on := s.providers[provider]; !on {
		return Fact{}, false
	}
	return FactOf(s.StatusOf(snap, slug, provider), now), true
}

// WarmItem rebuilds one slot entry from a stored row at startup. The caller (cmd/scp warm load) resolves the slug from
// the store.RepoKey and has already put the repository's existence slot. Corrupt rows are skipped, never fatal.
func (s *Service) WarmItem(b *readmodel.Builder, id readmodel.EntryID, it store.Item) error {
	name, ok := cutPrefix(it.Key.Kind, KindPrefix)
	if !ok {
		return nil
	}
	if _, on := s.providers[name]; !on {
		return nil // provider disabled since the row was written: ignore
	}
	v := freshness.Value[Status]{Info: freshness.Info{State: it.State, FetchedAt: it.FetchedAt, CheckedAt: it.CheckedAt, Attempts: it.Attempts, ErrCode: it.Error, ContentSHA: it.ContentSHA, TTL: s.ttl(name)}}
	if len(it.Payload) > 0 {
		st, err := decodeStatus(name, it.Payload)
		if err != nil {
			s.log.Warn("integrations: dropping unreadable stored status", "provider", name, "err", err)
			return nil
		}
		v.V = st
	} else {
		v.V = Status{Provider: name}
	}
	cur, _ := readmodel.Get(b.View(), id, s.d.Status)
	next := maps.Clone(cur)
	if next == nil {
		next = Statuses{}
	}
	next[name] = v
	if err := readmodel.Put(b, id, s.d.Status, next); err != nil && !errors.Is(err, readmodel.ErrUnknownRepo) {
		return err
	}
	return nil
}

func cutPrefix(s, p string) (string, bool) {
	if len(s) > len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return "", false
}

func (s *Service) ttl(name string) time.Duration {
	if name == ProviderJenkins {
		return s.cfg.Jenkins.TTL
	}
	return s.cfg.Snyk.TTL
}

// ---- sweep ----

type update struct {
	slug    readmodel.EntryID
	ref     scm.RepoRef
	val     freshness.Value[Status]
	remove  bool
	persist persistAction
}

type persistAction uint8

const (
	persistNone persistAction = iota
	persistPut
	persistRevalidate
	persistFailure
)

// Sweep runs one provider sweep: resolve targets, fetch, reduce against the previous values (stale-while-error),
// persist (outside the publish lock), publish once, record metrics. It is the body of the job.Spec.
func (s *Service) Sweep(ctx context.Context, name string) (job.Outcome, error) {
	p, ok := s.providers[name]
	if !ok {
		return job.Outcome{}, &PermanentError{Provider: name, Code: "config", Err: errors.New("provider not enabled")}
	}
	ctx, span := s.d.Tracer.Start(ctx, "integrations.sweep", trace.WithAttributes(attribute.String("provider", name)))
	defer span.End()
	start := s.now()
	snap := s.d.Publisher.Current()
	mem, _ := readmodel.GetGlobal(snap, s.d.Membership)

	var targets []Target
	var invalid []InvalidTarget
	if name == ProviderJenkins {
		targets, invalid = ResolveJenkins(s.cfg.Jenkins, snap, s.d.Repos, mem, s.d.Overrides)
	} else {
		targets, invalid = ResolveSnyk(snap, s.d.Repos, mem, s.d.Overrides)
	}
	for _, iv := range invalid {
		s.log.WarnContext(ctx, "integrations: invalid target", "provider", name, "repo", string(iv.Slug), "reason", iv.Reason)
	}

	slugOf := map[scm.RepoRef]scm.Slug{}
	for _, id := range snap.Repos() {
		if rv, ok := readmodel.Get(snap, id, s.d.Repos); ok {
			slugOf[rv.V.Ref] = rv.V.Slug
		}
	}

	// A target whose job was 404 within NotFoundTTL is carried, not probed again (no blind-probe storm).
	probe := make([]Target, 0, len(targets))
	carry := map[scm.RepoRef]bool{}
	for _, t := range targets {
		if name == ProviderJenkins && s.carryNotFound(snap, slugOf[t.Repo], name, start, t) {
			carry[t.Repo] = true
			continue
		}
		probe = append(probe, t)
	}

	fo := s.fetchAll(ctx, p, probe)
	results, sweepErr, partial := fo.results, fo.sweepErr, fo.partial
	now := s.now()
	if name == ProviderJenkins {
		s.mu.Lock()
		for i, t := range probe {
			if errors.Is(results[i].Err, scm.ErrNotFound) {
				s.nf[t.Repo] = t.Instance + "|" + t.Job
			} else {
				delete(s.nf, t.Repo)
			}
		}
		s.mu.Unlock()
	}

	ups := make([]update, 0, len(targets))
	inTargets := map[readmodel.EntryID]bool{}
	for _, t := range targets {
		id := readmodel.EntryID(slugOf[t.Repo])
		inTargets[id] = true
	}
	for i, t := range probe {
		id := readmodel.EntryID(slugOf[t.Repo])
		prev, _ := s.prev(snap, id, name)
		ups = append(ups, s.reduce(name, id, t.Repo, prev, results[i], now))
	}
	// Repositories that lost their target (descriptor edited, group default removed, repository archived).
	for _, id := range snap.Repos() {
		if inTargets[id] {
			continue
		}
		if m, ok := readmodel.Get(snap, id, s.d.Status); ok {
			if _, has := m[name]; has {
				ups = append(ups, update{slug: id, remove: true})
			}
		}
	}

	// Finalisation uses a detached context: a sweep cut short by its deadline still stores and publishes what it learned.
	fctx, cancelF := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelF()
	s.persist(fctx, name, ups)
	changed, perr := s.publish(name, ups)
	if perr != nil {
		return job.Outcome{}, &PermanentError{Provider: name, Code: "invalid_snapshot", Err: perr}
	}
	s.record(ctx, name, start, now, targets, invalid, ups, len(probe), sweepErr, partial)
	span.SetAttributes(attribute.Int("targets", len(targets)), attribute.Bool("changed", changed))
	return job.Outcome{Unchanged: !changed}, sweepErr
}

func (s *Service) prev(snap *readmodel.Snapshot, id readmodel.EntryID, name string) (freshness.Value[Status], bool) {
	m, ok := readmodel.Get(snap, id, s.d.Status)
	if !ok {
		return freshness.Value[Status]{}, false
	}
	v, ok := m[name]
	return v, ok
}

// carryNotFound: a 404 job is not probed again within NotFoundTTL, unless the descriptor or group default now points
// at a different instance/job than the one that answered 404 (known only for 404s seen by this process; after a
// restart the stored 404 is trusted, so a correction is picked up within NotFoundTTL).
func (s *Service) carryNotFound(snap *readmodel.Snapshot, slug scm.Slug, name string, now time.Time, t Target) bool {
	v, ok := s.prev(snap, readmodel.EntryID(slug), name)
	if !ok || v.Info.State != freshness.StateNotFound || now.Sub(v.Info.FetchedAt) >= s.cfg.Jenkins.NotFoundTTL {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if was, known := s.nf[t.Repo]; known && was != t.Instance+"|"+t.Job {
		return false
	}
	return true
}

// reduce turns one fetch result into the next stored value. Failure never replaces a good value (stale-while-error).
func (s *Service) reduce(name string, id readmodel.EntryID, ref scm.RepoRef, prev freshness.Value[Status], r Result, now time.Time) update {
	ttl := s.ttl(name)
	u := update{slug: id, ref: ref}
	switch {
	case r.Err == nil:
		st := r.Status
		st.Provider, st.ObservedAt = name, now
		st.Summary = CapSummary(st.Summary)
		_, sha := encodeStatus(st)
		u.val = freshness.Value[Status]{V: st, Info: freshness.Info{State: freshness.StateOK, FetchedAt: now, CheckedAt: now, ContentSHA: sha, TTL: ttl}}
		u.persist = persistPut
		if prev.Info.State == freshness.StateOK && prev.Info.ContentSHA == sha {
			u.persist = persistRevalidate
		}
	case errors.Is(r.Err, scm.ErrNotFound):
		state := freshness.StateNotFound
		if errors.Is(r.Err, ErrNoData) {
			state = freshness.StateNoContent
		}
		st := Status{Provider: name, State: StateUnknown, Summary: "no data", ObservedAt: now}
		_, sha := encodeStatus(st)
		u.val = freshness.Value[Status]{V: st, Info: freshness.Info{State: state, FetchedAt: now, CheckedAt: now, ContentSHA: sha, TTL: ttl}}
		u.persist = persistPut
		if prev.Info.State == state && prev.Info.ContentSHA == sha {
			u.persist = persistRevalidate
		}
	default:
		v := prev
		if v.V.Provider == "" {
			v.V = Status{Provider: name, State: StateUnknown}
		}
		v.Info.State = freshness.StateError
		v.Info.CheckedAt = now
		v.Info.Attempts = prev.Info.Attempts + 1
		v.Info.ErrCode = ErrCode(r.Err)
		v.Info.TTL = ttl
		u.val = v
		u.persist = persistFailure
	}
	return u
}

func (s *Service) persist(ctx context.Context, name string, ups []update) {
	if s.d.Store == nil {
		return
	}
	kind := KindPrefix + name
	for _, u := range ups {
		if u.remove || u.persist == persistNone {
			continue
		}
		key := store.ItemKey{Repo: store.KeyOf(u.ref), Kind: kind}
		var err error
		switch u.persist {
		case persistPut:
			b, _ := encodeStatus(u.val.V)
			err = s.d.Store.PutItem(ctx, store.Item{Key: key, State: u.val.Info.State, FetchedAt: u.val.Info.FetchedAt, CheckedAt: u.val.Info.CheckedAt,
				ContentSHA: u.val.Info.ContentSHA, Payload: b})
		case persistRevalidate:
			err = s.d.Store.Revalidated(ctx, key, u.val.Info.CheckedAt)
		case persistFailure:
			err = s.d.Store.RecordFailure(ctx, key, store.Failure{Code: u.val.Info.ErrCode, At: u.val.Info.CheckedAt})
		}
		if err != nil {
			// The snapshot stays authoritative for serving (architecture 9.3); a store failure is logged and counted by the store.
			s.log.WarnContext(ctx, "integrations: store write failed", "provider", name, "repo", u.ref.Name, "err", err)
		}
	}
}

// publish merges every update into the freshest slot value inside Apply (so a concurrent sweep of the other provider is
// never overwritten with a stale copy) and returns whether anything observable changed. fn does no I/O and never blocks.
func (s *Service) publish(name string, ups []update) (bool, error) {
	changed := false
	_, err := s.d.Publisher.Apply(func(b *readmodel.Builder) error {
		view := b.View()
		for _, u := range ups {
			cur, _ := readmodel.Get(view, u.slug, s.d.Status)
			next := maps.Clone(cur)
			if next == nil {
				next = Statuses{}
			}
			if u.remove {
				if _, has := next[name]; !has {
					continue
				}
				delete(next, name)
			} else {
				if old, has := next[name]; has && old.V.State == u.val.V.State && old.V.Summary == u.val.V.Summary &&
					old.V.DetailURL == u.val.V.DetailURL && maps.Equal(old.V.Counts, u.val.V.Counts) && old.Info.State == u.val.Info.State {
					// Same observable answer: still refresh freshness (it drives staleness), but not "changed".
				} else {
					changed = true
				}
				next[name] = u.val
			}
			if err := readmodel.Put(b, u.slug, s.d.Status, next); err != nil {
				if errors.Is(err, readmodel.ErrUnknownRepo) {
					continue // removed by a concurrent reconcile: nothing to attach to
				}
				return err
			}
			if u.remove {
				changed = true
			}
		}
		return nil
	})
	return changed, err
}

func (s *Service) record(ctx context.Context, name string, start, now time.Time, targets []Target, invalid []InvalidTarget, ups []update, probed int, sweepErr, partial error) {
	counts := map[string]int{}
	failCodes := map[string]int{}
	nodata, failed := 0, 0
	for _, u := range ups {
		if u.remove {
			continue
		}
		counts[u.val.V.State.String()]++
		if u.val.Info.State == freshness.StateNoContent {
			nodata++
		}
		if u.persist == persistFailure {
			failed++
			failCodes[u.val.Info.ErrCode]++
		}
	}
	failCode := ""
	for c, n := range failCodes { // most frequent, ties by code: deterministic
		if n > failCodes[failCode] || (n == failCodes[failCode] && c < failCode) {
			failCode = c
		}
	}
	// A sweep "succeeds" only when it has no sweep-level error, no aborted instance and not every probed target failed
	// (an HTML login page or a wrong base_url makes every target a per-target protocol error: that must not look healthy).
	code := ErrCode(sweepErr)
	if code == "" {
		code = ErrCode(partial)
	}
	if code == "" && probed > 0 && failed == probed {
		code = failCode
	}
	s.mu.Lock()
	h := s.health[name]
	h.Targets, h.Invalid, h.LastSweep = len(targets), len(invalid), now
	h.Probed, h.Failed, h.FailedCode, h.LastError = probed, failed, failCode, code
	if code == "" {
		h.LastSuccess, h.ConsecutiveFailures = now, 0
	} else {
		h.ConsecutiveFailures++
	}
	s.mu.Unlock()
	if s.sweepSecs == nil {
		return
	}
	pa := attribute.String("provider", name)
	s.sweepSecs.Record(ctx, now.Sub(start).Seconds(), metric.WithAttributes(pa))
	for _, st := range []State{StateUnknown, StateOK, StateFailing, StateUnstable} {
		s.stateGauge.Record(ctx, int64(counts[st.String()]), metric.WithAttributes(pa, attribute.String("state", st.String())))
	}
	s.targetGauge.Record(ctx, int64(len(targets)), metric.WithAttributes(pa, attribute.String("result", "resolved")))
	s.targetGauge.Record(ctx, int64(len(invalid)), metric.WithAttributes(pa, attribute.String("result", "invalid")))
	s.targetGauge.Record(ctx, int64(nodata), metric.WithAttributes(pa, attribute.String("result", "no_data")))
	if code == "" {
		s.lastOK.Record(ctx, now.Unix(), metric.WithAttributes(pa))
	}
}

// ---- fetching ----

// fetchAll returns len(targets) results (never fewer), the sweep-level error to hand to the engine and a "partial"
// error. Per-target failures are results, not errors. Sweep-level priority: rate limit > permanent (auth/config) >
// "instance unavailable" (transient), and the latter two are sweep-level ONLY when every probed instance is aborted:
// with one healthy instance left the sweep succeeds (otherwise the engine would park or back off the healthy instance's
// sweeps too) and the aborted instance is reported through partial (Health.LastError, /status).
func (s *Service) fetchAll(ctx context.Context, p StatusProvider, targets []Target) fetchOutcome {
	if len(targets) == 0 {
		return fetchOutcome{}
	}
	if bp, ok := p.(BulkProvider); ok {
		t0 := s.now()
		rs, err := bp.FetchBulk(ctx, targets)
		s.observe(ctx, p.Name(), t0, err)
		if err != nil {
			rs = make([]Result, len(targets))
			for i := range rs {
				rs[i].Err = err
			}
			return fetchOutcome{results: rs, sweepErr: err}
		}
		if len(rs) != len(targets) {
			err = fmt.Errorf("%w: provider returned %d results for %d targets", ErrProtocol, len(rs), len(targets))
			rs = make([]Result, len(targets))
			for i := range rs {
				rs[i].Err = err
			}
			return fetchOutcome{results: rs, sweepErr: err}
		}
		for i := range rs {
			s.observeOutcome(ctx, p.Name(), rs[i].Err)
		}
		return fetchOutcome{results: rs}
	}
	return s.fetchPool(ctx, p, targets)
}

// fetchOutcome is what a fetch pass hands to Sweep.
type fetchOutcome struct {
	results  []Result // len == len(probe), same order
	sweepErr error    // what the engine acts on
	partial  error    // an aborted instance while another instance stayed healthy: reported, not returned
}

// instState is the per-instance circuit of one sweep (this is the breaker of a batch upstream, see 4.2.1).
type instState struct {
	fails   atomic.Int32 // consecutive transient failures
	denied  atomic.Int32 // consecutive HTTP 403 answers (folder or job permission), a different failure than "down"
	aborted atomic.Bool
	mu      sync.Mutex
	err     error // why the instance was aborted (first cause wins)
}

func (is *instState) abort(err error) {
	is.mu.Lock()
	if is.err == nil {
		is.err = err
	}
	is.mu.Unlock()
	is.aborted.Store(true)
}

func (s *Service) fetchPool(ctx context.Context, p StatusProvider, targets []Target) fetchOutcome {
	cfg := s.cfg.Jenkins
	results := make([]Result, len(targets))
	ictx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	insts := map[string]*instState{}
	for _, t := range targets {
		if insts[t.Instance] == nil {
			insts[t.Instance] = &instState{}
		}
	}
	var (
		mu      sync.Mutex
		rateErr error // a 429 anywhere ends the whole sweep: the engine pauses the upstream
	)
	var paceMu sync.Mutex
	next := time.Time{}
	pace := func() {
		if cfg.Pace <= 0 {
			return
		}
		paceMu.Lock()
		now := s.now()
		if next.Before(now) {
			next = now
		}
		wait := next.Sub(now)
		next = next.Add(cfg.Pace)
		paceMu.Unlock()
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ictx.Done():
				t.Stop()
			}
		}
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				t := targets[i]
				is := insts[t.Instance]
				if is.aborted.Load() || ictx.Err() != nil {
					results[i].Err = ErrAborted
					continue
				}
				pace()
				t0 := s.now()
				st, err := p.Fetch(ictx, t)
				s.observe(ictx, p.Name(), t0, err)
				results[i] = Result{Status: st, Err: err}
				s.observeOutcome(ictx, p.Name(), err)
				var rl *RateLimitError
				var pe *PermanentError
				switch {
				case err == nil || errors.Is(err, scm.ErrNotFound) || errors.Is(err, ErrProtocol) || errors.Is(err, ErrTooLarge):
					is.fails.Store(0) // the instance answered
					is.denied.Store(0)
				case errors.Is(err, scm.ErrForbidden):
					// 403 is per job on Jenkins with folder or matrix permissions: a per-target result. Only a run of
					// them (anonymous read disabled, token without Overall/Read) condemns the instance.
					is.fails.Store(0)
					if int(is.denied.Add(1)) >= cfg.AbortAfter {
						is.abort(&PermanentError{Provider: p.Name(), Code: "auth", Err: fmt.Errorf("instance %q answered HTTP 403 for %d targets in a row: credentials lack read access", t.Instance, cfg.AbortAfter)})
					}
				case errors.As(err, &rl):
					mu.Lock()
					if rateErr == nil {
						rateErr = err
					}
					mu.Unlock()
					cancelAll()
				case errors.As(err, &pe):
					is.abort(err)
				case ictx.Err() != nil:
				default:
					if int(is.fails.Add(1)) >= cfg.AbortAfter {
						is.abort(fmt.Errorf("integrations: %s instance %q unavailable after %d consecutive failures: %w", p.Name(), t.Instance, cfg.AbortAfter, err))
					}
				}
			}
		}()
	}
feed:
	for i := range targets {
		select {
		case idx <- i:
		case <-ictx.Done():
			for j := i; j < len(targets); j++ {
				if results[j].Err == nil && results[j].Status.Provider == "" {
					results[j].Err = ErrAborted
				}
			}
			break feed
		}
	}
	close(idx)
	wg.Wait()

	// Fold the per-instance circuits into one sweep-level answer.
	var aborted []error
	healthy := 0
	for _, is := range insts {
		if is.aborted.Load() {
			aborted = append(aborted, is.err)
		} else {
			healthy++
		}
	}
	var sweepErr, partial error
	switch {
	case rateErr != nil:
		sweepErr = rateErr
	case len(aborted) > 0 && healthy == 0:
		sweepErr = worst(aborted)
	case len(aborted) > 0:
		partial = worst(aborted)
	}
	if sweepErr == nil && ctx.Err() != nil {
		sweepErr = ctx.Err()
	}
	return fetchOutcome{results: results, sweepErr: sweepErr, partial: partial}
}

// worst picks the abort cause the engine should act on: a permanent error (park, 24 h probe) beats a transient one.
func worst(errs []error) error {
	best := errs[0]
	for _, e := range errs {
		var pe *PermanentError
		if errors.As(e, &pe) {
			return e
		}
	}
	return best
}

func (s *Service) observe(ctx context.Context, provider string, t0 time.Time, _ error) {
	if s.fetchSecs != nil {
		s.fetchSecs.Record(ctx, s.now().Sub(t0).Seconds(), metric.WithAttributes(attribute.String("provider", provider)))
	}
}

func (s *Service) observeOutcome(ctx context.Context, provider string, err error) {
	if s.fetchTotal == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = ErrCode(err)
	}
	s.fetchTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", provider), attribute.String("outcome", outcome)))
}

var _ = slices.Sort[[]int]
