// Package netguard builds SSRF-guarded outbound HTTP clients (rank 0; third party: code.dny.dev/ssrf).
// The guard runs at connect time on the resolved IP, so DNS rebinding and every redirect hop are covered.
package netguard

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"code.dny.dev/ssrf"
)

// ErrMetadataAddress: cloud metadata endpoints are denied even when an allow-list covers them.
var ErrMetadataAddress = errors.New("prohibited IP address: cloud metadata endpoint")

// alwaysDenied is checked before any allow-list. Whole link-local ranges are listed rather than single addresses because
// almost every metadata service lives there (169.254.169.254 AWS/GCP/Azure/OpenStack/DigitalOcean/Oracle, 169.254.170.2
// AWS ECS, 169.254.0.23 Tencent, fe80::a9fe:a9fe OpenStack over IPv6) and nothing an operator integrates with does.
var alwaysDenied = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),     // IPv4 link-local
	netip.MustParsePrefix("fe80::/10"),          // IPv6 link-local
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud (inside CGNAT 100.64/10)
	netip.MustParsePrefix("168.63.129.16/32"),   // Azure wire server
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS IMDS over IPv6 (inside fc00::/7)
	netip.MustParsePrefix("fd20:ce::254/128"),   // GCE metadata over IPv6 (documented by Google; not re-verified on 2026-10-14)
}

// Policy is an explicit allow-list for ranges and ports that the default policy blocks (private addresses, odd ports).
// The zero Policy is the strict default: public addresses on ports 80 and 443 only.
type Policy struct {
	allowV4 []netip.Prefix
	allowV6 []netip.Prefix
	ports   []uint16
}

// NewPolicy validates operator input. A /0 prefix, an unparsable CIDR or a port outside 1..65535 is an error.
func NewPolicy(allowCIDRs []string, ports []int) (Policy, error) {
	var p Policy
	var errs []error
	for _, c := range allowCIDRs {
		pf, err := netip.ParsePrefix(strings.TrimSpace(c))
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("allow CIDR %q: %w", c, err))
		case pf.Bits() == 0:
			errs = append(errs, fmt.Errorf("allow CIDR %q allows every address", c))
		case pf.Addr().Is4() || pf.Addr().Is4In6():
			p.allowV4 = append(p.allowV4, netip.PrefixFrom(pf.Addr().Unmap(), pf.Bits()).Masked())
		default:
			p.allowV6 = append(p.allowV6, pf.Masked())
		}
	}
	for _, n := range ports {
		if n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("port %d is out of range", n))
			continue
		}
		p.ports = append(p.ports, uint16(n))
	}
	return p, errors.Join(errs...)
}

// PrivateNetworks lists the ranges an operator means by "this integration lives on a private network":
// RFC 1918, carrier-grade NAT (100.64/10) and IPv6 unique-local. It excludes loopback, link-local and every
// cloud metadata address, which stay denied (the metadata list is checked before any allow-list).
func PrivateNetworks() []string {
	return []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"}
}

// MustPolicy is for tests and constants.
func MustPolicy(allowCIDRs []string, ports []int) Policy {
	p, err := NewPolicy(allowCIDRs, ports)
	if err != nil {
		panic(err)
	}
	return p
}

func (p Policy) guardian() *ssrf.Guardian {
	var opts []ssrf.Option
	if len(p.allowV4) > 0 {
		opts = append(opts, ssrf.WithAllowedV4Prefixes(p.allowV4...))
	}
	if len(p.allowV6) > 0 {
		opts = append(opts, ssrf.WithAllowedV6Prefixes(p.allowV6...))
	}
	if len(p.ports) > 0 {
		opts = append(opts, ssrf.WithPorts(append([]uint16{80, 443}, p.ports...)...))
	}
	return ssrf.New(opts...)
}

// Control is the net.Dialer.Control hook: metadata addresses first, then the ssrf guardian (allow-list, deny-list, ports).
func (p Policy) Control() func(network, address string, c syscall.RawConn) error {
	g := p.guardian()
	return func(network, address string, c syscall.RawConn) error {
		if ap, err := netip.ParseAddrPort(address); err == nil {
			ip := ap.Addr().Unmap()
			for _, d := range alwaysDenied {
				if d.Contains(ip) {
					return ErrMetadataAddress
				}
			}
		}
		return g.Safe(network, address, c)
	}
}

// ClientOptions configure NewClient. Zero values mean the safe defaults named in each field.
type ClientOptions struct {
	Policy         Policy
	DialTimeout    time.Duration // default 5s
	TLSTimeout     time.Duration // default 5s
	HeaderTimeout  time.Duration // default 10s
	Timeout        time.Duration // whole exchange including the body; default 30s; negative = none (the caller uses context deadlines)
	MaxRedirects   int           // 0 = never follow redirects (the 3xx response is returned)
	MaxHeaderBytes int64         // default 64 KiB
	// Hosts pins the request host (and every redirect hop) to this exact list, "host" or "host:port", lower case.
	// Empty means any host: only the untrusted media client does that, and it also sets RefuseIPLiterals.
	Hosts            []string
	RefuseIPLiterals bool
	AllowHTTP        bool // plain http:// permitted (a configured private integration); https only otherwise
	RootCAs          *x509.CertPool
	// ProxyURL, when set, routes through an operator-configured proxy. The connect-time check then covers the proxy hop only,
	// so a client with a proxy is never used for attacker-influenced URLs. Nil is the default and the guarded one.
	ProxyURL  *url.URL
	UserAgent string
	// Wrap lets the caller add instrumentation (otelhttp) or fault injection around the guarded transport.
	Wrap func(http.RoundTripper) http.RoundTripper
}

// NewClient returns an http.Client whose every connection passes the policy. It never uses http.DefaultTransport
// and honours no environment proxy variables.
func NewClient(o ClientOptions) (*http.Client, error) {
	or := func(d, def time.Duration) time.Duration {
		if d > 0 {
			return d
		}
		return def
	}
	if o.MaxHeaderBytes == 0 {
		o.MaxHeaderBytes = 64 << 10
	}
	d := &net.Dialer{Timeout: or(o.DialTimeout, 5*time.Second), Control: o.Policy.Control(), KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext:            d.DialContext,
		TLSHandshakeTimeout:    or(o.TLSTimeout, 5*time.Second),
		ResponseHeaderTimeout:  or(o.HeaderTimeout, 10*time.Second),
		MaxResponseHeaderBytes: o.MaxHeaderBytes,
		MaxIdleConns:           16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second,
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.RootCAs},
	}
	if o.ProxyURL != nil {
		tr.Proxy = http.ProxyURL(o.ProxyURL)
	} // else Proxy stays nil: no environment proxy, and the dial target is the destination
	pinned := map[string]bool{}
	for _, h := range o.Hosts {
		pinned[strings.ToLower(h)] = true
	}
	var rt http.RoundTripper = &guarded{next: tr, o: o, pinned: pinned}
	if o.Wrap != nil {
		rt = o.Wrap(rt)
	}
	max := o.MaxRedirects
	timeout := or(o.Timeout, 30*time.Second)
	if o.Timeout < 0 {
		timeout = 0 // no overall deadline: the caller applies per-request contexts (GraphQL and REST need different values)
	}
	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > max {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}, nil
}

// ErrRefused is wrapped by every request the client refuses before dialing.
var ErrRefused = errors.New("netguard: request refused")

type guarded struct {
	next   *http.Transport
	o      ClientOptions
	pinned map[string]bool
}

func (g *guarded) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := g.check(r.URL); err != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, err
	}
	if g.o.UserAgent != "" && r.Header.Get("User-Agent") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", g.o.UserAgent)
	}
	return g.next.RoundTrip(r)
}

func (g *guarded) check(u *url.URL) error {
	switch {
	case u.Scheme == "https" || (u.Scheme == "http" && g.o.AllowHTTP):
	default:
		return fmt.Errorf("%w: scheme %q", ErrRefused, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in URL", ErrRefused)
	}
	host := strings.ToLower(u.Hostname())
	if g.o.RefuseIPLiterals {
		if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			return fmt.Errorf("%w: IP literal host", ErrRefused)
		}
	}
	if len(g.pinned) > 0 && !g.pinned[host] && !g.pinned[strings.ToLower(u.Host)] {
		return fmt.Errorf("%w: host %q is not pinned", ErrRefused, host)
	}
	return nil
}

// ValidateURL is the static check config validation uses: scheme, no credentials, a host, an allowed port,
// and (for an IP-literal host) the same address policy the dialer applies. Hostnames are only checked at connect time.
func ValidateURL(raw string, allowHTTP bool, p Policy) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an absolute URL", raw)
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return fmt.Errorf("scheme %q is not allowed", u.Scheme)
	}
	if u.User != nil {
		return errors.New("credentials in a URL are not allowed")
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil {
		addr := net.JoinHostPort(ip.String(), port)
		network := "tcp4"
		if ip.Is6() && !ip.Is4In6() {
			network = "tcp6"
		}
		if cerr := p.Control()(network, addr, nil); cerr != nil {
			return fmt.Errorf("%s: %w", addr, cerr)
		}
		return nil
	}
	n, _ := strconv.Atoi(port)
	if n != 80 && n != 443 {
		ok := false
		for _, a := range p.ports {
			ok = ok || int(a) == n
		}
		if !ok {
			return fmt.Errorf("port %d is not allowed; add it to the integration's allowed ports", n)
		}
	}
	return nil
}
