// Package freshness is a STUB of unit 01 (design 01-kernel.md section 3.2): the symbols internal/manifests uses, with the specified semantics.
package freshness

import "time"

type State string

const (
	StateOK          State = "ok"
	StateNotFound    State = "not_found"
	StateUnsupported State = "unsupported"
	StateTooLarge    State = "too_large"
	StateNoContent   State = "no_content"
	StateError       State = "error"
	StateNever       State = "never"
)

func (s State) HasResult() bool {
	switch s {
	case StateOK, StateNotFound, StateUnsupported, StateTooLarge, StateNoContent:
		return true
	}
	return false
}

type Status uint8

const (
	StatusUnknown Status = iota
	StatusFresh
	StatusStale
	StatusError
)

func (s Status) String() string {
	return [...]string{"unknown", "fresh", "stale", "error"}[min(int(s), 3)]
}

type Policy struct {
	TTL            time.Duration
	Jitter         float64
	IdleBackoffMax time.Duration
}
type Info struct {
	State      State
	FetchedAt  time.Time
	CheckedAt  time.Time
	Attempts   int
	ErrCode    string
	ContentSHA string
	TTL        time.Duration
}

func (i Info) HasValue() bool            { return !i.FetchedAt.IsZero() }
func (i Info) StaleAfter() time.Duration { return i.TTL * 3 / 2 }
func (i Info) StaleSince() (time.Time, bool) {
	if i.FetchedAt.IsZero() {
		return time.Time{}, false
	}
	return i.FetchedAt.Add(i.StaleAfter()), true
}

// Derive: unknown without a value; error when the last attempt failed and no value is held; stale past 1.5 x TTL (TTL <= 0 is stale).
func (i Info) Derive(now time.Time) Status {
	switch {
	case i.FetchedAt.IsZero():
		if i.State == StateError {
			return StatusError
		}
		return StatusUnknown
	case i.TTL <= 0 || now.Sub(i.FetchedAt) > i.StaleAfter():
		return StatusStale
	case i.State == StateError:
		return StatusStale // stale-while-error: the held value keeps serving but is marked
	}
	return StatusFresh
}
func (i Info) AfterSuccess(state State, now time.Time, contentSHA string) Info {
	if !state.HasResult() {
		panic("freshness: AfterSuccess with a non-result state")
	}
	i.State, i.FetchedAt, i.CheckedAt, i.Attempts, i.ErrCode, i.ContentSHA = state, now.UTC(), now.UTC(), 0, "", contentSHA
	return i
}
func (i Info) AfterRevalidated(now time.Time) Info {
	if !i.HasValue() {
		return i
	}
	i.State, i.FetchedAt, i.CheckedAt, i.Attempts, i.ErrCode = StateOK, now.UTC(), now.UTC(), 0, ""
	return i
}
func (i Info) AfterFailure(now time.Time, errCode string) Info {
	i.State, i.CheckedAt, i.ErrCode = StateError, now.UTC(), errCode
	i.Attempts++
	return i
}

type Value[T any] struct {
	V    T
	Info Info
}

func (v Value[T]) Status(now time.Time) Status { return v.Info.Derive(now) }
