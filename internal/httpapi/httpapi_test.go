package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPA(t *testing.T) {
	spa := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>")},
		"assets/app-1a2b3c.js": {Data: []byte("console.log(1)")},
	}
	h := Public(spa)
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
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.status)
		}
		if got := rec.Header().Get("Cache-Control"); tc.cache != "" && got != tc.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tc.path, got, tc.cache)
		}
	}
}

func TestLivez(t *testing.T) {
	rec := httptest.NewRecorder()
	Admin().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("livez: %d %q", rec.Code, rec.Body.String())
	}
}
