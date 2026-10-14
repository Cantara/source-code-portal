package ops

import (
	"fmt"
	"regexp"
	"time"
)

// ReadinessConfig is mounted at the top-level `readiness:` key.
type ReadinessConfig struct {
	WarmMaxAge      time.Duration `yaml:"warm_max_age"`     // 72h; >= 1m: a warm catalogue older than this does not open the catalogue gate
	MinCoverage     float64       `yaml:"min_coverage"`     // 0.9; (0, 1]
	CoreKinds       []string      `yaml:"core_kinds"`       // [commits]; item families that must reach MinCoverage; each must be a registered Kind.Family
	StartupDeadline time.Duration `yaml:"startup_deadline"` // 10m; 30s..24h: after it, a present catalogue is ready (degraded) whatever the coverage
}

// Config is mounted at the `ops:` key.
type Config struct {
	Liveness LivenessConfig `yaml:"liveness"`
	Pprof    PprofConfig    `yaml:"pprof"`
	Warnings WarningConfig  `yaml:"warnings"`
	Status   StatusConfig   `yaml:"status"`
}

type LivenessConfig struct {
	HeartbeatMax time.Duration `yaml:"heartbeat_max"` // 30s; 20s..10m: dispatcher heartbeat older than this fails /livez
	StuckMax     time.Duration `yaml:"stuck_max"`     // 2m; 10s..1h: an upstream with every slot stuck for this long fails /livez
	StartupGrace time.Duration `yaml:"startup_grace"` // 30s; 0..10m: a zero heartbeat is tolerated this long after process start
}

type PprofConfig struct {
	Enabled       bool          `yaml:"enabled"`        // true on the admin listener; false: the routes answer 404 feature-disabled
	MaxSeconds    time.Duration `yaml:"max_seconds"`    // 60s; 1s..10m: cap of ?seconds= on profile and trace; the write deadline is extended to match
	BlockRate     int           `yaml:"block_rate"`     // 0 (off); nanoseconds per sampled blocking event, applied by cmd/scp via PprofConfig.Apply
	MutexFraction int           `yaml:"mutex_fraction"` // 0 (off); 1 in n mutex contention events
}

type WarningConfig struct {
	RateLowRatio   float64       `yaml:"rate_low_ratio"`  // 0.10: remaining/limit below this is a warning (a fifth of it is critical)
	StaleFactor    float64       `yaml:"stale_factor"`    // 3: oldest item older than factor x TTL is a warning
	WebhookSilence time.Duration `yaml:"webhook_silence"` // 24h; 0 disables: no accepted delivery for this long, with webhooks enabled
}

type StatusConfig struct {
	MaxParkedShown int `yaml:"max_parked_shown"` // 200; 1..1000: parked keys listed in /status (the total is always shown)
}

func DefaultReadiness() ReadinessConfig {
	return ReadinessConfig{WarmMaxAge: 72 * time.Hour, MinCoverage: 0.9, CoreKinds: []string{"commits"}, StartupDeadline: 10 * time.Minute}
}

func DefaultConfig() Config {
	return Config{
		Liveness: LivenessConfig{HeartbeatMax: 30 * time.Second, StuckMax: 2 * time.Minute, StartupGrace: 30 * time.Second},
		Pprof:    PprofConfig{Enabled: true, MaxSeconds: 60 * time.Second},
		Warnings: WarningConfig{RateLowRatio: 0.10, StaleFactor: 3, WebhookSilence: 24 * time.Hour},
		Status:   StatusConfig{MaxParkedShown: 200},
	}
}

var familyRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (c ReadinessConfig) Validate(prefix string) []error {
	var errs []error
	if c.WarmMaxAge < time.Minute {
		errs = append(errs, fmt.Errorf("%s.warm_max_age: %s below 1m", prefix, c.WarmMaxAge))
	}
	if c.MinCoverage <= 0 || c.MinCoverage > 1 {
		errs = append(errs, fmt.Errorf("%s.min_coverage: %v outside (0, 1]", prefix, c.MinCoverage))
	}
	if len(c.CoreKinds) == 0 {
		errs = append(errs, fmt.Errorf("%s.core_kinds: at least one item family is required", prefix))
	}
	seen := map[string]bool{}
	for i, k := range c.CoreKinds {
		if !familyRE.MatchString(k) {
			errs = append(errs, fmt.Errorf("%s.core_kinds[%d]: %q is not an item family name", prefix, i, k))
		}
		if seen[k] {
			errs = append(errs, fmt.Errorf("%s.core_kinds[%d]: duplicate %q", prefix, i, k))
		}
		seen[k] = true
	}
	if c.StartupDeadline < 30*time.Second || c.StartupDeadline > 24*time.Hour {
		errs = append(errs, fmt.Errorf("%s.startup_deadline: %s outside 30s..24h", prefix, c.StartupDeadline))
	}
	return errs
}

func (c Config) Validate(prefix string) []error {
	var errs []error
	rng := func(key string, v, lo, hi time.Duration) {
		if v < lo || v > hi {
			errs = append(errs, fmt.Errorf("%s.%s: %s outside %s..%s", prefix, key, v, lo, hi))
		}
	}
	rng("liveness.heartbeat_max", c.Liveness.HeartbeatMax, 20*time.Second, 10*time.Minute) // the dispatcher wakes every 10 s (unit 04 Tunables.Heartbeat): below two beats /livez would flap
	rng("liveness.stuck_max", c.Liveness.StuckMax, 10*time.Second, time.Hour)
	rng("liveness.startup_grace", c.Liveness.StartupGrace, 0, 10*time.Minute)
	rng("pprof.max_seconds", c.Pprof.MaxSeconds, time.Second, 10*time.Minute)
	if c.Pprof.BlockRate < 0 || c.Pprof.MutexFraction < 0 {
		errs = append(errs, fmt.Errorf("%s.pprof: block_rate and mutex_fraction must be >= 0", prefix))
	}
	if c.Warnings.RateLowRatio <= 0 || c.Warnings.RateLowRatio >= 1 {
		errs = append(errs, fmt.Errorf("%s.warnings.rate_low_ratio: %v outside (0, 1)", prefix, c.Warnings.RateLowRatio))
	}
	if c.Warnings.StaleFactor < 1 {
		errs = append(errs, fmt.Errorf("%s.warnings.stale_factor: %v below 1", prefix, c.Warnings.StaleFactor))
	}
	if c.Warnings.WebhookSilence < 0 {
		errs = append(errs, fmt.Errorf("%s.warnings.webhook_silence: negative", prefix))
	}
	if c.Status.MaxParkedShown < 1 || c.Status.MaxParkedShown > 1000 {
		errs = append(errs, fmt.Errorf("%s.status.max_parked_shown: %d outside 1..1000", prefix, c.Status.MaxParkedShown))
	}
	return errs
}
