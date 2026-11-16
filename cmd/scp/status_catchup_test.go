package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/feature/webhook"
)

// A delivery log that is not being read is a warning on the status, not only a line in the log; one that is read is none.
func TestStatusWarnsOfALogNotRead(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		state webhook.CatchUpState
		warn  string // a part of the one warning expected; empty: none
	}{
		{"read a minute ago", webhook.CatchUpState{Source: "a", LastRead: now.Add(-time.Minute)}, ""},
		{"not read yet, no error", webhook.CatchUpState{Source: "a"}, ""},
		{"read an hour ago", webhook.CatchUpState{Source: "a", LastRead: now.Add(-time.Hour), Error: "502"}, "last read 1h0m0s ago (502)"},
		{"never read, failing", webhook.CatchUpState{Source: "a", Error: "401 bad credentials"}, "not being read (401 bad credentials)"},
		{"cut today", webhook.CatchUpState{Source: "a", LastRead: now, Truncated: now.Add(-time.Hour)}, "longer than a read"},
		{"cut last week", webhook.CatchUpState{Source: "a", LastRead: now, Truncated: now.Add(-7 * 24 * time.Hour)}, ""},
	}
	for _, tc := range tests {
		got, warns := catchUpStatus([]webhook.CatchUpState{tc.state}, now)
		if len(got) != 1 || got[0].Source != "a" {
			t.Errorf("%s: %+v", tc.name, got)
		}
		switch {
		case tc.warn == "" && len(warns) != 0, tc.warn != "" && (len(warns) != 1 || !strings.Contains(warns[0], tc.warn)):
			t.Errorf("%s: warnings %q, want %q", tc.name, warns, tc.warn)
		}
	}
}

// The receiver's answer times are counted against GitHub's 10 seconds, and the failed deliveries a log showed are listed by the status
// they got.
func TestStatusCountsAnswersAndFailures(t *testing.T) {
	st := &status{}
	for _, d := range []time.Duration{time.Millisecond, 6 * time.Second, 12 * time.Second} {
		st.answered(d)
	}
	if want := (statusAnswers{Count: 3, Slowest: 12, Slow: 2, Late: 1}); st.answers != want {
		t.Errorf("answers %+v, want %+v", st.answers, want)
	}
	got, _ := catchUpStatus([]webhook.CatchUpState{{Source: "a", Failed: map[int]int{0: 2, 401: 1}}}, time.Now())
	if f := got[0].Failed; len(f) != 2 || f["0"] != 2 || f["401"] != 1 {
		t.Errorf("failed %v", f)
	}
	if got, _ := catchUpStatus([]webhook.CatchUpState{{Source: "a"}}, time.Now()); got[0].Failed == nil {
		t.Error("no failures: null, want {}")
	}
}
