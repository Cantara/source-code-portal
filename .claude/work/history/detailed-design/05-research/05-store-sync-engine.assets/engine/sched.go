package engine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

type Task struct {
	Name         string
	Every        time.Duration
	Jitter       float64                            // 0.1 = +-10% of Every
	InitialDelay func(rnd *rand.Rand) time.Duration // e.g. derived from persisted last success
	Run          func(ctx context.Context) error
	Timeout      time.Duration
}

type TaskStatus struct {
	LastAttempt, LastSuccess          time.Time
	LastErr                           string
	Runs, Failures, Skipped, Overruns uint64
}

type Scheduler struct {
	mu    sync.Mutex
	tasks []*taskState
	rnd   *rand.Rand
}

type taskState struct {
	Task
	running atomic.Bool
	trigger chan struct{}
	st      TaskStatus
}

func NewScheduler(seed uint64) *Scheduler { return &Scheduler{rnd: rand.New(rand.NewPCG(seed, 1))} }

func (s *Scheduler) Add(t Task) {
	s.tasks = append(s.tasks, &taskState{Task: t, trigger: make(chan struct{}, 1)})
}

func (s *Scheduler) Status(name string) (TaskStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.Name == name {
			return t.st, true
		}
	}
	return TaskStatus{}, false
}

// Trigger requests an out-of-band run; skipped (returns false) if the task is already running.
func (s *Scheduler) Trigger(name string) bool {
	for _, t := range s.tasks {
		if t.Name == name {
			if t.running.Load() {
				s.mu.Lock()
				t.st.Skipped++
				s.mu.Unlock()
				return false
			}
			select {
			case t.trigger <- struct{}{}:
			default:
			}
			return true
		}
	}
	return false
}

// Run blocks until ctx is cancelled and every task goroutine has returned.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range s.tasks {
		seed := s.rnd.Uint64()
		wg.Go(func() { s.loop(ctx, t, rand.New(rand.NewPCG(seed, 2))) })
	}
	wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context, t *taskState, rnd *rand.Rand) {
	delay := time.Duration(0)
	if t.InitialDelay != nil {
		delay = t.InitialDelay(rnd)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-t.trigger:
		}
		start := time.Now()
		s.runOnce(ctx, t)
		// fixed-delay-with-jitter: the next interval starts after completion, so runs can never overlap.
		d := t.Every
		if t.Jitter > 0 {
			f := 1 + t.Jitter*(2*rnd.Float64()-1)
			d = time.Duration(float64(d) * f)
		}
		if time.Since(start) > t.Every {
			s.mu.Lock()
			t.st.Overruns++
			s.mu.Unlock()
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)
	}
}

func (s *Scheduler) runOnce(ctx context.Context, t *taskState) {
	if !t.running.CompareAndSwap(false, true) {
		return
	}
	defer t.running.Store(false)
	rctx := ctx
	if t.Timeout > 0 {
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(ctx, t.Timeout)
		defer cancel()
	}
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		err = t.Run(rctx)
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	t.st.LastAttempt = time.Now()
	t.st.Runs++
	if err != nil {
		t.st.Failures++
		t.st.LastErr = err.Error()
		return
	}
	t.st.LastSuccess = t.st.LastAttempt
	t.st.LastErr = ""
}
