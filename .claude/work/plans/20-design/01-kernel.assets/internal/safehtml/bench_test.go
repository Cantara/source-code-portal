package safehtml_test

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/safehtml"
)

// readme is a typical rendered README: headings, code with chroma spans, a table, links and images.
var readme = strings.Repeat(`<h2 id="doc-usage">Usage</h2><p>Install with <code>go get</code> and read the <a href="https://example.org/docs">docs</a>.</p>
<pre class="chroma"><code class="language-go"><span class="line"><span class="cl"><span class="kd">func</span> <span class="nf">main</span><span class="p">()</span> <span class="p">{}</span></span></span></code></pre>
<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td><img src="/media/acme/main/x.png" alt="x"></td></tr></tbody></table>
<ul><li><a href="/repos/other">other</a></li><li>item</li></ul>
`, 100)

func BenchmarkSanitizeReadme(b *testing.B) {
	p := safehtml.DocsPolicy()
	in := []byte(readme)
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	for b.Loop() {
		_ = safehtml.Sanitize(p, in)
	}
}
