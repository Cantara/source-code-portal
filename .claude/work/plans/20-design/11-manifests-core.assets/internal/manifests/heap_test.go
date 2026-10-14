package manifests

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/cantara/source-code-portal/internal/scm"
)

func heapNow() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// TestResidentHeap reports (not asserts tightly) the retained heap of 1,000 repositories x 200 dependencies:
// the RepoFacts and the Graph built from them. Spike S-D replaces the estimate with this number.
func TestResidentHeap(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	base := heapNow()
	var in []GraphInput
	for r := 0; r < 1000; r++ {
		var deps []Dependency
		for d := 0; d < 200; d++ {
			c := int(30000 * rng.Float64() * rng.Float64() * rng.Float64())
			deps = append(deps, Dependency{
				Target: ComponentID{Maven, "org.x", intern(fmt.Sprintf("lib%d", c))}, Requirement: intern(fmt.Sprintf("1.%d", rng.IntN(5))),
				Version: intern(fmt.Sprintf("1.%d", rng.IntN(5))), Resolution: ResPinned, Scope: ScopeRuntime, Group: "compile", Line: int32(d + 1)})
		}
		in = append(in, GraphInput{Slug: scm.Slug(fmt.Sprintf("repo%04d", r)), Facts: &RepoFacts{Units: []Unit{mkUnit(".", Maven, nil, slices.Clone(deps)...)}}})
	}
	t.Logf("sizeof(Dependency)=%d bytes", unsafe.Sizeof(Dependency{}))
	facts := heapNow() - base
	g := BuildGraph(in, 1, 2_000_000)
	withGraph := heapNow() - base
	t.Logf("RepoFacts (strings interned with unique.Make): %.1f MB; graph: %.1f MB; edges=%d comps=%d", float64(facts)/1e6, float64(withGraph-facts)/1e6, g.Stats.Edges, g.Stats.Components)
	runtime.KeepAlive(in)
	runtime.KeepAlive(g)
}
