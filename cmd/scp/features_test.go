package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	gogh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
)

// busy gives the public repository 60 open issues and pull requests on GitHub's listing, past the Backlog's thresholds.
func busy(gh **githubtest.Server) func(*githubtest.Server) {
	return func(s *githubtest.Server) {
		*gh = s
		s.Update(s.ID("acme", "open"), func(r *gogh.Repository) { r.OpenIssuesCount = new(60) })
	}
}

// TestFeaturesOff: with pull requests and issues switched off they are absent, not empty: GitHub is never asked for them, and no
// route, tool, search kind, badge, sitemap entry, layout element, rule or prerequisite of theirs is left. The Backlog judges GitHub's
// count of issues and pull requests together and says so.
func TestFeaturesOff(t *testing.T) {
	var gh *githubtest.Server
	get, _ := privatePortalAt(t, "access: {private_groups: [staff]}\nfeatures: {pulls: false, issues: false}\n", busy(&gh))
	for _, l := range gh.Ledger() {
		if strings.Contains(l, "/pulls") || strings.Contains(l, "/issues") {
			t.Errorf("GitHub asked for a switched-off feature: %s", l)
		}
	}
	for path, want := range map[string]int{"/api/v1/pulls": 404, "/api/v1/issues": 404, "/badges/repos/acme/open/pulls.svg": 404,
		"/badges/groups/all/pulls.svg": 404, "/badges/repos/acme/open/issues.svg": 200, "/api/v1/tools/open_issues": 404} {
		h := staff
		if strings.HasPrefix(path, "/api/v1/tools/") {
			h = append([]string{":post", `{"input":{}}`}, staff...)
		}
		if code, b, _ := get(path, h...); code != want {
			t.Errorf("%s: %d, want %d: %s", path, code, want, b)
		}
	}
	if _, b, _ := get("/api/v1/tools", staff...); strings.Contains(b, "open_pull_requests") || strings.Contains(b, "open_issues") ||
		strings.Contains(b, `"pull"`) || strings.Contains(b, `"issue"`) {
		t.Errorf("tools name a switched-off feature: %s", b)
	}
	if _, b, _ := get("/sitemap.xml"); strings.Contains(b, "/pulls") || strings.Contains(b, "/issues") {
		t.Errorf("sitemap: %s", b)
	}
	if _, b, _ := get("/api/v1/pages/dashboard", staff...); strings.Contains(b, `"pulls"`) {
		t.Errorf("dashboard layout: %s", b)
	}
	var rules oapi.Rules
	if _, b, _ := get("/api/v1/rules"); json.Unmarshal([]byte(b), &rules) != nil || rules.Features.Pulls || rules.Features.Issues ||
		strings.Contains(b, `"reviews"`) || strings.Contains(b, `"updates"`) || !strings.Contains(b, `"issues"`) {
		t.Errorf("rules: %s", b)
	}
	if _, b, _ := get("/api/v1/prerequisites", staff...); strings.TrimSpace(b) != `{"unmet":[]}` {
		t.Errorf("prerequisites: %s", b)
	}
	var d oapi.RepoDetail
	if _, b, _ := get("/api/v1/repos/acme/open", staff...); json.Unmarshal([]byte(b), &d) != nil || d.Repo.OpenIssues != 60 ||
		!d.Repo.OpenIssuesWithPulls || d.Repo.OpenPulls != nil || !strings.Contains(b, "60 open issues and pull requests") {
		t.Errorf("repository: %s", b)
	}
}

// TestPullsOff: with pull requests off and issues on, the issues stay, and the Backlog judges the issues read, exact.
func TestPullsOff(t *testing.T) {
	var gh *githubtest.Server
	get, _ := privatePortalAt(t, "access: {private_groups: [staff]}\nfeatures: {pulls: false}\n", busy(&gh))
	var d oapi.RepoDetail
	var body string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, body, _ = get("/api/v1/repos/acme/open", staff...); json.Unmarshal([]byte(body), &d) == nil && !d.Repo.OpenIssuesWithPulls {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the issues are not read: %+v", d.Repo)
		}
	}
	if d.Repo.OpenIssues != 0 || d.Repo.OpenPulls != nil || strings.Contains(body, "open issues") {
		t.Errorf("repository: %s", body)
	}
	if code, b, _ := get("/api/v1/issues", staff...); code != http.StatusOK || !strings.Contains(b, "zq hidden issue") {
		t.Errorf("issues: %d %s", code, b)
	}
	if code, _, _ := get("/api/v1/pulls", staff...); code != http.StatusNotFound {
		t.Errorf("pulls: %d", code)
	}
	if _, b, _ := get("/api/v1/tools", staff...); strings.Contains(b, "open_pull_requests") || !strings.Contains(b, "open_issues") {
		t.Errorf("tools: %s", b)
	}
	for _, l := range gh.Ledger() {
		if strings.Contains(l, "/pulls") {
			t.Errorf("GitHub asked for the pull requests: %s", l)
		}
	}
}
