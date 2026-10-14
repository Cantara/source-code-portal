package purl

import "testing"

func TestNormalizeTable(t *testing.T) {
	cases := []struct{ eco, ns, name, wns, wname string }{
		{"pypi", "", "Django_Foo.Bar", "", "django-foo-bar"},
		{"pypi", "", "zope.interface", "", "zope-interface"},
		{"npm", "@Scope", "Name", "Scope", "Name"},
		{"golang", "GitHub.com/Gorilla", "Mux", "github.com/gorilla", "mux"},
		{"nuget", "", "Newtonsoft.Json", "", "newtonsoft.json"},
		{"cargo", "", "Serde_Json", "", "serde-json"},
		{"composer", "Laravel", "Framework", "laravel", "framework"},
		{"github", "Actions", "Checkout", "actions", "checkout"},
		{"pub", "", "Some_Pkg", "", "some_pkg"},
		{"maven", "Org.Apache", "Commons-Lang3", "Org.Apache", "Commons-Lang3"},
	}
	for _, c := range cases {
		ns, n := Normalize(c.eco, c.ns, c.name)
		if ns != c.wns || n != c.wname {
			t.Errorf("%s %q/%q -> %q/%q want %q/%q", c.eco, c.ns, c.name, ns, n, c.wns, c.wname)
		}
		ns2, n2 := Normalize(c.eco, ns, n)
		if ns2 != ns || n2 != n {
			t.Errorf("not idempotent: %s %q/%q", c.eco, ns, n)
		}
	}
}

func TestBuildParseRoundTrip(t *testing.T) {
	for _, c := range []struct{ eco, ns, name, ver, q string }{
		{"maven", "org.acme", "core", "1.0", "classifier=sources&type=jar"},
		{"npm", "scope", "name", "1.2.3", ""},
		{"pypi", "", "django-foo", "1.0", ""},
		{"golang", "github.com/org", "lib", "v1.2.3", ""},
		{"terraform", "hashicorp", "aws", "5.0.0", ""},
	} {
		s, err := Build(c.eco, c.ns, c.name, c.ver, c.q)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if p.Ecosystem != c.eco || p.Namespace != c.ns || p.Name != c.name || p.Version != c.ver || p.Qualifiers != c.q {
			t.Errorf("%s -> %+v", s, p)
		}
	}
	for _, bad := range []string{"", "maven:x", "pkg:", "pkg:unknowntype/x/y", "pkg:npm/", "pkg:maven/a/b%ZZ"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestRepoKey(t *testing.T) {
	cases := map[string]string{
		"https://github.com/Org/Repo.git":    "github.com/org/repo",
		"git@github.com:Org/Repo.git":        "github.com/org/repo",
		"git+https://user:pw@github.com/o/r": "github.com/o/r",
		"github:org/repo":                    "github.com/org/repo",
		"org/repo":                           "github.com/org/repo",
		"ssh://git@ghe.corp.example/o/r.git": "ghe.corp.example/o/r",
		"https://github.com/o/r/tree/main/x": "github.com/o/r",
	}
	for in, want := range cases {
		if got, ok := RepoKey(in); !ok || got != want {
			t.Errorf("%q -> %q,%v want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "https://github.com/onlyowner", "not a url", "https:///x/y"} {
		if _, ok := RepoKey(bad); ok {
			t.Errorf("%q must not be a repo", bad)
		}
	}
	if k, ok := RepoKeyFromModulePath("github.com/Org/lib/v2/sub", map[string]bool{"github.com": true}); !ok || k != "github.com/org/lib" {
		t.Errorf("module path: %q %v", k, ok)
	}
}

func FuzzNormalizeIdempotent(f *testing.F) {
	f.Add("pypi", "", "Django_Foo.Bar")
	f.Add("npm", "@a", "b")
	f.Fuzz(func(t *testing.T, eco, ns, name string) {
		a, b := Normalize(eco, ns, name)
		c, d := Normalize(eco, a, b)
		if a != c || b != d {
			t.Fatalf("not idempotent %q %q %q -> %q/%q -> %q/%q", eco, ns, name, a, b, c, d)
		}
	})
}
