package catalog

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// archiveSnapshot is a fleet of five: two archived (one private), one deprecated by its descriptor, one live and one live
// whose descriptor says production.
func archiveSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	fresh := snapshot.Freshness{State: snapshot.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(), TTL: time.Hour}
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	entry := func(id int64, name string, archived bool, pushed time.Time, lifecycle string) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name,
			Visibility: "public", Archived: archived, PushedAt: pushed}, Fresh: fresh}
		if lifecycle != "" {
			descriptorFacet.Set(e, Descriptor{Present: true, Lifecycle: lifecycle}, fresh)
		}
		return e
	}
	private := entry(5, "secret-old", true, at.Add(-time.Hour), "")
	private.Repo.Visibility = "private"
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			entry(1, "old-lib", true, at.Add(-48*time.Hour), ""),
			entry(2, "legacy-app", false, at, "deprecated"),
			entry(3, "live-app", false, at.Add(24*time.Hour), "production"),
			entry(4, "both", true, at.Add(-24*time.Hour), "deprecated"), // archived wins
			private,
		}, nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "tools", Name: "Tools", Members: []snapshot.RepoKey{"acme/both", "acme/live-app", "acme/old-lib"}}})
	})
	p.Flush()
	return p.Current()
}

func TestListArchive(t *testing.T) {
	s := archiveSnapshot(t)
	used := map[snapshot.RepoKey]int{"acme/old-lib": 3, "acme/legacy-app": 1}
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }, ArchiveEntry: []func(*snapshot.Snapshot, *snapshot.RepoEntry, *oapi.ArchiveEntry){
		func(_ *snapshot.Snapshot, e *snapshot.RepoEntry, r *oapi.ArchiveEntry) { r.UsedBy = used[e.Repo.Key()] }}}
	list := func(p oapi.ListArchiveParams) oapi.ArchivePage {
		t.Helper()
		res, err := a.ListArchive(t.Context(), oapi.ListArchiveRequestObject{Params: p})
		page, ok := res.(oapi.ListArchive200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list %+v: %T %v", p, res, err)
		}
		return page.Body
	}
	names := func(p oapi.ArchivePage) (out []string) {
		for _, r := range p.Archive {
			out = append(out, r.Repo.Name)
		}
		return out
	}
	str, flag := func(v string) *string { return &v }, func(v bool) *bool { return &v }

	all := list(oapi.ListArchiveParams{})
	if got, want := names(all), []string{"legacy-app", "secret-old", "both", "old-lib"}; !slices.Equal(got, want) {
		t.Errorf("default order (last active first) = %v, want %v", got, want)
	}
	if c := all.Counts; c.Total != 4 || c.Archived != 3 || c.Deprecated != 1 || c.Used != 2 {
		t.Errorf("counts = %+v", c)
	}
	for _, r := range all.Archive {
		if r.Repo.Name == "both" && r.Reason != oapi.ArchiveReasonArchived {
			t.Errorf("archived and deprecated: reason %q, want archived", r.Reason)
		}
		if r.Repo.Name == "legacy-app" && r.Reason != oapi.ArchiveReasonDeprecated {
			t.Errorf("descriptor-deprecated: reason %q", r.Reason)
		}
	}

	reason := oapi.ArchiveReasonDeprecated
	if got := names(list(oapi.ListArchiveParams{Reason: &reason})); !slices.Equal(got, []string{"legacy-app"}) {
		t.Errorf("reason=deprecated = %v", got)
	}
	if p := list(oapi.ListArchiveParams{Reason: &reason}); p.Counts.Total != 4 {
		t.Errorf("counts follow the group and q only, not the reason: %+v", p.Counts)
	}
	if got := names(list(oapi.ListArchiveParams{Used: flag(true)})); !slices.Equal(got, []string{"legacy-app", "old-lib"}) {
		t.Errorf("used = %v", got)
	}
	if got := names(list(oapi.ListArchiveParams{Q: str("OLD")})); !slices.Equal(got, []string{"secret-old", "old-lib"}) {
		t.Errorf("q=OLD = %v", got)
	}
	if got := names(list(oapi.ListArchiveParams{Group: str("tools")})); !slices.Equal(got, []string{"both", "old-lib"}) {
		t.Errorf("group = %v", got)
	}
	for sort, want := range map[oapi.ArchiveSort][]string{
		oapi.ArchiveSortName:   {"both", "legacy-app", "old-lib", "secret-old"},
		oapi.ArchiveSortUsed:   {"old-lib", "legacy-app", "secret-old", "both"},
		oapi.ArchiveSortGroup:  {"both", "old-lib", "legacy-app", "secret-old"}, // ungrouped last
		oapi.ArchiveSortActive: {"legacy-app", "secret-old", "both", "old-lib"},
	} {
		if got := names(list(oapi.ListArchiveParams{Sort: &sort})); !slices.Equal(got, want) {
			t.Errorf("sort %s = %v, want %v", sort, got, want)
		}
	}

	one, two := 1, 2
	if p := list(oapi.ListArchiveParams{Limit: &two, Page: &two}); !slices.Equal(names(p), []string{"both", "old-lib"}) || p.Page.Pages != 2 || p.Page.Total != 4 {
		t.Errorf("page 2 of 2 = %v %+v", names(p), p.Page)
	}
	if p := list(oapi.ListArchiveParams{Limit: &one, Page: &two}); len(p.Archive) != 1 {
		t.Errorf("page size 1 = %v", names(p))
	}

	for name, p := range map[string]oapi.ListArchiveParams{"reason": {Reason: ptr(oapi.ArchiveReason("gone"))}, "limit": {Limit: ptr(0)}} {
		res, _ := a.ListArchive(t.Context(), oapi.ListArchiveRequestObject{Params: p})
		if _, ok := res.(oapi.ListArchive400ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("bad %s: %T, want 400", name, res)
		}
	}
	res, _ := a.ListArchive(t.Context(), oapi.ListArchiveRequestObject{Params: oapi.ListArchiveParams{Group: str("nope")}})
	if _, ok := res.(oapi.ListArchive404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown group: %T, want 404", res)
	}
}

// TestArchivePublicView: the public view of the same fleet holds no private archive entry: not as a row and not in the counts.
func TestArchivePublicView(t *testing.T) {
	pub := archiveSnapshot(t).Public()
	a := API{Current: func(context.Context) *snapshot.Snapshot { return pub }}
	res, _ := a.ListArchive(t.Context(), oapi.ListArchiveRequestObject{})
	page := res.(oapi.ListArchive200JSONResponse).Body
	if page.Counts.Total != 3 {
		t.Errorf("public total = %d, want 3", page.Counts.Total)
	}
	for _, r := range page.Archive {
		if r.Repo.Name == "secret-old" {
			t.Fatal("private archive entry listed for the public")
		}
	}
}

func ptr[T any](v T) *T { return &v }
