package san

import (
	"bytes"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

// SafeHTML is HTML that has passed Sanitize. Only this package constructs it.
type SafeHTML struct{ s string }

func (h SafeHTML) String() string { return h.s }

var (
	reLang    = regexp.MustCompile(`^language-[A-Za-z0-9_+#.-]{1,32}$`)
	reClass   = regexp.MustCompile(`^(chroma|highlight|task-list-item|contains-task-list|footnotes|footnote-ref|footnote-backref|markdown-alert markdown-alert-(note|tip|important|warning|caution)|markdown-alert-title|[a-z]{1,3}[0-9]?)$`)
	reID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:.-]{0,127}$`)
	reAlign   = regexp.MustCompile(`^(left|right|center|justify)$`)
	reNum     = regexp.MustCompile(`^[0-9]{1,4}%?$`)
	reRel     = regexp.MustCompile(`^[a-z ]{1,64}$`)
	reLoading = regexp.MustCompile(`^(lazy|eager)$`)
)

func Policy() *bluemonday.Policy {
	p := bluemonday.NewPolicy() // empty: deny by default (not UGCPolicy)

	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6", "p", "br", "hr", "blockquote",
		"pre", "code", "em", "strong", "b", "i", "del", "s", "ins", "sup", "sub", "kbd", "mark",
		"details", "summary", "ul", "ol", "li", "dl", "dt", "dd", "table", "thead", "tbody",
		"tfoot", "tr", "caption", "figure", "figcaption", "abbr", "cite", "q", "small", "time",
		"section", "div", "span", "picture")
	p.AllowElements("a", "img", "source", "th", "td", "input")

	p.AllowAttrs("class").Matching(reLang).OnElements("code", "pre")
	p.AllowAttrs("class").Matching(reClass).OnElements("div", "span", "pre", "code", "ul", "ol", "li", "p", "section", "a", "table")
	p.AllowAttrs("id").Matching(reID).Globally()
	p.AllowAttrs("lang", "dir", "title").Matching(regexp.MustCompile(`^[\p{L}\p{N} ._,:;()'/!?-]{0,200}$`)).Globally()
	p.AllowAttrs("align").Matching(reAlign).OnElements("p", "div", "th", "td", "img", "table")
	p.AllowAttrs("colspan", "rowspan", "start").Matching(reNum).OnElements("th", "td", "ol")
	p.AllowAttrs("open").Matching(regexp.MustCompile(`^(open|)$`)).OnElements("details")
	p.AllowAttrs("datetime").Matching(regexp.MustCompile(`^[0-9TZ:+.-]{4,40}$`)).OnElements("time")

	// links
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("rel").Matching(reRel).OnElements("a")
	p.AllowStandardURLs()                 // http, https, mailto + relative URLs; RequireParseableURLs
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoReferrerOnLinks(true)
	p.RequireNoFollowOnLinks(false)
	p.AddTargetBlankToFullyQualifiedLinks(true) // adds target=_blank + rel=noopener

	// images
	p.AllowAttrs("src", "srcset").OnElements("img", "source")
	p.AllowAttrs("alt").Matching(regexp.MustCompile(`^.{0,300}$`)).OnElements("img")
	p.AllowAttrs("width", "height").Matching(reNum).OnElements("img")
	p.AllowAttrs("loading").Matching(reLoading).OnElements("img")
	p.AllowAttrs("media", "type").Matching(regexp.MustCompile(`^[a-z0-9 ()/:+.,-]{1,100}$`)).OnElements("source")

	// task lists: only disabled checkboxes
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(checked|disabled|)$`)).OnElements("input")

	p.SkipElementsContent("script", "style", "iframe", "object", "embed", "noscript", "template", "textarea", "title", "xmp", "noembed", "noframes", "svg", "math")
	return p
}

var policy = Policy()

func Render(src []byte) (SafeHTML, error) { return RenderWith(src, RepoResolver{Repo: "demo", Ref: "main"}) }

func RenderWith(src []byte, res Resolver) (SafeHTML, error) {
	var buf bytes.Buffer
	p := parser.New(parser.WithExtensions(extension.GFMParser, extension.FootnoteParser), parser.WithAutoHeadingID())
	r := html.New(html.WithUnsafe(), html.WithExtensions(extension.NewTableHTMLRenderer(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)), extension.NewStrikethroughHTMLRenderer(), extension.NewTaskListItemHTMLRenderer(), extension.FootnoteHTMLRenderer))
	doc := p.Parse(src)
	if err := r.Render(&buf, src, doc); err != nil {
		return SafeHTML{}, err
	}
	rw, err := Rewrite(buf.String(), res)
	if err != nil {
		return SafeHTML{}, err
	}
	return SafeHTML{policy.Sanitize(rw)}, nil
}
