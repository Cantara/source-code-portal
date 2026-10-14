package npm

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/linex"
)

// jnode is an ordered JSON value with the 1-based line of its first token.
type jnode struct {
	kind byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	str  string
	keys []string
	obj  map[string]*jnode
	arr  []*jnode
	line int
}

func (n *jnode) get(k string) *jnode {
	if n == nil || n.kind != 'o' {
		return nil
	}
	return n.obj[k]
}
func (n *jnode) text() string {
	if n == nil || (n.kind != 's' && n.kind != 'n') {
		return ""
	}
	return n.str
}
func (n *jnode) isTrue() bool { return n != nil && n.kind == 'b' && n.str == "true" }

var errJSON = errors.New("json syntax")

// parseJSON decodes data into an ordered tree. Depth and node counts are bounded by g; duplicate keys keep the last value
// but the first position.
func parseJSON(data []byte, g *limits.Guard) (*jnode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	li := linex.New(data)
	var read func() (*jnode, error)
	read = func() (*jnode, error) {
		if err := g.Node(); err != nil {
			return nil, err
		}
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		line := li.Line(int(dec.InputOffset()))
		switch v := tok.(type) {
		case json.Delim:
			if err := g.Enter(); err != nil {
				return nil, err
			}
			defer g.Leave()
			if v == '{' {
				n := &jnode{kind: 'o', obj: map[string]*jnode{}, line: line}
				for dec.More() {
					kt, err := dec.Token()
					if err != nil {
						return nil, err
					}
					k, ok := kt.(string)
					if !ok {
						return nil, errJSON
					}
					kl := li.Line(int(dec.InputOffset()))
					c, err := read()
					if err != nil {
						return nil, err
					}
					if c.line < kl {
						c.line = kl
					}
					c.line = kl
					if _, dup := n.obj[k]; !dup {
						n.keys = append(n.keys, k)
					}
					n.obj[k] = c
				}
				if _, err := dec.Token(); err != nil {
					return nil, err
				}
				return n, nil
			}
			n := &jnode{kind: 'a', line: line}
			for dec.More() {
				c, err := read()
				if err != nil {
					return nil, err
				}
				n.arr = append(n.arr, c)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case string:
			return &jnode{kind: 's', str: v, line: line}, nil
		case json.Number:
			return &jnode{kind: 'n', str: v.String(), line: line}, nil
		case bool:
			s := "false"
			if v {
				s = "true"
			}
			return &jnode{kind: 'b', str: s, line: line}, nil
		case nil:
			return &jnode{kind: 'z', line: line}, nil
		}
		return nil, errJSON
	}
	root, err := read()
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errJSON
	}
	return root, nil
}

// stripJSONC blanks comments and trailing commas (bun.lock is JSONC) while keeping every newline, so lines stay valid.
func stripJSONC(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	inStr, esc := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/') {
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i++
			}
		case c == ',':
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				out[i] = ' '
			}
		}
	}
	return out
}
