// Package integrations holds the StatusProvider contract, target resolution, the sweep jobs and the status slot
// (Q-05, Q-25). Providers live in integrations/jenkins and integrations/snyk.
package integrations

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Config is the feature-owned slice of the root configuration (`integrations:`). Everything is off unless configured.
type Config struct {
	Jenkins JenkinsConfig `yaml:"jenkins"`
	Snyk    SnykConfig    `yaml:"snyk"`
}

// JenkinsConfig: the provider is enabled iff Instances is non-empty (there is no separate enabled flag to forget).
type JenkinsConfig struct {
	Instances    []JenkinsInstance `yaml:"instances"`
	TTL          time.Duration     `yaml:"ttl"`           // sweep interval, default 5m
	SweepTimeout time.Duration     `yaml:"sweep_timeout"` // default 4m, must be < ttl
	Concurrency  int               `yaml:"concurrency"`   // parallel requests inside one sweep, default 4
	Pace         time.Duration     `yaml:"pace"`          // minimum interval between request starts, default 100ms
	AbortAfter   int               `yaml:"abort_after"`   // consecutive transient failures that abort an instance's sweep, default 5
	NotFoundTTL  time.Duration     `yaml:"not_found_ttl"` // how long a 404 job is not probed again, default 1h
}

type JenkinsInstance struct {
	Name              string        `yaml:"name"`       // ^[a-z0-9][a-z0-9-]{0,31}$
	BaseURL           string        `yaml:"base_url"`   // https://host[:port][/context]; no userinfo, query or fragment
	Default           bool          `yaml:"default"`    // required on exactly one instance when there are several
	AllowHTTP         bool          `yaml:"allow_http"` // plain http only for explicit private instances
	User              string        `yaml:"user"`
	Token             secret.Secret `yaml:"token"`               // API token (basic auth password); optional
	AllowPrivateCIDRs []string      `yaml:"allow_private_cidrs"` // consumed by cmd/scp when it builds the netguard client
	Ports             []int         `yaml:"ports"`               // idem
	Discover          string        `yaml:"discover"`            // declared (default) | all
	JobPrefix         string        `yaml:"job_prefix"`          // used when discover: all and no group defines one
	Multibranch       string        `yaml:"multibranch"`         // auto (default) | always | never
	Timeout           time.Duration `yaml:"timeout"`             // per request, default 10s
	MaxResponseBytes  int64         `yaml:"max_response_bytes"`  // default 1 MiB
	ExposeDetailURL   bool          `yaml:"expose_detail_url"`   // default false: build links are not published (internal host names)
}

type SnykConfig struct {
	Enabled           bool          `yaml:"enabled"`
	Token             secret.Secret `yaml:"token"`
	OrgID             string        `yaml:"org_id"`   // UUID of the Snyk organisation
	BaseURL           string        `yaml:"base_url"` // regional REST host, default https://api.snyk.io
	AllowCustomHost   bool          `yaml:"allow_custom_host"`
	APIVersion        string        `yaml:"api_version"`         // default 2024-10-15
	Origins           []string      `yaml:"origins"`             // optional project-origin filter
	FailOn            string        `yaml:"fail_on"`             // critical|high (default)|medium|low
	DetailURLTemplate string        `yaml:"detail_url_template"` // optional; placeholders {project_id} {owner} {repo}
	TTL               time.Duration `yaml:"ttl"`                 // default 15m
	SweepTimeout      time.Duration `yaml:"sweep_timeout"`       // default 2m
	Timeout           time.Duration `yaml:"timeout"`             // per request, default 20s
	MaxResponseBytes  int64         `yaml:"max_response_bytes"`  // default 4 MiB
	AllowPrivateCIDRs []string      `yaml:"allow_private_cidrs"`
	Ports             []int         `yaml:"ports"`
}

// KnownSnykHosts are the documented regional REST hosts (docs.snyk.io regional hosting, read 2026-10-14).
var KnownSnykHosts = []string{"api.snyk.io", "api.us.snyk.io", "api.eu.snyk.io", "api.au.snyk.io"}

// DefaultConfig is the embedded default: everything off.
func DefaultConfig() Config {
	return Config{
		Jenkins: JenkinsConfig{TTL: 5 * time.Minute, SweepTimeout: 4 * time.Minute, Concurrency: 4, Pace: 100 * time.Millisecond, AbortAfter: 5, NotFoundTTL: time.Hour},
		Snyk: SnykConfig{BaseURL: "https://api.snyk.io", APIVersion: "2024-10-15", FailOn: "high", TTL: 15 * time.Minute,
			SweepTimeout: 2 * time.Minute, Timeout: 20 * time.Second, MaxResponseBytes: 4 << 20},
	}
}

// ApplyInstanceDefaults fills per-instance zero values (a list element cannot carry embedded defaults).
func (c *JenkinsConfig) ApplyInstanceDefaults() {
	for i := range c.Instances {
		in := &c.Instances[i]
		if in.Discover == "" {
			in.Discover = "declared"
		}
		if in.Multibranch == "" {
			in.Multibranch = "auto"
		}
		if in.Timeout == 0 {
			in.Timeout = 10 * time.Second
		}
		if in.MaxResponseBytes == 0 {
			in.MaxResponseBytes = 1 << 20
		}
	}
}

// WithDefaults returns a copy whose Jenkins instances carry the per-instance defaults. A list element cannot carry an
// embedded default, so the rule lives here and is applied by Validate and New themselves: a loader that forgets to call
// it cannot produce a "discover must be declared or all" error for an empty (defaulted) value, and the caller's slice
// is never mutated.
func (c Config) WithDefaults() Config {
	c.Jenkins.Instances = append([]JenkinsInstance(nil), c.Jenkins.Instances...)
	c.Jenkins.ApplyInstanceDefaults()
	return c
}

func (c Config) JenkinsEnabled() bool { return len(c.Jenkins.Instances) > 0 }
func (c Config) SnykEnabled() bool    { return c.Snyk.Enabled }

var (
	instanceNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	uuidRE         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	apiVersionRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(~(beta|experimental))?$`)
	placeholderRE  = regexp.MustCompile(`\{[a-z_]+\}`)
)

// Validate returns every violation, with its key path.
func (c Config) Validate() error {
	c = c.WithDefaults()
	var errs []error
	add := func(path, msg string, a ...any) {
		errs = append(errs, fmt.Errorf("integrations.%s: %s", path, fmt.Sprintf(msg, a...)))
	}
	j := c.Jenkins
	if len(j.Instances) > 0 {
		if j.TTL < time.Minute {
			add("jenkins.ttl", "must be at least 1m")
		}
		if j.SweepTimeout <= 0 || j.SweepTimeout >= j.TTL {
			add("jenkins.sweep_timeout", "must be positive and shorter than ttl")
		}
		if j.Concurrency < 1 || j.Concurrency > 16 {
			add("jenkins.concurrency", "must be between 1 and 16")
		}
		if j.Pace < 0 || j.AbortAfter < 1 || j.NotFoundTTL < time.Minute {
			add("jenkins", "pace must be >= 0, abort_after >= 1, not_found_ttl >= 1m")
		}
		names := map[string]bool{}
		defaults := 0
		for i, in := range j.Instances {
			p := fmt.Sprintf("jenkins.instances[%d]", i)
			if !instanceNameRE.MatchString(in.Name) {
				add(p+".name", "%q does not match %s", in.Name, instanceNameRE)
			}
			if names[in.Name] {
				add(p+".name", "duplicate %q", in.Name)
			}
			names[in.Name] = true
			if in.Default {
				defaults++
			}
			if err := ValidateBaseURL(in.BaseURL, in.AllowHTTP); err != nil {
				add(p+".base_url", "%v", err)
			}
			if (in.User == "") != in.Token.IsZero() {
				add(p+".token", "user and token must be set together (an empty token is unset)")
			}
			if _, err := parseCIDRs(in.AllowPrivateCIDRs); err != nil {
				add(p+".allow_private_cidrs", "%v", err)
			}
			for _, port := range in.Ports {
				if port < 1 || port > 65535 {
					add(p+".ports", "invalid port %d", port)
				}
			}
			switch in.Discover {
			case "declared", "all":
			default:
				add(p+".discover", "must be declared or all")
			}
			switch in.Multibranch {
			case "auto", "always", "never":
			default:
				add(p+".multibranch", "must be auto, always or never")
			}
			if in.JobPrefix != "" {
				if _, err := ParseJobPath(in.JobPrefix + "x"); err != nil {
					add(p+".job_prefix", "%v", err)
				}
			}
			if in.Timeout < time.Second || in.Timeout > time.Minute || in.MaxResponseBytes < 1024 || in.MaxResponseBytes > 16<<20 {
				add(p, "timeout must be 1s..1m and max_response_bytes 1KiB..16MiB")
			}
		}
		if len(j.Instances) > 1 && defaults != 1 {
			add("jenkins.instances", "with several instances exactly one must set default: true (found %d)", defaults)
		}
		if len(j.Instances) == 1 {
			// a single instance is implicitly the default
			_ = defaults
		}
	}
	s := c.Snyk
	if s.Enabled {
		if s.Token.IsZero() {
			add("snyk.token", "required when snyk.enabled")
		}
		if !uuidRE.MatchString(s.OrgID) {
			add("snyk.org_id", "must be the organisation UUID")
		}
		u, err := url.Parse(s.BaseURL)
		switch {
		case err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "":
			add("snyk.base_url", "must be a bare https origin such as https://api.eu.snyk.io")
		case !s.AllowCustomHost && !contains(KnownSnykHosts, u.Hostname()):
			add("snyk.base_url", "host %q is not one of %v (set allow_custom_host to accept it); Snyk tokens are region-specific", u.Hostname(), KnownSnykHosts)
		}
		if !apiVersionRE.MatchString(s.APIVersion) {
			add("snyk.api_version", "must look like 2024-10-15")
		}
		switch s.FailOn {
		case "critical", "high", "medium", "low":
		default:
			add("snyk.fail_on", "must be critical, high, medium or low")
		}
		if s.TTL < time.Minute || s.SweepTimeout <= 0 || s.SweepTimeout >= s.TTL || s.Timeout < time.Second {
			add("snyk", "ttl >= 1m, 0 < sweep_timeout < ttl and timeout >= 1s are required")
		}
		if s.DetailURLTemplate != "" {
			for _, ph := range placeholderRE.FindAllString(s.DetailURLTemplate, -1) {
				if ph != "{project_id}" && ph != "{owner}" && ph != "{repo}" {
					add("snyk.detail_url_template", "unknown placeholder %s", ph)
				}
			}
			if u, err := url.Parse(strings.NewReplacer("{project_id}", "x", "{owner}", "x", "{repo}", "x").Replace(s.DetailURLTemplate)); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				add("snyk.detail_url_template", "must be an https URL without userinfo")
			}
		}
		if _, err := parseCIDRs(s.AllowPrivateCIDRs); err != nil {
			add("snyk.allow_private_cidrs", "%v", err)
		}
		for _, port := range s.Ports {
			if port < 1 || port > 65535 {
				add("snyk.ports", "invalid port %d", port)
			}
		}
		if s.MaxResponseBytes < 1024 || s.MaxResponseBytes > 64<<20 {
			add("snyk.max_response_bytes", "must be 1 KiB..64 MiB")
		}
	} else if !s.Token.IsZero() || s.OrgID != "" {
		add("snyk", "token or org_id set while enabled is false (the keys would be ignored)")
	}
	return errors.Join(errs...)
}

// ValidateBaseURL: https (http only with allowHTTP), host present, no userinfo, query or fragment.
func ValidateBaseURL(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q is not an absolute URL", raw)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && allowHTTP) {
		return fmt.Errorf("scheme must be https (http needs allow_http)")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("must not contain userinfo, query or fragment")
	}
	if strings.Contains(u.Path, "//") || strings.Contains(u.EscapedPath(), "%") {
		return fmt.Errorf("path must be plain (no empty or escaped segments)")
	}
	return nil
}

func parseCIDRs(in []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range in {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR", s)
		}
		if p.Bits() == 0 {
			return nil, fmt.Errorf("%q allows every address", s)
		}
		out = append(out, p)
	}
	return out, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// InstanceNames lists the configured Jenkins instance names. cmd/scp hands it to the docs descriptor loader so a
// descriptor can only select an instance that exists ("integrations may only name instances defined in portal config").
func (c Config) InstanceNames() []string {
	var out []string
	for _, in := range c.Jenkins.Instances {
		out = append(out, in.Name)
	}
	return out
}

// ValidateGroupDefaults checks the group-level `integrations` blocks (scm.GroupMeta.Integrations) against this
// configuration. catalog cannot import this package, so cmd/scp calls it from the root Validate. A key for a provider
// that is not configured is an error: a setting that would be silently ignored is a decorative key (GR-12).
func (c Config) ValidateGroupDefaults(groups []scm.GroupMeta) error {
	var errs []error
	add := func(g scm.GroupID, k, msg string, a ...any) {
		errs = append(errs, fmt.Errorf("catalog.groups[%s].integrations.%s: %s", g, k, fmt.Sprintf(msg, a...)))
	}
	names := c.InstanceNames()
	for _, g := range groups {
		for prov, kv := range g.Integrations {
			for k, v := range kv {
				path := prov + "." + k
				switch {
				case prov == ProviderJenkins && !c.JenkinsEnabled(), prov == ProviderSnyk && !c.SnykEnabled():
					add(g.ID, path, "provider %q is not configured", prov)
				case prov == ProviderJenkins && k == "job_prefix":
					if v != "" {
						if _, err := ParseJobPath(v + "x"); err != nil {
							add(g.ID, path, "%v", err)
						}
					}
				case prov == ProviderJenkins && k == "instance":
					if !contains(names, v) {
						add(g.ID, path, "unknown instance %q (configured: %v)", v, names)
					}
				case prov == ProviderSnyk && k == "project_prefix":
					if v != "" && !validSnykName(v) {
						add(g.ID, path, "invalid project prefix")
					}
				default:
					add(g.ID, path, "unknown integration key")
				}
			}
		}
	}
	return errors.Join(errs...)
}
