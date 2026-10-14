package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/netguard"
	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/platform/telemetry"
)

// The telemetry package is rank 0 and imports neither secret nor config, so everything around it that touches a file, a
// credential or the network policy lives here (02 section 4.4): the headers file, the exporter's HTTP client, the OpenTelemetry
// error handler and the list of values that logs must never contain.

const (
	maxHeadersFile = 16 << 10
	maxHeaders     = 32
)

// parseHeadersFile reads telemetry.otlp.headers_file: `Name: value` per line, `#` comments, blank lines skipped (02 section 4.4,
// "Headers-file grammar"). Every problem is collected with its line number and none echoes a value. The returned secrets are the
// values, for logx scrubbing.
func parseHeadersFile(path string) (map[string]string, []secret.Secret, []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("telemetry.otlp.headers_file: %v", errors.Unwrap(err))}
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxHeadersFile+1)) // the cap is enforced while reading, not after
	if err != nil {
		return nil, nil, []string{"telemetry.otlp.headers_file: " + err.Error()}
	}
	if len(raw) > maxHeadersFile {
		return nil, nil, []string{fmt.Sprintf("telemetry.otlp.headers_file: larger than %d bytes", maxHeadersFile)}
	}
	return parseHeaders(string(raw))
}

// isToken reports whether s is an HTTP token (RFC 9110 5.6.2).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

func parseHeaders(text string) (map[string]string, []secret.Secret, []string) {
	var problems []string
	out := map[string]string{}
	var secrets []secret.Secret
	bad := func(n int, why string) {
		problems = append(problems, fmt.Sprintf("telemetry.otlp.headers_file:%d: %s", n, why))
	}
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		switch {
		case !ok:
			bad(n, "expected `Name: value`")
			continue
		case !isToken(name):
			bad(n, "the header name is not an HTTP token")
			continue
		case value == "":
			bad(n, "empty value")
			continue
		}
		for j := range len(value) {
			if c := value[j]; c < 0x20 && c != '\t' || c == 0x7f {
				bad(n, "the value contains a control character")
				break
			}
		}
		key := textproto.CanonicalMIMEHeaderKey(name)
		if _, dup := out[key]; dup {
			bad(n, fmt.Sprintf("duplicate header %s (two credentials for one name is a decision nobody made)", key))
			continue
		}
		if len(out) >= maxHeaders {
			bad(n, fmt.Sprintf("more than %d headers", maxHeaders))
			break
		}
		out[key] = value
		secrets = append(secrets, secret.New(value))
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return out, secrets, nil
}

// otlpClient is the exporter's HTTP client: a netguard client pinned to the configured endpoint host (and its port), with the
// operator's private CIDRs and extra ports as the only widening of the strict policy (02 section 4.4, D10). Nil when export is off.
func otlpClient(c telemetry.OTLPConfig) (*http.Client, error) {
	if !c.Enabled {
		return nil, nil
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("telemetry.otlp.endpoint: not a URL with a host")
	}
	pol, err := netguard.NewPolicy(append(c.AllowPrivateCIDRs, extraAllowedCIDRs()...), c.Ports)
	if err != nil {
		return nil, fmt.Errorf("telemetry.otlp: %w", err)
	}
	return netguard.NewClient(netguard.ClientOptions{
		Policy: pol, Hosts: []string{strings.ToLower(u.Host)}, AllowHTTP: u.Scheme == "http",
		Timeout:      -1, // the exporters apply telemetry.otlp.timeout per export through the request context
		MaxRedirects: 0, UserAgent: "scp/" + version,
	})
}

// routeOTelErrors sends the SDK's internal errors (a failed export, a dropped span) to the structured log at WARN instead of the
// default handler's stderr line, so they carry the service fields and reach the same sinks as everything else.
func routeOTelErrors(ctx context.Context, log *slog.Logger) {
	ctx = context.WithoutCancel(ctx) // the handler outlives the signal that cancels ctx: shutdown errors must still be logged
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.LogAttrs(ctx, slog.LevelWarn, "otel", slog.String("component", "otel"), logx.Err(err))
	}))
}

// secretsOf collects every secret value of the configuration, for logx.Options.Scrub: a credential that a caller formats by
// hand into a message is still masked (02 section 4.2).
func secretsOf(cfg any) []secret.Secret {
	var out []secret.Secret
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() { //nolint:exhaustive // only containers can hold a secret; every scalar kind is skipped on purpose
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			if v.Type() == reflect.TypeFor[secret.Secret]() {
				if s, ok := reflect.TypeAssert[secret.Secret](v); ok && !s.IsZero() {
					out = append(out, s)
				}
				return
			}
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		}
	}
	walk(reflect.ValueOf(cfg))
	return out
}
