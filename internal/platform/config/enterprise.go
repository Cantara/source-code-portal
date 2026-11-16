package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Enterprise holds the settings GitHub Enterprise and Microsoft Entra ID need beyond github.com and a generic OIDC provider
// (design/enterprise.md). Off by default, and while it is off every other key of it is refused. A source read with its viewers' own
// tokens (github[].people) makes the configuration one of the enterprise mode (scp enterprise), and scp serve refuses it.
type Enterprise struct {
	Enabled bool               `mapstructure:"enabled"`
	GitHub  []EnterpriseSource `mapstructure:"github"`
	OIDC    EnterpriseOIDC     `mapstructure:"oidc"`
	// TokenKeyFile holds the key (at least 32 bytes) the people's tokens are sealed with in the store; the enterprise mode needs it.
	TokenKeyFile string           `mapstructure:"token_key_file"`
	TokenKey     Secret           `mapstructure:"-"`
	Access       EnterpriseAccess `mapstructure:"access"`
}

// EnterpriseSource gives a GitHub source the addresses its API base does not tell, and whether its viewers' tokens read it.
type EnterpriseSource struct {
	Source     string `mapstructure:"source"`      // a github.sources[].id
	WebURL     string `mapstructure:"web_url"`     // links to repositories and files, ending in /
	GraphQLURL string `mapstructure:"graphql_url"` // optional: default beside the source's base_url
	// People: the source has no credential of its own; each viewer links their own token, and sees what it reads (scp enterprise).
	People bool `mapstructure:"people"`
	// Login is how a linked token's GitHub login is tied to the viewer's identity claim: managed (a managed user's login, the part of
	// the claim before @ with other characters as -, then _ and the enterprise's shortcode) or exact (the login is the claim). Default
	// managed.
	Login string `mapstructure:"login"`
}

// EnterpriseOIDC adapts sign-in to a provider whose tokens carry other claims (Entra ID).
type EnterpriseOIDC struct {
	IdentityClaim string `mapstructure:"identity_claim"` // matched against auth.admin_emails and shown; default email
	UserInfo      *bool  `mapstructure:"userinfo"`       // false: never call the provider's userinfo endpoint; default true
}

// EnterpriseAccess is how often a person's access is read from GitHub, and how old it may be before it counts as unknown.
type EnterpriseAccess struct {
	Refresh time.Duration `mapstructure:"refresh"` // default 15m
	MaxAge  time.Duration `mapstructure:"max_age"` // default 1h
}

// People reports whether the configuration is one of the enterprise mode: a source is read with its viewers' tokens.
func (c Config) People() bool {
	return slices.ContainsFunc(c.GitHub.Sources, func(s Source) bool { return s.People })
}

// set reports whether any key but enabled and the defaulted durations is configured.
func (e Enterprise) set() bool {
	return len(e.GitHub) > 0 || e.OIDC.IdentityClaim != "" || e.OIDC.UserInfo != nil || e.TokenKeyFile != ""
}

// applyEnterprise gives the sources and the OIDC settings their enterprise values; without the section they keep today's. A source
// read with people's tokens holds private and internal repositories, and a portal of such sources has no public view.
func (c *Config) applyEnterprise() {
	c.Auth.OIDC.IdentityClaim, c.Auth.OIDC.UserInfo = "email", true
	if !c.Enterprise.Enabled {
		return
	}
	if e := c.Enterprise.OIDC; e.IdentityClaim != "" {
		c.Auth.OIDC.IdentityClaim = e.IdentityClaim
	}
	if e := c.Enterprise.OIDC; e.UserInfo != nil {
		c.Auth.OIDC.UserInfo = *e.UserInfo
	}
	for i := range c.Enterprise.GitHub {
		e := &c.Enterprise.GitHub[i]
		if e.People && e.Login == "" {
			e.Login = "managed"
		}
		for j := range c.GitHub.Sources {
			if s := &c.GitHub.Sources[j]; s.ID == e.Source {
				s.WebURL, s.GraphQLURL, s.People, s.Login = e.WebURL, e.GraphQLURL, e.People, e.Login
				if e.People {
					s.IncludePrivate = true
				}
			}
		}
	}
	if c.People() {
		c.Access.RequireAuth = true
	}
}

func (e Enterprise) validate(c Config) []string {
	if !e.Enabled {
		if e.set() {
			return []string{"enterprise: set while enterprise.enabled is false"}
		}
		return nil
	}
	var p []string
	seen := map[string]bool{}
	for i, s := range e.GitHub {
		at := fmt.Sprintf("enterprise.github[%d]", i)
		switch {
		case !slices.ContainsFunc(c.GitHub.Sources, func(src Source) bool { return src.ID == s.Source }):
			p = append(p, fmt.Sprintf("%s.source: %q is not a github.sources id", at, s.Source))
		case seen[s.Source]:
			p = append(p, fmt.Sprintf("%s.source: %q is configured twice", at, s.Source))
		}
		seen[s.Source] = true
		if !webAddress(s.WebURL) || !strings.HasSuffix(s.WebURL, "/") {
			p = append(p, fmt.Sprintf("%s.web_url: %q must be an https URL ending in / (http only on loopback)", at, s.WebURL))
		}
		if s.GraphQLURL != "" && !webAddress(s.GraphQLURL) {
			p = append(p, fmt.Sprintf("%s.graphql_url: %q must be an https URL (http only on loopback)", at, s.GraphQLURL))
		}
		switch {
		case !s.People && s.Login != "":
			p = append(p, at+".login: only for a source read with people's tokens (people: true)")
		case s.People && s.Login != "managed" && s.Login != "exact":
			p = append(p, fmt.Sprintf("%s.login: %q is not one of managed, exact", at, s.Login))
		}
	}
	a := c.Auth
	o := e.OIDC
	if (o.IdentityClaim != "" || o.UserInfo != nil) && a.OIDC.Issuer == "" {
		p = append(p, "enterprise.oidc: needs auth.oidc.issuer")
	}
	if o.IdentityClaim != "" && !claimPattern.MatchString(o.IdentityClaim) {
		p = append(p, fmt.Sprintf("enterprise.oidc.identity_claim: %q is not a claim name", o.IdentityClaim))
	}
	if o.IdentityClaim != "" && o.IdentityClaim != "email" && a.OIDC.RequireVerifiedEmail {
		p = append(p, "enterprise.oidc.identity_claim: must be email with auth.oidc.require_verified_email (email_verified speaks of the email only)")
	}
	if e.Access.Refresh < time.Minute || e.Access.Refresh > 24*time.Hour {
		p = append(p, "enterprise.access.refresh: must be 1m..24h")
	}
	if e.Access.MaxAge < e.Access.Refresh || e.Access.MaxAge > 7*24*time.Hour {
		p = append(p, "enterprise.access.max_age: must be at least enterprise.access.refresh and at most 168h")
	}
	return append(p, e.validateMode(c)...)
}

// validateMode checks a configuration of the enterprise mode: every source read with people's tokens (the modes are not mixed),
// sign-in through OIDC (the identity a token is tied to), the key that seals the tokens, and no webhook (polling only).
func (e Enterprise) validateMode(c Config) []string {
	if !c.People() {
		if e.TokenKeyFile != "" {
			return []string{"enterprise.token_key_file: only with a source read with people's tokens (enterprise.github[].people)"}
		}
		return nil
	}
	var p []string
	for i, s := range c.GitHub.Sources {
		if !s.People {
			p = append(p, fmt.Sprintf("github.sources[%d]: every source is read with people's tokens once one is (enterprise.github[].people)", i))
		}
	}
	if c.Auth.OIDC.Issuer == "" {
		p = append(p, "auth.oidc.issuer: required when people's tokens read the sources (a token is tied to the signed-in identity)")
	}
	if e.TokenKeyFile == "" {
		p = append(p, "enterprise.token_key_file: required when people's tokens read the sources")
	}
	if c.Webhook.SecretFile != "" || c.Webhook.CatchUp {
		p = append(p, "webhook: not with people's tokens: the sources are polled")
	}
	if len(c.Auth.Tokens) > 0 {
		p = append(p, "auth.tokens: not with people's tokens: a viewer is a signed-in person")
	}
	if len(c.Access.PrivateGroups) > 0 {
		p = append(p, "access.private_groups: not with people's tokens: what a viewer sees is what their tokens read")
	}
	return p
}

// webAddress is an absolute https URL without user, query or fragment; http only on loopback.
func webAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" && loopback(u.Hostname()))
}
