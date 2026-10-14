package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrusted parses trusted proxy CIDRs (or bare addresses). It refuses a
// prefix that would trust the whole internet.
func ParseTrusted(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				return nil, fmt.Errorf("%q is not a CIDR or address", s)
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		p = p.Masked()
		if p.Bits() == 0 {
			return nil, fmt.Errorf("%q trusts every address; list the proxies explicitly", s)
		}
		out = append(out, p)
	}
	return out, nil
}

// ClientIPResolver implements the one client-IP rule used everywhere: the TCP
// peer, unless the peer is a trusted proxy, in which case X-Forwarded-For is
// walked right to left (all header lines merged) skipping trusted hops.
type ClientIPResolver struct{ trusted []netip.Prefix }

func NewClientIPResolver(trusted []netip.Prefix) *ClientIPResolver {
	return &ClientIPResolver{trusted: trusted}
}

// Trusted reports whether addr is inside a trusted proxy range.
func (c *ClientIPResolver) Trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range c.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Peer parses r.RemoteAddr. The zero Addr means it was unparsable (for example
// a unix socket in tests).
func Peer(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap().WithZone("")
}

// Resolve returns the client address and whether the request came through a
// trusted proxy. It never returns an error: an unparsable hop ends the walk and
// the nearest parsed address wins (fail towards the proxy, not towards a value
// the client chose).
func (c *ClientIPResolver) Resolve(r *http.Request) (ip netip.Addr, viaProxy bool) {
	peer := Peer(r)
	if !peer.IsValid() || !c.Trusted(peer) {
		return peer, false
	}
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for _, h := range strings.Split(line, ",") {
			hops = append(hops, strings.TrimSpace(h))
		}
	}
	cur := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(hops[i])
		if err != nil {
			return cur, true
		}
		a = a.Unmap().WithZone("")
		cur = a
		if !c.Trusted(a) {
			return a, true
		}
	}
	return cur, true
}

// ClientIP is Resolve reduced to the string form consumers want (webhook's
// invalid-signature limiter, logs): "" when the peer is unparsable.
func (c *ClientIPResolver) ClientIP(r *http.Request) string {
	if ip, _ := c.Resolve(r); ip.IsValid() {
		return ip.String()
	}
	return ""
}

// HTTPS reports whether the request reached the proxy over TLS. It trusts
// X-Forwarded-Proto only from a trusted peer.
func (c *ClientIPResolver) HTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if p := Peer(r); p.IsValid() && c.Trusted(p) {
		for _, v := range r.Header.Values("X-Forwarded-Proto") {
			if strings.EqualFold(strings.TrimSpace(strings.Split(v, ",")[0]), "https") {
				return true
			}
		}
	}
	return false
}

// RateKey is the limiter key for an address: IPv4 as is, IPv6 bucketed to /64.
func RateKey(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	if a.Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return a.String()
}
