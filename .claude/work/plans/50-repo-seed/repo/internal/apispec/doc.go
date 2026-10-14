// Package apispec embeds the bundled public and admin OpenAPI documents served at /api/v1/openapi.yaml.
//
// Architecture: rank 0 (internal/apispec); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/07-openapi-contract.md; architecture sections 4 and 5.2.
package apispec
