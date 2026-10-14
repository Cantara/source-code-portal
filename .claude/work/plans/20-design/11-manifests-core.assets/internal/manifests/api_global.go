package manifests

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/manifestsapi"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Caps of the graph operation (contract: getDependencyGraph).
const (
	GraphMaxNodes        = 2000
	GraphMaxEdges        = 20000
	GraphMermaidMaxNodes = 300
)

const unspecifiedVersion = "unspecified"

// graphOf returns the class graph or writes 503 not-ready (the graph job has not completed since start).
func (s *Service) graphOf(w http.ResponseWriter, r *http.Request, v view) (*Graph, bool) {
	if v.graph == nil {
		s.reply(w, r, notReady())
		return nil, false
	}
	return v.graph, true
}

// scopeFreshness is the Freshness and statuses of a response built from every repository (or group member) the class may see.
func (s *Service) scopeFreshness(v view, kind string, group map[scm.Slug]bool) fresh {
	_, infos, parsed := s.contributing(v, group)
	return s.aggregate(v.now, kind, infos, func(i int) time.Time { return parsed[i] })
}

// ---- getDependencyGraph ------------------------------------------------------------------------------------------------------------

type gEdge struct {
	from, to UnitIdx
	comp     CompIdx
	e        Edge
	prodVer  string
}

func (s *Service) handleGraph(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	p := &params{q: r.URL.Query()}
	scopes, eco := p.scopes(), p.ecosystems()
	intra := false
	if b := p.boolean("intra"); b != nil {
		intra = *b
	}
	internalOnly := true
	if b := p.boolean("internal"); b != nil {
		internalOnly = *b
	}
	focus := p.text("focus", 420)
	depth := p.integer("depth", 1, 1, 3)
	format := p.q.Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "mermaid" {
		p.bad("format", "must be json or mermaid")
	}
	group := p.text("group", 200)
	if pr := p.problem(); pr != nil {
		s.reply(w, r, *pr)
		return
	}
	set, ok := s.groupSet(v, group)
	if !ok {
		s.reply(w, r, httpapi.NotFound("Group not found."))
		return
	}
	g, ok := s.graphOf(w, r, v)
	if !ok {
		return
	}
	_ = internalOnly // v1: every graph edge is internal; the parameter is reserved for external-component nodes
	fr := s.scopeFreshness(v, "manifests.graph", set)
	etag := s.etagSnapshot(v, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var seeds []UnitIdx
	if focus != "" {
		seeds = s.focusUnits(g, focus)
		if len(seeds) == 0 {
			s.reply(w, r, httpapi.NotFound("The focus is not a repository or unit of the graph."))
			return
		}
	}
	nodeOK := func(u UnitIdx) bool {
		m := g.Unit(u)
		return (set == nil || set[g.Repo(m.Repo).Slug]) && (eco == nil || eco[string(m.Ecosystem)])
	}
	edges := s.selectEdges(g, nodeOK, scopes, intra)
	if focus != "" {
		edges = neighbourhood(edges, seeds, depth)
	}
	maxNodes := GraphMaxNodes
	if format == "mermaid" {
		maxNodes = GraphMermaidMaxNodes
	}
	out, truncated := s.shape(g, edges, seeds, maxNodes, GraphMaxEdges)
	out.Truncated = truncated || g.Truncated // the build itself may have hit graph.max_edges
	out.Freshness = fr.view
	if format == "mermaid" {
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(s.mermaid(out)))
		return
	}
	s.writeJSON(w, r, etag, out)
}

// focusUnits resolves "<slug>" (every unit of the repository) or "<slug>#<unit>" to graph units.
func (s *Service) focusUnits(g *Graph, focus string) []UnitIdx {
	if slug, unit, ok := splitUnitID(focus); ok {
		ri, found := g.RepoOf(slug)
		if !found {
			return nil
		}
		lo, hi := g.UnitRange(ri)
		for u := lo; u < hi; u++ {
			if g.Unit(u).ID == unit {
				return []UnitIdx{u}
			}
		}
		return nil
	}
	ri, found := g.RepoOf(scm.Slug(focus))
	if !found {
		return nil
	}
	lo, hi := g.UnitRange(ri)
	out := make([]UnitIdx, 0, hi-lo)
	for u := lo; u < hi; u++ {
		out = append(out, u)
	}
	return out
}

// selectEdges lists the unit-to-unit edges that pass the filters, ordered by (from, to, component, scope): the order is deterministic.
func (s *Service) selectEdges(g *Graph, nodeOK func(UnitIdx) bool, scopes map[Scope]bool, intra bool) []gEdge {
	var out []gEdge
	for u := UnitIdx(0); int(u) < g.NumUnits(); u++ {
		if !nodeOK(u) {
			continue
		}
		for _, e := range g.Edges(u) {
			if e.Flags&EdgeInternal == 0 || (scopes != nil && !scopes[e.Scope]) || (e.Flags&EdgeIntra != 0 && !intra) {
				continue
			}
			for _, pr := range g.ProducerRefs(e.Comp) {
				if pr.Unit == u || !nodeOK(pr.Unit) || (g.Unit(pr.Unit).Repo == g.Unit(u).Repo && !intra) {
					continue
				}
				out = append(out, gEdge{from: u, to: pr.Unit, comp: e.Comp, e: e, prodVer: pr.Version})
			}
		}
	}
	return out
}

// neighbourhood keeps the edges within depth hops of the seeds, in both directions.
func neighbourhood(edges []gEdge, seeds []UnitIdx, depth int) []gEdge {
	adj := map[UnitIdx][]UnitIdx{}
	for _, e := range edges {
		adj[e.from] = append(adj[e.from], e.to)
		adj[e.to] = append(adj[e.to], e.from)
	}
	seen := map[UnitIdx]bool{}
	frontier := slices.Clone(seeds)
	for _, u := range seeds {
		seen[u] = true
	}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []UnitIdx
		for _, u := range frontier {
			for _, n := range adj[u] {
				if !seen[n] {
					seen[n] = true
					next = append(next, n)
				}
			}
		}
		frontier = next
	}
	out := edges[:0:0]
	for _, e := range edges {
		if seen[e.from] && seen[e.to] {
			out = append(out, e)
		}
	}
	return out
}

// shape applies the caps (highest-degree nodes survive) and builds the DTO.
func (s *Service) shape(g *Graph, edges []gEdge, seeds []UnitIdx, maxNodes, maxEdges int) (manifestsapi.DependencyGraph, bool) {
	deg := map[UnitIdx][2]int{} // in, out
	for _, e := range edges {
		a, b := deg[e.from], deg[e.to]
		a[1]++
		b[0]++
		deg[e.from], deg[e.to] = a, b
	}
	for _, u := range seeds {
		if _, ok := deg[u]; !ok {
			deg[u] = [2]int{}
		}
	}
	nodes := make([]UnitIdx, 0, len(deg))
	for u := range deg {
		nodes = append(nodes, u)
	}
	slices.Sort(nodes)
	total := summarise(g, nodes, edges, s)
	truncated := false
	if len(nodes) > maxNodes {
		truncated = true
		slices.SortStableFunc(nodes, func(a, b UnitIdx) int { return cmp.Or(deg[b][0]+deg[b][1]-deg[a][0]-deg[a][1], int(a)-int(b)) })
		nodes = nodes[:maxNodes]
		slices.Sort(nodes)
		keep := make(map[UnitIdx]bool, len(nodes))
		for _, u := range nodes {
			keep[u] = true
		}
		edges = slices.DeleteFunc(slices.Clone(edges), func(e gEdge) bool { return !keep[e.from] || !keep[e.to] })
	}
	if len(edges) > maxEdges {
		truncated = true
		edges = edges[:maxEdges]
	}
	out := manifestsapi.DependencyGraph{Nodes: make([]manifestsapi.GraphNode, 0, len(nodes)), Edges: make([]manifestsapi.GraphEdge, 0, len(edges)), Cycles: [][]string{}, Summary: total}
	inSet := make(map[UnitIdx]bool, len(nodes))
	for _, u := range nodes {
		inSet[u] = true
		m := g.Unit(u)
		slug := g.Repo(m.Repo).Slug
		label := CleanText(string(slug), 200)
		if m.Dir != "" && m.Dir != "." {
			label += " / " + CleanText(m.Dir, 200)
		}
		n := manifestsapi.GraphNode{Id: unitID(slug, m.ID), Repo: string(slug), Dir: m.Dir, Ecosystem: string(m.Ecosystem), Label: label, Artifacts: []string{}, InCycle: g.InCycle(u),
			InDegree: deg[u][0], OutDegree: deg[u][1]}
		for i, a := range g.UnitData(u).Provides {
			if i == 5 {
				break
			}
			if p := s.purl(a.ID, a.Display, "", ""); p != "" {
				n.Artifacts = append(n.Artifacts, p)
			}
		}
		out.Nodes = append(out.Nodes, n)
	}
	for _, e := range edges {
		fm, to := g.Unit(e.from), g.Unit(e.to)
		dep := g.DepOf(e.from, e.e)
		c := g.Component(e.comp)
		ge := manifestsapi.GraphEdge{From: unitID(g.Repo(fm.Repo).Slug, fm.ID), To: unitID(g.Repo(to.Repo).Slug, to.ID), Component: s.purl(c, dep.TargetDisplay(), "", ""), Requirement: dep.Requirement,
			Version: ptr(g.Version(e.e.Ver)), Scope: e.e.Scope.String(), Intra: e.e.Flags&EdgeIntra != 0, VersionStatus: versionStatus(c.Ecosystem, g.effVersion(e.e), e.prodVer)}
		out.Edges = append(out.Edges, ge)
	}
	for _, cyc := range g.Cycles() {
		if len(out.Cycles) >= s.cfg.Graph.MaxCyclesReported {
			break
		}
		ids, all := make([]string, 0, len(cyc)), true
		for _, u := range cyc {
			if !inSet[u] {
				all = false
				break
			}
			m := g.Unit(u)
			ids = append(ids, unitID(g.Repo(m.Repo).Slug, m.ID))
		}
		if all {
			out.Cycles = append(out.Cycles, ids)
		}
	}
	return out, truncated
}

// summarise counts the filtered graph before the caps (the summary says how big the whole answer is, not how much was returned).
func summarise(g *Graph, nodes []UnitIdx, edges []gEdge, _ *Service) manifestsapi.GraphSummary {
	repos := map[RepoIdx]bool{}
	ecos := map[EcosystemID]*manifestsapi.EcosystemCount{}
	for _, u := range nodes {
		m := g.Unit(u)
		repos[m.Repo] = true
		c := ecos[m.Ecosystem]
		if c == nil {
			c = &manifestsapi.EcosystemCount{Ecosystem: string(m.Ecosystem)}
			ecos[m.Ecosystem] = c
		}
		c.Units++
	}
	for _, e := range edges {
		if c := ecos[g.Unit(e.from).Ecosystem]; c != nil {
			c.Dependencies++
		}
	}
	in := make(map[UnitIdx]bool, len(nodes))
	for _, u := range nodes {
		in[u] = true
	}
	cycles := 0
	for _, cyc := range g.Cycles() {
		if !slices.ContainsFunc(cyc, func(u UnitIdx) bool { return !in[u] }) {
			cycles++
		}
	}
	sum := manifestsapi.GraphSummary{Repos: len(repos), Units: len(nodes), Edges: len(edges), Cycles: cycles, Ecosystems: []manifestsapi.EcosystemCount{}}
	for _, k := range sortedKeys(ecos) {
		sum.Ecosystems = append(sum.Ecosystems, *ecos[k])
	}
	return sum
}

// mermaid renders the graph as flowchart text. Node ids are generated, labels are reduced to a safe alphabet: no repository- or
// manifest-controlled character can close the label or inject syntax.
func (s *Service) mermaid(d manifestsapi.DependencyGraph) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	if d.Truncated {
		b.WriteString("  %% truncated: the graph was capped\n")
	}
	idx := make(map[string]int, len(d.Nodes))
	for i, n := range d.Nodes {
		idx[n.Id] = i
		fmt.Fprintf(&b, "  n%d[\"%s\"]\n", i, mermaidText(n.Label))
	}
	for _, e := range d.Edges {
		f, ok1 := idx[e.From]
		t, ok2 := idx[e.To]
		if ok1 && ok2 {
			fmt.Fprintf(&b, "  n%d -->|%s| n%d\n", f, e.Scope, t)
		}
	}
	return b.String()
}

func mermaidText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == '/', r == ' ', r == '@', r == ':', r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ---- getDependencyVersions ---------------------------------------------------------------------------------------------------------

type useStat struct {
	users, repos int
	versions     []manifestsapi.VersionCount
	distinct     int
}

// statsOf is the use statistics of a component, optionally restricted to a repository set. Without a restriction it reads the precomputed
// counters and builds the histogram from the reverse index.
func (s *Service) statsOf(g *Graph, c CompIdx, set map[scm.Slug]bool) useStat {
	type acc struct {
		units int
		repos map[RepoIdx]struct{}
	}
	hist := map[string]*acc{}
	users := 0
	repos := map[RepoIdx]struct{}{}
	for _, ref := range g.userRefs[g.userOff[c]:g.userOff[c+1]] {
		m := g.Unit(ref.Unit)
		if set != nil && !set[g.Repo(m.Repo).Slug] {
			continue
		}
		users++
		repos[m.Repo] = struct{}{}
		ver := g.effVersion(g.Edges(ref.Unit)[ref.Edge])
		if ver == "" {
			ver = unspecifiedVersion
		}
		a := hist[ver]
		if a == nil {
			a = &acc{repos: map[RepoIdx]struct{}{}}
			hist[ver] = a
		}
		a.units++
		a.repos[m.Repo] = struct{}{}
	}
	out := useStat{users: users, repos: len(repos), distinct: len(hist)}
	for ver, a := range hist {
		out.versions = append(out.versions, manifestsapi.VersionCount{Version: CleanText(ver, 200), Units: a.units, Repos: len(a.repos)})
	}
	slices.SortFunc(out.versions, func(a, b manifestsapi.VersionCount) int {
		return cmp.Or(b.Units-a.Units, strings.Compare(a.Version, b.Version))
	})
	if n := s.cfg.Output.VersionsTopN; len(out.versions) > n {
		out.versions = out.versions[:n]
	}
	if out.versions == nil {
		out.versions = []manifestsapi.VersionCount{}
	}
	return out
}

type verCursor struct {
	Filt  string `json:"f"`
	Users int    `json:"u"`
	Key   string `json:"k"`
}

func (s *Service) spreadOf(g *Graph, c CompIdx, st useStat) manifestsapi.ComponentSpread {
	id := g.Component(c)
	prods := g.ProducerRefs(c)
	out := manifestsapi.ComponentSpread{Purl: s.purl(id, "", "", ""), Ecosystem: string(id.Ecosystem), Namespace: ptr(id.Namespace), Name: id.Name, Internal: len(prods) > 0,
		Users: st.users, Repos: st.repos, DistinctVersions: st.distinct, Versions: st.versions}
	if len(prods) > 0 { // external components have no known latest (no registry lookups in v1)
		vs := make([]string, 0, len(prods))
		for _, p := range prods {
			if p.Version != "" {
				vs = append(vs, p.Version)
			}
		}
		out.LatestKnown = ptr(latestOf(id.Ecosystem, vs))
	}
	return out
}

func (s *Service) handleVersions(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	p := &params{q: r.URL.Query()}
	o := s.cfg.Output
	limit := p.integer("limit", o.DefaultPage, 1, o.MaxPage)
	eco := p.ecosystems()
	internal := p.boolean("internal")
	minVersions := p.integer("minVersions", 1, 1, 100)
	needle := strings.ToLower(p.text("q", 200))
	group := p.text("group", 200)
	if pr := p.problem(); pr != nil {
		s.reply(w, r, *pr)
		return
	}
	filt := httpapi.QueryFingerprint(p.q, "group", "ecosystem", "internal", "minVersions", "q")
	var cur *verCursor
	if c := p.q.Get("cursor"); c != "" {
		cur = &verCursor{}
		if err := httpapi.DecodeCursor(c, cur); err != nil || cur.Filt != filt {
			s.reply(w, r, httpapi.InvalidCursor())
			return
		}
	}
	set, ok := s.groupSet(v, group)
	if !ok {
		s.reply(w, r, httpapi.NotFound("Group not found."))
		return
	}
	g, ok := s.graphOf(w, r, v)
	if !ok {
		return
	}
	fr := s.scopeFreshness(v, "manifests.versions", set)
	etag := s.etagSnapshot(v, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	type row struct {
		c     CompIdx
		users int
		key   string
		st    useStat
	}
	var rows []row
	for _, c := range g.ByUse() {
		id := g.Component(c)
		key := id.String()
		if (eco != nil && !eco[string(id.Ecosystem)]) || (needle != "" && !strings.Contains(strings.ToLower(key), needle)) ||
			(internal != nil && (len(g.ProducerRefs(c)) > 0) != *internal) {
			continue
		}
		if set == nil {
			if int(g.Stat(c).Distinct) < minVersions {
				continue
			}
			rows = append(rows, row{c: c, users: int(g.Stat(c).Users), key: key})
			continue
		}
		st := s.statsOf(g, c, set)
		if st.users == 0 || st.distinct < minVersions {
			continue
		}
		rows = append(rows, row{c: c, users: st.users, key: key, st: st})
	}
	if set != nil {
		slices.SortFunc(rows, func(a, b row) int { return cmp.Or(b.users-a.users, strings.Compare(a.key, b.key)) })
	}
	start := 0
	if cur != nil {
		start = sort_search(len(rows), func(i int) bool {
			return rows[i].users < cur.Users || (rows[i].users == cur.Users && rows[i].key > cur.Key)
		})
	}
	end := min(start+limit, len(rows))
	out := manifestsapi.VersionSpreadList{Items: make([]manifestsapi.ComponentSpread, 0, end-start), Freshness: fr.view}
	for _, rw := range rows[start:end] {
		st := rw.st
		if set == nil {
			st = s.statsOf(g, rw.c, nil)
		}
		out.Items = append(out.Items, s.spreadOf(g, rw.c, st))
	}
	next := ""
	if end < len(rows) {
		next = httpapi.EncodeCursor(verCursor{Filt: filt, Users: rows[end-1].users, Key: rows[end-1].key})
	}
	out.Page = httpapi.NewPageInfo(next, limit)
	s.writeJSON(w, r, etag, out)
}

// sort_search is sort.Search without the import (rows are ordered, the predicate is monotone).
func sort_search(n int, f func(int) bool) int {
	lo, hi := 0, n
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if !f(m) {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

// ---- getDependencyLicenses ---------------------------------------------------------------------------------------------------------

func (s *Service) handleLicences(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	p := &params{q: r.URL.Query()}
	group := p.text("group", 200)
	if pr := p.problem(); pr != nil {
		s.reply(w, r, *pr)
		return
	}
	set, ok := s.groupSet(v, group)
	if !ok {
		s.reply(w, r, httpapi.NotFound("Group not found."))
		return
	}
	fr := s.scopeFreshness(v, "manifests.licenses", set)
	etag := s.etagSnapshot(v, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	type acc struct{ comps, deps, repos int }
	b := map[string]*acc{}
	add := func(lic string, comps, deps int) {
		if comps == 0 && deps == 0 {
			return
		}
		a := b[lic]
		if a == nil {
			a = &acc{}
			b[lic] = a
		}
		a.comps, a.deps, a.repos = a.comps+comps, a.deps+deps, a.repos+1
	}
	cov := manifestsapi.LicenseCoverage{}
	ids, _, _ := s.contributing(v, set)
	for _, id := range ids {
		f, _ := readmodel.Get(v.snap, id, s.d.Slots.Facts)
		l := f.V.Licences
		cov.Total += l.Total
		cov.Declared += l.Declared
		for lic, n := range l.ByLicence {
			add(CleanText(lic, 100), l.ComponentsByLicence[lic], n)
		}
		add("unknown", l.Components-l.ComponentsDeclared, l.Total-l.Declared)
	}
	if cov.Total > 0 {
		cov.Ratio = float32(float64(cov.Declared) / float64(cov.Total))
	}
	out := manifestsapi.LicenseSummary{Buckets: make([]manifestsapi.LicenseBucket, 0, len(b)), Coverage: cov, Freshness: fr.view}
	for lic, a := range b {
		out.Buckets = append(out.Buckets, manifestsapi.LicenseBucket{License: lic, Components: a.comps, Dependencies: a.deps, Repos: a.repos})
	}
	slices.SortFunc(out.Buckets, func(a, b manifestsapi.LicenseBucket) int {
		return cmp.Or(b.Dependencies-a.Dependencies, strings.Compare(a.License, b.License))
	})
	s.writeJSON(w, r, etag, out)
}

// ---- getComponent and listComponentDependents --------------------------------------------------------------------------------------

// componentOf parses the purl parameter and resolves it in the graph; it writes the problem and returns false when it cannot.
func (s *Service) componentOf(w http.ResponseWriter, r *http.Request, g *Graph, raw string) (CompIdx, ComponentID, bool) {
	if raw == "" {
		s.reply(w, r, httpapi.Validation(httpapi.FieldError{Pointer: "#/query/purl", Detail: "purl is required"}))
		return 0, ComponentID{}, false
	}
	parts, err := purl.Parse(raw)
	if err != nil {
		s.reply(w, r, httpapi.Validation(httpapi.FieldError{Pointer: "#/query/purl", Detail: "not a valid package URL of a supported ecosystem"}))
		return 0, ComponentID{}, false
	}
	id := ComponentID{Ecosystem: EcosystemID(parts.Ecosystem), Namespace: parts.Namespace, Name: parts.Name}
	c, ok := g.CompIndex(id)
	if !ok || id.Namespace == LocalNamespace || (g.Stat(c).Users == 0 && len(g.ProducerRefs(c)) == 0) {
		s.reply(w, r, httpapi.NotFound("No tracked unit uses or provides this component."))
		return 0, id, false
	}
	return c, id, true
}

func (s *Service) handleComponent(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	rawPurl := r.URL.Query().Get("purl")
	g, ok := s.graphOf(w, r, v)
	if !ok {
		return
	}
	c, id, ok := s.componentOf(w, r, g, rawPurl)
	if !ok {
		return
	}
	fr := s.scopeFreshness(v, "manifests.component", nil)
	etag := s.etagSnapshot(v, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	pu := s.purl(id, "", "", "")
	prods := g.ProducerRefs(c)
	st := s.statsOf(g, c, nil)
	out := manifestsapi.Component{Purl: pu, Ecosystem: string(id.Ecosystem), Namespace: ptr(id.Namespace), Name: id.Name, Internal: len(prods) > 0, Producers: make([]manifestsapi.Producer, 0, len(prods)),
		Users: st.users, Repos: st.repos, Versions: st.versions, DependentsUrl: "/api/v1/components/dependents?purl=" + url.QueryEscape(pu), Freshness: fr.view}
	for _, pr := range prods {
		m := g.Unit(pr.Unit)
		out.Producers = append(out.Producers, manifestsapi.Producer{Repo: string(g.Repo(m.Repo).Slug), Unit: unitID(g.Repo(m.Repo).Slug, m.ID), Version: ptr(pr.Version), Kind: artifactKind(pr.Kind), Ambiguous: len(prods) > 1})
	}
	s.writeJSON(w, r, etag, out)
}

type depntCursor struct {
	Filt string `json:"f"`
	Repo string `json:"r"`
	Unit string `json:"u"`
	Idx  uint32 `json:"i"`
}

func (s *Service) handleDependents(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	p := &params{q: r.URL.Query()}
	o := s.cfg.Output
	limit := p.integer("limit", o.DefaultPage, 1, o.MaxPage)
	scopes := p.scopes()
	group := p.text("group", 200)
	if pr := p.problem(); pr != nil {
		s.reply(w, r, *pr)
		return
	}
	filt := httpapi.QueryFingerprint(p.q, "purl", "scope", "group")
	var cur *depntCursor
	if c := p.q.Get("cursor"); c != "" {
		cur = &depntCursor{}
		if err := httpapi.DecodeCursor(c, cur); err != nil || cur.Filt != filt {
			s.reply(w, r, httpapi.InvalidCursor())
			return
		}
	}
	set, ok := s.groupSet(v, group)
	if !ok {
		s.reply(w, r, httpapi.NotFound("Group not found."))
		return
	}
	g, ok := s.graphOf(w, r, v)
	if !ok {
		return
	}
	c, id, ok := s.componentOf(w, r, g, p.q.Get("purl"))
	if !ok {
		return
	}
	fr := s.scopeFreshness(v, "manifests.dependents", set)
	etag := s.etagSnapshot(v, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var prodVersions []string
	for _, pr := range g.ProducerRefs(c) {
		if pr.Version != "" {
			prodVersions = append(prodVersions, pr.Version)
		}
	}
	latest := latestOf(id.Ecosystem, prodVersions)
	type row struct {
		slug scm.Slug
		unit string
		ref  UserRef
	}
	var rows []row
	for _, ref := range g.userRefs[g.userOff[c]:g.userOff[c+1]] {
		m := g.Unit(ref.Unit)
		slug := g.Repo(m.Repo).Slug
		if (set != nil && !set[slug]) || (scopes != nil && !scopes[g.Edges(ref.Unit)[ref.Edge].Scope]) {
			continue
		}
		rows = append(rows, row{slug, m.ID, ref})
	}
	cmpRow := func(a, b row) int {
		return cmp.Or(strings.Compare(string(a.slug), string(b.slug)), strings.Compare(a.unit, b.unit), int(a.ref.Edge)-int(b.ref.Edge))
	}
	slices.SortFunc(rows, cmpRow)
	start := 0
	if cur != nil {
		start = sort_search(len(rows), func(i int) bool {
			return cmpRow(rows[i], row{scm.Slug(cur.Repo), cur.Unit, UserRef{Edge: cur.Idx}}) > 0
		})
	}
	end := min(start+limit, len(rows))
	pu := s.purl(id, "", "", "")
	out := manifestsapi.DependentList{Purl: pu, Items: make([]manifestsapi.Dependent, 0, end-start), Freshness: fr.view}
	for _, rw := range rows[start:end] {
		e := g.Edges(rw.ref.Unit)[rw.ref.Edge]
		dep := g.DepOf(rw.ref.Unit, e)
		path := dep.Loc().Path
		if path == "" {
			path = unitFile(*g.UnitData(rw.ref.Unit))
		}
		out.Items = append(out.Items, manifestsapi.Dependent{Repo: string(rw.slug), Unit: unitID(rw.slug, rw.unit), Ecosystem: string(id.Ecosystem), Requirement: dep.Requirement,
			Version: ptr(g.Version(e.Ver)), Resolution: resolutionName(dep.Resolution), Scope: e.Scope.String(), Direct: true, VersionStatus: versionStatus(id.Ecosystem, g.effVersion(e), latest),
			Loc: manifestsapi.Location{Path: path, Line: int(dep.Line)}})
	}
	next := ""
	if end < len(rows) {
		l := rows[end-1]
		next = httpapi.EncodeCursor(depntCursor{Filt: filt, Repo: string(l.slug), Unit: l.unit, Idx: l.ref.Edge})
	}
	out.Page = httpapi.NewPageInfo(next, limit)
	s.writeJSON(w, r, etag, out)
}

var _ = freshness.StatusFresh
