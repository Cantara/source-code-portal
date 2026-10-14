package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEvade(t *testing.T) {
	p := New()
	const N = 512000
	cases := map[string]string{
		"para8k-emph": strings.Repeat(strings.Repeat("*a_", 2700)+"\n\n", N/8200),
		"para8k-brk":  strings.Repeat(strings.Repeat("[a](", 2000)+"\n\n", N/8200),
		"para8k-bt":   strings.Repeat(strings.Repeat("`a", 4000)+"\n\n", N/8200),
		"para-lines":  strings.Repeat("*a_*a_*a_*a_\n", N/13),
		"table-evade": "|a|\n|-|\n" + strings.Repeat("|"+strings.Repeat("*a_", 2600)+"|\n", N/7900),
		"list-evade":  strings.Repeat("- "+strings.Repeat("*a_", 2600)+"\n", N/7900),
		"head-evade":  strings.Repeat("# "+strings.Repeat("*a_", 2600)+"\n", N/7900),
		"quote-evade": strings.Repeat("> "+strings.Repeat("*a_", 2600)+"\n>\n", N/7900),
		"crlf":        strings.Repeat(strings.Repeat("*a_", 2700)+"\r\n\r\n", N/8200),
	}
	only := os.Getenv("CASE")
	for name, c := range cases {
		if only != "" && only != name {
			continue
		}
		st := time.Now()
		_, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: N}})
		d := time.Since(st)
		fmt.Printf("%-12s %7d bytes %8v err=%v\n", name, len(c), d.Round(time.Millisecond), err)
		// the guard must reject these in one linear pass: no parse, no quadratic cost (verification V-13: the first test printed only)
		if !errors.Is(err, ErrTooComplex) && !errors.Is(err, ErrTooNested) {
			t.Errorf("%s: want ErrTooComplex or ErrTooNested, got %v", name, err)
		}
		if d > 200*time.Millisecond*timeScale {
			t.Errorf("%s: rejection took %v", name, d)
		}
	}
}
