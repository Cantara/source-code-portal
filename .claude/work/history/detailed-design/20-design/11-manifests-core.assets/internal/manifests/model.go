// Package manifests is the N-01 feature: detect, parse and assemble package-manager data into one normalised model,
// acquire it from GitHub through consumer interfaces, publish it into the read model, compute the org graph, and serve
// it through the JSON API, LLM Markdown and CycloneDX.
package manifests

import (
	"fmt"
	"strings"
	"time"
)

// EcosystemID is a stable lower-case identifier; it equals the purl type where the purl specification defines one.
type EcosystemID string

const (
	Maven         EcosystemID = "maven"
	NPM           EcosystemID = "npm"
	PyPI          EcosystemID = "pypi"
	Golang        EcosystemID = "golang"
	Cargo         EcosystemID = "cargo"
	NuGet         EcosystemID = "nuget"
	Gem           EcosystemID = "gem"
	Composer      EcosystemID = "composer"
	Swift         EcosystemID = "swift"
	Pub           EcosystemID = "pub"
	Hex           EcosystemID = "hex"
	CocoaPods     EcosystemID = "cocoapods"
	Docker        EcosystemID = "docker"
	GitHubActions EcosystemID = "github" // purl type "github": pkg:github/actions/checkout@v4
	Terraform     EcosystemID = "terraform"
	Helm          EcosystemID = "helm"
)

// ComponentID is the comparable identity of a package WITHOUT a version. Fields are already normalised by the
// ecosystem rules (internal/purl), so equality is struct equality and the type is a map key. Maven classifier/type live in
// Dependency.Qualifiers, never here.
type ComponentID struct {
	Ecosystem EcosystemID
	Namespace string // Maven groupId, npm scope without '@', Go module path prefix, Swift host/owner, Composer vendor
	Name      string // Maven artifactId, npm bare name, PyPI PEP 503 name, NuGet lower-cased id, crate name
}

func (c ComponentID) String() string {
	if c.Namespace == "" {
		return string(c.Ecosystem) + ":" + c.Name
	}
	return string(c.Ecosystem) + ":" + c.Namespace + "/" + c.Name
}

// Scope is the closed vocabulary every ecosystem maps its groups onto.
type Scope uint8

const (
	ScopeUnknown Scope = iota
	ScopeRuntime
	ScopeDev
	ScopeTest
	ScopeBuild
	ScopeOptional
	ScopePeer
	ScopeProvided
)

var scopeNames = [...]string{"unknown", "runtime", "dev", "test", "build", "optional", "peer", "provided"}

func (s Scope) String() string {
	if int(s) < len(scopeNames) {
		return scopeNames[s]
	}
	return "unknown"
}
func (s Scope) MarshalText() ([]byte, error) { return []byte(s.String()), nil }
func (s *Scope) UnmarshalText(b []byte) error {
	*s = ScopeUnknown
	for i, n := range scopeNames {
		if n == string(b) {
			*s = Scope(i)
		}
	}
	return nil // unknown strings decode to ScopeUnknown: persisted payloads stay readable across versions
}

// Relationship says how a locked component entered the graph.
type Relationship uint8

const (
	RelUnknown Relationship = iota
	RelDirect
	RelTransitive
)

// Resolution says how trustworthy Dependency.Version is.
type Resolution uint8

const (
	ResDeclared Resolution = iota // only a requirement was found
	ResPinned                     // the manifest pins an exact version
	ResLocked                     // exact version from a lockfile
	ResManaged                    // filled from BOM, catalog, CPM or workspace table
)

// Confidence flags heuristic extraction (Gradle scripts, Gemfile, Package.swift, Podfile, mix.exs).
type Confidence uint8

const (
	ConfExact Confidence = iota
	ConfHeuristic
)

// SourceKind says where a dependency comes from.
type SourceKind uint8

const (
	SourceRegistry SourceKind = iota
	SourceGit                 // SourceRef is a repository URL
	SourcePath                // SourceRef is a directory: file-relative as emitted by a parser, repo-relative after assembly
	SourceURL                 // archive or other URL
	SourceProject             // build-tool project reference (Gradle ":a:b"); an assembler resolves it to SourcePath
)

// Location points back into a source file. Path "" means "the file the value was parsed from" (parsers cannot know
// the path: results are cached by blob SHA); the runner fills it. Line is 1-based, 0 = unknown.
type Location struct {
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Dependency is one declared edge from a unit to a component. The layout is deliberately compact (120 bytes): the hot fields are
// inline and the rarely used ones live behind Extra, which keeps 200,000 dependencies at about 30 MB (measured, TestResidentHeap).
// Build values with the helper methods on *Dependency; never share an Extra between two dependencies.
type Dependency struct {
	Target      ComponentID
	Requirement string // raw, in the ecosystem's syntax ("^1.2", "[1.0,2.0)", "~> 3.1"); never evaluated
	Version     string // exact version when known (see Resolution)
	Group       string // original group/configuration name ("devDependencies", "testImplementation", extra name)
	Line        int32  // 1-based line in the file the dependency was read from; 0 = unknown
	Resolution  Resolution
	Scope       Scope
	Source      SourceKind
	Confidence  Confidence
	Optional    bool
	Extra       *DependencyExtra
}

// DependencyExtra holds the rarely used fields of a Dependency. Strings are validated like every other string (control/bidi stripped).
type DependencyExtra struct {
	TargetDisplay string // original-case name when Target was case-folded (Go module path, NuGet id)
	Condition     string // raw environment marker or target-framework condition; not evaluated
	Qualifiers    string // canonical purl qualifiers that matter for artifact identity ("classifier=sources&type=test-jar")
	SourceRef     string // repository URL, directory (file-relative from a parser, repo-relative after assembly) or project path
	LocPath       string // file the value came from when it is not the unit's own file (managed version from a BOM, catalog, CPM)
}

func (d *Dependency) extra() *DependencyExtra {
	if d.Extra == nil {
		d.Extra = &DependencyExtra{}
	}
	return d.Extra
}

func (d Dependency) TargetDisplay() string {
	if d.Extra == nil {
		return ""
	}
	return d.Extra.TargetDisplay
}
func (d Dependency) Condition() string {
	if d.Extra == nil {
		return ""
	}
	return d.Extra.Condition
}
func (d Dependency) Qualifiers() string {
	if d.Extra == nil {
		return ""
	}
	return d.Extra.Qualifiers
}
func (d Dependency) SourceRef() string {
	if d.Extra == nil {
		return ""
	}
	return d.Extra.SourceRef
}
func (d Dependency) Loc() Location {
	if d.Extra == nil {
		return Location{Line: int(d.Line)}
	}
	return Location{Path: d.Extra.LocPath, Line: int(d.Line)}
}
func (d *Dependency) SetTargetDisplay(s string) {
	if s != "" {
		d.extra().TargetDisplay = s
	}
}
func (d *Dependency) SetCondition(s string) {
	if s != "" {
		d.extra().Condition = s
	}
}
func (d *Dependency) SetQualifiers(s string) {
	if s != "" {
		d.extra().Qualifiers = s
	}
}
func (d *Dependency) SetSource(k SourceKind, ref string) {
	d.Source = k
	if ref != "" {
		d.extra().SourceRef = ref
	}
}
func (d *Dependency) SetLocPath(s string) {
	if s != "" {
		d.extra().LocPath = s
	}
}

// Artifact is something a unit publishes; the org graph matches Dependency.Target against it.
type Artifact struct {
	ID      ComponentID
	Display string // original-case name when ID was case-folded
	Version string // may be empty or a placeholder ("${revision}")
	Kind    string // "library", "application", "plugin", "bom", "image"
}

// LockedComponent is one resolved package from a lockfile (kept per unit in the cache, not in the snapshot).
type LockedComponent struct {
	ID           ComponentID
	Version      string
	Relationship Relationship
	Scope        Scope
	Integrity    string   // algorithm-prefixed digest when the lockfile has one
	Licenses     []string // SPDX expressions when the lockfile carries them
	DependsOn    []int    // indices into the same slice; nil when the format has no edges
	Loc          Location
	Source       string // "" for entries from our own parse, otherwise the provenance of a provider (unit 13, RR-7)
}

type Severity uint8

const (
	SevInfo Severity = iota
	SevWarning
	SevError
)

func (s Severity) String() string { return [...]string{"info", "warning", "error"}[min(int(s), 2)] }

// Diagnostic is a non-fatal observation. Parsers never panic and never drop input silently: a skipped construct is a Diagnostic.
type Diagnostic struct {
	Code     string // stable machine code "<namespace>.<name>", see DiagnosticCode docs
	Severity Severity
	Message  string // fixed text built by the parser from values it validated; capped at 512 bytes, no raw file excerpts beyond names
	Loc      Location
}

// FileKind classifies what a parsed file contributes.
type FileKind uint8

const (
	KindManifest FileKind = iota
	KindLockfile
	KindCatalog   // version catalog, CPM props, BOM-only POM
	KindWorkspace // workspace / module aggregator
)

// FileRef identifies the exact bytes that were parsed and by whom.
type FileRef struct {
	Path          string // slash-separated, repo-relative
	BlobSHA       string
	ParserID      string
	ParserVersion int
	Kind          FileKind // what the file contributed (manifest, lockfile, catalog, workspace); stamped by the runner from the parser
}

// Parsed is the pure result of parsing one file. It is the value cached under (BlobSHA, ParserID, ParserVersion).
type Parsed struct {
	File         FileRef // stamped by the runner, zero in the parser's own result
	Kind         FileKind
	Provides     []Artifact
	Dependencies []Dependency
	Locked       []LockedComponent
	Properties   map[string]string // unresolved variables for the assembler; keys sorted on encode
	Parent       *Dependency       // Maven <parent>
	Imports      []Dependency      // Maven scope=import BOMs, Gradle platform()
	Members      []string          // modules/workspaces/use directives: globs or paths relative to the file's directory
	Includes     []string          // files this file includes (requirements -r/-c, MSBuild Import), relative to the file's directory; resolved among candidates only
	Toolchain    map[string]string // "java": "21", "go": "1.24", "node": ">=20", "python": ">=3.10", "kotlin": "2.0"
	Licenses     []string
	Diagnostics  []Diagnostic
}

// Role separates real projects from examples and test fixtures.
type Role uint8

const (
	RolePrimary Role = iota
	RoleFixture
)

func (r Role) String() string {
	if r == RoleFixture {
		return "fixture"
	}
	return "primary"
}

// Unit is one logical project: a Maven module, an npm package, a Go module, a crate, a csproj.
// Locked components are NOT carried here after publication: LockedCount says how many exist and LockedKey where they are.
type Unit struct {
	ID           string // "<dir>#<ecosystem>[#<primary file base name>]", unique inside a repository
	Ecosystem    EcosystemID
	Dir          string // repo-relative directory of the primary file, "." for the root
	Role         Role
	Files        []FileRef
	Provides     []Artifact
	Dependencies []Dependency
	Locked       []LockedComponent // populated by the assembler, stripped by the publisher
	LockedCount  int
	Members      []string // repo-relative directories after assembly
	Toolchain    map[string]string
	Licenses     []string
	Diagnostics  []Diagnostic
}

// Technology is the rollup shown in the repository panel and badges.
type Technology struct {
	Languages  []string          // "Java", "Kotlin", "TypeScript", ... sorted, primary first
	BuildTools []string          // "maven", "gradle", "pnpm", ...
	Ecosystems []EcosystemID     // sorted
	Toolchain  map[string]string // merged, first unit in dir order wins per key
}

// LicenceSummary is declared-only (v1): where the manifest or lockfile carries no licence the component is "undeclared".
type LicenceSummary struct {
	Total               int            // occurrences (unit x component) considered: locked components, or declared dependencies for a unit without lockfile
	Declared            int            // of those, with a declared licence
	ByLicence           map[string]int // SPDX expression -> occurrences, at most 50 keys, remainder under "other"
	Components          int            // distinct (component, version) in this repository
	ComponentsDeclared  int            // of those, with a declared licence
	ComponentsByLicence map[string]int // SPDX expression -> distinct components, at most 50 keys, remainder under "other"
}

// RepoFacts is the per-repository output published into the snapshot slot.
type RepoFacts struct {
	TreeSHA     string // authoritative: the tree that was parsed
	CommitSHA   string // HEAD recorded by catalog when the job started; best effort
	ParsedAt    time.Time
	ParserSet   string // Registry.SetHash() at assembly time
	Units       []Unit
	Technology  Technology
	Licences    LicenceSummary
	Diagnostics []Diagnostic // repository level: limits, truncation, discovery
	Truncated   bool         // discovery was truncated or capped
	FileCount   int          // candidate files discovered
}

// EcosystemSpec carries per-ecosystem rules the graph and API need without importing a parser.
type EcosystemSpec struct {
	ID            EcosystemID
	DisplayName   string
	PURLType      string                                        // "" when no official purl type exists
	Normalize     func(namespace, name string) (string, string) // identity normalisation, idempotent
	VersionSystem string                                        // key understood by internal/version
	Language      string                                        // default language fact, "" for tooling ecosystems
	FormatPURL    PURLFormatter                                 // optional; nil = the default formatter (purl.Format with PURLType)
}

// PURL renders the version-less purl of c ("" when the name is empty).
func (c ComponentID) PURL() string { return renderPURL(c, "") }

func (c ComponentID) Valid() bool {
	return c.Ecosystem != "" && c.Name != "" && !strings.ContainsAny(c.Name+c.Namespace, "\x00\n\r")
}

func (u Unit) Primary() (FileRef, bool) {
	if len(u.Files) == 0 {
		return FileRef{}, false
	}
	return u.Files[0], true
}

func (f FileRef) ParserRef() string { return fmt.Sprintf("%s@%d", f.ParserID, f.ParserVersion) }
