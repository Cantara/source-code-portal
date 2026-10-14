package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/live/liveapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// Metric names (constants next to their instruments, G11).
const (
	MetricSSEClients     = "scp_sse_clients"
	MetricSSEConnections = "scp_sse_connections_total"
	MetricSSEEvents      = "scp_sse_events_total"
	MetricSSEWriteErrors = "scp_sse_write_errors_total"
	MetricSSEStream      = "scp_sse_stream_seconds"
	MetricSnapshotPolls  = "scp_snapshot_polls_total"
)

// Feed is what live needs from *readmodel.Publisher in v1 (satisfied directly: Current() only).
type Feed interface {
	Current() *readmodel.Snapshot
}

// ChangeFeed is the change feed behind GET /api/v1/events. unit 03 ships it in v1.1 (ADR-002, ADR-009), so v1 leaves
// Deps.Changes nil and the route is removed with Table.Without("api.events"); in v1.1 a bridge in cmd/scp adapts
// *readmodel.Publisher (cross-design review CD-IF-02: live no longer names a readmodel type that does not exist in v1).
type ChangeFeed interface {
	Subscribe() (wake <-chan struct{}, cancel func())
	ChangesSince(v uint64) ([]ChangeSet, bool)
}

// ChangeSet is one publish as live needs it. It mirrors the v1.1 readmodel record (architecture 8.4) plus Slots, the names
// of the per-repository slots touched, which live needs to map a change to a topic (D15-1; unit 03 v1.1 adds the field).
type ChangeSet struct {
	Version uint64
	Repos   []readmodel.EntryID
	Slots   []string
	Globals []string
	Resync  bool
}

// Phase reports the data readiness shown in the snapshot document: "warming", "ready" or "degraded" (never
// "shutting-down": the snapshot describes data, not the process). Satisfied directly by *ops.Readiness.
type Phase interface{ DataPhase() string }

// Revision is the version and the variant tag that identify the snapshot document of one caller.
type Revision struct {
	Number  uint64
	Variant string // "" or a view-class tag; part of the ETag
}

// RevisionFunc lets cmd/scp serve the public view class a revision that advances only when something visible to it
// changed (access unit requirement R-1). nil: the global snapshot version for everyone.
type RevisionFunc func(r *http.Request, s *readmodel.Snapshot) Revision

type Deps struct {
	Log          *slog.Logger
	Meter        metric.Meter
	Feed         Feed
	Changes      ChangeFeed // nil in v1 (SSE is v1.1); required when live.sse.enabled
	Readiness    Phase
	BuildID      string
	Epoch        string                     // random per process; lets a client detect a restart (hello event)
	ClientKey    func(*http.Request) string // the resolved client IP (httpx.ClientIP); nil: RemoteAddr host
	Revision     RevisionFunc
	MaxPerClient int  // ratelimit.sse_per_ip (unit 02): concurrent streams per client key; 1..MaxClients
	StatusTopic  bool // true on the admin listener's instance: invalidate events also carry the "status" topic
}

type Service struct {
	cfg Config
	d   Deps
	b   *broker

	clients metric.Int64ObservableGauge
	conns   metric.Int64Counter
	events  metric.Int64Counter
	werrs   metric.Int64Counter
	stream  metric.Float64Histogram
	polls   metric.Int64Counter
}

func New(cfg Config, d Deps) (*Service, error) {
	if errs := cfg.Validate("live"); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if d.Feed == nil || d.Readiness == nil {
		return nil, errors.New("live: Feed and Readiness are required")
	}
	if cfg.SSE.Enabled && d.Changes == nil {
		return nil, errors.New("live: live.sse.enabled needs Deps.Changes (the change feed ships with unit 03 v1.1)")
	}
	if d.MaxPerClient < 1 || d.MaxPerClient > cfg.SSE.MaxClients {
		return nil, fmt.Errorf("live: MaxPerClient %d outside 1..live.sse.max_clients (%d)", d.MaxPerClient, cfg.SSE.MaxClients)
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Meter == nil {
		d.Meter = metricnoop.NewMeterProvider().Meter("live")
	}
	if d.ClientKey == nil {
		d.ClientKey = remoteHost
	}
	s := &Service{cfg: cfg, d: d, b: newBroker(d.MaxPerClient, cfg.SSE.MaxClients)}
	s.clients, _ = d.Meter.Int64ObservableGauge(MetricSSEClients, metric.WithDescription("Open SSE streams."))
	_, _ = d.Meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(s.clients, int64(s.b.open()))
		return nil
	}, s.clients)
	s.conns, _ = d.Meter.Int64Counter(MetricSSEConnections, metric.WithDescription("SSE connection attempts by result (accepted, rejected_ip, rejected_total, disabled, drained)."))
	s.events, _ = d.Meter.Int64Counter(MetricSSEEvents, metric.WithDescription("SSE events written by event name (hello, invalidate, resync)."))
	s.werrs, _ = d.Meter.Int64Counter(MetricSSEWriteErrors, metric.WithDescription("SSE writes that failed (slow or gone client)."))
	s.stream, _ = d.Meter.Float64Histogram(MetricSSEStream, metric.WithUnit("s"), metric.WithDescription("Lifetime of one SSE stream."),
		metric.WithExplicitBucketBoundaries(1, 5, 15, 60, 300, 900, 1800, 3600))
	s.polls, _ = d.Meter.Int64Counter(MetricSnapshotPolls, metric.WithDescription("GET /api/v1/snapshot by result (ok, not_modified)."))
	return s, nil
}

// Mount registers both operations (api.snapshot, api.events); both are always registered so routes.Table.Verify passes.
func (s *Service) Mount(m httpapi.Mux) {
	m.HandleFunc("GET /api/v1/snapshot", s.snapshot)
	m.HandleFunc("GET /api/v1/events", s.serveEvents)
}

// Drain is the lifecycle Drain hook: every stream is told to finish (retry: drain_retry) and Drain waits for them.
func (s *Service) Drain(ctx context.Context) error { return s.b.drain(ctx) }

// remoteHost is the default client key: the host part of RemoteAddr. The full "host:port" would give every connection
// its own key and make the per-client limit a no-op (found in verification; cmd/scp passes httpx.ClientIP instead).
func remoteHost(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

func (s *Service) revision(r *http.Request, snap *readmodel.Snapshot) Revision {
	if s.d.Revision != nil {
		return s.d.Revision(r, snap)
	}
	return Revision{Number: snap.Version()}
}

// snapshot implements api.snapshot. It never answers 503: before the first publish it is version 0 and "warming".
func (s *Service) snapshot(w http.ResponseWriter, r *http.Request) {
	snap := s.d.Feed.Current()
	rev := s.revision(r, snap)
	phase := s.d.Readiness.DataPhase()
	etag := `W/"v` + strconv.FormatUint(rev.Number, 10)
	if rev.Variant != "" {
		etag += "." + rev.Variant
	}
	etag += "-" + phase + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache")
	h.Set("X-SCP-Build", s.d.BuildID)
	if httpapi.NotModified(r, etag) {
		s.polls.Add(r.Context(), 1, metric.WithAttributes(attribute.String("result", "not_modified")))
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.polls.Add(r.Context(), 1, metric.WithAttributes(attribute.String("result", "ok")))
	h.Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(liveapi.Snapshot{
		Version: int64(rev.Number), BuiltAt: snap.BuiltAt().UTC().Format(time.RFC3339), Readiness: phase, BuildID: s.d.BuildID})
}

type sseWriter struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
}

func (x *sseWriter) arm() {
	if err := x.rc.SetWriteDeadline(time.Now().Add(x.timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		_ = err // a failed re-arm surfaces as the write error that follows
	}
}

func (x *sseWriter) raw(s string) error {
	x.arm()
	if _, err := x.w.Write([]byte(s)); err != nil {
		return err
	}
	if err := x.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func (x *sseWriter) retry(d time.Duration) error {
	return x.raw("retry: " + strconv.FormatInt(d.Milliseconds(), 10) + "\n\n")
}
func (x *sseWriter) comment(c string) error { return x.raw(": " + c + "\n\n") }
func (x *sseWriter) event(id uint64, name string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return x.raw(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", id, name, b))
}

func parseLastEventID(h string) (uint64, bool) {
	if h == "" || len(h) > 20 {
		return 0, false
	}
	v, err := strconv.ParseUint(h, 10, 64)
	return v, err == nil
}

// serveEvents implements api.events (v1.1).
func (s *Service) serveEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodHead && s.cfg.SSE.Enabled {
		// HEAD must not open a stream: the handler would hold a slot and the (keep-alive) connection for max_lifetime.
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache, no-store, no-transform")
		h.Set("X-SCP-Build", s.d.BuildID)
		w.WriteHeader(http.StatusOK)
		return
	}
	count := func(result string) { s.conns.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result))) }
	if !s.cfg.SSE.Enabled {
		count("disabled")
		httpapi.WriteProblem(w, r, httpapi.Problem{Status: 404, Code: httpapi.CodeFeatureDisabled, Title: "Event stream disabled"})
		return
	}
	release, err := s.b.acquire(s.d.ClientKey(r))
	switch {
	case errors.Is(err, errDraining):
		// Never a non-200 here: EventSource does not reconnect after a failed response (WHATWG HTML, 9.2).
		count("drained")
		sw := s.open(w)
		_ = sw.retry(s.cfg.SSE.DrainRetry)
		return
	case errors.Is(err, errPerKey):
		count("rejected_ip")
		httpapi.WriteProblem(w, r, httpapi.RateLimited(30*time.Second))
		return
	case errors.Is(err, errTotal):
		count("rejected_total")
		httpapi.WriteProblem(w, r, httpapi.WithRetry(httpapi.New(503, httpapi.CodeQueueFull, "the event stream is at capacity"), 30*time.Second))
		return
	}
	defer release()
	count("accepted")
	started := time.Now()
	defer func() { s.stream.Record(context.WithoutCancel(ctx), time.Since(started).Seconds()) }()

	// Subscribe before reading the current version: a publish between the two is then a pending wake, never a gap.
	wake, cancel := s.d.Changes.Subscribe()
	defer cancel()

	sw := s.open(w)
	cur := s.d.Feed.Current().Version()
	last := cur
	emit := func(id uint64, name string, data any) bool {
		if err := sw.event(id, name, data); err != nil {
			s.werrs.Add(ctx, 1)
			return false
		}
		s.events.Add(ctx, 1, metric.WithAttributes(attribute.String("event", name)))
		return true
	}
	if err := sw.retry(s.cfg.SSE.Retry); err != nil {
		s.werrs.Add(ctx, 1)
		return
	}
	if !emit(cur, "hello", liveapi.SseHello{Generation: int64(cur), BuildID: s.d.BuildID, Epoch: s.d.Epoch}) {
		return
	}
	if id, ok := parseLastEventID(r.Header.Get("Last-Event-ID")); ok && id != cur {
		last = id
		if !s.catchUp(&last, cur, emit) {
			return
		}
	}

	hb := time.NewTicker(s.cfg.SSE.Heartbeat)
	defer hb.Stop()
	life := time.NewTimer(s.lifetime())
	defer life.Stop()
	var deb *time.Timer
	var debC <-chan time.Time
	defer func() {
		if deb != nil {
			deb.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.b.done:
			_ = sw.retry(s.cfg.SSE.DrainRetry)
			return
		case <-life.C:
			_ = sw.retry(s.cfg.SSE.Retry)
			return
		case <-hb.C:
			if err := sw.comment("hb"); err != nil {
				s.werrs.Add(ctx, 1)
				return
			}
		case <-wake:
			if deb == nil {
				deb = time.NewTimer(s.cfg.SSE.Debounce)
				debC = deb.C
			}
		case <-debC:
			deb, debC = nil, nil
			if !s.catchUp(&last, s.d.Feed.Current().Version(), emit) {
				return
			}
		}
	}
}

// catchUp brings a client from *last to cur: nothing, one invalidate, or one resync. It advances *last on success.
func (s *Service) catchUp(last *uint64, cur uint64, emit func(uint64, string, any) bool) bool {
	if cur == *last {
		return true
	}
	if cur < *last { // a client from a previous process lifetime that the hello epoch did not catch
		*last = cur
		return emit(cur, "resync", liveapi.SseResync{Generation: int64(cur)})
	}
	cs, ok := s.d.Changes.ChangesSince(*last)
	if !ok {
		*last = cur
		return emit(cur, "resync", liveapi.SseResync{Generation: int64(cur)})
	}
	topics, resync := Topics(cs, s.d.StatusTopic)
	*last = cur
	switch {
	case resync:
		return emit(cur, "resync", liveapi.SseResync{Generation: int64(cur)})
	case len(topics) == 0:
		return true // changes without a topic (nothing a client displays): advance silently
	}
	return emit(cur, "invalidate", liveapi.SseInvalidate{Topics: topics, Generation: int64(cur)})
}

func (s *Service) open(w http.ResponseWriter) *sseWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-store, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-SCP-Build", s.d.BuildID)
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, rc: http.NewResponseController(w), timeout: s.cfg.SSE.WriteTimeout}
}

// lifetime is max_lifetime +-10%, so a restart does not make every client reconnect in the same second.
func (s *Service) lifetime() time.Duration {
	f := 0.9 + 0.2*rand.Float64()
	return time.Duration(float64(s.cfg.SSE.MaxLifetime) * f)
}
