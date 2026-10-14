// Package pages serves config-driven Markdown pages and the optional announcement. Markdown arrives through an injected Renderer
// interface returning safehtml.SafeHTML.
//
// Architecture: rank 4 (internal/pages); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx, internal/pages/pagesapi,
// internal/safehtml.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
//
// Design: .claude/work/plans/20-design/10-docs-pages-media.md; architecture sections 4 and 5.2.
package pages
