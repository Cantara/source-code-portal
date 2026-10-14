// Package job is the contract between features and the sync engine: keys, priorities, specs, outcomes, the Enqueuer
// and the behaviour interfaces the engine uses to classify failures. It knows nothing about events, repositories
// or GitHub; keys are plain strings.
package job

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Key identifies one unit of work: "kind/part/part/...". Repository jobs use three parts (source, owner, name),
// global jobs use the single part "_". The engine, the planner and the store treat it as an opaque string.
type Key string

const (
	maxKeyLen  = 512
	maxPartLen = 200
	// GlobalPart is the only part of a global key.
	GlobalPart = "_"
)

var kindRE = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)*$`)

// ValidKind reports whether kind matches ^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)*$ and is at most 64 bytes.
func ValidKind(kind string) bool { return len(kind) <= 64 && kindRE.MatchString(kind) }

// validPart: 1..200 bytes of valid UTF-8 without '/', white space, control characters (C0, DEL, C1), U+2028 or U+2029 (white space: cross-design CD-JOB-02, so every key matches the admin contract pattern ^[a-z][a-z0-9._-]*/[^\s]+$). (Repository
// parts are ASCII by the scm grammars; the rule is wider so that other kinds can use plain names.)
func validPart(p string) error {
	if p == "" || len(p) > maxPartLen {
		return fmt.Errorf("part %q: length must be 1..%d", truncate(p), maxPartLen)
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("part %q: invalid UTF-8", truncate(p))
	}
	for _, r := range p {
		if r == '/' || unicode.IsControl(r) || unicode.IsSpace(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("part %q: '/', white space and control characters are not allowed", truncate(p))
		}
	}
	return nil
}

func truncate(s string) string {
	if len(s) > 32 {
		return strings.ToValidUTF8(s[:32], "") + "..."
	}
	return s
}

// ParseKey validates a key read from outside the process (persisted plan rows, parked keys, an admin request).
func ParseKey(s string) (Key, error) {
	if len(s) > maxKeyLen {
		return "", fmt.Errorf("job: key longer than %d bytes", maxKeyLen)
	}
	kind, rest, ok := strings.Cut(s, "/")
	if !ok || !ValidKind(kind) {
		return "", fmt.Errorf("job: invalid kind in key %q", truncate(s))
	}
	for part := range strings.SplitSeq(rest, "/") {
		if err := validPart(part); err != nil {
			return "", fmt.Errorf("job: key %q: %w", truncate(s), err)
		}
	}
	return Key(s), nil
}

// NewKey builds a key from a kind and its parts. It panics on an invalid kind, no parts, or a part that is empty,
// contains '/' or a control character: callers build keys from validated typed ids (scm.RepoRef.Validate), so a
// panic is an invariant violation, which the engine's recovery turns into a parked key.
func NewKey(kind string, parts ...string) Key {
	if !ValidKind(kind) || len(parts) == 0 {
		panic(fmt.Sprintf("job: invalid key kind %q or no parts", truncate(kind)))
	}
	var b strings.Builder
	b.WriteString(kind)
	for _, p := range parts {
		if err := validPart(p); err != nil {
			panic("job: " + err.Error())
		}
		b.WriteByte('/')
		b.WriteString(p)
	}
	if b.Len() > maxKeyLen {
		panic("job: key too long")
	}
	return Key(b.String())
}

// GlobalKey is the key of a global job kind: kind + "/_".
func GlobalKey(kind string) Key { return NewKey(kind, GlobalPart) }

func (k Key) String() string { return string(k) }

// Kind is everything before the first '/'.
func (k Key) Kind() string { kind, _, _ := strings.Cut(string(k), "/"); return kind }

// Tail is everything after the first '/' ("source/owner/name" for a repository job, "_" for a global job).
func (k Key) Tail() string { _, rest, _ := strings.Cut(string(k), "/"); return rest }

// Parts splits Tail on '/' into a fresh slice.
func (k Key) Parts() []string {
	t := k.Tail()
	if t == "" {
		return nil
	}
	return strings.Split(t, "/")
}

// IsGlobal reports a global job key.
func (k Key) IsGlobal() bool { return k.Tail() == GlobalPart }
