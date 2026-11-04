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
	ID        string `mapstructure:"id"`        // names the registry in the pages
	Ecosystem string `mapstructure:"ecosystem"` // one of Ecosystems
	URL       string `mapstructure:"url"`       // https, the registry's root
}

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
		if u, err := url.Parse(r.URL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			out = append(out, fmt.Sprintf("%s.url: %q is not an https URL", at, r.URL))
		}
	}
	return out
}
