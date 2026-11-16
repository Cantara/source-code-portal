package health

import (
	"fmt"
	"time"
)

// The rules the portal judges by, in one place: Assess, LevelOf and ActivityOf apply them, and Describe tells them, so every page
// says what the server does and a change here changes every word that names it (design/present.md, P-62).
const (
	// WindowDays is the window of the activity counts: the work of the last 30 days.
	WindowDays = 30
	// recentFor is how long a change keeps a repository recent; it also weighs a member twice in its product.
	recentFor = 182 * day
	// dormantAfter is how long without a change makes a repository dormant.
	dormantAfter = 730 * day
	// unreleasedAfter is how old a release with changes since must be before it is a finding.
	unreleasedAfter = 90 * day
	// issuesMany and issuesTooMany are the open issues that cost points.
	issuesMany, issuesTooMany = 20, 50
	// reviewsAfter and reviewsLongAfter are how long a person's pull request may wait idle for a review before it costs points.
	reviewsAfter, reviewsLongAfter = 7 * day, 30 * day
	// UpdatesAfter is how long automation's pull requests (dependency updates) may stay open before they cost points.
	UpdatesAfter = 30 * day
	// goodFrom and watchFrom are the scores the levels start at.
	goodFrom, watchFrom = 80, 50
	// highFrom and mediumFrom are the penalties the severities start at.
	highFrom, mediumFrom = 25, 20
)

// penalties are what each finding costs, by its code; issues costs the first past issuesTooMany and the second past issuesMany,
// reviews the first past reviewsLongAfter and the second past reviewsAfter.
var penalties = map[string][]int{ //nolint:gochecknoglobals // a constant table
	"dormant":    {30},
	"failing":    {25},
	"unreleased": {20},
	"no-licence": {20},
	"issues":     {20, 10},
	"reviews":    {15, 5},
	"updates":    {5},
	"quiet":      {10},
}

// Activity is how recently the work that counts changed a repository.
type Activity string

const (
	Active   Activity = "active"
	Recent   Activity = "recent"
	Quiet    Activity = "quiet"
	Dormant  Activity = "dormant"
	Archived Activity = "archived"
)

// ActivityOf is the activity of a repository whose last change of the work that counts was at last (zero: none read) at now.
func ActivityOf(last time.Time, archived bool, now time.Time) Activity {
	switch age := now.Sub(last); {
	case archived:
		return Archived
	case last.IsZero():
		return Dormant
	case age <= WindowDays*day:
		return Active
	case age <= recentFor:
		return Recent
	case age <= dormantAfter:
		return Quiet
	}
	return Dormant
}

// Severity is how much a finding weighs, by its penalty.
type Severity string

const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
)

// SeverityOf is the severity of a penalty.
func SeverityOf(penalty int) Severity {
	switch {
	case penalty >= highFrom:
		return High
	case penalty >= mediumFrom:
		return Medium
	}
	return Low
}

// Rules are the rules told: what a page needs to explain the server's signals in their current terms.
type Rules struct {
	WindowDays int
	Levels     []LevelRule
	Severities []SeverityRule
	Activity   []ActivityRule
	Findings   []Kind
}

// LevelRule is a health level and the score it starts at.
type LevelRule struct {
	Level Level
	Label string
	From  int
}

// SeverityRule is a severity and the penalty it starts at.
type SeverityRule struct {
	Severity Severity
	Label    string
	From     int
}

// ActivityRule is an activity, its name, what it means and the age of the last change it covers (WithinDays 0: no bound).
type ActivityRule struct {
	Activity   Activity
	Label      string
	About      string
	WithinDays int
}

// Kind is a kind of finding: its name, why it matters, when it costs how much (the most costly first) and what resolves it.
type Kind struct {
	Code      string
	Label     string
	About     string
	Penalties []Penalty
	Action    Action
}

// Action is the next step on a finding: what to do, and where on the repository's GitHub page (Path below it; empty: the page).
type Action struct {
	Label string
	Path  string
}

// Penalty is what a finding costs and when.
type Penalty struct {
	Points int
	When   string
}

// Describe is the rules as they are applied, in words built from the same values.
func Describe() Rules {
	days := func(d time.Duration) int { return int(d / day) }
	return Rules{
		WindowDays: WindowDays,
		Levels: []LevelRule{
			{Level: Good, Label: "Good", From: goodFrom},
			{Level: Watch, Label: "Watch", From: watchFrom},
			{Level: Attention, Label: "Needs attention", From: 0},
		},
		Severities: []SeverityRule{
			{Severity: High, Label: "High", From: highFrom},
			{Severity: Medium, Label: "Medium", From: mediumFrom},
			{Severity: Low, Label: "Low", From: 0},
		},
		Activity: []ActivityRule{
			{Activity: Active, Label: "Active", About: fmt.Sprintf("changed in the last %d days", WindowDays), WithinDays: WindowDays},
			{Activity: Recent, Label: "Recent", About: "changed in the last " + span(recentFor), WithinDays: days(recentFor)},
			{Activity: Quiet, Label: "Quiet", About: "changed in the last " + span(dormantAfter), WithinDays: days(dormantAfter)},
			{Activity: Dormant, Label: "Dormant", About: "no change for " + span(dormantAfter) + ", or none read"},
			{Activity: Archived, Label: "Archived", About: "archived on GitHub"},
		},
		Findings: []Kind{
			{Code: "dormant", Label: "Dormant",
				About:     fmt.Sprintf("No one has worked on it for %s: archive it, or bring it back to life.", span(dormantAfter)),
				Penalties: []Penalty{{penalties["dormant"][0], "No change by the team for more than " + span(dormantAfter) + "."}},
				Action:    Action{"Archive it in its settings", "settings"}},
			{Code: "failing", Label: "Not updating", About: "The portal cannot read it from GitHub, so what it shows may be old.",
				Penalties: []Penalty{{penalties["failing"][0], "The portal cannot update it from GitHub."}},
				Action:    Action{"Check that it is still on GitHub", ""}},
			{Code: "unreleased", Label: "Unreleased",
				About:     fmt.Sprintf("Changes since its last release, %s or more ago, wait to be released.", span(unreleasedAfter)),
				Penalties: []Penalty{{penalties["unreleased"][0], "Changes since the latest release, which is more than " + span(unreleasedAfter) + " old."}},
				Action:    Action{"Draft a release", "releases/new"}},
			{Code: "no-licence", Label: "No licence", About: "A public repository without a licence cannot safely be used by others.",
				Penalties: []Penalty{{penalties["no-licence"][0], "A public repository without a licence GitHub detects."}},
				Action:    Action{"Add a licence", "community/license/new"}},
			{Code: "issues", Label: "Backlog", About: "Many open issues wait for an answer.",
				Penalties: []Penalty{
					{penalties["issues"][0], fmt.Sprintf("More than %d open issues.", issuesTooMany)},
					{penalties["issues"][1], fmt.Sprintf("More than %d open issues.", issuesMany)},
				},
				Action: Action{"Triage the open issues", "issues"}},
			{Code: "reviews", Label: "Waiting for review", About: "A person's pull request waits for a review and nothing happens.",
				Penalties: []Penalty{
					{penalties["reviews"][0], "A pull request by a person idle for more than " + span(reviewsLongAfter) + " waiting for a review."},
					{penalties["reviews"][1], "A pull request by a person idle for more than " + span(reviewsAfter) + " waiting for a review."},
				},
				Action: Action{"Review the open pull requests", "pulls"}},
			{Code: "updates", Label: "Old updates", About: "Dependency updates from automation stay open and nobody merges or closes them.",
				Penalties: []Penalty{{penalties["updates"][0], "A pull request by automation open for more than " + span(UpdatesAfter) + "."}},
				Action:    Action{"Merge or close the dependency updates", "pulls"}},
			{Code: "quiet", Label: "Quiet",
				About:     fmt.Sprintf("No one has changed it for %s: is it done, or forgotten?", span(recentFor)),
				Penalties: []Penalty{{penalties["quiet"][0], "No change by the team for more than " + span(recentFor) + "."}},
				Action:    Action{"Look at its last changes", "commits"}},
		},
	}
}
