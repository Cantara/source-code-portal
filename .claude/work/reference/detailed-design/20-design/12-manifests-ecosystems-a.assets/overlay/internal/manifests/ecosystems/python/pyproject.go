package python

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
	"github.com/cantara/source-code-portal/internal/manifests/internal/tomlx"
)

type pyproject struct{ base }

func NewPyproject() manifests.Parser {
	return pyproject{base{id: PyprojectID, match: func(p string) bool { return baseName(p) == "pyproject.toml" && !vendoredPath(p) }, kind: manifests.KindManifest}}
}

// locator finds the source line of a string literal at or after a start line (elements of arrays have no index entry). It is built in one pass over
// the file: every quoted string is recorded with its line, and a lookup is a binary search. The first version scanned up to 2,000 lines per literal
// with strings.Contains, which is quadratic on one huge line (a 1 MiB single-line dependency array took 11.4 s; verification VF-12-02).
type locator struct{ occ map[string][]int }

func newLocator(data []byte) locator {
	occ := map[string][]int{}
	line := 1
	for i := 0; i < len(data); i++ {
		switch c := data[i]; c {
		case '\n':
			line++
		case '"', '\'':
			j := i + 1
			for j < len(data) && data[j] != c && data[j] != '\n' {
				if c == '"' && data[j] == '\\' && j+1 < len(data) && data[j+1] != '\n' {
					j++
				}
				j++
			}
			if j < len(data) && data[j] == c {
				if lit := string(data[i+1 : j]); lit != "" {
					if ls := occ[lit]; len(ls) == 0 || ls[len(ls)-1] != line {
						occ[lit] = append(ls, line)
					}
				}
				i = j
			} else {
				i = j - 1 // unterminated on this line: skip the quote only
			}
		}
	}
	return locator{occ}
}

// find returns the first line >= from that holds lit as a quoted string, else from.
func (l locator) find(from int, lit string) int {
	if from < 1 {
		from = 1
	}
	ls := l.occ[lit]
	if k := sort.SearchInts(ls, from); k < len(ls) {
		return ls[k]
	}
	return from
}

type ctxState struct {
	out *manifests.Parsed
	loc locator
}

func (c *ctxState) req(s string, scope manifests.Scope, group string, arrLine int) (manifests.Dependency, bool) {
	line := c.loc.find(arrLine, s)
	r, ok := ParseRequirement(s)
	if !ok {
		c.out.Diagnostics = append(c.out.Diagnostics, diag("python.requirement-invalid", line, "cannot parse requirement %q", manifests.CleanText(s, 80)))
		return manifests.Dependency{}, false
	}
	return depFrom(r, scope, group, line), true
}

var bareVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([a-zA-Z0-9.+-]*)$`)

func (p pyproject) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, rej := p.admit(f, lim)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest}
	tree, ix, err := tomlx.Decode(data)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag(tomlx.Code(err, "python.syntax"), 0, "pyproject.toml cannot be parsed: %v", err))
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return manifests.Parsed{}, err
	}
	cs := &ctxState{out: &out, loc: newLocator(data)}
	out.Toolchain = map[string]string{"language": "python"}
	project := tomlx.Map(tree["project"])
	poetry := tomlx.Map(tomlx.At(tree, "tool", "poetry"))
	uvt := tomlx.Map(tomlx.At(tree, "tool", "uv"))
	pdm := tomlx.Map(tomlx.At(tree, "tool", "pdm"))
	dynamic := map[string]bool{}
	for _, d := range tomlx.List(project["dynamic"]) {
		if s, ok := d.(string); ok {
			dynamic[s] = true
		}
	}
	switch {
	case poetry != nil:
		out.Toolchain["tool"] = "poetry"
	case uvt != nil:
		out.Toolchain["tool"] = "uv"
	case pdm != nil:
		out.Toolchain["tool"] = "pdm"
	}
	if bs := tomlx.Map(tree["build-system"]); bs != nil {
		if be, ok := tomlx.Str(bs["build-backend"]); ok {
			out.Toolchain["build_backend"] = manifests.CleanText(be, 100)
		}
		for _, e := range tomlx.List(bs["requires"]) {
			if s, ok := e.(string); ok {
				if d, ok := cs.req(s, manifests.ScopeBuild, "build-system.requires", ix.Line("build-system.requires")); ok {
					out.Dependencies = append(out.Dependencies, d)
				}
			}
		}
	}

	// ---- identity
	name, _ := tomlx.Str(project["name"])
	ver, _ := tomlx.Str(project["version"])
	if name == "" {
		name, _ = tomlx.Str(poetry["name"])
	}
	if ver == "" && !dynamic["version"] {
		ver, _ = tomlx.Str(poetry["version"])
	}
	if id := idOf(name); id.Name != "" {
		out.Provides = []manifests.Artifact{{ID: id, Display: cleanName(name), Version: manifests.CleanText(ver, 128), Kind: "library"}}
	} else if project != nil || poetry != nil {
		out.Diagnostics = append(out.Diagnostics, diag("python.no-name", ix.Line("project", "tool.poetry"), "no static project name"))
	}
	if rp, ok := tomlx.Str(project["requires-python"]); ok {
		out.Toolchain["python"] = manifests.CleanText(rp, 100)
	}
	switch l := project["license"].(type) {
	case string:
		out.Licenses = append(out.Licenses, manifests.CleanText(l, 200))
	case map[string]any:
		if t, ok := tomlx.Str(l["text"]); ok && len(t) <= 64 && !strings.ContainsAny(t, "\n\r") {
			out.Licenses = append(out.Licenses, manifests.CleanText(t, 64))
		}
	}
	if len(out.Licenses) == 0 {
		if l, ok := tomlx.Str(poetry["license"]); ok {
			out.Licenses = append(out.Licenses, manifests.CleanText(l, 200))
		}
	}

	// ---- PEP 621 dependencies
	if dynamic["dependencies"] {
		out.Diagnostics = append(out.Diagnostics, diag("python.dynamic-dependencies", ix.Line("project.dynamic"), "dependencies are dynamic (%s)", dynTool(poetry, uvt)))
	}
	for _, e := range tomlx.List(project["dependencies"]) {
		if s, ok := e.(string); ok {
			if d, ok := cs.req(s, manifests.ScopeRuntime, "dependencies", ix.Line("project.dependencies")); ok {
				out.Dependencies = append(out.Dependencies, d)
			}
		}
	}
	opt := tomlx.Map(project["optional-dependencies"])
	for _, extra := range ix.KeysBySource("project.optional-dependencies", opt) {
		for _, e := range tomlx.List(opt[extra]) {
			if s, ok := e.(string); ok {
				if d, ok := cs.req(s, manifests.ScopeOptional, extra, ix.Line("project.optional-dependencies."+extra)); ok {
					d.Optional = true
					out.Dependencies = append(out.Dependencies, d)
				}
			}
		}
	}
	// ---- PEP 735 dependency groups
	groups := tomlx.Map(tree["dependency-groups"])
	for _, g := range ix.KeysBySource("dependency-groups", groups) {
		reqs, errs := expandGroup(groups, g, nil, 0)
		for _, e := range errs {
			out.Diagnostics = append(out.Diagnostics, diag(e.code, ix.Line("dependency-groups."+g), "%s", e.msg))
		}
		for _, s := range reqs {
			if d, ok := cs.req(s, scopeForGroup(g), g, ix.Line("dependency-groups."+g)); ok {
				out.Dependencies = append(out.Dependencies, d)
			}
		}
	}
	// ---- Poetry
	if poetry != nil {
		poetryDeps(cs, ix, poetry, project != nil && len(tomlx.List(project["dependencies"])) > 0, &out)
	}
	// ---- uv
	if uvt != nil {
		for _, e := range tomlx.List(uvt["dev-dependencies"]) {
			if s, ok := e.(string); ok {
				if d, ok := cs.req(s, manifests.ScopeDev, "dev", ix.Line("tool.uv.dev-dependencies")); ok {
					out.Dependencies = append(out.Dependencies, d)
				}
			}
		}
		if ws := tomlx.Map(uvt["workspace"]); ws != nil {
			out.Kind = manifests.KindWorkspace
			if project != nil {
				out.Kind = manifests.KindManifest
			}
			out.Properties = ensure(out.Properties)
			for _, m := range tomlx.List(ws["members"]) {
				if s, ok := m.(string); ok {
					if rel, ok := repopath.Rel(s); ok {
						out.Members = append(out.Members, rel) // a glob relative to this pyproject.toml
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
		}
		applyUVSources(&out, uvt, ix)
	}
	// ---- PDM
	if pdm != nil {
		pd := tomlx.Map(pdm["dev-dependencies"])
		for _, g := range ix.KeysBySource("tool.pdm.dev-dependencies", pd) {
			for _, e := range tomlx.List(pd[g]) {
				if s, ok := e.(string); ok {
					if d, ok := cs.req(s, scopeForGroup(g), g, ix.Line("tool.pdm.dev-dependencies."+g)); ok {
						out.Dependencies = append(out.Dependencies, d)
					}
				}
			}
		}
	}
	return out, nil
}

func dynTool(poetry, uvt map[string]any) string {
	if poetry != nil {
		return "tool.poetry"
	}
	return "build backend"
}

func ensure(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

type groupErr struct{ code, msg string }

// expandGroup resolves include-group tables (PEP 735) recursively; names compare normalised.
func expandGroup(groups map[string]any, name string, stack []string, depth int) ([]string, []groupErr) {
	if depth > 16 {
		return nil, []groupErr{{"python.group-cycle", "include-group nesting deeper than 16"}}
	}
	norm := NormalizeName(name)
	for _, s := range stack {
		if s == norm {
			return nil, []groupErr{{"python.group-cycle", "include-group cycle at " + manifests.CleanText(name, 60)}}
		}
	}
	var key string
	for k := range groups {
		if NormalizeName(k) == norm && (key == "" || k < key) {
			key = k
		}
	}
	if key == "" {
		return nil, []groupErr{{"python.group-missing", "include-group names unknown group " + manifests.CleanText(name, 60)}}
	}
	var out []string
	var errs []groupErr
	for _, e := range tomlx.List(groups[key]) {
		switch v := e.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			if inc, ok := tomlx.Str(v["include-group"]); ok {
				r, er := expandGroup(groups, inc, append(stack, norm), depth+1)
				out = append(out, r...)
				errs = append(errs, er...)
			}
		}
	}
	return out, errs
}

// poetryDeps reads [tool.poetry] dependency tables. When PEP 621 dependencies exist, Poetry entries only enrich
// (git/path/url sources) or add entries the project table lacks (Poetry 2 semantics).
func poetryDeps(cs *ctxState, ix tomlx.Index, poetry map[string]any, pep621 bool, out *manifests.Parsed) {
	have := map[manifests.ComponentID]int{}
	for i, d := range out.Dependencies {
		if d.Group == "dependencies" {
			have[d.Target] = i
		}
	}
	handle := func(prefix string, tbl map[string]any, scope manifests.Scope, group string) {
		for _, name := range ix.KeysBySource(prefix, tbl) {
			if name == "python" && prefix == "tool.poetry.dependencies" {
				if s, ok := tomlx.Str(tbl[name]); ok && out.Toolchain["python"] == "" {
					out.Toolchain["python"] = manifests.CleanText(s, 100)
				}
				continue
			}
			for _, d := range poetryEntry(cs, ix.Line(prefix+"."+name), name, tbl[name], scope, group) {
				if i, dup := have[d.Target]; dup && pep621 && group == "dependencies" {
					if d.Source != manifests.SourceRegistry {
						out.Dependencies[i].SetSource(d.Source, d.SourceRef())
					}
					continue
				}
				out.Dependencies = append(out.Dependencies, d)
			}
		}
	}
	handle("tool.poetry.dependencies", tomlx.Map(poetry["dependencies"]), manifests.ScopeRuntime, "dependencies")
	handle("tool.poetry.dev-dependencies", tomlx.Map(poetry["dev-dependencies"]), manifests.ScopeDev, "dev")
	groups := tomlx.Map(poetry["group"])
	for _, g := range ix.KeysBySource("tool.poetry.group", groups) {
		sc := manifests.ScopeRuntime
		if g != "main" {
			sc = scopeForGroup(g)
		}
		handle("tool.poetry.group."+g+".dependencies", tomlx.Map(tomlx.Map(groups[g])["dependencies"]), sc, g)
	}
}

func poetryEntry(cs *ctxState, line int, name string, v any, scope manifests.Scope, group string) []manifests.Dependency {
	one := func(spec any) manifests.Dependency {
		d := manifests.Dependency{Target: idOf(name), Scope: scope, Group: group, Line: int32(line)}
		d.SetTargetDisplay(cleanName(name))
		switch x := spec.(type) {
		case string:
			d.Requirement = manifests.CleanText(x, 200)
		case map[string]any:
			if s, ok := tomlx.Str(x["version"]); ok {
				d.Requirement = manifests.CleanText(s, 200)
			}
			var cond []string
			if s, ok := tomlx.Str(x["markers"]); ok {
				cond = append(cond, s)
			}
			if s, ok := tomlx.Str(x["python"]); ok {
				cond = append(cond, "python "+s)
			}
			if s, ok := tomlx.Str(x["platform"]); ok {
				cond = append(cond, "sys_platform == '"+s+"'")
			}
			d.SetCondition(manifests.CleanText(strings.Join(cond, " and "), 300))
			if b, _ := x["optional"].(bool); b {
				d.Optional = true
				if scope == manifests.ScopeRuntime {
					d.Scope = manifests.ScopeOptional
				}
			}
			switch {
			case x["git"] != nil:
				g, _ := tomlx.Str(x["git"])
				ref := redactURL(g)
				for _, k := range []string{"rev", "tag", "branch"} {
					if r, ok := tomlx.Str(x[k]); ok {
						ref += "#" + manifests.CleanText(r, 100)
						break
					}
				}
				d.SetSource(manifests.SourceGit, ref)
			case x["path"] != nil:
				pth, _ := tomlx.Str(x["path"])
				rel, _ := repopath.Rel(pth) // relative to this pyproject.toml; "" when absolute
				d.SetSource(manifests.SourcePath, rel)
			case x["url"] != nil:
				u, _ := tomlx.Str(x["url"])
				d.SetSource(manifests.SourceURL, redactURL(u))
			}
		default:
			cs.out.Diagnostics = append(cs.out.Diagnostics, diag("python.type-mismatch", line, "dependency %s has an unsupported value type", manifests.CleanText(name, 60)))
		}
		if d.Requirement != "" && bareVersion.MatchString(d.Requirement) { // Poetry: a bare version is exact
			d.Version, d.Resolution = d.Requirement, manifests.ResPinned
		} else if v, ok := ExactPin(d.Requirement); ok {
			d.Version, d.Resolution = v, manifests.ResPinned
		}
		return d
	}
	if arr, ok := v.([]any); ok { // multiple-constraint dependency: one entry per element
		var out []manifests.Dependency
		for _, e := range arr {
			out = append(out, one(e))
		}
		return out
	}
	return []manifests.Dependency{one(v)}
}

func applyUVSources(out *manifests.Parsed, uvt map[string]any, ix tomlx.Index) {
	src := tomlx.Map(uvt["sources"])
	for i := range out.Dependencies {
		d := &out.Dependencies[i]
		var s map[string]any
		for k, v := range src {
			if NormalizeName(k) != d.Target.Name {
				continue
			}
			switch x := v.(type) {
			case map[string]any:
				s = x
			case []any:
				if len(x) > 0 {
					s = tomlx.Map(x[0])
				}
				if len(x) > 1 {
					out.Diagnostics = append(out.Diagnostics, diag("python.uv-source-multiple", ix.Line("tool.uv.sources."+k), "several sources for %s; the first is used", manifests.CleanText(k, 60)))
				}
			}
		}
		if s == nil {
			continue
		}
		switch {
		case s["workspace"] != nil:
			d.SetSource(manifests.SourceProject, "workspace") // resolved by the assembler to the member directory
		case s["git"] != nil:
			g, _ := tomlx.Str(s["git"])
			ref := redactURL(g)
			for _, k := range []string{"rev", "tag", "branch"} {
				if r, ok := tomlx.Str(s[k]); ok {
					ref += "#" + manifests.CleanText(r, 100)
					break
				}
			}
			d.SetSource(manifests.SourceGit, ref)
		case s["path"] != nil:
			pth, _ := tomlx.Str(s["path"])
			rel, _ := repopath.Rel(pth)
			d.SetSource(manifests.SourcePath, rel)
		case s["url"] != nil:
			u, _ := tomlx.Str(s["url"])
			d.SetSource(manifests.SourceURL, redactURL(u))
		case s["index"] != nil:
			n, _ := tomlx.Str(s["index"])
			d.SetSource(manifests.SourceRegistry, "index:"+manifests.CleanText(n, 80))
		}
	}
}
