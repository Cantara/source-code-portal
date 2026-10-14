// Package golang parses go.mod and go.work (pure, via golang.org/x/mod/modfile) and assembles Go module units.
package golang

import (
	"context"
	"errors"
	"path"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

// Parser identifiers (stable; part of every cache key together with the version).
const (
	ModParserID  = "golang/mod"
	WorkParserID = "golang/work"
	parserVer    = 1
)

// Spec is the EcosystemSpec of Go module identities. Identity rule (purl-spec golang type): the module path is split at its last '/'
// into Namespace and Name and both are lower-cased; the original case lives in Dependency.TargetDisplay and Artifact.Display.
func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: manifests.Golang, DisplayName: "Go modules", PURLType: "golang", Normalize: purl.NormalizeFunc("golang"),
		VersionSystem: "go", Language: "Go", Diagnostics: catalogue()}
}

// componentOf splits a module path at its last '/' and applies the registry normalisation. Empty segments ("/a//b/") are dropped first:
// an identity whose namespace starts or ends with "/" cannot round-trip through a purl (found by FuzzParsers).
func componentOf(modulePath string) manifests.ComponentID {
	// every segment is trimmed and empty segments are dropped: " /0/0" must not leave a namespace that purl normalisation re-trims
	segs := strings.Split(modulePath, "/")
	kept := segs[:0]
	for _, s := range segs {
		if s = strings.TrimSpace(s); s != "" {
			kept = append(kept, s)
		}
	}
	modulePath = strings.Join(kept, "/")
	ns, name := "", modulePath
	if i := strings.LastIndexByte(modulePath, '/'); i >= 0 {
		ns, name = modulePath[:i], modulePath[i+1:]
	}
	ns, name = purl.Normalize("golang", ns, name)
	return manifests.ComponentID{Ecosystem: manifests.Golang, Namespace: ns, Name: name}
}

func noFix(_, v string) (string, error) { return v, nil }

type modParser struct{}
type workParser struct{}

// NewModParser returns the go.mod parser.
func NewModParser() manifests.Parser { return modParser{} }

// NewWorkParser returns the go.work parser.
func NewWorkParser() manifests.Parser { return workParser{} }

func (modParser) ID() string                       { return ModParserID }
func (modParser) Version() int                     { return parserVer }
func (modParser) Ecosystem() manifests.EcosystemID { return manifests.Golang }
func (modParser) Match(p string) bool              { return path.Base(p) == "go.mod" && !vendored(p) }

func (workParser) ID() string                       { return WorkParserID }
func (workParser) Version() int                     { return parserVer }
func (workParser) Ecosystem() manifests.EcosystemID { return manifests.Golang }
func (workParser) Match(p string) bool              { return path.Base(p) == "go.work" && !vendored(p) }
func (workParser) Kind() manifests.FileKind         { return manifests.KindWorkspace }

// vendored reports a path below a vendor directory (go mod vendor copies foreign go.mod files there).
func vendored(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "vendor" || seg == "node_modules" {
			return true
		}
	}
	return false
}

// prepare gates the input (size, encoding) and builds the guard. A rejected input yields a Parsed that carries only the diagnostic.
func prepare(ctx context.Context, f manifests.File, lim manifests.Limits, kind manifests.FileKind) ([]byte, *limits.Guard, *manifests.Parsed) {
	g := limits.NewGuard(ctx, lim)
	if err := g.CheckSize(len(f.Data)); err != nil {
		return nil, g, &manifests.Parsed{Kind: kind, Diagnostics: []manifests.Diagnostic{diag("limit.size", 0, "file exceeds %d bytes", lim.WithDefaults().MaxBytes)}}
	}
	data, err := limits.Input(f.Data)
	if err != nil {
		return nil, g, &manifests.Parsed{Kind: kind, Diagnostics: []manifests.Diagnostic{diag("encoding.unsupported", 0, "file is not UTF-8 text without NUL bytes")}}
	}
	return data, g, nil
}

type replacement struct {
	oldPath, oldVer string
	newPath, newVer string
}

func (r replacement) key() string {
	if r.oldVer != "" {
		return "replace:" + r.oldPath + "@" + r.oldVer
	}
	return "replace:" + r.oldPath
}
func (r replacement) value() string {
	if r.newVer != "" {
		return r.newPath + "@" + r.newVer
	}
	return r.newPath
}

func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || p == "." || p == ".."
}

func (modParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, g, rej := prepare(ctx, f, lim, manifests.KindManifest)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindManifest}
	mf, err := modfile.Parse("go.mod", data, noFix)
	if err != nil {
		// strict parsing failed (unknown directive of a newer Go, malformed statement): keep what the lax parser can read
		strictErr := err
		mf, err = modfile.ParseLax("go.mod", data, noFix)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, diag("golang.syntax", errLine(strictErr), "go.mod cannot be parsed: %v", firstLine(strictErr)))
			return out, nil
		}
		out.Diagnostics = append(out.Diagnostics, diag("golang.strict-failed", errLine(strictErr), "strict parse failed, replace/exclude/tool directives ignored: %v", firstLine(strictErr)))
	}
	if err := ctx.Err(); err != nil {
		return manifests.Parsed{}, err
	}
	out.Toolchain = map[string]string{"language": "go", "tool": "go"}
	if mf.Go != nil {
		out.Toolchain["go"] = mf.Go.Version
	} else {
		out.Toolchain["go"] = "1.16"
		out.Diagnostics = append(out.Diagnostics, diag("golang.go-directive-missing", 0, "no go directive; the go command assumes go 1.16"))
	}
	if mf.Toolchain != nil {
		out.Toolchain["toolchain"] = mf.Toolchain.Name
	}
	if mf.Module == nil || mf.Module.Mod.Path == "" {
		out.Diagnostics = append(out.Diagnostics, diag("golang.module-missing", 0, "go.mod has no module directive"))
	} else {
		mp := manifests.CleanText(mf.Module.Mod.Path, 512)
		out.Provides = []manifests.Artifact{{ID: componentOf(mp), Display: mp, Kind: "library"}}
	}

	// replacements: version-specific beat wildcard (go.mod semantics)
	var reps []replacement
	repFor := func(p, v string) (replacement, bool) {
		var wild *replacement
		for i := range mf.Replace {
			r := mf.Replace[i]
			if r.Old.Path != p {
				continue
			}
			if r.Old.Version == v && v != "" {
				return replacement{r.Old.Path, r.Old.Version, r.New.Path, r.New.Version}, true
			}
			if r.Old.Version == "" {
				x := replacement{r.Old.Path, "", r.New.Path, r.New.Version}
				wild = &x
			}
		}
		if wild != nil {
			return *wild, true
		}
		return replacement{}, false
	}
	for _, r := range mf.Replace {
		reps = append(reps, replacement{r.Old.Path, r.Old.Version, r.New.Path, r.New.Version})
	}

	mkDep := func(rq *modfile.Require, scope manifests.Scope, group string) manifests.Dependency {
		p := manifests.CleanText(rq.Mod.Path, 512)
		d := manifests.Dependency{
			Target: componentOf(p), Requirement: manifests.CleanText(rq.Mod.Version, 128), Version: manifests.CleanText(rq.Mod.Version, 128),
			Resolution: manifests.ResPinned, Scope: scope, Group: group, Line: int32(rq.Syntax.Start.Line),
		}
		d.SetTargetDisplay(p)
		if r, ok := repFor(rq.Mod.Path, rq.Mod.Version); ok {
			if isLocalPath(r.newPath) {
				// the path is relative to the directory of this go.mod; the runner rebases it (rebasePaths) and judges escapes
				if strings.HasPrefix(r.newPath, "/") {
					d.SetSource(manifests.SourcePath, "")
					out.Diagnostics = append(out.Diagnostics, diag("golang.replace-absolute", rq.Syntax.Start.Line, "replace of %s uses an absolute path", p))
				} else {
					d.SetSource(manifests.SourcePath, path.Clean(manifests.CleanText(r.newPath, 256)))
				}
			} else {
				d.SetSource(manifests.SourceRegistry, manifests.CleanText(r.value(), 512))
			}
		}
		return d
	}

	byPath := map[string]*modfile.Require{}
	for i, rq := range mf.Require {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return manifests.Parsed{}, err
			}
		}
		if err := g.Node(); err != nil {
			out.Diagnostics = append(out.Diagnostics, diag("limit.nodes", rq.Syntax.Start.Line, "too many requirements"))
			break
		}
		byPath[rq.Mod.Path] = rq
		l := manifests.LockedComponent{ID: componentOf(rq.Mod.Path), Version: manifests.CleanText(rq.Mod.Version, 128),
			Relationship: manifests.RelDirect, Scope: manifests.ScopeRuntime, Loc: manifests.Location{Line: rq.Syntax.Start.Line}}
		if rq.Indirect {
			l.Relationship = manifests.RelTransitive
		} else {
			out.Dependencies = append(out.Dependencies, mkDep(rq, manifests.ScopeRuntime, "require"))
		}
		out.Locked = append(out.Locked, l)
	}
	for _, t := range mf.Tool {
		// a tool is a package path; the owning module is the longest required module path that prefixes it
		var best *modfile.Require
		for p, rq := range byPath {
			if (t.Path == p || strings.HasPrefix(t.Path, p+"/")) && (best == nil || len(p) > len(best.Mod.Path)) {
				best = rq
			}
		}
		if best == nil {
			out.Diagnostics = append(out.Diagnostics, diag("golang.tool-unresolved", t.Syntax.Start.Line, "tool %s matches no required module", manifests.CleanText(t.Path, 200)))
			continue
		}
		d := mkDep(best, manifests.ScopeBuild, "tool")
		d.Line = int32(t.Syntax.Start.Line)
		out.Dependencies = append(out.Dependencies, d)
	}
	// replaces are also exported for go.work-level overriding by the assembler
	if len(reps) > 0 {
		out.Properties = map[string]string{}
		for _, r := range reps {
			out.Properties[r.key()] = manifests.CleanText(r.value(), 512)
		}
	}
	return out, nil
}

func (workParser) Parse(ctx context.Context, f manifests.File, lim manifests.Limits) (manifests.Parsed, error) {
	data, _, rej := prepare(ctx, f, lim, manifests.KindWorkspace)
	if rej != nil {
		return *rej, nil
	}
	out := manifests.Parsed{Kind: manifests.KindWorkspace}
	wf, err := modfile.ParseWork("go.work", data, noFix)
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, diag("golang.syntax", errLine(err), "go.work cannot be parsed: %v", firstLine(err)))
		return out, nil
	}
	out.Toolchain = map[string]string{}
	if wf.Go != nil {
		out.Toolchain["go"] = wf.Go.Version
	}
	if wf.Toolchain != nil {
		out.Toolchain["toolchain"] = wf.Toolchain.Name
	}
	for _, u := range wf.Use {
		rel, ok := repopath.Rel(u.Path)
		if !ok {
			out.Diagnostics = append(out.Diagnostics, diag("golang.use-invalid", u.Syntax.Start.Line, "use %s is absolute or malformed", manifests.CleanText(u.Path, 200)))
			continue
		}
		out.Members = append(out.Members, rel) // relative to the directory of go.work
	}
	if len(wf.Replace) > 0 {
		out.Properties = map[string]string{}
		for _, r := range wf.Replace {
			rp := replacement{r.Old.Path, r.Old.Version, r.New.Path, r.New.Version}
			v := rp.value()
			if isLocalPath(rp.newPath) {
				if rel, ok := repopath.Rel(rp.newPath); ok {
					v = "path:" + rel // relative to go.work; the assembler joins it with the directory of go.work
				}
			}
			out.Properties[rp.key()] = manifests.CleanText(v, 512)
		}
	}
	return out, nil
}

func errLine(err error) int {
	var el modfile.ErrorList
	if errors.As(err, &el) && len(el) > 0 {
		return el[0].Pos.Line
	}
	return 0
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func catalogue() []manifests.DiagnosticDoc {
	return []manifests.DiagnosticDoc{
		{Code: "golang.syntax", Severity: manifests.SevError, Summary: "go.mod or go.work could not be parsed"},
		{Code: "golang.strict-failed", Severity: manifests.SevWarning, Summary: "strict go.mod parsing failed; replace, exclude and tool directives were ignored"},
		{Code: "golang.go-directive-missing", Severity: manifests.SevInfo, Summary: "go.mod has no go directive (go 1.16 assumed)"},
		{Code: "golang.module-missing", Severity: manifests.SevWarning, Summary: "go.mod has no module directive"},
		{Code: "golang.tool-unresolved", Severity: manifests.SevInfo, Summary: "a tool directive matches no required module"},
		{Code: "golang.replace-absolute", Severity: manifests.SevWarning, Summary: "a replace uses an absolute filesystem path and is not followed"},
		{Code: "golang.use-invalid", Severity: manifests.SevWarning, Summary: "a go.work use directive is absolute or malformed"},
		{Code: "golang.use-missing", Severity: manifests.SevWarning, Summary: "go.work uses a directory without a go.mod among the candidate files"},
	}
}
