package catalog

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

func mixed(id int, owner, name string) scm.Repository {
	return scm.Repository{Ref: scm.RepoRef{Source: "acme", ID: scm.RepoID(id), Owner: owner, Name: name}, DefaultBranch: "main", Visibility: scm.VisPublic}
}

// V-A: the real store rejects a RepoKey built from the display case (what the prototype wrote).
func TestVerifyRawCaseKeyIsRejectedByTheRealStore(t *testing.T) {
	raw := store.Repo{Key: store.RepoKey{Source: "acme", Owner: "Acme", Name: "Widget-STS"}, GitHubID: 1, Slug: "widget-sts", Meta: []byte(`{}`), SeenAt: time.Now()}
	if err := store.ValidateRepo(raw); err == nil {
		t.Fatal("expected the real store contract to reject the unfolded key")
	}
}

// V-A fixed: a mixed-case organisation persists and warms with its display case intact.
func TestVerifyMixedCaseOrganisationPersistsAndWarms(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig()
		cfg.Groups = []GroupConfig{{ID: "widget", DisplayName: "W", DefaultRepo: "Widget-STS", Match: []Matcher{{KindRegexp, "Widget-.*"}}}}
		h := newHarness(t, cfg, []scm.Repository{mixed(1, "Acme", "Widget-STS"), mixed(2, "Acme", "Other")})
		h.prober.heads = map[scm.RepoID]string{1: "aaaa", 2: "bbbb"}
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.runHeads(ctx, headsKey); err != nil {
			t.Fatal(err)
		}
		if len(h.st.repos) != 2 || len(h.st.items) != 2 {
			t.Fatalf("persisted %d repos %d items", len(h.st.repos), len(h.st.items))
		}
		h2 := newHarness(t, cfg, nil)
		h2.svc.d.Store = h.st
		fn, info, err := h2.svc.Warm(ctx)
		if err != nil || info.Repos != 2 || info.Skipped != 0 || info.StatsItems != 2 {
			t.Fatalf("warm %+v %v", info, err)
		}
		if _, err := h2.pub.Apply(fn); err != nil {
			t.Fatal(err)
		}
		v, ok := readmodel.Get(h2.pub.Current(), "widget-sts", h2.slots.Repo)
		if !ok || v.V.Ref.Name != "Widget-STS" || v.V.Ref.Owner != "Acme" {
			t.Fatalf("display case lost after warm: %+v", v.V.Ref)
		}
		m, _ := readmodel.GetGlobal(h2.pub.Current(), h2.slots.Membership)
		if got := m.ReposOf("widget"); !slices.Equal(got, []scm.Slug{"widget-sts"}) { // a case-sensitive regexp must still match after a warm start
			t.Fatalf("warm membership %v", got)
		}
	})
}

// V-B: an event hides a repository WHILE a commit is persisting; the commit must not resurrect it.
func TestVerifyHideDuringCommitIsNotUndone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		all := list("api", "pl-core", "docs")
		h := newHarness(t, testConfig(), all)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
		h.lister.set(slices.Concat(all, list("x1", "x2")[:0]), 2)
		h.lister.etag += 5
		fired := false
		h.st.onReplace = func() { // the store write of the commit is in flight: the webhook goroutine runs now
			if fired {
				return
			}
			fired = true
			time.Sleep(time.Second) // virtual time: the event is strictly later than the listing start
			ev := scm.Event{Kind: scm.EventRepository, Action: "privatized", Source: "acme", Repo: scm.RepoRef{Source: "acme", ID: 3, Owner: "acme-org", Name: "docs"}}
			if err := h.svc.React(ctx, ev, &recEnq{}); err != nil {
				t.Error(err)
			}
		}
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); ok {
			t.Fatal("the commit that was already planning resurrected a repository hidden by a privatized event")
		}
		if slices.ContainsFunc(h.svc.Repos(), func(r scm.RepoRef) bool { return r.Name == "docs" }) {
			t.Fatal("memory state still tracks the hidden repository")
		}
	})
}

// V-C: catalog.repo freshness of an unchanged repository must not go stale between listings.
func TestVerifyRepoFreshnessIsRefreshedByEveryCompleteListing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core"))
		h.svc.runReconcile(ctx, reconcileKey)
		for i := 0; i < 6; i++ {
			time.Sleep(31 * time.Minute)
			if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
				t.Fatal(err)
			}
		}
		v, _ := readmodel.Get(h.pub.Current(), "api", h.slots.Repo)
		if st := v.Info.Derive(time.Now()); st.String() != "fresh" {
			t.Fatalf("catalog.repo of an unchanged repository is %s after 3h of successful listings (FetchedAt %v)", st, v.Info.FetchedAt)
		}
	})
}

// V-D: privatized then (renamed earlier, not yet reconciled): the event carries the new name; lookup by id must still hide it.
func TestVerifyHideFindsRenamedRepositoryByID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		h.svc.runReconcile(ctx, reconcileKey)
		ev := scm.Event{Kind: scm.EventRepository, Action: "privatized", Source: "acme", Repo: scm.RepoRef{Source: "acme", ID: 3, Owner: "acme-org", Name: "docs-renamed"}}
		h.svc.React(ctx, ev, &recEnq{})
		if _, ok := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); ok {
			t.Fatal("a privatized event for a repository renamed since the last listing did not hide it")
		}
	})
}

// V-E: Status publishes the failure and the listed count.
func TestVerifyStatusShowsFailureAndListedCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		h.svc.runReconcile(ctx, reconcileKey)
		st, _ := readmodel.GetGlobal(h.pub.Current(), h.slots.Sync)
		if st.Sources[0].Listed != 3 {
			t.Fatalf("Listed = %d, want 3", st.Sources[0].Listed)
		}
		time.Sleep(time.Hour)
		h.lister.failAt = ""
		h.lister.etag += 3
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err == nil {
			t.Fatal("expected a listing error")
		}
		st, _ = readmodel.GetGlobal(h.pub.Current(), h.slots.Sync)
		if st.Sources[0].State != "failed" || st.Sources[0].ErrCode == "" {
			t.Fatalf("the published status does not show the failed listing: %+v", st.Sources[0])
		}
	})
}

// V-F: the stats job key must be the folded key the planner uses, or a webhook-accelerated run never coalesces with the planned one.
func TestVerifyStatsKeyIsFoldedLikeThePlannersKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), []scm.Repository{mixed(1, "Acme", "Widget-STS")})
		h.prober.heads = map[scm.RepoID]string{1: "aaaa"}
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		enq := &recEnq{}
		h.svc.React(ctx, scm.Event{Kind: scm.EventRelease, Source: "acme", Repo: scm.RepoRef{Source: "acme", Owner: "acme", Name: "WIDGET-sts"}}, enq)
		want := job.NewKey(JobStats, h.svc.Repos()[0].KeyParts()...)
		if len(enq.keys) != 1 || enq.keys[0] != want {
			t.Fatalf("enqueued %v, want %v", enq.keys, want)
		}
		// the job itself resolves the folded key back to the repository
		if _, err := h.svc.runStats(ctx, want); err != nil {
			t.Fatal(err)
		}
		if sv, ok := readmodel.Get(h.pub.Current(), "widget-sts", h.slots.Stats); !ok || sv.V.HeadSHA != "aaaa" {
			t.Fatalf("stats job did not publish: %+v %v", sv, ok)
		}
		// a key for a removed repository is Gone, a malformed one Permanent
		if _, err := h.svc.runStats(ctx, job.NewKey(JobStats, "acme", "acme", "nothing")); !job.IsGone(err) {
			t.Fatalf("want gone, got %v", err)
		}
		if _, err := h.svc.runStats(ctx, job.NewKey(JobStats, "acme", "acme")); !job.IsPermanent(err) {
			t.Fatalf("want permanent, got %v", err)
		}
	})
}

// V-G: a held mass removal must not make the service list every confirmGap for the whole hold period.
func TestVerifyHeldRemovalDoesNotPollEveryTwoMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		all := list("a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10")
		cfg := DefaultConfig()
		h := newHarness(t, cfg, all)
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		h.lister.set(all[:3], 10) // a mis-scoped listing: 7 of 10 repositories vanish, more than the guard allows
		start := time.Now()
		runs, listings := 0, 0
		next := start.Add(31 * time.Minute)
		time.Sleep(31 * time.Minute)
		for time.Since(start) < 3*time.Hour {
			before := len(h.lister.calls)
			o, err := h.svc.runReconcile(ctx, reconcileKey)
			if err != nil {
				t.Fatal(err)
			}
			runs++
			if len(h.lister.calls) > before {
				listings++
			}
			// the engine would run the job again at NextDue, or at the job TTL (30 min) when NextDue is zero
			wait := 30 * time.Minute
			if !o.NextDue.IsZero() {
				wait = max(o.NextDue.Sub(time.Now()), time.Second)
			}
			time.Sleep(wait)
			_ = next
		}
		if listings > 8 { // 2 confirmation listings, then the 30-minute cadence: about 6 in three hours
			t.Fatalf("%d listings in 3h while a removal was held back", listings)
		}
		if got := len(h.pub.Current().Repos()); got != 10 {
			t.Fatalf("%d repositories after 3h: the hold (6h) must still keep all 10", got)
		}
		time.Sleep(4 * time.Hour)
		for i := 0; i < 3; i++ { // the next listings after the hold has elapsed apply the removal
			if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
				t.Fatal(err)
			}
			time.Sleep(31 * time.Minute)
		}
		if got := len(h.pub.Current().Repos()); got != 3 {
			t.Fatalf("%d repositories after the hold elapsed, want 3", got)
		}
	})
}

// V-H: what normaliseListed accepts must satisfy the kernel's own identity validation (job.NewKey panics on a bad part; store keys are validated).
func FuzzNormaliseListedAgainstKernel(f *testing.F) {
	for _, s := range [][2]string{{"acme", "api"}, {"Acme", ".."}, {"a_b", "."}, {"-x", "y"}, {"a", "b/c"}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, owner, name string) {
		r := scm.Repository{Ref: scm.RepoRef{Source: "acme", ID: 1, Owner: owner, Name: name}, Visibility: scm.VisPublic}
		out, ok := normaliseListed(r)
		if !ok {
			return
		}
		if err := out.Ref.Validate(); err != nil {
			if slug, serr := MakeSlug("", out.Ref.Name); serr == nil {
				t.Fatalf("accepted %q/%q (slug %q) but the kernel rejects it: %v", owner, name, slug, err)
			}
			t.Fatalf("accepted %q/%q (rejected only by MakeSlug %v)? the kernel rejects it: %v", owner, name, serr(name), err)
		}
	})
}

func serr(name string) error { _, e := MakeSlug("", name); return e }

// V-I: under installation_scope a privatized repository is demoted to private in place (visible to ViewAll only), not removed.
func TestVerifyPrivatizedUnderInstallationScopeIsDemotedNotRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newHarness(t, testConfig(), list("api", "pl-core", "docs"))
		h.svc.d.Sources[0].Policy.Visibility = VisibilityInstallationScope
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		ev := scm.Event{Kind: scm.EventRepository, Action: "privatized", Source: "acme", Repo: scm.RepoRef{Source: "acme", ID: 3, Owner: "acme-org", Name: "docs"}}
		time.Sleep(time.Second)
		h.svc.React(ctx, ev, &recEnq{})
		s := h.pub.Current()
		v, ok := readmodel.Get(s, "docs", h.slots.Repo)
		if !ok || v.V.Visibility != scm.VisPrivate {
			t.Fatalf("entry %+v ok=%v", v.V, ok)
		}
		m, _ := readmodel.GetGlobal(s, h.slots.Membership)
		if slices.Contains(m.VisibleReposOf("all", scm.ViewPublic), "docs") || !slices.Contains(m.VisibleReposOf("all", scm.ViewAll), "docs") {
			t.Fatalf("public %v all %v", m.VisibleReposOf("all", scm.ViewPublic), m.VisibleReposOf("all", scm.ViewAll))
		}
		// an older listing still saying public must not flip it back
		src, _ := h.svc.source("acme")
		listStart := time.Now().UTC().Add(-time.Minute)
		if _, _, err := h.svc.commit(ctx, src, list("api", "pl-core", "docs"), listStart); err != nil {
			t.Fatal(err)
		}
		if v, _ := readmodel.Get(h.pub.Current(), "docs", h.slots.Repo); v.V.Visibility != scm.VisPrivate {
			t.Fatal("an older listing undid the demotion")
		}
	})
}

// V-J (T-11 at card level): the anonymous view of a catalogue with private repositories equals the view of a deployment that tracks only the public ones.
func TestVerifyAnonymousCardsAreIdenticalToAPublicOnlyDeployment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig()
		cfg.Groups = []GroupConfig{
			{ID: "g1", DisplayName: "G1", DefaultRepo: "p1", Match: []Matcher{{KindPrefix, "p"}, {KindExact, "x1"}}, Integrations: GroupIntegrations{Jenkins: &JenkinsDefaults{JobPrefix: "J-"}}},
			{ID: "g2", DisplayName: "G2", DefaultRepo: "x2", Match: []Matcher{{KindPrefix, "x2"}}},
			{ID: "g3", DisplayName: "G3", DefaultRepo: "x3", Match: []Matcher{{KindPrefix, "q"}, {KindExact, "x3"}}},
		}
		mk := func(id int, name string, vis scm.Visibility) scm.Repository {
			r := repo(id, name)
			r.Visibility = vis
			return r
		}
		pub := []scm.Repository{mk(1, "p1", scm.VisPublic), mk(2, "p2", scm.VisPublic), mk(3, "q1", scm.VisPublic), mk(4, "q2", scm.VisPublic), mk(5, "p5", scm.VisPublic)}
		priv := []scm.Repository{mk(6, "x1", scm.VisPrivate), mk(7, "x2-a", scm.VisPrivate), mk(8, "x3", scm.VisInternal)}
		a := newHarness(t, cfg, slices.Concat(pub, priv))
		a.svc.d.Sources[0].Policy.Visibility = VisibilityInstallationScope
		b := newHarness(t, cfg, pub)
		for _, h := range []*harness{a, b} {
			h.prober.heads = map[scm.RepoID]string{1: "s1", 3: "s3"}
			if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
				t.Fatal(err)
			}
			if _, err := h.svc.runHeads(ctx, headsKey); err != nil {
				t.Fatal(err)
			}
		}
		ga, ok1 := a.slots.Groups(a.pub.Current(), scm.ViewPublic)
		gb, ok2 := b.slots.Groups(b.pub.Current(), scm.ViewPublic)
		if !ok1 || !ok2 || !reflect.DeepEqual(ga, gb) {
			t.Fatalf("anonymous cards differ:\\n%+v\\n%+v", ga, gb)
		}
		if len(ga) != 2 || ga[0].RepoCount != 3 || ga[1].DefaultState != "missing" || ga[1].RepoCount != 2 {
			t.Fatalf("cards: %+v", ga)
		}
		if _, ok := a.slots.Group(a.pub.Current(), scm.ViewPublic, "g2"); ok {
			t.Fatal("a group with only private members must be a 404 for the public class")
		}
		if _, ok := a.slots.Group(a.pub.Current(), scm.ViewPublic, "nope"); ok {
			t.Fatal("unknown group")
		}
		all, _ := a.slots.Groups(a.pub.Current(), scm.ViewAll)
		if len(all) != 3 || all[0].RepoCount != 4 {
			t.Fatalf("ViewAll cards: %d", len(all))
		}
		g1, _ := a.slots.Group(a.pub.Current(), scm.ViewPublic, "g1")
		if !slices.Equal(g1.Members, []scm.Slug{"p1", "p2", "p5"}) {
			t.Fatalf("members %v", g1.Members)
		}
	})
}

// V-K: a catalogue persisted under installation_scope is not served after the policy was tightened to public_only.
func TestVerifyWarmAppliesTheCurrentVisibilityPolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		pr := repo(2, "secret")
		pr.Visibility = scm.VisPrivate
		h := newHarness(t, testConfig(), []scm.Repository{repo(1, "api"), pr})
		h.svc.d.Sources[0].Policy.Visibility = VisibilityInstallationScope
		if _, err := h.svc.runReconcile(ctx, reconcileKey); err != nil {
			t.Fatal(err)
		}
		h2 := newHarness(t, testConfig(), nil) // restart with the default (public_only) policy over the same store
		h2.svc.d.Store = h.st
		fn, info, err := h2.svc.Warm(ctx)
		if err != nil || info.Repos != 1 || info.Skipped != 1 {
			t.Fatalf("%+v %v", info, err)
		}
		h2.pub.Apply(fn)
		if _, ok := readmodel.Get(h2.pub.Current(), "secret", h2.slots.Repo); ok {
			t.Fatal("a private repository from the old policy was served after the warm start")
		}
	})
}
