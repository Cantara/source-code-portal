// Package oidctest is a fake OpenID Connect provider for tests, on a loopback listener: discovery, JWKS, an authorization endpoint
// that approves at once as the configured user, the token endpoint with PKCE, userinfo and end session.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

// User is who the provider signs in.
type User struct {
	Subject, Email, Name string
	Unverified           bool     // email_verified false
	Groups               []string // in the ID token, or with GroupsInUserinfo only from userinfo
	GroupsInUserinfo     bool
}

// Provider is the fake provider. Its fields change how the next tokens are issued.
type Provider struct {
	*httptest.Server
	ClientID string

	mu         sync.Mutex
	key        *rsa.PrivateKey
	user       User
	codes      map[string]grant
	access     map[string]User
	wrongNonce bool
	expired    bool
}

type grant struct {
	user                          User
	nonce, challenge, redirectURI string
}

// New starts a provider for the client clientID.
func New(clientID string) *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p := &Provider{ClientID: clientID, key: key, codes: map[string]grant{}, access: map[string]User{},
		user: User{Subject: "u-1", Email: "ada@example.org", Name: "Ada Lovelace"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /userinfo", p.userinfo)
	mux.HandleFunc("GET /end_session", p.endSession)
	p.Server = httptest.NewServer(mux)
	return p
}

// Issuer is the provider's issuer URL.
func (p *Provider) Issuer() string { return p.URL }

// SetUser makes u the user of the next sign-in.
func (p *Provider) SetUser(u User) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.user = u
}

// WrongNonce makes the next ID tokens carry another nonce; Expired makes them expired.
func (p *Provider) WrongNonce(on bool) { p.mu.Lock(); p.wrongNonce = on; p.mu.Unlock() }
func (p *Provider) Expired(on bool)    { p.mu.Lock(); p.expired = on; p.mu.Unlock() }

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token",
		"userinfo_endpoint": p.URL + "/userinfo", "jwks_uri": p.URL + "/jwks", "end_session_endpoint": p.URL + "/end_session",
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := p.key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]string{{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
		"n": b64(pub.N.Bytes()), "e": b64(big.NewInt(int64(pub.E)).Bytes())}}})
}

// authorize approves at once: the browser goes back to the client with a code.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := random()
	p.mu.Lock()
	p.codes[code] = grant{user: p.user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
	p.mu.Unlock()
	u, _ := url.Parse(q.Get("redirect_uri"))
	back := u.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	u.RawQuery = back.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound) //nolint:gosec // G710: a test provider
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	wrongNonce, expired := p.wrongNonce, p.expired
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	clientID := r.PostForm.Get("client_id")
	if id, _, basic := r.BasicAuth(); basic {
		clientID = id
	}
	if !ok || b64(sum[:]) != g.challenge || r.PostForm.Get("redirect_uri") != g.redirectURI || clientID != p.ClientID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": p.URL, "sub": g.user.Subject, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"nonce": g.nonce, "name": g.user.Name, "email": g.user.Email, "email_verified": !g.user.Unverified}
	if wrongNonce {
		claims["nonce"] = "another"
	}
	if expired {
		claims["exp"] = now.Add(-time.Hour).Unix()
	}
	if len(g.user.Groups) > 0 && !g.user.GroupsInUserinfo {
		claims["groups"] = g.user.Groups
	}
	access := random()
	p.mu.Lock()
	p.access[access] = g.user
	p.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "id_token": p.sign(claims)})
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	u, ok := p.access[stripBearer(r.Header.Get("Authorization"))]
	p.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"sub": u.Subject, "email": u.Email, "name": u.Name, "groups": u.Groups})
}

// endSession signs out and sends the browser back to post_logout_redirect_uri, as providers do.
func (p *Provider) endSession(w http.ResponseWriter, r *http.Request) {
	back := r.URL.Query().Get("post_logout_redirect_uri")
	if back == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, back, http.StatusFound) //nolint:gosec // G710: a test provider
}

func (p *Provider) sign(claims map[string]any) string {
	head, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	body, _ := json.Marshal(claims)
	in := b64(head) + "." + b64(body)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return in + "." + b64(sig)
}

func stripBearer(h string) string {
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return ""
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b64(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
