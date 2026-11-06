package insights

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// fleet is six repositories: core and web (group tools; core ships often, depends on nothing, is used by web, api and old), api (Go,
// one author), old (retired, still used by web), quiet (nothing for two years) and secret (private).
func fleet(t *testing.T) (*snapshot.Snapshot, API) {
	t.Helper()
	now := time.Now().UTC()
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	facts := map[snapshot.RepoKey]Facts{
		"acme/core": {Stable: []time.Time{ago(3), ago(13), ago(23), ago(43), ago(63), ago(400)}, LatestTag: "v6", Language: "Go", BuildTool: "Go modules",
			Runtimes: [][2]string{{"go", "1.27"}}, UsedBy: []snapshot.RepoKey{"acme/web", "acme/api", "acme/old"}, People: 40, Top: 20, LastChange: ago(2)},
		"acme/web": {Stable: []time.Time{ago(40)}, LatestTag: "v1", Language: "TypeScript", BuildTool: "pnpm",
			Runtimes: [][2]string{{"node", ">=18"}, {"go", "1.21"}}, People: 30, Top: 10, LastChange: ago(40)},
		"acme/api":    {Language: "Go", BuildTool: "Go modules", Runtimes: [][2]string{{"go", "1.23"}}, People: 50, Top: 48, LastChange: ago(100)},
		"acme/old":    {Language: "Java", BuildTool: "Maven", UsedBy: []snapshot.RepoKey{"acme/web"}, People: 10, Top: 10, LastChange: ago(900)},
		"acme/quiet":  {People: 25, Top: 22, LastChange: ago(800)},
		"acme/secret": {Language: "Go", People: 100, Top: 100, LastChange: ago(1)},
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	entry := func(id int64, name string, private bool) *snapshot.RepoEntry {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: "public"}}
		if private {
			e.Repo.Visibility = "private"
		}
		return e
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", []*snapshot.RepoEntry{entry(1, "core", false), entry(2, "web", false), entry(3, "api", false), entry(4, "old", false),
			entry(5, "quiet", false), entry(6, "secret", true)}, nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "tools", Name: "Tools", Members: []snapshot.RepoKey{"acme/core", "acme/web"}},
			{ID: "rest", Name: "Rest", Members: []snapshot.RepoKey{"acme/api", "acme/old", "acme/quiet", "acme/secret"}}})
	})
	p.Flush()
	api := API{
		Facts: func(_ *snapshot.Snapshot, e *snapshot.RepoEntry) Facts { return facts[e.Repo.Key()] },
		Retired: func(e *snapshot.RepoEntry) oapi.AlumniReason {
			if e.Repo.Name == "old" {
				return oapi.AlumniReasonDeprecated
			}
			return ""
		},
		Fresh: func(*snapshot.RepoEntry) snapshot.Freshness { return snapshot.Freshness{State: snapshot.StateOK} },
	}
	return p.Current(), api
}

func get(t *testing.T, s *snapshot.Snapshot, api API, group, repo *string) oapi.Insights {
	t.Helper()
	api.Current = func(context.Context) *snapshot.Snapshot { return s }
	res, err := api.GetInsights(t.Context(), oapi.GetInsightsRequestObject{Params: oapi.GetInsightsParams{Group: group, Repo: repo}})
	ok, isOK := res.(oapi.GetInsights200JSONResponse)
	if err != nil || !isOK {
		t.Fatalf("insights: %T %v", res, err)
	}
	return ok.Body
}

func TestInsights(t *testing.T) {
	s, api := fleet(t)
	in := get(t, s, api, nil, nil)
	if in.Repos != 5 {
		t.Errorf("repos = %d, want 5: the retired one is left out", in.Repos)
	}

	// Cadence: 6 stable releases of core, 1 of web; the one 400 days ago is outside 12 months.
	total := 0
	for _, m := range in.Cadence.Months {
		total += m.Releases
	}
	if len(in.Cadence.Months) != Months || total != 6 {
		t.Errorf("months = %d, releases %d, want %d months and 6 releases", len(in.Cadence.Months), total, Months)
	}
	var tools *oapi.InsightProduct
	for i := range in.Cadence.Products {
		if in.Cadence.Products[i].Group == "tools" {
			tools = &in.Cadence.Products[i]
		}
	}
	// The product's last five stable releases: 3, 13, 23, 40, 43 days ago: gaps 10, 10, 17, 3 -> sorted 3 10 10 17 -> median 10.
	if tools == nil || tools.Releases != 7 || tools.MedianDays == nil || *tools.MedianDays != 10 {
		t.Errorf("tools = %+v", tools)
	}
	for _, p := range in.Cadence.Products {
		if p.Group == "rest" && (p.Releases != 0 || p.MedianDays != nil || p.Last != nil) {
			t.Errorf("a product that never released says so: %+v", p)
		}
	}

	// Technology.
	langs := map[string]int{}
	for _, l := range in.Technology.Languages {
		langs[l.Name] = l.Repos
	}
	if langs["Go"] != 3 || langs["TypeScript"] != 1 || in.Technology.Unknown != 1 {
		t.Errorf("languages = %v unknown %d", langs, in.Technology.Unknown)
	}
	if in.Technology.Languages[0].Name != "Go" || in.Technology.Languages[0].Share != 0.6 {
		t.Errorf("languages[0] = %+v", in.Technology.Languages[0])
	}
	runtimes := map[string]oapi.InsightRuntime{}
	for _, r := range in.Technology.Runtimes {
		runtimes[r.Name] = r
	}
	if g := runtimes["go"]; g.Repos != 3 || g.Oldest != "1.21" || g.Newest != "1.27" {
		t.Errorf("go = %+v", g)
	}
	if n := runtimes["node"]; n.Repos != 1 || n.Oldest != ">=18" {
		t.Errorf("node = %+v", n)
	}

	// Libraries: core is used by web and api (old is retired, not counted); old is used by web and is retired and listed.
	if len(in.Libraries) != 2 || in.Libraries[0].Repo != "acme/core" || in.Libraries[0].Dependants != 2 || in.Libraries[0].Release == nil ||
		*in.Libraries[0].Release != "v6" || in.Libraries[1].Repo != "acme/old" || in.Libraries[1].Retired == nil || *in.Libraries[1].Retired != oapi.AlumniReasonDeprecated {
		t.Errorf("libraries = %+v", in.Libraries)
	}

	// Single author: api 48/50 and quiet 22/25 (88%: no); old is retired; web 10/30 no; core 20/40 no; secret 100/100 is in the view here.
	var single []string
	for _, r := range in.SingleAuthor {
		single = append(single, r.Repo)
	}
	if strings.Join(single, " ") != "acme/secret acme/api" {
		t.Errorf("single author = %v", single)
	}

	// Ageing: core 2 days active, web 40 and api 100 are recent or quiet, quiet 800 days is dormant, secret active.
	a := in.Ageing
	if a.Active != 2 || a.Dormant != 1 || a.Active+a.Recent+a.Quiet+a.Dormant != 5 || len(a.DormantRepos) != 1 || a.DormantRepos[0].Repo != "acme/quiet" {
		t.Errorf("ageing = %+v", a)
	}

	// A group and a repository narrow the scope; an unknown one is not found.
	g := "tools"
	if one := get(t, s, api, &g, nil); one.Repos != 2 || len(one.SingleAuthor) != 0 {
		t.Errorf("group tools = %+v", one)
	}
	r := "acme/api"
	if one := get(t, s, api, nil, &r); one.Repos != 1 || len(one.SingleAuthor) != 1 {
		t.Errorf("repo api = %+v", one)
	}
	api.Current = func(context.Context) *snapshot.Snapshot { return s }
	nope := "nope"
	if res, _ := api.GetInsights(t.Context(), oapi.GetInsightsRequestObject{Params: oapi.GetInsightsParams{Group: &nope}}); res == nil {
		t.Error("no answer for an unknown group")
	} else if _, ok := res.(oapi.GetInsights404ApplicationProblemPlusJSONResponse); !ok {
		t.Errorf("unknown group: %T, want 404", res)
	}
}

// TestInsightsPublicView: the public view of the same fleet counts and lists nothing of the private repository.
func TestInsightsPublicView(t *testing.T) {
	s, api := fleet(t)
	in := get(t, s.Public(), api, nil, nil)
	b, _ := json.Marshal(in)
	if in.Repos != 4 || strings.Contains(string(b), "secret") {
		t.Errorf("public insights count %d repos and carry a private one: %s", in.Repos, b)
	}
}

// TestInsightsEmpty: a scope with nothing in it answers lists, never null (the page reads their lengths).
func TestInsightsEmpty(t *testing.T) {
	s, api := fleet(t)
	r := "acme/old" // retired: nothing live in scope
	b, _ := json.Marshal(get(t, s, api, nil, &r))
	for _, field := range []string{"products", "languages", "buildTools", "runtimes", "libraries", "singleAuthor", "dormantRepos"} {
		if strings.Contains(string(b), `"`+field+`":null`) {
			t.Errorf("%s is null: %s", field, b)
		}
	}
	var in oapi.Insights
	if err := json.Unmarshal(b, &in); err != nil || in.Repos != 0 || len(in.Cadence.Months) != Months {
		t.Errorf("empty = %+v %v", in, err)
	}
}

func TestVersion(t *testing.T) {
	for in, want := range map[string][]int{"1.21": {1, 21}, ">=18": {18}, "17": {17}, "go1.22.3": {1, 22, 3}} {
		if got, ok := version(in); !ok || !slices.Equal(got, want) {
			t.Errorf("%q = %v %v, want %v", in, got, ok, want)
		}
	}
	if _, ok := version("latest"); ok {
		t.Error("a word is no version")
	}
}
