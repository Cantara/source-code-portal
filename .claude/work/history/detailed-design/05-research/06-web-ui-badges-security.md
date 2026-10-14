# 06 - SPA delivery, live updates, badges and web security (research)

Status: research input for the architecture and `internal/webapp`, `internal/httpapi`, `internal/badges`, `internal/docs` (sanitiser), `internal/access`, `internal/platform/httpx` designs.
Written 2026-10-13. Conforms to `04-decisions.md` (D-05 partly superseded, D-06, D-09, D-12, D-16). Frontend stack choice (React/Router/Query/Tailwind versions) belongs to another research task; this report only covers what the Go side and the CSP/security design must know about it.

Citation convention: `(S12, 2026-10-13)` = source S12 in the table at the end, fetched/verified on that date. `(E3)` = my own experiment run on 2026-10-13 against live packages; prototype code is in `05-research/06-prototypes/` (throwaway, not production). Anything marked **UNVERIFIED** could not be confirmed on the live web or by experiment.

---

## Executive summary (10 lines)

1. **Serve the SPA from Go with one small `webapp` handler**: exact-file lookup in an embedded `fs.FS`, `GET /{path...}` as the only catch-all, history fallback to `index.html` only for extension-less GET/HEAD paths outside the reserved prefixes; `/api/`, `/badges/`, `/media/` etc. are registered explicitly so they can never fall through to HTML (verified with a ServeMux test, E4).
2. **Cache policy**: Vite emits `assets/<name>-<hash>.<ext>` (not `/assets/{hash}/...` as D-12 says, E1) -> `public, max-age=31536000, immutable`; `index.html` -> `no-cache`; precompress at build time with brotli+gzip (221 KB JS -> 58.8 KB br / 68.3 KB gzip, E1), serve via `Accept-Encoding` negotiation + `Vary`; skip zstd for now (85.6% support, Safari partial).
3. **CSP**: per-request nonce injected by Go into `index.html` (Vite's `html.cspNonce` placeholder, E1) covers the inline theme pre-paint script, Vite's tags and the inline `<style>` that Radix ScrollArea/Select and Base UI inject; policy is `default-src 'none'`, `script-src 'self' 'nonce-N'`, `style-src 'self'`, `style-src-elem 'self' 'nonce-N'`, no third-party origin, Trusted Types report-only first then enforced (S6, S9, S10, S11, S12, S30).
4. **Live updates: polling with ETag/304 is the v1 mechanism** (TanStack Query `refetchInterval` pauses in background tabs by default, the browser cache does the conditional request, server answers 304 from the snapshot generation); SSE is a designed-for, optional v1.1 upgrade carrying only `invalidate` events, never data (E5 proves the Go side works past `WriteTimeout`).
5. **Badges**: write our own ~120 line flat-square renderer (no maintained Go library fits: narqo/go-badge last pushed 2024-03, freetype dependency, global state, Vera-based fudge factor); measure with a DejaVu-Sans-derived width table, which is within +/-3% of Verdana (E7) and is hidden by `textLength` anyway; `html/template`-free XML escaping, `role=img` + `aria-label` + `<title>`, ETag from snapshot generation, `Cross-Origin-Resource-Policy: cross-origin`, unknown state = 404 + a grey "unknown" badge body.
6. **Markdown safety**: pipeline order is goldmark(unsafe raw HTML) -> `x/net/html` rewrite pass (URLs, alerts, id namespacing) -> **bluemonday last** -> `SafeHTML`; bluemonday works but is near-stalled (release v1.0.27 2024-07-04, last commit 2025-04-04, open PRs unmerged), so pin it, bump `golang.org/x/net`, keep policy deny-by-default, never allow `style`/`srcset`/`svg`/`math`, and fuzz the idempotency property (2.5M execs clean in 30 s, E3).
7. **SPA display**: `dangerouslySetInnerHTML`/`innerHTML` through exactly one `SafeHtml` component using a Trusted Types policy (`scp-html`); no client-side Markdown parsing; DOMPurify NOT added by default (it had 6 advisories in the last 10 weeks, S31); same-origin `/media/...` image proxy instead of any third-party `img-src`.
8. **Outbound safety**: `code.dny.dev/ssrf` Control hook on a dedicated `http.Transport` with `Proxy: nil` (E8), per-integration explicit allow for private Jenkins hosts; **trusted-proxy identity** only from allow-listed CIDRs with right-to-left X-Forwarded-For parsing, own ~50 line implementation (E6), startup refuses private visibility without it.
9. **Request-side controls**: `http.CrossOriginProtection` (Go 1.25+) around every non-GET `/api` route with the webhook as an explicit bypass (E9); own keyed token-bucket rate limiter on `x/time/rate` with 429 + `Retry-After` (IETF RateLimit headers are still a draft); `MaxBytesReader` everywhere; no CORS in production, Vite proxy in dev; errors are RFC 9457 `application/problem+json`.
10. **Corrections found** (details at the end): D-12 route `/badges/{repo}/{kind}.svg` is an invalid ServeMux pattern, method-less `/webhooks/github` conflicts with `GET /{path...}`, goldmark is now `github.com/yuin/goldmark/v2`, asset URLs are `name-hash.ext`, shadcn now defaults to Base UI (CSP provider differs from Radix), bluemonday is not actively maintained.

---

## 1. Environment facts that shape the design (all verified 2026-10-13)

| Fact | Value | Source |
|---|---|---|
| Current Go | 1.27.0 (2026-08-19), patch 1.27.2 (2026-10-08); 1.26.9 also supported; 1.25 unsupported | S1 |
| Go 1.25 added `http.CrossOriginProtection` | yes, Sec-Fetch-Site/Origin based, no tokens | S5, S2 |
| Go 1.26: ServeMux trailing-slash redirect is now 307; `ReverseProxy.Director` deprecated | yes | S4 |
| Go 1.27: `Server.MaxHeaderValueCount`, HTTP/2 client priority (RFC 9218), `encoding/json/v2` backs `encoding/json` | yes | S3, S2 |
| Vite | 8.3.4, Rolldown bundler, default target `baseline-widely-available` (chrome111, safari16.4), `assetsInlineLimit` 4096 | S6, S7 |
| Tailwind CSS | 4.3.3 (npm), React 19.3.0, TanStack Query 5.104.2, TanStack Router 1.170.44, React Router 8.4.0, Zod 4.6.7 | S43 |
| shadcn/ui | `init` defaults to **Base UI** since 2026-07-02; Radix still supported (`-b radix`) | S14 |
| Base UI 1.9.0, `radix-ui` 1.8.1, `@radix-ui/react-scroll-area` 1.4.0 | npm | S43 |
| bluemonday | v1.0.27 (2024-07-04), last commit 2025-04-04, BSD-3 | S24 |
| goldmark | v2.1.6 (2026-09-27), module path `github.com/yuin/goldmark/v2`, go 1.25 | S25 |
| goldmark-highlighting | v3.1.0, needs go **1.27**, depends on chroma v3.0.0-alpha.5 | S25 |
| DOMPurify | 3.4.17 (2026-10-12) | S31 |
| WCAG 2.2 | W3C Recommendation 2023-10-05, WCAG 3 still draft | S40 |
| RFC 9457 | July 2023, obsoletes RFC 7807 | S19 |
| Trusted Types | MDN: Baseline 2026 "newly available" (since Feb 2026) | S30 |
| HTML Sanitizer API (`setHTML`) | MDN: limited availability, **not Baseline** | S29 |
| Brotli support | 97.24% global; zstd content-encoding 85.6% (Safari partial) | S41 |

Frontend-neutral note: everything in sections 3-5 holds if the owner later flips to Svelte 5, except the Radix/Base UI inline `<style>` item in 4.4.

---

## 2. Accessibility and theming principles (item 1, reduced scope)

Only principles that constrain the Go side or the cross-stack contract; component work belongs to the frontend research.

- **WCAG 2.2 AA is the bar** (S40). The new AA criteria that bite a dashboard: 2.4.11 Focus Not Obscured (sticky header must not cover focused rows), 2.5.7 Dragging Movements (no drag-only interactions), 2.5.8 Target Size 24x24 CSS px (icon-only links in the group card), 3.3.8 Accessible Authentication (relevant only if OIDC is added later). 4.1.1 Parsing was removed.
- **Landmarks and focus on route change**: SPA must move focus to the page `<h1>` (or a `main` skip target) and announce the title on navigation; this is the usual SPA regression and needs a Playwright+axe test per route (D-16 already has axe in CI). **UNVERIFIED as a WCAG-mandated technique; it is general SPA practice.**
- **Theming**: shadcn's Vite guide implements theme as a `ThemeProvider` that reads `localStorage` in a `useState` initialiser and toggles `light`/`dark` classes on `<html>` in a `useEffect` (S13) - i.e. **after first render, so it flashes**. The docs page mentions no pre-paint script (S13). Therefore ship a tiny inline script in `index.html` (section 4.3) and declare `color-scheme` both as `<meta name="color-scheme" content="light dark">` before any CSS and as `:root { color-scheme: light dark }` (S39, MDN `color-scheme`: "Baseline widely available since January 2022"; meta tag placement before CSS prevents flashes). `light-dark()` is Baseline 2024 "newly available" and works when `color-scheme` includes both (S39), so tokens can use it, but class-based `.dark` is still needed for the persisted override.
- **No third-party requests**: fonts self-hosted or system stack. `@fontsource-variable/inter` 5.3.0 exists on npm (S43); a system-UI font stack costs zero requests and zero `font-src` surface and is the recommended default. Icons: lucide tree-shaken inline SVG (lucide-react 1.56.0, S43), no icon font (GR-39).
- **Contrast of badges** is a product issue, not only SPA: Shields default colours fail AA with white text (computed, E10): brightgreen `#4b0` 2.51:1, yellow `#dfb317` 1.98:1, orange `#ea7233` 3.02:1, blue `#007ec6` 4.38:1, red `#e05d44` 3.61:1, label grey `#555` 7.46:1. Our palette should use darker equivalents (e.g. `#1a7f37` 5.08, `#b45309` 5.02, `#c0392b` 5.44, `#0b61a4` 6.44, E10) so badges pass 4.5:1.

---

## 3. Serving the embedded SPA from Go (item 3, adapted)

### 3.1 What Vite actually emits (E1: Vite 8.3.4 + Tailwind 4.3.3 + React 19.3.0, built 2026-10-13)

```html
<script nonce="__CSP_NONCE__">/* my inline theme script */</script>
  <meta property="csp-nonce" nonce="__CSP_NONCE__">
  <script type="module" crossorigin src="/assets/index-BOdAYDbw.js" nonce="__CSP_NONCE__"></script>
  <link rel="stylesheet" crossorigin href="/assets/index-CNbZmh1g.css" nonce="__CSP_NONCE__">
```

Observations:
- File names are `assets/<name>-<8-char hash>.<ext>`; `D-12`'s `/assets/{hash}/...` should become `/assets/{file}` (the hash is in the file name).
- Setting `html: { cspNonce: '__CSP_NONCE__' }` makes Vite add the nonce to its `<script>`, `<style>`, stylesheet and modulepreload `<link>` tags and inject `<meta property="csp-nonce" nonce=...>`; the built JS reads that meta (`i?.nonce||i?.getAttribute('nonce')` found in the bundle) for dynamically created tags. The placeholder must be replaced per request (S6, E1).
- My own inline `<script nonce=...>` survived untouched (Vite does not process non-module inline scripts), so the theme script is ours to hash or nonce.
- Tailwind v4 produces a single external CSS file (4.4 KB for the stub), no inline styles.
- A `lazy()` chunk produced a separate hashed JS file; the `modulepreload` polyfill is bundled into the entry JS, not inlined into HTML (S7: `build.modulePreload.polyfill` default true).
- Set `build.assetsInlineLimit: 0` so no `data:` URIs are produced; otherwise `img-src`/`font-src` need `data:` (S6 says exactly this, and warns never to allow `data:` in `script-src`).
- `build.sourcemap` defaults false (S7): keep it false for the embedded build (GR-19: no source maps in production). If wanted, use `'hidden'` and do not embed the `.map` files.

### 3.2 Package shape

`internal/webapp` knows nothing about features. Inputs: an `fs.FS` (embedded `web/dist`, plus a placeholder `index.html` for `go test` per D-16) and a CSP builder. It exposes one `http.Handler`.

Tested prototype (`06-prototypes/webapp`, E4): at construction it walks the FS once, records body, `.br`/`.gz` sidecars, a strong ETag (sha256 prefix of the identity body) and whether the path is under `assets/`; `index.html` is kept as a template.

Rules (all asserted by the prototype tests):

| Request | Result |
|---|---|
| exact file in dist | serve with `Content-Type` by extension, `X-Content-Type-Options: nosniff`, `Vary: Accept-Encoding`; `assets/*` -> `Cache-Control: public, max-age=31536000, immutable`; other static (`robots.txt`, `favicon.svg`) -> `no-cache` (S18: hashed URLs may be immutable, mutable documents `no-cache`) |
| `GET`/`HEAD` and extension-less path and not reserved | `index.html` with fresh nonce, `Cache-Control: no-cache`, **no ETag** (body differs per response), CSP header |
| anything with an extension that is not in dist (`/assets/missing.js`, `/x.png`) | real 404 (never HTML, so a stale deploy cannot serve HTML as JS) |
| `POST /dashboard` | 405 (catch-all is `GET`-only) |
| `/api/...` unknown | explicit `GET /api/` handler -> 404 `application/problem+json` |

Prototype ServeMux result table (E4): `/`, `/dashboard`, `/repos/foo/commits` -> 200 HTML; `/api/v1/ping` -> 200; `/api/v1/nope`, `/api/` -> 404 problem+json; `/badges/foo/issues.svg` -> 200; `POST /webhooks/github` -> 204; `POST /dashboard` -> 405; `/assets/missing-abc.js`, `/missing.png` -> 404.

**Do not gate the fallback on `Accept: text/html`** (my first prototype did; `curl`, link unfurlers and some crawlers send `*/*` and would get 404 on deep links). Gate on method + no extension + not reserved, and let `Sec-Fetch-Dest` be informational only.

**Real 404 for unknown SPA routes (D-12 "Unknown -> real 404")**: the Go side cannot know dynamic ids (`/repos/{repo}`), but it can know the *shape* of valid routes. Proposal (**design, not verified against TanStack Router internals**): a build step emits `web/dist/routes.json` from the typed route tree (TanStack path params are `$param`, S15b; the generated tree file name was not confirmed); `webapp` loads it into a private `http.ServeMux` (translating `$id` to `{id}`) and answers shape-mismatches with **404 + index.html body** (so the SPA still renders its NotFound page) and shape-matches with 200. Existence of the repo itself remains a client-side 404 (soft-404 for that case is acceptable and documented).

### 3.3 Route table and ServeMux gotchas (verified E4/E11)

- `GET /{path...}` as catch-all coexists with `POST /webhooks/github`, `GET /api/`, `GET /badges/{repo}/{file}`, `GET /livez` (E4).
- A **method-less** pattern such as `/webhooks/github` **conflicts** with `GET /{path...}` ("matches more methods than GET /{path...}, but has a more specific path pattern", E11). Register the webhook as `POST /webhooks/github`.
- A wildcard must occupy a full segment: `GET /badges/{repo}/{kind}.svg` **panics at registration** ("bad wildcard segment (must end with '}')", E11). Use `GET /badges/{repo}/{file}` and `kind, ok := strings.CutSuffix(file, ".svg")`.
- `r.PathValue` is unescaped: `/badges/x/a%2Fb.svg` yields `file == "a/b.svg"` (E11). Every handler must validate path values against typed IDs (repo name regex) and must not join them into file paths or upstream URLs unvalidated.
- `//` in a path is redirected with 307 in Go 1.26+ (E11, S4).
- ServeMux semantics: `GET` also matches `HEAD`; most specific wins; conflicts panic at registration (S2) - a startup-time guarantee that the route table has no ambiguity.

Proposed public route table (replaces D-12 text; admin listener unchanged):

```
GET  /livez  /readyz
POST /webhooks/github            (HMAC; CrossOriginProtection bypass; MaxBytes 26 MiB since GitHub caps payloads at 25 MB, S44)
GET  /api/v1/...                  (JSON; problem+json errors; ETag)      GET /api/   (catch-all 404 problem)
GET  /badges/{repo}/{file}        (file = kind + ".svg"; served directly, never via a redirect: see Risks)
GET  /media/{repo}/{ref}/{path...}   GET /media/ext/{id}                  (image proxy, section 8.5)
GET  /repos/{repo}/readme.md      GET /llms.txt  /llms-full.txt  /sitemap.xml  /robots.txt
GET  /{path...}                   -> webapp (static + SPA fallback)
```

### 3.4 Compression

- **Static**: precompress at build with brotli q11 + gzip -9 and embed `*.br`/`*.gz` next to each file. Measured (E1): `index-*.js` 221,081 B identity -> 58,812 B brotli q11 -> 68,320 B gzip -9 (brotli 14% smaller). The prototype negotiates by `Accept-Encoding` (honouring `q=0`), sets `Content-Encoding`, `Vary: Accept-Encoding`, and **per-variant ETags** (`"<hash>-br"`, `"<hash>-gz"`) so 304 works per representation (E4: conditional request returned 304).
- Build tooling: `vite-plugin-compression2` 2.5.3 (npm, modified 2026-03-21, S43) or a CI shell step (`brotli -q 11 -k`, `gzip -9 -k`; both CLIs exist). Prefer the CI step: no extra npm dependency, same result. **Runtime brotli at q11 in Go was not benchmarked (my run did not finish), so I do not recommend startup compression**; `andybalholm/brotli` v1.2.6 (MIT, pushed 2026-10-12, S44) is the fallback if precompression is unavailable.
- **zstd**: global support 85.6% with Chrome 123+, Firefox 126+, iOS Safari 26.3+, desktop Safari only partial (S41). Not worth a third variant in v1.
- **Dynamic** (JSON API, sanitised HTML payloads up to ~1.5 MB for pathological READMEs, E3): gzip only, via `klauspost/compress/gzhttp` (v1.20.3, pushed 2026-10-13, S44) or a 40-line own middleware; **exclude** `text/event-stream` and badges (tiny). Compression middleware must pass `Flush()` through for SSE.
- The `Content-Encoding` for already-immutable files must not be re-applied by a generic middleware (double compression); the middleware must skip responses that already carry `Content-Encoding`.

### 3.5 Dev loop

Vite dev server proxies `/api` (and `/badges`, `/media`, `/llms.txt`) to the Go server (`server.proxy`, keys are prefix matches; extends http-proxy-3; Vite does not check WebSocket origin before proxying, S8). Keep `server.host` default `localhost`, `server.allowedHosts` default (never `true`: DNS rebinding exposes source, S8), `server.cors` default (never `true`, S8). CSP is **not** applied in dev (Vite HMR injects inline scripts); the prod CSP is exercised by a Playwright run against the built binary.

---

## 4. Content-Security-Policy for the SPA (item 5, adapted)

### 4.1 Policy (document responses: `index.html`)

```
Content-Security-Policy:
  default-src 'none';
  script-src 'self' 'nonce-{N}';
  style-src 'self';
  style-src-elem 'self' 'nonce-{N}';
  img-src 'self';
  font-src 'self';
  connect-src 'self';
  manifest-src 'self';
  base-uri 'none';
  form-action 'self';
  frame-ancestors 'none';
  object-src 'none';
  require-trusted-types-for 'script';
  trusted-types scp-html;
  report-to csp
Reporting-Endpoints: csp="/api/v1/csp-reports"
```
(Single line in practice; `Content-Security-Policy-Report-Only` first, see 4.5.)

Why each part:
- `default-src 'none'` then explicit allow = no accidental third-party origin; `connect-src 'self'` covers `fetch` and `EventSource`.
- `script-src 'self' 'nonce-N'`: module chunks load from `'self'`; only the inline theme script needs the nonce. `'strict-dynamic'` is **not** needed because Vite loads chunks by `import()`/`<link modulepreload>` (S9: strict-dynamic is only for nonce-approved scripts that inject scripts; it weakens the policy).
- `style-src 'self'` blocks inline `<style>` and **`style="..."` attributes in markup and `setAttribute('style')`/`cssText`**, but **allows `el.style.prop = ...` CSSOM writes** (S10). React DOM and Radix positioning write through CSSOM, so they work; sanitised README HTML never contains `style=` (we strip it) and CSP backs that up.
- `style-src-elem ... 'nonce-N'` is for the inline `<style>` tags some primitives inject (4.4). Falls back to `style-src` otherwise.
- `img-src 'self'` only: README images go through `/media` (8.5). Add `data:` only if Playwright shows a need; with `assetsInlineLimit: 0` there should be none (S6).
- `base-uri 'none'`, `object-src 'none'`, `frame-ancestors 'none'` (replaces X-Frame-Options, S9, S37), `form-action 'self'`.
- Trusted Types: with `require-trusted-types-for 'script'` plain strings to `innerHTML` throw; `trusted-types scp-html` restricts policy creation to the one named policy (S30, S9). **Whether React 19.3 itself is clean under enforcement was not verified** (one secondary article claims React 19.3 supports TrustedHTML values; I could not confirm in React's source): hence report-only first.
- `report-to` + `Reporting-Endpoints` is the current mechanism; `report-uri` is deprecated (S9). Endpoint: tiny handler that logs a metric and drops the body; rate-limited; it is a state-changing `POST` and must be a `CrossOriginProtection` bypass (browsers send reports without `Sec-Fetch-Site: same-origin`), body-capped at 16 KiB.

### 4.2 Non-document responses

| Response | CSP |
|---|---|
| `/api/*`, `/llms*.txt`, `readme.md` | `default-src 'none'; frame-ancestors 'none'; sandbox` |
| `/badges/*`, `/media/*` (images, incl. SVG) | `default-src 'none'; sandbox` (+ `style-src 'unsafe-inline'` only if an SVG needs `<style>`; our badges do not) |
| static assets | none needed |

`sandbox` makes a directly navigated SVG run without script even if somebody opens `/media/...svg` as a document; as an `<img>`, scripts are disabled and external resources cannot load anyway (S39 "SVG as an image").

### 4.3 The inline theme script: nonce vs hash vs external

Options and verdict:
1. **Per-request nonce injected by Go (recommended).** `index.html` contains the placeholder `__CSP_NONCE__` (E1); Go replaces it with 18 random bytes base64 (24-char) per response and sets the header. One mechanism covers the theme script, Vite's tags, and inline `<style>` of UI primitives. Cost: `index.html` (about 0.8 KB) is `no-cache` without ETag, trivial. No CDN caching of the HTML: acceptable (single binary, single replica).
2. Hash: `script-src 'sha256-...'` computed by Go at startup from the inline script text (static headers, ETag-able HTML). Works for the theme script only; does **not** cover `<style>` injected at runtime by primitives (their text is not in the HTML) unless you also hash those (S9: hash is of the exact text; brittle across upgrades).
3. External `theme-init.js` in `public/`: no nonce/hash for scripts, but it is a render-blocking extra request, not content-hashed (Vite does not hash `public/`), and `public/` copying needs care. Acceptable fallback if the owner dislikes dynamic `index.html`.

Theme script (kept under 400 bytes, E1 used exactly this and Vite left it intact):

```html
<meta name="color-scheme" content="light dark">
<script nonce="__CSP_NONCE__">(function(){try{var t=localStorage.getItem('scp-theme');
var d=t==='dark'||((!t||t==='system')&&matchMedia('(prefers-color-scheme: dark)').matches);
document.documentElement.classList.toggle('dark',d);document.documentElement.style.colorScheme=d?'dark':'light'}catch(e){}})()</script>
```
It writes `style.colorScheme` via CSSOM, which `style-src` permits (S10). The storage key must match the one the React `ThemeProvider` uses (shadcn default key is `vite-ui-theme`, S13; we override).

### 4.4 What shadcn/Radix/Base UI do under this policy

- shadcn components are copied source on **Radix or Base UI primitives**; since 2026-07-02 `shadcn init` picks **Base UI by default** (S14). Both can inject inline `<style>` elements:
  - Radix ScrollArea (and Select from 2.1.0 per changelog) expose a **`nonce` prop** "to be able to pass CSP nonce to the inline styles" (S11 confirms `nonce` on `ScrollArea.Root`; the changelog entry text came from a search snippet, so treat the Select claim as secondary).
  - Base UI: `ScrollArea.Viewport` and `Select.Popup/List` (with `alignItemWithTrigger`) inject a `<style>` to hide native scrollbars; configure with `<CSPProvider nonce={...}>` and put the nonce in `style-src-elem`, or set `disableStyleElements` and supply the CSS yourself (S12).
- Plan: the SPA reads the nonce once from `<meta property="csp-nonce">` (the same tag Vite uses, S6) and passes it to `CSPProvider`/`nonce` props in the shadcn wrapper components (one place: `components/ui/csp.ts`). Alternative that removes the dependency: `disableStyleElements` + hand-written scrollbar CSS.
- Other libraries that may inject `<style>`: Sonner toasts, charts, Mermaid. **UNVERIFIED** here; the Playwright CSP-violation listener (4.5) is the safety net. Mermaid in particular is risky (it emits `<style>` inside generated SVG and needs `securityLevel: 'strict'`); lazy-load it only for docs containing mermaid fences and test it under the policy before promising it (D-16 says "if supported").

### 4.5 Rollout and tests

1. Ship `Content-Security-Policy-Report-Only` with the full policy on day one; Playwright e2e registers `page.on('console')`/`securitypolicyviolation` and **fails on any violation** for every route including the docs page with all markdown features.
2. Flip to enforcing for scripts/styles once clean; enable `require-trusted-types-for` last.
3. Golden test: the exact CSP string for each response class is a unit test (a header regression is a security regression).

---

## 5. Other security headers (item 5)

Middleware order (outermost first): request id -> recover -> access log -> security headers -> trusted-proxy client IP -> rate limit -> `CrossOriginProtection` (non-GET /api) -> routing -> handler timeouts/body limits.

| Header | Value | Applies to | Source / reason |
|---|---|---|---|
| `X-Content-Type-Options` | `nosniff` | all | S37 |
| `Referrer-Policy` | `strict-origin-when-cross-origin` (consider `same-origin` when `access` mode is on) | all | S37 recommends setting explicitly |
| `Permissions-Policy` | `geolocation=(), camera=(), microphone=(), payment=(), usb=()` | HTML | S37 pattern: disable what you do not use; **drop `clipboard-write`** from the deny list if a "copy" button is used |
| `Cross-Origin-Opener-Policy` | `same-origin` | HTML | S37 |
| `Cross-Origin-Embedder-Policy` | **not set** | - | S37 lists `require-corp`, but nothing here needs cross-origin isolation and it would only add breakage |
| `Cross-Origin-Resource-Policy` | `same-origin` for HTML/API/media; **`cross-origin` for `/badges`** | per class | S39 (MDN): embedded badges "must allow cross-origin loading" with `cross-origin` |
| `Strict-Transport-Security` | `max-age=31536000` (no `includeSubDomains`/`preload` by default) | only when the request is HTTPS: `r.TLS != nil` or `X-Forwarded-Proto: https` from a **trusted** proxy, and `server.hsts=true` | S39 (MDN: ignored over HTTP, preload needs includeSubDomains + 1 year); OWASP shows 2 years + preload (S37), which is wrong for an app that cannot know its parent domain policy. Prefer the TLS-terminating proxy to own HSTS |
| `X-Frame-Options` | `DENY` on HTML (CSP `frame-ancestors 'none'` is the real control) | HTML | S37 |
| `X-XSS-Protection` | **not set** (or `0`) | - | S37 calls it obsolete and harmful |
| `Server` / `X-Powered-By` | removed | all | S37 |
| `Cache-Control` | classes: hashed assets `public, max-age=31536000, immutable`; HTML/`index` `no-cache`; API `no-cache` + ETag (revalidate every time, 304 when unchanged); badges `public, max-age=300` (see 7.5; `stale-while-revalidate` not verified); problem responses `no-store` | S18 (`no-cache` means "store but revalidate", `no-store` is for nothing-stored; `immutable` skips revalidation) |

`Vary: Accept-Encoding` wherever a response can be compressed; never `Vary: User-Agent` (S18).

Admin listener (`/status`, `/metrics`, `/debug/pprof/`): separate port, not routed by the ingress, no SPA; Go 1.27 now also publishes `/debug/pprof/goroutineleak` (S3), useful for D-10's goleak mindset.

---

## 6. Live updates for the "latest commits" panel (item 2, adapted)

### 6.1 Options compared

| | Polling + ETag/304 | SSE (`EventSource`) |
|---|---|---|
| Browser mechanics | `fetch` + HTTP cache revalidation: with `Cache-Control: no-cache` + `ETag` the browser sends `If-None-Match` itself and the page sees the cached body on 304 (S18); **no client code** | `new EventSource('/api/v1/events')`; auto-reconnect, `retry:` and `id:`/Last-Event-ID built in; comment lines keep the connection alive (S16) |
| TanStack Query | `refetchInterval`; polling **pauses when the tab loses focus** unless `refetchIntervalInBackground: true`; many observers share one in-flight request (S15) | invalidate-on-event pattern: `queryClient.invalidateQueries(...)` (client pattern, no server change to Query) |
| Connections | none held | one long-lived connection per tab; **HTTP/1.1: 6 per browser+domain across tabs; HTTP/2: negotiated streams, default 100** (S16) |
| Proxies | nothing special | nginx buffers by default (`proxy_buffering on`) - disable per response with `X-Accel-Buffering: no` or per location; `proxy_read_timeout` default 60 s between reads, so heartbeat < 60 s (S17) |
| Go server | none | must lift `WriteTimeout` per response (E5), drain on shutdown, flush through every wrapping middleware |
| Cost at target scale | 100 viewers x 2 req/min = ~3.3 req/s of 304s; negligible (arithmetic) | 100 goroutines + sockets; negligible but operational surface larger |
| Freshness | <= interval (30 s) after webhook | sub-second after webhook |

Data freshness actually available: webhook-driven refresh in seconds, scheduled commits refresh every 5 min (Q-09). A 30 s poll is already finer than the scheduled path and within a few tens of seconds of the webhook path.

### 6.2 Recommendation

- **v1: polling with ETag.** Endpoint `GET /api/v1/commits/latest?limit=N` returns `ETag: "commits-<snapshot generation>"` (opaque, derived from the immutable `Snapshot` version, not by hashing the body) and `Cache-Control: no-cache`; `If-None-Match` handled by `NotModified` (prototype, E5, tested with weak/strong lists and `*`). Body carries `asOf`, `fetchedAt`, `status` (freshness contract from D-16). The SPA shows a "stale" indicator when `now - asOf` exceeds the SLO.
- TanStack Query settings (v5.104.2 exists, S43; option semantics from S15): `refetchInterval: 30_000`, keep `refetchIntervalInBackground` false, `staleTime: 10_000`, `refetchOnWindowFocus` default true. The effect: a hidden tab does not poll; returning to the tab refetches immediately.
- **v1.1 (optional): SSE as an invalidation channel only.** Server sends `event: invalidate` with `{topics:["commits"],"generation":N}`; client invalidates queries and the ETag path does the data transfer. On `EventSource` error the client falls back to polling. Rationale: the API contract and ETag semantics stay identical, SSE failure cannot corrupt state, and the OpenAPI contract needs no streaming schema beyond the event shape. Do not stream payloads (breaks the "JSON API is the LLM surface" contract and complicates auth).
- Prefer `fetch` streaming over `EventSource` only if custom headers are ever required (EventSource cannot set them; MDN page does not say, standard knowledge: **UNVERIFIED in S16**). Trusted-proxy auth is cookie/proxy based, so `EventSource` is fine.
- htmx is out of scope (D-16).

### 6.3 SSE handler outline (tested, E5)

```go
func SSE(b Broker, shutdown <-chan struct{}) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        rc := http.NewResponseController(w)
        if err := rc.SetWriteDeadline(time.Time{}); err != nil { /* 500 problem */ return } // lift Server.WriteTimeout for this response only
        h := w.Header()
        h.Set("Content-Type", "text/event-stream"); h.Set("Cache-Control", "no-cache, no-transform")
        h.Set("X-Accel-Buffering", "no")             // nginx: do not buffer (S17)
        w.WriteHeader(200); fmt.Fprint(w, "retry: 5000\n\n"); rc.Flush()
        ch, cancel := b.Subscribe(); defer cancel()
        hb := time.NewTicker(20 * time.Second); defer hb.Stop()   // < nginx proxy_read_timeout 60 s (S17)
        for {
            select {
            case <-r.Context().Done(): return
            case <-shutdown: return                   // srv.RegisterOnShutdown(func(){ close(shutdown) })
            case msg := <-ch: fmt.Fprintf(w, "id: %d\nevent: invalidate\ndata: %s\n\n", next(), msg)
            case <-hb.C: fmt.Fprint(w, ": hb\n\n")
            }
            if rc.Flush() != nil { return }
        }
    }
}
```
Verified (E5): with `Server.WriteTimeout = 1s` the stream stayed open and delivered an event at 2.5 s; `Content-Type: text/event-stream`. Needed extras: per-IP connection cap (e.g. 4) via the rate-limit layer; `Broker` fan-out with bounded per-subscriber buffers that drop-and-coalesce (an invalidation is idempotent, so keeping only the latest generation is correct); compression middleware must not wrap this route.

### 6.4 ETag helper (tested, E5)

```go
func NotModified(w http.ResponseWriter, r *http.Request, etag string) bool {
    w.Header().Set("ETag", etag); w.Header().Set("Cache-Control", "no-cache")
    for _, c := range strings.Split(r.Header.Get("If-None-Match"), ",") {
        c = strings.TrimSpace(c)
        if c == "*" || strings.TrimPrefix(c, "W/") == strings.TrimPrefix(etag, "W/") { w.WriteHeader(304); return true }
    }
    return false
}
```
When a compression middleware changes the representation it must keep ETags distinct per encoding or use weak ETags (`W/`); the static handler above uses per-variant strong ETags.

---

## 7. Local SVG badges (item 4, unchanged scope)

### 7.1 Reference behaviour (Shields `badge-maker` 6.0.0, licence `(MIT OR Apache-2.0)`, Node >= 22; S20, E2)

Observed by rendering with the real library on 2026-10-13 (E2):

`flat-square` (no gradient, no blur, `shape-rendering="crispEdges"`):

```
<svg xmlns="http://www.w3.org/2000/svg" width="98" height="20" role="img" aria-label="issues: 12 open"><title>issues: 12 open</title><g shape-rendering="crispEdges"><rect width="43" height="20" fill="#555"/><rect x="43" width="55" height="20" fill="#ea7233"/></g><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110"><text x="225" y="140" textLength="330" transform="scale(.1)">issues</text><text x="695" y="140" textLength="450" transform="scale(.1)">12 open</text></g></svg>
```

Specification extracted (S20 code summary + E2):
- Height 20, horizontal padding 5 each side, `font-size="110"` text scaled by `.1`, font stack `Verdana,Geneva,DejaVu Sans,sans-serif`, width font `11px Verdana`.
- Text width = `anafanafo` Verdana 11px, truncated to integer then rounded **up to odd**; `textLength = width*10`. Label rect width = `labelTextWidth + 10`; message likewise.
- Text x: label `5*labelRect + 10`, message `10*labelRect + 5*messageRect - 10` (fits both E2 outputs: 37 -> 195, 51 -> 615; 43 -> 225, 55 -> 695).
- `flat` adds a vertical `linearGradient` overlay, rounded corner clip (`rx=3`) and a blurred text-shadow pair (`feGaussianBlur stdDeviation=16`); I recommend implementing **only `flat-square`** (smaller, no filters, no ids that can collide when inlined).
- Accessibility: root has `role="img"`, `aria-label="label: message"`, and a `<title>` with the same text (when no links) (E2). The README of badge-maker does not document this (S20).
- Escaping: `&lt; &amp; &quot; &apos; &gt;` all escaped in both attribute and text positions (E2 third sample).
- Named colours seen: brightgreen `#4b0`, orange `#ea7233`, blue `#007ec6` (E2); badge-maker also accepts hex and CSS colours (S20). We accept only a closed enum plus validated `#rrggbb`.

### 7.2 Text measurement in Go

- **Verdana is proprietary**; `anafanafo` ships pre-generated tables (MIT) with the note "All rights to Verdana are owned by Microsoft Corp" (S23). Embedding that table is a licensing question I cannot resolve: **UNVERIFIED legal position, owner/lawyer call**.
- Experiment E7: Verdana 11px advance sums via `golang.org/x/image/font/opentype` (unhinted, DPI 72) match anafanafo to <= 0.1 px on 15 test strings (e.g. "build" 26.72 vs 26.70, "last commit" 64.72 vs 64.72). **Go Regular is 6-16% narrower** (unsuitable). **DejaVu Sans 2.37.3 is within +/-3.2% of Verdana** on the same strings (worst: "2026-10-13" 63.94 vs 66.00, "licence" 38.72 vs 37.56; i.e. <= 2 px per text run). DejaVu's licence is Bitstream Vera (permissive; modified fonts must be renamed; "DejaVu changes are public domain", licence file in the npm package, E7).
- Recommendation: `go generate` tool reads `DejaVuSans.ttf` once (x/image v0.47.0, BSD, S44) and writes `widths.go`: `[95]uint16` of advance in 1/64 px for U+0020-U+007E plus an average for everything else (non-ASCII text should be rare; badges are generated from ASCII enum labels and short values). No font file or `x/image` dependency at runtime. SVG `textLength` forces the drawn width to the computed box, so a +/-2 px measurement error shows as slightly tighter/looser letter spacing, never overflow (E2 shows Shields itself relies on this).
- Add a golden test pinning ~30 strings to the table so regenerating cannot silently change badge widths.

### 7.3 Go library landscape (S21, S22, S44 - all 2026-10-13)

| Library | State | Verdict |
|---|---|---|
| `narqo/go-badge` | MIT, 61 stars, last push **2024-03-16**, tags up to v0.3, no GitHub releases; go.mod `go 1.17`, depends on `golang/freetype` (2017) and old `x/image`; measures with embedded **Bitstream Vera** and a hand-tuned `extraDx = 13` fudge (source read, S21); global drawer behind `init()` and a mutex; `html/template` for SVG | Do not adopt (init side effects, mutex on hot path, stale deps, fudge factor, flat only) |
| `essentialkaos/go-badge` | Apache-2.0, 23 stars, pushed 2026-10-12, release v1.4.3 (2026-06-20); styles flat, flat-square, plastic; **requires a Verdana.ttf path at runtime** (`NewGenerator("/path/to/Verdana.ttf", 11)`, S22) | Do not adopt: needs the proprietary font at runtime |
| `factorysh/go-badge` | MIT, last push 2022-01-24 | dead |
| `adequatedq/badge` | GPLv3 fork | licence incompatible with an Apache/MIT product |
| own renderer | ~120 LOC | **Recommended** |

### 7.4 Prototype (E7, `06-prototypes/badge`)

```go
type Badge struct{ Label, Message, LabelColor, MessageColor string } // colours validated #rrggbb
func Render(b Badge, measure func(string) float64) []byte             // text/template with a custom xmlEscape
func xmlEscape(s string) string // &,<,>,",' escaped; drops chars illegal in XML 1.0 (NUL, C0 controls, U+FFFE/FFFF)
```
Do not use `html/template` for SVG (HTML escaping of attribute context differs, and `&#34;` vs `&quot;` noise); use `text/template` + the explicit escaper, **plus** a test that parses every golden output with `encoding/xml`. Inputs are never free text from the URL: label comes from the badge-kind enum, message from catalog data (issue count, tag, licence SPDX id) and is length-capped (e.g. 40 runes) with an ellipsis.

### 7.5 Endpoint semantics

- `GET /badges/{repo}/{file}` where `file` is `<kind>.svg`; kinds: `issues`, `last-commit`, `release`, `licence`, `repos` (group-level: `/badges/_group/{groupId}/repos.svg` or query form - architecture decides), plus optional `build`/`security` from providers.
- `Content-Type: image/svg+xml`; `X-Content-Type-Options: nosniff`; `Content-Security-Policy: default-src 'none'; sandbox`; `Cross-Origin-Resource-Policy: cross-origin` (S39); no cookies.
- **Caching**: ETag = generation of the repo's data (not body hash), `Cache-Control: public, max-age=300` (aligned with the 5 min staleness SLO). `stale-while-revalidate` was **not verified** in the fetched MDN caching page (S18 did not cover it), so omit it. GitHub proxies README images via Camo, which caches them and may keep serving old images unless the origin sends `Cache-Control: no-cache` or the URL is PURGEd (S42); a short `max-age` is the compromise, and the tradeoff is a documented limitation. Camo also requires a content type on its allow-list and a publicly reachable URL (S42) - a portal on a private network cannot be badge-embedded in github.com READMEs at all.
- **Unknown repo/kind** -> HTTP **404** with a small grey "unknown" badge body (so `<img>` still shows something) and `Cache-Control: public, max-age=60`; do not return 200 for unknown (GR-16). **Known repo but data unavailable** -> 200 with a grey `unknown` message and `max-age=60`. Both bodies are generated, never a static file read per request.
- No SVG from third parties is ever served by this endpoint (GR-33); `Jenkins`/`Snyk` providers return structured data and we render the badge.
- Badge URLs are **served directly**, never redirected, because I could not verify that Camo follows redirects.

---

## 8. Untrusted Markdown: server sanitising and safe display (item 5)

### 8.1 Pipeline (verified end to end, E3)

```
README bytes (<= 1 MiB cap)
 -> goldmark/v2 (GFM: table [align as attribute], strikethrough, tasklist, footnote; WithAutoHeadingID; html.WithUnsafe() so raw HTML survives to the next stage)
 -> x/net/html fragment parse -> Rewrite pass (untrusted tree, never trusted output):
      a/href:  fragment -> namespaced; relative -> portal route; http/https/mailto keep; "//host" and everything else drop
      img/src: relative -> /media/{repo}/{ref}/{path}; https external -> /media/ext/{id}; else drop
      <source srcset>: rewrite each candidate; <img srcset>/sizes: DROP
      blockquote starting with [!NOTE|TIP|IMPORTANT|WARNING|CAUTION] -> <div class="markdown-alert markdown-alert-note"><p class="markdown-alert-title">Note</p>...
      id attributes (and #fragment hrefs): prefix "md-" (DOM clobbering)
 -> bluemonday policy (deny by default) LAST
 -> SafeHTML (distinct type; constructed only by the sanitiser package)
```
Order matters: **the sanitiser is the last transformation**, so a bug in the rewrite pass can only change which allowed URL survives, never inject markup. Content addressed by blob SHA, computed off the request path.

Facts about the building blocks:
- goldmark by default does not render raw HTML or dangerous URLs; `html.WithUnsafe` renders them as written; the README says to use a sanitiser such as bluemonday for untrusted content and that the GFM extension does not filter disallowed raw HTML (S25).
- **goldmark v2** is the current line (`github.com/yuin/goldmark/v2`, v2.1.6, 2026-09-27; v2 "still in early stages of release"; maintains one previous major for security fixes; API is `parser.New(parser.WithExtensions(...))`, `html.New(html.WithExtensions(...))`, `r.Render(&buf, src, doc)`; GFM = Table/Strikethrough/Linkify/TaskList, Footnote and DefinitionList separate, S25). `goldmark-highlighting/v3` v3.1.0 needs Go 1.27 and `chroma/v3 v3.0.0-alpha.5` (pre-release, S25, S44). Decision for the docs design: v2 stack needs Go 1.27 (current, S1) and an alpha chroma; v1 stack (`goldmark` v1 + `goldmark-highlighting/v2`) is the conservative alternative. Either way the sanitiser input is the same.
- GFM spec baseline: 0.29-gfm (2019-04-06) (S28). GitHub alerts (5 types) are **not** in goldmark core; my rewrite pass implements them in 30 lines on the HTML tree (E3 output shows the `markdown-alert` div and title), so no 1-star extension is needed (the only one found, `ZMT-Creative/gm-alert-callouts`, has 1 star; S44).
- `<details>`/`<summary>`, `<picture>`, `<kbd>`, `<sup>`/`<sub>` are in GitHub's own sanitiser allow-list reference (`html-pipeline` `sanitization_filter.rb`, S26: h1-h6, br, b, i, strong, em, a, pre, code, img, tt, div, ins, del, sup, sub, p, picture, ol, ul, table, thead, tbody, tfoot, blockquote, dl, dt, dd, kbd, q, samp, var, hr, ruby, rt, rp, li, tr, td, th, s, strike, summary, details, caption, figure, figcaption, abbr, bdo, cite, dfn, mark, small, source, span, time, wbr; `a` href; `img` src/longdesc/loading/alt; img/a protocols http/https/relative). GitHub doc says `<picture>` is supported (S27).

### 8.2 bluemonday: status and policy

Status (S24 + GitHub API, 2026-10-13): latest tag **v1.0.27**, tagged commit dated 2024-07-04; last commit on master 2025-04-04; go.mod pins `golang.org/x/net v0.26.0` and `go 1.19`; open PRs not merged since Feb 2026 (#224 bump x/net for a vulnerability, #227 exponential-time CSS DoS fix, #225 "Unreasonably long sanitization" issue reproducible on 1.0.27). Known historical CVEs: CVE-2021-29272 (fixed 1.0.5) and CVE-2021-42576 (fixed 1.0.16, only with user policies allowing `style`); I found no 2025-2026 advisory (searches only; **the Go vulndb was not queried directly**). Docs warn: never allow the `style` attribute (no CSS sanitiser) and never `AllowUnsafe` for `script`/`style`; parsing is token-based, forward-only, "does not repair bad nesting" (S24).

Judgement: acceptable **if** (a) the policy never enables `style` (the CSS DoS and CVE-2021-42576 classes are then unreachable), (b) the project bumps `golang.org/x/net` itself (E3 used v0.61.0 via `go get golang.org/x/net@latest`; MVS honours it), (c) a fuzz test guards the idempotency (fixpoint) property, and (d) there is a documented exit: the tokeniser-based allow-list is about 300 lines on `x/net/html` if bluemonday is abandoned. The report does not recommend DOMPurify-on-server equivalents; none exist in Go.

Policy outline (full tested source `06-prototypes/san/san.go`; E3):

```go
p := bluemonday.NewPolicy()                 // empty, deny by default - NOT UGCPolicy
p.AllowElements("h1".."h6","p","br","hr","blockquote","pre","code","em","strong","b","i","del","s","ins",
  "sup","sub","kbd","mark","details","summary","ul","ol","li","dl","dt","dd","table","thead","tbody","tfoot",
  "tr","caption","figure","figcaption","abbr","cite","q","small","time","section","div","span","picture")
p.AllowElements("a","img","source","th","td","input")
p.AllowAttrs("class").Matching(`^language-[A-Za-z0-9_+#.-]{1,32}$`).OnElements("code","pre")
p.AllowAttrs("class").Matching(reClass).OnElements("div","span","pre","code","ul","ol","li","p","section","a","table") // chroma, task-list, footnote, "markdown-alert markdown-alert-(note|...)" as ONE token (multi-token needs an explicit regex)
p.AllowAttrs("id").Matching(`^[A-Za-z0-9][A-Za-z0-9_:.-]{0,127}$`).Globally()
p.AllowAttrs("align").Matching(`^(left|right|center|justify)$`).OnElements("p","div","th","td","img","table")
p.AllowAttrs("href").OnElements("a"); p.AllowStandardURLs(); p.AllowURLSchemes("http","https","mailto")
p.RequireNoReferrerOnLinks(true); p.AddTargetBlankToFullyQualifiedLinks(true)   // adds rel="noreferrer noopener" target=_blank
p.AllowAttrs("src","srcset").OnElements("img","source")  // srcset is rewritten earlier; see findings
p.AllowAttrs("alt").Matching(`^.{0,300}$`).OnElements("img"); p.AllowAttrs("width","height").Matching(`^[0-9]{1,4}%?$`).OnElements("img")
p.AllowAttrs("type").Matching(`^checkbox$`).OnElements("input"); p.AllowAttrs("checked","disabled").OnElements("input") // task lists, disabled only
p.SkipElementsContent("script","style","iframe","object","embed","noscript","template","textarea","title","xmp","noembed","noframes","svg","math")
// NOT allowed anywhere: style attr, on* attrs, name, form, button, base, meta, link, svg, math
```

Corpus results (E3, 36 payloads; `javascript:` links in all casings/entities, `data:`, `<svg onload>`, mXSS `math/mtext/mglyph/style`, `noscript` breakout, `<xmp>`, `<textarea>`, forms, `base`, `meta refresh`, `link`, `details ontoggle`, DOM-clobber ids/names): all dangerous markup removed. **Findings that change the design**:
1. **bluemonday does not URL-validate `srcset`**: `<img srcset="javascript:alert(1) 1x">` passed through untouched (browsers do not execute `javascript:` in srcset, but arbitrary external URLs would load, bypassing image rewriting/tracking control). Fix in the rewrite pass: drop `img[srcset]`, rewrite `source[srcset]` candidates (E3 re-run: output `""` / candidates rewritten to `/media/...`).
2. **Protocol-relative `//evil.example/x` is treated as a relative URL and allowed** (and got `target=_blank`). The rewrite pass drops any `//` URL (E3 re-run: link removed).
3. Alerts are not rendered by goldmark (plain `<blockquote>` with literal `[!NOTE]`); implemented in the rewrite pass.
4. goldmark tables emit alignment as `style="text-align:..."` by default and bluemonday strips it; configure `extension.TableCellAlignAttribute` (`align="left"`; E3 output verified) and allow `align` on `th/td`.
5. `rel="noreferrer"` is also added to in-page `#fn:1` links by `RequireNoReferrerOnLinks` (cosmetic).
6. Idempotency/fixpoint fuzz: `Render(x)` re-sanitised must equal itself; 2,510,117 execs in 30 s found no violation (E3). This is the cheap mXSS canary; keep it in CI (`go test -fuzz` short run on PRs, longer nightly).
7. Performance: 532 KB of dense Markdown -> 1.5 MB HTML in 614 ms on this machine (E3). Typical READMEs (5-50 KB) are single-digit ms; still do it off the request path and cap input at 1 MiB with a render deadline.

### 8.3 Heading ids and DOM clobbering

Allowed `id` values can shadow `window.<id>`/`document.<id>`; `name` is stripped by the policy (E3: `name="cookie"` removed). Namespace ids with a prefix and rewrite same-document fragments consistently (design item implemented in the rewrite pass, not yet in the prototype). GitHub's `user-content-` id prefix is how GitHub does it; **I did not verify that in the fetched pages**.

### 8.4 How the SPA displays it (D-16: no client-side parsing of untrusted Markdown)

```tsx
// the ONLY component allowed to touch innerHTML (eslint no-restricted-syntax for dangerouslySetInnerHTML elsewhere)
const policy = window.trustedTypes?.createPolicy('scp-html', { createHTML: (s) => s }) // pass-through: input is server SafeHTML
export function SafeHtml({ html }: { html: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => { if (ref.current) ref.current.innerHTML = (policy ? policy.createHTML(html) : html) as string }, [html])
  return <div ref={ref} className="prose dark:prose-invert max-w-none" onClick={delegateLinkClicks} />
}
```
- Using a ref+`innerHTML` instead of `dangerouslySetInnerHTML` avoids depending on React's handling of `TrustedHTML` objects (unverified), and gives one auditable sink for the CSP `trusted-types scp-html` allow-list (S30).
- `delegateLinkClicks`: one click handler; same-origin `/repos/...` links call the router (`navigate`) when no modifier key is pressed; fragment links `scrollIntoView` (ids are namespaced); external links already carry `target=_blank rel="noreferrer noopener"` from the server, never from client code.
- The API returns `{ html: string, toc: [...], meta: {...}, sha, renderedAt }`; the HTML is an opaque string typed `SafeHtml` (branded) in the generated TS client.
- **DOMPurify**: not in the default stack. Reasons (S31, GitHub advisories API 2026-10-13): latest 3.4.17 (2026-10-12) and **six advisories published between 2026-08-03 and 2026-10-12** (IN_PLACE hook handling, SMIL attribute guard under `application/xhtml+xml`, SAFE_FOR_TEMPLATES bypass; one CVE for mutation XSS fixed in 3.3.2 per a secondary source, S31). The advisories mostly concern non-default modes, but the churn means a second sanitiser is a second moving part that needs its own upgrade SLA. Where it earns its place: if a future client-rendered HTML source appears (e.g. Mermaid SVG output, user-pasted preview). Then pin and use only default config with `RETURN_TRUSTED_TYPE`.
- **HTML Sanitizer API (`setHTML`)**: MDN says "Limited availability... not Baseline" (S29); Firefox 148 shipped it (2026-02, S29b secondary), Chrome/Safari status **unverified** (one secondary source says Chrome 146). Do not depend on it in 2026; revisit when Baseline.
- Mermaid/diagrams: see 4.4; render client-side from fenced blocks only after a CSP test.

### 8.5 Images and SVG

- `img-src 'self'`: all README images are same-origin. Two handlers, both reuse the `github` adapter and its token, so they work for private repos once `access` is on:
  - `GET /media/{repo}/{ref}/{path...}`: repo-relative image; fetched by blob SHA, cached in memory/disk by SHA, content type from an allow-list after sniffing (`image/png|jpeg|gif|webp|avif|svg+xml`), max 5 MiB, `Content-Disposition: inline`, headers per 4.2 (`sandbox`, `nosniff`, CORP same-origin), `Cache-Control: public, max-age=86400` + ETag(SHA).
  - `GET /media/ext/{id}`: external image URLs found in rendered READMEs (shields.io badges, CI badges, logos). `{id}` = hex sha256 of the URL; the URL set is recorded in the snapshot at render time, so **it is not an open proxy and needs no signing key** (an HMAC-signed query URL would need a stable key to survive the SQLite warm start of cached HTML). Fetch is `https` only, via the SSRF-guarded client (section 9), 5 s timeout, 2 MiB cap, no cookies, no `Referer`, no `Authorization`, response content type allow-list, redirects limited to 3 and re-validated by the dialer hook. This is the same model GitHub uses (Camo: images proxied, content-type allow-list, private-network images unreachable; S42). Config `docs.external_images: proxy|block`, default `proxy`.
- **SVG policy**: never inline third-party SVG into the DOM (sanitiser removes `<svg>`/`<math>`); only as `<img src>` (scripts disabled, no external subresources, S39 "SVG as an image"). Proxied SVG is served with `sandbox` CSP and `nosniff` so direct navigation is inert. Our own badges and icons are first-party.

### 8.6 Raw `.md` and `llms.txt`

`Content-Type: text/markdown; charset=utf-8` (or `text/plain` if a client misbehaves), `nosniff`, `Content-Security-Policy: default-src 'none'; sandbox`; raw Markdown is never HTML-rendered by the browser. Front matter parsing stays in the docs package.

---

## 9. SSRF protection for configured outbound URLs (D-09)

Threat model: (a) admin-configured URLs (GHES base URL, Jenkins, Snyk) - trusted but fat-fingered or compromised config; (b) **README-derived external image URLs** - fully attacker-controlled by anyone who can push a README.

Design (E8, verified live 2026-10-13):

```go
g := ssrf.New()                                         // code.dny.dev/ssrf v0.3.0 (MIT, 2026-07-07)
d := &net.Dialer{Timeout: 5*time.Second, Control: g.Safe}
tr := &http.Transport{DialContext: d.DialContext, Proxy: nil, ResponseHeaderTimeout: 5*time.Second, MaxResponseHeaderBytes: 64<<10}
c := &http.Client{Transport: tr, Timeout: 10*time.Second, CheckRedirect: func(_ *http.Request, via []*http.Request) error { if len(via) >= 3 { return http.ErrUseLastResponse }; return nil }}
```
Verified blocks (E8): loopback `127.0.0.1:<random port>` (port not 80/443), `[::1]:80` ("outside the IPv6 Global Unicast range"), `169.254.169.254` (cloud metadata; "denied by 169.254.0.0/16"). The hook runs **at connect time on the resolved IP**, so DNS rebinding and every redirect hop are covered (S32: "DNS resolution still takes place... the check happens at dial time").
Defaults (S32): IPv4 special-purpose registry ranges (0/8, 10/8, 100.64/10, 127/8, 169.254/16, 172.16/12, 192.168/16, 224/4, 240/4) and IPv6 outside 2000::/3 blocked; ports 80/443 and tcp4/tcp6 only. Options `WithAllowedV4Prefixes`, `WithPorts`, `WithAnyPort`, ...
Rules:
1. **Two clients, not one**: `untrustedClient` (external images; guard on, no allow-list) and `integrationClient(name)` per configured integration built with an **explicit** allow-list entry from config (`jenkins.allow_private_cidrs: [10.20.0.0/16]`, `jenkins.ports: [8080]`), validated at startup, logged at startup. Jenkins on port 8080 on a private IP is blocked by default (E8 shows port and range both deny); the config must say so out loud.
2. `Proxy: nil` on guarded transports: with an HTTP proxy the dial target is the proxy, so the hook would see the proxy IP, not the destination (**standard behaviour, not verified in S32**).
3. Scheme check before dialing (`https` for untrusted; `http` allowed only for configured private integrations), reject userinfo in URLs, reject URLs whose host is an IP literal for untrusted fetches.
4. Never forward credentials across hosts: GitHub token is attached only by the `github` adapter to the configured GitHub base URL (GR-13, D-04); the media proxy never adds `Authorization`.
5. Alternative `doyensec/safeurl` v0.2.5 (Apache-2.0, 116 stars, 2026-06-11, S32/S44) wraps the whole client with allow/deny lists; it panics if a custom dialer is set on the transport. The Control-hook library composes better with our per-upstream transports (D-04).

---

## 10. Trusted-proxy identity and client IP (D-09, Q-03)

Principles (S33 adam-p, S34 oauth2-proxy, S33b realclientip-go):
- The leftmost `X-Forwarded-For` value is attacker-controlled; the only trustworthy entry is the first one added by a proxy you control, found by walking **right to left** and skipping addresses in your trusted set; merge **all** `X-Forwarded-For` header lines first; trusted-proxy ranges beat a hop count because chains change.
- If the server is reachable directly, forwarded headers must be ignored: **check `RemoteAddr` is a trusted proxy first**.
- oauth2-proxy sends `X-Forwarded-User/-Email/-Preferred-Username/-Groups` upstream (`--pass-user-headers`, default true) and `X-Auth-Request-*` as response headers for nginx `auth_request`; **an unset `--trusted-proxy-ip` defaults to trusting every source** and is unsafe; `--skip-auth-strip-headers` (default true) strips client-supplied copies (S34).
- `realclientip/realclientip-go` v1.0.0 (0BSD, 2022-04-03; repo still pushed 2026-10-12; pkg.go.dev says a newer module version exists, **not investigated**) offers `RightmostTrustedRangeStrategy`; it is small enough to own. My implementation (E6, `06-prototypes/httpapi`) is ~40 lines with `net/netip`.

Behaviour table verified by tests (E6): peer trusted + `XFF: 203.0.113.9` -> that IP; spoofed leftmost `1.2.3.4, 203.0.113.9` -> `203.0.113.9`; chain `203.0.113.9, 10.0.0.7` (second hop trusted) -> `203.0.113.9`; **untrusted peer sending XFF -> peer IP, header ignored**; garbage XFF from trusted peer -> falls back to the proxy IP (fail closed for per-client limits); IPv4-mapped IPv6 peer handled by `Unmap`; IPv6 rate keys bucket by /64 (`2001:db8:1:2:aaaa::1` and `...:bbbb::9` share a key).

`internal/access` contract:
```yaml
access:
  mode: public            # public | trusted-proxy
  trusted_proxies: ["10.0.0.0/8", "fd00::/8"]   # required and non-empty if mode != public; 0.0.0.0/0 is rejected by validation
  identity:
    user_header:   X-Forwarded-User              # or X-Auth-Request-User
    email_header:  X-Forwarded-Email
    groups_header: X-Forwarded-Groups
    proxy_secret_header: X-SCP-Proxy-Secret      # optional defence in depth (value from *_FILE secret), compared in constant time
```
Rules: identity headers are honoured only if the TCP peer is in `trusted_proxies` (and the secret matches when configured); otherwise they are **dropped from the request before any handler sees them**, counted in `scp_identity_header_spoof_total`, and the request proceeds as anonymous. `github.repository.visibility = private|all` + `mode = public` is a startup error (04 Q-03). The proxy must be documented to **overwrite** (not append) identity headers; nginx/oauth2-proxy snippet goes in the ops docs. Do not run the identity-trust logic on the admin listener.

Operational warning: behind an ingress with no `trusted_proxies` configured, every client appears as the ingress IP and one noisy client rate-limits everyone. Startup check: if peers are private addresses, XFF is present, and `trusted_proxies` is empty, log a loud warning and expose a `/status` flag.

---

## 11. Rate limiting for public endpoints

- Tools: `golang.org/x/time/rate` v0.16.0 (BSD-3, 2026-08-19): one token bucket per `Limiter`, **no built-in per-key map or eviction** (S35). `go-chi/httprate` v0.16.1 (MIT, 2026-10-08): sliding-window counter, local memory, `WithKeyFuncs`, `WithLimitHandler`; its README deprecates `KeyByRealIP`/`KeyByIP` because they trust spoofable headers or collapse clients behind a proxy, and recommends resolving the client IP first and bucketing IPv6 by /64 (S35). Both match my design; because stdlib-first (D-05) and one custom key function, **own ~80 line keyed limiter** over `x/time/rate`: sharded map keyed by `RateKey(ip)`, entry TTL sweep every minute, hard cap on keys (e.g. 50k; when full, shed to a shared "overflow" bucket rather than allocate), so an attacker rotating IPv6 /64s cannot exhaust memory.
- Suggested classes (tunable config, defaults for a 1,000-repo / small-org portal): `/badges` 120/min burst 60 (cheap, cacheable); `/api/v1` 300/min burst 100; `/media/ext` 60/min burst 20 (triggers outbound fetch); `/api/v1/events` 4 concurrent streams per IP; webhook: **no IP limit** on valid HMACs, but 10 invalid-signature attempts/min/IP, then 429; `/livez`,`/readyz` unlimited from loopback/probe CIDR.
- Response: `429` `application/problem+json` (type `.../rate-limited`) with `Retry-After` (RFC 9110). **Do not emit `RateLimit-*` headers yet**: the IETF draft `draft-ietf-httpapi-ratelimit-headers` is at version 11 (2026-05-23), expires 2026-11-24, "I-D Exists", defines `RateLimit` and `RateLimit-Policy` (not the older `RateLimit-Limit/Remaining/Reset`), and an early directorate review of v10 rated it "Not ready" (S36).
- Expose `scp_ratelimit_rejected_total{class}` and the overflow bucket usage as metrics.

---

## 12. Request size limits and server timeouts

`http.Server` (public): `ReadHeaderTimeout: 5s`, `ReadTimeout: 30s`, `WriteTimeout: 30s` (lifted per response for SSE with `ResponseController.SetWriteDeadline`, E5), `IdleTimeout: 120s`, `MaxHeaderBytes: 64<<10` (default is 1 MB, S2), `MaxHeaderValueCount` left at default 500 (new field, S2/S3). `ReadHeaderTimeout` semantics: "the read deadline resets after the headers are read, so handlers decide what counts as too slow for the body" (S2) - hence per-handler body caps.

Body caps via `http.MaxBytesReader` (returns `*http.MaxBytesError`, closes the connection if possible, S2): webhook 26 MiB (GitHub caps payloads at 25 MB, S44) but **verify HMAC before JSON parsing and read into a bounded buffer**; CSP reports 16 KiB; any other POST/PUT 1 MiB (there are none in v1); GET requests with a body are rejected (400). Query strings capped by `MaxHeaderBytes`; API `limit` parameters clamped by the OpenAPI schema (`maximum`) and re-checked in the handler.

---

## 13. CORS and CSRF

**CORS posture**: production is same-origin (SPA, API, badges on one origin) -> **no CORS headers at all**, no `OPTIONS` handling (GR-18). Dev: Vite proxies `/api`, so the browser still sees one origin (S8). If a cross-origin consumer is ever required (e.g. another internal site reading `/api/v1`), add an explicit exact-Origin allow-list for GET-only `/api/v1` (`jub0bs/cors` MIT, pushed 2026-10-12, or `rs/cors` MIT 2026-10-08, S44, both maintained; or 20 lines) - never `*` with credentials, never on `/webhooks`. Badges and `llms.txt` are fetched via `<img>`/navigation (no CORS needed); CORP `cross-origin` handles embedding (S39).

**CSRF**:
- State-changing public endpoints in v1: only `POST /webhooks/github` (authenticated by HMAC, not a browser; CSRF-irrelevant) and the CSP report sink. There is no cookie-authenticated mutation, so there is no CSRF surface today.
- **If a trusted-proxy SSO cookie sits in front, it is automatically attached to same-origin fetches**, so any future `POST/PUT/PATCH/DELETE` under `/api` would be cookie-authenticated from the app's point of view. Defence by default: wrap the whole mux with `http.CrossOriginProtection` (Go 1.25, S5/S2; **non-safe cross-origin browser requests rejected using `Sec-Fetch-Site` or Origin-vs-Host; GET/HEAD/OPTIONS always allowed; requests with neither header are assumed same-origin/non-browser**). Verified (E9): `POST /api/v1/x` with no headers -> 204, `Sec-Fetch-Site: same-origin` -> 204, `Sec-Fetch-Site: cross-site` -> **403**, `Origin: https://evil.example` vs Host -> **403**, and `cop.AddInsecureBypassPattern("POST /webhooks/github")` lets the webhook through even when cross-site.
- OWASP CSRF guidance (S38): Fetch Metadata and Origin verification need a fallback; custom request header for SPAs works because cross-origin custom headers trigger a preflight; SameSite is defence in depth only; "missing headers and `Origin: null` are not evidence of same-origin". Therefore for any future mutation endpoint also **require `Content-Type: application/json` and a custom header** (`X-SCP-Request: 1`) and use `SameSite=Lax` on any cookie the app itself might set. Header-based auth that scripts must attach explicitly is not auto-sent by browsers, so the classic CSRF mechanism does not apply to it; OWASP's page does not state immunity outright (S38) and XSS defeats every CSRF defence, which is why Section 4/8 matter more.
- Vite dev proxy: `rewriteWsOrigin` bypasses the origin check and can open CSRF (S8) - not used.

---

## 14. RFC 9457 problem details

Facts (S19): RFC 9457 (July 2023) obsoletes RFC 7807; media types `application/problem+json` / `+xml`; members `type` (URI reference, default `about:blank`), `title`, `status` (advisory; must match the HTTP status), `detail` (occurrence-specific, clients must not parse it), `instance`; extension members are allowed and **clients must ignore unknown ones**; avoid stack traces and implementation detail; IANA "HTTP Problem Types" registry; validation example uses an `errors` extension array with JSON-Pointer `pointer`.

Design:
- One `httpapi.Problem` struct (E5 prototype): `type,title,status,detail,instance` + extensions `code` (stable machine code), `errors[]{pointer,detail}`, `retryAfterSeconds`. `Content-Type: application/problem+json`, `Cache-Control: no-store`.
- Type URIs: relative references under the app origin are legal (`/problems/not-found`); recommended: stable `type` = `https://<configurable base>/problems/<slug>` and `code` = the same slug for machine use. Catalogue (OpenAPI `components/responses`): `validation-failed` 400/422, `not-found` 404, `method-not-allowed` 405, `rate-limited` 429, `payload-too-large` 413, `unauthorized`/`forbidden` 401/403, `upstream-unavailable` 503 (GitHub down; with stale data still 200 and `status: stale` in body), `internal` 500 (no detail).
- The OpenAPI document declares one shared `Problem` schema and a `default` response referencing it for every operation; contract tests (D-16) assert 4xx/5xx bodies validate against it. The generated TS client exposes a typed `ProblemError`; the SPA maps `code` to localised messages (never renders `detail` raw).
- ServeMux's built-in 404/405 plain-text bodies must be replaced: wrap the mux with a `ResponseWriter` that rewrites default `text/plain` 404/405 bodies for `/api/` into problem+json, or register explicit method-less `/api/` catch-all (done in E4) and handle 405 via a `Allow`-aware wrapper. **The 405 path inside `/api/v1` was not prototyped**.
- Panics -> recover middleware -> `internal` problem with request id in `instance` (`urn:uuid:<request id>`).

---

## 15. Test and verification checklist (security-relevant)

1. CSP golden strings per response class; Playwright CSP-violation gate on all routes including docs with every Markdown feature.
2. Sanitiser: the 36-payload corpus from E3 as a table test, plus fixpoint fuzz, plus a differential check that output contains no element/attribute outside the allow-list (parse output with `x/net/html` and walk).
3. Rewrite pass: table tests for `//host`, `javascript:`, `data:`, relative with `..` escaping root, `srcset` with commas in URLs, id/fragment namespacing.
4. Route table: startup registration test (panics on conflict), link-crawl replaced by typed-route + e2e (D-16); test that `/api/nope`, `/assets/missing.js`, `/x.png` never return HTML.
5. Static handler: ETag/304 per encoding, `Vary`, `q=0`, HEAD, Range (ServeContent), immutable header only under `assets/`.
6. Client IP matrix (E6) as a unit test; identity-header stripping test; config validation rejecting `0.0.0.0/0` trusted proxies and private visibility without auth.
7. SSRF: loopback, link-local metadata, IPv6 loopback, redirect to private, DNS rebinding with a fake resolver, port 8080 blocked unless allowed.
8. SSE: survives `WriteTimeout`, closes on shutdown, `goleak` clean, heartbeats, never wrapped by gzip.
9. Badge: golden SVGs parse as XML; XML-hostile input; all colours >= 4.5:1 against white; widths within +/-2 px of the Verdana reference for the golden string set.
10. `CrossOriginProtection` matrix (E9) plus webhook bypass.
11. Body limits: oversize webhook -> 413 problem; slowloris on headers handled by `ReadHeaderTimeout`.

---

## Corrections to the decision record (things the web/experiments show to be wrong or outdated)

1. **04 D-12 `GET /badges/{repo}/{kind}.svg`** is not a valid `ServeMux` pattern (wildcard must be a whole segment; panics at registration, E11). Use `/badges/{repo}/{file}` and cut `.svg` in the handler.
2. **04 D-12 `POST /webhooks/github`** must be registered with the `POST` method; a method-less pattern conflicts with the SPA catch-all `GET /{path...}` and panics (E11).
3. **04 D-12 / D-16 "static under `/assets/{hash}/...`"**: Vite 8 emits `/assets/<name>-<hash>.<ext>` (E1). Adjust the route text to `/assets/{file}`.
4. **04 D-16 "inline theme pre-paint handled with a hash/nonce"**: correct, but incomplete: UI primitives (Radix ScrollArea/Select, Base UI ScrollArea/Select) can inject inline `<style>` elements that also need a nonce in `style-src-elem` (S11, S12). A hash of the theme script alone is not enough.
5. **04 D-16 "shadcn/ui ... built on Radix UI/Base UI primitives"**: since 2026-07-02 `shadcn init` defaults to **Base UI** (S14); the CSP provider API differs between the two (S11 vs S12). The frontend research should pick one and the CSP wiring follows.
6. **04 D-06 "goldmark + chroma"**: goldmark's current major is **v2** (`github.com/yuin/goldmark/v2`, v2.1.6; "v2 is still in the early stages of release", S25) and its highlighting extension v3 requires **Go 1.27** and a **pre-release chroma v3 (alpha)**. The decision record does not mention the module-path change. The docs-rendering design must choose v1+extensions v2 (stable, older) or v2+v3 (current, needs Go 1.27 and alpha chroma).
7. **`microcosm-cc/bluemonday` as the sanitiser, mandatory on write**: right choice, but it is **not actively maintained**: last release 2024-07-04, last commit 2025-04-04, security-relevant PRs (x/net bump, exponential CSS DoS) unmerged since 2026-02/09 (S24, S44); it pins `x/net v0.26.0` and `go 1.19`. Keep it with the mitigations in 8.2 and record an exit plan.
8. **D-05 "HTMX vendored for the live panel"**: already superseded by D-16; the JSON+ETag design also means a live update never transfers a full HTML document per tick (GR-40).
9. **Route table and crawl test (GR-15) on the Go side**: partly replaced by typed routes in the SPA, but the Go side still needs the registration-time conflict guarantee and a no-HTML-for-API test (section 15).
10. **Badge URLs carry no branch segment**: branch is meaningless for badges derived from default-branch data, and keeping it out of the path means branches containing `/` never need to be addressable (GR-10, GR-33).
11. **Statement in the task brief that `narqo/go-badge` is a candidate library**: it is the most visible Go option but should not be adopted (section 7.3).
12. **Go 1.26 changed ServeMux trailing-slash redirects to 307** (S4): redirect tests must expect 307, not 301.

## Risks

1. **CSP + UI primitives**: inline `<style>` injection by third-party UI code is the main CSP-breakage vector; a library upgrade can add a new injection. Mitigation: nonce wiring + Playwright violation gate (4.5).
2. **Trusted Types enforcement** may break React internals or a library; enforce last, keep report-only telemetry.
3. **Sanitiser supply chain**: bluemonday is near-stalled; a future Go/`x/net` HTML parsing change could introduce a mXSS gap. Mitigations: deny-by-default policy, no `style`, fuzz fixpoint, CSP as second line, documented exit.
4. **Image proxy** adds an outbound fetch surface triggered by repository content (SSRF, bandwidth, cache growth). Mitigations: guarded client, allow-listed URL set, size/time/type limits, per-IP limits, optional `block` mode.
5. **Per-request nonce makes `index.html` uncacheable** and ties the Go handler to the Vite placeholder; a build that forgets `html.cspNonce` yields a page whose scripts are blocked. Mitigation: a Go unit test that asserts the built `index.html` contains the placeholder N times and the theme script; `go test` placeholder file must too.
6. **Badge width approximation** (DejaVu vs Verdana) differs by up to ~2 px from Shields; mixed rows of Shields and SCP badges can look uneven. Acceptable; documented.
7. **Camo and redirects/caching**: README badges via github.com are fetched and cached by Camo (S42); long `max-age` delays freshness there, and a private-network portal is unreachable by Camo. Redirect-following by Camo is unverified, hence the "serve badge URLs directly, never redirect" rule.
8. **Single-origin assumption**: no CORS means a future separate frontend host needs a design change (acceptable per D-16).
9. **Soft 404 for unknown dynamic ids** (`/repos/does-not-exist`) unless the Go side learns catalogue membership (rejected for coupling).
10. **Dependencies new to the plan**: `code.dny.dev/ssrf` (v0.3.0, 10 stars, one maintainer) and optionally `klauspost/compress`; both small, pinnable, replaceable by ~100 lines each.

## Unverified

- Whether React 19.3 works unmodified under `require-trusted-types-for 'script'` (only a secondary article claims support); whether Sonner/charts/Mermaid satisfy `style-src-elem` with nonce.
- Radix **Select** nonce prop (only from a search snippet of the Radix release notes; ScrollArea `nonce` confirmed on the docs page, S11).
- Chrome and Safari status of the HTML Sanitizer API (only Firefox 148 shipping is corroborated by a Mozilla Hacks post title and secondary text).
- That Camo follows redirects and its exact cache lifetime; GitHub's `user-content-` id prefix behaviour; GitHub webhook response-time limit (not on the fetched page).
- Legal status of reusing Verdana-derived advance widths (anafanafo tables) in our repo; DejaVu-derived table is the recommended safe path but I did not obtain legal advice.
- Runtime cost of Go-side brotli at quality 11 (my benchmark did not complete); precompression at build time avoids the question.
- `nginx proxy_http_version` default changing to 1.1 in 1.29.7 (stated by the fetched page; did not cross-check the changelog). The SSE advice does not depend on it (set `proxy_http_version 1.1` explicitly).
- `realclientip-go` newer module version (pkg.go.dev flagged one); the DOMPurify "CVE-2026-65914" identifier came from an aggregator (the GitHub advisories API list, S31, is the primary evidence used).
- The route-manifest idea (`routes.json` from the TanStack route tree) is a design proposal; generated route file names and a supported way to enumerate routes were not confirmed.
- 405 handling inside `/api/v1` and its problem+json wrapper not prototyped; HSTS deployment behind specific proxies not tested.
- Svelte 5 comparison: out of scope here (D-16 records it as an ADR item); nothing in this report depends on it except 4.4.

---

## Source table (all fetched or queried 2026-10-13)

| ID | URL | Used for |
|---|---|---|
| S1 | https://go.dev/doc/devel/release | Go versions/support |
| S2 | https://pkg.go.dev/net/http | ServeMux patterns, CrossOriginProtection, ResponseController, MaxBytesReader, Server timeouts |
| S3 | https://go.dev/doc/go1.27 | Go 1.27 net/http/json/pprof notes |
| S4 | https://go.dev/doc/go1.26 | 307 redirects, ReverseProxy.Director deprecation |
| S5 | https://go.dev/doc/go1.25 | CrossOriginProtection introduction |
| S6 | https://vite.dev/guide/features.html#content-security-policy-csp | `html.cspNonce`, data: warning |
| S7 | https://vite.dev/config/build-options | assetsInlineLimit, modulePreload, sourcemap, target |
| S8 | https://vite.dev/config/server-options | proxy, allowedHosts, cors, ws origin |
| S9 | https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/CSP | nonce/hash/strict-dynamic/report-to/Trusted Types directives |
| S10 | https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/style-src | inline style attribute vs CSSOM behaviour |
| S11 | https://www.radix-ui.com/primitives/docs/components/scroll-area | ScrollArea `nonce` prop |
| S12 | https://base-ui.com/react/utils/csp-provider | Base UI inline style tags, CSPProvider, disableStyleElements |
| S13 | https://ui.shadcn.com/docs/dark-mode/vite | shadcn ThemeProvider (no pre-paint script) |
| S14 | https://ui.shadcn.com/docs/changelog (and /docs/changelog/2026-07-base-ui-default) | Base UI default 2026-07-02 |
| S15 | https://raw.githubusercontent.com/TanStack/query/main/docs/framework/react/guides/polling.md ; https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults | polling semantics |
| S15b | https://tanstack.com/router/latest/docs/framework/react/guide/path-params | `$param` syntax |
| S16 | https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events | SSE format, connection limits |
| S17 | https://nginx.org/en/docs/http/ngx_http_proxy_module.html | proxy_buffering, X-Accel-Buffering, proxy_read_timeout |
| S18 | https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Caching | no-cache/no-store/immutable/ETag/Vary |
| S19 | https://www.rfc-editor.org/rfc/rfc9457.html | problem details |
| S20 | https://github.com/badges/shields/tree/master/badge-maker (+ badge-maker/lib/badge-renderers.js, badge-maker/package.json via GitHub API) | badge spec, version 6.0.0, licence |
| S21 | https://github.com/narqo/go-badge (+ source via GitHub API) | go-badge state/internals |
| S22 | https://github.com/essentialkaos/go-badge (GitHub API) | go-badge alternative |
| S23 | https://github.com/metabolize/anafanafo | Verdana width tables, licence note |
| S24 | https://pkg.go.dev/github.com/microcosm-cc/bluemonday ; GitHub API for microcosm-cc/bluemonday (tags, commits, issues, go.mod) | bluemonday status and API |
| S25 | https://github.com/yuin/goldmark ; GitHub API (go.mod, tags of goldmark, goldmark-highlighting, goldmark-meta) | goldmark v2 |
| S26 | https://raw.githubusercontent.com/gjtorikian/html-pipeline/main/lib/html_pipeline/sanitization_filter.rb | GitHub-style allow-list reference |
| S27 | https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax | alerts, task lists, picture |
| S28 | https://github.github.com/gfm/ | GFM spec version |
| S29 | https://developer.mozilla.org/en-US/docs/Web/API/HTML_Sanitizer_API (S29b: https://hacks.mozilla.org/2026/02/goodbye-innerhtml-hello-sethtml-stronger-xss-protection-in-firefox-148/ via search) | Sanitizer API status |
| S30 | https://developer.mozilla.org/en-US/docs/Web/API/Trusted_Types_API | Trusted Types status/directives |
| S31 | GitHub advisories API for cure53/DOMPurify (queried with `gh api`), releases API | DOMPurify churn |
| S32 | https://pkg.go.dev/code.dny.dev/ssrf ; https://github.com/doyensec/safeurl | SSRF libraries |
| S33 | https://adam-p.ca/blog/2022/03/x-forwarded-for/ ; https://pkg.go.dev/github.com/realclientip/realclientip-go | XFF guidance |
| S34 | https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview/ | identity headers, trusted proxies, stripping |
| S35 | https://pkg.go.dev/github.com/go-chi/httprate ; https://pkg.go.dev/golang.org/x/time/rate | rate limiting libraries |
| S36 | https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/ | RateLimit header draft status |
| S37 | https://cheatsheetseries.owasp.org/cheatsheets/HTTP_Headers_Cheat_Sheet.html | header values |
| S38 | https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html | CSRF defences |
| S39 | https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cross-Origin-Resource-Policy ; .../Strict-Transport-Security ; https://developer.mozilla.org/en-US/docs/Web/SVG/Guides/SVG_as_an_image ; .../Web/CSS/color-scheme ; .../Web/CSS/color_value/light-dark | CORP, HSTS, SVG-as-image, color-scheme |
| S40 | https://www.w3.org/WAI/standards-guidelines/wcag/new-in-22/ | WCAG 2.2 |
| S41 | https://caniuse.com/zstd ; https://caniuse.com/brotli | compression support |
| S42 | https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/about-anonymized-urls | Camo |
| S43 | `npm view <pkg> version time.modified` run 2026-10-13 (react, vite, tailwindcss, @tanstack/*, react-router, zod, typescript, lucide-react, @fontsource-variable/inter, @base-ui/react, radix-ui, @radix-ui/react-scroll-area, vite-plugin-compression2, dompurify, badge-maker, anafanafo) | versions |
| S44 | GitHub REST API via `gh api` (repo metadata, tags, releases, issues) for the repos named in the text; https://docs.github.com/en/webhooks/webhook-events-and-payloads (25 MB cap) | maintenance status, webhook limits |

Experiments (2026-10-13, scratch module; copies of the Go prototypes in `05-research/06-prototypes/`):
E1 Vite 8.3.4 + Tailwind 4.3.3 + React 19.3.0 build and compression sizes; E2 `badge-maker@6.0.0` reference SVGs; E3 goldmark/v2 + `x/net/html` rewrite + bluemonday v1.0.27 corpus, idempotency fuzz (2.5M execs) and timing; E4 SPA handler + ServeMux routing tests; E5 SSE/ETag/problem helpers; E6 trusted-proxy client IP matrix; E7 text-width comparison Verdana vs Go Regular vs DejaVu vs anafanafo and the flat-square renderer; E8 `code.dny.dev/ssrf` live blocks; E9 `http.CrossOriginProtection` behaviour; E10 WCAG contrast computation; E11 ServeMux pattern validity/conflict/escaping tests.
