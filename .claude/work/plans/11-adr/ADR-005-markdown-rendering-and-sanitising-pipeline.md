# ADR-005: Markdown rendering and sanitising pipeline producing `SafeHTML`

Status: Accepted. Date: 2026-10-14. Refines 04 D-06 (A-06).

## Context
README and docs are attacker-influenced (anyone who can push). Emitting unsanitised HTML from such sources is a stored-XSS vector, so trusted HTML must be a type that only the sanitiser can mint (GR-32). D-06/D-16 make the backend the single Markdown pipeline and forbid client-side parsing of untrusted Markdown.

## Decision
One pipeline in `docs/internal/render` (goldmark and chroma, G6) whose last step is `safehtml.Sanitize` (bluemonday, G6):
1. Split front matter in-house (five portal keys; unknown keys preserved, never interpreted).
2. goldmark v1.8.6 with GFM, footnotes and `WithUnsafe` (needed for `<details>`/`<img>`; sanitising afterwards is therefore mandatory), in-house GitHub-compatible heading ids and in-house alert transform to `div.md-alert` (no inline SVG).
3. AST rewrite: relative links and images resolved against the commit SHA (indexed docs become portal routes, other files GitHub blob URLs, images `/media/...`).
4. Code highlighting with chroma v2.27.0, always classes (`WithClasses(true)`), `mermaid` fences kept as `language-mermaid`.
5. `x/net/html` rewrite pass for the GitHub-rendered AsciiDoc HTML bridge and for id namespacing (`doc-` prefix against DOM clobbering).
6. bluemonday v1.0.27 last, inside `safehtml.Sanitize`: `UGCPolicy` extended with a checkbox, class and id allow-list; never `style`, `svg`, `iframe`; external links `rel="noopener noreferrer nofollow ugc"`.
7. Output type `safehtml.SafeHTML` lives in the rank-0 package `safehtml` next to the sanitiser; `Sanitize` is its **only** constructor (no `Mint`, no `UnmarshalJSON`). API DTOs receive `SafeHTML.String()`.
Cache key is blob SHA plus a pipeline version; sanitisation runs in the sync job under a deadline with input caps (document 512,000 bytes truncated with a notice, README over 1 MiB typed `TooLarge`).

## Alternatives considered
goldmark v2 (2.1.6 exists but alerts need v2 and highlighting needs chroma v3 alpha: revisit after the criteria in 01 section 7); a second sanitiser (DOMPurify) in the SPA (rejected: silently alters golden-tested output); sanitise-then-render order (unsafe); a local AsciiDoc engine (Q-06).

## Consequences
bluemonday is slow-moving with open CSS-handler DoS reports, so the policy is constrained, input and time bounded, fuzzed and covered by a 36-payload corpus plus a differential allow-list walk; a fallback sanitiser plan is a documented open item (risk W10). The pipeline version is part of every cache key so a policy change re-renders safely.

## Evidence
`05-research/01-docs-format-llm.md` sections 2.8, 4.4; `05-research/06-web-ui-badges-security.md` section 8; `05-research/02-go-stack-currency.md` sections 3.3 and 7.

## Exit condition
Move to goldmark v2 when its release line has 60 days without parser-bug patches and our four in-house pieces port cleanly.

## Amendment 2026-10-14 (review revision)

`SafeHTML` moved out of `docs/internal/render` into the rank-0 package `safehtml` (F-18): `pages` and `overview` can now name the type (a `Renderer` bridged to `docs` returns `safehtml.SafeHTML`), and the sanitiser is the only constructor by construction rather than by a private package. Persistence: rendered HTML is stored with the pipeline version as **untrusted bytes** and is re-sanitised at warm start (about 1 ms per document) or re-rendered from the persisted source when the version differs, so a tampered database cannot inject script. `safehtml.Text` escapes plain text for unsupported README formats. The announcement is plain text and needs no `SafeHTML`.
