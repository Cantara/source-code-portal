package overview

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/routes"
)

// Sitemap protocol limits (sitemaps.org, verified 2026-10-14): at most 50,000 URLs and 50 MB uncompressed per file.
const (
	sitemapMaxURLs  = 50_000
	sitemapMaxBytes = 50 << 20
)

type smURL struct {
	loc     string
	lastmod time.Time
}

// buildSitemap lists, in priority order, the dashboard, the dependencies page when manifests are on, the group pages,
// the repository pages, the portal pages and finally the repository documents, until the 50,000-URL limit. Hidden
// repositories and documents are not listed; only what the view class may see is. Only <loc> and <lastmod> are written:
// Google ignores <priority> and <changefreq>.
func (v view) buildSitemap() (urls []smURL, cut bool) {
	add := func(path string, t time.Time) bool {
		if len(urls) >= sitemapMaxURLs {
			cut = true
			return false
		}
		urls = append(urls, smURL{loc: v.s.abs(path), lastmod: t})
		return true
	}
	add(routes.Dashboard(), v.meta.LastFullSync)
	if v.s.d.Manifests != nil && v.s.d.Manifests.Enabled() {
		add(routes.Dependencies(), time.Time{})
	}
	for _, g := range v.groupsWithRepos() {
		if !add(routes.Group(string(g.ID)), time.Time{}) {
			return
		}
	}
	rows := v.rows(v.m.Slugs(v.class))
	for _, r := range rows {
		if !add(routes.Repo(string(r.slug)), r.repo.V.PushedAt) {
			return
		}
	}
	pages := v.s.d.Pages.Pages(v.snap)
	slices.SortFunc(pages, func(a, b PageEntry) int { return strings.Compare(a.Slug, b.Slug) })
	for _, p := range pages {
		if !add(routes.Page(p.Slug), p.UpdatedAt) {
			return
		}
	}
	for _, r := range rows {
		for _, d := range r.docs {
			if d.Kind != DocDoc {
				continue
			}
			if !add(routes.RepoDoc(string(r.slug), d.Path), d.UpdatedAt) {
				return
			}
		}
	}
	return
}

func renderSitemap(urls []smURL, cut bool) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	if cut {
		b.WriteString("<!-- truncated at the 50,000 URL limit of the sitemap protocol -->\n")
	}
	for _, u := range urls {
		b.WriteString("  <url><loc>")
		_ = xml.EscapeText(&b, []byte(u.loc))
		b.WriteString("</loc>")
		if !u.lastmod.IsZero() {
			b.WriteString("<lastmod>" + u.lastmod.UTC().Format("2006-01-02T15:04:05Z") + "</lastmod>")
		}
		b.WriteString("</url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}

func (s *Service) sitemap(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Site.Sitemap || s.d.Private || s.d.PublicURL == nil { // the protocol requires absolute URLs
		httpapi.WriteProblem(w, r, httpapi.NotFound("no sitemap is published"))
		return
	}
	started := time.Now()
	urls, cut := s.newView(r).buildSitemap()
	body := renderSitemap(urls, cut)
	if len(body) > sitemapMaxBytes { // cannot happen below 50,000 short URLs; kept as the protocol's second limit
		cut = true
		for len(body) > sitemapMaxBytes && len(urls) > 1 {
			urls = urls[:len(urls)/2]
			body = renderSitemap(urls, true)
		}
	}
	s.smURLs.Record(r.Context(), int64(len(urls)))
	if cut {
		s.smCut.Add(r.Context(), 1)
		s.d.Log.Warn("sitemap truncated", "urls", len(urls))
	}
	s.writeXML(w, r, body, started)
}

func (s *Service) writeXML(w http.ResponseWriter, r *http.Request, body []byte, started time.Time) {
	_ = started
	etag := httpapi.StrongETag(body)
	h := w.Header()
	h.Set("Content-Type", "application/xml; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", etag)
	h.Set("Cache-Control", s.textCacheControl())
	if httpapi.NotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// robots is /robots.txt (RFC 9309). In a public deployment it allows the human pages and the text outputs and disallows
// the machine and binary paths; behind access control it disallows everything and names no sitemap. site.robots.extra_rules
// are appended after a blank line as a further group.
func (s *Service) robots(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	if s.d.Private {
		b.WriteString("Disallow: /\n")
	} else {
		for _, p := range []string{"/api/", "/badges/", "/media/", "/webhooks/", "/csp-reports"} {
			b.WriteString("Disallow: " + p + "\n")
		}
		if s.cfg.Site.Sitemap && s.d.PublicURL != nil {
			b.WriteString("\nSitemap: " + s.abs(routes.Sitemap()) + "\n")
		}
	}
	if len(s.cfg.Site.Robots.ExtraRules) > 0 {
		b.WriteString("\n" + strings.Join(s.cfg.Site.Robots.ExtraRules, "\n") + "\n")
	}
	body := []byte(b.String())
	etag := httpapi.StrongETag(body)
	h := w.Header()
	h.Set("Content-Type", ctText)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=3600")
	if httpapi.NotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
