//go:build spa

package web

import (
	"embed"
	"io/fs"
)

// all: also embeds names starting with "." or "_" (dotfiles, _redirects); .br and .gz siblings are included.
//
//go:embed all:dist
var embedded embed.FS

// EmbeddedMode is ModeSPA in this build.
const EmbeddedMode = ModeSPA

// FS returns the embedded bundle with index.html at its root.
func FS() (fs.FS, error) { return fs.Sub(embedded, "dist") }
