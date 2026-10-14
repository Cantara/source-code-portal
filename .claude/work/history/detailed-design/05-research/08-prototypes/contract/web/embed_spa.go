//go:build spa

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the SPA root (index.html at the top level).
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Embedded reports whether the real SPA is compiled in.
const Embedded = true
