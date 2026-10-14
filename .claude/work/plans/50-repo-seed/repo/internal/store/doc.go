// Package store is the Store contract and its record types (items, commits, tasks, parked keys, generic cache).
//
// Architecture: rank 1 (internal/store); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/freshness, internal/scm.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/03-readmodel-store.md; architecture sections 4 and 5.2.
package store
