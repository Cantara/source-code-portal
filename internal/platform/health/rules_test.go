package health

import (
	"slices"
	"strings"
	"testing"
)

// TestDescribeTellsWhatAssessDoes holds the rules told to what Assess, LevelOf, SeverityOf and ActivityOf apply: every finding Assess
// can make is described with its penalties, and every level, severity and activity starts where its function says.
func TestDescribeTellsWhatAssessDoes(t *testing.T) {
	r := Describe()
	described := map[string][]int{}
	for _, k := range r.Findings {
		if k.Label == "" || k.About == "" || len(k.Penalties) == 0 || k.Action.Label == "" {
			t.Errorf("%s: undescribed %+v", k.Code, k)
		}
		for _, p := range k.Penalties {
			if p.When == "" {
				t.Errorf("%s: a penalty without when", k.Code)
			}
			described[k.Code] = append(described[k.Code], p.Points)
		}
	}
	for code, points := range penalties {
		if !slices.Equal(described[code], points) {
			t.Errorf("%s: described %v, applied %v", code, described[code], points)
		}
	}
	// Every finding Assess makes, from signals that trip them all, is a described kind at a described penalty.
	worst := Signals{Public: true, LastChange: ago(800), LastCommit: ago(1), Release: "v1", Released: ago(400), OpenIssues: 99, Failing: true}
	quiet := Signals{Public: true, License: "MIT", LastChange: ago(200), LastCommit: ago(200), OpenIssues: 30}
	for _, s := range []Signals{worst, quiet} {
		for _, f := range Assess(s, now).Findings {
			if !slices.Contains(described[f.Code], f.Penalty) {
				t.Errorf("Assess made %s at %d, not described", f.Code, f.Penalty)
			}
		}
	}
	for _, l := range r.Levels {
		if LevelOf(l.From) != l.Level || (l.From > 0 && LevelOf(l.From-1) == l.Level) {
			t.Errorf("level %s does not start at %d", l.Level, l.From)
		}
	}
	for _, v := range r.Severities {
		if SeverityOf(v.From) != v.Severity || (v.From > 0 && SeverityOf(v.From-1) == v.Severity) {
			t.Errorf("severity %s does not start at %d", v.Severity, v.From)
		}
	}
	for _, a := range r.Activity {
		if a.WithinDays == 0 {
			continue
		}
		if ActivityOf(ago(a.WithinDays), false, now) != a.Activity || ActivityOf(ago(a.WithinDays+1), false, now) == a.Activity {
			t.Errorf("activity %s does not end at %d days", a.Activity, a.WithinDays)
		}
	}
	if ActivityOf(ago(1), true, now) != Archived || ActivityOf(now.AddDate(-100, 0, 0), false, now) != Dormant {
		t.Error("archived or dormant")
	}
	if !strings.Contains(r.Activity[0].About, "30 days") || r.WindowDays != 30 {
		t.Errorf("the window: %d, %q", r.WindowDays, r.Activity[0].About)
	}
}
