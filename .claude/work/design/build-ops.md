# Build, CLI, config and operations
Status: v1 design note, 2026-10-14. Binding order: ../plan.md > this note > ../reference/.

## Scope (v1)
The repository shape, the `scp` command, configuration, process lifecycle and health, build, CI, container image, release, dependency
updates and the test strategy, for one instance serving up to 1,000 repositories (several: cluster.md).

## Decisions (bullets with one-line reasons)
- Cobra tree built by `newRootCmd()` in `cmd/scp`; state lives in values passed down: each test builds a fresh tree.
- One `viper.New()` per command run, decoded with `UnmarshalExact`: each run is isolated and every key must exist in the `Config` struct.
- Precedence flags > `SCP_*` env > file > built-in defaults: the usual operator expectation; defaults live in one Go struct.
- Secrets come from files (`*_file` keys) into a `config.Secret` type that redacts in `String`, `LogValue` and `config check`.
- Exit codes: 0 ok, 1 runtime failure (unreadable file, bind error), 2 refused input (bad flag, invalid config); `run` returns the code.
- Logs: stdlib `log/slog`, JSON to stderr; one `*slog.Logger` passed down, `slog.SetDefault` in `main` so library logs share the format.
- Public `:9090` serves the pages (through the Next.js server `scp serve` supervises, web.md), `/api/v1`, badges, llms and the webhook; admin `:9091` (internal) serves `/livez`, `/readyz`, `/metrics`,
  `/debug/pprof` and `/api/v1/status` (http-api.md Routes).
- `/readyz` stays 200 from its first success until shutdown: a single replica keeps serving last good data through a GitHub outage.
- Prometheus `client_golang` with an own `prometheus.Registry` plus Go and process collectors: the metric set is explicit and testable.
- One boundary test enforces plan rule 2 (features import no feature; core imports no feature): small, readable, fast.
- Go build tools (`oapi-codegen`, `govulncheck`) are `tool` directives in the main `go.mod` (`go get -tool`), run with `go tool <name>`:
  one module, one lockfile, versions updated like any dependency.
- The binary holds no web files: the web UI is the separate, platform-independent `web/bundle` (`web.dir`), so `go build` and
  `go test ./...` run with Go alone and one bundle serves every platform.
- Renovate updates every pin (Go, Node, pnpm, golangci-lint, images by digest, actions by SHA) after a 7-day cooldown: current and reviewed.
- Generated code is committed and drift-checked by `make check`: Go server code in `internal/httpapi/oapi`, web client types in
  `web/src/lib/api/schema.d.ts`, both from `api/openapi.yaml`.

## Layout
Root files: `go.mod` (`module github.com/cantara/source-code-portal`, `go 1.27.2`, `tool` directives), `go.sum`, `Makefile`, `.golangci.yml`,
`.goreleaser.yaml`, `renovate.json`, `.editorconfig`, `.gitattributes`, `.gitignore`, `.dockerignore`, `README.md`, `LICENSE` (Apache-2.0),
`SECURITY.md`, `CHANGELOG.md`, `AGENTS.md`, `CLAUDE.md` (imports `AGENTS.md`).

| Dir | Holds |
|---|---|
| `.github/workflows/` | `ci.yml` (M0), `release.yml` (M6); `.github/pull_request_template.md` |
| `api/` | `openapi.yaml`, the OpenAPI 3.1 contract for `/api/v1` |
| `cmd/scp/` | `main.go`, the Cobra tree, wiring (including the webhook sink that calls `sync.Notify`), the boundary test |
| `internal/` | the packages of plan section 3, each created with its first code; `testdata/` beside each package's tests |
| `web/` | the Next.js web UI (pnpm); `pnpm build` assembles `web/bundle` |
| `deploy/` | `Dockerfile`, `Dockerfile.release`, `config.example.yaml` |
| `docs/` | user documentation (operator guide, API guide) |
| `.claude/work/` | planning: `plan.md`, `decisions.md`, `design/`, `reference/` |

## CLI and config (with a minimal example YAML)
`scp` with persistent flags `--config/-c <path>` (YAML file; without it, defaults plus env) and `--log-level debug|info|warn|error`.
- `scp serve [--public-addr :9090] [--admin-addr :9091]`: runs the portal until SIGINT or SIGTERM.
- `scp config check [--json]`: loads and validates exactly like `serve`, prints the effective config with secrets as `[redacted]`; exit 0 or 2.
- `scp version [--json]`: version and commit (`-X main.version`, `-X main.build`), Go version, platform.
Cobra settings: `SilenceUsage`, `SilenceErrors`; `main` calls `run(ctx, args, stdout, stderr) int`, which prints an error once to stderr.

Loading (`internal/config.Load(path string, flags *pflag.FlagSet) (Config, error)`), in order:
1. `v := viper.New()`; `SetDefault` for every key from the `Defaults()` struct, so env lookup knows every key.
2. `SetConfigFile(path)` and `ReadInConfig` when `--config` is given; an unreadable file is exit 1.
3. `SetEnvPrefix("SCP")`, `SetEnvKeyReplacer(strings.NewReplacer(".", "_"))`, `AutomaticEnv()`: `server.public_url` is `SCP_SERVER_PUBLIC_URL`.
   A set `SCP_*` variable that maps to no known key is a violation, so an env typo fails as loudly as a file typo.
4. `BindPFlag` for the flags above (`log.level`, `server.public_addr`, `server.admin_addr`).
5. `UnmarshalExact(&cfg)` with `mapstructure` tags and the duration hook; then each `*_file` secret is read (missing or empty: violation).
6. `cfg.Validate()` collects every violation with its key path (for example: no `github.sources`; both or neither of `app` and
   `token_file`; an org in two sources; a bad matcher regexp; a duplicate group id; `catalog.reconcile_interval` below 1m) and joins them.
Each key is added with the code that reads it; `scp config check` output is the reference until the M6 operator guide.

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
Logging: JSON lines on stderr with `time`, `level`, `msg`; a `component` attribute per package logger; errors as `err`; levels debug,
info (default), warn, error. One access line per request at info (method, route pattern, status, bytes, duration); probe and
`/metrics` requests at debug. Secrets log as `[redacted]` (`Secret.LogValue`). Each startup step logs `startup step` with `step`, `took`.

Startup order (`serve`): 1 load and validate config (exit 2); 2 build logger and metrics registry; 3 open SQLite and migrate (exit 1
when the file can be neither opened nor re-created); 4 load the last good state from SQLite and publish snapshot 1 (warm start); 5 build
the GitHub client, sync, features, the webhook sink and HTTP handlers; 6 bind both listeners (bind error: exit 1); 7 start serving and
start the sync scheduler, which enqueues the first listing; GitHub traffic begins here; 8 start the web UI server (`internal/webui`, when
`web.dir` is set) with `SCP_API` pointing at the public listener on loopback. Pages show the warm snapshot, or "warming up" on a
cold start.

Shutdown on the first SIGINT or SIGTERM (`signal.NotifyContext`), within `server.shutdown_grace` (default 20 s, inside Kubernetes'
30 s): 1 `/readyz` answers 503; 2 `http.Server.Shutdown` on both listeners lets in-flight requests finish while the scheduler stops
starting jobs; 3 the web UI server gets SIGTERM (killed after 5 s); 4 running jobs get the rest of the grace, then their context is
cancelled; 5 the store closes; 6 exit 0. A second signal
or an expired grace exits 1.

Health (admin port): `/livez` is process-local and calls nothing outbound: 200 while the process serves and the sync dispatcher
heartbeat is fresh (heartbeat from M1; 200 in M0). `/readyz` is 200 once the store is open, a snapshot with a catalogue is published and the web UI server (when configured) has answered once
(warm, or after the first complete listing on a cold start), otherwise 503 with the reason; it stays 200 through GitHub errors, which
show as item freshness. Kubernetes: `startupProbe` on `/readyz` with a budget for a cold start of 1,000 repositories, `livenessProbe`
`/livez`, `readinessProbe` `/readyz`.

## Build, CI, image, release
Makefile (GNU make 3.81 compatible; each target is one command that CI also runs; tool versions are variables Renovate updates):

| Target | Does |
|---|---|
| `help` | lists targets (default goal) |
| `web` | `pnpm -C web install --frozen-lockfile && pnpm -C web build` -> `web/bundle` |
| `build` | `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X ..."` -> `bin/scp` |
| `gen` | `go tool oapi-codegen` (strict server into `internal/httpapi/oapi`), `openapi-typescript` into `web/src/lib/api/schema.d.ts`, `go generate ./...` |
| `lint` | `go vet`, `golangci-lint run` (version-checked), `go tool govulncheck ./...`, `pnpm lint`, `pnpm typecheck`, `pnpm format:check` |
| `test` | `CGO_ENABLED=1 go test -race -count=1 -timeout 10m ./...` and `pnpm test` (vitest) |
| `check` | the PR gate: `gen` then `git diff --exit-code`, `go mod tidy -diff`, `lint`, `test`, `e2e` |
| `e2e` | `web`, then Playwright against `cmd/scp/e2e_test.go` (Go supervising the bundle, the fake GitHub) |
| `image` | `docker build -f deploy/Dockerfile --build-arg VERSION --build-arg BUILD_ID -t scp:dev .` |
| `run` | `build` and `web`, then `SCP_WEB_DIR=web/bundle bin/scp serve --config $(CONFIG)` (default `scp.yaml`, a local copy of `deploy/config.example.yaml`) |
| `dev` | `scp serve` on :9090 plus `next dev` on :3000 (web.md decisions) |
| `clean` | removes `bin/`, `dist/`, `web/.next`, `web/bundle` |

CI (`ci.yml` on pull request, push to `main` and merge queue; `permissions: {}` with `contents: read` per job; actions pinned by SHA;
`GOTOOLCHAIN=local`, Go from `go.mod`): job `go` (mod verify and tidy, golangci-lint action, vet, `go test -race`, govulncheck); job `web`
(Node from `web/.node-version`, pinned pnpm, frozen install, lint, typecheck, vitest, build); job `image` (`make image`, then
`docker run --read-only --rm scp:dev version` and a Node check for `/web/server.js`); job `e2e` (bundle build plus Playwright); job `gate` (`if: always()`, `needs` all
other jobs, succeeds when all succeeded) is the one required check in branch protection; a new job joins the gate's `needs` list.

Image (`deploy/Dockerfile`, every base by digest): `node:24-alpine` with pnpm builds `web/bundle` -> `golang:1.27.2-alpine` builds the
static binary and runs `scp version` -> `gcr.io/distroless/nodejs24-debian13:nonroot` with `/scp`, the bundle in `/web`,
`SCP_WEB_DIR=/web` and `SCP_WEB_NODE=/nodejs/bin/node`; UID 65532, `/data` the writable volume for SQLite, `EXPOSE 9090 9091`,
`ENTRYPOINT ["/scp"]`, `CMD ["serve"]`, OCI labels for source, version, revision and licence. It runs with `readOnlyRootFilesystem`
and all capabilities dropped (the Next.js server writes nothing); SQLite uses `temp_store=MEMORY`, so `/data` is the only writable
mount. About 67 MB compressed.

Release (`release.yml` on tags `v*`, `contents: write`, `packages: write`, `id-token: write`): `make web`, then GoReleaser v2:
binaries `linux/amd64`, `linux/arm64`, `darwin/arm64`, tar.gz with `LICENSE`, `README.md` and `deploy/config.example.yaml`, the bundle
as `scp-web_<version>.tar.gz` (reproducible tar from a `before` hook), sha256 checksums over all of them; a multi-arch image from
`deploy/Dockerfile.release` (distroless Node.js, copies the GoReleaser binary and the bundle) pushed to `ghcr.io/cantara/source-code-portal:<version>` and
`:latest` for full releases, with a BuildKit SBOM; cosign keyless signatures of the image and the checksums file; changelog from
Conventional Commits; the GitHub release is published by the same run, which then runs the published image (`scp version`) and verifies
its signature and the bundle. `CHANGELOG.md` holds the human summary of each release.

Renovate (`renovate.json`, self-contained): `config:recommended`, `helpers:pinGitHubActionDigests`, `docker:pinDigests`; managers gomod
(the one `go.mod`, tool directives included), npm (`web/`), github-actions, dockerfile, and one regex manager for the Makefile tool
versions; `minimumReleaseAge` 7 days, weekly schedule, `postUpdateOptions: [gomodTidy]`, grouped toolchain updates (Go image with
`go.mod`, Node image with `.node-version`), vulnerability alerts at any time, every update merged by a person after CI,
`ignorePaths: [".claude/**"]`.

## Tests (strategy)
- Unit and table tests with stdlib `testing` and `go-cmp`; hand-written fakes in `_test.go`, named after the behaviour they provide.
- Golden files beside the test (`testdata/*.golden`, refreshed with `-update` through one small helper) for rendered HTML, SVG badges,
  `llms.txt`, Markdown output and `config check` output.
- Native fuzzing for every parser of untrusted input: group matchers, descriptor (`scp.yaml`), Markdown rendering with the `SafeHTML`
  sanitiser, link and image rewriting, webhook signature and payload decoding. Seed corpora run in every `go test`; crashers become seeds.
- `testing/synctest` for everything with time: scheduler, job queue de-duplication, retry loop, rate limiting, shutdown order.
- `internal/github/githubtest`: an `httptest` fake GitHub with recorded fixtures and fault injection (403 and 429 rate limits, 5xx,
  ETag 304, token expiry); it drives the client, sync and one `cmd/scp` end-to-end test that boots `run()` and waits for `/readyz`.
- Offline: tests dial only loopback listeners they create; `forbidigo` flags `http.DefaultClient` and `http.Get`.
- `goleak.VerifyTestMain` in every package that starts goroutines; `-race` in CI with cgo for tests, releases built with `CGO_ENABLED=0`.
- SQLite tests use real files in `t.TempDir()`, outside synctest bubbles; one test deletes the file and resyncs (plan rule 5).
- Web: vitest for components and hooks; Playwright against Go supervising the built bundle and the fake GitHub.
- Boundary test `cmd/scp/boundary_test.go`, about 50 lines: a `features` slice (`catalog commits docs badges webhook llms`); walk each
  `../../internal/<dir>` with `filepath.WalkDir`; read imports with `parser.ParseFile(fset, p, nil, parser.ImportsOnly)`; for an import
  `github.com/cantara/source-code-portal/internal/<f>...` where `<f>` is a feature other than the file's own feature, `t.Errorf` with
  file and import (this covers core directories too: any feature import from core is reported). A second case runs the same function
  on a temporary tree holding one violation and expects exactly one error.

## From the archive seed
M0 copies these files from `reference/detailed-design/50-repo-seed/repo/` and edits them to match this note:
- `go.mod` and `go.sum` (with `oapi-codegen` and `govulncheck` as `tool` directives), `.editorconfig`, `.gitattributes`, `.gitignore`,
  `.dockerignore`.
- `.golangci.yml` (linter set and settings; add `forbidigo` for `http.DefaultClient` and `http.Get`).
- `Makefile` (header, tool-version variables, `BUILD_ID`, `VERSION`, `LDFLAGS`, `export BUILD_ID`, `need-golangci`; the targets above).
- `.github/workflows/ci.yml` (`gate` job, SHA pins, `permissions`, `GOTOOLCHAIN=local`; with the `web` and `image` jobs added) and
  `.github/pull_request_template.md`.
- `deploy/Dockerfile`, `deploy/Dockerfile.release`, `.goreleaser.yaml` (builds, archives, checksums, `dockers_v2`, `docker_signs`,
  changelog; `IMAGE_REPO` set to `ghcr.io/cantara/source-code-portal`), `renovate.json`.
- `web/`: `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml` (supply-chain policy), `.node-version`, `tsconfig.json`,
  `tsconfig.app.json`, `tsconfig.node.json`, `vite.config.ts`, `vitest.config.ts`, `playwright.config.ts`, `eslint.config.js`,
  `.prettierrc.json`, `.prettierignore`, `.gitignore`, `index.html`, `public/`, `src/main.tsx`, `src/app/`,
  `src/styles/index.css`, `src/test/setup.ts`, `src/test/render.tsx`, `web.go`, `embed_spa.go`, `embed_placeholder.go`; scripts in
  `package.json` set to `dev`, `build`, `test`, `lint`, `typecheck`, `format:check`, `gen`, and the lockfile refreshed.
- `cmd/scp/buildinfo.go`; `cmd/scp/main.go` and `run.go` as the base for the Cobra version.
- `LICENSE`, `SECURITY.md`, `CHANGELOG.md`, `docs/README.md`, `api/openapi.yaml`; `README.md`, `AGENTS.md`, `CLAUDE.md` written short
  from `plan.md` and this note.
