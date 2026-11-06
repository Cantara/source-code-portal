package pulls

import (
	"encoding/json"
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
	now  time.Time
	repo snapshot.Repo
	spec sync.Spec
}

func newRig(t *testing.T) *rig {
	t.Helper()
	srv := githubtest.New(t)
	r := &rig{t: t, srv: srv, id: srv.AddRepo("acme", gh.Repository{Name: new("one")}), now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	srv.AddToken("ghp_test")
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"}, TokenFile: "x",
		Token: config.NewSecret([]byte("ghp_test"))}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r.spec = New(map[string]Fetcher{"gh": FromGitHub(c.Source("gh"))}, Options{
		Repo: func(snapshot.RepoKey) (snapshot.Repo, bool) { return r.repo, true },
		Now:  func() time.Time { return r.now },
	}).Spec()
	return r
}

func (r *rig) run(prev store.Item) (sync.Result, *Set, store.Item) {
	r.t.Helper()
	res, err := r.spec.Run(r.t.Context(), sync.Job{Key: Kind + "/acme/one", Source: "gh", Repo: "acme/one", Prev: prev})
	if err != nil {
		r.t.Fatalf("run: %v", err)
	}
	if res.Unchanged {
		return res, decode(r.t, prev.Payload), prev
	}
	return res, decode(r.t, res.Payload), store.Item{State: snapshot.StateOK, Payload: res.Payload}
}

func decode(t *testing.T, b []byte) *Set {
	t.Helper()
	var s Set
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return &s
}

// calls counts the fake's requests by path and by status since the last call.
func (r *rig) calls() map[string]int {
	n := map[string]int{}
	for _, l := range r.srv.Ledger() {
		f := strings.Fields(l)
		path, _, _ := strings.Cut(f[1], "?")
		n[path]++
		n[f[2]]++
	}
	return n
}

func diff(a, b map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range b {
		if v != a[k] {
			out[k] = v - a[k]
		}
	}
	return out
}

// TestEventRunConfirms: a run an event asked for reads again within ConfirmAfter, because GitHub's listing can lag the webhook that
// announced the pull request; a scheduled run keeps its own interval.
func TestEventRunConfirms(t *testing.T) {
	r := newRig(t)
	at := r.now.Add(-time.Hour)
	r.srv.SetPulls(r.id, githubtest.Pull{Number: 7, Title: "One", Author: "ann", Created: at, Updated: at, Base: "main", Head: "x",
		HeadSHA: strings.Repeat("a", 40), Checks: "SUCCESS"})
	run := func(event bool) sync.Result {
		res, err := r.spec.Run(t.Context(), sync.Job{Key: Kind + "/acme/one", Source: "gh", Repo: "acme/one", Event: event})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run(false); res.Again == ConfirmAfter {
		t.Errorf("a scheduled run asked again in %v", res.Again)
	}
	if res := run(true); res.Again != ConfirmAfter {
		t.Errorf("an event's run asked again in %v, want %v", res.Again, ConfirmAfter)
	}
}

func TestJob(t *testing.T) {
	r := newRig(t)
	at := r.now.Add(-48 * time.Hour)
	ann := githubtest.Pull{Number: 7, Title: "Add the \u202ething", Author: "ann", Created: at, Updated: at, Base: "main", Head: "feature",
		HeadSHA: strings.Repeat("a", 40), Labels: []string{"enhancement"}, Requested: 2, Review: "REVIEW_REQUIRED", Checks: "SUCCESS",
		Additions: 40, Deletions: 2, Files: 3}
	bot := githubtest.Pull{Number: 9, Title: "Update lib to 2.1", Author: "renovate[bot]", Bot: true, Created: at.Add(-40 * 24 * time.Hour),
		Updated: at.Add(time.Hour), Base: "main", Head: "renovate/lib-2.x", HeadSHA: strings.Repeat("b", 40), Checks: "SUCCESS"}
	draft := githubtest.Pull{Number: 3, Title: "WIP", Author: "bo", Draft: true, Created: at, Updated: at.Add(-time.Hour), Base: "main",
		Head: "wip", HeadSHA: strings.Repeat("c", 40), Fork: true}
	r.srv.SetPulls(r.id, ann, bot, draft)

	res, s, prev := r.run(store.Item{})
	if s.Total != 3 || len(s.Pulls) != 3 || s.Pulls[0].Number != 9 || res.Again != 0 {
		t.Fatalf("first run: %+v, again %v", s, res.Again)
	}
	p := s.Pulls[1]
	if p.Title != "Add the thing" || p.Bot || p.Requested != 2 || len(p.Labels) != 1 || p.Labels[0].Color != "0e8a16" ||
		p.Stats == nil || p.Stats.Additions != 40 || p.Asked.IsZero() || StateOf(&p) != StateReview {
		t.Errorf("a person's pull request: %+v, state %s", p, StateOf(&p))
	}
	if !s.Pulls[0].Bot || StateOf(&s.Pulls[0]) != StateReview || !s.Pulls[2].Fork || StateOf(&s.Pulls[2]) != StateDraft {
		t.Errorf("the bot's %+v, the draft %+v", s.Pulls[0], s.Pulls[2])
	}

	// Nothing changed: the listing is a 304 and no GraphQL query is sent.
	before := r.calls()
	r.now = r.now.Add(15 * time.Minute)
	res, _, prev = r.run(prev)
	if d := diff(before, r.calls()); !res.Unchanged || d["304"] != 1 || d["/graphql"] != 0 || d["200"] != 0 {
		t.Errorf("an unchanged repository: unchanged %v, calls %v; want one 304 and nothing else", res.Unchanged, d)
	}

	// One pull request reviewed: only it is asked.
	ann.Updated, ann.Review, ann.Approved = r.now, "APPROVED", true
	r.srv.SetPulls(r.id, ann, bot, draft)
	before = r.calls()
	_, s, prev = r.run(prev)
	if d := diff(before, r.calls()); d["/graphql"] != 1 || StateOf(&s.Pulls[0]) != StateReady {
		t.Errorf("after a review: calls %v, state %s", d, StateOf(&s.Pulls[0]))
	}

	// Checks start running on a push: asked again at 5, 10, 20 then 60 minutes, not every run.
	ann.Updated, ann.HeadSHA, ann.Checks = r.now.Add(time.Minute), strings.Repeat("d", 40), "PENDING"
	r.srv.SetPulls(r.id, ann, bot, draft)
	r.now = r.now.Add(2 * time.Minute)
	res, s, prev = r.run(prev)
	if StateOf(&s.Pulls[0]) != StateChecks || res.Again != 5*time.Minute {
		t.Fatalf("checks running: state %s, again %v", StateOf(&s.Pulls[0]), res.Again)
	}
	for _, want := range []time.Duration{10 * time.Minute, 20 * time.Minute, time.Hour, time.Hour} {
		r.now = r.now.Add(res.Again)
		before = r.calls()
		res, _, prev = r.run(prev)
		if d := diff(before, r.calls()); d["/graphql"] != 1 || res.Again != min(want, fullEvery) {
			t.Errorf("waiting checks: calls %v, again %v; want one query and %v", d, res.Again, want)
		}
	}
	ann.Checks = "FAILURE"
	r.srv.SetPulls(r.id, ann, bot, draft)
	r.now = r.now.Add(time.Hour)
	res, s, prev = r.run(prev)
	if StateOf(&s.Pulls[0]) != StateFailing || res.Again != 0 || s.Pending != 0 {
		t.Errorf("checks failed: state %s, again %v, pending %d", StateOf(&s.Pulls[0]), res.Again, s.Pending)
	}

	// A failing GraphQL keeps what was known and asks again soon.
	ann.Updated = r.now
	r.srv.SetPulls(r.id, ann, bot, draft)
	r.srv.Fail("POST /graphql", 502, 5, 0)
	res, s, prev = r.run(prev)
	if res.Again != failAgain || StateOf(&s.Pulls[0]) != StateFailing {
		t.Errorf("GraphQL down: again %v, state %s", res.Again, StateOf(&s.Pulls[0]))
	}
	r.srv.Heal()

	// An archived repository is not read again.
	r.repo.Archived = true
	before = r.calls()
	if res, _, _ = r.run(prev); !res.Unchanged || len(diff(before, r.calls())) != 0 {
		t.Errorf("archived: unchanged %v, calls %v", res.Unchanged, diff(before, r.calls()))
	}
	r.repo.Archived = false

	// No open pull request: the listing is empty and nothing is asked.
	r.srv.SetPulls(r.id)
	_, s, _ = r.run(prev)
	if s.Total != 0 || len(s.Pulls) != 0 {
		t.Errorf("none open: %+v", s)
	}
}

func TestStateOf(t *testing.T) {
	asked := time.Now()
	for want, p := range map[State]Pull{
		StateDraft:     {Draft: true, Checks: "FAILURE"},
		StateUnknown:   {},
		StateConflicts: {Asked: asked, Mergeable: "CONFLICTING", Checks: "FAILURE"},
		StateFailing:   {Asked: asked, Checks: "ERROR", Review: "APPROVED"},
		StateChanges:   {Asked: asked, Changes: true},
		StateReview:    {Asked: asked, Review: "REVIEW_REQUIRED", Approved: true},
		StateChecks:    {Asked: asked, Review: "APPROVED", Checks: "PENDING"},
		StateReady:     {Asked: asked, Approved: true, Checks: "SUCCESS"},
	} {
		if got := StateOf(&p); got != want {
			t.Errorf("StateOf(%+v) = %s, want %s", p, got, want)
		}
	}
}
