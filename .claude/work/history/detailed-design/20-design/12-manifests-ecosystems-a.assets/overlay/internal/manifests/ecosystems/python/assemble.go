package python

import (
	"context"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
)

const maxIncludeDepth = 8

type assembler struct{}

func NewAssembler() manifests.Assembler { return assembler{} }

func (assembler) Ecosystem() manifests.EcosystemID { return manifests.PyPI }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
func (assembler) AssemblerVersion() int { return assemblerVer }

const assemblerVer = 1

// projectDir maps a requirements file to the directory of the project it belongs to.
func projectDir(p string) string {
	d := repopath.Dir(p)
	if b := path.Base(d); (b == "requirements" || b == "requirements.d") && d != "" {
		return repopath.Dir(d)
	}
	return d
}

type dirGroup struct {
	dir      string
	pyproj   *manifests.Parsed
	setupcfg *manifests.Parsed
	pipfile  *manifests.Parsed
	reqs     []manifests.Parsed // plain requirement files and pip-compile locks
	locks    map[string]manifests.Parsed
}

func (g *dirGroup) isProject() bool {
	if g.pyproj != nil {
		p := g.pyproj
		if len(p.Provides) > 0 || len(p.Dependencies) > 0 || p.Toolchain["tool"] != "" || len(p.Members) > 0 {
			return true
		}
	}
	return g.setupcfg != nil || g.pipfile != nil || len(g.reqs) > 0
}

func (assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	groups := map[string]*dirGroup{}
	get := func(d string) *dirGroup {
		if groups[d] == nil {
			groups[d] = &dirGroup{dir: d, locks: map[string]manifests.Parsed{}}
		}
		return groups[d]
	}
	byPath := map[string]manifests.Parsed{}
	for i := range parsed {
		p := parsed[i]
		byPath[p.File.Path] = p
		switch p.File.ParserID {
		case PyprojectID:
			get(repopath.Dir(p.File.Path)).pyproj = &parsed[i]
		case SetupCfgID:
			get(repopath.Dir(p.File.Path)).setupcfg = &parsed[i]
		case PipfileID:
			get(repopath.Dir(p.File.Path)).pipfile = &parsed[i]
		case RequirementsID:
			g := get(projectDir(p.File.Path))
			g.reqs = append(g.reqs, p)
		case PoetryLockID, UVLockID, PylockID, PipfileLockID:
			get(repopath.Dir(p.File.Path)).locks[p.File.ParserID] = p
		}
	}
	dirs := make([]string, 0, len(groups))
	for d, g := range groups {
		if g.isProject() {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	unitAt := map[string]int{}
	var units []manifests.Unit
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		g := groups[d]
		sort.Slice(g.reqs, func(i, j int) bool { return g.reqs[i].File.Path < g.reqs[j].File.Path })
		u := buildUnit(byPath, g)
		unitAt[d] = len(units)
		units = append(units, u)
	}
	// uv workspaces: membership, source resolution, and the uv.lock join
	for _, d := range dirs {
		g := groups[d]
		if g.pyproj == nil {
			continue
		}
		if lk, ok := g.locks[UVLockID]; ok {
			joinUV(units, unitAt, d, lk, g.pyproj)
		}
	}
	for _, d := range dirs { // per-directory locks of the other kinds
		joinOtherLocks(&units[unitAt[d]], groups[d])
	}
	resolveWorkspaceSources(units, unitAt)
	return units, nil
}

func buildUnit(byPath map[string]manifests.Parsed, g *dirGroup) manifests.Unit {
	u := manifests.Unit{Ecosystem: manifests.PyPI, Dir: dirOrDot(g.dir), Toolchain: map[string]string{"language": "python"}}
	seen := map[[3]string]bool{}
	add := func(deps []manifests.Dependency) {
		for _, d := range deps {
			k := [3]string{d.Target.Name, d.Scope.String(), d.Group}
			if seen[k] && d.Condition() == "" {
				continue
			}
			seen[k] = true
			u.Dependencies = append(u.Dependencies, d.Clone())
		}
	}
	mergeCommon := func(p *manifests.Parsed) {
		u.Files = append(u.Files, p.File)
		u.Diagnostics = append(u.Diagnostics, p.Diagnostics...)
		for k, v := range p.Toolchain {
			if _, has := u.Toolchain[k]; !has || k == "tool" && u.Toolchain[k] == "" {
				u.Toolchain[k] = v
			}
		}
		u.Licenses = append(u.Licenses, p.Licenses...)
		if len(u.Provides) == 0 {
			u.Provides = append(u.Provides, p.Provides...)
		}
	}
	if g.pyproj != nil {
		mergeCommon(g.pyproj)
		add(g.pyproj.Dependencies)
	}
	if g.setupcfg != nil {
		mergeCommon(g.setupcfg)
		add(g.setupcfg.Dependencies)
	}
	if g.pipfile != nil {
		mergeCommon(g.pipfile)
		u.Toolchain["tool"] = firstNonEmpty(u.Toolchain["tool"], "pipenv")
		add(g.pipfile.Dependencies)
	}
	var constraints []manifests.Parsed
	var expandIncludes func(r manifests.Parsed, depth int, visited map[string]bool)
	expandIncludes = func(r manifests.Parsed, depth int, visited map[string]bool) {
		keys := make([]string, 0, len(r.Properties))
		for k := range r.Properties {
			if strings.HasPrefix(k, "include:") || strings.HasPrefix(k, "constraint:") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			kind := r.Properties[k]
			rel := k[strings.IndexByte(k, ':')+1:]
			ip, ok := repopath.Join(repopath.Dir(r.File.Path), rel) // includes are relative to the including file
			if !ok {
				u.Diagnostics = append(u.Diagnostics, diagAt("python.include-outside-repo", r.File.Path, 0, "%s escapes the repository", manifests.CleanText(rel, 120)))
				continue
			}
			if visited[ip] || depth >= maxIncludeDepth {
				u.Diagnostics = append(u.Diagnostics, diagAt("python.include-cycle", r.File.Path, 0, "%s is included again or nested deeper than %d", manifests.CleanText(ip, 120), maxIncludeDepth))
				continue
			}
			inc, ok := byPath[ip]
			if !ok {
				u.Diagnostics = append(u.Diagnostics, diagAt("python.include-not-candidate", r.File.Path, 0, "%s is not among the parsed files (only requirements*.txt, requirements*.in, constraints*.txt, constraints*.in and requirements/*.txt are read)", manifests.CleanText(ip, 120)))
				continue
			}
			visited[ip] = true
			u.Files = appendFile(u.Files, inc.File)
			if kind == "c" {
				constraints = append(constraints, inc)
				continue
			}
			add(rescope(inc))
			expandIncludes(inc, depth+1, visited)
		}
	}
	for _, r := range g.reqs {
		u.Files = append(u.Files, r.File)
		u.Diagnostics = append(u.Diagnostics, r.Diagnostics...)
		if r.Kind == manifests.KindLockfile {
			continue
		}
		add(rescope(r))
		expandIncludes(r, 0, map[string]bool{r.File.Path: true})
	}
	for _, c := range constraints {
		applyConstraints(&u, c)
	}
	u.Toolchain["tool"] = firstNonEmpty(u.Toolchain["tool"], "pip")
	if g.pyproj == nil && g.pipfile == nil && g.setupcfg == nil {
		u.Toolchain["tool"] = "pip"
	}
	return u
}

// rescope applies the file-name based scope, group and confidence of a requirements file to its dependencies. This is the only place
// that reads the file NAME (requirements-dev.txt), because parser output must not depend on the path.
func rescope(r manifests.Parsed) []manifests.Dependency {
	scope, heur := scopeForFile(r.File.Path)
	conf := manifests.ConfExact
	if heur {
		conf = manifests.ConfHeuristic
	}
	out := make([]manifests.Dependency, len(r.Dependencies))
	for i, d := range r.Dependencies {
		d = d.Clone()
		d.Scope, d.Group, d.Confidence = scope, baseName(r.File.Path), conf
		if d.Optional && scope == manifests.ScopeRuntime {
			d.Scope = manifests.ScopeOptional
		}
		out[i] = d
	}
	return out
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// applyConstraints pins unpinned registry dependencies to the exact versions of a constraints file (ResManaged).
func applyConstraints(u *manifests.Unit, c manifests.Parsed) {
	pins := map[string]manifests.Dependency{}
	for _, d := range c.Dependencies {
		if d.Version != "" {
			pins[d.Target.Name] = d
		}
	}
	for i := range u.Dependencies {
		d := &u.Dependencies[i]
		if p, ok := pins[d.Target.Name]; ok && d.Version == "" && d.Source == manifests.SourceRegistry {
			d.Version, d.Resolution = p.Version, manifests.ResManaged
		}
	}
}

// joinUV joins the uv.lock at dir lockDir to every unit whose directory is a root package of the lock.
func joinUV(units []manifests.Unit, unitAt map[string]int, lockDir string, lk manifests.Parsed, root *manifests.Parsed) {
	roots := map[int]string{} // locked index -> unit dir
	for k, v := range lk.Properties {
		if strings.HasPrefix(k, "root:") {
			i, err := strconv.Atoi(k[5:])
			if err != nil || i < 0 || i >= len(lk.Locked) {
				continue
			}
			if jp, ok := repopath.Join(lockDir, v); ok { // the lock stores the path relative to its own directory
				roots[i] = jp
			}
		}
	}
	rootUnit, hasRoot := unitAt[lockDir]
	if hasRoot {
		units[rootUnit].Files = append(units[rootUnit].Files, lk.File)
		units[rootUnit].Toolchain["tool"] = "uv"
		if py := lk.Toolchain["python"]; py != "" && units[rootUnit].Toolchain["python"] == "" {
			units[rootUnit].Toolchain["python"] = py
		}
	}
	// workspace members: pyproject units matched by the member globs
	if root != nil && len(root.Members) > 0 && hasRoot {
		var dirs []string
		for d := range unitAt {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			if d == lockDir {
				continue
			}
			if matchAny(lockDir, root.Members, d) && !excludedBy(lockDir, root, d) {
				units[rootUnit].Members = appendUniq(units[rootUnit].Members, d)
				units[unitAt[d]].Toolchain["workspace"] = root.File.Path
			}
		}
		for _, m := range root.Members {
			if !strings.ContainsAny(m, "*?[") {
				jp, ok := repopath.Join(lockDir, m)
				if _, has := unitAt[jp]; !ok || !has {
					units[rootUnit].Diagnostics = append(units[rootUnit].Diagnostics, diagAt("python.workspace-member-missing", root.File.Path, 0, "workspace member %s has no pyproject.toml", manifests.CleanText(m, 120)))
				}
			}
		}
	}
	// per-root dependency versions
	drop := map[int]bool{}
	for ri, dir := range roots {
		drop[ri] = true
		ui, ok := unitAt[dir]
		if !ok {
			continue
		}
		if dir != lockDir {
			units[ui].Files = append(units[ui].Files, lk.File)
			units[ui].Toolchain["tool"] = "uv"
		}
		direct := map[string][]int{}
		for _, j := range lk.Locked[ri].DependsOn {
			direct[lk.Locked[j].ID.Name] = append(direct[lk.Locked[j].ID.Name], j)
		}
		fillVersions(&units[ui], lk, func(name string) []int { return direct[name] })
	}
	if hasRoot {
		units[rootUnit].Locked = filterLocked(lk, drop)
	}
}

// resolveWorkspaceSources maps "tool.uv.sources: x = { workspace = true }" to the directory of the unit that provides x. A source nothing provides
// is demoted to a path dependency without a reference (a unit never keeps SourceProject, manifests.CheckTree) with a diagnostic. It does not
// need a uv.lock: the sources table is in pyproject.toml.
func resolveWorkspaceSources(units []manifests.Unit, unitAt map[string]int) {
	provider := map[string]string{}
	for _, d := range unitAtDirs(unitAt) {
		for _, a := range units[unitAt[d]].Provides {
			if _, taken := provider[a.ID.Name]; !taken {
				provider[a.ID.Name] = d
			}
		}
	}
	for _, d := range unitAtDirs(unitAt) {
		u := &units[unitAt[d]]
		for i := range u.Dependencies {
			dep := &u.Dependencies[i]
			if dep.Source != manifests.SourceProject {
				continue
			}
			if pd, ok := provider[dep.Target.Name]; ok && dep.SourceRef() == "workspace" {
				dep.SetSource(manifests.SourcePath, dirOrDot(pd))
				continue
			}
			dep.Source = manifests.SourcePath
			if dep.Extra != nil {
				dep.Extra.SourceRef = ""
			}
			file := ""
			if len(u.Files) > 0 {
				file = u.Files[0].Path
			}
			u.Diagnostics = append(u.Diagnostics, diagAt("python.workspace-source-unresolved", file, int(dep.Line), "%s is a workspace source but no workspace member provides it", dep.TargetDisplay()))
		}
	}
}

func unitAtDirs(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// matchAny reports whether d matches one of the member globs written relative to the workspace root directory base.
func matchAny(base string, globs []string, d string) bool {
	for _, g := range globs {
		if jp, ok := repopath.Join(base, g); ok && repopath.Match(jp, d) {
			return true
		}
	}
	return false
}

func excludedBy(base string, p *manifests.Parsed, d string) bool {
	for k := range p.Properties {
		if rel, ok := strings.CutPrefix(k, "exclude:"); ok {
			if jp, ok := repopath.Join(base, rel); ok && repopath.Match(jp, d) {
				return true
			}
		}
	}
	return false
}

func appendUniq(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// filterLocked drops locked entries and renumbers DependsOn.
func filterLocked(lk manifests.Parsed, drop map[int]bool) []manifests.LockedComponent {
	remap := make([]int, len(lk.Locked))
	n := 0
	for i := range lk.Locked {
		if drop[i] {
			remap[i] = -1
			continue
		}
		remap[i] = n
		n++
	}
	out := make([]manifests.LockedComponent, 0, n)
	for i, l := range lk.Locked {
		if drop[i] {
			continue
		}
		c := l
		c.DependsOn = nil
		for _, j := range l.DependsOn {
			if remap[j] >= 0 {
				c.DependsOn = append(c.DependsOn, remap[j])
			}
		}
		out = append(out, c)
	}
	return out
}

// fillVersions sets Version and ResLocked on registry dependencies from the candidate locked entries of the lock.
func fillVersions(u *manifests.Unit, lk manifests.Parsed, cands func(name string) []int) {
	for i := range u.Dependencies {
		d := &u.Dependencies[i]
		if d.Source != manifests.SourceRegistry {
			continue
		}
		cs := cands(d.Target.Name)
		if len(cs) == 0 {
			if d.Scope != manifests.ScopeOptional && d.Scope != manifests.ScopeBuild && d.Condition() == "" {
				u.Diagnostics = append(u.Diagnostics, diagAt("python.lock-missing-dep", lk.File.Path, int(d.Line), "%s is declared but absent from the lock", d.TargetDisplay()))
			}
			continue
		}
		l := lk.Locked[cs[0]]
		for _, c := range cs { // several candidates (markers): prefer one that satisfies the requirement
			if ok, known := version.Satisfies("pypi", lk.Locked[c].Version, d.Requirement); known && ok {
				l = lk.Locked[c]
				break
			}
		}
		if d.Resolution == manifests.ResPinned && d.Version != "" && d.Version != l.Version {
			u.Diagnostics = append(u.Diagnostics, diagAt("python.lock-stale", lk.File.Path, l.Loc.Line, "%s is pinned to %s but locked at %s", d.TargetDisplay(), d.Version, l.Version))
			continue
		}
		d.Version, d.Resolution = l.Version, manifests.ResLocked
		if d.Requirement != "" {
			if ok, known := version.Satisfies("pypi", l.Version, d.Requirement); known && !ok {
				u.Diagnostics = append(u.Diagnostics, diagAt("python.lock-stale", lk.File.Path, l.Loc.Line, "locked %s %s does not satisfy %q", d.TargetDisplay(), l.Version, d.Requirement))
			}
		}
	}
}

// joinOtherLocks joins poetry.lock, pylock.toml, Pipfile.lock and pip-compile lock files that sit in the unit's directory.
func joinOtherLocks(u *manifests.Unit, g *dirGroup) {
	type src struct {
		lk   manifests.Parsed
		tool string
	}
	var order []src
	for _, id := range []string{PoetryLockID, PylockID, PipfileLockID} {
		if lk, ok := g.locks[id]; ok {
			order = append(order, src{lk, map[string]string{PoetryLockID: "poetry", PylockID: "", PipfileLockID: "pipenv"}[id]})
		}
	}
	for _, r := range g.reqs {
		if r.Kind == manifests.KindLockfile {
			// a pip-compile lock: the scope of its entries follows the file name, like any requirements file
			sc, _ := scopeForFile(r.File.Path)
			locked := make([]manifests.LockedComponent, len(r.Locked))
			copy(locked, r.Locked)
			for i := range locked {
				locked[i].Scope = sc
			}
			r.Locked = locked
			order = append(order, src{r, ""})
		}
	}
	hasUV := false
	for _, f := range u.Files {
		if f.ParserID == UVLockID {
			hasUV = true
		}
	}
	for i, s := range order {
		lk := s.lk
		idx := newNameIndex()
		for j, l := range lk.Locked {
			idx.add(j, lockedName(l), l.Version)
		}
		u.Files = appendFile(u.Files, lk.File)
		if s.tool != "" && u.Toolchain["tool"] == "pip" {
			u.Toolchain["tool"] = s.tool
		}
		if py := lk.Toolchain["python"]; py != "" && u.Toolchain["python"] == "" {
			u.Toolchain["python"] = py
		}
		fillVersions(u, lk, func(name string) []int { return idx.byName[name] })
		if i == 0 && !hasUV {
			declared := map[string]bool{}
			for _, d := range u.Dependencies {
				declared[d.Target.Name] = true
			}
			for _, l := range lk.Locked {
				if declared[l.ID.Name] && l.Relationship != manifests.RelDirect {
					l.Relationship = manifests.RelDirect
				} else if l.Relationship == manifests.RelUnknown {
					l.Relationship = manifests.RelTransitive
				}
				u.Locked = append(u.Locked, l)
			}
			// DependsOn indices stay valid because the whole lock is copied in order
			copy(u.Locked, remapRel(lk, u.Locked, declared))
		}
	}
}

func remapRel(lk manifests.Parsed, cur []manifests.LockedComponent, declared map[string]bool) []manifests.LockedComponent {
	for i := range cur {
		if i < len(lk.Locked) {
			cur[i].DependsOn = lk.Locked[i].DependsOn
		}
	}
	return cur
}

func appendFile(fs []manifests.FileRef, f manifests.FileRef) []manifests.FileRef {
	for _, x := range fs {
		if x.Path == f.Path {
			return fs
		}
	}
	return append(fs, f)
}

func lockedName(l manifests.LockedComponent) string {
	if l.Display != "" {
		return l.Display
	}
	return l.ID.Name
}
