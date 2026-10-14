// Package webhook is the webhook receiver: size limit, signature verification through a Verifier, bounded in-memory delivery
// de-duplication after enqueue, the 202/400/401/503 mapping and the optional webhook.redelivery job.
//
// Architecture: rank 4 (internal/webhook); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/scm, internal/job, internal/httpapi, internal/platform/logx.
// Third-party libraries owned here: otel API.
// Note: 04 D1 (freshness) was WITHDRAWN by unit 04's verification: webhook stays inside the architecture row (cross-design review
// CD-DM-04).
//
// Design: docs/design/20-design/04-refresh-webhook.md; architecture sections 4 and 5.2.
package webhook
