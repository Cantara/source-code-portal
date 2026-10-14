package san

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Resolver maps a URL found in untrusted HTML to a portal URL, or "" to drop it.
type Resolver interface {
	Link(raw string) string  // <a href>
	Image(raw string) string // <img src>, <source srcset> candidates
}

var reAlert = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*`)

// Rewrite runs BEFORE the sanitiser. It never needs to be trusted: bluemonday is the last stage.
func Rewrite(in string, res Resolver) (string, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(in), ctx)
	if err != nil {
		return "", err
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.A:
				setAttr(n, "href", res.Link)
			case atom.Img:
				setAttr(n, "src", res.Image)
				delAttr(n, "srcset") // <img srcset> dropped; src is the fallback
				delAttr(n, "sizes")
			case atom.Source:
				if v := getAttr(n, "srcset"); v != "" {
					setAttrVal(n, "srcset", rewriteSrcset(v, res.Image))
				}
				delAttr(n, "src")
			case atom.Blockquote:
				if t, ok := alertType(n); ok {
					n.Data, n.DataAtom = "div", atom.Div
					n.Attr = []html.Attribute{{Key: "class", Val: "markdown-alert markdown-alert-" + strings.ToLower(t)}}
					title := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P,
						Attr: []html.Attribute{{Key: "class", Val: "markdown-alert-title"}}}
					title.AppendChild(&html.Node{Type: html.TextNode, Data: strings.ToUpper(t[:1]) + strings.ToLower(t[1:])})
					n.InsertBefore(title, n.FirstChild)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	var buf bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&buf, c); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func alertType(bq *html.Node) (string, bool) {
	for c := bq.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		if c.Type == html.ElementNode && c.DataAtom == atom.P && c.FirstChild != nil && c.FirstChild.Type == html.TextNode {
			if m := reAlert.FindStringSubmatch(c.FirstChild.Data); m != nil {
				c.FirstChild.Data = c.FirstChild.Data[len(m[0]):]
				return m[1], true
			}
		}
		return "", false
	}
	return "", false
}

func rewriteSrcset(v string, f func(string) string) string {
	var out []string
	for _, cand := range strings.Split(v, ",") {
		f1 := strings.Fields(cand)
		if len(f1) == 0 {
			continue
		}
		if u := f(f1[0]); u != "" {
			out = append(out, strings.Join(append([]string{u}, f1[1:]...), " "))
		}
	}
	return strings.Join(out, ", ")
}

func getAttr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}
func setAttr(n *html.Node, k string, f func(string) string) {
	if v := getAttr(n, k); v != "" {
		setAttrVal(n, k, f(strings.TrimSpace(v)))
	}
}
func setAttrVal(n *html.Node, k, v string) {
	for i, a := range n.Attr {
		if a.Key == k {
			if v == "" {
				n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			} else {
				n.Attr[i].Val = v
			}
			return
		}
	}
}
func delAttr(n *html.Node, k string) { setAttrVal(n, k, "") }

// RepoResolver: example implementation. base is the repo blob/raw location.
type RepoResolver struct{ Repo, Ref, Dir string }

func (r RepoResolver) Link(raw string) string {
	if strings.HasPrefix(raw, "#") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || strings.HasPrefix(raw, "//") {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return u.String()
	case "":
		p := cleanJoin(r.Dir, u.Path)
		return "/repos/" + url.PathEscape(r.Repo) + "/files/" + url.PathEscape(r.Ref) + "/" + p // route-table generated in real code
	}
	return ""
}

func (r RepoResolver) Image(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || strings.HasPrefix(raw, "//") {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "":
		return "/media/" + url.PathEscape(r.Repo) + "/" + url.PathEscape(r.Ref) + "/" + cleanJoin(r.Dir, u.Path)
	case "https":
		return "/media/ext?u=" + url.QueryEscape(u.String()) // + HMAC signature in real code
	}
	return ""
}

func cleanJoin(dir, p string) string {
	if strings.HasPrefix(p, "/") {
		dir = ""
	}
	parts := []string{}
	for _, s := range strings.Split(dir+"/"+p, "/") {
		switch s {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, url.PathEscape(s))
		}
	}
	return strings.Join(parts, "/")
}
