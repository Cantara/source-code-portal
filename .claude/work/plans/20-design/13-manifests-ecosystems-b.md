# 13. Manifests: ecosystem parsers B and optional providers (`internal/manifests/ecosystems/{nuget,ruby,php,swift,cocoapods,dart,elixir,docker,actions,terraform,helm}`, `internal/manifests/providers/{githubgraph,sbom,depsdev}`)

Status: design, revision 1 with independent verification fixes (revision 1.1, 2026-10-14; every change is logged in section 19 and in `30-verification/design-13-manifests-ecosystems-b.md`). Author: unit designer 13 (`13-manifests-ecosystems-b`); verifier: independent verifier of unit 13. Binding inputs, in precedence order: `04-decisions.md` (D-15, A-15, A-20..A-26), `10-architecture.md` (sections 4, 5, 6, 13, 15, 16, 17, 18, 22), ADR-021 and ADR-022, `05-research/07-manifest-parsing.md`, `05-research/09-github-app-integration.md`. Every ecosystem here is new work under D-15; section 11 traces the decisions this unit realises (a short table) and the design guardrails it enforces, with the mechanism and test for each (a long table).

How to read this document: sections 1-15 follow the required template. Section 4.1 is the shared parser contract every parser in this unit obeys; 4.2.1 to 4.2.11 are the eleven ecosystems, each with the same sub-headings (Files and match, Grammar handled, Grammar not handled, Mapping to the model, Identity and purl, Confidence, Limits and hardening, Diagnostics, Assembly, Corpus). 4.3 is the generic manifest-plus-lock join assembler; 4.4 is the provider layer. Section 16 (appendix) is the verification log with dates; section 17 lists the experiments and their numbers; section 18 lists the compile-checked surface; section 19 is the independent verification log (what was changed after review and why). Section 2.5 reconciles this unit with the core unit's reference scaffold (`11-manifests-core.reference.tar.gz`).

Identifiers used throughout: **RR-n** = a requirement this unit places on the `manifests` root (owned by the core manifests unit or created by this unit, owner stated in the table in 2.3); **RI-n** = a requirement on `manifests/internal/*`; **D-n** = a deviation proposed (section 14); **OQ-n** = open question (section 15); **V-n** = verified fact (section 16); **E-Bn** = experiment (section 17); **B0..B5, P5, P6** = milestones (section 13).

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

Turn the package-manager files of eleven more ecosystems into the one normalised model of `internal/manifests` (research 07 section 5.4: `ComponentID`, `Dependency`, `Artifact`, `LockedComponent`, `Parsed`, `Unit`), so that the repository dependency panel, the organisation-wide "who depends on what" graph, the version-spread and licence views and the JSON/LLM API (D-15, section 13.6 of the architecture) cover .NET, Ruby, PHP, Swift, CocoaPods, Dart/Flutter, Elixir, Docker, GitHub Actions, Terraform/OpenTofu and Helm. Add, behind switches that are off by default, three secondary sources (GitHub dependency-graph GraphQL, GitHub asynchronous SBOM report, deps.dev) behind a new `DependencySource` interface with strict precedence and provenance.

Why these eleven matter to this product specifically: Docker, Actions, Terraform and Helm are where an organisation's *internal* edges hide (an image built from another repo, a reusable workflow `uses: org/ci/.github/workflows/x.yml@v3`, a Terraform module `git::https://github.com/org/modules//vpc?ref=v1`, an umbrella chart `file://../child`); the channel-B source-URL match of the org graph (13.3 of the architecture) is only as good as these parsers' `SourceRef` values. NuGet, Ruby, PHP, Swift, Dart and Elixir are the long tail of D-15 "all major ecosystems".

### 1.2 In scope (this document specifies, the engineer implements)

1. Eleven ecosystem packages, 27 parsers, one dedicated assembler (NuGet), the generic join assembler (section 4.3) for eight others (ruby, php, swift, cocoapods, dart, elixir, terraform, helm) and the default per-directory assembler for docker and actions:

| Package | Parsers (ID, `Version()` starts at 1) | Assembler |
|---|---|---|
| `ecosystems/nuget` | `nuget/csproj`, `nuget/props`, `nuget/config`, `nuget/lock`, `nuget/nuspec` | dedicated (CPM, property inheritance, bounded `Import` following, lock join, `ProjectReference` edges) |
| `ecosystems/ruby` | `gem/lock`, `gem/gemfile`, `gem/gemspec` | join |
| `ecosystems/php` | `composer/json`, `composer/lock` | join |
| `ecosystems/swift` | `swift/package-swift`, `swift/resolved` | join (with Xcode `UnitDir`) |
| `ecosystems/cocoapods` | `cocoapods/podfile-lock`, `cocoapods/podfile`, `cocoapods/podspec` | join |
| `ecosystems/dart` | `pub/pubspec`, `pub/lock` | join (workspaces) |
| `ecosystems/elixir` | `hex/mix-exs`, `hex/mix-lock` | join (umbrella) |
| `ecosystems/docker` | `docker/dockerfile` | default per-directory (RR-9) |
| `ecosystems/actions` | `github/workflow`, `github/action` | default per-directory (RR-9) |
| `ecosystems/terraform` | `terraform/lock`, `terraform/tf` | join |
| `ecosystems/helm` | `helm/chart`, `helm/lock`, `helm/requirements` | join (with `Skip` for vendored subcharts) |

Parser IDs obey the registry rule `<EcosystemID>/<format>` (the reference registry rejects any other prefix, verified by running it, section 19 V-F1): the package directory is `ecosystems/ruby` but its `EcosystemID` is `gem`, so its parsers are `gem/lock`, `gem/gemfile`, `gem/gemspec`; likewise `composer/json`, `composer/lock` (dir `php`), `pub/pubspec`, `pub/lock` (dir `dart`), `hex/mix-exs`, `hex/mix-lock` (dir `elixir`), `github/workflow`, `github/action` (dir `actions`) (D-8). The `Version()` of every parser starts at 1.

2. The `DependencySource` interface, `ProvidedFact`, `Provenance`, `FactsRequest`, `Enriched`, `Merge` (root file `manifests/source.go`, created at P5 together with the first provider, as ADR-021 amendment requires) and the two job kinds that run providers (`manifests.enrich`, `manifests.enrich.depsdev`).
3. The three providers `providers/githubgraph`, `providers/sbom`, `providers/depsdev`.
4. The generic join assembler (`manifests/join.go`, RR-3) and, as *proposals*, two small shared internal packages `manifests/internal/yamlsafe` and `manifests/internal/rubydsl` (D-1).
5. Per-parser golden/fuzz/property tests and the shared corpus listed in section 12.

### 1.3 Out of scope (non-goals)

- Maven, Gradle, npm, Python, Cargo, Go (the other ecosystems unit), the `manifests` root harness, graph, API, SPA and CycloneDX export (core unit), the `github` adapter methods that the providers need (adapter unit, RR-16), the store DDL (store unit).
- **Executing or evaluating anything**, ever: no `ruby`, `swift`, `mix`, `msbuild`, `dotnet`, `terraform`, `helm`, `pod`, `bundle`, `composer` binary; no Elixir `Code.eval_quoted` (Mix itself evaluates `mix.lock`; we tokenise it as data), no MSBuild condition evaluation, no HCL function calls or variable evaluation, no Go-template rendering, no archive (`.nupkg`, `.tgz`, `.gem`) opening, no network access from any parser.
- Resolving versions the files do not contain (no registry lookups in parsers; the `PomSource` analogue for NuGet/Composer is *not* introduced), version-range solving, vulnerability data, Dockerfile image *contents*, Kubernetes manifests, docker-compose files (post-v1 candidates), Xcode `project.pbxproj` parsing (OQ-8), `.tf.json` (diagnosed, skipped), Forgejo/Gitea workflow directories, Terragrunt, Helmfile, Kustomize.
- Licence detection beyond what a file *declares* (D-15: declared-only in v1). deps.dev licences are an optional enrichment (4.4.5).
- Writing to any other repository path than `20-design/`; scratch code is throwaway and is not part of the deliverable.

### 1.4 Directories covered (exact)

```
internal/manifests/
  source.go            P5  DependencySource, FactsRequest, ProvidedFact, Provenance, Enriched, Merge, Rank   (this unit creates it)
  enrich.go            P5  EnrichConfig, JobKindEnrichGitHub, JobKindEnrichDepsDev                          (this unit creates it)
  join.go              B0  NewJoinAssembler, JoinOptions                                                    (this unit creates it, OQ-1)
  internal/yamlsafe/   B0  bounded YAML to own tree (D-1)
  internal/rubydsl/    B0  Ruby DSL scanner and call reader (D-1)
  ecosystems/{nuget,ruby,php,swift,cocoapods,dart,elixir,docker,actions,terraform,helm}/
  providers/{githubgraph,sbom,depsdev}/                                                                   P5/P6
```

Each ecosystem directory contains: `<eco>.go` (Config, Spec, Register), one file per parser (`<parser>.go`), `testdata/` (goldens and fuzz seeds) and `<parser>_test.go`. Parser test data lives next to the parser (`testdata/<case>/{input,expected.json}`), driven by `manifests/parsertest`.

---

## 2. Position in the architecture

### 2.1 Allowed imports and importers (rank table rows, verified against architecture 5.2 on 2026-10-14)

| Package | Rank | May import (internal) | External imports | Imported by |
|---|---|---|---|---|
| `manifests/ecosystems/<eco>` | 5 | `manifests`, `manifests/internal/*` (own tree only) | nuget: `golang.org/x/text/encoding/charmap` (row already lists it); terraform: `github.com/hashicorp/hcl/v2` and `.../hclsyntax` (row already lists it); all others: none. **No ecosystem package imports a YAML library**: YAML goes through `manifests/internal/yamlsafe` (D-1) | `cmd/scp` only (composition), `manifests/parsertest` tests |
| `manifests/providers/<p>` | 5 | `manifests`, `scm`, `freshness` | `go.opentelemetry.io/otel/metric`, `.../trace` (**row currently says "none"; D-5 adds "otel API"**, consistent with every other feature row and G11). **A provider may not import `job`** (not in its row): it returns errors that satisfy the `job` behaviour interfaces structurally (`RetryAt()`, `Permanent()`, `Gone()`), either passed through from the `github` adapter (`*github.Error`, `*github.PauseError`, which already do) or built from `manifests.ProviderError` (RR-20) | `cmd/scp` only |
| `manifests/internal/yamlsafe` (proposed) | 1 | none | `go.yaml.in/yaml/v3` (v3.0.5) | ecosystems `actions`, `dart`, `cocoapods`, `helm` (and other ecosystem packages, OQ-2) |
| `manifests/internal/rubydsl` (proposed) | 1 | none (takes a `tick func() error` instead of importing `limits`) | none | ecosystems `ruby`, `cocoapods` |
| `manifests/internal/{limits,purl,version}` | 1 | none | `packageurl-go` v0.1.7, `deps.dev/util/semver` | root, ecosystems |

Rank argument for D-1: both new packages sit at rank 1 beside `limits`; neither imports another internal package, so every edge still decreases rank. The `archtest` closed world needs two new rows (D-1). `cmd/scp` registers the ecosystems in a fixed order and wires providers; nothing else in the module imports these packages, and no ecosystem or provider imports another (verified in the compile-check: `go list -f '{{.Imports}}'` of all 14 packages shows only `internal/manifests` and, for githubgraph and sbom, `internal/scm`).

The provider-specific rule (architecture 5.3 G6 "provider SDKs only in their adapter"): **no provider links any GitHub or deps.dev SDK**. GitHub access is a consumer-side port that the `github` adapter satisfies (4.4.3, 4.4.4); deps.dev access is plain `net/http` JSON through an injected, SSRF-guarded client (4.4.5).

### 2.2 Interfaces this unit implements (architecture 13.2, research 5.4)

```go
type Parser interface {
    ID() string
    Version() int
    Ecosystem() EcosystemID
    Match(path string) bool
    Parse(ctx context.Context, f File, lim Limits) (Parsed, error)
}
type Assembler interface {
    Ecosystem() EcosystemID
    Assemble(ctx context.Context, files FileSet, parsed []Parsed) ([]Unit, error)
}
type DependencySource interface {             // created at P5 by this unit; signature deviates from 13.2, see D-2
    Name() string
    Facts(ctx context.Context, req FactsRequest) ([]ProvidedFact, error)
}
```

Each ecosystem package exports exactly: parser ID constants, `Config`, `DefaultConfig() Config`, `(Config) Validate() error`, `Spec() manifests.EcosystemSpec`, `Register(r *manifests.Registry, cfg Config) error`. `Register` calls `r.RegisterEcosystem(Spec())`, `r.Register(p)` for every parser and `r.RegisterAssembler(a)` when the ecosystem has one, and returns the registry's duplicate-ID error. No other symbol is exported (parsers and assemblers are unexported types constructed by `Register`), which keeps `go doc` of each package to six lines and forces tests through `parsertest` and `Register`.

### 2.3 What this unit requires from other units

**Required root surface (RR-n)** (`internal/manifests`, rank 4; compiled against a stub of research 5.4 on 2026-10-14, see 18):

| ID | Requirement | Owner and status |
|---|---|---|
| RR-1 | `EcosystemSpec` gains optional `FormatPURL PURLFormatter` where `type PURLFormatter func(id ComponentID, display, version, qualifiers string) (purl string, ok bool)`. `nil` means the default formatter (`purl.Format(typ=PURLType, ns, name, ...)`). `ok=false` means "this component has no purl" (terraform, helm). `qualifiers` is the canonical `Dependency.Qualifiers` string; the reserved key `subpath` is rendered as the purl `#subpath` fragment, never as a qualifier | core unit; additive field |
| RR-2 | `NewParser(id string, version int, eco EcosystemID, match func(string) bool, parse func(context.Context, File, Limits) (Parsed, error)) Parser` (research 5.4 note c "ParserFunc") | core unit |
| RR-3 | `NewJoinAssembler(eco EcosystemID, opt JoinOptions) Assembler` and `JoinOptions` as specified in 4.3 | **this unit writes `manifests/join.go`** (task B0-2); OQ-1 if the core unit prefers to own it |
| RR-4 | The harness counts every `Diagnostic` whose `Code` starts with `limit.` into `scp_manifests_limit_hits_total{limit=<suffix>}` and passes `ctx` deadline errors through as outcome `timeout`. Limit names are the core's (`size`, `depth`, `nodes`, `expand`, `timeout`) plus the ones this unit adds (`string`, `alias`, `diag`, `import-depth`, `import-count`, `hcl-depth`, `hcl-unary`, `hcl-ternary`); the reference scaffold names the byte-size limit `size`, not `bytes` | core unit; constant `manifests.LimitDiagPrefix` |
| RR-5 | `Limits` zero fields are resolved to defaults before a parser is called (manifests 1 MiB, lockfiles 8 MiB, depth 256, nodes 2,000,000, `MaxExpand` 1 MiB per value, `MaxExpandAll` 8 MiB per file, 2 s); parsers never see zero fields. The reference scaffold implements this as `Limits.WithDefaults()` and selects the lockfile size through the optional `manifests.Kinded` capability: **every lock parser of this unit implements `Kind() manifests.FileKind` returning `KindLockfile`** (`gem/lock`, `composer/lock`, `swift/resolved`, `cocoapods/podfile-lock`, `pub/lock`, `hex/mix-lock`, `terraform/lock`, `helm/lock`, `nuget/lock`); no parser implements `Weigher` (the default amplification of 10 is right for text formats) | core unit |
| RR-6 | The graph builder de-duplicates edges by `(unit, ComponentID, Scope)` because CocoaPods emits one `Dependency` per subspec (`Firebase/Auth`, `Firebase/Core`) that share one `ComponentID` | core unit |
| RR-7 | `LockedComponent.Source string` (`""` for own parses, otherwise a `Source*` constant): provenance of entries added by providers | this unit (P5) edits `model.go` |
| RR-8 | `(*Registry).Spec(EcosystemID) (EcosystemSpec, bool)` read-only lookup; providers normalise IDs with `Spec.Normalize` instead of importing ecosystem packages | core unit |
| RR-9 | A default assembler used for any ecosystem without a registered one: group `Parsed` by `(ecosystem, dir)` into one `Unit` per directory, concatenating `Provides`, `Dependencies`, `Locked`, `Members`, merging `Toolchain`/`Licenses`/`Diagnostics`, in path order; plus `RepoFacts.Enriched []Enriched` (P5) | core unit (default assembler); this unit (`Enriched` field) |
| RR-10 | The sentinel namespace `_local` (`ComponentID{Ecosystem, "_local", <cleaned path>}`) means "inside this repository". **Parser output is cached by blob SHA alone (architecture 13.4), so it must not depend on the file's path (4.1.9)**: a parser emits the path *relative to the directory of the declaring file* (`path.Clean`ed, `..` allowed, never absolute) as the `Name` of a `_local` target together with `Source=SourcePath` and `SourceRef` equal to the same path. The runner's `rebasePaths` (reference scaffold `assemble.go`, runs before every assembler) rewrites `SourceRef` to a repo-relative path; the join assembler (J-0) and the NuGet assembler rewrite the `Name` of `_local` targets the same way (`path.Join(dir(file), name)`) and drop a dependency that escapes the repository with the core's diagnostic `assemble.path-escape`. **Exception: GitHub Actions** resolves `uses: ./x` against the workspace root, so its parser emits the repo-relative path and an empty `SourceRef` (the runner then leaves it alone). The graph must (i) not list `_local` components as external dependencies, (ii) key them per repository (`modules/vpc` exists in many repositories; unkeyed they would merge into one fake component and "used by" would cross repositories), (iii) resolve them to a unit of the same repository whose `Dir` equals the path (the reference scaffold's channel C does this from `SourcePath` + `SourceRef`, which is why both are always set) | core unit |
| RR-11 | A parser may emit a `Target` whose `Ecosystem` is another registered ecosystem (Actions emits `docker` targets for `docker://` and `container.image`); invariants and the graph key on `ComponentID` only | core unit |
| RR-12 | For `Resolution == ResDeclared` the field `Version` may carry the *nominal* version (NuGet "minimum version", Actions mutable tag); version-spread views show `Version` when non-empty and `Requirement` otherwise, regardless of `Resolution` | core unit |
| RR-13 | `LockedComponent.Integrity` and any digest field use `<alg>:<lowercase hex>` (`sha512:...`, `sha256:...`, `sha1:...`); the Go-style `h1:<base64>` hash is kept verbatim because it names its own algorithm. npm-style SRI conversion is the other unit's concern (OQ-3) | core unit |
| RR-14 | The root builds `FactsRequest.IsPrivate`: true for a `ComponentID` that appears in the graph's `produced` map (channel A) or matches `manifests.pom_source.private_group_prefixes`; `depsdev` adds its own `private_group_prefixes` on top (the scaffold's `NewPrivacyGuard(cfg.PomSource, internal).Allow(id)` is exactly this predicate; wire `IsPrivate = func(id) bool { return !guard.Allow(id) }`) | core unit (`enrich.go` caller) |
| RR-15 | Cache namespace `manifests.depsdev` (default cap 16 MiB) and a bridge from `store` to `depsdev.Cache` | store/bridge units; key `store.cache_caps.depsdev` |
| RR-16 | The `github` adapter exposes `GraphQL(ctx, repo scm.RepoRef, query string, vars map[string]any) (json.RawMessage, error)` and `GenerateSBOMReport` / `FetchSBOMReport` with the stdlib/kernel-only signatures of 4.4.3-4.4.4, so `cmd/scp/assert.go` can assert them directly (A-20 "direct" satisfaction) | adapter unit; blocked on spike S-A |
| RR-17 | `manifests.CleanText(s string, max int) string` (exists in the reference scaffold, `sanitize.go`; this unit's earlier name `SafeText` is dropped): caps on a rune boundary, replaces invalid UTF-8, removes C0/C1 controls (except tab) and bidi controls; used by parsers and by providers (which may not import `manifests/internal/*`) on every string that reaches `Parsed` or `ProvidedFact` | core unit |
| RR-18 | `(*Registry).Register` accepts exactly IDs of the form `<EcosystemID>/<format>`; `RegisterEcosystem` replaces the preloaded built-in spec of the same ID (this unit's `Spec()` therefore overrides the scaffold's generic `purl.Normalize` for the eleven ecosystems; the two must agree on idempotence, tested by `TestSpecNormalizeIdempotent`) | core unit (exists in the scaffold) |
| RR-19 | One per-repository lock owned by the root's `Service` protects the sequence "load own facts, load enrichment items, `Merge`, publish slot": `Service.Reapply(ctx, repoKey)` runs it, `manifests.refresh` calls it after its own parse, and the enrich jobs call it after persisting their item (4.4.2). Without it two jobs of the same repository (`manifests.refresh/<repo>` and `manifests.enrich/<repo>` are different planner keys and may run concurrently) can publish an older tree over a newer one | core unit; this unit adds `RepoFacts.Enriched []Enriched` |
| RR-20 | Three error types in `manifests/source.go` (compiled, 18): `*RateLimitError{Op, At, Err}` (method `RetryAt() time.Time`), `*PermanentError{Op, Reason, Err}` (method `Permanent() bool`, true) and `*GoneError{Op, Err}` (method `Gone() bool`). They are separate types **on purpose**: the engine classifies by method set (`errors.As` against `job.RateLimited`, `job.Permanent`, `job.Gone`), so a single error type that carries a `RetryAt()` method would make every provider failure look rate-limited (found while compiling the classification against the scaffold's `job` package, `TestProviderErrorClassification`). Providers use them for conditions the adapter cannot classify itself (a GraphQL `FORBIDDEN` inside an HTTP 200 body, a deps.dev 429 with `Retry-After`); adapter errors (`*github.Error`, `*github.PauseError`) are wrapped with `%w` and pass through | this unit (`manifests/source.go`) |
| RR-21 | The engine needs the upstream `deps-dev` before `Register` of `manifests.enrich.depsdev` (`AddUpstream` panics otherwise, `04-refresh-webhook.md` section on `Engine.Register`): `cmd/scp` calls `eng.AddUpstream("deps-dev", ...)` from `refresh.upstreams.deps-dev` (defaults here: concurrency 1, pace 0, job timeout 2m; a job's own `Spec.Timeout` wins) only when the depsdev provider is enabled | refresh unit (generic), `cmd/scp` (wiring) |
| RR-22 | Config composition: ecosystem and provider `Config` types live in rank-5 packages that the rank-4 root may not import, so `cmd/scp/config.go` (rank 6) composes them in one struct: `ManifestsConfig` embeds `manifests.Config` inline (yaml tag `,inline`) and adds `Ecosystems EcosystemsConfig` (yaml `ecosystems`), `Enrich manifests.EnrichConfig` (yaml `enrich`) and `Providers []ProviderConfig` (yaml `providers`). Ecosystem enablement is the core's `manifests.acquisition.disabled_ecosystems` (list of `EcosystemID`), not a per-ecosystem key of this unit | cmd/scp |
| RR-23 | `Artifact.Kind` is restricted to the closed API enum `library, application, plugin, bom, image` (`api/openapi-public.yaml`, `Artifact.kind`; response validation in `apitest` rejects anything else): this unit maps NuGet `tool` to `application`, Composer `metapackage` and `replace` to `library` | this unit |

**Required internal surface (RI-n)** (`manifests/internal/*`; owner: core unit unless stated):

| ID | Surface |
|---|---|
| RI-1 | The cooperative guard. Logical operations used in this document: `Tick()` (count a token, poll `ctx` every 256 calls), `Enter()`/`Leave()` (depth), `Expand(n)` (interpolation budget), `IsLimit(err)`. Actual names in the reference scaffold: `limits.NewGuard(ctx, lim Limits)`, `Guard.Node()` (= `Tick`), `Guard.Poll()`, `Guard.CheckSize(n)`, `Enter`, `Leave`, `Expand`, `*limits.LimitError{Limit, Max}` (use `errors.As`; there is no `IsLimit`). **Wherever this document says `g.Tick()` read `g.Node()`; `limits.IsLimit(err)` is `errors.As(err, &le)` with `le.Limit` as the suffix** (2.5) |
| RI-2 | Text gate. The scaffold's `limits.Text(data []byte) ([]byte, error)` strips a UTF-8 BOM and rejects UTF-16, NUL and invalid UTF-8 with `*limits.EncodingError{Reason}` (`utf16`, `nul`, `invalid-utf8`, `charset:<name>`), mapped to the diagnostic `encoding.unsupported`; XML parsers use `StripBOM` + `RejectBinary` + `limits.CharsetReader` (which implements exactly `iso-8859-1` and `windows-1252`) instead, because the bytes are not UTF-8. This document's earlier name `limits.CleanText` is dropped |
| RI-3 | Output text hygiene is `manifests.CleanText(s, max)` in the root (RR-17), not a `limits` function: the seventh fuzz invariant needs it on every string that reaches `Parsed` |
| RI-4 | `purl`: the reference scaffold offers `purl.Build(eco, namespace, name, version, qualifiers string) (string, error)`, `purl.Parse`, `purl.RepoKey(raw string) (key string, ok bool)` and `purl.RepoKeyFromModulePath`. **`purl.RepoKey` is the one repository-URL canonicaliser** (4.1.6): lower-case `host/owner/repo`, scheme, userinfo, `.git`, ref and subdir removed, `owner/repo` shorthand and `git@host:o/r` accepted; the root's channel-B matcher uses the same function. `FormatPURL` (RR-1) sits on top of `purl.Build`; its qualifier string is `k=v&k=v` with sorted keys |
| RI-5 | `version.Compare(system, a, b) (int, bool)` and `version.Satisfies(system, v, req) (ok, known bool)`; systems used here: `nuget`, `rubygems`, `composer` (full grammars in `deps.dev/util/semver`, commit 7bac2db56a63 of 2026-10-13, V-17), `semver` (its `DefaultSystem`), `opaque` (no order) |
| RI-6 | `yamlsafe` (this unit proposes and specifies, 4.1.4, D-1) |
| RI-7 | `rubydsl` (this unit proposes and specifies, 4.1.5, D-1) |

### 2.4 What was compile-checked

A scratch module `github.com/cantara/source-code-portal` (Go 1.27.1, `go vet ./...` and `go build ./...` clean on 2026-10-14) contains: the research 5.4 model verbatim plus the RR-1..RR-9 and RR-17 signatures, `Registry.Spec`, the `ErrProviderConfig` sentinel; stubs of `scm.RepoRef`/`Visibility`, `limits`, `purl`, `version`, `yamlsafe`, `rubydsl`; the exported surface of all eleven ecosystem packages; and the three providers with their `Deps`, `Config`, ports and `var _ manifests.DependencySource = (*Provider)(nil)` assertions, against `go.opentelemetry.io/otel/metric` and `trace` v1.47.0 (2026-10-02). Only signatures were checked; bodies are stubs. Every Go block marked "(compiled)" below was extracted from that module.

Verification pass (2026-10-14): the surface was compiled a second time against the core unit's real reference scaffold (`11-manifests-core.reference.tar.gz`, Go 1.27.1, `go build` and `go vet` clean) with the corrections of this revision (section 18 lists the additions). That pass found that **11 of the 27 parser IDs of revision 1 were rejected by the scaffold's `Registry.Register`** (V-F1) and the other mismatches of 2.5.

### 2.5 Reconciliation with the core unit's reference scaffold

The core unit's design document is not yet in `20-design/`; its reference scaffold (non-normative, "the design document wins on any difference") is the only executable statement of the root. Where this document and the scaffold differ, the table fixes which side adapts so that an engineer can build both without asking:

| # | Topic | Scaffold | Revision 1 of this document | Resolution (binding for this unit) |
|---|---|---|---|---|
| 1 | Parser ID rule | `Register` requires prefix `<EcosystemID>/` | `ruby/*`, `php/*`, `dart/*`, `elixir/*`, `actions/*` | IDs renamed (D-8, 1.2) |
| 2 | Guard | `NewGuard(ctx, Limits)`, `Node()`, `Poll()`, `CheckSize`, `Enter/Leave`, `Expand`; ctx done surfaces as `*LimitError{Limit:"timeout"}` | `NewGuard(ctx, d, n, e)`, `Tick()`, `IsLimit` | scaffold names; **but a done context always wins**: the skeleton (4.1.1) tests `ctx.Err() != nil` before it classifies a limit error, so `Parse` still returns `ctx.Err()` and never a cached "timeout" result |
| 3 | Limit name for size | `size` | `bytes` | `size` |
| 4 | Text gate | `limits.Text(data) ([]byte, error)`, `*EncodingError{Reason}` | `CleanText(data) (out, problem)` | scaffold; hygiene of output strings is `manifests.CleanText(s, max)` |
| 5 | Repo URL key | `purl.RepoKey`: lower-case `host/owner/repo` | `RepoURL`: case preserved, any depth, `EqualFold` in the matcher | `purl.RepoKey` (the graph already uses it for channel B) |
| 6 | `Dependency` layout | compact struct, rare fields behind `Extra`, accessors `TargetDisplay()`, `Condition()`, `Qualifiers()`, `SourceRef()`, `Loc()` and setters `SetTargetDisplay`, `SetCondition`, `SetQualifiers`, `SetSource(kind, ref)`, `SetLocPath`; `Line int32` | field notation (`Dependency.Condition`, `.Loc` ...) | field names in this document are shorthand for the accessor/setter of the same name; `Loc` is `Line` only (the runner fills `LocPath` and `Location.Path`, a parser never knows its path, 4.1.9) |
| 7 | `Parsed.Members` base | relative to the file's directory (`ExpandMembers(base, patterns, dirs)`) | repo-relative | file-relative globs; join uses `manifests.ExpandMembers` |
| 8 | `Parsed.Includes` | files this file includes, file-relative | `Properties["import.<n>"]` | NuGet fills **both**: `Includes` with the unconditional literal `Import` paths (MSBuild variables stripped), `Properties["import.<n>"]` with every import and its condition |
| 9 | `FileSet` | `Paths()`, `Has()`; no `Read` | `Read` in research 5.4 | no assembler of this unit reads blobs; everything an assembler needs is in `Parsed` (NuGet properties, imports) |
| 10 | Path escape | runner `rebasePaths`, diagnostic `assemble.path-escape` | per-parser `*.path-escapes` | one code: `assemble.path-escape` (emitted by the runner or J-0); the `swift`, `terraform` and `helm` codes are removed; `actions.path-escapes` stays because Actions paths are root-relative and the parser can judge them |
| 11 | Ecosystem switch | `acquisition.disabled_ecosystems` + `Registry.Disable` | `manifests.ecosystems.<eco>.enabled` | core key only (RR-22) |
| 12 | Byte-size config type | `manifests.ByteSize` (text form `256KiB`) | `size` | `manifests.ByteSize` |
| 13 | Scope in API | `Dependency.scope` is a plain string, `unknown` allowed; `Artifact.kind` is a closed enum | `Kind "tool"`, `"metapackage"`, `"replace"` | RR-23 |

---

## 3. Domain model and Go types

The shared model (`ComponentID`, `Dependency`, `Artifact`, `LockedComponent`, `Parsed`, `Unit`, `Diagnostic`, `Limits`, `File`, `FileRef`) is exactly research 5.4 and is not repeated. This section fixes how *this unit* fills it, then lists the types it adds.

### 3.1 Field-filling conventions shared by all parsers in this unit

| Field | Rule |
|---|---|
| `Dependency.Target` | normalised per 3.2; never empty `Name` (an entry whose name cannot be determined is dropped with a diagnostic, never emitted with an empty name) |
| `Dependency.TargetDisplay` | original-case name when `Target` was case-folded or reduced (NuGet id, `Firebase/Auth`, Swift `Alamofire`, Actions `actions/Checkout`); `""` when equal to the ID's `Name`/`Namespace/Name` |
| `Dependency.Requirement` | the constraint exactly as written, capped by `manifests.CleanText(s, 512)`; for ecosystems whose source form is not a string (Swift `.package(from:)`, Mix keyword options) a canonical rendering fixed in the ecosystem's section |
| `Dependency.Version` | exact version when known; for `ResDeclared` the nominal version per RR-12 (only where the ecosystem section says so); otherwise `""` |
| `Dependency.Resolution` | `ResLocked` only from a lock join; `ResPinned` only when the manifest names exactly one version or an immutable reference (digest, 40-hex SHA, `[1.0]`, `==`); `ResManaged` for NuGet CPM; `ResDeclared` otherwise |
| `Dependency.Group` | the original group/section/configuration name; for Docker and Actions it carries the *pin kind* (`digest`, `tag`, `latest`; `sha`, `semver-tag`, `ref`). `Dependency.Optional` (the model's bool) is set from the source's own optional flag (Mix `optional: true`, Composer none, Gemfile none); it never changes `Scope` |
| `Dependency.Condition` | raw, never evaluated, capped at 256 bytes (MSBuild `Condition`, Podfile `:configurations`, Helm `condition`, Swift `#if` expression, Composer none) |
| `Dependency.Qualifiers` | canonical purl qualifier string (`k=v&k=v`, keys sorted); key `subpath` is reserved for the purl subpath (RR-1) |
| `Dependency.Source` / `SourceRef` | `SourceRegistry` + `""` for the default registry of the ecosystem, `SourceRegistry` + registry URL for a custom one, `SourceGit` + `purl.RepoKey(url)` for git (`host/owner/repo`, lower case: directly comparable with the key the graph derives from an org repository, 4.1.6), `SourcePath` + the cleaned path **relative to the declaring file's directory** (RR-10, 4.1.9; the runner rebases it to repo-relative before assembly), `SourceURL` + URL without userinfo/query |
| `Dependency.Confidence` | `ConfExact` for lockfiles and structured manifests (XML, JSON, YAML, HCL); `ConfHeuristic` for every lexical scan (Gemfile, gemspec, Podfile, podspec, `Package.swift`, `mix.exs`) and for any value obtained through property or `ARG` substitution |
| `Dependency.Loc` | 1-based line of the declaring token (`Dependency.Line`); `0` when unknown. The path half of the location is never set by a parser (4.1.9): the runner stamps it |
| `Parsed.Kind` | `KindManifest` for project files, `KindLockfile` for lock files, `KindCatalog` for NuGet props files whose content has no `PackageReference` item (only `PackageVersion`/properties; decided from the content, never from the file name, 4.1.9), `KindWorkspace` for a manifest that only aggregates members |
| `Parsed.Properties` | only what the assembler needs (NuGet properties, Dart `resolution`, Swift package name); keys documented per ecosystem |
| `Parsed.Toolchain` | language/runtime/tool constraints (`ruby`, `bundler`, `php`, `ext-*`, `swift-tools`, `dart`, `flutter`, `elixir`, `terraform`, `kubernetes`, `node`) |
| `Parsed.Diagnostics` | at most 100 per file; the 101st is replaced by one `diag.truncated`; each `Message` is a constant sentence plus at most one quoted token capped at 64 bytes |

Never-fail rule: **`Parse` returns a non-nil error only when `ctx` is done** (then `err == ctx.Err()`, the harness records outcome `timeout`/`canceled`). Every other problem (malformed input, limit hit, unsupported encoding, unknown construct) yields the recovered part of `Parsed` plus a diagnostic. This is what lets the harness cache a "parsed with diagnostics" result against `(blobSHA, parserID, version)` instead of retrying.

### 3.2 Identity table (ComponentID, display, purl)

`PURLType` is the purl type from purl-spec v1.1.0 (verified 2026-10-14, V-8). Where the spec has no type the field is `""` and `FormatPURL` returns `ok=false`.

| Ecosystem (`EcosystemID`) | Namespace | Name | `Normalize` | `PURLType` | `VersionSystem` | purl example |
|---|---|---|---|---|---|---|
| `nuget` | `""` | package id **lower-cased** | lower name | `nuget` | `nuget` | `pkg:nuget/Newtonsoft.Json@13.0.3` (display keeps case) |
| `gem` | `""` | gem name as written | none | `gem` | `rubygems` | `pkg:gem/rails@7.1.0` |
| `composer` | vendor lower | package lower | lower both | `composer` | `composer` | `pkg:composer/symfony/console@v6.4.1` |
| `swift` | `host/owner` lower-cased, or registry `scope` | repo name without `.git`, lower-cased | whole key lower-case (`purl.RepoKey`) | `swift` | `semver` | `pkg:swift/github.com/Alamofire/Alamofire@5.4.3` for `ComponentID{swift, github.com/alamofire, alamofire}` with `TargetDisplay github.com/Alamofire/Alamofire` (the purl-spec swift type is case-sensitive; this is the spec's own example) |
| `cocoapods` | `""` | pod root name (before `/`) | none | `cocoapods` | `rubygems` (verified: `Pod::Version < Pod::Vendor::Gem::Version`, V-F5) | `pkg:cocoapods/Firebase@10.0.0#Auth` |
| `pub` | `""` | lower `[a-z0-9_]` | lower name | `pub` | `semver` | `pkg:pub/http@1.2.0` |
| `hex` | organisation lower, or `""` | package lower | lower both | `hex` | `semver` | `pkg:hex/phoenix@1.7.10`, `pkg:hex/acme/private_pkg@1.0.0` |
| `docker` | `registry/path-without-last` with defaults `docker.io` and `library/` | last path segment lower | lower both | `docker` | `opaque` | `pkg:docker/nginx@1.27`, `pkg:docker/org/app@v1?repository_url=ghcr.io` (4.2.8) |
| `github` (Actions) | owner lower | repo lower | lower both | `github` | `semver` | `pkg:github/actions/checkout@v4#sub/path` |
| `terraform` | provider: `host/namespace`; module registry: `host/namespace`; git module: `git:host/ownerpath` | provider: type; registry module: `name/system`; git module: repo | lower both (git module: host lower only) | `""` (unofficial) | `semver` | none |
| `helm` | normalised repository (`ghcr.io/org/charts`, `charts.example.org`, `alias:name`) | chart name | host lower, rest as written | `""` (unofficial) | `semver` | none |

`_local` (RR-10) is a namespace value for any of these ecosystems when the target is a path inside the repository **and the file names no package** (Swift `path:`, Terraform local modules, Helm `file://` and in-tree subcharts, Actions `./x`, NuGet `ProjectReference`); a path dependency that names its package (Dart, CocoaPods, Ruby `path:`, Elixir `path:`) keeps the package as `Target` and only sets `SourcePath`.

Other `EcosystemSpec` fields (as the scaffold's built-in specs fill them): `DisplayName` NuGet, RubyGems, Composer, Swift Package Manager, CocoaPods, pub, Hex, Docker, GitHub Actions, Terraform, Helm; `Language` C#, Ruby, PHP, Swift, Swift, Dart, Elixir and `""` for the last four. `VersionSystem` values are the scaffold's `version.System` keys (`nuget`, `rubygems`, `composer`, `semver`, `opaque`); `RegisterEcosystem` requires `Normalize` and `VersionSystem` to be set. Because `RegisterEcosystem` replaces the preloaded spec, the table above is authoritative over the scaffold's generic `purl.Normalize` where they differ (git-module hosts for Terraform, Helm repositories, Swift repository names).

### 3.3 Types this unit adds

Per ecosystem (compiled; shown for NuGet and Terraform, the two with extra keys; the other nine have only `Enabled` except Actions `AliasNodeBudget int \`yaml:"alias_node_budget"\``). YAML tags exist only on the ecosystem `Config` types, which `cmd/scp` decodes directly (strict, `KnownFields`); the provider `Config` types below carry no tags because `cmd/scp` decodes one flat `ProviderConfig` and maps it (section 6):

```go
// package nuget
const (
	ParserCSProj         = "nuget/csproj"
	ParserProps          = "nuget/props"
	ParserPackagesConfig = "nuget/config"
	ParserLock           = "nuget/lock"
	ParserNuspec         = "nuget/nuspec"
)
type Config struct {
	Enabled        bool `yaml:"-"`               // derived by cmd/scp from manifests.acquisition.disabled_ecosystems (RR-22), no YAML key
	MaxImportDepth int  `yaml:"max_import_depth"` // default 8
	MaxImports     int  `yaml:"max_imports"`      // default 64 files followed per unit
}
func DefaultConfig() Config
func (c Config) Validate() error
func Spec() manifests.EcosystemSpec
func Register(r *manifests.Registry, cfg Config) error

// package terraform
type Config struct {
	Enabled      bool              `yaml:"-"`
	MaxTFBytes   manifests.ByteSize `yaml:"max_tf_bytes"`   // default 256 KiB per .tf/.tofu/.hcl file
	MaxHCLDepth  int               `yaml:"max_hcl_depth"`  // default 256: open brackets, braces, parens, quotes, template interpolations/directives and heredocs, counted on the token stream (a string with one `${` counts 2)
	MaxUnaryRun  int               `yaml:"max_unary_run"`  // default 256 (longest run of '!' or '-')
	MaxTernaries int               `yaml:"max_ternaries"`  // default 4096 ('?' tokens per file)
}
```

Providers (compiled):

```go
// package manifests (source.go, P5)
const (
	SourceOwn         = ""
	SourceSBOM        = "sbom"
	SourceGitHubGraph = "github-graph"
	SourceDepsDev     = "deps-dev"
)
func Rank(source string) int // own 0, sbom 1, github-graph 2, deps-dev 3, unknown 9

type Provenance struct {
	Source  string    // a Source* constant
	Ref     string    // report id, cursor or request path, audit only
	At      time.Time // from the injected clock
	Licence string    // data licence to attribute, "CC-BY-4.0" for deps.dev
}
type FactKind uint8
const (
	FactTransitive FactKind = iota + 1 // adds a locked-style entry and an optional parent edge
	FactLicence                        // fills an empty licence list
	FactLatest                         // fills an empty "latest version"
	FactDeprecated                     // marks a version deprecated
)
type ProvidedFact struct {
	Kind       FactKind
	UnitDir    string      // Unit.Dir the fact attaches to
	Parent     ComponentID // FactTransitive: who depends on Target; zero value = the unit root
	Target     ComponentID
	TargetName string      // original-case name when Target was case-folded
	Version    string      // FactTransitive, FactLicence, FactDeprecated
	Latest     string      // FactLatest
	Licences   []string
	Prov       Provenance
}
type FactsRequest struct {
	Repo       scm.RepoRef
	Ref        string          // branch or commit SHA the units were parsed at
	TreeSHA    string
	Visibility scm.Visibility
	Units      []Unit
	IsPrivate  func(ComponentID) bool // org-produced or matching a private prefix; never nil
}
type DependencySource interface {
	Name() string
	Facts(ctx context.Context, req FactsRequest) ([]ProvidedFact, error)
}
type Enriched struct {
	UnitDir    string
	Target     ComponentID
	Latest     string
	Licences   []string
	Deprecated bool
	Prov       Provenance
}
type MergeStats struct{ Added, Filled, Shadowed, Conflicts int }
func Merge(units []Unit, facts []ProvidedFact) (merged []Unit, enriched []Enriched, st MergeStats)

// Provider failures (RR-20): distinct types, classified by the engine through their method sets.
type RateLimitError struct{ Op string; At time.Time; Err error }   // RetryAt() time.Time
type PermanentError struct{ Op, Reason string; Err error }         // Permanent() bool { return true }
type GoneError struct{ Op string; Err error }                      // Gone() bool { return true }

// Enrichment jobs (root file enrich.go, P5).
const (
	JobKindEnrichGitHub  = "manifests.enrich"          // Upstream "github": githubgraph, sbom
	JobKindEnrichDepsDev = "manifests.enrich.depsdev"  // Upstream "deps-dev": depsdev
)
type EnrichConfig struct {
	TTL             time.Duration `yaml:"ttl"`
	IdleBackoffCap  time.Duration `yaml:"idle_backoff_cap"`
	MaxUnitsPerRepo int           `yaml:"max_units_per_repo"`
}
type EnrichDeps struct {
	GitHub  []DependencySource // run, in order, by manifests.enrich
	DepsDev []DependencySource // run by manifests.enrich.depsdev
	Now     func() time.Time
}
// EnrichSpecs returns the job specs to register: none when both lists are empty (a disabled provider registers nothing).
func (s *Service) EnrichSpecs(cfg EnrichConfig, d EnrichDeps) []job.Spec
```

`Enriched` is not written into `Unit`; it lives in `RepoFacts.Enriched` (RR-19) so that the own parse output stays reproducible from blobs alone.

Invariants: `Merge` never receives, and the run function never passes to a provider, a unit that already contains provider-sourced entries: the run starts from the *own* units (4.4.2) so a second run cannot see its own earlier additions as "present"; a `ProvidedFact` with a zero `Target` is dropped by `Merge` (counted `Shadowed`); `Prov.Source` must not be `SourceOwn`; `Prov.At` is never the zero time; `Licences` hold SPDX expressions only (providers drop `NOASSERTION` and `NONE`); `Merge` is a pure function and idempotent (`Merge(Merge(u,f),f)` equals `Merge(u,f)`). JSON tags are not needed: these types are persisted only inside the store item `manifests.enrich` (section 5) through `encoding/json` with default field names, versioned by the item's `schema` integer.

Provider ports and `Deps` (compiled):

```go
// package githubgraph
type GraphQLDoer interface {
	GraphQL(ctx context.Context, repo scm.RepoRef, query string, vars map[string]any) (json.RawMessage, error)
}
type Config struct {
	Enabled          bool
	Timeout          time.Duration // whole Facts call, default 60s
	ManifestsPage    int           // default 10
	DependenciesPage int           // default 100
	MaxPages         int           // default 20 requests per repository
	MaxDependencies  int           // default 5000 dependencies read per repository
}
type Deps struct {
	Registry *manifests.Registry
	GraphQL  GraphQLDoer
	Meter    metric.Meter
	Tracer   trace.Tracer
	Now      func() time.Time
}
func New(cfg Config, d Deps) (*Provider, error)

// package sbom
type SBOMReader interface {
	GenerateSBOMReport(ctx context.Context, repo scm.RepoRef) (reportID string, err error)
	FetchSBOMReport(ctx context.Context, repo scm.RepoRef, reportID string, maxBytes int64) (body []byte, pending bool, retryAfter time.Duration, err error)
}
type Config struct {
	Enabled       bool
	PollInterval  time.Duration // floor between polls, default 5s; Retry-After wins when larger
	PollTimeout   time.Duration // default 2m
	MaxBytes      int64         // default 32 MiB
	MaxPackages   int           // default 50000
	RedirectHosts []string      // optional allow-list of the 302 target host
}
type Deps struct {
	Registry *manifests.Registry
	Reader   SBOMReader
	Meter    metric.Meter
	Tracer   trace.Tracer
	Now      func() time.Time
	Sleep    func(ctx context.Context, d time.Duration) error // injected: tests use testing/synctest or a fake
}

// package depsdev
type Config struct {
	Enabled               bool
	BaseURL               string        // default https://api.deps.dev
	Systems               []string      // default [nuget rubygems]
	RPS                   float64       // default 2
	Burst                 int           // default 4
	Concurrency           int           // default 2
	CacheTTL              time.Duration // default 24h
	NegativeTTL           time.Duration // default 1h
	RequestTimeout        time.Duration // default 10s
	MaxCoordinatesPerRepo int           // default 300
	PrivateGroupPrefixes  []string
	IncludePrivateRepos   bool          // default false
}
type Cache interface {
	Get(ctx context.Context, key string) (val []byte, fresh bool, found bool, err error)
	Put(ctx context.Context, key string, val []byte, ttl time.Duration) error
}
type Deps struct {
	Registry *manifests.Registry
	HTTP     *http.Client // SSRF-guarded; transport dials only the configured host
	Cache    Cache
	Meter    metric.Meter
	Tracer   trace.Tracer
	Now      func() time.Time
}
```

Zero-value behaviour: every `Config` zero value is *disabled*; `DefaultConfig()` returns the documented defaults with `Enabled` set for ecosystems (true) and providers (false); `Validate` rejects out-of-range numbers (4.x) and, for an enabled provider, a missing collaborator in `Deps` at `New` time (typed error `manifests.ErrProviderConfig`, wrapping the field path), so a mis-wired provider fails startup rather than running silently empty.

---

## 4. Behavioural specification

### 4.1 Shared parser contract (applies to every parser in this unit)

#### 4.1.1 Skeleton

Every parser is the same five steps; only `scan` differs. Pseudocode (the shape is normative, the helper names are not):

```
Parse(ctx, f, lim):                       // lim is already resolved (RR-5): no zero fields
  var out Parsed                          // File is stamped by the runner (4.1.9); the parser never reads f.Path
  b := builder{out: &out}                 // diagnostic cap, manifests.CleanText on every string, no path anywhere
  data, err := limits.Text(f.Data)        // BOM stripped; UTF-16 / NUL / invalid UTF-8 rejected (XML parsers: StripBOM + RejectBinary + CharsetReader)
  if err != nil { b.diag("encoding.unsupported", reason(err), 0); return out, nil }
  out.Kind = kindOf(data)                 // from the CONTENT or fixed per parser, never from the file name
  g := limits.NewGuard(ctx, lim)
  if g.CheckSize(len(data)) != nil { b.diag("limit.size", "", 0); return out, nil }
  err = scan(g, data, &b)                 // ecosystem specific; polls g.Node() per token
  if ctx.Err() != nil { return out, ctx.Err() }            // a done context ALWAYS wins: the scaffold's Guard reports it as LimitError{timeout}
  var le *limits.LimitError
  if errors.As(err, &le) { b.diag("limit."+le.Limit, "", b.line); err = nil }
  if err != nil { b.diag(eco+".malformed", "", b.line) }   // anything else that escaped scan is a bug-shaped input: recovered, never fatal
  return out, nil
```

Rules that follow from the skeleton: (0) a parser's output is a pure function of `f.Data` (4.1.9); (1) a parser never calls `os`, `net`, `time.Now`, `math/rand`, `log/slog` or any global, and never reads `f.Path`; a parser has no metrics (the harness counts outcomes and `limit.*` diagnostics, RR-4); (2) `scan` polls `g.Node()` (the logical `Tick`) once per token/line/entry (a tight loop without a poll is a defect found by the fuzz time bound); (3) third-party decoders that cannot poll (`yamlsafe.Decode`, `hclsyntax.ParseConfig`, `encoding/json` on one value) are called only after the size and shape checks of the ecosystem section, so their worst case is bounded in advance; (4) all output strings pass through `manifests.CleanText(s, 512)`; (5) output order is a function of input bytes only (no map iteration order leaks into slices: maps are iterated through sorted keys or the source order is retained by a parallel slice).

#### 4.1.2 `Match` and excluded directories

`Match(path)` is a pure function of the slash-separated repo-relative path, cheap (no regexp compile per call: patterns are compiled once in the constructor, which G5 allows as logically constant) and never allocates on the negative path. File-name comparison is case-sensitive except for NuGet (Windows-origin files, `strings.EqualFold`). Every parser first rejects paths that contain a vendored/generated directory segment of its ecosystem, because those directories hold hundreds of foreign copies of exactly the files we match:

| Ecosystem | Excluded segments |
|---|---|
| nuget | `obj`, `bin`, `node_modules`, `.nuget` |
| ruby | `vendor`, `node_modules`, `.bundle` |
| php | `vendor`, `node_modules` |
| swift | `.build`, `Pods`, `Carthage`, `node_modules`, `DerivedData` |
| cocoapods | `Pods`, `Carthage`, `node_modules` |
| dart | `.dart_tool`, `.pub-cache`, `.symlinks`, `node_modules`, `build` |
| elixir | `deps`, `_build`, `node_modules` |
| docker, actions | `node_modules`, `vendor` |
| terraform | `.terraform`, `.terragrunt-cache`, `node_modules` |
| helm | `node_modules` (vendored `charts/<sub>/Chart.yaml` are handled by the assembler, 4.2.11) |

The root's allow-list (13.4 step 1) is the union of all `Match` functions; this unit assumes no global vendor exclusion (the root may add one, which only removes work).

#### 4.1.3 Determinism, ordering, duplicates

`Dependencies` are in source order; `Locked` is in source order for formats that have an order (Gemfile.lock, composer.lock, Podfile.lock, pubspec.lock sorted by name as written, mix.lock) and `DependsOn` lists are sorted ascending. Duplicate `Dependency` values that are equal on `(Target, Scope, Group, Condition, Requirement)` keep the first occurrence and add one `<eco>.duplicate` diagnostic per file (not per duplicate). `Locked` entries are unique per `(ID, Version)`.

#### 4.1.4 `yamlsafe` (RI-6, D-1)

Why: four ecosystem packages decode YAML; alias bombs, depth bombs and document floods must be handled identically and fuzzed once. The package decodes with `go.yaml.in/yaml/v3` v3.0.5 into the library's `yaml.Node` tree (never into Go values: Node decoding does not expand aliases and does not trip the library's alias-ratio check, E-B6) and converts it to the own `Node` (compiled signature in 18) while enforcing:

- Decoding loop: `yaml.NewDecoder(bytes.NewReader(data))` and `Decode(&node)` until `io.EOF` or `MaxDocs` (`yaml.Unmarshal` into a `yaml.Node` reads **only the first document**, verified; a 100,000-document stream of `---` therefore must not be decoded by `Unmarshal` and is cut at `MaxDocs` by the loop).
- Document cap `MaxDocs` (default 8; `pnpm`-style multi-document files are the other unit's concern, `Chart.yaml`/lock/action/workflow files are single-document): more documents produce `Problem{Code:"yaml.docs-capped"}`.
- `MaxDepth` (default 256) and `MaxNodes` (default 2,000,000) counted on the *converted* tree. The library itself refuses nesting deeper than 10,000 with a syntax error (measured: a 100,000-deep flow sequence is rejected in 3.8 ms, E-B6); our cap is lower, applied during conversion.
- **Aliases**: when `AliasBudget == 0`, an alias node is not followed and yields `Problem{Code:"yaml.alias-unsupported"}`, the aliased position becoming an empty scalar. When `AliasBudget > 0` (Actions only, 4.2.9) an alias is expanded by copying the target subtree, charging every copied node to one per-file counter; exceeding the budget stops expansion with `Problem{Code:"limit.alias"}`. Merge keys (`<<`) are never expanded (`Problem{Code:"yaml.merge-ignored"}`). **Alias cycles**: the library accepts `a: &a [*a]` and `a: &a {k: *a}` (verified 2026-10-14, v3.0.5: the Node tree then contains a pointer cycle and no error), so expansion keeps the stack of anchors being copied; an alias whose target is on that stack is not followed and yields `Problem{Code:"yaml.alias-cycle"}`. The depth cap applies to every copy as well, so a cycle that slipped through still stops at `MaxDepth`.
- Mapping keys must be scalars; a non-scalar key skips the pair with `yaml.non-scalar-key`. Duplicate keys are kept in order (`Get` returns the first).
- Scalars keep the *resolved short tag* so that `on:` stays `!!str` and `version: 1.10` is distinguishable from `"1.10"` (YAML 1.1 booleans `yes/no/on/off` stay strings in yaml.v3 node resolution, E-B6).
- Syntax errors yield the documents decoded so far plus `Problem{Code:"yaml.syntax", Line}`; the decoder's panics are recovered inside `Decode` (the library converts them, but the guard is kept) and reported as `yaml.syntax`.
- Line comments are captured on scalar nodes (`LineComment`) because Actions needs the `# v7.0.1` that follows a pinned SHA.

Ecosystems map `Problem` values 1:1 to diagnostics (`yaml.*` codes are shared, `limit.*` go through RR-4).

#### 4.1.5 `rubydsl` (RI-7, D-1)

Ruby DSL files are programs; we only *scan* them. The scanner produces the tokens listed in 18 (compiled stub) and obeys: comments (`#`, `=begin`/`=end`, `__END__`) dropped; `'...'` strings literal (only `\\` and `\'` escapes); `"..."` strings literal unless they contain `#{` (then `Interp=true`, never a literal); `%w[]`/`%i[]`/`%q()`/`%Q()` literals tokenised (`Words` for `%w`/`%i`); symbols `:name` and `:"x"`; `name:` and `:name =>` both produce `Key` on an `Arg`; heredocs (`<<~EOS`, `<<-EOS`, `<<EOS`) are consumed to their terminator and emitted as one `Heredoc` token (their body is never scanned); line continuation after `,`, `\`, a trailing binary operator and inside brackets; statement separators `\n` and `;` become `Newline`. `Scan` calls `tick()` once per token and returns its error. `Call(toks, i)` reads the arguments of `ident a, b: c` or `ident(a, b: c)` until `Newline`, a `do`/`{` block opener at depth 0 or the closing parenthesis; nested brackets inside an argument stay inside `Arg.Value`. `Literal(v)` returns the value of a one-token literal string/symbol without interpolation. Anything else is "dynamic": the caller emits `<eco>.dynamic` and skips the call.

#### 4.1.6 `purl.RepoKey` (RI-4) rules

The one canonicaliser of repository URLs is the core's `purl.RepoKey(raw) (key string, ok bool)` (this document's earlier `purl.RepoURL` is the same function under a rejected name). Input forms handled: `https://github.com/Org/Repo.git`, `http://...`, `ssh://git@github.com/Org/Repo.git`, `git@github.com:Org/Repo.git`, `git://host/path`, `git+https://...`, `github:owner/repo`, the bare shorthand `owner/repo`, and Terraform's `git::https://...//subdir?ref=v1` after the Terraform parser has stripped `git::`, the `//subdir` and the query (the parser keeps the subdir in `Qualifiers "subpath="` and the ref in `Requirement`). Output `host/owner/repo`, **lower-cased as a whole** (GitHub owner and repository names are case-insensitive, and the graph's repo index is keyed by the lower-cased form), scheme, userinfo, ports, query, fragment, a trailing `.git` and any path beyond the first two segments removed. `ok=false` for `file:`, relative paths, an empty host and anything over 2,048 bytes. A scheme-less `host/owner/repo` (Terraform's `github.com/org/repo` module shorthand) is **not** accepted by the scaffold's function (`url.Parse` finds no host; checked), so the parser prepends `https://` before the call (and the parser calls it **only on values it already knows to be git URLs**: the bare `owner/repo` shorthand would turn any `a/b` string, for example a registry coordinate, into `github.com/a/b`; verified: `Org/Repo` -> `github.com/org/repo`; a GitLab subgroup path is cut to two segments, acceptable because SCP is GitHub-only); the idempotence property of 12.4 is therefore `RepoKey("https://" + RepoKey(x)) == RepoKey(x)`. Consequences for this unit: `SourceRef` of a git dependency is this key (not a URL); a Swift package at `https://github.com/Apple/swift-nio.git` has `SourceRef github.com/apple/swift-nio` and `ComponentID{swift, "github.com/apple", "swift-nio"}` (4.2.4); channel-B equality is plain string equality. Self-hosted GitHub Enterprise hosts work the same way (host is part of the key).

#### 4.1.7 Diagnostic catalogue conventions

Codes are `<ecosystem or shared>.<kebab-case>`. Shared: `encoding.unsupported`, `limit.size|depth|nodes|expand|string|alias|diag|timeout`, `diag.truncated`, `yaml.syntax|alias-unsupported|alias-cycle|merge-ignored|non-scalar-key|docs-capped`, `assemble.path-escape` (runner or J-0, not a parser code), `enrich.units-capped|coordinates-capped|conflict|locked-missing`. Per-ecosystem codes are listed in each section's Diagnostics table; the test plan has one golden per code (12.2). Messages never contain input text longer than 64 bytes (prompt-injection and log-injection hygiene: diagnostics reach `dependencies.md`).

#### 4.1.8 Fixtures, licences and provenance of test data

All real-world fixtures were fetched on 2026-10-14 from public repositories whose licence permits redistribution in tests (MIT, Apache-2.0, BSD); each listed with size. At implementation, files are re-fetched at a pinned commit SHA and recorded in `testdata/SOURCES.tsv` (`path<TAB>upstream-url@sha<TAB>licence<TAB>sha256`), and the licence texts are added to `THIRD_PARTY.md` (architecture 17 supply-chain row). **No MPL-licensed or unlicensed file is used**: where only MPL/unlicensed examples exist (real `.terraform.lock.hcl`, Terraform module calls, Mix umbrellas, Dart workspaces, Swift v1/v2 `Package.resolved`) the fixture is hand-written from the format documentation and flagged `synthetic` in `SOURCES.tsv`.


#### 4.1.9 Path independence of parser output (verification finding V-F2)

The parse cache key is `(blobSHA, parserID, parserVersion)` (architecture 13.4, ADR-021): **the path is not part of it**. Two identical files at different paths (`charts/a/Chart.yaml` and `charts/b/Chart.yaml` with the same bytes, a copied `Dockerfile`, a vendored `Package.swift`) share one cached `Parsed`. Revision 1 of this document let parsers emit repo-relative paths (`_local/<repo-relative path>`, `Loc.Path`, `SourceRef`, `Members`), which would have served the first file's paths to every later copy. Binding rules:

1. A parser reads `f.Data` only. It never reads `f.Path` (not even the base name or extension); `Match` is the only function that sees paths. Behaviour that differed by extension in revision 1 is decided from content: NuGet `Kind` (`KindCatalog` when the file has no `PackageReference` item), the Podspec JSON form (`*.podspec.json`: the first non-space byte is `{`), Terraform `.tofu` (the default registry host folds to `registry.terraform.io` either way, OQ-10, so there is nothing to decide). `Parsed.File` and `Location.Path` are stamped by the runner.
2. Path-like values are emitted **relative to the declaring file's directory**, `path.Clean`ed, never absolute; an absolute path or a `file:` URL with a host is dropped with a diagnostic. `SourceRef` of a `SourcePath` dependency, the `Name` of a `_local` target, and every `Parsed.Members` entry follow this rule (RR-10, 2.5 row 7). Escapes (`..` beyond the repository root) can only be judged by whoever knows the directory: the runner (`rebasePaths`) and the join assembler (J-0) emit `assemble.path-escape`.
3. Exception: GitHub Actions `uses: ./x` and `$/x` are repository-root relative by definition, so the Actions parsers emit them repo-relative with an empty `SourceRef`.
4. Anything that needs the file's own name or place is computed by an assembler from `Parsed.File.Path`: the NuGet default `PackageId`/`AssemblyName` (file name without extension), the `packages.<project>.lock.json` preference, `Directory.Build.props` chains, Swift Xcode `UnitDir`, Helm vendored-subchart `Skip`.
5. Test: invariant 8 of `parsertest` for this unit, **`TestParserPathIndependence`**: every golden input is parsed under two different valid paths that `Match` accepts (`a/<base>` and `deep/er/<base>`) and the two `Parsed` values (with `File` zeroed) must be `reflect.DeepEqual`. A parser that smuggles a path in fails here on its first golden.
---

### 4.2 Ecosystems

#### 4.2.1 NuGet (`ecosystems/nuget`)

**Files and match** (names compared with `EqualFold`):

| Parser | Matches | Kind |
|---|---|---|
| `nuget/csproj` | `*.csproj`, `*.fsproj`, `*.vbproj` | `KindManifest` |
| `nuget/props` | `Directory.Build.props`, `Directory.Build.targets`, `Directory.Packages.props`, and `*.props` whose base name matches `(?i)^([a-z0-9._-]*[._-])?(versions?\|dependencies\|packages\|version\.details)\.props$` (covers `eng/Versions.props`, `eng/Dependencies.props`) | `KindCatalog` when the content has no `PackageReference` item (`KindManifest` otherwise): decided from the bytes, never from the file name (4.1.9) |
| `nuget/config` | `packages.config` | `KindManifest` |
| `nuget/lock` | `packages.lock.json`, `packages.*.lock.json` | `KindLockfile` |
| `nuget/nuspec` | `*.nuspec` | `KindManifest` |

Import targets that no `Match` selects are never fetched (the allow-list drives the blob fetch), so an `<Import>` of such a file stays unresolved (`nuget.import-unavailable`); this is accepted: it keeps parsing pure and bounded, and the common cases (`Directory.Build.props`, `eng/Versions.props`) are matched.

**XML reading** (shared by csproj, props, config, nuspec): a token loop over `encoding/xml` with `Strict=true`, `Entity=nil` (any `<!ENTITY>` use yields "invalid character entity" and ends the parse with `nuget.malformed`; no entity is ever expanded, so no billion-laughs vector exists, E-B3), `CharsetReader` accepting exactly `iso-8859-1`, `windows-1252` (via `charmap.ISO8859_1`, `charmap.Windows1252`) and rejecting every other declared charset with `encoding.unsupported`; `utf-16` is rejected by `CleanText` before the decoder sees it. Depth is `g.Enter()/Leave()` per `StartElement/EndElement` (a 100,000-deep document aborts at depth 257 with `limit.depth` in microseconds, E-B3); `g.Tick()` per token. Element and attribute names are compared case-insensitively (MSBuild property and item names are case-insensitive). Measured: a 225-item, 32 KB `Directory.Packages.props` parses in 0.3 ms; 200,000 `PackageReference` elements (8.6 MB, beyond the 1 MiB limit, parsed only to measure) took 116 ms.

**Grammar handled**

- `Project[@Sdk]`; `PropertyGroup` children as properties (unconditional groups: stored in `Parsed.Properties` under the lower-cased name, last definition in document order wins; conditional groups: names recorded in `Properties["cond:<name>"]="1"` so the assembler can say "defined only under a condition").
- `ItemGroup` children: `PackageReference` (`Include` with `;`-separated lists; `Version` attribute or `<Version>` child; `VersionOverride`; `PrivateAssets`; `Condition`), `PackageVersion` (`Include`, `Version`; `Update` treated as `Include`), `GlobalPackageReference`, `ProjectReference` (`Include` path with `\` or `/`), `Import` (top-level, `Project` literal), `Choose/When/Otherwise` (descended; items found under a `When` carry `Condition` = the `When`'s `Condition`, under `Otherwise` the literal `otherwise`).
- `Import` elements (top level, or inside `Choose` branches): recorded as `Properties["import.<n>"] = <Project literal>` (and `Properties["import.<n>.cond"]` when a `Condition` exists), in document order, for the assembler; the unconditional ones that name a literal path (after removing a leading `$(MSBuildThisFileDirectory)`) are also appended to `Parsed.Includes` (2.5 row 8); the parser follows nothing.
- Properties read: `PackageId`, `AssemblyName`, `Version`, `VersionPrefix`, `VersionSuffix`, `PackageVersion`, `IsPackable`, `IsTestProject`, `OutputType`, `PackAsTool`, `TargetFramework`, `TargetFrameworks`, `PackageLicenseExpression`, `ManagePackageVersionsCentrally`, `CentralPackageVersionOverrideEnabled`.
- `packages.config`: `<package id version targetFramework developmentDependency allowedVersions>`.
- `packages.lock.json`: shape-detected (not version-number-detected): if the root has a `dependencies` object whose children are target-framework keys (v1, v2) read them; else if the root has other object-valued keys that each contain `dependencies` (v3 with aliases, `AliasedVersion = 3` in `PackagesLockFileFormat.cs`, V-12; re-read 2026-10-14: NuGet's own `ReadTargetV3` reads every root key except `version` as `alias` or `alias/rid`, requires a non-empty `framework` string inside it and skips the key otherwise, then reads `dependencies`; v1/v2 keys under `dependencies` are `tfm` or `tfm/rid`) read those, skipping a v3 key without `framework` exactly as NuGet does (`nuget.lock-unreadable` only when nothing was readable at all); unknown shapes yield `nuget.lock-unreadable`. Entry fields: `type` (`Direct`, `Transitive`, `Project`, `CentralTransitive`), `requested`, `resolved`, `contentHash`, `dependencies` (name to range).
- `.nuspec`: `<metadata>` `id`, `version`, `license[@type=expression]`, `licenseUrl` (ignored), `<dependencies>` with `<group targetFramework>` and `<dependency id version exclude>`; `developmentDependency`.

**Grammar not handled** (each produces a diagnostic where a dependency may be lost): MSBuild `Condition` evaluation, property functions `$([MSBuild]::...)`, environment variables, item metadata batching, `Compile`/`Reference`/`HintPath` assembly references, `Sdk` attribute resolution (the SDK's implicit packages such as `Microsoft.NETCore.App` are not dependencies in this model), `.targets` property effects, `Directory.Build.rsp`, `global.json`, `nuget.config` sources (private feeds are not read: a custom feed is invisible, so a package from a private feed looks like a public NuGet.org id; documented limitation), `packages.config` `<reference>` HintPaths, `.sln` files, `Update`/`Remove` item operations on `PackageReference` (diagnosed `nuget.item-update-ignored`).

**Mapping**

| Source | Model |
|---|---|
| `PackageReference` (non-CPM, `Version="1.2.3"`) | `Dependency{Target nuget:<lower id>, TargetDisplay id, Requirement "1.2.3", Version "1.2.3" (RR-12: NuGet's lowest-applicable-version rule makes the minimum the restored version unless another reference raises it), Resolution ResDeclared, Scope per below, Group "PackageReference"}` |
| `Version="[1.2.3]"` | `Version "1.2.3"`, `ResPinned` |
| `Version="[1.0,2.0)"`, `1.*`, `(,1.0]` | `Requirement` raw, `Version ""`, `ResDeclared` |
| `Version` empty and CPM active | `Version` from `PackageVersion`, `Requirement` = that item's raw version, `ResManaged` |
| `VersionOverride="x"` | `Version`/`Requirement` from the override, `ResDeclared`, `Group "VersionOverride"` |
| `Version="$(Prop)"` resolved by the assembler | `Version` expanded value, `ConfHeuristic`, `ResDeclared` (or `ResManaged` through CPM) |
| `Version="$(Prop)"` unresolved | `Requirement "$(Prop)"` raw, `Version ""`, diagnostic `nuget.unresolved-property` (one per distinct property name per unit) |
| `PrivateAssets` containing `all`, or `developmentDependency="true"` | `ScopeDev`, `Group` keeps the original element name plus `;PrivateAssets=all` |
| `GlobalPackageReference` | added to every project under the file's directory, `ScopeDev`, `Group "GlobalPackageReference"` |
| `Condition` on item or enclosing group/`When` | `Dependency.Condition` raw (never evaluated) |
| `ProjectReference Include` | assembler (it knows the project's own path, 4.1.9): `Dependency{Target nuget:_local/<repo-relative directory of the referenced project>, Source SourcePath, SourceRef <the same path>}`; an `Include` that escapes the repository root is dropped with `assemble.path-escape` |
| csproj as producer | **Computed by the assembler, not the parser** (the default `PackageId` is the file name, which a parser may not read, 4.1.9; CPM and test-SDK facts also live outside the file): `Artifact{ID nuget:<lower PackageId>, Display PackageId, Version, Kind}` where `PackageId` defaults to `AssemblyName`, then the file name without extension; `Version` from `PackageVersion`, else `Version`, else `VersionPrefix[-VersionSuffix]`; `Kind` `library` (default), `application` (`OutputType` `Exe`/`WinExe`, and `PackAsTool=true`, RR-23: the closed API enum has no `tool`). The parser stores the raw property values in `Parsed.Properties` for it. Not provided when `IsPackable=false` (literal), `IsTestProject=true`, a dependency on `Microsoft.NET.Test.Sdk` exists, or the project is an `Exe` without literal `IsPackable=true` or `PackAsTool=true` (SDK default `IsPackable=true` is documented, V-11, but an app that nobody packs would create false producer edges) |
| `TargetFramework(s)` | `Toolchain["tfm"]` = the literal value (semicolon list kept) |
| `PackageLicenseExpression` / nuspec `license type=expression` | `Parsed.Licenses` |
| `packages.lock.json` entry | `LockedComponent{ID, Version=resolved, Integrity "sha512:<hex of base64 contentHash>" (64 decoded bytes, V-12 and the PowerApps fixture), Relationship Direct for `Direct`, Transitive for `Transitive` and `CentralTransitive`, Scope runtime, DependsOn from the entry's `dependencies` names matched within the same TFM}`; `Project` entries are skipped (no diagnostic: they are intra-repo edges from `ProjectReference`); entries repeated across TFMs are merged by `(ID, Version)` |

Test-project scoping: if `IsTestProject=true` or `Microsoft.NET.Test.Sdk` is referenced, the assembler maps `ScopeRuntime` dependencies of that unit to `ScopeTest` and marks the unit `Kind "test"` (no `Provides`).

**Identity and purl.** Lower-cased `Name`; `Display` keeps the original; `FormatPURL` emits `pkg:nuget/<display>@<version>` (purl-spec nuget type is case-preserving, V-8), falling back to the lower-cased name when no display exists.

**Confidence.** `ConfExact` for literal versions, CPM-managed versions and lock data; `ConfHeuristic` when the version came through property expansion or an `Import`.

**Limits and hardening.** File caps from RR-5 (props and csproj 1 MiB, lock 8 MiB); depth 256; expansion: `Expand(n)` is charged for every substituted value, per-value 1 MiB and per-unit 8 MiB (the MSBuild analogue of the 700-byte POM that produced 160 MB, research 07 E5); `MaxImportDepth` 8 and `MaxImports` 64 per unit; import cycles detected by a visited set; property definition cycles detected by in-progress marking (`nuget.property-cycle`); no entity expansion; `Include` lists split at most 1,000 items.

**Diagnostics**

| Code | When |
|---|---|
| `nuget.malformed` | XML/JSON syntax error (recovered part kept) |
| `nuget.unresolved-property` | `$(Name)` left in a version or id (one per distinct name per unit) |
| `nuget.unresolved-id` | `Include` contains `$(` after expansion: reference skipped |
| `nuget.property-function` | `$([...]` encountered: not expanded |
| `nuget.property-cycle` | self-referential property chain |
| `nuget.conditional-property` | a used property is defined only under a condition |
| `nuget.import-unavailable` / `nuget.import-cycle` | assembler could not follow or detected a cycle |
| `nuget.item-update-ignored` | `Update`/`Remove` on `PackageReference` |
| `nuget.cpm-assumed` | CPM treated as active because a `Directory.Packages.props` exists and no literal `ManagePackageVersionsCentrally=false` was found (aspnetcore sets it conditionally, V-14) |
| `nuget.multiple-projects` | two project files in one directory merged into one unit |
| `nuget.lock-unreadable` | unknown lock layout |
| `nuget.lock-stale` | a lock entry's `Direct` set differs from the manifest's declared set |

**Assembly (dedicated `nugetAssembler`)**: specified as ordered steps (inputs: all NuGet `Parsed`, `FileSet`).

```
Assemble(files, parsed):
  byDir := index parsed by dir (path.Dir); props := parsed with parser ID nuget/props
  for each project file P (nuget/csproj), in path order:
     env := {}                                                    // property environment, lower-cased names
     env.set(builtins: msbuildprojectname, assemblyname default, packageid default "$(assemblyname)")
     for f in propsChain(P.dir):                                  // outermost first
         env.layer(f.Properties)                                  // unconditional only, later layers override earlier
         for imp in f.imports in order: follow(imp, from=f, depth+1)   // literal paths found among parsed props only
     env.layer(P.Properties)                                      // project body overrides Directory.Build.props
     deps := P.Dependencies + inherited PackageReference items of propsChain (+ GlobalPackageReference)
     cpm  := cpmActive(P, propsChain, nearest Directory.Packages.props chain)
     for d in deps:
         d.id, d.version := expand(d, env, g)                     // memoised, bounded by Guard.Expand
         if d.version == "" && cpm: lookup nearest-first in the Directory.Packages.props chain -> ResManaged
     join packages.lock.json from the same dir (prefer packages.<project>.lock.json): set ResLocked, copy Locked
     merge packages.config / .nuspec of the same dir
     resolve ProjectReference include paths against P.dir -> intra-repo Dependency
     decide Provides (rules above); Kind test/application/library/tool
  units sorted by Dir; one Diagnostic per distinct unresolved property per unit
```

`propsChain(dir)`: the nearest `Directory.Build.props` in `dir` or an ancestor; the walk continues to the next ancestor only when that file contains the literal import `GetPathOfFileAbove('Directory.Build.props', ...)` (the documented way MSBuild chains them), at most `MaxImportDepth` hops. The same rule applies to `Directory.Packages.props`. `follow` resolves `$(MSBuildThisFileDirectory)` to the importing file's directory, converts `\` to `/`, cleans the path, rejects results that escape the repository root, and applies the target's unconditional properties and items at the position of the `Import` (document order). Imports with a `Condition` other than a bare `Exists('...')` guard are skipped with `nuget.conditional-property` unless the project's other imports already supplied the same names.

**Corpus.** (a) `dotnet/aspnetcore` `Directory.Packages.props` (MIT, 32,176 B): 225 `PackageVersion` items, 207 with unresolved `$(...)` versions that come from `eng/Versions.props`, `<Choose><When>` present; and `Directory.Build.props` (MIT, 22,523 B) for the conditional `ManagePackageVersionsCentrally` case. (b) `JamesNK/Newtonsoft.Json` `Newtonsoft.Json.csproj` (MIT, 13,664 B, begins with a UTF-8 BOM): `TargetFrameworks` conditional, `PackageId`, one `PackageReference` with `Version="$(MicrosoftSourceLinkGitHubPackageVersion)"` and `PrivateAssets="All"`. (c) `microsoft/PowerApps-Tooling` `src/PAModel/packages.lock.json` (MIT, 10,058 B): v2, three TFMs (19/5/6 entries), types Direct 8, Transitive 8, Project 3, CentralTransitive 11. (d) `MaximumADHD/Roblox-File-Format` `packages.config` (MIT, 1,079 B, BOM). (e) Synthetic: `Version3.lock.json` with alias keys, an iso-8859-1 csproj, `<!ENTITY>` bomb, 100,000-deep nesting, import cycle, 200,000 references, CPM with `VersionOverride`, `GlobalPackageReference`, and a two-level `Directory.Build.props` chain.

#### 4.2.2 Ruby / Bundler (`ecosystems/ruby`)

**Files and match.** `gem/lock`: `Gemfile.lock`, `gems.locked`. `gem/gemfile`: `Gemfile`, `gems.rb`. `gem/gemspec`: `*.gemspec`. Appraisal-style `gemfiles/*.gemfile(.lock)` are **not** matched (one unit per directory is the join key and they would collide; they are test matrices, not product dependencies). Excluded segments: 4.1.2.

**`gem/lock` grammar handled** (verified against `lib/bundler/lockfile_parser.rb`, ruby/rubygems master `6db466b`, V-1). Sections start at column 0 with an upper-case header: `GIT`, `GEM`, `PATH`, `PLUGIN SOURCE`, `DEPENDENCIES`, `CHECKSUMS` (Bundler 2.5.0+), `CONTENT ADDRESSES` (newer, ignored), `PLATFORMS`, `RUBY VERSION`, `BUNDLED WITH`. Inside source sections: `remote:`, `revision:`, `ref:`, `branch:`, `tag:`, `glob:`, `submodules:`, `specs:`; spec lines have **4 spaces** `name (version[-platform])`, their dependencies **6 spaces** `name (constraint, constraint)` or bare `name`; `DEPENDENCIES` entries have 2 spaces `name (constraint)` with an optional trailing `!` (non-registry source); `CHECKSUMS` lines `  name (version[-platform]) sha256=<hex>[,...]`. Line endings `\n` or `\r\n`. Parsing is a line scanner with a one-token state (current section, current source); no regexp backtracking (the Bundler `NAME_VERSION` shape is hand-matched).

**Mapping.**

| Source | Model |
|---|---|
| spec line | `LockedComponent{ID gem:<name>, Version (platform suffix removed; the first occurrence of a `(name, version)` wins, later platform variants of the same pair are merged), Scope runtime, Loc}`; the enclosing section (GEM, PLUGIN SOURCE, GIT, PATH) is remembered per gem name and applied to the matching `DEPENDENCIES` entry below |
| spec's 6-space dependency | `DependsOn` edge to the locked entry with that name (unknown names: no edge, no diagnostic: platform-specific deps often are absent) |
| `DEPENDENCIES` entry | `Dependency{Target, Requirement=constraint text, Version=locked, Resolution ResLocked, Scope ScopeUnknown, Group "DEPENDENCIES", Source from the spec's section: GIT -> `SourceGit` with `SourceRef = purl.RepoKey(remote)` and `Requirement` extended by `revision <sha>`, PATH -> `SourcePath` with the cleaned `remote` relative to the lock file's directory (4.1.9), GEM -> `SourceRegistry` (a non-default `remote` becomes `SourceRef`)}`; the matching `LockedComponent.Relationship = RelDirect`, all others `RelTransitive` |
| `CHECKSUMS` | `Integrity "sha256:<hex>"` on the matching locked entry (platform variant chosen = the one kept) |
| `RUBY VERSION` (`ruby 3.3.0p0`) | `Toolchain["ruby"]="3.3.0"`; `BUNDLED WITH` -> `Toolchain["bundler"]`; `PLATFORMS` -> `Properties["platforms"]` comma-joined |

Measured on the Rails `Gemfile.lock` (E-B4): 269 spec lines = 233 distinct gems + 36 platform variants; 397 dependency edges; 82 direct dependencies; 256 checksums; 5 platforms; `BUNDLED WITH 4.0.12`; a Bundler 2.5-style file with `CHECKSUMS` (112 specs, 111 checksums) also parses.

**`gem/gemfile` grammar handled** (`ConfHeuristic`, via `rubydsl`): `gem "name"[, "req"[, "req"...]][, opts]` with options `group:`/`groups:`, `git:`/`github:`/`gist:`/`bitbucket:`, `branch:`, `tag:`, `ref:`, `path:`, `platforms:`/`platform:`, `require:` (ignored), `source:`, `require_paths`; `group :a, :b do ... end` (nesting tracked by `do`/`end` counting at statement level); `source "url" do ... end`; `ruby "3.3.0"` / `ruby file: ".ruby-version"` (the latter: `ruby.dynamic`); `gemspec` (sets `Properties["gemspec"]="1"`, assembler merges the sibling gemspec); `git_source(:x) { ... }` (diagnosed `ruby.dynamic`, subsequent `x:` options are kept raw); `platforms :jruby do ... end` (`Condition = "platforms:jruby"`). **Not handled** (each `ruby.dynamic` once per file): `eval_gemfile`, `instance_eval`, `if/unless/case` around gems (gems inside are still emitted with `Condition="ruby-conditional"` when the opener is on a line starting with `if`/`unless`), loops, string interpolation in a gem name or requirement, `Dir`/`File` calls, `ENV[...]` (an `ENV.fetch` default is not evaluated).

Scope from groups: contains `test` -> `ScopeTest` unless `development` is also present (`development, test` -> `ScopeDev`); `development` -> `ScopeDev`; `production`/default/anything else -> `ScopeRuntime`; `Group` keeps the raw group list joined with `,`. `Requirement` = the quoted requirement strings joined with `, ` (Bundler's own form); `Version` empty (resolved by the lock join); an exact requirement `"= 1.2.3"`, `"1.2.3"` is **not** treated as pinned (RubyGems treats a bare version as `= 1.2.3`, but Bundler resolves it through the lock; the join sets `ResLocked`).

**`gem/gemspec`**: `Gem::Specification.new do |s| ... end` block; `s.name = "x"` (or `spec.name`), `s.version = "1.0"` (a constant such as `Foo::VERSION` is `ruby.dynamic`: version empty), `s.add_dependency "a", "~> 1"`, `s.add_runtime_dependency`, `s.add_development_dependency`, `s.required_ruby_version = ">= 3.0"`, `s.license = "MIT"` / `s.licenses = [...]`. `Provides: Artifact{gem:<name>, Version, Kind "library"}`; runtime deps `ScopeRuntime`, development deps `ScopeDev`; `Toolchain["ruby"]`; `Parsed.Licenses`.

**Identity.** `gem:<name>` as written (RubyGems names are case-sensitive in the registry, V-8 gem type); `FormatPURL` default (`pkg:gem/<name>@<version>`, plus `?platform=` is never emitted because platform variants are collapsed).

**Limits and hardening.** Lock 8 MiB, Gemfile/gemspec 1 MiB; scanner `Tick` per token; line length cap 64 KiB (a longer line is cut with `limit.string`); at most 50,000 spec lines (`limit.nodes` through the guard); heredocs are skipped, never scanned.

**Diagnostics**: `ruby.malformed` (unrecognised section structure; the recovered part is kept), `ruby.merge-conflict` (the lock contains `<<<<<<<`, `=======` or `>>>>>>>` lines, which Bundler itself rejects; the parser skips the marker lines, keeps both sides' entries de-duplicated by `(name, version)` and reports once), `ruby.dynamic`, `ruby.duplicate`, `ruby.unknown-section` (once per distinct header, so a future Bundler section is visible), `ruby.checksum-unmatched`.

**Assembly**: join (4.3) with `Manifests = Gemfile|gems.rb|*.gemspec`, `LockFor("Gemfile") = ["Gemfile.lock"]`, `LockFor("gems.rb") = ["gems.locked"]`, `LockFor("*.gemspec") = ["Gemfile.lock","gems.locked"]`, `Workspaces=false`. Manifests in one directory merge into one unit (Gemfile + gemspec: the gemspec's runtime deps appear once with `Group "gemspec"`).

**Corpus.** `rails/rails` `Gemfile.lock` (MIT, 43,769 B), `Gemfile` (4,001 B), `activesupport/activesupport.gemspec` (2,109 B); `kpumuk/meta-tags` `gemfiles/rails_8.0.gemfile.lock` (MIT, 18,225 B, with `CHECKSUMS`; renamed `Gemfile.lock` in testdata); synthetic: `GIT`/`PATH` sections, `PLUGIN SOURCE`, CRLF, a Gemfile with `eval_gemfile`, `if`, `group do`, a `%w[]` loop, and a 64 KiB line.

#### 4.2.3 PHP / Composer (`ecosystems/php`)

**Files and match.** `composer/json`: `composer.json`; `composer/lock`: `composer.lock`. Excluded: `vendor` segment (Composer's `vendor/` contains one `composer.json` per installed package; matching them would create thousands of false units).

**`composer.json` grammar handled** (JSON via `encoding/json` into `map`-free structs with `json.RawMessage` for polymorphic fields; the whole file is size-checked first; depth is bounded by a pre-scan counting `{`/`[` outside strings against `MaxDepth`): `name`, `description` (ignored), `type`, `version`, `license` (string or array), `require`, `require-dev`, `replace`, `provide` (ignored), `conflict` (ignored), `config.platform`, `repositories` (array or object form; counted only: `Properties["repositories"]=<n>`), `minimum-stability`, `extra` (ignored). **Not handled:** `autoload`, `scripts`, `suggest`, `config` other than `platform`, per-repository package mapping, `composer.json` `version` normalisation.

**Mapping.**

| Source | Model |
|---|---|
| `require` entry | `Dependency{Target composer:<vendor>/<name> lower, TargetDisplay original when it differs, Requirement raw constraint, Scope runtime, Group "require"}`; **platform packages** (`php`, `php-64bit`, `php-ipv6`, `php-zts`, `php-debug`, `hhvm`, `ext-*`, `lib-*`, `composer`, `composer-plugin-api`, `composer-runtime-api`, matching Composer's `PLATFORM_PACKAGE_REGEX`, V-2) become `Toolchain[<name>] = <constraint>` and are **not** dependencies |
| `require-dev` entry | `ScopeDev`, `Group "require-dev"` |
| exact constraint (`1.2.3`, `=1.2.3`, `v1.2.3`) | `Version` = the text without `=`, `ResPinned` (Composer treats a bare version as exact, unlike NuGet) |
| `^`, `~`, `*`, ranges, `\|\|`, `dev-*`, `x as y` aliases | `Requirement` raw, `ResDeclared` |
| `name` | `Artifact{composer:<lower name>, Version=<version field or "">, Kind: library (default), `application` for `project`, `plugin` for `composer-plugin`, `metapackage`}` |
| `replace` entry | additional `Artifact{composer:<lower name>, Version "", Kind "replace"}`: a monorepo such as `laravel/framework` legitimately produces the `illuminate/*` names (fixture `laravel/framework`) |

**`composer.lock`**: `packages` (`ScopeRuntime`) and `packages-dev` (`ScopeDev`) arrays of package objects with `name`, `version`, `source`, `dist{shasum}`, `require`, `license`, `replace`, `type`; top-level `platform`, `platform-dev` (-> `Toolchain`), `plugin-api-version`, `content-hash` (-> `Properties["content-hash"]`; not recomputed: the `md5` over a PHP-encoded subset of `composer.json` cannot be reproduced byte-exactly without PHP's `json_encode` rules, so staleness detection is a documented non-goal, V-2). `LockedComponent{ID composer:<lower>, Version (verbatim, a `v` prefix is kept), Integrity "sha1:<shasum>" when non-empty, Licenses (SPDX ids), DependsOn: names in `require` that exist in the lock, falling back to a package whose `replace` map contains the name (Composer satisfies `symfony/polyfill-php73` through replacers), platform requirements ignored}`. `Relationship` stays `RelUnknown` in the parser and the join sets Direct/Transitive from `composer.json`'s `require`/`require-dev` names.

**Identity.** Vendor and name lower-cased (Composer schema pattern is lower-case, V-2); `FormatPURL` default (`pkg:composer/<vendor>/<name>@<version>`, purl `composer` type lower-case rule, V-8).

**Limits.** composer.json 1 MiB; composer.lock 8 MiB (typical 90 KB; large Laravel apps 1-3 MB); package count cap 20,000; depth 256. **Diagnostics**: `php.malformed`, `php.name-case` (an upper-case name was folded), `php.platform-in-lock` (informational: ignored), `php.duplicate`, `php.lock-stale` (a `require` name absent from the lock).

**Assembly**: join, `Manifests = composer.json`, `LockFor = ["composer.lock"]`, same directory only.

**Corpus.** `composer/composer` `composer.json` (3,885 B) and `composer.lock` (MIT, 90,689 B: 30 packages, 6 dev, platform block, plugin-api-version 2.11.0); `laravel/framework` `composer.json` (MIT, 10,768 B; `replace` block); synthetic: dev branches (`dev-main as 1.0.x-dev`), `||` ranges, platform-only file, array `license`, duplicate keys, a lock with `replace`-satisfied edge.

#### 4.2.4 Swift (`ecosystems/swift`)

**Files and match.** `swift/package-swift`: `Package.swift` (also `Package@swift-5.9.swift`: version-specific manifests, matched by `^Package(@swift-[0-9.]+)?\.swift$`, and deduplicated by the join: when both exist the unversioned file wins). `swift/resolved`: `Package.resolved` anywhere, including `*.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved` and `*.xcworkspace/xcshareddata/swiftpm/Package.resolved` (Xcode projects without a `Package.swift`). Excluded: `.build`, `Pods`, `Carthage`, `DerivedData`.

**`Package.swift` grammar handled** (`ConfHeuristic`; a small Swift tokenizer in the package, not shared: it needs nested `/* */` comments, `"""` and raw `#"..."#` strings, `#if/#elseif/#else/#endif` lines, labelled arguments and leading-dot expressions): the first-line comment `// swift-tools-version: X` -> `Toolchain["swift-tools"]`; `Package(name: "X", platforms: [.iOS(.v13), .macOS("10.15")], ...)` -> `Properties["package-name"]`, `Toolchain["platform.ios"]="13"`, `Toolchain["platform.macos"]="10.15"` (`.v10_15` -> `10.15`); every `.package(...)` call **anywhere in the file** (so arrays built into `let` constants are found), forms: `url:` with `from:`, `.upToNextMajor(from:)`, `.upToNextMinor(from:)`, `exact:`/`.exact(...)`, `branch:`/`.branch(...)`, `revision:`/`.revision(...)`, bare `"1.0.0"..<"2.0.0"` and `..."` ranges; `path:`; `id:` (registry) with the same requirement forms; the deprecated `name:` argument is ignored; products used by test targets only (below).

| Form | `Requirement` (canonical text) | Version | Resolution | Source |
|---|---|---|---|---|
| `from: "1.2.3"`, `.upToNextMajor(from: "1.2.3")` | `from 1.2.3` | `""` | declared | git |
| `.upToNextMinor(from: "1.2.3")` | `upToNextMinor 1.2.3` | `""` | declared | git |
| `exact: "1.2.3"` | `exact 1.2.3` | `1.2.3` | pinned | git |
| `branch: "main"` | `branch main` | `""` | declared | git |
| `revision: "abc..."` | `revision abc...` | the revision | pinned | git |
| `"1.0.0"..<"2.0.0"` | `range 1.0.0..<2.0.0` | `""` | declared | git |
| `path: "../x"` | `""` | `""` | declared | `SourcePath`, `_local` namespace; the `Name` and `SourceRef` are the cleaned path relative to the `Package.swift` directory (4.1.9; rebased and escape-checked by the runner/join, `assemble.path-escape`) |

`SourceRef` = `purl.RepoKey(url)`. Identity of a URL package = SwiftPM's rule: the last path component without `.git`, lower-cased, under namespace `host/owner...`; we store `Namespace = <host>/<owner path>` and `Name = <repo>` (`github.com/apple`, `swift-nio`), with the whole key lower-cased (what `purl.RepoKey` returns and what SwiftPM's own identity rule does to the last path component; the graph's channel B compares lower-cased keys) and the original spelling in `TargetDisplay`, from which `FormatPURL` renders the purl. Scope: a package referenced only in `.testTarget(...)` blocks (found by balanced-paren scan for `.testTarget(` and collecting `package: "x"` and `.product(name:package:)` labels in it) and in no other target block is `ScopeTest`; otherwise `ScopeRuntime`. `#if ... #endif` regions set `Condition` to the `#if` expression text. **Not handled** (`swift.dynamic`, once per file): packages built through helper functions, `ProcessInfo.processInfo.environment`, string interpolation or concatenation in URLs, `Context.packageDirectory`, plugin and macro dependency specifics, `traits:` (SwiftPM 6.1).

**`Package.resolved`** (V-3; the file was named `PinsStore` in older SwiftPM sources and is `ResolvedPackagesStore.swift` now): shape-detected: `{"object":{"pins":[...]},"version":1}` (v1: `package`, `repositoryURL`, `state{branch,revision,version}`) or `{"pins":[...],"version":2|3,"originHash":...}` (v2/v3: `identity`, `kind` in `localSourceControl|remoteSourceControl|registry`, `location`, `state`). `LockedComponent{ID from location (`purl.RepoKey`, split into namespace `host/owner` and name `repo`) or, for `registry`, `swift:<scope>/<name>` split at the first `.` of the identity, Version = `state.version` else `state.revision`, Relationship RelUnknown (join decides), Scope runtime}`; a pin with `state.branch` and no version records the revision as `Version` and the branch in a `swift.branch-pin` diagnostic; `localSourceControl` pins have absolute machine paths and are skipped (`swift.local-pin`); `originHash` -> `Properties["originHash"]`. A pin with an unparseable URL is skipped with `swift.bad-url`.

**Identity and purl.** `FormatPURL`: `pkg:swift/<namespace>/<name>@<version>` where the purl `swift` type's namespace is host plus owner and names are case-sensitive (V-8). Because the parser lower-cases github.com identities, the formatter uses `Display` (original case) when present.

**Assembly**: join with `Manifests = Package.swift`, `LockFor = ["Package.resolved"]`, `UnitDir` mapping any path containing `/xcshareddata/swiftpm/` to the directory containing the enclosing `*.xcodeproj` or `*.xcworkspace` segment's parent, `LockOnly` accepting `swift/resolved` (Xcode units). Lock-only units synthesise `Dependency` entries from pins (4.3 rule J-5: `ConfHeuristic`, `Group "Package.resolved"`, diagnostic `swift.direct-unknown`), so iOS apps still show channel-B edges to org-internal packages.

**Limits.** `Package.swift` 1 MiB, token cap by `Guard`, nested comment depth by `Enter`; `Package.resolved` 8 MiB, pins cap 20,000. **Diagnostics**: `swift.malformed`, `swift.dynamic`, `swift.local-pin`, `swift.bad-url`, `swift.branch-pin`, `swift.direct-unknown`, `swift.duplicate`.

**Corpus.** `apple/swift-nio` `Package.swift` (Apache-2.0, 20,545 B: many `.package(url:from:)`, `#if` blocks); `Alamofire/Alamofire` `Package.swift` (MIT, 2,993 B); `sindresorhus/macos-trash` `Package.resolved` (MIT, 384 B, v3, one pin, originHash); live GraphQL on `layoutBox/FlexLayout` shows GitHub itself reads `Package.resolved` as `github.com/facebook/yoga` (V-16). Synthetic: v1 and v2 `Package.resolved` (no public MIT sample was found, OQ-9), registry pin, branch pin, local pin, Xcode path, `Package@swift-5.9.swift`, nested comments, raw strings.

#### 4.2.5 CocoaPods (`ecosystems/cocoapods`)

**Files and match.** `cocoapods/podfile-lock`: `Podfile.lock`; `cocoapods/podfile`: `Podfile`; `cocoapods/podspec`: `*.podspec`, `*.podspec.json` (JSON form, same fields). Excluded: `Pods`, `Carthage`.

**`Podfile.lock`** (YAML through `yamlsafe`, aliases rejected; keys verified against `lib/cocoapods-core/lockfile.rb` and a real lock, V-4): `PODS` (sequence of scalars `"Name (1.2.3)"` or single-key maps `"Name (1.2.3)": ["Dep (~> 1.0)", ...]`), `DEPENDENCIES` (scalars `"Name"`, `"Name (~> 1.0)"`, `"Name (from `url`, tag `1.0`)"`, `"Name (from `./`)"`), `SPEC REPOS` (map source -> names), `EXTERNAL SOURCES` (map name -> `{:git, :tag|:commit|:branch}` | `{:path}` | `{:podspec}`; keys begin with a colon and are plain strings), `CHECKOUT OPTIONS`, `SPEC CHECKSUMS` (name -> sha1 hex), `PODFILE CHECKSUM`, `COCOAPODS`. Entry strings are split by hand (name up to the first ` (`, version up to the last `)`).

| Source | Model |
|---|---|
| `PODS` entry (root or subspec) | one `LockedComponent` per **root pod** (name before `/`); subspec entries add their dependency edges to the same component; `Version` from the first entry |
| `DEPENDENCIES` entry | one `Dependency` per entry: `Target cocoapods:<root>`, `TargetDisplay` = the full spec name when it contains `/` (`Firebase/Auth`), `Requirement` = text in parentheses (or ""), `Version` = locked, `ResLocked`; the locked root component is marked `RelDirect`; the graph de-duplicates by `(unit, ComponentID, Scope)` (RR-6) |
| `EXTERNAL SOURCES` / `CHECKOUT OPTIONS` | `Source SourceGit` + `purl.RepoKey(:git)` (and `:tag`/`:commit`/`:branch` in `Requirement`), or `SourcePath` + the path relative to the Podfile directory (`from `./`` -> `.`; the pod keeps its own name as `Target`, no `_local`, because the lock names it), or `SourceURL` for `:podspec` |
| `SPEC CHECKSUMS` | `Integrity "sha1:<hex>"` on the root component |
| `COCOAPODS` | `Toolchain["cocoapods"]` |
| `SPEC REPOS` source other than `trunk`/CDN | `Dependency.SourceRef` = that spec repo URL (private spec repo) |

**`Podfile` grammar handled** (`ConfHeuristic`, `rubydsl`): `pod 'Name'[, 'req'...][, :git => 'url'|git:, :tag/:branch/:commit, :path => '..', :podspec => '..', :subspecs => [...], :configurations => [...], :modular_headers, :inhibit_warnings]`; `target 'X' do ... end` and `abstract_target` nesting (target name -> `Group`; names ending in `Tests`/`Spec`/`UITests` -> `ScopeTest`; `:configurations => ['Debug']` -> `ScopeDev`, `Condition "configurations:Debug"`); `source 'url'`; `platform :ios, '13.0'` -> `Toolchain["platform.ios"]`; `inherit! :search_paths` ignored; `post_install do ... end` and `pre_install` blocks skipped by `do`/`end` counting; `use_frameworks!`, `plugin`, `install!` ignored. Not handled: `if`/`case` around pods (emitted with `Condition="ruby-conditional"`), loops, interpolation, `Dir.glob` (`cocoapods.dynamic`).

**`*.podspec`**: `Pod::Spec.new do |s| ... end`: `s.name`, `s.version`, `s.license = { :type => 'MIT' }` or `'MIT'`, `s.dependency 'X', '~> 1'` (and inside `s.subspec 'Core' do |ss| ... end` / `s.ios.dependency`), `s.source = { :git => 'url', :tag => s.version.to_s }`, `s.ios.deployment_target`. `Provides: Artifact{cocoapods:<name>, Version, Kind "library"}` (a subspec adds `<name>/<sub>` only as display). Version expressions like `"#{s.version}"` are dynamic.

**Identity and purl.** `cocoapods:<root name>` as written (case-sensitive, V-8); `FormatPURL` renders `pkg:cocoapods/<root>@<version>#<sub>` from `TargetDisplay` (`Firebase/Auth` -> `#Auth`). **Limits** as 4.2.2 (lock 8 MiB, Podfile 1 MiB). **Diagnostics**: `cocoapods.malformed`, `cocoapods.dynamic`, `cocoapods.unknown-entry` (an entry string that does not match `Name (version)`), `cocoapods.duplicate`, `yaml.*`.

**Assembly**: join, `Manifests = Podfile|*.podspec`, `LockFor("Podfile") = ["Podfile.lock"]`, `LockFor("*.podspec") = []`.

**Corpus.** `layoutBox/FlexLayout` `Podfile` (443 B) and `Podfile.lock` (MIT, 615 B: 4 pods, one `:path` external source, 4 checksums); `Alamofire/Alamofire` `Alamofire.podspec` (MIT, 989 B); synthetic: subspecs (`Firebase/Auth`), `:git` with `:tag`, nested targets, `post_install`, a private spec repo, YAML anchor in the lock (rejected, `yaml.alias-unsupported`).

#### 4.2.6 Dart / Flutter (`ecosystems/dart`)

**Files and match.** `pub/pubspec`: `pubspec.yaml`; `pub/lock`: `pubspec.lock`. Excluded: `.dart_tool`, `.pub-cache`, `.symlinks`, `build`.

**`pubspec.yaml`** (verified against `lib/src/lock_file.dart` and dart.dev docs, V-5; `yamlsafe`, aliases rejected): `name`, `version`, `publish_to` (`none` -> `Artifact.Kind "application"`), `environment{sdk,flutter}` -> `Toolchain["dart"]`, `["flutter"]`, `dependencies`, `dev_dependencies`, `dependency_overrides`, `workspace` (list of paths or globs, Dart 3.11 globs) -> `Members`, `resolution: workspace` -> `Properties["resolution"]="workspace"`. Dependency value forms: scalar constraint (`^1.2.3`, `>=1 <2`, `any`), null (= `any`), `{sdk: flutter}` (skipped: SDK packages are not registry dependencies), `{hosted: <url>|{name,url}, version}` (custom registry -> `SourceRegistry` + URL; `pub.dev`/`pub.dartlang.org` are the default and leave `SourceRef ""`), `{git: <url>|{url,ref,path}, version?}` (`SourceGit`, `SourceRef` = `purl.RepoKey(url)`, `Requirement` = ref, `path` -> `Qualifiers "subpath=..."`), `{path: ...}` (`SourcePath` + the path relative to the pubspec directory; the dependency keeps its package name as `Target`, so channel A can match the sibling's `Provides` as well as channel C). `dependencies` -> `ScopeRuntime`, `dev_dependencies` -> `ScopeDev`, `dependency_overrides` -> `ScopeUnknown` with `Group "dependency_overrides"`.

**`pubspec.lock`**: `packages: {name: {dependency, description, source, version}}`, `sdks`. `dependency` values `"direct main"`, `"direct dev"`, `"direct overridden"`, `"transitive"` (V-5) -> `RelDirect` + `ScopeRuntime`/`ScopeDev`/`ScopeUnknown`, `RelTransitive` + `ScopeUnknown` (the lock does not distinguish runtime from dev transitive packages). `source: sdk` entries are skipped; `source: hosted` -> `Integrity "sha256:<description.sha256>"`; `git`/`path` locked entries keep only `Version`. `sdks` -> `Toolchain`. Measured on `dart-lang/pub`'s lock: 58 packages = 32 direct + 26 transitive (E-B6).

**Identity.** `pub:<lowercase name>`; a name with upper-case letters is folded with `dart.name-case`. **Limits.** pubspec 1 MiB; lock 8 MiB; package cap 20,000. **Diagnostics**: `dart.malformed`, `dart.name-case`, `dart.override-only`, `dart.sdk-skipped` (informational count), `yaml.*`.

**Assembly**: join with `Workspaces=true` (a workspace member has no `pubspec.lock`; the single lock sits at the workspace root, V-5): a member (listed in an ancestor manifest's `workspace:` globs, J-4) takes that ancestor's `pubspec.lock`; a non-member pubspec never reads an ancestor's lock; the lock's `Locked` slice is attached to the root unit only; members receive `Version`/`ResLocked` on their dependencies and a `join.shared-lock` diagnostic naming the lock path.

**Corpus.** `dart-lang/pub` `pubspec.yaml` (BSD-3-Clause, 720 B) and `pubspec.lock` (13,579 B, 58 packages); synthetic: workspace root + two members (`resolution: workspace`, glob `packages/*`), hosted custom URL, git with `path` and `ref`, path dep, `any`, null constraint, sdk dep, override-only package.

#### 4.2.7 Elixir / Hex (`ecosystems/elixir`)

**Files and match.** `hex/mix-exs`: `mix.exs`; `hex/mix-lock`: `mix.lock`. Excluded: `deps`, `_build`.

**Never evaluate.** Mix evaluates `mix.lock` with `Code.eval_quoted`; this parser tokenises both files as data with an own tokenizer and a recursive-descent reader for a *literal subset*: maps `%{ "k": v, "k" => v }`, tuples `{a, b}`, lists, keyword lists `[k: v]`, atoms `:a`/`:"a b"`/`a:`, strings (with `\"` escapes; `#{` makes the string non-literal), integers, booleans, `nil`, module attributes `@version` (resolved only when defined earlier in the same file by `@x "literal"`), and the `++` operator between lists. Anything else (function calls, sigils `~w`, binaries `<<>>`, pipes) makes the *enclosing entry* non-data: skipped with `elixir.non-data`, parsing continues with the next entry.

**`mix.lock`** (real shape from `phoenixframework/phoenix`, 40 entries, V-6): `"name": {:hex, :name, "version", "inner_checksum", [:mix], [deps...], "hexpm", "outer_checksum"}` (older files: 7-element tuple without the outer checksum, or older still with fewer fields), `{:git, "url", "rev", [opts]}` (`branch`, `ref`, `tag`, `sparse`, `subdir`, `depth`, `submodules`). Reading is positional-tolerant: `Version` = third element if string; inner = fourth; deps list = first list-of-tuples after the managers; repo = next string; outer = last string. `LockedComponent{ID hex:<tuple atom lower> (the *Hex package name*, which can differ from the map key when a dependency is renamed with `hex:`), Version, Integrity "sha256:<outer>" else "sha256:<inner>", DependsOn from the deps list resolved through the map keys, Scope runtime}`; git entries: a `{:git, url, rev, opts}` entry becomes `LockedComponent{ID hex:<map key lower>, Version = rev, Integrity ""}` (the same `ComponentID` the `mix.exs` dependency uses, whose `Source` is `SourceGit` with the URL from `git:`/`github:`), so the join matches them. Organisation packages: a repo string `hexpm:acme` -> `Namespace "acme"`.

**`mix.exs`** (`ConfHeuristic`): locate the `deps` function (`defp deps do [...] end` or `def deps`) or a literal `deps: [...]` in `project/0`; read each list element that is a tuple `{:name, "req"?, opts?}` or `{:name, opts}`. Options: `only:` (atom or list: contains `:prod` or absent -> `ScopeRuntime`; contains `:dev` -> `ScopeDev`; only `:test` -> `ScopeTest`), `runtime: false` -> `ScopeBuild`, `optional: true` -> `Optional`, `override: true` -> `Group "override"`, `git:`/`github:` (`"owner/repo"` -> `SourceRef "github.com/owner/repo"`), `branch:`/`tag:`/`ref:` -> `Requirement`, `path:`, `in_umbrella: true` (`SourcePath`, `SourceRef "umbrella"`; target is the sibling app, matched by `Provides`), `hex:` (package-name override), `repo:` (`"hexpm"` default; `"hexpm:org"` -> namespace; other names -> `SourceRegistry` + that name), `organization:` (deprecated). `project/0` keys read: `app:` and `package: [name: "x"]` -> `Provides: Artifact{hex:<package name or app>, Version from `version:` (literal or `@version`), Kind "library"}`, `elixir: "~> 1.15"` -> `Toolchain["elixir"]`, `apps_path: "apps"` -> `Members ["apps/*"]` (umbrella). Not handled (`elixir.dynamic`): `if`/`case` around deps, `++ other_deps()`, `Mix.env()`, interpolation, `Code.require_file`.

**Identity.** `hex:<lower name>`; `FormatPURL` default (`pkg:hex/<ns>/<name>@<version>`, namespace optional, V-8). **Limits.** mix.exs 1 MiB (lexical scan), mix.lock 8 MiB; nesting by `Enter` (tuples/lists/maps). **Diagnostics**: `elixir.malformed`, `elixir.non-data`, `elixir.dynamic`, `elixir.unknown-lock-shape`, `elixir.duplicate`.

**Assembly**: join, `Manifests = mix.exs`, `LockFor = ["mix.lock"]`, `Workspaces=true`: an umbrella root's `Members = ["apps/*"]` makes each `apps/<x>/mix.exs` a member sharing the root `mix.lock` (Mix umbrella children store no lock of their own); same shared-lock treatment as Dart.

**Corpus.** `phoenixframework/phoenix` `mix.exs` (MIT, 9,266 B: 24 deps, 4 `optional`, 14 with `only`) and `mix.lock` (12,906 B, 40 entries); synthetic: umbrella root + two apps, `{:git, ...}` lock entry, renamed hex dependency, 7-element older-format tuple, `~w` sigil in a lock entry, `if` around a dep, org repo (`hexpm:acme`).

#### 4.2.8 Docker (`ecosystems/docker`)

**Files and match.** `docker/dockerfile`: base name `Dockerfile` or `Containerfile`; `Dockerfile.<suffix>` / `Containerfile.<suffix>` where `<suffix>` is not one of `md txt yml yaml json sh go py j2 tpl sample bak`; `<name>.Dockerfile`, `<name>.dockerfile`, `<name>.containerfile`. docker-compose files are out of scope.

**Grammar handled** (a line-oriented scanner; `ConfExact` when the image reference was literal, `ConfHeuristic` when an `ARG` default contributed because the default may be overridden at build time):

1. **Parser directives** at the top (`# syntax=docker/dockerfile:1`, `# escape=\``, `# check=...`): `escape` changes the continuation character; `syntax` produces a `Dependency` on the frontend image (`Group "syntax"`, `ScopeBuild`).
2. **Line joining**: a line ending in the escape character (`\` default) continues; comment lines and empty lines inside a continuation are skipped (BuildKit behaviour); a joined instruction is capped at 64 KiB (`limit.string`).
3. **Heredocs** (`RUN <<EOT`, `COPY <<EOF /dst`, `<<-EOF`, `<<"EOF"`, several per line): bodies are skipped to their terminator and never interpreted (they may contain `FROM` text).
4. **`ARG` before the first `FROM`**: global arguments with optional default (`ARG GO_VERSION=1.26`); `ARG NAME` without default is unset. Substitution in `FROM` lines (and `--platform=` values) supports `$VAR`, `${VAR}`, `${VAR:-word}`, `${VAR-word}`, `${VAR:+word}`, `${VAR+word}`; other modifiers (`#`, `%`, `/`, `:?`) leave the reference unresolved with `docker.unsupported-modifier`. The expansion is bounded by `Guard.Expand` (an `ARG` that refers to itself or to a chain is cut at depth 8).
5. **`FROM [--platform=p] image[:tag][@digest] [AS name]`**; `scratch` is skipped; a reference equal (case-insensitively) to an earlier stage name is a stage reference, not an external image; numeric stage indexes likewise.
6. **`COPY --from=<ref>`** and **`RUN --mount=...,from=<ref>`**: `<ref>` that is neither a known stage name nor a number is an external image (`Group` `copy-from` / `mount-from` is appended to the pin kind as `digest;copy-from`).
7. `ONBUILD`, `LABEL`, `ENV`, `ADD <url>` are read for line structure only (an `ADD https://...` download is not a dependency in v1).

**Not handled**: `ARG` redefined inside a stage and used in a later `COPY --from` (not visible to `FROM` anyway), BuildKit variables (`BUILDPLATFORM`, `TARGETARCH`, ...) in image names (left unresolved), `FROM $VAR` where the name part is unresolved, multi-stage `--target` selection, Dockerfile frontends other than the default.

**Mapping.**

| Reference | `Target` | `Version` | `Requirement` | Resolution | `Group` (pin kind) |
|---|---|---|---|---|---|
| `nginx:1.27` | `docker: ns "docker.io/library", name "nginx"` | `1.27` | `1.27` | declared | `tag` |
| `nginx` | same | `""` | `latest` | declared | `latest` |
| `nginx@sha256:ab...` | same | `sha256:ab...` | `sha256:ab...` | **pinned** | `digest` |
| `nginx:1.27@sha256:ab...` | same | `sha256:ab...` | `1.27` | **pinned** | `digest` |
| `ghcr.io/org/app:v1` | `ns "ghcr.io/org", name "app"` | `v1` | `v1` | declared | `tag` |
| `registry.local:5000/team/img:2` | `ns "registry.local:5000/team", name "img"` | `2` | `2` | declared | `tag` |

Registry detection follows Docker's rule: the first path segment is a registry if it contains `.` or `:` or equals `localhost`; otherwise the registry is `docker.io` and a single-segment name gets the `library/` path (`index.docker.io` and `registry-1.docker.io` normalise to `docker.io`). The registry host is lower-cased; repository components must be lower-case by the OCI distribution spec, so an upper-case component is folded with `docker.unusual-case`. An unresolved *name* part skips the entry (`docker.unresolved-image`); an unresolved *tag* keeps the entry with `Requirement` = the raw text, `Version ""`, `Group "tag"` and `docker.unresolved-tag`.

**Scope.** The external image at the root of the `FROM` chain of the **last stage** (follow stage references downward) is `ScopeRuntime` (it is the base image of what ships); every other external image (builder stages, `COPY --from`, mounts, the `syntax` frontend) is `ScopeBuild`. `Provides`: none (an image's identity is its registry path, which a Dockerfile does not state; channel B uses the repository URL).

**Identity and purl.** `FormatPURL` (purl-spec docker type, V-8): `ns "docker.io/library"` is dropped (`pkg:docker/nginx@1.27`), `ns "docker.io/<org>"` becomes the purl namespace (`pkg:docker/moby/moby-bin@29.9`), any other registry host moves to `?repository_url=<host>` and the rest of the path is the namespace (`pkg:docker/org/app@v1?repository_url=ghcr.io`). The digest, when present, is the purl version (percent-encoded `sha256%3A...`).

**Limits.** 1 MiB file; instruction count cap 100,000 via `Guard`; `ARG` table cap 1,000. **Diagnostics**: `docker.unresolved-image`, `docker.unresolved-tag`, `docker.unsupported-modifier`, `docker.unusual-case`, `docker.no-from` (informational: file without any `FROM`), `docker.malformed`.

**Assembly**: the default per-directory assembler (RR-9): all Dockerfiles of one directory form one unit `Dir = <dir>` with `Loc.Path` telling them apart.

**Corpus.** `docker/buildx` `Dockerfile` (Apache-2.0, 6,421 B: 17 `ARG` before `FROM`, `--platform=$BUILDPLATFORM`, stage-alias references `golatest`/`gobase`, `COPY --from=xx`, `RUN --mount=target=...,type=cache <<EOT`, unresolved `${DOCKER_VERSION}`-style chains that do resolve through defaults); `nginxinc/docker-nginx` `mainline/debian/Dockerfile` (BSD-2-Clause, 7,274 B: `FROM debian:trixie-slim`, continuation lines); synthetic: digest-pinned, `scratch`, `FROM $UNSET`, CRLF, `# escape=\``, nested heredoc containing `FROM x`, a 100,000-line file.

#### 4.2.9 GitHub Actions (`ecosystems/actions`)

**Files and match.** `github/workflow`: `.github/workflows/<name>.yml|yaml` (exactly one level below `workflows`). `github/action`: any `action.yml` or `action.yaml` outside `.github/workflows` and excluded segments (composite and Docker actions in repositories that publish actions). Forgejo/Gitea workflow directories are not matched.

**Decoding**: `yamlsafe` with `AliasBudget = Config.AliasNodeBudget` (default 100,000 nodes per file): GitHub workflows may use anchors, so aliases are expanded under a hard node budget instead of rejected (E-B6 showed that walking `Alias` nodes unbounded can explode; the budget is charged per copied node). Merge keys are ignored. `limit.alias` stops expansion.

**`uses` forms handled** (docs verified 2026-10-14, V-7):

| Form | Mapping |
|---|---|
| `owner/repo@ref` | `Target github:<owner>/<repo>` (lower-cased; `TargetDisplay` original), `Source SourceGit`, `SourceRef github.com/<owner>/<repo>`, `Requirement` and `Version` per the pin table |
| `owner/repo/path@ref` | as above plus `Qualifiers "subpath=<path>"` (a composite action in a subdirectory, or a reusable workflow `owner/repo/.github/workflows/x.yml@ref`) |
| `./path` and `./.github/workflows/x.yml` | `Target github:_local/<cleaned repo-root-relative path>` (`.` for `./`), `Source SourcePath`, **`SourceRef` empty** (GitHub resolves these against the workspace root, so the path is already repo-relative and the runner must not rebase it, 4.1.9 rule 3); a path that escapes the repository root is dropped (`actions.path-escapes`) |
| `$/path` (same repository at the running commit; documented form without `@ref`, single source, V-7) | as `./path` with `Group "same-commit"` |
| `docker://image[:tag][@digest]` | a `Dependency` whose `Target.Ecosystem` is `docker` (RR-11), normalised exactly as 4.2.8, `ScopeBuild` |
| `jobs.<id>.container` (string or `{image}`) and `jobs.<id>.services.<id>.image` | `docker` target, `ScopeBuild`, `Group "container"` / `"service"` (`...;digest` etc. as 4.2.8) |
| `jobs.<id>.uses` (reusable workflow call) | same as `owner/repo/path@ref` |
| any `uses` containing `${{` | skipped with `actions.expression` (GitHub requires literal `uses`) |

Pin table: `ref` of 40 or 64 hex characters -> `Version=ref`, `ResPinned`, `Group "sha"`, and `Requirement` = the version from the trailing YAML comment when it matches `^v?\d+(\.\d+){0,2}([-+].*)?$` (the `# v7.0.1` convention maintained by Dependabot/Renovate; fixture `upload-pages-artifact`), else the SHA; `ref` that matches the semver-tag pattern -> `Version=ref`, `Requirement=ref`, `ResDeclared` (a moving tag, RR-12), `Group "semver-tag"`; any other ref (`main`, `master`, `release/v1`) -> `Version=ref`, `ResDeclared`, `Group "ref"`.

`Scope` is `ScopeBuild` for every action (CI tooling). `Condition`: none (step `if:` expressions are not recorded: they are unbounded text).

**Composite and Docker action metadata (`action.yml`)**: `runs.using: composite` -> `runs.steps[].uses` as above; `runs.using: docker` with `runs.image: docker://...` -> docker target, with `Dockerfile` -> nothing (the local Dockerfile is its own unit); `runs.using: node20|node24` -> `Toolchain["node"]`; `name` -> `Properties["name"]`; `inputs`/`outputs`/`branding` ignored.

**Identity and purl.** `FormatPURL`: `pkg:github/<owner>/<repo>@<ref>#<subpath>` (purl `github` type is lower-case, V-8). The GitHub dependency-graph API reports Actions as `pkg:githubactions/...` (live check 2026-10-14, V-16); that string is a GitHub-internal type, never emitted by this unit and never trusted from a provider (4.4.3 rebuilds the ID from `packageName`).

**Limits.** workflow 1 MiB; depth 256; alias budget; at most 5,000 `uses` per file. **Diagnostics**: `actions.malformed`, `actions.expression`, `actions.path-escapes`, `actions.bad-ref` (no `@` on a remote form), `actions.duplicate`, `yaml.*`, `limit.alias`.

**Assembly**: default per-directory (RR-9): `.github/workflows` forms one unit per repository; each action directory is its own unit.

**Corpus.** `actions/checkout` `.github/workflows/test.yml` (MIT, 9,656 B: `actions/setup-node@v6`, `actions/checkout@v7`, ~25 `./`, `./localClone`, `./actions-checkout`, `docker://bitnami/git:latest`) and `action.yml` (5,144 B); `actions/upload-pages-artifact` `action.yml` (MIT, 2,895 B: `actions/upload-artifact@<sha> # v7.0.1`); synthetic: anchors and aliases under budget and a billion-laughs workflow (stops at `limit.alias`), reusable workflow call, `$/` form, `container`/`services`, an expression in `uses`, `on:` as a string key.

#### 4.2.10 Terraform / OpenTofu (`ecosystems/terraform`)

**Files and match.** `terraform/lock`: `.terraform.lock.hcl`. `terraform/tf`: `*.tf`, `*.tofu`; `*.tf.json` and `*.tofu.json` are matched only to emit `terraform.json-unsupported` (so the gap is visible) and then ignored. Excluded: `.terraform`, `.terragrunt-cache` (downloaded modules and providers contain thousands of `.tf` files).

**HCL safety (the central finding of E-B1).** `hclsyntax.ParseConfig` (hcl/v2 v2.25.0, 2026-09-15, V-10) has **no recursion cap** and a Go stack overflow is a *fatal* error that `recover` cannot catch. Measured crash thresholds (Go 1.27.1, default 1 GB max stack): nested `[` or `(` about 70,000 levels (50,000 survives, 100,000 crashes); nested `{`, and template `"${` nesting, 100,000; a unary `!` chain 500,000 (250,000 survives); nested blocks 300,000; a ternary chain 500,000 (200,000 survives). Parse time is also not linear: 10,000 bracket levels already cost 36 ms. Therefore **no byte is given to `ParseConfig` before a flat pre-scan** that uses `hclsyntax.LexConfig` (flat, 17 ms for 200,000 tokens) and measures what the recursive parser would recurse on:

```
precheck(data):                                    // returns the limit name or "" (compiled and run against hcl/v2 v2.25.0, V-F4)
  if len(data) > cfg.MaxTFBytes { return "size" }                         // default 256 KiB
  toks, _ := hclsyntax.LexConfig(data, "x", hcl.InitialPos)               // flat scanner, never recurses
  if len(toks) > lim.MaxNodes { return "nodes" }
  depth, maxDepth, unary, maxUnary, q := 0, 0, 0, 0, 0
  for t in toks:                                                          // g.Node() per token
     switch t.Type:
       hclsyntax.TokenOBrace, TokenOBrack, TokenOParen, TokenTemplateInterp, TokenTemplateControl,
       TokenOQuote, TokenOHeredoc:                  depth++ ; maxDepth = max(maxDepth, depth) ; unary = 0
       hclsyntax.TokenCBrace, TokenCBrack, TokenCParen, TokenTemplateSeqEnd,
       TokenCQuote, TokenCHeredoc:                  depth-- (floor 0) ; unary = 0
       hclsyntax.TokenBang, TokenMinus:             unary++ ; maxUnary = max(maxUnary, unary)
       hclsyntax.TokenQuestion:                     q++ ; unary = 0
       default:                                     unary = 0
  if maxDepth > cfg.MaxHCLDepth { return "hcl-depth" }                    // default 256
  if maxUnary > cfg.MaxUnaryRun { return "hcl-unary" }                    // default 256
  if q > cfg.MaxTernaries      { return "hcl-ternary" }                   // default 4096
  return ""
```

The same `precheck` runs in front of **both** HCL parsers: `terraform/tf` and `terraform/lock` (`.terraform.lock.hcl` is HCL too and is untrusted input; revision 1 only described the `.tf` path). Notes from running it (V-F4): the depth counter treats a string that contains one `${` as depth 2 (`OQuote` + `TemplateInterp`), so the effective limit for pathological `"${"${...` nesting is about 128 levels; real configurations nest fewer than 10. A template directive `%{ if }` opens and closes on the same tag, so a pile of 100,000 nested `%{ if }` does not count as depth, and it is also harmless: `ParseConfig` handled it in 185 ms without recursion (measured). The traversal and operator shapes `a.b.c...`, `a[0][0]...`, `1 + 1 + ...`, `a && a && ...` and `a.*.*...` are iterative in the parser and were parsed at 100,000 repetitions without a crash (20-185 ms); they are deliberately not counted.

A rejection becomes `limit.hcl-depth` (or `hcl-unary`, `hcl-ternary`, `size`, `nodes`) plus an empty result for that file. Real files are far below the limits (the 63 KB `terraform-aws-modules/terraform-aws-vpc` `main.tf`: max depth 7, 134 ternaries, 12,110 tokens; E-B1). Heredoc bodies are one token and add no depth. Only after `precheck` passes is `hclsyntax.ParseConfig` called, and the walk uses the concrete `*hclsyntax.Body`/`Block`/`Attribute` types (not `hcl.Body` schema decoding), so nothing is evaluated: attribute values are read from `*hclsyntax.LiteralValueExpr`, `*hclsyntax.TemplateExpr` made of one literal part, `*hclsyntax.ObjectConsExpr` and `*hclsyntax.TupleConsExpr`; any other expression kind (function call, reference, interpolation) is "dynamic" for that attribute. A fuzz target feeds raw bytes **only through `Parse`** so the guard is part of what is fuzzed, and a dedicated regression test re-creates the five crash shapes at 100,000 levels and asserts `limit.hcl-depth` without process death.

**`.terraform.lock.hcl`**: blocks `provider "<host>/<namespace>/<type>" { version = "x"; constraints = "y"; hashes = [...] }` (V-9). `LockedComponent{ID terraform:<host>/<namespace> / <type> (lower; the two default registries registry.terraform.io and registry.opentofu.org are folded to registry.terraform.io, OQ-10), Version, Integrity = the first `h1:` hash verbatim (RR-13), RelDirect}`. The lock records no modules.

**`*.tf` / `*.tofu`**: `terraform { required_version = "..."; required_providers { name = { source = "[host/]ns/type", version = "..." } | name = "constraint" (older string-only form: source defaults to hashicorp/<name>) } }` and `module "name" { source = "..."; version = "..." }`. Provider `Dependency{Target terraform:<host>/<ns> / <type>, Requirement=version constraint, Version "", ResDeclared, ScopeRuntime, Group "required_providers", Source SourceRegistry (+ SourceRef host when not the default)}`; an unqualified source gets host `registry.terraform.io` for `.tf` and `registry.opentofu.org` (folded as above) for `.tofu`. `terraform.required_version` -> `Toolchain["terraform"]`.

Module source classification (`source` must be a literal or a `const` variable, V-9; anything else is `terraform.dynamic-source`):

| Source | Mapping |
|---|---|
| `./x`, `../x` | `Target terraform:_local/<cleaned path relative to the declaring file's directory>`, `Source SourcePath`, `SourceRef` the same path; the join assembler (J-0) rewrites both to repo-relative and drops an escaping module with `assemble.path-escape` |
| `ns/name/system` | registry module on `registry.terraform.io`: `Target ns "registry.terraform.io/<ns>", name "<name>/<system>"`; `version` -> `Requirement`; a bare `1.2.3` or `= 1.2.3` is exact -> `Version`, `ResPinned` (Terraform treats a bare version as `=`) |
| `host/ns/name/system` (host contains a dot) | the same on a private registry host |
| `github.com/org/repo` and `bitbucket.org/...` shorthand, `git::https://host/path.git//subdir?ref=v1.2.3`, `git@github.com:org/repo.git`, `git::ssh://...` | `Source SourceGit`, `SourceRef purl.RepoKey(...)` (after the parser has stripped `git::`, `//subdir` and `?ref=`, and prefixed `https://` to a scheme-less `host/owner/repo`), `Target terraform:git:<host>/<owner path> / <repo>`, `Qualifiers "subpath=<subdir>"`, `Requirement` = the `ref` query value, 40-hex ref -> `ResPinned` + `Version`, `Group "git"` |
| `hg::`, `s3::`, `gcs::`, `https://...archive` | `SourceURL` (query and userinfo removed), `Target terraform:url:<host> / <last path segment>`, `terraform.opaque-source` informational |

**Identity.** Providers/modules are not purl types in purl-spec v1.1.0 (V-8): `PURLType ""`, `FormatPURL` returns `ok=false`; the UI shows the `ComponentID` string and the registry URL. **Confidence**: `ConfExact` for literal attributes. **Diagnostics**: `terraform.malformed` (HCL diagnostics summarised into one entry per file), `terraform.dynamic-source`, `terraform.dynamic-version`, `terraform.opaque-source`, `terraform.json-unsupported`, `terraform.duplicate`, `limit.hcl-*`.

**Assembly**: join, `Manifests = *.tf|*.tofu`, `LockFor = [".terraform.lock.hcl"]` in the same directory; all `.tf` files of a directory merge into one unit (Terraform's own module = directory rule). Provider `Version`/`ResLocked` come from the lock. Local modules create intra-repo edges through `_local` (RR-10).

**Corpus.** `terraform-aws-modules/terraform-aws-vpc` `versions.tf` (Apache-2.0, 261 B) and `main.tf` (63,083 B, performance and guard-threshold fixture; contains no `module` block); synthetic (MPL-free): module calls for every source form above, `.terraform.lock.hcl` with two providers, string-only `required_providers`, `.tofu` file, and the five HCL crash shapes at 100,000 levels.

#### 4.2.11 Helm (`ecosystems/helm`)

**Files and match.** `helm/chart`: `Chart.yaml`. `helm/requirements`: `requirements.yaml` (chart API v1, still loaded by Helm, V-15). `helm/lock`: `Chart.lock`, `requirements.lock`. Helm's `charts/<name>/Chart.yaml` copies are matched here like any other and removed by the assembler (below).

**`Chart.yaml`** (`yamlsafe`, aliases rejected; fields from `pkg/chart/v2` and `manager.go`, V-15): `apiVersion`, `name`, `version`, `appVersion`, `type` (`application`/`library`), `kubeVersion` -> `Toolchain["kubernetes"]`, `dependencies[]` each `{name, version, repository, condition, tags, enabled, import-values, alias}`. `requirements.yaml` has the same `dependencies` list. Repository forms: `https://host/path`, `oci://host/path`, `file://../relative`, `@name` or `alias:name` (named repo defined in the user's `repositories.yaml`: unresolvable, kept as namespace `alias:<name>` with `helm.repo-alias`), and **absent** (the dependency is the subchart `charts/<name>` of this chart -> `_local/charts/<name>`, relative to the chart directory and rebased by J-0; fixture `kube-prometheus-stack` `crds` entry without a repository).

| Field | Mapping |
|---|---|
| dependency | `Dependency{Target helm:<repo ns> / <name>, TargetDisplay "", Requirement=version, Condition=raw `condition`, Scope runtime, Group "dependencies" or "requirements", Source registry (SourceRef repo URL) or path}` |
| `version` exact semver without operators or wildcards | `Version`, `ResPinned` (Helm resolves a bare version as `=`) |
| `~1.2.0`, `^1`, `>=1`, `0.12.*`, ranges | `Requirement` raw, `ResDeclared` (fixture: `0.12.*`) |
| `file://../x` | `_local/<cleaned path relative to the chart directory>`, `SourcePath` + the same path (J-0 rebases it; escaping the repo root -> `assemble.path-escape`) |
| repository URL | namespace = host + path with scheme removed, host lower-cased, trailing `/` removed (`ghcr.io/prometheus-community/charts`) |
| chart itself | `Provides`: none (a chart's published identity is repository + name and the repository is not stated in the file; org-internal umbrella charts are linked through `file://`, which resolves intra-repo; OQ-11) |

**`Chart.lock` / `requirements.lock`**: `dependencies[]` `{name, repository, version}` (exact versions), `digest` (`sha256:<hex>` over the dependency set -> `Properties["digest"]`), `generated`. `LockedComponent{ID as above, Version, RelDirect}` (Helm locks hold only direct dependencies; transitive charts live inside the packaged subcharts).

**Identity.** Unofficial purl (V-8): `FormatPURL` returns `ok=false`. **Limits.** YAML 1 MiB, depth 256. **Diagnostics**: `helm.malformed`, `helm.repo-alias`, `helm.vendored-subchart` (on the parent unit), `helm.template-syntax` (a Chart.yaml that contains `{{`), `helm.duplicate`, `yaml.*`.

**Assembly**: join, `Manifests = Chart.yaml|requirements.yaml` (merge in one directory), `LockFor = ["Chart.lock","requirements.lock"]`, `Skip`: a unit whose directory is `<parent>/charts/<name>` where `<parent>/Chart.yaml` is in the file set is dropped (a vendored or in-tree subchart is part of the parent unit) with `helm.vendored-subchart` on the parent.

**Corpus.** `prometheus-community/helm-charts` `charts/kube-prometheus-stack/Chart.yaml` (Apache-2.0, 2,612 B: 5 dependencies, `crds` without repository, `oci://` repositories, `0.12.*`); `keptn/lifecycle-toolkit` `chart/Chart.lock` (Apache-2.0, 427 B: three `file://` entries, digest); synthetic: v1 `requirements.yaml`, `@alias` repository, vendored `charts/sub/Chart.yaml`, templated Chart.yaml, `alias`, `import-values`, a YAML alias bomb (rejected).

---

### 4.3 The generic manifest-plus-lock join assembler (RR-3)

`NewJoinAssembler(eco, opt)` (the `Ancestors` option of revision 1 is deleted: an unconditional ancestor search would give a standalone `example/pubspec.yaml` the lock of its parent package; ancestors are consulted only through workspace membership) returns the assembler used by eight ecosystems of this unit (Ruby, PHP, Swift, CocoaPods, Dart, Elixir, Terraform, Helm); NuGet has its own, Docker and Actions use the default per-directory assembler. It is a root function because every ecosystem package already imports the root (no new package, no archtest row). Specification of `JoinOptions` (compiled):

```go
type JoinOptions struct {
	Manifests  func(path string) bool                                  // Parsed files that define units
	LockFor    func(manifestBase string) []string                      // lock base names for a manifest base name
	LockOnly   func(path string) bool                                  // lock files that may define a unit alone (nil: any KindLockfile)
	UnitDir    func(path string) string                                // directory a file belongs to (nil: path.Dir)
	Skip       func(unitDir string, has func(path string) bool) string // non-empty reason drops the unit
	Workspaces bool                                                    // Members (file-relative globs) name member units that share the workspace root's lock
}
```

Algorithm (pure; input `parsed` sorted by path, only this ecosystem's files; `files` read-only; `dir(p)` is `path.Dir(p.File.Path)`):

```
J-0  rebase:  for every Parsed p, base := dir(p):
                 for every Dependency x with Source == SourcePath: x.SourceRef = path.Join(base, x.SourceRef)   (idempotent with the runner's rebasePaths)
                 for every Dependency x with Target.Namespace == "_local": rel := path.Join(base, x.Target.Name); x.Target.Name = rel; x.SourceRef = rel
                 a result that is ".." or starts with "../" drops the dependency and adds diagnostic assemble.path-escape (same code and wording as the runner; one per distinct path)
                 p.Members stay file-relative; they are expanded in J-4 with manifests.ExpandMembers(base, p.Members, knownDirs)
J-1  group:   units := map[unitDir] -> {manifests []Parsed, locks []Parsed}
              for p in parsed: d := UnitDir(p.File.Path)
                  if Manifests(path)            { units[d].manifests += p }
                  elif p.Kind == KindLockfile   { locks += p (candidate) }
J-2  skip:    for d in units: if Skip != nil && (r := Skip(d, hasPath)) != "" { drop; diag on the parent unit }
J-3  merge:   a unit's manifests merge in path order:
                 Dependencies concatenated, de-duplicated on (Target, Scope, Group) keeping the first,
                 Provides union, Toolchain union (first wins), Licenses union, Members union, Diagnostics concatenated
J-4  lock:    candidates := LockFor(base) for every manifest base name, in order. The lock is the first existing candidate in the unit's own directory d.
              Workspaces: when d has no own lock and the unit's manifest path is matched by the expanded Members of a manifest in a proper ancestor directory
              (nearest such ancestor, `ExpandMembers`), the member uses that ancestor's lock (a workspace member has no lock of its own, V-5); a manifest that is not
              a member NEVER reads a lock from an ancestor. A manifest that is a member and also has its own lock uses its own lock.
              With the lock L:
                 for each Dependency x with Target found in L.Locked: x.Version = locked version, x.Resolution = ResLocked
                      (a pinned manifest version that differs from the locked one keeps the manifest value and adds diag join.version-mismatch)
                 unit.Locked = copy of L.Locked (sorted as in L); Relationship := RelDirect when Target in the unit's Dependencies,
                      else RelTransitive when the unit has >= 1 Dependency, else unchanged (RelUnknown stays unknown)
                 shared lock (member of a workspace): L is attached to the workspace ROOT unit only; members get versions
                      and a join.shared-lock diagnostic with the lock path (memory: one copy of a 600-entry lock, not thirty)
J-5  lock-only: a lock file with no manifest in its unit directory defines a unit when LockOnly(path) holds:
                 Dependencies are the lock file's own `Parsed.Dependencies` when it has any (Ruby and CocoaPods `DEPENDENCIES` carry the
                 requirement text and the git/path source), otherwise synthesised from locked entries with RelDirect (or all entries when every
                 Relationship is RelUnknown), Group = the lock file name, Confidence = ConfHeuristic,
                 diag join.direct-unknown when the direct set had to be guessed. In a unit that HAS a manifest, the lock's own Dependencies are
                 ignored (only Locked is used), so a Gemfile and its Gemfile.lock never produce two rows per gem
J-6  output:  Unit{Ecosystem, Dir, Files (every contributing FileRef, path order), Provides, Dependencies, Locked, Members, Toolchain, Licenses,
              Diagnostics}, units in Dir order, a function of (parsed, paths) only. `Unit.ID` ("<dir>#<ecosystem>[#<primary file base name>]"), `Role`,
              `LockedCount`, directory normalisation, caps and the final ordering are applied by the root's `finalize` to the output of every assembler
              (reference scaffold `assemble.go`), so no assembler of this unit sets them (a second manifest in one directory is distinguished by the
              base name that `finalize` takes from `Files[0]`)
```

`FileSet` is used only for `hasPath` (Paths) in `Skip`; the join never reads blobs. Edge cases fixed here: two lock candidates for one unit (Xcode project and workspace both holding `Package.resolved`): the union by `(ID, Version)`, conflicts on the same ID keep the first and add `join.lock-conflict`; a lock file whose ancestors search reaches the repository root without finding a manifest creates no unit unless `LockOnly` accepts it; Members globs that match nothing are ignored silently (the member may be absent from the allow-list).

---

### 4.4 Optional providers (`DependencySource`, P5/P6)

#### 4.4.1 Principles (from ADR-021 and architecture 13.1)

1. Providers are **secondary**: precedence **own parse > SBOM > graph manifests > registry metadata** (`Rank`: own 0, sbom 1, github-graph 2, deps-dev 3). A lower-ranked source may only (a) add `FactTransitive` entries that are absent, and (b) fill *empty* licence/latest/deprecation fields. It never changes a version, scope, requirement, source or relationship of an own entry.
2. **Off by default**, enabled per provider in config (`manifests.providers[]`), each with its own switch; a provider with a missing collaborator fails startup (typed `manifests.ErrProviderConfig`).
3. **Provenance on every fact** (`Provenance{Source, Ref, At, Licence}`); the API's `provenance` field carries `Source`; the UI labels non-own values.
4. **Private coordinates never leave the process**: `FactsRequest.IsPrivate` (org-produced coordinates and configured private prefixes) is applied before any outbound call by the third-party providers (deps.dev) and `Visibility != VisPublic` repositories are skipped by deps.dev unless `include_private_repos` is set. GitHub-backed providers talk to GitHub about the repository's own data and send no coordinate at all.
5. Providers never block readiness and run in their own job kinds with low priority; their failure never marks the repository's own facts stale.

#### 4.4.2 `Merge` and the enrich jobs

`Merge(units, facts)` (root `source.go`), per fact in `(Rank, UnitDir, Kind, Target, Version)` order: find the unit by `UnitDir` (unknown: `Shadowed`); `FactTransitive`: if the unit already has a `LockedComponent` with the same `ID` -> `Shadowed` when the version equals, `Conflicts` + diagnostic `enrich.conflict` when it differs (own wins); else append `LockedComponent{ID, Version, Relationship: RelTransitive, Scope: ScopeUnknown, Source: fact.Prov.Source}` and, when `Parent` is non-zero and present, add an index to its `DependsOn`; `FactLicence`/`FactLatest`/`FactDeprecated`: if the own `LockedComponent.Licenses` (or the component is a declared dependency with a known licence) is non-empty -> `Shadowed`, else append `Enriched{...}`. The function is deterministic and idempotent and is table-tested for every precedence pair.

Jobs (architecture 13.4 lists one `manifests.enrich`; **D-4** splits it because a `job.Spec` has exactly one `Upstream` and a deps.dev outage must not pause the GitHub budget or the reverse):

| Kind | Upstream | Scope | Policy | Providers run |
|---|---|---|---|---|
| `manifests.enrich` | `github` | per repository | TTL `manifests.enrich.ttl` (24 h), idle back-off to `idle_backoff_cap` (7 d), `PrioScheduled`, `Timeout` 3 min | `githubgraph`, `sbom` (sequential per repository) |
| `manifests.enrich.depsdev` | `deps-dev` | per repository | same TTL, `PrioScheduled`, `Timeout` 5 min | `depsdev` |

Status classification (the table that `TestProviderStatusMatrix` asserts; "adapter" means the `github` adapter has already turned the response into a typed error that passes through):

| Observation | `githubgraph` / `sbom` (through the adapter) | `depsdev` (own `net/http` client, redirects disabled) |
|---|---|---|
| 200 | decode; GraphQL `errors[]` with `FORBIDDEN` or "Resource not accessible" -> `PermanentError` (`graph-not-accessible`); other GraphQL errors -> plain error (transient, once per run) | decode the closed subset; a body over the 1 MiB cap or invalid JSON -> coordinate skipped, outcome `schema`, negative cache entry |
| 202 (SBOM fetch) | `pending=true`, poll | not applicable |
| 302 (SBOM fetch) | adapter follows it (no `Authorization`, https only, SSRF guard) and returns the body | any 3xx is an error (`CheckRedirect` returns `ErrUseLastResponse`), outcome `error`, breaker counts it |
| 304 | not used (no conditional requests) | treated like 3xx |
| 400, 422 | `PermanentError` reason `bad-request` | coordinate skipped, outcome `schema` |
| 401 | adapter refreshes the token once, then `*PauseError` | not applicable (no credential) |
| 403 | adapter: rate limit -> `*PauseError`; otherwise `PermanentError` (`graph-not-accessible` / `sbom-not-accessible`) | outcome `forbidden`, breaker counts it, plain error |
| 404 | repository -> `GoneError`; manifest or report missing -> outcome `not_found`, no facts | negative cache entry for `negative_ttl`, outcome `not_found` |
| 410 | SBOM report expired -> start a new generate (once per run), else `GoneError` | negative cache entry |
| 429 | adapter `*PauseError` | `*RateLimitError{At: now + Retry-After, default 60 s}`, outcome `rate_limited` |
| 5xx, timeout, connection error | adapter transient error (backoff by the engine) | plain error, outcome `error` or `timeout`, breaker counts it |
| context cancelled | `ctx.Err()` immediately | `ctx.Err()` immediately |

Run function (both kinds), in this order (every step is covered by a named test in 12.5):

1. **Own view.** Load the stored own parse (`manifests.facts`; its units carry `LockedCount` only) and, per unit, the own locked slice from the cache namespace `manifests.locked` (`repoKey|unit|treeSHA`). Entries whose `Source != SourceOwn` and any `Enriched` data are **removed before anything is passed to a provider**: a provider must see only what the repository's own files say, otherwise its own earlier additions look "already present" on the second run, are not emitted again, and vanish from the next persisted item (a flapping bug that the revision 1 design had; test `TestEnrichRerunStable`). If the facts item is missing or `Truncated`, return `Unchanged`; if a unit's locked slice was evicted from the cache, skip that unit with diagnostic `enrich.locked-missing` and enqueue `manifests.refresh` through the lock-miss path (never merge against a unit whose lock data is absent).
2. **Request.** Build `FactsRequest` (cap `MaxUnitsPerRepo` units in `Dir` order, diagnostic `enrich.units-capped`; `IsPrivate` per RR-14).
3. **Call** each provider of the kind, in order, with the job `ctx`. Classification (the engine only sees these shapes): an adapter error (`*github.Error`, `*github.PauseError`) passes through unchanged (`%w`); a deps.dev 429 or a `Retry-After` becomes `*manifests.RateLimitError` (pauses the `deps-dev` upstream only); `scm.ErrNotFound` on the repository itself becomes `*manifests.GoneError`; a GraphQL `FORBIDDEN` / "Resource not accessible by integration", a 403 on the dependency graph or the SBOM (feature disabled, no Advanced Security, missing permission) becomes `*manifests.PermanentError{Reason: "graph-not-accessible"|"sbom-not-accessible"}`, which parks the key with that reason (the expected state for private repositories without GHAS); 5xx and timeouts stay plain errors (transient, backoff). **Partial results are discarded on any error** except the explicit `capped` outcome (which is not an error): the item below is replaced only by a complete run, so a half-finished run can never shrink a previous good enrichment (revision 1 said both "partial facts are returned with the error" and "replaced only after a complete run"; the second is the rule).
4. **Persist** the provider facts of this job kind as the store item **of this job's kind** (`manifests.enrich` for the GitHub job, `manifests.enrich.depsdev` for the deps.dev job; `ValidKind` accepts both): `{schema:1, treeSHA, providers:[names+config hash], facts, enriched, completedAt}` as JSON. Two jobs of one repository must never write the same item (they run concurrently on different upstreams; revision 1 had one shared `manifests.enrich` item, a lost-update race).
5. **Re-apply** through `Service.Reapply(ctx, repoKey)` (RR-19), which takes the per-repository lock, reloads the own facts and **both** enrichment items, drops items whose `treeSHA` differs from the current own `TreeSHA`, calls `Merge`, publishes the slot and enqueues `manifests.graph` (debounced, 2 s). The enrich job never writes the slot itself; `manifests.refresh` ends in the same `Reapply`, so whichever job finishes last publishes a state derived from stored data only, and an older tree can never overwrite a newer one (test `TestEnrichRefreshRace`, two goroutines under `-race`).

#### 4.4.3 `githubgraph`

**Purpose.** Cross-check and fill: GitHub's dependency graph parses manifests with its own parsers; it contributes transitive entries from lock files and a second opinion when our heuristics (Gemfile, Package.swift, mix.exs) are weak.

**Verified API (2026-10-14, V-13, V-16).** `Repository.dependencyGraphManifests(first, after, withDependencies, dependenciesFirst, dependenciesAfter, ...)`; `DependencyGraphManifest{id, blobPath, filename, parseable, exceedsMaxSize, dependenciesCount, dependencies}`; `DependencyGraphDependency{packageName, packageManager, requirements, hasDependencies, relationship (public preview), packageUrl (public preview)}`. Live introspection of `DependencyGraphEcosystem` returned exactly `RUBYGEMS, NPM, PIP, MAVEN, NUGET, COMPOSER, GO, ACTIONS, RUST, PUB, SWIFT`: **Terraform, Helm, Docker, CocoaPods and Hex are not covered by GitHub's graph**, so for this unit's ecosystems the provider can contribute for NuGet, Ruby, PHP, Swift, Dart and Actions only. A live query on `layoutBox/FlexLayout` (a user token, cost 1 point) returned `Package.resolved` with `packageName "github.com/facebook/yoga"`, `requirements "= 3.2.1"`, `relationship "unknown"`, `packageUrl "pkg:swift/github.com%2Ffacebook%2Fyoga@3.2.1"`, and `.github/workflows/...yml` with `packageName "actions/checkout"`, `requirements "= 2.*.*"`, `packageUrl "pkg:githubactions/actions/checkout@2.%2A.%2A"`. Consequences: (1) `packageUrl` and `relationship` are public-preview and inconsistent with purl-spec (`githubactions` is not a purl type, `%2F` inside a namespace), so **they are never used**; the ID is rebuilt from `(packageManager, packageName)` through `Registry.Spec(eco).Normalize`; (2) `requirements` begins with `= ` for exact versions and is stored as `Version` only when it matches `^= (\S+)$` and has no wildcard.

**Static query** (one constant string; variables only for owner, name, cursors, page sizes; no string building from repository data):

```graphql
query($owner:String!,$name:String!,$mFirst:Int!,$mAfter:String,$dFirst:Int!){
  repository(owner:$owner,name:$name){
    dependencyGraphManifests(first:$mFirst,after:$mAfter,withDependencies:true,dependenciesFirst:$dFirst){
      totalCount pageInfo{hasNextPage endCursor}
      nodes{ id filename blobPath parseable exceedsMaxSize dependenciesCount
        dependencies(first:$dFirst){ totalCount pageInfo{hasNextPage endCursor}
          nodes{ packageName packageManager requirements hasDependencies } } } } }
  rateLimit{cost remaining resetAt}
}
```

Both queries were validated against GitHub's published GraphQL schema (`docs.github.com/public/fpt/schema.docs.graphql`, graphql-js 16.9 `validate`, 0 errors, 2026-10-14). The continuation query for a manifest whose dependency page has `hasNextPage` (`DependencyGraphManifest implements Node`):

```graphql
query($id:ID!,$dFirst:Int!,$dAfter:String){
  node(id:$id){ ... on DependencyGraphManifest { id
    dependencies(first:$dFirst,after:$dAfter){ totalCount pageInfo{hasNextPage endCursor}
      nodes{ packageName packageManager requirements hasDependencies } } } }
  rateLimit{cost remaining resetAt}
}
```

Paging: manifests by `pageInfo` up to `MaxPages` requests; a manifest whose dependency page has `hasNextPage` is continued with the query above until `MaxDependencies`; exceeding either cap stops with outcome `capped` and keeps what was read.

**Mapping.** `packageManager`: `NUGET`->`nuget`, `RUBYGEMS`->`gem`, `COMPOSER`->`composer`, `SWIFT`->`swift`, `PUB`->`pub`, `ACTIONS`->`github`; others are ignored (they are the other unit's ecosystems; the provider is ecosystem-agnostic and uses a table in `Config`-free code, so ecosystems-A can reuse it by adding rows). `packageName` split rules per ecosystem: composer and actions at the first `/`; swift at the last `/` (namespace = host/owner...); others no namespace. A manifest is matched to a unit by `blobPath` (`/owner/repo/blob/<ref>/<path>`): a ref can contain `/` (`feature/x`) and the request's ref may be a SHA while GitHub reports a branch, so the repo-relative path is the **longest suffix of the part after `/blob/` that equals a path in the unit file lists** (never a split at the first `/`); no match counts as `unmatched` (metrics only) and yields no fact; the matched file's unit gives `UnitDir`. Emitted facts: `FactTransitive` for dependencies that are absent from the unit's own `Dependencies` and `Locked` (relationship unknown, so scope `ScopeUnknown`, `Parent` zero), `FactLicence` never (the graph carries none). A manifest with `parseable=false` or `exceedsMaxSize=true` yields a `Provenance`-tagged diagnostic fact count only (metrics), no facts.

**Auth and status.** App installation tokens for `dependencyGraphManifests` are **unverified** (spike S-A, `10-architecture.md` V4/open items): the provider is implemented against the port and ships disabled; `Facts` maps a GraphQL `FORBIDDEN`/"Resource not accessible by integration" (an HTTP 200 body with an `errors` array, which the adapter cannot classify) to `*manifests.PermanentError{Reason: "graph-not-accessible"}` (RR-20). **Budget**: one query costs 1 point (live, `rateLimit.cost = 1`); a typical repository needs 1-3 requests; the adapter's budget gate applies (the provider never calls `net/http` itself).

#### 4.4.4 `sbom`

**Verified API (2026-10-14, V-13).** The synchronous endpoint `GET /repos/{owner}/{repo}/dependency-graph/sbom` is **removed on 2026-11-13** (`x-github.deprecationDate 2026-05-12`, `removalDate 2026-11-13`); the replacement is `GET /repos/{owner}/{repo}/dependency-graph/sbom/generate-report` (**GET, not POST**: the REST description 1.1.4 declares operation `dependency-graph/generate-sbom-report` as a GET that triggers the job; revision 1 wrote POST, a method that does not exist on that path; 201 `{sbom_url}`, the OAS example ends in `.../sbom/fetch-report/<uuid>`) and `GET /repos/{owner}/{repo}/dependency-graph/sbom/fetch-report/{sbom_uuid}` (202 while generating; 302 to a temporary download URL when ready; reports are kept up to one week). The OpenAPI `x-github` block of the two endpoints has **no `enabledForGitHubApps` key** (the neighbouring `create-repository-snapshot` has `true`), so App-token support is unconfirmed: spike S-A decides; until then `sbom` is not wired. The synchronous endpoint is never used.

**Flow** (`Facts`): (1) `reportID, err := Reader.GenerateSBOMReport(ctx, repo)` (the adapter parses the last path segment of `sbom_url`); (2) poll `FetchSBOMReport(ctx, repo, reportID, MaxBytes)`: `pending=true` -> `Deps.Sleep(ctx, max(PollInterval, retryAfter))` with jittered doubling capped at 30 s, until `PollTimeout` (outcome `timeout`, no facts, not an error: the next TTL tick retries); (3) decode the body.

**Adapter obligations (RR-16, security)**: the 302 target is followed **without** the `Authorization` header (a bearer token must not reach a storage host), over `https` only, through the SSRF-guarded transport, optionally restricted to `RedirectHosts`, with `maxBytes` enforced while reading (an over-size body is an error, never truncated JSON). The provider additionally decodes with `encoding/json` into a struct that lists only the fields it needs, after a depth pre-scan (`MaxDepth` 64) and a package cap `MaxPackages`.

**Mapping (SPDX 2.3 JSON).** `sbom.packages[]` each with `externalRefs[]` where `referenceCategory == "PACKAGE-MANAGER"` (or `PACKAGE_MANAGER`) and `referenceType == "purl"`: `referenceLocator` is parsed by an own 40-line purl parser (no `packageurl-go` import: the providers' row allows no external library) into type/namespace/name/version; type -> `EcosystemID` through a fixed table (`nuget`, `gem`, `composer`, `swift`, `pub`, `hex`, `cocoapods`, `docker`, `github`, plus the other unit's `npm`, `pypi`, `maven`, `cargo`, `golang`), ID through `Registry.Spec(eco).Normalize`. `relationships[]` with `DEPENDS_ON`/`DEPENDENCY_OF` give parent edges (the document's root package `SPDXRef-DOCUMENT DESCRIBES` -> `Parent` zero). Emitted: `FactTransitive` for components absent from own data, `FactLicence` from `licenseConcluded`/`licenseDeclared` when neither is `NOASSERTION`/`NONE`. Packages without a purl, with an unknown type, or whose name or version is empty are counted and dropped; every string that reaches a `ProvidedFact` passes `manifests.CleanText(s, 512)` (RR-17: providers may not import `manifests/internal/limits`).

#### 4.4.5 `depsdev`

**Verified API (2026-10-14, V-18).** `https://api.deps.dev/v3/systems/{system}/packages/{name}` (`GetPackage`: versions with `isDefault`, `publishedAt`), `.../versions/{version}` (`GetVersion`: `licenses`, `advisoryKeys`, `links`, `relatedProjects`), `.../requirements` (NuGet, RubyGems and others), `GetDependencies` (npm, Cargo, Maven, PyPI only: not used by this unit). System names are lower-case in the path (`nuget`, `rubygems`); the NuGet package name is lower-cased. Responses are `Access-Control-Allow-Origin: *` with `cache-control: max-age=3600`. **Data licence CC-BY 4.0; caching is expressly permitted.** **Rate limits are undocumented** (OQ-12): the provider is deliberately conservative (2 requests/second, burst 4, concurrency 2), honours `Retry-After`/429 by returning `*manifests.RateLimitError` (the engine pauses the upstream `deps-dev` only), and has a circuit breaker (3 consecutive 5xx/timeouts open it for 5 minutes, outcome `error` thereafter until half-open).

**What it fills**, for the systems `nuget` and `rubygems` among this unit's ecosystems (config `systems` may add others the registry knows, e.g. `npm`, `cargo`, `maven`, `pypi`, `go`, which the ecosystems-A unit's entries then also benefit from): `FactLatest` (`isDefault` version of `GetPackage`), `FactLicence` for the *locked or pinned* version (`GetVersion.licenses`), `FactDeprecated` is **not** emitted in v1: the fields exist (`isDeprecated`, `deprecatedReason` on every version of `GetPackage` and on `GetVersion`, verified live 2026-10-14, V-F6) but NuGet maps *unlisted* to `isDeprecated: true, deprecatedReason: "Unlisted"` (seen on `newtonsoft.json` 9.0.2-beta2), which is not deprecation in the sense users expect; emitting it needs a reason filter and is OQ-17.

**Algorithm** (`Facts`): skip when `Visibility != VisPublic && !IncludePrivateRepos`; **validate every coordinate before it can reach a URL** (names and versions come from untrusted manifests): the name must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` and the version `^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$` (NuGet and RubyGems grammars are subsets of these), otherwise the coordinate is skipped (`reason="invalid_coordinate"`); every path segment is `url.PathEscape`d and the request URL is built from `BaseURL` with `url.JoinPath`, never by string concatenation; collect distinct coordinates from `req.Units` `Dependencies` and `Locked` whose `ID.Ecosystem` maps to a configured system, whose `Source` is `SourceRegistry` with empty `SourceRef` (default public registry only), whose `Namespace` does not match `PrivateGroupPrefixes`, and for which `req.IsPrivate(id)` is false (count skipped ones by reason); cap at `MaxCoordinatesPerRepo` (largest first by number of units using them is not needed: source order, diagnostic `enrich.coordinates-capped`); for each coordinate look in `Cache` (`system/name` for latest, `system/name/version` for licences; fresh -> use; stale or missing -> fetch; 404 -> negative entry `NegativeTTL`); fetch through the rate limiter with `RequestTimeout`; decode a **closed subset** (`versions[].versionKey.version`, `isDefault`, `licenses[]`) with `encoding/json` after `Content-Length`/read cap of 1 MiB; on every successful fetch store the decoded subset (not the raw body) with `CacheTTL`. Cold-start arithmetic: an organisation with 1,000 repositories and about 3,000 distinct public NuGet/RubyGems coordinates needs about 3,000 `GetPackage` + 3,000 `GetVersion` calls = 3,000 s at 2 rps (50 minutes once); later runs fetch only new or expired coordinates (CacheTTL 24 h => about 6,000 calls per day worst case, about 50 minutes of the budget at 2 rps, spread by job jitter). `GetPackage` returns every version (15,026 bytes for the 86 versions of `newtonsoft.json`, 37,524 bytes for the 181 of `microsoft.extensions.logging`: 175-210 bytes per version): a package with more than about 5,000 versions exceeds the 1 MiB read cap, is skipped with outcome `schema` and a negative cache entry, and is counted in `scp_manifests_provider_skipped_total{reason="capped"}`.

**Attribution.** Facts carry `Provenance.Licence "CC-BY-4.0"`; the API's provenance object and the dependency table footer show "Data: deps.dev (CC BY 4.0)" whenever any displayed value has `Source "deps-dev"`.

---

## 5. Data

This unit introduces **no table and no migration**. State it uses:

| Item | Kind / namespace | Owner | Content | Retention |
|---|---|---|---|---|
| Parsed files | cache namespace `manifests.parse` (key `(blobSHA, parserID, parserVersion)`) | core | `Parsed` per file, including this unit's diagnostics | LRU within `store.cache_caps.parse` (128 MiB default); a parser `Version()` bump invalidates only that parser's entries |
| Repository facts | store item `manifests.facts` (content SHA = tree SHA) | core | `RepoFacts` with the units this unit's assemblers produced | replaced on every refresh; removed with the repository (cascade) |
| Locked components | cache namespace `manifests.locked` (key `repoKey\|unit\|treeSHA`) | core | the `Locked` slices (shared locks of workspaces appear once, J-4) | `store.cache_caps.locked` |
| Enrichment | store items `manifests.enrich` (GitHub job) and `manifests.enrich.depsdev` (deps.dev job), repository scope | this unit | `{schema:1, treeSHA, providers:[names+config hash], facts:[ProvidedFact], enriched:[Enriched], completedAt}` as JSON, one item per job kind | replaced per complete run; ignored when `treeSHA` differs from the current facts; removed with the repository |
| deps.dev responses | cache namespace `manifests.depsdev` (RR-15) | this unit via bridge | decoded subset JSON per key with an expiry timestamp | `store.cache_caps.depsdev` (16 MiB default); entries expire by `CacheTTL`/`NegativeTTL` and are evicted LRU |
| SBOM reports | none (transient; body discarded after decoding) | n/a | n/a | n/a |

Compatibility: the `manifests.enrich` item carries `schema`; a reader that sees a higher schema ignores the item (outcome `schema-newer`, facts treated as absent) and the next run rewrites it. `Parser.Version()` bumps follow the rule "any output change", enforced by the golden tests (a golden diff without a version bump fails `parsertest`, 12).

---

## 6. Configuration

Owned keys, all under `manifests:` in the root config (`cmd/scp/config.go` composes feature-owned `Config` types; strict decode, unknown key = error; env mapping `SCP_<PATH>` with upper-case path and list indices, ADR-012). Validation runs in `Config.Validate()`, collects path-accurate errors, and every duration is a Go duration string.

| Key | Type | Default | Validation | Env name | Consumer |
|---|---|---|---|---|---|
| `manifests.acquisition.disabled_ecosystems` | []EcosystemID | `[]` | each a registered `EcosystemID` of this unit (`nuget gem composer swift cocoapods pub hex docker github terraform helm`; note `gem`, `composer`, `pub`, `hex`, `github`, not the directory names) | `SCP_MANIFESTS_ACQUISITION_DISABLED_ECOSYSTEMS` (list indices) | **owned by the core unit** (`manifests.Config.Acquisition`), consumed by `Registry.Disable`; this unit adds no per-ecosystem `enabled` key (revision 1 had one: two switches for one thing, and a documented key without a consumer fails the "documented keys == consumed keys" test) |
| `manifests.ecosystems.nuget.max_import_depth` | int | 8 | 1..32 | `SCP_MANIFESTS_ECOSYSTEMS_NUGET_MAX_IMPORT_DEPTH` | `nugetAssembler` |
| `manifests.ecosystems.nuget.max_imports` | int | 64 | 1..1000 | `SCP_MANIFESTS_ECOSYSTEMS_NUGET_MAX_IMPORTS` | `nugetAssembler` |
| `manifests.ecosystems.actions.alias_node_budget` | int | 100000 | 0..2000000 (0 rejects aliases) | `SCP_MANIFESTS_ECOSYSTEMS_ACTIONS_ALIAS_NODE_BUDGET` | `actions/*` via `yamlsafe.Options.AliasBudget` |
| `manifests.ecosystems.terraform.max_tf_bytes` | size | 256KiB | 1KiB..1MiB | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_TF_BYTES` | `terraform/tf` pre-scan |
| `manifests.ecosystems.terraform.max_hcl_depth` | int | 256 | 8..4096 (the validator refuses more: measured process death near 70,000 for brackets) | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_HCL_DEPTH` | pre-scan |
| `manifests.ecosystems.terraform.max_unary_run` | int | 256 | 8..4096 | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_UNARY_RUN` | pre-scan |
| `manifests.ecosystems.terraform.max_ternaries` | int | 4096 | 16..65536 | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_TERNARIES` | pre-scan |
| `manifests.enrich.ttl` | duration | `24h` | 1h..30d | `SCP_MANIFESTS_ENRICH_TTL` | both enrich job specs |
| `manifests.enrich.idle_backoff_cap` | duration | `168h` | at least `ttl` | `SCP_MANIFESTS_ENRICH_IDLE_BACKOFF_CAP` | both enrich job specs |
| `manifests.enrich.max_units_per_repo` | int | 50 | 1..500 | `SCP_MANIFESTS_ENRICH_MAX_UNITS_PER_REPO` | `FactsRequest` builder |
| `manifests.providers[].kind` | enum | required | `githubgraph`, `sbom`, `depsdev`; each at most once | `SCP_MANIFESTS_PROVIDERS_<i>_KIND` | `cmd/scp` constructs the provider. **One flat `ProviderConfig` struct** with every key below as a pointer field (strict decode and the `caarlos0/env` layer both need static field names; a two-pass polymorphic decode cannot take `SCP_..._<i>_RPS`); `Validate` rejects a key that is set but foreign to the element's `kind` (`manifests.providers[1].rps: not valid for kind sbom`) |
| `manifests.providers[].enabled` | bool | `false` | `true` requires the collaborator (a GitHub source for the two GitHub kinds, an SSRF-guarded client for `depsdev`) | `SCP_MANIFESTS_PROVIDERS_<i>_ENABLED` | constructor |
| `manifests.providers[].acknowledge_spike_sa` | bool | `false` | kinds `githubgraph` and `sbom` only; must be `true` to enable while spike S-A is open | `SCP_MANIFESTS_PROVIDERS_<i>_ACKNOWLEDGE_SPIKE_SA` | `cmd/scp` validation only |
| `manifests.providers[githubgraph].timeout` | duration | `60s` | 5s..10m | `SCP_MANIFESTS_PROVIDERS_<i>_TIMEOUT` | `githubgraph.Config.Timeout` |
| `manifests.providers[githubgraph].manifests_page` | int | 10 | 1..100 | `SCP_MANIFESTS_PROVIDERS_<i>_MANIFESTS_PAGE` | query variable `mFirst` |
| `manifests.providers[githubgraph].dependencies_page` | int | 100 | 1..100 | `SCP_MANIFESTS_PROVIDERS_<i>_DEPENDENCIES_PAGE` | query variable `dFirst` |
| `manifests.providers[githubgraph].max_pages` | int | 20 | 1..200 | `SCP_MANIFESTS_PROVIDERS_<i>_MAX_PAGES` | `githubgraph` |
| `manifests.providers[githubgraph].max_dependencies` | int | 5000 | 1..100000 | `SCP_MANIFESTS_PROVIDERS_<i>_MAX_DEPENDENCIES` | `githubgraph` |
| `manifests.providers[sbom].poll_interval` | duration | `5s` | 1s..1m | `SCP_MANIFESTS_PROVIDERS_<i>_POLL_INTERVAL` | `sbom` |
| `manifests.providers[sbom].poll_timeout` | duration | `2m` | at least `poll_interval`, at most 10m | `SCP_MANIFESTS_PROVIDERS_<i>_POLL_TIMEOUT` | `sbom` |
| `manifests.providers[sbom].max_bytes` | size | 32MiB | 1MiB..128MiB | `SCP_MANIFESTS_PROVIDERS_<i>_MAX_BYTES` | passed to `FetchSBOMReport` |
| `manifests.providers[sbom].max_packages` | int | 50000 | 100..500000 | `SCP_MANIFESTS_PROVIDERS_<i>_MAX_PACKAGES` | decoder cap |
| `manifests.providers[sbom].redirect_hosts` | []string | `[]` | each a bare hostname or `*.suffix` | `SCP_MANIFESTS_PROVIDERS_<i>_REDIRECT_HOSTS` | the `github` adapter (via `cmd/scp`) |
| `manifests.providers[depsdev].base_url` | string | `https://api.deps.dev` | https URL passing the SSRF policy | `SCP_MANIFESTS_PROVIDERS_<i>_BASE_URL` | `depsdev` and the guarded transport's host pin |
| `manifests.providers[depsdev].systems` | []string | `[nuget, rubygems]` | subset of deps.dev's documented systems | `SCP_MANIFESTS_PROVIDERS_<i>_SYSTEMS` | `depsdev` |
| `manifests.providers[depsdev].rps` | float | 2 | 0.1..20 | `SCP_MANIFESTS_PROVIDERS_<i>_RPS` | limiter |
| `manifests.providers[depsdev].burst` | int | 4 | 1..20 | `SCP_MANIFESTS_PROVIDERS_<i>_BURST` | limiter |
| `manifests.providers[depsdev].concurrency` | int | 2 | 1..8 | `SCP_MANIFESTS_PROVIDERS_<i>_CONCURRENCY` | semaphore |
| `manifests.providers[depsdev].cache_ttl` | duration | `24h` | 1h..30d | `SCP_MANIFESTS_PROVIDERS_<i>_CACHE_TTL` | cache writes |
| `manifests.providers[depsdev].negative_ttl` | duration | `1h` | 1m..24h | `SCP_MANIFESTS_PROVIDERS_<i>_NEGATIVE_TTL` | cache writes |
| `manifests.providers[depsdev].request_timeout` | duration | `10s` | 1s..60s | `SCP_MANIFESTS_PROVIDERS_<i>_REQUEST_TIMEOUT` | HTTP client |
| `manifests.providers[depsdev].max_coordinates_per_repo` | int | 300 | 1..5000 | `SCP_MANIFESTS_PROVIDERS_<i>_MAX_COORDINATES_PER_REPO` | `depsdev` |
| `manifests.providers[depsdev].private_group_prefixes` | []string | `[]` | each non-empty | `SCP_MANIFESTS_PROVIDERS_<i>_PRIVATE_GROUP_PREFIXES` | added to `IsPrivate` |
| `manifests.providers[depsdev].include_private_repos` | bool | `false` | none | `SCP_MANIFESTS_PROVIDERS_<i>_INCLUDE_PRIVATE_REPOS` | `depsdev` |
| `store.cache_caps.depsdev` | size | 16MiB | 1MiB..256MiB | `SCP_STORE_CACHE_CAPS_DEPSDEV` | store (RR-15); key owned by the store unit's config, defined here |
| `refresh.upstreams.deps-dev.concurrency`, `.pace`, `.job_timeout` | int, duration, duration | `1`, `0`, `2m` | the refresh unit's ranges | `SCP_REFRESH_UPSTREAMS_DEPS_DEV_CONCURRENCY` (hyphen becomes underscore) | `eng.AddUpstream("deps-dev", ...)` in `cmd/scp`, only when the depsdev provider is enabled (RR-21); the `manifests.enrich.depsdev` spec's own `Timeout` (5 min) overrides `job_timeout` |

Provider list elements are addressed by index in the environment (`<i>` is the zero-based position in `manifests.providers`, ADR-012: "list indices; no dots"); the kind-specific keys are keys of that element (`providers: [{kind: depsdev, enabled: true, rps: 2}]`). Cross-key checks at startup: an enabled `githubgraph` or `sbom` element without a configured GitHub source is an error; `depsdev.base_url` must resolve to a public address under the SSRF policy at validation time (a DNS failure is a warning, not an error, so an air-gapped start does not fail); enabling `depsdev` logs one line stating that non-public repositories are excluded unless `include_private_repos` is set; `manifests.ecosystems.terraform.*` values above the stated maxima are validation errors, not clamps.

---

## 7. HTTP/API surface

n/a. This unit owns no route. The manifests API (`/api/v1/repos/{repo}/manifests`, `/dependencies`, `/dependencies/graph`, `/components`, `/sbom.cdx.json`) is owned by the core unit; it must expose `provenance` (`own`, `sbom`, `github-graph`, `deps-dev`), `confidence` and the diagnostics defined here. The only outbound HTTP of this unit is deps.dev (4.4.5) and the two GitHub endpoint families through the adapter (4.4.3, 4.4.4).

---

## 8. Concurrency and lifecycle

- **Parsers and assemblers own no goroutine**, no mutable state after construction, and are safe for concurrent use (their only fields are precompiled patterns and a `Config` copy). Parsing concurrency, the weighted memory semaphore and the per-file watchdog belong to the root (architecture 13.2); this unit's parsers poll `ctx` through `Guard.Tick` so the watchdog stays a last resort. `hclsyntax.ParseConfig` and `yamlsafe.Decode` cannot poll: the pre-scans bound them in advance (4.2.10, 4.1.4).
- **Providers** are plain structs with injected collaborators; they start no goroutine. `depsdev` holds a token-bucket limiter, a concurrency semaphore (`Concurrency`) and a breaker, all guarded by one mutex; calls inside `Facts` fan out to at most `Concurrency` goroutines that are joined before `Facts` returns (structured concurrency with `errgroup`-style cancellation: the first non-retryable error cancels the siblings; partial facts already fetched are discarded with the error classified as above, 4.4.2 step 3). `sbom` is sequential per repository; its only wait is `Deps.Sleep(ctx, d)`, which returns `ctx.Err()` on cancellation.
- **Start/stop order** (`cmd/scp`, architecture 10): build `Registry`, call each ecosystem's `Register` (fixed order: nuget, ruby, php, swift, cocoapods, dart, elixir, docker, actions, terraform, helm), then construct enabled providers (they take the registry), register the two enrich `job.Spec`s with the engine. Providers have nothing to stop; in-flight `Facts` calls are cancelled by the job's `ctx` at graceful shutdown (`lifecycle.Plan` stage "jobs"), and a cancelled run leaves the previous `manifests.enrich` item intact (items are replaced atomically only after a complete run).
- **Publication is serialised per repository.** `manifests.refresh/<repo>`, `manifests.enrich/<repo>` and `manifests.enrich.depsdev/<repo>` are three planner keys and may run at the same time on different upstreams. None of them publishes the slot directly: each ends in `Service.Reapply(repoKey)` (RR-19), which holds one mutex per repository while it derives the slot from stored data (own facts + both enrichment items whose `treeSHA` matches) and publishes it. Provider calls happen **outside** the lock (they take seconds); the lock covers only load, `Merge` and publish (milliseconds). Items are written before `Reapply` is called, so the last caller always sees every completed item. Test: `TestEnrichRefreshRace` (`-race`).
- **Time** is injected everywhere (`Deps.Now`, `Deps.Sleep`): no provider calls `time.Now` or `time.Sleep` directly (a `forbidigo` rule scoped to these packages), which is what makes `testing/synctest` tests of the poll loop and the limiter deterministic (12.5).

---

## 9. Observability

**Parsers emit nothing themselves** (purity); the harness exports per parser `scp_manifests_parse_total{ecosystem,parser,outcome}`, `scp_manifests_parse_seconds`, `scp_manifests_limit_hits_total{limit}` (this unit adds the label values `hcl-depth`, `hcl-unary`, `hcl-ternary`, `alias`, `import-depth`, `import-count`, `string`, `expand`, `bytes`, `depth`, `nodes`, through the `limit.` diagnostic prefix, RR-4) and `scp_manifests_abandoned_total{parser}`. Parser IDs added by this unit to the `parser` label domain: the 27 IDs in 1.2 (low, bounded cardinality).

**Provider metrics (owned by this unit; instruments created from the injected `Meter` in each provider's `New`, names are constants next to the instruments; they join the generated `docs/reference/metrics.md` test, G11):**

| Name | Type | Unit | Labels | Meaning |
|---|---|---|---|---|
| `scp_manifests_provider_requests_total` | counter | 1 | `provider` (`githubgraph\|sbom\|depsdev`), `outcome` (`ok\|capped\|not_found\|forbidden\|rate_limited\|timeout\|error\|schema`) | outbound operations (a GraphQL page, an SBOM generate/fetch, a deps.dev call) |
| `scp_manifests_provider_request_seconds` | histogram | s | `provider` | latency per operation (buckets 0.05..60 s) |
| `scp_manifests_provider_facts_total` | counter | 1 | `provider`, `kind` (`transitive\|licence\|latest\|deprecated`), `result` (`applied\|shadowed\|conflict`) | facts offered to `Merge` and what happened |
| `scp_manifests_provider_skipped_total` | counter | 1 | `provider`, `reason` (`private_coordinate\|org_produced\|unsupported_ecosystem\|private_repo\|custom_registry\|invalid_coordinate\|capped\|disabled`) | coordinates or units not sent |
| `scp_manifests_provider_cache_total` | counter | 1 | `provider`, `result` (`hit\|miss\|negative\|stale`) | deps.dev cache behaviour |
| `scp_manifests_sbom_polls_total` | counter | 1 | `result` (`pending\|ready\|timeout\|failed`) | SBOM report polling |
| `scp_manifests_sbom_report_bytes` | histogram | By | none | size of decoded reports |
| `scp_manifests_provider_breaker_state` | gauge | none (never `1`: the OTel Prometheus exporter appends `_ratio` to a gauge whose unit is `1`, verified 2026-10-14; CD-MT-03) | `provider` | 0 closed, 1 half-open, 2 open |

All label domains are closed enums; no repository, package or URL ever appears as a label (cardinality bound: at most 3 x 8 + ... a few dozen series). Provider results also surface in `/status` as "manifests diagnostics" counts per provider and the last outcome per repository (read from the `manifests.enrich` item).

**Logs** (`slog`, injected, context-attached fields from `logx`): ecosystem and provider code logs nothing at info; providers log at `debug` `provider`, `repo` (slug), `op`, `status`, `duration_ms`, `cost` (GraphQL points), `report_id` (SBOM) and at `warn` once per breaker transition and once per repository when a provider returns `Permanent` (reason code, never the response body). Package names, URLs and file contents are not logged.

**Traces** (injected `Tracer`): `manifests.provider` span per `Facts` call (attributes `provider`, `repo` slug, `units`, `facts`, `outcome`), child spans `manifests.provider.request` per outbound operation (`op`, `status`; for SBOM `poll_count`), and `manifests.merge`. Parse spans (`manifests.parse`: parser id, bytes) are the harness's.

---

## 10. Security considerations

| Threat | Where | Mechanism (and test) |
|---|---|---|
| Parser DoS by size | all | file caps before decoding (RR-5), `Tick` per token, per-file watchdog last resort; fuzz time bound |
| **Go stack exhaustion in HCL** (fatal, not recoverable) | terraform | lexer-level pre-scan (depth, unary run, ternaries, bytes, tokens) before `ParseConfig`; crash thresholds measured (E-B1); regression test at 100,000 levels for five shapes; config validator refuses `max_hcl_depth > 4096` |
| XML entity expansion, deep XML | nuget | `encoding/xml` with `Strict`, `Entity=nil` (undefined entity is an error, nothing is expanded), depth counter (257 levels abort a 100,000-deep file), `CharsetReader` limited to two charsets |
| MSBuild/Dockerfile/HCL interpolation bombs | nuget, docker | expansion budgets via `Guard.Expand` (1 MiB per value, 8 MiB per file), chain depth 8, cycle detection; `$([MSBuild]::...)` never evaluated |
| YAML alias/depth/document bombs | actions, dart, cocoapods, helm | `yamlsafe`: decode to nodes, alias budget (0 or per-file node counter), depth and node caps, document cap; library's own 10,000 depth error as a backstop |
| Code execution through manifests | ruby, swift, cocoapods, elixir | **never evaluated**: lexical scanners only (`rubydsl`, Swift tokenizer, Elixir literal reader); `mix.lock` is read as data although Mix `eval`s it; a fuzz invariant asserts no `os/exec`, `plugin` or `reflect.Call` in the packages (depguard `forbidigo` rules for the unit) |
| Path traversal and symlink tricks | actions, swift, terraform, helm, dart, nuget imports | every path-like value (`./x`, `file://`, `path:`, module `./`) is `path.Clean`ed and emitted relative to the declaring file (4.1.9); the runner (`rebasePaths`) and J-0 make it repo-relative and reject it when it escapes the repository root (`assemble.path-escape`; Actions: `actions.path-escapes`, judged by the parser because its paths are root-relative); the parsers never read other files (assemblers read only already-fetched blobs through `FileSet`); symlinks are not followed by the acquisition layer (architecture 13.4). Test: `TestPathEscape` per ecosystem plus `TestParserPathIndependence` |
| Cross-path cache poisoning (a parser bakes the file's path into a result that is cached by blob SHA) | all | parser output is path-independent (4.1.9); paths are emitted file-relative and rebased by the runner/J-0; `TestParserPathIndependence` on every golden and fuzz input |
| Enrichment races and flapping (a stale enrich job publishing an older tree, a provider's earlier additions treated as own data) | providers | own-view rule and per-repository `Reapply` lock (4.4.2, 8), separate items per job kind; `TestEnrichRerunStable`, `TestEnrichRefreshRace` |
| Alias cycles in YAML (`a: &a [*a]` is accepted by the library) | actions, dart, cocoapods, helm | `yamlsafe` anchor stack: a cycle is `yaml.alias-cycle`, never followed; depth cap on every copy |
| Poisoned identities (dependency confusion at the org graph) | all | `_local` is reserved: a registry name equal to `_local` is rejected by every `Normalize`; namespaces from untrusted text pass `manifests.CleanText`; private coordinates are matched by the root's channel A only against *produced* artifacts, and ambiguous producers are flagged (13.3) |
| Prompt injection and log injection through diagnostics and names | all | `manifests.CleanText` strips controls and bidi characters, diagnostics hold constant sentences plus at most 64 bytes of quoted input, `dependencies.md` carries `trust: untrusted-data` |
| Private coordinate leakage | depsdev, (sbom, githubgraph) | `FactsRequest.IsPrivate` before any call; non-public repositories skipped by default; custom registries (`SourceRef != ""`) never sent; the GitHub providers send no coordinates; deps.dev client is SSRF-guarded and host-pinned |
| SSRF and credential forwarding | sbom, depsdev | SBOM redirect followed without `Authorization`, https only, SSRF guard, optional host allow-list, size cap; deps.dev base URL validated under the SSRF policy, redirects disabled (`CheckRedirect` returns `ErrUseLastResponse` and the provider treats 3xx as error) |
| Oversize or hostile provider responses | all providers | `io.LimitReader` caps (1 MiB deps.dev, `MaxBytes` SBOM, GraphQL page bounded by page size), decode into closed structs, package/dependency caps, JSON depth pre-scan |
| Third-party data licence | depsdev | CC-BY 4.0 attribution in provenance and UI; no redistribution of raw responses (only a decoded subset is cached) |
| Over-trust of GitHub preview fields | githubgraph | `packageUrl` and `relationship` ignored (observed to violate purl-spec, V-16); IDs rebuilt through `Normalize` |
| Stale enrichment shown as current | providers | enrichment keyed by tree SHA and dropped on mismatch; every enriched value labelled with `At` and source |

---

## 11. Decision and guardrail trace

### 11.1 Decisions realised here

| Decision | Disposition | Implemented in |
|---|---|---|
| N-01 Package manifest intelligence (D-15): "know what each repository is built from" | delivered for this unit's eleven ecosystems with named consumers (dependency panel, org graph, used-by, version spread, licences, API/LLM); a manifest cache without a consumer is not built (GR-04) | 1.1, 4.2, 4.4 |
| D-15 "all major ecosystems, one interface and registry, purl identity, never execute, bounded and fuzzed pure parsers" | the eleven ecosystems of this unit plus the shared contract | 4.1, 4.2, 12 |
| D-15 / ADR-021 "SBOM and dependency graph as optional secondary providers with provenance and strict precedence; private coordinates never sent" | `DependencySource`, `Merge`, three providers | 4.4 |
| Q-14 "who consumes the manifest data" (reversed by owner directive) | every parsed field has a named consumer: `Provides` -> graph channel A; `SourceRef` -> channel B; `Requirement`/`Version`/`Resolution` -> version-spread and outdated views; `Licenses` -> licence coverage; `Toolchain` -> technology panel; `Diagnostics` -> `/status` and the API | 3.1, 7 |
| Architecture 13.2 text and encoding policy (A-26, GR-16) | applied to every parser by the skeleton | 4.1.1 |

### 11.2 Design guardrails and the mechanism enforcing each here

Guardrail ids refer to `../02-design-guardrails.md`.

| Guardrail | Hazard | Mechanism in this unit | Test |
|---|---|---|---|
| GR-34 | XML parsed with permissive defaults (DTDs and external entities resolved) and raw input echoed into logs lets a hostile manifest exhaust memory or leak content | NuGet XML via `encoding/xml` with `Strict`, `Entity=nil` (no entity resolution exists), depth counter, size cap, `CharsetReader` limited to two charsets; diagnostics never contain input text beyond 64 bytes; JSON endpoints (GraphQL, SBOM, deps.dev) are preferred over XML everywhere | `TestNuGetEntityIsError`, `TestNuGetDeepXML`, `FuzzNuGetCSProj` |
| GR-04 | data that is filled but never consumed is decorative infrastructure: it costs fetches and maintenance and protects nothing | no parser output without a consumer (11.1); `DependencySource` is created only together with the first provider; providers and `enrich` jobs are off by default and register nothing when disabled; deps.dev data is fetched only for values the manifests do not contain (latest, licence) | `TestRegisterDisabledRegistersNothing`, `TestEnrichJobsAbsentWhenNoProvider` |
| GR-16 | platform-default charsets and unchecked encodings corrupt text silently | `limits.Text` first (BOM stripped, UTF-16/NUL/invalid UTF-8 rejected with `encoding.unsupported`), XML declared `iso-8859-1`/`windows-1252` through `charmap`, all output through `manifests.CleanText` | `TestEncodingMatrix` (every parser x {BOM, UTF-16LE, NUL, invalid UTF-8, latin-1 XML}) |
| GR-22 | HTTP clients without timeouts, swallowed cancellation or an implicit redirect policy hang workers and follow untrusted locations | deps.dev client: dial/TLS/response-header/overall timeouts from `request_timeout`, `ctx` on every call, `CheckRedirect` returns `ErrUseLastResponse` and a 3xx is an error, cancellation returns `ctx.Err()` immediately (never swallowed); SBOM redirect policy is the adapter's and is specified in 4.4.4 | `TestDepsDevTimeoutAndCancel`, `TestDepsDevRedirectIsError` |
| GR-23 | collapsing 403/429/404/301/5xx into "not found, succeed" hides rate limits and permission problems | per-status classification table in 4.4.2: 404 -> negative cache with TTL (a real state), 429/`Retry-After` -> `*manifests.RateLimitError`, 403 on graph/SBOM -> `Permanent` with a reason code (visible in `/status`), 5xx/timeout -> transient with backoff | `TestProviderStatusMatrix` |
| GR-25 | per-repository third-party calls on a short timer and duplicated requests become fetch storms | enrichment TTL 24 h with idle back-off to 7 d; deps.dev responses cached across repositories (key = coordinate) with TTL; coordinate de-duplication per run; a hard request budget per repository (`max_coordinates_per_repo`, `max_pages`); jobs coalesce per repository key | `TestDepsDevCacheShared`, `TestEnrichBudgetCaps` |
| GR-13 | credentials embedded in URLs end up in logs and stores | SBOM temporary download URLs are credentials: never logged, never stored (`Provenance.Ref` holds the report id only), fetched without `Authorization`; deps.dev needs no credential; logs omit URLs and package names | `TestNoSecretInLogs` (captured `slog` output asserted against a canary URL) |
| GR-22 | a circuit breaker, bulkhead and limiter built per call never accumulate failures, so the breaker can never open | the deps.dev breaker, limiter and concurrency semaphore are fields of the one `depsdev.Provider` behind one mutex and shared by every repository; 3 consecutive failures open it for 5 minutes, half-open probe, gauge `scp_manifests_provider_breaker_state` | `TestDepsDevRateAndBreaker` (the breaker opens on the 3rd failure across two repositories, which a per-call breaker could never do) |
| GR-26 | cached data without an expiry, a `fetchedAt` or a status is shown as current when it is stale | enrichment items carry `completedAt`, TTL 24 h with idle back-off, `Provenance.At` on every fact, deps.dev cache entries expire by `CacheTTL`/`NegativeTTL`, enrichment keyed by tree SHA and dropped on mismatch; the API shows `At` and source | `TestEnrichJobs`, `TestDepsDevCacheShared` |
| GR-24 | permanent failures retried identically, and work dropped without a trace | `PermanentError` (403 graph/SBOM, 400/422) parks the key with a reason; transient errors back off by the engine; a capped run is an outcome, not an error; nothing is dropped without a counter (`scp_manifests_provider_skipped_total`) | `TestProviderStatusMatrix`, `TestProviderErrorClassification` |
| GR-25 | third-party integrations polled for values nobody reads waste the upstream budget | deps.dev is called only for values the manifests do not contain (latest, licence), cached across repositories, budgeted per repository, off by default; no scraping, a closed JSON subset is decoded | `TestDepsDevCacheShared`, `TestEnrichBudgetCaps` |
| GR-23, GR-25 | requests to GitHub that ignore rate state and cost exhaust the shared budget | GitHub-backed providers go through the adapter's budget gate and rate state (`rateLimit.cost` read per GraphQL query), a `PauseError` pauses the `github` upstream; the deps.dev upstream is separate (D-4) | `TestGraphQLPaging`, `TestEnrichJobs` |
| GR-42 | unstructured, uncorrelated logs cannot be traced and tend to leak data | injected `Meter`/`Tracer`/`slog`, spans `manifests.provider`, no URL or package name in logs | `TestNoSecretInLogs`, metrics registration test (P5-5) |
| Architecture G3, G5, G6, G11 | globals, `init`, heavy libraries, global telemetry | no `init`/mutable globals (compiled patterns are constructor locals or logically constant; sentinel errors only); hcl only in `terraform`, YAML only in `yamlsafe`, charmap only in `nuget`; `Meter`/`Tracer`/`Now`/`Sleep` injected via `Deps` | `archtest`, `gochecknoglobals`, `forbidigo` (no `time.Now`, `time.Sleep`, `os/exec`, `net` in these packages) |
| Research 07 E5 (a 700-byte POM expanded to 160 MB) | interpolation bomb | MSBuild/Dockerfile expansion budgets (`Guard.Expand`, 1 MiB per value, 8 MiB per file), chain depth 8 | `TestNuGetExpansionBomb`, `TestDockerArgBomb` |
| Research 07 E-class (BurntSushi quadratic) and this unit's **E-B1** | recursion without cap in a parser library | HCL pre-scan; fuzz through `Parse` only | `TestHCLCrashShapes` |

---

## 12. Test plan

### 12.1 Harness and conventions

All parser tests run through `manifests/parsertest` (core unit, P0): `parsertest.Golden(t, parser, dir)` walks `testdata/<case>/input.*`, parses with default `Limits`, and compares the JSON encoding of `Parsed` (stable key order, `File.BlobSHA` zeroed) with `expected.json` (`go test -update` rewrites; updates are reviewed in the PR). Guards: (1) a golden whose bytes changed without a `Parser.Version()` bump fails (`parsertest` stores `version` in each golden); (2) every diagnostic code in a parser's catalogue (section 4.2.x tables) must appear in at least one golden (`TestCatalogueCovered`, driven by an exported `Codes()` list in the *test* file, not the package); (3) the seven shared invariants plus this unit's eighth run on every golden and every fuzz input: no panic, bounded time and memory (2 s, 256 MiB, enforced by a watchdog in the fuzz target), determinism (two parses equal), valid purls (`FormatPURL` output re-parses and re-serialises identically), in-range locations (`Loc.Line` <= line count), valid UTF-8 without NUL or bidi controls in every string, and "diagnostic instead of silent drop" (an input whose recovered `Parsed` is empty while it is non-empty and non-comment must carry at least one diagnostic), and **eighth, `TestParserPathIndependence`** (4.1.9): the same bytes parsed under two different paths that `Match` accepts give `reflect.DeepEqual` results once `File` is zeroed. Further per-registry tests: `TestRegisterAllParserIDs` (all 27 IDs register against a real `manifests.NewRegistry()`; this is the test that would have caught the 11 rejected IDs of revision 1), `TestSpecNormalizeIdempotent` (every `Spec().Normalize` is idempotent and total), `TestRegisterDisabledRegistersNothing`. Tests never touch the network or the clock.

### 12.2 Per-parser named cases

| Parser | Cases (name -> fixture -> acceptance) |
|---|---|
| `nuget/csproj` | `Newtonsoft` (BOM csproj, 13,664 B) -> 1 `PackageReference`, `Requirement "$(MicrosoftSourceLinkGitHubPackageVersion)"`, `Version ""`, `ScopeDev`, `Properties` has `packageid=Newtonsoft.Json`, 8 conditional groups recorded; `Latin1` -> decoded; `EntityBomb` -> `nuget.malformed`, no entity expansion; `Deep100k` -> `limit.depth` at 257 in < 5 ms; `IncludeList` (`Include="A;B"`) -> two dependencies; `ChooseWhen` -> items carry the `When` condition; `Empty` -> no diagnostics |
| `nuget/props` | `AspNetCoreCPM` (32,176 B) -> 225 `PackageVersion` items, 207 raw `$(...)`, `KindCatalog`; `AspNetCoreBuild` -> conditional `ManagePackageVersionsCentrally` recorded in `cond:`; `GlobalPackageReference`; `VersionOverride` |
| `nuget/config` | `Roblox` (1,079 B, BOM) -> 10 packages, `developmentDependency` -> `ScopeDev` |
| `nuget/lock` | `PowerApps` (10,058 B, v2) -> 3 TFMs; Locked = 19+5+6 entries merged by `(ID,Version)`; types Direct 8 / Transitive 8 / CentralTransitive 11 / Project 3 skipped; `Integrity` = `sha512:` + 128 hex; `V3Alias` (synthetic) -> alias keys read by shape; `UnknownShape` -> `nuget.lock-unreadable` |
| `nuget/nuspec` | synthetic with `license type=expression`, dependency groups |
| `gem/lock` | `Rails` (43,769 B) -> 233 locked, 82 direct, 397 edges, 256 checksums, `ruby`/`bundler` toolchain, platforms; `MetaTags` (CHECKSUMS) -> 112 specs / 111 checksums; `GitPath` -> `SourceGit` with revision; `CRLF`; `LongLine64k` -> `limit.string`; `UnknownSection` -> `ruby.unknown-section` |
| `gem/gemfile` | `RailsGemfile` (4,001 B) -> groups, `git:`, `ruby` toolchain; `Dynamic` (`eval_gemfile`, `if`, `%w[].each`) -> `ruby.dynamic` and conditional gems; `Heredoc` -> body never scanned |
| `gem/gemspec` | `ActiveSupport` (2,109 B) -> `Provides activesupport`, `add_dependency` entries, `required_ruby_version` |
| `composer/json` | `Composer` (3,885 B) -> `php` and `ext-*` in `Toolchain`, 22 real dependencies, none platform; `Laravel` -> `replace` -> `Provides` kind `replace`; `DevAlias`; `ArrayLicense` |
| `composer/lock` | `Composer` lock (90,689 B) -> 30 + 6 locked, scopes by array, `platform`, `plugin-api-version`, `DependsOn` incl. a `replace`-satisfied edge |
| `swift/package-swift` | `SwiftNIO` (20,545 B) -> N `.package(url:from:)` entries (golden count), `#if` `Condition`, tools-version; `Alamofire` (2,993 B); `Forms` (every requirement form, 4.2.4 table); `NestedComments`; `RawStrings`; `TestTargetOnly` -> `ScopeTest` |
| `swift/resolved` | `MacosTrash` (v3, 384 B) -> 1 pin, `originHash`; `V1`, `V2` (synthetic); `Registry`; `BranchPin` -> revision + `swift.branch-pin`; `LocalPin` skipped |
| `cocoapods/podfile-lock` | `FlexLayout` (615 B) -> 4 pods, `FlexLayout` from `./` -> `SourcePath` `.` (file-relative), 4 sha1 checksums; `Subspecs` -> `TargetDisplay "Firebase/Auth"`, one component per root; `AnchorRejected` -> `yaml.alias-unsupported` |
| `cocoapods/podfile` | `FlexLayoutPodfile`; `NestedTargets` (`*Tests` -> test); `GitPod`; `PostInstallSkipped` |
| `cocoapods/podspec` | `Alamofire` (989 B) -> `Provides`, license, deployment targets; `Subspec` dependencies |
| `pub/pubspec` | `Pub` (720 B) -> deps, `environment`; `Forms` (hosted custom, git with path/ref, path, sdk skipped, null, `any`); `WorkspaceRoot`, `WorkspaceMember` |
| `pub/lock` | `Pub` (13,579 B) -> 58 packages, 32 direct / 26 transitive, sha256 integrity, `sdks`; `SdkSourceSkipped` |
| `hex/mix-exs` | `Phoenix` (9,266 B) -> 24 deps, 4 `Optional`, 14 with `only`; `Umbrella` -> `Members apps/*`; `GitHubDep`; `InUmbrella`; `AttributeVersion`; `IfAroundDep` -> `elixir.dynamic` |
| `hex/mix-lock` | `Phoenix` (12,906 B) -> 40 entries, outer checksums, deps edges; `SevenTuple`; `GitEntry`; `SigilEntry` -> `elixir.non-data`, other entries intact; `RenamedHex` (map key differs from package atom) |
| `docker/dockerfile` | `Buildx` (6,421 B) -> every external image resolved through the 17 ARG defaults, stage aliases `golatest`/`gobase` not external, `scratch` skipped, `COPY --from=xx` external with `copy-from`; `NginxDebian` (7,274 B) -> `debian:trixie-slim` `ScopeRuntime`; `Digest`; `UnresolvedArg`; `EscapeBacktick`; `HeredocContainsFrom`; `Lines100k` -> `limit.nodes` or fast pass < 2 s |
| `github/workflow` | `CheckoutTest` (9,656 B) -> `actions/setup-node@v6`, `actions/checkout@v7` x6, `./` locals, `./localClone`, `docker://bitnami/git:latest`; `AnchorsUnderBudget`; `AliasBomb` -> `limit.alias`; `ReusableWorkflow`; `SameCommitDollarSlash`; `ContainerServices`; `ExpressionUses` -> `actions.expression` |
| `github/action` | `CheckoutAction` (5,144 B) -> node toolchain; `UploadPages` (2,895 B) -> composite step `actions/upload-artifact@<sha>` pinned with `Requirement v7.0.1`, `Group sha`; `DockerAction` |
| `terraform/lock` | synthetic two-provider lock -> `h1:` integrity, constraints; `OpenTofuHost` -> folded host; `LockCrashShape` (a `.terraform.lock.hcl` of 100,000 nested brackets) -> `limit.hcl-depth`, no process death: the pre-scan guards this parser too |
| `terraform/tf` | `VPCVersions` (261 B) -> `hashicorp/aws >= 6.28`, `required_version`; `VPCMain` (63,083 B) -> no module, pre-scan numbers (depth 7, ternaries 134, tokens 12,110), parse < 100 ms; `ModuleForms` (every source form, 4.2.10 table); `Dynamic` -> `terraform.dynamic-source`; `TofuFile`; `JSONUnsupported`; **`CrashShapes`**: eleven generators at 100,000 levels (brackets, parens, braces, `"${`, `!`, `-`, ternary chain, nested blocks, nested calls, nested `for`, heredoc template) -> `limit.hcl-*` (measured 8-80 ms each; without the pre-scan brackets at 100,000 died with `fatal error: stack overflow` in the run of 2026-10-14), process survives; **`IterativeShapes`**: `a[0]` x100,000, `.y` x100,000, `+ 1` x100,000, `&& true` x100,000, `%{ if }` x100,000 are accepted by the pre-scan and parse without a crash (they are iterative in the parser); **`DepthBoundary`**: depth 256 accepted and 257 rejected for brackets, braces, parens, blocks, calls and `for` |
| `helm/chart` | `KubePromStack` (2,612 B) -> 5 dependencies, `crds` -> `_local/charts/crds` (file-relative), `0.12.*` declared, `oci://` namespaces; `AliasRepo`; `Templated` -> `helm.template-syntax` |
| `helm/requirements` | `V1Requirements` (synthetic chart API v1 file) -> same mapping as `Chart.yaml` dependencies, `Group "requirements"` |
| `helm/lock` | `Keptn` (427 B) -> three `file://` locked entries as file-relative `_local` paths, `digest` |

### 12.3 Assembler tests

`nugetAssembler` (golden units under `testdata/assemble/nuget/<case>/` with a small in-memory `FileSet`): `CPMChain` (nearest `Directory.Packages.props` plus a parent via `GetPathOfFileAbove`), `CPMPresenceAssumed` (aspnetcore-style conditional flag -> `nuget.cpm-assumed`), `PropertyFromVersionsProps` (the `eng/Versions.props` case resolves 207 of 207 in a synthetic mini-repo), `PropertyCycle`, `ImportDepth9` (`MaxImportDepth` 8 -> `limit.import-depth`), `ImportCycle`, `ImportEscapesRoot`, `LockJoinPreferred` (`packages.<project>.lock.json`), `ProjectReferenceIntraRepo` (`_local` resolves to a sibling unit `Dir`), `TestProjectScope`, `ProvidesRules` (IsPackable false, test, Exe, tool), `MultipleProjectsOneDir`, `ExpansionBudget` (a property chain doubling 40 times stops with `limit.expand`).

`JoinAssembler` (table tests over `JoinOptions` shapes, one test per rule J-1..J-6): `OwnLockWins`, `NonMemberNeverReadsAncestorLock` (a standalone `example/pubspec.yaml` below a package with a lock gets none), `MemberOwnLockWins`, `RebaseLocalTargets` (J-0: `../x` from `a/b/main.tf` becomes `a/x`; `../../../x` is dropped with `assemble.path-escape`), `WorkspaceSharedLockAttachedToRootOnly` (memory assertion: the member's `Locked` is nil, a `join.shared-lock` diagnostic exists), `LockOnlyXcode` (`Package.resolved` under `A.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/` -> unit `Dir` is the parent of `A.xcodeproj`, synthesised dependencies, `swift.direct-unknown`), `SkipVendoredHelmSubchart`, `VersionMismatchKeepsManifestPinned`, `TwoLockConflict`, `DeterministicOrder` (shuffled input gives identical output), `RelationshipAssignment`.

### 12.4 Property tests (`testing/quick` or `rapid`-style generators written in the test files; no new dependency)

- `Merge` idempotence and commutativity over fact order for equal ranks; `Rank` strictness (own never overwritten).
- `Normalize` idempotence and `ComponentID` equality for case variants (NuGet `Newtonsoft.Json` vs `newtonsoft.json`, Composer, Hex, Pub, Docker).
- `purl.RepoKey` equivalence classes (`https://github.com/O/R.git`, `git@github.com:O/R`, `ssh://git@github.com/O/R/` all map to `github.com/o/r`, verified against the scaffold), idempotence in the form `RepoKey("https://"+RepoKey(x)) == RepoKey(x)`, and the negative classes (`github.com/O/R` without scheme is `ok=false`).
- Docker reference round trip: parse -> `FormatPURL` -> parse equals.
- Join assembler: output independent of input order; adding an unrelated file never changes an existing unit.

### 12.5 Provider tests (deterministic)

Collaborators are fakes in `providers/<p>/<p>test`-style test files: a **fake GitHub** that implements `GraphQLDoer` and `SBOMReader` from recorded JSON (`testdata/graphql/*.json`, `testdata/sbom/*.json`, recorded 2026-10-14 or later from a public repository with the user's token and **sanitised**), and an `httptest.Server` for deps.dev serving recorded `GetPackage`/`GetVersion` bodies. Time uses `testing/synctest`: `Deps.Now` is `time.Now()` inside the bubble and `Deps.Sleep` is a `time.Timer` wait, so the 202/302 poll loop, the limiter and breaker run without real sleeping.

| Test | Asserts |
|---|---|
| `TestGraphQLPaging` | 3 manifest pages + one `node(id:)` continuation; stops at `max_pages`/`max_dependencies` with outcome `capped`; request count and variables exact; query text equals the constant |
| `TestGraphQLMapping` | `packageManager` x `packageName` table (`NUGET`, `RUBYGEMS`, `COMPOSER` `symfony/console`, `SWIFT` `github.com/facebook/yoga`, `ACTIONS` `actions/checkout`, `PUB`), `packageUrl` ignored, `= 3.2.1` -> version, `= 2.*.*` -> no version, unparseable manifest -> counted only |
| `TestGraphQLForbidden` | `FORBIDDEN` -> `Permanent` with reason `graph-not-accessible` |
| `TestSBOMFlow` | generate -> 202, 202 (`Retry-After` 7 s respected over `poll_interval`), ready -> decode; elapsed fake time exactly the sum of waits; `poll_timeout` -> outcome `timeout`, no facts, nil error |
| `TestSBOMDecodeLimits` | package cap, depth cap, oversize body error, `NOASSERTION` licences dropped, packages without purl dropped and counted |
| `TestSBOMMapping` | SPDX-2.3 example: purl -> `ComponentID` through `Spec.Normalize`; `DEPENDS_ON` relationships -> `Parent` |
| `TestDepsDevRequestShape` | URL path casing (`nuget` lower-case package name), only `api.deps.dev` host, no cookies, `User-Agent` set, no `Authorization`; hostile names (`../../v3/x`, `a?b=c`, `a#b`, `%2e%2e`, 200-byte names, control characters) send **zero** requests and count `invalid_coordinate` |
| `TestDepsDevPrivacy` | `IsPrivate`, `PrivateGroupPrefixes`, custom registry, org-produced and non-public-repo cases send **zero** requests (server request counter 0); skip reasons counted |
| `TestDepsDevRateAndBreaker` | 2 rps limiter spacing, burst, `Retry-After` pause, breaker opens after 3 failures and half-opens after 5 min |
| `TestDepsDevCacheShared` | two repositories using one coordinate cause one `GetPackage`; negative entry honoured for `negative_ttl`; stale entry refetched |
| `TestMergePrecedence` | 3x3 matrix of sources against own data: conflict, fill-empty, add-transitive, shadowed counts |
| `TestEnrichJobs` | jobs absent when no provider is enabled; split upstreams; tree-SHA change drops stored enrichment; `RateLimited` maps to pause; `Permanent` parks with reason |
| `TestEnrichRerunStable` | run the enrich job twice against the same fake provider on the same tree: the second persisted item equals the first (the second run's `FactsRequest.Units` contains no provider-sourced entry); revision 1 flapped here |
| `TestEnrichRefreshRace` | `manifests.refresh` for tree T2 and `manifests.enrich` started at T1 finish in both orders under `-race`: the published slot is always T2 with T2-matching enrichment only |
| `TestEnrichItemsSeparate` | the GitHub job and the deps.dev job write different item kinds and `Reapply` merges both; concurrent completion loses neither |
| `TestProviderErrorClassification` | `RateLimitError`, `PermanentError`, `GoneError` satisfy exactly one of `job.AsRateLimited`, `job.IsPermanent`, `job.IsGone` each (compile-time assertions plus `errors.As` through `%w` wrapping); a `*github.Error` and a `*github.PauseError` pass through unchanged |
| `TestEnrichLockedMissing` | an evicted `manifests.locked` entry skips that unit with `enrich.locked-missing` and enqueues a refresh; no provider call for it |
| `TestProviderStatusMatrix` | 200/202/302/304/401/403/404/410/422/429/500/timeout -> outcome and job classification table |

### 12.6 Fuzz, oracle, race, performance

- **Fuzz targets** (Go native, one per parser, 27 in total, plus `FuzzDecodeYAML`, `FuzzRubyScan`, `FuzzRepoKey`, `FuzzSBOMDecode`, `FuzzGraphQLDecode`, `FuzzDepsDevDecode`): seeds = goldens + the adversarial corpus of 4.2.x; invariants of 12.1; PR run `-fuzztime=30s` per touched parser, nightly 5-10 minutes each; crashers are committed under `testdata/fuzz/`. `FuzzTerraformTF` exercises the pre-scan *and* the parse.
- **Differential oracle** (separate optional module, as ADR-021): where `osv-scalibr` has an extractor for the format (checked against the `google/osv-scalibr` tree on 2026-10-14: extractors exist for `Gemfile.lock` (`ruby/gemfilelock`), `composer.lock` (`php/composerlock`), `pubspec` (`dart/pubspec`), `mix.lock` (`elixir/mixlock`), `Package.resolved` and `Podfile.lock` (`swift/packageresolved`, `swift/podfilelock`), `packages.lock.json`, `packages.config`, CPM and csproj (`dotnet/*`), GitHub Actions (`misc/githubactions`); there is none for Terraform, Helm, Dockerfile, `Gemfile`, `Podfile`, `Package.swift`, `mix.exs`, `.nuspec`: those formats have golden and fuzz coverage only), compare the set of `(name, version)` between the oracle and `Parsed.Locked`; differences are triaged into "ours wrong" or "documented" (`testdata/oracle-known-diffs.md`). The oracle never ships.
- **Race**: `go test -race` over `TestParserConcurrency` (each parser parses the same golden from 32 goroutines and compares outputs) and over provider `Facts` called concurrently for different repositories (shared limiter, cache and breaker).
- **Performance** (benchmarks, regression budgets are 10x the measured value, not gates except the survival tests): `Rails Gemfile.lock` ruby/lock < 20 ms; `AspNetCoreCPM` < 5 ms (0.3 ms measured); `VPCMain` pre-scan + parse < 150 ms; `PowerApps` lock < 5 ms; `Buildx` Dockerfile < 5 ms; allocations per parse reported with `-benchmem`.
- **Fixture hygiene test**: `TestFixtureLicences` checks every fixture has a `SOURCES.tsv` row with an allow-listed licence (MIT, Apache-2.0, BSD-2/3-Clause, `synthetic`) and a matching sha256.


### 12.7 Regression tests named in the trace table (11.2), now defined

Revision 1 named these tests in 11.2 without defining them in 12; an undefined test cannot protect a defect. Each is a Go test in the package stated, with the acceptance condition:

| Test | Package | Acceptance |
|---|---|---|
| `TestNuGetEntityIsError` | `ecosystems/nuget` | a csproj with `<!DOCTYPE x [<!ENTITY a "AAAA">]>` and `&a;` in a version yields `nuget.malformed`, no `Dependency` carries the entity text, memory stays under 8 MiB (GR-34) |
| `TestNuGetDeepXML` | `ecosystems/nuget` | 100,000 nested elements: `limit.depth` in < 5 ms (reported at depth 257) |
| `FuzzNuGetCSProj` | `ecosystems/nuget` | seeds = goldens + `EntityBomb`, `Deep100k`, `Latin1`; the eight invariants of 12.1 |
| `TestEncodingMatrix` | `manifests/parsertest` users, all 27 parsers | each parser x {UTF-8 BOM, UTF-16LE BOM, NUL byte, invalid UTF-8, latin-1 XML declaration (NuGet only)}: BOM-only input parses identically to the BOM-less input, the other three give `encoding.unsupported` and an empty result (GR-16) |
| `TestNuGetExpansionBomb` | `ecosystems/nuget` | a 40-step doubling property chain stops with `limit.expand`, peak allocation < 16 MiB (research 07 E5) |
| `TestDockerArgBomb` | `ecosystems/docker` | `ARG A=$B$B` chains of depth 40 stop at depth 8 / `limit.expand`; `ARG X=${X}` self reference terminates |
| `TestHCLCrashShapes` | `ecosystems/terraform` | the `CrashShapes`, `IterativeShapes`, `DepthBoundary` and `LockCrashShape` cases of 12.2, each in a **subprocess** (`os/exec` of the test binary with `-test.run`) so a regression that reintroduces a stack overflow fails the test instead of killing the test run; the subprocess must exit 0 |
| `TestYAMLAliasCycle` | `internal/yamlsafe` | `a: &a [*a]` and `a: &a {k: *a}` with `AliasBudget` 100,000: `yaml.alias-cycle`, bounded output; with `AliasBudget` 0: `yaml.alias-unsupported` |
| `TestPathEscape` | each ecosystem with path values | `../../../x` from a file two directories deep is dropped with `assemble.path-escape` (Actions: `actions.path-escapes`); `../x` from the same file resolves to a repo-relative path |
| `TestDepsDevTimeoutAndCancel` | `providers/depsdev` | a server that never answers: the call returns at `request_timeout`; cancelling the context returns `ctx.Err()` within 50 ms; no goroutine leak (`goleak`) (GR-22) |
| `TestDepsDevRedirectIsError` | `providers/depsdev` | a 302 from the fake is an error and the redirect target receives zero requests |
| `TestEnrichBudgetCaps` | `manifests` | `max_coordinates_per_repo` and `max_pages` stop the run with the `capped` outcome and the documented request counts |
| `TestEnrichJobsAbsentWhenNoProvider` | `manifests` | `EnrichSpecs` with empty provider lists returns no spec, so `Register` is never called with the unknown upstream `deps-dev` |
| `TestNoSecretInLogs` | `providers/sbom`, `providers/depsdev` | with a canary token in the fake redirect URL and package names in the fixtures, the captured `slog` output contains neither (GR-13) |
| `TestSBOMMethods` | `providers/sbom` | the fake adapter records `GenerateSBOMReport` then `FetchSBOMReport(reportID)`; the report id is the last path segment of `sbom_url`; no call to the removed synchronous endpoint exists in the package (compile-level: the port has no such method) |
---

## 13. Implementation tasks

Dependencies on other units: **CORE** (manifests root: model, `Registry`, harness, `parsertest`, graph, API; `limits`, `purl`, `version` internals), **ECO-A** (the other ecosystems unit), **GH** (`github` adapter), **STORE**, **REFRESH/JOB** (engine, `job.Spec`), **CMD** (`cmd/scp` wiring), **CFG** (config composition). Research phases are in brackets.

### B0 - shared contracts (needs CORE P0: model, registry, `parsertest`, `limits`)

| # | Task | Files | Definition of done |
|---|---|---|---|
| B0-1 | Land RR-1, RR-2, RR-4..RR-6, RR-8..RR-13, RR-17..RR-19, RR-23 in the root (CORE) or confirm they exist (RR-8, RR-17, RR-18 exist in the scaffold); `Kinded` on the nine lock parsers; the 2.5 reconciliation table | `manifests/*.go` | the compiled surface of 18 builds against the real root; `TestRegisterAllParserIDs`; unit tests for each RR |
| B0-2 | `manifests/join.go`: `JoinOptions`, `NewJoinAssembler` with rules J-0..J-6 (J-0 rebases `_local` targets and `SourceRef`) | `join.go`, `join_test.go` | the join cases of 12.3 green; deterministic-order property test green |
| B0-3 | `manifests/internal/yamlsafe` (D-1) incl. fuzz target, alias budget, doc limit, `Problem` codes | `internal/yamlsafe/*` | `FuzzDecodeYAML` 5 min clean; E-B6 shapes (100k-deep sequence, alias bomb) pass in < 50 ms; archtest row merged |
| B0-4 | `manifests/internal/rubydsl` (D-1) with `FuzzRubyScan` | `internal/rubydsl/*` | scans the Rails `Gemfile` and Alamofire podspec to the documented token streams; 5 min fuzz clean |
| B0-5 | `FormatPURL` formatters (RR-1) on top of `purl.Build`; confirm `purl.RepoKey` classes with property tests (core owns the function) | `internal/purl/*` | 12.4 `RepoKey` classes green |
| B0-6 | Test-data conventions: `testdata/SOURCES.tsv`, `THIRD_PARTY.md` entries, `TestFixtureLicences` | per package | hygiene test green on an empty corpus |

### B1 - NuGet [research P2] (needs B0)

| # | Task | DoD |
|---|---|---|
| B1-1 | XML token loop with `Guard`, charset reader, depth/entity behaviour (shared by the four XML parsers) | `Latin1`, `EntityBomb`, `Deep100k` green |
| B1-2 | `nuget/csproj`, `nuget/props`, `nuget/config`, `nuget/nuspec` parsers | goldens of 12.2, fuzz targets |
| B1-3 | `nuget/lock` (shape detection, TFM merge, integrity conversion) | `PowerApps`, `V3Alias`, `UnknownShape` |
| B1-4 | `nugetAssembler` (property environment, CPM, imports, lock join, `ProjectReference`, provides rules) | all 12.3 NuGet cases; oracle diff documented |
| B1-5 | `Spec`, `FormatPURL`, `Register`, `Config` validation | `Register` conformance test (`parsertest.RegistryContract`) |

### B2 - Docker and Actions [research P2] (needs B0-3, B0-5)

| # | Task | DoD |
|---|---|---|
| B2-1 | `docker/dockerfile`: joiner, heredoc skipper, ARG substitution, stage graph, reference normaliser, scope rule | `Buildx`, `NginxDebian`, `Digest`, bomb cases |
| B2-2 | `github/workflow` and `github/action` on `yamlsafe` with alias budget; `uses` classifier; pin table; docker targets | `CheckoutTest`, `UploadPages`, alias cases |
| B2-3 | `FormatPURL` for docker and github (subpath fragment) | purl round trip property |

### B3 - Ruby and PHP [research P5] (needs B0)

| # | Task | DoD |
|---|---|---|
| B3-1 | `gem/lock` line scanner | `Rails` numbers exact (233/82/397/256) |
| B3-2 | `gem/gemfile`, `gem/gemspec` on `rubydsl` | goldens, `ruby.dynamic` cases |
| B3-3 | `composer/json`, `composer/lock` (platform split, replace, `DependsOn`) | `Composer`, `Laravel` goldens |
| B3-4 | Register both with join options | 12.3 join cases per ecosystem |

### B4 - Swift, CocoaPods, Dart, Elixir [research P5] (needs B0)

| # | Task | DoD |
|---|---|---|
| B4-1 | Swift tokenizer + `.package` extractor + requirement forms; `swift/resolved` v1-v3 | `SwiftNIO`, `Forms`, v1/v2/v3 goldens |
| B4-2 | CocoaPods lock (hand-split entries), Podfile, podspec | `FlexLayout`, `Subspecs` |
| B4-3 | Dart pubspec + lock; workspace members | `Pub` numbers exact; workspace shared-lock test |
| B4-4 | Elixir literal reader (never evaluates), `mix.exs` heuristic, `mix.lock` | `Phoenix` numbers exact (24/4/14, 40); umbrella test |
| B4-5 | Xcode `UnitDir` and lock-only synthesis for Swift | `LockOnlyXcode` |

### P5 - providers and `DependencySource` [research P5] (needs B0, CORE P4 graph/jobs, GH, STORE, RR-14..RR-17)

| # | Task | DoD |
|---|---|---|
| P5-1 | `manifests/source.go` (`Provenance`, `ProvidedFact`, `FactsRequest`, `Enriched`, `Merge`, `Rank`, `ErrProviderConfig`, the three error types of RR-20), `RepoFacts.Enriched`, `LockedComponent.Source`; `enrich.go` (`EnrichConfig`, `EnrichDeps`, `EnrichSpecs`), `Service.Reapply` (RR-19), the two job specs and the run function of 4.4.2 | `TestMergePrecedence`, `TestEnrichJobs`, `TestEnrichRerunStable`, `TestEnrichRefreshRace`, `TestEnrichItemsSeparate`, `TestProviderErrorClassification` |
| P5-2 | `depsdev` provider + cache bridge (RR-15) | all deps.dev tests of 12.5 |
| P5-3 | `githubgraph` provider against the fake (both queries of 4.4.3); wiring behind `acknowledge_spike_sa`; blocked on OQ-18 for the adapter method | graph tests; **live smoke only after spike S-A** |
| P5-4 | `/status` rows and API provenance fields (CORE) | contract tests |
| P5-5 | metrics registration test (G11) | generated `metrics.md` lists the 8 provider metrics |

### B5 - Terraform and Helm [research P6] (needs B0, B0-3)

| # | Task | DoD |
|---|---|---|
| B5-1 | HCL pre-scan + `terraform/lock` + `terraform/tf` (providers, modules, source classifier) | `CrashShapes`, `VPCMain` timing, `ModuleForms` |
| B5-2 | `helm/chart`, `helm/requirements`, `helm/lock`, repository normaliser | `KubePromStack`, `Keptn` |
| B5-3 | join options incl. `Skip` for vendored subcharts; terraform lock join | assembler cases |

### P6 - SBOM provider [research P6] (needs P5, spike S-A positive, GH `SBOMReader`)

| # | Task | DoD |
|---|---|---|
| P6-1 | `sbom` provider: poll loop, decode limits, SPDX mapping, own purl parser | all `TestSBOM*` |
| P6-2 | adapter obligations verified in the `github` unit (redirect without `Authorization`, https only, size cap) | adapter tests against `githubtest` |
| P6-3 | live acceptance on one public and one private repository with an App token | recorded in `30-verification`; if negative, the provider is deleted rather than kept disabled (GR-04) |

Sequence: B0 -> B1 -> B2 (v1-critical per research P2) -> B3, B4 -> P5 -> B5 -> P6. Tasks inside a milestone are independent except where noted, so two engineers can split B3 and B4.

---

## 14. Deviations proposed

| ID | Deviation from architecture / ADR | Evidence | Fallback if rejected |
|---|---|---|---|
| D-1 | **Two new rank-1 internal packages** `manifests/internal/yamlsafe` and `manifests/internal/rubydsl` (each needs an `archtest` row; neither imports another internal package, so rank strictly decreases for every edge). `yamlsafe` also keeps the YAML library out of the ecosystem rows (the architecture text at 13.2 lists yaml for `helm/docker/actions`, the matrix row for `ecosystems/*` does not) | four ecosystems decode YAML and need identical alias/depth/doc bounds (E-B6: node decoding sidesteps the library's alias check, so every caller must bound it itself); two ecosystems need the same Ruby scanner (Gemfile, Podfile) | duplicate the 150-line decoder in each of `actions`, `dart`, `cocoapods`, `helm` and the scanner in `ruby` and `cocoapods`; four more fuzz targets |
| D-2 | `DependencySource.Facts(ctx, u Unit)` becomes `Facts(ctx, req FactsRequest)` | `Unit` carries no repository identity, ref or visibility; GitHub-backed providers cannot call GraphQL or the SBOM API without `scm.RepoRef`, and deps.dev must know the visibility and the private predicate. The interface does not exist before P5 (ADR-021 amendment), so nothing breaks | pass `(ctx, repo scm.RepoRef, u Unit)` per unit: N calls per repository instead of one batched call (the SBOM and the graph are per repository, not per unit) |
| D-3 | `EcosystemSpec` gains `FormatPURL` (RR-1) | NuGet is case-preserving while its `ComponentID` is lower-cased; Swift, CocoaPods subspecs, Docker `repository_url` and Actions subpaths cannot be rendered from `(namespace, name)` alone; purl-spec v1.1.0 has no terraform/helm type | default formatter plus per-ecosystem post-processing in the root (spreads ecosystem knowledge into the root) |
| D-4 | Job kind `manifests.enrich` split into `manifests.enrich` (upstream `github`) and `manifests.enrich.depsdev` (upstream `deps-dev`) | a `job.Spec` has one `Upstream`; a rate-limit pause is per upstream (architecture 15 table), so one kind would couple deps.dev outages to GitHub budget and vice versa | one kind with `Upstream "github"` and deps.dev errors mapped to transient (couples the pauses) |
| D-5 | Provider row externals: **"otel API"** added (`manifests/providers/*` row says "none"); no other external module | G11 requires `Meter`/`Tracer` injected by the constructor; the API modules are allowed in emitting packages (A-24) and the manifests root row already lists them | providers emit no metrics (loses the 8 provider instruments) |
| D-6 | Parser ID `docker/from` (research 2.3) is named `docker/dockerfile`; the allow-list additions beyond research 2.3: `*.tf`, `*.tofu`, `*.podspec(.json)`, `requirements.yaml`, `requirements.lock`, `Package@swift-*.swift`, NuGet `*.props` by name pattern, `Containerfile*`; per-ecosystem excluded segments (4.1.2) on top of the global list | module sources, `required_providers`, pod specs and chart v1 requirements carry the dependency data (and channel-B URLs) that the T2 list of research 2.3 omits; the parser reads more than `FROM` (ARG, `COPY --from`, mounts) | stay with the research list (Terraform edges to modules and providers would be missing; only lock-file providers visible) |
| D-7 | `_local` namespace, `Resolution`/`Version` semantics for NuGet and Actions (RR-10, RR-12), a parser emitting another ecosystem's `Target` (RR-11), and `LockedComponent.Source` (RR-7) | needed for intra-repo edges, version-spread of moving tags and provenance; all additive | encode locals as `SourcePath` only and filter by source kind in the graph |
| D-8 | Parser IDs are `<EcosystemID>/<format>`, so five of the eleven package directories register IDs under another name (`ecosystems/ruby` -> `gem/*`, `php` -> `composer/*`, `dart` -> `pub/*`, `elixir` -> `hex/*`, `actions` -> `github/*`); research 07 2.3 named `actions/workflow` | the reference `Registry.Register` rejects any other prefix: run on 2026-10-14, 11 of the 27 revision-1 IDs failed with `must be "<ecosystem>/<format>"` (`TestRegisterAllParserIDs` passes with the renamed set) | relax the rule in the core registry to "prefix = package directory" (needs an extra field on `Parser`), keep revision-1 names |
| D-9 | Enrichment is published through one per-repository `Service.Reapply` (RR-19) and each enrich job owns its own store item; `manifests.enrich` does not publish the slot itself (13.4 says only "optional providers") | two planner keys of one repository run concurrently on different upstreams; revision 1 had one shared item (lost update) and a direct slot write (an older tree could overwrite a newer one); found by reasoning over `04-refresh-webhook.md` (keys per kind, `Upstream` per spec) and the scaffold's `publish` | serialise all manifests jobs of a repository in the engine (couples deps.dev and GitHub pacing again, undoing D-4) |
| D-10 | New engine upstream `deps-dev` (`refresh.upstreams.deps-dev`, `AddUpstream`) added only when the depsdev provider is enabled | `Engine.Register` panics for an upstream that was not added (`04-refresh-webhook.md`); architecture 16 lists only `github` | none needed; follows from D-4 |

None of these changes a decision of `04-decisions.md`; D-1, D-2, D-4, D-5, D-8, D-9 and D-10 change text of `10-architecture.md` (package map/matrix, 13.2, 13.4, 5.2) and should be folded into the next architecture revision if accepted.

---

## 15. Open questions

| ID | Question | Why it is open | Needed by | Default taken in this document |
|---|---|---|---|---|
| OQ-1 | Who writes and owns `manifests/join.go` (RR-3) and the `limits`/`purl` helpers (RI-1..RI-4)? The ecosystems-A unit probably wants the same join for Cargo/Python/Go | units are designed in parallel; neither document can see the other | B0 | this unit specifies the join fully (4.3) and writes it at B0-2; the core unit owns the internals |
| OQ-2 | Will ecosystems-A also use `yamlsafe` (pnpm lockfile multi-document, Python)? It then needs `MaxDocs` configurable per call and a documented behaviour for `---` streams | not visible from here | B0-3 | `Options.MaxDocs` is per call, default 8 |
| OQ-3 | Integrity encoding: this unit uses `<alg>:<hex>`; npm lockfiles carry SRI `sha512-<base64>`. One representation is needed for the SBOM export | cross-unit | B0-1 | `<alg>:<hex>` everywhere new; conversion from SRI at the npm parser (RR-13) |
| OQ-4 | **Spike S-A**: do GitHub App installation tokens work for `dependencyGraphManifests` and for the async SBOM generate/fetch endpoints (private and public repositories)? The REST OpenAPI shows no `enabledForGitHubApps` key for the two SBOM endpoints; the GraphQL schema reference does not state token types | cannot be answered without an App and a private repository | P5-3, P6-3 | providers ship disabled behind `acknowledge_spike_sa`; a negative result deletes `sbom` and keeps only `depsdev` |
| OQ-5 | ~~Do GitHub workflow files officially support YAML anchors/aliases?~~ **Resolved 2026-10-14**: docs.github.com "Reusing workflow configurations" has a section "YAML anchors and aliases" (`&` anchor, `*` alias). Merge keys (`<<`) are not mentioned there and stay unsupported (ignored with `yaml.merge-ignored`) | resolved | B2-2 | bounded alias expansion with cycle detection (4.1.4) |
| OQ-6 | NuGet `packages.lock.json` v3 (alias-keyed) layout: now confirmed from `PackagesLockFileFormat.cs` (`ReadTargetV3`, 2026-10-14); still no real v3 sample file was found | no public sample | B1-3 | shape detection accepts both layouts; synthetic fixture built from the reader's rules |
| OQ-7 | The `$/` same-commit form of `uses` appears in the Actions documentation (V-7) but no changelog entry was located to date it | documentation only | B2-2 | supported, single-sourced, cheap to remove |
| OQ-8 | Xcode `project.pbxproj` `XCRemoteSwiftPackageReference` parsing would give real direct dependencies for iOS apps | out of scope, old-style plist grammar | post-v1 | lock-only units synthesise heuristic direct dependencies (J-5) |
| OQ-9 | Real Swift `Package.resolved` v1 and v2 samples with permissive licence | only a v3 sample was found | B4-1 | synthetic fixtures from the SwiftPM source structures |
| OQ-10 | Folding `registry.opentofu.org` into `registry.terraform.io` in provider IDs: correct for the mirrored `hashicorp/*` providers, unverified for OpenTofu-only providers | no OpenTofu registry doc was consulted | B5-1 | fold; `TargetDisplay` keeps the original source string |
| OQ-11 | Helm chart *producers*: a chart's repository is not in its `Chart.yaml`, so channel A cannot match; should the org config declare chart repository URLs per repo (`catalog` integration) so `Provides` can be emitted? | product decision | B5-2 | no `Provides`; umbrella charts link via `file://` |
| OQ-12 | deps.dev request limits are undocumented | docs silent; live calls succeeded | P5-2 | 2 rps, burst 4, breaker; revisit after the first cold start |
| OQ-13 | ~~Is CocoaPods version ordering the RubyGems ordering?~~ **Resolved 2026-10-14**: `CocoaPods/Core` `lib/cocoapods-core/version.rb` defines `Pod::Version < Pod::Vendor::Gem::Version` ("based on the RubyGems class", adds `-` prerelease and `+` metadata); `VersionSystem "rubygems"` is right, with the opaque fallback on parse failure | resolved | B4-2 | `rubygems` |
| OQ-14 | NuGet `packages.<project>.lock.json` naming rule (documented behaviour recalled, not re-read) | not re-verified | B1-3 | match both `packages.lock.json` and `packages.*.lock.json`, prefer project-named |
| OQ-15 | Does the core unit provide the default per-directory assembler (RR-9) and the `_local` resolution (RR-10), or must each ecosystem assembler do it? | cross-unit | B0-1 | required from CORE; if absent, B0-2 adds both (they are 40 lines) |
| OQ-16 | ~~Core OpenAPI lacks `provenance`~~ **Resolved against the current draft** (`api/openapi-public.yaml`, re-read 2026-10-14 after the author's reading): `Dependency.provenance` is a required enum `own, sbom, github-graph, deps-dev` and `source` has `project`, which the scaffold's `SourceProject` maps. Still open: the draft has no `enrichedAt`/data-licence attribution field (4.4.5 needs "Data: deps.dev (CC BY 4.0)" when any value has `provenance: deps-dev`), and `Artifact.kind` is a closed enum (RR-23) | cross-unit contract | P5-4 | ask the core unit for `enrichedAt` and a response-level `attribution: [string]`; until then the footer is the SPA's rule on `provenance` alone |
| OQ-17 | Should `FactDeprecated` be emitted from deps.dev `isDeprecated`/`deprecatedReason`? The fields exist (verified), but NuGet reports *unlisted* versions as deprecated with reason `Unlisted` | product decision on what "deprecated" means in the version-spread view | P5-2 | not emitted in v1; revisit with a reason allow-list |
| OQ-18 | The adapter unit's document (`05-github-adapter.md`) specifies no arbitrary `GraphQL(ctx, repo, query, vars)` method and no `GenerateSBOMReport`/`FetchSBOMReport`; it exposes only `Capabilities().AsyncSBOM` (RR-16 is therefore **unmet today**) | cross-unit, gated by spike S-A | P5-3, P6-1 | providers compile against their own consumer ports; `cmd/scp/assert.go` rows for them are added when the adapter adds the methods; until then both providers cannot be enabled and `cmd/scp` fails startup with `ErrProviderConfig` if configured |
| OQ-19 | The core unit's design document is absent from `20-design/`; only its scaffold and OpenAPI draft were available, so 2.5 binds this unit to the scaffold's names | cross-unit | B0-1 | 2.5 table; re-check when `11-manifests-core.md` lands |

---

## 16. Appendix A: verification log (all fetched or executed on 2026-10-14 unless stated)

| ID | Fact | Source |
|---|---|---|
| V-1 | Bundler lockfile sections `GIT`, `GEM`, `PATH`, `PLUGIN SOURCE`, `DEPENDENCIES`, `CHECKSUMS` (since 2.5.0), `CONTENT ADDRESSES` (since 4.1.0), `PLATFORMS`, `RUBY VERSION`, `BUNDLED WITH`; indentation 2/4/6 spaces; `NAME_VERSION` shape with optional `!` and checksum | `ruby/rubygems`, `lib/bundler/lockfile_parser.rb` at commit 6db466b (master moved to 4413aee the same day) |
| V-2 | `composer.lock` keys (`content-hash`, `packages`, `packages-dev`, `aliases`, `minimum-stability`, `stability-flags`, `prefer-stable`, `prefer-lowest`, `platform`, `platform-dev`, `platform-overrides`, `plugin-api-version`); name pattern lower-case; `PLATFORM_PACKAGE_REGEX` (`php(-64bit\|-ipv6\|-zts\|-debug)?\|hhvm\|(ext\|lib)-...\|composer(-plugin\|-runtime)?-api`); `getContentHash` is `md5` over selected keys | `composer/composer`: `res/composer-lock-schema.json`, `res/composer-schema.json`, `src/Composer/Repository/PlatformRepository.php` |
| V-3 | `Package.resolved` v1 `{object:{pins:[{package,repositoryURL,state{branch,revision,version}}]},version:1}`; v2/v3 `{pins:[{identity,kind,location,state}],version,originHash(v3)}`; kinds `localSourceControl\|remoteSourceControl\|registry` | `swiftlang/swift-package-manager`, `Sources/PackageGraph/ResolvedPackagesStore.swift` (renamed from the pins store) |
| V-4 | Podfile.lock keys `PODS`, `DEPENDENCIES`, `SPEC REPOS`, `EXTERNAL SOURCES`, `CHECKOUT OPTIONS`, `SPEC CHECKSUMS`, `PODFILE CHECKSUM`, `COCOAPODS`; `PODS` is a mix of strings and single-key maps | `CocoaPods/Core`, `lib/cocoapods-core/lockfile.rb`; real file `layoutBox/FlexLayout/Podfile.lock` |
| V-5 | pub `dependency` values `direct main`, `direct dev`, `direct overridden`, `transitive`; sources `hosted`, `git`, `path`, `sdk`; hosted description `{name,url,sha256}`; git `{url,ref,resolved-ref,path}`; path `{path,relative}`; top-level `sdks`; pub workspaces: `workspace:` list (globs from Dart 3.11), `resolution: workspace`, one lock at the root | `dart-lang/pub`, `lib/src/lock_file.dart`; dart.dev pub workspaces and pubspec pages |
| V-6 | `mix.lock` real shape (`{:hex, :name, "ver", "inner", [managers], [deps], "hexpm", "outer"}`, `{:git, url, rev, opts}`); Mix evaluates the lock with `Code.eval_quoted`; `mix.exs` dependency options (`:only`, `:optional`, `:runtime`, `:override`, `:git`, `:github`, `:path`, `:in_umbrella`, `:hex`, `:repo`) | `phoenixframework/phoenix` `mix.lock` (40 entries) and `mix.exs`; hexdocs `Mix.Tasks.Deps` |
| V-7 | Actions `uses` forms (`{owner}/{repo}@{ref}`, `{owner}/{repo}/{path}@{ref}`, `./path`, `docker://image:tag`, and `$/path` for the same repository at the running commit); reusable workflow forms; `container.image`, `services.<id>.image`; composite action `runs.using: composite`, `runs.steps[].uses`, `runs.image` `docker://...` or `Dockerfile`; file names `action.yml`/`action.yaml` | docs.github.com Actions workflow syntax and metadata syntax pages |
| V-8 | purl-spec v1.1.0 (2026-10-07): no `terraform` or `helm` type; `docker` (`repository_url` qualifier), `github` (lower-case), `swift` (host + owner namespace, case-sensitive), `pub` (lower-case), `hex` (optional org namespace), `cocoapods` (case-sensitive, subspec in subpath), `gem` (`platform` qualifier), `composer` (lower-case), `nuget` (case-preserving name), `oci` (no namespace) | github.com/package-url/purl-spec (type definitions) |
| V-9 | `.terraform.lock.hcl` `provider "<host>/<ns>/<type>" { version, constraints, hashes }`, no module tracking; `required_providers` `source = "[host/]ns/type"`, default `registry.terraform.io/hashicorp/<local>`, older string-only form; module source forms; `source` must be a literal or a `const` variable | developer.hashicorp.com Terraform language documentation (dependency lock file, provider requirements, module sources) |
| V-10 | `github.com/hashicorp/hcl/v2` v2.25.0 (2026-09-15); `hclsyntax.ParseConfig` has no recursion cap (crash thresholds measured, E-B1); `go.yaml.in/yaml/v3` v3.0.5; `golang.org/x/text` v0.43.0; `github.com/package-url/packageurl-go` v0.1.7 (`Normalize()` mutates and returns `error`); `go.opentelemetry.io/otel/metric` and `trace` v1.47.0 (2026-10-02) | `proxy.golang.org` module listings, local `go get` |
| V-11 | NuGet MSBuild properties: `PackageId` defaults to `$(AssemblyName)`; `PackageVersion` defaults to `Version`; `IsPackable` defaults to true; default `PrivateAssets` is `contentfiles;analyzers;build`; `Version="3.6.0"` means `>= 3.6.0`; central package management (`Directory.Packages.props`, `PackageVersion`, `VersionOverride`, `GlobalPackageReference`) | learn.microsoft.com NuGet MSBuild-properties, PackageReference and central-package-management pages |
| V-12 | `packages.lock.json` fields `type` (`Direct`, `Transitive`, `Project`, `CentralTransitive`), `requested`, `resolved`, `contentHash` (64 decoded bytes = SHA-512, confirmed on a real file), `dependencies`; format versions with `AliasedVersion = 3` | `NuGet/NuGet.Client`, `src/NuGet.Core/NuGet.ProjectModel/ProjectLockFile/PackagesLockFileFormat.cs`; `microsoft/PowerApps-Tooling` lock |
| V-13 | GitHub synchronous SBOM endpoint removed after 2026-11-13; async pair `generate-report` (201 `{sbom_url}`) and `fetch-report/{uuid}` (202 or 302 temporary URL; reports kept up to a week); OpenAPI description 1.1.4 `x-github` for the pair has no `enabledForGitHubApps` key (`create-repository-snapshot` has `true`), `githubCloudOnly:false`; SPDX 2.3 example with `externalRefs` purl and `relationships` | docs.github.com REST dependency-graph SBOMs page; `github/rest-api-description` |
| V-14 | `dotnet/aspnetcore` `Directory.Packages.props`: 225 `PackageVersion` items, 207 unresolved `$(...)`, `<Choose><When>`; `Directory.Build.props` sets `ManagePackageVersionsCentrally` under `Condition="'$(ManagePackageVersionsCentrally)' == ''"`; real csproj files start with a UTF-8 BOM | the two files, fetched |
| V-15 | Helm `Dependency` fields (`name`, `version`, `repository`, `condition`, `tags`, `enabled`, `import-values`, `alias`); lock `generated`, `digest`, `dependencies`; repository forms `file://`, `oci://`, `@name`, `alias:name`, https; `requirements.yaml` and `Chart.lock` loaded | `helm/helm`: `pkg/chart/v2/dependency.go`, `pkg/downloader/manager.go` |
| V-16 | GitHub GraphQL `Repository.dependencyGraphManifests` arguments; `DependencyGraphManifest` and `DependencyGraphDependency` fields (`packageUrl`, `relationship` in public preview); live introspection of `DependencyGraphEcosystem` = `RUBYGEMS, NPM, PIP, MAVEN, NUGET, COMPOSER, GO, ACTIONS, RUST, PUB, SWIFT`; live query on `layoutBox/FlexLayout` returned `Package.resolved` -> `github.com/facebook/yoga`, `= 3.2.1`, `relationship "unknown"`, `pkg:swift/github.com%2Ffacebook%2Fyoga@3.2.1`, and a workflow -> `actions/checkout`, `= 2.*.*`, `pkg:githubactions/actions/checkout@2.%2A.%2A`; query cost 1, remaining 4993 (user token, **not** an App token) | docs.github.com GraphQL reference; `gh api graphql` run 2026-10-14 |
| V-17 | `deps.dev/util/semver` (commit 7bac2db56a63, 2026-10-13) systems: DefaultSystem, Cargo, Go, Maven, NPM, NuGet, PyPI, RubyGems, Composer | module source read after `go get` |
| V-18 | deps.dev v3: `GetPackage`, `GetVersion` (licenses, advisoryKeys, links, relatedProjects), `GetRequirements` (NuGet, RubyGems and others), `GetDependencies` (npm, Cargo, Maven, PyPI only); systems GO, RUBYGEMS, NPM, CARGO, MAVEN, PYPI, NUGET; lower-case system names; NuGet package names lower-cased in the path; data CC-BY 4.0 with caching permitted; rate limits not documented; live calls for `nuget/newtonsoft.json` and `rubygems/rails` succeeded, `cache-control: max-age=3600`, CORS `*` | docs.deps.dev API v3 pages; `curl https://api.deps.dev/v3/...` |

## 17. Appendix B: experiments (scratch code, throwaway; numbers are reproducible with the methods stated)

| ID | Experiment | Result |
|---|---|---|
| E-B1 | HCL recursion depth versus process death (hcl/v2 v2.25.0, Go 1.27.1) with generators for nested brackets, parens, braces, template `${`, unary `!`, ternary chains, nested blocks; then a `LexConfig`-based pre-scan | crashes: brackets/parens about 70,000 (50,000 ok), braces and template 100,000, unary 500,000 (250,000 ok), blocks 300,000, ternary 500,000 (200,000 ok); 10,000 bracket levels = 36 ms; pre-scan of 200,000 tokens = 17 ms; `terraform-aws-modules/terraform-aws-vpc` `main.tf` (63 KB): depth 7, 134 ternaries, 12,110 tokens; function call attribute -> unknown value + diagnostics, no panic; `.terraform.lock.hcl` parses |
| E-B3 | `encoding/xml` token loop with depth counter | 225 `PackageVersion` refs in 0.3 ms (207 unresolved); `<!ENTITY>` use -> "invalid character entity"; iso-8859-1 decoded through `charmap`; utf-16 rejected; 100,000-deep XML aborted at depth 257; 200,000 `PackageReference` (8.6 MB) in 116 ms |
| E-B4 | Ruby line parser on real lockfiles | Rails: 269 spec lines = 233 gems + 36 platform variants, 397 edges, 82 direct, 256 checksums, 5 platforms, `BUNDLED WITH 4.0.12`; meta-tags: 112 specs, 111 checksums |
| E-B5 | Elixir tokenizer + literal reader (no evaluation) | `phoenix/mix.lock`: 40 entries (matches `grep`); `mix.exs`: 24 deps, 4 optional, 14 with `only` (match greps) |
| E-B6 | `go.yaml.in/yaml/v3` Node decoding | `pubspec.lock` 58 packages (32 direct, 26 transitive); Podfile.lock `PODS` mixed scalars/maps; workflow `on:` stays `!!str`; Helm `Chart.yaml` 5 dependencies with a missing `repository`; Node decoding does not trip the alias-ratio check and aliases expand only when walked; a 100,000-deep flow sequence -> "exceeded max depth of 10000" in 3.8 ms |
| E-B7 | Dockerfile prototype | continuation joining; global `ARG` substitution; unresolved `${DOCKER_VERSION}`-style chains resolve through defaults in the `docker/buildx` file; stage aliases not external; `scratch` skipped |
| E-B8 | `packageurl-go` v0.1.7 | `Normalize()` mutates the receiver and returns `error`; no `KnownTypes` symbol (do not rely on either; `purl.Format` is own code) |

## 18. Appendix C: what was compile-checked

Scratch module `github.com/cantara/source-code-portal`, Go 1.27.1, `go vet ./...` and `go build ./...` clean on 2026-10-14; **a second pass on the same day (revision 1.1, section 19) compiled the corrected surface inside the core unit's reference scaffold**, with three small test files: `TestIDs` (all 27 parser IDs register in a real `NewRegistry()`; the revision-1 IDs fail 11 of 27), `TestClassification` (the three error types against the scaffold's `job` package) and `TestStrictDecode` (the ecosystem `Config` types with their yaml tags and `manifests.ByteSize` under `KnownFields(true)`). First-pass contents (1,300 lines of stubs): `internal/scm` (`RepoRef`, `Visibility`); `internal/manifests` (research 5.4 model verbatim plus `FormatPURL`, `Enriched`, RR-2..RR-8 signatures, `source.go`, `enrich.go`); `internal/manifests/internal/{limits,purl,version,yamlsafe,rubydsl}`; the eleven ecosystem packages (constants, `Config`, `DefaultConfig`, `Validate`, `Spec`, `Register`); the three providers (`Config`, port interfaces, `Deps`, `New`, `Name`, `Facts`, and `var _ manifests.DependencySource = (*Provider)(nil)`). `go list` import audit: ecosystems import only `internal/manifests`; `githubgraph` and `sbom` import `internal/manifests` and `internal/scm`; `depsdev` imports `internal/manifests`; `limits`, `purl`, `version`, `yamlsafe`, `rubydsl` import nothing internal. Compiled signatures of `yamlsafe` and `rubydsl`:

```go
// package yamlsafe (compiled)
type Kind uint8 // Scalar, Mapping, Sequence
type Node struct {
	Kind Kind; Tag, Value string; Line int; LineComment string
	Pairs []Pair; Items []*Node
}
type Pair struct{ Key string; KeyLine int; Value *Node }
type Options struct{ MaxDepth, MaxNodes, MaxDocs, AliasBudget int }
type Problem struct{ Code string; Line int; Msg string }
func Decode(ctx context.Context, data []byte, o Options) (docs []*Node, probs []Problem, err error)
func (n *Node) Get(key string) *Node
func (n *Node) Str() (string, bool)
func (n *Node) Seq() []*Node

// package rubydsl (compiled)
type Token struct{ Kind Kind; Text string; Words []string; Interp bool; Line int }
type Arg struct{ Key string; Value []Token }
func Scan(data []byte, tick func() error) ([]Token, error)
func Call(toks []Token, i int) (args []Arg, next int)
func Literal(v []Token) (string, bool)

// package limits (reference scaffold, the names this unit uses; RI-1, RI-2)
func NewGuard(ctx context.Context, lim Limits) *Guard
func (g *Guard) CheckSize(n int) error
func (g *Guard) Node() error                  // "Tick" in the prose of this document
func (g *Guard) Poll() error
func (g *Guard) Enter() error
func (g *Guard) Leave()
func (g *Guard) Expand(n int) error
type LimitError struct{ Limit string; Max int }       // errors.As; Limit: size|depth|nodes|expand|timeout
func Text(data []byte) ([]byte, error)                 // *EncodingError{Reason}
func StripBOM(b []byte) []byte
func RejectBinary(b []byte) ([]byte, error)
func CharsetReader(label string, input io.Reader) (io.Reader, error)  // iso-8859-1, windows-1252 only

// package purl (reference scaffold; RI-4)
func Build(eco, namespace, name, version, qualifiers string) (string, error)
func RepoKey(raw string) (key string, ok bool)

// package manifests additions of this unit, all compiled in the second pass (revision 1.1)
type PURLFormatter func(id ComponentID, display, version, qualifiers string) (purl string, ok bool)   // RR-1, field EcosystemSpec.FormatPURL
func NewParser(id string, version int, eco EcosystemID, match func(string) bool, parse func(context.Context, File, Limits) (Parsed, error)) Parser // RR-2
var ErrProviderConfig error
type RateLimitError / PermanentError / GoneError   // RR-20, each asserted against job.RateLimited / job.Permanent / job.Gone
type EnrichConfig, EnrichDeps; func (s *Service) EnrichSpecs(EnrichConfig, EnrichDeps) []job.Spec
type JoinOptions                                    // without Ancestors
```

## 19. Independent verification log (revision 1.1, 2026-10-14)

Verifier: independent of the author. Method: read the whole document; re-read the binding inputs (`04-decisions.md`, `10-architecture.md` sections 4, 5, 8, 13, 15, 16, 17, 18, ADR-021/022); compiled and ran the surface inside the core unit's reference scaffold (`11-manifests-core.reference.tar.gz`, Go 1.27.1); ran experiments against hcl/v2 v2.25.0, yaml v3.0.5 and graphql-js 16.9; fetched the live sources named below. The full report with severities is `30-verification/design-13-manifests-ecosystems-b.md`. Findings are numbered F-nn there and here.

### 19.1 Defects found and fixed in this document

| F | Severity | Defect (revision 1) | Fix (where) |
|---|---|---|---|
| F-01 | high | 11 of 27 parser IDs (`ruby/*`, `php/*`, `dart/*`, `elixir/*`, `actions/*`) are rejected by `Registry.Register` (`must be "<ecosystem>/<format>"`) | IDs renamed to the `EcosystemID` prefix, D-8, 1.2, `TestRegisterAllParserIDs` |
| F-02 | high | Parsers emitted repo-relative paths (`_local/<path>`, `Members`, `SourceRef`, `Loc.Path`, NuGet `Kind` from the file name) while the cache key is the blob SHA alone: a second copy of the same bytes at another path would be served the first copy's paths | 4.1.9 (path independence), RR-10, J-0, 2.5 rows 6-10, `TestParserPathIndependence`, `TestPathEscape` |
| F-03 | high | Enrichment: both enrich jobs wrote one item `manifests.enrich` (lost update) and wrote the slot directly (an older tree could overwrite a newer one) | one item per job kind, `Service.Reapply` under a per-repository lock (RR-19, 4.4.2 steps 4-5, 8), D-9, `TestEnrichItemsSeparate`, `TestEnrichRefreshRace` |
| F-04 | high | Providers received the merged units, so a second run saw its own earlier additions as "present", did not re-emit them and the next persisted item lost them (flapping) | "own view" rule (4.4.2 step 1), `TestEnrichRerunStable` |
| F-05 | high | `generate-report` was specified as POST; the REST description 1.1.4 defines `GET /repos/{owner}/{repo}/dependency-graph/sbom/generate-report` (`dependency-graph/generate-sbom-report`) | 4.4.4, `TestSBOMMethods` |
| F-06 | high | Providers were told to return `job.RateLimited` / `job.Permanent`, but `job` is not in the providers' import row, and a single error type with a `RetryAt()` method would classify every failure as rate-limited | three distinct types in `manifests/source.go` (RR-20, compiled against the scaffold's `job`), status table in 4.4.2, `TestProviderErrorClassification` |
| F-07 | medium | Join `Ancestors=true` gave a standalone pubspec the lock of its parent package; the member rule was ambiguous | `Ancestors` deleted; membership-only ancestor lock (J-4), `NonMemberNeverReadsAncestorLock` |
| F-08 | medium | `terraform/lock` (`.terraform.lock.hcl`) was not covered by the HCL pre-scan; the pre-scan pseudo-code used token names that do not exist (`OBrace`, ...) and its depth rule was unexplained | 4.2.10: real `hclsyntax.Token*` names, both HCL parsers, measured notes, `LockCrashShape`, `IterativeShapes`, `DepthBoundary` (V-F4) |
| F-09 | medium | `yamlsafe`: the library accepts self-referential anchors (`a: &a [*a]`); `yaml.Unmarshal` into a Node reads only the first document; neither was handled | alias stack and `yaml.alias-cycle`, decoder loop (4.1.4), `TestYAMLAliasCycle` |
| F-10 | medium | Config: a second ecosystem switch beside the core's `acquisition.disabled_ecosystems`; ecosystem `Config` types without yaml tags; a polymorphic two-pass `providers[]` decode that the env layer cannot drive; no `refresh.upstreams.deps-dev` although `Register` panics for an unknown upstream | RR-21, RR-22, section 6, flat `ProviderConfig`, `TestStrictDecode` |
| F-11 | medium | `Artifact.Kind` values `tool`, `metapackage`, `replace` are outside the core OpenAPI's closed enum (response validation would fail) | RR-23, NuGet and Composer mapping |
| F-12 | medium | `purl.RepoURL` (case preserved, any depth) is not the core's `purl.RepoKey` (lower-case `host/owner/repo`, scheme-less input rejected): channel B would not match | 4.1.6, RI-4, 12.4 |
| F-13 | medium | `limits` API names, limit names (`bytes`), text gate and the done-context rule differ from the scaffold; `Unit.ID`, `Role` and `LockedCount` were set by the assembler although the root's `finalize` owns them | 2.5 table, RR-4/5/17, skeleton in 4.1.1, J-6 |
| F-14 | medium | deps.dev URL path built from untrusted names without validation or escaping | `invalid_coordinate` rule and test (4.4.5, 12.5) |
| F-15 | medium | Path dependencies that name their package (Dart, CocoaPods) were turned into anonymous `_local` targets | 3.2, 4.2.5, 4.2.6 |
| F-16 | low | `blobPath` split at the first `/` breaks refs such as `feature/x` | longest-suffix rule (4.4.3) |
| F-17 | low | 14 tests named in the trace table 11.2 had no definition in 12 | 12.7 |
| F-18 | low | Trace table lacked the shared-breaker, freshness, failure-handling, upstream-budget, rate-aware and logging guardrails (GR-22, GR-26, GR-24, GR-25, GR-23, GR-42) | 11.2 |
| F-19 | low | Stale or wrong facts: deps.dev cold-start 70 vs 50 minutes; `isDeprecated` was "not verified" (it exists, with a NuGet "Unlisted" caveat, OQ-17); OQ-5, OQ-6, OQ-13, OQ-16 answered by evidence | 4.4.5, section 15 |
| F-20 | low | A Gemfile.lock with merge-conflict markers (Bundler raises on them) had no diagnostic; lock-only join ignored the lock's own `DEPENDENCIES` rows | `ruby.merge-conflict`, J-5 |
| F-21 | low | 4.4.2 referred to a "per-status classification table" that did not exist | table added |

### 19.2 Facts re-verified (all 2026-10-14)

| V-F | Fact | Evidence |
|---|---|---|
| V-F1 | Scaffold `Registry.Register` rejects IDs without the `<EcosystemID>/` prefix | `TestIDs` run: 11 failures with the revision-1 IDs, 0 with the renamed set |
| V-F2 | Parse cache key excludes the path | architecture 13.4 step 2, ADR-021, scaffold `ParseKey(blobSHA, parserID, version)` and the comment on `File` ("Path is context for diagnostics only") |
| V-F3 | Module versions: `hcl/v2` v2.25.0 (2026-09-15), `go.yaml.in/yaml/v3` v3.0.5 (2026-07-26), `x/text` v0.43.0 (2026-10-09), `otel/metric` v1.47.0 (2026-10-02), `packageurl-go` v0.1.7 (2026-08-21) | `proxy.golang.org/<module>/@latest` |
| V-F4 | HCL: `hclsyntax.TokenOBrace` ... `TokenQuestion` exist; the pre-scan rejects 11 shapes at 100,000 in 8-80 ms; depth 256 accepted and 257 rejected; brackets at 100,000 end in `fatal error: stack overflow` without it; `a[0]` x100k, `.y` x100k, `+ 1` x100k, `&& true` x100k, `%{ if }` x100k parse in 20-185 ms | scratch program `hcl/main.go` |
| V-F5 | `Pod::Version < Pod::Vendor::Gem::Version` | `CocoaPods/Core` `lib/cocoapods-core/version.rb` |
| V-F6 | deps.dev `GetPackage` and `GetVersion` carry `isDeprecated` and `deprecatedReason`; `GetPackage` is about 175-210 bytes per version; a non-existent package is 404; `Newtonsoft.Json` (mixed case) also answers 200 | live `curl` |
| V-F7 | GraphQL `DependencyGraphEcosystem` = `ACTIONS COMPOSER GO MAVEN NPM NUGET PIP PUB RUBYGEMS RUST SWIFT`; `DependencyGraphManifest implements Node`; both queries (4.4.3) validate with 0 errors against the published schema; `packageManager` is a nullable `String`, `packageUrl` and `relationship` are "Public preview" | `docs.github.com/public/fpt/schema.docs.graphql`, graphql-js 16.9 |
| V-F8 | SBOM: `generate-report` is GET (201), `fetch-report` GET (202, 302), sync endpoint `deprecationDate 2026-05-12`, `removalDate 2026-11-13`; no `enabledForGitHubApps` on the three SBOM operations while `create-repository-snapshot` has `true` | `rest-api-description` 1.1.4 `api.github.com.json` |
| V-F9 | purl-spec v1.1.0 (2026-10-07): types present `docker`, `github`, `swift`, `pub`, `hex`, `cocoapods` (namespace prohibited, subspec in subpath), `composer`, `gem`, `nuget`; no `terraform`, no `helm`; swift examples are case-preserving | `package-url/purl-spec` releases and `types/*-definition.json` |
| V-F10 | Actions: `$/path` exists (not in GHES, no `@ref`); YAML anchors and aliases are documented for workflows | docs.github.com workflow-syntax and "Reusing workflow configurations" |
| V-F11 | Bundler sections and `CHECKSUMS` since 2.5.0, `CONTENT ADDRESSES` since 4.1.0; merge-conflict markers raise | `ruby/rubygems` `lib/bundler/lockfile_parser.rb` (master) |
| V-F12 | Composer `PLATFORM_PACKAGE_REGEX` equals the list in 4.2.3 | `composer/composer` `PlatformRepository.php` |
| V-F13 | NuGet lock v3: `AliasedVersion = 3`, root keys with `framework` and `dependencies` | `NuGet.Client` `PackagesLockFileFormat.cs` |
| V-F14 | yaml v3.0.5 Node decode: no alias expansion (a 341-byte laughs file in 100 us), self-referential anchors accepted, `max depth 10000` error for 100,000-deep flow and 200,000-deep block sequences in 2-4 ms, `on` keeps `!!str`, `yes: no` stays strings, `version: 1.10` is `!!float` with text `1.10`, line comment `# v7.0.1` is on the scalar, undefined anchor is an error | scratch program `yml/main.go` |

### 19.3 Not resolved by this verification (cross-unit or external)

1. RR-16: the adapter document specifies no `GraphQL(...)`, `GenerateSBOMReport` or `FetchSBOMReport` (OQ-18); spike S-A (OQ-4) still gates both GitHub-backed providers.
2. The core unit's design document is absent (OQ-19): 2.5 binds this unit to the scaffold; RR-10 (graph handling of `_local`), RR-19 (`Reapply`), RR-22 and RR-23 need the core unit's acceptance.
3. deps.dev rate limits remain undocumented (OQ-12); NuGet v3 and Swift v1/v2 lock fixtures remain synthetic (OQ-6, OQ-9).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: gauge unit `1` metrics export as `_ratio` in the metric table (CD-MT-03; 91).
- Open: spike S-A (installation tokens for dependency graph and SBOM), providers stay off by default; the fake GitHub has no dependency-graph endpoints (fixtures matrix).
- Providers import the otel API only (D-5, accepted).
