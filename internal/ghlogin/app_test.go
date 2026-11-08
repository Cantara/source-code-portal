package ghlogin

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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTheVerificationAddressMustBeAWebAddress(t *testing.T) {
	for _, uri := range []string{"javascript:alert(1)", "data:text/html,x", "//example/login", "file:///etc/passwd"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"device_code":"d","user_code":"AAAA-BBBB","verification_uri":%q,"expires_in":900,"interval":5}`, uri)
		}))
		c := &Client{Host: Host{Web: srv.URL, API: srv.URL + "/api/v3/"}, HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
		if _, err := c.StartDevice(t.Context(), "id", ""); err == nil {
			t.Errorf("%q was accepted as a verification address", uri)
		}
		srv.Close()
	}
}

func appKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// The App speaks to the host with a JWT signed by its key (ghinstallation signs it): RS256, the App's id as issuer.
func TestTheAppSpeaksWithItsOwnToken(t *testing.T) {
	key, pemKey := appKey(t)
	var bearer string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/app/installations", func(w http.ResponseWriter, r *http.Request) {
		bearer = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		fmt.Fprint(w, `[{"account":{"login":"acme"}},{"account":{"login":"old"},"suspended_at":"2026-01-01T00:00:00Z"}]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	h, err := ParseHost(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Host: h, HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	got, err := c.Installations(t.Context(), 42, pemKey)
	if err != nil || len(got) != 2 || got[0] != (Installation{Account: "acme"}) || got[1] != (Installation{Account: "old", Suspended: true}) {
		t.Fatalf("%+v %v", got, err)
	}
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", bearer)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("signature: %v", err)
	}
	head, _ := base64.RawURLEncoding.DecodeString(parts[0])
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var hd struct{ Alg string }
	var cl struct {
		Iss string
		Iat int64
		Exp int64
	}
	if json.Unmarshal(head, &hd) != nil || hd.Alg != "RS256" || json.Unmarshal(raw, &cl) != nil || cl.Iss != "42" || cl.Exp <= cl.Iat || cl.Exp-cl.Iat > 600 {
		t.Errorf("header %s claims %s", head, raw)
	}
	if _, err := c.Installations(t.Context(), 42, "not a key"); err == nil {
		t.Error("a key that is not PEM was accepted")
	}
}

func TestAManifestIsExchangedOnceForTheApp(t *testing.T) {
	_, pemKey := appKey(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v3/app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the exchange carries a credential: the code is the proof")
		}
		switch r.PathValue("code") {
		case "good":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "slug": "scp-x", "name": "scp-x", "pem": pemKey, "owner": map[string]string{"login": "acme"}})
		case "nokey":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "slug": "scp-x", "pem": "not a key"})
		case "empty":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	h, _ := ParseHost(srv.URL)
	c := &Client{Host: h, HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	app, err := c.ConvertManifest(t.Context(), "good")
	if err != nil || app.ID != 7 || app.Slug != "scp-x" || app.Owner != "acme" || app.Key != pemKey {
		t.Fatalf("%+v %v", app, err)
	}
	for code, want := range map[string]string{"gone": "404", "nokey": "RSA", "empty": "no App"} {
		if _, err := c.ConvertManifest(t.Context(), code); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestAManifestCodeIsOneSafePathSegment(t *testing.T) {
	c := &Client{Host: Host{Web: "http://127.0.0.1:1", API: "http://127.0.0.1:1/api/v3/"}}
	for _, code := range []string{"", "a/b", "a?b", "a#b", "a%2fb", "a b"} {
		if _, err := c.ConvertManifest(t.Context(), code); err == nil || !strings.Contains(err.Error(), "usable code") {
			t.Errorf("%q: %v", code, err)
		}
	}
}

// A host the person typed in cannot make the setup read without bound.
func TestAnAnswerHasALimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"login":"octo","pad":"`)
		for range maxAnswer / 1024 {
			fmt.Fprint(w, strings.Repeat("x", 1024))
		}
		fmt.Fprint(w, `"}`)
	}))
	defer srv.Close()
	h, _ := ParseHost(srv.URL)
	c := &Client{Host: h, HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	if _, err := c.Whoami(t.Context(), "t"); err == nil {
		t.Error("an answer past the limit was read")
	}
}

// x/oauth2 never closes the code endpoint's answer, and a client with a timeout keeps a goroutine for every body left open: the package's
// goleak check fails this test if StartDevice leaves one behind.
func TestStartingTheDeviceFlowLeavesNothingRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"device_code":"d","user_code":"AAAA-BBBB","verification_uri":"https://example/login/device","expires_in":900,"interval":5}`)
	}))
	defer srv.Close()
	c := &Client{Host: Host{Web: srv.URL, API: srv.URL + "/api/v3/"}, HTTP: &http.Client{Timeout: time.Minute, Transport: &http.Transport{DisableKeepAlives: true}}}
	if code, err := c.StartDevice(t.Context(), "id", ""); err != nil || code.UserCode != "AAAA-BBBB" {
		t.Fatalf("%+v %v", code, err)
	}
}
