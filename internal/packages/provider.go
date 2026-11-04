package packages

import (
	"context"
	"io"
	"maps"
	"slices"
	"strings"
	gosync "sync"
	"time"
)

// Provider knows one package manager: which build files say what a repository publishes, and how its registries say what the
// newest release is. Providers are pure but for Latest's reads through the Lookup.
type Provider interface {
	Ecosystem() string
	// Wants reports whether a file at path is a build file of this package manager.
	Wants(path string) bool
	// Coordinates reads what the repository publishes from its build files; a package its build file marks private is left out.
	Coordinates(files []File) []Coordinate
	// Latest asks the registries for c's newest release; nil when no registry holds one.
	Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error)
}

// File is a build file of the repository.
type File struct {
	Path    string
	Content []byte
}

// Coordinate is one package a repository publishes.
type Coordinate struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"` // group:artifact for Maven, the package name elsewhere
	Path      string `json:"path"` // the build file it came from
	// Publish are the registries the build file publishes to (Maven's distributionManagement, npm's publishConfig), in order.
	Publish []string `json:"publish,omitempty"`
	// Resolve are the registries the build file reads from (Maven's repositories): where an inherited parent may be.
	Resolve []string `json:"resolve,omitempty"`
	// Parent is Maven's parent group:artifact:version, whose distribution a POM without its own inherits.
	Parent string `json:"parent,omitempty"`
}

// Version is the newest release of a package at its registry.
type Version struct {
	Version   string    `json:"version"`
	Published time.Time `json:"published"` // zero when the registry does not say
	URL       string    `json:"url"`       // the package at its registry, https
	Registry  string    `json:"registry"`  // the configured registry's id, else the registry's host
}

// Registry is a configured registry.
type Registry struct {
	ID         string
	Ecosystem  string
	URL        string      // its root, ending in '/'
	Private    bool        // on the operator's network: not through the public-only guard, http allowed
	Credential *Credential // nil: none
}

// Lookup is what a provider reads registries through during one job: the shared client, the configured registries, whether the
// public registries are asked, and the answers that never change (a released parent POM), kept in the job's payload.
type Lookup struct {
	client     *Client
	registries []Registry
	public     bool
	web        string                   // the repository on GitHub
	tags       func() ([]string, error) // the repository's tags, read once per job when a provider asks

	mu   gosync.Mutex
	kept map[string]string
	used map[string]bool
}

// Get reads u through the shared client.
func (l *Lookup) Get(ctx context.Context, u, accept string, read func(io.Reader) error) error {
	return l.client.Get(ctx, u, accept, read)
}

// Configured is the configured registries of ecosystem, in order.
func (l *Lookup) Configured(ecosystem string) []Registry {
	var out []Registry
	for _, r := range l.registries {
		if r.Ecosystem == ecosystem {
			out = append(out, r)
		}
	}
	return out
}

// Public reports whether the ecosystems' public registries are asked: they are configured to be and the repository is public.
func (l *Lookup) Public() bool { return l.public }

// Tags are the repository's tags (the first 100 GitHub lists), read once per job.
func (l *Lookup) Tags() ([]string, error) { return l.tags() }

// Web is the repository's address on GitHub.
func (l *Lookup) Web() string { return l.web }

// Name is the name of the registry at u in the pages: the configured registry's id, else u's host.
func (l *Lookup) Name(u string) string {
	for _, r := range l.registries {
		if strings.HasPrefix(u, r.URL) {
			return r.ID
		}
	}
	h := strings.TrimPrefix(u, "https://")
	h, _, _ = strings.Cut(h, "/")
	return h
}

// Kept is an answer kept under key, one that never changes.
func (l *Lookup) Kept(key string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.kept[key]
	if ok {
		l.used[key] = true
	}
	return v, ok
}

// Keep keeps an answer that never changes under key.
func (l *Lookup) Keep(key, v string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.kept[key], l.used[key] = v, true
}

// keptUsed is what this job kept or used, for its payload: what no lookup needs any more is dropped with the next job.
func (l *Lookup) keptUsed() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(l.used)) {
		out[k] = l.kept[k]
	}
	return out
}

// registryURL is u as a registry root: https (an http address is asked over https; the public-only guard refuses http anyway) with
// a trailing slash; "" when it is no URL.
func registryURL(u string) string {
	u = strings.TrimSpace(u)
	if rest, ok := strings.CutPrefix(u, "http://"); ok {
		u = "https://" + rest
	}
	if !strings.HasPrefix(u, "https://") || len(u) <= len("https://") || strings.ContainsAny(u, " \t\n?#@") {
		return ""
	}
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// distinct is us without repeats and empties, in order.
func distinct(us ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range us {
		for _, u := range list {
			if u != "" && !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}
