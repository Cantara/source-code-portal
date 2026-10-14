package manifests

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metric names (constants next to their instruments, G11). The generated docs/reference/metrics.md must list exactly these.
const (
	MetricParseTotal    = "scp_manifests_parse_total"           // {ecosystem,parser,outcome=ok|failed|limit|abandoned|panic}
	MetricParseSeconds  = "scp_manifests_parse_seconds"         // histogram, {ecosystem}
	MetricLimitHits     = "scp_manifests_limit_hits_total"      // {limit=size|depth|nodes|expand|timeout|tree-calls}
	MetricAbandoned     = "scp_manifests_abandoned_total"       // {parser}
	MetricRefreshTotal  = "scp_manifests_refresh_total"         // {outcome=changed|unchanged|failed|gone}
	MetricFilesTotal    = "scp_manifests_files_total"           // {result=parse_cache|blob_cache|fetched|failed|skipped}
	MetricFetchTotal    = "scp_manifests_fetch_total"           // {mode=batch|rest|sha_mismatch}
	MetricTreeCalls     = "scp_manifests_tree_calls_total"      // {mode=recursive|walk}
	MetricReactorTotal  = "scp_manifests_reactor_total"         // {event,result=enqueued_full|enqueued_paths|enqueued_head|enqueued_create|enqueued_repo|dropped|ignored}
	MetricWarmTotal     = "scp_manifests_warm_total"            // {result=ok|no_payload|undecodable}
	MetricLockedCache   = "scp_manifests_locked_cache_total"    // {result=hit|miss|miss_suppressed|corrupt}
	MetricDiagnostics   = "scp_manifests_diagnostics_total"     // {severity,namespace}
	MetricFactsBytes    = "scp_manifests_facts_payload_bytes"   // histogram
	MetricGraphRebuild  = "scp_manifests_graph_rebuild_seconds" // histogram
	MetricGraphPublish  = "scp_manifests_graph_publish_total"   // {result=published|unchanged}
	MetricUnits         = "scp_manifests_units"                 // gauge {ecosystem}
	MetricStaleRepos    = "scp_manifests_stale_repos"           // gauge
	MetricGraphUnits    = "scp_manifests_graph_units"           // gauge
	MetricGraphEdges    = "scp_manifests_graph_edges"           // gauge
	MetricGraphCycles   = "scp_manifests_graph_cycles"          // gauge
	MetricGraphDangling = "scp_manifests_graph_dangling_units"  // gauge
	MetricParseInflight = "scp_manifests_parse_inflight_bytes"  // gauge (weight held by running parses)
	MetricCircuitOpen   = "scp_manifests_circuit_open"          // gauge {parser}, 1 = parser disabled until restart
)

type metrics struct {
	parseTotal, limitHits, abandoned, refreshTotal, filesTotal, fetchTotal, treeCalls, reactorTotal, lockedCache, diagnostics, graphPublish, warmTotal metric.Int64Counter
	parseSeconds, graphRebuild, factsBytes                                                                                                             metric.Float64Histogram
	units, stale, gUnits, gEdges, gCycles, gDangling, inflight, circuit                                                                                metric.Int64ObservableGauge
}

func newMetrics(m metric.Meter) (*metrics, error) {
	var err error
	x := &metrics{}
	c := func(dst *metric.Int64Counter, name, desc string) {
		if err == nil {
			*dst, err = m.Int64Counter(name, metric.WithDescription(desc))
		}
	}
	h := func(dst *metric.Float64Histogram, name, desc, unit string) {
		if err == nil {
			*dst, err = m.Float64Histogram(name, metric.WithDescription(desc), metric.WithUnit(unit))
		}
	}
	g := func(dst *metric.Int64ObservableGauge, name, desc string) {
		if err == nil {
			*dst, err = m.Int64ObservableGauge(name, metric.WithDescription(desc))
		}
	}
	c(&x.parseTotal, MetricParseTotal, "Parse attempts by ecosystem, parser and outcome")
	c(&x.limitHits, MetricLimitHits, "Parse and discovery limits that were hit")
	c(&x.abandoned, MetricAbandoned, "Parser goroutines abandoned by the watchdog")
	c(&x.refreshTotal, MetricRefreshTotal, "manifests.refresh job outcomes")
	c(&x.filesTotal, MetricFilesTotal, "Candidate files by how they were served")
	c(&x.fetchTotal, MetricFetchTotal, "Blob reads by mode")
	c(&x.treeCalls, MetricTreeCalls, "Tree reads by mode")
	c(&x.reactorTotal, MetricReactorTotal, "Event reactor decisions")
	c(&x.lockedCache, MetricLockedCache, "Locked-component cache lookups by the API")
	c(&x.diagnostics, MetricDiagnostics, "Diagnostics emitted by refresh jobs")
	c(&x.warmTotal, MetricWarmTotal, "Persisted facts rows read at start-up")
	c(&x.graphPublish, MetricGraphPublish, "Graph job publications")
	h(&x.parseSeconds, MetricParseSeconds, "Parse duration", "s")
	h(&x.graphRebuild, MetricGraphRebuild, "Graph rebuild duration", "s")
	h(&x.factsBytes, MetricFactsBytes, "Persisted RepoFacts payload size", "By")
	g(&x.units, MetricUnits, "Units in the current snapshot by ecosystem")
	g(&x.stale, MetricStaleRepos, "Repositories whose manifest facts are stale or behind HEAD")
	g(&x.gUnits, MetricGraphUnits, "Units in the current graph")
	g(&x.gEdges, MetricGraphEdges, "Edges in the current graph")
	g(&x.gCycles, MetricGraphCycles, "Dependency cycles in the current graph")
	g(&x.gDangling, MetricGraphDangling, "Graph units whose repository is gone from the snapshot")
	g(&x.inflight, MetricParseInflight, "Memory weight held by running parses")
	g(&x.circuit, MetricCircuitOpen, "Parsers disabled by the watchdog circuit")
	return x, err
}

func attrs(kv ...string) metric.MeasurementOption {
	a := make([]attribute.KeyValue, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		a = append(a, attribute.String(kv[i], kv[i+1]))
	}
	return metric.WithAttributes(a...)
}

func (m *metrics) inc(c metric.Int64Counter, kv ...string) {
	c.Add(context.Background(), 1, attrs(kv...))
}
