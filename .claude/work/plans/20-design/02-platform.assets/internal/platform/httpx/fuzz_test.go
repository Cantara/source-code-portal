package httpx

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func FuzzClientIP(f *testing.F) {
	f.Add("10.0.0.1:1", "1.2.3.4, 10.0.0.2", "https")
	f.Add("[::1]:9", "::ffff:1.2.3.4,,  , bogus", "")
	f.Add("garbage", "", "")
	res := NewClientIPResolver(mustParse("10.0.0.0/8", "::1"))
	f.Fuzz(func(t *testing.T, remote, xff, proto string) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		r.Header.Add("X-Forwarded-For", xff)
		r.Header.Set("X-Forwarded-Proto", proto)
		ip, _ := res.Resolve(r)
		_ = res.HTTPS(r)
		if ip.IsValid() && (ip.Zone() != "" || ip.Is4In6()) {
			t.Fatalf("resolved address %v keeps a zone or a mapped form", ip)
		}
		_ = RateKey(ip)
	})
}

func FuzzRate(f *testing.F) {
	for _, s := range []string{"120/min burst 60", "5/s", "0/min", "99999999999/min", "1/min burst 99999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var r Rate
		if err := r.UnmarshalText([]byte(s)); err == nil && (r.PerMinute < 1 || r.Burst < 1) {
			t.Fatalf("%q accepted as %+v", s, r)
		}
	})
}

func mustParse(c ...string) []netip.Prefix {
	p, err := ParseTrusted(c)
	if err != nil {
		panic(err)
	}
	return p
}
