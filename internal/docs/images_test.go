package docs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

const gif = "GIF89a\x01\x00\x01\x00" // ASCII, so it survives the fake blob SHA in JSON

// imageSite is a loopback https server for external images; hits counts requests per path.
func imageSite(t *testing.T) (*httptest.Server, func(string) int64) {
	t.Helper()
	var mu gosync.Mutex
	hits := map[string]*atomic.Int64{}
	count := func(p string) *atomic.Int64 {
		mu.Lock()
		defer mu.Unlock()
		if hits[p] == nil {
			hits[p] = &atomic.Int64{}
		}
		return hits[p]
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count(r.URL.Path).Add(1)
		switch r.URL.Path {
		case "/badge.svg":
			time.Sleep(50 * time.Millisecond) // concurrent requests overlap
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = io.WriteString(w, `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html><script>x</script></html>")
		case "/down":
			w.WriteHeader(http.StatusInternalServerError)
		case "/to-http":
			http.Redirect(w, r, "http://127.0.0.1/x.png", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func(p string) int64 { return count(p).Load() }
}

func TestImages(t *testing.T) {
	site, hits := imageSite(t)
	r := &repo{files: map[string]string{
		"README.md": "![logo](img/logo.png) ![badge](" + site.URL + "/badge.svg) ![page](" + site.URL + "/page) ![down](" + site.URL +
			"/down) ![redirect](" + site.URL + "/to-http) ![text](notes.txt)\n",
		"img/logo.png": gif,
		"notes.txt":    "plain text",
	}}
	d := New(map[string]GitHub{"gh": r}, safehtml.New())
	res, p := run(t, d, store.Item{})
	pub := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	defer pub.Close()
	pub.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}},
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "secret", Visibility: "private"}},
		}, nil, snapshot.SourceStatus{ID: "gh"})
		d.publish(b, store.Item{Repo: "acme/x", State: res.State, Payload: res.Payload}, snapshot.Freshness{})
		d.publish(b, store.Item{Repo: "acme/secret", State: res.State, Payload: res.Payload}, snapshot.Freshness{})
	})
	pub.Flush()
	h := d.Images(func(context.Context) *snapshot.Snapshot { return pub.Current().Public() }, site.Client().Transport)
	mux := http.NewServeMux()
	mux.Handle("GET /images/{owner}/{name}/{id}", h)
	id := map[string]string{}
	for _, img := range p.Docs[0].Images {
		id[strings.TrimPrefix(img.Path+img.URL, site.URL+"/")] = img.ID
	}
	get := func(path string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if len(hdr) == 2 {
			req.Header.Set(hdr[0], hdr[1])
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	blobs := r.blobs
	rec := get("/images/acme/x/" + id["img/logo.png"])
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/gif" || rec.Body.String() != gif || etag == "" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") || rec.Header().Get("Cache-Control") != imageMaxAge {
		t.Fatalf("repository image: %d %v", rec.Code, rec.Header())
	}
	if rec := get("/images/acme/x/"+id["img/logo.png"], "If-None-Match", etag); rec.Code != http.StatusNotModified || r.blobs != blobs+1 {
		t.Errorf("revalidation: %d, %d blob fetches", rec.Code, r.blobs-blobs)
	}

	var wg gosync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if rec := get("/images/acme/x/" + id["badge.svg"]); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
				t.Errorf("external svg: %d %s", rec.Code, rec.Header().Get("Content-Type"))
			}
		})
	}
	wg.Wait()
	if n := hits("/badge.svg"); n != 1 {
		t.Errorf("external image fetched %d times, want once", n)
	}

	for name, tc := range map[string]struct {
		path string
		code int
	}{
		"not an image":      {"/images/acme/x/" + id["page"], http.StatusNotFound},
		"text file":         {"/images/acme/x/" + id["notes.txt"], http.StatusNotFound},
		"upstream failure":  {"/images/acme/x/" + id["down"], http.StatusBadGateway},
		"redirect to http":  {"/images/acme/x/" + id["to-http"], http.StatusBadGateway},
		"unknown id":        {"/images/acme/x/0123456789abcdef0123456789abcdef", http.StatusNotFound},
		"unknown repo":      {"/images/acme/none/" + id["img/logo.png"], http.StatusNotFound},
		"private to public": {"/images/acme/secret/" + id["img/logo.png"], http.StatusNotFound},
	} {
		if rec := get(tc.path); rec.Code != tc.code {
			t.Errorf("%s: %d, want %d", name, rec.Code, tc.code)
		}
	}
	if get("/images/acme/x/" + id["down"]); hits("/down") != 1 {
		t.Errorf("a failed fetch was not remembered: %d fetches", hits("/down"))
	}
}

func TestCacheBound(t *testing.T) {
	c := newCache(10)
	now := time.Now()
	c.put("a", item{body: []byte("aaaa")}, 0, now)
	c.put("b", item{body: []byte("bbbb")}, 0, now)
	c.get("a", now) // a is the most recent
	c.put("c", item{body: []byte("cccc")}, 0, now)
	if _, ok := c.get("b", now); ok {
		t.Error("the least recently used entry was kept")
	}
	if _, ok := c.get("a", now); !ok {
		t.Error("a recently used entry was evicted")
	}
	c.put("big", item{body: make([]byte, 11)}, 0, now)
	if _, ok := c.get("big", now); ok || c.entries.Size() > 10 {
		t.Errorf("an entry above the bound was kept, size %d", c.entries.Size())
	}
	c.put("t", item{body: []byte("t")}, time.Minute, now)
	if _, ok := c.get("t", now.Add(2*time.Minute)); ok {
		t.Error("an expired entry was served")
	}
}
