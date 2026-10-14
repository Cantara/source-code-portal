package snapshot

import (
	"io"
	"log/slog"
	"strings"
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
		start := time.Now()
		for i := range 15 {
			p.Update(func(b *Builder) {
				b.ReplaceSource("gh", []*RepoEntry{repo("gh", int64(i), "o", "r")}, nil, SourceStatus{ID: "gh"})
			})
			time.Sleep(100 * time.Millisecond)
			synctest.Wait()
			if v := p.Current().Version; i < 9 && v != 0 {
				t.Fatalf("published after %v, before the max delay", time.Since(start))
			}
		}
		if v := p.Current().Version; v < 1 {
			t.Fatalf("version %d after 1.5 s of updates, want a publish at the max delay", v)
		}

		// A single update publishes after the quiet period.
		before := p.Current().Version
		time.Sleep(time.Second)
		synctest.Wait()
		v := p.Current().Version
		p.Update(func(b *Builder) { b.SetCatalogued() })
		time.Sleep(199 * time.Millisecond)
		synctest.Wait()
		if p.Current().Version != v {
			t.Fatal("published before the quiet period")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := p.Current(); got.Version != v+1 || !got.Catalogued || v < before {
			t.Fatalf("after quiet period: version %d catalogued %v, want %d true", got.Version, got.Catalogued, v+1)
		}
	})
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

	if _, ok := v1.Repos["cantara/old"]; !ok || len(v1.Repos) != 1 || v1.Version != 1 {
		t.Fatalf("v1 changed: %+v", v1.Repos)
	}
	if e, moved := v2.Repo("cantara/old"); e == nil || !moved || e.Repo.Name != "New" {
		t.Fatalf("v2 alias: entry %v moved %v", e, moved)
	}
	if got := v2.GroupsOf("cantara/new"); len(got) != 1 || got[0] != "all" {
		t.Fatalf("GroupsOf = %v", got)
	}
}

func TestPanicKeepsSnapshot(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer p.Close()
	p.Update(func(b *Builder) { b.SetCatalogued() })
	p.Update(func(*Builder) { panic("boom") })
	p.Flush()
	if got := p.Current(); got.Version != 0 || got.Catalogued {
		t.Fatalf("a panicking batch was published: version %d", got.Version)
	}
}

func TestFeedsAndUpdates(t *testing.T) {
	p := NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler))
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	a := Repo{ID: RepoID{Source: "gh", GitHubID: 1}, Owner: "acme", Name: "a"}
	b := Repo{ID: RepoID{Source: "gh", GitHubID: 2}, Owner: "acme", Name: "b"}
	p.Update(func(bd *Builder) {
		bd.ReplaceSource("gh", []*RepoEntry{{Repo: a}, {Repo: b}}, nil, SourceStatus{ID: "gh"})
		bd.SetGroups([]Group{{ID: "only-b", Members: []RepoKey{"acme/b"}}})
		bd.UpdateRepo("acme/a", func(e *RepoEntry) {
			e.Commits = []Commit{{SHA: "a2", CommittedAt: at}, {SHA: "a1", CommittedAt: at.Add(-2 * time.Hour)}}
		})
		bd.UpdateRepo("acme/b", func(e *RepoEntry) { e.Commits = []Commit{{SHA: "b1", CommittedAt: at.Add(-time.Hour)}} })
		if bd.UpdateRepo("acme/missing", func(*RepoEntry) {}) {
			t.Error("UpdateRepo of an unknown key reported true")
		}
	})
	p.Flush()
	s := p.Current()
	var shas []string
	for _, r := range s.Latest {
		shas = append(shas, r.Commit.SHA)
	}
	if strings.Join(shas, ",") != "a2,b1,a1" {
		t.Errorf("Latest = %v", shas)
	}
	if f, ok := s.Feed("only-b"); !ok || len(f) != 1 || f[0].Repo != "acme/b" {
		t.Errorf("group feed = %v", f)
	}

	// A new listing keeps the commits of repositories that stay, and drops those of a repository whose identity changed.
	b2 := b
	b2.ID.GitHubID = 3
	p.Update(func(bd *Builder) {
		bd.ReplaceSource("gh", []*RepoEntry{{Repo: a}, {Repo: b2}}, nil, SourceStatus{ID: "gh"})
	})
	p.Flush()
	s2 := p.Current()
	if len(s2.Repos["acme/a"].Commits) != 2 || len(s2.Repos["acme/b"].Commits) != 0 {
		t.Errorf("after relisting: a has %d commits, b %d", len(s2.Repos["acme/a"].Commits), len(s2.Repos["acme/b"].Commits))
	}
	if len(s.Repos["acme/b"].Commits) != 1 {
		t.Error("the old snapshot changed")
	}
}
