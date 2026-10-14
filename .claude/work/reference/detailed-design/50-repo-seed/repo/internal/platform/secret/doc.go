// Package secret is the redacting Secret type and secret-file loading.
//
// Architecture: rank 0 (internal/platform/secret); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/02-platform.md; architecture sections 4 and 5.2.
package secret
