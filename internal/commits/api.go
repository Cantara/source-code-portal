package commits

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// API serves the merged commit feeds from the current snapshot. It implements the commits methods of the strict server.
type API struct {
	Current func(context.Context) *snapshot.Snapshot // the viewer's view
}

// cursor is the position after the last commit of a page, bound to the filter it was issued for.
type cursor struct {
	Group string `json:"g,omitempty"`
	Repo  string `json:"r,omitempty"`
	Human bool   `json:"h,omitempty"` // automation left out
	At    int64  `json:"t"`
	Key   string `json:"k"`
	SHA   string `json:"s"`
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
	limit := defaultLimit
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > maxLimit {
		return badRequest("validation-failed", "limit must be 1 to 200"), nil
	}
	var filter cursor
	var scope []snapshot.RepoKey
	var refs []Ref
	if p.Group != nil {
		g, ok := s.Group(*p.Group)
		if !ok {
			return notFound("no such group"), nil
		}
		filter.Group, scope = g.ID, g.Members
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
		filter.Repo, refs = string(key), nil
		if slices.Contains(scope, key) {
			cs, _ := Of(e)
			for i := range cs {
				refs = append(refs, Ref{Repo: key, Commit: &cs[i]})
			}
			slices.SortFunc(refs, compareRefs)
		}
		scope = []snapshot.RepoKey{key}
	}

	if p.Automation != nil && !*p.Automation {
		filter.Human = true
		refs = slices.DeleteFunc(slices.Clone(refs), func(r Ref) bool { return Automated(r.Commit) })
	}
	start := 0
	if p.Cursor != nil {
		c, ok := decodeCursor(*p.Cursor)
		if !ok || c.Group != filter.Group || c.Repo != filter.Repo || c.Human != filter.Human {
			return badRequest("invalid-cursor", "the cursor is not valid for this filter"), nil
		}
		pos := Ref{Repo: snapshot.RepoKey(c.Key), Commit: &Commit{SHA: c.SHA, CommittedAt: time.Unix(0, c.At)}}
		start = sort.Search(len(refs), func(i int) bool { return compareRefs(refs[i], pos) > 0 })
	}
	end := min(start+limit, len(refs))
	page := oapi.CommitPage{Commits: make([]oapi.Commit, 0, end-start), Page: oapi.Page{Limit: limit},
		Freshness: scopeFreshness(s, scope, now)}
	for _, r := range refs[start:end] {
		page.Commits = append(page.Commits, commitOf(r.Repo, r.Commit))
	}
	if end < len(refs) {
		last := refs[end-1]
		next := filter
		next.At, next.Key, next.SHA = last.Commit.CommittedAt.UnixNano(), string(last.Repo), last.Commit.SHA
		page.Page.NextCursor = new(encodeCursor(next))
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

func encodeCursor(c cursor) string { return encodeAny(c) }

func decodeCursor(s string) (cursor, bool) {
	var c cursor
	return c, decodeAny(s, &c) && c.Key != "" && c.SHA != ""
}

func encodeAny(v any) string {
	b, _ := json.Marshal(v) //nolint:errchkjson // plain structs
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAny(s string, v any) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && json.Unmarshal(b, v) == nil
}
