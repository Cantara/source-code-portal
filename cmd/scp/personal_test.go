package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gogh "github.com/google/go-github/v93/github"
	"github.com/zalando/go-keyring"

	"github.com/cantara/source-code-portal/internal/config"
	"github.com/cantara/source-code-portal/internal/ghlogin"
	"github.com/cantara/source-code-portal/internal/github/githubtest"
	"github.com/cantara/source-code-portal/internal/store"
	"github.com/cantara/source-code-portal/internal/vault"
)

// fakeHost is a GitHub host for the wizard: the sign-in and the three calls it makes. The token is "ghp_personal".
func fakeHost(t *testing.T, header map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer ghp_personal" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			for k, v := range header {
				w.Header().Set(k, v)
			}
			fmt.Fprint(w, `{"login":"octo"}`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/orgs", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `[{"login":"acme"},{"login":"zeta"}]`)
		}
	})
	mux.HandleFunc("GET /api/v3/user/repos", func(w http.ResponseWriter, r *http.Request) {
		if authed(w, r) {
			fmt.Fprint(w, `[{"name":"newest"}]`)
		}
	})
	mux.HandleFunc("POST /login/device/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"device_code":"d","user_code":"AAAA-BBBB","verification_uri":"http://host/login/device","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"ghp_personal","scope":"repo","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

var mockKeychain sync.Once

func newPersonal(t *testing.T, dir, host, input string, f personalFlags) (*personal, *bytes.Buffer) {
	t.Helper()
	mockKeychain.Do(keyring.MockInit) // once: a second init would lose the keys of the first
	out := &bytes.Buffer{}
	f.dir, f.host = dir, host
	if f.port == 0 {
		f.port = 9090
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return &personal{flags: f, in: bufio.NewReader(strings.NewReader(input)), out: out,
		sleep: func(context.Context, time.Duration) error { return nil }, now: func() time.Time { return now }}, out
}

func credentialOf(t *testing.T, dir string) vault.Secret {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(dir, "scp.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, _, err := vault.OpenKey(dir, vault.Keychain(dir))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(key, st)
	sec, err := v.Get(t.Context(), personalSource)
	if err != nil {
		t.Fatal(err)
	}
	return sec
}

func TestPersonalWizardWithAToken(t *testing.T) {
	host := fakeHost(t, nil)
	dir := filepath.Join(t.TempDir(), "scp")
	// Enter: no client id (paste a token); the token; the accounts: 1 and 3 (octo and zeta).
	p, out := newPersonal(t, dir, host.URL, "\nghp_personal\n1,3\n", personalFlags{})
	path, err := p.setup(t.Context(), dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	yaml, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`credential: true`, `orgs: ["octo", "zeta"]`, `default_repo: "newest"`, `public_addr: "127.0.0.1:9090"`,
		`require_auth: true`, `base_url: "` + host.URL + `/api/v3/"`, `catch_all: true`} {
		if !strings.Contains(string(yaml), want) {
			t.Errorf("personal.yaml lacks %q:\n%s", want, yaml)
		}
	}
	for _, f := range []string{"personal.yaml", "session.key", "owner.token", "scp.db"} { // the store too: the directory is 0700, and so is it
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v %v", f, st, err)
		}
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Errorf("directory %v", st.Mode().Perm())
	}
	if sec := credentialOf(t, dir); sec.Value != "ghp_personal" || sec.Source != host.URL {
		t.Errorf("credential: %v", sec)
	}
	// The token is nowhere in the clear: not in personal.yaml, the store files or what was printed.
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		if b, _ := os.ReadFile(f); bytes.Contains(b, []byte("ghp_personal")) {
			t.Errorf("%s holds the token", f)
		}
	}
	if strings.Contains(out.String(), "ghp_personal") {
		t.Errorf("the token was printed:\n%s", out)
	}
	cfg, _, err := config.Load(path, nil)
	if err != nil {
		t.Fatalf("personal.yaml does not load: %v", err)
	}
	if err := requireLoopback(cfg); err != nil {
		t.Error(err)
	}

	// The next run asks nothing: no input at all.
	p2, out2 := newPersonal(t, dir, "", "", personalFlags{})
	if _, err := p2.setup(t.Context(), dir); err != nil {
		t.Fatalf("second run: %v\n%s", err, out2)
	}
	if out2.Len() != 0 {
		t.Errorf("the second run said: %s", out2)
	}
}

func TestPersonalWizardWithACode(t *testing.T) {
	host := fakeHost(t, nil)
	dir := t.TempDir()
	// The accounts question: Enter for all.
	p, out := newPersonal(t, dir, host.URL, "\n", personalFlags{clientID: "Iv1.abc", scope: "repo read:org"})
	if _, err := p.setup(t.Context(), dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"AAAA-BBBB", "http://host/login/device", "ends on 2026-11-07"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if sec := credentialOf(t, dir); sec.ExpiresAt.IsZero() {
		t.Error("the end of the token was not kept")
	}
}

func TestPersonalWizardRefuses(t *testing.T) {
	host := fakeHost(t, nil)
	for name, tc := range map[string]struct{ input, host string }{
		"a wrong token":   {"\nnope\n", host.URL},
		"no token":        {"\n\n", host.URL},
		"a bad host":      {"", "http://ghe.example.org"},
		"an unknown pick": {"\nghp_personal\n9\n", host.URL},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p, _ := newPersonal(t, dir, tc.host, tc.input, personalFlags{})
			if _, err := p.setup(t.Context(), dir); err == nil {
				t.Error("accepted")
			}
			if _, err := os.Stat(filepath.Join(dir, "personal.yaml")); err == nil {
				t.Error("personal.yaml written for a failed sign-in")
			}
		})
	}
}

func TestPersonalSignsInAgainWhenTheCredentialEnded(t *testing.T) {
	host := fakeHost(t, map[string]string{"github-authentication-token-expiration": "2026-11-07 11:00:00 UTC"})
	dir := t.TempDir()
	p, _ := newPersonal(t, dir, host.URL, "\nghp_personal\n\n", personalFlags{})
	if _, err := p.setup(t.Context(), dir); err != nil { // the header says the token ended an hour ago
		t.Fatal(err)
	}
	if sec := credentialOf(t, dir); !sec.Expired(p.clock()) {
		t.Fatalf("the end was not read from the header: %v", sec.ExpiresAt)
	}
	cfg, _, _ := config.Load(filepath.Join(dir, "personal.yaml"), nil)
	if err := p.resolve(t.Context(), dir, &cfg); err == nil || !strings.Contains(err.Error(), "ended") {
		t.Errorf("an ended credential was resolved: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "personal.yaml"))
	// Run again: it signs in again at the same host, and leaves personal.yaml as the person may have edited it.
	p2, out := newPersonal(t, dir, "", "\nghp_personal\n", personalFlags{})
	p2.now = func() time.Time { return time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) } // before the end: the credential is current
	if _, err := p2.setup(t.Context(), dir); err != nil || out.Len() != 0 {
		t.Fatalf("while current: %v %s", err, out)
	}
	p3, out3 := newPersonal(t, dir, "", "\nghp_personal\n", personalFlags{})
	if _, err := p3.setup(t.Context(), dir); err != nil || !strings.Contains(out3.String(), "ended on") {
		t.Fatalf("after the end: %v\n%s", err, out3)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "personal.yaml")); !bytes.Equal(before, after) {
		t.Error("personal.yaml was rewritten by a new sign-in")
	}
}

func TestPersonalResetKeepsTheOldFile(t *testing.T) {
	host := fakeHost(t, nil)
	dir := t.TempDir()
	p, _ := newPersonal(t, dir, host.URL, "\nghp_personal\n\n", personalFlags{})
	path, err := p.setup(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(path)
	p2, _ := newPersonal(t, dir, host.URL, "\nghp_personal\n1\n", personalFlags{reset: true, port: 9190})
	if _, err := p2.setup(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if bak, _ := os.ReadFile(path + ".bak"); !bytes.Equal(bak, old) {
		t.Error("the old personal.yaml was not kept")
	}
	if now, _ := os.ReadFile(path); !strings.Contains(string(now), `"127.0.0.1:9190"`) || !strings.Contains(string(now), `orgs: ["octo"]`) {
		t.Errorf("reset:\n%s", now)
	}
}

func TestRequireLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{"127.0.0.1:9090": true, "[::1]:9090": true, "localhost:9090": true, ":9090": false,
		"0.0.0.0:9090": false, "192.168.1.5:9090": false, "example.org:9090": false} {
		cfg := config.Config{Server: config.Server{PublicAddr: addr, AdminAddr: "127.0.0.1:9091"}}
		if err := requireLoopback(cfg); (err == nil) != ok {
			t.Errorf("%q: %v", addr, err)
		}
	}
}

func TestServeRefusesTheVaultsCredential(t *testing.T) {
	cfg := config.Config{GitHub: config.GitHub{Sources: []config.Source{{ID: "github", Credential: true}}}}
	if _, err := newServer(t.Context(), cfg, deps{}, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "scp personal") {
		t.Errorf("serve took a vault credential: %v", err)
	}
}

// TestPersonalPortal is the acceptance test: the portal that scp personal starts reads the private repository the person can read,
// refuses everyone without a session, and the launch link signs the owner in once.
func TestPersonalPortal(t *testing.T) {
	gh := githubtest.New(t)
	gh.AddToken("ghp_test")
	gh.AddRepo("acme", gogh.Repository{Name: new("open"), Visibility: new("public")})
	gh.AddRepo("acme", gogh.Repository{Name: new("secret"), Visibility: new("private")})
	dir := t.TempDir()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	// What the wizard writes, with the fake GitHub as the host and a free port.
	p, _ := newPersonal(t, dir, "", "", personalFlags{})
	st, err := store.Open(t.Context(), filepath.Join(dir, "scp.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := vault.OpenKey(dir, vault.Keychain(dir))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vault.New(key, st)
	if err := v.Put(t.Context(), vault.Secret{ID: personalSource, Source: "https://github.com", Kind: "token", Value: "ghp_test"}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if err := p.secrets(dir); err != nil {
		t.Fatal(err)
	}
	port := addr[strings.LastIndex(addr, ":")+1:]
	n := 0
	fmt.Sscan(port, &n)
	yaml := personalYAML(dir, ghHost(gh.BaseURL()), []string{"acme"}, "open", "", n)
	cfgPath := filepath.Join(dir, "personal.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "layout"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.AdminAddr = "127.0.0.1:0"
	if err := p.resolve(t.Context(), dir, &cfg); err != nil {
		t.Fatal(err)
	}
	d := testDeps(t)
	if d.launch, err = newLaunch(time.Now()); err != nil { // the auth package reads the real clock
		t.Fatal(err)
	}
	srv, err := newServer(t.Context(), cfg, d, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run: %v", err)
		}
	}()

	get := func(c *http.Client, path string) (int, string) {
		t.Helper()
		resp, err := c.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	anon := &http.Client{Timeout: 5 * time.Second}
	defer owner.CloseIdleConnections()
	defer anon.CloseIdleConnections()

	// Nobody sees anything without a session, whatever the path.
	for _, path := range []string{"/api/v1/repos", "/api/v1/dashboard", "/llms.txt", "/api/v1/admin/status"} {
		if code, b := get(anon, path); code != http.StatusUnauthorized || strings.Contains(b, "secret") {
			t.Errorf("anonymous %s: %d %s", path, code, b)
		}
	}
	if _, b := get(owner, "/auth/launch?code=wrong"); strings.Contains(b, "secret") {
		t.Error("a wrong launch code showed the repositories")
	}

	// The link signs the owner in, once.
	link := strings.TrimPrefix(launchLink(srv.publicLn.Addr(), d.launch.Code), "http://"+addr)
	get(owner, link)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, b := get(owner, "/api/v1/repos"); strings.Contains(b, `"acme/secret"`) && strings.Contains(b, `"acme/open"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the owner does not see the private repository within 10s")
		}
	}
	var me struct {
		SignedIn, Admin, Private bool
	}
	_, b := get(owner, "/api/v1/me")
	if err := json.Unmarshal([]byte(b), &me); err != nil || !me.SignedIn || !me.Admin || !me.Private {
		t.Errorf("the owner: %s %v", b, err)
	}
	if code, _ := get(owner, "/api/v1/admin/status"); code != http.StatusOK {
		t.Errorf("the owner's status: %d", code)
	}
	jar2, _ := cookiejar.New(nil)
	another := &http.Client{Jar: jar2, Timeout: 5 * time.Second}
	defer another.CloseIdleConnections()
	get(another, link)
	if code, b := get(another, "/api/v1/repos"); code != http.StatusUnauthorized || strings.Contains(b, "secret") {
		t.Errorf("a used link signed another browser in: %d %s", code, b)
	}
}

// ghHost is the Host whose REST base is base (the fake GitHub's).
func ghHost(base string) ghlogin.Host { return ghlogin.Host{Web: "https://github.com", API: base} }
