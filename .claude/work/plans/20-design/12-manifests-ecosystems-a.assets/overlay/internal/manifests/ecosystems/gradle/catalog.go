package gradle

import (
	"context"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/tomlx"
)

type catalogParser struct{ base }

func NewCatalog() manifests.Parser {
	return catalogParser{base{id: CatalogID, match: matchCatalog, kind: manifests.KindCatalog}}
}

// versionSpec renders a catalog version (string or rich table) as a Maven-style requirement.
func versionSpec(v any) (req string, ref string) {
	if s, ok := tomlx.Str(v); ok {
		return s, ""
	}
	m := tomlx.Map(v)
	if m == nil {
		return "", ""
	}
	if r, ok := tomlx.Str(m["ref"]); ok {
		return "", r
	}
	if s, ok := tomlx.Str(m["strictly"]); ok {
		if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "(") {
			return s, ""
		}
		return "[" + s + "]", ""
	}
	for _, k := range []string{"require", "prefer"} {
		if s, ok := tomlx.Str(m[k]); ok {
			return s, ""
		}
	}
	return "", ""
}

func (p catalogParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindCatalog}
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "gradle.syntax"), 0, "version catalog cannot be parsed: %v", err))
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	bad := func(table, key string, why string) {
		out.Diagnostics = append(out.Diagnostics, diag("gradle.catalog-invalid", ix.Line(table+"."+key), "%s %s: %s", table, clean(key, 60), why))
	}
	vs := tomlx.Map(tree["versions"])
	for _, k := range tomlx.SortedKeys(vs) {
		req, ref := versionSpec(vs[k])
		if req == "" && ref == "" {
			bad("versions", k, "unsupported version form")
			continue
		}
		setProp(&out, "version:"+clean(k, 100), clean(req, 200))
	}
	libs := tomlx.Map(tree["libraries"])
	for _, alias := range libs2(ix, "libraries", libs) {
		var g, a string
		var req, ref string
		switch v := libs[alias].(type) {
		case string:
			var ok bool
			g, a, req, _, _, ok = parseNotation(v)
			if !ok {
				bad("libraries", alias, "not group:name[:version]")
				continue
			}
		case map[string]any:
			if mod, ok := tomlx.Str(v["module"]); ok {
				var okp bool
				g, a, _, _, _, okp = parseNotation(mod)
				if !okp {
					bad("libraries", alias, "module is not group:name")
					continue
				}
			} else {
				g, _ = tomlx.Str(v["group"])
				a, _ = tomlx.Str(v["name"])
			}
			if g == "" || a == "" {
				bad("libraries", alias, "missing module or group and name")
				continue
			}
			req, ref = versionSpec(v["version"])
		default:
			bad("libraries", alias, "unexpected type")
			continue
		}
		d := manifests.Dependency{Target: mvn(clean(g, 200), clean(a, 200)), Requirement: clean(req, 200), Group: "catalog-lib", Line: int32(ix.Line("libraries." + alias))}
		d.SetTargetDisplay(clean(alias, 100))
		if ref != "" {
			d.SetSource(manifests.SourceRegistry, "ref:"+clean(ref, 100))
		}
		out.Dependencies = append(out.Dependencies, d)
	}
	pl := tomlx.Map(tree["plugins"])
	for _, alias := range libs2(ix, "plugins", pl) {
		var id, req, ref string
		switch v := pl[alias].(type) {
		case string:
			id, req, _ = strings.Cut(v, ":")
		case map[string]any:
			id, _ = tomlx.Str(v["id"])
			req, ref = versionSpec(v["version"])
		}
		if id == "" {
			bad("plugins", alias, "missing id")
			continue
		}
		d := manifests.Dependency{Target: mvn(clean(id, 200), clean(id+".gradle.plugin", 220)), Requirement: clean(req, 200), Scope: manifests.ScopeBuild, Group: "catalog-plugin", Line: int32(ix.Line("plugins." + alias))}
		d.SetTargetDisplay(clean(alias, 100))
		if ref != "" {
			d.SetSource(manifests.SourceRegistry, "ref:"+clean(ref, 100))
		}
		out.Dependencies = append(out.Dependencies, d)
	}
	bs := tomlx.Map(tree["bundles"])
	for _, k := range tomlx.SortedKeys(bs) {
		var members []string
		for _, m := range tomlx.List(bs[k]) {
			if s, ok := tomlx.Str(m); ok {
				members = append(members, clean(s, 100))
			}
		}
		setProp(&out, "bundle:"+clean(k, 100), strings.Join(members, "\t"))
	}
	if len(out.Dependencies) == 0 && len(out.Properties) == 0 && len(out.Diagnostics) == 0 {
		return out, nil
	}
	return out, nil
}

// libs2 returns the keys of a table in source order.
func libs2(ix tomlx.Index, table string, m map[string]any) []string {
	ks := ix.KeysBySource(table, m)
	sort.SliceStable(ks, func(i, j int) bool { return ix[table+"."+ks[i]] < ix[table+"."+ks[j]] })
	return ks
}
