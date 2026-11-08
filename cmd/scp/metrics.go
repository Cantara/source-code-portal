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
	private    *prometheus.CounterVec
	durations  *prometheus.HistogramVec
	deliveries *prometheus.CounterVec
	jobs       *prometheus.CounterVec
	jobTime    *prometheus.HistogramVec
	github     *prometheus.CounterVec
	asks       *prometheus.CounterVec
	askTokens  *prometheus.CounterVec
	searches   *prometheus.CounterVec
	searchTime *prometheus.HistogramVec
}

// gauges are read at scrape time from the live state.
type gauges struct {
	current func() *snapshot.Snapshot
	depth   func() (waiting, running int)
	rate    func() map[string]float64
	leader  func() bool // nil when the instance is alone
	streams func() int64
}

func newMetrics(g gauges) *metrics {
	m := &metrics{reg: prometheus.NewRegistry()}
	m.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_http_requests_total", Help: "HTTP requests served."},
		[]string{"listener", "pattern", "method", "code"})
	m.private = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_private_view_requests_total",
		Help: "Requests answered from the private view (each leaves an audit log line), by route pattern and status."}, []string{"pattern", "code"})
	m.durations = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scp_http_request_duration_seconds",
		Help: "Time to serve an HTTP request.", Buckets: prometheus.DefBuckets}, []string{"listener", "pattern"})
	m.deliveries = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_webhook_deliveries_total", Help: "GitHub webhook deliveries."},
		[]string{"event", "result"})
	m.jobs = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_sync_jobs_total", Help: "Finished sync jobs."}, []string{"kind", "result"})
	m.jobTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scp_sync_job_duration_seconds", Help: "Time a sync job ran.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}}, []string{"kind"})
	m.github = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_github_requests_total", Help: "GitHub API responses by status."},
		[]string{"source", "code"})
	m.asks = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_assistant_requests_total", Help: "Assistant requests by outcome."},
		[]string{"outcome"})
	m.askTokens = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_assistant_tokens_total", Help: "Language model tokens the assistant used."},
		[]string{"kind"})
	m.searches = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "scp_search_requests_total", Help: "Searches and suggestions answered."},
		[]string{"op"})
	m.searchTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "scp_search_duration_seconds", Help: "Time a search took.",
		Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1}}, []string{"op"})
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "scp_build_info", Help: "Build of the running binary."},
		[]string{"version", "commit", "goversion"})
	info.WithLabelValues(version, build, runtime.Version()).Set(1)
	m.reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.private, m.durations, m.deliveries, m.jobs, m.jobTime, m.github, m.asks, m.askTokens, m.searches, m.searchTime, info, newLive(g))
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

// privateView counts a request of the private view by the route pattern and the status: the audit line's counter, with no viewer and no
// repository in a label (a pattern holds {owner}/{name}, never a name).
func (m *metrics) privateView(pattern string, status int) {
	m.private.WithLabelValues(pattern, strconv.Itoa(status)).Inc()
}

func (m *metrics) search(op string, d time.Duration) {
	m.searches.WithLabelValues(op).Inc()
	m.searchTime.WithLabelValues(op).Observe(d.Seconds())
}

func (m *metrics) delivery(event, result string) { m.deliveries.WithLabelValues(event, result).Inc() }

func (m *metrics) assistant(outcome string, input, output int) {
	m.asks.WithLabelValues(outcome).Inc()
	m.askTokens.WithLabelValues("input").Add(float64(input))
	m.askTokens.WithLabelValues("output").Add(float64(output))
}

func (m *metrics) job(kind, result string, d time.Duration) {
	m.jobs.WithLabelValues(kind, result).Inc()
	m.jobTime.WithLabelValues(kind).Observe(d.Seconds())
}

func (m *metrics) githubResponse(source string, status int) {
	m.github.WithLabelValues(source, strconv.Itoa(status)).Inc()
}

// live collects the gauges that describe the current state.
type live struct {
	g                                             gauges
	age, items, queue, run, rate, leader, streams *prometheus.Desc
}

func newLive(g gauges) *live {
	return &live{g: g,
		age:   prometheus.NewDesc("scp_snapshot_age_seconds", "Time since the serving snapshot was built.", nil, nil),
		items: prometheus.NewDesc("scp_items", "Items in the snapshot by kind and freshness.", []string{"kind", "freshness"}, nil),
		queue: prometheus.NewDesc("scp_sync_queue_depth", "Sync jobs due and waiting.", nil, nil),
		run:   prometheus.NewDesc("scp_sync_running", "Sync jobs running.", nil, nil),
		rate: prometheus.NewDesc("scp_github_rate_remaining_ratio",
			"Smallest fraction of the GitHub core rate limit left over the source's credentials.", []string{"source"}, nil),
		leader:  prometheus.NewDesc("scp_cluster_leader", "1 when this instance leads the cluster and runs the sync jobs.", nil, nil),
		streams: prometheus.NewDesc("scp_event_streams", "Open server-sent event streams.", nil, nil),
	}
}

func (l *live) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{l.age, l.items, l.queue, l.run, l.rate, l.leader, l.streams} {
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
		counts[[2]string{"repo", e.Fresh.Derive(now).String()}]++
		for kind, f := range e.Facets() {
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
	if l.g.leader != nil {
		v := 0.0
		if l.g.leader() {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(l.leader, prometheus.GaugeValue, v)
	}
	if l.g.streams != nil {
		ch <- prometheus.MustNewConstMetric(l.streams, prometheus.GaugeValue, float64(l.g.streams()))
	}
}
