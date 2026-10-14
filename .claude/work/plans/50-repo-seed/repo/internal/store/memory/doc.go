// Package memory is the in-memory store.Store for unit tests and synctest bubbles.
//
// Architecture: rank 2 (internal/store/memory); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/store, internal/freshness.
// Additional imports allowed in _test.go files: internal/store/storetest.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/03-readmodel-store.md; architecture sections 4 and 5.2.
package memory
