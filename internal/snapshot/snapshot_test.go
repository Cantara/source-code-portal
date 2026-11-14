package snapshot

import (
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestDerive(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	ttl := 10 * time.Minute
	tests := []struct {
		name string
		f    Freshness
		want Status
	}{
		{"never", Freshness{State: StateNever}, StatusUnknown},
		{"failing, never confirmed", Freshness{State: StateNever, Attempts: 2}, StatusError},
		{"permanent error, never confirmed", Freshness{State: StateError}, StatusError},
		{"fresh", Freshness{State: StateOK, CheckedAt: now.Add(-ttl), TTL: ttl}, StatusFresh},
		{"at 1.5 x TTL", Freshness{State: StateOK, CheckedAt: now.Add(-ttl * 3 / 2), TTL: ttl}, StatusFresh},
		{"past 1.5 x TTL", Freshness{State: StateOK, CheckedAt: now.Add(-ttl*3/2 - time.Second), TTL: ttl}, StatusStale},
		{"failing with a value", Freshness{State: StateOK, CheckedAt: now, Attempts: 1, TTL: ttl}, StatusStale},
		{"not found is a result", Freshness{State: StateNotFound, CheckedAt: now, TTL: ttl}, StatusFresh},
		{"event-only item", Freshness{State: StateOK, CheckedAt: now.Add(-48 * time.Hour)}, StatusFresh},
		{"clock skew reads fresh", Freshness{State: StateOK, CheckedAt: now.Add(time.Hour), TTL: ttl}, StatusFresh},
	}
	for _, tc := range tests {
		if got := tc.f.Derive(now); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func repo(source string, id int64, owner, name string) *RepoEntry {
	return &RepoEntry{Repo: Repo{ID: RepoID{Source: source, GitHubID: id}, Owner: owner, Name: name}}
}

func TestDebounce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewPublisher(200*time.Millisecond, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
		defer p.Close()

		// A burst of updates 100 ms apart keeps resetting the quiet timer; the max delay publishes after 1 s.
		start, empty := time.Now(), p.Current()
		for i := range 15 {
			p.Update(func(b *Builder) {
				b.ReplaceSource("gh", []*RepoEntry{repo("gh", int64(i), "o", "r")}, nil, SourceStatus{ID: "gh"})
			})
			time.Sleep(100 * time.Millisecond)
			synctest.Wait()
			if i < 9 && p.Current() != empty {
				t.Fatalf("published after %v, before the max delay", time.Since(start))
			}
		}
		if p.Current() == empty {
			t.Fatal("no publish after 1.5 s of updates, want one at the max delay")
		}

		// A single update publishes after the quiet period.
		time.Sleep(time.Second)
		synctest.Wait()
		cur := p.Current()
		p.Update(func(b *Builder) { b.SetCatalogued() })
		time.Sleep(199 * time.Millisecond)
		synctest.Wait()
		if p.Current() != cur {
			t.Fatal("published before the quiet period")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := p.Current(); got == cur || !got.Catalogued {
			t.Fatalf("after quiet period: catalogued %v, want a publish", got.Catalogued)
		}
	})
}

func TestVersionFollowsTheItems(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.Close()
	publish := func(fn func(*Builder)) uint64 { p.Update(fn); p.Flush(); return p.Current().Version }
	ab := publish(func(b *Builder) { b.Mark("a", "", 1<<60|7, 1<<60|7); b.Mark("b", "", 9, 9) })
	if ab == 0 || ab >= 1<<53 {
		t.Fatalf("version %d, want nonzero within 53 bits", ab)
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 10, 10) }); v == ab {
		t.Error("a changed item kept the version")
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 9, 9) }); v != ab {
		t.Errorf("the same items give %d, want %d", v, ab)
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 0, 0) }); v == ab {
		t.Error("a dropped item kept the version")
	}
	q := NewPublisher(time.Hour, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer q.Close()
	q.Update(func(b *Builder) { b.Mark("b", "", 9, 9) })
	q.Update(func(b *Builder) { b.Mark("a", "", 3, 3); b.Mark("a", "", 1<<60|7, 1<<60|7) })
	q.Flush()
	if v := q.Current().Version; v != ab {
		t.Errorf("the same items in another order give %d, want %d", v, ab)
	}
}

func TestSnapshotsAreImmutable(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.Close()
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{repo("gh", 1, "Cantara", "Old")}, nil, SourceStatus{ID: "gh", Repos: 1})
		b.SetGroups([]Group{{ID: "all", Members: []RepoKey{"cantara/old"}}})
	})
	p.Flush()
	v1 := p.Current()

	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{repo("gh", 1, "Cantara", "New")}, map[RepoKey]RepoKey{"cantara/old": "cantara/new"}, SourceStatus{ID: "gh", Repos: 1})
		b.SetGroups([]Group{{ID: "all", Members: []RepoKey{"cantara/new"}}})
	})
	p.Flush()
	v2 := p.Current()

	if _, ok := v1.Repos["cantara/old"]; !ok || len(v1.Repos) != 1 {
		t.Fatalf("v1 changed: %+v", v1.Repos)
	}
	if e, moved := v2.Repo("cantara/old"); e == nil || !moved || e.Repo.Name != "New" {
		t.Fatalf("v2 alias: entry %v moved %v", e, moved)
	}
	if got := v2.GroupsOf("cantara/new"); len(got) != 1 || got[0] != "all" {
		t.Fatalf("GroupsOf = %v", got)
	}
}

func TestPanickingUpdateIsDroppedAlone(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	defer p.Close()
	p.Update(func(b *Builder) { b.SetCatalogued() })
	p.Update(func(*Builder) { panic("boom") })
	p.Flush()
	if got := p.Current(); got.BuiltAt.IsZero() || !got.Catalogued {
		t.Fatal("a panicking update took the other updates of its batch with it")
	}
}

// An index that cannot be built keeps its last good value: the rest of the batch is published, and the feature does not go dark.
func TestPanickingIndexKeepsItsLastValue(t *testing.T) {
	const flaky Index[int] = "flaky"
	broken := false
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), flaky.Build(func(s *Snapshot) int {
		if broken {
			panic("boom")
		}
		return len(s.Groups) + 1
	}))
	defer p.Close()
	p.Update(func(b *Builder) { b.SetCatalogued() })
	p.Flush()
	if got := flaky.Get(p.Current()); got != 1 {
		t.Fatalf("index before = %d, want 1", got)
	}
	broken = true
	p.Update(func(b *Builder) { b.SetGroups([]Group{{ID: "g", Members: []RepoKey{"acme/a"}}}) })
	p.Flush()
	cur := p.Current()
	if len(cur.Groups) != 1 {
		t.Fatal("the batch of a panicking index was dropped")
	}
	if got := flaky.Get(cur); got != 1 {
		t.Errorf("index kept = %d, want its last good value 1", got)
	}
	broken = false
	p.Update(func(b *Builder) {
		b.SetGroups([]Group{{ID: "g", Members: []RepoKey{"acme/a"}}, {ID: "h", Members: []RepoKey{"acme/a"}}})
	})
	p.Flush()
	if got := flaky.Get(p.Current()); got != 3 {
		t.Errorf("index after the fix = %d, want 3", got)
	}
}

// An index that panics with nothing before it to keep (the first build) fails the build: no half-built snapshot is served.
func TestPanickingFirstIndexKeepsTheOldSnapshot(t *testing.T) {
	const bad Index[int] = "bad"
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), bad.Build(func(*Snapshot) int { panic("boom") }))
	defer p.Close()
	p.Update(func(b *Builder) { b.SetCatalogued() })
	p.Flush()
	if got := p.Current(); !got.BuiltAt.IsZero() || got.Catalogued {
		t.Fatal("a snapshot whose first index panicked was published")
	}
}

// notes and noteCount are a test feature's facet and index.
const (
	notes     Facet[[]string] = "notes"
	noteCount Index[int]      = "notes"
)

func countNotes(s *Snapshot) int {
	n := 0
	for _, e := range s.Repos {
		v, _ := notes.Get(e)
		n += len(v)
	}
	return n
}

func TestFacetsAndIndexes(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), noteCount.Build(countNotes))
	a := Repo{ID: RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "a"}
	b := Repo{ID: RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "b"}
	fresh := Freshness{State: StateOK}
	p.Update(func(bd *Builder) {
		bd.ReplaceSource("gh", []*RepoEntry{{Repo: a}, {Repo: b}}, nil, SourceStatus{ID: "gh"})
		bd.UpdateRepo("acme/a", func(e *RepoEntry) { notes.Set(e, []string{"x", "y"}, fresh) })
		bd.UpdateRepo("acme/b", func(e *RepoEntry) { notes.Set(e, []string{"z"}, fresh) })
		if bd.UpdateRepo("acme/missing", func(*RepoEntry) {}) {
			t.Error("UpdateRepo of an unknown key reported true")
		}
	})
	p.Flush()
	s := p.Current()
	if got := noteCount.Get(s); got != 3 {
		t.Errorf("index = %d, want 3", got)
	}
	if v, f := notes.Get(s.Repos["acme/a"]); len(v) != 2 || f.State != StateOK {
		t.Errorf("facet = %v %+v", v, f)
	}
	for name, f := range s.Repos["acme/b"].Facets() {
		if name != "notes" || f.State != StateOK {
			t.Errorf("Facets yields %s %+v", name, f)
		}
	}
	const other Facet[int] = "other"
	if v, f := other.Get(s.Repos["acme/a"]); v != 0 || f.Derive(time.Now()) != StatusUnknown {
		t.Errorf("an absent facet = %d %+v", v, f)
	}

	// A new listing keeps the facets of repositories that stay, and drops those of a repository whose identity changed.
	b2 := b
	b2.ID.GitHubID = 3
	p.Update(func(bd *Builder) {
		bd.ReplaceSource("gh", []*RepoEntry{{Repo: a}, {Repo: b2}}, nil, SourceStatus{ID: "gh"})
	})
	p.Flush()
	s2 := p.Current()
	va, _ := notes.Get(s2.Repos["acme/a"])
	vb, _ := notes.Get(s2.Repos["acme/b"])
	if len(va) != 2 || len(vb) != 0 || noteCount.Get(s2) != 2 {
		t.Errorf("after relisting: a %v, b %v, index %d", va, vb, noteCount.Get(s2))
	}
	if v, _ := notes.Get(s.Repos["acme/b"]); len(v) != 1 {
		t.Error("the old snapshot changed")
	}
}

func TestPublicView(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), noteCount.Build(countNotes))
	pub := repo("gh", 1, "acme", "open")
	pub.Repo.Visibility = "public"
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{pub}, nil, SourceStatus{ID: "gh", Repos: 1})
		b.SetGroups([]Group{{ID: "all", DefaultRepo: "acme/open", Members: []RepoKey{"acme/open"}}})
	})
	p.Flush()
	if s := p.Current(); s.Public() != s {
		t.Error("a snapshot without private repositories is not its own public view")
	}

	secret := repo("gh", 2, "acme", "secret")
	secret.Repo.Visibility = "private"
	inner := repo("gh", 3, "acme", "inner")
	inner.Repo.Visibility = "internal"
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{pub, secret, inner}, map[RepoKey]RepoKey{"acme/was-secret": "acme/secret", "acme/was-open": "acme/open"},
			SourceStatus{ID: "gh", Repos: 3})
		b.SetGroups([]Group{{ID: "all", DefaultRepo: "acme/secret", Members: []RepoKey{"acme/inner", "acme/open", "acme/secret"}}})
		b.UpdateRepo("acme/open", func(e *RepoEntry) { notes.Set(e, []string{"o"}, Freshness{}) })
		b.UpdateRepo("acme/secret", func(e *RepoEntry) { notes.Set(e, []string{"s"}, Freshness{}) })
		b.Mark("notes/acme/open", "acme/open", 5, 5)
		b.Mark("notes/acme/secret", "acme/secret", 7, 7)
	})
	p.Flush()
	full := p.Current()
	v := full.Public()
	if len(full.Repos) != 3 || noteCount.Get(full) != 2 {
		t.Fatalf("full view: %d repos, index %d", len(full.Repos), noteCount.Get(full))
	}
	g, _ := v.Group("all")
	if len(v.Repos) != 1 || v.Repos["acme/open"] == nil || len(v.Aliases) != 1 || v.Aliases["acme/was-open"] != "acme/open" ||
		!slices.Equal(g.Members, []RepoKey{"acme/open"}) || g.DefaultRepo != "" || !g.DefaultMissing || v.Sources[0].Repos != 1 {
		t.Errorf("public view: repos %v, aliases %v, group %+v, sources %+v", v.Repos, v.Aliases, g, v.Sources)
	}
	if noteCount.Get(v) != 1 {
		t.Errorf("the public view's index counts %d notes, want its own 1", noteCount.Get(v))
	}
	if e, _ := v.Repo("acme/was-secret"); e != nil || v.Public() != v {
		t.Error("an alias to a private repository resolves, or the view is not its own public view")
	}
	if g, _ := full.Group("all"); len(g.Members) != 3 {
		t.Errorf("the full view changed: %+v", g)
	}

	// A private item moves the full version only.
	p.Update(func(b *Builder) { b.Mark("notes/acme/secret", "acme/secret", 8, 8) })
	p.Flush()
	if next := p.Current(); next.Version == full.Version || next.Public().Version != v.Version {
		t.Errorf("after a private change: full %d (was %d), public %d (was %d)", next.Version, full.Version, next.Public().Version, v.Version)
	}
	p.Update(func(b *Builder) { b.Mark("notes/acme/open", "acme/open", 6, 6) })
	p.Flush()
	if next := p.Current(); next.Public().Version == v.Version {
		t.Error("a public change kept the public version")
	}
}

func TestDigestFollowsContentNotChecks(t *testing.T) {
	p := NewPublisher(time.Microsecond, time.Microsecond, slog.New(slog.DiscardHandler))
	defer p.Close()
	open, secret := repo("gh", 1, "acme", "open"), repo("gh", 2, "acme", "secret")
	secret.Repo.Visibility = "private"
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{open, secret}, nil, SourceStatus{ID: "gh", Repos: 2})
		b.Mark("docs/acme/open", "acme/open", 1, 10)
		b.Mark("commits/acme/open", "acme/open", 2, 20)
		b.Mark("docs/acme/secret", "acme/secret", 3, 30)
		b.Mark("listing/gh", "", 4, 40)
	})
	p.Flush()
	s := p.Current()
	if s.Digest("acme/open") != 30 || s.Digest("acme/secret") != 30 || s.Digest("acme/none") != 0 {
		t.Fatalf("digests: open %d, secret %d, none %d", s.Digest("acme/open"), s.Digest("acme/secret"), s.Digest("acme/none"))
	}
	if s.Public().Digest("acme/open") != 30 {
		t.Errorf("the public view's digest of a public repository: %d", s.Public().Digest("acme/open"))
	}
	p.Update(func(b *Builder) { b.Mark("docs/acme/open", "acme/open", 5, 10) }) // checked again, nothing new
	p.Flush()
	if next := p.Current(); next.Digest("acme/open") != 30 || next.Version == s.Version {
		t.Errorf("a check alone: digest %d, version moved %v", next.Digest("acme/open"), next.Version != s.Version)
	}
	p.Update(func(b *Builder) { b.Mark("docs/acme/open", "acme/open", 6, 11); b.Mark("commits/acme/open", "", 0, 0) })
	p.Flush()
	if next := p.Current(); next.Digest("acme/open") != 11 {
		t.Errorf("after a change and a removal: %d, want 11", next.Digest("acme/open"))
	}
}

func TestConcurrentPublishesKeepEveryUpdate(t *testing.T) {
	p := NewPublisher(time.Microsecond, time.Microsecond, slog.New(slog.DiscardHandler))
	defer p.Close()
	const n = 64
	entries := make([]*RepoEntry, n)
	for i := range entries {
		entries[i] = &RepoEntry{Repo: Repo{ID: RepoID{Source: "gh", GitHubID: int64(i + 1)}, Owner: "acme", Name: fmt.Sprint("r", i)}}
	}
	p.Update(func(b *Builder) { b.ReplaceSource("gh", entries, nil, SourceStatus{ID: "gh"}) })
	p.Flush()
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			p.Update(func(b *Builder) {
				b.UpdateRepo(RepoKey(fmt.Sprint("acme/r", i)), func(e *RepoEntry) { notes.Set(e, []string{fmt.Sprint(i)}, Freshness{}) })
			})
			p.Flush() // races the debounce timer's publish
		})
	}
	wg.Wait()
	p.Flush()
	for k, e := range p.Current().Repos {
		if v, _ := notes.Get(e); len(v) != 1 {
			t.Errorf("%s lost its update", k)
		}
	}
}

// TestAddressOwnedByOneSource: two sources that list one address leave it to the source whose id sorts first, whichever published
// first, and a later listing of either does not change that (the store and the engine apply the same rule).
func TestAddressOwnedByOneSource(t *testing.T) {
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}, {"b", "a", "b", "a"}} {
		p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
		for _, src := range order {
			id := map[string]int64{"a": 1, "b": 7}[src]
			p.Update(func(b *Builder) {
				b.ReplaceSource(src, []*RepoEntry{repo(src, id, "acme", "core"), repo(src, id+100, "acme", src+"-only")}, nil, SourceStatus{ID: src})
				b.UpdateRepo("acme/core", func(e *RepoEntry) { notes.Set(e, []string{"from " + src}, Freshness{}) })
			})
			p.Flush()
		}
		s := p.Current()
		if got := s.Repos["acme/core"]; got == nil || got.Repo.ID.Source != "a" {
			t.Errorf("order %v: acme/core is %+v, want source a's", order, got)
		}
		if s.Repos["acme/a-only"] == nil || s.Repos["acme/b-only"] == nil {
			t.Errorf("order %v: a source lost its other repositories: %v", order, s.Repos)
		}
		p.Close()
	}
}

// A snapshot has four views, by private repositories and by hidden forks; a view that would hold what another holds is that view.
func TestViewsByPrivateAndByHiddenForks(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), noteCount.Build(countNotes))
	pub := repo("gh", 1, "acme", "open")
	pub.Repo.Visibility = "public"
	fork := repo("gh", 2, "acme", "copy")
	fork.Repo.Visibility, fork.Repo.Fork = "public", true
	pinned := repo("gh", 3, "acme", "main-copy")
	pinned.Repo.Visibility, pinned.Repo.Fork = "public", true
	secret := repo("gh", 4, "acme", "secret")
	secret.Repo.Visibility = "private"
	secretFork := repo("gh", 5, "acme", "secret-copy")
	secretFork.Repo.Visibility, secretFork.Repo.Fork = "private", true

	all := []RepoKey{"acme/copy", "acme/main-copy", "acme/open", "acme/secret", "acme/secret-copy"}
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{pub}, nil, SourceStatus{ID: "gh", Repos: 1})
		b.SetGroups([]Group{{ID: "all", DefaultRepo: "acme/open", Members: []RepoKey{"acme/open"}}})
	})
	p.Flush()
	s := p.Current()
	if s.View(true, true) != s || s.View(false, false) != s || s.View(true, false) != s || s.HiddenForks() != 0 {
		t.Error("a snapshot with neither private repositories nor hidden forks is every one of its views")
	}

	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{pub, fork, pinned, secret, secretFork}, nil, SourceStatus{ID: "gh", Repos: 5})
		b.SetGroups([]Group{{ID: "all", DefaultRepo: "acme/main-copy", Members: all}})
		b.SetHidden(map[RepoKey]bool{"acme/copy": true, "acme/secret-copy": true})
	})
	p.Flush()
	s = p.Current()
	for _, tc := range []struct {
		name            string
		private, forks  bool
		repos           []RepoKey
		hiddenForks     int
		defaultRepoKept bool
	}{
		{"everything", true, true, all, 2, true},
		{"private, forks hidden", true, false, []RepoKey{"acme/main-copy", "acme/open", "acme/secret"}, 2, true},
		{"public, forks shown", false, true, []RepoKey{"acme/copy", "acme/main-copy", "acme/open"}, 1, true},
		{"public, forks hidden", false, false, []RepoKey{"acme/main-copy", "acme/open"}, 1, true},
	} {
		v := s.View(tc.private, tc.forks)
		got := slices.Sorted(maps.Keys(v.Repos))
		g, _ := v.Group("all")
		if !slices.Equal(got, tc.repos) || !slices.Equal(g.Members, tc.repos) || v.HiddenForks() != tc.hiddenForks ||
			(g.DefaultRepo == "acme/main-copy") != tc.defaultRepoKept || v.Sources[0].Repos != len(tc.repos) {
			t.Errorf("%s: repos %v, members %v, hidden forks %d, default %q, source count %d", tc.name, got, g.Members, v.HiddenForks(), g.DefaultRepo, v.Sources[0].Repos)
		}
		if noteCount.Get(v) != 0 || v.Hidden("acme/copy") != true {
			t.Errorf("%s: index %d, hidden(copy) %v", tc.name, noteCount.Get(v), v.Hidden("acme/copy"))
		}
	}
	if s.Public().Public() != s.Public() || s.View(true, false).View(true, false) != s.View(true, false) {
		t.Error("a view is not its own sibling of its kind")
	}
	// A hidden fork's item moves the versions of the views that hold it, and of no other.
	p.Update(func(b *Builder) { b.Mark("notes/acme/copy", "acme/copy", 9, 9) })
	p.Flush()
	next := p.Current()
	if next.View(true, true).Version == s.View(true, true).Version || next.View(false, true).Version == s.View(false, true).Version {
		t.Error("a fork's change did not move the views that show it")
	}
	if next.View(true, false).Version != s.View(true, false).Version || next.View(false, false).Version != s.View(false, false).Version {
		t.Error("a hidden fork's change moved the views that leave it out")
	}
}

func TestRestrict(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), noteCount.Build(countNotes))
	mine := repo("gh", 1, "acme", "mine")
	mine.Repo.Visibility = "private"
	theirs := repo("gh", 2, "acme", "theirs")
	theirs.Repo.Visibility = "private"
	inner := repo("gh", 3, "acme", "inner")
	inner.Repo.Visibility = "internal"
	copyOf := repo("gh", 4, "acme", "copy")
	copyOf.Repo.Visibility, copyOf.Repo.Fork = "internal", true
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{mine, theirs, inner, copyOf}, map[RepoKey]RepoKey{"acme/was-theirs": "acme/theirs"},
			SourceStatus{ID: "gh", Repos: 4})
		b.SetGroups([]Group{{ID: "ours", DefaultRepo: "acme/mine", Members: []RepoKey{"acme/copy", "acme/inner", "acme/mine"}},
			{ID: "secret", DefaultRepo: "acme/theirs", Members: []RepoKey{"acme/theirs"}}})
		b.SetHidden(map[RepoKey]bool{"acme/copy": true})
		b.UpdateRepo("acme/mine", func(e *RepoEntry) { notes.Set(e, []string{"m"}, Freshness{}) })
		b.UpdateRepo("acme/theirs", func(e *RepoEntry) { notes.Set(e, []string{"t"}, Freshness{}) })
	})
	p.Flush()
	s := p.Current()
	log := slog.New(slog.DiscardHandler)

	for _, allowed := range []map[RepoKey]bool{nil, {}, {"acme/elsewhere": true}} {
		v := s.Restrict(allowed, true, log)
		if len(v.Repos) != 0 || len(v.Groups) != 0 || len(v.Aliases) != 0 || noteCount.Get(v) != 0 || v.Sources[0].Repos != 0 {
			t.Errorf("Restrict(%v): repos %v, groups %v, aliases %v, index %d", allowed, v.Repos, v.Groups, v.Aliases, noteCount.Get(v))
		}
	}

	set := map[RepoKey]bool{"acme/mine": true, "acme/inner": true, "acme/copy": true}
	fat, lean := s.Restrict(set, true, log), s.Restrict(set, false, log)
	if got := slices.Sorted(maps.Keys(fat.Repos)); !slices.Equal(got, []RepoKey{"acme/copy", "acme/inner", "acme/mine"}) {
		t.Errorf("with forks: %v", got)
	}
	if got := slices.Sorted(maps.Keys(lean.Repos)); !slices.Equal(got, []RepoKey{"acme/inner", "acme/mine"}) {
		t.Errorf("without forks: %v", got)
	}
	for _, v := range []*Snapshot{fat, lean} {
		if _, ok := v.Group("secret"); ok || len(v.Groups) != 1 || noteCount.Get(v) != 1 || v.HiddenForks() != 1 {
			t.Errorf("groups %v, index %d, hidden forks %d", v.Groups, noteCount.Get(v), v.HiddenForks())
		}
		if e, _ := v.Repo("acme/was-theirs"); e != nil || v.Public() != v || v.View(false, true).Repos["acme/theirs"] != nil {
			t.Error("an alias to another's repository resolves, or the view's public view is not its own")
		}
	}
	if all := s.Restrict(map[RepoKey]bool{"acme/mine": true, "acme/theirs": true, "acme/inner": true, "acme/copy": true}, true, log); all != s {
		t.Error("a set of every repository is not the full snapshot")
	}
	if len(s.Repos) != 4 || len(s.Groups) != 2 {
		t.Error("restricting changed the snapshot")
	}
}

// A retired member leaves its group's Members for its Retired at every publish, still belongs to the group, comes back when it is no
// longer retired, and keeps its group in a restricted view that holds only it.
func TestRetiredMembers(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	defer p.Close()
	p.Retire(func(e *RepoEntry) bool { return e.Repo.Archived })
	live, old := repo("gh", 1, "acme", "live"), repo("gh", 2, "acme", "old")
	old.Repo.Archived = true
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{live, old}, nil, SourceStatus{ID: "gh", Repos: 2})
		b.SetGroups([]Group{{ID: "all", Members: []RepoKey{"acme/live", "acme/old"}}})
	})
	p.Flush()
	s := p.Current()
	g, _ := s.Group("all")
	if !slices.Equal(g.Members, []RepoKey{"acme/live"}) || !slices.Equal(g.Retired, []RepoKey{"acme/old"}) {
		t.Fatalf("group %+v", g)
	}
	if got := s.GroupsOf("acme/old"); !slices.Equal(got, []string{"all"}) {
		t.Errorf("a retired member's groups: %v", got)
	}
	r, _ := s.Restrict(map[RepoKey]bool{"acme/old": true}, true, slog.New(slog.DiscardHandler)).Group("all")
	if len(r.Members) != 0 || !slices.Equal(r.Retired, []RepoKey{"acme/old"}) {
		t.Errorf("restricted to the retired member: %+v", r)
	}

	back := repo("gh", 2, "acme", "old")
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{live, back}, nil, SourceStatus{ID: "gh", Repos: 2})
	})
	p.Flush()
	if g, _ := p.Current().Group("all"); !slices.Equal(g.Members, []RepoKey{"acme/live", "acme/old"}) || len(g.Retired) != 0 {
		t.Errorf("unarchived: %+v", g)
	}
}

// A group that holds no repository a view shows is not in the view: not listed, not counted, not found. The base keeps every group,
// so a group whose repositories come later, or whose private ones the next reader may see, is back when they do.
func TestGroupsWithoutVisibleRepositoriesAreHidden(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	open := repo("gh", 1, "acme", "open")
	open.Repo.Visibility = "public"
	secret := repo("gh", 2, "acme", "secret")
	secret.Repo.Visibility = "private"
	fork := repo("gh", 3, "acme", "fork")
	fork.Repo.Visibility = "public"
	groups := []Group{
		{ID: "mixed", Members: []RepoKey{"acme/open", "acme/secret"}},
		{ID: "private-only", Members: []RepoKey{"acme/secret"}},
		{ID: "forks-only", Members: []RepoKey{"acme/fork"}},
		{ID: "nothing"},
	}
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{open, secret, fork}, nil, SourceStatus{ID: "gh", Repos: 3})
		b.SetHidden(map[RepoKey]bool{"acme/fork": true})
		b.SetGroups(groups)
	})
	p.Flush()
	ids := func(s *Snapshot) []string {
		out := []string{}
		for _, g := range s.Groups {
			out = append(out, g.ID)
		}
		return out
	}
	for _, c := range []struct {
		name           string
		private, forks bool
		want           []string
	}{
		{"anonymous", false, false, []string{"mixed"}},
		{"anonymous with forks", false, true, []string{"mixed", "forks-only"}},
		{"signed in", true, false, []string{"mixed", "private-only"}},
		{"signed in with forks", true, true, []string{"mixed", "private-only", "forks-only"}},
	} {
		v := p.Current().View(c.private, c.forks)
		if got := ids(v); !slices.Equal(got, c.want) {
			t.Errorf("%s: groups %v, want %v", c.name, got, c.want)
		}
		if _, ok := v.Group("nothing"); ok {
			t.Errorf("%s: a group that matches nothing is found", c.name)
		}
		if _, ok := v.Group("private-only"); ok != c.private {
			t.Errorf("%s: private-only found = %v", c.name, ok)
		}
	}

	// The next batch starts from every group, not from the ones a view showed: a private-only group stays, and a group that gained
	// its first member shows.
	p.Update(func(b *Builder) { b.UpdateRepo("acme/open", func(e *RepoEntry) { e.Repo.Stars = 1 }) })
	p.Flush()
	if got := ids(p.Current().View(true, false)); !slices.Equal(got, []string{"mixed", "private-only"}) {
		t.Errorf("after an update: %v", got)
	}
	groups[3].Members = []RepoKey{"acme/open"}
	p.Update(func(b *Builder) { b.SetGroups(groups) })
	p.Flush()
	if got := ids(p.Current().View(false, false)); !slices.Equal(got, []string{"mixed", "nothing"}) {
		t.Errorf("a group that gained a member: %v", got)
	}

	// A group with only retired members is still a group (its repositories are in the Archive).
	p = NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	p.Retire(func(e *RepoEntry) bool { return e.Repo.Name == "open" })
	p.Update(func(b *Builder) {
		b.ReplaceSource("gh", []*RepoEntry{open}, nil, SourceStatus{ID: "gh", Repos: 1})
		b.SetGroups([]Group{{ID: "old", Members: []RepoKey{"acme/open"}}})
	})
	p.Flush()
	if g, ok := p.Current().Group("old"); !ok || len(g.Members) != 0 || len(g.Retired) != 1 {
		t.Errorf("a group of retired members: %+v, %v", g, ok)
	}
}
