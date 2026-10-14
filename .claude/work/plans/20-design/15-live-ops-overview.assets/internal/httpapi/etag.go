package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
)

// ETagKind is the letter that starts a weak ETag: the kind of version the first part is.
type ETagKind byte

const (
	ETagSnapshot ETagKind = 'g' // W/"g<snapshot version>-<view class>": snapshot-derived resources
	ETagRepo     ETagKind = 'r' // W/"r<repository revision>-<view class>": per-repository resources
	ETagVersion  ETagKind = 'v' // W/"v<snapshot version>": the snapshot endpoint
)

// WeakETag is WeakETagOf(ETagSnapshot, parts...).
func WeakETag(parts ...any) string { return WeakETagOf(ETagSnapshot, parts...) }

// WeakETagOf renders W/"<kind><part>-<part>...". Parts are formatted with fmt and every byte outside
// [0-9A-Za-z._~:-] is escaped as %xx, so the result is always a valid entity-tag.
// Convention: parts[0] is the snapshot version (or the repository revision), parts[1] the view class.
// The time-derived part of the body (item freshness) is NOT a part: add it with WithDigest (D15).
func WeakETagOf(kind ETagKind, parts ...any) string {
	var b strings.Builder
	b.WriteString(`W/"`)
	b.WriteByte(byte(kind))
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('-')
		}
		for _, c := range []byte(fmt.Sprint(p)) {
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '_' || c == '~' || c == ':' || c == '-' {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, "%%%02x", c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// FreshnessDigest is "" when every status is "fresh" (the common case keeps the short tag) and otherwise a Digest of
// the statuses in order. Pass the derived status of every item that contributes to the body, in body order.
func FreshnessDigest(statuses ...string) string {
	for _, s := range statuses {
		if s != "fresh" {
			return Digest(statuses...)
		}
	}
	return ""
}

// WithDigest appends ".<digest>" inside the quotes of etag; an empty digest returns etag unchanged. A silent
// fresh-to-stale transition (time passing, no new snapshot) therefore changes the tag.
func WithDigest(etag, digest string) string {
	if digest == "" || !strings.HasSuffix(etag, `"`) {
		return etag
	}
	return strings.TrimSuffix(etag, `"`) + "." + digest + `"`
}

// Digest folds arbitrarily many values (for example the derived status of every repository in a list)
// into 16 hex characters (FNV-1a 64). Not cryptographic; collisions only cause a stale 304 for one
// pathological pair of states, and the ETag also carries the snapshot version.
func Digest(parts ...string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// StrongETag is a strong validator over the exact bytes.
func StrongETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:15]) + `"`
}

// NotModified reports whether the request's If-None-Match matches etag (weak comparison, RFC 9110 13.1.2).
// It is false for methods other than GET and HEAD. The caller sends the 304 (ETag and Cache-Control repeated).
func NotModified(r *http.Request, etag string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	vals := r.Header.Values("If-None-Match")
	if len(vals) == 0 || etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, line := range vals {
		for _, tag := range splitETags(line) {
			if tag == "*" || strings.TrimPrefix(tag, "W/") == want {
				return true
			}
		}
	}
	return false
}

// splitETags tokenises an If-None-Match value: "*" or a comma list of [W/]"opaque". Opaque tags may contain commas.
func splitETags(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		if s[i] == '*' {
			out = append(out, "*")
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "W/") {
			i += 2
		}
		if i >= len(s) || s[i] != '"' {
			// malformed token: skip to next comma
			for i < len(s) && s[i] != ',' {
				i++
			}
			continue
		}
		i++
		for i < len(s) && s[i] != '"' {
			i++
		}
		if i >= len(s) {
			break
		}
		i++
		out = append(out, s[start:i])
	}
	return out
}
