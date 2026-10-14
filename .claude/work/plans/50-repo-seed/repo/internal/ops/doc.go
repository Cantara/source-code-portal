// Package ops serves /livez, /readyz, /status, /metrics, pprof and the admin actions; owns the readiness gates and dependency
// freshness.
//
// Architecture: rank 4 (internal/ops); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/freshness, internal/readmodel, internal/httpapi, internal/httpapi/authz,
// internal/platform/logx, internal/ops/opsapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: prometheus/client_golang (owner), otel API.
//
// Design: docs/design/20-design/15-live-ops-overview.md; architecture sections 4 and 5.2.
package ops
