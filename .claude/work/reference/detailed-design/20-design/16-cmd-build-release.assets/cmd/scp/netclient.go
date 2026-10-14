package main

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/platform/netguard"
)

// newGitHubHTTP is github.Deps.NewHTTP: one SSRF-guarded client per source (design 05 section 2.4, 16 section 4.4). It is the only
// place the loopback allowance of a development build enters, and it is empty in every other build.
func newGitHubHTTP(n github.Network) (*http.Client, error) {
	cidrs := extraAllowedCIDRs()
	var ports []int
	if n.AllowPrivateAddresses {
		cidrs = append(cidrs, netguard.PrivateNetworks()...)
	}
	for _, h := range n.Hosts { // a pinned "host:port" opens that port (and only that port) in the policy
		if _, p, err := net.SplitHostPort(h); err == nil {
			if v, err := strconv.Atoi(p); err == nil {
				ports = append(ports, v)
			}
		}
	}
	pol, err := netguard.NewPolicy(cidrs, ports)
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", n.Source, err)
	}
	o := netguard.ClientOptions{
		Policy: pol, Hosts: n.Hosts, AllowHTTP: n.AllowInsecureHTTP,
		Timeout:       -1, // no overall deadline: the adapter applies per-request contexts (05 section 2.4 point 1)
		HeaderTimeout: n.HTTP.ResponseHeaderTimeout,
		MaxRedirects:  0, // the adapter follows nothing by itself: a 3xx is data
		UserAgent:     "scp/" + version,
	}
	if n.ProxyURL != "" {
		if o.ProxyURL, err = url.Parse(n.ProxyURL); err != nil {
			return nil, fmt.Errorf("source %s: proxy_url: %w", n.Source, err)
		}
	}
	if n.CAFile != "" {
		pem, err := os.ReadFile(n.CAFile)
		if err != nil {
			return nil, fmt.Errorf("source %s: tls.ca_file: %w", n.Source, err)
		}
		o.RootCAs = x509.NewCertPool()
		if !o.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("source %s: tls.ca_file: no certificate found in %s", n.Source, n.CAFile)
		}
	}
	return netguard.NewClient(o)
}
