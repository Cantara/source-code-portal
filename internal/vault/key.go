package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// Where the vault key was found or placed.
const (
	InKeychain = "the operating system's keychain"
	InFile     = "a key file beside the store"
)

// ErrNoKey is returned by a Ring that holds no key.
var ErrNoKey = errors.New("vault: no key")

// Ring is a place for the vault key outside the store: the operating system's keychain. Get returns ErrNoKey when there is none and
// another error when the ring cannot be used at all (no keychain service on a server); Set stores the key.
type Ring interface {
	Get() ([]byte, error)
	Set(key []byte) error
}

// Keychain is the Ring of the operating system's keychain (macOS Keychain, the Secret Service, the Windows Credential Manager). The
// account is derived from dir, so two directories hold two keys.
func Keychain(dir string) Ring {
	sum := sha256.Sum256([]byte(dir))
	return keychain{account: "vault-" + hex.EncodeToString(sum[:6])}
}

type keychain struct{ account string }

const keychainService = "scp"

func (k keychain) Get() ([]byte, error) {
	s, err := keyring.Get(keychainService, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(s)
}

func (k keychain) Set(key []byte) error {
	return keyring.Set(keychainService, k.account, base64.StdEncoding.EncodeToString(key))
}

// OpenKey returns the vault key and where it is kept. It looks in the ring, then in dir/vault.key; with none it makes a key and puts
// it in the ring, else (no keychain) in dir/vault.key, mode 0600 in a 0700 directory. A key in a file is only as safe as the
// directory: the caller says so (where == InFile).
func OpenKey(dir string, ring Ring) (key []byte, where string, err error) {
	ringUsable := true
	if k, err := ring.Get(); err == nil {
		return k, InKeychain, nil
	} else if !errors.Is(err, ErrNoKey) {
		ringUsable = false
	}
	file := filepath.Join(dir, "vault.key")
	if b, err := os.ReadFile(file); err == nil {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(k) < 32 {
			return nil, "", fmt.Errorf("vault: %s is not a vault key", file)
		}
		return k, InFile, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	if ringUsable && ring.Set(key) == nil {
		return key, InKeychain, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, "", err
	}
	return key, InFile, nil
}
