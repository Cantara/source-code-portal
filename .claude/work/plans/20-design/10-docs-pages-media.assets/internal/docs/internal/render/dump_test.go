package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const probe = "# Héllo Wörld ø æ å\n\n| a | b |\n|:-|-:|\n| 1 | 2 |\n\n> [!NOTE]\n> hello\n\n- [x] done\n- [ ] todo\n\n```go\nfunc main() {}\n```\n\n[ext](https://example.com) [rel](docs/a.md#Setup) ![i](img/a.png)\n\nText[^1]\n\n[^1]: note\n\n<details><summary>s</summary>\n\nbody\n\n</details>\n\n<picture><source media=\"(prefers-color-scheme: dark)\" srcset=\"https://example.com/d.png\"><img src=\"https://example.com/l.png\" width=\"100\" alt=\"x\"></picture>\n"

func TestDump(t *testing.T) {
	dir := os.Getenv("DUMPDIR")
	if dir == "" {
		t.Skip()
	}
	os.MkdirAll(dir, 0o755)
	p := New()
	files, _ := filepath.Glob("../../../../testdata/md/*")
	srcs := map[string][]byte{"probe.md": []byte(probe)}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		srcs[filepath.Base(f)] = b
	}
	for n, b := range srcs {
		out, _ := p.Markdown(context.Background(), Input{Source: b, Route: testResolver(), Limits: Limits{MaxSourceBytes: 512000}})
		os.WriteFile(filepath.Join(dir, n+".html"), []byte(out.HTML.String()), 0o644)
	}
}
