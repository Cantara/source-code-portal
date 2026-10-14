// Package job is the contract between features and the sync engine: Key, Priority, Spec (with Timeout), Outcome, Enqueuer, the
// ErrQueueFull and ErrClosed sentinels and the behavior-interface error classification (RateLimited, Permanent, Gone). It knows
// nothing about events.
//
// Architecture: rank 1 (internal/job); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/freshness.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/01-kernel.md; architecture sections 4 and 5.2.
package job
