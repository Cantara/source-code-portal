package llms

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// maxURLs is the most a sitemap file may list (sitemaps.org); beyond it /sitemap.xml is an index of /sitemaps/{n}.xml.
const maxURLs = 50_000

// machineCache is the caching of the files crawlers read: an hour in shared caches, served stale for a day while it revalidates.
const machineCache = "public, max-age=300, s-maxage=3600, stale-while-revalidate=86400"

type entry struct {
	loc     string
	lastmod time.Time
}

// sitemap is the files of one view of a snapshot at one origin: the URL set, or an index and its parts.
type sitemap struct {
	view   *snapshot.Snapshot
	origin string
	files  [][]byte // [0] is /sitemap.xml
}

// Sitemap serves /sitemap.xml, and /sitemaps/{n}.xml (the pattern's {file}) when the pages pass maxURLs.
func (l *LLMs) Sitemap() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		files := l.sitemaps(r)
		i := 0
		if file := r.PathValue("file"); file != "" {
			n, ok := strings.CutSuffix(file, ".xml")
			var err error
			if i, err = strconv.Atoi(n); !ok || err != nil || i < 1 || i >= len(files) || n != strconv.Itoa(i) {
				notFound(w)
				return
			}
		}
		serveAs(w, r, "application/xml; charset=utf-8", machineCache, files[i])
	})
}

// Robots serves /robots.txt: every page may be crawled but the API, and the sitemap is named.
func (l *LLMs) Robots() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := fmt.Sprintf("User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: %s/sitemap.xml\n", l.originOf(r))
		serveAs(w, r, "text/plain; charset=utf-8", machineCache, []byte(body))
	})
}

func (l *LLMs) originOf(r *http.Request) string {
	if l.base != "" || l.opts.Origin == nil {
		return l.base
	}
	return l.opts.Origin(r)
}

func (l *LLMs) sitemaps(r *http.Request) [][]byte {
	s, origin := l.current(r.Context()), l.originOf(r)
	slot := 0
	if s != s.Public() {
		slot = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if m := l.maps[slot]; m.view != s || m.origin != origin {
		l.maps[slot] = sitemap{view: s, origin: origin, files: l.buildSitemap(s, origin, maxURLs)}
	}
	return l.maps[slot].files
}

// pages are the indexable pages of a view: the dashboard, the commit log, the groups, the repositories and their documents, each
// with the time of its newest commit.
func (l *LLMs) pages(s *snapshot.Snapshot) []entry {
	last := map[snapshot.RepoKey]time.Time{}
	var newest time.Time
	keys := sortedKeys(s)
	for _, k := range keys {
		t := l.opts.LastCommit(s.Repos[k])
		last[k] = t
		if t.After(newest) {
			newest = t
		}
	}
	out := []entry{{"/", newest}, {"/commits", newest}}
	for _, g := range s.Groups {
		var t time.Time
		for _, k := range g.Members {
			if last[k].After(t) {
				t = last[k]
			}
		}
		out = append(out, entry{snapshot.GroupPage(g.ID), t})
	}
	for _, k := range keys {
		out = append(out, entry{snapshot.RepoPage(k), last[k]})
		docs, _ := l.opts.Docs(s.Repos[k])
		for _, d := range docs {
			if !d.README && !d.Hidden {
				out = append(out, entry{snapshot.DocPage(k, d.Path), last[k]})
			}
		}
	}
	return out
}

// buildSitemap is the URL set of s, or when it holds more than limit pages an index and its parts.
func (l *LLMs) buildSitemap(s *snapshot.Snapshot, origin string, limit int) [][]byte {
	pages := l.pages(s)
	if len(pages) <= limit {
		return [][]byte{urlset(origin, pages)}
	}
	var index bytes.Buffer
	files := [][]byte{nil}
	index.WriteString(xml.Header + `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for i := 0; i < len(pages); i += limit {
		part := pages[i:min(i+limit, len(pages))]
		files = append(files, urlset(origin, part))
		var t time.Time
		for _, p := range part {
			if p.lastmod.After(t) {
				t = p.lastmod
			}
		}
		fmt.Fprintf(&index, "<sitemap><loc>%s</loc>%s</sitemap>\n", escape(fmt.Sprintf("%s/sitemaps/%d.xml", origin, len(files)-1)), lastmod(t))
	}
	index.WriteString("</sitemapindex>\n")
	files[0] = index.Bytes()
	return files
}

func urlset(origin string, pages []entry) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "<url><loc>%s</loc>%s</url>\n", escape(origin+p.loc), lastmod(p.lastmod))
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}

func lastmod(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "<lastmod>" + t.UTC().Format(time.RFC3339) + "</lastmod>"
}

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
