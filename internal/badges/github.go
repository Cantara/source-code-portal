package badges

import (
	"context"

	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/github"
)

// Releases reads the latest release. nil, nil is no release.
type Releases interface {
	LatestRelease(ctx context.Context, owner, name string) (*Release, error)
}

// REST is a source's GitHub API client per owner; *github.Source implements it.
type REST interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
}

// FromGitHub reads releases through src.
func FromGitHub(src REST) Releases { return rest{src} }

type rest struct{ src REST }

// LatestRelease returns the latest published, non-prerelease release; nil when the repository has none.
func (r rest) LatestRelease(ctx context.Context, owner, name string) (*Release, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	rel, resp, err := c.Repositories.GetLatestRelease(ctx, owner, name)
	if err != nil {
		if err := github.Classify(resp, err); !github.IsNotFound(err) {
			return nil, err
		}
		return nil, nil //nolint:nilnil // no release is not an error
	}
	return &Release{Tag: rel.GetTagName(), Name: rel.GetName(), URL: rel.GetHTMLURL(), Published: rel.GetPublishedAt().UTC()}, nil
}
