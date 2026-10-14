package manifests

import (
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"abc", "abc"},
		{"a\x00b\x1bc\x7f", "abc"},
		{"line1\nline2\ttab", "line1 line2 tab"},
		{"x\u202Ey\u2066z\u200Bw\uFEFF", "xyzw"},
		{"t\U000E0041\U000E0042x", "tx"}, // Unicode tag characters (hidden instructions)
		{"bad\xffbyte", "bad\uFFFDbyte"},
		{"\u0085nel", "nel"},
		{strings.Repeat("é", 10), "ééééé"}, // cut at 10 bytes on a rune boundary
	} {
		max := 512
		if strings.HasPrefix(c.in, "éé") {
			max = 10
		}
		if got := CleanText(c.in, max); got != c.want {
			t.Errorf("CleanText(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func FuzzCleanText(f *testing.F) {
	f.Add("a\u202Eb\x00")
	f.Fuzz(func(t *testing.T, s string) {
		out := CleanText(s, 64)
		if len(out) > 64 || !utf8ValidNoControl(out) || CleanText(out, 64) != out {
			t.Fatalf("%q -> %q", s, out)
		}
	})
}

func utf8ValidNoControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || isInvisible(r) {
			return false
		}
	}
	return true
}

func TestSanitizeParsed(t *testing.T) {
	spec, _ := NewRegistry().Spec(PyPI)
	p := Parsed{Dependencies: []Dependency{
		{Target: ComponentID{PyPI, "", "Django_Foo.Bar"}, Requirement: ">=1\u202E", Line: 99},
		{Target: ComponentID{PyPI, "", "\x00"}},
	}}
	sanitizeParsed(&p, spec, DefaultConfig().Limits, 10)
	if len(p.Dependencies) != 1 || p.Dependencies[0].Target.Name != "django-foo-bar" || p.Dependencies[0].Requirement != ">=1" || p.Dependencies[0].Line != 0 {
		t.Fatalf("%+v", p.Dependencies)
	}
	if len(p.Diagnostics) != 1 || p.Diagnostics[0].Code != "parse.invalid-dependency" {
		t.Fatalf("%+v", p.Diagnostics)
	}
}
