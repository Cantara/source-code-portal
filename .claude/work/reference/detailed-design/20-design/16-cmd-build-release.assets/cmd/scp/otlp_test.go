package main

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

func TestHeadersFileParser(t *testing.T) {
	good := "# collector credentials\r\nauthorization: Bearer abcdef0123456789\r\n\r\n  X-Scope-OrgID :  tenant-7  \n"
	h, secrets, problems := parseHeaders(good)
	if len(problems) != 0 || h["Authorization"] != "Bearer abcdef0123456789" || h["X-Scope-Orgid"] != "tenant-7" || len(secrets) != 2 {
		t.Fatalf("good: %v %v %v", h, secrets, problems)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"no colon":     {"Authorization Bearer x", ":1: expected"},
		"bad name":     {"Auth orization: x", ":1: the header name"},
		"empty value":  {"Authorization:   ", ":1: empty value"},
		"control":      {"Authorization: a\x01b", ":1: the value contains a control character"},
		"duplicate":    {"Authorization: first-value-1\nauthorization: second-value-2", ":2: duplicate header Authorization"},
		"second error": {"X-A: ok\nbroken", ":2: expected"},
	} {
		_, _, p := parseHeaders(tc.in)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), tc.want) {
			t.Errorf("%s: %v, want %q", name, p, tc.want)
		}
		for _, line := range p { // a problem never echoes a value
			if strings.Contains(line, "Bearer") || strings.Contains(line, "second-value-2") || strings.Contains(line, "\x01") {
				t.Errorf("%s: a value leaked into %q", name, line)
			}
		}
	}
	var many strings.Builder
	for i := range maxHeaders + 1 {
		many.WriteString("X-H" + strings.Repeat("a", i) + ": v\n")
	}
	if _, _, p := parseHeaders(many.String()); len(p) == 0 {
		t.Error("more than 32 headers must be refused")
	}
}

func TestHeadersFileCapAndMissing(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("# padding\n"), maxHeadersFile/5), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, p := parseHeadersFile(big); len(p) != 1 || !strings.Contains(p[0], "larger than") {
		t.Errorf("cap: %v", p)
	}
	if _, _, p := parseHeadersFile(filepath.Join(t.TempDir(), "nope")); len(p) != 1 || !strings.Contains(p[0], "headers_file") {
		t.Errorf("missing: %v", p)
	}
}

func FuzzParseHeaders(f *testing.F) {
	for _, s := range []string{"", "a: b", "# c\nA: b\r\nA: c", ":", "x:\x00", strings.Repeat("a: b\n", 40)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		h, secrets, problems := parseHeaders(in)
		if (len(problems) > 0) == (h != nil) {
			t.Fatalf("either headers or problems: %v %v", h, problems)
		}
		if len(secrets) != len(h) {
			t.Fatalf("a secret per header: %d %d", len(secrets), len(h))
		}
		for _, p := range problems {
			for _, v := range h {
				if v != "" && strings.Contains(p, v) {
					t.Fatalf("value %q leaked into %q", v, p)
				}
			}
		}
	})
}

func TestSecretsOfFindsEverySecretOfTheConfig(t *testing.T) {
	var cfg Config
	cfg.Server.Admin.BearerToken = secret.New("admin-token-0123456789")
	got := secretsOf(&cfg)
	if len(got) != 1 || got[0].Reveal() != "admin-token-0123456789" {
		t.Fatalf("%v", got)
	}
	if len(secretsOf(&Config{})) != 0 {
		t.Error("an unset secret is not a scrub value")
	}
	// ...and they reach the log handler: a credential formatted into a message by hand is masked.
	var buf bytes.Buffer
	log := slog.New(logx.NewHandler(&buf, logx.Options{Level: slog.LevelInfo, Format: "json", Service: "scp", Scrub: got}))
	log.InfoContext(t.Context(), "oops admin-token-0123456789")
	if strings.Contains(buf.String(), "admin-token-0123456789") {
		t.Errorf("the credential reached the log: %s", buf.String())
	}
}

func TestOTELErrorHandlerRoutedToLogx(t *testing.T) {
	var buf bytes.Buffer
	routeOTelErrors(t.Context(), slog.New(logx.NewHandler(&buf, logx.Options{Level: slog.LevelInfo, Format: "json", Service: "scp"})))
	t.Cleanup(func() { otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {})) })
	otel.Handle(errors.New("export failed"))
	if out := buf.String(); !strings.Contains(out, `"level":"WARN"`) && !strings.Contains(out, `"level":"warn"`) || !strings.Contains(out, "export failed") || !strings.Contains(out, "otel") {
		t.Errorf("the SDK error did not reach the structured log: %q", out)
	}
}

func TestOTLPClientIsPinnedAndOffWhenDisabled(t *testing.T) {
	if c, err := otlpClient(telemetry.OTLPConfig{}); c != nil || err != nil {
		t.Fatalf("disabled: %v %v", c, err)
	}
	if _, err := otlpClient(telemetry.OTLPConfig{Enabled: true, Endpoint: "not a url"}); err == nil {
		t.Error("an endpoint without a host must be refused")
	}
	if _, err := otlpClient(telemetry.OTLPConfig{Enabled: true, Endpoint: "https://otel.example.org:4318", AllowPrivateCIDRs: []string{"0.0.0.0/0"}}); err == nil {
		t.Error("a /0 range must be refused")
	}
	c, err := otlpClient(telemetry.OTLPConfig{Enabled: true, Endpoint: "https://otel.example.org:4318", Ports: []int{4318}})
	if err != nil || c == nil {
		t.Fatalf("%v", err)
	}
	// A request to any other host fails before a connection is made (the host pin), whatever the policy says.
	if resp, err := getURL(c, "https://elsewhere.example.org:4318/v1/traces"); err == nil {
		_ = resp.Body.Close()
		t.Error("the client reached a host other than the configured endpoint")
	}
}
