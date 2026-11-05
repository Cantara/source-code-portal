package commits

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/jsonstream"
)

// CommitInfo is a commit as GitHub lists it; normalise cleans the fields.
type CommitInfo struct {
	SHA, Message, AuthorName, AuthorLogin, CommitterName, HTMLURL string
	Date                                                          time.Time // committer date
	Parents                                                       []string  // first parent first
}

// RefInfo is a branch or a tag and the commit it points at.
type RefInfo struct{ Name, SHA string }

// Fetcher reads a repository's history.
type Fetcher interface {
	// Commits lists page (from 1) of the commits reachable from ref ("" for the default branch), 100 per page, newest first; more
	// tells whether another page follows.
	Commits(ctx context.Context, owner, name, ref string, page int) (cs []CommitInfo, more bool, err error)
	// Branches and Tags list the first 100 branches and tags.
	Branches(ctx context.Context, owner, name string) ([]RefInfo, error)
	Tags(ctx context.Context, owner, name string) ([]RefInfo, error)
	// Stats asks the diff stats of at most statsBatch commits by SHA; a commit GitHub does not answer is left out.
	Stats(ctx context.Context, owner, name string, shas []string) (map[string]Stats, error)
}

// pageSize is the commits per listed page.
const pageSize = 100

// REST is a source's GitHub API client per owner; *github.Source implements it.
type REST interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
}

// FromGitHub lists commits through src.
func FromGitHub(src REST) Fetcher { return rest{src} }

type rest struct{ src REST }

func (r rest) Commits(ctx context.Context, owner, name, ref string, page int) ([]CommitInfo, bool, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, false, err
	}
	q := url.Values{"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
	if ref != "" {
		q.Set("sha", ref)
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/commits?"+q.Encode(), nil)
	if err != nil {
		return nil, false, err
	}
	// The body is read as a stream, not decoded whole: a page of commits has no size limit (a squashed pull request's message can
	// be tens of kilobytes), and each commit keeps only the bounded fields the history needs.
	resp, err := c.BareDo(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, false, github.Classify(resp, err)
	}
	cs, err := readCommits(resp.Body)
	switch {
	case errors.Is(err, jsonstream.ErrSyntax):
		return nil, false, github.Classify(resp, err) // a 200 that is not JSON: malformed
	case err != nil:
		return nil, false, &github.Error{Kind: github.Transient, Code: "transient", Err: err} // the connection broke mid-page
	}
	return cs, resp.NextPage != 0, nil
}

// The bounds of what a listed commit keeps as it is read. A message keeps its start and its end: the end holds the trailers that
// say who made it (an AI co-author, a tool's footer), and normalise cuts the body further to maxBody.
const (
	maxMessageHead = 16 << 10 // bytes
	maxMessageTail = 8 << 10  // bytes
	maxField       = 1 << 10  // bytes of a name, a login, an address, a date or a SHA
	maxRead        = 64       // parents read of a commit; normalise keeps maxParents
)

// readCommits reads GitHub's list of commits: per commit its SHA, address, message, author name and login, committer name and date,
// and parents. Everything else is skipped as it streams by.
func readCommits(body io.Reader) ([]CommitInfo, error) {
	d := jsonstream.New(body)
	var out []CommitInfo
	err := d.Array(func() error {
		var ci CommitInfo
		err := d.Object(func(key string) (err error) {
			switch key {
			case "sha":
				ci.SHA, err = d.String(maxField)
			case "html_url":
				ci.HTMLURL, err = d.String(maxField)
			case "author": // the GitHub account, null when the address matches none
				err = d.Object(func(k string) (err error) {
					if k == "login" {
						ci.AuthorLogin, err = d.String(maxField)
						return err
					}
					return d.Skip()
				})
			case "parents":
				err = d.Array(func() error {
					return d.Object(func(k string) error {
						if k != "sha" || len(ci.Parents) == maxRead {
							return d.Skip()
						}
						sha, err := d.String(maxField)
						ci.Parents = append(ci.Parents, sha)
						return err
					})
				})
			case "commit":
				err = readGitCommit(d, &ci)
			default:
				err = d.Skip()
			}
			return err
		})
		out = append(out, ci)
		return err
	})
	return out, err
}

// readGitCommit reads a listed commit's git part: its message and the names and date of its author and committer.
func readGitCommit(d *jsonstream.Reader, ci *CommitInfo) error {
	return d.Object(func(key string) error {
		switch key {
		case "message":
			start, end, cut, err := d.Ends(maxMessageHead, maxMessageTail)
			ci.Message = start
			if cut {
				ci.Message = start + "\n\n…\n\n" + end
			}
			return err
		case "author", "committer":
			return d.Object(func(k string) error {
				switch {
				case k == "name" && key == "author":
					s, err := d.String(maxField)
					ci.AuthorName = s
					return err
				case k == "name":
					s, err := d.String(maxField)
					ci.CommitterName = s
					return err
				case k == "date" && key == "committer":
					s, err := d.String(maxField)
					ci.Date, _ = time.Parse(time.RFC3339, s) // an unreadable date is the zero time: normalise drops the commit
					return err
				}
				return d.Skip()
			})
		}
		return d.Skip()
	})
}

func (r rest) Branches(ctx context.Context, owner, name string) ([]RefInfo, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	got, resp, err := c.Repositories.ListBranches(ctx, owner, name, &gh.BranchListOptions{ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, github.Classify(resp, err)
	}
	out := make([]RefInfo, 0, len(got))
	for _, b := range got {
		out = append(out, RefInfo{Name: b.GetName(), SHA: b.GetCommit().GetSHA()})
	}
	return out, nil
}

func (r rest) Tags(ctx context.Context, owner, name string) ([]RefInfo, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	got, resp, err := c.Repositories.ListTags(ctx, owner, name, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, github.Classify(resp, err)
	}
	out := make([]RefInfo, 0, len(got))
	for _, t := range got {
		out = append(out, RefInfo{Name: t.GetName(), SHA: t.GetCommit().GetSHA()})
	}
	return out, nil
}

// statsBatch is the commits one GraphQL query asks (design/diffstat.md).
const statsBatch = 100

// Stats asks GitHub's GraphQL API for the diff stats of shas, one alias per commit by its object id: one query of about one point
// for up to 100 commits, where REST needs a request per commit.
func (r rest) Stats(ctx context.Context, owner, name string, shas []string) (map[string]Stats, error) {
	if len(shas) == 0 {
		return map[string]Stats{}, nil
	}
	if len(shas) > statsBatch {
		return nil, fmt.Errorf("commits: %d stats asked, at most %d", len(shas), statsBatch)
	}
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	var q strings.Builder
	q.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	for i, sha := range shas {
		if !shaPattern.MatchString(sha) {
			return nil, fmt.Errorf("commits: %q is not a SHA", sha)
		}
		fmt.Fprintf(&q, `c%d:object(oid:"%s"){...on Commit{additions deletions changedFilesIfAvailable}}`, i, sha)
	}
	q.WriteString("}}")
	var out struct {
		Repository map[string]*struct {
			Additions               int  `json:"additions"`
			Deletions               int  `json:"deletions"`
			ChangedFilesIfAvailable *int `json:"changedFilesIfAvailable"`
		} `json:"repository"`
	}
	if err := github.GraphQL(ctx, c, q.String(), map[string]any{"owner": owner, "name": name}, &out); err != nil {
		return nil, err
	}
	if out.Repository == nil {
		return nil, &github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("graphql: no repository")}
	}
	stats := make(map[string]Stats, len(shas))
	for i, sha := range shas {
		v := out.Repository["c"+strconv.Itoa(i)]
		if v == nil || v.Additions < 0 || v.Deletions < 0 {
			continue // not a commit GitHub has: the job marks it asked
		}
		files := -1
		if v.ChangedFilesIfAvailable != nil && *v.ChangedFilesIfAvailable >= 0 {
			files = *v.ChangedFilesIfAvailable
		}
		stats[sha] = Stats{Additions: v.Additions, Deletions: v.Deletions, Files: files}
	}
	return stats, nil
}
