# Verification of design unit 06-httpapi (HTTP API infrastructure)

Date: 2026-10-14. Verifier: independent (not the author). Document: `20-design/06-httpapi.md` and its prototype `20-design/06-httpapi.assets/`.
Scratch work: the session scratchpad `verify-06-httpapi/` (a copy of the prototype with probe tests, the redocly bundles of the real 07 documents, the pre-edit copy of the design).

## 1. Result

**Verdict: ready with caveats.** 29 findings: 4 blockers, 7 major, 18 minor. 26 fixed (in the design document and, where the prototype was wrong, in the prototype), 3 open (V9 residual, V20, V26). The design was buildable only after the fixes: four of its own recipes and wiring snippets could not run as written (V3, V4, V5) and two defects of the prototype would have broken the product on day one (V1, V2).

State after the fixes: `go vet ./...`, `gofmt -l`, `go test -race -count=3 ./...` green on Go 1.27.1 (46 test functions, 38 original + 8 regression tests; coverage `httpapi` 78 %, `apitest` 69 %, `authz` 90 %, `validate` 87 %, `webapp` 90 %, `web` 100 %); `go vet -tags spa ./web` compiles with a `dist` and fails without one, as claimed.

Changed files: `20-design/06-httpapi.md` (edited, "Verification log" appended), `20-design/06-httpapi.assets/**` (bundle.go, defaults.go, problem.go, validate/problemmux.go, validate/validator.go, webapp/handler.go, httpapi/envelope.go, new httpapi/request.go, e2e/chain_test.go, validate_test.go, four new `*regress*`/`serve_test.go` files, refreshed `scm/repo.go` + `scm/membership.go` and `openapi/common.yaml` + regenerated `*.gen.go`, README.txt). No other unit's document was touched.

## 2. Findings

Severity: blocker = the product or a documented recipe cannot work; major = wrong behaviour or false claim with real consequence; minor = precision, completeness or hygiene.

| ID | Sev | Issue | Fixed | Evidence |
|---|---|---|---|---|
| V1 | blocker | `webapp.hashedName` took the text after the **last** `-` as the hash and demanded 8+ URL-safe characters. Vite/Rolldown hashes are 8 base64url characters and the alphabet contains `-` and `_`, so `assets/index-Ab-d1234.js` was rejected: about 12 % of names (1-(63/64)^8); a 20-asset build started with p about 8 %. | yes (4.9 rule, 6.3 build contract, code, `TestHashedAssetNamesFromViteAreAccepted`) | scratch test on the original: `index-Ab-d1234.js hashed=false`, `vendor-react-Zx9_K-qa.js hashed=false`; the new test fails on the original code with "must carry a content hash" |
| V2 | blocker | `APIDefaults` was documented as "under `/api/`" (2.5 comment, 4.5, D23) but had no prefix check. In the chain `GET /dashboard` with `Accept: text/html` and `GET /llms.txt` with `Accept: text/markdown` or `text/plain` answered 406 (this breaks the LLM surface, D-06 `Accept: text/markdown` on `/repos/{repo}`), and every 2xx badge/llms response without `Cache-Control` was forced to `no-cache`. | yes (code + 4.5 + `TestAPIDefaultsOnlyUnderAPI`) | scratch run: `/llms.txt accept="text/plain" -> 406`, `/dashboard accept="text/html" -> 406`, `/badges/... -> cc="no-cache"` |
| V3 | blocker | A strict handler is `Method(ctx, requestObject)`; it has no `*http.Request`. The recipes `NotModified(r, etag)`, `NextLink(r, cursor)` (4.3, 4.4, Appendix B) and the helpers `deref`/`ptr` could not be written; the prototype's e2e test hid this by faking a request from the `If-None-Match` parameter and hand-building the `Link`. | yes: `WithRequest`/`RequestFrom`/`Deref`/`Ptr` (new D29), recipes, wiring, e2e rewritten to use them | generated `commits.gen.go` `strictHandler.ListCommits` calls `ssi.ListCommits(ctx, request)` with `ctx = r.Context()` |
| V4 | blocker | 2.5 and 7.1 register `api.openapi` with `table.Handle(api, "api.openapi", ...)`. `Table.Handle` panics for an API-class route ("register it through Table.Mux"). | yes (recording mux, 2.5, 7.1, 13.15) | executed: `table.Handle(api.openapi) -> routes: api.openapi is an API route: register it through Table.Mux`; `rec.HandleFunc` succeeds |
| V5 | major | `Table.Verify()` fails for every table entry without a handler. 2.5/7.1 named only `spa.root`, assets and the catch-all; `spa.dashboard`, `spa.group.commits`, `spa.commits`, `spa.repo.commits`, `spa.repo.dependencies`, `spa.dependencies`, `spa.packages`, `spa.page`, `theme.init`, `favicon`, `manifest` (and who registers `negotiated.*`/`doc.md`) were missing. | yes (explicit registration set in 2.5/7.1, task 15) | `TestWiringAgainstTable`: Verify names `spa.dashboard` until registered |
| V6 | major | `ProblemMux` deleted `X-Content-Type-Options` on every rewritten 404/405 (claimed "since `WriteProblem` sets the right type", but `WriteProblem` set no nosniff), so those API errors left without nosniff (G14/T4). The test meant to guard it compared the header with `"text/plain"` and could never fail. | yes (WriteProblem sets nosniff, ProblemMux keeps it, test assertion fixed, new T18) | scratch run `nosniff=""`; `TestProblemMuxRewriteKeepsNosniff` fails on the original |
| V7 | major | 4.17 item 8 claimed `GET //` and `GET /repos/..` are "real 404 / shape-checked". Go 1.27.1's `ServeMux` answers `307` to the cleaned path before any handler (`//` -> `/`, `/dashboard//` -> `/dashboard/`). No 500 (path parsing is the mux's, GR-15) but not a 404; ADR-014 only retracted its own earlier claim. | yes (text, `TestWiringAgainstTable`) | executed table in the scratch run |
| V8 | major | 4.7 said `mapError` uses typed errors only and named `ErrInvalidContentType`. That error does not exist; kin-openapi v0.150.0 signals a bad Content-Type only by a `Reason` text starting with the unexported `prefixInvalidCT` (`validate_request.go:260,323`; its own encoder matches the same prefix). The prototype already did this; the text hid it. | yes (4.7 states the one exception and pins it by `TestValidator`) | read in the module cache; probe results `form ct`/`text ct`/`no ct` -> 415 |
| V9 | major | 4.9 claimed `.gz` and `.br` siblings are verified by decompression. Only `.gz` is (stdlib); `.br` is checked by size, so a corrupted brotli file reaches every browser as a blank page. | partly: text corrected, build-step round trip and Playwright net specified (6.3, 14.2, 12.4); residual risk accepted | `bundle.go` `verifyGzip` only |
| V10 | major | `BindingError` and the strict request handler rendered `safeDetail(err.Error())`, contradicting 3.1 ("never error strings of other packages") and T9: oapi-codegen decode messages contain Go struct field names and client input. | yes (fixed text; new D30; `TestStrictAndBindingErrorsDoNotRenderLibraryText`) | the generated error types are per package (`commitsapi.InvalidParamFormatError`...), so httpapi cannot type-switch on them |
| V11 | minor | Document sizes in section 5: bundled documents are 185 KB public / 187 KB admin (latest 07), not "110 / 20 KB"; embedded twice. | yes | redocly CLI 2.63.0 bundle sizes |
| V12 | minor | D2 evidence about `nethttp-middleware` partly wrong: its default path does return 405 (without `Allow`); `Prefix` rewrites `r.URL` only temporarily. The decision stands on the remaining evidence. | yes (D2, 4.7) | source of `oapi_validate.go` on `main`, fetched 2026-10-14 |
| V14 | minor | 4.6 said `Static` and webapp files are never compressed by `Gzip`; `application/yaml` is compressible, so `Static` is gzipped per request (~182 KB -> ~33 KB, ~2 ms) and its strong ETag weakened. | yes (4.6) | measured |
| V15 | minor | 2.4 `const (KindFile ServeKind = "file"; KindShell = "shell"; ...)` makes the later constants untyped strings. | yes | Go const rules; prototype has all typed |
| V16 | minor | `Recovered` contract incomplete: httpx must re-panic `http.ErrAbortHandler` and call the hook only if no header was sent. | yes (2.5 table, 14.2) | reasoning over `net/http` semantics |
| V17 | minor | `Reporting-Endpoints` needs a potentially trustworthy origin; on plain `http://` (non-localhost) browsers ignore it, so "zero CSP reports" proves nothing in that deployment. Relative endpoint URLs are valid. | yes (4.11) | w3c.github.io/reporting 3.2/3.3, fetched 2026-10-14 |
| V18 | minor | Trace table gaps. Missing and added: the group, commit-log, contents and pages routes, the badge route, the probe, build-info and status features, GR-16, GR-17, GR-19, GR-26, GR-32, GR-35, GR-40. | yes (11.1, 11.2) | grep of `02-design-guardrails.md` against 11.1-11.2 |
| V19 | minor | Static-file ETags are content hashes; architecture 11.3/ADR-010 say "derived from the build id". Not recorded as a deviation. | yes (D31) | 10-architecture.md 11.3 |
| V20 | major | **Cross-unit, open.** 07 D-1 says the admin listener serves the 22 public operations plus 2 and the admin SPA's `/status` page needs `getSite`/`getSnapshot`; `routes.Admin()` (unit 01) registers only `api.status`, `api.admin.retry`, `api.openapi`. Admin would 404 `/api/v1/site`, and the planned test "bundled OpenAPI paths equal the routes API class" cannot pass for the admin listener. | recorded (14.2, Q14); not this unit's to decide | `routes/admin.go`; `07-openapi-contract.md` D-1; admin bundle accepts `/api/v1/repos` |
| V22 | minor | Two `If-None-Match` header lines make the generated binding answer `TooManyValuesForParamError` (400); 4.17 item 1 implied they work everywhere. | yes (4.2, 4.17) | `commits.gen.go` lines 100-107 |
| V23 | minor | Empty `?limit=`/`?cursor=` pass the validator and are read as absent; an undeclared body member is accepted (07's `RetryJobRequest`). | yes (4.7, 14.2) | probe on the real documents |
| V24 | minor | A cursor payload `null`/`{}` decodes to the zero struct; only the mandatory fingerprint compare rejects it. | yes (3.2) | reading `DecodeCursor` |
| V25 | minor | 7.3 said a request under `/api/` that reached `webapp` is a plain-text 404; the chain rewrites it into a problem. | yes | `ProblemMux` |
| V26 | minor, open | kin-openapi v0.150.0 was released 2026-10-13, one day before the design. v0.149.0 (2026-08-28) passes the same tests. | Q13 added with a default | GitHub releases; ran the prototype with v0.149.0 |
| V27 | minor | The JSON example in 3.1 showed `"detail": ""` and `"retryAfterSeconds": 0`, which the encoder omits. | yes | encoder tags |
| V28 | minor | `Authorizer` must not depend on `ctx` (`ClassOf` calls it with `context.Background()`); stated only in a code comment. | yes (2.3) | `authz/principal.go` |
| V30 | minor | A bundle file was also served under `/theme-init.js/` (`path.Clean`). | yes (exact name, test) | scratch run |
| V32 | minor | `/favicon.ico` behaviour undocumented (GR-19: every asset request has a defined answer). | yes (4.17 item 18) | rule 7 of 4.10.1 |
| V33 | minor | The prototype's `scm` copy had drifted from unit 01 (`KeyParts`, by-ID index); the `common.yaml` copy and generated code had drifted from 07's latest (slug lower-case, `kind` required, all timestamps required-nullable). | yes (copies refreshed, `go generate` rerun with oapi-codegen v2.8.0, `NewFreshness` adapted; Q9 and Q12 marked resolved by 07) | `diff -rq` against the sibling assets; `diff` against `api/common.yaml` |

## 3. Conformance checks (a)

- **Import matrix**: `go list` of every package of the unit against the 5.2 rows: `httpapi` -> `httpapi/openapi`, `logx` (row allows `routes` too, unused); `authz` -> `scm`, `freshness`, `readmodel`, `httpapi`; `validate` -> `httpapi` + kin (row also lists `openapi`, `logx`, unused); `apitest` -> kin + `apispec` in tests; `webapp`, `apispec`, `openapi`, `web` import no internal package. All subsets of the rows. kin-openapi appears only in `validate` and `apitest`. Rank order holds. `routes/gorillamux` links `gorilla/mux` (transitive, not in the matrix: noted in 4.7).
- **Signatures vs section 8.14**: `Mux`, `Mounter`, `Problem` (+`Header`, D3), `WriteProblem`, `PageParams`, `ParsePage`, `EncodeCursor`/`DecodeCursor`, `WeakETag`, `NotModified`, `Static`, `Negotiate` (variadic options, compatible), `Principal`, `Authorizer`, `PrincipalFrom`, `ClassOf`, `LookupRepo`, `ProblemMux`, `RequestValidator` (+error, D1) all match; `go doc` output of every package was compared to 2.2-2.4 (only the `const` typing of V15 differed).
- **ADRs**: ADR-010 (embed tags, fallback rules, static CSP, admin manifest) conforms, with D5 (shape before extension) and D31 recorded; ADR-014 (middleware order, `Verify`, recording mux, `//` claim removed) conforms and V7 supplies the missing fact; ADR-020 (strict server, kin validation, committed generated source) conforms with D2; ADR-023/025 (`LookupRepo`, `ClassOf`, enumeration test) conform; the `access` unit's `WithPrincipal` requirement is met.
- **Structure**: all 15 required sections are present and concrete; open questions all carry defaults (Q13, Q14 added). "Verification log" appended.

## 4. Guardrail traceability (c)

The trace tables 11.1-11.2 were compared with `02-design-guardrails.md`. The root route redirects to `/dashboard`; unknown shapes are real 404s, never redirects (GR-16); only the build output is served and no source maps ship (GR-19); no CORS headers are emitted (GR-18); links are generated from the one route table (GR-15). Gaps are V18.

## 5. Technical checks (d)

- Prototype compiled and run clean before and after the changes (`-race`, `-count=3`).
- **Real 07 documents** (the first draft's "not checked" item): bundled with `@redocly/cli` 2.63.0 (`bundle`, output deterministic across two runs, `lint` valid), loaded with `RequestValidator`, `doc.Validate`, `apitest.Lint` (0 violations on public and admin, both on the 02:20 and the 02:44 versions) and 60+ probe requests (HEAD as GET, limits, cursor charset, unknown/repeated query parameters, path slugs, `docId` with `~`, body ops: wrong/absent Content-Type, malformed, missing member, empty body, oversize body, header guard `X-SCP-Request`). Behaviour matches 4.7 apart from the findings above.
- Wiring against the real unit-01 `Table` and a real `ServeMux`: `TestWiringAgainstTable`.
- kin-openapi v0.149.0 vs v0.150.0: same test results.
- `oapi-codegen` v2.8.0 regenerated against the current `common.yaml` and the sample fragment; strict-server shapes in 4.4 confirmed (`Headers.ETag *string`, `ListCommits304Response`).
- Snippets of the document: every Go block was run through `gofmt -e` (declarations and the wiring snippet parse; the 4.3 pseudo-code contains `...` by design).
- Algorithms: `NotModified` tokenizer, cursor codec, `Accept` q-value precedence, gzip state machine and `NextLink` were read and exercised by the existing tests; no defect found. SQL/regex/YAML: the only regexes are the slug/UUID/ID patterns and the hashed-name rule (fixed).

## 6. Web facts verified (2026-10-14)

| Fact | Source |
|---|---|
| kin-openapi v0.150.0 released 2026-10-13 (latest), v0.149.0 2026-08-28 | github.com/getkin/kin-openapi/releases |
| oapi-codegen v2.8.0 is the latest release (OpenAPI 3.1 support, spec validation before generation) | github.com/oapi-codegen/oapi-codegen/releases |
| `nethttp-middleware` 405/404/`Prefix` behaviour | raw.githubusercontent.com/oapi-codegen/nethttp-middleware/main/oapi_validate.go |
| CSP `report-to` "Baseline 2026, newly available (March 2026)"; MDN still shows `report-uri` alongside | developer.mozilla.org CSP `report-to` |
| `Reporting-Endpoints` is Baseline 2024; URLs parsed against the response URL, must be potentially trustworthy | MDN `Reporting-Endpoints`; w3c.github.io/reporting sections 3.2, 3.3 |
| `http.CrossOriginProtection` since Go 1.25; GET/HEAD/OPTIONS always allowed | pkg.go.dev/net/http |
| `application/yaml` registered by RFC 9512 | already cited by the design (not re-fetched) |

## 7. Testability (e)

The first draft's tests were green while V1, V2, V4, V5, V6, V10 and V30 were present: no test exercised `APIDefaults` outside `/api/`, no test named a Vite hash containing `-`, the nosniff assertion compared against the wrong value, no test wired the real `Table`, and the e2e chain bypassed the strict-handler request problem. Eight regression tests now fail on the first draft (verified for the webapp and validate ones by running them against the original tree; the `APIDefaults` and `nosniff` defects by scratch tests; `TestWithRequestAndHelpers` cannot compile there). Still to write, listed in 12.2: `Viewer` truth table and byte-equal 404s, size-limit test, HEAD shell 404, a generated-handler test with two `If-None-Match` lines, `instance == X-Request-Id` in the `cmd/scp` e2e once `httpx` exists. Other defect classes named by the design (GR-15, GR-16, GR-18, GR-19, GR-28) have a named test or a structural control.

## 8. Security and lifecycle (f)

Fixed: V6 (nosniff), V10 (library text in details), V17 (CSP report blind spot), V30. Reviewed without defect: LookupRepo indistinguishability (three failure causes `reflect.DeepEqual`), `Viewer` fail-closed class mapping, `Redirect` path-absolute check, `CSPReports` limits and sanitising, `WriteProblem` header allow-list, gzip pool lifecycle, immutable handlers and the race test. Open observation: a handler panic after `Gzip` buffered a partial 200 flushes the partial body instead of a 500 (same as without gzip once bytes are on the wire; httpx contract in V16).

## 9. Unresolved

1. **V20**: `routes.Admin()` (01) vs 07 D-1 (admin serves the public operations; admin SPA needs `getSite`/`getSnapshot`). Needs the owners of 01 and 07; recorded as 14.2 and Q14.
2. **V9 residual**: Go cannot verify `.br` files; mitigated by the web build's round trip and the Playwright gate (frontend unit must implement).
3. **V26**: pin v0.150.0 (one day old) or v0.149.0; both verified, default v0.150.0 (Q13).
4. Not verified, by nature: real `platform/httpx` behaviour (re-panic and header-sent contract, UUID request id), a browser's handling of the CSP rollout, Schemathesis.
