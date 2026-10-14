package npm

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
)

type assembler struct{}

func NewAssembler() manifests.Assembler { return assembler{} }

func (assembler) Ecosystem() manifests.EcosystemID { return manifests.NPM }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
func (assembler) AssemblerVersion() int { return assemblerVer }

const assemblerVer = 1

const maxWorkspacePatterns = 128

type wsRoot struct {
	dir     string
	members []string
	diags   []manifests.Diagnostic
}

type dirGroup struct {
	dir   string
	pkg   *manifests.Parsed
	locks map[string]*manifests.Parsed // by parser id; npm-shrinkwrap and package-lock share PackageLockID
	ws    *manifests.Parsed            // pnpm-workspace.yaml
}

func (g *dirGroup) isProject() bool {
	p := g.pkg
	if p == nil {
		return false
	}
	return len(p.Provides) > 0 || len(p.Dependencies) > 0 || len(p.Members) > 0 || g.ws != nil
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

func relDir(from, to string) string { // directory of to relative to from ("." when equal); to is below or equal from
	if from == to {
		return "."
	}
	if from == "" {
		return to
	}
	return strings.TrimPrefix(to, from+"/")
}

func (assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	groups := map[string]*dirGroup{}
	get := func(d string) *dirGroup {
		if groups[d] == nil {
			groups[d] = &dirGroup{dir: d, locks: map[string]*manifests.Parsed{}}
		}
		return groups[d]
	}
	for i := range parsed {
		p := &parsed[i]
		d := repopath.Dir(p.File.Path)
		switch p.File.ParserID {
		case PackageJSONID:
			get(d).pkg = p
		case PnpmWorkspaceID:
			get(d).ws = p
		case PackageLockID:
			key := PackageLockID
			if path.Base(p.File.Path) == "npm-shrinkwrap.json" {
				key = "npm-shrinkwrap"
			}
			get(d).locks[key] = p
		case PnpmLockID, YarnLockID, BunLockID:
			get(d).locks[p.File.ParserID] = p
		}
	}
	var dirs []string
	for d, g := range groups {
		if g.isProject() {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	isProject := map[string]bool{}
	for _, d := range dirs {
		isProject[d] = true
	}

	// workspace roots and membership (outermost root wins)
	roots := map[string]*wsRoot{}
	memberOf := map[string]string{}
	for _, d := range dirs {
		g := groups[d]
		var pats []string
		pats = append(pats, g.pkg.Members...)
		if g.ws != nil {
			pats = append(pats, g.ws.Members...)
		}
		if len(pats) == 0 {
			continue
		}
		wr := &wsRoot{dir: d}
		if len(pats) > maxWorkspacePatterns {
			pats = pats[:maxWorkspacePatterns]
			wr.diags = append(wr.diags, diagAt("npm.workspace-pattern-limit", g.pkg.File.Path, 0, "more than %d workspace patterns; the rest are ignored", maxWorkspacePatterns))
		}
		var pos, neg []string
		for _, p := range pats {
			p = strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/package.json")
			p = strings.TrimSuffix(p, "/")
			if strings.HasPrefix(p, "!") {
				neg = append(neg, p[1:])
			} else if p != "" {
				pos = append(pos, p)
			}
		}
		matched := map[string]bool{}
		for _, p := range pos {
			hit := false
			for _, cd := range dirs {
				if cd == d || !(d == "" || strings.HasPrefix(cd, d+"/")) {
					continue
				}
				rel := relDir(d, cd)
				if repopath.Match(p, rel) {
					hit = true
					matched[cd] = true
				}
			}
			if !hit && !strings.ContainsAny(p, "*?[") { // explicit directory that is not a fetched project
				wr.diags = append(wr.diags, diagAt("npm.workspace-member-unfetched", g.pkg.File.Path, 0, "workspace %q has no fetched package.json", manifests.CleanText(p, 120)))
			} else if !hit {
				wr.diags = append(wr.diags, diagAt("npm.workspace-glob-unmatched", g.pkg.File.Path, 0, "pattern %q matched no package.json", manifests.CleanText(p, 120)))
			}
		}
		for _, n := range neg {
			for cd := range matched {
				if repopath.Match(n, relDir(d, cd)) {
					delete(matched, cd)
				}
			}
		}
		for cd := range matched {
			wr.members = append(wr.members, cd)
		}
		sort.Strings(wr.members)
		roots[d] = wr
	}
	for _, d := range dirs { // outermost first (dirs sorted: parents sort before children)
		if wr, ok := roots[d]; ok {
			if _, nested := memberOf[d]; nested {
				continue
			}
			for _, m := range wr.members {
				if _, has := memberOf[m]; !has {
					memberOf[m] = d
				}
			}
		}
	}
	rootOf := func(d string) string {
		if r, ok := memberOf[d]; ok {
			return r
		}
		return d
	}

	// member artifacts by name for workspace: resolution
	byName := map[manifests.ComponentID]string{}
	for _, d := range dirs {
		if p := groups[d].pkg; len(p.Provides) > 0 {
			if _, has := byName[p.Provides[0].ID]; !has {
				byName[p.Provides[0].ID] = d
			}
		}
	}

	units := make([]manifests.Unit, 0, len(dirs))
	rootOfUnit := make([]string, 0, len(dirs))
	unitKey := make([]string, 0, len(dirs))
	chosen := map[string]*manifests.Parsed{}
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		g := groups[d]
		rd := rootOf(d)
		rg := groups[rd]
		u := manifests.Unit{Ecosystem: manifests.NPM, Dir: dirOrDot(d), Toolchain: map[string]string{}}
		for k, v := range g.pkg.Toolchain {
			u.Toolchain[k] = v
		}
		u.Files = append(u.Files, g.pkg.File)
		u.Provides = append(u.Provides, g.pkg.Provides...)
		u.Licenses = append(u.Licenses, g.pkg.Licenses...)
		u.Diagnostics = append(u.Diagnostics, g.pkg.Diagnostics...)
		if g.ws != nil {
			u.Files = append(u.Files, g.ws.File)
			u.Diagnostics = append(u.Diagnostics, g.ws.Diagnostics...)
		}
		if wr, ok := roots[d]; ok {
			u.Members = append(u.Members, wr.members...)
			u.Diagnostics = append(u.Diagnostics, wr.diags...)
		}
		// toolchain inheritance from the workspace root (packageManager is declared once)
		if rd != d {
			for _, k := range []string{"tool", "toolVersion"} {
				if _, has := u.Toolchain[k]; !has {
					if v := rg.pkg.Toolchain[k]; v != "" {
						u.Toolchain[k] = v
					}
				}
			}
		}
		cat := catalogs(rg, g)
		seen := map[[3]string]bool{}
		for _, dep := range g.pkg.Dependencies {
			dep = prepareDep(dep, g, cat, byName, &u)
			k := [3]string{dep.Target.String(), dep.Scope.String(), dep.Requirement}
			if seen[k] {
				continue
			}
			seen[k] = true
			u.Dependencies = append(u.Dependencies, dep)
		}
		// lockfile: chosen in the root directory of the workspace, attached to the root unit only
		sibling := func(dep manifests.Dependency) (string, bool) {
			md, ok := byName[dep.Target]
			if !ok || md == d || rootOf(md) != rd {
				return "", false
			}
			mv := ""
			if pv := groups[md].pkg.Provides; len(pv) > 0 {
				mv = pv[0].Version
			}
			sat, known := version.Satisfies("npm", mv, dep.Requirement)
			if dep.Requirement == "" || dep.Requirement == "*" || dep.Requirement == "latest" || (known && sat) {
				if md == "" {
					md = "."
				}
				return md, true
			}
			return "", false
		}
		lk := chooseLock(rg, &u, d == rd)
		rootOfUnit = append(rootOfUnit, rd)
		unitKey = append(unitKey, d)
		if lk != nil {
			chosen[rd] = lk
			if d == rd {
				u.Files = append(u.Files, lk.File)
				u.Locked = append(u.Locked, lk.Locked...)
				u.Diagnostics = append(u.Diagnostics, lk.Diagnostics...)
				for _, m := range lk.Members { // importer without a fetched package.json
					full, ok := repopath.Join(rd, m)
					if ok && !isProject[full] && !hasDiag(u.Diagnostics, "npm.workspace-member-unfetched", m) {
						u.Diagnostics = append(u.Diagnostics, diagAt("npm.workspace-member-unfetched", lk.File.Path, 0, "lockfile importer %q has no fetched package.json", manifests.CleanText(m, 120)))
					}
				}
			}
			joinLock(&u, lk, relDir(rd, d), g, sibling)
		}
		if lk == nil {
			for i := range u.Dependencies {
				if dep := &u.Dependencies[i]; dep.Source == manifests.SourceRegistry {
					if md, ok := sibling(*dep); ok {
						dep.SetSource(manifests.SourcePath, md)
					}
				}
			}
		}
		if d == rd && files.Has(path.Join(d, "bun.lockb")) {
			// the runner rejects the NUL bytes of the binary lockfile before any parser runs, so the explanation comes from the path set
			u.Diagnostics = append(u.Diagnostics, diagAt("npm.bun-binary-lockfile", path.Join(d, "bun.lockb"), 0, "bun.lockb is a binary lockfile and is not parsed; run bun install --save-text-lockfile to produce bun.lock"))
		}
		if len(u.Toolchain) == 0 {
			u.Toolchain = nil
		}
		units = append(units, u)
	}
	// workspace root units collect the lock for yarn/v1 classification after all members have joined their deps
	finalizeRoots(units, rootOfUnit, unitKey, chosen)
	if len(units) == 0 {
		return nil, nil // no package.json project (for example a lockfile alone): the root's default assembly shows the files
	}
	return units, nil
}

func hasDiag(ds []manifests.Diagnostic, code, frag string) bool {
	for _, d := range ds {
		if d.Code == code && strings.Contains(d.Message, frag) {
			return true
		}
	}
	return false
}

// catalogs merges catalog tables: workspace file beats package.json beats lock (fallback).
func catalogs(rg, g *dirGroup) map[string]string {
	out := map[string]string{}
	add := func(p *manifests.Parsed, overwrite bool) {
		if p == nil {
			return
		}
		for k, v := range p.Properties {
			if strings.HasPrefix(k, "catalog:") {
				if _, has := out[k]; !has || overwrite {
					out[k] = v
				}
			}
		}
	}
	for _, p := range []*manifests.Parsed{rg.ws, rg.pkg, lockOf(rg, PnpmLockID)} { // order = precedence (first set wins)
		add(p, false)
	}
	if g != rg {
		add(g.ws, true)
		add(g.pkg, true)
	}
	return out
}

func lockOf(g *dirGroup, id string) *manifests.Parsed { return g.locks[id] }

func prepareDep(dep manifests.Dependency, g *dirGroup, cat map[string]string, byName map[manifests.ComponentID]string, u *manifests.Unit) manifests.Dependency {
	dep = dep.Clone() // Parsed values are shared and cached
	req := dep.Requirement
	switch {
	case strings.HasPrefix(req, "catalog:"):
		name := strings.TrimPrefix(req, "catalog:")
		if name == "" {
			name = "default"
		}
		key := "catalog:" + name + ":" + displayOf(dep.Target)
		if v, ok := cat[key]; ok {
			sp := classify(displayOf(dep.Target), v)
			dep.Requirement, dep.Version, dep.Resolution = sp.Req, sp.Version, manifests.ResManaged
			if sp.Version != "" {
				dep.Resolution = manifests.ResManaged
			}
		} else {
			hasAny := false
			for k := range cat {
				hasAny = true
				_ = k
				break
			}
			code := "npm.catalog-missing"
			if !hasAny {
				code = "npm.catalog-unavailable"
			}
			u.Diagnostics = append(u.Diagnostics, diagAt(code, g.pkg.File.Path, int(dep.Line), "no entry for %s in catalog %q", displayOf(dep.Target), manifests.CleanText(name, 60)))
		}
	case dep.Source == manifests.SourceProject && dep.SourceRef() == "workspace":
		if md, ok := byName[dep.Target]; ok {
			dep.SetSource(manifests.SourcePath, dirOrDot(md))
		} else {
			dep.SetSource(manifests.SourcePath, "")
			u.Diagnostics = append(u.Diagnostics, diagAt("npm.workspace-ref-unresolved", g.pkg.File.Path, int(dep.Line), "no workspace package named %s", displayOf(dep.Target)))
		}
	}
	// SourcePath references were rebased by the runner (rebasePaths) and need no further work here
	return dep
}

// chooseLock picks the lockfile of a workspace root; several lockfiles are resolved by packageManager, then by priority.
func chooseLock(rg *dirGroup, u *manifests.Unit, isRoot bool) *manifests.Parsed {
	if len(rg.locks) == 0 {
		return nil
	}
	tool := rg.pkg.Toolchain["tool"]
	prefer := map[string][]string{
		"pnpm": {PnpmLockID}, "yarn": {YarnLockID}, "bun": {BunLockID}, "npm": {"npm-shrinkwrap", PackageLockID},
	}[tool]
	order := append(append([]string{}, prefer...), "npm-shrinkwrap", PackageLockID, PnpmLockID, YarnLockID, BunLockID)
	var pick *manifests.Parsed
	for _, id := range order {
		if lk := rg.locks[id]; lk != nil {
			pick = lk
			break
		}
	}
	if len(rg.locks) > 1 && isRoot {
		u.Diagnostics = append(u.Diagnostics, diagAt("npm.multiple-lockfiles", pick.File.Path, 0, "%d lockfiles; using %s", len(rg.locks), path.Base(pick.File.Path)))
	}
	return pick
}

// joinLock resolves the direct dependencies of one unit against the lockfile.
func joinLock(u *manifests.Unit, lk *manifests.Parsed, rel string, g *dirGroup, sibling func(manifests.Dependency) (string, bool)) {
	lockByID := map[manifests.ComponentID][]int{}
	for i, l := range lk.Locked {
		lockByID[l.ID] = append(lockByID[l.ID], i)
	}
	alias := func(dep manifests.Dependency) string { // local (declared) name of an npm: alias
		if strings.HasPrefix(strings.ToLower(dep.Requirement), "npm:") {
			if n, ok := g.pkg.Properties["alias:"+dep.Requirement]; ok {
				return n
			}
		}
		return displayOf(dep.Target)
	}
	isYarn := lk.File.ParserID == YarnLockID
	berry := isYarn && lk.Properties["lockfileVersion"] != "1"
	for i := range u.Dependencies {
		dep := &u.Dependencies[i]
		if dep.Source != manifests.SourceRegistry {
			continue
		}
		local := alias(*dep)
		var ver string
		switch {
		case isYarn:
			rng := dep.Requirement
			if dep.Resolution == manifests.ResManaged && rng != "" {
				// catalog-resolved ranges use the catalog's range as the descriptor
			}
			var cands []string
			if berry {
				if strings.Contains(rng, ":") && !strings.HasPrefix(rng, "npm:") {
					cands = append(cands, local+"@"+rng)
				} else {
					cands = append(cands, local+"@npm:"+rng, local+"@"+rng)
				}
			} else {
				cands = append(cands, local+"@"+rng)
			}
			for _, c := range cands {
				if v, ok := lk.Properties["desc:"+c]; ok {
					ver = v
					break
				}
			}
			if k := strings.Index(ver, "@"); k > 0 && ver != "workspace" { // alias: real@version
				ver = ver[strings.LastIndexByte(ver, '@')+1:]
			}
			if ver == "workspace" {
				ver = ""
			}
		default:
			ver = lk.Properties["lock:"+rel+":"+local]
		}
		if ver == "" && lk.File.ParserID == PackageLockID && lk.Properties["lockfileVersion"] == "1" {
			ver = pickByConstraint(lk, lockByID[dep.Target], dep.Requirement)
		}
		if ver == "" {
			if md, ok := sibling(*dep); ok {
				dep.SetSource(manifests.SourcePath, md)
				continue
			}
			if !dep.Optional && dep.Scope != manifests.ScopePeer {
				u.Diagnostics = append(u.Diagnostics, diagAt("npm.lock-missing-dep", g.pkg.File.Path, int(dep.Line), "%s is not in %s", displayOf(dep.Target), path.Base(lk.File.Path)))
			}
			continue
		}
		dep.Version, dep.Resolution = ver, manifests.ResLocked
	}
}

func pickByConstraint(lk *manifests.Parsed, idxs []int, req string) string {
	best := ""
	for _, i := range idxs {
		v := lk.Locked[i].Version
		if ok, known := version.Satisfies("npm", v, req); known && ok {
			if best == "" || v > best {
				best = v
			}
		}
	}
	if best == "" && len(idxs) == 1 {
		return lk.Locked[idxs[0]].Version
	}
	return best
}

// finalizeRoots classifies the locked components of lockfiles that carry no root information (yarn, package-lock v1):
// every unit of the workspace contributes roots (its locked direct dependencies); a component takes the best scope over
// all paths from them. Locks that already carry scopes (package-lock flags) keep them and only gain relationships.
func finalizeRoots(units []manifests.Unit, rootOfUnit, unitKey []string, chosen map[string]*manifests.Parsed) {
	for ri := range units {
		if rootOfUnit[ri] != unitKey[ri] || len(units[ri].Locked) == 0 {
			continue
		}
		lk := chosen[unitKey[ri]]
		if lk == nil || (lk.File.ParserID != YarnLockID && !(lk.File.ParserID == PackageLockID && lk.Properties["lockfileVersion"] == "1")) {
			continue
		}
		root := &units[ri]
		keepScopes := lk.File.ParserID == PackageLockID
		idx := map[manifests.ComponentID][]int{}
		for i, l := range root.Locked {
			idx[l.ID] = append(idx[l.ID], i)
		}
		var work []int
		rank := map[int]manifests.Scope{}
		relax := func(i int, sc manifests.Scope) {
			if cur, ok := rank[i]; !ok || scopeRank(sc) < scopeRank(cur) {
				rank[i] = sc
				work = append(work, i)
			}
		}
		for ui := range units {
			if rootOfUnit[ui] != unitKey[ri] {
				continue
			}
			for _, d := range units[ui].Dependencies {
				if d.Resolution != manifests.ResLocked || d.Source != manifests.SourceRegistry {
					continue
				}
				for _, i := range idx[d.Target] {
					if root.Locked[i].Version == d.Version {
						root.Locked[i].Relationship = manifests.RelDirect
						relax(i, d.Scope)
					}
				}
			}
		}
		for len(work) > 0 {
			i := work[len(work)-1]
			work = work[:len(work)-1]
			for _, t := range root.Locked[i].DependsOn {
				sc := rank[i]
				if sc == manifests.ScopePeer {
					sc = manifests.ScopeRuntime
				}
				relax(t, sc)
			}
		}
		for i := range root.Locked {
			sc, reached := rank[i]
			if !reached {
				continue
			}
			if !keepScopes {
				root.Locked[i].Scope = sc
			}
			if root.Locked[i].Relationship == manifests.RelUnknown {
				root.Locked[i].Relationship = manifests.RelTransitive
			}
		}
	}
}
