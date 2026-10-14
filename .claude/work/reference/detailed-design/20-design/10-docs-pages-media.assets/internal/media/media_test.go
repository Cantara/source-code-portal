package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

func pngBytes() []byte {
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes()
}

func TestCanonical(t *testing.T) {
	ok := map[string]string{
		"https://img.shields.io/badge/a-b-green.svg": "https://img.shields.io/badge/a-b-green.svg",
		"https://IMG.Example.org:443/a.png#frag":     "https://img.example.org/a.png",
		"https://example.org":                        "https://example.org/",
		"https://cdn.example.org/a.png?x=1&y=%20":    "https://cdn.example.org/a.png?x=1&y=%20",
		"https://xn--bcher-kva.example/a.png":        "https://xn--bcher-kva.example/a.png",
	}
	for in, want := range ok {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"http://example.org/a.png", "https://127.0.0.1/a.png", "https://[::1]/a.png", "https://2130706433/a.png", "https://0x7f.0.0.1/a.png",
		"https://127.1/a.png", "https://localhost/a.png", "https://intranet/a.png", "https://svc.internal/a.png", "https://a.local/a.png",
		"https://user:pw@example.org/a.png", "https://example.org:8443/a.png", "https://exa mple.org/", "https://example.org/a\\b", "ftp://example.org/x",
		"https://example.org/\x00", "//example.org/a.png", "https://169.254.169.254/latest", "https://bücher.example/a.png", "https://example.org./a.png.",
		"https://-bad.example.org/", "javascript:alert(1)", "data:image/png;base64,AAAA", "https://" + strings.Repeat("a", 64) + ".example.org/",
		"https://example.org/" + strings.Repeat("a", 2000),
	} {
		got, err := Canonical(in)
		if in == "https://example.org./a.png." {
			if err != nil || got != "https://example.org/a.png." {
				t.Errorf("trailing dot host: %q %v", got, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("Canonical(%q) accepted: %q", in, got)
		}
	}
}

func TestSignVerify(t *testing.T) {
	k1, k2 := secret.New(strings.Repeat("a", 32)), secret.New(strings.Repeat("b", 32))
	s1, _, _ := NewSigner(k1, nil)
	p, ok := s1.Sign("https://img.example.org/a.png#x")
	if !ok || !strings.HasPrefix(p, "/media/ext/") {
		t.Fatal(p, ok)
	}
	parts := strings.Split(p, "/")
	sig, id := parts[3], parts[4]
	if u, err := s1.Verify(sig, id); err != nil || u != "https://img.example.org/a.png" {
		t.Fatal(u, err)
	}
	if _, err := s1.Verify(sig, id+"A"); err == nil {
		t.Fatal("tampered id")
	}
	if _, err := s1.Verify(sig[:len(sig)-1]+"A", id); err == nil {
		t.Fatal("tampered sig")
	}
	// rotation: new current key, old key kept as previous
	s2, _, _ := NewSigner(k2, []secret.Secret{k1})
	if _, err := s2.Verify(sig, id); err != nil {
		t.Fatal("previous key rejected", err)
	}
	s3, _, _ := NewSigner(k2, nil)
	if _, err := s3.Verify(sig, id); err == nil {
		t.Fatal("removed key accepted")
	}
	if s1.KeyID() == s2.KeyID() || len(s1.KeyID()) != 8 {
		t.Fatal("key id")
	}
	// a validly signed id that is not canonical is refused (defence against a signer bug)
	raw := "https://img.example.org:443/a.png"
	bad := s1.sigFor(raw)
	if _, err := s1.Verify(bad[0], bad[1]); err == nil {
		t.Fatal("non-canonical signed id accepted")
	}
	if _, ok := s1.Sign("http://x.example.org/"); ok {
		t.Fatal("http signed")
	}
	if _, eph, _ := NewSigner(secret.Secret{}, nil); !eph {
		t.Fatal("ephemeral")
	}
}

func TestSniff(t *testing.T) {
	cases := map[string]Kind{
		"png": KindPNG, "svg": KindSVG, "svgdecl": KindSVG, "html": KindUnknown, "text": KindUnknown, "svgfake": KindUnknown, "empty": KindUnknown, "pdf": KindUnknown, "gif": KindGIF,
	}
	in := map[string][]byte{
		"png": pngBytes(), "svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1"/>`),
		"svgdecl": []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- c --><!DOCTYPE svg PUBLIC \"x\" \"y\">\n<svg xmlns=\"http://www.w3.org/2000/svg\"/>"),
		"html":    []byte("<!doctype html><svg></svg>"), "text": []byte("hello"), "svgfake": []byte("<div><svg/></div>"), "empty": nil,
		"pdf": []byte("%PDF-1.4"), "gif": []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
	}
	for n, want := range cases {
		if got := Sniff(in[n]); got != want {
			t.Errorf("Sniff(%s) = %q, want %q", n, got, want)
		}
	}
}

func TestLRUAndFlight(t *testing.T) {
	c := newLRU(1000, 0)
	for i := 0; i < 20; i++ {
		c.put(&entry{key: string(rune('a' + i)), body: make([]byte, 100), expires: farFuture})
	}
	if b, n := c.stats(); b > 1000 || n == 0 || n >= 20 {
		t.Fatal(b, n)
	}
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		f := newFlight()
		gate := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				f.do(context.Background(), "k", func() (*entry, error) { calls.Add(1); <-gate; return &entry{}, nil })
			}()
		}
		synctest.Wait() // leader blocked in fn, seven followers blocked on done
		close(gate)
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("fn ran %d times, want 1", calls.Load())
		}
	})
}

type fakeClient struct {
	mu    sync.Mutex
	calls []string
	fn    func(req *http.Request) (*http.Response, error)
}

func (f *fakeClient) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req.URL.String())
	f.mu.Unlock()
	return f.fn(req)
}

func resp(status int, body []byte, hdr ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(hdr); i += 2 {
		h.Set(hdr[i], hdr[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

type fakeImages struct {
	f   scm.File
	err error
}

func (f fakeImages) ReadRepoImage(context.Context, scm.RepoRef, string, string, int64) (scm.File, error) {
	return f.f, f.err
}

func newSvc(t *testing.T, cl HTTPDoer, mod func(*Config)) (*Service, *readmodel.Publisher, readmodel.Slot[freshness.Value[scm.Repository]]) {
	t.Helper()
	reg := &readmodel.Registry{}
	slot := readmodel.Register[freshness.Value[scm.Repository]](reg, "catalog.repo", readmodel.Existence())
	reg.Freeze()
	pub := readmodel.NewPublisher(reg, nil, nil, readmodel.Deps{})
	cfg := DefaultConfig()
	cfg.SigningKey = secret.New(strings.Repeat("k", 32))
	if mod != nil {
		mod(&cfg)
	}
	s, err := New(Deps{Cfg: cfg, Images: fakeImages{f: scm.File{SHA: "abc", Data: pngBytes()}}, Client: cl, Pub: pub, Repos: slot, Authz: authz.PublicOnly{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return s, pub, slot
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	mux := http.NewServeMux()
	mux.Handle("GET /media/ext/{sig}/{id}", h)
	mux.Handle("GET /media/{repo}/{ref}/{path...}", h)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestExternalFetch(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	cl := &fakeClient{fn: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/ok.png":
			return resp(200, pngBytes(), "Content-Type", "text/html"), nil // the lie is ignored
		case "/html.png":
			return resp(200, []byte("<html><script>alert(1)</script></html>"), "Content-Type", "image/png"), nil
		case "/badge.svg":
			return resp(200, svg, "Content-Type", "image/svg+xml"), nil
		case "/big.png":
			return resp(200, append(pngBytes(), make([]byte, 3<<20)...)), nil
		case "/gone.png":
			return resp(404, nil), nil
		case "/redir.png":
			return resp(302, nil, "Location", "https://evil.example.net/x.png"), nil
		case "/redir-ok.png":
			return resp(302, nil, "Location", "/ok.png"), nil
		case "/loop.png":
			return resp(302, nil, "Location", "/loop.png"), nil
		case "/five.png":
			return resp(503, nil), nil
		}
		return resp(404, nil), nil
	}}
	s, _, _ := newSvc(t, cl, func(c *Config) { c.AllowHosts = []string{"img.example.org", "*.cdn.example.org"} })
	sign := func(u string) string {
		p, ok := s.SignExternal(u)
		if !ok {
			t.Fatal(u)
		}
		return p
	}
	h := s.External()

	w := get(h, sign("https://img.example.org/ok.png"))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || w.Header().Get("ETag") == "" {
		t.Fatal(w.Code, w.Header())
	}
	etag := w.Header().Get("ETag")
	if w2 := get(h, sign("https://img.example.org/ok.png"), "If-None-Match", etag); w2.Code != 304 {
		t.Fatal("conditional", w2.Code)
	}
	if len(cl.calls) != 1 {
		t.Fatalf("cache missed: %v", cl.calls)
	}
	for _, c := range []struct {
		url  string
		want int
	}{
		{"https://img.example.org/html.png", 404}, {"https://img.example.org/badge.svg", 404} /* deny */, {"https://img.example.org/big.png", 404},
		{"https://img.example.org/gone.png", 404}, {"https://img.example.org/redir.png", 404}, {"https://img.example.org/loop.png", 404},
		{"https://other.example.org/ok.png", 404} /* not allowed */, {"https://img.example.org/five.png", 503}, {"https://img.example.org/redir-ok.png", 200},
	} {
		if w := get(h, sign(c.url)); w.Code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.url, w.Code, c.want, w.Body.String())
		}
	}
	n := len(cl.calls)
	get(h, sign("https://img.example.org/gone.png"))
	if len(cl.calls) != n {
		t.Fatal("negative cache not used")
	}
	if w := get(h, "/media/ext/AAAA/BBBB"); w.Code != 404 {
		t.Fatal("bad signature", w.Code)
	}
	if w := get(h, sign("https://img.example.org/ok.png"), "Sec-Fetch-Dest", "document"); w.Code != 403 {
		t.Fatal("navigation allowed", w.Code)
	}
}

func TestExternalSVGSandboxed(t *testing.T) {
	cl := &fakeClient{fn: func(*http.Request) (*http.Response, error) {
		return resp(200, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)), nil
	}}
	s, _, _ := newSvc(t, cl, func(c *Config) { c.ExternalSVG = SVGSandboxed })
	p, _ := s.SignExternal("https://img.example.org/b.svg")
	w := get(s.External(), p)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/svg+xml" || w.Header().Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestRepoImages(t *testing.T) {
	s, pub, slot := newSvc(t, &fakeClient{fn: func(*http.Request) (*http.Response, error) { return resp(404, nil), nil }}, nil)
	_, err := pub.Apply(func(b *readmodel.Builder) error {
		return readmodel.Put(b, "demo", slot, freshness.Value[scm.Repository]{V: scm.Repository{Slug: "demo", Visibility: scm.VisPublic, Ref: scm.RepoRef{Source: "s", ID: 1, Owner: "o", Name: "demo"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	h := s.Repo()
	w := get(h, "/media/demo/"+sha+"/docs/img/a.png")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || w.Header().Get("ETag") != `"abc"` {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	for _, p := range []string{"/media/demo/main/a.png", "/media/demo/" + sha + "/a.txt", "/media/missing/" + sha + "/a.png"} {
		if w := get(h, p); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	// ServeMux cleans dot segments and double slashes with a 307 before any handler runs; validImagePath is the second line.
	for _, p := range []string{"/media/demo/" + sha + "/../a.png", "/media/demo/" + sha + "/a//b.png"} {
		if w := get(h, p); w.Code != 307 && w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	for _, p := range []string{"../a.png", "a//b.png", "/a.png", "a/./b.png", "a\\b.png", "a.png/", ""} {
		if validImagePath(p) {
			t.Errorf("validImagePath(%q)", p)
		}
	}
}

// TestFlightLeaderPanic pins V-03: the design claimed a leader panic became an error for every follower; the first code
// closed the call with a nil entry and a nil error, which the handler dereferenced.
func TestFlightLeaderPanic(t *testing.T) {
	f := newFlight()
	func() {
		defer func() { _ = recover() }()
		f.do(context.Background(), "p", func() (*entry, error) { panic("boom") })
	}()
	if f.size() != 0 {
		t.Fatal("flight entry leaked after a panic")
	}
	started, release := make(chan struct{}), make(chan struct{})
	go func() {
		defer func() { _ = recover() }()
		f.do(context.Background(), "k", func() (*entry, error) { close(started); <-release; panic("boom") })
	}()
	<-started
	followerErr := make(chan error, 1)
	go func() {
		_, err, shared := f.do(context.Background(), "k", func() (*entry, error) { return &entry{}, nil })
		if !shared {
			err = errors.New("follower became a leader")
		}
		followerErr <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the follower join the flight
	close(release)
	if err := <-followerErr; !errors.Is(err, errFlightPanic) {
		t.Fatalf("follower got %v, want errFlightPanic", err)
	}
}

func TestSVGWithInternalSubsetIsNotSVG(t *testing.T) {
	ok := `<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg"/>`
	bad := `<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaa"><!ENTITY b "&a;&a;&a;&a;">]><svg xmlns="http://www.w3.org/2000/svg">&b;</svg>`
	if Sniff([]byte(ok)) != KindSVG {
		t.Fatal("a plain SVG 1.1 doctype must stay SVG")
	}
	if Sniff([]byte(bad)) != KindUnknown {
		t.Fatal("an SVG with an internal DTD subset must be refused")
	}
}
