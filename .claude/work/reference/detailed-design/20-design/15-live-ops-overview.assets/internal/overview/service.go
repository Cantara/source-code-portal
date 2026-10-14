package overview

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// Metric names (units in the name where Prometheus convention asks for it; see design 15 section 9).
const (
	MetricLLMsRequests   = "scp_llms_requests_total"
	MetricLLMsGenerate   = "scp_llms_generate_seconds"
	MetricLLMsBytes      = "scp_llms_output_bytes"
	MetricLLMsTruncated  = "scp_llms_truncated_total"
	MetricLLMsOmitted    = "scp_llms_docs_omitted_total"
	MetricSitemapURLs    = "scp_sitemap_urls"
	MetricSitemapCut     = "scp_sitemap_truncated_total"
	MetricAliasRedirects = "scp_alias_redirects_total"
	MetricNegotiated     = "scp_negotiated_requests_total"
)

// Snapshots is the one thing overview needs from the publisher.
type Snapshots interface{ Current() *readmodel.Snapshot }

// Deps are the collaborators of the Service. Everything is read-only for overview.
type Deps struct {
	Log          *slog.Logger
	Meter        metric.Meter // nil: a no-op meter
	Snapshots    Snapshots
	Slots        Slots
	Authz        authz.Authorizer
	Docs         DocsSource
	Commits      CommitsSource
	Manifests    ManifestsSource // may be nil: manifests disabled
	Integrations IntegrationsSource
	Badges       BadgesSource
	Pages        PagesSource
	Build        BuildInfo
	PublicURL    *url.URL // server.public.public_url: an http(s) origin, or nil when unset: text outputs then use root-relative URLs and there is no sitemap
	Private      bool     // access.mode is not public: Cache-Control private, robots disallows everything, no sitemap
	SSEEnabled   bool     // live.sse.enabled (mirrored in api.site features)

	// Readme is the docs-owned handler of route readme.md; the negotiated repository page serves it for Accept: text/markdown.
	Readme http.Handler
	// Shell and ShellNotFound are the webapp's index.html handlers (status 200 and status 404).
	Shell, ShellNotFound http.Handler
	Now                  func() time.Time
}

// Service is the whole of the package: immutable after New, safe for concurrent use, no goroutines, nothing to Close.
type Service struct {
	cfg  Config
	d    Deps
	full chan struct{} // counting semaphore of concurrent *-full.txt generations

	llmsReq   metric.Int64Counter
	llmsGen   metric.Float64Histogram
	llmsBytes metric.Int64Histogram
	llmsCut   metric.Int64Counter
	llmsOmit  metric.Int64Counter
	smURLs    metric.Int64Gauge
	smCut     metric.Int64Counter
	aliasReq  metric.Int64Counter
	negReq    metric.Int64Counter
}

// New validates the configuration and the wiring and builds the Service.
func New(cfg Config, d Deps) (*Service, error) {
	var errs []error
	errs = append(errs, cfg.Validate()...)
	if d.Snapshots == nil || d.Authz == nil || d.Docs == nil || d.Commits == nil || d.Integrations == nil || d.Badges == nil || d.Pages == nil {
		errs = append(errs, errors.New("overview: Snapshots, Authz, Docs, Commits, Integrations, Badges and Pages are required"))
	}
	if d.Readme == nil || d.Shell == nil || d.ShellNotFound == nil {
		errs = append(errs, errors.New("overview: Readme, Shell and ShellNotFound are required"))
	}
	if u := d.PublicURL; u != nil && ((u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/")) {
		errs = append(errs, errors.New("overview: PublicURL must be an absolute http(s) origin without path, query, fragment or credentials"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Meter == nil {
		d.Meter = metricnoop.NewMeterProvider().Meter("overview")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &Service{cfg: cfg, d: d, full: make(chan struct{}, cfg.LLMs.MaxConcurrentFull)}
	m := d.Meter
	s.llmsReq, _ = m.Int64Counter(MetricLLMsRequests, metric.WithDescription("Requests of the llms.txt family by output (index, group, repo, full_root, full_group, full_repo) and result (ok, not_modified, not_found, disabled, overloaded)."))
	s.llmsGen, _ = m.Float64Histogram(MetricLLMsGenerate, metric.WithUnit("s"), metric.WithDescription("Time to generate one llms output (not cached)."),
		metric.WithExplicitBucketBoundaries(0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1))
	s.llmsBytes, _ = m.Int64Histogram(MetricLLMsBytes, metric.WithUnit("By"), metric.WithDescription("Size of one generated llms output."),
		metric.WithExplicitBucketBoundaries(1<<10, 4<<10, 16<<10, 64<<10, 256<<10, 1<<20, 2<<20, 4<<20))
	s.llmsCut, _ = m.Int64Counter(MetricLLMsTruncated, metric.WithDescription("Outputs cut or reduced to stay within a cap, by output and reason (size, index_chars)."))
	s.llmsOmit, _ = m.Int64Counter(MetricLLMsOmitted, metric.WithDescription("Documents left out of a full-text output, by reason (unavailable, size)."))
	s.smURLs, _ = m.Int64Gauge(MetricSitemapURLs, metric.WithDescription("URLs in the most recently generated sitemap."))
	s.smCut, _ = m.Int64Counter(MetricSitemapCut, metric.WithDescription("Sitemaps cut at the 50,000-URL protocol limit."))
	s.aliasReq, _ = m.Int64Counter(MetricAliasRedirects, metric.WithDescription("Slug-alias requests by result (redirected, hidden, live_wins)."))
	s.negReq, _ = m.Int64Counter(MetricNegotiated, metric.WithDescription("Negotiated human URLs by route (repo, group), representation (html, markdown) and source of the Markdown (readme, index)."))
	return s, nil
}

// Mount registers every route this package owns (architecture 11.2): four API operations, the negotiated human URLs and
// the server-only text routes. The alias middleware is separate (AliasRedirect) because it wraps the whole mux.
func (s *Service) Mount(m httpapi.Mux) {
	m.HandleFunc("GET /api/v1/site", s.siteHandler)
	m.HandleFunc("GET /api/v1/repos", s.listHandler(false))
	m.HandleFunc("GET /api/v1/groups/{groupId}/repos", s.listHandler(true))
	m.HandleFunc("GET /api/v1/repos/{repo}", s.repoHandler)

	m.HandleFunc("GET /groups/{groupId}", s.negotiatedGroup().ServeHTTP)
	m.HandleFunc("GET /repos/{repo}", s.negotiatedRepo().ServeHTTP)
	m.HandleFunc("GET /repos/{repo}/{rest...}", s.negotiatedRest)

	m.HandleFunc("GET /llms.txt", s.llmsIndex)
	m.HandleFunc("GET /llms-full.txt", s.llmsFullRoot)
	m.HandleFunc("GET /groups/{groupId}/llms.txt", s.llmsGroup)
	m.HandleFunc("GET /groups/{groupId}/llms-full.txt", s.llmsGroupFull)
	m.HandleFunc("GET /repos/{repo}/llms.txt", s.llmsRepo)
	m.HandleFunc("GET /repos/{repo}/llms-full.txt", s.llmsRepoFull)
	m.HandleFunc("GET /sitemap.xml", s.sitemap)
	m.HandleFunc("GET /robots.txt", s.robots)
}

// textCacheControl is the Cache-Control of every text output: public while there are no identities, private otherwise.
func (s *Service) textCacheControl() string {
	d := "max-age=" + strconv.Itoa(int(s.cfg.LLMs.MaxAge/time.Second))
	if s.d.Private {
		return "private, " + d
	}
	return "public, " + d
}
