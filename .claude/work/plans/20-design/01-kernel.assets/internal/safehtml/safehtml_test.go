package safehtml_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/cantara/source-code-portal/internal/safehtml"
)

var pol = safehtml.DocsPolicy()

func TestCorpusIsSafeAndAFixpoint(t *testing.T) {
	for _, in := range corpus {
		out := safehtml.Sanitize(pol, []byte(in))
		low := strings.ToLower(out.String())
		for _, bad := range badSubstrings {
			if strings.Contains(low, bad) {
				t.Errorf("DANGEROUS %q in output %q for input %q", bad, out, in)
			}
		}
		again := safehtml.Sanitize(pol, []byte(out.String()))
		if again != out {
			t.Errorf("not a fixpoint for %q:\n 1: %q\n 2: %q", in, out, again)
		}
	}
}

func TestSpecificOutputs(t *testing.T) {
	cases := []struct{ in, contains, notContains string }{
		{`<a href="https://example.com">x</a>`, `target="_blank"`, ""},
		{`<a href="https://example.com">x</a>`, `noopener`, ""},
		{`<a href="https://example.com">x</a>`, `noreferrer`, ""},
		{`<a href="https://example.com">x</a>`, `nofollow`, ""},
		{`<a href="/repos/x">x</a>`, `href="/repos/x"`, `rel=`},
		{`<a href="https://example.com" rel="noopener noreferrer nofollow ugc">x</a>`, `ugc`, ""},
		{`<h2 id="heading-one">x</h2>`, `<h2>x</h2>`, `id=`},
		{`<h2 id="doc-heading-one">x</h2>`, `id="doc-heading-one"`, ""},
		{`<span class="fixed">x</span>`, `<span>x</span>`, `class=`},
		{`<span class="kd">x</span>`, `class="kd"`, ""},
		{`<div class="fixed inset-0 z-50">x</div>`, `<div>x</div>`, `class=`},
		{`<img srcset="/media/a.png 1x, /media/b.png 2x" src="/media/a.png">`, `srcset="/media/a.png 1x, /media/b.png 2x"`, ""},
		{`<img srcset="/media/a.png 1x, javascript:alert(1) 2x">`, ``, `srcset`},
		{`<img src="/media/a.png" srcset="/media/a.png 1x, javascript:alert(1) 2x">`, `<img src="/media/a.png">`, `srcset`},
		{`<a href="tel:+4712345678">t</a>`, `t`, `tel:`},
		{`<a name="x" href="#doc-x">a</a>`, `href="#doc-x"`, `name=`},
		{`<script>alert(1)</script>after`, `after`, `alert`},
		{`<div role="button">x</div>`, `<div>x</div>`, `role=`},
		{`<input type="checkbox" checked disabled>`, `type="checkbox"`, ""},
		{`<input type="text" value="x">`, ``, `<input`},
		{`<img src="https://t.example/p.gif" alt="a">`, `alt="a"`, `src=`},
		{`<img src="//t.example/p.gif" alt="a">`, `alt="a"`, `src=`},
		{`<img src="/\\t.example/p.gif" alt="a">`, `alt="a"`, `src=`},
		{`<img src="/media/x/main/a.png" alt="a">`, `src="/media/x/main/a.png"`, ``},
		{`<img src="a.png" alt="a">`, `alt="a"`, `src=`},
		{`<picture><source srcset="/media/a.png 1x, /media/b.png 2x" media="(prefers-color-scheme: dark)"><img src="/media/a.png" alt="a"></picture>`, `srcset=`, ``},
		{`<picture><source srcset="https://e.example/a.png"><img src="/media/a.png"></picture>`, `<picture><img src="/media/a.png"></picture>`, `https`},
		{`<a href="https://example.com" rel="me" target="_self">x</a>`, `rel="nofollow noreferrer noopener" target="_blank"`, `me`},
	}
	for _, c := range cases {
		out := safehtml.Sanitize(pol, []byte(c.in)).String()
		if !strings.Contains(out, c.contains) {
			t.Errorf("%q -> %q, want it to contain %q", c.in, out, c.contains)
		}
		if c.notContains != "" && strings.Contains(out, c.notContains) {
			t.Errorf("%q -> %q, must not contain %q", c.in, out, c.notContains)
		}
	}
}

func TestNilPolicyStripsEverything(t *testing.T) {
	out := safehtml.Sanitize(nil, []byte(`<p>hi <b>there</b><script>x</script></p>`)).String()
	if strings.Contains(out, "<") {
		t.Fatalf("markup survived a nil policy: %q", out)
	}
}

func TestOversizedInputBecomesNotice(t *testing.T) {
	out := safehtml.Sanitize(pol, make([]byte, safehtml.MaxInputBytes+1)).String()
	if out != "<p>Content too large to display.</p>" {
		t.Fatalf("got %q", out)
	}
}

func TestTextIsEscapedAndAFixpoint(t *testing.T) {
	for _, s := range []string{"plain", "<script>alert(1)</script>", "a & b < c", "\x00nul", "bad\xffutf8", "", "```md\n# x\n```"} {
		tx := safehtml.Text(s)
		if !strings.HasPrefix(tx.String(), "<pre>") || strings.Contains(tx.String(), "<script") {
			t.Errorf("Text(%q) = %q", s, tx)
		}
		if again := safehtml.Sanitize(pol, []byte(tx.String())); again != tx {
			t.Errorf("Text(%q) is not a Sanitize fixpoint: %q vs %q", s, tx, again)
		}
	}
}

func TestMarshalOnlyNeverUnmarshal(t *testing.T) {
	type dto struct {
		HTML safehtml.SafeHTML `json:"html"`
	}
	b, err := json.Marshal(dto{HTML: safehtml.Text("a")})
	if err != nil || string(b) != `{"html":"\u003cpre\u003ea\u003c/pre\u003e"}` {
		t.Fatalf("marshal: %s %v", b, err)
	}
	var d dto
	if err := json.Unmarshal(b, &d); err == nil {
		t.Fatal("unmarshal into SafeHTML must fail")
	}
}

var update = flag.Bool("update", false, "rewrite testdata/policy_golden.json")

type goldenFile struct {
	PolicyVersion int               `json:"policyVersion"`
	Outputs       map[string]string `json:"outputs"`
}

// TestPolicyGolden pins the observable behaviour of the policy to PolicyVersion. A changed output for a recorded input
// fails until PolicyVersion is bumped and the file regenerated with -update; new corpus inputs are added with -update
// and need no bump.
func TestPolicyGolden(t *testing.T) {
	const path = "testdata/policy_golden.json"
	cur := goldenFile{PolicyVersion: safehtml.PolicyVersion, Outputs: map[string]string{}}
	for _, in := range corpus {
		cur.Outputs[in] = safehtml.Sanitize(pol, []byte(in)).String()
	}
	if *update {
		b, _ := json.MarshalIndent(cur, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want goldenFile
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for in, out := range want.Outputs {
		got, ok := cur.Outputs[in]
		switch {
		case !ok:
			t.Errorf("golden input no longer in the corpus: %q", in)
		case got != out && want.PolicyVersion == safehtml.PolicyVersion:
			t.Errorf("policy output changed without a PolicyVersion bump for %q:\n golden: %q\n now:    %q", in, out, got)
		}
	}
	for in := range cur.Outputs {
		if _, ok := want.Outputs[in]; !ok {
			t.Errorf("corpus input missing from the golden file (run with -update): %q", in)
		}
	}
	if want.PolicyVersion != safehtml.PolicyVersion {
		t.Errorf("golden file is for version %d, code is %d: regenerate with -update", want.PolicyVersion, safehtml.PolicyVersion)
	}
}

var allowedElements = map[string]bool{}
var allowedAttrs = map[string]bool{"class": true, "id": true, "role": true, "lang": true, "dir": true, "title": true, "align": true, "colspan": true,
	"rowspan": true, "start": true, "open": true, "datetime": true, "href": true, "rel": true, "target": true, "src": true, "srcset": true, "alt": true,
	"width": true, "height": true, "loading": true, "media": true, "type": true, "checked": true, "disabled": true}

func init() {
	for _, e := range strings.Fields("h1 h2 h3 h4 h5 h6 p br hr blockquote pre code em strong b i u del s ins sup sub kbd samp var mark details summary " +
		"ul ol li dl dt dd table thead tbody tfoot tr caption figure figcaption abbr cite q small time section div span picture a img source th td input " +
		"html head body") { // the parser adds html/head/body around a fragment
		allowedElements[e] = true
	}
}

// structuralOracle parses sanitiser output and checks the properties that make it safe, instead of grepping for strings.
func structuralOracle(out string) error {
	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		return err
	}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			if !allowedElements[n.Data] {
				return fmt.Errorf("element <%s> in output", n.Data)
			}
			for _, a := range n.Attr {
				if !allowedAttrs[a.Key] || strings.HasPrefix(a.Key, "on") {
					return fmt.Errorf("attribute %s on <%s>", a.Key, n.Data)
				}
				switch a.Key {
				case "href":
					u, err := url.Parse(a.Val)
					if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
						return fmt.Errorf("href %q", a.Val)
					}
					// T4: a link that leaves the origin (http, https or scheme-relative) always carries the full rel and target
					if u.Scheme == "http" || u.Scheme == "https" || u.Host != "" {
						rel, target := "", ""
						for _, b := range n.Attr {
							switch b.Key {
							case "rel":
								rel = b.Val
							case "target":
								target = b.Val
							}
						}
						tokens := strings.Fields(rel) // the author's tokens are merged with ours, never replace them
						if target != "_blank" || u.Host == "" || !slices.Contains(tokens, "noopener") || !slices.Contains(tokens, "noreferrer") || !slices.Contains(tokens, "nofollow") {
							return fmt.Errorf("external link %q without rel=%q target=%q or without a host", a.Val, rel, target)
						}
					}
				case "src":
					if !strings.HasPrefix(a.Val, "/") || strings.HasPrefix(a.Val, "//") || strings.HasPrefix(a.Val, "/\\") {
						return fmt.Errorf("src %q is not same-origin", a.Val)
					}
				case "id":
					if !strings.HasPrefix(a.Val, safehtml.IDPrefix) {
						return fmt.Errorf("id %q without prefix", a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(doc)
}

func TestStructuralOracleOnCorpus(t *testing.T) {
	for _, in := range corpus {
		if err := structuralOracle(safehtml.Sanitize(pol, []byte(in)).String()); err != nil {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func FuzzSanitizeFixpoint(f *testing.F) {
	for _, s := range corpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		a := safehtml.Sanitize(pol, []byte(in))
		b := safehtml.Sanitize(pol, []byte(a.String()))
		if a != b {
			t.Fatalf("not a fixpoint:\n%q\n%q", a, b)
		}
		if err := structuralOracle(a.String()); err != nil {
			t.Fatalf("%v in %q", err, a)
		}
	})
}

// TestBombs records the time Sanitize needs for adversarial inputs at the size cap; the budget guards MaxInputBytes.
func TestBombs(t *testing.T) {
	const n = 4 << 20
	bombs := map[string]string{
		"deep-div":     strings.Repeat("<div>", n/5),
		"deep-span-a":  strings.Repeat(`<span><a href="https://e.example/">`, n/36),
		"lt-flood":     strings.Repeat("<", n),
		"entity-flood": strings.Repeat("&amp;&#x41;&lt;", n/14),
		"attr-flood":   "<a " + strings.Repeat(`x="1" `, n/6) + ">",
		"unclosed-cmt": "<!--" + strings.Repeat("a", n),
		"many-tags":    strings.Repeat("<p>a</p>", n/8),
		"many-links":   strings.Repeat(`<a href="https://e.example/x">x</a>`, n/36),
		"long-attr":    `<a href="` + strings.Repeat("a", n) + `">x</a>`,
		"class-regex":  `<span class="` + strings.Repeat("a", n) + `">x</span>`,
		"srcset-regex": `<img srcset="` + strings.Repeat("/a 1x, ", n/7) + `">`,
		"id-regex":     `<p id="doc-` + strings.Repeat("a", n) + `">x</p>`,
	}
	for name, in := range bombs {
		start := time.Now()
		out := safehtml.Sanitize(pol, []byte(in))
		d := time.Since(start)
		t.Logf("%-14s in=%8d out=%8d %v", name, len(in), len(out.String()), d)
		if d > 5*time.Second {
			t.Errorf("%s took %v", name, d)
		}
	}
}

// TestOpaqueAbsoluteLinksAreDropped (V-12): "https:///host" and "https:host" are absolute links for a browser but look
// relative to a URL parser, so they would escape the rel/target rule of fully-qualified links (T4).
func TestOpaqueAbsoluteLinksAreDropped(t *testing.T) {
	for in, want := range map[string]string{
		`<a href="https:///evil.example/x">t</a>`: "t",
		`<a href="https:evil.example">t</a>`:      "t",
		`<a href="x:y">t</a>`:                     "t",
		`<a href="HTTP://EVIL.example/x">t</a>`:   `<a href="http://EVIL.example/x" rel="nofollow noreferrer noopener" target="_blank">t</a>`,
		`<a href="./a:b#c">t</a>`:                 `<a href="./a:b#c">t</a>`,
		`<a href="/repos/x?y=1#z">t</a>`:          `<a href="/repos/x?y=1#z">t</a>`,
		`<a href="#doc-x">t</a>`:                  `<a href="#doc-x">t</a>`,
		`<a href="mailto:a@b.example">t</a>`:      `<a href="mailto:a@b.example">t</a>`,
	} {
		if got := safehtml.Sanitize(pol, []byte(in)).String(); got != want {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}

// TestOutputIsNotBalanced pins SH-03(g): the sanitiser filters tokens; it does not repair nesting. Callers inject the
// result as the innerHTML of one dedicated element and never concatenate it into a larger document.
func TestOutputIsNotBalanced(t *testing.T) {
	for in, want := range map[string]string{"<div>unclosed": "<div>unclosed", "</div></div>stray": "</div></div>stray"} {
		if got := safehtml.Sanitize(pol, []byte(in)).String(); got != want {
			t.Errorf("%q -> %q, want %q (if the sanitiser started balancing tags, update SH-03(g))", in, got, want)
		}
	}
}
