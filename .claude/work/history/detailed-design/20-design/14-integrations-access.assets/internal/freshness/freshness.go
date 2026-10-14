// Package freshness: STUB copied from architecture 8.2.
package freshness

import "time"

type State string

const (
	StateOK        State = "ok"
	StateNotFound  State = "not_found"
	StateNoContent State = "no_content"
	StateError     State = "error"
	StateNever     State = "never"
)

type Status uint8

const (
	StatusUnknown Status = iota
	StatusFresh
	StatusStale
	StatusError
)

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

func (i Info) Derive(now time.Time) Status {
	if i.State == StateNever || (i.FetchedAt.IsZero() && i.CheckedAt.IsZero()) {
		return StatusUnknown
	}
	if i.FetchedAt.IsZero() {
		return StatusError
	}
	if i.State == StateError || now.Sub(i.FetchedAt) > i.TTL*3/2 {
		return StatusStale
	}
	return StatusFresh
}

type Value[T any] struct {
	V    T
	Info Info
}
