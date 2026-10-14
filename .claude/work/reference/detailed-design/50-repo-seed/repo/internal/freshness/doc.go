// Package freshness is the status and age vocabulary: the stored State, the derived Status, Policy, Info and the generic Value[T]
// wrapper carried by every fetched item.
//
// Architecture: rank 0 (internal/freshness); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/01-kernel.md; architecture sections 4 and 5.2.
package freshness
