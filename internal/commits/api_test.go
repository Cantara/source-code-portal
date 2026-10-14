package commits

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func feedSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fresh := snapshot.Freshness{State: snapshot.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(), TTL: time.Hour}
	entry := func(id int64, name string, n int, f snapshot.Freshness) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name}}
		var cs []Commit
		for i := range n {
			cs = append(cs, Commit{SHA: sha(byte('a' + int(id)*5 + i)), CommittedAt: at.Add(-time.Duration(i) * time.Hour)})
		}
		facet.Set(e, cs, f)
		return e
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(0, "x", 3, fresh), entry(1, "y", 2, fresh), entry(2, "z", 1, snapshot.Freshness{})},
			nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "g", Name: "G", Members: []snapshot.RepoKey{"acme/x", "acme/y"}}})
	})
	p.Flush()
	return p.Current()
}

func TestListCommitsPages(t *testing.T) {
	s := feedSnapshot(t)
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }}
	list := func(p oapi.ListCommitsParams) oapi.ListCommitsResponseObject {
		res, err := a.ListCommits(t.Context(), oapi.ListCommitsRequestObject{Params: p})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	var all []string
	p := oapi.ListCommitsParams{Group: new("g"), Limit: new(2)}
	for {
		page, ok := list(p).(oapi.ListCommits200JSONResponse)
		if !ok || page.Body.Freshness.Status != "fresh" || len(all) > 5 {
			t.Fatalf("page: %+v", page)
		}
		for _, c := range page.Body.Commits {
			all = append(all, c.Repo+"@"+c.Sha[:1])
		}
		if page.Body.Page.NextCursor == nil {
			break
		}
		p.Cursor = page.Body.Page.NextCursor
	}
	want := []string{"acme/x@a", "acme/y@f", "acme/x@b", "acme/y@g", "acme/x@c"}
	if len(all) != len(want) {
		t.Fatalf("feed = %v, want %v", all, want)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("feed = %v, want %v", all, want)
		}
	}

	if page := list(oapi.ListCommitsParams{}).(oapi.ListCommits200JSONResponse); len(page.Body.Commits) != 6 ||
		page.Body.Freshness.Status != "stale" || page.Body.Freshness.FetchedAt != nil {
		t.Errorf("all repositories: %+v", page.Body)
	}
	if page := list(oapi.ListCommitsParams{Repo: new("ACME/z")}).(oapi.ListCommits200JSONResponse); len(page.Body.Commits) != 1 ||
		page.Body.Freshness.Status != "unknown" {
		t.Errorf("one repository: %+v", page.Body)
	}
	if page := list(oapi.ListCommitsParams{Group: new("g"), Repo: new("acme/z")}).(oapi.ListCommits200JSONResponse); len(page.Body.Commits) != 0 {
		t.Errorf("a repository outside the group: %+v", page.Body)
	}
	other := list(oapi.ListCommitsParams{Limit: new(1)}).(oapi.ListCommits200JSONResponse).Body.Page.NextCursor
	for name, p := range map[string]oapi.ListCommitsParams{
		"limit 0":           {Limit: new(0)},
		"limit 201":         {Limit: new(201)},
		"garbage cursor":    {Cursor: new("!!")},
		"cursor of another": {Group: new("g"), Cursor: other},
	} {
		if _, ok := list(p).(oapi.ListCommits400ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("%s: not a 400", name)
		}
	}
	for name, p := range map[string]oapi.ListCommitsParams{"group": {Group: new("nope")}, "repo": {Repo: new("acme/nope")}} {
		if _, ok := list(p).(oapi.ListCommits404ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("unknown %s: not a 404", name)
		}
	}
}
