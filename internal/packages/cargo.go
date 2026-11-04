package packages

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/cantara/source-code-portal/internal/jsonstream"
)

const (
	cratesIO       = "https://crates.io/"
	cratesBrowse   = "https://crates.io/crates/"
	maxIndexLine   = 64 << 10 // bytes of one line of a sparse index
	maxIndexLines  = 20_000
	cratesRegistry = "crates-io" // Cargo's name of crates.io in publish = [...]
)

var crateName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// Cargo is the provider of Rust registries: Cargo.toml names the crate and whether and where it publishes; crates.io's API says its
// newest release and when, a configured registry's sparse index its newest version.
type Cargo struct {
	API string // crates.io's root; crates.io when empty
}

func (c Cargo) api() string { return cmp.Or(c.API, cratesIO) }

// Ecosystem is "cargo".
func (Cargo) Ecosystem() string { return "cargo" }

// Wants reports whether path is a Cargo.toml.
func (Cargo) Wants(p string) bool { return path.Base(p) == "Cargo.toml" }

// Coordinates reads every Cargo.toml with a [package] that publishes: publish = false leaves it out, publish = [...] names the
// registries it goes to (crates-io for crates.io). A workspace's root without a [package] names nothing itself.
func (Cargo) Coordinates(files []File) []Coordinate {
	var out []Coordinate
	for _, f := range files {
		var ct struct {
			Package struct {
				Name    string `toml:"name"`
				Publish any    `toml:"publish"`
			} `toml:"package"`
		}
		if toml.Unmarshal(f.Content, &ct) != nil || !crateName.MatchString(ct.Package.Name) {
			continue
		}
		c := Coordinate{Ecosystem: "cargo", Name: ct.Package.Name, Path: f.Path}
		switch pub := ct.Package.Publish.(type) {
		case bool:
			if !pub {
				continue
			}
		case []any:
			if len(pub) == 0 {
				continue
			}
			for _, r := range pub {
				if s, ok := r.(string); ok {
					c.Publish = append(c.Publish, "registry:"+s)
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// Latest asks the registries the crate publishes to (all when it does not say): the configured Cargo registries, by their id when
// it names them, and crates.io.
func (cg Cargo) Latest(ctx context.Context, l *Lookup, c Coordinate) (*Version, error) {
	named := map[string]bool{}
	for _, p := range c.Publish {
		named[strings.TrimPrefix(p, "registry:")] = true
	}
	var failed error
	for _, r := range l.Configured("cargo") {
		if len(named) > 0 && !named[r.ID] {
			continue
		}
		v, err := cg.index(ctx, l, r.URL, c.Name)
		switch {
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		}
		return v, nil
	}
	if l.Public() && (len(named) == 0 || named[cratesRegistry]) {
		v, err := cg.crate(ctx, l, c.Name)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return v, err
		}
	}
	return nil, failed
}

// crate reads crates.io's crate as a stream: its newest stable version and that version's time.
func (cg Cargo) crate(ctx context.Context, l *Lookup, name string) (*Version, error) {
	var newest string
	created := map[string]string{}
	err := l.Get(ctx, cg.api()+"api/v1/crates/"+url.PathEscape(name), "application/json", func(r io.Reader) error {
		d := jsonstream.New(r)
		return d.Object(func(key string) error {
			switch key {
			case "crate":
				return d.Object(func(k string) error {
					if k != "max_stable_version" {
						return d.Skip()
					}
					var err error
					newest, err = d.String(100)
					return err
				})
			case "versions":
				return d.Array(func() error {
					var num, at string
					yanked := false
					err := d.Object(func(k string) (err error) {
						switch k {
						case "num":
							num, err = d.String(100)
						case "created_at":
							at, err = d.String(64)
						case "yanked":
							yanked, err = d.Bool()
						default:
							err = d.Skip()
						}
						return err
					})
					if !yanked && num != "" && len(created) < maxIndexLines {
						created[num] = at
					}
					return err
				})
			}
			return d.Skip()
		})
	})
	if err != nil {
		return nil, err
	}
	if newest == "" {
		newest = newestStable(slices.Collect(maps.Keys(created)))
	}
	if newest == "" {
		return nil, ErrNotFound
	}
	out := &Version{Version: newest, Registry: "crates.io", URL: cratesBrowse + url.PathEscape(name) + "/" + url.PathEscape(newest)}
	if t, err := time.Parse(time.RFC3339, created[newest]); err == nil {
		out.Published = t.UTC()
	}
	return out, nil
}

// index reads a sparse index's file of name: one JSON object per version, the newest stable one not yanked. The index says no time.
func (Cargo) index(ctx context.Context, l *Lookup, root, name string) (*Version, error) {
	var versions []string
	err := l.Get(ctx, root+indexPath(name), "text/plain, application/json", func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 4096), maxIndexLine)
		for n := 0; sc.Scan() && n < maxIndexLines; n++ {
			var e struct {
				Vers   string `json:"vers"`
				Yanked bool   `json:"yanked"`
			}
			if json.Unmarshal(sc.Bytes(), &e) == nil && !e.Yanked {
				versions = append(versions, e.Vers)
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	v := newestStable(versions)
	if v == "" {
		return nil, ErrNotFound
	}
	return &Version{Version: v, Registry: l.Name(root)}, nil
}

// indexPath is where a sparse index keeps a crate: 1/<name>, 2/<name>, 3/<first letter>/<name>, else <two>/<next two>/<name>, in
// lower case.
func indexPath(name string) string {
	n := strings.ToLower(name)
	switch len(n) {
	case 1:
		return "1/" + n
	case 2:
		return "2/" + n
	case 3:
		return "3/" + n[:1] + "/" + n
	}
	return n[:2] + "/" + n[2:4] + "/" + n
}
