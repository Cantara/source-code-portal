// Package reactor fans one normalised scm.Event out to the reaction functions of every feature, synchronously.
// It is the only package besides the features that knows both job and scm.
package reactor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Func is one feature's reaction to an event. Contract: a cheap pure decision plus non-blocking Enqueue calls; no I/O,
// no waiting, no retention of the event or its slices; safe for concurrent use (webhook goroutines and the catalog
// head sweep call it at the same time).
type Func func(ctx context.Context, ev scm.Event, enq job.Enqueuer) error

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,31}$`)

// Reactor is a named Func. The name labels logs and metrics.
type Reactor struct {
	Name  string // ^[a-z][a-z0-9.-]{0,31}$ (a metric label), e.g. "commits", "docs", "manifests", "catalog"
	React Func
}

// Outcome classifies one reactor run for Observer.
type Outcome string

const (
	OutcomeOK      Outcome = "ok"
	OutcomeRefused Outcome = "refused" // job.ErrQueueFull or job.ErrClosed: the webhook answers 503
	OutcomeError   Outcome = "error"
	OutcomePanic   Outcome = "panic"
)

// Observation is reported once per reactor run.
type Observation struct {
	Reactor  string
	Kind     scm.EventKind
	Outcome  Outcome
	Duration time.Duration
}

// Deps are the optional collaborators of a Fan.
type Deps struct {
	Log     *slog.Logger      // nil: discard
	Observe func(Observation) // nil: none. Must be cheap and non-blocking (cmd/scp wires it to metrics)
}

// PanicError is the error a recovered reactor panic becomes. The stack goes to the log only.
type PanicError struct {
	Reactor string
	Value   any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("reactor %s panicked: %v", e.Reactor, e.Value)
}

// Fan is an immutable, concurrency-safe fan-out. It starts no goroutine.
type Fan struct {
	rs  []Reactor
	enq job.Enqueuer
	log *slog.Logger
	obs func(Observation)
}

// New builds a Fan from anonymous reactors, named "reactor-0", "reactor-1", ... in registration order. It panics
// on a nil enqueuer or a nil Func (a wiring error at startup).
func New(enq job.Enqueuer, d Deps, rs ...Func) *Fan {
	named := make([]Reactor, len(rs))
	for i, f := range rs {
		named[i] = Reactor{Name: "reactor-" + strconv.Itoa(i), React: f}
	}
	return NewNamed(enq, d, named...)
}

// NewNamed builds a Fan from named reactors; this is the constructor cmd/scp uses.
func NewNamed(enq job.Enqueuer, d Deps, rs ...Reactor) *Fan {
	if enq == nil {
		panic("reactor: nil enqueuer")
	}
	for _, r := range rs {
		if r.React == nil || !nameRE.MatchString(r.Name) {
			panic("reactor: nil reactor or a name outside " + nameRE.String())
		}
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Fan{rs: append([]Reactor(nil), rs...), enq: enq, log: log, obs: d.Observe}
}

// Handle first rejects an event whose non-zero Repo fails RepoRef.Validate (no reactor runs; the error matches
// scm.ErrInvalid and is not a refusal). It then runs every reactor in registration order on the caller's goroutine
// and returns errors.Join of their errors, each wrapped with the reactor's name. A failing or panicking reactor never stops the others. Reactors
// receive ctx's values but not its cancellation (a client that hangs up must not leave a fan-out half done).
// The caller (the webhook) answers 503 only when job.IsRefusal(err); any other error is logged here and the
// delivery is still accepted, because events are hints.
func (f *Fan) Handle(ctx context.Context, ev scm.Event) error {
	ctx = context.WithoutCancel(ctx)
	// Defence in depth: reactors build job keys from ev.Repo and may assume it is well-formed. The decoder validates
	// first; a malformed ref that reaches here is refused as a whole (not a refusal in the 503 sense: the delivery is
	// still accepted, because a hostile or broken payload will not improve on redelivery).
	if !ev.Repo.IsZero() {
		if err := ev.Repo.Validate(); err != nil {
			f.log.LogAttrs(ctx, slog.LevelWarn, "event with an invalid repository dropped", slog.Any("event", ev), slog.String("err", err.Error()))
			return fmt.Errorf("reactor: event dropped: %w", err)
		}
	}
	var errs []error
	for _, r := range f.rs {
		if err := f.run(ctx, r, ev); err != nil {
			errs = append(errs, fmt.Errorf("reactor %s: %w", r.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (f *Fan) run(ctx context.Context, r Reactor, ev scm.Event) (err error) {
	start := time.Now()
	outcome := OutcomeOK
	defer func() {
		if v := recover(); v != nil {
			outcome = OutcomePanic
			err = &PanicError{Reactor: r.Name, Value: v}
			f.log.LogAttrs(ctx, slog.LevelError, "reactor panicked", slog.String("reactor", r.Name),
				slog.Any("event", ev), slog.Any("panic", v), slog.String("stack", string(debug.Stack())))
		} else if err != nil {
			if job.IsRefusal(err) {
				outcome = OutcomeRefused
				f.log.LogAttrs(ctx, slog.LevelWarn, "reactor refused by engine", slog.String("reactor", r.Name),
					slog.Any("event", ev), slog.String("err", err.Error()))
			} else {
				outcome = OutcomeError
				f.log.LogAttrs(ctx, slog.LevelError, "reactor failed", slog.String("reactor", r.Name),
					slog.Any("event", ev), slog.String("err", err.Error()))
			}
		}
		f.observe(ctx, Observation{Reactor: r.Name, Kind: ev.Kind, Outcome: outcome, Duration: time.Since(start)})
	}()
	return r.React(ctx, ev, f.enq)
}

// observe calls the observer; a panicking observer must not stop the remaining reactors (RA-01).
func (f *Fan) observe(ctx context.Context, o Observation) {
	if f.obs == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil {
			f.log.LogAttrs(ctx, slog.LevelError, "reactor observer panicked", slog.String("reactor", o.Reactor), slog.Any("panic", v))
		}
	}()
	f.obs(o)
}
