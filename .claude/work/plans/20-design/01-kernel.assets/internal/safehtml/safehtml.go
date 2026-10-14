// Package safehtml holds the SafeHTML type and its only mint: the sanitiser. Everything the portal shows as HTML
// (rendered README and documents, static pages) is a SafeHTML, so "was this sanitised" is a property of the type.
package safehtml

import (
	"errors"
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// PolicyVersion identifies the allow-list. It is part of every cache key and stored-HTML record of the render
// pipeline, so changing the policy re-renders safely. Bump it whenever DocsPolicy's observable behaviour changes;
// TestPolicyGolden fails until you do.
const PolicyVersion = 1

// IDPrefix is the prefix every element id must carry to survive sanitising (DOM-clobbering defence). The renderer adds
// it; the sanitiser enforces it.
const IDPrefix = "doc-"

// MaxInputBytes is the largest input Sanitize processes. Larger input yields the fixed notice SafeHTML.
const MaxInputBytes = 8 << 20

// SafeHTML is HTML that passed the sanitiser. The field is unexported and the only functions that build a non-zero
// value are Sanitize and Text. It marshals as text (so an API DTO can carry it) but can never be unmarshalled: decoding
// into a SafeHTML fails loudly, so stored HTML must come back through Sanitize.
type SafeHTML struct{ s string }

// String returns the HTML. It is the only way out and is called at the API boundary.
func (h SafeHTML) String() string { return h.s }

// IsZero reports the empty value.
func (h SafeHTML) IsZero() bool { return h.s == "" }

// MarshalText makes encoding/json emit the HTML as a string.
func (h SafeHTML) MarshalText() ([]byte, error) { return []byte(h.s), nil }

// ErrNotDecodable is returned by every attempt to decode into a SafeHTML.
var ErrNotDecodable = errors.New("safehtml: SafeHTML cannot be decoded; re-sanitise stored HTML with Sanitize")

// UnmarshalText always fails.
func (*SafeHTML) UnmarshalText([]byte) error { return ErrNotDecodable }

// Policy is a constrained bluemonday policy, built once by the caller and shared (it is read-only after construction).
type Policy struct{ p *bluemonday.Policy }

// Sanitize runs untrusted HTML through the policy. A nil policy strips all markup (text only). Input over
// MaxInputBytes is replaced by a fixed notice. The result is a fixpoint: Sanitize(p, Sanitize(p, x)) equals Sanitize(p, x).
func Sanitize(p *Policy, untrusted []byte) SafeHTML {
	if len(untrusted) > MaxInputBytes {
		return SafeHTML{s: "<p>Content too large to display.</p>"}
	}
	if p == nil || p.p == nil {
		return SafeHTML{s: bluemonday.StrictPolicy().Sanitize(string(untrusted))}
	}
	return SafeHTML{s: p.p.Sanitize(string(untrusted))}
}

// Text escapes plain text into a <pre> (unsupported README formats): invalid UTF-8 becomes U+FFFD, NUL is dropped,
// and the rest is html.EscapeString'd. Its output is a fixpoint of Sanitize(DocsPolicy()).
func Text(s string) SafeHTML {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "�")
	return SafeHTML{s: "<pre>" + html.EscapeString(s) + "</pre>"}
}
