package commits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

func sha(c byte) string { return strings.Repeat(string(c), 40) }

func TestNormalise(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	in := []CommitInfo{
		{SHA: sha('a'), Message: "older", Date: now.Add(-2 * time.Hour)},
		{SHA: strings.ToUpper(sha('b')), Message: "\nfix: thing\u202e\n\nbody", AuthorName: "Ann", AuthorLogin: "ann",
			HTMLURL: "https://github.com/acme/x/commit/b", Date: now.Add(-time.Hour).Add(300 * time.Millisecond)},
		{SHA: sha('a'), Message: "duplicate", Date: now},
		{SHA: "nothex", Message: "bad sha", Date: now},
		{SHA: sha('c'), Message: "no date"},
		{SHA: sha('d'), Message: strings.Repeat("x", 300), AuthorLogin: "not a login", HTMLURL: "http://insecure", Date: now.Add(72 * time.Hour)},
	}
	firstSeen := now.Add(-30 * time.Minute)
	got := normalise(in, []Commit{{SHA: sha('d'), CommittedAt: firstSeen}}, now)
	if len(got) != 3 {
		t.Fatalf("got %d commits: %+v", len(got), got)
	}
	if d := got[0]; d.SHA != sha('d') || !d.CommittedAt.Equal(firstSeen) || d.AuthorLogin != "" || d.URL != "" ||
		len([]rune(d.Subject)) != maxSubject || !strings.HasSuffix(d.Subject, "…") {
		t.Errorf("future-dated commit = %+v", d)
	}
	if b := got[1]; b.SHA != sha('b') || b.Subject != "fix: thing" || b.AuthorLogin != "ann" || b.CommittedAt.Nanosecond() != 0 {
		t.Errorf("commit b = %+v", b)
	}
	if got[2].SHA != sha('a') || got[2].Subject != "older" {
		t.Errorf("commit a = %+v (the first of duplicate SHAs wins)", got[2])
	}
	if got[2].Parents == nil || len(got[2].Parents) != 0 {
		t.Errorf("root commit parents = %#v, want empty, not nil", got[2].Parents)
	}
}

func TestNormaliseParents(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	many := []string{strings.ToUpper(sha('1')), "nothex", sha('1'), sha('2')}
	for _, c := range "3456789ab" {
		many = append(many, sha(byte(c)))
	}
	got := normalise([]CommitInfo{{SHA: sha('f'), Message: "octopus", Date: now, Parents: many}}, nil, now)
	want := []string{sha('1'), sha('2'), sha('3'), sha('4'), sha('5'), sha('6'), sha('7'), sha('8')}
	if !slices.Equal(got[0].Parents, want) {
		t.Errorf("parents = %v, want the valid ones lower-case, deduplicated, at most %d", got[0].Parents, maxParents)
	}
}

type fetcher struct {
	cs  []CommitInfo
	err error
}

func (f fetcher) Commits(context.Context, string, string, string, int) ([]CommitInfo, bool, error) {
	return f.cs, false, f.err
}

func (f fetcher) Branches(context.Context, string, string) ([]RefInfo, error) { return nil, nil }
func (f fetcher) Tags(context.Context, string, string) ([]RefInfo, error)     { return nil, nil }
func (f fetcher) Stats(context.Context, string, string, []string) (map[string]Stats, error) {
	return nil, nil
}

// counting is a fetcher that answers stats for every SHA it is asked, or fails, and keeps what it was asked.
type counting struct {
	fetcher
	asked *[][]string
	fail  bool
}

func (f counting) Stats(_ context.Context, _, _ string, shas []string) (map[string]Stats, error) {
	*f.asked = append(*f.asked, shas)
	if f.fail {
		return nil, &github.Error{Kind: github.Transient, Code: "graphql", Err: errors.New("502")}
	}
	out := map[string]Stats{}
	for _, s := range shas {
		out[s] = Stats{Additions: 12, Deletions: 3, Files: 2}
	}
	return out, nil
}

// TestRunAsksStats: the job asks the stats of the recent commits it holds without them, keeps them across reads (a SHA is asked
// once), never asks a commit older than the window, and a failed query keeps the history and runs again soon.
func TestRunAsksStats(t *testing.T) {
	now := time.Now()
	cs := []CommitInfo{
		{SHA: sha('a'), Message: "new", Date: now.Add(-time.Hour)},
		{SHA: sha('b'), Message: "old", Date: now.Add(-100 * 24 * time.Hour)},
	}
	var asked [][]string
	job := sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"}
	spec := New(map[string]Fetcher{"gh": counting{fetcher: fetcher{cs: cs}, asked: &asked}}).Spec()
	res, err := spec.Run(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	var p payload
	if err := json.Unmarshal(res.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !slices.Equal(asked[0], []string{sha('a')}) || res.Again != 0 {
		t.Fatalf("asked %v, again %v: the recent commit alone, once", asked, res.Again)
	}
	if s := p.Main[0].Stats; s == nil || *s != (Stats{Additions: 12, Deletions: 3, Files: 2}) || p.Main[1].Stats != nil {
		t.Errorf("stats = %+v, %+v", p.Main[0].Stats, p.Main[1].Stats)
	}
	job.Prev = store.Item{State: res.State, Payload: res.Payload}
	again, err := spec.Run(t.Context(), job)
	if err != nil || len(asked) != 1 || !again.Unchanged {
		t.Errorf("a second read asks again (%v) or changes (%v): %v", asked, again.Unchanged, err)
	}
	failing := New(map[string]Fetcher{"gh": counting{fetcher: fetcher{cs: cs}, asked: &asked, fail: true}}).Spec()
	job.Prev = store.Item{}
	res, err = failing.Run(t.Context(), job)
	if err != nil || res.Again != cutAgain || res.State != snapshot.StateOK {
		t.Errorf("a failed query: %v, again %v, state %v; want the history kept and asked again soon", err, res.Again, res.State)
	}
}

func TestRunStates(t *testing.T) {
	ok := fetcher{cs: []CommitInfo{{SHA: sha('a'), Message: "m", Date: time.Now()}}}
	tests := map[string]struct {
		f       Fetcher
		state   snapshot.State
		commits int
		err     bool
	}{
		"ok":        {f: ok, state: snapshot.StateOK, commits: 1},
		"empty":     {f: fetcher{err: &github.Error{Kind: github.NotFound, Code: "empty", Err: errors.New("409")}}, state: snapshot.StateNoContent},
		"not found": {f: fetcher{err: &github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("404")}}, state: snapshot.StateNotFound},
		"transient": {f: fetcher{err: &github.Error{Kind: github.Transient, Code: "transient", Err: errors.New("502")}}, err: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			spec := New(map[string]Fetcher{"gh": tc.f}).Spec()
			res, err := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"})
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			var p payload
			if res.State != tc.state || json.Unmarshal(res.Payload, &p) != nil || len(p.Main) != tc.commits {
				t.Errorf("result = %+v", res)
			}
			again, _ := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x",
				Prev: store.Item{State: res.State, Payload: res.Payload}})
			if !again.Unchanged {
				t.Error("the same commits again are not Unchanged")
			}
		})
	}
}

func TestPublish(t *testing.T) {
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	r := snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}
	b, _ := json.Marshal(payload{Schema: payloadSchema, Main: []Commit{{SHA: sha('a'), CommittedAt: time.Now()}}})
	p.Update(func(bd *snapshot.Builder) {
		bd.ReplaceSource("gh", []*snapshot.RepoEntry{{Repo: r}}, nil, snapshot.SourceStatus{ID: "gh"})
		publish(bd, store.Item{Repo: "acme/x", State: snapshot.StateOK, Payload: b}, snapshot.Freshness{State: snapshot.StateOK})
		publish(bd, store.Item{Repo: "acme/gone", Payload: b}, snapshot.Freshness{}) // a removed repository: ignored
	})
	p.Flush()
	s := p.Current()
	if cs, _ := Of(s.Repos["acme/x"]); len(cs) != 1 || len(s.Repos) != 1 {
		t.Errorf("published: %+v", s.Repos)
	}
}

func TestLastAuthored(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	bot := Commit{AuthorName: "renovate[bot]", Subject: "chore(deps): update", CommittedAt: day(9)}
	release := Commit{AuthorName: "Jenkins", Subject: "[maven-release-plugin] prepare release v1", CommittedAt: day(8)}
	person := Commit{AuthorName: "Ada", AuthorLogin: "ada", Subject: "Fix the parser", CommittedAt: day(7)}
	app := Commit{AuthorLogin: "github-actions[bot]", CommittedAt: day(6)}
	for name, tc := range map[string]struct {
		cs     []Commit
		at     time.Time
		before bool
	}{
		"a person after automation": {[]Commit{bot, release, person, app}, day(7), false},
		"only automation":           {[]Commit{bot, release, app}, day(6), true},
		"none":                      {nil, time.Time{}, false},
	} {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "x"}}
		facet.Set(e, newHistory(tc.cs, nil, nil, "main"), snapshot.Freshness{State: snapshot.StateOK})
		p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
		p.Update(func(b *snapshot.Builder) {
			b.ReplaceSource("gh", []*snapshot.RepoEntry{e}, nil, snapshot.SourceStatus{ID: "gh"})
		})
		p.Flush()
		if at, before := LastAuthored(p.Current(), "acme/x", WorkTeam); !at.Equal(tc.at) || before != tc.before {
			t.Errorf("%s: %v %v, want %v %v", name, at, before, tc.at, tc.before)
		}
	}
}

// TestSweepsAndActivity: one subject in more than 10 repositories on a day is a sweep, counted apart from people and automation, and
// a repository whose newest commits are the sweep and a bot's still shows the person's commit before them.
func TestSweepsAndActivity(t *testing.T) {
	now := time.Now().UTC()
	ago := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	n := 0
	commit := func(author, subject string, at time.Time) Commit {
		n++
		return Commit{SHA: sha(byte(n)), AuthorName: author, AuthorLogin: strings.ToLower(author), Subject: subject, CommittedAt: at}
	}
	p := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), Feeds())
	var entries []*snapshot.RepoEntry
	var members []snapshot.RepoKey
	for i := range 11 {
		e := &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: int64(i + 1)}, Owner: "acme", Name: fmt.Sprintf("r%d", i)}}
		cs := []Commit{commit("Sweeper", "chore: repair the manifest", ago(2))}
		if i == 0 {
			cs = append([]Commit{{SHA: sha(200), AuthorName: "renovate[bot]", Subject: "chore(deps): update", CommittedAt: ago(1)}}, cs...)
			assisted := commit("Ada", "Add a test", ago(20))
			assisted.Body = "Covers the empty case.\n\nCo-authored-by: Claude Opus 5 (1M context) <noreply@anthropic.com>"
			cs = append(cs, commit("Ada", "Fix the parser", ago(3)), assisted, commit("Bo", "Old work", ago(60)))
		}
		facet.Set(e, newHistory(cs, nil, nil, "main"), snapshot.Freshness{State: snapshot.StateOK})
		entries = append(entries, e)
		members = append(members, e.Repo.Key())
	}
	p.Update(func(b *snapshot.Builder) {
		b.ReplaceSource("gh", entries, nil, snapshot.SourceStatus{ID: "gh"})
		b.SetGroups([]snapshot.Group{{ID: "g", Name: "G", Members: members[:1]}})
	})
	p.Flush()
	s := p.Current()
	if at, before := LastAuthored(s, "acme/r0", WorkTeam); !at.Equal(ago(3)) || before {
		t.Errorf("last authored = %v %v, want the person's commit 3 days ago", at, before)
	}
	// Authorship counts people's commits only, without the bot's update and the sweep: Ada twice (one with an AI) and Bo once.
	if people, top := Authorship(s, s.Repos["acme/r0"]); people != 3 || top != 2 {
		t.Errorf("authorship = %d people's commits, %d by the busiest; want 3 and 2", people, top)
	}
	if people, top := Authorship(s, s.Repos["acme/r5"]); people != 0 || top != 0 {
		t.Errorf("a repository holding only the sweep: %d, %d", people, top)
	}
	if at, _ := LastAuthored(s, "acme/r5", WorkTeam); !at.Equal(ago(2)) {
		t.Errorf("a repository holding only the sweep: %v, want the oldest read commit", at)
	}
	// Without a lens the dashboard counts every kind: the bot's update a day ago is the latest commit.
	every := oapi.Dashboard{Groups: []oapi.DashboardGroup{{Id: "g", Members: []oapi.DashboardMember{{Address: "acme/r0"}}}}}
	Dashboard(s, &every)
	if len(every.Latest) == 0 || every.Latest[0].Kind != oapi.CommitKindAutomation || !every.Groups[0].Members[0].LastAuthoredAt.Equal(ago(1)) {
		t.Errorf("the default lens: latest %+v, member %+v; want every kind", every.Latest, every.Groups[0].Members[0])
	}
	d := oapi.Dashboard{Work: oapi.Work{oapi.WorkKindHand, oapi.WorkKindAi},
		Groups: []oapi.DashboardGroup{{Id: "g", Members: []oapi.DashboardMember{{Address: "acme/r0"}}}}}
	Dashboard(s, &d)
	g := d.Groups[0].Activity
	if g.Hand != 1 || g.Ai != 1 || g.Automation != 1 || g.Sweeps != 1 || g.AuthorCount != 1 || len(g.Authors) != 1 || g.Authors[0].Name != "Ada" {
		t.Errorf("group activity = %+v", g)
	}
	if g.LastAuthoredAt == nil || !g.LastAuthoredAt.Equal(ago(3)) || len(g.Recent) != 1 || g.Recent[0].Repo != "acme/r0" {
		t.Errorf("group last change = %v, recent %+v", g.LastAuthoredAt, g.Recent)
	}
	if g.WeeklyHand[11] != 1 || g.WeeklyHand[9] != 0 || len(g.WeeklyHand) != 12 {
		t.Errorf("weekly by hand = %v", g.WeeklyHand)
	}
	if g.WeeklyAutomation[11] != 2 || g.WeeklyAutomation[9] != 0 {
		t.Errorf("weekly by automation and sweeps = %v", g.WeeklyAutomation)
	}
	if g.Ai != 1 || !slices.Equal(g.Assistants, []string{"Claude"}) || g.WeeklyAi[9] != 1 || g.WeeklyAi[11] != 0 {
		t.Errorf("the commits with an AI: %d %v %v", g.Ai, g.Assistants, g.WeeklyAi)
	}
	if d.Activity.Sweeps != 11 || d.Activity.Hand+d.Activity.Ai != 2 {
		t.Errorf("fleet activity = %+v", d.Activity)
	}
	if len(d.Latest) != 3 || d.Latest[0].Subject != "Fix the parser" {
		t.Errorf("latest = %+v, want people's commits only", d.Latest)
	}
	if m := d.Groups[0].Members[0]; m.LastAuthoredAt == nil || !m.LastAuthoredAt.Equal(ago(3)) {
		t.Errorf("member = %+v", m)
	}
	// The lens: with AI, the work that counts is the assisted commit 20 days ago; by hand, the fix 3 days ago; automation, the bot's
	// update a day ago and the sweep, still naming the people beside it; every kind, the bot's update. The counts give every kind.
	// acme/r5 holds only the sweep: by hand it has no last change at all, not its oldest commit.
	for _, tc := range []struct {
		work    oapi.Work
		last    time.Time
		authors int
		latest  int
	}{
		{oapi.Work{oapi.WorkKindAi}, ago(20), 1, 1},
		{oapi.Work{oapi.WorkKindHand}, ago(3), 1, 2},
		{oapi.Work{oapi.WorkKindAutomation}, ago(1), 1, 10},
		{oapi.Work{oapi.WorkKindHand, oapi.WorkKindAi, oapi.WorkKindAutomation}, ago(1), 1, 10},
	} {
		d := oapi.Dashboard{Work: tc.work, Groups: []oapi.DashboardGroup{{Id: "g", Members: []oapi.DashboardMember{{Address: "acme/r0"}, {Address: "acme/r5"}}}}}
		Dashboard(s, &d)
		g := d.Groups[0].Activity
		if g.LastAuthoredAt == nil || !g.LastAuthoredAt.Equal(tc.last) || g.AuthorCount != tc.authors || g.Hand != 1 || g.Ai != 1 ||
			g.Automation != 1 || len(d.Latest) != tc.latest {
			t.Errorf("%v: last %v, %d authors, %d by hand, %d with AI, %d latest", tc.work, g.LastAuthoredAt, g.AuthorCount, g.Hand, g.Ai, len(d.Latest))
		}
		if m := d.Groups[0].Members[0]; m.LastAuthoredAt == nil || !m.LastAuthoredAt.Equal(tc.last) {
			t.Errorf("%v: member %+v", tc.work, m)
		}
		if m := d.Groups[0].Members[1]; len(tc.work) == 1 && tc.work[0] == oapi.WorkKindHand && m.LastAuthoredAt != nil {
			t.Errorf("%v: a member without such work has its last change at %v", tc.work, m.LastAuthoredAt)
		}
	}
}

// TestGraphOrder: on random histories (merges, octopus merges, criss-cross merges, clocks that disagree) every commit comes before
// its parents and every commit is listed once.
func TestGraphOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for round := range 200 {
		n := 2 + rng.IntN(60)
		cs := make([]Commit, n) // cs[i] is younger than cs[j] for i < j in the DAG; parents point to higher indexes
		for i := range cs {
			cs[i].SHA = fmt.Sprintf("%040x", round*1000+i)
			// Dates mostly follow the DAG, a fifth of them skewed by up to two days.
			cs[i].CommittedAt = base.Add(-time.Duration(i) * time.Hour)
			if rng.IntN(5) == 0 {
				cs[i].CommittedAt = cs[i].CommittedAt.Add(time.Duration(rng.IntN(96)-48) * time.Hour)
			}
		}
		for i := range n - 1 {
			parents := 1
			if rng.IntN(4) == 0 {
				parents = 2 + rng.IntN(3)
			}
			for range parents {
				p := cs[i+1+rng.IntN(n-i-1)].SHA
				if !slices.Contains(cs[i].Parents, p) {
					cs[i].Parents = append(cs[i].Parents, p)
				}
			}
		}
		split := rng.IntN(n)
		order := graphOrder(cs[:split], cs[split:])
		pos := map[string]int{}
		for i, c := range order {
			pos[c.SHA] = i
		}
		if len(order) != n || len(pos) != n {
			t.Fatalf("round %d: %d of %d commits ordered", round, len(order), n)
		}
		for _, c := range order {
			for _, p := range c.Parents {
				if pos[p] <= pos[c.SHA] {
					t.Fatalf("round %d: parent %s at %d before its child at %d", round, p[34:], pos[p], pos[c.SHA])
				}
			}
		}
	}
}

// paged is a Fetcher of a default branch in pages and other branches, each able to fail.
type paged struct {
	main        [][]CommitInfo // page n is main[n-1]
	mainErr     map[int]error  // by page
	branches    []RefInfo
	branchesErr error
	tagsErr     error
	side        map[string][]CommitInfo // by branch name
	sideErr     map[string]error
}

func (f paged) Commits(_ context.Context, _, _, ref string, page int) ([]CommitInfo, bool, error) {
	if ref != "" {
		if err := f.sideErr[ref]; err != nil {
			return nil, false, err
		}
		return f.side[ref], false, nil
	}
	if err := f.mainErr[page]; err != nil {
		return nil, false, err
	}
	if page > len(f.main) {
		return nil, false, nil
	}
	return f.main[page-1], page < len(f.main), nil
}

func (f paged) Branches(context.Context, string, string) ([]RefInfo, error) {
	return f.branches, f.branchesErr
}
func (f paged) Tags(context.Context, string, string) ([]RefInfo, error) { return nil, f.tagsErr }
func (f paged) Stats(context.Context, string, string, []string) (map[string]Stats, error) {
	return nil, nil
}

func TestRunKeepsWhatAFailureCut(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	commit := func(c byte, parent byte, age int) CommitInfo {
		ci := CommitInfo{SHA: sha(c), Message: "change " + string(c), AuthorName: "Ann", Date: at.Add(-time.Duration(age) * time.Minute)}
		if parent != 0 {
			ci.Parents = []string{sha(parent)}
		}
		return ci
	}
	pages := [][]CommitInfo{{commit('a', 'b', 0), commit('b', 'c', 1)}, {commit('c', 'd', 2), commit('d', 0, 3)}}
	malformed := &github.Error{Kind: github.Permanent, Code: "malformed", Err: errors.New("bad page")}
	rate := &github.Error{Kind: github.RateLimited, Code: "rate_limited", Err: errors.New("403")}
	feature := []RefInfo{{Name: "feature", SHA: sha('f')}, {Name: "broken", SHA: sha('e')}}
	side := map[string][]CommitInfo{"feature": {commit('f', 'a', -5)}}
	tests := map[string]struct {
		f           paged
		main, sides int
		cut, err    bool
	}{
		"whole":                {f: paged{main: pages}, main: 4},
		"page two fails":       {f: paged{main: pages, mainErr: map[int]error{2: malformed}}, main: 2, cut: true},
		"page one fails":       {f: paged{main: pages, mainErr: map[int]error{1: malformed}}, err: true},
		"rate limit stops":     {f: paged{main: pages, mainErr: map[int]error{2: rate}}, err: true},
		"a branch fails":       {f: paged{main: pages, branches: feature, side: side, sideErr: map[string]error{"broken": malformed}}, main: 4, sides: 1, cut: true},
		"the branches fail":    {f: paged{main: pages, branchesErr: malformed}, main: 4, cut: true},
		"the tags fail":        {f: paged{main: pages, branches: feature[:1], side: side, tagsErr: malformed}, main: 4, sides: 1, cut: true},
		"a branch rate limits": {f: paged{main: pages, branches: feature, side: side, sideErr: map[string]error{"broken": rate}}, err: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := New(map[string]Fetcher{"gh": tc.f}).Spec().Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"})
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want an error %v", err, tc.err)
			}
			if err != nil {
				return
			}
			var p payload
			if err := json.Unmarshal(res.Payload, &p); err != nil {
				t.Fatal(err)
			}
			if len(p.Main) != tc.main || len(p.Side) != tc.sides {
				t.Errorf("main %d, side %d; want %d, %d", len(p.Main), len(p.Side), tc.main, tc.sides)
			}
			if cut := res.Again > 0; cut != tc.cut || cut && !p.Full.IsZero() {
				t.Errorf("again %v, full %v; want cut %v and the next read in full", res.Again, p.Full, tc.cut)
			}
		})
	}
}

func TestRunMergesACutReadWithTheHeldHistory(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	held := []CommitInfo{{SHA: sha('c'), Message: "old", AuthorName: "Ann", Date: at.Add(-time.Hour), Parents: []string{sha('d')}},
		{SHA: sha('d'), Message: "older", AuthorName: "Ann", Date: at.Add(-2 * time.Hour)}}
	spec := New(map[string]Fetcher{"gh": paged{main: [][]CommitInfo{held}}}).Spec()
	first, err := spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x"})
	if err != nil {
		t.Fatal(err)
	}
	pushed := []CommitInfo{{SHA: sha('a'), Message: "new", AuthorName: "Ann", Date: at, Parents: []string{sha('b')}},
		{SHA: sha('b'), Message: "newer", AuthorName: "Ann", Date: at.Add(-time.Minute), Parents: []string{sha('c')}}}
	cut := paged{main: [][]CommitInfo{pushed, held}, mainErr: map[int]error{2: &github.Error{Kind: github.Transient, Code: "transient", Err: errors.New("502")}}}
	// A full read (the daily one) cut after its first page keeps what it held beyond it.
	var p payload
	_ = json.Unmarshal(first.Payload, &p)
	p.Full = time.Time{}
	prev, _ := json.Marshal(p)
	res, err := New(map[string]Fetcher{"gh": cut}).Spec().Run(t.Context(), sync.Job{Key: Kind + "/acme/x", Source: "gh", Repo: "acme/x",
		Prev: store.Item{State: snapshot.StateOK, Payload: prev}})
	if err != nil {
		t.Fatal(err)
	}
	var got payload
	_ = json.Unmarshal(res.Payload, &got)
	shas := []string{}
	for _, c := range got.Main {
		shas = append(shas, c.SHA[:1])
	}
	if !slices.Equal(shas, []string{"a", "b", "c", "d"}) || res.Again == 0 {
		t.Errorf("main = %v, again %v; want a b c d and a read again soon", shas, res.Again)
	}
}
