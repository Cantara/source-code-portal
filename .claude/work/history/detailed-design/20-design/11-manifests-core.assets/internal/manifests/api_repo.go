package manifests

import (
	"bytes"
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/openapi"
	"github.com/cantara/source-code-portal/internal/manifests/manifestsapi"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

// ---- getRepoManifests --------------------------------------------------------------------------------------------------------------

func (s *Service) handleRepoManifests(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	repo, f, ok := s.repoOf(w, r, v)
	if !ok {
		return
	}
	fr := s.aggregate(v.now, "manifests.repo", []freshness.Info{f.Info}, func(int) time.Time { return f.V.ParsedAt })
	etag := s.etagRepo(v, repo.Slug, fr)
	if httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.writeJSON(w, r, etag, s.buildRepoManifests(v, repo, f.V, fr))
}

// buildRepoManifests is the body of getRepoManifests: a pure function of the facts, the class graph and the freshness view.
func (s *Service) buildRepoManifests(v view, repo scm.Repository, f RepoFacts, fr fresh) manifestsapi.RepoManifests {
	g := v.graph
	ri, haveRepo := RepoIdx(0), false
	if g != nil {
		ri, haveRepo = g.RepoOf(repo.Slug)
	}
	out := manifestsapi.RepoManifests{Repo: string(repo.Slug), TreeSha: f.TreeSHA, CommitSha: ptr(f.CommitSHA), Truncated: f.Truncated,
		Toolchain: f.Technology.Toolchain, Units: []manifestsapi.ManifestUnit{}, Diagnostics: diagnostics(f.Diagnostics), Freshness: fr.view}
	if out.Toolchain == nil {
		out.Toolchain = map[string]string{}
	}
	if !f.ParsedAt.IsZero() {
		t := f.ParsedAt.UTC()
		out.ParsedAt = &t
	}
	eco := map[EcosystemID]*manifestsapi.EcosystemCount{}
	primary := map[string]int{} // language -> primary units
	for ui, u := range f.Units {
		internal := 0
		if haveRepo {
			internal = unitInternal(g, ri, ui)
		}
		mu := manifestsapi.ManifestUnit{Id: unitID(repo.Slug, u.ID), Dir: u.Dir, Ecosystem: string(u.Ecosystem), Role: u.Role.String(), Provides: []manifestsapi.Artifact{},
			Toolchain: u.Toolchain, Files: []manifestsapi.ManifestFile{}, Counts: manifestsapi.UnitCounts{Direct: len(u.Dependencies), Internal: internal, Locked: u.LockedCount}, Diagnostics: diagnostics(u.Diagnostics)}
		if mu.Toolchain == nil {
			mu.Toolchain = map[string]string{}
		}
		for _, a := range u.Provides {
			mu.Provides = append(mu.Provides, manifestsapi.Artifact{Purl: s.purl(a.ID, a.Display, "", ""), Version: ptr(a.Version), Kind: artifactKind(a.Kind)})
		}
		for _, fr := range u.Files {
			mu.Files = append(mu.Files, manifestsapi.ManifestFile{Path: fr.Path, BlobSha: fr.BlobSHA, Parser: fileParser(fr), Kind: kindName(fr.Kind)})
		}
		out.Units = append(out.Units, mu)
		if u.Role != RolePrimary {
			continue
		}
		c := eco[u.Ecosystem]
		if c == nil {
			c = &manifestsapi.EcosystemCount{Ecosystem: string(u.Ecosystem)}
			eco[u.Ecosystem] = c
		}
		c.Units++
		c.Dependencies += len(u.Dependencies)
		out.Counts.Units++
		out.Counts.DirectDependencies += len(u.Dependencies)
		out.Counts.InternalDependencies += internal
		out.Counts.LockedComponents += u.LockedCount
		if sp, ok := s.d.Registry.Spec(u.Ecosystem); ok && sp.Language != "" {
			primary[sp.Language]++
		}
	}
	out.Ecosystems = []manifestsapi.EcosystemCount{}
	for _, k := range sortedKeys(eco) {
		out.Ecosystems = append(out.Ecosystems, *eco[k])
	}
	out.Languages = derivedLanguages(f.Technology.Languages, primary)
	return out
}

// LangShare is a language and its share of the repository's primary units (by weight, see deriveTechnology). GitHub's byte counts are not in the
// kernel types (design section 14, D-9), so bytes are reported as 0 (unknown) by the HTTP layer.
type LangShare struct {
	Name  string
	Share float64
}

func languageShares(ordered []string, primary map[string]int) []LangShare {
	total := 0
	for _, n := range primary {
		total += n
	}
	out := make([]LangShare, 0, len(ordered))
	for _, name := range ordered {
		sh := 0.0
		if total > 0 {
			sh = float64(primary[name]) / float64(total)
		}
		out = append(out, LangShare{name, sh})
	}
	return out
}

func derivedLanguages(ordered []string, primary map[string]int) []openapi.LanguageShare {
	out := []openapi.LanguageShare{}
	for _, l := range languageShares(ordered, primary) {
		out = append(out, openapi.LanguageShare{Name: l.Name, Bytes: 0, Share: float32(l.Share)})
	}
	return out
}

// unitInternal counts the edges of one unit that reach an artifact produced by another unit (flag EdgeInternal).
func unitInternal(g *Graph, ri RepoIdx, ui int) int {
	gu, ok := g.UnitOf(ri, ui)
	if !ok {
		return 0
	}
	n := 0
	for _, e := range g.Edges(gu) {
		if e.Flags&EdgeInternal != 0 {
			n++
		}
	}
	return n
}

// artifactKind maps a parser's free-form artifact kind onto the contract's enum (library application plugin bom image other); "" is a library.
func artifactKind(k string) string {
	switch k {
	case "library", "application", "plugin", "bom", "image":
		return k
	case "":
		return "library"
	}
	return "other"
}

func fileParser(f FileRef) string { return f.ParserRef() }

func kindName(k FileKind) string {
	return [...]string{"manifest", "lockfile", "catalog", "workspace"}[min(int(k), 3)]
}

func diagnostics(ds []Diagnostic) []manifestsapi.Diagnostic {
	out := make([]manifestsapi.Diagnostic, 0, len(ds))
	for _, d := range ds {
		out = append(out, manifestsapi.Diagnostic{Code: d.Code, Message: d.Message, Severity: d.Severity.String(), Loc: manifestsapi.Location{Path: d.Loc.Path, Line: d.Loc.Line}})
	}
	return out
}

// ---- listRepoDependencies ----------------------------------------------------------------------------------------------------------

// depRow is one candidate row: either a declared dependency (idx into Unit.Dependencies) or a locked component (idx into the unit's locked list).
type depRow struct {
	eco, name, unit, scope, ver string
	locked                      bool
	ui, idx                     int
}

type depCursor struct {
	Sort string `json:"s"`
	Filt string `json:"f"`
	Tree string `json:"t"`
	Eco  string `json:"e"`
	Name string `json:"n"`
	Unit string `json:"u"`
	Scp  string `json:"c"`
	Ver  string `json:"v"`
	Lock bool   `json:"l"`
	Idx  int    `json:"i"`
}

func (s *Service) handleRepoDeps(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	repo, f, ok := s.repoOf(w, r, v)
	if !ok {
		return
	}
	p := &params{q: r.URL.Query()}
	o := s.cfg.Output
	limit := p.integer("limit", o.DefaultPage, 1, o.MaxPage)
	scopes := p.scopes()
	eco := p.ecosystems()
	rel := p.q.Get("relationship")
	if rel == "" {
		rel = "direct"
	}
	if rel != "direct" && rel != "transitive" && rel != "all" {
		p.bad("relationship", "must be direct, transitive or all")
	}
	internal := p.boolean("internal")
	needle := strings.ToLower(p.text("q", 200))
	sortKey := p.q.Get("sort")
	if sortKey == "" {
		sortKey = "ecosystem"
	}
	if !slices.Contains([]string{"name", "-name", "ecosystem", "-ecosystem", "scope", "-scope"}, sortKey) {
		p.bad("sort", "must be one of name, -name, ecosystem, -ecosystem, scope, -scope")
	}
	wantUnit := ""
	if u := p.text("unit", 420); u != "" {
		sl, un, ok := splitUnitID(u)
		if !ok || sl != repo.Slug {
			p.bad("unit", "must be a unit of this repository (<slug>#<unit>)")
		}
		wantUnit = un
	}
	if pr := p.problem(); pr != nil {
		s.reply(w, r, *pr)
		return
	}
	var cur *depCursor
	filt := httpapi.QueryFingerprint(p.q, "scope", "relationship", "internal", "ecosystem", "unit", "q", "sort")
	if c := p.q.Get("cursor"); c != "" {
		cur = &depCursor{}
		if err := httpapi.DecodeCursor(c, cur); err != nil || cur.Sort != sortKey || cur.Filt != filt || cur.Tree != f.V.TreeSHA {
			s.reply(w, r, httpapi.InvalidCursor())
			return
		}
	}
	fr := s.aggregate(v.now, "manifests.repo", []freshness.Info{f.Info}, func(int) time.Time { return f.V.ParsedAt })
	list := depQuery{slug: repo.Slug, ref: repo.Ref, facts: f.V, graph: v.graph, scopes: scopes, eco: eco, rel: rel, internal: internal, needle: needle,
		sortKey: sortKey, unit: wantUnit, limit: limit, cur: cur, filt: filt}
	out, lockedOK, prob := s.buildDependencies(r.Context(), list, fr)
	if prob != nil {
		s.reply(w, r, *prob)
		return
	}
	etag := ""
	if lockedOK { // a page that lacks evicted lockfile data must not be cached under the snapshot's validator
		etag = s.etagSnapshot(v, fr)
	}
	s.writeJSON(w, r, etag, out)
}

type depQuery struct {
	slug     scm.Slug
	ref      scm.RepoRef
	facts    RepoFacts
	graph    *Graph
	scopes   map[Scope]bool
	eco      map[string]bool
	rel      string
	internal *bool
	needle   string
	sortKey  string
	unit     string
	limit    int
	cur      *depCursor
	filt     string
}

func lessRow(sortKey string) func(a, b depRow) int {
	field := func(r depRow) string {
		switch strings.TrimPrefix(sortKey, "-") {
		case "name":
			return r.name
		case "scope":
			return r.scope
		}
		return r.eco
	}
	desc := strings.HasPrefix(sortKey, "-")
	return func(a, b depRow) int {
		if c := strings.Compare(field(a), field(b)); c != 0 {
			if desc {
				return -c
			}
			return c
		}
		return cmp.Or(strings.Compare(a.eco, b.eco), strings.Compare(a.name, b.name), strings.Compare(a.unit, b.unit), strings.Compare(a.scope, b.scope),
			strings.Compare(a.ver, b.ver), boolCmp(a.locked, b.locked), a.idx-b.idx)
	}
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}

// buildDependencies is the body of listRepoDependencies. It sorts lightweight rows, positions by keyset, and builds DTOs for one page only.
func (s *Service) buildDependencies(ctx context.Context, q depQuery, fr fresh) (manifestsapi.DependencyList, bool, *httpapi.Problem) {
	lockedOK := true
	var rows []depRow
	lockedBy := map[int][]LockedComponent{}
	wantDirect, wantLocked := q.rel == "direct" || q.rel == "all", q.rel == "transitive" || q.rel == "all"
	for ui, u := range q.facts.Units {
		if (q.unit != "" && u.ID != q.unit) || (len(q.eco) > 0 && !q.eco[string(u.Ecosystem)]) {
			continue
		}
		if wantDirect {
			for di, d := range u.Dependencies {
				name := d.TargetDisplay()
				if name == "" {
					name = d.Target.Name
				}
				if (q.scopes != nil && !q.scopes[d.Scope]) || (q.needle != "" && !strings.Contains(strings.ToLower(d.Target.String()+" "+name), q.needle)) {
					continue
				}
				if q.internal != nil && s.isInternal(q.graph, q.slug, u, d) != *q.internal {
					continue
				}
				rows = append(rows, depRow{eco: string(d.Target.Ecosystem), name: name, unit: u.ID, scope: d.Scope.String(), ver: d.Version, ui: ui, idx: di})
			}
		}
		if wantLocked && (q.internal == nil || !*q.internal) {
			lc, ok := s.loadLocked(ctx, q.ref, u, q.facts.TreeSHA)
			if !ok {
				lockedOK = false
			}
			lockedBy[ui] = lc
			for li, l := range lc {
				if l.Relationship == RelDirect {
					continue // already listed as a declared dependency
				}
				if (q.scopes != nil && !q.scopes[l.Scope]) || (q.needle != "" && !strings.Contains(strings.ToLower(l.ID.String()), q.needle)) {
					continue
				}
				rows = append(rows, depRow{eco: string(l.ID.Ecosystem), name: l.ID.Name, unit: u.ID, scope: l.Scope.String(), ver: l.Version, locked: true, ui: ui, idx: li})
			}
		}
	}
	less := lessRow(q.sortKey)
	slices.SortFunc(rows, less)
	start := 0
	if q.cur != nil {
		after := depRow{eco: q.cur.Eco, name: q.cur.Name, unit: q.cur.Unit, scope: q.cur.Scp, ver: q.cur.Ver, locked: q.cur.Lock, idx: q.cur.Idx}
		start, _ = slices.BinarySearchFunc(rows, after, less)
		for start < len(rows) && less(rows[start], after) <= 0 {
			start++
		}
	}
	end := min(start+q.limit, len(rows))
	out := manifestsapi.DependencyList{Repo: string(q.slug), TreeSha: q.facts.TreeSHA, Items: make([]manifestsapi.Dependency, 0, end-start), Freshness: fr.view}
	for _, row := range rows[start:end] {
		u := q.facts.Units[row.ui]
		if row.locked {
			out.Items = append(out.Items, s.lockedDTO(q.slug, u, lockedBy[row.ui][row.idx]))
		} else {
			out.Items = append(out.Items, s.depDTO(q.graph, q.slug, u, u.Dependencies[row.idx]))
		}
	}
	next := ""
	if end < len(rows) {
		last := rows[end-1]
		next = httpapi.EncodeCursor(depCursor{Sort: q.sortKey, Filt: q.filt, Tree: q.facts.TreeSHA, Eco: last.eco, Name: last.name, Unit: last.unit, Scp: last.scope, Ver: last.ver, Lock: last.locked, Idx: last.idx})
	}
	out.Page = httpapi.NewPageInfo(next, q.limit)
	return out, lockedOK, nil
}

// isInternal reports whether a declaration targets an artifact produced by another unit (or by a path inside the repository).
func (s *Service) isInternal(g *Graph, slug scm.Slug, u Unit, d Dependency) bool {
	if d.Target.Namespace == LocalNamespace {
		return true
	}
	if g == nil {
		return false
	}
	self, _ := g.RepoOf(slug)
	for _, p := range g.Producers(d.Target) {
		if pm := g.Unit(p); pm.Repo != self || pm.ID != u.ID {
			return true
		}
	}
	return false
}

func unitFile(u Unit) string {
	for _, f := range u.Files {
		if f.Kind == KindManifest {
			return f.Path
		}
	}
	if len(u.Files) > 0 {
		return u.Files[0].Path
	}
	return ""
}

func (s *Service) depDTO(g *Graph, slug scm.Slug, u Unit, d Dependency) manifestsapi.Dependency {
	display := d.TargetDisplay()
	name := d.Target.Name
	if display != "" {
		name = display
	}
	path := d.Loc().Path
	if path == "" {
		path = unitFile(u)
	}
	src := d.Source
	if src == SourceProject {
		src = SourcePath // an unresolved project reference is shown as a path
	}
	dto := manifestsapi.Dependency{Ecosystem: string(d.Target.Ecosystem), Namespace: ptr(d.Target.Namespace), Name: name, Requirement: d.Requirement, Version: ptr(d.Version),
		Resolution: resolutionName(d.Resolution), Scope: d.Scope.String(), Group: d.Group, Relationship: "direct", Optional: d.Optional, Condition: ptr(d.Condition()),
		Source: sourceName(src), SourceRef: ptr(d.SourceRef()), Confidence: confidenceName(d.Confidence), Loc: manifestsapi.Location{Path: path, Line: int(d.Line)},
		Unit: unitID(slug, u.ID), Provenance: "own"}
	if d.Target.Namespace == LocalNamespace {
		dto.Namespace, dto.Name = nil, d.Target.Name
	}
	dto.Purl = s.purl(d.Target, display, d.Version, d.Qualifiers())
	if g != nil {
		dto.Internal = s.internalRef(g, slug, u, d)
	}
	return dto
}

// internalRef names the first producer of the target outside the declaring unit (units are ordered by repository slug, then unit order).
func (s *Service) internalRef(g *Graph, slug scm.Slug, u Unit, d Dependency) *manifestsapi.InternalRef {
	self, _ := g.RepoOf(slug)
	if d.Target.Namespace == LocalNamespace {
		pth := d.SourceRef()
		if pth == "" {
			pth = d.Target.Name
		}
		lo, hi := g.UnitRange(self)
		for gu := lo; gu < hi; gu++ {
			if m := g.Unit(gu); m.Dir == pth && m.ID != u.ID {
				return &manifestsapi.InternalRef{Repo: string(slug), Unit: unitID(slug, m.ID)}
			}
		}
		return nil
	}
	ci, ok := g.CompIndex(d.Target)
	if !ok {
		return nil
	}
	for _, pr := range g.ProducerRefs(ci) {
		if m := g.Unit(pr.Unit); m.Repo != self || m.ID != u.ID {
			return &manifestsapi.InternalRef{Repo: string(g.Repo(m.Repo).Slug), Unit: unitID(g.Repo(m.Repo).Slug, m.ID), Version: ptr(pr.Version)}
		}
	}
	return nil
}

func (s *Service) lockedDTO(slug scm.Slug, u Unit, c LockedComponent) manifestsapi.Dependency {
	path := c.Loc.Path
	if path == "" {
		for _, f := range u.Files {
			if f.Kind == KindLockfile {
				path = f.Path
				break
			}
		}
	}
	dto := manifestsapi.Dependency{Ecosystem: string(c.ID.Ecosystem), Namespace: ptr(c.ID.Namespace), Name: c.ID.Name, Version: ptr(c.Version), Resolution: "locked",
		Scope: c.Scope.String(), Relationship: "transitive", Source: "registry", Confidence: "exact", Loc: manifestsapi.Location{Path: path, Line: c.Loc.Line},
		Unit: unitID(slug, u.ID), Provenance: provenanceOf(c.Source)}
	dto.Purl = s.purl(c.ID, "", c.Version, "")
	return dto
}

// loadLocked reads a unit's locked components from the bounded cache; false means evicted or never stored (a refresh is requested).
func (s *Service) loadLocked(ctx context.Context, ref scm.RepoRef, u Unit, tree string) ([]LockedComponent, bool) {
	if u.LockedCount == 0 {
		return nil, true
	}
	b, ok, err := s.d.Store.GetCache(ctx, NSLocked, lockedKey(store.KeyOf(ref), u.ID, tree))
	if err != nil || !ok {
		s.m.inc(s.m.lockedCache, "result", "miss")
		s.NoteLockMiss(store.KeyOf(ref))
		return nil, false
	}
	lc, err := DecodeLocked(b)
	if err != nil {
		s.m.inc(s.m.lockedCache, "result", "corrupt")
		return nil, false
	}
	s.m.inc(s.m.lockedCache, "result", "hit")
	return lc, true
}

func resolutionName(r Resolution) string {
	return [...]string{"declared", "pinned", "locked", "managed"}[min(int(r), 3)]
}
func confidenceName(c Confidence) string {
	if c == ConfHeuristic {
		return "heuristic"
	}
	return "exact"
}
func sourceName(k SourceKind) string {
	return [...]string{"registry", "git", "path", "url", "path"}[min(int(k), 4)]
}

// ---- getRepoSbom -------------------------------------------------------------------------------------------------------------------

func (s *Service) handleSBOM(w http.ResponseWriter, r *http.Request) {
	v := s.view(r)
	repo, f, ok := s.repoOf(w, r, v)
	if !ok {
		return
	}
	// The body is a function of the tree, the parser set that read it and the repository identity (root component, serial number), so the tree-sha
	// validator carries a digest of the last two (a parser bump re-reads the same tree into a different document).
	etag := httpapi.WithDigest(httpapi.WeakETagOf(httpapi.ETagKind('t'), f.V.TreeSHA, classOrd(v.class)), httpapi.Digest(f.V.ParserSet, repo.Ref.Key()))
	locked := map[string][]LockedComponent{}
	omitted := map[string]bool{}
	complete := true
	budget := s.cfg.Output.SBOMMaxLocked
	for _, u := range f.V.Units {
		if u.Role == RoleFixture || u.LockedCount == 0 {
			continue
		}
		if u.LockedCount > budget { // deterministic cut: units in order, a unit that does not fit as a whole is left out as a whole
			omitted[u.ID] = true
			continue
		}
		budget -= u.LockedCount
		lc, ok := s.loadLocked(r.Context(), repo.Ref, u, f.V.TreeSHA)
		complete = complete && ok
		if len(lc) > 0 {
			locked[u.ID] = lc
		}
	}
	if complete && httpapi.NotModified(r, etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var buf bytes.Buffer
	if err := WriteBOM(&buf, BuildBOM(s.d.Registry, repo, f.V, locked, omitted, s.d.Version)); err != nil {
		s.reply(w, r, httpapi.Internal())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json; version=1.7")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(string(repo.Slug))+`.cdx.json"`)
	if complete { // a document missing evicted lockfile data must not be cached under the tree's validator
		w.Header().Set("ETag", etag)
	}
	_, _ = w.Write(buf.Bytes())
}

func safeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
