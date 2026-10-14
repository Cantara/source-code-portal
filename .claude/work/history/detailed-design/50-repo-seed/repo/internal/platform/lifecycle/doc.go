// Package lifecycle is Run(ctx, Plan): ordered stages, each with its own context and stop deadline; the readiness flip, the drain
// and the reverse-order shutdown.
//
// Architecture: rank 0 (internal/platform/lifecycle); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/02-platform.md; architecture sections 4 and 5.2.
package lifecycle
