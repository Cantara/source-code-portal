// Package storetest is the conformance suite every store.Store implementation must pass. Imported from _test.go files only.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

// Config is what the harness passes to its constructor.
type Config struct {
	// CacheNamespaces: NSSmall (cap 1000 bytes) and NSBig (cap 100000 bytes).
	CacheNamespaces map[string]int64
}

const (
	NSSmall = "t.small"
	NSBig   = "t.big"
)

func DefaultConfig() Config {
	return Config{CacheNamespaces: map[string]int64{NSSmall: 1000, NSBig: 100000}}
}

// Harness plugs an implementation into the suite.
type Harness struct {
	New func(t *testing.T, cfg Config) store.Store
	// Reopen closes s (Run already stopped) and opens the same durable state again; nil for non-durable implementations.
	Reopen func(t *testing.T, s store.Store) store.Store
	// Degrade forces the degraded state (tests that writes are refused and reads are not); nil to skip.
	Degrade func(t *testing.T, s store.Store)
}

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func rk(i int) store.RepoKey {
	return store.RepoKey{Source: "src", Owner: "acme", Name: fmt.Sprintf("repo-%02d", i)}
}
func mkRepo(i int) store.Repo {
	return store.Repo{Key: rk(i), GitHubID: int64(1000 + i), Slug: fmt.Sprintf("repo-%02d", i), Meta: []byte(`{"default_branch":"main"}`), SeenAt: t0}
}
func ik(i int, kind string) store.ItemKey { return store.ItemKey{Repo: rk(i), Kind: kind} }
func okItem(i int, kind, payload string, at time.Time) store.Item {
	return store.Item{Key: ik(i, kind), State: "ok", FetchedAt: at, CheckedAt: at, ETag: `W/"e"`, ContentSHA: "sha-" + payload, Payload: []byte(payload)}
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seed(t *testing.T, s store.Store, n int) {
	t.Helper()
	var rs []store.Repo
	for i := 0; i < n; i++ {
		rs = append(rs, mkRepo(i))
	}
	_, err := s.ReplaceCatalogue(context.Background(), rs)
	must(t, err)
}

func items(t *testing.T, s store.Store) map[store.ItemKey]store.Item {
	t.Helper()
	m := map[store.ItemKey]store.Item{}
	must(t, s.LoadItems(context.Background(), func(it store.Item) error { m[it.Key] = it; return nil }))
	return m
}

func isGone(err error) bool {
	var g interface{ Gone() bool }
	return errors.As(err, &g) && g.Gone()
}

// Run executes the whole suite.
func Run(t *testing.T, h Harness) {
	ctx := context.Background()
	fresh := func(t *testing.T) store.Store {
		t.Helper()
		s := h.New(t, DefaultConfig())
		t.Cleanup(func() { _ = s.Close(context.Background()) })
		return s
	}
	run := func(name string, f func(t *testing.T, s store.Store)) {
		t.Run(name, func(t *testing.T) { f(t, fresh(t)) })
	}

	// ---- catalogue
	run("Catalogue/RoundTripOrderedBySlug", func(t *testing.T, s store.Store) {
		rs := []store.Repo{mkRepo(2), mkRepo(0), mkRepo(1)}
		rs[0].SeenAt = t0.Add(1500 * time.Millisecond)
		removed, err := s.ReplaceCatalogue(ctx, rs)
		must(t, err)
		if len(removed) != 0 {
			t.Fatal(removed)
		}
		got, err := s.LoadCatalogue(ctx)
		must(t, err)
		if len(got) != 3 || got[0].Slug != "repo-00" || got[2].Slug != "repo-02" {
			t.Fatalf("%+v", got)
		}
		if got[2].SeenAt != t0.Add(time.Second) || string(got[0].Meta) != `{"default_branch":"main"}` || got[0].GitHubID != 1000 {
			t.Fatalf("seconds precision / meta / gid: %+v", got)
		}
	})
	run("Catalogue/EmptyReplaceRemovesAll", func(t *testing.T, s store.Store) {
		seed(t, s, 3)
		removed, err := s.ReplaceCatalogue(ctx, nil)
		must(t, err)
		if len(removed) != 3 {
			t.Fatal(removed)
		}
		got, _ := s.LoadCatalogue(ctx)
		if len(got) != 0 {
			t.Fatal(got)
		}
	})
	run("Catalogue/RemovalCascadesItemsCommitsAndParkedKeys", func(t *testing.T, s store.Store) {
		seed(t, s, 2)
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a", t0)))
		must(t, s.PutItem(ctx, okItem(1, "docs.refresh", "b", t0)))
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "c1", CommittedAt: t0, Payload: []byte("x")}}, 30))
		must(t, s.Park(ctx, "docs.refresh/"+rk(0).String(), "boom", t0.Add(time.Hour)))
		must(t, s.Park(ctx, "docs.refresh/"+rk(1).String(), "boom", t0.Add(time.Hour)))
		must(t, s.Park(ctx, "catalog.reconcile/_", "global", t0.Add(time.Hour)))
		removed, err := s.ReplaceCatalogue(ctx, []store.Repo{mkRepo(1)})
		must(t, err)
		if len(removed) != 1 || removed[0] != rk(0) {
			t.Fatal(removed)
		}
		if m := items(t, s); len(m) != 1 || m[ik(1, "docs.refresh")].Payload == nil {
			t.Fatalf("%v", m)
		}
		if cs, _ := s.LoadCommits(ctx, rk(0), 10); len(cs) != 0 {
			t.Fatal("commits survived")
		}
		ps, _ := s.LoadParked(ctx)
		if len(ps) != 2 || ps[0].Key != "catalog.reconcile/_" || !strings.HasSuffix(ps[1].Key, rk(1).String()) {
			t.Fatalf("parked %+v", ps)
		}
		// a late write for the removed repository is Gone
		err = s.PutItem(ctx, okItem(0, "docs.refresh", "late", t0))
		if !errors.Is(err, store.ErrNoRepo) || !isGone(err) {
			t.Fatalf("want ErrNoRepo+Gone, got %v", err)
		}
	})
	run("Catalogue/RenameByGitHubIDKeepsRows", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "keepme", t0)))
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "c1", CommittedAt: t0, Payload: []byte("x")}}, 30))
		r := mkRepo(0)
		r.Key.Name, r.Slug = "renamed", "renamed"
		removed, err := s.ReplaceCatalogue(ctx, []store.Repo{r})
		must(t, err)
		if len(removed) != 0 {
			t.Fatalf("rename must not remove: %v", removed)
		}
		m := items(t, s)
		it := m[store.ItemKey{Repo: r.Key, Kind: "docs.refresh"}]
		if string(it.Payload) != "keepme" {
			t.Fatalf("items did not follow the rename: %v", m)
		}
		if cs, _ := s.LoadCommits(ctx, r.Key, 5); len(cs) != 1 {
			t.Fatal("commits did not follow the rename")
		}
	})
	run("Catalogue/SwapNamesWithinOneTransaction", func(t *testing.T, s store.Store) {
		seed(t, s, 2)
		a, b := mkRepo(0), mkRepo(1)
		a.Key, a.Slug, b.Key, b.Slug = rk(1), "repo-01", rk(0), "repo-00" // gid 1000 now has name repo-01 and vice versa
		_, err := s.ReplaceCatalogue(ctx, []store.Repo{a, b})
		must(t, err)
		got, _ := s.LoadCatalogue(ctx)
		if len(got) != 2 || got[0].GitHubID != 1001 || got[1].GitHubID != 1000 {
			t.Fatalf("%+v", got)
		}
	})
	run("Catalogue/NameReusedByAnotherRepositoryIsANewRow", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "old", t0)))
		n := mkRepo(0)
		n.GitHubID = 9999 // same name, different repository
		removed, err := s.ReplaceCatalogue(ctx, []store.Repo{n})
		must(t, err)
		if len(removed) != 0 && len(removed) != 1 {
			t.Fatal(removed)
		}
		if m := items(t, s); len(m) != 0 {
			t.Fatalf("old repository's items must not be inherited: %v", m)
		}
	})
	run("Catalogue/ValidationRejectsWholeCall", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		bad := mkRepo(5)
		dup := mkRepo(6)
		dup.Slug = bad.Slug
		for name, rs := range map[string][]store.Repo{
			"duplicate slug": {bad, dup}, "duplicate key": {mkRepo(0), mkRepo(0)},
			"slash in name": {{Key: store.RepoKey{Source: "s", Owner: "o", Name: "a/b"}, Slug: "x", Meta: []byte("{}"), SeenAt: t0}},
			"empty meta":    {{Key: rk(7), Slug: "x7", SeenAt: t0}},
		} {
			if _, err := s.ReplaceCatalogue(ctx, rs); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if got, _ := s.LoadCatalogue(ctx); len(got) != 1 {
			t.Fatal("a rejected call must leave the catalogue untouched")
		}
	})

	// ---- items
	run("Catalogue/MixedCaseKeyRejected", func(t *testing.T, s store.Store) { // V-01: keys are ASCII-folded like scm.RepoRef.Key
		r := mkRepo(0)
		r.Key.Owner = "Acme"
		if _, err := s.ReplaceCatalogue(ctx, []store.Repo{r}); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})
	run("Catalogue/InvalidMetaRejectedBeforeAnyWrite", func(t *testing.T, s store.Store) { // V-04: same inputs rejected by every implementation
		seed(t, s, 1)
		for _, meta := range []string{`not json`, `[]`, `null`, `1`, ``} {
			r := mkRepo(0)
			r.Meta = []byte(meta)
			if _, err := s.ReplaceCatalogue(ctx, []store.Repo{r}); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("meta %q: want ErrInvalid, got %v", meta, err)
			}
		}
		if got, _ := s.LoadCatalogue(ctx); len(got) != 1 {
			t.Fatal(got)
		}
	})
	run("Catalogue/UnknownIncomingGitHubIDKeepsKnownOne", func(t *testing.T, s store.Store) { // V-13
		seed(t, s, 1)
		r := mkRepo(0)
		r.GitHubID = 0
		_, err := s.ReplaceCatalogue(ctx, []store.Repo{r})
		must(t, err)
		if got, _ := s.LoadCatalogue(ctx); len(got) != 1 || got[0].GitHubID != 1000 {
			t.Fatalf("known GitHub id lost: %+v", got)
		}
	})
	run("Item/PutAndLoadRoundTrip", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		in := okItem(0, "docs.refresh", "payload-1", t0.Add(1900*time.Millisecond))
		must(t, s.PutItem(ctx, in))
		got := items(t, s)[in.Key]
		if got.State != "ok" || string(got.Payload) != "payload-1" || got.ETag != `W/"e"` || got.ContentSHA != "sha-payload-1" || got.Attempts != 0 || got.Error != "" {
			t.Fatalf("%+v", got)
		}
		if !got.FetchedAt.Equal(t0.Add(time.Second)) || !got.CheckedAt.Equal(t0.Add(time.Second)) || got.FetchedAt.Location() != time.UTC {
			t.Fatalf("times: %v %v", got.FetchedAt, got.CheckedAt)
		}
	})
	run("Item/NilPayloadStaysNil", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		it := okItem(0, "commits.refresh", "", t0)
		it.Payload = nil
		must(t, s.PutItem(ctx, it))
		if got := items(t, s)[it.Key]; got.Payload != nil {
			t.Fatalf("payload %v", got.Payload)
		}
	})
	run("Item/TypedStatesAreResults", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		for i, st := range []string{"not_found", "unsupported", "too_large", "no_content"} {
			it := okItem(0, fmt.Sprintf("docs.state%d", i), "", t0)
			it.State, it.Payload, it.ETag, it.ContentSHA = freshState(st), nil, "", ""
			must(t, s.PutItem(ctx, it))
			if got := items(t, s)[it.Key]; string(got.State) != st {
				t.Fatalf("%s: %+v", st, got)
			}
		}
	})
	run("Item/InvalidInputsNeverReachTheStore", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		bad := okItem(0, "docs.refresh", "x", t0)
		for name, mut := range map[string]func(*store.Item){
			"state error":   func(i *store.Item) { i.State = "error" },
			"state never":   func(i *store.Item) { i.State = "never" },
			"no fetched_at": func(i *store.Item) { i.FetchedAt = time.Time{} },
			"no checked_at": func(i *store.Item) { i.CheckedAt = time.Time{} },
			"kind":          func(i *store.Item) { i.Key.Kind = "Docs" },
			"kind no dot":   func(i *store.Item) { i.Key.Kind = "docsrefresh" },
			"owner slash":   func(i *store.Item) { i.Key.Repo.Owner = "a/b" },
			"payload > max": func(i *store.Item) { i.Payload = make([]byte, store.MaxPayload+1) },
		} {
			it := bad
			mut(&it)
			if err := s.PutItem(ctx, it); err == nil {
				t.Fatalf("%s accepted", name)
			}
		}
		if len(items(t, s)) != 0 {
			t.Fatal("rows written by rejected calls")
		}
	})
	run("Item/PutOverwritesAndResetsAttempts", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		k := ik(0, "docs.refresh")
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "v1", t0)))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_502", At: t0.Add(time.Minute)}))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_502", At: t0.Add(2 * time.Minute)}))
		if got := items(t, s)[k]; got.Attempts != 2 {
			t.Fatalf("%+v", got)
		}
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "v2", t0.Add(time.Hour))))
		got := items(t, s)[k]
		if got.Attempts != 0 || got.Error != "" || string(got.Payload) != "v2" {
			t.Fatalf("%+v", got)
		}
	})
	run("Item/StaleWhileError", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		k := ik(0, "docs.refresh")
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "good", t0)))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "rate_limited", Message: "m", At: t0.Add(time.Hour)}))
		got := items(t, s)[k]
		if string(got.Payload) != "good" || !got.FetchedAt.Equal(t0) || got.State != "ok" || got.Error != "rate_limited" || got.Attempts != 1 || !got.CheckedAt.Equal(t0.Add(time.Hour)) {
			t.Fatalf("failure must keep payload, fetched_at and the result state: %+v", got)
		}
	})
	run("Item/FailureWithoutGoodValueIsErrorState", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		k := ik(0, "docs.refresh")
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_500", At: t0}))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_500", At: t0.Add(time.Minute)}))
		got := items(t, s)[k]
		if got.State != "error" || got.Attempts != 2 || !got.FetchedAt.IsZero() || got.Payload != nil {
			t.Fatalf("%+v", got)
		}
		if err := s.Revalidated(ctx, k, t0); !errors.Is(err, store.ErrNoItem) {
			t.Fatalf("a never-succeeded item cannot be revalidated: %v", err)
		}
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "now", t0.Add(time.Hour)))) // recovery
		if got := items(t, s)[k]; got.State != "ok" || got.Attempts != 0 {
			t.Fatalf("%+v", got)
		}
	})
	run("Item/RevalidatedKeepsPayloadAndClearsFailure", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		k := ik(0, "docs.refresh")
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "body", t0)))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_502", At: t0.Add(time.Minute)}))
		must(t, s.Revalidated(ctx, k, t0.Add(time.Hour)))
		got := items(t, s)[k]
		if string(got.Payload) != "body" || !got.FetchedAt.Equal(t0.Add(time.Hour)) || !got.CheckedAt.Equal(t0.Add(time.Hour)) || got.Attempts != 0 || got.Error != "" || got.State != "ok" || got.ETag != `W/"e"` {
			t.Fatalf("%+v", got)
		}
	})
	run("Item/NonOKResultStateSurvivesFailureAndRevalidation", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		k := ik(0, "docs.refresh")
		it := okItem(0, "docs.refresh", "", t0)
		it.State, it.Payload = "too_large", nil
		must(t, s.PutItem(ctx, it))
		must(t, s.RecordFailure(ctx, k, store.Failure{Code: "http_502", At: t0.Add(time.Minute)}))
		must(t, s.Revalidated(ctx, k, t0.Add(time.Hour)))
		if got := items(t, s)[k]; got.State != "too_large" {
			t.Fatalf("result state lost: %+v", got)
		}
	})
	run("Item/UnknownRepoAndItem", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		for name, err := range map[string]error{
			"put":         s.PutItem(ctx, okItem(9, "docs.refresh", "x", t0)),
			"failure":     s.RecordFailure(ctx, ik(9, "docs.refresh"), store.Failure{Code: "x", At: t0}),
			"revalidated": s.Revalidated(ctx, ik(9, "docs.refresh"), t0),
			"commits":     s.AppendCommits(ctx, rk(9), []store.CommitRow{{SHA: "a", CommittedAt: t0, Payload: []byte("p")}}, 5),
		} {
			if !errors.Is(err, store.ErrNoRepo) || !isGone(err) {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if err := s.Revalidated(ctx, ik(0, "docs.refresh"), t0); !errors.Is(err, store.ErrNoItem) {
			t.Fatalf("missing item: %v", err)
		}
	})

	// ---- plans
	run("Plans/SaveLoadAndOwnership", func(t *testing.T, s store.Store) {
		seed(t, s, 2)
		k0, k1 := ik(0, "docs.refresh"), ik(1, "docs.refresh")
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a", t0)))
		must(t, s.PutItem(ctx, okItem(1, "docs.refresh", "b", t0)))
		var n int
		must(t, s.LoadPlans(ctx, func(store.PlanRow) error { n++; return nil }))
		if n != 0 {
			t.Fatal("PutItem must not create plan state")
		}
		must(t, s.SavePlans(ctx, []store.PlanRow{
			{Key: k0, NextDue: t0.Add(time.Hour), UnchangedStreak: 3},
			{Key: store.ItemKey{Kind: "catalog.reconcile"}, NextDue: t0},     // global kind: skipped by design
			{Key: ik(0, "commits.refresh"), NextDue: t0, UnchangedStreak: 1}, // item without a row: skipped
		}))
		var rows []store.PlanRow
		must(t, s.LoadPlans(ctx, func(p store.PlanRow) error { rows = append(rows, p); return nil }))
		if len(rows) != 1 || rows[0].Key != k0 || !rows[0].NextDue.Equal(t0.Add(time.Hour)) || rows[0].UnchangedStreak != 3 {
			t.Fatalf("%+v", rows)
		}
		// job writes never touch planner-owned columns
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a2", t0.Add(time.Minute))))
		must(t, s.RecordFailure(ctx, k0, store.Failure{Code: "x", At: t0}))
		must(t, s.Revalidated(ctx, k0, t0))
		rows = nil
		must(t, s.LoadPlans(ctx, func(p store.PlanRow) error { rows = append(rows, p); return nil }))
		if len(rows) != 1 || rows[0].UnchangedStreak != 3 || !rows[0].NextDue.Equal(t0.Add(time.Hour)) {
			t.Fatalf("job write changed plan: %+v", rows)
		}
		_ = k1
	})

	run("Plans/NegativeStreakRejected", func(t *testing.T, s store.Store) { // V-04
		seed(t, s, 1)
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a", t0)))
		if err := s.SavePlans(ctx, []store.PlanRow{{Key: ik(0, "docs.refresh"), NextDue: t0, UnchangedStreak: -1}}); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
	})

	// ---- commits
	run("Commits/AppendTrimOrderIdempotent", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		var cs []store.CommitRow
		for i := 0; i < 10; i++ {
			cs = append(cs, store.CommitRow{SHA: fmt.Sprintf("sha%02d", i), CommittedAt: t0.Add(time.Duration(i) * time.Minute), Payload: []byte{byte(i)}})
		}
		must(t, s.AppendCommits(ctx, rk(0), cs, 4))
		must(t, s.AppendCommits(ctx, rk(0), cs[:3], 4)) // re-delivery of old commits: ignored, then trimmed away
		got, err := s.LoadCommits(ctx, rk(0), 100)
		must(t, err)
		if len(got) != 4 || got[0].SHA != "sha09" || got[3].SHA != "sha06" {
			t.Fatalf("%+v", got)
		}
		if got2, _ := s.LoadCommits(ctx, rk(0), 2); len(got2) != 2 || got2[1].SHA != "sha08" {
			t.Fatalf("limit: %+v", got2)
		}
		if n, _ := s.LoadCommits(ctx, rk(0), 0); len(n) != 0 {
			t.Fatal("limit 0")
		}
	})
	run("Commits/EqualTimestampsOrderByInsertion", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "a", CommittedAt: t0, Payload: []byte("1")}, {SHA: "b", CommittedAt: t0, Payload: []byte("2")}}, 10))
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "c", CommittedAt: t0, Payload: []byte("3")}}, 10))
		got, _ := s.LoadCommits(ctx, rk(0), 10)
		if len(got) != 3 || got[0].SHA != "c" || got[1].SHA != "b" || got[2].SHA != "a" {
			t.Fatalf("%+v", got)
		}
	})
	run("Commits/ExistingSHAIsNotRewritten", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "a", CommittedAt: t0, Payload: []byte("first")}}, 10))
		must(t, s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "a", CommittedAt: t0, Payload: []byte("second")}}, 10))
		if got, _ := s.LoadCommits(ctx, rk(0), 10); len(got) != 1 || string(got[0].Payload) != "first" {
			t.Fatalf("%+v", got)
		}
	})
	run("Commits/InvalidKeep", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		for _, keep := range []int{0, -1} {
			if err := s.AppendCommits(ctx, rk(0), nil, keep); !errors.Is(err, store.ErrInvalid) {
				t.Fatalf("keep %d: %v", keep, err)
			}
		}
	})
	run("Commits/UnknownRepoLoadsEmpty", func(t *testing.T, s store.Store) {
		got, err := s.LoadCommits(ctx, rk(0), 5)
		if err != nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
	})

	run("Commits/EmptyPayloadRejectedAndCallIsAtomic", func(t *testing.T, s store.Store) { // V-03: INSERT OR IGNORE used to drop it silently
		seed(t, s, 1)
		err := s.AppendCommits(ctx, rk(0), []store.CommitRow{{SHA: "good", CommittedAt: t0, Payload: []byte("p")}, {SHA: "bad", CommittedAt: t0}}, 30)
		if !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("want ErrInvalid, got %v", err)
		}
		if cs, _ := s.LoadCommits(ctx, rk(0), 10); len(cs) != 0 {
			t.Fatalf("a rejected call must store nothing: %+v", cs)
		}
	})

	// ---- tasks and parked
	run("Tasks/RoundTrip", func(t *testing.T, s store.Store) {
		must(t, s.SaveTask(ctx, "planner.tick", store.TaskState{LastAttempt: t0, LastSuccess: t0.Add(-time.Hour), LastError: "x"}))
		must(t, s.SaveTask(ctx, "planner.tick", store.TaskState{LastAttempt: t0.Add(time.Minute)}))
		m, err := s.LoadTasks(ctx)
		must(t, err)
		got := m["planner.tick"]
		if len(m) != 1 || !got.LastAttempt.Equal(t0.Add(time.Minute)) || !got.LastSuccess.IsZero() || got.LastError != "" {
			t.Fatalf("%+v", m)
		}
	})
	run("Parked/ParkKeepsSinceUnparkDeletes", func(t *testing.T, s store.Store) {
		must(t, s.Park(ctx, "docs.refresh/src/acme/x", "first", t0.Add(time.Hour)))
		ps, _ := s.LoadParked(ctx)
		since := ps[0].Since
		must(t, s.Park(ctx, "docs.refresh/src/acme/x", "second", t0.Add(2*time.Hour)))
		ps, _ = s.LoadParked(ctx)
		if len(ps) != 1 || ps[0].Reason != "second" || !ps[0].Since.Equal(since) || !ps[0].RetryAt.Equal(t0.Add(2*time.Hour)) {
			t.Fatalf("%+v", ps)
		}
		must(t, s.Unpark(ctx, "docs.refresh/src/acme/x"))
		must(t, s.Unpark(ctx, "never-parked"))
		if ps, _ = s.LoadParked(ctx); len(ps) != 0 {
			t.Fatal(ps)
		}
	})

	// ---- cache
	run("Cache/PutGetMissOverwrite", func(t *testing.T, s store.Store) {
		if _, ok, err := s.GetCache(ctx, NSBig, "k"); ok || err != nil {
			t.Fatal(ok, err)
		}
		must(t, s.PutCache(ctx, NSBig, "k", []byte("v1")))
		must(t, s.PutCache(ctx, NSBig, "k", []byte("v22")))
		got, ok, err := s.GetCache(ctx, NSBig, "k")
		if err != nil || !ok || string(got) != "v22" {
			t.Fatal(string(got), ok, err)
		}
		got[0] = 'X' // the returned slice is the caller's
		if again, _, _ := s.GetCache(ctx, NSBig, "k"); string(again) != "v22" {
			t.Fatal("aliasing")
		}
	})
	run("Cache/BoundsAndNamespaces", func(t *testing.T, s store.Store) {
		if err := s.PutCache(ctx, "nope.ns", "k", []byte("x")); !errors.Is(err, store.ErrUnknownNamespace) {
			t.Fatal(err)
		}
		if _, _, err := s.GetCache(ctx, "nope.ns", "k"); !errors.Is(err, store.ErrUnknownNamespace) {
			t.Fatal(err)
		}
		if err := s.PutCache(ctx, NSSmall, "k", make([]byte, 1001)); !errors.Is(err, store.ErrTooLarge) {
			t.Fatal(err)
		}
		if err := s.PutCache(ctx, NSSmall, "", []byte("x")); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
		must(t, s.PutCache(ctx, NSSmall, "k", make([]byte, 1000))) // exactly the cap is allowed
	})
	run("Cache/LRUEvictsLeastRecentlyUsedToNinetyPercent", func(t *testing.T, s store.Store) {
		for i := 0; i < 10; i++ { // 10 x 100 bytes = exactly the 1000 cap
			must(t, s.PutCache(ctx, NSSmall, fmt.Sprintf("k%d", i), make([]byte, 100)))
		}
		for _, k := range []string{"k0", "k1"} { // k0 and k1 become the most recently used
			if _, ok, _ := s.GetCache(ctx, NSSmall, k); !ok {
				t.Fatal(k)
			}
		}
		must(t, s.PutCache(ctx, NSSmall, "new", make([]byte, 100))) // 1100 > 1000: evict down to <= 900: k2, k3 go
		var have []string
		for _, k := range []string{"k0", "k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9", "new"} {
			if _, ok, _ := s.GetCache(ctx, NSSmall, k); ok {
				have = append(have, k)
			}
		}
		if strings.Join(have, ",") != "k0,k1,k4,k5,k6,k7,k8,k9,new" {
			t.Fatalf("survivors: %v", have)
		}
	})
	run("Cache/NamespacesAreIndependent", func(t *testing.T, s store.Store) {
		for i := 0; i < 12; i++ {
			must(t, s.PutCache(ctx, NSSmall, fmt.Sprint(i), make([]byte, 100)))
		}
		must(t, s.PutCache(ctx, NSBig, "keep", []byte("x")))
		if _, ok, _ := s.GetCache(ctx, NSBig, "keep"); !ok {
			t.Fatal("eviction crossed namespaces")
		}
	})
	run("Cache/OverwriteAdjustsAccounting", func(t *testing.T, s store.Store) {
		for i := 0; i < 9; i++ {
			must(t, s.PutCache(ctx, NSSmall, fmt.Sprint(i), make([]byte, 100))) // 900
		}
		for i := 0; i < 20; i++ {
			must(t, s.PutCache(ctx, NSSmall, "0", make([]byte, 100))) // same key again and again: no growth, no eviction
		}
		for i := 0; i < 9; i++ {
			if _, ok, _ := s.GetCache(ctx, NSSmall, fmt.Sprint(i)); !ok {
				t.Fatalf("entry %d evicted by overwrites", i)
			}
		}
	})

	run("Cache/NilValueRoundTripsAsEmpty", func(t *testing.T, s store.Store) { // V-04
		must(t, s.PutCache(ctx, NSBig, "nil", nil))
		d, ok, err := s.GetCache(ctx, NSBig, "nil")
		must(t, err)
		if !ok || len(d) != 0 {
			t.Fatalf("ok=%v data=%q", ok, d)
		}
	})
	run("Cache/ConcurrentPutsStayWithinTheCap", func(t *testing.T, s store.Store) { // V-06
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < 150; i++ {
					_ = s.PutCache(ctx, NSBig, fmt.Sprintf("k%d-%d", g, i%40), make([]byte, 400+i%9))
				}
			}(g)
		}
		wg.Wait()
		var total int
		for g := 0; g < 8; g++ {
			for i := 0; i < 40; i++ {
				if d, ok, _ := s.GetCache(ctx, NSBig, fmt.Sprintf("k%d-%d", g, i)); ok {
					total += len(d)
				}
			}
		}
		if total > 100000 || total == 0 {
			t.Fatalf("resident bytes %d, cap 100000", total)
		}
	})

	// ---- lifecycle
	run("Lifecycle/RunReturnsOnCancelAndCloseIsIdempotent", func(t *testing.T, s store.Store) {
		c, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- s.Run(c) }()
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return")
		}
		must(t, s.Close(ctx))
		must(t, s.Close(ctx))
		if err := s.PutCache(ctx, NSBig, "k", []byte("x")); !errors.Is(err, store.ErrClosed) {
			t.Fatalf("after close: %v", err)
		}
		if _, err := s.LoadCatalogue(ctx); !errors.Is(err, store.ErrClosed) {
			t.Fatalf("after close read: %v", err)
		}
	})
	run("Lifecycle/CancelledContextIsNotAStoreFailure", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		c, cancel := context.WithCancel(ctx)
		cancel()
		if err := s.PutItem(c, okItem(0, "docs.refresh", "x", t0)); !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
		if !s.Health().OK {
			t.Fatal("a cancelled context must not degrade the store")
		}
	})
	run("Check/BackupSmoke", func(t *testing.T, s store.Store) {
		seed(t, s, 2)
		must(t, s.Check(ctx, false))
		must(t, s.Check(ctx, true))
		if !s.Health().OK {
			t.Fatal(s.Health())
		}
	})
	run("Reentrancy/CallbackThatCallsTheStoreDoesNotDeadlock", func(t *testing.T, s store.Store) {
		seed(t, s, 2)
		must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a", t0)))
		done := make(chan error, 1)
		go func() {
			done <- s.LoadItems(ctx, func(it store.Item) error { // forbidden by contract; implementations must not hang
				_ = s.PutCache(ctx, NSBig, "from-callback", []byte("x"))
				return nil
			})
		}()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("deadlock")
		}
	})
	run("Callbacks/ErrorAbortsLoad", func(t *testing.T, s store.Store) {
		seed(t, s, 1)
		for i := 0; i < 3; i++ {
			must(t, s.PutItem(ctx, okItem(0, fmt.Sprintf("docs.kind%d", i), "x", t0)))
		}
		boom := errors.New("boom")
		n := 0
		err := s.LoadItems(ctx, func(store.Item) error { n++; return boom })
		if !errors.Is(err, boom) || n != 1 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	run("Concurrency/ParallelWritersAndReaders", func(t *testing.T, s store.Store) {
		seed(t, s, 8)
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 25; i++ {
					at := t0.Add(time.Duration(i) * time.Second)
					if err := s.PutItem(ctx, okItem(g, "docs.refresh", fmt.Sprint(g, i), at)); err != nil {
						t.Error(err)
					}
					if err := s.AppendCommits(ctx, rk(g), []store.CommitRow{{SHA: fmt.Sprint(g, "-", i), CommittedAt: at, Payload: []byte("p")}}, 10); err != nil {
						t.Error(err)
					}
					if err := s.PutCache(ctx, NSBig, fmt.Sprint(g, i), []byte("c")); err != nil {
						t.Error(err)
					}
					if _, err := s.LoadCommits(ctx, rk(g), 5); err != nil {
						t.Error(err)
					}
				}
			}()
		}
		wg.Wait()
		m := items(t, s)
		for g := 0; g < 8; g++ {
			if string(m[ik(g, "docs.refresh")].Payload) != fmt.Sprint(g, 24) {
				t.Fatalf("g%d: %s", g, m[ik(g, "docs.refresh")].Payload)
			}
			if cs, _ := s.LoadCommits(ctx, rk(g), 100); len(cs) != 10 || cs[0].SHA != fmt.Sprint(g, "-", 24) {
				t.Fatalf("commits g%d: %d", g, len(cs))
			}
		}
	})

	// ---- degraded
	if h.Degrade != nil {
		run("Degraded/WritesRefusedReadsServed", func(t *testing.T, s store.Store) {
			seed(t, s, 1)
			must(t, s.PutItem(ctx, okItem(0, "docs.refresh", "a", t0)))
			h.Degrade(t, s)
			if s.Health().OK || s.Health().Reason == "" {
				t.Fatal("not degraded")
			}
			for name, err := range map[string]error{
				"put":     s.PutItem(ctx, okItem(0, "docs.refresh", "b", t0)),
				"failure": s.RecordFailure(ctx, ik(0, "docs.refresh"), store.Failure{Code: "x", At: t0}),
				"cache":   s.PutCache(ctx, NSBig, "k", []byte("x")),
				"plans":   s.SavePlans(ctx, nil),
				"catalog": func() error { _, err := s.ReplaceCatalogue(ctx, nil); return err }(),
			} {
				if !errors.Is(err, store.ErrDegraded) {
					t.Fatalf("%s: %v", name, err)
				}
			}
			if got := items(t, s)[ik(0, "docs.refresh")]; string(got.Payload) != "a" {
				t.Fatal("reads must still serve the last good state")
			}
		})
	}

	// ---- durability
	if h.Reopen != nil {
		t.Run("Durable/EverythingSurvivesReopen", func(t *testing.T) {
			s := fresh(t)
			seed(t, s, 3)
			must(t, s.PutItem(ctx, okItem(1, "docs.refresh", "persisted", t0)))
			must(t, s.RecordFailure(ctx, ik(2, "docs.refresh"), store.Failure{Code: "http_500", At: t0}))
			must(t, s.SavePlans(ctx, []store.PlanRow{{Key: ik(1, "docs.refresh"), NextDue: t0.Add(time.Hour), UnchangedStreak: 2}}))
			must(t, s.AppendCommits(ctx, rk(1), []store.CommitRow{{SHA: "c1", CommittedAt: t0, Payload: []byte("p")}}, 30))
			must(t, s.SaveTask(ctx, "tick", store.TaskState{LastAttempt: t0}))
			must(t, s.Park(ctx, "docs.refresh/src/acme/repo-01", "why", t0.Add(time.Hour)))
			must(t, s.PutCache(ctx, NSBig, "k", []byte("cached")))
			s2 := h.Reopen(t, s)
			t.Cleanup(func() { _ = s2.Close(ctx) })
			if cat, _ := s2.LoadCatalogue(ctx); len(cat) != 3 {
				t.Fatal(cat)
			}
			m := items(t, s2)
			if string(m[ik(1, "docs.refresh")].Payload) != "persisted" || m[ik(2, "docs.refresh")].State != "error" || m[ik(2, "docs.refresh")].Attempts != 1 {
				t.Fatalf("%+v", m)
			}
			var pr []store.PlanRow
			must(t, s2.LoadPlans(ctx, func(p store.PlanRow) error { pr = append(pr, p); return nil }))
			if len(pr) != 1 || pr[0].UnchangedStreak != 2 {
				t.Fatalf("%+v", pr)
			}
			if cs, _ := s2.LoadCommits(ctx, rk(1), 5); len(cs) != 1 {
				t.Fatal("commits")
			}
			if tm, _ := s2.LoadTasks(ctx); len(tm) != 1 {
				t.Fatal("tasks")
			}
			if ps, _ := s2.LoadParked(ctx); len(ps) != 1 {
				t.Fatal("parked")
			}
			if v, ok, _ := s2.GetCache(ctx, NSBig, "k"); !ok || string(v) != "cached" {
				t.Fatal("cache")
			}
		})
		t.Run("Durable/CacheAccountingSurvivesReopen", func(t *testing.T) {
			s := fresh(t)
			for i := 0; i < 9; i++ {
				must(t, s.PutCache(ctx, NSSmall, fmt.Sprint(i), make([]byte, 100)))
			}
			s2 := h.Reopen(t, s)
			t.Cleanup(func() { _ = s2.Close(ctx) })
			must(t, s2.PutCache(ctx, NSSmall, "x", make([]byte, 100))) // 1000: at the cap, no eviction
			must(t, s2.PutCache(ctx, NSSmall, "y", make([]byte, 100))) // 1100: evict to <= 900
			n := 0
			for _, k := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "x", "y"} {
				if _, ok, _ := s2.GetCache(ctx, NSSmall, k); ok {
					n++
				}
			}
			if n != 9 {
				t.Fatalf("want 9 survivors after reopen accounting, got %d", n)
			}
		})
	}
}
