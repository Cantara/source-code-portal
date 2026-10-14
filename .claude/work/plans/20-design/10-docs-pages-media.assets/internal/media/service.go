package media

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Route names registered by cmd/scp with routes.Table.Handle.
const (
	RouteRepo = "media.repo"
	RouteExt  = "media.ext"
)

// Metric names.
const (
	MetricFetchTotal   = "scp_media_fetch_total"
	MetricFetchSeconds = "scp_media_fetch_seconds"
	MetricCacheTotal   = "scp_media_cache_total"
	MetricCacheBytes   = "scp_media_cache_bytes"
	MetricRepoTotal    = "scp_media_repo_total"
	MetricSigTotal     = "scp_media_signature_total"
)

// RepoImageReader is satisfied directly by *github.Client (and *gitsource.Client later).
type RepoImageReader interface {
	ReadRepoImage(ctx context.Context, r scm.RepoRef, ref, path string, maxBytes int64) (scm.File, error)
}

// HTTPDoer is the injected outbound client. cmd/scp builds it with platform/netguard (connect-time IP policy, Proxy nil,
// dial/TLS/header timeouts) and with CheckRedirect returning http.ErrUseLastResponse: media validates every hop itself.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Deps are the constructor dependencies.
type Deps struct {
	Cfg    Config
	Images RepoImageReader
	Client HTTPDoer
	Pub    *readmodel.Publisher
	Repos  readmodel.Slot[freshness.Value[scm.Repository]]
	Authz  authz.Authorizer
	Meter  metric.Meter
	Tracer trace.Tracer
	Log    *slog.Logger
}

// Service is the image proxy. It starts no goroutine; it is safe for concurrent use.
type Service struct {
	d         Deps
	signer    *Signer
	ephemeral bool
	cache     *lru
	flights   *flight
	sem       chan struct{}
	m         struct {
		fetch, cache, repo, sig metric.Int64Counter
		fetchSeconds            metric.Float64Histogram
	}
}

// New validates the configuration and dependencies.
func New(d Deps) (*Service, error) {
	errs := d.Cfg.Validate("media")
	for n, isNil := range map[string]bool{"Images": d.Images == nil, "Client": d.Client == nil, "Pub": d.Pub == nil, "Authz": d.Authz == nil, "Log": d.Log == nil} {
		if isNil {
			errs = append(errs, fmt.Errorf("media: Deps.%s is required", n))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if d.Meter == nil {
		d.Meter = noop.NewMeterProvider().Meter("media")
	}
	if d.Tracer == nil {
		d.Tracer = nooptrace.NewTracerProvider().Tracer("media")
	}
	sg, eph, err := NewSigner(d.Cfg.SigningKey, d.Cfg.PreviousKeys)
	if err != nil {
		return nil, err
	}
	s := &Service{d: d, signer: sg, ephemeral: eph, cache: newLRU(d.Cfg.CacheBytes, 24*time.Hour), flights: newFlight(), sem: make(chan struct{}, d.Cfg.MaxConcurrent)}
	if eph {
		d.Log.Warn("media.signing_key is empty: using an ephemeral key; stored documents are re-rendered at every restart")
	}
	var e1, e2, e3, e4, e5 error
	s.m.fetch, e1 = d.Meter.Int64Counter(MetricFetchTotal)
	s.m.cache, e2 = d.Meter.Int64Counter(MetricCacheTotal)
	s.m.repo, e3 = d.Meter.Int64Counter(MetricRepoTotal)
	s.m.sig, e4 = d.Meter.Int64Counter(MetricSigTotal)
	s.m.fetchSeconds, e5 = d.Meter.Float64Histogram(MetricFetchSeconds, metric.WithUnit("s"))
	if err := errors.Join(e1, e2, e3, e4, e5); err != nil {
		return nil, err
	}
	if _, err := d.Meter.Int64ObservableGauge(MetricCacheBytes, metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		b, _ := s.cache.stats()
		o.Observe(b)
		return nil
	})); err != nil {
		return nil, err
	}
	return s, nil
}

// SignExternal satisfies docs.ImageSigner.
func (s *Service) SignExternal(rawURL string) (string, bool) { return s.signer.Sign(rawURL) }

// KeyID satisfies docs.ImageSigner.
func (s *Service) KeyID() string { return s.signer.KeyID() }

// Ephemeral reports whether the signing key was generated at start (shown in /status).
func (s *Service) Ephemeral() bool { return s.ephemeral }

func res(r string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("outcome", r))
}

// fetchError is a classified failure of an external fetch.
type fetchError struct {
	outcome string // metric label
	status  int    // HTTP status given to the image request
	retry   time.Duration
}

func (e *fetchError) Error() string { return "media: " + e.outcome }

func hostAllowed(host string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	for _, a := range allow {
		if a == host || (strings.HasPrefix(a, "*.") && strings.HasSuffix(host, a[1:])) {
			return true
		}
	}
	return false
}

// External serves GET /media/ext/{sig}/{id}.
func (s *Service) External() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		raw, err := s.signer.Verify(r.PathValue("sig"), r.PathValue("id"))
		if err != nil {
			s.m.sig.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "bad")))
			httpapi.WriteProblem(w, r, httpapi.NotFound("image not found"))
			return
		}
		s.m.sig.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "ok")))
		u, _ := url.Parse(raw)
		if !hostAllowed(u.Hostname(), s.d.Cfg.AllowHosts) {
			s.m.fetch.Add(ctx, 1, res("host_denied"))
			httpapi.WriteProblem(w, r, httpapi.NotFound("image not found"))
			return
		}
		if !imageRequest(r) {
			httpapi.WriteProblem(w, r, httpapi.Forbidden("images are served to image requests only"))
			return
		}
		now := time.Now()
		e, fresh := s.cache.get(raw, now)
		if e != nil && fresh {
			kind := "hit"
			if e.negative != 0 {
				kind = "negative"
			}
			s.m.cache.Add(ctx, 1, metric.WithAttributes(attribute.String("result", kind)))
			s.respond(w, r, e, "public, max-age=3600, stale-while-revalidate=86400")
			return
		}
		got, err, shared := s.flights.do(ctx, raw, func() (*entry, error) { return s.fetchExternal(ctx, raw) })
		if shared {
			s.m.cache.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "shared")))
		} else {
			s.m.cache.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "miss")))
		}
		if err != nil {
			if e != nil && e.negative == 0 { // stale-while-error
				s.m.cache.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "stale")))
				s.respond(w, r, e, "public, max-age=60")
				return
			}
			s.writeFetchError(w, r, err)
			return
		}
		s.respond(w, r, got, "public, max-age=3600, stale-while-revalidate=86400")
	})
}

// imageRequest rejects navigations: a browser asking for a document (Sec-Fetch-Dest: document, iframe, ...) is refused;
// a client that sends no Sec-Fetch-Dest (curl, old browsers, unfurlers) is served, the response CSP sandbox covers it.
func imageRequest(r *http.Request) bool {
	switch d := r.Header.Get("Sec-Fetch-Dest"); d {
	case "", "image", "empty":
		return true
	default:
		return false
	}
}

func (s *Service) writeFetchError(w http.ResponseWriter, r *http.Request, err error) {
	var fe *fetchError
	if !errors.As(err, &fe) {
		if errors.Is(err, context.Canceled) {
			return
		}
		fe = &fetchError{outcome: "error", status: http.StatusBadGateway}
	}
	switch {
	case fe.status == http.StatusNotFound:
		httpapi.WriteProblem(w, r, httpapi.NotFound("image not available"))
	default:
		httpapi.WriteProblem(w, r, httpapi.UpstreamUnavailable(max(fe.retry, time.Second)))
	}
}

func (s *Service) respond(w http.ResponseWriter, r *http.Request, e *entry, cc string) {
	if e.negative != 0 {
		httpapi.WriteProblem(w, r, httpapi.NotFound("image not available"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", string(e.kind))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if e.kind == KindSVG {
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Add("Vary", "Sec-Fetch-Dest") // the navigation gate answers 403 to the same URL: no cache may replay the image to a navigation (V-04)
	h.Set("Cache-Control", cc)
	h.Set("ETag", e.etag)
	if httpapi.NotModified(r, e.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(e.body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(e.body)
	}
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:12]) + `"`
}

// fetchExternal downloads, validates and caches one external image. Failures are remembered for NegativeTTL.
func (s *Service) fetchExternal(ctx context.Context, raw string) (*entry, error) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		s.m.fetch.Add(ctx, 1, res("busy"))
		return nil, &fetchError{outcome: "busy", status: http.StatusServiceUnavailable, retry: time.Second}
	}
	start := time.Now()
	ctx, span := s.d.Tracer.Start(context.WithoutCancel(ctx), "media.fetch")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, s.d.Cfg.FetchTimeout)
	defer cancel()
	e, ferr := s.download(ctx, raw)
	s.m.fetchSeconds.Record(ctx, time.Since(start).Seconds())
	if ferr != nil {
		s.m.fetch.Add(ctx, 1, res(ferr.outcome))
		if ferr.status == http.StatusNotFound { // permanent: remember
			s.cache.put(&entry{key: raw, negative: http.StatusNotFound, expires: time.Now().Add(s.d.Cfg.NegativeTTL)})
		}
		return nil, ferr
	}
	s.m.fetch.Add(ctx, 1, res("ok"))
	e.key, e.expires = raw, time.Now().Add(s.d.Cfg.CacheTTL)
	s.cache.put(e)
	return e, nil
}

const maxHops = 3

func (s *Service) download(ctx context.Context, raw string) (*entry, *fetchError) {
	cur := raw
	for hop := 0; ; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur, nil)
		if err != nil {
			return nil, &fetchError{outcome: "policy_denied", status: http.StatusNotFound}
		}
		req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif;q=0.9,image/svg+xml;q=0.5")
		req.Header.Set("User-Agent", "scp-media-proxy")
		resp, err := s.d.Client.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, &fetchError{outcome: "timeout", status: http.StatusGatewayTimeout, retry: 30 * time.Second}
			}
			return nil, &fetchError{outcome: "upstream_error", status: http.StatusBadGateway, retry: 30 * time.Second}
		}
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != http.StatusNotModified:
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if hop >= maxHops {
				return nil, &fetchError{outcome: "redirect_denied", status: http.StatusNotFound}
			}
			base, _ := url.Parse(cur)
			next, err := base.Parse(loc)
			if err != nil {
				return nil, &fetchError{outcome: "redirect_denied", status: http.StatusNotFound}
			}
			c, err := Canonical(next.String())
			nu, _ := url.Parse(c)
			if err != nil || !hostAllowed(nu.Hostname(), s.d.Cfg.AllowHosts) {
				return nil, &fetchError{outcome: "redirect_denied", status: http.StatusNotFound}
			}
			cur = c
			continue
		case resp.StatusCode == http.StatusOK:
			defer resp.Body.Close()
			return s.readBody(resp)
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
			resp.Body.Close()
			return nil, &fetchError{outcome: "upstream_status", status: http.StatusNotFound}
		default:
			resp.Body.Close()
			return nil, &fetchError{outcome: "upstream_status", status: http.StatusBadGateway, retry: 30 * time.Second}
		}
	}
}

func (s *Service) readBody(resp *http.Response) (*entry, *fetchError) {
	max := s.d.Cfg.MaxBytes
	if resp.ContentLength > max {
		return nil, &fetchError{outcome: "too_large", status: http.StatusNotFound}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, &fetchError{outcome: "upstream_error", status: http.StatusBadGateway, retry: 30 * time.Second}
	}
	if int64(len(b)) > max {
		return nil, &fetchError{outcome: "too_large", status: http.StatusNotFound}
	}
	k := Sniff(b)
	switch {
	case k == KindUnknown:
		return nil, &fetchError{outcome: "not_image", status: http.StatusNotFound}
	case k == KindSVG && s.d.Cfg.ExternalSVG != SVGSandboxed:
		return nil, &fetchError{outcome: "svg_denied", status: http.StatusNotFound}
	case k == KindSVG && int64(len(b)) > s.d.Cfg.MaxSVGBytes:
		return nil, &fetchError{outcome: "too_large", status: http.StatusNotFound}
	}
	return &entry{body: b, kind: k, etag: etagOf(b)}, nil
}

// Repo serves GET /media/{repo}/{ref}/{path...}: an image of a repository at a commit.
func (s *Service) Repo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		count := func(o string) { s.m.repo.Add(ctx, 1, res(o)) }
		ref, p := r.PathValue("ref"), r.PathValue("path")
		if !isCommitSHA(ref) || !validImagePath(p) {
			count("bad_request")
			httpapi.WriteProblem(w, r, httpapi.NotFound("image not found"))
			return
		}
		if !imageRequest(r) {
			httpapi.WriteProblem(w, r, httpapi.Forbidden("images are served to image requests only"))
			return
		}
		snap := s.d.Pub.Current()
		repo, prob := authz.LookupRepo(ctx, snap, s.d.Repos, s.d.Authz, scm.Slug(r.PathValue("repo")))
		if prob != nil {
			count("not_found")
			httpapi.WriteProblem(w, r, *prob)
			return
		}
		key := "repo|" + repo.Ref.Key() + "|" + ref + "|" + p
		cc := "public, max-age=31536000, immutable"
		if repo.Visibility != scm.VisPublic {
			cc = "private, max-age=31536000, immutable"
		}
		if e, fresh := s.cache.get(key, time.Now()); e != nil && fresh && e.negative == 0 {
			count("cache_hit")
			s.respond(w, r, e, cc)
			return
		}
		f, err := s.d.Images.ReadRepoImage(ctx, repo.Ref, ref, p, s.d.Cfg.MaxBytes)
		if err != nil {
			if errors.Is(err, scm.ErrNotFound) {
				count("not_found")
				httpapi.WriteProblem(w, r, httpapi.NotFound("image not found"))
				return
			}
			count("upstream_error")
			httpapi.WriteProblem(w, r, httpapi.UpstreamUnavailable(30*time.Second))
			return
		}
		k := Sniff(f.Data)
		if k == KindUnknown || f.Truncated || (k == KindSVG && int64(len(f.Data)) > s.d.Cfg.MaxSVGBytes) {
			count("not_image")
			httpapi.WriteProblem(w, r, httpapi.NotFound("image not found"))
			return
		}
		e := &entry{key: key, body: f.Data, kind: k, etag: `"` + f.SHA + `"`, expires: time.Now().Add(24 * time.Hour)}
		s.cache.put(e)
		count("ok")
		s.respond(w, r, e, cc)
	})
}

func isCommitSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true}

func validImagePath(p string) bool {
	if p == "" || len(p) > 500 || strings.ContainsAny(p, "\x00\\") || path.Clean(p) != p || strings.HasPrefix(p, "/") {
		return false
	}
	segs := strings.Split(p, "/")
	if len(segs) > 32 {
		return false
	}
	for _, sg := range segs {
		if sg == "" || sg == "." || sg == ".." {
			return false
		}
	}
	return imageExt[strings.ToLower(path.Ext(p))]
}
