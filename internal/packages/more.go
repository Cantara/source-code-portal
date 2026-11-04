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

	"github.com/cantara/source-code-portal/internal/jsonstream"
)

const (
	nugetFlat      = "https://api.nuget.org/v3-flatcontainer/"
	nugetBrowse    = "https://www.nuget.org/packages/"
	rubygems       = "https://rubygems.org/"
	rubygemsBrowse = "https://rubygems.org/gems/"
	packagist      = "https://repo.packagist.org/"
	packBrowse     = "https://packagist.org/packages/"
	maxVersions    = 20_000
)

var (
	nugetID      = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
	gemspecName  = regexp.MustCompile(`\.name\s*=\s*["']([A-Za-z0-9_.-]{1,100})["']`)
	composerName = regexp.MustCompile(`^[a-z0-9](?:[_.-]?[a-z0-9]+)*/[a-z0-9](?:(?:[_.]|-{1,2})?[a-z0-9]+)*$`)
)

// NuGet is the provider of NuGet feeds (C#, F#): a project file that packs (PackageId, IsPackable or GeneratePackageOnBuild) or a
// .nuspec names the package; the feed's flat container lists its versions.
type NuGet struct {
	Flat string // the public flat container; nuget.org's when empty
}

// Ecosystem is "nuget".
func (NuGet) Ecosystem() string { return "nuget" }

// Wants reports whether path is a .csproj, .fsproj, .vbproj or .nuspec.
func (NuGet) Wants(p string) bool {
	switch path.Ext(p) {
	case ".csproj", ".fsproj", ".vbproj", ".nuspec":
		return true
	}
	return false
}

// Coordinates reads each project that packs, and each .nuspec, for its id: PackageId, else the project file's name.
func (NuGet) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		id, packs := "", false
		_ = walkXML(bytes.NewReader(f.Content), func(at []string, text string) {
			switch {
			case len(at) == 3 && at[0] == "Project" && at[1] == "PropertyGroup":
				switch at[2] {
				case "PackageId":
					id, packs = text, true
				case "IsPackable", "GeneratePackageOnBuild":
					packs = packs || strings.EqualFold(text, "true")
				}
			case strings.Join(at, "/") == "package/metadata/id":
				id, packs = text, true
			}
		})
		if id == "" && packs {
			id = strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path))
		}
		if packs && nugetID.MatchString(id) {
			out = append(out, Coordinate{Ecosystem: "nuget", Name: id, Path: f.Path})
		}
	}
	return out
}

// Latest asks the configured feeds (NuGet v3: the service index at their url) and nuget.org.
func (n NuGet) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	var failed error
	ask := func(flat, registry, browse string) (*Version, error) {
		var idx struct {
			Versions []string `json:"versions"`
		}
		id := strings.ToLower(c.Name)
		err := l.Get(ctx, flat+url.PathEscape(id)+"/index.json", "application/json", func(r io.Reader) error {
			return json.NewDecoder(io.LimitReader(r, maxMetadata)).Decode(&idx)
		})
		if err != nil {
			return nil, err
		}
		v := newestStable(idx.Versions)
		if v == "" {
			return nil, ErrNotFound
		}
		out := &Version{Version: v, Registry: registry}
		if browse != "" {
			out.URL = browse + url.PathEscape(c.Name) + "/" + url.PathEscape(v)
		}
		return out, nil
	}
	for _, r := range l.Configured("nuget") {
		flat, err := n.flat(ctx, l, r.URL)
		if err == nil {
			var v *Version
			if v, err = ask(flat, r.ID, ""); err == nil {
				return v, nil
			}
		}
		if !errors.Is(err, ErrNotFound) {
			failed = cmp.Or(failed, err)
		}
	}
	if l.Public() {
		v, err := ask(cmp.Or(n.Flat, nugetFlat), "nuget.org", nugetBrowse)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return v, err
		}
	}
	return nil, failed
}

// flat is a configured feed's flat container: the PackageBaseAddress its service index (<url>index.json) names. It must lie under
// the feed's url, where its credentials go; a feed that keeps its packages elsewhere is not read.
func (NuGet) flat(ctx context.Context, l *Lookup, feed string) (string, error) {
	var idx struct {
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	err := l.Get(ctx, feed+"index.json", "application/json", func(r io.Reader) error {
		return json.NewDecoder(io.LimitReader(r, maxMetadata)).Decode(&idx)
	})
	if err != nil {
		return "", err
	}
	for _, res := range idx.Resources {
		if strings.HasPrefix(res.Type, "PackageBaseAddress/3.0.0") {
			u := res.ID
			if !strings.HasSuffix(u, "/") {
				u += "/"
			}
			if !under(u, feed) {
				return "", &RegistryError{Host: l.Name(feed), Err: errors.New("the feed's packages are not under its url")}
			}
			return u, nil
		}
	}
	return "", &RegistryError{Host: l.Name(feed), Err: errors.New("the service index names no PackageBaseAddress")}
}

// RubyGems is the provider of gem servers (Ruby): a gemspec at the root names the gem; the server's versions say its newest release
// and when.
type RubyGems struct {
	Server string // the public server; rubygems.org when empty
}

// Ecosystem is "rubygems".
func (RubyGems) Ecosystem() string { return "rubygems" }

// Wants reports whether path is a gemspec at the root.
func (RubyGems) Wants(p string) bool {
	return !strings.Contains(p, "/") && strings.HasSuffix(p, ".gemspec")
}

// Coordinates reads each gemspec's name, read as text: it is Ruby code, never run.
func (RubyGems) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		if m := gemspecName.FindSubmatch(f.Content); m != nil {
			out = append(out, Coordinate{Ecosystem: "rubygems", Name: string(m[1]), Path: f.Path})
		}
	}
	return out
}

// Latest asks the configured gem servers (the compact index Bundler reads, else the versions API) and rubygems.org (its versions
// API, which says when).
func (g RubyGems) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	var failed error
	for _, r := range l.Configured("rubygems") {
		v, err := g.compact(ctx, l, r.URL, c.Name)
		if errors.Is(err, ErrNotFound) {
			v, err = g.versions(ctx, l, r.URL, c.Name)
		}
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			failed = cmp.Or(failed, err)
		default:
			return v, nil
		}
	}
	if l.Public() {
		v, err := g.versions(ctx, l, cmp.Or(g.Server, rubygems), c.Name)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return v, err
		}
	}
	return nil, failed
}

// gemRelease reports whether a gem version is a release: RubyGems takes any letter in it for a pre-release.
func gemRelease(v string) bool {
	return v != "" && !strings.ContainsFunc(v, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' })
}

// compact reads the gem's line per version from the server's compact index (info/<name>): "1.2.0[-platform] deps|checksum".
func (RubyGems) compact(ctx context.Context, l *Lookup, server, name string) (*Version, error) {
	var stable []string
	err := l.Get(ctx, server+"info/"+url.PathEscape(name), "text/plain", func(r io.Reader) error {
		sc := bufio.NewScanner(io.LimitReader(r, maxMetadata))
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if line == "---" || line == "" {
				continue
			}
			v, _, _ := strings.Cut(line, " ")
			v, _, _ = strings.Cut(v, "-") // a platform gem: the same version
			if gemRelease(v) && len(stable) < maxVersions {
				stable = append(stable, v)
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	v := newestStable(stable)
	if v == "" {
		return nil, ErrNotFound
	}
	return &Version{Version: v, Registry: l.Name(server)}, nil
}

// versions reads the gem's versions from the server's versions API (api/v1/versions/<name>.json), with when each was pushed.
func (RubyGems) versions(ctx context.Context, l *Lookup, server, name string) (*Version, error) {
	created := map[string]string{}
	var stable []string
	err := l.Get(ctx, server+"api/v1/versions/"+url.PathEscape(name)+".json", "application/json", func(r io.Reader) error {
		d := jsonstream.New(r)
		return d.Array(func() error {
			var number, at string
			pre := false
			err := d.Object(func(k string) (err error) {
				switch k {
				case "number":
					number, err = d.String(100)
				case "created_at":
					at, err = d.String(64)
				case "prerelease":
					pre, err = d.Bool()
				default:
					err = d.Skip()
				}
				return err
			})
			if !pre && gemRelease(number) && len(stable) < maxVersions {
				stable, created[number] = append(stable, number), at
			}
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	v := newestStable(stable)
	if v == "" {
		return nil, ErrNotFound
	}
	out := &Version{Version: v, Registry: l.Name(server)}
	if server == rubygems {
		out.Registry, out.URL = "rubygems.org", rubygemsBrowse+url.PathEscape(name)+"/versions/"+url.PathEscape(v)
	}
	if t, err := time.Parse(time.RFC3339, created[v]); err == nil {
		out.Published = t.UTC()
	}
	return out, nil
}

// Packagist is the provider of Composer repositories (PHP): composer.json names the package; the repository's p2 metadata (as
// Packagist, Private Packagist and Satis serve it) lists its versions.
type Packagist struct {
	Repository string // the public repository; repo.packagist.org when empty
}

// Ecosystem is "packagist".
func (Packagist) Ecosystem() string { return "packagist" }

// Wants reports whether path is a composer.json.
func (Packagist) Wants(p string) bool { return path.Base(p) == "composer.json" }

// Coordinates reads each composer.json's name; a project (type "project") is an application, not a package.
func (Packagist) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		var cj struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if json.Unmarshal(f.Content, &cj) == nil && cj.Type != "project" && composerName.MatchString(cj.Name) {
			out = append(out, Coordinate{Ecosystem: "packagist", Name: cj.Name, Path: f.Path})
		}
	}
	return out
}

// Latest asks the configured Composer repositories and Packagist for the package's p2 metadata.
func (p Packagist) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	repos := []string{}
	for _, r := range l.Configured("packagist") {
		repos = append(repos, r.URL)
	}
	if l.Public() {
		repos = append(repos, cmp.Or(p.Repository, packagist))
	}
	var failed error
	for _, r := range distinct(repos) {
		v, err := p.metadata(ctx, l, r, c.Name)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		return v, nil
	}
	return nil, failed
}

func (Packagist) metadata(ctx context.Context, l *Lookup, repo, name string) (*Version, error) {
	times := map[string]string{}
	var versions []string
	err := l.Get(ctx, repo+"p2/"+name+".json", "application/json", func(r io.Reader) error {
		d := jsonstream.New(r)
		return d.Object(func(key string) error {
			if key != "packages" {
				return d.Skip()
			}
			return d.Object(func(pkg string) error {
				if pkg != name {
					return d.Skip()
				}
				return d.Array(func() error {
					var v, at string
					err := d.Object(func(k string) (err error) {
						switch k {
						case "version":
							v, err = d.String(100)
						case "time":
							at, err = d.String(64)
						default:
							err = d.Skip()
						}
						return err
					})
					if v != "" && len(versions) < maxVersions {
						versions, times[v] = append(versions, v), at
					}
					return err
				})
			})
		})
	})
	if err != nil {
		return nil, err
	}
	v := newestStable(versions)
	if v == "" {
		return nil, ErrNotFound
	}
	out := &Version{Version: v, Registry: l.Name(repo)}
	if repo == packagist {
		out.Registry, out.URL = "packagist.org", packBrowse+name+"#"+url.PathEscape(v)
	}
	if t, err := time.Parse(time.RFC3339, times[v]); err == nil {
		out.Published = t.UTC()
	}
	return out, nil
}
