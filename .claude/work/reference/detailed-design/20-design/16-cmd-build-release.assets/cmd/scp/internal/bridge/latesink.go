package bridge

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/cantara/source-code-portal/internal/scm"
)

// ErrSinkNotWired is returned by LateSink.Handle before Set: an event arrived while the process was still starting.
var ErrSinkNotWired = errors.New("bridge: event sink not wired yet")

// Handler is the one method of webhook.Sink and catalog.EventSink (both are satisfied by *reactor.Fan).
type Handler interface {
	Handle(ctx context.Context, ev scm.Event) error
}

// LateSink breaks the one construction cycle of the startup sequence: catalog needs an EventSink at New (it emits head-changed
// events), the reactor.Fan needs catalog's React method at New (reactor.New takes its reactors as arguments), so the fan cannot
// exist yet. cmd/scp passes a *LateSink to catalog, builds the fan after every feature, and calls Set once. Handle is safe for
// concurrent use; before Set it fails with ErrSinkNotWired (catalog logs it and tries again on the next sweep: head sweeps run only
// after the engine started, which is after Set).
type LateSink struct{ h atomic.Pointer[Handler] }

// Set installs the real sink. A second call panics: the wiring has exactly one fan.
func (s *LateSink) Set(h Handler) {
	if h == nil {
		panic("bridge: LateSink.Set(nil)")
	}
	if !s.h.CompareAndSwap(nil, &h) {
		panic("bridge: LateSink.Set called twice")
	}
}

// Handle forwards to the sink installed by Set.
func (s *LateSink) Handle(ctx context.Context, ev scm.Event) error {
	if h := s.h.Load(); h != nil {
		return (*h).Handle(ctx, ev)
	}
	return ErrSinkNotWired
}
