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

// features are the packages under internal/ that never import each other; core packages import none of them.
var features = []string{"catalog", "commits", "docs", "badges", "webhook", "llms"} //nolint:gochecknoglobals // test table

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
			if target != own && slices.Contains(features, target) {
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
	got := boundaryViolations(t, root)
	if len(got) != 1 || !strings.HasPrefix(got[0], "docs/docs.go imports") {
		t.Errorf("violations = %q, want exactly docs/docs.go importing catalog", got)
	}
}
