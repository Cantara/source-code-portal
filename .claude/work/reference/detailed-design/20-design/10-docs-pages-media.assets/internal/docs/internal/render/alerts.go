package render

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// KindAlert is the node kind of a GitHub alert (> [!NOTE]).
var KindAlert = ast.NewNodeKind("Alert")

type alertNode struct {
	ast.BaseBlock
	Variant string // note, tip, important, warning, caution
}

func (n *alertNode) Kind() ast.NodeKind { return KindAlert }
func (n *alertNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Variant": n.Variant}, nil)
}

var alertTypes = map[string]string{"NOTE": "Note", "TIP": "Tip", "IMPORTANT": "Important", "WARNING": "Warning", "CAUTION": "Caution"}

type alertTransformer struct{}

func (alertTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	src := reader.Source()
	var targets []*ast.Blockquote
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if bq, ok := n.(*ast.Blockquote); ok && entering {
			targets = append(targets, bq)
		}
		return ast.WalkContinue, nil
	})
	for _, bq := range targets {
		p, ok := bq.FirstChild().(*ast.Paragraph)
		if !ok || p.Lines().Len() == 0 {
			continue
		}
		first := p.Lines().At(0)
		line := bytes.TrimRight(first.Value(src), " \t\r\n")
		if len(line) < 5 || line[0] != '[' || line[1] != '!' || line[len(line)-1] != ']' {
			continue
		}
		typ := strings.ToUpper(string(line[2 : len(line)-1]))
		if _, ok := alertTypes[typ]; !ok {
			continue
		}
		// drop the marker line from the paragraph; drop the paragraph when nothing is left
		lines := text.NewSegments()
		for i := 1; i < p.Lines().Len(); i++ {
			lines.Append(p.Lines().At(i))
		}
		if lines.Len() == 0 {
			bq.RemoveChild(bq, p)
		} else {
			p.SetLines(lines)
			// remove the inline children belonging to the marker line (the first Text node plus its soft line break)
			for c := p.FirstChild(); c != nil; {
				next := c.NextSibling()
				if t, ok := c.(*ast.Text); ok && t.Segment.Start >= first.Start && t.Segment.Stop <= first.Stop+1 {
					p.RemoveChild(p, c)
					if t.SoftLineBreak() {
						break
					}
				} else {
					break
				}
				c = next
			}
		}
		a := &alertNode{Variant: strings.ToLower(typ)}
		for c := bq.FirstChild(); c != nil; {
			next := c.NextSibling()
			bq.RemoveChild(bq, c)
			a.AppendChild(a, c)
			c = next
		}
		bq.Parent().ReplaceChild(bq.Parent(), bq, a)
	}
}

type alertRenderer struct{}

func (alertRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindAlert, renderAlert)
}

func renderAlert(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	a := n.(*alertNode)
	if entering {
		_, _ = w.WriteString(`<div class="md-alert md-alert-` + a.Variant + `"><p class="md-alert-title">` + alertTypes[strings.ToUpper(a.Variant)] + "</p>\n")
	} else {
		_, _ = w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}
