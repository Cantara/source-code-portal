# Build, CLI, config and operations
Status: as built. Binding order: ../plan.md > this note.

## Scope
The repository shape, the `scp` command, configuration, process lifecycle and health, build, CI, container image, release, dependency
updates and the test strategy, for one instance serving up to 1,000 repositories (several: cluster.md).

## Decisions (bullets with one-line reasons)
- Cobra tree built by `newRootCmd(stdout, stderr)` in `cmd/scp`; state lives in values passed down: each test builds a fresh tree.
- One `viper.New()` per command run, decoded with `UnmarshalExact`: each run is isolated and every key must exist in the `Config` struct.
- Precedence flags > `SCP_*` env > file > built-in defaults: the usual operator expectation; defaults live in one map (`defaults()`).
- Secrets come from files (`*_file` keys) into a `config.Secret` type that redacts in `String`, `GoString`, `LogValue` and `MarshalText`.
- Exit codes: 0 ok, 1 runtime failure (unreadable file, bind error, unclean shutdown), 2 refused input (bad flag, invalid config);
  `run` returns the code.
- Logs: stdlib `log/slog`, JSON (or text) to stderr; one `*slog.Logger` passed down, `slog.SetDefault` in `serve` so library logs share
  the format.
- Public `:9090` serves the pages (through the Next.js server `scp serve` supervises, web.md), `/api/v1`, badges, llms and the webhook;
  admin `:9091` (internal) serves `/livez`, `/readyz`, `/metrics`, `/debug/pprof` and `/api/v1/status` (http-api.md Routes).
- `/readyz` stays 200 from its first success until shutdown: a single replica keeps serving last good data through a GitHub outage.
- Prometheus `client_golang` with an own `prometheus.Registry` plus Go and process collectors: the metric set is explicit and testable.
- One boundary test enforces the feature rule (features import no feature; core imports no feature): small, readable, fast.
- Go build tools (`oapi-codegen`, `govulncheck`) are `tool` directives in the main `go.mod`, run with `go tool <name>`: one module, one
  lockfile, versions updated like any dependency.
- The binary holds no web files: the web UI is the separate, platform-independent `web/bundle` (`web.dir`), so `go build` and
  `go test ./...` run with Go alone and one bundle serves every platform.
- Renovate updates every pin (Go, Node, pnpm, golangci-lint, GoReleaser, images by digest, actions by SHA) after a 7-day cooldown.
- Generated code is committed and drift-checked by `make check` and CI: Go server code in `internal/httpapi/oapi`, `web/src/styles/chroma.css`,
  web client types in `web/src/lib/api/schema.d.ts`.

## Layout
Root files: `go.mod` (`module github.com/cantara/source-code-portal`, `go 1.27.2`, `tool` directives), `go.sum`, `Makefile`, `.golangci.yml`,
`.goreleaser.yaml`, `renovate.json`, `.editorconfig`, `.gitattributes`, `.gitignore`, `.dockerignore`, `README.md`, `LICENSE` (Apache-2.0),
`SECURITY.md`, `CHANGELOG.md`, `AGENTS.md`, `CLAUDE.md` (imports `AGENTS.md`).

| Dir | Holds |
|---|---|
| `.github/workflows/` | `ci.yml`, `release.yml` |
| `api/` | `openapi.yaml`, the OpenAPI 3.1 contract for `/api/v1` |
| `cmd/scp/` | `main.go`, the Cobra tree, wiring (including the webhook sink that calls `Notify`), metrics, status, the boundary test |
| `internal/` | the server packages, each created with its first code; `testdata/` beside a package's tests |
| `web/` | the Next.js web UI (pnpm); `pnpm build` assembles `web/bundle` |
| `deploy/` | `Dockerfile`, `Dockerfile.release`, `config.example.yaml` |
| `docs/` | user documentation (operator guide, repository guide, API guide) |
| `.claude/work/` | planning: `plan.md`, `decisions.md`, `design/`; finished work in `history/` |

## CLI and config (with a minimal example YAML)
`scp` with persistent flags `--config/-c <path>` (YAML file; without it, defaults plus env) and `--log-level debug|info|warn|error`.
- `scp serve [--public-addr :9090] [--admin-addr :9091]`: runs the portal until SIGINT or SIGTERM.
- `scp config check`: loads and validates exactly like `serve`, prints the effective settings as YAML (secret file paths, never their
  contents); group warnings go to stderr; exit 0, 1 or 2.
- `scp version [--json]`: version and build (`-X main.version`, `-X main.build`), a modified flag, Go version, platform.
Cobra settings: `SilenceUsage`, `SilenceErrors`; `main` calls `run(ctx, args, stdout, stderr) int`, which prints an error once to stderr
as `scp: <error>`.

Loading (`internal/config.Load(path string, flags *pflag.FlagSet) (Config, map[string]any, error)`), in order:
1. `v := viper.New()`; `SetDefault` for every key of `defaults()`, so env lookup knows every key.
2. `SetConfigFile(path)` and `ReadInConfig` when `--config` is given; an unreadable file is exit 1, a YAML parse error exit 2.
3. `SetEnvPrefix("SCP")`, `SetEnvKeyReplacer(strings.NewReplacer(".", "_"))`, `AutomaticEnv()`: `server.public_url` is `SCP_SERVER_PUBLIC_URL`.
   A set `SCP_*` variable that names no key is a problem, so an env typo fails as loudly as a file typo.
4. `BindPFlag` for the changed flags (`log-level`, `public-addr`, `admin-addr`).
5. `UnmarshalExact(&cfg)`; then `normalise`, `validate` (every problem with its key path) and `readSecrets` (missing or empty: problem).
6. All problems are returned together as `*config.Invalid`; `cmd/scp` then compiles the groups (`catalog.Compile`), where errors are
   problems and warnings are printed.
Each key is added with the code that reads it; `docs/operator-guide.md` lists every key.

```yaml
server: { public_addr: ":9090", admin_addr: ":9091", public_url: "https://scp.example.org", shutdown_grace: 20s }
log: { level: info, format: json }          # format: json | text (text for local development)
store: { path: /data/scp.db }
github:
  sources:
    - id: github
      orgs: [Cantara]
      app: { id: 123456, private_key_file: /run/secrets/github-app.pem }   # or token_file: /run/secrets/github-token
catalog:
  reconcile_interval: 5m
  groups:
    - { id: platform, name: Platform, default_repo: source-code-portal, match: [{ prefix: "scp-" }, { topic: platform }] }
webhook: { secret_file: /run/secrets/webhook-secret }
```

## Runtime (logging, startup, shutdown, health)
Logging: JSON lines on stderr (`log.format: text` for local development) at `log.level` (default info); errors as `err`; the web UI
server's output carries `component=web`. One access line `request` per request at info (listener, route pattern, method, status, bytes,
duration, request id, client IP). Secrets log as `[redacted]` (`Secret.LogValue`).

Startup order (`serve`): 1 load and validate config (exit 2); 2 build the logger; 3 open SQLite and migrate (quarantine on doubt,
sync-snapshot-store.md); 4 build the GitHub client, the engine, the cluster member (when `cluster.addresses` is set), the features, the
metrics, the web UI supervisor, the webhook sink and the HTTP handlers; 5 bind both listeners (bind error: exit 1); 6 run the engine
(warm start from the store, then the first listing; GitHub traffic begins here) and the member, serve both listeners and start the web UI
server (`internal/webui`, when `web.dir` is set) with `SCP_API` pointing at the public listener on loopback.

Shutdown on the first SIGINT or SIGTERM (`signal.NotifyContext`), within `server.shutdown_grace` (default 20 s, inside Kubernetes'
30 s): 1 `/readyz` answers 503; 2 `http.Server.Shutdown` on both listeners lets in-flight requests finish; 3 the web UI server gets
SIGTERM (killed after 5 s); 4 the cluster member leaves; 5 the engine stops and cancels jobs in flight; 6 pending snapshot updates are
dropped; 7 the store closes; exit 0, or 1 when a step failed. After the first signal the default handling returns, so a second signal
ends the process at once.

Health (admin port): `/livez` is process-local and calls nothing outbound: 200 while the sync dispatcher heartbeat is fresh (3 minutes).
`/readyz` is 200 once a snapshot with a catalogue is published (warm, or after the first complete listing on a cold start) and the web UI
server, when configured, has answered once; otherwise 503. It stays 200 through GitHub errors, which show as item freshness. Kubernetes:
`startupProbe` on `/readyz` with a budget for a cold start of 1,000 repositories, `livenessProbe` `/livez`, `readinessProbe` `/readyz`.

## Build, CI, image, release
Makefile (each target is one command that CI also runs; `PNPM`, `VERSION`, `CONFIG`, `BUILD_ID` are variables; `GOLANGCI_LINT_VERSION`
2.14.0 is updated by Renovate):

| Target | Does |
|---|---|
| `help` | lists targets (default goal) |
| `web` | `pnpm -C web install --frozen-lockfile`, `pnpm -C web build` -> `web/bundle` |
| `build` | `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version -X main.build"` -> `bin/scp` |
| `gen` | `go generate ./...` (oapi-codegen into `internal/httpapi/oapi`, `chroma.css`) and `pnpm -C web gen` (`schema.d.ts`) |
| `lint` | `go vet`, `golangci-lint run` (version-checked), `go tool govulncheck ./...`, `pnpm lint`, `pnpm typecheck`, `pnpm format:check` |
| `test` | `CGO_ENABLED=1 go test -race -count=1 -timeout 10m ./...` and `pnpm test` (`vitest`) |
| `e2e` | `web`, then `pnpm -C web e2e`: Playwright against `cmd/scp/e2e_test.go` (Go supervising the bundle, the fake GitHub) |
| `check` | the gate: `gen`, then `git diff --exit-code`, `go mod tidy -diff`, `lint`, `test`, `e2e` |
| `image` | `docker build -f deploy/Dockerfile --build-arg VERSION --build-arg BUILD_ID -t scp:dev .` |
| `run` | `build` and `web`, then `SCP_WEB_DIR=web/bundle bin/scp serve --config $(CONFIG)` (default `scp.yaml`, a copy of `deploy/config.example.yaml`) |
| `dev` | `go run ./cmd/scp serve` on :9090 plus `next dev` on :3000 |
| `clean` | removes `bin/`, `dist/`, `web/.next`, `web/bundle`, the Playwright reports |

CI (`ci.yml` on pull request, push and merge queue; `permissions: {}` with `contents: read` per job; actions pinned by SHA;
`GOTOOLCHAIN=local`): job `go` (with a Hazelcast service: mod verify and tidy, `go generate` drift, golangci-lint action, vet,
`go test -race`, govulncheck); job `web` (Node from `web/.node-version`, pinned pnpm, frozen install, `gen` drift, lint, typecheck,
format, `vitest`, build); job `e2e` (Playwright Chromium, bundle build, `pnpm -C web e2e`); job `image` (`make image`, then
`docker run --read-only --cap-drop=ALL scp:dev version` and a Node check for `/web/server.js`); job `gate` (`if: always()`, `needs` all
other jobs, fails when any failed, was cancelled or skipped) is the one required check; a new job joins the gate's `needs` list.

Image (`deploy/Dockerfile`, every base by digest): `node:24-alpine` with pnpm builds `web/bundle` -> `golang:1.27.2-alpine` builds the
static binary and runs `scp version` -> `gcr.io/distroless/nodejs24-debian13:nonroot` with `/scp`, the bundle in `/web`,
`SCP_WEB_DIR=/web` and `SCP_WEB_NODE=/nodejs/bin/node`; UID 65532, `/data` the writable volume for SQLite, `EXPOSE 9090 9091`,
`ENTRYPOINT ["/scp"]`, `CMD ["serve"]`, OCI labels. It runs with a read-only root filesystem and all capabilities dropped; SQLite uses
`temp_store=MEMORY`, so `/data` is the only writable mount.

Release (`release.yml` on tags `v*`): `make web`, then GoReleaser v2: binaries `linux/amd64`, `linux/arm64`, `darwin/arm64`, tar.gz with
`LICENSE`, `README.md` and `deploy/config.example.yaml`, the bundle as `scp-web_<version>.tar.gz` (reproducible tar from a `before`
hook), sha256 checksums signed keyless by cosign; a `linux/amd64`+`linux/arm64` image from `deploy/Dockerfile.release` (distroless
Node.js around the GoReleaser binary and the bundle) pushed to `ghcr.io/cantara/source-code-portal:<version>` and `:latest` for
non-prereleases, with an SBOM, signed by digest; changelog from GitHub, without `chore(deps)`, `docs`, `test` and `ci` commits. The same
run then runs the published image (`scp version`, `/web/server.js`) read-only with all capabilities dropped and verifies its signature.
`CHANGELOG.md` holds the human summary of each release.

Renovate (`renovate.json`): `config:recommended`, `helpers:pinGitHubActionDigests`, `docker:pinDigests`, `chore` commit type; the
built-in managers (gomod with tool directives, npm in `web/`, github-actions, dockerfile) and regex managers for golangci-lint (Makefile,
CI), pnpm (CI, Dockerfile) and GoReleaser (release workflow); `minimumReleaseAge` 7 days, Mondays before 06:00 Europe/Oslo,
`postUpdateOptions: [gomodTidy]`, grouped toolchain updates (Go image with `go.mod`, Node image with `.node-version`), vulnerability
alerts at any time, `ignorePaths: [".claude/**"]`.

## Tests (strategy)
- Unit and table tests with stdlib `testing` and `go-cmp`; hand-written fakes in `_test.go`, named after the behaviour they provide.
- Golden files beside the test: `internal/badges/testdata/last-commit.golden.svg`, `internal/llms/testdata/` (`llms.txt`, `llms-full.txt`,
  `readme.md`, refreshed with `UPDATE_GOLDEN=1`).
- Native fuzzing for the parsers of untrusted input: `FuzzParseDescriptor`, `FuzzRender`, `FuzzSanitizeFixpoint`, `FuzzParsers`
  (manifests), `FuzzNormalise` (webhook), `FuzzDelimit` (llms). Seed corpora run in every `go test`.
- `testing/synctest` for everything with time: scheduler, coalescing, retry, rate limiting, debounce.
- `internal/github/githubtest`: an `httptest` fake GitHub with fault injection (rate limits, 5xx, ETag 304); it drives the client, sync
  and the `cmd/scp` end-to-end tests (`TestServeEndToEnd`, `TestExitCodes`, `TestConfigCheckPrintsEffectiveSettings`).
- Offline: tests dial only loopback listeners they create; `forbidigo` flags `http.DefaultClient` and `http.Get`/`Head`/`Post`/`PostForm`.
- `goleak.VerifyTestMain` in every package that starts goroutines; `-race` with cgo for tests, releases built with `CGO_ENABLED=0`.
- SQLite tests use real files in `t.TempDir()`.
- Web: `vitest` with MSW for components and hooks; Playwright against Go supervising the built bundle and the fake GitHub.
- Boundary test `cmd/scp/boundary_test.go`: a `features` slice (`catalog commits docs badges webhook llms manifests`); walk each
  `internal/<dir>`, read imports with `parser.ImportsOnly`, report an import of a feature other than the file's own
  (`TestFeaturesDoNotImportFeatures`; this covers core directories too). `TestBoundaryCheckFindsViolation` runs the same check on a
  temporary tree holding one violation and expects exactly one error.
