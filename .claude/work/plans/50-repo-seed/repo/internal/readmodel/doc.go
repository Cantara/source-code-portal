// Package readmodel is the feature-agnostic immutable snapshot: typed slots, builder, projectors, validators, the atomic
// publisher and the coalescing change feed.
//
// Architecture: rank 0 (internal/readmodel); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: otel API.
//
// Design: docs/design/20-design/03-readmodel-store.md; architecture sections 4 and 5.2.
package readmodel
