package maven

import (
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

// condActive reports whether a Condition ("" or "profile:<id>") holds for the POM x of the chain.
func (x *eff) condActive(cond string) bool {
	if cond == "" {
		return true
	}
	return x.active[strings.TrimPrefix(cond, "profile:")]
}

func (x *eff) condLabel(cond string) string {
	if cond == "" {
		return ""
	}
	if x.condActive(cond) {
		return cond + "=active"
	}
	return cond
}

func depKeyOf(g, a, q string) string { return g + ":" + a + ":" + q }

// findPom locates a POM by coordinates: the reactor first, then the PomSource.
func (r *run) findPom(id manifests.ComponentID, version string, e *eff) *pomNode {
	var cands []*pomNode
	for _, c := range r.byGA[ga(id.Namespace, id.Name)] {
		cands = append(cands, c)
	}
	for _, c := range cands {
		if _, _, cv := c.prov(); cv == version {
			return c
		}
	}
	if len(cands) == 1 {
		if _, _, cv := cands[0].prov(); !isLiteral(cv) {
			return cands[0]
		}
	}
	n, why := r.fetch(id, version, &e.diags, e.n.p.File.Path, 0)
	if n == nil {
		e.lastWhy = why
	}
	return n
}

// mgmtOf computes the dependencyManagement of e: inherited entries interpolated in e's own context, imported BOMs in
// their own context, direct entries beating imports (Maven model builder order).
func (r *run) mgmtOf(e *eff, depth int, stack map[*eff]bool) map[string]mEntry {
	if e.mgmtOK {
		return e.mgmt
	}
	m := map[string]mEntry{}
	if stack[e] || depth > maxImportDepth {
		e.diags = append(e.diags, diagAt("maven.bom-cycle", e.n.p.File.Path, 0, "BOM imports are cyclic or deeper than %d", maxImportDepth))
		return m
	}
	stack[e] = true
	defer delete(stack, e)
	rec := &recorder{}
	for _, x := range e.chain {
		for _, d := range x.n.p.Dependencies {
			if d.Group != "managed" || !x.condActive(d.Condition()) {
				continue
			}
			f, l := x.n.p.File.Path, int(d.Line)
			g, a := e.interp(d.Target.Namespace, rec, f, l), e.interp(d.Target.Name, rec, f, l)
			q := e.interp(d.Qualifiers(), rec, f, l)
			m[depKeyOf(g, a, q)] = mEntry{version: e.interp(d.Requirement, rec, f, l), scope: scopeName(d.Scope), optional: d.Optional, file: f, line: l}
		}
		for _, imp := range x.n.p.Imports {
			if !x.condActive(imp.Condition()) {
				continue
			}
			f, l := x.n.p.File.Path, int(imp.Line)
			id := manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: e.interp(imp.Target.Namespace, rec, f, l), Name: e.interp(imp.Target.Name, rec, f, l)}
			ver := e.interp(imp.Requirement, rec, f, l)
			bn := r.findPom(id, ver, e)
			if bn == nil {
				e.incomplete = true
				e.diags = append(e.diags, diagAt("maven.bom-unresolved", f, l, "BOM %s:%s (%s)", manifests.CleanText(id.Namespace+":"+id.Name, 100), manifests.CleanText(ver, 40), e.lastWhy))
				continue
			}
			be := r.effOf(bn, 0, map[*pomNode]bool{})
			bm := r.mgmtOf(be, depth+1, stack)
			e.diags = append(e.diags, be.diags...)
			if be.incomplete || be.unresolvedParent {
				e.incomplete = true
			}
			if !bn.fetched {
				e.used = append(e.used, bn)
			}
			keys := make([]string, 0, len(bm))
			for k := range bm {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if _, ok := m[k]; !ok {
					m[k] = bm[k]
				}
			}
		}
	}
	e.diags = append(e.diags, rec.diags...)
	e.mgmt, e.mgmtOK = m, true
	return m
}

func scopeName(s manifests.Scope) string {
	switch s {
	case manifests.ScopeRuntime:
		return "compile"
	case manifests.ScopeTest:
		return "test"
	case manifests.ScopeProvided:
		return "provided"
	}
	return ""
}

func scopeFromName(s string) manifests.Scope {
	if sc, ok := scopeOf(s); ok {
		return sc
	}
	return manifests.ScopeRuntime
}

func isRange(v string) bool { return strings.HasPrefix(v, "[") || strings.HasPrefix(v, "(") }

// exactOf returns the pinned version of a version spec: a plain soft requirement or a single-point range "[1.2]".
func exactOf(v string) (string, bool) {
	if v == "" || strings.Contains(v, "${") {
		return "", false
	}
	if !isRange(v) {
		if v == "LATEST" || v == "RELEASE" {
			return "", false
		}
		return v, true
	}
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") && !strings.Contains(v, ",") {
		return strings.TrimSpace(v[1 : len(v)-1]), true
	}
	return "", false
}

func (r *run) unit(n *pomNode) manifests.Unit {
	e := r.effOf(n, 0, map[*pomNode]bool{})
	rec := &recorder{}
	for _, d := range n.diags {
		rec.add(d)
	}
	u := manifests.Unit{Ecosystem: manifests.Maven, Dir: dirOrDot(n.dir)}
	g0, a0, v0 := n.prov()
	f := n.p.File.Path
	// provides
	g, a, v := e.interp(g0, rec, f, 1), e.interp(a0, rec, f, 1), e.interp(v0, rec, f, 1)
	u.Provides = append(u.Provides, manifests.Artifact{ID: manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: g, Name: a}, Display: g + ":" + a, Version: v, Kind: kindOf(n)})
	// files
	u.Files = append(u.Files, n.p.File)
	for _, x := range e.chain[:len(e.chain)-1] {
		if !x.n.fetched {
			u.Files = append(u.Files, x.n.p.File)
		}
	}
	if c := r.cfg[r.rootDir(n)]; c != nil {
		u.Files = append(u.Files, c.File)
		for _, d := range c.Diagnostics {
			rec.add(d)
		}
	}
	m := r.mgmtOf(e, 0, map[*eff]bool{})
	for _, bn := range e.used {
		u.Files = append(u.Files, bn.p.File)
	}
	// plugin management (version only), merged along the chain
	pm := map[string]string{}
	for _, x := range e.chain {
		for _, d := range x.n.p.Dependencies {
			if d.Group == "plugin-managed" && x.condActive(d.Condition()) {
				pf, pl := x.n.p.File.Path, int(d.Line)
				pm[e.interp(d.Target.Namespace, rec, pf, pl)+":"+e.interp(d.Target.Name, rec, pf, pl)] = e.interp(d.Requirement, rec, pf, pl)
			}
		}
	}
	// effective dependencies: chain root first, a child entry replaces an inherited one with the same key
	type slot struct {
		d  manifests.Dependency
		x  *eff
		ok bool
	}
	var order []string
	slots := map[string]*slot{}
	for _, x := range e.chain {
		for _, d := range x.n.p.Dependencies {
			if d.Group == "managed" || d.Group == "plugin-managed" {
				continue
			}
			d = d.Clone() // Parsed values are shared and cached
			l, fl := int(d.Line), x.n.p.File.Path
			dg, da := e.interp(d.Target.Namespace, rec, fl, l), e.interp(d.Target.Name, rec, fl, l)
			q := e.interp(d.Qualifiers(), rec, fl, l)
			k := d.Group + "|" + d.Condition() + "|" + depKeyOf(dg, da, q)
			d.Target = manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: dg, Name: da}
			d.SetTargetDisplay(dg + ":" + da)
			d.SetQualifiers(q)
			if x.n != n { // inherited from a parent POM: say which file the line belongs to
				d.SetLocPath(fl)
			}
			if old, ok := slots[k]; ok {
				if d.Group == "plugin" && d.Requirement == "" {
					d.Requirement = old.d.Requirement
				}
				*old = slot{d: d, x: x}
				continue
			}
			slots[k] = &slot{d: d, x: x}
			order = append(order, k)
		}
	}
	for _, k := range order {
		s := slots[k]
		d, x := s.d, s.x
		fl, l := x.n.p.File.Path, int(d.Line)
		if c := x.condLabel(d.Condition()); c != "" {
			d.SetCondition(c)
		}
		ver := e.interp(d.Requirement, rec, fl, l)
		var ent mEntry
		managed := false
		if d.Group == "" {
			ent, managed = m[depKeyOf(d.Target.Namespace, d.Target.Name, d.Qualifiers())]
		} else if d.Group == "plugin" && ver == "" {
			if mv, ok := pm[d.Target.Namespace+":"+d.Target.Name]; ok {
				ent, managed = mEntry{version: mv}, true
			}
		}
		if d.Group == "" && d.Scope == manifests.ScopeUnknown {
			if managed && ent.scope != "" {
				d.Scope = scopeFromName(ent.scope)
			} else {
				d.Scope = manifests.ScopeRuntime
			}
		}
		if managed && ent.optional && d.Group == "" {
			d.Optional = true
		}
		switch {
		case ver != "":
			d.Requirement = ver
			if pv, ok := exactOf(ver); ok {
				d.Version, d.Resolution = pv, manifests.ResPinned
			}
		case managed && ent.version != "":
			d.Requirement, d.Resolution = ent.version, manifests.ResManaged
			if pv, ok := exactOf(ent.version); ok {
				d.Version = pv
			}
		default:
			d.Requirement = ""
			if d.Group == "" && !e.unresolvedParent && !anyIncomplete(e) {
				rec.add(diagAt("maven.version-unmanaged", fl, l, "%s has no version and no management supplies one", manifests.CleanText(d.TargetDisplay(), 120)))
			}
		}
		d.Version = manifests.CleanText(d.Version, 128)
		d.Requirement = manifests.CleanText(d.Requirement, 200)
		u.Dependencies = append(u.Dependencies, d)
	}
	// members
	seenM := map[string]bool{}
	for _, mod := range n.p.Members {
		target := strings.TrimSpace(mod)
		file := path0(target)
		dir := target
		if strings.HasSuffix(target, ".xml") || strings.HasSuffix(target, ".pom") {
			dir = strings.TrimSuffix(target, "/"+file)
			if dir == target {
				dir = "."
			}
		} else {
			file = "pom.xml"
		}
		full, ok := repopath.Join(n.dir, dir)
		if !ok {
			rec.add(diagAt("maven.module-missing", f, 0, "module %q leaves the repository", manifests.CleanText(mod, 100)))
			continue
		}
		pp := file
		if full != "" {
			pp = full + "/" + file
		}
		if r.byPath[pp] == nil {
			rec.add(diagAt("maven.module-missing", f, 0, "module %q has no fetched POM", manifests.CleanText(mod, 100)))
			continue
		}
		if !seenM[full] {
			seenM[full] = true
			u.Members = append(u.Members, dirOrDot(full))
		}
	}
	sort.Strings(u.Members)
	// licences: the nearest declaring POM of the chain
	for i := len(e.chain) - 1; i >= 0; i-- {
		if ls := e.chain[i].n.p.Licenses; len(ls) > 0 {
			u.Licenses = append(u.Licenses, ls...)
			break
		}
	}
	// toolchain
	tc := map[string]string{"tool": "maven", "language": "java", "packaging": n.p.Properties["packaging"]}
	for _, x := range e.chain {
		if x.n.p.Toolchain["language"] == "kotlin" {
			tc["language"] = "kotlin"
		}
	}
	for _, d := range u.Dependencies {
		if d.Target.Namespace == "org.jetbrains.kotlin" && strings.HasPrefix(d.Target.Name, "kotlin-stdlib") {
			tc["language"] = "kotlin"
		}
	}
	if tc["language"] == "kotlin" {
		tc["kotlin"] = e.kotlinVersion(u.Dependencies) // the core adds the language Kotlin from this key or from org.jetbrains.kotlin dependencies
	}
	jv := ""
	for _, k := range []string{"maven.compiler.release", "maven.compiler.target", "maven.compiler.source", "java.version", "jdk.version", "java.release"} {
		if v, ok := e.lookup(k); ok {
			jv = e.interp(v, rec, f, 0)
			break
		}
	}
	if jv == "" {
		for i := len(e.chain) - 1; i >= 0 && jv == ""; i-- {
			for _, k := range []string{"compiler.release", "compiler.target", "compiler.source"} {
				if v := e.chain[i].n.p.Toolchain[k]; v != "" {
					jv = e.interp(v, rec, f, 0)
					break
				}
			}
		}
	}
	if jv != "" && !strings.Contains(jv, "${") {
		tc["java"] = manifests.CleanText(jv, 20)
	}
	u.Toolchain = tc
	// diagnostics: parsed, link-time, effective (own chain), recorded
	for _, x := range e.chain {
		for _, d := range x.n.p.Diagnostics {
			rec.add(d)
		}
		for _, d := range x.diags {
			rec.add(d)
		}
		for _, d := range x.n.diags {
			rec.add(d)
		}
	}
	// profile activation that could not be evaluated
	for _, x := range e.chain {
		for _, id := range strings.Split(x.n.p.Properties["profile-ids"], "\t") {
			if id == "" || x.active[id] {
				continue
			}
			if x.n.p.Properties["profile:"+id+":jdk"] != "" || x.n.p.Properties["profile:"+id+":os"] != "" || x.n.p.Properties["profile:"+id+":file"] != "" {
				rec.add(diagAt("maven.profile-unknown", x.n.p.File.Path, 0, "profile %s has an activation that is not evaluated", manifests.CleanText(id, 60)))
			}
		}
	}
	rec.flush(f)
	u.Diagnostics = rec.diags
	return u
}

func path0(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func anyIncomplete(e *eff) bool {
	for _, x := range e.chain {
		if x.incomplete || x.unresolvedParent {
			return true
		}
	}
	return false
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

// kindOf is the artifact kind of the POM's first provided artifact (set by the parser from the packaging).
func kindOf(n *pomNode) string {
	if len(n.p.Provides) > 0 && n.p.Provides[0].Kind != "" {
		return n.p.Provides[0].Kind
	}
	return "library"
}

// kotlinVersion is the Kotlin version of the POM: the kotlin.version property, else the version of an org.jetbrains.kotlin dependency, else "unknown".
func (e *eff) kotlinVersion(deps []manifests.Dependency) string {
	if v, ok := e.lookup("kotlin.version"); ok && v != "" && !strings.Contains(v, "${") {
		return manifests.CleanText(v, 40)
	}
	for _, d := range deps {
		if d.Target.Namespace == "org.jetbrains.kotlin" && d.Version != "" {
			return manifests.CleanText(d.Version, 40)
		}
	}
	return "unknown"
}
