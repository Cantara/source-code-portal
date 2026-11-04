// Package egress is the way out to addresses that untrusted content names: https to public unicast addresses only, checked on the
// address dialled, so neither a redirect nor a DNS answer reaches the portal's own network.
package egress

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Guarded is the transport for addresses that untrusted content names (an image in a README, a registry in a build file): https to port 443 of public unicast addresses only (checked on the address dialled,
// so a redirect or a DNS answer cannot reach the portal's network), no proxy from the environment.
func Guarded(timeout time.Duration) http.RoundTripper {
	d := &net.Dialer{Timeout: 5 * time.Second, Control: publicOnly}
	return &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: timeout,
		MaxIdleConns: 16, IdleConnTimeout: time.Minute, ForceAttemptHTTP2: true}
}

// publicOnly is the dial control of Guarded.
func publicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return err
	}
	if ap.Port() != 443 || !Public(ap.Addr()) {
		return fmt.Errorf("egress: %s is not a public https address", address)
	}
	return nil
}

var nonPublic = []netip.Prefix{ //nolint:gochecknoglobals // a constant table
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 reaches IPv4 addresses
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), // 6to4 embeds IPv4 addresses
}

// Public reports whether a is a public unicast address.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
