package expand

import (
	"fmt"
	"strings"
	"testing"
)

func lookup(m map[string]string) Lookup {
	return func(n string) (string, bool) { v, ok := m[n]; return v, ok }
}

func TestExpand(t *testing.T) {
	e := New(nil)
	out, unres, cyc, err := e.Expand("${a}-${b}-${missing}-${self}", lookup(map[string]string{"a": "1", "b": "${a}${a}", "self": "x${self}"}), map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1-11-${missing}-x${self}" || len(unres) != 1 || unres[0] != "missing" || len(cyc) == 0 {
		t.Errorf("out=%q unresolved=%v cyclic=%v", out, unres, cyc)
	}
}

func TestPlaceholderEdges(t *testing.T) {
	e := New(nil)
	for in, want := range map[string]string{"${}": "${}", "${unterminated": "${unterminated", "a$b": "a$b", "${" + strings.Repeat("x", 300) + "}": "${" + strings.Repeat("x", 300) + "}"} {
		if got, _, _, err := e.Expand(in, lookup(nil), map[string]string{}, nil); err != nil || got != in || got != want {
			t.Errorf("Expand(%.20q) = %.20q, %v", in, got, err)
		}
	}
}

// TestDoublingBomb: forty properties that each reference the next one four times stop at the expansion limit, not at memory exhaustion.
func TestDoublingBomb(t *testing.T) {
	props := map[string]string{"p40": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
	for i := 0; i < 40; i++ {
		props[fmt.Sprintf("p%d", i)] = strings.Repeat(fmt.Sprintf("${p%d}", i+1), 4)
	}
	e := New(nil)
	_, _, _, err := e.Expand("${p0}", lookup(props), map[string]string{}, nil)
	if !IsExpand(err) || !e.Tripped {
		t.Fatalf("err=%v tripped=%v", err, e.Tripped)
	}
}

func TestSharedBudget(t *testing.T) {
	shared := 100
	e := New(&shared)
	_, _, _, err := e.Expand("${a}${a}${a}", lookup(map[string]string{"a": strings.Repeat("y", 60)}), map[string]string{}, nil)
	if !IsExpand(err) {
		t.Fatalf("err=%v", err)
	}
}

func FuzzExpand(f *testing.F) {
	f.Add("${a}${b}${a}")
	f.Fuzz(func(t *testing.T, s string) {
		props := map[string]string{"a": "${b}${b}", "b": "${a}x", "c": s}
		shared := 1 << 16
		e := New(&shared)
		out, _, _, err := e.Expand(s, lookup(props), map[string]string{}, nil)
		if err == nil && len(out) > 1<<20 {
			t.Fatalf("output %d bytes", len(out))
		}
	})
}
