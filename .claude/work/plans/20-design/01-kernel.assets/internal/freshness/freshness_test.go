package freshness_test

import (
	"testing"
	"testing/quick"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func TestDerive(t *testing.T) {
	const ttl = time.Hour
	good := freshness.Info{State: freshness.StateOK, FetchedAt: t0, CheckedAt: t0, TTL: ttl}
	cases := []struct {
		name string
		i    freshness.Info
		now  time.Time
		want freshness.Status
	}{
		{"zero value is unknown", freshness.Info{}, t0, freshness.StatusUnknown},
		{"never", freshness.Info{State: freshness.StateNever, TTL: ttl}, t0, freshness.StatusUnknown},
		{"fresh at fetch", good, t0, freshness.StatusFresh},
		{"fresh at exactly 1.5 ttl", good, t0.Add(90 * time.Minute), freshness.StatusFresh},
		{"stale just past 1.5 ttl", good, t0.Add(90*time.Minute + time.Nanosecond), freshness.StatusStale},
		{"clock skew (fetched in the future) is fresh", good, t0.Add(-time.Hour), freshness.StatusFresh},
		{"error with a held value is stale", good.AfterFailure(t0.Add(time.Minute), "rate_limited"), t0.Add(time.Minute), freshness.StatusStale},
		{"error without a value is error", freshness.Info{TTL: ttl}.AfterFailure(t0, "not_reachable"), t0, freshness.StatusError},
		{"not_found is a definitive answer and ages like any", freshness.Info{TTL: ttl}.AfterSuccess(freshness.StateNotFound, t0, ""), t0, freshness.StatusFresh},
		{"not_found goes stale", freshness.Info{TTL: ttl}.AfterSuccess(freshness.StateNotFound, t0, ""), t0.Add(2 * time.Hour), freshness.StatusStale},
		{"no ttl is never fresh", freshness.Info{State: freshness.StateOK, FetchedAt: t0}, t0, freshness.StatusStale},
	}
	for _, c := range cases {
		if got := c.i.Derive(c.now); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestStatusStrings(t *testing.T) {
	for s, want := range map[freshness.Status]string{freshness.StatusUnknown: "unknown", freshness.StatusFresh: "fresh", freshness.StatusStale: "stale", freshness.StatusError: "error", freshness.Status(99): "unknown"} {
		if s.String() != want {
			t.Errorf("%d: %q", s, s.String())
		}
	}
}

func TestTransitionsKeepStaleWhileError(t *testing.T) {
	i := freshness.Info{TTL: time.Hour}.AfterSuccess(freshness.StateOK, t0, "sha1")
	f1 := i.AfterFailure(t0.Add(time.Minute), "upstream_5xx")
	f2 := f1.AfterFailure(t0.Add(2*time.Minute), "upstream_5xx")
	if f2.FetchedAt != t0 || f2.ContentSHA != "sha1" || f2.Attempts != 2 || f2.State != freshness.StateError || f2.ErrCode != "upstream_5xx" {
		t.Fatalf("%+v", f2)
	}
	r := f2.AfterRevalidated(t0.Add(3 * time.Minute))
	if r.State != freshness.StateOK || r.Attempts != 0 || r.ErrCode != "" || r.FetchedAt != t0.Add(3*time.Minute) || r.ContentSHA != "sha1" {
		t.Fatalf("%+v", r)
	}
	if got := (freshness.Info{}).AfterRevalidated(t0); got.HasValue() {
		t.Fatal("a 304 cannot create a value")
	}
	for _, x := range []freshness.Info{i, f1, f2, r} {
		if err := x.Validate(); err != nil {
			t.Errorf("%+v: %v", x, err)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("AfterSuccess must refuse a non-result state")
		}
	}()
	i.AfterSuccess(freshness.StateError, t0, "")
}

func TestStateSetMatchesTheSQLCheckConstraint(t *testing.T) {
	want := []string{"ok", "not_found", "unsupported", "too_large", "no_content", "error", "never"}
	got := freshness.States()
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if string(got[i]) != want[i] || !got[i].Valid() {
			t.Fatal(got)
		}
	}
	if freshness.State("bogus").Valid() || !freshness.State("").Valid() {
		t.Fatal("Valid")
	}
}

func TestPolicyValidate(t *testing.T) {
	ok := freshness.Policy{TTL: time.Hour, Jitter: 0.1, IdleBackoffMax: 6 * time.Hour}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []freshness.Policy{{}, {TTL: time.Hour, Jitter: 0.6}, {TTL: time.Hour, Jitter: -0.1}, {TTL: time.Hour, IdleBackoffMax: time.Minute}} {
		if p.Validate() == nil {
			t.Errorf("%+v accepted", p)
		}
	}
}

func TestIntervalBackoffAndJitter(t *testing.T) {
	p := freshness.Policy{TTL: time.Hour, Jitter: 0.1, IdleBackoffMax: 6 * time.Hour}
	mid := func(streak int) time.Duration { return p.Interval(streak, 0.5) } // u=0.5: no jitter
	want := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 6 * time.Hour, 6 * time.Hour}
	for s, w := range want {
		if got := mid(s); got != w {
			t.Errorf("streak %d: %v want %v", s, got, w)
		}
	}
	if got := p.Interval(0, 0); got != 54*time.Minute {
		t.Errorf("lower jitter bound %v", got)
	}
	if got := p.Interval(0, 0.999999); got < 65*time.Minute+50*time.Second || got > 66*time.Minute {
		t.Errorf("upper jitter bound %v", got)
	}
	noBackoff := freshness.Policy{TTL: time.Hour}
	if noBackoff.Interval(50, 0.5) != time.Hour {
		t.Error("IdleBackoffMax=0 disables back-off")
	}
	if err := quick.Check(func(streak uint16, u float64) bool {
		d := p.Interval(int(streak), u)
		return d >= 54*time.Minute && d <= 6*time.Hour+6*time.Hour/10+time.Nanosecond
	}, nil); err != nil {
		t.Error(err)
	}
	if (freshness.Policy{}).Interval(0, 0.5) != freshness.MinInterval {
		t.Error("the floor applies to an unset TTL")
	}
	if (freshness.Policy{TTL: time.Millisecond}).Interval(0, 0.5) != freshness.MinInterval {
		t.Error("the floor applies to a tiny TTL")
	}
}

func TestStaleSince(t *testing.T) {
	i := freshness.Info{TTL: time.Hour}.AfterSuccess(freshness.StateOK, t0, "")
	if at, ok := i.StaleSince(); !ok || !at.Equal(t0.Add(90*time.Minute)) {
		t.Fatal(at)
	}
	if _, ok := (freshness.Info{TTL: time.Hour}).StaleSince(); ok {
		t.Fatal("no value")
	}
}
