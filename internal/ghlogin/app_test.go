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

func TestAppJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tok, err := appJWT(42, pemKey, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %s", tok)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("signature: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss string
		Iat int64
		Exp int64
	}
	if err := json.Unmarshal(raw, &c); err != nil || c.Iss != "42" || c.Exp-c.Iat != 600 || c.Iat != now.Add(-time.Minute).Unix() {
		t.Errorf("claims: %s %v", raw, err)
	}
	if _, err := appJWT(42, "not a key", now); err == nil {
		t.Error("a key that is not PEM was accepted")
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
