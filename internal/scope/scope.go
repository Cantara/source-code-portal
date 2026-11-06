// Package scope is what the listings of one feature's items (commits, pull requests, issues) share: which repositories a request's
// group and repository parameters name, the numbered page asked for and the freshness of the scope. Core: it knows no feature.
package scope

import (
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	// DefaultLimit is the page size without a limit; MaxLimit and MaxPage bound what a request may ask.
	DefaultLimit = 50
	MaxLimit     = 200
	MaxPage      = 100000
)

// Feed is how a feature reads its items: all of them, a group's and a repository's.
type Feed[R any] struct {
	All     func(*snapshot.Snapshot) []R
	OfGroup func(s *snapshot.Snapshot, id string) []R
	OfRepo  func(s *snapshot.Snapshot, k snapshot.RepoKey) []R
}

// Of is the items of the repositories a request names, and those repositories (the scope): the group's members, else every
// repository; narrowed to one repository when repo is given. A group or repository that is not in the view is a missing one:
// Of returns what to tell the caller, else "".
func Of[R any](s *snapshot.Snapshot, group, repo *string, f Feed[R]) (refs []R, scope []snapshot.RepoKey, missing string) {
	if group != nil {
		g, ok := s.Group(*group)
		if !ok {
			return nil, nil, "no such group"
		}
		scope, refs = g.Members, f.OfGroup(s, g.ID)
	} else {
		refs = f.All(s)
		for k := range s.Repos {
			scope = append(scope, k)
		}
	}
	if repo != nil {
		owner, name, ok := strings.Cut(*repo, "/")
		e, _ := s.Repo(snapshot.KeyOf(owner, name))
		if !ok || e == nil {
			return nil, nil, "no such repository"
		}
		key := e.Repo.Key()
		refs = nil
		if slices.Contains(scope, key) {
			refs = f.OfRepo(s, key)
		}
		scope = []snapshot.RepoKey{key}
	}
	return refs, scope, ""
}

// Params are the page number and size asked for (def when no limit), or what is wrong with them.
func Params(page, limit *int, def int) (number, size int, problem string) {
	number, size = 1, def
	if page != nil {
		number = *page
	}
	if limit != nil {
		size = *limit
	}
	switch {
	case number < 1 || number > MaxPage:
		return 0, 0, "page must be 1 to 100000"
	case size < 1 || size > MaxLimit:
		return 0, 0, "limit must be 1 to 200"
	}
	return number, size, ""
}

// Paging is page number of total items, size per page: its bounds in the list (empty past the last page) and its description.
func Paging(total, number, size int) (start, end int, p oapi.Page) {
	start = min((number-1)*size, total)
	end = min(start+size, total)
	return start, end, oapi.Page{Number: number, Size: size, Total: total, Pages: max(1, (total+size-1)/size)}
}

// Freshness is fresh when the items of every repository in scope are fresh (of reads a repository's), unknown when the scope is
// empty and stale otherwise. The times are the oldest in scope; null when a repository was never fetched.
func Freshness(s *snapshot.Snapshot, scope []snapshot.RepoKey, now time.Time, of func(*snapshot.RepoEntry) snapshot.Freshness) oapi.Freshness {
	if len(scope) == 1 {
		if e := s.Repos[scope[0]]; e != nil {
			return oapi.FreshnessOf(of(e), now)
		}
	}
	var fetched, checked time.Time
	seen, stale, never := false, false, false
	for _, k := range scope {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		f := of(e)
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
