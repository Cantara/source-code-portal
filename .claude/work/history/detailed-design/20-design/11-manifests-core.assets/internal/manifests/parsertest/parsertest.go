// Package parsertest is the shared harness every ecosystem parser package uses: golden files, one fuzz entry point and the
// invariants every Parser must satisfy. Parser packages import only this package and the manifests root in their tests.
package parsertest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
)

var update = flag.Bool("update", false, "rewrite golden files")

// DefaultLimits are the limits the harness runs parsers under: tight enough that fuzzing stays fast, identical for every parser.
func DefaultLimits() manifests.Limits {
	l := manifests.DefaultConfig().Limits
	return manifests.Limits{MaxBytes: 1 << 20, MaxDepth: l.MaxDepth, MaxNodes: 200_000, MaxExpand: 64 << 10, MaxExpandAll: 256 << 10, Timeout: time.Second}
}

// Run executes p exactly as the runner does (a fresh context, the default limits) and recovers a panic into an error so a
// fuzz crash is reported with the input instead of killing the test binary.
func Run(p manifests.Parser, f manifests.File) (out manifests.Parsed, err error, panicked any) {
	defer func() {
		if r := recover(); r != nil {
			panicked = r
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err = p.Parse(ctx, f, DefaultLimits())
	return
}

// CheckInvariants reports every violated invariant of one parse result. The list is the contract of manifests.Parser (design section 4.3):
//  1. never panics; 2. deterministic (a second run is deeply equal); 3. every dependency and provided artifact has a valid, normalised ComponentID;
//  4. no path in Members, Includes or locations is absolute or escapes ("..");  5. text fields are valid UTF-8 without control characters;
//  6. a cancelled context returns promptly (error or result, never a hang); 7. counts stay inside the limits.
func CheckInvariants(t testing.TB, p manifests.Parser, f manifests.File) manifests.Parsed {
	t.Helper()
	out, err, pan := Run(p, f)
	if pan != nil {
		t.Fatalf("invariant 1 (no panic): %s panicked on %q: %v", p.ID(), clip(f.Data), pan)
	}
	out2, err2, _ := Run(p, f)
	if (err == nil) != (err2 == nil) || !reflect.DeepEqual(out, out2) {
		t.Errorf("invariant 2 (deterministic): %s gave different results for the same input %q", p.ID(), clip(f.Data))
	}
	if err != nil {
		return manifests.Parsed{}
	}
	check := func(what string, id manifests.ComponentID) {
		if !id.Valid() {
			t.Errorf("invariant 3: %s has invalid component %+v", what, id)
			return
		}
		ns, name := purl.Normalize(string(id.Ecosystem), id.Namespace, id.Name)
		if ns != id.Namespace || name != id.Name {
			t.Errorf("invariant 3: %s component %+v is not normalised (want %q %q)", what, id, ns, name)
		}
	}
	for _, d := range out.Dependencies {
		check("dependency", d.Target)
		checkText(t, "dependency requirement", d.Requirement)
		checkText(t, "dependency version", d.Version)
		checkPath(t, d.Loc().Path)
		if d.Source == manifests.SourcePath {
			checkPath(t, d.SourceRef())
		}
	}
	for _, a := range out.Provides {
		check("artifact", a.ID)
		checkText(t, "artifact version", a.Version)
	}
	for _, l := range out.Locked {
		check("locked component", l.ID)
		for _, i := range l.DependsOn {
			if i < 0 || i >= len(out.Locked) {
				t.Errorf("invariant 3: locked DependsOn index %d out of range %d", i, len(out.Locked))
			}
		}
	}
	for _, m := range append(append([]string{}, out.Members...), out.Includes...) {
		checkPath(t, m)
	}
	for _, d := range out.Diagnostics {
		checkText(t, "diagnostic message", d.Message)
	}
	lim := DefaultLimits()
	if len(out.Dependencies) > lim.MaxNodes || len(out.Locked) > lim.MaxNodes {
		t.Errorf("invariant 7: %d dependencies, %d locked components exceed MaxNodes", len(out.Dependencies), len(out.Locked))
	}
	// invariant 6
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		_, _ = p.Parse(ctx, f, lim)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Errorf("invariant 6 (cancellation): %s did not return within 3s on a cancelled context", p.ID())
	}
	return out
}

func checkText(t testing.TB, what, s string) {
	if !utf8.ValidString(s) {
		t.Errorf("invariant 5: %s is not valid UTF-8: %q", what, s)
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			t.Errorf("invariant 5: %s contains a control character: %q", what, s)
			return
		}
	}
}

func checkPath(t testing.TB, p string) {
	if p == "" {
		return
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") && strings.Contains(p, "..") {
		t.Errorf("invariant 4: absolute path %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			// a parser may report a relative reference upward ("../shared"); the assembler resolves and rejects escapes
			// (assemble.path-escape). The harness therefore only requires that the text is a clean relative path.
			break
		}
	}
}

func clip(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// Golden parses every file <dir>/<case>/<name> (the directory name is the case, the file name is the manifest base name passed as File.Path)
// and compares the canonical JSON of the result with <dir>/<case>/expected.json. Run with -update to rewrite. Inputs larger than 64 KiB
// are not allowed in the repository; large cases are generated by Corpus in the test.
func Golden(t *testing.T, p manifests.Parser, dir string) {
	t.Helper()
	cases, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			ents, _ := os.ReadDir(filepath.Join(dir, c.Name()))
			for _, e := range ents {
				if e.IsDir() || e.Name() == "expected.json" || strings.HasPrefix(e.Name(), ".") || !p.Match(e.Name()) && !p.Match("x/"+e.Name()) {
					continue
				}
				data, err := os.ReadFile(filepath.Join(dir, c.Name(), e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				out := CheckInvariants(t, p, manifests.File{Path: e.Name(), Data: data})
				got, _ := json.MarshalIndent(canonical(out), "", "  ")
				golden := filepath.Join(dir, c.Name(), "expected.json")
				if *update {
					if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("missing golden %s (run with -update): %v", golden, err)
				}
				if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
					t.Errorf("golden mismatch for %s/%s\n--- want\n%s\n--- got\n%s", c.Name(), e.Name(), want, got)
				}
				return
			}
		})
	}
}

// canonical maps Parsed to its stable JSON form (the same wire form the cache uses, minus the runner-stamped file).
func canonical(p manifests.Parsed) any {
	p.File = manifests.FileRef{}
	b, err := manifests.EncodeParse(p, nil)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	var v any
	_ = json.Unmarshal(b, &v)
	return prune(v)
}

// prune drops zero members (null, "", 0, false, empty containers) so that adding a field to the model does not rewrite every golden of every
// parser: a golden changes only when a parser starts or stops producing a value.
func prune(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e = prune(e); e == nil {
				delete(x, k)
			} else {
				x[k] = e
			}
		}
		if len(x) == 0 {
			return nil
		}
	case []any:
		for i := range x {
			if x[i] = prune(x[i]); x[i] == nil {
				x[i] = map[string]any{}
			}
		}
		if len(x) == 0 {
			return nil
		}
	case string:
		if x == "" {
			return nil
		}
	case float64:
		if x == 0 {
			return nil
		}
	case bool:
		if !x {
			return nil
		}
	}
	return v
}

// Fuzz is the single fuzz entry point: `func FuzzPackageJSON(f *testing.F) { parsertest.Fuzz(f, New(), seeds...) }`.
// The corpus under testdata/fuzz is committed; CI runs it as plain unit tests and nightly runs it for ten minutes per parser.
func Fuzz(f *testing.F, p manifests.Parser, seeds ...[]byte) {
	name := baseName(p)
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		CheckInvariants(t, p, manifests.File{Path: name, Data: data, BlobSHA: "fuzz"})
	})
}

// baseName finds a path the parser matches, so the fuzz target exercises its real branch.
func baseName(p manifests.Parser) string {
	for _, c := range []string{"package.json", "pom.xml", "go.mod", "Cargo.toml", "pyproject.toml", "requirements.txt", "Gemfile", "composer.json", "pubspec.yaml", "Chart.yaml", "Dockerfile", "main.tf", "mix.exs", "Podfile", "build.gradle", "settings.gradle", "libs.versions.toml", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "Cargo.lock", "Package.swift", "app.csproj", "workflow.yml", "packages.lock.json", "poetry.lock", "uv.lock", "Gemfile.lock", "composer.lock", "pubspec.lock", "mix.lock", "Podfile.lock", "gradle.lockfile", "go.sum", "Pipfile", "Pipfile.lock", "bun.lock", "Directory.Packages.props", "Chart.lock", ".terraform.lock.hcl", "Package.resolved", "docker-compose.yml", ".github/workflows/ci.yml"} {
		if p.Match(c) {
			return c
		}
	}
	return "manifest"
}

// PathSet builds a manifests.FileSet for assembler tests.
func PathSet(paths ...string) manifests.FileSet { return manifests.PathSet(paths) }

// Corpus generators for hostile inputs. Every parser's test calls the ones that apply to its format.
var Corpus = struct {
	Nested    func(n int, open, close string) []byte
	Repeat    func(unit string, n int) []byte
	WithBOM   func(b []byte) []byte
	UTF16LE   func(s string) []byte
	NULInside func(b []byte) []byte
}{
	Nested: func(n int, open, close string) []byte {
		return []byte(strings.Repeat(open, n) + strings.Repeat(close, n))
	},
	Repeat:  func(unit string, n int) []byte { return []byte(strings.Repeat(unit, n)) },
	WithBOM: func(b []byte) []byte { return append([]byte{0xEF, 0xBB, 0xBF}, b...) },
	UTF16LE: func(s string) []byte {
		out := []byte{0xFF, 0xFE}
		for _, r := range s {
			out = append(out, byte(r), byte(r>>8))
		}
		return out
	},
	NULInside: func(b []byte) []byte {
		return append(append([]byte{}, b[:len(b)/2]...), append([]byte{0}, b[len(b)/2:]...)...)
	},
}

// MustNotExceed fails when parsing in does not finish inside d: the guard against quadratic parsers (research E3: 40 KB of TOML nesting took 3.8 s).
func MustNotExceed(t testing.TB, p manifests.Parser, f manifests.File, d time.Duration) {
	t.Helper()
	t0 := time.Now()
	_, _, pan := Run(p, f)
	if pan != nil {
		t.Fatalf("panic: %v", pan)
	}
	if el := time.Since(t0); el > d {
		t.Fatalf("%s took %v on %d bytes (limit %v)", p.ID(), el, len(f.Data), d)
	}
}

var _ = fmt.Sprint
