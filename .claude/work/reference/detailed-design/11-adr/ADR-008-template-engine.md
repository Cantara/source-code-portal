# ADR-008: No server-side page templates; `text/template` for SVG and text only

Status: Accepted. Date: 2026-10-14. Supersedes the template parts of 04 D-05 (A-05).

## Context
D-05 chose `html/template` (or templ) with HTMX; D-16 replaced server-rendered pages with a React SPA. Remaining server-generated text: badges (SVG), `llms*.txt`, raw Markdown, sitemap/robots, one static fallback page.

## Decision
- No Go template engine renders application pages; the SPA shell `index.html` is served verbatim (static CSP, no nonce injection, ADR-010).
- SVG badges are built by marshalling small typed structs with `encoding/xml` (escaping is structural; invalid XML characters become U+FFFD); plain-text outputs (`llms*.txt`, sitemap, robots) use `text/template` or a `strings.Builder`. `html/template` is reserved for the single static fallback page shown when the SPA assets are the placeholder.
- templ and HTMX are not adopted; no HTML is produced by string concatenation.

## Alternatives considered
templ (pre-1.0, irrelevant after D-16); `html/template` for pages (duplicates the SPA).

## Consequences
Fewer server-side XSS surfaces and no template logic in Go; link correctness comes from typed SPA routes and `routes` builders rather than a Go template crawl test.

## Evidence
`05-research/02-go-stack-currency.md` section 7 (templ, D-05 row); `05-research/06-web-ui-badges-security.md` section 7.4.

## Amendment 2026-10-14 (review revision)

Go review F-19: a `text/template` plus a hand-written escaper is string-replacement into XML (GR-33) with a function the author must remember to call in every attribute and text position, and badge text (a release tag, a licence string) is attacker-influenced. The SVG is now built with `encoding/xml` so escaping cannot be forgotten; a fuzz test asserts that arbitrary label and message text always yields well-formed XML; the ~30-string golden test stays.
