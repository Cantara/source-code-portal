// Package health scores repositories and weighs them into products, so an owner sees what needs attention. Every point a score loses
// is a finding that says why.
package health

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"
)

const day = 24 * time.Hour

// Level is how a score reads.
type Level string

const (
	Good      Level = "good"
	Watch     Level = "watch"
	Attention Level = "attention"
)

// LevelOf is the level of a score: good from 80, watch from 50, attention below.
func LevelOf(score int) Level {
	switch {
	case score >= 80:
		return Good
	case score >= 50:
		return Watch
	}
	return Attention
}

// Signals are what a repository's health is read from.
type Signals struct {
	Archived   bool
	Deprecated bool // lifecycle: deprecated in its scp.yaml
	Public     bool
	LastChange time.Time // the newest default-branch commit by a person, else the last push
	Automated  bool      // the commits since LastChange are automated; with Before, every read commit is
	Before     bool      // the person's commit is older than LastChange: every read commit is automated
	LastCommit time.Time // the newest default-branch commit; zero when the commits are not read
	Release    string    // the latest release's tag; empty when there is none
	Released   time.Time
	License    string
	OpenIssues int
	Failing    bool // SCP keeps failing to read it
}

// Finding is a reason a score lost points.
type Finding struct {
	Code    string
	Penalty int
	Message string
}

// Repo is a repository's health. Counted is false for an archived or deprecated repository: it has no score.
type Repo struct {
	Counted  bool
	Score    int
	Level    Level
	Recent   bool // changed in the last 6 months
	Findings []Finding
}

// Assess scores a repository at now.
func Assess(s Signals, now time.Time) Repo {
	if s.Archived || s.Deprecated {
		return Repo{}
	}
	var fs []Finding
	add := func(code string, penalty int, format string, args ...any) {
		fs = append(fs, Finding{Code: code, Penalty: penalty, Message: fmt.Sprintf(format, args...)})
	}
	if s.Failing {
		add("failing", 25, "SCP cannot update it from GitHub")
	}
	age := now.Sub(s.LastChange)
	by, more := "", ""
	if s.Automated {
		by = " by a person"
		if s.Before {
			more = "more than "
		}
	}
	switch {
	case age > 730*day:
		add("dormant", 30, "No change%s for %s%s: archive it or revive it", by, more, span(age))
	case age > 182*day:
		add("quiet", 10, "No change%s for %s%s", by, more, span(age))
	}
	if s.Release != "" && !s.LastCommit.IsZero() && s.LastCommit.After(s.Released) && now.Sub(s.Released) > 90*day {
		add("unreleased", 20, "Unreleased changes since %s, released %s ago", s.Release, span(now.Sub(s.Released)))
	}
	if s.Public && s.License == "" {
		add("no-licence", 20, "No licence detected")
	}
	switch {
	case s.OpenIssues > 50:
		add("issues", 20, "%d open issues and pull requests", s.OpenIssues)
	case s.OpenIssues > 20:
		add("issues", 10, "%d open issues and pull requests", s.OpenIssues)
	}
	score := 100
	for _, f := range fs {
		score -= f.Penalty
	}
	score = max(score, 0)
	return Repo{Counted: true, Score: score, Level: LevelOf(score), Recent: age <= 182*day, Findings: fs}
}

// span is a duration in whole days, months or years.
func span(d time.Duration) string {
	days := int(d / day)
	switch {
	case days >= 730:
		return fmt.Sprintf("%d years", days/365)
	case days >= 60:
		return fmt.Sprintf("%d months", days/30)
	case days == 1:
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// Member is a repository of a product.
type Member struct {
	Address string
	Default bool // the group's default repository
	Health  Repo
}

// RepoFinding is a finding of one member.
type RepoFinding struct {
	Repo string
	Finding
}

// Product is the weighed health of a group of repositories. Counted is the number of members with a score; with none there is no score.
type Product struct {
	Counted, Attention, Watch int
	Score                     int
	Level                     Level
	Findings                  []RepoFinding
}

// weight is how much a member counts: 2 when changed in the last 6 months, else 1; three times that for the default repository.
func weight(m Member) int {
	w := 1
	if m.Health.Recent {
		w = 2
	}
	if m.Default {
		w *= 3
	}
	return w
}

// Weigh is the products' health: the weighted mean of its counted members, at least watch while one is at attention, with every
// member's findings ordered by penalty times weight.
func Weigh(members []Member) Product {
	var p Product
	type ranked struct {
		f    RepoFinding
		rank int
	}
	var all []ranked
	sum, total := 0, 0
	for _, m := range members {
		if !m.Health.Counted {
			continue
		}
		w := weight(m)
		p.Counted++
		sum += w * m.Health.Score
		total += w
		switch m.Health.Level {
		case Attention:
			p.Attention++
		case Watch:
			p.Watch++
		}
		for _, f := range m.Health.Findings {
			all = append(all, ranked{RepoFinding{Repo: m.Address, Finding: f}, f.Penalty * w})
		}
	}
	if total == 0 {
		return p
	}
	p.Score = int(math.Round(float64(sum) / float64(total)))
	p.Level = LevelOf(p.Score)
	if p.Level == Good && p.Attention > 0 {
		p.Level = Watch
	}
	slices.SortStableFunc(all, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.rank, a.rank), cmp.Compare(a.f.Repo, b.f.Repo))
	})
	p.Findings = make([]RepoFinding, len(all))
	for i, r := range all {
		p.Findings[i] = r.f
	}
	return p
}
