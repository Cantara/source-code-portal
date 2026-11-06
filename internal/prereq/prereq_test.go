package prereq

import (
	"slices"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/github"
)

func TestCheck(t *testing.T) {
	all := map[string]string{"contents": "read", "metadata": "read", "issues": "read", "pull_requests": "read", "checks": "read", "statuses": "read"}
	events := []string{"push", "release", "repository", "issues", "pull_request", "pull_request_review"}
	if got := Check(Needs, []Source{{ID: "gh", App: true, Known: true, Grants: []github.Grant{{Owner: "acme", Permissions: all, Events: events}}}}, true); len(got) != 0 {
		t.Errorf("every prerequisite holds: %+v", got)
	}

	// Cantara on 2026-11-05: Contents and Metadata only, events push, release and repository.
	narrow := github.Grant{Owner: "cantara", Permissions: map[string]string{"contents": "read", "metadata": "read"}, Events: []string{"push", "release", "repository"}}
	other := github.Grant{Owner: "acme", Permissions: narrow.Permissions, Events: narrow.Events}
	got := Check(Needs, []Source{{ID: "gh", App: true, Known: true, Grants: []github.Grant{narrow, other}}}, true)
	if len(got) != 2 || got[0].Feature != Pulls || got[1].Feature != Issues {
		t.Fatalf("unmet: %+v", got)
	}
	if u := got[1]; !slices.Equal(u.Permissions, []string{"issues"}) || !slices.Equal(u.Events, []string{"issues"}) ||
		!slices.Equal(u.Owners, []string{"cantara", "acme"}) || u.Refused {
		t.Errorf("issues: %+v", u)
	}
	if fix := got[1].Fix(); fix != "In the GitHub App's settings, Permissions & events: set Issues to Read-only, and subscribe to "+
		"Issues; then accept the new permissions on the installation of cantara, acme." {
		t.Errorf("fix: %s", fix)
	}
	if !strings.Contains(got[0].Fix(), "Pull requests, Checks and Commit statuses to Read-only") {
		t.Errorf("pulls fix: %s", got[0].Fix())
	}

	// Without webhooks the events do not matter; the grants not read yet say nothing.
	if got := Check(Needs, []Source{{ID: "gh", App: true, Known: true, Grants: []github.Grant{narrow}}}, false); len(got) != 2 || len(got[0].Events) != 0 {
		t.Errorf("without webhooks: %+v", got)
	}
	if got := Check(Needs, []Source{{ID: "gh", App: true}}, true); got != nil {
		t.Errorf("grants unknown: %+v", got)
	}

	// Permissions granted, an event missing: told as updates on the schedule only.
	late := github.Grant{Owner: "acme", Permissions: all, Events: []string{"push", "release", "repository", "pull_request", "pull_request_review"}}
	if got := Check(Needs, []Source{{ID: "gh", App: true, Known: true, Grants: []github.Grant{late}}}, true); len(got) != 1 || len(got[0].Permissions) != 0 ||
		!strings.Contains(got[0].Effect, "on their schedule") || !strings.Contains(got[0].Fix(), "subscribe to Issues") {
		t.Errorf("events only: %+v", got)
	}

	// A feature switched off is not checked.
	if got := Check(Needs[:1], []Source{{ID: "gh", App: true, Known: true, Grants: []github.Grant{narrow}}}, true); len(got) != 0 {
		t.Errorf("switched off: %+v", got)
	}

	// A token: told by the jobs GitHub refused.
	got = Check(Needs, []Source{{ID: "tok", Refused: map[string]bool{"issues": true}}}, true)
	if len(got) != 1 || got[0].Feature != Issues || !got[0].Refused || !strings.Contains(got[0].Fix(), "read access to Issues") {
		t.Errorf("token: %+v", got)
	}
}
