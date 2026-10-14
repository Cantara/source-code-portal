package httpapi_test

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/platform/logx"
)

func req(method, target string, hdr ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return r
}

func TestWriteProblem(t *testing.T) {
	r := req("GET", "/api/v1/x").WithContext(logx.WithRequestID(t.Context(), "0b8e3f4e-6c3d-4c0e-9d0f-2a1b3c4d5e6f"))
	w := httptest.NewRecorder()
	w.Header().Set("ETag", `"x"`)
	w.Header().Set("Content-Length", "99")
	httpapi.WriteProblem(w, r, httpapi.MethodNotAllowed("POST", "GET"))
	if w.Code != 405 || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Allow") != "GET, POST" ||
		w.Header().Get("ETag") != "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	var p map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p["type"] != "urn:scp:problem:method-not-allowed" || p["instance"] != "urn:uuid:0b8e3f4e-6c3d-4c0e-9d0f-2a1b3c4d5e6f" || p["code"] != "method-not-allowed" {
		t.Fatal(p)
	}
	w = httptest.NewRecorder()
	httpapi.WriteProblem(w, req("HEAD", "/api/v1/x"), httpapi.NotFound("nope"))
	if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD")
	}
	if got := httpapi.InstanceURN("abc123"); got != "urn:scp:request:abc123" {
		t.Fatal(got)
	}
	if httpapi.InstanceURN("a b") != "" {
		t.Fatal("unsafe id must be dropped")
	}
	w = httptest.NewRecorder()
	httpapi.WriteProblem(w, req("GET", "/"), httpapi.Problem{Status: 200})
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestCursor(t *testing.T) {
	type cur struct {
		T string `json:"t"`
		S string `json:"s"`
	}
	c := httpapi.EncodeCursor(cur{"2026-10-14T00:00:00Z", "abc"})
	var back cur
	if err := httpapi.DecodeCursor(c, &back); err != nil || back.S != "abc" {
		t.Fatal(err, back)
	}
	for _, bad := range []string{"", "!!", strings.Repeat("A", 513), "AAAA", httpapi.EncodeCursor(map[string]int{"x": 1})} {
		if err := httpapi.DecodeCursor(bad, &back); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if pp, p := httpapi.ParsePage(0, "", 50, 200); p != nil || pp.Limit != 50 {
		t.Fatal(pp, p)
	}
	if _, p := httpapi.ParsePage(201, "", 50, 200); p == nil || p.Errors[0].Pointer != "#/query/limit" {
		t.Fatal(p)
	}
	if _, p := httpapi.ParsePage(10, "bad cursor", 50, 200); p == nil || p.Code != "invalid-cursor" {
		t.Fatal(p)
	}
	q1, _ := url.ParseQuery("repo=a&group=b&limit=5")
	q2, _ := url.ParseQuery("group=b&repo=a&limit=50")
	if httpapi.QueryFingerprint(q1, "repo", "group") != httpapi.QueryFingerprint(q2, "group", "repo") {
		t.Fatal("fingerprint")
	}
}

func TestETag(t *testing.T) {
	if e := httpapi.WeakETag(1042, "all", "a b/c"); e != `W/"g1042-all-a%20b%2fc"` {
		t.Fatal(e)
	}
	cases := []struct {
		inm  string
		want bool
	}{
		{`W/"g1"`, true}, {`"g1"`, true}, {`"x", W/"g1"`, true}, {`*`, true}, {`"x,y", "z"`, false}, {`W/"g2"`, false}, {``, false}, {`garbage`, false},
	}
	for _, c := range cases {
		r := req("GET", "/")
		if c.inm != "" {
			r.Header.Set("If-None-Match", c.inm)
		}
		if got := httpapi.NotModified(r, `W/"g1"`); got != c.want {
			t.Errorf("%q: got %v", c.inm, got)
		}
	}
	if httpapi.NotModified(req("POST", "/", "If-None-Match", "*"), `W/"g1"`) {
		t.Fatal("POST")
	}
	if !httpapi.NotModified(req("GET", "/", "If-None-Match", `"a,b"`), `"a,b"`) {
		t.Fatal("comma in tag")
	}
}

func TestStatic(t *testing.T) {
	h := httpapi.Static(httpapi.ContentTypeYAML, []byte("openapi: 3.1.0\n"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/v1/openapi.yaml"))
	etag := w.Header().Get("ETag")
	if w.Code != 200 || etag == "" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal(w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/x", "If-None-Match", "W/"+etag))
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req("POST", "/x"))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestNegotiate(t *testing.T) {
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "html") })
	md := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "md") })
	h := httpapi.Negotiate(spa, md, httpapi.WithAlternate(func(r *http.Request) string { return r.URL.Path + "/readme.md" }))
	cases := map[string]string{
		"": "html", "*/*": "html", "text/*": "html",
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8": "html",
		"text/markdown": "md", "text/markdown, text/html;q=0.9": "md", "text/html, text/markdown": "html",
		"text/markdown;q=0.5, text/html": "html", "text/markdown;q=0": "html", "text/markdown, */*;q=0.1": "md",
		"application/json": "html", "text/markdown;q=bad, text/html": "html",
	}
	for accept, want := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", "/repos/x", "Accept", accept))
		if w.Body.String() != want {
			t.Errorf("Accept %q: got %q want %q", accept, w.Body.String(), want)
		}
		if w.Header().Get("Vary") != "Accept" || !strings.Contains(strings.Join(w.Header().Values("Link"), ","), "describedby") {
			t.Errorf("headers for %q: %v", accept, w.Header())
		}
	}
}

func TestGzip(t *testing.T) {
	big := strings.Repeat(`{"a":"bbbbbbbbbbbbbbbb"}`, 200)
	h := httpapi.Gzip(httpapi.GzipOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "{}")
		case "/big":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", `"strong"`)
			io.WriteString(w, big)
		case "/img":
			w.Header().Set("Content-Type", "image/svg+xml")
			io.WriteString(w, big)
		case "/pre":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "br")
			io.WriteString(w, big)
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: x\n\n")
			w.(http.Flusher).Flush()
		case "/304":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(304)
		}
	}))
	do := func(path, ae string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", path, "Accept-Encoding", ae))
		return w
	}
	w := do("/big", "gzip, br")
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("ETag") != `W/"strong"` || w.Header().Get("Content-Length") != "" {
		t.Fatal(w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != big {
		t.Fatal("round trip")
	}
	for path, wantCE := range map[string]string{"/small": "", "/img": "", "/pre": "br", "/stream": "", "/304": ""} {
		if got := do(path, "gzip").Header().Get("Content-Encoding"); got != wantCE {
			t.Errorf("%s: CE %q", path, got)
		}
	}
	if w := do("/small", "gzip"); w.Header().Get("Content-Length") != "2" || w.Body.String() != "{}" {
		t.Fatal("small passthrough", w.Header())
	}
	if w := do("/big", "identity"); w.Header().Get("Content-Encoding") != "" || w.Body.String() != big || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal("identity")
	}
	if w := do("/big", "gzip;q=0"); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0")
	}
	hh := httptest.NewRecorder()
	h.ServeHTTP(hh, req("HEAD", "/big", "Accept-Encoding", "gzip"))
	if hh.Header().Get("Content-Encoding") != "" {
		t.Fatal("HEAD")
	}
}

func TestAPIDefaults(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{}")
	})
	h := httpapi.APIDefaults(httpapi.DefaultsOptions{})(ok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/v1/x"))
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal(w.Header())
	}
	hp := httpapi.APIDefaults(httpapi.DefaultsOptions{Private: true})(ok)
	w = httptest.NewRecorder()
	hp.ServeHTTP(w, req("GET", "/api/v1/x", "Accept", "application/json"))
	if w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(w.Header())
	}
	for accept, code := range map[string]int{"text/csv": 406, "text/html,*/*;q=0.8": 200, "application/*": 200, "text/event-stream": 200, "junk": 200} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", "/api/v1/x", "Accept", accept))
		if w.Code != code {
			t.Errorf("%q: %d", accept, w.Code)
		}
	}
}

func TestRedirect(t *testing.T) {
	h := httpapi.Redirect(301, func(r *http.Request) string { return r.URL.Query().Get("to") }, 3600)
	for to, code := range map[string]int{"/dashboard": 301, "//evil.com": 404, "http://evil.com": 404, "/\\evil": 404, "": 404} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req("GET", "/x?to="+url.QueryEscape(to)))
		if w.Code != code {
			t.Errorf("%q -> %d", to, w.Code)
		}
	}
}

func TestNextLink(t *testing.T) {
	r := req("GET", "/api/v1/commits?limit=5&repo=a%20b")
	if got := httpapi.NextLink(r, "AbC_-"); got != `</api/v1/commits?cursor=AbC_-&limit=5&repo=a+b>; rel="next"` {
		t.Fatal(got)
	}
}

func TestGzipKeepsResponseControllerAndRange(t *testing.T) {
	var flushErr error
	h := httpapi.Gzip(httpapi.GzipOptions{})(httpapi.APIDefaults(httpapi.DefaultsOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, strings.Repeat("x", 3000))
		flushErr = http.NewResponseController(w).Flush() // reaches the real writer through both wrappers (Unwrap)
	})))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("GET", "/api/v1/x", "Accept-Encoding", "gzip"))
	if flushErr != nil {
		t.Fatalf("ResponseController.Flush through Gzip and APIDefaults: %v", flushErr)
	}
	rg := req("GET", "/api/v1/x", "Accept-Encoding", "gzip", "Range", "bytes=0-9")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, rg)
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatal("a Range request must not be compressed")
	}
}

func TestCursorStrictness(t *testing.T) {
	enc := func(b ...byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	var into struct {
		T string `json:"t"`
	}
	good := enc(append([]byte{1}, `{"t":"a"}`...)...)
	if err := httpapi.DecodeCursor(good, &into); err != nil || into.T != "a" {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"unknown version": enc(append([]byte{2}, `{"t":"a"}`...)...),
		"trailing data":   enc(append([]byte{1}, `{"t":"a"} {"t":"b"}`...)...),
		"unknown field":   enc(append([]byte{1}, `{"t":"a","x":1}`...)...),
		"padding":         good + "==",
		"not json":        enc(1, 'x'),
		"version only":    enc(1),
	} {
		if err := httpapi.DecodeCursor(bad, &into); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestETagKindsAndDigest(t *testing.T) {
	if e := httpapi.WeakETagOf(httpapi.ETagRepo, 17, "all"); e != `W/"r17-all"` {
		t.Fatal(e)
	}
	if e := httpapi.WeakETagOf(httpapi.ETagVersion, 1841); e != `W/"v1841"` {
		t.Fatal(e)
	}
	if d := httpapi.FreshnessDigest("fresh", "fresh"); d != "" {
		t.Fatalf("all fresh must not add a suffix: %q", d)
	}
	fresh := httpapi.WithDigest(httpapi.WeakETag(7, "all"), httpapi.FreshnessDigest("fresh", "fresh"))
	stale := httpapi.WithDigest(httpapi.WeakETag(7, "all"), httpapi.FreshnessDigest("fresh", "stale"))
	other := httpapi.WithDigest(httpapi.WeakETag(7, "all"), httpapi.FreshnessDigest("stale", "fresh"))
	if fresh != `W/"g7-all"` || fresh == stale || stale == other || !strings.HasPrefix(stale, `W/"g7-all.`) || !strings.HasSuffix(stale, `"`) {
		t.Fatalf("%s %s %s", fresh, stale, other)
	}
}
