package commits

import (
	"context"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/github"
)

// CommitInfo is a commit as GitHub lists it; normalise cleans the fields.
type CommitInfo struct {
	SHA, Message, AuthorName, AuthorLogin, HTMLURL string
	Date                                           time.Time // committer date
}

// Fetcher lists a repository's latest commits.
type Fetcher interface {
	Commits(ctx context.Context, owner, name string, n int) ([]CommitInfo, error)
}

// REST is a source's GitHub API client per owner; *github.Source implements it.
type REST interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
}

// FromGitHub lists commits through src.
func FromGitHub(src REST) Fetcher { return rest{src} }

type rest struct{ src REST }

// Commits returns the latest n commits of the repository's default branch, newest first.
func (r rest) Commits(ctx context.Context, owner, name string, n int) ([]CommitInfo, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	got, resp, err := c.Repositories.ListCommits(ctx, owner, name, &gh.CommitsListOptions{ListOptions: gh.ListOptions{PerPage: n}})
	if err != nil {
		return nil, github.Classify(resp, err)
	}
	out := make([]CommitInfo, 0, len(got))
	for _, rc := range got {
		out = append(out, CommitInfo{SHA: rc.GetSHA(), Message: rc.GetCommit().GetMessage(), AuthorName: rc.GetCommit().GetAuthor().GetName(),
			AuthorLogin: rc.GetAuthor().GetLogin(), HTMLURL: rc.GetHTMLURL(), Date: rc.GetCommit().GetCommitter().GetDate().Time})
	}
	return out, nil
}
