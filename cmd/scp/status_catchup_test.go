package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/webhook"
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
