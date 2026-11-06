package releases

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// API serves the recent releases from the current snapshot. It implements the releases method of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

// Ref is one release of a repository in the view: what a listing filters and sorts.
type Ref struct {
	Key    snapshot.RepoKey
	Name   string
	Groups []string
	Release
	latest bool // the repository's newest stable release
}

// refsOf is the releases of the repositories at keys, the newest first within each.
func refsOf(s *snapshot.Snapshot, keys []snapshot.RepoKey) []Ref {
	var out []Ref
	for _, k := range keys {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		set, _ := Of(e)
		if set == nil {
			continue
		}
		newest := set.Latest()
		for i := range set.Releases {
			out = append(out, Ref{Key: k, Name: e.Repo.Name, Groups: s.GroupsOf(k), Release: set.Releases[i], latest: &set.Releases[i] == newest})
		}
	}
	return out
}

func badRequest(detail string) oapi.ListReleases400ApplicationProblemPlusJSONResponse {
	return oapi.ListReleases400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: oapi.BadRequestApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusBadRequest, "validation-failed", "Bad Request", detail))}
}

func notFound(detail string) oapi.ListReleases404ApplicationProblemPlusJSONResponse {
	return oapi.ListReleases404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: oapi.NotFoundApplicationProblemPlusJSONResponse(
		oapi.NewProblem(http.StatusNotFound, "not-found", "Not Found", detail))}
}

// sinceOf reads a `since`: an RFC 3339 time, or a number of days with a trailing d ("30d"). The zero time is no bound.
func sinceOf(v string, now time.Time) (time.Time, bool) {
	if v == "" {
		return time.Time{}, true
	}
	if days, ok := strings.CutSuffix(v, "d"); ok {
		n, err := strconv.Atoi(days)
		return now.AddDate(0, 0, -n), err == nil && n >= 0 && n <= 36500
	}
	t, err := time.Parse(time.RFC3339, v)
	return t, err == nil
}

func (a API) ListReleases(ctx context.Context, req oapi.ListReleasesRequestObject) (oapi.ListReleasesResponseObject, error) {
	s, now, p := a.Current(ctx), time.Now(), req.Params
	number, size, bad := scope.Params(p.Page, p.Limit, scope.DefaultLimit)
	if bad != "" {
		return badRequest(bad), nil
	}
	variant := oapi.Latest
	if p.Variant != nil {
		variant = *p.Variant
		if !slices.Contains([]oapi.ReleaseVariant{oapi.Latest, oapi.All, oapi.Stable, oapi.Pre}, variant) {
			return badRequest("unknown variant"), nil
		}
	}
	var since time.Time
	if p.Since != nil {
		var ok bool
		if since, ok = sinceOf(*p.Since, now); !ok {
			return badRequest("since must be an RFC 3339 time or days such as 30d"), nil
		}
	}
	refs, in, missing := scope.Of(s, p.Group, p.Repo, scope.Feed[Ref]{
		All:     func(s *snapshot.Snapshot) []Ref { return refsOf(s, slices.Sorted(mapKeys(s))) },
		OfGroup: func(s *snapshot.Snapshot, id string) []Ref { g, _ := s.Group(id); return refsOf(s, g.Members) },
		OfRepo:  func(s *snapshot.Snapshot, k snapshot.RepoKey) []Ref { return refsOf(s, []snapshot.RepoKey{k}) },
	})
	if missing != "" {
		return notFound(missing), nil
	}
	var counts oapi.ReleaseCounts
	kept := make([]Ref, 0, len(refs))
	for _, r := range refs {
		if r.Published.Before(since) {
			continue
		}
		counts.All++
		if r.Pre {
			counts.Pre++
		} else {
			counts.Stable++
		}
		if r.latest {
			counts.Latest++
		}
		if keeps(variant, r) {
			kept = append(kept, r)
		}
	}
	slices.SortStableFunc(kept, order(p.Sort))
	start, end, pg := scope.Paging(len(kept), number, size)
	page := oapi.ReleasePage{Releases: make([]oapi.ReleaseEntry, 0, end-start), Counts: counts, Page: pg,
		Freshness: scope.Freshness(s, in, now, func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := Of(e); return f })}
	for _, r := range kept[start:end] {
		page.Releases = append(page.Releases, oapi.ReleaseEntry{Repo: string(r.Key), Name: r.Name, Groups: r.Groups, Tag: r.Tag, Title: r.Release.Name,
			Summary: r.Summary, Url: r.URL, PublishedAt: r.Published.UTC(), Prerelease: r.Pre, Author: r.Author, Bot: r.Bot, Assets: r.Assets})
	}
	return oapi.ListReleases200JSONResponse{Body: page}, nil
}

// keeps reports whether variant lists r.
func keeps(variant oapi.ReleaseVariant, r Ref) bool {
	switch variant {
	case oapi.Stable:
		return !r.Pre
	case oapi.Pre:
		return r.Pre
	case oapi.All:
		return true
	}
	return r.latest
}

// order is the order the sort names; ties fall to the newest, then the repository, so a page is stable.
func order(sort *oapi.ReleaseSort) func(x, y Ref) int {
	byRepo := func(x, y Ref) int { return cmp.Compare(x.Key, y.Key) }
	newest := func(x, y Ref) int {
		return cmp.Or(y.Published.Compare(x.Published), byRepo(x, y), cmp.Compare(x.Tag, y.Tag))
	}
	if sort == nil {
		return newest
	}
	switch *sort {
	case oapi.ReleaseSortOldest:
		return func(x, y Ref) int {
			return cmp.Or(x.Published.Compare(y.Published), byRepo(x, y), cmp.Compare(x.Tag, y.Tag))
		}
	case oapi.ReleaseSortRepo:
		return func(x, y Ref) int { return cmp.Or(byRepo(x, y), newest(x, y)) }
	case oapi.ReleaseSortGroup:
		return func(x, y Ref) int { return cmp.Or(cmp.Compare(firstGroup(x), firstGroup(y)), newest(x, y)) }
	}
	return newest
}

// firstGroup is the id of the first group a repository belongs to; repositories in none sort last.
func firstGroup(r Ref) string {
	if len(r.Groups) == 0 {
		return "￿"
	}
	return r.Groups[0]
}

// mapKeys is the repositories of the view, for a sorted walk.
func mapKeys(s *snapshot.Snapshot) func(yield func(snapshot.RepoKey) bool) {
	return func(yield func(snapshot.RepoKey) bool) {
		for k := range s.Repos {
			if !yield(k) {
				return
			}
		}
	}
}
