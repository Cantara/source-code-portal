package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/scm"
)

// Regression tests added by the independent verification of design unit 04 (2026-10-14).

// deadlineWriter is a ResponseWriter that supports ResponseController.SetReadDeadline, and slowBody is a body
// that honours it, like net/http's server connection does.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline atomic.Value // time.Time
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error { w.deadline.Store(t); return nil }

type slowBody struct{ w *deadlineWriter }

func (b slowBody) Read([]byte) (int, error) {
	if d, ok := b.w.deadline.Load().(time.Time); ok {
		time.Sleep(time.Until(d))
		return 0, os.ErrDeadlineExceeded
	}
	select {} // no deadline set: would hang forever (the defect)
}
func (slowBody) Close() error { return nil }

// A sender with a plausible signature header and a never-finishing body must not hold a body slot for longer
// than BodyTimeout; before the fix four such requests starved every real delivery for the server's 30 s.
func TestSlowBodyReleasesItsSlotAtBodyTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxConcurrent = 1
		cfg.AcquireWait = time.Second
		sink := &recSink{}
		_, h := newRig(t, cfg, sink, nil, nil, nil)
		body := []byte(`{"zen":"x"}`)
		w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		r := httptest.NewRequest("POST", "/webhooks/github", nil)
		r.Body = slowBody{w}
		r.ContentLength = 1000
		r.Header.Set("Content-Type", "application/json")
		for k, v := range signed(body, "ping", "slow") {
			r.Header.Set(k, v)
		}
		done := make(chan struct{})
		go func() { h.ServeHTTP(w, r); close(done) }()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		// the only slot is held by the slow sender: a real delivery gives up with 503 after AcquireWait
		if c := post(h, "/webhooks/github", body, signed(body, "ping", "real")).Code; c != 503 {
			t.Fatalf("real delivery while the slot is held: %d", c)
		}
		time.Sleep(cfg.BodyTimeout)
		synctest.Wait()
		<-done
		if w.Code != 408 {
			t.Fatalf("slow body: %d", w.Code)
		}
		if c := post(h, "/webhooks/github", body, signed(body, "ping", "real2")).Code; c != 202 {
			t.Fatalf("slot must be free after the timeout: %d", c)
		}
	})
}

func TestHostileDeliveryIdFromAVerifiedSenderIsRefusedAndNeverStored(t *testing.T) {
	sink := &recSink{}
	rc, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
	body := []byte(`{"zen":"x"}`)
	for _, id := range []string{"", strings.Repeat("a", 65), "a\"b", "a b", "a/b", "é"} {
		hd := signed(body, "ping", id)
		if id == "" {
			delete(hd, "X-GitHub-Delivery")
		}
		w := post(h, "/webhooks/github", body, hd)
		if w.Code != 400 || strings.Contains(w.Body.String(), id+"\"") && id != "" {
			t.Fatalf("%q: %d %s", id, w.Code, w.Body)
		}
	}
	if rc.Stats().DedupeSize != 0 || sink.n() != 0 {
		t.Fatal("nothing may be recorded or forwarded")
	}
	// Ignored events echo a sanitised id, always valid JSON
	w := post(h, "/webhooks/github", body, signed(body, "star", "ok-1"))
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"ignored"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

type invalidSigErr struct{}

func (invalidSigErr) Error() string          { return "bad signature" }
func (invalidSigErr) InvalidSignature() bool { return true }

// Unit 05's WebhookError names the behaviour InvalidSignature(); both spellings give 401, never 500.
func TestInvalidSignatureSpellingOfUnit05Gives401(t *testing.T) {
	sink := &recSink{}
	_, h := newRig(t, DefaultConfig(), sink, verifierFunc(func(scm.SourceID, http.Header, []byte) (scm.Event, error) {
		return scm.Event{}, invalidSigErr{}
	}), nil, nil)
	body := []byte(`{}`)
	if w := post(h, "/webhooks/github", body, signed(body, "push", "x1")); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

type truncErr struct{}

func (truncErr) Error() string   { return "page cap reached" }
func (truncErr) Truncated() bool { return true }

// The adapter stops at its page cap: the ids it found are redelivered, the watermark must not advance.
func TestRedeliveryAdapterTruncationKeepsWatermark(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := &fakeRD{failed: []int64{1, 2}, listErr: truncErr{}}
		wm := &memWM{}
		r := NewRedelivery([]RedeliverySource{{"acme", rdCfg()}}, fr, &recSink{}, wm, Deps{})
		if _, err := r.Run(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if len(fr.redone) != 2 || len(wm.m) != 0 {
			t.Fatalf("redone=%v watermark=%v", fr.redone, wm.m)
		}
		fr.listErr = errors.New("boom") // any other list error fails the run
		if _, err := r.Run(context.Background(), ""); err == nil {
			t.Fatal("list error swallowed")
		}
	})
}

func TestRedeliverySpecIsValidAgainstTheKernelRules(t *testing.T) {
	r := NewRedelivery([]RedeliverySource{{"a", rdCfg()}, {"b", func() RedeliveryConfig { c := rdCfg(); c.Interval = time.Hour; return c }()}}, &fakeRD{}, &recSink{}, &memWM{}, Deps{})
	sp := r.Jobs()
	if len(sp) != 1 {
		t.Fatal(len(sp))
	}
	s := sp[0]
	// unit 01 JB-03: Scope and Prio set, Policy TTL > 0, Jitter <= 0.5, IdleBackoffMax 0 or >= TTL, Upstream pattern, Timeout >= 0
	if s.Kind != RedeliveryKind || s.Upstream != "github" || s.Scope != job.ScopeGlobal || s.Prio != job.PrioScheduled ||
		s.Policy.TTL != time.Hour || s.Policy.Jitter != 0.1 || s.Policy.IdleBackoffMax != 0 || s.Timeout <= 0 || s.Run == nil {
		t.Fatalf("%+v", s)
	}
}

var _ = io.EOF
