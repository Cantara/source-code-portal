| Key | Type | Default | Environment | Description |
|---|---|---|---|---|
| `version` | integer | `1` | `SCP_VERSION` | Config schema version. Must be 1. |
| `server` | object |  |  | HTTP listeners. |
| `server.public` | object |  |  | Public listener: SPA, API, badges, media, webhooks, probes. |
| `server.public.listen` | string | `":9090"` | `SCP_SERVER_PUBLIC_LISTEN` | Listen address host:port. An empty host binds every interface. |
| `server.public.public_url` | string | `""` | `SCP_SERVER_PUBLIC_PUBLIC_URL` | External https URL of the portal, used for absolute links, webhook URLs and the CSRF trusted origin. Empty means relative links only. |
| `server.public.hsts` | boolean | `false` | `SCP_SERVER_PUBLIC_HSTS` | Send Strict-Transport-Security on HTTPS requests. Leave false when the TLS-terminating proxy owns HSTS. |
| `server.public.trusted_proxies` | list | `[]` | `SCP_SERVER_PUBLIC_TRUSTED_PROXIES` | CIDRs or addresses of reverse proxies whose X-Forwarded-For and X-Forwarded-Proto are believed for the client IP. 0.0.0.0/0 and ::/0 are refused. Independent of access.trusted_proxies. |
| `server.admin` | object |  |  | Admin listener: metrics, pprof and operator endpoints. Bind to a restricted network. |
| `server.admin.listen` | string | `":9091"` | `SCP_SERVER_ADMIN_LISTEN` | Listen address host:port. Never expose publicly. |
| `server.admin.bearer_token` | secret |  | `SCP_SERVER_ADMIN_BEARER_TOKEN` or `SCP_SERVER_ADMIN_BEARER_TOKEN_FILE` | Optional bearer token required on every admin request except the probes (file reference or SCP_SERVER_ADMIN_BEARER_TOKEN[_FILE]). Empty means network restriction only. |
| `server.admin.allowed_hosts` | list | `[]` | `SCP_SERVER_ADMIN_ALLOWED_HOSTS` | Optional Host header allow-list for the admin listener (host or host:port, case-insensitive); a request with another Host gets 421. A defence against DNS rebinding from an operator's browser. Empty means no check. |
| `server.timeouts` | object |  |  | http.Server timeouts shared by both listeners. |
| `server.timeouts.read_header` | duration | `"5s"` | `SCP_SERVER_TIMEOUTS_READ_HEADER` | Time allowed to read request headers. |
| `server.timeouts.read` | duration | `"30s"` | `SCP_SERVER_TIMEOUTS_READ` | Time allowed to read the whole request. |
| `server.timeouts.write` | duration | `"60s"` | `SCP_SERVER_TIMEOUTS_WRITE` | Time allowed to write the response. Streaming handlers extend it per response. |
| `server.timeouts.idle` | duration | `"120s"` | `SCP_SERVER_TIMEOUTS_IDLE` | Keep-alive idle timeout. |
| `server.timeouts.max_header_bytes` | bytes | `"16KiB"` | `SCP_SERVER_TIMEOUTS_MAX_HEADER_BYTES` | Maximum request header size. |
| `server.timeouts.max_header_values` | integer | `100` | `SCP_SERVER_TIMEOUTS_MAX_HEADER_VALUES` | Maximum number of header values in one request (Go 1.27 MaxHeaderValueCount). |
| `log` | object |  |  | Logging. |
| `log.level` | string | `"info"` | `SCP_LOG_LEVEL` | Minimum level that is written. |
| `log.format` | string | `"json"` | `SCP_LOG_FORMAT` | json for production, text for a developer terminal. |
| `log.add_source` | boolean | `false` | `SCP_LOG_ADD_SOURCE` | Add the source file and line to every record. |
| `telemetry` | object |  |  | Metrics, traces and OTLP export. |
| `telemetry.prometheus` | boolean | `true` | `SCP_TELEMETRY_PROMETHEUS` | Serve Prometheus metrics at GET /metrics on the admin listener. |
| `telemetry.otlp` | object |  |  | Optional OTLP/HTTP export of traces, metrics and logs. |
| `telemetry.otlp.enabled` | boolean | `false` | `SCP_TELEMETRY_OTLP_ENABLED` | Export over OTLP/HTTP. Off by default. |
| `telemetry.otlp.endpoint` | string | `""` | `SCP_TELEMETRY_OTLP_ENDPOINT` | Collector base URL, for example https://otel.example.com:4318. Required when enabled. |
| `telemetry.otlp.headers_file` | string | `""` | `SCP_TELEMETRY_OTLP_HEADERS_FILE` | Absolute path of a file with one Name: value header per line (credentials live here, not in the config file). |
| `telemetry.otlp.timeout` | duration | `"10s"` | `SCP_TELEMETRY_OTLP_TIMEOUT` | Per-export timeout. |
| `telemetry.otlp.allow_private_cidrs` | list | `[]` | `SCP_TELEMETRY_OTLP_ALLOW_PRIVATE_CIDRS` | CIDRs the collector may live in when it is on a private network (cmd/scp builds the netguard policy). A /0 range is refused. |
| `telemetry.otlp.ports` | list | `[]` | `SCP_TELEMETRY_OTLP_PORTS` | Extra destination ports besides 80 and 443, for example 4318. |
| `telemetry.otlp.signals` | object |  |  | Which signals to export. |
| `telemetry.otlp.signals.traces` | boolean | `true` | `SCP_TELEMETRY_OTLP_SIGNALS_TRACES` | Export traces. |
| `telemetry.otlp.signals.metrics` | boolean | `true` | `SCP_TELEMETRY_OTLP_SIGNALS_METRICS` | Export metrics. |
| `telemetry.otlp.signals.logs` | boolean | `false` | `SCP_TELEMETRY_OTLP_SIGNALS_LOGS` | Export logs (bridged from slog by cmd/scp). |
| `telemetry.traces` | object |  |  | Trace sampling. |
| `telemetry.traces.sample_ratio` | number | `0.1` | `SCP_TELEMETRY_TRACES_SAMPLE_RATIO` | Parent-based head sampling ratio for root spans, 0 to 1. |
| `telemetry.runtime` | object |  |  | Go runtime metrics. |
| `telemetry.runtime.enabled` | boolean | `true` | `SCP_TELEMETRY_RUNTIME_ENABLED` | Emit go.* runtime metrics. |
| `ratelimit` | object |  |  | Keyed rate limiter. |
| `ratelimit.badges` | text | `"120/min burst 60"` | `SCP_RATELIMIT_BADGES` | Class badge: GET /badges/*. |
| `ratelimit.api` | text | `"300/min burst 100"` | `SCP_RATELIMIT_API` | Class api: /api/v1/*. |
| `ratelimit.media_ext` | text | `"60/min burst 20"` | `SCP_RATELIMIT_MEDIA_EXT` | Class media_ext: /media/ext/* (external image proxy). |
| `ratelimit.csp_reports` | text | `"30/min burst 10"` | `SCP_RATELIMIT_CSP_REPORTS` | Class csp: POST /csp-reports. |
| `ratelimit.webhook_invalid` | text | `"10/min burst 10"` | `SCP_RATELIMIT_WEBHOOK_INVALID` | Invalid webhook signatures allowed per client IP before 429. Valid HMACs are never limited. |
| `ratelimit.sse_per_ip` | integer | `4` | `SCP_RATELIMIT_SSE_PER_IP` | Concurrent SSE streams per client IP. |
| `ratelimit.max_keys` | integer | `50000` | `SCP_RATELIMIT_MAX_KEYS` | Upper bound on tracked client keys per limiter class; excess keys share an overflow bucket. |
| `ratelimit.idle_ttl` | duration | `"10m"` | `SCP_RATELIMIT_IDLE_TTL` | A key idle for this long is evicted by the sweeper. |
| `shutdown` | object |  |  | Graceful shutdown budget. |
| `shutdown.drain_delay` | duration | `"5s"` | `SCP_SHUTDOWN_DRAIN_DELAY` | After SIGTERM, /readyz turns 503 and the process keeps serving for this long so load balancers stop sending traffic. |
| `shutdown.server_timeout` | duration | `"15s"` | `SCP_SHUTDOWN_SERVER_TIMEOUT` | Grace for in-flight HTTP requests; the public and admin listeners stop together within it. |
| `shutdown.store_timeout` | duration | `"3s"` | `SCP_SHUTDOWN_STORE_TIMEOUT` | Grace for the store to flush planner state and close. |
| `shutdown.telemetry_timeout` | duration | `"2s"` | `SCP_SHUTDOWN_TELEMETRY_TIMEOUT` | Grace for the final metric and trace flush. |
| `shutdown.supervisor_grace` | duration | `"45s"` | `SCP_SHUTDOWN_SUPERVISOR_GRACE` | The termination grace period the supervisor grants (Kubernetes terminationGracePeriodSeconds). Startup warns when the sum of the shutdown caps exceeds it; 0 disables the check. |
| `catalog` | object |  |  | Groups and repository selection. |
| `catalog.ungrouped` | string | `"track"` | `SCP_CATALOG_UNGROUPED` | track or ignore repositories no group selects. |
| `catalog.groups` | list |  |  | Groups. |
| `catalog.groups[].id` | string |  | `SCP_CATALOG_GROUPS_{i}_ID` | Group id. |
| `catalog.groups[].display_name` | string |  | `SCP_CATALOG_GROUPS_{i}_DISPLAY_NAME` | Display name. |
| `store` | object |  |  | SQLite store. |
| `store.path` | string | `"/data/scp.db"` | `SCP_STORE_PATH` | SQLite file; its directory must be a local writable volume (never NFS, EFS or SMB). |
| `refresh` | object |  |  | Refresh engine: upstream pacing, backoff, grace. |
| `refresh.upstreams` | map | `{"deps-dev":{"concurrency":1,"job_timeout":"2m","pace":"0s"},"github":{"concurrency":4,"job_timeout":"2m","pace":"100ms"},"jenkins":{"concurrency":1,"job_timeout":"5m","pace":"0s"},"snyk":{"concurrency":1,"job_timeout":"3m","pace":"0s"}}` |  | Per-upstream concurrency, pacing and job timeout. Every upstream a job kind names must be listed (startup refuses an unknown one); environment variables can change existing entries only. |
| `refresh.upstreams.*.concurrency` | integer |  | `SCP_REFRESH_UPSTREAMS_{k}_CONCURRENCY` | Jobs of this upstream that may run at once. |
| `refresh.upstreams.*.pace` | duration |  | `SCP_REFRESH_UPSTREAMS_{k}_PACE` | Minimum spacing between job starts of this upstream; 0 means none. |
| `refresh.upstreams.*.job_timeout` | duration |  | `SCP_REFRESH_UPSTREAMS_{k}_JOB_TIMEOUT` | Default per-run deadline of a job of this upstream when its spec sets none. |
| `refresh.backoff` | object |  |  | Retry backoff of failed jobs: full jitter between base and min(cap, base*2^attempt). |
| `refresh.backoff.base` | duration | `"2s"` | `SCP_REFRESH_BACKOFF_BASE` | First retry delay. |
| `refresh.backoff.cap` | duration | `"15m"` | `SCP_REFRESH_BACKOFF_CAP` | Upper bound of a retry delay. |
| `refresh.backoff.max_attempts` | integer | `8` | `SCP_REFRESH_BACKOFF_MAX_ATTEMPTS` | Attempts after which the job is reported exhausted once; retries then continue at the cap. |
| `refresh.grace` | duration | `"15s"` | `SCP_REFRESH_GRACE` | How long in-flight jobs may finish after shutdown begins; they are canceled after that. Also sizes the refresh stage's stop timeout (grace plus 4 s). |
| `refresh.planner_tick` | duration | `"15s"` | `SCP_REFRESH_PLANNER_TICK` | Interval of the planner's scan for due items (the planner.tick task). |
| `refresh.max_keys` | integer | `50000` | `SCP_REFRESH_MAX_KEYS` | Cap of the engine's keyed table (queued plus running plus parked keys); an Enqueue beyond it fails with job.ErrQueueFull. |
| `refresh.parking` | object |  |  | Poison-key parking. |
| `refresh.parking.retry` | duration | `"24h"` | `SCP_REFRESH_PARKING_RETRY` | Interval of the single retry probe of a parked key. |
