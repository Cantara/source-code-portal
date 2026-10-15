package commits

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	maxPage      = 100000
)

// API serves the merged commit feeds from the current snapshot. It implements the commits methods of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

func badRequest(code, detail string) oapi.ListCommits400ApplicationProblemPlusJSONResponse {
	return oapi.ListCommits400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: oapi.BadRequestApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusBadRequest, code, "Bad Request", detail))}
}

func notFound(detail string) oapi.ListCommits404ApplicationProblemPlusJSONResponse {
	return oapi.ListCommits404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))}
}

func (a API) ListCommits(ctx context.Context, req oapi.ListCommitsRequestObject) (oapi.ListCommitsResponseObject, error) {
	s, now, p := a.Current(ctx), time.Now(), req.Params
	number, limit, bad := pageParams(p.Page, p.Limit, defaultLimit)
	if bad != "" {
		return badRequest("validation-failed", bad), nil
	}
	var scope []snapshot.RepoKey
	var refs []Ref
	if p.Group != nil {
		g, ok := s.Group(*p.Group)
		if !ok {
			return notFound("no such group"), nil
		}
		scope = g.Members
		refs, _ = feed(s, g.ID)
	} else {
		refs, _ = feed(s, "")
		for k := range s.Repos {
			scope = append(scope, k)
		}
	}
	if p.Repo != nil {
		owner, name, ok := strings.Cut(*p.Repo, "/")
		e, _ := s.Repo(snapshot.KeyOf(owner, name))
		if !ok || e == nil {
			return notFound("no such repository"), nil
		}
		key := e.Repo.Key()
		refs = nil
		if slices.Contains(scope, key) {
			cs, _ := Of(e)
			for i := range cs {
				refs = append(refs, Ref{Repo: key, Commit: &cs[i]})
			}
			slices.SortFunc(refs, compareRefs)
		}
		scope = []snapshot.RepoKey{key}
	}

	people := p.Automation != nil && !*p.Automation
	total := len(refs)
	if people {
		total = 0
		for _, r := range refs {
			if !Automated(r.Commit) {
				total++
			}
		}
	}
	start, end, pg := paging(total, number, limit)
	page := oapi.CommitPage{Commits: make([]oapi.Commit, 0, end-start), Page: pg, Freshness: scopeFreshness(s, scope, now)}
	i := 0 // the position among the commits the filter keeps
	for _, r := range refs {
		if i >= end {
			break
		}
		if people && Automated(r.Commit) {
			continue
		}
		if i >= start {
			page.Commits = append(page.Commits, commitOf(r.Repo, r.Commit))
		}
		i++
	}
	return oapi.ListCommits200JSONResponse{Body: page}, nil
}

// scopeFreshness is fresh when the commits of every repository in scope are fresh, unknown when the scope is empty and stale
// otherwise. The times are the oldest in scope; null when a repository was never fetched.
func scopeFreshness(s *snapshot.Snapshot, scope []snapshot.RepoKey, now time.Time) oapi.Freshness {
	if len(scope) == 1 {
		if e := s.Repos[scope[0]]; e != nil {
			_, f := Of(e)
			return oapi.FreshnessOf(f, now)
		}
	}
	var fetched, checked time.Time
	seen, stale, never := false, false, false
	for _, k := range scope {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		_, f := Of(e)
		stale = stale || f.Derive(now) != snapshot.StatusFresh
		never = never || f.FetchedAt.IsZero() || f.CheckedAt.IsZero()
		if !seen || f.FetchedAt.Before(fetched) {
			fetched = f.FetchedAt
		}
		if !seen || f.CheckedAt.Before(checked) {
			checked = f.CheckedAt
		}
		seen = true
	}
	out := oapi.Freshness{Status: "fresh"}
	switch {
	case !seen:
		return oapi.Freshness{Status: "unknown"}
	case stale:
		out.Status = "stale"
	}
	if !never {
		out.FetchedAt, out.CheckedAt = oapi.TimeOrNil(fetched), oapi.TimeOrNil(checked)
	}
	return out
}

// pageParams are the page number and size asked for (def when no limit), or what is wrong with them.
func pageParams(page, limit *int, def int) (number, size int, problem string) {
	number, size = 1, def
	if page != nil {
		number = *page
	}
	if limit != nil {
		size = *limit
	}
	switch {
	case number < 1 || number > maxPage:
		return 0, 0, "page must be 1 to 100000"
	case size < 1 || size > maxLimit:
		return 0, 0, "limit must be 1 to 200"
	}
	return number, size, ""
}

// paging is page number of total items, size per page: its bounds in the list (empty past the last page) and its description.
func paging(total, number, size int) (start, end int, p oapi.Page) {
	start = min((number-1)*size, total)
	end = min(start+size, total)
	return start, end, oapi.Page{Number: number, Size: size, Total: total, Pages: max(1, (total+size-1)/size)}
}
