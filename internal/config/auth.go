package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Auth configures SCP's own sign-in (design/auth.md): sessions sealed with the session key, OIDC, bearer tokens and the admin
// role. Without a session key nobody signs in; tokens still work as bearer credentials.
type Auth struct {
	SessionKeyFile string        `mapstructure:"session_key_file"`
	SessionKey     Secret        `mapstructure:"-"` // read from SessionKeyFile
	SessionTTL     time.Duration `mapstructure:"session_ttl"`
	AdminEmails    []string      `mapstructure:"admin_emails"`
	AdminGroups    []string      `mapstructure:"admin_groups"`
	Tokens         []Token       `mapstructure:"tokens"`
	OIDC           OIDC          `mapstructure:"oidc"`
}

// Token is a bearer credential for machines: scripts and LLM agents.
type Token struct {
	Name       string   `mapstructure:"name"`
	SecretFile string   `mapstructure:"secret_file"`
	Secret     Secret   `mapstructure:"-"`
	Groups     []string `mapstructure:"groups"`
	Admin      bool     `mapstructure:"admin"`
}

// OIDC configures sign-in through an OpenID Connect provider; an empty issuer turns it off.
type OIDC struct {
	Issuer           string   `mapstructure:"issuer"`
	ClientID         string   `mapstructure:"client_id"`
	ClientSecretFile string   `mapstructure:"client_secret_file"` // empty: a public client with PKCE
	ClientSecret     Secret   `mapstructure:"-"`
	Scopes           []string `mapstructure:"scopes"`
	GroupsClaim      string   `mapstructure:"groups_claim"`
}

// SignIn reports whether people can sign in: a session key is configured.
func (a Auth) SignIn() bool { return a.SessionKeyFile != "" }

// MinSecret is the length a session key and a token secret need at least.
const MinSecret = 32

var (
	tokenNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	claimPattern     = regexp.MustCompile(`^[A-Za-z0-9_:/.-]{1,128}$`)
)

func (a Auth) validate(publicURL string) []string {
	var p []string
	if a.SessionTTL < 5*time.Minute || a.SessionTTL > 30*24*time.Hour {
		p = append(p, "auth.session_ttl: must be 5m..720h")
	}
	for _, e := range a.AdminEmails {
		if !strings.Contains(e, "@") || strings.TrimSpace(e) != e || len(e) > 254 {
			p = append(p, fmt.Sprintf("auth.admin_emails: %q is not an email address", e))
		}
	}
	p = append(p, groupNames("auth.admin_groups", a.AdminGroups)...)
	names := map[string]bool{}
	for i, t := range a.Tokens {
		at := fmt.Sprintf("auth.tokens[%d]", i)
		if !tokenNamePattern.MatchString(t.Name) {
			p = append(p, fmt.Sprintf("%s.name: %q must be 1..64 of a-z, 0-9 and -", at, t.Name))
		}
		if names[t.Name] {
			p = append(p, fmt.Sprintf("%s.name: duplicate %q", at, t.Name))
		}
		names[t.Name] = true
		if t.SecretFile == "" {
			p = append(p, at+".secret_file: required")
		}
		p = append(p, groupNames(at+".groups", t.Groups)...)
	}
	o := a.OIDC
	if o.Issuer == "" {
		return p
	}
	if u, err := url.Parse(o.Issuer); err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" ||
		u.Scheme != "https" && (u.Scheme != "http" || !loopback(u.Hostname())) {
		p = append(p, fmt.Sprintf("auth.oidc.issuer: %q is not an https URL (http only on loopback)", o.Issuer))
	}
	if !a.SignIn() {
		p = append(p, "auth.session_key_file: required with auth.oidc.issuer")
	}
	if strings.TrimSpace(o.ClientID) == "" {
		p = append(p, "auth.oidc.client_id: required with auth.oidc.issuer")
	}
	if publicURL == "" {
		p = append(p, "server.public_url: required with auth.oidc.issuer (the redirect URI is under it)")
	}
	if !slices.Contains(o.Scopes, "openid") {
		p = append(p, "auth.oidc.scopes: must include openid")
	}
	if !claimPattern.MatchString(o.GroupsClaim) {
		p = append(p, fmt.Sprintf("auth.oidc.groups_claim: %q is not a claim name", o.GroupsClaim))
	}
	return p
}

// readSecrets reads the session key, the OIDC client secret and the token secrets.
func (a *Auth) readSecrets(read func(key, path string, dst *Secret)) []string {
	var p []string
	if a.SessionKeyFile != "" {
		read("auth.session_key_file", a.SessionKeyFile, &a.SessionKey)
		if a.SessionKey.IsSet() && len(a.SessionKey.Reveal()) < MinSecret {
			p = append(p, fmt.Sprintf("auth.session_key_file: at least %d bytes", MinSecret))
		}
	}
	if a.OIDC.ClientSecretFile != "" {
		read("auth.oidc.client_secret_file", a.OIDC.ClientSecretFile, &a.OIDC.ClientSecret)
	}
	for i := range a.Tokens {
		t := &a.Tokens[i]
		if t.SecretFile == "" {
			continue
		}
		key := fmt.Sprintf("auth.tokens[%d].secret_file", i)
		read(key, t.SecretFile, &t.Secret)
		if t.Secret.IsSet() && len(t.Secret.Reveal()) < MinSecret {
			p = append(p, fmt.Sprintf("%s: at least %d characters", key, MinSecret))
		}
	}
	return p
}

func groupNames(key string, groups []string) []string {
	var p []string
	for _, g := range groups {
		if strings.TrimSpace(g) == "" || strings.Contains(g, ",") || len(g) > 256 {
			p = append(p, fmt.Sprintf("%s: %q is not a group name", key, g))
		}
	}
	return p
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
