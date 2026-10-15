// Package httpapi builds the HTTP handlers of the two listeners: the public port (web UI, API, badges, LLM surface, webhook)
// and the admin port (probes, metrics, profiling, status).
package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Route is a handler a feature serves outside the API contract (badges, the webhook, Markdown). MaxBody caps the request body
// (0: DefaultMaxBody). A Markdown route shares its address with a page of the UI: Handler answers when the path ends in .md or the
// client prefers text/markdown to text/html, the page answers otherwise. A Stream route has no request deadline and, under /api/,
// none of the API's buffering.
type Route struct {
	Pattern  string
	Handler  http.Handler
	MaxBody  int64
	Markdown bool
	Stream   bool
}

// Public returns the handler of the public listener. pages answers the paths no other handler has (Options.Pages); contract is
// api/openapi.yaml, served at /api/v1/openapi.yaml; api implements it; routes are the features' other handlers.
func Public(o Options, pages http.Handler, contract []byte, api oapi.StrictServerInterface, routes ...Route) http.Handler {
	mux := http.NewServeMux()
	streams := map[string]bool{}
	for _, r := range routes {
		n := r.MaxBody
		if n == 0 {
			n = DefaultMaxBody
		}
		h := r.Handler
		if r.Markdown {
			h = negotiated(h, pages)
		}
		if r.Stream {
			h = unbounded(h)
			_, p, _ := strings.Cut(r.Pattern, " ")
			streams[p] = true
		}
		mux.Handle(r.Pattern, limit(n, h))
	}
	mux.Handle("GET /", limit(DefaultMaxBody, pages))
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, "not-found", "No such API resource.")
	})
	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(contract)
	})
	strict := oapi.NewStrictHandlerWithOptions(api, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			WriteProblem(w, http.StatusBadRequest, "validation-failed", "The request parameters are not valid.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			WriteProblem(w, http.StatusInternalServerError, "internal", "The response could not be produced.")
		},
	})
	oapi.HandlerWithOptions(strict, oapi.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			WriteProblem(w, http.StatusBadRequest, "validation-failed", "The request parameters are not valid.")
		}})
	return o.wrap("public", apiGuard(mux, streams))
}

// Admin returns the handler of the admin listener. ready reports whether the portal serves a catalogue, alive whether the sync
// dispatcher is running; routes are the operator handlers (metrics, status, profiling).
func Admin(o Options, alive, ready func() bool, routes ...Route) http.Handler {
	mux := http.NewServeMux()
	probe := func(ok func() bool) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if !ok() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready\n"))
				return
			}
			_, _ = w.Write([]byte("ok\n"))
		}
	}
	mux.HandleFunc("GET /livez", probe(alive))
	mux.HandleFunc("GET /readyz", probe(ready))
	for _, r := range routes {
		mux.Handle(r.Pattern, limit(DefaultMaxBody, r.Handler))
	}
	return o.wrap("admin", mux)
}

// Server returns an http.Server with the fixed timeouts both listeners use.
func Server(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// SnapshotAPI implements getSnapshot.
type SnapshotAPI struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

func (a SnapshotAPI) GetSnapshot(ctx context.Context, _ oapi.GetSnapshotRequestObject) (oapi.GetSnapshotResponseObject, error) {
	return oapi.GetSnapshot200JSONResponse{Body: snapshotInfo(a.Current(ctx), time.Now())}, nil
}

func snapshotInfo(s *snapshot.Snapshot, now time.Time) oapi.SnapshotInfo {
	out := oapi.SnapshotInfo{Version: int64(s.Version), BuiltAt: oapi.TimeOrNil(s.BuiltAt), Ready: s.Catalogued, //nolint:gosec // G115: 53 bits
		Sources: make([]oapi.SourceInfo, 0, len(s.Sources))}
	for _, src := range s.Sources {
		out.Sources = append(out.Sources, oapi.SourceInfo{Id: src.ID, Repos: src.Repos, Freshness: oapi.FreshnessOf(src.Fresh, now)})
	}
	return out
}

// MeAPI implements getMe.
type MeAPI struct {
	SignIn func() (oidc, token bool) // the ways of signing in; nil: none
}

func (a MeAPI) GetMe(ctx context.Context, _ oapi.GetMeRequestObject) (oapi.GetMeResponseObject, error) {
	v := ViewerOf(ctx)
	out := oapi.Me{SignedIn: v.SignedIn(), Name: v.Name, Via: oapi.MeVia(v.Via), Private: v.Private, Admin: v.Admin}
	if !v.SignedIn() {
		out.Via = oapi.None
	}
	if a.SignIn != nil {
		out.SignIn.Oidc, out.SignIn.Token = a.SignIn()
	}
	return oapi.GetMe200JSONResponse(out), nil
}

// WriteProblem writes an RFC 9457 problem with fixed text; the instance is the request id the middleware set.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	h := w.Header()
	h.Del("ETag")
	h.Del("Content-Encoding")
	h.Del("Content-Length")
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	p := oapi.Problem{Type: "urn:scp:problem:" + code, Title: http.StatusText(status), Status: status, Code: code, Detail: &detail}
	if id := h.Get("X-Request-Id"); id != "" {
		inst := "urn:scp:request:" + id
		p.Instance = &inst
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// apiGuard applies the API conventions under /api/, except on the stream paths: 406 for an Accept without JSON or YAML, the mux's
// plain 405 rewritten to a problem, validators on GET.
func apiGuard(next http.Handler, streams map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || streams[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if !acceptsAPI(r.Header.Values("Accept")) {
			WriteProblem(w, http.StatusNotAcceptable, "not-acceptable", "The API answers application/json.")
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			conditional(w, r, next)
			return
		}
		next.ServeHTTP(&problem405{ResponseWriter: w}, r)
	})
}

// acceptsAPI reports whether the Accept header admits a JSON, problem or YAML answer. No header admits everything.
func acceptsAPI(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for v := range strings.SplitSeq(strings.Join(values, ","), ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		if q, ok := params["q"]; ok && strings.Trim(q, "0.") == "" {
			continue // q=0
		}
		switch mt {
		case "*/*", "application/*", "application/json", "application/problem+json", "application/yaml":
			return true
		}
	}
	return false
}

// problem405 turns the mux's plain-text 405 into a problem.
type problem405 struct {
	http.ResponseWriter
	swallow bool
}

func (w *problem405) WriteHeader(status int) {
	if status == http.StatusMethodNotAllowed && strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		w.swallow = true
		WriteProblem(w.ResponseWriter, status, "method-not-allowed", "The method is not allowed on this resource.")
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *problem405) Write(p []byte) (int, error) {
	if w.swallow {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

func (w *problem405) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// conditional buffers a 200 response, gives it a weak ETag over its body and answers 304 when If-None-Match carries that ETag.
// The body includes every derived freshness status, so a change from fresh to stale changes the ETag.
func conditional(w http.ResponseWriter, r *http.Request, next http.Handler) {
	buf := &buffered{header: w.Header().Clone(), status: http.StatusOK}
	next.ServeHTTP(buf, r)
	h := w.Header()
	clear(h)
	for k, v := range buf.header {
		h[k] = v
	}
	if buf.status != http.StatusOK {
		w.WriteHeader(buf.status)
		_, _ = w.Write(buf.body.Bytes())
		return
	}
	sum := sha256.Sum256(buf.body.Bytes())
	etag := `W/"` + hex.EncodeToString(sum[:12]) + `"`
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache")
	if Matches(r.Header.Get("If-None-Match"), etag) {
		h.Del("Content-Type")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.body.Bytes())
}

// Matches reports whether an If-None-Match header names etag (the weak comparison of RFC 9110) or is "*".
func Matches(inm, etag string) bool {
	for t := range strings.SplitSeq(inm, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

type buffered struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (b *buffered) Header() http.Header { return b.header }

func (b *buffered) WriteHeader(status int) {
	if !b.wrote {
		b.status, b.wrote = status, true
	}
}

func (b *buffered) Write(p []byte) (int, error) {
	b.wrote = true
	return b.body.Write(p)
}

// negotiated answers with md for a .md path or a client that prefers Markdown, with page otherwise.
func negotiated(md, page http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")
		w.Header().Set("Link", `</llms.txt>; rel="describedby"`)
		if strings.HasSuffix(r.URL.Path, ".md") || prefersMarkdown(r.Header.Values("Accept")) {
			md.ServeHTTP(w, r)
			return
		}
		page.ServeHTTP(w, r)
	})
}

// prefersMarkdown reports whether text/markdown has a strictly higher quality than text/html in the Accept header.
func prefersMarkdown(values []string) bool {
	return quality(values, "text/markdown") > quality(values, "text/html")
}

// quality is the q the most specific matching range gives mediaType (0 when none matches).
func quality(values []string, mediaType string) float64 {
	typ, _, _ := strings.Cut(mediaType, "/")
	best, rank := 0.0, 0
	for v := range strings.SplitSeq(strings.Join(values, ","), ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		r := 0
		switch mt {
		case mediaType:
			r = 3
		case typ + "/*":
			r = 2
		case "*/*":
			r = 1
		default:
			continue
		}
		q := 1.0
		if qs, ok := params["q"]; ok {
			if f, err := strconv.ParseFloat(qs, 64); err == nil {
				q = f
			}
		}
		if r > rank {
			best, rank = q, r
		}
	}
	return best
}
