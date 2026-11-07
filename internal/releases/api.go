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
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/scope"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// GitHub is the source of a release GitHub holds; a package's source is its ecosystem (maven, npm, ...).
const GitHub = "github"

// Published is a package a repository publishes, at the newest release a registry holds. The packages feature reads them and
// cmd/scp hands them over (features never import each other); a registry that dates no release gives a zero time, and such a package
// is not listed, as a list of releases is in time.
type Published struct {
	Ecosystem string
	Name      string
	Version   string
	URL       string
	Published time.Time
}

// API serves the recent releases from the current snapshot. It implements the releases method of the strict server.
type API struct {
	Current  func(context.Context) *snapshot.Snapshot            // the viewer's view
	Packages func(*snapshot.RepoEntry) []Published               // what a repository publishes; nil: the GitHub releases only
	Render   func(*snapshot.RepoEntry, string) safehtml.SafeHTML // a release's notes (Markdown) as sanitised HTML; nil: no notes
}

// Ref is one release of a repository in the view, a GitHub release or a published package's newest: what a listing filters and sorts.
type Ref struct {
	Key       snapshot.RepoKey
	Name      string
	Groups    []string
	Source    string // GitHub, else the package's ecosystem
	Package   string // the package's name; empty for a GitHub release
	NotesFrom string // "release", "changelog", or "" with no notes
	Release
	latestAll     bool // the newest of the repository's stable GitHub release and its packages' releases
	latestGitHub  bool // the repository's newest stable GitHub release
	latestPackage bool // the repository's newest package release
}

// version is a tag or a version without the v that tags carry.
func version(v string) string { return strings.TrimPrefix(strings.ToLower(v), "v") }

// refsOf is the releases of the repositories at keys, the newest first within each GitHub's, then the packages'. A package whose
// version is one of the repository's GitHub releases is the same release and is left out.
func (a API) refsOf(s *snapshot.Snapshot, keys []snapshot.RepoKey) []Ref {
	var out []Ref
	for _, k := range keys {
		e := s.Repos[k]
		if e == nil {
			continue
		}
		set, _ := Of(e)
		base := Ref{Key: k, Name: e.Repo.Name, Groups: s.GroupsOf(k), Source: GitHub}
		rows := make([]Ref, 0, 8)
		newest := set.Latest()
		gh := -1
		if set != nil {
			for i := range set.Releases {
				r := base
				r.Release = set.Releases[i]
				r.withNotes(set)
				if &set.Releases[i] == newest {
					gh = len(rows)
				}
				rows = append(rows, r)
			}
		}
		pk := -1
		if a.Packages != nil {
			for _, p := range a.Packages(e) {
				if p.Published.IsZero() || set != nil && slices.ContainsFunc(set.Releases, func(r Release) bool { return version(r.Tag) == version(p.Version) }) {
					continue
				}
				r := base
				r.Source, r.Package, r.Release = p.Ecosystem, p.Name, Release{Tag: p.Version, URL: p.URL, Published: p.Published}
				r.withNotes(set)
				if pk < 0 || r.Published.After(rows[pk].Published) {
					pk = len(rows)
				}
				rows = append(rows, r)
			}
		}
		all := gh
		if gh < 0 || pk >= 0 && rows[pk].Published.After(rows[gh].Published) {
			all = pk
		}
		if all >= 0 {
			rows[all].latestAll = true
		}
		if gh >= 0 {
			rows[gh].latestGitHub = true
		}
		if pk >= 0 {
			rows[pk].latestPackage = true
		}
		out = append(out, rows...)
	}
	return out
}

// withNotes settles where r's notes come from: its own, else the changelog's section of its version.
func (r *Ref) withNotes(set *Set) {
	switch sec := sectionOf(set, r.Tag); {
	case r.Notes != "":
		r.NotesFrom = "release"
	case sec != nil:
		r.Notes, r.NotesCut, r.NotesFrom = sec.Notes, sec.Cut, "changelog"
	}
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
	variant := oapi.ReleaseVariantLatest
	if p.Variant != nil {
		variant = *p.Variant
		if !slices.Contains([]oapi.ReleaseVariant{oapi.ReleaseVariantLatest, oapi.ReleaseVariantAll, oapi.ReleaseVariantStable, oapi.ReleaseVariantPre}, variant) {
			return badRequest("unknown variant"), nil
		}
	}
	source := oapi.ReleaseSourceAll
	if p.Source != nil {
		if source = *p.Source; !source.Valid() {
			return badRequest("unknown source"), nil
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
		All:     func(s *snapshot.Snapshot) []Ref { return a.refsOf(s, slices.Sorted(mapKeys(s))) },
		OfGroup: func(s *snapshot.Snapshot, id string) []Ref { g, _ := s.Group(id); return a.refsOf(s, g.Members) },
		OfRepo:  func(s *snapshot.Snapshot, k snapshot.RepoKey) []Ref { return a.refsOf(s, []snapshot.RepoKey{k}) },
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
		// The variants are counted under the chosen source, the sources under the chosen variant.
		for _, c := range [...]struct {
			variant oapi.ReleaseVariant
			n       *int
		}{{oapi.ReleaseVariantAll, &counts.All}, {oapi.ReleaseVariantStable, &counts.Stable}, {oapi.ReleaseVariantPre, &counts.Pre}, {oapi.ReleaseVariantLatest, &counts.Latest}} {
			if keeps(c.variant, source, r) {
				*c.n++
			}
		}
		if r.Source == GitHub && keeps(variant, oapi.ReleaseSourceGithub, r) {
			counts.Github++
		} else if r.Source != GitHub && keeps(variant, oapi.ReleaseSourcePackages, r) {
			counts.Packages++
		}
		if keeps(variant, source, r) {
			kept = append(kept, r)
		}
	}
	slices.SortStableFunc(kept, order(p.Sort))
	start, end, pg := scope.Paging(len(kept), number, size)
	page := oapi.ReleasePage{Releases: make([]oapi.ReleaseEntry, 0, end-start), Counts: counts, Page: pg,
		Freshness: scope.Freshness(s, in, now, func(e *snapshot.RepoEntry) snapshot.Freshness { _, f := Of(e); return f })}
	for _, r := range kept[start:end] {
		notes, from := "", ""
		if r.Notes != "" && a.Render != nil {
			notes, from = a.Render(s.Repos[r.Key], r.Notes).String(), r.NotesFrom
		}
		page.Releases = append(page.Releases, oapi.ReleaseEntry{Repo: string(r.Key), Name: r.Name, Groups: r.Groups, Source: r.Source, Package: r.Package, Tag: r.Tag, Title: r.Release.Name,
			Summary: r.Summary, Url: r.URL, PublishedAt: r.Published.UTC(), Prerelease: r.Pre, Author: r.Author, Bot: r.Bot, Assets: r.Assets,
			Notes: notes, NotesCut: r.NotesCut && notes != "", NotesFrom: oapi.ReleaseEntryNotesFrom(from)})
	}
	return oapi.ListReleases200JSONResponse{Body: page}, nil
}

// keeps reports whether variant lists r when the source is source: a GitHub release or a package, and for the latest the newest
// of its kind, or of both.
func keeps(variant oapi.ReleaseVariant, source oapi.ReleaseSource, r Ref) bool {
	switch {
	case source == oapi.ReleaseSourceGithub && r.Source != GitHub, source == oapi.ReleaseSourcePackages && r.Source == GitHub:
		return false
	}
	switch variant {
	case oapi.ReleaseVariantStable:
		return !r.Pre
	case oapi.ReleaseVariantPre:
		return r.Pre
	case oapi.ReleaseVariantAll:
		return true
	}
	switch source {
	case oapi.ReleaseSourceGithub:
		return r.latestGitHub
	case oapi.ReleaseSourcePackages:
		return r.latestPackage
	}
	return r.latestAll
}

// order is the order the sort names; ties fall to the newest, then the repository, so a page is stable.
func order(sort *oapi.ReleaseSort) func(x, y Ref) int {
	byRepo := func(x, y Ref) int { return cmp.Compare(x.Key, y.Key) }
	newest := func(x, y Ref) int {
		return cmp.Or(y.Published.Compare(x.Published), byRepo(x, y), cmp.Compare(x.Package, y.Package), cmp.Compare(x.Tag, y.Tag))
	}
	if sort == nil {
		return newest
	}
	switch *sort {
	case oapi.ReleaseSortOldest:
		return func(x, y Ref) int {
			return cmp.Or(x.Published.Compare(y.Published), byRepo(x, y), cmp.Compare(x.Package, y.Package), cmp.Compare(x.Tag, y.Tag))
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
