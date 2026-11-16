// Package actor tells automation from people by the names GitHub shows: one test for the commits and the pull requests.
package actor

import "strings"

// Bot reports whether a name or login is automation's: a GitHub App (`renovate[bot]`, `app/dependabot`), a known bot, or one named
// like a bot (`Renovate Bot`, `snyk-bot`).
func Bot(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "app/")
	switch s {
	case "renovate", "dependabot", "github-actions", "jenkins", "dependabot-preview", "renovate-bot", "snyk-bot":
		return true
	}
	return strings.HasSuffix(s, "[bot]") || strings.HasSuffix(s, " bot") || strings.HasSuffix(s, "-bot") || strings.HasSuffix(s, "_bot")
}

// Branch reports whether a branch is a dependency bot's update branch (Renovate, Dependabot, Snyk). Snyk opens its pull requests
// under the account of the person who connected it, so its branch, not its author, tells them apart.
func Branch(name string) bool {
	for _, p := range []string{"renovate/", "dependabot/", "snyk-fix-", "snyk-upgrade-"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
