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

func TestScaling(t *testing.T) {
	heavy(t)
	p := New()
	const N = 512000
	deep := func(marker string, d int) string {
		line := strings.Repeat(marker, d) + "x\n"
		return strings.Repeat(line, N/len(line))
	}
	cases := map[string]string{
		"list-d60":  deep("- ", 60),
		"quote-d60": deep("> ", 60),
		"list-d200": deep("- ", 200),
		"indent-list": func() string {
			var sb strings.Builder
			for i := 0; sb.Len() < N; i++ {
				sb.WriteString(strings.Repeat("  ", i%60) + "- x\n")
			}
			return sb.String()
		}(),
		"brackets":  strings.Repeat("[", N),
		"brackets2": strings.Repeat("[a](", N/4),
		"emph":      strings.Repeat("*a_", N/3),
		"emph2":     strings.Repeat("a*b_c**d__e", N/11),
		"linkdefs":  strings.Repeat("[a]: /x\n", N/8),
		"refs":      strings.Repeat("[a][b]", N/6),
		"backticks": strings.Repeat("`a", N/2),
		"bt-alt": func() string {
			var sb strings.Builder
			for i := 1; sb.Len() < N; i++ {
				sb.WriteString(strings.Repeat("`", i%40+1) + " ")
			}
			return sb.String()
		}(),
		"html-inline": strings.Repeat("<a ", N/3),
		"html-nested": strings.Repeat("<b>", N/3),
		"table":       strings.Repeat("|a|b|c|\n|-|-|-|\n", N/16),
		"fence-many":  strings.Repeat("```go\nx := 1\n```\n", N/17),
		"footnotes":   strings.Repeat("a[^1]\n\n[^1]: x\n\n", N/16),
		"autolink":    strings.Repeat("www.a.b ", N/8),
		"entities":    strings.Repeat("&amp;&#35;&#x41;", N/16),
		"headings":    strings.Repeat("# a\n", N/4),
	}
	only := os.Getenv("CASE")
	for name, c := range cases {
		if only != "" && only != name {
			continue
		}
		if len(c) > N {
			c = c[:N]
		}
		st := time.Now()
		_, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: N}})
		d := time.Since(st)
		if err != nil && !errors.Is(err, ErrTooNested) && !errors.Is(err, ErrTooComplex) && !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: unexpected error %v", name, err)
		}
		fmt.Printf("%-12s %7d bytes %8v err=%v\n", name, len(c), d.Round(time.Millisecond), err)
		if d > 3*time.Second*timeScale {
			t.Errorf("%s too slow: %v", name, d)
		}
	}
}
