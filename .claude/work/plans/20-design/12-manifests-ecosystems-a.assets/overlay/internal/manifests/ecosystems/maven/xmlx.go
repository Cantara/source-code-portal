package maven

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/linex"
)

// xnode is a namespace-less element tree (POM files use one namespace; its prefix is irrelevant to the model).
type xnode struct {
	name string
	text string // concatenated character data of this element, trimmed
	kids []*xnode
	line int
	// foreign marks an element in a namespace that is not a Maven POM namespace; Maven's own (non-namespace-aware) reader would not
	// recognise such an element by its unprefixed name, so lookups skip it.
	foreign bool
}

// pomNamespace is the prefix of every Maven POM namespace URI (4.0.0 and 4.1.0).
const pomNamespace = "http://maven.apache.org/POM/"

func foreignSpace(space string) bool { return space != "" && !strings.HasPrefix(space, pomNamespace) }

func (n *xnode) child(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name && !k.foreign {
			return k
		}
	}
	return nil
}

func (n *xnode) children(name string) []*xnode {
	var out []*xnode
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name && !k.foreign {
			out = append(out, k)
		}
	}
	return out
}

func (n *xnode) txt(name string) string {
	if c := n.child(name); c != nil {
		return c.text
	}
	return ""
}

var declEnc = regexp.MustCompile(`^\s*<\?xml[^>]*?encoding\s*=\s*["']([^"']+)["']`)

// prepareXML applies the text policy to a POM: BOM stripped, UTF-16 and NUL rejected, and UTF-8 validated unless the declaration names
// a supported single-byte charset (iso-8859-1, windows-1252: limits.CharsetReader). Errors are *limits.EncodingError.
func prepareXML(data []byte) ([]byte, error) {
	data, err := limits.RejectBinary(limits.StripBOM(data))
	if err != nil {
		return nil, err
	}
	head := data
	if len(head) > 200 {
		head = head[:200]
	}
	enc := "utf-8"
	if m := declEnc.FindSubmatch(head); m != nil {
		enc = strings.ToLower(string(m[1]))
	}
	switch enc {
	case "utf-8", "utf8":
		if !utf8.Valid(data) {
			return nil, &limits.EncodingError{Reason: "invalid-utf8"}
		}
	default:
		if _, err := limits.CharsetReader(enc, strings.NewReader("")); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// parseXML builds the element tree under the guard: every token counts as a node, nesting counts as depth.
func parseXML(data []byte, g *limits.Guard) (*xnode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	dec.CharsetReader = limits.CharsetReader
	li := linex.New(data)
	var root *xnode
	var stack []*xnode
	var text []*strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := g.Node(); err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := g.Enter(); err != nil {
				return nil, err
			}
			n := &xnode{name: t.Name.Local, line: li.Line(int(dec.InputOffset())), foreign: foreignSpace(t.Name.Space)}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			} else if root != nil {
				return nil, errors.New("multiple roots")
			} else {
				root = n
			}
			stack = append(stack, n)
			text = append(text, &strings.Builder{})
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("unbalanced")
			}
			n := stack[len(stack)-1]
			n.text = strings.TrimSpace(text[len(text)-1].String())
			stack, text = stack[:len(stack)-1], text[:len(text)-1]
			g.Leave()
		case xml.CharData:
			if len(stack) > 0 && len(t) > 0 {
				b := text[len(text)-1]
				if b.Len() < 1<<20 {
					b.Write(t)
				}
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errors.New("no document element")
	}
	return root, nil
}
