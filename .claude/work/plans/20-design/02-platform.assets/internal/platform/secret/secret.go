// Package secret holds the redacting Secret type (rank 0, stdlib only).
package secret

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Redacted is what every formatting path of a Secret prints.
const Redacted = "[REDACTED]"

// MaxFileSize bounds FromFile and Resolve (an App private key is about 3 KiB).
const MaxFileSize = 64 << 10

var (
	// ErrEmpty is returned when a secret file holds no bytes after trimming one trailing newline.
	ErrEmpty = errors.New("secret is empty")
	// ErrNotAbsolute is returned for a {file: ...} reference with a relative path.
	ErrNotAbsolute = errors.New("secret file path must be absolute")
)

// Secret is a credential that cannot be printed by accident. The zero value is "unset".
// A Secret decoded from {file: /path} is pending until Resolve reads the file.
//
// The value is held behind a pointer on purpose: fmt's bad-verb path (for example "%p") prints struct fields
// without calling any method, and a pointer prints an address instead of the credential. Compare with Equal,
// never with ==.
type Secret struct {
	v    *string
	file string
}

func (s Secret) val() string {
	if s.v == nil {
		return ""
	}
	return *s.v
}

// New wraps a literal value. An empty string yields the zero (unset) Secret.
func New(s string) Secret {
	if s == "" {
		return Secret{}
	}
	return Secret{v: &s}
}

// Ref returns a pending reference to a secret file; nothing is read until Resolve. The path must be absolute (checked by Resolve).
func Ref(path string) Secret { return Secret{file: path} }

// FromFile reads path, strips exactly one trailing "\n" or "\r\n" and returns the Secret.
func FromFile(path string) (Secret, error) {
	s := Ref(path)
	if err := s.Resolve(); err != nil {
		return Secret{}, err
	}
	return s, nil
}

// Resolve reads the referenced file if the Secret is pending. It is a no-op for literals and resolved values.
func (s *Secret) Resolve() error {
	if s.file == "" || s.v != nil {
		return nil
	}
	if !filepath.IsAbs(s.file) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, s.file)
	}
	// Stat follows symlinks (a Kubernetes secret mount is a chain of them); a FIFO or device would block or never end.
	if fi, err := os.Stat(s.file); err != nil {
		return fmt.Errorf("secret file: %w", err)
	} else if !fi.Mode().IsRegular() {
		return fmt.Errorf("secret file %q: not a regular file", s.file)
	}
	f, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("secret file: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return fmt.Errorf("secret file %q: %w", s.file, err)
	}
	if len(b) > MaxFileSize {
		return fmt.Errorf("secret file %q: larger than %d bytes", s.file, MaxFileSize)
	}
	b = bytes.TrimSuffix(bytes.TrimSuffix(b, []byte("\n")), []byte("\r"))
	if len(b) == 0 {
		return fmt.Errorf("secret file %q: %w", s.file, ErrEmpty)
	}
	v := string(b)
	s.v = &v
	return nil
}

// Pending reports a {file: ...} reference that has not been resolved yet.
func (s Secret) Pending() bool { return s.file != "" && s.v == nil }

// Reveal returns the value. Call it only where the credential is written (an Authorization header, a signing key).
func (s Secret) Reveal() string { return s.val() }

// IsZero is true when nothing is configured: no literal and no file reference (GR-13: empty is unset).
func (s Secret) IsZero() bool { return s.val() == "" && s.file == "" }

// Equal compares two secrets in constant time; two unset secrets are equal.
func (s Secret) Equal(o Secret) bool {
	return digestEqual(s.val(), o.val()) && s.file == o.file
}

// digestEqual compares SHA-256 digests in constant time, so neither the content nor the length of either side is observable
// (subtle.ConstantTimeCompare alone returns early when the lengths differ).
func digestEqual(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

// EqualString compares in constant time, so call sites never need Reveal for a header check.
func (s Secret) EqualString(candidate string) bool {
	v := s.val()
	return v != "" && digestEqual(v, candidate)
}

func (s Secret) String() string               { return Redacted }
func (s Secret) GoString() string             { return Redacted }
func (s Secret) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, Redacted) }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }
func (s Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }
func (s Secret) MarshalYAML() (any, error)    { return Redacted, nil }
func (s Secret) LogValue() slog.Value         { return slog.StringValue(Redacted) }

// UnmarshalText is used by the environment layer: SCP_X=literal. Empty means unset.
func (s *Secret) UnmarshalText(b []byte) error { *s = New(string(b)); return nil }

// UnmarshalYAML accepts only {file: /absolute/path}. A scalar is refused so that a config file never holds a credential.
// This is the obsolete-style signature on purpose: it needs no YAML import (the package stays stdlib-only).
func (s *Secret) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]string
	if err := unmarshal(&m); err != nil {
		return errors.New("a secret must be written as {file: /absolute/path}; inline values are not allowed in a config file")
	}
	if len(m) != 1 || m["file"] == "" {
		return errors.New("a secret must be written as {file: /absolute/path}")
	}
	p := m["file"]
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, p)
	}
	*s = Secret{file: p}
	return nil
}
