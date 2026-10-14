package catalog

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

// TestPlanInvariants drives 200 random reconcile passes (adds, renames, removals, visibility flips, duplicate listing entries,
// two owners whose names collide) and checks after every pass what must hold whatever the history.
func TestPlanInvariants(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		rnd := rand.New(rand.NewPCG(seed, seed*7919))
		world := map[int]scm.Repository{}
		for i := 1; i <= 30; i++ {
			world[i] = repo(i, fmt.Sprintf("r%d", i))
		}
		var cur []Tracked
		var held *Held
		now := t0
		for pass := 0; pass < 200; pass++ {
			now = now.Add(30 * time.Minute)
			switch rnd.IntN(8) {
			case 0:
				id := 1 + rnd.IntN(60)
				world[id] = repo(id, fmt.Sprintf("r%d", rnd.IntN(40)))
			case 1:
				delete(world, 1+rnd.IntN(60))
			case 2:
				if r, ok := world[1+rnd.IntN(60)]; ok {
					r.Visibility = scm.Visibility(rnd.IntN(3))
					world[int(r.Ref.ID)] = r
				}
			case 3:
				if r, ok := world[1+rnd.IntN(60)]; ok {
					r.Ref.Owner = "other-org"
					world[int(r.Ref.ID)] = r
				}
			}
			var listed []scm.Repository
			for _, r := range world {
				listed = append(listed, r)
				if rnd.IntN(10) == 0 {
					listed = append(listed, r) // duplicate entry
				}
			}
			rnd.Shuffle(len(listed), func(a, b int) { listed[a], listed[b] = listed[b], listed[a] })
			in := input(listed, cur, now)
			in.Held = held
			p := PlanReconcile(in)
			seen := map[string]bool{}
			for _, tr := range p.Next {
				f := FoldSlug(tr.Repo.Slug)
				if seen[f] {
					t.Fatalf("seed %d pass %d: duplicate slug %q", seed, pass, f)
				}
				seen[f] = true
				if _, err := MakeSlug("", tr.Repo.Ref.Name); err != nil {
					t.Fatalf("seed %d: illegal slug in catalogue: %v", seed, err)
				}
				if tr.Repo.Visibility != scm.VisPublic {
					t.Fatalf("seed %d pass %d: non-public repository %q in a public_only catalogue", seed, pass, tr.Repo.Slug)
				}
				for _, a := range tr.Aliases {
					if len(tr.Aliases) > MaxSlugAliasLen || FoldSlug(a) == f {
						t.Fatalf("seed %d: bad alias list %v for %q", seed, tr.Aliases, tr.Repo.Slug)
					}
				}
			}
			kept := map[RepoKey]bool{}
			for _, tr := range p.Next {
				kept[KeyOf(tr.Repo.Ref)] = true
			}
			for _, rm := range p.Removed {
				if kept[KeyOf(rm.Ref)] {
					t.Fatalf("seed %d: %v is both removed and kept", seed, rm.Ref)
				}
			}
			// order independence
			rnd.Shuffle(len(listed), func(a, b int) { listed[a], listed[b] = listed[b], listed[a] })
			in2 := input(listed, cur, now)
			in2.Held = held
			if p2 := PlanReconcile(in2); fmt.Sprint(slugs(p2)) != fmt.Sprint(slugs(p)) || len(p2.Removed) != len(p.Removed) {
				t.Fatalf("seed %d pass %d: plan depends on listing order", seed, pass)
			}
			cur, held = p.Next, p.Held
		}
	}
}
