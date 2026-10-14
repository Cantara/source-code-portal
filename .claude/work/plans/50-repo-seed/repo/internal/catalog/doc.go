// Package catalog owns sources, groups, matchers, the repository reconcile and head/stats sweep, the default repository, the
// membership index, slug aliases, the visibility policy and the config validation used by config lint.
//
// Architecture: rank 4 (internal/catalog); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx, internal/catalog/catalogapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
//
// Design: docs/design/20-design/08-catalog.md; architecture sections 4 and 5.2.
package catalog
