package san

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCorpus(t *testing.T) {
	cases := []string{
		"[x](javascript:alert(1))",
		"[x](JaVaScRiPt:alert(1))",
		"[x](&#106;avascript:alert(1))",
		"[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
		"![x](javascript:alert(1))",
		"<img src=x onerror=alert(1)>",
		"<script>alert(1)</script>",
		"<svg><script>alert(1)</script></svg>",
		"<svg onload=alert(1)>",
		"<math><mtext><table><mglyph><style><!--</style><img title=\"--&gt;&lt;img src=1 onerror=alert(1)&gt;\">",
		"<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\">",
		"<a href=\"#\" onclick=\"alert(1)\">x</a>",
		"<p style=\"background:url(javascript:alert(1))\">x</p>",
		"<iframe src=\"https://evil.example\"></iframe>",
		"<form action=\"https://evil.example\"><input name=p><button>go</button></form>",
		"<input type=text autofocus onfocus=alert(1)>",
		"<a href=\"https://evil.example\" target=\"_blank\">x</a>",
		"<base href=\"https://evil.example/\">",
		"<meta http-equiv=refresh content=\"0;url=https://evil.example\">",
		"<link rel=stylesheet href=https://evil.example/x.css>",
		"<details open ontoggle=alert(1)><summary>s</summary>b</details>",
		"<img srcset=\"javascript:alert(1) 1x\">",
		"<picture><source srcset=\"https://example.com/a.png\"><img src=\"a.png\" alt=\"a\"></picture>",
		"<a href=\"//evil.example/x\">protocol-relative</a>",
		"<div id=\"__proto__\" class=\"x\">",
		"<img src=\"a.png\" id=\"x\" name=\"cookie\">",
		"<xmp><script>alert(1)</script></xmp>",
		"<textarea><script>alert(1)</script></textarea>",
		"- [x] done\n- [ ] todo",
		"> [!NOTE]\n> hello",
		"| a | b |\n|:--|--:|\n| 1 | 2 |",
		"```go\nfmt.Println(\"<script>\")\n```",
		"Footnote[^1].\n\n[^1]: note",
		"# Heading One\n## Heading One",
		"<p align=\"center\"><img src=\"logo.png\" width=\"200\" alt=\"logo\"></p>",
	}
	for _, c := range cases {
		out, _ := Render([]byte(c))
		fmt.Printf("IN : %q\nOUT: %q\n\n", c, out.String())
		o := strings.ToLower(out.String())
		for _, bad := range []string{"<script", "onerror", "onclick", "onload", "javascript:", "<iframe", "<style", "<form", "<base", "<meta", "<link", "style=", "onfocus", "ontoggle", "<svg", "<math"} {
			if strings.Contains(o, bad) {
				t.Errorf("DANGEROUS %q in output for %q", bad, c)
			}
		}
	}
}

func TestIdempotent(t *testing.T) {
	src := "# T\n\n<p align=center><a href=\"https://x.example\">x</a></p>\n- [x] a\n"
	a, _ := Render([]byte(src))
	b := policy.Sanitize(a.String())
	if a.String() != b {
		t.Errorf("not a fixpoint:\n%q\n%q", a.String(), b)
	}
}

func TestPerf(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 4000; i++ {
		sb.WriteString("## Heading\n\nSome *text* with [a link](https://example.com/x) and `code` and <b>html</b>.\n\n- [x] item\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n")
	}
	st := time.Now()
	out, _ := Render([]byte(sb.String()))
	fmt.Printf("input %d bytes -> output %d bytes in %s\n", sb.Len(), len(out.String()), time.Since(st))
}
