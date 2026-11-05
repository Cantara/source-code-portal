package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/prereq"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// TestPrerequisites: an App installation without Issues and its events is told, the details to an administrator only; with every
// permission nothing is.
func TestPrerequisites(t *testing.T) {
	srv := githubtest.New(t)
	srv.Install("acme")
	srv.SetGrants("acme", map[string]string{"contents": "read", "metadata": "read", "pull_requests": "read", "checks": "read", "statuses": "read"},
		[]string{"push", "release", "repository", "pull_request", "pull_request_review", "check_suite", "status"})
	c, err := github.New(config.GitHub{Sources: []config.Source{{ID: "gh", BaseURL: srv.BaseURL(), Orgs: []string{"acme"},
		App: &config.App{ID: srv.AppID, PrivateKey: config.NewSecret(srv.PrivateKeyPEM())}}}}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	src := c.Source("gh")
	if err := src.RefreshInstallations(t.Context()); err != nil {
		t.Fatal(err)
	}
	empty := snapshot.NewPublisher(time.Hour, time.Hour, slog.New(slog.DiscardHandler)).Current()
	p := &prereqs{sources: []string{"gh"}, source: c.Source, current: func() *snapshot.Snapshot { return empty }, webhooks: true,
		log: slog.New(slog.DiscardHandler)}
	get := func(admin bool) oapi.PrerequisiteList {
		t.Helper()
		ctx := httpapi.WithViewer(t.Context(), httpapi.Viewer{Admin: admin, Private: admin})
		res, err := p.GetPrerequisites(ctx, oapi.GetPrerequisitesRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		return oapi.PrerequisiteList(res.(oapi.GetPrerequisites200JSONResponse))
	}
	pub := get(false)
	if len(pub.Unmet) != 1 || pub.Unmet[0].Feature != oapi.PrerequisiteFeatureIssues || pub.Unmet[0].Permissions != nil ||
		pub.Unmet[0].Owners != nil || pub.Unmet[0].Fix != nil {
		t.Errorf("the public's answer: %+v; want the feature and its effect only", pub)
	}
	adm := get(true).Unmet
	if len(adm) != 1 || !slices.Equal(*adm[0].Permissions, []string{"Issues"}) || !slices.Equal(*adm[0].Events, []string{"Issues", "Issue comment"}) ||
		!slices.Equal(*adm[0].Owners, []string{"acme"}) || !strings.Contains(*adm[0].Fix, "set Issues to Read-only") {
		t.Errorf("the administrator's answer: %+v", adm)
	}
	if w := warnings(p.check()); len(w) != 1 || !strings.HasPrefix(w[0], "Issues (gh): Open issues are not read") {
		t.Errorf("status warnings: %v", w)
	}

	srv.SetGrants("acme", githubtest.AllPermissions, githubtest.AllEvents)
	if err := src.RefreshInstallations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := get(true); len(got.Unmet) != 0 {
		t.Errorf("every prerequisite holds: %+v", got)
	}
}

// TestPrerequisitesOfAToken: a token GitHub refuses (403) for every repository's issues is told on /api/v1/prerequisites; its
// pull requests, read, are not.
func TestPrerequisitesOfAToken(t *testing.T) {
	get, _ := privatePortalAt(t, "access: {private_groups: [staff]}\n", func(gh *githubtest.Server) {
		gh.Fail("GET /repos/acme/open/issues", 403, 1000, 0)
		gh.Fail("GET /repos/acme/zq-hidden/issues", 403, 1000, 0)
	})
	var body string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, body, _ = get("/api/v1/prerequisites"); strings.Contains(body, `"issues"`) {
			break
		}
	}
	var list oapi.PrerequisiteList
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Unmet) != 1 || list.Unmet[0].Feature != oapi.PrerequisiteFeatureIssues {
		t.Fatalf("prerequisites = %s", body)
	}
}

// TestPrerequisitesDocumented keeps what the documentation says each feature needs of GitHub equal to internal/prereq: the
// operator guide's table row by row, and every permission and event named where operators set them up.
func TestPrerequisitesDocumented(t *testing.T) {
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	guide := read("docs/operator-guide.md")
	_, table, ok := strings.Cut(guide, "<!-- prerequisites:")
	table, _, ok2 := strings.Cut(table, "<!-- /prerequisites -->")
	if !ok || !ok2 {
		t.Fatal("docs/operator-guide.md: no prerequisites table between its markers")
	}
	var rows [][]string
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Feature") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		rows = append(rows, cells)
	}
	if len(rows) != len(prereq.Needs) {
		t.Fatalf("the operator guide's table has %d rows, internal/prereq %d features", len(rows), len(prereq.Needs))
	}
	for i, n := range prereq.Needs {
		perms, events := strings.Join(names(n.Permissions, prereq.PermissionName), ", "), strings.Join(names(n.Events, prereq.EventName), ", ")
		if len(rows[i]) != 3 || !strings.HasPrefix(rows[i][0], n.Title) || rows[i][1] != perms || rows[i][2] != events {
			t.Errorf("operator guide row %d = %q; want %s | %s | %s", i+1, rows[i], n.Title, perms, events)
		}
	}
	for _, p := range []string{"docs/production.md", "deploy/config.example.yaml", "plugins/scp/skills/run-scp/SKILL.md",
		"web/src/features/guide/view.tsx", ".claude/skills/configure-scp/SKILL.md", "docs/security-model.md"} {
		text := read(p)
		for _, n := range prereq.Needs {
			for _, perm := range n.Permissions {
				if name := prereq.PermissionName(perm); !strings.Contains(text, name) {
					t.Errorf("%s does not name the permission %s (%s)", p, name, n.Title)
				}
			}
		}
	}
	for _, p := range []string{"docs/production.md", "deploy/config.example.yaml", "plugins/scp/skills/run-scp/SKILL.md", "web/src/features/guide/view.tsx"} {
		text := read(p)
		for _, n := range prereq.Needs {
			for _, e := range n.Events {
				if name := prereq.EventName(e); !strings.Contains(text, name) {
					t.Errorf("%s does not name the webhook event %s (%s)", p, name, n.Title)
				}
			}
		}
	}
}
