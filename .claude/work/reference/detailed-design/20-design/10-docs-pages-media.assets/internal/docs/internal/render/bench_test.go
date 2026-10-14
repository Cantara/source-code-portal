package render

import (
	"context"
	"strings"
	"testing"
	"time"
)

func bigDoc(n int) []byte {
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString("## Heading\n\nSome *text* with [a link](https://example.com/x), [rel](docs/a.md#x) and `code` and <b>html</b>.\n\n- [x] item\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfunc main() { fmt.Println(\"x\") }\n```\n\n")
	}
	return []byte(sb.String())
}

func BenchmarkRender20KB(b *testing.B) {
	p := New()
	src := bigDoc(20 << 10)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		if _, err := p.Markdown(context.Background(), Input{Source: src, Route: testResolver()}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPathological(t *testing.T) {
	p := New()
	cases := map[string]string{
		"brackets":  strings.Repeat("[", 50000) + "x" + strings.Repeat("]", 50000),
		"emphasis":  strings.Repeat("*a ", 100000),
		"nest-list": strings.Repeat("  ", 1) + strings.Repeat("- ", 20000) + "x",
		"quotes":    strings.Repeat("> ", 30000) + "x",
		"backticks": strings.Repeat("`", 100000),
		"html-open": strings.Repeat("<div>", 80000),
		"big":       string(bigDoc(500 << 10)),
	}
	for name, c := range cases {
		st := time.Now()
		_, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: 512000}})
		t.Logf("%-10s %7d bytes  %v  err=%v", name, len(c), time.Since(st).Round(time.Millisecond), err)
	}
}
