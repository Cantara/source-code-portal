package badges

import (
	"fmt"
	"time"
)

// Config is the badges section of the root configuration (yaml key "badges").
type Config struct {
	Palette     string        `yaml:"palette"`       // "aa" (white text >= 4.5:1) or "aaa" (>= 7:1)
	CacheMaxAge time.Duration `yaml:"cache_max_age"` // max-age of badges that carry data; 0 means no-cache; at most 1h
}

func DefaultConfig() Config { return Config{Palette: "aa", CacheMaxAge: 5 * time.Minute} }

func (c Config) Validate(path string) []error {
	var errs []error
	if _, ok := parsePalette(c.Palette); !ok {
		errs = append(errs, fmt.Errorf("%s.palette: must be \"aa\" or \"aaa\", got %q", path, c.Palette))
	}
	if c.CacheMaxAge < 0 || c.CacheMaxAge > time.Hour {
		errs = append(errs, fmt.Errorf("%s.cache_max_age: must be between 0s and 1h, got %s", path, c.CacheMaxAge))
	}
	return errs
}
