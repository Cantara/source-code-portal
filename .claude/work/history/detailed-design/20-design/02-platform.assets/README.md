# Reference prototype for design unit 02 (`internal/platform/*` and the `cmd/scp` composition skeleton)

Throw-away, compile-checked and tested reference code for `20-design/02-platform.md`. It is NOT the shipped code: the
implementation copies `internal/platform/**` into the real module (the packages import only each other, so the copy
needs no stubs) and replaces the toy `cmd/scp` with the real composition root.

    go vet ./... && go test -race -count=1 ./...          # about 15 s
    go test -run xxx -fuzz '^FuzzLoadFile$' -fuzztime 8s ./internal/platform/config
    GEN_DIR=$PWD/out go test -run TestGenArtifacts ./cmd/scp   # regenerates out/{reference.md,schema.json,env.txt}

| Path | Content |
|---|---|
| `internal/platform/secret` | `Secret` (redacting, `{file: /abs}` decoding, constant-time compare) |
| `internal/platform/logx` | slog handler, context attributes, value scrubbing, `RedactURL`, request-id and client-ip accessors |
| `internal/platform/config` | tree-first loader, `Describe`, env overlay, `Schema`/`Reference`/`Overlay`, `Validator` fan-out |
| `internal/platform/telemetry` | `Provider` (Prometheus, OTLP/HTTP, runtime metrics, `scp_build_info`) |
| `internal/platform/httpx` | `NewServer`, `Serve`, `PublicChain`/`AdminChain`, `ClientIPResolver`, `Limiter`, header classes, `Config`/`RateConfig` |
| `internal/platform/netguard` | `Policy`, `NewClient`, `ValidateURL` (connect-time SSRF check) |
| `internal/platform/lifecycle` | `Run(ctx, Plan)` and the `shutdown:` config |
| `cmd/scp/config.go`, `default.yaml` | the platform-owned part of the root config and its embedded defaults |
| `cmd/scp/main.go`, `run_test.go` | a minimal composition (config -> telemetry -> servers -> lifecycle) driven end to end by a test |
| `cmd/scp/consumers_test.go` | compile-time assertions of the consumer-defined interfaces of design section 2.3 |
| `cmd/scp/consumed_test.go` | the documented-keys == consumed-keys analysis (`x/tools/go/packages`) |
| `out/` | generated reference table, JSON Schema and environment-variable list of the platform sections |

Verified 2026-10-14 with go1.27.1 darwin/arm64 (the architecture pins 1.27.2; the scratch toolchain was 1.27.1) and the
versions in `go.mod`.

Corrected by the independent verification of 2026-10-14 (see Appendix C of `02-platform.md` and
`30-verification/design-02-platform.md`): the default security headers are applied by the outermost writer, `httpx.ServeAll`
and one `servers` stage, `Limiter` counts its own refusals (`LimiterOptions.Class/Rejected`), `server.admin.allowed_hosts`
(Host guard), bearer probes exemption, per-hour rates only as multiples of 60, whole link-local ranges always denied in
`netguard`, OTLP endpoint shape checks, `Config.WorstCase`, the `_FILE` collision rule, and 10 new tests (67 test functions,
6 fuzz targets, 42 environment names). `go.mod` and `go.sum` are unchanged.
