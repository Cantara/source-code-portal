package main

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/cantara/source-code-portal/internal/snapshot"
)

// metrics owns the registry. Packages report through plain hooks; only this file knows Prometheus.
type metrics struct {
	reg        *prometheus.Registry
	requests   *prometheus.CounterVec
	durations  *prometheus.HistogramVec
	deliveries *prometheus.CounterVec
	jobs       *prometheus.CounterVec
	jobTime    *prometheus.HistogramVec
	github     *prometheus.CounterVec
}

// gauges are read at scrape time from the live state.
type gauges struct {
	current func() *snapshot.Snapshot
	depth   func() (waiting, running int)
	rate    func() map[string]float64
}

func newMetrics(g gauges) *metrics {
	m := &metrics{reg: prometheus.NewRegistry()}
	m.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_http_requests_total", Help: "HTTP requests served."},
		[]string{"listener", "pattern", "method", "code"})
	m.durations = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scp_http_request_duration_seconds",
		Help: "Time to serve an HTTP request.", Buckets: prometheus.DefBuckets}, []string{"listener", "pattern"})
	m.deliveries = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_webhook_deliveries_total", Help: "GitHub webhook deliveries."},
		[]string{"event", "result"})
	m.jobs = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_sync_jobs_total", Help: "Finished sync jobs."}, []string{"kind", "result"})
	m.jobTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scp_sync_job_duration_seconds", Help: "Time a sync job ran.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}}, []string{"kind"})
	m.github = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_github_requests_total", Help: "GitHub API responses by status."},
		[]string{"source", "code"})
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scp_build_info", Help: "Build of the running binary."},
		[]string{"version", "commit", "goversion"})
	info.WithLabelValues(version, build, runtime.Version()).Set(1)
	m.reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.durations, m.deliveries, m.jobs, m.jobTime, m.github, info, newLive(g))
	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{Registry: m.reg})
}

func (m *metrics) request(listener, pattern, method string, status int, d time.Duration) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		method = "other"
	}
	m.requests.WithLabelValues(listener, pattern, method, strconv.Itoa(status)).Inc()
	m.durations.WithLabelValues(listener, pattern).Observe(d.Seconds())
}

func (m *metrics) delivery(event, result string) { m.deliveries.WithLabelValues(event, result).Inc() }

func (m *metrics) job(kind, result string, d time.Duration) {
	m.jobs.WithLabelValues(kind, result).Inc()
	m.jobTime.WithLabelValues(kind).Observe(d.Seconds())
}

func (m *metrics) githubResponse(source string, status int) {
	m.github.WithLabelValues(source, strconv.Itoa(status)).Inc()
}

// live collects the gauges that describe the current state.
type live struct {
	g                            gauges
	age, items, queue, run, rate *prometheus.Desc
}

func newLive(g gauges) *live {
	return &live{g: g,
		age:   prometheus.NewDesc("scp_snapshot_age_seconds", "Time since the serving snapshot was built.", nil, nil),
		items: prometheus.NewDesc("scp_items", "Items in the snapshot by kind and freshness.", []string{"kind", "freshness"}, nil),
		queue: prometheus.NewDesc("scp_sync_queue_depth", "Sync jobs due and waiting.", nil, nil),
		run:   prometheus.NewDesc("scp_sync_running", "Sync jobs running.", nil, nil),
		rate: prometheus.NewDesc("scp_github_rate_remaining_ratio",
			"Smallest fraction of the GitHub core rate limit left over the source's credentials.", []string{"source"}, nil),
	}
}

func (l *live) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{l.age, l.items, l.queue, l.run, l.rate} {
		ch <- d
	}
}

func (l *live) Collect(ch chan<- prometheus.Metric) {
	s, now := l.g.current(), time.Now()
	if !s.BuiltAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(l.age, prometheus.GaugeValue, now.Sub(s.BuiltAt).Seconds())
	}
	counts := map[[2]string]int{}
	for _, e := range s.Repos {
		for kind, f := range map[string]snapshot.Freshness{"repo": e.Fresh, "commits": e.CommitsFresh, "docs": e.DocsFresh,
			"descriptor": e.DescriptorFresh, "release": e.ReleaseFresh, "manifests": e.ManifestsFresh} {
			counts[[2]string{kind, f.Derive(now).String()}]++
		}
	}
	for k, n := range counts {
		ch <- prometheus.MustNewConstMetric(l.items, prometheus.GaugeValue, float64(n), k[0], k[1])
	}
	waiting, running := l.g.depth()
	ch <- prometheus.MustNewConstMetric(l.queue, prometheus.GaugeValue, float64(waiting))
	ch <- prometheus.MustNewConstMetric(l.run, prometheus.GaugeValue, float64(running))
	for src, f := range l.g.rate() {
		ch <- prometheus.MustNewConstMetric(l.rate, prometheus.GaugeValue, f, src)
	}
}
