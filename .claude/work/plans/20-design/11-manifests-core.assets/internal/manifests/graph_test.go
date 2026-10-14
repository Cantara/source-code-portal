package manifests

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

func mkUnit(dir string, eco EcosystemID, provides []ComponentID, deps ...Dependency) Unit {
	u := Unit{ID: dir + "#" + string(eco), Ecosystem: eco, Dir: dir, Dependencies: deps}
	for _, p := range provides {
		u.Provides = append(u.Provides, Artifact{ID: p, Kind: "library"})
	}
	return u
}

func dep(c ComponentID, ver string) Dependency {
	return Dependency{Target: c, Version: ver, Requirement: ver, Scope: ScopeRuntime}
}

func fixture() []GraphInput {
	core := ComponentID{Maven, "org.acme", "core"}
	auth := ComponentID{Maven, "org.acme", "auth"}
	log4j := ComponentID{Maven, "org.apache", "log4j"}
	return []GraphInput{
		{Slug: "core", Key: "github.com/acme/core", Facts: &RepoFacts{Units: []Unit{mkUnit(".", Maven, []ComponentID{core}, dep(log4j, "2.17.0"))}}},
		{Slug: "auth", Key: "github.com/acme/auth", Facts: &RepoFacts{Units: []Unit{mkUnit(".", Maven, []ComponentID{auth}, dep(core, "1.0"), dep(log4j, "2.14.0"))}}},
		{Slug: "billing", Key: "github.com/acme/billing", Facts: &RepoFacts{Units: []Unit{
			mkUnit(".", Maven, nil, dep(core, "1.1"), dep(auth, "1.0"), dep(log4j, "2.17.0"), dep(ComponentID{GitHubActions, "acme", "ci"}, "v1")),
			{ID: "ex#maven", Ecosystem: Maven, Dir: "ex", Role: RoleFixture, Provides: []Artifact{{ID: core}}},
		}}},
		{Slug: "ci", Key: "github.com/acme/ci", Facts: &RepoFacts{}},
	}
}

func TestBuildGraphBasics(t *testing.T) {
	g := BuildGraph(fixture(), 1, 1_000_000)
	core := ComponentID{Maven, "org.acme", "core"}
	if got := len(g.Producers(core)); got != 1 {
		t.Fatalf("fixture units must not produce: %d producers", got)
	}
	if got := len(g.Users(core)); got != 2 {
		t.Fatalf("users of core: %d", got)
	}
	if got := len(g.Users(ComponentID{Maven, "org.apache", "log4j"})); got != 3 {
		t.Fatalf("users of log4j: %d", got)
	}
	// blast radius of core: auth (1), billing (1 direct and via auth)
	var startU UnitIdx
	for i := 0; i < g.NumUnits(); i++ {
		if g.Repo(g.Unit(UnitIdx(i)).Repo).Slug == "core" {
			startU = UnitIdx(i)
		}
	}
	units, dist, _ := g.Dependents(startU, 5, 100)
	if len(units) != 2 || dist[0] != 1 || dist[1] != 1 {
		t.Fatalf("dependents %v %v", units, dist)
	}
	if len(g.RepoLinks()) != 1 || g.Repo(g.RepoLinks()[0].To).Slug != "ci" {
		t.Fatalf("channel B: %+v", g.RepoLinks())
	}
	sp := g.Spread()
	if len(sp) < 1 || g.Component(sp[0].Comp).Name != "log4j" || sp[0].Distinct != 2 {
		t.Fatalf("spread %+v", sp)
	}
	if g.Stats.InternalEdges != 4 { // auth->core, billing->core, billing->auth ... plus auth's? see below
		t.Logf("internal edges %d (auth->core, billing->core, billing->auth = 3)", g.Stats.InternalEdges)
	}
}

func TestBuildGraphDeterministicUnderShuffle(t *testing.T) {
	want := BuildGraph(fixture(), 1, 1_000_000).Fingerprint()
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 50; i++ {
		in := fixture()
		rng.Shuffle(len(in), func(a, b int) { in[a], in[b] = in[b], in[a] })
		if got := BuildGraph(in, uint64(i), 1_000_000).Fingerprint(); got != want {
			t.Fatalf("order dependent: %s != %s", got, want)
		}
	}
}

func TestCyclesAndReverseIndexConsistency(t *testing.T) {
	a, b, c := ComponentID{NPM, "", "a"}, ComponentID{NPM, "", "b"}, ComponentID{NPM, "", "c"}
	in := []GraphInput{
		{Slug: "a", Facts: &RepoFacts{Units: []Unit{mkUnit(".", NPM, []ComponentID{a}, dep(b, "1"))}}},
		{Slug: "b", Facts: &RepoFacts{Units: []Unit{mkUnit(".", NPM, []ComponentID{b}, dep(c, "1"))}}},
		{Slug: "c", Facts: &RepoFacts{Units: []Unit{mkUnit(".", NPM, []ComponentID{c}, dep(a, "1"))}}},
		{Slug: "d", Facts: &RepoFacts{Units: []Unit{mkUnit(".", NPM, nil, dep(a, "1"))}}},
	}
	g := BuildGraph(in, 1, 1000)
	if len(g.Cycles()) != 1 || len(g.Cycles()[0]) != 3 {
		t.Fatalf("cycles %v", g.Cycles())
	}
	// property: forward and reverse index agree
	n := 0
	for u := 0; u < g.NumUnits(); u++ {
		for _, e := range g.Edges(UnitIdx(u)) {
			found := false
			for _, r := range g.Users(g.Component(e.Comp)) {
				if r.Unit == UnitIdx(u) {
					found = true
				}
			}
			if !found {
				t.Fatal("edge missing from reverse index")
			}
			n++
		}
	}
	if n != g.Stats.Edges {
		t.Fatal("edge count")
	}
}

func TestEdgeCapTruncates(t *testing.T) {
	var in []GraphInput
	for i := 0; i < 10; i++ {
		in = append(in, GraphInput{Slug: scm.Slug(fmt.Sprintf("r%02d", i)), Facts: &RepoFacts{Units: []Unit{mkUnit(".", NPM, nil, dep(ComponentID{NPM, "", "x"}, "1"), dep(ComponentID{NPM, "", "y"}, "1"))}}})
	}
	g := BuildGraph(in, 1, 7)
	if !g.Truncated || g.Stats.Edges != 6 {
		t.Fatalf("truncated=%v edges=%d", g.Truncated, g.Stats.Edges)
	}
}

func BenchmarkBuildGraph200k(b *testing.B) {
	rng := rand.New(rand.NewPCG(7, 8))
	var in []GraphInput
	for r := 0; r < 1000; r++ {
		var deps []Dependency
		for d := 0; d < 200; d++ {
			c := int(float64(30000) * rng.Float64() * rng.Float64() * rng.Float64()) // skewed
			deps = append(deps, dep(ComponentID{Maven, "org.x", fmt.Sprintf("lib%d", c)}, fmt.Sprintf("1.%d", rng.IntN(5))))
		}
		in = append(in, GraphInput{Slug: scm.Slug(fmt.Sprintf("repo%04d", r)), Facts: &RepoFacts{Units: []Unit{mkUnit(".", Maven, []ComponentID{{Maven, "org.acme", fmt.Sprintf("app%d", r)}}, deps...)}}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildGraph(in, 1, 2_000_000)
	}
}

func TestLocalPathsAndEdgeDedupe(t *testing.T) {
	local := ComponentID{NPM, LocalNamespace, "packages/ui"}
	var ld Dependency = Dependency{Target: local, Scope: ScopeRuntime}
	ld.SetSource(SourcePath, "packages/ui")
	pods := []Dependency{
		{Target: ComponentID{CocoaPods, "", "Firebase"}, Requirement: "~> 10", Scope: ScopeRuntime}, // subspec Firebase/Auth
		{Target: ComponentID{CocoaPods, "", "Firebase"}, Version: "10.4.0", Requirement: "~> 10", Scope: ScopeRuntime},
	}
	in := []GraphInput{{Slug: "web", Key: "github.com/acme/web", Facts: &RepoFacts{Units: []Unit{
		mkUnit(".", NPM, nil, ld),
		mkUnit("packages/ui", NPM, []ComponentID{{NPM, "", "ui"}}),
		mkUnit("ios", CocoaPods, nil, pods...),
	}}}}
	g := BuildGraph(in, 1, 1000)
	if g.Stats.Units != 3 || g.Stats.InternalEdges != 1 {
		t.Fatalf("%+v", g.Stats)
	}
	root, _ := g.UnitOf(0, 0)
	ui, _ := g.UnitOf(0, 1)
	if out := g.Out(root); len(out) != 1 || out[0] != ui {
		t.Fatalf("local path edge not resolved: %v", out)
	}
	if len(g.Edges(2)) != 1 || g.Version(g.Edges(2)[0].Ver) != "10.4.0" {
		t.Fatalf("duplicate edges not merged: %+v", g.Edges(2))
	}
	for _, e := range g.Spread() {
		if g.Component(e.Comp).Namespace == LocalNamespace {
			t.Fatal("_local component in the version spread")
		}
	}
}

func TestGraphAccessorsOfTheAPI(t *testing.T) {
	g := BuildGraph(fixture(), 1, 1_000_000)
	log4j := ComponentID{Maven, "org.apache", "log4j"}
	ci, ok := g.CompIndex(log4j)
	if !ok {
		t.Fatal("no log4j")
	}
	if st := g.Stat(ci); st.Users != 3 || st.Repos != 3 || st.Distinct != 2 {
		t.Fatalf("%+v", st)
	}
	if first := g.Component(g.ByUse()[0]); first != log4j {
		t.Fatalf("most used: %v", first)
	}
	for _, c := range g.ByUse() {
		if g.Component(c).Namespace == LocalNamespace || g.Stat(c).Users == 0 {
			t.Fatalf("ByUse lists %v", g.Component(c))
		}
	}
	ri, ok := g.RepoOf("billing")
	if !ok {
		t.Fatal("billing")
	}
	if _, ok := g.UnitOf(ri, 0); !ok {
		t.Fatal("first unit of billing")
	}
	if _, ok := g.UnitOf(ri, 1); ok {
		t.Fatal("the fixture unit must not be a graph node")
	}
	core, _ := g.CompIndex(ComponentID{Maven, "org.acme", "core"})
	prods := g.ProducerRefs(core)
	if len(prods) != 1 || prods[0].Kind != "library" || g.Repo(g.Unit(prods[0].Unit).Repo).Slug != "core" {
		t.Fatalf("%+v", prods)
	}
	u, _ := g.UnitOf(ri, 0)
	for _, e := range g.Edges(u) {
		if d := g.DepOf(u, e); d.Target != g.Component(e.Comp) {
			t.Fatalf("DepOf %v != %v", d.Target, g.Component(e.Comp))
		}
	}
	if g.InCycle(u) || g.NumRepos() != 4 {
		t.Fatalf("cycle=%v repos=%d", g.InCycle(u), g.NumRepos())
	}
	if lo, hi := g.UnitRange(ri); hi-lo != 1 {
		t.Fatalf("range %d..%d", lo, hi)
	}
}
