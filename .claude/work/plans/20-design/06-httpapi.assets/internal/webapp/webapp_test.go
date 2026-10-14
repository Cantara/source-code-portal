package webapp_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/cantara/source-code-portal/internal/webapp"
)

const routesJSON = `{"version":1,"buildId":"b1","routes":[
 {"pattern":"/dashboard","listeners":["public"]},
 {"pattern":"/groups/{groupId}","listeners":["public"]},
 {"pattern":"/groups/{groupId}/commits","listeners":["public"]},
 {"pattern":"/repos/{repo}","listeners":["public"]},
 {"pattern":"/repos/{repo}/docs/{path...}","listeners":["public"]},
 {"pattern":"/status","listeners":["admin"]}]}`

func gz(t *testing.T, b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

func dist(t *testing.T) fstest.MapFS {
	js := []byte(strings.Repeat("console.log('x');", 200))
	idx := []byte("<!doctype html><div id=root></div><script type=module src=/assets/index-AbCd1234.js></script>" + strings.Repeat("<!-- pad -->", 100))
	return fstest.MapFS{
		"index.html":                  {Data: idx},
		"index.html.gz":               {Data: gz(t, idx)},
		"routes.json":                 {Data: []byte(routesJSON)},
		"assets/index-AbCd1234.js":    {Data: js},
		"assets/index-AbCd1234.js.br": {Data: []byte("br-bytes")},
		"assets/index-AbCd1234.js.gz": {Data: gz(t, js)},
		"theme-init.js":               {Data: []byte("(function(){})()")},
		"favicon.svg":                 {Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
	}
}

func newH(t *testing.T, mut func(*webapp.Options), fs fstest.MapFS) *webapp.Handler {
	t.Helper()
	o := webapp.Options{BuildID: "b1", CheckBuild: true, Reserved: []string{"/api/", "/badges/", "/media/", "/webhooks/"}}
	if mut != nil {
		mut(&o)
	}
	h, err := webapp.New(fs, o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(h http.Handler, method, target string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRouting(t *testing.T) {
	h := newH(t, nil, dist(t))
	type tc struct {
		target string
		code   int
		shell  bool
		loc    string
	}
	for _, c := range []tc{
		{"/", 302, false, "/dashboard"},
		{"/dashboard", 200, true, ""},
		{"/groups/widget", 200, true, ""},
		{"/groups/widget/commits", 200, true, ""},
		{"/repos/next.js", 200, true, ""},               // dotted slug: shape wins over the extension rule
		{"/repos/socket.io/docs/a/b.md", 200, true, ""}, // {path...}
		{"/repos/a%2Fb", 200, true, ""},                 // %2F stays inside one segment
		{"/dashboard/", 308, false, "/dashboard"},
		{"/groups/x/", 308, false, "/groups/x"},
		{"/nope", 404, true, ""},                        // unknown shape: real 404 with the shell body
		{"/groups", 404, true, ""},                      // {groupId} needs a segment
		{"/groups//commits", 404, true, ""},             // empty segment
		{"/assets/missing-AbCd1234.js", 404, false, ""}, // stale deploy: never HTML
		{"/missing.png", 404, false, ""},
		{"/api/v1/anything", 404, false, ""},
		{"/badges/x/y.svg", 404, false, ""},
		{"/routes.json", 404, false, ""}, // build metadata is not served
		{"/status", 404, true, ""},       // admin-only shape on the public listener
		{"/theme-init.js", 200, false, ""},
		{"/favicon.svg", 200, false, ""},
		{"/index.html", 404, false, ""}, // the shell is only served through shapes
	} {
		w := get(h, "GET", c.target)
		if w.Code != c.code {
			t.Errorf("%s: %d want %d", c.target, w.Code, c.code)
			continue
		}
		if isShell := strings.Contains(w.Body.String(), `id=root`); isShell != c.shell {
			t.Errorf("%s: shell body %v want %v", c.target, isShell, c.shell)
		}
		if c.loc != "" && w.Header().Get("Location") != c.loc {
			t.Errorf("%s: Location %q", c.target, w.Header().Get("Location"))
		}
		if w.Header().Get("X-SCP-Build") != "b1" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: base headers %v", c.target, w.Header())
		}
	}
	if w := get(h, "POST", "/dashboard"); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Error("POST", w.Code)
	}
	if w := get(h, "HEAD", "/dashboard"); w.Code != 200 || w.Body.Len() != 0 {
		t.Error("HEAD shell")
	}
	if w := get(h, "HEAD", "/nope"); w.Code != 404 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Error("HEAD 404 shell")
	}
	// admin variant
	a := h.For(webapp.Admin)
	if get(a, "GET", "/status").Code != 200 || get(a, "GET", "/dashboard").Code != 404 {
		t.Error("admin shapes")
	}
	if w := get(a, "GET", "/"); w.Code != 302 || w.Header().Get("Location") != "/status" {
		t.Error("admin root", w.Header())
	}
}

func TestCaching(t *testing.T) {
	h := newH(t, nil, dist(t))
	w := get(h, "GET", "/assets/index-AbCd1234.js", "Accept-Encoding", "gzip, br")
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || w.Header().Get("Content-Encoding") != "br" ||
		!strings.HasSuffix(w.Header().Get("ETag"), `-br"`) || w.Header().Get("Vary") != "Accept-Encoding" || w.Body.String() != "br-bytes" {
		t.Fatal(w.Header(), w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if w2 := get(h, "GET", "/assets/index-AbCd1234.js", "Accept-Encoding", "br", "If-None-Match", etag); w2.Code != 304 {
		t.Fatal("conditional", w2.Code)
	}
	w = get(h, "GET", "/assets/index-AbCd1234.js", "Accept-Encoding", "gzip")
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("gzip")
	}
	zr, _ := gzip.NewReader(w.Body)
	if b, _ := io.ReadAll(zr); !strings.HasPrefix(string(b), "console.log") {
		t.Fatal("gzip body")
	}
	for _, ae := range []string{"", "identity", "br;q=0, gzip;q=0", "*;q=0"} {
		w = get(h, "GET", "/assets/index-AbCd1234.js", "Accept-Encoding", ae)
		if w.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(w.Body.String(), "console.log") {
			t.Errorf("AE %q: %q", ae, w.Header().Get("Content-Encoding"))
		}
	}
	if w = get(h, "GET", "/theme-init.js"); w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") == "" {
		t.Fatal(w.Header())
	}
	if w = get(h, "GET", "/dashboard", "Accept-Encoding", "gzip"); w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("shell gzip", w.Header())
	}
}

func TestCSPModes(t *testing.T) {
	want := map[webapp.CSPMode][2]string{
		webapp.CSPReportOnly:  {"", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html; report-to csp"},
		webapp.CSPEnforceCore: {"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; report-to csp", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html; report-to csp"},
		webapp.CSPEnforce:     {"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html; report-to csp", ""},
	}
	for mode, w := range want {
		h := newH(t, func(o *webapp.Options) { o.CSP = mode }, dist(t))
		rec := get(h, "GET", "/dashboard")
		if got := rec.Header().Get("Content-Security-Policy"); got != w[0] {
			t.Errorf("%s enforce:\n got %q\nwant %q", mode, got, w[0])
		}
		if got := rec.Header().Get("Content-Security-Policy-Report-Only"); got != w[1] {
			t.Errorf("%s report-only:\n got %q\nwant %q", mode, got, w[1])
		}
		if rec.Header().Get("Reporting-Endpoints") != `csp="/csp-reports"` || rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" {
			t.Errorf("%s: headers %v", mode, rec.Header())
		}
		if get(h, "GET", "/assets/index-AbCd1234.js").Header().Get("Content-Security-Policy") != "" {
			t.Error("assets carry no document CSP")
		}
	}
}

func TestNewRejectsBadDist(t *testing.T) {
	cases := map[string]func(fstest.MapFS){
		"source map":        func(m fstest.MapFS) { m["assets/index-AbCd1234.js.map"] = &fstest.MapFile{Data: []byte("{}")} },
		"unhashed asset":    func(m fstest.MapFS) { m["assets/app.js"] = &fstest.MapFile{Data: []byte("x")} },
		"unknown extension": func(m fstest.MapFS) { m["x.exe"] = &fstest.MapFile{Data: []byte("x")} },
		"second html":       func(m fstest.MapFS) { m["other.html"] = &fstest.MapFile{Data: []byte("x")} },
		"bad gzip":          func(m fstest.MapFS) { m["theme-init.js.gz"] = &fstest.MapFile{Data: []byte("nope")} },
		"orphan sibling":    func(m fstest.MapFS) { m["ghost.js.br"] = &fstest.MapFile{Data: []byte("x")} },
		"file under api":    func(m fstest.MapFS) { m["api/x.js"] = &fstest.MapFile{Data: []byte("x")} },
		"no routes.json":    func(m fstest.MapFS) { delete(m, "routes.json") },
		"bad routes.json":   func(m fstest.MapFS) { m["routes.json"] = &fstest.MapFile{Data: []byte(`{"version":2}`)} },
		"bad pattern": func(m fstest.MapFS) {
			m["routes.json"] = &fstest.MapFile{Data: []byte(`{"version":1,"routes":[{"pattern":"/a/{x...}/b","listeners":["public"]}]}`)}
		},
		"stale build": func(m fstest.MapFS) {
			m["routes.json"] = &fstest.MapFile{Data: []byte(strings.Replace(routesJSON, `"b1"`, `"old"`, 1))}
		},
	}
	for name, mut := range cases {
		m := dist(t)
		mut(m)
		if _, err := webapp.New(m, webapp.Options{BuildID: "b1", CheckBuild: true, Reserved: []string{"/api/"}}); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := webapp.New(dist(t), webapp.Options{}); err == nil {
		t.Error("empty Reserved must fail")
	}
	if _, err := webapp.New(dist(t), webapp.Options{Reserved: []string{"/badges/"}}); err == nil {
		t.Error("Reserved without /api/ must fail")
	}
	// precompression required
	m := dist(t)
	delete(m, "assets/index-AbCd1234.js.br")
	if _, err := webapp.New(m, webapp.Options{BuildID: "b1", Reserved: []string{"/api/"}, RequirePrecompressed: true}); err == nil {
		t.Error("missing .br must fail in release mode")
	}
}

func TestCSPReports(t *testing.T) {
	var got []webapp.CSPReport
	h := newH(t, func(o *webapp.Options) {
		o.OnCSPReport = func(_ context.Context, r webapp.CSPReport) { got = append(got, r) }
	}, dist(t))
	rep := h.CSPReports()
	post := func(ct, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/csp-reports", strings.NewReader(body))
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		rep.ServeHTTP(w, r)
		return w
	}
	w := post("application/reports+json", `[{"type":"csp-violation","body":{"documentURL":"https://scp.example/repos/x?token=secret#frag","blockedURL":"https://evil.example/a.js?x=1","effectiveDirective":"script-src-elem","disposition":"enforce","sourceFile":"https://scp.example/assets/a.js","lineNumber":7}},{"type":"deprecation","body":{}}]`)
	if w.Code != 204 || len(got) != 1 {
		t.Fatal(w.Code, got)
	}
	if got[0].DocumentPath != "/repos/x" || got[0].BlockedKind != "host:evil.example" || got[0].Directive != "script-src-elem" || got[0].Disposition != "enforce" || got[0].Line != 7 {
		t.Fatalf("%+v", got[0])
	}
	got = nil
	w = post("application/csp-report", `{"csp-report":{"document-uri":"https://scp.example/dashboard","blocked-uri":"inline","violated-directive":"style-src 'self'"}}`)
	if w.Code != 204 || len(got) != 1 || got[0].BlockedKind != "inline" || got[0].Directive != "other" && got[0].Directive != "style-src" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if post("application/csp-report", `{"csp-report":{}}`).Code != 204 {
		t.Fatal("empty report is accepted")
	}
	if post("text/plain", `x`).Code != 415 || post("application/csp-report", `nonsense`).Code != 400 {
		t.Fatal("bad input")
	}
	if post("application/csp-report", strings.Repeat("x", 70<<10)).Code != 413 {
		t.Fatal("oversize")
	}
	if w := get(rep, "GET", "/csp-reports"); w.Code != 405 {
		t.Fatal("GET")
	}
}

func TestRangeSymlinkAndConcurrency(t *testing.T) {
	h := newH(t, nil, dist(t))
	if w := get(h, "GET", "/theme-init.js", "Range", "bytes=0-3"); w.Code != 206 || w.Body.String() != "(fun" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	m := dist(t)
	m["link.js"] = &fstest.MapFile{Data: []byte("x"), Mode: fs.ModeSymlink}
	if _, err := webapp.New(m, webapp.Options{BuildID: "b1", CheckBuild: true, Reserved: []string{"/api/"}}); err == nil {
		t.Error("a symlink in dist must fail New")
	}
	admin := h.For(webapp.Admin)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, c := range []struct {
				h    http.Handler
				path string
				code int
			}{{h, "/dashboard", 200}, {h, "/status", 404}, {admin, "/status", 200}, {admin, "/dashboard", 404}, {h, "/assets/index-AbCd1234.js", 200}} {
				if w := get(c.h, "GET", c.path, "Accept-Encoding", "br"); w.Code != c.code {
					t.Errorf("%s: %d", c.path, w.Code)
				}
			}
		}()
	}
	wg.Wait()
}

// routes.Public().ReservedPrefixes() contains /assets/ (not an SPA page); bundle files there must still be served.
func TestAssetsServedUnderReservedAssetsPrefix(t *testing.T) {
	h := newH(t, func(o *webapp.Options) { o.Reserved = []string{"/api/", "/assets/"} }, dist(t))
	if w := get(h, "GET", "/assets/index-AbCd1234.js"); w.Code != 200 {
		t.Fatalf("asset: %d", w.Code)
	}
	if w := get(h, "GET", "/assets/missing-AbCd1234.js"); w.Code != 404 || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("missing asset: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}
