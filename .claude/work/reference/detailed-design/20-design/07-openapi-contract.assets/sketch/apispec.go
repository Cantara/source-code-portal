// Package apispec is a compile-check stand-in for the rank-0 package that embeds the two bundled documents
// (architecture 4.3, ADR-020): the bundles are produced by redocly and committed, so go test needs no Node.
package apispec

import _ "embed"

//go:embed openapi.bundled.yaml
var public []byte

//go:embed openapi.admin.bundled.yaml
var admin []byte

// Public returns the bundled public document; the caller must not modify the slice.
func Public() []byte { return public }

// Admin returns the bundled admin document (a superset of the public one).
func Admin() []byte { return admin }
