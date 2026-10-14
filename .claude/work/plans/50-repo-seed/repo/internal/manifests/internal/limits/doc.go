// Package limits holds the private parse limits and the watchdog.
//
// Architecture: rank 1 (internal/manifests/internal/limits); the closed-world row is in internal/archtest/rules.go and is the
// only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: x/sync (semaphore), x/text/encoding/charmap.
//
// Design: docs/design/20-design/11-manifests-core.md; architecture sections 4 and 5.2.
package limits
