package npm

import (
	"context"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type packageJSON struct{ base }

func NewPackageJSON() manifests.Parser {
	return packageJSON{base{id: PackageJSONID, match: func(p string) bool { return path0(p) == "package.json" && !ignoredPath(p) }, kind: manifests.KindManifest}}
}

func path0(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// depSections maps the package.json dependency sections to scopes.
func depSections() []struct {
	key   string
	scope manifests.Scope
} {
	return []struct {
		key   string
		scope manifests.Scope
	}{
		{"dependencies", manifests.ScopeRuntime},
		{"devDependencies", manifests.ScopeDev},
		{"peerDependencies", manifests.ScopePeer},
		{"optionalDependencies", manifests.ScopeOptional},
	}
}

func (p packageJSON) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest}
	g := limits.NewGuard(ctx, lim)
	root, err := parseJSON(data, g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "npm.syntax")
		return out, nil
	}
	if root.kind != 'o' {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", 1, "package.json must be an object"))
		return out, nil
	}
	typeMismatch := func(key string, n *jnode) {
		out.Diagnostics = append(out.Diagnostics, diag("npm.type-mismatch", n.line, "%s has an unexpected type and was skipped", key))
	}
	name := strings.TrimSpace(root.get("name").text())
	ver := root.get("version").text()
	if name != "" {
		a := manifests.Artifact{ID: idOf(name), Display: manifests.CleanText(name, 214), Version: manifests.CleanText(ver, 128), Kind: "library"}
		out.Provides = append(out.Provides, a)
	} else {
		out.Diagnostics = append(out.Diagnostics, diag("npm.no-name", 1, "package.json has no name"))
	}
	optionalPeers := map[string]bool{}
	if m := root.get("peerDependenciesMeta"); m != nil && m.kind == 'o' {
		for _, k := range m.keys {
			if m.obj[k].get("optional").isTrue() {
				optionalPeers[k] = true
			}
		}
	}
	for _, sec := range depSections() {
		n := root.get(sec.key)
		if n == nil {
			continue
		}
		if n.kind != 'o' {
			typeMismatch(sec.key, n)
			continue
		}
		for _, k := range n.keys {
			v := n.obj[k]
			if v.kind != 's' {
				typeMismatch(sec.key+"."+k, v)
				continue
			}
			d := depOf(k, v.str, sec.scope, sec.scope == manifests.ScopeOptional, "", v.line)
			if strings.HasPrefix(strings.ToLower(v.str), "npm:") && len(out.Properties) < 4*maxOverrideProps {
				setProp(&out, "alias:"+manifests.CleanText(v.str, 300), manifests.CleanText(k, 214))
			}
			if sec.scope == manifests.ScopePeer && optionalPeers[k] {
				d.Optional = true
			}
			out.Dependencies = append(out.Dependencies, d)
		}
	}
	// workspaces: array of globs or {packages: [...]} (yarn classic)
	ws := root.get("workspaces")
	if ws != nil {
		list := ws
		if ws.kind == 'o' {
			list = ws.get("packages")
		}
		if list != nil && list.kind == 'a' {
			for _, e := range list.arr {
				if e.kind == 's' && e.str != "" {
					if m, ok := memberOf(e.str); ok {
						out.Members = append(out.Members, m)
					} else {
						out.Diagnostics = append(out.Diagnostics, diag("npm.member-invalid", e.line, "workspace pattern is absolute or malformed"))
					}
				}
			}
		}
		// catalogs (bun, yarn): top level and inside workspaces
		for _, holder := range []*jnode{root, ws} {
			readCatalogs(holder, &out)
		}
	} else {
		readCatalogs(root, &out)
	}
	// overrides, resolutions: recorded as properties, never applied (precedence is the lockfile's)
	for _, key := range []string{"overrides", "resolutions"} {
		if n := root.get(key); n != nil && n.kind == 'o' {
			flattenOverrides(key, "", n, &out, 0)
		}
	}
	if pm := root.get("pnpm"); pm != nil {
		if o := pm.get("overrides"); o != nil && o.kind == 'o' {
			flattenOverrides("overrides", "", o, &out, 0)
		}
	}
	// toolchain
	tc := map[string]string{"language": "javascript"}
	if e := root.get("engines"); e != nil && e.kind == 'o' {
		for _, k := range []string{"node", "npm", "pnpm", "yarn", "bun"} {
			if v := e.get(k).text(); v != "" {
				tc["engines."+k] = manifests.CleanText(v, 100)
			}
		}
	}
	if pm := root.get("packageManager").text(); pm != "" {
		tool, ver := pm, ""
		if i := strings.IndexByte(pm, '@'); i > 0 {
			tool, ver = pm[:i], pm[i+1:]
		}
		if j := strings.IndexByte(ver, '+'); j >= 0 {
			ver = ver[:j]
		}
		tc["tool"], tc["toolVersion"] = manifests.CleanText(tool, 20), manifests.CleanText(ver, 64)
	}
	if t := root.get("type").text(); t != "" {
		tc["module"] = manifests.CleanText(t, 20)
	}
	for _, sec := range []string{"dependencies", "devDependencies"} {
		if d := root.get(sec); d != nil && d.get("typescript") != nil {
			tc["language"] = "typescript"
		}
	}
	out.Toolchain = tc
	// licence: SPDX string, {type}, or the deprecated licenses array
	switch lic := root.get("license"); {
	case lic != nil && lic.kind == 's':
		out.Licenses = append(out.Licenses, manifests.CleanText(lic.str, 200))
	case lic != nil && lic.kind == 'o':
		if t := lic.get("type").text(); t != "" {
			out.Licenses = append(out.Licenses, manifests.CleanText(t, 200))
		}
	}
	if ls := root.get("licenses"); ls != nil && ls.kind == 'a' && len(out.Licenses) == 0 {
		for _, e := range ls.arr {
			if t := e.get("type").text(); t != "" {
				out.Licenses = append(out.Licenses, manifests.CleanText(t, 200))
			}
		}
	}
	if root.get("private").isTrue() {
		setProp(&out, "private", "true")
	}
	return out, nil
}

func setProp(p *manifests.Parsed, k, v string) {
	if p.Properties == nil {
		p.Properties = map[string]string{}
	}
	p.Properties[k] = v
}

const maxOverrideProps = 500

func flattenOverrides(root, prefix string, n *jnode, out *manifests.Parsed, depth int) {
	if depth > 4 {
		return
	}
	for _, k := range n.keys {
		if len(out.Properties) >= maxOverrideProps+8 {
			return
		}
		v := n.obj[k]
		key := k
		if prefix != "" {
			key = prefix + ">" + k
		}
		switch v.kind {
		case 's':
			setProp(out, "override:"+manifests.CleanText(key, 200), manifests.CleanText(v.str, 200))
		case 'o':
			if d := v.get("."); d != nil && d.kind == 's' {
				setProp(out, "override:"+manifests.CleanText(key, 200), manifests.CleanText(d.str, 200))
			}
			flattenOverrides(root, key, v, out, depth+1)
		}
	}
}

// readCatalogs records bun/yarn style catalogs as Properties "catalog:<name>:<package>" = range ("default" is the unnamed one).
func readCatalogs(h *jnode, out *manifests.Parsed) {
	if h == nil || h.kind != 'o' {
		return
	}
	if c := h.get("catalog"); c != nil && c.kind == 'o' {
		for _, k := range c.keys {
			if c.obj[k].kind == 's' {
				setProp(out, "catalog:default:"+manifests.CleanText(k, 214), manifests.CleanText(c.obj[k].str, 200))
			}
		}
	}
	if cs := h.get("catalogs"); cs != nil && cs.kind == 'o' {
		for _, cn := range cs.keys {
			c := cs.obj[cn]
			if c.kind != 'o' {
				continue
			}
			for _, k := range c.keys {
				if c.obj[k].kind == 's' {
					setProp(out, "catalog:"+manifests.CleanText(cn, 100)+":"+manifests.CleanText(k, 214), manifests.CleanText(c.obj[k].str, 200))
				}
			}
		}
	}
}
