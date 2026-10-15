package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/assistant"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/llm"
)

// The handlers the assistant's tools read through: the contract's methods and the llms Markdown pages, called in process with the
// request's context, so a tool sees exactly what the viewer may see.
type assistantReads struct {
	catalog          catalogAPI
	commits          commitsAPI
	repo, doc, group http.Handler
}

// newAssistant wires the assistant from the config; without a provider it is off and answers only its status.
func newAssistant(cfg config.Config, reads assistantReads, groups func(context.Context) []string, observe func(string, int, int),
	log *slog.Logger,
) (*assistant.Assistant, error) {
	o := assistant.Options{Access: cfg.Assistant.Access, PublicURL: cfg.Server.PublicURL, Groups: groups, Observe: observe, Log: log}
	if cfg.LLM.Provider == "" {
		return assistant.New(o), nil
	}
	chat, err := llm.New(llm.Config{Provider: cfg.LLM.Provider, Model: cfg.LLM.Model, BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey.Reveal()})
	if err != nil {
		return nil, err
	}
	a := cfg.Assistant
	o.Chat, o.Tools = chat, reads.tools()
	o.Budget = assistant.Budget{Concurrency: a.Concurrency, MaxTurns: a.MaxTurns, MaxToolCalls: a.MaxToolCalls, MaxTokens: a.MaxTokens,
		MaxHistory: a.MaxHistory, MaxInputChars: a.MaxInputChars, ToolResultChars: a.ToolResultChars}
	return assistant.New(o), nil
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

func (r assistantReads) tools() []assistant.Tool {
	return []assistant.Tool{
		{Tool: llm.Tool{Name: "search_repositories",
			Description: "Lists the catalogued repositories, optionally of one group and matching words in the name, description, topics or language; newest activity first.",
			Schema: schema(nil, map[string]any{"query": prop("string", "Words to match; empty lists all"), "group": prop("string", "A group id"),
				"limit": prop("integer", "At most this many, up to 50 (default 20)")})},
			Label: func(in map[string]any) string {
				if q := arg(in, "query"); q != "" {
					return "Searching for " + q
				}
				return "Listing repositories"
			},
			Run: r.search},
		{Tool: llm.Tool{Name: "list_groups", Description: "Lists the groups of repositories with their ids, names and sizes.", Schema: schema(nil, map[string]any{})},
			Label: func(map[string]any) string { return "Listing groups" }, Run: r.listGroups},
		{Tool: llm.Tool{Name: "get_group", Description: "Reads a group: its description, health and repositories.",
			Schema: schema([]string{"group"}, map[string]any{"group": prop("string", "The group id")})},
			Label: func(in map[string]any) string { return "Reading the group " + arg(in, "group") }, Run: r.getGroup},
		{Tool: llm.Tool{Name: "get_repository", Description: "Reads a repository's README, its technology and its internal dependencies.",
			Schema: schema([]string{"owner", "name"}, map[string]any{"owner": ownerProp(), "name": nameProp()})},
			Label: func(in map[string]any) string { return "Reading " + arg(in, "owner") + "/" + arg(in, "name") }, Run: r.getRepository},
		{Tool: llm.Tool{Name: "get_document", Description: "Reads one document of a repository's docs/ by its path, as get_repository and get_group link it.",
			Schema: schema([]string{"owner", "name", "path"}, map[string]any{"owner": ownerProp(), "name": nameProp(), "path": prop("string", "The path, such as docs/setup.md")})},
			Label: func(in map[string]any) string {
				return "Reading " + arg(in, "path") + " of " + arg(in, "owner") + "/" + arg(in, "name")
			},
			Run: r.getDocument},
		{Tool: llm.Tool{Name: "recent_commits", Description: "Lists the newest default-branch commits, of everything, a group or one repository, without automation.",
			Schema: schema(nil, map[string]any{"group": prop("string", "A group id"), "repo": prop("string", "A repository as owner/name"),
				"limit": prop("integer", "At most this many, up to 50 (default 20)")})},
			Label: func(in map[string]any) string {
				if s := arg(in, "repo") + arg(in, "group"); s != "" {
					return "Reading the commits of " + s
				}
				return "Reading the latest commits"
			},
			Run: r.recentCommits},
		{Tool: llm.Tool{Name: "repository_health", Description: "Reads a repository's health score, level and findings, its latest release and activity.",
			Schema: schema([]string{"owner", "name"}, map[string]any{"owner": ownerProp(), "name": nameProp()})},
			Label: func(in map[string]any) string {
				return "Checking the health of " + arg(in, "owner") + "/" + arg(in, "name")
			},
			Run: r.health},
	}
}

var errNotFound = errors.New("not found in the catalogue")

func day(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02")
}

func repoLine(b *strings.Builder, p oapi.Repo) {
	fmt.Fprintf(b, "- [%s/%s](/repos/%s)", p.Owner, p.Name, p.Address)
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

func (r assistantReads) search(ctx context.Context, in map[string]any) (string, string, error) {
	var group *string
	if g := arg(in, "group"); g != "" {
		group = &g
	}
	res, err := r.catalog.ListRepos(ctx, oapi.ListReposRequestObject{Params: oapi.ListReposParams{Group: group}})
	if err != nil {
		return "", "", err
	}
	ok, isOK := res.(oapi.ListRepos200JSONResponse)
	if !isOK {
		return "", "", fmt.Errorf("the group %q is %w", arg(in, "group"), errNotFound)
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
	return "/repos", b.String(), nil
}

func (r assistantReads) listGroups(ctx context.Context, _ map[string]any) (string, string, error) {
	res, err := r.catalog.ListGroups(ctx, oapi.ListGroupsRequestObject{})
	if err != nil {
		return "", "", err
	}
	ok, _ := res.(oapi.ListGroups200JSONResponse)
	var b strings.Builder
	for _, g := range ok.Body.Groups {
		fmt.Fprintf(&b, "- [%s](/groups/%s), id %s, %d repositories", g.Name, g.Id, g.Id, g.RepoCount)
		if g.Description != "" {
			b.WriteString(": " + g.Description)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "/groups", "There are no groups.", nil
	}
	return "/groups", b.String(), nil
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

func (r assistantReads) getGroup(ctx context.Context, in map[string]any) (string, string, error) {
	g := arg(in, "group")
	text, err := page(ctx, r.group, "/groups/"+g, map[string]string{"group": g})
	if err != nil {
		return "", "", fmt.Errorf("the group %q is %w; list_groups names them", g, err)
	}
	return "/groups/" + g, text, nil
}

// resolve finds a repository the viewer may see, following a rename.
func (r assistantReads) resolve(ctx context.Context, in map[string]any) (oapi.RepoDetail, error) {
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

func (r assistantReads) getRepository(ctx context.Context, in map[string]any) (string, string, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return "", "", err
	}
	owner, name, _ := strings.Cut(d.Repo.Address, "/")
	text, err := page(ctx, r.repo, "/repos/"+d.Repo.Address, map[string]string{"owner": owner, "name": name})
	if err != nil {
		return "", "", err
	}
	return "/repos/" + d.Repo.Address, text, nil
}

func (r assistantReads) getDocument(ctx context.Context, in map[string]any) (string, string, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return "", "", err
	}
	owner, name, _ := strings.Cut(d.Repo.Address, "/")
	p := strings.TrimPrefix(arg(in, "path"), "/")
	target := "/repos/" + d.Repo.Address + "/docs/" + p
	text, err := page(ctx, r.doc, target, map[string]string{"owner": owner, "name": name, "path": p})
	if err != nil {
		return "", "", fmt.Errorf("the document %s of %s is %w", p, d.Repo.Address, err)
	}
	return target, text, nil
}

func (r assistantReads) recentCommits(ctx context.Context, in map[string]any) (string, string, error) {
	limit, no := intArg(in, "limit", 20, 50), false
	params := oapi.ListCommitsParams{Automation: &no, Limit: &limit}
	if g := arg(in, "group"); g != "" {
		params.Group = &g
	}
	if p := strings.ToLower(arg(in, "repo")); p != "" {
		params.Repo = &p
	}
	res, err := r.commits.ListCommits(ctx, oapi.ListCommitsRequestObject{Params: params})
	if err != nil {
		return "", "", err
	}
	ok, isOK := res.(oapi.ListCommits200JSONResponse)
	if !isOK {
		return "", "", fmt.Errorf("the group or repository is %w", errNotFound)
	}
	var b strings.Builder
	for _, c := range ok.Body.Commits {
		fmt.Fprintf(&b, "- %s %s [%s](/repos/%s) %s: %s\n", c.CommittedAt.UTC().Format("2006-01-02 15:04"), c.Sha[:min(7, len(c.Sha))], c.Repo, c.Repo, c.AuthorName, c.Subject)
	}
	if b.Len() == 0 {
		return "/commits", "No commits.", nil
	}
	return "/commits", b.String(), nil
}

func (r assistantReads) health(ctx context.Context, in map[string]any) (string, string, error) {
	d, err := r.resolve(ctx, in)
	if err != nil {
		return "", "", err
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
	fmt.Fprintf(&b, "Open issues and pull requests: %d. License: %s.\n", p.OpenIssues, orUnknown(p.License))
	return "/repos/" + p.Address, b.String(), nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
