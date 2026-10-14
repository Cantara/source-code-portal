// Package maven parses pom.xml (the raw model, no inheritance), .mvn/maven.config, and assembles Maven reactors:
// parent resolution, property interpolation, dependencyManagement and BOM imports, profile activation. Pure: the
// only I/O is the optional manifests.PomSource injected into the assembler.
package maven

import (
	"errors"
	"path"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
)

const (
	PomID     = "maven/pom"
	ConfigID  = "maven/config"
	parserVer = 1
)

// Spec is the EcosystemSpec of Maven identities: coordinates are case-sensitive and nothing is folded (the registry's built-in rule).
func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: manifests.Maven, DisplayName: "Maven", PURLType: "maven", Normalize: purl.NormalizeFunc("maven"),
		VersionSystem: "maven", Language: "Java", Diagnostics: catalogue()}
}

// idOf builds an identity from raw (possibly placeholder) coordinates.
func idOf(g, a string) manifests.ComponentID {
	return manifests.ComponentID{Ecosystem: manifests.Maven, Namespace: g, Name: a}
}

type base struct {
	id    string
	match func(string) bool
	kind  manifests.FileKind
}

func (b base) ID() string                       { return b.id }
func (b base) Version() int                     { return parserVer }
func (b base) Ecosystem() manifests.EcosystemID { return manifests.Maven }
func (b base) Match(p string) bool              { return b.match(p) }

// Kind implements manifests.Kinded (static per parser).
func (b base) Kind() manifests.FileKind { return b.kind }

// ignoredPath: build output and test or archetype fixtures are never projects.
func ignoredPath(p string) bool {
	segs := strings.Split(path.Dir(p), "/")
	for i, s := range segs {
		switch s {
		case "target", "node_modules", "archetype-resources":
			return true
		case "src":
			if i+2 < len(segs)+1 && i+1 < len(segs) {
				if next := segs[i+1]; next == "it" || (next == "test" && i+2 < len(segs) && segs[i+2] == "resources") {
					return true
				}
			}
		}
	}
	return false
}

func matchPom(p string) bool {
	b := path.Base(p)
	return (b == "pom.xml" || strings.HasSuffix(b, ".pom")) && !ignoredPath(p)
}

func matchConfig(p string) bool {
	return strings.HasSuffix(p, ".mvn/maven.config") || p == ".mvn/maven.config"
}

func fail(out *manifests.Parsed, err error) {
	code := "maven.syntax"
	var le *limits.LimitError
	var ee *limits.EncodingError
	switch {
	case errors.As(err, &le) && le.Limit != limits.LimitTimeout:
		code = "limit." + le.Limit
	case errors.As(err, &ee):
		code = "encoding.unsupported"
	}
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160]
	}
	out.Diagnostics = append(out.Diagnostics, diag(code, 0, "%s", msg))
}

func catalogue() []manifests.DiagnosticDoc {
	w, i, e := manifests.SevWarning, manifests.SevInfo, manifests.SevError
	return []manifests.DiagnosticDoc{
		{Code: "maven.syntax", Severity: e, Summary: "the POM is not well-formed XML (or uses an undefined entity)"},
		{Code: "maven.not-a-project", Severity: w, Summary: "the document element is not <project>"},
		{Code: "maven.no-artifactId", Severity: w, Summary: "the POM has no artifactId"},
		{Code: "maven.dependency-incomplete", Severity: w, Summary: "a dependency, parent or project lacks groupId or artifactId, or has one with a slash, colon or whitespace, and was skipped"},
		{Code: "maven.scope-placeholder", Severity: i, Summary: "a dependency scope is a placeholder or unknown; compile is assumed unless managed"},
		{Code: "maven.system-scope", Severity: i, Summary: "a system-scoped dependency points at a local file; treated as provided"},
		{Code: "maven.parent-unresolved", Severity: i, Summary: "the parent POM is not in the repository and no PomSource supplied it; inherited properties and management are unknown"},
		{Code: "maven.parent-mismatch", Severity: w, Summary: "the POM at relativePath is not the declared parent; coordinates lookup was used"},
		{Code: "maven.parent-cycle", Severity: w, Summary: "the parent chain is cyclic or deeper than 32"},
		{Code: "maven.parent-fetched", Severity: i, Summary: "a parent or BOM was supplied by the PomSource"},
		{Code: "maven.pom-source-failed", Severity: i, Summary: "the PomSource could not supply a POM"},
		{Code: "maven.property-unresolved", Severity: i, Summary: "a ${property} cannot be resolved statically (environment, settings, basedir or defined in an unknown parent)"},
		{Code: "maven.property-cycle", Severity: w, Summary: "properties reference each other cyclically"},
		{Code: "maven.version-unmanaged", Severity: w, Summary: "a dependency has no version and no dependencyManagement entry supplies one"},
		{Code: "maven.bom-unresolved", Severity: i, Summary: "an imported BOM is not in the repository and no PomSource supplied it"},
		{Code: "maven.bom-cycle", Severity: w, Summary: "BOM imports are cyclic or nested deeper than 8"},
		{Code: "maven.module-missing", Severity: w, Summary: "a declared module has no fetched pom.xml"},
		{Code: "maven.profile-unknown", Severity: i, Summary: "a profile activation (jdk, os, file) cannot be evaluated; the profile is treated as inactive"},
		{Code: "maven.config-option-ignored", Severity: i, Summary: "an option in .mvn/maven.config is not a property or profile selection"},
		{Code: "maven.module-invalid", Severity: w, Summary: "a module path is absolute or malformed"},
		{Code: "maven.relative-path-escape", Severity: w, Summary: "the parent relativePath leaves the repository"},
		{Code: "maven.snapshot-private-skipped", Severity: i, Summary: "the PomSource was not asked for a coordinate matching a private prefix or an organisation artifact"},
	}
}

// Option configures the assembler.
type Option func(*assembler)

// WithPomSource injects the optional parent/BOM lookup. allow is the privacy guard (manifests.PrivacyGuard.Allow): it reports whether a
// coordinate may be sent to the source; organisation artifacts and private_group_prefixes are never allowed. A nil allow is a programming
// error and disables the source.
func WithPomSource(src manifests.PomSource, allow func(manifests.ComponentID) bool) Option {
	return func(a *assembler) { a.src, a.allow = src, allow }
}

// Register wires the ecosystem into the registry (called from cmd/scp).
func Register(r *manifests.Registry, opts ...Option) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	for _, p := range []manifests.Parser{NewPom(), NewConfig()} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return r.RegisterAssembler(NewAssembler(opts...))
}
