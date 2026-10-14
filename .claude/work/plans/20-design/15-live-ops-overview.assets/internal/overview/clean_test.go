package overview

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\nb\tc", "a b c"},
		{"[x](http://e)", `\[x\](http://e)`},
		{"<!-- c -->", `&lt;\!-- c --&gt;`},
		{"C# and *bold* and _it_ and `code` and a|b", `C\# and \*bold\* and \_it\_ and \` + "`" + `code\` + "`" + ` and a\|b`},
		{"‮abc​⁦", "abc"},
		{"tag\U000E0041\U000E007F", "tag"},
		{"back\\slash", `back\\slash`},
		{"\xff\xfe", "��"},
		{"", ""},
	}
	for _, c := range cases {
		if got := line(c.in, 0); got != c.want {
			t.Errorf("line(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := line(strings.Repeat("é", 10), 5); got != "éééé…" {
		t.Errorf("cut by runes with an ellipsis inside the cap: %q", got)
	}
}

func TestFenceBodyAndAttr(t *testing.T) {
	if got := fenceBody("a <!-- scp:doc x --> b"); got != "a &lt;!-- scp:doc x --&gt; b" {
		t.Errorf("%q", got)
	}
	if got := fenceBody("no comments"); got != "no comments" {
		t.Errorf("%q", got)
	}
	if got := attr("dir with space/--> x\nREADME.md"); got != "dir%20with%20space/--%3E%20x%0AREADME.md" {
		t.Errorf("%q", got)
	}
	if got := urlForMarkdown("https://x/a(b) c<d>\\e"); got != "https://x/a%28b%29%20c%3Cd%3E%5Ce" {
		t.Errorf("%q", got)
	}
}

// FuzzLine: whatever repository text says, one cleaned line stays one line, opens no HTML or Markdown construct and
// contains nothing invisible.
func FuzzLine(f *testing.F) {
	for _, s := range []string{"", "a", "[a](b)", "<!--", "‮", "x\ny", "\U000E0041", "\xff", "# h", "`", "![i](u)"} {
		f.Add(s, 40)
	}
	f.Fuzz(func(t *testing.T, s string, max int) {
		out := line(s, max%200)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8: %q", out)
		}
		for i, r := range out {
			switch {
			case r == '\n' || r == '\r' || r == '\t' || r < 0x20 || r == 0x7f:
				t.Fatalf("control character in %q", out)
			case r == '<' || r == '>':
				t.Fatalf("raw angle bracket at %d in %q", i, out)
			case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200B, r == 0xFEFF, r >= 0xE0000 && r <= 0xE007F:
				t.Fatalf("invisible character %U in %q", r, out)
			}
		}
		for _, c := range []string{"[", "]", "*", "_", "`", "|", "#", "!"} {
			for i := 0; i < len(out); i++ {
				if out[i] == c[0] && (i == 0 || out[i-1] != '\\') {
					t.Fatalf("unescaped %q in %q", c, out)
				}
			}
		}
	})
}
