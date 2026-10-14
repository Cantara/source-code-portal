package docs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/docs/internal/render"
	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Route names registered by cmd/scp with routes.Table.Handle (the generated API routes api.docs.list/api.docs.get go
// through Mount).
const (
	RouteReadmeMD    = "readme.md"
	RouteAgentsMD    = "agents.md"
	RouteChangelogMD = "changelog.md"
	RouteDocMD       = "doc.md"
	RouteSchemas     = "schemas"
)

// Metric names (constants next to their instruments; the cmd/scp registry test compares them with docs/reference/metrics.md).
const (
	MetricRefreshTotal   = "scp_docs_refresh_total"
	MetricFetchTotal     = "scp_docs_fetch_total"
	MetricRenderSeconds  = "scp_docs_render_seconds"
	MetricRenderTotal    = "scp_docs_render_total"
	MetricResidentBytes  = "scp_docs_resident_bytes"
	MetricNotResident    = "scp_docs_not_resident"
	MetricRestoreTotal   = "scp_docs_restore_total"
	MetricReactionsTotal = "scp_docs_reactions_total"
	MetricRawTotal       = "scp_docs_raw_requests_total"
	MetricDescriptorProb = "scp_docs_descriptor_problems_total"
)

// Deps are the constructor dependencies of Service.
type Deps struct {
	Cfg       Config
	Files     FileReader
	Trees     TreeLister
	Rendered  RenderedReader // nil when Cfg.BridgeAsciiDoc is false
	Signer    ImageSigner
	Store     ItemStore
	Pub       *readmodel.Publisher
	Repos     readmodel.Slot[freshness.Value[scm.Repository]] // catalog.repo (existence slot)
	Stats     readmodel.Slot[freshness.Value[scm.RepoStats]]  // catalog.stats
	Aliases   readmodel.Global[map[scm.Slug]scm.Slug]         // catalog.aliases
	DocsSlot  readmodel.Slot[RepoDocs]                        // registered by cmd/scp as SlotName
	Authz     authz.Authorizer
	PublicURL string // server.public_url, validated by routes.Absolute in New
	Meter     metric.Meter
	Tracer    trace.Tracer
	Log       *slog.Logger
	Now       func() time.Time // optional; defaults to time.Now (tests inject a synctest-bubble clock value)
}

// Service owns docs ingestion, rendering orchestration and the docs HTTP surface. It starts no goroutine: jobs run in
// the engine, handlers on request goroutines. Safe for concurrent use.
type Service struct {
	d         Deps
	pipeline  string
	policy    *safehtml.Policy
	renderer  render.Renderer
	loader    *DescriptorLoader
	renderSem chan struct{}
	resident  atomic.Int64 // bytes of resident bodies in the whole process (admission counter)
	hints     hintMap
	m         instruments
}

type instruments struct {
	refresh, fetch, renderTotal, restore, reactions, raw, descProb metric.Int64Counter
	renderSeconds                                                  metric.Float64Histogram
	notResident                                                    metric.Int64Counter
}

// PipelineID names everything that determines the HTML of a document: the renderer version, the sanitiser policy
// version and the media signing key. It is stored with every payload and is the second half of the raw-Markdown ETag.
func PipelineID(signerKeyID string) string {
	return fmt.Sprintf("r%d.p%d.k%s", render.Version, safehtml.PolicyVersion, signerKeyID)
}

// New validates the dependencies and builds the Service.
func New(d Deps) (*Service, error) {
	var errs []error
	errs = append(errs, d.Cfg.Validate("docs")...)
	for name, nil_ := range map[string]bool{"Files": d.Files == nil, "Trees": d.Trees == nil, "Signer": d.Signer == nil, "Store": d.Store == nil,
		"Pub": d.Pub == nil, "Authz": d.Authz == nil, "Log": d.Log == nil} {
		if nil_ {
			errs = append(errs, fmt.Errorf("docs: Deps.%s is required", name))
		}
	}
	if d.Cfg.BridgeAsciiDoc && d.Rendered == nil {
		errs = append(errs, errors.New("docs: Deps.Rendered is required when docs.bridge_asciidoc is true"))
	}
	if d.PublicURL != "" {
		if _, err := absoluteURL(d.PublicURL, "/"); err != nil {
			errs = append(errs, fmt.Errorf("docs: public URL: %w", err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Meter == nil {
		d.Meter = noop.NewMeterProvider().Meter("docs")
	}
	if d.Tracer == nil {
		d.Tracer = nooptrace.NewTracerProvider().Tracer("docs")
	}
	loader, err := NewDescriptorLoader()
	if err != nil {
		return nil, err
	}
	s := &Service{d: d, pipeline: PipelineID(d.Signer.KeyID()), policy: safehtml.DocsPolicy(), renderer: render.New(), loader: loader,
		renderSem: make(chan struct{}, d.Cfg.RenderConcurrency), hints: newHintMap(4096, 10*time.Minute)}
	if err := s.initMetrics(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) initMetrics() error {
	var err error
	c := func(name, desc string) metric.Int64Counter {
		if err != nil {
			return nil
		}
		var m metric.Int64Counter
		m, err = s.d.Meter.Int64Counter(name, metric.WithDescription(desc))
		return m
	}
	s.m.refresh = c(MetricRefreshTotal, "docs.refresh outcomes")
	s.m.fetch = c(MetricFetchTotal, "GitHub reads by kind")
	s.m.renderTotal = c(MetricRenderTotal, "render outcomes")
	s.m.restore = c(MetricRestoreTotal, "warm-start decode outcomes")
	s.m.reactions = c(MetricReactionsTotal, "webhook and head-sweep reactions")
	s.m.raw = c(MetricRawTotal, "raw Markdown requests")
	s.m.descProb = c(MetricDescriptorProb, "descriptor problems by code")
	if err == nil {
		_, err = s.d.Meter.Int64ObservableGauge(MetricNotResident, metric.WithInt64Callback(s.observeNotResident))
	}
	if err == nil {
		s.m.renderSeconds, err = s.d.Meter.Float64Histogram(MetricRenderSeconds, metric.WithUnit("s"))
	}
	if err == nil {
		_, err = s.d.Meter.Int64ObservableGauge(MetricResidentBytes, metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(s.resident.Load())
			return nil
		}))
	}
	return err
}

// Pipeline returns the current PipelineID.
func (s *Service) Pipeline() string { return s.pipeline }

// ResidentBytes returns the admission counter.
func (s *Service) ResidentBytes() int64 { return s.resident.Load() }

// Jobs returns the docs.refresh spec (architecture 6.3, 9.6).
func (s *Service) Jobs() []job.Spec {
	return []job.Spec{{
		Kind: JobKind, Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled,
		Policy:  freshness.Policy{TTL: s.d.Cfg.TTL, Jitter: 0.1, IdleBackoffMax: s.d.Cfg.IdleBackoffMax},
		Timeout: s.d.Cfg.JobTimeout,
		Run:     s.run,
	}}
}

// run is docs.refresh for one repository key. Specified in section 4 of the design; the prototype stops at the
// signature.
func (s *Service) run(ctx context.Context, k job.Key) (job.Outcome, error) {
	return job.Outcome{}, errors.New("docs: run: see design section 4.1")
}

// Decode is the warm-start decoder: see the package function Decode.
func (s *Service) Decode(it []byte, info freshness.Info) (RepoDocs, error) {
	rd, err := Decode(it, info, s.policy, func(src string) (string, []string) { return render.NormaliseSource(src) })
	if err != nil {
		s.m.restore.Add(context.Background(), 1, metric.WithAttributes(attrResult("invalid")))
		return RepoDocs{}, err
	}
	res := "ok"
	if rd.Pipeline != s.pipeline {
		res = "pipeline_stale"
	}
	s.m.restore.Add(context.Background(), 1, metric.WithAttributes(attrResult(res)))
	return rd, nil
}

// Restore Puts a decoded value inside Apply and accounts its resident bytes. It returns readmodel.ErrUnknownRepo for an
// entry without an existence value (the caller skips it).
func (s *Service) Restore(b *readmodel.Builder, id readmodel.EntryID, rd RepoDocs) error {
	if err := readmodel.Put(b, id, s.d.DocsSlot, rd); err != nil {
		return err
	}
	s.resident.Add(rd.ResidentBytes())
	return nil
}

// Coverage reports how many catalogue repositories have a docs value (readiness probe; docs never gates readiness).
func (s *Service) Coverage(snap *readmodel.Snapshot) (have, total int) {
	for _, id := range snap.Repos() {
		total++
		if _, ok := readmodel.Get(snap, id, s.d.DocsSlot); ok {
			have++
		}
	}
	return have, total
}

// PageRender is what pages receives from RenderPage (the bridge converts it to pages.RenderedPage).
type PageRender struct {
	HTML        safehtml.SafeHTML
	Title       string
	Description string
	Order       *int
	Hidden      bool
	Tags        []string
	TOC         []TOCEntry
	Problems    []Problem
}

// RenderPage renders a configured page through the same pipeline as repository documents, with page link resolution
// (no repository: relative links are left as portal-relative, images may only be https and become signed /media/ext URLs).
func (s *Service) RenderPage(ctx context.Context, src []byte) (PageRender, error) {
	return PageRender{}, errors.New("docs: RenderPage: see design section 4.9")
}

// Mount registers the generated strict server of api.docs.list and api.docs.get.
func (s *Service) Mount(m httpapi.Mux) {}

// Readme serves GET /repos/{repo}/readme.md.
func (s *Service) Readme() http.Handler { return s.rawHandler(RouteReadmeMD) }

// Agents serves GET /repos/{repo}/agents.md.
func (s *Service) Agents() http.Handler { return s.rawHandler(RouteAgentsMD) }

// Changelog serves GET /repos/{repo}/changelog.md.
func (s *Service) Changelog() http.Handler { return s.rawHandler(RouteChangelogMD) }

// DocRoute serves GET /repos/{repo}/docs/{path...}: a path ending in .md is raw Markdown, any other path goes through
// httpapi.Negotiate(spa, markdown) so that Accept: text/markdown yields the same body.
func (s *Service) DocRoute(spa http.Handler) http.Handler {
	raw := s.rawHandler(RouteDocMD)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.PathValue("path")
		if strings.HasSuffix(strings.ToLower(p), ".md") {
			raw.ServeHTTP(w, r)
			return
		}
		httpapi.Negotiate(spa, raw).ServeHTTP(w, r)
	})
}

// Schemas serves GET /schemas/scp/{file}.
func (s *Service) Schemas() http.Handler { return schemasHandler() }

func (s *Service) rawHandler(route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteProblem(w, r, httpapi.New(http.StatusNotImplemented, "internal", route))
	})
}

// hintMap remembers, per job key, the commit a push event announced. Bounded, lazily evicted, lossy by design: webhooks
// are hints and the head sweep repairs a lost one.
type hintMap struct {
	mu  sync.Mutex
	max int
	ttl time.Duration
	m   map[job.Key]hint
}

type hint struct {
	After string
	At    time.Time
}

func newHintMap(max int, ttl time.Duration) hintMap {
	return hintMap{max: max, ttl: ttl, m: make(map[job.Key]hint)}
}

// Put records a hint, evicting expired entries (and, when full, the oldest) first.
func (h *hintMap) Put(k job.Key, after string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.m) >= h.max {
		var oldest job.Key
		var ot time.Time
		for kk, v := range h.m {
			if now.Sub(v.At) > h.ttl {
				delete(h.m, kk)
				continue
			}
			if ot.IsZero() || v.At.Before(ot) {
				oldest, ot = kk, v.At
			}
		}
		if len(h.m) >= h.max {
			delete(h.m, oldest)
		}
	}
	h.m[k] = hint{after, now}
}

// Peek returns the hint of k while it is younger than the ttl WITHOUT removing it: a continuation run, or a scheduled run
// that starts before the catalogue head sweep (5 minutes) has caught up, must read the same commit as the run that
// consumed the push, never an older one (verification V-11). An expired hint is dropped.
func (h *hintMap) Peek(k job.Key, now time.Time) (hint, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.m[k]
	if ok && now.Sub(v.At) > h.ttl {
		delete(h.m, k)
		return hint{}, false
	}
	return v, ok
}

// Forget drops the hint of k (the sweep value caught up with it, or the run finished a complete publish for a newer head).
func (h *hintMap) Forget(k job.Key) {
	h.mu.Lock()
	delete(h.m, k)
	h.mu.Unlock()
}

// Len is the number of entries (a gauge).
func (h *hintMap) Len() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.m) }

// observeNotResident counts, per reason, the documents of the current snapshot that are listed without a body. It walks
// the snapshot at scrape time (50,000 documents at the target size: well under a millisecond).
func (s *Service) observeNotResident(_ context.Context, o metric.Int64Observer) error {
	counts := map[string]int64{"budget": 0, "count_cap": 0, "pending": 0}
	snap := s.d.Pub.Current()
	for _, id := range snap.Repos() {
		rd, ok := readmodel.Get(snap, id, s.d.DocsSlot)
		if !ok {
			continue
		}
		for _, d := range rd.Docs {
			if d.Entry.State == StateNotResident {
				counts[d.Entry.Reason]++
			}
		}
	}
	for r, n := range counts {
		o.Observe(n, metric.WithAttributes(attribute.String("reason", r)))
	}
	return nil
}
