package egress

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestGuard(t *testing.T) {
	for addr, ok := range map[string]bool{
		"140.82.112.3:443": true, "[2606:50c0:8000::153]:443": true,
		"140.82.112.3:80": false, "127.0.0.1:443": false, "10.1.2.3:443": false, "172.16.0.1:443": false, "192.168.1.1:443": false,
		"169.254.169.254:443": false, "100.64.0.1:443": false, "0.0.0.0:443": false, "0.1.2.3:443": false, "224.0.0.1:443": false,
		"[::1]:443": false, "[fe80::1]:443": false, "[fd00::1]:443": false, "[::ffff:127.0.0.1]:443": false, "[::ffff:10.0.0.1]:443": false,
		"[64:ff9b::a00:1]:443": false, "[2002:a00:1::]:443": false,
	} {
		if err := publicOnly("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("%s: %v, want allowed %v", addr, err, ok)
		}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if resp, err := Guarded(5 * time.Second).RoundTrip(req); err == nil {
		_ = resp.Body.Close()
		t.Error("the guarded transport reached a loopback address")
	}
	if !Public(netip.MustParseAddr("8.8.8.8")) {
		t.Error("8.8.8.8 is public")
	}
}
