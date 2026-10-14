// Package gradle parses Gradle settings, build scripts (lexical heuristics, never executed), version catalogs, dependency
// lockfiles and gradle.properties, and assembles Gradle builds. Dependencies of a Gradle unit have Target.Ecosystem
// "maven": Gradle resolves from Maven repositories.
package gradle

import (
	"errors"
	"path"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
)

// ID is the ecosystem id of Gradle builds. Dependencies of a Gradle unit target the Maven ecosystem (Gradle resolves from Maven repositories),
// which is why the spec has PURLType "maven" and the runner normalises foreign targets with the purl rules (requirement RR-A3).
const ID manifests.EcosystemID = "gradle"

const (
	SettingsID   = "gradle/settings"
	ScriptID     = "gradle/script"
	CatalogID    = "gradle/catalog"
	LockfileID   = "gradle/lockfile"
	PropertiesID = "gradle/properties"
	parserVer    = 1
)

func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: ID, DisplayName: "Gradle", PURLType: "maven", Normalize: purl.NormalizeFunc("maven"), VersionSystem: "maven", Language: "Java", Diagnostics: catalogue()}
}

// mvn builds the identity of a Maven coordinate. A groupId or artifactId with a slash, backslash, colon or whitespace cannot be a Maven
// coordinate (and cannot round-trip through a purl); it yields an identity without a name, which the runner drops with a diagnostic.
func mvn(g, a string) manifests.ComponentID {
	if !coordOK(g) || !coordOK(a) {
		return manifests.ComponentID{Ecosystem: manifests.Maven}
	}
	return manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: g, Name: a}
}

// mvnRaw builds the identity of a placeholder that is not a coordinate: a project path (":lib:core") or an unresolved notation. The assembler
// replaces or demotes these.
func mvnRaw(g, a string) manifests.ComponentID {
	return manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: g, Name: a}
}

func coordOK(s string) bool { return !strings.ContainsAny(s, "/\\: ") }

type base struct {
	id    string
	match func(string) bool
	kind  manifests.FileKind
}

func (b base) ID() string                       { return b.id }
func (b base) Version() int                     { return parserVer }
func (b base) Ecosystem() manifests.EcosystemID { return ID }
func (b base) Match(p string) bool              { return b.match(p) }

// Kind implements manifests.Kinded (static per parser).
func (b base) Kind() manifests.FileKind { return b.kind }

func (b base) admit(f manifests.File, lim manifests.Limits) ([]byte, *manifests.Parsed) {
	out := &manifests.Parsed{Kind: b.kind}
	if max := lim.WithDefaults().MaxBytes; len(f.Data) > max {
		out.Diagnostics = []manifests.Diagnostic{diag("limit.size", 0, "file is %d bytes, limit %d", len(f.Data), max)}
		return nil, out
	}
	data, err := limits.Input(f.Data)
	if err != nil {
		out.Diagnostics = []manifests.Diagnostic{diag("encoding.unsupported", 0, "file is not UTF-8 text without NUL bytes")}
		return nil, out
	}
	return data, nil
}

// fail turns a typed limit or encoding error into its catalogued diagnostic; everything else is a syntax problem of the file.
func fail(out *manifests.Parsed, err error, syntax string) {
	code := syntax
	var le *limits.LimitError
	var ee *limits.EncodingError
	switch {
	case errors.As(err, &le) && le.Limit != limits.LimitTimeout:
		code = "limit." + le.Limit
	case errors.As(err, &ee):
		code = "encoding.unsupported"
	}
	out.Diagnostics = append(out.Diagnostics, diag(code, 0, "%s", err.Error()))
}

// mkd builds a Dependency. Gradle targets are Maven coordinates; every Gradle dependency except a lockfile entry is ConfHeuristic.
func mkd(t manifests.ComponentID, req, display, qual, group, cond string, sc manifests.Scope, line int) manifests.Dependency {
	d := manifests.Dependency{Target: t, Requirement: req, Group: group, Scope: sc, Line: int32(line), Confidence: manifests.ConfHeuristic}
	d.SetTargetDisplay(display)
	d.SetQualifiers(qual)
	d.SetCondition(cond)
	return d
}

func setProp(p *manifests.Parsed, k, v string) {
	if p.Properties == nil {
		p.Properties = map[string]string{}
	}
	if len(p.Properties) < 20000 {
		p.Properties[k] = v
	}
}

// ignoredPath: build output, caches and test fixtures are never builds.
func ignoredPath(p string) bool {
	segs := strings.Split(path.Dir(p), "/")
	for i, s := range segs {
		switch s {
		case "build", ".gradle", "node_modules", "target", "out":
			return true
		case "src":
			if i+2 < len(segs) && segs[i+1] == "test" && segs[i+2] == "resources" {
				return true
			}
		}
	}
	return false
}

func isBase(names ...string) func(string) bool {
	return func(p string) bool {
		if ignoredPath(p) {
			return false
		}
		b := path.Base(p)
		for _, n := range names {
			if b == n {
				return true
			}
		}
		return false
	}
}

func matchCatalog(p string) bool {
	return !ignoredPath(p) && strings.HasSuffix(path.Base(p), ".versions.toml")
}

func matchLockfile(p string) bool {
	if ignoredPath(p) {
		return false
	}
	b := path.Base(p)
	if b == "gradle.lockfile" || b == "settings-gradle.lockfile" || b == "buildscript-gradle.lockfile" {
		return true
	}
	return strings.HasSuffix(b, ".lockfile") && path.Base(path.Dir(p)) == "dependency-locks"
}

func catalogue() []manifests.DiagnosticDoc {
	w, i, e := manifests.SevWarning, manifests.SevInfo, manifests.SevError
	return []manifests.DiagnosticDoc{
		{Code: "gradle.syntax", Severity: e, Summary: "the file cannot be parsed"},
		{Code: "gradle.script-heuristic", Severity: i, Summary: "dependencies were read from a build script by lexical heuristics and are flagged heuristic"},
		{Code: "gradle.unresolved-notation", Severity: i, Summary: "a dependency argument is not a literal, catalog accessor or project reference and was skipped"},
		{Code: "gradle.template-unresolved", Severity: i, Summary: "a string template in a dependency could not be resolved statically"},
		{Code: "gradle.catalog-missing", Severity: w, Summary: "a catalog accessor names a catalog or alias that is not in the fetched catalogs"},
		{Code: "gradle.catalog-version-missing", Severity: w, Summary: "a catalog entry references a version that is not defined"},
		{Code: "gradle.catalog-invalid", Severity: w, Summary: "a catalog entry is malformed and was skipped"},
		{Code: "gradle.project-missing", Severity: w, Summary: "an included project has no build script among the fetched files"},
		{Code: "gradle.project-dir-override", Severity: i, Summary: "settings assign projectDir; the default directory layout is assumed"},
		{Code: "gradle.include-invalid", Severity: w, Summary: "an include argument is empty, absolute or malformed and was skipped"},
		{Code: "gradle.settings-dynamic", Severity: i, Summary: "settings use loops or computed values for include; only literal includes are read"},
		{Code: "gradle.apply-from", Severity: i, Summary: "a script applies another script (apply from); its dependencies are not read"},
		{Code: "gradle.local-dependency", Severity: i, Summary: "a file or tree dependency has no coordinates"},
		{Code: "gradle.lockfile-syntax", Severity: w, Summary: "a lockfile line is malformed and was skipped"},
		{Code: "gradle.include-build", Severity: i, Summary: "an included build is treated as a separate Gradle build"},
		{Code: "gradle.variable-unresolved", Severity: i, Summary: "a dependency refers to a variable that is not a string literal in the fetched files"},
	}
}

// Register wires the ecosystem into the registry (called from cmd/scp).
func Register(r *manifests.Registry) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	for _, p := range []manifests.Parser{NewSettings(), NewScript(), NewCatalog(), NewLockfile(), NewProperties()} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return r.RegisterAssembler(NewAssembler())
}
