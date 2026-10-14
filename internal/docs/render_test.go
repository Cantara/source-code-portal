package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func testLinks(dir string) links {
	return links{web: "https://github.com/", repo: "acme/x", dir: dir, indexed: map[string]bool{"README.md": true, "docs/setup.md": true}}
}

func TestRender(t *testing.T) {
	src := "---\ntitle: The X\norder: 2\nextra: kept\n---\n# Heading One\n\nFirst *para*graph.\n\n## Setup\n\n## Setup\n\n" +
		"> [!NOTE]\n> Mind the gap.\n\n" +
		"See [setup](docs/setup.md#run), [code](src/main.go), [dir](src/), [top](#setup), [ext](https://e.org), [bad](javascript:x), [up](../../x).\n\n" +
		"![logo](img/logo.png) ![remote](https://e.org/a.png) ![insecure](http://e.org/a.png)\n\n" +
		"<p align=\"center\"><img src=\"img/b.png\" onerror=\"x\" srcset=\"a.png 2x\"></p>\n\n" +
		"```go\nfunc main() {}\n```\n\n```mermaid\ngraph TD\n```\n\n- [x] done\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nNote[^1].\n\n[^1]: Foot.\n" +
		"<script>alert(1)</script>\n"
	r := &pipeline{policy: safehtml.New()}
	got := r.render("README.md", []byte(src), testLinks(""))
	h := got.HTML.String()
	for _, want := range []string{
		`<h1 id="doc-heading-one">Heading One</h1>`,
		`<h2 id="doc-setup">Setup</h2>`, `<h2 id="doc-setup-1">Setup</h2>`,
		`<blockquote class="md-alert md-alert-note">`, `<p class="md-alert-title">Note</p>`, `Mind the gap.`,
		`href="/repos/acme/x/docs/docs/setup#doc-run"`,
		`href="https://github.com/acme/x/blob/HEAD/src/main.go" rel="noopener noreferrer nofollow ugc"`,
		`href="https://github.com/acme/x/tree/HEAD/src/"`,
		`href="#doc-setup"`, `href="https://e.org" rel="noopener noreferrer nofollow ugc"`,
		`<img src="https://raw.githubusercontent.com/acme/x/HEAD/img/logo.png" alt="logo" loading="lazy" referrerpolicy="no-referrer">`,
		`<img src="https://e.org/a.png"`, `insecure`,
		`<p align="center"><img src="https://raw.githubusercontent.com/acme/x/HEAD/img/b.png" alt="" loading="lazy" referrerpolicy="no-referrer"></p>`,
		`<pre class="chroma"><code class="language-go"><span class="kd">func</span>`,
		`<pre class="chroma"><code class="language-mermaid">graph TD`,
		`<input checked="" disabled="" type="checkbox">`, `<table>`,
		`href="#doc-fn:1"`, `id="doc-fn:1"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{"<script", "javascript", "onerror", "srcset", "http://e.org", "../../x", "[!NOTE]"} {
		if strings.Contains(h, bad) {
			t.Errorf("holds %s", bad)
		}
	}
	if got.Title != "The X" || got.Order != 2 || got.Description != "First paragraph." || got.Format != "markdown" ||
		got.State != snapshot.StateOK || !strings.HasPrefix(string(got.Source), "---\ntitle") {
		t.Errorf("render = %+v", got)
	}
	if len(got.TOC) != 2 || got.TOC[0] != (snapshot.Heading{Level: 2, ID: "doc-setup", Text: "Setup"}) {
		t.Errorf("toc = %+v", got.TOC)
	}
	if testing.Verbose() {
		t.Log(h)
	}
}

func TestRenderStates(t *testing.T) {
	r := &pipeline{policy: safehtml.New()}
	tests := []struct {
		path, src         string
		format            string
		state             snapshot.State
		truncated, hidden bool
		title             string
	}{
		{"README.rst", "Title\n=====\n<b>x</b>", "text", snapshot.StateOK, false, false, "README.rst"},
		{"README", "# Hello\n", "markdown", snapshot.StateOK, false, false, "Hello"},
		{"README", "plain words\n", "text", snapshot.StateOK, false, false, "README"},
		{"README.md", "\xff\xfeh\x00i\x00", "text", snapshot.StateUnsupported, false, false, "README.md"},
		{"README.md", strings.Repeat("\x00", 100), "text", snapshot.StateUnsupported, false, false, "README.md"},
		{"docs/a.md", "---\nhidden: true\n---\nbody\n", "markdown", snapshot.StateOK, false, true, "a.md"},
		{"docs/a.md", "---\nx: &a 1\n---\nbody\n", "markdown", snapshot.StateOK, false, false, "a.md"},
		{"docs/big.md", strings.Repeat("line of text\n\n", 50000), "markdown", snapshot.StateOK, true, false, "big.md"},
		{"docs/deep.md", strings.Repeat(">", 100) + " x\n", "text", snapshot.StateOK, false, false, "deep.md"},
		{"docs/long.md", strings.Repeat("word ", 10000), "text", snapshot.StateOK, false, false, "long.md"},
	}
	for _, tc := range tests {
		got := r.render(tc.path, []byte(tc.src), testLinks("docs"))
		if got.Format != tc.format || got.State != tc.state || got.Truncated != tc.truncated || got.Hidden != tc.hidden || got.Title != tc.title {
			t.Errorf("%s %.20q: format %s, state %s, truncated %v, hidden %v, title %q", tc.path, tc.src, got.Format, got.State, got.Truncated,
				got.Hidden, got.Title)
		}
		if got.Format == "text" && got.State == snapshot.StateOK && !strings.HasPrefix(got.HTML.String(), "<pre>") {
			t.Errorf("%s: text not in <pre>: %.60s", tc.path, got.HTML)
		}
		if len(got.Source) > maxDocBytes {
			t.Errorf("%s: source of %d bytes", tc.path, len(got.Source))
		}
	}
}

func TestRenderBounds(t *testing.T) {
	r := &pipeline{policy: safehtml.New()}
	for name, tc := range map[string]struct {
		src    string
		format string // markdown when the guard admits it, text when it is rejected
	}{
		"brackets":   {strings.Repeat("[", 400<<10), "text"},
		"emphasis":   {strings.Repeat("*a _b ", 80<<10), "text"},
		"links":      {strings.Repeat("[a](b) ", 70<<10), "text"},
		"backticks":  {strings.Repeat("`", 400<<10), "text"},
		"paragraphs": {strings.Repeat("some *words* and [links](x) in a line\n", 1000) + "\n", "markdown"},
		"lists":      {strings.Repeat("- a *b* [c](d)\n  - e\n", 20<<10), "markdown"},
		"code":       {strings.Repeat("```go\nfunc f() { return }\n```\n", 15<<10), "markdown"},
		"html":       {strings.Repeat("<div>\n", 70<<10), "markdown"},
		"tables":     {"|a|b|\n|-|-|\n" + strings.Repeat("|1|*2*|\n", 50<<10), "markdown"},
		"emphasis2":  {strings.Repeat("*a* _b_ **c** ", 2000) + "\n\n", "markdown"},
	} {
		start := time.Now()
		got := r.render("docs/x.md", []byte(tc.src), testLinks("docs"))
		if d := time.Since(start); d > 3*time.Second || got.Format != tc.format {
			t.Errorf("%s: %v, format %s, want %s", name, d, got.Format, tc.format)
		}
	}
}

// lockDigest pins the pipeline output for renderCorpus. When it changes, bump pipelineVersion and set the new digest.
const lockDigest = "f0ac396f43dcf45e875d1dff2ae348704cce232fc14d6ce83c36b4b791fac592"

func TestPipelineLock(t *testing.T) {
	r := &pipeline{policy: safehtml.New()}
	h := sha256.New()
	fmt.Fprintf(h, "%d\n", pipelineVersion)
	for _, src := range renderCorpus {
		h.Write([]byte(r.render("docs/x.md", []byte(src), testLinks("docs")).HTML.String()))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != lockDigest {
		t.Errorf("the pipeline output changed: bump pipelineVersion if needed and set lockDigest = %q", got)
	}
}

var renderCorpus = []string{ //nolint:gochecknoglobals // test table
	"# T\n\ntext *em* **strong** ~~del~~ `code` <kbd>K</kbd>\n",
	"> [!WARNING]\n> careful\n",
	"```python\nprint('x')\n```\n",
	"[a](./b.md) [c](../README.md#x) ![i](i.png) <img src=\"j.png\" width=\"40\">\n",
	"| a |\n|---|\n| 1 |\n\n- [ ] t\n\nx[^n]\n\n[^n]: y\n",
	"<details><summary>s</summary>\n\nbody\n</details>\n",
}

func FuzzRender(f *testing.F) {
	for _, s := range renderCorpus {
		f.Add(s)
	}
	f.Add("<a href=\"javascript:x\">[x](javascript:y)</a> ![](data:image/png;base64,x)")
	r := &pipeline{policy: safehtml.New()}
	f.Fuzz(func(t *testing.T, src string) {
		got := r.render("docs/x.md", []byte(src), testLinks("docs"))
		again, err := r.policy.Sanitize([]byte(got.HTML.String()))
		if err != nil || again != got.HTML {
			t.Fatalf("output is not a sanitiser fixpoint:\n%q\n%q", got.HTML, again)
		}
		if strings.Contains(strings.ToLower(got.HTML.String()), "javascript:") && !strings.Contains(strings.ToLower(src), "javascript:") {
			t.Fatalf("javascript: appeared from nowhere: %q", got.HTML)
		}
	})
}
