package docs

import (
	"bytes"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/untrusted"
)

// frontMatter holds the four keys the portal reads; other keys stay in the source.
type frontMatter struct {
	title, description string
	order              int
	hidden             bool
}

// splitFrontMatter separates a leading "---" YAML block (at most 16 KiB, one mapping without anchors or aliases) from the body. A
// block that is not valid front matter is left in the body.
func splitFrontMatter(b []byte) (frontMatter, []byte) {
	fm := frontMatter{order: noOrder}
	rest, ok := bytes.CutPrefix(b, []byte("---\n"))
	if !ok {
		return fm, b
	}
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 || end > maxFrontMatter {
		return fm, b
	}
	var doc yaml.Node
	if yaml.Unmarshal(rest[:end], &doc) != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode || !plain(&doc) {
		return fm, b
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		v := m.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			continue
		}
		switch m.Content[i].Value {
		case "title":
			fm.title = untrusted.Text(v.Value, maxTitle)
		case "description":
			fm.description = untrusted.Text(v.Value, maxDescription)
		case "order":
			if n, err := strconv.Atoi(v.Value); err == nil && n >= 0 && n < noOrder {
				fm.order = n
			}
		case "hidden":
			fm.hidden = v.Value == "true"
		}
	}
	return fm, rest[end+len("\n---\n"):]
}

// plain reports whether n holds no anchors, aliases or merge keys.
func plain(n *yaml.Node) bool {
	if n.Kind == yaml.AliasNode || n.Anchor != "" || n.Tag == "!!merge" {
		return false
	}
	for _, c := range n.Content {
		if !plain(c) {
			return false
		}
	}
	return true
}
