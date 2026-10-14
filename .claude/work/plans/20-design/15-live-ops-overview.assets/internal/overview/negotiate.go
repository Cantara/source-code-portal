package overview

import (
	"context"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/routes"
	"github.com/cantara/source-code-portal/internal/scm"
)

func itoa(n int) string { return strconv.Itoa(n) }

func (s *Service) count(ctx context.Context, route, repr, src string) {
	s.negReq.Add(ctx, 1, metric.WithAttributes(attribute.String("route", route), attribute.String("representation", repr), attribute.String("source", src)))
}

// negotiatedRepo is `GET /repos/{repo}`: the SPA shell (404 status for an unknown or invisible repository, same body as
// every other 404 so invisibility is not distinguishable) or, when the client ranks text/markdown above text/html, the
// same body the .md URL serves.
func (s *Service) negotiatedRepo() http.Handler {
	html := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, prob := s.lookup(r); prob != nil {
			s.ShellNotFound(w, r)
			return
		}
		s.count(r.Context(), "repo", "html", "")
		s.d.Shell.ServeHTTP(w, r)
	})
	md := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo, prob := s.lookup(r)
		if prob != nil {
			httpapi.WriteProblem(w, r, *prob)
			return
		}
		snap := s.d.Snapshots.Current()
		if entries, _, ok := s.d.Docs.Index(snap, repo.Slug); ok && hasKind(entries, DocReadme) {
			s.count(r.Context(), "repo", "markdown", "readme")
			s.d.Readme.ServeHTTP(w, r) // the docs handler: Vary, canonical Link and the blob-keyed ETag are its own
			return
		}
		s.count(r.Context(), "repo", "markdown", "index")
		s.writeRepoIndex(w, r, repo.Slug, "text/markdown; charset=utf-8")
	})
	if !s.cfg.LLMs.Enabled { // llms.enabled=false switches the whole machine-text surface off, negotiated Markdown included (V15-22)
		return html
	}
	return httpapi.Negotiate(html, md, httpapi.WithAlternate(func(r *http.Request) string {
		repo, prob := s.lookup(r)
		if prob != nil {
			return ""
		}
		if entries, _, ok := s.d.Docs.Index(s.d.Snapshots.Current(), repo.Slug); ok && hasKind(entries, DocReadme) {
			return routes.ReadmeMD(string(repo.Slug))
		}
		return routes.LLMs("repo:" + string(repo.Slug))
	}))
}

// negotiatedGroup is `GET /groups/{groupId}`.
func (s *Service) negotiatedGroup() http.Handler {
	html := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.visibleGroup(r); !ok {
			s.ShellNotFound(w, r)
			return
		}
		s.count(r.Context(), "group", "html", "")
		s.d.Shell.ServeHTTP(w, r)
	})
	md := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, ok := s.visibleGroup(r)
		if !ok {
			httpapi.WriteProblem(w, r, httpapi.NotFound("group not found"))
			return
		}
		s.count(r.Context(), "group", "markdown", "index")
		s.writeGroupIndex(w, r, g.ID, "text/markdown; charset=utf-8")
	})
	if !s.cfg.LLMs.Enabled {
		return html
	}
	return httpapi.Negotiate(html, md, httpapi.WithAlternate(func(r *http.Request) string {
		if g, ok := s.visibleGroup(r); ok {
			return routes.LLMs(string(g.ID))
		}
		return ""
	}))
}

// negotiatedRest is `GET /repos/{repo}/{rest...}`: reached only when no explicit route matched, so the path is not a page
// shape of the SPA. The alias redirect has already run (AliasRedirect); what is left is a real 404 with the shell body.
func (s *Service) negotiatedRest(w http.ResponseWriter, r *http.Request) { s.ShellNotFound(w, r) }

// ShellNotFound answers with the SPA shell and status 404 (the SPA renders its NotFound page).
func (s *Service) ShellNotFound(w http.ResponseWriter, r *http.Request) {
	s.d.ShellNotFound.ServeHTTP(w, r)
}

// lookup resolves the {repo} path value through the only sanctioned resolver.
func (s *Service) lookup(r *http.Request) (scm.Repository, *httpapi.Problem) {
	return authz.LookupRepo(r.Context(), s.d.Snapshots.Current(), s.d.Slots.Repos, s.d.Authz, scm.Slug(r.PathValue("repo")))
}
