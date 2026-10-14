package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/ghverify"
	"github.com/cantara/source-code-portal/internal/job"
	"github.com/cantara/source-code-portal/internal/refresh"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/webhook"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var secret = []byte("s3cr3t")

type ver struct{}

func (ver) Verify(src scm.SourceID, h http.Header, b []byte) (scm.Event, error) {
	return ghverify.Verify(src, [][]byte{secret}, h, b)
}

// fanFunc is the tiny stand-in of reactor.Fan: default-branch push -> enqueue commits.refresh for the repo.
type fanFunc struct{ enq job.Enqueuer }

func (f fanFunc) Handle(ctx context.Context, ev scm.Event) error {
	if ev.Kind == scm.EventPush && ev.Ref == "refs/heads/master" {
		return f.enq.Enqueue(job.NewKey("commits.refresh", string(ev.Source), ev.Repo.Owner, ev.Repo.Name), job.PrioWebhook, time.Time{}, false)
	}
	return nil
}

// Cold start of 1,000 repositories x 3 kinds at the github defaults (4 concurrent, 100 ms pace): every job
// runs exactly once, the drain takes pace x jobs, and the planner never schedules a second run.
func TestColdStartThreeThousandItems(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := refresh.New(refresh.Backoff{}, 1)
		e.AddUpstream("github", refresh.UpstreamConfig{MaxConcurrent: 4, Pace: 100 * time.Millisecond})
		var mu sync.Mutex
		runs := map[job.Key]int{}
		var peak, cur atomic.Int32
		var lastStart atomic.Int64
		start0 := time.Now()
		mk := func(kind string, ttl time.Duration) job.Spec {
			return job.Spec{Kind: kind, Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: freshness.Policy{TTL: ttl, Jitter: 0.1},
				Run: func(ctx context.Context, k job.Key) (job.Outcome, error) {
					n := cur.Add(1)
					for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
					}
					lastStart.Store(int64(time.Since(start0)))
					mu.Lock()
					runs[k]++
					mu.Unlock()
					time.Sleep(150 * time.Millisecond)
					cur.Add(-1)
					return job.Outcome{}, nil
				}}
		}
		specs := []job.Spec{mk("commits.refresh", 2*time.Hour), mk("docs.refresh", time.Hour), mk("manifests.refresh", 30*time.Minute)}
		pl := refresh.NewPlanner(e, nil, 1)
		for _, s := range specs {
			e.Register(s)
			pl.Track(s)
		}
		e.Observe(pl.OnResult)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- e.Run(ctx) }()

		var repos []string
		for i := range 1000 {
			repos = append(repos, fmt.Sprintf("r%04d", i))
		}
		for _, s := range specs {
			var keys []job.Key
			for _, r := range repos {
				keys = append(keys, job.NewKey(s.Kind, "acme", "o", r))
			}
			pl.SetSubjects(s.Kind, keys)
		}
		t0 := time.Now()
		// planner.tick every 15 s, as the scheduler task does
		for range 25 {
			pl.Tick(time.Now())
			time.Sleep(15 * time.Second)
		}
		synctest.Wait()
		mu.Lock()
		total := 0
		for _, n := range runs {
			total += n
		}
		mu.Unlock()
		elapsed := time.Since(t0)
		if total < 3000 || peak.Load() > 4 {
			t.Fatalf("total=%d peak=%d after %v", total, peak.Load(), elapsed)
		}
		for k, n := range runs {
			if n > 2 {
				t.Fatalf("%s ran %d times", k, n)
			}
		}
		// first-pass completion time: 3,000 starts at 10/s = 300 s (+ the last job)
		pr := pl.Progress("commits.refresh", time.Now())
		if pr.Completed != 1000 {
			t.Fatalf("%+v", pr)
		}
		st := e.Inspect()
		if st.Jobs["started"] < 3000 || st.Jobs["marked_dirty"] != 0 {
			t.Fatalf("a tick must never cause a dirty follow-up: %+v", st.Jobs)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		t.Logf("cold start: %d runs, last first-pass start at %v virtual time (pace 100ms => 300s lower bound for 3000 jobs), %v of ticking", total, time.Duration(lastStart.Load()), elapsed)
		if ls := time.Duration(lastStart.Load()); ls < 299*time.Second || ls > 330*time.Second {
			t.Fatalf("drain time %v", ls)
		}
	})
}

// Webhook -> receiver -> fan-out -> planner.Enqueue -> engine -> job; the end-to-end metric is recorded.
func TestWebhookToJobAndEndToEndMetric(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader := metric.NewManualReader()
		mp := metric.NewMeterProvider(metric.WithReader(reader))
		deps := refresh.Deps{Meter: mp.Meter("scp")}
		e := refresh.New(refresh.Backoff{}, 1, refresh.WithDeps(deps))
		e.AddUpstream("github", refresh.UpstreamConfig{MaxConcurrent: 4, Pace: 100 * time.Millisecond})
		var ran atomic.Int32
		sp := job.Spec{Kind: "commits.refresh", Upstream: "github", Scope: job.ScopeRepo, Prio: job.PrioScheduled, Policy: freshness.Policy{TTL: 2 * time.Hour, IdleBackoffMax: 12 * time.Hour},
			Run: func(ctx context.Context, k job.Key) (job.Outcome, error) {
				ran.Add(1)
				time.Sleep(300 * time.Millisecond)
				return job.Outcome{}, nil
			}}
		e.Register(sp)
		pl := refresh.NewPlanner(e, nil, 1, refresh.WithDeps(deps))
		pl.Track(sp)
		e.Observe(pl.OnResult)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- e.Run(ctx) }()
		pl.SetSubjects("commits.refresh", []job.Key{job.NewKey("commits.refresh", "acme", "acme", "some-project")})
		pl.Tick(time.Now())
		time.Sleep(time.Minute)

		rc, err := webhook.NewReceiver(webhook.DefaultConfig(), webhook.Sources{IDs: []scm.SourceID{"acme"}, Default: "acme"}, ver{},
			fanFunc{enq: pl}, nil, nil, webhook.Deps{Meter: mp.Meter("scp")})
		if err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"ref":"refs/heads/master","commits":[{"id":"a","added":[],"removed":[],"modified":["README.md"]}],"repository":{"id":1,"name":"some-project","owner":{"name":"acme"}}}`)
		r := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Hub-Signature-256", ghverify.Sign(secret, body))
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-GitHub-Delivery", "g-1")
		w := httptest.NewRecorder()
		rc.ServeHTTP(w, r)
		time.Sleep(time.Second)
		synctest.Wait()
		if w.Code != 202 || ran.Load() != 2 {
			t.Fatalf("code=%d ran=%d", w.Code, ran.Load())
		}
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatal(err)
		}
		var names []string
		var toPublish float64
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				names = append(names, m.Name)
				if m.Name == refresh.MetricToPublish {
					h := m.Data.(metricdata.Histogram[float64])
					toPublish = h.DataPoints[0].Sum
				}
			}
		}
		sort.Strings(names)
		t.Logf("instruments: %v", names)
		if toPublish < 0.29 || toPublish > 0.31 {
			t.Fatalf("webhook-to-publish = %v s, want about 0.3 (the run time)", toPublish)
		}
		cancel()
		<-done
	})
}
