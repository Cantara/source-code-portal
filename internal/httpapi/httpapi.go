// Package httpapi builds the HTTP handlers of the two listeners: the public port (web UI, API, badges, LLM surface, webhook)
// and the admin port (probes, metrics, profiling, status).
package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// Public returns the handler of the public listener. spa holds the web UI build with index.html at its root; api implements the
// contract in api/openapi.yaml.
func Public(spa fs.FS, api oapi.StrictServerInterface) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", spaHandler(spa))
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, "not-found", "No such API resource.")
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
	return headers(mux)
}

// Admin returns the handler of the admin listener. ready reports whether the portal serves a catalogue.
func Admin(ready func() bool) http.Handler {
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
	mux.HandleFunc("GET /livez", probe(func() bool { return true }))
	mux.HandleFunc("GET /readyz", probe(ready))
	return headers(mux)
}

// SnapshotAPI implements getSnapshot.
type SnapshotAPI struct {
	Current func() *snapshot.Snapshot
}

func (a SnapshotAPI) GetSnapshot(context.Context, oapi.GetSnapshotRequestObject) (oapi.GetSnapshotResponseObject, error) {
	s, now := a.Current(), time.Now()
	out := oapi.SnapshotInfo{Version: int64(s.Version), BuiltAt: oapi.TimeOrNil(s.BuiltAt), Ready: s.Catalogued, //nolint:gosec // G115: a count
		Sources: make([]oapi.SourceInfo, 0, len(s.Sources))}
	for _, src := range s.Sources {
		out.Sources = append(out.Sources, oapi.SourceInfo{Id: src.ID, Repos: src.Repos, Freshness: oapi.FreshnessOf(src.Fresh, now)})
	}
	return oapi.GetSnapshot200JSONResponse{Body: out}, nil
}

// WriteProblem writes an RFC 9457 problem with fixed text.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	h := w.Header()
	h.Del("ETag")
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oapi.Problem{Type: "urn:scp:problem:" + code, Title: http.StatusText(status), Status: status,
		Code: code, Detail: &detail})
}

// headers sets the headers every response carries and adds validators to API responses.
func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			conditional(w, r, next)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// conditional buffers a 200 response, gives it a weak ETag over its body and answers 304 when If-None-Match carries that ETag.
// The body includes every derived freshness status, so a change from fresh to stale changes the ETag.
func conditional(w http.ResponseWriter, r *http.Request, next http.Handler) {
	buf := &buffered{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(buf, r)
	h := w.Header()
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
	if matches(r.Header.Get("If-None-Match"), etag) {
		h.Del("Content-Type")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.body.Bytes())
}

func matches(inm, etag string) bool {
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

// spaHandler serves the files of the web UI build. Files under assets/ carry a content hash in their name and are cached for a
// year; everything else is revalidated on each use. The root serves index.html.
func spaHandler(spa fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(spa, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, spa, name) //nolint:gosec // G703: fs.FS rejects names that leave its root (fs.ValidPath)
	})
}
