package docs

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		g, p string
		ok   bool
	}{
		{"**/*.md", "a.md", true}, {"**/*.md", "a/b/c.md", true}, {"**/*.md", "a/b/c.txt", false},
		{"internal/**", "internal/x/y.md", true}, {"internal/**", "internal", true}, {"internal/**", "other/x", false},
		{"*.md", "a/b.md", false}, {"a?c.md", "abc.md", true}, {"a?c.md", "ac.md", false},
		{"**/draft-*", "x/y/draft-1.md", true}, {"a/**/b", "a/b", true}, {"a/**/b", "a/x/y/b", true}, {"a/**/b", "a/x/y/c", false},
		{"**", "", true}, {"*", "", true}, {"**/**/x", "x", true},
	} {
		if got := Glob(c.g, c.p); got != c.ok {
			t.Errorf("Glob(%q, %q) = %v, want %v", c.g, c.p, got, c.ok)
		}
	}
}

func TestGlobNoBlowup(t *testing.T) {
	p := "**/a/**/a/**/a/**/a/**/a/**/b"
	s := "a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/c"
	// segment recursion is polynomial; the 200-candidate and 300-byte path caps bound it. The test pins "does not hang".
	_ = Glob(p, s)
}

func TestDocID(t *testing.T) {
	for _, c := range []struct{ path, id string }{
		{"docs/install.md", "docs~install.md"},
		{"docs/we~ird.md", "docs~we%7Eird.md"},
		{"docs/Release Notes.md", "docs~Release%20Notes.md"},
		{"docs/100%.md", "docs~100%25.md"},
		{"docs/\u00c5.md", "docs~%C3%85.md"},
	} {
		id, ok := DocIDOf(c.path)
		if !ok || string(id) != c.id || id.PathOf() != c.path {
			t.Errorf("DocIDOf(%q) = %q %v (PathOf %q), want %q", c.path, id, ok, id.PathOf(), c.id)
		}
		if !docIDRE.MatchString(string(id)) {
			t.Errorf("%q does not match the contract DocId pattern", id)
		}
	}
	if _, ok := DocIDOf(""); ok {
		t.Fatal("empty path")
	}
	if DocID("docs~a%2").PathOf() != "" || DocID("docs~a%zz").PathOf() != "" {
		t.Fatal("malformed ids must not decode")
	}
	// injective: '~' and '/' never collide
	a, _ := DocIDOf("a/b.md")
	b, _ := DocIDOf("a~b.md")
	if a == b {
		t.Fatal("collision")
	}
	if KeyOf("docs/Install.MD") != "docs/Install" || KeyOf("README.adoc") != "README" || KeyOf("x.txt") != "x.txt" || KeyOf("x.mar\u212adown") != "x.mar\u212adown" {
		t.Fatal("KeyOf")
	}
	if FormatOf("a.MARKDOWN") != FormatMarkdown || FormatOf("a.AsciiDoc") != FormatAsciiDoc || FormatOf("README") != FormatText || FormatOf("a.mar\u212adown") != FormatText {
		t.Fatal("FormatOf")
	}
}

// docIDRE is the DocId pattern of the OpenAPI contract (api/common.yaml).
var docIDRE = regexp.MustCompile(`^([A-Za-z0-9._~-]|%[0-9A-F]{2})+$`)

func tree(paths ...string) func(func(scm.TreeEntry) bool) {
	return func(y func(scm.TreeEntry) bool) {
		for i, p := range paths {
			if !y(scm.TreeEntry{Path: p, SHA: "sha" + string(rune('a'+i)), Type: scm.EntryBlob, Size: 10}) {
				return
			}
		}
	}
}

func TestDiscover(t *testing.T) {
	cs, ps := Discover(tree("README.md", "AGENTS.md", "CHANGELOG.md", "docs/a.md", "docs/b.markdown", "docs/b.md", "docs/internal/x.md",
		"docs/img.png", "docs/c~d.md", "src/main.go", "other/e.md", "docs/Guide.ADOC"), "docs", nil, []string{"internal/**"})
	var got []string
	for _, c := range cs {
		got = append(got, c.Path)
	}
	// the default include is **/*.md: b.markdown and Guide.ADOC need an explicit include; a '~' in a name is legal
	want := []string{"AGENTS.md", "CHANGELOG.md", "docs/a.md", "docs/b.md", "docs/c~d.md"}
	if !reflect.DeepEqual(got, want) || len(ps) != 0 {
		t.Fatalf("got %v problems %v, want %v", got, ps, want)
	}
	cs2, ps2 := Discover(tree("docs/b.markdown", "docs/b.md", "docs/Guide.ADOC"), "docs", []string{"**/*"}, nil)
	if len(cs2) != 2 || len(ps2) != 1 || ps2[0].Code != ProblemRouteCollision {
		t.Fatal(cs2, ps2)
	}
	for _, c := range cs {
		if c.Path == "README.md" || c.Path == "other/e.md" || c.Path == "docs/internal/x.md" {
			t.Fatalf("unexpected candidate %s", c.Path)
		}
	}
}

func TestOrder(t *testing.T) {
	cands := []Candidate{{Path: "docs/a.md"}, {Path: "docs/b.md"}, {Path: "docs/c.md"}, {Path: "docs/d.md"}}
	nav := []NavItem{{Path: "docs/c.md", Title: "See"}, {Title: "Guides", Items: []NavItem{{Path: "d.md"}, {Path: "missing.md"}}}}
	out, ps := Order(cands, nav, map[string]int{"docs/b.md": 1}, "docs")
	var got []string
	for _, o := range out {
		got = append(got, o.Path)
	}
	if !reflect.DeepEqual(got, []string{"docs/c.md", "docs/d.md", "docs/b.md", "docs/a.md"}) {
		t.Fatal(got)
	}
	if len(ps) != 1 || ps[0].Code != ProblemNavMissing || !reflect.DeepEqual(out[1].Section, []string{"Guides"}) {
		t.Fatal(ps, out[1].Section)
	}
}

func TestAdmit(t *testing.T) {
	order := []string{"readme", "a", "b", "c"}
	need := map[string]int64{"readme": 10, "a": 50, "b": 20, "c": 20}
	got := Admit(order, need, map[string]bool{"b": true}, 60, 1000, 10)
	// resident b first (20), then readme (10) -> 30, a (50) does not fit, c (20) -> 50
	if !got["b"] || !got["readme"] || got["a"] || !got["c"] {
		t.Fatal(got)
	}
	if g := Admit(order, need, nil, 1000, 35, 10); !g["readme"] || g["a"] || !g["b"] || g["c"] {
		t.Fatal("global budget", g)
	}
	if g := Admit(order, need, nil, 1000, 1000, 2); len(g) != 2 || !g["readme"] || !g["a"] {
		t.Fatal("count cap", g)
	}
}

func TestResolve(t *testing.T) {
	repo := scm.Repository{Ref: scm.RepoRef{Name: "svc"}, Description: "gh desc", Topics: []string{"go", "sso"}}
	ci := &CatalogInfo{Name: "svc-ci", Description: "ci desc", Tags: []string{"sso", "java"}, Owner: "group:default/team-a", Lifecycle: "production", Type: "service"}
	v := DescriptorView{Value: Descriptor{Title: "Title", Tags: []string{"sso"}}, CatalogInfo: ci, CodeOwners: []string{"@o/t"}}
	r := Resolve(v, repo, "Readme H1")
	if r.Title != (Sourced[string]{"Title", FromDescriptor}) || r.Summary != (Sourced[string]{"ci desc", FromCatalogInfo}) ||
		r.Lifecycle != (Sourced[string]{"production", FromCatalogInfo}) || r.Owners[0].Kind != "group" {
		t.Fatalf("%+v", r)
	}
	var tags []string
	for _, x := range r.Tags {
		tags = append(tags, x.Value+":"+string(x.Source))
	}
	if !reflect.DeepEqual(tags, []string{"go:github-topics", "java:catalog-info", "sso:descriptor"}) {
		t.Fatal(tags)
	}
	r2 := Resolve(DescriptorView{CodeOwners: []string{"@o/t"}}, repo, "Readme H1")
	if r2.Title != (Sourced[string]{"Readme H1", FromDerived}) || r2.Summary != (Sourced[string]{"gh desc", FromGitHub}) || r2.Owners[0].Source != FromCodeowners {
		t.Fatalf("%+v", r2)
	}
}

func TestCatalogInfoAndCodeOwners(t *testing.T) {
	ci, err := ParseCatalogInfo([]byte("apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: x\n  description: \"a\\u200bb\\nc\"\n  links:\n    - {url: 'http://no'}\n    - {url: 'https://yes', title: T}\nspec: {owner: team-a, lifecycle: production, type: service}\n"))
	if err != nil || ci.Description != "a b c" || len(ci.Links) != 1 || ci.Owner != "team-a" {
		t.Fatal(ci, err)
	}
	if _, err := ParseCatalogInfo([]byte("kind: Component\nx: &a 1\n")); err == nil {
		t.Fatal("anchor accepted")
	}
	o := ParseCodeOwners([]byte("# c\n*.go @a @b\n* @org/t1 bad @u # x\n/docs/ @d\n* @org/t2\n"))
	if !reflect.DeepEqual(o, []string{"@org/t2"}) {
		t.Fatal(o)
	}
}

// TestGlobAdversarial pins verification V-02: the recursive matcher was exponential in the number of `**` segments.
func TestGlobAdversarial(t *testing.T) {
	pat := strings.Repeat("**/a/", 38) + "b"
	p := strings.Repeat("a/", 31) + "a"
	st := time.Now()
	if Glob(pat, p) {
		t.Fatal("must not match")
	}
	if d := time.Since(st); d > 100*time.Millisecond {
		t.Fatalf("glob took %v", d)
	}
	if !Glob(strings.Repeat("**/a/", 38)+"a", strings.Repeat("a/", 60)+"a") {
		t.Fatal("must match")
	}
}

func TestDiscoverGlobsAcceptRepoRelativePatterns(t *testing.T) {
	cs, _ := Discover(tree("docs/a.md", "docs/internal/x.md", "docs/internal/deep/y.md"), "docs", nil, []string{"docs/internal/**"})
	if len(cs) != 1 || cs[0].Path != "docs/a.md" {
		t.Fatalf("%+v", cs)
	}
	cs, _ = Discover(tree("docs/a.md", "docs/internal/x.md"), "docs", nil, []string{"internal/**"})
	if len(cs) != 1 {
		t.Fatalf("%+v", cs)
	}
}
