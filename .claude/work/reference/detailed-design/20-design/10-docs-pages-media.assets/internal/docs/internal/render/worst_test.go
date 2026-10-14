package render

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWorstAccepted(t *testing.T) {
	heavy(t)
	p := New()
	for _, unit := range []string{"*a_", "[a](", "`a", "a*b_c**d__e"} {
		for _, L := range []int{40000, 30000, 20000, 8000, 4000, 2000} {
			para := strings.Repeat(unit, L/len(unit)) + "\n\n"
			n := int(1.9e9 / float64(L) / float64(L))
			if n*len(para) > 512000 {
				n = 512000 / len(para)
			}
			c := strings.Repeat(para, n)
			st := time.Now()
			_, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: 512000}})
			d := time.Since(st)
			fmt.Printf("%-12q L=%5d n=%4d total=%6d B %8v err=%v\n", unit, L, n, len(c), d.Round(time.Millisecond), err)
			// the worst input the guard ACCEPTS must stay near the budget (about 1 s measured); 4 s leaves room for a slow CI core
			if err != nil || d > 4*time.Second*timeScale {
				t.Errorf("%q L=%d: err=%v took %v", unit, L, err, d)
			}
		}
	}
}
