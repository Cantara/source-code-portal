package telemetry

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
)

func TestProviderMetricsNames(t *testing.T) {
	c := Config{Prometheus: true, Traces: TracesConfig{SampleRatio: 0.1}}
	p, err := New(context.Background(), c, BuildInfo{Service: "scp", Version: "1.2.3", Commit: "abc"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(context.Background())
	m := p.Meter("test")
	ctr, _ := m.Int64Counter("scp_sync_jobs_total")
	ctr.Add(context.Background(), 2)
	h, _ := m.Float64Histogram("scp_job_duration_seconds", metricUnit("s"))
	h.Record(context.Background(), 0.03)

	rec := httptest.NewRecorder()
	p.MetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`scp_sync_jobs_total 2`,
		`scp_job_duration_seconds_bucket{le="0.05"} 1`,
		`scp_build_info{commit="abc",go="` + runtime.Version() + `",version="1.2.3"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "otel_scope") || strings.Contains(body, "target_info") {
		t.Errorf("scope/target info leaked:\n%s", body)
	}
}

func TestValidate(t *testing.T) {
	var got []string
	Config{OTLP: OTLPConfig{Enabled: true, Endpoint: "ftp://x"}}.Validate(func(p, m string) { got = append(got, p) })
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func metricUnit(u string) metric.Float64HistogramOption { return metric.WithUnit(u) }

func TestValidateEndpointShapeAndNewTrimsTrailingSlash(t *testing.T) {
	var got []string
	Config{OTLP: OTLPConfig{Enabled: true, Endpoint: "https://u:p@collector:4318", Signals: OTLPSignalsConfig{Traces: true}}}.Validate(func(p, m string) { got = append(got, p+": "+m) })
	if len(got) != 1 || !strings.Contains(got[0], "userinfo") {
		t.Fatalf("userinfo in the endpoint must be refused: %v", got)
	}
	got = nil
	Config{OTLP: OTLPConfig{Enabled: true, Endpoint: "https://collector:4318?x=1", Signals: OTLPSignalsConfig{Traces: true}}}.Validate(func(p, m string) { got = append(got, p) })
	if len(got) != 1 {
		t.Fatalf("query in the endpoint must be refused: %v", got)
	}
}
