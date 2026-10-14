// Package purl is the identity layer of the manifests feature: per-ecosystem normalisation of (namespace, name),
// canonical purl formatting and parsing on top of packageurl-go (the only importer of that library), and
// repository-URL keys for the org graph. It knows ecosystems only as lower-case strings.
package purl

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"

	packageurl "github.com/package-url/packageurl-go"
)

var pep503 = regexp.MustCompile(`[-_.]+`)

// PyPIName implements PEP 503 normalisation (packageurl-go does not: Django_Foo.Bar stays django-foo.bar there).
func PyPIName(s string) string {
	return strings.ToLower(pep503.ReplaceAllString(strings.TrimSpace(s), "-"))
}

// Normalize applies the identity rules of eco (research 07 section 5.1). It is idempotent and total: unknown
// ecosystems get trim-only treatment. The result is the (Namespace, Name) of a manifests.ComponentID.
func Normalize(eco, namespace, name string) (string, string) {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	switch eco {
	case "maven", "gem", "cocoapods":
		return namespace, name // case-sensitive as written (cocoapods: name before the first '/')
	case "npm":
		return trimScope(namespace), name // scope without '@', case as written
	case "pypi":
		return "", PyPIName(name)
	case "golang":
		return strings.ToLower(namespace), strings.ToLower(name)
	case "nuget":
		return "", strings.ToLower(name) // display keeps the original case (Dependency.TargetDisplay)
	case "cargo":
		return "", strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	case "composer", "hex", "github":
		return strings.ToLower(namespace), strings.ToLower(name)
	case "pub":
		return "", strings.ToLower(name)
	case "swift":
		return strings.ToLower(namespace), name
	case "docker":
		return strings.ToLower(namespace), strings.ToLower(name)
	case "terraform", "helm":
		return strings.ToLower(namespace), strings.ToLower(name)
	}
	return namespace, name
}

// trimScope removes any run of leading "@" and spaces (fixed point, so Normalize stays idempotent: found by FuzzNormalizeIdempotent).
func trimScope(s string) string {
	for {
		n := strings.TrimSpace(strings.TrimLeft(s, "@"))
		if n == s {
			return s
		}
		s = n
	}
}

// NormalizeFunc returns Normalize bound to eco (the shape of manifests.EcosystemSpec.Normalize).
func NormalizeFunc(eco string) func(namespace, name string) (string, string) {
	return func(ns, n string) (string, string) { return Normalize(eco, ns, n) }
}

// Type maps an ecosystem id to its purl type; "" when the ecosystem has no official purl type (terraform, helm).
func Type(eco string) string {
	switch eco {
	case "terraform", "helm":
		return ""
	}
	return eco
}

// EcosystemOfType is the inverse of Type for the official types.
func EcosystemOfType(t string) (string, bool) {
	switch t {
	case "maven", "npm", "pypi", "golang", "cargo", "nuget", "gem", "composer", "swift", "pub", "hex", "cocoapods", "docker", "github":
		return t, true
	case "terraform", "helm":
		return t, true // accepted on input as custom types
	}
	return "", false
}

// Build formats the canonical purl string of a component (version and qualifiers optional; qualifiers is a canonical
// "k=v&k2=v2" string with sorted keys). Unofficial ecosystems use the custom types "terraform" and "helm".
func Build(eco, namespace, name, version, qualifiers string) (string, error) {
	if name == "" {
		return "", errors.New("purl: empty name")
	}
	t := Type(eco)
	if t == "" {
		t = eco
	}
	var q packageurl.Qualifiers
	if qualifiers != "" {
		vals, err := url.ParseQuery(qualifiers)
		if err != nil {
			return "", err
		}
		for k, v := range vals {
			if len(v) > 0 && v[0] != "" {
				q = append(q, packageurl.Qualifier{Key: strings.ToLower(k), Value: v[0]})
			}
		}
	}
	p := packageurl.NewPackageURL(t, namespace, name, version, q, "")
	return p.ToString(), nil
}

// Parts is a parsed purl in manifests terms.
type Parts struct {
	Ecosystem  string
	Namespace  string // already normalised
	Name       string // already normalised
	Version    string
	Qualifiers string // canonical k=v&k=v
	Subpath    string
}

// ErrInvalid: the purl does not parse or names an ecosystem manifests does not know.
var ErrInvalid = errors.New("purl: invalid")

// Parse parses s and applies the ecosystem normalisation, so Parse(Build(x)) equals the normalised x.
func Parse(s string) (Parts, error) {
	if len(s) == 0 || len(s) > 1024 || !strings.HasPrefix(s, "pkg:") {
		return Parts{}, ErrInvalid
	}
	p, err := packageurl.FromString(s)
	if err != nil {
		return Parts{}, ErrInvalid
	}
	eco, ok := EcosystemOfType(p.Type)
	if !ok || p.Name == "" {
		return Parts{}, ErrInvalid
	}
	ns, n := Normalize(eco, p.Namespace, p.Name)
	var q []string
	for _, kv := range p.Qualifiers {
		q = append(q, kv.Key+"="+kv.Value)
	}
	sortStrings(q)
	return Parts{eco, ns, n, p.Version, strings.Join(q, "&"), p.Subpath}, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// RepoKey turns a git/https/ssh URL or an npm-style shorthand into the graph key "host/owner/repo" (lower case, no
// scheme, no credentials, no ".git", no ref). ok=false when the value does not name a repository.
func RepoKey(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 2048 {
		return "", false
	}
	s = strings.TrimPrefix(s, "git+")
	switch {
	case strings.HasPrefix(s, "github:"):
		s = "https://github.com/" + strings.TrimPrefix(s, "github:")
	case strings.HasPrefix(s, "gitlab:"):
		s = "https://gitlab.com/" + strings.TrimPrefix(s, "gitlab:")
	case strings.HasPrefix(s, "bitbucket:"):
		s = "https://bitbucket.org/" + strings.TrimPrefix(s, "bitbucket:")
	case !strings.Contains(s, "://") && strings.Contains(s, "@") && strings.Contains(s, ":"):
		// scp-like: git@host:owner/repo.git
		at := strings.Index(s, "@")
		colon := strings.Index(s[at:], ":") + at
		s = "ssh://" + s[at+1:colon] + "/" + s[colon+1:]
	case !strings.Contains(s, "://") && strings.Count(s, "/") == 1 && !strings.ContainsAny(s, " :@"):
		s = "https://github.com/" + s // npm shorthand owner/repo
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" {
		return "", false
	}
	return strings.ToLower(u.Hostname() + "/" + parts[0] + "/" + repo), true
}

// RepoKeyFromModulePath derives the repo key from a module/package path such as "github.com/Org/lib/v2/sub"
// when its first element is one of hosts (lower case). Used for Go modules and Swift namespaces.
func RepoKeyFromModulePath(path string, hosts map[string]bool) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || !hosts[strings.ToLower(parts[0])] {
		return "", false
	}
	return strings.ToLower(parts[0] + "/" + parts[1] + "/" + parts[2]), true
}

// Qualifier is one purl qualifier (key lower-cased on output, empty values dropped).
type Qualifier struct{ Key, Value string }

// Format is the structured form of Build for ecosystem formatters (unit 13): typ is the purl type as written (official or custom),
// subpath becomes the "#subpath" fragment. It never fails: an empty name yields "".
func Format(typ, namespace, name, version string, q []Qualifier, subpath string) string {
	if name == "" || typ == "" {
		return ""
	}
	var qs packageurl.Qualifiers
	for _, x := range q {
		if x.Value != "" && x.Key != "" {
			qs = append(qs, packageurl.Qualifier{Key: strings.ToLower(x.Key), Value: x.Value})
		}
	}
	return packageurl.NewPackageURL(typ, namespace, name, version, qs, strings.Trim(subpath, "/")).ToString()
}

// RepoURL is RepoKey under the name unit 13 uses: the one canonical form shared by channel-B matching and provider code.
func RepoURL(raw string) (canon string, ok bool) { return RepoKey(raw) }

// ParseQualifiers splits a canonical "k=v&k2=v2" string (Dependency.Qualifiers) into qualifiers and the reserved "subpath" key.
func ParseQualifiers(s string) (q []Qualifier, subpath string) {
	vals, err := url.ParseQuery(s)
	if err != nil {
		return nil, ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "subpath" {
			subpath = vals[k][0]
			continue
		}
		q = append(q, Qualifier{k, vals[k][0]})
	}
	return q, subpath
}
