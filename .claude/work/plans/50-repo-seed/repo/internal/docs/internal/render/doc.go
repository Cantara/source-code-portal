// Package render is the private Markdown pipeline (goldmark, rewrite, chroma, x/net/html pass). Its last step calls
// safehtml.Sanitize; it never builds a SafeHTML itself.
//
// Architecture: rank 1 (internal/docs/internal/render); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: internal/scm, internal/freshness, internal/platform/secret, internal/safehtml.
// Third-party libraries owned here: goldmark, chroma/v2, x/net/html (owner), go.yaml.in/yaml/v3.
//
// Design: docs/design/20-design/10-docs-pages-media.md; architecture sections 4 and 5.2.
package render
