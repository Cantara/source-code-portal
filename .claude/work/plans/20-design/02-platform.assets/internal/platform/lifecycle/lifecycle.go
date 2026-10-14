// Package lifecycle runs an ordered plan of long-lived stages and stops it in the reverse order (rank 0, stdlib only).
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// DefaultStopTimeout applies to a Stage whose StopTimeout is zero.
const DefaultStopTimeout = 10 * time.Second

// Phases reported to Plan.Observe and carried by StageError.
const (
	PhaseRun   = "run"
	PhaseDrain = "drain"
	PhaseStop  = "stop"
	PhaseClose = "close"
)

var (
	// ErrStopTimeout: a stage's Run (or Close, or Drain) did not return within its StopTimeout.
	ErrStopTimeout = errors.New("stage did not stop in time")
	// ErrStageExited: a stage's Run returned nil while the process was not shutting down.
	ErrStageExited = errors.New("stage exited before shutdown")
)

// StageError names the stage and phase of a failure.
type StageError struct {
	Stage string
	Phase string
	Err   error
}

func (e *StageError) Error() string { return fmt.Sprintf("stage %s %s: %v", e.Stage, e.Phase, e.Err) }
func (e *StageError) Unwrap() error { return e.Err }

// PanicError wraps a recovered panic of a stage function.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Stage is one long-lived part of the process.
type Stage struct {
	Name string
	// Run is the stage's blocking worker. It must return when its context is cancelled. nil = no worker.
	Run func(ctx context.Context) error
	// Drain runs first at shutdown, in reverse stage order, while everything still serves (readiness off, SSE told to wait).
	Drain func(ctx context.Context) error
	// Close runs after Run has returned (never before), in reverse stage order.
	Close func(ctx context.Context) error
	// StopTimeout bounds waiting for Run to return and each of Drain and Close. Zero means DefaultStopTimeout.
	StopTimeout time.Duration
}

// Plan lists stages in START order; they are stopped in the reverse order.
type Plan struct {
	Stages     []Stage
	DrainDelay time.Duration // still serving, after every Drain and before the first stage is cancelled
	Log        *slog.Logger  // nil = discard
	// OnShutdown is called once, as shutdown begins, with its cause (cmd/scp passes the signal stop function so a second signal kills).
	OnShutdown func(cause error)
	// Observe is called after every phase of every stage (cmd/scp turns it into a metric).
	Observe func(stage, phase string, d time.Duration, err error)
}

type exit struct {
	i   int
	err error
}

type stageState struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // valid after done is closed
}

// Run starts every stage with a Run func, waits for the root context to end or for a stage to fail, then shuts down in order:
// all Drain hooks (reverse), DrainDelay, then per stage in reverse: cancel, wait for Run (StopTimeout), Close.
// The root context never reaches a stage directly: each stage gets WithoutCancel(root) plus a cancel only Run holds.
// It returns nil only when every stage stopped cleanly; a failed stage's error comes first.
func Run(root context.Context, p Plan) error {
	log := p.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	seen := map[string]bool{}
	for _, s := range p.Stages {
		if s.Name == "" || seen[s.Name] {
			return fmt.Errorf("lifecycle: stage name %q is empty or duplicated", s.Name)
		}
		seen[s.Name] = true
	}
	observe := func(stage, phase string, start time.Time, err error) {
		if p.Observe != nil {
			p.Observe(stage, phase, time.Since(start), err)
		}
	}
	var (
		mu       sync.Mutex
		stopping bool
	)
	exits := make(chan exit, len(p.Stages))
	states := make([]*stageState, len(p.Stages))
	for i := range p.Stages {
		st := p.Stages[i]
		if st.Run == nil {
			continue
		}
		ctx, cancel := context.WithCancel(context.WithoutCancel(root))
		s := &stageState{cancel: cancel, done: make(chan struct{})}
		states[i] = s
		go func() {
			err := call(st.Run, ctx)
			s.err = err
			close(s.done)
			mu.Lock()
			early := !stopping
			mu.Unlock()
			if early {
				if err == nil {
					err = ErrStageExited
				}
				exits <- exit{i, err}
			}
		}()
		log.Info("stage started", "stage", st.Name)
	}

	var errs []error
	failed := -1
	var cause error
	select {
	case <-root.Done():
		cause = context.Cause(root)
	case e := <-exits:
		failed, cause = e.i, e.err
		errs = append(errs, &StageError{p.Stages[e.i].Name, PhaseRun, e.err})
	}
	mu.Lock()
	stopping = true
	mu.Unlock()
	log.Info("shutdown begins", "cause", cause)
	if p.OnShutdown != nil {
		p.OnShutdown(cause)
	}

	hook := func(st Stage, phase string, f func(context.Context) error) {
		if f == nil {
			return
		}
		start := time.Now()
		to := stopTimeout(st)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(root), to)
		defer cancel()
		err := within(ctx, to, f)
		observe(st.Name, phase, start, err)
		if err != nil {
			errs = append(errs, &StageError{st.Name, phase, err})
		}
	}
	for i := len(p.Stages) - 1; i >= 0; i-- {
		hook(p.Stages[i], PhaseDrain, p.Stages[i].Drain)
	}
	if p.DrainDelay > 0 {
		time.Sleep(p.DrainDelay)
	}
	for i := len(p.Stages) - 1; i >= 0; i-- {
		st, s := p.Stages[i], states[i]
		if s != nil {
			start := time.Now()
			s.cancel()
			var err error
			if i != failed { // the failed stage has already returned
				select {
				case <-s.done:
					if s.err != nil && !errors.Is(s.err, context.Canceled) {
						err = s.err
					}
				case <-time.After(stopTimeout(st)):
					err = ErrStopTimeout
				}
			}
			observe(st.Name, PhaseStop, start, err)
			if err != nil {
				errs = append(errs, &StageError{st.Name, PhaseStop, err})
				if errors.Is(err, ErrStopTimeout) {
					log.Error("stage did not stop; its Close is skipped", "stage", st.Name)
					continue // closing resources under a live worker is the race this package prevents
				}
			}
		}
		hook(st, PhaseClose, st.Close)
	}
	log.Info("shutdown complete")
	return errors.Join(errs...)
}

func stopTimeout(s Stage) time.Duration {
	if s.StopTimeout > 0 {
		return s.StopTimeout
	}
	return DefaultStopTimeout
}

// call runs f and converts a panic into *PanicError.
func call(f func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	return f(ctx)
}

// within runs a hook and gives up waiting after d (the hook keeps running; the process is exiting).
func within(ctx context.Context, d time.Duration, f func(context.Context) error) error {
	ch := make(chan error, 1)
	go func() { ch <- call(f, ctx) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(d):
		return ErrStopTimeout
	}
}

// Config is the "shutdown" section of the config file. Platform owns it.
type Config struct {
	DrainDelay       time.Duration `yaml:"drain_delay" scp:"min=0s,max=2m" jsonschema_description:"After SIGTERM, /readyz turns 503 and the process keeps serving for this long so load balancers stop sending traffic."`
	ServerTimeout    time.Duration `yaml:"server_timeout" scp:"min=1s,max=5m" jsonschema_description:"Grace for in-flight HTTP requests; the public and admin listeners stop together within it."`
	StoreTimeout     time.Duration `yaml:"store_timeout" scp:"min=1s,max=5m" jsonschema_description:"Grace for the store to flush planner state and close."`
	TelemetryTimeout time.Duration `yaml:"telemetry_timeout" scp:"min=1s,max=1m" jsonschema_description:"Grace for the final metric and trace flush."`
}

// WorstCase is the longest a complete shutdown can take when every phase runs into its limit: the drain delay, the servers'
// grace, the store, the telemetry flush, plus the stop timeouts of the stages this section does not own (extra, for example the
// refresh stage's Grace + AbandonAfter + 1 s). cmd/scp logs it at startup and warns when it exceeds the orchestrator's
// termination grace period (45 s in the reference manifest), after which the process would be killed mid-shutdown.
func (c Config) WorstCase(extra ...time.Duration) time.Duration {
	d := c.DrainDelay + c.ServerTimeout + c.StoreTimeout + c.TelemetryTimeout
	for _, e := range extra {
		d += e
	}
	return d
}

// Validate implements config.Validator.
func (c Config) Validate(report func(path, msg string)) {
	if c.DrainDelay >= c.ServerTimeout+c.StoreTimeout+c.TelemetryTimeout {
		report("drain_delay", "should be well below the sum of the stage timeouts or the orchestrator will SIGKILL first")
	}
}
