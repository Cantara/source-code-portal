package parsertest

// ADDED by unit 12 (requirement RR-A4, owner: unit 11): the tree-level harness used by the ecosystem packages that have an assembler.
//
//   - TreeGolden runs one case directory (input/ tree + expected.golden.json) through manifests.RunTree and compares a readable view.
//   - FuzzTree is the shared fuzz body for a package with several parsers: one fuzz argument selects a seed name, the bytes are fuzzed.
//   - CheckParse applies the eight invariants to one parse: 1 no panic, 2 bounded time and output, 3 deterministic, 4 valid idempotent
//     identities and purls, 5 locations in range, 6 clean UTF-8 text, 7 declared diagnostics only, 8 path independence.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
)

var updateTree = flag.Bool("update-tree", false, "rewrite expected.golden.json files of tree cases")

// LoadTree reads <dir>/input into a path -> bytes map.
func LoadTree(dir string) (map[string][]byte, error) {
	root := filepath.Join(dir, "input")
	m := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		m[filepath.ToSlash(rel)] = b
		return err
	})
	return m, err
}

// View is the readable golden form of a TreeResult: enums as words, one object per dependency, counts instead of locked lists inside parsed files.
type View struct {
	Parsed      []ViewParsed `json:"parsed"`
	Units       []ViewUnit   `json:"units"`
	Diagnostics []ViewDiag   `json:"diagnostics,omitempty"`
}

type ViewParsed struct {
	File         string            `json:"file"`
	Parser       string            `json:"parser"`
	Kind         string            `json:"kind"`
	Provides     []ViewArtifact    `json:"provides,omitempty"`
	Parent       *ViewDep          `json:"parent,omitempty"`
	Imports      []ViewDep         `json:"imports,omitempty"`
	Dependencies []ViewDep         `json:"dependencies,omitempty"`
	LockedCount  int               `json:"lockedCount,omitempty"`
	Properties   map[string]string `json:"properties,omitempty"`
	Members      []string          `json:"members,omitempty"`
	Includes     []string          `json:"includes,omitempty"`
	Toolchain    map[string]string `json:"toolchain,omitempty"`
	Licenses     []string          `json:"licenses,omitempty"`
	Diagnostics  []ViewDiag        `json:"diagnostics,omitempty"`
}

type ViewUnit struct {
	ID           string            `json:"id"`
	Ecosystem    string            `json:"ecosystem"`
	Dir          string            `json:"dir"`
	Role         string            `json:"role"`
	Files        []string          `json:"files"`
	Provides     []ViewArtifact    `json:"provides,omitempty"`
	Dependencies []ViewDep         `json:"dependencies,omitempty"`
	Locked       []ViewLocked      `json:"locked,omitempty"`
	Members      []string          `json:"members,omitempty"`
	Toolchain    map[string]string `json:"toolchain,omitempty"`
	Licenses     []string          `json:"licenses,omitempty"`
	Diagnostics  []ViewDiag        `json:"diagnostics,omitempty"`
}

type ViewArtifact struct {
	ID      string `json:"id"`
	Display string `json:"display,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

type ViewDep struct {
	Target      string `json:"target"`
	Display     string `json:"display,omitempty"`
	Requirement string `json:"requirement,omitempty"`
	Version     string `json:"version,omitempty"`
	Resolution  string `json:"resolution"`
	Scope       string `json:"scope"`
	Group       string `json:"group,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
	Condition   string `json:"condition,omitempty"`
	Qualifiers  string `json:"qualifiers,omitempty"`
	Source      string `json:"source"`
	SourceRef   string `json:"sourceRef,omitempty"`
	Confidence  string `json:"confidence"`
	LocPath     string `json:"locPath,omitempty"`
	Line        int    `json:"line,omitempty"`
}

type ViewLocked struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Relationship string   `json:"relationship"`
	Scope        string   `json:"scope"`
	Integrity    string   `json:"integrity,omitempty"`
	Licenses     []string `json:"licenses,omitempty"`
	DependsOn    []int    `json:"dependsOn,omitempty"`
	Line         int      `json:"line,omitempty"`
}

type ViewDiag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message,omitempty"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
}

var kindNames = [...]string{"manifest", "lockfile", "catalog", "workspace"}

func kindName(k manifests.FileKind) string { return name(kindNames[:], int(k)) }

var resNames = [...]string{"declared", "pinned", "locked", "managed"}
var relNames = [...]string{"unknown", "direct", "transitive"}
var srcNames = [...]string{"registry", "git", "path", "url", "project"}

func name(tbl []string, i int) string {
	if i >= 0 && i < len(tbl) {
		return tbl[i]
	}
	return fmt.Sprint(i)
}

func vdep(d manifests.Dependency) ViewDep {
	conf := "exact"
	if d.Confidence == manifests.ConfHeuristic {
		conf = "heuristic"
	}
	return ViewDep{Target: d.Target.String(), Display: d.TargetDisplay(), Requirement: d.Requirement, Version: d.Version,
		Resolution: name(resNames[:], int(d.Resolution)), Scope: d.Scope.String(), Group: d.Group, Optional: d.Optional, Condition: d.Condition(),
		Qualifiers: d.Qualifiers(), Source: name(srcNames[:], int(d.Source)), SourceRef: d.SourceRef(), Confidence: conf, LocPath: d.Loc().Path, Line: int(d.Line)}
}

func vdeps(ds []manifests.Dependency) []ViewDep {
	var out []ViewDep
	for _, d := range ds {
		out = append(out, vdep(d))
	}
	return out
}

func vart(as []manifests.Artifact) []ViewArtifact {
	var out []ViewArtifact
	for _, a := range as {
		out = append(out, ViewArtifact{ID: a.ID.String(), Display: a.Display, Version: a.Version, Kind: a.Kind})
	}
	return out
}

func vdiags(ds []manifests.Diagnostic) []ViewDiag {
	var out []ViewDiag
	for _, d := range ds {
		out = append(out, ViewDiag{Code: d.Code, Severity: d.Severity.String(), Message: d.Message, Path: d.Loc.Path, Line: d.Loc.Line})
	}
	return out
}

// ViewOf converts a TreeResult to its golden view.
func ViewOf(r manifests.TreeResult) View {
	var v View
	for _, p := range r.Parsed {
		vp := ViewParsed{File: p.File.Path, Parser: p.File.ParserID, Kind: kindName(p.Kind), Provides: vart(p.Provides), Imports: vdeps(p.Imports),
			Dependencies: vdeps(p.Dependencies), LockedCount: len(p.Locked), Properties: p.Properties, Members: p.Members, Includes: p.Includes,
			Toolchain: p.Toolchain, Licenses: p.Licenses, Diagnostics: vdiags(p.Diagnostics)}
		if p.Parent != nil {
			d := vdep(*p.Parent)
			vp.Parent = &d
		}
		v.Parsed = append(v.Parsed, vp)
	}
	for _, u := range r.Units {
		vu := ViewUnit{ID: u.ID, Ecosystem: string(u.Ecosystem), Dir: u.Dir, Role: u.Role.String(), Provides: vart(u.Provides), Dependencies: vdeps(u.Dependencies),
			Members: u.Members, Toolchain: u.Toolchain, Licenses: u.Licenses, Diagnostics: vdiags(u.Diagnostics)}
		for _, f := range u.Files {
			vu.Files = append(vu.Files, f.Path+" ("+f.ParserID+")")
		}
		for _, l := range u.Locked {
			vu.Locked = append(vu.Locked, ViewLocked{ID: l.ID.String(), Version: l.Version, Relationship: name(relNames[:], int(l.Relationship)), Scope: l.Scope.String(),
				Integrity: l.Integrity, Licenses: l.Licenses, DependsOn: l.DependsOn, Line: l.Loc.Line})
		}
		v.Units = append(v.Units, vu)
	}
	v.Diagnostics = vdiags(r.Diagnostics)
	return v
}

// TreeConfig is the configuration tree tests run under: the documented defaults with smaller per-parse limits.
func TreeConfig() manifests.Config {
	c := manifests.DefaultConfig()
	c.Limits.ParseTimeout = 10 * time.Second
	c.Limits.AssembleTimeout = 20 * time.Second
	return c
}

// TreeConfigLimits is TreeConfig with the non-zero fields of l applied to the limit configuration (adversarial tests lower single limits).
func TreeConfigLimits(l manifests.Limits) manifests.Config {
	c := TreeConfig()
	if l.MaxBytes > 0 {
		c.Limits.ManifestBytes, c.Limits.LockfileBytes = manifests.ByteSize(l.MaxBytes), manifests.ByteSize(l.MaxBytes)
	}
	if l.MaxDepth > 0 {
		c.Limits.MaxDepth = l.MaxDepth
	}
	if l.MaxNodes > 0 {
		c.Limits.MaxNodes = l.MaxNodes
	}
	if l.MaxExpand > 0 {
		c.Limits.MaxExpand = manifests.ByteSize(l.MaxExpand)
	}
	if l.MaxExpandAll > 0 {
		c.Limits.MaxExpandFile = manifests.ByteSize(l.MaxExpandAll)
	}
	if l.Timeout > 0 {
		c.Limits.ParseTimeout = l.Timeout
	}
	return c
}

// TreeGolden runs <dir>/input through RunTree and compares the JSON view with <dir>/expected.golden.json.
func TreeGolden(t *testing.T, reg *manifests.Registry, dir string) manifests.TreeResult {
	t.Helper()
	files, err := LoadTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := manifests.RunTree(context.Background(), reg, TreeConfig(), files)
	CheckTree(t, reg, res)
	got, err := json.MarshalIndent(ViewOf(res), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	for i := 0; i < 2; i++ { // assembler determinism: Go randomises map iteration per range statement, so a leak shows up within a few runs
		again, _ := json.MarshalIndent(ViewOf(manifests.RunTree(context.Background(), reg, TreeConfig(), files)), "", "  ")
		if !bytes.Equal(append(again, '\n'), got) {
			t.Fatalf("invariant 3 (assembler determinism): two runs over %s differ", dir)
		}
	}
	gp := filepath.Join(dir, "expected.golden.json")
	if *updateTree {
		if err := os.WriteFile(gp, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return res
	}
	want, err := os.ReadFile(gp)
	if err != nil {
		t.Fatalf("missing golden (run with -update-tree): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch for %s (run with -update-tree and review the diff)\n--- got (first 6000 bytes) ---\n%s", dir, truncate(string(got), 6000))
	}
	return res
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}

// CheckTree applies the invariants that hold for assembled output: declared diagnostics, clean text, unit ids unique, locked edges in range.
func CheckTree(t testing.TB, reg *manifests.Registry, res manifests.TreeResult) {
	t.Helper()
	checkTree(t, reg, res, true)
}

// checkTree is CheckTree; strict=false (fuzzing) tolerates parse.failed, which hostile input may legitimately cause.
func checkTree(t testing.TB, reg *manifests.Registry, res manifests.TreeResult, strict bool) {
	t.Helper()
	declared := declaredCodes(reg)
	ids := map[string]bool{}
	for _, u := range res.Units {
		if ids[u.ID] {
			t.Errorf("duplicate unit id %q", u.ID)
		}
		ids[u.ID] = true
		for _, d := range u.Diagnostics {
			if !declared[d.Code] {
				t.Errorf("unit %s emits undeclared diagnostic %q", u.ID, d.Code)
			}
			cleanText(t, "unit diagnostic", d.Message)
		}
		for i, l := range u.Locked {
			for _, e := range l.DependsOn {
				if e < 0 || e >= len(u.Locked) || e == i {
					t.Errorf("unit %s locked[%d] has bad edge %d", u.ID, i, e)
				}
			}
		}
		for _, d := range u.Dependencies {
			cleanText(t, "unit dependency", d.Target.Name+d.Target.Namespace+d.Requirement+d.Version+d.SourceRef()+d.TargetDisplay()+d.Condition())
			if d.Source == manifests.SourceProject {
				t.Errorf("unit %s still has an unresolved project dependency %v", u.ID, d.Target)
			}
		}
	}
	for _, d := range res.Diagnostics {
		if !declared[d.Code] {
			t.Errorf("runner diagnostic %q is neither declared by an ecosystem nor a core code", d.Code)
		}
		switch d.Code {
		case "assemble.mutated-input", manifests.DiagParsePanic, manifests.DiagAssembleFailed:
			t.Errorf("runner diagnostic %q (%s) must never appear", d.Code, d.Message)
		case manifests.DiagParseFailed:
			if strict {
				t.Errorf("runner diagnostic %q (%s) must never appear in a golden case", d.Code, d.Message)
			}
		}
	}
}

// coreCodes are the diagnostic codes owned by the root (discover.go) and by the shared limit handling.
func coreCodes() []string {
	return []string{manifests.DiagLimitFiles, manifests.DiagLimitSize, manifests.DiagLimitTreeCalls, manifests.DiagLimitTreeEntry, manifests.DiagTreeTruncated,
		manifests.DiagPathInvalid, manifests.DiagSymlink, manifests.DiagSubmodule, manifests.DiagEncoding, manifests.DiagFetchFailed, manifests.DiagSHAMismatch,
		manifests.DiagBinary, manifests.DiagParseFailed, manifests.DiagParsePanic, manifests.DiagParseAbandoned, manifests.DiagCircuitOpen,
		manifests.DiagAssembleFailed, manifests.DiagPathEscape, manifests.DiagUnitCap, manifests.DiagDepsCap, manifests.DiagLockedCap,
		"parse.invalid-dependency", "limit.depth", "limit.nodes", "limit.expand", "limit.timeout", "lock.missing", "lock.stale", "assemble.mutated-input"}
}

func declaredCodes(reg *manifests.Registry) map[string]bool {
	m := map[string]bool{}
	for _, c := range coreCodes() {
		m[c] = true
	}
	for _, id := range reg.ParserIDs() {
		p, _ := reg.Parser(id)
		if s, ok := reg.Spec(p.Ecosystem()); ok {
			for _, d := range s.Diagnostics {
				m[d.Code] = true
			}
		}
	}
	return m
}

// Seed is one fuzz seed: a file path (selects the parsers through Match) and its content.
type Seed struct {
	Name string
	Data []byte
}

// Seeds collects every file below root/*/input that at least one registered parser matches.
func Seeds(reg *manifests.Registry, root string) []Seed {
	var out []Seed
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		sp := filepath.ToSlash(p)
		if err != nil || d.IsDir() || !strings.Contains(sp, "/input/") {
			return nil
		}
		rel := sp[strings.Index(sp, "/input/")+len("/input/"):]
		if len(reg.Match(rel)) == 0 {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 200_000 {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			out = append(out, Seed{rel, b})
		}
		return nil
	})
	slices.SortFunc(out, func(a, b Seed) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// FuzzTree is the shared fuzz body: `func FuzzParsers(f *testing.F) { parsertest.FuzzTree(f, newReg(f), "../../testdata/<eco>") }`.
func FuzzTree(f *testing.F, reg *manifests.Registry, seedRoot string) {
	seeds := Seeds(reg, seedRoot)
	names := make([]string, 0, len(seeds))
	idx := map[string]uint8{}
	for _, s := range seeds {
		if _, ok := idx[s.Name]; !ok && len(names) < 255 {
			idx[s.Name] = uint8(len(names))
			names = append(names, s.Name)
		}
		f.Add(idx[s.Name], s.Data)
	}
	f.Fuzz(func(t *testing.T, sel uint8, data []byte) {
		if len(names) == 0 || len(data) > 1<<16 {
			return
		}
		name := names[int(sel)%len(names)]
		for _, p := range reg.Match(name) {
			CheckParse(t, reg, p, name, data)
		}
		// the assembler on a one-file tree: no panic, no input mutation, deterministic, declared diagnostics only
		r1 := manifests.RunTree(context.Background(), reg, TreeConfig(), map[string][]byte{name: data})
		r2 := manifests.RunTree(context.Background(), reg, TreeConfig(), map[string][]byte{name: data})
		j1, _ := json.Marshal(ViewOf(r1))
		j2, _ := json.Marshal(ViewOf(r2))
		if !bytes.Equal(j1, j2) {
			t.Fatalf("invariant 3 (assembler determinism) for %s:\n%s\n%s", name, j1, j2)
		}
		checkTree(t, reg, r1, false)
	})
}

// FuzzLimits are the limits fuzzing runs under: tight enough to keep iterations fast, identical for every parser.
func FuzzLimits() manifests.Limits {
	return manifests.Limits{MaxBytes: 1 << 20, MaxDepth: 256, MaxNodes: 200_000, MaxExpand: 64 << 10, MaxExpandAll: 256 << 10, Timeout: 2 * time.Second}
}

// CheckParse applies the eight invariants to one parse of data under the file name name (a path the parser matches).
func CheckParse(t testing.TB, reg *manifests.Registry, p manifests.Parser, name string, data []byte) manifests.Parsed {
	t.Helper()
	run := func(fp string) (out manifests.Parsed) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("invariant 1 (no panic): %s on %s panicked: %v", p.ID(), fp, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		start := time.Now()
		// the runner strips the BOM and rejects binary before calling a parser
		text := data
		out, err := p.Parse(ctx, manifests.File{Path: fp, Data: text, BlobSHA: "x"}, FuzzLimits())
		if err != nil && ctx.Err() == nil {
			// typed limit and encoding errors are legitimate returns
			if !isTypedLimit(err) {
				t.Fatalf("invariant 1: parser returned an untyped error without cancellation: %v", err)
			}
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("invariant 2 (bounded time): %s took %v", p.ID(), d)
		}
		return out
	}
	a := run(name)
	b := run(name)
	if !reflect.DeepEqual(a, b) {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		t.Fatalf("invariant 3 (determinism): %s differs between two runs:\n%s\n%s", p.ID(), ja, jb)
	}
	// 8: path independence (the cache key has no path)
	base := path.Base(name)
	c := run("deep/er/" + base)
	if !reflect.DeepEqual(a, c) {
		ja, _ := json.Marshal(a)
		jc, _ := json.Marshal(c)
		t.Fatalf("invariant 8 (path independence): %s output depends on the path:\n%s\n%s", p.ID(), ja, jc)
	}
	spec, hasSpec := reg.Spec(p.Ecosystem())
	declared := declaredCodes(reg)
	lines := bytes.Count(data, []byte{'\n'}) + 2
	idOK := func(what string, id manifests.ComponentID) {
		if !id.Valid() {
			return // sanitizeParsed drops invalid components; not a parser defect by itself
		}
		eco := string(id.Ecosystem)
		if eco == "" {
			eco = string(p.Ecosystem())
		}
		ns, n := purl.Normalize(eco, id.Namespace, id.Name)
		if hasSpec && id.Ecosystem == spec.ID && spec.Normalize != nil {
			ns, n = spec.Normalize(id.Namespace, id.Name)
		}
		ns2, n2 := purl.Normalize(eco, ns, n)
		if hasSpec && id.Ecosystem == spec.ID && spec.Normalize != nil {
			ns2, n2 = spec.Normalize(ns, n)
		}
		if ns != ns2 || n != n2 {
			t.Fatalf("invariant 4 (idempotent identity): %s %q/%q normalises to %q/%q then %q/%q", what, id.Namespace, id.Name, ns, n, ns2, n2)
		}
		if s, err := purl.Build(eco, ns, n, "1.0", ""); err != nil {
			t.Fatalf("invariant 4: %s %q/%q has no valid purl: %v", what, ns, n, err)
		} else if parts, err := purl.Parse(s); err == nil && (parts.Namespace != ns || parts.Name != n) && eco != "terraform" && eco != "helm" {
			t.Fatalf("invariant 4 (purl round trip): %s %q/%q became %q/%q via %s", what, ns, n, parts.Namespace, parts.Name, s)
		}
	}
	loc := func(what string, l manifests.Location) {
		if l.Line < 0 || l.Line > lines {
			t.Fatalf("invariant 5 (in-range location): %s line %d of %d", what, l.Line, lines)
		}
		if l.Path != "" {
			t.Fatalf("invariant 8: %s carries a path %q (parsers never know their path)", what, l.Path)
		}
	}
	for _, d := range a.Dependencies {
		idOK("dependency", d.Target)
		loc("dependency", d.Loc())
		cleanText(t, "dependency", d.Target.Name+d.Target.Namespace+d.Requirement+d.Version+d.SourceRef()+d.TargetDisplay()+d.Group+d.Condition()+d.Qualifiers())
		if d.Source == manifests.SourcePath {
			checkRel(t, d.SourceRef())
		}
	}
	for _, d := range a.Imports {
		idOK("import", d.Target)
	}
	if a.Parent != nil {
		idOK("parent", a.Parent.Target)
	}
	edges := 0
	for i, l := range a.Locked {
		idOK("locked", l.ID)
		loc("locked", l.Loc)
		cleanText(t, "locked", l.ID.Name+l.ID.Namespace+l.Version+l.Integrity)
		for _, e := range l.DependsOn {
			edges++
			if e < 0 || e >= len(a.Locked) || e == i {
				t.Fatalf("invariant 5 (edge index): locked[%d] depends on %d of %d", i, e, len(a.Locked))
			}
		}
	}
	for _, x := range a.Provides {
		idOK("provides", x.ID)
		cleanText(t, "provides", x.ID.Name+x.ID.Namespace+x.Version+x.Display)
	}
	for _, d := range a.Diagnostics {
		if !declared[d.Code] {
			t.Fatalf("invariant 7 (diagnostics are declared): %s emitted undeclared code %q", p.ID(), d.Code)
		}
		loc("diagnostic", d.Loc)
		cleanText(t, "diagnostic", d.Message)
	}
	for k, v := range a.Properties {
		cleanText(t, "property", k+v)
	}
	for _, s := range append(append([]string{}, a.Members...), a.Includes...) {
		cleanText(t, "member", s)
		checkRel(t, s)
	}
	for _, s := range a.Licenses {
		cleanText(t, "licence", s)
	}
	if n := len(a.Dependencies) + len(a.Locked) + len(a.Properties) + len(a.Members) + len(a.Imports); n > len(data)+64 || edges > len(data)+64 {
		t.Fatalf("invariant 2 (bounded output): %d entries and %d edges from %d bytes", n, edges, len(data))
	}
	return a
}

func isTypedLimit(err error) bool {
	var le *limits.LimitError
	var ee *limits.EncodingError
	return errors.As(err, &le) || errors.As(err, &ee)
}

func checkRel(t testing.TB, p string) {
	if p == "" {
		return
	}
	if path.IsAbs(p) || strings.Contains(p, "\\") || strings.ContainsRune(p, 0) {
		t.Fatalf("invariant 8: path %q is absolute or malformed (parsers emit paths relative to the file)", p)
	}
}

// hiddenRune reports NUL, bidi controls and zero-width characters (the set CleanText removes).
func hiddenRune(r rune) bool {
	switch {
	case r == 0, r == 0x061c, r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r >= 0x200b && r <= 0x200d, r == 0x2060, r == 0xfeff:
		return true
	}
	return r >= 0xe0000 && r <= 0xe007f
}

func cleanText(t testing.TB, what, s string) {
	t.Helper()
	if !utf8.ValidString(s) || strings.IndexFunc(s, hiddenRune) >= 0 {
		t.Fatalf("invariant 6 (clean text): %s has %q", what, s)
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			t.Fatalf("invariant 6 (clean text): %s has control character in %q", what, s)
		}
	}
}

// AssertSamePaths is a helper for path-independence tests of assemblers: it re-runs a tree under a different root prefix.
func AssertSamePaths(t testing.TB, reg *manifests.Registry, files map[string][]byte, prefix string) {
	t.Helper()
	a := manifests.RunTree(context.Background(), reg, TreeConfig(), files)
	moved := map[string][]byte{}
	for p, b := range files {
		moved[prefix+"/"+p] = b
	}
	b := manifests.RunTree(context.Background(), reg, TreeConfig(), moved)
	if len(a.Units) != len(b.Units) {
		t.Fatalf("moving the tree under %s changed the unit count %d -> %d", prefix, len(a.Units), len(b.Units))
	}
	for i := range a.Units {
		if len(a.Units[i].Dependencies) != len(b.Units[i].Dependencies) || len(a.Units[i].Locked) != len(b.Units[i].Locked) {
			t.Fatalf("moving the tree under %s changed unit %s", prefix, a.Units[i].ID)
		}
	}
}
