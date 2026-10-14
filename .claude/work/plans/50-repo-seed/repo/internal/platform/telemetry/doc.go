// Package telemetry wires the OpenTelemetry SDK, the Prometheus exporter and OTLP. It is the only package allowed to import the
// OTel SDK.
//
// Architecture: rank 0 (internal/platform/telemetry); the closed-world row is in internal/archtest/rules.go and is the only
// authority.
// Allowed internal imports: none.
// Third-party libraries owned here: OTel SDK, Prometheus exporter (owner).
//
// Design: docs/design/20-design/02-platform.md; architecture sections 4 and 5.2.
package telemetry
