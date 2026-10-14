package archtest_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/archtest"
)

// instance turns a pattern row into one concrete package path.
func instance(p string) string {
	switch p {
	case "internal/manifests/internal/*":
		return "internal/manifests/internal/purl"
	case "internal/manifests/ecosystems/*":
		return "internal/manifests/ecosystems/golang"
	case "internal/manifests/providers/*":
		return "internal/manifests/providers/githubgraph"
	}
	return p
}

// writeMaximalGraph writes, under a fresh module, one package per non-planned row. Every package imports everything its row
// allows, so the result is the MAXIMAL legal import graph. If the Go compiler accepts it, the table has no import cycle
// (the executable form of architecture 5.4); archtest.Check must then report nothing.
func writeMaximalGraph(t *testing.T, dir string, rules []archtest.Rule) int {
	t.Helper()
	exists := map[string]archtest.Rule{}
	for _, r := range rules {
		if !r.Planned {
			exists[instance(r.Pkg)] = r
		}
	}
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module github.com/cantara/source-code-portal\n\ngo 1.27\n")
	for p, r := range exists {
		if p == "internal/archtest" {
			write(p+"/x.go", "package archtest\n")
			continue
		}
		name := filepath.Base(p)
		if strings.HasPrefix(p, "cmd/") && !strings.HasSuffix(p, "bridge") {
			name = "main"
		}
		var imps []string
		if r.AllowAll {
			for q, qr := range exists {
				if q != p && !strings.HasPrefix(q, "cmd/") || q == "cmd/scp/internal/bridge" {
					if !qr.TestHelper && q != "internal/archtest" && !strings.Contains(strings.TrimPrefix(q, "internal/"), "/internal/") {
						imps = append(imps, q)
					}
				}
			}
		}
		for _, a := range r.Allow {
			if _, ok := exists[instance(a)]; ok {
				imps = append(imps, instance(a))
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "package %s\n\n", name)
		for _, q := range imps {
			fmt.Fprintf(&b, "import _ %q\n", "github.com/cantara/source-code-portal/"+q)
		}
		if name == "main" {
			b.WriteString("\nfunc main() {}\n")
		}
		write(p+"/x.go", b.String())
		if len(r.Test) > 0 {
			var tb strings.Builder
			fmt.Fprintf(&tb, "package %s_test\n\n", name) // black-box: parsertest imports manifests, so an in-package test would be an import cycle
			for _, a := range r.Test {
				if _, ok := exists[instance(a)]; ok {
					fmt.Fprintf(&tb, "import _ %q\n", "github.com/cantara/source-code-portal/"+instance(a))
				}
			}
			write(p+"/x_test.go", tb.String())
		}
	}
	return len(exists)
}

func TestTableMaterialises(t *testing.T) {
	dir := t.TempDir()
	if keep := os.Getenv("ARCHTEST_KEEP"); keep != "" { // debugging aid: leave the generated tree for inspection
		dir = keep
	}
	want := writeMaximalGraph(t, dir, archtest.Rules())
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	_, pkgs, err := archtest.Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range archtest.Check(archtest.Rules(), archtest.OwnedImports(), pkgs) {
		t.Error(v)
	}
	// Exactly one package per non-planned row. (A fixed floor such as 60 would fail the first commits of the repository, where most rows
	// are still Planned (design 16 section 4.12), and would pass a table that silently lost rows later.)
	if len(pkgs) != want {
		t.Fatalf("%d packages written, %d found by go list", want, len(pkgs))
	}
	if want < 2 {
		t.Fatalf("the table has %d non-planned rows: the proof would prove nothing", want)
	}
}

func TestCheckCatchesViolations(t *testing.T) {
	rules := []archtest.Rule{
		{Pkg: "internal/scm"},
		{Pkg: "internal/commits", Rank: 4, Allow: []string{"internal/scm"}},
		{Pkg: "internal/docs", Rank: 4, Allow: []string{"internal/scm"}},
		{Pkg: "internal/manifests/ecosystems/*", Rank: 5, Allow: []string{"internal/scm"}},
		{Pkg: "internal/ghost", Rank: 1},
	}
	owned := []archtest.Owned{
		{Prefix: "modernc.org/sqlite", Owners: []string{"internal/store/sqlite"}, Why: "G6"},
		{Prefix: "log", Exact: true, Why: "G2"},
		{Prefix: "go.opentelemetry.io/otel", Exact: true, Owners: []string{"cmd/scp"}, Why: "G11"},
	}
	pkgs := []archtest.Pkg{
		{Path: "internal/scm", Name: "scm"},
		{Path: "internal/commits", Name: "commits", Imports: []string{"internal/docs", "internal/scm", "log/slog", "log", "modernc.org/sqlite", "go.opentelemetry.io/otel", "go.opentelemetry.io/otel/metric"}},
		{Path: "internal/docs", Name: "docs", TestImports: []string{"internal/commits"}},
		{Path: "internal/manifests/ecosystems/golang", Name: "golang"},
		{Path: "internal/manifests/ecosystems/npm", Name: "npm"},
		{Path: "internal/util", Name: "util"},
		{Path: "internal/orphan", Name: "orphan"},
	}
	got := map[string]bool{}
	for _, v := range archtest.Check(rules, owned, pkgs) {
		got[v.Code+" "+v.Pkg] = true
	}
	for _, want := range []string{
		"import internal/commits",       // commits -> docs: feature importing a feature
		"import internal/docs",          // test import of commits from docs
		"owned internal/commits",        // log and modernc.org/sqlite (log/slog stays legal)
		"tree.norule internal/orphan",   // closed world
		"tree.norule internal/util",     // closed world (and G1)
		"tree.nopackage internal/ghost", // row without a package
	} {
		if !got[want] {
			t.Errorf("missing violation %q; got %v", want, got)
		}
	}
	if n := countOwned(archtest.Check(rules, owned, pkgs), "internal/commits"); n != 3 {
		t.Errorf("internal/commits: want 3 owned violations (log, sqlite, root otel; otel/metric and log/slog are legal), got %d", n)
	}
	if got["owned internal/docs"] {
		t.Error("log/slog must not be reported")
	}
}

func countOwned(vs []archtest.Violation, pkg string) (n int) {
	for _, v := range vs {
		if v.Code == "owned" && v.Pkg == pkg {
			n++
		}
	}
	return n
}
