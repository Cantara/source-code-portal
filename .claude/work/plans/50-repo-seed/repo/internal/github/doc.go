// Package github is the GitHub adapter: HTTP client, REST and GraphQL readers, rate state, installation registry, webhook verify
// and decode, typed errors and doctor.
//
// Architecture: rank 2 (internal/github); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/platform/secret, internal/platform/logx,
// internal/platform/netguard, internal/github/auth.
// Additional imports allowed in _test.go files: internal/github/githubtest.
// Third-party libraries owned here: sony/gobreaker/v2 (owner), x/sync/singleflight, x/time/rate, otelhttp, otel API.
//
// Design: docs/design/20-design/05-github-adapter.md; architecture sections 4 and 5.2.
package github
