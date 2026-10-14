package npm

import (
	"bytes"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

const maxYAMLDocs = 4

// yw walks yaml.Node trees under a Guard. Aliases are dereferenced on access; every visited entry is counted, so an
// alias bomb is cut by the node budget rather than by yaml.v3's own protection alone.
type yw struct{ g *limits.Guard }

type ykv struct {
	key  string
	val  *yaml.Node
	line int
}

func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && i < 4; i++ {
		n = n.Alias
	}
	return n
}

// decodeDocs reads up to maxYAMLDocs documents. It returns the content nodes (not the DocumentNode wrappers).
//
// CRLF is folded to LF and a lone CR becomes a space first: the YAML library counts a lone CR as a line break, the rest of the model counts
// LF only (the runner bounds locations by the LF count), so a CR-only file would report lines past its end.
func decodeDocs(data []byte) ([]*yaml.Node, error) {
	if bytes.IndexByte(data, '\r') >= 0 {
		data = bytes.ReplaceAll(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\r"), []byte(" "))
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(docs) >= maxYAMLDocs {
			return nil, &limits.LimitError{Limit: limits.LimitNodes, Max: maxYAMLDocs}
		}
		c := &n
		if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
			c = n.Content[0]
		}
		docs = append(docs, c)
	}
}

// measure checks depth and node count of the tree without following aliases.
func (w *yw) measure(n *yaml.Node) error {
	if err := w.g.Node(); err != nil {
		return err
	}
	if len(n.Content) == 0 {
		return nil
	}
	if err := w.g.Enter(); err != nil {
		return err
	}
	defer w.g.Leave()
	for _, c := range n.Content {
		if err := w.measure(c); err != nil {
			return err
		}
	}
	return nil
}

// pairs returns the key/value pairs of a mapping in source order (merge keys spliced in, one level).
func (w *yw) pairs(n *yaml.Node) []ykv {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []ykv
	for i := 0; i+1 < len(n.Content); i += 2 {
		if w.g.Node() != nil {
			return out
		}
		k, v := n.Content[i], n.Content[i+1]
		if k.Tag == "!!merge" {
			if m := deref(v); m != nil && m.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(m.Content); j += 2 {
					out = append(out, ykv{m.Content[j].Value, m.Content[j+1], m.Content[j].Line})
				}
			}
			continue
		}
		out = append(out, ykv{k.Value, v, k.Line})
	}
	return out
}

func (w *yw) get(n *yaml.Node, key string) *yaml.Node {
	var found *yaml.Node
	for _, p := range w.pairs(n) {
		if p.key == key {
			found = p.val
		}
	}
	return deref(found)
}

func (w *yw) str(n *yaml.Node) string {
	n = deref(n)
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func (w *yw) strList(n *yaml.Node) []string {
	n = deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, c := range n.Content {
		if w.g.Node() != nil {
			return out
		}
		if s := w.str(c); s != "" {
			out = append(out, s)
		}
	}
	return out
}
