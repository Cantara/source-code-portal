//go:build devtools

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/github"
)

// Run in CI as `go test -tags devtools -run TestDev ./cmd/scp`: the development build reaches the fake on its port and nothing else.
func TestDevBuildReachesTheFakeOnly(t *testing.T) {
	if got := extraAllowedCIDRs(); len(got) != 1 || got[0] != "127.0.0.0/8" {
		t.Fatalf("cidrs = %v", got)
	}
	srv := fakeGitHub(t) // an httptest port is not 9191: refused even in a dev build
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	c, err := newGitHubHTTP(github.Network{Source: "dev", Hosts: []string{host}, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	// The pinned "host:port" opens exactly its own port, so this works too: the allowance is the pinned port, not a range.
	resp, err := getURL(c, srv.URL)
	if err != nil {
		t.Fatalf("a pinned loopback host:port is reachable in a dev build: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
	if _, err := getURL(c, "http://169.254.169.254/"); err == nil {
		t.Fatal("metadata stays denied")
	}
}
