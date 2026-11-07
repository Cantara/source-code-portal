package releases

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
)

// releaseSnapshot is five repositories: core (three releases, the newest a pre-release, in the group tools with old), web (one), old
// (one a year ago), a private one and one never read.
func releaseSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	now := time.Now().UTC()
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	spec := New(nil).Spec()
	entry := func(id int64, name string, private bool) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: "public"}}
		if private {
			e.Repo.Visibility = "private"
		}
		return e
	}
	sets := map[string][]Release{
		"core":   {{Tag: "v2.1.0-rc1", Pre: true, Published: ago(1)}, {Tag: "v2.0.0", Name: "Two", Published: ago(10)}, {Tag: "v1.0.0", Published: ago(40)}},
		"web":    {{Tag: "v0.3.0", Published: ago(5), Author: "ann"}},
		"old":    {{Tag: "v0.1.0", Published: ago(400)}},
		"secret": {{Tag: "v9.0.0", Published: ago(2)}},
	}
	repos := []*snapshot.RepoEntry{entry(1, "core", false), entry(2, "web", false), entry(3, "old", false), entry(4, "secret", true), entry(5, "unread", false)}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", repos, nil, snapshot.SourceStatus{ID: "gh"})
		for _, name := range []string{"core", "web", "old", "secret"} {
			payload, _ := json.Marshal(build(sets[name]))
			spec.Publish(b, store.Item{Repo: snapshot.KeyOf("acme", name), State: snapshot.StateOK, Payload: payload},
				snapshot.Freshness{State: snapshot.StateOK, FetchedAt: now, CheckedAt: now, TTL: time.Hour})
		}
		b.SetGroups([]snapshot.Group{{ID: "tools", Name: "Tools", Members: []snapshot.RepoKey{"acme/core", "acme/old"}}})
	})
	p.Flush()
	return p.Current()
}

func TestListReleases(t *testing.T) {
	s := releaseSnapshot(t)
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }}
	list := func(p oapi.ListReleasesParams) oapi.ReleasePage {
		t.Helper()
		res, err := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: p})
		page, ok := res.(oapi.ListReleases200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list %+v: %T %v", p, res, err)
		}
		return page.Body
	}
	tags := func(p oapi.ReleasePage) (out []string) {
		for _, r := range p.Releases {
			out = append(out, r.Repo[5:]+" "+r.Tag)
		}
		return out
	}
	str := func(v string) *string { return &v }
	variant := func(v oapi.ReleaseVariant) *oapi.ReleaseVariant { return &v }
	sort := func(v oapi.ReleaseSort) *oapi.ReleaseSort { return &v }

	all := list(oapi.ListReleasesParams{})
	if got, want := tags(all), []string{"secret v9.0.0", "web v0.3.0", "core v2.0.0", "old v0.1.0"}; !slices.Equal(got, want) {
		t.Errorf("latest (default): %v, want %v: a stable release per repository, newest first, the pre-release not the latest", got, want)
	}
	if c := all.Counts; c.Latest != 4 || c.All != 6 || c.Stable != 5 || c.Pre != 1 {
		t.Errorf("counts = %+v", c)
	}
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.ReleaseVariantAll)})); len(got) != 6 || got[0] != "core v2.1.0-rc1" || got[1] != "secret v9.0.0" {
		t.Errorf("all = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.ReleaseVariantPre)})); !slices.Equal(got, []string{"core v2.1.0-rc1"}) {
		t.Errorf("pre = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.ReleaseVariantStable), Group: str("tools")})); !slices.Equal(got, []string{"core v2.0.0", "core v1.0.0", "old v0.1.0"}) {
		t.Errorf("stable in the group = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Since: str("30d"), Variant: variant(oapi.ReleaseVariantAll)})); len(got) != 4 {
		t.Errorf("since 30d = %v, want the four published in the last 30 days", got)
	}
	if p := list(oapi.ListReleasesParams{Since: str("30d")}); p.Counts.All != 4 || p.Counts.Latest != 3 {
		t.Errorf("counts follow since: %+v", p.Counts)
	}
	if got := tags(list(oapi.ListReleasesParams{Repo: str("acme/core"), Variant: variant(oapi.ReleaseVariantAll)})); len(got) != 3 {
		t.Errorf("one repository = %v", got)
	}
	for sorted, want := range map[oapi.ReleaseSort][]string{
		oapi.ReleaseSortOldest: {"old v0.1.0", "core v2.0.0", "web v0.3.0", "secret v9.0.0"},
		oapi.ReleaseSortRepo:   {"core v2.0.0", "old v0.1.0", "secret v9.0.0", "web v0.3.0"},
		oapi.ReleaseSortGroup:  {"core v2.0.0", "old v0.1.0", "secret v9.0.0", "web v0.3.0"}, // tools first, then the ungrouped by recency
		oapi.ReleaseSortNewest: {"secret v9.0.0", "web v0.3.0", "core v2.0.0", "old v0.1.0"},
	} {
		if got := tags(list(oapi.ListReleasesParams{Sort: sort(sorted)})); !slices.Equal(got, want) {
			t.Errorf("sort %s = %v, want %v", sorted, got, want)
		}
	}
	two, one := 2, 1
	if p := list(oapi.ListReleasesParams{Limit: &two, Page: &two}); !slices.Equal(tags(p), []string{"core v2.0.0", "old v0.1.0"}) || p.Page.Pages != 2 || p.Page.Total != 4 {
		t.Errorf("page 2 = %v %+v", tags(p), p.Page)
	}
	if r := list(oapi.ListReleasesParams{Limit: &one}).Releases; len(r) != 1 || r[0].Tag != "v9.0.0" {
		t.Errorf("first = %+v", r)
	}
	if r := list(oapi.ListReleasesParams{Repo: str("acme/web")}).Releases[0]; r.Author != "ann" || r.Assets != 0 || r.Name != "web" {
		t.Errorf("fields = %+v", r)
	}

	for name, p := range map[string]oapi.ListReleasesParams{"variant": {Variant: variant("everything")}, "since": {Since: str("last week")},
		"limit": {Limit: new(0)}, "negative days": {Since: str("-3d")}} {
		res, _ := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: p})
		if _, ok := res.(oapi.ListReleases400ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("bad %s: %T, want 400", name, res)
		}
	}
	for _, p := range []oapi.ListReleasesParams{{Group: str("nope")}, {Repo: str("acme/nope")}} {
		res, _ := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: p})
		if _, ok := res.(oapi.ListReleases404ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("%+v: %T, want 404", p, res)
		}
	}
}

// TestReleasesPublicView: the public view of the same fleet lists nothing of the private repository, not even in the counts.
func TestReleasesPublicView(t *testing.T) {
	pub := releaseSnapshot(t).Public()
	a := API{Current: func(context.Context) *snapshot.Snapshot { return pub }}
	res, _ := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: oapi.ListReleasesParams{Variant: new(oapi.ReleaseVariantAll)}})
	page := res.(oapi.ListReleases200JSONResponse).Body
	if page.Counts.All != 5 {
		t.Errorf("public releases = %d, want 5", page.Counts.All)
	}
	for _, r := range page.Releases {
		if r.Name == "secret" {
			t.Fatalf("a private repository's release for the public: %+v", r)
		}
	}
}

// TestReleasesOfPackages: a published package's newest release is a row beside the GitHub releases (design/releases.md, RL4): the
// latest of a repository is the newest of both, a package of the version of a GitHub release is that release, one a registry does
// not date is not listed, and the counts follow the source and the variant.
func TestReleasesOfPackages(t *testing.T) {
	s := releaseSnapshot(t)
	now := time.Now().UTC()
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	published := map[string][]Published{
		"core":   {{Ecosystem: "maven", Name: "acme-core", Version: "2.0.0", Published: ago(9)}, {Ecosystem: "npm", Name: "core-js", Version: "2.2.0", URL: "https://registry/core-js", Published: ago(3)}},
		"web":    {{Ecosystem: "pypi", Name: "web", Version: "0.4.0", Published: ago(20)}, {Ecosystem: "go", Name: "web-go", Version: "0.5.0"}},
		"unread": {{Ecosystem: "cargo", Name: "unread", Version: "1.0.0", Published: ago(4)}},
	}
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }, Packages: func(e *snapshot.RepoEntry) []Published { return published[e.Repo.Name] }}
	list := func(p oapi.ListReleasesParams) oapi.ReleasePage {
		t.Helper()
		res, err := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: p})
		page, ok := res.(oapi.ListReleases200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list %+v: %T %v", p, res, err)
		}
		return page.Body
	}
	rows := func(p oapi.ReleasePage) (out []string) {
		for _, r := range p.Releases {
			out = append(out, r.Source+" "+r.Repo[5:]+" "+r.Tag)
		}
		return out
	}
	source := func(v oapi.ReleaseSource) *oapi.ReleaseSource { return &v }
	variant := func(v oapi.ReleaseVariant) *oapi.ReleaseVariant { return &v }

	both := list(oapi.ListReleasesParams{})
	want := []string{"github secret v9.0.0", "npm core 2.2.0", "cargo unread 1.0.0", "github web v0.3.0", "github old v0.1.0"}
	if got := rows(both); !slices.Equal(got, want) {
		t.Errorf("latest of both = %v, want %v: the newest of a repository's GitHub release and packages, a repository with only a package too", got, want)
	}
	if c := both.Counts; c.Latest != 5 || c.All != 9 || c.Stable != 8 || c.Pre != 1 || c.Github != 4 || c.Packages != 3 { // the sources count what choosing them lists
		t.Errorf("counts = %+v", c)
	}
	if r := both.Releases[1]; r.Package != "core-js" || r.Url != "https://registry/core-js" || r.Prerelease || r.Author != "" || r.Title != "" {
		t.Errorf("a package's row = %+v", r)
	}
	if got := rows(list(oapi.ListReleasesParams{Source: source(oapi.ReleaseSourceGithub)})); !slices.Equal(got, []string{"github secret v9.0.0", "github web v0.3.0", "github core v2.0.0", "github old v0.1.0"}) {
		t.Errorf("github = %v", got)
	}
	pk := list(oapi.ListReleasesParams{Source: source(oapi.ReleaseSourcePackages)})
	if got, want := rows(pk), []string{"npm core 2.2.0", "cargo unread 1.0.0", "pypi web 0.4.0"}; !slices.Equal(got, want) {
		t.Errorf("packages = %v, want %v: the maven one is GitHub's v2.0.0, the go one has no date", got, want)
	}
	if c := pk.Counts; c.Latest != 3 || c.All != 3 || c.Stable != 3 || c.Pre != 0 || c.Github != 4 || c.Packages != 3 {
		t.Errorf("counts of the packages = %+v: the variants follow the source, the sources the variant", c)
	}
	all := list(oapi.ListReleasesParams{Variant: variant(oapi.ReleaseVariantAll)})
	if len(all.Releases) != 9 || all.Counts.Github != 6 || all.Counts.Packages != 3 {
		t.Errorf("every release = %v %+v", rows(all), all.Counts)
	}
	if got := rows(list(oapi.ListReleasesParams{Variant: variant(oapi.ReleaseVariantPre), Source: source(oapi.ReleaseSourcePackages)})); len(got) != 0 {
		t.Errorf("a package is never a pre-release: %v", got)
	}
	if got := rows(list(oapi.ListReleasesParams{Repo: new("acme/unread"), Variant: variant(oapi.ReleaseVariantAll)})); !slices.Equal(got, []string{"cargo unread 1.0.0"}) {
		t.Errorf("a repository with only a package = %v", got)
	}
	if got := rows(list(oapi.ListReleasesParams{Since: new("30d"), Source: source(oapi.ReleaseSourcePackages), Variant: variant(oapi.ReleaseVariantAll)})); len(got) != 3 {
		t.Errorf("since keeps the packages published in the window: %v", got)
	}
	res, _ := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: oapi.ListReleasesParams{Source: source("registry")}})
	if _, ok := res.(oapi.ListReleases400ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("an unknown source: %T, want 400", res)
	}
	// Without a packages hook the list is GitHub's alone, as before.
	plain := API{Current: a.Current}
	if res, _ := plain.ListReleases(t.Context(), oapi.ListReleasesRequestObject{}); len(res.(oapi.ListReleases200JSONResponse).Body.Releases) != 4 {
		t.Error("without the hook: GitHub's four")
	}
}

// TestReleaseNotes: a row carries its release's notes, else the changelog's section of its version (a package too); the hook renders
// them, and without it a row has none.
func TestReleaseNotes(t *testing.T) {
	now := time.Now().UTC()
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	set := Set{
		Releases: []Release{{Tag: "v2.1.0", Notes: "own **notes**", Published: ago(2)}, {Tag: "v2.0.0", Published: ago(9)}, {Tag: "v1.0.0", Published: ago(40)}},
		Changelog: []Section{{Version: "2.1.0", Notes: "ignored: the release has its own"}, {Version: "2.0.0", Notes: "from the changelog", Cut: true},
			{Version: "2.2.0", Notes: "the package section"}},
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	spec := New(nil).Spec()
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "core", Visibility: "public"}}}, nil, snapshot.SourceStatus{ID: "gh"})
		payload, _ := json.Marshal(set)
		spec.Publish(b, store.Item{Repo: "acme/core", State: snapshot.StateOK, Payload: payload}, snapshot.Freshness{State: snapshot.StateOK, FetchedAt: now, CheckedAt: now, TTL: time.Hour})
	})
	p.Flush()
	s := p.Current()
	render := func(_ *snapshot.RepoEntry, md string) safehtml.SafeHTML { return safehtml.Text(md) }
	published := func(*snapshot.RepoEntry) []Published {
		return []Published{{Ecosystem: "maven", Name: "core", Version: "2.2.0", Published: ago(1)}}
	}
	list := func(a API) map[string]oapi.ReleaseEntry {
		t.Helper()
		all := oapi.ReleaseVariantAll
		res, err := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: oapi.ListReleasesParams{Variant: &all}})
		page, ok := res.(oapi.ListReleases200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list: %T %v", res, err)
		}
		out := map[string]oapi.ReleaseEntry{}
		for _, r := range page.Body.Releases {
			out[r.Tag] = r
		}
		return out
	}
	rows := list(API{Current: func(context.Context) *snapshot.Snapshot { return s }, Packages: published, Render: render})
	if r := rows["v2.1.0"]; r.NotesFrom != oapi.ReleaseEntryNotesFromRelease || !strings.Contains(r.Notes, "own **notes**") || r.NotesCut {
		t.Errorf("a release's own notes = %+v", r)
	}
	if r := rows["v2.0.0"]; r.NotesFrom != oapi.ReleaseEntryNotesFromChangelog || !strings.Contains(r.Notes, "from the changelog") || !r.NotesCut {
		t.Errorf("a release without notes takes the changelog's section, cut as it was = %+v", r)
	}
	if r := rows["2.2.0"]; r.Package != "core" || r.NotesFrom != oapi.ReleaseEntryNotesFromChangelog || !strings.Contains(r.Notes, "the package section") {
		t.Errorf("a package release takes the changelog's section = %+v", r)
	}
	if r := rows["v1.0.0"]; r.Notes != "" || r.NotesFrom != "" || r.NotesCut {
		t.Errorf("no notes anywhere = %+v", r)
	}
	for tag, r := range list(API{Current: func(context.Context) *snapshot.Snapshot { return s }}) {
		if r.Notes != "" || r.NotesFrom != "" {
			t.Errorf("%s has notes without a renderer: %+v", tag, r)
		}
	}
}
