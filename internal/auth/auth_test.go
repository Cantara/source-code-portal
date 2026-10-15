package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/auth/oidctest"
	"github.com/cantara/source-code-portal/internal/httpapi"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func key() []byte { return []byte(strings.Repeat("k", 32)) }

func TestSeal(t *testing.T) {
	s, _ := newSealer(key(), sessionLabel)
	v, err := s.seal("scp_session", session{Subject: "u-1"})
	if err != nil {
		t.Fatal(err)
	}
	var got session
	if !s.open("scp_session", v, &got) || got.Subject != "u-1" {
		t.Fatalf("round trip: %+v", got)
	}
	other, _ := newSealer([]byte(strings.Repeat("x", 32)), sessionLabel)
	flows, _ := newSealer(key(), flowLabel)
	b := []byte(v)
	b[len(b)/2] ^= 1
	for name, ok := range map[string]bool{
		"another cookie": s.open("scp_oidc", v, &got), "another key": other.open("scp_session", v, &got),
		"another label": flows.open("scp_session", v, &got), "tampered": s.open("scp_session", string(b), &got),
		"garbage": s.open("scp_session", "%%%", &got), "empty": s.open("scp_session", "", &got),
	} {
		if ok {
			t.Errorf("%s opened", name)
		}
	}
	if _, err := s.seal("scp_session", session{Groups: slices.Repeat([]string{strings.Repeat("g", 200)}, 30)}); !errors.Is(err, errTooLarge) {
		t.Errorf("a value too large for a cookie: %v", err)
	}
}

// portal is SCP's side of a test: the sign-in routes, and every other path answering with the identified viewer.
type portal struct {
	*httptest.Server
	auth *Auth
	jar  http.CookieJar
}

func newPortal(t *testing.T, o Options) *portal {
	t.Helper()
	p := &portal{}
	mux := http.NewServeMux()
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok, err := p.auth.Identify(w, r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		v := httpapi.Viewer{}
		if ok {
			v.Identity = id
		}
		mux.ServeHTTP(w, r.WithContext(httpapi.WithViewer(r.Context(), v)))
	}))
	t.Cleanup(p.Close)
	if o.PublicURL == "" {
		o.PublicURL = p.URL
	}
	if o.SessionTTL == 0 {
		o.SessionTTL = 12 * time.Hour
	}
	var err error
	if p.auth, err = New(o); err != nil {
		t.Fatal(err)
	}
	for _, rt := range p.auth.Routes() {
		mux.Handle(rt.Pattern, rt.Handler)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		v := httpapi.ViewerOf(r.Context())
		_, _ = io.WriteString(w, r.URL.RequestURI()+" "+v.Via+" "+v.Name+" "+v.Email+" "+strings.Join(v.Groups, ","))
	})
	p.jar, _ = cookiejar.New(nil)
	return p
}

// get follows redirects with the portal's cookie jar and returns the final URL and body.
func (p *portal) get(t *testing.T, path string) (string, string) {
	t.Helper()
	c := &http.Client{Jar: p.jar}
	res, err := c.Get(p.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.Request.URL.RequestURI(), string(b)
}

// post answers the status and headers of a POST, not following redirects.
func (p *portal) post(t *testing.T, path string, form url.Values, header ...string) (int, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	c := &http.Client{Jar: p.jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode, res.Header
}

func withProvider(t *testing.T) (*oidctest.Provider, Options) {
	t.Helper()
	idp := oidctest.New("scp")
	t.Cleanup(idp.Close)
	return idp, Options{SessionKey: key(), OIDC: &OIDC{Issuer: idp.Issuer(), ClientID: "scp", Scopes: []string{"openid", "email", "profile"},
		GroupsClaim: "groups"}, Keep: func(g []string) []string {
		return slices.DeleteFunc(slices.Clone(g), func(s string) bool { return s != "staff" })
	}}
}

func TestSignIn(t *testing.T) {
	idp, o := withProvider(t)
	idp.SetUser(oidctest.User{Subject: "u-1", Email: "ada@example.org", Name: "Ada", Groups: []string{"staff", "dev"}})
	p := newPortal(t, o)
	if at, body := p.get(t, "/"); body != "/    " {
		t.Fatalf("before sign-in: %s %q", at, body)
	}
	at, body := p.get(t, "/auth/login?return_to=/repos/acme/core%3Ftab=1")
	if at != "/repos/acme/core?tab=1" || body != "/repos/acme/core?tab=1 oidc Ada ada@example.org staff" {
		t.Fatalf("after sign-in: %s %q", at, body)
	}
	if _, body := p.get(t, "/x"); body != "/x oidc Ada ada@example.org staff" {
		t.Errorf("the session does not last: %q", body)
	}
	if oidcOn, token := p.auth.Methods(); !oidcOn || token {
		t.Errorf("methods: %v %v", oidcOn, token)
	}
}

func TestSignInUserinfoAndUnverified(t *testing.T) {
	idp, o := withProvider(t)
	idp.SetUser(oidctest.User{Subject: "u-2", Email: "bo@example.org", Name: "Bo", Unverified: true, Groups: []string{"staff"},
		GroupsInUserinfo: true})
	p := newPortal(t, o)
	if _, body := p.get(t, "/auth/login"); body != "/ oidc Bo  staff" { // groups from userinfo; an unverified email matches no list
		t.Errorf("signed in: %q", body)
	}
}

func TestSignInRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(*oidctest.Provider)
		path string
		want string
	}{
		"wrong nonce":    {set: func(p *oidctest.Provider) { p.WrongNonce(true) }, path: "/auth/login", want: "/signin?error=provider"},
		"expired token":  {set: func(p *oidctest.Provider) { p.Expired(true) }, path: "/auth/login", want: "/signin?error=provider"},
		"forged state":   {path: "/auth/callback?code=x&state=forged", want: "/signin?error=expired"}, // no pending sign-in
		"provider error": {path: "/auth/callback?error=access_denied", want: "/signin?error=expired"},
	} {
		t.Run(name, func(t *testing.T) {
			idp, o := withProvider(t)
			if tc.set != nil {
				tc.set(idp)
			}
			p := newPortal(t, o)
			if at, body := p.get(t, tc.path); !strings.HasPrefix(body, tc.want+" ") || at != tc.want {
				t.Errorf("at %s: %q", at, body)
			}
			if _, body := p.get(t, "/"); body != "/    " {
				t.Errorf("signed in after a refusal: %q", body)
			}
		})
	}
	t.Run("state of another sign-in", func(t *testing.T) {
		_, o := withProvider(t)
		p := newPortal(t, o)
		c := &http.Client{Jar: p.jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := c.Get(p.URL + "/auth/login") // the pending sign-in cookie is set; the provider is not visited
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if at, _ := p.get(t, "/auth/callback?code=x&state=forged"); at != "/signin?error=state" {
			t.Errorf("forged state: %s", at)
		}
		res, err = c.Get(p.URL + "/auth/login")
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if at, _ := p.get(t, "/auth/callback?error=access_denied"); at != "/signin?error=denied" {
			t.Errorf("refused at the provider: %s", at)
		}
	})
	t.Run("provider down", func(t *testing.T) {
		idp, o := withProvider(t)
		idp.Close()
		p := newPortal(t, o)
		if at, _ := p.get(t, "/auth/login"); at != "/signin?error=provider" {
			t.Errorf("provider down: %s", at)
		}
	})
}

func TestLogout(t *testing.T) {
	idp, o := withProvider(t)
	p := newPortal(t, o)
	p.get(t, "/auth/login")
	if code, _ := p.post(t, "/auth/logout", nil, "Sec-Fetch-Site", "cross-site"); code != http.StatusForbidden {
		t.Errorf("a cross-site sign-out: %d", code)
	}
	code, h := p.post(t, "/auth/logout", nil, "Sec-Fetch-Site", "same-origin")
	loc, _ := url.Parse(h.Get("Location"))
	if code != http.StatusSeeOther || !strings.HasPrefix(loc.String(), idp.Issuer()+"/end_session?") ||
		loc.Query().Get("client_id") != "scp" || loc.Query().Get("post_logout_redirect_uri") != p.URL+"/" {
		t.Errorf("sign-out: %d %s", code, loc)
	}
	if _, body := p.get(t, "/"); body != "/    " {
		t.Errorf("signed in after sign-out: %q", body)
	}
}

func TestTokens(t *testing.T) {
	secret := strings.Repeat("s", 40)
	o := Options{SessionKey: key(), Tokens: []Token{{Name: "ci", Secret: []byte(secret), Groups: []string{"staff"}, Admin: true}}}
	p := newPortal(t, o)
	for _, tc := range []struct {
		auth string
		want int
	}{{"Bearer " + secret, 200}, {"bearer " + secret, 200}, {"Bearer wrong", 401}, {"Basic " + secret, 401}, {"Bearer ", 401}} {
		req, _ := http.NewRequest(http.MethodGet, p.URL+"/", nil)
		req.Header.Set("Authorization", tc.auth)
		res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != tc.want || tc.want == 200 && string(b) != "/ token ci  staff" {
			t.Errorf("%q: %d %q", tc.auth, res.StatusCode, b)
		}
	}
	if _, h := p.post(t, "/auth/token", url.Values{"token": {"wrong"}}, "Sec-Fetch-Site", "same-origin"); h.Get("Location") != "/signin?error=token" {
		t.Errorf("a wrong token: %v", h)
	}
	code, h := p.post(t, "/auth/token", url.Values{"token": {secret}, "return_to": {"/groups/core"}}, "Sec-Fetch-Site", "same-origin")
	if code != http.StatusSeeOther || h.Get("Location") != "/groups/core" {
		t.Fatalf("token sign-in: %d %v", code, h)
	}
	if _, body := p.get(t, "/"); body != "/ token ci  staff" {
		t.Errorf("token session: %q", body)
	}
	// The token is looked up on every request: without it the session ends.
	p.auth, _ = New(Options{SessionKey: key(), SessionTTL: time.Hour, PublicURL: p.URL})
	if _, body := p.get(t, "/"); body != "/    " {
		t.Errorf("a removed token's session: %q", body)
	}
}

func TestSessionLifetime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	o := Options{SessionKey: key(), SessionTTL: time.Hour, Now: func() time.Time { return now }}
	p := newPortal(t, o)
	a := p.auth
	rec := httptest.NewRecorder()
	if err := a.start(rec, session{Subject: "u-1", Name: "Ada"}); err != nil {
		t.Fatal(err)
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 3600 || cookie.Secure {
		t.Errorf("cookie: %+v", cookie)
	}
	identify := func() (bool, []*http.Cookie) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		_, ok, err := a.Identify(w, r)
		if err != nil {
			t.Fatal(err)
		}
		return ok, w.Result().Cookies()
	}
	if ok, set := identify(); !ok || len(set) != 0 {
		t.Errorf("a fresh session: %v, set %v", ok, set)
	}
	now = now.Add(31 * time.Minute)
	ok, set := identify()
	if !ok || len(set) != 1 || set[0].Value == cookie.Value {
		t.Errorf("past half its lifetime the session is resealed: %v %v", ok, set)
	}
	now = now.Add(30 * time.Minute) // the original session has ended; the resealed one lasts
	if ok, set := identify(); ok || len(set) != 1 || set[0].MaxAge != -1 {
		t.Errorf("an ended session is cleared: %v %v", ok, set)
	}
	if secure, _ := New(Options{SessionKey: key(), SessionTTL: time.Hour, PublicURL: "https://scp.example"}); secure.session != "__Host-scp_session" {
		t.Errorf("https cookie name: %s", secure.session)
	}
}

func TestReturnTo(t *testing.T) {
	for in, want := range map[string]string{
		"/repos/a/b?x=1": "/repos/a/b?x=1", "": "/", "https://evil.example/": "/", "//evil.example": "/", `/\evil.example`: "/",
		"repos": "/", "/auth/logout": "/", "javascript:alert(1)": "/",
	} {
		if got := returnTo(in); got != want {
			t.Errorf("returnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
