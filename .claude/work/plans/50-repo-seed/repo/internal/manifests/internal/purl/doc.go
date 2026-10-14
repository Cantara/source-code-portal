// Package purl is the private package-URL normaliser.
//
// Architecture: rank 1 (internal/manifests/internal/purl); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: none.
// Third-party libraries owned here: package-url/packageurl-go.
//
// Design: docs/design/20-design/11-manifests-core.md; architecture sections 4 and 5.2.
package purl
