package media

import (
	"fmt"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// ExternalSVG policy values.
const (
	SVGDeny      = "deny"      // architecture 11.3 as written: external SVG is never served
	SVGSandboxed = "sandboxed" // proposed deviation D-1: served as an image only, under a sandbox CSP
)

// Config is the media section of the root configuration.
type Config struct {
	SigningKey    secret.Secret   `yaml:"signing_key"`    // HMAC key, at least 32 bytes; empty = ephemeral key (development only)
	PreviousKeys  []secret.Secret `yaml:"previous_keys"`  // keys still accepted for verification during a rotation
	AllowHosts    []string        `yaml:"allow_hosts"`    // exact hosts or "*.suffix"; empty = every public host
	MaxBytes      int64           `yaml:"max_bytes"`      // body cap of any image
	FetchTimeout  time.Duration   `yaml:"fetch_timeout"`  // total deadline of one external fetch including redirects
	CacheBytes    int64           `yaml:"cache_bytes"`    // in-memory cache, bytes
	CacheTTL      time.Duration   `yaml:"cache_ttl"`      // freshness of a cached external image
	NegativeTTL   time.Duration   `yaml:"negative_ttl"`   // how long a failed external fetch is remembered
	MaxConcurrent int             `yaml:"max_concurrent"` // simultaneous external fetches
	ExternalSVG   string          `yaml:"external_svg"`   // deny | sandboxed
	MaxSVGBytes   int64           `yaml:"max_svg_bytes"`  // body cap of an SVG
}

// MaxURLLength is the longest external URL that can be signed.
const MaxURLLength = 2000

// DefaultConfig returns the shipped defaults.
func DefaultConfig() Config {
	return Config{MaxBytes: 2 << 20, FetchTimeout: 5 * time.Second, CacheBytes: 64 << 20, CacheTTL: time.Hour, NegativeTTL: 5 * time.Minute,
		MaxConcurrent: 16, ExternalSVG: SVGDeny, MaxSVGBytes: 256 << 10}
}

// Validate returns every violation with its path.
func (c Config) Validate(prefix string) []error {
	var errs []error
	bad := func(key, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s.%s: %s", prefix, key, fmt.Sprintf(f, a...)))
	}
	if !c.SigningKey.IsZero() && len(c.SigningKey.Reveal()) < 32 {
		bad("signing_key", "must be at least 32 bytes")
	}
	for i, k := range c.PreviousKeys {
		if k.IsZero() || len(k.Reveal()) < 32 {
			bad(fmt.Sprintf("previous_keys[%d]", i), "must be at least 32 bytes")
		}
	}
	for i, h := range c.AllowHosts {
		if !validAllowHost(h) {
			bad(fmt.Sprintf("allow_hosts[%d]", i), "must be a lower-case host name or *.suffix, got %q", h)
		}
	}
	if c.MaxBytes < 16<<10 || c.MaxBytes > 16<<20 {
		bad("max_bytes", "must be between 16 KiB and 16 MiB, got %d", c.MaxBytes)
	}
	if c.FetchTimeout < time.Second || c.FetchTimeout > 30*time.Second {
		bad("fetch_timeout", "must be between 1s and 30s, got %s", c.FetchTimeout)
	}
	if c.CacheBytes < 1<<20 || c.CacheBytes > 4<<30 {
		bad("cache_bytes", "must be between 1 MiB and 4 GiB, got %d", c.CacheBytes)
	}
	if c.CacheTTL < time.Minute || c.CacheTTL > 24*time.Hour {
		bad("cache_ttl", "must be between 1m and 24h, got %s", c.CacheTTL)
	}
	if c.NegativeTTL < 10*time.Second || c.NegativeTTL > time.Hour {
		bad("negative_ttl", "must be between 10s and 1h, got %s", c.NegativeTTL)
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 256 {
		bad("max_concurrent", "must be between 1 and 256, got %d", c.MaxConcurrent)
	}
	if c.ExternalSVG != SVGDeny && c.ExternalSVG != SVGSandboxed {
		bad("external_svg", "must be %q or %q, got %q", SVGDeny, SVGSandboxed, c.ExternalSVG)
	}
	if c.MaxSVGBytes < 1<<10 || c.MaxSVGBytes > c.MaxBytes {
		bad("max_svg_bytes", "must be between 1 KiB and max_bytes, got %d", c.MaxSVGBytes)
	}
	return errs
}

func validAllowHost(h string) bool {
	h = strings.TrimPrefix(h, "*.")
	return validHostName(h)
}
