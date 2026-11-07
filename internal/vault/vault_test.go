package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"go.uber.org/goleak"

	"github.com/cantara/source-code-portal/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const token = "ghp_SECRETtokenVALUE0123456789"

func openStore(t *testing.T) (*store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scp.db")
	db, err := store.Open(context.Background(), path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func newVault(t *testing.T, db *store.DB, key byte) *Vault {
	t.Helper()
	v, err := New(bytes.Repeat([]byte{key}, 32), db)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, _ := openStore(t)
	v := newVault(t, db, 1)
	end := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := v.Put(ctx, Secret{ID: "me", Source: "github.com", Kind: "token", Value: token, ExpiresAt: end}); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get(ctx, "me")
	if err != nil || got.Value != token || got.Source != "github.com" || !got.ExpiresAt.Equal(end) {
		t.Fatalf("got %v %v", got, err)
	}
	if got.Expired(end.Add(-time.Second)) || !got.Expired(end) {
		t.Error("Expired is wrong around the end")
	}
	if (Secret{Value: "x"}).Expired(end) {
		t.Error("a credential without an end expired")
	}
	if err := v.Put(ctx, Secret{ID: "me", Source: "github.com", Kind: "token", Value: "next"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get(ctx, "me"); got.Value != "next" || !got.ExpiresAt.IsZero() {
		t.Errorf("replace: %v", got)
	}
	if err := v.Delete(ctx, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "me"); !errors.Is(err, store.ErrNoCredential) {
		t.Errorf("after delete: %v", err)
	}
}

func TestRefusesIncomplete(t *testing.T) {
	db, _ := openStore(t)
	if err := newVault(t, db, 1).Put(context.Background(), Secret{ID: "me", Source: "s", Kind: "token"}); err == nil {
		t.Error("a credential without a value was stored")
	}
	if _, err := New([]byte("short"), db); err == nil {
		t.Error("a short key was accepted")
	}
}

func TestDoesNotOpenElsewhere(t *testing.T) {
	ctx := context.Background()
	db, _ := openStore(t)
	v := newVault(t, db, 1)
	if err := v.Put(ctx, Secret{ID: "a", Source: "one", Kind: "token", Value: token}); err != nil {
		t.Fatal(err)
	}
	row, err := db.Credential(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(c *store.Credential){
		"another id":     func(c *store.Credential) { c.ID = "b" },
		"another source": func(c *store.Credential) { c.Source = "two" },
		"another kind":   func(c *store.Credential) { c.Kind = "app" },
		"a flipped bit":  func(c *store.Credential) { c.Sealed = append([]byte(nil), c.Sealed...); c.Sealed[len(c.Sealed)-1] ^= 1 },
		"a cut value":    func(c *store.Credential) { c.Sealed = c.Sealed[:5] },
	} {
		t.Run(name, func(t *testing.T) {
			c := row
			mutate(&c)
			if err := db.PutCredential(ctx, c); err != nil {
				t.Fatal(err)
			}
			if _, err := v.Get(ctx, c.ID); !errors.Is(err, ErrUnreadable) {
				t.Errorf("opened: %v", err)
			}
		})
	}
	t.Run("another key", func(t *testing.T) {
		if err := db.PutCredential(ctx, row); err != nil {
			t.Fatal(err)
		}
		if _, err := newVault(t, db, 2).Get(ctx, "a"); !errors.Is(err, ErrUnreadable) {
			t.Errorf("opened under another key: %v", err)
		}
	})
}

func TestNoPlaintextOnDisk(t *testing.T) {
	ctx := context.Background()
	db, path := openStore(t)
	if err := newVault(t, db, 1).Put(ctx, Secret{ID: "me", Source: "github.com", Kind: "token", Value: token}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMeta(ctx, "x", "y"); err != nil { // another write after, so the WAL holds more than the credential
		t.Fatal(err)
	}
	files, _ := filepath.Glob(path + "*")
	if len(files) == 0 {
		t.Fatal("no store files")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(token)) || bytes.Contains(b, []byte("SECRETtoken")) {
			t.Errorf("%s holds the token in the clear", f)
		}
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := Secret{ID: "me", Source: "github.com", Kind: "token", Value: token}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s), fmt.Sprint(&s)} {
		if bytes.Contains([]byte(out), []byte("SECRET")) {
			t.Errorf("printed the value: %s", out)
		}
	}
}

type fakeRing struct {
	key    []byte
	getErr error
	setErr error
}

func (r *fakeRing) Get() ([]byte, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.key == nil {
		return nil, ErrNoKey
	}
	return r.key, nil
}

func (r *fakeRing) Set(k []byte) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.key = k
	return nil
}

func TestOpenKey(t *testing.T) {
	t.Run("made in the ring, then found there", func(t *testing.T) {
		dir, ring := t.TempDir(), &fakeRing{}
		k1, where, err := OpenKey(dir, ring)
		if err != nil || where != InKeychain || len(k1) != 32 {
			t.Fatalf("%v %q %v", len(k1), where, err)
		}
		k2, where, err := OpenKey(dir, ring)
		if err != nil || where != InKeychain || !bytes.Equal(k1, k2) {
			t.Errorf("second open: %q %v", where, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "vault.key")); err == nil {
			t.Error("a key file was written beside a working keychain")
		}
	})
	t.Run("no keychain: a 0600 file, then found there", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "scp")
		ring := &fakeRing{getErr: errors.New("no secret service"), setErr: errors.New("no secret service")}
		k1, where, err := OpenKey(dir, ring)
		if err != nil || where != InFile {
			t.Fatalf("%q %v", where, err)
		}
		st, err := os.Stat(filepath.Join(dir, "vault.key"))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("key file: %v %v", st, err)
		}
		if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
			t.Errorf("directory mode %v", d.Mode().Perm())
		}
		k2, where, err := OpenKey(dir, ring)
		if err != nil || where != InFile || !bytes.Equal(k1, k2) {
			t.Errorf("second open: %q %v", where, err)
		}
	})
	t.Run("a keychain that refuses to store falls back to the file", func(t *testing.T) {
		_, where, err := OpenKey(t.TempDir(), &fakeRing{setErr: errors.New("locked")})
		if err != nil || where != InFile {
			t.Errorf("%q %v", where, err)
		}
	})
	t.Run("a damaged key file is an error, not a new key", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "vault.key"), []byte("nonsense"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := OpenKey(dir, &fakeRing{}); err == nil {
			t.Error("a damaged key file was replaced silently")
		}
	})
}

func TestKeychainRing(t *testing.T) {
	keyring.MockInit()
	ring := Keychain(t.TempDir())
	if _, err := ring.Get(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("empty keychain: %v", err)
	}
	if err := ring.Set([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	got, err := ring.Get()
	if err != nil || string(got) != "0123456789abcdef0123456789abcdef" {
		t.Errorf("%q %v", got, err)
	}
	if _, err := Keychain(t.TempDir()).Get(); !errors.Is(err, ErrNoKey) {
		t.Errorf("another directory shares the key: %v", err)
	}
}
