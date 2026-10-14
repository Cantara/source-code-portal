package golang_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/golang"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

func newReg(t testing.TB) *manifests.Registry {
	t.Helper()
	reg := manifests.NewRegistry()
	if err := golang.Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestGolden(t *testing.T) {
	reg := newReg(t)
	dirs, _ := filepath.Glob("testdata/*")
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, "input")); err != nil || !st.IsDir() {
			continue
		}
		t.Run(filepath.Base(d), func(t *testing.T) { parsertest.TreeGolden(t, reg, d) })
	}
}

func FuzzParsers(f *testing.F) { parsertest.FuzzTree(f, newReg(f), "testdata") }

func TestBombs(t *testing.T) {
	var many strings.Builder
	many.WriteString("module m\ngo 1.22\nrequire (\n")
	for i := 0; i < 20000; i++ {
		many.WriteString("\texample.com/x" + strings.Repeat("y", i%7) + "/m v1.0.0\n")
	}
	many.WriteString(")\n")
	cases := []struct{ name, file, data, want string }{
		{"size", "go.mod", strings.Repeat("//", 600000), "limit.size"},
		{"nul", "go.mod", "module a\x00b\n", "encoding.unsupported"},
		{"syntax", "go.mod", "module\nrequire (\n", "golang.syntax"},
		{"many-requires", "go.mod", many.String(), ""},
		{"work-escape", "go.work", "go 1.22\nuse (\n\t../../../etc\n\t/abs\n\t./ok\n)\n", "golang.use-invalid"},
		{"replace-absolute", "go.mod", "module m\nreplace b => /abs\nrequire b v1.0.0\n", "golang.replace-absolute"},
		{"replace-escape", "go.mod", "module m\nrequire a v1.0.0\nreplace a => ../../../../etc\n", manifests.DiagPathEscape},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := newReg(t)
			start := time.Now()
			res := manifests.RunTree(context.Background(), reg, parsertest.TreeConfig(), map[string][]byte{c.file: []byte(c.data)})
			if d := time.Since(start); d > 3*time.Second {
				t.Errorf("took %v", d)
			}
			got := false
			for _, ds := range [][]manifests.Diagnostic{res.Diagnostics} {
				for _, d := range ds {
					got = got || d.Code == c.want
				}
			}
			for _, p := range res.Parsed {
				for _, d := range p.Diagnostics {
					got = got || d.Code == c.want
				}
			}
			if c.want != "" && !got {
				t.Errorf("want diagnostic %s, got parsed=%d diags=%v", c.want, len(res.Parsed), res.Diagnostics)
			}
			for _, p := range reg.Match(c.file) {
				parsertest.CheckParse(t, reg, p, c.file, []byte(c.data))
			}
			for _, u := range res.Units {
				for _, m := range u.Members {
					if strings.HasPrefix(m, "..") || strings.HasPrefix(m, "/") {
						t.Errorf("member escapes: %q", m)
					}
				}
				for _, d := range u.Dependencies {
					if r := d.SourceRef(); d.Source == manifests.SourcePath && (strings.HasPrefix(r, "..") || strings.HasPrefix(r, "/")) {
						t.Errorf("path dependency escapes: %q", r)
					}
				}
			}
		})
	}
}
