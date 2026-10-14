package gradle

import (
	"path"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
)

// resolve turns one raw script declaration into zero or more effective dependencies.
func (u *uctx) resolve(d manifests.Dependency) []manifests.Dependency {
	file, line := d.Loc().Path, int(d.Line)
	switch {
	case d.SourceRef() == "catalog" && d.Source == manifests.SourceRegistry:
		return u.resolveCatalog(d)
	case d.Source == manifests.SourceProject || d.Source == manifests.SourcePath:
		return []manifests.Dependency{d}
	case d.Target.Namespace == "" && strings.HasPrefix(d.Target.Name, "${"):
		s := u.interp(d.Target.Name, file, line)
		if strings.Contains(s, "${") {
			u.rec.add(diagAt("gradle.variable-unresolved", file, line, "%s", clean(d.Target.Name, 80)))
			return nil
		}
		g, a, v, c, e, ok := parseNotation(s)
		if !ok {
			u.rec.add(diagAt("gradle.unresolved-notation", file, line, "notation %q", clean(s, 80)))
			return nil
		}
		d.Target, d.Requirement = mvn(clean(g, 200), clean(a, 200)), clean(v, 200)
		d.SetTargetDisplay(clean(g+":"+a, 400))
		setQualifiers(&d, qualifiers(c, e))
		return []manifests.Dependency{d}
	}
	d.Target = mvn(u.interp(d.Target.Namespace, file, line), u.interp(d.Target.Name, file, line))
	d.SetTargetDisplay(clean(d.Target.Namespace+":"+d.Target.Name, 400))
	d.Requirement = clean(u.interp(d.Requirement, file, line), 200)
	setQualifiers(&d, clean(u.interp(d.Qualifiers(), file, line), 200))
	if strings.Contains(d.Requirement, "${kotlinPluginVersion}") {
		d.Requirement, d.Resolution = "", manifests.ResManaged
	}
	return []manifests.Dependency{d}
}

func (u *uctx) resolveCatalog(d manifests.Dependency) []manifests.Dependency {
	file, line := d.Loc().Path, int(d.Line)
	cat, p := d.Target.Namespace, d.Target.Name
	c := u.b.cats[cat]
	if c == nil {
		u.rec.add(diagAt("gradle.catalog-missing", file, line, "catalog %q is not defined", clean(cat, 60)))
		return nil
	}
	mk := func(l catLib, group string, scope manifests.Scope) manifests.Dependency {
		req := u.resolveVersion(c, l)
		r := mkd(mvn(clean(l.g, 200), clean(l.a, 200)), clean(req, 200), clean(l.g+":"+l.a, 400), "", group, d.Condition(), scope, int(d.Line))
		r.SetLocPath(file)
		return r
	}
	switch {
	case strings.HasPrefix(p, "versions."):
		return nil
	case strings.HasPrefix(p, "bundles."):
		name := normAlias(strings.TrimPrefix(p, "bundles."))
		members, ok := c.bundles[name]
		if !ok {
			u.rec.add(diagAt("gradle.catalog-missing", file, line, "bundle %q is not in catalog %s", clean(name, 60), clean(cat, 40)))
			return nil
		}
		var out []manifests.Dependency
		for _, m := range members {
			if l, ok := c.libs[m]; ok {
				c.used["lib:"+m] = true
				out = append(out, mk(l, d.Group, d.Scope))
			} else {
				u.rec.add(diagAt("gradle.catalog-missing", file, line, "bundle member %q is not a library", clean(m, 60)))
			}
		}
		return out
	case strings.HasPrefix(p, "plugins."):
		name := normAlias(strings.TrimPrefix(p, "plugins."))
		l, ok := c.plugins[name]
		if !ok {
			u.rec.add(diagAt("gradle.catalog-missing", file, line, "plugin alias %q is not in catalog %s", clean(name, 60), clean(cat, 40)))
			return nil
		}
		c.used["plugin:"+name] = true
		ver := u.resolveVersion(c, l)
		u.plugins[l.g] = ver
		if ver == "" { // no version: a convention plugin from an included build, not a Maven artifact
			return nil
		}
		return []manifests.Dependency{mk(l, "plugin", manifests.ScopeBuild)}
	}
	name := normAlias(p)
	l, ok := c.libs[name]
	if !ok {
		u.rec.add(diagAt("gradle.catalog-missing", file, line, "alias %q is not in catalog %s", clean(name, 60), clean(cat, 40)))
		return nil
	}
	c.used["lib:"+name] = true
	return []manifests.Dependency{mk(l, d.Group, d.Scope)}
}

func lockRank(s manifests.Scope) int {
	switch s {
	case manifests.ScopeRuntime:
		return 0
	case manifests.ScopeProvided:
		return 1
	case manifests.ScopeTest:
		return 2
	}
	return 3
}

// joinLocks merges the lockfiles of one project into the unit and fills the locked versions of declared dependencies.
func joinLocks(unit *manifests.Unit, g *group, b *build, isRoot bool) []manifests.LockedComponent {
	if g == nil || len(g.locks) == 0 {
		return nil
	}
	type key struct {
		id  manifests.ComponentID
		ver string
	}
	merged := map[key]manifests.LockedComponent{}
	locks := append([]*manifests.Parsed{}, g.locks...)
	sort.Slice(locks, func(i, j int) bool { return locks[i].File.Path < locks[j].File.Path })
	for _, lk := range locks {
		unit.Files = append(unit.Files, lk.File)
		unit.Diagnostics = append(unit.Diagnostics, lk.Diagnostics...)
		perConf := ""
		if path.Base(path.Dir(lk.File.Path)) == "dependency-locks" {
			perConf = strings.TrimSuffix(path.Base(lk.File.Path), ".lockfile") // the configuration is the file name
		}
		for _, l := range lk.Locked {
			if l.Scope == manifests.ScopeUnknown { // a line without "=configurations"
				l.Scope = manifests.ScopeRuntime
				if perConf != "" {
					l.Scope = rankScope(confRank(perConf))
				}
			}
			k := key{l.ID, l.Version}
			if old, ok := merged[k]; ok {
				if lockRank(l.Scope) < lockRank(old.Scope) {
					old.Scope = l.Scope
					merged[k] = old
				}
				continue
			}
			merged[k] = l
		}
	}
	keys := make([]key, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].id != keys[j].id {
			return keys[i].id.String() < keys[j].id.String()
		}
		return keys[i].ver < keys[j].ver
	})
	out := make([]manifests.LockedComponent, 0, len(keys))
	idx := map[manifests.ComponentID][]int{}
	for i, k := range keys {
		out = append(out, merged[k])
		idx[k.id] = append(idx[k.id], i)
	}
	for i := range unit.Dependencies {
		d := &unit.Dependencies[i]
		if d.Source != manifests.SourceRegistry || d.Group == "plugin" || d.Group == "catalog-unused" || d.Group == "bom" {
			continue
		}
		cands := idx[d.Target]
		if len(cands) == 0 {
			continue
		}
		pick := cands[len(cands)-1]
		for _, c := range cands {
			if out[c].Version == d.Version {
				pick = c
			}
		}
		d.Version, d.Resolution = manifests.CleanText(out[pick].Version, 128), manifests.ResLocked
		out[pick].Relationship = manifests.RelDirect
	}
	return out
}

// setQualifiers replaces the purl qualifiers of a dependency this package owns (a private Extra), which the setter cannot do for "".
func setQualifiers(d *manifests.Dependency, q string) {
	if d.Extra != nil {
		d.Extra.Qualifiers = q
		return
	}
	d.SetQualifiers(q)
}
