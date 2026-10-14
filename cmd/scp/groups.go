package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

// listedRepo is a repository as the GitHub REST API lists it (`gh api --paginate orgs/<org>/repos`), the fields grouping reads.
type listedRepo struct {
	ID         int64                  `json:"id"`
	Name       string                 `json:"name"`
	Owner      struct{ Login string } `json:"owner"`
	Topics     []string               `json:"topics"`
	Fork       bool                   `json:"fork"`
	Archived   bool                   `json:"archived"`
	Visibility string                 `json:"visibility"`
}

func newGroupsCmd(opts *options) *cobra.Command {
	var reposFile string
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Show which group each repository lands in, from a repository listing, without calling GitHub",
		Long: `Show which group each repository lands in, with the groups and source filters of the configuration.

The listing is a file (or - for stdin) with either the JSON GitHub returns for an organisation's repositories, for example
  gh api --paginate "orgs/Cantara/repos?per_page=100" > repos.json
or one repository name per line (then topics, forks, archived and private repositories are not known).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := loadConfig(cmd, opts)
			if err != nil {
				return err
			}
			in, err := openListing(reposFile, cmd.InOrStdin())
			if err != nil {
				return failure{err}
			}
			listed, err := readListing(in)
			if err != nil {
				return err
			}
			return failure{previewGroups(cmd.OutOrStdout(), cfg, listed)}.orNil()
		},
	}
	cmd.Flags().StringVar(&reposFile, "repos", "", "repository listing: GitHub's JSON or one name per line (- for stdin)")
	_ = cmd.MarkFlagRequired("repos")
	return cmd
}

func openListing(file string, stdin io.Reader) (io.Reader, error) {
	if file == "-" {
		return stdin, nil
	}
	b, err := os.ReadFile(file) //nolint:gosec // G304: the operator names the file
	return bytes.NewReader(b), err
}

// readListing reads GitHub's JSON (one array, or one array per page as `gh api --paginate` prints them) or one name per line.
func readListing(r io.Reader) ([]listedRepo, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, failure{err}
	}
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '[' {
		var out []listedRepo
		dec := json.NewDecoder(bytes.NewReader(t))
		for {
			var page []listedRepo
			if err := dec.Decode(&page); errors.Is(err, io.EOF) {
				return out, nil
			} else if err != nil {
				return nil, fmt.Errorf("the repository listing is not GitHub's JSON: %w", err)
			}
			out = append(out, page...)
		}
	}
	var out []listedRepo
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if name := strings.TrimSpace(sc.Text()); name != "" && !strings.HasPrefix(name, "#") {
			out = append(out, listedRepo{ID: int64(len(out) + 1), Name: name, Visibility: "public"})
		}
	}
	return out, sc.Err()
}

// previewGroups prints every group with its members, then the repositories no group selects and those the source filters leave out.
func previewGroups(w io.Writer, cfg config.Config, listed []listedRepo) error {
	groups, _ := catalog.Compile(cfg.Catalog.Groups) // errors were refused by loadConfig
	repos := map[snapshot.RepoKey]*snapshot.RepoEntry{}
	var left []string
	for _, src := range cfg.GitHub.Sources {
		var mine []snapshot.Repo
		for _, l := range listed {
			if l.Owner.Login == "" || slices.ContainsFunc(src.Orgs, func(o string) bool { return strings.EqualFold(o, l.Owner.Login) }) {
				owner := l.Owner.Login
				if owner == "" {
					owner = src.Orgs[0]
				}
				mine = append(mine, snapshot.Repo{ID: snapshot.RepoID{Source: src.ID, GitHubID: l.ID}, Owner: owner, Name: l.Name,
					Topics: l.Topics, Fork: l.Fork, Archived: l.Archived, Visibility: l.Visibility})
			}
		}
		admitted := map[string]bool{}
		for _, t := range catalog.Plan(mine, nil, catalog.Filter{IncludeForks: src.IncludeForks, IncludeArchived: src.IncludeArchived,
			IncludePrivate: src.IncludePrivate}) {
			repos[t.Repo.Key()] = &snapshot.RepoEntry{Repo: t.Repo}
			admitted[t.Repo.Name] = true
		}
		for _, r := range mine {
			if !admitted[r.Name] {
				left = append(left, r.Name+" ("+why(r)+")")
			}
		}
	}
	grouped := map[snapshot.RepoKey]bool{}
	for _, g := range groups.Resolve(repos) {
		def := string(g.DefaultRepo)
		if g.DefaultMissing {
			def = "MISSING: no member is named " + defaultOf(cfg, g.ID)
		}
		fmt.Fprintf(w, "%s: %s (%d repositories; default %s)\n", g.ID, g.Name, len(g.Members), def)
		for _, k := range g.Members {
			grouped[k] = true
			fmt.Fprintf(w, "  %s\n", repos[k].Repo.Name)
		}
	}
	var none []string
	for k, e := range repos {
		if !grouped[k] {
			none = append(none, e.Repo.Name)
		}
	}
	for _, list := range []struct {
		title string
		names []string
	}{{"In no group", none}, {"Left out by the source filters", left}} {
		if len(list.names) > 0 {
			slices.Sort(list.names)
			fmt.Fprintf(w, "%s (%d):\n  %s\n", list.title, len(list.names), strings.Join(list.names, "\n  "))
		}
	}
	return nil
}

func why(r snapshot.Repo) string {
	switch {
	case r.Fork:
		return "fork; include_forks"
	case r.Archived:
		return "archived; include_archived"
	case !r.Public():
		return r.Visibility + "; include_private"
	}
	return "not admitted"
}

func defaultOf(cfg config.Config, id string) string {
	for _, g := range cfg.Catalog.Groups {
		if g.ID == id {
			return g.DefaultRepo
		}
	}
	return ""
}
