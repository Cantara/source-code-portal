// Package python parses pyproject.toml (PEP 621, PEP 735, Poetry, uv, PDM), requirements files, poetry.lock, uv.lock,
// pylock.toml, Pipfile(.lock) and setup.cfg, and assembles Python projects. All parsers are pure.
package python

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
	"github.com/cantara/source-code-portal/internal/manifests/internal/purl"
	"github.com/cantara/source-code-portal/internal/manifests/internal/redact"
	"github.com/cantara/source-code-portal/internal/manifests/internal/repopath"
)

const (
	PyprojectID    = "pypi/pyproject"
	RequirementsID = "pypi/requirements"
	ConstraintsID  = "pypi/constraints"
	PoetryLockID   = "pypi/poetry-lock"
	UVLockID       = "pypi/uv-lock"
	PylockID       = "pypi/pylock"
	PipfileID      = "pypi/pipfile"
	PipfileLockID  = "pypi/pipfile-lock"
	SetupCfgID     = "pypi/setup-cfg"
	parserVer      = 1
)

func idOf(name string) manifests.ComponentID {
	return manifests.ComponentID{Ecosystem: manifests.PyPI, Name: NormalizeName(strings.TrimSpace(manifests.CleanText(name, 128)))}
}

// Spec is the EcosystemSpec of PyPI identities (PEP 503 normalisation; the registry's built-in rule is the same function).
func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: manifests.PyPI, DisplayName: "Python", PURLType: "pypi", Normalize: purl.NormalizeFunc("pypi"),
		VersionSystem: "pypi", Language: "Python", Diagnostics: catalogue()}
}

type base struct {
	id    string
	match func(string) bool
	kind  manifests.FileKind
}

func (b base) ID() string                       { return b.id }
func (b base) Version() int                     { return parserVer }
func (b base) Ecosystem() manifests.EcosystemID { return manifests.PyPI }
func (b base) Match(p string) bool              { return b.match(p) }

// admit applies the size and encoding policy; a non-nil result is the finished Parsed carrying the diagnostic.
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

// lockBase adds the Kinded capability: lockfile parsers get the lockfile size limit and the runner stamps Kind lockfile.
type lockBase struct{ base }

func (b lockBase) Kind() manifests.FileKind { return manifests.KindLockfile }

func baseName(p string) string { return path.Base(p) }

// vendoredPath reports a path below a virtualenv, cache or vendored directory (their metadata files are not projects).
func vendoredPath(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		switch seg {
		case "node_modules", "site-packages", ".venv", "venv", ".tox", ".nox", "__pypackages__", "vendor":
			return true
		}
	}
	return false
}

// pathOfURL turns a "file:" URL or a bare relative path of a direct reference into a path relative to the declaring file ("" when absolute).
func pathOfURL(u string) string {
	u = strings.TrimPrefix(u, "file:")
	if strings.HasPrefix(u, "//") && !strings.HasPrefix(u, "///") { // file://host/x or file://relative: not a local absolute path
		u = strings.TrimPrefix(u, "//")
	}
	if i := strings.IndexAny(u, "#?"); i >= 0 {
		u = u[:i]
	}
	rel, _ := repopath.Rel(u)
	return rel
}

var reqFile = regexp.MustCompile(`^(requirements|constraints)([-_.][A-Za-z0-9._-]*)?\.(txt|in)$`)

func matchRequirements(p string) bool {
	if vendoredPath(p) || strings.HasPrefix(baseName(p), "constraints") {
		return false
	}
	b := baseName(p)
	if reqFile.MatchString(b) {
		return true
	}
	d := path.Dir(p)
	return (path.Base(d) == "requirements" || path.Base(d) == "requirements.d") && strings.HasSuffix(b, ".txt")
}

var constraintsFile = regexp.MustCompile(`^constraints([-_.][A-Za-z0-9._-]*)?\.(txt|in)$`)

func matchConstraints(p string) bool {
	return !vendoredPath(p) && constraintsFile.MatchString(baseName(p))
}

func matchPylock(p string) bool {
	if vendoredPath(p) {
		return false
	}
	b := baseName(p)
	return b == "pylock.toml" || (strings.HasPrefix(b, "pylock.") && strings.HasSuffix(b, ".toml"))
}

// redactURL removes credentials from a URL-like string (shared helper: manifests/internal/redact).
func redactURL(s string) string { return redact.URL(s) }

func scopeForGroup(group string) manifests.Scope {
	switch NormalizeName(group) {
	case "test", "tests", "testing", "pytest", "unittest", "coverage":
		return manifests.ScopeTest
	}
	return manifests.ScopeDev
}

// scopeForFile guesses the scope of a requirements file from its base name; heuristic=true when a name pattern decided.
func scopeForFile(p string) (manifests.Scope, bool) {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(baseName(p), ".txt"), ".in"))
	n = strings.TrimPrefix(strings.TrimPrefix(n, "requirements"), "constraints")
	n = strings.Trim(n, "-_.")
	if n == "" || n == "base" || n == "main" || n == "prod" || n == "production" || n == "common" || n == "runtime" || n == "all" {
		if n == "" {
			dir := path.Base(path.Dir(p))
			if dir == "requirements" || dir == "requirements.d" {
				n = strings.ToLower(strings.TrimSuffix(baseName(p), ".txt"))
			} else {
				return manifests.ScopeRuntime, false
			}
		}
		if n == "base" || n == "main" || n == "prod" || n == "production" || n == "common" || n == "runtime" || n == "all" {
			return manifests.ScopeRuntime, false
		}
	}
	for _, k := range []string{"test", "tests", "testing", "tox", "pytest", "ci"} {
		if n == k || strings.HasPrefix(n, k+"-") || strings.HasSuffix(n, "-"+k) {
			return manifests.ScopeTest, true
		}
	}
	for _, k := range []string{"dev", "development", "lint", "docs", "doc", "typing", "types", "mypy", "format", "style", "qa", "local"} {
		if n == k || strings.HasPrefix(n, k+"-") || strings.HasSuffix(n, "-"+k) {
			return manifests.ScopeDev, true
		}
	}
	return manifests.ScopeRuntime, true
}

func cleanName(s string) string { return manifests.CleanText(s, 128) }

// depFrom builds a Dependency from a parsed requirement.
func depFrom(r Requirement, scope manifests.Scope, group string, line int) manifests.Dependency {
	d := manifests.Dependency{
		Target: idOf(r.Name), Requirement: manifests.CleanText(r.Specifier, 200),
		Scope: scope, Group: manifests.CleanText(group, 100), Line: int32(line),
	}
	d.SetTargetDisplay(cleanName(r.Name))
	d.SetCondition(manifests.CleanText(r.Marker, 300))
	if r.URL != "" {
		kind := sourceKindOfURL(r.URL)
		ref := redactURL(r.URL)
		if kind == manifests.SourcePath {
			ref = pathOfURL(r.URL) // relative to the requirements file or pyproject.toml; the runner rebases it
		}
		d.SetSource(kind, ref)
		d.Requirement = ""
	}
	if v, ok := ExactPin(r.Specifier); ok {
		d.Version, d.Resolution = manifests.CleanText(v, 128), manifests.ResPinned
	}
	return d
}

func sourceKindOfURL(u string) manifests.SourceKind {
	l := strings.ToLower(u)
	switch {
	case strings.HasPrefix(l, "git+") || strings.HasPrefix(l, "git://") || strings.HasPrefix(l, "ssh://git@") || strings.Contains(l, ".git"):
		return manifests.SourceGit
	case strings.HasPrefix(l, "file:") || strings.HasPrefix(l, "./") || strings.HasPrefix(l, "../") || strings.HasPrefix(l, "/"):
		return manifests.SourcePath
	}
	return manifests.SourceURL
}

func catalogue() []manifests.DiagnosticDoc {
	w, i, e := manifests.SevWarning, manifests.SevInfo, manifests.SevError
	return []manifests.DiagnosticDoc{
		{Code: "python.syntax", Severity: e, Summary: "the file cannot be parsed (TOML, JSON or INI syntax)"},
		{Code: "python.requirement-invalid", Severity: w, Summary: "a PEP 508 requirement string cannot be parsed and was skipped"},
		{Code: "python.no-name", Severity: i, Summary: "the project has no static name (dynamic or missing)"},
		{Code: "python.dynamic-dependencies", Severity: i, Summary: "dependencies are declared dynamic; they are not statically known"},
		{Code: "python.group-cycle", Severity: w, Summary: "dependency-groups include-group forms a cycle"},
		{Code: "python.group-missing", Severity: w, Summary: "include-group names a group that does not exist"},
		{Code: "python.type-mismatch", Severity: w, Summary: "a value has an unexpected type and was skipped"},
		{Code: "python.include-not-candidate", Severity: w, Summary: "a requirements include (-r/-c) names a file that is not among the parsed candidate files"},
		{Code: "python.include-cycle", Severity: w, Summary: "requirements includes form a cycle or exceed depth 8"},
		{Code: "python.include-outside-repo", Severity: w, Summary: "a requirements include is absolute, a URL or escapes the repository"},
		{Code: "python.option-ignored", Severity: i, Summary: "a pip option in a requirements file was ignored"},
		{Code: "python.unnamed-requirement", Severity: i, Summary: "a path or URL requirement without a package name was skipped"},
		{Code: "python.env-var", Severity: i, Summary: "a ${VAR} reference in a requirements file was not expanded"},
		{Code: "python.lock-stale", Severity: w, Summary: "the lock file disagrees with the declared requirements"},
		{Code: "python.lock-missing-dep", Severity: w, Summary: "a declared dependency is absent from the lock"},
		{Code: "python.uv-source-multiple", Severity: i, Summary: "tool.uv.sources has several entries for one package; the first is used"},
		{Code: "python.workspace-member-missing", Severity: w, Summary: "a tool.uv.workspace member has no pyproject.toml"},
		{Code: "python.workspace-source-unresolved", Severity: w, Summary: "a tool.uv.sources workspace entry matches no workspace member"},
	}
}

// Register wires the ecosystem into the registry (called from cmd/scp).
func Register(r *manifests.Registry) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	for _, p := range []manifests.Parser{NewPyproject(), NewRequirements(), NewConstraints(), NewPoetryLock(), NewUVLock(), NewPylock(), NewPipfile(), NewPipfileLock(), NewSetupCfg()} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return r.RegisterAssembler(NewAssembler())
}

func sortStrings(s []string) { sort.Strings(s) }
