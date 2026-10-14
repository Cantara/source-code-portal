// Package freshness is the status and age vocabulary of every fetched item: the stored State, the derived Status,
// the refresh Policy and the generic Value wrapper carried by every snapshot slot.
//
// Time is never data here: Derive and Interval take their inputs as arguments, so the package has no clock and
// no randomness.
package freshness

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// State is the stored outcome of the last fetch of an item. Its text form is persisted (item.status) and must not change.
type State string

const (
	StateOK          State = "ok"          // fetched; payload holds the value
	StateNotFound    State = "not_found"   // upstream says it does not exist (a definitive answer)
	StateUnsupported State = "unsupported" // exists in a format the portal does not render
	StateTooLarge    State = "too_large"   // exists but over the size cap
	StateNoContent   State = "no_content"  // exists and is empty
	StateError       State = "error"       // the last attempt failed (a good value may still be held)
	StateNever       State = "never"       // not fetched yet; also the meaning of the zero value ""
)

// States lists every valid state in a fresh slice (the SQL CHECK constraint is generated from and tested against it).
func States() []State {
	return []State{StateOK, StateNotFound, StateUnsupported, StateTooLarge, StateNoContent, StateError, StateNever}
}

// Valid reports whether s is one of States() or the zero value.
func (s State) Valid() bool {
	if s == "" {
		return true
	}
	for _, v := range States() {
		if s == v {
			return true
		}
	}
	return false
}

// HasResult reports whether s is a definitive upstream answer: ok, not_found, unsupported, too_large or no_content.
func (s State) HasResult() bool {
	switch s {
	case StateOK, StateNotFound, StateUnsupported, StateTooLarge, StateNoContent:
		return true
	}
	return false
}

// Status is the freshness shown to users, derived at read time. The zero value is StatusUnknown.
type Status uint8

const (
	StatusUnknown Status = iota // never fetched
	StatusFresh
	StatusStale // a good value exists but is old or the last attempt failed
	StatusError // no good value and the last attempt failed
)

// String is the API enum value (freshness.status).
func (s Status) String() string {
	switch s {
	case StatusFresh:
		return "fresh"
	case StatusStale:
		return "stale"
	case StatusError:
		return "error"
	}
	return "unknown"
}

// Policy is a job kind's refresh policy.
type Policy struct {
	TTL            time.Duration // drift bound: the item is due again after about TTL
	Jitter         float64       // +- fraction applied to the interval, in [0, 0.5]
	IdleBackoffMax time.Duration // cap for the doubling interval after consecutive unchanged outcomes; 0 disables back-off
}

// Validate checks the policy invariants: TTL > 0, 0 <= Jitter <= 0.5, IdleBackoffMax == 0 or >= TTL.
func (p Policy) Validate() error {
	var errs []error
	if p.TTL <= 0 {
		errs = append(errs, errors.New("ttl must be > 0"))
	}
	if !(p.Jitter >= 0 && p.Jitter <= 0.5) { // written so that NaN fails
		errs = append(errs, fmt.Errorf("jitter %v outside [0, 0.5]", p.Jitter))
	}
	if p.IdleBackoffMax != 0 && p.IdleBackoffMax < p.TTL {
		errs = append(errs, fmt.Errorf("idle back-off cap %v below ttl %v", p.IdleBackoffMax, p.TTL))
	}
	return errors.Join(errs...)
}

// MinInterval is the floor of Policy.Interval (the planner's tick granularity).
const MinInterval = time.Second

// Interval is the time from a completed run to the next planned run: TTL doubled once per consecutive unchanged
// outcome (unchangedStreak) up to IdleBackoffMax, then multiplied by 1 + Jitter*(2u-1) where u is a uniform random
// number in [0,1) supplied by the caller (so the function is deterministic and testable). It never returns less than
// MinInterval and never overflows.
func (p Policy) Interval(unchangedStreak int, u float64) time.Duration {
	base := p.TTL
	if base <= 0 {
		base = MinInterval
	}
	capd := p.IdleBackoffMax
	for i := 0; i < unchangedStreak && capd > 0 && base < capd; i++ {
		if base > time.Duration(1<<62)/2 {
			base = capd
			break
		}
		base *= 2
	}
	if capd > 0 && base > capd {
		base = capd
	}
	switch {
	case !(u >= 0): // negative or NaN
		u = 0
	case u >= 1:
		u = 0.999999999
	}
	j := p.Jitter
	if !(j >= 0 && j <= 0.5) { // an unvalidated policy never produces a wild interval
		j = 0
	}
	f := float64(base) * (1 + j*(2*u-1))
	switch {
	case f >= math.MaxInt64: // float to int conversion out of range is platform-dependent: saturate explicitly
		return time.Duration(math.MaxInt64)
	case f < float64(MinInterval):
		return MinInterval
	}
	return time.Duration(f)
}

// Info is the freshness record of one item. The zero value means "never fetched".
type Info struct {
	State      State
	FetchedAt  time.Time // last successful fetch or successful 304 validation (UTC); zero: no good value
	CheckedAt  time.Time // last attempt, successful or not
	Attempts   int       // consecutive failures; reset by success
	ErrCode    string    // stable machine code of the last failure; "" when the last attempt succeeded
	ContentSHA string
	TTL        time.Duration
}

// HasValue reports whether a good value is held (stale-while-error keeps it across failures).
func (i Info) HasValue() bool { return !i.FetchedAt.IsZero() }

// StaleAfter is the age beyond which a held value is stale: 1.5 x TTL.
func (i Info) StaleAfter() time.Duration { return i.TTL + i.TTL/2 }

// StaleSince returns the instant the value became (or becomes) stale; false when there is no value or no TTL.
func (i Info) StaleSince() (time.Time, bool) {
	if !i.HasValue() || i.TTL <= 0 {
		return time.Time{}, false
	}
	return i.FetchedAt.Add(i.StaleAfter()), true
}

// Derive computes the user-facing status at time now:
//
//	no good value, last attempt failed            -> Error
//	no good value, never fetched                  -> Unknown
//	good value, last attempt failed               -> Stale
//	good value, age <= 1.5 x TTL                  -> Fresh
//	good value, older, or TTL not set             -> Stale
func (i Info) Derive(now time.Time) Status {
	failed := i.State == StateError
	if !i.HasValue() {
		if failed {
			return StatusError
		}
		return StatusUnknown
	}
	if failed || i.TTL <= 0 {
		return StatusStale
	}
	age := now.Sub(i.FetchedAt)
	if age < 0 {
		age = 0
	}
	if age <= i.StaleAfter() {
		return StatusFresh
	}
	return StatusStale
}

// AfterSuccess is the Info after a fetch that returned a definitive result (state.HasResult()).
// TTL is kept; ContentSHA is replaced.
func (i Info) AfterSuccess(state State, now time.Time, contentSHA string) Info {
	if !state.HasResult() {
		panic("freshness: AfterSuccess with non-result state " + string(state))
	}
	i.State, i.FetchedAt, i.CheckedAt = state, now.UTC(), now.UTC()
	i.Attempts, i.ErrCode, i.ContentSHA = 0, "", contentSHA
	return i
}

// AfterRevalidated is the Info after a conditional request answered 304 (or the SHA is equal): the held value is
// confirmed. It is only meaningful when HasValue; otherwise it returns i unchanged. A result state (ok, not_found,
// unsupported, too_large, no_content) is kept as it is; after a failure the state is "error" and the original result
// state is no longer known, so the value is taken to be "ok" (use AfterRevalidatedAs when the caller knows better).
func (i Info) AfterRevalidated(now time.Time) Info {
	if !i.HasValue() {
		return i
	}
	state := i.State
	if !state.HasResult() {
		state = StateOK
	}
	return i.AfterRevalidatedAs(state, now)
}

// AfterRevalidatedAs is AfterRevalidated for a caller that knows the result state of the value it holds (a commits
// ring that is "no_content", a README that is "too_large"): the state is set to it. It returns i unchanged when no value
// is held and panics on a non-result state, like AfterSuccess.
func (i Info) AfterRevalidatedAs(state State, now time.Time) Info {
	if !state.HasResult() {
		panic("freshness: AfterRevalidatedAs with non-result state " + string(state))
	}
	if !i.HasValue() {
		return i
	}
	i.State, i.FetchedAt, i.CheckedAt = state, now.UTC(), now.UTC()
	i.Attempts, i.ErrCode = 0, ""
	return i
}

// AfterFailure is the Info after a failed attempt: stale-while-error keeps FetchedAt, ContentSHA and the value.
func (i Info) AfterFailure(now time.Time, errCode string) Info {
	i.State, i.CheckedAt = StateError, now.UTC()
	i.Attempts++
	i.ErrCode = errCode
	return i
}

// Validate checks the cross-field invariants (used by tests and the storetest oracle).
func (i Info) Validate() error {
	var errs []error
	if !i.State.Valid() {
		errs = append(errs, fmt.Errorf("unknown state %q", i.State))
	}
	if i.State.HasResult() && i.FetchedAt.IsZero() {
		errs = append(errs, errors.New("result state without fetched_at"))
	}
	if i.State == StateOK && i.ErrCode != "" {
		errs = append(errs, errors.New("ok state with error code"))
	}
	if i.Attempts < 0 {
		errs = append(errs, errors.New("negative attempts"))
	}
	if !i.FetchedAt.IsZero() && !i.CheckedAt.IsZero() && i.CheckedAt.Before(i.FetchedAt) {
		errs = append(errs, errors.New("checked_at before fetched_at"))
	}
	return errors.Join(errs...)
}

// Value pairs a payload with its freshness. It is the type carried by every per-repository snapshot slot.
type Value[T any] struct {
	V    T
	Info Info
}

// Status derives the status of the value at time now.
func (v Value[T]) Status(now time.Time) Status { return v.Info.Derive(now) }
