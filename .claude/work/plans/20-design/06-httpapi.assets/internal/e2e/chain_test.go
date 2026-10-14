package e2e_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/apispec"
	"github.com/cantara/source-code-portal/internal/commits/commitsapi"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/apitest"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/httpapi/validate"
	"github.com/cantara/source-code-portal/internal/platform/logx"
)

// A feature as it will be written: strict handler, problems as errors, helpers from httpapi.
type feature struct{}

func (feature) Mount(m httpapi.Mux) {
	req, resp := httpapi.StrictErrors(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := commitsapi.NewStrictHandlerWithOptions(feature{}, nil, commitsapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: req, ResponseErrorHandlerFunc: resp})
	commitsapi.HandlerWithOptions(h, commitsapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}

func (feature) ListCommits(ctx context.Context, r commitsapi.ListCommitsRequestObject) (commitsapi.ListCommitsResponseObject, error) {
	repo := ""
	if r.Params.Repo != nil {
		repo = *r.Params.Repo
	}
	switch repo {
	case "missing":
		return nil, httpapi.Err(httpapi.NotFound("repository not found"))
	case "boom":
		return nil, errors.New("database password is hunter2")
	}
	req := httpapi.RequestFrom(ctx) // WithRequest is mounted innermost: a strict handler gets only (ctx, request object)
	pp, prob := httpapi.ParsePage(httpapi.Deref(r.Params.Limit), httpapi.Deref(r.Params.Cursor), 50, 200)
	if prob != nil {
		return nil, httpapi.Err(*prob)
	}
	etag := httpapi.WithDigest(httpapi.WeakETag(1042, "public"), httpapi.FreshnessDigest("fresh"))
	if httpapi.NotModified(req, etag) {
		return commitsapi.ListCommits304Response{Headers: openapi.NotModifiedResponseHeaders{ETag: &etag}}, nil
	}
	var body commitsapi.ListCommits200JSONResponse
	for i := 0; i < pp.Limit; i++ {
		body.Body.Items = append(body.Body.Items, struct {
			Sha string `json:"sha"`
		}{Sha: strings.Repeat("a", 40)})
	}
	next := httpapi.EncodeCursor(map[string]string{"s": "x"})
	body.Body.Page = httpapi.NewPageInfo(next, pp.Limit)
	body.Body.Freshness = httpapi.NewFreshness(httpapi.FreshnessView{Status: "fresh", Kind: "commits.view", FetchedAt: now, CheckedAt: now})
	body.Headers.ETag, body.Headers.Link = &etag, httpapi.Ptr(httpapi.NextLink(req, next))
	return body, nil
}

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func chain(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	feature{}.Mount(mux)
	mux.HandleFunc("GET /{path...}", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	rv, err := validate.RequestValidator(apispec.Public())
	if err != nil {
		t.Fatal(err)
	}
	var h http.Handler = httpapi.WithRequest(mux)
	h = httpapi.Gzip(httpapi.GzipOptions{})(h)
	h = httpapi.APIDefaults(httpapi.DefaultsOptions{})(h)
	h = rv(h)
	h = validate.ProblemMux(h)
	return h
}

func TestChain(t *testing.T) {
	doc := apitest.Load(t, apispec.Public())
	h := chain(t)
	do := func(method, target string, hdr ...string) (*httptest.ResponseRecorder, *http.Request) {
		r := httptest.NewRequest(method, target, nil).WithContext(logx.WithRequestID(t.Context(), "0b8e3f4e-6c3d-4c0e-9d0f-2a1b3c4d5e6f"))
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		apitest.ValidateResponse(t, doc, r, w)
		return w, r
	}

	w, _ := do("GET", "/api/v1/commits?limit=30", "Accept-Encoding", "gzip")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("Content-Encoding") != "gzip" ||
		w.Header().Get("ETag") != `W/"g1042-public"` || !strings.Contains(w.Header().Get("Link"), `rel="next"`) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	etag := w.Header().Get("ETag")
	if zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	} else if b, _ := io.ReadAll(zr); !bytes.Contains(b, []byte(`"status":"fresh"`)) {
		t.Fatalf("%s", b)
	}

	w, _ = do("GET", "/api/v1/commits", "If-None-Match", etag)
	if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") != etag {
		t.Fatalf("304: %d %v", w.Code, w.Header())
	}
	if w, _ = do("HEAD", "/api/v1/commits"); w.Code != 200 { // the ResponseRecorder keeps the body that net/http discards for HEAD
		t.Fatalf("HEAD %d", w.Code)
	}
	w, _ = do("GET", "/api/v1/commits?repo=missing")
	if w.Code != 404 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	w, _ = do("GET", "/api/v1/commits?repo=boom")
	if w.Code != 500 || strings.Contains(w.Body.String(), "hunter2") {
		t.Fatalf("internal error leaked: %d %s", w.Code, w.Body.String())
	}
	w, _ = do("GET", "/api/v1/commits?limit=999")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w, _ = do("GET", "/api/v1/nope")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w, _ = do("POST", "/api/v1/commits")
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal(w.Code, w.Header())
	}
}
