package main

import (
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
)

// TestServeForGuide serves a made-up catalogue for the screenshots of the guide page (`.claude/skills/guide-page`): three products of
// the organisation acme whose cards show every signal (people by hand and with AI, a coding agent, automation, a sweep, a dormant and
// a quiet repository, open issues, unreleased changes, an archived one), on GUIDE_SCP_ADDR until interrupted. Skipped without it.
// The dates are relative to now, so the pictures read the same whenever they are taken.
func TestServeForGuide(t *testing.T) {
	addr := os.Getenv("GUIDE_SCP_ADDR")
	if addr == "" {
		t.Skip("GUIDE_SCP_ADDR not set")
	}
	gh := githubtest.New(t)
	gh.AddToken("ghp_guide")
	now := time.Now()
	rng := rand.New(rand.NewPCG(7, 11))
	n := 0

	// work is count commits by who within the days before now (from, to: days ago), spread at random.
	type work struct {
		who      string
		count    int
		from, to float64
	}
	people := map[string]string{"ada": "Ada Example", "bo": "Bo Example", "chen": "Chen Example", "dana": "Dana Example"}
	subjects := []string{"Fix the retry on a timeout", "Add a test for the empty case", "Tidy the error messages", "Handle a missing header",
		"Speed up the lookup", "Document the configuration", "Split the handler", "Validate the input early"}
	// sha is a commit id whose short form differs from commit to commit.
	sha := func(n int) string { return fmt.Sprintf("%016x%024x", uint64(n)*0x9e3779b97f4a7c15, n) }
	commitOf := func(who string, at time.Time) githubtest.Commit {
		n++
		// Diff stats of every size, made up and the same at every run: small fixes, middling changes, a large one now and then.
		c := githubtest.Commit{SHA: sha(n), Date: at, Message: subjects[n%len(subjects)], Additions: []int{4, 18, 62, 7, 240, 11}[n%6],
			Deletions: []int{1, 9, 30, 0, 80, 11}[n%6], Files: 1 + n%5}
		switch who {
		case "renovate[bot]":
			c.AuthorName, c.AuthorLogin, c.Message = "renovate[bot]", "renovate[bot]", "chore(deps): update dependency to the latest minor"
		case "github-actions[bot]":
			c.AuthorName, c.AuthorLogin, c.Message = "github-actions[bot]", "github-actions[bot]", "ci: refresh the generated workflows"
		case "copilot-swe-agent[bot]":
			c.AuthorName, c.AuthorLogin = "Copilot", "copilot-swe-agent[bot]"
		default:
			person, ai, _ := strings.Cut(who, "+")
			c.AuthorName, c.AuthorLogin = people[person], person
			switch ai {
			case "claude":
				c.Message += "\n\nCo-authored-by: Claude Opus 5 <noreply@anthropic.com>"
			case "copilot":
				c.Message += "\n\nCo-authored-by: Copilot <175728472+Copilot@users.noreply.github.com>"
			}
		}
		return c
	}
	sweep := now.Add(-12 * 24 * time.Hour)
	repo := func(name, description, language string, works []work, swept bool, extra gogh.Repository) int64 {
		extra.Name, extra.Description, extra.Visibility = new(name), new(description), new("public")
		if extra.License == nil {
			extra.License = &gogh.License{SPDXID: new("Apache-2.0")}
		}
		id := gh.AddRepo("acme", extra)
		var cs []githubtest.Commit
		for _, w := range works {
			for range w.count {
				days := w.to + rng.Float64()*(w.from-w.to)
				cs = append(cs, commitOf(w.who, now.Add(-time.Duration(days*24*float64(time.Hour)))))
			}
		}
		if swept {
			n++
			cs = append(cs, githubtest.Commit{SHA: sha(n), Message: "chore: add the SPDX licence header",
				AuthorName: "Lin Example", AuthorLogin: "lin", Date: sweep})
		}
		slices.SortFunc(cs, func(a, b githubtest.Commit) int { return b.Date.Compare(a.Date) })
		gh.SetCommits(id, cs...)
		gh.SetFile(id, "README.md", "# "+name+"\n\n"+description+".\n")
		switch language {
		case "java":
			gh.SetFile(id, "pom.xml", "<project><groupId>com.acme</groupId><artifactId>"+name+"</artifactId><version>1.0</version></project>")
		case "go":
			gh.SetFile(id, "go.mod", "module github.com/acme/"+name+"\n\ngo 1.27\n")
		case "js":
			gh.SetFile(id, "package.json", `{"name": "`+name+`", "version": "1.0.0"}`)
		}
		return id
	}
	days := func(d float64) time.Time { return now.Add(-time.Duration(d * 24 * float64(time.Hour))) }

	api := repo("payments-api", "The payment API", "java", []work{{"ada", 9, 80, 1}, {"bo", 5, 70, 4}, {"ada+claude", 8, 40, 2},
		{"renovate[bot]", 16, 30, 0}}, true, gogh.Repository{})
	gh.SetRelease(api, "v4.2.0", "4.2.0", days(3))
	repo("payments-ledger", "Double-entry ledger", "java", []work{{"chen", 6, 80, 6}, {"chen+claude", 4, 25, 3},
		{"renovate[bot]", 6, 30, 1}}, true, gogh.Repository{})
	repo("payments-web", "The checkout pages", "js", []work{{"dana", 4, 60, 8}, {"copilot-swe-agent[bot]", 5, 20, 2},
		{"dana+copilot", 3, 15, 1}}, true, gogh.Repository{})
	repo("payments-gateway-legacy", "The old card gateway", "java", []work{{"bo", 12, 1600, 1300}}, true, gogh.Repository{})
	repo("payments-sdk", "Client library for the payment API", "java", []work{{"chen", 8, 600, 270}}, true,
		gogh.Repository{OpenIssuesCount: new(27)})
	identity := repo("identity-service", "Sign-in and tokens", "java", []work{{"ada", 7, 70, 2}, {"bo", 6, 60, 5},
		{"bo+claude", 3, 30, 4}, {"renovate[bot]", 8, 30, 2}}, true, gogh.Repository{})
	gh.SetRelease(identity, "v2.8.0", "2.8.0", days(140))
	repo("identity-admin", "The administrators' console", "js", []work{{"dana", 5, 90, 10}, {"dana+claude", 6, 28, 1}}, true,
		gogh.Repository{})
	sdk := repo("identity-sdk-go", "Go client for sign-in", "go", []work{{"chen", 5, 100, 12}, {"renovate[bot]", 3, 30, 5}}, true,
		gogh.Repository{})
	gh.SetRelease(sdk, "v0.9.1", "0.9.1", days(12))
	cli := repo("dev-cli", "The developers' command line", "go", []work{{"bo", 3, 50, 9}, {"bo+claude", 7, 30, 1}}, true,
		gogh.Repository{OpenIssuesCount: new(56)})
	gh.SetRelease(cli, "v1.4.0", "1.4.0", days(200))
	// Open pull requests (design/pulls.md): one waiting long for a review, one ready, one failing, and automation's updates.
	pull := func(n int, title, who string, opened, updated float64, p githubtest.Pull) githubtest.Pull {
		p.Number, p.Title, p.Author, p.Created, p.Updated, p.Base = n, title, who, days(opened), days(updated), "main"
		p.Head, p.HeadSHA, p.Bot = fmt.Sprintf("change-%d", n), fmt.Sprintf("%040x", 0xf000+n), strings.HasSuffix(who, "[bot]")
		return p
	}
	gh.SetPulls(api, pull(41, "Retry a declined card once", "bo", 12, 9, githubtest.Pull{Review: "REVIEW_REQUIRED", Checks: "SUCCESS",
		Requested: 1, Additions: 86, Deletions: 12, Files: 4}),
		pull(42, "Refund in the customer's currency", "ada", 2, 1, githubtest.Pull{Approved: true, Checks: "SUCCESS", Additions: 140, Deletions: 30, Files: 6}),
		pull(39, "chore(deps): update jackson to 2.20", "renovate[bot]", 44, 3, githubtest.Pull{Checks: "SUCCESS", Additions: 2, Deletions: 2, Files: 1}))
	gh.SetPulls(identity, pull(18, "Rotate the signing keys every 90 days", "chen", 5, 2, githubtest.Pull{Approved: true, Checks: "FAILURE",
		Additions: 210, Deletions: 45, Files: 9}))
	gh.SetPulls(sdk, pull(7, "chore(deps): update golang.org/x/oauth2", "renovate[bot]", 50, 20, githubtest.Pull{Checks: "SUCCESS",
		Additions: 4, Deletions: 4, Files: 2}))
	// Open issues (design/issues.md): one nobody has looked at, a bug nobody took, a feature someone has, automation's dashboard.
	issue := func(n int, title, who string, opened, updated float64, i githubtest.Issue) githubtest.Issue {
		i.Number, i.Title, i.Author, i.Created, i.Updated, i.Bot = n, title, who, days(opened), days(updated), strings.HasSuffix(who, "[bot]")
		return i
	}
	gh.SetIssues(api, issue(51, "Declined cards show no reason", "dana", 9, 8, githubtest.Issue{}),
		issue(48, "Refunds fail for amounts over 10,000", "chen", 20, 2, githubtest.Issue{Labels: []string{"bug"}, Comments: 4}),
		issue(45, "Pay by bank transfer", "ada", 60, 5, githubtest.Issue{Type: "Feature", Assignees: []string{"bo"}, Milestone: "Q1"}),
		issue(3, "Dependency Dashboard", "renovate[bot]", 200, 1, githubtest.Issue{}))
	gh.SetIssues(identity, issue(22, "Sessions end after a deploy", "bo", 3, 1, githubtest.Issue{Labels: []string{"bug"}, Assignees: []string{"chen"}}))
	// The command line's backlog: the Backlog finding judges the issues the portal read, not GitHub's count.
	backlog := make([]githubtest.Issue, 0, 56)
	for n := 1; n <= 56; n++ {
		backlog = append(backlog, issue(n, "Handbook page "+strconv.Itoa(n)+" is out of date", "dana", float64(20+n), float64(10+n), githubtest.Issue{}))
	}
	gh.SetIssues(cli, backlog...)
	repo("ci-templates", "Shared CI workflows", "", []work{{"ada", 1, 110, 100}, {"github-actions[bot]", 24, 30, 0}}, true,
		gogh.Repository{})
	repo("handbook", "How the team works", "", []work{{"dana", 4, 45, 6}}, true, gogh.Repository{})
	repo("dev-proxy", "A local proxy for the APIs", "go", []work{{"chen", 3, 1200, 900}}, true,
		gogh.Repository{License: &gogh.License{}})
	repo("old-prototype", "The first prototype", "java", []work{{"bo", 4, 2500, 2400}}, false, gogh.Repository{Archived: new(true)})

	bundle, err := filepath.Abs("../../web/bundle")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	token, path := filepath.Join(dir, "token"), filepath.Join(dir, "scp.yaml")
	body := `store: {path: ` + filepath.Join(dir, "scp.db") + `}
server: {public_addr: "` + addr + `", admin_addr: "127.0.0.1:0", public_url: "http://` + addr + `"}
web: {dir: ` + bundle + `}
github:
  sources:
    - {id: gh, base_url: "` + gh.BaseURL() + `", orgs: [acme], token_file: ` + token + `}
catalog:
  groups:
    - {id: payments, name: Payments, description: "Checkout, the ledger and the card gateway", default_repo: payments-api, match: [{prefix: payments-}]}
    - {id: identity, name: Identity, description: Sign-in and access management, default_repo: identity-service, match: [{prefix: identity-}]}
    - {id: tools, name: Developer tools, description: "The command line, CI templates and the handbook", default_repo: dev-cli, match: [{unclaimed: true}]}
`
	if os.WriteFile(token, []byte("ghp_guide"), 0o600) != nil || os.WriteFile(path, []byte(body), 0o600) != nil {
		t.Fatal("write config")
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := newServer(t.Context(), cfg, testDeps(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.run(ctx); err != nil {
		t.Fatal(err)
	}
}
