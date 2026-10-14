// Package overview (sketch): the renamed-repository redirect of design 07, section 4.7. The real package is rank 5 and owns
// `negotiated.repo.rest`, the SPA twin of this redirect, and reads the same `catalog.aliases` global (unit 08); this file shows
// only the API half, against the kernel types, so that its exact signatures are compile-checked.
package overview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// AliasLookup answers "is this slug a known alias of a repository the caller may see, and what is its current slug?".
type AliasLookup interface {
	Resolve(ctx context.Context, slug scm.Slug) (current scm.Slug, ok bool)
}

// AliasSource implements AliasLookup over one snapshot read. cmd/scp builds it: Snapshot is Publisher.Current, Aliases wraps
// readmodel.GetGlobal(s, catalog.Slots.Aliases), Repos is the catalogue's existence slot.
type AliasSource struct {
	Snapshot func() *readmodel.Snapshot
	Aliases  func(*readmodel.Snapshot) (map[scm.Slug]scm.Slug, bool)
	Repos    readmodel.Slot[freshness.Value[scm.Repository]]
	Authz    authz.Authorizer
}

func (a AliasSource) Resolve(ctx context.Context, slug scm.Slug) (scm.Slug, bool) {
	s := a.Snapshot()
	// 1. a slug that names a current repository (visible or not) is never an alias: the handler answers it (200 or the
	//    identical 404). A name that was reused by a new repository therefore stops redirecting.
	if _, exists := readmodel.Get(s, readmodel.EntryID(slug), a.Repos); exists {
		return "", false
	}
	m, ok := a.Aliases(s)
	if !ok {
		return "", false
	}
	cur, ok := m[slug]
	if !ok {
		return "", false
	}
	// 2. visibility first: an alias of a repository the caller cannot see is an unknown slug (404), never a redirect, because
	//    the Location would reveal both that the repository exists and its new name.
	if _, miss := authz.LookupRepo(ctx, s, a.Repos, a.Authz, cur); miss != nil {
		return "", false
	}
	return cur, true
}

const apiRepos = "/api/v1/repos/"

// AliasRedirect answers `308 Permanent Redirect` for GET and HEAD under /api/v1/repos/{alias}/..., keeping the rest of the path
// and the query string byte for byte. cmd/scp mounts it around the API mux, after the request validator (a malformed slug is a
// 400 there) and before the mux. The body is a problem document with code repo-renamed (clients that follow the redirect never
// read it). Unit 06's WriteProblem clamps the status to 400..599, so the response is written here.
func AliasRedirect(l AliasLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			esc := r.URL.EscapedPath()
			if (r.Method != http.MethodGet && r.Method != http.MethodHead) || !strings.HasPrefix(esc, apiRepos) {
				next.ServeHTTP(w, r)
				return
			}
			seg, tail, _ := strings.Cut(esc[len(apiRepos):], "/")
			raw, err := url.PathUnescape(seg)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if _, err := scm.ParseSlug(raw); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			cur, ok := l.Resolve(r.Context(), scm.Slug(raw))
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			loc := apiRepos + url.PathEscape(string(cur))
			if tail != "" || strings.HasSuffix(esc, "/") {
				loc += "/" + tail
			}
			if r.URL.RawQuery != "" {
				loc += "?" + r.URL.RawQuery
			}
			p := httpapi.New(http.StatusPermanentRedirect, "repo-renamed", raw+" is now "+string(cur)+".")
			p.Type, p.Title, p.Instance = httpapi.TypeURI(p.Code), "Repository renamed", httpapi.InstanceURN(requestID(r))
			b, _ := json.Marshal(p)
			h := w.Header()
			h.Set("Location", loc)
			h.Set("Content-Type", "application/problem+json")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Cache-Control", "no-store")
			h.Set("Content-Length", strconv.Itoa(len(b)))
			w.WriteHeader(http.StatusPermanentRedirect)
			if r.Method != http.MethodHead {
				_, _ = w.Write(b)
			}
		})
	}
}

func requestID(r *http.Request) string { return r.Header.Get("X-Request-Id") }
