package maven

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/expand"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

const (
	maxParentDepth = 32
	maxImportDepth = 8
	maxFetches     = 64
	runExpandBytes = 64 << 20
)

type assembler struct {
	src   manifests.PomSource
	allow func(manifests.ComponentID) bool // privacy guard: true when the coordinate may leave the process
}

func NewAssembler(opts ...Option) manifests.Assembler {
	a := &assembler{}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (*assembler) Ecosystem() manifests.EcosystemID { return manifests.Maven }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
// The units depend on whether a PomSource is wired (parents and BOMs outside the repository are resolved or not), and that is not part of the tree
// SHA the root uses for change detection, so the enabled state is folded into the version: switching manifests.pom_source.enabled changes
// Registry.SetHash and the root rebuilds the stored facts (verification VF-12-05).
func (a *assembler) AssemblerVersion() int {
	if a.src != nil && a.allow != nil {
		return assemblerVer | pomSourceSalt
	}
	return assemblerVer
}

const (
	assemblerVer  = 1
	pomSourceSalt = 1 << 16
)

// pomNode is one raw POM: a repository file or a PomSource result.
type pomNode struct {
	p       *manifests.Parsed
	dir     string
	fetched bool
	parent  *pomNode
	linked  bool
	eff     *eff
	diags   []manifests.Diagnostic // link-time diagnostics
}

func (n *pomNode) prov() (g, a, v string) {
	if len(n.p.Provides) > 0 {
		x := n.p.Provides[0]
		g, a, v = x.ID.Namespace, x.ID.Name, x.Version
	}
	if n.p.Parent != nil {
		if g == "" {
			g = n.p.Parent.Target.Namespace
		}
		if v == "" {
			v = n.p.Parent.Requirement
		}
	}
	return
}

type run struct {
	ctx      context.Context
	a        *assembler
	files    manifests.FileSet
	byPath   map[string]*pomNode
	byGA     map[string][]*pomNode
	cfg      map[string]*manifests.Parsed // maven.config by project root dir
	fetched  map[string]*pomNode
	fetches  int
	shared   int
	nodeList []*pomNode
}

func ga(g, a string) string { return g + ":" + a }

func (a *assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	r := &run{ctx: ctx, a: a, files: files, byPath: map[string]*pomNode{}, byGA: map[string][]*pomNode{}, cfg: map[string]*manifests.Parsed{}, fetched: map[string]*pomNode{}, shared: runExpandBytes}
	for i := range parsed {
		p := &parsed[i]
		switch p.File.ParserID {
		case PomID:
			n := &pomNode{p: p, dir: repopath.Dir(p.File.Path)}
			r.byPath[p.File.Path] = n
			r.nodeList = append(r.nodeList, n)
		case ConfigID:
			r.cfg[repopath.Dir(repopath.Dir(p.File.Path))] = p // <root>/.mvn/maven.config
		}
	}
	sort.Slice(r.nodeList, func(i, j int) bool { return r.nodeList[i].p.File.Path < r.nodeList[j].p.File.Path })
	for _, n := range r.nodeList {
		if len(n.p.Provides) == 0 && n.p.Parent == nil {
			continue
		}
		g, a, _ := n.prov()
		if a != "" {
			r.byGA[ga(g, a)] = append(r.byGA[ga(g, a)], n)
		}
	}
	for _, n := range r.nodeList {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.link(n)
	}
	var units []manifests.Unit
	for _, n := range r.nodeList {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(n.p.Provides) == 0 { // not a project (no artifactId)
			continue
		}
		units = append(units, r.unit(n))
	}
	// A PomSource call that ended because the assembly context ended is recorded as "not found" inside the last unit; never publish that as facts.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return units, nil
}

func isLiteral(s string) bool { return s != "" && !strings.Contains(s, "${") }

// safeCoordinate reports whether a coordinate may be handed to the PomSource at all. The satisfier turns it into a URL path, so a value from a
// hostile POM must not be able to carry path or URL syntax (".." segments, "?", "#", "%", a newline) even though the host is allow-listed. The
// alphabets are Maven's own: groupId and artifactId [A-Za-z0-9_.-], versions additionally "+"; dotted group segments must be non-empty.
func safeCoordinate(group, artifact, version string) bool {
	ok := func(s string, extra byte) bool {
		if s == "" || len(s) > 200 || s == "." || s == ".." {
			return false
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || (extra != 0 && c == extra)) {
				return false
			}
		}
		return true
	}
	if !ok(group, 0) || !ok(artifact, 0) || !ok(version, '+') {
		return false
	}
	for _, seg := range strings.Split(group, ".") {
		if seg == "" {
			return false
		}
	}
	return true
}

// link resolves the parent of n among repository POMs. External parents are resolved lazily (PomSource) in buildEff.
func (r *run) link(n *pomNode) {
	if n.linked {
		return
	}
	n.linked = true
	pd := n.p.Parent
	if pd == nil {
		return
	}
	pg, pa, pv := pd.Target.Namespace, pd.Target.Name, pd.Requirement
	rel, hasRel := n.p.Properties["parent.relativePath"]
	if !hasRel {
		rel = "../pom.xml"
	}
	matches := func(c *pomNode) bool {
		cg, ca, cv := c.prov()
		if ca != pa || (isLiteral(cg) && isLiteral(pg) && cg != pg) {
			return false
		}
		if isLiteral(cv) && isLiteral(pv) && cv != pv {
			return false
		}
		return true
	}
	if rel != "" && !n.fetched {
		target := rel
		if !strings.HasSuffix(target, ".xml") && !strings.HasSuffix(target, ".pom") {
			target = path.Join(target, "pom.xml")
		}
		if full, ok := repopath.Join(n.dir, target); ok {
			if c := r.byPath[full]; c != nil && c != n {
				if matches(c) {
					n.parent = c
					return
				}
				n.diags = append(n.diags, diagAt("maven.parent-mismatch", n.p.File.Path, int(pd.Line), "%s is not %s:%s", manifests.CleanText(full, 120), manifests.CleanText(pg, 60), manifests.CleanText(pa, 60)))
			}
		} else {
			n.diags = append(n.diags, diagAt("maven.relative-path-escape", n.p.File.Path, int(pd.Line), "relativePath %q leaves the repository", manifests.CleanText(rel, 120)))
		}
	}
	// reactor coordinates
	var cands []*pomNode
	for _, c := range r.byGA[ga(pg, pa)] {
		if c != n && matches(c) {
			cands = append(cands, c)
		}
	}
	if len(cands) == 1 {
		n.parent = cands[0]
	} else if len(cands) > 1 {
		for _, c := range cands {
			if _, _, cv := c.prov(); cv == pv {
				n.parent = c
				return
			}
		}
		n.parent = cands[0]
	}
}

// fetch asks the PomSource for a POM, honouring the privacy rule, the budget and the cache. The reason explains a nil result.
func (r *run) fetch(id manifests.ComponentID, version string, into *[]manifests.Diagnostic, file string, line int) (*pomNode, string) {
	if r.a.src == nil || r.a.allow == nil {
		return nil, "no PomSource configured"
	}
	if !isLiteral(id.Namespace) || !isLiteral(id.Name) || !isLiteral(version) {
		return nil, "coordinates contain unresolved placeholders"
	}
	if !safeCoordinate(id.Namespace, id.Name, version) {
		return nil, "coordinates are not valid Maven identifiers"
	}
	key := id.Namespace + ":" + id.Name + ":" + version
	if n, ok := r.fetched[key]; ok {
		if n == nil {
			return nil, "not available"
		}
		return n, ""
	}
	r.fetched[key] = nil
	if !r.a.allow(id) {
		*into = append(*into, diagAt("maven.snapshot-private-skipped", file, line, "%s is not looked up", manifests.CleanText(id.Namespace+":"+id.Name, 120)))
		return nil, "private coordinate"
	}
	if r.fetches >= maxFetches {
		return nil, "fetch budget exhausted"
	}
	r.fetches++
	b, err := r.a.src.Fetch(r.ctx, id, version)
	if err != nil || len(b) == 0 {
		return nil, "not found"
	}
	fp := "pomsource:" + key
	p, _ := NewPom().Parse(r.ctx, manifests.File{Path: fp, Data: b}, manifests.Limits{})
	p.File = manifests.FileRef{Path: fp, ParserID: PomID, ParserVersion: parserVer} // the runner stamps repository files; this one came from the PomSource
	if len(p.Provides) == 0 && p.Parent == nil {
		*into = append(*into, diagAt("maven.pom-source-failed", file, line, "%s is not a POM", manifests.CleanText(key, 120)))
		return nil, "not a POM"
	}
	n := &pomNode{p: &p, fetched: true}
	r.fetched[key] = n
	*into = append(*into, diagAt("maven.parent-fetched", file, line, "%s supplied by the PomSource", manifests.CleanText(key, 120)))
	r.link(n)
	return n, ""
}

func errString(err error) string {
	if err == nil {
		return "empty"
	}
	return err.Error()
}

// ---- effective model ----

type eff struct {
	n                *pomNode
	parent           *eff
	chain            []*eff // root first, ends with e
	props            map[string]string
	cli              map[string]string
	memo             map[string]string
	exp              *expand.Expander
	active           map[string]bool // active profile ids of this POM
	diags            []manifests.Diagnostic
	mgmt             map[string]mEntry
	mgmtOK           bool
	cyclic           bool
	unresolvedParent bool
	lastWhy          string
	incomplete       bool       // parent or BOM could not be resolved: management is a lower bound
	used             []*pomNode // local BOM POMs imported
}

type mEntry struct {
	version, scope string
	optional       bool
	file           string
	line           int
}

func (r *run) effOf(n *pomNode, depth int, seen map[*pomNode]bool) *eff {
	if n.eff != nil {
		return n.eff
	}
	e := &eff{n: n, memo: map[string]string{}, exp: expand.New(&r.shared)}
	n.eff = e
	seen[n] = true
	defer delete(seen, n)
	root := r.rootDir(n)
	if c := r.cfg[root]; c != nil {
		e.cli = map[string]string{}
		for k, v := range c.Properties {
			if strings.HasPrefix(k, "prop:") {
				e.cli[strings.TrimPrefix(k, "prop:")] = v
			}
		}
	}
	// parent
	pn := n.parent
	if pn == nil && n.p.Parent != nil {
		pd := n.p.Parent
		ver := pd.Requirement
		// expand the parent version with this POM's own properties only (CI-friendly ${revision})
		ver = r.expandPlain(n, ver)
		if f, why := r.fetch(pd.Target, ver, &e.diags, n.p.File.Path, int(pd.Line)); f != nil {
			pn = f
			n.parent = f
		} else {
			e.unresolvedParent = true
			e.diags = append(e.diags, diagAt("maven.parent-unresolved", n.p.File.Path, int(pd.Line), "parent %s:%s:%s (%s)", manifests.CleanText(pd.Target.Namespace, 60), manifests.CleanText(pd.Target.Name, 60), manifests.CleanText(ver, 40), why))
		}
	}
	if pn != nil {
		if seen[pn] || depth >= maxParentDepth {
			e.cyclic = true
			e.diags = append(e.diags, diagAt("maven.parent-cycle", n.p.File.Path, 0, "parent chain of %s is cyclic or deeper than %d", manifests.CleanText(n.p.File.Path, 100), maxParentDepth))
			n.parent = nil
		} else {
			e.parent = r.effOf(pn, depth+1, seen)
		}
	}
	e.props = map[string]string{}
	if e.parent != nil {
		for k, v := range e.parent.props {
			e.props[k] = v
		}
		e.chain = append(append([]*eff{}, e.parent.chain...), e)
	} else {
		e.chain = []*eff{e}
	}
	e.active = r.activeProfiles(n, e)
	for k, v := range n.p.Properties {
		if strings.HasPrefix(k, "prop:") {
			e.props[strings.TrimPrefix(k, "prop:")] = v
		}
	}
	ids := make([]string, 0, len(e.active))
	for id := range e.active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pfx := "profile:" + id + ":prop:"
		for k, v := range n.p.Properties {
			if strings.HasPrefix(k, pfx) {
				e.props[strings.TrimPrefix(k, pfx)] = v
			}
		}
	}
	return e
}

// rootDir finds the multi-module root of a POM: the nearest ancestor directory holding a fetched .mvn/maven.config.
func (r *run) rootDir(n *pomNode) string {
	d := n.dir
	for {
		if _, ok := r.cfg[d]; ok {
			return d
		}
		if d == "" {
			return ""
		}
		d = repopath.Dir(d)
	}
}

// expandPlain expands s with only the POM's own properties and the CLI properties (used before the chain exists).
func (r *run) expandPlain(n *pomNode, s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	cli := map[string]string{}
	if c := r.cfg[r.rootDir(n)]; c != nil {
		for k, v := range c.Properties {
			if strings.HasPrefix(k, "prop:") {
				cli[strings.TrimPrefix(k, "prop:")] = v
			}
		}
	}
	look := func(name string) (string, bool) {
		if v, ok := cli[name]; ok {
			return v, true
		}
		if v, ok := n.p.Properties["prop:"+name]; ok {
			return v, true
		}
		return "", false
	}
	ex := &expand.Expander{Shared: &r.shared, Budget: limits.DefaultMaxExpand, MaxValue: 64 << 10}
	out, _, _, err := ex.Expand(s, look, map[string]string{}, nil)
	if err != nil {
		return s
	}
	return out
}

// activeProfiles evaluates the profiles of one POM against .mvn/maven.config (profiles and -D properties) only.
func (r *run) activeProfiles(n *pomNode, e *eff) map[string]bool {
	out := map[string]bool{}
	idsRaw := n.p.Properties["profile-ids"]
	if idsRaw == "" {
		return out
	}
	ids := strings.Split(idsRaw, "\t")
	explicit, denied := map[string]bool{}, map[string]bool{}
	if c := r.cfg[r.rootDir(n)]; c != nil {
		for _, p := range strings.Split(c.Properties["profiles"], ",") {
			switch {
			case strings.HasPrefix(p, "!") || strings.HasPrefix(p, "-"):
				denied[p[1:]] = true
			case p != "":
				explicit[p] = true
			}
		}
	}
	other := false
	byOther := map[string]bool{}
	for _, id := range ids {
		if denied[id] {
			continue
		}
		act := explicit[id]
		if pv, ok := n.p.Properties["profile:"+id+":property"]; ok && !act {
			act = propertyActive(pv, e.cli)
		}
		if act {
			byOther[id] = true
			other = true
		}
	}
	for _, id := range ids {
		if byOther[id] {
			out[id] = true
		} else if !denied[id] && !other && n.p.Properties["profile:"+id+":activeByDefault"] == "true" {
			out[id] = true
		}
	}
	return out
}

func propertyActive(spec string, cli map[string]string) bool {
	name, val, _ := strings.Cut(spec, "=")
	neg := strings.HasPrefix(name, "!")
	name = strings.TrimPrefix(name, "!")
	if name == "" {
		return false
	}
	cur, present := cli[name]
	if neg {
		return !present
	}
	if val == "" {
		return present
	}
	if strings.HasPrefix(val, "!") {
		return !present || cur != val[1:]
	}
	return present && cur == val
}

// ---- interpolation in the context of an effective model ----

func (e *eff) implicit(k string) (string, bool) {
	g, a, v := e.n.prov()
	pk := e.n.p.Parent
	switch k {
	case "groupId":
		return g, g != ""
	case "artifactId":
		return a, a != ""
	case "version":
		return v, v != ""
	case "packaging":
		return e.n.p.Properties["packaging"], true
	case "name":
		n, ok := e.n.p.Properties["name"]
		return n, ok
	case "parent.groupId":
		if pk != nil {
			return pk.Target.Namespace, true
		}
	case "parent.artifactId":
		if pk != nil {
			return pk.Target.Name, true
		}
	case "parent.version":
		if pk != nil {
			return pk.Requirement, true
		}
	}
	if strings.HasPrefix(k, "properties.") {
		v, ok := e.props[strings.TrimPrefix(k, "properties.")]
		return v, ok
	}
	return "", false
}

func (e *eff) lookup(name string) (string, bool) {
	if v, ok := e.cli[name]; ok {
		return v, true
	}
	switch {
	case strings.HasPrefix(name, "project."):
		if v, ok := e.implicit(strings.TrimPrefix(name, "project.")); ok {
			return v, true
		}
	case strings.HasPrefix(name, "pom."):
		if v, ok := e.implicit(strings.TrimPrefix(name, "pom.")); ok {
			return v, true
		}
	case name == "groupId" || name == "artifactId" || name == "version":
		if _, ok := e.props[name]; !ok {
			return e.implicit(name)
		}
	}
	v, ok := e.props[name]
	return v, ok
}

// interp expands s in e's context and records unresolved/cyclic names into rec.
func (e *eff) interp(s string, rec *recorder, file string, line int) string {
	if !strings.Contains(s, "${") {
		return s
	}
	out, unres, cyc, err := e.exp.Expand(s, e.lookup, e.memo, nil)
	if err != nil {
		rec.add(diagAt("limit.expand", file, line, "property expansion budget exceeded"))
		return s
	}
	for _, u := range unres {
		rec.unresolved(u, file, line)
	}
	for _, c := range cyc {
		rec.add(diagAt("maven.property-cycle", file, line, "property %s refers to itself", manifests.CleanText(c, 80)))
	}
	return out
}

type recorder struct {
	diags []manifests.Diagnostic
	seen  map[string]bool
	count map[string]int
	unres int
}

const maxPerCode = 8

func (r *recorder) add(d manifests.Diagnostic) {
	k := d.Code + "|" + d.Loc.Path + "|" + d.Message
	if r.seen == nil {
		r.seen = map[string]bool{}
		r.count = map[string]int{}
	}
	if r.seen[k] {
		return
	}
	r.seen[k] = true
	r.count[d.Code]++
	if r.count[d.Code] > maxPerCode {
		return
	}
	r.diags = append(r.diags, d)
}

// flush appends one summary diagnostic per code that exceeded the cap.
func (r *recorder) flush(path string) {
	codes := make([]string, 0, len(r.count))
	for c, n := range r.count {
		if n > maxPerCode {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	for _, c := range codes {
		r.diags = append(r.diags, diagAt(c, path, 0, "%d more %s diagnostics suppressed", r.count[c]-maxPerCode, c))
	}
}

func (r *recorder) unresolved(name, file string, line int) {
	if r.seen == nil {
		r.seen = map[string]bool{}
		r.count = map[string]int{}
	}
	if r.seen["u|"+name] || r.unres >= 20 {
		return
	}
	r.seen["u|"+name] = true
	r.unres++
	r.diags = append(r.diags, diagAt("maven.property-unresolved", file, line, "${%s}", manifests.CleanText(name, 80)))
}
