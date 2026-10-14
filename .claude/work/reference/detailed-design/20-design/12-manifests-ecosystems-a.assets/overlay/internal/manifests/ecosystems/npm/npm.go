// Package npm parses package.json, package-lock.json / npm-shrinkwrap.json, pnpm-lock.yaml (one or two documents),
// pnpm-workspace.yaml, yarn.lock (v1 and Berry) and bun.lock, and assembles npm workspaces. All parsers are pure.
package npm

import (
	"errors"
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
	PackageJSONID   = "npm/package-json"
	PackageLockID   = "npm/package-lock"
	PnpmLockID      = "npm/pnpm-lock"
	PnpmWorkspaceID = "npm/pnpm-workspace"
	YarnLockID      = "npm/yarn-lock"
	BunLockID       = "npm/bun-lock"
	BunBinaryID     = "npm/bun-lockb"
	parserVer       = 1
)

// splitName splits "@scope/name" into ("scope", "name"); unscoped names have an empty namespace.
func splitName(full string) (string, string) {
	if strings.HasPrefix(full, "@") {
		if i := strings.IndexByte(full, '/'); i > 1 && i < len(full)-1 {
			return full[1:i], full[i+1:]
		}
	}
	return "", full
}

func idOf(full string) manifests.ComponentID {
	ns, n := splitName(strings.TrimSpace(manifests.CleanText(full, 214)))
	return manifests.ComponentID{Ecosystem: manifests.NPM, Namespace: ns, Name: n}
}

// memberOf validates a workspace pattern read from a file: relative to the file's directory, a leading "!" negates, never absolute.
func memberOf(g string) (string, bool) {
	g = manifests.CleanText(strings.TrimSpace(g), 256)
	if _, ok := repopath.Rel(strings.TrimPrefix(g, "!")); !ok {
		return "", false
	}
	return g, true
}

func displayOf(id manifests.ComponentID) string {
	if id.Namespace == "" {
		return id.Name
	}
	return "@" + id.Namespace + "/" + id.Name
}

// Spec is the EcosystemSpec of npm identities: names are case-sensitive as written (older mixed-case names exist), the scope is the
// namespace without '@' (the registry's built-in rule, restated so that the diagnostic catalogue travels with it).
func Spec() manifests.EcosystemSpec {
	return manifests.EcosystemSpec{ID: manifests.NPM, DisplayName: "npm", PURLType: "npm", Normalize: purl.NormalizeFunc("npm"),
		VersionSystem: "npm", Language: "JavaScript", Diagnostics: catalogue()}
}

type base struct {
	id    string
	match func(string) bool
	kind  manifests.FileKind
}

func (b base) ID() string                       { return b.id }
func (b base) Version() int                     { return parserVer }
func (b base) Ecosystem() manifests.EcosystemID { return manifests.NPM }
func (b base) Match(p string) bool              { return b.match(p) }

// Kind implements manifests.Kinded: the kind of every npm parser is static, and lockfile parsers get the lockfile size limit.
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

// fail turns a limit error into the matching diagnostic, or a syntax diagnostic.
func fail(out *manifests.Parsed, err error, syntax string) {
	code := syntax
	var le *limits.LimitError
	if errors.As(err, &le) && le.Limit != limits.LimitTimeout {
		code = "limit." + le.Limit
	}
	out.Diagnostics = append(out.Diagnostics, diag(code, 0, "%s", errText(err)))
}

func errText(err error) string {
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

func isFile(names ...string) func(string) bool {
	return func(p string) bool {
		b := path.Base(p)
		for _, n := range names {
			if b == n {
				return true
			}
		}
		return false
	}
}

// ignoredPath reports vendored trees that must never be treated as projects.
func ignoredPath(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "node_modules" {
			return true
		}
	}
	return false
}

var (
	exactVer  = regexp.MustCompile(`^=?v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$`)
	ghShort   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+(#.*)?$`)
	scpGitURL = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/].*$`)
)

// spec is the classification of one dependency specifier string.
type spec struct {
	Target    manifests.ComponentID // possibly the real package behind an npm: alias
	Display   string
	Req       string
	Version   string
	Res       manifests.Resolution
	Source    manifests.SourceKind
	SourceRef string
}

// classify interprets a specifier as npm, pnpm, yarn and bun do. name is the key under which it was declared.
func classify(name, s string) spec {
	s = strings.TrimSpace(s)
	sp := spec{Target: idOf(name), Display: manifests.CleanText(name, 214), Req: manifests.CleanText(s, 300)}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "npm:"):
		rest := s[4:]
		real, rng := rest, ""
		if i := strings.LastIndexByte(rest, '@'); i > 0 {
			real, rng = rest[:i], rest[i+1:]
		}
		if real != "" {
			sp.Target, sp.Display = idOf(real), manifests.CleanText(real, 214)
		}
		if m := exactVer.FindStringSubmatch(rng); m != nil {
			sp.Version, sp.Res = m[1], manifests.ResPinned
		}
	case strings.HasPrefix(low, "workspace:"):
		sp.Source, sp.SourceRef = manifests.SourceProject, "workspace" // resolved by the assembler to the member directory
	case strings.HasPrefix(low, "file:"), strings.HasPrefix(low, "link:"), strings.HasPrefix(low, "portal:"),
		strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"), strings.HasPrefix(s, "/"), strings.HasPrefix(s, "~/"):
		sp.Source, sp.SourceRef = manifests.SourcePath, localPath(s) // relative to the declaring file; "" when absolute
		sp.Req = ""
	case strings.HasPrefix(low, "git+"), strings.HasPrefix(low, "git://"), strings.HasPrefix(low, "github:"), strings.HasPrefix(low, "gitlab:"),
		strings.HasPrefix(low, "bitbucket:"), strings.HasPrefix(low, "gist:"), strings.HasPrefix(low, "ssh://git@"), scpGitURL.MatchString(s):
		sp.Source, sp.SourceRef = manifests.SourceGit, redactURL(s)
		sp.Req = ""
	case strings.HasPrefix(low, "http://"), strings.HasPrefix(low, "https://"):
		sp.Source, sp.SourceRef = manifests.SourceURL, redactURL(s)
		sp.Req = ""
	case ghShort.MatchString(s) && !strings.HasPrefix(s, "@") && !strings.ContainsAny(s, " |<>=^~*"):
		sp.Source, sp.SourceRef = manifests.SourceGit, "github:"+manifests.CleanText(s, 250)
		sp.Req = ""
	default:
		if m := exactVer.FindStringSubmatch(s); m != nil {
			sp.Version, sp.Res = m[1], manifests.ResPinned
		}
	}
	return sp
}

// redactURL removes credentials from a URL-like string (shared helper: manifests/internal/redact).
func redactURL(s string) string { return redact.URL(s) }

func depOf(name, specStr string, sc manifests.Scope, optional bool, group string, line int) manifests.Dependency {
	sp := classify(name, specStr)
	d := manifests.Dependency{
		Target: sp.Target, Requirement: sp.Req, Version: sp.Version, Resolution: sp.Res,
		Scope: sc, Optional: optional, Group: group, Line: int32(line),
	}
	d.SetTargetDisplay(sp.Display)
	d.SetSource(sp.Source, sp.SourceRef)
	return d
}

// localPath turns "file:../x", "link:./y", "portal:z" or a bare relative path into a path relative to the declaring file.
// Absolute paths and "~/..." are not repository paths and yield "".
func localPath(s string) string {
	for _, pfx := range []string{"file:", "link:", "portal:"} {
		if len(s) >= len(pfx) && strings.EqualFold(s[:len(pfx)], pfx) {
			s = s[len(pfx):]
			break
		}
	}
	s = strings.TrimPrefix(s, "//")
	if strings.HasPrefix(s, "~") {
		return ""
	}
	rel, _ := repopath.Rel(s)
	return rel
}

// scopeRank orders scopes from most to least production-relevant; lower wins when flags are merged.
func scopeRank(s manifests.Scope) int {
	switch s {
	case manifests.ScopeRuntime:
		return 0
	case manifests.ScopePeer:
		return 1
	case manifests.ScopeOptional:
		return 2
	case manifests.ScopeDev:
		return 3
	}
	return 4
}

func betterScope(a, b manifests.Scope) manifests.Scope {
	if scopeRank(b) < scopeRank(a) {
		return b
	}
	return a
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func catalogue() []manifests.DiagnosticDoc {
	w, i, e := manifests.SevWarning, manifests.SevInfo, manifests.SevError
	return []manifests.DiagnosticDoc{
		{Code: "npm.syntax", Severity: e, Summary: "the file cannot be parsed (JSON, JSONC or YAML syntax)"},
		{Code: "npm.no-name", Severity: i, Summary: "package.json has no name (private root or tooling file)"},
		{Code: "npm.type-mismatch", Severity: w, Summary: "a value has an unexpected type and was skipped"},
		{Code: "npm.lockfile-version-unsupported", Severity: w, Summary: "the lockfile version is not one this parser knows; best-effort result"},
		{Code: "npm.member-invalid", Severity: w, Summary: "a workspace pattern is absolute or malformed and was skipped"},
		{Code: "npm.lock-entry-invalid", Severity: w, Summary: "a lockfile entry is malformed and was skipped"},
		{Code: "npm.lock-missing-dep", Severity: w, Summary: "a declared dependency has no entry in the lockfile"},
		{Code: "npm.bun-binary-lockfile", Severity: w, Summary: "bun.lockb is a binary lockfile and is not parsed"},
		{Code: "npm.multiple-lockfiles", Severity: i, Summary: "several lockfiles in one directory; the package manager in packageManager (or the newest format) wins"},
		{Code: "npm.workspace-glob-unmatched", Severity: i, Summary: "a workspace pattern matched no package.json"},
		{Code: "npm.workspace-member-unfetched", Severity: w, Summary: "a workspace member directory has no fetched package.json"},
		{Code: "npm.workspace-pattern-limit", Severity: w, Summary: "more than 128 workspace patterns; the excess is ignored"},
		{Code: "npm.catalog-missing", Severity: w, Summary: "a catalog: reference has no entry in the catalogs"},
		{Code: "npm.catalog-unavailable", Severity: i, Summary: "a catalog: reference cannot be resolved because the catalog file is not fetched"},
		{Code: "npm.workspace-ref-unresolved", Severity: w, Summary: "a workspace: reference names no workspace member"},
		{Code: "npm.yaml-multi-document", Severity: i, Summary: "the pnpm lockfile has an environment document; the last document is the project"},
		{Code: "npm.yarn-version-unsupported", Severity: w, Summary: "the yarn.lock format is neither v1 nor Berry"},
	}
}

// Register wires the ecosystem into the registry (called from cmd/scp).
func Register(r *manifests.Registry) error {
	if err := r.RegisterEcosystem(Spec()); err != nil {
		return err
	}
	for _, p := range []manifests.Parser{NewPackageJSON(), NewPackageLock(), NewPnpmLock(), NewPnpmWorkspace(), NewYarnLock(), NewBunLock(), NewBunBinary()} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return r.RegisterAssembler(NewAssembler())
}
