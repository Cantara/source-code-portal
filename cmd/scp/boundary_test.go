package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const internalPrefix = "github.com/cantara/source-code-portal/internal/"

// layers are internal/'s four layers and what each may import of internal/, by layer: a lib package (a pure helper) nothing; a
// platform package (what the features stand on) the platform and lib; a feature (what users see) the platform, lib and its own
// packages, never another feature; an app package (what joins features: the tools, what search reads, the fleet's health) the
// features, the platform, lib and its own packages, never another app package. Nothing in internal/ imports app; cmd/scp wires.
var layers = map[string][]string{ //nolint:gochecknoglobals // test table
	"lib":      nil,
	"platform": {"platform", "lib"},
	"feature":  {"platform", "lib"},
	"app":      {"feature", "platform", "lib"},
}

// boundaryViolations returns one entry per import that breaks the layers, for the tree under root (an internal/ dir); a package
// outside the three layers is one too.
func boundaryViolations(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		layer, rest, _ := strings.Cut(rel, "/")
		own, _, _ := strings.Cut(rest, "/")
		allowed, known := layers[layer]
		if !known {
			out = append(out, rel+" is outside the layers app/, feature/, platform/ and lib/")
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			target, ok := strings.CutPrefix(path, internalPrefix)
			if !ok {
				continue
			}
			tLayer, tRest, _ := strings.Cut(target, "/")
			tOwn, _, _ := strings.Cut(tRest, "/")
			if tLayer == layer && tOwn == own || slices.Contains(allowed, tLayer) {
				continue
			}
			out = append(out, rel+" imports "+path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// TestLayersHold enforces plan rule 2: features share data through the snapshot and consumer-side interfaces, the platform and
// the helpers know no feature, and the wiring happens here in cmd/scp.
func TestLayersHold(t *testing.T) {
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
	imp := func(pkg, path string) string {
		return "package " + pkg + "\nimport _ \"" + internalPrefix + path + "\"\n"
	}
	write("feature/docs/docs.go", imp("docs", "feature/catalog"))    // a feature on another feature
	write("feature/docs/render/r.go", imp("render", "feature/docs")) // a feature's own package: fine
	write("feature/ci/ci.go", imp("ci", "platform/sync"))            // a feature on the platform: fine
	write("platform/sync/sync.go", imp("sync", "lib/lru"))           // the platform on a helper: fine
	write("platform/snapshot/s.go", imp("snapshot", "feature/ci"))   // the platform on a feature
	write("lib/lru/lru.go", imp("lru", "platform/sync"))             // a helper on the platform
	write("app/tools/t.go", imp("tools", "feature/docs"))            // app on a feature: fine
	write("app/feed/f.go", imp("feed", "app/tools"))                 // app on another app package
	write("feature/ci/app.go", imp("ci", "app/tools"))               // a feature on app
	write("stray/x.go", "package stray\n")                           // outside the layers
	got := boundaryViolations(t, root)
	slices.Sort(got)
	want := []string{"app/feed/f.go imports", "feature/ci/app.go imports", "feature/docs/docs.go imports", "lib/lru/lru.go imports", "platform/snapshot/s.go imports", "stray/x.go is outside"}
	if len(got) != len(want) {
		t.Fatalf("violations = %q, want %d", got, len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("violation %d = %q, want %q", i, got[i], w)
		}
	}
}

// TestSharedSiteDoesNotLinkThePersonalEdition: the shared site (`scp serve`, the prime way of deploying) must not be touched by the
// personal edition, so the packages that only the personal edition uses (its setup page, signing in to GitHub, the vault) are imported
// by its own files, personal*.go, and by no other file of the program.
func TestSharedSiteDoesNotLinkThePersonalEdition(t *testing.T) {
	personalOnly := []string{internalPrefix + "platform/setup", internalPrefix + "platform/ghlogin", internalPrefix + "platform/vault"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasPrefix(f, "personal") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if slices.Contains(personalOnly, path) {
				t.Errorf("%s imports %s: only personal*.go may", f, path)
			}
		}
	}
}

// TestTheServerBuildHoldsNothingOfThePersonalEdition: built with -tags server (the container image, a server archive) the program
// links none of the personal edition: not its setup page, its sign-in to GitHub, its vault, nor the libraries only they use (the
// keychain, the terminal). The full build, which has all of them, is the control: if it stopped having them the test would prove nothing.
func TestTheServerBuildHoldsNothingOfThePersonalEdition(t *testing.T) {
	deps := func(tags ...string) []string {
		args := append([]string{"list"}, tags...)
		out, err := exec.CommandContext(t.Context(), "go", append(args, "-deps", "-f", "{{.ImportPath}}", ".")...).Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		return strings.Fields(string(out))
	}
	onlyPersonal := []string{internalPrefix + "platform/setup", internalPrefix + "platform/ghlogin", internalPrefix + "platform/vault", "github.com/zalando/go-keyring", "golang.org/x/term"}
	full, server := deps(), deps("-tags", "server")
	for _, p := range onlyPersonal {
		if !slices.Contains(full, p) {
			t.Errorf("the full build does not link %s: the control is empty", p)
		}
		if slices.Contains(server, p) {
			t.Errorf("the server build links %s", p)
		}
	}
}

// TestTheProgramOpensTheStoreForAMode: store.Open claims no mode and is for tests; everything the program runs opens the store with
// store.OpenAs, so a store of the other mode is always refused (internal/platform/store/mode.go).
func TestTheProgramOpensTheStoreForAMode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "store.Open(") {
			t.Errorf("%s opens the store without a mode: use store.OpenAs", f)
		}
		opened += strings.Count(string(b), "store.OpenAs(")
	}
	if opened < 3 {
		t.Errorf("only %d store.OpenAs calls found: the check is looking in the wrong place", opened)
	}
}
