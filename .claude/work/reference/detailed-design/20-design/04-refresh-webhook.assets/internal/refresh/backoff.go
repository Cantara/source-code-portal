package refresh

import (
	"math/rand/v2"
	"time"
)

// Backoff configures transient-failure retries: full jitter with a floor (ADR-003).
type Backoff struct {
	Base, Cap   time.Duration // defaults 2s, 15m
	MaxAttempts int           // default 8: OnExhausted fires once, retries continue at Cap
}

// Delay returns the wait before retry number attempt (1 = first retry): Base + U[0, min(Cap, Base<<attempt) - Base].
// The shift saturates, so no overflow for large attempts or large Base.
func (b Backoff) Delay(attempt int, r *rand.Rand) time.Duration {
	if b.Base <= 0 {
		b.Base = time.Second
	}
	if b.Cap < b.Base {
		b.Cap = b.Base
	}
	hi := b.Cap
	if attempt < 62 {
		if shifted := b.Base << attempt; shifted > 0 && shifted>>attempt == b.Base && shifted < hi {
			hi = shifted
		}
	}
	if hi <= b.Base {
		return b.Base
	}
	return b.Base + time.Duration(r.Int64N(int64(hi-b.Base)+1))
}
