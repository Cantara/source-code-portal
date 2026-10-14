package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	// EventsPattern is the route of the snapshot event stream.
	EventsPattern = "GET /api/v1/events"
	maxStreams    = 1000
	heartbeat     = 25 * time.Second
	eventWrite    = 30 * time.Second // write deadline per event, in place of the server's write timeout
)

// Events streams server-sent events: the SnapshotInfo of the viewer's view when the stream opens and whenever a snapshot with another
// version is published, a comment every 25 s.
type Events struct {
	current   func(context.Context) *snapshot.Snapshot
	published func() <-chan struct{}
	streams   atomic.Int64
	max       int64
	beat      time.Duration
	done      chan struct{}
	once      sync.Once
}

// NewEvents returns the stream over the viewer's view (current) and the publisher's announcement (published: closed at the next
// publish).
func NewEvents(current func(context.Context) *snapshot.Snapshot, published func() <-chan struct{}) *Events {
	return &Events{current: current, published: published, max: maxStreams, beat: heartbeat, done: make(chan struct{})}
}

// Route is the stream's route.
func (e *Events) Route() Route { return Route{Pattern: EventsPattern, Handler: e, Stream: true} }

// Close ends every stream; register it with the server's shutdown.
func (e *Events) Close() { e.once.Do(func() { close(e.done) }) }

// Streams is the number of open streams.
func (e *Events) Streams() int64 { return e.streams.Load() }

func (e *Events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.streams.Add(1) > e.max {
		e.streams.Add(-1)
		w.Header().Set("Retry-After", "30")
		WriteProblem(w, http.StatusServiceUnavailable, "too-many-streams", "Too many event streams; poll the snapshot instead.")
		return
	}
	defer e.streams.Add(-1)
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{}) // the server's read timeout would end the request
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	send := func(msg []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(eventWrite))
		if _, err := w.Write(msg); err != nil {
			return false
		}
		return rc.Flush() == nil // the first flush sends the header: the stream is never compressed
	}
	if !send([]byte("retry: 5000\n\n")) {
		return
	}
	ctx := r.Context()
	tick := time.NewTicker(e.beat)
	defer tick.Stop()
	sent, last := false, uint64(0)
	for {
		changed := e.published() // before reading the snapshot, so no publish falls between
		if s := e.current(ctx); !sent || s.Version != last {
			data, err := json.Marshal(snapshotInfo(s, time.Now()))
			if err != nil || !send(append(append([]byte("event: snapshot\ndata: "), data...), '\n', '\n')) {
				return
			}
			sent, last = true, s.Version
		}
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case <-tick.C:
			if !send([]byte(": ping\n\n")) {
				return
			}
		case <-changed:
		}
	}
}

// unbounded serves a stream without the request deadline the middleware sets.
func unbounded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if base, ok := r.Context().Value(baseKey{}).(context.Context); ok {
			r = r.WithContext(base)
		}
		next.ServeHTTP(w, r)
	})
}

// baseKey holds the request context before the deadline.
type baseKey struct{}
