// Package freshness: scratch copy of the architecture section 8.2 types used by refresh.
package freshness

import "time"

type State string

type Policy struct {
	TTL            time.Duration
	Jitter         float64
	IdleBackoffMax time.Duration
}
