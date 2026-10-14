// Package safehtml defines the SafeHTML type and the bluemonday-based Sanitize, its only constructor. It is rank 0 so that pages,
// docs and render can all name the type.
//
// Architecture: rank 0 (internal/safehtml); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: microcosm-cc/bluemonday (owner).
//
// Design: docs/design/20-design/01-kernel.md; architecture sections 4 and 5.2.
package safehtml
