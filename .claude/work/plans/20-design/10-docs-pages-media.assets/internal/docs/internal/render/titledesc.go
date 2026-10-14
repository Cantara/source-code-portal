package render

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// titleAndDescription returns the first H1 text and the first paragraph with visible text (badge-only paragraphs are skipped).
func titleAndDescription(doc ast.Node, src []byte) (title, desc string) {
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Heading:
			if t.Level == 1 && title == "" {
				title = truncRunes(strings.Join(strings.Fields(plain(c, src)), " "), 120)
			}
		case *ast.Paragraph:
			if desc == "" {
				if p := strings.Join(strings.Fields(plain(c, src)), " "); p != "" {
					desc = firstParaRunes(p, 300)
				}
			}
		}
		if title != "" && desc != "" {
			return
		}
	}
	return
}

func plain(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch t := c.(type) {
			case *ast.Text:
				b.Write(t.Segment.Value(src))
				if t.SoftLineBreak() || t.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(t.Value)
			case *ast.CodeSpan, *ast.Emphasis, *ast.Link, *ast.AutoLink:
				walk(c)
			case *ast.Image:
				// images contribute nothing (badges)
			}
		}
	}
	walk(n)
	return b.String()
}
