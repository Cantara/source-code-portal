// Package config loads and validates the configuration of SCP: built-in defaults, then a YAML file, then SCP_* environment
// variables, then command-line flags. Every key belongs to the Config struct; unknown keys are refused.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config is the typed configuration. A key is added here together with the code that reads it.
type Config struct {
	Server Server `mapstructure:"server"`
	Log    Log    `mapstructure:"log"`
}

// Server configures the two listeners.
type Server struct {
	PublicAddr    string        `mapstructure:"public_addr"`    // SPA, API, badges, llms, webhook
	AdminAddr     string        `mapstructure:"admin_addr"`     // probes, metrics, pprof, status
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace"` // time in-flight work gets after the first signal
}

// Log configures the process logger.
type Log struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, or text for local development
}

// defaults holds every key with its built-in value; it is also the list of keys the environment may set.
func defaults() map[string]any {
	return map[string]any{
		"server.public_addr":    ":9090",
		"server.admin_addr":     ":9091",
		"server.shutdown_grace": "20s",
		"log.level":             "info",
		"log.format":            "json",
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
		problems = append(problems, cfg.validate()...)
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
	if c.Server.ShutdownGrace <= 0 {
		p = append(p, "server.shutdown_grace: must be positive")
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		p = append(p, fmt.Sprintf("log.level: %q is not one of debug, info, warn, error", c.Log.Level))
	}
	if !slices.Contains([]string{"json", "text"}, c.Log.Format) {
		p = append(p, fmt.Sprintf("log.format: %q is not one of json, text", c.Log.Format))
	}
	slices.Sort(p)
	return p
}
