package safehtml

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

var (
	reCodeClass = regexp.MustCompile(`^(chroma|language-[A-Za-z0-9_+#.-]{1,32})$`)
	reSpanClass = regexp.MustCompile(`^(line|[a-z]{1,3}[0-9]?)$`) // chroma token classes (verified against chroma v2.27.0)
	reAlert     = regexp.MustCompile(`^(md-alert( md-alert-(note|tip|important|warning|caution))?|footnotes)$`)
	reAlertHead = regexp.MustCompile(`^md-alert-title$`)
	reTaskList  = regexp.MustCompile(`^(contains-task-list|task-list-item)$`)
	reFootnote  = regexp.MustCompile(`^(footnote-ref|footnote-backref)$`)
	reID        = regexp.MustCompile(`^doc-[A-Za-z0-9_:.-]{1,100}$`)
	reRole      = regexp.MustCompile(`^doc-(noteref|backlink|endnotes|footnote)$`)
	reText      = regexp.MustCompile(`^[\p{L}\p{N} ._,:;()'/!?-]{0,200}$`)
	reAlign     = regexp.MustCompile(`^(left|right|center|justify)$`)
	reNum       = regexp.MustCompile(`^[0-9]{1,4}%?$`)
	reRel       = regexp.MustCompile(`^(noopener|noreferrer|nofollow|ugc)( (noopener|noreferrer|nofollow|ugc)){0,3}$`)
	reLoading   = regexp.MustCompile(`^(lazy|eager)$`)
	reAlt       = regexp.MustCompile(`^.{0,300}$`)
	reOpen      = regexp.MustCompile(`^(open|)$`)
	reDatetime  = regexp.MustCompile(`^[0-9TZ:+.-]{4,40}$`)
	reCheckbox  = regexp.MustCompile(`^checkbox$`)
	reFlag      = regexp.MustCompile(`^(checked|disabled|)$`)
	reMedia     = regexp.MustCompile(`^[a-z0-9 ()/:+.,-]{1,100}$`)
	// href: a relative reference (no scheme: no ':' before the first '/', '?' or '#'), http(s) with a non-empty host, or mailto.
	// Anything else is dropped: "https:///host" and "https:host" are parsed by browsers as absolute links yet look
	// relative to the sanitiser, which would then skip the rel and target rules of fully-qualified links (T4).
	reHref = regexp.MustCompile(`(?i)^(?:https?://[^/\\\s?#][^\s]*|mailto:[^\s]*|[^:/?#\s]*(?:[/?#][^\s]*)?)$`)
	// Images are same-origin only: img src must be a root-relative URL ("/media/..."), never "//host" or a scheme URL.
	reLocalSrc = regexp.MustCompile(`^/(?:[^/\\\s"'<>][^\s"'<>]*)?$`)
	// srcset: one to ten candidates, each a root-relative URL without spaces or commas, optional "1x"/"640w".
	reSrcset = regexp.MustCompile(`^/[^/\\\s,"'<>][^\s,"'<>]*(?: [0-9]+(?:\.[0-9]+)?[wx])?(?:, ?/[^/\\\s,"'<>][^\s,"'<>]*(?: [0-9]+(?:\.[0-9]+)?[wx])?){0,9}$`)
)

// DocsPolicy builds the allow-list for rendered documents. It starts from an empty (deny-all) policy; see the design
// document for the element, attribute and class tables. Call it once and share the result.
func DocsPolicy() *Policy {
	p := bluemonday.NewPolicy()

	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6", "p", "br", "hr", "blockquote", "pre", "code", "em", "strong", "b", "i",
		"u", "del", "s", "ins", "sup", "sub", "kbd", "samp", "var", "mark", "details", "summary", "ul", "ol", "li", "dl", "dt", "dd",
		"table", "thead", "tbody", "tfoot", "tr", "caption", "figure", "figcaption", "abbr", "cite", "q", "small", "time",
		"section", "div", "span", "picture", "a", "img", "source", "th", "td", "input")

	p.AllowAttrs("class").Matching(reCodeClass).OnElements("pre", "code")
	p.AllowAttrs("class").Matching(reSpanClass).OnElements("span")
	p.AllowAttrs("class").Matching(reAlert).OnElements("div")
	p.AllowAttrs("class").Matching(reAlertHead).OnElements("p")
	p.AllowAttrs("class").Matching(reTaskList).OnElements("ul", "ol", "li")
	p.AllowAttrs("class").Matching(reFootnote).OnElements("a")
	p.AllowAttrs("id").Matching(reID).Globally()
	p.AllowAttrs("role").Matching(reRole).OnElements("div", "a", "section")
	p.AllowAttrs("lang", "dir", "title").Matching(reText).Globally()
	p.AllowAttrs("align").Matching(reAlign).OnElements("p", "div", "th", "td", "img", "table")
	p.AllowAttrs("colspan", "rowspan", "start").Matching(reNum).OnElements("th", "td", "ol")
	p.AllowAttrs("open").Matching(reOpen).OnElements("details")
	p.AllowAttrs("datetime").Matching(reDatetime).OnElements("time")

	p.AllowAttrs("href").Matching(reHref).OnElements("a")
	p.AllowAttrs("rel").Matching(reRel).OnElements("a")
	// Not AllowStandardURLs(): it also switches on rel=nofollow for every link, relative ones included.
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(true)
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoReferrerOnFullyQualifiedLinks(true)
	p.RequireNoFollowOnFullyQualifiedLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true) // adds target=_blank and rel=noopener

	p.AllowAttrs("src").Matching(reLocalSrc).OnElements("img", "source")
	p.AllowAttrs("srcset").Matching(reSrcset).OnElements("img", "source")
	p.AllowAttrs("alt").Matching(reAlt).OnElements("img")
	p.AllowAttrs("width", "height").Matching(reNum).OnElements("img")
	p.AllowAttrs("loading").Matching(reLoading).OnElements("img")
	p.AllowAttrs("media", "type").Matching(reMedia).OnElements("source")

	p.AllowAttrs("type").Matching(reCheckbox).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(reFlag).OnElements("input")

	p.SkipElementsContent("script", "style", "iframe", "object", "embed", "noscript", "template", "textarea", "title",
		"xmp", "noembed", "noframes", "svg", "math", "frame", "frameset", "nostyle", "head", "select", "option", "button", "form")
	return &Policy{p: p}
}
