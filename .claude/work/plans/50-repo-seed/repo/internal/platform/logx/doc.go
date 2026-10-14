// Package logx provides the slog handlers and the context-carried attributes (request id, delivery id, job key).
//
// Architecture: rank 1 (internal/platform/logx); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/platform/secret.
// Third-party libraries owned here: otel API (trace, for trace_id).
//
// Design: .claude/work/plans/20-design/02-platform.md; architecture sections 4 and 5.2.
package logx
