package commits

import (
	"context"
	"net/http"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
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
	number, limit, bad := scope.Params(p.Page, p.Limit, scope.DefaultLimit)
	if bad != "" {
		return badRequest("validation-failed", bad), nil
	}
	refs, in, missing := scope.Of(s, p.Group, p.Repo, scope.Feed[Ref]{
		All:     func(s *snapshot.Snapshot) []Ref { r, _ := feed(s, ""); return r },
		OfGroup: func(s *snapshot.Snapshot, id string) []Ref { r, _ := feed(s, id); return r },
		OfRepo:  func(s *snapshot.Snapshot, k snapshot.RepoKey) []Ref { return feedIndex.Get(s).byRepo[k] },
	})
	if missing != "" {
		return notFound(missing), nil
	}

	keep := filterOf(p.Automation, p.Work)
	total := len(refs)
	if keep != nil {
		total = 0
		for _, r := range refs {
			if keep(r) {
				total++
			}
		}
	}
	start, end, pg := scope.Paging(total, number, limit)
	page := oapi.CommitPage{Commits: make([]oapi.Commit, 0, end-start), Page: pg, Freshness: scope.Freshness(s, in, now, freshOf)}
	i := 0 // the position among the commits the filter keeps
	for _, r := range refs {
		if i >= end {
			break
		}
		if keep != nil && !keep(r) {
			continue
		}
		if i >= start {
			page.Commits = append(page.Commits, commitOf(r))
		}
		i++
	}
	return oapi.ListCommits200JSONResponse{Body: page}, nil
}

// freshOf is how fresh a repository's commits are.
func freshOf(e *snapshot.RepoEntry) snapshot.Freshness {
	_, f := Of(e)
	return f
}

// filterOf is the commits a listing keeps (nil: every one): automation=false leaves out automation; work keeps the kinds it names.
func filterOf(automation *bool, work *oapi.Work) func(Ref) bool {
	w := WorkEvery
	if work != nil {
		w = WorkOf(*work, WorkEvery)
	}
	noBots := automation != nil && !*automation
	if w == WorkEvery && !noBots {
		return nil
	}
	return func(r Ref) bool { return (!noBots || r.Origin != OriginAutomation) && w.Counts(r.Origin, r.Person) }
}
