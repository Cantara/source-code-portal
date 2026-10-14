package scp_test

// Differential tests between the two Store implementations. They live in cmd/scp (rank 6) because store/sqlite and
// store/memory are both rank 2 and may not import each other, not even from tests (architecture 5.1 rule 1, 5.2).

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/store/memory"
	"github.com/cantara/source-code-portal/internal/store/sqlite"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
)

func openSQLite(t *testing.T, ns map[string]int64) *sqlite.Store {
	t.Helper()
	c := sqlite.DefaultConfig()
	c.Path = filepath.Join(t.TempDir(), "scp.db")
	s, _, err := sqlite.Open(ctx, c, sqlite.Deps{CacheNamespaces: ns, BuildID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(ctx) })
	return s
}

func mkRepo(i int) store.Repo {
	return store.Repo{Key: store.RepoKey{Source: "src", Owner: "acme", Name: fmt.Sprintf("repo-%02d", i)}, GitHubID: int64(1000 + i), Slug: fmt.Sprintf("repo-%02d", i), Meta: []byte(`{}`), SeenAt: t0}
}

func dump(s store.Store, repos int) string {
	var lines []string
	cs, _ := s.LoadCatalogue(ctx)
	for _, r := range cs {
		lines = append(lines, fmt.Sprintf("repo %v gid=%d slug=%s seen=%d", r.Key, r.GitHubID, r.Slug, r.SeenAt.Unix()))
	}
	_ = s.LoadItems(ctx, func(it store.Item) error { // LoadItems order is unspecified by contract: sort the lines
		lines = append(lines, fmt.Sprintf("item %v|%s|%d|%d|%d|%s|%s|%s", it.Key, it.State, it.FetchedAt.Unix(), it.CheckedAt.Unix(), it.Attempts, it.ETag, it.Error, it.Payload))
		return nil
	})
	_ = s.LoadPlans(ctx, func(p store.PlanRow) error {
		lines = append(lines, fmt.Sprintf("plan %v %d %d", p.Key, p.NextDue.Unix(), p.UnchangedStreak))
		return nil
	})
	slices.Sort(lines)
	var sb strings.Builder
	sb.WriteString(strings.Join(lines, "\n") + "\n")
	for i := 0; i < repos; i++ {
		cm, _ := s.LoadCommits(ctx, mkRepo(i).Key, 100)
		for _, c := range cm {
			fmt.Fprintf(&sb, "c%d %s %d %s\n", i, c.SHA, c.CommittedAt.Unix(), c.Payload)
		}
	}
	tk, _ := s.LoadTasks(ctx)
	keys := make([]string, 0, len(tk))
	for k := range tk {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "task %s %v\n", k, tk[k].LastAttempt.Unix())
	}
	pk, _ := s.LoadParked(ctx)
	for _, p := range pk {
		fmt.Fprintf(&sb, "parked %s %s\n", p.Key, p.Reason)
	}
	return sb.String()
}

// TestDifferentialAgainstMemory: seeded random operation sequences, including invalid inputs and repository removal, must
// produce the same errors (class) and the same loads on both implementations.
func TestDifferentialAgainstMemory(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		rnd := rand.New(rand.NewPCG(seed, 7))
		ns := map[string]int64{"t.small": 1000}
		real, model := openSQLite(t, ns), memory.New(memory.Options{CacheNamespaces: ns})
		both := func(step int, f func(s store.Store) error) {
			e1, e2 := f(real), f(model)
			if (e1 == nil) != (e2 == nil) ||
				errors.Is(e1, store.ErrNoRepo) != errors.Is(e2, store.ErrNoRepo) ||
				errors.Is(e1, store.ErrInvalid) != errors.Is(e2, store.ErrInvalid) ||
				errors.Is(e1, store.ErrNoItem) != errors.Is(e2, store.ErrNoItem) ||
				errors.Is(e1, store.ErrTooLarge) != errors.Is(e2, store.ErrTooLarge) {
				t.Fatalf("seed %d step %d: sqlite=%v memory=%v", seed, step, e1, e2)
			}
		}
		catalogue := func(step int) {
			var cat []store.Repo
			for _, i := range rnd.Perm(5)[:1+rnd.IntN(4)] {
				r := mkRepo(i)
				if rnd.IntN(4) == 0 {
					r.GitHubID = 0 // unknown id on the incoming side
				}
				cat = append(cat, r)
			}
			var r1, r2 []store.RepoKey
			both(step, func(s store.Store) (err error) {
				if s == store.Store(real) {
					r1, err = s.ReplaceCatalogue(ctx, cat)
				} else {
					r2, err = s.ReplaceCatalogue(ctx, cat)
				}
				return
			})
			if fmt.Sprint(r1) != fmt.Sprint(r2) {
				t.Fatalf("seed %d step %d removed differ: %v vs %v", seed, step, r1, r2)
			}
		}
		catalogue(-1)
		for step := 0; step < 500; step++ {
			r := mkRepo(rnd.IntN(6)) // repos 4 and 5 may not exist
			at := t0.Add(time.Duration(rnd.IntN(50)) * time.Second)
			ik := store.ItemKey{Repo: r.Key, Kind: fmt.Sprintf("docs.k%d", rnd.IntN(3))}
			switch rnd.IntN(12) {
			case 0, 1:
				both(step, func(s store.Store) error {
					return s.PutItem(ctx, store.Item{Key: ik, State: "ok", FetchedAt: at, CheckedAt: at, ETag: "e", Payload: []byte(fmt.Sprint(step))})
				})
			case 2:
				both(step, func(s store.Store) error { return s.RecordFailure(ctx, ik, store.Failure{Code: "c", At: at}) })
			case 3:
				both(step, func(s store.Store) error { return s.Revalidated(ctx, ik, at) })
			case 4:
				sha, keep := fmt.Sprint(rnd.IntN(12)), 1+rnd.IntN(5)
				var payload []byte
				if rnd.IntN(6) != 0 {
					payload = []byte("p")
				} // else: empty payload, rejected by both
				both(step, func(s store.Store) error {
					return s.AppendCommits(ctx, r.Key, []store.CommitRow{{SHA: sha, CommittedAt: at, Payload: payload}}, keep)
				})
			case 5:
				k, sz := fmt.Sprint(rnd.IntN(15)), rnd.IntN(250)
				both(step, func(s store.Store) error { return s.PutCache(ctx, "t.small", k, make([]byte, sz)) })
			case 6:
				k := fmt.Sprint(rnd.IntN(15))
				_, ok1, _ := real.GetCache(ctx, "t.small", k)
				_, ok2, _ := model.GetCache(ctx, "t.small", k)
				if ok1 != ok2 {
					t.Fatalf("seed %d step %d: cache hit differs for %s: sqlite=%v memory=%v", seed, step, k, ok1, ok2)
				}
			case 7:
				streak := step%4 - rnd.IntN(2) // sometimes negative: rejected by both
				both(step, func(s store.Store) error {
					return s.SavePlans(ctx, []store.PlanRow{{Key: ik, NextDue: at, UnchangedStreak: streak}})
				})
			case 8:
				catalogue(step)
			case 9:
				name := fmt.Sprintf("task%d", rnd.IntN(3)) // draw once: both stores must see the same call
				both(step, func(s store.Store) error { return s.SaveTask(ctx, name, store.TaskState{LastAttempt: at}) })
			case 10:
				key := fmt.Sprintf("docs.k0/%s", r.Key)
				both(step, func(s store.Store) error { return s.Park(ctx, key, "boom", t0.Add(time.Hour)) })
			case 11:
				key := fmt.Sprintf("docs.k0/%s", r.Key)
				both(step, func(s store.Store) error { return s.Unpark(ctx, key) })
			}
		}
		a, b := dump(real, 6), dump(model, 6)
		// parked.since is stamped by each store's own clock: compare keys and reasons only (dump omits since)
		if a != b {
			t.Fatalf("seed %d diverged\n--- sqlite\n%s\n--- memory\n%s", seed, a, b)
		}
	}
}
