// Package scm holds the normalised, provider-neutral value types (repository, commit, immutable tree, blob, release, event, page,
// validators and result) and the portal identity of a repository (Slug, GroupID, GroupMeta, Membership, ViewClass) that at least
// three features read.
//
// Architecture: rank 0 (internal/scm); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/01-kernel.md; architecture sections 4 and 5.2.
package scm
