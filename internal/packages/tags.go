package packages

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	goProxy        = "https://proxy.golang.org/"
	goBrowse       = "https://pkg.go.dev/"
	cocoaTrunk     = "https://trunk.cocoapods.org/"
	cocoaBrowse    = "https://cocoapods.org/pods/"
	maxProxyAnswer = 64 << 10
)

var (
	goModule     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~/-]{0,250}$`)
	pseudo       = regexp.MustCompile(`-?\d{14}-[0-9a-f]{12}$`) // a pseudo-version names a commit, not a release
	majorSuffix  = regexp.MustCompile(`/v([2-9]|[1-9][0-9]+)$`)
	swiftPackage = regexp.MustCompile(`Package\s*\(\s*name\s*:\s*"([^"\n]{1,100})"`)
	podspecName  = regexp.MustCompile(`\.name\s*=\s*['"]([A-Za-z0-9_.+-]{1,100})['"]`)
)

// Go is the provider of Go modules: go.mod names the module; a public repository's module is asked of the module proxy (and the
// configured proxies), a private one is never named to a public proxy and its versions are the repository's own tags, as Go
// resolves them.
type Go struct {
	Proxy string // the public module proxy; proxy.golang.org when empty
}

func (g Go) proxy() string { return cmp.Or(g.Proxy, goProxy) }

// Ecosystem is "go".
func (Go) Ecosystem() string { return "go" }

// Wants reports whether path is a go.mod.
func (Go) Wants(p string) bool { return path.Base(p) == "go.mod" }

// Coordinates reads every go.mod's module path.
func (Go) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		sc := bufio.NewScanner(bytes.NewReader(f.Content))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if rest, ok := strings.CutPrefix(line, "module"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
				m := strings.Trim(strings.TrimSpace(strings.SplitN(strings.TrimSpace(rest), "//", 2)[0]), `"`)
				if goModule.MatchString(m) && !strings.Contains(m, "..") {
					out = append(out, Coordinate{Ecosystem: "go", Name: m, Path: f.Path})
				}
				break
			}
		}
	}
	return out
}

// Latest asks the configured proxies and, for a public repository, the public one for the module's @latest; without an answer
// that is a release, the repository's tags for the module's directory.
func (g Go) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	var proxies []string
	for _, r := range l.Configured("go") {
		proxies = append(proxies, r.URL)
	}
	if l.Public() {
		proxies = append(proxies, g.proxy())
	}
	var failed error
	for _, p := range distinct(proxies) {
		v, err := g.latest(ctx, l, p, c.Name)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		return v, nil
	}
	if v, err := g.tagged(l, c); v != nil || err != nil || failed == nil {
		return v, err
	}
	return nil, failed
}

func (g Go) latest(ctx context.Context, l *Lookup, proxy, module string) (*Version, error) {
	var info struct {
		Version string
		Time    time.Time
	}
	err := l.Get(ctx, proxy+escapeModule(module)+"/@latest", "application/json", func(r io.Reader) error {
		return json.NewDecoder(io.LimitReader(r, maxProxyAnswer)).Decode(&info)
	})
	if err != nil {
		return nil, err
	}
	if rel, ok := parseVersion(info.Version); !ok || !rel.stable || pseudo.MatchString(info.Version) {
		return nil, ErrNotFound // no tagged release: @latest falls back to a commit
	}
	out := &Version{Version: info.Version, Published: info.Time.UTC(), Registry: l.Name(proxy)}
	if proxy == goProxy {
		out.URL = goBrowse + module + "@" + info.Version
	}
	return out, nil
}

// tagged is the newest stable tag of the module's directory, in its major version (a module path ending in /vN takes vN tags).
func (Go) tagged(l *Lookup, c Coordinate) (*Version, error) {
	tags, err := l.Tags()
	if err != nil {
		return nil, err
	}
	prefix := ""
	if dir := path.Dir(c.Path); dir != "." {
		prefix = dir + "/"
	}
	major := "v0"
	if m := majorSuffix.FindStringSubmatch(c.Name); m != nil {
		major = "v" + m[1]
	}
	for _, t := range stableTags(tags, prefix) {
		v := strings.TrimPrefix(t, prefix)
		if !strings.HasPrefix(v, "v") {
			continue
		}
		if mj := strings.SplitN(v, ".", 2)[0]; major == "v0" && (mj == "v0" || mj == "v1") || mj == major {
			return &Version{Version: v, Registry: "tags", URL: tagURL(l, t)}, nil
		}
	}
	return nil, nil //nolint:nilnil // no tagged release
}

// escapeModule is a module path as the proxies address it: an upper-case letter as '!' and its lower case.
func escapeModule(m string) string {
	var b strings.Builder
	for _, r := range m {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func tagURL(l *Lookup, tag string) string {
	if !strings.HasPrefix(l.Web(), "https://") {
		return ""
	}
	return strings.TrimSuffix(l.Web(), "/") + "/tree/" + url.PathEscape(tag)
}

// Swift is the provider of Swift packages: Package.swift names the package, and its versions are the repository's tags, which is
// how the Swift Package Manager resolves them.
type Swift struct{}

// Ecosystem is "swift".
func (Swift) Ecosystem() string { return "swift" }

// Wants reports whether path is a Package.swift at the root.
func (Swift) Wants(p string) bool { return p == "Package.swift" }

// Coordinates reads the package's name from Package.swift, read as text: it is Swift code, never run.
func (Swift) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		if m := swiftPackage.FindSubmatch(f.Content); m != nil {
			out = append(out, Coordinate{Ecosystem: "swift", Name: string(m[1]), Path: f.Path})
		}
	}
	return out
}

// Latest is the newest stable tag of the repository.
func (Swift) Latest(_ context.Context, l *Lookup, _ Coordinate) (*Version, error) {
	tags, err := l.Tags()
	if err != nil {
		return nil, err
	}
	if t := stableTags(tags, ""); len(t) > 0 {
		return &Version{Version: t[0], Registry: "tags", URL: tagURL(l, t[0])}, nil
	}
	return nil, nil //nolint:nilnil // no tagged release
}

// CocoaPods is the provider of CocoaPods: a podspec at the root names the pod, CocoaPods trunk says its versions.
type CocoaPods struct {
	Trunk string // trunk's root; trunk.cocoapods.org when empty
}

func (c CocoaPods) trunk() string { return cmp.Or(c.Trunk, cocoaTrunk) }

// Ecosystem is "cocoapods".
func (CocoaPods) Ecosystem() string { return "cocoapods" }

// Wants reports whether path is a podspec at the root.
func (CocoaPods) Wants(p string) bool {
	return !strings.Contains(p, "/") && strings.HasSuffix(p, ".podspec")
}

// Coordinates reads each podspec's name, read as text: it is Ruby code, never run.
func (CocoaPods) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		if m := podspecName.FindSubmatch(f.Content); m != nil {
			out = append(out, Coordinate{Ecosystem: "cocoapods", Name: string(m[1]), Path: f.Path})
		}
	}
	return out
}

// Latest asks trunk for the pod, for a public repository only: trunk is the one CocoaPods registry.
func (c CocoaPods) Latest(ctx context.Context, l *Lookup, co Coordinate) (*Version, error) {
	if !l.Public() {
		return nil, nil //nolint:nilnil // trunk is public: a private repository's pod is not named there
	}
	var pod struct {
		Versions []struct {
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"versions"`
	}
	err := l.Get(ctx, c.trunk()+"api/v1/pods/"+url.PathEscape(co.Name), "application/json", func(r io.Reader) error {
		return json.NewDecoder(io.LimitReader(r, maxMetadata)).Decode(&pod)
	})
	if errors.Is(err, ErrNotFound) {
		return nil, nil //nolint:nilnil // not on trunk
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(pod.Versions))
	for _, v := range pod.Versions {
		names = append(names, v.Name)
	}
	v := newestStable(names)
	if v == "" {
		return nil, nil //nolint:nilnil // no stable version
	}
	out := &Version{Version: v, Registry: "cocoapods.org", URL: cocoaBrowse + url.PathEscape(co.Name)}
	for _, pv := range pod.Versions {
		if pv.Name == v {
			if t, err := time.Parse("2006-01-02 15:04:05 MST", pv.CreatedAt); err == nil {
				out.Published = t.UTC()
			} else if t, err := time.Parse(time.RFC3339, pv.CreatedAt); err == nil {
				out.Published = t.UTC()
			}
		}
	}
	return out, nil
}
