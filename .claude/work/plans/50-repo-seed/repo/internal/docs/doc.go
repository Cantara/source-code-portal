// Package docs owns README and docs ingestion, descriptor loading, rendering orchestration, the AsciiDoc bridge (GitHub-rendered HTML), raw
// Markdown handlers and the doc API.
//
// Architecture: rank 4 (internal/docs); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx, internal/docs/docsapi,
// internal/docs/internal/render, internal/docs/schema, internal/safehtml.
// Additional imports allowed in _test.go files: internal/httpapi/apitest.
// Third-party libraries owned here: santhosh-tekuri/jsonschema/v6, go.yaml.in/yaml/v3, otel API.
//
// Design: docs/design/20-design/10-docs-pages-media.md; architecture sections 4 and 5.2.
package docs
