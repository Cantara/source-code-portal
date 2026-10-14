// Package reactor holds reactor.Func and reactor.Fan: the synchronous fan-out of one scm.Event to every feature reaction
// function. It is the only package besides github and the features that knows both job and scm.
//
// Architecture: rank 2 (internal/reactor); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/job, internal/scm, internal/platform/logx.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/01-kernel.md; architecture sections 4 and 5.2.
package reactor
