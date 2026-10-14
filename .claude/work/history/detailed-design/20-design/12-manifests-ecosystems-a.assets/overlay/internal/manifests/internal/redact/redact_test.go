package redact

import (
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:pw@github.com/o/r.git":   "https://github.com/o/r.git",
		"https://token@github.com/o/r":         "https://github.com/o/r",
		"https://example.com/a?token=abc&x=1":  "https://example.com/a?x=1",
		"https://example.com/a?sig=1&ApiKey=2": "https://example.com/a",
		"https://example.com/a#v1.2":           "https://example.com/a#v1.2",
		"https://example.com/a#token=zzz":      "https://example.com/a",
		"git@github.com:o/r.git":               "git@github.com:o/r.git",
		"github:o/r":                           "github:o/r",
		"../local/path":                        "../local/path",
		"ssh://git@host/o/r":                   "ssh://host/o/r",
		"https://a.example/p?%74oken=1":        "https://a.example/p",
		// verification VF-12-07: scheme-relative userinfo, ";" separators, a secret key that is not the first fragment parameter
		"//user:pw@host/x":                    "//host/x",
		"https://h/x?a=1;token=zzz;b=2":       "https://h/x?a=1&b=2",
		"git+https://h/o/r.git#egg=y&token=z": "git+https://h/o/r.git#egg=y",
		"git+https://h/o/r.git#token=z&egg=y": "git+https://h/o/r.git#egg=y",
	} {
		if got := URL(in); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := URL("https://h/" + strings.Repeat("a", 500)); len(got) != 300 {
		t.Errorf("not clipped: %d", len(got))
	}
	if got := URL("https://h/a\x00b\nc"); strings.ContainsAny(got, "\x00\n") {
		t.Errorf("control characters survive: %q", got)
	}
}

func FuzzURL(f *testing.F) {
	for _, s := range []string{"https://u:p@h/x?token=1#a", "a@b:c", "%zz?=&&#"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := URL(s)
		if len(out) > 300 || strings.ContainsAny(out, "\x00\n\r") {
			t.Fatalf("bad output %q", out)
		}
		if strings.Contains(s, "://") && strings.Contains(s, "pw@") && strings.Contains(out, "pw@") && !strings.Contains(s[strings.Index(s, "pw@"):], "/") {
			t.Fatalf("userinfo survived: %q -> %q", s, out)
		}
	})
}
