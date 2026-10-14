package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/ghverify"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/scm"
)

var secret = []byte("It's a Secret to Everybody")

type realVerifier struct{}

func (realVerifier) Verify(src scm.SourceID, h http.Header, body []byte) (scm.Event, error) {
	return ghverify.Verify(src, [][]byte{secret}, h, body)
}

type recSink struct {
	mu  sync.Mutex
	evs []scm.Event
	err error
}

func (s *recSink) Handle(ctx context.Context, ev scm.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, ev)
	return s.err
}
func (s *recSink) n() int        { s.mu.Lock(); defer s.mu.Unlock(); return len(s.evs) }
func (s *recSink) set(err error) { s.mu.Lock(); s.err = err; s.mu.Unlock() }

type fakeLimiter struct{ left atomic.Int32 }

func (l *fakeLimiter) Allow(string) bool { return l.left.Add(-1) >= 0 }

func pushBody(t testing.TB) []byte {
	b, err := os.ReadFile("../../testdata/push_two_commits.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newRig(t testing.TB, cfg Config, sink Sink, ver Verifier, lim Limiter, log *slog.Logger) (*Receiver, http.Handler) {
	if ver == nil {
		ver = realVerifier{}
	}
	rc, err := NewReceiver(cfg, Sources{IDs: []scm.SourceID{"acme", "other"}, Default: "acme"}, ver, sink, lim,
		func(*http.Request) string { return "203.0.113.9" }, Deps{Log: log})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/github/{source}", rc)
	mux.Handle("POST /webhooks/github", rc)
	return rc, mux
}

func post(h http.Handler, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func signed(body []byte, event, delivery string) map[string]string {
	return map[string]string{"X-Hub-Signature-256": ghverify.Sign(secret, body), "X-GitHub-Event": event, "X-GitHub-Delivery": delivery}
}

func TestAcceptedPushReachesSink(t *testing.T) {
	sink := &recSink{}
	rc, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
	body := pushBody(t)
	for i, path := range []string{"/webhooks/github/acme", "/webhooks/github"} {
		w := post(h, path, body, signed(body, "push", fmt.Sprintf("g-%d", i)))
		if w.Code != 202 || !strings.Contains(w.Body.String(), `"accepted"`) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if sink.n() != 2 || sink.evs[0].Kind != scm.EventPush || sink.evs[0].Source != "acme" || len(sink.evs[0].ChangedPaths) != 2 {
		t.Fatalf("%+v", sink.evs)
	}
	if s := rc.Stats(); s.Results[ResAccepted] != 2 || s.DedupeSize != 2 || s.LastAccepted.IsZero() {
		t.Fatalf("%+v", s)
	}
}

func TestDuplicateWithinTTLAndAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &recSink{}
		_, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
		body := pushBody(t)
		hd := signed(body, "push", "same-guid")
		if w := post(h, "/webhooks/github", body, hd); w.Code != 202 {
			t.Fatal(w.Code)
		}
		w := post(h, "/webhooks/github", body, hd)
		if w.Code != 202 || !strings.Contains(w.Body.String(), "duplicate") || sink.n() != 1 {
			t.Fatalf("%d %s n=%d", w.Code, w.Body, sink.n())
		}
		time.Sleep(15*time.Minute + time.Second)
		post(h, "/webhooks/github", body, hd)
		if sink.n() != 2 {
			t.Fatalf("a redelivery after the TTL is processed again: n=%d", sink.n())
		}
	})
}

func TestFailedEnqueueIsNotRecordedSoRedeliveryIsAccepted(t *testing.T) {
	for _, bad := range []error{job.ErrQueueFull, job.ErrClosed, fmt.Errorf("wrapped: %w", job.ErrQueueFull), errors.Join(errors.New("x"), job.ErrClosed)} {
		sink := &recSink{err: bad}
		_, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
		body := pushBody(t)
		hd := signed(body, "push", "g1")
		w := post(h, "/webhooks/github", body, hd)
		if w.Code != 503 || w.Header().Get("Retry-After") != "5" {
			t.Fatalf("%v: %d %v", bad, w.Code, w.Header())
		}
		sink.set(nil)
		if w := post(h, "/webhooks/github", body, hd); w.Code != 202 || strings.Contains(w.Body.String(), "duplicate") {
			t.Fatalf("redelivery after a 503 must be processed: %d %s", w.Code, w.Body)
		}
	}
}

func TestOtherReactorErrorsAreAcceptedAndRecorded(t *testing.T) {
	sink := &recSink{err: errors.New("reactor panicked")}
	rc, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
	body := pushBody(t)
	if w := post(h, "/webhooks/github", body, signed(body, "push", "g")); w.Code != 202 {
		t.Fatal(w.Code)
	}
	if rc.Stats().DedupeSize != 1 {
		t.Fatal("recorded")
	}
}

func TestInvalidSignatureMatrix(t *testing.T) {
	sink := &recSink{}
	verified := 0
	counting := verifierFunc(func(src scm.SourceID, h http.Header, b []byte) (scm.Event, error) {
		verified++
		return realVerifier{}.Verify(src, h, b)
	})
	_, h := newRig(t, DefaultConfig(), sink, counting, nil, nil)
	body := pushBody(t)
	good := ghverify.Sign(secret, body)
	cases := map[string]map[string]string{
		"missing":     {"X-Hub-Signature-256": ""},
		"sha1 only":   {"X-Hub-Signature-256": "", "X-Hub-Signature": "sha1=" + good[7:47]},
		"short":       {"X-Hub-Signature-256": good[:30]},
		"not hex":     {"X-Hub-Signature-256": "sha256=" + strings.Repeat("z", 64)},
		"wrong value": {"X-Hub-Signature-256": ghverify.Sign([]byte("other"), body)},
	}
	for name, hd := range cases {
		all := signed(body, "push", "g-"+name)
		for k, v := range hd {
			all[k] = v
		}
		if w := post(h, "/webhooks/github", body, all); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if sink.n() != 0 {
		t.Fatal("sink must not see unauthenticated events")
	}
	if verified != 1 { // only "wrong value" is plausible enough to need HMAC work
		t.Fatalf("verifier called %d times: implausible headers are rejected before reading the body", verified)
	}
}

type verifierFunc func(scm.SourceID, http.Header, []byte) (scm.Event, error)

func (f verifierFunc) Verify(s scm.SourceID, h http.Header, b []byte) (scm.Event, error) {
	return f(s, h, b)
}

func TestInvalidSignatureBudgetGives429ButValidIsNeverLimited(t *testing.T) {
	lim := &fakeLimiter{}
	lim.left.Store(2)
	sink := &recSink{}
	_, h := newRig(t, DefaultConfig(), sink, nil, lim, nil)
	body := pushBody(t)
	bad := map[string]string{"X-Hub-Signature-256": ghverify.Sign([]byte("x"), body), "X-GitHub-Event": "push"}
	codes := []int{}
	for range 4 {
		codes = append(codes, post(h, "/webhooks/github", body, bad).Code)
	}
	if fmt.Sprint(codes) != "[401 401 429 429]" {
		t.Fatalf("%v", codes)
	}
	if w := post(h, "/webhooks/github", body, signed(body, "push", "ok")); w.Code != 202 {
		t.Fatalf("valid delivery from a throttled address: %d", w.Code)
	}
}

func TestRequestShapeRejections(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxBody = 256
	sink := &recSink{}
	_, h := newRig(t, cfg, sink, nil, nil, nil)
	body := pushBody(t) // > 256 B
	if w := post(h, "/webhooks/github", body, signed(body, "push", "a")); w.Code != 413 {
		t.Fatalf("content-length too large: %d", w.Code)
	}
	// chunked / unknown length: enforced by MaxBytesReader
	r := httptest.NewRequest("POST", "/webhooks/github", io.NopCloser(bytes.NewReader(body)))
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")
	for k, v := range signed(body, "push", "b") {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("chunked too large: %d", w.Code)
	}
	small := []byte(`{"zen":"x"}`)
	form := post(h, "/webhooks/github", small, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "X-Hub-Signature-256": ghverify.Sign(secret, small)})
	if form.Code != 415 {
		t.Fatalf("form encoded: %d", form.Code)
	}
	if w := post(h, "/webhooks/github/nope", small, signed(small, "ping", "p")); w.Code != 404 {
		t.Fatalf("unknown source: %d", w.Code)
	}
	if w := post(h, "/webhooks/github/other", small, signed(small, "ping", "p")); w.Code != 202 {
		t.Fatalf("second source: %d", w.Code)
	}
	if w := post(h, "/webhooks/github", small, signed(small, "ping", "p")); w.Code != 202 || !strings.Contains(w.Body.String(), "pong") || sink.n() != 0 {
		t.Fatalf("ping: %d %s sink=%d", w.Code, w.Body, sink.n())
	}
	if w := post(h, "/webhooks/github", small, signed(small, "star", "s")); w.Code != 202 || !strings.Contains(w.Body.String(), "ignored") || sink.n() != 0 {
		t.Fatalf("ignored event: %d %s", w.Code, w.Body)
	}
	garbage := []byte(`not json`)
	if w := post(h, "/webhooks/github", garbage, signed(garbage, "push", "m")); w.Code != 400 {
		t.Fatalf("malformed after auth: %d", w.Code)
	}
	unexpected := verifierFunc(func(scm.SourceID, http.Header, []byte) (scm.Event, error) { return scm.Event{}, errors.New("bug") })
	_, h2 := newRig(t, DefaultConfig(), sink, unexpected, nil, nil)
	if w := post(h2, "/webhooks/github", small, signed(small, "push", "u")); w.Code != 500 {
		t.Fatalf("unexpected verifier error is a 500, never a 400: %d", w.Code)
	}
	// a ServeMux answers other methods with 405 before the receiver is reached
	rq := httptest.NewRequest("GET", "/webhooks/github", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, rq)
	if rw.Code != 405 && rw.Code != 404 {
		t.Fatalf("GET: %d", rw.Code)
	}
}

func TestNoDefaultSourceMeansSourcelessRouteIs404(t *testing.T) {
	rc, err := NewReceiver(DefaultConfig(), Sources{IDs: []scm.SourceID{"a", "b"}}, realVerifier{}, &recSink{}, nil, nil, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	small := []byte(`{}`)
	r := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(small))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	rc.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	for name, f := range map[string]func() error{
		"nil verifier": func() error {
			_, e := NewReceiver(DefaultConfig(), Sources{IDs: []scm.SourceID{"a"}}, nil, &recSink{}, nil, nil, Deps{})
			return e
		},
		"no sources": func() error {
			_, e := NewReceiver(DefaultConfig(), Sources{}, realVerifier{}, &recSink{}, nil, nil, Deps{})
			return e
		},
		"zero config": func() error {
			_, e := NewReceiver(Config{}, Sources{IDs: []scm.SourceID{"a"}}, realVerifier{}, &recSink{}, nil, nil, Deps{})
			return e
		},
	} {
		if f() == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBodySlotsBoundConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxConcurrent = 2
		cfg.AcquireWait = 2 * time.Second
		release := make(chan struct{})
		var inside atomic.Int32
		slow := verifierFunc(func(s scm.SourceID, h http.Header, b []byte) (scm.Event, error) {
			inside.Add(1)
			<-release
			return realVerifier{}.Verify(s, h, b)
		})
		sink := &recSink{}
		_, h := newRig(t, cfg, sink, slow, nil, nil)
		body := []byte(`{"zen":"x"}`)
		codes := make(chan int, 3)
		for i := range 3 {
			go func() {
				codes <- post(h, "/webhooks/github", body, signed(body, "ping", fmt.Sprint(i))).Code
			}()
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if inside.Load() != 2 {
			t.Fatalf("inside=%d", inside.Load())
		}
		if c := <-codes; c != 503 {
			t.Fatalf("third request must give up with 503, got %d", c)
		}
		close(release)
		synctest.Wait()
		if a, b := <-codes, <-codes; a != 202 || b != 202 {
			t.Fatalf("%d %d", a, b)
		}
	})
}

type captureHandler struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (c *captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool { c.buf.WriteString(" " + a.Key + "=" + a.Value.String()); return true })
	c.buf.WriteString("\n")
	return nil
}
func (c *captureHandler) WithAttrs(a []slog.Attr) slog.Handler { return c }
func (c *captureHandler) WithGroup(string) slog.Handler        { return c }

func TestBodyIsNeverLoggedAndUnauthenticatedHeadersAreSanitised(t *testing.T) {
	ch := &captureHandler{}
	_, h := newRig(t, DefaultConfig(), &recSink{}, nil, nil, slog.New(ch))
	body := []byte(`{"canary":"SECRET-BODY-CANARY"}`)
	hd := map[string]string{"X-Hub-Signature-256": ghverify.Sign([]byte("wrong"), body), "X-GitHub-Event": "push", "X-GitHub-Delivery": "evil\nforged line " + strings.Repeat("A", 200)}
	if w := post(h, "/webhooks/github", body, hd); w.Code != 401 {
		t.Fatal(w.Code)
	}
	out := ch.buf.String()
	if strings.Contains(out, "CANARY") || strings.Contains(out, "forged line") && strings.Contains(out, "\nforged") {
		t.Fatalf("log leaked: %q", out)
	}
	if !strings.Contains(out, "delivery_id=") || strings.Count(out, "\n") != 1 {
		t.Fatalf("one line expected: %q", out)
	}
}

func TestConcurrentDeliveriesRace(t *testing.T) {
	sink := &recSink{}
	_, h := newRig(t, DefaultConfig(), sink, nil, nil, nil)
	body := pushBody(t)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Go(func() {
			if w := post(h, "/webhooks/github", body, signed(body, "push", fmt.Sprint(i%10))); w.Code != 202 {
				t.Errorf("%d", w.Code)
			}
		})
	}
	wg.Wait()
	// exactness is not required (two concurrent identical deliveries may both pass), but never more than the senders
	if n := sink.n(); n < 10 || n > 200 {
		t.Fatalf("n=%d", n)
	}
}

func TestDedupeEvictionAndNoRefreshOnHit(t *testing.T) {
	d := newDedupe(10*time.Minute, 3)
	t0 := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	d.Record("a", t0)
	d.Record("b", t0.Add(time.Minute))
	d.Record("c", t0.Add(2*time.Minute))
	d.Record("d", t0.Add(3*time.Minute)) // cap 3: a goes
	if d.Seen("a", t0.Add(3*time.Minute)) || !d.Seen("b", t0.Add(3*time.Minute)) {
		t.Fatal("FIFO eviction at the cap")
	}
	if !d.Seen("b", t0.Add(10*time.Minute+59*time.Second)) {
		t.Fatal("b lives until +11m")
	}
	for i := 0; i < 5; i++ { // hits do not extend life
		d.Seen("b", t0.Add(time.Duration(i)*time.Minute))
	}
	if d.Seen("b", t0.Add(11*time.Minute+time.Second)) {
		t.Fatal("expired")
	}
	if d.Len() > 3 {
		t.Fatal(d.Len())
	}
}

func FuzzReceiverNeverPanics(f *testing.F) {
	f.Add([]byte(`{}`), "push", "sha256="+strings.Repeat("a", 64), "application/json")
	f.Fuzz(func(t *testing.T, body []byte, event, sig, ct string) {
		rc, _ := NewReceiver(DefaultConfig(), Sources{IDs: []scm.SourceID{"acme"}, Default: "acme"}, realVerifier{}, &recSink{}, nil, nil, Deps{})
		r := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
		r.Header.Set("Content-Type", ct)
		r.Header.Set("X-GitHub-Event", event)
		r.Header.Set("X-Hub-Signature-256", sig)
		w := httptest.NewRecorder()
		rc.ServeHTTP(w, r)
		switch w.Code {
		case 202, 400, 401, 413, 415, 429, 500, 503:
		default:
			t.Fatalf("status %d", w.Code)
		}
		if w.Code == 202 && !strings.Contains(w.Body.String(), "pong") && !strings.Contains(w.Body.String(), "ignored") && !strings.Contains(w.Body.String(), "accepted") {
			t.Fatal(w.Body.String())
		}
	})
}

// ---- redelivery ----

type fakeRD struct {
	mu        sync.Mutex
	failed    []int64
	listErr   error
	redeliver func(id int64) error
	redone    []int64
	times     []time.Time
	since     time.Time
}

func (f *fakeRD) FailedDeliveries(ctx context.Context, src scm.SourceID, since time.Time) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = since
	return append([]int64(nil), f.failed...), f.listErr
}
func (f *fakeRD) Redeliver(ctx context.Context, src scm.SourceID, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redone = append(f.redone, id)
	f.times = append(f.times, time.Now())
	if f.redeliver != nil {
		return f.redeliver(id)
	}
	return nil
}

type memWM struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (w *memWM) Load(_ context.Context, n string) (time.Time, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.m[n], nil
}
func (w *memWM) Save(_ context.Context, n string, t time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m == nil {
		w.m = map[string]time.Time{}
	}
	w.m[n] = t
	return nil
}

type goneE struct{}

func (goneE) Error() string { return "gone" }
func (goneE) Gone() bool    { return true }

func rdCfg() RedeliveryConfig { c := DefaultRedelivery(); c.Enabled = true; c.MaxPerRun = 3; return c }

func TestRedeliveryPacesAdvancesWatermarkAndSkipsGone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := &fakeRD{failed: []int64{30, 20, 10}}
		fr.redeliver = func(id int64) error {
			if id == 20 {
				return goneE{}
			}
			return nil
		}
		wm := &memWM{}
		r := NewRedelivery([]RedeliverySource{{"acme", rdCfg()}}, fr, &recSink{}, wm, Deps{})
		specs := r.Jobs()
		if len(specs) != 1 || specs[0].Kind != "webhook.redelivery" || specs[0].Scope != job.ScopeGlobal || specs[0].Policy.TTL != 6*time.Hour {
			t.Fatalf("%+v", specs)
		}
		start := time.Now()
		out, err := specs[0].Run(context.Background(), job.NewKey(RedeliveryKind, "_"))
		if err != nil || out.Unchanged {
			t.Fatalf("%v %+v", err, out)
		}
		if len(fr.redone) != 3 || fr.times[1].Sub(fr.times[0]) != time.Second || fr.times[2].Sub(fr.times[1]) != time.Second {
			t.Fatalf("pacing: %v %v", fr.redone, fr.times)
		}
		if got := fr.since; start.Sub(got) != 24*time.Hour {
			t.Fatalf("first run looks back 24h, got %v", start.Sub(got))
		}
		if w := wm.m["webhook.redelivery/acme"]; !w.Equal(start.Add(-time.Minute)) {
			t.Fatalf("watermark %v", w)
		}
		// nothing failed: Unchanged
		fr.failed = nil
		if out, _ := r.Run(context.Background(), ""); !out.Unchanged {
			t.Fatal("idle run must report Unchanged")
		}
	})
}

func TestRedeliveryTruncatedRunDoesNotAdvanceAndErrorsPropagate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := &fakeRD{failed: []int64{5, 4, 3, 2, 1}}
		wm := &memWM{}
		r := NewRedelivery([]RedeliverySource{{"acme", rdCfg()}}, fr, &recSink{}, wm, Deps{})
		if _, err := r.Run(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if len(fr.redone) != 3 || len(wm.m) != 0 {
			t.Fatalf("cap 3, no watermark advance: %v %v", fr.redone, wm.m)
		}
		fr.failed = []int64{9}
		fr.redeliver = func(int64) error { return rlE{} }
		_, err := r.Run(context.Background(), "")
		if _, is := job.AsRateLimited(err); !is || len(wm.m) != 0 {
			t.Fatalf("a rate limit reaches the engine unchanged and keeps the watermark: %v", err)
		}
	})
}

type rlE struct{}

func (rlE) Error() string      { return "rl" }
func (rlE) RetryAt() time.Time { return time.Now().Add(time.Minute) }

func TestRedeliveryGapEmitsResyncAndLimitsWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fr := &fakeRD{}
		sink := &recSink{}
		wm := &memWM{m: map[string]time.Time{"webhook.redelivery/acme": time.Now().Add(-100 * time.Hour)}}
		r := NewRedelivery([]RedeliverySource{{"acme", rdCfg()}}, fr, sink, wm, Deps{})
		now := time.Now()
		if _, err := r.Run(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if sink.n() != 1 || sink.evs[0].Kind != scm.EventInstallationRepos || sink.evs[0].Action != "resync" || sink.evs[0].Source != "acme" {
			t.Fatalf("%+v", sink.evs)
		}
		if now.Sub(fr.since) != 72*time.Hour {
			t.Fatalf("window %v", now.Sub(fr.since))
		}
		// a queue-full refusal of the resync is an error: the engine retries the whole run
		sink.set(job.ErrQueueFull)
		wm.m["webhook.redelivery/acme"] = time.Now().Add(-100 * time.Hour)
		if _, err := r.Run(context.Background(), ""); !errors.Is(err, job.ErrQueueFull) {
			t.Fatalf("%v", err)
		}
	})
}

func TestRedeliveryDisabledHasNoJob(t *testing.T) {
	c := rdCfg()
	c.Enabled = false
	if jobs := NewRedelivery([]RedeliverySource{{"acme", c}}, &fakeRD{}, &recSink{}, nil, Deps{}).Jobs(); jobs != nil {
		t.Fatalf("%v", jobs)
	}
}
