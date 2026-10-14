// Package commits owns commit ingestion, bounded retention, the latest-N and per-group projections and the commit API.
//
// Architecture: rank 4 (internal/commits); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx, internal/commits/commitsapi,
// internal/httpapi/openapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
//
// Design: docs/design/20-design/09-commits-badges.md; architecture sections 4 and 5.2.
package commits
