// Package safehtml holds SafeHTML, HTML that passed the portal's one sanitiser policy, and that policy.
package safehtml

import (
	"errors"
	"html"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// PolicyVersion changes whenever the policy's output changes; stored HTML of another version is rendered again.
const PolicyVersion = 1

const maxInput = 8 << 20

// ErrTooLarge is returned for input over 8 MiB.
var ErrTooLarge = errors.New("safehtml: input over 8 MiB")

// SafeHTML is sanitised HTML. Sanitize and Text are its only constructors; the zero value is empty.
type SafeHTML struct{ s string }

// String returns the HTML.
func (h SafeHTML) String() string { return h.s }

// Text returns s as escaped text in one <pre>.
func Text(s string) SafeHTML { return SafeHTML{"<pre>" + html.EscapeString(s) + "</pre>"} }

// Policy is the sanitiser. Build one with New and share it; it is safe for concurrent use.
type Policy struct{ p *bluemonday.Policy }

// Sanitize returns b reduced to the policy's elements, attributes and URLs.
func (p *Policy) Sanitize(b []byte) (SafeHTML, error) {
	if len(b) > maxInput {
		return SafeHTML{}, ErrTooLarge
	}
	return SafeHTML{string(p.p.SanitizeBytes(b))}, nil
}

// classToken is one allowed class: chroma token classes, language-*, alerts, footnotes.
const classToken = //nolint:gosec // a class pattern, not a credential
`(?:chroma|line|cl|[a-z]{1,3}[0-9]?|language-[A-Za-z0-9_+#.-]{1,32}|md-alert(?:-[a-z]+)?|footnotes?|footnote-(?:ref|backref)|task-list-item)`

// New builds the policy (PolicyVersion 1): GitHub-flavoured Markdown output, highlighted code with classes, raw HTML that READMEs commonly
// use (details, kbd, sub, sup, aligned divs, sized images). Ids must carry the doc- prefix. Links are http, https, mailto or relative;
// images are https only. Everything else, including style, scripts, forms, embeds, svg and event handlers, is removed.
func New() *Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "code", "em", "strong", "del", "s", "b", "i",
		"u", "mark", "small", "kbd", "sub", "sup", "ul", "ol", "li", "dl", "dt", "dd", "table", "thead", "tbody", "tfoot", "tr", "caption",
		"details", "summary", "span", "div", "abbr", "cite", "q", "samp", "var", "ins", "figure", "figcaption")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`^(?i:left|center|right)$`)).OnElements("div", "p", "td", "th", "img", "h1", "h2", "h3")
	p.AllowAttrs("colspan", "rowspan").Matching(bluemonday.Integer).OnElements("td", "th")
	p.AllowElements("td", "th")
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	p.AllowAttrs("open").Matching(regexp.MustCompile(`^(?:|open)$`)).OnElements("details")
	p.AllowAttrs("title").Matching(bluemonday.Paragraph).OnElements("abbr", "a", "img")
	p.AllowAttrs("id").Matching(regexp.MustCompile(`^doc-[A-Za-z0-9_:.-]{1,100}$`)).Globally()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^` + classToken + `(?: ` + classToken + `)*$`)).Globally()
	p.AllowAttrs("role").Matching(regexp.MustCompile(`^doc-(?:noteref|backlink|endnotes)$`)).OnElements("a", "div", "section", "sup")
	p.AllowElements("section")

	p.SkipElementsContent("script", "style", "iframe", "object", "embed", "svg", "math", "form", "textarea", "select", "button", "noscript",
		"template", "audio", "video", "canvas")
	p.RequireParseableURLs(true)
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^noopener noreferrer nofollow ugc$`)).OnElements("a")
	p.AllowAttrs("src").Matching(regexp.MustCompile(`^https://`)).OnElements("img")
	p.AllowAttrs("alt").Matching(bluemonday.Paragraph).OnElements("img")
	p.AllowAttrs("width", "height").Matching(regexp.MustCompile(`^[0-9]{1,4}%?$`)).OnElements("img")
	p.AllowAttrs("loading").Matching(regexp.MustCompile(`^lazy$`)).OnElements("img")
	p.AllowAttrs("referrerpolicy").Matching(regexp.MustCompile(`^no-referrer$`)).OnElements("img")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(?:|checked|disabled)$`)).OnElements("input")
	return &Policy{p}
}
