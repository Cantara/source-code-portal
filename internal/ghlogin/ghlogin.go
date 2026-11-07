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
	"strconv"
	"strings"
	"time"
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

func (c *Client) get(ctx context.Context, token, path string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Host.API+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, ErrBadToken
	default:
		return nil, fmt.Errorf("%s answered %d", path, res.StatusCode)
	}
	return res.Header, json.Unmarshal(body, out)
}

// ErrBadToken is returned when the host does not accept the token.
var ErrBadToken = errors.New("ghlogin: the host does not accept this token")

// Whoami asks the host who the token is (GET /user).
func (c *Client) Whoami(ctx context.Context, token string) (User, error) {
	var u struct {
		Login string `json:"login"`
	}
	h, err := c.get(ctx, token, "user", &u)
	if err != nil {
		return User{}, err
	}
	if u.Login == "" {
		return User{}, errors.New("ghlogin: the host named no user")
	}
	out := User{Login: u.Login}
	if e := h.Get("github-authentication-token-expiration"); e != "" { // "2026-12-01 10:00:00 UTC"
		if t, err := time.Parse("2006-01-02 15:04:05 MST", e); err == nil {
			out.Expires = t.UTC()
		}
	}
	return out, nil
}

// Orgs lists the organisations the token reaches (GET /user/orgs, up to 5 pages of 100), by login.
func (c *Client) Orgs(ctx context.Context, token string) ([]string, error) {
	var logins []string
	for page := 1; page <= 5; page++ {
		var r []struct {
			Login string `json:"login"`
		}
		if _, err := c.get(ctx, token, "user/orgs?per_page=100&page="+strconv.Itoa(page), &r); err != nil {
			return nil, err
		}
		for _, o := range r {
			logins = append(logins, o.Login)
		}
		if len(r) < 100 {
			break
		}
	}
	return logins, nil
}

// Newest names the repository the token's account pushed to last (GET /user/repos), "" when there is none.
func (c *Client) Newest(ctx context.Context, token string) (string, error) {
	var r []struct {
		Name string `json:"name"`
	}
	if _, err := c.get(ctx, token, "user/repos?sort=pushed&per_page=1", &r); err != nil || len(r) == 0 {
		return "", err
	}
	return r[0].Name, nil
}
