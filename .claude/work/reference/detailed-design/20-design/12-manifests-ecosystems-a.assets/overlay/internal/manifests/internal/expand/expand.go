// Package expand (rank 2, imports limits only: the matrix rule "equal rank never imports equal rank" is kept, VF-12-08; new package of unit 12, D-4): bounded ${name} interpolation shared
// by the Maven and Gradle assemblers. Every byte produced is charged against a per-value cap, a per-unit budget and an optional
// run-wide budget, so property chains that double at each level (the "billion laughs" of POM properties) stop at limit.expand.
package expand

import (
	"errors"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

// errExpand is the typed limit error returned when a budget trips.
func errExpand() error {
	return &limits.LimitError{Limit: limits.LimitExpand, Max: limits.DefaultMaxExpand}
}

// IsExpand reports whether err is the expansion-budget limit error (errors.As against *limits.LimitError with Limit "expand").
func IsExpand(err error) bool {
	var le *limits.LimitError
	return errors.As(err, &le) && le.Limit == limits.LimitExpand
}

const (
	MaxDepth       = 16  // nesting of property references
	MaxPlaceholder = 256 // longest accepted ${name}
)

// Expander holds the budgets. Not safe for concurrent use.
type Expander struct {
	Shared   *int // run-wide remaining bytes; nil means unlimited
	Budget   int  // remaining bytes for this unit
	MaxValue int  // cap for one expanded value
	Tripped  bool
}

// New returns an Expander with the default caps (1 MiB per value, 8 MiB per unit).
func New(shared *int) *Expander {
	return &Expander{Shared: shared, Budget: limits.DefaultMaxExpandAll, MaxValue: limits.DefaultMaxExpand}
}

// Lookup returns the raw value of a name; known=false keeps the placeholder in the output and reports it unresolved.
type Lookup func(name string) (raw string, known bool)

// Expand replaces every resolvable ${name} in s. memo caches fully expanded names and must be private to one lookup scope.
// Cyclic references keep the placeholder and are reported in cyclic.
func (e *Expander) Expand(s string, look Lookup, memo map[string]string, stack []string) (out string, unresolved, cyclic []string, err error) {
	if !strings.Contains(s, "${") {
		return s, nil, nil, nil
	}
	if len(stack) > MaxDepth {
		return s, nil, nil, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], "${")
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		i += j
		end := strings.IndexByte(s[i:], '}')
		if end < 0 || end > MaxPlaceholder {
			b.WriteString(s[i:])
			break
		}
		name := s[i+2 : i+end]
		raw := s[i : i+end+1]
		i += end + 1
		if name == "" {
			b.WriteString(raw)
			continue
		}
		if v, ok := memo[name]; ok {
			b.WriteString(v)
			if v == raw {
				unresolved = append(unresolved, name)
			}
			if err := e.charge(len(v), b.Len()); err != nil {
				return s, unresolved, cyclic, err
			}
			continue
		}
		cyc := false
		for _, st := range stack {
			if st == name {
				cyc = true
			}
		}
		if cyc {
			b.WriteString(raw)
			cyclic = append(cyclic, name)
			continue
		}
		val, known := look(name)
		if !known {
			b.WriteString(raw)
			unresolved = append(unresolved, name)
			memo[name] = raw
			continue
		}
		v, u, c, err := e.Expand(val, look, memo, append(stack, name))
		if err != nil {
			return s, unresolved, cyclic, err
		}
		unresolved, cyclic = append(unresolved, u...), append(cyclic, c...)
		if !contains(c, name) {
			memo[name] = v
		}
		b.WriteString(v)
		if err := e.charge(len(v), b.Len()); err != nil {
			return s, unresolved, cyclic, err
		}
	}
	return b.String(), unresolved, cyclic, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (e *Expander) charge(n, valueLen int) error {
	e.Budget -= n
	if e.Shared != nil {
		*e.Shared -= n
		if *e.Shared < 0 {
			e.Tripped = true
			return errExpand()
		}
	}
	if e.Budget < 0 || valueLen > e.MaxValue {
		e.Tripped = true
		return errExpand()
	}
	return nil
}
