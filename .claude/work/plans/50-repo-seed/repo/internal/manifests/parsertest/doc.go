// Package parsertest is the parser test helper: golden runner, fuzz harness and shared invariants. Imported from _test.go files
// only.
//
// Architecture: rank 5 (internal/manifests/parsertest); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: internal/manifests, internal/manifests/internal/*.
// Third-party libraries owned here: go-cmp.
//
// Design: .claude/work/plans/20-design/11-manifests-core.md; architecture sections 4 and 5.2.
package parsertest
