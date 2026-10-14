// Package validate is the kin-openapi request validation middleware and ProblemMux. Imported only by cmd/scp.
//
// Architecture: rank 3 (internal/httpapi/validate); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: internal/httpapi, internal/httpapi/openapi, internal/platform/logx.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: kin-openapi, oapi-codegen/nethttp-middleware (owner).
//
// Design: docs/design/20-design/06-httpapi.md; architecture sections 4 and 5.2.
package validate
