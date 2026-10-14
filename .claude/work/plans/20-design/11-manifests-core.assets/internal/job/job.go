package job

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

type Key string

const GlobalPart = "_"

func NewKey(kind string, parts ...string) Key {
	return Key(strings.Join(append([]string{kind}, parts...), "/"))
}
func GlobalKey(kind string) Key { return NewKey(kind, GlobalPart) }
func (k Key) Kind() string      { return strings.SplitN(string(k), "/", 2)[0] }
func (k Key) Parts() []string {
	p := strings.Split(string(k), "/")
	return p[1:]
}
func (k Key) IsGlobal() bool { return strings.HasSuffix(string(k), "/"+GlobalPart) }

type marked struct {
	error
	gone, permanent bool
}

func (m marked) Unwrap() error   { return m.error }
func (m marked) Gone() bool      { return m.gone }
func (m marked) Permanent() bool { return m.permanent }

// MarkGone wraps err so that IsGone reports true; nil stays nil.
func MarkGone(err error) error {
	if err == nil {
		return nil
	}
	return marked{err, true, false}
}

// MarkPermanent wraps err so that IsPermanent reports true; nil stays nil.
func MarkPermanent(err error) error {
	if err == nil {
		return nil
	}
	return marked{err, false, true}
}

func IsPermanent(err error) bool { var p Permanent; return errors.As(err, &p) && p.Permanent() }
func IsGone(err error) bool      { var g Gone; return errors.As(err, &g) && g.Gone() }
func AsRateLimited(err error) (time.Time, bool) {
	var r RateLimited
	if errors.As(err, &r) {
		return r.RetryAt(), true
	}
	return time.Time{}, false
}

type Priority uint8

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

var ErrQueueFull, ErrClosed = errors.New("queue full"), errors.New("closed")

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
