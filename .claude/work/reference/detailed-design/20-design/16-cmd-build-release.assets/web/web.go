// Package web exposes the embedded single-page application. It is rank 0, stdlib only, and imported by cmd/scp only.
// Which files are embedded is chosen by the build tag `spa`: with it, web/dist (built by `pnpm build`);
// without it, the committed placeholder, so `go test ./...` needs no Node.
package web

// Mode says which bundle is embedded.
type Mode string

const (
	ModePlaceholder Mode = "placeholder"
	ModeSPA         Mode = "spa"
)
