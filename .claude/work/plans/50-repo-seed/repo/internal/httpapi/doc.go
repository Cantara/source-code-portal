// Package httpapi provides problem details, cursor pagination, ETag and conditional helpers, Static, Negotiate and the
// Mux/Mounter types. It imports no kin-openapi, no httpx and no readmodel.
//
// Architecture: rank 2 (internal/httpapi); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/routes, internal/platform/logx, internal/httpapi/openapi.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/06-httpapi.md; architecture sections 4 and 5.2.
package httpapi
