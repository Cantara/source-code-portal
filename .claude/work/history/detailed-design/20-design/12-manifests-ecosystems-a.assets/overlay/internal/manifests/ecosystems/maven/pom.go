package maven

import (
	"context"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

type pomParser struct{ base }

func NewPom() manifests.Parser {
	return pomParser{base{id: PomID, match: matchPom, kind: manifests.KindManifest}}
}

const maxProps = 5000

func (p pomParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	out := manifests.Parsed{Kind: manifests.KindManifest}
	if max := lim.WithDefaults().MaxBytes; len(f.Data) > max {
		out.Diagnostics = []manifests.Diagnostic{diag("limit.size", 0, "file is %d bytes, limit %d", len(f.Data), max)}
		return out, nil
	}
	data, err := prepareXML(f.Data)
	if err != nil {
		fail(&out, err)
		return out, nil
	}
	g := limits.NewGuard(ctx, lim)
	root, err := parseXML(data, g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err)
		return out, nil
	}
	if root.name != "project" || root.foreign {
		out.Diagnostics = append(out.Diagnostics, diag("maven.not-a-project", root.line, "document element is <%s>", manifests.CleanText(root.name, 40)))
		return out, nil
	}
	pr := &pomReader{out: &out, tc: map[string]string{"tool": "maven", "language": "java"}}
	pr.read(root)
	out.Toolchain = pr.tc
	return out, nil
}

type pomReader struct {
	out *manifests.Parsed
	tc  map[string]string
}

// dep builds a dependency of the POM model: coordinates, raw version, group label, profile condition and source line.
func dep(g, a, req, group, cond string, scope manifests.Scope, line int) manifests.Dependency {
	d := manifests.Dependency{Target: idOf(g, a), Requirement: req, Group: group, Scope: scope, Line: int32(line)}
	d.SetTargetDisplay(g + ":" + a)
	d.SetCondition(cond)
	return d
}

func (r *pomReader) prop(k, v string) {
	if r.out.Properties == nil {
		r.out.Properties = map[string]string{}
	}
	if len(r.out.Properties) < 4*maxProps {
		r.out.Properties[k] = v
	}
}

// coordOK reports whether s can be a groupId or artifactId: Maven coordinates never contain a slash, backslash, colon or whitespace, and an
// identity with one of them cannot round-trip through a purl (found by FuzzParsers). Placeholders such as ${project.groupId} are fine.
func coordOK(s string) bool { return !strings.ContainsAny(s, "/\\: ") }

func clean(s string, max int) string { return manifests.CleanText(s, max) }

func (r *pomReader) diag(code string, line int, format string, args ...any) {
	r.out.Diagnostics = append(r.out.Diagnostics, diag(code, line, format, args...))
}

func (r *pomReader) read(root *xnode) {
	// parent
	if pn := root.child("parent"); pn != nil {
		pg, pa := clean(pn.txt("groupId"), 200), clean(pn.txt("artifactId"), 200)
		if coordOK(pg) && coordOK(pa) {
			d := manifests.Dependency{Target: idOf(pg, pa), Requirement: clean(pn.txt("version"), 200), Group: "parent", Line: int32(pn.line)}
			r.out.Parent = &d
		} else {
			r.diag("maven.dependency-incomplete", pn.line, "parent with an invalid groupId or artifactId")
		}
		if rp := pn.child("relativePath"); rp == nil {
			r.prop("parent.relativePath", "../pom.xml")
			r.prop("parent.relativePath.default", "true")
		} else {
			r.prop("parent.relativePath", clean(rp.text, 300))
		}
	}
	g, a, v, pkg := clean(root.txt("groupId"), 200), clean(root.txt("artifactId"), 200), clean(root.txt("version"), 200), clean(root.txt("packaging"), 40)
	if pkg == "" {
		pkg = "jar"
	}
	r.prop("packaging", pkg)
	if a == "" {
		r.diag("maven.no-artifactId", root.line, "POM has no artifactId")
	} else if !coordOK(g) || !coordOK(a) {
		r.diag("maven.dependency-incomplete", root.line, "project with an invalid groupId or artifactId")
	} else {
		r.out.Provides = append(r.out.Provides, manifests.Artifact{ID: idOf(g, a), Display: a, Version: v, Kind: artifactKind(pkg, root)})
	}
	if n := root.txt("name"); n != "" {
		r.prop("name", clean(n, 200))
	}
	// properties
	r.readProps(root.child("properties"), "prop:")
	// own sections
	r.readDeps(root.child("dependencies"), "", "", manifests.ScopeRuntime)
	if dm := root.child("dependencyManagement"); dm != nil {
		r.readDeps(dm.child("dependencies"), "managed", "", manifests.ScopeRuntime)
	}
	r.readBuild(root.child("build"), "")
	// profiles
	var ids []string
	for _, pf := range root.child("profiles").children("profile") {
		id := clean(pf.txt("id"), 100)
		if id == "" {
			id = "default"
		}
		ids = append(ids, id)
		cond := "profile:" + id
		r.readProps(pf.child("properties"), "profile:"+id+":prop:")
		if act := pf.child("activation"); act != nil {
			if act.txt("activeByDefault") == "true" {
				r.prop("profile:"+id+":activeByDefault", "true")
			}
			if j := act.txt("jdk"); j != "" {
				r.prop("profile:"+id+":jdk", clean(j, 60))
			}
			if o := act.child("os"); o != nil {
				r.prop("profile:"+id+":os", clean(o.txt("name")+"|"+o.txt("family")+"|"+o.txt("arch")+"|"+o.txt("version"), 200))
			}
			if pr := act.child("property"); pr != nil {
				r.prop("profile:"+id+":property", clean(pr.txt("name")+"="+pr.txt("value"), 200))
			}
			if fl := act.child("file"); fl != nil {
				r.prop("profile:"+id+":file", clean(fl.txt("exists")+"|"+fl.txt("missing"), 300))
			}
		}
		r.readDeps(pf.child("dependencies"), "", cond, manifests.ScopeRuntime)
		if dm := pf.child("dependencyManagement"); dm != nil {
			r.readDeps(dm.child("dependencies"), "managed", cond, manifests.ScopeRuntime)
		}
		r.readBuild(pf.child("build"), cond)
	}
	if len(ids) > 0 {
		r.prop("profile-ids", strings.Join(ids, "\t"))
	}
	// modules (Maven 3) and subprojects (Maven 4)
	for _, m := range append(root.child("modules").children("module"), root.child("subprojects").children("subproject")...) {
		if m.text == "" {
			continue
		}
		if rel, ok := repopath.Rel(clean(m.text, 300)); ok { // a directory (or a pom file) relative to this POM
			r.out.Members = append(r.out.Members, rel)
		} else {
			r.diag("maven.module-invalid", m.line, "module %s is absolute or malformed", clean(m.text, 80))
		}
	}
	// licences (declared names only)
	for _, l := range root.child("licenses").children("license") {
		if n := l.txt("name"); n != "" {
			r.out.Licenses = append(r.out.Licenses, clean(n, 200))
		}
	}
	if r.tc["kotlin"] == "true" {
		r.tc["language"] = "kotlin"
	}
	if r.tc["language"] == "java" {
		for _, d := range r.out.Dependencies {
			if d.Target.Namespace == "org.jetbrains.kotlin" && strings.HasPrefix(d.Target.Name, "kotlin-stdlib") {
				r.tc["language"] = "kotlin"
			}
		}
	}
}

func (r *pomReader) readProps(n *xnode, prefix string) {
	if n == nil {
		return
	}
	for _, k := range n.kids {
		if k.name != "" && !k.foreign {
			r.prop(prefix+clean(k.name, 200), clean(k.text, 1000))
		}
	}
}

func scopeOf(s string) (manifests.Scope, bool) {
	switch s {
	case "compile", "runtime":
		return manifests.ScopeRuntime, true
	case "test":
		return manifests.ScopeTest, true
	case "provided", "system":
		return manifests.ScopeProvided, true
	}
	return manifests.ScopeUnknown, false
}

// readDeps reads a <dependencies> element. group "managed" collects dependencyManagement; import-scoped BOMs go to Imports.
func (r *pomReader) readDeps(n *xnode, group, cond string, _ manifests.Scope) {
	for _, dn := range n.children("dependency") {
		g, a := clean(dn.txt("groupId"), 200), clean(dn.txt("artifactId"), 200)
		if g == "" || a == "" || !coordOK(g) || !coordOK(a) {
			r.diag("maven.dependency-incomplete", dn.line, "dependency without a valid groupId or artifactId")
			continue
		}
		d := dep(g, a, clean(dn.txt("version"), 200), group, cond, manifests.ScopeUnknown, dn.line)
		typ, cls := clean(dn.txt("type"), 60), clean(dn.txt("classifier"), 100)
		var q []string
		if cls != "" {
			q = append(q, "classifier="+cls)
		}
		if typ != "" && typ != "jar" {
			q = append(q, "type="+typ)
		}
		sort.Strings(q)
		d.SetQualifiers(strings.Join(q, "&"))
		if dn.txt("optional") == "true" {
			d.Optional = true
		}
		sc := dn.txt("scope")
		if sc == "import" && group == "managed" && (typ == "pom") {
			imp := d
			imp.Group = "import"
			r.out.Imports = append(r.out.Imports, imp)
			continue
		}
		if s, ok := scopeOf(sc); ok {
			d.Scope = s
			if sc == "system" {
				r.diag("maven.system-scope", dn.line, "system-scoped dependency %s", clean(g+":"+a, 120))
			}
		} else if sc != "" {
			r.diag("maven.scope-placeholder", dn.line, "scope %q on %s", clean(sc, 40), clean(g+":"+a, 120))
		}
		r.out.Dependencies = append(r.out.Dependencies, d)
	}
}

// readBuild reads plugins, pluginManagement and extensions as build-scope dependencies.
func (r *pomReader) readBuild(b *xnode, cond string) {
	if b == nil {
		return
	}
	plug := func(list *xnode, group string) {
		for _, pn := range list.children("plugin") {
			g, a := clean(pn.txt("groupId"), 200), clean(pn.txt("artifactId"), 200)
			if a == "" {
				continue
			}
			if g == "" {
				g = "org.apache.maven.plugins"
			}
			d := dep(g, a, clean(pn.txt("version"), 200), group, cond, manifests.ScopeBuild, pn.line)
			r.out.Dependencies = append(r.out.Dependencies, d)
			if a == "kotlin-maven-plugin" && group == "plugin" {
				r.tc["kotlin"] = "true"
			}
			if a == "maven-compiler-plugin" {
				if cfg := pn.child("configuration"); cfg != nil {
					for _, k := range []string{"release", "source", "target"} {
						if v := cfg.txt(k); v != "" {
							r.tc["compiler."+k] = clean(v, 40)
						}
					}
				}
			}
			for _, dn := range pn.child("dependencies").children("dependency") {
				dg, da := clean(dn.txt("groupId"), 200), clean(dn.txt("artifactId"), 200)
				if dg == "" || da == "" {
					continue
				}
				r.out.Dependencies = append(r.out.Dependencies, dep(dg, da, clean(dn.txt("version"), 200), "plugin-dependency", cond, manifests.ScopeBuild, dn.line))
			}
		}
	}
	plug(b.child("plugins"), "plugin")
	if pm := b.child("pluginManagement"); pm != nil {
		plug(pm.child("plugins"), "plugin-managed")
	}
	for _, en := range b.child("extensions").children("extension") {
		g, a := clean(en.txt("groupId"), 200), clean(en.txt("artifactId"), 200)
		if g != "" && a != "" {
			r.out.Dependencies = append(r.out.Dependencies, dep(g, a, clean(en.txt("version"), 200), "extension", cond, manifests.ScopeBuild, en.line))
		}
	}
}

// ---- .mvn/maven.config ----

type configParser struct{ base }

func NewConfig() manifests.Parser {
	return configParser{base{id: ConfigID, match: matchConfig, kind: manifests.KindWorkspace}}
}

func (p configParser) Parse(_ context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	out := manifests.Parsed{Kind: manifests.KindWorkspace}
	if max := lim.WithDefaults().MaxBytes; len(f.Data) > max {
		out.Diagnostics = []manifests.Diagnostic{diag("limit.size", 0, "file is %d bytes, limit %d", len(f.Data), max)}
		return out, nil
	}
	data, err := limits.Input(f.Data)
	if err != nil {
		out.Diagnostics = []manifests.Diagnostic{diag("encoding.unsupported", 0, "file is not UTF-8 text without NUL bytes")}
		return out, nil
	}
	toks := splitArgs(string(data))
	var profiles []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case strings.HasPrefix(t, "-D") && len(t) > 2:
			k, v, _ := strings.Cut(t[2:], "=")
			if out.Properties == nil {
				out.Properties = map[string]string{}
			}
			if len(out.Properties) < maxProps {
				if v == "" && !strings.Contains(t, "=") {
					v = "true"
				}
				out.Properties["prop:"+clean(k, 200)] = clean(v, 1000)
			}
		case t == "-D" && i+1 < len(toks):
			i++
			k, v, _ := strings.Cut(toks[i], "=")
			if out.Properties == nil {
				out.Properties = map[string]string{}
			}
			if v == "" && !strings.Contains(toks[i], "=") {
				v = "true"
			}
			out.Properties["prop:"+clean(k, 200)] = clean(v, 1000)
		case t == "-P" || t == "--activate-profiles":
			if i+1 < len(toks) {
				i++
				profiles = append(profiles, strings.Split(toks[i], ",")...)
			}
		case strings.HasPrefix(t, "-P") && len(t) > 2:
			profiles = append(profiles, strings.Split(t[2:], ",")...)
		case strings.HasPrefix(t, "--activate-profiles="):
			profiles = append(profiles, strings.Split(strings.TrimPrefix(t, "--activate-profiles="), ",")...)
		case quietOption(t):
		default:
			out.Diagnostics = append(out.Diagnostics, diag("maven.config-option-ignored", 1, "option %s", clean(t, 60)))
		}
	}
	if len(profiles) > 0 {
		if out.Properties == nil {
			out.Properties = map[string]string{}
		}
		for i := range profiles {
			profiles[i] = clean(strings.TrimSpace(profiles[i]), 100)
		}
		out.Properties["profiles"] = strings.Join(profiles, ",")
	}
	return out, nil
}

// splitArgs splits on whitespace honouring double and single quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var q rune
	has := false
	for _, r := range s {
		switch {
		case q != 0:
			if r == q {
				q = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			q, has = r, true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
		if len(out) > 2000 {
			break
		}
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// quietOption lists CLI flags that carry no dependency information and are not worth a diagnostic.
func quietOption(t string) bool {
	switch t {
	case "-B", "--batch-mode", "-ntp", "--no-transfer-progress", "-q", "--quiet", "-e", "--errors", "-X", "--debug", "-V", "--show-version",
		"-U", "--update-snapshots", "-o", "--offline", "-fae", "--fail-at-end", "-ff", "--fail-fast", "-fn", "--fail-never", "-N", "--non-recursive",
		"-T", "--threads", "-1C", "-C", "--strict-checksums", "-c", "--lax-checksums", "-nsu", "--no-snapshot-updates":
		return true
	}
	return strings.HasPrefix(t, "-T") || strings.HasPrefix(t, "--threads=")
}

// artifactKind maps the packaging to the closed artifact kind vocabulary (library, application, plugin, bom, image).
func artifactKind(packaging string, root *xnode) string {
	switch packaging {
	case "maven-plugin":
		return "plugin"
	case "war", "ear", "rar", "app-client":
		return "application"
	case "pom":
		// a pure bill of materials manages versions and declares nothing else
		if root.child("dependencyManagement") != nil && root.child("modules") == nil && root.child("dependencies") == nil && root.child("build") == nil {
			return "bom"
		}
	}
	return "library"
}
