package manifests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

func TestNewParserKindAndRegistration(t *testing.T) {
	match := func(p string) bool { return strings.HasSuffix(p, "x.lock") }
	p := WithKind(NewParser("npm/x-lock", 3, NPM, match, func(context.Context, File, Limits) (Parsed, error) { return Parsed{}, nil }), KindLockfile)
	if k, ok := p.(Kinded); !ok || k.Kind() != KindLockfile {
		t.Fatal("kind not applied")
	}
	r := NewRegistry()
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(p); err == nil {
		t.Fatal("duplicate parser id accepted")
	}
	r.Freeze()
	if len(r.Match("a/x.lock")) != 1 || len(r.Match("a/y.lock")) != 0 {
		t.Fatal("match")
	}
}

func TestFormatPURLDefaultAndHook(t *testing.T) {
	r := NewRegistry()
	got, ok := r.FormatPURL(ComponentID{Maven, "org.acme", "core"}, "", "1.0", "classifier=sources&type=jar")
	if !ok || got != "pkg:maven/org.acme/core@1.0?classifier=sources&type=jar" {
		t.Fatalf("%q %v", got, ok)
	}
	sp, _ := r.Spec(Terraform)
	sp.FormatPURL = func(ComponentID, string, string, string) (string, bool) { return "", false }
	if err := r.RegisterEcosystem(sp); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.FormatPURL(ComponentID{Terraform, "hashicorp", "aws"}, "", "", ""); ok {
		t.Fatal("formatter said no purl")
	}
	if got, _ := r.FormatPURL(ComponentID{Swift, "github.com/apple", "swift-nio"}, "", "2.0.0", "subpath=Sources/NIO"); !strings.HasSuffix(got, "#Sources/NIO") {
		t.Fatalf("subpath: %q", got)
	}
}

func TestLimitsSurface(t *testing.T) {
	if l := (limits.Limits{}).Resolved(true); l.MaxBytes != 8<<20 || l.Timeout == 0 {
		t.Fatalf("%+v", l)
	}
	if l := (limits.Limits{}).Resolved(false); l.MaxBytes != 1<<20 {
		t.Fatalf("%+v", l)
	}
	if out := limits.Text("ab‮cd\x00ef", 4); out != "abcd" {
		t.Fatalf("%q", out)
	}
	var ee *limits.EncodingError
	if _, err := limits.Input([]byte{0xFF, 0xFE, 'a', 0}); !errors.As(err, &ee) || ee.Reason != "utf16" {
		t.Fatal(err)
	}
	if out, err := limits.Input([]byte("\xEF\xBB\xBFok")); err != nil || string(out) != "ok" {
		t.Fatal(string(out), err)
	}
	g := limits.NewGuard(context.Background(), limits.Limits{MaxNodes: 2})
	_ = g.Tick()
	_ = g.Tick()
	if name, ok := limits.IsLimit(g.Tick()); !ok || name != limits.LimitNodes {
		t.Fatalf("%q %v", name, ok)
	}
}
