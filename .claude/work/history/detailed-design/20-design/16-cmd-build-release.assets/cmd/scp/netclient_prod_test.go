//go:build !devtools

package main

import (
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/github"
)

// Without the devtools tag a loopback fake GitHub is unreachable, with or without allow_private_addresses (netguard: private
// ranges never include loopback). This is the reason `make dev` builds with -tags devtools.
func TestLoopbackRefusedInProductionBuild(t *testing.T) {
	srv := fakeGitHub(t)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, private := range []bool{false, true} {
		c, err := newGitHubHTTP(github.Network{Source: "dev", Hosts: []string{host}, AllowInsecureHTTP: true, AllowPrivateAddresses: private})
		if err != nil {
			t.Fatal(err)
		}
		if resp, err := getURL(c, srv.URL); err == nil {
			resp.Body.Close()
			t.Fatalf("allow_private_addresses=%v: loopback must be refused", private)
		}
	}
}
