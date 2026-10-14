package access

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

func trustedCfg() Config {
	c := DefaultConfig()
	c.Mode = ModeTrustedProxy
	c.TrustedProxies = []string{"10.0.0.0/8", "fd00::/8"}
	c.Identity.ProxySecretHeader = "X-SCP-Proxy-Secret"
	c.Identity.ProxySecret = secret.New("s3cret")
	return c
}

func TestConfigValidate(t *testing.T) {
	mut := func(f func(*Config)) Config { c := trustedCfg(); f(&c); return c }
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // substring of the joined error; "" = valid
	}{
		{"default public", DefaultConfig(), ""},
		{"trusted ok", trustedCfg(), ""},
		{"public with proxies", func() Config { c := DefaultConfig(); c.TrustedProxies = []string{"10.0.0.0/8"}; return c }(), "access.trusted_proxies: set but mode is public"},
		{"unknown mode", Config{Mode: "oidc"}, "access.mode"},
		{"no proxies", mut(func(c *Config) { c.TrustedProxies = nil }), "required and non-empty"},
		{"zero prefix v4", mut(func(c *Config) { c.TrustedProxies = []string{"0.0.0.0/0"} }), "trusts every address"},
		{"zero prefix v6", mut(func(c *Config) { c.TrustedProxies = []string{"::/0"} }), "trusts every address"},
		{"public range", mut(func(c *Config) { c.TrustedProxies = []string{"8.8.0.0/16"} }), "allow_public_proxies"},
		{"public range allowed", mut(func(c *Config) { c.TrustedProxies = []string{"8.8.0.0/16"}; c.AllowPublicProxies = true }), ""},
		{"straddling private", mut(func(c *Config) { c.TrustedProxies = []string{"10.0.0.0/7"} }), "allow_public_proxies"},
		{"bare address", mut(func(c *Config) { c.TrustedProxies = []string{"10.1.2.3"} }), ""},
		{"garbage", mut(func(c *Config) { c.TrustedProxies = []string{"nope"} }), "not a CIDR"},
		{"secret without header", mut(func(c *Config) { c.Identity.ProxySecretHeader = "" }), "must be set together"},
		{"header without secret", mut(func(c *Config) { c.Identity.ProxySecret = secret.New("") }), "must be set together"},
		{"duplicate header names", mut(func(c *Config) { c.Identity.EmailHeader = "x-forwarded-user" }), "already used"},
		{"reserved header", mut(func(c *Config) { c.Identity.UserHeader = "Authorization" }), "reserved"},
		{"bad header chars", mut(func(c *Config) { c.Identity.UserHeader = "X Forwarded" }), "not a valid header name"},
		{"groups rule without header", mut(func(c *Config) { c.Identity.GroupsHeader = ""; c.ViewAll.Groups = []string{"a"} }), "requires identity.groups_header"},
		{"all errors reported", mut(func(c *Config) { c.TrustedProxies = nil; c.Identity.UserHeader = ""; c.Identity.MaxGroups = 0 }), "user_header"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
	// every violation at once, not the first only
	err := mutErr()
	for _, want := range []string{"trusted_proxies", "user_header", "max_groups"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("joined error misses %q: %v", want, err)
		}
	}
}

func mutErr() error {
	c := trustedCfg()
	c.TrustedProxies = nil
	c.Identity.UserHeader = ""
	c.Identity.MaxGroups = 0
	return c.Validate()
}

func req(remote string, hdr map[string][]string) *http.Request {
	r := httptest.NewRequest("GET", "/api/v1/repos", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header[http.CanonicalHeaderKey(k)] = v
	}
	return r
}

func TestIdentify(t *testing.T) {
	a, err := New(trustedCfg(), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	sec := []string{"s3cret"}
	tests := []struct {
		name       string
		remote     string
		hdr        map[string][]string
		wantSub    string
		wantGroups []string
		wantAnon   bool
		wantReason Reason
	}{
		{"trusted full", "10.0.0.7:5555", map[string][]string{"X-Forwarded-User": {"alice"}, "X-Forwarded-Groups": {"dev, ops ,dev"}, "X-SCP-Proxy-Secret": sec}, "alice", []string{"dev", "ops"}, false, ""},
		{"trusted ipv6", "[fd00::5]:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-SCP-Proxy-Secret": sec}, "alice", nil, false, ""},
		{"ipv4-mapped peer", "[::ffff:10.1.1.1]:9", map[string][]string{"X-Forwarded-User": {"alice"}, "X-SCP-Proxy-Secret": sec}, "alice", nil, false, ""},
		{"untrusted peer spoofing", "203.0.113.9:1", map[string][]string{"X-Forwarded-User": {"admin"}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonUntrustedPeer},
		{"unix socket peer", "@", map[string][]string{"X-Forwarded-User": {"admin"}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonUntrustedPeer},
		{"wrong secret", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-SCP-Proxy-Secret": {"nope"}}, "", nil, true, ReasonBadSecret},
		{"missing secret", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}}, "", nil, true, ReasonBadSecret},
		{"two secret lines", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-SCP-Proxy-Secret": {"s3cret", "s3cret"}}, "", nil, true, ReasonBadSecret},
		{"duplicate user lines (smuggling)", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice", "admin"}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonDuplicate},
		{"email fallback", "10.0.0.7:1", map[string][]string{"X-Forwarded-Email": {"a@example.org"}, "X-SCP-Proxy-Secret": sec}, "a@example.org", nil, false, ""},
		{"no identity at all", "10.0.0.7:1", nil, "", nil, true, ""},
		{"bidi in subject", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"al‮ice"}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonMalformed},
		{"control char in group", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-Forwarded-Groups": {"a\x01b"}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonMalformed},
		{"subject too long", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {strings.Repeat("a", 257)}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonMalformed},
		{"too many groups", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-Forwarded-Groups": {manyGroups(65)}, "X-SCP-Proxy-Secret": sec}, "", nil, true, ReasonMalformed},
		{"groups on several lines merge", "10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-Forwarded-Groups": {"a", "b"}, "X-SCP-Proxy-Secret": sec}, "alice", []string{"a", "b"}, false, ""},
		{"unrelated x-auth-request header only, untrusted", "198.51.100.1:1", map[string][]string{"X-Auth-Request-User": {"root"}}, "", nil, true, ReasonUntrustedPeer},
		{"underscore variant stripped", "198.51.100.1:1", map[string][]string{"X_forwarded_user": {"root"}}, "", nil, true, ReasonUntrustedPeer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := req(tc.remote, tc.hdr)
			p, reason := a.id.identify(r.RemoteAddr, r.Header)
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			if p.Anonymous != tc.wantAnon || p.Subject != tc.wantSub {
				t.Errorf("principal = %+v, want anon=%v subject=%q", p, tc.wantAnon, tc.wantSub)
			}
			if strings.Join(p.Groups, "|") != strings.Join(tc.wantGroups, "|") {
				t.Errorf("groups = %v, want %v", p.Groups, tc.wantGroups)
			}
			for _, n := range a.id.strip {
				if len(r.Header[n]) != 0 {
					t.Errorf("header %s survived stripping", n)
				}
			}
		})
	}
}

func manyGroups(n int) string {
	var s []string
	for i := 0; i < n; i++ {
		s = append(s, "g"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	return strings.Join(s, ",")
}

func TestPublicModeStripsAndNeverTrusts(t *testing.T) {
	a, _ := New(DefaultConfig(), Deps{})
	r := req("10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}})
	p, reason := a.id.identify(r.RemoteAddr, r.Header)
	if !p.Anonymous || reason != ReasonModePublic || len(r.Header) != 0 {
		t.Fatalf("p=%+v reason=%q hdr=%v", p, reason, r.Header)
	}
}

func TestMiddlewareContextAndMetrics(t *testing.T) {
	rd := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(rd))
	a, err := New(trustedCfg(), Deps{Meter: mp.Meter("test")})
	if err != nil {
		t.Fatal(err)
	}
	var seen authz.Principal
	var rawHeader string
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = authz.PrincipalFrom(r.Context())
		rawHeader = r.Header.Get("X-Forwarded-User")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(200)
	}))
	// authenticated, trusted
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req("10.0.0.7:1", map[string][]string{"X-Forwarded-User": {"alice"}, "X-SCP-Proxy-Secret": {"s3cret"}}))
	if seen.Subject != "alice" || rawHeader != "" {
		t.Fatalf("seen=%+v raw=%q", seen, rawHeader)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	// spoof from outside
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req("203.0.113.1:1", map[string][]string{"X-Forwarded-User": {"alice"}}))
	if !seen.Anonymous || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("spoofed request got %+v / %q", seen, rec.Header().Get("Cache-Control"))
	}
	var rm metricdata.ResourceMetrics
	if err := rd.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			found[m.Name] = true
		}
	}
	if !found[MetricSpoof] || !found[MetricRequests] {
		t.Fatalf("instruments missing: %v", found)
	}
	if a.Info().SpoofDropped != 1 {
		t.Errorf("SpoofDropped = %d", a.Info().SpoofDropped)
	}
}

func TestAuthorizerTable(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  func(*Config)
		p    authz.Principal
		vis  scm.Visibility
		want bool
	}{
		{"public repo anon", nil, authz.Principal{Anonymous: true}, scm.VisPublic, true},
		{"private anon", nil, authz.Principal{Anonymous: true}, scm.VisPrivate, false},
		{"private zero principal (not anonymous flag)", nil, authz.Principal{}, scm.VisPrivate, false},
		{"private authenticated open", nil, authz.Principal{Subject: "a"}, scm.VisPrivate, true},
		{"internal authenticated open", nil, authz.Principal{Subject: "a"}, scm.VisInternal, true},
		{"unknown visibility value fails closed for anon", nil, authz.Principal{Anonymous: true}, scm.Visibility(99), false},
		{"group list match", func(c *Config) { c.ViewAll.Groups = []string{"eng"} }, authz.Principal{Subject: "a", Groups: []string{"eng"}}, scm.VisPrivate, true},
		{"group list miss", func(c *Config) { c.ViewAll.Groups = []string{"eng"} }, authz.Principal{Subject: "a", Groups: []string{"Eng"}}, scm.VisPrivate, false},
		{"subject list case-insensitive", func(c *Config) { c.ViewAll.Subjects = []string{"Alice@Example.org"} }, authz.Principal{Subject: "alice@example.ORG"}, scm.VisPrivate, true},
		{"subject list miss", func(c *Config) { c.ViewAll.Subjects = []string{"bob"} }, authz.Principal{Subject: "alice"}, scm.VisPrivate, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := trustedCfg()
			if tc.cfg != nil {
				tc.cfg(&c)
			}
			a, err := New(c, Deps{})
			if err != nil {
				t.Fatal(err)
			}
			if got := a.Authorizer().CanView(ctx, tc.p, tc.vis); got != tc.want {
				t.Fatalf("CanView = %v, want %v", got, tc.want)
			}
		})
	}
	pub, _ := New(DefaultConfig(), Deps{})
	if pub.Authorizer().CanView(ctx, authz.Principal{Subject: "a"}, scm.VisPrivate) {
		t.Fatal("public mode must never grant non-public visibility")
	}
	if !Operator().CanView(ctx, authz.Principal{Anonymous: true}, scm.VisPrivate) {
		t.Fatal("operator authorizer must allow all")
	}
}

// Property: CanView never differs between internal and private, and ClassOf is derived from it.
func TestAuthorizerMonotone(t *testing.T) {
	ctx := context.Background()
	subjects := []string{"", "a", "b"}
	groups := [][]string{nil, {"g"}, {"h"}, {"g", "h"}}
	for _, mode := range []Mode{ModePublic, ModeTrustedProxy} {
		for _, lists := range []struct{ g, s []string }{{}, {g: []string{"g"}}, {s: []string{"a"}}, {g: []string{"g"}, s: []string{"b"}}} {
			c := trustedCfg()
			c.Mode = mode
			if mode == ModePublic {
				c = DefaultConfig()
			} else {
				c.ViewAll = ViewAll{Groups: lists.g, Subjects: lists.s}
			}
			a, err := New(c, Deps{})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range subjects {
				for _, g := range groups {
					for _, anon := range []bool{true, false} {
						p := authz.Principal{Subject: s, Groups: g, Anonymous: anon}
						az := a.Authorizer()
						if az.CanView(ctx, p, scm.VisInternal) != az.CanView(ctx, p, scm.VisPrivate) {
							t.Fatalf("internal and private differ for %+v mode=%s", p, mode)
						}
						if !az.CanView(ctx, p, scm.VisPublic) {
							t.Fatalf("public must always be visible: %+v", p)
						}
						cls := authz.ClassOf(az, p)
						if (cls == scm.ViewAll) != az.CanView(ctx, p, scm.VisInternal) {
							t.Fatalf("ClassOf inconsistent for %+v", p)
						}
					}
				}
			}
		}
	}
}

func FuzzIdentify(f *testing.F) {
	a, _ := New(trustedCfg(), Deps{})
	f.Add("10.0.0.1:1", "alice", "dev,ops", "s3cret")
	f.Add("1.2.3.4:5", "‮", "\x00", "")
	f.Fuzz(func(t *testing.T, remote, user, groups, sec string) {
		h := http.Header{}
		h.Add("X-Forwarded-User", user)
		h.Add("X-Forwarded-Groups", groups)
		h.Add("X-SCP-Proxy-Secret", sec)
		p, _ := a.id.identify(remote, h)
		if !p.Anonymous {
			if p.Subject == "" || len(p.Subject) > maxSubjectLen || !validText(p.Subject, maxSubjectLen) {
				t.Fatalf("authenticated principal with bad subject %q", p.Subject)
			}
			if sec != "s3cret" {
				t.Fatalf("authenticated without the proxy secret: %q", sec)
			}
			for _, g := range p.Groups {
				if !validText(g, maxGroupLen) || strings.Contains(g, ",") {
					t.Fatalf("bad group %q", g)
				}
			}
		}
		if len(h) != 0 {
			t.Fatalf("headers survived: %v", h)
		}
	})
}

func TestInfoWarnings(t *testing.T) {
	c := trustedCfg()
	c.TrustedProxies = []string{"10.0.1.0/24", "fd00:1:2::/64"}
	a, _ := New(c, Deps{})
	if w := a.Info().Warnings; len(w) != 0 || !a.Info().ProxySecret {
		t.Fatalf("warnings = %v", w)
	}
	c.Identity.ProxySecretHeader, c.Identity.ProxySecret = "", secret.New("")
	c.TrustedProxies = []string{"10.0.0.0/8"}
	a, _ = New(c, Deps{})
	if w := strings.Join(a.Info().Warnings, ","); w != "no_proxy_secret,broad_trusted_range" {
		t.Fatalf("warnings = %q", w)
	}
}

// Verification 2026-10-14 regressions.

// strings.ToLower folds U+212A KELVIN SIGN to "k": a subject registered at the IdP that way must not match the
// allow-list entry of another user.
func TestSubjectCaseFoldIsASCIIOnly(t *testing.T) {
	c := trustedCfg()
	c.ViewAll.Subjects = []string{"kevin@example.org"}
	a, err := New(c, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !a.Authorizer().CanView(ctx, authz.Principal{Subject: "KEVIN@Example.ORG"}, scm.VisPrivate) {
		t.Fatal("ASCII case-insensitivity lost")
	}
	if a.Authorizer().CanView(ctx, authz.Principal{Subject: "Kevin@example.org"}, scm.VisPrivate) {
		t.Fatal("KELVIN SIGN subject matched an ASCII allow-list entry")
	}
}

func TestIdentifyRejectsLineSeparatorsAndStripsAccessToken(t *testing.T) {
	a, _ := New(trustedCfg(), Deps{})
	for _, subj := range []string{"ali ce", "ali ce", "alice"} {
		r := req("10.0.0.7:1", map[string][]string{"X-Forwarded-User": {subj}, "X-SCP-Proxy-Secret": {"s3cret"}})
		p, reason := a.id.identify(r.RemoteAddr, r.Header)
		if !p.Anonymous || reason != ReasonMalformed {
			t.Errorf("%q: p=%+v reason=%q", subj, p, reason)
		}
	}
	r := req("203.0.113.9:1", map[string][]string{"X-Forwarded-Access-Token": {"tok"}})
	if _, reason := a.id.identify(r.RemoteAddr, r.Header); reason != ReasonUntrustedPeer || len(r.Header) != 0 {
		t.Fatalf("X-Forwarded-Access-Token must be stripped and counted: reason=%q hdr=%v", reason, r.Header)
	}
}

func TestMappedIPv6TrustedRangeIsNormalised(t *testing.T) {
	ps, err := ParsePrefixes([]string{"::ffff:10.0.0.0/104", "::ffff:192.168.1.5"}, false)
	if err != nil || ps[0].String() != "10.0.0.0/8" || ps[1].String() != "192.168.1.5/32" {
		t.Fatalf("%v %v", ps, err)
	}
	if _, err := ParsePrefixes([]string{"::ffff:0:0/96"}, true); err == nil {
		t.Fatal("::ffff:0:0/96 is 0.0.0.0/0 and must be refused")
	}
}

// 103 Early Hints must not consume the one-shot Cache-Control rewrite of the final status.
func TestPrivateWriterIgnoresInformationalStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &privateWriter{ResponseWriter: rec}
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusEarlyHints)
	if w.done {
		t.Fatal("1xx consumed the rewrite")
	}
	w.WriteHeader(200)
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

// The warn line is sampled (one per reason per minute); the counter is not.
func TestSpoofLogIsSampledCounterIsNot(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	a, _ := New(trustedCfg(), Deps{Logger: log})
	now := time.Unix(1_000_000, 0)
	a.now = func() time.Time { return now }
	h := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for i := 0; i < 50; i++ {
		h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.1:1", map[string][]string{"X-Forwarded-User": {"x"}}))
	}
	if n := strings.Count(buf.String(), "identity headers dropped"); n != 1 {
		t.Fatalf("%d warn lines for 50 spoofs in one minute", n)
	}
	now = now.Add(2 * time.Minute)
	h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.1:1", map[string][]string{"X-Forwarded-User": {"x"}}))
	if n := strings.Count(buf.String(), "identity headers dropped"); n != 2 {
		t.Fatalf("%d warn lines after the interval", n)
	}
	if a.Info().SpoofDropped != 51 {
		t.Fatalf("SpoofDropped = %d", a.Info().SpoofDropped)
	}
}
