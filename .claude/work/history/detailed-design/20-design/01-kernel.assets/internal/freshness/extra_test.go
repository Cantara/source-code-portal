package freshness_test

import (
	"math"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
)

func TestInfoValidateRejects(t *testing.T) {
	bad := map[string]freshness.Info{
		"unknown state":              {State: "bogus"},
		"result state without fetch": {State: freshness.StateOK},
		"ok with error code":         {State: freshness.StateOK, FetchedAt: t0, CheckedAt: t0, ErrCode: "x"},
		"negative attempts":          {Attempts: -1},
		"checked before fetched":     {State: freshness.StateOK, FetchedAt: t0, CheckedAt: t0.Add(-time.Hour)},
	}
	for name, i := range bad {
		if i.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if (freshness.Info{}).Validate() != nil {
		t.Error("the zero Info is valid (never fetched)")
	}
}

func TestValueStatus(t *testing.T) {
	v := freshness.Value[string]{V: "x", Info: freshness.Info{TTL: time.Hour}.AfterSuccess(freshness.StateOK, t0, "")}
	if v.Status(t0.Add(time.Minute)) != freshness.StatusFresh || v.Status(t0.Add(3*time.Hour)) != freshness.StatusStale {
		t.Error("Value.Status delegates to Derive")
	}
	if (freshness.Value[int]{}).Status(t0) != freshness.StatusUnknown {
		t.Error("the zero Value is unknown")
	}
}

func TestResultStatesSurviveRoundTripThroughTransitions(t *testing.T) {
	for _, s := range freshness.States() {
		if !s.HasResult() {
			continue
		}
		i := freshness.Info{TTL: time.Hour}.AfterSuccess(s, t0, "sha")
		if i.Validate() != nil || i.Derive(t0) != freshness.StatusFresh || i.State != s {
			t.Errorf("%s: %+v", s, i)
		}
	}
}

// V-03: the "equal SHA / 304" confirmation must not turn a definitive non-ok answer into "ok".
func TestRevalidationKeepsTheResultState(t *testing.T) {
	later := t0.Add(time.Hour)
	for _, st := range []freshness.State{freshness.StateOK, freshness.StateNotFound, freshness.StateUnsupported, freshness.StateTooLarge, freshness.StateNoContent} {
		held := freshness.Info{TTL: time.Hour}.AfterSuccess(st, t0, "sha")
		got := held.AfterRevalidated(later)
		if got.State != st || !got.FetchedAt.Equal(later) || got.ContentSHA != "sha" || got.Validate() != nil {
			t.Errorf("%s: revalidation produced %+v", st, got)
		}
	}
	// after a failure the original result state is no longer in Info: the default is ok, the caller can say better
	failed := freshness.Info{TTL: time.Hour}.AfterSuccess(freshness.StateNoContent, t0, "sha").AfterFailure(t0.Add(time.Minute), "upstream")
	if got := failed.AfterRevalidated(later); got.State != freshness.StateOK || got.Attempts != 0 || got.ErrCode != "" || !got.FetchedAt.Equal(later) {
		t.Errorf("after failure: %+v", got)
	}
	if got := failed.AfterRevalidatedAs(freshness.StateNoContent, later); got.State != freshness.StateNoContent || got.Attempts != 0 || got.ErrCode != "" {
		t.Errorf("AfterRevalidatedAs: %+v", got)
	}
	if got := (freshness.Info{}).AfterRevalidatedAs(freshness.StateOK, later); got.HasValue() {
		t.Error("a confirmation cannot create a value")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("AfterRevalidatedAs with a non-result state must panic")
			}
		}()
		_ = failed.AfterRevalidatedAs(freshness.StateError, later)
	}()
}

// V-06: NaN is not a valid jitter, and a NaN or huge input must never produce a 1 s hot loop or a platform-dependent value.
func TestIntervalNeverMisbehavesOnNaNOrOverflow(t *testing.T) {
	if (freshness.Policy{TTL: time.Hour, Jitter: math.NaN()}).Validate() == nil {
		t.Error("NaN jitter accepted")
	}
	if (freshness.Policy{TTL: time.Hour, Jitter: math.Inf(1)}).Validate() == nil {
		t.Error("infinite jitter accepted")
	}
	p := freshness.Policy{TTL: time.Hour, Jitter: 0.1}
	if got := p.Interval(0, math.NaN()); got < 54*time.Minute || got > 66*time.Minute {
		t.Errorf("NaN u: %v", got)
	}
	if got := (freshness.Policy{TTL: time.Hour, Jitter: math.NaN()}).Interval(0, 0.5); got != time.Hour {
		t.Errorf("NaN jitter on an unvalidated policy: %v", got)
	}
	huge := freshness.Policy{TTL: time.Duration(math.MaxInt64 - 5), Jitter: 0.5, IdleBackoffMax: time.Duration(math.MaxInt64)}
	for _, u := range []float64{0, 0.5, 0.999999} {
		for _, streak := range []int{0, 1, 70} {
			if got := huge.Interval(streak, u); got < time.Hour {
				t.Errorf("overflow: streak %d u %v gave %v", streak, u, got)
			}
		}
	}
}
