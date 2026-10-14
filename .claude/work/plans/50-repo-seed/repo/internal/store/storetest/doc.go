// Package storetest is the conformance suite run against every store.Store implementation. Imported from _test.go files only.
//
// Architecture: rank 2 (internal/store/storetest); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/store, internal/freshness.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/03-readmodel-store.md; architecture sections 4 and 5.2.
package storetest
