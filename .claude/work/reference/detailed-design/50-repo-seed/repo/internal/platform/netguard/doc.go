// Package netguard provides the SSRF-guarded dialers and http.Client constructors with per-integration allow-lists.
//
// Architecture: rank 0 (internal/platform/netguard); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: none.
// Third-party libraries owned here: code.dny.dev/ssrf (owner).
//
// Design: .claude/work/plans/20-design/02-platform.md; architecture sections 4 and 5.2.
package netguard
