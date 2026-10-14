// Package api embeds the /api/v1 contract so the binary can serve it.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
