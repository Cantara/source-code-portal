# ADR-013: Observability: slog JSON, OpenTelemetry, Prometheus on the admin listener

Status: Accepted. Date: 2026-10-14. Refines 04 D-08 (A-08).

## Context
D-08 asks for `slog` JSON, OpenTelemetry metrics and traces, Prometheus on the admin listener, `/livez`/`/readyz` public, `/status`/`/metrics`/pprof admin only. OpenTelemetry Go logs became stable on 2026-10-02 (v1.47.0); the Prometheus exporter, `otelhttp` and `otelslog` remain 0.x modules.

## Decision
- Logging: `slog` JSON from `cmd/scp`, injected (no global logger, `sloglint`); fields: request id (`crypto/rand.Text`), trace id, delivery id, job key/kind, source, installation, repo slug, upstream, status, duration, error code, snapshot version.
- Metrics and traces: OpenTelemetry SDK confined to `platform/telemetry` (G6); instruments and spans are created through the OTel **API** modules (`otel/metric`, `otel/trace`, verified as separate modules from the SDK, v1.47.0) from a `Meter` and `Tracer` injected through each package's `Deps` by `cmd/scp`; the root `otel` package and its global accessors are forbidden outside `cmd/scp` and `platform/telemetry` (G11); Prometheus exporter on the admin listener; optional OTLP; otelhttp for server and for the GitHub client transport; Go runtime metrics through the OTel runtime instrumentation.
- Metric names, span boundaries and log fields are specified in the architecture (section 18); health derives from real outcomes (item age, last success, queue depth, rate state), never from thread existence; probes make no outbound calls.
- `/status` aggregates readiness gates, store health, snapshot version/age, tasks, parked keys, upstream pause, installations (rate remaining/reset, token expiry), descriptor problems and manifests diagnostics.

## Alternatives considered
Prometheus client only (no traces), zap/zerolog (non-stdlib), OTLP-only (no scrape target for simple deployments).

## Consequences
Three 0.x OTel contrib modules are pinned and bumped by Renovate; metric cardinality is bounded by route names and kind enums, never raw paths or slugs except in the per-installation gauges.

## Evidence
`05-research/02-go-stack-currency.md` section 3.6; `05-research/05-store-sync-engine.md` section 5.13.

## Amendment 2026-10-14 (review revision)

The original text allowed features to create instruments but the dependency matrix gave them no OTel import and no injected meter, so every implementation would have been rejected by `archtest` or would have reintroduced a global (F-03). The matrix now lists the API modules for every package that emits telemetry; `logx` imports `otel/trace` to read the span context for `trace_id`. Metric names are constants next to their instruments and a `cmd/scp` test compares the emitted names with the generated `docs/reference/metrics.md`. Access logs and `http.route` use `r.Pattern` (bounded), not route names (F-23).
