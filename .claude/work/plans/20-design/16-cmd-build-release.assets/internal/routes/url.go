package routes

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

// URL builders: the only place URLs are formatted on the Go side. Every dynamic value is escaped per path segment, so
// no value can add a segment, a dot segment, a query or a fragment. Parameters are plain strings because this package
// imports nothing from the module.

func seg(s string) string { return url.PathEscape(s) }

// escapePath escapes a multi-segment relative path after rooted lexical cleaning: "a/../b" becomes "b" and ".." can
// never climb out of the root; the empty result is "".
func escapePath(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" || p == "." {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = seg(s)
	}
	return strings.Join(parts, "/")
}

func Dashboard() string                   { return "/dashboard" }
func Commits() string                     { return "/commits" }
func Dependencies() string                { return "/dependencies" }
func Group(groupID string) string         { return "/groups/" + seg(groupID) }
func GroupCommits(groupID string) string  { return Group(groupID) + "/commits" }
func Repo(slug string) string             { return "/repos/" + seg(slug) }
func RepoCommits(slug string) string      { return Repo(slug) + "/commits" }
func RepoDependencies(slug string) string { return Repo(slug) + "/dependencies" }
func Page(pageSlug string) string         { return "/pages/" + seg(pageSlug) }

// RepoDoc is the page of a document (docPath is relative to the repository root, '/' separated).
func RepoDoc(slug, docPath string) string { return Repo(slug) + "/docs/" + escapePath(docPath) }

// RepoDocMD is the raw Markdown of a document.
func RepoDocMD(slug, docPath string) string     { return RepoDoc(slug, docPath) }
func ReadmeMD(slug string) string               { return Repo(slug) + "/readme.md" }
func AgentsMD(slug string) string               { return Repo(slug) + "/agents.md" }
func ChangelogMD(slug string) string            { return Repo(slug) + "/changelog.md" }
func RepoDependenciesMD(slug string) string     { return Repo(slug) + "/dependencies.md" }
func GroupDependenciesMD(groupID string) string { return Group(groupID) + "/dependencies.md" }

// Packages is the SPA page of one component; the purl is a query parameter, never a path segment.
func Packages(purl string) string { return "/packages?purl=" + url.QueryEscape(purl) }

// Badge is "/badges/{slug}/{kind}.svg".
func Badge(slug, kind string) string { return "/badges/" + seg(slug) + "/" + seg(kind) + ".svg" }

// BadgeGroup is "/badges/groups/{groupId}/{kind}.svg".
func BadgeGroup(groupID, kind string) string {
	return "/badges/groups/" + seg(groupID) + "/" + seg(kind) + ".svg"
}

// Media is a repository-relative image served by the media proxy.
func Media(slug, ref, filePath string) string {
	return "/media/" + seg(slug) + "/" + seg(ref) + "/" + escapePath(filePath)
}

// MediaExt is a signed external image.
func MediaExt(sig, id string) string { return "/media/ext/" + seg(sig) + "/" + seg(id) }

// LLMs is the llms.txt URL of a scope: "" (the portal), a group id, or "repo:<slug>".
func LLMs(scope string) string { return llmsURL(scope, "llms.txt") }

// LLMsFull is the llms-full.txt URL of a scope.
func LLMsFull(scope string) string { return llmsURL(scope, "llms-full.txt") }

func llmsURL(scope, file string) string {
	switch {
	case scope == "":
		return "/" + file
	case strings.HasPrefix(scope, "repo:"):
		return Repo(strings.TrimPrefix(scope, "repo:")) + "/" + file
	default:
		return Group(scope) + "/" + file
	}
}

func Sitemap() string           { return "/sitemap.xml" }
func Robots() string            { return "/robots.txt" }
func OpenAPI() string           { return "/api/v1/openapi.yaml" }
func Schema(file string) string { return "/schemas/scp/" + seg(file) }
func Webhook(source string) string {
	if source == "" {
		return "/webhooks/github"
	}
	return "/webhooks/github/" + seg(source)
}

// API builds "/api/v1/<segments...>" with every segment escaped.
func API(segments ...string) string {
	var b strings.Builder
	b.WriteString("/api/v1")
	for _, s := range segments {
		b.WriteString("/" + seg(s))
	}
	return b.String()
}

// Absolute joins the externally visible base URL (server.public_url) and a path produced by a builder. base must be
// http or https with a host and no query or fragment; p must start with '/'.
func Absolute(base, p string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("routes: public URL must be http(s)://host[/prefix] without userinfo, query or fragment")
	}
	if !strings.HasPrefix(p, "/") {
		return "", errors.New("routes: path must start with /")
	}
	return strings.TrimSuffix(u.String(), "/") + p, nil
}
