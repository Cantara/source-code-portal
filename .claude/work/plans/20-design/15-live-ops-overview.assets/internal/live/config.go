// Package live serves GET /api/v1/snapshot (the version poll) and GET /api/v1/events (the SSE invalidation stream).
package live

import (
	"fmt"
	"time"
)

// Config is mounted at the `live:` key of the root configuration. The zero value is invalid: use DefaultConfig.
type Config struct {
	SSE SSEConfig `yaml:"sse"`
}

type SSEConfig struct {
	Enabled      bool          `yaml:"enabled"`       // false: GET /api/v1/events answers 404 feature-disabled
	Debounce     time.Duration `yaml:"debounce"`      // 300ms; 50ms..5s: coalescing window between a publish and the invalidate event
	Heartbeat    time.Duration `yaml:"heartbeat"`     // 15s; 5s..60s: comment line cadence (below every proxy read timeout)
	WriteTimeout time.Duration `yaml:"write_timeout"` // 10s; 1s..60s: per-write deadline, re-armed before every write
	Retry        time.Duration `yaml:"retry"`         // 3s; 1s..60s: reconnect delay announced with `retry:`
	DrainRetry   time.Duration `yaml:"drain_retry"`   // 30s; 5s..10m: reconnect delay announced when the server shuts down
	MaxLifetime  time.Duration `yaml:"max_lifetime"`  // 30m; 1m..24h (jittered +-10%): a stream is closed after this age, the client reconnects
	MaxClients   int           `yaml:"max_clients"`   // 1000; 1..100000: concurrent streams in the process
}

func DefaultConfig() Config {
	return Config{SSE: SSEConfig{Debounce: 300 * time.Millisecond, Heartbeat: 15 * time.Second, WriteTimeout: 10 * time.Second,
		Retry: 3 * time.Second, DrainRetry: 30 * time.Second, MaxLifetime: 30 * time.Minute, MaxClients: 1000}}
}

// Validate collects every violation with a path-accurate key (prefix is "live").
func (c Config) Validate(prefix string) []error {
	var errs []error
	dur := func(key string, v, lo, hi time.Duration) {
		if v < lo || v > hi {
			errs = append(errs, fmt.Errorf("%s.sse.%s: %s outside %s..%s", prefix, key, v, lo, hi))
		}
	}
	num := func(key string, v, lo, hi int) {
		if v < lo || v > hi {
			errs = append(errs, fmt.Errorf("%s.sse.%s: %d outside %d..%d", prefix, key, v, lo, hi))
		}
	}
	s := c.SSE
	dur("debounce", s.Debounce, 50*time.Millisecond, 5*time.Second)
	dur("heartbeat", s.Heartbeat, 5*time.Second, 60*time.Second)
	dur("write_timeout", s.WriteTimeout, time.Second, 60*time.Second)
	dur("retry", s.Retry, time.Second, 60*time.Second)
	dur("drain_retry", s.DrainRetry, 5*time.Second, 10*time.Minute)
	dur("max_lifetime", s.MaxLifetime, time.Minute, 24*time.Hour)
	num("max_clients", s.MaxClients, 1, 100000)
	return errs
}
