package manifests

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/scm"
)

type (
	RepoIdx uint32
	UnitIdx uint32
	CompIdx uint32
	VerIdx  uint32 // 0 = none
)

// Edge flags.
const (
	EdgeIntra     uint8 = 1 << iota // consumer and producer unit are in the same repository
	EdgeHeuristic                   // the declaration was extracted heuristically
	EdgeInternal                    // the component is produced by at least one unit of the org (channel A or C)
	EdgeRepoLink                    // resolved through a repository URL (channel B)
)

// Edge is one declared dependency of a unit, interned (20 bytes).
type Edge struct {
	Comp  CompIdx
	Ver   VerIdx // exact/locked/managed version, 0 when unknown
	Req   VerIdx // raw requirement, 0 when none
	Dep   uint32 // index into the unit's Dependencies of the declaration this edge came from (the first one when duplicates were merged)
	Scope Scope
	Flags uint8
}

// UserRef is one use of a component: the unit and the index of the edge in that unit's edge list.
type UserRef struct {
	Unit UnitIdx
	Edge uint32
}

// RepoLink is a channel-B dependency edge: a unit depends on a repository (Actions `uses`, git dependencies, module paths).
type RepoLink struct {
	From UnitIdx
	To   RepoIdx
	Comp CompIdx
	Ver  VerIdx
}

type UnitMeta struct {
	Repo      RepoIdx
	Index     int // index into the repository's RepoFacts.Units
	ID        string
	Dir       string
	Ecosystem EcosystemID
}

type GraphRepo struct {
	Slug  scm.Slug
	Key   string // "host/owner/repo", lower case; "" when unknown
	facts *RepoFacts
}

// GraphInput is what Build reads: one entry per visible repository with facts.
type GraphInput struct {
	Slug  scm.Slug
	Key   string
	Facts *RepoFacts
}

// Graph is the immutable org graph: interned, CSR-packed, rebuilt in full by the manifests.graph job.
type Graph struct {
	BuildID uint64
	repos   []GraphRepo
	units   []UnitMeta
	comps   []ComponentID
	compIdx map[ComponentID]CompIdx
	vers    []string // vers[0] == ""
	edges   [][]Edge // per unit
	// CSR: producers of a component, users of a component, internal unit adjacency out/in
	prodOff, userOff, outOff, inOff []uint32
	prodUnits                       []UnitIdx
	prodVers                        []VerIdx // artifact version of each producer, parallel to prodUnits (0 = none)
	compStat                        []CompStat
	byUse                           []CompIdx // components ordered by users (desc), then identity
	inCycle                         []bool
	userRefs                        []UserRef
	outUnits, inUnits               []UnitIdx
	repoLinks                       []RepoLink
	cycles                          [][]UnitIdx
	spread                          []SpreadEntry
	Truncated                       bool
	Stats                           GraphStats
	fingerprint                     string
	repoStart                       []UnitIdx // units of repo r are [repoStart[r], repoStart[r+1]), ascending by UnitMeta.Index
	bySlug                          map[scm.Slug]RepoIdx
}

type SpreadEntry struct {
	Comp     CompIdx
	Distinct int
	Users    int
}

type GraphStats struct {
	Repos              int `json:"repos"`
	Units              int `json:"units"`
	Components         int `json:"components"`
	Edges              int `json:"edges"`
	InternalEdges      int `json:"internalEdges"`
	RepoLinks          int `json:"repoLinks"`
	Cycles             int `json:"cycles"`
	AmbiguousProducers int `json:"ambiguousProducers"`
}

// LocalNamespace is the reserved namespace of a component that is a path inside the declaring repository
// (ComponentID{Ecosystem, "_local", cleaned repo-relative path}); it is never an external dependency, never counted in the version spread,
// never exported as an SBOM component, and resolves to a unit of the same repository whose Dir equals the path.
const LocalNamespace = "_local"

// BuildGraph builds the graph from inputs (any order; the result does not depend on it). Fixture units are excluded.
// maxEdges bounds the work: beyond it the remaining units are dropped and Truncated is set.
func BuildGraph(in []GraphInput, buildID uint64, maxEdges int) *Graph {
	in = slices.Clone(in)
	slices.SortFunc(in, func(a, b GraphInput) int { return strings.Compare(string(a.Slug), string(b.Slug)) })
	g := &Graph{BuildID: buildID, compIdx: map[ComponentID]CompIdx{}, vers: []string{""}}
	verIdx := map[string]VerIdx{"": 0}
	vid := func(s string) VerIdx {
		if i, ok := verIdx[s]; ok {
			return i
		}
		i := VerIdx(len(g.vers))
		g.vers = append(g.vers, s)
		verIdx[s] = i
		return i
	}
	cid := func(c ComponentID) CompIdx {
		if i, ok := g.compIdx[c]; ok {
			return i
		}
		i := CompIdx(len(g.comps))
		g.comps = append(g.comps, c)
		g.compIdx[c] = i
		return i
	}
	type raw struct {
		unit UnitIdx
		dep  *Dependency
		slot int
	}
	var provides []struct {
		c CompIdx
		u UnitIdx
		v VerIdx
	}
	var pathDeps []struct { // channel C
		from UnitIdx
		dir  string
		repo RepoIdx
		comp CompIdx
		edge int
	}
	repoByKey := map[string]RepoIdx{}
	unitsByDir := map[RepoIdx]map[string][]UnitIdx{}
	total := 0
outer:
	for _, r := range in {
		if r.Facts == nil {
			continue
		}
		ri := RepoIdx(len(g.repos))
		g.repos = append(g.repos, GraphRepo{r.Slug, r.Key, r.Facts})
		if r.Key != "" {
			repoByKey[r.Key] = ri
		}
		for ui := range r.Facts.Units {
			u := &r.Facts.Units[ui]
			if u.Role == RoleFixture {
				continue
			}
			if total+len(u.Dependencies) > maxEdges {
				g.Truncated = true
				break outer
			}
			total += len(u.Dependencies)
			idx := UnitIdx(len(g.units))
			g.units = append(g.units, UnitMeta{Repo: ri, Index: ui, ID: u.ID, Dir: u.Dir, Ecosystem: u.Ecosystem})
			if unitsByDir[ri] == nil {
				unitsByDir[ri] = map[string][]UnitIdx{}
			}
			unitsByDir[ri][u.Dir] = append(unitsByDir[ri][u.Dir], idx)
			for _, a := range u.Provides {
				provides = append(provides, struct {
					c CompIdx
					u UnitIdx
					v VerIdx
				}{cid(a.ID), idx, vid(a.Version)})
			}
			es := make([]Edge, 0, len(u.Dependencies))
			type edgeKey struct {
				c CompIdx
				s Scope
			}
			seenEdge := make(map[edgeKey]int, len(u.Dependencies))
			for di := range u.Dependencies {
				d := &u.Dependencies[di]
				if !d.Target.Valid() {
					continue
				}
				e := Edge{Comp: cid(d.Target), Req: vid(d.Requirement), Dep: uint32(di), Scope: d.Scope}
				if d.Version != "" {
					e.Ver = vid(d.Version)
				}
				// one edge per (unit, component, scope): CocoaPods subspecs and repeated declarations share a component (RR-6)
				if at, dup := seenEdge[edgeKey{e.Comp, e.Scope}]; dup {
					if es[at].Ver == 0 && e.Ver != 0 {
						es[at].Ver, es[at].Req = e.Ver, e.Req
					}
					continue
				}
				seenEdge[edgeKey{e.Comp, e.Scope}] = len(es)
				if d.Confidence == ConfHeuristic {
					e.Flags |= EdgeHeuristic
				}
				switch {
				case d.Target.Namespace == LocalNamespace: // RR-10: a path inside this repository, resolved by channel C
					pth := d.SourceRef()
					if pth == "" {
						pth = d.Target.Name
					}
					pathDeps = append(pathDeps, struct {
						from UnitIdx
						dir  string
						repo RepoIdx
						comp CompIdx
						edge int
					}{idx, pth, ri, e.Comp, len(es)})
				case d.Source == SourcePath && d.SourceRef() != "":
					pathDeps = append(pathDeps, struct {
						from UnitIdx
						dir  string
						repo RepoIdx
						comp CompIdx
						edge int
					}{idx, d.SourceRef(), ri, e.Comp, len(es)})
				}
				es = append(es, e)
			}
			g.edges = append(g.edges, es)
		}
	}
	nu, nc := len(g.units), len(g.comps)
	// producers CSR (channel A): component -> units
	g.prodOff, g.prodUnits = csr(nc, len(provides), func(emit func(int, uint32)) {
		for _, p := range provides {
			emit(int(p.c), uint32(p.u))
		}
	})
	g.prodVers = make([]VerIdx, len(provides))
	{
		cur := slices.Clone(g.prodOff[:nc])
		for _, p := range provides {
			g.prodVers[cur[p.c]] = p.v
			cur[p.c]++
		}
	}
	// users CSR: component -> (unit, edge)
	nEdges := 0
	for _, es := range g.edges {
		nEdges += len(es)
	}
	g.userOff = make([]uint32, nc+1)
	for _, es := range g.edges {
		for _, e := range es {
			g.userOff[e.Comp+1]++
		}
	}
	for i := 0; i < nc; i++ {
		g.userOff[i+1] += g.userOff[i]
	}
	g.userRefs = make([]UserRef, nEdges)
	cur := slices.Clone(g.userOff[:nc])
	for u, es := range g.edges {
		for ei, e := range es {
			g.userRefs[cur[e.Comp]] = UserRef{UnitIdx(u), uint32(ei)}
			cur[e.Comp]++
		}
	}
	// internal adjacency
	type pair struct{ a, b UnitIdx }
	seen := map[pair]bool{}
	var adj []pair
	add := func(a, b UnitIdx) {
		if a != b && !seen[pair{a, b}] {
			seen[pair{a, b}] = true
			adj = append(adj, pair{a, b})
		}
	}
	for u, es := range g.edges {
		for ei := range es {
			e := &es[ei]
			ps := g.producersOf(e.Comp)
			var internal bool
			for _, p := range ps {
				if p == UnitIdx(u) {
					continue
				}
				internal = true
				add(UnitIdx(u), p)
				if g.units[p].Repo == g.units[u].Repo {
					e.Flags |= EdgeIntra
				}
			}
			if internal {
				e.Flags |= EdgeInternal
				g.Stats.InternalEdges++
			}
			if len(ps) > 1 {
				g.Stats.AmbiguousProducers++
			}
			// channel B: repository-URL match for components whose identity names a repository
			if !internal && g.comps[e.Comp].Namespace != LocalNamespace {
				if key := componentRepoKey(g.comps[e.Comp], g.repos[g.units[u].Repo].Key); key != "" {
					if to, ok := repoByKey[key]; ok && to != g.units[u].Repo {
						e.Flags |= EdgeRepoLink
						g.repoLinks = append(g.repoLinks, RepoLink{UnitIdx(u), to, e.Comp, e.Ver})
					}
				}
			}
		}
	}
	for _, pd := range pathDeps { // channel C: path dependencies to a unit of the same repository
		for _, to := range unitsByDir[pd.repo][pd.dir] {
			add(pd.from, to)
			if f := &g.edges[pd.from][pd.edge].Flags; *f&EdgeInternal == 0 {
				g.Stats.InternalEdges++
			}
			g.edges[pd.from][pd.edge].Flags |= EdgeIntra | EdgeInternal
		}
	}
	slices.SortFunc(adj, func(x, y pair) int {
		if x.a != y.a {
			return int(x.a) - int(y.a)
		}
		return int(x.b) - int(y.b)
	})
	g.outOff, g.outUnits = csr(nu, len(adj), func(emit func(int, uint32)) {
		for _, p := range adj {
			emit(int(p.a), uint32(p.b))
		}
	})
	g.inOff, g.inUnits = csr(nu, len(adj), func(emit func(int, uint32)) {
		for _, p := range adj {
			emit(int(p.b), uint32(p.a))
		}
	})
	g.cycles = g.tarjan()
	g.inCycle = make([]bool, nu)
	for _, c := range g.cycles {
		for _, u := range c {
			g.inCycle[u] = true
		}
	}
	g.spread = g.computeSpread()
	g.computeStats()
	g.Stats.Repos, g.Stats.Units, g.Stats.Components, g.Stats.Edges = len(g.repos), nu, nc, nEdges
	g.Stats.RepoLinks, g.Stats.Cycles = len(g.repoLinks), len(g.cycles)
	g.fingerprint = g.computeFingerprint()
	g.repoStart = make([]UnitIdx, len(g.repos)+1)
	g.bySlug = make(map[scm.Slug]RepoIdx, len(g.repos))
	for i, r := range g.repos {
		g.bySlug[r.Slug] = RepoIdx(i)
	}
	for _, u := range g.units {
		g.repoStart[u.Repo+1]++
	}
	for i := 1; i < len(g.repoStart); i++ {
		g.repoStart[i] += g.repoStart[i-1]
	}
	return g
}

// RepoOf returns the graph index of a repository by slug (false when it has no facts or was cut by the edge cap).
func (g *Graph) RepoOf(s scm.Slug) (RepoIdx, bool) { r, ok := g.bySlug[s]; return r, ok }

// UnitOf maps (repository, index into RepoFacts.Units) to the graph unit; false for fixtures and units cut by the edge cap.
func (g *Graph) UnitOf(r RepoIdx, index int) (UnitIdx, bool) {
	lo, hi := int(g.repoStart[r]), int(g.repoStart[r+1])
	i, ok := sort.Find(hi-lo, func(i int) int { return index - g.units[lo+i].Index })
	return UnitIdx(lo + i), ok
}

// CompOf returns the interned index of a component.
func (g *Graph) CompOf(c ComponentID) (CompIdx, bool) { i, ok := g.compIdx[c]; return i, ok }

func csr(n, m int, fill func(emit func(int, uint32))) ([]uint32, []UnitIdx) {
	off := make([]uint32, n+1)
	fill(func(i int, _ uint32) { off[i+1]++ })
	for i := 0; i < n; i++ {
		off[i+1] += off[i]
	}
	vals := make([]UnitIdx, m)
	cur := slices.Clone(off[:n])
	fill(func(i int, v uint32) { vals[cur[i]] = UnitIdx(v); cur[i]++ })
	return off, vals
}

// componentRepoKey maps component identities that name a repository to the graph's repo key.
func componentRepoKey(c ComponentID, consumerKey string) string {
	switch c.Ecosystem {
	case GitHubActions:
		host := "github.com"
		if i := strings.Index(consumerKey, "/"); i > 0 {
			host = consumerKey[:i]
		}
		return host + "/" + c.Namespace + "/" + c.Name
	case Golang, Swift:
		if k, ok := purl.RepoKeyFromModulePath(c.Namespace+"/"+c.Name, hostSet); ok {
			return k
		}
	}
	return ""
}

var hostSet = map[string]bool{"github.com": true}

func (g *Graph) producersOf(c CompIdx) []UnitIdx { return g.prodUnits[g.prodOff[c]:g.prodOff[c+1]] }

// Producers returns the units that publish c.
func (g *Graph) Producers(c ComponentID) []UnitIdx {
	i, ok := g.compIdx[c]
	if !ok {
		return nil
	}
	return g.producersOf(i)
}

// Users returns every use of c (reverse index), in unit order.
func (g *Graph) Users(c ComponentID) []UserRef {
	i, ok := g.compIdx[c]
	if !ok {
		return nil
	}
	return g.userRefs[g.userOff[i]:g.userOff[i+1]]
}

func (g *Graph) Unit(u UnitIdx) UnitMeta         { return g.units[u] }
func (g *Graph) Repo(r RepoIdx) GraphRepo        { return g.repos[r] }
func (g *Graph) Edges(u UnitIdx) []Edge          { return g.edges[u] }
func (g *Graph) Component(c CompIdx) ComponentID { return g.comps[c] }
func (g *Graph) Version(v VerIdx) string         { return g.vers[v] }
func (g *Graph) Cycles() [][]UnitIdx             { return g.cycles }
func (g *Graph) Spread() []SpreadEntry           { return g.spread }
func (g *Graph) RepoLinks() []RepoLink           { return g.repoLinks }
func (g *Graph) Fingerprint() string             { return g.fingerprint }
func (g *Graph) Out(u UnitIdx) []UnitIdx         { return g.outUnits[g.outOff[u]:g.outOff[u+1]] }
func (g *Graph) In(u UnitIdx) []UnitIdx          { return g.inUnits[g.inOff[u]:g.inOff[u+1]] }
func (g *Graph) NumUnits() int                   { return len(g.units) }

// Dependents is the blast radius: units that reach start through internal edges, breadth first, with distance, at most max results.
func (g *Graph) Dependents(start UnitIdx, depth, max int) (units []UnitIdx, dist []int, truncated bool) {
	seen := make([]bool, len(g.units))
	seen[start] = true
	frontier := []UnitIdx{start}
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var next []UnitIdx
		for _, u := range frontier {
			for _, p := range g.In(u) {
				if seen[p] {
					continue
				}
				seen[p] = true
				if len(units) >= max {
					return units, dist, true
				}
				units, dist = append(units, p), append(dist, d)
				next = append(next, p)
			}
		}
		frontier = next
	}
	return units, dist, false
}

// tarjan finds strongly connected components of size > 1 (iterative; the unit graph can be thousands deep in theory).
func (g *Graph) tarjan() [][]UnitIdx {
	n := len(g.units)
	index := make([]int32, n)
	low := make([]int32, n)
	on := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var stack []UnitIdx
	var out [][]UnitIdx
	var counter int32
	type frame struct {
		v UnitIdx
		i uint32
	}
	for s := 0; s < n; s++ {
		if index[s] != -1 {
			continue
		}
		call := []frame{{UnitIdx(s), 0}}
		for len(call) > 0 {
			f := &call[len(call)-1]
			v := f.v
			if f.i == 0 && index[v] == -1 {
				index[v], low[v] = counter, counter
				counter++
				stack = append(stack, v)
				on[v] = true
			}
			ws := g.Out(v)
			if int(f.i) < len(ws) {
				w := ws[f.i]
				f.i++
				if index[w] == -1 {
					call = append(call, frame{w, 0})
				} else if on[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			if low[v] == index[v] {
				var comp []UnitIdx
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					slices.Sort(comp)
					out = append(out, comp)
				}
			}
			call = call[:len(call)-1]
			if len(call) > 0 {
				p := call[len(call)-1].v
				if low[v] < low[p] {
					low[p] = low[v]
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// computeSpread lists components used with at least two distinct versions (resolved version, else requirement), widest first.
func (g *Graph) computeSpread() []SpreadEntry {
	var out []SpreadEntry
	scratch := map[VerIdx]struct{}{}
	for c := range g.comps {
		if g.comps[c].Namespace == LocalNamespace {
			continue
		}
		refs := g.userRefs[g.userOff[c]:g.userOff[c+1]]
		if len(refs) < 2 {
			continue
		}
		clear(scratch)
		for _, r := range refs {
			e := g.edges[r.Unit][r.Edge]
			v := e.Ver
			if v == 0 {
				v = e.Req
			}
			scratch[v] = struct{}{}
		}
		if len(scratch) >= 2 {
			out = append(out, SpreadEntry{CompIdx(c), len(scratch), len(refs)})
		}
	}
	slices.SortFunc(out, func(a, b SpreadEntry) int {
		if a.Distinct != b.Distinct {
			return b.Distinct - a.Distinct
		}
		if a.Users != b.Users {
			return b.Users - a.Users
		}
		return strings.Compare(g.comps[a.Comp].String(), g.comps[b.Comp].String())
	})
	return out
}

// computeFingerprint hashes a canonical serialisation: equal graphs have equal fingerprints (used to skip no-op publishes and in tests).
func (g *Graph) computeFingerprint() string {
	h := sha256.New()
	var buf [8]byte
	w := func(n uint64) { binary.LittleEndian.PutUint64(buf[:], n); h.Write(buf[:]) }
	ws := func(s string) { w(uint64(len(s))); h.Write([]byte(s)) }
	for _, r := range g.repos {
		ws(string(r.Slug))
	}
	for _, c := range g.comps { // interned tables once; edges are then plain integers
		ws(string(c.Ecosystem))
		ws(c.Namespace)
		ws(c.Name)
	}
	for _, v := range g.vers {
		ws(v)
	}
	for u, m := range g.units {
		ws(m.ID)
		w(uint64(m.Repo))
		w(uint64(len(g.edges[u])))
		for _, e := range g.edges[u] {
			w(uint64(e.Comp)<<32 | uint64(e.Ver))
			w(uint64(e.Req)<<32 | uint64(e.Scope)<<8 | uint64(e.Flags))
		}
	}
	for _, p := range g.prodUnits {
		w(uint64(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// effVersion is the version shown in spread and consumer views: the exact version when known, else the raw requirement (RR-12).
func (g *Graph) effVersion(e Edge) string {
	if e.Ver != 0 {
		return g.vers[e.Ver]
	}
	return g.vers[e.Req]
}

// CompStat summarises the use of one component.
type CompStat struct {
	Users    uint32 // using units
	Repos    uint32 // using repositories
	Distinct uint32 // distinct effective versions in use
}

func (g *Graph) computeStats() {
	g.compStat = make([]CompStat, len(g.comps))
	scratch := map[VerIdx]struct{}{}
	for c := range g.comps {
		refs := g.userRefs[g.userOff[c]:g.userOff[c+1]]
		if len(refs) == 0 {
			continue
		}
		clear(scratch)
		repos, last := uint32(0), RepoIdx(1<<31)
		for _, r := range refs {
			e := g.edges[r.Unit][r.Edge]
			v := e.Ver
			if v == 0 {
				v = e.Req
			}
			scratch[v] = struct{}{}
			if rp := g.units[r.Unit].Repo; rp != last { // users are in unit order, which is repository order
				repos, last = repos+1, rp
			}
		}
		g.compStat[c] = CompStat{uint32(len(refs)), repos, uint32(len(scratch))}
	}
	g.byUse = make([]CompIdx, 0, len(g.comps))
	for c := range g.comps {
		if g.compStat[c].Users > 0 && g.comps[c].Namespace != LocalNamespace {
			g.byUse = append(g.byUse, CompIdx(c))
		}
	}
	slices.SortFunc(g.byUse, func(a, b CompIdx) int {
		if x, y := g.compStat[a].Users, g.compStat[b].Users; x != y {
			return int(y) - int(x)
		}
		return strings.Compare(g.comps[a].String(), g.comps[b].String())
	})
}

// Stat returns the use statistics of a component.
func (g *Graph) Stat(c CompIdx) CompStat { return g.compStat[c] }

// ByUse lists the used components, most used first, ties by identity (the order of getDependencyVersions).
func (g *Graph) ByUse() []CompIdx { return g.byUse }

// InCycle reports whether the unit belongs to a strongly connected component of size > 1.
func (g *Graph) InCycle(u UnitIdx) bool { return g.inCycle[u] }

// ProducerRef is one unit that publishes a component and the artifact version it declares ("" when none or a placeholder).
type ProducerRef struct {
	Unit    UnitIdx
	Version string
	Kind    string
}

// ProducerRefs lists the producers of a component in unit order.
func (g *Graph) ProducerRefs(c CompIdx) []ProducerRef {
	out := make([]ProducerRef, 0, g.prodOff[c+1]-g.prodOff[c])
	for i := g.prodOff[c]; i < g.prodOff[c+1]; i++ {
		u := g.prodUnits[i]
		ref := ProducerRef{Unit: u, Version: g.vers[g.prodVers[i]]}
		for _, a := range g.UnitData(u).Provides {
			if g.compIdx[a.ID] == c {
				ref.Kind = a.Kind
				break
			}
		}
		out = append(out, ref)
	}
	return out
}

// CompIndex returns the interned index of a component.
func (g *Graph) CompIndex(c ComponentID) (CompIdx, bool) { i, ok := g.compIdx[c]; return i, ok }

// UnitData returns the unit the graph node was built from (immutable; shared with the facts it was built from).
func (g *Graph) UnitData(u UnitIdx) *Unit {
	m := g.units[u]
	return &g.repos[m.Repo].facts.Units[m.Index]
}

// DepOf returns the declaration an edge came from.
func (g *Graph) DepOf(u UnitIdx, e Edge) *Dependency { return &g.UnitData(u).Dependencies[e.Dep] }

// Facts returns the repository facts the graph was built from.
func (g *Graph) Facts(r RepoIdx) *RepoFacts { return g.repos[r].facts }

// NumRepos is the number of repositories in the graph.
func (g *Graph) NumRepos() int { return len(g.repos) }

// UnitRange is the half-open range of graph units of a repository.
func (g *Graph) UnitRange(r RepoIdx) (lo, hi UnitIdx) { return g.repoStart[r], g.repoStart[r+1] }
