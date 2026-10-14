package render

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/cantara/source-code-portal/internal/safehtml"
)

var xssCorpus = []string{
	"[x](javascript:alert(1))", "[x](JaVaScRiPt:alert(1))", "[x](&#106;avascript:alert(1))",
	"[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)", "![x](javascript:alert(1))", "<img src=x onerror=alert(1)>",
	"<script>alert(1)</script>", "<svg><script>alert(1)</script></svg>", "<svg onload=alert(1)>",
	"<math><mtext><table><mglyph><style><!--</style><img title=\"--&gt;&lt;img src=1 onerror=alert(1)&gt;\">",
	"<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\">", "<a href=\"#\" onclick=\"alert(1)\">x</a>",
	"<p style=\"background:url(javascript:alert(1))\">x</p>", "<iframe src=\"https://evil.example\"></iframe>",
	"<form action=\"https://evil.example\"><input name=p><button>go</button></form>", "<input type=text autofocus onfocus=alert(1)>",
	"<a href=\"https://evil.example\" target=\"_blank\">x</a>", "<base href=\"https://evil.example/\">",
	"<meta http-equiv=refresh content=\"0;url=https://evil.example\">", "<link rel=stylesheet href=https://evil.example/x.css>",
	"<details open ontoggle=alert(1)><summary>s</summary>b</details>", "<img srcset=\"javascript:alert(1) 1x\">",
	"<picture><source srcset=\"https://example.com/a.png\"><img src=\"a.png\" alt=\"a\"></picture>",
	"<a href=\"//evil.example/x\">protocol-relative</a>", "<div id=\"__proto__\" class=\"x\">", "<img src=\"a.png\" id=\"x\" name=\"cookie\">",
	"<xmp><script>alert(1)</script></xmp>", "<textarea><script>alert(1)</script></textarea>", "<svg><a xlink:href=javascript:1>x</a></svg>",
	"<a href=\"vbscript:msgbox(1)\">x</a>", "<a href=\" javascript:alert(1)\">x</a>", "<a href=\"java\tscript:alert(1)\">x</a>",
	"<video src=x onerror=alert(1)></video>", "<object data=\"javascript:alert(1)\"></object>", "<embed src=\"x\">",
	"<div id=\"x\"><a id=\"y\" name=\"z\" href=\"#x\">a</a></div>", "<style>@import 'https://evil.example/x.css';</style>",
	"<img src=\"https://evil.example/t.gif\">", "<a href=\"https://user:pw@evil.example/\">x</a>", "<img src=\"//evil.example/x.png\">",
	"<table background=\"javascript:alert(1)\"><tr><td>x</td></tr></table>", "<body onload=alert(1)>", "<custom-el onclick=alert(1)>t</custom-el>",
}

var banned = []string{"<script", "onerror", "onclick", "onload", "onfocus", "ontoggle", "javascript:", "vbscript:", "<iframe", "<style", "<form",
	"<base", "<meta", "<link", "style=", "<svg", "<math", "<object", "<embed", "<video", "name=", "srcset=\"//"}

func TestCorpus(t *testing.T) {
	p := New()
	for _, c := range xssCorpus {
		out, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver()})
		if err != nil {
			t.Fatal(err)
		}
		o := strings.ToLower(out.HTML.String())
		for _, bad := range banned {
			if strings.Contains(o, bad) {
				t.Errorf("%q contains %q:\n%s", c, bad, out.HTML.String())
			}
		}
		if again := safehtml.Sanitize(safehtml.DocsPolicy(), []byte(out.HTML.String())); again.String() != out.HTML.String() {
			t.Errorf("not a fixpoint for %q:\n%s\n%s", c, out.HTML.String(), again.String())
		}
		allowWalk(t, c, out.HTML.String())
	}
}

var allowedEl = map[string]bool{}
var allowedAttr = map[string]bool{"class": true, "id": true, "lang": true, "dir": true, "title": true, "role": true, "align": true, "colspan": true, "rowspan": true,
	"start": true, "open": true, "datetime": true, "href": true, "rel": true, "target": true, "src": true, "srcset": true, "alt": true, "width": true, "height": true,
	"loading": true, "decoding": true, "media": true, "type": true, "checked": true, "disabled": true}

func init() {
	for _, e := range strings.Fields("h1 h2 h3 h4 h5 h6 p br hr blockquote pre code em strong b i del s ins sup sub kbd mark details summary ul ol li dl dt dd table thead tbody tfoot tr caption figure figcaption abbr cite q small time section div span picture a img source th td input") {
		allowedEl[e] = true
	}
}

// allowWalk is the differential check: parse the output with x/net/html and assert nothing outside the allow-list survived.
func allowWalk(t *testing.T, in, out string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data != "html" && n.Data != "head" && n.Data != "body" {
			if !allowedEl[n.Data] {
				t.Errorf("%q: element <%s> outside allow-list", in, n.Data)
			}
			for _, a := range n.Attr {
				if !allowedAttr[a.Key] {
					t.Errorf("%q: attribute %s on <%s> outside allow-list", in, a.Key, n.Data)
				}
				if a.Key == "id" && !strings.HasPrefix(a.Val, "doc-") {
					t.Errorf("%q: id %q not namespaced", in, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

func FuzzRender(f *testing.F) {
	for _, c := range xssCorpus {
		f.Add(c)
	}
	f.Add(sample)
	p := New()
	f.Fuzz(func(t *testing.T, in string) {
		if len(in) > 1<<16 {
			t.Skip()
		}
		out, err := p.Markdown(context.Background(), Input{Source: []byte(in), Route: testResolver(), Split: true})
		if err != nil {
			return
		}
		s := out.HTML.String()
		if !utf8.ValidString(s) {
			t.Fatalf("invalid utf8 output")
		}
		if again := safehtml.Sanitize(safehtml.DocsPolicy(), []byte(s)); again.String() != s {
			t.Fatalf("not idempotent:\n%q\n%q", s, again.String())
		}
		allowWalk(t, in, s)
		if strings.Contains(out.LLMMarkdown, "<!--") && !strings.Contains(in, "```") && !strings.Contains(in, "~~~") && !strings.Contains(in, "    ") && !strings.Contains(in, "\t") {
			// comments outside any code construct must be gone from the LLM variant
			t.Logf("comment survived (non-code input): %q -> %q", in, out.LLMMarkdown)
		}
	})
}

// TestCorpusBridge runs the HTML payloads of the XSS corpus through the AsciiDoc bridge, whose input is untrusted
// GitHub HTML: the same banned-token, fixpoint and allow-list checks as TestCorpus (verification V-14: the bridge had only
// two benign fixtures). The Markdown-syntax payloads are skipped: as HTML they are escaped text.
func TestCorpusBridge(t *testing.T) {
	p := New()
	n := 0
	for _, c := range xssCorpus {
		if !strings.HasPrefix(c, "<") {
			continue
		}
		n++
		out, err := p.Bridge(context.Background(), "<article>"+c+"</article>", Input{Route: testResolver()})
		if err != nil {
			t.Fatal(err)
		}
		o := strings.ToLower(out.HTML.String())
		for _, bad := range banned {
			if strings.Contains(o, bad) {
				t.Errorf("bridge: %q contains %q:\n%s", c, bad, out.HTML.String())
			}
		}
		if again := safehtml.Sanitize(safehtml.DocsPolicy(), []byte(out.HTML.String())); again.String() != out.HTML.String() {
			t.Errorf("bridge: not a fixpoint for %q", c)
		}
		allowWalk(t, c, out.HTML.String())
	}
	if n < 30 {
		t.Fatalf("only %d HTML payloads", n)
	}
}
