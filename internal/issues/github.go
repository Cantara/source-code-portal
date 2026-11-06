package issues

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/cantara/source-code-portal/internal/github"
	"github.com/cantara/source-code-portal/internal/jsonstream"
)

// Listed is an open issue as GitHub's REST listing gives it.
type Listed struct {
	Number             int
	Title, URL, Author string
	AuthorBot          bool // the author's account type is Bot
	Created, Updated   time.Time
	Labels             []Label
	Assignees          []string // the first maxListed logins
	Assigned           int      // every assignee counted
	Comments           int
	Milestone, Type    string
	pull               bool // a pull request in the listing
}

// Fetcher reads a repository's open issues.
type Fetcher interface {
	// Open lists up to maxIssues open issues, the most recently updated first; exact reports that every one was listed.
	Open(ctx context.Context, owner, name string) (issues []Listed, exact bool, err error)
}

const (
	perPage   = 100
	maxPages  = 3   // the listing holds the pull requests too: a repository with many still reaches its issues
	maxIssues = 100 // held per repository
)

// REST is a source's GitHub API client per owner; *github.Source implements it.
type REST interface {
	REST(ctx context.Context, owner string) (*gh.Client, error)
}

// FromGitHub reads issues through src.
func FromGitHub(src REST) Fetcher { return rest{src} }

type rest struct{ src REST }

// Open reads the listing page by page through the conditional transport: an unchanged page is a 304, which GitHub does not count.
func (r rest) Open(ctx context.Context, owner, name string) ([]Listed, bool, error) {
	c, err := r.src.REST(ctx, owner)
	if err != nil {
		return nil, false, err
	}
	var out []Listed
	dropped := false
	for page := 1; page <= maxPages; page++ {
		items, err := r.page(ctx, c, owner, name, page)
		if err != nil {
			return nil, false, err
		}
		for _, it := range items {
			switch {
			case it.pull:
			case len(out) < maxIssues:
				out = append(out, it)
			default:
				dropped = true
			}
		}
		if len(items) < perPage { // the last page
			return out, !dropped, nil
		}
		if len(out) >= maxIssues {
			break
		}
	}
	return out, false, nil
}

func (r rest) page(ctx context.Context, c *gh.Client, owner, name string, page int) ([]Listed, error) {
	q := url.Values{"state": {"open"}, "per_page": {strconv.Itoa(perPage)}, "sort": {"updated"}, "direction": {"desc"}}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/issues?"+q.Encode(), nil)
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
	items, err := readIssues(resp.Body)
	switch {
	case errors.Is(err, jsonstream.ErrSyntax):
		return nil, github.Classify(resp, err)
	case err != nil:
		return nil, &github.Error{Kind: github.Transient, Code: "transient", Err: err}
	}
	return items, nil
}

const (
	maxField  = 1 << 10 // the bytes of a read field; titles and labels are cut again when they are cleaned
	maxLabels = 5
	maxListed = 20 // assignees read; the count goes on
)

// readIssues reads GitHub's list of issues, keeping only the fields a Listed holds; bodies and everything else are skipped.
func readIssues(body io.Reader) ([]Listed, error) {
	d := jsonstream.New(body)
	var out []Listed
	err := d.Array(func() error {
		var l Listed
		var created, updated string
		err := d.Object(func(key string) (err error) {
			switch key {
			case "number":
				var n int64
				n, err = d.Int()
				l.Number = int(n)
			case "title":
				l.Title, err = d.String(maxField)
			case "html_url":
				l.URL, err = d.String(maxField)
			case "comments":
				var n int64
				n, err = d.Int()
				l.Comments = int(max(n, 0))
			case "created_at":
				created, err = d.String(maxField)
			case "updated_at":
				updated, err = d.String(maxField)
			case "pull_request":
				l.pull = true
				err = d.Skip()
			case "user":
				err = d.Object(func(k string) (err error) {
					switch k {
					case "login":
						l.Author, err = d.String(maxField)
					case "type":
						var t string
						t, err = d.String(maxField)
						l.AuthorBot = t == "Bot"
					default:
						err = d.Skip()
					}
					return err
				})
			case "milestone", "type": // null when none
				err = d.Object(func(k string) (err error) {
					field := "name"
					if key == "milestone" {
						field = "title"
					}
					if k != field {
						return d.Skip()
					}
					var v string
					v, err = d.String(maxField)
					if key == "milestone" {
						l.Milestone = v
					} else {
						l.Type = v
					}
					return err
				})
			case "assignees":
				err = d.Array(func() error {
					l.Assigned++
					return d.Object(func(k string) (err error) {
						if k != "login" || len(l.Assignees) >= maxListed {
							return d.Skip()
						}
						var login string
						login, err = d.String(maxField)
						if login != "" {
							l.Assignees = append(l.Assignees, login)
						}
						return err
					})
				})
			case "labels":
				err = d.Array(func() error {
					var lb Label
					if err := d.Object(func(k string) (err error) {
						switch k {
						case "name":
							lb.Name, err = d.String(maxField)
						case "color":
							lb.Color, err = d.String(maxField)
						default:
							err = d.Skip()
						}
						return err
					}); err != nil {
						return err
					}
					if len(l.Labels) < maxLabels && lb.Name != "" {
						l.Labels = append(l.Labels, lb)
					}
					return nil
				})
			default:
				err = d.Skip()
			}
			return err
		})
		if err != nil {
			return err
		}
		l.Created, _ = time.Parse(time.RFC3339, created)
		l.Updated, _ = time.Parse(time.RFC3339, updated)
		if l.Number > 0 {
			out = append(out, l)
		}
		return nil
	})
	return out, err
}
