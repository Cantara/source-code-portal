package main

import (
	"context"
	"net"
	"net/http"
)

// listenTCP is net.Listen with a context (noctx): the tests bind loopback ports and the process owns no deadline for it.
func listenTCP(addr string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
}

// getURL is a GET with a context; a nil client means the default one (tests only; the product never uses it, G4).
func getURL(c *http.Client, url string) (*http.Response, error) {
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}
