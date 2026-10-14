// Package apitest validates responses against the bundled spec. Imported from _test.go files only.
//
// Architecture: rank 3 (internal/httpapi/apitest); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/httpapi, internal/httpapi/openapi, internal/apispec.
// Third-party libraries owned here: kin-openapi.
//
// Design: .claude/work/plans/20-design/06-httpapi.md; architecture sections 4 and 5.2.
package apitest
