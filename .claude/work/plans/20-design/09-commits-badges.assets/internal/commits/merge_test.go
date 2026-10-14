package commits

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

var base = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func sha(n int) string { return fmt.Sprintf("%040x", n) }

// randomSources builds k repositories with up to n commits each, with many equal timestamps and shared SHAs across
// repositories (forks) so that every tie-break rule is exercised.
func randomSources(rnd *rand.Rand, k, n int) []Source {
	srcs := make([]Source, k)
	for i := range srcs {
		cs := make([]scm.Commit, rnd.IntN(n+1))
		for j := range cs {
			cs[j] = scm.Commit{SHA: sha(rnd.IntN(40)), CommittedAt: base.Add(-time.Duration(rnd.IntN(20)) * time.Hour)}
		}
		// the ring invariant: deduplicated by SHA and sorted by compareWithin
		seen := map[string]bool{}
		cs = slices.DeleteFunc(cs, func(c scm.Commit) bool { d := seen[c.SHA]; seen[c.SHA] = true; return d })
		slices.SortFunc(cs, compareWithin)
		srcs[i] = Source{Slug: scm.Slug(fmt.Sprintf("repo-%02d", i)), Commits: cs}
	}
	return srcs
}

func naive(srcs []Source) []Entry {
	var all []Entry
	for i := range srcs {
		for j := range srcs[i].Commits {
			all = append(all, Entry{Slug: srcs[i].Slug, C: &srcs[i].Commits[j]})
		}
	}
	slices.SortFunc(all, func(a, b Entry) int { return Compare(a.Slug, *a.C, b.Slug, *b.C) })
	return all
}

func TestMergeEqualsNaiveSortPaged(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))
	for iter := 0; iter < 200; iter++ {
		srcs := randomSources(rnd, 1+rnd.IntN(30), 12)
		want := naive(srcs)
		limit := 1 + rnd.IntN(9)
		var got []Entry
		var pos *Position
		for pages := 0; ; pages++ {
			page, more := Merge(srcs, pos, limit, nil)
			got = append(got, page...)
			if !more {
				break
			}
			p := PositionOf(page[len(page)-1])
			pos = &p
			if pages > 10_000 {
				t.Fatal("no termination")
			}
		}
		if len(got) != len(want) {
			t.Fatalf("iter %d: got %d entries, want %d", iter, len(got), len(want))
		}
		for i := range got {
			if got[i].Slug != want[i].Slug || got[i].C.SHA != want[i].C.SHA || !got[i].C.CommittedAt.Equal(want[i].C.CommittedAt) {
				t.Fatalf("iter %d pos %d: got %s/%s want %s/%s", iter, i, got[i].Slug, got[i].C.SHA, want[i].Slug, want[i].C.SHA)
			}
		}
	}
}

func TestMergeCursorSurvivesInsertionAtTop(t *testing.T) {
	srcs := randomSources(rand.New(rand.NewPCG(3, 4)), 8, 10)
	page1, more := Merge(srcs, nil, 5, nil)
	if !more {
		t.Skip("too few commits")
	}
	p := PositionOf(page1[len(page1)-1])
	// a new commit arrives at the top of repo-00 between the two requests
	srcs[0].Commits = append([]scm.Commit{{SHA: sha(999), CommittedAt: base.Add(time.Hour)}}, srcs[0].Commits...)
	page2, _ := Merge(srcs, &p, 5, nil)
	for _, e := range page2 {
		for _, f := range page1 {
			if e.Slug == f.Slug && e.C.SHA == f.C.SHA {
				t.Fatalf("duplicate across pages: %s/%s", e.Slug, e.C.SHA)
			}
		}
		if e.C.SHA == sha(999) {
			t.Fatal("new top commit leaked into page 2")
		}
	}
}

func TestMergeLimitEdges(t *testing.T) {
	srcs := randomSources(rand.New(rand.NewPCG(5, 6)), 3, 4)
	total := len(naive(srcs))
	got, more := Merge(srcs, nil, total, nil)
	if len(got) != total || more {
		t.Fatalf("exact fit: %d entries more=%v, want %d false", len(got), more, total)
	}
	got, more = Merge(srcs, nil, total-1, nil)
	if len(got) != total-1 || !more {
		t.Fatalf("one short: %d entries more=%v", len(got), more)
	}
	if got, more = Merge(nil, nil, 5, nil); len(got) != 0 || more {
		t.Fatal("empty input")
	}
	if got, more = Merge(srcs, nil, 0, nil); len(got) != 0 || more {
		t.Fatal("zero limit")
	}
}

func FuzzMerge(f *testing.F) {
	f.Add(uint64(1), uint8(5), uint8(3))
	f.Fuzz(func(t *testing.T, seed uint64, k, limit uint8) {
		rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b9))
		srcs := randomSources(rnd, int(k%40), 10)
		want := naive(srcs)
		lim := int(limit%11) + 1
		var got []Entry
		var pos *Position
		for i := 0; i < len(want)+2; i++ {
			page, more := Merge(srcs, pos, lim, nil)
			got = append(got, page...)
			if !more {
				break
			}
			p := PositionOf(page[len(page)-1])
			pos = &p
		}
		if len(got) != len(want) {
			t.Fatalf("got %d want %d", len(got), len(want))
		}
	})
}

// BenchmarkCommitsPage is the GR-28 gate: 1,000 repositories with a full ring of 30, a deep cursor in the middle.
func BenchmarkCommitsPage(b *testing.B) {
	rnd := rand.New(rand.NewPCG(7, 8))
	srcs := make([]Source, 1000)
	for i := range srcs {
		cs := make([]scm.Commit, 30)
		for j := range cs {
			cs[j] = scm.Commit{SHA: sha(i*100 + j), CommittedAt: base.Add(-time.Duration(rnd.IntN(90*24*3600)) * time.Second)}
		}
		slices.SortFunc(cs, compareWithin)
		srcs[i] = Source{Slug: scm.Slug(fmt.Sprintf("repo-%04d", i)), Commits: cs}
	}
	all := naive(srcs)
	mid := PositionOf(all[len(all)/2])
	for _, tc := range []struct {
		name string
		pos  *Position
	}{{"first", nil}, {"deep", &mid}} {
		b.Run(tc.name, func(b *testing.B) {
			buf := make([]Entry, 0, 50)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf, _ = Merge(srcs, tc.pos, 50, buf)
			}
		})
	}
}
