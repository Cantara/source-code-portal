package commits

import (
	"cmp"
	"regexp"
	"strings"
)

// Origin is who made a commit (design/layout.md, P-40): a person, a person with an AI, a coding agent, or automation.
type Origin uint8

const (
	OriginPerson     Origin = iota // a person, no trace of an AI
	OriginAssisted                 // a person with an AI co-author (trailer) or an AI tool's footer
	OriginAgent                    // a coding agent as the author
	OriginAutomation               // CI, dependency and release bots
)

func (k Origin) String() string {
	return [...]string{"person", "assisted", "agent", "automation"}[k]
}

// AI reports whether the commit carries an AI's work: assisted or by an agent.
func (k Origin) AI() bool { return k == OriginAssisted || k == OriginAgent }

// agents are the coding agents that commit as themselves, by login (or name without one), lower case, with the assistant they are.
var agents = map[string]string{ //nolint:gochecknoglobals // a fixed table
	"claude":                       "Claude",
	"claude[bot]":                  "Claude",
	"copilot":                      "Copilot",
	"copilot-swe-agent[bot]":       "Copilot",
	"devin-ai-integration[bot]":    "Devin",
	"google-labs-jules[bot]":       "Jules",
	"chatgpt-codex-connector[bot]": "Codex",
	"cursor[bot]":                  "Cursor",
	"cursoragent":                  "Cursor",
	"cursor-agent":                 "Cursor",
	"openhands-agent":              "OpenHands",
}

// aiDomains are the e-mail domains of AI co-authors; aiLocals the local parts (after a GitHub noreply's "<id>+").
var (
	aiDomains = map[string]bool{"anthropic.com": true, "claude.com": true, "cursor.com": true, "aider.chat": true, //nolint:gochecknoglobals // a fixed table
		"openai.com": true, "cognition.ai": true}
	aiLocals = map[string]bool{"claude": true, "copilot": true, "cursoragent": true, "codex": true, //nolint:gochecknoglobals // a fixed table
		"devin-ai-integration[bot]": true, "gemini-code-assist[bot]": true, "google-labs-jules[bot]": true}
	trailer = regexp.MustCompile(`(?im)^co-authored-by:[ \t]*([^<\n]*?)[ \t]*<([^>\n]*)>`)
	note    = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
)

// footers are AI tools' lines in a message body, with the assistant they name.
var footers = []struct{ mark, assistant string }{ //nolint:gochecknoglobals // a fixed table
	{"generated with [claude code]", "Claude Code"},
	{"generated with claude code", "Claude Code"},
}

// botLike reports whether a name or login is automation's: a GitHub App (`renovate[bot]`) or named like a bot (`Renovate Bot`,
// `snyk-bot`).
func botLike(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "renovate", "dependabot", "github-actions", "jenkins", "dependabot-preview":
		return true
	}
	return strings.HasSuffix(s, "[bot]") || strings.HasSuffix(s, " bot") || strings.HasSuffix(s, "-bot") || strings.HasSuffix(s, "_bot")
}

// Classify returns the origin of c and the assistant it names: the AI co-author's or agent's name ("Claude Opus 5", "Copilot"), "" for
// none. The order: a release plugin or a bot author is automation; a known agent is an agent; an AI co-author or footer is assisted;
// a bot co-author (a bot's pull request merged by a person) is automation.
func Classify(c *Commit) (Origin, string) {
	if strings.HasPrefix(c.Subject, "[maven-release-plugin]") {
		return OriginAutomation, ""
	}
	who := strings.ToLower(c.AuthorLogin)
	if who == "" {
		who = strings.ToLower(c.AuthorName)
	}
	ai, bot := coAuthors(c.Body)
	if a, ok := agents[who]; ok {
		return OriginAgent, cmp.Or(ai, a)
	}
	if botLike(c.AuthorLogin) || botLike(c.AuthorName) {
		return OriginAutomation, ""
	}
	if ai != "" {
		return OriginAssisted, ai
	}
	lower := strings.ToLower(c.Body)
	for _, f := range footers {
		if strings.Contains(lower, f.mark) {
			return OriginAssisted, f.assistant
		}
	}
	if bot {
		return OriginAutomation, ""
	}
	return OriginPerson, ""
}

// coAuthors reads the Co-authored-by trailers of a body: the first AI co-author's name (its context note dropped:
// "Claude Opus 5 (1M context)" is "Claude Opus 5") and whether a bot co-authored.
func coAuthors(body string) (ai string, bot bool) {
	if !strings.Contains(strings.ToLower(body), "co-authored-by:") {
		return "", false
	}
	for _, m := range trailer.FindAllStringSubmatch(body, -1) {
		name, mail := m[1], strings.ToLower(m[2])
		local, domain, _ := strings.Cut(mail, "@")
		if _, after, ok := strings.Cut(local, "+"); ok {
			local = after
		}
		switch {
		case aiDomains[domain] || aiLocals[local]:
			if ai == "" {
				ai = cmp.Or(strings.TrimSpace(note.ReplaceAllString(name, "")), local)
			}
		case botLike(name) || botLike(local):
			bot = true
		}
	}
	return ai, bot
}

// Assistant is the family of an assistant's name, its first word: "Claude Opus 5" is "Claude".
func Assistant(name string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(name), " ")
	return first
}
