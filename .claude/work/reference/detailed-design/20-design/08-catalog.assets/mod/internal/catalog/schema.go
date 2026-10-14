package catalog

import _ "embed"

// Schema is the JSON Schema (2020-12) of the `catalog:` block and of the catalog-owned discovery keys. cmd/scp grafts it into the
// generated root schema (`scp config schema`); a cmd/scp test checks that every yaml tag of Config, GroupConfig, HeadsConfig,
// GroupIntegrations and DiscoveryPolicy appears in it and vice versa.
//
//go:embed catalog.schema.json
var Schema []byte
