package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/lifecycle"
)

// storeWorker is the lifecycle surface of *sqlite.Store (03 section 4.2): Run is the writer, backup and probe worker, Close is
// PRAGMA optimize, wal_checkpoint(TRUNCATE) and closing the three connections. Declared here by its consumer.
type storeWorker interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

// planFlusher is *bridge.PlanSink: Run flushes the planner's states every 5 s, Close is the final flush.
type planFlusher interface {
	Run(ctx context.Context) error
	Close(ctx context.Context) error
}

// storeStage is the lifecycle stage "store": the store worker and the plan flusher run side by side, and Close performs the final
// flush BEFORE the store closes. The stage stops after the refresh stage (reverse start order), so by the time Close runs the
// engine has joined and the planner has handed over its last states: nothing can enqueue and nothing can still be saved.
// If the flush fails the store is closed anyway and both errors are returned: losing the last 5 s of plan state only makes an item
// due early after the restart (04 section 5).
func storeStage(st storeWorker, plans planFlusher, timeout time.Duration) lifecycle.Stage {
	return lifecycle.Stage{
		Name:        "store",
		StopTimeout: timeout,
		Run: func(ctx context.Context) error {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var (
				wg    sync.WaitGroup
				mu    sync.Mutex
				first error
			)
			note := func(err error) {
				mu.Lock()
				if first == nil && err != nil {
					first = err
				}
				mu.Unlock()
				cancel() // either half ending ends the stage: a store without its flusher (or the reverse) is not a state to serve in
			}
			wg.Go(func() { note(st.Run(ctx)) })
			if plans != nil {
				wg.Go(func() { note(plans.Run(ctx)) })
			}
			wg.Wait()
			return first
		},
		Close: func(ctx context.Context) error {
			var flush error
			if plans != nil {
				flush = plans.Close(ctx)
			}
			return errors.Join(flush, st.Close(ctx))
		},
	}
}
