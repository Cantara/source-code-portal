// Package config loads and validates the configuration of SCP: built-in defaults, then a YAML file, then SCP_* environment
// variables, then command-line flags. Every key belongs to the Config struct; unknown keys are refused.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config is the typed configuration. A key is added here together with the code that reads it.
type Config struct {
	Server  Server  `mapstructure:"server"`
	Log     Log     `mapstructure:"log"`
	Store   Store   `mapstructure:"store"`
	Sync    Sync    `mapstructure:"sync"`
	GitHub  GitHub  `mapstructure:"github"`
	Catalog Catalog `mapstructure:"catalog"`
	Webhook Webhook `mapstructure:"webhook"`
	Access  Access  `mapstructure:"access"`
	Cluster Cluster `mapstructure:"cluster"`
	Web     Web     `mapstructure:"web"`
	Ops     Ops     `mapstructure:"ops"`
}

// Server configures the two listeners.
type Server struct {
	PublicAddr    string        `mapstructure:"public_addr"`    // web UI, API, badges, llms, webhook
	AdminAddr     string        `mapstructure:"admin_addr"`     // probes, metrics, pprof, status
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace"` // time in-flight work gets after the first signal
	// PublicURL is the absolute base of the public listener (no trailing slash): absolute links in llms and Markdown, HSTS when https.
	PublicURL string `mapstructure:"public_url"`
	// RawTrustedProxies are CIDRs allowed to set X-Forwarded-For; TrustedProxies holds them parsed.
	RawTrustedProxies []string       `mapstructure:"trusted_proxies"`
	TrustedProxies    []netip.Prefix `mapstructure:"-"`
}

// Ops configures operator tooling on the admin listener.
type Ops struct {
	Pprof bool `mapstructure:"pprof"` // serve /debug/pprof/
}

// Log configures the process logger.
type Log struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, or text for local development
}

// defaults holds every key with its built-in value; it is also the list of keys the environment may set.
func defaults() map[string]any {
	return map[string]any{
		"server.public_addr":           ":9090",
		"server.admin_addr":            ":9091",
		"server.shutdown_grace":        "20s",
		"server.public_url":            "",
		"server.trusted_proxies":       []string{},
		"ops.pprof":                    false,
		"log.level":                    "info",
		"log.format":                   "json",
		"store.path":                   "/data/scp.db",
		"sync.concurrency":             4,
		"catalog.reconcile_interval":   "5m",
		"webhook.secret_file":          "",
		"webhook.previous_secret_file": "",
		"access.user_header":           "X-Forwarded-User",
		"access.groups_header":         "X-Forwarded-Groups",
		"access.private_groups":        []string{},
		"cluster.addresses":            []string{},
		"cluster.name":                 "dev",
		"cluster.namespace":            "scp",
		"web.dir":                      "",
		"web.node":                     "node",
	}
}

// flagKeys maps command-line flag names to the keys they set.
func flagKeys() map[string]string {
	return map[string]string{
		"log-level":   "log.level",
		"public-addr": "server.public_addr",
		"admin-addr":  "server.admin_addr",
	}
}

// EnvPrefix is the prefix of environment overrides: server.public_addr is SCP_SERVER_PUBLIC_ADDR.
const EnvPrefix = "SCP"

// Invalid reports refused input: unknown keys, wrong types or values that fail validation. It lists every problem.
type Invalid struct{ Problems []string }

func (e *Invalid) Error() string {
	return "invalid configuration:\n  " + strings.Join(e.Problems, "\n  ")
}

// Load reads the configuration. path may be empty (defaults and environment only); flags may be nil.
// It returns the typed config and the effective settings for display. A *Invalid error means refused input;
// any other error means the file could not be read.
func Load(path string, flags *pflag.FlagSet) (Config, map[string]any, error) {
	v := viper.New()
	for k, val := range defaults() {
		v.SetDefault(k, val)
	}
	if path != "" {
		v.SetConfigFile(path)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			var parse viper.ConfigParseError
			if errors.As(err, &parse) {
				return Config{}, nil, &Invalid{Problems: []string{err.Error()}}
			}
			return Config{}, nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if flags != nil {
		for name, key := range flagKeys() {
			if f := flags.Lookup(name); f != nil && f.Changed {
				if err := v.BindPFlag(key, f); err != nil {
					return Config{}, nil, err
				}
			}
		}
	}

	var problems []string
	problems = append(problems, unknownEnv(os.Environ())...)
	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		problems = append(problems, err.Error())
	} else {
		cfg.normalise()
		problems = append(problems, cfg.validate()...)
		problems = append(problems, cfg.readSecrets()...)
	}
	if len(problems) > 0 {
		return Config{}, nil, &Invalid{Problems: problems}
	}
	return cfg, v.AllSettings(), nil
}

// unknownEnv reports SCP_* variables that name no key, so an environment typo fails like a file typo.
func unknownEnv(environ []string) []string {
	known := map[string]bool{}
	for k := range defaults() {
		known[EnvPrefix+"_"+strings.ToUpper(strings.ReplaceAll(k, ".", "_"))] = true
	}
	var out []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, EnvPrefix+"_") && !known[name] {
			out = append(out, fmt.Sprintf("%s: environment variable names no configuration key", name))
		}
	}
	slices.Sort(out)
	return out
}

func (c Config) validate() []string {
	var p []string
	for key, addr := range map[string]string{"server.public_addr": c.Server.PublicAddr, "server.admin_addr": c.Server.AdminAddr} {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			p = append(p, fmt.Sprintf("%s: %q is not host:port", key, addr))
		}
	}
	if c.Server.PublicAddr == c.Server.AdminAddr {
		p = append(p, "server.admin_addr: must differ from server.public_addr")
	}
	if c.Server.PublicURL != "" {
		if u, err := url.Parse(c.Server.PublicURL); err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" ||
			u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			p = append(p, fmt.Sprintf("server.public_url: %q is not an absolute http(s) URL without query", c.Server.PublicURL))
		}
	}
	for _, cidr := range c.Server.RawTrustedProxies {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			p = append(p, fmt.Sprintf("server.trusted_proxies: %q is not a CIDR", cidr))
		}
	}
	if c.Server.ShutdownGrace <= 0 {
		p = append(p, "server.shutdown_grace: must be positive")
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		p = append(p, fmt.Sprintf("log.level: %q is not one of debug, info, warn, error", c.Log.Level))
	}
	if !slices.Contains([]string{"json", "text"}, c.Log.Format) {
		p = append(p, fmt.Sprintf("log.format: %q is not one of json, text", c.Log.Format))
	}
	if c.Store.Path == "" {
		p = append(p, "store.path: required")
	}
	if c.Sync.Concurrency < 1 || c.Sync.Concurrency > 64 {
		p = append(p, "sync.concurrency: must be 1..64")
	}
	p = append(p, c.GitHub.validate()...)
	p = append(p, c.Catalog.validate(c.GitHub)...)
	if c.Webhook.PreviousSecretFile != "" && c.Webhook.SecretFile == "" {
		p = append(p, "webhook.previous_secret_file: needs webhook.secret_file")
	}
	p = append(p, c.Access.validate(c.GitHub, len(c.Server.TrustedProxies) > 0)...)
	p = append(p, c.Cluster.validate()...)
	p = append(p, c.Web.validate()...)
	slices.Sort(p)
	return p
}
