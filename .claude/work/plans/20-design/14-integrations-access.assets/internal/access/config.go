// Package access implements optional trusted-proxy identity and the view-class authorizer (G-08, Q-03, ADR-023).
package access

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Mode selects how a principal is established.
type Mode string

const (
	ModePublic       Mode = "public"
	ModeTrustedProxy Mode = "trusted-proxy"
)

// Config is the feature-owned slice of the root configuration (`access:`).
type Config struct {
	Mode               Mode     `yaml:"mode"`
	TrustedProxies     []string `yaml:"trusted_proxies"`
	AllowPublicProxies bool     `yaml:"allow_public_proxies"`
	Identity           Identity `yaml:"identity"`
	ViewAll            ViewAll  `yaml:"view_all"`
}

// Identity names the headers the SSO proxy sets. Names are matched case-insensitively (canonical MIME form).
type Identity struct {
	UserHeader        string        `yaml:"user_header"`
	EmailHeader       string        `yaml:"email_header"`
	GroupsHeader      string        `yaml:"groups_header"`
	GroupsSeparator   string        `yaml:"groups_separator"`
	MaxGroups         int           `yaml:"max_groups"`
	ProxySecretHeader string        `yaml:"proxy_secret_header"`
	ProxySecret       secret.Secret `yaml:"proxy_secret"`
}

// ViewAll lists who may see non-public repositories. Both lists empty means every authenticated principal.
type ViewAll struct {
	Groups   []string `yaml:"groups"`
	Subjects []string `yaml:"subjects"`
}

// DefaultConfig is the embedded default: public mode, conventional oauth2-proxy header names ready for trusted-proxy mode.
func DefaultConfig() Config {
	return Config{
		Mode: ModePublic,
		Identity: Identity{
			UserHeader: "X-Forwarded-User", EmailHeader: "X-Forwarded-Email", GroupsHeader: "X-Forwarded-Groups",
			GroupsSeparator: ",", MaxGroups: 64,
		},
	}
}

var headerNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// reservedHeaders may never be configured as identity headers.
var reservedHeaders = map[string]bool{
	"Authorization": true, "Cookie": true, "Host": true, "Forwarded": true, "Connection": true, "Content-Length": true,
	"Content-Type": true, "Transfer-Encoding": true, "Upgrade": true, "Te": true, "Trailer": true,
	"X-Forwarded-For": true, "X-Forwarded-Proto": true, "X-Forwarded-Host": true, "X-Real-Ip": true, "X-Scp-Request": true,
}

// Validate returns every violation with a path, never just the first.
func (c Config) Validate() error {
	var errs []error
	add := func(path, msg string, a ...any) {
		errs = append(errs, fmt.Errorf("access.%s: %s", path, fmt.Sprintf(msg, a...)))
	}
	switch c.Mode {
	case ModePublic:
		if len(c.TrustedProxies) > 0 {
			add("trusted_proxies", "set but mode is public (the key would be ignored)")
		}
		if !c.Identity.ProxySecret.IsZero() || c.Identity.ProxySecretHeader != "" {
			add("identity.proxy_secret", "set but mode is public")
		}
		if len(c.ViewAll.Groups)+len(c.ViewAll.Subjects) > 0 {
			add("view_all", "set but mode is public")
		}
	case ModeTrustedProxy:
		c.validateTrusted(add)
	default:
		add("mode", "must be %q or %q, got %q", ModePublic, ModeTrustedProxy, c.Mode)
	}
	return errors.Join(errs...)
}

func (c Config) validateTrusted(add func(path, msg string, a ...any)) {
	if len(c.TrustedProxies) == 0 {
		add("trusted_proxies", "required and non-empty in mode trusted-proxy")
	}
	if _, err := ParsePrefixes(c.TrustedProxies, c.AllowPublicProxies); err != nil {
		add("trusted_proxies", "%v", err)
	}
	id := c.Identity
	seen := map[string]string{}
	check := func(path, name string, required bool) {
		if name == "" {
			if required {
				add(path, "required in mode trusted-proxy")
			}
			return
		}
		if !headerNameRE.MatchString(name) {
			add(path, "%q is not a valid header name", name)
			return
		}
		canon := canonical(name)
		if reservedHeaders[canon] {
			add(path, "%q is reserved and cannot carry identity", name)
		}
		if prev, dup := seen[canon]; dup {
			add(path, "%q is already used by %s", name, prev)
		}
		seen[canon] = path
	}
	check("identity.user_header", id.UserHeader, true)
	check("identity.email_header", id.EmailHeader, false)
	check("identity.groups_header", id.GroupsHeader, false)
	check("identity.proxy_secret_header", id.ProxySecretHeader, false)
	if (id.ProxySecretHeader == "") != id.ProxySecret.IsZero() {
		add("identity.proxy_secret", "proxy_secret_header and proxy_secret must be set together (an empty secret is unset)")
	}
	if id.GroupsHeader != "" && (id.GroupsSeparator == "" || len(id.GroupsSeparator) > 4) {
		add("identity.groups_separator", "must be 1 to 4 bytes")
	}
	if id.MaxGroups < 1 || id.MaxGroups > 256 {
		add("identity.max_groups", "must be between 1 and 256")
	}
	for i, g := range c.ViewAll.Groups {
		if strings.TrimSpace(g) == "" || len(g) > maxGroupLen {
			add(fmt.Sprintf("view_all.groups[%d]", i), "empty or longer than %d bytes", maxGroupLen)
		}
	}
	for i, s := range c.ViewAll.Subjects {
		if strings.TrimSpace(s) == "" || len(s) > maxSubjectLen {
			add(fmt.Sprintf("view_all.subjects[%d]", i), "empty or longer than %d bytes", maxSubjectLen)
		}
	}
	if len(c.ViewAll.Groups) > 0 && id.GroupsHeader == "" {
		add("view_all.groups", "requires identity.groups_header")
	}
}

// AllowsNonPublic reports whether this configuration may serve non-public repositories. cmd/scp calls it from the
// root Validate to refuse catalog visibility private/internal/installation_scope without an auth mode (ADR-023).
func (c Config) AllowsNonPublic() bool { return c.Mode == ModeTrustedProxy }

// ParsePrefixes parses CIDRs or bare addresses. It rejects zero-length prefixes always, and public ranges unless allowPublic.
func ParsePrefixes(in []string, allowPublic bool) ([]netip.Prefix, error) {
	var out []netip.Prefix
	var errs []error
	for i, s := range in {
		s = strings.TrimSpace(s)
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				errs = append(errs, fmt.Errorf("[%d] %q is not a CIDR or address", i, s))
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if p.Addr().Is4In6() && p.Bits() >= 96 { // ::ffff:10.0.0.0/104 is 10.0.0.0/8: peers are unmapped before matching
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		p = p.Masked()
		if p.Bits() == 0 {
			errs = append(errs, fmt.Errorf("[%d] %q trusts every address and is refused", i, s))
			continue
		}
		if !allowPublic && !isPrivateRange(p) {
			errs = append(errs, fmt.Errorf("[%d] %q is not a private, loopback or link-local range (set allow_public_proxies to accept it)", i, s))
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

// isPrivateRange is true when the whole prefix lies inside private, loopback, link-local or unique-local space.
func isPrivateRange(p netip.Prefix) bool {
	for _, a := range []netip.Addr{p.Addr(), lastAddr(p)} {
		a = a.Unmap()
		if !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast()) {
			return false
		}
	}
	return true
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 1 << (7 - uint(i%8))
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}
