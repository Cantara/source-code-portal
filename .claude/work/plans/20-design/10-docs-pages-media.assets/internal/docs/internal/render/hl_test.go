package render

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestHighlightWorst(t *testing.T) {
	heavy(t)
	p := New()
	for _, lang := range []string{"yaml", "makefile", "java"} {
		blk := "```" + lang + "\n" + strings.Repeat("1.", 2000) + "\n```\n\n"
		c := strings.Repeat(blk, 100)
		st := time.Now()
		out, err := p.Markdown(context.Background(), Input{Source: []byte(c), Route: testResolver(), Limits: Limits{MaxSourceBytes: 512000}})
		d := time.Since(st)
		fmt.Printf("%-9s %d blocks of 4000 B: %v err=%v html=%d\n", lang, 100, d.Round(time.Millisecond), err, len(out.HTML.String()))
		// 1.5 s highlight budget plus one over-running lexer call (0.8 s measured) plus parsing: about 2.3 s worst; a result is either
		// a document or the typed ErrTooLarge (the 8 MiB output cap), never another error and never a stall
		if err != nil && !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: %v", lang, err)
		}
		if d > 5*time.Second*timeScale {
			t.Errorf("%s: took %v", lang, d)
		}
	}
}
