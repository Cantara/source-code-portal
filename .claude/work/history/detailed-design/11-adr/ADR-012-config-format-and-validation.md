# ADR-012: Configuration format and validation

Status: Accepted. Date: 2026-10-14. Refines 04 D-07 (A-07).

## Context
Two configuration systems, string-keyed lookups, silent defaults, keys without consumers and secrets held as plain strings each cause misconfiguration that is discovered only in production, so configuration must be one typed, strict, validated tree with secrets as a type (GR-12, GR-13). D-07 asks for one typed, versioned YAML with env overrides, `*_FILE` secrets, strict decoding, validation, generated reference and schema, and a documented-equals-consumed test.

## Decision
- YAML via `go.yaml.in/yaml/v3` with `KnownFields(true)` (the `gopkg.in/yaml.v3` repository is archived); layering: embedded default < `--config` file < `SCP_` environment (mapping `SCP_<PATH>`; list indices; no dots) < flags; secrets from `*_FILE` or `{file:}` blocks into `secret.Secret`; `caarlos0/env/v11` for the env layer.
- Root `Config` in `cmd/scp/config.go`, composed from feature-owned `Config` types so keys live with the code that reads them; `platform/config` holds only machinery.
- `Validate()` collects all errors with JSON-pointer-like paths; startup refuses: private visibility without an auth mode, a webhook route without a secret, trusted proxies of `0.0.0.0/0`, duplicate slugs, unknown keys, URLs failing the SSRF policy, private integration addresses without an explicit allow entry.
- `invopop/jsonschema` (`FieldNameTag: yaml`) generates `scp config schema`; `santhosh-tekuri/jsonschema/v6` validates in tests; the descriptor schema is hand-written (it needs `oneOf` navigation).
- Tests: documented keys == consumed keys; every shipped example passes `scp config lint`.
- `version: 1` at the top; durations are `time.Duration` strings.

## Alternatives considered
koanf/viper (stringly-typed lookups, silent defaults), TOML/JSON config, a generic tree-walker (stringly-typed, GR-12).

## Consequences
A new key requires the owner's struct field, a doc string and a consumer, or CI fails. Environment names are long but standard.

## Evidence
`05-research/02-go-stack-currency.md` sections 3.4-3.5, 7; `05-research/09-github-app-integration.md` section 7.4; `02-design-guardrails.md` GR-12, GR-13, GR-14.

## Amendment 2026-10-14 (review revision)

`site:` (title, description, nav, display time zone, default theme, sitemap, robots) and group-level `integrations` defaults are config keys with consumers (`api.site`, the integrations target merge); `webhook.dedupe_ttl` is 15 min (the in-memory LRU TTL; the 168 h value is removed); `store.cache_caps.locked`, `docs.max_resident_per_repo`, `manifests.pom_source.private_group_prefixes`, Snyk `org_id`/`base_url`, `ratelimit.csp_reports` are added. `cmd/scp` imports `time/tzdata` because the distroless static image has no zone database and `site.display_time_zone` is validated with `time.LoadLocation`.
