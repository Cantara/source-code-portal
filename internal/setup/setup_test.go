package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/ghlogin"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const token = "ghp_SETUPsecret0123456789"

// host is a GitHub host for the page: the sign-in, who the token is, its organisations and its repositories.
func host(t *testing.T, gate <-chan struct{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"device_code":"d","user_code":"AAAA-BBBB","verification_uri":"http://github.example/login/device","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"`+token+`","scope":"repo","expires_in":3600}`)
	})
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `{"login":"octo"}`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `[{"login":"acme"},{"login":"zeta"}]`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var parts []string
		for _, rp := range []struct {
			owner, name   string
			private, fork bool
		}{
			{"acme", "whydah-sso", true, false}, {"acme", "whydah-admin", false, false}, {"acme", "whydah-ui", false, false},
			{"acme", "tools", false, false}, {"acme", "forked", false, true}, {"octo", "notes", true, false}, {"zeta", "other", false, false},
		} {
			parts = append(parts, fmt.Sprintf(`{"name":%q,"private":%v,"fork":%v,"archived":false,"pushed_at":"2026-11-01T10:00:00Z","owner":{"login":%q}}`,
				rp.name, rp.private, rp.fork, rp.owner))
		}
		fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// noKeepAlive keeps no connection after a call, so a test leaves no goroutine behind.
var noKeepAlive = &http.Transport{DisableKeepAlives: true} //nolint:gochecknoglobals // a test value

type rig struct {
	*httptest.Server
	s        *Server
	gh       *httptest.Server
	client   *http.Client
	code     string
	finished chan Result
	bodies   *bytes.Buffer // every answer the page was given
}

func newRig(t *testing.T, clientID string, sleep func(context.Context, time.Duration) error) *rig {
	t.Helper()
	gh := host(t, nil)
	r := &rig{gh: gh, code: "entry-code-0123456789", finished: make(chan Result, 1), bodies: &bytes.Buffer{}}
	ts := httptest.NewUnstartedServer(nil)
	r.s = New(Config{Addr: ts.Listener.Addr().String(), Code: r.code, Host: gh.URL, ClientID: clientID, Sleep: sleep, HTTP: &http.Client{Transport: noKeepAlive},
		Finish: func(_ context.Context, res Result) (string, error) {
			r.finished <- res
			return "/auth/launch?code=once", nil
		}})
	ts.Config.Handler = r.s.Handler()
	ts.Start()
	t.Cleanup(func() { ts.Close(); r.s.Close() })
	r.Server = ts
	jar, _ := cookiejar.New(nil)
	r.client = &http.Client{Jar: jar, Transport: noKeepAlive, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return r
}

func (r *rig) enter(t *testing.T) {
	t.Helper()
	res, err := r.client.Get(r.URL + "/setup/?code=" + r.code)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("entering: %d", res.StatusCode)
	}
}

func (r *rig) call(t *testing.T, method, path string, body any, header ...string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for i := 0; i+1 < len(header); i += 2 {
		if header[i+1] == "" {
			req.Header.Del(header[i])
		} else {
			req.Header.Set(header[i], header[i+1])
		}
	}
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	r.bodies.Write(b)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func TestEntry(t *testing.T) {
	r := newRig(t, "", nil)
	get := func(path, hostHeader string) int {
		req, _ := http.NewRequest(http.MethodGet, r.URL+path, nil)
		if hostHeader != "" {
			req.Host = hostHeader
		}
		res, err := r.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if c := get("/setup/", ""); c != http.StatusForbidden {
		t.Errorf("without the code: %d", c)
	}
	if c := get("/setup/?code=wrong", ""); c != http.StatusForbidden {
		t.Errorf("a wrong code: %d", c)
	}
	if c := get("/setup/?code="+r.code, "evil.example"); c != http.StatusMisdirectedRequest {
		t.Errorf("another host name (DNS rebinding): %d", c)
	}
	if c, _ := r.call(t, http.MethodGet, "/setup/api/state", nil); c != http.StatusForbidden {
		t.Errorf("the api without the cookie: %d", c)
	}
	r.enter(t)
	if c := get("/setup/", ""); c != http.StatusOK {
		t.Errorf("with the cookie: %d", c)
	}
	// The code is used up: another browser gets nothing.
	other := newRigClient(r)
	res, _ := other.Get(r.URL + "/setup/?code=" + r.code)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("the code twice: %d", res.StatusCode)
	}
	localhost := strings.Replace(r.URL, "127.0.0.1", "localhost", 1)
	req, _ := http.NewRequest(http.MethodGet, localhost+"/setup/", nil)
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode == http.StatusMisdirectedRequest {
		t.Error("localhost is the same machine")
	}
}

func newRigClient(r *rig) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Transport: noKeepAlive, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestAPIRefuses(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	if c, _ := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": "x"}, "Sec-Fetch-Site", "cross-site"); c != http.StatusForbidden {
		t.Errorf("cross-site: %d", c)
	}
	if c, _ := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": "x"}, "Sec-Fetch-Site", "", "Origin", "http://evil.example"); c != http.StatusForbidden {
		t.Errorf("another origin: %d", c)
	}
	if c, _ := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": "x"}, "Content-Type", "text/plain"); c != http.StatusUnsupportedMediaType {
		t.Errorf("not JSON: %d", c)
	}
	if c, _ := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"nope": "x"}); c != http.StatusBadRequest {
		t.Errorf("an unknown field: %d", c)
	}
	if c, o := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": "wrong"}); c != http.StatusBadRequest || !strings.Contains(fmt.Sprint(o["error"]), "does not accept") {
		t.Errorf("a wrong token: %d %v", c, o)
	}
	if c, _ := r.call(t, http.MethodPost, "/setup/api/repos", map[string]any{"accounts": []string{"octo"}}); c != http.StatusBadRequest {
		t.Errorf("repositories before signing in: %d", c)
	}
	if c, _ := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": []string{"octo"}}); c != http.StatusBadRequest {
		t.Errorf("finishing before signing in: %d", c)
	}
}

func TestWithAToken(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	code, st := r.call(t, http.MethodGet, "/setup/api/state", nil)
	if code != 200 || st["hostOk"] != true || st["canCode"] != false || st["user"] != nil {
		t.Fatalf("state: %d %v", code, st)
	}
	if code, st = r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": token}); code != 200 || st["user"] != "octo" {
		t.Fatalf("token: %d %v", code, st)
	}
	if fmt.Sprint(st["orgs"]) != "[acme zeta]" {
		t.Errorf("orgs: %v", st["orgs"])
	}
	// A stranger's account is refused; the chosen ones list their repositories without forks and other accounts'.
	if code, _ := r.call(t, http.MethodPost, "/setup/api/repos", map[string]any{"accounts": []string{"intruder"}}); code != http.StatusBadRequest {
		t.Errorf("an account that is not the person's: %d", code)
	}
	code, list := r.call(t, http.MethodPost, "/setup/api/repos", map[string]any{"accounts": []string{"octo", "acme"}})
	if code != 200 {
		t.Fatalf("repos: %d %v", code, list)
	}
	var names []string
	for _, x := range list["repos"].([]any) {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	if fmt.Sprint(names) != "[notes tools whydah-admin whydah-sso whydah-ui]" {
		t.Errorf("repositories: %v", names)
	}
	sug, _ := json.Marshal(list["suggested"])
	if !strings.Contains(string(sug), `"name":"Whydah"`) || !strings.Contains(string(sug), `"whydah-sso"`) {
		t.Errorf("suggested: %s", sug)
	}

	for name, body := range map[string]map[string]any{
		"an unknown repository":   {"accounts": []string{"octo"}, "products": []Product{{Name: "X", Repos: []string{"not-listed"}}}},
		"a repository twice":      {"accounts": []string{"octo"}, "products": []Product{{Name: "X", Repos: []string{"notes"}}, {Name: "Y", Repos: []string{"notes"}}}},
		"hidden and in a product": {"accounts": []string{"octo"}, "products": []Product{{Name: "X", Repos: []string{"notes"}}}, "hidden": []string{"notes"}},
		"an empty product":        {"accounts": []string{"octo"}, "products": []Product{{Name: "X"}}},
		"two with one name":       {"accounts": []string{"octo", "acme"}, "products": []Product{{Name: "X", Repos: []string{"notes"}}, {Name: "x", Repos: []string{"tools"}}}},
		"no name":                 {"accounts": []string{"octo"}, "products": []Product{{Name: " ", Repos: []string{"notes"}}}},
	} {
		if code, o := r.call(t, http.MethodPost, "/setup/api/finish", body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, o)
		}
	}
	select {
	case <-r.finished:
		t.Fatal("Finish ran for a refused choice")
	default:
	}

	good := map[string]any{"accounts": []string{"octo", "acme"},
		"products": []Product{{Name: "Whydah", Repos: []string{"whydah-admin", "whydah-sso", "whydah-ui"}}}, "hidden": []string{"notes"}}
	code, st = r.call(t, http.MethodPost, "/setup/api/finish", good)
	if code != 200 || st["launch"] != "/auth/launch?code=once" || st["done"] != true {
		t.Fatalf("finish: %d %v", code, st)
	}
	res := <-r.finished
	if res.User != "octo" || res.Token.Value != token || res.Host.API != r.gh.URL+"/api/v3/" || res.Default != "whydah-sso" && res.Default != "whydah-admin" && res.Default != "whydah-ui" && res.Default != "tools" {
		t.Errorf("result: %+v", res)
	}
	if !slices.Equal(res.Accounts, []string{"octo", "acme"}) || !slices.Equal(res.Hidden, []string{"notes"}) || len(res.Products) != 1 {
		t.Errorf("result: %+v", res)
	}
	select {
	case <-r.s.Done():
	case <-time.After(time.Second):
		t.Error("Done is not closed")
	}
	if code, st = r.call(t, http.MethodPost, "/setup/api/finish", good); code != 200 || st["done"] != true { // again: no second write
		t.Errorf("finishing twice: %d %v", code, st)
	}
	select {
	case <-r.finished:
		t.Error("Finish ran twice")
	default:
	}
	if strings.Contains(r.bodies.String(), token) {
		t.Error("the token was sent to the page")
	}
}

func TestWithACode(t *testing.T) {
	r := newRig(t, "Iv1.abc", func(context.Context, time.Duration) error { return nil })
	r.enter(t)
	if _, st := r.call(t, http.MethodGet, "/setup/api/state", nil); st["canCode"] != true {
		t.Fatalf("state: %v", st)
	}
	code, st := r.call(t, http.MethodPost, "/setup/api/device", map[string]any{})
	if code != 200 {
		t.Fatalf("device: %d %v", code, st)
	}
	flow := st["flow"].(map[string]any)
	if flow["userCode"] != "AAAA-BBBB" || flow["verificationUri"] != "http://github.example/login/device" {
		t.Errorf("flow: %v", flow)
	}
	for deadline := time.Now().Add(5 * time.Second); st["user"] == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the sign-in did not finish: %v", st)
		}
		_, st = r.call(t, http.MethodGet, "/setup/api/state", nil)
	}
	if st["user"] != "octo" || st["ends"] == nil || strings.Contains(r.bodies.String(), token) {
		t.Errorf("signed in: %v", st)
	}
}

func TestACodeForAnotherHostIsDropped(t *testing.T) {
	gate := make(chan struct{})
	r := newRig(t, "Iv1.abc", func(ctx context.Context, _ time.Duration) error {
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	r.enter(t)
	r.call(t, http.MethodGet, "/setup/api/state", nil)
	if code, _ := r.call(t, http.MethodPost, "/setup/api/device", map[string]any{}); code != 200 {
		t.Fatal("device")
	}
	// The person goes back before confirming; the code that is confirmed afterwards must not sign them in.
	_, st := r.call(t, http.MethodPost, "/setup/api/signout", map[string]any{})
	if st["flow"].(map[string]any)["state"] != "" {
		t.Fatalf("after going back: %v", st)
	}
	close(gate)
	time.Sleep(100 * time.Millisecond)
	if _, st = r.call(t, http.MethodGet, "/setup/api/state", nil); st["user"] != nil {
		t.Errorf("a cancelled sign-in signed in: %v", st)
	}
}

func TestNoCodeWithoutAClientID(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	r.call(t, http.MethodGet, "/setup/api/state", nil)
	if code, _ := r.call(t, http.MethodPost, "/setup/api/device", map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("a code sign-in without a client id: %d", code)
	}
	if code, st := r.call(t, http.MethodPost, "/setup/api/host", map[string]string{"host": r.gh.URL, "clientId": "Iv1.mine"}); code != 200 || st["canCode"] != true {
		t.Errorf("a client id for the host: %d %v", code, st)
	}
	if code, _ := r.call(t, http.MethodPost, "/setup/api/host", map[string]string{"host": "http://ghe.example.org"}); code != http.StatusBadRequest {
		t.Errorf("a plain http host: %d", code)
	}
}

func TestSuggest(t *testing.T) {
	repos := []ghlogin.Repo{
		{Owner: "acme", Name: "whydah-sso"}, {Owner: "acme", Name: "whydah_admin"}, {Owner: "acme", Name: "Whydah.ui"},
		{Owner: "acme", Name: "docs-site"}, {Owner: "acme", Name: "docs-api"}, // two: below the threshold
		{Owner: "acme", Name: "tools"}, {Owner: "octo", Name: "notes"}, {Owner: "octo", Name: "dotfiles"},
	}
	got := Suggest(repos)
	if len(got) != 3 || got[1].Name != "Whydah" || !slices.Equal(got[1].Repos, []string{"Whydah.ui", "whydah-sso", "whydah_admin"}) {
		t.Fatalf("suggest: %+v", got)
	}
	// Several accounts: what is left is sorted by account (docs-site and docs-api are two, below the threshold of a word).
	if got[0].Name != "acme" || !slices.Equal(got[0].Repos, []string{"docs-api", "docs-site", "tools"}) ||
		got[2].Name != "octo" || !slices.Equal(got[2].Repos, []string{"dotfiles", "notes"}) {
		t.Errorf("by account: %+v", got)
	}
	// One account: what is left stays without a product.
	one := Suggest([]ghlogin.Repo{{Owner: "a", Name: "x"}, {Owner: "a", Name: "y"}})
	if len(one) != 0 {
		t.Errorf("one account: %+v", one)
	}
	var many []ghlogin.Repo
	for i := range 100 {
		many = append(many, ghlogin.Repo{Owner: "a", Name: fmt.Sprintf("big-%03d", i)})
	}
	if p := Suggest(many); len(p) != 1 || len(p[0].Repos) != MaxPerProduct {
		t.Errorf("a product is capped at what the configuration allows: %+v", p)
	}
}
