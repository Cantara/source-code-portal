package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi"
)

// Options configures sign-in. Without a session key nobody signs in, and only the tokens identify.
type Options struct {
	SessionKey []byte        // at least 32 bytes; nil: no sessions
	SessionTTL time.Duration // a session's lifetime; one older than half of it is resealed on use
	Tokens     []Token
	OIDC       *OIDC  // nil: no provider
	PublicURL  string // server.public_url: the redirect URI's base and whether cookies are Secure
	// Keep returns the groups a session keeps of the provider's, those the access lists name: a session must fit in a cookie.
	Keep   func(groups []string) []string
	Client *http.Client // to the provider; nil: one with timeouts
	Now    func() time.Time
	Log    *slog.Logger
}

// Token is a bearer credential.
type Token struct {
	Name   string
	Secret []byte
	Groups []string
	Admin  bool
}

// OIDC is the provider.
type OIDC struct {
	Issuer, ClientID, ClientSecret string
	Scopes                         []string
	GroupsClaim                    string
}

// Auth identifies requests by their bearer token or session cookie, and serves the sign-in routes.
type Auth struct {
	o        Options
	sessions sealer // nil aead: no sessions
	flows    sealer // the pending OIDC sign-ins
	tokens   []token
	secure   bool
	session  string // the session cookie's name
	flow     string // the pending sign-in cookie's name
	provider *provider
}

type token struct {
	Token
	digest [sha256.Size]byte
}

// session is what the session cookie holds.
type session struct {
	Issuer   string   `json:"iss,omitempty"`
	Subject  string   `json:"sub"`
	Email    string   `json:"email,omitempty"`
	Verified bool     `json:"ev,omitempty"` // the provider verified the email: only then does it match auth.admin_emails
	Name     string   `json:"name,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Token    string   `json:"tok,omitempty"` // a token's sign-in: the token is looked up on every request, so removing it ends the session
	Issued   int64    `json:"iat"`
	Expires  int64    `json:"exp"`
}

const (
	sessionLabel = "scp session v1"
	flowLabel    = "scp oidc v1"
	maxGroups    = 64
)

// New returns the authenticator of o.
func New(o Options) (*Auth, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Keep == nil {
		o.Keep = func(g []string) []string { return g }
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 10 * time.Second}
	}
	a := &Auth{o: o, secure: strings.HasPrefix(o.PublicURL, "https://"), session: "scp_session", flow: "scp_oidc"}
	if a.secure { // __Host-: the browser keeps it only Secure, on this host and path /
		a.session, a.flow = "__Host-scp_session", "__Host-scp_oidc"
	}
	for _, t := range o.Tokens {
		a.tokens = append(a.tokens, token{Token: t, digest: sha256.Sum256(t.Secret)})
	}
	if o.SessionKey == nil {
		return a, nil
	}
	var err error
	if a.sessions, err = newSealer(o.SessionKey, sessionLabel); err != nil {
		return nil, err
	}
	if a.flows, err = newSealer(o.SessionKey, flowLabel); err != nil {
		return nil, err
	}
	if o.OIDC != nil {
		a.provider = &provider{o: *o.OIDC, client: o.Client, redirect: o.PublicURL + "/auth/callback", log: o.Log}
	}
	return a, nil
}

// Methods reports how a person can sign in: through the provider, with a token.
func (a *Auth) Methods() (oidc, token bool) {
	return a.provider != nil, a.sessions.aead != nil && len(a.tokens) > 0
}

// Identify implements httpapi.Authenticator.
func (a *Auth) Identify(w http.ResponseWriter, r *http.Request) (httpapi.Identity, bool, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, cred, _ := strings.Cut(h, " ")
		t, ok := a.match(strings.TrimSpace(cred))
		if !strings.EqualFold(scheme, "Bearer") || !ok {
			return httpapi.Identity{}, false, httpapi.ErrUnauthenticated
		}
		return t.identity(), true, nil
	}
	if a.sessions.aead == nil {
		return httpapi.Identity{}, false, nil
	}
	c, err := r.Cookie(a.session)
	if err != nil {
		return httpapi.Identity{}, false, nil
	}
	var s session
	now := a.o.Now()
	if !a.sessions.open(a.session, c.Value, &s) || now.Unix() >= s.Expires {
		a.clear(w, a.session) // sealed under another key, tampered or ended: anonymous, not refused
		return httpapi.Identity{}, false, nil
	}
	id := s.identity()
	if s.Token != "" {
		t, ok := a.named(s.Token)
		if !ok {
			a.clear(w, a.session)
			return httpapi.Identity{}, false, nil
		}
		id = t.identity()
	}
	if now.Sub(time.Unix(s.Issued, 0)) > a.o.SessionTTL/2 {
		_ = a.start(w, s) // a failure keeps the current session until it ends
	}
	return id, true, nil
}

// match returns the token whose secret is cred, comparing every token's digest in constant time.
func (a *Auth) match(cred string) (token, bool) {
	d := sha256.Sum256([]byte(cred))
	var found token
	ok := false
	for _, t := range a.tokens {
		if subtle.ConstantTimeCompare(d[:], t.digest[:]) == 1 {
			found, ok = t, true
		}
	}
	return found, ok && cred != ""
}

func (a *Auth) named(name string) (token, bool) {
	for _, t := range a.tokens {
		if t.Name == name {
			return t, true
		}
	}
	return token{}, false
}

func (t token) identity() httpapi.Identity {
	return httpapi.Identity{User: "token:" + t.Name, Name: t.Name, Groups: t.Groups, Admin: t.Admin, Via: "token"}
}

func (s session) identity() httpapi.Identity {
	id := httpapi.Identity{User: s.Subject, Name: s.Name, Groups: s.Groups, Via: "oidc"}
	if s.Verified {
		id.Email = s.Email
	}
	return id
}

// start seals s into the session cookie, issued now.
func (a *Auth) start(w http.ResponseWriter, s session) error {
	now := a.o.Now()
	s.Issued, s.Expires = now.Unix(), now.Add(a.o.SessionTTL).Unix()
	if len(s.Groups) > maxGroups {
		s.Groups = s.Groups[:maxGroups]
	}
	v, err := a.sessions.seal(a.session, s)
	if err != nil {
		return err
	}
	a.cookie(w, a.session, v, int(a.o.SessionTTL.Seconds()))
	return nil
}

func (a *Auth) clear(w http.ResponseWriter, name string) { a.cookie(w, name, "", -1) }

// cookie sets one of SCP's cookies: HttpOnly, SameSite Lax (the provider's redirect back is a top-level GET, which carries it), on
// path /, and Secure whenever server.public_url is https.
func (a *Auth) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, //nolint:gosec // G124: Secure follows public_url; plain http only on loopback
		Secure: a.secure, SameSite: http.SameSiteLaxMode})
}

// returnTo keeps a same-origin path, else "/": a sign-in never sends the browser to another site.
func returnTo(s string) string {
	u, err := url.Parse(s)
	if s == "" || err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(s, "//") || strings.Contains(s, `\`) || strings.HasPrefix(u.Path, "/auth/") {
		return "/"
	}
	return u.RequestURI()
}

// signInError sends the browser to the sign-in page with a code it explains.
func signInError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/signin?error="+code, http.StatusSeeOther)
}
