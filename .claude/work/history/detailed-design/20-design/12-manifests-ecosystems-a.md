# 12. Manifests: ecosystem parsers and assemblers A (`internal/manifests/ecosystems/{golang,npm,python,maven,gradle,cargo}`)

Status: design for implementation, revision 1.1 (independent verification applied, 2026-10-14; 22 findings, 19 fixed and 3 recorded for other owners, are in Appendix F, the findings report is `30-verification/design-12-manifests-ecosystems-a.md`). Author: unit designer 12 (`12-manifests-ecosystems-a`). Binding inputs, in precedence order: `04-decisions.md` (D-15 and section 7: A-15, A-18, A-20, A-26), `10-architecture.md` (sections 5, 13, 15, 16, 17, 21), `11-adr/` (ADR-021, ADR-022), then `02-design-guardrails.md` and the background document `05-research/07-manifest-parsing.md`. The normative core contract is `11-manifests-core.md` (revision 1.1, verified, with `11-manifests-core.assets/`); the sibling is `13-manifests-ecosystems-b.md`. Manifest parsing reads untrusted files from every repository of the organisation, so the design is driven by the guardrails as much as by the features: XML is parsed without entities and within budgets (GR-34), nothing is cached that nothing reads (GR-04), the commit that is read is resolved from the repository and never taken from a configured branch name (GR-10), and file content is never logged (GR-13). The guardrail trace (section 11) is therefore long.

How to read this document: sections 1 to 15 follow the required template. Section 4.1 is the contract that all six ecosystems obey; 4.2 to 4.7 are the six ecosystems, each with the same sub-headings (Files and match, Grammar handled, Grammar not handled, Mapping to the model, Identity and purl, Confidence, Limits and hardening, Diagnostics, Assembly, Corpus). 4.8 lists limitations that cross ecosystems. Appendix A is the verification log with dates, B the experiments with numbers, C what was compile-checked, D the file inventory of the assets and the patch list against the core, E the defects the fuzzers found in this unit's own code, F the independent verification log. The code that backs this document is in `12-manifests-ecosystems-a.assets/` (about 14,200 lines of Go including tests, 44 golden trees, 23 fuzz regression seeds); `apply.sh` rebuilds a scratch module from the core scaffold plus the overlay plus two patch files and runs `gofmt`, `go vet` and `go test -race` (green on 2026-10-14, Go 1.27.1).

Identifiers used throughout: **RR-An** = a requirement this unit places on the `manifests` root (unit 11), answered in section 2.3; **RI-n** = the root and `manifests/internal/*` surface this unit relies on (numbers of unit 13 section 2.3, answered in doc 11 section 2.8); **D-n** = deviation proposed (section 14); **OQ-n** = open question (section 15); **V-n** = verified fact (appendix A); **E-An** = experiment (appendix B); **F-An** = defect found by the fuzzers in this unit's code (appendix E); **M0..M7** = milestones (section 13); **L-n** = known limitation (section 4.8). Library and standard facts were fetched or executed on 2026-10-14; the date is repeated in appendix A.

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

Turn the package-manager files of Go, npm (npm, Yarn, pnpm, Bun), Python (pip, Poetry, uv, Pipenv, setuptools), Maven, Gradle and Cargo into the one normalised model of `internal/manifests` (`ComponentID`, `Dependency`, `Artifact`, `LockedComponent`, `Parsed`, `Unit`), so that the repository dependency panel, the organisation-wide "who depends on what" graph, the version-spread and licence views, the CycloneDX export and the JSON/LLM API (D-15) cover the six tier-0 ecosystems of research 07. Everything here is a **pure function of file bytes**: no build tool is executed, no network is touched, no clock or random source is read, no file is read outside the bytes handed in. The single exception is the Maven assembler, which may call an injected, opt-in `manifests.PomSource` for parent and BOM POMs outside the organisation, behind the privacy guard (4.5.6).

Why these six matter to this product specifically: Maven and Gradle are where an organisation's *internal* edges hide in an organisation that builds with Maven or Gradle (`<parent>`, `project(":x")`, a BOM published by another repository); npm and Go workspaces and Cargo workspaces are the monorepo layouts the unit graph must understand (`workspace:*`, `go.work`, `path = "../x"`); Python's `requirements*.txt`, `pyproject.toml` and the three lock formats are the long tail that decides whether a repository shows any dependencies at all. The channel-A and channel-C matches of the org graph (doc 11 4.8) are only as good as the `Artifact` and `SourceRef` values these parsers emit.

### 1.2 In scope (this document specifies, the engineer implements)

1. Six ecosystem packages, 27 parsers and six assemblers (all `Parser` versions start at 1, all `AssemblerVersion` at 1; the Maven one is `1 | 1<<16` while a `PomSource` is wired):

| Package | `EcosystemID` | Parser ids | Kind (`Kinded`) | Assembler |
|---|---|---|---|---|
| `ecosystems/golang` | `golang` | `golang/mod`, `golang/work` | manifest; workspace | per `go.mod`, with `go.work` membership and replace overlay |
| `ecosystems/npm` | `npm` | `npm/package-json`, `npm/package-lock`, `npm/pnpm-lock`, `npm/pnpm-workspace`, `npm/yarn-lock`, `npm/bun-lock`, `npm/bun-lockb` | manifest; lockfile x5; workspace; lockfile | per `package.json` directory; workspace roots, catalogs, lock join |
| `ecosystems/python` | `pypi` | `pypi/pyproject`, `pypi/requirements`, `pypi/constraints`, `pypi/poetry-lock`, `pypi/uv-lock`, `pypi/pylock`, `pypi/pipfile`, `pypi/pipfile-lock`, `pypi/setup-cfg` | manifest x4 (requirements and constraints decide by content: catalog, lockfile for pip-compile output); lockfile x4 | per project directory; includes, constraints, uv workspaces, lock join |
| `ecosystems/maven` | `maven` | `maven/pom`, `maven/config` | manifest; workspace | reactor: parent chain, property interpolation, `dependencyManagement`, BOM import, profiles, optional `PomSource` |
| `ecosystems/gradle` | `gradle` (new, D-5) | `gradle/settings`, `gradle/script`, `gradle/catalog`, `gradle/lockfile`, `gradle/properties` | workspace; manifest; catalog; lockfile; workspace | per build: projects, catalogs, plugins, `project(...)` edges, lock join |
| `ecosystems/cargo` | `cargo` | `cargo/manifest`, `cargo/lock` | manifest (workspace root: workspace); lockfile | per crate; workspace membership and inheritance; lock join with reachability scopes |

The parser ids obey the registry rule `<EcosystemID>/<format>`. Two ids differ from the research table of 07 section 5.3 (`python/...` became `pypi/...` because the ecosystem id is `pypi`, the purl type; D-6).

2. Five helper packages under `manifests/internal/` (`tomlx`, `linex`, `repopath` rank 1; `expand`, `redact` rank 2) that the six packages share (section 3.4): `tomlx` (bounded TOML to an own tree with source lines), `expand` (bounded placeholder expansion), `redact` (URL credential removal), `linex` (byte offset to line), `repopath` (repository-relative path and glob algebra).
3. Per-parser golden, table, fuzz, property, race and integration tests and the corpus of section 4 (tier 1 committed, tier 2 oracle).
4. Requirements on the root (RR-A1 to RR-A8, section 2.3) with a working patch (`patches/core-rr-a.diff`, 119 lines) and the test harness they enable (`manifests/tree.go`, `parsertest/tree.go`).

### 1.3 Out of scope (non-goals)

- The other eleven ecosystems, the providers and the join assembler (unit 13); the root harness, graph, API, SPA and CycloneDX export (unit 11); the GitHub adapter and the store (units 05 and 03).
- **Executing or evaluating anything**, ever: no `go`, `mvn`, `gradle`, `npm`, `pnpm`, `yarn`, `bun`, `pip`, `poetry`, `uv`, `cargo` binary; no Groovy or Kotlin script evaluation (the Gradle DSL is scanned lexically); no Maven profile activation by `jdk`, `os` or `file`; no `setup.py` execution; no `build.rs`; no archive opening; no network access from any parser (D-15, principle 6).
- Resolving versions the files do not contain: no registry lookup, no version-range solving, no `cargo update`, no Gradle dynamic-version resolution, no `npm ls`. `PomSource` is the only lookup, opt-in, Maven only.
- Licence detection beyond what a file *declares* (declared-only in v1: `license` of `package.json`, `pyproject.toml`, `Cargo.toml`, `<licenses>` of a POM, `license` of a `package-lock` entry). Vulnerability data.
- `setup.py`, `pdm.lock`, `conda`, `go.sum` and `vendor/modules.txt` (diagnosed by absence: no parser matches them), `buildSrc` and convention-plugin dependency extraction, `.tf`/Helm/Docker (unit 13), `bun.lockb` content (binary; diagnosed), `.pnp.cjs`, `Gemfile`.
- Writing to any other path than `20-design/`; scratch code is not part of the deliverable except the assets.

### 1.4 Directories covered (exact)

```
internal/manifests/
  tree.go                      RR-A2  BlobSHA, TreeResult, RunTree, assembleChecked   (test harness inside the root; see 2.3)
  parsertest/tree.go           RR-A4  LoadTree, TreeGolden, CheckTree, FuzzTree, CheckParse, AssertSamePaths
  internal/tomlx/              tomlx.go      Decode, Index, accessors                  (go-toml/v2 unstable parser; walk before decode)
  internal/expand/             expand.go     Expander (bounded ${...} expansion)
  internal/redact/             redact.go     URL (credential and query removal)
  internal/linex/              linex.go      byte offset to 1-based line
  internal/repopath/           repopath.go   Rel, Join, Dir, Match
  ecosystems/golang/           golang.go assemble.go diag.go
  ecosystems/npm/              npm.go packagejson.go packagelock.go pnpm.go yarn.go bun.go jsonx.go yamlx.go assemble.go diag.go
  ecosystems/python/           python.go pep508.go pyproject.go requirements.go locks.go pipfile.go setupcfg.go assemble.go diag.go
  ecosystems/maven/            maven.go pom.go xmlx.go assemble.go unit.go diag.go
  ecosystems/gradle/           gradle.go lex.go scan.go script.go settings.go catalog.go lockfile.go resolve.go assemble.go diag.go
  ecosystems/cargo/            cargo.go diag.go
  ecosystems/integration/      integration_test.go lockonly_test.go surface_test.go scale_test.go   (real runner end to end; compile-time surface; cost at scale; needs its matrix row, D-12)
```

Each ecosystem directory contains `<eco>.go` (ids, `Spec`, `Register`), one file per parser family, the assembler, `diag.go` (the three-line helpers `diag`, `diagAt`, `severityOf` that build a `Diagnostic` from the declared catalogue), `testdata/<case>/{input/,expected.golden.json}` (and `pomsource/` for Maven), `testdata/fuzz/FuzzParsers/` (committed crashers kept as seeds) and tests. Non-test source is 552 (golang), 2,825 (npm), 2,361 (python), 1,932 (maven), 3,034 (gradle) and 879 (cargo) non-blank lines.

---

## 2. Position in the architecture

### 2.1 Allowed imports and importers (measured with `go list` on 2026-10-14)

| Package | Rank | Internal imports | External imports | Imported by |
|---|---|---|---|---|
| `manifests/ecosystems/golang` | 5 | `manifests`, `manifests/internal/{limits,purl,repopath}` | `golang.org/x/mod/modfile` v0.42.0 | `cmd/scp` (composition), `integration` tests |
| `manifests/ecosystems/npm` | 5 | `manifests`, `manifests/internal/{limits,linex,purl,redact,repopath,version}` | `go.yaml.in/yaml/v3` v3.0.5 (pnpm lockfiles, own bounded walker `yamlx.go`) | `cmd/scp` |
| `manifests/ecosystems/python` | 5 | `manifests`, `manifests/internal/{limits,purl,redact,repopath,tomlx,version}` | none | `cmd/scp` |
| `manifests/ecosystems/maven` | 5 | `manifests`, `manifests/internal/{expand,limits,linex,purl,repopath}` | stdlib `encoding/xml` only | `cmd/scp` |
| `manifests/ecosystems/gradle` | 5 | `manifests`, `manifests/internal/{expand,limits,purl,repopath,tomlx}` | none | `cmd/scp` |
| `manifests/ecosystems/cargo` | 5 | `manifests`, `manifests/internal/{limits,purl,redact,repopath,tomlx,version}` | none | `cmd/scp` |
| `manifests/internal/tomlx` | 1 | none | `github.com/pelletier/go-toml/v2` v2.4.3 (`unstable` parser) | python, gradle, cargo |
| `manifests/internal/expand` | **2** | `limits` | none | maven, gradle |
| `manifests/internal/redact` | **2** | `limits` | none | npm, python, cargo |
| `manifests/internal/linex`, `repopath` | 1 | none | none | maven, npm; all six |
| `manifests/ecosystems/integration` (tests only, plus `doc.go`) | 6 (proposed row, D-12) | `manifests`, all six ecosystems, `manifests/parsertest`, `scm`, `freshness`, `job`, `readmodel`, `store`; otel API `noop` packages | none | nobody |

The architecture row of ecosystems (5.2) lists `x/mod`, `go-toml/v2`, `hcl/v2` and `x/text/encoding/charmap`; it does **not** list `go.yaml.in/yaml/v3`, which architecture 13.1 and ADR-021 assign to `npm` ("pnpm multi-document", checked by the verifier: the first revision of this paragraph attributed it to the 5.2 row; the matrix row should gain it, D-12). This unit reaches `go-toml/v2` through `tomlx` instead of from each package (one place that bounds the decoder, D-3) and `x/text/charmap` through `limits.CharsetReader` (doc 11 D-3), so no ecosystem imports either directly; `hcl` is unit 13's. `go.yaml.in/yaml/v3` is imported directly by `npm` only. Every edge decreases rank (5 to 4 to 2 to 1): `expand` and `redact` import `limits` and are therefore **rank 2**, so the rule "equal rank never imports equal rank" holds without an exception (the first revision called them rank 1 and asked for an exception, D-4; verification VF-12-11). The `integration` package is a test-only composition of every ecosystem and needs its own matrix row (D-12, VF-12-12); the `go list` import table above was re-measured by the verifier on the scratch module (identical to the table). No ecosystem imports another ecosystem (checked: `go list -f '{{.Imports}}'`), nothing imports an ecosystem except `cmd/scp` and tests, and no package here has a goroutine, a mutable global or an `init()` (A-18; regexps and error sentinels are package-level values that are never reassigned, and every table is a function).

### 2.2 Interfaces this unit implements and consumes

Implemented (defined by the root, doc 11 2.5 and 3.5, compiled in `surface_test.go`):

```go
type Parser interface {
	ID() string
	Version() int
	Ecosystem() EcosystemID
	Match(path string) bool
	Parse(ctx context.Context, f File, lim Limits) (Parsed, error)
}
type Kinded interface{ Kind() FileKind }            // every lockfile parser, the workspace parsers, bun.lockb
type Assembler interface {
	Ecosystem() EcosystemID
	Assemble(ctx context.Context, files FileSet, parsed []Parsed) ([]Unit, error)
}
type VersionedAssembler interface{ AssemblerVersion() int } // all six, returning 1 (Maven with a PomSource: 1|1<<16)
type FileSet interface { Paths() []string; Has(path string) bool }
```

Consumed (consumer-defined by the root, satisfied in `cmd/scp`, never imported from an adapter): only one.

```go
// manifests.PomSource, doc 11 2.4 (the satisfier is a hardened client in cmd/scp restricted to manifests.pom_source.hosts)
type PomSource interface {
	Fetch(ctx context.Context, id ComponentID, version string) ([]byte, error)
}
```

Exported by this unit (and nothing else; `go doc` of each package is short on purpose):

```go
// every ecosystem package
func Spec() manifests.EcosystemSpec                       // identity rule, purl type, version system, language, diagnostic catalogue
func Register(r *manifests.Registry) error                // RegisterEcosystem, Register for every parser, RegisterAssembler
func NewAssembler() manifests.Assembler                   // and the New<Parser> constructors, for tests and for cmd/scp assertions
// maven only
type Option func(*assembler)
func WithPomSource(src manifests.PomSource, allow func(manifests.ComponentID) bool) Option
func NewAssembler(opts ...Option) manifests.Assembler
func Register(r *manifests.Registry, opts ...Option) error
// python only: pure helpers also used by tests (candidates to become internal, OQ-9)
func NormalizeName(name string) string                     // PEP 503
type Requirement struct{ Name string; Extras []string; Specifier string; URL string; Marker string }
func ParseRequirement(s string) (Requirement, bool)        // PEP 508, hand-written scanner, linear time
func ExactPin(spec string) (string, bool)
// gradle only
const ID manifests.EcosystemID = "gradle"
```

Parser id constants (`PomID`, `ConfigID`, `ModParserID`, `WorkParserID`, `PackageJSONID`, `PackageLockID`, `PnpmLockID`, `PnpmWorkspaceID`, `YarnLockID`, `BunLockID`, `BunBinaryID`, `PyprojectID`, `RequirementsID`, `ConstraintsID`, `PoetryLockID`, `UVLockID`, `PylockID`, `PipfileID`, `PipfileLockID`, `SetupCfgID`, `SettingsID`, `ScriptID`, `CatalogID`, `LockfileID`, `PropertiesID`, `ManifestParserID`, `LockParserID`) are exported strings. There is **no `Config` type** in any package: an ecosystem has no knob of its own (section 6); enabling is the root's `manifests.acquisition.disabled_ecosystems`.

The composition in `cmd/scp` is the one sentence of doc 11 2.6, adapted (the real function names differ from the sketch there, see 2.4 row 2):

```go
must(golang.Register(reg)); must(npm.Register(reg)); must(python.Register(reg)); must(gradle.Register(reg)); must(cargo.Register(reg))
must(maven.Register(reg, maven.WithPomSource(pomSource, guard.Allow)))   // pomSource is nil unless manifests.pom_source.enabled
```

where `guard := svc.Guard()` is the root's `PrivacyGuard` (doc 11 RR-14); `WithPomSource` is simply omitted when the source is disabled, and a nil `allow` disables the source (programming error, never an open door).

### 2.3 Requirements on the core unit (RR-A1 to RR-A8) and on other units

The prototype was built and tested against the verified core scaffold. Seven small changes to the root were needed; they are applied in `patches/core-rr-a.diff` (`model.go`, `sanitize.go`, `refresh.go`, 119 lines; plus the new files `tree.go` and `parsertest/tree.go`) and the whole module still passes its own 97 tests. Doc 11 should adopt them; the owner decides (D-1).

| Id | Requirement on `internal/manifests` | Why (evidence) | State |
|---|---|---|---|
| RR-A1 | `EcosystemSpec.Diagnostics []DiagnosticDoc` and `type DiagnosticDoc struct{ Code string; Severity Severity; Summary string }` | The seventh fuzz invariant of this unit ("only declared diagnostics") and the generated reference page of diagnostic codes need the catalogue to live next to the spec; 92 codes are declared (section 4, per ecosystem) | patch applied |
| RR-A2 | `manifests/tree.go`: `BlobSHA(data []byte) string`, `type TreeResult`, `RunTree(ctx, reg, cfg Config, files map[string][]byte) TreeResult`. It runs discovery, the parse path (`StripBOM`, `RejectBinary`, `limits.Run`, `sanitizeParsed`, kind and `FileRef` stamping, `stampPaths`, `rebasePaths`), the registered assembler (with a snapshot check, `assemble.mutated-input`) and `finalize`, with no store, no GitHub and no goroutines of its own | Multi-file trees (a `pom.xml` and its parent, a workspace with its lock) cannot be tested through `parsertest.Golden`, which is per file. The harness needs unexported runner functions, so it cannot live in `parsertest` | patch applied (new file) |
| RR-A3 | `sanitizeParsed` keeps a component's own `Ecosystem` when one is named; only an empty ecosystem defaults to the parser's; identity rules are those of the component's ecosystem (`purl.Normalize(eco, ns, name)` for a foreign one) | Gradle emits `maven` targets from `gradle` units, and unit 13's Actions parser emits `docker` targets (its RR-11, accepted in doc 11 2.8). The scaffold rewrote the ecosystem of every target to the parser's own, silently turning `maven:org.slf4j/slf4j-api` into `gradle:org.slf4j/slf4j-api` (found by the golden of `gradle/kotlin-multiproject`; `TestSanitizeKeepsForeignEcosystem`, overlay file `internal/manifests/sanitize_foreign_test.go`) | patch applied |
| RR-A4 | `parsertest/tree.go`: `LoadTree`, `ViewOf`, `TreeConfig`, `TreeConfigLimits`, `TreeGolden` (flag `-update-tree`, three runs for determinism), `CheckTree`, `Seeds`, `FuzzTree`, `FuzzLimits`, `CheckParse`, `AssertSamePaths` (the eight invariants of 4.1.9 plus the assembler invariants) | the shared test engine of the six packages (about 600 lines) | patch applied (new file) |
| RR-A5 | `func (d Dependency) Clone() Dependency` (private copy of `Extra`), and the rule "an assembler never writes to a `Parsed` it received". The root's own `defaultAssemble` must follow it: it stamps `LocPath` through `Dependency.Extra`, which is shared with the cached `Parsed`, so the default fallback of a one-file tree mutates the parse cache | The setters (`SetSource`, `SetCondition`, `SetLocPath`) write through the `Extra` pointer. Found by the assembler fuzzer as `assemble.mutated-input` on a one-file tree that fell back to the default assembly. `assembleChecked` therefore snapshots only around the registered assembler | patch applied for `Clone`; the `defaultAssemble` fix is a task of unit 11 (M0-1) |
| RR-A6 | `LockedComponent.Display string`: original-case name when the identity was case-folded (Go module paths, Cargo crates); cleaned by `sanitizeParsed` | `ComponentID` of Go and Cargo is lower case; the API and the SBOM must show `github.com/Azure/azure-sdk` and `Serde`-style names; `Artifact.Display` and `Dependency.TargetDisplay()` already exist, the locked component had none | patch applied |
| RR-A7 | `stampPaths(*Parsed)`: the runner fills an empty `Loc.Path` of parser diagnostics and locked components with `File.Path` (copying both slices first: `Parsed` comes from a shared cache), after `File.Kind` and before `rebasePaths` | A parser cannot know its path (output is cached by blob SHA); an assembler merges diagnostics of several files into one unit, so only the runner knows which file a diagnostic came from. Without it the unit diagnostics of a workspace say "line 12" without a file | patch applied |
| RR-A8 | `GlobMatch` must be polynomial. Today `**` mixed with `*` segments is exponential: `GlobMatch(strings.Repeat("*/**/", 16)+"x", 30 segments)` takes 6.97 s (8 repeats: 70 ms, 12: 1.67 s), measured 2026-10-14. A workspace glob comes from a repository file, so a hostile repository can occupy a refresh job for the whole timeout through the default assembler's `ExpandMembers` | `repopath.Match` of this unit is a dynamic-programming matcher (bounded pattern length and 16 segments; `TestMatchIsPolynomial`) and can replace the root's implementation line for line | not patched (unit 11 owns `glob.go`); M0-2 |
| RR-A9 | **Facts that depend on state outside the tree SHA.** The Maven assembler's output depends on (a) whether a `PomSource` is wired, (b) whether the source answered, (c) what the `PrivacyGuard` knew when it was asked (the guard is built on the all-repositories graph, doc 11 RR-14). The root's change detection (`refresh.go` lines 59 and 72: stored `TreeSHA` equal and `ParserSet` equal, not forced, gives `Unchanged` without re-assembling) sees none of these. Requested of unit 11: (1) `Service.Guard().Allow` answers **false until the first complete all-repositories graph build** (cold start: an internal coordinate whose producer repository is not parsed yet must not leave the process); (2) a repository whose facts carry `maven.parent-unresolved` with reason "not found" or `maven.snapshot-private-skipped` is re-assembled (not `Unchanged`) at most every `idle_backoff_max` while the PomSource is enabled, so a transient failure or a guard that learned more heals without a push. (a) is handled inside this unit: `AssemblerVersion()` differs with and without a source (4.5.6, VF-12-06) | not patched (unit 11); OQ-14, L-11, L-12 |

Requirements on other units: none new. Unit 13 (sibling) is informed of RR-A3 because its Actions parser depends on it; its proposed `manifests/internal/yamlsafe` overlaps this unit's `npm/yamlx.go` (D-7, OQ-6).

### 2.4 Reconciliation with the core document (doc 11) and its scaffold

| # | Topic | Doc 11 or scaffold says | This unit | Resolution |
|---|---|---|---|---|
| 1 | Parser id rule | `<EcosystemID>/<format>` | `golang/*`, `npm/*`, `pypi/*`, `maven/*`, `gradle/*`, `cargo/*` | follows; `gradle` is a new ecosystem id (D-5) |
| 2 | Registration helper | 2.6 sketch: `reg.Register(npm.NewPackageJSON())`, `maven.NewAssembler(pomSource, guard)` | one `Register(reg)` per package (spec, parsers, assembler); Maven takes options: `maven.Register(reg, maven.WithPomSource(src, guard.Allow))` | doc 11's sketch is prose-level ("not compiled"); the real signatures are in 2.2 |
| 3 | `PomSource` | `Fetch(ctx, ComponentID, version) ([]byte, error)` | used exactly so; the coordinate is passed with the `maven` ecosystem | follows |
| 4 | `JoinLock`, `ExpandMembers`, `GlobMatch` | exported helpers for ecosystem authors (OQ-6 of doc 11 asks unit 12 to accept them) | **not used**. Each ecosystem joins its own lock with richer rules (per-importer versions, aliases, reachability scopes, `--hash` integrity) and reports `<eco>.lock-stale`/`.lock-missing-dep` with file and line; members are matched by `repopath.Match` (bounded, with negation and `exclude`) | D-2; RR-A8 for `GlobMatch` |
| 5 | `FormatPURL` hook | optional, `nil` means the default formatter | not used: every ecosystem of this unit has an official purl type and the default `purl.Format` is right (golang namespaces, npm scopes, pypi names, maven groups, cargo crates; Gradle uses `maven`) | follows |
| 6 | `Kinded` | optional, selects the lockfile size limit | all lockfile parsers implement it; `requirements.txt` produced by `pip-compile` is a lockfile by content, so the parser sets `Parsed.Kind = KindLockfile` itself but the size limit stays the manifest limit (1 MiB; a pip-compile file over that size is a `limit.size` diagnostic, acceptable) | follows |
| 7 | `Weigher` | optional, default amplification 10 | none implemented. Allocation volume per input byte measured 2026-10-14 (E-A5): 4.8 (uv.lock) to 28.6 (Yarn Berry); heap still retained after a GC: 0.6 to 2.8 bytes per input byte. The semaphore weight models resident peak, not allocation volume, so the default of 10 is kept | follows; OQ-3 |
| 8 | `Parsed.Members`, `Includes` | file-relative, not rebased | `Members` are globs or paths relative to the file's directory (`repopath.Rel` validated); `Includes` is filled by `requirements*.txt` only, the assembler joins and resolves among the candidate files | follows |
| 9 | Dependency `SourceRef` of kind path | relative to the declaring file, rebased by `rebasePaths`, escapes dropped with `assemble.path-escape` | all parsers emit the cleaned relative path (`repopath.Rel`: never absolute, `..` allowed); absolute paths become `SourcePath` with an empty ref and a `<eco>.*-absolute|-invalid` diagnostic | follows |
| 10 | `SourceProject` | build-tool project reference, "an assembler resolves it to `SourcePath`" | npm `workspace:`, uv `workspace = true` and Gradle `project(":x")` are emitted as `SourceProject` by the parser and **always** resolved or demoted by the assembler; a unit never keeps `SourceProject` (`CheckTree` asserts it) | follows |
| 11 | Parse-fail handling | `Parse` returns an error only on context end | follows; every other problem is a diagnostic | follows |
| 12 | `Artifact.Kind` | closed enum (`library application plugin bom image`, anything else is `other` at the API boundary) | Go, npm, Python and Cargo produce `library` (Cargo: `application` for bin-only crates); Maven also `application` (war, ear), `plugin` (maven-plugin) and `bom` (a pure bill of materials); Gradle `library` or `application` (a project that applies an application plugin) | follows |
| 13 | Technology: Kotlin | "Kotlin is added for Maven units with `org.jetbrains.kotlin` dependencies or a `kotlin` toolchain entry" | a Gradle unit has ecosystem `gradle`, so the dependency rule does not fire; both the Maven and the Gradle assembler therefore set `Toolchain["kotlin"]` (version, else `unknown`) when they detect Kotlin | follows (E-A6) |
| 14 | Golden form | `testdata/<case>/<manifest>` plus `expected.json`, inputs at most 64 KiB (12.4) | a case is a directory tree `input/` plus `expected.golden.json` (parsed files, units, diagnostics), driven by `TreeGolden`; real fixtures are larger than 64 KiB where that is the point (BOM POMs of 97 KB under `pomsource/`; the largest golden is 153 KB) | D-8 |
| 15 | `Spec.Language` | one default language per ecosystem | Java for Maven and Gradle; the Kotlin refinement of row 13; JavaScript and TypeScript (the core promotes it when `typescript` is a dependency); Python; Go; Rust | follows |

### 2.5 What was compile-checked

A scratch module `github.com/cantara/source-code-portal` (Go 1.27.1, darwin/arm64; Go 1.27.2 was released later the same day and changes nothing here) is built by `apply.sh` from the verified core scaffold (`11-manifests-core.assets`, as revised by its verifier at 03:33 to 03:44 on 2026-10-14), the overlay and two patch files. `gofmt -l .` is empty, `go vet ./...` is clean, the purity guard of `apply.sh` (no goroutine, `init`, clock, `os` or network import in the six packages and five helpers) passes and `go test -race -count=1 ./internal/manifests/...` passes (281 passing test and subtest executions in the six ecosystem packages alone, among them 44 golden trees and 23 fuzz regression seeds; recounted by the verifier, who also re-ran `apply.sh` from scratch after the fixes of Appendix F: green, including `gofmt`, `go mod tidy -diff` and the purity guard, which now covers `os/exec`, `sync/atomic`, `math/rand/v2`, `crypto/rand`, `net` and `runtime` and fails on an unformatted file). `integration/surface_test.go` asserts every exported signature of section 2.2 at compile time, and `TestRealRunnerMatchesRunTree` runs every golden tree through the **real** `manifests.refresh` job (fake GitHub, fake store, real parse cache and assembly) and requires the same units, counts and diagnostics as the pure `RunTree` harness. Appendix C lists the compiled surface.

---
## 3. Domain model and Go types

The shared model (`ComponentID`, `Dependency`, `Artifact`, `LockedComponent`, `Parsed`, `Unit`, `Diagnostic`, `Limits`, `File`, `FileRef`) is exactly doc 11 3.1 to 3.4 plus the three additions of 2.3 (`DiagnosticDoc`, `Dependency.Clone`, `LockedComponent.Display`) and is not repeated. This section fixes how *this unit* fills it, then lists the helper types it adds.

### 3.1 Field-filling conventions shared by all six ecosystems

| Field | Rule |
|---|---|
| `Dependency.Target` | normalised by the identity rule of 3.2; never an empty name (an entry whose name cannot be determined is dropped with a diagnostic or, for Gradle project placeholders, left nameless so the runner drops it). Identities that cannot round-trip through a purl (a `/`, `\`, `:` or space in a Maven coordinate, an empty Go path segment) are cleaned or rejected in the parser (F-A1, F-A2, F-A4) |
| `Dependency.TargetDisplay()` | original-case or alias-resolved name when `Target` was folded or reduced (Go module path, Cargo crate as written in `package =` or as the key, Python name as written, Maven `group:artifact`, npm alias target: the **real** package, never the local alias). Exception, parser output only: a Cargo `{ workspace = true }` entry and a `[workspace.dependencies]` entry carry their TOML **key** here until the assembler resolves the pool (inheritance is by key, VF-12-07) |
| `Dependency.Requirement` | the constraint exactly as written, cleaned and capped (128 to 512 bytes by ecosystem); empty for path, git and URL sources |
| `Dependency.Version` | the exact version when the file names exactly one (`==1.2`, `=1.2.3`, `[1.2]`, `1.2!!`, a Go `require`, a lock entry); otherwise empty |
| `Dependency.Resolution` | `ResPinned` for an exact pin written in the manifest or a Go `require`; `ResManaged` for a version that came from `dependencyManagement`, a BOM, a Gradle constraint, a catalog, an npm or pnpm `catalog:` or a constraints file; `ResLocked` only from a lock join; `ResDeclared` otherwise |
| `Dependency.Scope` | per ecosystem (4.2 to 4.7): `runtime`, `dev`, `test`, `build`, `optional`, `peer`, `provided`; never a value invented for an unknown configuration (`unknown` is allowed where the file does not say) |
| `Dependency.Group` | the original section or configuration name (`dependencies`, `devDependencies`, `implementation`, `dependency-groups.test`, `workspace.dependencies`), the marker `managed`, `plugin`, `plugin-managed`, `extension`, `parent`, `import` or `constraint:<group>` for entries that are not ordinary dependencies and are consumed by the assembler |
| `Dependency.Condition()` | raw, never evaluated: Cargo `cfg(...)` target table, PEP 508 marker, Maven `profile:<id>` (or `profile:<id>=active` when the assembler could show the profile is active), Gradle `allprojects`, `subprojects`, `project:<path>`, `inherited:<context>`, `buildscript` |
| `Dependency.Source`, `SourceRef()` | `SourceRegistry` with an empty ref for the ecosystem's default registry (a custom Cargo registry is `registry:<name>`, a Go `replace` to another module is `<path>@<ver>`); `SourceGit` with the redacted URL plus `#<ref>`; `SourcePath` with the cleaned path relative to the declaring file; `SourceURL` with the redacted URL; `SourceProject` (npm `workspace`, uv `workspace`, Gradle project path) only in parser output |
| `Dependency.Confidence` | `ConfExact` for structured files; `ConfHeuristic` for every Gradle script and for `requirements-*.txt` whose scope was guessed from the file name. **Maven emits `ConfExact` only** (the first revision promised `ConfHeuristic` for an unresolved or `maven.config`-supplied value; no code path did that and no golden contained it, VF-12-09): an unresolved placeholder keeps `Version` empty, stays in `Requirement` and carries `maven.property-unresolved`, which is the honest signal |
| `Dependency.Line` | 1-based line of the declaring token (`ix.Line` of `tomlx`, `jnode.line`, YAML node line, XML start offset through `linex`); 0 when unknown; the path half is stamped by the runner (RR-A7) or, for a dependency read from an inherited file, by the assembler through `SetLocPath` |
| `Artifact` | `Display` is the original-case name (`com.acme:widget`, `github.com/Org/Repo`, `@scope/name`); `Kind` is `library` or `application` (Maven also `plugin` and `bom`); `Version` may be a placeholder (`${revision}`) |
| `Parsed.Kind` | `KindManifest` for project files, `KindLockfile` for locks (including `requirements.txt` produced by `pip-compile` or `uv pip compile`, decided from the header comment), `KindCatalog` for Gradle catalogs and `constraints*.txt`, `KindWorkspace` for files that only aggregate (`go.work`, `pnpm-workspace.yaml`, `.mvn/maven.config`, `settings.gradle*`, `gradle.properties`, a virtual Cargo workspace root) |
| `Parsed.Properties` | only what the assembler needs; the keys are a contract between a parser and its assembler (3.3) and appear in no API output |
| `Parsed.Toolchain` | `language`, `tool`, plus language and tool constraints (`go`, `toolchain`, `node`, `engines.*`, `python`, `rust`, `edition`, `java`, `kotlin`, `packaging`, `dsl`, `android`, `module`) |
| `Parsed.Licenses` | declared SPDX strings or names only, capped at 200 bytes each |
| `Parsed.Diagnostics` | catalogued codes only (3.5), message a constant sentence plus at most one quoted token, at most 100 per file in the runner |

**Never-fail rule.** `Parse` returns a non-nil error only when `ctx` is done (then the error is `ctx.Err()` and the runner records `timeout` or `canceled`). Every other problem (malformed input, a limit hit, an unsupported encoding, an unknown construct) yields the recovered part of `Parsed` plus a diagnostic. This is what lets the runner cache a "parsed with diagnostics" result against `(blobSHA, parserID, parserVersion)` instead of retrying.

### 3.2 Identity table (ComponentID, display, purl)

`Normalize` is the root's `purl.NormalizeFunc(<ecosystem>)`; this unit restates it in each `Spec()` so the catalogue travels with it and does not change it.

| Ecosystem (`EcosystemID`) | Namespace | Name | `Normalize` | `PURLType` | `VersionSystem` | purl example |
|---|---|---|---|---|---|---|
| `golang` | module path up to the last `/`, lower case | last path segment, lower case; empty segments are dropped first (F-A1) | lower both | `golang` | `go` | `pkg:golang/github.com/azure/azure-sdk-for-go@v68.0.0` (display `github.com/Azure/azure-sdk-for-go`) |
| `npm` | scope without `@` (empty if unscoped) | name as written | trim scope | `npm` | `npm` | `pkg:npm/%40babel/core@7.24.0` |
| `pypi` | empty | PEP 503 normal form (`typing_extensions` to `typing-extensions`) | PEP 503 | `pypi` | `pypi` | `pkg:pypi/typing-extensions@4.12.2` |
| `maven` | groupId | artifactId; `classifier` and `type` (other than `jar`) are qualifiers | none (case-sensitive) | `maven` | `maven` | `pkg:maven/org.slf4j/slf4j-api@2.0.13` |
| `gradle` (unit ecosystem only) | the targets are `maven` identities | | | `maven` | `maven` | `pkg:maven/com.squareup.okhttp3/okhttp@4.12.0`; plugin `id("org.jetbrains.kotlin.jvm")` is `pkg:maven/org.jetbrains.kotlin.jvm/org.jetbrains.kotlin.jvm.gradle.plugin@2.0.21` (the plugin-marker coordinate, V-9) |
| `cargo` | empty | crate name, lower case, `_` folded to `-` (registry rule; the purl spec prescribes none, V-15) | the root rule | `cargo` | `cargo` | `pkg:cargo/serde-json@1.0.120` |

Local project references (a Go `replace => ../x`, a Cargo `path = "../x"`, an npm `file:../x`, a Maven module, a Gradle `project(":x")`) keep the **package identity** the file names when it names one (Go, Cargo, npm) and set `SourcePath` plus the relative path; where the file names no package (a Gradle project without a known `group`) the identity is `maven` with an empty namespace and the project path as name (`maven::tools` for `:tools`, display `:tools`), `SourcePath` and the directory. The sentinel namespace `_local` of doc 13 RR-10 is not used by this unit because every path dependency here either names its package or is resolved to a unit by the assembler.

Other `EcosystemSpec` fields: `DisplayName` Go modules, npm, Python, Maven, Gradle, Cargo; `Language` Go, JavaScript, Python, Java, Java (Kotlin refined by 2.4 row 13), Rust.

### 3.3 Properties key contracts (parser to assembler)

Keys are namespaced by a colon prefix; values are cleaned strings; every writer caps the number of keys (npm 500 overrides, Maven 20,000, Gradle 20,000, Cargo and Python bounded by the list sizes).

| Ecosystem and parser | Key | Value | Consumed by |
|---|---|---|---|
| golang `golang/mod` | `replace:<old>[@<ver>]` | `<new>[@<ver>]` (a local path stays as written, relative to the `go.mod`) | assembler (go.work override) |
| golang `golang/work` | `replace:<old>[@<ver>]` | `path:<rel to go.work>` for a local target, else `<new>[@<ver>]` | assembler |
| npm `npm/package-json` | `alias:<npm: spec>` | local name of an `npm:` alias | lock join |
| | `override:<path>` | override value; nested objects joined with `>` (kept, never applied) | informational |
| | `catalog:<name>:<pkg>` | range; the unnamed catalog is `default` (`bun`, `yarn` catalogs) | `catalog:` resolution |
| | `private` | `true` | informational |
| npm locks | `lockfileVersion` | `1`, `2`, `3`, pnpm `6.0` to `9.x`, yarn `1` or the Berry `__metadata.version`, bun `1` | assembler (Berry or v1 selection) |
| | `lock:<dir>:<name>` | locked version of the direct dependency `<name>` of importer `<dir>` (`.` root) | lock join |
| | `desc:<descriptor>` | yarn: version (alias: `real@version`) of a descriptor `name@range` | lock join |
| | `catalog:<name>:<pkg>`, `override:<k>` | pnpm catalogs and overrides of the lockfile | catalogs fallback |
| npm `npm/pnpm-workspace` | `catalog:...`, `override:...` | as above | catalogs |
| python `pypi/pyproject` | `exclude:<rel>` | `1` (uv workspace exclude glob) | uv membership |
| python `pypi/requirements` | `include:<rel>` | `r` | include resolution |
| | `constraint:<rel>` | `c` | constraint application |
| python locks | `lock.version` | lock format version | informational |
| | `root:<i>` | directory (relative to `uv.lock`) of the editable or virtual package `<i>` | uv join |
| cargo `cargo/manifest` | `version.workspace`, `edition.workspace`, `rust-version.workspace`, `license.workspace` | `true` (inheritance marker) | inheritance |
| | `workspace.package.<k>` | `version`, `edition`, `rust-version`, `license` of `[workspace.package]` | inheritance |
| | `exclude:<rel>`, `resolver`, `publish` | `1`, `1` to `3`, `false` | membership; informational |
| | `pool.real:<key>` | the real crate of a renamed `[workspace.dependencies]` entry (`key = { package = "real" }`) | inheritance (display of the resolved dependency) |
| cargo `cargo/lock` | `lock.version`, `local:<i>` | lock format; `1` for a package without `source` (workspace member or path dependency) | assembler |
| maven `maven/pom` | `packaging`, `name`, `parent.relativePath`, `parent.relativePath.default` | text; `true` when `<relativePath>` was absent | assembler |
| | `prop:<name>` | `<properties>` value | interpolation |
| | `profile-ids` | profile ids joined by a tab | activation |
| | `profile:<id>:prop:<name>`, `profile:<id>:activeByDefault`, `:jdk`, `:os`, `:property`, `:file` | profile properties and activation facts | activation |
| maven `maven/config` | `prop:<name>`, `profiles` | `-D` properties; `-P` ids joined by a comma | interpolation, activation |
| gradle `gradle/settings` | `rootProject.name`, `includeBuild:<path>`, `plugin:<id>`, `catalog-file:<name>`, `catalog-remote:<name>`, `cat:<name>:version:<alias>`, `var:<name>` | text | assembler |
| gradle `gradle/script` | `plugin:<id>`, `var:<name>`, `group`, `version`, `all:group`, `all:version`, `sub:group`, `sub:version`, `java` | text | assembler |
| gradle `gradle/catalog` | `version:<key>`, `bundle:<name>` | version string; member aliases joined by a tab | assembler |
| gradle `gradle/properties` | `prop:<key>` | value; `<redacted>` when the key looks like a credential | interpolation |

### 3.4 Helper packages (new; rank 1 except `expand` and `redact`, rank 2)

Compiled signatures (the full code is in the assets):

```go
// manifests/internal/tomlx: bounded TOML decode with a key-to-line index
func Decode(data []byte) (map[string]any, Index, error)       // ErrDepth: a header or dotted key above MaxKeySegments (32); ErrKeys: more than MaxKeysPerTable (10,000) direct keys in one table
func Code(err error, syntax string) string                    // "limit.depth" for ErrDepth, "limit.nodes" for ErrKeys, else the ecosystem's syntax code
type Index map[string]int                                      // "package[3].name" to 1-based line
func (ix Index) Line(paths ...string) int
func (ix Index) KeysBySource(prefix string, m map[string]any) []string // keys of a table in source order, not map order
func Map(v any) map[string]any; func List(v any) []any; func Str(v any) (string, bool); func Bool(v any) (bool, bool)
func At(tree map[string]any, keys ...string) any; func SortedKeys(m map[string]any) []string

// manifests/internal/expand (rank 2): bounded ${name} expansion (Maven properties, Gradle templates)
type Expander struct{ Shared *int; Budget, MaxValue int; Tripped bool }
func New(shared *int) *Expander                                // 1 MiB per value, 8 MiB per unit, optional run-wide budget
type Lookup func(name string) (raw string, known bool)
func (e *Expander) Expand(s string, look Lookup, memo map[string]string, stack []string) (out string, unresolved, cyclic []string, err error)
func IsExpand(err error) bool
const MaxDepth = 16; const MaxPlaceholder = 256

// manifests/internal/redact (rank 2)
func URL(s string) string                                      // strips userinfo (also scheme-relative "//u:p@h"), credential-looking query keys ("&" and ";") and fragment parameters, clips to 300 bytes

// manifests/internal/linex
func New(data []byte) Index; func (l Index) Line(off int) int; func (l Index) Count() int

// manifests/internal/repopath
func Rel(p string) (string, bool)                              // validated, cleaned, never absolute, ".." allowed (the runner judges escapes)
func Join(dir, rel string) (string, bool)                      // repo-relative join; false when it escapes, is absolute or hostile
func Dir(p string) string                                      // "" for the root, never "."
func Match(pattern, name string) bool                          // path.Match segments plus "**"; dynamic programming; pattern at most 256 bytes and 16 segments
```

Why a new `tomlx` rather than `toml.Unmarshal` into `map[string]any`: go-toml/v2 offers no line numbers on the decoded tree, and research 07 E3 measured 3.8 s for 40 KB of nesting in the decoder of an earlier version. `tomlx` walks the `unstable` parser **first** (linear), builds the key-to-line index in that pass, refuses a key path above 32 segments and a table with more than 10,000 direct keys, and only then hands the document to `toml.Unmarshal`. Three facts the verifier measured on 2026-10-14 (appendix B, E-A8) change what the first revision said:

1. The first revision called `toml.Unmarshal` **before** the walk, so the depth cap did not protect it, and took line numbers from `unstable.Parser.Shape`, which rescans the document from its start on every call. `Decode` was therefore quadratic in the number of keys: a 1 MiB `Cargo.lock` took 6.7 s, 2 MiB 27 s, 8 MiB (the lockfile limit) 443 s, in code that cannot be interrupted by the 2 s watchdog (VF-12-01). Lines now come from a one-pass newline table and a binary search; the same 8 MiB lock takes 0.24 s.
2. go-toml/v2 v2.4.3 checks duplicate keys of one table with a linear scan per key (`internal/tracker.SeenTracker.find`): 40,000 keys in one table cost 1.2 s and 90,000 (under the 1 MiB manifest limit) about 6 s, and about 40 times that under `-race`. `tomlx` refuses a table above `MaxKeysPerTable` = 10,000 direct keys (`limit.nodes`, VF-12-02); the largest real table seen in the corpus has under 1,000 (a Gradle catalog `[libraries]` of 2,000 to 3,000 entries is the realistic maximum). Arrays of tables (`[[package]]`) are separate tables and are not affected.
3. The earlier claim that a 100,000-segment header takes 37 s in `toml.Unmarshal` could not be reproduced with v2.4.3 (the whole `Decode` of such a header, including the walk, returns `limit.depth` in 16 ms). The depth cap stays as defence in depth, and the claim is dropped from this document (E-A4).

The `unstable` package is documented by its author as subject to change ("APIs that do not meet the backward compatibility guarantees yet", re-read 2026-10-14); the pin in `go.mod` and `TestDecodeIndex` are the guard (OQ-4).

### 3.5 Diagnostic helpers and catalogues

Each package has the same three functions (`diag.go`, 30 lines): `diag(code, line, format, args...)` builds a `Diagnostic` of a catalogued code with its declared severity and a message cleaned and clipped to 240 bytes and no path (a parser does not know its path); `diagAt(code, file, line, ...)` adds the path (assemblers are not stamped by the runner); `severityOf(code)` reads the catalogue and defaults to a warning for the root's shared `limit.*` and `encoding.*` codes. The catalogue is the `Diagnostics` field of `Spec()` (RR-A1) and is the only list of codes the package may emit; `parsertest.CheckParse` and `CheckTree` fail on any other code (invariant 7 of 4.1.9). Shared codes of the root that appear in this unit's output: from the parsers `limit.size`, `limit.depth`, `limit.nodes`, `limit.expand` and `encoding.unsupported`; added by the runner `limit.deps`, `limit.locked`, `limit.timeout`, `parse.invalid-dependency` (a nameless identity was dropped), `assemble.path-escape` and `assemble.failed`; `assemble.mutated-input` exists only in the `RunTree` test harness (RR-A2). The catalogue tables are in 4.2 to 4.7.

---

## 4. Behavioural specification

### 4.1 The contract every parser and assembler of this unit obeys

#### 4.1.1 Parser skeleton

```
Parse(ctx, f, lim):
    lim := lim.WithDefaults()                       // the runner resolves the size limit (manifest 1 MiB, lockfile 8 MiB) before the call
    if len(f.Data) > lim.MaxBytes: return Parsed{Kind, [limit.size]}                   // a direct caller is safe too
    data, err := limits.Input(f.Data)               // BOM stripped; UTF-16, NUL, invalid UTF-8 rejected
    if err:  return Parsed{Kind, [encoding.unsupported]}
    g := limits.NewGuard(ctx, lim)                  // depth, node count, context polling every 256 ticks
    tree, err := decode(data, g)                    // go-toml (tomlx), own JSON, own XML token loop, yaml.v3 + yamlx walker, modfile, own lexers
    if err:  if ctx.Err() != nil { return Parsed{}, ctx.Err() }
             return Parsed{Kind, [limit.<name> | <eco>.syntax]}                         // never the document, never the raw parser message beyond 160 bytes
    read the tree into Parsed in source order; g.Node() once per token, line or entry; map iteration only through SortedKeys or KeysBySource
    return Parsed, nil
```

Rules that follow from doc 11 4.1 and that every parser here satisfies (each has a named test in section 12):

1. Third-party decoders are called only after the size and encoding checks, and never on input a hostile 8 MiB file could blow up: `modfile` (linear), `go-toml/v2` through `tomlx` (key-path cap, per-table key cap, walk before decode), `encoding/xml` (strict, no DTD processing, no external entities, 1 MiB text cap per element, token and depth budgets), `go.yaml.in/yaml/v3` (at most four documents, aliases dereferenced up to four levels and every visited entry charged to the guard).
2. Own tokenisers (JSON `jsonx.go`, Gradle `lex.go`, requirements, INI, yarn v1, bun JSONC) call `g.Node()` per token. Their cost is linear (E-A1).
3. Every output string passes `manifests.CleanText`; every URL passes `redact.URL`; every path passes `repopath.Rel`. A diagnostic message never contains more than one quoted token of the file.
4. Output order is a function of the input bytes: sorted keys, source order, no map order.
5. A parser never reads `File.Path` to decide its output (path independence, 4.1.9); facts that depend on the name of the file (scope of `requirements-dev.txt`, `.kts` versus Groovy, `npm-shrinkwrap.json` versus `package-lock.json`, the configuration of a per-configuration Gradle lock file) are added by the assembler.
6. No goroutine, no clock, no random source, no `os`, no `net`, no `log`, no mutable global.

#### 4.1.2 Assembler skeleton

```
Assemble(ctx, files, parsed):
    group the Parsed values by ParserID and directory (inputs are sorted by path then parser id by the runner)
    per unit: copy every slice and Extra of the Parsed values it reads (Clone); never write to a Parsed
    resolve files that cross directories (workspace roots, parents, includes, catalogs) among the candidate files only
    judge membership with repopath.Match (patterns, negation, exclude); diagnose a member that is not among the fetched files
    join lock data per importer; compute scopes and relationships by reachability where the lock has edges
    return units sorted by directory; return NIL (not an empty slice) when no unit was placed
```

An assembler returns `nil` **exactly** when it placed no unit, so the root's default assembly shows what was parsed (a lockfile without a manifest, a `go.work` without `go.mod`); an empty non-nil slice would hide the files (`TestLockOnlyDirectoriesStillShow`). It polls `ctx` once per unit. It never fabricates a version; an unresolved value stays empty and carries a diagnostic. Directory-crossing lookups use `FileSet` only for existence (`Has`) and otherwise only the `Parsed` values.

#### 4.1.3 Encoding policy

UTF-8 BOM is stripped; UTF-16 and NUL bytes are rejected with `encoding.unsupported` before any parser runs (`bun.lockb`, the binary Bun lock, therefore never reaches its parser: the assembler reports it from `files.Has`, 4.3.7); invalid UTF-8 in text formats is rejected. Maven POMs may declare `iso-8859-1` or `windows-1252` in the XML prolog and are decoded by `limits.CharsetReader` (A-26; case `latin1`); every other declared charset is `encoding.unsupported`. CRLF is accepted everywhere; a lone CR counts as a space in YAML (so locations stay in range).

#### 4.1.4 Secrets

Credentials in URLs (`https://user:pw@host/x.git`, `?token=...`, `#token=...`) are removed by `redact.URL` before a value reaches `SourceRef`, `Requirement`, a property or a diagnostic; `gradle.properties` values whose key looks like a credential (`pass`, `secret`, `token`, `credential`, `key`, `auth`, `cert`, `signing`, `user`, `login`, `pwd`) are stored as `<redacted>` and never interpolated (`TestSecrets` in python, gradle, cargo, npm; `TestNoCredentialsEmitted` in maven). Index URLs of `requirements.txt` (`--index-url`) are ignored with `python.option-ignored` and never copied.

#### 4.1.5 Determinism and duplicates

Two runs on the same bytes give deeply equal results (invariant 2; `TreeGolden` runs `RunTree` three times). Duplicate declarations: the same `(target, scope, group, condition)` collapses to the first (npm, python), except where a duplicate carries a condition (python markers) or a different requirement (Maven keeps both only across different `profile` conditions). Lock entries are grouped by `(ComponentID, version)` and sorted by id then version (npm), or kept in file order (Cargo, uv, poetry).

#### 4.1.6 Match and excluded directories

`Match` looks at the path only, case-sensitive on the base name. Each ecosystem refuses paths under directories that hold copies of foreign projects: `vendor`, `node_modules` (Go, Cargo, npm), `site-packages`, `.venv`, `venv`, `.tox`, `.nox`, `__pypackages__` (Python), `target`, `archetype-resources`, `src/it`, `src/test/resources` (Maven), `build`, `.gradle`, `out`, `src/test/resources` (Gradle). The root's `acquisition.exclude_segments` applies before `Match` is asked; these are the second defence. Matches verified on 2026-10-14 (rows of 4.2 to 4.7): `setup.py`, `pdm.lock` and `go.sum` are matched by no parser; `bun.lockb` is matched only to be diagnosed.

#### 4.1.7 Limits

| Limit | Default | Where it bites in this unit |
|---|---|---|
| bytes | manifests 1 MiB, lockfiles 8 MiB | parser entry (`limit.size`) and discovery |
| depth | 256 | JSON, XML, YAML nesting; Gradle blocks; TOML key path 32 |
| nodes | 2,000,000 | tokens and entries; also `maxProps` 4 x 5,000 (Maven), 20,000 (Gradle), 200,000 lines (requirements), 200,000 yarn properties |
| expand | 1 MiB per value, 8 MiB per unit, 64 MiB per run | Maven properties, Gradle templates (`limit.expand`) |
| time | 2 s per parse (root watchdog), `assemble_timeout` per ecosystem | `limits.Run`; assemblers poll `ctx` per unit |
| fetches | 64 `PomSource` calls per assembly | Maven (`maxFetches`) |
| structural | parent chain 32, BOM import depth 8, include depth 8, workspace patterns 128, group include depth 16, TOML key path 32 segments, 10,000 direct keys per TOML table, diagnostics per code 8 in a Maven or Gradle unit | typed diagnostics, never a crash |
| **cost** | every format parses and assembles in time linear in its size: the scale test of 12.3 holds each of 18 shapes at 512 KiB to 2 MiB to under 3 s, the nightly job runs the full 8 MiB (E-A8). No parser may call a function that is linear in the whole file once per key, token or element | `TestInputsAtScaleStayLinear` |

#### 4.1.8 Corpus tiers and licences

Tier 1 (committed under `testdata/`): synthetic cases written for this unit (original work, MIT) and **real** files whose licence is Apache-2.0 and whose headers are intact (apache/maven, apache/commons-lang, square/okhttp, ktorio/ktor, android/nowinandroid and eight POMs from Maven Central; see `oracle/SOURCES.tsv`). Tier 2 (never committed, fetched by blob id with `fetch-oracle.sh` and verified): large real lockfiles and manifests whose licences are not Apache-2.0 or whose size does not belong in a repository (pnpm lockfile of vercel/ai 1.9 MB, npm/cli, Yarn Berry, React, Bun, uv, Poetry, Flask, ripgrep, tokio, kubernetes). The oracle numbers (locked counts) are compared against independent counts (E-A2). The licence column of `SOURCES.tsv` is the one the GitHub licence API reports, re-read from the repository's `LICENSE` text where the API answers `NOASSERTION` (vercel/ai, npm/cli); Bun's file is unverified and therefore tier 2 only.

#### 4.1.9 The invariants the harness holds every parser and assembler to

`parsertest.CheckParse` (parsers) and `CheckTree` (units) check, on every golden and on every fuzz input:

1. no panic; 2. bounded time and memory (the fuzz limits are 64 KiB per file, 200,000 nodes, 0.5 s); 3. deterministic (deep equality of two runs); 4. every identity is valid, normalised (a fixed point of the spec's `Normalize`) and survives a purl round trip (`purl.Build` then `purl.Parse` gives the same namespace and name); 5. every location is inside the file (`1..lines+1`), every path relative and clean; 6. every text field is valid UTF-8 without C0 controls (tab excepted), DEL or bidi controls; 7. every diagnostic code is declared in the catalogue or is one of the root's; 8. path independence: the same bytes under two different paths give the same output.

For assemblers `CheckTree` adds: no unit keeps `SourceProject`; every unit directory is clean and inside the repository; `DependsOn` indices are in range; the assembler did not modify its input (`assemble.mutated-input`); two runs give equal units; unit ids are unique. The fuzz harness (`FuzzTree`) feeds a one-file tree of every seed through `RunTree` twice so the assembler is fuzzed too.

---
### 4.2 Go modules (`ecosystems/golang`)

#### 4.2.1 Files and match

| Parser id | Matches (base name, case-sensitive) | Kind | Not matched on purpose |
|---|---|---|---|
| `golang/mod` | `go.mod`, anywhere except below a `vendor` or `node_modules` directory | manifest | `go.sum` (hashes of a module graph this unit does not build), `vendor/modules.txt`, `go.mod` copies under `vendor/` |
| `golang/work` | `go.work`, same exclusion | workspace | `go.work.sum` |

#### 4.2.2 Grammar handled

The syntax of `go.mod` and `go.work` is read by `golang.org/x/mod/modfile` v0.42.0 (the reference implementation, a linear lexer). `modfile.Parse` is tried first; when it fails (a directive of a newer Go than the library knows, or a malformed statement) `modfile.ParseLax` supplies what can still be read and the file gets `golang.strict-failed` (warning) with the strict error's line. Handled directives: `module`, `go`, `toolchain`, `require` (block and single form, `// indirect` comment), `replace` (module and version-specific, local path or module target), `tool` (Go 1.24), and in `go.work`: `go`, `toolchain`, `use` (block and single), `replace`. `exclude`, `retract`, `godebug` and `ignore` are parsed by the library and deliberately ignored (they do not change which versions the module graph selects from the point of view of a dependency list).

#### 4.2.3 Grammar not handled (and why)

- Module graph pruning and minimal version selection: the `require` list of a `go.mod` at go 1.17 or later already lists every module that supplies a package to the main module's build (including indirect ones), so the file is its own lock (V-1). For a pre-1.17 `go.mod` the indirect set is incomplete; the unit says so through `Toolchain["go"]` and nothing else (no heuristic fill).
- `go.sum` is not read. `vendor/modules.txt` is not read. Neither adds an edge that the `require` list lacks.
- `replace` is recorded with its target but never *applied* to the version: the replaced dependency keeps the required version and gets `Source` `path` (local) or `registry` with the replacement `<path>@<ver>` as ref. Version selection is Go's job.
- Build constraints, `//go:build` tags and `internal` packages: no source file is read.

#### 4.2.4 Mapping to the normalised model

| Source | Model |
|---|---|
| `module <path>` | `Provides[0]` `Artifact{ID: componentOf(path), Display: path, Kind: library}`; no version (Go modules have none in the file) |
| `require m v` not `// indirect` | `Dependency{Target m, Requirement v, Version v, Resolution pinned, Scope runtime, Group "require", Line}` and a `LockedComponent{Relationship direct, Scope runtime}` |
| `require m v // indirect` | `LockedComponent{Relationship transitive, Scope runtime}` only (no `Dependency`; the declared list is the direct set) |
| `tool pkg` | the owning module (longest required module path that is a prefix of `pkg`) gets a second `Dependency{Scope build, Group "tool"}`; none found gives `golang.tool-unresolved` (info) |
| `replace old[@v] => new[@v]` | `Properties["replace:old[@v]"] = "new[@v]"` and the matching `Dependency` gets `Source` path (relative to the `go.mod`, cleaned) or registry ref; an absolute path is not followed (`golang.replace-absolute`) |
| `go 1.22`, `toolchain go1.22.3` | `Toolchain["go"]`, `Toolchain["toolchain"]`; a missing `go` directive records `go` = `1.16` and `golang.go-directive-missing` (info), as the go command does |
| `go.work` `use ./a` | `Members` (relative to the `go.work`; absolute or malformed paths give `golang.use-invalid`) |
| `go.work` `replace` | `Properties["replace:..."]`, local targets as `path:<rel to go.work>` |

`Parsed.Licenses` is always empty: `go.mod` declares none; the licence of a Go module lives in its repository and is out of this unit's reach (provider work, unit 13 and later).

#### 4.2.5 Identity and purl

Module path `github.com/Azure/azure-sdk-for-go` becomes namespace `github.com/azure` and name `azure-sdk-for-go` (both lower-cased, the purl-spec rule for `golang`), and the original case is kept in `Artifact.Display`, `Dependency.TargetDisplay()` and `LockedComponent.Display` (RR-A6). A major-version suffix stays part of the name (`github.com/foo/bar/v2` has name `v2` and namespace `github.com/foo/bar`): this is how the purl spec defines it and it keeps `bar` and `bar/v2` distinguishable in the graph; the display carries the full path. Single-segment paths (`std`, `example`) have an empty namespace. Empty and whitespace-only segments are dropped before the split (F-A1), otherwise `//0` yields a namespace that a purl cannot round-trip.

#### 4.2.6 Confidence

`ConfExact` throughout. The only inexact data is `Version` for a dependency whose `replace` points at a local path: the version is the one required, but the code is not (the dependency is marked `SourcePath` so the graph does not match it against the registry).

#### 4.2.7 Assembly

```
Assemble(files, parsed):
    mods := parsed with ParserID golang/mod, sorted by path; works := golang/work
    moduleDirs[dir] = module; producer[ComponentID] = dir of the first go.mod that Provides it
    for each go.work w (sorted):
        w.replaces := Properties with every "path:<rel>" joined to the directory of w (dropped when it escapes)
        for each use directory u: join(dir(w), u); escape -> golang.use-missing; no go.mod there -> golang.use-missing
        first workspace to list a directory owns it (memberOf[dir])
    for each go.mod m (sorted):
        unit := {Ecosystem golang, Dir, Files [go.mod (+ go.work)], Provides, Locked, Toolchain (+ "workspace", "workspace.go"), Diagnostics}
        for each Dependency d (cloned):
            if member of a workspace and d is not already a local path:
                 go.work replace for (display, version) or display beats go.mod's replace
                 else if another workspace member provides d.Target: d becomes SourcePath = that member's directory (workspace use overrides the registry)
        diagnostics of the workspace for this directory appended
    return nil when no go.mod was placed (a go.work alone: the root's default assembly lists it)
```

The unit is **per `go.mod`**, not per workspace, because each module has its own `require` list and its own artifact; a `go.work` only adds overlays and a membership fact (`Toolchain["workspace"]`). Local `replace` or workspace edges become `SourcePath` dependencies, which the core's graph builder (doc 11 4.8) turns into intra-repository edges (channel B) without a registry lookup.

#### 4.2.8 Limits and hardening

`modfile` is linear and bounded by the 1 MiB manifest limit; one `g.Node()` per requirement stops a file with 2,000,000 `require` lines at `limit.nodes`. Every module path, version and replacement target passes `CleanText` (512, 128, 512 bytes). `TestBombs` covers: 1.2 MB comment, NUL byte, truncated block, 20,000 requirements, a go.work `use ../../../etc` plus an absolute path, an absolute replace target and a replace escaping the repository (`assemble.path-escape` from the runner). The fuzz corpus (3 seeds) is in section 12 and appendix E.

#### 4.2.9 Diagnostics (8 codes, `Spec().Diagnostics`)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `golang.syntax` | error | parser | neither strict nor lax parsing reads the file; the line comes from `modfile.ErrorList` |
| `golang.strict-failed` | warning | parser | strict parsing failed, lax succeeded; replace, exclude and tool were ignored |
| `golang.go-directive-missing` | info | parser | no `go` line (go 1.16 assumed) |
| `golang.module-missing` | warning | parser | no `module` line; the file still yields its requirements |
| `golang.tool-unresolved` | info | parser | a `tool` path no required module prefixes |
| `golang.replace-absolute` | warning | parser | a replace to `/abs/path` |
| `golang.use-invalid` | warning | parser | `use` absolute or malformed |
| `golang.use-missing` | warning | assembler | `use` names a directory without a fetched `go.mod`, or outside the repository |

#### 4.2.10 Corpus

| Case | Kind | Source and licence | What it proves | Expected (golden) |
|---|---|---|---|---|
| `testdata/basic` | tier 1 synthetic | written for this unit (MIT) | require block, indirect, replace to path and to module, tool directive, case-folded identity | 6 deps, 7 locked, `golang.tool-unresolved`, `assemble.path-escape` from a replace that leaves the repository |
| `testdata/nogo` | tier 1 synthetic | idem | go 1.16 default | `golang.go-directive-missing` |
| `testdata/syntax-error` | tier 1 synthetic | idem | unparseable file keeps going | `golang.syntax`, unit with no deps |
| `testdata/workspace` | tier 1 synthetic tree (`go.work`, `a`, `b`, `tools`) | idem | `use`, go.work replace beats go.mod, sibling module becomes a path dependency | 3 units, 3 deps |
| `t/k8s/go.mod`, `go.work` | tier 2 | kubernetes/kubernetes, Apache-2.0, blobs in `SOURCES.tsv` | real scale and replace/`use` practice | 114 direct requirements, 198 locked, matching an independent `awk` count of the same file; 0.8 ms |
| `FuzzParsers` seeds | regression | found by the fuzzer | module paths `/0/00`, ` /0/0`, `/0` | see appendix E |

---

### 4.3 npm, Yarn, pnpm and Bun (`ecosystems/npm`)

#### 4.3.1 Files and match

| Parser id | Matches (base name) | Kind | Notes |
|---|---|---|---|
| `npm/package-json` | `package.json` | manifest | not below `node_modules` |
| `npm/package-lock` | `package-lock.json`, `npm-shrinkwrap.json` | lockfile | lockfileVersion 1, 2, 3 |
| `npm/pnpm-lock` | `pnpm-lock.yaml` | lockfile | lockfileVersion 6.0 to 9.x; one or two YAML documents |
| `npm/pnpm-workspace` | `pnpm-workspace.yaml` | workspace | `packages`, `catalog`, `catalogs`, `overrides` |
| `npm/yarn-lock` | `yarn.lock` | lockfile | Yarn Classic v1 and Berry (2+) |
| `npm/bun-lock` | `bun.lock` | lockfile | text format (JSONC) of Bun 1.2 and later |
| `npm/bun-lockb` | `bun.lockb` | lockfile | matched so that the file is *seen*; the runner rejects its NUL bytes before the parser runs, and the assembler reports `npm.bun-binary-lockfile` from `files.Has` |

`.pnp.cjs`, `.yarnrc.yml`, `.npmrc`, `lerna.json`, `nx.json`, `turbo.json` are not read (workspaces are defined by `package.json` and `pnpm-workspace.yaml` only; `.npmrc` can hold credentials and is never fetched by acquisition).

#### 4.3.2 Grammar handled

- **package.json**: own JSON reader `jsonx.go` (about 180 lines; strict RFC 8259 plus nothing; preserves key order and line numbers; depth and node budgets; duplicate keys keep the first). Sections `dependencies`, `devDependencies`, `peerDependencies` (+ `peerDependenciesMeta.<n>.optional`), `optionalDependencies`; `workspaces` as array or `{packages: []}` (Yarn classic) ; `catalog` and `catalogs` (Bun, Yarn Berry 4); `overrides`, `resolutions`, `pnpm.overrides` (recorded, never applied); `engines`, `packageManager` (`pnpm@9.1.0+sha512...`), `type`, `license` (string, `{type}` or the deprecated `licenses[]`), `private`, `name`, `version`. Specifier classes (`classify`): semver range or tag; exact version (`1.2.3`, `=1.2.3`, `v1.2.3`); `npm:` alias (real package becomes the target, local name kept in `Properties["alias:<spec>"]`); `workspace:` (resolved by the assembler); `file:`, `link:`, `portal:` and bare relative paths; git (`git+ssh://`, `git://`, `github:`, `gitlab:`, `bitbucket:`, `gist:`, scp-like `git@host:path`, `owner/repo` shorthand); `http(s)://` tarball.
- **package-lock.json** v1 (`dependencies` tree), v2 and v3 (flat `packages` keyed by `node_modules` path, workspace members and `link: true` entries): resolution by Node module lookup over the flattened tree (`resolve`, walk up `node_modules` parents), `dev`, `optional`, `devOptional`, `peer` flags, `integrity`, `license` per entry, aliases (`name` field of an entry), `inBundle` ignored.
- **pnpm-lock.yaml** v6, v7, v9 (`importers`, `packages`, `snapshots`, `catalogs`, `overrides`, `time` ignored), keys `/name@1.2.3(peer@1)` and `name@1.2.3(...)`, aliases (`real@1.2.3`), `link:` and `file:` importers; a leading environment document (`importers`-less) before the project document is detected (`npm.yaml-multi-document`, V-5).
- **pnpm-workspace.yaml**: `packages` (globs with `!` negation), `catalog`, `catalogs`, `overrides`.
- **yarn.lock** v1 (own line parser, comma-joined descriptors, quoted keys, `dependencies` and `optionalDependencies` indented blocks) and Berry (YAML-shaped, parsed by `go.yaml.in/yaml/v3` through the bounded walker: `__metadata`, `resolution: "name@npm:1.2.3"`, `linkType`, `languageName: unknown` for patches and workspaces, `checksum`).
- **bun.lock**: JSONC (comments and trailing commas stripped by `stripJSONC`, a linear pass) with `lockfileVersion`, `workspaces` (`""` is the root), `packages` (`[ "name@version", registry, { deps }, "integrity" ]` tuples), `overrides`, `catalogs`.

#### 4.3.3 Grammar not handled

- pnpm lockfile v5 and older (`lockfileVersion: 5.x`): `npm.lockfile-version-unsupported` and a best-effort partial read. The pnpm lockfile page of pnpm.io could not be fetched on 2026-10-14 (404), so the supported range is stated from the code and the corpus (V-12).
- Yarn Berry plug-and-play resolution (`.pnp.cjs`), constraints, `resolutions` with patches (recorded only) and the **transitive optional edges** of Yarn v1 (which `yarn.lock` does not mark): optional-only transitive packages are classified `runtime`.
- `bun.lockb` content (binary, undocumented, version dependent; diagnosed only).
- Dependency aliases through `overrides` or `resolutions`, `bundledDependencies`, `dependenciesMeta`, `publishConfig` and `exports`: not dependency edges.
- npm `workspaces` with `nohoist`, Lerna and Nx project graphs (`project.json`) and Rush (`rush.json`): file-defined workspaces only.
- Evaluating `${VAR}` references or `.npmrc` registry scopes.

#### 4.3.4 Mapping to the normalised model

| Source | Model |
|---|---|
| `name`, `version`, `license` | `Provides[0]` (`Kind` library), `Licenses` |
| `dependencies` | `Dependency{Scope runtime, Group "dependencies"}` |
| `devDependencies` | `Scope dev` |
| `peerDependencies` | `Scope peer`; `Optional` when `peerDependenciesMeta.<n>.optional` |
| `optionalDependencies` | `Scope optional, Optional true` |
| `workspace:*` and friends | `SourceProject` in the parser, resolved by the assembler to `SourcePath` (member directory) or demoted with `npm.workspace-ref-unresolved` |
| `catalog:` / `catalog:<name>` | resolved by the assembler through pnpm-workspace.yaml, then package.json (`catalog`, `workspaces.catalog`), then the lockfile's `catalogs` fallback; the value becomes `Requirement` with `Resolution managed`; a missing entry gives `npm.catalog-missing`, an unfetched catalog file `npm.catalog-unavailable` |
| lock entry | `LockedComponent{ID, Display, Version, Integrity, Relationship, Scope, DependsOn}`; package-lock flags give scope (`dev`/`devOptional` dev, `optional` optional, `peer` peer, else runtime); pnpm and Bun importer sections give `direct`, reachability gives the rest; Yarn v1 and package-lock v1 carry no roots, so `finalizeRoots` classifies by reachability from the declared dependencies of every unit of the workspace (best scope wins: runtime, peer, optional, dev) |
| lock integrity | `Integrity` as written in the file (`sha512-...`, Yarn `checksum` text), cleaned and capped |
| `engines`, `packageManager`, `type` | `Toolchain` (`engines.node`, `tool`, `toolVersion`, `module`; `language` becomes `typescript` when `typescript` is a dependency) |
| `workspaces`, `pnpm-workspace.yaml packages` | `Members` (globs relative to the file), at most 128 patterns |

#### 4.3.5 Identity and purl

Scope `@babel` is the namespace without `@`; names are case-sensitive as written (older mixed-case packages exist; the npm registry now forbids them for new names). `pkg:npm/%40babel/core@7.24.0` (the `%40` encoding is the purl library's). An `npm:` alias dependency targets the **real** package (`"react17": "npm:react@17"` targets `react`, with `Properties["alias:npm:react@17"]="react17"` so the lock join can find the entry under the alias name). Names that cannot be an identity (empty, whitespace) are dropped at the source (F-A5).

#### 4.3.6 Confidence

`ConfExact`. A dependency whose specifier needs resolution that this unit cannot perform (a dist-tag, `latest`, an unresolved `catalog:`) has `Version` empty and `Resolution declared`; once the lock join supplies a version it becomes `locked`.

#### 4.3.7 Assembly

```
Assemble(files, parsed):
    group Parsed by directory: package.json, pnpm-workspace.yaml, locks keyed by parser id (package-lock and npm-shrinkwrap separate)
    project directories := those with a package.json (a directory with only a lockfile or workspace file is not a project: it falls to default assembly if nothing at all is placed)
    for each project directory d (sorted) with workspace patterns (package.json + pnpm-workspace.yaml, <= 128, "!" negations):
         members := project directories below d matching a positive pattern and no negative pattern  (repopath.Match against the relative directory)
         explicit pattern without a fetched package.json -> npm.workspace-member-unfetched; glob matching nothing -> npm.workspace-glob-unmatched
         outermost root owns a member (nested workspace roots do not steal members)
    byName := first project directory that Provides each package id
    per project directory (one unit each):
         copy toolchain (+ tool/toolVersion from the workspace root), provides, licences, diagnostics
         dependencies: prepareDep (catalog: resolution, workspace: resolution, npm: alias) then duplicates (target, scope, requirement) collapsed
         lock: chosen in the workspace root directory: packageManager tool first (pnpm, yarn, bun, npm), then priority npm-shrinkwrap, package-lock, pnpm, yarn, bun; more than one -> npm.multiple-lockfiles (info)
               the lock is attached to the ROOT unit only (Files, Locked, Diagnostics); every unit of the workspace joins its own direct dependencies against it (joinLock):
                   per-importer locked version (lock:<dir>:<name>), else the descriptor map (desc:<name>@<range>), else the single locked version, else the highest version satisfying the range (internal/version, npm semver)
                   dependency satisfied by a sibling member whose version satisfies the range -> SourcePath to the member directory
                   no entry -> npm.lock-missing-dep
         bun.lockb present next to the root: npm.bun-binary-lockfile
    finalizeRoots: classify Yarn v1 / package-lock v1 locked components by reachability (runtime > peer > optional > dev)
    return nil when no project directory exists
```

Why the lock sits on the root unit: a workspace has **one** lock for every member, and attaching it to each member would multiply the locked set by the member count (a 1,000-package lock times 40 members). The members keep the resolved `Version` of their own dependencies (`Resolution locked`), and the unit graph links them to the root's locked set through `Unit.Members`.

#### 4.3.8 Limits and hardening

| Threat | Mechanism | Test |
|---|---|---|
| deep JSON (`[[[[...`), huge arrays | `jsonx` uses an explicit depth budget and `g.Node()` per token; a deeply nested document gives `limit.depth` inside a 1 s test budget, a many-key one `limit.nodes` | `TestAdversarial/json-depth-bomb`, `json-node-budget` |
| YAML alias bomb (billion laughs) in `pnpm-lock.yaml` or Berry `yarn.lock` | `yamlx.go` walks the `yaml.Node` tree itself, charges the guard for every visited entry, dereferences aliases to depth 4 and refuses documents past 4; the decoder is never asked to build a `map[string]any` (the amplification point of yaml.v3) | `TestAdversarial/yaml-alias-bomb-lock`, `yaml-alias-bomb-workspace`, `yaml-depth-bomb` (2 s budget each) |
| lock cycles (`a -> b -> a`) and deep chains | reachability uses an explicit worklist and a visited set; the lock resolver has a step budget for `link` chains | `TestLockCycles` (mutual and self links, a dependency cycle `a -> b -> a`, a missing target) |
| hostile text (RLO, ZWSP, controls in names and ranges) | every string through `CleanText` | `TestHostileText` |
| secrets in URLs and `_authToken` | `redact.URL`; `.npmrc` never fetched; `resolved` URLs of lock entries are dropped, not stored | `TestSecretsNeverLeak` |
| workspace glob explosion | 128 patterns, `repopath.Match` is polynomial (RR-A8), members matched against the fetched project set only | `TestWorkspaceGlobBounds` (a pattern with 14 `**/` segments over a deep tree took 6 s with an exponential matcher; F-A12) |
| Yarn v1 descriptor collisions and map-order nondeterminism | descriptors emitted from a sorted slice, first wins (F-A7) | seed |

#### 4.3.9 Diagnostics (17 codes)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `npm.syntax` | error | parser | JSON, JSONC or YAML cannot be parsed (a limit hit becomes its own `limit.*` code) |
| `npm.no-name` | info | parser | `package.json` without `name` |
| `npm.type-mismatch` | warning | parser | a section or value of the wrong type is skipped |
| `npm.lockfile-version-unsupported` | warning | parser | unknown lock format version; best effort |
| `npm.member-invalid` | warning | parser | workspace pattern absolute or malformed |
| `npm.lock-entry-invalid` | warning | parser | malformed lock entry skipped |
| `npm.lock-missing-dep` | warning | assembler | a declared dependency has no entry in the lock (stale lock) |
| `npm.bun-binary-lockfile` | warning | assembler | `bun.lockb` next to the root manifest |
| `npm.multiple-lockfiles` | info | assembler | several lock files in one workspace root |
| `npm.workspace-glob-unmatched` | info | assembler | pattern matched no project |
| `npm.workspace-member-unfetched` | warning | assembler | explicit member or lock importer without a fetched `package.json` |
| `npm.workspace-pattern-limit` | warning | assembler | more than 128 patterns |
| `npm.catalog-missing` | warning | assembler | `catalog:` entry absent from the catalogs found |
| `npm.catalog-unavailable` | info | assembler | no catalog source fetched |
| `npm.workspace-ref-unresolved` | warning | assembler | `workspace:` names no member |
| `npm.yaml-multi-document` | info | parser | pnpm environment document before the project document |
| `npm.yarn-version-unsupported` | warning | parser | `yarn.lock` is neither v1 nor Berry |

#### 4.3.10 Corpus

| Case | Kind | Source, licence | What it proves | Expected |
|---|---|---|---|---|
| `package-json` | tier 1 synthetic (13 KB golden) | this unit (MIT) | every specifier class, peers, optional peers, overrides, catalog placeholders, `type`, engines | 17 deps; `npm.type-mismatch`, `npm.workspace-glob-unmatched`, `npm.workspace-ref-unresolved`, `npm.catalog-unavailable` x2, `assemble.path-escape` |
| `npm-v1` | tier 1 | this unit | package-lock v1 nested `dependencies` tree and flags | 3 deps, 6 locked, reachability scopes |
| `npm-v3-workspaces` | tier 1 | idem | flat `packages`, member `link` entries, per-importer versions | 3 units, 6 locked on the root unit |
| `pnpm-catalog` | tier 1 | idem | `catalog:` and `catalog:<name>`, pnpm-workspace.yaml, importers | 3 units, 12 locked, `npm.catalog-missing`, `npm.lock-missing-dep` |
| `pnpm-two-doc` | tier 1 | idem | environment document before the project document | `npm.yaml-multi-document` |
| `yarn-berry`, `yarn1-workspaces` | tier 1 | idem | Berry resolution strings, patches, workspace lines; v1 descriptors, classification by reachability | 7 and 8 locked |
| `bun-workspaces`, `bun-binary` | tier 1 | idem | JSONC tuples, `workspaces`; binary lock diagnosed | 3 units; `npm.bun-binary-lockfile` and `encoding.unsupported` |
| `multi-lock` | tier 1 | idem | package-lock and yarn.lock together, `packageManager` decides | `npm.multiple-lockfiles` |
| `vuejs/core` | tier 2 | MIT | real pnpm 9, catalog, workspace | 634 locked (independent count 634), 8.3 ms |
| `vercel/ai` | tier 2 | Apache-2.0 (LICENSE text) | largest corpus file, 1.98 MB two-document pnpm | 4,061 locked, 60.8 ms (pnpm parsing allocates 22.1 bytes per input byte, measured on vue/core, E-A5) |
| `npm/cli` | tier 2 | Artistic-2.0 | package-lock v3 with workspaces | 993 locked (993), 7.8 ms |
| `yarnpkg/berry` | tier 2 | BSD-2-Clause | Berry 810 KB | 2,050 locked (2,050), 24.4 ms |
| `facebook/react` | tier 2 | MIT | Yarn v1 workspace root plus its lock (consistent pair) | 2,388 locked (2,388), 15.4 ms; the stale 2018 lock of the same repo gave 107 `npm.lock-missing-dep`, the expected behaviour |
| `oven-sh/bun` | tier 2 | Bun licence (unverified) | `bun.lock` | 42 locked, 0.2 ms |

---

### 4.4 Python (`ecosystems/python`, ecosystem id `pypi`)

#### 4.4.1 Files and match

| Parser id | Matches | Kind |
|---|---|---|
| `pypi/pyproject` | `pyproject.toml` | manifest (a `[tool.uv.workspace]` root without `[project]` is workspace) |
| `pypi/requirements` | `requirements*.txt`, `requirements*.in` (regex `^(requirements)([-_.][A-Za-z0-9._-]*)?\.(txt\|in)$`), any `*.txt` below a directory named `requirements` or `requirements.d`; `pip-compile` or `uv pip compile` output is recognised by its header and becomes a lockfile by content | manifest |
| `pypi/constraints` | `constraints*.txt`, `constraints*.in` | catalog |
| `pypi/poetry-lock` | `poetry.lock` | lockfile |
| `pypi/uv-lock` | `uv.lock` | lockfile |
| `pypi/pylock` | `pylock.toml`, `pylock.<name>.toml` (PEP 751, regex `^pylock\.([^.]+)\.toml$`) | lockfile |
| `pypi/pipfile` | `Pipfile` | manifest |
| `pypi/pipfile-lock` | `Pipfile.lock` | lockfile |
| `pypi/setup-cfg` | `setup.cfg` | manifest |

Excluded path segments (second defence behind `acquisition.exclude_segments`): `node_modules`, `site-packages`, `.venv`, `venv`, `.tox`, `.nox`, `__pypackages__`, `vendor`. **Not matched:** `setup.py` (executable code), `pdm.lock`, `environment.yml` (conda), `tox.ini`.

#### 4.4.2 Grammar handled

- **pyproject.toml** (TOML 1.0 through `tomlx`): `[build-system]` (`requires` as `build` scope, `build-backend`), `[project]` (PEP 621: `name`, `version`, `dynamic`, `requires-python`, `license` as string or `{text}`, `dependencies`, `optional-dependencies`), `[dependency-groups]` (PEP 735 with `include-group` recursion, cycle and missing-group diagnostics, normalised names), `[tool.poetry]` (Poetry 1 `dependencies`, `dev-dependencies`, `group.<n>.dependencies`; Poetry 2 merges with PEP 621: the PEP 621 list wins, Poetry entries only enrich git, path and url sources or add what the project table lacks), `[tool.uv]` (`dev-dependencies`, `sources` with `workspace`, `path`, `git`, `url`, `index`; `workspace.members` and `exclude`), `[tool.pdm]` (`dev-dependencies`). PEP 508 strings are read by the unit's own scanner `ParseRequirement` (linear, no regular expression with backtracking): name, extras, version specifier (comma list), URL (`name @ url`) and environment marker (kept raw as `Condition`).
- **requirements*.txt**: logical lines (backslash continuation, odd/even trailing backslash rule, 64 KiB per logical line), comments, `-r`/`--requirement` and `-c`/`--constraint` includes, `-e path|url`, `--hash=` options (the first hash becomes `Integrity` of a lock-like `pip-compile` file), `--index-url`, `--extra-index-url`, `--find-links`, `--trusted-host`, `--pre`, `--no-binary` (ignored with `python.option-ignored`), VCS and URL requirements, `${VAR}` references (kept unexpanded with `python.env-var`), `# via` comment blocks of `pip-compile` (parents become `DependsOn`).
- **poetry.lock** (TOML `[[package]]`, `category` or `groups`, `source`, `files[].hash`, `dependencies` edges) ; **uv.lock** (`[[package]]`, `source = { registry | git | path | editable | virtual | directory }`, `dependencies`, `optional-dependencies`, `dev-dependencies`, `sdist`/`wheels` hashes, `resolution-markers` ignored, root packages `root:<i>`); **pylock.toml** (PEP 751 `lock-version` "1.0", `created-by`, `[[packages]]` with `name`, `version`, `vcs`, `directory`, `archive`, `sdist`, `wheels`, `dependencies`, `marker`); **Pipfile** (`[packages]`, `[dev-packages]`, `[requires]`, `[[source]]` ignored) and **Pipfile.lock** (JSON `default`, `develop`, `_meta`); **setup.cfg** (INI: `[metadata]` name, version, license; `[options]` `install_requires`, `python_requires`, `setup_requires`, `tests_require`; `[options.extras_require]`).

#### 4.4.3 Grammar not handled

- `setup.py` and `setup.cfg` values computed by `attr:` or `file:`; `pdm.lock`; conda and Pixi environments; `tox.ini` and `noxfile.py` environments.
- Environment-marker **evaluation** and extras resolution: markers are stored raw; `--hash` verification; wheel tag selection; index resolution (`--index-url` is never copied, V-14).
- `[tool.uv.sources]` conflicts across indexes, `override-dependencies`, `constraint-dependencies` of `tool.uv` (recorded only through `python.uv-source-multiple`).
- Requirements files included through a URL or an absolute path (`python.include-outside-repo`), or by a path that is not among the fetched candidate files (`python.include-not-candidate`: pip would read them, this unit has no I/O).

#### 4.4.4 Mapping to the normalised model

| Source | Scope, group | Notes |
|---|---|---|
| `[project].dependencies` | runtime, `dependencies` | marker -> `Condition`; `==1.2` or `===1.2` -> `Version`, `ResPinned` |
| `optional-dependencies.<extra>` | optional, `<extra>`, `Optional true` | |
| `dependency-groups.<g>` | `test` for groups `test`, `tests`, `testing`, `pytest`, `unittest`, `coverage`; otherwise `dev` | `include-group` expanded in place; cycles and missing groups diagnosed |
| `build-system.requires` | build, `build-system.requires` | |
| `tool.poetry.dependencies` / `group.<g>` | runtime / by group name rule | `python = "^3.10"` goes to `Toolchain["python"]`, not a dependency |
| `tool.uv.sources.<n>` | rewrites the matching dependency: `workspace = true` -> `SourceProject` (resolved by the assembler), `path`, `git` (+ `rev`, `tag`, `branch`), `url` | |
| `requirements.txt` (plain) | scope by **file name** (assembler `rescope`): `requirements.txt` and `base|main|prod|production|common|runtime|all` runtime, `test*` test, `dev*`, `lint`, `docs`, `typing`, `mypy`, ... dev; unknown suffix runtime, flagged heuristic | the parser cannot read its own path (invariant 8) |
| constraints file | `Kind catalog`; the assembler pins matching unpinned dependencies (`ResManaged`) and never adds dependencies | |
| lock entries | `LockedComponent` with `Version`, `Integrity` (`sha256:`, `git:<rev>`), `Relationship` (`direct` for names the root declares, others `transitive`), `Scope` from Poetry `groups` or `category`, uv dependency groups, Pipfile `develop`, requirements file name | |
| `requires-python`, `build-backend`, tool | `Toolchain["python"]`, `["build_backend"]`, `["tool"]` (`poetry`, `uv`, `pdm`) | |

#### 4.4.5 Identity and purl

PEP 503 normal form: lower case, runs of `-`, `_`, `.` collapsed to `-` (`Typing_Extensions` and `typing.extensions` are the same component, `typing-extensions`); this is stricter than the purl-spec `pypi` rule (lower case and `_` to `-`, V-15) and is the root's rule because `packageurl-go` does not apply PEP 503; the name as written is `TargetDisplay`. Namespace is empty. purl type `pypi`, version system `pypi` (PEP 440 ordering is `internal/version`'s). Names with control characters are dropped at the parser (`"\x01"`, bidi controls, F-A9).

#### 4.4.6 Confidence

`ConfExact` for structured files. `ConfHeuristic` when the scope came from a requirements **file name** pattern that is not one of the exact names `requirements.txt`, `base`, `main`, ... (so `requirements-ci.txt` is flagged), and for dependencies of a file whose lock disagrees (`python.lock-stale`).

#### 4.4.7 Assembly

```
Assemble(files, parsed):
    group parsed by project directory (a requirements file below requirements/ or requirements.d/ belongs to the parent directory)
    project := pyproject with provides, dependencies, a tool, or members; or setup.cfg; or Pipfile; or any requirements file
    per project directory:
        unit.Dependencies from the pyproject (PEP 621, groups, poetry, uv, pdm), setup.cfg, Pipfile
        requirements files: parsed once each; includes (-r) resolved by repopath.Join among the fetched candidate files (depth <= 8, cycle detection), not-candidate -> python.include-not-candidate;
             each file's dependencies rescoped by file name; a lock-like file (pip-compile header) becomes locked data instead of dependencies
        constraints (-c, or a constraints file in the directory): pin unpinned registry dependencies (ResManaged); never add
        locks in the same directory: uv.lock, poetry.lock, pylock*.toml, Pipfile.lock: join by normalised name (candidates by name, nearest version satisfying a pin), set Version and ResLocked, report python.lock-missing-dep (declared, not locked) and python.lock-stale (locked root disagrees)
    uv workspace: a pyproject with tool.uv.workspace members/exclude (repopath.Match on relative directories among the fetched pyprojects; a member without pyproject.toml -> python.workspace-member-missing)
         one uv.lock at the root is joined to every unit whose directory is a root package of the lock (root:<i>); the whole locked set is attached to the workspace root unit only
         tool.uv.sources workspace = true -> SourcePath to the member that provides the name (resolveWorkspaceSources works without a lock); unresolved -> demoted with python.workspace-source-unresolved
    return nil when no project directory exists
```

#### 4.4.8 Limits and hardening

TOML key paths above 32 segments are refused before decoding (`limit.depth`; the research 07 E3 case: 3.8 s for 40 KB of nesting, now a refusal in microseconds). Requirement strings: one logical line is capped at 64 KiB, a retained marker at 300 bytes and a specifier at 200; the scanner is linear (`TestBombs/marker-bomb`, `extras-bomb`, `line-continuations` with 200,000 continuation lines). Include depth 8 and a visited set stop `-r` cycles (`TestIncludeGraph`: two files including each other, a 40-deep chain, three kinds of escaping include). `--index-url` and `--extra-index-url` values never leave the parser (`TestSecrets`: user:password in an index URL, a `git+https://token@...` requirement). Lone `\r` in requirement files counts as whitespace so that line numbers stay in range (F-A8). The line of an array element of `pyproject.toml` (arrays have no index entry) comes from a one-pass table of quoted strings and a binary search (`locator`); the first version scanned up to 2,000 lines per element with `strings.Contains` and took 11.4 s for a 1 MiB single-line array (VF-12-03, `TestPyprojectLocatorIsLinear`; it also reported the wrong line for an element that is a substring of an earlier line, the new one does not).

#### 4.4.9 Diagnostics (18 codes)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `python.syntax` | error | parser | TOML, JSON or INI cannot be parsed |
| `python.requirement-invalid` | warning | parser | a PEP 508 string cannot be parsed |
| `python.no-name` | info | parser | no static project name |
| `python.dynamic-dependencies` | info | parser | `dynamic = ["dependencies"]` |
| `python.group-cycle` | warning | parser | `include-group` cycle |
| `python.group-missing` | warning | parser | `include-group` of an unknown group |
| `python.type-mismatch` | warning | parser | wrong value type skipped |
| `python.include-not-candidate` | warning | assembler | `-r`/`-c` file not among the fetched files |
| `python.include-cycle` | warning | assembler | include cycle or depth over 8 |
| `python.include-outside-repo` | warning | assembler | absolute, URL or escaping include |
| `python.option-ignored` | info | parser | pip option skipped |
| `python.unnamed-requirement` | info | parser | path or URL without a name |
| `python.env-var` | info | parser | `${VAR}` left unexpanded |
| `python.lock-stale` | warning | assembler | lock disagrees with declared requirements |
| `python.lock-missing-dep` | warning | assembler | declared dependency absent from the lock |
| `python.uv-source-multiple` | info | parser | several sources for one package; first used |
| `python.workspace-member-missing` | warning | assembler | uv member without `pyproject.toml` |
| `python.workspace-source-unresolved` | warning | assembler | `workspace = true` source without a member |

#### 4.4.10 Corpus

| Case | Kind | Source, licence | What it proves | Expected |
|---|---|---|---|---|
| `pep621` | tier 1 synthetic | this unit (MIT) | PEP 621, PEP 735 groups, cycles, invalid requirement | 11 deps; `group-cycle` x2, `group-missing`, `requirement-invalid` |
| `poetry1`, `poetry2` | tier 1 | idem | Poetry 1 groups and Poetry 2 PEP 621 merge, poetry.lock | 6 deps; 5 deps and 5 locked |
| `uv-workspace` | tier 1 tree | idem | members, exclude, `sources`, `uv.lock` root packages | 3 units, 6 deps, 5 locked on the root |
| `requirements` | tier 1 tree (6 files) | idem | includes, constraints, options, env var, VCS, hashes, pip-compile lock | 2 units, 12 deps, 3 locked, 5 info and warning codes |
| `pipenv`, `setupcfg` | tier 1 | idem | Pipfile and Pipfile.lock; setup.cfg | 4 deps and 5 locked; 4 deps |
| `pallets/flask` | tier 2 | BSD-3-Clause | PEP 621 + PEP 735 groups + uv.lock | 78 locked plus the root package as a unit (79 packages in the lock), 9.8 ms |
| `python-poetry/poetry` | tier 2 | MIT | Poetry 2 | 78 locked, 24.0 ms |
| `astral-sh/uv` | tier 2 | MIT OR Apache-2.0 | large uv workspace and lock | 95 locked, 14.0 ms |
| `django/django` | tier 2 | BSD-3-Clause | setuptools backend, `dynamic` dependencies | `python.dynamic-dependencies`, no invented dependencies |

---
### 4.5 Maven (`ecosystems/maven`)

The parser reads a POM as a **raw model** (one parser per file, no inheritance) and the assembler builds the **effective model** of every POM of a repository: parent chain, property interpolation, `dependencyManagement`, BOM import, profile activation from `.mvn/maven.config`, and an optional, privacy-guarded `PomSource` for parents and BOMs that live outside the organisation.

#### 4.5.1 Files and match

| Parser id | Matches | Kind | Excluded directories |
|---|---|---|---|
| `maven/pom` | `pom.xml` and `*.pom` | manifest | `target`, `node_modules`, `archetype-resources`, `src/it`, `src/test/resources` anywhere on the path |
| `maven/config` | `.mvn/maven.config` (at the root or below) | workspace | none |

`.mvn/jvm.config`, `.mvn/extensions.xml`, `.mvn/maven-build-cache-config.xml`, `settings.xml` (credentials) and `.mvn/wrapper/maven-wrapper.properties` are not read. `*.pom` is matched because Maven Central parents and BOMs are distributed under that name and repositories vendor them; it is also the form the `PomSource` returns, so one parser covers both.

#### 4.5.2 Grammar handled

A POM is untrusted XML from any repository of the organisation: a parser built on a default XML factory with no `disallow-doctype-decl`, no secure processing and no entity limits lets a POM carry an XXE or billion-laughs payload, a reader that matches element names without namespaces can be fed injected elements, and a reader that keeps only the root fields cannot see dependencies, inherited coordinates or `${revision}` (GR-34). The reader therefore:

- uses `encoding/xml` in **Strict** mode over the token stream (`Decoder.Token`, never `Unmarshal`), with a `CharsetReader` limited to `iso-8859-1` and `windows-1252` (A-26; the declared encoding of the prolog; every other charset is `encoding.unsupported`), no DTD processing and no entity expansion (the standard library knows only the five predefined entities and numeric references; any other entity is a syntax error, reported as `maven.syntax`), a depth budget, a token budget and a 1 MiB text cap per element. This is GR-34: "prefer JSON; if XML use `encoding/xml` (no entity resolution), size caps".
- is **namespace-aware by prefix**: the model namespace is `http://maven.apache.org/POM/<version>` (4.0.0 and 4.1.0); elements in any other namespace are ignored by lookups (`xnode.foreign`), so an `<x:dependencies xmlns:x="evil">` cannot inject a dependency, and a document without a namespace (the common hand-written case) is read normally.
- reads the whole model that matters for dependencies, not the root fields only: `parent` (+ `relativePath`), `groupId`, `artifactId`, `version`, `packaging`, `name`, `properties`, `modules` and `subprojects` (Maven 4), `licenses`, `dependencies` (with `type`, `classifier`, `scope`, `optional`; `exclusions` are not dependency edges and are skipped), `dependencyManagement` (including `import` scoped BOMs, type `pom`), `build/plugins`, `build/pluginManagement`, `build/extensions` (plugins and extensions are build-scope dependencies, plugin `dependencies` too; the `maven-compiler-plugin` `release|source|target` becomes `Toolchain["compiler.*"]`), and `profiles` with their `activation` (`activeByDefault`, `jdk`, `os`, `property`, `file`), `properties`, `dependencies`, `dependencyManagement`, `build`.
- `.mvn/maven.config`: one option per line or several per line; `-Dk=v` and `-D k=v` (properties), `-P a,b` and `--activate-profiles` (explicit profiles; `!x` and `-x` deactivate), everything else `maven.config-option-ignored`.

#### 4.5.3 Grammar not handled

- Profile activation by `jdk`, `os` and `file` (the unit has no build environment): such a profile is treated as inactive and diagnosed once (`maven.profile-unknown`); its dependencies are still emitted with `Condition "profile:<id>"` (the dependency exists in some builds), but its properties do not enter interpolation.
- `settings.xml` properties, `${env.X}`, `${user.home}`, `${basedir}` and other build-time values: reported `maven.property-unresolved` (info) and kept literally.
- Dependency **mediation** (nearest wins), transitive resolution of POMs out of the repository, `exclusions`, `systemPath` contents, version **range resolution** (`[1.0,2.0)` stays a requirement; only `[1.2]` is read as an exact pin), `LATEST` and `RELEASE`, `SNAPSHOT` resolution, dependency scopes of transitive edges.
- DTD declarations and custom entities (`maven.syntax`), XInclude, `xml:base`.
- Gradle metadata, `.module` files and `maven-metadata.xml`.
- `<repositories>`, `<distributionManagement>`, `<scm>` (not dependency data; URLs could carry credentials).

#### 4.5.4 Mapping to the normalised model

| Source | Model |
|---|---|
| `<groupId>`, `<artifactId>`, `<version>` | `Provides[0]` `Artifact{Display artifactId, Kind library|application, Version raw}`; groupId and version are inherited from the parent when absent (assembler) |
| `<packaging>` | `Properties["packaging"]` and `Artifact.Kind`: `maven-plugin` gives `plugin`; `war`, `ear`, `rar`, `app-client` give `application`; a `pom` that only has `dependencyManagement` (no modules, dependencies or build) gives `bom`; everything else `library` |
| `<dependency>` scope `compile`, `runtime` | `Scope runtime`; `test` -> `test`; `provided`, `system` -> `provided` (system adds `maven.system-scope`); missing scope: the managed scope, else `runtime`; a placeholder or unknown scope: `maven.scope-placeholder`, runtime |
| `<optional>true` | `Optional true` (also from management) |
| `<type>`, `<classifier>` | qualifiers `type=<t>` (not `jar`), `classifier=<c>`, sorted and joined with `&`, part of the purl and of the management key |
| `<version>` | `Requirement` after interpolation; `Version` and `ResPinned` when it is a plain soft version or `[x]`; from `dependencyManagement` or a BOM: `ResManaged`; none: `maven.version-unmanaged` (only when the chain and BOMs were fully resolvable) |
| `<parent>` | `Parsed.Parent` (declared, group `parent`) resolved by the assembler; never a dependency of the unit |
| `<dependencyManagement>` | not a dependency list: contributes management entries (version, scope, optional) to the chain |
| import-scoped BOM | `Parsed.Imports`; resolved among repository POMs, then through the `PomSource` |
| `<plugin>` | `Dependency{Scope build, Group "plugin"}` (version from `pluginManagement` when absent: group `plugin-managed` entries are the management table); plugin `<dependencies>`: group `plugin-dependency`; `<extension>`: group `extension` |
| `<profile>` | its dependencies carry `Condition "profile:<id>"` (`=active` when the profile is active by `maven.config` or `activeByDefault`); properties only for active profiles |
| `<modules>`, `<subprojects>` | `Members` (relative directories or POM file paths); the unit lists members that are fetched, `maven.module-missing` for those that are not |
| `<licenses><license><name>` | `Licenses` (declared names) |
| compiler plugin, Kotlin plugin | `Toolchain["compiler.release"]`, `["kotlin"]` (value `unknown` unless a version property is known), `Toolchain["language"]` kotlin when `kotlin-maven-plugin` or `kotlin-stdlib*` is present |

#### 4.5.5 Identity, purl and confidence

`pkg:maven/<groupId>/<artifactId>@<version>` with `classifier` and `type` qualifiers; nothing is folded (Maven coordinates are case-sensitive). Because `${...}` placeholders are legitimate in a raw model, the parser accepts them in the coordinate; `coordOK` rejects only `/`, `\`, `:` and whitespace, which cannot round-trip through a purl (F-A2). `ConfExact` for everything (see 3.1: the first revision promised `ConfHeuristic` for an unresolved or `maven.config`-supplied value, which no code did; an unresolved value has an empty `Version` and a `maven.property-unresolved` diagnostic, and a value from `maven.config` is shown as the build the file declares).

#### 4.5.6 Assembly: effective model and the PomSource privacy rule

```
Assemble(files, parsed):
    nodes := every maven/pom Parsed (path, directory, Parsed); config := every maven/config by directory (a multi-module root is the nearest ancestor directory with a fetched .mvn/maven.config)
    link(n): the parent of n among repository POMs:
        1. the file at <relativePath> (default ../pom.xml) when its coordinates equal the declared parent (a mismatch -> maven.parent-mismatch)
        2. else any repository POM providing exactly the parent coordinates (reactor lookup)
        3. else the PomSource, when configured and allowed (below)
        4. else maven.parent-unresolved (info): the unit still gets its own dependencies; inherited properties and management are unknown (flag: incomplete)
        a relativePath that leaves the repository -> maven.relative-path-escape (and falls back to 2 and 3)
    effOf(n): memoised; parent chain depth <= 32 and cycle-checked (maven.parent-cycle: the chain is cut)
        properties := parent's, then the POM's own, then active-profile properties, then the CLI -D properties of maven.config (which win)
        implicit: project.groupId/artifactId/version/packaging/name, parent.*, pom.*, properties.*; the CI-friendly ${revision} is an ordinary property of the unit's own chain (or a -D of maven.config)
        active profiles := explicit (-P) > property activation against the CLI properties > activeByDefault (only when no other profile is active); jdk/os/file profiles -> maven.profile-unknown
        management := walk the chain root first, the child's entries override; import BOMs expanded depth-first (<= 8, cycle-checked: maven.bom-cycle), a BOM's own management and properties interpolated in the BOM's context
    per POM -> one unit (packaging pom units included; they carry members and management only):
        coordinates inherited (groupId, version from the parent when absent; ${revision} expanded from the unit's own properties first)
        for each dependency (own, then active profiles): interpolate version and coordinates; apply management (version, scope, optional); version unmanaged -> maven.version-unmanaged
        plugins and extensions as build-scope dependencies; plugin versions from pluginManagement
        diagnostics: the recorder keeps at most 8 per code per unit and appends one summary diagnostic with the remaining count
    return nil when no POM exists
```

Budgets: run-wide expansion 64 MiB (`expand.Expander` shared across the run), per value 1 MiB, per POM 8 MiB, `maxProps` 20,000 entries per POM, parent depth 32, BOM depth 8, `PomSource` calls 64. `TestChainCycles` (a to b to a, self parent, BOM cycles), `TestPropertyBomb` (40 properties that each quadruple their successor, plus a two-property cycle and a self reference: `limit.expand` and `maven.property-cycle` within 2 s, no requirement above 1 MiB), `TestManyBombedPoms` (200 POMs each carrying a 30-level bomb: the shared run budget bounds the sum, 5 s allowed), `TestPathEscapes` (relativePath, modules), `TestOrderIndependent` (the same tree with the file order permuted gives equal units).

**PomSource privacy rule (4.5.6, the part of D-15 and doc 11 RR-14 that touches Maven).** A POM outside the repository (a parent such as `org.springframework.boot:spring-boot-starter-parent:3.4.0`, a BOM such as `org.junit:junit-bom`) can only be seen through an injected `manifests.PomSource`, never through the network from this package. The assembler asks it for a coordinate only if all of these hold, in this order, and reports the reason otherwise:

1. a `PomSource` and an `allow` guard are both present (`WithPomSource`; a nil guard disables the source: programming error, never an open door);
2. the coordinates contain no unresolved `${...}` placeholder (`maven.parent-unresolved` with reason "coordinates contain unresolved placeholders"; a placeholder would leak a property name and cannot resolve anyway) **and are valid Maven identifiers**: groupId and artifactId only `[A-Za-z0-9_.-]`, the version additionally `+`, no segment of the groupId empty, none of the three `.` or `..`, at most 200 bytes (`safeCoordinate`, reason "coordinates are not valid Maven identifiers"). The satisfier in `cmd/scp` turns a coordinate into a URL path; a POM from a hostile repository must not be able to put `..`, `?`, `#`, `%` or a newline into it (`TestPomSourceCoordinatesAreSafe`, VF-12-04). The satisfier must still escape every path segment itself (task 5.3);
3. the guard allows the coordinate: the root's `PrivacyGuard` answers false for any `groupId` of an organisation artifact (an artifact the repository set provides), for `manifests.pom_source.private_group_prefixes` and for hosts outside `manifests.pom_source.hosts`. A refusal is `maven.snapshot-private-skipped` (info, the coordinate is not sent);
4. the 64-fetch budget of the assembly is not exhausted;
5. the answer is memoised per `(group, artifact, version)` for the assembly (a failure is memoised too, so a missing parent costs one call);
6. **a source error caused by the assembly context ending is not an answer.** `Assemble` checks `ctx` after the last unit and returns `ctx.Err()` and no units (the first revision checked only at the top of each unit, so a deadline that expired during the last unit's `PomSource` call published a unit saying "parent not found" as final facts; `TestAssembleReturnsContextErrorAfterAFetchTimeout`, VF-12-05, GR-22);
7. **the facts depend on the wiring.** `AssemblerVersion()` is `1` without a source and `1 | 1<<16` with one (a nil guard counts as no source), so switching `manifests.pom_source.enabled` changes `Registry.SetHash` and the root rebuilds the stored facts; before, the stored facts kept saying `parent-unresolved` until the next push (`TestPomSourceEnabledChangesTheRegistryHash`, VF-12-06). What the hash cannot capture (a transient source failure, a guard that learns more producers) is L-11 and L-12, RR-A9.

The `PomSource` answer is parsed with the **same parser** and the default limits (1 MiB, depth 256), tagged with the pseudo path `pomsource:<g>:<a>:<v>`, checked to be a POM (`maven.pom-source-failed` otherwise) and recorded as `maven.parent-fetched` (info, with the coordinate). A fetched POM never becomes a unit and never adds files to a unit's `Files`; it only supplies inherited properties and management. What goes out is a coordinate; what comes in is parsed as untrusted XML; nothing of the repository's content (names, paths, other coordinates) is ever sent. `TestPrivateGuard` builds the real `manifests.NewPrivacyGuard` with private prefixes `com.acme` and `org.secret`, a parent in `com.acme.platform` and two imported BOMs (`org.secret.bom`, `org.public`): the fake `PomSource` is asked exactly once, for `org.public`, and exactly two `maven.snapshot-private-skipped` diagnostics are emitted.

#### 4.5.7 Limits and hardening

| Threat | Mechanism | Test |
|---|---|---|
| XXE, external entities, DTD | none expanded (standard library); `maven.syntax` for any undefined entity | `TestParserBombs/entity-bomb` |
| billion laughs | no entity expansion exists | idem |
| deep nesting `<a><a>...` | depth budget -> `limit.depth` | `depth-bomb` |
| property bombs (`${a}${a}...` doubling) | `expand.Expander` budgets (value, unit, run) -> `limit.expand` | `TestPropertyBomb`, `TestManyBombedPoms` |
| UTF-16, wrong declared charset, NUL | `encoding.unsupported`, never guessed | `utf16-bom`, `utf16-declared`, `unknown-charset`, `nul` |
| foreign-namespace injection | `xnode.foreign` ignored in lookups | golden `foreign-namespace` |
| parent and BOM cycles, deep chains | memo + `seen` set, depth 32 and 8 | `TestChainCycles` |
| path escape through `relativePath` and `<module>` | `repopath.Join`; `maven.relative-path-escape`, `maven.module-invalid` | `TestPathEscapes` |
| secrets in a POM (`<password>`, `settings` copies, URL userinfo) | only the listed elements are read, `<repositories>` and `<scm>` never; text through `CleanText` | `TestNoCredentialsEmitted` |
| unresolved placeholders leaking to the source | rule 2 above | `TestPrivateGuard` |
| path or URL syntax in a coordinate sent to the source (`..`, `?`, `#`, `%`, newline, space) | `safeCoordinate` (rule 2) | `TestPomSourceCoordinatesAreSafe` |
| deadline during a source call published as "not found" | final `ctx` check (rule 6) | `TestAssembleReturnsContextErrorAfterAFetchTimeout` |
| diagnostic flood (one unresolved property in 5,000 dependencies) | recorder: 8 per code per unit plus one summary | golden `real-spring-boot` |

#### 4.5.8 Diagnostics (22 codes)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `maven.syntax` | error | parser | not well-formed XML or undefined entity |
| `maven.not-a-project` | warning | parser | root element is not `<project>` |
| `maven.no-artifactId` | warning | parser | no artifactId |
| `maven.dependency-incomplete` | warning | parser | dependency, parent or project without or with an invalid coordinate |
| `maven.scope-placeholder` | info | parser | placeholder or unknown scope |
| `maven.system-scope` | info | parser | `system` scope |
| `maven.module-invalid` | warning | parser | absolute or malformed module |
| `maven.config-option-ignored` | info | parser | other option in `maven.config` |
| `maven.parent-unresolved` | info | assembler | parent not found |
| `maven.parent-mismatch` | warning | assembler | POM at `relativePath` is not the declared parent |
| `maven.parent-cycle` | warning | assembler | cycle or depth over 32 |
| `maven.parent-fetched` | info | assembler | PomSource supplied a parent or BOM |
| `maven.pom-source-failed` | info | assembler | PomSource answer is not a POM |
| `maven.property-unresolved` | info | assembler | `${x}` not resolvable statically |
| `maven.property-cycle` | warning | assembler | property cycle |
| `maven.version-unmanaged` | warning | assembler | dependency without version and without management |
| `maven.bom-unresolved` | info | assembler | BOM not found |
| `maven.bom-cycle` | warning | assembler | BOM cycle or depth over 8 |
| `maven.module-missing` | warning | assembler | declared module without fetched POM |
| `maven.profile-unknown` | info | assembler | jdk, os or file activation |
| `maven.relative-path-escape` | warning | assembler | parent relativePath leaves the repository |
| `maven.snapshot-private-skipped` | info | assembler | private or organisation coordinate not sent |

#### 4.5.9 Corpus

| Case | Kind | Source, licence | What it proves | Expected |
|---|---|---|---|---|
| `single-module` | tier 1 synthetic | this unit (MIT) | scopes, types, classifiers, system scope, incomplete dependency, unmanaged version, placeholder | 13 deps; `version-unmanaged`, `property-unresolved`, `system-scope`, `dependency-incomplete` |
| `reactor-inheritance` | tier 1 tree (4 POMs) | idem | parent via relativePath, inherited groupId and version, management chain, missing module | 4 units, 12 deps, `module-missing` |
| `ci-friendly` | tier 1 tree | idem | `${revision}` in version and parent | 2 units, no diagnostics |
| `bom-local` | tier 1 tree | idem | local BOM import with its own properties | 2 units; `bom-unresolved` x2 for BOMs not in the tree |
| `external-parent` | tier 1 | idem | no PomSource: parent unresolved | `parent-unresolved`, `property-unresolved` |
| `profiles` | tier 1 | idem | activeByDefault, property activation, jdk/os | 6 deps, `profile-unknown` x2 |
| `kotlin`, `latin1`, `foreign-namespace` | tier 1 | idem | Kotlin plugin and stdlib detection; `iso-8859-1` prolog; foreign namespace ignored | 3, 1, 1 deps |
| `real-maven` | real | apache/maven `pom.xml` 55 KB (4.1.0-SNAPSHOT), Apache-2.0, blob in `SOURCES.tsv`; PomSource fixtures `maven-parent-50`, `apache-40` from Maven Central, Apache-2.0 | real inheritance through the PomSource, 33 dependencies, plugins, profiles | `parent-fetched` x2, `bom-unresolved` x3 (junit-bom, mockito-bom not served), `profile-unknown` x4, `module-missing` x4 |
| `real-commons-lang` | real | apache/commons-lang `pom.xml` 35 KB, Apache-2.0; PomSource: `commons-parent-105`, `apache-39` | 67 dependencies and a `relativePath` that leaves the repository | `relative-path-escape`, `parent-fetched` x2, `profile-unknown` x9 |
| `real-spring-boot` | semi-synthetic | `spring-boot-starter-parent:3.4.0` as parent and `spring-boot-dependencies:3.4.0` (97 KB BOM, Apache-2.0, Maven Central) under `pomsource/`; the input `pom.xml` is synthetic | Spring Boot parent chain and a 9-BOM import closure | 6 deps resolved `managed`, `bom-unresolved` x9 (the nested BOMs not supplied) |
| `fuzz` | regression | 1 seed: parent groupId `/` | purl round trip | |

Timing: the largest real POM (apache/maven, 55 KB) assembles in 1.1 ms; parse-only allocation volume is 11.0 bytes per input byte (E-A5).

---

### 4.6 Gradle (`ecosystems/gradle`, ecosystem id `gradle`, targets are `maven` identities)

Gradle has no manifest format: dependencies are written in a Groovy or Kotlin **program**. The honest design is a lexical reader that finds the **declarative idioms** (`dependencies { implementation("g:a:v") }`, version catalogs, `plugins { id("...") version "..." }`, `project(":x")`, lockfiles) and says so everywhere: every dependency read from a script is `ConfHeuristic`; only version-catalog entries and lockfile lines are `ConfExact`. Nothing is evaluated.

#### 4.6.1 Files and match

| Parser id | Matches | Kind |
|---|---|---|
| `gradle/settings` | `settings.gradle`, `settings.gradle.kts` | workspace |
| `gradle/script` | `build.gradle`, `build.gradle.kts` | manifest |
| `gradle/catalog` | `*.versions.toml` (default `gradle/libs.versions.toml`, others declared in settings) | catalog |
| `gradle/lockfile` | `gradle.lockfile`, `settings-gradle.lockfile`, `buildscript-gradle.lockfile`, `*.lockfile` inside a `dependency-locks` directory (older per-configuration locks) | lockfile |
| `gradle/properties` | `gradle.properties` | workspace |

Excluded directories: `build`, `.gradle`, `out`, `target`, `node_modules`, `src/test/resources`. Not read: `buildSrc` and `build-logic` convention-plugin code for dependencies (scripts inside them are standalone builds and give their own units), `init.gradle`, `~/.gradle`, `gradle/wrapper/*`, `*.gradle` files other than the two names above.

The ecosystem id `gradle` is new (D-5); its `Spec` declares `PURLType` `maven` and `VersionSystem` `maven` because the artifacts are Maven coordinates, and every dependency target is a `maven:` identity (the foreign-ecosystem rule of RR-A3). The units are `gradle`, so a repository with both a `pom.xml` and a `build.gradle` shows two units.

#### 4.6.2 Grammar handled

- A **lexer** (`lex.go`, linear, never fails): Groovy and Kotlin tokens, `'..'`, `".."` with `$x` and `${x}` templates (kept for the expander), `'''` and `"""` multi-line strings, `//`, `/* */`, numbers, identifiers, punctuation, newlines. Unterminated strings and comments end at end of input.
- A **scanner** (`scan.go`): block structure by brace matching to a depth budget; statements as `call`, `assign`, `decl` and `enter` events; command calls without parentheses (Groovy `implementation 'g:a:v'`), calls with trailing lambdas.
- **Dependencies**: inside `dependencies { }` (also nested in `allprojects`, `subprojects`, `buildscript` and `project(":x") {}`): `conf "g:a:v"`, `conf("g:a:v")`, `conf group: 'g', name: 'a', version: 'v'`, map and named-argument forms, `@ext` suffix, `:classifier`, `platform(...)`, `enforcedPlatform(...)`, `testFixtures(...)`, `kotlin("stdlib")` (`org.jetbrains.kotlin:kotlin-stdlib`), `libs.foo.bar` and `libs.bundles.x` (catalog accessors, with `.get()`/`.asProvider()`), `project(":x")` and `projects.x.y` (typesafe accessors), `files(...)`, `fileTree(...)` (`gradle.local-dependency`), string templates over literal variables defined in the same file, `ext {}`, `extra["x"]`, `val`/`def` string variables, `gradle.properties` values (`project.property("x")`), `constraints { }` (managed, never added as dependencies).
- **Plugins** and the identity: `plugins { id("x") version "v"; kotlin("jvm"); alias(libs.plugins.x) }`, `apply plugin: 'x'`; `group = ...`, `version = ...` (also in `allprojects`/`subprojects`, or `gradle.properties`), `java { toolchain { languageVersion } }`, `sourceCompatibility`, `JavaVersion.VERSION_17`. A plugin `id` gets a **marker dependency** `<id>:<id>.gradle.plugin:<version>` (V-9; the real Gradle plugin marker artifact) in scope `build`, group `plugin`.
- **Settings**: `rootProject.name`, `include(":a", ":b:c")` and `include ':a'`, `includeFlat`, `includeBuild`, `pluginManagement { plugins { id(..) version .. } }`, `dependencyResolutionManagement { versionCatalogs { create("libs") { from(files("../x.toml")); version("a","1"); library("n","g","a").version("v") } } }`, `projectDir` assignments (diagnosed).
- **Version catalogs** (`tomlx`): `[versions]`, `[libraries]` (string `g:a:v`, `{ module = "g:a", version.ref = "v" }`, `{ group, name, version }`, `version = { strictly | require | prefer | reject | rejectAll }`), `[bundles]`, `[plugins]` (`id`, `version`, `version.ref`) per the Gradle 9.8 catalog documentation (V-8): rich versions give `Requirement` = `strictly` else `require` else `prefer`; `strictly` and `[x]` and `x!!` are exact pins.
- **Lockfiles** (V-10): `group:artifact:version=conf1,conf2` lines, `empty=` last line; the config list gives scope by rank (runtime 0, compile-only 1, test 2, annotation processor and buildscript 3); per-configuration files take the scope from the configuration named by the file (`dependency-locks/compileClasspath.lockfile`); `settings-gradle.lockfile` and `buildscript-gradle.lockfile` are build scope.

#### 4.6.3 Grammar not handled

`apply from:` scripts (the dependencies of the applied script are not read: `gradle.apply-from`), `projectDir` overrides (default layout assumed: `gradle.project-dir-override`), loops and computed values in `settings` includes (`gradle.settings-dynamic`: only literal includes), convention plugins and `buildSrc` extension functions that declare dependencies, `dependencies { add("x", ...) }`, `configurations.all { resolutionStrategy }`, dynamic strings built by function calls, slashy and dollar-slashy strings, `if`/`when` guards (all branches are read, so a dependency in a branch is emitted; flagged heuristic by the whole-script rule), `libs` accessors for catalogs not fetched (`gradle.catalog-missing`), Gradle's own dynamic version selection (`1.+`, `latest.release` stay requirements), version catalog `import`s from plugins, Android `variant` configurations beyond the name patterns of the scope table, `gradle/verification-metadata.xml`.

#### 4.6.4 Mapping to the normalised model

| Configuration (exact names first, then lower-case patterns) | Scope | Flag |
|---|---|---|
| `implementation`, `api`, `compile`, `runtime`, `runtimeOnly`, `releaseImplementation`, `releaseApi`, `shadow`, `embed` | runtime | known |
| `compileOnly`, `compileOnlyApi`, `providedCompile`, `providedRuntime` | provided | known |
| `developmentOnly`, `debugImplementation`, `debugApi`, `debugRuntimeOnly` | dev | known |
| `annotationProcessor`, `kapt`, `ksp`, `classpath`, `detektPlugins`, `lintChecks`, `lintPublish`, `agent` | build | known |
| `testFixtures*` | test | known |
| anything containing `test` (`testImplementation`, `androidTestImplementation`, `integrationTestImplementation`, `functionalTest...`), prefix `kapt`/`ksp`, suffix `AnnotationProcessor`, `CompileOnly`, `Implementation`, `Api`, `RuntimeOnly` | test; build; provided; runtime | pattern (the dependency is `heuristic`) |
| unknown | runtime | pattern |

| Source | Model |
|---|---|
| project (`build.gradle*`) | one **unit** per project directory (settings-included projects first, root last, standalone scripts last); `Provides` only when the group is known (`group =`, `gradle.properties`, `allprojects`), name from `rootProject.name` or the project path, `Kind application` for projects applying `application`, `com.android.application`, `org.springframework.boot`, `io.ktor.plugin`, `org.jetbrains.compose`, else `library` |
| `project(":x")`, `projects.x.y` | `SourceProject` in the parser; the assembler maps the project path to its directory (from settings includes; default layout `a/b` when there is no settings file) and demotes to `SourcePath` + directory; a project with a known group targets `group:name` (so the organisation graph links it to the producing unit), else `maven::<path>` placeholder with the path in the display (`:tools`); an unresolved project is `gradle.project-missing` and a path dependency without a reference |
| catalog accessor | the parser emits a placeholder (`SourceRef "catalog"`); the assembler replaces it by the catalog entry (`libs.okhttp.bom` matches the alias `okhttp-bom`, `okhttp.bom` or `okhttp_bom` by `normAlias`), the version from `[versions]` through `version.ref`; the dependency stays `ConfHeuristic` (the accessor was found lexically); a missing alias gives `gradle.catalog-missing`, a missing version ref `gradle.catalog-version-missing` |
| unreferenced catalog entries | the build-root unit lists every library and plugin of a fetched catalog file that no fetched script references, as `Dependency{Group "catalog-unused", Scope unknown (libraries) or build (plugins), ConfExact}` with the catalog file as location; this keeps the catalog visible when the scripts of subprojects are not among the fetched files, and distinguishes declared-but-unused from used |
| bundles | `libs.bundles.x` expands to one dependency per member alias |
| plugins | marker dependency, scope build, group `plugin` |
| `constraints { }` | parsed with group `constraint:<configuration>`; the assembler applies the version to a declared dependency of the same `group:artifact` that has none (`Resolution managed`) and never emits the constraint alone |
| lockfile | `LockedComponent{Relationship unknown -> direct when a declared dependency matches, else transitive; Scope by config rank; Integrity empty}`; attached to the unit whose directory holds the lock file (the root unit for a root lock); the joined declared dependency gets `Version` and `ResLocked` |
| tool | `Toolchain["tool"]="gradle"`, `dsl` (`groovy`/`kotlin`, decided in the assembler from the file name), `language` (`kotlin` when a Kotlin plugin or `org.jetbrains.kotlin` non-build dependency exists; `Toolchain["kotlin"]` its version or `unknown`), `java` (toolchain or compatibility), `android` |

#### 4.6.5 Confidence

Every dependency read from a script, a settings file or a property is `ConfHeuristic`, including those whose coordinates come from a catalog accessor; the diagnostic `gradle.script-heuristic` (info, once per file) says so. Only the `catalog-unused` entries (a catalog file is data) and the lockfile entries are exact. A project that applies `kotlin("jvm")` through a convention plugin in `build-logic` is not detected (no heuristic invention): the Kotlin technology then relies on the dependencies.

#### 4.6.6 Assembly

```
Assemble(files, parsed):
    index parsed by directory: settings, script, properties, locks (a "dependency-locks/x.lockfile" belongs to the parent of that directory), catalogs by path
    for each directory with a settings file (a build root, sorted):
         build := {projects: ":" -> root dir; include(":a:b") -> dir/a/b ("includeFlat": ../a); explicit projectDir assignments -> default layout + gradle.project-dir-override}
         catalogs := libs (the file gradle/libs.versions.toml next to settings), settings-declared files (from(files("...")), resolved against the settings directory among fetched catalogs), inline catalogs (version/library calls in settings)
         one unit per project with a fetched script (project dirs first), then the root unit (members = project dirs; gradle.project-missing for included projects without a fetched script, only for explicit includes)
    standalone script directories (no settings file owns them: buildSrc, build-logic, single-project): one unit each, with the default layout and no catalogs unless gradle/libs.versions.toml is next to them
    per unit: identity (group/version from own script, allprojects/subprojects fan-out of the root script, gradle.properties, the root's), declarations in scope (own script; root-script allprojects and subprojects fan-out, project(":x") {} blocks; settings and root-script plugins), expansion of ${var}/$var through script vars, gradle.properties and ext (expand budget shared: 64 MiB per run), catalog resolution, project resolution, constraints, lock join, toolchain
    recorder: at most 8 diagnostics per code per unit and one summary
    return nil when no unit was placed
```

A unit never keeps `SourceProject`; a unit's `Members` are directories (sorted). A `settings.gradle` that includes a project whose script was not fetched still lists it as a member (explicit includes only), and a `includeBuild` (composite build) is *not* followed: the included build is a separate Gradle build whose own `settings` file, if fetched, makes its own build root (`gradle.include-build`, info).

#### 4.6.7 Limits and hardening

`lex` calls `g.Node()` per token and polls the context; the scanner has a block depth budget (`limit.depth`); templates expand through `expand.Expander` (`TestTemplateBomb`: a variable that doubles through 30 references stops at `limit.expand`); includes cannot escape the repository (`TestIncludeEscapes`: `include("../../x")`, `include("/abs")`, `"\` member F-A4); `gradle.properties` values whose key looks like a credential become `<redacted>` and are never interpolated into a dependency (`TestSecrets`); `TestLexicalBombs` covers 100,000 nested `a {` blocks (`limit.depth`) and parentheses, unterminated strings and comments, a `${` repeated 50,000 times, a token budget of 500 over 8,000 tokens (`limit.nodes`), 1 MiB size, NUL and UTF-16 input, TOML key depth, a garbage lockfile (1,000 lines of `::=`) and a 100,000-line properties continuation, each within 3 s.

#### 4.6.8 Diagnostics (16 codes)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `gradle.syntax` | error | parser | lexer or TOML failure (a limit hit is `limit.*`) |
| `gradle.script-heuristic` | info | parser | dependencies read lexically from a script |
| `gradle.unresolved-notation` | info | parser | argument is not a literal, accessor or project |
| `gradle.template-unresolved` | info | assembler | `${x}` not resolvable |
| `gradle.catalog-missing` | warning | assembler | accessor names a missing catalog or alias |
| `gradle.catalog-version-missing` | warning | assembler | `version.ref` without `[versions]` entry |
| `gradle.catalog-invalid` | warning | parser | malformed catalog entry |
| `gradle.project-missing` | warning | assembler | included or referenced project has no script or is not included |
| `gradle.project-dir-override` | info | parser | `projectDir` assigned |
| `gradle.include-invalid` | warning | parser | empty, absolute or malformed include |
| `gradle.settings-dynamic` | info | parser | loops or computed includes |
| `gradle.apply-from` | info | parser | `apply from:` |
| `gradle.local-dependency` | info | parser | `files()`/`fileTree()` |
| `gradle.lockfile-syntax` | warning | parser | malformed lockfile line |
| `gradle.include-build` | info | parser | `includeBuild` |
| `gradle.variable-unresolved` | info | assembler | dependency refers to a non-literal variable |

(The catalogue had a seventeenth code `gradle.type-mismatch` that no code path emitted; it was removed, appendix E.)

#### 4.6.9 Corpus

| Case | Kind | Source, licence | What it proves | Expected |
|---|---|---|---|---|
| `groovy-single` | tier 1 synthetic (Groovy, 20 KB golden) | this unit (MIT) | Groovy command calls, map notation, `ext`, `apply from`, `files()`, platform, exclude closures | 22 deps; `apply-from`, `local-dependency`, `unresolved-notation` |
| `dynamic-and-vars` | tier 1 (Kotlin DSL) | idem | `val` variables, templates, string concatenation, unresolved variables | 15 deps; `template-unresolved` x3, `variable-unresolved`, `unresolved-notation` |
| `kotlin-multiproject` | tier 1 tree (7 files) | idem | settings includes, catalog with bundles, plugin markers, `project(":x")` with and without a group, includeBuild, Kotlin toolchain, `gradle.properties` credentials redacted | 4 units, 30 deps, `include-build`, `catalog-missing` x2, `catalog-version-missing` |
| `lockfiles` | tier 1 tree | idem | `gradle.lockfile`, `settings-gradle.lockfile`, `buildscript-gradle.lockfile`, per-configuration `dependency-locks`, `empty=` | 3 units, 10 locked, `lockfile-syntax` |
| `real-okhttp` | real | square/okhttp (Apache-2.0): `build.gradle.kts`, `settings.gradle.kts`, `gradle/libs.versions.toml` 11.8 KB, `gradle.properties` | real catalog and plugins with 7 included projects whose scripts are not fetched | 103 deps on the root unit: 84 pinned and 4 declared `catalog-unused` entries, 15 plugin markers; `project-missing` x7, `include-build`; 1.0 ms |
| `real-ktor` | real | ktorio/ktor (Apache-2.0), settings 9 KB, catalog 14.5 KB | large settings, near-empty root script | 126 `catalog-unused` entries, `include-build` x3; 1.3 ms |
| `real-nowinandroid` | real | android/nowinandroid (Apache-2.0): root and `app` scripts, catalog 16.5 KB | Android plugins, bundles, `projects.*` accessors, KSP, product-flavor test configurations | 2 units, 138 deps (root 103, `app` 35, all `app` entries heuristic), `project-missing` x7, `catalog-missing`; 1.7 ms |
| `fuzz` | regression | 5 seeds | lock coordinate with blank part or slash, include `"\`, `project(" ")` | |

---

### 4.7 Cargo (`ecosystems/cargo`)

#### 4.7.1 Files and match

| Parser id | Matches | Kind |
|---|---|---|
| `cargo/manifest` | `Cargo.toml` (not below `target`, `node_modules`, `vendor`) | manifest (workspace when the file has `[workspace]` and no `[package]`) |
| `cargo/lock` | `Cargo.lock` (same exclusions) | lockfile |

`.cargo/config.toml` (registries, credentials), `rust-toolchain.toml` and `build.rs` are not read.

#### 4.7.2 Grammar handled

TOML 1.0 through `tomlx`. `[package]` (`name`, `version`, `edition`, `rust-version`, `license`, `publish`, each also as `key.workspace = true` or `key = { workspace = true }`), `[lib]`/`[[bin]]` (application when a `bin` and no `lib`), `[dependencies]`, `[dev-dependencies]`, `[build-dependencies]` (also with `_` spelling) and the same under `[target.'cfg(...)'.]`, every dependency form: `name = "1.2"`, `{ version, features, optional, default-features, package = "real-name", path, git (+ rev, tag, branch), registry, workspace = true }`; `[workspace]` (`members`, `exclude`, `resolver`, `package`, `dependencies`); `Cargo.lock` v1 to v4 (`[[package]]` with `name`, `version`, `source`, `checksum`, `dependencies` as `"name"`, `"name version"` or `"name version (source)"`).

#### 4.7.3 Grammar not handled

`[patch]` and `[replace]` (detected: `cargo.patch-ignored`, info), `[features]` resolution (`optional = true` is read; feature-gated edges are not evaluated), `[workspace.metadata]`, `[profile]`, `.cargo/config.toml` `paths` overrides and source replacement, `default-members`, `cargo vendor` trees, per-target `cfg` evaluation (the raw predicate is the `Condition`), `Cargo.lock` `[metadata]` checksums of v1 (a lock without a `version` field, format 1, gives `cargo.lock-v1`: edges may be incomplete).

#### 4.7.4 Mapping to the normalised model

| Source | Model |
|---|---|
| `[package]` | `Provides[0]` (`Kind application` for bin-only crates); `version.workspace = true` filled from `[workspace.package]` by the assembler (`cargo.inherit-missing` when the root has none) |
| `[dependencies]` | `Scope runtime`, `Group "dependencies"`; `optional = true` gives `Scope optional, Optional true` |
| `[dev-dependencies]` | `Scope test`, group `dev-dependencies` |
| `[build-dependencies]` | `Scope build` |
| `[target.'cfg(unix)'.dependencies]` | same scopes, `Condition "cfg(unix)"` |
| `{ package = "real" }` | the **real** crate is the target and the `TargetDisplay` (the local rename is not kept: the first revision of this row said the opposite; the code never did). In `[workspace.dependencies]`, inheritance is by the **key**: `key = { package = "real" }` is found by a member's `key.workspace = true`, and the resolved dependency shows `real` (`pool.real:<key>` property, golden `workspace-rename`, VF-12-07) |
| `{ path = "../x" }` | `SourcePath`; the runner rebases it and judges escapes (`assemble.path-escape`) |
| `{ git = "...", rev = "..." }` | `SourceGit`, ref `redact.URL(url)#rev` |
| `{ registry = "name" }` | `SourceRegistry`, ref `registry:name` |
| `=1.2.3` | `Version 1.2.3`, `ResPinned` |
| `{ workspace = true }` | resolved against `[workspace.dependencies]` of the workspace root; the member's scope, group, condition, position and `optional` win; version, source and display come from the pool entry |
| `Cargo.lock` package | `LockedComponent{Version, Integrity "sha256:<checksum>" or "git:<rev>", Relationship, Scope, DependsOn}`; packages without `source` (workspace members, path dependencies) are dropped from `Locked` (they are units, not components) and kept as `local:<i>` facts |

#### 4.7.5 Identity and purl

Crate names are case-insensitive and `-`/`_` equivalent for the registry: `Normalize` (the root's identity table, research 07 section 5.1) lower-cases and folds `_` to `-`; the purl-spec definition of `cargo` itself prescribes no folding (name case-sensitive, no namespace, V-15), so this is the registry's rule, chosen so that `serde_json` and `serde-json` are one component in the graph; the original name is the display (`serde_json` displays as written, identity `serde-json`) and the purl is built from the identity. `pkg:cargo/serde-json@1.0.120`.

#### 4.7.6 Confidence

`ConfExact`. Optional features and `cfg` conditions are exact data; their evaluation is not claimed.

#### 4.7.7 Assembly

```
Assemble(files, parsed):
    manifests sorted by path; dir -> manifest; locks by directory
    workspace roots := manifests with a [workspace] table, members or workspace.* facts
    membership := directories of fetched manifests matching a member glob of a root (repopath.Match, relative to the root's directory) and no exclude; outermost root claims a directory first
    per manifest -> one unit (the virtual workspace root gets a unit without Provides; it carries Members):
         package fields inherited (version, edition -> Toolchain, rust-version -> Toolchain["rust"], license) from the root's workspace.package
         dependencies: { workspace = true } resolved from the root's [workspace.dependencies] pool; pool entries themselves are not dependencies
         lock: the Cargo.lock of the workspace root directory joins every member (Version, ResLocked on registry dependencies; cargo.lock-stale when a declared requirement is not satisfied by the locked version; absent -> declared only)
    root unit: the whole Cargo.lock attached as Locked: direct = declared by any member; scope by reachability from the members' runtime > optional > build > test dependencies; local packages dropped
    return nil when no manifest exists (a Cargo.lock alone falls to default assembly)
```

#### 4.7.8 Limits and hardening

`tomlx` key depth 32 (`TestBombs/key-depth`: a header of 100,000 segments gives `limit.depth`), 8 MiB lock limit, one `g.Node()` per table entry and per lock package, a self-dependency in the lock is harmless (`TestBombs/self-dependency`); member globs bounded by `repopath.Match` (`TestWorkspaceBounds`: members `../../*`, `/abs`, `a/../../b`, a pattern of 14 `**/` segments and `crates/*` over 300 crates, plus `{path='../../../etc'}`, finished within 3 s and no member escapes); `git` URLs and `registry` names pass `redact.URL` and `CleanText` (`TestSecrets`: `https://user:pw@host/repo.git`, `https://host/x?token=...`); names with control characters are rejected at the parser (F-A10); `Cargo.lock` of 8 MiB parses in 0.24 s (the first revision: 443 s, VF-12-01).

#### 4.7.9 Diagnostics (11 codes)

| Code | Severity | Emitted by | When |
|---|---|---|---|
| `cargo.syntax` | error | parser | invalid TOML (key depth: `limit.depth`) |
| `cargo.no-package` | warning | parser | neither `[package]` nor `[workspace]` |
| `cargo.no-name` | warning | parser | `[package]` without name |
| `cargo.type-mismatch` | warning | parser | dependency entry of an unsupported type |
| `cargo.member-invalid` | warning | parser | absolute or malformed member |
| `cargo.path-invalid` | warning | parser | absolute or malformed path dependency |
| `cargo.patch-ignored` | info | parser | `[patch]` or `[replace]` present |
| `cargo.inherit-missing` | warning | assembler | `workspace = true` without a pool entry or workspace value |
| `cargo.lock-v1` | info | parser | lock format 1 |
| `cargo.lock-dangling` | warning | parser | a lock edge names an unknown package |
| `cargo.lock-stale` | warning | assembler | lock disagrees with the manifest |

#### 4.7.10 Corpus

| Case | Kind | Source, licence | What it proves | Expected |
|---|---|---|---|---|
| `standalone` | tier 1 synthetic | this unit (MIT) | dependency forms: rename with `package`, git with rev, path escaping the repository, registry, target `cfg` | 2 deps, `assemble.path-escape` from the runner |
| `workspace` | tier 1 tree (6 files) | idem | members glob, exclude, inheritance of package fields and dependencies, lock join, patch | 4 units, 10 deps, 7 locked, `patch-ignored`, `lock-stale` |
| `workspace-rename` | tier 1 tree (3 files, added by the verifier) | idem | `key = { package = "real" }` in `[workspace.dependencies]` inherited by `key.workspace = true`; a missing pool entry | the resolved dependency has the pool's requirement `0.7` and the display `tokio-util`; one `cargo.inherit-missing` (for `missing`) |
| `errors` | tier 1 | idem | invalid TOML | `cargo.syntax` |
| `BurntSushi/ripgrep` | tier 2 | Unlicense OR MIT | workspace with package, lock | 52 locked (independent count 52), 2.1 ms |
| `tokio-rs/tokio` | tier 2 | MIT | virtual workspace root with `[patch]` | `patch-ignored`, members glob over a large list |
| `fuzz` | regression | 3 seeds | control character in a crate name and others | |

---

### 4.8 Limitations that cross ecosystems (L-n)

| Id | Limitation | Consequence | Mitigation or decision |
|---|---|---|---|
| L-1 | A directory that has **only a lockfile** next to **project** directories of the same ecosystem gets no unit: the core's default assembly applies only when the ecosystem assembler returns nil | the lock file is parsed (cached, visible in the file list) but contributes no locked components to a unit | typical layouts (a lock beside its manifest) are covered; OQ-1 asks for a "leftover files" hook in the core |
| L-2 | All assemblers are pure and see only fetched files (`acquisition.max_files` and exclusions apply) | a workspace member outside the fetched set is a diagnostic (`*.member-unfetched`, `*.module-missing`, `gradle.project-missing`), never a guess | by design |
| L-3 | No transitive resolution beyond what a lock contains; `go.mod` before Go 1.17 is incomplete | dependency counts of such repositories are lower bounds | `Toolchain["go"]` shows the language level |
| L-4 | Markers, `cfg`, profiles (jdk, os, file) and Gradle `if` branches are not evaluated | conditional dependencies are shown with their condition, including dead branches | the `Condition` is raw and displayed |
| L-5 | Gradle scripts are read lexically | convention plugins, `buildSrc` logic and computed notations are invisible | flagged heuristic and `gradle.script-heuristic`; catalogs and lockfiles are exact |
| L-6 | Yarn v1 transitive optional edges and pnpm `lockfileVersion` 5 and older are not understood | optional-only transitive packages show as runtime; very old pnpm locks are best effort | diagnosed |
| L-7 | `bun.lockb` is binary | no components | `npm.bun-binary-lockfile` explains how to produce `bun.lock` |
| L-8 | Maven external parents and BOMs need the optional `PomSource` | without it versions managed by an external framework BOM (Quarkus, for example) are `unmanaged` (warning suppressed when a parent is unresolved) | `manifests.pom_source.enabled`, privacy guard (4.5.6) |
| L-9 | A requirements file included by `-r` but not fetched | its dependencies are missing | `python.include-not-candidate` |
| L-10 | Cargo `[patch]`, npm `overrides`/`resolutions`, Go `exclude`, Maven `exclusions` are recorded or ignored, never applied | the declared graph can differ from the resolved graph | the lock file, where one exists, is the truth for versions |
| L-11 | Maven facts depend on the `PomSource` outcome, which is not part of the tree SHA: a transient failure (`maven.parent-unresolved` "not found", `maven.pom-source-failed`) stays in the stored facts until the repository's tree, the parser set or the assembler version changes | an external parent or BOM shows as unresolved for longer than the failure lasted (versions managed by it stay empty, `maven.version-unmanaged` is suppressed) | RR-A9 (2): unit 11 re-assembles such repositories on the idle back-off; until then the info diagnostic says what is missing |
| L-12 | The `PrivacyGuard` is built on the graph, which is empty on a cold start | an internal coordinate that matches no `private_group_prefixes` entry and whose producer repository is not parsed yet is not known to be internal | RR-A9 (1): `Guard().Allow` answers false until the first complete graph; **set `private_group_prefixes` for every internal groupId before enabling `pom_source`** (operator note, task 7.5) |

---
## 5. Data

**This unit owns no table, no store key, no slot and no migration.** Parsers and assemblers are pure functions; everything they produce is persisted by the root (unit 11) under its own keys: the parse cache is keyed by `(blobSHA, parserID, parserVersion)` (`Parsed` values of this unit are the cached payload), the facts value `manifests.facts/<repo>` carries `[]Unit` (and through it every `Dependency`, `LockedComponent` and `Diagnostic` of this unit's output), and the graph slot is derived from the facts by the root. The three version numbers that decide whether stored facts are still valid are this unit's responsibility:

| Number | Where | Rule |
|---|---|---|
| `Parser.Version()` | every parser, constant `parserVer = 1` | bump on **any** change of the `Parsed` a parser produces for the same bytes (a new field filled, a changed scope rule, a new diagnostic code emitted); the root then re-parses every blob of that parser (cache miss) and the registry hash changes |
| `VersionedAssembler.AssemblerVersion()` | every assembler, constant `assemblerVer = 1` (Maven: `1` without a `PomSource`, `1 \| 1<<16` with one, VF-12-06) | bump on any change of the units built from the same parsed inputs; part of `Registry.SetHash`, so the root rebuilds facts without re-fetching (the assembly is always re-run on a parse-cache hit; the hash decides whether a stored unit list is still valid) |
| `Spec().Diagnostics` | `EcosystemSpec` | the diagnostic catalogue; adding a code is not a version change, removing or repurposing one is (stored facts carry the code string, the API documents it) |

**Persisted size.** The root bounds a facts value at `manifests.limits.max_facts_bytes` (8 MiB default) and a repository at 500 units. Measured JSON size of `[]Unit` for the corpus (E-A3; the root's encoding differs by a constant factor, so these are an upper-bound guide, not a promise):

| Tree | Locked | Declared | `[]Unit` as JSON |
|---|---|---|---|
| pnpm monorepo (vue/core) | 634 | 52 | 232 KB |
| pnpm (vercel/ai, 1.9 MB lock) | 4,061 | 0 (lock only) | 1.49 MB |
| package-lock v3 (npm/cli) | 993 | 0 | 295 KB |
| Yarn Berry (yarnpkg/berry) | 2,050 | 0 | 527 KB |
| Yarn v1 (react) | 2,388 | 112 | 825 KB |
| uv (flask) | 78 | 31 | 28 KB |
| Cargo (ripgrep) | 52 | 14 | 19 KB |
| Go (kubernetes) | 198 | 114 | 70 KB |
| Gradle (okhttp, ktor, nowinandroid) | 0 | 103, 126, 138 | 25 KB, 28 KB, 33 KB |

About 370 bytes per locked component with edges (pnpm, Yarn v1), 100 to 200 without. The largest known real lock files therefore fit in the 8 MiB budget with a factor of five to ten; a lock above `max_locked_per_file` (100,000) is truncated by `sanitizeParsed` with a diagnostic, never persisted whole.

**Test data** (`testdata/`, committed) is part of this unit's deliverable and is described in section 12 and appendix D. No fixture contains a credential; the synthetic ones that exercise redaction build their secrets in the test source, not in a file.

---

## 6. Configuration

**This unit owns no configuration key.** The six ecosystem packages have no `Config` type, no environment variable and no flag. A knob per ecosystem would be a second place to disable a parser, and the root already has the only one (`manifests.acquisition.disabled_ecosystems`, doc 11 RR-22). The keys that **act on** this unit's code are the root's; they are listed here so the engineer knows which setting reaches which line of this unit, with the key names exactly as doc 11 section 6 defines them:

| Key (owner: root, unit 11) | Type, default | How this unit's code is affected | Consumer in this unit |
|---|---|---|---|
| `manifests.limits.manifest_bytes` | size, `1MiB` | `Limits.MaxBytes` for every non-lockfile parser; every parser also checks it itself (`limit.size`) so a direct caller is safe | `admit`, `prepare`, `reject` in each package |
| `manifests.limits.lockfile_bytes` | size, `8MiB` | the limit for parsers that implement `Kinded` with `KindLockfile`: `npm/package-lock`, `npm/pnpm-lock`, `npm/yarn-lock`, `npm/bun-lock`, `pypi/poetry-lock`, `pypi/uv-lock`, `pypi/pylock`, `pypi/pipfile-lock`, `gradle/lockfile`, `cargo/lock`. A `pip-compile` `requirements.txt` is a lockfile **by content**, so it keeps the manifest limit | same |
| `manifests.limits.max_depth` | int, `256` | `Limits.MaxDepth` through `limits.NewGuard`: JSON and XML nesting, Gradle block depth, YAML depth | `jsonx.go`, `xmlx.go`, `scan.go`, `yamlx.go` |
| `manifests.limits.max_nodes` | int, `2000000` | `Limits.MaxNodes`: tokens, entries, lock packages; a hit is `limit.nodes` | every `g.Node()` call |
| `manifests.limits.max_expand`, `max_expand_file` | size, `1MiB`, `8MiB` | Limits fields `MaxExpand`, `MaxExpandAll`. **Not wired into the assemblers**: `Assemble` receives no `Limits`, so `expand.Expander` uses constants equal to the defaults (1 MiB per value, 8 MiB per unit, 64 MiB per run). Deviation D-9, OQ-5 | `expand.New` |
| `manifests.limits.parse_timeout` | duration, `2s` | the root watchdog and the context handed to `Parse`; parsers poll `ctx` and return `ctx.Err()` only then | `Guard.Node`, `ctx.Err()` in loops |
| `manifests.limits.assemble_timeout` | duration, `5s` per ecosystem | the context handed to `Assemble`; assemblers poll it once per unit and return `ctx.Err()` | per-unit loops |
| `manifests.limits.max_deps_per_file`, `max_locked_per_file`, `max_units_per_repo` | ints | enforced by the root in `sanitizeParsed` and `finalize`; this unit's output is truncated there, with a diagnostic | none (root) |
| `manifests.acquisition.disabled_ecosystems` | list | `Registry.Disable`: the ecosystem's parsers do not match and its assembler is not called | none (root) |
| `manifests.acquisition.exclude_segments`, `max_files`, `max_depth` | lists, ints | which files reach the parsers at all; the `Match` functions repeat the vendor-directory exclusions as a second defence (4.1.6) | `Match` |
| `manifests.pom_source.enabled`, `hosts`, `private_group_prefixes` | bool `false`, list `[repo1.maven.org]`, list `[]` | `cmd/scp` constructs the `PomSource` only when enabled, restricted to `hosts`; `private_group_prefixes` feeds `NewPrivacyGuard`, whose `Allow` is passed to `maven.WithPomSource` | `maven.WithPomSource`, `run.fetch` |

Environment variable names follow the root rule (`SCP_MANIFESTS_...`, comma-separated lists, doc 11 section 6) and are not repeated. No key of this unit has a value that needs validation beyond the root's: every number this unit hard-codes (workspace patterns 128, parent depth 32, BOM depth 8, include depth 8, `PomSource` calls 64, diagnostics per code 8, key-path depth 32, logical requirement line 64 KiB, YAML documents 4 and alias depth 4) is a **structural safety constant**, listed in 4.1.7, tested at its boundary, and deliberately not configurable: a deployment that wants to raise them is asking for a different parser.

---

## 7. HTTP / API surface

**n/a.** This unit has no route, no handler and no JSON schema of its own. What the API shows of this unit's work is defined by the root (doc 11 section 7): the eight operations return `Unit`, `Dependency`, `LockedComponent` and `Diagnostic` values whose **vocabulary** this unit fixes:

- `EcosystemId` values this unit adds to the open set `^[a-z][a-z0-9-]*$`: `golang`, `npm`, `pypi`, `maven`, `gradle`, `cargo`. `gradle` has no purl type (its targets are `maven`); the contract's description of `EcosystemId` ("equals purl type where one exists") should list it (D-5, OQ-8).
- `Diagnostic.code` values: the 92 codes of 4.2.9, 4.3.9, 4.4.9, 4.5.8, 4.6.8 and 4.7.9 plus the root's shared codes. The generated reference page `docs/reference/diagnostics.md` is built from `Spec().Diagnostics` (RR-A1), so no code is documented by hand.
- `Dependency.group` and `Dependency.condition` strings (3.1) are free text to the API but stable vocabulary to the SPA's filters: `dependencies`, `devDependencies`, `peer`, `optional`, `dev`, `test`, `build`, `plugin`, `plugin-managed`, `plugin-dependency`, `extension`, `managed`, `import`, `catalog-unused`, `constraint:<conf>`, and for conditions `profile:<id>[=active]`, `cfg(...)`, PEP 508 markers, `allprojects`, `subprojects`, `project:<path>`, `inherited:<ctx>`.
- `Unit.toolchain` keys (3.1).

Contract tests for these strings are the goldens of section 12 (a renamed group changes a golden and therefore needs a deliberate `-update-tree`).

---

## 8. Concurrency and lifecycle

**Parsers and assemblers are goroutine-free, clock-free and stateless.** Every exported constructor returns a value with no mutable field (`modParser{}`, `base{id, match, kind}`); `Parse` and `Assemble` allocate everything they use on the stack or the heap of the call and share nothing, so any number of calls may run concurrently on one instance (the root runs `parse_concurrency` parsers in parallel under its memory semaphore and one assembly per ecosystem). Consequences, each enforced by a test or a lint:

| Rule | Mechanism |
|---|---|
| no goroutine, no `init()` (A-18), no `os`, `os/exec`, `sync`, `sync/atomic`, `time`, `math/rand` (also `/v2`), `crypto/rand`, `log`, `slog`, `net`, `net/http`, `runtime` import in the six packages and the five helpers | the purity guard in `apply.sh` (a `grep` over the non-test sources that fails the build; verified to catch a planted `init`); the only concurrency in the module is the root's |
| no package-level mutable state | regexps and error sentinels are `var` values never reassigned; tables are functions returning fresh slices; the race detector over the 281 executions and the three-run determinism check of `TreeGolden` |
| deterministic output | `TreeGolden` runs `RunTree` three times; `TestOrderIndependent` shuffles the parsed inputs of the Maven assembler; no map iteration reaches output |
| the assembler never mutates its input | `assembleChecked` snapshots `parsed` around the registered assembler and fails with `assemble.mutated-input` (RR-A5); `Dependency.Clone` is used wherever a dependency is edited |
| cancellation | parsers poll `ctx` through `Guard.Node` (every 256 nodes) and between phases; assemblers poll once per unit; both return `ctx.Err()` and nothing else; a cancelled parse is never cached |
| the watchdog | the root abandons a parser that ignores its deadline (`watchdog_grace`, `abandon_trip`); this unit's parsers do not need it (the longest bounded run in the corpus is 61 ms) but must not hold locks or files, which they do not |
| memory | the root's weighted semaphore uses `Weigher` (default 10 bytes per input byte, OQ-3); measured allocation volume per input byte is 4.8 to 28.6 (E-A5) and retained heap 0.6 to 2.8, so the default stays |

**Lifecycle.** There is none beyond construction: `Register(reg)` is called once by `cmd/scp` before `Registry.Freeze()`; `Spec()` and the `New<Parser>()` constructors are cheap and idempotent; nothing needs a `Close`. The only injected dependency with a lifecycle is the Maven `PomSource` (owned and closed by `cmd/scp`), which the assembler calls **sequentially** within one assembly (no fan-out; at most 64 calls; each call receives the assembly's context, so an `assemble_timeout` cancels an in-flight fetch; and `Assemble` returns `ctx.Err()` rather than units when the context ended during the last fetch, VF-12-05). A CPU-bound parser step that the context cannot interrupt (go-toml's decoder, `encoding/xml` between tokens) is bounded by **input-size-independent structural caps and linear-time algorithms**, not by the watchdog: the watchdog only abandons the goroutine, which keeps burning CPU until it ends (the 443 s `Cargo.lock` of VF-12-01 would have been abandoned after 2 s and then run on for 7 minutes).

---

## 9. Observability

**This unit owns no metric, no span and no log line.** Parsers and assemblers take no logger, meter or tracer (a parser that logged would need either a global or a dependency; neither exists in rank 5), and they never write to stdout or stderr. What the root records about this unit's work, with labels that this unit's identifiers supply:

| Metric (owner: root, doc 11 section 9) | Labels this unit determines | Notes for operators |
|---|---|---|
| `scp_manifests_parse_total` | `ecosystem` in `golang npm pypi maven gradle cargo`, `parser` = the 27 ids of 1.2, `outcome` | the cardinality contribution of this unit is 27 parser values and 6 ecosystem values |
| `scp_manifests_parse_seconds` | `ecosystem` | expect tens of microseconds for a manifest and 10 to 60 ms for the largest locks (appendix B) |
| `scp_manifests_limit_hits_total` | `limit` = `size depth nodes expand` plus the suffix of any `limit.<name>` code a parser emits | the parsers emit `limit.<name>` for the names a `limits.LimitError` carries: `size`, `depth`, `nodes`, `expand` (the runner adds `limit.deps`, `limit.locked`, `limit.timeout` itself) |
| `scp_manifests_diagnostics_total` | `severity`, `namespace` = the code prefix: `golang npm python maven gradle cargo` (the python prefix is `python`, the ecosystem id is `pypi`; the namespace label is the code's prefix) | a rise of `maven.property-unresolved` after enabling `pom_source` is expected to fall |
| `scp_manifests_units` | `ecosystem` | units per ecosystem in the snapshot |

**Diagnostics are this unit's observability channel.** They are user-visible by design (`getRepoManifests` lists them per repository, the repository page shows them as a collapsed "parsing notes" section), carry a stable code, a severity, a file and a line, and never contain file content beyond one quoted token of at most 80 to 160 bytes, so they may be shown and logged freely. Three practices keep them useful: (1) info-level codes describe **expected** limitations (`maven.profile-unknown`, `gradle.script-heuristic`, `npm.yaml-multi-document`) and are filtered out of the default view; (2) high-volume codes are summarised per unit (Maven and Gradle recorders keep 8 per code and one summary); (3) a diagnostic is a **fact about the repository**, never about the service (no timing, no memory), so a golden file can hold it.

**Logging rules for the engineer** (G-12, GR-13, GR-34, GR-42): none of these packages logs. Should a future change add logging inside a parser it must be at DEBUG, through the root's logger handed in via `Parse`'s context (not today's signature), and must never print file content, a path or a package name: a parser that prints a document on error leaks whatever the document holds.

**Spans.** The root creates `manifests.parse` per file and `manifests.assemble` per ecosystem; nothing inside this unit creates a span. If a profile of a 4,000-package lock ever shows a hot spot, the right instrument is a benchmark (task 7.4), not a span.

---

## 10. Security

The parsers of this unit read **untrusted repository content** and, for Maven, optionally talk (through the root) to a third-party service. The threat model is the one of doc 11 section 10 (hostile content, resource exhaustion, secret leakage, path traversal, privacy of internal coordinates) specialised to six file families. Each row names the mechanism, where it lives and the test that fails if it is removed.

| # | Threat | Where it applies | Mechanism (location) | Test |
|---|---|---|---|---|
| S-1 | XXE and external entities, DTD retrieval, billion laughs | `pom.xml` (the only XML) | stdlib `encoding/xml` Strict: no DTD processing, no external entity support, entities other than the five predefined and numeric ones are syntax errors; only the token API is used (`xmlx.go`); a 1 MiB text cap per element and depth/node budgets. (GR-34) | `TestParserBombs/entity-bomb`, `depth-bomb` |
| S-2 | XML namespace and element injection | POM | elements in a foreign namespace are ignored by every lookup (`xnode.foreign`); only the `http://maven.apache.org/POM/` family is read | golden `foreign-namespace` |
| S-3 | YAML alias bombs and deep documents | `pnpm-lock.yaml`, Berry `yarn.lock`, `pnpm-workspace.yaml` | `yamlx.go` walks `yaml.Node` with a node budget and alias depth 4, at most 4 documents, never decodes into `map[string]any` | `TestAdversarial/yaml-alias-bomb-lock`, `-workspace`, `yaml-depth-bomb` |
| S-4 | TOML nesting and quadratic decode | `pyproject.toml`, `uv.lock`, `poetry.lock`, `pylock.toml`, `Cargo.toml`, `Cargo.lock`, `*.versions.toml` | `tomlx` walks first and refuses key paths over 32 segments and tables over 10,000 keys before `toml.Unmarshal` runs; unstable parser used only through it; array depth bombs end as syntax errors | `toml-key-depth`, `key-depth`, `toml-depth`, `array-depth` cases |
| S-5 | JSON depth and node bombs | `package.json`, `package-lock.json`, `bun.lock`, `Pipfile.lock` | own reader with explicit depth and node budgets, no recursion over untrusted depth beyond the budget | `json-depth-bomb`, `json-node-budget` |
| S-6 | Regular-expression and scanner blow-up | requirements, PEP 508, Gradle, INI | hand-written linear scanners; the few regexps (`pipCompileHeader`, `exactVer`, `ghShort`, the match patterns) are RE2 (Go's `regexp` is linear) | `marker-bomb`, `extras-bomb`, `line-continuations`, `TestLexicalBombs` |
| S-7 | Expansion bombs (properties, Gradle templates) | Maven, Gradle | `expand.Expander`: 1 MiB per value, 8 MiB per unit, 64 MiB per run, depth 16, placeholder 256 bytes, cycle detection | `TestPropertyBomb`, `TestManyBombedPoms`, `TestTemplateBomb` |
| S-8 | Glob and graph blow-up | workspace members (npm, Go, Python, Cargo), Maven parents, includes | `repopath.Match` is a dynamic program over at most 16 pattern segments and 256 bytes; 128 patterns; visited sets and depth caps for parents (32), BOMs (8), includes (8), groups (16) | `TestWorkspaceGlobBounds`, `TestWorkspaceBounds`, `TestChainCycles`, `TestIncludeGraph` |
| S-9 | Path traversal through manifest-controlled paths | `replace => ../..`, `use`, `path =`, `file:`, `-r`, `<relativePath>`, `<module>`, `include`, `from(files())` | `repopath.Rel` and `Join` never return an absolute path; `Join` refuses a result that leaves the repository; parsers emit `..` relative paths for the runner to judge (`assemble.path-escape`); assemblers drop escaping targets with a diagnostic; no file is ever opened (the unit has no `os` import) | `TestBombs/work-escape`, `replace-escape`, `TestPathEscapes`, `TestIncludeEscapes` |
| S-10 | Credentials in manifests | URLs with userinfo, `?token=`, `#token=` fragments, `gradle.properties`, requirements `--index-url`, `.npmrc` | `redact.URL` before any URL is stored (userinfo including a bare token as user name and a scheme-relative `//u:p@h`, credential keys in the query with `&` or `;` separators and in any fragment parameter, VF-12-08); credential-looking property keys redacted and never interpolated; `.npmrc`, `settings.xml`, `.cargo/config.toml` never read; index URLs dropped | `TestSecretsNeverLeak`, python/gradle/cargo `TestSecrets`, `TestNoCredentialsEmitted` |
| S-11 | Hostile text (bidi override, zero-width, C0/C1/DEL controls) in names, versions, messages | every parser | all strings through `manifests.CleanText` (also re-applied by `sanitizeParsed` in the root: defence in depth); invariant 6 of the harness | `TestHostileText`, fuzz |
| S-12 | Identity injection (a name that cannot round-trip a purl, or spoofs another component) | every parser | `coordOK` (Maven, Gradle), `componentOf` segment cleaning (Go), PEP 503 and name validation (Python), name checks (npm, Cargo); the runner drops nameless identities with a diagnostic | invariant 4 of the harness; seeds F-A1..F-A10 |
| S-13 | Privacy of internal coordinates (Maven) | `PomSource` | the five-step rule of 4.5.6; only a coordinate is ever sent; the answer is parsed as untrusted XML with the default limits; source off by default | `TestPrivateGuard` |
| S-14 | Parsing before checking | all | order is size, encoding, then decode (GR-20, GR-34); third-party decoders (`modfile`, `go-toml`, `yaml.v3`, `encoding/xml`) are reached only through code that already bounded the input | `TestBombs/size`, `nul`, `utf16` cases |
| S-15 | Hostile or oversized diagnostics | every code | message is a fixed sentence plus at most one cleaned, clipped token; the root caps 100 per file | `CheckParse` invariant 7 |
| S-16 | Supply chain of this unit's own dependencies | build | three direct third-party modules (`x/mod`, `go-toml/v2`, `go.yaml.in/yaml/v3`) plus `x/text` through the root; versions verified on 2026-10-14 (appendix A); `go.sum` pinned; `govulncheck` in the root's CI (not run here) | n/a |
| S-17 | Algorithmic complexity (CPU exhaustion by a large but well-formed file) | every parser and assembler | linear-time algorithms only; `tomlx` takes line numbers from a newline table and refuses a table of more than 10,000 keys (go-toml's duplicate-key check is quadratic per table); `pyproject` element lines from a one-pass index; the scale test holds 18 shapes to 3 s (E-A8) | `TestInputsAtScaleStayLinear`, `TestDecodeIsLinearInTheNumberOfTables`, `TestKeysPerTableCap`, `TestPyprojectLocatorIsLinear` |
| S-18 | Hostile coordinates reaching a third party (Maven) | `PomSource` | `safeCoordinate` (valid Maven identifier alphabets only, no `.`/`..` segment) before the guard and before any call | `TestPomSourceCoordinatesAreSafe` |

**What is deliberately not defended against**, with the reason: a repository that is *legitimately* enormous (the root's `max_files`, byte and node limits decide); a manifest that **lies** (a `package.json` naming someone else's package: the unit reports what the file says, and the organisation graph labels edges by evidence channel, doc 11 4.8); malicious *version strings* reaching a downstream consumer (the SPA renders text only; CycloneDX export escapes it).

---
## 11. Guardrail and improvement trace

Manifest parsing is a first-class feature by owner directive D-15 (`04-decisions.md`; N-01, Q-14 answered): the whole unit exists because the repository dependency panel, the organisation graph, the API, CycloneDX, badges and the overview consume its output. This section is mostly a **prevention** trace: every guardrail of `02-design-guardrails.md` that applies to parsing untrusted manifests, mapped to the mechanism that enforces it and the test that fails if the mechanism is removed; then the improvements D-15 asks for, mapped the same way.

### 11.1 Guardrail trace

| Source | Hazard the guardrail prevents | Verdict | Mechanism in this unit | Test |
|---|---|---|---|---|
| **GR-04** nothing exists without a consumer | a per-ecosystem cache that is filled and read by nothing but health counters | prevented | no per-ecosystem cache exists; this unit's output is consumed by the root for the eight API operations, the graph, the text routes, CycloneDX, badges and overview; a value nobody reads is a failing review of unit 11, not a cache | `TestRealRunnerMatchesRunTree` (the real refresh job publishes what the harness computed) |
| **GR-34** untrusted documents are parsed safely and within limits | an XML parser with a default factory (DTDs, external entities, unbounded expansion: XXE and billion laughs) and a document logged whole on failure | prevented | S-1, S-2: stdlib `encoding/xml` Strict, token API only, no entities beyond the predefined five, depth and node budgets, 1 MiB text cap, prefix-aware namespaces; diagnostics carry a code and at most one quoted token; the packages log nothing (section 9). Applied as stated: "prefer JSON; if XML use `encoding/xml` (no entity resolution), size caps"; Maven is the only XML format of this unit, every other file is JSON, TOML, YAML or text | `TestParserBombs`, golden `foreign-namespace`, `TestNoCredentialsEmitted` |
| **Whole-model parsing** (GR-34, GR-29) | a reader of the root fields only (`groupId`, `artifactId`, `version`, `packaging`, `name`, `description`, `parent`, `modules`) sees no dependencies, properties, management or profiles; a POM with `${revision}` or an inherited `groupId` yields empty or wrong coordinates | applied | raw model of the whole dependency-relevant POM (4.5.2), effective model in the assembler (4.5.6): inheritance, interpolation, management, BOM, profiles | goldens `reactor-inheritance`, `ci-friendly`, `bom-local`, `profiles`, `real-*` |
| **Namespace-aware reading** (GR-34) | element names matched without namespace: an element `<x:dependency xmlns:x="...">` is indistinguishable from the real one | applied | namespace-aware by prefix; foreign elements ignored in lookups | golden `foreign-namespace` |
| **GR-10** Git refs are resolved from the repository | a configured branch name used as the Git ref for a manifest fetch reads a branch that is not the default | out of this unit's reach by design | parsers receive bytes and a path; **which commit** is read is the root's acquisition by tree SHA (doc 11 4.3). No parser or assembler takes a ref, a branch or a URL | `FuzzTree`, `TestRealRunnerMatchesRunTree` (same bytes, same facts) |
| **GR-09** matchers are typed and anchored | a glob evaluated as an unanchored regex (`IoT*` matching any name containing "Io") | prevented in the one place this unit matches globs | `repopath.Match`: `path.Match` segments plus `**`, anchored, polynomial; unmatched patterns are reported (`npm.workspace-glob-unmatched`, `maven.module-missing`, `python.workspace-member-missing`, `golang.use-missing`) instead of silently matching nothing | `repopath_test.go`, `TestWorkspaceGlobBounds` |
| **GR-20** limit, verify, then parse | decoding an input before its size is checked; logging untrusted bodies | prevented | S-14: size, encoding, then decode; nothing logs file content | `TestBombs` (size cases) in every package |
| **GR-13** secrets are a type and never leave headers | credentials kept as strings, in URLs and in logs | prevented | S-10: `redact.URL` on every stored URL, credential-looking properties redacted, no logging | `TestSecrets` and variants |
| **GR-24** the job engine never loses work silently | permanent failures (a malformed POM) treated as errors and retried identically by a worker queue | prevented | the never-fail rule (3.1): a parse problem is a diagnostic in a cached `Parsed` keyed by `(blobSHA, parserID, parserVersion)`, never an error to retry; `Parse` returns an error only for `ctx.Err()` and such a result is never cached | every `TestBombs`-family test (a malformed input yields a diagnostic and no error), `CheckParse` invariant 7 |
| **GR-16** truthful types and charsets | platform-default charsets; text decoded before the parser sees bytes | prevented | 4.1.3: bytes in, BOM stripped, UTF-16 and NUL rejected, XML prolog charset `iso-8859-1`/`windows-1252` through `limits.CharsetReader`, nothing guessed | golden `latin1`, `utf16-bom`, `utf16-declared`, `unknown-charset`, `nul` |
| **GR-30** retention is bounded and measured | unbounded growth of collections derived from untrusted input | prevented | every collection is bounded: diagnostics per code (8), per file (100, root), workspace patterns (128), TOML key path (32), lines of a requirements file (200,000), YAML documents (4); the root bounds deps, locked components, units and facts bytes | `TestBombs`, `TestAdversarial` |
| **GR-03** dependencies are explicit | global mutable singletons, static engines and clients | prevented | no package-level mutable state, no `init()` (A-18); constructors return stateless values | purity guard of `apply.sh`, `-race` |
| **GR-22 / GR-23** bounded by time; failures are typed | swallowed cancellation, every failure collapsed into one generic error, "not found, succeed" | prevented | `Parse` and `Assemble` return an error only for `ctx.Err()` (the Maven assembler checks after its last unit too, rule 6 of 4.5.6); every other failure is a typed diagnostic; the `PomSource` outcome is classified (`parent-fetched`, `pom-source-failed`, `parent-unresolved` with reason, `snapshot-private-skipped`) instead of one "not found" (a transient failure is still not distinguished from a definitive miss, L-11) | `TestPrivateGuard`, `TestAssembleReturnsContextErrorAfterAFetchTimeout`, goldens `real-*` |
| **G-12** structured logs without content (GR-42) | | applied | none of these packages logs (section 9) | purity guard |
| **G-13** recorded-fixture harness, fake GitHub (GR-43) | | applied | real files committed by blob id (tier 1) or fetched by blob id and verified (tier 2, `fetch-oracle.sh`); `TestRealRunnerMatchesRunTree` runs the real refresh job over a fake GitHub (the core scaffold's) | section 12 |
| **A-15** go-toml/v2 as the TOML library | | applied through one package | `tomlx` (D-3) | `tomlx_test.go` |
| **A-18** no `init()` | | applied | purity guard | |
| **A-26** encoding policy (UTF-8 BOM stripped, UTF-16 rejected, XML `iso-8859-1` and `windows-1252` through `x/text/charmap`; GR-16) | | applied | 4.1.3; `limits.Input`, `limits.CharsetReader` | `encoding.unsupported` cases, golden `latin1` |

### 11.2 Improvement trace (new capabilities of D-15 mapped to mechanisms)

| Capability | Mechanism | Evidence |
|---|---|---|
| Dependencies for the six tier-0 ecosystems into one model | six `Register` functions, 27 parsers, six assemblers | 44 golden trees, 281 test executions in the six packages |
| Direct versus transitive, per scope, with the exact locked version | lock join per ecosystem; `LockedComponent.Relationship` and `Scope` | oracle counts equal independent counts for 11 real locks (appendix B) |
| Monorepo awareness (workspaces) | npm workspaces and catalogs, Go `go.work`, Cargo workspaces and inheritance, uv workspaces, Maven reactors, Gradle multi-project builds | goldens `npm-v3-workspaces`, `pnpm-catalog`, `workspace` (Go, Cargo), `uv-workspace`, `reactor-inheritance`, `kotlin-multiproject` |
| Internal edges for the organisation graph | `SourcePath` and `Provides`: the graph builder links a dependency whose `Target` equals another unit's artifact (channel A) or whose path resolves to another unit | Gradle `project(":x")` with a group, npm `workspace:`, Go `use`, Cargo `path` |
| Honest confidence | `ConfExact` versus `ConfHeuristic`; diagnostics for everything not evaluated | 4.1.1, 4.6.5, section 4.8 |
| Version and licence views | `Version`, `Resolution`, declared `Licenses`, lock `Integrity` | goldens |
| Safe on hostile repositories | section 10 | 7 adversarial test files, 23 fuzz seeds |
| Maven parents and BOMs outside the organisation without leaking internal coordinates | opt-in `PomSource` behind the privacy guard | `TestPrivateGuard`, goldens `real-*` with `pomsource/` |
| Searchable diagnostics reference | `EcosystemSpec.Diagnostics` (RR-A1) | 92 codes |

---

## 12. Test plan

All tests below exist in the assets and run under `go test -race -count=1 ./internal/manifests/...` (281 passing test and subtest executions in the six ecosystem packages, 353 with the `integration` package, 484 in `./internal/manifests/...` including the core and the helpers, counted by the verifier on 2026-10-14 with `go test -count=1 -v | grep -c -- "--- PASS"`; the first revision said 323 for the six packages, a figure the verifier could not reproduce by any counting rule). Per-test details are in the code; this section states the **plan**: layers, what each guarantees, the fixtures and the acceptance criteria that gate a merge.

### 12.1 Layers

| Layer | What | Where | Per package |
|---|---|---|---|
| L1 table tests | one input, one expectation, typed diagnostics: `TestBombs`/`TestAdversarial`/`TestParserBombs`/`TestLexicalBombs` (size, NUL, UTF-16, invalid UTF-8, depth, node budget, syntax, truncated, trailing garbage), include and workspace bounds, cycles, escapes, secrets | `*_test.go` next to the code | all six; helper packages have their own (`expand_test.go`, `linex_test.go`, `redact_test.go`, `repopath_test.go`, `tomlx_test.go`) |
| L2 golden trees | `TreeGolden(t, reg, "testdata/<case>")`: loads `input/` as a repository tree, runs `RunTree` **three times** (determinism), compares parsed files, units, diagnostics with `expected.golden.json`; update with `-update-tree`; every golden is also checked by `CheckTree` (invariants 1 to 8 and the assembler invariants) | `testdata/*/` | golang 4, npm 10, python 7, maven 12, gradle 7, cargo 4 = 44 |
| L3 fuzz | `FuzzParsers` (corpus: every file of every golden input as a seed, mutated; every parser that matches the name; `CheckParse` after each call) and `FuzzTree` (one-file trees through `RunTree` twice: parser, runner and assembler together) | `testdata/fuzz/FuzzParsers/` keeps 23 regression seeds (golang 3, npm 6, python 5, maven 1, gradle 5, cargo 3) | all six |
| L4 property | the eight invariants of 4.1.9 as `CheckParse` and `CheckTree` (no panic, bounded, deterministic, purl round trip, locations in range, clean text, declared codes only, path independence); `TestOrderIndependent` (assembly order independence); `TestSanitizeKeepsForeignEcosystem` (RR-A3) | `parsertest/tree.go`, `maven`, root overlay | all |
| L5 race | `-race` over every layer; assemblers and parsers run in parallel from the core's own tests | CI | all |
| L6 integration | `TestRealRunnerMatchesRunTree`: every golden tree through the **real** `manifests.refresh` job with the test's own fake GitHub (`newEnv`: trees, blobs, call counters) and the core scaffold's store, same units, counts and diagnostics as `RunTree`; `TestUnchangedTreeCostsNothing` (a second refresh whose HEAD tree id equals the stored facts' tree makes no tree call and no blob call); `TestLockOnlyDirectoriesStillShow` (lock-only trees for npm, cargo, python, gradle, golang yield units through default assembly); `surface_test.go` (compile-time check of every exported signature of 2.2) | `ecosystems/integration/` | all |
| L7 oracle (tier 2) | `oracle/fetch-oracle.sh` downloads the 21 tier-2 files (and, to refresh them, the 21 committed ones) by git blob id (GitHub blob API, `curl` for Maven Central) and verifies `git hash-object`; `tools/corpus-main.go.txt` runs `RunTree` on 15 trees and prints files, units, dependencies, locked components, best-of-5 time and diagnostics; the locked counts are compared with **independent counts** made with ad hoc scripts over the same files (not with this unit's code) | `oracle/` (never committed) | all |
| L7b scale | `TestInputsAtScaleStayLinear`: 18 shapes (every lockfile format at 2 MiB, every manifest format at 512 KiB, the two worst shapes of the verification: one huge TOML table and a single-line array) through `RunTree`, 3 s each under `-race` | `ecosystems/integration/scale_test.go` | all six |
| L8 benchmarks | `go test -bench` for the four largest inputs (task 7.4); not a merge gate, a regression signal | `*_bench_test.go` (to write, task 7.4) | npm, python, maven, gradle |

`testing/synctest` is **not used**: no code of this unit reads a clock, starts a timer or waits (section 8), so there is nothing to virtualise; the time-bound tests (`d > 3*time.Second`) measure real elapsed time with generous bounds and run in milliseconds. The root's watchdog tests, which do need virtual time, are doc 11's.

### 12.2 Fixtures and the fake GitHub

- **Tier 1** (committed, 44 trees): the synthetic cases are written for this unit; the real cases are Apache-2.0 files with headers intact (apache/maven and apache/commons-lang POMs, square/okhttp, ktorio/ktor and android/nowinandroid Gradle files, and eight POMs from Maven Central under `pomsource/`). `SOURCES.tsv` lists each file with repository, blob id, bytes, licence and whether the blob was still the head on 2026-10-14. The largest golden is 153 KB (D-8). Goldens are produced by the code and **reviewed**, not written by hand: a change of a golden in a pull request is a change of the contract and needs a line in the PR description saying why.
- **Tier 2** (never committed): the 21 files of `SOURCES.tsv` under `t/` (the 1.9 MB pnpm lock and the other large locks, Cargo, Go, Python manifests). The licences are mixed (MIT, BSD, Apache, Artistic-2.0, Unlicense OR MIT), several are not Apache-2.0, and size does not belong in the repository. Fetch with `sh oracle/fetch-oracle.sh <dir>` (needs `gh` and `curl`; verifies every blob id; fails on a mismatch).
- **Fake GitHub:** `TestRealRunnerMatchesRunTree` runs the real refresh job against a fake GitHub defined in the test (`newEnv`), which serves trees and blobs from the golden `input/` directories and counts calls. Nothing in this unit talks to GitHub.
- **Fake `PomSource`:** `dirSource` in `maven_test.go` serves `testdata/<case>/pomsource/<group>_<artifact>_<version>.pom` and records every call (`TestPrivateGuard` asserts on the recorded coordinates).

### 12.3 Test catalogue (by risk, with the case names)

| Risk | Tests (package) |
|---|---|
| Limits: size, encoding, depth, nodes, expansion | `TestBombs` (golang, python, cargo), `TestAdversarial` (npm: 16 cases), `TestParserBombs` (maven: 11), `TestLexicalBombs` (gradle: 13), `TestPropertyBomb`, `TestManyBombedPoms`, `TestTemplateBomb`, helper tests of `expand`, `tomlx` |
| Cycles and graph bounds | `TestLockCycles`, `TestChainCycles`, `TestIncludeGraph`, group cycle in golden `pep621`, Maven BOM cycles |
| Path safety | `TestBombs/work-escape`, `replace-escape` (golang), `TestPathEscapes` (maven), `TestIncludeEscapes` (gradle), `TestWorkspaceBounds` (cargo), `TestWorkspaceGlobBounds` (npm), `assemble.path-escape` in goldens |
| Secrets | `TestSecretsNeverLeak` (npm), `TestSecrets` (python, gradle, cargo), `TestNoCredentialsEmitted` (maven), `redact_test.go` |
| Hostile text and identities | `TestHostileText` (npm), fuzz seeds F-A1..F-A10, invariants 4 and 6 |
| Privacy of the `PomSource` | `TestPrivateGuard` |
| Determinism and order independence | `TreeGolden` three runs, `TestOrderIndependent`, invariant 3 |
| Real-world correctness | goldens `real-*`, the tier 2 oracle with independent counts |
| Algorithmic cost | `TestInputsAtScaleStayLinear` (integration, 18 shapes), `TestDecodeIsLinearInTheNumberOfTables`, `TestKeysPerTableCap` (tomlx), `TestPyprojectLocatorIsLinear` (python) |
| Third-party exposure (Maven) | `TestPomSourceCoordinatesAreSafe`, `TestPomSourceEnabledChangesTheRegistryHash`, `TestAssembleReturnsContextErrorAfterAFetchTimeout` |
| Cargo inheritance by key | golden `workspace-rename` |
| Redaction | `redact_test.go` (scheme-relative userinfo, `;` separators, fragment parameters), `TestExactPin` |
| Root integration | `ecosystems/integration/*`, `sanitize_foreign_test.go` |

### 12.4 Acceptance criteria (a merge is blocked unless all hold)

1. `gofmt -l` empty, `go vet ./...` clean, the purity guard passes, `go test -race -count=1 ./internal/manifests/...` green on Go 1.27 (compile-checked on 1.27.1).
2. Every golden tree passes `CheckTree`; no golden changed without a recorded reason; every diagnostic code emitted anywhere is in the catalogue of its package (invariant 7).
3. Every fuzz target runs 60 s per package in CI (nightly: 10 minutes) without a finding; every finding becomes a seed under `testdata/fuzz/FuzzParsers/` with the fix, in the same change (the 23 seeds are the record).
4. Tier 2 oracle (a non-gating nightly job): all 15 trees parse with no error-severity diagnostic and locked counts equal the independent counts of appendix B; best-of-5 times stay within 3 times the figures of appendix B on the CI runner class (the figures are 0.2 to 61 ms).
5. Allocation volume of the largest corpus parse (the 1.98 MB vercel/ai pnpm lock) stays under 64 MiB (22.1 bytes per input byte measured on pnpm gives about 44 MB; retained heap is at most 2.8 bytes per input byte, E-A5).
6. No parser or assembler imports a package outside the allowed table of 2.1 (`go list` check in CI).
7. `docs/reference/diagnostics.md` regenerated from `Spec().Diagnostics` has no code without a summary and no duplicate.
8. `TestInputsAtScaleStayLinear` is green under `-race` (3 s per shape), and the nightly oracle job runs the 8 MiB originals of E-A8 (each under 1 s, none above 3 s); a new parser or assembler adds a shape to the scale test in the same change.
9. Coverage of the six ecosystem packages by statement is reported, with no gate (the fuzz seeds and goldens drive it): the engineer reads uncovered branches in review.

---

## 13. Implementation tasks

Milestones are ordered by dependency; sizes are S (under a day), M (one to three days), L (three to five days) for one engineer who has read this document. Every task ends in a green `apply.sh`-equivalent run. The assets already contain a complete working implementation of every task from M1 on; the engineer **ports it into the repository, reviews it line by line and owns it**, which is why the sizes are small. Dependencies on other units are named; an engineer is never blocked on a unit that is not listed.

| Id | Task | Size | Depends on | Done when |
|---|---|---|---|---|
| **M0 Core adoption (unit 11 owner)** | | | | |
| 0.1 | Apply `patches/core-rr-a.diff`: `DiagnosticDoc` and `EcosystemSpec.Diagnostics` (RR-A1), foreign-ecosystem identity rule in `sanitizeParsed` (RR-A3), `LockedComponent.Display` (RR-A6), `Dependency.Clone` (RR-A5), `stampPaths` (RR-A7) | S | unit 11 | the 97 core tests and `TestSanitizeKeepsForeignEcosystem` pass |
| 0.2 | Add `manifests/tree.go` (`BlobSHA`, `TreeResult`, `RunTree`, `assembleChecked`) and `parsertest/tree.go` (RR-A2, RR-A4); decide OQ-12 (production file or build-tagged) | M | 0.1 | goldens of the core run through `TreeGolden` |
| 0.3 (**M0-1**) | Fix `defaultAssemble` to clone dependencies before stamping `LocPath` (RR-A5) | S | 0.1 | `FuzzTree` finds no `assemble.mutated-input` |
| 0.4 (**M0-2**) | Replace `GlobMatch` with the dynamic program of `repopath.Match` (RR-A8) | S | | a `*/**/` x16 pattern over 30 segments matches in under 10 ms |
| 0.5 | Add `go-toml/v2` v2.4.3 and `golang.org/x/mod` v0.42.0 as **direct** requirements to `go.mod` (`patches/go-mod.diff`; the first revision marked them `// indirect`, which `go mod tidy -diff` rejects, VF-12-14); confirm `go.yaml.in/yaml/v3` v3.0.5 | S | | `go mod tidy -diff` clean (it is a line of `apply.sh`) |
| 0.6 | Contract text: `EcosystemId` description lists `gradle` (OQ-8); diagnostics reference generator reads `Spec().Diagnostics` | S | unit 11 | `docs/reference` regenerated |
| **M1 Helpers and harness** | | | | |
| 1.1 | `internal/{linex,repopath,redact}` with tests | S | 0.4 | their tests green |
| 1.2 | `internal/expand` (budgets, cycles) | S | | `expand_test.go` green, `TestPropertyBomb` later |
| 1.3 | `internal/tomlx` (walk first, newline-table line index, key-depth cap, per-table key cap, accessors; **never** `unstable.Parser.Shape` in a loop) | M | 0.5 | `tomlx_test.go` green incl. `TestDecodeIsLinearInTheNumberOfTables` and `TestKeysPerTableCap`; `FuzzDecode` 60 s |
| 1.4 | the purity guard in CI (the `grep` of `apply.sh`) and the import-table check (`go list`) | S | | both fail on a planted violation |
| **M2 Go and Cargo** | | | | |
| 2.1 | `ecosystems/golang`: parsers, assembler, catalogue, goldens, `TestBombs`, fuzz seeds | M | M1 | acceptance 1-3 for the package; k8s oracle 114/198 |
| 2.2 | `ecosystems/cargo`: manifest and lock parsers, workspace inheritance, reachability scopes, goldens | L | M1 | ripgrep oracle 52 locked |
| 2.3 | `integration` skeleton: `TestRealRunnerMatchesRunTree`, `surface_test.go`, `TestLockOnlyDirectoriesStillShow` for these two, `TestInputsAtScaleStayLinear` (a shape per ecosystem added with that ecosystem); the package needs its archtest row first (D-12) | M | 0.2, D-12 | green |
| **M3 npm** | | | | |
| 3.1 | `jsonx.go`, `packagejson.go`, `packagelock.go` (v1 to v3) | M | M1 | goldens `package-json`, `npm-v1`, `npm-v3-workspaces` |
| 3.2 | `yamlx.go` (bounded YAML walker), `pnpm.go`, `yarn.go` (v1 and Berry), `bun.go` | L | OQ-6 outcome (reuse unit 13's `yamlsafe` when it exists) | goldens `pnpm-*`, `yarn-*`, `bun-*`; vue 634, ai 4,061, berry 2,050 |
| 3.3 | npm assembler: workspace roots, catalogs, `workspace:`, lock choice and join, `finalizeRoots` | L | 3.1, 3.2 | `multi-lock`, `pnpm-catalog`, `yarn1-workspaces`; react 2,388 |
| **M4 Python** | | | | |
| 4.1 | `pep508.go` (scanner), `pyproject.go`, `setupcfg.go`, `pipfile.go` | M | M1 | goldens `pep621`, `poetry*`, `setupcfg`, `pipenv` |
| 4.2 | `requirements.go` (logical lines, includes, hashes, pip-compile detection), `locks.go` (poetry, uv, pylock, Pipfile.lock) | L | 4.1 | golden `requirements`; flask 78, poetry 78, uv 95 |
| 4.3 | python assembler: includes, constraints, uv workspaces, source resolution, lock join | L | 4.1, 4.2 | golden `uv-workspace` |
| **M5 Maven** | | | | |
| 5.1 | `xmlx.go` (token reader, namespaces, charsets), `pom.go`, `maven.go` (+ `maven.config`) | M | M1 | goldens `single-module`, `foreign-namespace`, `latin1`; `TestParserBombs` |
| 5.2 | assembler: parent resolution, effective model, management, BOM, profiles, CI-friendly, reactor | L | 5.1 | goldens `reactor-inheritance`, `ci-friendly`, `bom-local`, `profiles`; `TestChainCycles`, `TestOrderIndependent` |
| 5.3 | `PomSource` wiring: `WithPomSource`, privacy rule, budgets; `cmd/scp` constructs the hardened client (`platform/netguard`) behind `manifests.pom_source.*` | M | 5.2; unit 11 `NewPrivacyGuard`; cmd/scp owner | `TestPrivateGuard`, goldens `real-*` with `pomsource/` |
| **M6 Gradle** | | | | |
| 6.1 | `lex.go`, `scan.go`, `script.go`, `settings.go`, `catalog.go`, `lockfile.go`, `gradle.go` (properties) | L | M1 | `TestLexicalBombs`; goldens `groovy-single`, `dynamic-and-vars` |
| 6.2 | assembler: builds, includes, catalogs, project resolution, plugins, constraints, lock join, toolchain, unreferenced catalog entries | L | 6.1 | goldens `kotlin-multiproject`, `lockfiles`, `real-*` |
| **M7 Integration, oracle, hardening** | | | | |
| 7.1 | `cmd/scp` composition lines of 2.2 and the Maven options | S | M2 to M6; cmd/scp owner | a repository with one file of each ecosystem shows six units end to end |
| 7.2 | oracle scripts in CI (nightly), `fetch-oracle.sh`, corpus tool | S | | acceptance 4 |
| 7.3 | fuzz in CI (60 s per package per PR, 10 minutes nightly), seeds committed | S | | acceptance 3 |
| 7.4 | benchmarks for pnpm lock, Berry lock, uv lock, Maven POM chain, Gradle catalog | S | | numbers recorded in the repository docs |
| 7.5 | diagnostics reference page and the operator note "how to read parsing notes", including the L-12 warning (set `private_group_prefixes` before enabling `pom_source`) | S | 0.6 | docs build |
| 7.6 | `cmd/scp` `PomSource` satisfier escapes every URL path segment of a coordinate (defence in depth behind `safeCoordinate`) and keeps its own size cap and permanent cache for releases | S | 5.3 | satisfier test with `..`, `%2e`, `?` coordinates |

Critical path: 0.1 to 0.2 to 2.3 (the harness) and then the ecosystems in any order; Python, npm and Gradle are the largest. Nothing in this unit blocks on units 05 (GitHub adapter), 03 (store) or 13 (sibling), except OQ-6 (an optional reuse).

---

## 14. Deviations proposed

Each deviation names the architecture or core text it departs from, the reason, the cost of staying and the action requested of the owner. None changes a package name, a dependency direction (rank order) or the composition root.

| Id | Deviation | From | Reason | Decision requested |
|---|---|---|---|---|
| D-1 | Adopt the core changes RR-A1 to RR-A8 (4.1 and 2.3): `DiagnosticDoc`, `RunTree`, foreign-ecosystem rule, `Clone`, `Display`, `stampPaths`, polynomial glob | doc 11 3.x, 4.x | each was found by a test of this unit (golden of Gradle, assembler fuzzer, GlobMatch timing); patch included and green | unit 11 owner: accept and merge (M0) |
| D-2 | Do not use `JoinLock`, `ExpandMembers`, `GlobMatch` | doc 11 2.5, OQ-6 of doc 11 | richer per-ecosystem joins (per-importer versions, aliases, reachability scopes, integrity) and member rules (negation, exclude, bounded) than a generic helper offers; no behaviour is lost; the core helpers remain for unit 13 | accept; the core may keep the helpers |
| D-3 | Reach `go-toml/v2` only through `internal/tomlx` (rank 1), using its `unstable` parser, instead of `toml.Unmarshal` in each package | architecture 5.2 row of ecosystems lists the library per package | line numbers, a key-depth cap and a per-table key cap are impossible with `Unmarshal` alone (the decoder is quadratic in the keys of one table, E-A8; the 37 s figure for a deep header of the first revision was not reproducible, E-A4) | accept; OQ-4 tracks the `unstable` API |
| D-4 | Five new packages under `manifests/internal/` (`tomlx` rank 1, `linex` rank 1, `repopath` rank 1, `expand` rank **2**, `redact` rank **2**; the last two import `limits`, so no equal-rank edge exists and the rule of 5.1 needs no exception: revised by the verifier, VF-12-11, the first revision proposed two rank-1 to rank-1 edges) | architecture 5.2 (one row `manifests/internal/{purl,version,limits}`; closed-world archtest needs a row per package) | shared by two to six ecosystem packages; keeps third-party types out of the ecosystems and gives one tested place for each concern | accept five new rows in the matrix (`tomlx`: third-party `go-toml/v2`; `expand`, `redact`: `manifests/internal/limits`; `linex`, `repopath`: none) |
| D-5 | New ecosystem id `gradle` (units are `gradle`, targets are `maven`) with `PURLType` `maven` | `EcosystemId` closed-looking list in the API contract text | a Gradle build and a Maven build in one repository are two units; scope and configuration vocabularies differ; purl type does not | document `gradle` in the contract (OQ-8) |
| D-6 | Parser ids `pypi/*` instead of the research table's `python/*` | research 07 section 5.3 | registry rule `<EcosystemID>/<format>` and the ecosystem id `pypi` | none (rule wins) |
| D-7 | npm uses its own bounded YAML walker (`yamlx.go`) | unit 13 proposes `manifests/internal/yamlsafe` | unit 13's document did not exist when this unit was built; the two overlap | converge in one package (OQ-6) |
| D-8 | Golden form: tree directories with `expected.golden.json`, `-update-tree`, real inputs above 64 KiB (up to 97 KB POMs, 153 KB golden) | doc 11 12.4 (single-file goldens, inputs at most 64 KiB) | workspaces, parent chains and lock joins are multi-file by nature; real BOMs are large | accept for tree goldens; single-file `Golden` stays for unit 13 |
| D-9 | Assembler expansion and structural budgets are constants (1 MiB, 8 MiB, 64 MiB; depths 32, 8, 8; 64 fetches), not `Limits` fields | doc 11 6 (`max_expand`, `max_expand_file`) | `Assemble` has no `Limits` parameter | accept constants (4.1.7) or add `Limits` to `Assemble` (OQ-5) |
| D-10 | Python exports `NormalizeName`, `Requirement`, `ParseRequirement`, `ExactPin` | "ecosystem packages export `Spec`, `Register`, constructors only" | used by tests of this package and handy for unit 13's `uv` tooling; no consumer outside the package yet | keep until OQ-9 is decided, then make internal |
| D-11 | `Kinded` is implemented by every lockfile parser, the workspace parsers and `bun.lockb` | doc 11 2.5 (optional) | selects the lockfile size limit and the kind before parsing | none |
| D-12 | Matrix rows still missing (VF-12-12, VF-12-13): (a) `manifests/ecosystems/integration` (test-only; imports every ecosystem, `manifests`, `parsertest`, `scm`, `freshness`, `job`, `readmodel`, `store`, otel `noop`), proposed as a rank-6 row of the same kind as `cmd/scp` because rule 5.1.3 forbids an adapter importing another adapter and composition belongs to `cmd/scp`; the alternative is moving the three test files to `cmd/scp` (the testdata path in `scale_test.go` and `integration_test.go` changes, nothing else); (b) `go.yaml.in/yaml/v3` in the third-party column of the `manifests/ecosystems/*` row (architecture 13.1 and ADR-021 already say so; only the table omits it) | architecture 5.2, 5.1 rule 3 | without a row the closed-world `archtest` fails on the first CI run | accept the row (default) or relocate the tests |
| D-13 | `go.sum` is **not** parsed although D-15 lists it among the Go inputs (`go.mod`, `go.sum`, `go.work`) | 04 D-15 ecosystems | `go.sum` holds hashes of module versions, adds no edge that `require` lacks (4.2.3, V-1) and no scope; parsing it would double the Go locked set with entries that were never selected | accept; revisit only if an integrity (`h1:`) column is wanted |
| D-14 | The Maven POM model and effective-model assembler are built in-house on `encoding/xml` tokens; `deps.dev/util/maven`, which ADR-021 lists among the adopted leaf libraries ("POM model and version ordering"), is **not** used (`deps.dev/util/semver` is, through `manifests/internal/version`) | ADR-021 Decision, adopt list | architecture 13.2 requires "XML via `encoding/xml`" with token-loop depth counting, an own expansion bound (E5: 160 MB from 700 bytes in the adopted library), `PomSource` privacy and namespace-aware lookups; an adopted model would have to be wrapped so thinly that nothing is left of it | accept; ADR-021 should say "POM model: built; version ordering: adopted" |

---

## 15. Open questions

| Id | Question | Owner | Default if unanswered |
|---|---|---|---|
| OQ-1 | Should the core give an ecosystem assembler a hook for **files it did not place** (a lock beside no manifest in a directory that has other projects)? Today default assembly runs only when the assembler returns nil (L-1) | unit 11 | keep: such a lock is parsed and cached but contributes no locked components |
| OQ-2 | Python `-r`/`-c` includes outside the fetched set: should acquisition fetch them by path on demand (`python.include-not-candidate`)? It needs a request from the assembler back to the acquirer, which the pure-assembler rule forbids | unit 11 | no; the diagnostic stays |
| OQ-3 | `Weigher`: keep the default amplification of 10 although YAML allocates up to 28.6 bytes per input byte (Berry, E-A5)? Retained heap is 0.6 to 2.8 | unit 11 | keep 10; revisit after the first production profile |
| OQ-4 | `go-toml/v2` `unstable` is documented as not covered by compatibility guarantees. Pin the minor version and rely on `tomlx_test.go`, or vendor the 300 lines of walker logic? | unit 12 | pin; the test fails on an API change |
| OQ-5 | Add a `Limits` parameter to `Assembler.Assemble` so `max_expand*` apply to assemblers (D-9)? | unit 11 | constants equal to the defaults |
| OQ-6 | Converge `npm/yamlx.go` and unit 13's `yamlsafe` into one rank-1 package with the same budgets (documents 4, alias depth 4, node budget) | units 12 and 13 | each keeps its own until both exist; then merge |
| OQ-7 | Maven namespace policy: a POM without a namespace and one in `http://maven.apache.org/POM/4.0.0` or `4.1.0` are read; elements in any other namespace are ignored, and a root element in a foreign namespace is `maven.not-a-project`. Should a namespace-less POM carry an info diagnostic (Maven itself accepts it)? | unit 12 | no diagnostic |
| OQ-8 | The API contract's `EcosystemId` description ("equals purl type where one exists") should name `gradle` as the one id without a purl type | unit 11 (contract) | add the sentence (task 0.6) |
| OQ-9 | Make the Python helpers (`NormalizeName`, `ParseRequirement`, `ExactPin`) `internal/` or keep them exported (D-10)? | unit 12 | internal once nothing outside uses them |
| OQ-10 | `bun.lockb`: stay diagnosed-only, or ship a reader of the binary layout (undocumented, version dependent, risky)? | unit 12 | diagnosed only; Bun has had text `bun.lock` since 1.2 |
| OQ-11 | Gradle `apply from:` and convention plugins: follow `apply from` among fetched scripts (cheap, local) or leave `gradle.apply-from` as the answer? | unit 12 | leave; revisit with real-world false negatives |
| OQ-12 | `manifests/tree.go` exports test-harness API (`RunTree`) from the production package because it needs unexported runner functions. Keep it in the production build, or put it behind a build tag or an `internal/manifests/manifeststest` package that the root exposes through an `export_test.go`-style bridge? | unit 11 | production file, documented as test support |
| OQ-13 | Licences are declared-only. Should the Maven `<licenses><license><url>` and `package.json` `license` file references be kept for a later detector? | unit 12 with provider owners | names only (v1) |
| OQ-14 | RR-A9: may unit 11 (1) keep `Guard().Allow` false until the first complete graph and (2) re-assemble repositories whose Maven facts carry a transient-looking `maven.parent-unresolved` on the idle back-off while `pom_source` is enabled? | unit 11 | no: L-11 and L-12 stay, with the operator note of task 7.5 (set `private_group_prefixes` first) |

---
## Appendix A. Verification log (all on 2026-10-14)

Libraries were checked against `proxy.golang.org` (latest tagged version and release date) and by compiling against them; standards and tool documentation were fetched from the sources named. "Fetched" means the page was read on that date; "not verified" is stated where a fetch failed.

| Id | Fact | Source and result |
|---|---|---|
| V-1 | At `go 1.17` or later `go.mod` lists a `require` for every module that supplies a package transitively imported by the main module (the file is its own lock); at `go 1.16` and lower an indirect dependency is listed only if MVS would otherwise select a different version; `// indirect` means no package of the module is imported directly; `tool` directive since Go 1.24 | go.dev/ref/mod (graph pruning, `require`, `tool`), fetched |
| V-2 | `golang.org/x/mod` v0.42.0 provides `modfile.Parse`, `ParseLax`, `ParseWork` with `Tool`, `Godebug`, `Toolchain`, `Use`, `Replace` | proxy.golang.org; compiled |
| V-3 | `github.com/pelletier/go-toml/v2` v2.4.3; the `unstable` parser is documented as outside the compatibility promise | proxy.golang.org; compiled; tests of `tomlx` |
| V-4 | `go.yaml.in/yaml/v3` v3.0.5 (the maintained fork path used by the architecture); `golang.org/x/text` v0.43.0 (`charmap`, through `limits`); `github.com/package-url/packageurl-go` v0.1.7 and `golang.org/x/sync` v0.24.0 are the core's | proxy.golang.org |
| V-5 | A pnpm lockfile can contain two YAML documents (an environment document before the project document); observed in vercel/ai's `pnpm-lock.yaml` (blob `83e1681f...`) | corpus file, read |
| V-6 | Maven dependency mechanism: six scopes (`compile`, `provided`, `runtime`, `test`, `system`, `import`); `import` is valid only for `type` `pom` inside `dependencyManagement`; Maven 4.1.0 adds `bom` packaging | maven.apache.org dependency-mechanism page, fetched |
| V-7 | Cargo workspaces: `[workspace] members`/`exclude` globs, `resolver`, `[workspace.package]` with `key.workspace = true`, `[workspace.dependencies]` with `{ workspace = true }` (the member may add `features`, `optional`) | doc.rust-lang.org cargo reference (workspaces), fetched |
| V-8 | Gradle version catalogs (documentation for Gradle 9.8.1): default `gradle/libs.versions.toml`; tables `[versions]`, `[libraries]`, `[bundles]`, `[plugins]`; `version.ref`; rich versions `strictly`, `require`, `prefer`, `reject`, `rejectAll` | docs.gradle.org, fetched |
| V-9 | Gradle plugin marker artifact: `<plugin.id>:<plugin.id>.gradle.plugin:<version>` | docs.gradle.org (plugin development / publishing), fetched |
| V-10 | Gradle dependency locking: lines `group:artifact:version=configuration,...`, final `empty=` line, `gradle.lockfile`, `buildscript-gradle.lockfile`; `settings-gradle.lockfile` and the older `gradle/dependency-locks/*.lockfile` are **not** mentioned on that page (kept from earlier Gradle versions and real repositories; the parser is lenient) | docs.gradle.org, fetched |
| V-11 | npm CLI v11.21.0: `workspaces` is an array of globs or paths; uv: `tool.uv.workspace` `members`/`exclude`, `tool.uv.sources` `workspace = true`, every member needs a `pyproject.toml` | docs.npmjs.com, docs.astral.sh/uv, fetched |
| V-12 | pnpm lockfile format: the pnpm.io page for `pnpm-lock.yaml` returned 404 on that date. **Not verified against documentation**; the supported range (`lockfileVersion` 6.0 to 9.x) is what the code reads and what the corpus exercises (vue/core, vercel/ai) | pnpm.io (404) |
| V-13 | PEP 751 (Final): `pylock.toml` and `pylock.<name>.toml` (regex `^pylock\.([^.]+)\.toml$`), required `lock-version` "1.0", `created-by`, `packages`; PEP 735 (Final): `[dependency-groups]`, `include-group`, no cycles | peps.python.org, fetched |
| V-14 | pip requirements files: global options `-r -c -e -i --extra-index-url --no-index -f --no-binary --only-binary --prefer-binary --require-hashes --pre --trusted-host --use-feature`; `--hash` is per requirement; a line ending in an unescaped backslash continues; whitespace followed by `#` starts a comment, applied after continuation; `${VAR}` with braces and an upper-case name (pip 10) | pip.pypa.io requirements file format, fetched |
| V-15 | purl type definitions: `golang` namespace and name lower-cased; `cargo` no namespace, name case-sensitive, no folding; `pypi` no namespace, name lower-cased with `_` replaced by `-`; `maven` groupId and artifactId case-sensitive; `npm` scope in the namespace (the `@` percent-encoded), name case-sensitive | GitHub `package-url/purl-spec` `types/<type>-definition.json`, read through the GitHub API |
| V-16 | The POM model namespace of Maven 4.1.0 is `http://maven.apache.org/POM/4.1.0` and its schema declares `subprojects`; Maven Central POMs use `http://maven.apache.org/POM/4.0.0` | maven.apache.org/xsd (`maven-4.1.0.xsd` redirects to `maven-4.1.0-rc-6.xsd`), a Central POM, fetched |
| V-17 | Go toolchain: go.dev lists 1.27.2 and 1.26.9 as the newest releases on that date; the compile check used 1.27.1 darwin/arm64 | go.dev/dl |
| V-18 | GitHub licence API results for the corpus repositories (apache/maven, commons-lang, spring-boot, okhttp, ktor, nowinandroid, kubernetes: Apache-2.0; flask, django: BSD-3-Clause; poetry, tokio, vue, hono, react, create-react-app: MIT; uv: MIT OR Apache-2.0; ripgrep: Unlicense OR MIT; berry: BSD-2-Clause; npm/cli, vercel/ai, yarn, bun: `NOASSERTION`, re-read from the LICENSE text for npm/cli (Artistic-2.0) and vercel/ai (Apache-2.0); Bun's `LICENSE.md` not re-read in full) | `gh api repos/<r>/license` |

| V-19 | Re-verified by the independent verifier on 2026-10-14: `proxy.golang.org/<module>/@latest` gives go-toml/v2 v2.4.3 (2026-07-05), x/mod v0.42.0 (2026-10-09), go.yaml.in/yaml/v3 v3.0.5 (2026-07-26), x/text v0.43.0 (2026-10-09), packageurl-go v0.1.7 (2026-08-21); `go doc github.com/pelletier/go-toml/v2/unstable`: "APIs that do not meet the backward compatibility guarantees yet"; `go mod verify`: all modules verified | proxy, `go doc` |
| V-20 | Re-read by the verifier: purl-spec `pypi-definition.json` (name lower-cased, `_` replaced by `-`, case-insensitive; dots unchanged in the purl name) and `cargo-definition.json` (name case-sensitive, "The name is the repository name"); PEP 751 file names `pylock.toml` or `^pylock\.([^.]+)\.toml$`, `lock-version` `"1.0"` (the PEP page is marked historical, the maintained text is the PyPA "pylock.toml specification"); `maven-4.1.0.xsd` `targetNamespace` is `http://maven.apache.org/POM/4.1.0` | raw.githubusercontent.com purl-spec, peps.python.org/pep-0751, maven.apache.org/xsd |

---

## Appendix B. Experiments and measurements

All timings are best of five (best of three for E-A1) on the build machine (darwin/arm64, Go 1.27.1), through `RunTree` (parse and assembly of the tree; no GitHub, no store) unless stated.

**E-A1 Linear scaling of the own tokenisers.** Synthetic inputs with 5,000, 20,000 and 80,000 dependency entries, `Parse` only, limits raised to 64 MiB and 50 M nodes: `pypi/requirements` 3.2, 11.9, 45.0 ms (0.2, 0.8, 3.4 MB); `npm/package-json` 2.2, 8.6, 37.8 ms (0.1, 0.4, 1.7 MB); `gradle/script` 4.1, 14.6, 58.0 ms (0.2, 0.9, 3.5 MB); `maven/pom` 8.9, 34.6, 144.6 ms (0.5, 2.2, 8.7 MB). Each fourfold increase of input costs 3.5 to 4.2 times the time. The Maven 8.7 MB case is above the 1 MiB manifest limit and is therefore only reachable by a direct caller.

**E-A2 Real corpus: counts and times.** Locked counts equal independent counts made with ad hoc scripts over the same files.

| Tree | Input | Units | Declared deps | Locked | Time (best of 5) |
|---|---|---|---|---|---|
| npm: vuejs/core (`package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`) | 207 KB | 1 | 52 | 634 (634) | 8.3 ms |
| npm: vercel/ai (`pnpm-lock.yaml`, synthetic root `package.json`) | 1.98 MB | 1 | 0 | 4,061 | 60.8 ms |
| npm: npm/cli (`package-lock.json` v3) | 437 KB | 1 | 0 | 993 (993) | 7.8 ms |
| npm: yarnpkg/berry (`yarn.lock` Berry) | 810 KB | 1 | 0 | 2,050 (2,050) | 24.4 ms |
| npm: oven-sh/bun (`bun.lock`) | 10 KB | 1 | 0 | 42 | 0.2 ms |
| npm: facebook/react (`package.json` + `yarn.lock` v1) | 820 KB | workspace root unit | 112 | 2,388 (2,388) | 15.4 ms |
| python: pallets/flask (`pyproject.toml` + `uv.lock`) | 285 KB | 1 | 31 | 78 (+ the root package, 79 in the lock) | 9.8 ms |
| python: python-poetry/poetry (`poetry.lock`) | 256 KB | 1 | 36 | 78 | 24.0 ms |
| python: astral-sh/uv (`uv.lock`) | 356 KB | 1 | 23 | 95 | 14.0 ms |
| cargo: BurntSushi/ripgrep (`Cargo.toml` + `Cargo.lock`) | 17 KB | | 14 | 52 | 2.1 ms |
| go: kubernetes (`go.mod`) | 11 KB | 1 | 114 | 198 (of which 114 direct) | 0.8 ms |
| gradle: okhttp / ktor / nowinandroid | 16 KB / 29 KB / 31 KB | 1 / 1 / 2 | 103 / 126 / 138 | 0 | 1.0 / 1.3 / 1.7 ms |
| maven: apache/maven `pom.xml` (no PomSource) | 55 KB | 1 | 17 | 0 | 1.1 ms |

A consistency check worth recording: an older `yarn.lock` (2018) paired with the current `package.json` of react produced 107 `npm.lock-missing-dep` warnings, which is exactly what a stale lock must produce; the table uses the consistent pair.

**E-A3 Persisted size.** See the table in section 5 (JSON of `[]Unit`, 5 KB for a Maven POM to 1.49 MB for the largest lock).

**E-A4 TOML decoding (corrected by the verifier).** The first revision reported 37 s for `toml.Unmarshal` of a table header with 100,000 dotted segments and research 07 E3 3.8 s for 40 KB of nesting. With go-toml/v2 v2.4.3 the verifier measured the **whole** first-revision `tomlx.Decode` (Unmarshal first, then the walk) on headers and dotted keys of 1,000, 5,000, 20,000 and 100,000 segments: 0.2, 0.8, 3.7 and 16 ms, all ending in `limit.depth`; the 37 s was not reproducible (it may have been an earlier library version). The cap stays as defence in depth. `tomlx_test.go` pins the line index (`TestDecodeIndex`).

**E-A8 Scaling of the whole parser set (verifier, 2026-10-14, darwin/arm64, Go 1.27.1, one run each, synthetic well-formed files at the size limits).** Before the fixes: `Cargo.lock` of 0.25 / 1 / 2 / 4 / 8 MiB: 0.45 / 6.8 / 27.6 / 107 / 443 s (quadratic: `unstable.Parser.Shape`); 32,000 keys in one table (447 KiB): 9.8 s; `pyproject.toml` with a 1 MiB single-line array: 11.4 s (quadratic `locator`); Gradle catalog with 16,000 libraries and 16,000 accessors: 2.2 s (4,000: 172 ms; the cost was `tomlx`, not the accessor lookup; after the fixes 4,000 entries take 50 ms and 16,000 exceed `MaxKeysPerTable` and end in `limit.nodes` after 67 ms). After the fixes: `Cargo.lock` 0.25 / 1 / 2 / 4 / 8 MiB: 8 / 30 / 59 / 119 / 238 ms; the 8 MiB originals of `package-lock.json` 273 ms, `pnpm-lock.yaml` 579 ms, `yarn.lock` v1 238 ms and Berry 387 ms, `bun.lock` 335 ms, `uv.lock` 243 ms, `poetry.lock` 397 ms, `Pipfile.lock` 321 ms, `gradle.lockfile` 522 ms (100,000 locked, `limit.locked`); the 1 MiB manifests `requirements.txt` 41 ms, `go.mod` 177 ms, `package.json` 83 ms, `pom.xml` 54 ms, `build.gradle` 103 ms, `pyproject.toml` single-line array 87 ms, `settings.gradle` 72 ms; assembly: 320 npm workspace members x 100 dependencies against one lock 93 ms, Maven 4,000 properties x 21 POMs 69 ms, 800 Cargo crates 10 ms, include and BOM diamonds of depth 24 under 2 ms. One table of 40,000 keys still costs 1.2 s inside go-toml (`SeenTracker`), hence `MaxKeysPerTable` = 10,000 (about 60 ms; 40 times that under `-race`).

**E-A5 Allocation volume and retained heap per input byte** (parse only, measured with `runtime.MemStats`: `TotalAlloc` delta for volume, `HeapAlloc` after a forced collection for the retained part): pnpm lock (vue/core) 22.1; Yarn Berry 28.6; Yarn v1 8.9; package-lock v3 14.3; `uv.lock` 4.8; `poetry.lock` 6.7; `Cargo.lock` 13.3; Maven POM 11.0. Retained after GC: 0.6 to 2.8. The root's default weight of 10 bytes per input byte models resident peak, not allocation volume, so no `Weigher` is registered (OQ-3).

**E-A6 Kotlin detection.** The core derives the technology `Kotlin` only from `Maven` ecosystem dependencies on `org.jetbrains.kotlin` or from a `kotlin` toolchain key. A Gradle unit has ecosystem `gradle`, so the golden `kotlin-multiproject` first showed Java for a Kotlin build. Both assemblers now set `Toolchain["kotlin"]` (the plugin version or `unknown`) when they detect Kotlin (Maven: `kotlin-maven-plugin` or a `kotlin-stdlib*` dependency; Gradle: a Kotlin plugin or a non-build `org.jetbrains.kotlin` dependency).

**E-A7 Exponential `GlobMatch` of the core.** `GlobMatch(strings.Repeat("*/**/", n)+"x", <30-segment path>)`: n=8 70 ms, n=12 1.67 s, n=16 6.97 s. `repopath.Match` (dynamic programming, pattern limited to 256 bytes and 16 segments) answers the same inputs in microseconds; the npm test with a 14-segment `**/` pattern had taken 6 s before the switch (F-A12).

---

## Appendix C. What was compile-checked

| Check | Result |
|---|---|
| Toolchain | go1.27.1 darwin/arm64 (go.dev lists 1.27.2 and 1.26.9 as newest, V-17) |
| Build | `sh apply.sh <dir>` builds the scratch module `github.com/cantara/source-code-portal` from `11-manifests-core.assets` (revision verified 2026-10-14 03:33 to 03:44), the overlay and the two patches |
| `gofmt -l .` | empty |
| `go vet ./...` | clean |
| Purity guard | no goroutine, `init`, `os`, `sync`, `time`, `math/rand`, `log`, `net/http` in the six packages and five helpers |
| `go test -race -count=1 ./internal/manifests/...` | all packages pass: the core's 97 tests, the helper tests, and 281 test and subtest executions in the six ecosystem packages (353 with `integration`); re-run from scratch by the verifier after the fixes of Appendix F with `apply.sh` (which now also runs `go mod tidy -diff` and fails on unformatted files) |
| Fuzz (verifier) | 40 s per package against the unmodified seeds on the first revision (golang, npm, python, maven, gradle, cargo): no finding; one more 60 s run of gradle on the fixed code: no finding |
| Fuzz | 60 s per package, no finding: npm, python, maven before the last change (a Gradle-only change: the `buildscript-gradle.lockfile` match); golang, cargo and gradle after it |
| Signatures | `ecosystems/integration/surface_test.go` assigns every exported function of section 2.2 to a typed variable (`Register`, `Spec`, `NewAssembler`, `WithPomSource`, `NormalizeName`, `ParseRequirement`, `ExactPin`, `gradle.ID`) and every assembler and lockfile parser to `VersionedAssembler` and `Kinded` |
| Not compile-checked | the `cmd/scp` composition lines of 2.2 (no `cmd/scp` in the scratch module), the `PomSource` HTTP satisfier (unit 11 and `platform/netguard`), CI configuration |

---

## Appendix D. File inventory and the patch list

`12-manifests-ecosystems-a.assets/` (about 14,200 lines of Go including tests, 250 files, 2.3 MB):

| Path | Content |
|---|---|
| `README.txt` | how to use the assets |
| `apply.sh` | builds the scratch module, runs gofmt, vet, the purity guard and the race tests (about 40 s) |
| `overlay/internal/manifests/ecosystems/{golang,npm,python,maven,gradle,cargo}/` | the six packages: `<eco>.go`, parser files, `assemble.go`, `diag.go`, tests, `testdata/<case>/{input/,expected.golden.json}` (and `pomsource/`), `testdata/fuzz/FuzzParsers/` |
| `overlay/internal/manifests/ecosystems/integration/` | `integration_test.go`, `lockonly_test.go`, `surface_test.go` |
| `overlay/internal/manifests/internal/{tomlx,expand,redact,linex,repopath}/` | the five helpers with tests |
| `overlay/internal/manifests/tree.go` | RR-A2 |
| `overlay/internal/manifests/parsertest/tree.go` | RR-A4 |
| `overlay/internal/manifests/sanitize_foreign_test.go` | `TestSanitizeKeepsForeignEcosystem` (RR-A3) |
| `patches/core-rr-a.diff` (119 lines) | `model.go` (RR-A1 `DiagnosticDoc`, `EcosystemSpec.Diagnostics`; RR-A5 `Dependency.Clone`; RR-A6 `LockedComponent.Display`), `sanitize.go` (RR-A3 foreign-ecosystem rule, RR-A6 display cleaning), `refresh.go` (RR-A7 `stampPaths`) |
| `patches/go-mod.diff` (32 lines) | `go-toml/v2` v2.4.3, `golang.org/x/mod` v0.42.0 |
| `oracle/SOURCES.tsv`, `oracle/fetch-oracle.sh` | 42 real files with repository, blob id, bytes, licence, head flag; downloader and verifier |
| `tools/corpus-main.go.txt` | the timing and count program of E-A2 |

Added or changed by the verifier (Appendix F): `overlay/.../ecosystems/maven/{assemble.go,maven_test.go}`, `.../cargo/cargo.go` and `testdata/workspace-rename/`, `.../python/{pyproject.go,pep508.go,adversarial_test.go}`, `.../internal/tomlx/{tomlx.go,tomlx_test.go}`, `.../internal/redact/{redact.go,redact_test.go}`, `.../internal/expand/expand.go` (comment), `.../ecosystems/integration/scale_test.go`, `patches/go-mod.diff` (direct requirements), `apply.sh` (tidy, gofmt, wider purity guard).

Files of the core **not** patched but needing a decision: `glob.go` (RR-A8 / M0-2), `defaultAssemble` in `assemble.go` (RR-A5 / M0-1), the `EcosystemId` text of the API contract (OQ-8).

---

## Appendix E. Defects the fuzzers found in this unit's own code (all fixed, seeds kept)

| Id | Package | Finding | Fix |
|---|---|---|---|
| F-A1 | golang | module paths `/0/00`, ` /0/0` and `/0` produced a namespace that did not survive a purl round trip (empty or space-padded segments) | `componentOf` trims each segment and drops empty ones (3 seeds) |
| F-A2 | maven | `<parent><groupId>/</groupId>` and similar coordinates with `/`, `\`, `:` or whitespace | `coordOK` in parser and assembler; `maven.dependency-incomplete` (1 seed) |
| F-A3 | gradle | lock coordinate with a blank part or a slash | `coordOK` in `mvn`, lock line rejected with `gradle.lockfile-syntax` |
| F-A4 | gradle | `include` member `"\`; `project(" ")` produced a nameless identity | include validation through `repopath`; `projectDep` trims the path |
| F-A5 | npm | `{"name":" "}` | name trimmed; empty name is `npm.no-name` |
| F-A6 | npm | Bun `{"workspaces":{"/":{}}}` produced an absolute member | members pass `repopath.Rel` |
| F-A7 | npm | Yarn v1 descriptor map iteration order and key collisions made output nondeterministic | descriptors emitted from a sorted slice, first wins |
| F-A8 | npm, python | a lone `\r` made a location out of range (line counting disagreed with the line splitter) | lone CR is whitespace in YAML-like and requirements text; `linex` and the splitters agree |
| F-A9 | python | requirement `-c0\r0` with a control character; names `"\x01"` and with bidi controls | names rejected by `cleanName`/`idOf`; controls cleaned |
| F-A10 | cargo | crate name with a control character | name checked at the parser |
| F-A11 | python | a single-file tree whose `tool.uv.sources` had `workspace = true` kept `SourceProject` in the unit | `resolveWorkspaceSources` runs without a lock and demotes unresolved sources with `python.workspace-source-unresolved` |
| F-A12 | npm | workspace glob test took 6 s with the core's exponential `GlobMatch` | `repopath.Match` (dynamic programming) |
| F-A13 | core | the assembler fuzzer reported `assemble.mutated-input` on a one-file tree that fell back to the core's `defaultAssemble` (it stamps `LocPath` through the shared `Extra` pointer of a cached `Parsed`) | RR-A5: `Dependency.Clone`; the core's `defaultAssemble` must clone (M0-1) |

Defects found by the **verifier** (not by the fuzzers: they are algorithmic or semantic and live in Appendix F as VF-12-01 to VF-12-08, VF-12-22): the fuzzers could not see them because their inputs are small (64 KiB, 0.5 s) and the invariants are about correctness, not about cost. Hence the scale test (acceptance criterion 8).

Seeds: golang 3, npm 6, python 5, maven 1, gradle 5, cargo 3 (23 files under `testdata/fuzz/FuzzParsers/`). Two classes of finding were **tooling** rather than parser defects and are recorded for the next engineer: the harness first rejected the root's shared diagnostic codes (`parse.invalid-dependency`) as undeclared (invariant 7 now accepts the root's codes), and one fuzz run was killed when the scratch module was rebuilt underneath it (a `no such file` failure of the fuzz cache, not a finding).

---

---

## Appendix F. Verification log (independent verifier, 2026-10-14)

Method: read the whole document against `04-decisions.md`, `10-architecture.md` (5, 8, 13, 15 to 17, 21, appendix A), ADR-021 and ADR-022, `11-manifests-core.md` and its assets; rebuilt the scratch module with `apply.sh` (green on the unchanged revision 1: gofmt, vet, purity guard, `go test -race`); measured the import table with `go list`; extracted `Spec().Diagnostics` from the code and diffed it with the 92 codes of section 4 (identical, severities included); re-read the guardrails of 11.1; re-fetched library versions and standards; wrote probe tests (trees for each ecosystem, scaling inputs at the size limits, URL redaction, PEP 508 helpers, privacy of `PomSource` coordinates); fuzzed every package for 40 s; mutation-checked each new regression test against the unfixed code (each fails without its fix). Findings report: `30-verification/design-12-manifests-ecosystems-a.md`. Severity: high = a hostile or merely large repository can exhaust CPU or leak data, medium = wrong facts or a lifecycle hole, low = document or tooling accuracy.

| Id | Sev | Finding | State | Evidence and pin |
|---|---|---|---|---|
| VF-12-01 | high | `tomlx.Decode` was quadratic in the number of keys (`unstable.Parser.Shape` rescans the document per call; `toml.Unmarshal` ran before the depth cap). `Cargo.lock`: 1 MiB 6.8 s, 2 MiB 27.6 s, 8 MiB (the limit) 443 s of uninterruptible CPU; affects every TOML format (`uv.lock`, `poetry.lock`, `Cargo.lock`, `pylock.toml`, catalogs) | fixed: newline table plus binary search, walk before decode; 8 MiB now 0.24 s | E-A8; `TestDecodeIsLinearInTheNumberOfTables`, `TestInputsAtScaleStayLinear/Cargo.lock` |
| VF-12-02 | medium | go-toml's duplicate-key check is quadratic per table: 40,000 keys 1.2 s, 90,000 (under 1 MiB) about 6 s, 40 times that under `-race` | fixed: `MaxKeysPerTable` 10,000 (`limit.nodes`) | `TestKeysPerTableCap`, scale shape "Cargo.toml one table" |
| VF-12-03 | high | `pyproject.toml` element line lookup (`locator.find`) was quadratic on a long line: 1 MiB single-line array 11.4 s; it also returned a wrong line when the element was a substring of an earlier line (`"c"` in `dependencies`) | fixed: one-pass index of quoted strings, binary search; 87 ms | `TestPyprojectLocatorIsLinear` (fails on the unfixed code with 10.8 s and `c:3`) |
| VF-12-04 | medium | `PomSource` coordinates were checked only for `/ \ : space` and `${`: a hostile POM could send `..`, `?`, `#`, `%2e`, a newline or a version with spaces to the satisfier that builds a URL path | fixed: `safeCoordinate` (Maven identifier alphabets) before the guard; task 7.6 for the satisfier | `TestPomSourceCoordinatesAreSafe` (12 hostile shapes fail without the fix) |
| VF-12-05 | medium | a `PomSource` call cut short by the assembly deadline in the **last** unit was recorded as "not found" and published (the context was polled only at the top of each unit), contradicting "returns an error only for `ctx.Err()`" and GR-22 | fixed: final `ctx` check returns the error and no units | `TestAssembleReturnsContextErrorAfterAFetchTimeout` |
| VF-12-06 | medium | stored Maven facts did not change when `manifests.pom_source.enabled` was switched (change detection sees only the tree SHA and `SetHash`; the assembler version was constant) | fixed: `AssemblerVersion` is `1` or `1 \| 1<<16` with a wired source | `TestPomSourceEnabledChangesTheRegistryHash` |
| VF-12-07 | medium | Cargo: `key = { package = "real" }` in `[workspace.dependencies]` was keyed by `real`, so a member's `key.workspace = true` found nothing: `cargo.inherit-missing` and the pool's version lost | fixed: pool keyed by TOML key, `pool.real:<key>` property restores the display | golden `workspace-rename` (fails with the unfixed keying) |
| VF-12-08 | low | `redact.URL` missed scheme-relative userinfo (`//u:p@h`), `;`-separated query parameters and a secret that is not the first fragment parameter (`#egg=x&token=y`) | fixed | `redact_test.go` |
| VF-12-09 | low | document: Maven `ConfHeuristic` for unresolved or `maven.config` values was promised (3.1, 4.5.5) but never emitted and no golden contained it | document corrected to the code (3.1, 4.5.5) | `grep ConfHeuristic ecosystems/maven` is empty |
| VF-12-10 | low | document: 4.7.4 said the Cargo local rename is the `TargetDisplay`; the code (and 3.1) use the real crate | document corrected | probe `cg1` |
| VF-12-11 | medium | architecture 5.1: "equal rank never imports equal rank"; D-4 asked for two rank-1 to rank-1 edges (`expand`, `redact` import `limits`) | fixed in the design: both packages are rank 2 (comments in the code, 2.1, D-4) | `go list` table, section 5.4 proof still holds |
| VF-12-12 | medium | `ecosystems/integration` (all six ecosystems, `store`, `job`, `readmodel` in tests) has no row in the matrix; closed-world `archtest` fails on it and rule 5.1.3 forbids an adapter importing another | recorded: D-12 (row, or move to `cmd/scp`); task 2.3 depends on it | `go list -test` imports |
| VF-12-13 | low | the 5.2 row of ecosystems was said to list `go.yaml.in/yaml/v3` (it is in 13.1 and ADR-021, not in the table); D-15 lists `go.sum` as a Go input (not parsed); ADR-021 lists `deps.dev/util/maven` as adopted (not used) | recorded: D-12(b), D-13, D-14 | architecture 5.2 and 13.1 re-read |
| VF-12-14 | low | `patches/go-mod.diff` marked `go-toml/v2` and `x/mod` `// indirect`; `go mod tidy -diff` was not clean though task 0.5 required it | fixed: direct requirements, `apply.sh` runs `go mod tidy -diff` | `apply.sh` |
| VF-12-15 | low | `apply.sh` printed `gofmt -l` without failing, and the purity guard missed `math/rand/v2`, `crypto/rand`, `sync/atomic`, `os/exec`, `net`, `runtime` | fixed; planted `math/rand/v2` import caught (single-line and block forms) | `apply.sh` |
| VF-12-16 | low | trace gaps: GR-24 (parse failures must not be retried identically) and GR-16 (encoding) had no row | added to 11.1 | `02-design-guardrails.md` re-read |
| VF-12-17 | low | document: the list of POM root fields in 4.5.2 and 11.1 omitted `packaging`, `name`, `description` | corrected in 4.5.2 and 11.1 | `maven/pom.go` |
| VF-12-18 | low | "323 executions" not reproducible; measured 281 (six packages), 353 (with `integration`), 484 (`./internal/manifests/...`) | corrected | `go test -v | grep -c -- "--- PASS"` |
| VF-12-19 | low | E-A4 (37 s for a 100,000-segment header) not reproducible with go-toml v2.4.3 (16 ms for the whole `Decode`) | corrected, claim withdrawn | E-A4 |
| VF-12-20 | medium | the guard is graph-based (empty on a cold start) and Maven facts that depend on the `PomSource` outcome are not re-assembled while the tree is unchanged (`refresh.go` lines 59 and 72): an internal coordinate without a configured prefix can leave the process before its producer is parsed, and a transient failure persists | **not fixed here** (unit 11): RR-A9, OQ-14, L-11, L-12, operator note (task 7.5); VF-12-06 closes the enable toggle | `refresh.go`, `extras.go` |
| VF-12-21 | medium | testability: no test held any parser to a cost bound at realistic size (the fuzz harness caps at 64 KiB and 0.5 s; bombs are small); VF-12-01 and 03 passed every existing test | fixed: `TestInputsAtScaleStayLinear`, acceptance criterion 8 | 18 shapes, 3 s each under `-race` |
| VF-12-22 | low | exported `python.ExactPin("== 1.0")` returned `" 1.0"` and `ExactPin("==1.0 ; python_version<'3'")` returned the marker as part of the version | fixed | `TestExactPin` |

**Checked and found correct** (no change): the import table of 2.1 (identical to `go list`); the 92-code catalogue against the code (codes and severities); 27 parsers and their ids; the rows of 11.1 (each mechanism and test re-read; one field list incomplete: VF-12-17); the library and standard facts V-2 to V-4, V-13, V-15, V-16 (re-fetched); `TestRealRunnerMatchesRunTree` and `surface_test.go` compile against the revised core assets; determinism of three runs on every golden; probe trees for Go (`replace`, `indirect`, `tool`, `/v2` identity), npm (aliases, git and tarball credentials, lock v3 join), Python (PEP 508 corner cases, includes, constraints, PEP 735 groups, uv source credentials), Maven (reactor, `${revision}`, profiles, BOM import without a source, `[x]` pins, foreign namespace), Gradle (settings includes, catalog accessors, `project(":x")`, credentials in `gradle.properties` not emitted), Cargo (workspace inheritance, lock join, `cfg` targets, registry and git sources): all as documented except the findings above; the 40 s fuzz runs of all six packages found nothing; include and BOM diamonds of depth 24 and a 24-pattern workspace glob finish in under 5 ms.

**Not verified** (and why): the tier-2 oracle counts and timings of E-A2 (the 21 real files were not downloaded; the committed tier-1 real files pass); `cmd/scp` composition lines and the `PomSource` satisfier (no such code exists yet); pnpm lockfile documentation (404 on 2026-10-14 as in V-12; the syntax is exercised by the goldens only); Gradle and Maven documentation facts V-6 to V-11 (not re-fetched; consistent with the real files in the corpus).

*End of document.*

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: the integration test registers `catalog.repo` with `readmodel.Existence()` (CD-IF-11).
- The five new internal packages of D-4 (`tomlx`, `linex`, `repopath`, `expand`, `redact`) are accepted as matrix rows under `manifests/internal/*` (A-30).
