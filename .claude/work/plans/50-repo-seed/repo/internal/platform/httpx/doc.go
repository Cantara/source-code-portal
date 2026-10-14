// Package httpx is NewServer (the only constructor of *http.Server), the middleware chain, client-IP resolution and the keyed
// rate limiter.
//
// Architecture: rank 2 (internal/platform/httpx); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/platform/logx, internal/platform/secret.
// Third-party libraries owned here: x/time/rate, otelhttp.
// Note: platform/secret added: 02-platform deviation D2.
//
// Design: .claude/work/plans/20-design/02-platform.md; architecture sections 4 and 5.2.
package httpx
