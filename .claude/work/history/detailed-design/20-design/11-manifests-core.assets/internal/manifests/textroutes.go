package manifests

import (
	"bytes"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Route names of the two server-only text routes this feature owns (the architecture's `llms` row is split so that each owner has its own name).
const (
	RouteNameRepoDepsMD  = "deps.repo.md"  // GET /repos/{repo}/dependencies.md
	RouteNameGroupDepsMD = "deps.group.md" // GET /groups/{groupId}/dependencies.md
)

// RepoDependenciesMD serves /repos/{repo}/dependencies.md. cmd/scp registers it with routes.Table.Handle(mux, RouteNameRepoDepsMD, h).
func (s *Service) RepoDependenciesMD() http.Handler {
	return s.guardText(func(w http.ResponseWriter, r *http.Request) {
		v := s.view(r)
		repo, f, ok := s.repoOf(w, r, v)
		if !ok {
			return
		}
		fr := s.aggregate(v.now, "manifests.repo", []freshness.Info{f.Info}, func(int) time.Time { return f.V.ParsedAt })
		etag := httpapi.WithDigest(httpapi.WeakETagOf(httpapi.ETagRepo, v.snap.Rev(readmodel.EntryID(repo.Slug)), classOrd(v.class)), httpapi.Digest(fr.digest(), graphBuild(v.graph)))
		s.writeMD(w, r, etag, string(repo.Slug), []MDRepo{{Slug: repo.Slug, Facts: f}}, v.graph, s.cfg.Output.LLMMaxRows, "/repos/"+string(repo.Slug)+"/dependencies.md")
	})
}

// GroupDependenciesMD serves /groups/{groupId}/dependencies.md: only repositories the caller may see, from the caller's graph variant.
func (s *Service) GroupDependenciesMD() http.Handler {
	return s.guardText(func(w http.ResponseWriter, r *http.Request) {
		v := s.view(r)
		gid := r.PathValue("groupId")
		set, ok := s.groupSet(v, gid)
		if !ok || set == nil {
			httpapi.WriteProblem(w, r, httpapi.NotFound("Group not found."))
			return
		}
		var repos []MDRepo
		ids, infos, parsed := s.contributing(v, set)
		for _, id := range ids {
			repo, _ := readmodel.Get(v.snap, id, s.d.Slots.Repos)
			f, _ := readmodel.Get(v.snap, id, s.d.Slots.Facts)
			repos = append(repos, MDRepo{Slug: repo.V.Slug, Facts: f})
		}
		fr := s.aggregate(v.now, "manifests.group", infos, func(i int) time.Time { return parsed[i] })
		slices.SortFunc(repos, func(a, b MDRepo) int { return strings.Compare(string(a.Slug), string(b.Slug)) })
		rows := s.cfg.Output.LLMMaxRows
		if n := (len(repos) + 9) / 10; n > 1 { // keep the document bounded: fewer rows per repository in large groups
			rows = max(10, rows/n)
		}
		etag := httpapi.WithDigest(httpapi.WeakETag(v.snap.Version(), classOrd(v.class)), httpapi.Digest(fr.digest(), graphBuild(v.graph)))
		s.writeMD(w, r, etag, gid, repos, v.graph, rows, "/groups/"+gid+"/dependencies.md")
	})
}

func graphBuild(g *Graph) string {
	if g == nil {
		return "0"
	}
	return fmt.Sprint(g.BuildID)
}

// guardText answers 404 feature-disabled like the JSON operations do.
func (s *Service) guardText(h http.HandlerFunc) http.Handler { return s.guard(h) }

func (s *Service) writeMD(w http.ResponseWriter, r *http.Request, etag, title string, repos []MDRepo, g *Graph, rows int, canonical string) {
	w.Header().Set("ETag", etag)
	if httpapi.NotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var buf bytes.Buffer
	if err := RenderDependenciesMD(&buf, title, repos, g, rows, s.d.PublicURL+canonical, time.Now()); err != nil {
		httpapi.WriteProblem(w, r, httpapi.Internal())
		return
	}
	cc := "no-cache"
	if s.d.Private {
		cc = "private, no-cache"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", cc)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Add("Vary", "Authorization, Cookie")
	_, _ = w.Write(buf.Bytes())
}

// DependsOnRepos lists the repositories (by slug, sorted) that the repository's units depend on through internal components (channels A, B, C).
func (g *Graph) DependsOnRepos(slug scm.Slug) []scm.Slug { return g.neighbourRepos(slug, g.Out, true) }

// UsedByRepos lists the repositories that depend on the repository's units.
func (g *Graph) UsedByRepos(slug scm.Slug) []scm.Slug { return g.neighbourRepos(slug, g.In, false) }

func (g *Graph) neighbourRepos(slug scm.Slug, adj func(UnitIdx) []UnitIdx, withLinks bool) []scm.Slug {
	if g == nil {
		return nil
	}
	ri, ok := g.RepoOf(slug)
	if !ok {
		return nil
	}
	set := map[RepoIdx]struct{}{}
	for u := g.repoStart[ri]; u < g.repoStart[ri+1]; u++ {
		for _, n := range adj(u) {
			if r := g.units[n].Repo; r != ri {
				set[r] = struct{}{}
			}
		}
	}
	for _, l := range g.repoLinks {
		if withLinks && g.units[l.From].Repo == ri && l.To != ri {
			set[l.To] = struct{}{}
		}
		if !withLinks && l.To == ri && g.units[l.From].Repo != ri {
			set[g.units[l.From].Repo] = struct{}{}
		}
	}
	out := make([]scm.Slug, 0, len(set))
	for r := range set {
		out = append(out, g.repos[r].Slug)
	}
	slices.Sort(out)
	return out
}
