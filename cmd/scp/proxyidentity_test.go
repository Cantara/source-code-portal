package main

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/config"
)

// TestProxyIdentityOff: access.proxy_identity: false never believes the trusted proxy's identity headers, so a proxy that passes a
// client's headers on does not let it claim a group; a token still opens the private repositories. By default the headers are
// believed (the proxy authenticates), as before.
func TestProxyIdentityOff(t *testing.T) {
	forged := []string{"X-Forwarded-User", "mallory", "X-Forwarded-Groups", "staff"}
	tokens := "auth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}\naccess: {private_groups: [staff], proxy_identity: false}\n"
	off, _ := privatePortalAt(t, tokens)
	if code, b, _ := off("/api/v1/repos", forged...); code != http.StatusOK || strings.Contains(b, "zq-hidden") {
		t.Errorf("forged headers with proxy_identity off: %d, shows the private repository:\n%s", code, b)
	}
	if code, b, _ := off("/api/v1/repos", "Authorization", "Bearer "+botSecret); code != http.StatusOK || !strings.Contains(b, "zq-hidden") {
		t.Errorf("a token with proxy_identity off: %d, does not show the private repository:\n%s", code, b)
	}
	on, _ := privatePortalAt(t, "auth: {tokens: [{name: bot, secret_file: $DIR/bot, groups: [staff]}]}\naccess: {private_groups: [staff]}\n")
	if code, b, _ := on("/api/v1/repos", forged...); code != http.StatusOK || !strings.Contains(b, "zq-hidden") {
		t.Errorf("the proxy's headers by default: %d, are not believed:\n%s", code, b)
	}
}

func TestIdentityWarning(t *testing.T) {
	cfg := config.Config{
		GitHub: config.GitHub{Sources: []config.Source{{ID: "gh", IncludePrivate: true}}},
		Server: config.Server{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")}},
		Access: config.Access{ProxyIdentity: true, UserHeader: "X-Forwarded-User", GroupsHeader: "X-Forwarded-Groups"},
		Auth:   config.Auth{Tokens: []config.Token{{Name: "ci"}}},
	}
	if w := identityWarnings(cfg); len(w) != 1 || !strings.Contains(w[0], "access.proxy_identity: false") {
		t.Errorf("a trusted proxy, believed identity headers and sign-in: %v", w)
	}
	off := cfg
	off.Access.ProxyIdentity = false
	noAuth := cfg
	noAuth.Auth = config.Auth{}
	noProxy := cfg
	noProxy.Server.TrustedProxies = nil
	for name, c := range map[string]config.Config{"identity off": off, "no sign-in (the proxy is the identity)": noAuth, "no proxy": noProxy} {
		if w := identityWarnings(c); len(w) != 0 {
			t.Errorf("%s: warns: %v", name, w)
		}
	}
}
