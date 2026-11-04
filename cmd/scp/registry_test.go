package main

import (
	"net/http"
	"net/http/httptest"
	gosync "sync"
	"testing"
)

// fakeRegistry is every package registry in the tests, answered in memory by host and path (404 for anything not set): a
// transport that never dials, so no build file in a fixture can make a test reach the network, and fake time in a synctest bubble
// is never held by a socket.
type fakeRegistry struct {
	mu      gosync.Mutex
	answers map[string]string // "host/path" (escaped) -> body
}

func newFakeRegistry(testing.TB) *fakeRegistry { return &fakeRegistry{answers: map[string]string{}} }

// set answers host and path (escaped) with body.
func (f *fakeRegistry) set(host, path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[host+path] = body
}

// RoundTrip answers req from the answers set.
func (f *fakeRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	body, ok := f.answers[req.URL.Host+req.URL.EscapedPath()]
	f.mu.Unlock()
	rec := httptest.NewRecorder()
	if ok {
		_, _ = rec.WriteString(body)
	} else {
		rec.WriteHeader(http.StatusNotFound)
	}
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

// testDeps are the outside services of a test server: the fake GitHub's transport stays the default (its URL is configured), the
// package registries are a fake one.
func testDeps(t testing.TB) deps { return deps{registries: newFakeRegistry(t)} }
