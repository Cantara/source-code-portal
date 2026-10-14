// Package routes is the single route table (names, methods, patterns, route class, including the API routes), the recording mux
// that registers handlers by name, and the URL builders for every server-answered URL. Standard library only.
//
// Architecture: rank 0 (internal/routes); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: none.
// Third-party libraries owned here: none (net/http only).
//
// Design: .claude/work/plans/20-design/01-kernel.md; architecture sections 4 and 5.2.
package routes
