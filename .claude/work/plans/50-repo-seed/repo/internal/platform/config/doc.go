// Package config is the strict layered configuration machinery: embedded default, file, SCP_ environment, *_FILE, flags; JSON
// Schema generation; lint.
//
// Architecture: rank 1 (internal/platform/config); the closed-world row is in internal/archtest/rules.go and is the only authority.
// Allowed internal imports: internal/platform/secret.
// Third-party libraries owned here: go.yaml.in/yaml/v3, caarlos0/env/v11, invopop/jsonschema, santhosh-tekuri/jsonschema/v6.
//
// Design: docs/design/20-design/02-platform.md; architecture sections 4 and 5.2.
package config
