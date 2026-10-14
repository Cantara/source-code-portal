package catalog

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestMakeSlug(t *testing.T) {
	ok := []struct{ prefix, name, want string }{
		{"", "Widget-TokenService", "widget-tokenservice"}, // canonical slugs are lower-case (scm.ParseSlug),
		{"", "_parentpom", "_parentpom"},
		{"", ".github", ".github"},
		{"", "acme.github.io", "acme.github.io"},
		{"acme-", "api", "acme-api"},
		{"Acme-", "API", "acme-api"},
		{"", "notes.md", "notes-md"},   // would be served as a raw document otherwise
		{"", "data.JSON", "data-json"}, // suffix check is case-insensitive
		{"", "a.b.txt", "a.b-txt"},
	}
	for _, c := range ok {
		got, err := MakeSlug(c.prefix, c.name)
		if err != nil || string(got) != c.want {
			t.Errorf("MakeSlug(%q,%q) = %q, %v; want %q", c.prefix, c.name, got, err, c.want)
		}
	}
	// 100-byte GitHub name + prefix may exceed the kernel's 100-byte slug limit: too_long, never a truncated slug
	if _, err := MakeSlug("p-", strings.Repeat("a", 100)); err == nil || err.(*SlugError).Code != "too_long" {
		t.Errorf("a 102-byte slug must be too_long, got %v", err)
	}
	for _, n := range []string{"", ".", "..", "ext", "Groups", "AVATARS", "a b", "a/b", "é", "a:b"} {
		if s, err := MakeSlug("", n); err == nil {
			t.Errorf("MakeSlug(%q) = %q, want error", n, s)
		}
	}
}

func FuzzMakeSlug(f *testing.F) {
	for _, s := range []string{"a", "x.md", ".", "..", "ext", "é", "a b"} {
		f.Add("", s)
		f.Add("p-", s)
	}
	f.Fuzz(func(t *testing.T, prefix, name string) {
		s, err := MakeSlug(prefix, name)
		if err != nil {
			return
		}
		if _, perr := scm.ParseSlug(string(s)); perr != nil || len(s) > MaxSlugBytes { // the kernel grammar is the oracle
			t.Fatalf("illegal slug %q from %q+%q: %v", s, prefix, name, perr)
		}
		if reservedSlugs[FoldSlug(s)] {
			t.Fatalf("illegal slug %q from %q+%q", s, prefix, name)
		}
		if again, _ := MakeSlug(prefix, name); again != s {
			t.Fatal("not deterministic")
		}
	})
}
