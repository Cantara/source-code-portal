package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Enterprise holds the settings GitHub Enterprise and Microsoft Entra ID need beyond github.com and a generic OIDC provider
// (design/enterprise.md). Off by default, and while it is off every other key of it is refused.
type Enterprise struct {
	Enabled bool               `mapstructure:"enabled"`
	GitHub  []EnterpriseSource `mapstructure:"github"`
	OIDC    EnterpriseOIDC     `mapstructure:"oidc"`
}

// EnterpriseSource gives a GitHub source the addresses its API base does not tell.
type EnterpriseSource struct {
	Source     string `mapstructure:"source"`      // a github.sources[].id
	WebURL     string `mapstructure:"web_url"`     // links to repositories and files, ending in /
	GraphQLURL string `mapstructure:"graphql_url"` // optional: default beside the source's base_url
}

// EnterpriseOIDC adapts sign-in to a provider whose tokens carry other claims (Entra ID).
type EnterpriseOIDC struct {
	IdentityClaim string `mapstructure:"identity_claim"` // matched against auth.admin_emails and shown; default email
	UserInfo      *bool  `mapstructure:"userinfo"`       // false: never call the provider's userinfo endpoint; default true
}

// set reports whether any key but enabled is configured.
func (e Enterprise) set() bool {
	return len(e.GitHub) > 0 || e.OIDC.IdentityClaim != "" || e.OIDC.UserInfo != nil
}

// applyEnterprise gives the sources and the OIDC settings their enterprise values; without the section they keep today's.
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
	for _, e := range c.Enterprise.GitHub {
		for i := range c.GitHub.Sources {
			if s := &c.GitHub.Sources[i]; s.ID == e.Source {
				s.WebURL, s.GraphQLURL = e.WebURL, e.GraphQLURL
			}
		}
	}
}

func (e Enterprise) validate(g GitHub, a Auth) []string {
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
		case !slices.ContainsFunc(g.Sources, func(src Source) bool { return src.ID == s.Source }):
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
	}
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
	return p
}

// webAddress is an absolute https URL without user, query or fragment; http only on loopback.
func webAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" && loopback(u.Hostname()))
}
