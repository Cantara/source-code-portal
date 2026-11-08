package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type fakeRepo struct {
	name          string
	private, fork bool
	topics        string // the JSON list's items
}

// orgRepos are the repositories each account holds, the most recently pushed first.
var orgRepos = map[string][]fakeRepo{ //nolint:gochecknoglobals // a test table, never written
	"acme": {{"whydah-sso", true, false, `"iam"`}, {"whydah-admin", false, false, `"iam"`}, {"whydah-ui", false, false, `"iam"`},
		{"tools", false, false, ""}, {"forked", false, true, ""}},
	"octo": {{"notes", true, false, ""}},
	"zeta": {{"other", false, false, ""}},
}

// host is a GitHub host for the page: the sign-in, who the token is, its organisations and its repositories.
func host(t *testing.T, gate <-chan struct{}, app *fakeApp) *httptest.Server {
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
			fmt.Fprint(w, `{"login":"octo","public_repos":0,"owned_private_repos":1}`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `[{"login":"acme"},{"login":"zeta"}]`)
		}
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprintf(w, `{"login":%q,"public_repos":%d,"total_private_repos":%d}`, r.PathValue("org"), len(orgRepos[r.PathValue("org")])-1, 1)
		}
	})
	list := func(w http.ResponseWriter, owner string) {
		var parts []string
		for _, rp := range orgRepos[owner] {
			parts = append(parts, fmt.Sprintf(`{"name":%q,"private":%v,"fork":%v,"archived":false,"pushed_at":"2026-11-01T10:00:00Z","topics":[%s],"owner":{"login":%q}}`,
				rp.name, rp.private, rp.fork, rp.topics, owner))
		}
		fmt.Fprint(w, "["+strings.Join(parts, ",")+"]")
	}
	mux.HandleFunc("GET /api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			list(w, "octo")
		}
	})
	mux.HandleFunc("GET /api/v3/orgs/{org}/repos", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		if gate != nil { // the test holds the read to look at its progress
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		list(w, r.PathValue("org"))
	})
	app.routes(mux)
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
	app      *fakeApp
	client   *http.Client
	code     string
	finished chan Result
	bodies   *bytes.Buffer                         // every answer the page was given
	cli      func(hostname string) (string, error) // the GitHub CLI's answer
}

func newRig(t *testing.T, clientID string, sleep func(context.Context, time.Duration) error) *rig {
	t.Helper()
	return newRigHeld(t, clientID, sleep, nil)
}

// newRigHeld is a rig whose organisation listings wait for hold to close.
func newRigHeld(t *testing.T, clientID string, sleep func(context.Context, time.Duration) error, hold <-chan struct{}) *rig {
	t.Helper()
	app := newFakeApp()
	gh := host(t, hold, app)
	r := &rig{gh: gh, app: app, code: "entry-code-0123456789", finished: make(chan Result, 1), bodies: &bytes.Buffer{},
		cli: func(string) (string, error) { return "", errors.New("no login") }}
	ts := httptest.NewUnstartedServer(nil)
	r.s = New(Config{Addr: ts.Listener.Addr().String(), Code: r.code, Host: gh.URL, ClientID: clientID, Sleep: sleep, HTTP: &http.Client{Transport: noKeepAlive},
		CLIToken: func(_ context.Context, hostname string) (string, error) { return r.cli(hostname) },
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

// list reads the repositories of the accounts the way the page does: starts the job and waits until it is over.
func (r *rig) list(t *testing.T, accounts ...string) map[string]any {
	t.Helper()
	code, st := r.call(t, http.MethodPost, "/setup/api/list", map[string]any{"accounts": accounts})
	if code != 200 {
		t.Fatalf("list: %d %v", code, st)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		job, _ := st["job"].(map[string]any)
		if job != nil && job["state"] != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("the listing did not end: %v", st)
		}
		_, st = r.call(t, http.MethodGet, "/setup/api/state", nil)
	}
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
	if code, _ := r.call(t, http.MethodPost, "/setup/api/repos", map[string]any{"accounts": []string{"octo", "acme"}}); code != http.StatusBadRequest {
		t.Errorf("repositories before they are read: %d", code)
	}
	if job := r.list(t, "octo", "acme"); job["state"] != "done" || job["done"] != 6.0 || job["total"] != 6.0 {
		t.Fatalf("job: %v", job)
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
	if p := Suggest(many); len(p) != 1 || len(p[0].Repos) != 100 {
		t.Errorf("a product holds all its repositories; rules, not a cap, keep the matchers within the limit: %d", len(p))
	}
}

// TestSuggestNoDuplicates holds what a real list showed: one name in two accounts, and an account named like a first word.
func TestSuggestNoDuplicates(t *testing.T) {
	var repos []ghlogin.Repo
	for i := range 3 { // the first word cantara is also an account: the account's leftovers must not take the same product name
		repos = append(repos, ghlogin.Repo{Owner: "Cantara", Name: fmt.Sprintf("cantara-%03d", i)})
	}
	repos = append(repos, ghlogin.Repo{Owner: "Cantara", Name: "left-1"}, ghlogin.Repo{Owner: "Cantara", Name: "left-2"},
		ghlogin.Repo{Owner: "Cantara", Name: ".github"}, ghlogin.Repo{Owner: "DNB", Name: ".github"},
		ghlogin.Repo{Owner: "descoped", Name: "same"}, ghlogin.Repo{Owner: "oranheim", Name: "same"})
	if got := OnePerName(repos); len(got) != 3+2+1+1 {
		t.Fatalf("one per name: %d", len(got))
	}
	names, held := map[string]bool{}, map[string]bool{}
	for _, p := range Suggest(repos) {
		if names[strings.ToLower(p.Name)] {
			t.Errorf("two products named %q", p.Name)
		}
		names[strings.ToLower(p.Name)] = true
		for _, n := range p.Repos {
			if held[n] {
				t.Errorf("%q is in two products", n)
			}
			held[n] = true
		}
	}
	if !names["cantara"] || !names["cantara (other)"] {
		t.Errorf("products: %v", names)
	}
}

func TestWithTheGitHubCLI(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	var asked string
	r.cli = func(hostname string) (string, error) { asked = hostname; return token + "\n", nil }
	if _, st := r.call(t, http.MethodGet, "/setup/api/state", nil); st["canCli"] != true {
		t.Fatalf("the button is not offered: %v", st)
	}
	code, st := r.call(t, http.MethodPost, "/setup/api/cli", map[string]string{})
	if code != 200 || st["user"] != "octo" {
		t.Fatalf("cli: %d %v", code, st)
	}
	if want := strings.Split(strings.TrimPrefix(r.gh.URL, "http://"), ":")[0]; asked != want {
		t.Errorf("the CLI was asked for %q, want %q", asked, want)
	}
	if strings.Contains(r.bodies.String(), token) {
		t.Error("the token reached the page")
	}

	r2 := newRig(t, "", nil)
	r2.enter(t)
	if code, o := r2.call(t, http.MethodPost, "/setup/api/cli", map[string]string{}); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(o), "gh auth login") {
		t.Errorf("no login: %d %v", code, o)
	}
	r2.cli = func(string) (string, error) { return "not-a-token", nil }
	if code, o := r2.call(t, http.MethodPost, "/setup/api/cli", map[string]string{}); code != http.StatusBadRequest || strings.Contains(fmt.Sprint(o), "not-a-token") {
		t.Errorf("a token GitHub refuses: %d %v", code, o)
	}
}

func TestListingProgressAndCancel(t *testing.T) {
	hold := make(chan struct{})
	r := newRigHeld(t, "", nil, hold)
	r.enter(t)
	if code, st := r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": token}); code != 200 || st["user"] != "octo" {
		t.Fatalf("token: %d %v", code, st)
	}
	_, st := r.call(t, http.MethodPost, "/setup/api/list", map[string]any{"accounts": []string{"acme", "octo"}})
	var job map[string]any
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		job, _ = st["job"].(map[string]any)
		// acme (sorted first) is held: the sizes are in (acme 5 in the host's table minus one public, plus one private; octo 1)
		if job != nil && job["total"] != 0.0 && job["state"] == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no progress: %v", st)
		}
		_, st = r.call(t, http.MethodGet, "/setup/api/state", nil)
	}
	if job["account"] != "acme" || job["step"] != 1.0 || job["steps"] != 2.0 || job["done"] != 0.0 {
		t.Errorf("running: %v", job)
	}
	if code, _ := r.call(t, http.MethodPost, "/setup/api/repos", map[string]any{"accounts": []string{"acme", "octo"}}); code != http.StatusBadRequest {
		t.Errorf("repositories while the job runs: %d", code)
	}
	// Finish is refused while the repositories are not read.
	if code, _ := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": []string{"acme", "octo"}, "products": []Product{}}); code != http.StatusBadRequest {
		t.Errorf("finish before the read: %d", code)
	}
	_, st = r.call(t, http.MethodPost, "/setup/api/list/cancel", map[string]any{})
	if job, _ = st["job"].(map[string]any); job["state"] != "cancelled" {
		t.Errorf("cancel: %v", job)
	}
	close(hold)
	time.Sleep(50 * time.Millisecond) // a late page must not bring the cancelled read back
	if _, st = r.call(t, http.MethodGet, "/setup/api/state", nil); st["job"].(map[string]any)["state"] != "cancelled" {
		t.Errorf("a cancelled read came back: %v", st["job"])
	}
	// Reading again works, and another choice of accounts reads again.
	if job := r.list(t, "acme"); job["state"] != "done" || job["done"] != 5.0 {
		t.Errorf("again: %v", job)
	}
	if job := r.list(t, "acme", "zeta"); job["state"] != "done" || job["done"] != 6.0 {
		t.Errorf("other accounts: %v", job)
	}
}

func TestRulesPreviewTestAndFinish(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": token})
	accounts := []string{"octo", "acme"}
	if code, _ := r.call(t, http.MethodPost, "/setup/api/rules", map[string]any{"accounts": accounts, "products": []Product{}}); code != http.StatusBadRequest {
		t.Errorf("a preview before the read: %d", code)
	}
	r.list(t, accounts...)
	products := []Product{{Name: "Whydah", Repos: []string{"whydah-sso", "whydah-admin"}}}
	code, out := r.call(t, http.MethodPost, "/setup/api/rules", map[string]any{"accounts": accounts, "products": products})
	if code != 200 {
		t.Fatalf("rules: %d %v", code, out)
	}
	b, _ := json.Marshal(out["products"])
	for _, want := range []string{`"kind":"prefix"`, `"value":"whydah-"`, `"own":2`, `"extra":["whydah-ui"]`, `"open":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the preview lacks %s: %s", want, b)
		}
	}
	// A topic every member has is a rule too.
	_, out = r.call(t, http.MethodPost, "/setup/api/rules", map[string]any{"accounts": accounts, "products": []Product{{Name: "I", Repos: []string{"whydah-ui", "whydah-sso"}}}})
	if b, _ = json.Marshal(out["products"]); !strings.Contains(string(b), `"kind":"topic"`) && !strings.Contains(string(b), `"whydah-"`) {
		t.Errorf("rules: %s", b)
	}
	// Testing a rule.
	_, out = r.call(t, http.MethodPost, "/setup/api/rule-test", map[string]any{"accounts": accounts, "kind": "prefix", "value": "WHYDAH-"})
	if names, _ := out["names"].([]any); out["matches"] != 3.0 || len(names) != 3 {
		t.Errorf("rule-test: %v", out)
	}
	if _, out = r.call(t, http.MethodPost, "/setup/api/rule-test", map[string]any{"accounts": accounts, "kind": "regexp", "value": "("}); out["error"] == nil || out["error"] == "" {
		t.Errorf("a bad regexp was not refused: %v", out)
	}
	// A fixed product has no derived rules, only its repositories.
	_, out = r.call(t, http.MethodPost, "/setup/api/rules", map[string]any{"accounts": accounts, "products": []Product{{Name: "F", Repos: []string{"whydah-sso", "whydah-admin"}, Fixed: true}}})
	if b, _ = json.Marshal(out["products"]); strings.Contains(string(b), `"kind"`) && !strings.Contains(string(b), `"pinned":["whydah-admin","whydah-sso"]`) {
		t.Errorf("a fixed product: %s", b)
	}
	// Finishing: a bad rule of the person's own is refused; a good product is written with its derived matchers.
	bad := []Product{{Name: "Whydah", Repos: []string{"whydah-sso"}, Rules: []Rule{{Kind: "regexp", Value: "("}}}}
	if code, _ := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": accounts, "products": bad}); code != http.StatusBadRequest {
		t.Errorf("a bad rule: %d", code)
	}
	good := []Product{{Name: "Whydah", Repos: []string{"whydah-sso", "whydah-admin"}, Rules: []Rule{{Kind: "glob", Value: "tool*"}}}, {Name: "Notes", Repos: []string{"notes"}}}
	if code, o := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": accounts, "products": good}); code != 200 {
		t.Fatalf("finish: %d %v", code, o)
	}
	res := <-r.finished
	w := res.Products[0].Matchers
	if len(w) != 2 || w[0].Kind != "glob" || w[1].Kind != "prefix" || w[1].Value != "whydah-" {
		t.Errorf("matchers: %+v", w)
	}
	if n := res.Products[1].Matchers; len(n) != 1 || n[0].Kind != "exact" || n[0].Value != "notes" {
		t.Errorf("a lone repository is pinned: %+v", n)
	}
	// What no product selects is the portal's "Other": whydah-ui is caught by the prefix, tools by the glob.
	if !slices.Equal(res.Unassigned, []string{}) && len(res.Unassigned) != 0 {
		t.Errorf("unassigned: %v", res.Unassigned)
	}
}
