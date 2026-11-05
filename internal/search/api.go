package search

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// The API's paging (design/search.md): numbered pages of the best MaxHits.
const (
	defaultLimit = 25
	maxLimit     = 100
	suggestions  = 8
)

// API serves the search from the viewer's view. It implements the search methods of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
	Observe func(op string, d time.Duration)         // each answer, by op (search, suggest); nil: none
}

func (a API) observe(op string, start time.Time) {
	if a.Observe != nil {
		a.Observe(op, time.Since(start))
	}
}

func badRequest(detail string) oapi.BadRequestApplicationProblemPlusJSONResponse {
	return oapi.BadRequestApplicationProblemPlusJSONResponse(oapi.NewProblem(http.StatusBadRequest, "bad-query", "Bad Request", detail))
}

// Search answers GET /api/v1/search.
func (a API) Search(ctx context.Context, req oapi.SearchRequestObject) (oapi.SearchResponseObject, error) {
	defer a.observe("search", time.Now())
	p := req.Params
	number, size := 1, defaultLimit
	if p.Page != nil {
		number = *p.Page
	}
	if p.Limit != nil {
		size = *p.Limit
	}
	if number < 1 || (number-1)*size >= MaxHits && number > 1 || size < 1 || size > maxLimit {
		return oapi.Search400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: badRequest(
			"page must be from 1 within the best 1,000 hits, limit 1 to 100")}, nil
	}
	var only *Kind
	if p.In != nil {
		k, ok := KindOf(string(*p.In))
		if !ok {
			return oapi.Search400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: badRequest("in names no kind")}, nil
		}
		only = &k
	}
	q, err := Parse(p.Q)
	if err != nil {
		return oapi.Search400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: badRequest(problem(err))}, nil
	}
	out := oapi.SearchResult{Query: q.Text, Corrections: []oapi.SearchCorrection{}, Hits: []oapi.SearchHit{},
		Page: oapi.Page{Number: number, Size: size, Pages: 1}}
	ix := Of(a.Current(ctx))
	if ix == nil {
		return oapi.Search200JSONResponse{Body: out}, nil
	}
	r := ix.Search(ctx, q, only, (number-1)*size, size)
	out.Counts = oapi.SearchCounts{Repo: r.Counts[KindRepo], Group: r.Counts[KindGroup], Doc: r.Counts[KindDoc], Dep: r.Counts[KindDep],
		Package: r.Counts[KindPackage], Commit: r.Counts[KindCommit]}
	for _, c := range r.Corrections {
		out.Corrections = append(out.Corrections, oapi.SearchCorrection{From: c.From, To: c.To})
	}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, apiHit(h))
	}
	out.Page.Total, out.Page.Pages = r.Total, max(1, (r.Total+size-1)/size)
	return oapi.Search200JSONResponse{Body: out}, nil
}

// Suggest answers GET /api/v1/search/suggest.
func (a API) Suggest(ctx context.Context, req oapi.SuggestRequestObject) (oapi.SuggestResponseObject, error) {
	defer a.observe("suggest", time.Now())
	if _, err := Parse(req.Params.Q); err != nil {
		return oapi.Suggest400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: badRequest(problem(err))}, nil
	}
	out := oapi.Suggestions{Hits: []oapi.SearchHit{}}
	if ix := Of(a.Current(ctx)); ix != nil {
		for _, h := range ix.Suggest(ctx, req.Params.Q, suggestions) {
			out.Hits = append(out.Hits, apiHit(h))
		}
	}
	return oapi.Suggest200JSONResponse{Body: out}, nil
}

// problem is a query's error as the reader is told it.
func problem(err error) string {
	if errors.Is(err, ErrQuery) {
		return strings.TrimPrefix(err.Error(), ErrQuery.Error()+": ")
	}
	return err.Error()
}

func apiHit(h Hit) oapi.SearchHit {
	out := oapi.SearchHit{Kind: oapi.SearchKind(h.Kind.String()), Title: apiParts(h.Title), Url: h.URL, Snippet: apiParts(h.Snippet)}
	if h.Repo != "" {
		repo := string(h.Repo)
		out.Repo = &repo
	}
	if !h.At.IsZero() {
		at := h.At.UTC()
		out.At = &at
	}
	return out
}

func apiParts(ps []Part) []oapi.TextPart {
	out := make([]oapi.TextPart, 0, len(ps))
	for _, p := range ps {
		tp := oapi.TextPart{Text: p.Text}
		if p.Mark {
			tp.Mark = new(true)
		}
		out = append(out, tp)
	}
	return out
}
