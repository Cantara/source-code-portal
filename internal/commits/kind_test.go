package commits

import (
	"strings"
	"testing"
)

// TestClassify tells people, people with an AI, coding agents and automation apart, by the shapes the tools leave in commits.
func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name              string
		c                 Commit
		origin            Origin
		assistant, family string
	}{
		{"a person", Commit{AuthorName: "Ada Lovelace", AuthorLogin: "ada", Body: "Explains why.\n\nSigned-off-by: Ada <ada@example.org>"},
			OriginPerson, "", ""},
		{"Claude Code's trailer and footer", Commit{AuthorName: "Thor", AuthorLogin: "totto",
			Body: "Why.\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)\n\nCo-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"},
			OriginAssisted, "Claude Opus 4.8", "Claude"},
		{"the footer alone", Commit{AuthorName: "Bo", Body: "🤖 Generated with [Claude Code](https://claude.com/claude-code)"},
			OriginAssisted, "Claude Code", "Claude"},
		{"Copilot as co-author", Commit{AuthorLogin: "bo", Body: "Co-authored-by: Copilot <175728472+Copilot@users.noreply.github.com>"},
			OriginAssisted, "Copilot", "Copilot"},
		{"an own agent at an AI address", Commit{AuthorLogin: "totto", Body: "Co-authored-by: ExoCortex <claude@exoreaction.com>"},
			OriginAssisted, "ExoCortex", "ExoCortex"},
		{"Cursor's agent", Commit{AuthorLogin: "bo", Body: "Co-authored-by: Cursor Agent <cursoragent@cursor.com>"},
			OriginAssisted, "Cursor Agent", "Cursor"},
		{"a person as co-author", Commit{AuthorLogin: "bo", Body: "Co-authored-by: Ada <ada@example.org>"}, OriginPerson, "", ""},
		{"Claude as the author", Commit{AuthorName: "Claude", AuthorLogin: "claude", Body: "🤖 Generated with [Claude Code](https://claude.com/claude-code)"},
			OriginAgent, "Claude", "Claude"},
		{"Copilot's coding agent", Commit{AuthorName: "Copilot", AuthorLogin: "copilot-swe-agent[bot]",
			Body: "Co-authored-by: bo <12+bo@users.noreply.github.com>"}, OriginAgent, "Copilot", "Copilot"},
		{"an agent naming its model", Commit{AuthorLogin: "claude[bot]", Body: "Co-authored-by: Claude Sonnet 5 <noreply@anthropic.com>"},
			OriginAgent, "Claude Sonnet 5", "Claude"},
		{"Renovate as an App", Commit{AuthorName: "renovate[bot]", AuthorLogin: "renovate[bot]"}, OriginAutomation, "", ""},
		{"Renovate before Apps", Commit{AuthorName: "Renovate Bot"}, OriginAutomation, "", ""},
		{"a bot named with a dash", Commit{AuthorName: "snyk-bot"}, OriginAutomation, "", ""},
		{"a release", Commit{AuthorLogin: "ada", Subject: "[maven-release-plugin] prepare release v1.2"}, OriginAutomation, "", ""},
		{"a bot's pull request merged by a person", Commit{AuthorLogin: "totto",
			Body: "* Update dependency x\n\nCo-authored-by: renovate[bot] <29139614+renovate[bot]@users.noreply.github.com>"}, OriginAutomation, "", ""},
		{"an AI beats a bot co-author", Commit{AuthorLogin: "totto",
			Body: "Co-authored-by: snyk-bot <snyk-bot@snyk.io>\nCo-authored-by: Claude <noreply@anthropic.com>"}, OriginAssisted, "Claude", "Claude"},
	} {
		origin, assistant := Classify(&tc.c)
		if origin != tc.origin || assistant != tc.assistant || Assistant(assistant) != tc.family {
			t.Errorf("%s: %s %q (%q), want %s %q (%q)", tc.name, origin, assistant, Assistant(assistant), tc.origin, tc.assistant, tc.family)
		}
	}
}

// TestBodyKeepsTrailers: a body past the bound is cut in the middle, keeping the trailers that say who made the commit.
func TestBodyKeepsTrailers(t *testing.T) {
	long := "Subject\n\n" + strings.Repeat("word ", 2000) + "\n\nCo-authored-by: Claude Opus 5 <noreply@anthropic.com>"
	b := body(long)
	if n := len([]rune(b)); n > maxBody || !strings.HasSuffix(b, "Co-authored-by: Claude Opus 5 <noreply@anthropic.com>") ||
		!strings.Contains(b, "…\n\n") {
		t.Errorf("cut body (%d runes) ends %q", n, b[len(b)-80:])
	}
	if k, _ := Classify(&Commit{AuthorLogin: "ada", Body: b}); k != OriginAssisted {
		t.Errorf("the cut body is %s", k)
	}
	plain := body("Subject\n\n" + strings.Repeat("word ", 2000))
	if n := len([]rune(plain)); n != maxBody || !strings.HasSuffix(plain, "…") {
		t.Errorf("a body without trailers: %d runes", n)
	}
}
