// Package web holds the single-page application and embeds its build. The build tag `spa` embeds web/dist (from `pnpm build`);
// without it a committed placeholder page is embedded, so `go build` and `go test ./...` need only Go.
package web

//go:generate go run ../internal/docs/chromacss src/styles/chroma.css

// Mode says which bundle is embedded.
type Mode string

const (
	ModePlaceholder Mode = "placeholder"
	ModeSPA         Mode = "spa"
)
