// Package secret: STUB copied from architecture 8.11.
package secret

import "log/slog"

type Secret struct{ v string }

func New(s string) Secret             { return Secret{v: s} }
func (s Secret) Reveal() string       { return s.v }
func (s Secret) IsZero() bool         { return s.v == "" }
func (s Secret) String() string       { return "[REDACTED]" }
func (s Secret) GoString() string     { return "[REDACTED]" }
func (s Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }
