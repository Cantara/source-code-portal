// Package version holds the private version-system wrappers.
//
// Architecture: rank 1 (internal/manifests/internal/version); the closed-world row is in internal/archtest/rules.go and is the
// only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: deps.dev/util/semver.
//
// Design: docs/design/20-design/11-manifests-core.md; architecture sections 4 and 5.2.
package version
