# ADR-010: SPA hosting: embed behind a build tag, history fallback rules, static CSP

Status: Accepted. Date: 2026-10-14. Refines 04 D-16 (delivery) and replaces the nonce CSP of 06 (A-16).

## Context
D-16 requires the SPA embedded in the Go binary, SPA history fallback that never swallows API/badge/llms/webhook paths, hashed immutable assets, `index.html` no-cache, precompressed variants and a strict CSP with no third-party origins. `go test ./...` must work without Node. Vite emits `assets/<name>-<hash>.<ext>`.

## Decision
- `web/embed_placeholder.go` (`//go:build !spa`, `//go:embed placeholder`) and `web/embed_spa.go` (`//go:build spa`, `//go:embed all:dist`). Release: `go build -tags spa` after `pnpm build`.
- `internal/webapp` knows no feature: it walks the `fs.FS` once, records body, `.br`/`.gz` siblings and per-encoding ETags (`"<hash>-br"`), serves GET/HEAD only, hashed assets `public, max-age=31536000, immutable`, everything else `no-cache` with an ETag from the build id, `X-SCP-Build` on every response.
- Fallback to `index.html` only for extensionless GET/HEAD outside the reserved prefixes (`/api/`, `/badges/`, `/media/`, `/webhooks/`, `/schemas/`, `/.well-known/`, files ending `.md .adoc .txt .xml .svg .json .yaml`); never gated on `Accept: text/html`; any path with an extension that is not in `dist` is a real 404 (a stale deploy must not serve HTML as JavaScript).
- Real 404 status for unknown SPA route shapes using `dist/routes.json` generated from `web/routes.ts` (same body, status 404 so the SPA renders NotFound); repository existence stays a client-side 404.
- Static CSP without nonce: `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; require-trusted-types-for 'script'; trusted-types scp-html; report-to csp`, report-only first, Trusted Types enforced last. The theme pre-paint script is the external `/theme-init.js`. Three shadcn patches (`CSPProvider disableStyleElements`, chart variables through CSSOM, theme-transition `<style>` removed). Other response classes get `default-src 'none'; sandbox`. Mermaid (v1.1) renders in a sandboxed iframe with `frame-src 'self'` and a stricter CSP on `/_frame/mermaid.html`.
- The admin listener mounts the same bundle for `/status` with its own route manifest.

## Alternatives considered
Per-request nonce injected into `index.html` (works, but dynamic HTML without ETag and template logic in Go; the research verified zero violations with the static policy); hash of the inline script (does not cover runtime `<style>` of primitives); no precompression (larger transfer: brotli 14% smaller than gzip in the measurement).

## Consequences
Components that start injecting `<style>` in a future shadcn/Base UI version fail the Playwright CSP gate and the upgrade PR (risk W11). Generated `routes.json` ties the Go server to the SPA route tree; a test pins them together.

## Evidence
`05-research/06-web-ui-badges-security.md` sections 3-5; `05-research/08-frontend-stack.md` sections 2.5, 2.21, 9 (deviation 4); shadcn Base UI default and `cn` change verified 2026-10-14 at https://ui.shadcn.com/docs/changelog.

## Amendment 2026-10-14 (review revision)

`webapp` (rank 0) imports nothing: the reserved-prefix list (including the v1.1 `/_frame/`) is computed from the route table in `cmd/scp` and passed in `Options` (F-23). The `/_frame/mermaid.html` response class carries `frame-ancestors 'self'` while the document CSP says `'none'`. `/csp-reports` has its own rate-limit class and a 64 KiB body cap (m9). Slugs may not equal the reserved words `ext`, `groups`, `avatars`.
