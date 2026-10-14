// Package authz is Principal, Authorizer, ClassOf and LookupRepo. Visibility is a security control (architecture 17.3).
//
// Architecture: rank 3 (internal/httpapi/authz); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/readmodel, internal/httpapi, internal/platform/logx.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/06-httpapi.md; architecture sections 4 and 5.2.
package authz
