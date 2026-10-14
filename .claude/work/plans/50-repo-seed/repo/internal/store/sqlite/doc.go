// Package sqlite is the SQLite implementation of store.Store: modernc driver, WAL, its own migration runner, quarantine and
// backup. Only cmd/scp may import it.
//
// Architecture: rank 2 (internal/store/sqlite); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/store, internal/freshness.
// Additional imports allowed in _test.go files: internal/store/storetest.
// Third-party libraries owned here: modernc.org/sqlite, modernc.org/libc (owner), otel API.
//
// Design: docs/design/20-design/03-readmodel-store.md; architecture sections 4 and 5.2.
package sqlite
