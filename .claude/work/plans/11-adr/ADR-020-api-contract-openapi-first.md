# ADR-020: API contract: OpenAPI 3.1 spec-first, generated on both sides

Status: Accepted. Date: 2026-10-14. Implements 04 D-16 (API-first) and amends D-14 (generated source is committed) (A-16, A-14).

## Context
D-16: OpenAPI 3.1 is the source of truth; handlers verified by contract tests or generated stubs; TypeScript client generated; RFC 9457 errors; fixed pagination, ETag and naming conventions; the JSON API is also the LLM-facing structured surface. D-14: no committed generated assets.

## Decision
- Fragments per feature (`internal/<feature>/<feature>api/<feature>.yaml`; generated Go package named after its directory, so no import aliases) plus `internal/httpapi/openapi/common.yaml`; roots `api/openapi.yaml` (public) and `api/openapi.admin.yaml`; `redocly bundle` writes `internal/apispec/*.bundled.yaml` (embedded; served at `/api/v1/openapi.yaml`).
- `oapi-codegen` v2.8.0 per fragment with `std-http-server` and `strict-server`, `import-mapping` to the shared package; each feature's generated package (`<feature>api`) is imported only by the feature root and `cmd/scp` (G7). Strict handlers make an undeclared status or body a compile error.
- Request validation in production with kin-openapi through `oapi-codegen/nethttp-middleware`, mounted under `/api/` only, errors rendered as problem+json from the validator's error type; response validation in handler tests and Schemathesis.
- Client: `openapi-typescript` types + `openapi-fetch` in `web/src/lib/api`; no hand-written fetch types.
- Conventions (architecture section 11.4): `/api/v1`, camelCase, UTC `Z`, envelopes with `freshness`, cursor pagination, weak ETags from the snapshot, RFC 9457 with `code`, deprecation headers, additive evolution enforced by `oasdiff breaking`.
- A custom check requires every GET to declare `If-None-Match`, `200` with `ETag`, `304` and the shared default problem response.
- Generated source (`*.gen.go`, bundled specs, `schema.d.ts`, `routeTree.gen.ts`) is committed with CI drift checks so `go test ./...` works without Node or codegen; build outputs (`web/dist`) are not.

## Alternatives considered
Huma v2 (code-first; RFC 9457 and SSE built in; contradicts contract-first and stays the documented alternative), ogen (own router; 3.1 support not stated), Goa (DSL as a second source), Connect (protobuf, POST-only), no codegen with contract tests only.

## Consequences
oapi-codegen's 3.1 support is "initial" (verified 2026-10-14), so the spec stays within a small 3.1 subset (`type: [T, "null"]`, no multi-type unions) with an Overlay downgrade path; an M0 spike proves one real fragment. The ServeMux 404/405 problem wrapper and its Schemathesis coverage are open items (risk W3).

## Evidence
`05-research/08-frontend-stack.md` section 3 (experiments E8, E9, E16); oapi-codegen v2.8.0 release notes verified 2026-10-14 at https://github.com/oapi-codegen/oapi-codegen/releases.

## Amendment 2026-10-14 (review revision)

Go review F-11 (verified on the v2.8.0 tag): oapi-codegen's std-http `ServeMux` is `HandleFunc` plus `http.Handler`, so `httpapi.Mux` embeds `http.Handler` and `routes.Table.Mux` returns a recording mux that registers generated patterns through the table; the API class of the table is held equal to the bundled OpenAPI paths by a test. `internal/spec` was renamed `internal/apispec`; `internal/<feature>/contract` directories became `<feature>api`. kin-openapi lives in `httpapi/validate` and `httpapi/apitest` only.
