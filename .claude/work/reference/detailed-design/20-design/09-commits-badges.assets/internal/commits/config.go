package commits

import (
	"fmt"
	"time"
)

// Config is the commits section of the root configuration (yaml key "commits").
type Config struct {
	Keep           int           `yaml:"keep"`             // commits retained per repository (ring size), 1..100
	LatestN        int           `yaml:"latest_n"`         // length of the precomputed dashboard feeds, 1..200
	RefreshTTL     time.Duration `yaml:"refresh_ttl"`      // drift bound of commits.refresh, 1m..24h
	IdleBackoffMax time.Duration `yaml:"idle_backoff_max"` // cap of the idle back-off, RefreshTTL..168h
}

func DefaultConfig() Config {
	return Config{Keep: 30, LatestN: 50, RefreshTTL: 2 * time.Hour, IdleBackoffMax: 12 * time.Hour}
}

// Validate returns every violation, each prefixed with the path of the section (for example "commits").
func (c Config) Validate(path string) []error {
	var errs []error
	if c.Keep < 1 || c.Keep > 100 {
		errs = append(errs, fmt.Errorf("%s.keep: must be between 1 and 100 (GitHub page size), got %d", path, c.Keep))
	}
	if c.LatestN < 1 || c.LatestN > 200 {
		errs = append(errs, fmt.Errorf("%s.latest_n: must be between 1 and 200, got %d", path, c.LatestN))
	}
	if c.RefreshTTL < time.Minute || c.RefreshTTL > 24*time.Hour {
		errs = append(errs, fmt.Errorf("%s.refresh_ttl: must be between 1m and 24h, got %s", path, c.RefreshTTL))
	}
	if c.IdleBackoffMax < c.RefreshTTL || c.IdleBackoffMax > 168*time.Hour {
		errs = append(errs, fmt.Errorf("%s.idle_backoff_max: must be between refresh_ttl (%s) and 168h, got %s", path, c.RefreshTTL, c.IdleBackoffMax))
	}
	return errs
}
