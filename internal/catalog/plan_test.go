package catalog

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func pub(id int64, name string) snapshot.Repo {
	return snapshot.Repo{ID: snapshot.RepoID{Source: "gh", GitHubID: id}, Owner: "acme", Name: name, Visibility: "public",
		HTMLURL: "https://github.com/acme/" + name}
}

func names(ts []Tracked) []string {
	var out []string
	for _, t := range ts {
		out = append(out, string(t.Repo.Key()))
	}
	return out
}

func TestPlan(t *testing.T) {
	all := Filter{IncludeArchived: true}
	cur := Plan([]snapshot.Repo{pub(1, "a"), pub(2, "b"), pub(3, "c")}, nil, all)

	// b is missed once: kept with one miss; missed again: removed.
	once := Plan([]snapshot.Repo{pub(1, "a"), pub(3, "c")}, cur, all)
	if got := names(once); !slices.Equal(got, []string{"acme/a", "acme/b", "acme/c"}) || once[1].Misses != 1 {
		t.Fatalf("after one miss = %v", once)
	}
	twice := Plan([]snapshot.Repo{pub(1, "a"), pub(3, "c")}, once, all)
	if got := names(twice); !slices.Equal(got, []string{"acme/a", "acme/c"}) {
		t.Errorf("after two misses = %v", got)
	}

	// A rename keeps the old address as an alias; renaming back drops it.
	renamed := Plan([]snapshot.Repo{pub(1, "a2"), pub(3, "c")}, twice, all)
	if renamed[0].Repo.Key() != "acme/a2" || !slices.Equal(renamed[0].Aliases, []snapshot.RepoKey{"acme/a"}) {
		t.Errorf("rename = %+v", renamed[0])
	}
	back := Plan([]snapshot.Repo{pub(1, "a"), pub(3, "c")}, renamed, all)
	if !slices.Equal(back[0].Aliases, []snapshot.RepoKey{"acme/a2"}) {
		t.Errorf("rename back: aliases = %v, want only acme/a2", back[0].Aliases)
	}

	// Turning private removes at once.
	private := pub(3, "c")
	private.Visibility = "private"
	if got := names(Plan([]snapshot.Repo{pub(1, "a"), private}, back, all)); !slices.Equal(got, []string{"acme/a"}) {
		t.Errorf("private = %v", got)
	}
}

func TestPlanFilters(t *testing.T) {
	fork, archived, bad := pub(2, "fork"), pub(3, "old"), pub(4, "..")
	fork.Fork, archived.Archived = true, true
	internal := pub(5, "internal")
	internal.Visibility = "internal"
	listed := []snapshot.Repo{pub(1, "a"), fork, archived, bad, internal, pub(6, "a")}
	if got := names(Plan(listed, nil, Filter{IncludeArchived: true})); !slices.Equal(got, []string{"acme/a", "acme/old"}) {
		t.Errorf("default filter = %v (an address taken twice keeps the lower id)", got)
	}
	if got := names(Plan(listed, nil, Filter{IncludeForks: true})); !slices.Equal(got, []string{"acme/a", "acme/fork"}) {
		t.Errorf("forks, no archived = %v", got)
	}
}

func TestPlanExclude(t *testing.T) {
	secret := pub(3, "vault")
	secret.Topics = []string{"confidential"}
	listed := []snapshot.Repo{pub(1, "core"), pub(2, "Client-Acme"), secret}
	f, err := SourceFilter(config.Source{ID: "gh", Exclude: []config.Match{{Glob: "client-*"}, {Topic: "confidential"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(Plan(listed, nil, f)); !slices.Equal(got, []string{"acme/core"}) {
		t.Errorf("excluded = %v", got)
	}
	// A tracked repository that becomes excluded leaves at the next listing.
	if got := names(Plan(listed, Plan(listed, nil, Filter{}), f)); !slices.Equal(got, []string{"acme/core"}) {
		t.Errorf("tracked then excluded = %v", got)
	}
	if _, err := SourceFilter(config.Source{ID: "gh", Exclude: []config.Match{{Regexp: "("}}}); err == nil {
		t.Error("a bad regexp is accepted")
	}
}

func TestPlanCleans(t *testing.T) {
	r := pub(1, "a")
	r.Description = "ok\u202e\x07 text"
	r.Homepage = "http://example.org"
	r.Topics = []string{"good", "Bad Topic"}
	got := Plan([]snapshot.Repo{r}, nil, Filter{})[0].Repo
	if got.Description != "ok text" || got.Homepage != "" || !slices.Equal(got.Topics, []string{"good"}) {
		t.Errorf("cleaned = %+v", got)
	}
}

// The result does not depend on listing order, and invariants hold over random worlds.
func TestPlanProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		var cur []Tracked
		for round := range 6 {
			var listed []snapshot.Repo
			for id := int64(1); id <= 12; id++ {
				if rng.IntN(4) == 0 {
					continue // missing from this listing
				}
				r := pub(id, string(rune('a'+rng.IntN(8)))) // names collide and change
				if rng.IntN(10) == 0 {
					r.Visibility = "private"
				}
				listed = append(listed, r)
				if rng.IntN(5) == 0 {
					listed = append(listed, r) // duplicate entry
				}
			}
			next := Plan(listed, cur, Filter{})
			shuffled := slices.Clone(listed)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			if diff := cmp.Diff(next, Plan(shuffled, cur, Filter{})); diff != "" {
				t.Fatalf("round %d depends on order:\n%s", round, diff)
			}
			addrs := map[snapshot.RepoKey]bool{}
			for _, tr := range next {
				k := tr.Repo.Key()
				if addrs[k] || tr.Repo.Visibility != "public" || len(tr.Aliases) > maxAliases || slices.Contains(tr.Aliases, k) ||
					tr.Misses >= missesToRemove {
					t.Fatalf("round %d broke an invariant: %+v", round, tr)
				}
				addrs[k] = true
			}
			cur = next
		}
	}
}
