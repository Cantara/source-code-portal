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

// core are the shared packages under internal/. Every other package there is a feature: features never import each other and core
// packages import none of them, so a new package is a feature unless it is added here.
var core = []string{ //nolint:gochecknoglobals // test table
	"actor", "cluster", "config", "egress", "docstore", "ghlogin", "github", "health", "httpapi", "jsonstream", "llm", "lru", "prereq", "safehtml", "scope", "setup", "snapshot", "mcpserver", "store", "sync", "theme", "tools", "untrusted", "webui",
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

// TestSharedSiteDoesNotLinkThePersonalEdition: the shared site (`scp serve`, the prime way of deploying) must not be touched by the
// personal edition, so the packages that only the personal edition uses (its setup page, signing in to GitHub, the vault) are imported
// by its own files, personal*.go, and by no other file of the program.
func TestSharedSiteDoesNotLinkThePersonalEdition(t *testing.T) {
	personalOnly := []string{internalPrefix + "setup", internalPrefix + "ghlogin", internalPrefix + "vault"}
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
	onlyPersonal := []string{internalPrefix + "setup", internalPrefix + "ghlogin", internalPrefix + "vault", "github.com/zalando/go-keyring", "golang.org/x/term"}
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
// store.OpenAs, so a store of the other mode is always refused (internal/store/mode.go).
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
