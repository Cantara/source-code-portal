// Package ghverify is the REFERENCE implementation of the signature check and push decoding that
// github.Verifier must provide (design document 04-refresh-webhook, section 4.9). It lives in the scratch
// module only; in the real repository this code belongs to internal/github/webhook.go.
package ghverify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/cantara/source-code-portal/internal/scm"
)

// Error kinds satisfy the behaviour interfaces the webhook receiver classifies with.
type Error struct {
	Kind string // "unauthenticated", "malformed", "ignored"
	Msg  string
}

func (e *Error) Error() string         { return "webhook: " + e.Kind + ": " + e.Msg }
func (e *Error) Unauthenticated() bool { return e.Kind == "unauthenticated" }
func (e *Error) Malformed() bool       { return e.Kind == "malformed" }
func (e *Error) Ignored() bool         { return e.Kind == "ignored" }

const sigPrefix = "sha256="

// Plausible is the cheap pre-body check: "sha256=" + 64 hex digits. It authenticates nothing.
func Plausible(h string) bool {
	if len(h) != len(sigPrefix)+sha256.Size*2 || !strings.HasPrefix(h, sigPrefix) {
		return false
	}
	for _, c := range h[len(sigPrefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// VerifySignature checks X-Hub-Signature-256 over the RAW body against every non-empty secret (current,
// then previous during a rotation overlap). Constant-time per secret; never falls back to SHA-1; an empty
// secret list or empty secrets never verify.
func VerifySignature(header string, body []byte, secrets ...[]byte) error {
	if !Plausible(header) {
		return &Error{"unauthenticated", "missing or malformed X-Hub-Signature-256"}
	}
	got, err := hex.DecodeString(header[len(sigPrefix):])
	if err != nil {
		return &Error{"unauthenticated", "signature is not hex"}
	}
	ok := false
	for _, s := range secrets {
		if len(s) == 0 {
			continue
		}
		m := hmac.New(sha256.New, s)
		m.Write(body)
		if hmac.Equal(m.Sum(nil), got) { // do not break: constant work per configured secret
			ok = true
		}
	}
	if !ok {
		return errors.Join(&Error{"unauthenticated", "signature mismatch"})
	}
	return nil
}

// Sign returns the header value for tests.
func Sign(secret, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	return sigPrefix + hex.EncodeToString(m.Sum(nil))
}

var _ = http.Header{}
var _ scm.Event
