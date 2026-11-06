package pulls

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves the open pull requests from the current snapshot. It implements the pulls method of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
	Off     bool                                     // switched off (features.pulls): every request is answered 404
}

func badRequest(detail string) oapi.ListPulls400ApplicationProblemPlusJSONResponse {
	return oapi.ListPulls400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: oapi.BadRequestApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusBadRequest, "validation-failed", "Bad Request", detail))}
}

func notFound(detail string) oapi.ListPulls404ApplicationProblemPlusJSONResponse {
	return oapi.ListPulls404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))}
}

func (a API) ListPulls(ctx context.Context, req oapi.ListPullsRequestObject) (oapi.ListPullsResponseObject, error) {
	if a.Off {
		return oapi.ListPulls404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
			oapi.NewProblem(http.StatusNotFound, "switched-off", "Not Found", "The open pull requests are switched off on this portal."))}, nil
	}
	s, now, p := a.Current(ctx), time.Now(), req.Params
	number, size, bad := scope.Params(p.Page, p.Limit, scope.DefaultLimit)
	switch {
	case bad != "":
		return badRequest(bad), nil
	case p.State != nil && !slices.Contains(States, State(*p.State)):
		return badRequest("unknown state"), nil
	}
	refs, in, missing := scope.Of(s, p.Group, p.Repo, scope.Feed[Ref]{All: All, OfGroup: OfGroup, OfRepo: OfRepo})
	if missing != "" {
		return notFound(missing), nil
	}
	by := ByAll
	if p.By != nil {
		switch *p.By {
		case oapi.ListPullsParamsByPeople:
			by = ByPeople
		case oapi.ListPullsParamsByAutomation:
			by = ByAutomation
		default:
			return badRequest("by must be people or automation"), nil
		}
	}
	counts := Counts(refs, by)
	drafts := p.Drafts != nil && *p.Drafts
	kept := make([]Ref, 0, len(refs))
	for _, r := range refs {
		switch {
		case !by.keeps(r):
		case p.State != nil && r.State != State(*p.State):
		case p.State == nil && !drafts && r.State == StateDraft:
		default:
			kept = append(kept, r)
		}
	}
	if p.Sort != nil {
		switch *p.Sort {
		case oapi.Newest:
			kept = slices.SortedStableFunc(slices.Values(kept), compareNewest)
		case oapi.Updated:
			kept = slices.SortedStableFunc(slices.Values(kept), compareUpdated)
		}
	}
	start, end, pg := scope.Paging(len(kept), number, size)
	page := oapi.PullPage{Pulls: make([]oapi.PullRequest, 0, end-start), Counts: counts,
		Page: pg, Freshness: scope.Freshness(s, in, now, freshOf)}
	for _, r := range kept[start:end] {
		page.Pulls = append(page.Pulls, PullOf(r))
	}
	return oapi.ListPulls200JSONResponse{Body: page}, nil
}

// freshOf is how fresh a repository's pull requests are.
func freshOf(e *snapshot.RepoEntry) snapshot.Freshness {
	_, f := Of(e)
	return f
}
