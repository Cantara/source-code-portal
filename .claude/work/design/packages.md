# Published packages (R1 to R4)

Status: R1 to R4 built (plan.md section 4). Binding order: ../plan.md > this note.

A repository's release is often not a GitHub release: a Java library is published to a Maven repository by a CI server, a
TypeScript one to npm, a Rust crate to crates.io, a Go module is a tag that the module proxy serves. The portal reads the
repository's own build files, finds what it publishes and where, and asks that registry for the newest version. Public registries
are read without credentials; a private registry is read with the credentials the operator configured for it, never with any other.

## The choice

**Package providers**, one per package manager, behind one interface, the way `internal/llm` keeps its language model providers:

```go
// internal/packages: the feature. A Provider knows one package manager.
type Provider interface {
	Ecosystem() string // "maven", "npm", "pypi", "cargo", "go", "swift", "nuget", "rubygems", "packagist", ...
	// Coordinates reads what a repository publishes from its build files (bounded parsers, no network).
	Coordinates(files Files) []Coordinate
	// Latest asks the registries of c for its newest release; nil when none is published.
	Latest(ctx context.Context, r Registries, c Coordinate) (*Version, error)
}

type Coordinate struct {
	Ecosystem string
	Name      string   // group:artifact for Maven, the package name elsewhere, the module path for Go
	Path      string   // the build file it came from
	Registries []string // where the build file says it publishes or resolves (distributionManagement, publishConfig, ...), in order
	Private   bool      // the build file says it is not public (npm "private", Cargo "publish = false"): no lookup
}

type Version struct {
	Version   string
	Published time.Time // zero when the registry does not say
	URL       string    // the package's page or directory at the registry, https
	Registry  string    // the registry's id (config) or host
}
```

A provider is pure Go, small and tested against recorded answers; adding a package manager is adding a provider. The providers are
registered in `cmd/scp` (a list, no `init()`).

Rejected: one generic "registry URL + JSON path" configuration (every registry has its own protocol, version order and
authentication); deps.dev or libraries.io as one source (public packages only, a third party between the portal and the registry,
no private registries).

## Providers

| Provider | Languages | Build files | Public registry | Protocol read | Private registries and their auth |
|---|---|---|---|---|---|
| `maven` | Java, Kotlin, Scala | `pom.xml` (the parent chain), `build.gradle(.kts)`, `gradle.properties`, `settings.gradle(.kts)` | Maven Central (`repo1.maven.org/maven2`) | `<repo>/<group path>/<artifact>/maven-metadata.xml`: `release`, `lastUpdated` | Nexus, Artifactory, Reposilite, GitHub Packages (`maven.pkg.github.com`), GitLab: HTTP Basic (user and token) or a bearer token |
| `npm` | TypeScript, JavaScript, React | `package.json` (`name`, `private`, `publishConfig.registry`), `.npmrc` (scoped registries) | `registry.npmjs.org` | `GET /<name>`, abbreviated (`application/vnd.npm.install-v1+json`): `dist-tags.latest`; `time` from the full document, streamed | Verdaccio, Nexus, Artifactory, GitHub Packages (`npm.pkg.github.com`), GitLab: a bearer token (`_authToken`) or Basic |
| `pypi` | Python | `pyproject.toml` (`[project].name`, Poetry, Hatch), `setup.cfg` | `pypi.org` | the Simple API in JSON (PEP 691, `application/vnd.pypi.simple.v1+json`): versions, upload times (PEP 700) | devpi, Nexus, Artifactory, GitLab, CodeArtifact: HTTP Basic |
| `cargo` | Rust | `Cargo.toml` (`[package].name`, `publish`, workspace members) | crates.io | the sparse index (`index.crates.io/<prefix>/<name>`, one JSON line per version), the newest not yanked | alternative registries with a sparse index (`config.json`, `auth-required`): the token as the `Authorization` header |
| `go` | Go | `go.mod` (`module`) | `proxy.golang.org` | `<module>/@latest`: `Version`, `Time` (case-encoded path) | a private module is its tags on GitHub, which the commits job reads already; a private proxy (Athens, Artifactory) with HTTP Basic |
| `swift`, `cocoapods` | Swift, Objective-C | `Package.swift` (the package name, read as text); a root `*.podspec` | the tags (SwiftPM resolves tags); CocoaPods trunk | tags; `trunk.cocoapods.org/api/v1/pods/<name>` | a Swift package registry (SE-0292): a bearer token or Basic (R3) |
| `nuget` | C#, F# | `*.csproj`, `*.fsproj`, `*.vbproj` that pack (`PackageId`, `IsPackable`, `GeneratePackageOnBuild`), `*.nuspec` | `api.nuget.org` | the flat container `<id>/index.json`; a configured feed's `PackageBaseAddress` from its service index `<url>index.json`, which must lie under its `url` (no published date) | Nexus, Artifactory, GitHub Packages: HTTP Basic (user and token); Azure Artifacts keeps its packages outside the feed's path and is not read |
| `rubygems` | Ruby | a root `*.gemspec` (`name`, read as text) | rubygems.org | `api/v1/versions/<name>.json` (a version with a letter is a pre-release); a configured server's compact index `info/<name>`, else its versions API | Gemfury, Artifactory, GitHub Packages, Gemstash: Basic or an API key header |
| `packagist` | PHP | `composer.json` (`name`, not `"type": "project"`) | packagist.org | `p2/<vendor>/<name>.json` (streamed; tagged releases only) | Private Packagist, Satis, Repman, Artifactory: a bearer token or Basic |

R1 ships `maven` and `npm`; R2 `pypi`, `cargo`, `go`, `swift`; R3 private registries; R4 `nuget`, `rubygems`, `packagist`. A
container image (GHCR, Docker Hub) is a later provider of the same interface.

## Where a package is looked up

1. The registries the build file names, in its order: Maven's `distributionManagement` (inherited through the parent chain: each
   parent POM fetched from the repositories the child lists, then Maven Central, at most 5 levels; a released parent never changes,
   so it is kept by its coordinates and never fetched again), npm's `publishConfig.registry` and `.npmrc` scopes, Cargo's
   `publish = ["<registry>"]`.
2. The configured registries of that ecosystem (`packages.registries[]`), in their order.
3. The ecosystem's public registry, only for a public repository: a private or internal repository's coordinates are never sent to a
   public registry (a Go module's versions are then its tags).

The first registry that knows the package answers. A registry that does not know it (404) is the next one's turn; a registry that
fails is a failure of this lookup only (the other packages of the repository are kept, P-49), retried with the job.

## Configuration

```yaml
packages:
  registries:
    - id: cantara                      # names the registry in the pages
      ecosystem: maven
      url: https://mvnrepo.cantara.no/content/repositories/releases/
    - id: internal-npm
      ecosystem: npm
      url: https://npm.example.org/
      auth: { bearer_file: /run/secrets/npm-token }           # or basic: { user: ci, password_file: ... }
      network: private                 # it is on the operator's network: allowed past the public-only guard
  public: true                         # the public registries; false: only the configured ones
```

- Credentials are files, as every secret (`config.Secret`, redacted). A registry's credentials are sent only to its own `url`
  (scheme, host, port and path prefix), never to a registry a build file named, never across a redirect to another origin.
- A registry named by a build file and not configured is read without credentials, through the guarded transport (https to public
  addresses only, `internal/egress`, moved there from `docs`), so a repository cannot make the portal reach its network. A
  configured registry with `network: private` may be on the operator's network.
- `config check` validates every registry: an ecosystem with a provider, https (http only with `network: private`), one auth kind.

## Fetching: structured concurrency

One `packages` job per repository (TTL 6 h, `EventPush`, `EventTag`). Inside it the lookups run as structured concurrency: an
`errgroup` per job whose goroutines all end with it (the job's deadline cancels them), one goroutine per coordinate, at most 8. The
registries are shared by every job through one client: a limit of requests in flight per registry host (4), conditional requests
(ETag and Last-Modified, kept by the client's cache), answers bounded (1 MiB; a larger one is streamed and read in part, P-47), a
timeout per request (10 s), and a pause per host after a 429 or 503 (its `Retry-After`, else doubling from 1 min to 15 min), so one
slow or failing registry never holds the others or the GitHub jobs. Immutable answers (a released parent POM) are kept by their
coordinates in the job's payload.

## What the pages show

- The repository summary gains `packages` (ecosystem, name, version, published, url, registry). The release chip shows the GitHub
  release when there is one, else the newest package: `3.0.21 · Maven`, linking to the package at its registry; the hint lists every
  package.
- The repository page gets a Packages panel: each coordinate with its newest version, registry and build file.
- A product card's "N of M released" and the health signal `no-release` count a published package as released.
- `/llms.txt`, the Markdown answers and `knowledge.yaml` name the packages.

## Access and security

- A private repository's packages are part of the private view only, like everything of it; the leak test covers the new fields.
- A registry's answers are untrusted: versions and names through `untrusted.Text`, URLs https only (else no link), dates bounded.
- Parsers are bounded like the manifests': size caps, depth and token caps; TOML through `pelletier/go-toml/v2` with a size cap.
- No credential reaches a browser, a log or a page; a failed lookup names the registry and the HTTP status only.

## Milestones

| M | Delivers | Done when |
|---|---|---|
| R1 | The feature and its client: `Provider`, the job with structured concurrency, the shared registry client (limits per host, cache, pauses, the guard), `packages.registries` without auth; `maven` (POM parent chain, `distributionManagement`, `maven-metadata.xml`) and `npm`; the summary field, the release chip and the Packages panel; a fake registry server for the tests | a repository whose POM inherits its distribution from a parent shows the release from that repository; an npm package shows its `latest`; a registry that hangs holds no other lookup; the leak test passes |
| R2 | `pypi`, `cargo`, `go` (proxy, and tags for a private module), `swift` (tags, CocoaPods); Gradle coordinates for `maven`; the manifests job reads `Cargo.toml` and `pyproject.toml` for technology and dependencies | each provider passes recorded answers of its public registry; a repository of each language in the e2e catalogue shows its package |
| R3 | Private registries: `auth` (Basic, bearer, header), `network: private`, credentials only to their origin; GitHub Packages for Maven and npm; the operator guide and the Guide's section on private packages | a fake private registry answers only with its credentials; a build file naming another host never receives them; `config check` refuses a broken registry |
| R4 | `nuget`, `rubygems`, `packagist` | as R2 |

## Tests

- Providers: coordinates from build files (table tests), registry answers from recorded fixtures (`testdata/`), version order.
- Client: per-host limit, pause after 429, conditional requests, the guard refusing a private address, credentials never sent to
  another origin (a fake second host records any `Authorization` it receives).
- Job: `testing/synctest`, a registry that hangs, one that fails, partial results kept.
- `cmd/scp`: the end-to-end catalogue gains packages through a loopback fake registry; the leak test.
