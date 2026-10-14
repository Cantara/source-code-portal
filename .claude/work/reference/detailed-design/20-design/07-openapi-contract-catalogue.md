# 20-design / 07 - Endpoint catalogue of the `/api/v1` contract

Status: companion of `07-openapi-contract.md` (design for implementation). Date: 2026-10-14. Unit key `07-openapi-contract`.

This catalogue is generated from `api/openapi-public.yaml`, `api/openapi-admin.yaml` and `api/common.yaml` (the sources of truth; if they disagree with this file, they win). Sections 1, 2, 4 and 5 come from `apicheck -catalogue -owners` and `tools/catalogue.py` of the design scratch area; in the repository the same output is produced by `apicheck catalogue` into `docs/reference/api.md` and gated by a drift check (design task T-12). Section 3 is prose that the documents cannot carry. The error catalogue, the freshness envelope, the SSE event schema and the conventions are in the main document, section 7 and section 4.

Reading guide: "Owner" is the Go package that implements the operation (architecture 4.4). "Route" is the name in `routes.Table` (architecture 8.13). "ETag" is the derivation named by `x-scp-etag` (main document 4.5): `snapshot-variant` = `W/"g<epoch>-<snapshot version>-<view class>[.<digest>]"`, `repo-rev` = `W/"r<epoch>-<repository revision>-<view class>[.<digest>]"`, `snapshot-version` = `W/"v<epoch>-<version>"` (the epoch is a random token per process start; the whole tag is opaque to clients), `tree-sha` = `W/"t<epoch>-<tree sha>-<view class>"`, `body-hash` = hash of the body bytes. Every operation also answers `HEAD` like the `GET` without a body, and every GET declares `If-None-Match`, a `200` with `ETag`, a `304` and the shared `default` problem response (rule A9), except the event stream.

## 1. Operations (24: 22 public, 2 admin only)

| Method and path | operationId | Tag | Owner | Route name | ETag | Statuses |
|---|---|---|---|---|---|---|
| `GET /api/v1/commits` | `listCommits` | commits | commits | `api.commits` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/components` | `getComponent` | manifests | manifests | `api.manifests.component` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/components/dependents` | `listComponentDependents` | manifests | manifests | `api.manifests.dependents` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/dependencies/graph` | `getDependencyGraph` | manifests | manifests | `api.manifests.graph` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/dependencies/licenses` | `getDependencyLicenses` | manifests | manifests | `api.manifests.licenses` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/dependencies/versions` | `getDependencyVersions` | manifests | manifests | `api.manifests.versions` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/events` | `streamEvents` | live | live | `api.events` |  | 200 404 429 default |
| `GET /api/v1/groups` | `listGroups` | catalog | catalog | `api.groups` | snapshot-variant | 200 304 400 429 503 default |
| `GET /api/v1/groups/{groupId}` | `getGroup` | catalog | catalog | `api.group` | snapshot-variant | 200 304 404 429 503 default |
| `GET /api/v1/groups/{groupId}/repos` | `listGroupRepos` | overview | overview | `api.group.repos` | snapshot-variant | 200 304 400 404 429 503 default |
| `GET /api/v1/openapi.yaml` | `getOpenApiDocument` | meta | apispec | `api.openapi` | body-hash | 200 304 429 default |
| `GET /api/v1/pages` | `listPages` | pages | pages | `api.pages` | snapshot-variant | 200 304 400 429 503 default |
| `GET /api/v1/pages/{slug}` | `getPage` | pages | pages | `api.page` | snapshot-variant | 200 304 404 429 503 default |
| `GET /api/v1/repos` | `listRepos` | overview | overview | `api.repos` | snapshot-variant | 200 304 400 429 503 default |
| `GET /api/v1/repos/{repo}` | `getRepo` | overview | overview | `api.repo` | snapshot-variant | 200 304 308 404 429 503 default |
| `GET /api/v1/repos/{repo}/dependencies` | `listRepoDependencies` | manifests | manifests | `api.manifests.deps` | snapshot-variant | 200 304 308 400 404 429 503 default |
| `GET /api/v1/repos/{repo}/docs` | `listRepoDocs` | docs | docs | `api.docs.list` | repo-rev | 200 304 308 400 404 429 503 default |
| `GET /api/v1/repos/{repo}/docs/{docId}` | `getRepoDoc` | docs | docs | `api.docs.get` | repo-rev | 200 304 308 404 429 503 default |
| `GET /api/v1/repos/{repo}/manifests` | `getRepoManifests` | manifests | manifests | `api.manifests.repo` | repo-rev | 200 304 308 404 429 503 default |
| `GET /api/v1/repos/{repo}/sbom.cdx.json` | `getRepoSbom` | manifests | manifests | `api.manifests.sbom` | tree-sha | 200 304 308 404 429 503 default |
| `GET /api/v1/site` | `getSite` | overview | overview | `api.site` | snapshot-variant | 200 304 429 503 default |
| `GET /api/v1/snapshot` | `getSnapshot` | live | live | `api.snapshot` | snapshot-version | 200 304 429 default |
| `GET /api/v1/status` (admin only) | `getStatus` | ops | ops | `api.status` | body-hash | 200 304 429 default |
| `POST /api/v1/admin/jobs/retry` (admin only) | `retryJob` | ops | ops | `api.admin.retry` |  | 202 400 404 429 503 default |

The two rows marked admin only exist in `openapi-admin.yaml` only; the admin document is a superset of the public one (main document, deviation D-1).

## 2. Operation details (parameters, responses, example names)

Parameters not listed (`If-None-Match`, and `cursor` and `limit` where shown) follow the shared definitions in `common.yaml`. `limit` is `Limit` (default 50, maximum 200) on unbounded collections and `LimitBounded` (default and maximum 1000) on collections bounded by configuration or by the catalogue. The `[...]` after a status lists the named examples of that response; a response with one unnamed example shows `[example]`. Query arrays are repeated parameters (`?scope=runtime&scope=dev`).

### `POST /api/v1/admin/jobs/retry` : `retryJob`

Force a job to run now. Tag `ops`, owner `ops`, route `api.admin.retry`, ETag `none`, admin listener only.

Parameters: `X-SCP-Request` (header, required): enum(1); body `application/json`: RetryJobRequest.

Responses: `202` application/json [example]; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/commits` : `listCommits`

Commit log. Tag `commits`, owner `commits`, route `api.commits`, ETag `snapshot-variant`.

Parameters: `repo` (query): Slug; `group` (query): GroupId; `limit` (query): integer minimum=1 maximum=200 default=50; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/components` : `getComponent`

One component by package URL. Tag `manifests`, owner `manifests`, route `api.manifests.component`, ETag `snapshot-variant`.

Parameters: `purl` (query, required): Purl.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/components/dependents` : `listComponentDependents`

Reverse dependencies of a component ("used by"). Tag `manifests`, owner `manifests`, route `api.manifests.dependents`, ETag `snapshot-variant`.

Parameters: `purl` (query, required): Purl; `scope` (query): array<DependencyScope>; `group` (query): GroupId; `limit` (query): integer minimum=1 maximum=200 default=50; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/dependencies/graph` : `getDependencyGraph`

Organisation-wide internal dependency graph. Tag `manifests`, owner `manifests`, route `api.manifests.graph`, ETag `snapshot-variant`.

Parameters: `group` (query): GroupId; `scope` (query): array<DependencyScope>; `ecosystem` (query): array<EcosystemId>; `intra` (query): boolean default=False; `internal` (query): boolean default=True; `focus` (query): string minLength=1 maxLength=420; `depth` (query): integer minimum=1 maximum=3 default=1; `format` (query): enum(json, mermaid).

Responses: `200` application/json, text/plain [example, example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/dependencies/licenses` : `getDependencyLicenses`

Licence summary of dependencies. Tag `manifests`, owner `manifests`, route `api.manifests.licenses`, ETag `snapshot-variant`.

Parameters: `group` (query): GroupId.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/dependencies/versions` : `getDependencyVersions`

Version spread of components across the organisation. Tag `manifests`, owner `manifests`, route `api.manifests.versions`, ETag `snapshot-variant`.

Parameters: `group` (query): GroupId; `ecosystem` (query): array<EcosystemId>; `internal` (query): boolean; `minVersions` (query): integer minimum=1 maximum=100 default=1; `q` (query): string minLength=1 maxLength=200; `limit` (query): integer minimum=1 maximum=200 default=50; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/events` : `streamEvents`

Invalidation event stream (Server-Sent Events). Tag `live`, owner `live`, route `api.events`, ETag `none`, flag `live.sse.enabled`, since 1.1.

Parameters: `Last-Event-ID` (header): string pattern=^[0-9]{1,20}$.

Responses: `200` text/event-stream [hello, invalidate, resync]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/groups` : `listGroups`

Groups (dashboard cards). Tag `catalog`, owner `catalog`, route `api.groups`, ETag `snapshot-variant`.

Parameters: `sort` (query): enum(config, name); `q` (query): string minLength=1 maxLength=200; `limit` (query): integer minimum=1 maximum=1000 default=1000; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/groups/{groupId}` : `getGroup`

One group. Tag `catalog`, owner `catalog`, route `api.group`, ETag `snapshot-variant`.

Parameters: `groupId` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$.

Responses: `200` application/json [example]; `304` no body; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/groups/{groupId}/repos` : `listGroupRepos`

Repositories of a group. Tag `overview`, owner `overview`, route `api.group.repos`, ETag `snapshot-variant`.

Parameters: `groupId` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$; `q` (query): string minLength=1 maxLength=200; `lifecycle` (query): array<Lifecycle>; `type` (query): array<RepoType>; `tag` (query): array<string minLength=1 maxLength=63>; `archived` (query): enum(include, exclude, only); `sort` (query): enum(name, -name, pushedAt, -pushedAt, lastCommit, -lastCommit, openIssues, -openIssues, lifecycle, -lifecycle); `limit` (query): integer minimum=1 maximum=1000 default=1000; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/openapi.yaml` : `getOpenApiDocument`

This contract. Tag `meta`, owner `apispec`, route `api.openapi`, ETag `body-hash`.

Parameters: none (besides `If-None-Match`).

Responses: `200` application/yaml [example]; `304` no body; `429` application/problem+json [rateLimited]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/pages` : `listPages`

Config-driven pages. Tag `pages`, owner `pages`, route `api.pages`, ETag `snapshot-variant`.

Parameters: `limit` (query): integer minimum=1 maximum=1000 default=1000; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/pages/{slug}` : `getPage`

One page. Tag `pages`, owner `pages`, route `api.page`, ETag `snapshot-variant`.

Parameters: `slug` (path, required): PageSlug.

Responses: `200` application/json [example]; `304` no body; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos` : `listRepos`

Repositories. Tag `overview`, owner `overview`, route `api.repos`, ETag `snapshot-variant`.

Parameters: `q` (query): string minLength=1 maxLength=200; `group` (query): array<GroupId>; `lifecycle` (query): array<Lifecycle>; `type` (query): array<RepoType>; `tag` (query): array<string minLength=1 maxLength=63>; `archived` (query): enum(include, exclude, only); `sort` (query): enum(name, -name, pushedAt, -pushedAt, lastCommit, -lastCommit, openIssues, -openIssues, lifecycle, -lifecycle); `limit` (query): integer minimum=1 maximum=1000 default=1000; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `400` application/problem+json [validationFailed, invalidCursor]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}` : `getRepo`

Repository (composite resource). Tag `overview`, owner `overview`, route `api.repo`, ETag `snapshot-variant`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$.

Responses: `200` application/json [example]; `304` no body; `308` application/problem+json [renamed]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}/dependencies` : `listRepoDependencies`

Dependencies of a repository. Tag `manifests`, owner `manifests`, route `api.manifests.deps`, ETag `snapshot-variant`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$; `scope` (query): array<DependencyScope>; `relationship` (query): enum(direct, transitive, all); `internal` (query): boolean; `ecosystem` (query): array<EcosystemId>; `unit` (query): UnitId; `q` (query): string minLength=1 maxLength=200; `sort` (query): enum(name, -name, ecosystem, -ecosystem, scope, -scope); `limit` (query): integer minimum=1 maximum=200 default=50; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `308` application/problem+json [renamed]; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}/docs` : `listRepoDocs`

Documentation index of a repository. Tag `docs`, owner `docs`, route `api.docs.list`, ETag `repo-rev`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$; `q` (query): string minLength=1 maxLength=200; `limit` (query): integer minimum=1 maximum=1000 default=1000; `cursor` (query): string minLength=1 maxLength=512 pattern=^[A-Za-z0-9_-]+$.

Responses: `200` application/json [example]; `304` no body; `308` application/problem+json [renamed]; `400` application/problem+json [validationFailed, invalidCursor]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}/docs/{docId}` : `getRepoDoc`

One rendered document. Tag `docs`, owner `docs`, route `api.docs.get`, ETag `repo-rev`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$; `docId` (path, required): string minLength=1 maxLength=3100 pattern=^[A-Za-z0-9._~%-]+$.

Responses: `200` application/json [example]; `304` no body; `308` application/problem+json [renamed]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}/manifests` : `getRepoManifests`

Technology and package manifests of a repository. Tag `manifests`, owner `manifests`, route `api.manifests.repo`, ETag `repo-rev`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$.

Responses: `200` application/json [example]; `304` no body; `308` application/problem+json [renamed]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/repos/{repo}/sbom.cdx.json` : `getRepoSbom`

CycloneDX SBOM of a repository. Tag `manifests`, owner `manifests`, route `api.manifests.sbom`, ETag `tree-sha`.

Parameters: `repo` (path, required): string minLength=1 maxLength=300 pattern=^[A-Za-z0-9._~%-]+$.

Responses: `200` application/vnd.cyclonedx+json [example]; `304` no body; `308` application/problem+json [renamed]; `404` application/problem+json [notFound, featureDisabled]; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/site` : `getSite`

Site settings and header status. Tag `overview`, owner `overview`, route `api.site`, ETag `snapshot-variant`.

Parameters: none (besides `If-None-Match`).

Responses: `200` application/json [example]; `304` no body; `429` application/problem+json [rateLimited]; `503` application/problem+json [notReady, queueFull]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/snapshot` : `getSnapshot`

Snapshot version for live updates. Tag `live`, owner `live`, route `api.snapshot`, ETag `snapshot-version`.

Parameters: none (besides `If-None-Match`).

Responses: `200` application/json [example]; `304` no body; `429` application/problem+json [rateLimited]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

### `GET /api/v1/status` : `getStatus`

Operator status. Tag `ops`, owner `ops`, route `api.status`, ETag `body-hash`, admin listener only.

Parameters: none (besides `If-None-Match`).

Responses: `200` application/json [example]; `304` no body; `429` application/problem+json [rateLimited]; `default` application/problem+json [internal, methodNotAllowed, forbidden].

## 3. Notes that the documents cannot carry

* **Why `repos` and `groups/{groupId}/repos` are owned by `overview` and not `catalog`.** Architecture 11.2 lists `api.repos` and `api.group.repos` under `catalog`. A repository summary carries stats, badges, tags and the lifecycle with provenance, which are `overview` concerns (descriptor resolution, architecture 14.2), and `catalog` may not import `overviewapi` (rule G7). The two list operations moved to `overview`, whose schema `RepoSummary` is the element of both lists. Deviation D-2 in the main document.
* **Why `docId` is one encoded segment.** `GET /api/v1/repos/{repo}/docs/{id...}` of architecture 11.2 cannot be expressed in OpenAPI path templating and kin-openapi does not route a `{id...}` template. `docId` is `readme` or the repository-relative path with every `/` replaced by `~`, and every byte outside `[A-Za-z0-9._-]` of the path (a literal `~` and `%` included) written as `%XX` with upper-case hex, so the encoding is injective: two paths never share an id (`docs/Release Notes.md` is `docs~Release%20Notes.md`). The path parameter schema is the wire form (the `%` of the id percent-encoded once more) because kin-openapi validates the encoded path. A path whose id would exceed 1024 characters is not indexed (diagnostic `doc.id-too-long`). The raw-Markdown route `GET /repos/{repo}/docs/{path...}` (server-only) keeps the real path. Deviation D-3.
* **Why `{repo}` and `{groupId}` are wire-form strings here.** The generated parameter schema is `minLength 1, maxLength 300, pattern ^[A-Za-z0-9._~%-]+$`: the validator sees the percent-encoded segment, the handler receives the decoded value and parses it with `scm.ParseSlug` / `scm.ParseGroupID` (the grammar of `Slug` and `GroupId`, which stay the schemas of response members). A leading `.` is legal in a slug (`.github`), `.` and `..` never.
* **Why `purl` is a query parameter.** A package URL contains `/`, `:`, `@` and `%`, and `ServeMux` hands handlers unescaped path values (architecture 11.2).
* **Why the dependency graph has a `format` parameter.** `GET /api/v1/dependencies/graph?format=mermaid` returns `text/plain` (a Mermaid flowchart, bounded by `focus` and `depth`) for agents and for the v1.1 diagram view; the JSON form is the default.
* **`getRepoSbom`.** Returns CycloneDX 1.7 JSON as `application/vnd.cyclonedx+json`; the `ETag` is derived from the epoch and the repository tree sha, so within one process it changes only when the tree changes.
* **`streamEvents` is the only operation without conditional GET** (`x-scp-conditional: false`). It answers `404 feature-disabled` unless `live.sse.enabled` is true.

## 4. Schema ownership (generated package of each schema)

A schema lives in exactly one place: in `common.yaml` (generated package `httpapi/openapi`) when operations of more than one feature tag reach it, otherwise in the fragment of the feature that owns it (rule A18; architecture rule G7 forbids a feature importing another feature's generated package).

| Local schema (owner feature) | Feature |
|---|---|
| `Announcement` | overview |
| `ApiRef` | overview |
| `Artifact` | manifests |
| `CommitPage` | commits |
| `Component` | manifests |
| `ComponentSpread` | manifests |
| `Confidence` | manifests |
| `Dependency` | manifests |
| `DependencyGraph` | manifests |
| `DependencyList` | manifests |
| `DependencyScope` | manifests |
| `Dependent` | manifests |
| `DependentList` | manifests |
| `DependsOn` | overview |
| `Diagnostic` | manifests |
| `Doc` | docs |
| `DocIndex` | docs |
| `DocLink` | docs |
| `DocProblem` | docs |
| `EcosystemCount` | manifests |
| `EcosystemId` | manifests |
| `GraphEdge` | manifests |
| `GraphNode` | manifests |
| `GraphSummary` | manifests |
| `GroupDetail` | catalog |
| `GroupList` | catalog |
| `GroupRef` | overview |
| `GroupSummary` | catalog |
| `InternalRef` | manifests |
| `InvalidationTopic` | live |
| `License` | overview |
| `LicenseBucket` | manifests |
| `LicenseCoverage` | manifests |
| `LicenseSummary` | manifests |
| `Lifecycle` | overview |
| `Link` | overview |
| `Location` | manifests |
| `ManifestFile` | manifests |
| `ManifestUnit` | manifests |
| `NavItem` | overview |
| `Owner` | overview |
| `Page` | pages |
| `PageList` | pages |
| `PageSummary` | pages |
| `Producer` | manifests |
| `ProviderStatus` | overview |
| `Purl` | manifests |
| `Release` | overview |
| `Repo` | overview |
| `RepoLink` | catalog |
| `RepoList` | overview |
| `RepoManifests` | manifests |
| `RepoSections` | overview |
| `RepoStats` | overview |
| `RepoSummary` | overview |
| `RepoType` | overview |
| `Resolution` | manifests |
| `Site` | overview |
| `SiteFeatures` | overview |
| `Snapshot` | live |
| `SourcedLifecycle` | overview |
| `SourcedRepoType` | overview |
| `SourcedString` | overview |
| `SseEvent` | live |
| `SseHello` | live |
| `SseInvalidate` | live |
| `SseResync` | live |
| `Tag` | overview |
| `TechnologySummary` | overview |
| `UnitCounts` | manifests |
| `UnitId` | manifests |
| `VersionCount` | manifests |
| `VersionSpreadList` | manifests |
| `VersionStatus` | manifests |
| `Visibility` | overview |

| Common schema (reached directly by operations) | Features |
|---|---|
| `Badge` | catalog, overview |
| `BadgeKind` | catalog, overview |
| `BuildInfo` | overview |
| `Commit` | commits, overview |
| `DescriptorInfo` | docs, overview |
| `DescriptorProblem` | docs, overview |
| `DocFormat` | docs, overview |
| `DocId` | docs, overview |
| `DocIndexEntry` | docs, overview |
| `DocState` | docs, overview |
| `FieldError` | catalog, commits, docs, live, manifests, meta, overview, pages |
| `Freshness` | catalog, commits, docs, manifests, overview, pages |
| `FreshnessStatus` | catalog, commits, docs, manifests, overview, pages |
| `GroupId` | catalog, commits, manifests, overview |
| `HttpsUrl` | catalog, commits, docs, overview |
| `LanguageShare` | manifests, overview |
| `NullableTimestamp` | catalog, commits, docs, manifests, overview, pages |
| `PageInfo` | catalog, commits, docs, manifests, overview, pages |
| `PageSlug` | overview, pages |
| `PortalPath` | catalog, docs, manifests, overview, pages |
| `Problem` | catalog, commits, docs, live, manifests, meta, overview, pages |
| `Provenance` | manifests, overview |
| `ProviderState` | overview |
| `Sha` | commits, docs, manifests, overview |
| `Slug` | catalog, commits, docs, manifests, overview |
| `Timestamp` | commits, live, overview |
| `TocEntry` | docs, pages |

## 5. Open enumerations

Every enumeration is open: new values are additive inside v1 and clients must tolerate an unknown value (the TypeScript union is closed, so the SPA switches with a default branch).

| Enum | Document | Values |
|---|---|---|
| `BadgeKind` | common | `issues`, `last-commit`, `release`, `licence`, `repos`, `language`, `build-tool`, `latest-version`, `internal-deps`, `build`, `security` |
| `DocFormat` | common | `markdown`, `asciidoc`, `text` |
| `DocState` | common | `resident`, `not_resident`, `too_large`, `unsupported`, `no_content` |
| `FreshnessStatus` | common | `fresh`, `stale`, `error`, `unknown` |
| `Provenance` | common | `descriptor`, `catalog-info`, `codeowners`, `github-topics`, `github`, `config`, `derived`, `own`, `github-graph`, `sbom` |
| `ProviderState` | common | `ok`, `failing`, `unstable`, `unknown`, `disabled` |
| `Confidence` | public | `exact`, `heuristic` |
| `DependencyScope` | public | `runtime`, `dev`, `test`, `build`, `optional`, `peer`, `provided`, `unknown` |
| `InvalidationTopic` | public | `site`, `catalog`, `commits`, `docs`, `manifests`, `pages`, `status` |
| `Lifecycle` | public | `experimental`, `production`, `deprecated` |
| `RepoType` | public | `service`, `library`, `website`, `tool`, `documentation`, `other` |
| `Resolution` | public | `declared`, `pinned`, `locked`, `managed` |
| `VersionStatus` | public | `current`, `behind`, `ahead`, `unknown` |
| `Visibility` | public | `public`, `internal`, `private` |

## 6. Server-only routes (not part of the OpenAPI documents)

These routes never fall back to the SPA and are not under `/api/`; they are declared in `x-scp-server-routes` of the public document so that agents can discover them.

| Route | Owner | Content type |
|---|---|---|
| `GET /badges/{repo}/{file}` (`file` = kind + `.svg`) | badges | `image/svg+xml` |
| `GET /badges/groups/{groupId}/{file}` | badges | `image/svg+xml` |
| `GET /media/{repo}/{ref}/{path...}`, `GET /media/ext/{sig}/{id}` | media | image types (never SVG for `ext`) |
| `GET /llms.txt`, `/llms-full.txt`, `/groups/{groupId}/llms.txt`, `/repos/{repo}/llms.txt` | overview | `text/plain; charset=utf-8` |
| `GET /repos/{repo}/readme.md`, `/repos/{repo}/docs/{path...}` | docs | `text/markdown; charset=utf-8` |
| `GET /repos/{repo}/dependencies.md` | manifests | `text/markdown; charset=utf-8` |
| `POST /webhooks/github[/{source}]` | webhook | none (`202`) |
| `GET /livez`, `GET /readyz` | ops | JSON probe documents |
| `GET /sitemap.xml`, `GET /robots.txt`, `GET /schemas/scp/{file}` | overview, docs | XML, text, JSON Schema |

