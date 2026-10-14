package render

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const maxFrontMatter = 16 << 10

// SplitFrontMatter splits a leading YAML block. The file must start with a "---" line; the block ends at the first line that is exactly
// "---" or "..." within 16 KiB. ok is false when there is no (terminated) block: the whole input is then body.
func SplitFrontMatter(src []byte) (fm, body []byte, ok bool) {
	if !bytes.HasPrefix(src, []byte("---")) {
		return nil, src, false
	}
	rest := src[3:]
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 || len(bytes.TrimRight(rest[:nl], " \t\r")) != 0 {
		return nil, src, false
	}
	rest = rest[nl+1:]
	limit := len(rest)
	if limit > maxFrontMatter+8 {
		limit = maxFrontMatter + 8
	}
	off := 0
	for off < limit {
		end := bytes.IndexByte(rest[off:], '\n')
		var line []byte
		next := 0
		if end < 0 {
			line, next = rest[off:], len(rest)
		} else {
			line, next = rest[off:off+end], off+end+1
		}
		t := string(bytes.TrimRight(line, " \t\r"))
		if t == "---" || t == "..." {
			if off > maxFrontMatter {
				return nil, src, false
			}
			return rest[:off], rest[next:], true
		}
		off = next
		if end < 0 {
			break
		}
	}
	return nil, src, false
}

// PortalKeys are the only front-matter keys the portal interprets.
var PortalKeys = [...]string{"title", "description", "order", "tags", "hidden"}

// ParseFrontMatter parses the block leniently: anchors, aliases, multiple documents and non-mapping roots yield an empty map and one
// problem; a bad portal key is dropped with a problem; unknown keys are kept (JSON-safe values only, 8 KiB cap).
func ParseFrontMatter(fm []byte) (map[string]any, []Problem) {
	var probs []Problem
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, []Problem{{Path: "", Code: "frontmatter.yaml", Message: "invalid YAML"}}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, []Problem{{Code: "frontmatter.multi_document", Message: "more than one YAML document"}}
	}
	if bad := rejectAliases(&root); bad != "" {
		return nil, []Problem{{Code: "frontmatter.anchors", Message: bad}}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, []Problem{{Code: "frontmatter.not_mapping", Message: "front matter must be a mapping"}}
	}
	m := map[string]any{}
	n := root.Content[0]
	size := 0
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		val, ok := toJSONValue(v, 0)
		if !ok {
			probs = append(probs, Problem{Path: "/" + k.Value, Code: "frontmatter.value", Message: "unsupported value"})
			continue
		}
		if !validPortalKey(k.Value, val) {
			probs = append(probs, Problem{Path: "/" + k.Value, Code: "frontmatter.schema", Message: fmt.Sprintf("invalid value for %q", k.Value)})
			continue
		}
		if k.Value == "scp" {
			probs = append(probs, Problem{Path: "/scp", Code: "frontmatter.reserved_key", Message: `"scp" is reserved for portal metadata`})
			continue
		}
		size += len(k.Value) + approxSize(val)
		if size > 8<<10 {
			probs = append(probs, Problem{Path: "/" + k.Value, Code: "frontmatter.too_large", Message: "front matter values exceed 8 KiB"})
			break
		}
		m[k.Value] = val
	}
	return m, probs
}

func approxSize(v any) int {
	switch t := v.(type) {
	case string:
		return len(t)
	case []any:
		n := 0
		for _, e := range t {
			n += approxSize(e)
		}
		return n
	case map[string]any:
		n := 0
		for k, e := range t {
			n += len(k) + approxSize(e)
		}
		return n
	}
	return 8
}

func rejectAliases(n *yaml.Node) string {
	var walk func(n *yaml.Node, depth int) string
	count := 0
	walk = func(n *yaml.Node, depth int) string {
		count++
		if count > 20000 || depth > 32 {
			return "document too deep or too large"
		}
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return "anchors and aliases are not allowed"
		}
		for _, c := range n.Content {
			if s := walk(c, depth+1); s != "" {
				return s
			}
		}
		return ""
	}
	return walk(n, 0)
}

func toJSONValue(n *yaml.Node, depth int) (any, bool) {
	if depth > 8 {
		return nil, false
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			return nil, true
		case "!!bool":
			var b bool
			if n.Decode(&b) != nil {
				return nil, false
			}
			return b, true
		case "!!int":
			var i int64
			if n.Decode(&i) != nil {
				return nil, false
			}
			return i, true
		case "!!float":
			var f float64
			if n.Decode(&f) != nil {
				return nil, false
			}
			return f, true
		default:
			return CleanText(n.Value), true
		}
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, ok := toJSONValue(c, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	case yaml.MappingNode:
		out := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Kind != yaml.ScalarNode {
				return nil, false
			}
			v, ok := toJSONValue(n.Content[i+1], depth+1)
			if !ok {
				return nil, false
			}
			out[CleanText(n.Content[i].Value)] = v
		}
		return out, true
	}
	return nil, false
}

func validPortalKey(k string, v any) bool {
	switch k {
	case "title":
		s, ok := v.(string)
		return ok && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 120
	case "description":
		s, ok := v.(string)
		return ok && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 300
	case "order":
		i, ok := v.(int64)
		return ok && i >= -100000 && i <= 100000
	case "hidden":
		_, ok := v.(bool)
		return ok
	case "tags":
		a, ok := v.([]any)
		if !ok || len(a) > 20 {
			return false
		}
		seen := map[string]bool{}
		for _, e := range a {
			s, ok := e.(string)
			if !ok || len(s) > 63 || !tagRE.MatchString(s) || seen[s] {
				return false
			}
			seen[s] = true
		}
		return true
	}
	return true
}

// CleanText removes C0/C1 controls (except tab/newline), bidi controls and invisible characters from a string value.
func CleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if isStrippable(r) {
			return -1
		}
		return r
	}, s)
}
