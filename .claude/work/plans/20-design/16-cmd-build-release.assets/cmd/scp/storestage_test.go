package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type stageLog struct {
	mu  sync.Mutex
	log []string
}

func (r *stageLog) add(s string) { r.mu.Lock(); r.log = append(r.log, s); r.mu.Unlock() }

type fakeWorker struct {
	name     string
	r        *stageLog
	closeErr error
}

func (f *fakeWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	f.r.add(f.name + ".run-returned")
	return nil
}
func (f *fakeWorker) Close(context.Context) error { f.r.add(f.name + ".close"); return f.closeErr }

// Close order is the contract: the final plan flush first, then the store closes; Run returns only when both halves returned.
func TestStoreStageFlushesBeforeTheStoreCloses(t *testing.T) {
	r := &stageLog{}
	st := storeStage(&fakeWorker{name: "store", r: r}, &fakeWorker{name: "plans", r: r}, 3*time.Second)
	if st.Name != "store" || st.StopTimeout != 3*time.Second {
		t.Fatalf("stage = %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- st.Run(ctx) }()
	cancel()
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	if err := st.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	closes := []string{}
	for _, e := range r.log {
		if e == "plans.close" || e == "store.close" {
			closes = append(closes, e)
		}
	}
	if len(closes) != 2 || closes[0] != "plans.close" || closes[1] != "store.close" {
		t.Errorf("close order = %v, want [plans.close store.close]", closes)
	}
	if len(r.log) != 4 {
		t.Errorf("log = %v, want both runs returned before the closes", r.log)
	}
}

// A failing flush must not leave the store open: both errors come back.
func TestStoreStageClosesTheStoreEvenWhenTheFlushFails(t *testing.T) {
	r := &stageLog{}
	flushErr, closeErr := errors.New("flush failed"), errors.New("close failed")
	st := storeStage(&fakeWorker{name: "store", r: r, closeErr: closeErr}, &fakeWorker{name: "plans", r: r, closeErr: flushErr}, time.Second)
	err := st.Close(context.Background())
	if !errors.Is(err, flushErr) || !errors.Is(err, closeErr) {
		t.Fatalf("err = %v, want both", err)
	}
	if len(r.log) != 2 || r.log[1] != "store.close" {
		t.Errorf("log = %v", r.log)
	}
}

type failing struct{ err error }

func (f failing) Run(context.Context) error { return f.err }
func (failing) Close(context.Context) error { return nil }

// A worker that ends by itself ends the stage with its error and stops the other half.
func TestStoreStageEndsWhenOneHalfFails(t *testing.T) {
	r := &stageLog{}
	boom := errors.New("writer died")
	st := storeStage(failing{boom}, &fakeWorker{name: "plans", r: r}, time.Second)
	done := make(chan error, 1)
	go func() { done <- st.Run(context.Background()) }()
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stage did not end")
	}
}
