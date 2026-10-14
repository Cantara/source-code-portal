package config

import "log/slog"

// Secret holds a credential. It prints, logs and marshals as [redacted]; Reveal returns the value.
type Secret struct{ v []byte }

// NewSecret wraps b.
func NewSecret(b []byte) Secret { return Secret{v: b} }

// Reveal returns the secret value.
func (s Secret) Reveal() []byte { return s.v }

// IsSet reports whether the secret holds a value.
func (s Secret) IsSet() bool { return len(s.v) > 0 }

func (s Secret) String() string               { return "[redacted]" }
func (s Secret) GoString() string             { return "[redacted]" }
func (s Secret) LogValue() slog.Value         { return slog.StringValue("[redacted]") }
func (s Secret) MarshalText() ([]byte, error) { return []byte("[redacted]"), nil }
