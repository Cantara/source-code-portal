package cluster_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/cluster"
)

// hazelcast returns the address of the Hazelcast server for tests (CI runs one on loopback), or skips.
func hazelcast(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("TEST_HAZELCAST")
	if addr == "" {
		t.Skip("TEST_HAZELCAST is not set")
	}
	return addr
}

func TestHazelcastBackend(t *testing.T) {
	addr := hazelcast(t)
	ctx := t.Context()
	ns := fmt.Sprintf("scptest-%d", time.Now().UnixNano())
	dial := cluster.DialHazelcast([]string{addr}, "dev", ns, slog.New(slog.DiscardHandler))
	a, err := dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close(context.Background()) }()

	if ok, err := a.TryLead(ctx); !ok || err != nil {
		t.Fatalf("a TryLead: %v %v", ok, err)
	}
	if ok, err := a.TryLead(ctx); !ok || err != nil {
		t.Errorf("a TryLead again: %v %v", ok, err)
	}
	if ok, err := b.TryLead(ctx); ok || err != nil {
		t.Errorf("b TryLead while a holds the lock: %v %v", ok, err)
	}

	var mu gosync.Mutex
	var watched, messages []string
	if err := b.Watch(ctx, func(k string, v []byte) { mu.Lock(); watched = append(watched, k+"="+string(v)); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	if err := b.Subscribe(ctx, func(m []byte) { mu.Lock(); messages = append(messages, string(m)); mu.Unlock() }); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"k1", "v1"}, {"k2", "v2"}, {"k1", "v3"}} {
		if err := a.Put(ctx, kv[0], []byte(kv[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Delete(ctx, "k2"); err != nil {
		t.Fatal(err)
	}
	if err := a.Publish(ctx, []byte(`{"event":1}`)); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries(ctx)
	if err != nil || len(entries) != 1 || string(entries["k1"]) != "v3" {
		t.Errorf("entries = %v, %v", entries, err)
	}
	if keys, err := b.Keys(ctx); err != nil || strings.Join(keys, ",") != "k1" {
		t.Errorf("keys = %v, %v", keys, err)
	}
	if first, err := a.Once(ctx, "d1", time.Minute); !first || err != nil {
		t.Errorf("Once first: %v %v", first, err)
	}
	if first, err := b.Once(ctx, "d1", time.Minute); first || err != nil {
		t.Errorf("Once again: %v %v", first, err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(watched, ",") == "k1=v1,k2=v2,k1=v3" && len(messages) == 1
	})

	start := time.Now()
	if err := a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { ok, _ := b.TryLead(ctx); return ok })
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the lock passed %s after the holder closed, want at once", d)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 10s")
		}
	}
}
