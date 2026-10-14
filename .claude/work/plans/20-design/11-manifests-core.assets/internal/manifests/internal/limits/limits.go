// Package limits holds the resource bounds every manifest parser runs under:
// the Limits value, the cooperative Guard, the last-resort Watchdog, the weighted
// memory Budget, bounded text/encoding helpers and the interpolation bound.
package limits

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits bound ONE Parse call. Zero fields mean "package default" (WithDefaults).
type Limits struct {
	MaxBytes     int           // input size; default 1 MiB (manifests); the runner passes MaxLockBytes for lockfile parsers
	MaxDepth     int           // nesting depth; default 256
	MaxNodes     int           // tokens/entries; default 2_000_000
	MaxExpand    int           // bytes produced by interpolation per value; default 1 MiB
	MaxExpandAll int           // bytes produced by interpolation per file; default 8 MiB
	Timeout      time.Duration // wall clock enforced through ctx; default 2 s
}

const (
	DefaultMaxBytes     = 1 << 20
	DefaultMaxLockBytes = 8 << 20
	DefaultMaxDepth     = 256
	DefaultMaxNodes     = 2_000_000
	DefaultMaxExpand    = 1 << 20
	DefaultMaxExpandAll = 8 << 20
	DefaultTimeout      = 2 * time.Second
)

// Resolved fills every zero field with its default; a lockfile parser gets the lockfile size default (8 MiB) for MaxBytes.
func (l Limits) Resolved(lockfile bool) Limits {
	if l.MaxBytes <= 0 && lockfile {
		l.MaxBytes = DefaultMaxLockBytes
	}
	return l.WithDefaults()
}

func (l Limits) WithDefaults() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultMaxBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = DefaultMaxNodes
	}
	if l.MaxExpand <= 0 {
		l.MaxExpand = DefaultMaxExpand
	}
	if l.MaxExpandAll <= 0 {
		l.MaxExpandAll = DefaultMaxExpandAll
	}
	if l.Timeout <= 0 {
		l.Timeout = DefaultTimeout
	}
	return l
}

// Limit names double as diagnostic codes ("limit."+name) and as the metric label.
const (
	LimitSize    = "size"
	LimitDepth   = "depth"
	LimitNodes   = "nodes"
	LimitExpand  = "expand"
	LimitTimeout = "timeout"
)

// LimitError is returned by Guard methods. Parsers return it (or wrap it with %w) and the runner turns it into a diagnostic.
type LimitError struct {
	Limit string // one of the Limit* constants
	Max   int
}

func (e *LimitError) Error() string { return fmt.Sprintf("limit.%s exceeded (max %d)", e.Limit, e.Max) }

// Guard is the cooperative limit checker used inside token loops. Not safe for concurrent use (one Guard per Parse call).
type Guard struct {
	ctx    context.Context
	lim    Limits
	nodes  int
	depth  int
	expand int
}

func NewGuard(ctx context.Context, lim Limits) *Guard {
	return &Guard{ctx: ctx, lim: lim.WithDefaults()}
}

// CheckSize rejects an input larger than MaxBytes before any decoding.
func (g *Guard) CheckSize(n int) error {
	if n > g.lim.MaxBytes {
		return &LimitError{LimitSize, g.lim.MaxBytes}
	}
	return nil
}

// Tick is Node under the name unit 13 uses (counts one token, polls ctx every 256 calls).
func (g *Guard) Tick() error { return g.Node() }

// Node counts one token/entry and polls ctx every 256 nodes.
func (g *Guard) Node() error {
	g.nodes++
	if g.nodes > g.lim.MaxNodes {
		return &LimitError{LimitNodes, g.lim.MaxNodes}
	}
	if g.nodes&0xff == 0 {
		return g.Poll()
	}
	return nil
}

// Poll checks cancellation and the deadline (ctx carries Limits.Timeout).
func (g *Guard) Poll() error {
	if err := g.ctx.Err(); err != nil {
		return &LimitError{LimitTimeout, int(g.lim.Timeout / time.Millisecond)}
	}
	return nil
}

// Enter/Leave track nesting depth; Enter fails past MaxDepth.
func (g *Guard) Enter() error {
	g.depth++
	if g.depth > g.lim.MaxDepth {
		return &LimitError{LimitDepth, g.lim.MaxDepth}
	}
	return nil
}
func (g *Guard) Leave() { g.depth-- }

// Expand accounts for n bytes produced by interpolation (per file budget MaxExpandAll).
func (g *Guard) Expand(n int) error {
	g.expand += n
	if n > g.lim.MaxExpand || g.expand > g.lim.MaxExpandAll {
		return &LimitError{LimitExpand, g.lim.MaxExpand}
	}
	return nil
}

// Input validates a text input: strips one UTF-8 BOM, rejects UTF-16 BOMs, NUL bytes and invalid UTF-8.
// XML parsers with a declared single-byte charset use StripBOM + RejectBinary instead (the bytes are not UTF-8).
func Input(data []byte) ([]byte, error) {
	data, err := RejectBinary(StripBOM(data))
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, &EncodingError{Reason: "invalid-utf8"}
	}
	return data, nil
}

// EncodingError maps to the diagnostic code "encoding.unsupported".
type EncodingError struct{ Reason string } // utf16, nul, invalid-utf8, charset:<name>

func (e *EncodingError) Error() string { return "encoding.unsupported: " + e.Reason }

func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// RejectBinary rejects UTF-16/32 BOMs and NUL bytes.
func RejectBinary(b []byte) ([]byte, error) {
	if len(b) >= 2 && ((b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF)) {
		return nil, &EncodingError{Reason: "utf16"}
	}
	for _, c := range b {
		if c == 0 {
			return nil, &EncodingError{Reason: "nul"}
		}
	}
	return b, nil
}

// IsLimit reports whether err is (or wraps) a *LimitError and returns the limit name.
func IsLimit(err error) (string, bool) {
	var le *LimitError
	if errors.As(err, &le) {
		return le.Limit, true
	}
	return "", false
}

// Text makes one untrusted string safe for logs, JSON consumers and LLM outputs: invalid UTF-8 becomes U+FFFD; C0 and C1 controls and DEL
// are removed (tab, CR and LF become one space); bidi controls (U+061C, U+200E/F, U+202A-E, U+2066-9), zero-width characters
// (U+200B-D, U+2060, U+FEFF) and Unicode tag characters (U+E0000-U+E007F, hidden prompt injection) are removed; the result is cut on a rune
// boundary at max bytes.
func Text(s string, max int) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(s), max))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == utf8.RuneError && n == 1:
			r = '\uFFFD'
		case r == '\t' || r == '\n' || r == '\r':
			r = ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			continue
		case IsInvisible(r):
			continue
		}
		if b.Len()+utf8.RuneLen(r) > max {
			return b.String()
		}
		b.WriteRune(r)
	}
	return b.String()
}

// IsInvisible reports characters that are never legitimate in a package name or version: bidi controls, zero-width characters,
// Unicode tags and every other non-printable, non-space rune.
func IsInvisible(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0x200B && r <= 0x200D, r == 0x2060, r == 0xFEFF:
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return !unicode.IsPrint(r) && !unicode.IsSpace(r)
}
