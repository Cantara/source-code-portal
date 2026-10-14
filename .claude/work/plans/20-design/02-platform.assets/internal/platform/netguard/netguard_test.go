package netguard_test

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/cantara/source-code-portal/internal/platform/netguard"
)

func port(t *testing.T, s *httptest.Server) int {
	u, _ := url.Parse(s.URL)
	n, _ := strconv.Atoi(u.Port())
	return n
}

func TestDefaultPolicyBlocksLoopbackAndMetadata(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer s.Close()
	c, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true})
	if _, err := c.Get(s.URL); err == nil {
		t.Fatal("loopback must be blocked by default")
	}
	// An allow-list that covers the metadata address does not open it.
	c, _ = netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: netguard.MustPolicy([]string{"169.254.0.0/16"}, nil)})
	_, err := c.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil || !errors.Is(err, netguard.ErrMetadataAddress) {
		t.Fatalf("metadata must stay blocked: %v", err)
	}
	// IPv4-mapped IPv6 spelling of the metadata address.
	_, err = c.Get("http://[::ffff:169.254.169.254]/")
	if err == nil {
		t.Fatal("mapped metadata address must be blocked")
	}
}

func TestAllowListPortsHostPinAndRedirects(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("other")) }))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "http://127.0.0.2:"+strconv.Itoa(port(t, other))+"/", http.StatusFound)
			return
		}
		w.Write([]byte("ok:" + r.Header.Get("User-Agent")))
	}))
	defer s.Close()
	pol := netguard.MustPolicy([]string{"127.0.0.1/32"}, []int{port(t, s)})
	c, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: pol, MaxRedirects: 3, UserAgent: "scp-test"})
	resp, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// A redirect to a second loopback address that is not on the allow-list is refused at connect time.
	if _, err := c.Get(s.URL + "/redir"); err == nil {
		t.Fatal("redirect to 127.0.0.2 must be blocked")
	}
	// A pinned client refuses other hosts before dialing.
	pc, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: pol, Hosts: []string{"ci.example"}})
	if _, err := pc.Get(s.URL); !errors.Is(err, netguard.ErrRefused) {
		t.Fatalf("pin: %v", err)
	}
	// https only unless AllowHTTP.
	hc, _ := netguard.NewClient(netguard.ClientOptions{Policy: pol})
	if _, err := hc.Get(s.URL); !errors.Is(err, netguard.ErrRefused) {
		t.Fatalf("scheme: %v", err)
	}
	// IP literals refused for the untrusted client.
	uc, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: pol, RefuseIPLiterals: true})
	if _, err := uc.Get(s.URL); !errors.Is(err, netguard.ErrRefused) {
		t.Fatalf("literal: %v", err)
	}
	_ = net.IPv4zero
}

func TestPolicyAndValidateURL(t *testing.T) {
	if _, err := netguard.NewPolicy([]string{"0.0.0.0/0", "bad", "10.0.0.0/8"}, []int{0, 8080}); err == nil {
		t.Fatal("want errors")
	}
	p := netguard.MustPolicy([]string{"10.20.0.0/16"}, []int{8080})
	for _, c := range []struct {
		url  string
		ok   bool
		http bool
	}{
		{"https://ci.example.org/", true, false},
		{"http://10.20.1.5:8080/", true, true},
		{"http://10.20.1.5:8080/", false, false},
		{"http://10.30.1.5:8080/", false, true},
		{"https://u:p@h.example/", false, false},
		{"https://ci.example.org:8443/", false, false},
		{"https://ci.example.org:8080/", true, false},
		{"https://169.254.169.254/", false, false},
	} {
		if err := netguard.ValidateURL(c.url, c.http, p); (err == nil) != c.ok {
			t.Errorf("%s: err=%v want ok=%v", c.url, err, c.ok)
		}
	}
}

func TestPrivateNetworksAllowPrivateButNeverLoopbackOrMetadata(t *testing.T) {
	p := netguard.MustPolicy(netguard.PrivateNetworks(), nil)
	ctl := p.Control()
	for addr, wantErr := range map[string]bool{
		"10.1.2.3:443": false, "172.20.0.5:443": false, "192.168.1.9:80": false, "100.64.1.1:443": false, "[fd12::1]:443": false,
		"127.0.0.1:443": true, "[::1]:443": true, "169.254.169.254:80": true, "169.254.1.1:443": true, "100.100.100.200:80": true, "8.8.8.8:22": true,
	} {
		network := "tcp4"
		if addr[0] == '[' {
			network = "tcp6"
		}
		err := ctl(network, addr, nil)
		if (err != nil) != wantErr {
			t.Errorf("%s: err=%v wantErr=%v", addr, err, wantErr)
		}
	}
	if err := netguard.MustPolicy(nil, nil).Control()("tcp4", "10.1.2.3:443", nil); err == nil {
		t.Error("the default policy must block private addresses")
	}
}

func TestNegativeTimeoutMeansNoOverallDeadline(t *testing.T) {
	c, _ := netguard.NewClient(netguard.ClientOptions{Timeout: -1})
	if c.Timeout != 0 {
		t.Fatalf("timeout %v", c.Timeout)
	}
	if d, _ := netguard.NewClient(netguard.ClientOptions{}); d.Timeout.Seconds() != 30 {
		t.Fatalf("default %v", d.Timeout)
	}
}

func TestWholeLinkLocalAndEveryMetadataAddressStayDeniedEvenWhenAllowListed(t *testing.T) {
	ctl := netguard.MustPolicy([]string{"169.254.0.0/16", "fe80::/10", "fc00::/7", "100.64.0.0/10", "168.63.0.0/16"}, nil).Control()
	for addr, network := range map[string]string{
		"169.254.169.254:80": "tcp4", "169.254.170.2:80": "tcp4", "169.254.0.23:80": "tcp4", "[fe80::a9fe:a9fe]:80": "tcp6",
		"100.100.100.200:80": "tcp4", "168.63.129.16:80": "tcp4", "[fd00:ec2::254]:80": "tcp6", "[fd20:ce::254]:80": "tcp6",
	} {
		if err := ctl(network, addr, nil); err == nil {
			t.Errorf("%s must be denied even with an allow-list that covers it", addr)
		}
	}
}

func TestHostnameResolvingToLoopbackIsBlockedAtConnectTime(t *testing.T) { // DNS rebinding: the name is fine, the answer is not
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer s.Close()
	c, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: netguard.MustPolicy(nil, []int{port(t, s)})})
	if _, err := c.Get("http://localhost:" + strconv.Itoa(port(t, s))); err == nil {
		t.Fatal("localhost resolves to loopback and must be refused by the dialer")
	}
}

func TestClientIgnoresProxyEnvironmentAndDoesNotFollowRedirectsByDefault(t *testing.T) {
	hit := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit <- struct{}{} }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	var dest *httptest.Server
	dest = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Write([]byte("final"))
	}))
	defer dest.Close()
	c, _ := netguard.NewClient(netguard.ClientOptions{AllowHTTP: true, Policy: netguard.MustPolicy([]string{"127.0.0.1/32"}, []int{port(t, dest), port(t, proxy)})})
	resp, err := c.Get(dest.URL + "/r")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("default MaxRedirects 0 must return the 3xx, got %d", resp.StatusCode)
	}
	select {
	case <-hit:
		t.Fatal("the environment proxy was used")
	default:
	}
}
