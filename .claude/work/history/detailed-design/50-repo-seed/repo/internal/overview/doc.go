// Package overview is cross-feature read composition and nothing else: site info, the composite repo resource, the llms.txt
// family, sitemap, robots, negotiated Markdown for human URLs and slug-alias redirects. No state, no jobs, no store, no upstream,
// no bridges (those live in cmd/scp/internal/bridge).
//
// Architecture: rank 5 (internal/overview); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/routes, internal/readmodel, internal/httpapi,
// internal/httpapi/authz, internal/platform/logx, internal/overview/overviewapi, internal/catalog, internal/commits,
// internal/docs, internal/manifests, internal/badges, internal/pages, internal/integrations, internal/httpapi/openapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: otel API.
// Note: A-30: httpapi/openapi added (shared generated schemas; design 07 alias files make this removable).
//
// Design: .claude/work/plans/20-design/15-live-ops-overview.md; architecture sections 4 and 5.2.
package overview
