06-httpapi reference prototype (throw-away; the design document is ../06-httpapi.md)

Module path github.com/cantara/source-code-portal, Go 1.27.1. Every exported signature quoted in the design was taken from
`go doc` on this tree. Run:

  go vet ./... && go test -race -count=1 ./...
  mkdir web/dist && cp -r web/placeholder/* web/dist/ && go vet -tags spa ./web   # embed_spa.go compiles only with dist present
  go run ./cmd/apilint ../api/openapi-public.yaml                                  # lint rules L1-L11 on a design-unit spec

Layout (target packages in the new repository):
  internal/httpapi            problem, cursor, etag, static, negotiate, gzip, defaults, envelope, redirect, mux
  internal/httpapi/authz      principal, class (Viewer), lookup (LookupRepo)
  internal/httpapi/validate   RequestValidator, ProblemMux (kin-openapi)
  internal/httpapi/apitest    Load, Lint (L1-L11), ValidateResponse/ValidateProblem, ETagCoversBody, NoLeak
  internal/httpapi/openapi    common.yaml (byte copy of ../api/common.yaml) + oapi-codegen v2.8.0 output
  internal/apispec            Public(), Admin() over embedded bundles (here: small test bundles, NOT the real contract)
  internal/webapp             Handler, bundle loader, routes.json, CSP, CSP reports
  web                         embed_placeholder.go / embed_spa.go, placeholder bundle
  internal/e2e                the whole API chain with a generated strict server
  internal/wiring             seam tests against the real unit-01 tables (placeholder manifest == SPAShapes, ReservedPrefixes, Status enum)
  internal/commits/commitsapi sample feature fragment proving the generated strict-server shapes
  cmd/apilint                 runs apitest.Lint over a spec file (external refs allowed)

COPIES of other units (taken from ../01-kernel.assets; the *_test.go files of scm/freshness/routes are dropped):
  internal/scm, internal/freshness, internal/routes
STUBS with the signatures of 10-architecture.md (not designs): internal/readmodel, internal/platform/logx.

Verification 2026-10-14 (independent; see ../06-httpapi.md "Verification log" and ../../30-verification/design-06-httpapi.md):
  fixed in this tree: webapp hashedName (Vite hashes contain '-'), APIDefaults scoped to /api/, nosniff kept on problems,
  fixed-text decode/binding details, exact-name file lookup, httpapi/request.go (WithRequest, RequestFrom, Deref, Ptr),
  e2e chain uses them; internal/scm refreshed from unit 01. New tests: internal/httpapi/regress_test.go,
  internal/httpapi/validate/regress_test.go, internal/webapp/regress_test.go, internal/wiring/serve_test.go (46 test functions).
