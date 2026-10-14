package overview

import (
	"cmp"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/overview/overviewapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/routes"
	"github.com/cantara/source-code-portal/internal/scm"
)

// ---- one row: everything the summary of one repository is resolved from -------------------------------------------

type row struct {
	slug    scm.Slug
	repo    freshness.Value[scm.Repository]
	stats   freshness.Value[scm.RepoStats]
	hasStat bool
	meta    MetaView
	docs    []DocEntry
	docInfo freshness.Info
	hasDocs bool

	title     Sourced
	summary   *Sourced
	typ, life *Sourced
	tags      []Sourced
	lastAt    time.Time // newest commit, zero when unknown
	groups    []scm.GroupID
}

var tagRE = regexp.MustCompile(`^[a-z0-9:+#]+(-[a-z0-9:+#]+)*$`)

// clean1 is the boundary rule for repository-authored strings in JSON: control, bidi and zero-width characters out.
// (The JSON API otherwise returns the text as authored; consumers treat it as data, design 07 section 4.8 item 7.)
func clean1(s string) string { return scm.CleanLine(s, 0) }

func cleanSourced(x *Sourced) *Sourced {
	if x == nil {
		return nil
	}
	v := clean1(x.V)
	if v == "" {
		return nil
	}
	return &Sourced{V: v, Source: x.Source}
}

// newRow resolves one repository for the given snapshot. ok is false when the repository is not in the Repos slot or
// not visible to class.
func (s *Service) newRow(snap *readmodel.Snapshot, m scm.Membership, class scm.ViewClass, slug scm.Slug) (row, bool) {
	rp, ok := readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Repos)
	if !ok || !rp.V.VisibleTo(class) {
		return row{}, false
	}
	r := row{slug: slug, repo: rp, groups: m.GroupsOf(slug)}
	r.stats, r.hasStat = readmodel.Get(snap, readmodel.EntryID(slug), s.d.Slots.Stats)
	r.meta, _ = s.d.Docs.Meta(snap, slug)
	r.docs, r.docInfo, r.hasDocs = s.d.Docs.Index(snap, slug)

	r.title = Sourced{V: rp.V.Ref.Name, Source: "derived"}
	if t := cleanSourced(r.meta.Title); t != nil {
		r.title = *t
	}
	r.summary = cleanSourced(r.meta.Summary)
	if r.summary == nil {
		if d := clean1(rp.V.Description); d != "" {
			r.summary = &Sourced{V: d, Source: "github"}
		}
	}
	r.typ, r.life = cleanSourced(r.meta.Type), cleanSourced(r.meta.Lifecycle)
	tags := r.meta.Tags
	if len(tags) == 0 {
		for _, t := range rp.V.Topics {
			tags = append(tags, Sourced{V: t, Source: "github-topics"})
		}
	}
	for _, t := range tags {
		if v := strings.ToLower(clean1(t.V)); v != "" && len(v) <= 63 && tagRE.MatchString(v) {
			r.tags = append(r.tags, Sourced{V: v, Source: t.Source})
		}
	}
	if cs, _, ok := s.d.Commits.Recent(snap, slug, 1); ok && len(cs) > 0 {
		r.lastAt = cs[0].CommittedAt
	}
	return r, true
}

func (r row) readme() (DocEntry, bool) {
	for _, d := range r.docs {
		if d.Kind == DocReadme {
			return d, true
		}
	}
	return DocEntry{}, false
}

// summary builds the RepoSummary of a row (the element of both lists and the head of the composite).
func (s *Service) summaryOf(snap *readmodel.Snapshot, m scm.Membership, r row, providers []string, now time.Time) overviewapi.RepoSummary {
	out := overviewapi.RepoSummary{
		Slug: string(r.slug), Name: r.repo.V.Ref.Name, Owner: r.repo.V.Ref.Owner,
		Title:   overviewapi.SourcedString{Value: r.title.V, Source: r.title.Source},
		Summary: sourcedOut(r.summary), Type: sourcedOut(r.typ), Lifecycle: sourcedOut(r.life),
		Archived: r.repo.V.Archived, Fork: r.repo.V.Fork, Visibility: r.repo.V.Visibility.String(),
		DefaultBranch: r.repo.V.DefaultBranch, HtmlUrl: r.repo.V.HTMLURL,
		Tags:   make([]overviewapi.Tag, 0, len(r.tags)),
		Groups: make([]overviewapi.GroupRef, 0, len(r.groups)),
		Badges: s.d.Badges.For(r.slug, providers),
		UiUrl:  routes.Repo(string(r.slug)), ApiUrl: routes.API("repos", string(r.slug)),
		Freshness: freshnessOf("catalog.repo", []itemInfo{{r.slug, r.repo.Info}}, now),
	}
	if out.Badges == nil {
		out.Badges = []openapi.Badge{}
	}
	for _, t := range r.tags {
		out.Tags = append(out.Tags, overviewapi.Tag{Value: t.V, Source: t.Source})
	}
	for _, g := range r.groups {
		if gm, ok := m.Group(g); ok {
			out.Groups = append(out.Groups, overviewapi.GroupRef{Id: string(g), DisplayName: clean1(gm.DisplayName)})
		}
	}
	if rd, ok := r.readme(); ok && rd.Format == "markdown" && rd.State == "resident" {
		u := routes.ReadmeMD(string(r.slug))
		out.MarkdownUrl = &u
	}
	out.Stats = statsOf(r)
	return out
}

func sourcedOut(x *Sourced) *overviewapi.SourcedString {
	if x == nil {
		return nil
	}
	return &overviewapi.SourcedString{Value: x.V, Source: x.Source}
}

func statsOf(r row) overviewapi.RepoStats {
	var st overviewapi.RepoStats
	pushed := r.repo.V.PushedAt
	if r.hasStat {
		v := r.stats.V
		n := v.OpenIssues
		st.OpenIssues, st.Stars = &n, v.Stars
		if v.License != "" {
			st.License = &overviewapi.License{SpdxId: v.License}
		}
		if v.Release != nil {
			rel := overviewapi.Release{Tag: v.Release.Tag, Url: v.Release.URL, Prerelease: v.Release.Prerelease}
			if v.Release.Name != "" {
				nm := v.Release.Name
				rel.Name = &nm
			}
			if !v.Release.PublishedAt.IsZero() {
				t := v.Release.PublishedAt.UTC()
				rel.PublishedAt = &t
			}
			st.LatestRelease = &rel
		}
		if v.HeadSHA != "" {
			h := v.HeadSHA
			st.HeadSha = &h
		}
		if !v.PushedAt.IsZero() {
			pushed = v.PushedAt
		}
	}
	if !pushed.IsZero() {
		t := pushed.UTC()
		st.PushedAt = &t
	}
	if !r.lastAt.IsZero() {
		t := r.lastAt.UTC()
		st.LastCommitAt = &t
	}
	return st
}

// ---- freshness aggregation --------------------------------------------------------------------------------------

type itemInfo struct {
	slug scm.Slug
	info freshness.Info
}

// freshnessOf aggregates the freshness of the contributing items (design 07 common.yaml Freshness; same table as the
// commits feed in design 09 section 3.3): no items is fresh; every item without a value is unknown, or error when one of
// them failed; otherwise any item without a value, failing, or older than 1.5 x TTL makes it stale. FetchedAt (and AsOf)
// is the oldest among items with a value, CheckedAt the oldest attempt, StaleSince the earliest stale instant already
// passed, LastErrorCode the code of the failing item with the smallest slug.
func freshnessOf(kind string, items []itemInfo, now time.Time) openapi.Freshness {
	v := httpapi.FreshnessView{Kind: kind}
	if len(items) == 0 {
		v.Status = freshness.StatusFresh.String()
		return httpapi.NewFreshness(v)
	}
	never, failing, withValue := 0, 0, 0
	stale := false
	var smallest scm.Slug
	for _, it := range items {
		in := it.info
		if in.State == freshness.StateError {
			failing++
			if smallest == "" || it.slug < smallest {
				smallest, v.LastErrorCode = it.slug, in.ErrCode
			}
		}
		if !in.HasValue() {
			never++
			continue
		}
		withValue++
		if v.FetchedAt.IsZero() || in.FetchedAt.Before(v.FetchedAt) {
			v.FetchedAt, v.AsOf = in.FetchedAt, in.FetchedAt
		}
		if !in.CheckedAt.IsZero() && (v.CheckedAt.IsZero() || in.CheckedAt.Before(v.CheckedAt)) {
			v.CheckedAt = in.CheckedAt
		}
		if at, ok := in.StaleSince(); ok && !at.After(now) && (v.StaleSince.IsZero() || at.Before(v.StaleSince)) {
			v.StaleSince = at
		}
		if in.Derive(now) != freshness.StatusFresh {
			stale = true
		}
	}
	switch {
	case never == len(items) && failing == 0:
		v.Status = freshness.StatusUnknown.String()
	case never == len(items):
		v.Status = freshness.StatusError.String()
	case never > 0 || failing > 0 || stale:
		v.Status = freshness.StatusStale.String()
	default:
		v.Status = freshness.StatusFresh.String()
	}
	return httpapi.NewFreshness(v)
}

// ---- list: api.repos and api.group.repos ------------------------------------------------------------------------

// ListQuery is the typed query of both list operations.
type ListQuery struct {
	Q         string
	Groups    []scm.GroupID // api.repos only
	Lifecycle []string
	Type      []string
	Tag       []string
	Archived  string // include (default) | exclude | only
	Sort      string // name (default) ...
	Limit     int
	Cursor    string
	Scope     scm.GroupID // api.group.repos: the {groupId} path value
}

type listCursor struct {
	F string `json:"f"` // fingerprint of filters, sort and scope: a cursor is valid only for the query that issued it
	M bool   `json:"m"` // the anchor's key is missing
	N int64  `json:"n"` // numeric key (unix nanoseconds, count, rank)
	S string `json:"s"` // string key (name)
	L string `json:"l"` // anchor slug
}

// The closed sets of the list query. Functions, not package-level slices: a mutable global would break architecture G5.
func lifecycleSet() []string { return []string{"experimental", "production", "deprecated"} }
func typeSet() []string {
	return []string{"service", "library", "website", "tool", "documentation", "other"}
}
func sortSet() []string { return []string{"name", "pushedAt", "lastCommit", "openIssues", "lifecycle"} }

type key struct {
	missing bool
	n       int64
	s       string
}

func keyOf(r row, field string) key {
	switch field {
	case "name":
		return key{s: strings.ToLower(r.repo.V.Ref.Name)}
	case "pushedAt":
		t := r.repo.V.PushedAt
		if r.hasStat && !r.stats.V.PushedAt.IsZero() {
			t = r.stats.V.PushedAt
		}
		if t.IsZero() {
			return key{missing: true}
		}
		return key{n: t.UnixNano()}
	case "lastCommit":
		if r.lastAt.IsZero() {
			return key{missing: true}
		}
		return key{n: r.lastAt.UnixNano()}
	case "openIssues":
		if !r.hasStat {
			return key{missing: true}
		}
		return key{n: int64(r.stats.V.OpenIssues)}
	case "lifecycle":
		if r.life == nil {
			return key{missing: true}
		}
		return key{n: int64(slices.Index(lifecycleSet(), r.life.V))}
	}
	return key{missing: true}
}

// cmpRows orders two rows by the sort key (descending when desc), missing values last in both directions, ties by slug.
func cmpKeyed(a key, as scm.Slug, b key, bs scm.Slug, desc bool) int {
	switch {
	case a.missing && b.missing:
	case a.missing:
		return 1
	case b.missing:
		return -1
	default:
		c := cmp.Compare(a.n, b.n)
		if c == 0 {
			c = strings.Compare(a.s, b.s)
		}
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return strings.Compare(string(as), string(bs))
}

func (s *Service) listHandler(scoped bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, prob := parseListQuery(r, scoped)
		if prob != nil {
			httpapi.WriteProblem(w, r, *prob)
			return
		}
		snap := s.d.Snapshots.Current()
		class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
		m, _ := readmodel.GetGlobal(snap, s.d.Slots.Membership)
		if scoped {
			if len(m.VisibleReposOf(q.Scope, class)) == 0 { // unknown, or no visible member: identical 404
				httpapi.WriteProblem(w, r, httpapi.NotFound("group not found"))
				return
			}
		}
		now := s.d.Now()
		list, etag, prob := s.listRepos(snap, m, class, q, now)
		if prob != nil {
			httpapi.WriteProblem(w, r, *prob)
			return
		}
		h := w.Header()
		h.Set("ETag", etag)
		h.Set("Cache-Control", "no-cache")
		if httpapi.NotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if list.Page.NextCursor != nil {
			h.Add("Link", httpapi.NextLink(r, *list.Page.NextCursor))
		}
		httpapi.WriteJSON(w, r, http.StatusOK, list)
	}
}

func parseListQuery(r *http.Request, scoped bool) (ListQuery, *httpapi.Problem) {
	v := r.URL.Query()
	var errs []httpapi.FieldError
	bad := func(name, detail string) {
		errs = append(errs, httpapi.FieldError{Pointer: "#/query/" + name, Detail: detail})
	}
	q := ListQuery{Q: strings.TrimSpace(v.Get("q")), Tag: v["tag"], Archived: cmp.Or(v.Get("archived"), "include"), Sort: cmp.Or(v.Get("sort"), "name")}
	if scoped {
		q.Scope = scm.GroupID(r.PathValue("groupId"))
		if v.Has("group") {
			bad("group", "not accepted on this operation")
		}
	}
	for _, g := range v["group"] {
		q.Groups = append(q.Groups, scm.GroupID(g))
	}
	q.Lifecycle, q.Type = v["lifecycle"], v["type"]
	if n := utf8Len(v.Get("q")); v.Has("q") && (n < 1 || n > 200) {
		bad("q", "must be 1 to 200 characters")
	}
	enum := func(name string, vals, allowed []string, max int) {
		if len(vals) > max {
			bad(name, "at most "+strconv.Itoa(max)+" values")
		}
		for _, x := range vals {
			if !slices.Contains(allowed, x) {
				bad(name, "unknown value")
				return
			}
		}
	}
	enum("lifecycle", q.Lifecycle, lifecycleSet(), 3)
	enum("type", q.Type, typeSet(), 6)
	if len(q.Groups) > 50 {
		bad("group", "at most 50 values")
	}
	if len(q.Tag) > 10 {
		bad("tag", "at most 10 values")
	}
	for _, t := range q.Tag {
		if l := utf8Len(t); l < 1 || l > 63 {
			bad("tag", "each value must be 1 to 63 characters")
			break
		}
	}
	if !slices.Contains([]string{"include", "exclude", "only"}, q.Archived) {
		bad("archived", "must be include, exclude or only")
	}
	sortField, desc := strings.CutPrefix(q.Sort, "-")
	_ = desc
	if !slices.Contains(sortSet(), sortField) {
		bad("sort", "unknown sort key")
	}
	lim := 0
	if v.Has("limit") {
		n, err := strconv.Atoi(v.Get("limit"))
		if err != nil || n < 1 {
			bad("limit", "must be an integer between 1 and 1000")
			n = 1 // reported above; keep ParsePage from reporting it twice
		}
		lim = n
	}
	pp, prob := httpapi.ParsePage(lim, v.Get("cursor"), 1000, 1000)
	if prob != nil {
		if len(prob.Errors) > 0 && prob.Code == httpapi.CodeValidationFailed {
			errs = append(errs, prob.Errors...)
		} else {
			return ListQuery{}, prob // invalid-cursor
		}
	}
	if len(errs) > 0 {
		p := httpapi.Validation(errs...)
		return ListQuery{}, &p
	}
	q.Limit, q.Cursor = pp.Limit, string(pp.Cursor)
	return q, nil
}

func utf8Len(s string) int { return len([]rune(s)) }

// listRepos is the whole of both list operations: filter by visibility, hidden flag and the query, sort, find the
// keyset position, build one page. It returns the response and its weak ETag. Cost is O(n log n) in the visible
// repositories (at most 1,000 by contract, 2,500 at the design limit) with no allocation per repository beyond the row.
func (s *Service) listRepos(snap *readmodel.Snapshot, m scm.Membership, class scm.ViewClass, q ListQuery, now time.Time) (overviewapi.RepoList, string, *httpapi.Problem) {
	var slugs []scm.Slug
	if q.Scope != "" {
		slugs = m.VisibleReposOf(q.Scope, class)
	} else {
		slugs = m.Slugs(class)
	}
	groupSet := map[scm.GroupID]bool{}
	for _, g := range q.Groups {
		groupSet[g] = true
	}
	needle := strings.ToLower(clean1(q.Q))
	var rows []row
	for _, slug := range slugs {
		r, ok := s.newRow(snap, m, class, slug)
		if !ok || r.meta.Hidden {
			continue
		}
		if len(groupSet) > 0 && !slices.ContainsFunc(r.groups, func(g scm.GroupID) bool { return groupSet[g] }) {
			continue
		}
		if len(q.Lifecycle) > 0 && (r.life == nil || !slices.Contains(q.Lifecycle, r.life.V)) {
			continue
		}
		if len(q.Type) > 0 && (r.typ == nil || !slices.Contains(q.Type, r.typ.V)) {
			continue
		}
		if len(q.Tag) > 0 && !hasAllTags(r.tags, q.Tag) {
			continue
		}
		switch q.Archived {
		case "exclude":
			if r.repo.V.Archived {
				continue
			}
		case "only":
			if !r.repo.V.Archived {
				continue
			}
		}
		if needle != "" && !matches(r, needle) {
			continue
		}
		rows = append(rows, r)
	}
	field, desc := strings.CutPrefix(q.Sort, "-")
	slices.SortFunc(rows, func(a, b row) int { return cmpKeyed(keyOf(a, field), a.slug, keyOf(b, field), b.slug, desc) })

	fp := httpapi.QueryFingerprint(url.Values{"q": {q.Q}, "group": groupStrings(q.Groups), "lifecycle": q.Lifecycle, "type": q.Type, "tag": q.Tag,
		"archived": {q.Archived}, "sort": {q.Sort}, "scope": {string(q.Scope)}}, "q", "group", "lifecycle", "type", "tag", "archived", "sort", "scope")
	start := 0
	if q.Cursor != "" {
		var c listCursor
		if err := httpapi.DecodeCursor(q.Cursor, &c); err != nil || c.F != fp {
			p := httpapi.InvalidCursor()
			return overviewapi.RepoList{}, "", &p
		}
		anchor := key{missing: c.M, n: c.N, s: c.S}
		start = sort.Search(len(rows), func(i int) bool {
			return cmpKeyed(keyOf(rows[i], field), rows[i].slug, anchor, scm.Slug(c.L), desc) > 0
		})
	}
	end := min(start+q.Limit, len(rows))
	page := rows[start:end]

	providers := s.d.Integrations.Providers()
	items := make([]overviewapi.RepoSummary, 0, len(page))
	infos := make([]itemInfo, 0, len(page))
	statuses := make([]string, 0, len(page))
	for _, r := range page {
		items = append(items, s.summaryOf(snap, m, r, providers, now))
		infos = append(infos, itemInfo{r.slug, r.repo.Info})
		statuses = append(statuses, r.repo.Info.Derive(now).String())
	}
	next := ""
	if end < len(rows) && len(page) > 0 {
		last := page[len(page)-1]
		k := keyOf(last, field)
		next = httpapi.EncodeCursor(listCursor{F: fp, M: k.missing, N: k.n, S: k.s, L: string(last.slug)})
	}
	etag := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), classTag(class)), httpapi.FreshnessDigest(statuses...))
	return overviewapi.RepoList{Items: items, Page: httpapi.NewPageInfo(next, q.Limit), Freshness: freshnessOf("catalog.repo", infos, now)}, etag, nil
}

func groupStrings(g []scm.GroupID) []string {
	out := make([]string, len(g))
	for i, x := range g {
		out[i] = string(x)
	}
	return out
}

func hasAllTags(have []Sourced, want []string) bool {
	for _, w := range want {
		w = strings.ToLower(w)
		if !slices.ContainsFunc(have, func(t Sourced) bool { return t.V == w }) {
			return false
		}
	}
	return true
}

func matches(r row, needle string) bool {
	if strings.Contains(string(r.slug), needle) || strings.Contains(strings.ToLower(r.title.V), needle) {
		return true
	}
	return r.summary != nil && strings.Contains(strings.ToLower(r.summary.V), needle)
}

// ---- composite: api.repo -----------------------------------------------------------------------------------------

func (s *Service) repoHandler(w http.ResponseWriter, r *http.Request) {
	repo, prob := s.lookup(r)
	if prob != nil {
		httpapi.WriteProblem(w, r, *prob)
		return
	}
	snap := s.d.Snapshots.Current()
	class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
	m, _ := readmodel.GetGlobal(snap, s.d.Slots.Membership)
	now := s.d.Now()
	rw, ok := s.newRow(snap, m, class, repo.Slug)
	if !ok { // cannot happen after LookupRepo unless the class differs from the viewer: treat as invisible
		httpapi.WriteProblem(w, r, httpapi.NotFound("repository not found"))
		return
	}
	body := s.composite(snap, m, rw, now)
	etag := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), classTag(class)), etagDigest(body))
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache")
	if httpapi.NotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpapi.WriteJSON(w, r, http.StatusOK, body)
}

// etagDigest folds the five derived section statuses in: the only time-dependent members of the body.
func etagDigest(b overviewapi.Repo) string {
	st := []string{string(b.Freshness.Status)}
	for _, f := range []*openapi.Freshness{b.Sections.Commits, b.Sections.Docs, b.Sections.Manifests, b.Sections.Integrations} {
		if f != nil {
			st = append(st, string(f.Status))
		}
	}
	for _, p := range b.Statuses {
		st = append(st, string(p.Freshness.Status))
	}
	return httpapi.FreshnessDigest(st...)
}

// composite assembles the repository resource. Each upstream-backed section carries its own freshness, so a failed
// upstream degrades one section and never the page; an absent section source (manifests disabled, no provider) is omitted.
func (s *Service) composite(snap *readmodel.Snapshot, m scm.Membership, rw row, now time.Time) overviewapi.Repo {
	providers := s.d.Integrations.Providers()
	out := overviewapi.Repo{RepoSummary: s.summaryOf(snap, m, rw, providers, now)}
	if d := clean1(rw.repo.V.Description); d != "" {
		out.Description = &d
	}
	out.Owners, out.Links, out.Apis, out.DependsOn = []overviewapi.Owner{}, []overviewapi.Link{}, []overviewapi.ApiRef{}, []overviewapi.DependsOn{}
	for _, o := range rw.meta.Owners {
		out.Owners = append(out.Owners, overviewapi.Owner{Handle: clean1(o.Handle), Kind: o.Kind, Source: o.Source})
	}
	for _, l := range rw.meta.Links {
		out.Links = append(out.Links, overviewapi.Link{Url: l.URL, Title: clean1(l.Title), Type: l.Type, Source: l.Source})
	}
	if sy := clean1(rw.meta.System); sy != "" {
		out.System = &sy
	}
	for _, a := range rw.meta.APIs {
		out.Apis = append(out.Apis, overviewapi.ApiRef{Type: a.Type, Title: clean1(a.Title), Path: nilIfEmpty(a.Path), Url: nilIfEmpty(a.URL), SourceUrl: nilIfEmpty(a.SourceURL), Source: a.Source})
	}
	for _, d := range rw.meta.DependsOn {
		x := overviewapi.DependsOn{Repo: clean1(d.Repo), Resolved: d.Resolved, Source: d.Source}
		if d.Resolved {
			sl := string(d.Slug)
			x.Slug = &sl
		}
		out.DependsOn = append(out.DependsOn, x)
	}
	out.Docs = make([]openapi.DocIndexEntry, 0, len(rw.docs))
	for _, d := range rw.docs {
		out.Docs = append(out.Docs, docIndexEntry(rw.slug, d))
	}
	out.Descriptor = rw.meta.Descriptor
	if out.Descriptor.Problems == nil {
		out.Descriptor.Problems = []openapi.DescriptorProblem{}
	}

	// sections
	if rw.hasDocs {
		f := freshnessOf("docs.repo", []itemInfo{{rw.slug, rw.docInfo}}, now)
		out.Sections.Docs = &f
	}
	if cs, info, ok := s.d.Commits.Recent(snap, rw.slug, 5); ok {
		f := freshnessOf("commits.view", []itemInfo{{rw.slug, info}}, now)
		out.Sections.Commits = &f
		for _, c := range cs {
			out.LatestCommits = append(out.LatestCommits, commitOut(rw.slug, c))
		}
	}
	if out.LatestCommits == nil {
		out.LatestCommits = []openapi.Commit{}
	}
	if s.d.Manifests != nil && s.d.Manifests.Enabled() {
		if tv, ok := s.d.Manifests.Technology(snap, rw.slug); ok {
			f := freshnessOf("manifests.repo", []itemInfo{{rw.slug, tv.Info}}, now)
			out.Sections.Manifests = &f
			out.Technology = tv.Summary
		}
	}
	out.Statuses = []overviewapi.ProviderStatus{}
	var pinfos []itemInfo
	for _, p := range providers {
		v := s.d.Integrations.StatusOf(snap, rw.slug, p)
		if !v.Info.HasValue() && (v.Info.State == "" || v.Info.State == freshness.StateNever) {
			continue // enabled provider without an entry: absent, never "ok" (GR-41)
		}
		ps := overviewapi.ProviderStatus{Provider: p, State: v.V.State, Summary: clean1(v.V.Summary), Counts: v.V.Counts,
			Freshness: freshnessOf("integrations."+p, []itemInfo{{rw.slug, v.Info}}, now)}
		if ps.Counts == nil {
			ps.Counts = map[string]int{}
		}
		if v.V.DetailURL != "" {
			u := v.V.DetailURL
			ps.DetailUrl = &u
		}
		if !v.V.ObservedAt.IsZero() {
			t := v.V.ObservedAt.UTC()
			ps.ObservedAt = &t
		}
		out.Statuses = append(out.Statuses, ps)
		pinfos = append(pinfos, itemInfo{rw.slug, v.Info})
	}
	if len(pinfos) > 0 {
		f := freshnessOf("integrations", pinfos, now)
		out.Sections.Integrations = &f
	}
	return out
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func commitOut(slug scm.Slug, c scm.Commit) openapi.Commit {
	o := openapi.Commit{Repo: string(slug), Sha: c.SHA, Subject: c.Subject, CommittedAt: c.CommittedAt.UTC(), Url: c.URL}
	if c.AuthorLogin != "" {
		l := c.AuthorLogin
		o.AuthorLogin = &l
	}
	if c.AuthorName != "" {
		n := c.AuthorName
		o.AuthorName = &n
	}
	return o
}

func docIndexEntry(slug scm.Slug, d DocEntry) openapi.DocIndexEntry {
	e := openapi.DocIndexEntry{
		Id: d.ID, Title: clean1(d.Title), Path: d.Path, Format: openapi.DocFormat(d.Format), GithubRendered: d.GithubRendered, State: openapi.DocState(d.State),
		Order: d.Order, Tags: append([]string{}, d.Tags...), Section: append([]string{}, d.Section...),
		UiUrl: docUI(slug, d), ApiUrl: routes.API("repos", string(slug), "docs", d.ID), SourceUrl: d.SourceURL, BlobSha: d.BlobSHA,
	}
	if x := clean1(d.Description); x != "" {
		e.Description = &x
	}
	if d.Format == "markdown" && d.State == "resident" {
		u := docMarkdownURL(slug, d)
		e.MarkdownUrl = &u
	}
	if !d.UpdatedAt.IsZero() {
		t := d.UpdatedAt.UTC()
		e.UpdatedAt = &t
	}
	return e
}

func docUI(slug scm.Slug, d DocEntry) string {
	if d.Kind == DocReadme {
		return routes.Repo(string(slug))
	}
	return routes.RepoDoc(string(slug), d.Path)
}

// docMarkdownURL is the raw route of a document: readme.md, agents.md, changelog.md, or docs/<path> with a .md suffix.
func docMarkdownURL(slug scm.Slug, d DocEntry) string {
	switch d.Kind {
	case DocReadme:
		return routes.ReadmeMD(string(slug))
	case DocAgents:
		return routes.AgentsMD(string(slug))
	case DocChangelog:
		return routes.ChangelogMD(string(slug))
	}
	return routes.RepoDocMD(string(slug), d.Path)
}

func hasKind(entries []DocEntry, k DocKind) bool {
	return slices.ContainsFunc(entries, func(e DocEntry) bool { return e.Kind == k })
}

// visibleGroup resolves {groupId}: unknown and "no member visible to the caller" are the same absence.
func (s *Service) visibleGroup(r *http.Request) (scm.GroupMeta, bool) {
	snap := s.d.Snapshots.Current()
	class := authz.ClassOf(s.d.Authz, authz.PrincipalFrom(r.Context()))
	m, _ := readmodel.GetGlobal(snap, s.d.Slots.Membership)
	g := scm.GroupID(r.PathValue("groupId"))
	gm, ok := m.Group(g)
	if !ok || len(m.VisibleReposOf(g, class)) == 0 {
		return scm.GroupMeta{}, false
	}
	return gm, true
}
