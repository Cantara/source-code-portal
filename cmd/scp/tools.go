package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
	"github.com/cantara/source-code-portal/internal/tools"
)

// The handlers the tools read through: the contract's methods and the llms Markdown pages, called in process with the request's
// context, so a tool sees exactly what the viewer may see.
type toolReads struct {
	catalog          catalogAPI
	commits          commitsAPI
	repo, doc, group http.Handler
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
	return tools.New(
		tool("search_repositories", "Search repositories",
			"Lists the catalogued repositories, optionally of one group and matching words in the name, description, topics or language; newest activity first.",
			schema(nil, map[string]any{"query": prop("string", "Words to match; empty lists all"), "group": prop("string", "A group id"), "limit": limit}),
			func(in map[string]any) string {
				if q := arg(in, "query"); q != "" {
					return "Searching for " + q
				}
				return "Listing repositories"
			}, r.search),
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
		repoTool("repository_health", "Repository health", "Reads a repository's health score, level and findings, its latest release, packages and activity.", "Checking the health of", r.health),
		repoTool("repository_packages", "Repository packages", "Lists what a repository publishes to package registries (Maven, npm, PyPI, crates.io, Go, NuGet, ...) with the newest release of each.", "Reading the packages of", r.packages),
	)
}

var errNotFound = errors.New("not found in the catalogue")

func day(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02")
}

func repoLine(b *strings.Builder, p oapi.Repo) {
	fmt.Fprintf(b, "- [%s/%s](%s)", p.Owner, p.Name, snapshot.RepoPage(snapshot.RepoKey(p.Address)))
	if p.Description != "" {
		b.WriteString(": " + p.Description)
	}
	var facts []string
	if p.Language != "" {
		facts = append(facts, p.Language)
	}
	facts = append(facts, "last commit "+day(p.LastCommitAt))
	if p.Health != nil {
		facts = append(facts, fmt.Sprintf("health %d (%s)", p.Health.Score, p.Health.Level))
	}
	if p.Archived {
		facts = append(facts, "archived")
	}
	if p.Visibility != "public" {
		facts = append(facts, p.Visibility)
	}
	fmt.Fprintf(b, " (%s)\n", strings.Join(facts, ", "))
}

func (r toolReads) search(ctx context.Context, in map[string]any) (tools.Result, error) {
	var group *string
	if g := arg(in, "group"); g != "" {
		group = &g
	}
	res, err := r.catalog.ListRepos(ctx, oapi.ListReposRequestObject{Params: oapi.ListReposParams{Group: group}})
	if err != nil {
		return tools.Result{}, err
	}
	ok, isOK := res.(oapi.ListRepos200JSONResponse)
	if !isOK {
		return tools.Result{}, fmt.Errorf("the group %q is %w", arg(in, "group"), errNotFound)
	}
	words := strings.Fields(strings.ToLower(arg(in, "query")))
	repos := slices.DeleteFunc(ok.Body.Repos, func(p oapi.Repo) bool {
		hay := strings.ToLower(strings.Join(append([]string{p.Address, p.Description, p.Language}, p.Topics...), " "))
		for _, w := range words {
			if !strings.Contains(hay, w) {
				return true
			}
		}
		return false
	})
	slices.SortStableFunc(repos, func(a, b oapi.Repo) int { return b.PushedAt.Compare(a.PushedAt) })
	limit := intArg(in, "limit", 20, 50)
	var b strings.Builder
	fmt.Fprintf(&b, "%d repositories match", len(repos))
	if len(repos) > limit {
		fmt.Fprintf(&b, "; the %d with the newest activity", limit)
		repos = repos[:limit]
	}
	b.WriteString(".\n\n")
	for _, p := range repos {
		repoLine(&b, p)
	}
	return tools.Result{Source: "/repos", Text: b.String()}, nil
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
	return oapi.RepoDetail{}, fmt.Errorf("the repository %s/%s is %w; search_repositories finds names", arg(in, "owner"), arg(in, "name"), errNotFound)
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
	fmt.Fprintf(&b, "Open issues and pull requests: %d. License: %s.\n", p.OpenIssues, orUnknown(p.License))
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
