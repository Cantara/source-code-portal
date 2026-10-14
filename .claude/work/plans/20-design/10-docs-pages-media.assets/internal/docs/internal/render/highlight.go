package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

var langRE = regexp.MustCompile(`^[A-Za-z0-9_+#.-]{1,32}$`)

// renderState is per Markdown() call: goldmark renderers are shared, so the highlight budget lives in a state built for each call.
type renderState struct {
	maxBlock int       // bytes: larger blocks are not highlighted (chroma lexers are quadratic: 0.6 s for 4 KiB, 2.4 s for 8 KiB, 10 s for 16 KiB, adversarial)
	deadline time.Time // total highlight budget of the document
	skipped  int       // blocks left plain because of size or budget (metric/notice)
}

type codeRenderer struct{ st *renderState }

func (r *codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.render)
}

var formatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))

func (r *codeRenderer) render(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fc := n.(*ast.FencedCodeBlock)
	var code bytes.Buffer
	for i := 0; i < fc.Lines().Len(); i++ {
		s := fc.Lines().At(i)
		code.Write(s.Value(src))
	}
	lang := string(fc.Language(src))
	if i := strings.IndexAny(lang, " \t{"); i >= 0 {
		lang = lang[:i]
	}
	if !langRE.MatchString(lang) {
		lang = ""
	}
	limit := r.st.maxBlock
	cls := ""
	if lang != "" {
		cls = ` class="language-` + lang + `"`
	}
	var lex chroma.Lexer
	if lang != "" && lang != "mermaid" {
		if code.Len() > limit || time.Now().After(r.st.deadline) {
			r.st.skipped++
		} else {
			lex = lexers.Get(lang)
		}
	}
	if lex == nil {
		_, _ = w.WriteString("<pre><code" + cls + ">" + html.EscapeString(code.String()) + "</code></pre>\n")
		return ast.WalkContinue, nil
	}
	plain := func() {
		_, _ = w.WriteString("<pre><code" + cls + ">" + html.EscapeString(code.String()) + "</code></pre>\n")
	}
	it, err := chroma.Coalesce(lex).Tokenise(nil, code.String())
	if err != nil {
		plain()
		return ast.WalkContinue, nil
	}
	var cut bool
	var hl bytes.Buffer
	if err := formatter.Format(&hl, styles.Fallback, budgeted(it, r.st.deadline, &cut)); err != nil || cut {
		r.st.skipped++
		plain() // the block ran out of budget: show it whole and unhighlighted rather than truncated
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<pre class="chroma"><code` + cls + `>`)
	_, _ = w.Write(hl.Bytes())
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkContinue, nil
}

// budgeted ends the token stream once the deadline has passed and records that it did.
func budgeted(it chroma.Iterator, deadline time.Time, cut *bool) chroma.Iterator {
	return func() chroma.Token {
		if time.Now().After(deadline) {
			*cut = true
			return chroma.EOF
		}
		return it()
	}
}
