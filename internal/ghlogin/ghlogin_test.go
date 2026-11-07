package ghlogin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
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
	expiry    string
	orgPages  int
	repoPages int // pages of 100 repositories the listing has; 0: three repositories on one page
}

func newFake(t *testing.T) *fakeHost {
	t.Helper()
	f := &fakeHost{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.scope = r.PostForm.Get("scope")
		f.mu.Unlock()
		if r.PostForm.Get("client_id") == "bad" {
			fmt.Fprint(w, `{"error":"incorrect_client_credentials","error_description":"The client_id is not valid."}`)
			return
		}
		fmt.Fprint(w, `{"device_code":"dev123","user_code":"WDJB-MJHT","verification_uri":"https://example/login/device","expires_in":900,"interval":5}`)
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
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
		fmt.Fprint(w, `{"login":"octo"}`)
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
		if q.Get("per_page") != "100" || !strings.Contains(q.Get("affiliation"), "organization_member") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if f.repoPages > 0 {
			var parts []string
			for i := range 100 {
				parts = append(parts, fmt.Sprintf(`{"name":"r%s-%d","owner":{"login":"acme"}}`, q.Get("page"), i))
			}
			fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
			return
		}
		fmt.Fprint(w, `[{"name":"api","private":true,"pushed_at":"2026-11-01T10:00:00Z","owner":{"login":"acme"}},`+
			`{"name":"old","fork":true,"archived":true,"owner":{"login":"octo"}},{"name":"","owner":{"login":"x"}},{"name":"nobody"}]`)
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
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeHost) client(t *testing.T, slept *[]time.Duration) *Client {
	h, err := ParseHost(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return &Client{Host: h, Now: func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			if slept != nil {
				*slept = append(*slept, d)
			}
			now = now.Add(d)
			return nil
		}}
}

func TestDeviceFlow(t *testing.T) {
	ctx := context.Background()
	t.Run("pending, slow down, then a token that expires", func(t *testing.T) {
		f := newFake(t)
		f.answers = []string{`{"error":"authorization_pending"}`, `{"error":"slow_down","interval":10}`, `{"error":"authorization_pending"}`,
			`{"access_token":"good","scope":"repo,read:org","token_type":"bearer","expires_in":28800}`}
		var slept []time.Duration
		c := f.client(t, &slept)
		code, err := c.StartDevice(ctx, "Iv1.abc", "repo read:org")
		if err != nil || code.UserCode != "WDJB-MJHT" || code.Interval != 5*time.Second || f.scope != "repo read:org" {
			t.Fatalf("%+v %v scope=%q", code, err, f.scope)
		}
		tok, err := c.WaitDevice(ctx, "Iv1.abc", code)
		if err != nil || tok.Value != "good" || tok.Scopes != "repo,read:org" {
			t.Fatalf("%+v %v", tok, err)
		}
		if want := c.Now().Add(28800 * time.Second); !tok.Expires.Equal(want) {
			t.Errorf("expiry %v, want %v", tok.Expires, want)
		}
		if !slices.Equal(slept, []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}) {
			t.Errorf("waits %v", slept)
		}
	})
	t.Run("a token that does not expire", func(t *testing.T) {
		f := newFake(t)
		f.answers = []string{`{"access_token":"good","scope":""}`}
		c := f.client(t, nil)
		code, _ := c.StartDevice(ctx, "id", "")
		tok, err := c.WaitDevice(ctx, "id", code)
		if err != nil || !tok.Expires.IsZero() {
			t.Errorf("%+v %v", tok, err)
		}
		if f.scope != "" {
			t.Errorf("a scope was sent: %q", f.scope)
		}
	})
	t.Run("denied", func(t *testing.T) {
		f := newFake(t)
		f.answers = []string{`{"error":"access_denied"}`}
		c := f.client(t, nil)
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrDenied) {
			t.Errorf("%v", err)
		}
	})
	t.Run("expired by the host and by the clock", func(t *testing.T) {
		f := newFake(t)
		f.answers = []string{`{"error":"expired_token"}`}
		c := f.client(t, nil)
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("host: %v", err)
		}
		f.answers, f.polls = []string{`{"error":"authorization_pending"}`}, 0
		if _, err := c.WaitDevice(ctx, "id", code); !errors.Is(err, ErrExpired) {
			t.Errorf("clock: %v", err)
		}
	})
	t.Run("a refused client id and an unknown error", func(t *testing.T) {
		f := newFake(t)
		c := f.client(t, nil)
		if _, err := c.StartDevice(ctx, "bad", ""); err == nil || !strings.Contains(err.Error(), "incorrect_client_credentials") {
			t.Errorf("%v", err)
		}
		f.answers = []string{`{"error":"unsupported_grant_type","error_description":"no"}`}
		code, _ := c.StartDevice(ctx, "id", "")
		if _, err := c.WaitDevice(ctx, "id", code); err == nil || !strings.Contains(err.Error(), "unsupported_grant_type") {
			t.Errorf("%v", err)
		}
	})
	t.Run("a cancelled context stops the wait", func(t *testing.T) {
		f := newFake(t)
		f.answers = []string{`{"error":"authorization_pending"}`}
		c := f.client(t, nil)
		code, _ := c.StartDevice(ctx, "id", "")
		cctx, cancel := context.WithCancel(ctx)
		c.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
		if _, err := c.WaitDevice(cctx, "id", code); !errors.Is(err, context.Canceled) {
			t.Errorf("%v", err)
		}
	})
}

func TestWhoamiAndOrgs(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	c := f.client(t, nil)
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

func TestRepos(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	c := f.client(t, nil)
	repos, more, err := c.Repos(ctx, "good")
	if err != nil || more || len(repos) != 2 {
		t.Fatalf("%+v %v %v", repos, more, err)
	}
	if r := repos[0]; r.Owner != "acme" || r.Name != "api" || !r.Private || r.Fork || !r.Pushed.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("first: %+v", r)
	}
	if r := repos[1]; r.Owner != "octo" || !r.Fork || !r.Archived || !r.Pushed.IsZero() {
		t.Errorf("second: %+v", r)
	}
	f.repoPages = 9
	repos, more, err = c.Repos(ctx, "good")
	if err != nil || !more || len(repos) != MaxRepos {
		t.Errorf("capped: %d %v %v", len(repos), more, err)
	}
	if _, _, err := c.Repos(ctx, "wrong"); err == nil {
		t.Error("a wrong token listed repositories")
	}
}
