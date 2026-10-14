// Package live serves GET /api/v1/snapshot and the SSE invalidation stream and owns the shutdown-aware long-lived handlers.
//
// Architecture: rank 4 (internal/live); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/readmodel, internal/httpapi, internal/platform/logx, internal/live/liveapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: none.
//
// Design: docs/design/20-design/15-live-ops-overview.md; architecture sections 4 and 5.2.
package live
