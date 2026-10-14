// Package integrations is the StatusProvider contract, targets, the refresh job and the status slot. integrations/jenkins and
// integrations/snyk are the optional providers.
//
// Architecture: rank 4 (internal/integrations); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
//
// Design: docs/design/20-design/14-integrations-access.md; architecture sections 4 and 5.2.
package integrations
