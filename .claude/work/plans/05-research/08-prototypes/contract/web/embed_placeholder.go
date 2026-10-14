//go:build !spa

// Package web exposes the built single-page application as an fs.FS.
// Default (no tags): a tiny placeholder so `go test ./...` works without Node.
package web

import (
	"embed"
	"io/fs"
)

//go:embed placeholder
var placeholder embed.FS

// Dist returns the SPA root (index.html at the top level).
func Dist() fs.FS {
	sub, err := fs.Sub(placeholder, "placeholder")
	if err != nil {
		panic(err) // embed layout is static; cannot fail at runtime
	}
	return sub
}

// Embedded reports whether the real SPA is compiled in.
const Embedded = false
