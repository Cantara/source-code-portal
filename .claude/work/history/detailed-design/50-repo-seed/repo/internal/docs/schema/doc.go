// Package schema embeds the JSON Schemas (scp-v1.json, front matter) served at /schemas/scp/*.
//
// Architecture: rank 0 (internal/docs/schema); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none (embed only).
//
// Design: .claude/work/plans/20-design/10-docs-pages-media.md; architecture sections 4 and 5.2.
package schema
