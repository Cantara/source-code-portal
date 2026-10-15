package commits

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func graphOf(spec ...string) ([]Row, func(string) bool) {
	var order []*Commit
	known := map[string]bool{}
	for _, s := range spec {
		f := strings.Fields(s)
		order = append(order, &Commit{SHA: f[0], Parents: f[1:]})
		known[f[0]] = true
	}
	held := func(sha string) bool { return known[sha] }
	return layout(order, held), held
}

func lines(es []Edge) string {
	var out []string
	for _, e := range es {
		out = append(out, fmt.Sprintf("%d>%d", e.From, e.To))
	}
	slices.Sort(out)
	return strings.Join(out, " ")
}

func TestLayout(t *testing.T) {
	t.Run("a linear history is one lane", func(t *testing.T) {
		rows, _ := graphOf("c b", "b a", "a")
		got := []string{}
		for _, r := range rows {
			got = append(got, fmt.Sprintf("%d|%s|%s|%v|%d", r.Lane, lines(r.Above), lines(r.Below), r.Merge, r.Color))
		}
		if want := []string{"0||0>0|false|0", "0|0>0|0>0|false|0", "0|0>0||false|0"}; !slices.Equal(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
		if width(rows) != 1 {
			t.Errorf("width = %d", width(rows))
		}
	})
	t.Run("a merged branch opens a lane and converges at the fork", func(t *testing.T) {
		rows, _ := graphOf("m b1 f1", "f1 b0", "b1 b0", "b0")
		got := []string{}
		for _, r := range rows {
			got = append(got, fmt.Sprintf("%d|%v|%s|%s", r.Lane, r.Merge, lines(r.Above), lines(r.Below)))
		}
		want := []string{"0|true||0>0 0>1", "1|false|0>0 1>1|0>0 1>1", "0|false|0>0 1>1|0>0 1>1", "0|false|0>0 1>0|"}
		if !slices.Equal(got, want) || width(rows) != 2 || rows[1].Color == rows[0].Color {
			t.Errorf("rows = %v, want %v", got, want)
		}
	})
	t.Run("a merge parent joins the lane already expecting it", func(t *testing.T) {
		rows, _ := graphOf("m2 a x", "m1 a x", "x a", "a")
		if rows[1].Lane != 2 || lines(rows[1].Below) != "0>0 1>1 2>1 2>2" || lines(rows[3].Above) != "0>0 1>0 2>0" {
			t.Errorf("rows = %+v", rows)
		}
	})
	t.Run("lines to parents older than the history are open", func(t *testing.T) {
		rows, _ := graphOf("b a", "a z")
		if rows[0].Below[0].Open || !rows[1].Below[0].Open {
			t.Errorf("rows = %+v", rows)
		}
	})
	t.Run("an unexpected commit takes a free lane, and the width is bounded", func(t *testing.T) {
		rows, _ := graphOf("b a", "x", "a")
		if rows[0].Lane != 0 || rows[1].Lane != 1 || rows[2].Lane != 0 {
			t.Errorf("lanes = %d %d %d", rows[0].Lane, rows[1].Lane, rows[2].Lane)
		}
		parents := []string{"o"}
		for i := range 12 {
			parents = append(parents, fmt.Sprint("p", i))
		}
		wide, _ := graphOf(strings.Join(parents, " "))
		if width(wide) != maxLanes {
			t.Errorf("width = %d", width(wide))
		}
	})
}

// TestLayoutRandom: on random histories in graph order every line meets a node or goes on into the next row at the same lane.
func TestLayoutRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	for round := range 200 {
		n := 2 + rng.IntN(90)
		spec := make([]string, n)
		for i := range n {
			s := fmt.Sprint("c", i)
			if i < n-1 {
				for range 1 + rng.IntN(2)*rng.IntN(4) {
					if rng.IntN(10) == 0 {
						s += fmt.Sprint(" old", rng.IntN(4))
					} else {
						s += fmt.Sprint(" c", i+1+rng.IntN(n-i-1))
					}
				}
			}
			spec[i] = s
		}
		rows, _ := graphOf(spec...)
		for i, r := range rows {
			for _, e := range r.Above {
				if e.To != r.Lane && e.To != e.From {
					t.Fatalf("round %d row %d: a line from above ends nowhere: %+v", round, i, e)
				}
			}
			for _, e := range r.Below {
				if e.From != r.Lane && e.To != e.From {
					t.Fatalf("round %d row %d: a line below starts nowhere: %+v", round, i, e)
				}
				if i+1 < len(rows) && !slices.ContainsFunc(rows[i+1].Above, func(a Edge) bool { return a.From == e.To }) {
					t.Fatalf("round %d row %d: a line out does not come into the next row: %+v", round, i, e)
				}
			}
		}
	}
}
