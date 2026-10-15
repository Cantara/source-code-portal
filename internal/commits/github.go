package commits

import (
	"context"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/github"
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
	got, resp, err := c.Repositories.ListCommits(ctx, owner, name,
		&gh.CommitsListOptions{SHA: ref, ListOptions: gh.ListOptions{PerPage: pageSize, Page: page}})
	if err != nil {
		return nil, false, github.Classify(resp, err)
	}
	out := make([]CommitInfo, 0, len(got))
	for _, rc := range got {
		ci := CommitInfo{SHA: rc.GetSHA(), Message: rc.GetCommit().GetMessage(), AuthorName: rc.GetCommit().GetAuthor().GetName(),
			AuthorLogin: rc.GetAuthor().GetLogin(), CommitterName: rc.GetCommit().GetCommitter().GetName(), HTMLURL: rc.GetHTMLURL(),
			Date: rc.GetCommit().GetCommitter().GetDate().Time}
		for _, p := range rc.Parents {
			ci.Parents = append(ci.Parents, p.GetSHA())
		}
		out = append(out, ci)
	}
	return out, resp.NextPage != 0, nil
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
