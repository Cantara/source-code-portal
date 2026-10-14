package httpapi

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fakeBroker struct{ ch chan []byte }

func (f fakeBroker) Subscribe() (<-chan []byte, func()) { return f.ch, func() {} }

func TestSSEOutlivesWriteTimeout(t *testing.T) {
	fb := fakeBroker{make(chan []byte, 1)}
	done := make(chan struct{})
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/events", SSE(fb, done))
	ts := httptest.NewUnstartedServer(mux)
	ts.Config.WriteTimeout = 1 * time.Second
	ts.Start()
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	go func() { time.Sleep(2500 * time.Millisecond); fb.ch <- []byte(`{"keys":["commits"]}`) }()
	start := time.Now()
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
		if strings.HasPrefix(sc.Text(), "data:") {
			break
		}
	}
	t.Logf("ct=%s after %s got=%q", resp.Header.Get("Content-Type"), time.Since(start).Round(100*time.Millisecond), got)
	if time.Since(start) < 2*time.Second {
		t.Fatal("event arrived too early?")
	}
}

func TestNotModified(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", `W/"commits-7", "x"`)
	w := httptest.NewRecorder()
	if !NotModified(w, r, ETagFor(7, "commits")) || w.Code != 304 {
		t.Fatalf("expected 304, got %d", w.Code)
	}
}

func TestClientIP(t *testing.T) {
	c, _ := NewClientIP("10.0.0.0/8", "fd00::/8")
	for _, tc := range []struct {
		remote, xff, want string
		honoured          bool
	}{
		{"10.0.0.5:1234", "203.0.113.9", "203.0.113.9", true},
		{"10.0.0.5:1234", "1.2.3.4, 203.0.113.9", "203.0.113.9", true}, // spoofed leftmost ignored
		{"10.0.0.5:1234", "203.0.113.9, 10.0.0.7", "203.0.113.9", true},
		{"198.51.100.1:99", "1.2.3.4", "198.51.100.1", false}, // direct client lies: ignored
		{"10.0.0.5:1234", "not-an-ip", "10.0.0.5", true},
		{"[::ffff:10.0.0.5]:1", "203.0.113.9", "203.0.113.9", true},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.xff)
		got, hon := c.From(r)
		if got.String() != tc.want || hon != tc.honoured {
			t.Errorf("%+v -> %s %v", tc, got, hon)
		}
	}
	a, _ := NewClientIP()
	_ = a
	if RateKey(mustAddr("2001:db8:1:2:aaaa::1")) != RateKey(mustAddr("2001:db8:1:2:bbbb::9")) {
		t.Error("v6 /64 bucketing")
	}
}

func mustAddr(s string) netip.Addr { a, _ := netip.ParseAddr(s); return a }
