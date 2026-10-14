package limits

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

// ErrAbandoned: the parse goroutine did not return before the watchdog deadline. It is still running and still holds its memory weight.
var ErrAbandoned = errors.New("limits: parser abandoned by watchdog")

// ErrCircuitOpen: the parser was abandoned Trip times; it is disabled until restart.
var ErrCircuitOpen = errors.New("limits: parser circuit open")

// Budget is the process-wide weighted memory semaphore for in-flight parses.
type Budget struct {
	sem   *semaphore.Weighted
	cap   int64
	inUse atomic.Int64
}

func NewBudget(capBytes int64) *Budget {
	return &Budget{sem: semaphore.NewWeighted(capBytes), cap: capBytes}
}

// Clamp makes a weight acquirable: a weight above the capacity would block forever in semaphore.Weighted.
func (b *Budget) Clamp(w int64) int64 {
	if w < 1 {
		return 1
	}
	if w > b.cap {
		return b.cap
	}
	return w
}
func (b *Budget) InUse() int64 { return b.inUse.Load() }

// Watchdog runs parse functions under a deadline, a memory budget and a per-parser circuit.
type Watchdog struct {
	Budget    *Budget
	Grace     time.Duration         // added to Limits.Timeout before a goroutine is declared abandoned; default 500 ms
	Trip      int                   // abandonments that open the circuit; default 3
	OnAbandon func(parserID string) // metric hook; may be nil; called on the caller's goroutine

	mu       sync.Mutex
	abandons map[string]int
	wg       sync.WaitGroup
}

func NewWatchdog(b *Budget) *Watchdog {
	return &Watchdog{Budget: b, Grace: 500 * time.Millisecond, Trip: 3, abandons: map[string]int{}}
}

func (w *Watchdog) Open(parserID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.abandons[parserID] >= w.Trip
}

// Wait blocks until every abandoned goroutine has returned or ctx ends (shutdown, goleak in tests).
func (w *Watchdog) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type result[T any] struct {
	v   T
	err error
}

// Run executes fn in its own goroutine. fn receives a context that is cancelled at timeout (cooperative stop).
// If fn does not return within timeout+Grace, Run returns ErrAbandoned while the goroutine keeps running and
// keeps weight until it really exits. weight is clamped to the budget capacity.
func Run[T any](ctx context.Context, w *Watchdog, parserID string, timeout time.Duration, weight int64, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	if w.Open(parserID) {
		return zero, ErrCircuitOpen
	}
	weight = w.Budget.Clamp(weight)
	if err := w.Budget.sem.Acquire(ctx, weight); err != nil {
		return zero, err
	}
	w.Budget.inUse.Add(weight)
	pctx, cancel := context.WithTimeout(ctx, timeout)
	ch := make(chan result[T], 1)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			w.Budget.inUse.Add(-weight)
			w.Budget.sem.Release(weight)
		}()
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				ch <- result[T]{err: &PanicError{Value: r}}
			}
		}()
		v, err := fn(pctx)
		ch <- result[T]{v, err}
	}()
	t := time.NewTimer(timeout + w.Grace)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-t.C:
		w.mu.Lock()
		w.abandons[parserID]++
		w.mu.Unlock()
		if w.OnAbandon != nil {
			w.OnAbandon(parserID)
		}
		return zero, ErrAbandoned
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// PanicError wraps a recovered parser panic (a bug: the Parser contract forbids panics).
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return "limits: parser panicked" }
