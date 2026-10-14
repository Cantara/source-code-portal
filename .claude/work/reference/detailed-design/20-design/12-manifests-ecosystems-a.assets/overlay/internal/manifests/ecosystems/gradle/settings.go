package gradle

import (
	"context"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

func (p settingsParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, fin := p.admit(f, lim)
	if fin != nil {
		return *fin, nil
	}
	out := manifests.Parsed{Kind: manifests.KindWorkspace}
	g := limits.NewGuard(ctx, lim)
	toks, err := lex(data, g)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "gradle.syntax")
		return out, nil
	}
	s := &st{out: &out, settings: true}
	h := s.handler()
	baseCall, baseAssign, baseEnter := h.call, h.assign, h.enter
	seen := map[string]bool{}
	var catalog string // current versionCatalogs create("name") block
	h.enter = func(stack []string, name string, args [][]tok, line int) string {
		if name == "create" || name == "register" {
			if len(stack) > 0 && stack[len(stack)-1] == "versionCatalogs" && len(args) > 0 {
				if n, ok := strExpr(args[0]); ok {
					catalog = clean(n, 60)
					return "catalog:" + catalog
				}
			}
		}
		return baseEnter(stack, name, args, line)
	}
	h.assign = func(stack []string, lhs string, rhs []tok, line int) {
		switch lhs {
		case "rootProject.name":
			if v, ok := strExpr(rhs); ok {
				setProp(&out, "rootProject.name", clean(v, 100))
			}
			return
		}
		if strings.HasPrefix(lhs, "project(") || strings.Contains(lhs, "projectDir") {
			if !seen["projectDir"] {
				seen["projectDir"] = true
				s.diag("gradle.project-dir-override", line, "projectDir is assigned")
			}
			return
		}
		baseAssign(stack, lhs, rhs, line)
	}
	h.call = func(stack []string, name string, args [][]tok, tail []tok, line int) {
		inCatalog := len(stack) > 0 && strings.HasPrefix(stack[len(stack)-1], "catalog:")
		switch name {
		case "include", "includeFlat":
			for _, a := range args {
				if v, ok := strExpr(a); ok {
					if !strings.Contains(v, "${") {
						path := v
						if name == "includeFlat" {
							path = "..flat/" + strings.TrimPrefix(v, ":")
						} else if !strings.HasPrefix(path, ":") {
							path = ":" + path
						}
						if _, ok := repopath.Rel(strings.ReplaceAll(strings.Trim(strings.TrimPrefix(path, "..flat/"), ":"), ":", "/")); !ok {
							s.diag("gradle.include-invalid", line, "include %q is not a project path", clean(v, 60))
							continue
						}
						out.Members = append(out.Members, clean(path, 300))
						continue
					}
				}
				if !seen["dyn"] {
					seen["dyn"] = true
					s.diag("gradle.settings-dynamic", line, "include with a computed argument")
				}
			}
			return
		case "includeBuild":
			if len(args) > 0 {
				if v, ok := strExpr(args[0]); ok && !strings.Contains(v, "${") {
					if strings.Contains(v, "://") || strings.ContainsAny(v, "@\\") {
						s.diag("gradle.include-build", line, "included build is not a local path and is ignored")
					} else {
						setProp(&out, "includeBuild:"+clean(v, 300), "")
						s.diag("gradle.include-build", line, "included build %s", clean(v, 100))
					}
				}
			}
			return
		case "from":
			if inCatalog && len(args) > 0 {
				a := args[0]
				// from(files("gradle/x.toml"))
				if len(a) >= 3 && a[0].k == tIdent && a[0].s == "files" {
					if v, ok := strExpr(splitTopFirst(a[2 : len(a)-1])); ok {
						setProp(&out, "catalog-file:"+catalog, clean(v, 300))
					}
				} else if v, ok := strExpr(a); ok {
					setProp(&out, "catalog-remote:"+catalog, clean(v, 300))
				}
			}
			return
		case "library", "version", "plugin", "bundle":
			if inCatalog {
				inlineCatalog(s, catalog, name, args, tail, line)
				return
			}
		}
		baseCall(stack, name, args, tail, line)
	}
	if err := scan(toks, g, h); err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		fail(&out, err, "gradle.syntax")
		return out, nil
	}
	if len(out.Dependencies) > 0 {
		s.diag("gradle.script-heuristic", 0, "%d plugin declarations read lexically from settings", len(out.Dependencies))
	}
	out.Toolchain = map[string]string{"tool": "gradle"}
	return out, nil
}

func splitTopFirst(ts []tok) []tok {
	if a := splitTop(ts); len(a) > 0 {
		return a[0]
	}
	return nil
}

// inlineCatalog records version("x","1"), library("alias","g","a").version("1") and bundle("b", listOf(...)) of a settings catalog.
func inlineCatalog(s *st, cat, name string, args [][]tok, tail []tok, line int) {
	str := func(i int) string {
		if i < len(args) {
			v, _ := strExpr(args[i])
			return v
		}
		return ""
	}
	pfx := "cat:" + cat + ":"
	switch name {
	case "version":
		if k, v := str(0), str(1); k != "" && v != "" {
			setProp(s.out, pfx+"version:"+clean(k, 100), clean(v, 200))
		}
	case "library":
		alias, g, a := str(0), str(1), str(2)
		if alias == "" {
			return
		}
		if g == "" || a == "" { // library("alias", "g:a:v")
			if gg, aa, vv, _, _, ok := parseNotation(g + a); ok {
				g, a = gg, aa
				_ = vv
			}
		}
		d := mkd(mvn(clean(g, 200), clean(a, 200)), "", clean(alias, 100), "", "catalog-lib", "", manifests.ScopeUnknown, line)
		// trailing .version("1") / .versionRef("r")
		flat := flatToks(args)
		_ = flat
		for i := 0; i+3 < len(tail); i++ {
			if tail[i].k == tIdent && (tail[i].s == "version" || tail[i].s == "versionRef") && tail[i+1].s == "(" {
				if v, ok := strExpr(tail[i+2 : i+3]); ok {
					if tail[i].s == "versionRef" {
						d.SetSource(manifests.SourceRegistry, "ref:"+clean(v, 100))
					} else {
						d.Requirement = clean(v, 200)
					}
				}
			}
		}
		d.Group = "catalog:" + cat + ":lib"
		s.dep(d)
	}
}
