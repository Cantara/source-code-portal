package logx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/cantara/source-code-portal/internal/platform/logx"
	"github.com/cantara/source-code-portal/internal/platform/secret"
)

func TestHandlerFields(t *testing.T) {
	var buf bytes.Buffer
	tok := secret.New("ghs_supersecrettoken123")
	l := slog.New(logx.NewHandler(&buf, logx.Options{Level: slog.LevelInfo, Service: "scp", Version: "1.2.3", Build: "abc", Scrub: []secret.Secret{tok, secret.New("short")}}))
	tid, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sid, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	ctx = logx.WithRequestID(ctx, "req-1")
	ctx = logx.With(ctx, slog.String("repo", "a"))
	ctx = logx.With(ctx, slog.String("repo", "b"))
	l.InfoContext(ctx, "hello "+tok.Reveal(), "err", errors.New("bad Authorization: token "+tok.Reveal()), "word", "short", "tok", tok)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err, buf.String())
	}
	for k, want := range map[string]string{"level": "info", "service": "scp", "version": "1.2.3", "build": "abc", "request_id": "req-1", "repo": "b", "trace_id": "0102030405060708090a0b0c0d0e0f10", "span_id": "0102030405060708", "word": "short"} {
		if m[k] != want {
			t.Errorf("%s=%v want %s", k, m[k], want)
		}
	}
	if _, ok := m["ts"]; !ok || strings.Contains(buf.String(), "supersecret") {
		t.Fatalf("ts or leak: %s", buf.String())
	}
	if logx.RequestID(ctx) != "req-1" {
		t.Fatal("RequestID")
	}
}

func TestRedactURL(t *testing.T) {
	if got := logx.RedactURL("https://u:p@h.example/a/b?client_secret=x#f"); got != "https://h.example/a/b" {
		t.Fatal(got)
	}
}

func FuzzRedactURL(f *testing.F) {
	for _, s := range []string{"https://u:p@h/p?client_secret=abc&x=1#frag", "://", "http://[::1", "x?access_token=%zz", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := logx.RedactURL(s)
		if strings.Contains(s, "client_secret=SECRETVALUE") && strings.Contains(out, "SECRETVALUE") {
			t.Fatalf("secret survived: %q -> %q", s, out)
		}
	})
}
