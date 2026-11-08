package webhook

import (
	"context"
	"errors"
	"log/slog"
	gosync "sync"
	"time"

	"github.com/cantara/source-code-portal/internal/sync"
)

// Delivery is a line of a webhook delivery log: one thing GitHub sent, and whether the portal answered it with a success.
type Delivery struct {
	GUID         string
	Event        string
	Action       string
	RepositoryID int64
	At           time.Time
	OK           bool
}

// ErrTruncated is what Read returns with the deliveries it did read when the log reaches back further than it reads: the newest
// deliveries are in the result, older ones down to the time asked for are not.
var ErrTruncated = errors.New("webhook: the delivery log is longer than was read")

const (
	catchUpEvery   = 2 * time.Minute // how often the delivery log is read
	catchUpOverlap = 5 * time.Minute // each read starts this far before the last one ended: a delivery logged late is still found
	catchUpFirst   = time.Hour       // how far back the first read of a store that never read the log goes
	catchUpMax     = 72 * time.Hour  // GitHub keeps the log three days
	catchUpWorst   = time.Hour       // the longest wait after failures
	catchUpMemory  = 2 * time.Hour   // how long a delivery already acted on is remembered
	catchUpKey     = "webhook/caught-up"
)

// CatchUp makes the receiver complete: GitHub sends a delivery once and does not send it again, so one that arrives while the portal
// is down (every update restarts it), full or failing is gone. An App keeps a log of its deliveries with the answer each got;
// CatchUp reads it and passes every delivery the portal did not answer with a success to the sink, as if it had just arrived. Reading
// the log is a GET with the App's own token and needs no permission.
type CatchUp struct {
	// Source names the source whose App's log this is, for the status and the log.
	Source string
	// Read reads the deliveries made since a time, newest first.
	Read func(ctx context.Context, since time.Time) ([]Delivery, error)
	// Resolve names a repository by its GitHub id; false when the portal does not know it (its listing repairs that).
	Resolve func(id int64) (owner, name string, ok bool)
	Sink    Sink
	// Last and SetLast keep the time of the last complete read over a restart.
	Last    func(ctx context.Context) time.Time
	SetLast func(ctx context.Context, at time.Time)
	Observe func(event, result string)
	Logger  *slog.Logger
	Now     func() time.Time
	Every   time.Duration // 0: catchUpEvery

	done map[string]time.Time // delivery ids acted on, by when they were logged

	mu    gosync.Mutex
	state CatchUpState
}

// CatchUpState is what the status says of one delivery log.
type CatchUpState struct {
	Source string
	// LastRead is when the log was last read and every delivery in it passed on; zero before the first complete read.
	LastRead time.Time
	// Error is why the last read or pass did not complete; empty when it did.
	Error string
	// Truncated is when a read last found the log longer than it reads: deliveries older than the newest ones were not passed on,
	// and the polls repair what they told.
	Truncated time.Time
}

// State returns what the last reads did.
func (c *CatchUp) State() CatchUpState {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.state
	st.Source = c.Source
	return st
}

func (c *CatchUp) note(f func(*CatchUpState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f(&c.state)
}

// eventsOf is what a delivery asks the portal to read again. A push says nothing of its ref in the log, so it stands for the default
// branch, another branch and a tag.
func eventsOf(d Delivery) []sync.Event {
	switch d.Event {
	case "push":
		return []sync.Event{sync.EventPush, sync.EventBranch, sync.EventTag}
	case "release":
		return []sync.Event{sync.EventTag}
	case "repository":
		if sourceActions[d.Action] {
			return []sync.Event{sync.EventSourceChanged}
		}
	case "installation", "installation_repositories", "ping", "":
	default:
		if eventPattern.MatchString(d.Event) {
			return []sync.Event{sync.Event(d.Event)}
		}
	}
	return nil
}

// Once reads the log once and passes on what the portal missed; it returns how many deliveries it passed on.
func (c *CatchUp) Once(ctx context.Context) (int, error) {
	now := c.Now()
	since := now.Add(-catchUpFirst)
	if last := c.Last(ctx); !last.IsZero() {
		since = last.Add(-catchUpOverlap)
	}
	if oldest := now.Add(-catchUpMax); since.Before(oldest) {
		since = oldest
	}
	log, err := c.Read(ctx, since)
	truncated := errors.Is(err, ErrTruncated)
	if err != nil && !truncated {
		c.note(func(st *CatchUpState) { st.Error = err.Error() })
		return 0, err
	}
	if truncated {
		// Nothing can be read further back than the newest deliveries (GitHub lists newest first). They are passed on, the cursor
		// moves on, and the polls repair the rest; the status and the log say so, so that it is known.
		c.note(func(st *CatchUpState) { st.Truncated = now })
		c.Observe("log", "truncated")
		c.Logger.Warn("webhook delivery log longer than was read; the oldest deliveries are left to the polls", "read", len(log), "since", since)
	}
	if c.done == nil {
		c.done = map[string]time.Time{}
	}
	passed, incomplete := 0, false
	for i := len(log) - 1; i >= 0; i-- { // oldest first
		d := log[i]
		if d.OK || d.GUID == "" || d.At.Before(since) {
			continue
		}
		if _, seen := c.done[d.GUID]; seen {
			continue
		}
		owner, name, known := c.Resolve(d.RepositoryID)
		evs := eventsOf(d)
		if !known || len(evs) == 0 {
			continue // the catalogue's listing repairs what it does not know
		}
		if d.Event == "repository" {
			name = ""
		}
		failed := false
		for _, ev := range evs {
			if err := c.Sink(ev, owner, name); err != nil {
				failed = true
			}
		}
		if failed {
			incomplete = true // the next round reads from the same time
			continue
		}
		c.done[d.GUID] = d.At
		passed++
		c.Observe(d.Event, "caught_up")
	}
	for id, at := range c.done {
		if now.Sub(at) > catchUpMemory {
			delete(c.done, id)
		}
	}
	if incomplete {
		c.note(func(st *CatchUpState) { st.Error = "a delivery could not be passed on; it is tried again" })
	} else {
		c.SetLast(ctx, now)
		c.note(func(st *CatchUpState) { st.LastRead, st.Error = now, "" })
	}
	return passed, nil
}

// Run reads the log once the engine knows its repositories (warm), then every Every, with a growing wait after failures, until ctx ends.
func (c *CatchUp) Run(ctx context.Context, warm <-chan struct{}) {
	select {
	case <-warm:
	case <-ctx.Done():
		return
	}
	every := c.Every
	if every <= 0 {
		every = catchUpEvery
	}
	failures := 0
	for {
		n, err := c.Once(ctx)
		wait := every
		switch {
		case err != nil:
			failures++
			wait = min(catchUpWorst, every<<min(failures, 6))
			// The wait grows to an hour, so a log that stays unreadable is a warning an hour, not a flood.
			c.Logger.Warn("webhook delivery log not read", "source", c.Source, "err", err, "again_in", wait.String())
		case n > 0:
			failures = 0
			c.Logger.Info("webhook deliveries the portal had missed passed on", "deliveries", n)
		default:
			failures = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
