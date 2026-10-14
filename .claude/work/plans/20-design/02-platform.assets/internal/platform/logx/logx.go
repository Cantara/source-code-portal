// Package logx builds the process logger and carries request-scoped attributes in a context (rank 1:
// imports platform/secret and the OpenTelemetry trace API only).
package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

// Field names (stable: dashboards and tests key on them).
const (
	KeyTimestamp  = "ts"
	KeyRequestID  = "request_id"
	KeyTraceID    = "trace_id"
	KeySpanID     = "span_id"
	KeyDelivery   = "delivery_id"
	KeyJobKey     = "job_key"
	KeyJobKind    = "job_kind"
	KeyAttempt    = "attempt"
	KeySource     = "source"
	KeyInstall    = "installation_id"
	KeyRepo       = "repo"
	KeyUpstream   = "upstream"
	KeyStatus     = "status"
	KeyDurationMS = "duration_ms"
	KeyErrCode    = "err_code"
	KeySnapshot   = "snapshot_version"
	KeyClientIP   = "client_ip"
	KeyErr        = "err"
)

// Config is the `log:` section of the config file.
type Config struct {
	Level     string `yaml:"level" jsonschema:"enum=debug,enum=info,enum=warn,enum=error" jsonschema_description:"Minimum level that is written."`
	Format    string `yaml:"format" jsonschema:"enum=json,enum=text" jsonschema_description:"json for production, text for a developer terminal."`
	AddSource bool   `yaml:"add_source" jsonschema_description:"Add the source file and line to every record."`
}

// ParseLevel maps the config string to a slog level.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log level %q: want debug, info, warn or error", s)
	}
	return l, nil
}

// Options configures NewHandler. Everything is injected; the package reads no environment.
type Options struct {
	Level     slog.Leveler
	Format    string // "json" (default) or "text"
	AddSource bool
	Service   string
	Version   string
	Build     string
	// Scrub lists credentials whose value must never appear in a record, even if a caller formats it by hand.
	// Values shorter than MinScrubLen are ignored (they would mangle ordinary words).
	Scrub []secret.Secret
}

// MinScrubLen is the shortest secret that is value-scrubbed.
const MinScrubLen = 8

// NewHandler returns the process handler: JSON (or text) with the standard fields, context attributes,
// trace and span ids from the context, and value scrubbing.
func NewHandler(w io.Writer, o Options) slog.Handler {
	ho := &slog.HandlerOptions{Level: o.Level, AddSource: o.AddSource, ReplaceAttr: replace}
	var inner slog.Handler
	if o.Format == "text" {
		inner = slog.NewTextHandler(w, ho)
	} else {
		inner = slog.NewJSONHandler(w, ho)
	}
	var static []slog.Attr
	if o.Service != "" {
		static = append(static, slog.String("service", o.Service))
	}
	if o.Version != "" {
		static = append(static, slog.String("version", o.Version))
	}
	if o.Build != "" {
		static = append(static, slog.String("build", o.Build))
	}
	if len(static) > 0 {
		inner = inner.WithAttrs(static)
	}
	h := &handler{inner: inner}
	for _, s := range o.Scrub {
		if v := s.Reveal(); len(v) >= MinScrubLen {
			h.scrub = append(h.scrub, v)
		}
	}
	return h
}

func replace(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		return slog.String(KeyTimestamp, a.Value.Time().UTC().Format(time.RFC3339Nano))
	case slog.LevelKey:
		if l, ok := a.Value.Any().(slog.Level); ok {
			return slog.String(slog.LevelKey, strings.ToLower(l.String()))
		}
	}
	return a
}

type handler struct {
	inner slog.Handler
	scrub []string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, h.clean(r.Message), r.PC)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		nr.AddAttrs(slog.String(KeyTraceID, sc.TraceID().String()), slog.String(KeySpanID, sc.SpanID().String()))
	}
	nr.AddAttrs(Attrs(ctx)...)
	r.Attrs(func(a slog.Attr) bool { nr.AddAttrs(h.cleanAttr(a)); return true })
	return h.inner.Handle(ctx, nr)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	cs := make([]slog.Attr, len(as))
	for i, a := range as {
		cs[i] = h.cleanAttr(a)
	}
	return &handler{inner: h.inner.WithAttrs(cs), scrub: h.scrub}
}

// WithGroup is supported but SCP code does not use groups; context attributes are top-level only while no group is open.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), scrub: h.scrub}
}

func (h *handler) clean(s string) string {
	for _, sec := range h.scrub {
		if strings.Contains(s, sec) {
			s = strings.ReplaceAll(s, sec, secret.Redacted)
		}
	}
	return s
}

func (h *handler) cleanAttr(a slog.Attr) slog.Attr {
	if len(h.scrub) == 0 {
		return a
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.clean(v.String()))
	case slog.KindGroup:
		g := v.Group()
		out := make([]slog.Attr, len(g))
		for i := range g {
			out[i] = h.cleanAttr(g[i])
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		if e, ok := v.Any().(error); ok {
			return slog.String(a.Key, h.clean(e.Error()))
		}
	}
	return slog.Attr{Key: a.Key, Value: v}
}

type ctxKey struct{}

// With returns a context whose later log records carry attrs. A later attribute with the same key replaces the earlier one.
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	old, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	out := make([]slog.Attr, 0, len(old)+len(attrs))
	for _, o := range old {
		replaced := false
		for _, n := range attrs {
			if n.Key == o.Key {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, o)
		}
	}
	return context.WithValue(ctx, ctxKey{}, append(out, attrs...))
}

// Attrs returns the context attributes (read-only).
func Attrs(ctx context.Context) []slog.Attr {
	a, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return a
}

func lookup(ctx context.Context, key string) string {
	for _, a := range Attrs(ctx) {
		if a.Key == key {
			return a.Value.String()
		}
	}
	return ""
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return With(ctx, slog.String(KeyRequestID, id))
}
func RequestID(ctx context.Context) string { return lookup(ctx, KeyRequestID) }
func WithClientIP(ctx context.Context, ip string) context.Context {
	return With(ctx, slog.String(KeyClientIP, ip))
}
func ClientIP(ctx context.Context) string { return lookup(ctx, KeyClientIP) }
func WithDelivery(ctx context.Context, id string) context.Context {
	return With(ctx, slog.String(KeyDelivery, id))
}
func WithJob(ctx context.Context, key, kind string, attempt int) context.Context {
	return With(ctx, slog.String(KeyJobKey, key), slog.String(KeyJobKind, kind), slog.Int(KeyAttempt, attempt))
}

// Err is the one way to log an error value.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.String(KeyErr, "")
	}
	return slog.String(KeyErr, err.Error())
}

// RedactURL keeps scheme, host and path and drops userinfo, query and fragment (GR-13). Unparseable input yields "[invalid-url]".
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[invalid-url]"
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}
