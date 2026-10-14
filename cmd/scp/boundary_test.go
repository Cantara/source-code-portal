package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// core are the shared packages under internal/. Every other package there is a feature: features never import each other and core
// packages import none of them, so a new package is a feature unless it is added here.
var core = []string{ //nolint:gochecknoglobals // test table
	"cluster", "config", "github", "httpapi", "safehtml", "snapshot", "store", "sync", "untrusted", "webui",
}

const internalPrefix = "github.com/cantara/source-code-portal/internal/"

// boundaryViolations returns one entry per import of a feature from outside that feature, for the tree under root (an internal/ dir).
func boundaryViolations(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		own, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			rest, ok := strings.CutPrefix(path, internalPrefix)
			if !ok {
				continue
			}
			target, _, _ := strings.Cut(rest, "/")
			if target != own && !slices.Contains(core, target) {
				out = append(out, filepath.ToSlash(rel)+" imports "+path)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// TestFeaturesDoNotImportFeatures enforces plan rule 2: features share data through the snapshot and consumer-side interfaces,
// and the wiring happens here in cmd/scp.
func TestFeaturesDoNotImportFeatures(t *testing.T) {
	for _, v := range boundaryViolations(t, filepath.Join("..", "..", "internal")) {
		t.Error(v)
	}
}

func TestBoundaryCheckFindsViolation(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/docs.go", "package docs\nimport _ \""+internalPrefix+"catalog\"\n")
	write("docs/render/r.go", "package render\nimport _ \""+internalPrefix+"docs\"\n")
	write("sync/sync.go", "package sync\nimport _ \""+internalPrefix+"snapshot\"\n")
	write("ci/ci.go", "package ci\nimport _ \""+internalPrefix+"sync\"\n")          // a new feature on core: fine
	write("snapshot/s.go", "package snapshot\nimport _ \""+internalPrefix+"ci\"\n") // core on a new feature
	got := boundaryViolations(t, root)
	slices.Sort(got)
	if len(got) != 2 || !strings.HasPrefix(got[0], "docs/docs.go imports") || !strings.HasPrefix(got[1], "snapshot/s.go imports") {
		t.Errorf("violations = %q, want docs/docs.go importing catalog and snapshot/s.go importing ci", got)
	}
}
