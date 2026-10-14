//go:build spa

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// EmbeddedMode is ModeSPA in this build.
const EmbeddedMode = ModeSPA

// FS returns the embedded bundle with index.html at its root.
func FS() (fs.FS, error) { return fs.Sub(embedded, "dist") }
