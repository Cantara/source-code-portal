package gradle

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/expand"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

type assembler struct{}

func NewAssembler() manifests.Assembler { return assembler{} }

func (assembler) Ecosystem() manifests.EcosystemID { return ID }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
func (assembler) AssemblerVersion() int { return assemblerVer }

const assemblerVer = 1

type catLib struct {
	alias, g, a, req, ref string
	file                  manifests.FileRef
	line                  int
	plugin                bool
}

type catalog struct {
	name     string
	file     *manifests.FileRef
	versions map[string]string
	libs     map[string]catLib // by normalised alias
	plugins  map[string]catLib
	bundles  map[string][]string // normalised bundle name -> normalised aliases
	used     map[string]bool
}

type build struct {
	dir      string
	settings *manifests.Parsed
	projects map[string]string // ":a:b" -> dir
	order    []string
	explicit map[string]bool // projects named by an include (parents of nested includes are implicit)
	rootName string
	cats     map[string]*catalog
	exp      *expand.Expander
}

type group struct {
	script *manifests.Parsed
	props  *manifests.Parsed
	locks  []*manifests.Parsed
}

func normAlias(s string) string { return strings.NewReplacer("-", ".", "_", ".").Replace(s) }

func (assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	groups := map[string]*group{}
	get := func(d string) *group {
		if groups[d] == nil {
			groups[d] = &group{}
		}
		return groups[d]
	}
	settings := map[string]*manifests.Parsed{}
	catalogFiles := map[string]*manifests.Parsed{}
	for i := range parsed {
		p := &parsed[i]
		d := repopath.Dir(p.File.Path)
		switch p.File.ParserID {
		case SettingsID:
			settings[d] = p
		case ScriptID:
			get(d).script = p
		case PropertiesID:
			get(d).props = p
		case CatalogID:
			catalogFiles[p.File.Path] = p
		case LockfileID:
			ld := d
			if path.Base(d) == "dependency-locks" {
				ld = repopath.Dir(repopath.Dir(d))
			}
			get(ld).locks = append(get(ld).locks, p)
		}
	}
	shared := 64 << 20
	// build roots
	roots := make([]string, 0, len(settings))
	for d := range settings {
		roots = append(roots, d)
	}
	sort.Strings(roots)
	builds := map[string]*build{}
	owner := map[string]*build{} // project dir -> build
	for _, rd := range roots {
		b := newBuild(rd, settings[rd], catalogFiles, &shared)
		builds[rd] = b
		for _, p := range b.order {
			if _, taken := owner[b.projects[p]]; !taken {
				owner[b.projects[p]] = b
			}
		}
	}
	var units []manifests.Unit
	done := map[string]bool{}
	emit := func(b *build, dir, projPath string, isRoot bool) {
		if done[dir] {
			return
		}
		done[dir] = true
		units = append(units, buildUnit(b, dir, projPath, isRoot, groups, groups[b.dir], files))
	}
	for _, rd := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := builds[rd]
		for _, p := range b.order { // projects first: catalog usage is known when the root reports unused entries
			if p == ":" {
				continue
			}
			dir := b.projects[p]
			if g := groups[dir]; g == nil || g.script == nil {
				continue
			}
			emit(b, dir, p, false)
		}
		emit(b, rd, ":", true)
	}
	// standalone builds: script directories no settings file owns (buildSrc, build-logic, single-project builds)
	var rest []string
	for d, g := range groups {
		if g.script != nil && !done[d] {
			rest = append(rest, d)
		}
	}
	sort.Strings(rest)
	for _, d := range rest {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b := newBuild(d, nil, catalogFiles, &shared)
		emit(b, d, ":", true)
	}
	sort.SliceStable(units, func(i, j int) bool { return units[i].Dir < units[j].Dir })
	return units, nil
}

func newBuild(dir string, st *manifests.Parsed, catalogFiles map[string]*manifests.Parsed, shared *int) *build {
	b := &build{dir: dir, settings: st, projects: map[string]string{":": dir}, order: []string{":"}, cats: map[string]*catalog{}, exp: expand.New(shared)}
	add := func(p, d string) {
		if _, ok := b.projects[p]; !ok {
			b.projects[p] = d
			b.order = append(b.order, p)
		}
	}
	b.explicit = map[string]bool{}
	if st != nil {
		b.rootName = st.Properties["rootProject.name"]
		for _, m := range st.Members {
			if strings.HasPrefix(m, "..flat/") {
				if d, ok := repopath.Join(dir, "../"+strings.TrimPrefix(m, "..flat/")); ok {
					add(":"+path.Base(d), d)
				}
				continue
			}
			b.explicit[":"+strings.Trim(m, ":")] = true
			segs := strings.Split(strings.Trim(m, ":"), ":")
			cur, curDir := "", dir
			for _, s := range segs {
				if s == "" {
					continue
				}
				cur += ":" + s
				d, ok := repopath.Join(curDir, s)
				if !ok {
					break
				}
				curDir = d
				add(cur, d)
			}
		}
	}
	sort.Strings(b.order[1:])
	// catalogs: the default libs file, settings-declared files, inline catalogs
	if f, ok := catalogFiles[join(dir, "gradle/libs.versions.toml")]; ok {
		b.cats["libs"] = fromCatalogFile("libs", f)
	}
	if st != nil {
		for k, v := range st.Properties {
			if name, ok := strings.CutPrefix(k, "catalog-file:"); ok {
				if p, ok := repopath.Join(dir, v); ok {
					if f, ok := catalogFiles[p]; ok {
						b.cats[name] = fromCatalogFile(name, f)
					}
				}
			}
		}
		for _, d := range st.Dependencies {
			if !strings.HasPrefix(d.Group, "catalog:") || !strings.HasSuffix(d.Group, ":lib") {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(d.Group, "catalog:"), ":lib")
			c := b.cats[name]
			if c == nil {
				c = newCatalog(name)
				b.cats[name] = c
			}
			c.libs[normAlias(d.TargetDisplay())] = catLib{alias: d.TargetDisplay(), g: d.Target.Namespace, a: d.Target.Name, req: d.Requirement, ref: strings.TrimPrefix(d.SourceRef(), "ref:"), file: st.File, line: int(d.Line)}
		}
		for k, v := range st.Properties {
			if rest, ok := strings.CutPrefix(k, "cat:"); ok {
				if name, vn, ok := strings.Cut(rest, ":version:"); ok {
					c := b.cats[name]
					if c == nil {
						c = newCatalog(name)
						b.cats[name] = c
					}
					c.versions[normAlias(vn)] = v
				}
			}
		}
	}
	return b
}

func join(dir, rel string) string {
	if dir == "" {
		return rel
	}
	return dir + "/" + rel
}

func newCatalog(name string) *catalog {
	return &catalog{name: name, versions: map[string]string{}, libs: map[string]catLib{}, plugins: map[string]catLib{}, bundles: map[string][]string{}, used: map[string]bool{}}
}

func fromCatalogFile(name string, f *manifests.Parsed) *catalog {
	c := newCatalog(name)
	ref := f.File
	c.file = &ref
	for k, v := range f.Properties {
		switch {
		case strings.HasPrefix(k, "version:"):
			c.versions[normAlias(strings.TrimPrefix(k, "version:"))] = v
		case strings.HasPrefix(k, "bundle:"):
			var ms []string
			for _, m := range strings.Split(v, "\t") {
				if m != "" {
					ms = append(ms, normAlias(m))
				}
			}
			c.bundles[normAlias(strings.TrimPrefix(k, "bundle:"))] = ms
		}
	}
	for _, d := range f.Dependencies {
		l := catLib{alias: d.TargetDisplay(), g: d.Target.Namespace, a: d.Target.Name, req: d.Requirement, ref: strings.TrimPrefix(d.SourceRef(), "ref:"), file: f.File, line: int(d.Line)}
		switch d.Group {
		case "catalog-lib":
			c.libs[normAlias(d.TargetDisplay())] = l
		case "catalog-plugin":
			l.plugin = true
			c.plugins[normAlias(d.TargetDisplay())] = l
		}
	}
	return c
}

// ---- unit assembly ----

var bareVar = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// prepTemplate turns "$name" into "${name}" and protects the "$$" escape produced for single-quoted strings.
func prepTemplate(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	s = strings.ReplaceAll(s, "$$", "\x01")
	s = bareVar.ReplaceAllString(s, "$${$1}")
	return s
}

func restoreDollar(s string) string { return strings.ReplaceAll(s, "\x01", "$") }

type uctx struct {
	b        *build
	dir      string
	g        *group
	rg       *group
	rec      recorder
	memo     map[string]string
	version  string
	group    string
	plugins  map[string]string
	unitCats map[string]bool
}

type recorder struct {
	diags []manifests.Diagnostic
	seen  map[string]bool
	n     map[string]int
}

const maxPerCode = 6

func (r *recorder) add(d manifests.Diagnostic) {
	if r.seen == nil {
		r.seen, r.n = map[string]bool{}, map[string]int{}
	}
	k := d.Code + "|" + d.Loc.Path + "|" + d.Message
	if r.seen[k] {
		return
	}
	r.seen[k] = true
	r.n[d.Code]++
	if r.n[d.Code] > maxPerCode {
		return
	}
	r.diags = append(r.diags, d)
}

func (r *recorder) flush(file string) {
	codes := make([]string, 0)
	for c, n := range r.n {
		if n > maxPerCode {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	for _, c := range codes {
		r.diags = append(r.diags, diagAt(c, file, 0, "%d more %s diagnostics suppressed", r.n[c]-maxPerCode, c))
	}
}

func prop(g *group, key string) (string, bool) {
	if g == nil || g.props == nil {
		return "", false
	}
	v, ok := g.props.Properties["prop:"+key]
	return v, ok
}

var propCall = regexp.MustCompile(`^(?:project\.|rootProject\.)?(?:property|findProperty|getProperty)\(\s*["']([^"']+)["']\s*\)(?:\.toString\(\))?$`)
var propIndex = regexp.MustCompile(`^(?:project\.|rootProject\.)?(?:extra|ext|properties|project\.extra)\[\s*["']([^"']+)["']\s*\](?:\.toString\(\))?$`)

func (u *uctx) lookup(name string) (string, bool) {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".get()"), ".toString()")
	if m := propCall.FindStringSubmatch(name); m != nil {
		name = m[1]
	} else if m := propIndex.FindStringSubmatch(name); m != nil {
		name = m[1]
	}
	for _, pfx := range []string{"rootProject.ext.", "project.ext.", "ext.", "rootProject.", "project."} {
		if strings.HasPrefix(name, pfx) {
			name = strings.TrimPrefix(name, pfx)
			break
		}
	}
	if strings.HasPrefix(name, "extra[") {
		name = strings.TrimSuffix(strings.TrimPrefix(name, "extra["), "]")
	}
	// catalog version accessors: <catalog>.versions.<name>
	if i := strings.Index(name, ".versions."); i > 0 {
		if c := u.b.cats[name[:i]]; c != nil {
			vn := strings.TrimSuffix(strings.TrimSuffix(name[i+len(".versions."):], ".asProvider"), ".get")
			if v, ok := c.versions[normAlias(vn)]; ok {
				return v, true
			}
		}
		return "", false
	}
	switch name {
	case "version":
		if u.version != "" {
			return u.version, true
		}
	case "group":
		if u.group != "" {
			return u.group, true
		}
	case "kotlinPluginVersion":
		ids := make([]string, 0, len(u.plugins))
		for id := range u.plugins {
			ids = append(ids, id)
		}
		sort.Strings(ids) // map order must not decide the answer
		for _, id := range ids {
			if v := u.plugins[id]; strings.HasPrefix(id, "org.jetbrains.kotlin") && v != "" {
				return v, true
			}
		}
		return "", false
	}
	for _, g := range []*group{u.g, u.rg} {
		if g != nil && g.script != nil {
			if v, ok := g.script.Properties["var:"+name]; ok {
				return prepTemplate(v), true
			}
		}
	}
	if v, ok := prop(u.g, name); ok {
		return v, true
	}
	if v, ok := prop(u.rg, name); ok {
		return v, true
	}
	return "", false
}

func (u *uctx) interp(s string, file string, line int) string {
	if !strings.Contains(s, "$") {
		return s
	}
	s = prepTemplate(s)
	out, unres, cyc, err := u.b.exp.Expand(s, u.lookup, u.memo, nil)
	unres = append(unres, cyc...)
	if err != nil {
		u.rec.add(diagAt("limit.expand", file, line, "template expansion budget exceeded"))
		return restoreDollar(s)
	}
	for _, n := range unres {
		u.rec.add(diagAt("gradle.template-unresolved", file, line, "${%s}", clean(n, 80)))
	}
	return restoreDollar(out)
}

func isDynamic(v string) bool {
	return strings.ContainsAny(v, "+[](),") || strings.HasPrefix(v, "latest.") || strings.Contains(v, "${") || v == ""
}

// exactVersion returns the pinned version of a Gradle version string: a plain version, "1.0!!" (strictly) or "[1.0]".
func exactVersion(v string) (string, bool) {
	v = strings.TrimSuffix(v, "!!")
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") && !strings.Contains(v, ",") {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	if v == "" || isDynamic(v) {
		return "", false
	}
	return v, true
}

func (u *uctx) resolveVersion(c *catalog, l catLib) string {
	if l.req != "" {
		return l.req
	}
	if l.ref != "" {
		if v, ok := c.versions[normAlias(l.ref)]; ok {
			return v
		}
		u.rec.add(diagAt("gradle.catalog-version-missing", l.file.Path, l.line, "version.ref %q is not defined", clean(l.ref, 60)))
	}
	return ""
}

func buildUnit(b *build, dir, projPath string, isRoot bool, groups map[string]*group, rg *group, _ manifests.FileSet) manifests.Unit {
	g := groups[dir]
	if g == nil {
		g = &group{}
	}
	u := &uctx{b: b, dir: dir, g: g, rg: rg, memo: map[string]string{}, plugins: map[string]string{}, unitCats: map[string]bool{}}
	if rg == nil {
		u.rg = g
	}
	unit := manifests.Unit{Ecosystem: ID, Dir: dirOrDot(dir), Toolchain: map[string]string{"tool": "gradle"}}
	script := g.script
	scriptPath := ""
	if script != nil {
		scriptPath = script.File.Path
		unit.Files = append(unit.Files, script.File)
		unit.Diagnostics = append(unit.Diagnostics, script.Diagnostics...)
		for k, v := range script.Properties {
			if id, ok := strings.CutPrefix(k, "plugin:"); ok {
				u.plugins[id] = v
			}
		}
		unit.Toolchain["dsl"] = "groovy" // the file name is known here, not to the (path independent) parser
		if strings.HasSuffix(script.File.Path, ".kts") {
			unit.Toolchain["dsl"] = "kotlin"
		}
	}
	if isRoot && b.settings != nil {
		unit.Files = append(unit.Files, b.settings.File)
		unit.Diagnostics = append(unit.Diagnostics, b.settings.Diagnostics...)
		for k, v := range b.settings.Properties {
			if id, ok := strings.CutPrefix(k, "plugin:"); ok {
				u.plugins[id] = v
			}
		}
	}
	if g.props != nil {
		unit.Files = append(unit.Files, g.props.File)
	}
	if rg != nil && rg != g && rg.props != nil {
		unit.Files = append(unit.Files, rg.props.File)
	}
	// identity
	if script != nil {
		u.group, u.version = script.Properties["group"], script.Properties["version"]
	}
	if u.group == "" && u.rg != nil && u.rg.script != nil {
		u.group = u.rg.script.Properties["all:group"]
		if !isRoot && u.group == "" {
			u.group = u.rg.script.Properties["sub:group"]
		}
	}
	if u.version == "" && u.rg != nil && u.rg.script != nil {
		u.version = u.rg.script.Properties["all:version"]
		if !isRoot && u.version == "" {
			u.version = u.rg.script.Properties["sub:version"]
		}
	}
	if u.group == "" {
		u.group, _ = prop(g, "group")
		if u.group == "" && g != u.rg {
			u.group, _ = prop(u.rg, "group")
		}
	}
	if u.version == "" {
		u.version, _ = prop(g, "version")
		if u.version == "" && g != u.rg {
			u.version, _ = prop(u.rg, "version")
		}
	}
	u.group = u.interp(u.group, scriptPath, 0)
	u.version = u.interp(u.version, scriptPath, 0)
	name := b.rootName
	if !isRoot {
		name = path.Base(dir)
		if i := strings.LastIndexByte(projPath, ':'); i >= 0 {
			name = projPath[i+1:]
		}
	} else if name == "" {
		name = path.Base(dir)
		if dir == "" {
			name = "root"
		}
	}
	if u.group != "" {
		unit.Provides = append(unit.Provides, manifests.Artifact{ID: mvn(clean(u.group, 200), clean(name, 200)), Display: clean(u.group+":"+name, 400), Version: clean(u.version, 128), Kind: u.artifactKind()})
	}
	// raw declarations in scope: own script, root script fan-out, settings plugins, root catalog leftovers
	var raw []manifests.Dependency
	if script != nil {
		for _, d := range script.Dependencies {
			if c := d.Condition(); c == "subprojects" || strings.HasPrefix(c, "project:") {
				continue // applies to other projects; fanned out from the build root
			}
			raw = append(raw, owned(d, script.File.Path))
		}
	}
	if rg != nil && rg != g && rg.script != nil {
		for _, d := range rg.script.Dependencies {
			switch c := d.Condition(); {
			case c == "allprojects" || (c == "subprojects" && !isRoot) || c == "project:"+projPath:
				d = owned(d, rg.script.File.Path)
				d.SetCondition("inherited:" + c)
				raw = append(raw, d)
			}
		}
	}
	if isRoot && b.settings != nil {
		for _, d := range b.settings.Dependencies {
			if d.Group == "plugin" || d.Group == "catalog-ref" || d.SourceRef() == "catalog" {
				raw = append(raw, owned(d, b.settings.File.Path))
			}
		}
	}
	// scripts that are the root of their own fan-out keep only their own plain declarations
	var deps []manifests.Dependency
	seen := map[string]bool{}
	push := func(d manifests.Dependency) {
		base := d.Group + "|" + d.Target.String() + "|" + d.Qualifiers() + "|" + d.SourceRef() + "|" + d.Requirement
		k := d.Condition() + "|" + base
		if seen[k] || (strings.HasPrefix(d.Condition(), "inherited:") && seen["|"+base]) {
			return
		}
		seen[k] = true
		deps = append(deps, d)
	}
	cons := map[string]string{} // constraint versions by GA
	for _, d := range raw {
		if strings.HasPrefix(d.Group, "constraint:") && d.Source == manifests.SourceRegistry {
			cons[d.Target.Namespace+":"+d.Target.Name] = u.interp(d.Requirement, d.Loc().Path, int(d.Line))
		}
	}
	for _, d := range raw {
		for _, r := range u.resolve(d) {
			if strings.HasPrefix(r.Group, "constraint:") {
				continue
			}
			if r.Requirement == "" && r.Source == manifests.SourceRegistry {
				if v, ok := cons[r.Target.Namespace+":"+r.Target.Name]; ok && v != "" {
					r.Requirement, r.Resolution = v, manifests.ResManaged
				}
			}
			if r.Source == manifests.SourceRegistry {
				if v, ok := exactVersion(r.Requirement); ok {
					r.Version = manifests.CleanText(v, 128)
					if r.Resolution == manifests.ResDeclared {
						r.Resolution = manifests.ResPinned
					}
				}
			}
			push(r)
		}
	}
	// project dependencies: map ":x" to a directory and, when known, to its published coordinates. A project that cannot be placed is demoted
	// to a SourcePath without a reference: a unit never leaves SourceProject behind (manifests.CheckTree).
	for i := range deps {
		d := &deps[i]
		if d.Source != manifests.SourceProject {
			continue
		}
		file, line := d.Loc().Path, int(d.Line)
		ref := d.SourceRef()
		pp, pd, ok := "", "", false
		switch {
		case strings.HasPrefix(ref, "projects."): // typesafe accessor projects.core.dataTest
			for _, cand := range b.order {
				if accessorOf(cand) == strings.TrimPrefix(ref, "projects.") {
					pp, pd, ok = cand, b.projects[cand], true
					break
				}
			}
			if !ok {
				u.rec.add(diagAt("gradle.project-missing", file, line, "accessor %s matches no included project", clean(ref, 80)))
			}
		default:
			pp = ref
			if !strings.HasPrefix(pp, ":") {
				pp = ":" + pp
			}
			if pd, ok = b.projects[pp]; !ok && b.settings == nil {
				// a build without settings: the default layout maps ":a:b" to a/b below the build directory
				pd, ok = repopath.Join(b.dir, strings.ReplaceAll(strings.TrimPrefix(pp, ":"), ":", "/"))
			}
			if !ok {
				u.rec.add(diagAt("gradle.project-missing", file, line, "project %s is not included by settings", clean(ref, 80)))
			}
		}
		if !ok {
			d.Source = manifests.SourcePath
			setRef(d, "")
			continue
		}
		d.Source = manifests.SourcePath
		setRef(d, dirOrDot(pd))
		if pg := groups[pd]; pg != nil && pg.script != nil {
			if gg := pg.script.Properties["group"]; gg != "" {
				d.Target = mvn(gg, path.Base(strings.ReplaceAll(pp, ":", "/")))
			}
		}
	}
	// unreferenced catalog entries belong to the build root
	if isRoot {
		names := make([]string, 0, len(b.cats))
		for n := range b.cats {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			c := b.cats[n]
			if c.file == nil {
				continue
			}
			unit.Files = append(unit.Files, *c.file)
			keys := make([]string, 0, len(c.libs))
			for k := range c.libs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if c.used["lib:"+k] {
					continue
				}
				l := c.libs[k]
				req := u.resolveVersion(c, l)
				d := mkd(mvn(l.g, l.a), clean(req, 200), clean(l.g+":"+l.a, 400), "", "catalog-unused", "", manifests.ScopeUnknown, l.line)
				d.Confidence = manifests.ConfExact // a catalog entry is a declaration, not a lexical guess
				d.SetLocPath(l.file.Path)
				if v, ok := exactVersion(req); ok {
					d.Version, d.Resolution = clean(v, 128), manifests.ResPinned
				}
				push(d)
			}
			pk := make([]string, 0, len(c.plugins))
			for k := range c.plugins {
				pk = append(pk, k)
			}
			sort.Strings(pk)
			for _, k := range pk {
				if c.used["plugin:"+k] {
					continue
				}
				l := c.plugins[k]
				req := u.resolveVersion(c, l)
				d := mkd(mvn(l.g, l.a), clean(req, 200), clean(l.g, 200), "", "catalog-unused", "", manifests.ScopeBuild, l.line)
				d.Confidence = manifests.ConfExact
				d.SetLocPath(l.file.Path)
				if v, ok := exactVersion(req); ok {
					d.Version, d.Resolution = clean(v, 128), manifests.ResPinned
				}
				push(d)
			}
		}
	}
	for i := range deps { // the unit's own build script is the default location
		if x := deps[i].Extra; x != nil && x.LocPath == scriptPath {
			x.LocPath = ""
		}
	}
	unit.Dependencies = deps
	// lockfiles of this project
	unit.Locked = joinLocks(&unit, g, b, isRoot)
	// members
	if isRoot {
		for _, p := range b.order[1:] {
			unit.Members = append(unit.Members, b.projects[p])
		}
		sort.Strings(unit.Members)
		for _, p := range b.order[1:] {
			if pg := groups[b.projects[p]]; (pg == nil || pg.script == nil) && b.explicit[p] {
				u.rec.add(diagAt("gradle.project-missing", settingsOrScript(script, b), 0, "project %s has no fetched build script", clean(p, 80)))
			}
		}
	}
	// toolchain
	tc := unit.Toolchain
	tc["language"] = "java"
	for id := range u.plugins {
		switch {
		case strings.HasPrefix(id, "org.jetbrains.kotlin") || id == "kotlin" || strings.HasPrefix(id, "kotlin-"):
			tc["language"] = "kotlin"
		case strings.HasPrefix(id, "com.android."):
			tc["android"] = "true"
		}
	}
	for _, d := range deps {
		if d.Target.Namespace == "org.jetbrains.kotlin" && d.Scope != manifests.ScopeBuild && d.Group != "plugin" {
			tc["language"] = "kotlin"
		}
	}
	if tc["language"] == "kotlin" {
		tc["kotlin"] = kotlinVersion(u.plugins, deps) // the core adds the language Kotlin from this key (Gradle units are not ecosystem maven)
	}
	jv := ""
	if script != nil {
		jv = script.Properties["java"]
	}
	if jv == "" && u.rg != nil && u.rg.script != nil && u.rg != g {
		jv = u.rg.script.Properties["java"]
	}
	if jv != "" {
		tc["java"] = jv
	}
	u.rec.flush(settingsOrScript(script, b))
	unit.Diagnostics = dedupeDiags(append(unit.Diagnostics, u.rec.diags...))
	return unit
}

// accessorOf maps a project path to its typesafe accessor: ":core:data-test" becomes "core.dataTest".
func accessorOf(p string) string {
	var segs []string
	for _, s := range strings.Split(strings.Trim(p, ":"), ":") {
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
		for i := 1; i < len(parts); i++ {
			if parts[i] != "" {
				parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
			}
		}
		segs = append(segs, strings.Join(parts, ""))
	}
	return strings.Join(segs, ".")
}

func scriptOrSettings(script *manifests.Parsed, b *build) string {
	if script != nil {
		return script.File.Path
	}
	if b.settings != nil {
		return b.settings.File.Path
	}
	return ""
}

func settingsOrScript(script *manifests.Parsed, b *build) string {
	if b.settings != nil {
		return b.settings.File.Path
	}
	return scriptOrSettings(script, b)
}

func dedupeDiags(ds []manifests.Diagnostic) []manifests.Diagnostic {
	seen := map[string]bool{}
	var out []manifests.Diagnostic
	for _, d := range ds {
		k := d.Code + "|" + d.Loc.Path + "|" + d.Message
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}

// owned returns a private copy of a parsed dependency stamped with the file it was read from: assemblers never write to a Parsed (requirement RR-A5),
// and a Gradle unit mixes declarations of its own script, the root script, settings and catalogs.
func owned(d manifests.Dependency, file string) manifests.Dependency {
	d = d.Clone()
	d.SetLocPath(file)
	return d
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

// setRef replaces the SourceRef of a dependency this assembler owns.
func setRef(d *manifests.Dependency, ref string) {
	if d.Extra == nil {
		if ref == "" {
			return
		}
		d.SetSource(d.Source, ref)
		return
	}
	d.Extra.SourceRef = ref
}

// kotlinVersion is the Kotlin version a unit declares: the version of a Kotlin plugin, else of any org.jetbrains.kotlin dependency, else "unknown".
func kotlinVersion(plugins map[string]string, deps []manifests.Dependency) string {
	ids := make([]string, 0, len(plugins))
	for id := range plugins {
		if strings.HasPrefix(id, "org.jetbrains.kotlin") || id == "kotlin" || strings.HasPrefix(id, "kotlin-") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if v := plugins[id]; v != "" && !strings.Contains(v, "${") {
			return clean(v, 40)
		}
	}
	for _, d := range deps {
		if strings.HasPrefix(d.Target.Namespace, "org.jetbrains.kotlin") && d.Version != "" {
			return clean(d.Version, 40)
		}
	}
	return "unknown"
}

// artifactKind is "application" for a project that applies an application plugin, else "library" (the closed enum of the API; never a build-tool specific value).
func (u *uctx) artifactKind() string {
	for _, id := range []string{"application", "com.android.application", "org.springframework.boot", "io.ktor.plugin", "org.jetbrains.compose"} {
		if _, ok := u.plugins[id]; ok {
			return "application"
		}
	}
	return "library"
}
