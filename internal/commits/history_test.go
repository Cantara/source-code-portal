package commits

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// historyAPI serves one repository acme/x holding h.
func historyAPI(t *testing.T, h *History) API {
	t.Helper()
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x", DefaultBranch: "main"}}
	facet.Set(e, h, snapshot.Freshness{State: snapshot.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(), TTL: time.Hour})
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{e}, nil, snapshot.SourceStatus{ID: "gh"})
	})
	p.Flush()
	s := p.Current()
	return API{Current: func(context.Context) *snapshot.Snapshot { return s }}
}

func getHistory(t *testing.T, a API, p oapi.GetHistoryParams) oapi.GetHistoryResponseObject {
	t.Helper()
	res, err := a.GetHistory(t.Context(), oapi.GetHistoryRequestObject{Owner: "acme", Name: "x", Params: p})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// walk reads every page and returns the SHAs in order.
func walk(t *testing.T, a API, p oapi.GetHistoryParams) []string {
	t.Helper()
	var out []string
	for range 1000 {
		page, ok := getHistory(t, a, p).(oapi.GetHistory200JSONResponse)
		if !ok {
			t.Fatalf("page: %+v", page)
		}
		for _, c := range page.Body.Commits {
			out = append(out, c.Sha)
		}
		if page.Body.Page.NextCursor == nil {
			return out
		}
		p.Cursor = page.Body.Page.NextCursor
	}
	t.Fatal("no end of pages")
	return nil
}

// randomDAG is n commits, cs[i] younger than cs[j] for i < j, with merges and skewed clocks.
func randomDAG(rng *rand.Rand, round, n int) []Commit {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cs := make([]Commit, n)
	for i := range cs {
		cs[i] = Commit{SHA: fmt.Sprintf("%040x", round*10000+i), Subject: fmt.Sprint("commit ", i), AuthorName: "Ada", Parents: []string{},
			CommittedAt: base.Add(-time.Duration(i) * time.Hour)}
		if rng.IntN(5) == 0 {
			cs[i].CommittedAt = cs[i].CommittedAt.Add(time.Duration(rng.IntN(96)-48) * time.Hour)
		}
		if rng.IntN(4) == 0 {
			cs[i].AuthorName = "renovate[bot]"
		}
	}
	for i := range n - 1 {
		for range 1 + rng.IntN(2)*rng.IntN(3) {
			if p := cs[i+1+rng.IntN(n-i-1)].SHA; !slices.Contains(cs[i].Parents, p) {
				cs[i].Parents = append(cs[i].Parents, p)
			}
		}
	}
	return cs
}

// TestHistoryPages: on random DAGs every page split lists each commit once, parents after children, and a filtered walk is the
// unfiltered order without the filtered-out commits.
func TestHistoryPages(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for round := range 100 {
		cs := randomDAG(rng, round, 2+rng.IntN(120))
		a := historyAPI(t, newHistory(cs, nil, []Label{{Name: "main", Kind: "branch", SHA: cs[0].SHA}}, "main"))
		all := walk(t, a, oapi.GetHistoryParams{Limit: new(1 + rng.IntN(30))})
		pos := map[string]int{}
		for i, s := range all {
			pos[s] = i
		}
		if len(all) != len(cs) || len(pos) != len(cs) {
			t.Fatalf("round %d: %d of %d commits", round, len(pos), len(cs))
		}
		for _, c := range cs {
			for _, p := range c.Parents {
				if pos[p] <= pos[c.SHA] {
					t.Fatalf("round %d: a parent before its child", round)
				}
			}
		}
		people := walk(t, a, oapi.GetHistoryParams{Automation: new(false), Limit: new(1 + rng.IntN(30))})
		want := slices.DeleteFunc(slices.Clone(all), func(s string) bool {
			return cs[slices.IndexFunc(cs, func(c Commit) bool { return c.SHA == s })].AuthorName == "renovate[bot]"
		})
		if !slices.Equal(people, want) {
			t.Fatalf("round %d: without automation %d commits, want %d", round, len(people), len(want))
		}
	}
}

// TestHistoryPush: a page read after a push continues where the last one ended, neither repeating nor skipping a commit.
func TestHistoryPush(t *testing.T) {
	cs := randomDAG(rand.New(rand.NewPCG(5, 6)), 0, 80)
	before := historyAPI(t, newHistory(cs, nil, nil, "main"))
	first := getHistory(t, before, oapi.GetHistoryParams{Limit: new(30)}).(oapi.GetHistory200JSONResponse).Body
	pushed := []Commit{{SHA: sha('f'), Parents: []string{sha('e')}, CommittedAt: cs[0].CommittedAt.Add(2 * time.Hour)},
		{SHA: sha('e'), Parents: []string{cs[0].SHA}, CommittedAt: cs[0].CommittedAt.Add(time.Hour)}}
	after := historyAPI(t, newHistory(append(pushed, cs...), nil, nil, "main"))
	got := slices.Collect(func(yield func(string) bool) {
		for _, c := range first.Commits {
			yield(c.Sha)
		}
	})
	got = append(got, walk(t, after, oapi.GetHistoryParams{Limit: new(30), Cursor: first.Page.NextCursor})...)
	if want := walk(t, before, oapi.GetHistoryParams{Limit: new(200)}); !slices.Equal(got, want) {
		t.Errorf("pages across a push = %d commits, want the %d held before it in order", len(got), len(want))
	}
}

func TestHistoryFilters(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cs := []Commit{
		{SHA: sha('d'), Subject: "Merge feature", AuthorName: "Ada", AuthorLogin: "ada", Parents: []string{sha('c'), sha('b')}, CommittedAt: at.Add(3 * time.Hour)},
		{SHA: sha('c'), Subject: "chore(deps): update", AuthorName: "renovate[bot]", Parents: []string{sha('a')}, CommittedAt: at.Add(2 * time.Hour)},
		{SHA: sha('a'), Subject: "Initial", AuthorName: "Bob", Body: "the parser", Parents: []string{}, CommittedAt: at},
	}
	side := []Commit{{SHA: sha('b'), Subject: "Add feature", AuthorName: "Ada", Parents: []string{sha('a')}, CommittedAt: at.Add(time.Hour)}}
	labels := []Label{{Name: "main", Kind: "branch", SHA: sha('d')}, {Name: "feature", Kind: "branch", SHA: sha('b')}, {Name: "v1", Kind: "tag", SHA: sha('a')}}
	a := historyAPI(t, newHistory(cs, side, labels, "main"))
	for name, tc := range map[string]struct {
		p        oapi.GetHistoryParams
		want     string
		filtered bool
	}{
		"all":           {oapi.GetHistoryParams{}, "dcba", false},
		"branch":        {oapi.GetHistoryParams{Ref: new("feature")}, "ba", false},
		"tag":           {oapi.GetHistoryParams{Ref: new("v1")}, "a", false},
		"author login":  {oapi.GetHistoryParams{Author: new("ADA")}, "db", true},
		"text in body":  {oapi.GetHistoryParams{Q: new("Parser")}, "a", true},
		"sha prefix":    {oapi.GetHistoryParams{Q: new(sha('c')[:7])}, "c", true},
		"no automation": {oapi.GetHistoryParams{Automation: new(false)}, "dba", true},
	} {
		page := getHistory(t, a, tc.p).(oapi.GetHistory200JSONResponse).Body
		var got string
		for _, c := range page.Commits {
			got += c.Sha[:1]
		}
		if got != tc.want || page.Filtered != tc.filtered || page.Total != len(tc.want) || len(page.Labels) != 3 {
			t.Errorf("%s: %q filtered %v total %d labels %d, want %q filtered %v", name, got, page.Filtered, page.Total, len(page.Labels), tc.want, tc.filtered)
		}
	}
	page := getHistory(t, a, oapi.GetHistoryParams{}).(oapi.GetHistory200JSONResponse).Body
	if d := page.Commits[0]; len(d.Labels) != 1 || !d.Labels[0].Default || d.Automated || !page.Commits[1].Automated {
		t.Errorf("labels and automation: %+v", page.Commits[:2])
	}

	next := getHistory(t, a, oapi.GetHistoryParams{Limit: new(1)}).(oapi.GetHistory200JSONResponse).Body.Page.NextCursor
	for name, p := range map[string]oapi.GetHistoryParams{
		"unknown ref":         {Ref: new("nope")},
		"limit":               {Limit: new(201)},
		"cursor of a filter":  {Cursor: next, Author: new("ada")},
		"cursor of no commit": {Cursor: new(encodeAny(historyCursor{F: digest("", "", "", true), S: sha('9')}))},
		"garbage cursor":      {Cursor: new("!!")},
	} {
		if _, ok := getHistory(t, a, p).(oapi.GetHistory400ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("%s: not refused", name)
		}
	}
	if _, ok := getHistory(t, a, oapi.GetHistoryParams{Cursor: next}).(oapi.GetHistory200JSONResponse); !ok {
		t.Error("its own cursor refused")
	}
	if res, _ := a.GetHistory(t.Context(), oapi.GetHistoryRequestObject{Owner: "acme", Name: "nope"}); res == nil {
		t.Error("no answer for an unknown repository")
	} else if _, ok := res.(oapi.GetHistory404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown repository: %T", res)
	}
}
