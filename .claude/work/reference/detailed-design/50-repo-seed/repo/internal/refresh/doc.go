// Package refresh is the sync engine: the keyed coalescing queue, per-upstream dispatch, the level-triggered planner, the
// jittered scheduler, the budget gate and the shutdown grace. Keys are strings: it does not know scm, features or adapters.
//
// Architecture: rank 2 (internal/refresh); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/job, internal/freshness, internal/platform/logx.
// Third-party libraries owned here: x/sync, x/time/rate, otel API.
//
// Design: .claude/work/plans/20-design/04-refresh-webhook.md; architecture sections 4 and 5.2.
package refresh
