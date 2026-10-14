package gradle

import (
	"context"
	"regexp"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

type scriptParser struct{ base }

func NewScript() manifests.Parser {
	return scriptParser{base{id: ScriptID, match: isBase("build.gradle", "build.gradle.kts"), kind: manifests.KindManifest}}
}

type settingsParser struct{ base }

func NewSettings() manifests.Parser {
	return settingsParser{base{id: SettingsID, match: isBase("settings.gradle", "settings.gradle.kts"), kind: manifests.KindWorkspace}}
}

// st is the lexical interpretation state of one script.
type st struct {
	out      *manifests.Parsed
	settings bool
	local    bool // a local-dependency diagnostic was already emitted
	apply    bool
	// inline catalog builder (settings only)
	catName string
}

func (s *st) diag(code string, line int, format string, args ...any) {
	if len(s.out.Diagnostics) < 200 {
		s.out.Diagnostics = append(s.out.Diagnostics, diag(code, line, format, args...))
	}
}

func (s *st) dep(d manifests.Dependency) {
	if len(s.out.Dependencies) < 50000 {
		s.out.Dependencies = append(s.out.Dependencies, d)
	}
}

// ---- string and notation helpers ----

// strExpr evaluates a string expression made of literals, identifiers and '+': templates stay raw.
func strExpr(ts []tok) (string, bool) {
	for len(ts) >= 2 && ts[0].k == tPunct && ts[0].s == "(" && ts[len(ts)-1].k == tPunct && ts[len(ts)-1].s == ")" {
		ts = ts[1 : len(ts)-1]
	}
	if len(ts) == 0 {
		return "", false
	}
	var b strings.Builder
	expectOperand := true
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		switch {
		case expectOperand && t.k == tStr:
			if t.templ {
				b.WriteString(t.s)
			} else {
				b.WriteString(strings.ReplaceAll(t.s, "$", "$$"))
			}
			expectOperand = false
		case expectOperand && t.k == tIdent:
			name := t.s
			for i+2 < len(ts) && ts[i+1].k == tPunct && (ts[i+1].s == "." || ts[i+1].s == "?.") && ts[i+2].k == tIdent {
				name += "." + ts[i+2].s
				i += 2
			}
			if i+1 < len(ts) && ts[i+1].k == tPunct && ts[i+1].s == "(" { // method call: cannot evaluate
				return "", false
			}
			name = strings.TrimSuffix(strings.TrimSuffix(name, ".get"), ".toString")
			b.WriteString("${" + name + "}")
			expectOperand = false
		case expectOperand && t.k == tNum:
			b.WriteString(t.s)
			expectOperand = false
		case !expectOperand && t.k == tPunct && t.s == "+":
			expectOperand = true
		default:
			return "", false
		}
	}
	return b.String(), !expectOperand
}

var notationExt = regexp.MustCompile(`@([A-Za-z0-9]+)$`)

// parseNotation splits "group:name[:version[:classifier]][@ext]".
func parseNotation(s string) (g, a, v, classifier, ext string, ok bool) {
	if m := notationExt.FindStringSubmatch(s); m != nil && !strings.Contains(m[1], "{") {
		ext = m[1]
		s = strings.TrimSuffix(s, "@"+m[1])
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 4 {
		return
	}
	g, a = parts[0], parts[1]
	if len(parts) > 2 {
		v = parts[2]
	}
	if len(parts) > 3 {
		classifier = parts[3]
	}
	if g == "" || a == "" {
		return
	}
	return g, a, v, classifier, ext, true
}

func qualifiers(classifier, ext string) string {
	var q []string
	if classifier != "" {
		q = append(q, "classifier="+manifests.CleanText(classifier, 100))
	}
	if ext != "" && ext != "jar" {
		q = append(q, "type="+manifests.CleanText(ext, 40))
	}
	return strings.Join(q, "&")
}

func clean(s string, n int) string { return manifests.CleanText(s, n) }

// confScope maps a Gradle configuration name to a scope; known=false means a name pattern decided.
func confScope(conf string) (manifests.Scope, bool) {
	switch conf {
	case "implementation", "api", "compile", "runtime", "runtimeOnly", "releaseImplementation", "releaseApi", "shadow", "embed", "implementationDependenciesMetadata":
		return manifests.ScopeRuntime, true
	case "compileOnly", "compileOnlyApi", "providedCompile", "providedRuntime":
		return manifests.ScopeProvided, true
	case "developmentOnly", "debugImplementation", "debugApi", "debugRuntimeOnly":
		return manifests.ScopeDev, true
	case "annotationProcessor", "kapt", "ksp", "classpath", "detektPlugins", "lintChecks", "lintPublish", "agent", "buildscript.classpath":
		return manifests.ScopeBuild, true
	case "testFixturesApi", "testFixturesImplementation", "testFixturesCompileOnly":
		return manifests.ScopeTest, true
	}
	l := strings.ToLower(conf)
	switch {
	case strings.HasPrefix(l, "test") || strings.HasPrefix(l, "androidtest") || strings.HasPrefix(l, "integrationtest") || strings.HasPrefix(l, "functionaltest") || strings.HasPrefix(l, "inttest") || strings.Contains(l, "test"):
		return manifests.ScopeTest, false
	case strings.HasSuffix(l, "annotationprocessor") || strings.HasPrefix(l, "kapt") || strings.HasPrefix(l, "ksp"):
		return manifests.ScopeBuild, false
	case strings.HasSuffix(l, "compileonly") || strings.HasSuffix(l, "compileonlyapi"):
		return manifests.ScopeProvided, false
	case strings.HasSuffix(l, "implementation") || strings.HasSuffix(l, "api") || strings.HasSuffix(l, "runtimeonly"):
		return manifests.ScopeRuntime, false
	}
	return manifests.ScopeRuntime, false
}

// isCatalogIgnoreRoot lists identifiers that look like a catalog accessor ("project.version") but are Gradle API.
func isCatalogIgnoreRoot(s string) bool {
	switch s {
	case "project", "rootProject", "gradle", "it", "this", "kotlin", "versions", "tasks", "ext", "extra", "properties", "System", "JavaVersion":
		return true
	}
	return false
}

// catalogRef recognises "libs.foo.bar[.get()|.asProvider()]" and returns catalog name and accessor path.
func catalogRef(ts []tok) (cat, path string, ok bool) {
	var segs []string
	i := 0
	for i < len(ts) {
		if ts[i].k != tIdent {
			return "", "", false
		}
		segs = append(segs, ts[i].s)
		i++
		if i < len(ts) && ts[i].k == tPunct && ts[i].s == "(" { // trailing call: drop the segment when it is get/asProvider
			last := segs[len(segs)-1]
			if last != "get" && last != "asProvider" && last != "asConstraint" && last != "toString" {
				return "", "", false
			}
			segs = segs[:len(segs)-1]
			depth := 0
			for i < len(ts) {
				if ts[i].k == tPunct && ts[i].s == "(" {
					depth++
				} else if ts[i].k == tPunct && ts[i].s == ")" {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
		}
		if i < len(ts) {
			if ts[i].k == tPunct && (ts[i].s == "." || ts[i].s == "?.") {
				i++
				continue
			}
			return "", "", false
		}
	}
	for len(segs) > 1 {
		if l := segs[len(segs)-1]; l == "get" || l == "asProvider" || l == "toString" {
			segs = segs[:len(segs)-1]
			continue
		}
		break
	}
	if len(segs) < 2 || isCatalogIgnoreRoot(segs[0]) {
		return "", "", false
	}
	return segs[0], strings.Join(segs[1:], "."), true
}

// ---- the dependency handler ----

// blockKind finds the innermost block that gives statements their meaning. direct reports that the statement sits
// directly in a dependencies or constraints block (not inside the closure of a dependency declaration).
func (s *st) blockKind(stack []string) (kind, cond string, bs bool, direct bool) {
	first := true
	for i := len(stack) - 1; i >= 0; i-- {
		l := stack[i]
		switch l {
		case "if", "else", "then", "afterEvaluate", "with", "apply", "run", "let", "also", "configureEach", "all", "whenObjectAdded":
			continue
		case "dependencies":
			kind = "deps"
		case "constraints":
			kind = "constraints"
		case "plugins", "imports", "ext", "extra", "versionCatalogs", "pluginManagement":
			kind = l
		default:
			if strings.HasPrefix(l, "catalog:") {
				kind = "catalog"
			} else {
				first = false
				continue
			}
		}
		cond, bs = s.condAndBuildscript(stack[:i])
		return kind, cond, bs, first
	}
	return "", "", false, false
}

func (s *st) condAndBuildscript(outer []string) (cond string, buildscript bool) {
	for _, l := range outer {
		switch {
		case l == "subprojects" || l == "allprojects":
			cond = l
		case strings.HasPrefix(l, "project:"):
			cond = l
		case l == "buildscript":
			buildscript = true
		}
	}
	return
}

// catalogDep is the placeholder for a version-catalog accessor; the assembler replaces it with the catalog entry (marker SourceRef "catalog").
func catalogDep(cat, p string, sc manifests.Scope, group, cond string, line int) manifests.Dependency {
	d := mkd(mvn(clean(cat, 100), clean(p, 200)), "", clean(cat+"."+p, 300), "", group, cond, sc, line)
	d.SetSource(manifests.SourceRegistry, "catalog")
	return d
}

func (s *st) mkDep(conf, g, a, v, classifier, ext string, line int, cond string) manifests.Dependency {
	sc, _ := confScope(conf)
	return mkd(mvn(clean(g, 200), clean(a, 200)), clean(v, 200), clean(g+":"+a, 400), qualifiers(classifier, ext), clean(conf, 100), cond, sc, line)
}

// depsFromArgs interprets the arguments of one dependency declaration.
func (s *st) depsFromArgs(conf string, args [][]tok, line int, cond string, depth int) {
	if depth > 4 {
		return
	}
	if conf == "add" && len(args) > 0 {
		if c, ok := strExpr(args[0]); ok {
			conf, args = c, args[1:]
		}
	}
	// named-argument form
	named := map[string]string{}
	for _, a := range args {
		if len(a) >= 3 && a[0].k == tIdent && a[1].k == tPunct && (a[1].s == ":" || a[1].s == "=") {
			if v, ok := strExpr(a[2:]); ok {
				named[a[0].s] = v
			} else if ref, ok2 := catalogRefStr(a[2:]); ok2 {
				named[a[0].s] = ref
			}
		}
	}
	if len(named) > 0 {
		if p, ok := named["path"]; ok {
			s.dep(s.projectDep(conf, p, line, cond))
			return
		}
		if g, ok := named["group"]; ok && named["name"] != "" {
			d := s.mkDep(conf, g, named["name"], named["version"], named["classifier"], named["ext"], line, cond)
			s.dep(d)
			return
		}
		if m, ok := named["module"]; ok {
			if g, a, v, c, e, ok := parseNotation(m); ok {
				if named["version"] != "" {
					v = named["version"]
				}
				s.dep(s.mkDep(conf, g, a, v, c, e, line, cond))
				return
			}
		}
	}
	for _, a := range args {
		if len(a) >= 2 && a[0].k == tIdent && a[1].k == tPunct && (a[1].s == ":" || a[1].s == "=") {
			continue // consumed above
		}
		s.depFromExpr(conf, a, line, cond, depth)
	}
}

func catalogRefStr(ts []tok) (string, bool) {
	c, p, ok := catalogRef(ts)
	if !ok {
		return "", false
	}
	return "${" + c + "." + p + "}", true
}

func (s *st) projectDep(conf, p string, line int, cond string) manifests.Dependency {
	sc, _ := confScope(conf)
	p = strings.TrimSpace(clean(p, 200)) // a blank path yields a nameless identity, which the runner drops (found by FuzzParsers: project(" "))
	d := mkd(mvnRaw("", p), "", p, "", clean(conf, 100), cond, sc, line)
	d.SetSource(manifests.SourceProject, p) // the assembler resolves the project path to a directory
	return d
}

// isWrapperFunc lists the functions that wrap a dependency notation ("platform(libs.bom)").
func isWrapperFunc(s string) bool {
	switch s {
	case "platform", "enforcedPlatform", "testFixtures", "module", "create", "dependencies.create", "project", "kotlin", "files", "fileTree", "gradleApi", "localGroovy", "gradleTestKit", "variantOf", "enforcedPlatformSupport":
		return true
	}
	return false
}

func (s *st) depFromExpr(conf string, a []tok, line int, cond string, depth int) {
	for len(a) >= 2 && a[0].k == tPunct && a[0].s == "(" && a[len(a)-1].k == tPunct && a[len(a)-1].s == ")" {
		a = a[1 : len(a)-1]
	}
	if len(a) == 0 {
		return
	}
	// function wrapper: name(args) [trailing]
	if a[0].k == tIdent && len(a) >= 3 {
		name, j := a[0].s, 1
		for j+1 < len(a) && a[j].k == tPunct && a[j].s == "." && a[j+1].k == tIdent {
			name += "." + a[j+1].s
			j += 2
		}
		if j < len(a) && a[j].k == tPunct && a[j].s == "(" && isWrapperFunc(name) {
			end, depthP := -1, 0
			for k := j; k < len(a); k++ {
				if a[k].k == tPunct && a[k].s == "(" {
					depthP++
				} else if a[k].k == tPunct && a[k].s == ")" {
					depthP--
					if depthP == 0 {
						end = k
						break
					}
				}
			}
			if end < 0 {
				return
			}
			inner := splitTop(a[j+1 : end])
			switch name {
			case "platform", "enforcedPlatform":
				s.depsFromArgs("platform:"+conf, inner, line, cond, depth+1)
				if n := len(s.out.Dependencies); n > 0 && s.out.Dependencies[n-1].Group == "platform:"+conf {
					s.out.Dependencies[n-1].Scope, _ = confScope(conf)
				}
			case "testFixtures", "variantOf", "module", "create", "dependencies.create":
				if name == "variantOf" && len(inner) > 0 {
					inner = inner[:1]
				}
				s.depsFromArgs(conf, inner, line, cond, depth+1)
			case "project":
				if len(inner) > 0 {
					if p, ok := strExpr(inner[0]); ok {
						s.dep(s.projectDep(conf, p, line, cond))
					} else if len(inner[0]) >= 3 {
						if p, ok := strExpr(inner[0][2:]); ok {
							s.dep(s.projectDep(conf, p, line, cond))
						}
					}
				}
			case "kotlin":
				if len(inner) > 0 {
					if m, ok := strExpr(inner[0]); ok {
						v := "${kotlinPluginVersion}"
						if len(inner) > 1 {
							if vv, ok := strExpr(inner[1]); ok {
								v = vv
							}
						}
						d := s.mkDep(conf, "org.jetbrains.kotlin", "kotlin-"+m, v, "", "", line, cond)
						s.dep(d)
					}
				}
			default: // files, fileTree, gradleApi, localGroovy, gradleTestKit
				if !s.local {
					s.local = true
					s.diag("gradle.local-dependency", line, "%s(...) has no coordinates", clean(name, 40))
				}
			}
			return
		}
	}
	if str, ok := strExpr(a); ok {
		if g, an, v, c, e, ok := parseNotation(str); ok {
			s.dep(s.mkDep(conf, g, an, v, c, e, line, cond))
			return
		}
		if strings.HasPrefix(str, "${") && strings.HasSuffix(str, "}") && strings.Count(str, "${") == 1 && !strings.ContainsAny(str[2:len(str)-1], ":/ ") {
			if cat, p, ok := catalogRef(a); ok {
				s.dep(catalogDep(cat, p, scopeOf(conf), clean(conf, 100), cond, line))
				return
			}
			// a bare variable: the assembler may know its value
			s.dep(mkd(mvnRaw("", clean(str, 200)), "", clean(str, 200), "", clean(conf, 100), cond, scopeOf(conf), line))
			return
		}
		s.diag("gradle.unresolved-notation", line, "notation %q", clean(str, 80))
		return
	}
	if cat, p, ok := catalogRef(a); ok {
		if cat == "projects" { // typesafe project accessor: projects.core.data == project(":core:data")
			d := s.projectDep(conf, "projects."+p, line, cond)
			s.dep(d)
			return
		}
		s.dep(catalogDep(cat, p, scopeOf(conf), clean(conf, 100), cond, line))
		return
	}
	s.diag("gradle.unresolved-notation", line, "dependency argument is not a literal")
}

func scopeOf(conf string) manifests.Scope { sc, _ := confScope(conf); return sc }

// ---- statement handlers shared by build scripts and settings ----

var javaVer = regexp.MustCompile(`^(?:VERSION_)?(\d+(?:[._]\d+)?)$`)

func javaVersionOf(ts []tok) string {
	for _, t := range ts {
		switch t.k {
		case tNum, tStr:
			if m := javaVer.FindStringSubmatch(strings.TrimSpace(t.s)); m != nil {
				return strings.ReplaceAll(m[1], "_", ".")
			}
		case tIdent:
			if strings.HasPrefix(t.s, "VERSION_") || strings.HasPrefix(t.s, "JVM_") {
				v := strings.TrimPrefix(strings.TrimPrefix(t.s, "VERSION_"), "JVM_")
				return strings.ReplaceAll(v, "_", ".")
			}
		}
	}
	return ""
}

func (s *st) pluginDecl(name string, args [][]tok, tail []tok, line int, cond string) {
	var id, ver string
	all := append(append([][]tok{}, args...), tail)
	flat := func() []tok {
		var f []tok
		for _, a := range all {
			f = append(f, a...)
		}
		return f
	}()
	switch name {
	case "id":
		if len(flat) > 0 {
			id, _ = strExpr(flat[:1])
		}
	case "kotlin":
		if len(flat) > 0 {
			if m, ok := strExpr(flat[:1]); ok {
				id = "org.jetbrains.kotlin." + m
			}
		}
	case "alias":
		var argToks []tok
		for _, a := range args {
			argToks = append(argToks, a...)
		}
		if cat, p, ok := catalogRef(argToks); ok {
			s.dep(catalogDep(cat, p, manifests.ScopeBuild, "plugin", cond, line))
		}
		return
	default:
		return
	}
	for i := 0; i+1 < len(flat); i++ {
		if flat[i].k == tIdent && flat[i].s == "version" {
			if v, ok := strExpr(flat[i+1 : i+2]); ok {
				ver = v
			}
			break
		}
	}
	if id == "" {
		return
	}
	setProp(s.out, "plugin:"+clean(id, 200), clean(ver, 100))
	if ver != "" && !strings.HasPrefix(id, "org.gradle.") {
		d := s.mkDep("plugin", id, id+".gradle.plugin", ver, "", "", line, cond)
		d.Scope, d.Group = manifests.ScopeBuild, "plugin"
		s.dep(d)
	}
}

func (s *st) handler() handler {
	return handler{
		enter: func(stack []string, name string, args [][]tok, line int) string {
			if name == "project" && len(args) > 0 {
				if p, ok := strExpr(args[0]); ok {
					return "project:" + clean(p, 200)
				}
			}
			return name
		},
		call: func(stack []string, name string, args [][]tok, tail []tok, line int) {
			kind, cond, bs, direct := s.blockKind(stack)
			switch kind {
			case "deps", "constraints":
				if !direct {
					return
				}
				switch name {
				case "exclude", "because", "version", "strictly", "require", "prefer", "reject", "capabilities", "isTransitive", "attributes", "module", "components", "modules", "replace", "replacedBy", "excludes":
					if name != "module" {
						return
					}
				}
				conf := name
				if bs && (name == "classpath") {
					conf = "buildscript.classpath"
				}
				before := len(s.out.Dependencies)
				s.depsFromArgs(conf, args, line, cond, 0)
				for i := before; i < len(s.out.Dependencies); i++ {
					if kind == "constraints" {
						s.out.Dependencies[i].Group = "constraint:" + s.out.Dependencies[i].Group
					}
					if bs {
						s.out.Dependencies[i].Scope = manifests.ScopeBuild
					}
				}
			case "plugins":
				s.pluginDecl(name, args, tail, line, cond)
			case "imports":
				if (name == "mavenBom" || name == "bom") && len(args) > 0 {
					if str, ok := strExpr(args[0]); ok {
						if g, a, v, _, _, ok := parseNotation(str); ok {
							d := s.mkDep("bom", g, a, v, "", "pom", line, cond)
							d.Scope, d.Group = manifests.ScopeUnknown, "bom"
							s.dep(d)
						}
					}
				}
			case "ext", "extra":
				if (name == "set" || strings.HasSuffix(name, ".set")) && len(args) == 2 {
					if k, ok := strExpr(args[0]); ok {
						if v, ok := strExpr(args[1]); ok {
							setProp(s.out, "var:"+clean(k, 100), clean(v, 300))
						}
					}
				}
			}
			switch name {
			case "apply":
				for _, a := range args {
					if len(a) >= 3 && a[0].k == tIdent && (a[1].s == ":" || a[1].s == "=") {
						switch a[0].s {
						case "plugin":
							if id, ok := strExpr(a[2:]); ok {
								setProp(s.out, "plugin:"+clean(id, 200), "")
							}
						case "from":
							if !s.apply {
								s.apply = true
								s.diag("gradle.apply-from", line, "apply from is not followed")
							}
						}
					}
				}
				if len(args) == 0 && len(tail) == 0 {
					return
				}
			case "jvmToolchain", "kotlin.jvmToolchain":
				if v := javaVersionOf(flatToks(args)); v != "" {
					setProp(s.out, "java", v)
				}
			case "include", "includeFlat", "includeBuild", "rootProject.name":
				// handled by the settings parser when it installs its own call hook
			}
		},
		assign: func(stack []string, lhs string, rhs []tok, line int) {
			kind, _, _, _ := s.blockKind(stack)
			switch lhs {
			case "group", "version", "project.group", "project.version":
				if v, ok := strExpr(rhs); ok {
					key := strings.TrimPrefix(lhs, "project.")
					for _, l := range stack {
						switch l {
						case "allprojects":
							key = "all:" + key
						case "subprojects":
							key = "sub:" + key
						}
					}
					setProp(s.out, key, clean(v, 200))
				}
				return
			case "sourceCompatibility", "targetCompatibility", "jvmTarget", "options.release", "release", "languageVersion", "jvmToolchain":
				if v := javaVersionOf(rhs); v != "" {
					setProp(s.out, "java", v)
				}
				return
			}
			for _, pfx := range []string{"rootProject.ext.", "project.ext.", "ext.", "extra["} {
				if strings.HasPrefix(lhs, pfx) {
					name := strings.TrimSuffix(strings.TrimPrefix(lhs, pfx), "]")
					if v, ok := strExpr(rhs); ok {
						setProp(s.out, "var:"+clean(name, 100), clean(v, 300))
					}
					return
				}
			}
			if kind == "ext" || kind == "extra" {
				if v, ok := strExpr(rhs); ok {
					setProp(s.out, "var:"+clean(lhs, 100), clean(v, 300))
				}
			}
		},
		decl: func(stack []string, name string, rhs []tok, line int) {
			if len(stack) > 1 { // only top-level and ext-level declarations are visible to dependency strings
				return
			}
			if v, ok := strExpr(rhs); ok {
				setProp(s.out, "var:"+clean(name, 100), clean(v, 300))
			}
		},
	}
}

func flatToks(args [][]tok) []tok {
	var f []tok
	for _, a := range args {
		f = append(f, a...)
	}
	return f
}

// ---- Parse ----

func (p scriptParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest}
	g := limits.NewGuard(ctx, lim)
	toks, err := lex(data, g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "gradle.syntax")
		return out, nil
	}
	s := &st{out: &out}
	if err := scan(toks, g, s.handler()); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "gradle.syntax")
		return out, nil
	}
	out.Toolchain = map[string]string{"tool": "gradle"} // the assembler adds "dsl": the parser does not know the file name
	if v := out.Properties["java"]; v != "" {
		out.Toolchain["java"] = v
	}
	if len(out.Dependencies) > 0 {
		s.diag("gradle.script-heuristic", 0, "%d dependencies read lexically from the build script", len(out.Dependencies))
	}
	return out, nil
}
