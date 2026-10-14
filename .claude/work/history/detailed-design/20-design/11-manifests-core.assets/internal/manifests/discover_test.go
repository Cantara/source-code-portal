package manifests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/scm"
)

type stubParser struct {
	id   string
	eco  EcosystemID
	ver  int
	base string
	kind FileKind
	fn   func(f File) (Parsed, error)
}

func (s stubParser) ID() string             { return s.id }
func (s stubParser) Version() int           { return s.ver }
func (s stubParser) Ecosystem() EcosystemID { return s.eco }
func (s stubParser) Kind() FileKind         { return s.kind }
func (s stubParser) Match(p string) bool    { return p == s.base || strings.HasSuffix(p, "/"+s.base) }
func (s stubParser) Parse(ctx context.Context, f File, l Limits) (Parsed, error) {
	if s.fn != nil {
		return s.fn(f)
	}
	return Parsed{}, nil
}

func newTestRegistry(t testing.TB, ps ...Parser) *Registry {
	r := NewRegistry()
	for _, p := range ps {
		if err := r.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	r.Freeze()
	return r
}

func TestDiscover(t *testing.T) {
	reg := newTestRegistry(t,
		stubParser{id: "npm/package-json", eco: NPM, ver: 1, base: "package.json"},
		stubParser{id: "npm/package-lock", eco: NPM, ver: 1, base: "package-lock.json", kind: KindLockfile})
	entries := []scm.TreeEntry{
		{Path: "package.json", SHA: "a1", Size: 10},
		{Path: "package-lock.json", SHA: "a2", Size: 5 << 20},       // lockfile limit applies: ok
		{Path: "node_modules/x/package.json", SHA: "a3", Size: 10},  // excluded segment
		{Path: "examples/web/package.json", SHA: "a4", Size: 10},    // fixture role
		{Path: "huge/package.json", SHA: "a5", Size: 2 << 20},       // over the manifest limit
		{Path: "link/package.json", SHA: "a6", Type: scm.EntryLink}, // symlink
		{Path: "../evil/package.json", SHA: "a7"},                   // escape
		{Path: "sub", SHA: "t1", Type: scm.EntryTree},
		{Path: "a/b/c/d/e/f/g/h/i/j/k/l/m/package.json", SHA: "a8", Size: 1}, // depth 13 > 12
		{Path: "sub/module", SHA: "c1", Type: scm.EntryCommit},               // submodule
	}
	cfg, lim := DefaultConfig().Acquisition, DefaultConfig().Limits
	d := Discover(func(yield func(scm.TreeEntry) bool) {
		for _, e := range entries {
			if !yield(e) {
				return
			}
		}
	}, reg, cfg, lim)
	var got []string
	for _, c := range d.Candidates {
		got = append(got, fmt.Sprintf("%s:%d", c.Path, c.Role))
	}
	want := []string{"package-lock.json:0", "package.json:0", "examples/web/package.json:1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	codes := map[string]int{}
	for _, dg := range d.Diagnostics {
		codes[dg.Code]++
	}
	if codes[DiagLimitSize] != 1 || codes[DiagSymlink] != 1 || codes[DiagPathInvalid] != 1 {
		t.Fatalf("diagnostics %v", codes)
	}
}

func TestDiscoverFileCap(t *testing.T) {
	reg := newTestRegistry(t, stubParser{id: "npm/package-json", eco: NPM, ver: 1, base: "package.json"})
	cfg, lim := DefaultConfig().Acquisition, DefaultConfig().Limits
	cfg.MaxFiles = 3
	d := Discover(func(yield func(scm.TreeEntry) bool) {
		for i := 0; i < 10; i++ {
			yield(scm.TreeEntry{Path: fmt.Sprintf("p%d/package.json", i), SHA: "x", Size: 1})
		}
		yield(scm.TreeEntry{Path: "package.json", SHA: "r", Size: 1})
	}, reg, cfg, lim)
	if len(d.Candidates) != 3 || d.Candidates[0].Path != "package.json" || d.Skipped != 8 {
		t.Fatalf("%v skipped=%d", d.Candidates, d.Skipped)
	}
}

func TestGitBlobSHA(t *testing.T) {
	// `printf 'hello' | git hash-object --stdin` = b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0
	if !GitBlobSHA("b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0", []byte("hello")) {
		t.Fatal("sha1 blob id")
	}
	if GitBlobSHA("b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0", []byte("hellO")) || GitBlobSHA("short", nil) {
		t.Fatal("must reject")
	}
}
