# ADR-021: Manifests parser strategy: build vs adopt per ecosystem, purl identity, SBOM as secondary source

Status: Accepted. Date: 2026-10-14. Refines 04 D-15 "Ecosystems" and "Build vs adopt" (A-15).

## Context
D-15 requires parsing package-manager data for all major languages with purl identity, never executing build tools, bounded and fuzzed pure parsers, new ecosystems by registering one interface, and evaluation of adoptable libraries and the GitHub SBOM source. A parser whose output has no consumer is dead weight, so every parsed fact must feed the graph, the API or a badge (GR-04).

## Decision
- Pure `Parser` (`File{Path, Data []byte, BlobSHA}` in, `Parsed` out; no I/O, clock, network or globals; deterministic; never panics) plus optional `Assembler`, registered in a `Registry`; per-ecosystem subpackages import only the `manifests` root. Cache key `(blobSHA, parserID, parserVersion)`.
- Adopt leaf libraries: `golang.org/x/mod/modfile` (Go), `deps.dev/util/{maven,semver}` (POM model and version ordering; pinned by commit; wrapped; vendoring fallback), `packageurl-go` behind an own per-ecosystem normaliser (it does not implement PEP 503 and does not enforce `pub` lowercasing), `pelletier/go-toml/v2` (BurntSushi is quadratic: 40 KB of nesting took 3.8 s), `go.yaml.in/yaml/v3` (alias-bomb and depth protection, multi-document `Decoder` loop for pnpm), `hashicorp/hcl/v2` (Terraform, T2), `cyclonedx-go` v0.12.0 at the export boundary only.
- Build: npm family (package.json, package-lock, pnpm two-document lockfile, yarn v1/Berry, `bun.lock` via a JSONC pre-pass), Python (PEP 621/Poetry/uv/requirements/pylock with an own PEP 508 parser), Cargo, NuGet/MSBuild subset, Gradle settings/catalog/lockfile, Maven assembler with a pre-computed interpolation expansion bound, Ruby/PHP/Swift/Dart/Elixir/CocoaPods/Docker/Actions/Helm.
- Gradle build scripts, `Package.swift`, `Gemfile`, `*.gemspec`, `Podfile`, `mix.exs` are programs: lexical heuristics with `Confidence=Heuristic` and diagnostics; Kotlin is a language fact on Maven/Gradle units, not a separate ecosystem.
- `osv-scalibr` is **not adopted** as a library (the OSV scanner's lockfile parsers are scalibr extractors, a heavy dependency) but kept as an **oracle** in a separate optional module for differential tests; `cyclonedx-gomod` shells out to `go` and is excluded.
- Identity: `ComponentID{Ecosystem, Namespace, Name}` normalised per ecosystem, equal to the purl type where one exists; raw requirement and exact version stored separately; versions compared with the ecosystem's system, raw strings preserved.
- Limits and fuzzing as in architecture section 13.2; never execute anything; no archive parsing; paths cleaned and symlinks/submodules not followed.
- SBOM and dependency graph are **secondary, optional** `DependencySource` providers with provenance and a strict precedence; the synchronous SBOM endpoint is removed on 2026-11-13 (verified 2026-10-14); the async pair is used only after an App-token spike (S-A). Third-party providers receive only public coordinates.
- Export CycloneDX first (1.0-1.7 supported); SPDX export deferred (tools-golang handles only 2.1-2.3).

## Alternatives considered
Adopt scalibr or syft (heavy, execution-oriented extras, API stability unknown), GitHub dependency graph as the primary (inaccurate for pnpm multi-document lockfiles and PEP 735 groups; private repos may need Advanced Security), ecosyste.ms (share-alike terms, leaks private manifests).

## Consequences
We own about 20 small parsers; each ships goldens, a fuzz target and an oracle diff. Parser drift is watched by a nightly canary on public repositories.

## Evidence
`05-research/07-manifest-parsing.md` sections 2-5, 7, 13-15 (experiments E1-E11); https://docs.github.com/en/rest/dependency-graph/sboms (verified 2026-10-14).

## Amendment 2026-10-14 (review revision)

`DependencySource` is created together with the first provider (P5), not in v1 (F-25). Registry tree adds `cocoapods` (listed here and in the research but missing from the first tree); C/C++ (Conan, vcpkg) and Scala (sbt) are recorded as post-v1 registry additions (m7). `PomSource` never looks up a coordinate produced by an org unit or matching `private_group_prefixes` (m5). Encoding policy (m3): UTF-8 BOM stripped, UTF-16 and NUL rejected with a diagnostic, XML declared `iso-8859-1`/`windows-1252` supported through `x/text/encoding/charmap` (exports verified), other charsets rejected. Parsers poll `ctx.Err()` at token boundaries; the per-file watchdog is a last resort that keeps its memory weight until the goroutine exits and trips a per-parser circuit after 3 abandonments (F-14). Licences are declared-only in v1 and the UI reports coverage (m4).
