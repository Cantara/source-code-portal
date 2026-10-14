package npm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/npm"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

func TestGolden(t *testing.T) {
	reg := manifests.NewRegistry()
	if err := npm.Register(reg); err != nil {
		t.Fatal(err)
	}
	dirs, _ := filepath.Glob("testdata/*")
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, "input")); err != nil || !st.IsDir() {
			continue
		}
		t.Run(filepath.Base(d), func(t *testing.T) { parsertest.TreeGolden(t, reg, d) })
	}
}

func newReg(t testing.TB) *manifests.Registry {
	reg := manifests.NewRegistry()
	if err := npm.Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func FuzzParsers(f *testing.F) {
	parsertest.FuzzTree(f, newReg(f), "testdata")
}
