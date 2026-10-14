package config

import (
	"fmt"
	"strconv"
	"strings"
)

// The merged configuration is held as a generic tree: map[string]any, []any and scalars
// (string, int64, float64, bool, literalSecret). Layers are applied to it; the typed struct is built last.

type literalSecret string // a secret value given in the environment; never present in a file

// seg is one step of a path: a key or a list index.
type seg struct {
	key string
	idx int
	isI bool
}

func (s seg) String() string {
	if s.isI {
		return "[" + strconv.Itoa(s.idx) + "]"
	}
	return s.key
}

func joinPath(segs []seg) string {
	var b strings.Builder
	for i, s := range segs {
		if s.isI {
			b.WriteString(s.String())
		} else {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.key)
		}
	}
	return b.String()
}

// parsePath parses "github.sources[0].auth.kind".
func parsePath(p string) ([]seg, error) {
	var out []seg
	for _, part := range strings.Split(p, ".") {
		name, rest, _ := strings.Cut(part, "[")
		if name != "" {
			out = append(out, seg{key: name})
		}
		for rest != "" {
			num, after, ok := strings.Cut(rest, "]")
			if !ok {
				return nil, fmt.Errorf("bad path %q", p)
			}
			n, err := strconv.Atoi(num)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad index in path %q", p)
			}
			out = append(out, seg{idx: n, isI: true})
			rest = strings.TrimPrefix(after, "[")
			if after != "" && !strings.HasPrefix(after, "[") {
				return nil, fmt.Errorf("bad path %q", p)
			}
			if after == "" {
				rest = ""
			}
		}
	}
	return out, nil
}

// merge applies src over dst: mappings merge key by key, everything else (lists included) is replaced.
func merge(dst, src any) any {
	sm, sok := src.(map[string]any)
	dm, dok := dst.(map[string]any)
	if !sok || !dok {
		return clone(src)
	}
	for k, v := range sm {
		if cur, has := dm[k]; has {
			dm[k] = merge(cur, v)
		} else {
			dm[k] = clone(v)
		}
	}
	return dm
}

func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = clone(e)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = clone(e)
		}
		return s
	}
	return v
}

// set stores v at the path below node and returns the updated node. Mappings are created on demand and a list may
// grow by exactly one element (index == len); a larger index is an error, so a typo cannot silently create a gap.
func set(node any, segs []seg, v any) (any, error) {
	if len(segs) == 0 {
		return v, nil
	}
	s := segs[0]
	if s.isI {
		list, ok := node.([]any)
		if node != nil && !ok {
			return nil, fmt.Errorf("not a list")
		}
		if s.idx > len(list) {
			return nil, fmt.Errorf("index %d leaves a gap (the list has %d elements)", s.idx, len(list))
		}
		if s.idx == len(list) {
			list = append(list, nil)
		}
		elem, err := set(list[s.idx], segs[1:], v)
		if err != nil {
			return nil, err
		}
		list[s.idx] = elem
		return list, nil
	}
	m, ok := node.(map[string]any)
	if node != nil && !ok {
		return nil, fmt.Errorf("not a mapping")
	}
	if m == nil {
		m = map[string]any{}
	}
	child, err := set(m[s.key], segs[1:], v)
	if err != nil {
		return nil, err
	}
	m[s.key] = child
	return m, nil
}

// getAt returns the value at the path.
func getAt(root any, segs []seg) (any, bool) {
	cur := root
	for _, s := range segs {
		switch c := cur.(type) {
		case map[string]any:
			if s.isI {
				return nil, false
			}
			v, ok := c[s.key]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			if !s.isI || s.idx >= len(c) {
				return nil, false
			}
			cur = c[s.idx]
		default:
			return nil, false
		}
	}
	return cur, true
}
