// Package badges renders local flat-square SVG badges: badge kinds, cache headers and the unknown state.
//
// Architecture: rank 4 (internal/badges); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API (SVG built with encoding/xml).
//
// Design: .claude/work/plans/20-design/09-commits-badges.md; architecture sections 4 and 5.2.
package badges
