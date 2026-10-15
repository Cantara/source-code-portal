package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestPages(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.Header().Set("X-Powered-By", "Next.js")
		w.Header().Set("Cache-Control", "private, no-cache, no-store, max-age=0, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/nope" || r.URL.Path == notFoundPath {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = io.WriteString(w, "<!doctype html>"+r.Host+r.URL.RequestURI())
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	o := testOptions()
	o.PublicURL, o.WebToken, o.FormTargets = "https://scp.example/portal", "t0ken", []string{"https://id.example"}
	o.Access = Access{UserHeader: "X-Forwarded-User", GroupsHeader: "X-Forwarded-Groups", PrivateGroups: []string{"*"}}
	h := Public(o, o.Pages(u), nil, stub{})

	r := httptest.NewRequest(http.MethodGet, "http://portal.local/repos/acme/core?tab=1", nil)
	r.RemoteAddr = "10.0.0.1:1" // a trusted proxy with an authenticated user
	r.Header.Set("X-Forwarded-User", "ann")
	r.Header.Set("X-Request-Id", "req-7")
	r.Header.Set(headerToken, "forged")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	csp := rec.Header().Get("Content-Security-Policy")
	nonce := regexp.MustCompile(`'nonce-([A-Za-z0-9+/=]{24})'`).FindStringSubmatch(csp)
	if rec.Code != http.StatusOK || rec.Body.String() != "<!doctype html>portal.local/repos/acme/core?tab=1" || nonce == nil ||
		len(rec.Header().Values("Content-Security-Policy")) != 1 || rec.Header().Get("X-Powered-By") != "" ||
		rec.Header().Get("Permissions-Policy") != permissionsPage || !strings.HasPrefix(rec.Header().Get("Cache-Control"), "private") ||
		!strings.Contains(csp, "; form-action 'self' https://id.example;") {
		t.Fatalf("page: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	for k, want := range map[string]string{"Content-Security-Policy": csp, headerToken: "t0ken", headerViewer: "private",
		headerOrigin: "https://scp.example", "X-Request-Id": "req-7", "X-Forwarded-User": "", "X-Forwarded-Proto": "http"} {
		if got := seen.Get(k); got != want {
			t.Errorf("upstream %s = %q, want %q", k, got, want)
		}
	}
	rec2 := get(h, "/")
	if c := rec2.Header().Get("Content-Security-Policy"); c == csp || seen.Get(headerViewer) != "public" {
		t.Errorf("second page: CSP %q (nonce reused?), viewer %q", c, seen.Get(headerViewer))
	}
	if rec := get(h, "/nope?x=1"); rec.Code != http.StatusNotFound || !strings.HasSuffix(rec.Body.String(), notFoundPath) {
		t.Errorf("a 404 page: %d %q, want the not-found page", rec.Code, rec.Body)
	}
	if rec := get(h, "/nope", "RSC", "1"); rec.Code != http.StatusNotFound || !strings.HasSuffix(rec.Body.String(), "/nope") {
		t.Errorf("a 404 client navigation: %d %q, want its own answer", rec.Code, rec.Body)
	}
	seen = nil
	for _, p := range []string{"/auth/login", "/badges/x", "/images/a/b/c/d", "/webhooks/github", "/.well-known/security.txt"} {
		if rec := get(h, p); rec.Code != http.StatusNotFound || seen != nil {
			t.Errorf("%s: %d, proxied %v", p, rec.Code, seen != nil)
		}
	}

	o.PublicURL = ""
	r = httptest.NewRequest(http.MethodGet, "http://portal.local/", nil)
	r.RemoteAddr = "10.0.0.1:1"
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := o.origin(r); got != "https://portal.local" {
		t.Errorf("origin behind a proxy: %q", got)
	}
	r.RemoteAddr = "203.0.113.9:1"
	if got := o.origin(r); got != "http://portal.local" {
		t.Errorf("origin from an untrusted peer: %q", got)
	}

	upstream.Close()
	if rec := get(h, "/"); rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "5" {
		t.Errorf("server down: %d %v", rec.Code, rec.Header())
	}
	if rec := get(Public(o, o.Pages(nil), nil, stub{}), "/"); rec.Code != http.StatusServiceUnavailable ||
		!strings.Contains(rec.Body.String(), "/llms.txt") {
		t.Errorf("no web UI: %d %q", rec.Code, rec.Body)
	}
}
