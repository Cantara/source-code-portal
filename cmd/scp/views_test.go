package main

import (
	"context"
	"log/slog"
	gosync "sync"
	"testing"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
)

// Many requests at once for few views: each view is built once and shared, under the race detector.
func TestPeopleViewsAtOnce(t *testing.T) {
	s := &snapshot.Snapshot{Repos: map[snapshot.RepoKey]*snapshot.RepoEntry{}}
	for n, name := range []string{"a", "b", "c", "d"} {
		k := snapshot.KeyOf("acme", name)
		s.Repos[k] = &snapshot.RepoEntry{Repo: snapshot.Repo{ID: snapshot.RepoID{Source: "ghe", GitHubID: int64(n + 1)}, Owner: "acme", Name: name}}
	}
	sets := []map[snapshot.RepoID]bool{{{Source: "ghe", GitHubID: 1}: true}, {{Source: "ghe", GitHubID: 2}: true, {Source: "ghe", GitHubID: 3}: true}}
	type who struct{}
	pv := newPeopleViews(func() *snapshot.Snapshot { return s }, func(ctx context.Context) (string, map[snapshot.RepoID]bool) {
		n := ctx.Value(who{}).(int) % len(sets) //nolint:forcetypeassert // the test's own value
		return []string{"one", "two"}[n], sets[n]
	}, slog.New(slog.DiscardHandler))
	views := make([]*snapshot.Snapshot, 64)
	var wg gosync.WaitGroup
	for i := range views {
		wg.Go(func() { views[i] = pv.of(context.WithValue(t.Context(), who{}, i), i%4 >= 2) })
	}
	wg.Wait()
	for i, v := range views {
		if v != views[i%4] {
			t.Fatalf("request %d has a view of its own", i)
		}
	}
	if len(views[0].Repos) != 1 || len(views[1].Repos) != 2 {
		t.Errorf("views of %d and %d repositories", len(views[0].Repos), len(views[1].Repos))
	}
}
