// Package prereq is what each feature needs from GitHub (read permissions, webhook events) and the check of it against what a
// source's credential is granted (design/prerequisites.md, P-71). It is core: the table is the one the code, the status and the
// documentation follow.
package prereq

import (
	"slices"
	"strings"

	"github.com/cantara/source-code-portal/internal/github"
)

// Feature is a part of the portal that needs something of GitHub.
type Feature string

const (
	Catalogue Feature = "catalogue" // repositories, README and documents, manifests, commits, releases, packages
	Pulls     Feature = "pulls"     // open pull requests
	Issues    Feature = "issues"    // open issues
)

// Need is what a feature needs: GitHub's names of its read permissions and webhook events, the jobs whose 403 tells that a token
// lacks them, and what the reader sees without them.
type Need struct {
	Feature     Feature
	Title       string
	Permissions []string // GitHub's names: contents, metadata, issues, pull_requests, checks, statuses
	Events      []string // GitHub's names: push, release, repository, issues, issue_comment, pull_request, ...
	Jobs        []string // job kinds whose items fail with 403 when a token lacks the permissions
	Effect      string
}

// Needs is every feature's need, in the order they are told.
var Needs = []Need{ //nolint:gochecknoglobals // the one table of prerequisites
	{Feature: Catalogue, Title: "The catalogue", Permissions: []string{"contents", "metadata"}, Events: []string{"push", "release", "repository"},
		Jobs: []string{"commits", "docs", "manifests"}, Effect: "Repositories, documents, manifests and commits are not read."},
	{Feature: Pulls, Title: "Pull requests", Permissions: []string{"pull_requests", "checks", "statuses"},
		Events: []string{"pull_request", "pull_request_review"}, Jobs: []string{"pulls"},
		Effect: "Open pull requests are not read: /pulls and the pull request panels stay empty."},
	{Feature: Issues, Title: "Issues", Permissions: []string{"issues"}, Events: []string{"issues"}, Jobs: []string{"issues"},
		Effect: "Open issues are not read: /issues and the issue panels stay empty."},
}

// Of is a feature's need.
func Of(f Feature) (Need, bool) {
	i := slices.IndexFunc(Needs, func(n Need) bool { return n.Feature == f })
	if i < 0 {
		return Need{}, false
	}
	return Needs[i], true
}

// permissionNames and eventNames are GitHub's settings page words for its API names.
var (
	permissionNames = map[string]string{"contents": "Contents", "metadata": "Metadata", "issues": "Issues", //nolint:gochecknoglobals // a constant table
		"pull_requests": "Pull requests", "checks": "Checks", "statuses": "Commit statuses"}
	eventNames = map[string]string{"push": "Push", "release": "Release", "repository": "Repository", "issues": "Issues", //nolint:gochecknoglobals // a constant table
		"issue_comment": "Issue comment", "pull_request": "Pull request", "pull_request_review": "Pull request review",
		"check_suite": "Check suite", "status": "Status"}
)

// PermissionName is a permission as GitHub's App settings page names it ("Pull requests" for pull_requests).
func PermissionName(p string) string { return nameOf(permissionNames, p) }

// EventName is a webhook event as GitHub's App settings page names it ("Issue comment" for issue_comment).
func EventName(e string) string { return nameOf(eventNames, e) }

func nameOf(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return k
}

// Source is what the check knows of one configured source: its grants when it is an App that told them, and for a token the jobs
// whose items GitHub refused (403) for most repositories.
type Source struct {
	ID      string
	App     bool
	Grants  []github.Grant // App mode, from the last installation listing
	Known   bool           // the grants were read
	Refused map[string]bool
}

// Unmet is a feature whose prerequisite does not hold in a source: the accounts, the missing permissions (GitHub's names) and
// events, or, for a token, that GitHub refused its reads.
type Unmet struct {
	Feature     Feature
	Title       string
	Effect      string
	Source      string
	Owners      []string
	Permissions []string
	Events      []string
	Refused     bool // a token GitHub refused; its permissions cannot be asked
}

// Fix is what to do about u, in the words of GitHub's settings pages.
func (u Unmet) Fix() string {
	if u.Refused {
		return "GitHub refuses the token's reads for " + strings.ToLower(u.Title) + ": give it read access to " + names(u.Permissions, PermissionName) +
			", or use a GitHub App."
	}
	var b strings.Builder
	b.WriteString("In the GitHub App's settings, Permissions & events: ")
	if len(u.Permissions) > 0 {
		b.WriteString("set " + names(u.Permissions, PermissionName) + " to Read-only")
	}
	if len(u.Events) > 0 {
		if len(u.Permissions) > 0 {
			b.WriteString(", and ")
		}
		b.WriteString("subscribe to " + names(u.Events, EventName))
	}
	b.WriteString("; then accept the new permissions on the installation of " + strings.Join(u.Owners, ", ") + ".")
	return b.String()
}

func names(xs []string, name func(string) string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = name(x)
	}
	switch len(out) {
	case 0:
		return ""
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

// Check is the unmet prerequisites of needs (Needs, less the features switched off) across sources; events count only when
// webhooks reach the portal. An App's installations that lack the same things are told together.
func Check(needs []Need, sources []Source, webhooks bool) []Unmet {
	var out []Unmet
	for _, n := range needs {
		for _, s := range sources {
			if s.App {
				out = append(out, checkApp(n, s, webhooks)...)
				continue
			}
			if slices.ContainsFunc(n.Jobs, func(j string) bool { return s.Refused[j] }) {
				out = append(out, Unmet{Feature: n.Feature, Title: n.Title, Effect: n.Effect, Source: s.ID, Permissions: n.Permissions, Refused: true})
			}
		}
	}
	return out
}

func checkApp(n Need, s Source, webhooks bool) []Unmet {
	if !s.Known {
		return nil
	}
	byGap := map[string]*Unmet{}
	var order []string
	for _, g := range s.Grants {
		var perms, events []string
		for _, p := range n.Permissions {
			if v := g.Permissions[p]; v != "read" && v != "write" {
				perms = append(perms, p)
			}
		}
		if webhooks {
			for _, e := range n.Events {
				if !slices.Contains(g.Events, e) {
					events = append(events, e)
				}
			}
		}
		if len(perms) == 0 && len(events) == 0 {
			continue
		}
		key := strings.Join(perms, ",") + "|" + strings.Join(events, ",")
		u := byGap[key]
		if u == nil {
			u = &Unmet{Feature: n.Feature, Title: n.Title, Effect: n.Effect, Source: s.ID, Permissions: perms, Events: events}
			if len(perms) == 0 {
				u.Effect = n.Title + " update only on their schedule, not within seconds: GitHub sends no webhook for them."
			}
			byGap[key] = u
			order = append(order, key)
		}
		u.Owners = append(u.Owners, g.Owner)
	}
	out := make([]Unmet, 0, len(order))
	for _, k := range order {
		out = append(out, *byGap[k])
	}
	return out
}
