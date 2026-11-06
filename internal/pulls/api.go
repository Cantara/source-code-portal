package pulls

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
	number, size := 1, defaultLimit
	if p.Page != nil {
		number = *p.Page
	}
	if p.Limit != nil {
		size = *p.Limit
	}
	switch {
	case number < 1 || number > maxPage:
		return badRequest("page must be 1 to 100000"), nil
	case size < 1 || size > maxLimit:
		return badRequest("limit must be 1 to 200"), nil
	case p.State != nil && !slices.Contains(States, State(*p.State)):
		return badRequest("unknown state"), nil
	}
	var scope []snapshot.RepoKey
	var refs []Ref
	if p.Group != nil {
		g, ok := s.Group(*p.Group)
		if !ok {
			return notFound("no such group"), nil
		}
		scope, refs = g.Members, OfGroup(s, g.ID)
	} else {
		refs = All(s)
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
			refs = OfRepo(s, key)
		}
		scope = []snapshot.RepoKey{key}
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
	total := len(kept)
	start := min((number-1)*size, total)
	end := min(start+size, total)
	page := oapi.PullPage{Pulls: make([]oapi.PullRequest, 0, end-start), Counts: counts,
		Page: oapi.Page{Number: number, Size: size, Total: total, Pages: max(1, (total+size-1)/size)}, Freshness: scopeFreshness(s, scope, now)}
	for _, r := range kept[start:end] {
		page.Pulls = append(page.Pulls, PullOf(r))
	}
	return oapi.ListPulls200JSONResponse{Body: page}, nil
}

// scopeFreshness is fresh when the pull requests of every repository in scope are fresh, unknown when the scope is empty and
// stale otherwise, with the oldest times.
func scopeFreshness(s *snapshot.Snapshot, scope []snapshot.RepoKey, now time.Time) oapi.Freshness {
	var fetched, checked time.Time
	seen, stale, never := false, false, false
	for _, k := range scope {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		_, f := Of(e)
		if len(scope) == 1 {
			return oapi.FreshnessOf(f, now)
		}
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
