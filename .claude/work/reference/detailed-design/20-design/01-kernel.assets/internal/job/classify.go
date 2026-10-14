package job

import (
	"context"
	"errors"
	"time"
)

// RateLimited marks an error that means "the upstream asked us to wait until RetryAt". A zero RetryAt means the reset
// time is unknown; the engine then pauses the upstream for its default (60 s).
type RateLimited interface {
	error
	RetryAt() time.Time
}

// Permanent marks an error that retrying cannot fix (parse failure bound to a content SHA, 410, 451, 422).
type Permanent interface {
	error
	Permanent() bool
}

// Gone marks an error meaning the subject of the job no longer exists (repository removed). The key is dropped.
type Gone interface {
	error
	Gone() bool
}

// AsRateLimited extracts the retry time from any error in the chain implementing RateLimited.
func AsRateLimited(err error) (time.Time, bool) {
	var rl RateLimited
	if errors.As(err, &rl) {
		return rl.RetryAt(), true
	}
	return time.Time{}, false
}

// IsPermanent reports an error in the chain implementing Permanent with Permanent() == true.
func IsPermanent(err error) bool {
	var p Permanent
	return errors.As(err, &p) && p.Permanent()
}

// IsGone reports an error in the chain implementing Gone with Gone() == true.
func IsGone(err error) bool {
	var g Gone
	return errors.As(err, &g) && g.Gone()
}

// Class is the engine's single decision for a failed run.
type Class uint8

const (
	ClassNone        Class = iota // err == nil
	ClassTransient                // retry with backoff
	ClassRateLimited              // pause the upstream until RetryAt, do not consume an attempt
	ClassPermanent                // park the key
	ClassGone                     // drop the key
	ClassCanceled                 // the context was cancelled (shutdown): neither a failure nor a retry reason
)

func (c Class) String() string {
	names := [...]string{"none", "transient", "rate_limited", "permanent", "gone", "canceled"}
	if int(c) >= len(names) {
		return "invalid"
	}
	return names[c]
}

// Classify applies the precedence Gone > Permanent > RateLimited > Canceled > Transient. The engine additionally
// calls AsRateLimited whenever it is true, whatever the class, so that a rate-limit signal is never lost.
func Classify(err error) Class {
	switch {
	case err == nil:
		return ClassNone
	case IsGone(err):
		return ClassGone
	case IsPermanent(err):
		return ClassPermanent
	}
	if _, ok := AsRateLimited(err); ok {
		return ClassRateLimited
	}
	if errors.Is(err, context.Canceled) {
		return ClassCanceled
	}
	return ClassTransient
}

type gone struct{ err error }

func (e gone) Error() string { return e.err.Error() }
func (e gone) Unwrap() error { return e.err }
func (gone) Gone() bool      { return true }

// MarkGone wraps err so that it classifies as Gone. nil stays nil. Features use it in their job wrappers for
// "the repository disappeared" (scm.ErrNotFound on the repository itself, readmodel.ErrUnknownRepo, store.ErrNoRepo).
func MarkGone(err error) error {
	if err == nil {
		return nil
	}
	return gone{err}
}

type permanent struct{ err error }

func (e permanent) Error() string { return e.err.Error() }
func (e permanent) Unwrap() error { return e.err }
func (permanent) Permanent() bool { return true }

// MarkPermanent wraps err so that it classifies as Permanent. nil stays nil.
func MarkPermanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err}
}
