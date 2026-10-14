package snapshot

import (
	"fmt"
	"io"
	"log/slog"
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
	ab := publish(func(b *Builder) { b.Mark("a", "", 1<<60|7); b.Mark("b", "", 9) })
	if ab == 0 || ab >= 1<<53 {
		t.Fatalf("version %d, want nonzero within 53 bits", ab)
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 10) }); v == ab {
		t.Error("a changed item kept the version")
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 9) }); v != ab {
		t.Errorf("the same items give %d, want %d", v, ab)
	}
	if v := publish(func(b *Builder) { b.Mark("b", "", 0) }); v == ab {
		t.Error("a dropped item kept the version")
	}
	q := NewPublisher(time.Hour, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer q.Close()
	q.Update(func(b *Builder) { b.Mark("b", "", 9) })
	q.Update(func(b *Builder) { b.Mark("a", "", 3); b.Mark("a", "", 1<<60|7) })
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

func TestPanickingIndexKeepsSnapshot(t *testing.T) {
	const bad Index[int] = "bad"
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler), bad.Build(func(*Snapshot) int { panic("boom") }))
	defer p.Close()
	p.Update(func(b *Builder) { b.SetCatalogued() })
	p.Flush()
	if got := p.Current(); !got.BuiltAt.IsZero() || got.Catalogued {
		t.Fatal("a snapshot whose index panicked was published")
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
		b.Mark("notes/acme/open", "acme/open", 5)
		b.Mark("notes/acme/secret", "acme/secret", 7)
	})
	p.Flush()
	full := p.Current()
	v := full.Public()
	if len(full.Repos) != 3 || noteCount.Get(full) != 2 {
		t.Fatalf("full view: %d repos, index %d", len(full.Repos), noteCount.Get(full))
	}
	g, _ := v.Group("all")
	if len(v.Repos) != 1 || v.Repos["acme/open"] == nil || len(v.Aliases) != 1 || v.Aliases["acme/was-open"] != "acme/open" ||
		!slices.Equal(g.Members, []RepoKey{"acme/open"}) || g.DefaultRepo != "" || g.DefaultMissing || v.Sources[0].Repos != 1 {
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
	p.Update(func(b *Builder) { b.Mark("notes/acme/secret", "acme/secret", 8) })
	p.Flush()
	if next := p.Current(); next.Version == full.Version || next.Public().Version != v.Version {
		t.Errorf("after a private change: full %d (was %d), public %d (was %d)", next.Version, full.Version, next.Public().Version, v.Version)
	}
	p.Update(func(b *Builder) { b.Mark("notes/acme/open", "acme/open", 6) })
	p.Flush()
	if next := p.Current(); next.Public().Version == v.Version {
		t.Error("a public change kept the public version")
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
