package parsertest_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/parsertest"
)

type toy struct {
	bad func(call int) manifests.Parsed
	n   *int
}

func (toy) ID() string                       { return "npm/package-json" }
func (toy) Version() int                     { return 1 }
func (toy) Ecosystem() manifests.EcosystemID { return manifests.NPM }
func (toy) Match(p string) bool              { return strings.HasSuffix(p, "package.json") }
func (t toy) Parse(ctx context.Context, f manifests.File, l manifests.Limits) (manifests.Parsed, error) {
	if t.bad != nil {
		*t.n++
		return t.bad(*t.n), nil
	}
	name, deps, _ := strings.Cut(string(f.Data), ":")
	var p manifests.Parsed
	if okName(name) {
		p.Provides = []manifests.Artifact{{ID: manifests.ComponentID{Ecosystem: manifests.NPM, Name: name}, Kind: "library"}}
	}
	for _, d := range strings.Split(deps, ",") {
		if okName(d) {
			p.Dependencies = append(p.Dependencies, manifests.Dependency{Target: manifests.ComponentID{Ecosystem: manifests.NPM, Name: d}, Requirement: "^1"})
		}
	}
	return p, nil
}

type rec struct {
	testing.TB
	errs []string
}

func (r *rec) Helper()                   {}
func (r *rec) Errorf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }
func (r *rec) Fatalf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }

func TestHarnessAcceptsWellBehavedParser(t *testing.T) {
	r := &rec{}
	parsertest.CheckInvariants(r, toy{}, manifests.File{Path: "package.json", Data: []byte("a:b,c")})
	if len(r.errs) != 0 {
		t.Fatal(r.errs)
	}
}

func TestHarnessCatchesViolations(t *testing.T) {
	n := 0
	cases := map[string]func(int) manifests.Parsed{
		"nondeterministic": func(c int) manifests.Parsed {
			return manifests.Parsed{Provides: []manifests.Artifact{{ID: manifests.ComponentID{Ecosystem: manifests.NPM, Name: fmt.Sprint("a", rand.Int())}}}}
		},
		"unnormalised": func(int) manifests.Parsed {
			return manifests.Parsed{Dependencies: []manifests.Dependency{{Target: manifests.ComponentID{Ecosystem: manifests.PyPI, Name: "Foo_Bar"}}}}
		},
		"invalid id": func(int) manifests.Parsed {
			return manifests.Parsed{Dependencies: []manifests.Dependency{{Target: manifests.ComponentID{Ecosystem: manifests.NPM}}}}
		},
		"control char": func(int) manifests.Parsed {
			return manifests.Parsed{Dependencies: []manifests.Dependency{{Target: manifests.ComponentID{Ecosystem: manifests.NPM, Name: "a"}, Version: "1\x00"}}}
		},
		"absolute member": func(int) manifests.Parsed { return manifests.Parsed{Members: []string{"/etc"}} },
		"panic":           func(int) manifests.Parsed { panic("boom") },
	}
	for name, bad := range cases {
		r := &rec{}
		parsertest.CheckInvariants(r, toy{bad: bad, n: &n}, manifests.File{Path: "package.json", Data: []byte("x")})
		if len(r.errs) == 0 {
			t.Errorf("%s: violation not detected", name)
		}
	}
}

func TestGoldenAndCorpus(t *testing.T) {
	parsertest.Golden(t, toy{}, "testdata")
	if b := parsertest.Corpus.Nested(10, "[", "]"); len(b) != 20 {
		t.Fatal(len(b))
	}
	parsertest.MustNotExceed(t, toy{}, manifests.File{Path: "package.json", Data: parsertest.Corpus.Repeat("a,", 1000)}, 1e9)
}

func FuzzToy(f *testing.F) { parsertest.Fuzz(f, toy{}, []byte("a:b,c")) }

func okName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return s != ""
}
