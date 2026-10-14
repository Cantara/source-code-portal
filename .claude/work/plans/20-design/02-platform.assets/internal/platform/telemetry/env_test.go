package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// OTEL_* variables in the container environment (an operator, a sidecar injector) must not redirect or
// authenticate SCP's exporters: only the explicit config counts. This pins what the SDK really does.
func TestOTELEnvDoesNotRedirectOrLeak(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var hdrs []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		hdrs = append(hdrs, r.Header.Clone())
		mu.Unlock()
	}))
	defer srv.Close()
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("exporter followed OTEL_EXPORTER_OTLP_ENDPOINT to %s", r.URL)
	}))
	defer evil.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", evil.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", evil.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer from-env")
	t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "none")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	t.Setenv("OTEL_SDK_DISABLED", "true")

	c := Config{OTLP: OTLPConfig{Enabled: true, Endpoint: srv.URL, Timeout: 2 * time.Second, Signals: OTLPSignalsConfig{Traces: true}}, Traces: TracesConfig{SampleRatio: 1}}
	p, err := New(context.Background(), c, BuildInfo{Service: "scp"}, Options{Headers: map[string]string{"Authorization": "Bearer from-config"}})
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer("t").Start(context.Background(), "x")
	span.End()
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/v1/traces" {
		t.Fatalf("paths %v", paths)
	}
	if got := hdrs[0].Get("Authorization"); got != "Bearer from-config" {
		t.Fatalf("authorization %q (env must not win or merge)", got)
	}
	if got := hdrs[0].Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("compression is set explicitly (gzip); the environment said none and got %q", got)
	}
}
