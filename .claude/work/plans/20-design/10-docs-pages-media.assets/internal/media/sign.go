package media

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/cantara/source-code-portal/internal/platform/secret"
	"github.com/cantara/source-code-portal/internal/routes"
)

const (
	sigContext   = "scp-media-ext-v1\n"
	keyIDContext = "scp-media-keyid-v1\n"
)

// Signer mints and verifies /media/ext identifiers. The identifier is the base64url of the canonical URL (the server
// stores nothing); the signature is the base64url of HMAC-SHA256(key, context + id). Verification accepts the current
// key and every previous key, in constant time per key. Safe for concurrent use.
type Signer struct {
	cur  []byte
	prev [][]byte
	id   string
}

// NewSigner builds a Signer. A zero current key makes a random ephemeral key (development): ephemeral reports it.
func NewSigner(cur secret.Secret, prev []secret.Secret) (s *Signer, ephemeral bool, err error) {
	k := []byte(cur.Reveal())
	if len(k) == 0 {
		k = make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, false, err
		}
		ephemeral = true
	}
	s = &Signer{cur: k}
	for _, p := range prev {
		s.prev = append(s.prev, []byte(p.Reveal()))
	}
	sum := sha256.Sum256(append([]byte(keyIDContext), k...))
	s.id = hex.EncodeToString(sum[:4])
	return s, ephemeral, nil
}

// KeyID identifies the current key without revealing it (8 hex characters).
func (s *Signer) KeyID() string { return s.id }

func (s *Signer) mac(key []byte, id string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(sigContext))
	m.Write([]byte(id))
	return m.Sum(nil)
}

// Sign returns the signed portal path for rawURL, or ok=false when the URL cannot be proxied (not https, bad syntax,
// too long). Equal URLs (after canonicalisation) give equal paths.
func (s *Signer) Sign(rawURL string) (path string, ok bool) {
	c, err := Canonical(rawURL)
	if err != nil {
		return "", false
	}
	id := base64.RawURLEncoding.EncodeToString([]byte(c))
	sig := base64.RawURLEncoding.EncodeToString(s.mac(s.cur, id))
	return routes.MediaExt(sig, id), true
}

// ErrBadSignature is returned by Verify for a wrong, malformed or foreign signature.
var ErrBadSignature = errors.New("media: bad signature")

// Verify checks sig over id and returns the canonical URL.
func (s *Signer) Verify(sig, id string) (string, error) {
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(got) != sha256.Size || len(id) > base64.RawURLEncoding.EncodedLen(MaxURLLength) {
		return "", ErrBadSignature
	}
	ok := hmac.Equal(got, s.mac(s.cur, id))
	for _, p := range s.prev {
		if hmac.Equal(got, s.mac(p, id)) {
			ok = true
		}
	}
	if !ok {
		return "", ErrBadSignature
	}
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", ErrBadSignature
	}
	c, err := Canonical(string(b))
	if err != nil || c != string(b) {
		return "", ErrBadSignature
	}
	return c, nil
}

// Canonical parses and normalises an external image URL: https only, lower-case ASCII host without a default port,
// no userinfo, no fragment, no control characters or backslashes, at most MaxURLLength bytes. It does NOT resolve
// names; the connect-time check of the injected client does.
func Canonical(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > MaxURLLength {
		return "", errors.New("media: URL length")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] <= 0x20 || raw[i] == 0x7f || raw[i] == '\\' {
			return "", errors.New("media: URL contains a control character, space or backslash")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" {
		return "", errors.New("media: only plain https URLs without userinfo are proxied")
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")
	if !validHostName(host) {
		return "", errors.New("media: host is not a public DNS name")
	}
	if port := u.Port(); port != "" && port != "443" {
		return "", errors.New("media: only port 443 is proxied")
	}
	u.Host = host
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// validHostName accepts ASCII DNS names with at least two labels whose last label is alphabetic (so every numeric
// spelling of an IP address, dotted or not, is refused) and refuses names that are internal by convention.
func validHostName(h string) bool {
	if len(h) < 4 || len(h) > 253 {
		return false
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	hasLetter := false
	for i := 0; i < len(tld); i++ {
		if tld[i] >= 'a' && tld[i] <= 'z' {
			hasLetter = true
		}
	}
	if !hasLetter || (len(tld) < 2) {
		return false
	}
	switch tld {
	case "localhost", "local", "internal", "localdomain", "lan", "home", "corp", "intranet", "arpa", "invalid", "test":
		return false
	}
	return true
}

// sigFor signs an arbitrary (possibly non-canonical) string; used by tests to check that Verify re-canonicalises.
func (s *Signer) sigFor(raw string) [2]string {
	id := base64.RawURLEncoding.EncodeToString([]byte(raw))
	return [2]string{base64.RawURLEncoding.EncodeToString(s.mac(s.cur, id)), id}
}
