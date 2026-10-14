# ADR-025: Security model, trust zones and cross-cutting controls

Status: Accepted. Date: 2026-10-14. Refines 04 D-09 (A-11).

## Context
D-09 lists HMAC webhooks with dedupe, typed secrets, CSP and headers, sanitised content, SSRF guards with allow-lists and a hardened container. Research added CSRF behaviour of a trusted-proxy cookie, rate limiting, identity-header spoofing and prompt injection through repository text.

## Decision
Seven trust zones and their controls are fixed in architecture section 17: webhook (SHA-256 on raw bytes, body limit and semaphore, bounded in-memory dedupe (LRU, 15 min) after enqueue, route absent without a secret); repository content (sanitiser, pure bounded parsers, no execution); outbound URLs (`platform/netguard` using `code.dny.dev/ssrf`: connect-time IP policy, `Proxy: nil` on guarded transports, explicit CIDR/port allow-lists per integration, credentials attached only by the owning adapter, redirect host pinning); public API (keyed rate limiter with IPv6 /64 bucketing and key cap, `Retry-After`, no `RateLimit-*` headers yet); state-changing endpoints (`http.CrossOriginProtection` with explicit webhook and CSP-report bypass; admin mutation also needs a JSON content type and `X-SCP-Request`); secrets (`Secret` type, installation tokens in memory only, App key never persisted); admin plane (separate listener; identity-trust logic never runs there); supply chain (checksum database, `govulncheck`, Renovate, pnpm cooldown, SHA-pinned Actions). No CORS in production. Trusted-proxy identity per ADR-023. LLM-facing text strips hidden comments and bidi/control characters and marks repository-authored data as untrusted.

## Alternatives considered
A WAF in front (not available in all deployments), per-IP limits on valid webhooks (would block GitHub's delivery IPs), SameSite cookies as the only CSRF defence (defence in depth only).

## Consequences
A small amount of cross-cutting code in `httpx`, `netguard` and `webhook`; every control has a named test (architecture section 20).

## Evidence
`05-research/06-web-ui-badges-security.md` sections 4-5, 9-14; `05-research/09-github-app-integration.md` section 8; `05-research/07-manifest-parsing.md` section 7.1.

## Amendment 2026-10-14 (review revision)

Webhook de-duplication is specified once (it was three different ways): a bounded in-memory LRU with TTL 15 min and a 10,000-entry cap, lazily evicted, recorded after a successful enqueue; exactness is not required because webhooks are hints and jobs are idempotent. `/csp-reports` gets a 30/min per-IP class and a 64 KiB body cap. Visibility is enforced at the **count** level through `scm.ViewClass` variants of global projections (ADR-023). The `Retrier` admin hook is bridged over the engine (ops imports no `job`).
