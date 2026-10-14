package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// stub serves the snapshot endpoint for real and nothing else.
type stub struct {
	SnapshotAPI
	oapi.StrictServerInterface
}

func (s stub) GetSnapshot(ctx context.Context, r oapi.GetSnapshotRequestObject) (oapi.GetSnapshotResponseObject, error) {
	return s.SnapshotAPI.GetSnapshot(ctx, r)
}

func public(cur *snapshot.Snapshot) http.Handler {
	spa := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>")},
		"assets/app-1a2b3c.js": {Data: []byte("console.log(1)")},
	}
	return Public(spa, stub{SnapshotAPI: SnapshotAPI{Current: func() *snapshot.Snapshot { return cur }}})
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSPA(t *testing.T) {
	h := public(&snapshot.Snapshot{})
	tests := []struct {
		path, cache string
		status      int
	}{
		{"/", "no-cache", http.StatusOK},
		{"/assets/app-1a2b3c.js", "public, max-age=31536000, immutable", http.StatusOK},
		{"/assets/missing.js", "", http.StatusNotFound},
		{"/assets", "", http.StatusNotFound},
		{"/../index.html", "", http.StatusTemporaryRedirect},
	}
	for _, tc := range tests {
		rec := get(h, tc.path)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.status)
		}
		if got := rec.Header().Get("Cache-Control"); tc.cache != "" && got != tc.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tc.path, got, tc.cache)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", tc.path)
		}
	}
}

func TestSnapshotAndValidators(t *testing.T) {
	cur := &snapshot.Snapshot{Version: 3, BuiltAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), Catalogued: true,
		Sources: []snapshot.SourceStatus{{ID: "gh", Repos: 2, Fresh: snapshot.Freshness{State: snapshot.StateOK, CheckedAt: time.Now(), TTL: time.Hour}}}}
	h := public(cur)
	rec := get(h, "/api/v1/snapshot")
	var got oapi.SnapshotInfo
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("snapshot: %d %s", rec.Code, rec.Body)
	}
	if got.Version != 3 || !got.Ready || len(got.Sources) != 1 || got.Sources[0].Freshness.Status != "fresh" {
		t.Errorf("snapshot = %+v", got)
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("validators: ETag %q, Cache-Control %q", etag, rec.Header().Get("Cache-Control"))
	}
	if rec := get(h, "/api/v1/snapshot", "If-None-Match", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 ||
		rec.Header().Get("ETag") != etag {
		t.Errorf("If-None-Match: %d, ETag %q", rec.Code, rec.Header().Get("ETag"))
	}
	cur.Version = 4
	if rec := get(h, "/api/v1/snapshot", "If-None-Match", etag); rec.Code != http.StatusOK {
		t.Errorf("changed body answered %d", rec.Code)
	}
}

func TestAPINotFoundIsProblem(t *testing.T) {
	rec := get(public(&snapshot.Snapshot{}), "/api/v1/nope")
	var p oapi.Problem
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" ||
		json.Unmarshal(rec.Body.Bytes(), &p) != nil || p.Code != "not-found" || rec.Header().Get("ETag") != "" {
		t.Errorf("unknown API path: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestProbes(t *testing.T) {
	ready := false
	h := Admin(func() bool { return ready })
	if rec := get(h, "/livez"); rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("livez: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz while warming: %d", rec.Code)
	}
	ready = true
	if rec := get(h, "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("readyz when ready: %d", rec.Code)
	}
}
