package job

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

// Priority orders ready work: lower runs first. The zero value is invalid (unset).
type Priority uint8

const (
	PrioInteractive Priority = iota + 1 // an operator or a user waiting
	PrioWebhook                         // accelerators from events
	PrioScheduled                       // the planner's level-triggered work
	PrioBackfill                        // cold start and bulk catch-up
)

func (p Priority) Valid() bool { return p >= PrioInteractive && p <= PrioBackfill }

func (p Priority) String() string {
	switch p {
	case PrioInteractive:
		return "interactive"
	case PrioWebhook:
		return "webhook"
	case PrioScheduled:
		return "scheduled"
	case PrioBackfill:
		return "backfill"
	}
	return "invalid"
}

// Scope says how many keys a kind has. The zero value is invalid (unset).
type Scope uint8

const (
	ScopeRepo   Scope = iota + 1 // the planner tracks one key per repository
	ScopeGlobal                  // one key per kind: kind + "/_"
)

func (s Scope) Valid() bool { return s == ScopeRepo || s == ScopeGlobal }

// Outcome tells the planner what happened without leaking engine types.
type Outcome struct {
	// Unchanged: upstream confirmed nothing changed (304, equal SHA, equal tree oid). It lengthens the next interval.
	Unchanged bool
	// NextDue, when non-zero, replaces the planned next run (an absolute time); the planner still applies no jitter to it.
	NextDue time.Time
}

// RunFunc brings one item up to date. It must be idempotent and level-triggered: "make this key current", not
// "apply this event". It must honour ctx and return promptly when it is cancelled.
type RunFunc func(ctx context.Context, k Key) (Outcome, error)

// Spec registers one job kind with the engine and the planner.
type Spec struct {
	Kind     string // "commits.refresh"; ValidKind
	Upstream string // "github", "jenkins", "snyk", "maven-central", "local"; ^[a-z][a-z0-9-]{0,31}$
	Scope    Scope
	Prio     Priority         // the planner's priority; an Enqueue call carries its own
	Policy   freshness.Policy // TTL, jitter, idle back-off
	Timeout  time.Duration    // per-run deadline enforced by the engine; 0 means the upstream's JobTimeout
	Run      RunFunc
}

var upstreamRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Validate returns every violation, joined.
func (s Spec) Validate() error {
	var errs []error
	if !ValidKind(s.Kind) {
		errs = append(errs, fmt.Errorf("kind %q is invalid", s.Kind))
	}
	if !upstreamRE.MatchString(s.Upstream) {
		errs = append(errs, fmt.Errorf("upstream %q is invalid", s.Upstream))
	}
	if !s.Scope.Valid() {
		errs = append(errs, errors.New("scope is unset"))
	}
	if !s.Prio.Valid() {
		errs = append(errs, errors.New("priority is unset"))
	}
	if err := s.Policy.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("policy: %w", err))
	}
	if s.Timeout < 0 {
		errs = append(errs, errors.New("timeout is negative"))
	}
	if s.Run == nil {
		errs = append(errs, errors.New("run is nil"))
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("job spec %q: %w", s.Kind, errors.Join(errs...))
}
