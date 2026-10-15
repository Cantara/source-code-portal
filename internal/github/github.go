// Package github reads repositories from GitHub for the sync jobs: GitHub App or token authentication, REST calls through go-github,
// conditional requests with ETags, rate-limit observation and error classification. It returns snapshot value types.
package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v92/github"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

const (
	requestTimeout = 30 * time.Second
	maxPages       = 100
	perPage        = 100
	inFlight       = 4 // requests at once per source
	etagEntries    = 5000
	userAgent      = "source-code-portal"
)

// Client holds one Source per configured source.
type Client struct {
	sources map[string]*Source
}

// New builds a client for the configured sources. base is the HTTP transport underneath (nil: a cloned default transport).
func New(cfg config.GitHub, base http.RoundTripper, log *slog.Logger) (*Client, error) {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	c := &Client{sources: map[string]*Source{}}
	for _, sc := range cfg.Sources {
		s, err := newSource(sc, base, log.With("source", sc.ID))
		if err != nil {
			return nil, fmt.Errorf("github source %s: %w", sc.ID, err)
		}
		c.sources[sc.ID] = s
	}
	return c, nil
}

// Source returns the source with id, or nil.
func (c *Client) Source(id string) *Source { return c.sources[id] }

// Observe sets fn to receive the HTTP status of every GitHub response (304 included). Call it before the first request.
func (c *Client) Observe(fn func(source string, status int)) {
	for _, s := range c.sources {
		s.observe = fn
	}
}

// RateRemaining returns, per source, the smallest fraction of the core rate limit left over its credentials (1 when unknown).
func (c *Client) RateRemaining() map[string]float64 {
	out := map[string]float64{}
	for id, s := range c.sources {
		s.mu.Lock()
		least := 1.0
		creds := []*credential{s.token}
		for _, cr := range s.owners {
			creds = append(creds, cr)
		}
		for _, cr := range creds {
			if cr != nil {
				least = min(least, cr.rate.fraction())
			}
		}
		s.mu.Unlock()
		out[id] = least
	}
	return out
}

// Remaining reports the fraction of the core rate limit left for owner's credential and when it resets. Unknown limits report 1.
func (c *Client) Remaining(source, owner string) (float64, time.Time) {
	s := c.sources[source]
	if s == nil {
		return 1, time.Time{}
	}
	cred := s.credentialFor(owner)
	if cred == nil {
		return 1, time.Time{}
	}
	return cred.rate.remaining()
}

// Source is one GitHub host with one credential.
type Source struct {
	id    string
	cfg   config.Source
	log   *slog.Logger
	cache *etagCache
	sem   chan struct{}
	base  http.RoundTripper

	app *ghinstallation.AppsTransport // nil in token mode
	// appClient lists installations with the App's JWT.
	appClient *gh.Client

	observe func(source string, status int) // set before the first request

	mu       sync.Mutex
	token    *credential            // token mode
	owners   map[string]*credential // App mode: lower-case owner login -> installation
	accounts map[string]account     // token mode: lower-case owner login -> what it is, looked up once
}

// credential is one rate bucket: a token, or one App installation.
type credential struct {
	client *gh.Client
	rate   *rateBook
	user   bool // App mode: the installation is on a user account
}

// account is what listing an owner's repositories needs to know about it.
type account struct {
	user bool // a user account, not an organisation
	self bool // token mode: the token's own account, whose private repositories it lists
}

func newSource(sc config.Source, base http.RoundTripper, log *slog.Logger) (*Source, error) {
	s := &Source{id: sc.ID, cfg: sc, log: log, cache: newETagCache(etagEntries), sem: make(chan struct{}, inFlight), base: base,
		owners: map[string]*credential{}, accounts: map[string]account{}}
	if sc.App == nil {
		rate := &rateBook{}
		client, err := s.newClient(s.transport(rate), sc.Token.Reveal())
		if err != nil {
			return nil, err
		}
		s.token = &credential{client: client, rate: rate}
		return s, nil
	}
	atr, err := ghinstallation.NewAppsTransport(s.transport(&rateBook{}), sc.App.ID, sc.App.PrivateKey.Reveal())
	if err != nil {
		return nil, fmt.Errorf("app private key: %w", err)
	}
	atr.BaseURL = strings.TrimSuffix(sc.BaseURL, "/")
	s.app = atr
	if s.appClient, err = s.newClient(atr, nil); err != nil {
		return nil, err
	}
	return s, nil
}

// transport is the chain below authentication: request headers, conditional cache, rate observer, concurrency limit.
func (s *Source) transport(rate *rateBook) http.RoundTripper {
	return &scpTransport{base: s.base, cache: s.cache, rate: rate, sem: s.sem, src: s}
}

func (s *Source) newClient(rt http.RoundTripper, token []byte) (*gh.Client, error) {
	opts := []gh.ClientOptionsFunc{
		gh.WithHTTPClient(&http.Client{Transport: rt, Timeout: requestTimeout}),
		gh.WithUserAgent(userAgent),
	}
	if token != nil {
		opts = append(opts, gh.WithAuthToken(string(token)))
	}
	if s.cfg.BaseURL != config.DefaultBaseURL {
		opts = append(opts, gh.WithURLs(&s.cfg.BaseURL, &s.cfg.BaseURL))
	}
	return gh.NewClient(opts...)
}

func (s *Source) credentialFor(owner string) *credential {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != nil {
		return s.token
	}
	return s.owners[strings.ToLower(owner)]
}

// RefreshInstallations lists the App's installations and keeps those of the configured organisations (filtered by
// app.installations when set). It does nothing in token mode.
func (s *Source) RefreshInstallations(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, a := range s.cfg.App.Installations {
		allowed[strings.ToLower(a)] = true
	}
	var installs []*gh.Installation
	opts := &gh.ListOptions{PerPage: perPage}
	for page := 0; page < maxPages; page++ {
		got, resp, err := s.appClient.Apps.ListInstallations(ctx, opts)
		if err != nil {
			return classify(resp, err)
		}
		installs = append(installs, got...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	owners := map[string]*credential{}
	for _, in := range installs {
		login := strings.ToLower(in.GetAccount().GetLogin())
		if login == "" || in.SuspendedAt != nil || len(allowed) > 0 && !allowed[login] {
			continue
		}
		if prev := s.owners[login]; prev != nil {
			owners[login] = prev // keep the token and rate state
			continue
		}
		// Each installation has its own token and rate bucket, so its transport chain carries its own rate book.
		rate := &rateBook{}
		atr, err := ghinstallation.NewAppsTransport(s.transport(rate), s.cfg.App.ID, s.cfg.App.PrivateKey.Reveal())
		if err != nil {
			return err
		}
		atr.BaseURL = s.app.BaseURL
		itr := ghinstallation.NewFromAppsTransport(atr, in.GetID())
		client, err := s.newClient(itr, nil)
		if err != nil {
			return err
		}
		owners[login] = &credential{client: client, rate: rate, user: in.GetAccount().GetType() == "User"}
	}
	s.owners = owners
	return nil
}

// REST returns the go-github client for owner's credential, for the calls only one feature makes; classify their failures with
// Classify.
func (s *Source) REST(ctx context.Context, owner string) (*gh.Client, error) {
	return s.client(ctx, owner)
}

// client returns the client for owner, listing installations first when owner has none yet.
func (s *Source) client(ctx context.Context, owner string) (*gh.Client, error) {
	if c := s.credentialFor(owner); c != nil {
		return c.client, nil
	}
	if err := s.RefreshInstallations(ctx); err != nil {
		return nil, err
	}
	if c := s.credentialFor(owner); c != nil {
		return c.client, nil
	}
	return nil, &Error{Kind: Permanent, Code: "no_installation", Err: fmt.Errorf("the GitHub App is not installed on %s", owner)}
}

// ListRepos returns every repository of owner, an organisation or a user account, most recently pushed first (an App's user
// installation lists in GitHub's order). Any failed page fails the listing.
func (s *Source) ListRepos(ctx context.Context, owner string) ([]snapshot.Repo, error) {
	c, err := s.client(ctx, owner)
	if err != nil {
		return nil, err
	}
	acct, err := s.account(ctx, c, owner)
	if err != nil {
		return nil, err
	}
	list := lister(c, owner, acct, s.token == nil)
	seen := map[int64]bool{}
	var out []snapshot.Repo
	for page := 0; ; page++ {
		if page == maxPages {
			return nil, &Error{Kind: Permanent, Code: "too_many_pages", Err: fmt.Errorf("%s has more than %d pages of repositories", owner, maxPages)}
		}
		got, resp, err := list(ctx, gh.ListOptions{PerPage: perPage, Page: page + 1})
		if err != nil {
			return nil, classify(resp, err)
		}
		for _, r := range got {
			if seen[r.GetID()] {
				continue // moved to a later page while listing
			}
			seen[r.GetID()] = true
			out = append(out, s.convert(r))
		}
		if resp.NextPage == 0 {
			return out, nil
		}
	}
}

type listPage func(ctx context.Context, page gh.ListOptions) ([]*gh.Repository, *gh.Response, error)

// lister picks the listing for owner: the organisation's repositories, the repositories of an App's user installation, every
// repository the token's own account owns, or another user's public repositories.
func lister(c *gh.Client, owner string, acct account, app bool) listPage {
	switch {
	case !acct.user:
		return func(ctx context.Context, p gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
			return c.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{Type: "all", Sort: "pushed", Direction: "desc", ListOptions: p})
		}
	case app:
		return func(ctx context.Context, p gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
			got, resp, err := c.Apps.ListRepos(ctx, &p)
			return got.GetRepositories(), resp, err
		}
	case acct.self:
		return func(ctx context.Context, p gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
			return c.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{Affiliation: "owner",
				Visibility: "all", Sort: "pushed", Direction: "desc", ListOptions: p})
		}
	default:
		return func(ctx context.Context, p gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
			return c.Repositories.ListByUser(ctx, owner, &gh.RepositoryListByUserOptions{Type: "owner", Sort: "pushed", Direction: "desc", ListOptions: p})
		}
	}
}

// account says what owner is: from its installation in App mode, looked up once (the owner, then the token's own login) in token mode.
func (s *Source) account(ctx context.Context, c *gh.Client, owner string) (account, error) {
	key := strings.ToLower(owner)
	s.mu.Lock()
	if s.token == nil {
		defer s.mu.Unlock()
		return account{user: s.owners[key] != nil && s.owners[key].user}, nil
	}
	a, ok := s.accounts[key]
	s.mu.Unlock()
	if ok {
		return a, nil
	}
	u, resp, err := c.Users.Get(ctx, owner)
	if err != nil {
		return account{}, classify(resp, err)
	}
	if a.user = u.GetType() == "User"; a.user {
		me, resp, err := c.Users.Get(ctx, "")
		if err != nil {
			return account{}, classify(resp, err)
		}
		a.self = strings.EqualFold(me.GetLogin(), owner)
	}
	s.mu.Lock()
	s.accounts[key] = a
	s.mu.Unlock()
	return a, nil
}

// Repo returns one repository.
func (s *Source) Repo(ctx context.Context, owner, name string) (snapshot.Repo, error) {
	c, err := s.client(ctx, owner)
	if err != nil {
		return snapshot.Repo{}, err
	}
	r, resp, err := c.Repositories.Get(ctx, owner, name)
	if err != nil {
		return snapshot.Repo{}, classify(resp, err)
	}
	return s.convert(r), nil
}

// TreeEntry is one entry of a git tree.
type TreeEntry struct {
	Path string // relative to the tree
	Type string // blob | tree | commit
	SHA  string
	Size int64 // blobs only
}

// Tree returns the entries of one tree; recursive includes every entry below it, with paths relative to it. ref is a tree SHA or a
// branch name; "HEAD" is the default branch.
func (s *Source) Tree(ctx context.Context, owner, name, ref string, recursive bool) ([]TreeEntry, error) {
	c, err := s.client(ctx, owner)
	if err != nil {
		return nil, err
	}
	t, resp, err := c.Git.GetTree(ctx, owner, name, ref, recursive)
	if err != nil {
		return nil, classify(resp, err)
	}
	out := make([]TreeEntry, 0, len(t.Entries))
	for _, e := range t.Entries {
		out = append(out, TreeEntry{Path: e.GetPath(), Type: e.GetType(), SHA: e.GetSHA(), Size: int64(e.GetSize())})
	}
	return out, nil
}

// WebURL is the web host of the source with a trailing slash: https://github.com/ for github.com, the GHES host otherwise.
func (s *Source) WebURL() string {
	if u, err := url.Parse(s.cfg.BaseURL); err == nil && u.Host != "api.github.com" {
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/v3") + "/"
		return u.String()
	}
	return "https://github.com/"
}

// Blob returns the content of a blob.
func (s *Source) Blob(ctx context.Context, owner, name, sha string) ([]byte, error) {
	c, err := s.client(ctx, owner)
	if err != nil {
		return nil, err
	}
	b, resp, err := c.Git.GetBlobRaw(ctx, owner, name, sha)
	if err != nil {
		return nil, classify(resp, err)
	}
	return b, nil
}

func (s *Source) convert(r *gh.Repository) snapshot.Repo {
	return snapshot.Repo{
		ID:            snapshot.RepoID{Source: s.id, GitHubID: r.GetID()},
		Owner:         r.GetOwner().GetLogin(),
		Name:          r.GetName(),
		Description:   r.GetDescription(),
		HTMLURL:       r.GetHTMLURL(),
		Homepage:      r.GetHomepage(),
		DefaultBranch: r.GetDefaultBranch(),
		License:       r.GetLicense().GetSPDXID(),
		Visibility:    r.GetVisibility(),
		Topics:        r.Topics,
		Archived:      r.GetArchived(),
		Fork:          r.GetFork(),
		Stars:         r.GetStargazersCount(),
		OpenIssues:    r.GetOpenIssuesCount(),
		PushedAt:      r.GetPushedAt().UTC(),
	}
}

// Kind classifies a failed call by what the caller should do about it.
type Kind uint8

const (
	Transient   Kind = iota + 1 // 5xx, timeout, connection reset: retry with backoff
	RateLimited                 // wait until RetryAt
	Auth                        // the credential was refused
	NotFound                    // missing or not visible
	Permanent                   // will not succeed by retrying soon
)

// Error is a classified GitHub failure.
type Error struct {
	Kind    Kind
	RetryAt time.Time // RateLimited: from Retry-After or the rate reset; zero when absent
	Code    string
	Err     error
}

func (e *Error) Error() string { return fmt.Sprintf("github %s: %v", e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// RateLimitedUntil, AuthFailed, Permanent, IsNotFound and ErrorCode let callers classify without importing this package.

func (e *Error) RateLimitedUntil() (time.Time, bool) { return e.RetryAt, e.Kind == RateLimited }
func (e *Error) AuthFailed() bool                    { return e.Kind == Auth }
func (e *Error) Permanent() bool                     { return e.Kind == Permanent }
func (e *Error) IsNotFound() bool                    { return e.Kind == NotFound }
func (e *Error) ErrorCode() string                   { return e.Code }

// IsNotFound reports whether err is a NotFound classification.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == NotFound
}

// Classify turns the failure of a go-github call into an *Error (a cancelled context stays as it is).
func Classify(resp *gh.Response, err error) error { return classify(resp, err) }

func classify(resp *gh.Response, err error) error {
	var rl *gh.RateLimitError
	if errors.As(err, &rl) {
		return &Error{Kind: RateLimited, RetryAt: rl.Rate.Reset.Time, Code: "rate_limited", Err: err}
	}
	var abuse *gh.AbuseRateLimitError
	if errors.As(err, &abuse) {
		e := &Error{Kind: RateLimited, Code: "secondary_rate_limit", Err: err}
		if abuse.RetryAfter != nil {
			e.RetryAt = time.Now().Add(*abuse.RetryAfter)
		}
		return e
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	status := 0
	if resp != nil && resp.Response != nil {
		status = resp.StatusCode
	}
	switch {
	case status == http.StatusNotFound:
		return &Error{Kind: NotFound, Code: "not_found", Err: err}
	case status == http.StatusConflict:
		return &Error{Kind: NotFound, Code: "empty", Err: err}
	case status == http.StatusUnauthorized:
		return &Error{Kind: Auth, Code: "unauthorized", Err: err}
	case status == http.StatusForbidden || status == http.StatusUnprocessableEntity:
		return &Error{Kind: Permanent, Code: "forbidden", Err: err}
	case status >= 500 || status == 0:
		return &Error{Kind: Transient, Code: "transient", Err: err}
	case status >= 400:
		return &Error{Kind: Permanent, Code: fmt.Sprintf("http_%d", status), Err: err}
	}
	return &Error{Kind: Permanent, Code: "malformed", Err: err}
}
