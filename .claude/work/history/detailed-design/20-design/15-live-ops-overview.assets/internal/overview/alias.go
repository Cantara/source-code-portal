package overview

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// AliasRedirect is the one slug-alias middleware (architecture 11.2, design 07 section 4.7). It wraps the public mux so
// that it runs BEFORE routing: ServeMux gives every explicit `/repos/{repo}/commits`, `/repos/{repo}/readme.md`,
// `/api/v1/repos/{repo}/docs` ... precedence over the catch-all `negotiated.repo.rest`, so an alias check inside that one
// handler would never see most renamed URLs.
//
// For GET and HEAD under `/repos/` and `/api/v1/repos/` whose first segment is a known alias it answers 308 with the same
// request addressed to the current slug (remaining path and query kept). It applies visibility first: when the TARGET is
// not visible to the principal it does nothing, and the request then reaches an ordinary 404 exactly as an unknown slug
// does (a redirect would reveal the existence and the new name of a repository the caller may not see). A slug that is
// both an alias and a live repository is live (the alias is ignored), and an alias chain is not followed: the catalogue
// stores `old -> current` flattened.
func (s *Service) AliasRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		var prefix string
		path := r.URL.EscapedPath()
		switch {
		case strings.HasPrefix(path, "/api/v1/repos/"):
			prefix = "/api/v1/repos/"
		case strings.HasPrefix(path, "/repos/"):
			prefix = "/repos/"
		default:
			next.ServeHTTP(w, r)
			return
		}
		seg, tail, hasTail := strings.Cut(path[len(prefix):], "/")
		old, err := url.PathUnescape(seg)
		if err != nil || seg == "" {
			next.ServeHTTP(w, r)
			return
		}
		target, result := s.resolveAlias(r.Context(), scm.Slug(old))
		if result != "" {
			s.aliasReq.Add(r.Context(), 1, metric.WithAttributes(attribute.String("result", result)))
		}
		if target == "" {
			next.ServeHTTP(w, r)
			return
		}
		loc := prefix + url.PathEscape(string(target))
		if hasTail {
			loc += "/" + tail
		}
		if r.URL.RawQuery != "" {
			loc += "?" + r.URL.RawQuery
		}
		h := w.Header()
		h.Set("Location", loc)
		if prefix == "/api/v1/repos/" {
			writeRenamed(w, r, string(old), string(target))
			return
		}
		h.Set("Cache-Control", s.textCacheControl())
		h.Set("Content-Length", "0")
		w.WriteHeader(http.StatusPermanentRedirect)
	})
}

// resolveAlias returns the current slug of an alias visible to the caller. result is "" when old is not an alias at all
// (the common case, not counted), "redirected", "hidden" (the target is invisible) or "live_wins".
func (s *Service) resolveAlias(ctx context.Context, old scm.Slug) (target scm.Slug, result string) {
	if _, err := scm.ParseSlug(string(old)); err != nil {
		return "", ""
	}
	snap := s.d.Snapshots.Current()
	aliases, ok := readmodel.GetGlobal(snap, s.d.Slots.Aliases)
	if !ok {
		return "", ""
	}
	cur, isAlias := aliases[old]
	if !isAlias {
		return "", ""
	}
	if snap.Has(readmodel.EntryID(old)) {
		return "", "live_wins"
	}
	if _, prob := authz.LookupRepo(ctx, snap, s.d.Slots.Repos, s.d.Authz, cur); prob != nil {
		return "", "hidden"
	}
	return cur, "redirected"
}

// writeRenamed writes the 308 problem document of design 07 section 4.7 through httpapi.WriteProblem, which accepts the 308 status
// for the code repo-renamed only (R-06-3, closed by the cross-design review CD-ER-02) and copies the Location header from the problem.
func writeRenamed(w http.ResponseWriter, r *http.Request, old, current string) {
	p := httpapi.RepoRenamed(w.Header().Get("Location"))
	p.Detail = old + " is now " + current + "."
	httpapi.WriteProblem(w, r, p)
}
