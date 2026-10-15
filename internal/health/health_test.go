package health

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // the tests' clock

func ago(days int) time.Time { return now.Add(-time.Duration(days) * day) }

func codes(r Repo) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func TestAssess(t *testing.T) {
	healthy := Signals{Public: true, LastChange: ago(3), LastCommit: ago(3), Release: "v1.2.0", Released: ago(10), License: "Apache-2.0"}
	for _, c := range []struct {
		name  string
		edit  func(*Signals)
		score int
		level Level
		codes []string
	}{
		{"healthy", func(*Signals) {}, 100, Good, nil},
		{"quiet", func(s *Signals) { s.LastChange, s.LastCommit = ago(200), ago(200) }, 90, Good, []string{"quiet"}},
		{"only automation for long", func(s *Signals) { s.LastChange, s.Automated = ago(800), true }, 70, Watch, []string{"dormant"}},
		{"dormant and unlicensed", func(s *Signals) { s.LastChange, s.License = ago(800), "" }, 50, Watch, []string{"dormant", "no-licence"}},
		{"private needs no licence", func(s *Signals) { s.Public, s.License = false, "" }, 100, Good, nil},
		{"unreleased", func(s *Signals) { s.Released = ago(120) }, 80, Good, []string{"unreleased"}},
		{"released after the last commit", func(s *Signals) { s.LastCommit, s.Released = ago(130), ago(120) }, 100, Good, nil},
		{"unreleased needs the commits", func(s *Signals) { s.Released, s.LastCommit = ago(120), time.Time{} }, 100, Good, nil},
		{"no release", func(s *Signals) { s.Release = "" }, 100, Good, nil},
		{"issues", func(s *Signals) { s.OpenIssues = 21 }, 90, Good, []string{"issues"}},
		{"many issues", func(s *Signals) { s.OpenIssues = 51 }, 80, Good, []string{"issues"}},
		{"everything", func(s *Signals) {
			s.Failing, s.LastChange, s.Released, s.License, s.OpenIssues = true, ago(900), ago(1000), "", 99
		},
			0, Attention, []string{"failing", "dormant", "unreleased", "no-licence", "issues"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := healthy
			c.edit(&s)
			r := Assess(s, now)
			if !r.Counted || r.Score != c.score || r.Level != c.level {
				t.Fatalf("got %+v, want score %d %s", r, c.score, c.level)
			}
			if c.codes != nil && !equal(codes(r), c.codes) {
				t.Fatalf("codes %v, want %v", codes(r), c.codes)
			}
		})
	}
	if r := Assess(Signals{Archived: true, LastChange: ago(5000)}, now); r.Counted || r.Findings != nil {
		t.Fatalf("archived is counted: %+v", r)
	}
	if r := Assess(Signals{Deprecated: true, LastChange: ago(5000)}, now); r.Counted {
		t.Fatalf("deprecated is counted: %+v", r)
	}
	f := Assess(Signals{LastChange: ago(800), LastCommit: ago(800), Release: "v2", Released: ago(900)}, now).Findings
	if f[0].Message != "No change for 2 years: archive it or revive it" || f[1].Message != "Unreleased changes since v2, released 2 years ago" {
		t.Fatalf("messages %+v", f)
	}
	f = Assess(Signals{LastChange: ago(400), Automated: true, Before: true, LastCommit: ago(2)}, now).Findings
	if f[0].Message != "No change by a person for more than 13 months" {
		t.Fatalf("automated message %+v", f)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSpan(t *testing.T) {
	for days, want := range map[int]string{0: "0 days", 1: "1 day", 59: "59 days", 60: "2 months", 400: "13 months", 730: "2 years", 1100: "3 years"} {
		if got := span(time.Duration(days) * day); got != want {
			t.Errorf("span(%d) = %q, want %q", days, got, want)
		}
	}
}

func TestWeigh(t *testing.T) {
	repo := func(score int, recent bool, codes ...string) Repo {
		r := Repo{Counted: true, Score: score, Level: LevelOf(score), Recent: recent}
		for _, c := range codes {
			r.Findings = append(r.Findings, Finding{Code: c, Penalty: 100 - score})
		}
		return r
	}
	if p := Weigh(nil); p.Counted != 0 || p.Level != "" {
		t.Fatalf("empty: %+v", p)
	}
	if p := Weigh([]Member{{Address: "a/x"}}); p.Counted != 0 || p.Level != "" {
		t.Fatalf("archived only: %+v", p)
	}
	// weights: a/core 2x3 = 6, a/lib 2, a/old 1: (6*100 + 2*70 + 1*40) / 9 = 86.7
	p := Weigh([]Member{
		{Address: "a/core", Default: true, Health: repo(100, true)},
		{Address: "a/lib", Health: repo(70, true, "issues")},
		{Address: "a/old", Health: repo(40, false, "dormant")},
		{Address: "a/gone", Health: Repo{}},
	})
	if p.Counted != 3 || p.Score != 87 || p.Level != Watch || p.Attention != 1 || p.Watch != 1 {
		t.Fatalf("product %+v", p)
	}
	// a/lib (30 x 2) and a/old (60 x 1) rank equal, then by address
	if len(p.Findings) != 2 || p.Findings[0].Repo != "a/lib" || p.Findings[1].Repo != "a/old" {
		t.Fatalf("findings %+v", p.Findings)
	}
	if p := Weigh([]Member{{Address: "a/x", Health: repo(90, true)}, {Address: "a/y", Health: repo(85, false)}}); p.Level != Good || p.Score != 88 {
		t.Fatalf("good product %+v", p)
	}
	if p := Weigh([]Member{{Address: "a/x", Health: repo(30, true)}}); p.Level != Attention {
		t.Fatalf("attention product %+v", p)
	}
}
