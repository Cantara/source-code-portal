# Reference tests for the GitHub adapter unit (05-github-adapter)

Companion to `05-github-adapter.md` (Appendix B). Every listing was compiled and run on 2026-10-14 in the scratch module described there; the paths are those of section 1.4 of the main document.


## 1 `internal/github/auth/auth_test.go`

```go
package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
)

func TestJWTShapeAndSignature(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Unix(1_800_000_000, 0)
	j, err := SignAppJWT(k, "Iv23li", now)
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Split(j, ".")
	var c struct {
		Iat, Exp int64
		Iss      string
	}
	b, _ := base64.RawURLEncoding.DecodeString(p[1])
	_ = json.Unmarshal(b, &c)
	if c.Iat != now.Unix()-60 || c.Exp-c.Iat != 9*60 || c.Iss != "Iv23li" {
		t.Fatalf("%+v", c)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(p[2])
	d := sha256.Sum256([]byte(p[0] + "." + p[1]))
	if err := rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, d[:], sig); err != nil {
		t.Fatal(err)
	}
}

func TestParsePrivateKey(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	p1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	der8, _ := x509.MarshalPKCS8PrivateKey(k)
	p8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der8})
	for _, in := range [][]byte{p1, p8, []byte(strings.ReplaceAll(string(p1), "\n", `\n`)), []byte(base64.StdEncoding.EncodeToString(p1))} {
		if _, err := ParsePrivateKey(in); err != nil {
			t.Fatal(err)
		}
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	if _, err := ParsePrivateKey(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(small)})); err == nil {
		t.Fatal("1024 bit must be refused")
	}
}

func TestAppMintCoalescesAndCaches(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	var mints atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(401)
			return
		}
		mints.Add(1)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_" + strings.Repeat("x", 40), "expires_at": time.Now().Add(time.Hour), "repository_selection": "all"})
	}))
	defer srv.Close()
	app, err := NewApp(AppConfig{ClientID: "Iv", APIBase: srv.URL, Keys: StaticKeys{k}, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		tok, err := app.Token(context.Background(), Scope{Installation: 7}, time.Minute)
		if err != nil || tok.Kind != KindInstallation {
			t.Fatal(err)
		}
	}
	if mints.Load() != 1 {
		t.Fatalf("mints=%d", mints.Load())
	}
	cur, _ := app.Token(context.Background(), Scope{Installation: 7}, time.Minute)
	app.Reject(Scope{Installation: 7}, Token{}) // a stale token is not current: cache survives
	if again, _ := app.Token(context.Background(), Scope{Installation: 7}, time.Minute); again.Value.Reveal() != cur.Value.Reveal() || mints.Load() != 1 {
		t.Fatal("cache dropped by a stale Reject")
	}
	app.Reject(Scope{Installation: 7}, cur)
	_, _ = app.Token(context.Background(), Scope{Installation: 7}, time.Minute)
	if mints.Load() != 2 {
		t.Fatalf("mints=%d", mints.Load())
	}
}

func mintServer(t *testing.T, handler func(call int32, w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(n.Add(1), w, r) }))
	t.Cleanup(s.Close)
	return s, &n
}

func okToken(w http.ResponseWriter) {
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_" + strings.Repeat("y", 40), "expires_at": time.Now().Add(time.Hour)})
}

func TestMintTriesNextKeyOn401AndStopsOnSuspended(t *testing.T) {
	good, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv, calls := mintServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		// the fake of GitHub accepts only the good key: verify the RS256 signature
		jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p := strings.Split(jwt, ".")
		sig, _ := base64.RawURLEncoding.DecodeString(p[2])
		d := sha256.Sum256([]byte(p[0] + "." + p[1]))
		if rsa.VerifyPKCS1v15(&good.PublicKey, crypto.SHA256, d[:], sig) != nil {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"A JSON web token could not be decoded"}`))
			return
		}
		okToken(w)
	})
	app, _ := NewApp(AppConfig{ClientID: "Iv", APIBase: srv.URL, Keys: StaticKeys{bad, good}, HTTP: srv.Client()})
	if _, err := app.Token(context.Background(), Scope{Installation: 1}, 0); err != nil || calls.Load() != 2 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
	sus, _ := mintServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"message":"This installation has been suspended"}`))
	})
	app2, _ := NewApp(AppConfig{ClientID: "Iv", APIBase: sus.URL, Keys: StaticKeys{bad, good}, HTTP: sus.Client()})
	_, err := app2.Token(context.Background(), Scope{Installation: 1}, 0)
	var ae *Error
	if !errors.As(err, &ae) || !errors.Is(err, ErrInstallationUnavailable) {
		t.Fatalf("%v", err)
	}
}

func TestLeaderCancellationDoesNotFailOthers(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	release := make(chan struct{})
	srv, calls := mintServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) { <-release; okToken(w) })
	app, _ := NewApp(AppConfig{ClientID: "Iv", APIBase: srv.URL, Keys: StaticKeys{k}, HTTP: srv.Client()})
	leaderCtx, cancel := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() { _, err := app.Token(leaderCtx, Scope{Installation: 3}, 0); leaderErr <- err }()
	time.Sleep(50 * time.Millisecond) // the leader is inside the flight
	followerErr := make(chan error, 1)
	go func() { _, err := app.Token(context.Background(), Scope{Installation: 3}, 0); followerErr <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader: %v", err)
	}
	close(release)
	if err := <-followerErr; err != nil {
		t.Fatalf("follower failed because the leader was canceled: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestFileKeysKeepsPreviousOnBadRotation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/app.pem"
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	write := func(b []byte) {
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
	fk := &FileKeys{Paths: []string{path}, CheckEvery: time.Nanosecond}
	if s, err := fk.Signers(context.Background()); err != nil || len(s) != 1 {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	write([]byte("not a key, and longer than before"))
	s, err := fk.Signers(context.Background())
	if err != nil || len(s) != 1 {
		t.Fatalf("previous key must stay in service: %v", err)
	}
	if fk.LastReloadError() == nil {
		t.Fatal("the absorbed failure must be visible")
	}
	if err := fk.Reload(); err == nil {
		t.Fatal("explicit Reload reports the failure")
	}
}

func TestStaticAndExternal(t *testing.T) {
	if _, err := NewStatic(KindPAT, "x", secret.New("")); err == nil {
		t.Fatal("empty token is not a credential")
	}
	dir := t.TempDir()
	p := dir + "/tok"
	_ = os.WriteFile(p, []byte("ghp_one\n"), 0o600)
	st, err := NewStaticFile(KindPAT, "x", p)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := st.Token(context.Background(), Scope{}, 0); tok.Value.Reveal() != "ghp_one" {
		t.Fatal(tok.Value.Reveal())
	}
	time.Sleep(10 * time.Millisecond)
	_ = os.WriteFile(p, []byte("ghp_two\n"), 0o600)
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, future, future)
	if tok, _ := st.Token(context.Background(), Scope{}, 0); tok.Value.Reveal() != "ghp_two" {
		t.Fatal("rotated file not picked up")
	}
	var runs int
	ex, _ := NewExternal(ExternalConfig{Argv: []string{"/bin/tok"}, Run: func(context.Context, []string, time.Duration) ([]byte, error) {
		runs++
		return []byte(`{"token":"ghs_cmd","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`), nil
	}})
	for i := 0; i < 3; i++ {
		if tok, err := ex.Token(context.Background(), Scope{}, 0); err != nil || tok.Value.Reveal() != "ghs_cmd" {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Fatalf("runs=%d", runs)
	}
	if fmt.Sprint(secret.New("ghs_cmd")) != "[REDACTED]" {
		t.Fatal("Secret must not format")
	}
}
```

## 2 `internal/github/auth/synctest_test.go`

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// An installation token is reused until its remaining life drops under RefreshAt + jitter, then exactly one caller
// mints; the whole hour runs on virtual time (no wall-clock sleep).
func TestTokenRefreshOnVirtualTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k, _ := rsa.GenerateKey(rand.Reader, 2048)
		var mints atomic.Int32
		srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := mints.Add(1)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_" + strings.Repeat("v", 30) + string(rune('a'+n)), "expires_at": time.Now().Add(time.Hour)})
		}))
		app, err := NewApp(AppConfig{ClientID: "Iv", APIBase: srv.URL, Keys: StaticKeys{k}, HTTP: srv.Client(), Rand01: func() float64 { return 0.5 }})
		if err != nil {
			t.Fatal(err)
		}
		get := func() Token {
			tok, err := app.Token(context.Background(), Scope{Installation: 5}, 0)
			if err != nil {
				t.Fatal(err)
			}
			return tok
		}
		first := get()
		time.Sleep(49 * time.Minute) // 11 min left, margin is 10 min + 30 s jitter: still cached
		if again := get(); again.Value.Reveal() != first.Value.Reveal() || mints.Load() != 1 {
			t.Fatalf("remint too early, mints=%d", mints.Load())
		}
		time.Sleep(90 * time.Second) // 9.5 min left: below the margin
		if renewed := get(); renewed.Value.Reveal() == first.Value.Reveal() || mints.Load() != 2 {
			t.Fatalf("no early refresh, mints=%d", mints.Load())
		}
		// a burst after the next margin coalesces to a single mint
		time.Sleep(50 * time.Minute)
		done := make(chan struct{})
		for i := 0; i < 20; i++ {
			go func() { get(); done <- struct{}{} }()
		}
		for i := 0; i < 20; i++ {
			<-done
		}
		if mints.Load() != 3 {
			t.Fatalf("burst minted %d times", mints.Load()-2)
		}
	})
}
```

## 3 `internal/github/github_test.go`

```go
package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

func TestSignVector(t *testing.T) { // GitHub's published vector
	got := SignSHA256(secret.New("It's a Secret to Everybody"), []byte("Hello, World!"))
	if got != "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17" {
		t.Fatal(got)
	}
}

func TestBlobSHA(t *testing.T) {
	if got := BlobSHA([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" { // git hash-object
		t.Fatal(got)
	}
}

func verifier() *Verifier {
	return NewVerifier(1<<20, map[scm.SourceID]VerifierSource{"acme": {
		Secrets: []secret.Secret{secret.New("new"), secret.New("old")}, AppID: 42,
		KnownInstall: func(id scm.InstallationID) bool { return id == 7 },
	}})
}

func hdr(body []byte, sec, event string) http.Header {
	h := http.Header{}
	h.Set("X-Hub-Signature-256", SignSHA256(secret.New(sec), body))
	h.Set("X-GitHub-Event", event)
	h.Set("X-GitHub-Delivery", "d-1")
	h.Set("Content-Type", "application/json")
	return h
}

func TestVerifyPushAndRotation(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main","before":"a","after":"b","commits":[{"id":"1","added":["README.md"],"removed":[],"modified":["pom.xml"]},{"id":"2","added":[],"removed":[],"modified":["pom.xml","src/x.go"]}],
	"head_commit":{"id":"2","tree_id":"tt"},"repository":{"id":9,"name":"api","owner":{"login":"acme"}},"installation":{"id":7}}`)
	for _, sec := range []string{"new", "old"} { // current and previous secret both verify
		ev, err := verifier().Verify("acme", hdr(body, sec, "push"), body)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind != scm.EventPush || ev.CommitCount != 2 || !ev.PathsComplete || ev.HeadTreeOID != "tt" || len(ev.ChangedPaths) != 3 || ev.Repo.ID != 9 || ev.Installation != 7 {
			t.Fatalf("%+v", ev)
		}
	}
	if _, err := verifier().Verify("acme", hdr(body, "wrong", "push"), body); err == nil {
		t.Fatal("expected mismatch")
	} else if we := new(WebhookError); !errors.As(err, &we) || !we.InvalidSignature() {
		t.Fatal(err)
	}
}

func TestVerifyPathListsAbsent(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main","commits":[{"id":"1"}],"head_commit":{"id":"1"},"repository":{"id":9,"name":"api","owner":{"login":"acme"}},"installation":{"id":7}}`)
	ev, err := verifier().Verify("acme", hdr(body, "new", "push"), body)
	if err != nil || ev.PathsComplete {
		t.Fatalf("%v %+v", err, ev)
	}
}

func TestVerifyIgnoredAndMalformed(t *testing.T) {
	body := []byte(`{"repository":{"id":9,"name":"api","owner":{"login":"acme"}},"installation":{"id":8}}`)
	_, err := verifier().Verify("acme", hdr(body, "new", "push"), body) // unknown installation 8
	if we := new(WebhookError); !errors.As(err, &we) || !we.Ignored() || we.Reason() != ReasonUnknownInstall {
		t.Fatal(err)
	}
	body = []byte(`{"installation":{"id":7}}`)
	_, err = verifier().Verify("acme", hdr(body, "new", "delete"), body)
	if we := new(WebhookError); !errors.As(err, &we) || !we.Ignored() {
		t.Fatal(err)
	}
	h := hdr(body, "new", "ping")
	h.Del("X-Hub-Signature-256")
	_, err = verifier().Verify("acme", h, body)
	if we := new(WebhookError); !errors.As(err, &we) || !we.InvalidSignature() {
		t.Fatal(err)
	}
	h = hdr(body, "new", "ping")
	h.Set("X-GitHub-Hook-Installation-Target-ID", "99")
	_, err = verifier().Verify("acme", h, body)
	if we := new(WebhookError); !errors.As(err, &we) || we.Reason() != ReasonTargetMismatch {
		t.Fatal(err)
	}
}

func TestClassify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	sec := func(time.Time) time.Duration { return 60 * time.Second }
	h := func(kv ...string) http.Header {
		hh := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			hh.Set(kv[i], kv[i+1])
		}
		return hh
	}
	cases := []struct {
		name  string
		f     failure
		pause bool
		perm  bool
		gone  bool
		is    error
		code  string
		wait  time.Duration
	}{
		{"primary", failure{Status: 403, Header: h("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1800000120"), Body: []byte(`{"message":"API rate limit exceeded"}`)}, true, false, false, scm.ErrUnavailable, "rate_primary", 120 * time.Second},
		{"secondary retry-after", failure{Status: 429, Header: h("Retry-After", "37")}, true, false, false, scm.ErrUnavailable, "rate_secondary", 37 * time.Second},
		{"secondary message", failure{Status: 403, Header: h("X-RateLimit-Remaining", "4000"), Body: []byte(`{"message":"You have exceeded a secondary rate limit"}`)}, true, false, false, scm.ErrUnavailable, "rate_secondary", 60 * time.Second},
		{"permission", failure{Status: 403, Header: h(), Body: []byte(`{"message":"Resource not accessible by integration"}`)}, false, true, false, scm.ErrForbidden, "permission_missing", 0},
		{"blocked", failure{Status: 403, Header: h(), Body: []byte(`{"message":"Repository access blocked"}`)}, false, true, false, scm.ErrForbidden, "forbidden", 0},
		{"repo 404", failure{Status: 404, Subject: SubjectRepository}, false, false, true, scm.ErrNotFound, "", 0},
		{"file 404", failure{Status: 404, Subject: SubjectFile}, false, false, false, scm.ErrNotFound, "", 0},
		{"503", failure{Status: 503}, false, false, false, scm.ErrUnavailable, "", 0},
		{"410", failure{Status: 410}, false, true, false, nil, "status_410", 0},
	}
	for _, c := range cases {
		c.f.Now, c.f.Secondary, c.f.Op = now, sec, "x"
		if c.f.Header == nil {
			c.f.Header = h()
		}
		err := classify(c.f)
		at, isPause := asRateLimited(err)
		if isPause != c.pause || isPermanent(err) != c.perm || isGone(err) != c.gone {
			t.Errorf("%s: pause=%v perm=%v gone=%v (%v)", c.name, isPause, isPermanent(err), isGone(err), err)
		}
		if c.is != nil && !errors.Is(err, c.is) {
			t.Errorf("%s: not Is %v", c.name, c.is)
		}
		if c.pause && at.Sub(now) != c.wait {
			t.Errorf("%s: wait %v want %v", c.name, at.Sub(now), c.wait)
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != c.code {
			t.Errorf("%s: code %+v", c.name, e)
		}
	}
	// a plain transient error must NOT look like a pause
	if _, ok := asRateLimited(classify(failure{Status: 502, Header: h(), Now: now, Secondary: sec})); ok {
		t.Error("502 must not be a pause")
	}
}

func TestGate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	rs := newRateState(DefaultConfig().Defaults.Budget.Reserve)
	hh := http.Header{}
	hh.Set("X-RateLimit-Resource", "core")
	hh.Set("X-RateLimit-Limit", "5000")
	hh.Set("X-RateLimit-Remaining", "1500") // 30%
	hh.Set("X-RateLimit-Used", "3500")
	hh.Set("X-RateLimit-Reset", "1800001800")
	rs.observe("acme", 7, hh, now, "core")
	if rs.admit(ClassBackfill, now) == 0 { // needs > 40%
		t.Error("backfill must wait at 30%")
	}
	if rs.admit(ClassScheduled, now) != 0 || rs.admit(ClassWebhook, now) != 0 {
		t.Error("scheduled and webhook proceed at 30%")
	}
	hh.Set("X-RateLimit-Remaining", "0")
	rs.observe("acme", 7, hh, now, "core")
	if w := rs.admit(ClassInteractive, now); w != maxGateWait {
		t.Errorf("empty bucket waits (capped): %v", w)
	}
	if w := rs.hardWait("acme", 7, "core", now); w != 30*time.Minute {
		t.Errorf("hard wait %v", w)
	}
	if got := rs.secondaryWait("acme", now); got != time.Minute {
		t.Error(got)
	}
	if got := rs.secondaryWait("acme", now); got != 2*time.Minute {
		t.Error(got)
	}
	rs.success("acme")
	if got := rs.secondaryWait("acme", now); got != time.Minute {
		t.Error(got)
	}
}

func TestConfigValidate(t *testing.T) {
	c := DefaultConfig()
	c.Sources = []SourceConfig{{ID: "Bad_ID", Host: HostConfig{Kind: HostGHES}, Auth: AuthConfig{Kind: AuthGitHubApp}, Discovery: DiscoveryConfig{Mode: "installation"}}}
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatal(err)
	}
	t.Log(err)
}

func TestLinkAndCursor(t *testing.T) {
	base, _ := url.Parse("https://ghe.example/api/v3")
	h := http.Header{}
	h.Add("Link", `<https://ghe.example/api/v3/organizations/5/repos?per_page=100&page=2>; rel="next", <https://ghe.example/api/v3/organizations/5/repos?per_page=100&page=9>; rel="last"`)
	p, ok, err := nextLink(h, base)
	if err != nil || !ok || p != "/organizations/5/repos?per_page=100&page=2" {
		t.Fatal(p, ok, err)
	}
	h = http.Header{}
	h.Add("Link", `<https://evil.example/x?page=2>; rel="next"`)
	if _, _, err := nextLink(h, base); err == nil {
		t.Fatal("foreign origin must be refused")
	}
	c := encodeCursor(cursor{I: 2, P: p})
	back, err := decodeCursor(c)
	if err != nil || back.P != p || back.I != 2 {
		t.Fatal(back, err)
	}
	if _, err := decodeCursor(encodeCursor(cursor{P: "//evil.example/x"})); err == nil {
		t.Fatal("cursor must not address another host")
	}
}

func TestEscapeRepoPath(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "a/../b", "a//b", `a\b`, "a/\x00", "./x"} {
		if _, err := escapeRepoPath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	got, err := escapeRepoPath("docs/my file#1.md")
	if err != nil || got != "docs/my%20file%231.md" {
		t.Fatal(got, err)
	}
}

func TestHeadsQueryShape(t *testing.T) {
	q := buildHeadsQuery(3)
	for _, want := range []string{"$o2: String!, $n2: String!", "r1: repository(owner: $o1, name: $n1)", "fragment ScpHead on Repository", "issues(first: 1, states: OPEN)"} {
		if !strings.Contains(q, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func validApp() SourceConfig {
	return SourceConfig{
		ID:        "acme",
		Auth:      AuthConfig{Kind: AuthGitHubApp, ClientID: "Iv1.x", PrivateKey: SecretRef{File: "/run/secrets/app.pem"}, Installations: InstallSet{Accounts: []string{"acme-org"}}},
		Webhook:   SourceWebhook{Secret: SecretRef{File: "/run/secrets/hook"}},
		Discovery: DiscoveryConfig{Mode: "installation"},
	}
}

func TestConfigDefaultsAndValid(t *testing.T) {
	c := DefaultConfig()
	c.Sources = []SourceConfig{validApp()}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	s := c.Sources[0]
	if s.Host.Kind != HostGitHubCom || s.Discovery.Visibility != "public_only" || s.Discovery.IncludeArchived == nil || !*s.Discovery.IncludeArchived ||
		s.Discovery.ReconcileEvery != 30*time.Minute || s.Discovery.MaxRemovalRatio != 0.3 || s.Acquisition.Mode != "api" || s.Acquisition.GraphQL != "auto" {
		t.Fatalf("%+v", s)
	}
	before := c
	c.ApplyDefaults()
	if c.Defaults != before.Defaults || c.Sources[0].Discovery.ReconcileEvery != before.Sources[0].Discovery.ReconcileEvery {
		t.Fatal("ApplyDefaults is not idempotent")
	}
	f := false
	c.Sources[0].Discovery.IncludeArchived = &f
	c.ApplyDefaults()
	if *c.Sources[0].Discovery.IncludeArchived {
		t.Fatal("an explicit false was overwritten")
	}
}

func TestConfigRejects(t *testing.T) {
	cases := map[string]func(*SourceConfig){
		"github.sources[0].auth.private_key":            func(s *SourceConfig) { s.Auth.PrivateKey = SecretRef{} },
		"github.sources[0].webhook.secret":              func(s *SourceConfig) { s.Webhook.Secret = SecretRef{} },
		"github.sources[0].auth.installations.accounts": func(s *SourceConfig) { s.Auth.Installations.Accounts = nil },
		"github.sources[0].auth.dev":                    func(s *SourceConfig) { s.Auth = AuthConfig{Kind: AuthAnonymous} },
		"github.sources[0].auth.command":                func(s *SourceConfig) { s.Auth = AuthConfig{Kind: AuthCommand, Command: []string{"token-helper"}} },
		"github.sources[0].host":                        func(s *SourceConfig) { s.Host.WebURL = "https://x" },
		"github.sources[0].acquisition.mode":            func(s *SourceConfig) { s.Acquisition.Mode = "git" },
		"github.sources[0].discovery.org":               func(s *SourceConfig) { s.Discovery.Mode = "org" },
		"github.sources[0].discovery.access_acknowledgement": func(s *SourceConfig) {
			s.Discovery.Visibility, s.Discovery.AccessAcknowledgement = "installation_scope", "whatever"
		},
		"github.sources[0].webhook.redelivery.enabled": func(s *SourceConfig) {
			s.Auth = AuthConfig{Kind: AuthPAT, Token: SecretRef{File: "/t"}}
			s.Discovery = DiscoveryConfig{Mode: "org", Org: "x"}
			s.Webhook.Redelivery.Enabled = true
		},
		"github.sources[0].host.web_url": func(s *SourceConfig) { s.Host = HostConfig{Kind: HostGHES, WebURL: "http://ghe.example"} },
	}
	for path, mutate := range cases {
		c := DefaultConfig()
		s := validApp()
		mutate(&s)
		c.Sources = []SourceConfig{s}
		err := c.Validate()
		found := false
		for _, e := range splitJoined(err) {
			var ve *ValidationError
			if errors.As(e, &ve) && ve.Path == path {
				found = true
			}
		}
		if !found {
			t.Errorf("want violation at %s, got %v", path, err)
		}
	}
	// all violations are reported together, not only the first
	c := DefaultConfig()
	c.Sources = []SourceConfig{{ID: "Bad_ID"}, {ID: "Bad_ID"}}
	if n := len(splitJoined(c.Validate())); n < 4 {
		t.Fatalf("expected many violations, got %d", n)
	}
}

func splitJoined(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

func TestConfigDefaultsSection(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Defaults.Budget.Reserve = ReserveConfig{Backfill: 0.02, Scheduled: 0.15, Webhook: 0.4}
	c.Defaults.APIVersion = "latest"
	c.Defaults.Limits.HeadBatch = 500
	paths := map[string]bool{}
	for _, e := range splitJoined(c.Validate()) {
		var ve *ValidationError
		if errors.As(e, &ve) {
			paths[ve.Path] = true
		}
	}
	for _, want := range []string{"github.defaults.budget.reserve", "github.defaults.api_version", "github.defaults.limits.head_batch"} {
		if !paths[want] {
			t.Errorf("missing %s in %v", want, paths)
		}
	}
	var p Percent
	if err := p.UnmarshalText([]byte("40%")); err != nil || p != 0.4 {
		t.Fatal(p, err)
	}
	var b ByteSize
	if err := b.UnmarshalText([]byte("25MiB")); err != nil || b != 25<<20 {
		t.Fatal(b, err)
	}
	if err := b.UnmarshalText([]byte("-1")); err == nil {
		t.Fatal("negative size accepted")
	}
}

func TestRedactTokensAndExcerpt(t *testing.T) {
	long := "ghs_" + strings.Repeat("A1b2C3", 87) // about 520 characters, as installation tokens are since 2026
	for _, tok := range []string{long, "ghp_" + strings.Repeat("a", 36), "github_pat_" + strings.Repeat("B", 40), "gho_" + strings.Repeat("c", 36)} {
		in := `{"message":"Bad credentials for ` + tok + ` end"}`
		got := excerpt([]byte(in))
		if strings.Contains(got, tok[:16]) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("not redacted: %q", got)
		}
	}
	if got := excerpt([]byte(`{"message":"` + strings.Repeat("x", 500) + `"}`)); len(got) != 200 {
		t.Fatalf("len=%d", len(got))
	}
	if got := excerpt([]byte("line1\nline2\x00")); strings.ContainsAny(got, "\n\x00") {
		t.Fatalf("control characters kept: %q", got)
	}
}

// A canary token sent through an error must never reach Error(), the Message field, or a formatted PauseError.
func TestErrorsCarryNoCredential(t *testing.T) {
	canary := "ghs_" + strings.Repeat("Z9", 30)
	h := http.Header{}
	h.Set("Retry-After", "30")
	err := classify(failure{Op: "commits", Status: 403, Header: h, Body: []byte(`{"message":"secondary rate limit ` + canary + `"}`), Now: time.Unix(1000, 0), Secondary: func(time.Time) time.Duration { return time.Minute }})
	var pe *PauseError
	if !errors.As(err, &pe) {
		t.Fatalf("%T", err)
	}
	for _, s := range []string{err.Error(), pe.Cause.Message, fmtV(err)} {
		if strings.Contains(s, canary[:12]) {
			t.Fatalf("credential leaked: %q", s)
		}
	}
}

func fmtV(v any) string { return fmt.Sprintf("%v %+v %#v", v, v, v) }

// Regression cases found by FuzzNextLink: a Link must stay under the API base path and on the API origin.
func TestNextLinkRejectsLookalikes(t *testing.T) {
	base, _ := url.Parse("https://ghe.example/api/v3")
	for _, link := range []string{`</api/v30>; rel="next"`, `<//evil.example/api/v3/x>; rel="next"`, `<x?page=2>; rel="next"`, `<https://ghe.example:8443/api/v3/x>; rel="next"`, `<https://u@ghe.example/api/v3/x>; rel="next"`} {
		h := http.Header{}
		h.Add("Link", link)
		if rel, ok, err := nextLink(h, base); err == nil {
			t.Errorf("%s accepted as %q (%v)", link, rel, ok)
		}
	}
	h := http.Header{}
	h.Add("Link", `</api/v3/organizations/5/repos?page=2>; rel="next"`)
	if rel, ok, err := nextLink(h, base); err != nil || !ok || rel != "/organizations/5/repos?page=2" {
		t.Fatal(rel, ok, err)
	}
}

// Local copies of the job behaviour interfaces (architecture 8.3). A test of package github may import only what github
// may import (matrix 5.2), and job is not in that row, so the tests assert the same structural contract locally.
type (
	rateLimitedErr interface {
		error
		RetryAt() time.Time
	}
	permanentErr interface {
		error
		Permanent() bool
	}
	goneErr interface {
		error
		Gone() bool
	}
)

func asRateLimited(err error) (time.Time, bool) {
	var r rateLimitedErr
	if errors.As(err, &r) {
		return r.RetryAt(), true
	}
	return time.Time{}, false
}
func isPermanent(err error) bool {
	var p permanentErr
	return errors.As(err, &p) && p.Permanent()
}
func isGone(err error) bool {
	var g goneErr
	return errors.As(err, &g) && g.Gone()
}
```

## 4 `internal/github/fuzz_test.go`

```go
package github

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/scm"
)

// FuzzVerify: whatever the body and headers, Verify never panics, never returns an event together with an error, and
// returns an event only for a correctly signed body (the signature is computed by the fuzz target, so every input
// reaches the decoder; a second pass with a wrong secret must never yield an event).
func FuzzVerify(f *testing.F) {
	f.Add([]byte(`{"ref":"refs/heads/main","commits":[{"id":"1","added":["a"],"removed":[],"modified":[]}],"repository":{"id":1,"name":"r","owner":{"login":"o"}}}`), "push")
	f.Add([]byte(`{`), "push")
	f.Add([]byte(`{"repository":{"id":1,"name":"../x","owner":{"login":"o"}}}`), "release")
	f.Add([]byte(`null`), "ping")
	v := NewVerifier(1<<20, map[scm.SourceID]VerifierSource{"s": {Secrets: []secret.Secret{secret.New("k")}}})
	f.Fuzz(func(t *testing.T, body []byte, event string) {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", SignSHA256(secret.New("k"), body))
		h.Set("X-GitHub-Event", event)
		h.Set("X-GitHub-Delivery", "g")
		ev, err := v.Verify("s", h, body)
		if err != nil && ev.Kind != "" {
			t.Fatalf("event and error together: %+v %v", ev, err)
		}
		if err == nil {
			if ev.Kind == "" {
				t.Fatal("success without a kind")
			}
			if ev.Repo.Owner != "" && !validOwnerName(ev.Repo.Owner) || ev.Repo.Name != "" && !validOwnerName(ev.Repo.Name) {
				t.Fatalf("unvalidated identity escaped: %+v", ev.Repo)
			}
			if len(ev.ChangedPaths) > maxChangedPaths {
				t.Fatal("unbounded paths")
			}
		}
		h.Set("X-Hub-Signature-256", SignSHA256(secret.New("other"), body))
		if _, err := v.Verify("s", h, body); err == nil {
			t.Fatal("wrong secret verified")
		}
	})
}

func FuzzEscapeRepoPath(f *testing.F) {
	for _, s := range []string{"README.md", "a/b", "../x", "a//b", "a b#c?d%e", "\x00", "ü/ö"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		esc, err := escapeRepoPath(p)
		if err != nil {
			return
		}
		u, perr := url.Parse("https://api.example/repos/o/r/contents/" + esc)
		if perr != nil || u.RawQuery != "" || u.Fragment != "" || u.Host != "api.example" {
			t.Fatalf("escaped path changes request meaning: %q -> %q", p, esc)
		}
		for _, seg := range strings.Split(u.Path, "/") {
			if seg == ".." || seg == "." {
				t.Fatalf("dot segment survived: %q -> %q", p, esc)
			}
		}
		if got, derr := url.PathUnescape(esc); derr != nil || got != p {
			t.Fatalf("round trip: %q -> %q -> %q", p, esc, got)
		}
	})
}

func FuzzDecodeCursor(f *testing.F) {
	f.Add(encodeCursor(cursor{I: 1, P: "/orgs/x/repos?page=2"}))
	f.Add("%%%")
	f.Fuzz(func(t *testing.T, s string) {
		c, err := decodeCursor(s)
		if err == nil && c.P != "" && (!strings.HasPrefix(c.P, "/") || strings.Contains(c.P, "//") || strings.Contains(c.P, "..")) {
			t.Fatalf("cursor addresses outside the base: %+v", c)
		}
	})
}

func FuzzNextLink(f *testing.F) {
	f.Add(`<https://ghe.example/api/v3/x?page=2>; rel="next"`)
	f.Add(`<//evil.example/x>; rel="next"`)
	f.Add(`garbage`)
	base, _ := url.Parse("https://ghe.example/api/v3")
	f.Fuzz(func(t *testing.T, line string) {
		h := http.Header{}
		h.Add("Link", line)
		rel, ok, err := nextLink(h, base)
		if err == nil && ok && !strings.HasPrefix(rel, "/") && rel != "" {
			t.Fatalf("relative target %q does not start with /", rel)
		}
	})
}

// Property: the gate is monotone in priority (a higher class never waits longer than a lower one) for any bucket state,
// and never exceeds maxGateWait.
func TestGateMonotoneProperty(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for limit := 1000; limit <= 12500; limit += 1500 {
		for rem := 0; rem <= limit; rem += limit / 13 {
			rs := newRateState(DefaultConfig().Defaults.Budget.Reserve)
			h := http.Header{}
			h.Set("X-RateLimit-Resource", "core")
			h.Set("X-RateLimit-Limit", itoa(limit))
			h.Set("X-RateLimit-Remaining", itoa(rem))
			h.Set("X-RateLimit-Reset", "1800002000")
			rs.observe("s", 1, h, now, "core")
			b, s, w, i := rs.admit(ClassBackfill, now), rs.admit(ClassScheduled, now), rs.admit(ClassWebhook, now), rs.admit(ClassInteractive, now)
			if !(b >= s && s >= w && w >= i) {
				t.Fatalf("limit=%d remaining=%d: %v %v %v %v", limit, rem, b, s, w, i)
			}
			if b > maxGateWait {
				t.Fatalf("wait %v over cap", b)
			}
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
```

## 5 `internal/github/schema_test.go`

```go
package github

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestHeadsQueryMatchesPublicSchema checks that every field of the head query exists on its type in GitHub's public
// GraphQL schema. The schema (about 1.5 MB) is not committed: scripts/fetch-github-schema.sh downloads it and the
// weekly CI job sets SCP_GRAPHQL_SCHEMA. Without the variable the test is skipped, never silently passed.
func TestHeadsQueryMatchesPublicSchema(t *testing.T) {
	path := os.Getenv("SCP_GRAPHQL_SCHEMA")
	if path == "" {
		t.Skip("SCP_GRAPHQL_SCHEMA not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	body := func(kind, name string) string {
		re := regexp.MustCompile(`(?m)^` + kind + ` ` + name + `\b[^{]*\{`)
		loc := re.FindStringIndex(schema)
		if loc == nil {
			t.Fatalf("%s %s not found in schema", kind, name)
		}
		end := strings.Index(schema[loc[1]:], "\n}")
		return schema[loc[1] : loc[1]+end]
	}
	has := func(block, field string) bool {
		return regexp.MustCompile(`(?m)^  ` + field + `\b[(:]`).MatchString(block)
	}
	checks := map[string][]string{
		"Repository":      {"databaseId", "pushedAt", "stargazerCount", "licenseInfo", "issues", "latestRelease", "defaultBranchRef", "isArchived"},
		"Release":         {"tagName", "name", "url", "isPrerelease", "publishedAt"},
		"Commit":          {"oid", "tree"},
		"Tree":            {"oid"},
		"License":         {"spdxId"},
		"RateLimit":       {"cost", "remaining", "resetAt"},
		"IssueConnection": {"totalCount"},
		"Ref":             {"target"},
	}
	for typ, fields := range checks {
		blk := body("type", typ)
		for _, f := range fields {
			if !has(blk, f) {
				t.Errorf("type %s lacks field %s", typ, f)
			}
		}
	}
	if !strings.Contains(body("enum", "RepositoryVisibility"), "INTERNAL") {
		t.Error("RepositoryVisibility lacks INTERNAL")
	}
	q := buildHeadsQuery(2)
	for _, f := range []string{"databaseId", "pushedAt", "stargazerCount", "licenseInfo", "latestRelease", "defaultBranchRef", "rateLimit"} {
		if !strings.Contains(q, f) {
			t.Errorf("query does not select %s", f)
		}
	}
}
```

## 6 `internal/github/githubtest/gitobjects_test.go` and `fixture_test.go`

```go
package githubtest

import (
	"testing"
	"time"
)

// Golden vectors produced with the real git binary (scripts/verify-git-vectors.sh regenerates and compares them; the
// shell script, not this test, runs git, because os/exec is reserved for gitsource and cmd/scp, rule G6).
const (
	vecTree   = "7aa1e116b7f89ff53e9fbb020497f8b4a8a7abdf"
	vecCommit = "e54748a8fd2a76f7b6fcb8374c2623ddecc35f87"
	vecChild  = "b7a57e04d324330e1c5f29773e4fb569b2d308e1"
	vecBlob   = "77ebc6577e3fc2c38b5bc7f233510fafba6822ac" // git hash-object of "# hi\n"
)

func TestObjectIDsMatchGitGoldenVectors(t *testing.T) {
	files := map[string]string{
		"README.md":         "# hi\n",
		"docs/overview.md":  "overview\n",
		"docs/a-b.md":       "x\n",
		"docs/a/deep.md":    "deep\n", // git sorts a tree as "a/": "a-b.md" < "a/" ('-' is 0x2d, '/' is 0x2f)
		"pom.xml":           "<project/>\n",
		"src/main/App.java": "class App {}\n",
	}
	root, trees, blobs := BuildTrees(files)
	if root != vecTree {
		t.Fatalf("tree %s, want %s", root, vecTree)
	}
	if len(trees) != 5 || len(blobs) != 6 {
		t.Fatalf("trees=%d blobs=%d", len(trees), len(blobs))
	}
	if GitBlobSHA([]byte("# hi\n")) != vecBlob {
		t.Fatal("blob id differs from git hash-object")
	}
	a := Author{Name: "Ada Lovelace", Email: "ada@example.org"}
	at := time.Unix(1_700_000_000, 0)
	if got := GitCommitSHA(root, nil, a, at, "Initial commit"); got != vecCommit {
		t.Fatalf("commit %s, want %s", got, vecCommit)
	}
	if got := GitCommitSHA(root, []string{vecCommit}, a, at, "Second"); got != vecChild {
		t.Fatalf("child commit %s, want %s", got, vecChild)
	}
}
```

```go
package githubtest

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// demo is the fixture directory, relative to the design package root; in the product repository it is
// testdata/worlds/demo/ (relative to the module root).
const demo = "20-design/05-github-adapter.assets/worlds/demo/"

func strict(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestDemoFixtureDecodes(t *testing.T) {
	var w FixtureWorld
	strict(t, demo+"world.json", &w)
	for _, r := range w.Repos {
		var fr FixtureRepo
		strict(t, demo+r+"/repo.json", &fr)
		if len(fr.Commits) == 0 {
			t.Fatal(r)
		}
	}
}
```

## internal/github/breaker_test.go

```go
package github

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"
)

func resp(code int) func() (*http.Response, error) {
	return func() (*http.Response, error) { return &http.Response{StatusCode: code}, nil }
}

func TestBreakerOpensOnTransientOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var states []string
		b := newBreaker("acme", func(from, to string) { states = append(states, from+">"+to) })
		// 50 answers that mean "GitHub is healthy" never trip it
		for _, code := range []int{404, 403, 429, 401, 422, 200, 304} {
			for i := 0; i < 8; i++ {
				if r, err := b.Do(resp(code)); err != nil || r.StatusCode != code {
					t.Fatal(code, err)
				}
			}
		}
		if _, open := b.Open(time.Now()); open {
			t.Fatal("client errors must not open the breaker")
		}
		// a canceled caller says nothing about GitHub
		for i := 0; i < 20; i++ {
			_, _ = b.Do(func() (*http.Response, error) { return nil, context.Canceled })
		}
		if _, open := b.Open(time.Now()); open {
			t.Fatal("cancellations must not open the breaker")
		}
		// 8 consecutive 5xx open it; the caller still gets each response
		for i := 0; i < 8; i++ {
			if r, err := b.Do(resp(503)); err != nil || r.StatusCode != 503 {
				t.Fatalf("response must reach the caller: %v", err)
			}
		}
		until, open := b.Open(time.Now())
		if !open || until.Sub(time.Now()) != breakerOpenFor || b.State() != 2 {
			t.Fatalf("open=%v until=%v state=%d", open, until, b.State())
		}
		called := false
		if _, err := b.Do(func() (*http.Response, error) { called = true; return nil, nil }); !refused(err) || called {
			t.Fatalf("open breaker must refuse without calling: %v called=%v", err, called)
		}
		time.Sleep(breakerOpenFor + time.Second) // virtual; the breaker leaves the open state strictly after its timeout
		if b.State() != 1 {
			t.Fatalf("state %d, want half-open", b.State())
		}
		for i := 0; i < breakerHalfOpenN; i++ {
			if _, err := b.Do(resp(200)); err != nil {
				t.Fatal(err)
			}
		}
		if b.State() != 0 {
			t.Fatalf("state %d, want closed", b.State())
		}
		if len(states) != 3 {
			t.Fatalf("transitions %v", states)
		}
	})
}

func TestBreakerRatioTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBreaker("acme", nil)
		fail := func() (*http.Response, error) { return nil, errors.New("connection reset") }
		for i := 0; i < 22; i++ { // alternating: never 8 in a row; ReadyToTrip is evaluated on a failure once 20 samples exist
			if i%2 == 0 {
				_, _ = b.Do(fail)
			} else {
				_, _ = b.Do(resp(200))
			}
		}
		if _, open := b.Open(time.Now()); !open {
			t.Fatal("a 50 percent failure ratio over 20 requests must open the breaker")
		}
	})
}
```

## Verification additions (2026-10-14, independent verifier)

Compiled and run with `go vet` and `go test -race` in the verifier's copy of the scratch module (`scratchpad/verify-05-github-adapter/mod`). They pin the defects V-03, V-05 to V-08, V-14 and V-15 of the Verification log. Note that `github_test.go` above no longer imports `job` (finding V-01): the behaviour interfaces are copied into the test file.

### `internal/github/auth/verify_test.go`

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 403 on mint is "suspended" only when it is not a rate limit (verification finding V-03).
func TestMintClassifiesRateLimitAndScope(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := []struct {
		name   string
		status int
		hdr    map[string]string
		body   string
		want   error
		retry  time.Duration
	}{
		{"secondary with retry-after", 403, map[string]string{"Retry-After": "30"}, `{"message":"You have exceeded a secondary rate limit"}`, ErrRateLimited, 30 * time.Second},
		{"primary exhausted", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1800000100"}, `{"message":"API rate limit exceeded"}`, ErrRateLimited, 0},
		{"message only", 403, nil, `{"message":"You have exceeded a secondary rate limit. Please wait"}`, ErrRateLimited, time.Minute},
		{"429", 429, nil, `{"message":"slow down"}`, ErrRateLimited, time.Minute},
		{"suspended", 403, nil, `{"message":"This installation has been suspended"}`, ErrInstallationUnavailable, 0},
		{"removed", 404, nil, `{"message":"Not Found"}`, ErrInstallationUnavailable, 0},
		{"invalid scope", 422, nil, `{"message":"Validation Failed"}`, ErrInvalidScope, 0},
	}
	for _, c := range cases {
		srv, _ := mintServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
			for k, v := range c.hdr {
				w.Header().Set(k, v)
			}
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		app, _ := NewApp(AppConfig{ClientID: "Iv", APIBase: srv.URL, Keys: StaticKeys{k}, HTTP: srv.Client(), Now: func() time.Time { return time.Unix(1_800_000_000, 0) }})
		_, err := app.Token(context.Background(), Scope{Installation: 1}, 0)
		var ae *Error
		if !errors.Is(err, c.want) || !errors.As(err, &ae) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if c.name == "primary exhausted" {
			if ae.RetryAfter != 100*time.Second {
				t.Fatalf("%s: retry %v", c.name, ae.RetryAfter)
			}
		} else if c.retry != 0 && ae.RetryAfter != c.retry {
			t.Fatalf("%s: retry %v want %v", c.name, ae.RetryAfter, c.retry)
		}
	}
}

// The fingerprint equals what `openssl rsa -pubout -outform DER | openssl sha256 -binary | openssl base64` prints.
func TestFingerprintMatchesGitHubFormat(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	sum := sha256.Sum256(der)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if got := Fingerprint(&k.PublicKey); got != want || len(got) != 44 {
		t.Fatalf("%q %q", got, want)
	}
}

func TestNewRand01IsConcurrencySafe(t *testing.T) {
	r := NewRand01(7)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if v := r(); v < 0 || v >= 1 {
					t.Error(v)
				}
			}
		}()
	}
	wg.Wait()
}

func TestExternalKeepsValidTokenWhenRefreshFails(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	fail := false
	e, _ := NewExternal(ExternalConfig{Argv: []string{"/bin/x"}, Now: func() time.Time { return now }, Run: func(context.Context, []string, time.Duration) ([]byte, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return []byte(`{"token":"t1","expires_at":"2027-01-15T08:10:00Z"}`), nil // now+10m
	}})
	if _, err := e.Token(context.Background(), Scope{}, 0); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute) // inside the 5 min refresh margin, token still valid for 4 min
	fail = true
	if tok, err := e.Token(context.Background(), Scope{}, 0); err != nil || tok.Value.Reveal() != "t1" {
		t.Fatalf("%v %v", tok, err)
	}
	now = now.Add(5 * time.Minute) // expired
	if _, err := e.Token(context.Background(), Scope{}, 0); !errors.Is(err, ErrCommand) {
		t.Fatal(err)
	}
}
```

### `internal/github/verify_test.go`

```go
package github

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func wrap60(b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	var sb strings.Builder
	for len(enc) > 60 {
		sb.WriteString(enc[:60] + "\n")
		enc = enc[60:]
	}
	sb.WriteString(enc + "\n")
	return sb.String()
}

// V-05: a 1 MiB README, base64-wrapped as GitHub sends it, must fit the response bound (the old 4/3+4KiB bound did not).
func TestContentsBoundFitsGitHubBase64(t *testing.T) {
	const fileBytes = 1 << 20
	body := `{"name":"README.md","path":"README.md","sha":"` + strings.Repeat("a", 40) + `","size":1048576,"encoding":"base64","content":"` + strings.ReplaceAll(wrap60(make([]byte, fileBytes)), "\n", `\n`) + `"}`
	oldBound := int64(fileBytes)*4/3 + 4<<10
	if int64(len(body)) <= oldBound {
		t.Fatalf("test premise: body %d must exceed the old bound %d", len(body), oldBound)
	}
	if int64(len(body)) > contentsJSONLimit(fileBytes) {
		t.Fatalf("body %d exceeds the new bound %d", len(body), contentsJSONLimit(fileBytes))
	}
	// a smaller configured file_bytes still accepts what GitHub sends in JSON (up to 1 MiB) so the adapter can truncate
	if contentsJSONLimit(64<<10) < int64(len(body)) {
		t.Fatal("bound must not depend on a file_bytes below 1 MiB")
	}
	// the standard decoder ignores the newlines
	if _, err := base64.StdEncoding.DecodeString(wrap60([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
}

func TestExcerptNeverSplitsARuneAndRedactsJWT(t *testing.T) {
	in := `{"message":"` + strings.Repeat("a", 199) + "é" + `"}`
	if got := excerpt([]byte(in)); !utf8.ValidString(got) || len(got) > 200 {
		t.Fatalf("%q", got)
	}
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJJdjEyMyJ9.c2lnbmF0dXJlc2lnbmF0dXJl"
	if got := excerpt([]byte(`{"message":"bad jwt ` + jwt + ` end"}`)); strings.Contains(got, "eyJ") {
		t.Fatalf("%q", got)
	}
}

func TestAuthPauseDoublesAndSuccessResets(t *testing.T) {
	rs := newRateState(ReserveConfig{})
	now := time.Unix(1_800_000_000, 0)
	var got []time.Duration
	for i := 0; i < 6; i++ {
		got = append(got, rs.authPause("s", now))
	}
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v want %v", got, want)
		}
	}
	if w := rs.hardWait("s", 0, "core", now); w != time.Hour {
		t.Fatalf("hardWait %v", w)
	}
	rs.success("s")
	if w := rs.authPause("s", now.Add(2*time.Hour)); w != 5*time.Minute {
		t.Fatalf("after success %v", w)
	}
}

func TestObserveFallsBackToCallerResource(t *testing.T) {
	rs := newRateState(ReserveConfig{})
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "10")
	h.Set("X-RateLimit-Reset", "1800000100")
	rs.observe("s", 1, h, time.Unix(1_800_000_000, 0), "graphql")
	if s := rs.snapshots(); len(s) != 1 || s[0].Resource != "graphql" {
		t.Fatalf("%+v", s)
	}
}

func TestNextLinkCommaInTarget(t *testing.T) {
	base, _ := url.Parse("https://api.github.com")
	h := http.Header{}
	h.Add("Link", `<https://api.github.com/x?a=1,2&page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`)
	rel, ok, err := nextLink(h, base)
	if err != nil || !ok || rel != "/x?a=1,2&page=2" {
		t.Fatalf("%q %v %v", rel, ok, err)
	}
}
```
