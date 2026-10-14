package render

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// TestChromaBudget measures the wall-clock overrun of a per-block deadline checked between tokens (the design's mitigation).
func TestChromaBudget(t *testing.T) {
	heavy(t)
	size := 4 << 10
	if v := os.Getenv("SIZE"); v != "" {
		fmt.Sscan(v, &size)
	}
	inputs := map[string]string{
		"quotes": strings.Repeat(`"`, size), "backslash": strings.Repeat(`\`, size), "cstart": strings.Repeat(`/*`, size/2),
		"angle": strings.Repeat(`<`, size), "dollar": strings.Repeat(`$(`, size/2), "brace": strings.Repeat(`{`, size),
		"hash": strings.Repeat(`#`, size), "mixed": strings.Repeat(`"'`+"`"+`\/*<{($#`, size/12), "words": strings.Repeat(`a_b `, size/4),
		"nums": strings.Repeat(`1.`, size/2), "heredoc": strings.Repeat("<<EOF\n", size/6), "cmt": strings.Repeat("<!-- ", size/5),
	}
	langs := []string{"go", "python", "javascript", "typescript", "java", "c", "cpp", "rust", "ruby", "php", "sql", "yaml", "json", "xml", "html", "css", "bash", "docker", "markdown", "diff", "kotlin", "swift", "scala", "toml", "ini", "makefile", "powershell", "lua", "perl", "haskell", "csharp", "jsx", "tsx", "scss", "dart", "elixir", "terraform", "protobuf", "graphql", "nginx"}
	const budget = 200 * time.Millisecond
	var worst, worstTok time.Duration
	var wn string
	aborted := 0
	for _, l := range langs {
		lex := lexers.Get(l)
		for n, in := range inputs {
			st := time.Now()
			it, err := chroma.Coalesce(lex).Tokenise(nil, in)
			if err != nil {
				continue
			}
			for {
				t0 := time.Now()
				tok := it()
				if d := time.Since(t0); d > worstTok {
					worstTok = d
				}
				if tok == chroma.EOF {
					break
				}
				if time.Since(st) > budget {
					aborted++
					break
				}
			}
			if d := time.Since(st); d > worst {
				worst, wn = d, l+"/"+n
			}
		}
	}
	if worst > 2*time.Second*timeScale {
		t.Errorf("a 200ms budget was overrun by %v (%s): the per-block cap of 4 KiB no longer bounds one lexer call", worst, wn)
	}
	fmt.Printf("worst wall time with a %v budget: %v (%s); worst single token call: %v; aborted blocks: %d\n", budget, worst.Round(time.Millisecond), wn, worstTok.Round(time.Millisecond), aborted)
}
