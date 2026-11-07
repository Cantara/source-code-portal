package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/tools"
)

// The handlers the tools read through: the contract's methods and the llms Markdown pages, called in process with the request's
// context, so a tool sees exactly what the viewer may see.
type toolReads struct {
	find             searchAPI
	catalog          catalogAPI
	commits          commitsAPI
	pulls            pullsAPI
	issues           issuesAPI
	releases         releasesAPI
	insights         insightsAPI
	repo, doc, group http.Handler
	features         config.Features // a switched-off feature has no tool and no search kind (design/features.md)
}

func schema(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

func ownerProp() map[string]any {
	return prop("string", "The repository's owner (organisation or user)")
}

func nameProp() map[string]any { return prop("string", "The repository's name") }

func arg(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return strings.TrimSpace(s)
}

func intArg(in map[string]any, key string, def, maxN int) int {
	f, ok := in[key].(float64)
	if !ok || f < 1 {
		return def
	}
	return min(int(f), maxN)
}

// catalogue is the tools agents call (design/agents.md): the assistant, MCP and WebMCP.
func (r toolReads) catalogue() (*tools.Catalogue, error) {
	limit := map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "description": "At most this many, up to 50 (default 20)"}
	work := map[string]any{"type": "array", "description": "The work that counts: hand (people without an AI), ai (with an AI co-author or a coding agent), automation (bots and sweeps); default every kind but automation",
		"items": map[string]any{"type": "string", "enum": []string{"hand", "ai", "automation"}}}
	repo := func(label string, run func(context.Context, map[string]any) (tools.Result, error)) (map[string]any, func(map[string]any) string, func(context.Context, map[string]any) (tools.Result, error)) {
		return schema([]string{"owner", "name"}, map[string]any{"owner": ownerProp(), "name": nameProp()}),
			func(in map[string]any) string { return label + " " + arg(in, "owner") + "/" + arg(in, "name") }, run
	}
	tool := func(name, title, description string, s map[string]any, label func(map[string]any) string, run func(context.Context, map[string]any) (tools.Result, error)) tools.Tool {
		return tools.Tool{Name: name, Title: title, Description: description, Schema: s, Label: label, Run: run}
	}
	repoTool := func(name, title, description, label string, run func(context.Context, map[string]any) (tools.Result, error)) tools.Tool {
		s, l, ru := repo(label, run)
		return tool(name, title, description, s, l, ru)
	}
	holds, kinds := "published packages, default-branch commits and releases", []string{"repo", "group", "doc", "dep", "package", "commit", "release"}
	switch {
	case r.features.Pulls && r.features.Issues:
		holds, kinds = "published packages, default-branch commits, releases, open pull requests and open issues", append(kinds, "pull", "issue")
	case r.features.Pulls:
		holds, kinds = "published packages, default-branch commits, releases and open pull requests", append(kinds, "pull")
	case r.features.Issues:
		holds, kinds = "published packages, default-branch commits, releases and open issues", append(kinds, "issue")
	}
	all := []tools.Tool{
		tool("search", "Search the portal",
			"Searches what the portal holds: repositories, groups, document sections (README and docs/), manifests and their dependencies, "+
				holds+"; not source code. The query is words (all required), \"phrases\", "+
				"-exclusions, OR between two words, and filters: in:"+strings.Join(kinds, "|")+", group:, repo:, lang:, topic:, "+
				"lifecycle:, author:, kind:hand|ai|automation, dep: (a dependency's name), before: and after: (dates, 2026-11-05). Best first.",
			schema([]string{"query"}, map[string]any{"query": prop("string", "The query, up to 256 characters"),
				"in": map[string]any{"type": "string", "enum": kinds,
					"description": "Only hits of this kind; the counts stay every kind's"}, "limit": limit}),
			func(in map[string]any) string { return "Searching for " + arg(in, "query") }, r.search),
		tool("list_groups", "List groups", "Lists the groups of repositories with their ids, names and sizes.", schema(nil, map[string]any{}),
			func(map[string]any) string { return "Listing groups" }, r.listGroups),
		tool("get_group", "Read a group", "Reads a group: its description, health and repositories.",
			schema([]string{"group"}, map[string]any{"group": prop("string", "The group id")}),
			func(in map[string]any) string { return "Reading the group " + arg(in, "group") }, r.getGroup),
		repoTool("get_repository", "Read a repository", "Reads a repository's README, its technology and its internal dependencies.", "Reading", r.getRepository),
		tool("get_document", "Read a document", "Reads one document of a repository's docs/ by its path, as get_repository and get_group link it.",
			schema([]string{"owner", "name", "path"}, map[string]any{"owner": ownerProp(), "name": nameProp(), "path": prop("string", "The path, such as docs/setup.md")}),
			func(in map[string]any) string {
				return "Reading " + arg(in, "path") + " of " + arg(in, "owner") + "/" + arg(in, "name")
			}, r.getDocument),
		tool("recent_commits", "Recent commits", "Lists the newest default-branch commits, of everything, a group or one repository; without automation unless work names it.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name"), "work": work, "limit": limit}),
			func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the commits of " + s
				}
				return "Reading the latest commits"
			}, r.recentCommits),
		tool("open_pull_requests", "Open pull requests",
			"Lists the open pull requests whose turn it is first, of everything, a group or one repository: each its state (needs review, "+
				"ready to merge, changes requested, failing checks, conflicts, checks running, draft) and whom it waits for, who opened it "+
				"(a person or automation such as Dependabot, Renovate or Snyk), when it was opened and last updated, and the counts by state.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name"),
				"state": map[string]any{"type": "string", "enum": []string{"review", "ready", "changes", "failing", "conflicts", "checks", "draft"},
					"description": "Only pull requests in this state"},
				"by":    map[string]any{"type": "string", "enum": []string{"people", "automation"}, "description": "Only people's or automation's; default both"},
				"limit": limit}),
			func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the pull requests of " + s
				}
				return "Reading the open pull requests"
			}, r.openPulls),
		tool("open_issues", "Open issues",
			"Lists the open issues (pull requests left out) that wait longest first, of everything, a group or one repository: each where it "+
				"stands (needs triage: no label, milestone or type; unassigned: triaged, nobody took it; assigned), its kind (bug, feature, "+
				"question, other), who opened it (a person or automation such as Renovate's dependency dashboard), who has it, its labels, "+
				"when it was opened and last updated, and the counts by state and kind.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name"),
				"state": map[string]any{"type": "string", "enum": []string{"triage", "unassigned", "assigned"}, "description": "Only issues where they stand so"},
				"kind":  map[string]any{"type": "string", "enum": []string{"bug", "feature", "question", "other"}, "description": "Only issues of this kind"},
				"by":    map[string]any{"type": "string", "enum": []string{"people", "automation"}, "description": "Only people's or automation's; default both"},
				"limit": limit}),
			func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the issues of " + s
				}
				return "Reading the open issues"
			}, r.openIssues),
		tool("recent_releases", "Recent releases",
			"Lists the recent releases (the 20 newest GitHub releases of each repository are held, and the newest release of each package a "+
				"repository publishes to a registry: Maven, npm, PyPI and so on) of everything, a group or one repository, newest first: "+
				"by default the latest release of each repository; variant all lists every release held, stable only the stable ones, "+
				"pre only the pre-releases; source limits it to GitHub's or to the packages'. Each has its source, tag or version, title, whether "+
				"it is a pre-release, when it was published and by whom (a person or automation), and the counts by variant and by source.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name"),
				"variant": map[string]any{"type": "string", "enum": []string{"latest", "all", "stable", "pre"}, "description": "Which releases; default latest"},
				"source":  map[string]any{"type": "string", "enum": []string{"all", "github", "packages"}, "description": "GitHub releases, published packages or both; default both"},
				"since":   prop("string", "Only releases published since this RFC 3339 time or number of days such as 30d"),
				"sort":    map[string]any{"type": "string", "enum": []string{"newest", "oldest", "repo", "group"}, "description": "The order; default newest"},
				"limit":   limit}),
			func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the releases of " + s
				}
				return "Reading the recent releases"
			}, r.recentReleases),
		tool("retired_repositories", "Retired repositories",
			"Lists the retired repositories (Archive): archived on GitHub, or marked deprecated in scp.yaml, last active first. Each has why it is "+
				"retired, when it was last active, how many authors its recent commits had (counted, never named) and how many repositories "+
				"still depend on it; used limits the list to those another repository still depends on.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "q": prop("string", "Part of the repository's name"),
				"reason": map[string]any{"type": "string", "enum": []string{"archived", "deprecated"}, "description": "Only retired for this reason"},
				"used":   map[string]any{"type": "boolean", "description": "Only those another repository still depends on"},
				"sort":   map[string]any{"type": "string", "enum": []string{"active", "name", "group", "used"}, "description": "The order; default last active first"},
				"limit":  limit}),
			func(map[string]any) string { return "Reading the retired repositories" }, r.retired),
		tool("fleet_insights", "Fleet insights",
			"Reads what the portal's held data says about the fleet, or a group or one repository: how often it ships (stable releases per "+
				"month, the median days between a product's last releases), the technology its manifests declare, the internal libraries most "+
				"repositories depend on, the repositories where one person wrote nearly all of the people's recent commits (named by "+
				"repository, never by person) and the repositories by how long ago they changed, with the dormant ones listed.",
			schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name")}),
			func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the insights of " + s
				}
				return "Reading the fleet insights"
			}, r.fleetInsights),
		repoTool("repository_health", "Repository health", "Reads a repository's health score, level and findings, its latest release, packages and activity.", "Checking the health of", r.health),
		repoTool("repository_packages", "Repository packages", "Lists what a repository publishes to package registries (Maven, npm, PyPI, crates.io, Go, NuGet, ...) with the newest release of each.", "Reading the packages of", r.packages),
	}
	return tools.New(slices.DeleteFunc(all, func(t tools.Tool) bool {
		return t.Name == "open_pull_requests" && !r.features.Pulls || t.Name == "open_issues" && !r.features.Issues
	})...)
}

var errNotFound = errors.New("not found in the catalogue")

// openIssues lists open issues what waits first (design/issues.md) with the counts by state and kind.
func (r toolReads) openIssues(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit := intArg(in, "limit", 20, 50)
	params := oapi.ListIssuesParams{Limit: &limit}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	if st := arg(in, "state"); st != "" {
		v := oapi.IssueState(st)
		params.State = &v
	}
	if k := arg(in, "kind"); k != "" {
		v := oapi.IssueKind(k)
		params.Kind = &v
	}
	if by := arg(in, "by"); by != "" {
		v := oapi.ListIssuesParamsBy(by)
		params.By = &v
	}
	res, err := r.issues.ListIssues(ctx, oapi.ListIssuesRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListIssues200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group or repository is %w", errNotFound)
	}
	c := ok.Body.Counts
	var b strings.Builder
	fmt.Fprintf(&b, "%d open (%d by people, %d by automation): %d need triage, %d unassigned, %d assigned; %d bugs, %d features, "+
		"%d questions, %d other.\n\n", c.Open, c.People, c.Automation, c.States.Triage, c.States.Unassigned, c.States.Assigned,
		c.Kinds.Bug, c.Kinds.Feature, c.Kinds.Question, c.Kinds.Other)
	for _, i := range ok.Body.Issues {
		who := i.Author
		if i.Bot {
			who += " (automation)"
		}
		fmt.Fprintf(&b, "- [%s #%d](%s) %s, %s: %s, by %s", i.Repo, i.Number, i.Url, i.State, i.Kind, i.Title, who)
		if len(i.Assignees) > 0 {
			fmt.Fprintf(&b, ", for %s", strings.Join(i.Assignees, ", "))
		}
		fmt.Fprintf(&b, ", opened %s, updated %s\n", i.CreatedAt.UTC().Format("2006-01-02"), i.UpdatedAt.UTC().Format("2006-01-02"))
	}
	if len(ok.Body.Issues) == 0 {
		b.WriteString("No open issues.\n")
	}
	return tools.Result{Source: "/issues", Text: b.String()}, nil
}

// openPulls lists open pull requests whose turn first (design/pulls.md) with the counts by state.
func (r toolReads) openPulls(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit := intArg(in, "limit", 20, 50)
	params := oapi.ListPullsParams{Limit: &limit}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	if st := arg(in, "state"); st != "" {
		v := oapi.PullState(st)
		params.State = &v
	}
	if by := arg(in, "by"); by != "" {
		v := oapi.ListPullsParamsBy(by)
		params.By = &v
	}
	res, err := r.pulls.ListPulls(ctx, oapi.ListPullsRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListPulls200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group or repository is %w", errNotFound)
	}
	c := ok.Body.Counts
	var b strings.Builder
	fmt.Fprintf(&b, "%d open (%d by people, %d by automation): %d need review, %d ready to merge, %d changes requested, %d failing checks, "+
		"%d with conflicts, %d checks running, %d drafts.\n\n", c.Open, c.People, c.Automation, c.States.Review, c.States.Ready,
		c.States.Changes, c.States.Failing, c.States.Conflicts, c.States.Checks, c.States.Draft)
	for _, p := range ok.Body.Pulls {
		who := p.Author
		if p.Bot {
			who += " (automation)"
		}
		fmt.Fprintf(&b, "- [%s #%d](%s) %s, waits for %s: %s, by %s, opened %s, updated %s\n", p.Repo, p.Number, p.Url, p.State,
			p.WaitsFor, p.Title, who, p.CreatedAt.UTC().Format("2006-01-02"), p.UpdatedAt.UTC().Format("2006-01-02"))
	}
	if len(ok.Body.Pulls) == 0 {
		b.WriteString("No open pull requests.\n")
	}
	return tools.Result{Source: "/pulls", Text: b.String()}, nil
}

// recentReleases lists the recent releases (design/releases.md) with the counts by variant.
func (r toolReads) recentReleases(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit := intArg(in, "limit", 20, 50)
	params := oapi.ListReleasesParams{Limit: &limit}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	if v := arg(in, "variant"); v != "" {
		x := oapi.ReleaseVariant(v)
		params.Variant = &x
	}
	if v := arg(in, "source"); v != "" {
		x := oapi.ReleaseSource(v)
		params.Source = &x
	}
	if v := arg(in, "since"); v != "" {
		params.Since = &v
	}
	if v := arg(in, "sort"); v != "" {
		x := oapi.ReleaseSort(v)
		params.Sort = &x
	}
	res, err := r.releases.ListReleases(ctx, oapi.ListReleasesRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListReleases200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group, the repository, the variant or the time is not valid, or is %w", errNotFound)
	}
	c := ok.Body.Counts
	var b strings.Builder
	fmt.Fprintf(&b, "%d releases held (%d stable, %d pre-releases); the latest of %d repositories; %d of them GitHub's, %d packages'.\n\n",
		c.All, c.Stable, c.Pre, c.Latest, c.Github, c.Packages)
	for _, e := range ok.Body.Releases {
		fmt.Fprintf(&b, "- [%s %s](%s)", e.Repo, e.Tag, e.Url)
		if e.Package != "" {
			fmt.Fprintf(&b, " package %s on %s", e.Package, e.Source)
		}
		if e.Prerelease {
			b.WriteString(" pre-release")
		}
		if e.Title != "" && e.Title != e.Tag {
			fmt.Fprintf(&b, ": %s", e.Title)
		}
		fmt.Fprintf(&b, ", published %s", e.PublishedAt.UTC().Format("2006-01-02"))
		if e.Author != "" {
			b.WriteString(" by " + e.Author)
			if e.Bot {
				b.WriteString(" (automation)")
			}
		}
		b.WriteString("\n")
	}
	if len(ok.Body.Releases) == 0 {
		b.WriteString("No releases.\n")
	}
	return tools.Result{Source: "/releases", Text: b.String()}, nil
}

// retired lists the retired repositories (design/archive.md) with the counts by reason.
func (r toolReads) retired(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit := intArg(in, "limit", 20, 50)
	params := oapi.ListArchiveParams{Limit: &limit}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if q := arg(in, "q"); q != "" {
		params.Q = &q
	}
	if v := arg(in, "reason"); v != "" {
		x := oapi.ArchiveReason(v)
		params.Reason = &x
	}
	if used, _ := in["used"].(bool); used {
		params.Used = &used
	}
	if v := arg(in, "sort"); v != "" {
		x := oapi.ArchiveSort(v)
		params.Sort = &x
	}
	res, err := r.catalog.ListArchive(ctx, oapi.ListArchiveRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListArchive200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group, the reason or the sort is not valid, or is %w", errNotFound)
	}
	c := ok.Body.Counts
	var b strings.Builder
	fmt.Fprintf(&b, "%d retired (%d archived, %d deprecated); %d still depended on.\n\n", c.Total, c.Archived, c.Deprecated, c.Used)
	for _, a := range ok.Body.Archive {
		fmt.Fprintf(&b, "- [%s](%s) %s, last active %s, %d authors", a.Repo.Address, snapshot.RepoPage(snapshot.RepoKey(a.Repo.Address)), a.Reason,
			a.LastActive.UTC().Format("2006-01-02"), a.Authors)
		if a.UsedBy > 0 {
			fmt.Fprintf(&b, ", still used by %d", a.UsedBy)
		}
		b.WriteString("\n")
	}
	if len(ok.Body.Archive) == 0 {
		b.WriteString("No retired repositories.\n")
	}
	return tools.Result{Source: "/archive", Text: b.String()}, nil
}

// fleetInsights tells the five insights (design/insights.md) in words and lists.
func (r toolReads) fleetInsights(ctx context.Context, in map[string]any) (tools.Result, error) {
	params := oapi.GetInsightsParams{}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	res, err := r.insights.GetInsights(ctx, oapi.GetInsightsRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.GetInsights200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group or repository is %w", errNotFound)
	}
	i, th := ok.Body, ok.Body.Thresholds
	var b strings.Builder
	releases := 0
	for _, m := range i.Cadence.Months {
		releases += m.Releases
	}
	fmt.Fprintf(&b, "%d repositories (retired ones left out).\n\n## Cadence\n%d stable releases in the last %d months.\n", i.Repos, releases, th.Months)
	for _, p := range i.Cadence.Products {
		if p.MedianDays != nil {
			fmt.Fprintf(&b, "- %s: %d stable releases held, a median of %d days between the last %d\n", p.Name, p.Releases, *p.MedianDays, th.Releases)
		} else {
			fmt.Fprintf(&b, "- %s: %d stable releases held\n", p.Name, p.Releases)
		}
	}
	b.WriteString("\n## Technology\n")
	for _, l := range i.Technology.Languages {
		fmt.Fprintf(&b, "- %s: %d repositories (%d%%)\n", l.Name, l.Repos, int(l.Share*100+0.5))
	}
	for _, rt := range i.Technology.Runtimes {
		fmt.Fprintf(&b, "- %s: %d repositories, from %s to %s\n", rt.Name, rt.Repos, rt.Oldest, rt.Newest)
	}
	fmt.Fprintf(&b, "- %d repositories name no language\n\n## Shared libraries\n", i.Technology.Unknown)
	for _, l := range i.Libraries {
		fmt.Fprintf(&b, "- %s: %d dependants", l.Repo, l.Dependants)
		if l.Retired != nil {
			fmt.Fprintf(&b, ", %s and still used", *l.Retired)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n## Mostly one person's (%d%% or more of %d people's commits or more; the person is never named)\n", th.SingleAuthorShare, th.SingleAuthorCommits)
	for _, s := range i.SingleAuthor {
		fmt.Fprintf(&b, "- %s\n", s.Repo)
	}
	a := i.Ageing
	fmt.Fprintf(&b, "\n## Ageing\n%d active, %d recent, %d quiet, %d dormant.\n", a.Active, a.Recent, a.Quiet, a.Dormant)
	for _, d := range a.DormantRepos {
		fmt.Fprintf(&b, "- %s, last changed %s\n", d.Repo, day(d.LastChange))
	}
	return tools.Result{Source: "/insights", Text: b.String()}, nil
}

func day(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02")
}

// search runs the portal's search (design/search.md) and lists the best hits with their snippets.
func (r toolReads) search(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit := intArg(in, "limit", 20, 50)
	params := oapi.SearchParams{Q: arg(in, "query"), Limit: &limit}
	if k := arg(in, "in"); k != "" {
		kind := oapi.SearchKind(k)
		params.In = &kind
	}
	res, err := r.find.Search(ctx, oapi.SearchRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.Search200JSONResponse)
	if !isOK {
		if bad, isBad := res.(oapi.Search400ApplicationProblemPlusJSONResponse); isBad && bad.Detail != nil {
			return tools.Result{}, &tools.InputError{Msg: "the query: " + *bad.Detail}
		}
		return tools.Result{}, &tools.InputError{Msg: "the query cannot be searched"}
	}
	sr := ok.Body
	c := sr.Counts
	var b strings.Builder
	fmt.Fprintf(&b, "%d hits for %q: %d repositories, %d groups, %d document sections, %d manifests, %d packages, %d commits", sr.Page.Total,
		sr.Query, c.Repo, c.Group, c.Doc, c.Dep, c.Package, c.Commit)
	if sr.Page.Total > len(sr.Hits) {
		fmt.Fprintf(&b, "; the best %d", len(sr.Hits))
	}
	b.WriteString(".\n")
	for _, fix := range sr.Corrections {
		fmt.Fprintf(&b, "No document holds %q; searched for %q.\n", fix.From, fix.To)
	}
	b.WriteString("\n")
	for _, h := range sr.Hits {
		fmt.Fprintf(&b, "- %s [%s](%s)", h.Kind, plainParts(h.Title), h.Url)
		if h.Repo != nil && h.Kind != oapi.SearchKindRepo {
			b.WriteString(" in " + *h.Repo)
		}
		if h.At != nil {
			b.WriteString(", " + day(h.At))
		}
		if s := plainParts(h.Snippet); s != "" {
			b.WriteString(": " + s)
		}
		b.WriteString("\n")
	}
	return tools.Result{Source: "/search?q=" + url.QueryEscape(sr.Query), Text: b.String()}, nil
}

// plainParts is a title or snippet as text, its matches in bold.
func plainParts(ps []oapi.TextPart) string {
	var b strings.Builder
	for _, p := range ps {
		if p.Mark != nil && *p.Mark {
			b.WriteString("**" + p.Text + "**")
		} else {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func (r toolReads) listGroups(ctx context.Context, _ map[string]any) (tools.Result, error) {
	res, err := r.catalog.ListGroups(ctx, oapi.ListGroupsRequestObject{})
	if err != nil {
		return tools.Result{}, err
	}
	ok, _ := res.(oapi.ListGroups200JSONResponse)
	var b strings.Builder
	for _, g := range ok.Body.Groups {
		fmt.Fprintf(&b, "- [%s](%s), id %s, %d repositories", g.Name, snapshot.GroupPage(g.Id), g.Id, g.RepoCount)
		if g.Description != "" {
			b.WriteString(": " + g.Description)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return tools.Result{Source: "/groups", Text: "There are no groups."}, nil
	}
	return tools.Result{Source: "/groups", Text: b.String()}, nil
}

// page reads one llms Markdown page in process.
func page(ctx context.Context, h http.Handler, target string, values map[string]string) (string, error) {
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("Accept", "text/markdown")
	for k, v := range values {
		req.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return "", errNotFound
	}
	return w.Body.String(), nil
}

func (r toolReads) getGroup(ctx context.Context, in map[string]any) (tools.Result, error) {
	g := arg(in, "group")
	text, err := page(ctx, r.group, snapshot.GroupPage(g), map[string]string{"group": g})
	if err != nil {
		return tools.Result{}, fmt.Errorf("the group %q is %w; list_groups names them", g, err)
	}
	return tools.Result{Source: snapshot.GroupPage(g), Text: text}, nil
}

// resolve finds a repository the viewer may see, following a rename.
func (r toolReads) resolve(ctx context.Context, in map[string]any) (oapi.RepoDetail, error) {
	owner, name := arg(in, "owner"), arg(in, "name")
	for range 2 {
		res, err := r.catalog.GetRepo(ctx, oapi.GetRepoRequestObject{Owner: owner, Name: name})
		if err != nil {
			return oapi.RepoDetail{}, err
		}
		switch v := res.(type) {
		case oapi.GetRepo200JSONResponse:
			return v.Body, nil
		case oapi.GetRepo308Response:
			owner, name, _ = strings.Cut(strings.TrimPrefix(v.Headers.Location, "/api/v1/repos/"), "/")
			continue
		}
		break
	}
	return oapi.RepoDetail{}, fmt.Errorf("the repository %s/%s is %w; search finds them", arg(in, "owner"), arg(in, "name"), errNotFound)
}

func (r toolReads) getRepository(ctx context.Context, in map[string]any) (tools.Result, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return tools.Result{}, err
	}
	owner, name, _ := strings.Cut(d.Repo.Address, "/")
	text, err := page(ctx, r.repo, "/", map[string]string{"owner": owner, "name": name})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Source: snapshot.RepoPage(snapshot.RepoKey(d.Repo.Address)), Text: text}, nil
}

func (r toolReads) getDocument(ctx context.Context, in map[string]any) (tools.Result, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return tools.Result{}, err
	}
	owner, name, _ := strings.Cut(d.Repo.Address, "/")
	p := strings.TrimPrefix(arg(in, "path"), "/")
	text, err := page(ctx, r.doc, "/", map[string]string{"owner": owner, "name": name, "path": p})
	if err != nil {
		return tools.Result{}, fmt.Errorf("the document %s of %s is %w", p, d.Repo.Address, err)
	}
	return tools.Result{Source: snapshot.DocPage(snapshot.RepoKey(d.Repo.Address), p), Text: text}, nil
}

func (r toolReads) recentCommits(ctx context.Context, in map[string]any) (tools.Result, error) {
	limit, no := intArg(in, "limit", 20, 50), false
	params := oapi.ListCommitsParams{Automation: &no, Limit: &limit}
	if kinds, _ := in["work"].([]any); len(kinds) > 0 {
		w := make(oapi.Work, 0, len(kinds))
		for _, k := range kinds {
			s, _ := k.(string)
			w = append(w, oapi.WorkKind(s))
		}
		params.Automation, params.Work = nil, &w
	}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	res, err := r.commits.ListCommits(ctx, oapi.ListCommitsRequestObject{Params: params})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListCommits200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group or repository is %w", errNotFound)
	}
	var b strings.Builder
	for _, c := range ok.Body.Commits {
		fmt.Fprintf(&b, "- %s %s [%s](%s) %s: %s\n", c.CommittedAt.UTC().Format("2006-01-02 15:04"), c.Sha[:min(7, len(c.Sha))], c.Repo,
			snapshot.RepoPage(snapshot.RepoKey(c.Repo)), c.AuthorName, c.Subject)
	}
	if b.Len() == 0 {
		return tools.Result{Source: "/commits", Text: "No commits."}, nil
	}
	return tools.Result{Source: "/commits", Text: b.String()}, nil
}

func (r toolReads) health(ctx context.Context, in map[string]any) (tools.Result, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return tools.Result{}, err
	}
	p := d.Repo
	var b strings.Builder
	fmt.Fprintf(&b, "# %s/%s\n\n", p.Owner, p.Name)
	if p.Health == nil {
		b.WriteString("No health score: the repository is archived or deprecated.\n")
	} else {
		fmt.Fprintf(&b, "Health %d of 100, level %s.\n", p.Health.Score, p.Health.Level)
		for _, f := range p.Health.Findings {
			fmt.Fprintf(&b, "- %s (-%d)\n", f.Message, f.Penalty)
		}
	}
	fmt.Fprintf(&b, "\nLast commit %s, last by a person %s, pushed %s.\n", day(p.LastCommitAt), day(p.LastAuthoredAt), p.PushedAt.UTC().Format("2006-01-02"))
	if p.Release != nil {
		fmt.Fprintf(&b, "Latest release %s.\n", p.Release.Tag)
	}
	for _, pk := range p.Packages {
		fmt.Fprintf(&b, "Publishes %s %s, newest %s at %s.\n", pk.Ecosystem, pk.Name, pk.Version, pk.Registry)
	}
	switch {
	case p.OpenIssuesWithPulls:
		fmt.Fprintf(&b, "Open issues and pull requests: %d.", p.OpenIssues)
	case p.OpenPulls != nil:
		fmt.Fprintf(&b, "Open issues: %d. Open pull requests: %d.", p.OpenIssues, *p.OpenPulls)
	default:
		fmt.Fprintf(&b, "Open issues: %d.", p.OpenIssues)
	}
	fmt.Fprintf(&b, " License: %s.\n", orUnknown(p.License))
	return tools.Result{Source: snapshot.RepoPage(snapshot.RepoKey(p.Address)), Text: b.String()}, nil
}

func (r toolReads) packages(ctx context.Context, in map[string]any) (tools.Result, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return tools.Result{}, err
	}
	p := d.Repo
	var b strings.Builder
	fmt.Fprintf(&b, "# Packages of %s/%s\n\n", p.Owner, p.Name)
	if len(p.Packages) == 0 {
		b.WriteString("It publishes no package the portal found in its build files.\n")
	}
	for _, pk := range p.Packages {
		fmt.Fprintf(&b, "- %s %s: newest %s at %s", pk.Ecosystem, pk.Name, pk.Version, pk.Registry)
		if pk.PublishedAt != nil {
			fmt.Fprintf(&b, ", published %s", day(pk.PublishedAt))
		}
		if pk.Url != "" {
			fmt.Fprintf(&b, " (%s)", pk.Url)
		}
		b.WriteString("\n")
	}
	if p.Release != nil {
		fmt.Fprintf(&b, "\nThe latest GitHub release is %s.\n", p.Release.Tag)
	}
	return tools.Result{Source: snapshot.RepoPage(snapshot.RepoKey(p.Address)), Text: b.String()}, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
