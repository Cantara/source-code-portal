// Package cargo parses Cargo.toml and Cargo.lock (pure, go-toml/v2) and assembles crates and workspaces.
package cargo

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/internal/redact"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/tomlx"
	"github.com/cantara/source-code-portal/internal/manifests/internal/version"
)

const (
	ManifestParserID = "cargo/manifest"
	LockParserID     = "cargo/lock"
	parserVer        = 1
	inheritMarker    = "workspace=true" // Dependency.Requirement placeholder for { workspace = true }; resolved by the assembler
	poolGroup        = "workspace.dependencies"
)

func idOf(name string) manifests.ComponentID {
	_, n := purl.Normalize("cargo", "", strings.TrimSpace(manifests.CleanText(name, 128))) // lower case, '_' folded to '-' (crates.io treats them as the same name)
	return manifests.ComponentID{Ecosystem: manifests.Cargo, Name: n}
}

// Spec is the EcosystemSpec of crate identities (the registry's built-in rule, restated so that the diagnostic catalogue travels with it).
func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: manifests.Cargo, DisplayName: "Cargo", PURLType: "cargo", Normalize: purl.NormalizeFunc("cargo"),
		VersionSystem: "cargo", Language: "Rust", Diagnostics: catalogue()}
}

type manifestParser struct{}
type lockParser struct{}

func NewManifestParser() manifests.Parser { return manifestParser{} }
func NewLockParser() manifests.Parser     { return lockParser{} }

func (manifestParser) ID() string                       { return ManifestParserID }
func (manifestParser) Version() int                     { return parserVer }
func (manifestParser) Ecosystem() manifests.EcosystemID { return manifests.Cargo }
func (manifestParser) Match(p string) bool              { return path.Base(p) == "Cargo.toml" && !vendored(p) }
func (lockParser) ID() string                           { return LockParserID }
func (lockParser) Version() int                         { return parserVer }
func (lockParser) Ecosystem() manifests.EcosystemID     { return manifests.Cargo }
func (lockParser) Match(p string) bool                  { return path.Base(p) == "Cargo.lock" && !vendored(p) }
func (lockParser) Kind() manifests.FileKind             { return manifests.KindLockfile }

// vendored excludes build output and vendored copies (target/package/.../Cargo.toml is a packaged crate, not a project).
func vendored(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "target" || seg == "node_modules" || seg == "vendor" {
			return true
		}
	}
	return false
}

// reject gates size and encoding; a non-nil Parsed carries only the diagnostic.
func reject(ctx context.Context, f manifests.File, kind manifests.FileKind, lim manifests.Limits) ([]byte, *limits.Guard, *manifests.Parsed) {
	g := limits.NewGuard(ctx, lim)
	if err := g.CheckSize(len(f.Data)); err != nil {
		return nil, g, &manifests.Parsed{Kind: kind, Diagnostics: []manifests.Diagnostic{diag("limit.size", 0, "file is %d bytes, limit %d", len(f.Data), lim.WithDefaults().MaxBytes)}}
	}
	data, err := limits.Input(f.Data)
	if err != nil {
		return nil, g, &manifests.Parsed{Kind: kind, Diagnostics: []manifests.Diagnostic{diag("encoding.unsupported", 0, "file is not UTF-8 text without NUL bytes")}}
	}
	return data, g, nil
}

func depTables() []struct {
	key   string
	scope manifests.Scope
	group string
} {
	return []struct {
		key   string
		scope manifests.Scope
		group string
	}{
		{"dependencies", manifests.ScopeRuntime, "dependencies"},
		{"dev-dependencies", manifests.ScopeTest, "dev-dependencies"},
		{"dev_dependencies", manifests.ScopeTest, "dev-dependencies"},
		{"build-dependencies", manifests.ScopeBuild, "build-dependencies"},
		{"build_dependencies", manifests.ScopeBuild, "build-dependencies"},
	}
}

func (manifestParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, _, rej := reject(ctx, f, manifests.KindManifest, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest}
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "cargo.syntax"), 0, "Cargo.toml cannot be parsed: %v", err))
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return manifests.Parsed{}, err
	}
	out.Toolchain = map[string]string{"language": "rust", "tool": "cargo"}
	out.Properties = map[string]string{}

	pkg := tomlx.Map(tree["package"])
	ws := tomlx.Map(tree["workspace"])
	if pkg == nil && ws != nil {
		out.Kind = manifests.KindWorkspace
	}
	if pkg == nil && ws == nil {
		out.Diagnostics = append(out.Diagnostics, diag("cargo.no-package", 0, "Cargo.toml has neither [package] nor [workspace]"))
	}
	wsPkg := tomlx.Map(tomlx.At(tree, "workspace", "package"))
	if pkg != nil {
		name, _ := tomlx.Str(pkg["name"])
		ver := inheritable(pkg, "version")
		if v, ok := ver.(string); ok {
			ver = manifests.CleanText(v, 128)
		}
		kind := "library"
		if tree["bin"] != nil && tree["lib"] == nil {
			kind = "application"
		}
		art := manifests.Artifact{ID: idOf(name), Display: manifests.CleanText(name, 128), Kind: kind}
		switch v := ver.(type) {
		case string:
			art.Version = v
		case inherit:
			out.Properties["version.workspace"] = "true"
		}
		if art.ID.Name == "" {
			out.Diagnostics = append(out.Diagnostics, diag("cargo.no-name", ix.Line("package"), "[package] has no name"))
		} else {
			out.Provides = []manifests.Artifact{art}
		}
		for _, k := range []string{"edition", "rust-version", "license"} {
			switch v := inheritable(pkg, k).(type) {
			case string:
				setField(&out, k, manifests.CleanText(v, 128))
			case inherit:
				out.Properties[k+".workspace"] = "true"
			}
		}
		if p, ok := pkg["publish"].(bool); ok && !p {
			out.Properties["publish"] = "false"
		}
	}
	if ws != nil {
		for _, m := range tomlx.List(ws["members"]) {
			if s, ok := m.(string); ok {
				if rel, ok := repopath.Rel(s); ok {
					out.Members = append(out.Members, rel) // a glob relative to this Cargo.toml; the assembler joins it with the directory
				} else {
					out.Diagnostics = append(out.Diagnostics, diag("cargo.member-invalid", ix.Line("workspace.members"), "workspace member %s is absolute or malformed", manifests.CleanText(s, 120)))
				}
			}
		}
		for _, m := range tomlx.List(ws["exclude"]) {
			if s, ok := m.(string); ok {
				if rel, ok := repopath.Rel(s); ok {
					out.Properties["exclude:"+rel] = "1"
				}
			}
		}
		for _, k := range []string{"version", "edition", "rust-version", "license"} {
			if v, ok := tomlx.Str(wsPkg[k]); ok {
				out.Properties["workspace.package."+k] = manifests.CleanText(v, 128)
			}
		}
		if r, ok := tomlx.Str(ws["resolver"]); ok {
			out.Properties["resolver"] = manifests.CleanText(r, 16)
		}
		pool := tomlx.Map(ws["dependencies"])
		for _, name := range ix.KeysBySource("workspace.dependencies", pool) {
			d := parseDep(&out, ix, "workspace.dependencies."+name, name, pool[name], manifests.ScopeRuntime, poolGroup, "")
			out.Dependencies = append(out.Dependencies, d)
		}
	}
	for _, t := range depTables() {
		tbl := tomlx.Map(tree[t.key])
		for _, name := range ix.KeysBySource(t.key, tbl) {
			out.Dependencies = append(out.Dependencies, parseDep(&out, ix, t.key+"."+name, name, tbl[name], t.scope, t.group, ""))
		}
	}
	targets := tomlx.Map(tree["target"])
	for _, cfg := range tomlx.SortedKeys(targets) {
		tm := tomlx.Map(targets[cfg])
		for _, t := range depTables() {
			tbl := tomlx.Map(tm[t.key])
			for _, name := range ix.KeysBySource("target."+cfg+"."+t.key, tbl) {
				out.Dependencies = append(out.Dependencies, parseDep(&out, ix, "target."+cfg+"."+t.key+"."+name, name, tbl[name], t.scope, t.group, manifests.CleanText(cfg, 200)))
			}
		}
	}
	if tree["patch"] != nil || tree["replace"] != nil {
		out.Diagnostics = append(out.Diagnostics, diag("cargo.patch-ignored", ix.Line("patch", "replace"), "[patch] and [replace] are not applied"))
	}
	if len(out.Properties) == 0 {
		out.Properties = nil
	}
	return out, nil
}

type inherit struct{}

// inheritable reads package.<key>: a plain value, or { workspace = true } (also written key.workspace = true).
func inheritable(pkg map[string]any, key string) any {
	switch v := pkg[key].(type) {
	case string:
		return v
	case map[string]any:
		if b, _ := v["workspace"].(bool); b {
			return inherit{}
		}
	}
	return nil
}

func setField(out *manifests.Parsed, key, v string) {
	switch key {
	case "edition":
		out.Toolchain["edition"] = v
	case "rust-version":
		out.Toolchain["rust"] = v
	case "license":
		out.Licenses = append(out.Licenses, v)
	}
}

// parseDep converts one dependency entry. keyPath is the index path of the entry.
func parseDep(out *manifests.Parsed, ix tomlx.Index, keyPath, local string, v any, scope manifests.Scope, group, cond string) manifests.Dependency {
	d := manifests.Dependency{Scope: scope, Group: group, Line: int32(ix.Line(keyPath))}
	d.SetCondition(cond)
	srcKind, srcRef := manifests.SourceRegistry, ""
	name := local
	switch x := v.(type) {
	case string:
		d.Requirement = manifests.CleanText(x, 128)
	case map[string]any:
		if s, ok := tomlx.Str(x["package"]); ok && s != "" {
			name = s
		}
		if b, _ := x["workspace"].(bool); b {
			d.Requirement = inheritMarker
		}
		if s, ok := tomlx.Str(x["version"]); ok {
			d.Requirement = manifests.CleanText(s, 128)
		}
		if b, _ := x["optional"].(bool); b {
			d.Optional = true
			if scope == manifests.ScopeRuntime {
				d.Scope = manifests.ScopeOptional
			}
		}
		switch {
		case x["path"] != nil:
			p, _ := tomlx.Str(x["path"])
			srcKind = manifests.SourcePath
			if rel, ok := repopath.Rel(p); ok {
				srcRef = rel // relative to this Cargo.toml; the runner rebases it and judges escapes (assemble.path-escape)
			} else {
				out.Diagnostics = append(out.Diagnostics, diag("cargo.path-invalid", int(d.Line), "path dependency %s is absolute or malformed", manifests.CleanText(name, 80)))
			}
		case x["git"] != nil:
			g, _ := tomlx.Str(x["git"])
			srcKind, srcRef = manifests.SourceGit, redact.URL(g)
			for _, k := range []string{"rev", "tag", "branch"} {
				if r, ok := tomlx.Str(x[k]); ok {
					srcRef += "#" + manifests.CleanText(r, 100)
					break
				}
			}
		case x["registry"] != nil:
			r, _ := tomlx.Str(x["registry"])
			srcRef = "registry:" + manifests.CleanText(r, 100)
		}
	default:
		out.Diagnostics = append(out.Diagnostics, diag("cargo.type-mismatch", int(d.Line), "dependency %s has an unsupported value type", manifests.CleanText(local, 80)))
	}
	d.Target = idOf(name)
	d.SetTargetDisplay(manifests.CleanText(name, 128))
	if group == poolGroup || d.Requirement == inheritMarker {
		// Inheritance is by the TOML KEY, not by the package name: foo = { workspace = true } finds [workspace.dependencies] foo even when that
		// entry says package = "real". The key is the display until the assembler resolves the pool entry (VF-12-06).
		d.SetTargetDisplay(manifests.CleanText(local, 128))
		if group == poolGroup && name != local {
			out.Properties["pool.real:"+manifests.CleanText(local, 128)] = manifests.CleanText(name, 128)
		}
	}
	d.SetSource(srcKind, srcRef)
	if strings.HasPrefix(d.Requirement, "=") && !strings.HasPrefix(d.Requirement, "==") {
		d.Version = strings.TrimSpace(strings.TrimPrefix(d.Requirement, "="))
		d.Resolution = manifests.ResPinned
	}
	return d
}

// ---------------------------------------------------------------- Cargo.lock

func (lockParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, g, rej := reject(ctx, f, manifests.KindLockfile, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindLockfile}
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "cargo.syntax"), 0, "Cargo.lock cannot be parsed: %v", err))
		return out, nil
	}
	pkgs := tomlx.List(tree["package"])
	if v, ok := tree["version"].(int64); ok {
		out.Properties = map[string]string{"lock.version": itoa(v)}
	} else {
		out.Diagnostics = append(out.Diagnostics, diag("cargo.lock-v1", 0, "Cargo.lock has no version field (lock format 1): edges between packages may be incomplete"))
	}
	type key struct{ name, ver string }
	byKey := map[key]int{}
	byName := map[string][]int{}
	for i, p := range pkgs {
		m := tomlx.Map(p)
		n, _ := tomlx.Str(m["name"])
		v, _ := tomlx.Str(m["version"])
		line := ix.Line("package[" + itoa(int64(i)) + "]")
		lc := manifests.LockedComponent{ID: idOf(n), Display: manifests.CleanText(n, 128), Version: manifests.CleanText(v, 128), Relationship: manifests.RelUnknown, Scope: manifests.ScopeUnknown, Loc: manifests.Location{Line: line}}
		src, _ := tomlx.Str(m["source"])
		switch {
		case src == "":
			if out.Properties == nil {
				out.Properties = map[string]string{}
			}
			out.Properties["local:"+itoa(int64(i))] = "1" // workspace member or path dependency: a unit, not a locked component
		case strings.HasPrefix(src, "git+"):
			if h := strings.LastIndexByte(src, '#'); h >= 0 {
				lc.Integrity = "git:" + manifests.CleanText(src[h+1:], 64)
			}
		}
		if cs, ok := tomlx.Str(m["checksum"]); ok && cs != "" {
			lc.Integrity = "sha256:" + manifests.CleanText(cs, 80)
		}
		out.Locked = append(out.Locked, lc)
		byKey[key{n, v}] = i
		byName[n] = append(byName[n], i)
		if err := g.Node(); err != nil {
			if ctx.Err() != nil {
				return manifests.Parsed{}, ctx.Err()
			}
			out.Diagnostics = append(out.Diagnostics, diag("limit.nodes", line, "too many packages"))
			break
		}
	}
	for i, p := range pkgs {
		if i >= len(out.Locked) {
			break
		}
		m := tomlx.Map(p)
		for _, e := range tomlx.List(m["dependencies"]) {
			s, _ := e.(string)
			fields := strings.Fields(s)
			if len(fields) == 0 {
				continue
			}
			idx := -1
			if len(fields) >= 2 {
				if j, ok := byKey[key{fields[0], fields[1]}]; ok {
					idx = j
				}
			} else if c := byName[fields[0]]; len(c) == 1 {
				idx = c[0]
			}
			if idx == i { // a package cannot depend on itself; a hostile lock may claim it
				continue
			}
			if idx < 0 {
				out.Diagnostics = append(out.Diagnostics, diag("cargo.lock-dangling", out.Locked[i].Loc.Line, "dependency %s of %s is not in the lock", manifests.CleanText(s, 100), out.Locked[i].Display))
				continue
			}
			out.Locked[i].DependsOn = append(out.Locked[i].DependsOn, idx)
		}
		sort.Ints(out.Locked[i].DependsOn)
	}
	return out, nil
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---------------------------------------------------------------- assembler

type assembler struct{}

func NewAssembler() manifests.Assembler { return assembler{} }

func (assembler) Ecosystem() manifests.EcosystemID { return manifests.Cargo }

// AssemblerVersion takes part in Registry.SetHash: bump it on ANY change of the units this assembler builds (cached facts are then rebuilt).
func (assembler) AssemblerVersion() int { return assemblerVer }

const assemblerVer = 1

func (assembler) Assemble(ctx context.Context, files manifests.FileSet, parsed []manifests.Parsed) ([]manifests.Unit, error) {
	var mans []manifests.Parsed
	locks := map[string]manifests.Parsed{} // dir -> lock
	for _, p := range parsed {
		switch p.File.ParserID {
		case ManifestParserID:
			mans = append(mans, p)
		case LockParserID:
			locks[repopath.Dir(p.File.Path)] = p
		}
	}
	sort.Slice(mans, func(i, j int) bool { return mans[i].File.Path < mans[j].File.Path })
	manByDir := map[string]int{}
	for i, m := range mans {
		manByDir[repopath.Dir(m.File.Path)] = i
	}
	// workspace roots: manifests with Members or with a workspace table; membership by glob (patterns are relative to the root's directory)
	rootOf := map[string]string{} // member dir -> root dir
	memberDirs := map[string][]string{}
	for _, m := range mans {
		isRoot := m.Kind == manifests.KindWorkspace || len(m.Members) > 0 || hasWorkspaceProps(m)
		if !isRoot {
			continue
		}
		rd := repopath.Dir(m.File.Path)
		for _, d := range sortedDirs(manByDir) {
			if d == rd {
				continue
			}
			if matchesAny(rd, m.Members, d) && !excluded(rd, m, d) {
				if _, taken := rootOf[d]; !taken {
					rootOf[d] = rd
					memberDirs[rd] = append(memberDirs[rd], d)
				}
			}
		}
	}
	var units []manifests.Unit
	var unitRoot []string
	for _, m := range mans {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := repopath.Dir(m.File.Path)
		root, isMember := rootOf[dir]
		if !isMember {
			root = dir
		}
		rootMan := mans[manByDir[root]]
		u := manifests.Unit{Ecosystem: manifests.Cargo, Dir: dirOrDot(dir), Files: []manifests.FileRef{m.File}, Provides: append([]manifests.Artifact(nil), m.Provides...),
			Members: dirsOrNil(memberDirs[dir]), Toolchain: cloneMap(m.Toolchain), Licenses: append([]string(nil), m.Licenses...), Diagnostics: append([]manifests.Diagnostic(nil), m.Diagnostics...)}
		if isMember {
			u.Files = append(u.Files, rootMan.File)
		}
		// inheritance of package fields
		for i := range u.Provides {
			if m.Properties["version.workspace"] == "true" {
				if v := rootMan.Properties["workspace.package.version"]; v != "" {
					u.Provides[i].Version = v
				} else {
					u.Diagnostics = append(u.Diagnostics, diagAt("cargo.inherit-missing", m.File.Path, 0, "version.workspace = true but the workspace defines no package.version"))
				}
			}
		}
		for _, k := range []string{"edition", "rust-version", "license"} {
			if m.Properties[k+".workspace"] == "true" {
				if v := rootMan.Properties["workspace.package."+k]; v != "" {
					switch k {
					case "edition":
						u.Toolchain["edition"] = v
					case "rust-version":
						u.Toolchain["rust"] = v
					case "license":
						u.Licenses = append(u.Licenses, v)
					}
				}
			}
		}
		pool := map[string]manifests.Dependency{}
		for _, d := range rootMan.Dependencies {
			if d.Group == poolGroup {
				pool[d.TargetDisplay()] = d
			}
		}
		for _, d := range m.Dependencies {
			if d.Group == poolGroup {
				continue
			}
			d = d.Clone()
			if d.Requirement == inheritMarker {
				pd, ok := pool[d.TargetDisplay()]
				if !ok {
					u.Diagnostics = append(u.Diagnostics, diagAt("cargo.inherit-missing", m.File.Path, int(d.Line), "%s = { workspace = true } but [workspace.dependencies] has no such entry", d.TargetDisplay()))
					d.Requirement = ""
				} else {
					// the member's own position, scope, group and condition win; requirement, version, source and display come from the pool entry
					opt, scope, group, cond, line := d.Optional, d.Scope, d.Group, d.Condition(), d.Line
					d = pd.Clone()
					if real, renamed := rootMan.Properties["pool.real:"+pd.TargetDisplay()]; renamed {
						d.SetTargetDisplay(real) // the display of a resolved dependency is the real crate, as for a non-inherited rename
					}
					d.Optional = d.Optional || opt
					d.Scope, d.Group, d.Line = scope, group, line
					if d.Extra != nil {
						d.Extra.Condition = ""
					}
					d.SetCondition(cond)
					if opt && scope == manifests.ScopeRuntime {
						d.Scope = manifests.ScopeOptional
					}
				}
			}
			u.Dependencies = append(u.Dependencies, d)
		}
		// lock join: the lock belongs to the workspace root directory
		if lk, ok := locks[root]; ok {
			u.Files = append(u.Files, lk.File)
			joinLock(&u, m, lk)
		}
		units = append(units, u)
		unitRoot = append(unitRoot, root)
	}
	for rd, lk := range locks {
		var rootU *manifests.Unit
		var members []*manifests.Unit
		for i := range units {
			if unitRoot[i] != rd {
				continue
			}
			members = append(members, &units[i])
			if units[i].Dir == dirOrDot(rd) {
				rootU = &units[i]
			}
		}
		if rootU != nil {
			buildLocked(rootU, members, lk)
		}
	}
	return units, nil
}

func hasWorkspaceProps(m manifests.Parsed) bool {
	for k := range m.Properties {
		if strings.HasPrefix(k, "workspace.package.") || k == "resolver" {
			return true
		}
	}
	return false
}

func sortedDirs(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for d := range m {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// matchesAny reports whether dir matches one of the member globs, which are written relative to the workspace root's directory rd.
func matchesAny(rd string, globs []string, dir string) bool {
	for _, g := range globs {
		if jp, ok := repopath.Join(rd, g); ok && repopath.Match(jp, dir) {
			return true
		}
	}
	return false
}

func excluded(rd string, m manifests.Parsed, dir string) bool {
	for k := range m.Properties {
		if rel, ok := strings.CutPrefix(k, "exclude:"); ok {
			if jp, ok := repopath.Join(rd, rel); ok && repopath.Match(jp, dir) {
				return true
			}
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

func dirsOrNil(ds []string) []string {
	if len(ds) == 0 {
		return nil
	}
	sort.Strings(ds)
	return ds
}

// joinLock fills Version/Resolution on the unit's dependencies from the lock and, on the workspace root unit,
// attaches the whole locked set with direct/transitive relationship and scope computed by reachability.
func joinLock(u *manifests.Unit, m manifests.Parsed, lk manifests.Parsed) {
	// find the member's own package in the lock
	rootIdx := -1
	for i, l := range lk.Locked {
		for _, a := range m.Provides {
			ver := a.Version
			if (ver == "" || ver == l.Version) && l.ID == a.ID && lk.Properties["local:"+itoa(int64(i))] == "1" {
				rootIdx = i
			}
		}
	}
	direct := map[int]manifests.Scope{}
	if rootIdx >= 0 {
		for _, j := range lk.Locked[rootIdx].DependsOn {
			direct[j] = manifests.ScopeUnknown
		}
	}
	for i := range u.Dependencies {
		d := &u.Dependencies[i]
		if d.Source == manifests.SourcePath && d.SourceRef() != "" {
			continue
		}
		cands := []int{}
		for j := range direct {
			if lk.Locked[j].ID == d.Target {
				cands = append(cands, j)
			}
		}
		sort.Ints(cands)
		switch len(cands) {
		case 0:
			if rootIdx >= 0 && !d.Optional {
				u.Diagnostics = append(u.Diagnostics, diagAt("cargo.lock-stale", m.File.Path, 0, "%s is declared in %s but missing from the lock", d.TargetDisplay(), m.File.Path))
			}
		default:
			l := lk.Locked[cands[0]]
			if len(cands) > 1 { // two versions of one crate are direct: pick the one satisfying the requirement
				for _, c := range cands {
					if ok, known := version.Satisfies("cargo", lk.Locked[c].Version, d.Requirement); known && ok {
						l = lk.Locked[c]
						break
					}
				}
			}
			if d.Resolution != manifests.ResPinned {
				d.Version = l.Version
				d.Resolution = manifests.ResLocked
			}
			if ok, known := version.Satisfies("cargo", l.Version, d.Requirement); known && !ok && d.Requirement != "" {
				u.Diagnostics = append(u.Diagnostics, diagAt("cargo.lock-stale", lk.File.Path, l.Loc.Line, "locked %s %s does not satisfy %q", d.TargetDisplay(), l.Version, d.Requirement))
			}
		}
	}
}

// buildLocked attaches the whole lock to the workspace root unit: direct = declared by any member of the workspace;
// scope = reachability from the members' runtime, optional, build and dev dependencies, in that precedence.
func buildLocked(root *manifests.Unit, members []*manifests.Unit, lk manifests.Parsed) {
	// scope by reachability from the root package (runtime first, then build, then test)
	scopeOf := make([]manifests.Scope, len(lk.Locked))
	rel := make([]manifests.Relationship, len(lk.Locked))
	order := []manifests.Scope{manifests.ScopeRuntime, manifests.ScopeOptional, manifests.ScopeBuild, manifests.ScopeTest}
	seeds := map[manifests.Scope][]int{}
	for _, mu := range members {
		for _, d := range mu.Dependencies {
			if d.Source == manifests.SourcePath || d.Version == "" {
				continue
			}
			for j, l := range lk.Locked {
				if l.ID == d.Target && l.Version == d.Version && lk.Properties["local:"+itoa(int64(j))] != "1" {
					seeds[d.Scope] = append(seeds[d.Scope], j)
				}
			}
		}
	}
	for _, sc := range order {
		var stack []int
		stack = append(stack, seeds[sc]...)
		for _, s := range seeds[sc] {
			rel[s] = manifests.RelDirect
		}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if scopeOf[n] != manifests.ScopeUnknown {
				continue
			}
			scopeOf[n] = sc
			for _, c := range lk.Locked[n].DependsOn {
				if scopeOf[c] == manifests.ScopeUnknown {
					stack = append(stack, c)
				}
			}
		}
	}
	// local (workspace) packages are units, not locked components
	keep := make([]int, len(lk.Locked))
	n := 0
	for i := range lk.Locked {
		if lk.Properties["local:"+itoa(int64(i))] == "1" {
			keep[i] = -1
			continue
		}
		keep[i] = n
		n++
	}
	for i, l := range lk.Locked {
		if keep[i] < 0 {
			continue
		}
		c := l
		c.Relationship = manifests.RelTransitive
		if rel[i] == manifests.RelDirect {
			c.Relationship = manifests.RelDirect
		}
		c.Scope = scopeOf[i]
		c.DependsOn = nil
		for _, j := range l.DependsOn {
			if keep[j] >= 0 {
				c.DependsOn = append(c.DependsOn, keep[j])
			}
		}
		root.Locked = append(root.Locked, c)
	}
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func catalogue() []manifests.DiagnosticDoc {
	w, i, e := manifests.SevWarning, manifests.SevInfo, manifests.SevError
	return []manifests.DiagnosticDoc{
		{Code: "cargo.syntax", Severity: e, Summary: "Cargo.toml or Cargo.lock is not valid TOML"},
		{Code: "cargo.no-package", Severity: w, Summary: "Cargo.toml has neither [package] nor [workspace]"},
		{Code: "cargo.no-name", Severity: w, Summary: "[package] has no name"},
		{Code: "cargo.type-mismatch", Severity: w, Summary: "a dependency entry has an unsupported value type"},
		{Code: "cargo.member-invalid", Severity: w, Summary: "a workspace member is absolute or malformed"},
		{Code: "cargo.path-invalid", Severity: w, Summary: "a path dependency is absolute or malformed"},
		{Code: "cargo.patch-ignored", Severity: i, Summary: "[patch]/[replace] are not applied"},
		{Code: "cargo.inherit-missing", Severity: w, Summary: "workspace inheritance refers to a missing workspace entry"},
		{Code: "cargo.lock-v1", Severity: i, Summary: "Cargo.lock uses lock format 1"},
		{Code: "cargo.lock-dangling", Severity: w, Summary: "a lock dependency edge refers to an unknown package"},
		{Code: "cargo.lock-stale", Severity: w, Summary: "Cargo.lock disagrees with Cargo.toml"},
	}
}

func Register(r *manifests.Registry) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	for _, p := range []manifests.Parser{NewManifestParser(), NewLockParser()} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return r.RegisterAssembler(NewAssembler())
}
