package issues

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/sync"
)

type rig struct {
	t    *testing.T
	srv  *githubtest.Server
	id   int64
	repo snapshot.Repo
	spec sync.Spec
}

func newRig(t *testing.T) *rig {
	t.Helper()
	srv := githubtest.New(t)
	r := &rig{t: t, srv: srv, id: srv.AddRepo("acme", gh.Repository{Name: new("one")})}
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r.spec = New(map[string]Fetcher{"gh": FromGitHub(c.Source("gh"))}, Options{
		Repo: func(snapshot.RepoKey) (snapshot.Repo, bool) { return r.repo, true },
	}).Spec()
	return r
}

func (r *rig) run(prev store.Item) (sync.Result, *Set, store.Item) {
	r.t.Helper()
	res, err := r.spec.Run(r.t.Context(), sync.Job{Key: Kind + "/acme/one", Source: "gh", Repo: "acme/one", Prev: prev})
	if err != nil {
		r.t.Fatalf("run: %v", err)
	}
	payload := res.Payload
	if res.Unchanged {
		payload = prev.Payload
	}
	var s Set
	if err := json.Unmarshal(payload, &s); err != nil {
		r.t.Fatalf("payload: %v", err)
	}
	return res, &s, store.Item{State: snapshot.StateOK, Payload: payload}
}

// calls counts the fake's requests by status.
func (r *rig) calls() map[string]int {
	n := map[string]int{}
	for _, l := range r.srv.Ledger() {
		n[strings.Fields(l)[2]]++
	}
	return n
}

func TestJob(t *testing.T) {
	r := newRig(t)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	bug := githubtest.Issue{Number: 4, Title: "Crash on \u202estart", Author: "ann", Created: at, Updated: at, Labels: []string{"bug"},
		Comments: 2}
	fresh := githubtest.Issue{Number: 5, Title: "What is this?", Author: "bo", Created: at, Updated: at.Add(time.Hour)}
	taken := githubtest.Issue{Number: 6, Title: "Add a flag", Author: "cy", Created: at, Updated: at.Add(-time.Hour),
		Assignees: []string{"dee", "ed", "fi", "gu"}, Milestone: "1.0", Type: "Feature"}
	dash := githubtest.Issue{Number: 1, Title: "Dependency Dashboard", Author: "renovate[bot]", Bot: true, Created: at, Updated: at}
	r.srv.SetIssues(r.id, bug, fresh, taken, dash)
	r.srv.SetPulls(r.id, githubtest.Pull{Number: 7, Title: "A pull request", Author: "ann", Created: at, Updated: at.Add(2 * time.Hour),
		Base: "main", Head: "x", HeadSHA: strings.Repeat("a", 40)})

	_, s, prev := r.run(store.Item{})
	if s.Total != 4 || !s.Exact || len(s.Issues) != 4 || s.Issues[0].Number != 5 {
		t.Fatalf("first run: %+v", s)
	}
	byNumber := map[int]*Issue{}
	for i := range s.Issues {
		byNumber[s.Issues[i].Number] = &s.Issues[i]
	}
	if byNumber[7] != nil {
		t.Error("the pull request in the listing was kept")
	}
	if b := byNumber[4]; b.Title != "Crash on start" || b.Comments != 2 || StateOf(b) != StateUnassigned || KindOf(b) != KindBug ||
		b.Labels[0].Color != "d73a4a" {
		t.Errorf("the bug: %+v", b)
	}
	if f := byNumber[5]; StateOf(f) != StateTriage || KindOf(f) != KindOther || f.Bot {
		t.Errorf("the new one: %+v", f)
	}
	if tk := byNumber[6]; StateOf(tk) != StateAssigned || KindOf(tk) != KindFeature || tk.Assigned != 4 || len(tk.Assignees) != 3 ||
		tk.Milestone != "1.0" || tk.Type != "Feature" {
		t.Errorf("the taken one: %+v", tk)
	}
	if !byNumber[1].Bot {
		t.Error("Renovate's dashboard is not automation")
	}

	// Nothing changed: one 304 and nothing else.
	before := r.calls()
	res, _, _ := r.run(prev)
	after := r.calls()
	if !res.Unchanged || after["304"]-before["304"] != 1 || after["200"] != before["200"] {
		t.Errorf("an unchanged repository: unchanged %v, calls %v then %v", res.Unchanged, before, after)
	}

	// An archived repository is read once.
	r.repo.Archived = true
	before = r.calls()
	if res, _, _ := r.run(prev); !res.Unchanged || len(r.calls()) != len(before) || r.calls()["304"] != before["304"] {
		t.Errorf("archived: %+v", res)
	}
}

// TestEventRunConfirms: a run an event asked for that found nothing new reads again in ConfirmAfter, because GitHub's listing can lag the
// webhook that announced the issue; one that found a change, or a scheduled run, does not.
func TestEventRunConfirms(t *testing.T) {
	r := newRig(t)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	r.srv.SetIssues(r.id, githubtest.Issue{Number: 4, Title: "One", Author: "ann", Created: at, Updated: at})
	run := func(prev store.Item, event bool) (sync.Result, store.Item) {
		res, err := r.spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/one", Source: "gh", Repo: "acme/one", Event: event, Prev: prev})
		if err != nil {
			t.Fatal(err)
		}
		return res, store.Item{State: snapshot.StateOK, Payload: res.Payload}
	}
	res, prev := run(store.Item{}, true)
	if res.Again != 0 {
		t.Errorf("an event's run that found a change asked again in %v", res.Again)
	}
	if res, _ = run(prev, false); res.Again != 0 {
		t.Errorf("a scheduled run asked again in %v", res.Again)
	}
	if res, _ = run(prev, true); !res.Unchanged || res.Again != ConfirmAfter {
		t.Errorf("an event's run that found nothing new: unchanged %v, again %v, want %v", res.Unchanged, res.Again, ConfirmAfter)
	}
}

// TestPages reads past a page of pull requests to the issues, and holds at most 100 issues, the total then not exact.
func TestPages(t *testing.T) {
	r := newRig(t)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var ps []githubtest.Pull
	for n := range 150 {
		ps = append(ps, githubtest.Pull{Number: 1000 + n, Title: "p", Author: "renovate[bot]", Created: at, Updated: at.Add(time.Hour),
			Base: "main", Head: fmt.Sprint("renovate/", n), HeadSHA: strings.Repeat("a", 40)})
	}
	r.srv.SetPulls(r.id, ps...)
	r.srv.SetIssues(r.id, githubtest.Issue{Number: 1, Title: "old", Author: "ann", Created: at, Updated: at})
	if _, s, _ := r.run(store.Item{}); len(s.Issues) != 1 || !s.Exact || s.Issues[0].Number != 1 {
		t.Fatalf("past the pull requests: %+v", s)
	}
	var is []githubtest.Issue
	for n := range 120 {
		is = append(is, githubtest.Issue{Number: n + 1, Title: "i", Author: "ann", Created: at, Updated: at.Add(-time.Duration(n) * time.Minute)})
	}
	r.srv.SetIssues(r.id, is...)
	if _, s, _ := r.run(store.Item{}); len(s.Issues) != maxIssues || s.Exact || s.Issues[0].Number != 1 {
		t.Fatalf("more than 100: %d held, exact %v", len(s.Issues), s.Exact)
	}
}

func TestKindOf(t *testing.T) {
	for in, want := range map[string]IssueKind{"bug": KindBug, "Type: Bug": KindBug, "kind/regression": KindBug, "enhancement": KindFeature,
		"Feature Request": KindFeature, "question": KindQuestion, "help wanted": KindOther, "good first issue": KindOther} {
		if got := KindOf(&Issue{Labels: []Label{{Name: in}}}); got != want {
			t.Errorf("KindOf(%q) = %s, want %s", in, got, want)
		}
	}
	if got := KindOf(&Issue{Type: "Bug", Labels: []Label{{Name: "enhancement"}}}); got != KindBug {
		t.Errorf("the issue type wins: %s", got)
	}
	if got := KindOf(&Issue{Labels: []Label{{Name: "help wanted"}, {Name: "bug"}}}); got != KindBug {
		t.Errorf("the first label naming a kind: %s", got)
	}
}
