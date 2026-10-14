# Verification of design unit 18-web-pages (frontend pages and components)

Date: 2026-10-14. Verifier: independent (not the author). Document: `20-design/18-web-pages.md` and its companion `20-design/18-web-pages.assets/`.
Scratch work: the session scratchpad `verify-18-web-pages/` (copy of the companion tree with node_modules, Chrome/Playwright probes, the pre-edit copy of the design).

## 1. Result

**Verdict: ready with caveats.** 28 findings: 5 blockers, 11 major, 12 minor. 27 fixed (26 fully, V-26 in part) in the design document and, where the companion code was wrong, in the companion code with regression tests; 1 open (V-28, cross-unit). The unit was buildable only after the fixes: the first draft had five defects that would have shipped broken behaviour (Copy buttons wiped on every re-render under Trusted Types, a docs id encoding that rejected most real file names, a graph that kept the wrong theme after a light/dark switch, import-matrix violations, a live commit log that reordered under the reader).

State after the fixes: `tsc --noEmit` clean with TypeScript 6.0.3 and 7.0.2; Vitest 5.0.3 on Node 26.10.0: 14 files, 145 tests green (133 as authored, 12 regression tests); `tokens.css` reproduced by its generator and 0 contrast failures (own script and an independent culori 4.0.2 check); Chrome 155 CSP probe: zero violations, plain worker URL blocked; palette and context-loss probe passes.

Changed files: `18-web-pages.md` (many edits; this log appended), `18-web-pages.assets/README.md`, `18-web-pages.assets/package.json` (jsdom 26.1.0), `18-web-pages.assets/csp-probe/palette-probe/{app.ts,build.mjs,run.mjs}` (new), and under `18-web-pages.assets/web/src/`: `features/docs/{links.ts,doc-content.tsx,docId.ts,tree.ts,index.ts (new),headings.ts (new)}`, `features/commits/{filter.ts,api.ts}`, `features/manifests/api.ts`, `features/manifests/graph/{sigma-controller.ts,dependency-graph.tsx}`, `shared/{safe-html.tsx,problem-view.ts,data-table.tsx,freshness.ts}`, `app/search-model.ts`, `styles/contrast.test.ts`, and the tests `features/docs/{docs.test.ts,headings.test.ts (new)}`, `features/commits/commits.test.ts`, `features/manifests/api.test.ts (new)`, `shared/{data-table.test.tsx,shared.test.ts,safe-html.test.tsx (new)}`. No other unit's document was touched.

## 2. Findings

Severity: blocker = the product or a documented recipe cannot work; major = wrong behaviour or false claim with real consequence; minor = precision, completeness or hygiene.

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | minor | `decideClick` called `decodeURIComponent` on the hash: a heading or author link `#100%` throws `URIError` inside the click handler (the page then navigates natively and loses the in-page scroll). | yes (`safeDecode`, raw id fallback; U18-DOC-02) | `decodeURIComponent('100%')` throws in node 26; new test `a malformed percent escape in an anchor never throws` |
| V-02 | blocker | `SafeHtml` called `policy.createHTML()` in render, which returns a NEW TrustedHTML object each time; React compares `__html` by identity, so every parent re-render re-assigned `innerHTML`, destroying the Copy buttons and scroll anchors added by `DocContent`'s effect. | yes (`useMemo` on the html string; C18-DOC-02 extended) | jsdom test with a fake `trustedTypes` that returns a new object per call fails on the first-draft implementation and passes on the new one (`safe-html.test.tsx`) |
| V-03 | major | A non-`ProblemError` (a `TypeError` from a bug, or the graph endpoint answering a string body) was mapped to the "You are offline" page with automatic retry; a code bug looked like a network problem and retried forever. | yes (`server-error` kind; `assertGraphBody` throws `client-contract`) | `problemView(new TypeError(...)).kind === 'server-error'`; `graph body guard` tests |
| V-04 | blocker | `docs/docId.ts` rejected every path with a space or a non-ASCII letter (`docs/Release Notes.md`, `docs/Okonomi.md`) and mapped `~` back by a different rule from the contract's `DocId` (`/` -> `~`, `%XX` for every byte outside `[A-Za-z0-9._-]`). | yes (rewritten to the contract; malformed input returns null) | round-trip and rejection tests (`..`, `.`, empty, controls, lone surrogates, > 1,024); contract schema `DocId` in `07` read |
| V-05 | major | Section 4.3 promised that `lastCommit` and `pushedAt` start descending, but `nextSortToken` had no such parameter: the first click sorted ascending. | yes (`meta.defaultDesc`, `descFirst`; C18-GRP-01) | `data-table.test.tsx`: `a column whose natural order is descending starts descending` |
| V-06 | major | `countNewer` compared the sha only; a mirror or fork shares shas, so the "N new commits" pill under-counted (a commit's identity is repository plus sha, GR-07). | yes (`commitKey` = `repo@sha`) | `commits.test.ts` regression test |
| V-07 | minor | `app/search-model.ts` imported `docSplat` from `@/features/docs/docId`: a cross-feature import that bypasses `index.ts` (architecture import rule). | yes (new pure `features/docs/index.ts`) | grep over the companion tree: the only cross-slice import left in `src/` outside the slices is `test/synthetic-graph.ts` (test support, not a page import); no slice imports another slice |
| V-08 | minor | The doc claimed the companion `vitest` config could run everything in jsdom; `contrast.test.ts` reads a file by `new URL(import.meta.url)` and fails under jsdom (`URL must be of scheme file`). | yes (per-file pragmas, README, 12.1 text) | run with a jsdom default: 1 failure; reverted to node + pragmas: 145/145 |
| V-09 | blocker | `GraphController.setPalette` stored the palette but did not rewrite per-node and per-edge colours or the label colour: after a light/dark switch the graph kept the other theme. | yes (`updateEachNodeAttributes`, `updateEachEdgeAttributes`, `setSetting('labelColor')`, `refresh`; E18-GRF-06) | Chromium probe `csp-probe/palette-probe`: node colour `#111111` -> `#aa0000`, label `#eeeeee`, edge `#555555` after `setPalette(dark)` |
| V-10 | major | The layout worker could fail (thrown layout, `onerror`, 404 of the worker URL) and a WebGL context could be lost; neither was handled, so the page showed "Arranging N units" forever. | yes (`onerror`, `{type:'error'}` branch, try/catch around `new Worker`, `webglcontextlost` capture listener, probe context released; E18-GRF-07/08) | palette probe: `onContextLostCallback: true`, one `webglcontextlost` seen at the container in the capture phase |
| V-11 | major | D-18-09 / OQ-18-01 justified "Status needs no `/api/v1/site`" by "the admin listener serves neither": false. Kernel RT-14 lists 24 admin API rows including `getSite` and `getSnapshot` (the 22 public operations plus two admin ones), so no listener-specific skipping is needed. | yes (D-18-09 corrected, OQ-18-01 closed) | `01-kernel` RT-14 table and `06-httpapi` admin table read |
| V-12 | blocker | The import matrix was violated: `commits` imported `groupsQuery`, `groupQuery`, `repoQuery` from other features (an edge that does not exist, and with `dashboard -> commits` it would be a cycle). | yes (`commitGroupOptionsQuery` duplicate factory with the same key, `getRouteApi` for the layout data; D-18-11) | architecture 12 import matrix compared with every import in the companion tree |
| V-13 | blocker | Unit 17 invalidates every query on a snapshot change and refetches every active query every 120 s (`SWEEP_MS`). A live infinite commit log would have its loaded pages refetched and reordered while the reader scrolls. | yes (`follow` switch: `enabled: follow`, `FOLLOW_SCROLL_Y = 200`; pill takes over when scrolled) | TanStack Query 5 probe: a disabled infinite observer is not refetched on `invalidateQueries` and refetches when re-enabled; `commit log follow switch` tests |
| V-14 | minor | The "Documentation" tab named no target; a bare `/repos/x/docs` is not an SPA shape (the server answers 404). | yes (`docsEntry(entries)`: first non-README document; tab hidden when only the README exists) | `docs.test.ts` ordering and target test |
| V-15 | major | `native-select` and `button-group` are used by the group, dependency and graph pages but are in neither unit 17's installed list nor the first draft's additions. | yes (T-18-002, Appendix A: four `+` components) | unit 17 list compared with Appendix A |
| V-16 | minor | `ExternalLink` was listed as a new component demanding `https://`; it exists in unit 17 with `isWebUrl` (http and https). | yes (only the additive `className` prop is requested, T-18-007) | `design-17-web-foundation.md` section on `ExternalLink` |
| V-17 | major | Section 5 said panels fail alone through `PanelBoundary`, but with a plain `useQuery` the boundary never fires (the hook returns the error instead of throwing). | yes (suspense hooks required, text in 5) | TanStack Query 5 semantics; `PanelBoundary` is an error boundary (unit 17) |
| V-18 | minor | The worker lifecycle contradicted itself: "created once per mounted canvas" in 4.9 and "terminated after each response" in 8. | yes (one worker per layout run; terminated on response, supersede and unmount) | 4.9 and 8 now identical; positions are kept in a `Map` per canvas |
| V-19 | major | Trace gaps: the licence and release facts, GR-18 (security headers, no inline handlers or `style=` attributes), GR-02 (serving before data exists) and the Recharts statement of the architecture were not traced. | yes (rows added to section 11, D-18-10) | `02-design-guardrails.md` and architecture 21 compared with section 11 |
| V-20 | minor | No test for the visible literals `null`, `undefined`, `NaN`, `[object Object]`, `src=""` (an absent value is a typed state, never printed as text: GR-29, GR-39). | yes (E18-TXT-01) | string concatenation of an optional field (`'Updated: ' + updatedTime`) is the failure mode the test pins |
| V-21 | minor | The mount-time claim (29 to 31 ms) cannot be reproduced under software WebGL; the gate would flake in CI. | yes (gate loosened to <= 600 ms / <= 1 s at 6x on software GL; real-GPU numbers informational) | Chrome 155, SwiftShader, load average about 50: mount 264 to 843 ms in repeated runs (claimed 29 to 31 ms); layout stays about 800 ms |
| V-22 | major | "Tables are wrapped by CSS so they scroll" cannot be done in CSS: the scroll region needs `tabindex` and a label only when it overflows (WCAG 2.1.1 / axe `scrollable-region-focusable`). | yes (ResizeObserver on `div.md-table`; unit 10 D-3) | unit 10 design D-3 (`div.md-table`) read |
| V-23 | minor | The search palette silently truncated the repository list when the API has more than 1,000 repositories. | yes (row "Some repositories are not listed, narrow the search" when `nextCursor` is non-null) | `ListRepos` `limit` maximum 1,000 in the contract |
| V-24 | minor | `Owner.kind = unresolved` was not specified (it must be plain text, never a link). | yes (4.6: handle as text plus "(unresolved)") | contract `Owner` schema |
| V-25 | minor | The group table promises GitHub, docs and commit links per row (4.3); the first draft dropped the docs link without saying so. | yes (the title link is the docs link; recorded) | 4.3 group table |
| V-26 | major | `freshness.ts` mapped `lastErrorCode` by prefix only; units 07 (dotted: `github.not_found`) and 09 (closed set) disagree, so a 404 cause read "Unknown cause". | partly: exact causes checked before prefixes and `causeId` used where present; the vocabulary disagreement itself is OQ-18-03 | `shared.test.ts` cases for both vocabularies; `07` and `09` examples compared |
| V-27 | major | Two `h1` per page: the repository header and the page heading, and every `h1` inside rendered Markdown (WCAG one-h1 rule of 4.1.3; axe `page-has-heading-one`, GR-39). | yes (`normaliseDocH1`, compact repository header has no `h1`) | `headings.test.ts` (jsdom), idempotence test |
| V-28 | minor | Job key grammar differs between the kernel (JB-01: up to 512 bytes, parts may contain spaces) and the contract (`RetryJobRequest.key` `maxLength 300`, pattern `^[a-z][a-z0-9._-]*/[^\s]+$`): a long or spaced key can be shown but not retried. | no (cross-unit; `canRetry` mirrors the contract, OQ-18-16) | `01-kernel` JB-01 and `20-design/api/openapi-admin.yaml` compared |

## 3. Conformance checks (a)

* Package names and layout match architecture section 12 (`web/src/features/{dashboard,groups,commits,docs,manifests,pages,status}`, `app/search-*`, `shared/`, `styles/tokens.css`).
* Import matrix (dashboard -> groups, commits; groups -> commits; docs, manifests, pages, status import no feature; cross-feature only through `index.ts`): checked with a grep over the companion tree; violations found and fixed (V-07, V-12); the design now documents the three data needs resolved without a forbidden edge (2.1, D-18-11).
* Unit 17 ownership respected: `ExternalLink`, `siteQuery`, `useDisplayZone`, `ProblemAlert`, `PanelBoundary`, `qk`, `serverRoutes`, snapshot polling; the copies of unit 17 files in the companion tree are marked as copies. D-18-06/10/11 record the extensions.
* The CSP is static and unchanged; the only addition (worker URL) goes through the one Trusted Types policy with `createHTML` and `createScriptURL` (verified in Chrome).
* Stack claims (React 19.3 + Compiler, TypeScript 7 with the 6 alias, Vite 8, Tailwind 4, TanStack Router/Query/Table 9, sigma 3.0.3) match the pins in the companion `package.json`; no chart library is used (D-18-10).

## 4. Completeness of the 15 sections (b)

All 15 sections plus Appendices A and B exist; sections 5 to 9 are short by nature (pure client) and say so. Gaps found: the trace rows listed in V-19, the missing test of V-20, and the dangling "V-nn" references (now all resolved to the log). The header row "Verification" and the appended "Verification log (independent verifier)" are consistent with this report (28 / 27 / 1).

## 5. Guardrail traceability (c)

The trace table (section 11) was checked against `02-design-guardrails.md`: the number of commits shown is configured (`commits.latest_n`), never hard-coded (GR-14, D-18-03/05); no column ships as a stub (GR-04); absent values never render as text (GR-29, E18-TXT-01); no inline handlers or `style=` attributes (GR-18, section 10); commit authors are shown as initials, with no third-party avatar request (GR-35, 4.5); commit identity is repository plus sha (GR-07, V-06); the group page links to its commit log (D-18-05). The gaps are V-19 and V-20.

## 6. Technical checks (d) and web facts verified (2026-10-14)

| Fact | How verified (2026-10-14) | Result |
|---|---|---|
| Static CSP with `require-trusted-types-for 'script'` blocks `new Worker(plainUrl)`; one policy with `createHTML` and `createScriptURL` is enough | Chrome 155 + Playwright, `csp-probe/run.mjs` with and without `?tt=plain` | zero violations with the policy; `This document requires 'TrustedScriptURL' assignment` with a plain URL |
| sigma 3.0.3 `updateEachNodeAttributes`, `setSetting('labelColor')`, `refresh` repaint after a palette change | `csp-probe/palette-probe` in Chrome 155 (SwiftShader) | node `#111111` -> `#aa0000`, label `#eeeeee`, edge `#555555` |
| `WEBGL_lose_context.loseContext()` raises `webglcontextlost` that reaches a capture listener on the container | same probe | seen once; `onContextLost` called |
| TanStack Query 5: a disabled infinite observer is not refetched by `invalidateQueries` and refetches when re-enabled | probe test in the scratch tree | confirmed (V-13 mechanism) |
| Graph chunk 39.2 KB gzip, layout worker 6.0 KB gzip | esbuild bundle + gzip | reproduced as authored |
| Mount time 29 to 31 ms | Chrome 155, SwiftShader, load average about 50 | not reproduced: 264 to 843 ms (V-21) |
| Design tokens: generator reproduces `tokens.css`; contrast | `python3 -I gen.py` + diff; `contrast.py` (FAILS 0); independent check with culori 4.0.2 (26 foreground/background pairs, light and dark, plus 8 avatar backgrounds against white) | identical file; 0 failures in both |
| Type checking | `tsc --noEmit` with TypeScript 6.0.3 and with 7.0.2 | clean, both |
| Tests | `vitest run` 5.0.3 on Node 26.10.0 | 14 files, 145 tests passed (133 as authored + 12 regression tests) |

## 7. Testability (e)

Every behavioural fix has a regression test that fails on the old code (the Trusted Types test was run against the old `SafeHtml`). Remaining e2e specs are marked `(stub)` in section 12 and depend on unit 17's Playwright project. The jsdom/node split is documented (V-08): unit 17's `unit` project must keep per-file pragmas.

## 8. Security and lifecycle (f)

* Trusted Types: the single policy; `SafeHtml` now produces a stable `__html` (V-02), so HTML is not re-parsed on re-render. Worker creation is a TrustedScriptURL sink and is covered by the CSP probe.
* Links: only `isWebUrl` URLs are rendered as links (unit 17 `ExternalLink`); owners with `unresolved` kind are plain text (V-24); doc links never throw (V-01).
* Lifecycle: worker per layout run, terminated on response, supersede and unmount; WebGL context loss and worker failure leave a usable Outline (V-10, V-18); the live commit log does not refetch while the reader scrolls (V-13).

## 9. Unresolved

* V-28 / OQ-18-16: job key grammar mismatch between kernel JB-01 and the contract; needs units 01 and 07.
* OQ-18-03: `lastErrorCode` vocabularies of units 07 and 09 differ (mitigated in the UI by exact-then-prefix mapping and `causeId`).
* OQ-18-02: docs index order is not promised by the contract.
* Software WebGL mount timing is environment-sensitive (gate loosened; V-21).
