package config

import (
	"errors"
	"strings"
	"testing"
)

// oidcYAML is a sign-in configuration the enterprise.oidc keys need.
func oidcYAML(t *testing.T) string {
	t.Helper()
	return "server: {public_url: 'https://scp.example'}\nauth:\n  session_key_file: " + write(t, "key", strings.Repeat("k", 32)) +
		"\n  oidc: {issuer: 'https://login.example/tenant/v2.0', client_id: scp}\n"
}

func TestEnterprise(t *testing.T) {
	cfg, _, err := Load(file(t, oidcYAML(t)+`enterprise:
  enabled: true
  github:
    - {source: gh, web_url: 'https://octocorp.ghe.com/', graphql_url: 'https://api.octocorp.ghe.com/graphql'}
  oidc: {identity_claim: preferred_username, userinfo: false}
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, o := cfg.GitHub.Sources[0], cfg.Auth.OIDC
	if s.WebURL != "https://octocorp.ghe.com/" || s.GraphQLURL != "https://api.octocorp.ghe.com/graphql" ||
		o.IdentityClaim != "preferred_username" || o.UserInfo {
		t.Errorf("source %+v, oidc %+v", s, o)
	}
}

func TestEnterpriseOffKeepsToday(t *testing.T) {
	cfg, _, err := Load(file(t, oidcYAML(t)+"enterprise: {enabled: false}\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, o := cfg.GitHub.Sources[0], cfg.Auth.OIDC; s.WebURL != "" || s.GraphQLURL != "" || o.IdentityClaim != "email" || !o.UserInfo {
		t.Errorf("source %+v, oidc %+v", s, o)
	}
}

func TestEnterpriseInvalid(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"enterprise: {github: [{source: gh, web_url: 'https://octocorp.ghe.com/'}]}\n", "enterprise: set while enterprise.enabled is false"},
		{"enterprise: {oidc: {userinfo: false}}\n", "enterprise: set while enterprise.enabled is false"},
		{"enterprise: {enabled: true, github: [{source: other, web_url: 'https://octocorp.ghe.com/'}]}\n", `enterprise.github[0].source: "other" is not a github.sources id`},
		{"enterprise: {enabled: true, github: [{source: gh, web_url: 'https://a.example/'}, {source: gh, web_url: 'https://a.example/'}]}\n", `enterprise.github[1].source: "gh" is configured twice`},
		{"enterprise: {enabled: true, github: [{source: gh}]}\n", "enterprise.github[0].web_url"},
		{"enterprise: {enabled: true, github: [{source: gh, web_url: 'https://octocorp.ghe.com'}]}\n", "enterprise.github[0].web_url"},
		{"enterprise: {enabled: true, github: [{source: gh, web_url: 'http://octocorp.ghe.com/'}]}\n", "enterprise.github[0].web_url"},
		{"enterprise: {enabled: true, github: [{source: gh, web_url: 'https://octocorp.ghe.com/', graphql_url: 'ftp://x/'}]}\n", "enterprise.github[0].graphql_url"},
		{"enterprise: {enabled: true, oidc: {identity_claim: upn}}\n", "enterprise.oidc: needs auth.oidc.issuer"},
	} {
		_, _, err := Load(file(t, tc.yaml), nil)
		var inv *Invalid
		if !errors.As(err, &inv) || !strings.Contains(strings.Join(inv.Problems, "\n"), tc.want) {
			t.Errorf("%s: %v, want %q", tc.yaml, err, tc.want)
		}
	}
	for _, tc := range []struct{ yaml, want string }{
		{"enterprise: {enabled: true, oidc: {identity_claim: 'bad claim'}}\n", "enterprise.oidc.identity_claim: \"bad claim\" is not a claim name"},
		{"enterprise: {enabled: true, oidc: {identity_claim: preferred_username}}\n", "must be email with auth.oidc.require_verified_email"},
	} {
		yaml := strings.Replace(oidcYAML(t), "client_id: scp}", "client_id: scp, require_verified_email: true}", 1) + tc.yaml
		_, _, err := Load(file(t, yaml), nil)
		var inv *Invalid
		if !errors.As(err, &inv) || !strings.Contains(strings.Join(inv.Problems, "\n"), tc.want) {
			t.Errorf("%s: %v, want %q", tc.yaml, err, tc.want)
		}
	}
}
