package issues

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func issueSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	fresh := snapshot.Freshness{State: snapshot.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(), TTL: time.Hour}
	entry := func(id int64, name string, is ...Issue) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name}}
		facet.Set(e, &Set{Total: len(is), Exact: true, Issues: is}, fresh)
		return e
	}
	bug := []Label{{Name: "bug"}}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			entry(1, "x",
				Issue{Number: 1, Author: "ann", Created: at, Updated: at, Labels: bug},                               // unassigned bug
				Issue{Number: 2, Author: "bo", Created: at.Add(-72 * time.Hour), Updated: at.Add(-72 * time.Hour)},   // triage, the oldest
				Issue{Number: 3, Author: "renovate[bot]", Bot: true, Created: at, Updated: at.Add(-96 * time.Hour)}), // automation
			entry(2, "y",
				Issue{Number: 4, Author: "cy", Created: at, Updated: at.Add(-time.Hour), Assigned: 1, Assignees: []string{"cy"}}, // assigned
				Issue{Number: 5, Author: "di", Created: at, Updated: at.Add(-2 * time.Hour), Type: "Feature"}),                   // unassigned feature
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 3}, Owner: "acme", Name: "z"}},
		}, nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "g", Name: "G", Members: []snapshot.RepoKey{"acme/y", "acme/z"}}})
	})
	p.Flush()
	return p.Current()
}

func TestListIssues(t *testing.T) {
	s := issueSnapshot(t)
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }}
	list := func(p oapi.ListIssuesParams) oapi.IssuePage {
		t.Helper()
		res, err := a.ListIssues(t.Context(), oapi.ListIssuesRequestObject{Params: p})
		page, ok := res.(oapi.ListIssues200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list %+v: %T %v", p, res, err)
		}
		return page.Body
	}
	numbers := func(p oapi.IssuePage) (out []int) {
		for _, x := range p.Issues {
			out = append(out, x.Number)
		}
		return out
	}
	all := list(oapi.ListIssuesParams{})
	if got := numbers(all); !slices.Equal(got, []int{2, 5, 1, 4, 3}) {
		t.Errorf("what waits first: %v; want people's needing triage, unassigned, assigned, each the longest idle first, automation last", got)
	}
	c := all.Counts
	if c.Open != 5 || c.People != 4 || c.Automation != 1 || c.States.Triage != 2 || c.States.Unassigned != 2 || c.States.Assigned != 1 ||
		c.Kinds.Bug != 1 || c.Kinds.Feature != 1 || c.Kinds.Other != 3 || c.OldestTriage == nil || !c.OldestTriage.Equal(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("counts: %+v", c)
	}
	if got := numbers(list(oapi.ListIssuesParams{By: new(oapi.ListIssuesParamsByPeople), State: new(oapi.Unassigned)})); !slices.Equal(got, []int{5, 1}) {
		t.Errorf("people's unassigned: %v", got)
	}
	if got := numbers(list(oapi.ListIssuesParams{Kind: new(oapi.Bug)})); !slices.Equal(got, []int{1}) {
		t.Errorf("bugs: %v", got)
	}
	g := list(oapi.ListIssuesParams{Group: new("g")})
	if got := numbers(g); !slices.Equal(got, []int{5, 4}) || g.Counts.Open != 2 || g.Freshness.Status != "stale" {
		t.Errorf("group: %v, %+v, %s", got, g.Counts, g.Freshness.Status)
	}
	if got := numbers(list(oapi.ListIssuesParams{Sort: new(oapi.Newest), Limit: new(2)})); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("newest: %v", got)
	}
	if res, _ := a.ListIssues(t.Context(), oapi.ListIssuesRequestObject{Params: oapi.ListIssuesParams{Repo: new("acme/nope")}}); res == nil {
		t.Error("no answer for an unknown repository")
	} else if _, ok := res.(oapi.ListIssues404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown repository: %T", res)
	}
	if _, ok := must(a.ListIssues(t.Context(), oapi.ListIssuesRequestObject{Params: oapi.ListIssuesParams{Kind: new(oapi.IssueKind("x"))}})).(oapi.ListIssues400ApplicationProblemPlusJSONResponse); !ok {
		t.Error("an unknown kind is not refused")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
