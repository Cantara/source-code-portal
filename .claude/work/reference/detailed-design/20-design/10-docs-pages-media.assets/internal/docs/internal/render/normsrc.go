package render

import (
	"github.com/yuin/goldmark/text"
)

// NormaliseSource rebuilds the LLM Markdown variant of an already processed body (Output.Body). It parses without
// rendering, so it costs a fraction of Markdown; checkShape guards the parse like everywhere else, and a body that
// fails the guard (a tampered store) yields "" and the marker "rejected".
func NormaliseSource(body string) (string, []string) {
	src := []byte(body)
	if err := checkShape(src); err != nil {
		return "", []string{"rejected"}
	}
	p := New()
	md := p.newMarkdown(&renderState{maxBlock: 4 << 10})
	doc := md.Parser().Parse(text.NewReader(src))
	return Normalise(doc, src)
}
