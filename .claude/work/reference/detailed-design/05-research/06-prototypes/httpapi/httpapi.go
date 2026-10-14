package httpapi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// ---- RFC 9457 problem details ----

type Problem struct {
	Type              string       `json:"type,omitempty"` // "about:blank" when omitted
	Title             string       `json:"title"`
	Status            int          `json:"status"`
	Detail            string       `json:"detail,omitempty"`
	Instance          string       `json:"instance,omitempty"`
	Code              string       `json:"code,omitempty"`   // extension member: stable machine code
	Errors            []FieldError `json:"errors,omitempty"` // extension member
	RetryAfterSeconds int          `json:"retryAfterSeconds,omitempty"`
}
type FieldError struct{ Pointer, Detail string }

func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// ---- ETag / conditional GET ----

// NotModified reports (and writes 304) when If-None-Match matches etag (weak comparison, RFC 9110 13.1.2).
func NotModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache") // store, but always revalidate
	inm := r.Header.Get("If-None-Match")
	if inm == "" {
		return false
	}
	for _, c := range strings.Split(inm, ",") {
		c = strings.TrimSpace(c)
		if c == "*" || strings.TrimPrefix(c, "W/") == strings.TrimPrefix(etag, "W/") {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

func ETagFor(version uint64, variant string) string {
	return fmt.Sprintf(`"%s-%d"`, variant, version) // opaque; derived from the snapshot generation, not the body
}

// ---- trusted-proxy client IP: rightmost-untrusted algorithm over X-Forwarded-For ----

type ClientIP struct{ trusted []netip.Prefix }

func NewClientIP(cidrs ...string) (*ClientIP, error) {
	c := &ClientIP{}
	for _, s := range cidrs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		c.trusted = append(c.trusted, p)
	}
	return c, nil
}
func (c *ClientIP) isTrusted(a netip.Addr) bool {
	for _, p := range c.trusted {
		if p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

// From returns the client IP and whether forwarded headers were honoured.
func (c *ClientIP) From(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if !c.isTrusted(peer) { // direct connection or untrusted peer: ignore every forwarded header
		return peer, false
	}
	// Join ALL X-Forwarded-For header lines, walk right to left, skip trusted proxies.
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(line, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer, true // garbage from a trusted proxy: fall back to the proxy itself (fail closed for per-client limits)
		}
		a = a.Unmap()
		if !c.isTrusted(a) {
			return a, true
		}
	}
	return peer, true
}

// RateKey buckets IPv6 by /64 so a client cannot rotate inside its own prefix.
func RateKey(a netip.Addr) string {
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}

// ---- SSE ----

type Broker interface {
	Subscribe() (<-chan []byte, func())
}

func SSE(b Broker, shutdown <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		// A server-wide WriteTimeout would kill the stream: lift it for this response only.
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			WriteProblem(w, r, Problem{Status: 500, Code: "sse_unsupported"})
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache, no-transform")
		h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
		w.WriteHeader(200)
		fmt.Fprint(w, "retry: 5000\n\n") // client reconnect delay
		if rc.Flush() != nil {
			return
		}
		ch, cancel := b.Subscribe()
		defer cancel()
		hb := time.NewTicker(20 * time.Second) // < proxy idle timeouts (nginx proxy_read_timeout default 60s)
		defer hb.Stop()
		var id uint64
		for {
			select {
			case <-r.Context().Done():
				return
			case <-shutdown: // srv.RegisterOnShutdown(close) so Shutdown does not wait forever
				return
			case msg := <-ch:
				id++
				fmt.Fprintf(w, "id: %s\nevent: invalidate\ndata: %s\n\n", strconv.FormatUint(id, 10), msg)
			case <-hb.C:
				fmt.Fprint(w, ": hb\n\n")
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}
