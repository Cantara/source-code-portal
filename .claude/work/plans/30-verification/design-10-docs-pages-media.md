# Verification report: design unit 10 (documentation, pages and media)

Verifier: independent (not the author). Date: 2026-10-14. Document: `20-design/10-docs-pages-media.md` plus companion `20-design/10-docs-pages-media.assets/`.
Method: read the binding inputs (`04-decisions.md`, `10-architecture.md` 5.2, 8.10, 14, ADR-005/006/007/010) and the contracts the unit consumes (`07` operations and problem catalogue, `06` helpers and authorisation middleware, `01` `safehtml`, `02` `netguard`, `03` slots and store, `05` adapter); rebuilt the reference module in a scratch module against the current sibling assets; wrote adversarial tests before fixing; checked the trace tables against `02-design-guardrails.md`; checked library claims against the module proxy; edited the document and the assets; appended section 16.

Result: **35 findings, 35 fixed in the document and the companion code; 6 open items that need an owner outside this unit (none blocks implementation).** Baseline before changes: the prototype built, but `go test -race` failed in `render` (`TestScaling`) and took 270 s. After: `go vet`, `go test -race -count=1` green for `internal/docs/...`, `internal/media`, `internal/pages`, `cmd/scp`, `internal/safehtml`; the adversarial timing tests (`Worst|Budget|Scaling`) pass without `-race`; `assemble.sh` rebuilds the module from a clean directory with the same results (checked in `scratchpad/asm-final`).

## (a) Conformance with the architecture and ADRs

Import directions checked with `go list` on the reference code against matrix 5.2:

| Package | Imports (internal) | Matrix | Verdict |
|---|---|---|---|
| `docs/schema` (0) | none | none | conforms |
| `docs/internal/render` (1) | `safehtml` only | `scm freshness platform/secret safehtml` | conforms; third-party `go.yaml.in/yaml/v3` is **not** in the matrix column for this row: **V-28, new D-16** |
| `docs` (4) | `docs/internal/render docs/schema freshness httpapi httpapi/authz job readmodel routes safehtml scm store` | K4 + render, schema, safehtml | conforms |
| `media` (4) | `freshness httpapi httpapi/authz platform/secret readmodel routes scm` | K4 | conforms |
| `pages` (4) | `freshness httpapi readmodel safehtml` | K4 + safehtml | conforms |

No feature imports another feature; `cmd/scp/assert_test.go` composes the unit with the real `job`, `readmodel`, `store`, `httpapi`. Consumer-interface signatures of section 2.3 compile against the sibling prototypes. Section 8 signatures (8.10 sketch) differ from the architecture only where section 14 lists the deviation (D-4, D-8, D-14). Contract conformance found real mismatches: the document identifier encoding (V-01), problem codes (V-31), ETag kind (V-29), the alias redirect (V-32).

## (b) Completeness

All 15 required sections are present and concrete; section 16 (verification log) was appended. Section 12.1 states honestly which parts are implemented (render, descriptor and pure logic, codec, media, pages) and which are specified only (the `docs.refresh` run loop, raw and API handlers, generated `docsapi`/`pagesapi`). Open questions carry blockers and defaults; Q-3 was extended and Q-11 added.

## (c) Guardrail traceability

The unit's guardrails were each checked for a mechanism and a test: rendered HTML reaches a page only as `safehtml.HTML` minted by the sanitiser (GR-32); document types are matched case-insensitively and an unsupported type is a typed state, never an empty body (GR-29, GR-37); no embedded interpreter renders documents and rendering happens once per content hash (GR-36); XML and SVG input is parsed safely within limits (GR-34); unknown groups and pages are real 404s (GR-16); descriptor globs are anchored and bounded (GR-09); per-repository URLs come from the repository, not the group (GR-21); freshness is stored per item (GR-26).

Missing from the trace table before verification and added: the real-404, refresh-cadence, push-docs and `llms.txt` features; GR-16, GR-18, GR-21, GR-24, GR-26, GR-31, GR-33, GR-34, GR-39 (V-34). Every other guardrail that concerns docs, pages or media was already present with a mechanism.

## (d) Technical correctness

Compile-checked in the scratch module `github.com/cantara/source-code-portal` (Go 1.27.1, darwin/arm64). Library claims verified 2026-10-14 against `proxy.golang.org` and `go list -m -u`: goldmark v1.8.6, chroma/v2 v2.27.0, `x/net` v0.61.0, jsonschema/v6 v6.0.3, `go.yaml.in/yaml/v3` v3.0.5 exist, no newer release. `os.OpenRoot` (Go 1.24) and `testing/synctest` (Go 1.25) are standard library in the pinned Go. Behaviour claims were measured: quadratic nesting, catastrophic lexers, and the exponential glob were each reproduced. Fuzz re-run after the changes: `FuzzRender`, `FuzzSanitizeFixpoint`, `FuzzDescriptor` found nothing.

## (e) Testability

Every fix has a test that failed before the fix (listed in section 12.2). Time-dependent behaviour uses `testing/synctest`; the race job and the non-race job are separated (V-13) so absolute-time assertions do not run under the race detector.

## (f) Security and races

Hostile-input review produced V-02 (CPU denial of service by any repository author through descriptor globs), V-03 (leader panic leaves a nil entry for followers), V-06 (descriptor and catalog-info fields copied unvalidated), V-17 (link-comment idiom hides text from humans but not from LLMs), V-23 (SVG DOCTYPE internal subset), V-21/V-22 (stored payload trusted structurally). SSRF, signature, sniffing, sandbox, path rules and single-flight were re-read and tested; no further defect found.

## (g) Buildability

`assemble.sh` copies siblings from the 01, 02, 03 and 06 assets, drops what is not needed, repairs the `logx` import path, and applies `safehtml-D2-D3.patch`; the clean-directory build passes vet and tests. README.txt lists what is stub and what is complete.

## Findings

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V-01 | High | `DocIDOf` replaced only `/` and refused paths containing `~`; the contract's `DocId` is a percent-encoding of the path | yes (code, 3.1) | `TestDocID` with the contract regex `^(readme\|[A-Za-z0-9._~%-]{1,1024})$` |
| V-02 | High | `Glob` was a recursive matcher: 38 `**` segments in a 191-byte pattern took 57 s (descriptor authors control it) | yes (dynamic program) | `TestGlobAdversarial` under 100 ms |
| V-03 | High | `flight.do`: a panicking leader left followers a nil entry (nil dereference in a handler) | yes (`errFlightPanic`, finished flag) | `TestFlightLeaderPanic` |
| V-04 | Med | include/exclude matched only the root-relative path; descriptor examples use repo-relative patterns | yes (both forms match) | `TestDiscoverGlobsAcceptRepoRelativePatterns` |
| V-05 | Med | published `frontmatter-v1.json` had drifted from the code (title 200 vs 120, order range, empty description, duplicate tags) | yes | `TestFrontMatterSchemaMatchesCode` (26 rows) |
| V-06 | High | catalog-info `name`, `owner`, `system`, tags, link types were copied without cleaning or caps | yes | `TestDescriptorFieldGranularity`, `FuzzDescriptor` |
| V-07 | Med | loader: unchecked root type assertion (panic path), byte-slicing truncation split UTF-8, `docs.root` not normalised | yes | `TestDescriptorBombs`, `TestDescriptorSamples` |
| V-08 | Low | 4.2 did not say what a nav entry naming the README does | yes (document) | 4.2 |
| V-09 | Low | `strings.ToLower` maps U+212A to `k`: `x.mar<KELVIN>down` was Markdown with a mis-sliced key | yes (ASCII fold, `hasExt`) | `TestDocID`, `TestDiscover` |
| V-10 | Low | README `Key` differed between a live and a restored snapshot | yes (always empty) | `TestCodec*` |
| V-11 | High | the head hint was consumed (`Take`) so a late older hint could select an older head, and a failed run lost it | yes (`Peek`/`Forget`, verified against the catalogue) | `TestHintMapBoundedAndExpires` |
| V-12 | High | image URLs embed the commit; an image-only change was invisible to `TouchesDocs`, leaving stale images and slugs in stored HTML | yes (`touchesImages`, `RepoDocs.ImageSet`) | `TestTouchesDocs` |
| V-13 | Med | under `-race` the render package took 270 s and `TestScaling` failed | yes (`timeScale`, `heavy(t)`, non-race CI job) | 12.5 |
| V-14 | Med | `.github/readme*` (GitHub's README location) was not a docs input | yes | `TestTouchesDocs` rows |
| V-15 | Low | no injectable clock for the unit; warm-start label did not name the stale pipeline case | yes (`Deps.Now`, `pipeline_stale`) | `TestMetricNames` (planned), 9.1 |
| V-16 | Med | shape guard rejected any physical line over 8 KiB (ordinary single-line paragraphs) | yes (40 KiB stream, sum of squares 2e9) | `TestEvade`, `TestWorstAccepted`, `TestRealDocs` |
| V-17 | Med | `[x]: <> (hidden)` link-comment idiom survived the LLM variant | yes (`stripLinkComments`, marker) | `TestNormaliseLinkComments` |
| V-18 | Low | repository lookup in `react.go` compared unfolded keys | yes | `TestTouchesDocs` |
| V-19 | Low | cache metrics did not distinguish `negative` and `stale` | yes | 9.1 |
| V-20 | Low | no test for dotted keys such as `docs/v1.2/install.md` | yes (`hasExt`; test planned M2) | `TestDocKeyWithDots` (P) |
| V-21 | Med | `Decode` accepted structurally invalid rows (enums, id, link scheme, TOC prefix, count) | yes (`payload.validate`) | `TestCodecKeepsReasonAuxImagesAndRejectsTampering` |
| V-22 | Med | `Reason`, `Aux`, `Images` were not persisted; a warm start lost them | yes | same test |
| V-23 | Med | `Sniff` accepted an SVG with a DOCTYPE internal subset or `<!ENTITY` | yes | `TestSVGWithInternalSubsetIsNotSVG` |
| V-24 | Low | `/media/ext` answers differ by `Sec-Fetch-Dest` but carried no `Vary` | yes | 4.11 |
| V-25 | Med | pages: a failed first load published nothing (no announcement, `readmodel` slot unset) | yes (empty set with announcement and error info) | `TestFirstLoadFailurePublishesEmptySetWithAnnouncement` |
| V-26 | Low | pages: problems were not in the fingerprint, so a new problem did not republish | yes | `TestNewProblemChangesFingerprint` |
| V-27 | Med | D-2 id regex allowed `%` (no slug contains it; ids could look like escapes) | yes (patch and D-2) | `safehtml` golden tests on the patched policy |
| V-28 | Med | `render` imports yaml, not in the matrix column; no deviation recorded | yes (D-16, 2.1) | `go list` |
| V-29 | Med | doc API ETag used `WeakETag` (snapshot kind `g`) for a repository-scoped resource; derived-status digest by hand | yes (`WithDigest(WeakETagOf(ETagRepo,...), FreshnessDigest(...))`) | `06-httpapi.assets/internal/httpapi/etag.go` |
| V-30 | Low | `markdownUrl` was non-null for text documents that have no raw route | yes (markdown only) | 4.9 |
| V-31 | Med | four raw-route problem codes (`not-resident`, `too-large`, `not-markdown`, `not-available`) are not in the contract catalogue | yes (`not-found` + `detail`; D-17 optional) | `07` 4.7 |
| V-32 | High | the unit's handlers redirected aliases themselves, before visibility: a hidden repository's old name answered `308`, not the uniform `404` | yes (06 middleware, visibility-first) | `06` 4.8 `repo-renamed` |
| V-33 | Low | 6.4 said the schema `$id` follows `server.public_url`, contradicting the embedded bytes | yes | 4.3, 6.4 |
| V-34 | Low | trace table lacked the real-404, refresh-cadence, push-docs and `llms.txt` features and GR-16, GR-18, GR-21, GR-24, GR-26, GR-31, GR-33, GR-34, GR-39 | yes | 11.1 to 11.3 |
| V-35 | Low | 12.1 line counts and test inventory stale; assets did not match the document | yes (counts, inventory, assets re-synced) | 12.1, 12.2 |

## Open items (owner outside this unit)

1. Contract edits D-2 (`TocEntry.id` pattern, unicode headings) and D-9 (UI vs markdown URL example in `07`): unit 07 owner.
2. `safehtml.PolicyVersion` 2 and the three regular-expression deltas (`safehtml-D2-D3.patch`): unit 01 owner.
3. D-1 external SVG default (`deny` ships until approved): product owner.
4. D-17 (optional dedicated problem codes in the catalogue): unit 07 owner.
5. The `docs.refresh` run loop, raw and API handlers, generated `docsapi`/`pagesapi` are **specified, not implemented**; milestones M3 and M4 own them.
6. Q-1 (reading of Q-20 on sub-folder docs) still needs the owner's answer.
