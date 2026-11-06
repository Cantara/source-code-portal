package releases

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
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
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.All)})); len(got) != 6 || got[0] != "core v2.1.0-rc1" || got[1] != "secret v9.0.0" {
		t.Errorf("all = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.Pre)})); !slices.Equal(got, []string{"core v2.1.0-rc1"}) {
		t.Errorf("pre = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Variant: variant(oapi.Stable), Group: str("tools")})); !slices.Equal(got, []string{"core v2.0.0", "core v1.0.0", "old v0.1.0"}) {
		t.Errorf("stable in the group = %v", got)
	}
	if got := tags(list(oapi.ListReleasesParams{Since: str("30d"), Variant: variant(oapi.All)})); len(got) != 4 {
		t.Errorf("since 30d = %v, want the four published in the last 30 days", got)
	}
	if p := list(oapi.ListReleasesParams{Since: str("30d")}); p.Counts.All != 4 || p.Counts.Latest != 3 {
		t.Errorf("counts follow since: %+v", p.Counts)
	}
	if got := tags(list(oapi.ListReleasesParams{Repo: str("acme/core"), Variant: variant(oapi.All)})); len(got) != 3 {
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
	res, _ := a.ListReleases(t.Context(), oapi.ListReleasesRequestObject{Params: oapi.ListReleasesParams{Variant: new(oapi.All)}})
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
