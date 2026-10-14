# Package manifests
Status: v1 design note (M7), 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope
- `internal/manifests` (feature): per repository, the `go.mod`, `package.json` and `pom.xml` files read as untrusted input, the
  technology they state (languages, build tools, toolchain versions) and their declared dependencies.
- Internal dependencies: which catalogued repositories a repository uses and is used by, matched on the names manifests declare.
- `GET /api/v1/repos/{owner}/{name}/manifests`, a technology and dependency panel on the repository page, a section in the
  repository's Markdown.

## Decisions
- Declared dependencies only: no lockfiles, no registry lookups, no build tools run. The manifest says what the repository asks for.
- One recursive tree call on `HEAD` finds the files (with ETag, so an unchanged tree costs nothing); a blob is fetched only when its SHA
  changed or the parser version moved. The tree's truncation flag is ignored, as in docs: very large trees lose their deepest files.
- Discovery: base names `go.mod`, `package.json`, `pom.xml`; at most 4 directory levels; directories `node_modules`, `vendor`, `testdata`,
  `test`, `tests`, `example`, `examples`, `fixtures` and any starting with `.` are skipped; shallow paths first, then by path; at most 30
  files; files over 512 KiB are listed with a `too-large` problem and not fetched.
- Parsers are pure functions of the bytes with fixed bounds (1,000 dependencies per file, names 256 bytes, versions 128 bytes, every string
  through `untrusted.Text`); a parse failure is a problem on that file, never a job failure.
  - `go.mod`: `golang.org/x/mod/modfile.ParseLax`: module path, `go`, `toolchain`, `require` (`// indirect` is scope `indirect`), `tool`.
  - `package.json`: `encoding/json` into a fixed struct: `name`, `version`, `dependencies`, `devDependencies`, `peerDependencies`,
    `optionalDependencies`, `engines.node`, `packageManager`. TypeScript when `typescript` is a dependency or `tsconfig.json` sits beside it;
    the build tool from `packageManager`, else the lockfile beside it (`pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, `package-lock.json`), else npm.
  - `pom.xml`: `encoding/xml` token stream (strict, no entities beyond the five predefined, depth 64, 200,000 tokens, UTF-8, US-ASCII or
    ISO-8859-1): coordinates (group and version inherited from `parent`), `parent` (scope `parent`), `dependencies` (scope `compile`
    and `runtime` are `runtime`, `provided` and `system` are `provided`, `test`), BOM imports in `dependencyManagement` (scope `import`),
    `properties`; `${name}` is replaced once from the properties and `project.version`/`project.groupId`/`project.parent.version`. Java
    version from `maven.compiler.release`, `maven.compiler.target`, `maven.compiler.source` or `java.version`.
- Names: Go the module path, npm the package name, Maven `groupId:artifactId`. A dependency is internal when another catalogued repository
  declares a manifest with the same ecosystem and name; a name declared by several repositories links to each of them.
- The index (name to repositories, repository to users) is built with each snapshot, like the commit feeds.
- Job `manifests` per repository: TTL 6 h plus `EventPush`. Tuning is constants; no config keys.

## Shapes
```go
// internal/snapshot
type Manifest struct {
    Path, Ecosystem, Name, Version string // ecosystem: go | npm | maven
    Language, BuildTool                    string
    Toolchain                              []Toolchain // go, node, java
    Dependencies                           []Dependency // sorted by scope order, then name
    Problems                               []Problem
}
type Toolchain struct{ Name, Version string }
type Dependency struct{ Name, Version, Scope string } // runtime dev peer optional provided test indirect tool parent import
// RepoEntry gains Manifests []Manifest (by path) and ManifestsFresh; Snapshot gains Providers(eco, name) and UsedBy(key).
```

API `GET /api/v1/repos/{owner}/{name}/manifests`: `technology` (languages, build tools, toolchains with their versions), `manifests`
(each dependency carries `repo` when it is internal), `uses` and `usedBy` (repository references with the names that link them),
`freshness`. Weak ETag as every API response; an old address answers 308.

## Tests
- Golden tables per parser (typical files, inheritance and interpolation, scopes, hostile input: deep nesting, entity declarations, a
  billion-laughs DTD, huge token counts, invalid UTF-8, bidi names); a fuzz target per parser (no panic, bounds hold).
- The job against the fake GitHub: discovery rules, blob reuse by SHA, too-large files, empty repositories.
- Internal links across repositories in the snapshot (both directions, multiple providers, self-reference ignored); end to end in
  `cmd/scp/run_test.go`; the panel in Vitest and the Playwright smoke.

## Reference (archive pointers)
- `../reference/detailed-design/20-design/11-manifests-core.md` sections 3 and 4.1-4.5 (model, parser contract, discovery).
- `../reference/detailed-design/20-design/12-manifests-ecosystems-a.md` (Maven, npm and Go format details).
