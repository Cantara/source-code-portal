package commits

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
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

// walk reads every page and returns the SHAs in order, checking the page descriptions.
func walk(t *testing.T, a API, p oapi.GetHistoryParams) []string {
	t.Helper()
	var out []string
	for n := 1; ; n++ {
		p.Page = new(n)
		page, ok := getHistory(t, a, p).(oapi.GetHistory200JSONResponse)
		if !ok {
			t.Fatalf("page %d: %+v", n, page)
		}
		pg := page.Body.Page
		if pg.Number != n || pg.Pages != max(1, (pg.Total+pg.Size-1)/pg.Size) || (n < pg.Pages && len(page.Body.Commits) != pg.Size) {
			t.Fatalf("page %d: %+v with %d commits", n, pg, len(page.Body.Commits))
		}
		for _, c := range page.Body.Commits {
			out = append(out, c.Sha)
		}
		if n >= pg.Pages {
			if len(out) != pg.Total {
				t.Fatalf("%d commits over %d pages, total %d", len(out), pg.Pages, pg.Total)
			}
			return out
		}
	}
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

// TestHistoryGraph: every page carries its rows of the graph laid out over the whole history, the same rows whatever the page
// size; a branch's history is laid out on its own; a filtered page has none.
func TestHistoryGraph(t *testing.T) {
	cs := randomDAG(rand.New(rand.NewPCG(5, 6)), 0, 80)
	h := newHistory(cs, nil, nil, "main")
	a := historyAPI(t, h)
	rows := func(limit int) []oapi.GraphRow {
		var out []oapi.GraphRow
		for n := 1; n <= (80+limit-1)/limit; n++ {
			page := getHistory(t, a, oapi.GetHistoryParams{Limit: new(limit), Page: new(n)}).(oapi.GetHistory200JSONResponse).Body
			if page.Lanes < 1 || page.Lanes > maxLanes {
				t.Fatalf("lanes = %d", page.Lanes)
			}
			for _, c := range page.Commits {
				out = append(out, *c.Graph)
			}
		}
		return out
	}
	whole := rows(200)
	for _, limit := range []int{1, 7, 30} {
		if got := rows(limit); !reflect.DeepEqual(got, whole) {
			t.Fatalf("rows paged by %d differ from the whole history's", limit)
		}
	}
	if !reflect.DeepEqual(whole[0], *rowOf(h.Rows[0])) {
		t.Errorf("first row %+v, want %+v", whole[0], h.Rows[0])
	}
	page := getHistory(t, a, oapi.GetHistoryParams{Automation: new(false)}).(oapi.GetHistory200JSONResponse).Body
	if page.Lanes != 0 || page.Commits[0].Graph != nil {
		t.Errorf("a filtered page draws a graph: lanes %d", page.Lanes)
	}
}

// TestHistoryAt: at answers the page holding the commit, by SHA or prefix, and names it; an unknown one leaves the page asked for.
func TestHistoryAt(t *testing.T) {
	cs := randomDAG(rand.New(rand.NewPCG(9, 9)), 0, 50)
	a := historyAPI(t, newHistory(cs, nil, nil, "main"))
	order := walk(t, a, oapi.GetHistoryParams{Limit: new(200)})
	for _, i := range []int{0, 9, 10, 49} {
		for _, at := range []string{order[i], strings.ToUpper(order[i])} {
			page := getHistory(t, a, oapi.GetHistoryParams{Limit: new(10), At: new(at), Page: new(3)}).(oapi.GetHistory200JSONResponse).Body
			if page.Page.Number != i/10+1 || page.At == nil || *page.At != order[i] || page.Commits[i%10].Sha != order[i] {
				t.Errorf("at %s (commit %d): page %d, at %v", at, i, page.Page.Number, page.At)
			}
		}
	}
	page := getHistory(t, a, oapi.GetHistoryParams{Limit: new(10), At: new("ffffffff"), Page: new(2)}).(oapi.GetHistory200JSONResponse).Body
	if page.Page.Number != 2 || page.At != nil {
		t.Errorf("an unknown commit: page %d, at %v", page.Page.Number, page.At)
	}
}

func TestHistoryFilters(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cs := []Commit{
		{SHA: sha('d'), Subject: "Merge feature", AuthorName: "Ada", AuthorLogin: "ada", Parents: []string{sha('c'), sha('b')}, CommittedAt: at.Add(3 * time.Hour)},
		{SHA: sha('c'), Subject: "chore(deps): update", AuthorName: "renovate[bot]", Parents: []string{sha('a')}, CommittedAt: at.Add(2 * time.Hour)},
		{SHA: sha('a'), Subject: "Initial", AuthorName: "Bob", Body: "the parser", Parents: []string{sha('0')}, CommittedAt: at},
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
		if got != tc.want || page.Filtered != tc.filtered || page.Page.Total != len(tc.want) || len(page.Labels) != 3 ||
			(page.Lanes == 0) != tc.filtered {
			t.Errorf("%s: %q filtered %v total %d labels %d lanes %d, want %q filtered %v", name, got, page.Filtered, page.Page.Total,
				len(page.Labels), page.Lanes, tc.want, tc.filtered)
		}
	}
	page := getHistory(t, a, oapi.GetHistoryParams{}).(oapi.GetHistory200JSONResponse).Body
	if d := page.Commits[0]; len(d.Labels) != 1 || !d.Labels[0].Default || d.Automated || !page.Commits[1].Automated || len(d.Beyond) != 0 {
		t.Errorf("labels and automation: %+v", page.Commits[:2])
	}
	if a := page.Commits[3]; len(a.Beyond) != 1 || a.Beyond[0] != sha('0') {
		t.Errorf("a parent older than the history: %+v", a.Beyond)
	}

	branch := getHistory(t, a, oapi.GetHistoryParams{Ref: new("feature")}).(oapi.GetHistory200JSONResponse).Body
	if branch.Commits[0].Graph == nil || branch.Commits[0].Graph.Lane != 0 || branch.Lanes != 1 {
		t.Errorf("a branch's own graph: %+v, lanes %d", branch.Commits[0].Graph, branch.Lanes)
	}
	for name, p := range map[string]oapi.GetHistoryParams{
		"unknown ref":  {Ref: new("nope")},
		"limit":        {Limit: new(201)},
		"page":         {Page: new(0)},
		"at too short": {At: new("abc")},
		"at not hex":   {At: new("xyzxyz")},
	} {
		if _, ok := getHistory(t, a, p).(oapi.GetHistory400ApplicationProblemPlusJSONResponse); !ok {
			t.Errorf("%s: not refused", name)
		}
	}
	if res, _ := a.GetHistory(t.Context(), oapi.GetHistoryRequestObject{Owner: "acme", Name: "nope"}); res == nil {
		t.Error("no answer for an unknown repository")
	} else if _, ok := res.(oapi.GetHistory404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown repository: %T", res)
	}
}
