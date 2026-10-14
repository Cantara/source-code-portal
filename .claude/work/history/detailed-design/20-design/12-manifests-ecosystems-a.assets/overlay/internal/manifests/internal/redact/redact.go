// Package redact (rank 2, imports only internal/limits (rank 1) for its output cleaner; new package of unit 12, D-4): removal of credentials from URL-like
// strings that parsers copy into Dependency.SourceRef and diagnostics. No parser may emit a URL that did not pass through URL.
package redact

import (
	"net/url"
	"strings"

	"github.com/cantara/source-code-portal/internal/manifests/internal/limits"
)

// secretParam reports query keys whose values are credentials.
func secretParam(k string) bool {
	l := strings.ToLower(k)
	for _, w := range []string{"token", "key", "secret", "pass", "pwd", "auth", "sig", "credential", "apikey", "sas"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// URL strips userinfo ("user:pw@" and a bare token used as the user name), credential-looking query parameters (separated by "&" or ";") and
// credential-looking fragment parameters ("#egg=x&token=y"), and clips the result to 300 bytes.
// Strings that are not URLs (paths, "github:o/r", scp-style "git@host:o/r.git") pass through unchanged apart from cleaning.
func URL(s string) string {
	s = strings.TrimSpace(s)
	frag := ""
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s, frag = s[:i], s[i:]
	}
	query := ""
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s, query = s[:i], s[i+1:]
	}
	// userinfo: after "scheme://", or after a scheme-relative "//" (verification VF-12-07)
	if start, ok := authorityStart(s); ok {
		rest := s[start:]
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			end = len(rest)
		}
		if at := strings.LastIndexByte(rest[:end], '@'); at >= 0 {
			s = s[:start] + rest[at+1:]
		}
	}
	if query != "" {
		var keep []string
		for _, kv := range strings.FieldsFunc(query, func(r rune) bool { return r == '&' || r == ';' }) {
			k, _, _ := strings.Cut(kv, "=")
			if dk, err := url.QueryUnescape(k); err == nil {
				k = dk
			}
			if !secretParam(k) {
				keep = append(keep, kv)
			}
		}
		if len(keep) > 0 {
			s += "?" + strings.Join(keep, "&")
		}
	}
	// a fragment can carry credentials too ("#token=...", "#egg=x&token=y") but also legitimate refs ("#v1.2"): keep refs, drop key=value pairs with secret keys
	if frag != "" && strings.Contains(frag, "=") {
		var keep []string
		for _, kv := range strings.FieldsFunc(strings.TrimPrefix(frag, "#"), func(r rune) bool { return r == '&' || r == ';' }) {
			k, _, _ := strings.Cut(kv, "=")
			if !secretParam(k) {
				keep = append(keep, kv)
			}
		}
		frag = ""
		if len(keep) > 0 {
			frag = "#" + strings.Join(keep, "&")
		}
	}
	return clip(s+frag, 300)
}

// authorityStart returns the index just after "scheme://" or a leading "//".
func authorityStart(s string) (int, bool) {
	if strings.HasPrefix(s, "//") {
		return 2, true
	}
	if i := strings.Index(s, "://"); i > 0 {
		return i + 3, true
	}
	return 0, false
}

// clip removes control, bidi, zero-width and tag characters and cuts at max bytes on a rune boundary (limits.Text: the same rules as
// manifests.CleanText, so a redacted URL is already clean when the root sanitiser sees it).
func clip(s string, max int) string { return limits.Text(s, max) }
