package actor

import "testing"

func TestBot(t *testing.T) {
	for s, want := range map[string]bool{
		"renovate[bot]": true, "dependabot[bot]": true, "app/dependabot": true, "Renovate Bot": true, "snyk-bot": true,
		"Jenkins": true, "github-actions": true, "oranheim": false, "totto": false, "robotics": false, "": false,
	} {
		if got := Bot(s); got != want {
			t.Errorf("Bot(%q) = %v, want %v", s, got, want)
		}
	}
}
