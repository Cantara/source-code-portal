package llms

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

func sitemapOptions() Options {
	o := options()
	o.LastCommit = func(e *snapshot.RepoEntry) time.Time {
		if e.Repo.Name == "Core" {
			return time.Date(2026, 10, 12, 8, 30, 0, 0, time.FixedZone("CEST", 2*3600))
		}
		return time.Time{}
	}
	return o
}

func sitemapFixture() *snapshot.Snapshot {
	s := fixture()
	core := s.Repos["acme/core"]
	docs, fresh := docsFacet.Get(core)
	docsFacet.Set(core, append(docs, Doc{Path: "docs/draft.md", Hidden: true}, Doc{Path: "docs/a b.md"}), fresh)
	return s
}

func TestSitemap(t *testing.T) {
	s := sitemapFixture()
	l := New(func(context.Context) *snapshot.Snapshot { return s }, "https://scp.example/", sitemapOptions())
	mux := http.NewServeMux()
	mux.Handle("GET /sitemap.xml", l.Sitemap())
	mux.Handle("GET /sitemaps/{file}", l.Sitemap())
	mux.Handle("GET /robots.txt", l.Robots())

	rec := get(mux, "/sitemap.xml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/xml; charset=utf-8" || rec.Header().Get("Cache-Control") != machineCache {
		t.Fatalf("sitemap: %d %v", rec.Code, rec.Header())
	}
	// The pages with the newest commit as lastmod; the README is the repository's page, a hidden document is left out.
	golden(t, "sitemap.xml", rec.Body.String())
	if again := get(mux, "/sitemap.xml", "If-None-Match", rec.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d", again.Code)
	}
	if rec := get(mux, "/sitemaps/1.xml"); rec.Code != http.StatusNotFound {
		t.Errorf("a part of a sitemap without parts: %d", rec.Code)
	}
	robots := get(mux, "/robots.txt")
	if robots.Body.String() != "User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: https://scp.example/sitemap.xml\n" ||
		robots.Header().Get("Cache-Control") != machineCache {
		t.Errorf("robots.txt: %q %v", robots.Body.String(), robots.Header())
	}

	// Without server.public_url, the request's origin.
	o := sitemapOptions()
	o.Origin = func(r *http.Request) string { return "http://" + r.Host }
	local := New(func(context.Context) *snapshot.Snapshot { return s }, "", o).Sitemap()
	if body := get(local, "/sitemap.xml").Body.String(); !strings.Contains(body, "<loc>http://example.com/repos/acme/core</loc>") {
		t.Errorf("origin from the request:\n%s", body)
	}
}

func TestSitemapIndex(t *testing.T) {
	s := sitemapFixture()
	l := New(func(context.Context) *snapshot.Snapshot { return s }, "https://scp.example", sitemapOptions())
	files := l.buildSitemap(s, "https://scp.example", 3)
	if len(files) != 4 { // 7 pages: the index and three parts
		t.Fatalf("%d files", len(files))
	}
	for _, want := range []string{"<sitemapindex", "<loc>https://scp.example/sitemaps/1.xml</loc>", "<loc>https://scp.example/sitemaps/3.xml</loc>",
		"<lastmod>2026-10-12T06:30:00Z</lastmod>"} {
		if !strings.Contains(string(files[0]), want) {
			t.Errorf("index lacks %s:\n%s", want, files[0])
		}
	}
	if n := strings.Count(string(files[1]), "<url>"); n != 3 {
		t.Errorf("first part: %d URLs", n)
	}
}
