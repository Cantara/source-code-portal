package sync

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/snapshot"
	"github.com/cantara/source-code-portal/internal/platform/store"
)

type class uint8

const (
	classOK class = iota
	classTransient
	classRate
	classAuth
	classPermanent
	classWaiting
)

func classify(err error) (class, time.Time) {
	if err == nil {
		return classOK, time.Time{}
	}
	var rl rateLimited
	if errors.As(err, &rl) {
		if t, ok := rl.RateLimitedUntil(); ok {
			return classRate, t
		}
	}
	var wt waiting
	if errors.As(err, &wt) && wt.Waiting() {
		return classWaiting, time.Time{}
	}
	var af authFailed
	if errors.As(err, &af) && af.AuthFailed() {
		return classAuth, time.Time{}
	}
	var pm permanent
	if errors.As(err, &pm) && pm.Permanent() {
		return classPermanent, time.Time{}
	}
	return classTransient, time.Time{}
}

// succeeded updates en after a successful run. Callers hold e.mu.
func (e *Engine) succeeded(en *entry, r Result, now time.Time) {
	it := en.item
	if !r.Unchanged {
		it.State, it.FetchedAt, it.ContentSHA, it.Payload = r.State, now, r.ContentSHA, r.Payload
		if it.State == "" {
			it.State = snapshot.StateOK
		}
	}
	it.CheckedAt, it.Attempts, it.ErrCode = now, 0, ""
	en.item = it
	ttl := e.TTLOf(en.spec, en.source)
	en.due, en.prio, en.backoff = now.Add(withJitter(ttl)), en.spec.Prio, false
	if r.Again > 0 && r.Again < ttl {
		en.due = now.Add(r.Again)
	}
	if p := e.pauses[en.source]; p != nil {
		p.rateStreak, p.authStreak = 0, 0
	}
}

func failed(it store.Item, err error) store.Item {
	it.Attempts++
	it.ErrCode = "error"
	var c coded
	if errors.As(err, &c) {
		it.ErrCode = c.ErrorCode()
	}
	if it.CheckedAt.IsZero() {
		it.State = snapshot.StateError
	}
	return it
}

// pauseSource stops starts for source. A pause already in force is not extended or escalated, so one rate limit seen by several
// jobs in flight pauses once. Callers hold e.mu.
func (e *Engine) pauseSource(source string, retryAt time.Time, auth bool, now time.Time) time.Time {
	p := e.pauses[source]
	if p == nil {
		p = &pause{}
		e.pauses[source] = p
	}
	if p.until.After(now) {
		return time.Time{}
	}
	switch {
	case auth:
		p.until = now.Add(min(authWaitBase<<p.authStreak, authWaitMax))
		p.authStreak++
	case !retryAt.IsZero():
		p.until = retryAt.Add(rand.N(time.Second)) //nolint:gosec // G404: stagger, not security
	default:
		p.until = now.Add(min(rateWaitBase<<p.rateStreak, rateWaitMax))
		p.rateStreak++
	}
	return p.until
}

// backoff is the wait before retry number attempts: full jitter between retryBase and the capped exponential.
func backoff(attempts int) time.Duration {
	ceil := retryMax
	if attempts < 20 {
		ceil = min(retryBase<<(attempts-1), retryMax)
	}
	if ceil <= retryBase {
		return retryBase
	}
	return retryBase + rand.N(ceil-retryBase) //nolint:gosec // G404: jitter, not security
}

func withJitter(ttl time.Duration) time.Duration {
	d := time.Duration(float64(ttl) * jitter)
	if d <= 0 {
		return ttl
	}
	return ttl - d + rand.N(2*d) //nolint:gosec // G404: jitter, not security
}
