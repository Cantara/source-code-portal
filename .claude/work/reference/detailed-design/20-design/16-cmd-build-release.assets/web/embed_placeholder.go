//go:build !spa

package web

import (
	"embed"
	"io/fs"
)

//go:embed placeholder
var embedded embed.FS

// EmbeddedMode is ModePlaceholder in this build.
const EmbeddedMode = ModePlaceholder

// FS returns the embedded bundle with index.html at its root.
func FS() (fs.FS, error) { return fs.Sub(embedded, "placeholder") }
