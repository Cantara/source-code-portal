// Package vault seals the credentials the portal reads GitHub with (design/personal.md): AES-256-GCM under a key that lives in the
// operating system's keychain, else in a 0600 file beside the store, never in the store. A sealed value carries its row's id, source
// and kind as additional data, so a value moved to another row, or a row edited to another source, does not open.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/cantara/source-code-portal/internal/store"
)

// Secret is a credential in the clear. It is never logged: String redacts it.
type Secret struct {
	ID        string
	Source    string
	Kind      string // "token" (a personal access token or a user token)
	Value     string
	ExpiresAt time.Time // zero: does not expire
}

// String redacts the value, so a Secret printed by mistake shows nothing.
func (s Secret) String() string { return fmt.Sprintf("credential %s (%s, %s)", s.ID, s.Source, s.Kind) }

// GoString redacts the value for %#v.
func (s Secret) GoString() string { return s.String() }

// Expired reports whether the credential has ended by now.
func (s Secret) Expired(now time.Time) bool { return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt) }

// ErrUnreadable is returned for a sealed value that does not open under this key for its row.
var ErrUnreadable = errors.New("vault: credential does not open (wrong key, or the row was changed)")

// Vault seals into and opens from the store's credential rows.
type Vault struct {
	aead  cipher.AEAD
	store *store.DB
	now   func() time.Time
}

// New returns a vault over s under key (at least 32 bytes of randomness; OpenKey makes one).
func New(key []byte, s *store.DB) (*Vault, error) {
	if len(key) < 32 {
		return nil, errors.New("vault: key shorter than 32 bytes")
	}
	derived, err := hkdf.Key(sha256.New, key, nil, "scp vault credential v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, store: s, now: time.Now}, nil
}

func aad(id, source, kind string) []byte { return []byte(id + "|" + source + "|" + kind) }

// Put seals sec and stores it, replacing the credential with the same id.
func (v *Vault) Put(ctx context.Context, sec Secret) error {
	if sec.ID == "" || sec.Source == "" || sec.Kind == "" || sec.Value == "" {
		return errors.New("vault: a credential needs an id, a source, a kind and a value")
	}
	nonce := make([]byte, v.aead.NonceSize(), v.aead.NonceSize()+len(sec.Value)+v.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	return v.store.PutCredential(ctx, store.Credential{
		ID: sec.ID, Source: sec.Source, Kind: sec.Kind,
		Sealed:    v.aead.Seal(nonce, nonce, []byte(sec.Value), aad(sec.ID, sec.Source, sec.Kind)),
		ExpiresAt: sec.ExpiresAt, CreatedAt: v.now().UTC(),
	})
}

// Get opens the credential with id. store.ErrNoCredential when there is none, ErrUnreadable when it does not open.
func (v *Vault) Get(ctx context.Context, id string) (Secret, error) {
	c, err := v.store.Credential(ctx, id)
	if err != nil {
		return Secret{}, err
	}
	n := v.aead.NonceSize()
	if len(c.Sealed) < n {
		return Secret{}, ErrUnreadable
	}
	plain, err := v.aead.Open(nil, c.Sealed[:n], c.Sealed[n:], aad(c.ID, c.Source, c.Kind))
	if err != nil {
		return Secret{}, ErrUnreadable
	}
	return Secret{ID: c.ID, Source: c.Source, Kind: c.Kind, Value: string(plain), ExpiresAt: c.ExpiresAt}, nil
}

// Delete removes the credential with id.
func (v *Vault) Delete(ctx context.Context, id string) error {
	return v.store.DeleteCredential(ctx, id)
}
