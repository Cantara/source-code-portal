package render

import (
	"net/url"
	"path"
	"strings"
)

// RepoResolver implements Resolver for a document inside a repository at a fixed commit. docs builds it; render never formats a portal URL.
type RepoResolver struct {
	Dir      string                                 // repository directory of the document ("" = root), forward slashes
	DocURL   func(repoPath string) (string, bool)   // indexed document -> portal route (without fragment)
	FileURL  func(repoPath string, dir bool) string // any other repository path -> provider URL at the commit
	MediaURL func(repoPath string) (string, bool)   // repository image -> portal /media URL (ok=false: extension not an image)
	ExtImage func(raw string) (string, bool)        // external https image -> signed portal URL; nil or ok=false drops it
}

// ResolvePath resolves a relative destination against dir with GitHub's rules: a leading "/" is relative to the repository root; the
// result is cleaned; a result that escapes the root is rejected (ok=false). dirLink reports a trailing slash.
func ResolvePath(dir, dest string) (p string, dirLink, ok bool) {
	dirLink = strings.HasSuffix(dest, "/")
	var joined string
	if strings.HasPrefix(dest, "/") {
		joined = dest
	} else {
		joined = "/" + dir + "/" + dest
	}
	c := path.Clean(joined)
	// path.Clean on a rooted path swallows ".." at the root; detect an escape by counting segments instead
	depth := 0
	for _, seg := range strings.Split(joined, "/") {
		switch seg {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return "", false, false
			}
		default:
			depth++
		}
	}
	c = strings.TrimPrefix(c, "/")
	return c, dirLink, true
}

func (r RepoResolver) Link(raw string) (string, bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || hasControl(raw) {
		return "", false, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.User != nil {
			return "", false, false
		}
		return u.String(), true, true
	case "mailto":
		return u.String(), true, true
	case "":
	default:
		return "", false, false
	}
	if u.Host != "" || strings.HasPrefix(raw, "//") || u.Path == "" {
		return "", false, false
	}
	p, dirLink, ok := ResolvePath(r.Dir, u.Path)
	if !ok {
		return "", false, false
	}
	if r.DocURL != nil {
		if route, ok := r.DocURL(p); ok {
			if u.Fragment != "" {
				route += "#" + NamespaceID(slugBase(u.Fragment))
			}
			return route, false, true
		}
	}
	if r.FileURL == nil {
		return "", false, false
	}
	href := r.FileURL(p, dirLink)
	if u.RawQuery != "" {
		href += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		href += "#" + url.PathEscape(u.Fragment)
	}
	return href, true, true
}

func (r RepoResolver) Image(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || hasControl(raw) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if r.ExtImage == nil {
			return "", false
		}
		return r.ExtImage(u.String())
	case "":
	default:
		return "", false // http, data, javascript, file ...
	}
	if u.Host != "" || strings.HasPrefix(raw, "//") || u.Path == "" || r.MediaURL == nil {
		return "", false
	}
	p, _, ok := ResolvePath(r.Dir, u.Path)
	if !ok {
		return "", false
	}
	return r.MediaURL(p)
}

// PageResolver implements Resolver for config-driven pages: relative links name other pages, relative images are not supported.
type PageResolver struct {
	PageURL  func(slug string) (string, bool)
	ExtImage func(raw string) (string, bool)
}

func (r PageResolver) Link(raw string) (string, bool, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || raw == "" || hasControl(raw) {
		return "", false, false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" || u.User != nil {
			return "", false, false
		}
		return u.String(), true, true
	case "mailto":
		return u.String(), true, true
	case "":
	default:
		return "", false, false
	}
	if u.Host != "" || r.PageURL == nil {
		return "", false, false
	}
	base := path.Base(strings.TrimSuffix(strings.TrimSuffix(path.Clean(u.Path), ".markdown"), ".md"))
	href, ok := r.PageURL(base)
	if !ok || strings.Contains(u.Path, "..") {
		return "", false, false
	}
	if u.Fragment != "" {
		href += "#" + NamespaceID(slugBase(u.Fragment))
	}
	return href, false, true
}

func (r PageResolver) Image(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.ToLower(u.Scheme) != "https" || r.ExtImage == nil {
		return "", false
	}
	return r.ExtImage(u.String())
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
