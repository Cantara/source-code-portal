package config

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Ecosystems are the package managers the portal has a provider for (design/packages.md).
// Swift packages and CocoaPods have no registry of their own to configure: their versions are tags and CocoaPods trunk.
var Ecosystems = []string{"maven", "npm", "pypi", "cargo", "go"} //nolint:gochecknoglobals // a constant table

// Packages configures where the portal looks up what a repository publishes: its configured registries, in order, after the ones a
// build file names and before the ecosystem's public registry.
type Packages struct {
	Registries []Registry `mapstructure:"registries"`
	Public     bool       `mapstructure:"public"` // ask the public registries (Maven Central, npmjs, ...); false: only the configured
}

// Registry is one configured package registry.
type Registry struct {
	ID        string       `mapstructure:"id"`        // names the registry in the pages
	Ecosystem string       `mapstructure:"ecosystem"` // one of Ecosystems
	URL       string       `mapstructure:"url"`       // the registry's root: https, http only on a private network
	Network   string       `mapstructure:"network"`   // public (default): reached through the public-only guard; private: on your network
	Auth      RegistryAuth `mapstructure:"auth"`
}

// RegistryAuth is how the portal signs in to a registry: Basic with a user and a password (or a token in its place), or a token,
// sent as a bearer token or in a header of its own. The credentials go only to the registry's own URL.
type RegistryAuth struct {
	User         string `mapstructure:"user"`          // Basic: with password_file
	PasswordFile string `mapstructure:"password_file"` // Basic: the password or an access token
	TokenFile    string `mapstructure:"token_file"`    // a token: Authorization: Bearer, or the header below
	Header       string `mapstructure:"header"`        // the token's header instead (X-JFrog-Art-Api; Authorization for Cargo's raw token)
	Password     Secret `mapstructure:"-"`
	Token        Secret `mapstructure:"-"`
}

// IsSet reports whether the registry has credentials.
func (a RegistryAuth) IsSet() bool { return a.User != "" || a.PasswordFile != "" || a.TokenFile != "" }

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

var registryID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func (p Packages) validate() []string {
	var out []string
	seen := map[string]bool{}
	for i, r := range p.Registries {
		at := fmt.Sprintf("packages.registries[%d]", i)
		if !registryID.MatchString(r.ID) {
			out = append(out, fmt.Sprintf("%s.id: %q is not [a-z0-9-], 1 to 40 characters", at, r.ID))
		} else if seen[r.ID] {
			out = append(out, fmt.Sprintf("%s.id: %q is used twice", at, r.ID))
		}
		seen[r.ID] = true
		if !slices.Contains(Ecosystems, r.Ecosystem) {
			out = append(out, fmt.Sprintf("%s.ecosystem: %q is none of %s", at, r.Ecosystem, strings.Join(Ecosystems, ", ")))
		}
		private := r.Network == "private"
		if r.Network != "" && r.Network != "public" && !private {
			out = append(out, fmt.Sprintf("%s.network: %q is neither public nor private", at, r.Network))
		}
		if u, err := url.Parse(r.URL); err != nil || (u.Scheme != "https" && (u.Scheme != "http" || !private)) || u.Host == "" || u.User != nil ||
			u.RawQuery != "" || u.Fragment != "" {
			out = append(out, fmt.Sprintf("%s.url: %q is not an https URL (http only with network: private; no credentials in it)", at, r.URL))
		}
		a := r.Auth
		switch {
		case a.TokenFile != "" && (a.User != "" || a.PasswordFile != ""):
			out = append(out, at+".auth: either user and password_file, or token_file")
		case (a.User == "") != (a.PasswordFile == "") && a.TokenFile == "":
			out = append(out, at+".auth: user and password_file go together")
		case a.Header != "" && a.TokenFile == "":
			out = append(out, at+".auth.header: only with token_file")
		case a.Header != "" && !headerName.MatchString(a.Header):
			out = append(out, fmt.Sprintf("%s.auth.header: %q is not a header name", at, a.Header))
		}
		if a.IsSet() && strings.HasPrefix(r.URL, "http://") {
			out = append(out, at+".url: credentials are sent over https only")
		}
	}
	return out
}

// readSecrets reads the registries' credentials.
func (p *Packages) readSecrets(read func(key, path string, dst *Secret)) {
	for i := range p.Registries {
		a := &p.Registries[i].Auth
		at := fmt.Sprintf("packages.registries[%d].auth", i)
		if a.PasswordFile != "" {
			read(at+".password_file", a.PasswordFile, &a.Password)
		}
		if a.TokenFile != "" {
			read(at+".token_file", a.TokenFile, &a.Token)
		}
	}
}
