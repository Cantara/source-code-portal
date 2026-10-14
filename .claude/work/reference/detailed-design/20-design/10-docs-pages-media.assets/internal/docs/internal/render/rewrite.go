package render

import (
	"bytes"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type passOptions struct{ bridge bool }

type passResult struct {
	html               string
	toc                []TOCEntry
	links              []Link
	words              int
	title, description string
}

const maxDepth = 400

var dropWithContent = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true, atom.Embed: true, atom.Noscript: true, atom.Template: true,
	atom.Textarea: true, atom.Title: true, atom.Xmp: true, atom.Noembed: true, atom.Noframes: true, atom.Svg: true, atom.Math: true,
	atom.Head: true, atom.Form: true, atom.Select: true, atom.Button: true, atom.Audio: true, atom.Video: true, atom.Canvas: true,
	atom.Link: true, atom.Meta: true, atom.Base: true, atom.Frame: true, atom.Frameset: true, atom.Applet: true, atom.Dialog: true,
	atom.Track: true, atom.Map: true, atom.Area: true,
}

func rewriteHTML(in string, r Resolver, opt passOptions) (passResult, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(in), ctx)
	if err != nil {
		return passResult{}, err
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	if opt.bridge {
		if art := findArticle(root); art != nil {
			root = art
		}
	}
	st := &passState{r: r, opt: opt, slug: newSlugger()}
	st.clean(root, 0)         // phase 1: structure, drop, unwrap, ids
	st.assignHeadingIDs(root) // phase 2: ids in document order
	st.collectIDs(root)
	st.rewriteRefs(root) // phase 3: links, images, fragments
	st.finish(root)
	var buf bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&buf, c); err != nil {
			return passResult{}, err
		}
	}
	return passResult{html: buf.String(), toc: st.toc, links: st.links, words: st.words, title: st.title, description: st.desc}, nil
}

type passState struct {
	r     Resolver
	opt   passOptions
	slug  *slugger
	ids   map[string]string // lower-case id -> id
	toc   []TOCEntry
	links []Link
	words int
	title string
	desc  string
}

func findArticle(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Article {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if a := findArticle(c); a != nil {
			return a
		}
	}
	return nil
}

func (s *passState) clean(n *html.Node, depth int) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch {
		case c.Type == html.CommentNode || c.Type == html.DoctypeNode || c.Type == html.RawNode:
			n.RemoveChild(c)
		case c.Type == html.ElementNode:
			if depth > maxDepth || dropWithContent[c.DataAtom] {
				n.RemoveChild(c)
			} else if strings.Contains(c.Data, "-") && c.Namespace == "" { // custom element: unwrap (g-emoji, markdown-accessiblity-table, math-renderer)
				first := c.FirstChild
				unwrap(n, c)
				if first != nil {
					next = first // children are re-examined at their new position
				}
			} else {
				s.cleanAttrs(c)
				s.clean(c, depth+1)
			}
		}
		c = next
	}
}

func unwrap(parent, el *html.Node) {
	for el.FirstChild != nil {
		ch := el.FirstChild
		el.RemoveChild(ch)
		parent.InsertBefore(ch, el)
	}
	parent.RemoveChild(el)
}

func (s *passState) cleanAttrs(n *html.Node) {
	keep := n.Attr[:0]
	for _, a := range n.Attr {
		k := strings.ToLower(a.Key)
		if a.Namespace != "" || strings.HasPrefix(k, "on") || k == "style" || k == "name" || k == "srcdoc" || k == "formaction" || k == "ping" {
			continue
		}
		if strings.HasPrefix(k, "data-") && !(s.opt.bridge && k == "data-canonical-src") {
			continue
		}
		a.Key = k
		keep = append(keep, a)
	}
	n.Attr = keep
	if n.DataAtom == atom.Input {
		if get(n, "type") != "checkbox" {
			n.Attr = nil
			n.Data, n.DataAtom = "span", atom.Span
			return
		}
		set(n, "disabled", "")
	}
}

func get(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}
func has(n *html.Node, k string) bool {
	for _, a := range n.Attr {
		if a.Key == k {
			return true
		}
	}
	return false
}
func set(n *html.Node, k, v string) {
	for i, a := range n.Attr {
		if a.Key == k {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: k, Val: v})
}
func del(n *html.Node, k string) {
	for i, a := range n.Attr {
		if a.Key == k {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return
		}
	}
}

func isHeading(n *html.Node) bool {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return n.Type == html.ElementNode
	}
	return false
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// assignHeadingIDs gives every heading an id in document order: an author id is kept, otherwise github-slugger(text) is used.
// In bridge mode GitHub's "user-content-" ids and permalink anchors are normalised first.
func (s *passState) assignHeadingIDs(root *html.Node) {
	if s.opt.bridge {
		s.normaliseGitHubAnchors(root)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if isHeading(c) {
				if id := get(c, "id"); id != "" {
					s.slug.put(safeID(id))
				}
			}
			walk(c)
		}
	}
	walk(root)
	var assign func(*html.Node)
	assign = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if isHeading(c) && get(c, "id") == "" {
				set(c, "id", s.slug.next(textOf(c)))
			}
			assign(c)
		}
	}
	assign(root)
}

func (s *passState) normaliseGitHubAnchors(root *html.Node) {
	var anchors []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if c.DataAtom == atom.A && strings.HasPrefix(get(c, "id"), "user-content-") {
					anchors = append(anchors, c)
				}
				walk(c)
			}
		}
	}
	walk(root)
	for _, a := range anchors {
		id := strings.TrimPrefix(get(a, "id"), "user-content-")
		var target *html.Node
		if isHeading(a.Parent) {
			target = a.Parent
		} else if p := a.PrevSibling; p != nil && isHeading(p) {
			target = p
		} else if p := a.Parent; p != nil {
			for c := p.FirstChild; c != nil; c = c.NextSibling {
				if isHeading(c) {
					target = c
					break
				}
			}
		}
		if target != nil && !has(target, "id") && id != "" {
			set(target, "id", id)
		}
		if strings.Contains(get(a, "class"), "anchor") || a.FirstChild == nil || textOf(a) == "" {
			a.Parent.RemoveChild(a)
		} else {
			del(a, "id")
		}
	}
	// strip the prefix from any remaining GitHub ids
	var strip func(*html.Node)
	strip = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if v := get(c, "id"); strings.HasPrefix(v, "user-content-") {
					set(c, "id", strings.TrimPrefix(v, "user-content-"))
				}
				if v := get(c, "href"); c.DataAtom == atom.A && strings.HasPrefix(v, "#user-content-") {
					set(c, "href", "#"+strings.TrimPrefix(v, "#user-content-"))
				}
				strip(c)
			}
		}
	}
	strip(root)
}

func safeID(v string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.M, r) || r == '_' || r == ':' || r == '.' || r == '-' {
			return r
		}
		return '-'
	}, v)
}

func (s *passState) collectIDs(root *html.Node) {
	s.ids = map[string]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if id := get(c, "id"); id != "" {
				ns := NamespaceID(safeID(id))
				set(c, "id", truncRunes(ns, 124))
				if _, dup := s.ids[strings.ToLower(ns)]; !dup {
					s.ids[strings.ToLower(ns)] = ns
				}
			}
			walk(c)
		}
	}
	walk(root)
	for _, h := range headingsOf(root) {
		s.toc = append(s.toc, TOCEntry{Depth: int(h.Data[1] - '0'), Text: truncRunes(textOf(h), 200), ID: get(h, "id")})
		if h.DataAtom == atom.H1 && s.title == "" {
			s.title = truncRunes(textOf(h), 200)
		}
	}
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func headingsOf(root *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if isHeading(c) {
					out = append(out, c)
				}
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

func (s *passState) fragment(frag string) string {
	if dec, err := url.PathUnescape(frag); err == nil {
		frag = dec
	}
	ns := NamespaceID(safeID(frag))
	if _, ok := s.ids[strings.ToLower(ns)]; ok {
		if exact, ok := s.ids[strings.ToLower(ns)]; ok {
			return "#" + exact
		}
	}
	return "#" + ns
}

func (s *passState) rewriteRefs(root *html.Node) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode {
				switch c.DataAtom {
				case atom.A:
					s.rewriteA(n, c)
				case atom.Img:
					if s.rewriteImg(n, c) {
						c = next
						continue
					}
				case atom.Source:
					if !s.rewriteSource(n, c) {
						c = next
						continue
					}
				case atom.Table:
					w := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div, Attr: []html.Attribute{{Key: "class", Val: "md-table"}}}
					if n.DataAtom != atom.Div || get(n, "class") != "md-table" {
						n.InsertBefore(w, c)
						n.RemoveChild(c)
						w.AppendChild(c)
					}
				}
				if c.Parent != nil {
					walk(c)
				}
			}
			c = next
		}
	}
	walk(root)
}

func (s *passState) rewriteA(parent, a *html.Node) {
	href := strings.TrimSpace(get(a, "href"))
	del(a, "target")
	del(a, "rel")
	switch {
	case href == "":
		del(a, "href")
	case strings.HasPrefix(href, "#"):
		set(a, "href", s.fragment(strings.TrimPrefix(href, "#")))
	default:
		if s.opt.bridge {
			href = deCamo(href)
		}
		out, external, ok := s.r.Link(href)
		if !ok {
			del(a, "href")
		} else {
			set(a, "href", out)
			if external {
				set(a, "rel", "noopener noreferrer nofollow ugc")
				set(a, "target", "_blank")
				s.addLink(a, Link{Text: textOf(a), Href: out, Kind: LinkExternal})
			} else {
				s.addLink(a, Link{Text: textOf(a), Href: out, Kind: LinkDoc})
			}
		}
	}
	if !has(a, "href") && a.Parent != nil { // an <a> without a destination is just its content
		unwrapKeepPos(a)
	}
}

func unwrapKeepPos(a *html.Node) {
	p := a.Parent
	for a.FirstChild != nil {
		ch := a.FirstChild
		a.RemoveChild(ch)
		p.InsertBefore(ch, a)
	}
	p.RemoveChild(a)
}

func (s *passState) addLink(_ *html.Node, l Link) {
	if len(s.links) < 500 {
		l.Text = truncRunes(l.Text, 120)
		s.links = append(s.links, l)
	}
}

// rewriteImg returns true when the element was removed.
func (s *passState) rewriteImg(parent, img *html.Node) bool {
	raw := get(img, "src")
	if s.opt.bridge {
		if c := get(img, "data-canonical-src"); c != "" {
			raw = c
		}
	}
	if s.opt.bridge {
		raw = deCamo(raw)
	}
	del(img, "data-canonical-src")
	del(img, "srcset")
	del(img, "sizes")
	del(img, "longdesc")
	out, ok := s.r.Image(raw)
	if !ok {
		if alt := strings.TrimSpace(get(img, "alt")); alt != "" {
			parent.InsertBefore(&html.Node{Type: html.TextNode, Data: alt}, img)
		}
		parent.RemoveChild(img)
		return true
	}
	set(img, "src", out)
	set(img, "loading", "lazy")
	if !has(img, "alt") {
		set(img, "alt", "")
	}
	return false
}

func (s *passState) rewriteSource(parent, src *html.Node) bool {
	del(src, "src")
	var out []string
	for _, cand := range strings.Split(get(src, "srcset"), ",") {
		f := strings.Fields(cand)
		if len(f) == 0 {
			continue
		}
		if u, ok := s.r.Image(f[0]); ok {
			out = append(out, strings.Join(append([]string{u}, f[1:]...), " "))
		}
	}
	if parent.DataAtom != atom.Picture || len(out) == 0 {
		parent.RemoveChild(src)
		return false
	}
	set(src, "srcset", strings.Join(out, ", "))
	return true
}

func (s *passState) finish(root *html.Node) {
	var words int
	var firstPara string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				words += len(strings.Fields(c.Data))
			}
			if c.Type == html.ElementNode {
				if c.DataAtom == atom.P && firstPara == "" {
					if t := textOf(c); t != "" {
						firstPara = t
					}
				}
				walk(c)
			}
		}
	}
	walk(root)
	s.words = words
	if s.opt.bridge {
		s.desc = firstParaRunes(firstPara, 300)
	}
}

func firstParaRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	cut := string(r[:n-1])
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// deCamo turns a GitHub camo proxy URL (https://camo.githubusercontent.com/<hmac>/<hex of the original URL>) back into the original URL.
// Only used for the GitHub-rendered bridge; anything that does not decode to an http(s) URL is returned unchanged.
func deCamo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "camo.githubusercontent.com" {
		return raw
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return raw
	}
	b, err := hex.DecodeString(parts[1])
	if err != nil || len(b) > 2048 || !utf8.Valid(b) {
		return raw
	}
	if o := string(b); strings.HasPrefix(o, "https://") || strings.HasPrefix(o, "http://") {
		return o
	}
	return raw
}
