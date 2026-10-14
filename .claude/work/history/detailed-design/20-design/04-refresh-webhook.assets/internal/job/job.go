// Package job: scratch copy of architecture section 8.3 (kernel contract between features and the engine).
package job

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

type Key string

func NewKey(kind string, parts ...string) Key {
	return Key(kind + "/" + strings.Join(parts, "/"))
}
func (k Key) Kind() string {
	kind, _, _ := strings.Cut(string(k), "/")
	return kind
}
func (k Key) Parts() []string {
	_, rest, ok := strings.Cut(string(k), "/")
	if !ok || rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

type Priority uint8

// Priorities are 1-based as in design unit 01 (the zero value is invalid on purpose): an engine must never use
// the numeric value as an array index (see refresh.classOf).
const (
	PrioInteractive Priority = iota + 1
	PrioWebhook
	PrioScheduled
	PrioBackfill
)

type Scope uint8

const (
	ScopeRepo Scope = iota + 1
	ScopeGlobal
)

type Outcome struct {
	Unchanged bool
	NextDue   time.Time
}

type Spec struct {
	Kind     string
	Upstream string
	Scope    Scope
	Prio     Priority
	Policy   freshness.Policy
	Timeout  time.Duration
	Run      func(ctx context.Context, k Key) (Outcome, error)
}

type Enqueuer interface {
	Enqueue(k Key, p Priority, notBefore time.Time, force bool) error
}

var (
	ErrQueueFull = errors.New("job: queue full")
	ErrClosed    = errors.New("job: engine closed")
)

type RateLimited interface {
	error
	RetryAt() time.Time
}
type Permanent interface {
	error
	Permanent() bool
}
type Gone interface {
	error
	Gone() bool
}

func AsRateLimited(err error) (time.Time, bool) {
	var r RateLimited
	if errors.As(err, &r) {
		return r.RetryAt(), true
	}
	return time.Time{}, false
}
func IsPermanent(err error) bool {
	var p Permanent
	return errors.As(err, &p) && p.Permanent()
}
func IsGone(err error) bool {
	var g Gone
	return errors.As(err, &g) && g.Gone()
}
