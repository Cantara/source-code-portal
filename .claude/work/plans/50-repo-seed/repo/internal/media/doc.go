// Package media is the image proxy and signed-URL minting: SSRF-guarded fetch of external README images and repo-relative image
// serving. The HTTP client is injected.
//
// Architecture: rank 4 (internal/media); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
//
// Design: docs/design/20-design/10-docs-pages-media.md; architecture sections 4 and 5.2.
package media
