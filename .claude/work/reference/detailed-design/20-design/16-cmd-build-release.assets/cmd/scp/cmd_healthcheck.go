package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// cmdHealthcheck is the probe a container HEALTHCHECK can run in an image that has no shell, curl or wget (deviation D-5; the
// Kubernetes manifests use httpGet probes and never call it). It dials loopback only, so it cannot be turned into a fetcher.
func cmdHealthcheck(ctx context.Context, env Env, args []string) int {
	fl := newFlagSet(env, "healthcheck")
	target := fl.String("url", "http://127.0.0.1:9090/livez", "URL to probe; the host must be a loopback address")
	timeout := fl.Duration("timeout", 3*time.Second, "request deadline")
	if code, done := parseFlags(fl, args); done {
		return code
	}
	u, err := url.Parse(*target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isLoopbackHost(u.Hostname()) || fl.NArg() != 0 || *timeout <= 0 {
		_, _ = fmt.Fprintln(env.Stderr, "scp healthcheck: --url must be an http(s) URL on a loopback address (127.0.0.1 or [::1])")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return exitUsage
	}
	// A dedicated client: no redirects (a redirect would leave loopback), no proxy, no keep-alive.
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	resp, err := c.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "scp healthcheck: %v\n", err)
		return exitFail
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = fmt.Fprintf(env.Stderr, "scp healthcheck: %s answered %d\n", u.Redacted(), resp.StatusCode)
		return exitFail
	}
	return exitOK
}
