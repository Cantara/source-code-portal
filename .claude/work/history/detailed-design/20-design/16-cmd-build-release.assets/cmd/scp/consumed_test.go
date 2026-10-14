package main

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestEveryConfigKeyIsConsumed fails for a leaf field of Config that no non-test, non-Validate code reads (GR-12).
func TestEveryConfigKeyIsConsumed(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps, Tests: false}, "github.com/cantara/source-code-portal/...")
	if err != nil {
		t.Fatal(err)
	}
	var root types.Object
	for _, p := range pkgs {
		if p.Name == "main" && p.Types != nil {
			root = p.Types.Scope().Lookup("Config")
		}
	}
	if root == nil {
		t.Fatal("Config not found")
	}
	leaves := map[*types.Var]string{}
	var walk func(t types.Type, path string, seen map[types.Type]bool)
	walk = func(ty types.Type, path string, seen map[types.Type]bool) {
		switch x := ty.(type) {
		case *types.Named:
			if seen[x] || x.Obj().Pkg() == nil || !strings.HasPrefix(x.Obj().Pkg().Path(), "github.com/cantara/source-code-portal/") {
				return // time.Duration, secret.Secret, stdlib: leaves
			}
			seen[x] = true
			walk(x.Underlying(), path, seen)
			delete(seen, x)
		case *types.Struct:
			for f := range x.Fields() {
				if !f.Exported() {
					continue
				}
				ft := f.Type()
				if isContainer(ft) {
					walk(ft, path+"."+f.Name(), seen)
				} else {
					leaves[f] = path + "." + f.Name()
				}
			}
		case *types.Slice:
			walk(x.Elem(), path+"[]", seen)
		case *types.Pointer:
			walk(x.Elem(), path, seen)
		case *types.Map:
			walk(x.Elem(), path+"[*]", seen)
		}
	}
	walk(root.Type(), "Config", map[types.Type]bool{})
	consumed := map[*types.Var]bool{}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			name := p.Fset.Position(f.Pos()).Filename
			if strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".gen.go") {
				continue
			}
			for _, decl := range f.Decls {
				fd, isFn := decl.(*ast.FuncDecl)
				if isFn && (fd.Name.Name == "Validate") {
					continue // a key read only for validation is not consumed
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						if s := p.TypesInfo.Selections[sel]; s != nil {
							if v, ok := s.Obj().(*types.Var); ok {
								consumed[v] = true
							}
						}
					}
					return true
				})
			}
		}
	}
	var dead []string
	for v, path := range leaves {
		if !consumed[v] {
			dead = append(dead, path)
		}
	}
	sort.Strings(dead)
	t.Logf("unconsumed: %v", dead)
	// allowUnconsumed lists keys that are legitimately not read by Go code. Every entry needs a reason.
	allow := map[string]string{
		"Config.Version": "checked by the loader against the schema enum",
		// The prototype composition does not include these consumers; in the real tree each has one.
		"Config.RateLimit.SSEPerIP":           "live (SSE) unit",
		"Config.Catalog.Ungrouped":            "stand-in for catalog.Config, consumed by catalog.New (the union module checks the real type)",
		"Config.Catalog.Groups[].ID":          "stand-in for catalog.Config",
		"Config.Catalog.Groups[].DisplayName": "stand-in for catalog.Config",
		"Config.Shutdown.StoreTimeout":        "store stage in cmd/scp",
		"Config.Telemetry.OTLP.HeadersFile":   "cmd/scp reads the file and passes telemetry.Options.Headers",
	}
	for _, d := range dead {
		if _, ok := allow[d]; !ok {
			t.Errorf("config key %s is documented but no code reads it (GR-12)", d)
		}
	}
}

func isContainer(t types.Type) bool {
	switch x := t.(type) {
	case *types.Named:
		if p := x.Obj().Pkg(); p == nil || !strings.HasPrefix(p.Path(), "github.com/cantara/source-code-portal/") {
			return false
		}
		if x.Obj().Pkg().Path() == "github.com/cantara/source-code-portal/internal/platform/secret" {
			return false
		}
		return isContainer(x.Underlying())
	case *types.Struct, *types.Slice, *types.Map:
		return true
	case *types.Pointer:
		return isContainer(x.Elem())
	}
	return false
}

// TestDefaultsHaveNoForeignHosts: the embedded default points at no company (GR-14). Only vendor API hosts the product
// is built for may appear, and each needs a reason here.
func TestDefaultsHaveNoForeignHosts(t *testing.T) {
	allowed := map[string]string{} // e.g. "https://api.snyk.io": "documented regional default of the Snyk REST API (14 section 6.1)"
	for i, line := range strings.Split(string(defaultYAML), "\n") {
		if idx := strings.Index(line, "://"); idx >= 0 {
			start := strings.LastIndexAny(line[:idx], " \":'") + 1
			end := idx + 3 + strings.IndexAny(line[idx+3:]+"\" ", "\" ")
			url := line[start:end]
			if _, ok := allowed[url]; !ok {
				t.Errorf("default.yaml:%d names %q; defaults must not point at a company (GR-14)", i+1, url)
			}
		}
	}
}
