# 90 - Configuration key reference (all units, 2026-10-14)

Written by the cross-design consistency review (`30-verification/cross-design-consistency.md`). It is the **single complete table** of configuration keys that the eighteen unit designs define, with the owner unit, the type, the default and the environment name. It is generated from the section 6 tables of the unit designs **after** the review's edits (the table of a unit is still the authority for validation details; this document is the authority for *which unit owns which key*, and for the conflicts it settled). Where a row's `Env` says "(derived)" the unit's table gave no explicit name and the name follows rule 3 below.

## 1. Rules that every key obeys (unit 02, section 4.3.5 and 6.1)

1. One typed, versioned YAML (`version: 1`), strict decode (an unknown key is an error), layered: embedded default < file (`--config`) < `SCP_` environment < flags (`--set path=value`). Secrets are files (`secret.Secret`, `{file: /path}`); their environment twin is `SCP_<PATH>_FILE`.
2. Durations are Go duration strings; sizes are `int64` tagged `scp:"unit=bytes"` (IEC units `16KiB`, `2MiB`, or a plain integer; decimal `MB` is refused; the tables below say `ByteSize`, `size` or `bytes int` for the same thing); rates are text (`120/min burst 60`).
3. Environment name: `SCP_` + the dotted path upper-cased with `.` and `-` replaced by `_`; a list element is addressed by its zero-based index (`SCP_GITHUB_SOURCES_0_AUTH_KIND`); a list of scalars is one variable with a comma-separated value; an environment variable never creates a map or list entry that is not in the embedded default or the file; two keys that give the same name are refused by `config.Describe`.
4. Every key has exactly one **owner** (the unit whose `Config` struct declares it) and at least one consumer; a key with no consumer fails `TestEveryConfigKeyIsConsumed` (GR-04, GR-12). The root struct is `cmd/scp/config.go` and only composes owner types.
5. Cross-section rules (private visibility needs an access mode, a webhook route needs a secret, `trusted_proxies` never `/0`) are in the root `Validate` (02 6.5, 16 3.4).

## 2. Root sections and their owners (the platform root structure, verified)

The root `Config` of unit 16 (`cmd/scp/config.go`, section 3.4) was checked against this table. **Before the review it lacked `llms` and typed `manifests.providers` as a struct; it now has all 24 sections** (CD-CF-01).

| Section | Go type | Owner | Sections of the architecture 16 outline |
|---|---|---|---|
| `version`, `server`, `log`, `telemetry`, `ratelimit`, `shutdown` | root, `httpx.Config`, `logx.Config`, `telemetry.Config`, `httpx.RateConfig`, `lifecycle.Config` + `supervisor_grace` | 02 (shutdown.supervisor_grace: 16) | yes |
| `store` | `store/sqlite.Config` | 03 | yes (`cache_caps.depsdev` is 13's consumer key) |
| `github` | `github.Config` (05) with the webhook, redelivery and discovery keys that 04 and 08 consume declared in 05's structs | 05 | yes |
| `catalog` | `catalog.Config` | 08 | yes |
| `refresh` | `RefreshConfig` (cmd/scp type, keys of 04) | 04 | yes |
| `site` | `overview.SiteConfig` | 15 | yes |
| `readiness`, `ops` | `ops.ReadinessConfig`, `ops.Config` | 15 | `readiness` yes; `ops` added |
| `live` | `live.Config` | 15 | yes |
| `llms` | `overview.LLMsConfig` | 15 | **added** (the outline has none; 15 R-02-2) |
| `commits`, `badges` | `commits.Config`, `badges.Config` | 09 | yes |
| `docs`, `pages`, `media` | `docs.Config`, `pages.Config`, `media.Config` | 10 | yes |
| `manifests` | `manifests.Config` (11) inline + `ecosystems`, `enrich`, `providers[]` (13) | 11 (13 for its three subtrees; 12 owns no key) | yes (`allow_list` of the outline does not exist: see 6) |
| `integrations`, `access` | `integrations.Config`, `access.Config` | 14 | yes |
| `web` | `webapp.Config` | 06 | yes |

## 3. The table

Rows are grouped by root section. `Read by` lists the other units whose section 6 names the key as a key they read.

### `version`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `version` | int | `1` | `SCP_VERSION` | 02 platform | 16 | enum `[1]` (the schema reports an unsupported version before anything else) ; consumer: the loader (allow-listed in the consumption test); a future `2` is read by `s... |

### `server`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `server.public.listen` | string | `:9090` | `SCP_SERVER_PUBLIC_LISTEN` | 02 platform | 06 | `host:port` with a **literal IP or empty host** (a name such as `localhost` is refused: it may re... ; consumer: `cmd/scp` (`net.Listen`), `httpx.NewServer` |
| `server.public.public_url` | string | `""` | `SCP_SERVER_PUBLIC_PUBLIC_URL` | 02 platform | 07, 10, 15 | empty, or `http(s)://host[:port]` with **no** userinfo, path, query or fragment ; consumer: `httpx.Deps.PublicURL` (CSRF trusted origin), `routes.Absolute` base (sitemap... |
| `server.public.hsts` | bool | `false` | `SCP_SERVER_PUBLIC_HSTS` | 02 platform |  | consumer: `httpx.Deps.HSTS` (only on HTTPS requests; leave false when the TLS-terminati... |
| `server.public.trusted_proxies` | []CIDR or address | `[]` | `SCP_SERVER_PUBLIC_TRUSTED_PROXIES` | 02 platform |  | each parses; **`/0` of either family refused**; bare address = /32 or /128 ; consumer: `httpx.ParseTrusted` -> `ClientIPResolver` |
| `server.admin.listen` | string | `:9091` | `SCP_SERVER_ADMIN_LISTEN` | 02 platform |  | as above; must differ from `public.listen` (except `:0` in tests) ; consumer: `cmd/scp`, `httpx.NewServer` |
| `server.admin.bearer_token` | secret | unset | `SCP_SERVER_ADMIN_BEARER_TOKEN` | 02 platform | 15 | when set, every admin request except the probes must present it ; consumer: `httpx.AdminChain` (new key, Deviation D5; GR-17 "optional bearer auth") |
| `server.admin.allowed_hosts` | []string | `[]` | `SCP_SERVER_ADMIN_ALLOWED_HOSTS` | 02 platform |  | each entry a host or host:port without space, `/` or `@` ; consumer: `httpx.Deps.AdminHosts` -> `AdminChain` Host guard (421 for any other `Host`;... |
| `server.timeouts.read_header` | duration | `5s` | `SCP_SERVER_TIMEOUTS_READ_HEADER` | 02 platform |  | 100ms..1m; must not exceed `read` ; consumer: `http.Server.ReadHeaderTimeout` |
| `server.timeouts.read` | duration | `30s` | `SCP_SERVER_TIMEOUTS_READ` | 02 platform |  | 1s..10m ; consumer: `ReadTimeout` |
| `server.timeouts.write` | duration | `60s` | `SCP_SERVER_TIMEOUTS_WRITE` | 02 platform |  | 1s..10m ; consumer: `WriteTimeout` (SSE extends per response) |
| `server.timeouts.idle` | duration | `120s` | `SCP_SERVER_TIMEOUTS_IDLE` | 02 platform |  | 1s..1h ; consumer: `IdleTimeout` |
| `server.timeouts.max_header_bytes` | size | `16KiB` | `SCP_SERVER_TIMEOUTS_MAX_HEADER_BYTES` | 02 platform |  | 1KiB..1MiB ; consumer: `MaxHeaderBytes` |
| `server.timeouts.max_header_values` | int | `100` | `SCP_SERVER_TIMEOUTS_MAX_HEADER_VALUES` | 02 platform |  | 16..4096 ; consumer: `MaxHeaderValueCount` (Go 1.27) |

### `log`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `log.level` | enum | `info` | `SCP_LOG_LEVEL` | 02 platform |  | `debug`, `info`, `warn`, `error` ; consumer: `logx.ParseLevel` in `cmd/scp` |
| `log.format` | enum | `json` | `SCP_LOG_FORMAT` | 02 platform |  | `json`, `text` ; consumer: `logx.Options.Format` |
| `log.add_source` | bool | `false` | `SCP_LOG_ADD_SOURCE` | 02 platform |  | consumer: `logx.Options.AddSource` |

### `telemetry`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `telemetry.prometheus` | bool | `true` | `SCP_TELEMETRY_PROMETHEUS` | 02 platform |  | consumer: `telemetry.New` (reader), `cmd/scp` (mounts `admin.metrics` `GET /metrics`: *... |
| `telemetry.otlp.enabled` | bool | `false` | `SCP_TELEMETRY_OTLP_ENABLED` | 02 platform |  | consumer: `telemetry.New` |
| `telemetry.otlp.endpoint` | string | `""` | `SCP_TELEMETRY_OTLP_ENDPOINT` | 02 platform |  | required when enabled; `http(s)://host[:port]` (base URL; `/v1/{traces,metrics,logs}` is appended) ; consumer: exporters |
| `telemetry.otlp.headers_file` | path | `""` | `SCP_TELEMETRY_OTLP_HEADERS_FILE` | 02 platform |  | absolute when set; <= 16 KiB; `Name: value` per line ; consumer: `cmd/scp` parses it into `Options.Headers` (secret-wrapped) |
| `telemetry.otlp.timeout` | duration | `10s` | `SCP_TELEMETRY_OTLP_TIMEOUT` | 02 platform |  | 1s..2m ; consumer: exporters |
| `telemetry.otlp.allow_private_cidrs` | []CIDR | `[]` | `SCP_TELEMETRY_OTLP_ALLOW_PRIVATE_CIDRS` | 02 platform |  | narrower than /0 ; consumer: `cmd/scp` -> `netguard.NewPolicy` for the exporter client |
| `telemetry.otlp.ports` | []int | `[]` | `SCP_TELEMETRY_OTLP_PORTS` | 02 platform |  | 1..65535 ; consumer: same |
| `telemetry.traces.sample_ratio` | float | `0.1` | `SCP_TELEMETRY_TRACES_SAMPLE_RATIO` | 02 platform |  | 0..1 ; consumer: `ParentBased(TraceIDRatioBased)` |
| `telemetry.runtime.enabled` | bool | `true` | `SCP_TELEMETRY_RUNTIME_ENABLED` | 02 platform |  | consumer: `runtime.Start` |

### `ratelimit`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `ratelimit.badges` | rate | `120/min burst 60` | `SCP_RATELIMIT_BADGES` | 02 platform |  | consumer: limiter class `badges` (`GET /badges/*`) |
| `ratelimit.api` | rate | `300/min burst 100` | `SCP_RATELIMIT_API` | 02 platform | 06, 07 | consumer: class `api` (`/api/v1/*`, and the server-text routes `/repos/*.md`, `llms*`, ... |
| `ratelimit.media_ext` | rate | `60/min burst 20` | `SCP_RATELIMIT_MEDIA_EXT` | 02 platform | 10 | consumer: class `media_ext` (`/media/ext/*`, the external image proxy) |
| `ratelimit.csp_reports` | rate | `30/min burst 10` | `SCP_RATELIMIT_CSP_REPORTS` | 02 platform | 06 | consumer: class `csp_reports` (`POST /csp-reports`) |
| `ratelimit.webhook_invalid` | rate | `10/min burst 10` | `SCP_RATELIMIT_WEBHOOK_INVALID` | 02 platform | 04 | consumer: class `webhook_invalid` behind `webhook.Limiter` (04) |
| `ratelimit.sse_per_ip` | int 1..1000 | `4` | `SCP_RATELIMIT_SSE_PER_IP` | 02 platform | 15 | consumer: `live` (concurrent SSE streams per client IP; `httpx` supplies the IP only) |
| `ratelimit.max_keys` | int 1000..5,000,000 | `50000` | `SCP_RATELIMIT_MAX_KEYS` | 02 platform |  | consumer: `LimiterOptions.MaxKeys` per class |
| `ratelimit.idle_ttl` | duration 10s..24h | `10m` | `SCP_RATELIMIT_IDLE_TTL` | 02 platform |  | consumer: `LimiterOptions.IdleTTL` and the sweep interval |

### `shutdown`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `shutdown.drain_delay` | duration | `5s` | `SCP_SHUTDOWN_DRAIN_DELAY` | 02 platform | 15 | 0..2m; `Validate`: below `server_timeout + store_timeout + telemetry_timeout` ; consumer: `lifecycle.Plan.DrainDelay` (architecture 8.16: 5 s, still serving) |
| `shutdown.server_timeout` | duration | `15s` | `SCP_SHUTDOWN_SERVER_TIMEOUT` | 02 platform |  | 1s..5m ; consumer: `httpx.ServeAll` grace (both listeners together) and the servers stage's `Sto... |
| `shutdown.store_timeout` | duration | `3s` | `SCP_SHUTDOWN_STORE_TIMEOUT` | 02 platform |  | 1s..5m ; consumer: store stage `StopTimeout` (final plan flush, `Close`: optimize, `wal_checkpoi... |
| `shutdown.telemetry_timeout` | duration | `2s` | `SCP_SHUTDOWN_TELEMETRY_TIMEOUT` | 02 platform |  | 1s..1m ; consumer: telemetry stage `Close` |
| `shutdown.supervisor_grace` | duration | `45s` | `SCP_SHUTDOWN_SUPERVISOR_GRACE` | 16 cmd/scp |  | `0s` to `10m`; `0` disables the check; `drain_delay` must be shorter (`Config.Validate`) ; consumer: startup warning when the worst case exceeds it; the k8s example's `terminatio... |

### `store`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `store.path` | string | `/data/scp.db` | `SCP_STORE_PATH` | 03 readmodel/store |  | non-empty; no NUL; not `:memory:`; not a `file:` URI ; consumer: `sqlite.Open` (`buildDSN`); also the base of the backup, quarantine and resto... |
| `store.sync` | enum | `normal` | `SCP_STORE_SYNC` | 03 readmodel/store |  | `normal` or `full` ; consumer: `rw` DSN pragma `synchronous` |
| `store.busy_timeout` | duration | `5s` | `SCP_STORE_BUSY_TIMEOUT` | 03 readmodel/store |  | 100ms..1m ; consumer: all three DSNs (`busy_timeout`) |
| `store.read_conns` | int | `2` | `SCP_STORE_READ_CONNS` | 03 readmodel/store |  | 1..8 ; consumer: `ro` pool size (`SetMaxOpenConns`) |
| `store.journal_size_limit` | ByteSize | `64MiB` | `SCP_STORE_JOURNAL_SIZE_LIMIT` | 03 readmodel/store |  | 1MiB..1GiB ; consumer: `rw` DSN pragma |
| `store.quarantine_keep` | int | `2` | `SCP_STORE_QUARANTINE_KEEP` | 03 readmodel/store |  | 1..10 ; consumer: `quarantine`/`pruneQuarantine`, `Restore` |
| `store.probe_interval` | duration | `30s` | `SCP_STORE_PROBE_INTERVAL` | 03 readmodel/store |  | 5s..10m ; consumer: `Run` worker (degraded-state write probe) |
| `store.backup.enabled` | bool | `false` | `SCP_STORE_BACKUP_ENABLED` | 03 readmodel/store |  | none ; consumer: `Run`, `Close` |
| `store.backup.interval` | duration | `1h` | `SCP_STORE_BACKUP_INTERVAL` | 03 readmodel/store |  | at least `1m` when enabled ; consumer: `Run` |
| `store.backup.path` | string | `""` = `<store.path>.bak` | `SCP_STORE_BACKUP_PATH` | 03 readmodel/store |  | must differ from `store.path` (cleaned, absolute) when enabled ; consumer: `Backup`, `Close`, `Info` |
| `store.backup.on_shutdown` | bool | `true` | `SCP_STORE_BACKUP_ON_SHUTDOWN` | 03 readmodel/store |  | none ; consumer: `Close` (only when `backup.enabled`) |
| `store.cache_caps.blob` | ByteSize | `64MiB` | `SCP_STORE_CACHE_CAPS_BLOB` | 03 readmodel/store |  | 1MiB..4GiB ; consumer: `cmd/scp` maps it to the namespace constant exported by `manifests` (`manifes... |
| `store.cache_caps.parse` | ByteSize | `128MiB` | `SCP_STORE_CACHE_CAPS_PARSE` | 03 readmodel/store |  | 1MiB..4GiB ; consumer: maps to `manifests.parse` |
| `store.cache_caps.locked` | ByteSize | `64MiB` | `SCP_STORE_CACHE_CAPS_LOCKED` | 03 readmodel/store |  | 1MiB..4GiB ; consumer: maps to `manifests.locked` (unit 13 section on locked components; resolves Q6) |
| `store.cache_caps.depsdev` | ByteSize | `16MiB` | `SCP_STORE_CACHE_CAPS_DEPSDEV` | 03 readmodel/store | 13 | 1MiB..256MiB ; consumer: maps to `manifests.depsdev` (unit 13 RR-15, which states that this key is "de... |

### `github`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `github.defaults.webhook.max_body` | size | `25MiB` | `SCP_GITHUB_DEFAULTS_WEBHOOK_MAX_BODY` (derived) | 05 github adapter | 04 | at most 25MiB ; consumer: **webhook** (`MaxBytesReader`) and github (`NewVerifier`, second guard) |
| `github.defaults.webhook.dedupe_ttl` | duration | `15m` | `SCP_GITHUB_DEFAULTS_WEBHOOK_DEDUPE_TTL` (derived) | 05 github adapter | 04 | consumer: **webhook** |
| `github.defaults.webhook.dedupe_max` | int | `10000` | `SCP_GITHUB_DEFAULTS_WEBHOOK_DEDUPE_MAX` (derived) | 05 github adapter | 04 | consumer: **webhook** |
| `github.defaults.webhook.max_concurrent` | int | `4` | `SCP_GITHUB_DEFAULTS_WEBHOOK_MAX_CONCURRENT` (derived) | 05 github adapter | 04 | 1..32 ; consumer: **webhook** (CD-CF-04: moved here from 04 section 6.2, one owner per key) |
| `github.defaults.webhook.acquire_wait` | duration | `2s` | `SCP_GITHUB_DEFAULTS_WEBHOOK_ACQUIRE_WAIT` (derived) | 05 github adapter | 04 | 0..9s (below GitHub's 10 s) ; consumer: **webhook** |
| `github.defaults.webhook.retry_after` | duration | `5s` | `SCP_GITHUB_DEFAULTS_WEBHOOK_RETRY_AFTER` (derived) | 05 github adapter | 04 | 1s..5m ; consumer: **webhook** |
| `github.defaults.webhook.body_timeout` | duration | `8s` | `SCP_GITHUB_DEFAULTS_WEBHOOK_BODY_TIMEOUT` (derived) | 05 github adapter | 04 | 1s..9s ; consumer: **webhook** |
| `github.sources[].webhook.enabled` | bool | `true` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_ENABLED` (derived) | 05 github adapter | 04 | a source with `enabled` and an empty secret is a startup error (GR-20) ; consumer: **cmd/scp** (`Sources.IDs`; CD-CF-04, key defined by 04) |
| `github.sources[].webhook.default` | bool | implied for a single source | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_DEFAULT` (derived) | 05 github adapter | 04 | at most one `true` ; consumer: **webhook** (`Sources.Default`; CD-CF-04, key defined by 04) |
| `github.sources[].webhook.secret` | secret | none | `SCP_GITHUB_SOURCES_0_WEBHOOK_SECRET_FILE` | 04 refresh/webhook |  | non-empty when `enabled`; **startup error** otherwise (GR-20) ; consumer: `github.Verifier` (current secret) |
| `github.sources[].webhook.previous_secret` | secret | none | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_PREVIOUS_SECRET` (derived) | 04 refresh/webhook |  | optional; must differ from `secret` ; consumer: `github.Verifier` (rotation overlap; new) |
| `github.sources[].webhook.public_url` | URL | none | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_PUBLIC_URL` (derived) | 05 github adapter | 04 | https ; consumer: doctor (`app.hook`), manifest flow |
| `github.sources[].webhook.redelivery.enabled` | bool | `false` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_REDELIVERY_ENABLED` (derived) | 05 github adapter | 04 | App sources only ; consumer: **cmd/scp** (registers `webhook.redelivery`) |
| `github.sources[].webhook.redelivery.interval` | duration | `6h` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_REDELIVERY_INTERVAL` (derived) | 05 github adapter | 04 | consumer: **cmd/scp** (`freshness.Policy` of the job) |
| `github.sources[].webhook.redelivery.max_per_run` | int | `50` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_REDELIVERY_MAX_PER_RUN` (derived) | 05 github adapter | 04 | 1..500 ; consumer: github (`FailedDeliveries` cap) |
| `github.sources[].webhook.redelivery.rate` | duration | `1s` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_REDELIVERY_RATE` (derived) | 05 github adapter | 04 | 0..1m ; consumer: **cmd/scp** (pause between `Redeliver` calls inside the job; CD-CF-03) |
| `github.sources[].webhook.redelivery.max_age` | duration | `72h` | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_REDELIVERY_MAX_AGE` (derived) | 05 github adapter | 04 | 1h..168h (github.com keeps 3 days, GHES documents 7) ; consumer: github (`since = max(since, now - max_age)`; CD-CF-03) |
| `github.defaults.api_version` | string | `auto` | `SCP_GITHUB_DEFAULTS_API_VERSION` | 05 github adapter |  | `auto` or `YYYY-MM-DD` ; consumer: github (4.2.4); per-source `sources[i].api_version` overrides |
| `github.defaults.http.timeout` | duration | `30s` | `SCP_GITHUB_DEFAULTS_HTTP_TIMEOUT` | 05 github adapter |  | not negative ; consumer: github (REST per-request deadline) |
| `github.defaults.http.graphql_timeout` | duration | `20s` | `SCP_GITHUB_DEFAULTS_HTTP_GRAPHQL_TIMEOUT` (derived) | 05 github adapter |  | not negative ; consumer: github (GitHub cuts GraphQL at 10 s; the margin covers queueing) |
| `github.defaults.http.response_header_timeout` | duration | `20s` | `SCP_GITHUB_DEFAULTS_HTTP_RESPONSE_HEADER_TIMEOUT` (derived) | 05 github adapter |  | consumer: cmd/scp client factory (`Network`) |
| `github.defaults.http.max_idle_conns_per_host` | int | `16` | `SCP_GITHUB_DEFAULTS_HTTP_MAX_IDLE_CONNS_PER_HOST` (derived) | 05 github adapter |  | consumer: cmd/scp client factory |
| `github.defaults.limits.file_bytes` | size | `1MiB` | `SCP_GITHUB_DEFAULTS_LIMITS_FILE_BYTES` (derived) | 05 github adapter |  | consumer: github (README and contents reads; truncation point of `File.Data`; the JSON ... |
| `github.defaults.limits.json_bytes` | size | `8MiB` | `SCP_GITHUB_DEFAULTS_LIMITS_JSON_BYTES` (derived) | 05 github adapter |  | consumer: github (any JSON page) |
| `github.defaults.limits.tree_bytes` | size | `16MiB` | `SCP_GITHUB_DEFAULTS_LIMITS_TREE_BYTES` (derived) | 05 github adapter |  | consumer: github (GitHub caps trees at 7 MB) |
| `github.defaults.limits.tree_memo_ttl` | duration | `60s` | `SCP_GITHUB_DEFAULTS_LIMITS_TREE_MEMO_TTL` (derived) | 05 github adapter |  | consumer: github (shared `ListTree` memo, architecture 8.7) |
| `github.defaults.limits.tree_memo_max` | int | `256` | `SCP_GITHUB_DEFAULTS_LIMITS_TREE_MEMO_MAX` (derived) | 05 github adapter |  | consumer: github |
| `github.defaults.limits.immutable_max` | int | `512` | `SCP_GITHUB_DEFAULTS_LIMITS_IMMUTABLE_MAX` (derived) | 05 github adapter |  | consumer: github (trees by SHA) |
| `github.defaults.limits.validator_max` | int | `4096` | `SCP_GITHUB_DEFAULTS_LIMITS_VALIDATOR_MAX` (derived) | 05 github adapter |  | consumer: github (in-memory ETag memo) |
| `github.defaults.limits.max_pages` | int | `50` | `SCP_GITHUB_DEFAULTS_LIMITS_MAX_PAGES` (derived) | 05 github adapter |  | consumer: github (ceiling for every internal pagination loop) |
| `github.defaults.limits.head_batch` | int | `50` | `SCP_GITHUB_DEFAULTS_LIMITS_HEAD_BATCH` (derived) | 05 github adapter |  | 0 to 100 ; consumer: github (repositories per GraphQL head query) |
| `github.defaults.limits.release_recheck` | duration | `1h` | `SCP_GITHUB_DEFAULTS_LIMITS_RELEASE_RECHECK` (derived) | 05 github adapter |  | consumer: github (REST fallback only) |
| `github.defaults.budget.reserve.backfill` | percent | `40%` | `SCP_GITHUB_DEFAULTS_BUDGET_RESERVE_BACKFILL` (derived) | 05 github adapter |  | 0 to 100; backfill >= scheduled >= webhook ; consumer: github (`Admit`) |
| `github.defaults.budget.reserve.scheduled` | percent | `15%` | `SCP_GITHUB_DEFAULTS_BUDGET_RESERVE_SCHEDULED` (derived) | 05 github adapter |  | same ; consumer: github |
| `github.defaults.budget.reserve.webhook` | percent | `2%` | `SCP_GITHUB_DEFAULTS_BUDGET_RESERVE_WEBHOOK` (derived) | 05 github adapter |  | same ; consumer: github |
| `github.defaults.budget.concurrency` | int | `4` | `SCP_GITHUB_DEFAULTS_BUDGET_CONCURRENCY` (derived) | 05 github adapter |  | 0 to 100 ; consumer: github (in-flight requests per source) |
| `github.defaults.budget.rps` | float | `8` | `SCP_GITHUB_DEFAULTS_BUDGET_RPS` (derived) | 05 github adapter |  | not negative ; consumer: github (`x/time/rate`) |
| `github.defaults.budget.burst` | int | `16` | `SCP_GITHUB_DEFAULTS_BUDGET_BURST` (derived) | 05 github adapter |  | consumer: github |
| `github.sources[].id` | string | required | `SCP_GITHUB_SOURCES_<` | 05 github adapter |  | `^[a-z0-9][a-z0-9-]{0,39}$`, unique ; consumer: github; becomes `scm.SourceID` and the webhook route `{source}` |
| `github.sources[].api_version` | string | `""` (= defaults) | `SCP_GITHUB_SOURCES_<I>_API_VERSION` (derived) | 05 github adapter |  | `auto` or date ; consumer: github |
| `github.sources[].host.kind` | enum | `github.com` | `SCP_GITHUB_SOURCES_<I>_HOST_KIND` (derived) | 05 github adapter |  | `github.com`, `ghec_dr`, `ghes` ; consumer: github (URL derivation, API version, capabilities) |
| `github.sources[].host.subdomain` | string | none | `SCP_GITHUB_SOURCES_<I>_HOST_SUBDOMAIN` (derived) | 05 github adapter |  | required for `ghec_dr` ; consumer: github |
| `github.sources[].host.web_url` | URL | none | `SCP_GITHUB_SOURCES_<I>_HOST_WEB_URL` (derived) | 05 github adapter |  | required for `ghes` (unless `api_url`); https; no userinfo, query or fragment ; consumer: github (HTMLURL, manifest flow) |
| `github.sources[].host.api_url` | URL | `<web_url>/api/v3` | `SCP_GITHUB_SOURCES_<I>_HOST_API_URL` (derived) | 05 github adapter |  | same ; consumer: github |
| `github.sources[].host.graphql_url` | URL | `<web_url>/api/graphql` | `SCP_GITHUB_SOURCES_<I>_HOST_GRAPHQL_URL` (derived) | 05 github adapter |  | same ; consumer: github |
| `github.sources[].network.proxy_url` | URL | none | `SCP_GITHUB_SOURCES_<I>_NETWORK_PROXY_URL` (derived) | 05 github adapter |  | absolute URL ; consumer: cmd/scp client factory |
| `github.sources[].network.tls.ca_file` | path | none | `SCP_GITHUB_SOURCES_<I>_NETWORK_TLS_CA_FILE` (derived) | 05 github adapter |  | file must parse at startup (`New`) ; consumer: cmd/scp client factory |
| `github.sources[].network.allow_private_addresses` | bool | `false` | `SCP_GITHUB_SOURCES_<I>_NETWORK_ALLOW_PRIVATE_ADDRESSES` (derived) | 05 github adapter |  | SSRF policy exception for a GHES in a private network ; consumer: cmd/scp client factory (`netguard`) |
| `github.sources[].network.allow_insecure_http` | bool | `false` | `SCP_GITHUB_SOURCES_<I>_NETWORK_ALLOW_INSECURE_HTTP` (derived) | 05 github adapter |  | permits `http://` for a lab host ; consumer: github (URL check), factory |
| `github.sources[].network.pin_hosts` | []string | derived hosts | `SCP_GITHUB_SOURCES_<I>_NETWORK_PIN_HOSTS` (derived) | 05 github adapter |  | hostnames only ; consumer: cmd/scp client factory |
| `github.sources[].auth.kind` | enum | required | `SCP_GITHUB_SOURCES_<I>_AUTH_KIND` (derived) | 05 github adapter |  | `github_app`, `pat`, `fine_grained_pat`, `oauth_user_token`, `token_command`, `anonymous` ; consumer: github (strategy) |
| `github.sources[].auth.client_id` | string | none | `SCP_GITHUB_SOURCES_<I>_AUTH_CLIENT_ID` (derived) | 05 github adapter |  | `client_id` or `app_id` required for `github_app` ; consumer: auth (`iss`) |
| `github.sources[].auth.app_id` | int | none | `SCP_GITHUB_SOURCES_<I>_AUTH_APP_ID` (derived) | 05 github adapter |  | same ; consumer: auth, webhook target check |
| `github.sources[].auth.private_key.file` | secret.Secret | required for `github_app` unless `private_keys` | `SCP_GITHUB_SOURCES_<I>_AUTH_PRIVATE_KEY_FILE` (derived) | 05 github adapter |  | file readable, PEM RSA >= 2048 bits, mode warned by doctor ; consumer: auth |
| `github.sources[].auth.private_keys[]` | []SecretRef | none | `SCP_GITHUB_SOURCES_<I>_AUTH_PRIVATE_KEYS_<I>` (derived) | 05 github adapter |  | rotation: current first ; consumer: auth |
| `github.sources[].auth.installations.mode` | enum | `allow_list` | `SCP_GITHUB_SOURCES_<I>_AUTH_INSTALLATIONS_MODE` (derived) | 05 github adapter |  | `allow_list`, `pinned`, `all`; `all` must be explicit ; consumer: github (registry filter) |
| `github.sources[].auth.installations.accounts[]` | []string | none | `SCP_GITHUB_SOURCES_<I>_AUTH_INSTALLATIONS_ACCOUNTS_<I>` (derived) | 05 github adapter |  | required (non-empty) for `allow_list` ; consumer: github |
| `github.sources[].auth.installations.ids[]` | []int | none | `SCP_GITHUB_SOURCES_<I>_AUTH_INSTALLATIONS_IDS_<I>` (derived) | 05 github adapter |  | required for `pinned` ; consumer: github |
| `github.sources[].auth.token.file` | secret.Secret | required for PAT kinds | `SCP_GITHUB_SOURCES_<I>_AUTH_TOKEN_FILE` (derived) | 05 github adapter |  | non-empty content ; consumer: auth (`Static`, re-read on mtime change) |
| `github.sources[].auth.command[]` | []string | required for `token_command` | `SCP_GITHUB_SOURCES_<I>_AUTH_COMMAND_<I>` (derived) | 05 github adapter |  | argv, first element absolute path, no shell ; consumer: auth (`External`), runner in cmd/scp |
| `github.sources[].auth.timeout` | duration | `10s` | `SCP_GITHUB_SOURCES_<I>_AUTH_TIMEOUT` (derived) | 05 github adapter |  | consumer: auth (command) |
| `github.sources[].auth.refresh_before` | duration | `5m` | `SCP_GITHUB_SOURCES_<I>_AUTH_REFRESH_BEFORE` (derived) | 05 github adapter |  | consumer: auth (command) |
| `github.sources[].auth.ttl` | duration | `10m` | `SCP_GITHUB_SOURCES_<I>_AUTH_TTL` (derived) | 05 github adapter |  | consumer: auth (command without `expires_at`) |
| `github.sources[].auth.dev` | bool | `false` | `SCP_GITHUB_SOURCES_<I>_AUTH_DEV` (derived) | 05 github adapter |  | required `true` for `anonymous`; anonymous refused on `ghec_dr` ; consumer: github |
| `github.sources[].webhook.secret.file` | secret.Secret | required for `github_app` (and for the route to... | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_SECRET_FILE` (derived) | 05 github adapter |  | non-empty content ; consumer: github (`VerifierSource.Secrets[0]`); wiring registers the route only if set |
| `github.sources[].webhook.previous_secret.file` | secret.Secret | none | `SCP_GITHUB_SOURCES_<I>_WEBHOOK_PREVIOUS_SECRET_FILE` (derived) | 05 github adapter |  | consumer: github (`Secrets[1]`, bounded overlap during rotation) |
| `github.sources[].discovery.mode` | enum | required | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_MODE` (derived) | 05 github adapter |  | `installation` (needs `github_app`), `org`, `user`, `explicit` ; consumer: github (`ListRepositories`) |
| `github.sources[].discovery.org` | string | none | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_ORG` (derived) | 05 github adapter |  | required for `org`, `user` ; consumer: github |
| `github.sources[].discovery.repos[]` | []string | none | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_REPOS_<I>` (derived) | 05 github adapter |  | `owner/name`, required for `explicit` ; consumer: github |
| `github.sources[].discovery.visibility` | enum | `public_only` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_VISIBILITY` (derived) | 05 github adapter | 08, 10 | `public_only`, `installation_scope` ; consumer: github (filter); **cmd/scp** cross-check with `access.mode` |
| `github.sources[].discovery.include_archived` | bool | `true` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_INCLUDE_ARCHIVED` (derived) | 05 github adapter | 08 | consumer: github |
| `github.sources[].discovery.include_forks` | bool | `false` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_INCLUDE_FORKS` (derived) | 05 github adapter | 08 | consumer: github |
| `github.sources[].discovery.reconcile_every` | duration | `30m` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_RECONCILE_EVERY` (derived) | 05 github adapter | 08 | consumer: **catalog** (`Policy` of `catalog.reconcile`) |
| `github.sources[].discovery.max_removal_ratio` | float | `0.3` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_MAX_REMOVAL_RATIO` (derived) | 05 github adapter | 08 | 0 to 1 ; consumer: **catalog** (mass-removal guard) and github (installation guard) |
| `github.sources[].discovery.removal_confirmations` | int | `2` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_REMOVAL_CONFIRMATIONS` (derived) | 05 github adapter | 08 | 1..10 ; consumer: **catalog** (absence rule R3; declared here because this unit owns the `disco... |
| `github.sources[].discovery.mass_removal_hold` | duration | `6h` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_MASS_REMOVAL_HOLD` (derived) | 05 github adapter | 08 | 0..168h ; consumer: **catalog** (mass-removal hold R4; CD-CF-07) |
| `github.sources[].discovery.access_acknowledgement` | enum | `""` | `SCP_GITHUB_SOURCES_<I>_DISCOVERY_ACCESS_ACKNOWLEDGEMENT` (derived) | 05 github adapter | 08 | `network_restricted`, `oidc_proxy`, `trusted_proxy_headers` ; consumer: **cmd/scp** (startup refusal), doctor |
| `github.sources[].acquisition.mode` | enum | `api` | `SCP_GITHUB_SOURCES_<I>_ACQUISITION_MODE` (derived) | 05 github adapter |  | `api`; `git` refused in v1 ; consumer: **cmd/scp** (implementation choice) |
| `github.sources[].acquisition.graphql` | enum | `auto` | `SCP_GITHUB_SOURCES_<I>_ACQUISITION_GRAPHQL` (derived) | 05 github adapter |  | `auto`, `on`, `off` ; consumer: github (`Capabilities().GraphQL`) |
| `github.sources[].acquisition.graphql_blobs` | enum | `off` | `SCP_GITHUB_SOURCES_<I>_ACQUISITION_GRAPHQL_BLOBS` (derived) | 05 github adapter |  | `off`, `auto`, `on` (staged, spike S-B) ; consumer: github (`Capabilities().GraphQLBlobs`) |
| `github.sources[].acquisition.webhook_assisted` | bool | `true` | `SCP_GITHUB_SOURCES_<I>_ACQUISITION_WEBHOOK_ASSISTED` (derived) | 05 github adapter |  | consumer: **cmd/scp** (registers reactors for the source; freshness degrades without it... |
| `github.sources[].acquisition.git.*` |  | see 4.15 | `SCP_GITHUB_SOURCES_<I>_ACQUISITION_GIT_*` (derived) | 05 github adapter |  | post-v1 ; consumer: gitsource |

### `catalog`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `catalog.ungrouped` | enum `track`\\|`ignore` | `track` | `SCP_CATALOG_UNGROUPED` | 08 catalog |  | C002 ; consumer: `Service.commit` (R-step 2 `Grouped`) |
| `catalog.slug_prefix_by_source` | map string to string | `{}` | `SCP_CATALOG_SLUG_PREFIX_BY_SOURCE` (derived) | 08 catalog |  | key is `source` or `source/owner` with a known source (C060); prefix `[A-Za-z0-9._-]{0,32}` (C061) ; consumer: `Service.prefix` -> `PlanInput.Prefix` |
| `catalog.heads.batch_size` | int | `50` | `SCP_CATALOG_HEADS_BATCH_SIZE` | 08 catalog |  | 1..100 (C003) ; consumer: `runHeads` |
| `catalog.heads.interval` | duration | `5m` | `SCP_CATALOG_HEADS_INTERVAL` | 08 catalog |  | 1m..1h (C003) ; consumer: `Jobs()` TTL of `catalog.heads`; `freshness.Info.TTL` of `catalog.stats` slots |
| `catalog.heads.archived_interval` | duration | `24h` | `SCP_CATALOG_HEADS_ARCHIVED_INTERVAL` | 08 catalog |  | 1h..168h (C003) ; consumer: `Jobs()` TTL and `IdleBackoffMax` of `catalog.stats` |
| `catalog.groups` | list | `[]` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | at most 500 (C090) ; consumer: `Compile`, `BuildMembership` |
| `catalog.groups[].id` | string | required | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C010, C011 ; consumer: membership, routes |
| `catalog.groups[].display_name` | string | required | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C012 ; consumer: `GroupMeta.DisplayName` -> API |
| `catalog.groups[].description` | string | `""` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C013 ; consumer: `GroupMeta.Description` -> API |
| `catalog.groups[].default_repo` | GitHub repository **name** | required | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C030, C031, C032; online R205, R209 ; consumer: `BuildMembership` defaults |
| `catalog.groups[].sources` | list of source ids | `[]` (all) | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C070 ; consumer: `compiledGroup.sources` |
| `catalog.groups[].match` | list of one-key maps | required, 1..64 | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C020, C022, W108, R201 ; consumer: `Compile`, `Matches` |
| `catalog.groups[].ref` | branch name | `""` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C040, W103, R207 ; consumer: `BuildMembership` `TrackedRef` -> `catalog.repo.DefaultBranch` |
| `catalog.groups[].integrations.jenkins.job_prefix` | string | `""` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C050 ; consumer: `GroupConfig.Meta` -> `scm.GroupMeta.Integrations["jenkins"]["job_prefix"]` -... |
| `catalog.groups[].integrations.jenkins.instance` | string | `""` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C051 (existence: `integrations`) ; consumer: same, `["jenkins"]["instance"]` |
| `catalog.groups[].integrations.snyk.project_prefix` | string | `""` | `SCP_CATALOG_GROUPS_<` | 08 catalog |  | C050 ; consumer: same, `["snyk"]["project_prefix"]` |

### `refresh`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `refresh.upstreams.<name>.concurrency` | int | github 4; others 1 | `SCP_REFRESH_UPSTREAMS_GITHUB_CONCURRENCY` | 04 refresh/webhook | 16 | 1..64 ; consumer: `UpstreamConfig.MaxConcurrent` |
| `refresh.upstreams.<name>.pace` | duration | github 100ms; others 0 | `SCP_REFRESH_UPSTREAMS_GITHUB_PACE` | 04 refresh/webhook | 16 | 0..10s ; consumer: `UpstreamConfig.Pace` |
| `refresh.upstreams.<name>.job_timeout` | duration | 2m | `SCP_REFRESH_UPSTREAMS_GITHUB_JOB_TIMEOUT` | 04 refresh/webhook | 16 | 1s..30m ; consumer: `UpstreamConfig.JobTimeout` (addition to the outline; the field already exist... |
| `refresh.backoff.base` | duration | 2s | `SCP_REFRESH_BACKOFF_BASE` | 04 refresh/webhook | 16 | > 0 ; consumer: `Backoff.Base` |
| `refresh.backoff.cap` | duration | 15m | `SCP_REFRESH_BACKOFF_CAP` | 04 refresh/webhook | 16 | >= base ; consumer: `Backoff.Cap` |
| `refresh.backoff.max_attempts` | int | 8 | `SCP_REFRESH_BACKOFF_MAX_ATTEMPTS` | 04 refresh/webhook | 16 | >= 1 ; consumer: `Backoff.MaxAttempts` |
| `refresh.grace` | duration | 15s | `SCP_REFRESH_GRACE` | 04 refresh/webhook | 16 | 1s..60s ; consumer: `WithGrace` (also sizes the stage `StopTimeout`, section 2.5) |
| `refresh.planner_tick` | duration | 15s | `SCP_REFRESH_PLANNER_TICK` | 04 refresh/webhook | 16 | 1s..5m ; consumer: `planner.tick` `Task.Every` |
| `refresh.max_keys` | int | 50000 | `SCP_REFRESH_MAX_KEYS` | 04 refresh/webhook | 16 | >= 1000 ; consumer: `WithMaxKeys` |
| `refresh.parking.retry` | duration | 24h | `SCP_REFRESH_PARKING_RETRY` | 04 refresh/webhook | 16 | 1m..30d ; consumer: `Tunables.ParkRetry` (new) |
| `refresh.upstreams.deps-dev.concurrency` | int, duration, duration | `1`, `0`, `2m` | `SCP_REFRESH_UPSTREAMS_DEPS_DEV_CONCURRENCY` | 13 ecosystems B |  | the refresh unit's ranges ; consumer: `eng.AddUpstream("deps-dev", ...)` in `cmd/scp`, only when the depsdev provid... |

### `site`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `site.title` | string | `Source Code Portal` | `SCP_SITE_TITLE` | 15 live/ops/overview |  | 1..120 runes; no control characters; no surrounding space ; consumer: `siteHandler`, llms H1, sitemap |
| `site.description` | string | `""` | `SCP_SITE_DESCRIPTION` | 15 live/ops/overview |  | at most 500 runes; no control characters ; consumer: `siteHandler`, llms index blockquote |
| `site.nav` | []struct | `[]` | `SCP_SITE_NAV_<` | 15 live/ops/overview |  | at most 20; see below ; consumer: `siteHandler` |
| `site.nav[].label` | string | required | `SCP_SITE_NAV_<I>_LABEL` (derived) | 15 live/ops/overview |  | 1..60 runes; no control characters |
| `site.nav[].page` | string | `""` | `SCP_SITE_NAV_<I>_PAGE` (derived) | 15 live/ops/overview |  | exactly one of `page` and `url`; a page slug `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 63 ; consumer: an entry naming a missing page is dropped at request time |
| `site.nav[].url` | string | `""` | `SCP_SITE_NAV_<I>_URL` (derived) | 15 live/ops/overview |  | https URL, host required, no credentials, at most 2000 bytes |
| `site.display_time_zone` | string | `UTC` | `SCP_SITE_DISPLAY_TIME_ZONE` | 15 live/ops/overview |  | an IANA zone that `time.LoadLocation` accepts; not empty and not `Local` ; consumer: `siteHandler` (SPA renders every timestamp in this zone; the API is always UTC) |
| `site.default_theme` | enum | `system` | `SCP_SITE_DEFAULT_THEME` | 15 live/ops/overview |  | `system`, `light`, `dark` ; consumer: `siteHandler` |
| `site.sitemap` | bool | `true` | `SCP_SITE_SITEMAP` | 15 live/ops/overview |  | none ; consumer: `sitemap`, `robots`, llms Optional section |
| `site.robots.extra_rules` | []string | `[]` | `SCP_SITE_ROBOTS_EXTRA_RULES` (derived) | 15 live/ops/overview |  | at most 50 lines; each at most 200 printable ASCII bytes matching `(User-agent ; consumer: Disallow |
| `site.dashboard_commits` | integer | `10` | `SCP_SITE_DASHBOARD_COMMITS` | 18 web pages |  | 1..50 and `<= commits.latest_n` ; consumer: exposed as `Site.dashboardCommits` (additive, optional integer in `openapi-pu... ; **PROPOSAL of unit 18 (D-18-03), NOT adopted in v1 (CD-RT-07): the SPA keeps a constant of 10; the key is listed so that nobody implements it by accident** |

### `readiness`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `readiness.warm_max_age` | duration | `72h` | `SCP_READINESS_WARM_MAX_AGE` | 15 live/ops/overview |  | at least 1m ; consumer: `Readiness.Evaluate` (catalogue gate: a warm catalogue confirmed against GitH... |
| `readiness.min_coverage` | float | `0.9` | `SCP_READINESS_MIN_COVERAGE` | 15 live/ops/overview |  | in (0, 1] ; consumer: coverage gate |
| `readiness.core_kinds` | []string | `[commits]` | `SCP_READINESS_CORE_KINDS` | 15 live/ops/overview |  | at least one; each `^[a-z][a-z0-9_]{0,31}$`; no duplicates; each must be a registered `Kind.Famil... ; consumer: `NewReadiness`, `Collector` (status shows them) |
| `readiness.startup_deadline` | duration | `10m` | `SCP_READINESS_STARTUP_DEADLINE` | 15 live/ops/overview |  | 30s..24h ; consumer: `Evaluate` (degraded by deadline) |

### `ops`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `ops.liveness.heartbeat_max` | duration | `30s` | `SCP_OPS_LIVENESS_HEARTBEAT_MAX` | 15 live/ops/overview |  | 20s..10m ; consumer: `livenessFailure` |
| `ops.liveness.stuck_max` | duration | `2m` | `SCP_OPS_LIVENESS_STUCK_MAX` | 15 live/ops/overview |  | 10s..1h ; consumer: `livenessFailure` |
| `ops.liveness.startup_grace` | duration | `30s` | `SCP_OPS_LIVENESS_STARTUP_GRACE` | 15 live/ops/overview |  | 0..10m ; consumer: `livenessFailure` (a zero heartbeat is tolerated this long after process start) |
| `ops.pprof.enabled` | bool | `true` | `SCP_OPS_PPROF_ENABLED` | 15 live/ops/overview |  | none ; consumer: `PprofHandlers` (admin listener only; the listener is the access control) |
| `ops.pprof.max_seconds` | duration | `60s` | `SCP_OPS_PPROF_MAX_SECONDS` | 15 live/ops/overview |  | 1s..10m ; consumer: `PprofHandlers` (cap of `?seconds=`) |
| `ops.pprof.block_rate` | int | `0` | `SCP_OPS_PPROF_BLOCK_RATE` | 15 live/ops/overview |  | at least 0 ; consumer: `PprofConfig.Apply` (`runtime.SetBlockProfileRate`) |
| `ops.pprof.mutex_fraction` | int | `0` | `SCP_OPS_PPROF_MUTEX_FRACTION` | 15 live/ops/overview |  | at least 0 ; consumer: `PprofConfig.Apply` (`runtime.SetMutexProfileFraction`) |
| `ops.warnings.rate_low_ratio` | float | `0.10` | `SCP_OPS_WARNINGS_RATE_LOW_RATIO` | 15 live/ops/overview |  | in (0, 1) ; consumer: `deriveWarnings` (`github.rate-low`; a fifth of it is critical) |
| `ops.warnings.stale_factor` | float | `3` | `SCP_OPS_WARNINGS_STALE_FACTOR` | 15 live/ops/overview |  | at least 1 ; consumer: `deriveWarnings` (`jobs.stale`) |
| `ops.warnings.webhook_silence` | duration | `24h` | `SCP_OPS_WARNINGS_WEBHOOK_SILENCE` | 15 live/ops/overview |  | at least 0 (0 disables) ; consumer: `deriveWarnings` (`webhook.silent`) |
| `ops.status.max_parked_shown` | int | `200` | `SCP_OPS_STATUS_MAX_PARKED_SHOWN` | 15 live/ops/overview |  | 1..1000 ; consumer: `Collector.Status` |

### `live`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `live.sse.enabled` | bool | `false` | `SCP_LIVE_SSE_ENABLED` | 15 live/ops/overview | 07 | none ; consumer: `live.serveEvents`; mirrored into `api.site` `features.sse` by `cmd/scp` (`ov... |
| `live.sse.debounce` | duration | `300ms` | `SCP_LIVE_SSE_DEBOUNCE` | 15 live/ops/overview |  | 50ms..5s ; consumer: `serveEvents` (coalescing window after a publish) |
| `live.sse.heartbeat` | duration | `15s` | `SCP_LIVE_SSE_HEARTBEAT` | 15 live/ops/overview |  | 5s..60s ; consumer: `serveEvents` (comment line cadence; keep below every proxy read timeout, 30 ... |
| `live.sse.write_timeout` | duration | `10s` | `SCP_LIVE_SSE_WRITE_TIMEOUT` | 15 live/ops/overview |  | 1s..60s ; consumer: `sseWriter.arm` (per-write deadline, re-armed before every write) |
| `live.sse.retry` | duration | `3s` | `SCP_LIVE_SSE_RETRY` | 15 live/ops/overview |  | 1s..60s ; consumer: `sseWriter.retry` (reconnect delay announced on connect and at lifetime end) |
| `live.sse.drain_retry` | duration | `30s` | `SCP_LIVE_SSE_DRAIN_RETRY` | 15 live/ops/overview |  | 5s..10m ; consumer: drain path (reconnect delay announced at shutdown; 30 s so the replacement in... |
| `live.sse.max_lifetime` | duration | `30m` | `SCP_LIVE_SSE_MAX_LIFETIME` | 15 live/ops/overview |  | 1m..24h ; consumer: `lifetime()` (jittered +-10%) |
| `live.sse.max_clients` | int | `1000` | `SCP_LIVE_SSE_MAX_CLIENTS` | 15 live/ops/overview |  | 1..100000 ; consumer: `newBroker` (process-wide stream cap) |

### `llms`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `llms.full_max_bytes` | ByteSize | `2MiB` | `SCP_LLMS_FULL_MAX_BYTES` | 15 live/ops/overview | 10 | 64KiB..32MiB ; consumer: `fullWriter` (cap of the three full-text outputs; the writer stops 256 bytes ... |
| `llms.enabled` | bool | `true` | `SCP_LLMS_ENABLED` | 15 live/ops/overview |  | none ; consumer: every `llms*.txt` handler (`false`: 404 `feature-disabled`) |
| `llms.index_max_chars` | int | `100000` | `SCP_LLMS_INDEX_MAX_CHARS` | 15 live/ops/overview |  | 1,000..1,000,000 ; consumer: `buildIndex` (the `## Repositories` section is omitted above this) |
| `llms.summary_max_runes` | int | `160` | `SCP_LLMS_SUMMARY_MAX_RUNES` | 15 live/ops/overview |  | 40..300 ; consumer: `repoLine`, `buildGroup`, `buildRepo` |
| `llms.max_concurrent_full` | int | `4` | `SCP_LLMS_MAX_CONCURRENT_FULL` | 15 live/ops/overview |  | 1..64 ; consumer: `acquireFull` (semaphore) |
| `llms.max_age` | duration | `5m` | `SCP_LLMS_MAX_AGE` | 15 live/ops/overview |  | 0..24h ; consumer: `textCacheControl` (`max-age` of every text output, the sitemap and the brows... |

### `commits`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `commits.keep` | int | 30 | `SCP_COMMITS_KEEP` | 09 commits/badges | 07, 15 | 1..100 (one GitHub page) ; consumer: `run` (fetch size, ring cap), `Restore`, `AppendCommits` trim, `badges`-indep... |
| `commits.latest_n` | int | 50 | `SCP_COMMITS_LATEST_N` | 09 commits/badges |  | 1..200 (`limit` maximum is 200: a feed longer than any page is pointless) ; consumer: `Projector`, handler fast-path predicate |
| `commits.refresh_ttl` | duration | 2h | `SCP_COMMITS_REFRESH_TTL` | 09 commits/badges |  | 1m..24h ; consumer: `Jobs()` policy TTL; `FeedInfo.TTL`; freshness 1.5 x rule |
| `commits.idle_backoff_max` | duration | 12h | `SCP_COMMITS_IDLE_BACKOFF_MAX` | 09 commits/badges |  | `refresh_ttl`..168h ; consumer: `Jobs()` policy |

### `badges`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `badges.palette` | enum | `aa` | `SCP_BADGES_PALETTE` | 09 commits/badges |  | `aa` or `aaa` ; consumer: `New` (palette), `render` |
| `badges.cache_max_age` | duration | 5m | `SCP_BADGES_CACHE_MAX_AGE` | 09 commits/badges |  | 0..1h; `0` means `no-cache` ; consumer: `respond` (Cache-Control of data-carrying badges) |

### `docs`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `docs.max_doc_bytes` | int | 512000 | `SCP_DOCS_MAX_DOC_BYTES` | 10 docs/pages/media |  | 1 KiB .. 1 MiB |
| `docs.max_docs_per_repo` | int | 50 | `SCP_DOCS_MAX_DOCS_PER_REPO` | 10 docs/pages/media |  | 1 .. 200 |
| `docs.max_resident_per_repo` | bytes int | 2097152 | `SCP_DOCS_MAX_RESIDENT_PER_REPO` | 10 docs/pages/media |  | 64 KiB .. 8 MiB |
| `docs.resident_budget` | bytes int | 201326592 (192 MiB) | `SCP_DOCS_RESIDENT_BUDGET` | 10 docs/pages/media |  | at least `max_resident_per_repo` |
| `docs.bridge_asciidoc` | bool | true | `SCP_DOCS_BRIDGE_ASCIIDOC` | 10 docs/pages/media |  |  |
| `docs.default_root` | string | `docs` | `SCP_DOCS_DEFAULT_ROOT` | 10 docs/pages/media |  | repository-relative, no `..`, no leading `/`, no empty or `.` segment, no NUL or backslash, at mo... |
| `docs.ttl` | duration | 1h | `SCP_DOCS_TTL` | 10 docs/pages/media |  | 5m .. 24h |
| `docs.idle_backoff_max` | duration | 6h | `SCP_DOCS_IDLE_BACKOFF_MAX` | 10 docs/pages/media |  | `ttl` .. 168h |
| `docs.asciidoc_revalidate` | duration | 24h | `SCP_DOCS_ASCIIDOC_REVALIDATE` | 10 docs/pages/media |  | 1h .. 168h |
| `docs.job_timeout` | duration | 3m | `SCP_DOCS_JOB_TIMEOUT` | 10 docs/pages/media |  | 10s .. 30m |
| `docs.max_fetch_per_run` | int | 25 | `SCP_DOCS_MAX_FETCH_PER_RUN` | 10 docs/pages/media |  | 1 .. 200 |
| `docs.render_timeout` | duration | 5s | `SCP_DOCS_RENDER_TIMEOUT` | 10 docs/pages/media |  | 1s .. 1m |
| `docs.render_concurrency` | int | 2 | `SCP_DOCS_RENDER_CONCURRENCY` | 10 docs/pages/media |  | 1 .. 16 |
| `docs.catalog_info` | bool | true | `SCP_DOCS_CATALOG_INFO` | 10 docs/pages/media |  |  |
| `docs.codeowners` | bool | true | `SCP_DOCS_CODEOWNERS` | 10 docs/pages/media |  |  |

### `pages`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `pages.dir` | string | empty (pages disabled) | `SCP_PAGES_DIR` | 10 docs/pages/media |  | when set: an existing readable directory at startup (a missing directory is a startup warning and... |
| `pages.max_pages` | int | 100 | `SCP_PAGES_MAX_PAGES` | 10 docs/pages/media |  | 1 .. 500 |
| `pages.max_page_bytes` | int | 262144 | `SCP_PAGES_MAX_PAGE_BYTES` | 10 docs/pages/media |  | 1 KiB .. 1 MiB |
| `pages.reload_every` | duration | 0 (on demand) | `SCP_PAGES_RELOAD_EVERY` | 10 docs/pages/media |  | 0 or 10s .. 24h |
| `pages.announcement.enabled` | bool | false | `SCP_PAGES_ANNOUNCEMENT_ENABLED` | 10 docs/pages/media |  |  |
| `pages.announcement.text` | string | empty | `SCP_PAGES_ANNOUNCEMENT_TEXT` | 10 docs/pages/media |  | required when enabled; 1 .. 500 runes after cleaning |
| `pages.announcement.level` | enum | `info` | `SCP_PAGES_ANNOUNCEMENT_LEVEL` | 10 docs/pages/media |  | `info`, `warning`, `critical` |
| `pages.announcement.dismissible` | bool | true | `SCP_PAGES_ANNOUNCEMENT_DISMISSIBLE` | 10 docs/pages/media |  |  |

### `media`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `media.signing_key` | secret | empty (ephemeral key, development only) | `SCP_MEDIA_SIGNING_KEY` | 10 docs/pages/media |  | when set: at least 32 bytes |
| `media.previous_keys` | list of secret | empty | `SCP_MEDIA_PREVIOUS_KEYS_0` | 10 docs/pages/media |  | each at least 32 bytes |
| `media.allow_hosts` | list of string | empty (every public host) | `SCP_MEDIA_ALLOW_HOSTS_0` | 10 docs/pages/media |  | each a lower-case DNS name or `*.suffix` passing the same host rules as `Canonical` |
| `media.max_bytes` | bytes int | 2097152 | `SCP_MEDIA_MAX_BYTES` | 10 docs/pages/media |  | 16 KiB .. 16 MiB |
| `media.fetch_timeout` | duration | 5s | `SCP_MEDIA_FETCH_TIMEOUT` | 10 docs/pages/media |  | 1s .. 30s |
| `media.cache_bytes` | bytes int | 67108864 | `SCP_MEDIA_CACHE_BYTES` | 10 docs/pages/media |  | 1 MiB .. 4 GiB |
| `media.cache_ttl` | duration | 1h | `SCP_MEDIA_CACHE_TTL` | 10 docs/pages/media |  | 1m .. 24h |
| `media.negative_ttl` | duration | 5m | `SCP_MEDIA_NEGATIVE_TTL` | 10 docs/pages/media |  | 10s .. 1h |
| `media.max_concurrent` | int | 16 | `SCP_MEDIA_MAX_CONCURRENT` | 10 docs/pages/media |  | 1 .. 256 |
| `media.external_svg` | enum | `deny` | `SCP_MEDIA_EXTERNAL_SVG` | 10 docs/pages/media |  | `deny` or `sandboxed` (D-1) |
| `media.max_svg_bytes` | bytes int | 262144 | `SCP_MEDIA_MAX_SVG_BYTES` | 10 docs/pages/media |  | 1 KiB .. `max_bytes` |

### `manifests`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `manifests.enabled` | bool | `true` | `SCP_MANIFESTS_ENABLED` | 11 manifests core | 07 | consumer: `Service.guard` (404 `feature-disabled`), `Enabled()`, `cmd/scp` (job and rea... |
| `manifests.refresh.ttl` | duration | `30m` | `SCP_MANIFESTS_REFRESH_TTL` | 11 manifests core |  | 1m..24h ; consumer: `Jobs()`: `Policy.TTL` of `manifests.refresh` (also the drift bound for misse... |
| `manifests.refresh.idle_backoff_max` | duration | `4h` | `SCP_MANIFESTS_REFRESH_IDLE_BACKOFF_MAX` | 11 manifests core |  | `refresh.ttl`..168h ; consumer: `Policy.IdleBackoffMax`; `Info.TTL` of every published facts value (staleness... |
| `manifests.refresh.jitter` | float | `0.1` | `SCP_MANIFESTS_REFRESH_JITTER` | 11 manifests core |  | 0..0.5 ; consumer: `Policy.Jitter` |
| `manifests.refresh.job_timeout` | duration | `3m` | `SCP_MANIFESTS_REFRESH_JOB_TIMEOUT` | 11 manifests core |  | 10s..30m ; consumer: `Spec.Timeout` |
| `manifests.refresh.republish_after` | duration | `1h` | `SCP_MANIFESTS_REFRESH_REPUBLISH_AFTER` | 11 manifests core |  | 1m..`idle_backoff_max` ; consumer: `unchanged`: how often an unchanged result republishes the slot so `FetchedAt... |
| `manifests.graph.ttl` | duration | `5m` | `SCP_MANIFESTS_GRAPH_TTL` | 11 manifests core |  | 1m..1h ; consumer: `Policy.TTL` of `manifests.graph` (repair bound) |
| `manifests.graph.debounce` | duration | `2s` | `SCP_MANIFESTS_GRAPH_DEBOUNCE` | 11 manifests core |  | 0..30s ; consumer: `enqueueGraph` (`notBefore`) |
| `manifests.graph.max_edges` | int | `2000000` | `SCP_MANIFESTS_GRAPH_MAX_EDGES` | 11 manifests core |  | 1000..20,000,000 ; consumer: `BuildGraph` (stops, `Truncated`) |
| `manifests.graph.max_cycles_reported` | int | `50` | `SCP_MANIFESTS_GRAPH_MAX_CYCLES_REPORTED` | 11 manifests core |  | 0..1000 ; consumer: `getDependencyGraph` |
| `manifests.acquisition.disabled_ecosystems` | list of string | `[]` | `SCP_MANIFESTS_ACQUISITION_DISABLED_ECOSYSTEMS` | 11 manifests core | 13 | each lower case, no space, `/` or `*` ; consumer: `cmd/scp` -> `Registry.Disable` (the only per-ecosystem switch, RR-22) |
| `manifests.acquisition.exclude_segments` | list of string | `node_modules vendor .git target build dist out... | `SCP_MANIFESTS_ACQUISITION_EXCLUDE_SEGMENTS` | 11 manifests core |  | plain segment (no `/ * ? [`) ; consumer: `Discover`, `walk`, `React` (`Interesting`) |
| `manifests.acquisition.fixture_segments` | list of string | `examples example samples sample test tests e2e... | `SCP_MANIFESTS_ACQUISITION_FIXTURE_SEGMENTS` | 11 manifests core |  | plain segment ; consumer: `Discover` (`Role=fixture`) |
| `manifests.acquisition.max_depth` | int | `12` | `SCP_MANIFESTS_ACQUISITION_MAX_DEPTH` | 11 manifests core |  | 1..32 ; consumer: `Discover`, `walk` |
| `manifests.acquisition.max_files` | int | `200` | `SCP_MANIFESTS_ACQUISITION_MAX_FILES` | 11 manifests core |  | 1..2000 ; consumer: `Discover` |
| `manifests.acquisition.max_tree_calls` | int | `200` | `SCP_MANIFESTS_ACQUISITION_MAX_TREE_CALLS` | 11 manifests core |  | 1..2000 ; consumer: `walk` |
| `manifests.acquisition.max_tree_entries` | int | `500000` | `SCP_MANIFESTS_ACQUISITION_MAX_TREE_ENTRIES` | 11 manifests core |  | 1000..5,000,000 ; consumer: `TreeOptions.MaxEntries` |
| `manifests.acquisition.fetch_concurrency` | int | `4` | `SCP_MANIFESTS_ACQUISITION_FETCH_CONCURRENCY` | 11 manifests core |  | 1..16 ; consumer: `fetchBlobs` |
| `manifests.acquisition.batch_size` | int | `25` | `SCP_MANIFESTS_ACQUISITION_BATCH_SIZE` | 11 manifests core |  | 1..100 ; consumer: `fetchBlobs` (`BlobBatchReader`) |
| `manifests.acquisition.batch_max_blob_bytes` | size | `256KiB` | `SCP_MANIFESTS_ACQUISITION_BATCH_MAX_BLOB_BYTES` | 11 manifests core |  | 1KiB..4MiB ; consumer: `fetchBlobs` (above this: REST raw) |
| `manifests.acquisition.raw_blob_cache_max` | size | `256KiB` | `SCP_MANIFESTS_ACQUISITION_RAW_BLOB_CACHE_MAX` | 11 manifests core |  | 0..4MiB ; consumer: `load` (what enters `manifests.blob`) |
| `manifests.limits.manifest_bytes` | size | `1MiB` | `SCP_MANIFESTS_LIMITS_MANIFEST_BYTES` | 11 manifests core |  | 1KiB..8MiB ; consumer: `Discover`, `parseOne` |
| `manifests.limits.lockfile_bytes` | size | `8MiB` | `SCP_MANIFESTS_LIMITS_LOCKFILE_BYTES` | 11 manifests core |  | `manifest_bytes`..32MiB ; consumer: same, for `Kinded` lockfile parsers; `ReadBlob` `maxBytes` |
| `manifests.limits.max_depth` | int | `256` | `SCP_MANIFESTS_LIMITS_MAX_DEPTH` | 11 manifests core |  | 8..10000 ; consumer: `Limits.MaxDepth` |
| `manifests.limits.max_nodes` | int | `2000000` | `SCP_MANIFESTS_LIMITS_MAX_NODES` | 11 manifests core |  | 1000..50,000,000 ; consumer: `Limits.MaxNodes` |
| `manifests.limits.max_expand` | size | `1MiB` | `SCP_MANIFESTS_LIMITS_MAX_EXPAND` | 11 manifests core |  | 1KiB..16MiB ; consumer: `Limits.MaxExpand` (interpolation, per value) |
| `manifests.limits.max_expand_file` | size | `8MiB` | `SCP_MANIFESTS_LIMITS_MAX_EXPAND_FILE` | 11 manifests core |  | `max_expand`..64MiB ; consumer: `Limits.MaxExpandAll` (per file) |
| `manifests.limits.parse_timeout` | duration | `2s` | `SCP_MANIFESTS_LIMITS_PARSE_TIMEOUT` | 11 manifests core |  | 100ms..1m ; consumer: `Limits.Timeout` |
| `manifests.limits.assemble_timeout` | duration | `5s` | `SCP_MANIFESTS_LIMITS_ASSEMBLE_TIMEOUT` | 11 manifests core |  | 100ms..1m ; consumer: `assembleAll` (per ecosystem) |
| `manifests.limits.parse_concurrency` | int | `0` (= `min(NumCPU, 4)`) | `SCP_MANIFESTS_LIMITS_PARSE_CONCURRENCY` | 11 manifests core |  | 0..64 ; consumer: `load` errgroup |
| `manifests.limits.parse_memory` | size | `256MiB` | `SCP_MANIFESTS_LIMITS_PARSE_MEMORY` | 11 manifests core |  | 8MiB..4GiB ; consumer: `limits.NewBudget` (weighted semaphore) |
| `manifests.limits.watchdog_grace` | duration | `500ms` | `SCP_MANIFESTS_LIMITS_WATCHDOG_GRACE` | 11 manifests core |  | 0..10s ; consumer: `Watchdog.Grace` |
| `manifests.limits.abandon_trip` | int | `3` | `SCP_MANIFESTS_LIMITS_ABANDON_TRIP` | 11 manifests core |  | 1..100 ; consumer: `Watchdog.Trip` (circuit) |
| `manifests.limits.max_deps_per_file` | int | `20000` | `SCP_MANIFESTS_LIMITS_MAX_DEPS_PER_FILE` | 11 manifests core |  | 100..1,000,000 ; consumer: `sanitizeParsed`, `finalize` (x4 per unit) |
| `manifests.limits.max_locked_per_file` | int | `100000` | `SCP_MANIFESTS_LIMITS_MAX_LOCKED_PER_FILE` | 11 manifests core |  | 100..5,000,000 ; consumer: `sanitizeParsed` |
| `manifests.limits.max_units_per_repo` | int | `500` | `SCP_MANIFESTS_LIMITS_MAX_UNITS_PER_REPO` | 11 manifests core |  | 1..5000 ; consumer: `finalize` |
| `manifests.limits.max_facts_bytes` | size | `8MiB` | `SCP_MANIFESTS_LIMITS_MAX_FACTS_BYTES` | 11 manifests core |  | 64KiB..16MiB (`store.MaxPayload`) ; consumer: `publish` (`EncodeFacts`) |
| `manifests.pom_source.enabled` | bool | `false` | `SCP_MANIFESTS_POM_SOURCE_ENABLED` | 11 manifests core |  | consumer: `cmd/scp`: whether the Maven assembler gets a `PomSource` (off by default: no... |
| `manifests.pom_source.hosts` | list of string | `[repo1.maven.org]` | `SCP_MANIFESTS_POM_SOURCE_HOSTS` | 11 manifests core |  | consumer: `cmd/scp`: allow-list of the hardened client behind `PomSource` (`platform/ne... |
| `manifests.pom_source.private_group_prefixes` | list of string | `[]` | `SCP_MANIFESTS_POM_SOURCE_PRIVATE_GROUP_PREFIXES` | 11 manifests core |  | groupId prefix, no space, `/` or `*` ; consumer: `NewPrivacyGuard` (never looked up externally) |
| `manifests.output.llm_max_rows` | int | `200` | `SCP_MANIFESTS_OUTPUT_LLM_MAX_ROWS` | 11 manifests core |  | 10..5000 ; consumer: `RepoDependenciesMD`, `GroupDependenciesMD` |
| `manifests.output.default_page` | int | `50` | `SCP_MANIFESTS_OUTPUT_DEFAULT_PAGE` | 11 manifests core |  | 1..`max_page` ; consumer: `listRepoDependencies`, `getDependencyVersions`, `listComponentDependents` |
| `manifests.output.max_page` | int | `200` | `SCP_MANIFESTS_OUTPUT_MAX_PAGE` | 11 manifests core |  | 1..1000 ; consumer: the same (upper bound of `limit`) |
| `manifests.output.versions_top_n` | int | `100` | `SCP_MANIFESTS_OUTPUT_VERSIONS_TOP_N` | 11 manifests core |  | 1..1000 ; consumer: version histograms of `getDependencyVersions` and `getComponent` |
| `manifests.output.sbom_max_locked` | int | `200000` | `SCP_MANIFESTS_OUTPUT_SBOM_MAX_LOCKED` | 11 manifests core |  | 1000..5,000,000 ; consumer: `handleSBOM`: the budget of locked components per document (VF-16) |
| `manifests.ecosystems.nuget.max_import_depth` | int | 8 | `SCP_MANIFESTS_ECOSYSTEMS_NUGET_MAX_IMPORT_DEPTH` | 13 ecosystems B |  | 1..32 ; consumer: `nugetAssembler` |
| `manifests.ecosystems.nuget.max_imports` | int | 64 | `SCP_MANIFESTS_ECOSYSTEMS_NUGET_MAX_IMPORTS` | 13 ecosystems B |  | 1..1000 ; consumer: `nugetAssembler` |
| `manifests.ecosystems.actions.alias_node_budget` | int | 100000 | `SCP_MANIFESTS_ECOSYSTEMS_ACTIONS_ALIAS_NODE_BUDGET` | 13 ecosystems B |  | 0..2000000 (0 rejects aliases) ; consumer: `actions/*` via `yamlsafe.Options.AliasBudget` |
| `manifests.ecosystems.terraform.max_tf_bytes` | size | 256KiB | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_TF_BYTES` | 13 ecosystems B |  | 1KiB..1MiB ; consumer: `terraform/tf` pre-scan |
| `manifests.ecosystems.terraform.max_hcl_depth` | int | 256 | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_HCL_DEPTH` | 13 ecosystems B |  | 8..4096 (the validator refuses more: measured process death near 70,000 for brackets) ; consumer: pre-scan |
| `manifests.ecosystems.terraform.max_unary_run` | int | 256 | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_UNARY_RUN` | 13 ecosystems B |  | 8..4096 ; consumer: pre-scan |
| `manifests.ecosystems.terraform.max_ternaries` | int | 4096 | `SCP_MANIFESTS_ECOSYSTEMS_TERRAFORM_MAX_TERNARIES` | 13 ecosystems B |  | 16..65536 ; consumer: pre-scan |
| `manifests.enrich.ttl` | duration | `24h` | `SCP_MANIFESTS_ENRICH_TTL` | 13 ecosystems B |  | 1h..30d ; consumer: both enrich job specs |
| `manifests.enrich.idle_backoff_cap` | duration | `168h` | `SCP_MANIFESTS_ENRICH_IDLE_BACKOFF_CAP` | 13 ecosystems B |  | at least `ttl` ; consumer: both enrich job specs |
| `manifests.enrich.max_units_per_repo` | int | 50 | `SCP_MANIFESTS_ENRICH_MAX_UNITS_PER_REPO` | 13 ecosystems B |  | 1..500 ; consumer: `FactsRequest` builder |
| `manifests.providers[].kind` | enum | required | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | `githubgraph`, `sbom`, `depsdev`; each at most once ; consumer: `cmd/scp` constructs the provider. **One flat `ProviderConfig` struct** with ... |
| `manifests.providers[].enabled` | bool | `false` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | `true` requires the collaborator (a GitHub source for the two GitHub kinds, an SSRF-guarded clien... ; consumer: constructor |
| `manifests.providers[].acknowledge_spike_sa` | bool | `false` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | kinds `githubgraph` and `sbom` only; must be `true` to enable while spike S-A is open ; consumer: `cmd/scp` validation only |
| `manifests.providers[].timeout` | duration | `60s` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 5s..10m ; consumer: `githubgraph.Config.Timeout` |
| `manifests.providers[].manifests_page` | int | 10 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..100 ; consumer: query variable `mFirst` |
| `manifests.providers[].dependencies_page` | int | 100 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..100 ; consumer: query variable `dFirst` |
| `manifests.providers[].max_pages` | int | 20 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..200 ; consumer: `githubgraph` |
| `manifests.providers[].max_dependencies` | int | 5000 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..100000 ; consumer: `githubgraph` |
| `manifests.providers[].poll_interval` | duration | `5s` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1s..1m ; consumer: `sbom` |
| `manifests.providers[].poll_timeout` | duration | `2m` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | at least `poll_interval`, at most 10m ; consumer: `sbom` |
| `manifests.providers[].max_bytes` | size | 32MiB | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1MiB..128MiB ; consumer: passed to `FetchSBOMReport` |
| `manifests.providers[].max_packages` | int | 50000 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 100..500000 ; consumer: decoder cap |
| `manifests.providers[].redirect_hosts` | []string | `[]` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | each a bare hostname or `*.suffix` ; consumer: the `github` adapter (via `cmd/scp`) |
| `manifests.providers[].base_url` | string | `https://api.deps.dev` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | https URL passing the SSRF policy ; consumer: `depsdev` and the guarded transport's host pin |
| `manifests.providers[].systems` | []string | `[nuget, rubygems]` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | subset of deps.dev's documented systems ; consumer: `depsdev` |
| `manifests.providers[].rps` | float | 2 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 0.1..20 ; consumer: limiter |
| `manifests.providers[].burst` | int | 4 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..20 ; consumer: limiter |
| `manifests.providers[].concurrency` | int | 2 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..8 ; consumer: semaphore |
| `manifests.providers[].cache_ttl` | duration | `24h` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1h..30d ; consumer: cache writes |
| `manifests.providers[].negative_ttl` | duration | `1h` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1m..24h ; consumer: cache writes |
| `manifests.providers[].request_timeout` | duration | `10s` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1s..60s ; consumer: HTTP client |
| `manifests.providers[].max_coordinates_per_repo` | int | 300 | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | 1..5000 ; consumer: `depsdev` |
| `manifests.providers[].private_group_prefixes` | []string | `[]` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | each non-empty ; consumer: added to `IsPrivate` |
| `manifests.providers[].include_private_repos` | bool | `false` | `SCP_MANIFESTS_PROVIDERS_<` | 13 ecosystems B |  | none ; consumer: `depsdev` |

### `integrations`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `integrations.jenkins.instances[]` | list | `[]` (disabled) | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>` (derived) | 14 integrations/access |  | names unique; if more than one, exactly one `default: true` ; consumer: `Config.JenkinsEnabled`, `ResolveJenkins`, `jenkins.New` |
| `integrations.jenkins.instances[].name` | string | required | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_NAME` (derived) | 14 integrations/access |  | `^[a-z0-9][a-z0-9-]{0,31}$` ; consumer: `ResolveJenkins` (descriptor/group `instance` lookup), metrics label |
| `integrations.jenkins.instances[].base_url` | string | required | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_BASE_URL` (derived) | 14 integrations/access |  | absolute `https` (`http` only with `allow_http`); host present; **no userinfo, query, fragment, e... ; consumer: `jenkins.New`, `cmd/scp` (netguard host allow-list) |
| `integrations.jenkins.instances[].default` | bool | `false` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_DEFAULT` (derived) | 14 integrations/access |  | see above; a single instance is implicitly the default ; consumer: `ResolveJenkins` |
| `integrations.jenkins.instances[].allow_http` | bool | `false` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_ALLOW_HTTP` (derived) | 14 integrations/access |  | explicit opt-in for plain http on a private instance ; consumer: `ValidateBaseURL`, `cmd/scp` (netguard `AllowHTTP`) |
| `integrations.jenkins.instances[].user` | string / Secret | unset | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_USER` (derived) | 14 integrations/access |  | **both or neither** (empty secret = unset, GR-13) ; consumer: `jenkins` (basic auth) |
| `integrations.jenkins.instances[].allow_private_cidrs` | `[]CIDR` | `[]` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_ALLOW_PRIVATE_CIDRS` (derived) | 14 integrations/access |  | valid CIDRs; `/0` refused ; consumer: `cmd/scp` -> `netguard` policy (a Jenkins on a private address is blocked unl... |
| `integrations.jenkins.instances[].ports` | `[]int` | `[]` (= 80/443) | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_PORTS` (derived) | 14 integrations/access |  | 1..65535 ; consumer: `cmd/scp` -> `netguard` |
| `integrations.jenkins.instances[].discover` | `declared`\\|`all` | `declared` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_DISCOVER` (derived) | 14 integrations/access |  | enum ; consumer: `ResolveJenkins` |
| `integrations.jenkins.instances[].job_prefix` | string | `""` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_JOB_PREFIX` (derived) | 14 integrations/access |  | valid path prefix ; consumer: `ResolveJenkins` (only with `discover: all`) |
| `integrations.jenkins.instances[].multibranch` | `auto`\\|`always`\\|`never` | `auto` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_MULTIBRANCH` (derived) | 14 integrations/access |  | enum ; consumer: `jenkins.Fetch` |
| `integrations.jenkins.instances[].timeout` | duration | `10s` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_TIMEOUT` (derived) | 14 integrations/access |  | 1s..1m ; consumer: `jenkins.New` (client timeout) |
| `integrations.jenkins.instances[].max_response_bytes` | int | `1048576` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_MAX_RESPONSE_BYTES` (derived) | 14 integrations/access |  | 1 KiB..16 MiB ; consumer: `jenkins` (`io.LimitReader`) |
| `integrations.jenkins.instances[].expose_detail_url` | bool | `false` | `SCP_INTEGRATIONS_JENKINS_INSTANCES_<I>_EXPOSE_DETAIL_URL` (derived) | 14 integrations/access |  | consumer: `jenkins` (detail URL) |
| `integrations.jenkins.ttl` | duration | `5m` | `SCP_INTEGRATIONS_JENKINS_TTL` (derived) | 14 integrations/access |  | >= 1m ; consumer: `Jobs()` (`Policy.TTL`) |
| `integrations.jenkins.sweep_timeout` | duration | `4m` | `SCP_INTEGRATIONS_JENKINS_SWEEP_TIMEOUT` (derived) | 14 integrations/access |  | > 0 and < `ttl` ; consumer: `Jobs()` (`Spec.Timeout`) |
| `integrations.jenkins.concurrency` | int | `4` | `SCP_INTEGRATIONS_JENKINS_CONCURRENCY` (derived) | 14 integrations/access |  | 1..16 ; consumer: `fetchPool` |
| `integrations.jenkins.pace` | duration | `100ms` | `SCP_INTEGRATIONS_JENKINS_PACE` (derived) | 14 integrations/access |  | >= 0 ; consumer: `fetchPool` |
| `integrations.jenkins.abort_after` | int | `5` | `SCP_INTEGRATIONS_JENKINS_ABORT_AFTER` (derived) | 14 integrations/access |  | >= 1 ; consumer: `fetchPool` |
| `integrations.jenkins.not_found_ttl` | duration | `1h` | `SCP_INTEGRATIONS_JENKINS_NOT_FOUND_TTL` (derived) | 14 integrations/access |  | >= 1m ; consumer: `Sweep` (carry) |
| `integrations.snyk.enabled` | bool | `false` | `SCP_INTEGRATIONS_SNYK_ENABLED` (derived) | 14 integrations/access |  | consumer: `Config.SnykEnabled` |
| `integrations.snyk.token` | Secret | unset | `SCP_INTEGRATIONS_SNYK_TOKEN` (derived) | 14 integrations/access |  | required when enabled; set while disabled = error (decorative key) ; consumer: `snyk.New` |
| `integrations.snyk.org_id` | string | `""` | `SCP_INTEGRATIONS_SNYK_ORG_ID` (derived) | 14 integrations/access |  | UUID when enabled ; consumer: `snyk` request path |
| `integrations.snyk.base_url` | string | `https://api.snyk.io` | `SCP_INTEGRATIONS_SNYK_BASE_URL` (derived) | 14 integrations/access |  | bare https origin (no path/query/userinfo); host in `{api.snyk.io, api.us.snyk.io, api.eu.snyk.io... ; consumer: `snyk.New`, `cmd/scp` (netguard host) |
| `integrations.snyk.allow_custom_host` | bool | `false` | `SCP_INTEGRATIONS_SNYK_ALLOW_CUSTOM_HOST` (derived) | 14 integrations/access |  | explicit acknowledgement for hosts outside the documented list (for example a government region) ; consumer: `Validate` |
| `integrations.snyk.api_version` | string | `2024-10-15` | `SCP_INTEGRATIONS_SNYK_API_VERSION` (derived) | 14 integrations/access |  | `^\d{4}-\d{2}-\d{2}(~(beta\\|experimental))?$` ; consumer: `snyk` (`version=`) |
| `integrations.snyk.origins` | `[]string` | `[]` (no filter) | `SCP_INTEGRATIONS_SNYK_ORIGINS` (derived) | 14 integrations/access |  | consumer: `snyk` (`origins=`); the legal values are not enumerated in the OpenAPI text ... |
| `integrations.snyk.fail_on` | enum | `high` | `SCP_INTEGRATIONS_SNYK_FAIL_ON` (derived) | 14 integrations/access |  | critical\\|high\\|medium\\|low ; consumer: `snyk` state mapping |
| `integrations.snyk.detail_url_template` | string | `""` (no link) | `SCP_INTEGRATIONS_SNYK_DETAIL_URL_TEMPLATE` (derived) | 14 integrations/access |  | https, placeholders only `{project_id}` `{owner}` `{repo}` ; consumer: `snyk` |
| `integrations.snyk.ttl` | duration | `15m` | `SCP_INTEGRATIONS_SNYK_TTL` (derived) | 14 integrations/access |  | >= 1m ; consumer: `Jobs()` |
| `integrations.snyk.sweep_timeout` | duration | `2m` | `SCP_INTEGRATIONS_SNYK_SWEEP_TIMEOUT` (derived) | 14 integrations/access |  | > 0, < `ttl` ; consumer: `Jobs()` |
| `integrations.snyk.timeout` | duration | `20s` | `SCP_INTEGRATIONS_SNYK_TIMEOUT` (derived) | 14 integrations/access |  | >= 1s ; consumer: `snyk.New` |
| `integrations.snyk.max_response_bytes` | int | `4194304` | `SCP_INTEGRATIONS_SNYK_MAX_RESPONSE_BYTES` (derived) | 14 integrations/access |  | 1 KiB..64 MiB ; consumer: `snyk` |
| `integrations.snyk.allow_private_cidrs` | as Jenkins | `[]` | `SCP_INTEGRATIONS_SNYK_ALLOW_PRIVATE_CIDRS` (derived) | 14 integrations/access |  | valid CIDRs (`/0` refused); ports 1..65535 (the first draft did not validate Snyk ports) ; consumer: `cmd/scp` -> netguard (GHES-style private Snyk proxies) |

### `access`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `access.mode` | `public`\\|`trusted-proxy` | `public` | `SCP_ACCESS_MODE` (derived) | 14 integrations/access | 06, 07, 15 | enum ; consumer: `New`, `AllowsNonPublic` (root `Validate`) |
| `access.trusted_proxies` | `[]CIDR` | `[]` | `SCP_ACCESS_TRUSTED_PROXIES` (derived) | 14 integrations/access |  | required non-empty in `trusted-proxy`; error in `public`; **`/0` of either family refused**; each... ; consumer: `identifier.peerTrusted` |
| `access.allow_public_proxies` | bool | `false` | `SCP_ACCESS_ALLOW_PUBLIC_PROXIES` (derived) | 14 integrations/access |  | acknowledgement ; consumer: `ParsePrefixes` |
| `access.identity.user_header` | string | `X-Forwarded-User` | `SCP_ACCESS_IDENTITY_USER_HEADER` (derived) | 14 integrations/access |  | token chars `^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`; required in `trusted-proxy`; not reserved (`Author... ; consumer: `identifier` |
| `access.identity.email_header` | string | `X-Forwarded-Email` | `SCP_ACCESS_IDENTITY_EMAIL_HEADER` (derived) | 14 integrations/access |  | as above; optional (`""` disables); fallback subject only ; consumer: `identifier` |
| `access.identity.groups_header` | string | `X-Forwarded-Groups` | `SCP_ACCESS_IDENTITY_GROUPS_HEADER` (derived) | 14 integrations/access |  | as above; optional ; consumer: `identifier` |
| `access.identity.groups_separator` | string | `,` | `SCP_ACCESS_IDENTITY_GROUPS_SEPARATOR` (derived) | 14 integrations/access |  | 1..4 bytes when a groups header is set ; consumer: `parseGroups` |
| `access.identity.max_groups` | int | `64` | `SCP_ACCESS_IDENTITY_MAX_GROUPS` (derived) | 14 integrations/access |  | 1..256 ; consumer: `parseGroups` |
| `access.identity.proxy_secret_header` | string | `""` | `SCP_ACCESS_IDENTITY_PROXY_SECRET_HEADER` (derived) | 14 integrations/access |  | **set together with `proxy_secret`** ; consumer: `identifier` |
| `access.identity.proxy_secret` | Secret | unset | `SCP_ACCESS_IDENTITY_PROXY_SECRET` (derived) | 14 integrations/access |  | together with the header; empty = unset ; consumer: `identifier` (hashed once at construction) |
| `access.view_all.groups` | `[]string` | `[]` | `SCP_ACCESS_VIEW_ALL_GROUPS` (derived) | 14 integrations/access |  | non-empty entries <= 128 bytes; requires `groups_header`; error in `public` ; consumer: `authorizer` |
| `access.view_all.subjects` | `[]string` | `[]` | `SCP_ACCESS_VIEW_ALL_SUBJECTS` (derived) | 14 integrations/access |  | non-empty entries <= 256 bytes; error in `public` ; consumer: `authorizer` |

### `web`

| Key | Type | Default | Env | Owner | Read by | Notes |
|---|---|---|---|---|---|---|
| `web.csp.mode` | enum `report_only` \\| `enforce_core`... | `report_only` | `SCP_WEB_CSP_MODE` | 06 httpapi |  | `webapp.Config.Validate()` (path-accurate error `web.csp.mode: "x" is not one of ...`; `platform/... ; consumer: `webapp.Options.CSP` |

## 4. Conflicts found and how this review settled them

| ID | Conflict | Settlement | Edited |
|---|---|---|---|
| CD-CF-01 | The root struct (16 3.4) had no `llms`, typed `manifests.providers` as a struct although architecture 16 and unit 13 say a list, and had no `manifests.enrich`; 02's section table said "not yet written" for six sections and had no `ops`/`llms` row | all 24 sections now exist in the root and in 02's table; `Providers []ProviderConfig`, `Enrich EnrichConfig` | 16 3.4, 02 6.1.2 |
| CD-CF-02 | Reserve arithmetic twice: `refresh.gate.reserves.{backfill,scheduled,webhook}` (04 D3, `refresh.NewReserveGate`) and `github.defaults.budget.reserve.*` (05, `Client.Admit`) (X6) | **05 owns it** (as architecture 6.2/8.6, ADR-015 and unit 16 D-7 say); 04 lost `NewReserveGate`, `Budget`, `Reserves` and the three keys (code and tests adjusted) | 04 (doc, `gate.go`, tests), 16 `gate.go` |
| CD-CF-03 | `webhook.redelivery.max_per_run`: 200 (04) vs 50 (05); `rate` and `max_age` only in 04 | default **50**, range 1..500; `rate` (1s) and `max_age` (72h, range 1h..168h) added to 05's struct | 04, 05 |
| CD-CF-04 | `github.defaults.webhook`: 05 lists 3 keys, 04 uses 7; `webhook.enabled` and `webhook.default` only in 04 | all declared in 05's `WebhookDefaults` and `SourceWebhook`; 04 is the consumer (`webhook.Config` is a copy filled by `cmd/scp`); ranges are 04's | 04, 05 |
| CD-CF-05 | `github.defaults.budget.{concurrency,rps,burst}` (request layer, 05) vs `refresh.upstreams.github.{concurrency,pace}` (job layer, 04); 02 Q2 suggested one knob | **both kept** (architecture 16 already shows both): the job layer limits how many jobs run, the request layer limits in-flight HTTP calls and request rate of everything the adapter does, including calls not made by a job (installation reconcile, doctor, webhook-assisted reads); the effective limit is the smaller; `Validate` warns when `budget.concurrency < upstreams.github.concurrency`. Recorded as a deviation in 93 | 93 |
| CD-CF-06 | 05 `SecretRef{File, Env}` + `Deps.LookupEnv` vs `secret.Secret` (02 Q3) | `secret.Secret`, `{file: ...}` only; `env` member and `LookupEnv` dropped | 05 |
| CD-CF-07 | `discovery.*`: 05 lists 9 keys, 08 lists 8 (two new: `removal_confirmations`, `mass_removal_hold`) under the same path; `Describe` refuses a key declared twice | 05 declares all 11; 08's table is a consumer view | 05 |
| CD-CF-08 | 06 used `server.public_listen`/`admin_listen` and `server.hsts`; 07 and 10 used `server.public_url` | nested names of 02: `server.public.listen`, `server.admin.listen`, `server.public.hsts`, `server.public.public_url` | 06, 07, 10 |
| CD-CF-09 | Keys that do not exist: `ratelimit.default` and `net.*` (10), `site.url` (11), `site.announcement.*` (18), `github.sources[].visibility` (10) | renamed to `ratelimit.media_ext`, "no `net` section", `server.public.public_url`, `pages.announcement.*`, `github.sources[].discovery.visibility` | 10, 11, 18 |
| CD-CF-10 | `catalog.Config` fails `config.Describe` (missing `jsonschema_description`, unnamed matcher fields) and a one-key matcher mapping (`match: [{prefix: x}]`) cannot be assigned by `config.Load` (16 U-1/U-2) | **open, owners 02 and 08**: recommended settlement: 08 gives `Matcher` an `UnmarshalYAML` and tags, 02's `Load` honours `yaml.Unmarshaler` while staying strict, and `catalog.schema.json` stays the schema of this subtree (08 6.4). Not changed here because it changes the code of two units | 93 |
| CD-CF-11 | Shutdown budget: architecture 10.4 says 38 s; the defaults sum to 44 s (16 D-12) | accepted: 44 s worst case, `shutdown.supervisor_grace` 45 s; architecture 10.4 amended | architecture |
| CD-CF-12 | `site.dashboard_commits` (18 D-18-03) | not adopted in v1 (CD-RT-07) | 93 |
| CD-CF-13 | `live.sse.max_per_ip` mentioned as an alternative to `ratelimit.sse_per_ip` (02 Q13) | **`ratelimit.sse_per_ip` stays** (15 consumes it; the architecture 17.4 policy lists it) | none |
| CD-CF-14 | `manifests.acquisition.disabled_ecosystems` appears in 11 and 13; `store.cache_caps.depsdev` in 03 and 13 | single declaration each (11, 03); 13 is the consumer | none (checked) |
| CD-CF-15 | architecture 16 shows `manifests: {allow_list: default, ...}`, `github.defaults.budget.reserve: 10%` and `media.signing_key: {file}` | `allow_list` has no counterpart (11 uses `acquisition.*`), `reserve` is three class keys, `signing_key` is a `secret.Secret` as shown; architecture 16 outline updated | architecture |

## 5. Checks that were run

* Every key-like token in the eighteen designs (`root.section.key` form) was compared with this table: the only unmatched ones are Go identifiers, file names, slot or job names and the five renamed or removed in CD-CF-08/09.
* `access.mode` takes `public` or `trusted-proxy` (14); 06 and 05 use the same spelling (05 text "trusted-proxy"; 06 "mode != public").
* Defaults that the architecture outline fixes were compared: `server.timeouts`, `log`, `store.cache_caps` (64/128/64 MiB), `refresh`, `commits.keep 30`, `commits.latest_n 50`, `badges`, `live.sse.enabled false`, `ratelimit` (badges 120/min, api 300/min, media_ext 60/min, csp_reports 30/min): all equal.
* Generated references of unit 02 (`02-platform.assets/out/{reference.md,env.txt,schema.json}`) and unit 16 (`docs/reference/*`) describe only the platform skeleton (6 sections plus three stand-ins). They must be regenerated from the full root in the new repository (`go generate ./cmd/scp`); this table is what they have to equal.
