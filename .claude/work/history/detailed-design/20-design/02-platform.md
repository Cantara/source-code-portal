# 20-design / 02 - Platform (`internal/platform/{secret,logx,config,telemetry,httpx,netguard,lifecycle}`)

Status: design for implementation, independently verified and corrected on 2026-10-14 (Appendix C lists every correction; the report is `30-verification/design-02-platform.md`). Date: 2026-10-14. Unit key `02-platform`.
Conforms to `04-decisions.md`, `10-architecture.md` (sections 4.2, 5, 8.16, 10.1-10.4, 11.1, 11.3, 16, 17, 18; rules G3, G4, G7, G11), `11-adr/` and the neighbouring unit designs `01-kernel`, `03-readmodel-store`, `04-refresh-webhook`, `06-httpapi`, `08-catalog`, `09-commits-badges`, `13-manifests-ecosystems-b` and `14-integrations-access` (read, not restated; where one of them states a contract on this unit it is quoted and honoured in section 2.6).

This document is self-sufficient: an engineer who has never seen the conversation that produced it can implement all seven packages from it. A throw-away reference prototype, compile-checked and tested, is kept next to it in `02-platform.assets/` (a Go module that needs no kernel stubs because the platform packages import only each other; see its `README.md`). **Nothing in the prototype is final until it is described here**; where the document and the prototype disagree, the document wins and the prototype is wrong.

Conventions: "MUST/MUST NOT" are binding for the implementation; "design choice" marks a decision taken here where the architecture was silent; "Deviation Dn" points at section 14; "Question Qn" at section 15. "Verified" always carries a date. All library facts below were checked on 2026-10-14 against the Go module proxy (`go get`, `go list -m -u`), `go doc` of the downloaded module and a compiled test in the scratch module (go1.27.1 darwin/arm64; the architecture pins 1.27.2, a patch release that does not change any API used here).

---

## 1. Purpose, scope, non-goals, packages

### 1.1 Purpose

Everything the process needs before any feature code runs, and everything every feature needs without wanting to know how it is built:

- a **credential type that cannot be printed** and a **logger that cannot leak** (masking keys by name, logging a configuration map, putting credentials in URLs or defaulting to a trace level all leak sooner or later; the type and the scrub make leaking a compile or test failure: GR-13, GR-42);
- **one typed configuration**, loaded from an embedded default, a file, `SCP_` environment variables and flags, validated as a whole, documented by generation, and impossible to leave half-wired (a second configuration path, a string-keyed lookup or a setting that configures nothing each lets the documented and the running behaviour drift apart: GR-12, GR-04);
- **telemetry providers** whose instruments are handed down as dependencies (G11), never reached through globals;
- an **HTTP server layer**: the only constructor of `*http.Server`, one middleware chain with the security headers, request id, trusted-proxy client IP, keyed rate limiting and cross-origin protection, on by default (GR-17, GR-18);
- an **SSRF-guarded outbound client builder** (a client without timeouts or a redirect policy can hang a worker forever or be steered at internal addresses: GR-22);
- an **ordered start/stop runner** so the store is never closed under a running worker.

### 1.2 In scope

| Package | Rank | What it provides |
|---|---|---|
| `internal/platform/secret` | 0 | `Secret` (redacting in every formatting path, `{file: /abs}` decoding, constant-time compare), `FromFile`, size cap |
| `internal/platform/netguard` | 0 | `Policy` (connect-time address/port policy over `code.dny.dev/ssrf`, cloud-metadata addresses always denied), `NewClient`, `ValidateURL` |
| `internal/platform/telemetry` | 0 | `Provider` (meter and tracer providers, Prometheus exporter, optional OTLP over HTTP, runtime metrics, `scp_build_info`), `Config` of the `telemetry:` section |
| `internal/platform/lifecycle` | 0 | `Run(ctx, Plan)`, `Stage`, `Plan`, typed errors, `Config` of the `shutdown:` section |
| `internal/platform/logx` | 1 | process `slog.Handler`, context attributes, trace ids, value scrubbing, `RedactURL`, request-id and client-IP accessors, `Config` of the `log:` section |
| `internal/platform/config` | 1 | the generic loader (`Load`), `Describe`, env rules, JSON Schema and reference generation, `Overlay`, `Validator` |
| `internal/platform/httpx` | 2 | `NewServer`, `Serve`/`ServeAll`, `PublicChain`/`AdminChain`, `ClientIPResolver`, `Limiter` (counts its own refusals), header classes, `Rate`, `Config` and `RateConfig` of the `server:` and `ratelimit:` sections |

The generic config machinery, the platform-owned sections (`version`, `server`, `log`, `telemetry`, `ratelimit`, `shutdown`), the **complete top-level file structure with its owner per section**, the environment-variable naming rule and the schema/lint/reference generation are all in this unit (section 6).

### 1.3 Non-goals

- No business configuration: every section other than the six above is owned by its feature unit and only *composed* by `cmd/scp` (section 6.1). This unit never learns what a repository, a group or a webhook is.
- No route table and no problem-details renderer (`routes`, `httpapi`): `httpx` receives them as function values (section 2.4).
- No OIDC, sessions or identity: `access` (unit 14) establishes identity; `httpx` only resolves the client address.
- No gRPC OTLP in v1 (HTTP only), no log shipping other than the optional OTLP log bridge.
- No hot reload of configuration. A change needs a restart (the process is cheap to restart and the store warm-starts); `SIGHUP` is not handled.
- No `.env` loading, no config server, no secret-manager clients: secrets arrive as files (`*_FILE`) mounted by the platform, which is what Kubernetes, Docker and systemd all provide.
- No TLS termination or ACME. The portal sits behind an operator-owned proxy or is served plain on a private network (architecture 19); `server.public.hsts` and `X-Forwarded-Proto` handling exist only to cooperate with that proxy.

### 1.4 Exact packages and files

```text
internal/platform/
  secret/     secret.go
  netguard/   netguard.go
  telemetry/  telemetry.go
  lifecycle/  lifecycle.go
  logx/       logx.go
  config/     describe.go tree.go coerce.go env.go assign.go load.go schema.go
  httpx/      config.go server.go middleware.go requestid.go clientip.go ratelimit.go securityheaders.go
cmd/scp/      config.go (root composition struct), default.yaml (go:embed), main.go, assert.go, consumed_test.go, config_test.go
```

The architecture's tree (section 4, `httpx/` line) lists `server.go middleware.go requestid.go clientip.go ratelimit.go securityheaders.go`; this unit adds `config.go` for the section types. Test files sit beside the code (`*_test.go`); the prototype contains all of them.

---

## 2. Position in the architecture

### 2.1 Allowed imports (rank rule, architecture 5.2)

| Package | Rank | Imports (internal) | Imports (third party) | Matrix row (5.2) |
|---|---|---|---|---|
| `secret` | 0 | none | none | conforms |
| `netguard` | 0 | none | `code.dny.dev/ssrf` v0.3.0 | conforms |
| `telemetry` | 0 | none | `go.opentelemetry.io/otel` v1.47.0 (API + `sdk`, `sdk/metric`, `sdk/trace`, `sdk/log`, `exporters/prometheus` v0.69.0, `exporters/otlp/{otlptrace,otlpmetric,otlplog}/*http`, `semconv/v1.40.0`), `contrib/instrumentation/runtime` v0.72.0, `prometheus/client_golang` v1.25.0 | conforms (the only package allowed to import the SDK) |
| `lifecycle` | 0 | none | none (stdlib) | conforms |
| `logx` | 1 | `secret` | `otel/trace` (API) | conforms |
| `config` | 1 | `secret` | `go.yaml.in/yaml/v3` v3.0.5, `invopop/jsonschema` v0.14.0, `santhosh-tekuri/jsonschema/v6` v6.0.3, `golang.org/x/text` v0.43.0 | **Deviation D1** (drops `caarlos0/env/v11`, adds `x/text`) |
| `httpx` | 2 | `logx`, `secret` | `golang.org/x/time` v0.16.0 (`rate`), `otelhttp` v0.72.0, `otel/{attribute,metric,propagation,trace}` (API) | **Deviation D2** (adds `secret` and the otel API modules) |

"Equal rank never imports equal rank" holds: `telemetry` (0) cannot import `secret` (0), which is why the OTLP credential travels as a headers *file* parsed by `cmd/scp` and passed in `Options.Headers` (section 4.4), and why `httpx` (2) may import `secret` (0) and `logx` (1) but `logx` (1) may not import `config` (1).

Rules this unit enforces on the rest of the tree (all already in the architecture, restated because they are testable here):

1. No feature, service or adapter imports `platform/{config,telemetry,httpx,netguard,lifecycle}` (5.1 rule 2). They import `platform/secret` and `platform/logx` only. `cmd/scp` is the only importer of the other five. The `arch` test (`internal/archtest`, owned by the build unit) pins this; this unit contributes the five packages' import rows to its table.
2. G1/G2/G3: no `os.Getenv`, no package-level mutable state, no `init()` in any platform package. `os.Getenv` appears in `cmd/scp` only; `config.Load` receives `Environ []string` and a `ReadFile` function.
3. G4: `http.Server{}` literals only in `httpx.NewServer` and `cmd/scp`; `http.DefaultClient`, `http.DefaultTransport` and `http.Get` nowhere. `netguard.NewClient` builds its own `*http.Transport`.
4. G11: the OTel globals (`otel.SetMeterProvider`, `otel.GetTracerProvider`, `otel.SetTextMapPropagator`) are never called by the platform; `telemetry.Provider` hands out `Meter`, `Tracer` and `Propagator`. The one caveat, recorded in section 10, is that the OTLP exporters read `OTEL_EXPORTER_OTLP_*` environment variables inside the SDK; every setting that matters is passed explicitly so the environment cannot override it, and a test pins that.

### 2.2 Who imports this unit

| Importer | Imports | Why |
|---|---|---|
| every feature that holds a credential (`github/auth`, `integrations`, `access`, `docs/internal/render` via kernel, `webhook`) | `secret` | `secret.Secret` fields in their `Config` types decode from YAML, env and `{file:}` without importing `config` |
| `webhook`, `access`, `ops` and anything that logs per request | `logx` | `logx.RequestID(ctx)`, `logx.ClientIP(ctx)`, `logx.With`, field-name constants |
| `github` (adapter) | `secret`, `logx`, `netguard` (its 5.2 row lists all three) | receives a `*http.Client` built by `cmd/scp` with `netguard.NewClient`; may import `netguard` to classify `ErrRefused` and `ErrMetadataAddress` |
| `cmd/scp` | all seven | composition |
| tests | any | fixtures |

### 2.3 Consumer-defined interfaces (exact Go signatures)

The platform exports **concrete types**. Consumers declare the narrow interface they need next to their code, and `cmd/scp/assert.go` (architecture 10.1 step 6) proves at compile time that the platform types satisfy them (the prototype keeps the same assertions in `cmd/scp/consumers_test.go`). Written as the consumers declare them:

```go
// webhook (unit 04, section 3.4): the invalid-signature budget. Satisfied DIRECTLY by *httpx.Limiter (class ratelimit.webhook_invalid);
// the limiter increments scp_ratelimit_rejected_total{class="webhook_invalid"} itself when LimiterOptions.Rejected is set, so no bridge is needed.
type Limiter interface{ Allow(key string) bool }
// webhook's second parameter: func(*http.Request) string, satisfied by the method value (*httpx.ClientIPResolver).ClientIP.

// any feature that creates instruments (G11): satisfied by *telemetry.Provider
type MeterSource  interface{ Meter(name string) metric.Meter }
type TracerSource interface{ Tracer(name string) trace.Tracer }

// ops (admin /metrics route is mounted by cmd/scp from this):
type MetricsHandler interface{ MetricsHandler() http.Handler }

// every feature Config type implements this to take part in the whole-file validation, with no config import:
type Validator interface{ Validate(report func(path, msg string)) }
// satisfied by httpx.Config, httpx.RateConfig, telemetry.Config, lifecycle.Config (and, in the other units, by their Config types).

// any component with a blocking worker exposes the lifecycle shape:
type Worker func(ctx context.Context) error // satisfied by (*httpx.Limiter).Run

// logx accessors used without importing httpx:
var _ func(context.Context) string = logx.RequestID
var _ func(context.Context) string = logx.ClientIP

// slog redaction: any value holding a secret.Secret is safe in a log call because Secret implements slog.LogValuer.
var _ slog.LogValuer = secret.Secret{}
```

`config.Validator` is the same interface (`Validate(report func(path, msg string))`); it lives in `config` for the loader's benefit and is *not* imported by features. Its signature uses only builtin types on purpose.

### 2.4 What `httpx` receives instead of importing

`httpx` (rank 2) cannot import `httpapi`, `routes`, `access` or `webapp`. `cmd/scp` passes function values and data:

```go
type Hooks struct {
    Recovered       func(w http.ResponseWriter, r *http.Request, rec any)                   // = httpapi.Recovered(log)
    RateLimited     func(w http.ResponseWriter, r *http.Request, retry time.Duration)        // = httpapi.RateLimitedFunc()
    DenyCrossOrigin http.Handler                                                            // = httpapi.DenyCrossOrigin()
    Unauthorized    func(w http.ResponseWriter, r *http.Request)                            // optional; nil = minimal problem+json written by httpx
}
type Class struct {
    RateClass string      // key into Deps.Limiters; "" = not limited
    Headers   HeaderClass // html | asset | api | badge | media | "" (none)
    BodyLimit int64       // 0 = Deps.BodyLimit
    Quiet     bool        // probes: no span, access log at debug
}
type Deps struct {
    Log *slog.Logger; MeterProvider metric.MeterProvider; TracerProvider trace.TracerProvider   // no Meter: the limiters count their own refusals (section 4.5.5)
    Propagator propagation.TextMapPropagator; Clients *ClientIPResolver
    Classify func(*http.Request) Class      // built by cmd/scp from the routes.Table (section 7.2); httpx memoises it once per request
    Hooks Hooks; Limiters map[string]*Limiter
    BodyLimit int64; PublicURL string; HSTS bool; CSRFBypass []string
    AdminHosts []string                      // admin chain only: accepted Host header values (DNS-rebinding guard); empty = no check
    AfterClientIP []func(http.Handler) http.Handler // access.Middleware goes here (14, section 4.7.2)
}
```

`06-httpapi` section 2.5 lists exactly the first three hooks and the request-id/security-header/HSTS contract; section 7 states how each is honoured. `14-integrations-access` places `access.Middleware` "directly after client-IP resolution": that is `Deps.AfterClientIP`.

### 2.5 Wiring in `cmd/scp` (this unit's part of architecture 10.1)

```go
// step 1 of 10.1 (config, logger, telemetry); every call below exists and is compile-checked in the prototype (the sketch omits error handling).
res, err := config.Load(&cfg, config.Options{Defaults: defaultYAML, File: *configPath, ReadFile: os.ReadFile, Environ: os.Environ(), Set: sets})
// err is *config.Error: print every violation with file:line and exit 2. res.Warnings are logged (unknown SCP_* variables, did-you-mean).
log := slog.New(logx.NewHandler(os.Stderr, logx.Options{Level: lvl, Format: cfg.Log.Format, AddSource: cfg.Log.AddSource,
    Service: "scp", Version: version, Build: commit, Scrub: secretsOf(&cfg)}))
tel, err := telemetry.New(ctx, cfg.Telemetry, telemetry.BuildInfo{Service: "scp", Version: version, Commit: commit, Instance: hostname}, telemetry.Options{Headers: otlpHeaders, HTTPClient: otlpClient})
// step 7: servers
pubSrv := httpx.NewServer(cfg.Server.Public.Listen, httpx.PublicChain(deps)(publicMux), cfg.Server.Timeouts, log)
admSrv := httpx.NewServer(cfg.Server.Admin.Listen, httpx.AdminChain(deps, cfg.Server.Admin.BearerToken)(adminMux), cfg.Server.Timeouts, log)
// one limiter per class, each counting its own refusals (the same counter instrument for every class):
rejected, _ := tel.Meter("scp/httpx").Int64Counter("scp_ratelimit_rejected_total", metric.WithDescription("Requests refused by a keyed rate limiter."))
apiLim := httpx.NewLimiter(httpx.LimiterOptions{Rule: cfg.RateLimit.API, MaxKeys: cfg.RateLimit.MaxKeys, IdleTTL: cfg.RateLimit.IdleTTL, Class: "api", Rejected: rejected})
// listeners are opened before lifecycle.Run so a bind error is a startup error, then ONE servers stage serves both:
serversStage := lifecycle.Stage{Name: "servers", StopTimeout: cfg.Shutdown.ServerTimeout + time.Second,
    Run:   func(ctx context.Context) error { return httpx.ServeAll(ctx, cfg.Shutdown.ServerTimeout, httpx.Listener{Srv: pubSrv, Ln: pubLn}, httpx.Listener{Srv: admSrv, Ln: admLn}) },
    Drain: func(context.Context) error { ready.SetShuttingDown(); return nil }} // /readyz 503, /livez stays 200, SSE told retry: 30000
err = lifecycle.Run(ctx, lifecycle.Plan{DrainDelay: cfg.Shutdown.DrainDelay, Log: log, OnShutdown: stop, Observe: observe,
    Stages: []lifecycle.Stage{telemetryStage, storeStage, refreshStage, limiterStage, serversStage}})
// a cmd/scp warning: if cfg.Shutdown.WorstCase(refreshStage.StopTimeout) > 45s (the reference terminationGracePeriodSeconds), log it at startup.
```

The prototype `cmd/scp/main.go` is a runnable miniature of exactly this wiring (config, telemetry, two servers, lifecycle) driven by a test over real sockets.

### 2.6 Contracts this unit places on other units

Each of these is checked mechanically; a unit that violates one fails `config.Describe` or the consumption test at startup/CI, which is the intent (GR-12).

| Contract | Consequence for a feature `Config` type | Checked by |
|---|---|---|
| every key has a `yaml:"snake_case"` name | `yaml:"matchers"` ok, `yaml:"matchRules"` rejected | `Describe` |
| every key has `jsonschema_description:"..."` (the doc of the key, one sentence) | the reference document is complete by construction | `Describe` |
| constraints are `jsonschema:"minimum=,maximum=,enum=,pattern=,required"`; **a `pattern` MUST NOT contain a comma** (invopop splits tags on commas; verified 2026-10-14: `{1,5}` truncated the pattern) | use `[0-9]+` instead of `{1,5}`, or an `Overlay` | prototype test run |
| durations are `time.Duration` with optional `scp:"min=1s,max=5m"`; sizes are an `int64` tagged `scp:"unit=bytes,min=1KiB,max=1MiB"` (IEC units only, plain integer accepted, decimal `MB` refused) | no `ByteSize` type is needed in a feature: `03-readmodel-store` says "ByteSize" in its tables, which is this same `int64` with the tag; `14` says "plain integer bytes", which is accepted by the same rule | `Describe`, `coerce` |
| secrets are `secret.Secret` (decodes `{file: /abs}` from YAML, a literal from `SCP_X`, a path from `SCP_X_FILE`) | no `string` password field exists anywhere | `Describe` |
| a rate is `httpx.Rate`-style text (`"10/min burst 10"`), any type implementing `encoding.TextUnmarshaler` is a text leaf | `04` and the outline's `ratelimit` scalars work unchanged | `Describe` |
| cross-field rules live in a `Validate` method on the section type (value or pointer receiver). Three shapes are accepted: `Validate(report func(path, msg string))` (preferred; `path` relative to the section), `Validate() error`, and `Validate(prefix string) error`. An `error` is placed at the leaf named by an optional `ConfigPath() string` method on the error (a `errors.Join` is unwrapped), else by the text-prefix promotion rule (message begins with the full key path), else at the section | e.g. `03-readmodel-store` `Store.Validate(prefix) error`, `05` `Validate() error`: no adapter is needed. **Question Q1** (optional `ConfigPath`) | `Load` |
| an embedded `yaml:",inline"` struct puts two owners' keys in one mapping (`github.sources[].discovery` = github's `mode`/`org` + catalog's keys, as `08-catalog` states) | both Validate methods run at the shared path; a key declared twice is an error | `Describe` |
| a map keyed by name (`refresh.upstreams.<name>`) is addressable from the environment **only for entries that exist in the embedded default or the file**; env never creates entries | `04`'s `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY` works because `github` is in `default.yaml` | `overlayEnv` |

---

## 3. Domain model and Go types

All declarations below were compiled in the prototype (go1.27.1). Unexported helpers are omitted.

### 3.1 `secret`

```go
package secret

const MaxFileSize = 64 << 10 // a secret file larger than this is refused
const Redacted = "[REDACTED]"
var ErrEmpty, ErrNotAbsolute error

type Secret struct { /* v *string; file string: a pointer on purpose, see 4.1 */ }

func New(s string) Secret                 // literal (tests, env)
func Ref(path string) Secret              // pending file reference
func FromFile(path string) (Secret, error)
func (s *Secret) Resolve() error          // reads the file of a pending secret; trims one trailing newline; refuses empty, non-regular and > MaxFileSize
func (s Secret) Pending() bool
func (s Secret) Reveal() string           // the only way to get the value; grep-able (rule: only the code that sends it calls Reveal)
func (s Secret) IsZero() bool             // empty is unset (GR-13)
func (s Secret) Equal(o Secret) bool
func (s Secret) EqualString(candidate string) bool // crypto/subtle, length-independent
// every formatting path yields "[REDACTED]":
func (s Secret) String() string; GoString() string; Format(fmt.State, rune); MarshalJSON() ([]byte, error)
func (s Secret) MarshalText() ([]byte, error); MarshalYAML() (any, error); LogValue() slog.Value
// decoding:
func (s *Secret) UnmarshalText(b []byte) error                          // env literal
func (s *Secret) UnmarshalYAML(unmarshal func(any) error) error         // only {file: /absolute/path}; a scalar or a relative path is an error
```

### 3.2 `logx`

```go
package logx

type Config struct { Level string; Format string; AddSource bool }   // the `log:` section (tags in 6.2)
type Options struct {
    Level slog.Leveler; Format string /* json (default) | text */; AddSource bool
    Service, Version, Build string
    Scrub []secret.Secret // values never to appear in any record; ignored below MinScrubLen
}
const MinScrubLen = 8

func NewHandler(w io.Writer, o Options) slog.Handler
func ParseLevel(s string) (slog.Level, error)
func With(ctx context.Context, attrs ...slog.Attr) context.Context   // a later attribute with the same key replaces the earlier
func Attrs(ctx context.Context) []slog.Attr
func WithRequestID(ctx context.Context, id string) context.Context;  func RequestID(ctx context.Context) string
func WithClientIP(ctx context.Context, ip string) context.Context;   func ClientIP(ctx context.Context) string
func WithDelivery(ctx context.Context, id string) context.Context
func WithJob(ctx context.Context, key, kind string, attempt int) context.Context
func Err(err error) slog.Attr
func RedactURL(raw string) string // scheme+host+path only; unparsable -> "[invalid-url]"

// stable field names (dashboards and tests key on them), architecture 18.1:
const ( KeyTimestamp = "ts"; KeyRequestID = "request_id"; KeyTraceID = "trace_id"; KeySpanID = "span_id"; KeyDelivery = "delivery_id"
        KeyJobKey = "job_key"; KeyJobKind = "job_kind"; KeyAttempt = "attempt"; KeySource = "source"; KeyInstall = "installation_id"
        KeyRepo = "repo"; KeyUpstream = "upstream"; KeyStatus = "status"; KeyDurationMS = "duration_ms"; KeyErrCode = "err_code"
        KeySnapshot = "snapshot_version"; KeyClientIP = "client_ip"; KeyErr = "err" )
```

### 3.3 `config`

```go
package config

type Options struct {
    Defaults    []byte                       // embedded default.yaml: the ONLY source of defaults
    File        string                       // "" = none
    ReadFile    func(string) ([]byte, error) // required when File is set (cmd/scp passes os.ReadFile)
    Environ     []string                     // os.Environ(); nil skips the environment layer
    Set         []string                     // repeated --set path=value
    SkipSecrets bool                         // lint: validate {file:} references without reading the files
    Overlays    []Overlay                    // hand-written schema fragments for subtrees tags cannot express
}
type Result struct { Applied []Applied; Warnings []string }          // Applied: which layer set which path (never the value)
type Applied struct { Layer Layer; Path, From string }
type Layer string // "default" | "file" | "env" | "flag"
type Violation struct { Path, Message, File string; Line int }
type Error struct{ Violations []Violation }                          // returned by Load; lists every problem, not the first
func Load(dst any, o Options) (Result, error)

type Validator interface{ Validate(report func(path, msg string)) }

type Overlay struct{ Path string; Schema []byte }                    // Path: "catalog" or "github.sources[].discovery"
func Schema(dst any, defaults []byte, overlays ...Overlay) ([]byte, error) // JSON Schema 2020-12, deterministic
func Reference(dst any, defaults []byte) (string, error)             // Markdown table of every key
func EnvNames(root *Field) []string                                  // every settable variable, {i} list index, {k} map key
func ParseBytes(s string) (int64, error)                             // "64MiB" | "1048576"

type Kind uint8 // KindStruct KindList KindMap KindString KindInt KindUint KindFloat KindBool KindDuration KindBytes KindSecret KindText
type Field struct {
    Name, Path, Doc, Owner string; Kind Kind; Type reflect.Type; Index []int
    Children []*Field; Inlines [][]int; Elem *Field; NoEnv bool; Env string; Tag reflect.StructTag
    Min, Max int64; HasMin, HasMax bool
}
func Describe(root any) (*Field, error) // fails listing every malformed declaration; Walk and Child are helpers
```

### 3.4 `telemetry`

```go
package telemetry

type Config struct { Prometheus PrometheusConfig; OTLP OTLPConfig; Traces TracesConfig; Runtime RuntimeConfig } // the `telemetry:` section (6.2)
type BuildInfo struct{ Service, Version, Commit, Instance string }
type Options struct {
    Headers        map[string]string // parsed OTLP headers (cmd/scp reads telemetry.otlp.headers_file)
    LatencyBuckets []float64         // seconds; default DefaultBuckets
    HTTPClient     *http.Client      // a netguard client for the collector; nil = SDK default
}
var DefaultBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}

type Provider struct{ /* meter, tracer and logger providers, registry */ }
func New(ctx context.Context, c Config, b BuildInfo, o Options) (*Provider, error)
func (p *Provider) Meter(name string) metric.Meter
func (p *Provider) Tracer(name string) trace.Tracer
func (p *Provider) MeterProvider() metric.MeterProvider     // for otelhttp / otelhttp.NewTransport
func (p *Provider) TracerProvider() trace.TracerProvider
func (p *Provider) Propagator() propagation.TextMapPropagator // W3C trace context + baggage
func (p *Provider) LoggerProvider() log.LoggerProvider        // nil unless OTLP logs are enabled
func (p *Provider) MetricsHandler() http.Handler
func (p *Provider) Shutdown(ctx context.Context) error       // flush and stop, errors joined
func (c Config) Validate(report func(path, msg string))
```

### 3.5 `httpx`

```go
package httpx

// config (section 6.2): Config{Public, Admin, Timeouts}, PublicConfig{Listen, PublicURL, HSTS, TrustedProxies}, AdminConfig{Listen, BearerToken secret.Secret, AllowedHosts []string},
// TimeoutsConfig{ReadHeader, Read, Write, Idle time.Duration; MaxHeaderBytes int64; MaxHeaderValues int}, RateConfig{Badges, API, MediaExt, CSPReports, WebhookInvalid Rate; SSEPerIP, MaxKeys int; IdleTTL time.Duration}

type Rate struct{ PerMinute, Burst int }                      // text: "120/min burst 60" | "5/s burst 20" | "30/min" | "120/h" (per-hour only as a multiple of 60: PerMinute is an integer)
func (r *Rate) UnmarshalText(b []byte) error; func (r Rate) MarshalText() ([]byte, error); func (r Rate) String() string

func NewServer(addr string, h http.Handler, t TimeoutsConfig, errLog *slog.Logger) *http.Server
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error
type Listener struct{ Srv *http.Server; Ln net.Listener }
func ServeAll(ctx context.Context, grace time.Duration, ls ...Listener) error   // serve and stop all listeners together within ONE grace; a failure of one stops the rest, its error first
func PublicChain(d Deps) func(http.Handler) http.Handler
func AdminChain(d Deps, token secret.Secret) func(http.Handler) http.Handler

func ParseTrusted(cidrs []string) ([]netip.Prefix, error)     // refuses /0 of either family
type ClientIPResolver struct{ /* trusted prefixes */ }
func NewClientIPResolver(trusted []netip.Prefix) *ClientIPResolver
func (c *ClientIPResolver) Resolve(r *http.Request) (ip netip.Addr, viaProxy bool)
func (c *ClientIPResolver) ClientIP(r *http.Request) string   // "" when the peer is unparsable
func (c *ClientIPResolver) Trusted(a netip.Addr) bool
func (c *ClientIPResolver) HTTPS(r *http.Request) bool
func Peer(r *http.Request) netip.Addr
func RateKey(a netip.Addr) string                              // IPv4 as is; IPv6 bucketed to its /64; "unknown" for the zero Addr

type LimiterOptions struct {
    Rule Rate; MaxKeys int; IdleTTL time.Duration
    Class string; Rejected metric.Int64Counter // when Rejected != nil every refused Allow adds 1 with attribute class=Class (outside the shard lock)
}
type Limiter struct{ /* 16 shards */ }
func NewLimiter(o LimiterOptions) *Limiter
func (l *Limiter) Allow(key string) bool                       // consumes one token
func (l *Limiter) RetryAfter(key string) time.Duration         // >= 1 s
func (l *Limiter) Len() int; func (l *Limiter) Sweep() int; func (l *Limiter) Run(ctx context.Context) error
func RunSweepers(ctx context.Context, ls ...*Limiter) error   // one stage body for all limiters; returns nil on ctx cancel

type HeaderClass string // HeadersNone "" | HeadersHTML | HeadersAsset | HeadersAPI | HeadersBadge | HeadersMedia
func (c HeaderClass) Defaults(hsts bool) []Pair                // golden-tested strings, 7.3
const HeaderRequestID = "X-Request-Id"
func RetryAfterSeconds(d time.Duration) string; func ErrBodyTooLarge(err error) bool
```

### 3.6 `netguard`

```go
package netguard

type Policy struct{ /* allow prefixes, extra ports */ }          // the zero Policy: public addresses, ports 80 and 443
func NewPolicy(allowCIDRs []string, ports []int) (Policy, error) // /0, bad CIDR, bad port: all reported (errors.Join)
func MustPolicy(allowCIDRs []string, ports []int) Policy
func (p Policy) Control() func(network, address string, c syscall.RawConn) error // net.Dialer.Control

type ClientOptions struct {
    Policy Policy
    DialTimeout, TLSTimeout, HeaderTimeout, Timeout time.Duration // 5s, 5s, 10s, 30s
    MaxRedirects int; MaxHeaderBytes int64                        // 0 = never follow; 64 KiB
    Hosts []string; RefuseIPLiterals bool; AllowHTTP bool
    RootCAs *x509.CertPool; ProxyURL *url.URL; UserAgent string
    Wrap func(http.RoundTripper) http.RoundTripper                // instrumentation (otelhttp.NewTransport) goes here
}
func NewClient(o ClientOptions) (*http.Client, error)
func ValidateURL(raw string, allowHTTP bool, p Policy) error     // static: scheme, userinfo, port, IP literal
func PrivateNetworks() []string                                  // 10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7 (what "lives on a private network" means; never loopback, link-local or metadata)
var ErrMetadataAddress, ErrRefused error
```

### 3.7 `lifecycle`

```go
package lifecycle

const DefaultStopTimeout = 10 * time.Second
const ( PhaseRun = "run"; PhaseDrain = "drain"; PhaseStop = "stop"; PhaseClose = "close" )
var ErrStopTimeout, ErrStageExited error
type StageError struct{ Stage, Phase string; Err error }          // Unwrap
type PanicError struct{ Value any; Stack []byte }
type Stage struct {
    Name string
    Run, Drain, Close func(ctx context.Context) error             // any may be nil
    StopTimeout time.Duration                                    // bounds waiting for Run and each of Drain and Close
}
type Plan struct {
    Stages     []Stage                                           // START order; stopped in reverse
    DrainDelay time.Duration
    Log        *slog.Logger                                      // nil = discard
    OnShutdown func(cause error)                                 // called once as shutdown begins (cmd/scp passes signal.NotifyContext's stop)
    Observe    func(stage, phase string, d time.Duration, err error)
}
func Run(root context.Context, p Plan) error
type Config struct{ DrainDelay, ServerTimeout, StoreTimeout, TelemetryTimeout time.Duration }     // the `shutdown:` section
func (c Config) WorstCase(extra ...time.Duration) time.Duration                                // drain + server + store + telemetry + the stop timeouts of stages this section does not own
```

---

## 4. Behavioural spec, pseudocode, errors

### 4.1 `secret`

1. `Secret` stores the value behind a `*string`. A struct field printed by `fmt` with a verb the type does not handle (`%p`, or `%v` of a struct that embeds it by a bad-verb path) falls into fmt's bad-verb printing, which walks struct fields **without calling methods**; a pointer prints an address instead of the credential. Found 2026-10-14 by the prototype test (`TestNeverPrints`): the first version held a `string` and `%p` leaked it. The test now covers `%v %+v %#v %s %q %x %X %d %t %p` of the value and of a struct holding it (by value and by pointer), `fmt.Fprint`/`Fprintln`, `json.Marshal`, `yaml.Marshal` and a `slog` JSON handler.
2. `Secret` is **comparable only through `Equal`/`EqualString`**, which hash both sides with SHA-256 and compare with `crypto/subtle.ConstantTimeCompare` (length-independent). Never `==` (it would compare pointers).
3. **Empty is unset** (GR-13): `New("")`, an empty env value and an empty file all yield a zero `Secret`; `IsZero()` is true; `Resolve()` of an empty file is `ErrEmpty` (a file reference that resolves to nothing is a configuration error, not "no credential").
4. Decoding. YAML: a mapping with exactly the key `file` whose value is an **absolute** path (`ErrNotAbsolute` otherwise; a scalar is refused with the message "a secret must be written as {file: /absolute/path}"). A relative path would resolve against whatever the working directory is at start, which differs between `docker run`, systemd and a shell. Env: `SCP_X` is the literal, `SCP_X_FILE` the path (section 4.3.5). `--set` refuses secrets (they would appear in `ps`).
5. `Resolve()` reads the file once at startup, after validation: `os.Stat` (follows symlinks, so a Kubernetes secret mount `..data -> ..2026_...` works) must say regular file (a FIFO would block forever), then at most `MaxFileSize` (64 KiB) + 1 bytes are read, exactly one trailing `\n` or `\r\n` is trimmed, empty is `ErrEmpty`. The file is **not** watched; rotation needs a restart, except for the features that re-read their own reference (`github/auth` re-reads a PAT on mtime change, 05 section 2.2); they call `secret.FromFile` themselves.
6. Where a secret is *used*, the code calls `Reveal()` (greppable; `forbidigo` lists `Reveal` outside the sender packages as a review flag, not a ban).

### 4.2 `logx`

**Handler** (`NewHandler(w, Options)`): `slog.JSONHandler` (or `TextHandler` when `Format == "text"`, for a developer terminal) wrapped by a `handler` that:

1. renames `time` to `ts` (UTC, RFC 3339 with nanoseconds) and lower-cases `level` (`info`, `warn`, `error`, `debug`), so every record starts `{"ts":...,"level":...,"msg":...,"service":...,"version":...,"build":...}` (the order the architecture lists, 18.1);
2. adds the **context attributes** carried by `logx.With` (request id, client IP, delivery id, job key/kind/attempt, ...) to every record logged with a `*Context` method; `With` returns a new context, a later attribute with the same key replaces the earlier (so a retry's `attempt` replaces the first);
3. adds `trace_id` and `span_id` when `trace.SpanContextFromContext(ctx)` is valid (OTel API only; rank 1 rule);
4. **scrubs values**: every `Options.Scrub` secret of at least `MinScrubLen` (8) bytes is replaced by `[REDACTED]` inside every string value, error text and group member of the record, including text produced by hand-formatting (`fmt.Sprintf("token %s", tok.Reveal())` in a careless line). Shorter values are ignored on purpose: replacing a 3-letter password everywhere would mangle ordinary words, and the dev token is not a credential. **Limit (stated, accepted)**: the scrub covers string, error and group attribute values and the message; a value of another kind (`slog.Any("req", someStruct)`, a map) is encoded by the handler unscrubbed, which is why the rules below forbid passing such values and why the real protection is the type, not the scrub. `Scrub` is `secretsOf(&cfg)`, built in `cmd/scp` by walking the loaded config for `secret.Secret` leaves with `config.Describe` (a registered secret that is `Pending` or zero is skipped);
5. keeps `AddSource` off by default (the line number of a log call helps developers, not operators).

**Accessors.** `RequestID(ctx)` and `ClientIP(ctx)` exist so `httpapi` (renders `instance`), `webhook` (keys the invalid-signature limiter) and `ops` read them **without importing `httpx`**. `WithRequestID`/`WithClientIP` are called by `httpx` only.

**`RedactURL(raw)`** keeps scheme, host and path; drops userinfo, query and fragment; unparsable input yields `[invalid-url]`. It is what any code that must mention a URL in an error or log line uses (GR-13): credentials travel in headers, never in a query string, and an error line that echoes a full URI is the usual way a query-string credential or a private repository path reaches a log aggregator. **Limit**: `RedactURL` keeps the path, so a URL whose *path* carries a credential (Slack-style webhook URLs) is not made safe by it; SCP has no such integration.

**Level.** `info` default (a trace or debug default floods the log, hides the records that matter and makes accidental leaks likelier; GR-42). The level is fixed for the process lifetime (no runtime level endpoint in v1; `slog.LevelVar` is a one-line change if an operator ever needs it, Q9).

**Rules for callers** (reviewed, with `sloglint` configured to enforce key style): `slog.*Context(ctx, ...)` only; attribute keys are the `Key*` constants or lower snake_case literals; no secrets in attributes (type prevents), no request bodies, no raw URLs with queries, no `Authorization` headers, no webhook payload before signature verification (GR-20).

### 4.3 `config`

#### 4.3.1 Pipeline (the whole of `Load`)

```text
Load(dst *Config, Options{Defaults, File, ReadFile, Environ, Set, SkipSecrets, Overlays}):
  1. root := Describe(dst)                           // reflect once: names, kinds, docs, env names, owners; ALL declaration errors at once
  2. tree  := parse(Defaults)                        // exactly one YAML document, string keys only, a mapping at the top, aliases bounded by yaml.v3
  3. if File != "": data := ReadFile(File) (<= MaxFileBytes 1 MiB); tree = merge(tree, parse(data)); remember a line index (path -> line)
  4. if Environ != nil: for each SCP_* variable: overlayEnv(root)      // section 4.3.5
  5. for each --set path=value: applySet                                // coerces by the field's kind; refuses sections and secrets
  6. if any violation so far: return *Error{all of them}              // still no schema work on a half-built tree
  7. schema := compile(Schema(dst, Defaults, Overlays...))             // generated, deterministic, draft 2020-12
  8. validateTree(schema)                                              // type, enum, pattern, min/max, required, unknown key (+ did-you-mean); leaf causes -> paths; plus scp min/max of durations and sizes
  9. if any violation: return *Error
 10. assign(tree -> dst)                                               // own reflection; text leaves call UnmarshalText; secrets get a slot
 11. unless SkipSecrets: Resolve every secret slot (reads files)       // after validation, so a typo is reported before an unreadable file
 12. if no violation: runValidators(dst)                               // section Validate methods, in declaration order, every violation collected
 13. return Result{Applied (layer, path, from; never values), Warnings}, nil  or *Error
```

Merge is on the generic tree: maps merge key by key, **lists replace** (a file that sets `server.public.trusted_proxies` replaces the default list, it does not append; appending would make removing a default impossible), scalars replace, `null` is refused by the schema (use the key's zero value or omit it).

**Violations** carry `Path`, `Message`, `File`, `Line`. `Error()` prints them sorted by line then path, one per line, as `scp.yaml:14: github.sources[0].auth.kind: "app2" is not one of "github_app", "pat", ...` and, for an unknown key, `unknown key "listn" (did you mean "listen"?)`. A violation the parser reports without a line (syntax error, duplicate key, second document) is printed as `scp.yaml: <message>`: the file name is never dropped. `cmd/scp` prints the error and exits 2 (usage/config error; 1 is reserved for runtime failures). Example outputs are golden-tested.

**Strictness.** Unknown keys, wrong types, multiple YAML documents, duplicate keys, non-string keys and a non-mapping top level are all violations. There is no "ignore unknown fields" mode (GR-12). A key of the wrong case is unknown (`Listen` is not `listen`).

#### 4.3.2 `Describe`: declaration rules (also the contract of section 2.6)

For every exported struct field of the root type and below (`yaml:"-"` skips a field):

- `yaml:"name"` is required and must match `^[a-z][a-z0-9_]*$`; the environment name derives from it, so it must be unambiguous;
- `jsonschema_description:"..."` is required (the docs are generated; an undocumented key fails the build, not a review);
- kinds supported: `string`, signed and unsigned ints, floats, `bool`, `time.Duration`, `secret.Secret`, any type implementing `encoding.TextUnmarshaler` (a **text leaf**: string or bare integer in the file, env and flags), structs (sections), slices of any of those, `map[string]T` (T one of those, not another slice or map). Anything else (pointers to maps, channels, interfaces, slice of slices) is a declaration error;
- an embedded struct tagged `yaml:",inline"` contributes its keys at this level; its own `Validate` runs at this path; a key declared twice in a mapping is an error;
- `scp:"unit=bytes,min=1KiB,max=1MiB"` on an `int64` makes a size; `scp:"min=1s,max=5m"` bounds a duration; `scp:"noenv"` withdraws a key from the environment layer (nothing in v1 uses it; it exists for a key whose value would be unsafe in `ps` or shell history and that is not a `Secret`);
- `Validate` must have one of the three shapes of section 4.3.7, otherwise a declaration error (a method that is silently never called is exactly the "decorative setting" failure GR-04 and GR-12 forbid);
- two keys must not produce the same environment name (`a_b.c` and `a.b_c` both give `SCP_A_B_C`): detected here, with both paths in the message. The `_FILE` twin of a secret is a second variable and is reserved the same way (a secret `token` next to a string `token_file` is a declaration error; `TestDescribeRejectsFileTwinCollision`).

#### 4.3.3 Types accepted from each layer

| Kind | YAML | Env / `--set` | Notes |
|---|---|---|---|
| string, bool, int, uint, float | native scalar | string parsed (`true`/`false` only for bool) | wrong type is a violation naming the expected type |
| duration | string `"500ms"`, `"30s"`, `"5m"`, `"1h30m"` (`time.ParseDuration`) | same | a bare integer is refused (what unit?) (GR-12); schema pattern and `scp` min/max |
| bytes (`unit=bytes`) | `"64MiB"`, `"512KiB"`, `"1GiB"`, `"512B"` or an integer | same | IEC only: `64MB` is refused so it cannot mean two things; overflow is refused |
| secret | `{file: /abs/path}` | `SCP_X` literal or `SCP_X_FILE` path, **never both** | `--set` refused |
| text leaf | string, or integer for sizes | string | `UnmarshalText` error text becomes the violation message |
| list of scalars | YAML sequence | **one comma-separated variable**, elements trimmed, empty string = empty list | no escaping: elements may not contain commas (CIDRs, ports, enum words: none do) |
| list of structs | sequence of mappings | per element and field: `SCP_..._0_FIELD` | indices may have gaps only in the sense of "unset"; a gap that leaves a *required* field of element 0 unset is a violation of that field |
| map | mapping | fields of **existing** entries only (below) | env never creates an entry |

#### 4.3.4 `--set` flag

`--set path=value`, repeatable, path in the YAML dotted form with `[i]` for list indices: `--set server.public.listen=:8080`, `--set github.sources[0].webhook.enabled=false`. Applies after the environment. It refuses: a section (`server`), a secret, a list of structs as a whole, a path the schema does not know (message "no such configuration key"). Purpose: one-off overrides in a shell and in tests without writing files. It is *not* a second configuration surface for production (Kubernetes manifests use env and files).

#### 4.3.5 The environment naming rule (complete)

1. A variable is `SCP_` + the key path in upper snake case: path segments upper-cased, joined with `_`. The segments come from the `yaml` names, so `server.public.trusted_proxies` is `SCP_SERVER_PUBLIC_TRUSTED_PROXIES` and `refresh.backoff.max_attempts` is `SCP_REFRESH_BACKOFF_MAX_ATTEMPTS`. There are no dots and no per-key aliases (GR-12).
2. A list element is addressed by its **zero-based index as a number**: `github.sources[0].auth.kind` is `SCP_GITHUB_SOURCES_0_AUTH_KIND`. Indices need not be contiguous in the environment, but element order is by index. A list of structs cannot be created *only* from the environment unless every required field of each element is also set; the schema reports the missing ones.
3. A **list of scalars** is one variable with comma-separated values: `SCP_SERVER_PUBLIC_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16`. An empty value is "unset", not "empty list" (to empty a default list use the file or `--set path=`, which is the empty list).
4. A **map entry** is addressed by its key **only if the entry already exists** in the embedded default or the file: `refresh.upstreams.github.concurrency` is `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY`. When two keys could claim a variable (`a` with field `b_c` and `a_b` with field `c`), the longest key wins and the variable counts as used once. A variable for a non-existent entry is a warning (it cannot create one: a typo in a name must not silently create a new upstream).
5. A **secret leaf** accepts `SCP_X` (literal value; discouraged: it shows in `docker inspect` and `/proc/<pid>/environ`) or `SCP_X_FILE` (absolute path read at startup). Both set is a violation naming both. An empty value is unset.
6. An **empty value counts as unset** for every kind (Docker Compose and Kubernetes templates routinely export empty strings; treating `""` as configured would turn an unset credential into an empty one that is sent: GR-13).
7. **Reserved**: `SCP_CONFIG` (the config file path, read by `cmd/scp` before `Load`). **Ignored without warning**: the Kubernetes service-link variables `SCP_SERVICE_HOST`, `SCP_SERVICE_PORT`, `SCP_SERVICE_PORT_*`, `SCP_PORT`, `SCP_PORT_*` (a Service named `scp` makes the kubelet inject them; a deployment must not fail or warn because of them).
8. **Any other `SCP_*` variable is a warning, never fatal**: `environment variable SCP_SERVER_PUBLIC_LISTN matches no configuration key; did you mean SCP_SERVER_PUBLIC_LISTEN?` (Levenshtein distance up to 3 over the generated list). A variable containing a dot gets the specific hint "dotted names are not supported; derive the name from the key path in upper snake case". Warnings are logged once at startup and counted in `/status`. They are warnings, not errors, because other tooling legitimately shares the `SCP_` prefix (an operator's `SCP_*` for scp(1) in a shell) and failing a pod start on it would be hostile; `scp config lint --strict-env` turns them into errors for CI.
9. Variables without the `SCP_` prefix are **never read** by the loader (`SERVER_PORT`, `GITHUB_ACCESS_TOKEN`, `HTTPS_PROXY`, `OTEL_*` have no meaning to it). The one exception is outside the loader: the OTLP exporters read `OTEL_EXPORTER_OTLP_*` inside the SDK, which the telemetry package neutralises by passing every setting explicitly (section 4.4, tested).
10. The full generated list is `out/env.txt` in the prototype (42 variables for the platform sections) and the reference document `docs/reference/config.md` for the whole tree.

#### 4.3.6 Defaults, the single source

`cmd/scp/default.yaml` (embedded with `go:embed`) is the **only** place a default value is written. Go struct tags carry no `default:`, and no code path falls back to a literal when a field is zero (`ApplyDefaults`-style methods in feature packages are not called by the machinery and MUST NOT be needed by `cmd/scp`; a unit may keep one for its own tests). A required key with no sensible default is simply absent from `default.yaml` and carries `jsonschema:"required"`; the schema then reports it when absent. The generated reference prints the default next to the key; the consumption test (4.3.9) guarantees that nobody re-defaults it in code.

#### 4.3.7 `Validate`: whole-file rules, three accepted shapes

Per-key constraints (type, enum, range, pattern, required) are schema-generated. Rules that span keys or need code (a list element's `id` unique, `public_url` consistent with `trusted_proxies`, `webhook.enabled` implies a secret) live in `Validate` methods on the **section types**, called after assign and secret resolution, parents before children, every violation collected:

| Shape | Behaviour |
|---|---|
| `Validate(report func(path, msg string))` | **preferred**: no import, no error juggling; `path` is relative to the section (`""` = the section itself) |
| `Validate() error` | the returned tree (`errors.Join`, multiple `%w`) is flattened; a leaf implementing `ConfigPath() string` is placed at that path (relative to the section, or already starting with the section path); a leaf whose text starts with the section path then `": "` is promoted to that path; any other leaf is reported at the section |
| `Validate(prefix string) error` | as above, called with the section's own path as the prefix |

This is what lets units written against the architecture's outline ("`Validate()` collects path-accurate errors") work unchanged: `03-readmodel-store` (`Validate(prefix) error`), `05-github-adapter` (`Validate() error` with `*ValidationError{Path, Msg}`; it needs the one-line method `func (e *ValidationError) ConfigPath() string { return e.Path }` to be placed exactly, otherwise the text-prefix promotion applies because its `Error()` is `Path + ": " + Msg`) and `14-integrations-access`. Identical reports collapse (a promoted and an inline call can both fire). Cross-section rules (private visibility needs `access.mode: trusted-proxy`; a Jenkins instance referenced by a group exists) live in the **root** type's `Validate` in `cmd/scp`, which is the only code allowed to see two sections.

#### 4.3.8 Schema, lint, reference

- `Schema(dst, defaults, overlays...)` = `invopop/jsonschema` with `FieldNameTag: "yaml"`, `DoNotReference: true`, `AllowAdditionalProperties: false`, `RequiredFromJSONSchemaTags: true`, mappers for `time.Duration` (string with a duration pattern), `secret.Secret` (`{file}` object with absolute-path pattern), text leaves (string or integer); then a patch pass that adds `default` (from `default.yaml`), `x-scp-env`, `x-scp-owner` (the package path that declared the section), `x-scp-min`/`x-scp-max`. Output is deterministic (sorted, indented) and **committed** as `docs/reference/config.schema.json` with a CI drift check (`git diff --exit-code` after `make gen`), and served by `scp config schema` for editors (`# yaml-language-server: $schema=...`).
- The same schema is compiled by `santhosh-tekuri/jsonschema/v6` for validation, so **the published schema and the enforced rules cannot differ**. Its error tree is flattened to leaf causes mapped to YAML paths; `additionalProperties` becomes `unknown key` with a did-you-mean from the sibling names; duration and size bounds (`scp:"min=,max="`) are checked in a final pass so they are reported together with everything else.
- `Overlay{Path, Schema}` replaces a generated subtree with a hand-written JSON Schema fragment for the few shapes tags cannot express (the catalog's matcher `oneOf`; "if kind is github_app then private_key is required"). The overlay keeps the generated `description`, `default` and `x-scp-*` unless it sets them; it can only tighten validation (a key the Go struct lacks still fails at assign). `08-catalog` ships a hand-written schema for `catalog:`; it plugs in here, and the unit may keep the file as the source of truth.
- `Reference(dst, defaults)` prints the Markdown table (key, kind, default, environment variable, description, plus bounds) that becomes `docs/reference/config.md`; an `x-scp-owner` column maps keys to units in the committed version.
- **`scp config` subcommands** (implemented in `cmd/scp`, each a few lines over the package): `scp config lint [--config f] [--strict-env]` (= `Load` with `SkipSecrets`: validates the file, secret *references* and every shipped example without reading secret files; exit 2 on violations), `scp config schema`, `scp config reference`, `scp config env` (the variable list) and `scp config show` (the effective configuration with every secret `[REDACTED]` and, per key, the layer that set it: `default`, `file`, `env`, `flag`). Every shipped example under `examples/` is linted in CI (GR-12).

#### 4.3.9 Documented keys equal consumed keys (GR-12, GR-04, architecture 16)

`cmd/scp/consumed_test.go` loads the module with `golang.org/x/tools/go/packages` (`Tests: false`), finds the root `Config`, walks it to its **leaf fields** (secrets and text leaves are leaves; structs, lists and maps are walked), and records every field that is the target of a selector expression (`sel.Obj().(*types.Var)`) in any non-test, non-generated file **outside** `Validate` methods. A leaf that is never selected is reported as `config key X is documented but no code reads it`. A short allow-list with a reason per entry covers the genuinely loader-consumed keys (`version`). Prototyped 2026-10-14 against the platform sections: it correctly reported the four keys the prototype's miniature `main` did not wire (`ratelimit.sse_per_ip`, `shutdown.store_timeout`, `telemetry.otlp.headers_file`, `version`), and only those; reading a key inside `Validate` does not count as consuming it. Cost: about 3-8 s in CI (type-checks the module once). It is a **test dependency of `cmd/scp` only** (Deviation D8). Limits (stated, accepted): a key read through reflection or copied wholesale (`json.Marshal(cfg.X)`) looks consumed; a key read only in a test is correctly flagged.

#### 4.3.10 Errors of this package

| Condition | Result |
|---|---|
| declaration error (undocumented key, bad name, colliding env name, unsupported type, bad `Validate`) | `Load`/`Describe` return an error listing all of them; **this is a programmer error and fails the unit test of the declaring package** (each unit calls `config.Describe(&Config{})` in a test) |
| unreadable file | `config file: <os error>` (exit 1; not a violation) |
| any layer or schema or validator problem | `*config.Error` with every violation (exit 2) |
| secret file unreadable/empty/too large | a violation at the key's path (after validation, before validators) |
| unknown `SCP_*` variable | warning in `Result.Warnings`, never an error |

### 4.4 `telemetry`

`New(ctx, Config, BuildInfo, Options)` builds, in this order:

1. **Resource**: `service.name`, `service.version`, `service.instance.id` (semconv v1.40.0). No `OTEL_RESOURCE_ATTRIBUTES` (`resource.WithFromEnv` is not used).
2. **Meter provider** with a `View` that gives every histogram of unit `s` the `DefaultBuckets` (or `Options.LatencyBuckets`), and these readers: the **Prometheus exporter** when `telemetry.prometheus.enabled` (`WithRegisterer` on a private registry, `WithoutScopeInfo()`, `WithoutTargetInfo()`, so metric names and labels pass through exactly as the architecture lists them), and a 30 s **periodic OTLP/HTTP reader** when `telemetry.otlp.enabled` and `signals.metrics`.
3. **Tracer provider**: `ParentBased(TraceIDRatioBased(traces.sample_ratio))`, batch exporter to OTLP/HTTP when enabled and `signals.traces`. No exporter means spans are created and dropped (cheap, and `trace_id` still reaches logs).
4. **Logger provider** only when `signals.logs`; `cmd/scp` bridges it with `otelslog` into a tee handler beside `logx`. Off by default (logs go to stdout; shipping them is the platform's job).
5. **`scp_build_info`** (observable gauge, value 1, labels `version`, `commit`, `go`) and, when `telemetry.runtime.enabled`, the OTel **runtime instrumentation** (`go.memory.used`, `go.goroutine.count`, `go.processor.limit`, GC goal, ...: names as of contrib/instrumentation/runtime v0.72.0).
6. **`MetricsHandler()`**: `promhttp.HandlerFor(registry, ...)`. `cmd/scp` mounts it on the admin mux at `telemetry.prometheus.path` only when enabled.

**Prometheus name fidelity** (verified 2026-10-14 by a compiled test, `proto/promnames` and `telemetry_test.go`): counters named `scp_*_total` stay `scp_*_total` (no `_total_total`); histograms with unit `s` named `scp_*_seconds` render `scp_*_seconds_bucket|sum|count`; up-down counters render as gauges; observable gauges keep their names; `scp_build_info{commit,go,version} 1`; otelhttp's `http.server.request.duration` renders `http_server_request_duration_seconds`. The rule for every unit: **name the instrument exactly as the Prometheus series is wanted** and give histograms unit `"s"`.

**OTLP and the environment.** The OTLP exporters read `OTEL_EXPORTER_OTLP_*` from the process environment inside the SDK. That would let a sidecar injector or a stray variable redirect telemetry or add credentials, bypassing the "no `os.Getenv` outside `cmd/scp`" rule. Every setting is therefore passed explicitly: endpoint (`WithEndpointURL(endpoint + "/v1/<signal>")`), timeout, **compression (gzip)**, headers (only from `Options.Headers`), HTTP client (`Options.HTTPClient`, a netguard client). Verified 2026-10-14 (`env_test.go`): with `OTEL_EXPORTER_OTLP_ENDPOINT`, `..._TRACES_ENDPOINT`, `..._HEADERS`, `..._COMPRESSION=none`, `OTEL_TRACES_SAMPLER=always_off` and `OTEL_SDK_DISABLED=true` set, the export still went to the configured endpoint, carried only the configured `Authorization`, was gzip-compressed and was sampled by the configured sampler. (Found while testing: with compression left unset the environment **did** win, `Content-Encoding: gzip` came from `OTEL_EXPORTER_OTLP_COMPRESSION`; hence the explicit option.) `OTEL_EXPORTER_OTLP_CERTIFICATE`/`..._INSECURE` are neutralised by always passing an `HTTPClient` from `cmd/scp` when OTLP is enabled.

**OTLP destination policy.** The collector is operator-configured but often on a private address. `cmd/scp` builds `netguard.NewClient` with `netguard.NewPolicy(telemetry.otlp.allow_private_cidrs, telemetry.otlp.ports)` and a `Hosts` pin of the endpoint host, so a private collector needs an explicit allow-list (for example `["10.0.0.0/8"]`, ports `[4318]`) exactly like a private Jenkins (14) or GHES (05).

**Endpoint shape.** `telemetry.otlp.endpoint` is a *base* URL: `Validate` refuses userinfo, a query and a fragment (a credential in the URL would end up in SDK error messages and logs; it belongs in `headers_file`), and `New` trims trailing slashes before appending `/v1/<signal>`. When `New` fails half way it shuts down every exporter and provider it had already built (no exporter goroutine outlives a failed `New`).

**Credentials.** `telemetry` is rank 0 and cannot import `secret`; the OTLP credential is therefore a **headers file** (`telemetry.otlp.headers_file`, absolute path, one `Name: value` per line, `#` comments, 16 KiB cap) parsed by `cmd/scp` into `Options.Headers` and registered with `logx.Options.Scrub`-style value scrubbing by wrapping each value in a `secret.Secret`. The file is mounted like any other secret. (Deviation D5.)

**Headers-file grammar** (it was unspecified in the first draft; `cmd/scp` implements it, `FuzzParseHeadersFile`): UTF-8, lines ended by LF or CRLF; blank lines and lines whose first non-space character is `#` are skipped; every other line is `Name: value` split at the first colon, the name trimmed and required to be an HTTP token (`^[!#$%&'*+.^_`|~0-9A-Za-z-]+$`, case-insensitive, stored canonicalised), the value trimmed and non-empty; a value containing NUL, CR or LF is impossible by construction and a non-printable byte is refused. Errors, all collected with line numbers and **never echoing a value**: a line without a colon, a bad name, an empty value, a **duplicate name** (case-insensitive: silently merging two `Authorization` lines would be a credential decision nobody made), a file larger than 16 KiB (`secret.MaxFileSize`-style cap, checked while reading), more than 32 headers. A parse error is reported like a configuration violation at `telemetry.otlp.headers_file` (exit 2). Every parsed value is wrapped in a `secret.Secret` for `logx.Options.Scrub` (values shorter than 8 bytes are not scrubbed, as everywhere).

**Shutdown.** `Shutdown(ctx)` shuts the tracer provider first (flushes the batch), then the meter provider (final export), then the logger provider, joins errors, and is called from the telemetry stage's `Close` with its own context (`shutdown.telemetry_timeout`), so a shutdown remains observable to the end.

### 4.5 `httpx`

#### 4.5.1 `NewServer`

```go
func NewServer(addr string, h http.Handler, t TimeoutsConfig, errLog *slog.Logger) *http.Server
// ReadHeaderTimeout t.ReadHeader (5s)  ReadTimeout t.Read (30s)  WriteTimeout t.Write (60s)  IdleTimeout t.Idle (120s)
// MaxHeaderBytes int(t.MaxHeaderBytes) (16 KiB)  MaxHeaderValueCount t.MaxHeaderValues (100; new in Go 1.27, verified with go doc 2026-10-14)
// ErrorLog: slog.NewLogLogger(errLog.Handler(), slog.LevelWarn)   // net/http's own lines (handshake noise) go through logx, never to stderr raw
```

SSE and other streaming handlers extend their own write deadline per response with `http.ResponseController.SetWriteDeadline` (architecture 11.1); `httpx`'s writers implement `Unwrap()` and `Flush()` so `ResponseController` reaches the real connection through every wrapper.

#### 4.5.2 `Serve(ctx, srv, ln, grace)`

Runs `srv.Serve(ln)` in a goroutine. On `ctx.Done()` calls `srv.Shutdown` with a context derived from `context.WithoutCancel(ctx)` and `grace` as deadline; if that fails (grace exceeded) calls `srv.Close()` and returns the deadline error. Returns `nil` after an orderly stop, the serve error otherwise.

`ServeAll(ctx, grace, listeners...)` runs `Serve` for every `Listener{Srv, Ln}` concurrently with one shared grace and returns when all have stopped. If one fails on its own the others are stopped the same way (graceful, same grace) and the errors are joined with the failing one first. **There is one `servers` stage, not one stage per listener** (architecture 10.4 speaks of "the servers stage"): with two stages the stops would be sequential and the worst case would double (a verification finding, Appendix C). It is the body of the lifecycle servers stage and exists so `httpx` need not import `lifecycle`:

```go
lifecycle.Stage{Name: "servers",
    Run:         func(ctx context.Context) error { return httpx.ServeAll(ctx, cfg.Shutdown.ServerTimeout, httpx.Listener{Srv: pubSrv, Ln: pubLn}, httpx.Listener{Srv: admSrv, Ln: admLn}) },
    Drain:       func(context.Context) error { ready.SetShuttingDown(); return nil }, // /readyz 503, /livez stays 200, SSE told retry: 30000
    StopTimeout: cfg.Shutdown.ServerTimeout + time.Second}
```

Verified: `TestServeGracefulStop` starts a request that blocks, cancels the context, releases the handler and asserts the client received its full response and `Serve` returned `nil`; `TestServeAllStopsTogetherAndOneFailureStopsAll` asserts that both listeners stop on cancellation and that a listener dying by itself stops the other and is reported.

#### 4.5.3 The public chain (outermost first)

```text
requestID -> accessLog (its writer also applies the default security headers) -> recover -> otelhttp -> bodyLimit -> clientIP -> [AfterClientIP...] -> rateLimit -> crossOrigin -> routeCapture -> mux
```

1. **requestID**: a fresh **UUIDv4** (`uuid.NewV4()`, stdlib `uuid` package of Go 1.27, verified present 2026-10-14) stored with `logx.WithRequestID` and set as the response header `X-Request-Id` **before** anything else can write. An inbound `X-Request-Id` is **never adopted** (a caller could inject a log-forging or colliding id); a well-formed one (`^[A-Za-z0-9._:-]{1,64}$`) is logged as `inbound_request_id` so a client can still correlate its own id. The id is a UUID so `httpapi` renders `instance: urn:uuid:<id>` (06 D4). (Deviation D11: the architecture said `crypto/rand.Text`.)
2. **accessLog**: after the response, one record `msg:"http request"` with `method`, `route` (the matched **pattern**, `unmatched` for none), `status`, `duration_ms`, `bytes`, `client_ip`, `request_id` (via context), `inbound_request_id`; level `error` for 5xx, `debug` for `Class.Quiet` routes (probes), `info` otherwise. **Never the raw path or the query** (GR-13): pattern strings are a bounded set, raw paths are not (`/api/v1/repos/<slug>` would explode log cardinality and leak slugs of private repositories).
3. **recover**: sits *inside* the access log (architecture 11.1 lists recover before the access log; with that order a panic skips the access log entirely, so the one request an operator most wants a line for has none. Deviation D3). On panic: re-panic `http.ErrAbortHandler`; log `panic in handler` with the panic value and `debug.Stack()` at error; if the response has not started, call `Hooks.Recovered(w, r, rec)` with a copy of the request whose `Pattern` is the matched pattern (06's `Recovered` logs the route pattern and renders the 500 `internal` problem with the request id in `instance`); if it has started, nothing more can be sent and the connection is cut by the server. The hook writes through the access log's writer, so its 500 receives the class's default headers (item 8).
4. **otelhttp** (`otelhttp.NewHandler(next, "http.server", ...)`) with the injected meter/tracer providers and propagator, `WithPublicEndpointFn(true)` (**an inbound `traceparent` becomes a span *link*, never the parent**: callers are anonymous and could otherwise join or inflate our traces; tested), `WithFilter` skipping `Class.Quiet` (probes produce no spans and no metric series), and a span-name formatter returning the method. Metrics: the semconv server metrics (`http.server.request.duration` etc.).
5. **bodyLimit**: `http.MaxBytesReader(w, r.Body, n)` with `n = Class.BodyLimit` or `Deps.BodyLimit` (1 MiB). The webhook route's class carries 26 MiB (04 owns the 25 MiB effective limit and the 4-slot body semaphore), `/csp-reports` 64 KiB. `httpx.ErrBodyTooLarge(err)` lets a handler map `*http.MaxBytesError` to 413.
6. **clientIP**: resolves the client (4.5.4), stores it in the shared request state (for the access log, which sits outside) and in the context with `logx.WithClientIP`.
7. **AfterClientIP**: caller-supplied middlewares. `access.Middleware` goes here (14, 4.7.2): identity is decided from the TCP peer before anything is rate limited or answered.
8. **default security headers** (no longer a separate layer: a verification finding). The writer created by `accessLog` (the outermost wrapper, the one that records status and bytes) applies the class defaults at the **first final `WriteHeader` or `Write`** for every header **the handler has not already set** (lazy and only-if-absent, so `webapp` can set its own document CSP and `httpapi` its own `Cache-Control` without a race or a fight), and once more after the handler returned if it wrote nothing (net/http then sends an implicit 200 with whatever is in the header map). A 1xx interim response (103 Early Hints) is passed through and does not consume the final status or trigger it. Because the writer sits outside `recover` and every other layer, **every** response carries the headers: a recovered 500, a 429, a CSRF 403 and anything an `AfterClientIP` middleware writes. (In the first draft the header writer sat inside `recover`, so the one response an operator most wants hardened, the recovered 500, had none; `TestRecoveredAndSilentHandlersAndInterimResponsesKeepHeaders` and `TestChainRecoversPanicWithRequestID` pin it.) The class comes from `Deps.Classify`, memoised per request (one private-mux lookup, not five). HSTS is added when `Deps.HSTS` and the request is HTTPS (`r.TLS != nil`, or `X-Forwarded-Proto: https` **from a trusted peer**).
9. **rateLimit**: `Class.RateClass` names a `Limiter`; the key is `RateKey(clientIP)` (IPv6 bucketed to its /64, so one host cannot rotate through 2^64 addresses). On rejection `Hooks.RateLimited(w, r, limiter.RetryAfter(key))` renders the 429 problem with `Retry-After`; the `scp_ratelimit_rejected_total{class}` increment happens **inside `Limiter.Allow`** (section 4.5.5), so the webhook's invalid-signature limiter is counted identically without a bridge. No `RateLimit-*` headers (the IETF draft is unstable, architecture 17.4).
10. **crossOrigin**: `http.CrossOriginProtection` (non-safe methods only; `Sec-Fetch-Site`, else `Origin` against `Host`), with the **public URL's origin as a trusted origin** (a proxy that rewrites `Host` would otherwise lock browser POSTs out), `AddInsecureBypassPattern` for each `Deps.CSRFBypass` pattern (`POST /webhooks/github`, `POST /webhooks/github/{source}`, `POST /csp-reports`: no browser, no cookie, authenticated by HMAC or deliberately open), and `SetDenyHandler(Hooks.DenyCrossOrigin)` (403 `forbidden`).
11. **routeCapture** (innermost): after the mux returns it publishes `r.Pattern` to the request state (for the access log and recover hook), as the `http.route` attribute and span name, and into the otelhttp `Labeler` (so the server metrics carry `http.route`). **Why it exists** (found 2026-10-14 by the first test): `ServeMux` sets `r.Pattern` on the `*http.Request` it receives; every middleware above that calls `r.WithContext` hands the mux a copy, so the outer layers (access log, otelhttp) never see the pattern. Without this wrapper the access log said `route: unmatched` and the metrics had no route label. Runs in a `defer` so it also works when the handler panics.

The **admin chain** is `requestID -> accessLog (fixed API-class headers) -> recover -> bodyLimit -> clientIP -> [hostGuard] -> [bearer] -> routeCapture -> mux`: no tracing of operator calls, no rate limit, no CSRF layer (mutating admin routes require `Content-Type: application/json` and `X-SCP-Request` in their own handlers, architecture 17.1), headers of the API class on every response (including the SPA shell and static files of `/status`: `webapp` overrides the headers it owns, only-if-absent). When `server.admin.bearer_token` is set every request except the probes (`Class.Quiet`: `/livez`, `/readyz`, unauthenticated on the public listener too, so nothing more is exposed and a kubelet probe on 9091 keeps working) must carry `Authorization: Bearer <token>` (scheme compared case-insensitively as RFC 9110 requires, token with `Secret.EqualString`, constant time); failure is 401 with `WWW-Authenticate: Bearer realm="scp-admin"` through `Hooks.Unauthorized` (or the built-in minimal `application/problem+json` when nil). When `server.admin.allowed_hosts` is non-empty a request whose `Host` is not in the list (case-insensitive, with or without the port) is answered `421` with a minimal problem before anything else runs: **DNS rebinding** (an attacker's page whose name is re-pointed at the admin address would otherwise be same-origin for `/debug/pprof` and the mutating admin routes, bypassing the `X-SCP-Request` preflight rationale of 17.1) carries the attacker's host name. Probes are exempt. The list is empty by default (a Kubernetes `Service` name, a pod IP and a port-forward all produce different `Host` values, so a default would break scrapes); the open-admin warning (6.2) names it. The admin listener has **no identity logic** (ADR-025): `access.Middleware` is never installed on it.

#### 4.5.4 Client IP resolution (the one rule)

```text
peer := parse(r.RemoteAddr)  // host:port or bare; IPv4-mapped IPv6 unmapped; zone dropped; unparsable -> invalid
if peer invalid or peer not in trusted: return peer, viaProxy=false          // X-Forwarded-For from an untrusted peer is ignored entirely
hops := every X-Forwarded-For header line split on "," (lines merged in order), trimmed
cur := peer
for i := len(hops)-1; i >= 0; i--:                                           // right to left
    a, err := parse(hops[i]); if err != nil: return cur, true               // garbage ends the walk at the nearest parsed hop
    cur = a (unmapped, zone dropped)
    if a not in trusted: return a, true                                      // the first untrusted hop is the client
return cur, true                                                             // every hop trusted: the left-most
```

`ParseTrusted` refuses `/0` of either family and any unparsable entry; a bare address is a /32 or /128. `HTTPS(r)` trusts `X-Forwarded-Proto` only from a trusted peer. `access` does **not** use this (it reads `r.RemoteAddr` itself and has its own, stricter `access.trusted_proxies`, 14 section 4.7.3: the two lists are different trust statements and neither defaults to the other; `server.public.trusted_proxies` here is about believing `X-Forwarded-For`/`-Proto`, `access.trusted_proxies` about believing a *user identity*). Fuzzed (`FuzzClientIP`): the result never has a zone or a mapped form, never panics. **Limit (stated)**: a hop written with a port (`203.0.113.7:51234`, which some proxies such as Azure Application Gateway append) or in brackets does not parse, so the walk stops at the nearest parsed hop, which is the proxy: every user behind such a proxy shares one rate-limit bucket and one logged address. The conservative failure (fail towards the proxy, never towards a value the client chose) is deliberate; an operator behind such a proxy configures it to send a clean `X-Forwarded-For`.

#### 4.5.5 Keyed limiter

`Limiter` = 16 shards, each a mutex and a `map[string]*entry{*rate.Limiter, last}` plus an **overflow bucket**. Per-key cap = `MaxKeys/16`; when a shard is full an unseen key is served by the shard's overflow limiter (same rate and burst) **without** allocating, so an attacker rotating keys gets at most one overflow bucket's budget per shard, not a fresh bucket per request (tested: 5,000 rotating keys on a tiny cap produced at most `2 x shards` allowances). `Allow(key)` consumes a token (`rate.Limiter.AllowN(time.Now(), 1)`; `time.Now()` is virtualised by `testing/synctest`) and, when it refuses and `LimiterOptions.Rejected` is set, adds 1 to `scp_ratelimit_rejected_total{class=Class}` after releasing the shard lock (`TestLimiterCountsItsOwnRejections`). Every class uses the same counter instrument (OTel returns the one instrument for a repeated identical creation). `RetryAfter(key)` = one token interval, at least 1 s. `Sweep()` evicts entries idle longer than `IdleTTL`; `Run(ctx)` calls it every `IdleTTL/4` (at least 1 s) and is a lifecycle worker stage ("rate-limiter sweeper", architecture 10.3). Memory is bounded: the default 50,000 keys at roughly 150-250 B each (limiter, entry, map slot) is on the order of 10 MB.

**Behaviour at the key cap (residual risk, stated).** When a shard is full an unseen key shares the shard's overflow bucket. That bounds memory and never fails open, but it also means that an attacker who can present more distinct keys than `max_keys` (an IPv6 /48 holds 65,536 /64 buckets; botnets hold more IPv4 addresses) makes *new* legitimate clients share one small bucket, so some are answered 429 until entries age out (`idle_ttl`). The signal is `scp_ratelimit_keys{class}` approaching `ratelimit.max_keys`; raise the cap (about 200 B per key) or put a network-level limiter in front. Not fixable inside a per-process limiter without failing open.

**Webhook invalid-signature budget** (04 section 4.14): the webhook calls `limiter.Allow(clientIP)` **only after** signature verification failed; a false return upgrades the 401 to 429 `rate-limited`. A valid signature is **never** limited (nothing is blocked before verification: an IP shared with GitHub's delivery addresses behind a misconfigured proxy must not lock real deliveries out). `Peek` was prototyped and removed: no consumer needs it.

#### 4.5.6 Header classes (golden-tested)

See section 7.3 for the exact strings per class.

### 4.6 `netguard`

**Policy.** `NewPolicy(allowCIDRs, ports)`: every CIDR must parse and be narrower than `/0`, every port 1..65535; **all** errors are returned (`errors.Join`). `Control()` returns a `net.Dialer.Control` hook that runs **after DNS resolution, at connect time, on the actual socket address** (so DNS rebinding and redirects to internal names cannot bypass it):

1. If the address is in `alwaysDenied` return `ErrMetadataAddress` **even if an allow-list covers it** (an operator who allow-lists `10.0.0.0/8`, `100.64.0.0/10` or `fc00::/7` must not thereby expose a metadata service). The list is the **whole of the link-local ranges** `169.254.0.0/16` and `fe80::/10` (where AWS, GCP, Azure, OpenStack, DigitalOcean and Oracle `169.254.169.254`, AWS ECS `169.254.170.2`, Tencent `169.254.0.23` and OpenStack `fe80::a9fe:a9fe` live; the first draft listed single addresses and would have let an allow-listed `169.254.0.0/16` expose Tencent's) plus the metadata addresses that sit inside ranges an operator may legitimately allow: `100.100.100.200/32` (Alibaba, inside CGNAT), `168.63.129.16/32` (Azure wire server), `fd00:ec2::254/128` (AWS IMDS over IPv6, inside `fc00::/7`) and `fd20:ce::254/128` (GCE over IPv6; recalled from Google's documentation, **not re-verified on 2026-10-14**: a web search did not surface it, and denying it costs nothing). `TestWholeLinkLocalAndEveryMetadataAddressStayDeniedEvenWhenAllowListed`.
2. Else `ssrf.Guardian.Safe(network, address, c)` of `code.dny.dev/ssrf` v0.3.0: denies loopback, link-local, private, CGNAT, multicast, documentation, benchmarking, 6to4/Teredo/NAT64 and other reserved ranges; allows ports 80 and 443 plus `ports`; the allow-lists lift the deny for exactly the listed prefixes. IPv4-mapped IPv6 addresses are unmapped before comparison (tested).
3. `netguard.PrivateNetworks()` returns `10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 100.64.0.0/10, fc00::/7` for an integration that means "lives on a private network" (05's `network.allow_private_addresses: true`). It does not include loopback, link-local or any metadata address (tested).

**`NewClient(ClientOptions)`** returns an `*http.Client` over a fresh `*http.Transport` (never `DefaultTransport`): `Dialer{Timeout: DialTimeout, Control: policy.Control()}`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`, `MaxResponseHeaderBytes` (64 KiB), TLS 1.2 minimum, HTTP/2 allowed, bounded idle pool (16 total, 4 per host, 90 s). **`Proxy` is nil unless `ProxyURL` is set**: no `HTTP_PROXY`/`HTTPS_PROXY` from the environment, and with a nil proxy the dialed address is the destination, so the connect-time check sees what it must; a client with an operator proxy is never used for attacker-influenced URLs (the proxy hop is what the check sees then). `Timeout` is the whole-exchange deadline (default 30 s; **negative = none**, for 05's clients that apply per-request contexts). Redirects: `MaxRedirects` 0 (default) returns the 3xx response itself (`http.ErrUseLastResponse`), N follows up to N hops, each hop passing the same RoundTripper checks and the same connect-time policy. `TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault` pins both defaults (with `HTTP_PROXY` set to a live listener nothing reaches it; a 302 is returned, not followed) and `TestHostnameResolvingToLoopbackIsBlockedAtConnectTime` the rebinding property (a *name* that resolves to loopback is refused by the dialer, not by any static check).

The RoundTripper (`guarded`) refuses **before dialing**: a scheme other than https (http only with `AllowHTTP`), userinfo in the URL, an IP-literal host when `RefuseIPLiterals`, a host not in `Hosts` (pinned list, lower case, `host` or `host:port`, checked on every redirect hop), each wrapped in `ErrRefused`. `Wrap` lets `cmd/scp` put `otelhttp.NewTransport` around it. Credentials are never forwarded across hosts because a pinned client cannot reach another host; features that make a shallow copy to install their own `CheckRedirect` (14) keep the same transport and therefore the same guarantees.

**`ValidateURL(raw, allowHTTP, policy)`** is the *static* twin used by config validation: absolute URL, https (or http when allowed), no userinfo, an allowed port, and for an IP-literal host the same address check as the dialer (`tcp4`/`tcp6` passed to the guardian; plain `tcp` is rejected by the library, found 2026-10-14). Hostnames are not resolved at validation time (that would make startup depend on DNS); they are checked at connect time.

**Testing.** `httptest` servers listen on loopback, which the default policy blocks, so tests use a policy that allows `127.0.0.0/8` with the server's port; the metadata and rebinding behaviour is tested by calling `Control()` directly with crafted addresses. A fake-GitHub test of 05 uses its bubble client instead.

### 4.7 `lifecycle`

`Run(root, Plan)`:

```text
validate: stage names non-empty and unique
for each stage with Run (START order): ctx_i, cancel_i := WithCancel(WithoutCancel(root)); go call(Run, ctx_i)     // the signal context never reaches a worker
select:
   root.Done()          -> cause = context.Cause(root)
   a stage's Run returned before shutdown (nil -> ErrStageExited; error; panic -> PanicError) -> that stage is "failed", its StageError{Phase:run} comes FIRST
shutdown:
   OnShutdown(cause)                       // cmd/scp: stop() so a second signal kills the process
   Drain hooks, reverse order, each bounded by StopTimeout                         // /readyz 503, SSE retry: 30000
   sleep DrainDelay                        // still serving
   for i from last to first:
        cancel_i
        unless stage i failed: wait for Run to return, bounded by StopTimeout     // else ErrStopTimeout, log, and SKIP Close of this stage
        Close(ctx with StopTimeout)        // only after Run was joined
   return errors.Join(...)                 // nil only when everything stopped cleanly
```

Rules worth restating: **a failed stage is not waited on** (it already returned), its error is first in the joined error; **a stage that does not stop in time gets `ErrStopTimeout` and its `Close` is skipped** (closing the SQLite handle under a live worker is the exact race this package exists to prevent; the process is exiting anyway: Deviation D12, the architecture's text says to call `Close` after the expiry); **every hook is bounded** (Drain and Close get `StopTimeout` too, default 10 s); panics in `Run`, `Drain` or `Close` become `*PanicError` with the stack and do not take the process down mid-shutdown; `Observe(stage, phase, d, err)` (`cmd/scp` maps `outcome`: `ok` when `err == nil`, `timeout` when `errors.Is(err, ErrStopTimeout)`, else `error`) is called after every `drain`, `stop` and `close` phase (never for `run`: `PhaseRun` only labels a `StageError`) so `cmd/scp` can export `scp_lifecycle_phase_seconds`; `Plan.Log` receives `stage started`, `shutdown begins` (with the cause), `shutdown complete`. A stage with only `Close` (telemetry, started first, stopped last) is legal.

Verified by `testing/synctest` tests (fake time, `-race -count=3`): ordered shutdown timings, a failed stage reported first and not joined, a stuck stage bounded by its timeout with `Close` skipped, an early `nil` exit (`ErrStageExited`), a panic (`PanicError`), and that a stage never sees the root cancellation directly (the stage context is only cancelled by `lifecycle`).

### 4.8 Error vocabulary (what each package returns)

| Package | Sentinel / type | Meaning | Caller action |
|---|---|---|---|
| `secret` | `ErrEmpty`, `ErrNotAbsolute` | empty value; relative path | config violation |
| `config` | `*Error{Violations}`; `Warnings` | every config problem; non-fatal findings | print and exit 2; log warnings |
| `httpx` | `ErrBodyTooLarge(err)` (predicate) | `*http.MaxBytesError` | 413 problem |
| `netguard` | `ErrRefused`, `ErrMetadataAddress` (`errors.Is`) | request refused before dialing / metadata address | permanent upstream error (04's classification: `Permanent`, never retried) |
| `lifecycle` | `ErrStopTimeout`, `ErrStageExited`, `*StageError`, `*PanicError` | see 4.7 | process exit code 1, errors printed |
| `telemetry` | wrapped constructor errors | exporter or provider could not be built | startup error (exit 1) |

---

## 5. Data

This unit owns **no table, no migration and no persistent state**. Inventory of what it reads and holds:

| Item | Where | Lifetime | Notes |
|---|---|---|---|
| configuration file | read once by `cmd/scp` (`os.ReadFile`), <= 1 MiB | startup | path from `--config`, else `SCP_CONFIG`, else none (defaults only) |
| secret files | read once by `Secret.Resolve`, <= 64 KiB each, regular files (symlinks followed) | startup | a rotation needs a restart (04's/05's own re-read excepted) |
| `telemetry.otlp.headers_file` | read once by `cmd/scp` (16 KiB cap), values wrapped in `secret.Secret` | startup | `Name: value` per line |
| `Limiter` state | memory, <= `ratelimit.max_keys` keys x 16 shards plus 16 overflow buckets | process | evicted after `idle_ttl`; lost at restart (a restart resetting rate budgets is acceptable) |
| Prometheus registry and OTel aggregation state | memory | process | cumulative counters reset at restart as Prometheus expects |
| context attributes | in `context.Context` | request / job | nothing stored |

Files **written** by this unit: none. (`scp config` subcommands print to stdout.)

---

## 6. Configuration

### 6.1 The complete top-level file structure and who owns each section

One typed, versioned YAML document. The root struct lives in `cmd/scp/config.go` and is **only composition**: each top-level key is a type owned by the unit that reads it, so a feature's keys live with its code (architecture 16). Defaults are the embedded `cmd/scp/default.yaml`, assembled from the defaults each unit documents. The structure below is the architecture's outline (section 16) **as amended by the unit designs written so far** (checked in `20-design/` on 2026-10-14); `*` marks a name that differs from the outline and is explained in section 14.

```yaml
version: 1                    # 02-platform  root           enum [1]; a future v2 is an explicit, versioned schema change
server:                       # 02-platform  httpx.Config
  public: { listen: ":9090", public_url: "", hsts: false, trusted_proxies: [] }
  admin:  { listen: ":9091", bearer_token: { file: ... }, allowed_hosts: [] }               # bearer_token and allowed_hosts are new *
  timeouts: { read_header: 5s, read: 30s, write: 60s, idle: 120s, max_header_bytes: 16KiB, max_header_values: 100 }
log:       { level: info, format: json, add_source: false }                                  # 02-platform  logx.Config
telemetry: { prometheus: true, otlp: { enabled: false, endpoint: "", headers_file: "", timeout: 10s,        # 02-platform  telemetry.Config
             allow_private_cidrs: [], ports: [], signals: { traces: true, metrics: true, logs: false } }, # otlp.protocol removed, HTTP only *
             traces: { sample_ratio: 0.1 }, runtime: { enabled: true } }
ratelimit: { badges: "120/min burst 60", api: "300/min burst 100", media_ext: "60/min burst 20",    # 02-platform  httpx.RateConfig
             csp_reports: "30/min burst 10", webhook_invalid: "10/min burst 10", sse_per_ip: 4, max_keys: 50000, idle_ttl: 10m }
shutdown:  { drain_delay: 5s, server_timeout: 15s, store_timeout: 3s, telemetry_timeout: 2s }   # 02-platform  lifecycle.Config (new section *; sized so that with refresh's 19 s the worst case is 44 s < 45 s)
site:      { title, description, nav, display_time_zone, default_theme, sitemap, robots }       # overview (feature unit not yet in 20-design)
store:     { path, sync, busy_timeout, read_conns, journal_size_limit, quarantine_keep,         # 03-readmodel-store  store/sqlite.Config (its section 6)
             probe_interval, backup: {...}, cache_caps: { blob, parse, locked } }
github:                       # shared mapping, composed in cmd/scp (6.1.1)
  defaults: { api_version, http: {...}, budget: {...}, webhook: { max_body, dedupe_ttl, dedupe_max, max_concurrent, acquire_wait, retry_after } }
  sources:  [ { id, host, network, auth, webhook: {...}, discovery: {...}, acquisition: {...} } ]   # 05-github-adapter + 04-refresh-webhook + 08-catalog
catalog:   { slug_prefix_by_source, heads: {...}, groups: [ ... ], ungrouped }                  # 08-catalog  catalog.Config
refresh:   { upstreams: { <name>: { concurrency, pace, job_timeout } }, backoff: {...}, grace, planner_tick, max_keys, gate: {...}, parking: {...} }   # 04-refresh-webhook section 6.1
readiness: { warm_max_age, min_coverage, core_kinds, startup_deadline }                         # ops (unit not yet in 20-design)
commits:   { keep, latest_n, refresh_ttl, idle_backoff_max }                                    # 09-commits-badges
docs:      { max_doc_bytes, max_docs_per_repo, max_resident_per_repo, resident_budget, bridge_asciidoc }   # docs (unit not yet in 20-design)
manifests: { enabled, allow_list, limits, pom_source, providers }                               # 11-manifests-core (providers.*: 13-manifests-ecosystems-b)
badges:    { palette, cache_max_age }                                                           # 09-commits-badges
integrations: { jenkins: { instances: [...], ttl, ... }, snyk: { enabled, token, org_id, ... } }  # 14-integrations-access
pages:     { dir, announcement: {...} }                                                         # pages (unit not yet in 20-design)
access:    { mode, trusted_proxies, allow_public_proxies, identity: {...}, view_all: {...} }    # 14-integrations-access
media:     { signing_key, allow_hosts, max_bytes }                                              # media (unit not yet in 20-design)
live:      { sse: { enabled } }                                                                 # live (unit not yet in 20-design; flag named by 07)
web:       { csp: { mode } }                                                                    # 06-httpapi  webapp.Config
```

#### 6.1.1 How `cmd/scp` composes shared mappings

Two kinds of seam, both supported by the machinery:

1. **A section owned by one unit** (`store`, `catalog`, `refresh`, `commits`, `integrations`, `access`, `web`, ...): `Store sqlite.Config \`yaml:"store" jsonschema_description:"..."\``. The owning unit's `Config` carries its own yaml/jsonschema tags and `Validate`. Nothing else is needed.
2. **A mapping owned by more than one unit** (`github`): `cmd/scp/config.go` declares *glue types* that embed each unit's piece with `yaml:",inline"`, so the generated schema, the environment names and the `Validate` fan-out all see **one mapping with several owners** (`x-scp-owner` names the package per key):

```go
// cmd/scp/config.go (shape; the exact split is for the units to agree, Question Q2)
type GitHub struct {
    Defaults GitHubDefaults `yaml:"defaults" jsonschema_description:"Settings applied to every source."`
    Sources  []GitHubSource `yaml:"sources"  jsonschema_description:"One entry per GitHub host or App."`
}
type GitHubDefaults struct {
    github.Defaults `yaml:",inline"`                                  // api_version, http, budget ... (05)
    Webhook webhook.Config `yaml:"webhook" jsonschema_description:"..."` // 04 section 6.2 mounts webhook.Config at github.defaults.webhook
}
type GitHubSource struct {
    github.Source `yaml:",inline"`  // id, host, network, auth, webhook.secret|previous_secret|public_url, discovery, acquisition (05)
    // keys that 04 or 08 own inside the same mapping (redelivery.*, discovery.reconcile_every, ...) are added the same
    // way, as further inline structs, once the owners have split them without overlap
}
```

The rule that makes this work without any unit importing another: **each unit declares the keys it owns, and `cmd/scp` is the only package that sees both**. When two units want the same sub-mapping (`github.sources[].webhook.redelivery.*` is tabulated by both 04 and 05, with different `max_per_run` defaults: 200 and 50; `github.defaults.budget.concurrency` of 05 versus `refresh.upstreams.github.concurrency` of 04 are two knobs for one limit), the machinery refuses the second declaration at startup and in the unit tests (`Describe`), which is the design intent: it forces the owners to agree (Question Q2) instead of shipping two settings for one thing (GR-12).

The root also owns the **root `Validate`** (cross-section rules; section 6.5) and the `--config`/`SCP_CONFIG` selection.

#### 6.1.2 Section ownership table

| Section | Go type (package) | Owner unit | Keys tabulated in |
|---|---|---|---|
| `version`, `server`, `log`, `telemetry`, `ratelimit`, `shutdown` | root, `httpx.Config`, `logx.Config`, `telemetry.Config`, `httpx.RateConfig`, `lifecycle.Config` | `02-platform` | 6.2 below |
| `site` | `overview.SiteConfig` | `15-live-ops-overview` | its section 6 (complete table: `90-config-reference.md`) |
| `store` | `store/sqlite.Config` | `03-readmodel-store` | its section 6 |
| `github.defaults`, `github.sources[]` | `github.Config` (05) + glue | `05-github-adapter` declares **every** key (CD-CF-04/07: the webhook admission keys and the `redelivery` keys that unit 04 tabulates, and the `discovery` keys that unit 08 consumes, are fields of 05's structs); `04` and `08` are consumers | 05 section 6 |
| `catalog` | `catalog.Config` | `08-catalog` | its schema `catalog.schema.json` |
| `refresh` | `refresh` config types | `04-refresh-webhook` | its section 6.1 |
| `readiness`, `ops` | `ops.ReadinessConfig`, `ops.Config` | `15-live-ops-overview` | its section 6 |
| `commits`, `badges` | `commits.Config`, `badges.Config` | `09-commits-badges` | its section 6 |
| `docs` | `docs.Config` | `10-docs-pages-media` | its section 6 |
| `manifests` | `manifests.Config` (11) + `ecosystems`, `enrich`, `providers[]` (13) | `11-manifests-core` (12 owns no key) | their configuration sections |
| `integrations`, `access` | `integrations.Config`, `access.Config` | `14-integrations-access` | its sections 6.1, 6.2 |
| `pages`, `media` | `pages.Config`, `media.Config` | `10-docs-pages-media` | its section 6 |
| `live`, `llms` | `live.Config`, `overview.LLMsConfig` | `15-live-ops-overview` | its section 6 (`llms` was missing from this table: CD-CF-01) |
| `web` | `webapp.Config` | `06-httpapi` | its section 6.1 |

Keys that other units tabulate **about this unit's sections** and how they resolve:

| Mentioned by | Mentions | Resolution |
|---|---|---|
| `06-httpapi` 6.2 | `server.public_listen`, `server.admin_listen` (flat) | the section is `server.public.listen` and `server.admin.listen` as in architecture 16 (nested); `06`'s table should use the nested names (Q4) |
| `04-refresh-webhook` 6.2 | `ratelimit.webhook_invalid` "rate, 10/min burst 10, `SCP_RATELIMIT_WEBHOOK_INVALID`" | exact match: `httpx.Rate` text type |
| `06-httpapi` 6.2 | `ratelimit.csp_reports`, `ratelimit.api` | exact match, same defaults (30/min burst 10; 300/min burst 100) |
| `14-integrations-access` 4.7.3 | `server.public.trusted_proxies` separate from `access.trusted_proxies` | same decision (section 4.5.4): two lists, never defaulted from each other |
| `01-kernel` RT-13 | `server.public.public_url` as the base of `routes.Absolute` | the key exists with the validation of 6.2 (origin only: no path, query, userinfo) |
| `04` 4.10 | stage `StopTimeout` of the refresh stage = `refresh.grace` | `refresh.grace` sizes the refresh stage; `shutdown.*` only covers servers, store, telemetry |

### 6.2 Platform-owned keys

Env names are `SCP_` + path; defaults are `cmd/scp/default.yaml`. "Consumer" is the code the documented-keys == consumed-keys test must find. The machine-generated twin of these tables is `02-platform.assets/out/reference.md` (and `schema.json`, `env.txt`).

**`version`**

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `version` | int | `1` | enum `[1]` (the schema reports an unsupported version before anything else) | `SCP_VERSION` | the loader (allow-listed in the consumption test); a future `2` is a new schema with its own release notes |

**`server`** (`httpx.Config`)

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `server.public.listen` | string | `:9090` | `host:port` with a **literal IP or empty host** (a name such as `localhost` is refused: it may resolve to several addresses); port 0..65535 | `SCP_SERVER_PUBLIC_LISTEN` | `cmd/scp` (`net.Listen`), `httpx.NewServer` |
| `server.public.public_url` | string | `""` | empty, or `http(s)://host[:port]` with **no** userinfo, path, query or fragment | `SCP_SERVER_PUBLIC_PUBLIC_URL` | `httpx.Deps.PublicURL` (CSRF trusted origin), `routes.Absolute` base (sitemap, llms.txt, Link headers, webhook URL hints), `webapp` |
| `server.public.hsts` | bool | `false` | | `SCP_SERVER_PUBLIC_HSTS` | `httpx.Deps.HSTS` (only on HTTPS requests; leave false when the TLS-terminating proxy owns HSTS) |
| `server.public.trusted_proxies` | []CIDR or address | `[]` | each parses; **`/0` of either family refused**; bare address = /32 or /128 | `SCP_SERVER_PUBLIC_TRUSTED_PROXIES` (comma list) | `httpx.ParseTrusted` -> `ClientIPResolver` |
| `server.admin.listen` | string | `:9091` | as above; must differ from `public.listen` (except `:0` in tests) | `SCP_SERVER_ADMIN_LISTEN` | `cmd/scp`, `httpx.NewServer` |
| `server.admin.bearer_token` | secret | unset | when set, every admin request except the probes must present it | `SCP_SERVER_ADMIN_BEARER_TOKEN` or `..._FILE` | `httpx.AdminChain` (new key, Deviation D5; GR-17 "optional bearer auth") |
| `server.admin.allowed_hosts` | []string | `[]` | each entry a host or host:port without space, `/` or `@` | `SCP_SERVER_ADMIN_ALLOWED_HOSTS` (comma list) | `httpx.Deps.AdminHosts` -> `AdminChain` Host guard (421 for any other `Host`; DNS-rebinding defence; new key, Deviation D5) |
| `server.timeouts.read_header` | duration | `5s` | 100ms..1m; must not exceed `read` | `SCP_SERVER_TIMEOUTS_READ_HEADER` | `http.Server.ReadHeaderTimeout` |
| `server.timeouts.read` | duration | `30s` | 1s..10m | `SCP_SERVER_TIMEOUTS_READ` | `ReadTimeout` |
| `server.timeouts.write` | duration | `60s` | 1s..10m | `SCP_SERVER_TIMEOUTS_WRITE` | `WriteTimeout` (SSE extends per response) |
| `server.timeouts.idle` | duration | `120s` | 1s..1h | `SCP_SERVER_TIMEOUTS_IDLE` | `IdleTimeout` |
| `server.timeouts.max_header_bytes` | size | `16KiB` | 1KiB..1MiB | `SCP_SERVER_TIMEOUTS_MAX_HEADER_BYTES` | `MaxHeaderBytes` |
| `server.timeouts.max_header_values` | int | `100` | 16..4096 | `SCP_SERVER_TIMEOUTS_MAX_HEADER_VALUES` | `MaxHeaderValueCount` (Go 1.27) |

Startup warnings from `cmd/scp` (not violations): the admin listener is bound to a non-loopback address **and** has no bearer token ("the admin listener is open to the network; restrict it by network policy, set `server.admin.bearer_token`, and set `server.admin.allowed_hosts` against DNS rebinding"); `public_url` is `http://` while `hsts` is true.

**`log`** (`logx.Config`)

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `log.level` | enum | `info` | `debug`, `info`, `warn`, `error` | `SCP_LOG_LEVEL` | `logx.ParseLevel` in `cmd/scp` |
| `log.format` | enum | `json` | `json`, `text` | `SCP_LOG_FORMAT` | `logx.Options.Format` |
| `log.add_source` | bool | `false` | | `SCP_LOG_ADD_SOURCE` | `logx.Options.AddSource` |

**`telemetry`** (`telemetry.Config`)

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `telemetry.prometheus` | bool | `true` | | `SCP_TELEMETRY_PROMETHEUS` | `telemetry.New` (reader), `cmd/scp` (mounts `admin.metrics` `GET /metrics`: **fixed path**, the route table owns every URL) |
| `telemetry.otlp.enabled` | bool | `false` | | `SCP_TELEMETRY_OTLP_ENABLED` | `telemetry.New` |
| `telemetry.otlp.endpoint` | string | `""` | required when enabled; `http(s)://host[:port]` (base URL; `/v1/{traces,metrics,logs}` is appended) | `SCP_TELEMETRY_OTLP_ENDPOINT` | exporters |
| `telemetry.otlp.headers_file` | path | `""` | absolute when set; <= 16 KiB; `Name: value` per line | `SCP_TELEMETRY_OTLP_HEADERS_FILE` | `cmd/scp` parses it into `Options.Headers` (secret-wrapped) |
| `telemetry.otlp.timeout` | duration | `10s` | 1s..2m | `SCP_TELEMETRY_OTLP_TIMEOUT` | exporters |
| `telemetry.otlp.allow_private_cidrs` | []CIDR | `[]` | narrower than /0 | `SCP_TELEMETRY_OTLP_ALLOW_PRIVATE_CIDRS` | `cmd/scp` -> `netguard.NewPolicy` for the exporter client |
| `telemetry.otlp.ports` | []int | `[]` | 1..65535 | `SCP_TELEMETRY_OTLP_PORTS` | same |
| `telemetry.otlp.signals.traces|metrics|logs` | bool | `true`, `true`, `false` | at least one when `enabled` | `SCP_TELEMETRY_OTLP_SIGNALS_TRACES|METRICS|LOGS` | `telemetry.New` |
| `telemetry.traces.sample_ratio` | float | `0.1` | 0..1 | `SCP_TELEMETRY_TRACES_SAMPLE_RATIO` | `ParentBased(TraceIDRatioBased)` |
| `telemetry.runtime.enabled` | bool | `true` | | `SCP_TELEMETRY_RUNTIME_ENABLED` | `runtime.Start` |

**`ratelimit`** (`httpx.RateConfig`; rates are text leaves `N/s|min|h [burst M]`, burst defaults to N, both at least 1, burst at least one second of refill)

| Key | Type | Default | Env | Consumer |
|---|---|---|---|---|
| `ratelimit.badges` | rate | `120/min burst 60` | `SCP_RATELIMIT_BADGES` | limiter class `badges` (`GET /badges/*`) |
| `ratelimit.api` | rate | `300/min burst 100` | `SCP_RATELIMIT_API` | class `api` (`/api/v1/*`, and the server-text routes `/repos/*.md`, `llms*`, `dependencies.md`, `sitemap.xml`, `robots.txt`, `/schemas/*`: design choice, Q6) |
| `ratelimit.media_ext` | rate | `60/min burst 20` | `SCP_RATELIMIT_MEDIA_EXT` | class `media_ext` (`/media/ext/*`, the external image proxy) |
| `ratelimit.csp_reports` | rate | `30/min burst 10` | `SCP_RATELIMIT_CSP_REPORTS` | class `csp_reports` (`POST /csp-reports`) |
| `ratelimit.webhook_invalid` | rate | `10/min burst 10` | `SCP_RATELIMIT_WEBHOOK_INVALID` | class `webhook_invalid` behind `webhook.Limiter` (04) |
| `ratelimit.sse_per_ip` | int 1..1000 | `4` | `SCP_RATELIMIT_SSE_PER_IP` | `live` (concurrent SSE streams per client IP; `httpx` supplies the IP only) |
| `ratelimit.max_keys` | int 1000..5,000,000 | `50000` | `SCP_RATELIMIT_MAX_KEYS` | `LimiterOptions.MaxKeys` per class |
| `ratelimit.idle_ttl` | duration 10s..24h | `10m` | `SCP_RATELIMIT_IDLE_TTL` | `LimiterOptions.IdleTTL` and the sweep interval |

**`shutdown`** (`lifecycle.Config`; the refresh stage's timeout is `refresh.grace` (04), not a key here)

| Key | Type | Default | Validation | Env | Consumer |
|---|---|---|---|---|---|
| `shutdown.drain_delay` | duration | `5s` | 0..2m; `Validate`: below `server_timeout + store_timeout + telemetry_timeout` | `SCP_SHUTDOWN_DRAIN_DELAY` | `lifecycle.Plan.DrainDelay` (architecture 8.16: 5 s, still serving) |
| `shutdown.server_timeout` | duration | `15s` | 1s..5m | `SCP_SHUTDOWN_SERVER_TIMEOUT` | `httpx.ServeAll` grace (both listeners together) and the servers stage's `StopTimeout` (+1 s); architecture 10.4: 15 s |
| `shutdown.store_timeout` | duration | `3s` | 1s..5m | `SCP_SHUTDOWN_STORE_TIMEOUT` | store stage `StopTimeout` (final plan flush, `Close`: optimize, `wal_checkpoint(TRUNCATE)`; if it overruns, WAL recovery covers it at the next open) |
| `shutdown.telemetry_timeout` | duration | `2s` | 1s..1m | `SCP_SHUTDOWN_TELEMETRY_TIMEOUT` | telemetry stage `Close` |

A container orchestrator's termination grace period (Kubernetes `terminationGracePeriodSeconds`) must exceed the **worst case** `Config.WorstCase(refreshStage.StopTimeout)` = `drain_delay + server_timeout + store_timeout + telemetry_timeout + the refresh stage's StopTimeout`. The reference manifest sets **45 s** (architecture 19, ADR-017, 04-decisions A-10; the first draft of this document claimed 90 s and defaults that summed to 55-75 s, which would have been SIGKILLed mid-shutdown), and unit 04 fixes the refresh stage at `Grace + AbandonAfter + 1 s` = 19 s and its own shutdown test at 42 s. With the defaults above: 5 + 15 + 19 + 3 + 2 = **44 s**, one second inside the budget (the servers stop concurrently in one stage; sequential stages would add 15 s). `cmd/scp` logs the figure at startup and warns when it exceeds 45 s (`TestWorstCaseOfTheDefaultsFitsTheReferenceTerminationGrace` pins the arithmetic); an operator who raises a timeout raises `terminationGracePeriodSeconds` with it.

### 6.3 Environment-variable rule and the generated list

The rule is section 4.3.5. The generated list for the platform sections (42 names; `_FILE` twin of the one secret included) is `out/env.txt`; the same generator emits the complete list for the whole tree (`scp config env`). Examples across units: `SCP_GITHUB_SOURCES_0_AUTH_KIND`, `SCP_GITHUB_SOURCES_0_AUTH_PRIVATE_KEY_FILE`, `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY`, `SCP_STORE_BACKUP_ENABLED`, `SCP_INTEGRATIONS_JENKINS_INSTANCES_0_BASE_URL`, `SCP_ACCESS_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16`.

### 6.4 Schema, lint and reference generation (committed artefacts)

| Artefact | Generator | Committed at | CI check |
|---|---|---|---|
| JSON Schema 2020-12 | `scp config schema` = `config.Schema(&Config{}, defaultYAML, overlays...)` | `docs/reference/config.schema.json` (also embedded and served for editors) | `make gen` then `git diff --exit-code` |
| Reference (Markdown table, all keys, owner column) | `scp config reference` | `docs/reference/config.md` | same |
| Environment list | `scp config env` | `docs/reference/env.txt` | same |
| Metrics reference | cmd/scp test that boots every constructor against an in-memory reader (architecture 18.2) | `docs/reference/metrics.md` | same |
| Lint of every example | `scp config lint` over `examples/*.yaml` | n/a | CI job, fails on any violation (GR-12) |

The schema is **not** hand-edited; the only hand-written JSON is an `Overlay` fragment, and a test asserts that each overlay's `Path` still exists in the generated schema.

### 6.5 Startup validation owned by the root `Validate` in `cmd/scp`

The architecture's list (16, "Startup validation refuses ...") split by implementer; each row is a test in `cmd/scp`:

| Rule | Implemented by |
|---|---|
| unknown key; wrong type; unknown `version` | `config` (schema) |
| `trusted_proxies` containing `0.0.0.0/0` or `::/0` (`server.public` here; `access` has its own check, 14) | `httpx.Config.Validate`, `httpx.ParseTrusted` |
| an empty secret where one is required; a webhook route without a secret | the unit that requires it (`github` for webhook secrets, 05 6.3) via `Validate`; `Secret.IsZero` |
| `public_url` not an origin; listen not `host:port`; admin = public listen | `httpx.Config.Validate` |
| a slug equal to a reserved word; duplicate slugs; unknown `site.display_time_zone`; `acquisition.mode: git` in a build without `gitsource` | `catalog`, `overview`, `github` (their `Validate`) |
| `visibility` private/installation_scope with `access.mode: public` | root `Validate` (needs `github` and `access`) |
| any operator URL that fails the SSRF policy; a Jenkins instance on a private address with no allow entry | the root `Validate` calls `netguard.ValidateURL(raw, allowHTTP, policy)` for each operator URL (Jenkins and Snyk `base_url`, GitHub host URLs, the OTLP endpoint) with the policy built from that unit's `allow_private_*` and `ports` keys; features themselves cannot (only `github` may import `netguard`), and static syntax checks stay in their own `Validate` |
| OTLP endpoint private without an allow-list | `telemetry.Config.Validate` (syntax) and the root `Validate` (`netguard.ValidateURL` against the OTLP policy) |

---

## 7. HTTP surface

### 7.1 Routes this unit owns

None. `platform/*` registers no pattern in the route table (`01-kernel`'s `routes.Table` is the only place URLs are named). It supplies handlers and behaviour that `cmd/scp` and the owning features mount:

| Route (table name) | Owner | Handler from this unit |
|---|---|---|
| `admin.metrics` `GET /metrics` (admin listener only) | `ops` | `telemetry.Provider.MetricsHandler()` (the path is fixed by the table; there is no `telemetry.prometheus.path` key). When `telemetry.prometheus` is false `cmd/scp` removes the route with `routes.Admin().Without("admin.metrics")` (01-kernel RT-15) before registration; merely not registering a handler would make `Table.Verify()` abort the start. |

Both servers answer `probe.live`/`probe.ready` (`/livez`, `/readyz`) per the table; `/readyz` is 503 from the first `Drain` hook.

### 7.2 Request classification (how `httpx` learns the route class without importing `routes`)

`httpx.Deps.Classify func(*http.Request) Class` is built in `cmd/scp` from the `routes.Table`: a private `*http.ServeMux` is populated with every table pattern and a no-op handler; `Classify(r)` calls `mux.Handler(r)` (cheap, allocation-free in the common case) to get the matched pattern, maps pattern to route name and route name to a `Class`. The mapping is **data** in `cmd/scp/classes.go` keyed by route class and name prefix, with a test that fails when a table route has no entry or an entry names no route:

| Route(s) (names from `01-kernel.routes.tsv`) | `RateClass` | `Headers` | `BodyLimit` | `Quiet` |
|---|---|---|---|---|
| `api.*` (public) | `api` | `api` | default (1 MiB) | |
| `spa.*`, `negotiated.group`, `negotiated.repo`, `doc.md` | | `html` | | |
| `assets`, `theme.init`, `favicon`, `manifest` | | `asset` | | |
| `badge`, `badge.group` | `badges` | `badge` | | |
| `media.repo` | | `media` | | |
| `media.ext` | `media_ext` | `media` | | |
| `readme.md`, `agents.md`, `changelog.md`, `negotiated.repo.rest`, `llms.*`, `deps.*.md`, `sitemap`, `robots`, `schemas` | `api` | `api` | | |
| `webhook`, `webhook.source` | | `api` | 26 MiB | |
| `csp.report` | `csp_reports` | `api` | 64 KiB | |
| `probe.live`, `probe.ready` | | `api` | | yes |
| admin listener routes (the 24 API operations of the admin table: the public ones answered with an anonymous principal, `api.status`, `api.admin.retry`; `admin.metrics`, `admin.pprof*`, `spa.status`, `spa.fallback`, the probes and the four asset routes; 01 RT-14) | (admin chain: no limiter, fixed class, `Classify` is used for `Quiet` only) | `api` | | `probe.*` yes |
| `frame.mermaid` (v1.1) | | its own class (`frame-ancestors 'self'`) | | |

The CSRF bypass patterns (`Deps.CSRFBypass`) are exactly the webhook patterns (`webhook`, `webhook.source`) and `POST /csp-reports`, derived from the same table entries so they cannot drift.

**Completeness is checked against `01-kernel.routes.tsv`** (every public and admin row; `TestRoutesClassComplete` fails when the file gains a route this table does not classify): the first draft of this table had no row for `negotiated.group`, `negotiated.repo` or `doc.md` (class `spa`, owner `overview`/`docs`), and `TestRoutesClassComplete` would have failed on day one. Two contracts follow for other units: (1) the Markdown branch of `httpapi.Negotiate` for `negotiated.*` and `doc.md` should set `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; sandbox` itself (only-if-absent lets it), otherwise a `text/markdown` response carries the HTML class's CSP-less set; (2) on the admin listener every response gets the `api` class, including the SPA shell and static files of `/status`, so `webapp` must set the document CSP and any header it needs different on those responses itself (it does, 06 4.10-4.11).

### 7.3 Header classes: exact strings (golden-tested in `TestHeaderGolden`)

Defaults are set at the first write **only if the handler has not set the header**. `P` below is the `Permissions-Policy` value `accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()` (identical to the one `webapp` sets, 06 4.10.3).

| Class | Headers (in order) |
|---|---|
| `html` | `X-Content-Type-Options: nosniff`; `Referrer-Policy: strict-origin-when-cross-origin`; `Permissions-Policy: P`; `Cross-Origin-Opener-Policy: same-origin`; `X-Frame-Options: DENY`. **No CSP**: the document CSP is `webapp`'s (06 4.11) and is set by it. |
| `asset` | `X-Content-Type-Options: nosniff` (a CSP or CORP on the SPA's own JS and CSS means nothing; 06 states the same) |
| `api` (JSON, `llms*.txt`, `*.md`, probes, admin) | `nosniff`; `Referrer-Policy`; `Permissions-Policy: P`; `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; sandbox`; `Cross-Origin-Resource-Policy: same-origin` |
| `badge` | `nosniff`; `Referrer-Policy`; `Permissions-Policy: P`; `Content-Security-Policy: default-src 'none'; sandbox`; `Cross-Origin-Resource-Policy: cross-origin` |
| `media` | `nosniff`; `Referrer-Policy`; `Permissions-Policy: P`; `Content-Security-Policy: default-src 'none'; sandbox`; `Cross-Origin-Resource-Policy: same-origin` |
| any class, HTTPS request, `server.public.hsts: true` | adds `Strict-Transport-Security: max-age=31536000` (no `includeSubDomains`, no `preload`: those are the proxy owner's decision) |

Never emitted: `Server`, `X-Powered-By`, `X-XSS-Protection`, any `Access-Control-*`, `Cross-Origin-Embedder-Policy`. The `Content-Security-Policy-Report-Only` rollout and `Reporting-Endpoints` are `webapp`'s.

### 7.4 Responses this unit's code produces

| Situation | Status | Body | Headers |
|---|---|---|---|
| rate limited | 429 | problem `rate-limited` rendered by `Hooks.RateLimited` | `Retry-After: <s>` (>= 1) |
| cross-origin unsafe request | 403 | problem `forbidden` by `Hooks.DenyCrossOrigin` | |
| handler panic before the response started | 500 | problem `internal` with the request id in `instance`, by `Hooks.Recovered` | |
| admin without the right bearer | 401 | problem `unauthorized` | `WWW-Authenticate: Bearer realm="scp-admin"` |
| body over the class limit | the handler's choice (413 problem `payload-too-large` by convention) | | `Connection: close` set by `MaxBytesReader` |
| every response | | | `X-Request-Id: <uuid>` |

`httpx` never caches, compresses, negotiates or CORS-handles anything; `Cache-Control`, `ETag`, `Gzip` and `Vary` belong to `httpapi` and `webapp`.

---

## 8. Concurrency and lifecycle

### 8.1 Goroutines owned by this unit

| Goroutine(s) | Owner | Started by | Stopped by | Notes |
|---|---|---|---|---|
| HTTP servers (+ per-connection goroutines) | the one `servers` stage | `lifecycle.Run` -> `httpx.ServeAll` (one goroutine per listener) | stage cancel -> `Server.Shutdown(grace)` on both in parallel, then `Close` | handlers use `r.Context()` |
| limiter sweeper (1) | `httpx.RunSweepers` | `lifecycle` worker stage `ratelimit-sweeper` | stage cancel | one ticker (a quarter of the shortest idle TTL, at least 1 s) sweeping every limiter class |
| OTel batch span processor, periodic metric reader, log batch processor | SDK, owned by `telemetry.Provider` | `telemetry.New` | `Provider.Shutdown` in the telemetry stage's `Close` | own context, not cancelled with the workers |
| `lifecycle` internals: one goroutine per stage `Run`, one per bounded hook | `lifecycle.Run` | `Run` | joined before `Run` returns (except a stuck stage, which is reported and abandoned: the process is exiting) | |
| runtime metrics callbacks | `contrib/instrumentation/runtime` | `runtime.Start` | meter provider shutdown | |

`config`, `secret`, `logx`, `netguard` start no goroutines (a `netguard` client's transport owns idle-connection goroutines, closed with the client's idle timeout).

### 8.2 Shared state and its protection

| State | Protection |
|---|---|
| `Limiter` shards (map + overflow bucket per shard) | one `sync.Mutex` per shard, held only for a map lookup and a token bucket call; `Len`/`Sweep` take each lock in turn (no lock ordering issue: never two at once) |
| `ClientIPResolver`, `Provider`, `Policy`, `handler` of `logx` | immutable after construction |
| request state (`id`, `inbound`, `ip`, `pattern`) | one struct per request; written by the middleware that owns each field, in order, on the request's goroutine; the access log reads it after the handler returned (the same goroutine) |
| `logx` context attributes | immutable `[]slog.Attr` copied on `With` |
| `config.Load` | single goroutine at startup |

Verified by `go test -race` on every package, including `TestLimiterRace` (8 goroutines x `Allow`/`Sweep`), the lifecycle tests (`-race -count=3`) and the end-to-end `TestRunServesAndStops` over real sockets.

### 8.3 Start and stop order (this unit's stages in architecture 10.1/10.4)

Start order in the `Plan` (stop order is the reverse): `telemetry` (Close only), `store`, `refresh`, `ratelimit-sweeper`, `servers` (public and admin together). The servers start **last and stop first**. (Architecture 10.1 step 8 lists the sweeper inside the refresh stage and 10.3 calls it "a lifecycle worker"; it is its own two-line stage here because `httpx` cannot be part of a stage built from `refresh`, and a stage of its own keeps its stop bounded independently. No deviation: the architecture names the worker, not the stage.) Listeners are opened before `lifecycle.Run` (a bind failure is a startup error with a clear message, not a stage failure); both listeners accept immediately so `/livez` answers while the store warms.

```text
SIGTERM -> NotifyContext cancels root; OnShutdown(stop) so a second signal kills the process
  Drain hooks (reverse stage order): servers.Drain = ready.SetShuttingDown() (/readyz 503, /livez 200), live.Drain = SSE "retry: 30000" and close
  DrainDelay (5 s) still serving: load balancers stop sending traffic
  servers: cancel -> Shutdown(server_timeout) on public and admin in parallel waits for in-flight requests -> Close on expiry
  ratelimit-sweeper: cancel -> returns
  refresh: cancel -> engine Grace (15 s) -> joined -> Close (final plan flush)
  store: cancel -> Store.Run returns -> Close (WAL checkpoint, optional backup on shutdown)
  telemetry: Close = Provider.Shutdown(telemetry_timeout) (final flush)
exit code: 0 if the joined error is nil, else 1 with the errors printed
```

A stage failing at runtime (for example a listener of the `servers` stage failing, which also stops the other listener gracefully, or any stage returning early with nil: `ErrStageExited`) triggers the **same ordered shutdown**, with the failed stage's error first and the failed stage not waited on.

### 8.4 Failure of this unit's pieces at runtime

| Failure | Behaviour |
|---|---|
| OTLP collector unreachable | exporters give up after their timeout and drop data (bounded queues); `/metrics` is unaffected; SDK errors reach `logx` at warn through `otel.SetErrorHandler` in `cmd/scp` (a permitted global accessor, architecture 5.1 rule 6) |
| limiter at its key cap | overflow bucket (4.5.5): never allocates beyond the cap, never fails open |
| `Hooks.*` nil | `PublicChain`/`AdminChain` panic at construction naming every missing `Deps` member (not at the first request, where a nil hook would panic inside the panic handler); `TestChainsRefuseMissingDeps` |
| panic in a handler | 500 via `Hooks.Recovered`, process continues |
| a secret file removed after start | no effect (read once) |

---

## 9. Observability

### 9.1 Metrics owned

Instrument names are Go constants next to the instrument and are listed in the generated `docs/reference/metrics.md`. Verified 2026-10-14 against a live process (`TestMetricNames` while writing this document; the output is reproduced in the prototype's `out/` after the next generation):

| Series (Prometheus name) | Type | Labels | Owner / created by | Meaning |
|---|---|---|---|---|
| `scp_build_info` | gauge (always 1) | `version`, `commit`, `go` | `telemetry` | architecture 18.2 |
| `scp_ratelimit_rejected_total` | counter | `class` (`badges`, `api`, `media_ext`, `csp_reports`, `webhook_invalid`) | `httpx.Limiter` (counter injected through `LimiterOptions.Rejected`; one instrument shared by every class; the webhook's limiter is counted the same way, no bridge) | requests (or, for `webhook_invalid`, invalid-signature deliveries) refused by the keyed limiter |
| `scp_ratelimit_keys` | gauge | `class` | `cmd/scp` (observable, callback = `Limiter.Len`) | tracked keys per class; at `max_keys` the overflow bucket is in use |
| `scp_lifecycle_phase_seconds` | histogram (s) | `stage`, `phase` (`drain|stop|close`; `run` is never observed), `outcome` (`ok|error|timeout`) | `cmd/scp` (`Plan.Observe`) | shutdown phases, so a slow stop is visible |
| `http_server_request_duration_seconds` (`http.server.request.duration`) | histogram (s) | `http.request.method`, `http.response.status_code`, `http.route` (pattern), `url.scheme`, network attrs | `otelhttp` via `httpx` | `Quiet` routes excluded |
| `http_server_request_body_size_bytes`, `http_server_response_body_size_bytes` | histograms | same | `otelhttp` | |
| `go_goroutine_count`, `go_memory_used_bytes`, `go_memory_allocated_bytes_total`, `go_memory_allocations_total`, `go_memory_gc_goal_bytes`, `go_processor_limit`, `go_config_gogc_percent` | gauges/counters | | `contrib/instrumentation/runtime` v0.72.0 | `telemetry.runtime.enabled` |
| `promhttp_metric_handler_errors_total` | counter | `cause` | `client_golang` | scrape-time errors |

Names that other units own and rely on this unit for: every instrument is created from the injected `Meter`; histograms use unit `s` and the `DefaultBuckets` View; counters end in `_total`. The `scp_identity_header_spoof_total` counter belongs to `access`, `scp_csp_reports_total` to `webapp`.

### 9.2 Log fields

Common on every record: `ts`, `level`, `msg`, `service`, `version`, `build`. Context fields added by `logx`: `request_id`, `client_ip`, `trace_id`, `span_id`, plus those other units add (`delivery_id`, `job_key`, `job_kind`, `attempt`, `source`, `installation_id`, `repo`, `upstream`, `status`, `duration_ms`, `err_code`, `snapshot_version`).

Records emitted by this unit:

| `msg` | Level | Fields | When |
|---|---|---|---|
| `http request` | info (error for 5xx, debug for probes) | `method`, `route`, `status`, `duration_ms`, `bytes`, `client_ip`, `inbound_request_id` | every request, after it finished |
| `panic in handler` | error | `panic`, `stack` | recover |
| `config` | warn | `warning` | each `Result.Warnings` entry |
| `configuration loaded` | info | `layers` (counts per layer), no values | startup (from `Result.Applied`: which layer set which **keys**, never values) |
| `stage started`, `shutdown begins` (`cause`), `shutdown complete`, `stage did not stop; its Close is skipped` | info / error | `stage` | lifecycle |
| `admin listener is open to the network` | warn | `listen` | non-loopback admin, no bearer |
| `shutdown budget exceeds the termination grace` | warn | `worst_case_seconds` | `Config.WorstCase(refresh StopTimeout)` > 45 s |

### 9.3 Spans

`http.server` (otelhttp): name = the matched pattern (set after the mux returns), attributes `http.route`, method, status, `url.scheme`, `server.address`, `network.peer.address`; **inbound `traceparent` is a link, not the parent**. Probes (`Quiet`) produce none. Other spans (`job.run`, `github.request`, `store.tx`, ...) belong to their units and use the `Tracer` from `Provider`.

---

## 10. Security

| Threat | Control in this unit | Evidence |
|---|---|---|
| Credential leaks into logs, errors, JSON, YAML, `%v`, `%p` (GR-13) | `Secret` formats as `[REDACTED]` in every path, holds its value behind a pointer; `logx` scrubs registered values (>= 8 bytes) from every attribute and message; `RedactURL`; configuration is logged as layers and keys, never values (masking by key name misses every key nobody listed) | `TestNeverPrints`, `logx` scrub tests, `FuzzRedactURL` |
| A secret in the config file or in the process list | file only accepts `{file: /abs}` (a literal in YAML is an error); `--set` refuses secrets; env literal is allowed but documented as the weaker form | `config` tests |
| Empty credential treated as configured (GR-13) | empty is unset everywhere; a required secret that resolves to nothing is a startup error | `secret`, `config` tests |
| Config typos silently ignored (GR-12) | schema `additionalProperties: false`, unknown-key did-you-mean, unknown `SCP_*` warning, documented == consumed test | `config` and `cmd/scp` tests |
| SSRF through operator-controlled or attacker-influenced URLs (GR-22, architecture 17.2) | `netguard`: connect-time address check after DNS (so rebinding and redirects cannot bypass it), **all link-local ranges and every known metadata address always denied even when allow-listed**, `Proxy: nil` by default, host pinning on every redirect hop, scheme/userinfo/IP-literal refusal, port allow-list | `netguard` tests: loopback and metadata blocked, mapped IPv6, redirect to a disallowed IP, host pin, scheme, IP literal, policy validation, `PrivateNetworks` |
| Spoofed `X-Forwarded-For` choosing the rate-limit key or the logged IP | honoured only from `server.public.trusted_proxies` peers, right-to-left walk, `/0` refused, IPv6 /64 bucketing | `TestClientIP` (10 cases), `FuzzClientIP` |
| Rate-limit key rotation to exhaust memory | per-shard key cap with a shared overflow bucket; IPv6 bucketing | `TestLimiterCapUsesOverflowBucket` |
| CSRF on state-changing routes | `http.CrossOriginProtection` with explicit bypass for exactly the webhook and CSP-report routes | `TestChainCSRFAndBypassAndBodyLimit` |
| Missing security headers (GR-18) | class defaults, golden-pinned, never overwriting a handler's own, applied by the outermost writer so that **every** response has them (recovered 500, 429, 403, implicit 200, final status after a 103) | `TestHeaderGolden`, `TestRecoveredAndSilentHandlersAndInterimResponsesKeepHeaders` |
| Request-id injection / log forging | the id is ours (UUIDv4); inbound ids are validated and logged under a different field | `TestChainHeadersRequestIDAndLog` |
| Unauthenticated management plane on the public port (GR-17) | separate admin listener, optional bearer token (constant-time, hashed compare, probes exempt), no identity logic on it, startup warning when open | `TestAdminBearer` |
| DNS rebinding against the admin listener from an operator's browser (a same-origin page of the attacker's name re-pointed at the admin address reaches `/debug/pprof`, `/status` and, with the `X-SCP-Request` header it can now send, the mutating admin routes) | optional `server.admin.allowed_hosts` Host guard (421); the bearer token also defeats it (the attacker's page does not know the token); the open-admin warning names both | `TestAdminHostGuardRefusesRebindingHosts` |
| Trace-context abuse by anonymous callers | inbound `traceparent` is a span link; sampler is ours | `TestQuietProbeNoSpanDebugLog` |
| Telemetry redirected or authenticated by the environment | every OTLP setting passed explicitly (endpoint, headers, compression, client); pinned by a test with hostile `OTEL_*` variables | `TestOTELEnvDoesNotRedirectOrLeak` |
| Hostile config input (huge file, alias bomb, odd YAML) | 1 MiB cap, one document, string keys, yaml.v3 alias limits, fuzzed loader | `FuzzLoadFile`, `FuzzEnv`, `FuzzParseBytes`, size-limit test |
| Secret file tricks (FIFO, device, symlink to huge file) | `Stat`-follow + regular-file check, 64 KiB cap, absolute path | `TestResolveRefusals` |
| Slowloris / oversized headers | `ReadHeaderTimeout` 5 s, `MaxHeaderBytes` 16 KiB, `MaxHeaderValueCount` 100, `IdleTimeout` | `NewServer` values pinned by a test |
| Shutdown races leaving a corrupt store | `Close` only after `Run` joined; a stuck stage's `Close` is skipped; every hook bounded | lifecycle synctest tests |
| Dependency supply chain | every library below is a maintained, widely used module; versions pinned in `go.mod`; `govulncheck` in CI (architecture 22) | versions in Appendix A |

Residual risks (accepted, owner informed): a limiter at its key cap throttles *new* legitimate clients while an attacker holds the cap (section 4.5.5); a proxy that writes ports into `X-Forwarded-For` collapses its users into one bucket (4.5.4); the value scrub does not reach non-string attributes (4.2); `RedactURL` keeps paths (4.2); `trusted_proxies` refuses `/0` but accepts a `/1`, which is a misconfiguration the operator owns (`access` warns about broad ranges for its own list, 14; here it is Q16); the environment-literal form of a secret is visible to anyone who can read `/proc/<pid>/environ` or `docker inspect` (documented; `_FILE` is the recommended form); the `SCP_*` unknown-variable check is a warning because other tools share the prefix; `ratelimit` state is per process (two replicas have independent budgets, acceptable for the single-replica deployment the architecture targets).

---

## 11. Guardrail and capability trace table

Every capability and every design guardrail (`../02-design-guardrails.md`) that touches this unit, with the section that answers it and the mechanism that keeps it answered. Mechanism codes as in architecture section 20: **TYPE** type system, **TEST** automated test, **LINT** linter rule, **AT** archtest, **CI** pipeline step, **DESIGN** structure. Items that look related but belong to another unit are listed with "no platform part" so the omission is visible.

### 11.1 Capabilities

| Capability | Where | Mechanism |
|---|---|---|
| Outbound HTTP resilience | 4.6 (`netguard`: dial/TLS/header/total timeouts, redirect policy, connect-time SSRF check, shared pooled client). Breaker, bulkhead and pacing are `github`'s and `refresh`'s | TEST (`netguard` tests), LINT (G4, `noctx`), DESIGN (one client per upstream built once in `cmd/scp`) |
| Liveness and readiness probes | `/livez` and `/readyz` (route table); 4.5.3 (probe class `Quiet`: no span, debug log, no metric series), 4.7 (`Drain` hook flips `/readyz` to 503 first), 7.1, 7.2; the health *content* is `ops`'s. No probe calls out (GR-41) | TEST (`TestQuietProbeNoSpanDebugLog`, lifecycle order), AT (`ops` has no client) |
| Build information | 3.4, 9.1: `scp_build_info{version,commit,go}`; the JSON view is `ops`'s `/status` | TEST (`TestProviderMetricsNames`) |
| Metrics | 4.4, 9.1: OTel SDK + Prometheus exporter on the admin listener at `/metrics`, runtime metrics, name fidelity rule | TEST (name assertions, live-process series list) |
| Dependency "last seen" tracking | no platform part beyond the `Meter` every unit receives (the per-upstream last-success gauges are created by `github`, `refresh`, `integrations`) | DESIGN |
| Logging | 4.2, 9.2: JSON to stdout, `info` default, request/trace ids, scrubbing | TEST, LINT (`sloglint`, `forbidigo` on `log.Print*`) |
| Same-origin only (no CORS) | 7.3: no `Access-Control-*` is ever emitted; no CORS middleware exists (GR-18) | TEST (e2e asserts absence) |
| Layered process configuration | 4.3, 6: embedded default < file < `SCP_` env < flags; strict; one typed root | TEST (precedence, violations, fuzz) |
| One configuration format, no profiles | 6.1: one YAML document over the embedded default; no `mode` or profile key exists | DESIGN |
| Secrets handling | 3.1, 4.1, 4.2, 4.3.3, 10 | TYPE (`Secret`), TEST (`TestNeverPrints`, `TestResolveRefusals`, scrub tests) |
| No browser credential tooling | no OAuth-client credential keys, no browser automation anywhere; credentials arrive as files (GR-13) | AT (dependency check), CI |
| Readiness gating and explicit startup | 2.5, 8.3: `Drain` hooks, `DrainDelay`, ordered stages (the gates themselves are `ops`'s) | TEST (lifecycle synctest) |
| Admin listener | 4.5.3 (admin chain), 6.2 (`server.admin.*`), 7.1 | AT (`pprof` only in `ops`), TEST (`TestAdminBearer`; public mux has no `/debug`) |
| Config lint, schema and reference | 4.3.8 (`scp config lint`, schema, reference, `show`), 6.4 | CI (examples linted), TEST |
| Graceful shutdown | 4.7, 8.3 (resumable sync is `store` + `refresh`) | TEST (synctest: order, bounds, failure) |
| Structured logs, request ids, trace context | 4.2, 4.4, 4.5.3, 9 | TEST (`TestChainHeadersRequestIDAndLog`), LINT |
| CSP and security headers | 4.5.3, 7.3 (`httpx` classes; the document CSP is `webapp`'s) | TEST (golden strings), CI (Playwright CSP gate is `webapp`'s) |
| Health content | no platform part: no health-indicator framework exists; the content of health is `ops`'s; the platform supplies only the drain hook and the `Quiet` probe class (4.5.3) | DESIGN |
| No request-reflecting diagnostics | no route, no handler, no platform helper that reflects a request; debugging goes through pprof and the access log on the admin listener (GR-17) | AT (route table has no echo), TEST (`TestPublicHasNoManagementRoutes`, 01) |
| Task status | no cache or scheduler introspection endpoint; task status is `ops`'s `/status` | DESIGN |
| Visibility policy | 6.5: the cross-section refusal (private visibility with `access.mode: public`) is the root `Validate` in `cmd/scp`; the policy itself is `catalog`'s, `access`'s (GR-35) | TEST (`cmd/scp` root `Validate` table test) |
| Latest-commit limit | no platform part: `commits.latest_n` is `09`'s key; the documented == consumed test guarantees it is read | TEST (`TestEveryConfigKeyIsConsumed`) |
| Container image shutdown budget | 6.2: 44 s worst case inside `terminationGracePeriodSeconds: 45`; `Config.WorstCase` warns; the image is the build unit's | TEST (`TestWorstCaseOfTheDefaultsFitsTheReferenceTerminationGrace`), CI |
| Deployment descriptor | `/livez` and `/readyz` distinct, `Drain` flips only readiness (4.5.2, 8.3); one replica is the architecture's decision | TEST (lifecycle order) |
| Recorded upstream responses | no production switch dumps upstream responses; record/replay is a test transport (05, GR-27) | DESIGN |
| One server mode | one `main`, one `run()`, one mode; no mode key (GR-01) | DESIGN |

(The architecture rules `G1`..`G12` of section 5.3 are a different series from the guardrail ids; this unit implements rules G1-G4 and G11.)

### 11.2 Guardrails

| Guardrail | What it demands of this unit | Prevented here by |
|---|---|---|
| GR-01 | one composition root and an explicit startup sequence | one binary, one `run()`, one `lifecycle.Plan` (4.7, 8.3); no mode or profile concept (DESIGN, TEST: `TestRunServesAndStops` boots the real wiring) |
| GR-03 | explicit dependencies, owned lifecycles | no package-level mutable state in any platform package, no `init()`, no OTel globals (2.1 rules 2-4; LINT `gochecknoinits`, `forbidigo`); every goroutine has an owner, `Run`-based lifecycles, `goleak` (8.1, 12.3) (TEST); health vocabulary and "last seen" semantics are `ops`'s and `refresh`'s |
| GR-04 | nothing without a consumer | documented keys == consumed keys (TEST `TestEveryConfigKeyIsConsumed`, 4.3.9); `Peek` was removed because nothing needed it (4.5.5) |
| GR-11 | one encoder | no platform part (`httpapi`); `config` uses YAML and JSON Schema only |
| GR-12 | one typed, strict, validated configuration | one loader and one typed struct, no string-keyed lookup; `os.Getenv` only in `cmd/scp` (LINT `forbidigo`); schema `additionalProperties: false` + did-you-mean; unknown `SCP_*` warning; `time.Duration` strings only, a bare integer refused; derived `SCP_A_B_0_C` names only, keys snake_case, dotted names get a hint; typed decode via the schema, then `Validate`, order never matters, `version:` field; `--config` flag + embedded default; every example linted in CI (TYPE, TEST, LINT, CI: `TestExamplesLint`) |
| GR-13 | secrets are a type and never leave headers | `Secret` formatting + scrubbing, `Secret.IsZero` (empty is unset in `Secret`, env and files; `Resolve` of an empty file is `ErrEmpty`), `RedactURL`, access log has the route pattern and no query, config logging lists layers and keys never values (4.3.1); no browser-automated credentials (TYPE, TEST, AT) |
| GR-14 | neutral defaults | `TestDefaultsHaveNoForeignHosts` scans `default.yaml`; `github.com` is the only default host kind; Jenkins and Snyk are off; the platform sections contain no organisation-specific default (TEST) |
| GR-16 | truthful status codes | probes are 200/503 and can fail; the `Drain` hook flips `/readyz` (8.3) (TEST) |
| GR-17 | separate management plane, nothing reflects requests | admin listener with optional bearer (constant time, probes exempt), optional Host guard, no identity logic on it, startup warning when open; no echo route and no thread dump (AT, TEST `TestAdminBearer`, `TestAdminHostGuardRefusesRebindingHosts`) |
| GR-18 | strict browser security policy | 7.3 golden header classes, HSTS option; no CORS, `CrossOriginProtection` for unsafe methods (TEST) |
| GR-20 | webhook ingress: limit, verify, then parse | `logx` rules forbid logging bodies (4.2); `Secret.IsZero` and the schema refuse a required empty secret; the webhook route's presence is `webhook`'s (TEST) |
| GR-22 | one client per upstream, bounded by time | `netguard.NewClient` only: one pooled client that `cmd/scp` builds once per upstream; dial 5 s, TLS 5 s, response header 10 s, total 30 s (or caller deadlines), redirects off by default, `Proxy: nil`, connect-time policy, bounded idle pool; `http.Client{}` literals forbidden outside `netguard`/`cmd/scp` (LINT G4, `noctx`); the breaker is `github`'s, pacing `refresh`'s (DESIGN, TEST) |
| GR-23 | typed upstream failures | no platform part except the sentinels `ErrRefused` and `ErrMetadataAddress`, which `github` classifies as `Permanent`; classification itself is `github`'s (15) |
| GR-24 | the job engine never loses work silently | `lifecycle`: every `Run`, `Drain` and `Close` is bounded and panic-safe, a failed stage is reported first, a stuck stage is reported and its `Close` skipped (TEST: synctest suite); retries and periodic jobs are `refresh`'s |
| GR-25 | budgeted upstream requests | no platform part (`refresh`, `github`) |
| GR-27 | test seams live in tests | no dev switches in production code; tests use a policy that allows loopback, not a production switch (4.6); record/replay is a test transport (05) (DESIGN) |
| GR-35 | explicit access policy | the cross-section refusal of private visibility on a public portal is the root `Validate` (6.5); the visibility semantics are `catalog`'s and `access`'s (TEST) |
| GR-41 | health from outcomes; local, binary probes | probes are 200/503, separate and `Quiet`, read memory only; the platform supplies the drain hook only (`ops` owns content); no thread-pool or cache indicator exists; metrics are Go runtime and per-route (9.1) (DESIGN, TEST) |
| GR-42 | structured, correlated logs | `logx` JSON with request, trace and delivery ids, `info` default (TEST, LINT) |
| GR-45 | reproducible, verifiable artefacts | no platform part for the image beyond the shutdown budget that fits the manifest (6.2); `netguard` sets `MinVersion: TLS 1.2` and never `InsecureSkipVerify` (TEST) |

---

## 12. Test plan

All tests were written and run in the prototype (go1.27.1, `-race`); the counts below are what exists there. "Planned" marks what the implementation adds and the prototype does not contain.

### 12.1 Layers

| Layer | What | Tooling |
|---|---|---|
| Unit | every exported function and each rule of section 4 | `testing`, table tests, `-race` |
| Bubble (`testing/synctest`) | limiter refill/eviction, sweeper tick, lifecycle ordering and timeouts, server graceful stop timing where fake time helps | `synctest.Test` (Go 1.25 GA; real sockets are not used inside a bubble) |
| Fake collaborators | OTLP collector (`httptest.Server` receiving `/v1/traces`), a hostile `OTEL_*` environment (`t.Setenv`), upstream servers for `netguard` (a policy allowing `127.0.0.0/8` plus the server's port), real sockets for `Serve` and `run()`. **There is no GitHub in this unit**; the fake GitHub (`cmd/fakegithub`, 05) is used by the `cmd/scp` e2e test that boots `run()` and by `github`'s tests through a netguard client | `httptest`, `t.Setenv` |
| Fuzz | the parsers that read hostile or operator input | native fuzzing; seeds committed; CI runs each 10 s per PR and 5 min nightly |
| Golden | header strings per class, generated reference/schema/env list, a set of violation messages with line numbers | strings in tests (headers), files under `testdata/` with `-update` flag (generated artefacts) |
| Compile-time | the consumer-interface assertions of 2.3 | `cmd/scp/assert.go` |
| Analysis | documented == consumed keys | `x/tools/go/packages` in `cmd/scp` |
| e2e | `run()` boots two real listeners, serves a request, answers `/metrics`, shuts down | `cmd/scp/run_test.go` |

### 12.2 Tests by package (existing in the prototype)

| Package | Tests |
|---|---|
| `secret` (3) | `TestNeverPrints` (10 verbs, struct by value and pointer, Fprint/Fprintln, JSON, YAML, slog), `TestFileAndYAML` (resolve, trim, `{file:}` only, relative path, scalar refused, empty), `TestResolveRefusals` (empty, over size, directory, relative path, symlink chain accepted) |
| `logx` (3) | `TestHandlerFields` (fields, order, level case, context attributes replace by key, trace/span ids, value scrubbing, short secrets ignored), `TestRedactURL`, `FuzzRedactURL` |
| `lifecycle` (7, synctest, `-race -count=3`) | `TestOrderedShutdown`, `TestFailedStageStartsShutdownAndIsFirst` (its assertion was a near-vacuous disjunction in the first draft; now the exact event string), `TestStageIgnoringCancelIsSkippedAndBounded`, `TestEarlyNilExitAndPanic`, `TestStagesNeverSeeRootCancel`, `TestDrainAndCloseHooksArePanicSafeAndBounded` (a panicking `Drain` and a never-returning `Close` are both reported, total time = signal + `StopTimeout`), `TestWorstCaseOfTheDefaultsFitsTheReferenceTerminationGrace` |
| `netguard` (8) | `TestDefaultPolicyBlocksLoopbackAndMetadata`, `TestAllowListPortsHostPinAndRedirects`, `TestPolicyAndValidateURL`, `TestPrivateNetworksAllowPrivateButNeverLoopbackOrMetadata`, `TestNegativeTimeoutMeansNoOverallDeadline`, `TestWholeLinkLocalAndEveryMetadataAddressStayDeniedEvenWhenAllowListed`, `TestHostnameResolvingToLoopbackIsBlockedAtConnectTime`, `TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault` (the last three added by the verification: they pin GR-22's proxy, redirect and rebinding properties, which the first draft left untested) |
| `telemetry` (4) | `TestProviderMetricsNames` (names, labels, no scope/target info), `TestValidate`, `TestOTELEnvDoesNotRedirectOrLeak`, `TestValidateEndpointShapeAndNewTrimsTrailingSlash` |
| `config` (18 incl. 3 fuzz) | `TestDefaultsOnly`, `TestPrecedence` (layers, comma lists, reserved and service-link variables, did-you-mean warning), `TestAllViolationsWithLinesAndHints`, `TestListFromEnvAndGap`, `TestSecretFileResolveAndFlagRefusal`, `TestCrossFieldValidatorPaths`, `TestSchemaAndReferenceAreDeterministicAndDocumented`, `TestDescribeRejectsUndocumentedAndColliding`, `TestMapEntryEnv`, `TestInlineTwoOwners`, `TestOverlayAddsConditionalRule`, `TestFileSizeLimit`, `TestTextLeafTakesIntegerAndString`, `TestValidateShapes`, `TestDescribeRejectsFileTwinCollision`; `FuzzLoadFile`, `FuzzParseBytes`, `FuzzEnv` |
| `httpx` (26 incl. 2 fuzz) | `TestParseTrustedRefusesAll`, `TestClientIP` (10 cases), `TestRateKeyBucketsIPv6`, `TestLimiterRefillAndSweep` (synctest), `TestLimiterCapUsesOverflowBucket`, `TestLimiterRace`, `TestRunSweepersEvictsAndStops` (synctest), `TestHeaderGolden`, `TestChainHeadersRequestIDAndLog`, `TestChainRateLimit429`, `TestChainCSRFAndBypassAndBodyLimit`, `TestChainRecoversPanicWithRequestID`, `TestQuietProbeNoSpanDebugLog`, `TestAdminBearer`, `TestServeGracefulStop`, `TestConfigValidate`, `TestRateText`, `TestExtraMiddlewareRunsAfterClientIPBeforeHeaders`, `TestNewServerLimits`, `TestChainsRefuseMissingDeps`, `TestRecoveredAndSilentHandlersAndInterimResponsesKeepHeaders`, `TestAdminHostGuardRefusesRebindingHosts`, `TestLimiterCountsItsOwnRejections`, `TestServeAllStopsTogetherAndOneFailureStopsAll`; `FuzzClientIP`, `FuzzRate` |
| `cmd/scp` (4) | `TestEveryConfigKeyIsConsumed`, `TestDefaultsHaveNoForeignHosts`, `TestRunServesAndStops` (real sockets, env with a service-link and a typo variable, `/metrics` series, graceful stop), `TestGenArtifacts` (regenerates `out/`) |

Fuzz status: each fuzz target ran 8 s with 16 workers on 2026-10-14 with no finding (re-run by the verification after the corrections: `FuzzRate` 8 s and 1.5M executions, `FuzzClientIP` 5 s, `FuzzLoadFile` 8 s, no finding); first-draft figures (`FuzzLoadFile` about 90k executions, `FuzzParseBytes` 2M, `FuzzEnv` 90k, `FuzzClientIP` 740k, `FuzzRate` 1.4M, `FuzzRedactURL` 2.5M).

### 12.3 Planned additions (implementation)

| Test | Layer | Asserts |
|---|---|---|
| `goleak.VerifyTestMain` in `httpx`, `lifecycle`, `telemetry`, `netguard` | analysis | no goroutine survives `Serve`, `Run`, `Provider.Shutdown`, `client.CloseIdleConnections` (architecture 10.3) |
| `TestRoutesClassComplete` (`cmd/scp`) | unit | every `routes.Table` entry maps to a `Class`; every class entry names a route; every `CSRFBypass` pattern is a table pattern |
| `TestPublicChainGoldenPerClass` (`cmd/scp`) | golden | one request per route class through the real chain: the exact header set (including a handler that sets its own CSP) |
| `TestConfigViolationsGolden` | golden | a bad file with 12 distinct problems prints exactly the expected lines, with line numbers |
| `TestExamplesLint` | CI | every file under `examples/` passes `scp config lint --strict-env` |
| `TestSchemaDrift` | CI | generated schema, reference and env list equal the committed files |
| `TestMetricsReferenceDrift` | CI | boots every constructor against an in-memory reader and compares the series list with `docs/reference/metrics.md` |
| `TestNoOsGetenv` | AT/LINT | `forbidigo` rule: `os.Getenv`, `os.LookupEnv`, `os.Environ` only in `cmd/scp` |
| `TestHeadersFileParser` | unit | `telemetry.otlp.headers_file`: comments, blank lines, duplicate names, CRLF, 16 KiB cap, a line without a colon |
| `TestAdminWarnsWhenOpen` | unit | non-loopback admin and no token logs the warning |
| `TestOTELErrorHandlerRoutedToLogx` | unit | an exporter failure produces one `warn` record, not stderr noise |
| `FuzzParseHeadersFile`, `FuzzParsePath` (config `--set` and env path parser) | fuzz | no panic |
| a 60 s soak of the limiter with 200 goroutines under `-race` | stress | no data race, memory within the cap |
| `TestPublicChainGoldenPerClass` also asserts a recovered 500 and a 429 per class carry the class headers, and that every badge route has the badge class | golden | the verification's header and classification findings stay fixed |
| `TestAdminProbesOnTheAdminListenerNeedNoToken` (wiring) | unit | a kubelet probe against 9091 with a token configured returns 200 |
| `TestShutdownBudgetOfTheRealPlan` (`cmd/scp`, synctest) | bubble | the real `Plan` with every stage stuck stops within `Config.WorstCase(refresh StopTimeout)` and that is at most 45 s with the defaults |

### 12.4 Running

```text
cd 02-platform.assets && go vet ./... && go test -race -count=1 ./...                      # about 20 s
go test -run xxx -fuzz '^FuzzLoadFile$' -fuzztime 8s ./internal/platform/config
GEN_DIR=$PWD/out go test -run TestGenArtifacts ./cmd/scp                                    # regenerate the generated artefacts
```

---

## 13. Ordered implementation tasks and milestones

Dependencies on other units are named; everything without a name depends only on earlier tasks here. Sizes are engineer-days for someone who has read this document (prototype code can be lifted).

| M | Task | Deliverable (done when) | Depends on | Days |
|---|---|---|---|---|
| **M0 Foundations** | T0.1 `secret` | package + `TestNeverPrints`, `TestFileAndYAML`, `TestResolveRefusals` pass; prototype copied | none | 0.5 |
| | T0.2 `lifecycle` | `Run`, `Plan`, `Config`, errors; the five synctest tests pass with `-race -count=3` | none | 1 |
| | T0.3 `logx` | handler, accessors, scrub, `RedactURL`; tests + fuzz | T0.1 | 1 |
| | T0.4 archtest rows for the seven packages | the rank table rows of 2.1 in `internal/archtest` (the build unit owns the test) | none | 0.25 |
| **M1 Config machinery** | T1.1 `Describe`, kinds, env derivation, collisions, `Validate` shape check | all declaration rules of 4.3.2 with tests | T0.1 | 1.5 |
| | T1.2 tree, merge, path grammar, coercion, `ParseBytes` | tests incl. fuzz | T1.1 | 1 |
| | T1.3 env overlay (lists, map entries, secrets, reserved/service-link, warnings, did-you-mean) | `TestPrecedence`, `TestListFromEnvAndGap`, `TestMapEntryEnv`, `FuzzEnv` | T1.2 | 1.5 |
| | T1.4 schema generation, patch, overlay, compile (`santhosh`), leaf-cause mapping, ranges | deterministic output; `TestSchemaAndReferenceAreDeterministicAndDocumented`, `TestOverlayAddsConditionalRule` | T1.1 | 2 |
| | T1.5 `Load` pipeline, violations with lines, `--set`, secrets resolution order | `TestAllViolationsWithLinesAndHints`, `FuzzLoadFile`, size limit | T1.3, T1.4 | 1.5 |
| | T1.6 validator fan-out (3 shapes, inline, dedupe) | `TestValidateShapes`, `TestInlineTwoOwners`, `TestCrossFieldValidatorPaths` | T1.5 | 1 |
| | T1.7 `Reference`, `EnvNames` generators + golden files | `TestGenArtifacts` output committed | T1.4 | 0.5 |
| | T1.8 `cmd/scp` root `Config`, `default.yaml` (platform sections), `scp config {lint,schema,reference,env,show}`, consumption test, `TestDefaultsHaveNoForeignHosts`, CI drift steps | `scp config lint examples/*.yaml` green in CI; every unit adds its own section as it lands | T1.6, T1.7; each unit's `Config` | 2 |
| **M2 Telemetry** | T2.1 `Provider`, views, Prometheus exporter, runtime metrics, build info, names test | `TestProviderMetricsNames`, `TestValidate` | none | 1 |
| | T2.2 OTLP exporters over HTTP with explicit settings, hostile-env test | `TestOTELEnvDoesNotRedirectOrLeak` | T2.1 | 1 |
| | T2.3 `cmd/scp`: headers-file parser (+fuzz), exporter client from `netguard`, `otel.SetErrorHandler` to `logx`, `scp_lifecycle_phase_seconds`, `scp_ratelimit_keys` | `TestHeadersFileParser`, `TestOTELErrorHandlerRoutedToLogx` | T2.2, T3.1, T0.2 | 1 |
| **M3 netguard** | T3.1 `Policy`, `PrivateNetworks`, `NewClient`, `ValidateURL`, sentinels | the five tests | none | 1.5 |
| | T3.2 `cmd/scp` client factory: `Network` (05) to `ClientOptions` (proxy, CA pool, allow-private, insecure HTTP, host pins), Jenkins/Snyk instance policies (14), OTLP | factory tests with a fake server | T3.1; `05-github-adapter`, `14-integrations-access` | 1 |
| **M4 httpx** | T4.1 `NewServer`, `Serve` | `TestNewServerLimits`, `TestServeGracefulStop` | none | 0.5 |
| | T4.2 `ClientIPResolver`, `ParseTrusted`, `RateKey` | `TestClientIP`, `FuzzClientIP` | none | 1 |
| | T4.3 `Limiter`, `Rate`, `RunSweepers` | limiter tests, `FuzzRate` | none | 1 |
| | T4.4 header classes + goldens | `TestHeaderGolden` | none | 0.5 |
| | T4.5 `PublicChain`/`AdminChain`, request state, the header-applying writer, `routeCapture`, hooks, `mustDeps`, `hostGuard`, `ServeAll` | the chain tests, `TestServeAllStopsTogetherAndOneFailureStopsAll` | T4.1-T4.4, T0.3, T2.1 | 2.5 |
| | T4.6 `cmd/scp` classifier from `routes.Table`, hooks from `httpapi`, CSRF bypass derivation, `AfterClientIP` with `access.Middleware`, `TestRoutesClassComplete`, per-class golden | `TestPublicChainGoldenPerClass` | T4.5; `01-kernel`, `06-httpapi`, `14-integrations-access` | 1.5 |
| **M5 Composition** | T5.1 `run()` steps 1-8 for the platform parts: flags, load, logger, telemetry, listeners, plan (one `servers` stage), drain hooks, `Observe`, warnings (open admin, `WorstCase` > 45 s), `SetErrorHandler`, `Without("admin.metrics")` when Prometheus is off | `TestRunServesAndStops` (extended with the real stages) | M1-M4; `ops` (ready flag), `store` and `refresh` stages | 2 |
| | T5.2 `goleak`, soak, race matrix, metrics reference generator | `TestMetricsReferenceDrift`, goleak green | T5.1 | 1 |
| **M6 Release hardening** | T6.1 nightly fuzz (5 min each), `govulncheck`, dependency review of the OTel set, docs (`docs/reference/*`) committed | green nightly | M5 | 1 |

Critical path to a runnable binary skeleton: M0 -> T1.1-T1.5 -> T4.1 -> T5.1 (about 10 engineer-days). Everything else can be done in parallel by a second person (telemetry and netguard have no dependency on config).

---

## 14. Deviations proposed

Each deviation names the architecture text, what is built instead, the evidence and the cheaper alternative that was rejected.

| # | Architecture text | Proposed | Evidence / reason | Alternative rejected |
|---|---|---|---|---|
| **D1** | 5.2 row `platform/config`: `go.yaml.in/yaml/v3`, `caarlos0/env/v11`, `invopop/jsonschema`, `santhosh-tekuri/jsonschema/v6` | drop `caarlos0/env/v11`; add `golang.org/x/text` (a `message.Printer` for the schema validator's error text) | the env names must be **derived from the yaml path**, with `_FILE` twins for secrets, comma lists, map-entry addressing and unknown-variable detection with did-you-mean. `env/v11` is not the obstacle (it handles indexed slices of structs: tested 2026-10-14); it needs a second set of tags that would have to equal the yaml names, which is exactly the duplication that lets two configuration systems drift apart (GR-12). A tree-first loader that validates against the *published* schema makes "what the schema says is what runs" true | keep `env/v11` with `env:"..."` tags on every field (two sources of names; collisions found only at runtime) |
| **D2** | 5.2 row `platform/httpx`: imports `logx`; `x/time/rate`, otelhttp | add internal `platform/secret` (the admin bearer token is a `Secret` in `httpx.Config`) and the otel **API** modules `attribute`, `metric`, `propagation`, `trace` | `scp_ratelimit_rejected_total` needs a `Meter` (G11); otelhttp options take providers and a propagator; rank rule still holds (secret 0, logx 1 < httpx 2) | pass the token as a string (loses redaction in the struct) and the counter as a callback (loses G11 uniformity); same trade-off as `14` D-1 |
| **D3** | 11.1: order "request id -> recover -> access log ..." | request id -> **access log -> recover** -> otelhttp ... | with recover outside the access log a panic skips the access log, so the request an operator most wants a line for has none; with it inside, the record carries `status: 500` and the route pattern | architecture order plus a recover that logs its own access line (two code paths for one record) |
| **D4** | 16: `ratelimit: { badges: 120/min, ... }` | each class is a text `Rate`: `"120/min burst 60"` (burst optional) and the set gains `webhook_invalid`, `sse_per_ip`, `max_keys`, `idle_ttl` | 17.4 gives burst values the outline's scalar cannot hold; `04` already tabulates `ratelimit.webhook_invalid` as "rate, 10/min burst 10" with env `SCP_RATELIMIT_WEBHOOK_INVALID`; `06` uses `csp_reports` 30/min burst 10 | a struct `{per_minute, burst}` per class (works, but breaks `04`'s documented env name) |
| **D5** | 16 config outline | (a) new `shutdown:` section; (b) `server.admin.bearer_token`; (c) `telemetry.otlp`: no `protocol` (HTTP only in v1), plus `headers_file`, `allow_private_cidrs`, `ports`, `signals`, `timeout`; `telemetry.prometheus` stays a bool; (d) `telemetry.runtime.enabled`; (e) `server.timeouts.max_header_values`; (f) no `telemetry.prometheus.path` (the route table fixes `/metrics`) | (a) 8.16 names `DrainDelay` 5 s and per-stage timeouts without a config home; (b) GR-17 says "optional bearer auth"; (c) the gRPC exporter pulls grpc and protobuf runtime into a deliberately small binary, HTTP/protobuf is enough for every common collector, and credentials in a rank-0 package need a file; (d)(e) trivial knobs the stdlib/SDK now provide; (f) 11.2 and 01-kernel make the table the only place URLs are named | gRPC in v1 (heavier module graph, nothing needs it), no shutdown keys (timeouts as constants: operators with large `refresh.grace` could not fit) |
| **D6** | 8.16: `Plan{Stages, DrainDelay}`, `Run` returns the joined error | `Plan` also has `Log`, `OnShutdown`, `Observe`; `ErrStageExited` for an early nil exit; `StageError` and `PanicError` types; every `Drain`/`Close` is bounded by `StopTimeout` | 10.4 needs the signal-stop call (`stop()` so a second signal kills) and per-phase observability; an early `nil` exit would otherwise look like success | external wrappers in `cmd/scp` (they cannot see phase boundaries) |
| **D7** | 16: "platform/config supplies only the machinery" | machinery additions that other units already rely on: `yaml:",inline"` (two owners, one mapping), `Overlay` (hand-written schema fragments, used by `08-catalog`), env addressing of existing map entries (`04`'s `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY`), three accepted `Validate` shapes (units `03`, `05`, `14` returned `error`), text leaves that also take a bare integer (`05`'s `ByteSize`) | each follows from a statement in a neighbouring unit design; none changes the architecture's rule that features do not import `config` | requiring every unit to rewrite its config types to one shape before M1 can finish |
| **D8** | 16: "a test asserting documented keys == consumed keys" (no mechanism) | `golang.org/x/tools/go/packages` as a **test-only** dependency of `cmd/scp` | prototyped: finds exactly the unread leaves (4.3.9), 3-8 s | grep for key names (misses renames and indirect reads), runtime access tracking (needs instrumented getters: a second config API) |
| **D9** | 8.1: `type Secret struct{ v string }` | `Secret` holds `*string`, adds `Ref`/`Resolve`/`Pending`/`Equal`/`EqualString`/`Format`/`MarshalYAML`/`UnmarshalText`/`UnmarshalYAML` | **bug in the architecture's struct**: `fmt` prints a struct by a bad verb (`%p`) **without calling methods**, so `fmt.Sprintf("%p", secretValue)` printed the credential (reproduced 2026-10-14; the pointer field prints an address instead). The extra methods are what the config machinery and constant-time header checks need | keep the `string` field and document "never use `%p`" (an unreviewable rule) |
| **D10** | 5.2 row `platform/telemetry`: OTel SDK, Prometheus exporter | `Options.HTTPClient` and `Options.Headers` (caller-supplied client and header map) and explicit passing of every exporter setting | the OTLP SDK reads `OTEL_EXPORTER_OTLP_*` by itself (compression leaked through during the prototype's first run of `TestOTELEnvDoesNotRedirectOrLeak`), which would break G1's spirit (no environment reads outside `cmd/scp`) and let an injected variable add credentials or redirect telemetry; rank 0 cannot import `secret`, so credentials are a file parsed by `cmd/scp` | trust the SDK's environment handling (an operator's `OTEL_EXPORTER_OTLP_ENDPOINT` would silently win) |
| **D11** | 11.1: request id from `crypto/rand.Text` | UUIDv4 (`uuid.NewV4()`, stdlib `uuid` of Go 1.27) | `06-httpapi` D4 requires a UUID so `instance` is a valid `urn:uuid:`; the stdlib package exists in 1.27.1 (`go doc uuid`, 2026-10-14) | `crypto/rand.Text` and `urn:scp:request:<id>` (06 supports it as a fallback) |
| **D12** | 8.16 point 3: on `StopTimeout` expiry "log, count and continue ..., then call `Close`" | on expiry the stage's `Close` is **skipped** (logged at error, `ErrStopTimeout` in the result) | closing the SQLite handle under a live worker is the race this package exists to prevent (the stated purpose of point 3); the process is exiting, WAL recovery covers the skipped checkpoint | call `Close` anyway (can corrupt or deadlock) |
| **D13** | 8.2 `logx`: handlers and context attributes | `logx` also exports `Options.Scrub` (value scrubbing), `RequestID`/`ClientIP` accessors, `RedactURL`, `Err`, `ParseLevel`, the `Key*` constants | scrubbing is the backstop for hand-formatted strings; the accessors let `httpapi`, `webhook`, `access` read request facts **without importing `httpx`** (`14` and `04` rely on it) | each consumer defines its own context keys (they could not see each other's values) |
| **D14** | 5.2 test-file rule: a `_test.go` may import what its package may import plus test libraries and helper packages | `httpx` and `telemetry` tests import the OTel **SDK** (`sdk/metric`, `sdk/trace`, `sdk/trace/tracetest`) for in-memory readers and exporters | the chain tests must observe spans, links and metric labels; the same allowance `14` asks for as its D-9 (G6 stays for non-test code). `archtest` needs one rule: the SDK may be imported from `_test.go` files of any package | assert on a recording `trace.Tracer` fake (cannot observe otelhttp's own span links and metric labels, the very thing `routeCapture` exists for) |
| **D15** | 16 config outline, 17.1 admin plane | `server.admin.allowed_hosts` (optional Host allow-list, 421) next to `bearer_token`; the `servers` lifecycle stage is **one** stage for both listeners (`httpx.ServeAll`) | the admin listener is reachable by an operator's browser on a restricted network, where DNS rebinding is the standard attack and Host validation the standard defence (found by the verification); one stage keeps the shutdown worst case inside the architecture's 45 s (10.4 already says "the servers stage") | a hard-coded Host rule (breaks Kubernetes `Service` names), or two sequential server stages (worst case 15 s longer) |

---

## 15. Open questions

| # | Question | Owner | Default if unanswered |
|---|---|---|---|
| Q1 | Units `03` (`Validate(prefix) error`), `05` (`Validate() error` with `*ValidationError{Path, Msg}`) and `14` return `error`. The machinery accepts all three shapes, but placing a leaf exactly needs `ConfigPath() string` on the error type (a one-line method; without it the text-prefix promotion applies when the message starts with the full key path). Should each unit add it, and should new units use the callback shape? | owners of 03, 05, 14 | accepted as is; promotion rule; callback shape recommended for new code |
| Q2 | The `github:` mapping has several owners and several overlaps: `webhook.redelivery.*` (04: `max_per_run` 200, plus `rate`, `max_age`; 05: `max_per_run` 50), `github.defaults.webhook` (05's example vs 04's `webhook.Config`), `discovery.*` (05 vs the catalog-owned `reconcile_every`/`max_removal_ratio` in 08), and the same concurrency limit as `github.defaults.budget.concurrency` (05) and `refresh.upstreams.github.concurrency` (04). `Describe` refuses a key declared twice. Who owns which key, and which of the two concurrency knobs survives? | owners of 04, 05, 08 | `refresh.upstreams.<name>.{concurrency,pace}` is the one limit (the engine enforces it); `github.defaults.budget` keeps only `reserve`; the rest of the split follows the table where each unit says "consumer" |
| Q3 | `05` uses `SecretRef{File, Env}` and `Deps.LookupEnv` (an `env` member naming an environment variable) instead of `secret.Secret`. The machinery accepts it as an ordinary struct (`..._PRIVATE_KEY_FILE` happens to equal the file key), but it loses never-both validation, redaction in `scp config show`, registration with `logx` scrubbing and the `{file:}` shape check. Move to `secret.Secret` and drop the `env` member (`SCP_..._FILE` already covers it)? | owner of 05 | recommend `secret.Secret`; `SecretRef` tolerated |
| Q4 | `06` section 6.2 names `server.public_listen` and `server.admin_listen`; the outline and this unit use `server.public.listen` and `server.admin.listen`. Update `06`. | owner of 06 | nested names (this unit's) |
| Q5 | Default admin bind: the outline says `:9091` (reachable by a Prometheus on another pod); loopback is safer but breaks that. This unit follows the outline and warns at startup when the admin listener is open without a token. Confirm. | owner | `:9091` + warning |
| Q6 | Rate class for the server-text routes (`/repos/*.md`, `llms*.txt`, `dependencies.md`, `sitemap.xml`, `robots.txt`, `/schemas/*`): section 17.4 lists no class for them. Proposed: share the `api` bucket. Should `media.repo` (served from cache) be limited too? | owner | shared `api` bucket; `media.repo` unlimited |
| Q7 | `OTEL_*` variables are ignored (every exporter setting is explicit). Should `OTEL_EXPORTER_OTLP_ENDPOINT` be honoured as a convenience for Kubernetes operators that inject it? | owner | no: documented, `telemetry.otlp.*` only |
| Q8 | Configuration hot reload (`SIGHUP`)? Out of scope; every change needs a restart (the warm store makes that cheap). | owner | no reload |
| Q9 | A runtime log-level switch (admin `PUT`)? `logx` can hold a `slog.LevelVar` at no cost; no endpoint exists in the route table. | owner | fixed level for the process lifetime |
| Q10 | New instrument names not listed in 18.2: `scp_ratelimit_keys{class}` and `scp_lifecycle_phase_seconds{stage,phase,outcome}`. Add them to the catalogue? | owner | yes |
| Q11 | Unit keys of the feature units not yet written (`overview` for `site`, `ops` for `readiness` and `/metrics`, `docs`, `pages`, `media`, `live`) are referenced by feature name in 6.1.2; update when their designs land. | all | update the table at integration |
| Q12 | A failure between "store opened" (10.1 step 2) and `lifecycle.Run` (for example a fatal credential check on a cold start) exits without running any `Close`. SQLite WAL recovers at the next open. Acceptable, or should `cmd/scp` hold early resources in a `defer` stack until `Run` takes over? | owner | accept; document |
| Q13 | `ratelimit.sse_per_ip` is read by `live`, not by `httpx` (the limiter is a rate limiter, not a concurrency limiter). Keep the key here (architecture 17.4 lists the policy) or move it to `live.sse.max_per_ip`? | owner, `live` unit | keep here until `live` is designed |
| Q14 | Should `scp config show --diff-defaults` (print only keys that differ from `default.yaml`) be in v1? Cheap with `Result.Applied`. | owner | yes, v1 |
| Q15 | Should the Host guard be **on by default** with a derived list (IP literals, `localhost`, the configured `public_url` host)? It would stop DNS rebinding for every deployment without configuration but break Prometheus scrapes by `Service` DNS name. | owner | off by default; the open-admin warning names the key |
| Q16 | `trusted_proxies` refuses `/0` but accepts `/1`-`/7`; `access` warns about broad ranges for its own list (14). Refuse or warn here too, and at which width? | owner | accepted as is (operator-owned); recommend a startup warning below /8 (IPv4) and /32 (IPv6) |
| Q17 | The server-text routes and `negotiated.*`/`doc.md` Markdown responses: who sets the sandboxing CSP, `httpapi.Negotiate` (recommended) or `httpx` through a finer class? | owners of 06, overview | the Markdown branch sets it itself; otherwise the HTML class headers apply (7.2) |

---

## Appendix A. Verification record (2026-10-14)

| Fact | How verified | Result |
|---|---|---|
| Module versions are current | `go list -m -u` against the module proxy: `code.dny.dev/ssrf` v0.3.0, `invopop/jsonschema` v0.14.0, `santhosh-tekuri/jsonschema/v6` v6.0.3, `go.opentelemetry.io/otel` and `sdk`, `sdk/metric`, `sdk/trace`, `sdk/log`, `metric`, `trace`, `exporters/otlp/{otlptrace,otlpmetric}/*http` v1.47.0, `exporters/prometheus` v0.69.0, `otlplog/otlploghttp` v0.23.0, `contrib/instrumentation/net/http/otelhttp` and `.../runtime` v0.72.0, `prometheus/client_golang` v1.25.0, `golang.org/x/time` v0.16.0, `golang.org/x/tools` v0.52.0, `go.yaml.in/yaml/v3` v3.0.5, `caarlos0/env/v11` v11.4.1 | no newer version reported for any |
| Go 1.27 APIs used exist | `go doc` on go1.27.1: `uuid.NewV4`, `http.CrossOriginProtection` (`AddTrustedOrigin`, `AddInsecureBypassPattern`, `SetDenyHandler`), `http.Server.MaxHeaderValueCount`, `testing/synctest.Test`, `ResponseController` | present |
| `ssrf.Guardian` needs `tcp4`/`tcp6` | compiled test | plain `tcp` is rejected by the library; `ValidateURL` passes the family |
| invopop splits `jsonschema:` tag values on commas | test run | a `pattern` containing `{1,5}` is truncated; section 2.6 forbids commas in patterns |
| `ServeMux` sets `r.Pattern` on the request it receives only | first chain test | outer middlewares that call `WithContext` never see it; `routeCapture` exists because of this |
| otelhttp `WithPublicEndpointFn(true)` turns inbound `traceparent` into a link | `TestQuietProbeNoSpanDebugLog` | root span with one link |
| Prometheus exporter names | `TestProviderMetricsNames` and a live-process series list | `_total`, `_seconds`, gauges and `scp_build_info{commit,go,version}` pass through unchanged; `WithoutScopeInfo`/`WithoutTargetInfo` remove `otel_scope_*` and `target_info` |
| OTLP exporters and the environment | `TestOTELEnvDoesNotRedirectOrLeak` | explicit endpoint, headers, sampler and compression win; compression did **not** when left unset (env value applied), hence the explicit option |
| Neighbour units | `20-design/` read on 2026-10-14: `01-kernel`, `03`, `04`, `05`, `06`, `07`, `08` (assets), `09`, `11`, `13`, `14` | contracts quoted in 2.6, 6.1 and 15 |

## Appendix B. What was compile-checked

`02-platform.assets/` is a stand-alone Go module (`github.com/cantara/source-code-portal`, go 1.27.1) containing the seven packages and a miniature `cmd/scp`. Verified 2026-10-14 on darwin/arm64: `gofmt -l` clean, `go vet ./...` clean, `go test -race -count=1 ./...` green (about 20 s; 67 test functions and 6 fuzz targets across 8 packages after the verification's additions, 56 before), each fuzz target run for 8 s without a finding, `GEN_DIR=out go test -run TestGenArtifacts ./cmd/scp` regenerating `out/reference.md` (platform sections), `out/schema.json` and `out/env.txt` (42 names after `server.admin.allowed_hosts`; 41 before). The verification re-ran all of it from a fresh copy with the original `go.mod`/`go.sum` under `-mod=readonly` (Appendix C). Every declaration in sections 2.3, 2.4 and 3 is copied from that code or from `go doc` of it; section 2.5's wiring is the body of the prototype's `run()`.

Not compiled (stated so nobody assumes otherwise): the headers-file parser, the `scp config` subcommands, the route classifier, the glue types of 6.1.1 (they need the other units' packages), the `scp_ratelimit_keys` and `scp_lifecycle_phase_seconds` instruments, `otel.SetErrorHandler` wiring, `goleak`.

## Appendix C. Verification log (independent verifier, 2026-10-14)

Scope: conformance with `10-architecture.md`, `04-decisions.md` (section 7) and `11-adr/`; section structure; guardrail traceability; technical correctness (Go, libraries, algorithms); testability; security and lifecycle races; buildability. The verifier did not write the document. Method: read the whole document and the whole prototype; copied the prototype to a scratch module (`scratchpad/verify-02-platform/`), ran `go vet`, `go test -race`, fuzzing, and wrote probe tests for every suspicion before and after fixing; queried the module proxy and the live web. Toolchain `go1.27.1 darwin/arm64`. The full report with evidence is `30-verification/design-02-platform.md`.

### C.1 Findings and what was changed

| # | Severity | Finding | Resolution |
|---|---|---|---|
| V-01 | medium | The header-applying writer sat inside `recover`, so a recovered 500 (reproduced: `500 map[Content-Type X-Request-Id]`, no `nosniff`, no CSP) and any response an `AfterClientIP` middleware wrote carried **no** security headers; a handler that wrote nothing got none either; a `103` marked the response as started and swallowed the final status | Header defaults moved into the `accessLog` writer (outermost), applied at the first final write and after a silent handler, 1xx passed through (4.5.3 item 8); 2 tests |
| V-02 | high | The shutdown design contradicted the architecture: it claimed the manifest of architecture 19 sets `terminationGracePeriodSeconds: 90` (it is **45**, ADR-017, A-10), defaults summed to 55 s (the text said 65), two server stages stopped sequentially (worst case 75 s), unit 04 requires 42 s total | One `servers` stage (`httpx.ServeAll`), defaults 5/15/3/2 s, `Config.WorstCase`, startup warning above 45 s, correct text (2.5, 4.5.2, 6.2, 8.1, 8.3); 3 tests |
| V-03 | medium | `Rate` accepted `30/h` and silently stored 1/min (`PerMinute` is an integer): a 2x to 60x wrong limit for `/h` rates | Per-hour only as a multiple of 60, otherwise a clear error (3.5, tests `TestRateText`, `FuzzRate` re-run) |
| V-04 | medium | 2.3 and unit 04 say `*httpx.Limiter` satisfies `webhook.Limiter` directly; 9.1 said the `webhook_invalid` rejection counter goes through a `cmd/scp` bridge. Both cannot hold | `Limiter` counts its own refusals (`LimiterOptions.Class/Rejected`); no bridge; `Deps.Meter` (decorative afterwards) removed (3.5, 4.5.5, 9.1); test |
| V-05 | medium | The route classification (7.2) had no row for `negotiated.group`, `negotiated.repo` and `doc.md`; `TestRoutesClassComplete` would fail at once | Rows added and checked against `01-kernel.routes.tsv`; contracts for 06/overview noted (7.2, Q17) |
| V-06 | low | `telemetry.prometheus: false` meant "do not mount `/metrics`", which makes `Table.Verify()` fail the start | `Table.Without("admin.metrics")` (7.1) |
| V-07 | medium | `telemetry.otlp.endpoint` accepted userinfo (`https://u:pw@host`, which ends up in SDK error text and logs), a query and a trailing slash (`//v1/traces`); a failed `New` leaked exporter goroutines | Validate refuses userinfo/query/fragment, `New` trims slashes and shuts down what it built (4.4); test |
| V-08 | medium | `netguard` denied only five exact metadata addresses before the allow-list: an operator allow-listing `169.254.0.0/16` exposed e.g. Tencent `169.254.0.23` and `fe80::a9fe:a9fe`. GR-22's properties (no environment proxy, no redirect following, rebinding by name, timeouts) had no test | Whole link-local ranges plus the non-link-local metadata addresses always denied (4.6); 3 tests |
| V-09 | - | Withdrawn and not reused (the finding concerned material no longer in this design) | - |
| V-10 | medium | Trace table (11) lacked health content, request-reflecting diagnostics, task status, visibility policy, the latest-commit limit, the shutdown budget, the deployment descriptor, recorded upstream responses and the single server mode, and guardrails GR-01, GR-03, GR-04, GR-14, GR-16, GR-20, GR-24, GR-27, GR-35, GR-41 and GR-45 | Added with mechanisms (11.1, 11.2) |
| V-11 | low | `config.Error()` dropped the file name for violations without a line (YAML syntax errors, duplicate keys) | Prints `scp.yaml: message` (4.3.1) |
| V-12 | low | Admin bearer: scheme compared case-sensitively (RFC 9110 says insensitive); kubelet probes on 9091 would get 401 once a token is set | Case-insensitive scheme, probes exempt (4.5.3); test |
| V-13 | low | `scp_lifecycle_phase_seconds` listed phase `run`, which `Observe` never reports; `outcome` mapping unspecified | Doc corrected, mapping stated (4.7, 9.1) |
| V-14 | low | Inbound request id logged `url.QueryEscape`d (`:` became `%3A`) while the text said verbatim | Code fixed (logged verbatim after the regexp) |
| V-15 | medium | No defence against DNS rebinding on the admin listener (no bearer by default; the mutating routes' `X-SCP-Request` argument assumes a cross-origin page) | `server.admin.allowed_hosts` Host guard (421), warning text, D15, Q15; default off (a default breaks Kubernetes `Service` scrapes); test |
| V-16 | low | `Describe` did not reserve the `_FILE` twin of a secret in its collision check | Reserved (4.3.2); test |
| V-17 | low | Residual risks unstated: limiter at its key cap throttles new legitimate clients while an attacker holds the cap; `X-Forwarded-For` hops with ports collapse a proxy's users; value scrub covers only string-like values; `RedactURL` keeps paths | Stated in 4.2, 4.5.4, 4.5.5, 10 (not fixable inside the design) |
| V-18 | low | httpx and telemetry tests import the OTel SDK, which the matrix and G6 do not allow outside `telemetry` | D14 (same allowance as 14's D-9) |
| V-19 | low | Count inconsistencies: "40 variables" versus 41, test counts; `PrivateNetworks` missing from section 3 | Fixed |
| V-20 | low | Headers-file grammar unspecified (duplicates, bad names, values in errors) | Grammar and error rules written (4.4) |
| V-21 | low | `TestFailedStageStartsShutdownAndIsFirst` ended in a near-vacuous disjunction; panicking or stuck `Drain`/`Close` hooks untested although 4.7 claims them | Exact assertion; new test |

### C.2 Libraries and standards

Versions in section 2.1 equal `go list -m <module>@latest` on the proxy (2026-10-14), including `code.dny.dev/ssrf` v0.3.0 (2026-07-07) and `otelhttp` v0.72.0 (2026-10-02); `go.dev/doc/go1.27` lists the new `uuid` package and `Server.MaxHeaderValueCount`; pkg.go.dev lists the `ssrf` options and `Guardian.Safe` as used and the `otelhttp` options (`WithPublicEndpointFn`, `WithFilter`, ...) as used (the deprecated one, `WithMetricAttributesFn`, is not). Not verified: `fd20:ce::254` as the GCE IPv6 metadata address (kept in the deny list defensively); real OTLP collector behaviour (every exporter test uses an `httptest` server).

### C.3 Results after the corrections

`gofmt` clean, `go vet ./...` clean, `go test -race -count=1 ./...` green from a fresh copy with the original `go.mod`/`go.sum` (67 test functions, 6 fuzz targets), lifecycle tests `-race -count=3` green, `FuzzRate`, `FuzzClientIP`, `FuzzLoadFile` re-run without finding, generated artefacts regenerated (42 environment names).

## Cross-design review amendments (2026-10-14)

Produced by the cross-design consistency review (`30-verification/cross-design-consistency.md`). Identifiers CD-xx refer to its findings register; companions are `90-config-reference.md`, `91-metrics-catalogue.md`, `92-error-catalogue.md` and `93-deviations-and-open-questions.md`. Text above this section was edited in place where a fix was needed; this section lists what changed and what this unit must still do.

- Changed: section 6.1.2 root config now lists `llms`, `ops`, the typed `manifests.providers` list and `manifests.enrich` (CD-CF-01); the full table is `90-config-reference.md`.
- Still to do in this unit: the telemetry `View` must not discard the histogram bucket advice of instrument creators (CD-MT-06, 15 R-02-1); `httpx` imports `secret` and the otel API (matrix addition A-30); Q2 is answered differently: both concurrency knobs exist (CD-CF-05, A-28).
- Still to do with unit 08: `catalog.Config` `Describe` tags and the one-key matcher `UnmarshalYAML` (CD-CF-10, 16 U-1/U-2).
- Owner: Q5 (`:9091` plus warning) and Q15 (Host guard off by default).
