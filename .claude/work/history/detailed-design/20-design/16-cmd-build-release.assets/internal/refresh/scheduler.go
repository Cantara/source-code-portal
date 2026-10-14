package refresh

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Task is local housekeeping only: it never calls an upstream (those are global job.Specs in the engine).
type Task struct {
	Name         string
	Every        time.Duration
	Jitter       float64
	InitialDelay func(rnd *rand.Rand) time.Duration // derived from persisted last_success so a restart resumes cadence
	Run          func(ctx context.Context) error
	Timeout      time.Duration
	OnDone       func(name string, st TaskStatus) // after every run, outside the scheduler mutex (bridge persists store.SaveTask)
}

type TaskStatus struct {
	LastAttempt, LastSuccess          time.Time
	LastErr                           string
	Runs, Failures, Skipped, Overruns uint64
}

type taskState struct {
	Task
	running atomic.Bool
	trigger chan struct{}
	st      TaskStatus
}

type Scheduler struct {
	seed   uint64
	o      options
	runs   metric.Int64Counter
	over   metric.Int64Counter
	mu     sync.Mutex
	tasks  []*taskState
	byName map[string]*taskState
	frozen bool
}

func NewScheduler(seed uint64, opts ...Option) *Scheduler {
	o := buildOptions(opts)
	s := &Scheduler{seed: seed, o: o, byName: map[string]*taskState{}}
	s.runs, _ = o.deps.Meter.Int64Counter(MetricTaskRuns)
	s.over, _ = o.deps.Meter.Int64Counter(MetricTaskOverruns)
	return s
}

// Add registers a task before Run. It panics afterwards (frozen like readmodel.Registry) and on duplicates.
func (s *Scheduler) Add(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		panic("refresh: Scheduler.Add after Run")
	}
	if _, dup := s.byName[t.Name]; dup || t.Every <= 0 || t.Run == nil {
		panic("refresh: Scheduler.Add: duplicate name, non-positive Every or nil Run: " + t.Name)
	}
	ts := &taskState{Task: t, trigger: make(chan struct{}, 1)}
	s.tasks = append(s.tasks, ts)
	s.byName[t.Name] = ts
}

func (s *Scheduler) Status(name string) (TaskStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.byName[name]
	if t == nil {
		return TaskStatus{}, false
	}
	return t.st, true
}

// Trigger requests an out-of-band run; false (counted Skipped) if the task is running or unknown.
func (s *Scheduler) Trigger(name string) bool {
	s.mu.Lock()
	t := s.byName[name]
	if t == nil {
		s.mu.Unlock()
		return false
	}
	if t.running.Load() {
		t.st.Skipped++
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	select {
	case t.trigger <- struct{}{}:
	default:
	}
	return true
}

// Run blocks until ctx is done and every task goroutine returned. Each task owns a *rand.Rand seeded from
// (seed, name), so jitter is reproducible and independent of registration order.
func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	s.frozen = true
	tasks := append([]*taskState(nil), s.tasks...)
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range tasks {
		h := fnv.New64a()
		h.Write([]byte(t.Name))
		rnd := rand.New(rand.NewPCG(s.seed, h.Sum64()))
		wg.Go(func() { s.loop(ctx, t, rnd) })
	}
	wg.Wait()
	return nil
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
		d := t.Every
		if t.Jitter > 0 {
			d = time.Duration(float64(d) * (1 + t.Jitter*(2*rnd.Float64()-1)))
		}
		if time.Since(start) > t.Every {
			s.mu.Lock()
			t.st.Overruns++
			s.mu.Unlock()
			s.over.Add(ctx, 1, metric.WithAttributes(attribute.String("task", t.Name)))
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
	t.st.LastAttempt = time.Now()
	t.st.Runs++
	outcome := "ok"
	if err != nil {
		t.st.Failures++
		t.st.LastErr = err.Error()
		outcome = "error"
	} else {
		t.st.LastSuccess = t.st.LastAttempt
		t.st.LastErr = ""
	}
	st := t.st
	s.mu.Unlock()
	s.runs.Add(ctx, 1, metric.WithAttributes(attribute.String("task", t.Name), attribute.String("outcome", outcome)))
	if t.OnDone != nil {
		func() { // a panicking bridge (store write) must not kill the process or the task loop
			defer func() {
				if r := recover(); r != nil {
					s.o.deps.Log.LogAttrs(ctx, slog.LevelError, "task OnDone panicked", slog.String("task", t.Name), slog.Any("panic", r))
				}
			}()
			t.OnDone(t.Name, st)
		}()
	}
}
