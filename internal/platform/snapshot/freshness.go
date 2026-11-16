package snapshot

import "time"

// State is the stored outcome of the last completed fetch of an item. The names are persisted.
type State string

const (
	StateNever       State = "never"     // no fetch completed yet
	StateOK          State = "ok"        // content present
	StateNotFound    State = "not_found" // the thing does not exist (no README, no release)
	StateTooLarge    State = "too_large" // present but over the size cap
	StateNoContent   State = "no_content"
	StateUnsupported State = "unsupported" // present in a form that cannot be shown
	StateError       State = "error"       // the last attempt failed permanently
)

// Freshness is the fetch history of one item.
type Freshness struct {
	State     State         `json:"state"`
	FetchedAt time.Time     `json:"fetchedAt"` // last time the content was read (200)
	CheckedAt time.Time     `json:"checkedAt"` // last time the content was confirmed (200 or 304)
	Attempts  int           `json:"attempts"`  // consecutive failed attempts since the last success
	ErrCode   string        `json:"errCode,omitempty"`
	TTL       time.Duration `json:"ttl"` // refresh interval; zero for items refreshed by events only
}

// Status is the freshness shown to readers, derived at read time.
type Status uint8

const (
	StatusUnknown Status = iota // never confirmed
	StatusFresh
	StatusStale // older than 1.5 x TTL, or the last attempt failed and an older value is served
	StatusError // failing and no value was ever confirmed
)

func (s Status) String() string {
	switch s {
	case StatusFresh:
		return "fresh"
	case StatusStale:
		return "stale"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Derive returns the status at now.
func (f Freshness) Derive(now time.Time) Status {
	if f.CheckedAt.IsZero() {
		if f.Attempts > 0 || f.State == StateError {
			return StatusError
		}
		return StatusUnknown
	}
	if f.Attempts > 0 || f.State == StateError {
		return StatusStale
	}
	if f.TTL > 0 && now.Sub(f.CheckedAt) > f.TTL*3/2 {
		return StatusStale
	}
	return StatusFresh
}
