package catalog

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/scm"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func repo(id int, name string) scm.Repository {
	return scm.Repository{Ref: scm.RepoRef{Source: "acme", ID: scm.RepoID(id), Owner: "acme-org", Name: name}, DefaultBranch: "main", Visibility: scm.VisPublic}
}

func input(listed []scm.Repository, cur []Tracked, now time.Time) PlanInput {
	return PlanInput{Source: "acme", Policy: DefaultDiscoveryPolicy(), Listed: listed, Current: cur, Now: now}
}

func slugs(p Plan) []string {
	var s []string
	for _, t := range p.Next {
		s = append(s, string(t.Repo.Slug))
	}
	return s
}

func TestPlanAddRenameAlias(t *testing.T) {
	p1 := PlanReconcile(input([]scm.Repository{repo(1, "api"), repo(2, "web")}, nil, t0))
	if got := slugs(p1); !slices.Equal(got, []string{"api", "web"}) || len(p1.Added) != 2 {
		t.Fatalf("p1: %v %v", got, p1.Added)
	}
	// repository 1 is renamed to gateway
	p2 := PlanReconcile(input([]scm.Repository{repo(1, "gateway"), repo(2, "web")}, p1.Next, t0.Add(time.Hour)))
	if got := slugs(p2); !slices.Equal(got, []string{"gateway", "web"}) {
		t.Fatalf("p2: %v", got)
	}
	if len(p2.Renamed) != 1 || p2.Renamed[0].OldSlug != "api" || p2.Renamed[0].NewSlug != "gateway" || len(p2.Removed) != 0 {
		t.Fatalf("rename: %+v removed %+v", p2.Renamed, p2.Removed)
	}
	if as := p2.Next[0].Aliases; !slices.Equal(as, []scm.Slug{"api"}) {
		t.Fatalf("aliases: %v", as)
	}
	if got := buildAliases(p2.Next); got["api"] != "gateway" || got["gateway"] != "" {
		t.Fatalf("alias map: %v", got)
	}
	// a new repository takes the old name: the alias must not shadow the real slug
	p3 := PlanReconcile(input([]scm.Repository{repo(1, "gateway"), repo(2, "web"), repo(3, "api")}, p2.Next, t0.Add(2*time.Hour)))
	if got := buildAliases(p3.Next); got["api"] != "" {
		t.Fatalf("alias shadows a real slug: %v", got)
	}
}

func TestPlanRemovalNeedsTwoPasses(t *testing.T) {
	var cur []Tracked
	list := []scm.Repository{}
	for i := 1; i <= 20; i++ {
		list = append(list, repo(i, fmt.Sprintf("r%02d", i)))
	}
	cur = PlanReconcile(input(list, nil, t0)).Next
	// r05 disappears: first pass keeps it, second removes it
	l2 := slices.DeleteFunc(slices.Clone(list), func(r scm.Repository) bool { return r.Ref.ID == 5 })
	p := PlanReconcile(input(l2, cur, t0.Add(30*time.Minute)))
	if len(p.Removed) != 0 || len(p.Next) != 20 {
		t.Fatalf("first absence removed: %+v", p.Removed)
	}
	p = PlanReconcile(input(l2, p.Next, t0.Add(60*time.Minute)))
	if len(p.Removed) != 1 || p.Removed[0].Reason != "absent" || len(p.Next) != 19 {
		t.Fatalf("second absence: %+v", p.Removed)
	}
	// a repository that reappears resets the counter
	cur = PlanReconcile(input(l2, cur, t0.Add(30*time.Minute))).Next
	p = PlanReconcile(input(list, cur, t0.Add(60*time.Minute)))
	if len(p.Removed) != 0 || len(p.Next) != 20 || p.Next[4].Misses != 0 {
		t.Fatalf("reappearing repository: %+v", p.Removed)
	}
}

func TestPlanMassRemovalHeldThenReleased(t *testing.T) {
	var list []scm.Repository
	for i := 1; i <= 20; i++ {
		list = append(list, repo(i, fmt.Sprintf("r%02d", i)))
	}
	cur := PlanReconcile(input(list, nil, t0)).Next
	few := list[:5] // 15 of 20 vanish (75% > 30%)
	now := t0.Add(30 * time.Minute)
	p := PlanReconcile(input(few, cur, now))
	if len(p.Removed) != 0 || p.Held != nil { // first absence: below the confirmation count, nothing to hold yet
		t.Fatalf("pass 1: removed=%d held=%v", len(p.Removed), p.Held)
	}
	in := input(few, p.Next, now.Add(30*time.Minute))
	p = PlanReconcile(in) // second absence: 15 removals would apply -> held
	if p.Held == nil || p.HeldCount != 15 || len(p.Removed) != 0 || len(p.Next) != 20 {
		t.Fatalf("pass 2: held=%v removed=%d next=%d", p.Held, len(p.Removed), len(p.Next))
	}
	since := p.Held.Since
	in = input(few, p.Next, now.Add(2*time.Hour))
	in.Held = p.Held
	p = PlanReconcile(in) // same fingerprint, 1.5 h held: still held, timer not reset
	if p.Held == nil || !p.Held.Since.Equal(since) || len(p.Removed) != 0 {
		t.Fatalf("pass 3: %+v", p.Held)
	}
	in = input(few, p.Next, since.Add(6*time.Hour))
	in.Held = p.Held
	p = PlanReconcile(in) // hold period elapsed with the identical candidate set: applied
	if p.Held != nil || len(p.Removed) != 15 || len(p.Next) != 5 {
		t.Fatalf("release: held=%v removed=%d next=%d", p.Held, len(p.Removed), len(p.Next))
	}
}

func TestPlanEverythingVanishingIsHeld(t *testing.T) {
	cur := PlanReconcile(input([]scm.Repository{repo(1, "a"), repo(2, "b")}, nil, t0)).Next // 2 repositories: below guardFloor, but N -> 0
	p := PlanReconcile(input(nil, cur, t0.Add(time.Hour)))
	p = PlanReconcile(input(nil, p.Next, t0.Add(2*time.Hour)))
	if p.Held == nil || len(p.Next) != 2 {
		t.Fatalf("N->0 must be held: %+v", p.Held)
	}
}

func TestPlanVisibilityIsImmediateAndUnguarded(t *testing.T) {
	var list []scm.Repository
	for i := 1; i <= 10; i++ {
		list = append(list, repo(i, fmt.Sprintf("r%02d", i)))
	}
	cur := PlanReconcile(input(list, nil, t0)).Next
	for i := range list {
		list[i].Visibility = scm.VisPrivate // GitHub says every repository is private now
	}
	p := PlanReconcile(input(list, cur, t0.Add(time.Minute)))
	if len(p.Removed) != 10 || p.Held != nil || len(p.Next) != 0 || p.Excluded[ExcludedVisibility] != 10 {
		t.Fatalf("visibility removal: removed=%d held=%v", len(p.Removed), p.Held)
	}
}

func TestPlanForksAndArchived(t *testing.T) {
	r1, r2, r3 := repo(1, "a"), repo(2, "b"), repo(3, "c")
	r2.Fork, r3.Archived = true, true
	p := PlanReconcile(input([]scm.Repository{r1, r2, r3}, nil, t0))
	if got := slugs(p); !slices.Equal(got, []string{"a", "c"}) || p.Excluded[ExcludedFork] != 1 { // default: archived included, forks excluded
		t.Fatalf("%v %v", got, p.Excluded)
	}
}

func TestPlanSlugCollisionAndReserved(t *testing.T) {
	a := repo(1, "api")
	b := repo(2, "API")
	b.Ref.Owner = "other-org"
	g := repo(3, "groups")
	p := PlanReconcile(input([]scm.Repository{a, b, g}, nil, t0))
	if got := slugs(p); !slices.Equal(got, []string{"api"}) || len(p.Quarantined) != 2 {
		t.Fatalf("slugs %v quarantined %+v", got, p.Quarantined)
	}
	reasons := map[string]bool{}
	for _, q := range p.Quarantined {
		reasons[q.Reason] = true
	}
	if !reasons["slug_collision"] || !reasons["slug_reserved"] || !HasErrors(p.Findings) {
		t.Fatalf("reasons %v", reasons)
	}
	// the prefix of the second owner resolves the collision
	in := input([]scm.Repository{a, b}, nil, t0)
	in.Prefix = func(_ scm.SourceID, owner string) string {
		if owner == "other-org" {
			return "other-"
		}
		return ""
	}
	if got := slugs(PlanReconcile(in)); !slices.Equal(got, []string{"api", "other-api"}) {
		t.Fatalf("with prefix: %v", got)
	}
}

func TestPlanSlugWinnerIsStable(t *testing.T) {
	a, b := repo(1, "api"), repo(2, "API")
	b.Ref.Owner = "other-org"
	cur := PlanReconcile(input([]scm.Repository{a}, nil, t0)).Next // a holds "api"
	for i := 0; i < 5; i++ {
		order := []scm.Repository{a, b}
		if i%2 == 1 {
			order = []scm.Repository{b, a}
		}
		p := PlanReconcile(input(order, cur, t0.Add(time.Hour)))
		if got := slugs(p); !slices.Equal(got, []string{"api"}) || p.Next[0].Repo.Ref.ID != 1 {
			t.Fatalf("the incumbent must keep the slug whatever the listing order: %v", got)
		}
	}
}

func TestPlanIsOrderIndependent(t *testing.T) {
	var list []scm.Repository
	for i := 1; i <= 40; i++ {
		list = append(list, repo(i, fmt.Sprintf("Repo-%02d", 41-i)))
	}
	want := slugs(PlanReconcile(input(list, nil, t0)))
	rnd := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 20; i++ {
		l := slices.Clone(list)
		rnd.Shuffle(len(l), func(a, b int) { l[a], l[b] = l[b], l[a] })
		if got := slugs(PlanReconcile(input(l, nil, t0))); !slices.Equal(got, want) {
			t.Fatal("plan depends on listing order")
		}
	}
}

func TestPlanUngroupedIgnore(t *testing.T) {
	in := input([]scm.Repository{repo(1, "keep"), repo(2, "drop")}, nil, t0)
	in.Grouped = func(r scm.Repository) bool { return r.Ref.Name == "keep" }
	p := PlanReconcile(in)
	if got := slugs(p); !slices.Equal(got, []string{"keep"}) || p.Excluded[ExcludedUngrouped] != 1 {
		t.Fatalf("%v %v", got, p.Excluded)
	}
}

func TestPlanDuplicateAndInvalidListEntries(t *testing.T) {
	a := repo(1, "a")
	a2 := repo(1, "a-renamed") // same id twice: last wins
	bad := repo(0, "noid")
	p := PlanReconcile(input([]scm.Repository{a, a2, bad}, nil, t0))
	if got := slugs(p); !slices.Equal(got, []string{"a-renamed"}) || p.Excluded[ExcludedInvalid] != 1 {
		t.Fatalf("%v %v", got, p.Excluded)
	}
}

func BenchmarkPlanReconcile1000(b *testing.B) {
	var listed []scm.Repository
	for i := 1; i <= 1000; i++ {
		listed = append(listed, repo(i, fmt.Sprintf("service-%04d", i)))
	}
	cur := PlanReconcile(input(listed, nil, t0)).Next
	in := input(listed, cur, t0.Add(time.Hour))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanReconcile(in)
	}
}

func TestNormaliseListed(t *testing.T) {
	good := repo(1, "api")
	good.Description = "Billing\x00 ‮service\nwith\ttabs"
	good.Topics = []string{"iam", "Has Space", "ok-1", "UPPER"}
	good.HTMLURL = "javascript:alert(1)"
	r, ok := normaliseListed(good)
	if !ok || r.Description != "Billing service with tabs" || len(r.Topics) != 2 || r.HTMLURL != "" {
		t.Fatalf("%+v %v", r, ok)
	}
	r, _ = normaliseListed(func() scm.Repository { x := repo(1, "api"); x.HTMLURL = "https://github.com/acme-org/api"; return x }())
	if r.HTMLURL != "https://github.com/acme-org/api" {
		t.Fatalf("a normal URL was dropped: %q", r.HTMLURL)
	}
	r, _ = normaliseListed(func() scm.Repository { x := repo(1, "api"); x.HTMLURL = "http://github.example/acme-org/api"; return x }())
	if r.HTMLURL != "" {
		t.Fatalf("a non-https URL was kept (the public API type is HttpsUrl): %q", r.HTMLURL)
	}
	long := repo(1, "api")
	long.Description = strings.Repeat("é", 800)
	if r, _ := normaliseListed(long); len(r.Description) > maxDescriptionBytes || !utf8.ValidString(r.Description) {
		t.Fatalf("description not truncated at a rune boundary: %d bytes", len(r.Description))
	}
	for name, mod := range map[string]func(*scm.Repository){
		"zero id":        func(x *scm.Repository) { x.Ref.ID = 0 },
		"owner slash":    func(x *scm.Repository) { x.Ref.Owner = "a/b" },
		"empty owner":    func(x *scm.Repository) { x.Ref.Owner = "" },
		"name space":     func(x *scm.Repository) { x.Ref.Name = "a b" },
		"name too long":  func(x *scm.Repository) { x.Ref.Name = strings.Repeat("a", 101) },
		"bad branch":     func(x *scm.Repository) { x.DefaultBranch = "a..b" },
		"name with .md/": func(x *scm.Repository) { x.Ref.Name = "x/../y" },
	} {
		x := repo(1, "api")
		mod(&x)
		if _, ok := normaliseListed(x); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	many := repo(1, "api")
	for i := 0; i < 40; i++ {
		many.Topics = append(many.Topics, fmt.Sprintf("t%d", i))
	}
	if r, _ := normaliseListed(many); len(r.Topics) != maxTopics {
		t.Fatalf("topics: %d", len(r.Topics))
	}
}

// A three-repository portal must be able to lose one repository: the guard's floor of two removals per pass.
func TestPlanGuardFloorLetsASmallPortalLoseOne(t *testing.T) {
	list := []scm.Repository{repo(1, "a"), repo(2, "b"), repo(3, "c")}
	cur := PlanReconcile(input(list, nil, t0)).Next
	now := t0
	var p Plan
	for i := 0; i < 2; i++ { // two complete listings without "c": confirmed
		now = now.Add(2 * time.Minute)
		p = PlanReconcile(input(list[:2], cur, now))
		cur = p.Next
	}
	if p.Held != nil || len(p.Removed) != 1 || len(p.Next) != 2 {
		t.Fatalf("one removal in a small portal must pass: held=%v removed=%d", p.Held, len(p.Removed))
	}
}
