# 20-design / 07 - The OpenAPI contract (`api/`, `internal/apispec`, `internal/httpapi/openapi`, the per-feature `<feature>api` fragments, `tools/apicheck`)

Status: design for implementation, independently verified and corrected on 2026-10-14 (Verification log at the end; findings report `30-verification/design-07-openapi-contract.md`). Date: 2026-10-14. Unit key `07-openapi-contract`.
Conforms to `04-decisions.md` (D-16, D-12, D-15, A-12, A-16, A-25), `10-architecture.md` (sections 4, 5.2, 6, 8.13, 8.14, 9.6, 11, 12.3, 15, 17, 18, 20-23) and ADR-009, ADR-014, ADR-020, ADR-023, ADR-024, ADR-025.

This document is self-sufficient: an engineer who has never seen this conversation can create the contract, the generated packages, the checks and the CI gates from it. The contract itself is not prose: it is three machine-checked files next to this document, `20-design/api/common.yaml`, `api/openapi-public.yaml` and `api/openapi-admin.yaml`, plus a human catalogue, `20-design/07-openapi-contract-catalogue.md` (all 24 operations, parameters, responses, example names, schema ownership, enumerations). In the new repository these files become the fragments of ADR-020 mechanically (a throw-away splitter, section 12.6, proves that the single-file design documents are exactly the repository layout, concatenated).

Conventions of this document: "MUST/MUST NOT" are binding for the implementation; "design choice" marks a decision taken here where the architecture was silent; "Deviation Dn" points at section 14; "Question Qn" at section 15; "Rule An" at the `apicheck` rule list in section 12.2. Facts about libraries carry the date they were verified (all 2026-10-14) and the evidence is in section 12.6 and Appendix B.

---

## 1. Purpose, scope, non-goals

### 1.1 Purpose

D-16 makes the JSON API the single interface between the Go backend and the React SPA, and the structured surface for scripts and LLM agents. ADR-020 makes OpenAPI 3.1 the source of truth for it: handlers are generated as strict servers from the document, requests are validated against it in production, the TypeScript client is generated from it, and a custom check keeps every GET conditional. This unit designs that contract:

1. the wire vocabulary (naming, time, null versus absent, envelopes, freshness, pagination, filtering, sorting, conditional GET, errors, provenance, versioning), written once so that eight feature authors do not invent eight dialects (an API without one contract drifts into several JSON encoders, ad hoc shapes, soft 404s and wildcard CORS; GR-11, GR-16, GR-18);
2. every operation of `/api/v1` with its parameters, responses and examples (24 operations: 22 public, 2 admin only);
3. the files, generation steps and checks that keep documents, generated Go, the TypeScript client, the route table and the handlers equal;
4. the interfaces a feature consumes to implement an operation correctly (section 2.3), compile-checked.

### 1.2 In scope

| Area | In this unit |
|---|---|
| Documents | `api/common.yaml` (shared vocabulary), `api/openapi-public.yaml`, `api/openapi-admin.yaml`, the split into fragments, the generator configurations, the two embedded bundles (`internal/apispec`) |
| Vocabulary | shared schemas, parameters, headers and responses; the error catalogue; the freshness envelope; the SSE event schema; provenance; extensions `x-scp-*` |
| Conventions | naming, time, null, pagination, filtering, sorting, field selection (none), conditional GET and ETag derivation, HEAD, renamed repositories, visibility classes, versioning and deprecation policy |
| Checks | `tools/apicheck` (27 rules, section 12.2; `apicheck aliases` generates the alias file of every feature api package), the contract mock, Schemathesis and oasdiff invocations, the drift gates |
| Go surface owned | package `apispec` (two functions), the generated package `httpapi/openapi`, the generation recipe of every `<feature>api` including its `common_aliases.go`, the renamed-repository redirect (`overview`, section 4.7); specified for the HTTP platform unit (06) to add: `AggregateFreshness`, `NonNil` and the `If-None-Match` header fold (section 2.3.3, deviation D-5). Everything else a feature needs from the contract already exists in unit 06's `httpapi` |
| Handler test helper | the behaviour of `httpapi/apitest` (response validation against the contract), section 12.3 |

### 1.3 Non-goals

- No handler logic. Each feature design owns the semantics behind its operations (what a commit page contains, how a composite is built); this unit owns only the shape and the rules every operation obeys. The schemas do describe the shape of every field, so a feature author can implement from the document without a second source.
- No frontend code. The generated `schema.d.ts` and `openapi-fetch` usage are specified only to the extent needed to prove the contract is consumable (section 12.6); `web/` is another unit.
- No server-only routes (badges, media, llms, raw Markdown, webhooks, probes, sitemap, robots, schemas). They are not under `/api/` and are not JSON resources; they are listed in `x-scp-server-routes` so that agents can discover them (catalogue section 6).
- No authentication scheme. Public data needs none; identity arrives through the trusted-proxy `access` mode (unit 14) and is invisible to the contract except through the view class (section 4.8). `security: []` in both documents.
- No GraphQL, no JSON:API, no JSON-RPC, no query language, no sparse fieldsets (section 4.3 explains why).
- No v2. The policy for evolving inside v1 and for retiring an operation is specified (section 4.9); nothing is deprecated at v1.0.
- No SDK for languages other than TypeScript. The bundled documents are served at `/api/v1/openapi.yaml`, so any consumer can generate one.

### 1.4 Directories and files (exact)

Design documents (this package, `20-design/`), the sources the implementation starts from:

```
20-design/api/common.yaml            436 lines   shared schemas (27), parameters (7), headers (7), responses (8)
20-design/api/openapi-public.yaml   2012 lines   22 operations, 75 local schemas, 6 local parameters, x-scp-fixtures
20-design/api/openapi-admin.yaml     356 lines   superset: 22 public path items by $ref + getStatus + retryJob, 21 local schemas
20-design/api/trusted-members.txt     54 entries  free-text members written by the server or the operator, not by a repository (rule A24)
20-design/07-openapi-contract-catalogue.md       generated catalogue (operations, parameters, schema ownership, enums)
```

Implementation repository (ADR-020 layout; `tools/split.py` of the scratch area produced exactly this from the files above, section 12.6):

```
api/
  openapi.yaml                       public root: info, tags, servers, x-scp-*, paths as $ref to fragment path items
  openapi.admin.yaml                 admin root: the same plus the ops fragment
internal/httpapi/openapi/
  common.yaml                        = api/common.yaml (rank 0 generated package "openapi")
  oapi.yaml                          oapi-codegen config: package openapi, models + std-http-server + strict-server, skip-prune
  common.gen.go                      generated, committed (782 lines)
internal/<feature>/<feature>api/     feature in catalog commits docs live manifests overview pages ops
  <feature>.yaml                     hand-written fragment: that feature's paths and local schemas/parameters
  oapi.yaml                          config: package <feature>api, import-mapping of common.yaml to httpapi/openapi, always-prefix-enum-values
  <feature>.gen.go                   generated, committed
  common_aliases.go                  generated by `apicheck aliases`, committed (78 lines): type and constant aliases of the common types the feature names
internal/apispec/
  openapi.bundled.yaml               redocly bundle of api/openapi.yaml (committed; embedded)
  openapi.admin.bundled.yaml         redocly bundle of api/openapi.admin.yaml
  apispec.go                         //go:embed of the two files; Public() []byte, Admin() []byte
tools/apicheck/                      nested module tools/: the contract linter (section 12.2), the catalogue generator, the alias generator
tools/mock/                          the contract mock (section 12.5)
api/trusted-members.txt              the server-authored free-text members (rule A24)
web/src/lib/api/schema.d.ts          generated from the ADMIN bundle (one schema for the SPA and /status), committed
```

`badges`, `integrations`, `media`, `webhook` and `access` own no fragment (server-only routes or no JSON surface); `ops` owns the admin-only fragment. Generated line counts (oapi-codegen v2.8.0, 2026-10-14): catalog 886, commits 493, docs 910, live 745, manifests 3,488, ops 1,126, overview 2,154, pages 714, common 782: 11,298 lines, plus eight alias files of 78 lines, all committed (ADR-020 amendment of D-14) and covered by a drift check (section 12.4).

**The alias files exist because of architecture rule 5.2.** A feature (rank 4 or 5) may import its own `<feature>api` (rank 1) and `httpapi`, but not `httpapi/openapi`; yet the generated strict code of every feature names common types (`Problem`, `Freshness`, `NotModifiedResponseHeaders`, `Commit`, the enum constants). `apicheck aliases` writes `type X = openapi.X` and `const C = openapi.C` for every exported common name into the feature's own package, so a feature writes `liveapi.Freshness` and never imports `httpapi/openapi`. The file is generated from `common.gen.go` and gated by the same drift check; the stage-2 proof (12.6) compiles `internal/live` and `internal/overview` against it with no import of `httpapi/openapi`. `always-prefix-enum-values` is set in every `oapi.yaml`: without it a constant is named by its bare value (`Critical`, `Dark`, `Public` in `overviewapi`) and is renamed with a type prefix as soon as another enum of the package declares the same word, which would break handler code on an additive change; with it the names are `AnnouncementLevelCritical`, `SiteDefaultThemeDark`, `VisibilityPublic` (verified by generating both ways).

### 1.5 Vocabulary

| Term | Meaning |
|---|---|
| Public document | `openapi-public.yaml`: what the public listener `:9090` serves (22 operations). |
| Wire form | the percent-encoded text of a path segment as it arrives on the wire. kin-openapi validates path parameters in this form (its router uses the encoded path); the handler receives the decoded value. The path parameter schemas of `{repo}`, `{groupId}` and `{docId}` are therefore wire-form schemas (section 3.3). |
| Epoch | a random token chosen at process start; entity tags and `Snapshot.version` consumers compare it, because the snapshot version restarts at 1 after a warm start (section 4.5). |
| Admin document | `openapi-admin.yaml`: what the admin listener `:9091` serves (24 operations, a superset, D-1). |
| Fragment | the per-feature OpenAPI file (`<feature>api/<feature>.yaml`); in the design documents a fragment is a slice of the single files, selected by tag. |
| Common | `common.yaml`: the schemas, parameters, headers and responses referenced by more than one feature tag. |
| View class | `scm.ViewClass`, `public` or `all`: which variant of a global projection a principal may read (architecture 17.3). Wire ordinal `0` public, `1` all. |
| Envelope | `{items, page, freshness}` of a collection response. |
| Freshness | the derived age and health of the cached upstream data a response is built from (GR-26, GR-40). |
| Strict handler | the oapi-codegen `StrictServerInterface` method: typed request object in, typed response object out. |
| Problem | an RFC 9457 `application/problem+json` document with the portal's members (section 7.5). |

---

## 2. Position in the architecture

### 2.1 Packages and ranks

| Package | Rank | May import (internal) | Notable third-party | Owner of |
|---|---|---|---|---|
| `internal/apispec` | 0 | none | none (`embed`) | the two bundled documents and two accessors |
| `internal/httpapi/openapi` (generated) | 0 | none | `oapi-codegen/runtime` | the common wire types, parameters, headers and response objects of `common.yaml` (no operations) |
| `internal/<feature>/<feature>api` (generated) | 1 | `httpapi/openapi` | `oapi-codegen/runtime` | one feature's request/response objects, strict server and `common_aliases.go` |
| `tools/apicheck`, `tools/mock` | tools module | none of the product | `getkin/kin-openapi`, `yaml` | the linter, catalogue generator, alias generator and contract mock |

These rows are exactly those of architecture 5.2; this unit adds no package. The contract is a source of generated rank-0/1 packages: nothing else of the product depends on the YAML at run time except `httpapi/validate` (rank 3, imported by `cmd/scp` only), which receives `apispec.Public()` or `Admin()` as bytes, and `httpapi/apitest` (rank 3, test only).

Imports forbidden by construction (checked by archtest, ADR-018): a feature may import its own `<feature>api` and nothing else generated (G7); no feature imports `httpapi/openapi` directly (the matrix of 5.2 gives it to rank 1 and to `httpapi` only). The types a feature needs from `common.yaml` come through the alias file of its own `<feature>api` (section 1.4), and the helpers that produce them (`NewFreshness`, `AggregateFreshness`, `WriteProblem`) live in `httpapi`. kin-openapi is linked only by `httpapi/validate`, `httpapi/apitest` and `tools/apicheck`.

### 2.2 Who imports it

| Importer | What it imports | Why |
|---|---|---|
| each API-owning feature root (`catalog`, `commits`, `docs`, `manifests`, `pages`, `overview`, `live`, `ops`) | its own `<feature>api` | implement `StrictServerInterface`, call `HandlerWithOptions` from `Mount` |
| `cmd/scp` | every `<feature>api` is reached only through the features' `Mount`; `apispec`; `httpapi/validate`; `overview` (the alias redirect) | choose the public or admin bundle per listener, build the request validator, serve `/api/v1/openapi.yaml` through `httpapi.Static`, mount the redirect of 4.7 |
| `httpapi` | `httpapi/openapi` | `Freshness` and `PageInfo` wire types behind `NewFreshness`, `AggregateFreshness` and `NewPageInfo` |
| `httpapi/validate` | `httpapi`, `httpapi/openapi` | render validator errors as problems |
| `httpapi/apitest` | `apispec` | validate every handler-test response against the contract |
| `web/` build | the bundled admin document | `openapi-typescript` |
| CI | `tools/apicheck`, redocly, oasdiff, Schemathesis | section 12 |

### 2.3 Consumer-facing interfaces (exact Go signatures)

Every signature in this section was compile-checked on Go 1.27.1 (darwin/arm64) on 2026-10-14 against code generated by oapi-codegen v2.8.0 from the documents next to this one **and against the real unit 06 prototype** (`06-httpapi.assets`, stage 2 of `tools/prove.sh`); `go build`, `go vet` and `go test -race` pass over the whole tree (section 12.6). The names of unit 06's `httpapi` are authoritative; this unit adds only what Appendix A shows.

#### 2.3.1 `apispec` (owned here)

```go
package apispec // rank 0; //go:embed openapi.bundled.yaml, //go:embed openapi.admin.bundled.yaml

func Public() []byte // the bundled public document; callers must not modify the slice
func Admin() []byte  // the bundled admin document (a superset of the public one)
```

#### 2.3.2 What oapi-codegen v2.8.0 generates per feature (verified on `liveapi`, 2026-10-14)

A feature never writes these; it implements them. Names are the generator's, derived from `operationId`.

```go
package liveapi
type ServeMux interface { HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)); http.Handler }
type StdHTTPServerOptions struct {
    BaseURL string; BaseRouter ServeMux; Middlewares []MiddlewareFunc
    ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}
func HandlerFromMux(si ServerInterface, m ServeMux) http.Handler
func HandlerWithOptions(si ServerInterface, options StdHTTPServerOptions) http.Handler

type StrictServerInterface interface {
    StreamEvents(ctx context.Context, request StreamEventsRequestObject) (StreamEventsResponseObject, error)
    GetSnapshot(ctx context.Context, request GetSnapshotRequestObject) (GetSnapshotResponseObject, error)
}
type GetSnapshotRequestObject struct{ Params GetSnapshotParams }             // Params.IfNoneMatch *openapi.IfNoneMatch
type GetSnapshotResponseObject interface{ VisitGetSnapshotResponse(w http.ResponseWriter) error }
type GetSnapshot200JSONResponse struct{ Body Snapshot; Headers GetSnapshot200ResponseHeaders } // Headers.ETag *string
type GetSnapshot304Response openapi.NotModifiedResponse                                         // Headers.ETag *string
type GetSnapshot429ApplicationProblemPlusJSONResponse struct{ openapi.RateLimitedApplicationProblemPlusJSONResponse }
type GetSnapshotdefaultApplicationProblemPlusJSONResponse struct{ Body openapi.Problem; Headers openapi.ProblemResponseHeaders; StatusCode int } // Headers: Allow, Retry-After, X-Request-Id (pointers; nil is not written)

type StrictHTTPServerOptions struct {
    RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
    ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}
func NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface
```

Facts that shape the design, each verified by compiling and running the code of Appendix A:

1. `ServeMux` is exactly architecture 8.14's `httpapi.Mux`, so `*http.ServeMux` and `*routes.Recorder` both satisfy it.
2. The generated registration is `GET /api/v1/...`; `ServeMux` therefore also routes `HEAD` to the same handler (verified: a `HEAD` through the generated handler answers 200 with the `ETag`). The request validator does not (section 7.2, D-6).
3. A response object is any value with the operation's `Visit<Op>Response(w)` method. The generated types cover the declared statuses; a hand-written type with the same method is accepted by the strict handler, which is how SSE streams (section 7.6) without the generated `io.Reader` copy loop.
4. The generated defaults for request and response errors call `http.Error` (`text/plain`), which would break the problem contract. Every feature MUST pass the two hooks of `httpapi.StrictErrors(log)` in `StrictHTTPServerOptions` and `httpapi.BindingError` as `StdHTTPServerOptions.ErrorHandlerFunc` (the binder's errors are per-package types, so the hook takes `error`).
5. The strict request object carries typed parameters (`Params.IfNoneMatch`) but no `*http.Request`. Unit 06's `WithRequest` (mounted innermost, directly around the API mux) stores it, and the handler calls `httpapi.NotModified(httpapi.RequestFrom(ctx), etag)`, which is architecture 8.14's function unchanged (an earlier draft of this unit asked for a typed `IfNoneMatch(header, etag)`; unit 06 made that unnecessary and D-5 no longer lists it). `RequestFrom` is nil when `WithRequest` is not mounted, so a handler test mounts it (`apitest` does).
6. Response headers declared in the document become typed fields. A header member is a pointer (`Headers.ETag *string`), and a nil pointer writes nothing, so declaring a header that middleware also sets cannot overwrite the middleware's value; what is declared is what the document promises. Declared: `ETag`, `Link`, `Retry-After`, `Location`, and on the shared `Problem` response `Allow`, `Retry-After`, `X-Request-Id` (7.4). Not declared per response, because they are unconditional and set by middleware: `X-SCP-Build`, `Cache-Control`, the security headers.
7. The generated header binder accepts **exactly one** header line: `TooManyValuesForParamError` when a client sends `If-None-Match` twice, which RFC 9110 5.3 allows (a list may be split over lines) and which proxies and some HTTP libraries do. The binder's error handler answers 400, so a legitimate conditional request would fail. `httpapi.FoldIfNoneMatch` (first step of `WithRequest`) joins repeated lines into one before the binder runs (verified: 400 without the fold, 304 with it, `TestRepeatedIfNoneMatchLines`). kin-openapi's validator tolerates repeated lines (verified against the mock), so the fold may sit innermost.
8. Enum constants are generated with `always-prefix-enum-values` (`FreshnessStatusStale`, `SnapshotReadinessReady`); without it the constant name is the bare value and changes when another enum declares the same word (section 1.4).

#### 2.3.3 What this contract uses of `httpapi`, and the three additions it asks for (D-4, D-5)

```go
package httpapi // rank 2: imports routes, logx, httpapi/openapi. Unit 06 (06-httpapi.md 2.2) is authoritative for every name below.

// already in unit 06 and used by this contract unchanged
func WithRequest(next http.Handler) http.Handler; func RequestFrom(ctx context.Context) *http.Request  // the *http.Request inside a strict handler
func NotModified(r *http.Request, etag string) bool                      // GET/HEAD, weak comparison, "*" matches
func WeakETagOf(kind ETagKind, parts ...any) string                      // ETagSnapshot 'g', ETagRepo 'r', ETagVersion 'v'; parts joined with "-"
func FreshnessDigest(statuses ...string) string                          // "" while every status is "fresh", else Digest(...) (16 hex chars, 64-bit FNV-1a)
func WithDigest(etag, digest string) string                              // W/"g...-all" + digest -> W/"g...-all.<digest>"
type FreshnessView struct{ Status, Kind string; AsOf, FetchedAt, CheckedAt, StaleSince time.Time; LastErrorCode string } // zero time is null
func NewFreshness(v FreshnessView) openapi.Freshness
func NewPageInfo(next string, limit int) openapi.PageInfo; func NextLink(r *http.Request, cursor string) string
func StrictErrors(log *slog.Logger) (request, response func(http.ResponseWriter, *http.Request, error)); func BindingError(w http.ResponseWriter, r *http.Request, err error)
func Err(p Problem) error; func Deref[T any](p *T) T; func Ptr[T any](v T) *T

// added by this contract (sketch/httpapi_additions.go, compiled and tested inside the unit 06 tree)
func AggregateFreshness(kind string, parts ...FreshnessView) openapi.Freshness  // the rule of section 4.4
func NonNil[T any](s []T) []T                                                    // nil slice -> empty slice (a nil slice marshals as null)
func FoldIfNoneMatch(next http.Handler) http.Handler                             // joins repeated If-None-Match lines; the first step of WithRequest
```

`httpapi` cannot import `freshness`, so the caller maps `freshness.Status` to the wire string in `FreshnessView.Status`; the freshness kernel is asked for `Status.String()` returning exactly `fresh`, `stale`, `error`, `unknown` (Q-3). `AggregateFreshness` treats an unrecognised status as `unknown`. Both documents' `Freshness` schema has all members required, so a nil pointer encodes as `null` (rule A22, D-10). Earlier drafts also specified `IfNoneMatch`, `RequestError`, `ResponseError` and a `FreshnessSource` type; they are gone because unit 06 provides `NotModified` + `RequestFrom`, `StrictErrors` + `BindingError` and `FreshnessView`.

#### 2.3.4 The shape of a feature (`Mount`) and of a handler, compile-checked

```go
func (s *Service) Mount(m httpapi.Mux) {
    req, resp := httpapi.StrictErrors(s.Log)
    h := liveapi.NewStrictHandlerWithOptions(s, nil, liveapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: req, ResponseErrorHandlerFunc: resp})
    liveapi.HandlerWithOptions(h, liveapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}
```

`cmd/scp` hands every `Mount` the recording mux of `routes.Table` (architecture 8.13). A pattern with no table entry panics at registration; a table entry with no handler fails `Verify()`. The full 87-line example (conditional GET with the epoch tag, SSE response type with `resync`) is Appendix A; it imports `httpapi` and its own `liveapi` only.

### 2.4 How an operation reaches its handler (request path)

```
client -> middleware chain (request id, recover, access log, body limit, client IP, headers, rate limit, CrossOriginProtection; architecture 11.1)
       -> under /api/: ProblemMux (404/405 as problems) -> request validator (kin-openapi; HEAD validated as GET; unknown query keys, empty and repeated scalar values refused; Accept checked; D-6)
       -> under /api/v1/repos/: AliasRedirect (4.7; GET/HEAD; after validation, before the mux)
       -> WithRequest (folds repeated If-None-Match lines, stores the request) -> ServeMux (patterns registered by HandlerWithOptions through the Recorder)
       -> generated wrapper (binds path/query/header parameters into Params)
       -> strict handler -> feature method (LookupRepo first for any {repo}; reads one Snapshot; builds the response object)
       -> Visit<Op>Response writes status, headers, JSON; middleware adds X-Request-Id, X-SCP-Build, Cache-Control
```

Request validation runs in production under `/api/` only; response validation runs in handler tests and in Schemathesis (ADR-020), never in production.

---

## 3. Domain model and Go types

### 3.1 The documents

| File | Role | Contents |
|---|---|---|
| `common.yaml` | shared vocabulary | 27 schemas: primitives and enums (`Timestamp`, `NullableTimestamp`, `Slug`, `GroupId`, `DocId`, `PageSlug`, `Sha`, `PortalPath`, `HttpsUrl`, `FreshnessStatus`, `Provenance`, `DocFormat`, `DocState`, `BadgeKind`, `ProviderState`), envelopes (`Problem`, `FieldError`, `Freshness`, `PageInfo`) and resources reached by more than one feature (`Commit`, `Badge`, `DescriptorProblem`, `DescriptorInfo`, `DocIndexEntry`, `BuildInfo`, `TocEntry`, `LanguageShare`); 7 parameters (`Limit`, `LimitBounded`, `Cursor`, `IfNoneMatch`, `RepoSlug`, `GroupIdPath`, `Q`); 7 headers; 8 responses (`Problem`, `BadRequest`, `NotFound`, `NotReady`, `RateLimited`, `NotModified`, `RepoRenamed`, `Unauthorized`) |
| `openapi-public.yaml` | the public contract | info with the binding conventions, tags, `x-scp-contract`, `x-scp-server-routes`, `x-scp-fixtures` (YAML anchors shared by the examples), 22 paths, 75 schemas, 6 parameters |
| `trusted-members.txt` | the server-authored free-text members | `Schema.member  # reason`, 54 lines, read by rule A24 |
| `openapi-admin.yaml` | the admin contract | 22 public path items by `$ref` plus 2 admin operations and 21 `ops` schemas (`Status*`, `Retry*`, ...) |

### 3.2 Which schema lives where (rule A18, G7)

A schema reached by operations of more than one feature tag lives in `common.yaml`; every other schema lives in its feature's fragment. Architecture rule G7 forbids a feature importing another feature's generated package, so a schema that two features must both name cannot be owned by either. The checker computes the set from the operations and compares it with where the schema is defined. A local schema may not reuse the name of a common schema either: the alias file of the feature (1.4) would declare the name twice (rule A18, found by the alias generator).

| Feature (tag) | Fragment | Operations | Local schemas | Generated lines |
|---|---|---|---|---|
| `meta` | root (`httpapi.Static` handler, no fragment) | 1 (`getOpenApiDocument`) | 0 | 0 |
| `overview` | `internal/overview/overviewapi/overview.yaml` | 4 (`getSite`, `listRepos`, `getRepo`, `listGroupRepos`; the last two lists moved here from `catalog`, D-2) | 25 | 2,154 |
| `live` | `internal/live/liveapi/live.yaml` | 2 (`getSnapshot`, `streamEvents`) | 6 | 745 |
| `catalog` | `internal/catalog/catalogapi/catalog.yaml` | 2 (`listGroups`, `getGroup`) | 4 | 886 |
| `commits` | `internal/commits/commitsapi/commits.yaml` | 1 (`listCommits`) | 1 | 493 |
| `docs` | `internal/docs/docsapi/docs.yaml` | 2 (`listRepoDocs`, `getRepoDoc`) | 4 | 910 |
| `manifests` | `internal/manifests/manifestsapi/manifests.yaml` | 8 | 32 | 3,488 |
| `pages` | `internal/pages/pagesapi/pages.yaml` | 2 (`listPages`, `getPage`) | 3 | 714 |
| `ops` (admin only) | `internal/ops/opsapi/ops.yaml` | 2 (`getStatus`, `retryJob`) | 21 | 1,126 |

Operation counts: 1 (`meta`) + 4 (`overview`) + 2 (`live`) + 2 (`catalog`) + 1 (`commits`) + 2 (`docs`) + 8 (`manifests`) + 2 (`pages`) = 22 public operations; `ops` adds 2 admin ones, 24 in all. Local schema counts: 25 + 6 + 4 + 1 + 4 + 32 + 3 = 75 public, plus 21 for `ops`.

### 3.3 Wire primitives and their Go mapping

oapi-codegen v2.8.0 generates **type aliases** for primitives (`type Slug = string`, `type Timestamp = time.Time`), not defined types; the strong typing of the domain (`scm.Slug`) is applied by the handler at the edge. Verified on `common.gen.go`, 2026-10-14.

| Schema | Wire form | Go (generated) | Rule for handlers |
|---|---|---|---|
| `Timestamp` | RFC 3339 date-time, pattern `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$` | `time.Time` | MUST be `.UTC()` before use: `time.Time` marshals with the value's own zone offset, which would violate the `Z` pattern. Sub-second precision is allowed, never required. |
| `NullableTimestamp` | `[string, "null"]` | `*time.Time` (alias target `time.Time`; properties are pointers) | `nil` means null. The property MUST be listed in `required` (rule A22) so that the generated field has no `omitempty` and `nil` encodes as `null`. |
| `Slug` | `^([a-z0-9_-][a-z0-9._-]{0,99}\|\.[a-z0-9_-][a-z0-9._-]{0,98})$`: one URL segment of at most 100 characters, lower case, a leading `.` allowed (`.github`), never `.` or `..` | `string` | the grammar of the kernel's `scm.Slug`; `scm.ParseSlug` at the edge; reserved words `ext`, `groups`, `avatars` are refused by the catalogue, not by the pattern |
| `GroupId` | `^[a-z0-9]+(-[a-z0-9]+)*$` | `string` | the grammar of `scm.GroupID`; lower case only (mixed-case ids break key equality and joins, GR-07, GR-08) |
| `DocId` | `readme` or the repository-relative path with `/` replaced by `~` and every other byte outside `[A-Za-z0-9._-]` written `%XX` (upper-case hex); injective; at most 1,024 characters | `string` | D-3; a path whose id is longer is not indexed (`doc.id-too-long`) |
| path parameters `{repo}`, `{groupId}`, `{docId}` | the **wire form**: `minLength 1`, `maxLength 300` (`3100` for `docId`), `^[A-Za-z0-9._~%-]+$` | `string` | kin-openapi validates the percent-encoded segment (verified 2026-10-14: its gorillamux router uses `UseEncodedPath()`), so a schema with the decoded pattern or length would reject a legal request (`docs~Release%2520Notes.md`: the `%` is two characters on the wire and three bytes longer than the id; found by Schemathesis, which sent a maximum-length id). The handler gets the decoded value and parses it with `scm.ParseSlug` / `scm.ParseGroupID` / the docs unit's id parser; a value that parses to nothing is the ordinary `404` |
| `Sha` | lower-case hex object id | `string` | |
| `PortalPath` | origin-relative path starting with `/` | `string` | built only from `routes` URL builders (GR-15) |
| `HttpsUrl` | absolute `https` URL | `string` | untrusted when repository-authored |
| enums | `type: string, enum` | `string` type with prefixed constants (`always-prefix-enum-values`) | decoding never fails on an unknown value; the contract calls every enum **open**; every value is a lower-case word `[a-z0-9]+([_-][a-z0-9]+)*` (rule A26) |
| `[T, "null"]` on a primitive | `type: [T, "null"]` | `*T`, required, no `omitempty` | the only nullable form in the subset |

### 3.4 Subset rules (ADR-020: oapi-codegen 3.1 support is "initial")

Enforced by `apicheck` (rules A13-A17, A22, A23, A25-A27); each is written in `common.yaml`'s header.

| Rule | Why |
|---|---|
| `type: [T, "null"]` only on primitives; never `oneOf: [$ref, null]`; no multi-type unions otherwise; no OpenAPI 3.0 `nullable` | generator support; an optional object is omitted instead of nullable |
| a nullable member MUST be `required` (A22) | an optional nullable generates `*T` with `omitempty`: `nil` is **omitted**, not `null`, so "known to be empty" could not be said |
| arrays are required or optional but never nullable | a Go `nil` slice marshals as `null`; handlers use `httpapi.NonNil` and the contract tests fail on `null` |
| `allOf` only as one `$ref` plus one inline object (A14) | oapi-codegen merges it into one struct (`GroupDetail` = `GroupSummary` + `repos`) |
| unions only as `anyOf` of object schemas | `SseEvent` is the one union (generated with `AsSseHello()` style accessors); `oneOf` was ambiguous for the shared `generation` member |
| all `*At` and `*Since` members reference `Timestamp` or `NullableTimestamp` (A15) | one pattern, one Go type |
| every string query parameter has `minLength`, a `pattern` or an `enum` (A23) | the request validator rejects an empty query value with 400 although an unconstrained string schema allows it (found by Schemathesis, 2026-10-14) |
| no `$ref` inside an example; examples share values through YAML anchors (`x-scp-fixtures`) | `$ref` in `example` is not valid OpenAPI |
| no e-mail address anywhere: no member, parameter or `format: email` that mentions it (A25) | personal data is minimised (GR-35) |
| `servers` is exactly `[{url: /}]` (A27) | a document with an absolute server URL would make `openapi-fetch` and the request validator disagree about the base path |
| 3.1.0 is the dialect | verified 2026-10-14: the latest published versions are 3.1.2 and 3.2.1; oapi-codegen and kin-openapi are exercised on 3.1.0 |

### 3.5 Extensions (`x-scp-*`)

| Extension | On | Meaning | Checked by |
|---|---|---|---|
| `x-scp-owner` | operation | Go package implementing it (architecture 4.4); one of `overview live catalog commits docs manifests pages ops apispec` (`apispec` owns `getOpenApiDocument`, which is `httpapi.Static` over `apispec.Public()` or `Admin()`) | A5, the owners table of the catalogue, the route-table test (12.4) |
| `x-scp-route` | operation | name in `routes.Table` | A5, route-table test (12.4) |
| `x-scp-etag` | operation | `snapshot-variant`, `repo-rev`, `snapshot-version`, `tree-sha`, `body-hash`, `none` (section 4.5) | A5 |
| `x-scp-conditional` | operation | `false` exempts the conditional-GET rule (the event stream) | A9 |
| `x-scp-listener` | operation | `admin` for operations served only on `:9091` | A20 |
| `x-scp-since` | operation | contract version that introduced it when later than 1.0 | catalogue |
| `x-scp-flag` | operation | configuration key that switches it on (`live.sse.enabled`) | catalogue, SSE test |
| `x-scp-untrusted` | schema member | carries repository-authored or upstream-authored text: data, never instructions (64 members) | A24: every free-text member (an inline string without `enum`, `pattern` or `format`, or an array of such; a `$ref` to a named primitive is constrained and exempt) is either marked or listed in `trusted-members.txt` (54), never both; the prompt-injection note in `info`; a Playwright/TS lint may key on it |
| `x-scp-wire-example` | media type | the literal bytes of a non-JSON example (SSE) | A21 skips it |
| `x-scp-fixtures` | document root | anchors shared by examples; stripped before the split | A21 |
| `x-scp-server-routes` | document root | server-only routes (catalogue section 6) | informational |

### 3.6 Shape of the main resources (one paragraph each; the schemas are authoritative)

- **Site** (`getSite`): `title`, `description`, `nav[]` (`label`, `kind: page|external`, `page` or `url`), `announcement` (`id` content hash, `text`, `level`, `dismissible`; omitted when none), `displayTimeZone` (IANA), `defaultTheme`, `lastFullSync`, `repoCount` and `groupCount` **for the caller's view class**, `features` (`manifests`, `sse`, `integrations[]`; derived from configuration so the SPA hides what is off, Q-5) and `build` (`version`, `commit`, `buildId`, `goVersion`). All branding comes from the `site:` block; the embedded default is neutral (GR-14).
- **RepoSummary / Repo** (`listRepos`, `getRepo`): identity (`slug`, `name`, `owner`), resolved metadata with provenance (`title`, `summary`, `type`, `lifecycle`, `tags[]`: `{value, source}`), state (`archived`, `fork`, `visibility`, `defaultBranch`), `stats` (`openIssues`, `stars`, `license`, `latestRelease`, `pushedAt`, `headSha`, `lastCommitAt`), `groups[]`, `badges[]`, URLs (`uiUrl`, `apiUrl`, `markdownUrl`, `htmlUrl`), `freshness`. `Repo` adds the composite sections: `description`, `owners[]`, `links[]`, `system`, `apis[]`, `dependsOn[]`, `docs[]` (index entries), `descriptor`, `statuses[]` (integration providers; `disabled`, never `ok`, when unconfigured: GR-41), `technology`, `latestCommits[]` and `sections` (one freshness per section, so one failed upstream degrades one section).
- **CommitPage** (`listCommits`): `items[]` of `Commit` (`sha`, `repo`, `subject` at most 200 characters, `authorLogin` nullable, `authorName` nullable, `committedAt`, `url`); no e-mail address is ever returned (GR-35); a commit whose author is unknown has `authorLogin: null` (GR-11).
- **Doc** (`getRepoDoc`): the index entry plus `html` (sanitised, for `innerHTML` through the Trusted Types policy only), `toc[]`, `frontMatter`, `links[]`, `problems[]`, `renderedBy`, `normalised[]`, `wordCount`, `freshness`. Documents beyond the residency budget are listed with `state: not_resident` and no body.
- **Manifests resources**: `RepoManifests` (units, files with parser ids, languages, ecosystems, toolchain, diagnostics, truncation flag), `DependencyList` (paged, filtered), `DependencyGraph` (nodes, edges, cycles, summary, `truncated`), `VersionSpreadList`, `LicenseSummary`, `Component`/`DependentList` (by `purl`), and the CycloneDX 1.7 SBOM (`application/vnd.cyclonedx+json`).
- **Status** (admin): readiness gates and coverage, snapshot, store, upstreams, installations, jobs and parked keys, tasks, docs residency, descriptor and manifest problem counts, integrations, warnings. It shows repository names of parked keys, which is why it lives on the admin listener only.

---

## 4. Behavioural specification

Everything here is binding for every operation unless an operation's description in the document says otherwise. The same rules are stated, shorter, in `info.description` of the public document so that a consumer of `/api/v1/openapi.yaml` has them.

### 4.1 Names, time, null, units

| Topic | Rule | Origin |
|---|---|---|
| Paths | `/api/v1/<plural noun>[/{id}[/<sub-resource>]]`; the URL carries the major version only; singletons are nouns (`/site`, `/snapshot`, `/status`) | 11.4 |
| Operation ids | camelCase, unique, verb first (`listRepos`, `getRepo`, `streamEvents`, `retryJob`); one tag per feature package (rule A3, A4) | 11.4 |
| JSON members | camelCase; enum values lower-case, hyphenated where needed (`last-commit`, `github-topics`), except stored states that use underscores (`not_resident`, `too_large`) because they mirror `freshness.State` | 11.4 |
| Identifiers | strings, never numbers (a GitHub id is never exposed); slugs and group ids are the only keys in URLs | ADR-023 |
| Time | RFC 3339 `date-time`, always UTC with the literal `Z`; durations are integers named `...Seconds`; sizes in bytes (`...Bytes`) | 11.4, GR-38 |
| Display zone | the server never formats times for display; `site.displayTimeZone` tells the SPA which IANA zone to use with `Intl.DateTimeFormat` | Q-16 |
| Null versus absent | **absent** = not applicable to this resource; `null` = known to be empty or unknown, only where the schema says `[T, "null"]` and then the member is `required`; an optional object is omitted, never `null`; arrays are `[]`, never `null` and never omitted when required | 11.4, A22 |
| Booleans and counts | counts are integers with `minimum: 0`; ratios are numbers in `[0, 1]`; no string-typed booleans | |
| Text | UTF-8 only; no HTML except the members named `html` (sanitised server-side, `x-scp-untrusted`) | ADR-005 |
| Unknown members | clients MUST ignore unknown response members; the server MUST reject unknown **request** query keys (4.12) | design choice |
| Enums | open in responses (clients tolerate unknown values); closed in requests (the validator rejects an unknown filter value) | 11.4 |
| URLs in bodies | origin-relative `PortalPath` for portal resources built from `routes`, absolute `HttpsUrl` only for external systems; absolute portal URLs appear only in text outputs (llms, sitemap) from `server.public.public_url` | GR-15 |

### 4.2 Envelopes and pagination

A collection is always `{items: [...], page: {nextCursor, limit}, freshness: {...}}`, never a bare array, even when the collection is small and bounded. Single resources that come from cached GitHub data carry `freshness` (or per-section freshness for composites); pure configuration resources (`getSite`, `getPage`) do not need it where the schema does not list it.

| Collection kind | Operations | `limit` (parameter) | Default | Maximum |
|---|---|---|---|---|
| unbounded, growth proportional to history or to the dependency set | `listCommits`, `listRepoDependencies`, `listComponentDependents`, `getDependencyVersions` | `Limit` | 50 | 200 |
| bounded by configuration or by the catalogue size | `listGroups`, `listRepos`, `listGroupRepos`, `listRepoDocs`, `listPages` | `LimitBounded` | 1000 | 1000 |

Cursor (keyset) pagination only; there is **no total count and no page number** (a count over a 1,000-repository catalogue is cheap but a count over commits or dependencies is not, and a count visible to an anonymous caller is a visibility leak, 17.3). Rules:

1. `cursor` is opaque to clients: nothing in this contract lets a client build, parse or order one. The format belongs to unit 06 (`httpapi.EncodeCursor` / `DecodeCursor`: base64url without padding of a version byte `0x01` followed by a JSON object of endpoint-specific keyset members, for commits the commit time and a sha prefix as tiebreak, and the fingerprint `f` of the canonicalised filter and sort parameters from `httpapi.QueryFingerprint`); the cursors printed in the examples of the documents are illustrative and are not byte-exact output of that function. A cursor used with different filter or sort parameters, a malformed cursor, an unknown version or a cursor older than the data it points into answers `400 invalid-cursor` (never an empty page, never a 500). The `Cursor` parameter limits the length to 512 characters and the alphabet to `[A-Za-z0-9_-]`.
2. Order is total and deterministic: every sort has a unique tiebreak (commits: `committedAt` descending then `sha`; repositories: the chosen field then `slug`), so a page boundary never skips or repeats an item that existed before the first request. Items created while paging may be missed or seen; that is the keyset contract.
3. `page.nextCursor` is `null` on the last page, and the same cursor is advertised as `Link: <same URL with the new cursor>; rel="next"` (RFC 8288) on every 200 of a collection operation; absent on the last page. `page.limit` is the page size that was applied (the default when the parameter is absent); a `limit` above the maximum is not clamped, it is `400 validation-failed` with pointer `#/query/limit`.
4. A bounded collection returns everything in one page by default; callers that want smaller pages pass `limit` and follow `nextCursor` like any other.
5. The `ETag` of a page is the ETag of the collection (4.5); a client that follows cursors revalidates each page separately.
6. Pagination is applied **after** visibility filtering (4.8): cursors, `Link` headers and `limit` never reveal an invisible item.

### 4.3 Filtering, sorting, field selection

- **Filtering** is by explicit query parameters, documented per operation (catalogue section 2). Arrays are repeated parameters (`?scope=runtime&scope=dev`, `style: form, explode: true`). The values of one repeated parameter are OR-ed, except `tag` of the repository lists, which is AND-ed ("carrying all of these tags"), as its description says. Different parameters are AND-ed. `q` is a case-insensitive substring match on the name and title fields of the resource (never a query language, never a regular expression: a user-supplied regex is a CPU hazard). `archived` is `include|exclude|only` (default `include`), because a three-valued boolean is clearer than two parameters.
- **Sorting** is `sort=<field>` or `sort=-<field>` from a per-operation enum (`name`, `pushedAt`, `lastCommit`, `openIssues`, `lifecycle` for repositories; `name`, `ecosystem`, `scope` for dependencies; `config`, `name` for groups, where `config` is the default and means the configuration order, so the operator's deliberate ordering is what readers see). One sort key per request; the tiebreak is fixed (4.2).
- **Field selection**: none. No `fields=` parameter, no sparse fieldsets, no `expand=`. Reasons: (a) the payloads are modest (a 212-repository `listRepos` response is about 340 KB uncompressed from the fixture's 1.6 KB per summary, and JSON this repetitive typically compresses 7-10x, an estimate not a measurement, section 5); (b) ETag 304s make repeat fetches free; (c) the composite `getRepo` already exists for the one place where joins matter; (d) selection multiplies the ETag, cache and test matrix and the generated TypeScript types (a response whose shape depends on a query parameter cannot be typed by `openapi-typescript`). If a payload becomes a problem, add a narrower operation (additive), not a mask.
- **Search across resources** is not an API concern in v1; the SPA filters the bounded `listRepos` collection client-side for instant results and uses `q` for the server-side equivalent.

### 4.4 The freshness envelope

Every resource built from cached upstream data carries a `Freshness` object (GR-26, GR-40). It is derived at read time: `freshness.Info.Derive(now)` is evaluated in the handler because the engine never publishes merely because time passed (architecture 8.2).

| Member | Type | Meaning |
|---|---|---|
| `status` | `fresh \| stale \| error \| unknown` (open enum) | `fresh`: a good value exists and `now - fetchedAt <= 1.5 x TTL`; `stale`: a good value exists but is older or the last attempt failed; `error`: no good value and the last attempt failed; `unknown`: never fetched |
| `kind` | string | the item family: `catalog.stats`, `commits.view`, `docs.repo`, `manifests.repo`, `pages.set`, `integrations.<provider>` ... (the engine job kinds of 9.6) |
| `asOf` | timestamp or null | when the upstream data was last known to be current (the commit time, the tree time) |
| `fetchedAt` | timestamp or null | last successful fetch or successful `304` revalidation |
| `checkedAt` | timestamp or null | last attempt, successful or not |
| `staleSince` | timestamp or null | `fetchedAt + 1.5 x TTL` while `stale`, else `null` (a pure function of `Info`, so it does not change between reads) |
| `lastErrorCode` | string or null | stable code of the last failed refresh, a member of the closed snake_case vocabulary of `92-error-catalogue.md` section 3 (`rate_limited`, `not_found`, `unavailable`, `permanent`, `transient`, ...), else `null` |

All seven members are required: absent never means null here (D-10). The SPA shows a stale indicator when `status != fresh` and never from the clock.

**Aggregate rule** (design choice; implemented by `httpapi.AggregateFreshness`, compile-checked and unit-tested, Appendix A). A response built from several items (a list, a composite, a global projection) reports one aggregate that is **never better than its worst contributor**:

- `status` is the worst contributor in the order `error > unknown > stale > fresh`;
- `asOf`, `fetchedAt`, `checkedAt`, `staleSince` are each the oldest non-null value among the contributors (so `fetchedAt` is the age of the oldest item), `null` when every contributor's value is null;
- `lastErrorCode` is the code of the worst contributor (first one wins on a tie);
- `kind` names the aggregate family (`catalog.stats` for the repository list);
- no contributor at all (an empty catalogue after a successful reconcile) yields `status: unknown`, all timestamps `null`.

Items inside a list carry their own `freshness` where the schema says so (`RepoSummary.freshness`), so the SPA can mark one stale row. Composite `getRepo` additionally carries `sections: {commits, docs, manifests, ...}`, one `Freshness` per section, so one failed upstream degrades one section, not the page.

**Stale versus not ready** (11.4): once any snapshot exists every operation answers `200` with the best value it has and `freshness.status` telling the truth (stale-while-error, GR-40). Only a cold start without any data answers `503 not-ready` with `Retry-After`. `getSnapshot` is the exception: it never answers 503, a cold start is `readiness: warming` with `version: 0`, so the SPA's poller works during warm-up (D-11).

### 4.5 Conditional GET and entity tags

Every cacheable GET returns a **weak** `ETag` and `Cache-Control: no-cache` (set by the middleware of the API response class, not declared per operation); the client revalidates every time and a match is `304` without a body. The tag is computed **before** the body is built, so a `304` costs no rendering.

| `x-scp-etag` | Tag | Used by | Property |
|---|---|---|---|
| `snapshot-variant` | `W/"g<epoch>-<snapshot version>-<view class>[.<digest>]"` | 15 operations whose body depends on more than one repository or on global projections: `getSite`, `listGroups`, `getGroup`, `listGroupRepos`, `listRepos`, `getRepo`, `listCommits`, `listPages`, `getPage`, the five `manifests` global/query operations, `listRepoDependencies` | changes on every new snapshot; correct without per-handler dependency analysis |
| `repo-rev` | `W/"r<epoch>-<Rev(slug)>-<view class>[.<digest>]"` | `listRepoDocs`, `getRepoDoc`, `getRepoManifests` (bodies that are a pure function of one repository entry) | stays valid while other repositories change, so a 212-repository portal does not refetch every document index on every publish |
| `tree-sha` | `W/"t<epoch>-<tree sha>-<view class>"` | `getRepoSbom` | the SBOM is a function of the tree and of the parser version, which only a new process can change |
| `snapshot-version` | `W/"v<epoch>-<version>"` | `getSnapshot` | ADR-009 with the epoch |
| `body-hash` | strong `"<sha256 prefix>"` for the embedded document (`httpapi.Static`); weak `W/"b<hash>"` of the encoded body for `getStatus` | `getOpenApiDocument`, `getStatus` | |
| `none` | no tag | `streamEvents`, `retryJob` | |

`<epoch>` is a random token chosen once per process start (`Snapshot.Epoch()`, a request to unit 03: Q-13). `<view class>` is the ordinal of `scm.ViewClass` (`0` public, `1` all) of the principal (4.8), so two principals that may see different variants can never collide in a shared cache. `getRepo` and `listRepoDependencies` use `snapshot-variant` because they join other repositories' data (`dependsOn[].resolved`, `internal.repo`), which `Rev(slug)` of one repository does not cover (D-4).

**The epoch.** A warm start rebuilds the snapshot and its version restarts at 1 (unit 03), so `W/"g1841-0"` can name two different bodies, one per process life: a browser that cached the tag before a restart would be told `304` for a different body, and an SPA that polls `getSnapshot` would see a lower version and never notice the change. The epoch makes every tag of one process life disjoint from every tag of another; the cost is one revalidation per resource after each restart, which is what a restart means anyway. `Snapshot.version` itself is still a plain counter; the SPA treats **any inequality** with the version it holds as "changed" (never "greater"), and the SSE stream answers `resync` to a `Last-Event-ID` that is ahead of the process (7.6). The tag is opaque: nothing parses it (a test asserts that the SPA contains no ETag code, ADR-009).

**The digest.** Freshness status is time-derived (4.4), so a body can change from `fresh` to `stale` without any new snapshot; a tag built only from the snapshot would answer `304` forever and hide the staleness the contract exists to show (GR-40). The tag therefore gains a suffix `.<digest>` while any contributing item is not fresh; the digest is a 64-bit hash (16 hex characters) of the contributing statuses in order (`httpapi.FreshnessDigest`, empty while all are fresh, so the common tag is `W/"g4f7a1c2e-1841-0"`; `httpapi.WithDigest` appends it). A fresh-to-stale transition changes the tag; the digest costs one pass over statuses the handler reads anyway. This refines ADR-009's "per-resource ETags use `Rev`" (D-4).

**Matching.** `If-None-Match` carries one or more entity tags or `*`; the comparison is the weak comparison of RFC 9110 section 13.1.2 (`W/` prefixes ignored); `*` matches any current representation; a malformed value is treated as no match, never as an error (`httpapi.NotModified`, called as `NotModified(RequestFrom(ctx), etag)`). The header may arrive on several lines (RFC 9110 5.3): `httpapi.FoldIfNoneMatch` joins them before the generated binder, which accepts exactly one line and would answer 400 (2.3.2 fact 7). The typed parameter `Params.IfNoneMatch` exists for the contract and the validator; the handler decides from the request. A `304` carries the `ETag` (generated `NotModified` response) and no body. A request without `If-None-Match` always gets the full response. `Cache-Control: no-cache` plus the tag lets browsers and the SPA's `fetch` revalidate transparently, so the SPA contains no ETag code (ADR-009).

**Shared caches.** Responses a `ViewAll` principal may receive are rewritten to `Cache-Control: private` by the access wrapper (unit 14, 4.7.5); anonymous responses keep `no-cache`. There is no `Vary` on identity headers because the identity is never a cache key for a public response.

### 4.6 HEAD

`HEAD` is supported on every GET operation and behaves as the `GET` without a body (same status, same headers including `ETag` and `Content-Length` where known). `ServeMux` routes `HEAD` to the generated `GET` handler (verified), but the OpenAPI request validator does not: kin-openapi's router matches methods literally, and `nethttp-middleware` v1.2.0 answers `405 method not allowed` (`text/plain`) for a `HEAD` on a `GET`-only operation (verified 2026-10-14). `httpapi/validate` therefore validates a `HEAD` as a `GET` (it clones the request with the method replaced for validation only; D-6) and the contract mock does the same. `OPTIONS` is not supported (no CORS, GR-18): `405` with `Allow: GET, HEAD` (the `Allow` header is declared on the shared `Problem` response, 7.4). The alias redirect of 4.7 answers `HEAD` too.

### 4.7 Renamed repositories: `308`

A repository that was renamed or whose slug changed (a `slug_prefix_by_source` collision resolution, ADR-023) keeps its old slug as an alias in `catalog.aliases` of the snapshot. Every operation with a `{repo}` path parameter (`getRepo`, `listRepoDocs`, `getRepoDoc`, `getRepoManifests`, `listRepoDependencies`, `getRepoSbom`) answers a known alias with:

```
HTTP/1.1 308 Permanent Redirect
Location: /api/v1/repos/billing-service/docs
Content-Type: application/problem+json
Cache-Control: no-store

{"type":"urn:scp:problem:repo-renamed","title":"Repository renamed","status":308,"code":"repo-renamed",
 "detail":"billing-svc is now billing-service.","instance":"urn:uuid:d5a3f7c9-1b2e-4084-9a6d-8e0c4b2f1a73"}
```

`Location` is the **same request** with the current slug (path remainder and query string kept byte for byte), so `fetch` and `curl -L` follow it transparently; the problem body is for clients that do not follow redirects. `308` (not `301`) because the method must not change. It is done by a **middleware**, not in each handler: a strict handler has no `*http.Request` and rebuilding the query from typed parameters per operation would be six copies of the same bug.

**Where it lives and what it does** (compile-checked in `sketch/alias.go` against the kernel types and unit 06, Appendix A). Architecture 4.4 and 9.1 give the redirect to `overview` (the package row says "slug-alias redirects"; the global `catalog.aliases` is "served as 308 by `overview`"), so the middleware is `overview.AliasRedirect`, **not** `authz` (an earlier draft of this unit placed it in `authz`, rank 3, which may not read a feature's slots; that deviation is withdrawn):

```go
type AliasLookup interface{ Resolve(ctx context.Context, slug scm.Slug) (current scm.Slug, ok bool) }
type AliasSource struct { Snapshot func() *readmodel.Snapshot; Aliases func(*readmodel.Snapshot) (map[scm.Slug]scm.Slug, bool)
                          Repos readmodel.Slot[freshness.Value[scm.Repository]]; Authz authz.Authorizer }  // implements AliasLookup
func AliasRedirect(l AliasLookup) func(http.Handler) http.Handler
```

`cmd/scp` mounts it for the API class, **after** the request validator (a segment outside the wire pattern is a `400` there) and before the mux, for `GET` and `HEAD` under `/api/v1/repos/`. `Resolve` does three things in this order: (1) a slug that names a **current** repository, visible or not, is never an alias (a name that a new repository reused stops redirecting at once; the handler answers it, `200` or the identical `404`); (2) the alias map of the snapshot (`catalog.aliases`, `old -> current`); (3) **visibility first**: `authz.LookupRepo` on the *current* slug, and a miss is an unknown slug, never a `308`, because a redirect would reveal both existence and new name (architecture 17.3; GR-35). The SPA routes (`/repos/{repo}...`) answer the same `308` through `negotiated.repo.rest` (architecture 11.2) from the same lookup.

Three details are not obvious and each has a test (`TestRenamedRepositoryRedirects`, `TestVisibilityFirst`): `Location` is built from `r.URL.EscapedPath()` with the first segment re-escaped (`url.PathEscape`) and the rest copied unchanged, so an encoded `docId` (`docs~Release%2520Notes.md`) survives; the answer is written **by the middleware**, not through `httpapi.WriteProblem`, because `WriteProblem` clamps the status to `400..599` and its header allow-list (`Allow`, `Retry-After`, `WWW-Authenticate`) has no `Location` (deviation D-14 below); dot segments (`..`), separators (`A%2FB`), upper case and every other non-slug segment fall through to the validator and handler untouched. The redirect covers **path** parameters only: `listCommits?repo=old-slug` is a `404 not-found` like any unknown slug (a silent substitution would give two different answers for the same repository and cache them under one tag), and the document says so.

### 4.8 Visibility, view classes and the admin listener

1. **An invisible repository is indistinguishable from a nonexistent one** in status code, body, headers and ETag (GR-35): `404 not-found` with the same `detail` template (`No repository with slug "x".`). `authz.LookupRepo` is the only slug resolver and the first statement of every `{repo}` handler.
2. **Lists** (`listRepos`, `listGroupRepos`, `listCommits`) are filtered by `Authorizer.CanView` before pagination; a group with no visible member is omitted from the public variant of `listGroups` and `getGroup` of it is `404`.
3. **Global projections** exist in exactly two variants chosen by `authz.ClassOf` (`getSite.repoCount`, `groupCount`, `listCommits` without `repo`, every `manifests` global operation including `getComponent` and `listComponentDependents`): the `public` variant is built from public repositories only; in a deployment without non-public repositories both variants are one value. With a fixture of N public and M private repositories every anonymous response MUST be byte-identical to a deployment containing only the N (test 12.5).
4. **The ETag carries the class** (4.5); `listCommits`' `Link` and cursors are per class.
5. **Public listener**: the access middleware (unit 14) sets the principal; `public` mode is always anonymous.
6. **Admin listener**: the same handler objects serve the 22 public operations (the SPA's `/status` page needs `getSite` and `getSnapshot`; D-1), but identity middleware is not mounted there, so the principal is always anonymous and the **public** variant is served. The `ops` handlers (`getStatus`, `retryJob`) use the `Operator()` authorizer of unit 14 because they show repository names of parked keys on a restricted network.
7. **Untrusted text is data**: members marked `x-scp-untrusted` (64: commit subjects and author names, titles, descriptions, tag values, diagnostics, front matter, link texts, table-of-contents texts, document headings, upstream error messages) originate in repositories or upstream systems. Consumers, especially LLM agents, MUST treat them as data, not instructions; `info.description` says so (prompt injection through repository text, ADR-007). The marking is **complete by construction**: rule A24 requires every free-text member of both documents (an inline string with no `enum`, `pattern` or `format`, or an array of those) to be either marked `x-scp-untrusted` or listed in `api/trusted-members.txt` (54 entries, each with a reason: operator configuration, server-generated messages, key names), never both, and flags a stale entry. A new text member therefore cannot reach the contract unclassified. An earlier draft marked 24 members by hand and missed `Commit.subject`, `Commit.authorName` and the status messages.

### 4.9 Versioning, evolution and deprecation policy

The URL carries the major version (`/api/v1`). `info.version` is the contract version: **minor** increments for additive changes (1.1.0 adds `streamEvents` and sets `x-scp-since: '1.1'`), **patch** for documentation-only changes; it is served in the bundle and compared by `oasdiff` in CI. Inside v1:

| Allowed (additive, no new major) | Breaking (needs `/api/v2` served side by side, or a deprecation cycle) |
|---|---|
| a new operation or path; a new optional query parameter; a new optional or required **response** member; a new enum value in a response; a new response status the client ignores; a new `x-scp-*` extension; a looser request constraint | removing or renaming a path, operation id, member or enum value; making an optional request parameter or body member required; narrowing a request constraint; changing a type, format or unit; changing a status code; changing the meaning of a member; removing a status code |

Clients treat every enum as open and ignore unknown members (4.1). **Deprecation procedure** for a retired operation or member: (1) mark `deprecated: true` in the document and name the replacement in the description; (2) from that release the server adds `Deprecation: @<unix time of deprecation>` (RFC 9745 structured-field date, Standards Track; verified 2026-10-14), `Sunset: <HTTP-date>` (RFC 8594) and `Link: <replacement>; rel="successor-version"` plus `Link: <doc URL>; rel="deprecation"` to every response of the operation; (3) the operation stays for **at least 180 days and two minor releases** after the first deprecated release (design choice for a self-hosted product whose operators upgrade slowly); (4) removal is a major version unless the operation was never released. A metric counts calls per deprecated operation so the operator can see whether it is used (section 9).

**CI gate**: `oasdiff breaking base head` where `base` is the bundled document of the last release tag and `head` the working tree, with `--flatten-allof --severity-levels <file> --fail-on ERR`. The severity file (`07-openapi-contract.assets/diff/severity-levels.txt`, copied to `api/severity-levels.txt`) is a single line `response-property-enum-value-added  info`, because oasdiff's default marks a new enum value in a response as an error (25 errors for one added value on the shared `FreshnessStatus`, measured 2026-10-14) while the contract defines enums as open (D-8). Verified: the additive experiment (optional query parameter, new response member, new enum value) exits 0; the breaking experiment (new required parameter, removed required property, removed path) exits 1 with `new-required-request-parameter`, `response-property-became-optional`, `api-path-removed-without-deprecation`. Deprecated operations are exempt from the removal rule only after the sunset date in the base document.

### 4.10 Error handling

The catalogue is section 7.5. The rules:

1. Every error response is `application/problem+json`, `Cache-Control: no-store`, `status` equal to the HTTP status, `code` stable, `type` `urn:scp:problem:<code>` (no dereferenceable URL is needed; `about:blank` is the default of the schema), `instance` `urn:uuid:<request id>` equal to the `X-Request-Id` response header, `retryAfterSeconds` equal to `Retry-After` when present.
2. **Clients key on `code`, never on `title` or `detail`.** `detail` is occurrence text for humans and logs; the SPA localises by `code`. Errors never contain stack traces, file paths, upstream response bodies or secrets (architecture 15.6); a `500 internal` says only to quote the request id.
3. 4xx are the caller's, 5xx are ours (GR-16). `503` always carries `Retry-After` (`not-ready`, `upstream-unavailable`, `queue-full`).
4. Validation errors list **every** violation (`errors[]` of `{pointer, detail}`; `#/query/<name>`, `#/path/<name>`, `#/header/<name>`, or a JSON Pointer into the body), built from the validator's typed errors (`openapi3filter.RequestError`, `openapi3.MultiError`), never from message strings (11.1).
5. A status that is not in an operation's `responses` can still occur through the `default` response (the shared problem); strict handlers make an undeclared status or body a **compile** error for handler authors (ADR-020).

### 4.11 Provenance

Every resolved metadata field carries its source: `{value, source}` with `source` one of `descriptor`, `catalog-info`, `codeowners`, `github-topics`, `github`, `config`, `derived`, `own`, `github-graph`, `sbom` (open enum; the architecture lists eight, `github` and `config` are added, D-9). Precedence (architecture 14.2: descriptor, then catalog-info, then CODEOWNERS, then GitHub topics, then derived) is the docs/overview features' business; the contract only guarantees the member and the enum. The SPA shows the source as a tooltip and the LLM surface prints it, so a consumer can tell an operator-declared value from a guess.

### 4.12 Requests

- **Methods.** `GET` and `HEAD` everywhere under `/api/v1` except the single admin `POST /api/v1/admin/jobs/retry`. Anything else is `405 method-not-allowed` with `Allow` (rewritten from `ServeMux`'s `text/plain` by `ProblemMux`, risk W3).
- **Unknown query keys, empty values and repeated scalars are refused**: `GET /api/v1/repos?limt=5` is `400 validation-failed` with pointer `#/query/limt`, not a silently ignored typo that returns an unfiltered 1,000-row page; `?limit=` (a declared key with an empty value, for **any** parameter type) and `?limit=5&limit=6` (a repeated scalar) are `400` with `#/query/limit`. kin-openapi accepts all three (verified: `?limit=` is read as absent; Schemathesis found it against the mock), so `httpapi/validate` compares `r.URL.Query()` with the operation's declared query parameters (design choice; the SPA is generated from the same document and cannot send an unknown key). Unit 06 implements unknown keys and repeated scalars (`queryShape`, its D13); **the empty-value rule for non-string parameters is a requirement this unit puts on it** (section 14, D-6), because rule A23 can only make an empty *string* invalid by schema (`minLength: 1`). The contract mock implements all three, which is what Schemathesis checked.
- **`Accept`.** Operations with one success media type answer `406 not-acceptable` only when `Accept` explicitly excludes it (`*/*` and an absent header always pass). `getDependencyGraph` selects its representation with `format`, not `Accept`.
- **Request bodies** exist only on `retryJob`: `Content-Type: application/json` required (`415 unsupported-media-type` otherwise), body limit 1 MiB (`413 payload-too-large`), plus the header `X-SCP-Request: 1` (CSRF guard) and `http.CrossOriginProtection` (a cross-origin browser request is `403 forbidden`).
- **Rate limits.** The API class is limited per client IP (default 300/min, burst 100; SSE 4 concurrent streams per IP); the answer is `429 rate-limited` with `Retry-After` and `retryAfterSeconds`; no `RateLimit-*` headers (the IETF draft is unstable, architecture 17.4).
- **Compression.** Dynamic JSON may be gzip-encoded by the platform (`klauspost/compress/gzhttp`); the event stream and `HEAD` are never compressed.

### 4.13 `retryJob` (the only mutation)

`POST /api/v1/admin/jobs/retry` with `{"key": "docs.refresh/acme/acme/billing-service"}` (pattern `^[a-z][a-z0-9._-]*/[^\s]+$`, 3-300 characters; per-repository kinds are `kind/source/owner/name`, global kinds `kind/_`) enqueues the keyed job at interactive priority with `force`, which also releases a parked key. It is **idempotent and coalescing**: retrying a job that is queued or running is accepted and changes nothing, so `202 {key, accepted: true}` is correct for both. An unknown key kind is `404 not-found`; a malformed key `400 validation-failed`; a full queue `503 queue-full` with `Retry-After` (`job.ErrQueueFull`); the job still passes pacing and the budget gate (the operator cannot exceed the GitHub budget with it). The route class is `admin`, so it is reachable only on `:9091`.

---

## 5. Data

This unit persists nothing at run time. Its data are build artefacts and in-memory documents.

| Artefact | Where | Size (2026-10-14) | Life cycle |
|---|---|---|---|
| design sources | `api/openapi.yaml`, `openapi.admin.yaml`, `internal/httpapi/openapi/common.yaml`, fragments | public 2,012, admin 356, common 436 lines in the single-file design form, plus `trusted-members.txt` (54 entries) | hand-written, reviewed |
| bundles | `internal/apispec/openapi.bundled.yaml` (4,854 lines), `openapi.admin.bundled.yaml` (5,588 lines) | 165 KB and 182 KB of YAML; embedded | produced by `redocly bundle`, committed, drift-checked |
| generated Go | 9 `*.gen.go` files and 8 `common_aliases.go` | 11,298 + 624 lines | produced by oapi-codegen v2.8.0 and `apicheck aliases`, committed, drift-checked |
| generated TypeScript | `web/src/lib/api/schema.d.ts` | 4,137 lines from the admin bundle | produced by `openapi-typescript` 7.13.0, committed, drift-checked |
| catalogue | `docs/reference/api.md` | ~390 lines | generated by `apicheck catalogue`, drift-checked |
| parsed spec in memory | `httpapi/validate` holds one `*openapi3.T` per listener | a few MB; built once at startup | immutable; no reload |

Memory and size budget of the responses (the contract's own limits; measured on the fixture and extrapolated): `listRepos` at 212 repositories is about 340 KB uncompressed (the fixture's `RepoSummary` is 1.6 KB minified), at the 1,000-item bound about 1.6 MB; gzip typically shrinks such repetitive JSON 7-10x (an estimate, to be measured in task T-11); the `getRepo` composite about 6-25 KB; `getDependencyGraph` is bounded by `truncated: true` beyond a node cap (manifests design); `getRepoDoc` bodies are bounded by `docs.max_doc_bytes` (512,000 default). `listRepos` at the 1,000-item bound is the largest ordinary response; a client that wants less passes `limit` and follows the cursor, and the bound is never raised.

There is no database table, no cache key and no persisted event in this unit. The snapshot version used for ETags and SSE comes from `readmodel` (architecture 8.4); the contract only specifies how it is rendered (`W/"g<epoch>-<version>-<class>"`, 4.5); the epoch is a per-process token that `readmodel` must provide next to the version (Q-13).

---

## 6. Configuration

This unit owns **no configuration keys** (it defines a contract, not a feature). It references keys owned by other units; each one is read by the unit named, never by the contract code. The "documented keys equal consumed keys" test (architecture 16) therefore has nothing to add for 07.

| Key | Owner | Effect on the contract |
|---|---|---|
| `live.sse.enabled` (default `false`) | live | when false `streamEvents` answers `404 feature-disabled`; `getSite.features.sse` mirrors it; the operation stays in the document (`x-scp-flag`) so the SPA type and the route table are stable |
| `manifests.enabled` (default `true`) | manifests | when false the eight `manifests` operations answer `404 feature-disabled`; `getSite.features.manifests` mirrors it |
| `integrations.*` | integrations | `getSite.features.integrations[]` lists configured providers; `Repo.statuses[]` shows `disabled` for the unconfigured |
| `site.*` (`title`, `description`, `nav`, `display_time_zone`, `default_theme`) | overview | the members of `getSite` |
| `pages.announcement.*` | pages | `getSite.announcement` (plain text, a level, `dismissible`; `id` is a content hash so a dismissal lasts only until the text changes) |
| `server.public.public_url` | cmd/scp | absolute URLs in text outputs only; never in JSON bodies |
| `ratelimit.api` (default `300/min`) | httpapi middleware | the `429` of the API class |
| `commits.keep`, `commits.latest_n` | commits | the bounds behind `listCommits` |
| `readiness.*` | ops | `getSnapshot.readiness` and the 503 `not-ready` rule |
| `access.mode` | access | view classes (4.8); `unauthorized` 401 occurs only when a mode requires an identity |

Build-time settings (not configuration keys): the oapi-codegen config per fragment (section 1.4), the redocly config (`redocly.yaml` extends `recommended`; `redocly lint` is clean on both documents with that default, 2026-10-14), and the oasdiff severity file (4.9). Every generator and linter version is pinned in `tools/go.mod` and `web/package.json`; Renovate updates them (ADR-017).

---

## 7. HTTP/API surface

### 7.1 Listeners and documents

| Listener | Document served at `/api/v1/openapi.yaml` | Operations | Notes |
|---|---|---|---|
| public `:9090` | public bundle (`apispec.Public()`), strong ETag = content hash | 22 | request validation under `/api/`; identity middleware (unit 14) before the mux |
| admin `:9091` | admin bundle (`apispec.Admin()`) | 24 (the 22 plus `getStatus`, `retryJob`) | restricted network only; the same handler objects; no identity middleware (4.8) |

The public bundle never contains an admin operation (checked by a test: `retryJob` absent from `apispec.Public()`, present in `Admin()`; Appendix A). The route table's API class equals the bundled document paths **in both directions** on each listener: the public `Table` has 22 entries, the admin `Table` 24 (a test in `cmd/scp`, section 12.4).

### 7.2 Operation summary (24; the full parameter and response detail is the catalogue)

| Method and path | operationId | Owner | Route name | ETag | Notable parameters |
|---|---|---|---|---|---|
| `GET /api/v1/openapi.yaml` | `getOpenApiDocument` | apispec | `api.openapi` | body-hash (strong) | none |
| `GET /api/v1/site` | `getSite` | overview | `api.site` | snapshot-variant | none |
| `GET /api/v1/snapshot` | `getSnapshot` | live | `api.snapshot` | snapshot-version | none |
| `GET /api/v1/events` | `streamEvents` | live | `api.events` | none (SSE) | `Last-Event-ID` header; flag `live.sse.enabled`, since 1.1 |
| `GET /api/v1/groups` | `listGroups` | catalog | `api.groups` | snapshot-variant | `sort=config\|name`, `q` |
| `GET /api/v1/groups/{groupId}` | `getGroup` | catalog | `api.group` | snapshot-variant | `groupId` |
| `GET /api/v1/groups/{groupId}/repos` | `listGroupRepos` | overview | `api.group.repos` | snapshot-variant | repository filters and `sort` |
| `GET /api/v1/repos` | `listRepos` | overview | `api.repos` | snapshot-variant | `q`, `group[]`, `lifecycle[]`, `type[]`, `tag[]` (AND), `archived`, `sort` |
| `GET /api/v1/repos/{repo}` | `getRepo` | overview | `api.repo` | snapshot-variant | `repo`; 308 for an alias |
| `GET /api/v1/commits` | `listCommits` | commits | `api.commits` | snapshot-variant | `repo`, `group`, `limit` (max 200), `cursor` |
| `GET /api/v1/repos/{repo}/docs` | `listRepoDocs` | docs | `api.docs.list` | repo-rev | `q` |
| `GET /api/v1/repos/{repo}/docs/{docId}` | `getRepoDoc` | docs | `api.docs.get` | repo-rev | `docId` (`~` for `/`, `%XX` for other bytes) |
| `GET /api/v1/repos/{repo}/manifests` | `getRepoManifests` | manifests | `api.manifests.repo` | repo-rev | |
| `GET /api/v1/repos/{repo}/dependencies` | `listRepoDependencies` | manifests | `api.manifests.deps` | snapshot-variant | `scope[]`, `relationship`, `internal`, `ecosystem[]`, `unit`, `q`, `sort` |
| `GET /api/v1/repos/{repo}/sbom.cdx.json` | `getRepoSbom` | manifests | `api.manifests.sbom` | tree-sha | CycloneDX 1.7 |
| `GET /api/v1/dependencies/graph` | `getDependencyGraph` | manifests | `api.manifests.graph` | snapshot-variant | `group`, `scope[]`, `ecosystem[]`, `intra`, `internal`, `focus`, `depth` (1-3), `format=json\|mermaid` |
| `GET /api/v1/dependencies/versions` | `getDependencyVersions` | manifests | `api.manifests.versions` | snapshot-variant | `group`, `ecosystem[]`, `internal`, `minVersions`, `q`, paging |
| `GET /api/v1/dependencies/licenses` | `getDependencyLicenses` | manifests | `api.manifests.licenses` | snapshot-variant | `group` |
| `GET /api/v1/components` | `getComponent` | manifests | `api.manifests.component` | snapshot-variant | `purl` (query, required) |
| `GET /api/v1/components/dependents` | `listComponentDependents` | manifests | `api.manifests.dependents` | snapshot-variant | `purl`, `scope[]`, `group`, paging |
| `GET /api/v1/pages` | `listPages` | pages | `api.pages` | snapshot-variant | paging |
| `GET /api/v1/pages/{slug}` | `getPage` | pages | `api.page` | snapshot-variant | `slug` |
| `GET /api/v1/status` | `getStatus` | ops | `api.status` | body-hash (weak) | admin only |
| `POST /api/v1/admin/jobs/retry` | `retryJob` | ops | `api.admin.retry` | none | header `X-SCP-Request: 1`, body `{key}`; admin only |

Route names: architecture 11.2 groups several paths under one name (`api.manifests.graph` covers three paths, `api.manifests.component` two, `api.groups`/`api.group`/`api.group.repos` three). `routes.Table` maps **one pattern to one name** (the recording mux registers a pattern and looks up its entry), so the contract splits them: `api.manifests.graph`, `.versions`, `.licenses`, `.component`, `.dependents`, and `api.group.repos`, `api.repos` owned by `overview` (D-2, D-7). Each name above is `x-scp-route` and must exist in `routes.Public()` or `Admin()`. The route-table test compares **owners as well as names**: unit 01's table (`01-kernel.routes.tsv`) still gives `api.repos` and `api.group.repos` to `catalog` and `api.openapi` to `httpapi`; the test fails until unit 01 adopts D-2 and the `apispec` owner (a cross-unit requirement recorded in the findings report). `getOpenApiDocument` has no strict handler: it is `httpapi.Static("application/yaml", apispec.Public())` registered by `cmd/scp`, and its owner `apispec` names the package that owns the bytes.

### 7.3 Examples for every response

Rule A7 fails the build if any response of any operation lacks an example, and kin-openapi validates every example against its schema at load time (rule A1), so **an example can never drift from its schema** (found and fixed during this design: an SSE example, a stray member in the admin status example, an unquoted YAML scalar that parsed as two keys). On 2026-10-14 all 24 operations pass A7 (every status of every operation has an example); shared responses carry named examples (`validationFailed`, `invalidCursor`, `notFound`, `featureDisabled`, `notReady`, `queueFull`, `rateLimited`, `renamed`, `unauthorized`, `internal`, `methodNotAllowed`, `forbidden`). Examples draw from one fictional organisation `acme` (anchors in `x-scp-fixtures`), so a list item, its composite and its document index agree. The same fixtures seed the contract mock (12.5) and the SPA's MSW handlers.

A paged collection (`GET /api/v1/commits?limit=2`), full exchange:

```http
GET /api/v1/commits?limit=2 HTTP/1.1
If-None-Match: W/"g4f7a1c2e-1840-0"

HTTP/1.1 200 OK
Content-Type: application/json
ETag: W/"g4f7a1c2e-1841-0"
Cache-Control: no-cache
Link: </api/v1/commits?limit=2&cursor=eyJ2IjoxLCJ0IjoxNzYwNDMxMjAwLCJzIjoiM2YyYTljMSIsImYiOiJhMWIyYzNkNCJ9>; rel="next"
X-Request-Id: 0e6c1d5a-7a3b-4c52-9f31-5d2f0f6a8b11
X-SCP-Build: 9c4e1f2
```
```json
{
  "items": [
    {"sha": "3f2a9c1d7b5e4a0f8c6d2b1a9e7f5c3d1b0a8e6f", "repo": "billing-service", "subject": "Fix rounding of VAT on credit notes",
     "authorLogin": "kari-n", "authorName": "Kari Nordmann", "committedAt": "2026-10-14T08:12:44Z",
     "url": "https://github.com/acme/billing-service/commit/3f2a9c1d7b5e4a0f8c6d2b1a9e7f5c3d1b0a8e6f"},
    {"sha": "8d1e5b7a2c9f4036a1d8e7b5c2f09a4d6e3b1c87", "repo": "core-lib", "subject": "Bump guava to 33.3.1-jre",
     "authorLogin": null, "authorName": "Dependabot", "committedAt": "2026-10-14T07:55:02Z",
     "url": "https://github.com/acme/core-lib/commit/8d1e5b7a2c9f4036a1d8e7b5c2f09a4d6e3b1c87"}
  ],
  "page": {"nextCursor": "eyJ2IjoxLCJ0IjoxNzYwNDMxMjAwLCJzIjoiM2YyYTljMSIsImYiOiJhMWIyYzNkNCJ9", "limit": 2},
  "freshness": {"status": "fresh", "kind": "commits.view", "asOf": "2026-10-14T08:12:44Z", "fetchedAt": "2026-10-14T08:13:02Z",
                "checkedAt": "2026-10-14T09:38:02Z", "staleSince": null, "lastErrorCode": null}
}
```

Revalidation of an unchanged resource, and a resource whose data went stale without a new snapshot (the digest of 4.5):

```http
GET /api/v1/repos/billing-service/manifests HTTP/1.1
If-None-Match: W/"r4f7a1c2e-1790-0"

HTTP/1.1 304 Not Modified
ETag: W/"r4f7a1c2e-1790-0"

GET /api/v1/repos/billing-service/manifests HTTP/1.1
If-None-Match: W/"r4f7a1c2e-1790-0"          (the manifests item crossed 1.5 x TTL; no publish happened)

HTTP/1.1 200 OK
ETag: W/"r4f7a1c2e-1790-0.3a9f5c20d7b1e684"   (digest present: a contributing item is stale)
... "freshness": {"status": "stale", "staleSince": "2026-10-14T08:26:07Z", "lastErrorCode": "rate_limited", ...}
```

A validation error (`GET /api/v1/repos?limit=5000&limt=1`, two violations):

```json
{"type": "urn:scp:problem:validation-failed", "title": "Validation failed", "status": 400, "code": "validation-failed",
 "detail": "The request has 2 invalid parameters.", "instance": "urn:uuid:3a8d4c77-6e0b-41f5-9c0d-2b7e5f1a9c68",
 "errors": [{"pointer": "#/query/limit", "detail": "must be at most 1000"}, {"pointer": "#/query/limt", "detail": "unknown parameter"}]}
```

### 7.4 Response headers

| Header | Declared in the document | Set by | When |
|---|---|---|---|
| `ETag` | yes, typed field on every `200` and `304` of a conditional GET | handler | always on a conditional GET |
| `Link` (`rel="next"`) | yes, on every collection `200` | handler | when `nextCursor` is not null |
| `Retry-After` | yes (`RetryAfter`), on `429`, the `NotReady` responses and the shared `Problem` response | handler or rate limiter | `429`, `503` |
| `Location` | yes (`Location`), on the `308` | `overview.AliasRedirect` (4.7) | renamed repository |
| `Allow` | yes (`Allow`), on the shared `Problem` response (the `default` of every operation, so the generated `...default...` type has the field) | `ProblemMux` | `405` |
| `X-Request-Id` | yes, on the shared `Problem` response only (equals `instance`, 4.10) | middleware sets it on every response; a handler that returns a `default` problem leaves the pointer nil | every error |
| `Content-Type` | by media type | generated `Visit` method | always (`application/json`, `application/problem+json`, `application/yaml`, `application/vnd.cyclonedx+json`, `text/plain`, `text/event-stream`) |
| `Cache-Control` (`no-cache`; `no-store` on problems; `private` for the `all` class) | **no** | API-class middleware, unit 14 wrapper | always |
| `X-Request-Id` on success, `X-SCP-Build` | **no** (stated once in `info.description`) | middleware | every response |
| `Deprecation`, `Sunset`, `Link rel=deprecation` | no (appear with the first deprecation) | deprecation middleware in `cmd/scp` | deprecated operations only |
| `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; sandbox`, `X-Content-Type-Options: nosniff` | no | security-headers middleware, API class (architecture 11.3) | always |

A declared header is a pointer field in the generated response type and a nil pointer writes nothing, so it can never overwrite a value the middleware set (2.3.2 fact 6). Headers that exist on every response and carry nothing the client acts on (`X-SCP-Build`, `Cache-Control`, the security headers) stay undeclared so that no operation grows a field for them. The headers a client *does* act on (`ETag`, `Link`, `Retry-After`, `Location`, `Allow`) are declared, which is what lets `apitest` assert them against the document (12.3).

### 7.5 Error catalogue

Every row is a stable `code`. "Produced by" names the first component that can answer; the examples are in `common.yaml` under `components/responses` (the example name is given). Status `308` is listed for completeness; it is an answer, not an error.

| `code` | Status | Meaning and when | Produced by | `Retry-After` | Example |
|---|---|---|---|---|---|
| `validation-failed` | 400 | a parameter, header or body violates the contract; `errors[]` lists every violation | request validator (`httpapi/validate`), `httpapi.BindingError` | no | `BadRequest.validationFailed` |
| `invalid-cursor` | 400 | cursor malformed, wrong version, from different filters or sort, or expired | handler via `httpapi.DecodeCursor` | no | `BadRequest.invalidCursor` |
| `unauthorized` | 401 | an identity is required (`access.mode` demands it) and none was presented | access middleware (unit 14) | no | `Unauthorized.unauthorized` |
| `forbidden` | 403 | `CrossOriginProtection` rejected a cross-origin state-changing request, or the identity lacks the right; **never** used for an invisible repository | platform middleware | no | `Problem.forbidden` |
| `not-found` | 404 | no such resource, or an invisible repository (identical, GR-35) | handler (`authz.LookupRepo`), `ProblemMux` | no | `NotFound.notFound` |
| `feature-disabled` | 404 | the operation's feature is switched off (`manifests.enabled`, `live.sse.enabled`) | handler | no | `NotFound.featureDisabled` |
| `method-not-allowed` | 405 | wrong method on a known path | `ProblemMux` (with `Allow`) | no | `Problem.methodNotAllowed` |
| `not-acceptable` | 406 | `Accept` excludes the only representation | request validator | no | catalogue (no separate example: same shape) |
| `payload-too-large` | 413 | body above the class limit (1 MiB; webhook and CSP reports have their own) | body-limit middleware | no | `Problem` (same shape) |
| `unsupported-media-type` | 415 | a body that is not `application/json` on `retryJob` | request validator | no | `Problem` (same shape) |
| `rate-limited` | 429 | per-client limit of the route class exceeded; SSE stream limit exceeded | rate-limit middleware | yes | `RateLimited.rateLimited` |
| `internal` | 500 | a bug on our side; recovered panic; `detail` never leaks cause | recover middleware, the response hook of `httpapi.StrictErrors` | no | `Problem.internal` |
| `upstream-unavailable` | 503 | no cached value exists and the upstream needed to build one is down | handler | yes | `NotReady` family |
| `not-ready` | 503 | cold start with no snapshot yet (never for `getSnapshot`) | handler | yes (default 15 s) | `NotReady.notReady` |
| `queue-full` | 503 | a bounded queue refused work (`job.ErrQueueFull`; `retryJob`) | handler | yes | `NotReady.queueFull` |
| `overloaded` | 503 | an admission limit is exhausted (the webhook's body slots); distinct from `queue-full`, which is the job queue | `webhook` (server route, not an API operation) | yes | none (server route); added by CD-ER-03 (unit 04 D5) |
| `request-timeout` | 408 | the request body was read too slowly (webhook `body_timeout`) | `webhook` | no | none (server route); added by CD-ER-03 (unit 04 D5) |
| `unhealthy` | 503 | a liveness check failed (`/livez` on the admin and public listener; not an API operation) | `ops` | yes | none (server route, problem shape of `Problem`); added by the cross-design review CD-ER-02 (unit 15 R-06-1) |
| `repo-renamed` | 308 | the slug is an alias; `Location` carries the current URL | `overview.AliasRedirect` | no | `RepoRenamed.renamed` |

`code` is an open vocabulary: clients treat an unknown code by its status class. A client that wants to retry follows: `429` and `503` retry after `Retry-After` with jitter, `500` retries once, `4xx` never. The SPA maps `code` to localised text; `not-found` for a `{repo}` page shows the NotFound view, `not-ready` shows the warm-up banner, `rate-limited` the retry banner.

### 7.6 The SSE event schema (`GET /api/v1/events`, v1.1, behind `live.sse.enabled`)

An **invalidation channel only**: payloads carry no resource data and no repository names (a stream must not enumerate invisible repositories), so a lost, duplicated or replayed event is harmless and the contract of every other operation is unchanged (ADR-009). The client invalidates the named query topics and the ordinary conditional GETs transfer the data. Polling `getSnapshot` remains the baseline when the stream is off, blocked or failing.

Wire format (`text/event-stream`, UTF-8):

```
retry: 3000

id: 1841
event: hello
data: {"generation":1841,"buildId":"9c4e1f2"}

id: 1842
event: invalidate
data: {"topics":["commits","catalog"],"generation":1842}

: hb

id: 1900
event: resync
data: {"generation":1900}

```

| Element | Rule |
|---|---|
| `retry: 3000` | first line of every connection; on graceful shutdown the server sends `retry: 30000` and closes, so reconnecting clients wait until the replacement is up |
| `id:` | the snapshot version (a decimal integer); the browser sends it back as `Last-Event-ID` on reconnect; pattern `^[0-9]{1,20}$`. A version is meaningful only inside one process life (versions restart at 1), which is why the next row exists |
| `event: hello` | first event of every connection: `SseHello {generation, buildId}`; a `buildId` different from the SPA's means a new deployment, reload |
| `event: invalidate` | `SseInvalidate {generation, topics[]}`; topics are an open enum `site`, `catalog`, `commits`, `docs`, `manifests`, `pages`, `status`; changes are debounced 250-500 ms into one event per generation |
| `event: resync` | `SseResync {generation}`: the client must invalidate **everything**. The server sends it right after `hello` when the client's `Last-Event-ID` is **ahead of** the server's version (the server restarted; versions restart at 1 and a plain "nothing newer" would hide every change) or further behind than the 256-entry change ring (`ChangesSince`). A client compares generations for **inequality**, never for "greater" |
| `: hb` | comment heartbeat every 15 s (keeps proxies from closing an idle stream) |
| framing | one `data:` line of JSON per event, no multi-line data; never compressed; excluded from the write timeout through `http.ResponseController` |
| limits | 4 concurrent streams per client IP (`429 rate-limited` beyond); a slow client never blocks the publisher (each client holds one coalesced pending invalidation, not a queue) |
| disabled | `404 feature-disabled` when `live.sse.enabled` is false; `getSite.features.sse` tells the SPA not to try |

Schema (in the `live` fragment; `anyOf` because the three payloads share the `generation` member and `oneOf` was ambiguous, 12.6):

```yaml
SseEvent: {anyOf: [{$ref: '#/components/schemas/SseHello'}, {$ref: '#/components/schemas/SseInvalidate'}, {$ref: '#/components/schemas/SseResync'}]}
SseHello:      {type: object, required: [generation, buildId], properties: {generation: {type: integer, format: int64, minimum: 0}, buildId: {type: string}}}
SseInvalidate: {type: object, required: [generation, topics], properties: {generation: {type: integer, format: int64, minimum: 0}, topics: {type: array, items: {$ref: '#/components/schemas/InvalidationTopic'}}}}
SseResync:     {type: object, required: [generation], properties: {generation: {type: integer, format: int64, minimum: 0}}}
InvalidationTopic: {type: string, enum: [site, catalog, commits, docs, manifests, pages, status]}   # open
```

OpenAPI cannot describe an event stream natively (3.1 has no `itemSchema`; 3.2.0 adds one, "a schema describing each item within a sequential media type", verified 2026-10-14; this contract stays on 3.1.0), so the media type carries one named example per event (`hello`, `invalidate`, `resync`, each validated against its schema) and the literal bytes in `x-scp-wire-example`. The generated Go `StreamEvents200TexteventStreamResponse{Body io.Reader}` is **not** used for streaming; the feature returns a hand-written response type with `VisitStreamEventsResponse(w)` that writes the headers, flushes after each event and watches `r.Context()` through the handler's `ctx` (Appendix A shows it compiling; the generated copy loop blocks on `io.Reader` and cannot select on shutdown). In the browser the SPA uses `EventSource('/api/v1/events')`, with `hello.buildId` skew detection and a fall-back to polling after three failed reconnects.

### 7.7 Server-only routes

`x-scp-server-routes` of the public document and catalogue section 6 list the routes that are part of the portal's HTTP surface but not of the JSON contract: badges, media, `llms*.txt`, raw Markdown, `dependencies.md`, webhooks, probes, `sitemap.xml`, `robots.txt`, `/schemas/scp/{file}`. They keep their own route names, CSP classes and cache policies (architecture 11.2) and are not validated against OpenAPI. A link in any JSON body to one of them is a `PortalPath` built from `routes` (`markdownUrl`, `badges[].url`, `llmsUrl`).

---

## 8. Concurrency and lifecycle

- **Documents are immutable.** The bundled bytes are embedded; `httpapi/validate` parses each once at startup into a read-only `*openapi3.T` and router. Both are read concurrently by all requests without locks (race-tested, 12.4); there is no reload and no hot swap (a new contract is a new binary). The epoch is generated once in `cmd/scp` before the publisher starts and never changes.
- **Startup (fail closed).** In `cmd/scp`, before listeners open: (1) load and validate `apispec.Public()` and `Admin()` with kin-openapi (a bad bundle is a build bug caught in CI, a startup error here); (2) build the request validator per listener; (3) call every feature's `Mount` with the recording mux; (4) `routes.Table.Verify()` fails the start when an operation has no handler or a handler pattern has no table entry; (5) compare the table's API class with the bundled paths (a startup assertion mirroring the test). Steps 1-5 belong to the existing startup steps 1-3 of architecture 10.1 (abort on failure); they add well under 100 ms (measured 2026-10-14 with kin-openapi v0.150.0: loading a bundle from file and building the gorillamux router took 28 ms for the public and 24 ms for the admin bundle, best of five, on the design machine).
- **Per request.** A strict handler runs on the connection's goroutine; it loads the snapshot **once** (`Publisher.Current()`, wait-free) and never again, so one response is consistent with one version, and the ETag it computes names that version. Handlers hold no locks and do no I/O (the serving path never touches the store or the network, architecture 10.5); the only blocking handler is the event stream.
- **Event stream lifecycle.** One goroutine per connected client (the handler's own); it selects on the client context, a coalesced wake channel from the publisher, a 15 s heartbeat ticker and the shutdown drain signal. The `Drain` hook of `live` (architecture 10.4) sets the drain signal first, every stream sends `retry: 30000` and returns, and `Server.Shutdown` then finds no long-lived handler. A stream owns no unbounded buffer; a slow client coalesces to one pending invalidation or is dropped with `resync` on reconnect. Goroutine leaks are asserted with `testing/synctest` and Go 1.27's `goroutineleak` profile (12.4).
- **Conditional GET is lock-free**: the tag is computed from the loaded snapshot, the process epoch (immutable after start) and the freshness inputs; a `304` allocates nothing but the response head.
- **The alias redirect** reads the same one snapshot as the handler would (`Publisher.Current()`, wait-free), holds no state and no lock, and writes its own response; it adds one map lookup to every `GET`/`HEAD` under `/api/v1/repos/` (a slug that is a current repository returns after one slot read).
- **No background work** belongs to this unit; it adds no ticker, no worker and no shutdown stage.
- **Idempotency.** Every GET and HEAD is safe and idempotent; `retryJob` is idempotent by key coalescing (4.13).

---

## 9. Observability

This unit adds no business metric. The contract's observability is the **vocabulary** that the existing instruments and logs use.

| Signal | Source | What the contract fixes |
|---|---|---|
| access log | `httpx` (architecture 11.1) | the route **pattern** (`GET /api/v1/repos/{repo}`), status, duration, bytes, request id; never the query string with secrets, never the raw path; fields `operation` (the `operationId`) and `problem_code` are added when the validator or `WriteProblem` ran (design choice, Q-6) |
| request metrics | otelhttp server metrics (`http.server.request.duration` by route pattern and status class) | the route pattern comes from `r.Pattern`, so the label set is exactly the contract's paths (bounded cardinality: 24 patterns) |
| `scp_api_deprecated_requests_total{operation}` | `cmd/scp` deprecation middleware (owned here) | a counter registered **only when at least one operation is `deprecated: true`** (none at v1.0); label is the `operationId`; lets the operator see whether a retired endpoint is still used before the sunset date (4.9) |
| `scp_sse_clients` | `live` (architecture 18.2) | not owned here; the stream's count |
| traces | `http.server` span with the route pattern | the span name is the contract pattern; handlers add attributes of their own |
| freshness in the body | the envelope itself | `freshness.status` is the user-visible health of the data; the same values drive badges, `/status` and logs (architecture 15.3) |
| contract version | `getSite.build.version`, `info.version` of the bundle | support and the SPA's skew check; `scp_build_info` already exists |

Contract-test telemetry (CI only): the Schemathesis run prints per-operation coverage, the `apicheck` run prints per-rule counts (section 12), the oasdiff run prints the changelog entry that goes into `CHANGELOG.md`.

---

## 10. Security considerations

Threats here are those of an **input contract**: what a hostile or confused client can make the API do, and what the API may reveal. The controls are structural (generated, validated, tested), not reviewed prose.

| Threat | Control | Test |
|---|---|---|
| malformed or oversized input reaching handlers | request validation against the contract under `/api/` (types, bounds, patterns, enums, required); every string parameter has `maxLength` or a pattern; `limit` is bounded (200 or 1000); `Cursor` is 512 characters of base64url; bodies 1 MiB; unknown query keys refused; empty string values refused (A23) | Schemathesis negative tests, apicheck A23, validator tests |
| path traversal and injection through identifiers | `Slug` excludes `.`, `..` and every separator; `GroupId` is `^[a-z0-9]+(-[a-z0-9]+)*$`; `DocId` is one segment (`/` becomes `~`, every other byte outside `[A-Za-z0-9._-]` becomes `%XX`), injective and at most 1,024 characters, so a document id can never address another path or collide with another; the path parameters validate the wire form and the handler re-parses the decoded value (`scm.ParseSlug`), so an encoded `%2e%2e` or `%2F` is a `404`, never a lookup; `purl` is a query parameter, never a path segment; no identifier is ever used in a file path (documents come from the snapshot) | pattern tests, fuzz of `DocId` and `Slug`, `TestVisibilityFirst` (dot segments, `%2F`) |
| ReDoS | no user-supplied regular expression anywhere (`q` is a substring); contract `pattern`s are RE2 (Go's `regexp`, linear time) and simple | review rule, apicheck lists patterns |
| enumeration of private repositories | one resolver (`LookupRepo`), filtering before pagination, per-view-class global projections, ETag carries the class, `308` only after visibility, identical `404` for unknown and invisible, no repository names in SSE payloads, no total counts | the anonymous enumeration test over **every route in the table**, extended to counts (12.5) |
| cross-site request forgery on the only mutation | admin listener only; `Content-Type: application/json` (a cross-origin form cannot send it without a preflight, and there is no CORS); header `X-SCP-Request: 1` (a custom header triggers a preflight that is never granted); `http.CrossOriginProtection` | tests with forged `Origin`, missing header, wrong media type |
| cross-origin reads | no CORS headers at all, `OPTIONS` is `405`; JSON served with `nosniff` and a `default-src 'none'; sandbox` CSP, `problem+json` cannot be rendered as a document | header golden tests per response class |
| script injection through repository text | every `html` member is `safehtml.SafeHTML` produced by the single sanitiser and consumed by one sink (`innerHTML` through the Trusted Types policy `scp-html`); plain text members are `x-scp-untrusted` and rendered as text; heading ids are prefixed `doc-` (DOM clobbering) | sanitiser corpus (unit 05), Playwright CSP check |
| prompt injection through the LLM-facing JSON | `info.description` states that `x-scp-untrusted` members are data, not instructions; **rule A24 makes the marking complete** (every free-text member is marked or on the reviewed `trusted-members.txt`); `dependencies.md` carries `trust: untrusted-data`; provenance tells a consumer which values an operator declared | apicheck A24 with a negative test per failure mode, ADR-007 |
| information disclosure in errors | `500 internal` says only to quote the request id; validator errors name the parameter and the constraint, never internals; no stack traces, no upstream bodies, no tokens (secrets are a type that cannot be formatted) | error golden tests, a grep test for `panic`/`goroutine` in problem bodies |
| personal data | no e-mail addresses anywhere in the contract (GR-35); commit authors are login and display name only | apicheck A25: no member, parameter or `format` that mentions e-mail, with a negative test |
| a stale `304` hiding a changed body | every weak tag carries the process epoch, so a restart (versions restart at 1) invalidates every client-held tag; the freshness digest covers time-derived changes; the SPA compares versions for inequality | `TestTagDoesNotSurviveARestart`, digest tests |
| a valid conditional request failing | repeated `If-None-Match` lines are folded before the generated binder (RFC 9110 5.3); an unparseable value is no match, not an error | `TestRepeatedIfNoneMatchLines` |
| resource exhaustion | per-IP rate limits (API 300/min burst 100, SSE 4 streams), bounded response sizes (section 5), `getDependencyGraph` depth <= 3 and `focus` length, node cap with `truncated`, cursor pagination instead of offsets, every path parameter and string query parameter length-bounded (the wire form allows 3 bytes per id byte) | limit tests, load smoke |
| spec and codegen supply chain | all generators pinned (`tools/go.mod`, `web/package.json`), generated source committed with CI drift gates, generators are dev-time only; runtime dependencies are `oapi-codegen/runtime` and its two transitive modules (`go-jsonmerge/v2`, `google/uuid`; Q-2) | drift gate, `go mod verify`, vulnerability scan |
| the contract as an attack map | the public document lists only public operations; the admin document is served only on `:9091`; examples use a fictional organisation and no real hostnames or tokens | test: public bundle contains no `retryJob`, no `/status`; secret-pattern scan of examples |
| a version number as a side channel | `getSnapshot` exposes one global counter that also advances when a private repository changes; an anonymous caller learns only that something changed, not what (Q-8) | recorded, accepted for v1 |

The admin listener is **not** an authentication boundary of its own: it relies on being unreachable from outside (network policy, architecture 19). The contract therefore makes the single mutation harmless by construction: it only enqueues a keyed job that the engine would run anyway, passes the same budget gate, and is idempotent.

---

## 11. Guardrail and capability trace

### 11.1 Capabilities

| Capability | Contract realisation |
|---|---|
| root redirect to the dashboard | not a JSON resource: `/` is an SPA route of the route table (architecture 11.2); the only redirect in the contract is the `308` of 4.7, and `x-scp-server-routes` lists the server-only routes |
| dashboard: one card per group | `listGroups` (configuration order, `description` from config, default repository's GitHub description, repository count, default repository badges, `defaultRepoState` so a missing default degrades instead of disappearing, GR-16) |
| latest-commits panel with live refresh | `listCommits` with `limit` (the panel length is `commits.latest_n`); the refresh is `getSnapshot` polling or SSE, then a conditional GET; `freshness` makes staleness visible (GR-40) |
| group page: repository table and latest commits | `getGroup` + `listGroupRepos` (filters, sort) + `listCommits?group=` |
| commit log: all, per group, per repository | one operation `listCommits` with `repo` and `group` filters and cursor paging over write-time indexes, never a per-request sort of the whole data set (GR-28); the API has no branch (Q-18) |
| README and contents viewer | `getRepoDoc` (`readme` is the README; `docId` for others), `listRepoDocs`; sanitised `html`, `toc`, `frontMatter`; `/repos/{repo}/readme.md` server-only |
| configured Markdown pages | `listPages`, `getPage` (config-driven Markdown pages, Q-04) |
| layout, navigation, header status ("Updated", "Connected repos") | `getSite` (`nav`, `announcement`, `lastFullSync`, `repoCount`, `groupCount`, `build`); `lastFullSync` is never null-by-omission (GR-11) |
| syntax highlighting | the API carries sanitised `html` only (`Doc.html`, the one HTML member, `x-scp-untrusted`); highlighting is the SPA's; there is no highlight member to keep in step |
| dark/light theme | `getSite.defaultTheme`; the toggle itself is client-side |
| not-found and error pages | real status codes and problem documents with a request id (GR-16); the SPA renders the page by `code` |
| static assets, robots | server-only routes; `robots.txt`, `sitemap.xml` listed in `x-scp-server-routes` |
| grouping configuration, many-to-many membership by pattern, default (lead) repository | `listGroups`, `getGroup` (`GroupSummary`: configuration order, `defaultRepo`, `defaultRepoState: ok\|missing\|unset`), `listGroupRepos`; a repository lists its `groups[]` (many-to-many); `GroupId` has the grammar of `scm.GroupID` and is lower case only; `GroupIdPath` validates the wire form |
| discovery with a visibility policy (public/private/all) | view classes of 4.8: every list is filtered before pagination, global projections have a `public` and an `all` variant, the ETag carries the class, an invisible repository is the identical `404`; tested by the anonymous enumeration over every route (12.5) |
| branches | by design the API has **no branch** anywhere (Q-18): `Repo.defaultBranch` reports what GitHub says, `listCommits` has no `branch` parameter, `docId` addresses a path on the default branch |
| commit ingestion (latest N, default branch) | `listCommits` over the per-repository lists of the snapshot (`commits.keep`, 30 per repository), `Commit` without any e-mail member (A25), `freshness.kind: commits.view` |
| licence detection | `RepoStats.license`, `getDependencyLicenses` (`LicenseSummary`), licences in the SBOM; the licence badge is `BadgeKind` `licence` |
| release/tag data | `RepoStats.latestRelease` (`Release`: tag, name, url, prerelease, `publishedAt`), `Repo.sections` freshness per section |
| badges (CI, security scan, shields-style, licence, repository count, fallback) | the badge routes are server-only (`x-scp-server-routes`; `image/svg+xml`); the API carries `badges[]` of `Badge {kind: BadgeKind, url: PortalPath, label}` on `RepoSummary`, `Repo` and `GroupSummary`, with `url` built from `routes`; a provider that is not configured is `ProviderState: disabled` (never `ok`, GR-41), and an unknown repository is a `404`, never a placeholder with a `200` (GR-16, GR-33) |
| Markdown and AsciiDoc rendering | `Doc.format` (`DocFormat`), `renderedBy: scp\|github` (`github` for AsciiDoc rendered through GitHub's renderer), sanitised `html`, `toc[]`, `frontMatter`, `normalised[]`; `markdownUrl` points at the raw variant |
| liveness, readiness and upstream health | probes are server-only routes; the readiness truth is `getSnapshot.readiness` (never 503, D-11) and the admin `getStatus` (gates, upstreams, jobs; derived from outcomes, GR-41); no thread dump route exists |
| build information and metrics | `getSite.build` (`version`, `commit`, `buildId`, `goVersion`) and `info.version`; metrics are the admin listener's own route, not the contract's (section 9 fixes the vocabulary) |
| request-reflection diagnostics, CORS, cache and task introspection, alternate JSON key spellings | not provided by construction: no route reflects a request or exposes internals (the route-table equality test fails on one, GR-17), `OPTIONS` is `405` and no CORS header exists (GR-18), and every response is a typed generated struct encoded by `encoding/json` (GR-11) |
| upstream "last seen" tracking | per-source `lastSuccessAt` (`NullableTimestamp`, `null` = never, never `1970-01-01`) in `StatusUpstream` and `StatusTask`, and `Freshness.fetchedAt`/`checkedAt` on every resource |
| latest-commit count limit | `commits.latest_n` bounds the dashboard feed; `limit` bounds any page |
| D-15 / N-01 package manifest intelligence | the eight `manifests` operations, CycloneDX 1.7 export |

### 11.2 Platform guardrails

| Guardrail | Principle | Realised by |
|---|---|---|
| GR-26 | freshness is data on every fetched item | the `Freshness` envelope (4.4), required on every cached-data response |
| GR-02 | readiness is gated on data | `getSnapshot.readiness` (`warming`/`ready`, `version: 0` while cold, never 503), `503 not-ready` + `Retry-After` for every other operation on a cold start, the `StatusReadiness` gates in `getStatus` |
| GR-40 | stale-while-error with a visible indicator | `200` with `status: stale` once any snapshot exists; the ETag digest keeps a stale transition visible through conditional GET (4.5) |
| GR-15 | one route table; links are generated | `x-scp-route` on every operation; route-table equality test both ways; `PortalPath` links built from `routes` |
| GR-17 | the management plane is separate | `x-scp-listener: admin` on `getStatus` and `retryJob` (A20), a separate admin bundle, `TestPublicBundleHasNoAdminOperation`, the route-table equality per listener |
| GR-35 | access is an explicit policy | the view class in every ETag and projection, `401 unauthorized` only when `access.mode` demands an identity, the `Unauthorized` response in `common.yaml` (4.8; unit 14 owns the mechanism) |
| GR-18 | browser security policy is strict by default | API response class headers (7.4), no CORS |

### 11.3 Contract guardrails

| Guardrail | Risk addressed | Prevented by |
|---|---|---|
| GR-15 | user-facing URLs drift from the handlers | one route name per operation checked against the bundled paths; every URL in a body is a `PortalPath` from `routes` |
| GR-17 | unauthenticated diagnostics (request echo, thread dump, per-request upstream calls) | none of them exists in either document; `getStatus` is admin-only and reads precomputed state; `retryJob` is admin-only, idempotent and gated by the budget |
| GR-18 | missing security headers, inline handlers, permissive CORS | the API response class headers `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; sandbox` and `nosniff` (7.4), header golden tests; JSON bodies carry no markup except `html` through one sink; no CORS headers, `OPTIONS` is `405`; the validator and mux answer problems, not `text/plain` |
| GR-35 | private content or personal data published; existence of private repositories revealed | view classes and the identical `404` (4.8), per-class ETags; no e-mail member, parameter or format in either document (A25) |
| GR-07, GR-08 | unstable key equality, mixed-case ids used as join keys | `GroupId` lower case only; `Slug` one segment; ids are strings with patterns; no composite key is exposed |
| GR-10 | a configured branch literal used as the Git ref | no branch in the API (Q-18); `Repo.defaultBranch` is reported, never configured through the contract |
| GR-26, GR-29 | data that never expires; a never-seen value rendered as `1970-01-01` | `Freshness` on every cached resource (`staleSince`, `lastErrorCode`, required members); every `lastSuccessAt`, `fetchedAt`, `staleSince` is `NullableTimestamp` and `null` means never (A22, A15) |
| GR-23 | unclassified rate limits and failures; no `Retry-After`, no ETag | `429 rate-limited` and `503` with `Retry-After` and `retryAfterSeconds` (both declared headers), stable `lastErrorCode` values (`rate_limited`, `not_found`: the closed vocabulary of `92-error-catalogue.md`), a weak `ETag` and `304` on every GET (A9) |
| GR-41, GR-11 | health that cannot signal failure; JSON built by more than one encoder | `getSnapshot`/`getStatus` derive from outcomes; `Repo.statuses[].state: disabled` for the unconfigured; one encoder (`encoding/json` via the generated `Visit` methods) and typed generated structs; the handler tests validate every response against the contract (12.3) |
| GR-11 | pages missing base fields, `Updated: null`, a group count shown as the repository count | `getSite.lastFullSync` and `repoCount`/`groupCount` are required members (null is explicit, `repoCount` is per view class); strict generated types make a missing required member a compile error |
| GR-16 | soft 404s, status codes that cannot fail, platform-default charsets | `404 not-found` problem for unknown group, repo, page, document; 2xx/4xx/5xx mean what HTTP says; `503` + `Retry-After` for saturation (4.10); UTF-8 only (4.1); the generated `Visit` methods set the declared media type on every response; the bundle is served `application/yaml` |
| GR-33 | a badge answering `200` with a placeholder for an unknown repository; unescaped SVG | `Badge.url` is a `PortalPath` from `routes`; the unknown repository is a `404` problem like every `{repo}` route; `Badge.label` is plain text (`alt`) |
| GR-38 | null commit dates, server-zone times without a label | `committedAt` required; all times UTC `Z`; `displayTimeZone` is a separate member; `authorLogin` nullable by contract |
| GR-37, GR-29 | dropped `README` variants; a cached `null` served as a blank `200` | `getRepoDoc` has no blank `200`: a missing document is `404 not-found`, an over-budget one is `state: not_resident` with no body, a failed render is in `problems[]`; `renderedBy` and `normalised[]` say what was done |
| GR-28 | O(N) reverse lookups and sorts per request | cursor keysets with total order; filters are indexed in the projections of the owning features |
| GR-40 | polling regardless of visibility, full HTML, silent non-2xx, no stale indicator | `getSnapshot` 304 polling (visibility-aware in the SPA), JSON, problems, `freshness` |
| GR-15 | router quirks (`GET //` as a 500, a trailing segment silently ignored) | exact `ServeMux` patterns, `ProblemMux` 404/405 as problems, the wire-form patterns of every path parameter, the dot-segment and `%2F` tests of the alias redirect (`TestVisibilityFirst`) |
| GR-39 | missing `alt` text, the literal string "null" in a page, empty `src=""` | `Badge.label` is required; an absent value is an absent member or an explicit `null` of the schema, never the string `"null"` (typed structs, A22) |
| GR-14 | hard-coded branding and limits | everything in `getSite` comes from `site:`; limits are parameters |

### 11.4 Design highlights

Conditional GET with a freshness-aware digest and a per-process epoch (no stale `304` across a restart); cursor pagination with filter-bound cursors; provenance on every resolved field; one error vocabulary with machine codes; `308` for renamed repositories with visibility first; a contract that generates handlers and the client; generated alias files that keep features inside the import matrix; completeness rules for untrusted text (A24) and for personal data (A25); a mock that lets the SPA and Schemathesis run before any handler exists; an automated additive-only gate.

---

## 12. Test plan

### 12.1 Layers

| Layer | What it proves | Where it runs |
|---|---|---|
| `apicheck` (27 rules, each with a failing mutation) | the documents obey the conventions and the subset | every PR, `tools/` module |
| Redocly lint + bundle | the documents are valid OpenAPI 3.1 and bundle | every PR |
| oapi-codegen + `go build/vet` + drift | the documents generate compilable Go; committed Go equals regenerated Go | every PR |
| route-table equality and mux registration | one pattern, one name, no `ServeMux` conflict, paths = bundled paths | `go test` |
| handler tests with `apitest` | each handler's responses satisfy the contract (status, headers, body schema, examples) | `go test`, per feature |
| request validator tests | HEAD, unknown keys, empty values, cursor limits, problem rendering | `go test`, `httpapi/validate` |
| contract mock + Schemathesis | the contract is self-consistent and its constraints are enforced by a server | every PR (mock), nightly extended (binary) |
| anonymous enumeration (e2e) | no route reveals a private repository, by name, content or count | e2e job |
| TypeScript proof | `schema.d.ts` + `openapi-fetch` type-check; a wrong call fails | web job |
| oasdiff | only additive changes inside v1 | every PR |
| SSE tests | stream framing, resync, drain, no leaks | `go test`, `synctest` |

### 12.2 `apicheck` rules (a failing rule fails the build)

`apicheck -trusted api/trusted-members.txt -public <file> -admin <file> [-catalogue file] [-owners file] [-bundled] [-no-examples]` loads both documents with kin-openapi (external refs allowed, examples validated) and reports `FAIL <rule>: <message>`; `INFO` lines are advisory; the exit status is 1 when any rule failed. `-bundled` skips the ownership rules that need the fragments; `-no-examples` is for the negative test of A17 only. A second command, `apicheck aliases -common common.yaml -gen common.gen.go -module <module> -pkg <feature>api`, writes `common_aliases.go` (section 1.4) and is part of the drift gate.

| Rule | Checks |
|---|---|
| A1 | the document loads and validates, every example validates against its schema |
| A2 | every path is under `/api/v1/` |
| A3 | `operationId` is camelCase and unique |
| A4 | exactly one tag per operation and it is declared |
| A5 | `x-scp-owner` (one of `overview live catalog commits docs manifests pages ops apispec`) and `x-scp-route` present, route names unique and well-formed; `x-scp-etag` present on conditional GETs and one of `snapshot-variant repo-rev tree-sha snapshot-version body-hash none` |
| A6 | `summary` and `description` present |
| A7 | every response of every operation has an example (media-type `example` or `examples`, or the named shared response's) |
| A8 | `default` is the shared `Problem` response |
| A9 | conditional GET rule (ADR-020): every GET declares `IfNoneMatch`, a `200` with an `ETag` header, a `304` and the default problem; exempt with `x-scp-conditional: false` |
| A10 | every `GET` with a `{repo}` path parameter declares `308` |
| A11 | every `{name}` in a path is a declared path parameter |
| A12 | a collection response (`items`) has `page` (`nextCursor`, `limit`), `freshness`, the `cursor` and `limit` parameters, and the `Link` header on its `200` |
| A13 | 3.1 subset (only `[T, "null"]`, no nullable object or array, no `nullable`, no `oneOf`/`anyOf` with null) |
| A14 | `allOf` is exactly one `$ref` plus one inline object |
| A15 | every timestamp is a `Timestamp` or `NullableTimestamp` reference; no raw `date-time`; `*At` members are references |
| A16 | property names are camelCase |
| A17 | every `required` entry is a property |
| A18 | a local schema is reached by one feature only; a common schema by more than one (INFO when a common schema has one user: `BuildInfo`, `ProviderState`, kept in common because the admin `Status` also uses them); a local schema never reuses the name of a common schema (the alias file would declare it twice) |
| A19 | no unreachable schema |
| A20 | the admin document is a strict superset of the public one (22 public + 2) |
| A21 | examples contain no member the schema does not declare (kin-openapi accepts extra members; this rule catches typos) |
| A22 | a nullable member is `required` |
| A23 | a string query parameter has `minLength`, `pattern` or `enum` |
| A24 | every free-text member (an inline string without `enum`, `pattern` or `format`, or an array of those) of every schema reachable from an operation, in either document, is marked `x-scp-untrusted` **or** listed in `trusted-members.txt`, never both; a listed entry that names no free-text member is stale (checked when both documents are given) |
| A25 | no e-mail address anywhere: no schema member or parameter whose name mentions e-mail, no `format: email` or `idn-email` (GR-35) |
| A26 | every string enum value is a lower-case word `[a-z0-9]+([_-][a-z0-9]+)*` (the vocabulary of 4.1) |
| A27 | `servers` is exactly the same-origin `/` |

State on 2026-10-14: `apicheck: 0 failures` on the public and admin design documents and on the redocly bundles of the split layout (`-bundled`). **Every rule has a failing mutation**: `go test ./apicheck` (`main_test.go`) copies the documents, applies 32 one-line mutations (for A24 the three failure modes: a missing mark, a marked member also on the allow-list, a stale allow-list entry; for A5 the three value sets) and asserts that the named rule fails, plus `TestBaselineIsClean`; a new rule must add its mutation to the table (review rule, T-3). Two rules were added late because the tools above found real defects: A22 (the generated `Freshness` struct omitted a `nil` `lastErrorCode` instead of writing `null`) and A23 (Schemathesis sent `tag=`; the schema said valid, the validator answered 400); A24 to A27 were added by the verification pass (an incomplete, hand-maintained `x-scp-untrusted` list; no e-mail rule although GR-35 requires personal data to be minimised; a mixed-case enum value would have been legal; an absolute `servers` URL). `tools/keycheck.py` reports every schema keyword outside an allow-list of the keywords the documents are exercised with (a typo such as `maxlength` is otherwise silently ignored by YAML consumers); it prints `unknown keys: 0` on all three documents and runs in the `api` CI job next to `apicheck`.

### 12.3 `httpapi/apitest` (specified here, implemented with the HTTP platform unit, Q-1)

Unit 06 owns the API of the package (`06-httpapi.md` 2.3): `Load(t, spec) *openapi3.T`, `Lint(doc) []string` (rules L1-L11, the same conventions as A2-A15 on the *bundled* document), `ValidateResponse(t, doc, req, rec)`, `ValidateProblem(t, doc, rec)`, `ETagCoversBody`, `Normalised`, `NoLeak`. An earlier draft of this unit specified a different shape (`apitest.Contract(t, spec, h) *Client`); the **behaviour** it asked for is kept and mapped onto unit 06's functions, which are authoritative for the signatures. What a handler test must get from them:

1. the operation is found by method and path (`HEAD` as `GET`); no match fails the test (an undeclared route) (`ValidateResponse`);
2. the status is declared in the operation's `responses` or `default` (`ValidateResponse`);
3. every declared header of that response is present with a valid value; `ETag` on a conditional `200`; a declared `Retry-After` or `Allow` on the statuses that say so (`ValidateResponse`; this needs the headers to be declared, which 7.4 now does for `Allow`, `Retry-After`, `X-Request-Id` on the shared problem);
4. the body validates against the schema of the actual content type (`openapi3filter.ValidateResponse`), including `null` versus absent and `[]` versus `null` (`ValidateResponse`);
5. conditional GET properties: replaying with `If-None-Match` set to the returned `ETag` yields `304`, same `ETag`, empty body; the tag changes when the body changes (`ETagCoversBody`); `HEAD` has the `GET`'s status and headers and no body; an invisible repository's response equals an unknown slug's byte for byte (`Normalised`, `NoLeak`);
6. problems: `Content-Type: application/problem+json`, `status` equals the HTTP status, `code` matches `^[a-z][a-z0-9-]*$` and is in the catalogue, `instance` equals `urn:uuid:<X-Request-Id>` (`ValidateProblem`);
7. **no zero time** (an addition this unit asks of 06, `sketch/apitest_zerotime.go`): `NoZeroTimes` fails a 2xx JSON body that contains `0001-01-01T00:00:00Z` (Go's zero `time.Time`) or `1970-01-01T00:00:00Z`; both match the `Timestamp` pattern, so response validation alone passes a handler that forgot to map a missing time to `null` (GR-29, GR-38). `ValidateResponse` calls it for every 2xx JSON body.

Every feature's handler test mounts `httpapi.WithRequest` (the handler needs `RequestFrom`), validates with `apispec.Public()`; `ops` uses `Admin()`. kin-openapi is imported from `_test.go` only (architecture 5.2).

### 12.4 Gates and unit tests

| Test | Assertion |
|---|---|
| `TestOpenAPIDriftBundles` | `redocly bundle` output equals the committed `internal/apispec/*.bundled.yaml` |
| `go generate ./... && git diff --exit-code` | every `*.gen.go` equals regeneration with oapi-codegen v2.8.0; `schema.d.ts` equals regeneration |
| `TestRouteTableEqualsBundledPaths` (`cmd/scp`) | the API class of `routes.Public()` equals the bundled public paths and `Admin()` the admin paths, both directions, with `x-scp-route` names **and `x-scp-owner` values** equal (proof of concept executed: 24 generated plus root patterns equal the 24 bundled operations, Appendix B); a table entry with another owner fails the test, which is how a unit that still lists `api.repos` under `catalog` is found |
| `TestMuxRegistration` | every `HandlerFromMux` registration of all eight packages goes through one `ServeMux` without a conflict (23 generated patterns plus the root-owned `openapi.yaml`, executed) |
| `TestAliasesDrift` (specified) | `apicheck aliases` output equals every committed `common_aliases.go`; no package outside `<feature>api` and `httpapi` imports `httpapi/openapi` (archtest; the executed form is the `go list` import check of `prove.sh`, which lists each api package's imports and the `live` feature's) |
| `TestPublicBundleHasNoAdminOperation` | `retryJob` and `/status` absent from `Public()`, present in `Admin()` (executed) |
| `TestHeadThroughValidator` | `HEAD` of every GET is validated as `GET` and answered 200/304, never 405 |
| `TestUnknownQueryKey`, `TestEmptyQueryValue`, `TestRepeatedScalar` | `?limt=1` is 400 with `#/query/limt`; `?tag=` and `?limit=` are 400; `?limit=5&limit=6` is 400 (the last two are requirements on unit 06's validator, D-6; the mock already behaves so) |
| `TestCursor*` | malformed, wrong version, other filter, over-long cursors are `invalid-cursor`; round trip |
| `TestIfNoneMatch` | list, `*`, weak/strong mix, malformed value |
| `TestRepeatedIfNoneMatchLines` | two header lines are one list: 304 when the current tag is on the second; the generated binder alone answers 400 (executed, `live_test.go`) |
| `TestTagDoesNotSurviveARestart` | a tag from another epoch never matches (executed) |
| `TestRenamedRepositoryRedirects`, `TestVisibilityFirst` | the 308 keeps path and query byte for byte, `HEAD` too; an invisible alias, a current slug, an unknown slug, dot segments, `%2F`, upper case and other routes fall through (executed, `alias_test.go`) |
| `TestZeroTimes` | a zero time in a body is found, a real time is not (executed, `apitest_zerotime_test.go`) |
| `TestAggregateFreshness`, `TestNonNil` | the rule of 4.4 and `NonNil` (executed, Appendix A); the digest is unit 06's `FreshnessDigest` and its tests |
| `TestTimestampsAreUTC` | a handler given a non-UTC `time.Time` still emits `Z` (property test over every response with a response validator) |
| `TestEnumsAreOpen` | the SPA's decoder and the Go client tolerate an unknown enum value |
| `TestStream` | `hello` first; a `Last-Event-ID` ahead of the process, including a 20-digit value that overflows `uint64`, is answered with `resync` right after `hello`; a malformed one is ignored (executed, `live_test.go`) |
| race and leak | `go test -race`; `goleak` in `TestMain`; the shared `*openapi3.T` is hammered by 64 goroutines |

### 12.5 Contract mock, Schemathesis, enumeration, SSE

- **Contract mock** (`tools/mock/main.go`, 176 lines, executed): serves the first example of every operation behind the request validator (HEAD as GET), answers `304` for a matching `If-None-Match`, a problem for unknown routes (404), wrong methods (405 with `Allow`) and invalid requests (400), including the three rules kin-openapi does not enforce (unknown query keys, empty values, repeated scalar parameters; 4.12). It lets the SPA (MSW fixtures are the same examples), Schemathesis and the TypeScript tests run before any handler exists.
- **Schemathesis 4.31.0** against the mock, 22 operations minus `streamEvents` (SSE) and `getOpenApiDocument` (it parses `application/yaml` to an object before checking the `type: string` schema, a tool limitation covered by a handler test instead): examples, coverage, fuzzing and stateful phases pass on seeds 1, 2 and 3 with 40 examples per operation, no failures (2026-10-14, re-run after the corrections of the verification pass). The first runs found the `tag=` defect (A23), a path-parameter `maxLength` that kin-openapi applied to the encoded segment (the wire-form schemas of 3.3) and the accepted empty `?limit=` (4.12). Against the real binary the nightly job runs the extended profile with the fixture snapshot (seeded).
- **Anonymous enumeration** (e2e): for **every** route of `routes.Public()` (API and server-only), with a fixture of N public and M private repositories and `access.mode: trusted-proxy`, an anonymous caller's response is byte-identical to a deployment with only the N; no body, header, ETag, cursor or `Link` contains a private slug or count; the same for `HEAD` and `If-None-Match`.
- **SSE**: with `testing/synctest` and a fake publisher: framing and `retry`; `hello` first; debounce into one `invalidate` per generation; `Last-Event-ID` behind the ring gives `resync`; heartbeat every 15 s; drain sends `retry: 30000` and returns every stream; a slow client does not block the publisher; no goroutine survives; the 5th stream from one IP is `429`; flag off is `404 feature-disabled`.

### 12.6 What was executed on 2026-10-14 (verification log of this unit)

Machine: darwin/arm64, Go 1.27.1, Node/npx current. All commands are in `07-openapi-contract.assets/`. `tools/prove.sh [scratch]` repeats steps 1-7 and 13 from nothing in two stages: **stage 1** needs only this unit (split, lint, bundle, generate, aliases, build, vet, test, route comparison, import check); **stage 2** overlays the generated packages on the real unit 06 prototype (`06-httpapi.assets`) with the additions of this unit and runs `go test -race` over the whole tree. The first author's run used a scratch `build.sh` that is not in the repository; the verification pass replaced it with `prove.sh` and re-executed everything below (run log in the findings report).

| Step | Tool and version | Result |
|---|---|---|
| 1 load, validate, examples, rules | kin-openapi v0.150.0 (`apicheck`), `keycheck.py` | public 22 paths and admin 24 paths valid, `apicheck: 0 failures` (27 rules), `unknown keys: 0`; `go test ./apicheck`: baseline clean and 32 mutations each fail their rule |
| 2 split | `tools/split.py` | 8 fragments, `common.yaml`, two roots, `trusted-members.txt`, in the ADR-020 layout |
| 3 lint | Redocly CLI 2.59.0 (`npx`; pinned) | `Your API description is valid` for both roots; the newest release on npm that day (2.63.0) validates the admin root too and produces a byte-identical bundle |
| 4 bundle | Redocly CLI 2.59.0 | 4,854 and 5,588 lines; `apicheck -bundled` passes on both |
| 5 generate | oapi-codegen v2.8.0, `runtime` v1.7.0, models + std-http-server + strict-server, import-mapping, `always-prefix-enum-values` | 9 files, 11,298 lines, plus 8 alias files (78 lines each) from `apicheck aliases`; `gofmt -l` clean; `go build ./...`, `go vet ./...` and `go test ./...` pass |
| 6 register and compare | Go test | 23 generated patterns register on one `ServeMux` without conflict; with the root-owned path they equal the 24 bundled operations in both directions |
| 7 import matrix | `go list` | every `<feature>api` imports `httpapi/openapi` only; `internal/live` (stage 2) imports `httpapi` and `liveapi` only, never `httpapi/openapi` |
| 8 feature sketches on the unit 06 tree | Go 1.27.1 `go test -race`, 11 packages | conditional GET `200`/`304`/stale and foreign-epoch validator, repeated `If-None-Match` lines, `HEAD`, SSE `hello` and `resync` (ahead, overflow, malformed), responses validated by unit 06's `apitest` against the real admin bundle and its `Lint` L1-L11, aggregate freshness, `NonNil`, alias redirect (308 and fall-through cases), zero-time finder: pass |
| 9 validator | `nethttp-middleware` v1.2.0 | `GET` 200; **`HEAD` 405 `text/plain`**; `limit=5000` is a plain-text 400 (hence `ErrorHandlerWithOpts`, `Skipper` exist and D-6 applies); repeated `If-None-Match` lines are accepted by the validator (verified against the mock) and rejected by the generated binder |
| 10 mock + Schemathesis | Schemathesis 4.31.0 (`uvx`) | pass on three seeds (12.5) |
| 11 TypeScript | openapi-typescript 7.13.0, openapi-fetch 0.17.0, `tsc` 7.0.2 (`latest`) | `schema.d.ts` 4,137 lines from the admin bundle; `use.ts` type-checks under strict, including the negative `@ts-expect-error` case |
| 12 diff | oasdiff v1.33.0 | an additive change (a new enum value on `FreshnessStatus`) gives 25 errors and exit 1 without the severity file and exit 0 with it; a removed path exits 1 with `api path removed without deprecation` (4.9) |
| 13 load time | kin-openapi + gorillamux | 28 ms public, 24 ms admin |

### 12.7 CI jobs

`api` job (architecture 22): `redocly lint`, `redocly bundle` + drift, `apicheck` (rules, `go test ./apicheck`, `keycheck.py`), `apicheck aliases` + drift, `go generate` + `git diff --exit-code`, `oasdiff breaking` vs the base release bundle, mock + Schemathesis. `web` job: `pnpm gen` drift. `go` job: the tests of 12.4. nightly: Schemathesis extended against the binary. Versions pinned in `tools/go.mod` and `web/package.json`.

---

## 13. Implementation tasks and milestones

Sizes: S under a day, M 1-3 days, L 3-5 days. The contract is **contract-first**: M0 exists now (this design); handlers arrive with their features; nothing blocks on handlers because the mock serves the examples.

| Task | Milestone | Size | Description and acceptance |
|---|---|---|---|
| T-1 | M0 | S | Create `api/`, `internal/httpapi/openapi/`, `internal/apispec/`, the eight `<feature>api` directories from the design files (run `split.py` once, then it is never used again); `oapi.yaml` per package (with `always-prefix-enum-values`); `go generate` directives including `apicheck aliases` for every `common_aliases.go`; acceptance: `go build ./...`, committed `*.gen.go` and `common_aliases.go` equal regeneration, no package imports `httpapi/openapi` except `httpapi` and the `<feature>api` packages |
| T-2 | M0 | S | Redocly config, bundle step into `internal/apispec`, `apispec.go` with the two accessors, drift gate; acceptance: Appendix A `TestEmbedded` |
| T-3 | M0 | M | `tools/apicheck` and `tools/mock` (move from the assets: 27 rules, `catalogue`, `aliases`, `main_test.go` with 32 mutations, `keycheck.py`) in the `tools/` module, plus `api/trusted-members.txt`; acceptance: `apicheck: 0 failures` on the committed documents; every rule has a failing mutation; a new free-text member fails A24 until it is marked or listed with a reason |
| T-4 | M0 | S | `routes` table entries for all 24 operations (the 5 split manifests names, D-7; `api.repos` and `api.group.repos` owned by `overview`, D-2; `api.openapi` owned by `apispec`), with the unit 01 table amended accordingly; route-table equality test (names and owners) and `TestMuxRegistration`; acceptance: both pass |
| T-5 | M1 | M | the three `httpapi` additions of 2.3.3 (`AggregateFreshness`, `NonNil`, the `If-None-Match` fold inside `WithRequest`; with the HTTP platform unit) and the `httpapi/validate` rules of 4.6 and 4.12 (HEAD as GET, unknown keys, **empty values for every parameter type**, repeated scalars, Accept, problem rendering with `ErrorHandlerWithOpts`); acceptance: `TestHeadThroughValidator`, `TestEmptyQueryValue`, `TestRepeatedIfNoneMatchLines` |
| T-6 | M1 | M | `httpapi/apitest` of 12.3 (unit 06's functions plus `NoZeroTimes`) and its use by the first two features (`live`, `ops`); acceptance: a deliberately wrong handler fails each of the seven checks |
| T-7 | M1 | S | contract mock subcommand in CI and MSW fixtures generated from the examples; acceptance: the SPA's first page runs against the mock |
| T-8 | M1 | S | `web/src/lib/api`: `schema.d.ts` generation from the admin bundle, `openapi-fetch` client, drift gate, TS proof as a type test |
| T-9 | M2 | M | `overview.AliasRedirect` and `AliasSource` of 4.7 (from `sketch/alias.go`) with the catalogue unit's `catalog.aliases` slot and `authz.LookupRepo` (visibility first); acceptance: `TestRenamedRepositoryRedirects` and `TestVisibilityFirst` on the real slots, 308 follows for a public repo, 404 for an invisible one and for `listCommits?repo=<alias>` |
| T-10 | M2 | M | `getSnapshot` and `streamEvents` in `live` against the contract; SSE tests of 12.5; behind `live.sse.enabled` (v1.1) |
| T-11 | M3 | S | measure response sizes and gzip ratios at 212 and 1,000 repositories, record in 5, adjust the bounds only if a budget is missed |
| T-12 | M3 | S | `apicheck catalogue` generating `docs/reference/api.md` and its drift gate; `oasdiff` job with the severity file and changelog output |
| T-13 | each feature | per feature | implement the strict interface of the feature's fragment; handler tests with `apitest`; the feature's design says how |
| T-14 | M4 | M | anonymous enumeration e2e over the route table (12.5), extended to counts |
| T-15 | M4 | S | deprecation middleware and `scp_api_deprecated_requests_total` (registered only when something is deprecated); test with a synthetic deprecated operation |
| T-16 | M1 | S | the process epoch: `readmodel` exposes `Snapshot.Epoch()` (a random token chosen once in `cmd/scp` before the publisher starts, unit 03); `live`, `overview`, `docs`, `manifests` build every weak tag with it (`WeakETagOf(kind, epoch, ...)`); acceptance: `TestTagDoesNotSurviveARestart` in each feature that builds a tag |
| T-17 | M1 | S | docs unit: the injective `docId` encoding of 3.3 (`/` to `~`, other bytes outside `[A-Za-z0-9._-]` to `%XX`), diagnostics `doc.id-too-long` (over 1,024 characters); acceptance: a round-trip property test over the repositories of the fixture and over random byte strings |

---

## 14. Deviations proposed

Each deviation is an amendment request against `10-architecture.md` or an ADR; none changes a decision in `04-decisions.md`.

| ID | Architecture says | This design does | Why |
|---|---|---|---|
| D-1 | 11.2 lists only `api.status` and `api.admin.retry` on the admin listener; ADR-020 says the admin root is "public + ops admin operations" | the admin document is a **superset**: the 22 public operations plus 2; the same handler objects are mounted on the admin mux, anonymous principal, public view class; `api.openapi` there serves the admin bundle | the SPA bundle serves `/status` and needs `getSite`/`getSnapshot`; one `schema.d.ts` from the admin bundle; ADR-020's own sentence |
| D-2 | 11.2 assigns `api.repos` and `api.group.repos` to `catalog`; unit 01's route table (`01-kernel.routes.tsv`) repeats it; unit 01 also lists `httpapi` as owner of `api.openapi` | `api.repos` and `api.group.repos` are owned by `overview` (`RepoSummary` carries descriptor-resolved fields); `catalog` keeps `api.groups`, `api.group`; `api.openapi` is owned by `apispec`; the `{repo}` alias redirect is **one middleware in `overview`** (which architecture 4.4 and 9.1 already say), not six handler copies | rule G7: `catalog` cannot import `overviewapi`, and the summary needs `overview`'s schemas. The route-table test compares owners (12.4), so unit 01 must adopt the three owners or the test fails; an earlier draft of this unit put the redirect in `authz`, which cannot read a feature's slots (withdrawn) |
| D-3 | 11.2: `GET /api/v1/repos/{repo}/docs/{id...}` | `GET /api/v1/repos/{repo}/docs/{docId}` with `docId` = `readme` or the path with `/` replaced by `~` and every other byte outside `[A-Za-z0-9._-]` (a literal `~` and `%` included, every UTF-8 byte of a non-ASCII name) written `%XX`; the encoding is injective and at most 1,024 characters (docs diagnostic `doc.id-too-long`) | OpenAPI path templating cannot express `{id...}` and kin-openapi does not route it (verified); `~` is legal in a segment and survives proxies, unlike `%2F` which routes in kin-openapi but is mangled by some proxies. The first draft's rule (`/` to `~`, skip paths that contain `~`) was not injective for `%`, spaces and non-ASCII names and silently dropped real files; repositories routinely contain files with spaces or non-ASCII names |
| D-4 | 11.4 and ADR-009: weak ETag `W/"g<version>-<variant>"`, per-repo resources use `Rev`, `W/"v<version>"` for the snapshot endpoint | (a) the variant is the view-class ordinal plus an optional `.<digest>` of the contributing freshness statuses, present only while an item is not fresh; (b) `repo-rev` is used only by bodies that are a pure function of one repository entry (`listRepoDocs`, `getRepoDoc`, `getRepoManifests`); `getRepo` and `listRepoDependencies` join other repositories and use `snapshot-variant`; (c) the process **epoch** is part of every weak tag (D-13) | freshness status is time-derived, so a snapshot-only tag answers `304` after data went stale; `Rev` of one repository cannot cover joined data |
| D-5 | 8.14 `httpapi`; unit 06 `httpapi` (2.2) | unit 06 already supplies everything a strict handler needs (`WithRequest`/`RequestFrom`, `NotModified`, `StrictErrors`, `BindingError`, `FreshnessView`, `FreshnessDigest`, `WithDigest`). Three additions are requested (2.3.3): `AggregateFreshness(kind, ...FreshnessView)`, `NonNil[T]`, and the fold of repeated `If-None-Match` lines as the first step of `WithRequest`. The first draft's `IfNoneMatch`, `RequestError`, `ResponseError` and `FreshnessSource` are withdrawn | the freshness aggregate must be written once (4.4); a nil slice marshals as `null`; the generated header binder rejects a legal two-line `If-None-Match` with a 400 (verified, `TestRepeatedIfNoneMatchLines`; unit 06 4.17 records the 400 as accepted for generated handlers, this unit asks to remove it because proxies and HTTP libraries split lists over lines) |
| D-6 | 11.1: `ProblemMux` and "the OpenAPI request validator" under `/api/` | `httpapi/validate` additionally validates `HEAD` as `GET`, refuses unknown query keys, repeated scalar values and **empty values of every parameter type**, checks `Accept`, and renders errors through `ErrorHandlerWithOpts` | `nethttp-middleware` v1.2.0 answers `HEAD` with `405 text/plain` (verified 2026-10-14) although `ServeMux` routes it; kin-openapi ignores unknown query keys and reads `?limit=` as absent (Schemathesis found it against the mock); unit 06 implements the first two, the empty-value rule is still to add |
| D-7 | 11.2: `api.manifests.graph` for three paths, `api.manifests.component` for two | five names: `api.manifests.graph`, `.versions`, `.licenses`, `.component`, `.dependents` | one pattern maps to one name in the recording mux |
| D-8 | ADR-020: additive evolution enforced by `oasdiff breaking` | `oasdiff breaking --flatten-allof --severity-levels <file> --fail-on ERR` with `response-property-enum-value-added  info` | oasdiff's default marks a new response enum value an error (25 errors for one value on `FreshnessStatus`); the contract defines enums as open |
| D-9 | 11.4 provenance: eight sources | ten: `github` and `config` added | an owner or default taken straight from the GitHub API, and a value set by group configuration, are neither `derived` nor `own` |
| D-10 | 11.4: `freshness: { status, asOf, fetchedAt, staleSince?, lastErrorCode? }` | all members required, `null` when unknown; rule A22 for every nullable member | optional nullable members generate `omitempty`, so `null` could not be emitted; one shape for the SPA |
| D-11 | 11.4: cold start answers 503 `not-ready`; ADR-009 snapshot `{version, builtAt, readiness}` | `getSnapshot` never answers 503 (`readiness: warming`, version 0) and adds `buildId` | the SPA's poller must work during warm-up; the skew check needs the build id without another request |
| D-12 | ADR-020 custom check: every GET declares `If-None-Match`, `200` with `ETag`, `304`, default problem | `streamEvents` is exempt (`x-scp-conditional: false`) | an event stream has no entity tag; it is the only exemption and the checker lists it |
| D-13 | ADR-009 and 11.4: `W/"v<version>"`, `W/"g<version>-<variant>"`; `Snapshot.version` is the change detector of the SPA | every weak tag starts with a per-process **epoch** token (`W/"g<epoch>-<version>-<class>"`, `W/"v<epoch>-<version>"`), `readmodel` provides `Snapshot.Epoch()` (Q-13), the SPA compares versions for inequality and the SSE stream answers `resync` to a `Last-Event-ID` ahead of the process | unit 03 4.6: warm start publishes snapshot version 1, so versions restart at 1 after every restart; `W/"v1"` then names two different bodies and a browser that cached the tag would be told `304`, and a poller holding version 1841 would never see version 3 as a change (executed: `TestTagDoesNotSurviveARestart`, `TestStream`) |
| D-14 | unit 06 `WriteProblem(w, r, p)`: status clamped to `400..599`, header allow-list `Allow`, `Retry-After`, `WWW-Authenticate` | `overview.AliasRedirect` writes the `308` itself (problem body, `Location`, `Cache-Control: no-store`) | `WriteProblem` would turn a `308` into a 500-class answer and drop `Location`; an alternative is to add `Location` to the allow-list and let status 308 through, which is unit 06's call (Q-14) |

Additions without conflict (recorded for completeness): `getSite.features`, `groupCount`, `build` (Q-5); `x-scp-*` extensions; the rejection of unknown query keys (D-6 covers the mechanism); the shared `Problem` response declares `Allow`, `Retry-After` and `X-Request-Id` (7.4); the generated `common_aliases.go` files (1.4), which are how the import matrix of 5.2 and the generated strict code fit together without a matrix change.

---

## 15. Open questions

| ID | Question | Proposed answer | Owner |
|---|---|---|---|
| Q-1 | Who owns `httpapi/apitest` and the additions of 2.3.3 (HTTP platform unit or this one)? | the HTTP platform unit implements them from the specs here; this unit supplies the contract tests | architecture, HTTP platform unit |
| Q-2 | `oapi-codegen/runtime` v1.7.0 pulls `apapsch/go-jsonmerge/v2` v2.0.0 and `google/uuid` v1.6.0 into the production binary (verified with `go list -deps` on the split layout, 2026-10-14). Principle 5 "stdlib first": accept? | accept, pin and list in Appendix A of the architecture; the alternative is hand-binding every parameter | architecture |
| Q-3 | the freshness kernel must give `Status.String()` returning exactly `fresh`, `stale`, `error`, `unknown`, so features map without a table | request to the freshness/kernel unit | kernel |
| Q-4 | *(closed)* the signature of the `{repo}` alias middleware was compile-checked against the kernel types and the unit 06 tree (`sketch/alias.go`, 4.7); it lives in `overview` | the catalogue unit supplies the `catalog.aliases` slot as `map[scm.Slug]scm.Slug` (08, 9.1 of the architecture) | catalog |
| Q-5 | `getSite.features` (`manifests`, `sse`, `integrations[]`) and `groupCount` are not in architecture 11.2's `api.site` list; keep? | keep: the SPA must not render links to disabled features | overview |
| Q-6 | can `logx` carry per-request attributes set after the access log middleware wrapped the request (`operation`, `problem_code`)? | yes if it holds a mutable attribute bag; else drop the two fields | platform |
| Q-7 | the docs unit implements the injective `docId` encoding of D-3 (T-17) and `doc.id-too-long`; a path whose id exceeds 1,024 characters is the only one that is not indexed | confirm in the docs design | docs |
| Q-8 | `getSnapshot.version` is one global counter that also advances for private repositories (a timing side channel: "something changed") | accept for v1; per-class versions if the threat model asks | security |
| Q-9 | SSE when the flag is off: keep the operation in both documents answering `404 feature-disabled` (chosen) or omit it from the bundle? | keep: one stable route table and one generated type | live |
| Q-10 | response sizes: `listRepos` at 1,000 repositories is about 1.6 MB uncompressed (fixture arithmetic); is that acceptable for the dashboard, or should the SPA page it with `limit`? | measure in T-11 before M3; the SPA may page without a contract change | frontend |
| Q-11 | Provenance values `github` and `config` (D-9) need a precedence position in 14.2 | docs/overview design | docs |
| Q-13 | `Snapshot.Epoch()` (D-13): who generates the token and where does it live? | `cmd/scp` generates 8 random bytes (hex) before the publisher starts and hands it to `readmodel.NewPublisher`; unit 03 exposes it on the snapshot; features read it from the snapshot they loaded | readmodel (unit 03) |
| Q-14 | the `308` and its `Location` header (D-14): extend `WriteProblem` (status 308 and `Location` in the allow-list) or keep the middleware's own writer? | keep the own writer: it is 30 lines, tested, and `WriteProblem` stays a pure error writer | httpapi (unit 06) |
| Q-15 | unit 09 (commits and badges) proposes in its D-12 that `commits` imports `httpapi/openapi`, an exception to the matrix of 5.2 | withdraw it: `commitsapi/common_aliases.go` already exports `Commit` and every other common type (`type Commit = openapi.Commit`), so `commits` writes `commitsapi.Commit` and `overview` writes `overviewapi.Commit` (G7: each feature names the generated package of its own) and the matrix stays as written (proved by the `go list` check of `prove.sh` on `internal/live`, the same shape) | commits |
| Q-12 | `info.version` is `1.0.0` here; the SSE operation is marked `x-scp-since: '1.1'` though present at 1.0.0 behind a flag: which version number does the first release carry? | release engineering decides; the document is stable either way | release |

---

## Appendix A. Compile-checked code (Go 1.27.1, executed 2026-10-14)

Files in `07-openapi-contract.assets/sketch/`; `tools/prove.sh` (stage 2) copies them into the real unit 06 tree beside the generated packages and runs `go build ./...`, `go vet ./...` and `go test -race ./...` over all 11 packages with tests:

| File | Package it joins | What it is |
|---|---|---|
| `httpapi_additions.go`, `_test.go` | `internal/httpapi` | `AggregateFreshness`, `NonNil`, `FoldIfNoneMatch` (2.3.3); `TestAggregateFreshness`, `TestNonNil` |
| `live.go`, `live_test.go` | `internal/live` | a complete feature on the generated strict server, imports `httpapi` and `liveapi` only (below); `TestConditionalGetHeadAndSSE`, `TestTagDoesNotSurviveARestart`, `TestRepeatedIfNoneMatchLines`, `TestStream`, `TestResponsesSatisfyTheContract` |
| `alias.go`, `alias_test.go` | `internal/overview` | `AliasLookup`, `AliasSource`, `AliasRedirect` (4.7); `TestRenamedRepositoryRedirects`, `TestVisibilityFirst` |
| `apitest_zerotime.go`, `_test.go` | `internal/httpapi/apitest` | `ZeroTimes`, `NoZeroTimes` (12.3 check 7); `TestZeroTimes` |
| `apispec.go`, `_test.go`, `mux_test.go` | `internal/apispec`, repository root | the two embedded bundles; registration of all eight generated handlers on one `ServeMux`; `TestEmbedded`, `TestRegistrationNoConflict` |

The feature sketch (`live.go`, 89 lines). Note what it does not import: `httpapi/openapi`; every contract type it names (`liveapi.Snapshot`, `liveapi.NotModifiedResponseHeaders`, `liveapi.SnapshotReadinessReady`) is in `liveapi` or in its generated alias file.

```go
// Package live is a compile-check sketch of one feature mounting its generated strict server (design 07, sections 2.3.4
// and 7.6). It imports exactly what the architecture matrix gives rank 4 `live`: httpapi and its own liveapi. It does NOT
// import httpapi/openapi: every contract type it needs is an alias in liveapi/common_aliases.go (apicheck aliases).
package live

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/live/liveapi"
)

// SnapshotSource is the consumer-defined interface (architecture 8.7): live asks only for what it uses. The numeric types are
// those of the kernel (readmodel.Snapshot.Version is a uint64). Epoch is a random token that changes at every process start:
// snapshot versions restart at 1 on a warm start (unit 03), so a version alone is not a stable validator across restarts.
type SnapshotSource interface {
	Version() uint64
	BuiltAt() time.Time
	Ready() bool
	Epoch() string
}

type Service struct {
	Snap    SnapshotSource
	BuildID string
	Log     *slog.Logger
}

var _ httpapi.Mounter = (*Service)(nil)
var _ liveapi.StrictServerInterface = (*Service)(nil)

func (s *Service) Mount(m httpapi.Mux) {
	req, resp := httpapi.StrictErrors(s.Log)
	h := liveapi.NewStrictHandlerWithOptions(s, nil, liveapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: req, ResponseErrorHandlerFunc: resp})
	liveapi.HandlerWithOptions(h, liveapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}

func (s *Service) GetSnapshot(ctx context.Context, _ liveapi.GetSnapshotRequestObject) (liveapi.GetSnapshotResponseObject, error) {
	v := s.Snap.Version()
	// The tag is computed before the body and names the process (epoch) as well as the version: W/"v<epoch>-<version>".
	etag := httpapi.WeakETagOf(httpapi.ETagVersion, s.Snap.Epoch(), v)
	if httpapi.NotModified(httpapi.RequestFrom(ctx), etag) {
		return liveapi.GetSnapshot304Response{Headers: liveapi.NotModifiedResponseHeaders{ETag: &etag}}, nil
	}
	ready := liveapi.SnapshotReadinessWarming
	if s.Snap.Ready() {
		ready = liveapi.SnapshotReadinessReady
	}
	return liveapi.GetSnapshot200JSONResponse{
		Body:    liveapi.Snapshot{Version: int64(v), BuiltAt: s.Snap.BuiltAt().UTC(), Readiness: ready, BuildId: s.BuildID},
		Headers: liveapi.GetSnapshot200ResponseHeaders{ETag: &etag},
	}, nil
}

// sse is the hand-written response type: a strict handler accepts any value that has the Visit method of the operation, so a
// stream does not need the generated io.Reader copy loop (which cannot select on shutdown).
type sse struct {
	run func(w http.ResponseWriter) error
}

func (e sse) VisitStreamEventsResponse(w http.ResponseWriter) error { return e.run(w) }

func (s *Service) StreamEvents(_ context.Context, req liveapi.StreamEventsRequestObject) (liveapi.StreamEventsResponseObject, error) {
	last := httpapi.Deref(req.Params.LastEventID)
	return sse{run: func(w http.ResponseWriter) error {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		v := s.Snap.Version()
		_, err := io.WriteString(w, fmt.Sprintf("retry: 3000\n\nid: %d\nevent: hello\ndata: {\"generation\":%d,\"buildId\":%q}\n\n", v, v, s.BuildID))
		// A client whose Last-Event-ID is ahead of this process (a restart: versions restart at 1) or older than the change ring
		// must invalidate everything: the server answers resync right after hello (design 7.6).
		// A value too large for uint64 (the pattern allows 20 digits) is ahead of any real version: ParseUint reports ErrRange.
		if n, perr := strconv.ParseUint(last, 10, 64); (perr == nil && n > v || errors.Is(perr, strconv.ErrRange)) && err == nil {
			_, err = io.WriteString(w, fmt.Sprintf("id: %d\nevent: resync\ndata: {\"generation\":%d}\n\n", v, v))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return err
	}}, nil
}
```

## Appendix B. External facts verified on 2026-10-14 and what stays unverified

| Fact | Evidence | Status |
|---|---|---|
| oapi-codegen v2.8.0 generates strict std-http servers from the 3.1 documents; 3.1 support is initial | generation of 9 files, `go build`, `go vet`; release notes at github.com/oapi-codegen/oapi-codegen/releases | verified |
| generated primitives are aliases (`type Timestamp = time.Time`); optional nullable properties are `*T` with `omitempty`; required nullable are `*T` without | `common.gen.go` read | verified |
| `GET` pattern also routes `HEAD`; the validator does not | `TestConditionalGetHeadAndSSE`; validator test | verified |
| `{id...}` is not routable in kin-openapi; `%2F` in a segment routes | spike, earlier in this design | verified |
| `nethttp-middleware` v1.2.0 has `ErrorHandlerWithOpts`, `Skipper`, `Prefix` | source read | verified |
| RFC 9745 `Deprecation` header uses a structured-field date (`@<unix>`), Standards Track | rfc-editor.org | verified |
| OpenAPI 3.1.2 and 3.2.1 are the latest published; 3.2.0 adds `itemSchema` for sequential media types | spec.openapis.org | verified |
| TypeScript 7.0.2 is `latest` on npm; `schema.d.ts` type-checks under it | `npm view`, `tsc` | verified |
| oasdiff v1.33.0 flags a new response enum value as an error by default (25 errors for one value on the shared `FreshnessStatus`); `--severity-levels` with `response-property-enum-value-added  info` exits 0; a removed path exits 1 | run twice (author, verification pass) | verified |
| kin-openapi v0.150.0 gorillamux matches and validates the **percent-encoded** path (`UseEncodedPath`), so a path parameter schema sees `%25`, not `%` | source read; Schemathesis sent a maximum-length id that a decoded-form `maxLength` rejected | verified |
| the oapi-codegen v2.8.0 header binder accepts exactly one header line (`TooManyValuesForParamError`, 400); kin-openapi's validator accepts repeated lines | generated `live.gen.go` read; `TestRepeatedIfNoneMatchLines`; request to the mock with two lines answered 200 | verified |
| without `always-prefix-enum-values` an enum constant is named by its bare value (`Critical`, `Dark`, `Public`, `Internal` in `overviewapi`) and takes a type prefix only when another enum of the package declares the same word, so adding an enum renames existing constants | `overviewapi` generated with and without the option and the constant lists compared | verified |
| kin-openapi reads an empty query value (`?limit=`) as absent and accepts repeated scalars and unknown keys | Schemathesis against a mock without the extra checks; mock now enforces them | verified |
| unit 06 `WriteProblem` clamps the status to `400..599` and its header allow-list has no `Location` | `06-httpapi.md` section 4.x and prototype source read | verified (unit 06 text, a moving target) |
| RFC 9457 problem details, RFC 8288 `Link`, RFC 8594 `Sunset`, RFC 9110 conditional requests | standards named in the architecture | used as published, not re-read for this design |
| real-binary behaviour of Schemathesis, response sizes, gzip ratios | no binary exists yet | unverified; tasks T-11 and the nightly job |

---

## Verification log (independent pass, 2026-10-14)

Verifier: independent of the author. Full findings with evidence: `30-verification/design-07-openapi-contract.md`. Method: the contract was loaded, bundled, generated, compiled and exercised from the committed files with `tools/prove.sh` (two stages, second stage on the real unit 06 prototype with `go test -race`), apicheck mutation tests, Schemathesis 4.31.0 on three seeds, oasdiff v1.33.0 experiments, the TypeScript proof under `tsc` 7.0.2, plus a read of architecture 5.2, 8.13, 8.14, 11, ADR-009 and ADR-020 and units 01, 03, 06, 08 and 09.

What changed because of it (each item is in the findings report with severity and evidence):

| Area | Change in this document and its companions |
|---|---|
| Import matrix (1.4, 2.1, 2.3) | features no longer import `httpapi/openapi`; `apicheck aliases` generates `common_aliases.go`; `always-prefix-enum-values` set; `httpapi` surface reconciled with unit 06 (`WithRequest`/`RequestFrom`, `NotModified`, `StrictErrors`, `BindingError`, `FreshnessView`); `IfNoneMatch`, `RequestError`, `ResponseError`, `FreshnessSource` withdrawn; three additions remain (`AggregateFreshness`, `NonNil`, header fold) |
| Entity tags (4.5, D-13) | process epoch in every weak tag; digest is 64-bit; tree-sha tag is weak and carries the epoch; repeated `If-None-Match` lines folded; SSE `resync` for a `Last-Event-ID` ahead of the process, overflow included |
| Identifiers (3.3, D-3) | `Slug` and `GroupId` equal the kernel grammar; `DocId` is an injective percent-encoding; path parameters are wire-form schemas |
| Aliases (4.7, D-2, D-14) | the 308 middleware is `overview.AliasRedirect` (compile-checked), visibility first, current slug wins, `Location` from the escaped path, written by the middleware; a query alias is a 404 |
| Untrusted text and personal data (3.5, 4.8, A24-A25) | 64 members marked (was 24 and incomplete), 54 allow-listed with a reason, rules A24 to A27 with mutations |
| Requests (4.12, D-6) | empty values, unknown keys and repeated scalars are 400 (mock implements all three; unit 06 owes the empty-value rule) |
| Headers (7.4) | `Allow`, `Retry-After`, `X-Request-Id` declared on the shared `Problem` response |
| Tests and proof (12) | 27 rules, 32 mutations, route-table owners compared, apitest mapped to unit 06 plus a zero-time check, executed results replaced by a reproducible two-stage script |
| Trace and counts (11, 1.4, 5) | the guardrail and capability trace of section 11 completed with mechanisms; line counts, schema counts and bundle sizes recomputed |

Open items that need another unit (not blocking this contract, all listed in sections 14 and 15): unit 01 route-table owners (D-2), unit 03 `Snapshot.Epoch()` (Q-13), unit 06 header fold, empty-value rule and 308 writer (D-5, D-6, D-14, Q-14), unit 09 withdrawing its D-12 (Q-15), docs unit `docId` encoding (T-17, Q-7), kernel `Status.String()` (Q-3).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `x-scp-route` and `x-scp-owner` equal the route table (22 public and 2 admin operations; verified); `listRepos` and `listGroupRepos` are owned by `overview` (CD-RT-01).
- Changed: closed `ErrCode` vocabulary text and examples (CD-ER-01); new catalogue rows `unhealthy`, `overloaded`, `request-timeout` (CD-ER-02); `RetryJobRequest.key` maxLength 512 (CD-JB-02); `docInstall` uiUrl without extension (CD-RT-04).
- Open: Q-15 asks commits to use `commitsapi` aliases; the matrix cell for `httpapi/openapi` is the interim decision (A-30).
