package archtest

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Pkg is the subset of `go list -json` output the checks need. Paths are module-relative.
type Pkg struct {
	Path        string   // "internal/scm"
	Name        string   // package clause name
	Imports     []string // non-test imports, module-relative when internal, full path otherwise
	TestImports []string // imports of _test.go files of the package and of its external test package
	NoGoFiles   bool
}

// Violation is one failed rule; Check returns all of them (never stops at the first).
type Violation struct {
	Pkg, Code, Msg string
}

func (v Violation) String() string { return fmt.Sprintf("[%s] %s: %s", v.Code, v.Pkg, v.Msg) }

func match(pattern, p string) bool {
	ok, err := path.Match(pattern, p)
	return err == nil && ok
}

func matchAny(patterns []string, p string) bool {
	for _, pat := range patterns {
		if match(pat, p) {
			return true
		}
	}
	return false
}

// RuleFor returns the first rule whose Pkg equals or matches p.
func RuleFor(rules []Rule, p string) (Rule, bool) {
	for _, r := range rules {
		if r.Pkg == p {
			return r, true
		}
	}
	for _, r := range rules {
		if strings.Contains(r.Pkg, "*") && match(r.Pkg, p) {
			return r, true
		}
	}
	return Rule{}, false
}

// CheckTable verifies the table itself: the executable acyclicity proof (every allowed edge strictly lowers rank),
// duplicate rows, and that every Allow entry resolves to some row. It does not look at the tree.
func CheckTable(rules []Rule) []Violation {
	var vs []Violation
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.Pkg] {
			vs = append(vs, Violation{r.Pkg, "table.duplicate", "two rows for the same package"})
		}
		seen[r.Pkg] = true
	}
	for _, r := range rules {
		if r.Rank == RankTest || r.AllowAll {
			continue
		}
		for _, a := range r.Allow {
			targets := resolve(rules, a)
			if len(targets) == 0 {
				vs = append(vs, Violation{r.Pkg, "table.dangling", "allows " + a + " which has no row"})
			}
			for _, t := range targets {
				if t.Rank >= r.Rank && (!t.TestHelper || r.Pkg != "cmd/fakegithub") {
					vs = append(vs, Violation{r.Pkg, "table.rank", fmt.Sprintf("allows %s (rank %d) from rank %d: an edge must strictly lower the rank", t.Pkg, t.Rank, r.Rank)})
				}
			}
		}
		for _, a := range r.Test {
			if len(resolve(rules, a)) == 0 {
				vs = append(vs, Violation{r.Pkg, "table.dangling", "test-allows " + a + " which has no row"})
			}
		}
	}
	return vs
}

func resolve(rules []Rule, pattern string) []Rule {
	var out []Rule
	for _, r := range rules {
		if r.Pkg == pattern || (strings.Contains(pattern, "*") && (match(pattern, r.Pkg) || (strings.Contains(r.Pkg, "*") && globOverlap(pattern, r.Pkg)))) {
			out = append(out, r)
		}
	}
	return out
}

// globOverlap reports whether two '*' patterns can match a common path (segment-wise).
func globOverlap(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != "*" && bs[i] != "*" && as[i] != bs[i] {
			return false
		}
	}
	return true
}

// Check compares the package list of the module with the table. internal is the set of module-relative paths of
// all packages of the module (including the ones in pkgs); anything else is third-party or stdlib.
func Check(rules []Rule, owned []Owned, pkgs []Pkg) []Violation {
	vs := CheckTable(rules)
	internal := map[string]bool{}
	for _, p := range pkgs {
		internal[p.Path] = true
	}
	banned := map[string]bool{}
	for _, n := range BannedPackageNames() {
		banned[n] = true
	}
	// Tree versus table, both directions.
	matched := map[string]bool{}
	for _, p := range pkgs {
		r, ok := RuleFor(rules, p.Path)
		if !ok {
			vs = append(vs, Violation{p.Path, "tree.norule", "package has no rule: add a row to archtest.Rules (closed world)"})
			continue
		}
		matched[r.Pkg] = true
	}
	for _, r := range rules {
		if !matched[r.Pkg] && !r.Planned {
			vs = append(vs, Violation{r.Pkg, "tree.nopackage", "row names a package that does not exist (mark it Planned if it is for later)"})
		}
		if matched[r.Pkg] && r.Planned {
			vs = append(vs, Violation{r.Pkg, "tree.planned", "row is Planned but the package exists: drop the flag"})
		}
	}
	for _, p := range pkgs {
		r, ok := RuleFor(rules, p.Path)
		if !ok {
			continue
		}
		last := path.Base(p.Path)
		if banned[last] || banned[p.Name] {
			vs = append(vs, Violation{p.Path, "G1.name", "package name " + p.Name + " is banned"})
		}
		check := func(imports []string, test bool) {
			for _, imp := range imports {
				if !internal[imp] {
					vs = append(vs, checkOwned(owned, p.Path, imp, test)...)
					continue
				}
				if imp == p.Path {
					continue
				}
				if !edgeAllowed(rules, r, p.Path, imp, test) {
					kind := "non-test"
					if test {
						kind = "test"
					}
					vs = append(vs, Violation{p.Path, "import", fmt.Sprintf("%s import of %s is not in the allowed list of rule %s", kind, imp, r.Pkg)})
				}
			}
		}
		check(p.Imports, false)
		check(p.TestImports, true)
	}
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Pkg != vs[j].Pkg {
			return vs[i].Pkg < vs[j].Pkg
		}
		return vs[i].Msg < vs[j].Msg
	})
	return vs
}

func edgeAllowed(rules []Rule, from Rule, fromPath, to string, test bool) bool {
	target, ok := RuleFor(rules, to)
	if !ok {
		return false // the importee has no row: reported once as tree.norule, and the edge is not allowed either
	}
	if from.AllowAll {
		// "Every package" minus what Go itself forbids (a feature's nested internal/ packages), minus test helpers and
		// archtest outside _test.go files (found by TestTableMaterialises, which compiled the maximal graph).
		if !visibleFrom(fromPath, to) {
			return false
		}
		return !target.TestHelper && target.Rank != RankTest || test
	}
	if matchAny(from.Allow, to) {
		return true
	}
	if test {
		if matchAny(from.Test, to) {
			return true
		}
		// An external test package (x_test) and own subpackages are covered by `imp == p.Path`; a package's tests may
		// also import its own subtree.
		return strings.HasPrefix(to, fromPath+"/")
	}
	return false
}

func checkOwned(owned []Owned, from, imp string, test bool) []Violation {
	var vs []Violation
	for _, o := range owned {
		hit := imp == o.Prefix
		if !o.Exact && strings.HasPrefix(imp, o.Prefix+"/") {
			hit = true
		}
		if !hit {
			continue
		}
		if matchAny(o.Owners, from) || (test && o.TestOK) {
			continue
		}
		vs = append(vs, Violation{from, "owned", fmt.Sprintf("imports %s: %s", imp, o.Why)})
	}
	return vs
}

// visibleFrom applies Go's own internal-directory rule to the "every package" row: a package below some dir/internal/ is importable
// only from dir and its subtree ("internal/docs/internal/render" from internal/docs/..., "cmd/scp/internal/bridge" from cmd/scp). The top-level
// internal/ is the module's own and is visible to the whole module.
func visibleFrom(from, to string) bool {
	parent, _, ok := strings.CutLast(to, "/internal/")
	if !ok {
		return true
	}
	return from == parent || strings.HasPrefix(from, parent+"/")
}
