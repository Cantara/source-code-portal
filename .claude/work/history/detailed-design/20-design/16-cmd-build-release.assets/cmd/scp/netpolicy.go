//go:build !devtools

package main

// extraAllowedCIDRs is the development-build hook of newGitHubHTTP. In every build without the
// `devtools` tag (the release binary, the image, `go test ./...`) they are empty: loopback stays denied by netguard.
func extraAllowedCIDRs() []string { return nil }
