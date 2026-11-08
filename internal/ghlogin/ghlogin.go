// Package ghlogin signs a person in to a GitHub host for the personal edition (design/personal.md): the host's forms (github.com, a
// GitHub Enterprise Server, a GHE.com tenant), the device flow, and the two read-only calls that say who the token is and which
// organisations it reaches. It only reads.
package ghlogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"
)

// Host is a GitHub host: where people sign in (Web) and where the REST API is (API, with a trailing slash, as github.sources[].base_url).
type Host struct {
	Web string
	API string
}

// ParseHost reads what a person types for a host: "" or "github.com", "https://ghe.example.org" (a GitHub Enterprise Server: REST at
// /api/v3/), or "https://name.ghe.com" (a GHE.com tenant: REST at api.name.ghe.com). http is accepted for loopback only.
func ParseHost(s string) (Host, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "github.com" || s == "https://github.com" {
		return Host{Web: "https://github.com", API: "https://api.github.com/"}, nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return Host{}, fmt.Errorf("%q is not a host: write github.com or https://host", s)
	}
	host := u.Hostname()
	if u.Scheme != "https" && (u.Scheme != "http" || !loopback(host)) {
		return Host{}, fmt.Errorf("%q: a host must be https (http only on loopback)", s)
	}
	web := u.Scheme + "://" + u.Host
	if name, ok := strings.CutSuffix(host, ".ghe.com"); ok && !strings.Contains(name, ".") && name != "" && name != "api" {
		return Host{Web: web, API: "https://api." + host + "/"}, nil
	}
	return Host{Web: web, API: web + "/api/v3/"}, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// Client talks to a host. HTTP and Sleep are replaced in tests.
type Client struct {
	Host  Host
	HTTP  *http.Client
	Sleep func(context.Context, time.Duration) error
	Now   func() time.Time
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Code is what the device flow shows the person: open VerificationURI and enter UserCode.
type Code struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	Expires         time.Time
}

// Token is what signing in yields. Expires is zero for a token that does not expire.
type Token struct {
	Value   string
	Scopes  string
	Expires time.Time
}

// Errors of the device flow the caller words for the person.
var (
	ErrDenied  = errors.New("ghlogin: the sign-in was cancelled on GitHub")
	ErrExpired = errors.New("ghlogin: the code ran out before the sign-in was confirmed")
)

func (c *Client) form(ctx context.Context, path string, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host.Web+path, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", c.Host.Web+path, res.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// StartDevice asks the host for a device code for the OAuth app or GitHub App clientID with scope (space-separated; empty for a GitHub
// App, whose permissions are its own).
func (c *Client) StartDevice(ctx context.Context, clientID, scope string) (Code, error) {
	var r struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		Description     string `json:"error_description"`
	}
	v := url.Values{"client_id": {clientID}}
	if scope != "" {
		v.Set("scope", scope)
	}
	if err := c.form(ctx, "/login/device/code", v, &r); err != nil {
		return Code{}, err
	}
	if r.Error != "" {
		return Code{}, fmt.Errorf("%s: %s", r.Error, r.Description)
	}
	if r.DeviceCode == "" || r.UserCode == "" || r.VerificationURI == "" {
		return Code{}, errors.New("ghlogin: the host sent no device code")
	}
	if u, err := url.Parse(r.VerificationURI); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Code{}, errors.New("ghlogin: the host sent a verification address that is not a web address")
	}
	if r.Interval < 1 {
		r.Interval = 5
	}
	if r.ExpiresIn < 1 {
		r.ExpiresIn = 900
	}
	return Code{DeviceCode: r.DeviceCode, UserCode: r.UserCode, VerificationURI: r.VerificationURI,
		Interval: time.Duration(r.Interval) * time.Second, Expires: c.now().Add(time.Duration(r.ExpiresIn) * time.Second)}, nil
}

// WaitDevice polls until the person confirms, denies or the code ends. It waits the host's interval and slows down when told to.
func (c *Client) WaitDevice(ctx context.Context, clientID string, code Code) (Token, error) {
	interval := code.Interval
	for {
		if err := c.sleep(ctx, interval); err != nil {
			return Token{}, err
		}
		if !c.now().Before(code.Expires) {
			return Token{}, ErrExpired
		}
		var r struct {
			AccessToken string `json:"access_token"`
			Scope       string `json:"scope"`
			ExpiresIn   int    `json:"expires_in"`
			Error       string `json:"error"`
			Description string `json:"error_description"`
			Interval    int    `json:"interval"`
		}
		err := c.form(ctx, "/login/oauth/access_token", url.Values{"client_id": {clientID}, "device_code": {code.DeviceCode},
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &r)
		if err != nil {
			return Token{}, err
		}
		switch r.Error {
		case "":
			if r.AccessToken == "" {
				return Token{}, errors.New("ghlogin: the host sent no token")
			}
			t := Token{Value: r.AccessToken, Scopes: r.Scope}
			if r.ExpiresIn > 0 {
				t.Expires = c.now().Add(time.Duration(r.ExpiresIn) * time.Second)
			}
			return t, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
			if r.Interval > 0 {
				interval = time.Duration(r.Interval) * time.Second
			}
		case "access_denied":
			return Token{}, ErrDenied
		case "expired_token":
			return Token{}, ErrExpired
		default:
			return Token{}, fmt.Errorf("%s: %s", r.Error, r.Description)
		}
	}
}

// User is who a token is.
type User struct {
	Login   string
	Expires time.Time // when the token ends, if the host says (fine-grained tokens do)
}

// ErrBadToken is returned when the host does not accept the token.
var ErrBadToken = errors.New("ghlogin: the host does not accept this token")

const (
	userAgent    = "source-code-portal-setup"
	maxAnswer    = 4 << 20 // the most the host may send in one answer
	maxOrgPages  = 5
	pageSize     = 100
	tokenEndFmt  = "2006-01-02 15:04:05 MST" // the header github-authentication-token-expiration: "2026-12-01 10:00:00 UTC"
	tokenEndName = "github-authentication-token-expiration"
)

var errTooLarge = errors.New("ghlogin: the host's answer is larger than the setup reads")

// capped bounds what a host (one the person typed in) can make the setup read in one answer; go-github does not.
type capped struct{ base http.RoundTripper }

func (c capped) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := c.base.RoundTrip(r)
	if err == nil {
		res.Body = &limited{ReadCloser: res.Body, left: maxAnswer}
	}
	return res, err
}

type limited struct {
	io.ReadCloser
	left int64
}

func (l *limited) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.ReadCloser.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, errTooLarge
	}
	return n, err
}

// base is the transport every call goes through: the one the caller gave, else the default.
func (c *Client) base() http.RoundTripper {
	if t := c.http().Transport; t != nil {
		return t
	}
	return http.DefaultTransport
}

// api is go-github's client for the host, over rt (the base transport, or an App's), with token when there is one. Its answers are bounded.
func (c *Client) api(rt http.RoundTripper, token string) (*gh.Client, error) {
	opts := []gh.ClientOptionsFunc{
		gh.WithHTTPClient(&http.Client{Transport: capped{rt}, Timeout: c.http().Timeout}),
		gh.WithUserAgent(userAgent),
		gh.WithURLs(&c.Host.API, &c.Host.API),
	}
	if token != "" {
		opts = append(opts, gh.WithAuthToken(token))
	}
	return gh.NewClient(opts...)
}

// asToken is the client for a person's token.
func (c *Client) asToken(token string) (*gh.Client, error) { return c.api(c.base(), token) }

// failure turns what go-github returns into this package's errors: a 401 is a token the host does not accept.
func failure(err error) error {
	if er := new(gh.ErrorResponse); errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == http.StatusUnauthorized {
		return ErrBadToken
	}
	return err
}

// Whoami asks the host who the token is (GET /user).
func (c *Client) Whoami(ctx context.Context, token string) (User, error) {
	cl, err := c.asToken(token)
	if err != nil {
		return User{}, err
	}
	u, res, err := cl.Users.Get(ctx, "")
	if err != nil {
		return User{}, failure(err)
	}
	if u.GetLogin() == "" {
		return User{}, errors.New("ghlogin: the host named no user")
	}
	out := User{Login: u.GetLogin()}
	if e := res.Header.Get(tokenEndName); e != "" {
		if t, err := time.Parse(tokenEndFmt, e); err == nil {
			out.Expires = t.UTC()
		}
	}
	return out, nil
}

// Orgs lists the organisations the token reaches (GET /user/orgs, up to 5 pages of 100), by login.
func (c *Client) Orgs(ctx context.Context, token string) ([]string, error) {
	cl, err := c.asToken(token)
	if err != nil {
		return nil, err
	}
	var logins []string
	for page := 1; page <= maxOrgPages; page++ {
		r, _, err := cl.Organizations.List(ctx, "", &gh.ListOptions{PerPage: pageSize, Page: page})
		if err != nil {
			return nil, failure(err)
		}
		for _, o := range r {
			logins = append(logins, o.GetLogin())
		}
		if len(r) < pageSize {
			break
		}
	}
	return logins, nil
}

// Newest names the repository the token's account pushed to last (GET /user/repos), "" when there is none.
func (c *Client) Newest(ctx context.Context, token string) (string, error) {
	cl, err := c.asToken(token)
	if err != nil {
		return "", err
	}
	r, _, err := cl.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{Sort: "pushed", ListOptions: gh.ListOptions{PerPage: 1}})
	if err != nil || len(r) == 0 {
		return "", failure(err)
	}
	return r[0].GetName(), nil
}

// Repo is a repository the token can read, as the setup page lists it.
type Repo struct {
	Owner, Name string
	Private     bool
	Fork        bool
	Archived    bool
	Pushed      time.Time
	Topics      []string
}

// MaxPerAccount is how many repositories AccountRepos reads of one account (20 pages of 100, the most recently pushed first).
const MaxPerAccount = 2000

// AccountSize estimates how many repositories the token will see in an account, from the counts the account's own record carries
// (the public ones and the private ones it owns or, for an organisation, holds); 0 when the host gives no count. self is the
// token's own account (GET /user), else an organisation.
func (c *Client) AccountSize(ctx context.Context, token, account string, self bool) (int, error) {
	cl, err := c.asToken(token)
	if err != nil {
		return 0, err
	}
	var public int
	var private int64
	if self {
		u, _, err := cl.Users.Get(ctx, "")
		if err != nil {
			return 0, failure(err)
		}
		public, private = u.GetPublicRepos(), u.GetOwnedPrivateRepos()
	} else {
		o, _, err := cl.Organizations.Get(ctx, account)
		if err != nil {
			return 0, failure(err)
		}
		public, private = o.GetPublicRepos(), o.GetTotalPrivateRepos()
	}
	return public + int(private), nil
}

// AccountRepos lists the repositories of one account that the token can read, the most recently pushed first, up to
// MaxPerAccount, and tells progress how many it has after each page. self is the token's own account (the repositories it owns:
// GET /user/repos), else an organisation (GET /orgs/{org}/repos). more is true when there were more than the ceiling.
func (c *Client) AccountRepos(ctx context.Context, token, account string, self bool, progress func(n int)) (repos []Repo, more bool, err error) {
	cl, err := c.asToken(token)
	if err != nil {
		return nil, false, err
	}
	for page := 1; page <= MaxPerAccount/pageSize; page++ {
		lo := gh.ListOptions{PerPage: pageSize, Page: page}
		var r []*gh.Repository
		if self {
			r, _, err = cl.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{Affiliation: "owner", Sort: "pushed", ListOptions: lo})
		} else {
			r, _, err = cl.Repositories.ListByOrg(ctx, account, &gh.RepositoryListByOrgOptions{Type: "all", Sort: "pushed", ListOptions: lo})
		}
		if err != nil {
			return nil, false, failure(err)
		}
		for _, x := range r {
			if x.GetName() == "" || x.GetOwner().GetLogin() == "" {
				continue
			}
			repos = append(repos, Repo{Owner: x.GetOwner().GetLogin(), Name: x.GetName(), Private: x.GetPrivate(), Fork: x.GetFork(), Archived: x.GetArchived(),
				Pushed: x.GetPushedAt().Time, Topics: x.Topics})
		}
		if progress != nil {
			progress(len(repos))
		}
		if len(r) < pageSize {
			return repos, false, nil
		}
	}
	return repos, true, nil
}
