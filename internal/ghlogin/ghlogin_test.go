package ghlogin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestParseHost(t *testing.T) {
	for in, want := range map[string]Host{
		"":                             {"https://github.com", "https://api.github.com/"},
		"github.com":                   {"https://github.com", "https://api.github.com/"},
		"ghe.example.org":              {"https://ghe.example.org", "https://ghe.example.org/api/v3/"},
		"https://ghe.example.org/":     {"https://ghe.example.org", "https://ghe.example.org/api/v3/"},
		"https://ghe.example.org:8443": {"https://ghe.example.org:8443", "https://ghe.example.org:8443/api/v3/"},
		"https://acme.ghe.com":         {"https://acme.ghe.com", "https://api.acme.ghe.com/"},
		"http://127.0.0.1:8080":        {"http://127.0.0.1:8080", "http://127.0.0.1:8080/api/v3/"},
		"http://localhost:1":           {"http://localhost:1", "http://localhost:1/api/v3/"},
	} {
		got, err := ParseHost(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v, want %+v", in, got, err, want)
		}
	}
	if h, err := ParseHost("  ghe.example.org \n"); err != nil || h.API != "https://ghe.example.org/api/v3/" {
		t.Errorf("whitespace around a host: %+v %v", h, err)
	}
	for _, in := range []string{"http://ghe.example.org", "https://u:p@ghe.example.org", "https://ghe.example.org/path", "https://ghe.example.org?x=1",
		"ftp://ghe.example.org", "https://", "https://a b"} {
		if h, err := ParseHost(in); err == nil {
			t.Errorf("%q accepted: %+v", in, h)
		}
	}
}

type fakeHost struct {
	*httptest.Server
	mu        sync.Mutex
	answers   []string // the access_token endpoint's JSON answers, in order
	polls     int
	scope     string
	pollAuth  string // the Authorization header of the last poll
	pollBody  url.Values
	handler   http.Handler
	expiry    string
	orgPages  int
	repoPages int // pages of 100 repositories the listing has; 0: three repositories on one page
}

func newFake(t *testing.T) *fakeHost {
	t.Helper()
	f := &fakeHost{}
	f.routes()
	f.Server = httptest.NewServer(f.handler)
	t.Cleanup(f.Close)
	return f
}

// memory serves a handler without a socket, so a test of the device flow can run on fake time in a synctest bubble.
type memory struct{ h http.Handler }

func (m memory) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, r)
	res := rec.Result()
	res.Request = r
	return res, nil
}

func (f *fakeHost) memClient() *Client {
	h, _ := ParseHost("http://127.0.0.1:1")
	return &Client{Host: h, HTTP: &http.Client{Transport: memory{f.handler}}}
}

func (f *fakeHost) routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = r.ParseForm()
		f.mu.Lock()
		f.scope = r.PostForm.Get("scope")
		f.mu.Unlock()
		switch r.PostForm.Get("client_id") {
		case "bad": // what github.com answers for a client id it does not know (observed): 404 and this body
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"Not Found"}`)
			return
		case "disabled": // GitHub's page lists these errors without saying their status; a success status is the worst case for a client
			fmt.Fprint(w, `{"error":"device_flow_disabled","error_description":"Device Flow must be explicitly enabled for this App."}`)
			return
		}
		fmt.Fprint(w, `{"device_code":"dev123","user_code":"WDJB-MJHT","verification_uri":"https://example/login/device","expires_in":900,"interval":5}`)
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.pollAuth, f.pollBody = r.Header.Get("Authorization"), r.PostForm
		if r.PostForm.Get("device_code") != "dev123" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a := f.answers[min(f.polls, len(f.answers)-1)]
		f.polls++
		fmt.Fprint(w, a)
	})
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.expiry != "" {
			w.Header().Set("github-authentication-token-expiration", f.expiry)
		}
		fmt.Fprint(w, `{"login":"octo","public_repos":2,"owned_private_repos":1,"total_private_repos":5}`)
	})
	mux.HandleFunc("GET /api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("sort") != "pushed" || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if q.Get("per_page") == "1" {
			fmt.Fprint(w, `[{"name":"newest"}]`)
			return
		}
		if q.Get("per_page") != "100" || q.Get("affiliation") != "owner" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.listRepos(w, q.Get("page"), "octo")
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" || r.PathValue("org") != "acme" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"login":"acme","public_repos":3,"total_private_repos":4}`)
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}/repos", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Header.Get("Authorization") != "Bearer good" || q.Get("type") != "all" || q.Get("sort") != "pushed" || q.Get("per_page") != "100" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.listRepos(w, q.Get("page"), r.PathValue("org"))
	})
	mux.HandleFunc("GET /api/v3/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		page := r.URL.Query().Get("page")
		var parts []string
		switch {
		case f.orgPages > 1 && page == "1":
			for i := range 100 {
				parts = append(parts, fmt.Sprintf(`{"login":"org%d"}`, i))
			}
		case f.orgPages > 1:
			parts = []string{`{"login":"last"}`}
		default:
			parts = []string{`{"login":"acme"}`, `{"login":"zeta"}`}
		}
		fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
	})
	f.handler = mux
}

// listRepos answers a repository listing: three repositories on one page, or (repoPages > 0) a full page of 100 on every page.
func (f *fakeHost) listRepos(w http.ResponseWriter, page, owner string) {
	if f.repoPages > 0 {
		var parts []string
		for i := range 100 {
			parts = append(parts, fmt.Sprintf(`{"name":"r%s-%d","owner":{"login":%q}}`, page, i, owner))
		}
		fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
		return
	}
	fmt.Fprintf(w, `[{"name":"api","private":true,"pushed_at":"2026-11-01T10:00:00Z","topics":["kcp","go"],"owner":{"login":%q}},`+
		`{"name":"old","fork":true,"archived":true,"owner":{"login":%q}},{"name":"","owner":{"login":"x"}},{"name":"nobody"}]`, owner, owner)
}

func (f *fakeHost) client(t *testing.T) *Client {
	h, err := ParseHost(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return &Client{Host: h, Now: func() time.Time { return now }}
}

// The device flow runs on fake time: x/oauth2 waits with real timers, so each case is a synctest bubble over an in-memory host.
func TestDeviceFlow(t *testing.T) {
	ctx := context.Background()
	run := func(name string, answers []string, fn func(t *testing.T, f *fakeHost, c *Client)) {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &fakeHost{answers: answers}
				f.routes()
				fn(t, f, f.memClient())
			})
		})
	}
	run("pending, slow down, then a token that expires", []string{`{"error":"authorization_pending"}`, `{"error":"slow_down","interval":10}`,
		`{"error":"authorization_pending"}`, `{"access_token":"good","scope":"repo,read:org","token_type":"bearer","expires_in":28800}`},
		func(t *testing.T, f *fakeHost, c *Client) {
			code, err := c.StartDevice(ctx, "Iv1.abc", "repo read:org")
			if err != nil || code.UserCode != "WDJB-MJHT" || code.Interval != 5*time.Second || f.scope != "repo read:org" {
				t.Fatalf("%+v %v scope=%q", code, err, f.scope)
			}
			start := time.Now()
			tok, err := c.WaitDevice(ctx, "Iv1.abc", code)
			if err != nil || tok.Value != "good" || tok.Scopes != "repo,read:org" {
				t.Fatalf("%+v %v", tok, err)
			}
			if want := time.Now().Add(28800 * time.Second); !tok.Expires.Equal(want) {
				t.Errorf("expiry %v, want %v", tok.Expires, want)
			}
			if got := time.Since(start); got != 30*time.Second { // 5, 5 (told to slow down: 10), 10, 10
				t.Errorf("waited %v", got)
			}
			if f.pollAuth != "" || f.pollBody.Get("client_id") != "Iv1.abc" || f.pollBody.Get("scope") != "" || f.pollBody.Get("client_secret") != "" {
				t.Errorf("the poll: authorization %q body %v", f.pollAuth, f.pollBody)
			}
		})
	run("a token that does not expire", []string{`{"access_token":"good","scope":""}`}, func(t *testing.T, f *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		tok, err := c.WaitDevice(ctx, "id", code)
		if err != nil || !tok.Expires.IsZero() {
			t.Errorf("%+v %v", tok, err)
		}
		if f.scope != "" {
			t.Errorf("a scope was sent: %q", f.scope)
		}
	})
	run("denied", []string{`{"error":"access_denied"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
	})
	run("expired by the host", []string{`{"error":"expired_token"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("host: %v", err)
		}
	})
	run("expired by the clock", []string{`{"error":"authorization_pending"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		code.Expires = time.Now().Add(7 * time.Second) // polls at 5 s, and the next would come at 10 s
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("clock: %v", err)
		}
		code.Expires = time.Now().Add(-time.Second)
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("a code that has ended: %v", err)
		}
	})
	run("a refused client id and an unknown error", []string{`{"error":"unsupported_grant_type","error_description":"no"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		if _, err := c.StartDevice(ctx, "bad", ""); err == nil || !strings.Contains(err.Error(), "does not know this client id") {
			t.Errorf("an unknown client id: %v", err)
		}
		if _, err := c.StartDevice(ctx, "disabled", ""); err == nil || !strings.Contains(err.Error(), "device_flow_disabled") || !strings.Contains(err.Error(), "explicitly enabled") {
			t.Errorf("a refusal sent with a success status must reach the person: %v", err)
		}
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); err == nil || !strings.Contains(err.Error(), "unsupported_grant_type") {
			t.Errorf("%v", err)
		}
	})
	run("expired, spelled the other way", []string{`{"error":"token_expired"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("%v", err)
		}
	})
	run("a cancelled context stops the wait", []string{`{"error":"authorization_pending"}`}, func(t *testing.T, _ *fakeHost, c *Client) {
		code, _ := c.StartDevice(ctx, "id", "")
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(7*time.Second, cancel)
		if _, err := c.WaitDevice(cctx, "id", code); !errors.Is(err, context.Canceled) {
			t.Errorf("%v", err)
		}
	})
	run("an answer that is not a code", nil, func(t *testing.T, f *fakeHost, c *Client) {
		f.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
		})
		c.HTTP = &http.Client{Transport: memory{f.handler}}
		if _, err := c.StartDevice(ctx, "id", ""); err == nil || !strings.Contains(err.Error(), "no device code") {
			t.Errorf("%v", err)
		}
	})
}

func TestWhoamiAndOrgs(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	c := f.client(t)
	if _, err := c.Whoami(ctx, "wrong"); !errors.Is(err, ErrBadToken) {
		t.Errorf("a wrong token: %v", err)
	}
	u, err := c.Whoami(ctx, "good")
	if err != nil || u.Login != "octo" || !u.Expires.IsZero() {
		t.Errorf("%+v %v", u, err)
	}
	f.expiry = "2026-12-01 10:00:00 UTC"
	if u, _ = c.Whoami(ctx, "good"); !u.Expires.Equal(time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("the token's end: %v", u.Expires)
	}
	if name, err := c.Newest(ctx, "good"); err != nil || name != "newest" {
		t.Errorf("newest: %q %v", name, err)
	}
	orgs, err := c.Orgs(ctx, "good")
	if err != nil || !slices.Equal(orgs, []string{"acme", "zeta"}) {
		t.Errorf("%v %v", orgs, err)
	}
	f.orgPages = 2
	if orgs, err = c.Orgs(ctx, "good"); err != nil || len(orgs) != 101 || orgs[100] != "last" {
		t.Errorf("pages: %d %v", len(orgs), err)
	}
	if _, err := c.Orgs(ctx, "wrong"); !errors.Is(err, ErrBadToken) {
		t.Errorf("%v", err)
	}
}

func TestAccountRepos(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	c := f.client(t)
	if n, err := c.AccountSize(ctx, "good", "octo", true); err != nil || n != 3 {
		t.Errorf("the person's own size (public and owned private): %d %v", n, err)
	}
	if n, err := c.AccountSize(ctx, "good", "acme", false); err != nil || n != 7 {
		t.Errorf("an organisation's size: %d %v", n, err)
	}
	if _, err := c.AccountSize(ctx, "wrong", "acme", false); err == nil {
		t.Error("a wrong token got a size")
	}
	var seen []int
	repos, more, err := c.AccountRepos(ctx, "good", "acme", false, func(n int) { seen = append(seen, n) })
	if err != nil || more || len(repos) != 2 || !slices.Equal(seen, []int{2}) {
		t.Fatalf("%+v %v %v %v", repos, more, seen, err)
	}
	if r := repos[0]; r.Owner != "acme" || r.Name != "api" || !r.Private || r.Fork || !slices.Equal(r.Topics, []string{"kcp", "go"}) ||
		!r.Pushed.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("first: %+v", r)
	}
	if r := repos[1]; !r.Fork || !r.Archived || !r.Pushed.IsZero() {
		t.Errorf("second: %+v", r)
	}
	if repos, _, err = c.AccountRepos(ctx, "good", "octo", true, nil); err != nil || len(repos) != 2 || repos[0].Owner != "octo" {
		t.Errorf("the person's own: %+v %v", repos, err)
	}
	f.repoPages = 99
	seen = nil
	repos, more, err = c.AccountRepos(ctx, "good", "acme", false, func(n int) { seen = append(seen, n) })
	if err != nil || !more || len(repos) != MaxPerAccount || len(seen) != MaxPerAccount/100 || seen[0] != 100 {
		t.Errorf("capped: %d %v %v %d", len(repos), more, err, len(seen))
	}
	if _, _, err := c.AccountRepos(ctx, "wrong", "acme", false, nil); err == nil {
		t.Error("a wrong token listed repositories")
	}
}
