package limits

import "strings"

// Expand performs bounded ${name}-style interpolation (open/close delimiters, e.g. "${", "}" or "$(", ")").
// Unknown names are left as written. Cycles and self references expand to "" (Maven behaviour). The result of any
// single value is bounded by g (Expand accounting) so a 700-byte property chain cannot produce 160 MB (research E5).
func Expand(s string, lookup func(name string) (string, bool), open, close string, g *Guard) (string, error) {
	return expand(s, lookup, open, close, g, map[string]bool{}, 0)
}

func expand(s string, lookup func(string) (string, bool), open, close string, g *Guard, active map[string]bool, depth int) (string, error) {
	if !strings.Contains(s, open) {
		return s, nil
	}
	if depth > 64 {
		return "", &LimitError{LimitDepth, 64}
	}
	var b strings.Builder
	for {
		i := strings.Index(s, open)
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.Index(s[i+len(open):], close)
		if j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		name := s[i+len(open) : i+len(open)+j]
		rest := s[i+len(open)+j+len(close):]
		val, ok := lookup(name)
		switch {
		case !ok:
			b.WriteString(open + name + close)
		case active[name]:
			// cycle: expands to nothing
		default:
			active[name] = true
			v, err := expand(val, lookup, open, close, g, active, depth+1)
			delete(active, name)
			if err != nil {
				return "", err
			}
			if err := g.Expand(len(v)); err != nil {
				return "", err
			}
			b.WriteString(v)
		}
		if b.Len() > g.lim.MaxExpand {
			return "", &LimitError{LimitExpand, g.lim.MaxExpand}
		}
		s = rest
	}
	return b.String(), nil
}

// PreCheck computes, WITHOUT expanding, the worst-case expanded length of every property (memoised, saturating at max)
// and returns the names whose expansion exceeds max. Used before handing a property map to a library that has no output cap
// (deps.dev/util/maven Interpolate). refs extracts the referenced names from a value.
func PreCheck(props map[string]string, refs func(value string) []string, max int) (over []string) {
	const unknown = -1
	memo := map[string]int{}
	state := map[string]int{} // 0 new, 1 visiting, 2 done
	var length func(name string) int
	length = func(name string) int {
		if state[name] == 2 {
			return memo[name]
		}
		if state[name] == 1 {
			return 0 // cycle
		}
		state[name] = 1
		v := props[name]
		n := len(v)
		for _, r := range refs(v) {
			if _, ok := props[r]; !ok {
				continue
			}
			l := length(r)
			n += l
			if n > max {
				n = max + 1
			}
		}
		state[name] = 2
		memo[name] = n
		return n
	}
	_ = unknown
	for name := range props {
		if length(name) > max {
			over = append(over, name)
		}
	}
	sortStrings(over)
	return over
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
