package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
	tnoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// ---- fakes (hand-written, named after behaviour: the feature tests never import an adapter) ----

type fakeLister struct {
	mu     sync.Mutex
	pages  map[string][]scm.Repository // cursor -> items; next cursor is "p<n+1>" until the last
	order  []string
	failAt string // cursor whose request fails
	calls  []string
	etag   int
}

func newLister(all []scm.Repository, per int) *fakeLister {
	l := &fakeLister{pages: map[string][]scm.Repository{}, failAt: "-"}
	l.set(all, per)
	return l
}
func (l *fakeLister) set(all []scm.Repository, per int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pages, l.order = map[string][]scm.Repository{}, nil
	for i, c := 0, ""; i < len(all) || i == 0; i += per {
		l.pages[c] = all[i:min(i+per, len(all))]
		l.order = append(l.order, c)
		c = fmt.Sprintf("p%d", len(l.order))
	}
	l.etag++
}
func (l *fakeLister) ListRepositories(_ context.Context, _ scm.SourceID, cursor string, v scm.Validators) (scm.Result[scm.Page[scm.Repository]], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, cursor)
	if cursor == l.failAt {
		return scm.Result[scm.Page[scm.Repository]]{}, errors.New("boom")
	}
	items := l.pages[cursor]
	idx := slices.Index(l.order, cursor)
	next := ""
	if idx+1 < len(l.order) {
		next = l.order[idx+1]
	}
	tag := fmt.Sprintf("v%d", l.etag)
	if v.ETag == tag {
		return scm.Result[scm.Page[scm.Repository]]{NotModified: true, Validators: v}, nil
	}
	return scm.Result[scm.Page[scm.Repository]]{Value: scm.Page[scm.Repository]{Items: slices.Clone(items), Next: next}, Validators: scm.Validators{ETag: tag}}, nil
}

type fakeProber struct {
	mu    sync.Mutex
	heads map[scm.RepoID]string
	calls int
	omit  map[scm.RepoID]bool
}

func (p *fakeProber) ProbeHeads(_ context.Context, rs []scm.RepoRef) (map[scm.RepoID]scm.RepoStats, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	out := map[scm.RepoID]scm.RepoStats{}
	for _, r := range rs {
		if p.omit[r.ID] {
			continue
		}
		out[r.ID] = scm.RepoStats{HeadSHA: p.heads[r.ID], HeadTreeOID: "tree-" + p.heads[r.ID], OpenIssues: 3}
	}
	return out, nil
}

type memStore struct {
	mu        sync.Mutex
	repos     []store.Repo
	items     map[store.ItemKey]store.Item
	onReplace func() // test hook: runs inside ReplaceCatalogue before the rows are stored (simulates a slow store write)
}

func (s *memStore) LoadCatalogue(context.Context) ([]store.Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.repos), nil
}
func (s *memStore) LoadItems(_ context.Context, fn func(store.Item) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}
func (s *memStore) ReplaceCatalogue(_ context.Context, rs []store.Repo) ([]store.RepoKey, error) {
	for _, r := range rs { // the fake enforces the same input contract as store/sqlite and store/memory (verification V-01)
		if err := store.ValidateRepo(r); err != nil {
			return nil, err
		}
	}
	if s.onReplace != nil {
		s.onReplace()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[store.RepoKey]bool{}
	for _, r := range rs {
		keep[r.Key] = true
	}
	var removed []store.RepoKey
	for _, r := range s.repos {
		if !keep[r.Key] {
			removed = append(removed, r.Key)
		}
	}
	for k := range s.items {
		if !keep[k.Repo] {
			delete(s.items, k)
		}
	}
	s.repos = slices.Clone(rs)
	return removed, nil
}
func (s *memStore) PutItem(_ context.Context, it store.Item) error {
	if err := store.ValidateItem(it); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.repos, func(r store.Repo) bool { return r.Key == it.Key.Repo }) {
		return store.ErrNoRepo
	}
	s.items[it.Key] = it
	return nil
}
func (s *memStore) Revalidated(_ context.Context, k store.ItemKey, _ time.Time) error {
	return k.Validate()
}

type recTracker struct{ last []scm.RepoRef }

func (t *recTracker) SetRepos(r []scm.RepoRef) { t.last = r }

type recEvents struct{ evs []scm.Event }

func (e *recEvents) Handle(_ context.Context, ev scm.Event) error {
	e.evs = append(e.evs, ev)
	return nil
}

type recEnq struct {
	keys []job.Key
	prio []job.Priority
}

func (e *recEnq) Enqueue(k job.Key, p job.Priority, _ time.Time, _ bool) error {
	e.keys, e.prio = append(e.keys, k), append(e.prio, p)
	return nil
}

type harness struct {
	svc    *Service
	lister *fakeLister
	prober *fakeProber
	st     *memStore
	tr     *recTracker
	ev     *recEvents
	pub    *readmodel.Publisher
	slots  Slots
}

func newHarness(t *testing.T, cfg Config, repos []scm.Repository) *harness {
	t.Helper()
	reg := &readmodel.Registry{}
	slots := RegisterSlots(reg)
	reg.Freeze()
	pub := readmodel.NewPublisher(reg, nil, []readmodel.Validator{slots.Validator()}, readmodel.Deps{})
	h := &harness{lister: newLister(repos, 2), prober: &fakeProber{heads: map[scm.RepoID]string{}}, st: &memStore{items: map[store.ItemKey]store.Item{}},
		tr: &recTracker{}, ev: &recEvents{}, pub: pub, slots: slots}
	svc, err := New(Deps{Config: cfg, Sources: []SourceDeps{{ID: "acme", Policy: DefaultDiscoveryPolicy()}}, Lister: h.lister, Prober: h.prober, Store: h.st,
		Publisher: pub, Slots: slots, Tracker: h.tr, Events: h.ev, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Meter: noop.NewMeterProvider().Meter("t"), Tracer: tnoop.NewTracerProvider().Tracer("t")})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	return h
}

func testConfig() Config {
	c := DefaultConfig()
	c.Groups = []GroupConfig{
		{ID: "platform", DisplayName: "Platform", DefaultRepo: "api", Match: []Matcher{{KindPrefix, "pl-"}, {KindExact, "api"}}, Aliases: []string{"Platform Team"}},
		{ID: "all", DisplayName: "All", DefaultRepo: "api", Match: []Matcher{{KindCatchAll, "true"}}},
	}
	return c
}

func list(names ...string) []scm.Repository {
	var out []scm.Repository
	for i, n := range names {
		out = append(out, repo(i+1, n))
	}
	return out
}

func TestServiceReconcileLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs", "zeta"))
		run := func() job.Outcome {
			t.Helper()
			o, err := h.svc.runReconcile(ctx, reconcileKey)
			if err != nil {
				t.Fatal(err)
			}
			return o
		}
		// cold start: two pages, everything added, snapshot and store agree
		if o := run(); o.Unchanged {
			t.Fatal("first reconcile reports Unchanged")
		}
		s := h.pub.Current()
		if got := s.Repos(); !slices.Equal(got, []readmodel.EntryID{"api", "docs", "pl-core", "zeta"}) {
			t.Fatalf("snapshot repos: %v", got)
		}
		m, _ := readmodel.GetGlobal(s, h.slots.Membership)
		if got := m.ReposOf("platform"); !slices.Equal(got, []scm.Slug{"api", "pl-core"}) {
			t.Fatalf("platform members: %v", got)
		}
		if d, ok := m.DefaultRepo("platform"); !ok || d != "api" {
			t.Fatalf("default: %v %v", d, ok)
		}
		if got := m.GroupsOf("api"); !slices.Equal(got, []scm.GroupID{"platform", "all"}) {
			t.Fatalf("groups of api: %v", got) // config order, many-to-many
		}
		if len(h.st.repos) != 4 || len(h.tr.last) != 4 {
			t.Fatalf("store %d tracker %d", len(h.st.repos), len(h.tr.last))
		}
		ga, _ := readmodel.GetGlobal(s, h.slots.GroupAliases)
		if ga["platform team"] != "platform" {
			t.Fatalf("group aliases: %v", ga)
		}
		v1 := s.Version()

		// not yet due: nothing happens and no upstream call is made
		calls := len(h.lister.calls)
		time.Sleep(10 * time.Minute)
		run()
		if len(h.lister.calls) != calls || h.pub.Current().Version() != v1 {
			t.Fatal("reconcile ran before reconcile_every elapsed")
		}

		// due, unchanged: every page answers 304, no publish content change, Unchanged
		time.Sleep(25 * time.Minute)
		if o := run(); !o.Unchanged {
			t.Fatal("304 pass must report Unchanged")
		}
		if got := h.lister.calls[calls:]; !slices.Equal(got, []string{"", "p1"}) {
			t.Fatalf("calls: %v", got)
		}

		// a rename: gateway replaces api; the old slug stays reachable as an alias, the default repo follows by name only
		time.Sleep(31 * time.Minute)
		renamed := list("gateway", "pl-core", "docs", "zeta")
		h.lister.set(renamed, 2)
		run()
		s = h.pub.Current()
		if _, ok := readmodel.Get(s, "api", h.slots.Repo); ok {
			t.Fatal("old slug still has an entry")
		}
		al, _ := readmodel.GetGlobal(s, h.slots.Aliases)
		if al["api"] != "gateway" {
			t.Fatalf("aliases: %v", al)
		}
		m, _ = readmodel.GetGlobal(s, h.slots.Membership)
		if _, ok := m.DefaultRepo("platform"); ok {
			t.Fatal("default repository 'api' no longer exists; the group must report no default, not guess one")
		}
		st, _ := readmodel.GetGlobal(s, h.slots.Sync)
		if !slices.ContainsFunc(st.Findings, func(f Finding) bool { return f.Code == "R205" }) {
			t.Fatalf("expected R205 in status findings: %+v", st.Findings)
		}
	})
}

func TestServiceListFailureChangesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs", "zeta"))
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		v := h.pub.Current().Version()
		time.Sleep(time.Hour)
		h.lister.failAt = "p1" // the second page fails
		h.lister.set(list("api"), 2)
		h.lister.set(list("api", "pl-core", "docs", "zeta"), 2)
		h.lister.etag += 10
		_, err := h.svc.runReconcile(ctx, reconcileKey)
		var le *ListError
		if !errors.As(err, &le) || le.Page != 1 {
			t.Fatalf("want *ListError at page 1, got %v", err)
		}
		after := h.pub.Current()
		if len(h.st.repos) != 4 || len(after.Repos()) != 4 || after.Version() > v+1 { // only catalog.sync may change: it now says "failed"
			t.Fatal("a failed listing changed state")
		}
		if st, _ := readmodel.GetGlobal(after, h.slots.Sync); st.Sources[0].State != "failed" || st.Sources[0].ErrCode != "transient" {
			t.Fatalf("the failure is not visible in catalog.sync: %+v", st.Sources[0])
		}
		if code := errCode(err); code != "transient" {
			t.Fatalf("code %q", code)
		}
	})
}

func TestServiceHeadsSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		h.svc.d.Config.Heads.BatchSize = 2
		h.prober.heads = map[scm.RepoID]string{1: "aaaaaaaaaaaaaaaa", 2: "bbbbbbbbbbbbbbbb", 3: "cccccccccccccccc"}
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.runHeads(ctx, headsKey); err != nil {
			t.Fatal(err)
		}
		if h.prober.calls != 2 || len(h.ev.evs) != 0 {
			t.Fatalf("first sweep: calls=%d events=%d (first sight must not emit)", h.prober.calls, len(h.ev.evs))
		}
		if sv, ok := readmodel.Get(h.pub.Current(), "api", h.slots.Stats); !ok || sv.V.HeadSHA != "aaaaaaaaaaaaaaaa" {
			t.Fatalf("stats slot: %+v %v", sv, ok)
		}
		// repository 2 moves, repository 3 is not returned (never a removal)
		h.prober.heads[2] = "dddddddddddddddd"
		h.prober.omit = map[scm.RepoID]bool{3: true}
		o, err := h.svc.runHeads(ctx, headsKey)
		if err != nil || o.Unchanged {
			t.Fatalf("second sweep: %v %+v", err, o)
		}
		if len(h.ev.evs) != 1 || h.ev.evs[0].Kind != scm.EventHeadChanged || h.ev.evs[0].Repo.ID != 2 || h.ev.evs[0].Delivery != "head-2-dddddddddddd" || h.ev.evs[0].Before != "bbbbbbbbbbbbbbbb" {
			t.Fatalf("events: %+v", h.ev.evs)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); !ok {
			t.Fatal("a repository missing from the probe was removed")
		}
		if got := len(h.st.items); got != 3 {
			t.Fatalf("persisted stats items: %d", got)
		}
		// an identical third sweep changes nothing and emits nothing
		h.prober.omit = nil
		h.svc.runHeads(ctx, headsKey)
		h.ev.evs = nil
		o, _ = h.svc.runHeads(ctx, headsKey)
		if !o.Unchanged || len(h.ev.evs) != 0 {
			t.Fatalf("steady state: %+v events=%d", o, len(h.ev.evs))
		}
	})
}

func TestServiceReactHidesPrivatizedAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		enq := &recEnq{}
		ev := scm.Event{Kind: scm.EventRepository, Action: "privatized", Source: "acme", Repo: scm.RepoRef{Source: "acme", ID: 3, Owner: "acme-org", Name: "docs"}}
		if err := h.svc.React(ctx, ev, enq); err != nil {
			t.Fatal(err)
		}
		s := h.pub.Current()
		if _, ok := readmodel.Get(s, "docs", h.slots.Repo); ok {
			t.Fatal("privatized repository still served")
		}
		m, _ := readmodel.GetGlobal(s, h.slots.Membership)
		if slices.Contains(m.ReposOf("all"), "docs") {
			t.Fatal("membership still lists it")
		}
		if len(enq.keys) != 1 || enq.keys[0] != reconcileKey || enq.prio[0] != job.PrioInteractive {
			t.Fatalf("enqueue: %v %v", enq.keys, enq.prio)
		}
		// GitHub still lists it, now private: the next reconcile keeps it out and persists the removal
		list3 := list("api", "pl-core", "docs")
		list3[2].Visibility = scm.VisPrivate
		h.lister.set(list3, 2)
		time.Sleep(time.Hour)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); ok || len(h.st.repos) != 2 {
			t.Fatalf("after reconcile: store has %d rows", len(h.st.repos))
		}
		// a release event enqueues the per-repository stats job; an unknown repository is rate-limited to one reconcile
		enq = &recEnq{}
		h.svc.React(ctx, scm.Event{Kind: scm.EventRelease, Source: "acme", Repo: scm.RepoRef{Owner: "acme-org", Name: "API"}}, enq)
		if len(enq.keys) != 1 || enq.keys[0] != job.NewKey(JobStats, "acme", "acme-org", "api") {
			t.Fatalf("release: %v", enq.keys)
		}
		enq = &recEnq{}
		unknown := scm.Event{Kind: scm.EventPush, Source: "acme", Repo: scm.RepoRef{Owner: "acme-org", Name: "new-thing"}}
		h.svc.React(ctx, unknown, enq)
		h.svc.React(ctx, unknown, enq)
		if len(enq.keys) != 1 {
			t.Fatalf("unknown-repository reconciles: %d", len(enq.keys))
		}
	})
}

func TestServiceWarmRoundTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		h.prober.heads = map[scm.RepoID]string{1: "a1", 2: "b2", 3: "c3"}
		h.svc.runReconcile(ctx, reconcileKey)
		h.svc.runHeads(ctx, headsKey)

		// a new process over the same store
		h2 := newHarness(t, testConfig(), nil)
		h2.st = h.st
		h2.svc.d.Store = h.st
		fn, info, err := h2.svc.Warm(ctx)
		if err != nil || info.Repos != 3 || info.StatsItems != 3 {
			t.Fatalf("warm: %+v %v", info, err)
		}
		if _, err := h2.pub.Apply(fn); err != nil {
			t.Fatal(err)
		}
		s := h2.pub.Current()
		if sv, ok := readmodel.Get(s, "pl-core", h2.slots.Stats); !ok || sv.V.HeadSHA != "b2" {
			t.Fatalf("warm stats: %+v %v", sv, ok)
		}
		st, _ := readmodel.GetGlobal(s, h2.slots.Sync)
		if !st.LastReconcile.IsZero() {
			t.Fatal("a warm start must not claim a live reconcile")
		}
		if have, total := h2.slots.Coverage(s); have != 3 || total != 3 {
			t.Fatalf("coverage %d/%d", have, total)
		}
		// the first live reconcile after a warm start is due at once, however young the warm rows are, and the warm set is available to the planner bridge
		if !h2.svc.due("acme") {
			t.Fatal("a warm start must not postpone the first live listing")
		}
		if got := h2.svc.Repos(); len(got) != 3 || got[0].Name != "api" {
			t.Fatalf("Repos after Warm: %v", got)
		}
	})
}

func TestServiceEventForcesListingBeforeReconcileEvery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core"))
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		h.lister.set(list("api", "pl-core", "pl-new"), 2)
		time.Sleep(time.Minute) // far below reconcile_every (30m)
		if o, _ := h.svc.runReconcile(ctx, reconcileKey); !o.Unchanged {
			t.Fatal("without an event the source is not due")
		}
		if _, ok := readmodel.Get(h.pub.Current(), "pl-new", h.slots.Repo); ok {
			t.Fatal("listed before it was due")
		}
		ev := scm.Event{Kind: scm.EventRepository, Action: "created", Source: "acme", Repo: scm.RepoRef{Source: "acme", Owner: "acme-org", Name: "pl-new"}}
		if err := h.svc.React(ctx, ev, &recEnq{}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "pl-new", h.slots.Repo); !ok {
			t.Fatal("an authenticated repository event must make the source due at once")
		}
		// a resync of another source id is ignored; an empty source means every source (refresh design D10)
		h.svc.React(ctx, scm.Event{Kind: scm.EventInstallationRepos, Action: "resync"}, &recEnq{})
		calls := len(h.lister.calls)
		h.svc.runReconcile(ctx, reconcileKey)
		if len(h.lister.calls) == calls {
			t.Fatal("resync without a source did not list")
		}
	})
}

func TestServiceAbsenceIsConfirmedByAFollowUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		all := list("api", "pl-core", "docs", "zeta", "a1", "a2", "a3", "a4", "a5", "a6")
		h := newHarness(t, testConfig(), all)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		h.lister.set(slices.Delete(slices.Clone(all), 2, 3), 3) // "docs" (id 3) leaves the listing; every other id is unchanged
		time.Sleep(31 * time.Minute)
		start := time.Now()
		o, err := h.svc.runReconcile(ctx, reconcileKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); !ok {
			t.Fatal("one missed listing must not remove a repository")
		}
		if want := start.Add(confirmGap); !o.NextDue.Equal(want) {
			t.Fatalf("NextDue = %v, want %v", o.NextDue, want)
		}
		time.Sleep(confirmGap)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); ok {
			t.Fatal("the second complete listing without it must remove it")
		}
	})
}

func TestServiceRenameCarriesStatsWithTheirOwnFreshness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core"))
		h.prober.heads = map[scm.RepoID]string{1: "a1", 2: "b2"}
		h.svc.runReconcile(ctx, reconcileKey)
		h.svc.runHeads(ctx, headsKey)
		before, _ := readmodel.Get(h.pub.Current(), "api", h.slots.Stats)
		time.Sleep(2 * time.Hour)
		h.lister.set(list("gateway", "pl-core"), 2) // id 1 renamed
		h.svc.runReconcile(ctx, reconcileKey)
		after, ok := readmodel.Get(h.pub.Current(), "gateway", h.slots.Stats)
		if !ok || after.V.HeadSHA != "a1" {
			t.Fatalf("stats not carried to the renamed entry: %+v %v", after, ok)
		}
		if !after.Info.FetchedAt.Equal(before.Info.FetchedAt) {
			t.Fatalf("carried stats were re-stamped: %v -> %v", before.Info.FetchedAt, after.Info.FetchedAt)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "api", h.slots.Stats); ok {
			t.Fatal("old entry still has stats")
		}
	})
}

func TestServiceHiddenRepositoryIsNotResurrectedByAnOlderListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		all := list("api", "pl-core", "docs")
		h := newHarness(t, testConfig(), all)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		listStart := time.Now().UTC()
		time.Sleep(time.Second) // the listing began, then the event arrived while it was still being fetched
		ev := scm.Event{Kind: scm.EventRepository, Action: "privatized", Source: "acme", Repo: scm.RepoRef{Source: "acme", ID: 3, Owner: "acme-org", Name: "docs"}}
		h.svc.React(ctx, ev, &recEnq{})
		src, _ := h.svc.source("acme")
		if _, _, err := h.svc.commit(ctx, src, all, listStart); err != nil { // the older listing still says docs is public
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); ok {
			t.Fatal("an older listing resurrected a repository that an event hid")
		}
		time.Sleep(time.Second) // a listing that began after the event decides
		src2 := time.Now().UTC()
		if _, _, err := h.svc.commit(ctx, src, all, src2); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); !ok {
			t.Fatal("a newer listing that still says public must re-add it (the hint was false)")
		}
	})
}

// Reconcile, head sweeps, webhook reactions and readers run concurrently under -race; every snapshot a reader sees is consistent.
func TestServiceConcurrentUse(t *testing.T) {
	ctx := context.Background()
	var all []scm.Repository
	for i := 1; i <= 40; i++ {
		all = append(all, repo(i, fmt.Sprintf("pl-r%02d", i)))
	}
	h := newHarness(t, testConfig(), all)
	h.prober.heads = map[scm.RepoID]string{}
	for i := 1; i <= 40; i++ {
		h.prober.heads[scm.RepoID(i)] = "sha0"
	}
	if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
		t.Fatal(err)
	}
	bad := make(chan string, 1)
	report := func(s string) {
		select {
		case bad <- s:
		default:
		}
	}
	var writers, readers sync.WaitGroup
	stop := make(chan struct{})
	writers.Add(3)
	go func() { // the listing flips between two worlds; some repositories are renamed or made private
		defer writers.Done()
		for i := 0; i < 60; i++ {
			w := slices.Clone(all)
			if i%2 == 1 {
				w = w[:35]
				w[3].Ref.Name = "pl-renamed"
				w[4].Visibility = scm.VisPrivate
			}
			h.lister.set(w, 7)
			h.svc.want("acme")
			if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
				report(err.Error())
			}
		}
	}()
	go func() {
		defer writers.Done()
		for i := 0; i < 60; i++ {
			h.prober.mu.Lock()
			for id := range h.prober.heads {
				h.prober.heads[id] = fmt.Sprintf("sha%d", i)
			}
			h.prober.mu.Unlock()
			if _, err := h.svc.runHeads(ctx, headsKey); err != nil {
				report(err.Error())
			}
		}
	}()
	go func() {
		defer writers.Done()
		for i := 0; i < 200; i++ {
			ev := scm.Event{Kind: scm.EventRepository, Action: []string{"privatized", "created", "renamed"}[i%3], Source: "acme",
				Repo: scm.RepoRef{Source: "acme", Owner: "acme-org", Name: fmt.Sprintf("pl-r%02d", 1+i%40)}}
			if err := h.svc.React(ctx, ev, &recEnq{}); err != nil {
				report(err.Error())
			}
		}
	}()
	readers.Add(2)
	for range 2 {
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s := h.pub.Current()
				m, ok := readmodel.GetGlobal(s, h.slots.Membership)
				if !ok {
					continue
				}
				for _, g := range m.Groups() {
					for _, slug := range m.ReposOf(g.ID) {
						if _, ok := readmodel.Get(s, readmodel.EntryID(slug), h.slots.Repo); !ok {
							report("membership lists " + string(slug) + " without a repository value")
							return
						}
					}
				}
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	select {
	case s := <-bad:
		t.Fatal(s)
	default:
	}
	if st := h.pub.Status(); st.Rejected != 0 || st.Aborted != 0 { // a hide that publishes a membership older than the snapshot is rejected by the validator
		t.Fatalf("snapshot candidates rejected or aborted under concurrency: %+v (last: %v)", st, st.LastRejected)
	}
}
