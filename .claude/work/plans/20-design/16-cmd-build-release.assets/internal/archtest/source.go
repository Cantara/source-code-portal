package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Source rules that import analysis cannot see and forbidigo cannot express (it matches identifiers, not literals,
// and cannot read a message argument): ad-hoc server URLs (GR-15), skipped tests without an issue (GR-43) and
// unowned TODOs (GR-05). They run over the AST of every hand-written .go file of the module.
//
//   - routeLiteral: a string literal that starts with a server route prefix belongs to internal/routes (builders) or to
//     generated code; everywhere else it is formatted by routes.Repo, routes.Badge, ... instead.
//   - skipNeedsIssue: t.Skip, t.Skipf and t.SkipNow need an issue reference (#123 or /issues/123) in the message.
//   - todoNeedsIssue: a marker comment (the four keywords listed in todoRE) needs an issue reference `(#123)` right after the keyword.
func routePrefixRE() *regexp.Regexp {
	return regexp.MustCompile(`^/(api|assets|badges|commits|csp-reports|dashboard|debug|dependencies|favicon|groups|livez|llms|llms-full|manifest|media|metrics|packages|pages|readyz|repos|robots|schemas|sitemap|status|theme-init|webhooks|_frame)(/|\.|$)`)
}

// RouteLiteral reports whether s starts with the first path segment of a route of the portal's two route tables (the public one and the
// admin one). The list is the set of first literal segments of those tables;
// TestRouteLiteralRuleCoversEveryRoute in cmd/scp fails when a route is added whose first segment is not in it.
func RouteLiteral(s string) bool { return routePrefixRE().MatchString(s) }

// routeLiteralExempt lists module-relative path prefixes whose files may hold route-looking literals: the route builders themselves,
// the OpenAPI and SPA packages, and the code that speaks to UPSTREAM servers, whose paths are not the portal's (GitHub's REST
// "/repos/{o}/{r}/commits" and "/api/v3", Jenkins' "/api/json", Snyk's "/api/..." and the fake GitHub that serves the same paths).
func routeLiteralExempt() []string {
	return []string{"internal/routes/", "internal/apispec/", "internal/webapp/", "internal/archtest/", "web/",
		"internal/github/", "internal/gitsource/", "internal/integrations/", "cmd/fakegithub/"}
}

var (
	todoRE      = regexp.MustCompile(`\b(TODO|FIXME|XXX|HACK)\b(\(#\d+\))?`)
	skipIssueRE = regexp.MustCompile(`#\d+|/issues/\d+`)
)

// ScanSource walks dir (the module root) and applies the three rules.
func ScanSource(dir string) ([]Violation, error) {
	var vs []Violation
	fset := token.NewFileSet()
	routeRE := routePrefixRE()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".gen.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if ast.IsGenerated(f) { // the toolchain's own definition: "// Code generated ... DO NOT EDIT." anywhere before the package clause
			return nil
		}
		isTest := strings.HasSuffix(rel, "_test.go")
		pos := func(n ast.Node) string { return rel + ":" + strconv.Itoa(fset.Position(n.Pos()).Line) }
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if m := todoRE.FindStringSubmatch(c.Text); m != nil && m[2] == "" {
					vs = append(vs, Violation{rel, "todo", pos(c) + ": " + m[1] + " needs an issue reference, e.g. " + m[1] + "(#123)"})
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING && !isTest && !exempt(rel) {
					if s, err := strconv.Unquote(x.Value); err == nil && routeRE.MatchString(s) {
						vs = append(vs, Violation{rel, "route-literal", pos(x) + ": " + x.Value + " is a server URL: use the internal/routes builders"})
					}
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || !isTest {
					return true
				}
				switch sel.Sel.Name {
				case "SkipNow":
					vs = append(vs, Violation{rel, "skip", pos(x) + ": SkipNow carries no issue reference; use t.Skip with #123"})
				case "Skip", "Skipf":
					if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "t" || id.Name == "b" || id.Name == "f" || id.Name == "tb") {
						if !skipMentionsIssue(x) {
							vs = append(vs, Violation{rel, "skip", pos(x) + ": t.Skip needs an issue reference (#123 or /issues/123) in its message"})
						}
					}
				}
			}
			return true
		})
		return nil
	})
	return vs, err
}

func exempt(rel string) bool {
	for _, p := range routeLiteralExempt() {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

func skipMentionsIssue(c *ast.CallExpr) bool {
	for _, a := range c.Args {
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING && skipIssueRE.MatchString(lit.Value) {
			return true
		}
	}
	return false
}
