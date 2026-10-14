// Package repopath (rank 1, stdlib only): repository-relative path helpers with escape rejection.
package repopath

import (
	"path"
	"strings"
	"unicode/utf8"
)

// Join resolves rel against dir (both slash-separated, repo-relative, dir "" or "." = root) and returns "" for the root. It reports ok=false when
// the result escapes the repository root, is absolute, contains a backslash, a control character or invalid UTF-8, or exceeds 512 bytes.
func Join(dir, rel string) (string, bool) {
	if rel == "" || len(rel) > 512 || !utf8.ValidString(rel) || strings.ContainsRune(rel, '\\') || hasHidden(rel) || strings.HasPrefix(rel, "/") {
		return "", false
	}
	p := path.Clean(path.Join(dir, rel))
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	if p == "." {
		p = ""
	}
	return p, true
}

// Rel validates a path that a parser read from a file and emits relative to that file's directory (path-independent output). It returns the
// cleaned path ("." for the directory itself), allows leading ".." (only the runner knows the directory and judges escapes), and reports
// ok=false for absolute paths, backslashes, control characters (NUL, CR, LF, DEL, C1), invalid UTF-8, drive letters, the empty string and
// anything over 512 bytes: a path is later used in Properties keys and diagnostics, which must be clean text.
func Rel(p string) (string, bool) {
	if p == "" || len(p) > 512 || !utf8.ValidString(p) || strings.ContainsRune(p, '\\') || strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') {
		return "", false
	}
	if hasHidden(p) {
		return "", false
	}
	return path.Clean(p), true
}

// hasHidden reports control, bidi and zero-width characters.
func hasHidden(p string) bool {
	for _, r := range p {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0xfeff || (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return true
		}
	}
	return false
}

// Dir returns the directory of a file path ("" for root files, never ".").
func Dir(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

// Match reports whether name matches the glob pattern. Segments are matched with path.Match; a "**" segment
// matches zero or more directories. Patterns are bounded (<= 256 bytes, <= 16 segments).
func Match(pattern, name string) bool {
	if len(pattern) > 256 {
		return false
	}
	ps := strings.Split(strings.TrimPrefix(path.Clean(pattern), "./"), "/")
	ns := strings.Split(name, "/")
	if len(ps) > 16 {
		return false
	}
	return match(ps, ns)
}

// match is the dynamic-programming form of segment globbing: O(len(ps) x len(ns)) path.Match calls at most, whatever the pattern ("**/a/**/a/**/b"
// against a deep path is exponential for the recursive form, found by the npm TestWorkspaceGlobBounds under -race).
func match(ps, ns []string) bool {
	n := len(ns)
	next := make([]bool, n+1) // next[j]: ps[i+1:] matches ns[j:]
	cur := make([]bool, n+1)
	next[n] = true
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i] == "**" {
			for j := n; j >= 0; j-- {
				cur[j] = next[j] || (j < n && cur[j+1])
			}
		} else {
			cur[n] = false
			for j := n - 1; j >= 0; j-- {
				ok, err := path.Match(ps[i], ns[j])
				cur[j] = err == nil && ok && next[j+1]
			}
		}
		next, cur = cur, next
	}
	return next[0]
}
