package archtest_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/archtest"
)

// TestDocGoStatesTheRow keeps the prose honest: every package under internal/ (and the bridge) carries a doc.go whose package comment
// says "Allowed internal imports: ..." and that list equals the Allow list of the package's row in rules.go. The table stays the
// authority; this test makes a stale comment a failure instead of a lie. Rows with AllowAll (cmd/scp) and the archtest itself are exempt.
func TestDocGoStatesTheRow(t *testing.T) {
	_, pkgs, err := archtest.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	rules := archtest.Rules()
	listRE := regexp.MustCompile(`Allowed internal imports: (.*?)\.(?:\s+(?:Additional imports|Third-party libraries|Note:|Design:)|\s*$)`)
	for _, p := range pkgs {
		r, ok := archtest.RuleFor(rules, p.Path)
		if !ok || r.AllowAll || r.Rank == archtest.RankTest {
			continue
		}
		if !strings.HasPrefix(p.Path, "internal/") && p.Path != "cmd/scp/internal/bridge" {
			continue
		}
		file := filepath.Join("..", "..", filepath.FromSlash(p.Path), "doc.go")
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s: no doc.go (every package states its responsibility and allowed imports)", p.Path)
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments|parser.PackageClauseOnly)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if f.Doc == nil || !strings.HasPrefix(f.Doc.Text(), "Package "+p.Name+" ") {
			t.Errorf("%s: the package comment must start with %q", file, "Package "+p.Name+" ")
			continue
		}
		flat := strings.Join(strings.Fields(f.Doc.Text()), " ")
		m := listRE.FindStringSubmatch(flat)
		if m == nil {
			t.Errorf("%s: the package comment lacks an %q sentence", file, "Allowed internal imports: ...")
			continue
		}
		var got []string
		if m[1] != "none" {
			for s := range strings.SplitSeq(m[1], ", ") {
				got = append(got, strings.TrimSpace(s))
			}
		}
		want := append([]string(nil), r.Allow...)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: doc.go lists %v but the row %s allows %v", p.Path, got, r.Pkg, want)
		}
	}
}
