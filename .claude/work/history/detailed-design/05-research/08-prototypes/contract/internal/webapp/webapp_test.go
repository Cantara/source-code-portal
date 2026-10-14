package webapp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"example.com/scp/internal/webapp"
	"example.com/scp/web"
)

func fsys() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>app")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
		"assets/app-abc123.js.br": {Data: []byte("BR")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}
}

func do(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandler(t *testing.T) {
	h := webapp.Handler(fsys(), webapp.Options{APIPrefixes: []string{"/api", "/badges", "/llms.txt", "/webhooks", "/livez", "/readyz"}, Build: "t1"})
	cases := []struct {
		name, path string
		hdr        map[string]string
		code       int
		cache, enc string
	}{
		{"root", "/", nil, 200, "no-cache", ""},
		{"deep link falls back", "/groups/foo/commits", nil, 200, "no-cache", ""},
		{"api is never html", "/api/v1/nope", nil, 404, "", ""},
		{"llms.txt is never html", "/llms.txt", nil, 404, "", ""},
		{"hashed asset immutable", "/assets/app-abc123.js", nil, 200, "public, max-age=31536000, immutable", ""},
		{"asset brotli", "/assets/app-abc123.js", map[string]string{"Accept-Encoding": "gzip, br"}, 200, "public, max-age=31536000, immutable", "br"},
		{"missing asset 404", "/assets/missing.js", nil, 404, "", ""},
		{"non-assets file revalidates", "/favicon.svg", nil, 200, "no-cache", ""},
	}
	for _, c := range cases {
		w := do(h, "GET", c.path, c.hdr)
		if w.Code != c.code {
			t.Errorf("%s: code=%d want %d", c.name, w.Code, c.code)
		}
		if c.cache != "" && w.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: cache=%q want %q", c.name, w.Header().Get("Cache-Control"), c.cache)
		}
		if w.Header().Get("Content-Encoding") != c.enc {
			t.Errorf("%s: enc=%q want %q", c.name, w.Header().Get("Content-Encoding"), c.enc)
		}
	}
	if w := do(h, "POST", "/", nil); w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
}

func TestPlaceholderBuilds(t *testing.T) { // go test without Node: placeholder embedded
	h := webapp.Handler(web.Dist(), webapp.Options{})
	if w := do(h, "GET", "/anything", nil); w.Code != 200 {
		t.Fatalf("placeholder: %d", w.Code)
	}
}
