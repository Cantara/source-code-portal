// Package auth signs people in (design/auth.md): sessions are cookies sealed with AES-256-GCM, so no instance keeps state; OpenID
// Connect with PKCE identifies people at their provider; bearer tokens identify machines. It implements httpapi.Authenticator.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// sealer seals values into cookie values: JSON, encrypted and authenticated with AES-256-GCM under a key derived from the session
// key and a label, the cookie's name as additional data, so a value sealed for one cookie does not open as another.
type sealer struct{ aead cipher.AEAD }

func newSealer(secret []byte, label string) (sealer, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, label, 32)
	if err != nil {
		return sealer{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return sealer{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return sealer{}, err
	}
	return sealer{aead: aead}, nil
}

// maxSealed bounds a sealed value: a browser keeps a cookie up to 4,096 bytes with its name and attributes.
const maxSealed = 3800

var errTooLarge = errors.New("sealed value too large for a cookie")

func (s sealer) seal(name string, v any) (string, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plain)+s.aead.Overhead())
	_, _ = rand.Read(nonce)
	out := base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, plain, []byte(name)))
	if len(out) > maxSealed {
		return "", errTooLarge
	}
	return out, nil
}

// open reports false for a value that was not sealed by this key for this name, or does not decode into v.
func (s sealer) open(name, text string, v any) bool {
	if len(text) > maxSealed {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return false
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(name))
	return err == nil && json.Unmarshal(plain, v) == nil
}
