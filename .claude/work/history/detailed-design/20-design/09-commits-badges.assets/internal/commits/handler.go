package commits

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/commits/commitsapi"
	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	// RetryAfterNotReady is the Retry-After of the 503 before the first snapshot exists.
	retryAfterNotReady = 5 * time.Second
)

// ListQuery is the decoded request of operation listCommits (the generated ListCommitsParams mapped one to one).
type ListQuery struct {
	Repo   string // slug, "" absent
	Group  string // group id, "" absent
	Limit  *int   // nil absent
	Cursor string // "" absent
}

// ListResult is everything the generated response needs: the three body members and the two headers. NotModified
// means: answer 304 with ETag and no body.
type ListResult struct {
	Items       []openapi.Commit
	Page        openapi.PageInfo
	Freshness   openapi.Freshness
	ETag        string
	Link        string // "" on the last page
	NotModified bool
}

// cursorV1 is the keyset position plus the fingerprint of the filter the cursor was issued for (httpapi.QueryFingerprint:
// a cursor is untrusted input and is bound to its filter, 06-httpapi T12).
type cursorV1 struct {
	At   int64  `json:"t"`
	Slug string `json:"r"`
	SHA  string `json:"s"`
	F    string `json:"f"`
}

func fingerprint(q ListQuery) string {
	return httpapi.QueryFingerprint(url.Values{"repo": {q.Repo}, "group": {q.Group}}, "repo", "group")
}

// List implements listCommits. r is the request (for If-None-Match and the Link header); every failure is returned as
// httpapi.Err(problem) so that the strict server's ResponseErrorHandlerFunc renders it (06-httpapi D16).
func (s *Service) List(ctx context.Context, r *http.Request, q ListQuery) (ListResult, error) {
	start := time.Now()
	limit := 0
	if q.Limit != nil {
		limit = *q.Limit
		if limit < 1 { // ParsePage reads 0 as "absent"; an explicit 0 or negative value is a client error
			return ListResult{}, httpapi.Err(httpapi.Validation(httpapi.FieldError{Pointer: "#/query/limit", Detail: "must be between 1 and " + strconv.Itoa(maxLimit)}))
		}
	}
	page, prob := httpapi.ParsePage(limit, q.Cursor, defaultLimit, maxLimit)
	if prob != nil {
		return ListResult{}, httpapi.Err(*prob)
	}
	var pos *Position
	if page.Cursor != "" {
		var c cursorV1
		if err := httpapi.DecodeCursor(string(page.Cursor), &c); err != nil || !validCursor(c) || c.F != fingerprint(q) {
			return ListResult{}, httpapi.Err(httpapi.InvalidCursor())
		}
		pos = &Position{At: time.Unix(c.At, 0).UTC(), Slug: scm.Slug(c.Slug), SHA: c.SHA}
	}

	snap := s.d.Pub.Current()
	if snap == nil || snap.Version() == 0 {
		return ListResult{}, httpapi.Err(httpapi.NotReady(retryAfterNotReady))
	}
	class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(ctx))
	now := time.Now().UTC()

	var (
		entries []Entry
		more    bool
		info    FeedInfo
		path    = "merge"
	)
	lat, _ := readmodel.GetGlobal(snap, s.d.Latest)
	latest := lat.For(class)
	switch {
	case q.Repo != "" || q.Group != "":
		var slug scm.Slug
		if q.Repo != "" {
			slug = scm.Slug(asciiLower(q.Repo)) // GitHub names are case-insensitive; README links often use the display case
			if aliases, _ := readmodel.GetGlobal(snap, s.d.Aliases); aliases != nil {
				if cur, ok := aliases[slug]; ok {
					slug = cur // a renamed repository: resolved silently (API contract)
				}
			}
			repo, p := authz.LookupRepo(ctx, snap, s.d.Repos, s.d.Authz, slug)
			if p != nil {
				return ListResult{}, httpapi.Err(*p)
			}
			slug = repo.Slug
		}
		mem, _ := readmodel.GetGlobal(snap, s.d.Members)
		g := scm.GroupID(q.Group)
		if _, ok := mem.Group(g); q.Group != "" && !ok {
			return ListResult{}, httpapi.Err(httpapi.NotFound("group not found"))
		}
		switch {
		case q.Group != "" && q.Repo == "" && pos == nil && s.fast(latest.ByGroup[g], page.Limit):
			entries, more = s.fromRefs(snap, latest.ByGroup[g], page.Limit)
			info, path = latest.GroupInfo[g], "fast"
		default:
			var srcs []Source
			switch {
			case q.Repo != "" && q.Group == "":
				v, _ := readmodel.Get(snap, readmodel.EntryID(slug), s.d.View)
				srcs = []Source{{Slug: slug, Commits: v.Latest}}
				info = s.singleInfo(snap, slug, true)
			case q.Group != "":
				srcs, info = s.groupSources(snap, mem, g, class)
				if q.Repo != "" { // both filters: the repository must be a member of the group, otherwise the page is empty
					srcs = slices.DeleteFunc(srcs, func(x Source) bool { return x.Slug != slug })
					info = s.singleInfo(snap, slug, len(srcs) == 1)
				}
			}
			entries, more = Merge(srcs, pos, page.Limit, nil)
		}
	default:
		if pos == nil && s.fast(latest.All, page.Limit) {
			entries, more = s.fromRefs(snap, latest.All, page.Limit)
			info, path = latest.AllInfo, "fast"
			break
		}
		var srcs []Source
		srcs, info = s.allSources(snap, class)
		entries, more = Merge(srcs, pos, page.Limit, nil)
	}
	s.m.merge(ctx, path, time.Since(start))

	status := info.Derive(now)
	// The derived status is part of the body and ages without a new snapshot, so it is part of the validator (06 D15):
	// W/"g<version>-<class>" while fresh, W/"g<version>-<class>.<digest>" otherwise.
	etag := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), classTag(class)), httpapi.FreshnessDigest(status.String()))
	if httpapi.NotModified(r, etag) {
		return ListResult{ETag: etag, NotModified: true}, nil
	}

	fv := httpapi.FreshnessView{Status: status.String(), Kind: "commits.view", AsOf: snap.BuiltAt(), FetchedAt: info.OldestFetchedAt, LastErrorCode: info.ErrCode}
	if !info.OldestFetchedAt.IsZero() && status != freshness.StatusFresh {
		fv.StaleSince = info.OldestFetchedAt.Add(info.TTL * 3 / 2)
	}
	res := ListResult{Items: make([]openapi.Commit, 0, len(entries)), ETag: etag, Freshness: httpapi.NewFreshness(fv)}
	for _, e := range entries {
		res.Items = append(res.Items, APICommit(e.Slug, *e.C))
	}
	next := ""
	if more && len(entries) > 0 {
		p := PositionOf(entries[len(entries)-1])
		next = httpapi.EncodeCursor(cursorV1{At: p.At.Unix(), Slug: string(p.Slug), SHA: p.SHA, F: fingerprint(q)})
		res.Link = httpapi.NextLink(r, next)
	}
	res.Page = httpapi.NewPageInfo(next, page.Limit)
	return res, nil
}

// ListCommits implements the generated commitsapi.StrictServerInterface: it maps the request object onto ListQuery,
// calls List and maps ListResult onto the generated response types. Problems are returned as errors and rendered by the
// ResponseErrorHandlerFunc of httpapi.StrictErrors (06 D16). The *http.Request List needs (If-None-Match, Link) comes
// from httpapi.RequestFrom, which cmd/scp makes available by mounting httpapi.WithRequest innermost (06 D29).
func (s *Service) ListCommits(ctx context.Context, req commitsapi.ListCommitsRequestObject) (commitsapi.ListCommitsResponseObject, error) {
	r := httpapi.RequestFrom(ctx)
	if r == nil { // fails closed: without the request there is no conditional handling and no Link header
		return nil, errors.New("commits: httpapi.WithRequest is not mounted")
	}
	p := req.Params
	q := ListQuery{Repo: httpapi.Deref(p.Repo), Group: httpapi.Deref(p.Group), Limit: p.Limit, Cursor: httpapi.Deref(p.Cursor)}
	res, err := s.List(ctx, r, q)
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return commitsapi.ListCommits304Response{Headers: openapi.NotModifiedResponseHeaders{ETag: &res.ETag}}, nil
	}
	h := commitsapi.ListCommits200ResponseHeaders{ETag: &res.ETag}
	if res.Link != "" {
		h.Link = &res.Link
	}
	return commitsapi.ListCommits200JSONResponse{Body: commitsapi.CommitPage{Items: res.Items, Page: res.Page, Freshness: res.Freshness}, Headers: h}, nil
}

// Mount registers GET /api/v1/commits through the generated strict handler (the same shape as every feature, 06
// Appendix B).
func (s *Service) Mount(m httpapi.Mux) {
	reqErr, respErr := httpapi.StrictErrors(s.d.Log)
	h := commitsapi.NewStrictHandlerWithOptions(s, nil, commitsapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: reqErr, ResponseErrorHandlerFunc: respErr})
	commitsapi.HandlerWithOptions(h, commitsapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}

// asciiLower folds only ASCII letters, like scm.RepoRef.Key: slugs are canonical lower-case ASCII (scm.ParseSlug).
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// APICommit maps a stored commit onto the shared schema common.yaml#/Commit. overview (rank 5, repository composite
// `latestCommits`) uses the same function, so the two endpoints cannot disagree about a commit's wire form.
func APICommit(slug scm.Slug, c scm.Commit) openapi.Commit {
	return openapi.Commit{Sha: c.SHA, Repo: string(slug), Subject: c.Subject, AuthorName: nonEmpty(c.AuthorName),
		AuthorLogin: nonEmpty(c.AuthorLogin), CommittedAt: c.CommittedAt, Url: c.URL}
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func validCursor(c cursorV1) bool {
	if len(c.Slug) == 0 || len(c.Slug) > 100 || c.At < 0 || c.At > 1<<40 {
		return false
	}
	_, ok := cleanSHA(c.SHA)
	return ok
}

func classTag(c scm.ViewClass) string {
	if c == scm.ViewAll {
		return "a"
	}
	return "p"
}

// fromRefs resolves the first limit refs of a precomputed feed to entries (O(limit * ring) lookups, ring <= 100).
func (s *Service) fromRefs(snap *readmodel.Snapshot, refs []CommitRef, limit int) ([]Entry, bool) {
	n := min(limit, len(refs))
	out := make([]Entry, 0, n)
	for _, r := range refs[:n] {
		v, ok := readmodel.Get(snap, readmodel.EntryID(r.Slug), s.d.View)
		if !ok {
			continue
		}
		for i := range v.Latest {
			if v.Latest[i].SHA == r.SHA {
				out = append(out, Entry{Slug: r.Slug, C: &v.Latest[i]})
				break
			}
		}
	}
	return out, len(refs) > n
}

// fast reports whether a first page of limit entries can be served from a precomputed feed. The feeds hold LatestN+1
// refs (the last one is a sentinel), so any limit <= LatestN is answered completely and `more` is exact: it is true
// iff the feed is longer than the page. A longer page takes the merge path, which produces the identical result.
func (s *Service) fast(refs []CommitRef, limit int) bool { return limit <= s.d.Cfg.LatestN }

func (s *Service) allSources(snap *readmodel.Snapshot, class scm.ViewClass) ([]Source, FeedInfo) {
	repos := snap.Repos()
	srcs := make([]Source, 0, len(repos))
	backing := make([]repoEntry, 0, len(repos)) // one allocation; the capacity is fixed, so the pointers stay valid
	es := make([]*repoEntry, 0, len(repos))
	for _, id := range repos {
		rv, ok := readmodel.Get(snap, id, s.d.Repos)
		if !ok || !rv.V.Visibility.Visible(class) {
			continue
		}
		backing = append(backing, repoEntry{slug: scm.Slug(id), src: Source{Slug: scm.Slug(id)}})
		e := &backing[len(backing)-1]
		if v, ok := readmodel.Get(snap, id, s.d.View); ok {
			e.src.Commits, e.info, e.has = v.Latest, v.Info, true
		}
		srcs = append(srcs, e.src)
		es = append(es, e)
	}
	return srcs, s.feedInfo(es)
}

func (s *Service) groupSources(snap *readmodel.Snapshot, mem scm.Membership, g scm.GroupID, class scm.ViewClass) ([]Source, FeedInfo) {
	var srcs []Source
	var es []*repoEntry
	for _, slug := range mem.VisibleReposOf(g, class) {
		id := readmodel.EntryID(slug)
		if _, ok := readmodel.Get(snap, id, s.d.Repos); !ok {
			continue
		}
		e := &repoEntry{slug: slug, src: Source{Slug: slug}}
		if v, ok := readmodel.Get(snap, id, s.d.View); ok {
			e.src.Commits, e.info, e.has = v.Latest, v.Info, true
		}
		srcs = append(srcs, e.src)
		es = append(es, e)
	}
	return srcs, s.feedInfo(es)
}

func (s *Service) singleInfo(snap *readmodel.Snapshot, slug scm.Slug, member bool) FeedInfo {
	if !member {
		return s.feedInfo(nil)
	}
	e := &repoEntry{slug: slug}
	if v, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.View); ok {
		e.info, e.has = v.Info, true
	}
	return s.feedInfo([]*repoEntry{e})
}
