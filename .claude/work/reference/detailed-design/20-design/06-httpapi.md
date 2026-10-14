# 20-design / 06 - HTTP API foundation, authorisation, request validation, SPA hosting (`internal/httpapi`, `internal/httpapi/{authz,validate,openapi,apitest}`, `internal/apispec`, `internal/webapp`, `web`)

Status: design for implementation. Date: 2026-10-14. Unit key `06-httpapi`.
Conforms to `04-decisions.md` (D-05, D-12, D-16, amendments A-12, A-14, A-16, A-23), `10-architecture.md` (sections 4, 5, 6.1, 8.13, 8.14, 11, 12.4, 15-18, 20-23) and ADR-010, ADR-014, ADR-020, ADR-023, ADR-025, and to the guardrails of `02-design-guardrails.md` (traced in section 11.2).

This document is self-sufficient: an engineer who has never seen this conversation can implement every package in the unit from it. A throw-away reference prototype that compile-checks and tests every contract below lives next to it in `20-design/06-httpapi.assets/` (`README.txt` there). **What was compile-checked and executed** is listed in section 12.1; the one-line version: every exported signature quoted here is the output of `go doc` on that module (Go 1.27.1, `go vet` clean), and `go test -race -count=3 ./...` is green (46 test functions after the 2026-10-14 independent verification, which found 29 defects in the first draft and its prototype (4 blockers) and fixed 26: see the Verification log at the end, 69-100 % statement coverage per package, a `-tags spa` compile check, the lint rules run clean against the real `20-design/api/openapi-public.yaml` and `openapi-admin.yaml`, and the real kernel packages `scm`, `freshness` and `routes` of unit 01 compiled in and exercised by seam tests).

Conventions: "MUST / MUST NOT" are binding for the implementation; "design choice" marks a decision taken here where the architecture was silent; "Dn" points at the deviation list (section 14); "Qn" at the open questions (section 15). The prototype is a **reference, not a source to copy blindly**: where this text and the prototype disagree, this text wins and the prototype has a bug.

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

This unit is everything between the TCP listener and a feature's handler that is not feature logic, plus the serving of the single-page application:

- `internal/httpapi` is the **vocabulary every API-owning feature shares**: RFC 9457 problem details, cursor pagination, weak ETags and conditional GET, content negotiation, response compression, the `Mux`/`Mounter` contracts, the freshness/page envelope builders and the redirect helper. It is cheap to import (rank 2, no kin-openapi, no `httpx`, no `readmodel`).
- `internal/httpapi/authz` is **visibility as a security control**: `Principal`, `Authorizer`, the per-request `Viewer`, `ClassOf` (which projection variant a caller may read) and `LookupRepo` (the only sanctioned slug resolver; a missing and an invisible repository are byte-identical 404s).
- `internal/httpapi/validate` is the **only place kin-openapi is used at run time**: request validation against the bundled OpenAPI document, and `ProblemMux`, which turns the mux's plain-text 404/405 under `/api/` into problem+json.
- `internal/httpapi/openapi` is the **generated shared types** package (`Problem`, `Freshness`, `PageInfo`, parameters, headers, responses) generated from the `common.yaml` that design unit 07 owns.
- `internal/httpapi/apitest` is the **contract-test toolkit**: spec lint, response validation against the document, ETag-covers-body property, no-leak and no-CORS assertions.
- `internal/apispec` embeds the two bundled OpenAPI documents; `internal/webapp` hosts the SPA (precompressed static files, history fallback, `routes.json` shape check, document CSP, CSP report endpoint); `web` is the two-file embed package (`placeholder` versus `dist`).

Product intent (traced in section 11): unknown things are real 404s, never a soft redirect to the dashboard (GR-16); only the files the build produced are served (GR-19); there is no CORS, and every document carries strict security headers (GR-18); a private repository is indistinguishable from a missing one (GR-35).

### 1.2 What is in scope

| Area | In this unit |
|---|---|
| Problem details | `Problem`, the 13 stable codes, constructors, `WriteProblem`, `InstanceURN`, `ProblemError`/`Err`/`Wrap`, `StrictErrors`, `BindingError`, and the hooks `Recovered`, `RateLimitedFunc`, `DenyCrossOrigin` that `platform/httpx` plugs in |
| Pagination | `ParsePage`, `EncodeCursor`/`DecodeCursor` (opaque, versioned, strict), `QueryFingerprint`, `NextLink`, `NewPageInfo` |
| Conditional GET | `WeakETag`, `Digest`, `StrongETag`, `NotModified` (weak comparison, tokenizer), the rule that the ETag covers every input of the body (including time-derived freshness) |
| Negotiation and defaults | `Negotiate` (markdown vs SPA), `AcceptsAny`, `AcceptsEncoding`, `APIDefaults` (406 and `Cache-Control`), `Gzip` (dynamic responses), `Static` (embedded bytes with a strong ETag) |
| Envelopes | `FreshnessView`/`NewFreshness`, `NewPageInfo` over the generated types |
| Mux contracts | `Mux`, `Mounter`, `MountAll` |
| Request access | `WithRequest`, `RequestFrom`, `Deref`, `Ptr` (a strict handler has no `*http.Request`, D29) |
| Authorisation | `Principal`, `Authorizer`, `PublicOnly`, `Viewer`, `ClassOf`, `LookupRepo`, `WithPrincipal`/`PrincipalFrom` |
| Validation | `RequestValidator` (path, query, header, body; unknown and repeated query parameters; 404/405 from the document), `ProblemMux`, the error mapping table |
| Contract tooling | `apitest.Load`, `Lint` (rules L1-L11), `ValidateResponse`, `ValidateProblem`, `ETagCoversBody`, `Normalised`, `NoLeak`; `apispec.Public()/Admin()` |
| SPA hosting | `webapp.New`, `Handler.For`, file serving, encodings and ETags, caching classes, history fallback with real 404, trailing-slash 308, reserved prefixes, `routes.json`, `X-SCP-Build`, security headers of the document, CSP three-mode rollout, `CSPReports` |
| Embedding | `web.FS()`, build tags `spa` / `!spa`, placeholder bundle, the rule that `go test ./...` needs no Node |

### 1.3 Non-goals

- No feature endpoint, no OpenAPI **content** (design unit 07 owns `common.yaml`, the feature fragments and the two roots; this unit owns the generated `openapi` package, the lint rules and the validator that consume them).
- No process-wide middleware (request id, recover, access log, body limit, trusted proxy, security-header baseline for non-document responses, rate limit, `CrossOriginProtection`, HSTS): that is `platform/httpx` (rank 2). This unit supplies the **response renderers** httpx needs (`Recovered`, `RateLimitedFunc`, `DenyCrossOrigin`) and states what it expects (section 2.5).
- No OTel in `httpapi`, `webapp` or `validate` (ranks and matrix: they cannot import it). Metrics and logs leave through callbacks wired in `cmd/scp` (section 9).
- No runtime brotli: static files are precompressed in the build (D8). No response-body validation in production. No CORS middleware (it must not exist, GR-18). No `Accept: text/html` gating of the SPA fallback (curl and unfurlers send `*/*`).
- No server-rendered HTML. `webapp` never generates markup; it serves bytes the build produced.
- No cursor semantics (what goes inside a cursor): each feature defines its keyset struct; this unit defines the envelope, the limits and the failure mapping.

### 1.4 Directories and files (exact; line counts are the prototype's, hand-written code only)

```
internal/httpapi/                    rank 2   imports: routes (not yet needed), platform/logx, httpapi/openapi
  mux.go          Mux, Mounter, MountAll
  problem.go      Problem, FieldError, codes, constructors, InstanceURN, WriteProblem, ProblemError/Err/Wrap/AsProblem,
                  StrictErrors, BindingError, Recovered, RateLimitedFunc, DenyCrossOrigin                       (309)
  cursor.go       MaxCursorLen, Cursor, PageParams, ParsePage, EncodeCursor, DecodeCursor, QueryFingerprint        (115)
  etag.go         ETagKind, WeakETag, WeakETagOf, WithDigest, FreshnessDigest, Digest, StrongETag, NotModified     (~150)
  static.go       ContentTypeYAML, Static                                                                          (35)
  accept.go       Accept / Accept-Encoding parsing: AcceptsAny, AcceptsEncoding, preferMarkdown                   (133)
  negotiate.go    Negotiate, WithAlternate                                                                         (41)
  gzip.go         Gzip, GzipOptions (stdlib compress/gzip, pooled)                                                 (169)
  defaults.go     APIPrefix, APIDefaults (acts under /api/ only), DefaultsOptions                                  (~75)
  envelope.go     FreshnessView, NewFreshness, NewPageInfo, NextLink                                               (~70)
  json.go         WriteJSON (the one encoder for hand-written JSON endpoints)                                      (27)
  request.go      WithRequest, RequestFrom (the *http.Request for strict handlers), Deref, Ptr                     (~35)
  redirect.go     Redirect                                                                                         (28)
internal/httpapi/openapi/            rank 0   generated: common.yaml (copy of 20-design/api/common.yaml), oapi.yaml, gen.go (go:generate), common.gen.go
internal/httpapi/authz/              rank 3   imports: scm, freshness, readmodel, httpapi
  principal.go    Principal, Authorizer, PublicOnly, WithPrincipal, PrincipalFrom                                  (54)
  class.go        Viewer, NewViewer, ClassOf                                                                       (47)
  lookup.go       LookupRepo                                                                                       (34)
internal/httpapi/validate/           rank 3   imports: httpapi; kin-openapi (cmd/scp is the only importer)
  validator.go    RequestValidator, Option, AllowUnknownQuery, WithObserver, APIPrefix, queryShape, mapError        (224)
  problemmux.go   ProblemMux                                                                                       (81)
internal/httpapi/apitest/            rank 3   imports: httpapi, openapi, apispec; kin-openapi; imported from _test.go only
  spec.go         Load, Lint (L1-L11)                                                                              (170)
  assert.go       ValidateResponse, ValidateProblem, ETagCoversBody, Normalised, NoLeak                           (~125)
internal/apispec/                    rank 0
  embed.go        Public(), Admin()  (//go:embed of openapi.bundled.yaml, openapi.admin.bundled.yaml)             (17)
  openapi.bundled.yaml, openapi.admin.bundled.yaml        generated by the redocly bundle step, committed (A-14)
internal/webapp/                     rank 0   imports NOTHING from this module
  options.go      Listener, CSPMode, Config(+Validate), ServeKind, CSPReport, Options(+defaults)                  (~100)
  handler.go      Handler, New, For, ServeHTTP, pick, serveFile, serveShell                                       (220)
  bundle.go       loadBundle: the one-time walk, allow-lists, siblings, size limits                              (214)
  routesjson.go   manifest, parseManifest, shape, shapeSet                                                         (~140)
  csp.go          DocumentCSP, permissionsPolicy                                                                   (29)
  cspreport.go    CSPReports (POST /csp-reports)                                                                   (193)
web/                                 rank 0   embed only; imported by cmd/scp only
  web.go                  Mode, ModePlaceholder, ModeSPA
  embed_placeholder.go    //go:build !spa   //go:embed placeholder   FS(), EmbeddedMode
  embed_spa.go            //go:build spa    //go:embed all:dist      FS(), EmbeddedMode
  placeholder/index.html, placeholder/routes.json         (committed; the only files under web/ that this unit adds)
```

Everything outside `web/placeholder/` that lives under `web/` (the React app, `web/src`, `web/dist`) belongs to the frontend units; `web/dist` is a build output and never committed (A-14).

---

## 2. Position in the architecture, and exact signatures

### 2.1 Ranks and the import matrix (10-architecture.md 5.2, restated for this unit)

| Rank | Package | Allowed internal imports | Third-party |
|---|---|---|---|
| 0 | `webapp` | none (reserved prefixes, links, build id, callbacks arrive through `Options`) | none |
| 0 | `apispec` | none | none |
| 0 | `httpapi/openapi` (generated) | none | `oapi-codegen/runtime` |
| 0 | `web` | none | none (`embed` only) |
| 2 | `httpapi` | `routes`, `platform/logx`, `httpapi/openapi` | none |
| 3 | `httpapi/authz` | `scm`, `freshness`, `readmodel`, `httpapi`, `platform/logx` | none |
| 3 | `httpapi/validate` | `httpapi`, `httpapi/openapi`, `platform/logx` | kin-openapi (**not** `nethttp-middleware`, D2) |
| 3 | `httpapi/apitest` | `httpapi`, `httpapi/openapi`, `apispec` | kin-openapi (imported from `_test.go` only) |

Facts that follow and that archtest MUST assert: `validate` is imported by `cmd/scp` only; kin-openapi appears in no package other than `validate` and `apitest`; `net/http/pprof` appears nowhere here; `httpapi` and `webapp` import no OTel module; `webapp` imports no module-internal package at all (its own `acceptsEnc` duplicates `httpapi.AcceptsEncoding` for exactly that reason, 20 lines); `web` is imported by `cmd/scp` only. The prototype's `httpapi` imports only `openapi` and `logx` (it does not need `routes` yet; the matrix allows it).

Third-party pins this unit needs (re-verified 2026-10-14, section 12.3): `github.com/getkin/kin-openapi v0.150.0`, `github.com/oapi-codegen/runtime v1.7.0`, generator `oapi-codegen v2.8.0` (run through `go run ...@v2.8.0` in `go:generate`, not a module dependency), Go 1.27.

### 2.2 `httpapi`: the exported surface (output of `go doc`)

```go
package httpapi

const APIPrefix = "/api/" // the only prefix APIDefaults and validate act on (validate.APIPrefix = httpapi.APIPrefix)

// ---- the request inside a strict handler (request.go) -----------------------------------------------------
// oapi-codegen strict handlers receive (ctx, requestObject) and no *http.Request, yet NotModified and NextLink need the
// request. cmd/scp mounts WithRequest innermost (directly around the API mux); handlers call RequestFrom(ctx).
func WithRequest(next http.Handler) http.Handler
func RequestFrom(ctx context.Context) *http.Request // nil when WithRequest is not mounted
func Deref[T any](p *T) T                           // generated optional parameters are pointers; nil gives the zero value
func Ptr[T any](v T) *T                             // for optional response header members (Headers.Link)

// ---- mux contracts (mux.go) -------------------------------------------------------------------------------
// Mux is exactly oapi-codegen v2.8.0's generated ServeMux interface. *http.ServeMux and *routes.Recorder satisfy it.
type Mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
	http.Handler
}
type Mounter interface{ Mount(m Mux) }
func MountAll(m Mux, ms ...Mounter) // a nil Mounter is a wiring bug: panics

// ---- problem details (problem.go) -------------------------------------------------------------------------
const (
	CodeValidationFailed     = "validation-failed"       // 400
	CodeInvalidCursor        = "invalid-cursor"          // 400
	CodeUnauthorized         = "unauthorized"            // 401
	CodeForbidden            = "forbidden"               // 403
	CodeNotFound             = "not-found"               // 404
	CodeMethodNotAllowed     = "method-not-allowed"      // 405 (+ Allow)
	CodeNotAcceptable        = "not-acceptable"          // 406
	CodePayloadTooLarge      = "payload-too-large"       // 413
	CodeUnsupportedMediaType = "unsupported-media-type"  // 415
	CodeRateLimited          = "rate-limited"            // 429 (+ Retry-After)
	CodeInternal             = "internal"                // 500
	CodeUpstreamUnavailable  = "upstream-unavailable"    // 503 (+ Retry-After)
	CodeNotReady             = "not-ready"               // 503 (+ Retry-After)
)
type FieldError struct {
	Pointer string `json:"pointer"` // "#/query/limit", "#/path/repo", "#/header/If-None-Match", "#/body"
	Detail  string `json:"detail"`
}
type Problem struct {
	Type              string       `json:"type"`     // "urn:scp:problem:<code>"
	Title             string       `json:"title"`
	Status            int          `json:"status"`
	Detail            string       `json:"detail,omitempty"`
	Instance          string       `json:"instance,omitempty"` // urn:uuid:<request id> (D4)
	Code              string       `json:"code"`
	Errors            []FieldError `json:"errors,omitempty"`
	RetryAfterSeconds int          `json:"retryAfterSeconds,omitempty"`
	Header            http.Header  `json:"-"` // allow-list on write: Allow, Retry-After, WWW-Authenticate (D3)
}
func TypeURI(code string) string
func New(status int, code, detail string) Problem
func Validation(errs ...FieldError) Problem
func InvalidCursor() Problem
func Unauthorized() Problem
func Forbidden(detail string) Problem
func NotFound(detail string) Problem
func MethodNotAllowed(allow ...string) Problem
func NotAcceptable(offered ...string) Problem
func PayloadTooLarge(max int64) Problem
func UnsupportedMediaType(want string) Problem
func RateLimited(retry time.Duration) Problem
func Internal() Problem
func UpstreamUnavailable(retry time.Duration) Problem
func NotReady(retry time.Duration) Problem
func InstanceURN(requestID string) string
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem)

type ProblemError struct {
	Problem Problem
	Cause   error // logged by StrictErrors, never rendered
}
func (e *ProblemError) Error() string
func (e *ProblemError) Unwrap() error
func Err(p Problem) error
func Wrap(p Problem, cause error) error
func AsProblem(err error) (Problem, bool)

func StrictErrors(log *slog.Logger) (request, response func(http.ResponseWriter, *http.Request, error))
func BindingError(w http.ResponseWriter, r *http.Request, err error) // fixed detail; the generated error types are per package (4.2)
func Recovered(log *slog.Logger) func(w http.ResponseWriter, r *http.Request, rec any)            // httpx recover hook
func RateLimitedFunc() func(w http.ResponseWriter, r *http.Request, retry time.Duration)           // httpx rate-limit hook
func DenyCrossOrigin() http.Handler                                                                // http.CrossOriginProtection deny handler

// ---- pagination (cursor.go, envelope.go) ------------------------------------------------------------------
const MaxCursorLen = 512
var ErrInvalidCursor = errors.New("httpapi: invalid cursor")
type Cursor string
type PageParams struct{ Limit int; Cursor Cursor }
func ParsePage(limit int, cursor string, def, max int) (PageParams, *Problem)
func EncodeCursor(v any) string                      // base64url(0x01 || JSON), no padding; panics on programmer error
func DecodeCursor(s string, into any) error          // strict; any failure is ErrInvalidCursor
func QueryFingerprint(q url.Values, keys ...string) string
func NextLink(r *http.Request, cursor string) string // `<path?query>; rel="next"` (relative reference)
func NewPageInfo(next string, limit int) openapi.PageInfo

// ---- conditional GET (etag.go) ----------------------------------------------------------------------------
type ETagKind byte
const (ETagSnapshot ETagKind = 'g'; ETagRepo ETagKind = 'r'; ETagVersion ETagKind = 'v')
func WeakETag(parts ...any) string                   // = WeakETagOf(ETagSnapshot, parts...): W/"g<part>-<part>..."
func WeakETagOf(kind ETagKind, parts ...any) string  // W/"r17-all" (per repository), W/"v1841" (snapshot endpoint)
func FreshnessDigest(statuses ...string) string      // "" when every status is "fresh", else Digest(statuses...)
func WithDigest(etag, digest string) string          // W/"g7-all" + "abc" -> W/"g7-all.abc"; "" digest returns etag unchanged
func Digest(parts ...string) string                  // 16 hex chars (FNV-1a 64) over arbitrarily many values
func StrongETag(body []byte) string
func NotModified(r *http.Request, etag string) bool  // GET/HEAD only; weak comparison; "*" matches

// ---- negotiation, compression, defaults, static, redirect (accept.go, negotiate.go, gzip.go, defaults.go, static.go, redirect.go, json.go)
const ContentTypeYAML = "application/yaml"           // RFC 9512
func AcceptsAny(accept string, offered ...string) bool
func AcceptsEncoding(header, coding string) bool
type NegotiateOption func(*negotiateConfig)
func WithAlternate(fn func(*http.Request) string) NegotiateOption
func Negotiate(spa, markdown http.Handler, opts ...NegotiateOption) http.Handler
type GzipOptions struct{ MinSize int; Level int }     // zero: 1024, gzip.DefaultCompression
func Gzip(o GzipOptions) func(http.Handler) http.Handler
type DefaultsOptions struct{ Private bool }
func APIDefaults(o DefaultsOptions) func(http.Handler) http.Handler
func Static(contentType string, body []byte) http.Handler
func Redirect(status int, target func(*http.Request) string, maxAge int) http.Handler
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, v any)

// ---- freshness envelope (envelope.go) ---------------------------------------------------------------------
type FreshnessView struct {
	Status        string // freshness.Status.String(): fresh | stale | error | unknown   (D6)
	Kind          string // item family, "commits.view"; REQUIRED (07 made `kind` a required string on 2026-10-14); "" is a bug the contract test catches
	AsOf, FetchedAt, CheckedAt, StaleSince time.Time // zero => null / absent
	LastErrorCode string
}
func NewFreshness(v FreshnessView) openapi.Freshness
```

### 2.3 `authz`, `validate`, `apitest`, `apispec`, `web`

```go
package authz // internal/httpapi/authz
type Principal struct{ Subject string; Groups []string; Anonymous bool } // zero value = anonymous
func (p Principal) IsAnonymous() bool                                    // Anonymous set OR Subject == ""
func NewPrincipal(subject string, groups []string) Principal             // empty subject yields the anonymous principal
func WithPrincipal(ctx context.Context, p Principal) context.Context     // called by the access middleware and tests
func PrincipalFrom(ctx context.Context) Principal                        // anonymous when absent
// An Authorizer MUST decide from (p, v) only and MUST NOT depend on ctx: ClassOf has no ctx parameter (architecture 8.14)
// and calls CanView with context.Background().
type Authorizer interface{ CanView(ctx context.Context, p Principal, v scm.Visibility) bool }
type PublicOnly struct{}                                                  // access.mode: public
func (PublicOnly) CanView(_ context.Context, _ Principal, v scm.Visibility) bool
type Viewer struct{ /* public, internal, private bool */ }
func NewViewer(ctx context.Context, az Authorizer, p Principal) Viewer    // public is always visible
func (v Viewer) Can(vis scm.Visibility) bool                             // unknown values: false
func (v Viewer) Class() scm.ViewClass                                    // ViewAll only if internal AND private visible (D18)
func ClassOf(az Authorizer, p Principal) scm.ViewClass
func LookupRepo(ctx context.Context, s *readmodel.Snapshot,
	repos readmodel.Slot[freshness.Value[scm.Repository]], az Authorizer, slug scm.Slug) (scm.Repository, *httpapi.Problem)

package validate // internal/httpapi/validate; imported by cmd/scp only
const APIPrefix = "/api/"
type Option func(*config)
func AllowUnknownQuery() Option
func WithObserver(f func(status int, code string)) Option
func RequestValidator(spec []byte, opts ...Option) (func(http.Handler) http.Handler, error) // D1: returns an error, never panics
func ProblemMux(next http.Handler) http.Handler

package apitest // internal/httpapi/apitest; _test.go importers only
func Load(t testing.TB, spec []byte) *openapi3.T
func Lint(doc *openapi3.T) []string
func ValidateResponse(t testing.TB, doc *openapi3.T, req *http.Request, rec *httptest.ResponseRecorder)
func ValidateProblem(t testing.TB, doc *openapi3.T, rec *httptest.ResponseRecorder)
func ETagCoversBody(t testing.TB, call func() (etag string, body []byte), mutate func())
func Normalised(body []byte) string
func NoLeak(t testing.TB, route string, rec *httptest.ResponseRecorder, needles ...string)

package apispec
func Public() []byte  // served at GET /api/v1/openapi.yaml by the public listener
func Admin() []byte   // the public document plus the admin-only operations; served by the admin listener

package web
type Mode string
const (ModePlaceholder Mode = "placeholder"; ModeSPA Mode = "spa")
const EmbeddedMode = ModePlaceholder          // ModeSPA in a -tags spa build
func FS() (fs.FS, error)                      // the bundle with index.html at its root
```

### 2.4 `webapp`: the exported surface

```go
package webapp
type Listener string; const (Public Listener = "public"; Admin Listener = "admin")
type CSPMode string
const (CSPReportOnly CSPMode = "report_only"; CSPEnforceCore CSPMode = "enforce_core"; CSPEnforce CSPMode = "enforce")
type Config struct{ CSP struct{ Mode CSPMode `yaml:"mode" json:"mode"` } `yaml:"csp" json:"csp"` } // YAML key `web`
func (c Config) Validate() []error

type ServeKind string
const (
	KindFile      ServeKind = "file"
	KindShell     ServeKind = "shell"
	KindShell404  ServeKind = "shell404"
	KindNotFound  ServeKind = "notfound"
	KindRedirect  ServeKind = "redirect"
	KindCSPReport ServeKind = "csp_report"
)
type CSPReport struct {
	Directive, Disposition, BlockedKind, DocumentPath, SourceFile string
	Line int
}
type Options struct {
	BuildID              string // X-SCP-Build; with CheckBuild it must equal routes.json buildId
	CheckBuild           bool   // true only in a spa build
	Reserved             []string // REQUIRED, must contain "/api/"; each entry starts and ends with "/"
	RootRedirect         string // "/" on the public listener, default "/dashboard"
	AdminRedirect        string // "/" on the admin listener, default "/status"
	CSP                  CSPMode // default report_only
	ReportPath           string  // default "/csp-reports"
	ReferrerPolicy       string  // default "strict-origin-when-cross-origin"
	RequirePrecompressed bool    // release builds: a compressible file >= 1 KiB without a .br sibling fails New
	OnCSPReport          func(ctx context.Context, r CSPReport)
	Observe              func(kind ServeKind, encoding string)
}
type Handler struct{ /* immutable after New */ }
func New(fsys fs.FS, o Options) (*Handler, error)
func (h *Handler) For(l Listener) *Handler       // same files, other route manifest (admin: /status)
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (h *Handler) CSPReports() http.Handler       // POST /csp-reports
func DocumentCSP(mode CSPMode) (enforce, reportOnly string)
```

### 2.5 The wiring this unit assumes (composition happens only in `cmd/scp`)

The API chain, outermost first, for **one** listener (the middleware that `httpx` owns wraps the whole mux and is not repeated):

```go
// cmd/scp, per listener. rv comes from validate.RequestValidator(apispec.Public() | apispec.Admin(), validate.WithObserver(...)).
api := http.NewServeMux()                        // the one mux per listener
rec := table.Mux(api)                            // *routes.Recorder (satisfies httpapi.Mux): the ONLY way to register an API-class route
httpapi.MountAll(rec, catalog, commits, docs, manifests, pages, overview /* ... */)
// api.openapi is an API-class route: table.Handle PANICS for it ("is an API route: register it through Table.Mux", verified).
// The admin listener passes apispec.Admin().
rec.HandleFunc("GET /api/v1/openapi.yaml", httpapi.Static(httpapi.ContentTypeYAML, apispec.Public()).ServeHTTP)

// EVERY other table entry needs a handler too, or table.Verify() fails at startup (it lists "no handler registered for ..."):
spaH := spa             // *webapp.Handler (admin listener: spa.For(webapp.Admin))
for _, r := range table.Routes(routes.ClassSPA) {              // spa.root, spa.dashboard, spa.group.commits, spa.commits, spa.repo.commits,
	if r.Owner == "webapp" { table.Handle(api, r.Name, spaH) } //   spa.repo.dependencies, spa.dependencies, spa.packages, spa.page, spa.fallback
}                                                              //   (admin table: spa.status, spa.fallback)
for _, n := range []string{"assets", "theme.init", "favicon", "manifest" /* , "frame.mermaid" in v1.1 */} { table.Handle(api, n, spaH) }
table.Handle(api, "csp.report", spaH.CSPReports())              // public listener only
// SPA-class entries owned by other units (negotiated.group, negotiated.repo, doc.md; and negotiated.repo.rest, which is server-class) are
// registered by cmd/scp with httpapi.Negotiate(spaH, markdownHandler): overview and docs receive spaH as an http.Handler dependency.
// badges, media, llms, webhook and probes are the other units'.

var h http.Handler = httpapi.WithRequest(api)            // 5. innermost: puts the *http.Request into ctx for strict handlers (NotModified, NextLink)
h = httpapi.Gzip(httpapi.GzipOptions{})(h)               // 4. dynamic compression (skips anything that already carries Content-Encoding)
h = httpapi.APIDefaults(httpapi.DefaultsOptions{Private: cfg.Access.Mode != "public"})(h) // 3. under /api/ only: 406 + Cache-Control default
h = rv(h)                                                // 2. request validation (acts under /api/ only)
h = validate.ProblemMux(h)                               // 1. text/plain 404/405 under /api/ -> problem+json
// ProblemMux, rv and APIDefaults pass every non-/api/ request through untouched. Gzip is the one that applies to every compressible
// dynamic response (llms.txt, markdown, sitemap, the OpenAPI document served by Static); it skips responses that carry Content-Encoding.
// then httpx.Chain(h, ...) with the hooks below; the access identity middleware (unit 14) is an httpx chain step after client-IP
// and outside this chain, so the Principal is already in ctx here.
```

Order is load-bearing (proved by `internal/e2e/chain_test.go` and `internal/httpapi/regress_test.go`): `ProblemMux` outermost so that a 404 produced by `rv` itself and one produced inside the mux both leave as problem+json; `rv` before `APIDefaults` so a request that fails validation never gets `Cache-Control: no-cache` (problems are `no-store`); `APIDefaults` outside `Gzip` so the 406 is not compressed and the ETag weakening in `Gzip` sees the final headers; `WithRequest` innermost so the stored request is the one the mux routes.

What `cmd/scp` MUST pass to `platform/httpx` (both are rank 2 and `httpx` imports only `logx`, so the renderers reach it as function values, never as an import):

| httpx hook | Value | Effect |
|---|---|---|
| recover | `httpapi.Recovered(log)` | 500 `internal` problem with the request id, log line with the route pattern. **httpx contract (added by the verification):** it MUST re-panic `http.ErrAbortHandler` instead of calling the hook, and MUST call the hook only when no response header has been sent yet (its writer wrapper tracks that); otherwise the problem would be a second `WriteHeader` on a half-written response |
| rate limit rejection | `httpapi.RateLimitedFunc()` | 429 problem with `Retry-After` and `retryAfterSeconds` |
| `http.CrossOriginProtection` deny handler | `httpapi.DenyCrossOrigin()` | 403 `forbidden` problem |
| request id | **UUIDv4** string (D4) in `logx.WithRequestID` | `instance` becomes a valid `urn:uuid:`; the `X-Request-Id` response header carries the same value |
| security-header baseline | httpx sets `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` and the per-class CSP for **non-document** classes (api, llms, badges, media: section 4.10.3) before the mux runs | `webapp` re-sets every header it owns on its own responses, so the two never disagree; `WriteProblem` sets `nosniff` itself so a problem is safe even if the baseline is absent |
| HSTS | httpx only (not `webapp`) | section 10 |

Cross-unit contracts this unit **requires** and the owner who must supply them are collected in section 14.2 (kernel `freshness.Status.String()`, the `routes` builders and table, the 07 contract changes, the `access` unit's `Authorizer`).

---

## 3. Domain model and types

### 3.1 The problem document (RFC 9457)

Wire shape (the schema is `Problem` in `common.yaml`; `required: [type, title, status, code]`):

```json
{
  "type": "urn:scp:problem:validation-failed",
  "title": "Request validation failed",
  "status": 400,
  "detail": "one or more request parameters are invalid",
  "instance": "urn:uuid:0e6c1d5a-7a3b-4c52-9f31-5d2f0f6a8b11",
  "code": "validation-failed",
  "errors": [{"pointer": "#/query/limit", "detail": "must be between 1 and 200"}]
}
```
(`detail` and `instance` are `omitempty`, `retryAfterSeconds` appears only on 429/503: the first draft's example showed `"detail": ""` and `"retryAfterSeconds": 0`, which the encoder never produces.)

| Member | Rule |
|---|---|
| `type` | `urn:scp:problem:<code>`: an identifier, not a dereferenceable URL (nothing serves it). Filled by `New` / `WriteProblem` |
| `title` | Fixed per code (see table); never request-specific |
| `status` | Equals the HTTP status; `WriteProblem` clamps anything outside 400-599 to 500 |
| `detail` | Occurrence text for humans and logs. **Never parsed by clients, never contains secrets, internal paths, SQL, upstream bodies or error strings of other packages** (`internal` has an empty detail) |
| `instance` | `urn:uuid:<request id>` when the id is a UUID; `urn:scp:request:<id>` when it matches `^[A-Za-z0-9._~-]{1,64}$` but is not a UUID; omitted otherwise |
| `code` | Open enum (features may add codes; the SPA keys on it, ignoring unknown ones to a generic message) |
| `errors[]` | Field violations of a `validation-failed`; `pointer` uses the URI-fragment form so it is also a valid JSON Pointer fragment |
| `retryAfterSeconds` | Equal to the `Retry-After` header when present (429, 503) |
| `Header` | Go-only (`json:"-"`): the headers the problem needs (`Allow` on 405, `Retry-After` on 429/503, `WWW-Authenticate` on 401). `WriteProblem` copies **only** these three |

Code table (the catalogue of 10-architecture.md 11.4 plus `unsupported-media-type`, which 07's `Problem` response already documents):

| Code | Status | Title | Emitted by |
|---|---|---|---|
| `validation-failed` | 400 | Request validation failed | `validate`, `ParsePage` (limit), `BindingError`, `StrictErrors` (malformed body) |
| `invalid-cursor` | 400 | Invalid cursor | `ParsePage` (syntax), features on `DecodeCursor` failure or fingerprint mismatch |
| `unauthorized` | 401 | Authentication required | `access` (only when `access.mode` requires identity) |
| `forbidden` | 403 | Forbidden | `DenyCrossOrigin`, `access` |
| `not-found` | 404 | Not found | `validate` (unknown API path), `ProblemMux`, `LookupRepo`, feature handlers |
| `method-not-allowed` | 405 | Method not allowed | `validate`, `ProblemMux` (with `Allow`) |
| `not-acceptable` | 406 | Not acceptable | `APIDefaults` |
| `payload-too-large` | 413 | Payload too large | `validate`, `StrictErrors` (`*http.MaxBytesError`) |
| `unsupported-media-type` | 415 | Unsupported media type | `validate` |
| `rate-limited` | 429 | Too many requests | `RateLimitedFunc` (httpx) |
| `internal` | 500 | Internal error | `Recovered`, `StrictErrors` (any unclassified error) |
| `upstream-unavailable` | 503 | Upstream unavailable | features that have no data and the upstream is down |
| `not-ready` | 503 | Not ready | features before the first snapshot (`Retry-After`) |

### 3.2 Cursor

Wire form: `base64url_nopad( 0x01 || JSON(struct) )`, at most 512 characters, charset `[A-Za-z0-9_-]`. The first byte is the **format version**; `DecodeCursor` rejects an unknown version, unknown JSON fields (`DisallowUnknownFields`), trailing data after the JSON value, and anything over 512 bytes, all as `ErrInvalidCursor`. A feature's cursor struct is private to the feature (keyset values such as `{"t": <unix>, "s": "<sha prefix>", "f": "<query fingerprint>"}`); it MUST contain `QueryFingerprint` of the filter/sort parameters so that a cursor used with different filters is `invalid-cursor`, never a silently wrong page. A payload of `null` or `{}` decodes without error into the zero struct, so the fingerprint comparison is what rejects it: every cursor struct MUST carry a non-empty `F` and the comparison is mandatory. A cursor is **not** a security token: it is not signed, so it must never contain data the caller may not see; features re-apply visibility on every page.

### 3.3 ETag

Format (the contract of `common.yaml`'s `ETag` header, unit 07): snapshot-derived resources `W/"g<snapshot version>-<view class>"`, per-repository resources `W/"r<repository revision>-<view class>"`, the snapshot endpoint `W/"v<version>"`, and a **`.<digest>` suffix inside the quotes while at least one contributing item is not fresh**, so a silent fresh-to-stale transition changes the tag (D15). Helpers: `WeakETagOf(kind, parts...)` (`WeakETag` is the `g` form) joins the parts with `-` and escapes every byte outside `[0-9A-Za-z._~:-]` as `%xx`, so the result is always a valid entity-tag whatever a part contains; `FreshnessDigest(statuses...)` returns `""` when every derived status is `"fresh"` (the common case keeps the short tag) and otherwise `Digest(statuses...)`; `WithDigest(etag, d)` appends `.d` inside the quotes (a no-op for `""`). `Digest` is FNV-1a 64 over NUL-separated parts, 16 hex characters; it is not cryptographic (a collision only yields one stale 304 for a pathological pair of states, and the version part also changes on every real publish). `StrongETag(body)` = `"` + base64url(first 15 bytes of SHA-256) + `"`, used for embedded immutable bytes (the OpenAPI bundles, the SPA files).

### 3.4 Principal, Viewer, ViewClass

`Principal{Subject, Groups, Anonymous}`: the zero value is anonymous. `Viewer` is the three answers of the `Authorizer` evaluated once per request (`public` is forced true: public data has no access control whatever a custom `Authorizer` says). `ClassOf` maps a viewer to the two kernel classes (`ViewPublic`, `ViewAll`); the architecture has exactly two classes (A-23), so a principal who may see `internal` but not `private` is mapped to `ViewPublic` (fail closed, D18, Q1).

### 3.5 The `routes.json` manifest (generated from `web/routes.ts` by the frontend build)

```json
{"version":1,"buildId":"<the build id>","routes":[
  {"pattern":"/dashboard","listeners":["public"]},
  {"pattern":"/repos/{repo}/docs/{path...}","listeners":["public"]},
  {"pattern":"/status","listeners":["admin"]}]}
```

Strict: unknown fields, `version != 1`, duplicate patterns, patterns that do not start with `/`, equal `/`, or end with `/`, unknown listeners, segments that are neither a literal (`[A-Za-z0-9._~-]+`) nor `{name}` nor a **final** `{name...}` are all start-time errors. `/` is never listed (it is a redirect). A `{name}` needs one non-empty segment, a `{name...}` one or more; matching is done on the **escaped** path so `%2F` inside a segment never adds a segment. The manifest file is removed from the served file set (the SPA never fetches it).

### 3.6 Bundle (in memory, immutable after `New`)

`files map[path]*file{body, br, gz []byte; ctype string; etag string; immutable bool}`; `shell *file` (index.html); `manifest`. ETags are `"<hash>"`, `"<hash>-br"`, `"<hash>-gz"` where `<hash>` = `StrongETag`-style digest of the identity bytes (strong, one per representation, so a cache never confuses encodings).

---

## 4. Behavioural spec

### 4.1 Writing a problem (`WriteProblem`)

```
func WriteProblem(w, r, p):
    if p.Status not in 400..599: p.Status = 500
    if p.Code == "":  p.Code = "internal"
    if p.Type == "":  p.Type = "urn:scp:problem:" + p.Code
    if p.Title == "": p.Title = http.StatusText(p.Status)
    if p.Instance == "": p.Instance = InstanceURN(logx.RequestID(r.Context()))
    body = JSON(p) with SetEscapeHTML(false)        // on encoder failure: http.Error 500 and return (cannot happen)
    delete headers that describe another representation:
        ETag, Content-Length, Content-Encoding, Content-Range, Content-Disposition, Last-Modified, Link, Accept-Ranges
    copy ONLY Allow, Retry-After, WWW-Authenticate from p.Header
    set Content-Type: application/problem+json ; X-Content-Type-Options: nosniff ; Cache-Control: no-store ; Content-Length: len(body)
    WriteHeader(p.Status)
    if r.Method != HEAD: write body
```

Why the deletes: a handler may have set `ETag`/`Content-Encoding` before failing; a problem must never inherit them (a cached error body under the success ETag is the failure mode). Why `no-store`: problems are per-occurrence (`instance`). `WriteProblem` is the **only** way an error leaves any API handler or middleware of this module.

### 4.2 The strict-server error path

Handlers implement the generated `StrictServerInterface` and return problems as **errors**: `return nil, httpapi.Err(httpapi.NotFound("..."))` (D16). `StrictErrors(log)` returns the two functions for `StrictHTTPServerOptions{RequestErrorHandlerFunc, ResponseErrorHandlerFunc}`:

```
request(err):   // body decode failures that got past the validator
    *http.MaxBytesError   -> PayloadTooLarge(limit)                       413
    *json.SyntaxError     -> Validation({#/body, "malformed JSON"})        400
    other                 -> Validation({#/body, "request body could not be decoded"})   400   // fixed text: library messages carry Go field names and client input
response(err):
    ProblemError (errors.As) -> WriteProblem(p); log at ERROR when p.Status >= 500 (with r.Pattern, code, err)
    context.Canceled AND r.Context().Err() != nil -> write nothing (the client left)
    anything else -> log at ERROR (err, r.Pattern); WriteProblem(Internal())      // the error text is never rendered
```

`HandlerWithOptions(..., StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})` routes parameter-binding failures (which the validator normally rejects first) to a `validation-failed` problem with pointer `#/` and the fixed detail `a request parameter could not be bound`: the generated error types (`InvalidParamFormatError`, `RequiredHeaderError`, `TooManyValuesForParamError`, ...) are declared once **per generated package**, so `httpapi` cannot type-switch on them, and their `Error()` text is never rendered (verified by reading `commits.gen.go`; the first draft rendered it, against its own T9). One case is reachable in practice: two `If-None-Match` header **lines** make the generated binding answer `TooManyValuesForParamError`, i.e. 400 here; the supported form is one line with a comma list. A strict handler has no `*http.Request` parameter (`StrictServerInterface` methods take `(ctx, request object)`), so `NotModified` and `NextLink` read it through `httpapi.RequestFrom(ctx)` (D29). A handler may also use the typed default response the generator produces; both paths end in `WriteProblem`-equivalent bytes, but the error path is the only one that can carry `Allow`/`Retry-After` today (07's shared `Problem` response declares no headers, Q4).

### 4.3 Pagination

```
func ParsePage(limit, cursor, def, max) (PageParams, *Problem):
    if limit == 0: limit = def                        // 0 means "absent" (the generated param is *int, nil -> 0)
    errs = []
    if limit < 1 or limit > max: errs += {#/query/limit, "must be between 1 and <max>"}
    if cursor != "" and not validCursorSyntax(cursor):  return InvalidCursor()      // charset + 1..512
    if errs: return Validation(errs...)
    return {limit, Cursor(cursor)}
```

Defaults and maxima (07 `Limit`/`LimitBounded`): unbounded collections (commits, dependencies, components) `def 50, max 200`; config-bounded collections (groups, repos, docs, pages) `def 1000, max 1000`. A feature handler:

```
req := httpapi.RequestFrom(ctx)                       // strict handlers get (ctx, request object) only; WithRequest is mounted innermost
q := req.URL.Query()
pp, prob := httpapi.ParsePage(httpapi.Deref(p.Limit), httpapi.Deref(p.Cursor), 50, 200); if prob != nil { return nil, httpapi.Err(*prob) }
var c myCursor
if pp.Cursor != "" {
    if err := httpapi.DecodeCursor(string(pp.Cursor), &c); err != nil || c.F != httpapi.QueryFingerprint(q, "repo","group") {
        return nil, httpapi.Err(httpapi.InvalidCursor())
    }
}
rows := keyset query/merge, fetching pp.Limit+1
next := ""; if len(rows) > pp.Limit { rows = rows[:pp.Limit]; next = httpapi.EncodeCursor(myCursor{...last row..., F: fp}) }
resp.Page = httpapi.NewPageInfo(next, pp.Limit)          // nextCursor: null on the last page
if next != "" { resp.Headers.Link = httpapi.Ptr(httpapi.NextLink(req, next)) }   // absent on the last page
```

`NextLink` builds `<` + `req.URL.Path` + `?` + the request's query with `cursor` replaced (sorted by `url.Values.Encode`) + `>; rel="next"`. It is deliberately a **relative reference** (RFC 8288 allows it; the client resolves it against the request URI), so no `Host` header or `server.public.public_url` is trusted. Cursor invariants: a cursor MUST remain valid while the underlying snapshot advances (keyset by value, never by offset or by snapshot version); a cursor older than the data it points past is still valid (it just starts from that key).

### 4.4 Conditional GET

Rule: every cacheable GET returns `ETag` (weak) and `Cache-Control: no-cache` (added by `APIDefaults`, `private, no-cache` when `access.mode != public`); `If-None-Match` matching answers `304` with **no body and the same `ETag` and `Cache-Control`**; HEAD is answered like GET without a body (the standard library discards the body for HEAD; the handler runs the same code).

`NotModified(r, etag)`:

```
if method not in {GET, HEAD} or no If-None-Match header lines or etag == "": return false
want = strip "W/" prefix from etag
for each header line, for each token in splitETags(line):      // lines are joined as if comma-separated
    if token == "*" or strip "W/"(token) == want: return true  // weak comparison, RFC 9110 13.1.2
return false
```

`splitETags` is a tokenizer, not `strings.Split(",")`: opaque tags may contain commas inside the quotes; a malformed token is skipped to the next comma (never an error, never a match). Handler recipe (the **only** shape allowed):

```go
etag := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), class), httpapi.FreshnessDigest(statusOfEveryItem...)) // BEFORE building the body
if httpapi.NotModified(httpapi.RequestFrom(ctx), etag) { return ListThings304Response{Headers: openapi.NotModifiedResponseHeaders{ETag: &etag}}, nil }
body := build(...)                                                                        // may be skipped entirely on 304
return ListThings200JSONResponse{Body: body, Headers: ListThings200ResponseHeaders{ETag: &etag, Link: link}}, nil   // link = httpapi.Ptr(httpapi.NextLink(req, next)) or nil
```

**D15 (the freshness trap).** `freshness.Info.Derive(now)` is evaluated in the handler (architecture 8.2: time is never data). The derived `status` of an item changes from `fresh` to `stale` when `now - FetchedAt` crosses `1.5*TTL` **without a new snapshot**. An ETag made only of the snapshot version would keep answering 304 while the body would now say `stale`. Therefore the ETag MUST carry `FreshnessDigest` of every derived status that appears in the body, in body order (for a list: each row's status, then the aggregate), whenever any of them is not `fresh`. `apitest.ETagCoversBody(t, call, mutate)` is the property test every conditional endpoint runs: call, mutate one input (flip a status, add a row), call again, fail if the body changed and the ETag did not.

Cache correctness with a shared cache in front: the ETag also carries the view class, and `Cache-Control` is `private` when responses depend on identity. A response that varies by `Accept` (negotiated routes) sets `Vary: Accept`; one that varies by `Accept-Encoding` sets it too (the gzip middleware adds it on every pass, including pass-through).

### 4.5 Content negotiation

**Accept parsing** (`accept.go`): comma-separated media ranges with optional `q` (0..1, malformed `q` counts as 0), `type/subtype`, `type/*`, `*/*`; specificity beats breadth (`text/markdown` > `text/*` > `*/*`) when computing the q of a candidate; unknown parameters ignored; case-insensitive. `AcceptsAny(accept, offered...)` is true when the header is absent/empty (anything is acceptable) or at least one offered type has q > 0.

**`APIDefaults`** (acts **only** when the path starts with `/api/`; every other request passes through untouched: found by the verification, the first prototype applied it to every path and answered `406` to `Accept: text/markdown` on `/repos/{repo}` and `/llms.txt`, to `Accept: text/html` on `/dashboard`, and overwrote the `Cache-Control` of badges; tests `TestAPIDefaultsOnlyUnderAPI`): if `Accept` is present, well-formed and excludes every API type (`application/json`, `application/problem+json`, `application/yaml`, `application/vnd.cyclonedx+json`, `text/event-stream`) it answers `406 not-acceptable` (`detail` lists the offered types). Absent header, `*/*`, `application/*` all pass. After the handler, for GET/HEAD responses with status 2xx or 304 that carry no `Cache-Control`, it sets `no-cache` (or `private, no-cache`). A `ccWriter` wraps the writer: it implements `Unwrap` (so `http.ResponseController` reaches the real writer for SSE deadlines) and `Flush`.

**`Negotiate(spa, markdown)`** (human URLs that also have a Markdown twin: `/repos/{repo}`, `/groups/{groupId}`, `/repos/{repo}/docs/{path...}`):

```
w.Header().Add("Vary", "Accept"); w.Header().Add("Link", `</llms.txt>; rel="describedby"`)
if preferMarkdown(Accept): markdown.ServeHTTP
else: if WithAlternate(fn) and fn(r) != "": Link += `<url>; rel="alternate"; type="text/markdown"`
      spa.ServeHTTP
```

`preferMarkdown` is true **only** when `text/markdown` is explicitly present with q > 0 and strictly greater than the q of `text/html` (ties and `*/*` mean HTML). It never answers 406 (D23): a human URL always has an HTML representation, and `curl`'s `*/*` gets the shell. This means a client sending `Accept: text/markdown, text/html;q=0.9` gets Markdown; `Accept: text/html, text/markdown` (equal q) gets HTML.

### 4.6 Dynamic compression (`Gzip`)

```
if HEAD or Range header or client does not accept gzip: add Vary: Accept-Encoding; pass through
wrap writer; decide on the first Write/WriteHeader/Flush:
    pass-through when: status 1xx, 204, 206, 304; Content-Encoding already set; content type is image/*, text/event-stream,
                       or not in the compressible set (json, problem+json, yaml, markdown, text/*, html, xml, +json, +xml)
    otherwise buffer until MinSize (default 1024 B); below it: send identity; at or above: compress with a pooled gzip.Writer
when compressing: Content-Encoding gzip; delete Content-Length; Vary Accept-Encoding; a STRONG ETag becomes weak (W/ prefix, RFC 9110 8.8.1)
Flush() before the decision forces the decision (compress if the type allows) and passes the flush down; Unwrap() exposes the real writer.
```

Dynamic gzip only, level default; no brotli at run time (D8). `webapp` files are not compressed here when they are served from a precompressed sibling (they then carry `Content-Encoding`); an identity file of 1 KiB or more with no sibling (only possible in a placeholder or dev build, `RequirePrecompressed` forbids it in a release) **is** compressed on the fly like any other response. `Static` (the OpenAPI document, `application/yaml`, ~182 KB bundled, measured) is compressed per request (about 2 ms of CPU, ~33 KB on the wire) and its strong ETag is weakened by `Gzip`; `If-None-Match` still matches because the comparison is weak. Caching a gzip copy inside `Static` is not worth the state (design choice). The stdlib `compress/gzip` is used instead of `klauspost/gzhttp` because the import matrix gives `httpapi` no third-party row (D7).

### 4.7 Request validation (`validate.RequestValidator`)

Construction (`RequestValidator(spec, opts...)`, once, in `cmd/scp`): load the **bundled, self-contained** document with `openapi3.NewLoader().LoadFromData(spec)` (external `$ref`s fail: that is why the redocly bundle exists), `doc.Validate`, set `doc.Servers = nil` (any `Host` matches), `gorillamux.NewRouter(doc)`. Any failure returns an error and `cmd/scp` aborts startup (D1).

Per request, only when the path starts with `/api/` (everything else passes through):

```
lookup = r (HEAD resolves as GET for routing)
route, params, err = router.FindRoute(lookup)
  err is ErrMethodNotAllowed -> 405 method-not-allowed, Allow = methods the document defines for the path (HEAD added with GET)
  any other err              -> 404 not-found "no such API route"
errs = queryShape(route, r)            // unknown query parameters; repeated values of scalar parameters
verr = openapi3filter.ValidateRequest(ctx, {Request: lookup, PathParams, Route, Options{MultiError, ExcludeResponseBody, NoopAuthentication}})
if verr: p, fe = mapError(verr); if p != nil -> reject(p)  (413 / 415) ; errs += fe
if errs: sort by pointer; 400 validation-failed with errors[]
else next.ServeHTTP
```

`mapError` works on **typed errors** (never kin's message strings) with **one exception that kin forces**: kin-openapi has no typed error for an unsupported request `Content-Type`; `ValidateRequestBody` returns a `RequestError` whose `Reason` starts with the unexported constant `"header Content-Type has unexpected value"` (verified in v0.150.0 `validate_request.go:260,323`; kin's own `ValidationErrorEncoder` matches the same prefix), and there is no `ErrInvalidContentType` (the first draft named one). `mapError` therefore matches that prefix, **only** when `RequestBody != nil`, and `TestValidator` (case `post wrong content type`) pins it and the verification's probe covers `no ct` and `application/x-www-form-urlencoded` so a kin upgrade that changes the text fails a test instead of turning 415 into 400. Order matters: check `*openapi3filter.RequestError` **before** `openapi3.MultiError` (a `RequestError` can wrap a nested `MultiError`; matching the outer one first flattens it wrongly).

| kin error | Result |
|---|---|
| `*RequestError` with `Parameter` | `FieldError{"#/"+in+"/"+name, reason}`; for a schema failure the `SchemaError.JSONPointer()` is appended and `Reason` is the detail; missing required parameter: detail `required` |
| `*RequestError` with `RequestBody` | `FieldError{"#/body", "body does not match the schema"}` (fixed text, D13a: kin's body failures are multi-line dumps with no stable pointer; the single v1 body operation `retry` is tiny) |
| `*RequestError` wrapping `*http.MaxBytesError` | 413 `payload-too-large` |
| `*RequestError` with `RequestBody` whose `Reason` has the kin Content-Type prefix (the one string match) | 415 `unsupported-media-type` (`expected Content-Type application/json`); `application/json; charset=utf-8` is accepted |
| `MultiError` | each element visited |
| anything else | `FieldError{"#/", "invalid request"}` (never the message) |

Details are cleaned and cut to 200 bytes: kin's parameter failures arrive as a multi-line dump (`jsonschema validation failed with 'https://example.com/schema.json#'\n- at '': maximum: got 500, want 200`), and `cleanReason` keeps only the last line without the `- at '..': ` location prefix and replaces control characters (found by running the validator; without it clients would see the fake schema URL; D26). A value the client sent may be echoed in a detail (for example the rejected slug); details are JSON strings only, never HTML. `kin` reports only the **first** parameter error of a request (a limitation of `MultiError` in v0.150.0; the unknown-parameter check runs separately, so `?limt=5&limit=999` reports both). `WithObserver(func(status int, code string))` is called for every rejection (wired to `scp_api_rejected_total`). `AllowUnknownQuery()` exists for tests and for a deliberate escape; production does not use it: an unknown parameter is a 400 so a typo such as `?limt=5` cannot silently return an unfiltered list (D13). Repeated scalar parameters (`limit=1&limit=2`) are rejected for the same reason (kin accepts them and uses the first). An **empty** value (`?limit=`, `?cursor=`) passes the validator and is read as absent by the generated binding (`ParsePage` limit 0 / cursor ""); a value the schema cannot parse (`?limit=abc`) is `400 #/query/limit "invalid value"`. A body with undeclared members (`{"key":"a/b","x":1}`) is accepted (07's `RetryJobRequest` sets no `additionalProperties: false`; 07's call, cross-unit row in 14.2). Verified on the **real bundled 07 documents** (redocly CLI 2.63.0 `bundle`, deterministic output; both documents load, `doc.Validate` passes, `Lint` is clean, and more than 60 probe requests behave as this section says).

Why not `oapi-codegen/nethttp-middleware` (the architecture's choice, D2). Re-verified on its `main` source (2026-10-14, by the independent verification), the first draft's evidence was partly wrong: in `ErrorHandlerWithOpts` mode every `FindRoute` error is a `404` (no 405, as claimed), but its default `ErrorHandler` path does return 405 (`errors.Is(err, routers.ErrMethodNotAllowed)`) without an `Allow` header; `Prefix` rewrites `r.URL` **temporarily** and restores it (it does not mutate it for downstream handlers, as claimed); there is no HEAD-as-GET mapping in the file. The decision stands on what is still true: no `Allow`, no HEAD mapping, no unknown-query-parameter rejection, and error rendering from the validator's typed error needs the code written here anyway; the ~150 lines of code on kin-openapi + gorillamux are covered by tests. The matrix entry for `nethttp-middleware` on `validate` is unused and should be dropped. Dependency note: `routers/gorillamux` links `github.com/gorilla/mux` (v1.8.0 pinned by kin; v1.8.1 exists), a transitive third-party package the matrix does not list.

**`ProblemMux`**: a response writer wrapper active only under `/api/`. When the handler writes status 404 or 405 **and** `Content-Type` starts with `text/plain` **and** `X-Content-Type-Options: nosniff` is set (exactly what `http.NotFound`, `http.Error` and `ServeMux` emit) it discards the body and writes a problem instead (`Allow` copied from the original on 405; `nosniff` is **kept**: the first draft deleted it, so every rewritten 404/405 left without `X-Content-Type-Options`, and `WriteProblem` now sets it itself; test `TestProblemMuxRewriteKeepsNosniff`). Our own problems carry `application/problem+json`, so they are never rewritten. It is a safety net behind the validator: the validator answers unknown API paths from the document, but the SPA catch-all (`GET /{path...}`) and any handler that calls `http.NotFound` would otherwise leak `text/plain` into the API.

### 4.8 Visibility (`authz`)

```
func LookupRepo(ctx, snap, repos, az, slug):
    miss = NotFound("repository not found")          // ONE problem value for all three cases
    if scm.ParseSlug(slug) fails: return miss      // kernel grammar: canonical lower-case [a-z0-9._-]{1,100}, no "." / "..", no reserved word
    v, ok = readmodel.Get(snap, EntryID(slug), repos); if !ok: return miss
    if !NewViewer(ctx, az, PrincipalFrom(ctx)).Can(v.V.Visibility): return miss
    return v.V, nil
```

The three failure causes produce **identical bytes** (apart from `instance`): the API is not an existence oracle. This is tested (`reflect.DeepEqual` of the problems, then byte equality of two rendered responses with `Normalised`). Rules for every feature (enforced by review checklist and by the proposed AST test D19):

1. A handler that takes a slug or repo id from the request resolves it with `LookupRepo` and nothing else.
2. A handler that lists repositories builds one `Viewer` per request (`authz.NewViewer(ctx, az, authz.PrincipalFrom(ctx))`) and calls `Viewer.Can(repo.Visibility)` per item. `Authorizer.CanView` is never called in a loop.
3. A handler that returns a **global projection** (commits.latest, manifests graph, site repo count, group badge counts, `llms-full.txt`) picks the variant `scm.Classed[T].For(authz.ClassOf(az, principal))`. The anonymous principal can only reach the `public` variant.
4. `PublicOnly` is the authorizer for `access.mode: public`: public repositories are visible to everyone, anything else to no one.

### 4.9 SPA hosting: construction (`webapp.New`)

Done once; **nothing is read from the filesystem per request** and a bad build fails startup (GR-19):

```
Options.defaults(): Reserved non-empty, every entry starts and ends with "/", contains "/api/"; fill RootRedirect, AdminRedirect, CSP, ReportPath, ReferrerPolicy
walk fsys once; collect ALL problems, return errors.Join:
    symlink                                              -> error
    file > 8 MiB, dist > 16 MiB total                    -> error
    *.map                                                -> error ("source maps must not be embedded", GR-19)
    extension not in the allow-list                      -> error  (allow-list below; no OS mime database is consulted)
    a second *.html (only index.html may be HTML; it carries the CSP) -> error
    a file under assets/ whose stem is not <name>-<8 characters of [A-Za-z0-9_-]>   -> error  (immutable caching is only safe for hashed names)
    foo.gz / foo.br without foo -> error (orphan); a .gz that does not decompress to foo -> error; a .gz or .br not smaller than foo -> ignored
    (a .br is checked by size only: the standard library has no brotli decoder; see "Brotli siblings" below)
    RequirePrecompressed and a compressible file >= 1 KiB with no usable .br sibling -> error
    a file under api/ (it would shadow the API)          -> error
    index.html or routes.json missing                    -> error
    routes.json invalid (section 3.5)                    -> error
    CheckBuild and routes.json buildId != Options.BuildID -> error ("stale dist?")
remove routes.json from the served set; compute ETags; compile shapes for Public
```

**Hashed names (verified defect).** Vite/Rolldown names are `<name>-<hash>.<ext>` with the default 8-character base64url hash (`[hash:8]`), and the alphabet contains `-` and `_`. The rule is therefore "the 8 characters before the extension are in `[A-Za-z0-9_-]` and are preceded by `-`, with at least one character of name before that dash" (`hashLen = 8`); the web build pins `build.rollupOptions.output.{assetFileNames,chunkFileNames,entryFileNames}` to `assets/[name]-[hash:8].[ext]` (`.js`) so the length is a contract, not an observation. The first draft took the text after the **last** `-` as the hash and rejected `assets/index-Ab-d1234.js`: with 8 random characters about 12 % of names contain a `-`, so a 20-asset build started with probability about 8 % (`TestHashedAssetNamesFromViteAreAccepted`). The rule is a guard against unhashed output (`assets/app.js`, `assets/my-long-name.js` are rejected), not a proof of hashing.

**Brotli siblings.** Go cannot decode brotli with the standard library, so `New` verifies a `.gz` by decompression but a `.br` only by size. A corrupted `.br` would reach every brotli-capable browser as a blank page, so the **web build's precompression step must round-trip every file** (`zlib.brotliDecompressSync(br)` equals the source, else the build fails; 6.3) and the Playwright CSP gate (which loads the SPA through the real server with `Accept-Encoding: br`) is the second net. A test-only dependency on a brotli decoder in `webapp`'s tests is allowed by the matrix only as a "brotli fallback if precompression is unavailable" and is not taken (D8).

Extension allow-list and content types (hard-coded in a `switch`, no globals): `.html text/html; charset=utf-8`, `.js .mjs text/javascript; charset=utf-8`, `.css text/css; charset=utf-8`, `.json application/json`, `.webmanifest application/manifest+json`, `.svg image/svg+xml`, `.png`, `.jpg/.jpeg`, `.gif`, `.webp`, `.avif`, `.ico image/x-icon`, `.woff2 font/woff2`, `.woff font/woff`, `.txt text/plain; charset=utf-8`. Compressible (precompressed siblings expected): html, js, mjs, css, json, webmanifest, svg, txt, ico. Anything else in `dist` is a build error: adding a file type is a deliberate code change.

### 4.10 SPA hosting: serving (`Handler.ServeHTTP`)

#### 4.10.1 Decision order (the table is the spec; the prototype test `TestRouting` asserts every row)

Every response first gets `X-Content-Type-Options: nosniff` and, if configured, `X-SCP-Build: <BuildID>`.

| # | Condition (in this order) | Response |
|---|---|---|
| 1 | method not GET/HEAD | 405, `Allow: GET, HEAD`, `text/plain`, `Cache-Control: no-store`, sandbox CSP. With the registrations of 2.5 (every SPA pattern is `GET ...`) `ServeMux` answers a `POST`/`OPTIONS` to an SPA path itself with the same status and `Allow` (text/plain, verified on Go 1.27.1, `TestWiringAgainstTable`) and `webapp` never sees it; this row is the behaviour of a `webapp.Handler` used on its own |
| 2 | path `""` or `/` | 302 to `RootRedirect` (public) or `AdminRedirect` (admin), `Cache-Control: no-store` |
| 3 | `p` (the decoded path, as `ServeMux` already cleaned it) names a file in the bundle by its exact name (not index.html; `/theme-init.js/` is not the file) | the file (4.10.2). Checked **before** the reserved prefixes because `routes.Table.ReservedPrefixes()` lists `/assets/` (it is not an SPA page) and the assets are `webapp`'s own files; `New` refuses any bundle file under `/api/` |
| 4 | path has a `Reserved` prefix (from the table: `/.well-known/`, `/_frame/`, `/api/`, `/assets/`, `/badges/`, `/media/`, `/schemas/`, `/webhooks/`; `/api/` is mandatory) | 404 plain text (`default-src 'none'; frame-ancestors 'none'; sandbox`). The shell is **never** returned under these prefixes |
| 5 | the **escaped** path matches a manifest shape of this listener | 200 with the shell (4.10.3) |
| 6 | path ends in `/` and the trimmed path matches a shape | 308 to the trimmed path, query string kept, `Cache-Control: public, max-age=3600` |
| 7 | path starts with `/assets/` or has a file extension | 404 plain text (a stale deploy must never answer HTML for a JS/CSS request) |
| 8 | otherwise | **404 with the shell body** (`Content-Length` set, no `ETag`, no conditional handling): the SPA renders its NotFound page and the status is a real 404 (GR-16) |

Rule 5 before rule 7 is deliberate (D5): the architecture's wording ("any path ending in `.md`, `.adoc`, `.txt`, `.xml`, `.svg`, `.json`, `.yaml` is reserved") would reject the SPA route `/repos/next.js` (a real repository slug with a dot). Dotted slugs are legal (`[A-Za-z0-9._-]`), so the extension rule applies only to paths that did **not** match an SPA shape. The server-only routes that really end in those extensions (`/repos/{repo}/readme.md`, `.../dependencies.md`, `/llms.txt`, `/sitemap.xml`, `/robots.txt`) are registered **before** the catch-all in the mux and never reach `webapp`; their exact patterns are in `routes`, and `routes.Verify()` guarantees the more specific pattern wins.

#### 4.10.2 Files

```
pick(f, Accept-Encoding): br if f.br != nil and q(br) > 0 ; else gzip if f.gz != nil and q(gzip) > 0 ; else identity   // br > gzip > identity
headers: Content-Type = f.ctype; ETag = per-representation strong tag; Vary += Accept-Encoding; Content-Encoding when not identity
Cache-Control: assets/* (hashed) -> "public, max-age=31536000, immutable";  everything else -> "no-cache"
http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))      // Range, HEAD, If-None-Match, If-Range, 304 for free; no Last-Modified
```

`Accept-Encoding` parsing honours `q=0` and `*`. `theme-init.js`, `favicon.svg`, `manifest.webmanifest` and `index.html` are `no-cache` with an ETag (D22: `theme-init.js` is on the critical path of first paint; an ETag revalidation costs one round trip per page view; Q5 asks whether to hash and inline-reference it instead).

#### 4.10.3 The shell (index.html) and the header classes

Shell 200 response headers: `Content-Type: text/html; charset=utf-8`, `Cache-Control: no-cache`, `ETag`, `Vary: Accept-Encoding`, `Content-Encoding` as picked, plus the **document header set**:

| Header | Value |
|---|---|
| `Content-Security-Policy` / `-Report-Only` | by `web.csp.mode` (4.11) |
| `Reporting-Endpoints` | `csp="<ReportPath>"` (default `csp="/csp-reports"`) |
| `X-Frame-Options` | `DENY` |
| `Cross-Origin-Opener-Policy` | `same-origin` (no COEP) |
| `Referrer-Policy` | `strict-origin-when-cross-origin` (configurable through `Options`) |
| `Permissions-Policy` | `accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()` |

Static files other than the shell carry only `nosniff` and `X-SCP-Build` (a CSP on a JS file is meaningless; `Cross-Origin-Resource-Policy` is not set on the SPA's own files). The other response classes (owned by httpx per 10-architecture.md 11.3, repeated here so the two stay equal): `/api/*`, `llms*.txt`, `*.md`: `default-src 'none'; frame-ancestors 'none'; sandbox`; `/badges/*`, `/media/*`: `default-src 'none'; sandbox` (`Cross-Origin-Resource-Policy: cross-origin` on badges, `same-origin` elsewhere); plain 404/405 from `webapp`: `default-src 'none'; frame-ancestors 'none'; sandbox`. No `Server`, no `X-Powered-By`, no `X-XSS-Protection`, no `Access-Control-*` anywhere.

### 4.11 The document CSP and its rollout

Static (no nonce, no per-request value; ADR-010, A-16). Exact strings (golden-tested):

```
core  = default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self';
        manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'
tt    = require-trusted-types-for 'script'; trusted-types scp-html
full  = core + "; " + tt + "; report-to csp"
corer = core + "; report-to csp"
```

| `web.csp.mode` | `Content-Security-Policy` | `Content-Security-Policy-Report-Only` | Meaning |
|---|---|---|---|
| `report_only` (default) | absent | `full` | Step 1: observe |
| `enforce_core` | `corer` | `full` | Step 2: enforce script/style/etc.; Trusted Types still report-only |
| `enforce` | `full` | absent | Step 3: everything enforced |

(D9: the architecture lists only `web.csp.mode: report_only` in the config outline; the three values encode the "Report-Only first, script/style next, Trusted Types last" rollout of 10-architecture.md 11.3 and are validated by `Config.Validate`.) The theme pre-paint script is the external `/theme-init.js`, so no inline script, hash or nonce exists. `report-to csp` is enough: MDN lists `report-to` as "Baseline 2026, newly available, since March 2026" (checked 2026-10-14) while still showing both directives side by side for older browsers, so `report-uri` is **not** added (design choice: Firefox and Safari engines that lack `report-to` simply send nothing; the `application/csp-report` format of `report-uri` is still accepted by the endpoint). `Reporting-Endpoints: csp="/csp-reports"` may be a relative URL (Reporting API section 3.3 parses it against the response URL; verified at w3c.github.io/reporting 2026-10-14) but the endpoint's origin must be **potentially trustworthy**: over plain `http://` on a non-localhost host (a bare binary on an intranet without TLS) browsers ignore the header and **no report is ever sent**, so zero reports there proves nothing. The rollout runbook (M7) therefore only advances `web.csp.mode` from a deployment that serves HTTPS (or localhost) and from Playwright, never from "zero reports" on a plain-HTTP deployment. v1.1 Mermaid adds a `/_frame/` class (`frame-ancestors 'self'`, `frame-src 'self'`, `img-src 'self' data:`); `/_frame/` is already a reserved prefix.

### 4.12 `POST /csp-reports` (`Handler.CSPReports`)

Not a general logging sink: it accepts, sanitises, counts and forgets.

```
method != POST            -> 405 (Allow: POST)
Content-Type not in {application/csp-report, application/reports+json, application/json} -> 415
body = io.ReadAll(http.MaxBytesReader(64 KiB))   too large -> 413
parse: report-uri form {"csp-report": {...}}  OR  Reporting API array [{"type":"csp-violation","body":{...}}]; malformed -> 400
at most 20 reports per request are processed (the rest dropped)
for each: CSPReport{
    Directive:    the effective/violated directive's first word if it is in the closed set
                  (default-src script-src script-src-elem script-src-attr style-src style-src-elem style-src-attr img-src font-src connect-src
                   manifest-src frame-src frame-ancestors form-action base-uri object-src require-trusted-types-for trusted-types), else "other"
    Disposition:  "enforce" | "report"
    BlockedKind:  "inline" | "eval" | "data" | "blob" | "self" | "host:<host>" | "unknown"      (never a full URL)
    DocumentPath: URL path only, no query, no fragment, <= 200 chars, control chars removed
    SourceFile:   path only, same rules;   Line: >= 0 }
call Options.OnCSPReport(ctx, report) for each; respond 204 (empty body)
```

Nothing is stored and no field of the request is echoed. The route is exempt from `CrossOriginProtection` (browsers send reports without `Origin` semantics we can trust) and has its own rate-limit class (`ratelimit.csp_reports: 30/min`, burst 10, per IP) and a 64 KiB body cap set by httpx **and** enforced here (defence in depth). The wired callback increments `scp_csp_reports_total{directive,disposition}` and logs at DEBUG; a spike of `enforce` reports is the signal to step the rollout back.

### 4.13 Embedding (`web`)

```
web/embed_placeholder.go   //go:build !spa   //go:embed placeholder   var embedded embed.FS   const EmbeddedMode = ModePlaceholder   FS() = fs.Sub(embedded, "placeholder")
web/embed_spa.go           //go:build spa    //go:embed all:dist      var embedded embed.FS   const EmbeddedMode = ModeSPA           FS() = fs.Sub(embedded, "dist")
```

`go test ./...` and `go vet ./...` need no Node: the placeholder is committed (a one-paragraph `index.html` containing `<div id="root">` and a `routes.json` listing the SPA route shapes with `buildId: "placeholder"`). `go build -tags spa` **fails to compile** when `web/dist` is absent (verified: `pattern all:dist: no matching files found`), which is the intended guard against shipping the placeholder as a release. `all:dist` (not `dist`) so files beginning with `.` or `_` are embedded. The package-level `var embedded embed.FS` is exempt from `gochecknoglobals` (v0.2.2 exempts a variable whose declaration is preceded by `//go:embed`; checked in its source 2026-10-14), so G5 holds.

`cmd/scp` behaviour (section 2.5): in placeholder mode `webapp.Options.CheckBuild = false`, `RequirePrecompressed = false`, a loud WARN `"serving the placeholder web bundle; build with -tags spa"` at startup and `web.mode=placeholder` in `/api/v1/status`; in spa mode both are true and `BuildID` is the ldflags build id (the frontend build writes the same id into `routes.json`). The release smoke test asserts the served `/dashboard` body contains the Vite-built root and **not** the placeholder text. The placeholder `routes.json` is a golden: a `cmd/scp` test compares its shapes with `routes.Public()` SPA-class entries so the placeholder cannot drift from the route table (D14).

### 4.14 `Static` (the OpenAPI bundles)

`Static(contentType, body)`: strong ETag computed **once** at construction, `Cache-Control: no-cache`, `Content-Type: application/yaml` (RFC 9512 registered the type in 2024; `text/yaml` is not used), `If-None-Match` (weak comparison) -> 304, HEAD supported, any other method -> 405 problem with `Allow: GET, HEAD`. The body is the `apispec` bytes (immutable, in the binary), so no per-request work beyond header writes.

### 4.15 Contract tooling (`apitest`)

`Lint(doc)` returns every convention violation (empty means clean). The rules are the machine-checkable part of 10-architecture.md 11.4 ("a custom check fails the build if a GET operation lacks `IfNoneMatch`, a `304`, an `ETag` header or the shared `default` problem response"):

| Rule | Check |
|---|---|
| L1 | `operationId` present, unique, camelCase |
| L2 | exactly one tag per operation |
| L3 | path starts with `/api/v1/` and has no trailing slash |
| L4 | the `default` response is `$ref: #/components/responses/Problem` (content `application/problem+json`) |
| L5 | every GET: `If-None-Match` parameter `$ref`, a `200` with an `ETag` header, a `304` `$ref: NotModified`; opt-out `x-scp-conditional: false` (SSE, the OpenAPI document itself uses `Static`) |
| L6 | 2xx JSON bodies are `$ref` to a **named** schema |
| L7 | property names are camelCase |
| L8 | a paged GET (has the `Limit` parameter) declares the `Link` header on 200 and its body has `page` |
| L9 | no OpenAPI 3.0 `nullable` (use `type: [T, "null"]`) |
| L10 | string enum values are lower-case (`[a-z][a-z0-9]*([_-][a-z0-9]+)*`, kebab or snake: the kernel `freshness.State` vocabulary is snake_case, `not_found`, `too_large`) |
| L11 | `servers`, if present, only `url: /` |

L10 and L11 are looser than the first draft because the real 07 documents use snake_case state enums and `servers: [{url: /}]` (D17a, found by running the linter on `20-design/api/openapi-public.yaml` and `openapi-admin.yaml`; both are clean). `ValidateResponse(t, doc, req, rec)` checks a recorded exchange against the document: status declared, headers present and typed, body valid (gunzips first; falls back to `ValidateProblem` for a route the document does not know, e.g. the 404 path), **and no header with the prefix `Access-Control-` (GR-18)**. `ValidateProblem` checks `application/problem+json`, the schema, and `status` equal to the HTTP status. `ETagCoversBody` is the D15 property. `NoLeak(t, route, rec, needles...)` fails when a status line, header or body contains a needle (case-insensitive): the enumeration test passes the private repository's name and slug. `Normalised` replaces the per-request `instance` so two problem bodies can be compared byte for byte.

### 4.16 Edge cases the implementation MUST handle (each has a test)

1. `If-None-Match: W/"a", "b, c"` (comma inside a tag), `*`, a malformed token, multiple header lines (the multi-line case applies to `Static`, `webapp` and any hand-written handler; on a generated strict handler two `If-None-Match` lines are a 400 from the generated binding, 4.2).
2. HEAD on every API route returns the GET headers with no body; HEAD on a problem returns the problem headers with no body.
3. A handler that sets `ETag` and then returns an error: the problem has no `ETag` (`WriteProblem` deletes it).
4. `Accept: application/xml` on `/api/`: 406. `Accept: */*`, none, `application/json;q=0.1` pass.
5. A gzip-capable client and a 100-byte JSON: identity, `Vary: Accept-Encoding` present. A 5 KB JSON with a strong ETag: gzip, ETag weakened.
6. `limit=0` is "absent" in `ParsePage` (the generator gives `*int`) but `?limit=0` on the wire fails the validator (`minimum: 1`); `?limit=` (empty) passes the validator and is absent; both paths are 400-or-default consistently.
7. A cursor with trailing garbage after the JSON, with a different version byte, with `=` padding, 513 characters: all `invalid-cursor` (tested).
8. `GET //`, `GET /repos/..`, `GET /dashboard//`: **no 500 and not a 404 either**: Go 1.27.1's `ServeMux` cleans the path and answers `307` with the cleaned `Location` (`/`, `/`, `/dashboard/`) before any handler runs (the first draft claimed a real 404; `TestWiringAgainstTable`; ADR-014 only says its own earlier claim was removed). `GET /dashboard/anything` is a shell 404; `GET /repos/a%2Fb` reaches the SPA shape matcher on the escaped path (one segment, 200 shell) and the API's `LookupRepo`/validator slug check rejects it later.
9. `POST /dashboard` is 405 with `Allow: GET, HEAD`; `OPTIONS` likewise (there is no CORS preflight: nothing answers it).
10. A dist with `index.map`, `assets/app.js` (no hash), a second html file, a symlink, an orphan `.br`, a `.gz` that is larger than the original or does not decompress: each fails `New` with all problems listed.
11. A shell 404 answer to `HEAD`: same headers and `Content-Length`, no body.
12. An `If-None-Match` on the shell 404: ignored (no `ETag` sent, so no 304 for an error).
13. Concurrent `ServeHTTP` on one `Handler` and on `For(Admin)` derived handlers: no locks, no mutation (race test).
14. `Accept: text/markdown` or `text/plain` on `/repos/{repo}`, `/llms.txt`, a badge: never 406, and the handler's own `Cache-Control` survives (`APIDefaults` is `/api/`-only).
15. An asset name whose hash contains `-` or `_` (`assets/index-Ab-d1234.js`) is accepted; `assets/app.js` and `assets/my-long-name.js` are rejected; a bundle file is served under its exact name only (`/theme-init.js/` is a 404).
16. A mux 404/405 rewritten by `ProblemMux`, and every problem written by `WriteProblem`, carries `X-Content-Type-Options: nosniff`.
17. A malformed JSON body whose decoder message contains Go field names or secrets renders only the fixed detail (strict request errors and `BindingError`).
18. `/favicon.ico` is a plain 404 (a path with an extension that is not in `dist`): `index.html` must declare `<link rel="icon" href="/favicon.svg">` so browsers never need to request it.

---

## 5. Data

This unit owns **no persistent data**, no SQLite table, no snapshot slot and no job. Its state is:

| State | Where | Lifetime | Size bound |
|---|---|---|---|
| The SPA bundle (`files`, `.br`/`.gz` siblings, ETags, manifest) | `webapp.Handler` | process (built once in `New`, immutable) | `dist` <= 16 MiB total, one file <= 8 MiB (checked at start). Resident cost is the embedded bytes themselves (`go:embed` data lives in the binary's read-only segment; `bytes` slices alias it where possible) plus ETag strings |
| The OpenAPI bundles | `apispec` (embedded strings) and the two `httpapi.Static` handlers | process | the **bundled** documents (~185 KB public, ~187 KB admin today, measured on the real 07 sources of 2026-10-14 02:44 with redocly CLI 2.63.0; the sources are ~111 KB and ~20 KB, but the admin document is a superset that repeats the public operations, 07 D-1), embedded twice in the binary |
| Compiled route shapes | `webapp.Handler` (`shapeSet` per listener) | process | one slice of segments per manifest route (tens) |
| The kin-openapi router and parsed document | `validate.RequestValidator` closure | process | the parsed document (a few MB at the 1,000-repo scale; one per listener) |
| The gzip writer pool | `httpapi.Gzip` (`sync.Pool`) | process | a few `gzip.Writer` (~800 KB each at default level, pooled, reclaimable by the GC) |
| Per-request values | `authz.Viewer` (3 booleans), `Principal` in the context | request | O(1) |

No cache is added: conditional GET makes the **client** the cache; the server recomputes bodies from the immutable snapshot (the snapshot is the cache, ADR-002). Wire data shapes are in sections 3.1-3.5; the only generated artefact with data meaning is `common.yaml` (owned by 07; the copy under `internal/httpapi/openapi/` is byte-identical to `20-design/api/common.yaml`, enforced by the generate-and-diff CI step).

Cursors, ETags and CSP reports are **ephemeral wire values**: none is stored, none is signed, none is trusted (section 10).

---

## 6. Configuration

### 6.1 Keys this unit owns

| Key (YAML) | Env | Type | Default | Validation | Consumed by |
|---|---|---|---|---|---|
| `web.csp.mode` | `SCP_WEB_CSP_MODE` | enum `report_only` \| `enforce_core` \| `enforce` | `report_only` | `webapp.Config.Validate()` (path-accurate error `web.csp.mode: "x" is not one of ...`; `platform/config` collects it with all other violations) | `webapp.Options.CSP` |

`webapp.Config` is the struct the root config embeds under `web:`; `cmd/scp` copies `cfg.Web.CSP.Mode` into `Options.CSP`. The key is documented in the generated config reference with the rollout meaning (4.11). A "documented keys == consumed keys" test (GR-04, GR-06) covers it.

### 6.2 Keys this unit reads but does not own

| Key | Owner | Use |
|---|---|---|
| `access.mode` | `access` | `httpapi.DefaultsOptions.Private = (mode != public)`; selects `authz.PublicOnly` versus the access authorizer |
| `ratelimit.csp_reports` | `platform/httpx` | 30/min burst 10 per IP on `POST /csp-reports` |
| `ratelimit.api` | `platform/httpx` | 300/min burst 100 on `/api/v1` |
| `server.public.listen`, `server.admin.listen` (nested; 02 Q4, corrected by CD-CF-08) | `platform/httpx` | one `webapp.Handler`, `For(Admin)` for the second |

### 6.3 Build-time inputs (not configuration)

| Input | Where | Meaning |
|---|---|---|
| `-tags spa` | `go build` | embeds `web/dist` instead of the placeholder; flips `CheckBuild`/`RequirePrecompressed` on |
| `-X main.build=$BUILD_ID` | ldflags | `Options.BuildID`; the frontend build writes the same id into `dist/routes.json` (`buildId`) |
| `web/dist` | `pnpm -C web build` + the precompression step | includes `routes.json` (generated from `web/routes.ts`), `.br` (brotli q11) and `.gz` (gzip -9) siblings for compressible files >= 1 KiB |

Fixed constants (not configurable, on purpose): CSP strings, `Permissions-Policy`, the 64 KiB / 20-report CSP limits, the 8 MiB / 16 MiB dist limits, `MaxCursorLen = 512`, gzip `MinSize 1024`, `Cache-Control` values. There is no "enable CORS", "enable inline scripts" or "allow source maps" switch (GR-18, GR-19).

Precompression recipe (design choice, D8): a Node step in the web build walks `dist` and, for every file with an extension in the compressible set and size >= 1024 bytes, writes `<file>.br` with `zlib.brotliCompressSync(buf, {params: {[BROTLI_PARAM_QUALITY]: 11, [BROTLI_PARAM_SIZE_HINT]: buf.length}})` and `<file>.gz` with `zlib.gzipSync(buf, {level: 9})`, keeping a sibling only when it is smaller than the original, and **round-trips each sibling** (`zlib.brotliDecompressSync` / `gunzipSync` must reproduce the source bytes, otherwise the build fails; Go can verify only the `.gz`, 4.9). The same build sets Rolldown/Rollup `output.assetFileNames`, `chunkFileNames` and `entryFileNames` to `assets/[name]-[hash:8].[ext]` (hash length 8 is the contract `webapp` checks). Go never compresses static files; a missing `.br` in a release build is a startup error (`RequirePrecompressed`), so a build that forgot the step cannot ship.

---

## 7. HTTP surface

### 7.1 Routes this unit owns (names are the `routes.Table` registration keys)

| Route name | Class | Pattern | Handler |
|---|---|---|---|
| `api.openapi` | API | `GET /api/v1/openapi.yaml` | `httpapi.Static(httpapi.ContentTypeYAML, apispec.Public())` (admin mux: `apispec.Admin()`), registered through the **recording mux** (`rec.HandleFunc("GET /api/v1/openapi.yaml", h.ServeHTTP)`): `Table.Handle` panics for an API-class name (the first draft said `table.Handle`) |
| `csp.report` | server-only | `POST /csp-reports` | `webapp.Handler.CSPReports()` (`table.Handle`) |
| `assets`, `theme.init`, `favicon`, `manifest` (`frame.mermaid` in v1.1) | server-only (file) | `GET /assets/{file}`, `GET /theme-init.js`, `GET /favicon.svg`, `GET /manifest.webmanifest` | `webapp.Handler` (file branch) |
| `spa.root` | SPA | `GET /{$}` | `webapp.Handler` (302 to `RootRedirect` / `AdminRedirect`) |
| `spa.dashboard`, `spa.group.commits`, `spa.commits`, `spa.repo.commits`, `spa.repo.dependencies`, `spa.dependencies`, `spa.packages`, `spa.page`, `spa.fallback` (admin: `spa.status`, `spa.fallback`) | SPA | one `GET` pattern each (`spa.fallback` is `GET /{path...}`) | `webapp.Handler` (shell branch / 404). Every table entry needs a handler or `Verify()` fails, so all `ClassSPA` entries whose owner is `webapp` are registered; `negotiated.group`, `negotiated.repo`, `doc.md` (owner `overview`/`docs`) and `negotiated.repo.rest` get `httpapi.Negotiate(spa, markdown)` from their owners |

The SPA-class **shapes** (`Table.SPAShapes()`, minus the root `/{$}`) must equal the shapes in `dist/routes.json`; `Table.ReservedPrefixes()` is passed as `Options.Reserved`. `webapp` never reads `routes`; `cmd/scp` does the translation.

### 7.2 Behaviour of the API contract, as a client sees it

| Request | Response |
|---|---|
| `GET /api/v1/openapi.yaml` | 200 `application/yaml`, strong ETag, `Cache-Control: no-cache`; `If-None-Match` -> 304; `HEAD` ok; `POST` -> 405 problem with `Allow: GET, HEAD` |
| `GET /api/v1/anything-unknown` | 404 `application/problem+json`, `code: not-found`, `Cache-Control: no-store` |
| `DELETE /api/v1/repos` | 405 problem, `Allow: GET, HEAD` |
| `GET /api/v1/repos?limt=5` | 400 problem, `errors: [{"pointer":"#/query/limt","detail":"unknown parameter"}]` |
| `GET /api/v1/repos?limit=500` | 400 problem, `errors: [{"pointer":"#/query/limit","detail":"maximum: got 500, want 200"}]` (kin's reason, cleaned by `cleanReason`) |
| `GET /api/v1/commits?cursor=!!` | 400 problem (validator: pattern) or `invalid-cursor` (feature: undecodable) |
| `GET /api/v1/repos/%20x` | 400 problem, `#/path/repo` |
| `GET /api/v1/repos/not-here` or a private repo as anonymous | 404 `not-found` "repository not found" (identical bytes) |
| `GET` with `Accept: text/html` on `/api/v1/...` | 406 problem listing the offered types |
| `GET /api/v1/commits` with a matching `If-None-Match` | 304, no body, same `ETag`, `Cache-Control: no-cache` |
| any 2xx/304 GET under `/api/` | `Cache-Control: no-cache` (or `private, no-cache`), never `Access-Control-*` |
| cold start before the first snapshot | 503 `not-ready` with `Retry-After` (feature decision, renderer here) |

Response header contract (what a feature's generated response type must set): 200 of a GET: `ETag` (weak) always, `Link` on paged collections; 304: `ETag`. Problem: set by `WriteProblem` only. The shared `X-Request-Id` response header is set by `httpx`; `instance` must equal it.

### 7.3 SPA responses (listener-aware)

| Request (public listener) | Result |
|---|---|
| `GET /` | 302 `/dashboard` |
| `GET /dashboard`, `/repos/next.js`, `/repos/x/docs/a/b.md` (a `{path...}` shape) | 200 shell + document headers |
| `GET /dashboard/` | 308 `/dashboard` (query kept) |
| `GET /nope` or `GET /repos` (no shape) | **404 with the shell body** |
| `GET /assets/app-ab12cd34.js` | 200 file, `immutable`, `br` if accepted |
| `GET /assets/missing.js`, `GET /x.js`, `GET /x.map` | 404 plain text (`/assets/` is a reserved prefix, but a real bundle file under it is served first, 4.10.1 row 3) |
| `GET /badges/...`, `/media/...` that reached `webapp` (nothing else matched) | 404 plain text, never HTML. Under `/api/` the same 404 is rewritten by `ProblemMux` into `application/problem+json` (the chain of 2.5), so an API client never sees `text/plain` |
| `GET /status` | 404 with the shell body on the public listener (shape belongs to `admin`); 200 on the admin listener |
| `POST /dashboard` | 405 `Allow: GET, HEAD` |

On the admin listener only `/status` (and files) match; `/` redirects to `/status`; the SPA bundle is the same.

---

## 8. Concurrency and lifecycle

- **Everything is immutable after construction.** `webapp.Handler`, the validator closure, `Static` handlers, the shape sets and `authz.Viewer` have no mutex and no goroutine. `Handler.For(l)` copies the struct (it shares the file map read-only). The race test hammers one handler from many goroutines.
- **Only one pooled resource**: `Gzip`'s `sync.Pool` of `gzip.Writer`. A writer is `Reset` onto the response and returned in `finish()`, which `Gzip` defers, including on panic (the deferred call runs while httpx's recover runs further out).
- **Response-writer wrappers** (`ccWriter`, `gzWriter`, `pmWriter`) MUST implement `Unwrap() http.ResponseWriter` (so `http.NewResponseController(w)` reaches the real connection for SSE per-response deadlines, `Flush` and hijack refusal) and `Flush()`. They MUST NOT implement `http.Hijacker` or `http.Pusher`. Any new wrapper in this unit follows the same rule; a test asserts `ResponseController.Flush` works through the full chain.
- **No goroutines, no timers, no contexts stored.** Request cancellation: `StrictErrors` writes nothing when the client has gone (`context.Canceled` with a cancelled request context).
- **Startup order** (inside `cmd/scp` `run()`, after config validation and before listeners open): `web.FS()` -> `webapp.New` (fails the process on a bad dist) -> `validate.RequestValidator` for each listener's bundle (fails the process on a bad document) -> `routes.Table.Verify()` -> listeners. All four are fatal; none is retried. They run before readiness can be true.
- **Shutdown**: nothing to stop. In-flight requests drain with the server's graceful shutdown; SSE streams are the `live` unit's.
- **Hot paths allocate little**: file and shell responses do no per-request hashing, no `Stat`, no `ReadFile`; `NotModified` allocates only the token slice; ETag strings are built once per request from a few parts.

---

## 9. Observability

`httpapi`, `webapp` and `validate` cannot import OTel; they expose **hooks**, and `cmd/scp` (the only place that holds a `metric.Meter`) wires them.

| Metric | Type, labels | Source hook | Owner status |
|---|---|---|---|
| `scp_csp_reports_total` | counter `{directive, disposition}` | `webapp.Options.OnCSPReport` | **in the architecture without labels** (18.2); labels added here (D10) |
| `scp_webapp_responses_total` | counter `{kind, encoding}`; `kind` in `file shell shell404 notfound redirect csp_report`, `encoding` in `br gzip identity` | `webapp.Options.Observe` | proposed (D10): shows br adoption, stale-deploy 404s and the real-404 rate |
| `scp_api_rejected_total` | counter `{status, code}` | `validate.WithObserver` | proposed (D10): validator rejections (400/404/405/413/415); the SPA/ops can alert on a 400 spike after a deploy |

Instrument names are constants next to their creation in `cmd/scp` (G11), and the docs generator test (18.2) boots them against an in-memory reader. Existing architecture metrics this unit's behaviour feeds without owning them: `http.server.request.duration` (route pattern `r.Pattern`, bounded cardinality), `scp_ratelimit_rejected_total{class}` (httpx).

Logs (through the injected `*slog.Logger`, fields per `logx`): `StrictErrors`/`Recovered` log `err`, `pattern`, `code` at ERROR for 5xx only (4xx problems are the caller's and are not logged here; the access log has the status). The request id and trace id are attached by `logx` from the context. **Never logged**: request bodies, query strings with secrets, cursors' decoded contents, CSP-report URLs beyond the sanitised path.

Status and probes: `/api/v1/status` (ops) shows `web.mode` (`placeholder|spa`, from `web.EmbeddedMode`), `web.build_id`, `web.csp_mode`, and the validator's document title/version. A placeholder in a release build is a critical status entry (D21).

No traces are created here; the otelhttp span name is the route pattern and the `webapp` branch is visible through `kind` in the metric.

---

## 10. Security

### 10.1 Threat table (assets: the visibility boundary, the user's browser, the integrity of served bytes)

| # | Threat | Control (where) | Test |
|---|---|---|---|
| T1 | Existence oracle for private repositories (404 vs 403, timing, body difference) | `LookupRepo` returns one problem value for missing, malformed and invisible; no early return reveals which | `authz` DeepEqual + byte-equal rendered responses; enumeration test with `NoLeak` |
| T2 | Counts and aggregates leak private data | global projections are `scm.Classed` and picked with `ClassOf`; the anonymous principal reaches only the public variant | enumeration test compares anonymous responses byte for byte with a public-only deployment (A-23) |
| T3 | Open redirect through `Redirect` or a slug-redirect target | targets built by the `routes` builders (which escape per segment) or validated by `httpapi.Redirect`, must start with a single `/`, must not contain `//` or `\` at position 1; `%2F`/`%5C` in a segment re-validated against slug and group syntax | table test with crafted inputs; fuzz target proposed |
| T4 | SPA fallback serves HTML for a script/style/API path (MIME confusion, cache poisoning) | rules 3 and 7 of 4.10.1: reserved prefixes and any extension or `/assets/` that is not an exact file are plain 404; `nosniff` on every response | routing table test |
| T5 | Source maps, templates, orphan files served (GR-19) | only `web/dist` is embedded; allow-listed extensions; `.map` rejected; one HTML file; hashed assets only; symlinks rejected | `New` rejection table |
| T6 | Stale or hand-edited dist (manifest/bundle mismatch) | `routes.json` strict parse, `buildId` check in spa builds, `RequirePrecompressed`, sibling verification (decompress equals original) | bad-dist tests |
| T7 | XSS/injection in the document | static CSP without `unsafe-inline`/`unsafe-eval`, Trusted Types (`scp-html`), `base-uri 'none'`, `object-src 'none'`, `frame-ancestors 'none'` + `X-Frame-Options: DENY`; no server-generated HTML in this unit | golden CSP strings for the 3 modes; Playwright (frontend unit) |
| T8 | Wildcard CORS / credentialed cross-origin reads (GR-18) | no CORS code exists; `ValidateResponse` fails any `Access-Control-*`; `http.CrossOriginProtection` guards non-GET (httpx) | apitest, e2e |
| T9 | Error text leakage (SQL, paths, upstream bodies, stack) | handlers return `Err(Problem)`; unclassified errors become `internal` with empty detail; `safeDetail` strips control chars and caps at 200 bytes; kin messages are never rendered raw | e2e "hunter2" leak test; `NoLeak` |
| T10 | Cache poisoning / wrong variant served by a shared cache | ETag carries view class and a digest of derived freshness; `Cache-Control: private, no-cache` when identity matters; `Vary: Accept` (negotiated), `Vary: Accept-Encoding` (compressed); problems `no-store` | `ETagCoversBody`; header assertions |
| T11 | Header injection through problem or redirect values | only `Allow`, `Retry-After`, `WWW-Authenticate` copied from `Problem.Header` (set by constructors from fixed values); redirect targets validated; `Content-Length` computed | unit tests |
| T12 | Cursor tampering/forgery | cursors are untrusted input: strict decode, bound to the filter by `QueryFingerprint`, carry no authority; visibility re-applied on every page | `DecodeCursor` table + proposed fuzz |
| T13 | Request smuggling of query shape (`limit=1&limit=2`, unknown params) | `queryShape` rejects both under `/api/` | validator tests |
| T14 | CSP-report abuse (log flooding, injection, memory) | own rate class (httpx), 64 KiB cap, <= 20 reports, closed directive set, path-only URLs, control characters stripped, nothing stored or echoed | cspreport tests |
| T15 | Decompression/compression oracles (BREACH-style) | JSON bodies hold no per-request secret reflected from the request; CSRF tokens do not exist; no authenticated secrets in API bodies | design rule: no secret and attacker-controlled text in one compressed body |
| T16 | Path traversal in static serving | files come from an in-memory map keyed by cleaned relative names; no `os` file access at run time | routing test with `..`, `%2e%2e`, `//` |
| T17 | Placeholder shipped as a release | `-tags spa` fails to compile without `dist`; the release smoke asserts the real root; WARN + critical status when placeholder | CI smoke |
| T18 | MIME sniffing of an error body (a problem or a rewritten mux 404/405 without `nosniff`) | `WriteProblem` sets `X-Content-Type-Options: nosniff` itself and `ProblemMux` keeps it (the first draft deleted it, V6) | `TestWriteProblemSetsNosniff`, `TestProblemMuxRewriteKeepsNosniff` |

HSTS is **not** set here: the TLS-terminating proxy owns it; httpx sets it only when the request is HTTPS (TLS or trusted `X-Forwarded-For` proto) **and** `server.public.hsts` is configured (CD-CF-08: nested name) (10-architecture.md 11.3). Cookies: this unit sets none.

### 10.2 The visibility discipline for feature authors (binding)

1. No handler reads `repo.Visibility` directly except as `viewer.Can(repo.Visibility)` or `repo.Visibility.Visible(class)`; D19 proposes an AST test that flags any other read outside `internal/httpapi/authz`, `catalog` (which assigns it) and tests.
2. No feature caches a **per-principal** response in process memory. Per-class projections are the only per-audience caches and are keyed by `ViewClass`.
3. A 404 for "invisible" and for "missing" must be indistinguishable in status, headers (excluding `instance`), body and **work done** (no extra lookups before the visibility check that a missing repo skips).

---

## 11. Requirements and guardrail trace

### 11.1 Product features this unit carries

| Feature | Where it lands in this unit |
|---|---|
| Root redirect | `webapp` rule 2 (`/` -> `/dashboard` on the public listener, `/status` on the admin listener, 302) |
| Shared layout, navigation and header status (API half) | the `site` envelope pieces: `Freshness`, `NewFreshness`; the endpoint is `overview`'s. `ETag`/`Link`/`PageInfo` helpers used by `api.site` |
| Not-found and error pages | `webapp` rule 8 (real 404 with the shell; the SPA NotFound page) + problem+json for the API |
| Static assets, favicon, robots.txt | `webapp` file branch (`assets`, `theme-init.js`, `favicon.svg`, `manifest.webmanifest`); `robots.txt` is `overview`'s server-only route registered before the catch-all |
| Header status: updated time, repository count (API half) | `Freshness` envelope + class-aware count via `ClassOf` (the count itself is `overview`'s) |
| Visibility policy (enforcement) | `authz` (`Authorizer`, `Viewer`, `ClassOf`, `LookupRepo`) |
| Badges (URL half) | `/badges/` is a reserved prefix, so a badge URL never falls to the shell; the handler is `badges'` |
| Probes | `/livez` and `/readyz` are `ops'`; no other health, ping or echo route exists, so such a path falls to the catch-all and answers 404 with the shell body on both listeners (Q7) |
| No cross-origin API (no CORS) | nothing is implemented; `ValidateResponse` fails any `Access-Control-*`; `OPTIONS` is 405 (T8, 4.16 item 9) |
| Freshness on every item; stale-while-error with a visible indicator | the `Freshness` envelope (`NewFreshness`), the ETag digest of derived freshness (D15), the `not-ready` problem versus 200 `stale` rule |
| Admin listener separate from public | `Handler.For(Admin)` with its own manifest; `/api/v1/openapi.yaml` served from `apispec.Admin()` there; `/metrics` and `/debug/` exist only in the admin table |
| Authorisation option for non-public content | `authz` (`Principal`, `Authorizer`, `PublicOnly`, `Viewer`) with the identity middleware in unit 14 |
| Request ids and trace context | `Problem.instance = urn:uuid:<request id>` (D4), `instance` equals the `X-Request-Id` header (to write in the `cmd/scp` e2e once `httpx` exists; the prototype's chain test sets the id in the context only) |
| Frontend asset pipeline (hosting side) | `web` embed, `webapp` bundle loader, precompression contract (6.3) |
| Route table and crawl test | `httpapi.Mux` + `routes.Recorder` contract; `webapp` `Reserved` from `Table.ReservedPrefixes()`; the e2e crawl follows every `Location` and `Link: rel=next` |
| CSP and security headers | 4.10.3, 4.11, 4.12 |
| A-12: route patterns | consumed through `Table.SPAShapes()` (the `routes.json` equality, D14) and `Table.ReservedPrefixes()` (`Options.Reserved`) |
| A-16 / ADR-010: static CSP without nonce, the admin listener serves the shell | 4.11, `Handler.For(Admin)` |
| A-23: classed projections | `ClassOf`, `Viewer.Class` |

### 11.2 Design guardrails (02-design-guardrails.md, enforcement in 10-architecture.md section 20)

| Guardrail | How this unit honours it |
|---|---|
| GR-01 One composition root; GR-03 explicit dependencies and owned lifecycles | no package-level mutable state in this unit (regexes via `regexp.MustCompile` and `//go:embed` are the only package-level values, both exempt); `Handler` is a constructed value; everything is assembled in `cmd/scp` (2.5) and the startup order is fixed (section 8) |
| GR-04 Nothing exists without a consumer; GR-06 documentation checked against the code | the one key this unit owns (`web.csp.mode`) is covered by the "documented keys == consumed keys" test (6.1) |
| GR-11 Domain, wire and view types are distinct; one encoder | `Freshness`/`PageInfo` are generated types with required members (`fetchedAt`, `nextCursor`, `limit`), so a missing member is a strict-server compile error; `encoding/json` only, `WriteJSON` is the one encoder for hand-written JSON, generated DTOs otherwise |
| GR-15 One route table; links are generated, escaped and crawled | the `routes` builders are the only URL source; a `forbidigo` rule bans `"/repos/"+` outside `routes` (applies to `webapp`); routing is `net/http` `ServeMux` and the only matcher here is the manifest shape matcher, a closed grammar with a fuzz target; slugs are checked by `scm.ParseSlug` in `LookupRepo` and refs are query parameters, never raw path segments; `//`, `/dashboard/anything` and `/dashboard/` are 307/404/308 by rule (4.16 item 8), never a 500 or a wrong page |
| GR-16 HTTP responses are truthful: status, type and charset | unknown pages are real 404s (4.10.1 rule 8: shell 404 for paths that match no shape, problem 404 for the API), never a redirect to the dashboard; the problem catalogue has fixed statuses and the 503 `not-ready` versus 200 `stale` rule; the content-type `switch` of 4.9 puts `charset=utf-8` on every text type and consults no OS mime database; JSON is UTF-8 by definition |
| GR-17 The management plane is separate; nothing reflects requests | `/metrics` and `/debug/` are registered only on the admin mux; on the public listener they reach the catch-all and answer 404 (test: public mux has no `/debug`); no route echoes request headers, `routes.Verify()` rejects unknown names and `webapp`'s 404s echo nothing |
| GR-18 Browser security policy is strict by default | `webapp` sets the document header set and the static CSP (4.10.3, 4.11) and golden-tests them; httpx owns the other classes; there is no CORS code and `ValidateResponse` fails any `Access-Control-*` (T8) |
| GR-19 One reproducible asset build; only its output is served | only `web/dist` is embedded; bundle allow-list, `.map` and orphan rejection (T5); hashed `assets/*` with `immutable` (the hash rule of 4.9), everything else `no-cache` + strong ETag, `X-SCP-Build` for skew detection; `New` walks the bundle once and nothing is read per request (4.9); `favicon.svg` is a bundle file and `index.html` declares it (4.16 item 18) |
| GR-26 Freshness is data; GR-40 live updates show staleness | `Freshness` envelope and the D15 ETag digest, so a client polling with `If-None-Match` sees `stale` the moment it happens |
| GR-28 The read model is an immutable snapshot | this unit adds no per-request scans and no per-request file reads; conditional GET makes the client the cache (section 5) |
| GR-32 Trusted HTML is a type minted only by the sanitiser (defence in depth) | `docs`/`safehtml` sanitise at the source; here the static CSP (no inline script, Trusted Types `scp-html`) and `sandbox` on every non-document class limit the blast radius of any sanitiser bug (T7) |
| GR-35 Access is an explicit policy | `authz` (`LookupRepo`, `Viewer`, `ClassOf`), 4.8, T1/T2: a private repository and a missing one produce identical bytes |

---

## 12. Test plan

### 12.1 What was compile-checked and executed for this design

A prototype module (`20-design/06-httpapi.assets/`, module `github.com/cantara/source-code-portal`, Go 1.27.1 darwin/arm64) contains the real implementations of every package in this unit; `scm`, `freshness` and `routes` are **copies of unit 01's real packages** (`20-design/01-kernel.assets/`, test files dropped), `readmodel` and `logx` are stubs with the exact architecture signatures. On 2026-10-14:

- `go vet ./...` clean; `gofmt -l` clean; `go test -race -count=3 ./...` green: packages `internal/e2e`, `httpapi`, `httpapi/apitest`, `httpapi/authz`, `httpapi/validate`, `webapp`, `web`; **46 test functions** (38 of the first draft plus 8 regression tests added by the verification; package `internal/wiring` included); statement coverage `httpapi` 78 %, `apitest` 69 %, `authz` 90 %, `validate` 87 %, `webapp` 90 %, `web` 100 %. The `scm` copy was refreshed from unit 01's current `repo.go`/`membership.go` (it had drifted by `KeyParts` and the by-ID index; `freshness` is byte-identical to unit 01). The `authz` tests use the `readmodel` stub (`readmodel.Get(s, EntryID, slot)`); the real package must keep that call shape.
- **Seam tests against unit 01** (`internal/wiring`): the placeholder `routes.json` equals `routes.Public().SPAShapes()` minus the root `/{$}` (D14); `routes.Public().ReservedPrefixes()` passed as `Options.Reserved` yields real 404s under `/api/`, `/badges/`, `/media/`, `/webhooks/` and serves the bundle's own `/assets/` files (the computed list is `/.well-known/ /_frame/ /api/ /assets/ /badges/ /media/ /schemas/ /webhooks/`); every `freshness.Status.String()` is a member of the generated `FreshnessStatus` enum (D6 is already satisfied by the kernel). `scm.ParseSlug` is the slug grammar used by `LookupRepo`.
- oapi-codegen v2.8.0 was run against the real `20-design/api/common.yaml` (copied into `internal/httpapi/openapi/`) and against a sample feature fragment (`commitsapi`) with `import-mapping` to the common package; the generated `StrictServerInterface` shapes (`ListCommits200JSONResponse{Body, Headers{ETag *string, Link *string}}`, `ListCommits304Response{Headers: openapi.NotModifiedResponseHeaders{ETag *string}}`) are what section 4.4 shows. `Freshness.FetchedAt`/`AsOf` generate as `*time.Time` (`NullableTimestamp = time.Time`).
- The full chain (`ProblemMux -> RequestValidator -> APIDefaults -> Gzip -> ServeMux -> generated strict handler`) is exercised end to end (`internal/e2e/chain_test.go`): 200 + gzip + `Link` + weak ETag, 304 with no body, HEAD, 404/500/400 problems with no leak, unknown route 404, 405 with `Allow`; every response is validated against the bundled spec by `apitest.ValidateResponse`.
- `go run ./cmd/apilint` (the prototype's CLI over `apitest.Lint`) reports **0 violations** on the real `20-design/api/openapi-public.yaml` and `openapi-admin.yaml`.
- `go vet -tags spa ./web` compiles with a `web/dist` present and fails with `pattern all:dist: no matching files found` without one.

**Checked by the verification (2026-10-14), closing the first draft's "not checked" list in part**: the real 07 documents bundled with redocly CLI 2.63.0 load, validate and lint clean through `RequestValidator`/`apitest.Lint` (public and admin), and 60+ probe requests against them behave as 4.7 says; the wiring of 2.5 against the real `Table.Mux`/`Handle`/`Verify` and a real `ServeMux` (`TestWiringAgainstTable`); `kin-openapi` v0.149.0 passes the same tests (fallback pin). Still **not checked**: real `platform/httpx`, a browser (CSP violations, `Reporting-Endpoints`), Schemathesis, `oapi-codegen` against more than one real fragment (07 owns that). Those are in the milestones below.

### 12.2 Tests by package (items marked **to write** or **proposed** are NOT in the prototype; everything else is, and the prototype tests are a floor the real tests must meet or exceed)

`httpapi`: problem (every constructor: status, code, title, headers; `WriteProblem` drops `ETag`, clamps status, no body for HEAD, `instance` forms; `StrictErrors` mapping incl. 413 and cancel-silent and no leak); cursor (round trip; unknown version, trailing data, unknown field, `=` padding, non-JSON, version-only, 513 characters, empty; `ParsePage` defaults and both failure codes; `QueryFingerprint` order independence); etag (`WeakETag` escaping, `NotModified` table including commas inside tags, `*`, weak/strong mix, malformed tokens, POST false; multiple `If-None-Match` lines **to write**); static (strong ETag, 304, 405 with `Allow`, HEAD); negotiate (markdown only when strictly higher, tie -> HTML, `*/*` -> HTML, `Vary`, `Link` headers); gzip (below MinSize, above, existing `Content-Encoding`, HEAD, `q=0`, SSE, 304, images, ETag weakening, Range not compressed, `ResponseController.Flush` through `Gzip` and `APIDefaults`); defaults (406 table, `Cache-Control` rules, `Private`); redirect (open-redirect rejects); `NextLink`. **Proposed fuzz targets (M7)**: `FuzzDecodeCursor` (never panics, only `ErrInvalidCursor`), `FuzzNotModified`/`FuzzSplitETags`, `FuzzParseAccept`, `FuzzShapeMatch`, `FuzzRedirect` (a `Location` produced by `httpapi.Redirect` is empty or starts with exactly one `/`), `FuzzParseManifest`.

`authz`: an `Authorizer` that denies everything still shows public; `ClassOf` for public-only and allow-all; zero principal is anonymous; `LookupRepo` equality of missing, private and `..` (`reflect.DeepEqual`), a subject-based authorizer seeing or not seeing a private repository. **To write**: the full `Viewer` truth table (internal-only grants map to `ViewPublic`, D18), slug syntax rejects of 101 characters and `/`, byte equality of two rendered 404 responses.

`validate`: a 16-case table (HEAD as GET, limit too big, limit not a number, bad cursor charset, unknown parameter, repeated scalar, bad slug path, wrong method with `Allow`, unknown path, content-type 415, body schema failure, missing required body member); a separate test for multi-error output (`#/query/limit` and `#/query/zzz` together) and for the body pointer; observer called with `(400, validation-failed)` and `(413, payload-too-large)`; 413 through `http.MaxBytesReader`; `ProblemMux` alone (text/plain 404/405 rewritten, `Allow` kept, handler-bug `http.NotFound` caught); construction error for a broken document.

`apitest`: `Lint` fires every rule on a deliberately bad document and is silent on the good one; `ValidateProblem` accepts every constructor's output; `ETagCoversBody` detects a status flip; `ValidateResponse` rejects `Access-Control-Allow-Origin` (GR-18).

`webapp`: routing table (rows of 4.10.1, both listeners); caching and encodings (br > gzip > identity, `q=0`/`*;q=0`, per-encoding ETags, immutable vs no-cache, 304, a `Range` request answered 206 by `ServeContent`); CSP golden strings for all three modes and the exact header set of the shell; rejection of a bad dist (source map, unhashed asset, unknown extension, second html, bad gzip, orphan sibling, missing or bad `routes.json`, bad pattern, stale build, symlink, a file under `api/`, missing `.br` under `RequirePrecompressed`, empty or incomplete `Reserved`); CSP reports (formats, limits, sanitising, status codes); a 64-goroutine race test over both listeners. **To write**: a size-limit test (file > 8 MiB, dist > 16 MiB), a `HEAD` shell-404 test, a test that every response carries `X-SCP-Build`.

`wiring`: placeholder manifest equals `SPAShapes()`, reserved prefixes, status enum (12.1). `web`: the embedded bundle loads through `web.FS()` and passes `webapp.New` (placeholder `routes.json`, `RequirePrecompressed` only in a spa build).

`e2e` (in `cmd/scp` tests, **to write**, using the real tables and bundled documents): (a) the **anonymous enumeration** test: every route in `routes.Public()` against a fixture with a private repository, `NoLeak` for name/slug/content and the byte-identical-counts comparison (17.3); (b) **API vs table**: bundled OpenAPI paths equal `routes` API class both directions; (c) **SPA shapes vs table**: `dist/routes.json` equals the SPA-class entries (and the placeholder golden); (d) **crawl**: follow every `Location` and `Link: rel="next"` and assert 2xx/3xx/real-404 as designed; (e) every response of the above passes `ValidateResponse`; (f) `OPTIONS` anywhere is 405 and never carries `Access-Control-*`.

**Regression tests added by the verification** (the webapp, validate and APIDefaults/nosniff ones fail on the first draft's code, which was run to prove it; `TestWithRequestAndHelpers` cannot compile there; `TestWiringAgainstTable` pins facts about unit 01's table and `ServeMux` that the first draft got wrong in prose): `TestAPIDefaultsOnlyUnderAPI` (V2), `TestStrictAndBindingErrorsDoNotRenderLibraryText` (V10), `TestWithRequestAndHelpers` (V3), `TestWriteProblemSetsNosniff` (V6), `TestProblemMuxRewriteKeepsNosniff` (V6; the first draft's assertion compared `X-Content-Type-Options` with `"text/plain"` and could never fail, now `!= "nosniff"`), `TestHashedAssetNamesFromViteAreAccepted` (V1), `TestFileServedUnderExactNameOnly` (V30), `TestWiringAgainstTable` (V4/V5/V7: `Table.Handle` panics for an API route, `Verify()` names every unregistered SPA entry, `ServeMux` answers `//` with 307 and a POST to an SPA path with 405 `Allow: GET, HEAD`). **Still to write** (found missing, none exists in the prototype): `Viewer` truth table and byte-equal 404s (listed above), a size-limit test, `HEAD` shell 404 (listed above), and an `If-None-Match` test through a **generated** strict handler with two header lines (expects 400, 4.2).

Property tests: `ETagCoversBody` for **every** conditional endpoint (each feature's contract test calls it with a mutation that flips a derived freshness status; the check is mandatory in the feature design template). Archtests: kin-openapi only in `validate`/`apitest`; `validate` imported by `cmd/scp` only; `webapp` imports nothing internal; no OTel in `httpapi/webapp/validate`; `web` imported by `cmd/scp` only; **proposed** visibility AST scan (D19).

### 12.3 Verification log (web and source checks, all 2026-10-14)

| Fact | Source | Used for |
|---|---|---|
| `application/yaml` is registered by RFC 9512 (2024) | RFC 9512 | `ContentTypeYAML` instead of `text/yaml` |
| `Link` relative references and `rel="next"` | RFC 8288 | `NextLink` |
| weak comparison for `If-None-Match` | RFC 9110 13.1.2, 8.8.1 | `NotModified`, gzip ETag weakening |
| problem details `type`, `instance` as URI references (`urn:` allowed) | RFC 9457 | `urn:scp:problem:*`, `urn:uuid:` |
| CSP `report-to` + `Reporting-Endpoints` is Baseline 2026; MDN's advice to add `report-uri` is for browsers without it | MDN, CSP `report-to` page (fetched today) | no `report-uri` in the golden string |
| oapi-codegen v2.8.0 generates `Mux` as `HandleFunc` + `http.Handler`, strict servers, header structs with pointer members for optional headers | generated output (this session) | `httpapi.Mux`, section 4.4 shapes, Q3 |
| `nethttp-middleware` v1.2.0: no 405 in `WithOpts` mode, no HEAD mapping, rewrites `r.URL` | its source | D2 |
| kin-openapi v0.150.0: `MultiError` reports the first parameter error; body errors are jsonschema dumps; repeated scalar parameters accepted | executed in the prototype | `queryShape`, `mapError`, D13/D13a |
| kin-openapi v0.150.0 is a real release dated 2026-10-13 (one day old at design time; v0.149.0 of 2026-08-28 passes the same tests); oapi-codegen v2.8.0 released 2026-07-17; redocly CLI 2.63.0 bundles the real 07 documents deterministically | GitHub releases pages, executed | pins (Q13), 4.7 |
| `Reporting-Endpoints` accepts relative URLs but needs a potentially trustworthy origin; `http.CrossOriginProtection` is Go 1.25+ and always allows GET/HEAD/OPTIONS | w3c.github.io/reporting section 3.3, pkg.go.dev/net/http | 4.11, 2.5 |
| `ServeMux` answers `//`, `/repos/..` and `/dashboard//` with 307 to the cleaned path; unknown-method requests to a `GET` pattern with 405 + `Allow: GET, HEAD` | executed on Go 1.27.1 | 4.16 item 8, 4.10.1 row 1 |
| `gochecknoglobals` v0.2.2 exempts a variable preceded by `//go:embed ` | its `check_no_globals.go` (line 123) | `web/embed_*.go` under G5 |
| `go build -tags spa` without `web/dist` fails | executed | 4.13 |

### 12.4 CI jobs this unit adds or feeds

`go test -race ./...` (placeholder embed; no Node); **generate-and-diff**: `go generate ./internal/httpapi/openapi/...` leaves `git diff` empty, and `internal/httpapi/openapi/common.yaml` equals `api/common.yaml`'s source of truth (07); **spec lint**: `apitest.Lint` over both bundled documents via a test in `internal/apispec`, and `redocly bundle` (pin `@redocly/cli` 2.63.0, output verified deterministic) leaves `internal/apispec/*.bundled.yaml` unchanged; **dist verify** (after `pnpm build`, before `go build -tags spa`): every `assets/*` stem is `<name>-<8 base64url chars>`, no `.map`, `routes.json` buildId equals the build id, every `.br`/`.gz` round-trips; **release smoke** on the built image: `/dashboard` contains the real root, the placeholder text is absent, `X-SCP-Build` equals the build id, a `.br` asset is served with `Content-Encoding: br`, `/x.map` is 404, `/nope` is 404 with an HTML body, `Access-Control-*` absent everywhere; **Playwright** (frontend unit): zero CSP violations on every route in the chosen `web.csp.mode`; Schemathesis against the running binary (07).

---

## 13. Implementation tasks (ordered; each task ends with its tests green and `go vet` clean)

**M0 - Skeleton and generation (no behaviour)**
1. `web/`: `web.go`, `embed_placeholder.go`, `embed_spa.go`, `placeholder/index.html`, `placeholder/routes.json`, `web_test.go`. Exit: `go test ./web`; `-tags spa` fails without `dist` and compiles with it.
2. `internal/httpapi/openapi/`: copy `api/common.yaml`, `oapi.yaml` (models, std-http-server, strict-server; `compatibility.always-prefix-enum-values: true`; `output-options.skip-prune: true`), `gen.go` with the `go:generate` line pinned to `@v2.8.0`. Exit: `go build ./...`, generate-and-diff job.
3. `internal/apispec/`: `embed.go` with the two `//go:embed` strings; until 07 delivers the bundles, a minimal valid bundle committed by 07's task. Exit: `Public()`/`Admin()` non-empty and loadable by `apitest.Load`.

**M1 - `httpapi` core** (depends on `logx.RequestID`, kernel types)
4. `problem.go` (+ tests): constants, constructors, `InstanceURN`, `WriteProblem`, `ProblemError`, `StrictErrors`, `BindingError`, hooks. Exit: the unit tests of 12.2 `problem`.
5. `cursor.go`, `etag.go`, `envelope.go` (`FreshnessView`, `NewFreshness`, `NewPageInfo`, `NextLink`), `json.go`, `redirect.go`, `static.go`, `mux.go`, `request.go` (`WithRequest`, `RequestFrom`, `Deref`, `Ptr`). Exit: matching tests; `NotModified` table complete.
6. **Gate to the kernel unit**: `freshness.Status.String()` exists (D6). Exit: `NewFreshness(FreshnessView{Status: st.String()...})` used in the e2e chain.

**M2 - Negotiation, compression, defaults**
7. `accept.go`, `negotiate.go`, `defaults.go`, `gzip.go` (+ tests). Exit: gzip tests incl. `Flush` and `ResponseController`; `APIDefaults` table.

**M3 - Authorisation** (depends on `readmodel.Get`, `scm.ViewClass`)
8. `authz/principal.go`, `class.go`, `lookup.go` (+ tests). Exit: indistinguishability test.

**M4 - Validation and contract tooling** (depends on 07's bundled documents)
9. `validate/validator.go`, `problemmux.go` (+ tests with the real bundled public document). Exit: every 07 operation has at least one valid and one invalid request in the table; `HEAD` mapping; observer.
10. `apitest/spec.go` (`Lint` L1-L11), `assert.go`. Exit: lint clean on both real documents; `ValidateResponse` rejects `Access-Control-*`.
11. `internal/e2e` chain test with one generated feature (copy the prototype's). Exit: the chain table of 12.1.

**M5 - SPA hosting** (the frontend build must pin `assets/[name]-[hash:8].[ext]` and round-trip every `.br`/`.gz`, 6.3)
12. `webapp/options.go`, `routesjson.go`, `bundle.go` (+ rejection tests). Exit: all-problems-listed behaviour.
13. `webapp/handler.go`, `csp.go` (+ routing, caching, golden CSP tests). Exit: every row of 4.10.1.
14. `webapp/cspreport.go` (+ tests). Exit: 204/4xx table, formats, limits, sanitising.

**M6 - Wiring and cross-checks (in `cmd/scp`; depends on `routes`, `platform/httpx`, `access`, 07)**
15. Wire the chain of 2.5 for both listeners; `rec.HandleFunc` (recording mux, **not** `table.Handle`) for `api.openapi`; `table.Handle` for `csp.report`, **every** `ClassSPA` entry owned by `webapp`, and the file routes (`assets`, `theme.init`, `favicon`, `manifest`); `Negotiate` for the entries owned by `overview`/`docs`; `table.Verify()` must pass with no `Missing`; pass the httpx hooks (including the re-panic and header-sent contract); pass `Options` (`Reserved: table.ReservedPrefixes()`, `RootRedirect`/`AdminRedirect` from `routes.Dashboard()` / the admin status path); `CheckBuild`/`RequirePrecompressed` only in spa mode; `OnCSPReport`/`Observe`/`WithObserver` to the three metrics.
16. Cross-checks (14.2): `SPAShapes()` vs `dist/routes.json`; bundled OpenAPI paths vs the API class; archtests of 12.2; the enumeration test.
17. CI: generate-and-diff, spec lint, release smoke, placeholder-warning status entry.

**M7 - Hardening (before v1 freeze)**: fuzz targets (cursor, ETag tokenizer, Accept, shape matcher, redirect targets, manifest parser), the visibility AST scan (D19), the `web.csp.mode` rollout runbook (report-only for one release with Playwright and report counters at zero, then `enforce_core`, then `enforce`).

---

## 14. Deviations proposed

### 14.1 From the architecture and decisions (each with evidence)

| ID | Deviation | Evidence | Touches |
|---|---|---|---|
| D1 | `validate.RequestValidator` returns `(func(http.Handler) http.Handler, error)` instead of a bare function | the document can be invalid; a panic in a library constructor violates "constructors return errors"; startup must list the cause | 8.14 signature |
| D2 | `nethttp-middleware` is **not** used; the validator is ~150 lines of code on kin-openapi + gorillamux. The matrix entry for `nethttp-middleware` on `validate` is dropped | `main` source re-read by the verification: `ErrorHandlerWithOpts` mode answers every route error 404 (no 405), the default path returns 405 without `Allow`, there is no HEAD-as-GET mapping, `Prefix` rewrites `r.URL` only temporarily (the first draft said it mutates it: wrong); it also rejects no undeclared query parameter and renders nothing from typed errors. The prototype supplies 405+`Allow`, HEAD, unknown/repeated query rejection and typed mapping with tests | 5.2 matrix, 11.4 pipeline text, ADR-020 |
| D3 | `Problem` gains `Header http.Header` (`json:"-"`) so `Allow`/`Retry-After`/`WWW-Authenticate` are part of the value | 405 and 429/503 problems need headers and the generated default response cannot carry them (07 `Problem` response declares none) | 8.14 `Problem` |
| D4 | The request id **is a UUID** (UUIDv4, generated by httpx); `InstanceURN` falls back to `urn:scp:request:<id>` for any other safe id | `crypto/rand.Text` is base32, not a UUID, so `urn:uuid:<id>` would be an invalid URN; 07's `X-Request-Id` header is declared `format: uuid` | 11.1 request id, 8.14 |
| D5 | The reserved extension rule (`.md`, `.json`, ...) applies **after** the SPA shape match, not before | slugs may contain dots (`next.js`, `socket.io`); `/repos/next.js` is a legal SPA route; server-only routes with those extensions are registered before the catch-all | 11.2 last paragraph |
| D6 | `httpapi` takes the freshness status as a **string** (`FreshnessView.Status`), the value of `freshness.Status.String()`; it cannot import `freshness` | rank/matrix: `httpapi` imports `routes`, `logx`, `openapi` only. Unit 01 already provides `Status.String()` returning `fresh|stale|error|unknown`, equal to the generated enum (seam test) | 8.14 |
| D7 | Dynamic compression is an own stdlib middleware (`httpapi.Gzip`), not `klauspost/compress/gzhttp` | the matrix gives `httpapi` no third-party dependency and `httpx` only `x/time/rate`+otelhttp; gzhttp appears in no row | 11.3 |
| D8 | No runtime brotli fallback (`andybalholm/brotli`); precompression is a build step and `RequirePrecompressed` fails release startup | matrix lists the brotli fallback as "only if precompression is unavailable"; Node's `zlib.brotliCompressSync` is always available in the web build | 5.2 `webapp` row |
| D9 | `web.csp.mode` has three values (`report_only`, `enforce_core`, `enforce`) | 11.3 describes a three-step rollout but 16 shows only `report_only` | 16 config outline |
| D10 | `scp_csp_reports_total` gets labels `{directive, disposition}`; two metrics are added (`scp_webapp_responses_total`, `scp_api_rejected_total`) | needed to run the rollout and to see stale-deploy 404s and validator rejections; no metric of those exists | 18.2 |
| D13 | Undeclared and repeated scalar query parameters under `/api/` are `400 validation-failed` | kin accepts both silently; a typo (`?limt=5`) would return an unfiltered list; the OpenAPI conventions say filters are explicit | 11.4 |
| D13a | A body schema failure is one fixed `FieldError` `#/body` "body does not match the schema" | kin's body errors are multi-line jsonschema dumps with no stable pointer; v1 has one tiny body (`admin retry`) | 11.1 |
| D14 | The placeholder `routes.json` is a committed golden; a test compares it with `routes.Public().SPAShapes()` minus `/{$}` (and the real `dist/routes.json` the same way in the release job) | prevents the placeholder drifting from the route table; the seam test passes today | 11.3 |
| D15 | ETag MUST include a `Digest` of every derived freshness status in the body; `ETagCoversBody` is a mandatory property test | `Derive(now)` changes status with time and no new snapshot, so a version-only ETag serves a wrong 304 | 11.4 "Conditional GET" |
| D16 | Handlers return problems as **errors** (`httpapi.Err`) through the strict server's `ResponseErrorHandlerFunc` | the generated typed default response cannot carry `Allow`/`Retry-After`; one rendering path is simpler to test | 11.4 |
| D17 | `Cache-Control: no-cache` for 2xx/304 GET/HEAD is added by one middleware (`APIDefaults`), not declared per operation | removes a per-handler way to forget it; the contract (`ValidateResponse`) still checks it | 11.4 |
| D17a | Lint rules L10/L11 are looser than the first draft (snake_case enums allowed; `servers: [{url: /}]` allowed) | the real 07 documents use `not_found`, `too_large`, `rate_limit` etc. and `servers: /`; 0 violations after the change | 11.4 custom check |
| D18 | `Viewer.Class` is `ViewAll` only when both `internal` and `private` are visible | A-23 defines two classes; fail closed rather than leak private counts. Consequence in Q1 | 8.14, 17.3 |
| D19 | A visibility AST scan test (flag reads of `.Visibility` outside the allow-list) is proposed | `LookupRepo` and `Viewer` are conventions otherwise; the architecture relies on the enumeration test only | 17.3, 20 |
| D20 | httpx contract: `Recovered`, `RateLimitedFunc`, `DenyCrossOrigin` are supplied by `httpapi`; httpx sets the baseline for non-document classes; `webapp` sets every header it owns on its own responses; HSTS stays httpx's | httpx (rank 2) cannot import `httpapi` problems; two writers of one header would disagree | 11.1, 11.3 |
| D21 | A placeholder build logs a WARN at start, shows `web.mode` in `/api/v1/status`, and the release smoke asserts the real root | a placeholder shipped as a release would be a green build that serves a stub | 11.3 |
| D22 | `theme-init.js` is `no-cache` + ETag (not immutable) | its URL is unhashed (it is referenced from `index.html`); changing it must reach clients (Q5 asks for a hashed alternative) | 11.3 |
| D23 | Requests without `Accept` or with `*/*` get HTML; 406 exists only under `/api/` | 11.2 forbids gating the fallback on `Accept: text/html` (curl, unfurlers) | 11.2 |
| D24 | `FieldError.pointer` is the URI-fragment form (`#/query/limit`, `#/path/repo`, `#/header/...`, `#/body`) | 07's `FieldError` schema documents exactly this; the architecture only says "JSON-Pointer `errors[]`" | 11.1, 11.4 |
| D25 | `NewFreshness` takes a `FreshnessView` struct (status string, kind, four times, last error code) | 07's `Freshness` has `kind` and `checkedAt` (as of 02:34 on 2026-10-14 all seven members are required and the four timestamps are required-nullable); positional arguments would not scale | 8.14 |
| D26 | Validator details are the cleaned last line of kin-openapi's message, never the raw dump | kin v0.150.0 messages contain a placeholder schema URL and newlines (executed) | 11.1 "no error text" |
| D27 | A bundle file is served **before** the reserved-prefix check, and `New` refuses any file under `/api/` | `routes.Table.ReservedPrefixes()` returns `/assets/` (a non-SPA first segment), so the reserved-first order of the first draft would 404 every asset; the seam test found it | 11.2 "Reserved prefixes" |
| D28 | `WeakETagOf(kind, ...)` with kinds `g`, `r`, `v`, and a `.<digest>` suffix only while an item is not fresh (`FreshnessDigest`, `WithDigest`) | the ETag contract of 07's `common.yaml` (snapshot `g`, per-repository `r`, snapshot endpoint `v`; digest suffix while not fresh) is more precise than the architecture's `W/"g<version>-<variant>"`; the first draft always appended a digest | 11.4 |
| D29 | `httpapi` gains `WithRequest`/`RequestFrom` (the request in the context) and the generic helpers `Deref`/`Ptr`; `cmd/scp` mounts `WithRequest` innermost | an oapi-codegen strict handler is `Method(ctx, requestObject)` and has no `*http.Request` (read in the generated `commits.gen.go`), so the architecture's `NotModified(r, etag)` and the design's `NextLink(r, ...)` recipes could not be written; the first prototype worked around it by faking a request from the `If-None-Match` parameter and hand-building the `Link`. A per-feature `StrictMiddlewareFunc` would repeat five lines in every feature, and the generated middleware type is per package so `httpapi` cannot offer one | 8.14 |
| D30 | Binding and decode error details are fixed text; the generated error types are never type-switched | the generated `InvalidParamFormatError`, `TooManyValuesForParamError`, ... are declared once per generated package, so `httpapi` cannot name them, and their messages carry Go field names and client input (T9) | 11.4 "Errors" |
| D31 | Static-file ETags are a content hash (`"<hash>"`, `-br`, `-gz`), not "derived from the build id" | architecture 11.3 and ADR-010 say "ETag derived from the build id"; a content hash gives the same revalidation and survives a rebuild that changes nothing, and `webapp` needs no build-id knowledge beyond `X-SCP-Build` | 11.3, ADR-010 |
| D32 | `Reporting-Endpoints` only; no `report-uri` | MDN lists `report-to` as Baseline 2026 (newly available) and still shows both directives for older browsers; a deployment that needs the `report-uri` fallback adds it in the proxy; the endpoint accepts both formats anyway | 11.3 |

### 14.2 What other units must supply or change (cross-unit requirements)

| To | Requirement |
|---|---|
| kernel `freshness`, `scm` | nothing to add: `Status.String()` (D6) and `scm.ParseSlug` exist; slug case is aligned (Q12) |
| `routes` (unit 01) | (a) keep `Table.SPAShapes()` and `ReservedPrefixes()` as they are (this unit consumes both) and document that `SPAShapes()` contains the root `/{$}`, which is not a manifest shape; (b) `spa.root` is served by `webapp` |
| `platform/httpx` | UUIDv4 request id in `logx.WithRequestID` and `X-Request-Id`; accept the three hooks of 2.5; own the baseline headers for non-document classes and HSTS; apply `ratelimit.csp_reports` and the 64 KiB cap to `/csp-reports`; no CORS |
| 07 openapi-contract | (a) mark the `ETag` header `required: true` on every 200 (and the `NotModified` 304) so generated header structs hold a value, not a pointer (Q3); (b) declare `Allow`, `Retry-After`, `X-Request-Id` on the shared `Problem` response (Q4); (c) keep `common.yaml` free of other changes without telling 06 (the `openapi` package is generated from it); (d) decide ownership of the lint (Q2) |
| `routes` (unit 01), 07 | **Unresolved cross-unit inconsistency found by the verification**: 07 D-1 says the admin listener serves the 22 public operations plus 2 (the admin document is a superset, 187 KB bundled), but `routes.Admin()` registers only `api.status`, `api.admin.retry` and `api.openapi` as API routes. With the table as it is, the admin mux 404s `/api/v1/site` and `/api/v1/snapshot` (which the admin SPA's `/status` page needs per 07) and the test "bundled OpenAPI paths equal the `routes` API class" cannot pass for the admin listener. One of the two must change; this unit's wiring (`MountAll` on both listeners, `Verify()`) works unchanged either way |
| 07 | `RetryJobRequest` accepts undeclared members (set `additionalProperties: false`), and the `If-None-Match` header is declared once per operation: keep it that way so the generated binding stays single-valued |
| frontend build | `assetFileNames`/`chunkFileNames`/`entryFileNames` = `assets/[name]-[hash:8].[ext]`; round-trip every `.br`/`.gz` sibling in the build; `index.html` declares `<link rel="icon" href="/favicon.svg">` |
| `platform/httpx` (addition) | recover: re-panic `http.ErrAbortHandler`, call `Recovered` only if no header has been sent (2.5) |
| `access` | provide the `Authorizer` and call `authz.WithPrincipal` in its middleware; document how `internal` is treated (Q1) |
| each API feature | `Mount(httpapi.Mux)`; return problems with `httpapi.Err`; ETag recipe of 4.4; `LookupRepo`/`Viewer`/`ClassOf` discipline of 4.8; cursor struct with `QueryFingerprint`; call `ETagCoversBody` in its contract test |
| `cmd/scp` | the wiring of 2.5 and 13-M6; archtests; the three equality tests; fatal startup on `webapp.New`/`RequestValidator` errors |
| frontend build | `dist/routes.json` (version 1) generated from `web/routes.ts`; `.br`/`.gz` siblings; hashed `assets/*`; no `.map`; `buildId` equal to the ldflags build id |

---

## 15. Open questions

| # | Question | Default taken here |
|---|---|---|
| Q1 | `ViewClass` has two values, so a principal who may see `internal` but not `private` gets the public variant of global projections and cannot see internal repositories in counts/graphs. Does v1 need a third class (`ViewInternal`), or is `access` mode defined so that `internal` and `private` are always granted together? | two classes, fail closed (D18) |
| Q2 | Who owns the OpenAPI lint: `apitest.Lint` here, or 07's `apicheck` (07 mentions a build check in its section 13)? | `apitest.Lint` is a library function; 07 may call it from `apicheck` so there is one implementation |
| Q3 | Should 07 mark `ETag`/`Link` headers `required: true` where the rule is "always present"? Today the generated header members are `*string`, so a handler can compile while forgetting the ETag (only the contract test catches it) | contract test (`ValidateResponse`) is the guard until 07 changes |
| Q4 | Should 07's shared `Problem` response declare `Allow`, `Retry-After` and `X-Request-Id` headers so the document is complete and the typed default response could carry them? | the error path (D16) carries them regardless |
| Q5 | `theme-init.js` costs a revalidation round trip per page view. Hash it and reference the hashed name from `index.html` (immutable), or keep the stable name? | stable name, `no-cache` + ETag (D22) |
| Q6 | The kin-openapi body error detail is fixed text (D13a). Is a generic "body does not match the schema" acceptable for the first admin operation, or should the admin retry body be validated by hand for field-level pointers? | fixed text |
| Q7 | `/metrics`, `/debug/` and any other ops-looking path on the **public** listener answer 404 with the shell body (they are not reserved prefixes; a trailing-slash-less path cannot be a prefix). Should the shell body be replaced by plain 404 for a closed list of "ops" paths to avoid confusing a scanner? | shell 404 (harmless, tested as 404) |
| Q8 | Cursors are unsigned. Acceptable (threat T12: they carry no authority and visibility is re-applied)? A signed cursor would need a server secret and a rotation story | unsigned |
| Q9 | (resolved by 07 on 2026-10-14) `Freshness.asOf` is now required-nullable like the other timestamps, so the helper emits `null` for a zero time and `kind` is a required string; the prototype and `NewFreshness` were regenerated and fixed accordingly | `null` when zero |
| Q10 | Brotli level 11 at build time adds seconds to the web build; accept, or use level 9 in `dev`/CI and 11 only in the release job? | 11 in release only |
| Q11 | The `Reserved` list is computed by `cmd/scp` from `routes`; should `webapp` also refuse `/.git/` and `/.env` explicitly (they have an extension-less dot form and would currently get the shell 404)? | not special-cased: a shell 404 reveals nothing |
| Q12 | **Slug case.** (resolved by 07 on 2026-10-14: `common.yaml` `Slug` is now the lower-case grammar `^([a-z0-9_-][a-z0-9._-]{0,99}|\.[a-z0-9_-][a-z0-9._-]{0,98})$`, slightly stricter than `scm.ParseSlug` (the pattern rejects `..x`, which `ParseSlug` accepts); an upper-case slug is now a `400` from the validator, and `LookupRepo` remains the 404 authority for lower-case misses). Residual (for 01/07, not this unit): 07's `GroupId` `^[a-z0-9]+(-[a-z0-9]+)*$` rejects a trailing or doubled `-` that `scm.ParseGroupID` accepts (`a--b`, `a-`), so such a group id would exist in the catalogue and be a 400 in the API | kernel (lower-case), 07 aligned |
| Q13 | kin-openapi v0.150.0 was released on 2026-10-13, one day before this design. Pin it, or pin v0.149.0 (2026-08-28) until v0.150.0 has aged? Both pass every test of this unit | v0.150.0 (as designed); fall back to v0.149.0 with `go get` if a regression shows, no code change needed |
| Q14 | The admin SPA's `/status` page needs `getSite`/`getSnapshot` (07 D-1) but the admin route table has no such routes (14.2). Does the admin listener serve the public API (then `routes.Admin()` grows) or does the admin SPA call only `/api/v1/status` (then 07's admin document shrinks to the two admin operations)? | unresolved; this unit is neutral |

---

## Appendix A. Companion files

| File | Content |
|---|---|
| `20-design/06-httpapi.assets/` | the reference prototype (module `github.com/cantara/source-code-portal`); `README.txt` explains how to run it; `internal/httpapi/openapi/common.yaml` is a byte copy of `20-design/api/common.yaml` at the time of writing |
| `20-design/api/common.yaml` | owned by design unit 07; the **input** of this unit's generated `openapi` package |

## Appendix B. Quick reference for feature authors

```go
// 0. a strict handler gets (ctx, request object) and no *http.Request: req := httpapi.RequestFrom(ctx)   (cmd/scp mounts httpapi.WithRequest innermost)
// 1. mount
func (f *Feature) Mount(m httpapi.Mux) {
	h := commitsapi.NewStrictHandlerWithOptions(f, nil, commitsapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: reqErr, ResponseErrorHandlerFunc: respErr}) // reqErr, respErr := httpapi.StrictErrors(log)
	commitsapi.HandlerWithOptions(h, commitsapi.StdHTTPServerOptions{BaseRouter: m, ErrorHandlerFunc: httpapi.BindingError})
}
// 2. resolve a repository: repo, prob := authz.LookupRepo(ctx, snap, slot, az, scm.Slug(p.Repo)); if prob != nil { return nil, httpapi.Err(*prob) }
// 3. page:  pp, prob := httpapi.ParsePage(httpapi.Deref(p.Limit), httpapi.Deref(p.Cursor), 50, 200)
// 4. ETag:  etag := httpapi.WithDigest(httpapi.WeakETag(snap.Version(), class), httpapi.FreshnessDigest(statuses...)); if httpapi.NotModified(req, etag) { return 304 }
// 5. errors: return nil, httpapi.Err(httpapi.NotFound("..."))   // never http.Error, never fmt.Errorf text to the client
// 6. test:  apitest.ValidateResponse + apitest.ETagCoversBody + the enumeration fixture
```

---

## Verification log (independent verification, 2026-10-14)

Verifier: not the author. Method: read the whole document and prototype, compile and run the prototype in a scratch module (Go 1.27.1: `go vet`, `go test -race`, `-count=3` after the fixes), bundle the real 07 documents with redocly CLI 2.63.0 and push them through `RequestValidator`/`apitest.Lint`, run the unit-01 tables against a real `ServeMux`, re-check libraries and standards on the live web. Full report with evidence: `30-verification/design-06-httpapi.md`. Counts: **29 findings, 26 fixed in this document (and in the prototype where it was wrong), 3 open: V9 residual (a `.br` cannot be verified in Go), V20 (cross-unit routes/07 inconsistency), V26 (pin decision, Q13)**.

| ID | Sev | Finding | Fix |
|---|---|---|---|
| V1 | blocker | `hashedName` took the text after the last `-` as the hash; Vite hashes contain `-`/`_`, so about 12 % of asset names (a 20-asset build starts with p about 8 %) were rejected at startup | 4.9 rule rewritten (exactly 8 characters preceded by `-`), prototype fixed, test, build contract in 6.3 |
| V2 | blocker | `APIDefaults` was not scoped to `/api/`: 406 for `Accept: text/markdown` on `/repos/{repo}` and `/llms.txt` and for `Accept: text/html` on `/dashboard`; badge `Cache-Control` overwritten | scoped in code and text (4.5, 2.5), test |
| V3 | blocker | strict handlers have no `*http.Request`: the `NotModified(r, ...)`, `NextLink(r, ...)`, `deref`, `ptr` recipes (4.3, 4.4, App. B) could not be written | `WithRequest`/`RequestFrom`/`Deref`/`Ptr` (D29), recipes and e2e rewritten |
| V4 | blocker | wiring `table.Handle(api, "api.openapi", ...)` panics (API-class route) | recording mux (2.5, 7.1, 13.15), test |
| V5 | major | `Verify()` needs a handler for every table entry; 2.5/7.1 named only root, assets, catch-all | explicit registration set (2.5, 7.1), test |
| V6 | major | `ProblemMux` deleted `X-Content-Type-Options`; the test asserting it compared the header with `"text/plain"` | `WriteProblem` sets it, `ProblemMux` keeps it, test fixed |
| V7 | major | 4.16 item 8: `GET //` is not a 404; `ServeMux` answers 307 | text corrected, test |
| V8 | major | 4.7: "typed errors only / `ErrInvalidContentType`" is false; kin has no such error and matches a message prefix itself | text corrected, test pinned |
| V9 | major | `.br` siblings were claimed verified by decompression; only `.gz` can be | text corrected; build-step round trip required (6.3, 14.2); residual risk open |
| V10 | major | `BindingError` and the strict request handler rendered `err.Error()` against 3.1/T9 | fixed text (D30), test |
| V11 | minor | document sizes wrong (bundled 185/187 KB, not 110/20) | section 5 |
| V12 | minor | D2 evidence about `nethttp-middleware` partly wrong | D2 rewritten |
| V14 | minor | "Static is never compressed" false (`application/yaml` is compressible) | 4.6 |
| V15 | minor | 2.4 `const` block left `KindShell...` untyped | typed |
| V16 | minor | `Recovered` contract missing `ErrAbortHandler` and header-sent handling | 2.5 table, 14.2 |
| V17 | minor | `Reporting-Endpoints` needs a trustworthy origin; plain HTTP never reports | 4.11, rollout rule |
| V18 | minor | the trace tables lacked several features (badges, probes, CORS, freshness, admin listener, authorisation, request ids) and guardrails (GR-16, GR-17, GR-19, GR-26, GR-32, GR-35) | 11.1, 11.2 |
| V19 | minor | static ETag is a content hash, architecture says build id | D31 |
| V20 | major, open | `routes.Admin()` lacks the public API routes that 07 D-1 says the admin listener serves | 14.2, Q14 (unresolved) |
| V22-V24 | minor | two `If-None-Match` lines are a 400 on generated handlers; empty `?limit=` passes the validator; cursor `null` decodes to zero | 4.2, 4.7, 3.2 |
| V25 | minor | 7.3 claimed a plain-text 404 under `/api/` | 7.3 |
| V26 | minor | kin v0.150.0 is one day old | Q13 (v0.149.0 verified as fallback); decision open |
| V27 | minor | the JSON example in 3.1 showed fields the encoder omits | 3.1 |
| V28 | minor | `Authorizer` must not depend on ctx (`ClassOf` has none) | 2.3 |
| V30 | minor | a file was also served under `/theme-init.js/` (`path.Clean`) | exact name, test |
| V32 | minor | `/favicon.ico` behaviour undocumented | 4.16 item 18 |
| V33 | minor | the `scm` copy in the prototype had drifted from unit 01 | refreshed |

Verified without change: the import matrix (every package's real imports are a subset of its row; `go list`), `go doc` signatures of sections 2.2-2.4 (all but the `const` typing), 38 original test functions and coverage, `application/yaml` (RFC 9512), weak `If-None-Match` comparison, the kin-openapi first-error-only behaviour and fixed body detail on the real documents, the CSP strings.

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: `problem.go` gains the codes `feature-disabled`, `queue-full`, `repo-renamed`, `unhealthy`, `overloaded`, `request-timeout`, `WithRetry`, the constructors, and allows 308 for `repo-renamed` with `Location`; test `TestCrossDesignCodes` (CD-ER-02, 92).
- Changed: nested keys `server.public.*` (CD-CF-08); logx moved to `internal/platform/logx` (CD-IF-06).
- Known stale: the `common.yaml` copy under this unit's assets predates the review; `20-design/api/common.yaml` is authoritative.
