package limits

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestExpandBomb(t *testing.T) {
	props := map[string]string{"p0": strings.Repeat("x", 10)}
	for i := 1; i <= 24; i++ {
		prev := "p" + string(rune('a'+i-1))
		_ = prev
	}
	// p_i = ${p_{i-1}}${p_{i-1}}  (research E5: 24 levels, 10 bytes -> 167,772,160 bytes)
	name := func(i int) string { return "p" + strings.Repeat("_", i) }
	props[name(0)] = strings.Repeat("x", 10)
	for i := 1; i <= 24; i++ {
		props[name(i)] = "${" + name(i-1) + "}${" + name(i-1) + "}"
	}
	refs := func(v string) []string {
		var out []string
		for {
			i := strings.Index(v, "${")
			if i < 0 {
				return out
			}
			j := strings.Index(v[i:], "}")
			out = append(out, v[i+2:i+j])
			v = v[i+j+1:]
		}
	}
	over := PreCheck(props, refs, DefaultMaxExpand)
	if len(over) == 0 {
		t.Fatal("PreCheck must flag the bomb")
	}
	g := NewGuard(context.Background(), Limits{})
	_, err := Expand("${"+name(24)+"}", func(n string) (string, bool) { v, ok := props[n]; return v, ok }, "${", "}", g)
	var le *LimitError
	if !errors.As(err, &le) || le.Limit != LimitExpand {
		t.Fatalf("want limit.expand, got %v", err)
	}
}

func TestExpandCycle(t *testing.T) {
	props := map[string]string{"a": "${b}", "b": "${a}", "s": "${s}"}
	g := NewGuard(context.Background(), Limits{})
	v, err := Expand("v=${a}${s}", func(n string) (string, bool) { v, ok := props[n]; return v, ok }, "${", "}", g)
	if err != nil || v != "v=" {
		t.Fatalf("got %q %v", v, err)
	}
}

func TestTextPolicy(t *testing.T) {
	if b, err := Input(append([]byte{0xEF, 0xBB, 0xBF}, "ok"...)); err != nil || string(b) != "ok" {
		t.Fatal("BOM must be stripped")
	}
	for _, in := range [][]byte{{0xFF, 0xFE, 'a', 0}, []byte("a\x00b"), {0xC3, 0x28}} {
		if _, err := Input(in); err == nil {
			t.Fatalf("%x must be rejected", in)
		}
	}
}

func TestCharsetXML(t *testing.T) {
	doc := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a>caf\xe9</a>"
	d := xml.NewDecoder(strings.NewReader(doc))
	d.CharsetReader = CharsetReader
	var v struct {
		S string `xml:",chardata"`
	}
	if err := d.Decode(&v); err != nil || v.S != "café" {
		t.Fatalf("got %q %v", v.S, err)
	}
	d = xml.NewDecoder(strings.NewReader("<?xml version=\"1.0\" encoding=\"koi8-r\"?><a/>"))
	d.CharsetReader = CharsetReader
	var ee *EncodingError
	if err := d.Decode(&v); !errors.As(err, &ee) {
		t.Fatalf("koi8-r must be rejected: %v", err)
	}
}

func TestGuardDepthNodes(t *testing.T) {
	g := NewGuard(context.Background(), Limits{MaxDepth: 3, MaxNodes: 5})
	for i := 0; i < 3; i++ {
		if err := g.Enter(); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Enter(); err == nil {
		t.Fatal("depth")
	}
	g = NewGuard(context.Background(), Limits{MaxNodes: 5})
	var err error
	for i := 0; i < 6 && err == nil; i++ {
		err = g.Node()
	}
	if err == nil {
		t.Fatal("nodes")
	}
}

func TestWatchdogAbandonAndCircuit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWatchdog(NewBudget(100))
		var abandoned atomic.Int32
		w.OnAbandon = func(string) { abandoned.Add(1) }
		release := make(chan struct{})
		stuck := func(ctx context.Context) (int, error) { <-release; return 1, nil } // ignores ctx
		for i := 0; i < 3; i++ {
			if _, err := Run(context.Background(), w, "bad/parser", time.Second, 10, stuck); !errors.Is(err, ErrAbandoned) {
				t.Fatalf("attempt %d: want ErrAbandoned, got %v", i, err)
			}
		}
		if w.Budget.InUse() != 30 {
			t.Fatalf("abandoned goroutines keep their weight, got %d", w.Budget.InUse())
		}
		if _, err := Run(context.Background(), w, "bad/parser", time.Second, 10, stuck); !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("circuit must be open after 3 abandonments, got %v", err)
		}
		if abandoned.Load() != 3 {
			t.Fatalf("hook calls: %d", abandoned.Load())
		}
		close(release)
		if err := w.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		if w.Budget.InUse() != 0 {
			t.Fatalf("weight returned after exit, got %d", w.Budget.InUse())
		}
		// weight above the capacity is clamped, not blocked forever
		if v, err := Run(context.Background(), w, "ok/parser", time.Second, 1<<40, func(context.Context) (int, error) { return 7, nil }); err != nil || v != 7 {
			t.Fatalf("clamp: %v %v", v, err)
		}
	})
}

func TestWatchdogCooperative(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := NewWatchdog(NewBudget(1 << 20))
		v, err := Run(context.Background(), w, "p", time.Second, 10, func(ctx context.Context) (string, error) {
			<-ctx.Done() // cooperative parser stops at the deadline
			return "", &LimitError{LimitTimeout, 1000}
		})
		var le *LimitError
		if v != "" || !errors.As(err, &le) {
			t.Fatalf("got %v %v", v, err)
		}
		if err := w.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}
