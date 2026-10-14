package webapp

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

//go:embed all:dist
var distFS embed.FS

func mux(t *testing.T) http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	h, err := New(sub, func(n string) string { return "default-src 'none'; script-src 'self' 'nonce-" + n + "'" })
	if err != nil {
		t.Fatal(err)
	}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) { // unknown API paths never fall to SPA
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
	})
	m.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("pong")) })
	m.HandleFunc("GET /badges/{repo}/{kind}", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("badge " + r.PathValue("repo"))) })
	m.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	m.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.Handle("GET /{path...}", h) // catch-all for SPA + static
	return m
}

func do(m http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func TestRoutes(t *testing.T) {
	m := mux(t)
	html := map[string]string{"Accept": "text/html,application/xhtml+xml"}
	for _, c := range []struct {
		method, url string
		hdr         map[string]string
		want        int
	}{
		{"GET", "/", html, 200}, {"GET", "/dashboard", html, 200}, {"GET", "/repos/foo/commits", html, 200},
		{"GET", "/api/v1/ping", nil, 200}, {"GET", "/api/v1/nope", html, 404}, {"GET", "/api/", html, 404},
		{"GET", "/badges/foo/issues.svg", nil, 200}, {"POST", "/webhooks/github", nil, 204},
		{"POST", "/dashboard", html, 405}, {"GET", "/assets/missing-abc.js", html, 404},
		{"GET", "/missing.png", html, 404}, {"GET", "/livez", nil, 200},
		{"GET", "/dashboard", map[string]string{"Accept": "application/json"}, 404},
	} {
		w := do(m, c.method, c.url, c.hdr)
		t.Logf("%-5s %-28s accept=%-22q -> %d  ct=%s", c.method, c.url, c.hdr["Accept"], w.Code, w.Header().Get("Content-Type"))
		if w.Code != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.url, w.Code, c.want)
		}
	}
}

func TestIndexNonce(t *testing.T) {
	m := mux(t)
	a := do(m, "GET", "/", map[string]string{"Accept": "text/html"})
	b := do(m, "GET", "/", map[string]string{"Accept": "text/html"})
	re := regexp.MustCompile(`nonce="([^"]+)"`)
	na, nb := re.FindStringSubmatch(a.Body.String()), re.FindStringSubmatch(b.Body.String())
	if na == nil || na[1] == nb[1] || strings.Contains(a.Body.String(), "__CSP_NONCE__") {
		t.Fatalf("nonce not unique/replaced: %v %v", na, nb)
	}
	if !strings.Contains(a.Header().Get("Content-Security-Policy"), "nonce-"+na[1]) {
		t.Fatal("header nonce != body nonce")
	}
	t.Logf("CC=%s CSP=%s", a.Header().Get("Cache-Control"), a.Header().Get("Content-Security-Policy"))
}

func TestAssets(t *testing.T) {
	m := mux(t)
	sub, _ := fs.Sub(distFS, "dist/assets")
	ents, _ := fs.ReadDir(sub, ".")
	var js string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "index-") && strings.HasSuffix(e.Name(), ".js") {
			js = "/assets/" + e.Name()
		}
	}
	for _, ae := range []string{"br, gzip", "gzip", "identity", "br;q=0, gzip"} {
		w := do(m, "GET", js, map[string]string{"Accept-Encoding": ae})
		t.Logf("AE=%-14q -> %d CE=%q len=%d CC=%q ETag=%s Vary=%s", ae, w.Code, w.Header().Get("Content-Encoding"), w.Body.Len(), w.Header().Get("Cache-Control"), w.Header().Get("ETag"), w.Header().Get("Vary"))
	}
	w := do(m, "GET", js, map[string]string{"Accept-Encoding": "br"})
	w2 := do(m, "GET", js, map[string]string{"Accept-Encoding": "br", "If-None-Match": w.Header().Get("ETag")})
	t.Logf("conditional -> %d", w2.Code)
	if w2.Code != 304 {
		t.Errorf("want 304 got %d", w2.Code)
	}
}
