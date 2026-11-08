package setup

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeApp is GitHub's App for a test: its key and where it is installed (account logins, set by the test).
type fakeApp struct {
	key       *rsa.PrivateKey
	pem       string
	installed atomic.Pointer[[]string]
}

func newFakeApp() *fakeApp {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	return &fakeApp{key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))}
}

// appRoutes is GitHub's side of the manifest flow: the code's conversion and the App's installations, which only the App's own token
// (RS256, issuer 42) may read.
func (a *fakeApp) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v3/app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("code") != "good-code" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		pemKey := a.pem
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "slug": "scp-acme-ab12", "name": "scp-acme-ab12", "pem": pemKey, "webhook_secret": "whsec",
			"owner": map[string]string{"login": "acme"}})
	})
	mux.HandleFunc("GET /api/v3/app/installations", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		key := a.key
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var c struct {
			Iss string `json:"iss"`
			Exp int64  `json:"exp"`
			Iat int64  `json:"iat"`
		}
		_ = json.Unmarshal(claims, &c)
		if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig) != nil || c.Iss != "42" || c.Exp-c.Iat > 600 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var out []map[string]any
		if on := a.installed.Load(); on != nil {
			for _, a := range *on {
				out = append(out, map[string]any{"account": map[string]string{"login": a}})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func TestTheAppFlow(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": token})

	if code, _ := r.call(t, http.MethodPost, "/setup/api/app/start", map[string]string{"account": "stranger"}); code != http.StatusBadRequest {
		t.Errorf("an account that is not the person's: %d", code)
	}
	code, out := r.call(t, http.MethodPost, "/setup/api/app/start", map[string]string{"account": "acme"})
	if code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	action, _ := out["action"].(string)
	u, err := url.Parse(action)
	if err != nil || !strings.HasPrefix(u.Path, "/organizations/acme/settings/apps/new") || u.Query().Get("state") == "" {
		t.Fatalf("the manifest goes to %q", action)
	}
	state := u.Query().Get("state")
	var m struct {
		Perms  map[string]string    `json:"default_permissions"`
		Events []string             `json:"default_events"`
		Hook   struct{ URL string } `json:"hook_attributes"`
		Redir  string               `json:"redirect_url"`
		Public bool                 `json:"public"`
	}
	if err := json.Unmarshal([]byte(out["manifest"].(string)), &m); err != nil {
		t.Fatal(err)
	}
	for p, v := range m.Perms {
		if v != "read" {
			t.Errorf("the App asks for %s %s: only reading is allowed", p, v)
		}
	}
	for _, want := range []string{"contents", "metadata", "issues", "pull_requests"} {
		if m.Perms[want] != "read" {
			t.Errorf("the App lacks the %s permission: %v", want, m.Perms)
		}
	}
	if !strings.Contains(strings.Join(m.Events, " "), "push") || !strings.Contains(strings.Join(m.Events, " "), "pull_request") || m.Public {
		t.Errorf("events %v, public %v", m.Events, m.Public)
	}
	if !strings.HasPrefix(m.Redir, "http://"+r.s.cfg.Addr+"/setup/app/callback") || !strings.HasPrefix(m.Hook.URL, "http://"+r.s.cfg.Addr+"/") {
		t.Errorf("the App returns to %q and hooks %q", m.Redir, m.Hook.URL)
	}
	// The own account's App is made in the person's settings.
	if _, o := r.call(t, http.MethodPost, "/setup/api/app/start", map[string]string{"account": "octo"}); !strings.Contains(fmt.Sprint(o["action"]), "/settings/apps/new?state=") || strings.Contains(fmt.Sprint(o["action"]), "/organizations/") {
		t.Errorf("own account: %v", o["action"])
	}

	// GitHub sends the person back: no cookie travels from another site, so the state alone must do it, and only once.
	bare := &http.Client{Transport: noKeepAlive}
	get := func(q string) (int, string) {
		res, err := bare.Get(r.URL + "/setup/app/callback?" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get("code=good-code&state=wrong"); code != http.StatusBadRequest || !strings.Contains(body, "not the one") {
		t.Errorf("a state nobody made: %d %s", code, body)
	}
	if code, body := get("code=bad-code&state=" + url.QueryEscape(state)); code != http.StatusBadRequest || strings.Contains(body, "BEGIN") {
		t.Errorf("a code GitHub refuses: %d %s", code, body)
	}
	_, o := r.call(t, http.MethodPost, "/setup/api/app/start", map[string]string{"account": "acme"})
	u, _ = url.Parse(o["action"].(string))
	state = u.Query().Get("state")
	code, body := get("code=good-code&state=" + url.QueryEscape(state))
	if code != 200 || !strings.Contains(body, "/setup/callback.js") || !strings.Contains(body, `content="`+r.token+`"`) {
		t.Fatalf("callback: %d %s", code, body)
	}
	if code, _ := get("code=good-code&state=" + url.QueryEscape(state)); code != http.StatusBadRequest {
		t.Errorf("a state works once: %d", code)
	}

	// The App exists but is not installed: events are not on yet.
	_, st := r.call(t, http.MethodGet, "/setup/api/state", nil)
	apps, _ := st["apps"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["status"] != "created" || !strings.HasSuffix(apps[0].(map[string]any)["installUrl"].(string), "/apps/scp-acme-ab12/installations/new") {
		t.Fatalf("apps: %v", st["apps"])
	}
	r.call(t, http.MethodPost, "/setup/api/app/check", map[string]string{"account": "acme"})
	if _, st = r.call(t, http.MethodGet, "/setup/api/state", nil); st["apps"].([]any)[0].(map[string]any)["status"] != "created" {
		t.Errorf("not installed yet: %v", st["apps"])
	}
	r.app.installed.Store(&[]string{"acme"})
	r.call(t, http.MethodPost, "/setup/api/app/check", map[string]string{"account": "acme"})
	if _, st = r.call(t, http.MethodGet, "/setup/api/state", nil); st["apps"].([]any)[0].(map[string]any)["status"] != "installed" {
		t.Errorf("installed: %v", st["apps"])
	}

	// Finishing hands the installed App's key to Finish; the page never saw it.
	accounts := []string{"octo", "acme"}
	r.list(t, accounts...)
	if code, o := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": accounts, "products": []Product{}}); code != 200 {
		t.Fatalf("finish: %d %v", code, o)
	}
	res := <-r.finished
	if len(res.Apps) != 1 || res.Apps[0].Account != "acme" || res.Apps[0].ID != 42 || !strings.Contains(res.Apps[0].Key, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("apps: %+v", res.Apps)
	}
	if strings.Contains(r.bodies.String(), "PRIVATE KEY") {
		t.Error("the page was shown a private key")
	}
}

func TestAnAppNotInstalledIsPolled(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	r.call(t, http.MethodPost, "/setup/api/token", map[string]string{"token": token})
	_, o := r.call(t, http.MethodPost, "/setup/api/app/start", map[string]string{"account": "acme"})
	u, _ := url.Parse(o["action"].(string))
	res, err := (&http.Client{Transport: noKeepAlive}).Get(r.URL + "/setup/app/callback?code=good-code&state=" + url.QueryEscape(u.Query().Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	accounts := []string{"octo", "acme"}
	r.list(t, accounts...)
	if code, o := r.call(t, http.MethodPost, "/setup/api/finish", map[string]any{"accounts": accounts, "products": []Product{}}); code != 200 {
		t.Fatalf("finish: %d %v", code, o)
	}
	if got := <-r.finished; len(got.Apps) != 0 {
		t.Errorf("an App that is not installed gives no events: %+v", got.Apps)
	}
}

func TestTheManifestMayBePostedToTheHostOnly(t *testing.T) {
	r := newRig(t, "", nil)
	r.enter(t)
	req, _ := http.NewRequest(http.MethodGet, r.URL+"/setup/api/state", nil)
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' "+r.gh.URL+";") {
		t.Errorf("form-action: %s", csp)
	}
}
