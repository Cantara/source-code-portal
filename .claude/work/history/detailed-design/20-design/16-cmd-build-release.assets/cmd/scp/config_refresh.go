package main

import "time"

// RefreshConfig is the `refresh:` section (04 section 6.1). The types live here, not in package refresh: refresh is rank 2 and
// imports no configuration machinery, and 04's task T3.3 puts them in cmd/scp/config.go. `refresh.gate.reserves.*` of the
// first draft of 04 does not exist: the reserve thresholds are `github.defaults.budget.reserve.*` of unit 05 (deviation D-7).
type RefreshConfig struct {
	Upstreams   map[string]UpstreamSettings `yaml:"upstreams" jsonschema_description:"Per-upstream concurrency, pacing and job timeout. Every upstream a job kind names must be listed (startup refuses an unknown one); environment variables can change existing entries only."`
	Backoff     BackoffSettings             `yaml:"backoff" jsonschema_description:"Retry backoff of failed jobs: full jitter between base and min(cap, base*2^attempt)."`
	Grace       time.Duration               `yaml:"grace" scp:"min=1s,max=1m" jsonschema_description:"How long in-flight jobs may finish after shutdown begins; they are canceled after that. Also sizes the refresh stage's stop timeout (grace plus 4 s)."`
	PlannerTick time.Duration               `yaml:"planner_tick" scp:"min=1s,max=5m" jsonschema_description:"Interval of the planner's scan for due items (the planner.tick task)."`
	MaxKeys     int                         `yaml:"max_keys" jsonschema:"minimum=1000" jsonschema_description:"Cap of the engine's keyed table (queued plus running plus parked keys); an Enqueue beyond it fails with job.ErrQueueFull."`
	Parking     ParkingSettings             `yaml:"parking" jsonschema_description:"Poison-key parking."`
}

// UpstreamSettings is refresh.UpstreamConfig as configuration.
type UpstreamSettings struct {
	Concurrency int           `yaml:"concurrency" jsonschema:"minimum=1,maximum=64" jsonschema_description:"Jobs of this upstream that may run at once."`
	Pace        time.Duration `yaml:"pace" scp:"min=0s,max=10s" jsonschema_description:"Minimum spacing between job starts of this upstream; 0 means none."`
	JobTimeout  time.Duration `yaml:"job_timeout" scp:"min=1s,max=30m" jsonschema_description:"Default per-run deadline of a job of this upstream when its spec sets none."`
}

// BackoffSettings is refresh.Backoff as configuration.
type BackoffSettings struct {
	Base        time.Duration `yaml:"base" scp:"min=100ms,max=1h" jsonschema_description:"First retry delay."`
	Cap         time.Duration `yaml:"cap" scp:"min=1s,max=24h" jsonschema_description:"Upper bound of a retry delay."`
	MaxAttempts int           `yaml:"max_attempts" jsonschema:"minimum=1,maximum=100" jsonschema_description:"Attempts after which the job is reported exhausted once; retries then continue at the cap."`
}

// Validate is the cross-field rule of the section.
func (b BackoffSettings) Validate(report func(path, msg string)) {
	if b.Cap < b.Base {
		report("cap", "must not be smaller than base")
	}
}

// ParkingSettings is the poison-key policy.
type ParkingSettings struct {
	Retry time.Duration `yaml:"retry" scp:"min=1m,max=720h" jsonschema_description:"Interval of the single retry probe of a parked key."`
}
