package render

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExponent(t *testing.T) {
	p := New()
	pats := map[string]string{"brackets2": "[a](", "emph": "*a_", "emph2": "a*b_c**d__e", "backticks": "`a", "linkdefs": "[a]: /x\n", "headings": "# a\n"}
	only := os.Getenv("CASE")
	for name, unit := range pats {
		if only != "" && only != name {
			continue
		}
		var prev time.Duration
		for _, n := range []int{8 << 10, 16 << 10, 32 << 10, 64 << 10} {
			c := strings.Repeat(unit, n/len(unit))
			st := time.Now()
			_, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: 1 << 20}})
			d := time.Since(st)
			ratio := 0.0
			if prev > 0 {
				ratio = float64(d) / float64(prev)
			}
			fmt.Printf("%-10s %6d B %10v x%.1f err=%v\n", name, n, d.Round(time.Microsecond), ratio, err)
			prev = d
		}
	}
}
