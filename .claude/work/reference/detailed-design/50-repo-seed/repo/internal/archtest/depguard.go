package archtest

import (
	"fmt"
	"sort"
	"strings"
)

// Markers delimit the generated depguard rules inside .golangci.yml. Everything between them is owned by GenerateDepguard.
const (
	DepguardBegin = "        # BEGIN GENERATED depguard rules"
	DepguardEnd   = "        # END GENERATED depguard rules"
)

func fileGlob(pkg string) string { return "**/" + pkg + "/*.go" }

// GenerateDepguard renders the depguard.rules mapping (indented for .golangci.yml, 8 spaces) from the table.
// depguard is the fast editor and pre-commit feedback, one rule per row; test helpers are therefore also permitted in
// non-test files of the rows that list them, which TestRealTree (closed world, test imports, rank proof) forbids. Two properties were verified against golangci-lint v2.14.0: `files: [glob, "!$test"]` combines, and
// `files: ["$all", "!glob"]` excludes the owners of an owned library.
func GenerateDepguard(module string, rules []Rule, owned []Owned) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("%s (internal/archtest/depguard.go; run `make arch-fix` after editing rules.go)", DepguardBegin)
	sorted := append([]Rule(nil), rules...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Pkg < sorted[j].Pkg })
	for _, r := range sorted {
		if r.Rank == RankTest || r.AllowAll {
			continue
		}
		name := strings.NewReplacer("/", "_", "*", "x").Replace(strings.TrimPrefix(r.Pkg, "internal/"))
		allow := append(append([]string(nil), r.Allow...), r.Test...)
		if len(r.Test) > 0 || strings.Contains(r.Pkg, "*") {
			allow = append(allow, r.Pkg+"/**")
		}
		w("        arch_%s:", name)
		w("          list-mode: lax")
		w("          files: [%q]", fileGlob(r.Pkg))
		w("          deny:")
		w("            - pkg: %s", module)
		w("              desc: %q", fmt.Sprintf("%s (rank %d) may import only the packages of its row in internal/archtest/rules.go", r.Pkg, r.Rank))
		w("          allow:")
		w("            - $gostd")
		w("            - %s/%s$", module, r.Pkg)
		sort.Strings(allow)
		for _, a := range uniq(allow) {
			if before, _, found := strings.Cut(a, "*"); found {
				// A '*' row means "this subtree": depguard allow entries without '$' are prefixes, which is what editor
				// feedback needs (a local run showed that glob entries did not match a segment wildcard). TestRealTree stays exact.
				w("            - %s/%s", module, strings.TrimSuffix(before, "/"))
			} else {
				w("            - %s/%s$", module, a)
			}
		}
	}
	for i, o := range owned {
		w("        owned_%02d:", i+1)
		w("          list-mode: lax")
		files := []string{"$all"}
		for _, owner := range o.Owners {
			files = append(files, "!"+fileGlob(owner))
		}
		if o.TestOK {
			files = append(files, "!$test")
		}
		qs := make([]string, len(files))
		for i, f := range files {
			qs[i] = fmt.Sprintf("%q", f)
		}
		w("          files: [%s]", strings.Join(qs, ", "))
		w("          deny:")
		pkg := o.Prefix
		if o.Exact {
			pkg += "$"
		}
		w("            - pkg: %s", pkg)
		w("              desc: %q", o.Why)
	}
	b.WriteString(DepguardEnd) // no trailing newline: Splice keeps the one that follows the marker
	return b.String()
}

func uniq(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Splice replaces the generated block of a .golangci.yml; ok is false when the markers are missing.
func Splice(doc, block string) (out string, ok bool) {
	i, j := strings.Index(doc, DepguardBegin), strings.Index(doc, DepguardEnd)
	if i < 0 || j < i {
		return doc, false
	}
	return doc[:i] + block + doc[j+len(DepguardEnd):], true
}
