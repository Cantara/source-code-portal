// Package analytics is the optional Plausible Analytics of the pages (design/analytics.md, P-113). The tracker is part of the portal's
// own scripts and posts each event to an address of the portal itself, which forwards it to Plausible's endpoint (its own service, or
// a self-hosted one) with the visitor's address and browser and nothing else: the browser never talks to another host, the policy of
// the pages is unchanged, and no filter list has a third-party script or address to block.
package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// Path is where the tracker posts its events: a name no filter list matches (it has none of plausible, analytics, stats, track
	// or event), the same in every portal.
	Path = "/_v/m"
	// DefaultEndpoint is where Plausible's own service takes events.
	DefaultEndpoint = "https://plausible.io/api/event"

	maxBody    = 8 << 10
	maxAgent   = 512
	inFlight   = 16
	sendBudget = 5 * time.Second
)

// site is a site as Plausible knows it: a host name, optionally with a path.
var site = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(/[a-z0-9._~/-]*)?$`)

// Validate lists what is wrong with web.plausible_domain and web.plausible_endpoint.
func Validate(domain, endpoint string) []string {
	if domain == "" {
		if endpoint != "" {
			return []string{"web.plausible_endpoint: needs web.plausible_domain"}
		}
		return nil
	}
	var p []string
	if !site.MatchString(domain) {
		p = append(p, fmt.Sprintf("web.plausible_domain: %q is not a site's domain as Plausible knows it (example.org)", domain))
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		loopback := err == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
		switch {
		case err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "":
			p = append(p, fmt.Sprintf("web.plausible_endpoint: %q is not an absolute URL without credentials, query or fragment", endpoint))
		case u.Scheme != "https" && (u.Scheme != "http" || !loopback):
			p = append(p, "web.plausible_endpoint: must be https (http only on loopback)")
		}
	}
	return p
}

// Env is what the web UI server is told (SCP_ANALYTICS): the site and the portal's address for events as JSON; empty without a site.
func Env(domain string) string {
	if domain == "" {
		return ""
	}
	b, _ := json.Marshal(struct {
		Domain string `json:"domain"`
		Path   string `json:"path"`
	}{domain, Path})
	return string(b)
}

// Relay forwards the tracker's events to Plausible's endpoint.
type Relay struct {
	endpoint string
	client   *http.Client
	ip       func(*http.Request) string
	slots    chan struct{}
}

// New returns the relay to endpoint (empty: DefaultEndpoint). ip tells a request's client address (behind a trusted proxy, the one
// the proxy saw); rt is the transport to the endpoint (nil: the default).
func New(endpoint string, ip func(*http.Request) string, rt http.RoundTripper) *Relay {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Relay{endpoint: endpoint, ip: ip, slots: make(chan struct{}, inFlight), client: &http.Client{Timeout: sendBudget, Transport: rt,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Handler serves POST Path. It builds the request to the endpoint afresh: the visitor's browser and address (Plausible counts a
// visitor by them, and drops an event without the address as a bot) and the media type, and nothing else, so no cookie, token, referrer
// or origin of the portal goes to a third party. A request another site made (Sec-Fetch-Site: cross-site), one without a body or with
// too large a one, and one past the limit in flight are refused; the answer never tells what the endpoint said beyond whether it took
// the event.
func (r *Relay) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if req.Header.Get("Sec-Fetch-Site") == "cross-site" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
		if err != nil || len(body) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case r.slots <- struct{}{}:
			defer func() { <-r.slots }()
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.send(req.Context(), req, body) {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})
}

func (r *Relay) send(ctx context.Context, from *http.Request, body []byte) bool {
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return false
	}
	if ua := from.UserAgent(); ua != "" {
		out.Header.Set("User-Agent", ua[:min(len(ua), maxAgent)])
	}
	if ip := r.ip(from); ip != "" {
		out.Header.Set("X-Forwarded-For", ip)
	}
	out.Header.Set("Content-Type", "text/plain")
	resp, err := r.client.Do(out)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	return resp.StatusCode/100 == 2
}
