package docs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	xhtml "golang.org/x/net/html"

	"github.com/cantara/source-code-portal/internal/safehtml"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/untrusted"
)

// pipelineVersion is part of every stored document; a different value renders the stored source again. Bump it whenever the output
// for the same input changes (TestPipelineLock).
const pipelineVersion = 3

const (
	maxDocBytes      = 512 << 10 // rendered; the rest is cut at a line boundary
	maxFrontMatter   = 16 << 10
	maxNesting       = 64
	maxInlineStream  = 40 << 10
	maxStreamSquares = 2e9
	maxHighlight     = 4 << 10 // bytes per code block
	highlightBudget  = 1500 * time.Millisecond
	maxTitle         = 120
	maxDescription   = 300
	maxTOCText       = 120
)

// links is what a document's links and images resolve against.
type links struct {
	web     string // https://github.com/
	repo    snapshot.RepoKey
	dir     string           // the document's directory, "" at the root
	indexed map[string]bool  // repository paths with a portal page
	images  map[string]Image // filled with the images the document shows, by id
}

// rendered is the outcome of the pipeline for one file.
type rendered struct {
	Title, Description string
	Order              int
	Hidden, Truncated  bool
	Format             string
	HTML               safehtml.SafeHTML
	Source             []byte
	TOC                []Heading
	State              snapshot.State
}

// pipeline renders documents: decode, front matter, truncate, shape guard, goldmark, link and image rewrite, chroma, sanitise.
type pipeline struct {
	policy *safehtml.Policy
}

// render runs the pipeline for the file at p (a repository path) with content b.
func (r *pipeline) render(p string, b []byte, l links) rendered {
	name := path.Base(p)
	b, ok := decodeText(b)
	if !ok {
		return rendered{Title: name, Format: "text", State: snapshot.StateUnsupported}
	}
	out := rendered{Title: name, Format: format(p, b), Order: noOrder, State: snapshot.StateOK}
	if len(b) > maxDocBytes {
		cut := bytes.LastIndexByte(b[:maxDocBytes], '\n')
		if cut < 0 {
			cut = maxDocBytes
		}
		for !utf8.Valid(b[:cut]) {
			cut--
		}
		b, out.Truncated = b[:cut], true
	}
	out.Source = b
	body := b
	if out.Format == "markdown" {
		var fm frontMatter
		fm, body = splitFrontMatter(b)
		out.Order, out.Hidden = fm.order, fm.hidden
		if !shapeOK(body) {
			out.Format = "text"
		}
		if out.Format == "markdown" {
			h, title, desc, toc, err := r.markdown(body, l)
			if err == nil {
				out.HTML, out.TOC = h, toc
				out.Title, out.Description = first(fm.title, title, name), first(fm.description, desc)
				return out
			}
			out.Format = "text"
		}
		out.Title, out.Description = first(fm.title, name), fm.description
	}
	out.HTML = safehtml.Text(string(body))
	return out
}

const noOrder = 1 << 30

func first(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// decodeText strips a BOM and replaces invalid UTF-8. UTF-16 and binary input are not text.
func decodeText(b []byte) ([]byte, bool) {
	if bytes.HasPrefix(b, []byte{0xfe, 0xff}) || bytes.HasPrefix(b, []byte{0xff, 0xfe}) {
		return nil, false
	}
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	head := b[:min(len(b), 8<<10)]
	if bytes.Count(head, []byte{0}) > len(head)/100 {
		return nil, false
	}
	if !utf8.Valid(b) {
		b = bytes.ToValidUTF8(b, []byte("\ufffd"))
	}
	return b, true
}

// format is markdown for .md and .markdown, and for an extensionless README that looks like Markdown; text otherwise.
func format(p string, b []byte) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "markdown"
	case "":
		head := string(b[:min(len(b), 4<<10)])
		if strings.HasPrefix(head, "#") || strings.Contains(head, "```") || strings.Contains(head, "](") ||
			strings.Contains(head, "\n- ") || strings.Contains(head, "\n* ") {
			return "markdown"
		}
	}
	return "text"
}

// shapeOK is a linear check that keeps goldmark's time linear: bounded nesting, bounded inline streams (paragraph-like runs of
// non-blank lines) and a bounded sum of their squared lengths.
func shapeOK(b []byte) bool {
	var stream, squares float64
	for line := range bytes.Lines(b) {
		depth := 0
		for _, c := range line {
			switch c {
			case '>', '[', '(', '*', '_', '`', '<':
				depth++
			case ' ', '\t', '-', '+':
			default:
				goto counted
			}
		}
	counted:
		if depth > maxNesting {
			return false
		}
		if t := bytes.TrimSpace(line); len(t) == 0 || blockStart(t) {
			squares += stream * stream
			stream = 0
		}
		if stream += float64(len(line)); stream > maxInlineStream {
			return false
		}
	}
	return squares+stream*stream <= maxStreamSquares
}

// blockStart reports whether a trimmed line starts a block (list item, heading, table row, quote, fence), which ends an inline stream.
func blockStart(t []byte) bool {
	switch t[0] {
	case '-', '*', '+', '#', '|', '>', '`', '~', '<':
		return true
	}
	i := 0
	for i < len(t) && i < 9 && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i < len(t) && (t[i] == '.' || t[i] == ')')
}

// markdown renders body to sanitised HTML and returns the first H1 text, the first paragraph text and the TOC.
func (r *pipeline) markdown(body []byte, l links) (safehtml.SafeHTML, string, string, []Heading, error) {
	hl := &highlighter{deadline: time.Now().Add(highlightBudget)}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(alerts{}, 100))),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe(), renderer.WithNodeRenderers(util.Prioritized(hl, 100))),
	)
	var buf bytes.Buffer
	if err := md.Convert(body, &buf); err != nil {
		return safehtml.SafeHTML{}, "", "", nil, err
	}
	rw, err := rewrite(buf.Bytes(), l)
	if err != nil {
		return safehtml.SafeHTML{}, "", "", nil, err
	}
	h, err := r.policy.Sanitize(rw.html)
	return h, rw.title, rw.description, rw.toc, err
}

// highlighter renders fenced code with chroma token classes, within a per-block size and a per-document time budget.
type highlighter struct{ deadline time.Time }

func (h *highlighter) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, h.render)
}

func (h *highlighter) render(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fc := n.(*ast.FencedCodeBlock) //nolint:forcetypeassert // registered for this kind only
	var code bytes.Buffer
	for i := range fc.Lines().Len() {
		seg := fc.Lines().At(i)
		code.Write(seg.Value(src))
	}
	lang := ""
	if l := string(fc.Language(src)); validLanguage(l) {
		lang = l
	}
	if lang == "" {
		_, _ = w.WriteString("<pre><code>")
	} else {
		_, _ = fmt.Fprintf(w, `<pre class="chroma"><code class="language-%s">`, lang)
	}
	if lexer := lexers.Get(lang); lang != "" && lang != "mermaid" && lexer != nil && code.Len() <= maxHighlight && time.Now().Before(h.deadline) {
		if it, err := lexer.Tokenise(nil, code.String()); err == nil {
			var out bytes.Buffer
			if chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true)).Format(&out, styles.Fallback, it) == nil {
				_, _ = w.Write(out.Bytes())
				_, _ = w.WriteString("</code></pre>\n")
				return ast.WalkSkipChildren, nil
			}
		}
	}
	_, _ = w.WriteString(html.EscapeString(code.String()))
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

func validLanguage(l string) bool {
	if l == "" || len(l) > 32 {
		return false
	}
	for _, c := range l {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("_+#.-", c) {
			return false
		}
	}
	return true
}

// alerts turns a blockquote that starts with [!NOTE], [!TIP], [!IMPORTANT], [!WARNING] or [!CAUTION] into a GitHub alert:
// class md-alert md-alert-<kind> and a p.md-alert-title.
type alerts struct{}

func (alerts) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		bq, ok := n.(*ast.Blockquote)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		p, ok := bq.FirstChild().(*ast.Paragraph)
		if !ok || p.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		line := p.Lines().At(0)
		marker := strings.TrimSpace(string(line.Value(src)))
		kind := ""
		for _, k := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
			if strings.EqualFold(marker, "[!"+k+"]") {
				kind = k
			}
		}
		if kind == "" {
			return ast.WalkContinue, nil
		}
		for c := p.FirstChild(); c != nil; {
			next := c.NextSibling()
			p.RemoveChild(p, c)
			if t, ok := c.(*ast.Text); ok && (t.SoftLineBreak() || t.HardLineBreak()) {
				break
			}
			c = next
		}
		if p.ChildCount() == 0 {
			bq.RemoveChild(bq, p)
		}
		title := ast.NewParagraph()
		title.SetAttributeString("class", "md-alert-title")
		title.AppendChild(title, ast.NewString([]byte(kind[:1]+strings.ToLower(kind[1:]))))
		bq.InsertBefore(bq, bq.FirstChild(), title)
		bq.SetAttributeString("class", "md-alert md-alert-"+strings.ToLower(kind))
		return ast.WalkSkipChildren, nil
	})
}

// rewritten is the output of rewrite.
type rewritten struct {
	html               []byte
	title, description string
	toc                []Heading
}

// rewrite resolves links and images, prefixes ids with doc-, drops what the policy would strip anyway in a way that keeps the text
// (images without a usable source become their alt text), and collects the first H1, the first paragraph and the h2/h3 headings. It
// works on the HTML, so raw HTML in Markdown is treated like rendered Markdown.
func rewrite(in []byte, l links) (rewritten, error) {
	var out rewritten
	var buf bytes.Buffer
	z := xhtml.NewTokenizer(bytes.NewReader(in))
	var capture *strings.Builder // text of the heading or paragraph being collected
	var captureTag, captureID string
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				out.html = buf.Bytes()
				return out, nil
			}
			return out, z.Err()
		}
		tok := z.Token()
		switch tt {
		case xhtml.CommentToken, xhtml.DoctypeToken:
			continue
		case xhtml.TextToken:
			if capture != nil {
				capture.WriteString(tok.Data)
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			as, keep := rewriteAttrs(tok.Data, tok.Attr, l)
			if !keep {
				if tok.Data == "img" {
					buf.WriteString(html.EscapeString(attr(tok.Attr, "alt")))
				}
				continue
			}
			tok.Attr = as
			if capture == nil && tt == xhtml.StartTagToken && (tok.Data == "h1" && out.title == "" || tok.Data == "h2" || tok.Data == "h3" ||
				tok.Data == "p" && out.description == "") {
				capture, captureTag, captureID = &strings.Builder{}, tok.Data, attr(tok.Attr, "id")
			}
		case xhtml.EndTagToken:
			if tok.Data == "source" || tok.Data == "picture" {
				continue
			}
			if capture != nil && tok.Data == captureTag {
				t := untrusted.Text(capture.String(), maxTOCText+1)
				switch captureTag {
				case "h1":
					out.title = untrusted.Text(t, maxTitle)
				case "p":
					out.description = untrusted.Text(capture.String(), maxDescription)
				default:
					if captureID != "" && t != "" {
						out.toc = append(out.toc, Heading{Level: int(captureTag[1] - '0'), ID: captureID, Text: untrusted.Text(t, maxTOCText)})
					}
				}
				capture = nil
			}
		}
		buf.WriteString(tok.String())
	}
}

func attr(as []xhtml.Attribute, key string) string {
	for _, a := range as {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// rewriteAttrs returns the attributes of one start tag, and false when the tag is dropped.
func rewriteAttrs(tag string, as []xhtml.Attribute, l links) ([]xhtml.Attribute, bool) {
	switch tag {
	case "source", "picture":
		return nil, false
	case "input":
		if !strings.EqualFold(attr(as, "type"), "checkbox") {
			return nil, false
		}
	}
	out := as[:0:0]
	external := false
	for _, a := range as {
		switch a.Key {
		case "name", "srcset", "rel", "loading", "referrerpolicy":
			continue
		case "id":
			if !strings.HasPrefix(a.Val, "doc-") {
				a.Val = "doc-" + a.Val
			}
		case "href":
			if tag != "a" {
				continue
			}
			if a.Val, external = l.link(a.Val); a.Val == "" {
				continue
			}
		case "src":
			if tag != "img" {
				continue
			}
			if a.Val = l.image(a.Val); a.Val == "" {
				return nil, false
			}
		}
		out = append(out, a)
	}
	switch {
	case tag == "img" && attr(out, "src") == "":
		return nil, false
	case tag == "img":
		if !slices.ContainsFunc(out, func(a xhtml.Attribute) bool { return a.Key == "alt" }) {
			out = append(out, xhtml.Attribute{Key: "alt"})
		}
		out = append(out, xhtml.Attribute{Key: "loading", Val: "lazy"}, xhtml.Attribute{Key: "referrerpolicy", Val: "no-referrer"})
	case tag == "a" && external:
		out = append(out, xhtml.Attribute{Key: "rel", Val: "noopener noreferrer nofollow ugc"})
	}
	return out, true
}

// link resolves an href: fragments get the doc- prefix, indexed documents their portal page, other repository paths their GitHub
// page; http, https and mailto stay. external is true for links that leave the portal.
func (l links) link(href string) (string, bool) {
	href = strings.TrimSpace(href)
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		if frag == "" {
			return "", false
		}
		if !strings.HasPrefix(frag, "doc-") {
			frag = "doc-" + frag
		}
		return "#" + frag, false
	}
	u, err := url.Parse(href)
	switch {
	case err != nil || href == "":
		return "", false
	case u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto":
		return u.String(), true
	case u.Scheme != "" || u.Host != "":
		return "", false
	}
	p, dir, ok := l.resolve(u.Path)
	if !ok {
		return "", false
	}
	if p == "" {
		return l.web + string(l.repo), true
	}
	frag := ""
	if u.Fragment != "" {
		frag = "#" + u.Fragment
	}
	if l.indexed[p] && !dir {
		if frag != "" {
			frag = "#doc-" + strings.TrimPrefix(u.Fragment, "doc-")
		}
		return snapshot.DocPage(l.repo, p) + frag, false
	}
	kind := "blob"
	if dir {
		kind, p = "tree", p+"/"
	}
	return l.web + string(l.repo) + "/" + kind + "/HEAD/" + escapePath(p) + frag, true
}

// image resolves an img src to its address on the portal and records the image: a repository path, or an absolute https URL.
// Anything else is "".
func (l links) image(src string) string {
	u, err := url.Parse(strings.TrimSpace(src))
	var img Image
	switch {
	case err != nil || src == "":
		return ""
	case u.Scheme == "https" && u.Host != "":
		img.URL = u.String()
	case u.Scheme != "" || u.Host != "":
		return ""
	default:
		p, dir, ok := l.resolve(u.Path)
		if !ok || dir || p == "" {
			return ""
		}
		img.Path = p
	}
	img.ID = imageID(img)
	if l.images != nil {
		l.images[img.ID] = img
	}
	return "/images/" + string(l.repo) + "/" + img.ID
}

// imageID names an image within its repository: 16 bytes of SHA-256 over its path or URL, hex.
func imageID(img Image) string {
	ref := img.URL
	if img.Path != "" {
		ref = "path:" + img.Path
	}
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:16])
}

// resolve turns a relative path into a repository path; a leading / is repository-relative. ok is false above the root.
func (l links) resolve(p string) (string, bool, bool) {
	dir := strings.HasSuffix(p, "/")
	if !strings.HasPrefix(p, "/") {
		p = l.dir + "/" + p
	}
	clean := path.Clean("/" + p)
	if strings.Contains(p, "..") {
		// path.Clean keeps the result at the root; refuse paths that tried to leave it.
		depth := 0
		for _, seg := range strings.Split(p, "/") {
			switch seg {
			case "", ".":
			case "..":
				if depth--; depth < 0 {
					return "", false, false
				}
			default:
				depth++
			}
		}
	}
	return strings.TrimPrefix(clean, "/"), dir, true
}

func escapePath(p string) string { return (&url.URL{Path: p}).EscapedPath() }
