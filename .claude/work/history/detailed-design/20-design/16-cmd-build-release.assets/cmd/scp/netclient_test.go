package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cantara/source-code-portal/internal/github"
)

func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
}

func TestMetadataRefusedAlways(t *testing.T) {
	c, err := newGitHubHTTP(github.Network{Source: "x", Hosts: []string{"169.254.169.254"}, AllowInsecureHTTP: true, AllowPrivateAddresses: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := getURL(c, "http://169.254.169.254/latest/meta-data/"); err == nil {
		resp.Body.Close()
		t.Fatal("the metadata address must be refused")
	}
}

func TestFactoryRefusesBadInput(t *testing.T) {
	for name, n := range map[string]github.Network{
		"proxy": {Source: "x", ProxyURL: "://bad"},
		"ca":    {Source: "x", CAFile: "/definitely/not/here.pem"},
	} {
		if _, err := newGitHubHTTP(n); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
