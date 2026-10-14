// Package ruby is the pure ruby Parser (and optional Assembler) of package manifest intelligence. It imports only the manifests
// root and its internal helpers.
//
// Architecture: rank 5 (internal/manifests/ecosystems/ruby); the closed-world row is in internal/archtest/rules.go and is the
// only authority.
// Allowed internal imports: internal/manifests, internal/manifests/internal/*.
// Additional imports allowed in _test.go files: internal/manifests/parsertest.
// Third-party libraries owned here: none.
//
// Design: .claude/work/plans/20-design/13-manifests-ecosystems-b.md; architecture sections 4 and 5.2.
package ruby
