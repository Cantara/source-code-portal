// Package webhook is the GitHub webhook receiver: size limit, signature verification through a Verifier,
// bounded delivery de-duplication after a successful enqueue, 202/4xx/5xx mapping, and the optional
// webhook.redelivery job. It imports scm, job, httpapi and (in production) logx only.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/scm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Verifier is satisfied directly by *github.Verifier (kernel types only).
type Verifier interface {
	Verify(src scm.SourceID, header http.Header, body []byte) (scm.Event, error)
}

// Sink is the wired *reactor.Fan. Only job.ErrQueueFull and job.ErrClosed in its result become a 503.
type Sink interface {
	Handle(ctx context.Context, ev scm.Event) error
}

// Limiter is the keyed "invalid signature" limiter class (ratelimit.webhook_invalid), satisfied directly by *httpx.Limiter (no bridge; D4).
// Allow consumes one token and reports whether the key is still within its budget.
type Limiter interface{ Allow(key string) bool }

// Behaviour interfaces the Verifier's errors satisfy (structurally; github never imports webhook).
type unauthenticated interface {
	error
	Unauthenticated() bool
}

// invalidSignature is the name unit 05's WebhookError uses for the same thing; both are honoured until the
// two units agree on one (design section 14, cross-unit item X1).
type invalidSignature interface {
	error
	InvalidSignature() bool
}
type malformed interface {
	error
	Malformed() bool
}
type ignored interface {
	error
	Ignored() bool
}

// Deps is injected by cmd/scp (G11).
type Deps struct {
	Log    *slog.Logger
	Meter  metric.Meter
	Tracer trace.Tracer
}

func (d Deps) norm() Deps {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Meter == nil {
		d.Meter = noop.NewMeterProvider().Meter("webhook")
	}
	if d.Tracer == nil {
		d.Tracer = tracenoop.NewTracerProvider().Tracer("webhook")
	}
	return d
}

// Config is mounted at github.defaults.webhook (yaml tags are the config keys).
type Config struct {
	MaxBody       int64         `yaml:"max_body"`       // 25 MiB
	DedupeTTL     time.Duration `yaml:"dedupe_ttl"`     // 15m
	DedupeMax     int           `yaml:"dedupe_max"`     // 10000
	MaxConcurrent int           `yaml:"max_concurrent"` // 4 bodies read and verified at once
	AcquireWait   time.Duration `yaml:"acquire_wait"`   // 2s: wait for a body slot before answering 503
	RetryAfter    time.Duration `yaml:"retry_after"`    // 5s: Retry-After of every 503/429 answer
	BodyTimeout   time.Duration `yaml:"body_timeout"`   // 8s: the whole body must arrive within this (slow-body defence), < GitHub's 10 s
}

func DefaultConfig() Config {
	return Config{MaxBody: 25 << 20, DedupeTTL: 15 * time.Minute, DedupeMax: 10000, MaxConcurrent: 4, AcquireWait: 2 * time.Second, RetryAfter: 5 * time.Second, BodyTimeout: 8 * time.Second}
}

// Sources tells the receiver which {source} path values exist and which one the source-less route means.
type Sources struct {
	IDs     []scm.SourceID
	Default scm.SourceID // "" when several sources exist and none is the default: the source-less route answers 404
}

// Result labels of scp_webhook_total.
const (
	ResAccepted        = "accepted"
	ResDuplicate       = "duplicate"
	ResInvalidSig      = "invalid_signature"
	ResMalformed       = "malformed"
	ResIgnored         = "ignored"
	ResPing            = "ping"
	ResQueueFull       = "queue_full"
	ResUnknownSource   = "unknown_source"
	ResTooLarge        = "too_large"
	ResUnsupportedType = "unsupported_media"
	ResThrottled       = "throttled"
	ResBusy            = "busy"
	ResBodyTimeout     = "body_timeout"
	ResError           = "error"
)

const (
	MetricWebhookTotal      = "scp_webhook_total"
	MetricWebhookBodyBytes  = "scp_webhook_body_bytes"
	MetricWebhookDedupeSize = "scp_webhook_dedupe_entries"
	MetricWebhookLastAccept = "scp_webhook_last_accepted_timestamp_seconds"
	MetricRedelivery        = "scp_webhook_redelivery_total"
)

// Receiver is the HTTP handler for POST /webhooks/github[/{source}].
type Receiver struct {
	cfg          Config
	srcs         map[scm.SourceID]struct{}
	def          scm.SourceID
	ver          Verifier
	sink         Sink
	lim          Limiter
	clientIP     func(*http.Request) string
	log          *slog.Logger
	tr           trace.Tracer
	slots        chan struct{}
	dd           *dedupe
	mTotal       metric.Int64Counter
	mBody        metric.Int64Histogram
	lastAccepted atomic.Int64 // unix nanoseconds

	mu     sync.Mutex
	counts map[string]uint64
}

// NewReceiver validates its inputs: a nil Verifier or Sink, an empty source set or a non-positive limit is an error.
func NewReceiver(cfg Config, srcs Sources, v Verifier, sink Sink, lim Limiter, clientIP func(*http.Request) string, d Deps) (*Receiver, error) {
	if v == nil || sink == nil || len(srcs.IDs) == 0 {
		return nil, errors.New("webhook: Verifier, Sink and at least one source are required")
	}
	if cfg.MaxBody <= 0 || cfg.DedupeTTL <= 0 || cfg.DedupeMax <= 0 || cfg.MaxConcurrent <= 0 || cfg.AcquireWait < 0 || cfg.RetryAfter <= 0 || cfg.BodyTimeout <= 0 {
		return nil, errors.New("webhook: Config values must be positive")
	}
	d = d.norm()
	r := &Receiver{cfg: cfg, srcs: map[scm.SourceID]struct{}{}, def: srcs.Default, ver: v, sink: sink, lim: lim,
		clientIP: clientIP, log: d.Log, tr: d.Tracer, slots: make(chan struct{}, cfg.MaxConcurrent),
		dd: newDedupe(cfg.DedupeTTL, cfg.DedupeMax), counts: map[string]uint64{}}
	for _, id := range srcs.IDs {
		r.srcs[id] = struct{}{}
	}
	if srcs.Default != "" {
		if _, ok := r.srcs[srcs.Default]; !ok {
			return nil, errors.New("webhook: default source is not in the source set")
		}
	}
	r.mTotal, _ = d.Meter.Int64Counter(MetricWebhookTotal)
	r.mBody, _ = d.Meter.Int64Histogram(MetricWebhookBodyBytes, metric.WithUnit("By"))
	_, _ = d.Meter.Int64ObservableGauge(MetricWebhookDedupeSize, metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(int64(r.dd.Len()))
		return nil
	}))
	_, _ = d.Meter.Float64ObservableGauge(MetricWebhookLastAccept, metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
		if n := r.lastAccepted.Load(); n > 0 {
			o.Observe(float64(n) / 1e9)
		}
		return nil
	}))
	return r, nil
}

// Stats is the read-only view for /status (bridged by cmd/scp).
type Stats struct {
	LastAccepted time.Time
	Results      map[string]uint64
	DedupeSize   int
}

func (rc *Receiver) Stats() Stats {
	rc.mu.Lock()
	m := make(map[string]uint64, len(rc.counts))
	for k, v := range rc.counts {
		m[k] = v
	}
	rc.mu.Unlock()
	var last time.Time
	if n := rc.lastAccepted.Load(); n > 0 {
		last = time.Unix(0, n)
	}
	return Stats{LastAccepted: last, Results: m, DedupeSize: rc.dd.Len()}
}

func (rc *Receiver) result(ctx context.Context, res string) {
	rc.mTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("result", res)))
	rc.mu.Lock()
	rc.counts[res]++
	rc.mu.Unlock()
}

func (rc *Receiver) problem(w http.ResponseWriter, r *http.Request, res string, p httpapi.Problem) {
	rc.result(r.Context(), res)
	if p.Status == http.StatusServiceUnavailable || p.Status == http.StatusTooManyRequests {
		// unit 06: the header comes from Problem.Header only; the body member must equal it
		p = withRetryAfter(p, max(1, int(rc.cfg.RetryAfter/time.Second)))
	}
	httpapi.WriteProblem(w, r, p)
}

// withRetryAfter sets header and body member together (unit 06 requires them to be equal).
func withRetryAfter(p httpapi.Problem, seconds int) httpapi.Problem {
	p.Header = http.Header{"Retry-After": {strconv.Itoa(seconds)}}
	p.RetryAfterSeconds = seconds
	return p
}

type accepted struct {
	Status   string `json:"status"`
	Delivery string `json:"delivery,omitempty"`
}

func ok(w http.ResponseWriter, status string, delivery string) {
	b, _ := json.Marshal(accepted{status, delivery}) // delivery is validated ([0-9A-Za-z-]{1,64}) before it gets here
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(append(b, '\n'))
}

// validDelivery: GitHub's X-GitHub-Delivery is a UUID. Anything else from a verified sender is refused so that
// the de-dup set (10,000 keys) and the response cannot be inflated by a long or hostile header.
func validDelivery(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
			return false
		}
	}
	return true
}

// ServeHTTP implements the receive pipeline: source, media type, cheap signature syntax, body slot, bounded
// read, verify, ping, de-dupe check, sink, de-dupe record, answer. Order matters: nothing is parsed or
// logged from the body before Verify succeeds (GR-20).
func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := rc.tr.Start(r.Context(), "webhook.handle")
	defer span.End()
	r = r.WithContext(ctx)

	src := scm.SourceID(r.PathValue("source"))
	if src == "" {
		src = rc.def
	}
	if _, known := rc.srcs[src]; !known || src == "" {
		rc.problem(w, r, ResUnknownSource, httpapi.Problem{Status: 404, Code: "not-found", Title: "Unknown webhook source"})
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		rc.problem(w, r, ResUnsupportedType, httpapi.Problem{Status: 415, Code: "unsupported-media-type", Title: "Configure the webhook with content type application/json"})
		return
	}
	if !plausibleSig(r.Header.Get("X-Hub-Signature-256")) {
		rc.invalid(w, r, "missing or malformed X-Hub-Signature-256")
		return
	}
	if r.ContentLength > rc.cfg.MaxBody {
		rc.problem(w, r, ResTooLarge, httpapi.Problem{Status: 413, Code: "payload-too-large", Title: "Payload too large"})
		return
	}
	// body slot: bounds memory to MaxConcurrent x MaxBody and the HMAC CPU an unauthenticated sender can cause
	acq := time.NewTimer(rc.cfg.AcquireWait)
	select {
	case rc.slots <- struct{}{}:
		acq.Stop()
	case <-acq.C:
		rc.problem(w, r, ResBusy, httpapi.Problem{Status: 503, Code: "overloaded", Title: "Too many webhook bodies in flight"})
		return
	case <-r.Context().Done():
		acq.Stop()
		return
	}
	released := false
	release := func() {
		if !released {
			released = true
			<-rc.slots
		}
	}
	defer release()

	// slow-body defence: a plausible header costs nothing to forge, so a sender must not be able to hold a body
	// slot longer than BodyTimeout (the server's ReadTimeout of 30 s is far above GitHub's own 10 s).
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(rc.cfg.BodyTimeout))
	body, err := readBody(w, r, rc.cfg.MaxBody)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			rc.problem(w, r, ResTooLarge, httpapi.Problem{Status: 413, Code: "payload-too-large", Title: "Payload too large"})
		} else if errors.Is(err, os.ErrDeadlineExceeded) {
			rc.problem(w, r, ResBodyTimeout, httpapi.Problem{Status: 408, Code: "request-timeout", Title: "Request body too slow"})
		} else {
			rc.problem(w, r, ResMalformed, httpapi.Problem{Status: 400, Code: "validation-failed", Title: "Request body could not be read"})
		}
		return
	}
	rc.mBody.Record(ctx, int64(len(body)))

	ev, verr := rc.ver.Verify(src, r.Header, body)
	body = nil
	release()
	if verr != nil {
		var ua unauthenticated
		var is invalidSignature
		var mf malformed
		var ig ignored
		switch {
		case errors.As(verr, &ua) && ua.Unauthenticated(), errors.As(verr, &is) && is.InvalidSignature():
			rc.invalid(w, r, "signature verification failed")
		case errors.As(verr, &ig) && ig.Ignored():
			rc.result(ctx, ResIgnored)
			ok(w, "ignored", safeID(ev.Delivery))
		case errors.As(verr, &mf) && mf.Malformed():
			rc.problem(w, r, ResMalformed, httpapi.Problem{Status: 400, Code: "validation-failed", Title: "Malformed webhook payload"})
		default:
			rc.log.LogAttrs(ctx, slog.LevelError, "webhook verifier failed", slog.String("source", string(src)), slog.String("err", verr.Error()))
			rc.problem(w, r, ResError, httpapi.Problem{Status: 500, Code: "internal", Title: "Internal error"})
		}
		return
	}
	if !validDelivery(ev.Delivery) { // verified sender, hostile or broken delivery id
		rc.problem(w, r, ResMalformed, httpapi.Problem{Status: 400, Code: "validation-failed", Title: "Malformed delivery id"})
		return
	}
	span.SetAttributes(attribute.String("event", string(ev.Kind)), attribute.String("delivery", ev.Delivery))
	log := rc.log.With(slog.String("delivery_id", ev.Delivery), slog.String("source", string(src)), slog.String("event", string(ev.Kind)))

	switch ev.Kind {
	case scm.EventPing:
		rc.result(ctx, ResPing)
		ok(w, "pong", ev.Delivery)
		return
	case scm.EventHeadChanged, "":
		rc.problem(w, r, ResMalformed, httpapi.Problem{Status: 400, Code: "validation-failed", Title: "Unsupported event"})
		return
	}
	ev.Source = src
	dkey := string(src) + "/" + ev.Delivery
	if rc.dd.Seen(dkey, time.Now()) {
		rc.result(ctx, ResDuplicate)
		ok(w, "duplicate", ev.Delivery)
		return
	}
	if herr := rc.sink.Handle(ctx, ev); herr != nil {
		if errors.Is(herr, job.ErrQueueFull) || errors.Is(herr, job.ErrClosed) {
			log.LogAttrs(ctx, slog.LevelWarn, "webhook not accepted: engine refused", slog.String("err", herr.Error()))
			rc.problem(w, r, ResQueueFull, httpapi.Problem{Status: 503, Code: "queue-full", Title: "Refresh queue is full or shutting down"})
			return
		}
		// events are hints: any other reactor error is logged, the delivery is still accepted
		log.LogAttrs(ctx, slog.LevelError, "webhook reactor error", slog.String("err", herr.Error()))
	}
	rc.dd.Record(dkey, time.Now())
	rc.lastAccepted.Store(time.Now().UnixNano())
	rc.result(ctx, ResAccepted)
	ok(w, "accepted", ev.Delivery)
}

// invalid answers 401, or 429 once the client IP exhausted its invalid-signature budget. Valid deliveries
// are never limited and nothing is blocked before verification.
func (rc *Receiver) invalid(w http.ResponseWriter, r *http.Request, why string) {
	if rc.lim != nil && rc.clientIP != nil && !rc.lim.Allow(rc.clientIP(r)) {
		rc.problem(w, r, ResThrottled, httpapi.Problem{Status: 429, Code: "rate-limited", Title: "Too many invalid signatures"})
		return
	}
	rc.log.LogAttrs(r.Context(), slog.LevelWarn, "webhook rejected", slog.String("reason", why),
		slog.String("delivery_id", safeID(r.Header.Get("X-GitHub-Delivery"))))
	rc.problem(w, r, ResInvalidSig, httpapi.Problem{Status: 401, Code: "unauthorized", Title: "Invalid signature"})
}

// readBody reads at most max bytes. With a known Content-Length the buffer is allocated once (io.ReadAll's
// doubling would transiently hold up to 2.5x the body per slot); chunked bodies fall back to growth.
func readBody(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	var buf bytes.Buffer
	if cl := r.ContentLength; cl > 0 && cl <= max {
		buf.Grow(int(cl) + 512) // +512: bytes.Buffer.ReadFrom wants MinRead free bytes before it sees EOF
	}
	_, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, max))
	return buf.Bytes(), err
}

func plausibleSig(h string) bool {
	const p = "sha256="
	if len(h) != len(p)+64 || h[:len(p)] != p {
		return false
	}
	for i := len(p); i < len(h); i++ {
		c := h[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// safeID returns an untrusted delivery header for logging: at most 64 characters of [0-9A-Za-z-].
func safeID(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	b := []byte(s)
	for i, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
			b[i] = '?'
		}
	}
	return string(b)
}
