package pulls

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

	gh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/jsonstream"
)

// Listed is an open pull request as GitHub's REST listing gives it.
type Listed struct {
	Number                 int
	Title, URL, Author     string
	AuthorBot              bool // the author's account type is Bot
	Draft, Fork            bool
	Created, Updated       time.Time
	Base, Head, HeadSHA    string
	Labels                 []Label
	Requested              int // reviewers and teams asked
	Reviewers              []string
	baseRepo, headRepoName string
}

// Detail is what GraphQL adds to a pull request: its review decision, the reviews that hold an opinion, its checks, whether it
// merges and its diff stats.
type Detail struct {
	Review    string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or empty when no review is required
	Approved  bool   // a reviewer's latest opinion approves
	Changes   bool   // a reviewer's latest opinion asks for changes
	Checks    string // the head commit's rollup: SUCCESS, FAILURE, ERROR, PENDING, EXPECTED, or empty for none
	Mergeable string // MERGEABLE, CONFLICTING, UNKNOWN
	Stats     Stats
}

// Fetcher reads a repository's open pull requests.
type Fetcher interface {
	// Open lists the first 100 open pull requests, the most recently updated first.
	Open(ctx context.Context, owner, name string) ([]Listed, error)
	// Details asks at most maxAsk pull requests by number, and the total of open pull requests when total is set (-1 otherwise).
	Details(ctx context.Context, owner, name string, numbers []int, total bool) (map[int]Detail, int, error)
}

// maxAsk is the pull requests one GraphQL query asks, as the REST listing's page.
const maxAsk = 100

// REST is a source's GitHub API client per owner; *github.Source implements it.
type REST interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
}

// FromGitHub reads pull requests through src.
func FromGitHub(src REST) Fetcher { return rest{src} }

type rest struct{ src REST }

// Open lists the open pull requests through the conditional transport: an unchanged listing is a 304, which GitHub does not count.
func (r rest) Open(ctx context.Context, owner, name string) ([]Listed, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, err
	}
	q := url.Values{"state": {"open"}, "per_page": {strconv.Itoa(maxAsk)}, "sort": {"updated"}, "direction": {"desc"}}
	req, err := c.NewRequest(ctx, http.MethodGet, "repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/pulls?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.BareDo(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, github.Classify(resp, err)
	}
	ps, err := readPulls(resp.Body)
	switch {
	case errors.Is(err, jsonstream.ErrSyntax):
		return nil, github.Classify(resp, err)
	case err != nil:
		return nil, &github.Error{Kind: github.Transient, Code: "transient", Err: err}
	}
	return ps, nil
}

// maxField is the bytes of a read field; titles and labels are cut again when they are cleaned.
const (
	maxField     = 1 << 10
	maxLabels    = 5
	maxReviewers = 10 // people asked to review kept by login
)

// readPulls reads GitHub's list of pull requests, keeping only the fields a Listed holds; bodies and everything else are skipped.
func readPulls(body io.Reader) ([]Listed, error) {
	d := jsonstream.New(body)
	var out []Listed
	err := d.Array(func() error {
		var p Listed
		var created, updated string
		err := d.Object(func(key string) (err error) {
			switch key {
			case "number":
				var n int64
				n, err = d.Int()
				p.Number = int(n)
			case "title":
				p.Title, err = d.String(maxField)
			case "html_url":
				p.URL, err = d.String(maxField)
			case "draft":
				p.Draft, err = d.Bool()
			case "created_at":
				created, err = d.String(maxField)
			case "updated_at":
				updated, err = d.String(maxField)
			case "user":
				err = d.Object(func(k string) (err error) {
					switch k {
					case "login":
						p.Author, err = d.String(maxField)
					case "type":
						var t string
						t, err = d.String(maxField)
						p.AuthorBot = t == "Bot"
					default:
						err = d.Skip()
					}
					return err
				})
			case "base", "head":
				err = d.Object(func(k string) (err error) {
					switch k {
					case "ref":
						var ref string
						ref, err = d.String(maxField)
						if key == "base" {
							p.Base = ref
						} else {
							p.Head = ref
						}
					case "sha":
						var sha string
						sha, err = d.String(maxField)
						if key == "head" {
							p.HeadSHA = sha
						}
					case "repo": // null for a head whose fork was deleted
						err = d.Object(func(k string) (err error) {
							if k != "full_name" {
								return d.Skip()
							}
							var full string
							full, err = d.String(maxField)
							if key == "base" {
								p.baseRepo = full
							} else {
								p.headRepoName = full
							}
							return err
						})
					default:
						err = d.Skip()
					}
					return err
				})
			case "labels":
				err = d.Array(func() error {
					var l Label
					if err := d.Object(func(k string) (err error) {
						switch k {
						case "name":
							l.Name, err = d.String(maxField)
						case "color":
							l.Color, err = d.String(maxField)
						default:
							err = d.Skip()
						}
						return err
					}); err != nil {
						return err
					}
					if len(p.Labels) < maxLabels && l.Name != "" {
						p.Labels = append(p.Labels, l)
					}
					return nil
				})
			case "requested_teams":
				err = d.Array(func() error { p.Requested++; return d.Skip() })
			case "requested_reviewers":
				err = readReviewers(d, &p)
			default:
				err = d.Skip()
			}
			return err
		})
		if err != nil {
			return err
		}
		p.Created, _ = time.Parse(time.RFC3339, created)
		p.Updated, _ = time.Parse(time.RFC3339, updated)
		p.Fork = !strings.EqualFold(p.baseRepo, p.headRepoName)
		if p.Number > 0 {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// Details asks GraphQL for the pull requests by number, one alias each: about one point for up to 100.
func (r rest) Details(ctx context.Context, owner, name string, numbers []int, total bool) (map[int]Detail, int, error) {
	if len(numbers) > maxAsk {
		return nil, -1, fmt.Errorf("pulls: %d asked, at most %d", len(numbers), maxAsk)
	}
	if len(numbers) == 0 && !total {
		return map[int]Detail{}, -1, nil
	}
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, -1, err
	}
	var q strings.Builder
	q.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	if total {
		q.WriteString("open:pullRequests(states:OPEN,first:1){totalCount}")
	}
	for _, n := range numbers {
		fmt.Fprintf(&q, "p%d:pullRequest(number:%d){reviewDecision mergeable additions deletions changedFiles "+
			"latestOpinionatedReviews(first:10){nodes{state}} commits(last:1){nodes{commit{statusCheckRollup{state}}}}}", n, n)
	}
	q.WriteString("}}")
	var out struct {
		Repository map[string]*detailJSON `json:"repository"`
	}
	if err := github.GraphQL(ctx, c, q.String(), map[string]any{"owner": owner, "name": name}, &out); err != nil {
		return nil, -1, err
	}
	if out.Repository == nil {
		return nil, -1, &github.Error{Kind: github.NotFound, Code: "not_found", Err: errors.New("graphql: no repository")}
	}
	n := -1
	if v := out.Repository["open"]; v != nil && v.TotalCount != nil {
		n = *v.TotalCount
	}
	got := make(map[int]Detail, len(numbers))
	for _, num := range numbers {
		v := out.Repository["p"+strconv.Itoa(num)]
		if v == nil {
			continue
		}
		dt := Detail{Review: v.ReviewDecision, Mergeable: v.Mergeable,
			Stats: Stats{Additions: max(v.Additions, 0), Deletions: max(v.Deletions, 0), Files: max(v.ChangedFiles, 0)}}
		for _, rv := range v.LatestOpinionatedReviews.Nodes {
			dt.Approved = dt.Approved || rv.State == "APPROVED"
			dt.Changes = dt.Changes || rv.State == "CHANGES_REQUESTED"
		}
		if len(v.Commits.Nodes) > 0 && v.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			dt.Checks = v.Commits.Nodes[0].Commit.StatusCheckRollup.State
		}
		got[num] = dt
	}
	return got, n, nil
}

// detailJSON is an alias's answer: a pull request, or the open count.
type detailJSON struct {
	TotalCount               *int   `json:"totalCount"`
	ReviewDecision           string `json:"reviewDecision"`
	Mergeable                string `json:"mergeable"`
	Additions                int    `json:"additions"`
	Deletions                int    `json:"deletions"`
	ChangedFiles             int    `json:"changedFiles"`
	LatestOpinionatedReviews struct {
		Nodes []struct {
			State string `json:"state"`
		} `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// readReviewers reads the people asked to review: counted, and the first maxReviewers kept by login.
func readReviewers(d *jsonstream.Reader, p *Listed) error {
	return d.Array(func() error {
		p.Requested++
		return d.Object(func(k string) (err error) {
			if k != "login" {
				return d.Skip()
			}
			var login string
			if login, err = d.String(maxField); err == nil && len(p.Reviewers) < maxReviewers && login != "" {
				p.Reviewers = append(p.Reviewers, login)
			}
			return err
		})
	})
}
