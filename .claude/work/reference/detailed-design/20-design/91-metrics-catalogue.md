# 91 - Metrics catalogue (all units, 2026-10-14)

Written by the cross-design consistency review. It lists **every instrument that the unit designs create**, with its owner, and settles the collisions between units. It is generated from the section 9 tables of the units after the review's edits; the unit table is still the authority for semantics, this document is the authority for **ownership, naming and the rules below**. It supersedes the list in architecture 18.2 (which is amended to point here) and the partial index in unit 15 section 9.3 (which omitted most manifests, docs, media and reactor metrics).

179 names (176 parsed plus three single-row companions). All instruments are created from an injected `metric.Meter` (G11); names are constants next to the instrument; `cmd/scp` boots every constructor against an in-memory reader and generates `docs/reference/metrics.md`, which must equal this catalogue (the test fails on a name that is in only one of them).

## 1. Rules (new rules are marked with their finding)

1. **Prefix and suffix.** Every name starts `scp_`. Counters end `_total`; histograms of seconds end `_seconds`, of bytes `_bytes`; the OTel Prometheus exporter *rewrites* a name that does not follow this: verified 2026-10-14 against the exporter in the module graph (a counter without `_total` gets it, a histogram with unit `s` gets `_seconds`, **a gauge with unit `1` gets `_ratio`**). Consequence (CD-MT-03): never declare unit `1` on a gauge. Two instruments did and were fixed (`scp_integrations_enabled` in unit 14's code, `scp_manifests_provider_breaker_state` in unit 13's table); a count of things uses no unit or a UCUM annotation in braces (`{repository}`, `{request}`), which the exporter drops.
2. **Label vocabulary.** A label value is a closed enum, a configured name, or a bounded identifier. **Never** a repository, owner, path, URL, token, user, request id or free text. The bounded identifiers that exist: `source` (configured sources), `installation` (App installations, tens), `group` (at most `MaxGroups` 500, only on `scp_catalog_group_members`), `kind` (job/item families, about 15), `task`, `reactor` (4), `ecosystem` (17), `parser` (27), `provider` (4), `upstream` (4), `class` (6), `stage` (6).
3. **`result` versus `outcome`.** Both spellings exist and are kept as the units wrote them (`outcome`: sync engine, scheduler, kernel reactor, media fetch, integrations fetch, provider requests, parse; `result`: everything else). The one place where the architecture's text disagrees with its unit is `scp_docs_render_total` (architecture: `{outcome}`, unit 10: `{result}`); architecture 18.2 is corrected to the unit's name (CD-MT-04). A new instrument chooses `result` unless it is a job or reactor outcome.
4. **Who counts a store error.** `scp_store_errors_total{op}` is incremented **only** by `store/sqlite` (and `store/memory` in tests) for a failed SQLite call. A feature that wants its own signal counts a feature-prefixed counter (`scp_catalog_store_errors_total`, `scp_commits_persist_errors_total`) and does **not** touch `scp_store_errors_total`. Unit 10's bump of `op="docs.put"` was replaced by `scp_docs_persist_errors_total` (CD-MT-02); unit 04's text ("calls fail inside the bridge, are counted (`scp_store_errors_total{op}`)") is consistent, because the store counts the failed call it was asked to make.
5. **`scp_item_age_seconds{kind,stat}` is emitted by `ops` (unit 15, D15-6)**, from `freshness.Info.FetchedAt`; the engine, the planner and unit 09 do not (the text of 01, 04 and 09 was corrected, CD-MT-02).
6. **Histogram buckets.** The platform telemetry View applies default buckets to unit-`s` histograms and, as verified by unit 15 with SDK v1.47.0, **overrides** the explicit bucket advice of an instrument (R-02-1). The View must be changed to keep the advice when an instrument declares it; otherwise every histogram below has the default buckets and the latency figures in the units' alert tables (for example the 1 ms .. 10 s render buckets) are wrong. Open item for unit 02 (CD-MT-06).
7. **Observable gauges** register one callback per meter (never a callback that reads a lock the hot path holds); absent-until-first-value gauges (`scp_store_backup_age_seconds`, `scp_ready_since_timestamp_seconds`) are absent, not zero.

## 2. Collisions and gaps found

| ID | Finding | Settlement |
|---|---|---|
| CD-MT-01 | `scp_lifecycle_phase_seconds`: 02 says `phase` drain, stop, close and `outcome` ok, error, timeout; 16 said start, drain, stop, close and ok, error (its code recorded `error` for a stop timeout) | 02's vocabulary; 16's table and code corrected (`timeout` = `ErrStopTimeout`; no start phase) |
| CD-MT-02 | Ownership of `scp_item_age_seconds` (arch: engine; 09: engine; 04, 15: `ops`) and of `scp_store_errors_total` (store vs 04 bridge vs 10) | rules 4 and 5 |
| CD-MT-03 | Gauge unit `1` produces `_ratio` names | rule 1; 14 code and 13 table corrected |
| CD-MT-04 | Architecture 18.2 lists `scp_docs_render_total{outcome}` (10: `result`), omits 02's `scp_ratelimit_keys` and `scp_lifecycle_phase_seconds`, and gives `scp_csp_reports_total` and `scp_webhook_total` fewer labels and values than 06 and 04 | architecture 18.2 replaced by a pointer to this catalogue; the 14 `scp_webhook_total` result values (04 D5), the `{directive, disposition}` labels of `scp_csp_reports_total` and `{kind, encoding}` of `scp_webapp_responses_total` (06 D10) are accepted |
| CD-MT-05 | Four near-identical reaction counters with four names: `scp_catalog_react_total`, `scp_commits_reactions_total`, `scp_docs_reactions_total`, `scp_manifests_reactor_total`, next to the kernel's `scp_reactor_runs_total{reactor,kind,outcome}` | **kept** (renaming four implemented instruments is churn; the kernel metric is the cross-feature one and the four feature counters carry decisions that the kernel counter cannot see). Recommendation for the implementation: pick `..._reactions_total` for all four when the code is written |
| CD-MT-06 | The telemetry View overrides histogram bucket advice (15 R-02-1) | open for unit 02 (rule 6) |
| CD-MT-07 | Unit 15 section 9.3 claims to be "complete" but omits unit 01, 10, 11, 12 and 13 metrics | this catalogue replaces it; 15's section is marked superseded |
| CD-MT-08 | `scp_manifests_limit_hits_total{limit}`: unit 12 allows "the suffix of any `limit.<name>` a parser's `LimitError` can carry" | the label must be a closed set: the `limit.<name>` values are constants (unit 11 `limits`, units 12 and 13 per parser); **requirement on units 11, 12 and 13**: one test per package enumerates them and a `LimitError` with any other name is mapped to `other` |
| CD-MT-09 | `scp_client_errors_total{kind}` appears in unit 17 | not a v1 metric (17 Q3): no endpoint, no instrument; not in this catalogue |

## 3. Cardinality review (worst case series, public deployment of 1,000 repositories and 4 sources)

* No label carries a repository. The widest instruments are `scp_manifests_parse_total{ecosystem,parser,outcome}` (27 parsers x 5 outcomes = 135), `scp_sync_jobs_total{kind,outcome}` (about 15 x 9 = 135), `scp_github_requests_total{source,resource,status_class}` (4 x 5 x 5 = 100), `scp_github_rate_remaining{source,installation,resource}` (installations x 5; fifty installations give 250), `scp_reactor_runs_total{reactor,kind,outcome}` (at most 4 x 8 x 4 = 128, unit 01) and `scp_docs_fetch_total{kind,result}` (5 x about 8).
* `scp_catalog_group_members{group}` is the only instrument labelled by an operator-chosen id; it is bounded by `MaxGroups` (500) and is the one to drop first if a deployment shows cardinality trouble.
* `http.server.request.duration` is labelled by the mux **pattern**, never the raw path (A-24).

## 4. The catalogue

### composition

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_reactor_runs_total` | counter | `reactor`, `kind` (event kind), `outcome` | 16 cmd/scp | 01 | `bridge.NewReactorMetrics` |
| `scp_reactor_seconds` | histogram | `reactor` | 16 cmd/scp | 01 | `bridge.NewReactorMetrics` |

### platform

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_build_info` | gauge | `version`, `commit`, `go` | 02 platform | 15 | `telemetry` |
| `scp_ratelimit_rejected_total` | counter | `class` (`badges`, `api`, `media_ext`, `csp_reports`, `webhook_invalid`) | 02 platform | 15 | `httpx.Limiter` (counter injected through `LimiterOptions.Rejected`; one instrument shared by every class; ... |
| `scp_ratelimit_keys` | gauge | `class` | 02 platform | 15, 16 | `cmd/scp` (observable, callback = `Limiter.Len`) |
| `scp_lifecycle_phase_seconds` | histogram | `stage`, `phase` (`drain\|stop\|close`; `run` is never observed), `outcome` (`ok\|error\|timeout`) | 02 platform | 15, 16 | `cmd/scp` (`Plan.Observe`) |

### snapshot and store

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_snapshot_publish_total` | counter | `result` = `ok` \| `invalid` \| `aborted` | 03 store | 15 | publish attempts; `invalid` = projector or validator rejected (Permanent); `aborted` = `fn` returned an err... |
| `scp_snapshot_publish_seconds` | histogram | none | 03 store | 15 | duration of one successful `Apply` (build, project, validate, swap); buckets 50us, 100us, 250us, 500us, 1ms... |
| `scp_snapshot_version` | gauge (observable) | none | 03 store | 15 | current version |
| `scp_snapshot_age_seconds` | gauge (observable) | none | 03 store |  | seconds since the current snapshot was built (data age is the freshness model's, not this) |
| `scp_snapshot_repos` | gauge (observable) | none | 03 store |  | entries in the current snapshot |
| `scp_store_state` | gauge (observable) | none | 03 store | 15 | 0 ok, 1 degraded (writes suspended) |
| `scp_store_errors_total` | counter | `op` | 03 store | 15 | store operations that failed with a SQLite error (context errors and typed store errors excluded) |
| `scp_store_rebuilds_total` | counter | `reason` = `corrupt` \| `not_a_database` \| `schema_newer` \| `migration_failed` \| `schema_drift` | 03 store | 15 | starts from a quarantined database |
| `scp_store_tx_seconds` | histogram | `op` | 03 store | 15 | duration of one store call (one transaction); buckets 0.5ms to 5s |
| `scp_store_cache_bytes` | gauge (observable) | `ns` | 03 store | 15 | bytes held per cache namespace |
| `scp_store_cache_ops_total` | counter | `ns`, `result` = `hit` \| `miss` | 03 store | 15 | `GetCache` lookups |
| `scp_store_cache_evictions_total` | counter | `ns` | 03 store | 15 | entries evicted by the LRU cap |
| `scp_store_commits_trimmed_total` | counter | none | 03 store | 15 | commit rows deleted by retention |
| `scp_store_pruned_total` | counter | `table` = `item` \| `cache` | 03 store | 15 | rows deleted at open (kind or namespace no longer registered) |
| `scp_store_plan_rows_skipped_total` | counter | `reason` = `global` \| `no_item` | 03 store | 15 | `SavePlans` rows not persisted: a global job kind (expected) or a row that matched no item (a growing `no_i... |
| `scp_store_db_bytes` (and `scp_store_wal_bytes`, same row of unit 03) | gauge (observable) | none | 03 store | 15 | file sizes (stat at scrape) |
| `scp_store_backup_total` | counter | `result` = `ok` \| `error` | 03 store | 15 | backups |
| `scp_store_backup_seconds` | histogram | none | 03 store | 15 | duration of one backup |
| `scp_store_backup_age_seconds` | gauge (observable) | none | 03 store | 15 | seconds since the last successful backup; **absent** until the first one |

### sync engine, planner, scheduler, webhook

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_sync_pending` | gauge | `upstream` | 04 refresh/webhook | 15 | ready + delayed entries |
| `scp_sync_running` | gauge | `upstream` | 04 refresh/webhook |  | running jobs |
| `scp_sync_parked` | gauge |  | 04 refresh/webhook | 15 | parked keys |
| `scp_upstream_paused_seconds` | gauge | `upstream` | 04 refresh/webhook | 15 | seconds until the pause ends, 0 when not paused |
| `scp_sync_jobs_total` | counter | `kind`, `outcome` in `started success coalesced marked_dirty retried ratelimited parked parked_suppressed panic exhausted gone canceled queue_full` | 04 refresh/webhook | 15 | architecture 18.2 set plus `started`, `parked_suppressed`, `gone`, `canceled`, `queue_full` |
| `scp_job_duration_seconds` | histogram | `kind` | 04 refresh/webhook | 15 | run time |
| `scp_job_wait_seconds` | histogram | `kind` | 04 refresh/webhook |  | `now - runAt` at start (queue latency) |
| `scp_job_overrun_total` | counter | `kind` | 04 refresh/webhook | 15 | returned after its deadline |
| `scp_job_ignored_cancel_total` | counter | `kind` | 04 refresh/webhook |  | stuck: ignored cancellation for 5 s |
| `scp_gate_denied_total` | counter | `upstream`, `priority` in `interactive webhook scheduled backfill` | 04 refresh/webhook | 15 | gate denials per dispatch pass (names, not numbers: kernel priorities are 1-based) |
| `scp_webhook_to_publish_seconds` | histogram | `kind` | 04 refresh/webhook | 15 | **end-to-end freshness**: time from the first event-priority (`PrioInteractive`/`PrioWebhook`) enqueue of a... |
| `scp_planner_tick_seconds` | histogram |  | 04 refresh/webhook | 15 | duration of a `Tick` |
| `scp_planner_enqueue_total` | counter | `kind`, `result` in `ok closed queue_full error` | 04 refresh/webhook | 15 | planner enqueue outcomes |
| `scp_planner_subjects` | gauge | `kind` | 04 refresh/webhook | 15 | subjects per kind |
| `scp_task_runs_total` | counter | `task`, `outcome` in `ok error` | 04 refresh/webhook | 15 | scheduler runs |
| `scp_task_overruns_total` | counter | `task` | 04 refresh/webhook | 15 | `duration > Every` |
| `scp_webhook_total` | counter | `result` in `accepted duplicate invalid_signature malformed ignored ping queue_full unknown_source too_large unsupported_media throttled busy body_... | 04 refresh/webhook | 15 | the architecture's six values plus eight additive ones (D5) |
| `scp_webhook_body_bytes` | histogram |  | 04 refresh/webhook | 15 | accepted body sizes (sizing `max_body`) |
| `scp_webhook_dedupe_entries` | gauge |  | 04 refresh/webhook | 15 | size of the de-dup set |
| `scp_webhook_last_accepted_timestamp_seconds` | gauge |  | 04 refresh/webhook | 15 | unix time of the last accepted delivery (alert on silence) |
| `scp_webhook_redelivery_total` | counter | `source`, `result` in `redelivered gone failed gap` | 04 refresh/webhook | 15 | sweep outcomes |

### live, ops, overview

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_item_age_seconds` | gauge (observable) | `kind`, `stat` = `max`, `p95` | 15 live/ops/overview | 04 | `ops` |
| `scp_sse_clients` | gauge (observable) | none | 15 live/ops/overview | 07 | `live` |
| `scp_sse_connections_total` | counter | `result` = `accepted`, `rejected_ip`, `rejected_total`, `disabled`, `drained` | 15 live/ops/overview |  | `live` |
| `scp_sse_events_total` | counter | `event` = `hello`, `invalidate`, `resync` | 15 live/ops/overview |  | `live` |
| `scp_sse_write_errors_total` | counter | none | 15 live/ops/overview |  | `live` |
| `scp_sse_stream_seconds` | histogram | none | 15 live/ops/overview |  | `live` |
| `scp_snapshot_polls_total` | counter | `result` = `ok`, `not_modified` | 15 live/ops/overview |  | `live` |
| `scp_start_time_seconds` | gauge (observable) | none | 15 live/ops/overview |  | `ops` |
| `scp_readiness_state` | gauge (observable) | `phase` = `warming`, `degraded`, `ready`, `shutting-down` | 15 live/ops/overview |  | `ops` |
| `scp_readiness_gate` | gauge (observable) | `gate` = `store`, `catalogue`, `coverage`, `startup_deadline` | 15 live/ops/overview |  | `ops` |
| `scp_ready_since_timestamp_seconds` | gauge (observable) | none | 15 live/ops/overview |  | `ops` |
| `scp_coverage_ratio` | gauge (observable) | `kind` (item family) | 15 live/ops/overview |  | `ops` |
| `scp_items` | gauge (observable) | `kind`, `status` = `unknown`, `fresh`, `stale`, `error` | 15 live/ops/overview |  | `ops` |
| `scp_liveness_heartbeat_age_seconds` | gauge (observable) | none | 15 live/ops/overview |  | `ops` |
| `scp_status_warnings` | gauge (observable) | `severity` = `info`, `warning`, `critical` | 15 live/ops/overview |  | `ops` |
| `scp_status_build_seconds` | histogram | none | 15 live/ops/overview |  | `ops` |
| `scp_admin_actions_total` | counter | `action` = `retry`, `result` = `accepted`, `unsupported_media_type`, `forbidden`, `too_large`, `invalid`, `unknown_key`, `queue_full`, `closed`, `e... | 15 live/ops/overview |  | `ops` |
| `scp_llms_requests_total` | counter | `output` = `index`, `group`, `repo`, `full_root`, `full_group`, `full_repo`; `result` = `ok`, `not_modified`, `not_found`, `disabled`, `overloaded`... | 15 live/ops/overview |  | `overview` |
| `scp_llms_generate_seconds` | histogram | `output` | 15 live/ops/overview |  | `overview` |
| `scp_llms_output_bytes` | histogram | `output` | 15 live/ops/overview |  | `overview` |
| `scp_llms_truncated_total` | counter | `output`, `reason` = `size`, `index_chars` | 15 live/ops/overview |  | `overview` |
| `scp_llms_docs_omitted_total` | counter | `reason` = `unavailable`, `size` | 15 live/ops/overview |  | `overview` |
| `scp_sitemap_urls` | gauge | none | 15 live/ops/overview |  | `overview` |
| `scp_sitemap_truncated_total` | counter | none | 15 live/ops/overview |  | `overview` |
| `scp_alias_redirects_total` | counter | `result` = `redirected`, `hidden`, `live_wins` | 15 live/ops/overview |  | `overview` |
| `scp_negotiated_requests_total` | counter | `route` = `repo`, `group`; `representation` = `html`, `markdown`; `source` = `readme`, `index`, empty | 15 live/ops/overview |  | `overview` |

### GitHub adapter

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_github_requests_total` | counter | `source`, `resource`, `status_class` | 05 github |  | every outbound call |
| `scp_github_request_seconds` | histogram | `resource` | 05 github |  | latency including queueing for the limiter |
| `scp_github_rate_remaining` | gauge (observable) | `source`, `installation`, `resource` | 05 github |  | last observed `x-ratelimit-remaining` |
| `scp_github_rate_reset_seconds` | gauge (observable) | same | 05 github |  | seconds until the bucket resets |
| `scp_github_token_expires_seconds` | gauge (observable) | `installation` | 05 github |  | seconds until the cached installation token expires (absent when none) |
| `scp_github_breaker_state` | gauge (observable) | `upstream` | 05 github |  | 0 closed, 1 half-open, 2 open |
| `scp_github_installations` | gauge (observable) | `state` | 05 github |  | registry size per `InstallationState` |
| `scp_github_conditional_total` | counter | `result` (`200`, `304`) | 05 github |  | conditional requests by outcome; the 304 ratio alerts the validator problem (under 70 percent, research 09 ... |
| `scp_github_errors_total` | counter | `kind` (closed set of 3.3), `op` | 05 github |  | typed failures returned |
| `scp_github_skipped_total` | counter | `reason` | 05 github |  | repositories dropped by policy or malformed (`disabled`, `archived`, `fork`, `private`, `bad_name`, `no_def... |
| `scp_github_probe_missing_total` | counter |  | 05 github |  | repositories absent from a `ProbeHeads` result |
| `scp_github_graphql_cost_total` | counter | `source` | 05 github |  | sum of `rateLimit.cost` (checks the 1 point per 50 repositories estimate) |
| `scp_github_token_mint_total` | counter | `result` (`ok`, `suspended`, `credential`, `unavailable`) | 05 github |  | installation token mints |
| `scp_github_tree_memo_total` | counter | `result` (`hit`, `miss`, `shared`) | 05 github |  | `ListTree` sharing between features |
| `scp_github_redirect_total` | counter |  | 05 github |  | followed same-origin redirects |
| `scp_github_gate_wait_seconds` | gauge (observable) | `class` | 05 github |  | what `Admit` would answer now |

### HTTP and web app

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_csp_reports_total` | counter | {directive, disposition} | 06 httpapi | 17 | **in the architecture without labels** (18.2); labels added here (D10) |
| `scp_webapp_responses_total` | counter | {kind, encoding} | 06 httpapi | 17 | proposed (D10): shows br adoption, stale-deploy 404s and the real-404 rate |
| `scp_api_rejected_total` | counter | {status, code} | 06 httpapi |  | proposed (D10): validator rejections (400/404/405/413/415); the SPA/ops can alert on a 400 spike after a de... |

### HTTP

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_api_deprecated_requests_total` | counter | {operation} | 07 contract |  |  |

### catalogue

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_catalog_repositories` | gauge | `source`, `state` = tracked, quarantined, held | 08 catalog |  | repositories by state, refreshed after every publish and at warm start |
| `scp_catalog_excluded` | gauge | `source`, `reason` = invalid, visibility, archived, fork, ungrouped | 08 catalog |  | listed repositories the policy kept out (admin information; the same numbers as `catalog.sync`) |
| `scp_catalog_groups` | gauge | `state` = ok, degraded | 08 catalog |  | groups with a resolved default and at least one member / the rest |
| `scp_catalog_group_members` | gauge | `group` | 08 catalog |  | members per group (bounded by `MaxGroups`) |
| `scp_catalog_reconcile_total` | counter | `source`, `result` = ok, failed, held, skipped | 08 catalog |  | reconcile outcomes per source |
| `scp_catalog_reconcile_seconds` | histogram | `source` | 08 catalog |  | listing plus commit |
| `scp_catalog_list_pages_total` | counter | `source`, `result` = 200, 304 | 08 catalog |  | pages fetched; the 304 share shows the ETag saving |
| `scp_catalog_changes_total` | counter | `source`, `change` = added, removed, renamed, updated | 08 catalog |  | repositories changed by reconciles |
| `scp_catalog_heads_probe_total` | counter | `result` = ok, error, missing | 08 catalog |  | repositories probed |
| `scp_catalog_heads_seconds` | histogram | - | 08 catalog |  | duration of one sweep |
| `scp_catalog_head_changes_total` | counter | - | 08 catalog |  | repositories whose HEAD moved |
| `scp_catalog_head_events_total` | counter | `result` = emitted, error | 08 catalog |  | synthetic `head_changed` events handed to the fan |
| `scp_catalog_react_total` | counter | `kind`, `result` = enqueued, hidden, ignored, unknown_repo, error | 08 catalog |  | webhook events handled by `React` |
| `scp_catalog_membership_build_seconds` | histogram | - | 08 catalog |  | duration of `BuildMembership` |
| `scp_catalog_store_errors_total` | counter | `op` = replace_catalogue, stats | 08 catalog |  | store writes that failed |

### commits and badges

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_commits_refresh_total` | counter | `result`: `changed`, `unchanged`, `skipped_head`, `not_modified`, `empty`, `error`, `deferred`, `gone` | 09 commits/badges |  | outcome of `commits.refresh` |
| `scp_commits_dropped_total` | counter | `reason`: `no_date`, `invalid_sha`, `no_url`, `duplicate` | 09 commits/badges |  | commits rejected by validation |
| `scp_commits_clamped_total` | counter |  | 09 commits/badges |  | future-dated commits clamped |
| `scp_commits_evicted_total` | counter | `reason`: `aged_out`, `rewritten` | 09 commits/badges |  | commits that left a ring |
| `scp_commits_persist_errors_total` | counter |  | 09 commits/badges |  | store writes of the job that failed (served from memory anyway) |
| `scp_commits_restore_total` | counter | `result`: `ok`, `invalid`, `incomplete` | 09 commits/badges |  | warm-start outcome per repository |
| `scp_commits_reactions_total` | counter | `result`: `enqueued`, `not_default_branch`, `unknown_repo` | 09 commits/badges |  | webhook / head-sweep reactions |
| `scp_commits_merge_seconds` | histogram | `path`: `fast`, `merge` | 09 commits/badges |  | duration of one feed page |
| `scp_commits_retained` | gauge (observable) |  | 09 commits/badges |  | commits held in the current snapshot |
| `scp_badges_total` | counter | `kind` (closed set plus `invalid`), `state`: `ok`, `none`, `unknown`, `not_found`, `unavailable` | 09 commits/badges |  | badge responses |
| `scp_badges_conditional_total` | counter | `result`: `200`, `304` | 09 commits/badges |  | conditional-request effectiveness |

### docs, media, pages

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_docs_refresh_total` | counter | `result`: `published`, `unchanged_tree`, `unchanged_fingerprint`, `continuation`, `deferred`, `gone`, `error` | 10 docs/pages/media |  | one increment per `docs.refresh` run |
| `scp_docs_fetch_total` | counter | `kind`: `tree`, `readme`, `file`, `rendered`, `descriptor`; `result`: `ok`, `not_modified`, `not_found`, `permanent`, `error` | 10 docs/pages/media |  | GitHub reads this unit causes (the adapter counts HTTP calls; this counts intent) |
| `scp_docs_render_seconds` | histogram | `format`: `markdown`, `asciidoc`, `text` | 10 docs/pages/media |  | per-document render time, buckets 1 ms .. 10 s |
| `scp_docs_render_total` | counter | `result`: `ok`, `text_fallback`, `too_large`, `error` | 10 docs/pages/media |  | outcomes; `text_fallback` is the typed-error path |
| `scp_docs_resident_bytes` | gauge (observable) | none | 10 docs/pages/media |  | the admission counter; compare with `docs.resident_budget` |
| `scp_docs_not_resident` | gauge (observable) | `reason`: `budget`, `count_cap`, `pending` | 10 docs/pages/media |  | documents indexed without a body, summed over the snapshot |
| `scp_docs_restore_total` | counter | `result`: `ok`, `invalid`, `pipeline_stale` | 10 docs/pages/media |  | warm-start rows |
| `scp_docs_reactions_total` | counter | `result`: `enqueued`, `ignored_branch`, `ignored_paths`, `unknown_repo`, `no_hint` | 10 docs/pages/media |  | webhook and head-change reactions |
| `scp_docs_raw_requests_total` | counter | `result`: `ok`, `not_modified`, `not_resident`, `not_markdown`, `not_available`, `too_large`, `not_found`, `redirect` | 10 docs/pages/media |  | raw and negotiated Markdown requests |
| `scp_docs_descriptor_problems_total` | counter | `code` (closed set of 4.3 codes), `severity` | 10 docs/pages/media |  | problems found while loading descriptors (increments on change only, not on every unchanged run) |
| `scp_media_fetch_total` | counter | `outcome`: `ok`, `bad_url`, `denied_host`, `redirect_denied`, `not_image`, `too_large`, `svg_denied`, `upstream_status`, `timeout`, `transient`, `b... | 10 docs/pages/media |  | external fetches |
| `scp_media_fetch_seconds` | histogram | `outcome` | 10 docs/pages/media |  | origin fetch duration |
| `scp_media_cache_total` | counter | `result`: `hit`, `miss`, `shared`, `stale`, `negative` | 10 docs/pages/media |  | cache behaviour of `/media/ext` and repository images |
| `scp_media_cache_bytes` | gauge (observable) | none | 10 docs/pages/media |  | bytes resident in the LRU |
| `scp_media_repo_total` | counter | `result`: `ok`, `not_modified`, `bad_ref`, `bad_path`, `not_found`, `not_image`, `upstream_error` | 10 docs/pages/media |  | repository image requests |
| `scp_media_signature_total` | counter | `result`: `ok`, `bad` | 10 docs/pages/media |  | signature verification (a spike of `bad` is a probing signal or a key rotation without `previous_keys`) |
| `scp_pages_reload_total` | counter | `result`: `ok`, `unchanged`, `error` | 10 docs/pages/media |  | reload attempts |
| `scp_pages` | gauge (observable) | none | 10 docs/pages/media |  | pages currently served |
| `scp_pages_problems` | gauge (observable) | none | 10 docs/pages/media |  | problems of the current set |

### manifests

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_manifests_parse_total` | counter | `ecosystem parser outcome` (`ok failed limit abandoned panic`) | 11 manifests | 12 | parse attempts |
| `scp_manifests_parse_seconds` | histogram | `ecosystem` | 11 manifests | 12 | parse duration |
| `scp_manifests_limit_hits_total` | counter | `limit` (`size depth nodes expand timeout tree-calls` and parser-defined `limit.*` suffixes) | 11 manifests | 12 | limits that were hit |
| `scp_manifests_abandoned_total` | counter | `parser` | 11 manifests |  | parser goroutines abandoned by the watchdog |
| `scp_manifests_refresh_total` | counter | `outcome` (`changed unchanged failed gone`) | 11 manifests |  | `manifests.refresh` results |
| `scp_manifests_files_total` | counter | `result` (`parse_cache blob_cache fetched failed skipped`) | 11 manifests |  | candidate files by how they were served |
| `scp_manifests_fetch_total` | counter | `mode` (`batch rest sha_mismatch`) | 11 manifests |  | blob reads |
| `scp_manifests_tree_calls_total` | counter | `mode` (`recursive walk`) | 11 manifests |  | tree reads |
| `scp_manifests_reactor_total` | counter | `event result` (`enqueued_full enqueued_paths enqueued_head enqueued_create enqueued_repo dropped ignored`) | 11 manifests |  | reactor decisions |
| `scp_manifests_warm_total` | counter | `result` (`ok no_payload undecodable`) | 11 manifests |  | rows seen by the warm start |
| `scp_manifests_locked_cache_total` | counter | `result` (`hit miss miss_suppressed corrupt`) | 11 manifests |  | API lookups of locked components (`miss_suppressed`: a forced re-parse of that repository was already reque... |
| `scp_manifests_diagnostics_total` | counter | `severity namespace` | 11 manifests | 12 | diagnostics emitted by refresh jobs |
| `scp_manifests_facts_payload_bytes` | histogram |  | 11 manifests |  | persisted payload size |
| `scp_manifests_graph_rebuild_seconds` | histogram |  | 11 manifests |  | graph build time |
| `scp_manifests_graph_publish_total` | counter | `result` (`published unchanged`) | 11 manifests |  | graph job outcomes |
| `scp_manifests_units` | gauge | `ecosystem` | 11 manifests | 12 | units in the snapshot |
| `scp_manifests_stale_repos` | gauge |  | 11 manifests |  | repositories whose facts derive stale |
| `scp_manifests_graph_units` | gauge |  | 11 manifests |  | size of the current graph |
| `scp_manifests_graph_dangling_units` | gauge |  | 11 manifests |  | graph units whose repository left the snapshot |
| `scp_manifests_parse_inflight_bytes` | gauge |  | 11 manifests |  | memory weight held by running (and abandoned) parses |
| `scp_manifests_circuit_open` | gauge | `parser` | 11 manifests |  | 1 = parser disabled until restart |

### manifests providers

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_manifests_provider_requests_total` | counter | `provider` (`githubgraph\|sbom\|depsdev`), `outcome` (`ok\|capped\|not_found\|forbidden\|rate_limited\|timeout\|error\|schema`) | 13 eco B |  | outbound operations (a GraphQL page, an SBOM generate/fetch, a deps.dev call) |
| `scp_manifests_provider_request_seconds` | histogram | `provider` | 13 eco B |  | latency per operation (buckets 0.05..60 s) |
| `scp_manifests_provider_facts_total` | counter | `provider`, `kind` (`transitive\|licence\|latest\|deprecated`), `result` (`applied\|shadowed\|conflict`) | 13 eco B |  | facts offered to `Merge` and what happened |
| `scp_manifests_provider_skipped_total` | counter | `provider`, `reason` (`private_coordinate\|org_produced\|unsupported_ecosystem\|private_repo\|custom_registry\|invalid_coordinate\|capped\|disabled`) | 13 eco B |  | coordinates or units not sent |
| `scp_manifests_provider_cache_total` | counter | `provider`, `result` (`hit\|miss\|negative\|stale`) | 13 eco B |  | deps.dev cache behaviour |
| `scp_manifests_sbom_polls_total` | counter | `result` (`pending\|ready\|timeout\|failed`) | 13 eco B |  | SBOM report polling |
| `scp_manifests_sbom_report_bytes` | histogram | none | 13 eco B |  | size of decoded reports |
| `scp_manifests_provider_breaker_state` | gauge | `provider` | 13 eco B |  | 0 closed, 1 half-open, 2 open |

### integrations and access

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_integrations_enabled` | gauge | `provider` | 14 integrations/access |  | 1 when configured, 0 otherwise (recorded once at construction): "disabled" is visible, never faked as ok (D... |
| `scp_integrations_status` | gauge | `provider`, `state` (`unknown`,`ok`,`failing`,`unstable`) | 14 integrations/access |  | repositories per state after the last sweep |
| `scp_integrations_targets` | gauge | `provider`, `result` (`resolved`,`invalid`,`no_data`) | 14 integrations/access |  | resolved targets, invalid descriptors/groups, targets Snyk does not monitor |
| `scp_integrations_fetch_total` | counter | `provider`, `outcome` (`ok`, or an `ErrCode`: `not_found`,`no_data`,`auth`,`config`,`rate_limited`,`protocol`,`too_large`,`aborted`,`timeout`,`tran... | 14 integrations/access |  | per-target (Jenkins) or per-listing result (Snyk bulk counts every target) |
| `scp_integrations_fetch_seconds` | histogram | `provider` | 14 integrations/access |  | latency of one provider call (one Jenkins request or one Snyk listing) |
| `scp_integrations_sweep_seconds` | histogram | `provider` | 14 integrations/access |  | duration of one sweep job body |
| `scp_integrations_last_success_timestamp_seconds` | gauge | `provider` | 14 integrations/access |  | Unix time of the last sweep without a sweep-level error; alert on `time() - x > 3 * ttl` |
| `scp_job_` | ? | `kind=integrations.jenkins\|integrations.snyk` | 14 integrations/access |  | duration, outcome (`ratelimited`, `parked`, `retried`) of the sweep keys |
| `scp_identity_header_spoof_total` | counter | `reason` (`untrusted_peer`,`bad_secret`,`duplicate`,`malformed`,`mode_public`) | 14 integrations/access |  | requests whose identity headers were dropped (the architecture names the counter; the `reason` label is add... |
| `scp_access_requests_total` | counter | `principal` (`anonymous`,`authenticated`), `class` (`public`,`all`) | 14 integrations/access |  | public-listener requests by principal kind and view class |

### Added rows for names that two units wrote as one row (the generator reads the first name of a row)

| Name | Instrument | Labels | Owner | Also described by | Meaning |
|---|---|---|---|---|---|
| `scp_store_wal_bytes` | observable gauge (By) | none | 03 store | 15 | WAL file size (stat at scrape) |
| `scp_manifests_graph_edges` | gauge | none | 11 manifests | | edges of the current graph |
| `scp_manifests_graph_cycles` | gauge | none | 11 manifests | | cycles in the current graph |

### Supplied by libraries (no unit table owns them)

| Name | Instrument | Labels | Owner | Meaning |
|---|---|---|---|---|
| `http.server.request.duration` and the other `http.server.*` instruments | histogram | `http.route` (the mux pattern, bounded by the route table), `http.request.method`, `http.response.status_code` | otelhttp (02 `httpx` chain, architecture 18.2) | request latency by route pattern; the exporter publishes `http_server_request_duration_seconds` |
| `go_*`, `process_*`, `runtime` metrics | various | none | OTel runtime instrumentation (`telemetry.runtime.enabled`) | Go runtime and process health |

## 5. Alerts that depend on a name changed by this review

`scp_lifecycle_phase_seconds{outcome="timeout"}` (new value) replaces "stop of any stage exceeds 80 percent of its cap" in 16's table as the primary alert; `scp_item_age_seconds` is emitted by `ops` only (15 section 9.4 already assumes this).
