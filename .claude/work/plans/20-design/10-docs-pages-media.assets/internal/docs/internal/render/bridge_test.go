package render

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestBridgeFixtures(t *testing.T) {
	p := New()
	for _, f := range []string{"asciidoctor_asciidoctor", "synthetic_billing-service"} {
		b, err := os.ReadFile("../../../../testdata/bridge/" + f + ".html")
		if err != nil {
			t.Skip(err)
		}
		out, err := p.Bridge(context.Background(), string(b), Input{Route: testResolver(), Format: FormatGitHubHTML})
		if err != nil {
			t.Fatal(err)
		}
		h := out.HTML.String()
		fmt.Printf("== %s: in=%d out=%d toc=%d title=%q words=%d\n", f, len(b), len(h), len(out.TOC), out.Title, out.WordCount)
		if len(out.TOC) > 3 {
			fmt.Printf("toc[0..2]=%+v\n", out.TOC[:3])
		}
		cls := map[string]int{}
		for _, m := range regexp.MustCompile(`class="([^"]+)"`).FindAllStringSubmatch(h, -1) {
			cls[m[1]]++
		}
		fmt.Printf("classes: %v\n", cls)
		for _, bad := range []string{"<svg", "user-content-", "camo.githubusercontent", "data-", "style=", "<g-emoji", "markdown-accessiblity"} {
			if strings.Contains(h, bad) {
				t.Errorf("%s: %q survived", f, bad)
			}
		}
		fmt.Println(h[:900])
	}
}
