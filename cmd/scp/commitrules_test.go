package main

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/commits"
	"github.com/cantara/source-code-portal/internal/config"
)

// The deployment's assistants (commits.assistants) reach the rules the feeds classify with; without them the rules are the built-in.
func TestCommitRulesFromConfig(t *testing.T) {
	cfg, _, err := config.Load(configFile(t, "commits:\n  assistants:\n    - name: Helper Cloud\n      co_authors: [\"helper-cloud[bot]\"]\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &commits.Commit{AuthorLogin: "ada", Body: "Co-authored-by: helper-cloud[bot] <7+helper-cloud[bot]@users.noreply.ghe.com>"}
	if o, a := commitRules(cfg.Commits).Classify(c); o != commits.OriginAssisted || a != "Helper Cloud" {
		t.Errorf("configured: %s %q", o, a)
	}
	if o, _ := commitRules(config.Commits{}).Classify(c); o != commits.OriginAutomation {
		t.Errorf("built-in: %s", o)
	}
}
