// Package job: STUB copied from architecture 8.3.
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
	i := strings.IndexByte(string(k), '/')
	if i < 0 {
		return string(k)
	}
	return string(k)[:i]
}

type Priority uint8

const (
	PrioInteractive Priority = iota
	PrioWebhook
	PrioScheduled
	PrioBackfill
)

type Scope uint8

const (
	ScopeRepo Scope = iota
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

var ErrQueueFull = errors.New("job: queue full")
var ErrClosed = errors.New("job: closed")

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
