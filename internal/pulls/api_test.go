package pulls

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func pullSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	asked := at
	fresh := snapshot.Freshness{State: snapshot.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(), TTL: time.Hour}
	entry := func(id int64, name string, ps ...Pull) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, OpenIssues: 10}}
		facet.Set(e, &Set{Total: len(ps), Pulls: ps}, fresh)
		return e
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{
			entry(1, "x",
				Pull{Number: 1, Author: "ann", Updated: at, Asked: asked, Approved: true, Checks: "SUCCESS"},         // ready
				Pull{Number: 2, Author: "bo", Updated: at.Add(-72 * time.Hour), Asked: asked},                        // review, the oldest
				Pull{Number: 3, Author: "renovate[bot]", Bot: true, Updated: at.Add(-96 * time.Hour), Asked: asked}), // automation
			entry(2, "y",
				Pull{Number: 4, Author: "cy", Updated: at.Add(-time.Hour), Asked: asked, Changes: true}, // changes
				Pull{Number: 5, Author: "di", Draft: true, Updated: at.Add(-200 * time.Hour)}),          // draft
			{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 3}, Owner: "acme", Name: "z"}},
		}, nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "g", Name: "G", Members: []snapshot.RepoKey{"acme/y", "acme/z"}}})
	})
	p.Flush()
	return p.Current()
}

func TestListPulls(t *testing.T) {
	s := pullSnapshot(t)
	a := API{Current: func(context.Context) *snapshot.Snapshot { return s }}
	list := func(p oapi.ListPullsParams) oapi.PullPage {
		t.Helper()
		res, err := a.ListPulls(t.Context(), oapi.ListPullsRequestObject{Params: p})
		page, ok := res.(oapi.ListPulls200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list %+v: %T %v", p, res, err)
		}
		return page.Body
	}
	numbers := func(p oapi.PullPage) (out []int) {
		for _, x := range p.Pulls {
			out = append(out, x.Number)
		}
		return out
	}
	all := list(oapi.ListPullsParams{})
	if got := numbers(all); !slices.Equal(got, []int{2, 4, 1, 3}) {
		t.Errorf("whose turn first: %v; want people's waiting for review, then the longest idle, automation last, no draft", got)
	}
	c := all.Counts
	if c.Open != 5 || c.People != 4 || c.Automation != 1 || c.States.Review != 2 || c.States.Draft != 1 || c.States.Ready != 1 ||
		c.States.Changes != 1 || c.OldestReview == nil || !c.OldestReview.Equal(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("counts: %+v", c)
	}
	if p := all.Pulls[0]; p.Repo != "acme/x" || p.State != oapi.PullStateReview || p.WaitsFor != oapi.PullRequestWaitsForReviewers {
		t.Errorf("first: %+v", p)
	}
	if got := numbers(list(oapi.ListPullsParams{By: new(oapi.ListPullsParamsByPeople), Drafts: new(true)})); !slices.Equal(got, []int{2, 4, 1, 5}) {
		t.Errorf("people's with drafts: %v", got)
	}
	if got := numbers(list(oapi.ListPullsParams{State: new(oapi.PullStateDraft)})); !slices.Equal(got, []int{5}) {
		t.Errorf("drafts: %v", got)
	}
	g := list(oapi.ListPullsParams{Group: new("g")})
	if got := numbers(g); !slices.Equal(got, []int{4}) || g.Counts.Open != 2 || g.Freshness.Status != "stale" {
		t.Errorf("group: %v, %+v, %s", got, g.Counts, g.Freshness.Status)
	}
	if got := numbers(list(oapi.ListPullsParams{Sort: new(oapi.QueueSortUpdated), Limit: new(2), Page: new(2)})); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("updated, page 2: %v", got)
	}
	if res, _ := (API{Current: a.Current, Off: true}).ListPulls(t.Context(), oapi.ListPullsRequestObject{}); res == nil {
		t.Error("no answer switched off")
	} else if _, ok := res.(oapi.ListPulls404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("switched off: %T", res)
	}
	if res, _ := a.ListPulls(t.Context(), oapi.ListPullsRequestObject{Params: oapi.ListPullsParams{Repo: new("acme/nope")}}); res == nil {
		t.Error("no answer for an unknown repository")
	} else if _, ok := res.(oapi.ListPulls404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown repository: %T", res)
	}

	// The summary hooks: a repository's open pull requests; the dashboard counts the work that counts.
	e, _ := s.Repo("acme/x")
	var r oapi.Repo
	Repo(s, e, &r)
	if r.OpenPulls == nil || *r.OpenPulls != 3 {
		t.Errorf("repo summary: pulls %v", r.OpenPulls)
	}
	d := oapi.Dashboard{Work: oapi.Work{oapi.WorkKindHand, oapi.WorkKindAi}, Groups: []oapi.DashboardGroup{{Id: "g"}}}
	Dashboard(s, &d)
	if d.Pulls.Open != 4 || d.Pulls.Automation != 0 || d.Groups[0].Pulls.Open != 2 {
		t.Errorf("dashboard without automation: %+v, group %+v", d.Pulls, d.Groups[0].Pulls)
	}
}
