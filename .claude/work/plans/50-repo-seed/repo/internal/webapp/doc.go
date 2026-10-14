// Package webapp hosts the embedded SPA: static files, history fallback, precompression negotiation, CSP and security headers and
// the routes.json shape check.
//
// Architecture: rank 0 (internal/webapp); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none (brotli fallback only if precompression is unavailable).
//
// Design: docs/design/20-design/06-httpapi.md; architecture sections 4 and 5.2.
package webapp
