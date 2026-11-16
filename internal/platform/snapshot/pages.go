package snapshot

import "strings"

// The web UI's page addresses (web/src/lib/paths.ts builds the same ones; web/src/lib/paths.cases.json is shared by both tests).

// RepoPage is a repository's page.
func RepoPage(k RepoKey) string {
	owner, name, _ := strings.Cut(string(k), "/")
	return "/repos/" + Segment(owner) + "/" + Segment(name)
}

// GroupPage is a group's page.
func GroupPage(id string) string { return "/groups/" + Segment(id) }

// DocPage is a document's page: its repository path without the extension, each segment escaped.
func DocPage(k RepoKey, path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 && i < len(path)-1 && !strings.Contains(path[i:], "/") {
		path = path[:i]
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = Segment(s)
	}
	return RepoPage(k) + "/docs/" + strings.Join(segs, "/")
}

// Segment escapes one path segment as JavaScript's encodeURIComponent does: everything but A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func Segment(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
