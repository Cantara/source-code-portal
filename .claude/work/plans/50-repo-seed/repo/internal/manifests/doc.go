// Package manifests is package manifest intelligence (D-15): the parser/assembler registry and model, acquisition and refresh
// jobs, the org graph and reverse index, the manifests API and the LLM/CycloneDX outputs.
//
// Architecture: rank 4 (internal/manifests); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/job, internal/routes, internal/readmodel, internal/store,
// internal/httpapi, internal/httpapi/authz, internal/platform/secret, internal/platform/logx, internal/manifests/manifestsapi,
// internal/manifests/internal/*, internal/httpapi/openapi.
// Additional imports allowed in _test.go files: internal/httpapi/apitest, internal/manifests/parsertest.
// Third-party libraries owned here: x/sync, cyclonedx-go (export boundary), otel API.
//
// Design: docs/design/20-design/11-manifests-core.md; architecture sections 4 and 5.2.
package manifests
