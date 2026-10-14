//go:build devtools

package main

// Development build only (`make dev` runs `go run -tags devtools`): the fake GitHub listens on 127.0.0.1:9191 and the SSRF
// policy would otherwise refuse it. Loopback only, and only the port the source pins (newGitHubHTTP opens a pinned host:port
// and nothing else); metadata addresses stay denied by netguard. The release workflow asserts the shipped binary was not
// built with this tag.
func extraAllowedCIDRs() []string { return []string{"127.0.0.0/8"} }
