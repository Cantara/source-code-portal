package job

import (
	"errors"
	"time"
)

// Enqueuer is how features and reactors ask for work. *refresh.Engine satisfies it.
type Enqueuer interface {
	// Enqueue never blocks. It coalesces with a queued or running entry of the same key (raising priority and pulling
	// notBefore earlier, never the reverse). force unparks a parked key. Errors: ErrQueueFull when the keyed table is at
	// its cap and ErrClosed once the engine has stopped. An engine may also return its own sentinel for a key whose kind has no
	// registered Spec (a wiring bug); that one is not a refusal.
	Enqueue(k Key, p Priority, notBefore time.Time, force bool) error
}

// EnqueueFunc adapts a function to Enqueuer (for tests and bridges).
type EnqueueFunc func(k Key, p Priority, notBefore time.Time, force bool) error

func (f EnqueueFunc) Enqueue(k Key, p Priority, notBefore time.Time, force bool) error {
	return f(k, p, notBefore, force)
}

var (
	// ErrQueueFull: the engine's keyed table is at capacity. Retryable by the sender (the webhook answers 503).
	ErrQueueFull = errors.New("job: queue full")
	// ErrClosed: the engine has stopped or is shutting down. Retryable elsewhere (the webhook answers 503).
	ErrClosed = errors.New("job: engine closed")
)

// IsRefusal reports that the engine refused the work for a reason the sender should retry later: ErrQueueFull or ErrClosed.
func IsRefusal(err error) bool { return errors.Is(err, ErrQueueFull) || errors.Is(err, ErrClosed) }
