package routes_test

import (
	"testing"

	"github.com/cantara/source-code-portal/internal/routes"
)

// TestBuilderOutputs is the golden list of every URL the Go side can format: the contract the SPA, the llms files, the
// sitemap and the e2e crawl rely on.
func TestBuilderOutputs(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"Dashboard", routes.Dashboard(), "/dashboard"},
		{"Commits", routes.Commits(), "/commits"},
		{"Dependencies", routes.Dependencies(), "/dependencies"},
		{"Group", routes.Group("platform"), "/groups/platform"},
		{"GroupCommits", routes.GroupCommits("platform"), "/groups/platform/commits"},
		{"Repo", routes.Repo("auth-service"), "/repos/auth-service"},
		{"RepoCommits", routes.RepoCommits("auth-service"), "/repos/auth-service/commits"},
		{"RepoDependencies", routes.RepoDependencies("auth-service"), "/repos/auth-service/dependencies"},
		{"Page", routes.Page("about"), "/pages/about"},
		{"RepoDoc", routes.RepoDoc("r", "docs/a b/c.md"), "/repos/r/docs/docs/a%20b/c.md"},
		{"RepoDocMD", routes.RepoDocMD("r", "x.md"), "/repos/r/docs/x.md"},
		{"ReadmeMD", routes.ReadmeMD("r"), "/repos/r/readme.md"},
		{"AgentsMD", routes.AgentsMD("r"), "/repos/r/agents.md"},
		{"ChangelogMD", routes.ChangelogMD("r"), "/repos/r/changelog.md"},
		{"RepoDependenciesMD", routes.RepoDependenciesMD("r"), "/repos/r/dependencies.md"},
		{"GroupDependenciesMD", routes.GroupDependenciesMD("g"), "/groups/g/dependencies.md"},
		{"Badge", routes.Badge("r", "issues"), "/badges/r/issues.svg"},
		{"BadgeGroup", routes.BadgeGroup("g", "repos"), "/badges/groups/g/repos.svg"},
		{"Media", routes.Media("r", "main", "img/a b.png"), "/media/r/main/img/a%20b.png"},
		{"MediaExt", routes.MediaExt("sig", "id"), "/media/ext/sig/id"},
		{"LLMs portal", routes.LLMs(""), "/llms.txt"},
		{"LLMs group", routes.LLMs("g"), "/groups/g/llms.txt"},
		{"LLMs repo", routes.LLMs("repo:r"), "/repos/r/llms.txt"},
		{"LLMsFull portal", routes.LLMsFull(""), "/llms-full.txt"},
		{"LLMsFull group", routes.LLMsFull("g"), "/groups/g/llms-full.txt"},
		{"LLMsFull repo", routes.LLMsFull("repo:r"), "/repos/r/llms-full.txt"},
		{"Sitemap", routes.Sitemap(), "/sitemap.xml"},
		{"Robots", routes.Robots(), "/robots.txt"},
		{"OpenAPI", routes.OpenAPI(), "/api/v1/openapi.yaml"},
		{"Schema", routes.Schema("scp-v1.json"), "/schemas/scp/scp-v1.json"},
		{"Webhook any", routes.Webhook(""), "/webhooks/github"},
		{"Webhook source", routes.Webhook("acme"), "/webhooks/github/acme"},
		{"API", routes.API("repos", "a b", "docs", "x/y.md"), "/api/v1/repos/a%20b/docs/x%2Fy.md"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestReservedSuffixesAndClassNames(t *testing.T) {
	if got := routes.ReservedSuffixes(); len(got) != 7 || got[0] != ".md" {
		t.Errorf("suffixes %v", got)
	}
	for c, want := range map[routes.Class]string{routes.ClassAPI: "api", routes.ClassSPA: "spa", routes.ClassServer: "server", routes.ClassAdmin: "admin", 0: "invalid"} {
		if c.String() != want {
			t.Errorf("%d: %s", c, c.String())
		}
	}
}
