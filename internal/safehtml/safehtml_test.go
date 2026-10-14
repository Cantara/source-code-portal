package safehtml

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// unsafe reports the first element, attribute or URL in out that the policy must never let through, walking the output on its own.
func unsafe(out string) string {
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.CommentToken:
			return "comment"
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			switch string(name) {
			case "script", "style", "svg", "math", "iframe", "object", "embed", "base", "form", "link", "meta", "template":
				return string(name)
			}
			for more {
				var k, v []byte
				k, v, more = z.TagAttr()
				key, val := string(k), strings.ToLower(strings.TrimSpace(string(v)))
				switch {
				case strings.HasPrefix(key, "on"), key == "style", key == "name", key == "srcdoc", strings.HasPrefix(key, "data-"):
					return key
				case (key == "href" || key == "src") && !safeURL(val):
					return key + "=" + val
				case key == "id" && !strings.HasPrefix(val, "doc-"):
					return key + "=" + val
				}
			}
		}
	}
}

func TestSanitize(t *testing.T) {
	p := New()
	tests := []struct{ in, want string }{
		{`<p>hi <script>alert(1)</script></p>`, `<p>hi </p>`},
		{`<a href="javascript:alert(1)">x</a>`, `x`},
		{`<a href="&#106;avascript:alert(1)">x</a>`, `x`},
		{`<a href="https://e.org" rel="noopener noreferrer nofollow ugc">x</a>`, `<a href="https://e.org" rel="noopener noreferrer nofollow ugc">x</a>`},
		{`<a href="/repos/a/b/docs/x.md">x</a>`, `<a href="/repos/a/b/docs/x.md">x</a>`},
		{`<a href="#doc-intro">x</a>`, `<a href="#doc-intro">x</a>`},
		{`<img src="http://e.org/a.png" alt="a">`, `<img alt="a">`},
		{`<img src="https://e.org/a.png" alt="a">`, `<img alt="a">`},
		{`<img src="/images/a/b/0123456789abcdef0123456789abcdef" alt="a" onerror="x" style="x" loading="lazy" referrerpolicy="no-referrer">`,
			`<img src="/images/a/b/0123456789abcdef0123456789abcdef" alt="a" loading="lazy" referrerpolicy="no-referrer">`},
		{`<img src="/images/a/b/../../x">`, ``},
		{`<h2 id="doc-setup">Setup</h2><h2 id="__proto__">x</h2><a name="top">t</a>`, `<h2 id="doc-setup">Setup</h2><h2>x</h2>t`},
		{`<pre class="chroma"><code class="language-go"><span class="kd">func</span></code></pre>`,
			`<pre class="chroma"><code class="language-go"><span class="kd">func</span></code></pre>`},
		{`<span class="evil">x</span>`, `<span>x</span>`},
		{`<details open><summary>s</summary>b</details>`, `<details open=""><summary>s</summary>b</details>`},
		{`<svg><script>x</script><text>t</text></svg><math><mi>x</mi></math>`, ``},
		{`<div align="center" data-x="1"><kbd>K</kbd><sub>2</sub></div>`, `<div align="center"><kbd>K</kbd><sub>2</sub></div>`},
		{`<input type="checkbox" checked disabled>`, `<input type="checkbox" checked="" disabled="">`},
		{`<!-- c --><base href="https://e.org"><form><input></form>`, ``},
	}
	for _, tc := range tests {
		got, err := p.Sanitize([]byte(tc.in))
		if bad := unsafe(got.String()); bad != "" {
			t.Errorf("Sanitize(%s) lets %s through", tc.in, bad)
		}
		if err != nil || got.String() != tc.want {
			t.Errorf("Sanitize(%s)\n got %s\nwant %s", tc.in, got, tc.want)
		}
	}
	if _, err := p.Sanitize(make([]byte, maxInput+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the cap: %v", err)
	}
	if got := Text("<b>&</b>").String(); got != "<pre>&lt;b&gt;&amp;&lt;/b&gt;</pre>" {
		t.Errorf("Text = %s", got)
	}
}

func safeURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "" || u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto")
}

func FuzzSanitizeFixpoint(f *testing.F) {
	for _, s := range []string{`<a href="javascript:x">`, `<img src=x onerror=y>`, `<svg><p><style><img src=x onerror=alert(1)>`,
		`<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>`, `<details open><summary>x`, `<a href="https://e.org">`} {
		f.Add(s)
	}
	p := New()
	f.Fuzz(func(t *testing.T, in string) {
		once, err := p.Sanitize([]byte(in))
		if err != nil {
			return
		}
		twice, _ := p.Sanitize([]byte(once.String()))
		if once != twice {
			t.Fatalf("not a fixpoint:\n%q\n%q", once, twice)
		}
		if bad := unsafe(once.String()); bad != "" {
			t.Fatalf("output %q lets %s through", once, bad)
		}
	})
}
